package externalcli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	atevents "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── 测试助手 ────────────────────────────

// newTestMessageManagerFor 建真实内存库消息管理器（leader 视角收信箱）。
// 沿用 tools/message_manager_test.go 的构造惯例（对齐 Python send_message 契约）。
func newTestMessageManagerFor(member string) *tools.TeamMessageManager {
	db := database.NewInMemoryTeamDatabase()
	_ = db.Initialize(context.Background())
	_ = db.CreateTeam(context.Background(), "team1", "Team1", "leader", "", "", "autonomous", false)
	_ = db.CreateMember(context.Background(), "leader", "team1", "Leader", "", "active", "leader", "", "idle", "build_mode", "", "")
	_ = db.CreateMember(context.Background(), member, "team1", member, "", "active", "teammate", "", "idle", "build_mode", "", "")
	return tools.NewTeamMessageManager(db, "team1", member, nil)
}

// fakeReliabilityMessager 最小 Messager 桩：收集 Publish 的 topicID 与事件；
// 进程边界 mock（合规）。
type fakeReliabilityMessager struct {
	topicIDs []string
	events   []*atevents.EventMessage
	fail     bool
}

func (f *fakeReliabilityMessager) Start(context.Context) error { return nil }
func (f *fakeReliabilityMessager) Stop(context.Context) error  { return nil }
func (f *fakeReliabilityMessager) Subscribe(context.Context, string, messager.MessagerHandler) error {
	return nil
}
func (f *fakeReliabilityMessager) Unsubscribe(context.Context, string) error { return nil }
func (f *fakeReliabilityMessager) Send(context.Context, string, *atevents.EventMessage) error {
	return nil
}
func (f *fakeReliabilityMessager) RegisterDirectMessageHandler(context.Context, messager.MessagerHandler) error {
	return nil
}
func (f *fakeReliabilityMessager) UnregisterDirectMessageHandler(context.Context) error {
	return nil
}

func (f *fakeReliabilityMessager) Publish(_ context.Context, topicID string, message *atevents.EventMessage) error {
	if f.fail {
		return errors.New("bus down")
	}
	f.topicIDs = append(f.topicIDs, topicID)
	f.events = append(f.events, message)
	return nil
}

// newTestReliabilityContext 构造默认上下文（真实信箱 + 收集型 messager）。
func newTestReliabilityContext(t *testing.T) (*RuntimeReliabilityContext, *tools.TeamMessageManager, *fakeReliabilityMessager) {
	t.Helper()
	mm := newTestMessageManagerFor("memberB")
	msgr := &fakeReliabilityMessager{}
	ctx := NewRuntimeReliabilityContext(
		"memberB", "team1", "sess-1", "claude",
		mm, msgr, "leader", nil, nil, "",
	)
	return ctx, mm, msgr
}

// ──────────────────────────── 用例 ────────────────────────────

// TestRuntimeReliabilityContext_FinalizeExactlyOnce 对齐 test_runtime_reliability.py
// 之 finalize 只发一条：首次铸 id 并落信箱，重复调用返回 nil 不写第二条。
func TestRuntimeReliabilityContext_FinalizeExactlyOnce(t *testing.T) {
	ctx, mm, _ := newTestReliabilityContext(t)
	ctx.BeginAttempt(atschema.ExternalRuntimePhaseTurn, nil)
	first := ctx.FinalizeFailure(atschema.ExternalRuntimeFailureCategoryRateLimited,
		atschema.NewExternalRuntimeFailureReason("boom", "", "", nil), "turn failed")
	if first == nil || first.FailureID == "" {
		t.Fatalf("first finalize returned %+v", first)
	}
	if first.UserActionRequired {
		t.Fatal("rate_limited 不需要用户介入（USER_ACTION_REQUIRED 闭集）")
	}
	second := ctx.FinalizeFailure(atschema.ExternalRuntimeFailureCategorySDKError,
		atschema.NewExternalRuntimeFailureReason("late", "", "", nil), "late candidate")
	if second != nil {
		t.Fatalf("repeated finalize must return nil, got %+v", second)
	}
	if ctx.FailureID() != first.FailureID || !ctx.HasFinalized() {
		t.Fatalf("failure_id accessor = %q", ctx.FailureID())
	}
	msgs, err := mm.GetMessages(context.Background(), "leader", false, "")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("mailbox messages = %d, want 1", len(msgs))
	}
	if msgs[0].FromMemberName != "memberB" || msgs[0].ToMemberName == nil || *msgs[0].ToMemberName != "leader" {
		t.Fatalf("mailbox route = %s→%s", msgs[0].FromMemberName, *msgs[0].ToMemberName)
	}
	var failure atschema.ExternalRuntimeFailure
	if err := json.Unmarshal([]byte(msgs[0].Content), &failure); err != nil {
		t.Fatalf("content not JSON: %v", err)
	}
	if failure.FailureID != first.FailureID || failure.Phase != atschema.ExternalRuntimePhaseTurn ||
		failure.Category != atschema.ExternalRuntimeFailureCategoryRateLimited {
		t.Fatalf("failure = %+v", failure)
	}
	if !strings.Contains(msgs[0].Content, `"type":"external_runtime_failed"`) {
		t.Fatalf("content missing type discriminator: %s", msgs[0].Content)
	}
}

// TestRuntimeReliabilityContext_BeginAttemptClears 对齐 begin_attempt 清空。
func TestRuntimeReliabilityContext_BeginAttemptClears(t *testing.T) {
	ctx, _, _ := newTestReliabilityContext(t)
	round := 3
	ctx.BeginAttempt(atschema.ExternalRuntimePhaseTurn, &round)
	ctx.RecordPending(atschema.ExternalRuntimeFailureCategoryRateLimited,
		atschema.NewExternalRuntimeFailureReason("429", "", "", nil))
	if !ctx.HasPending() {
		t.Fatal("pending expected")
	}
	ctx.BeginAttempt(atschema.ExternalRuntimePhaseStartup, nil)
	if ctx.HasPending() || ctx.HasFinalized() {
		t.Fatal("attempt state not cleared")
	}
	if ctx.Phase() != atschema.ExternalRuntimePhaseStartup || ctx.RoundID() != nil {
		t.Fatalf("phase/round = %s/%v", ctx.Phase(), ctx.RoundID())
	}
}

// TestRuntimeReliabilityContext_PendingSpecificity 对齐 record_pending 覆盖规则：
// 更强结构化信号才覆盖 category；reason merge 保留旧结构化字段与新 message。
func TestRuntimeReliabilityContext_PendingSpecificity(t *testing.T) {
	ctx, _, _ := newTestReliabilityContext(t)
	ctx.BeginAttempt(atschema.ExternalRuntimePhaseTurn, nil)
	ctx.RecordPending(atschema.ExternalRuntimeFailureCategorySDKError,
		atschema.NewExternalRuntimeFailureReason("first message", "Timeout", "code-1", nil))
	// 更弱（无结构化信号）→ category 保持，message 取新非空、其余保留旧
	ctx.RecordPending(atschema.ExternalRuntimeFailureCategoryUnknown,
		atschema.NewExternalRuntimeFailureReason("second", "", "", nil))
	if ctx.PendingCategory() != atschema.ExternalRuntimeFailureCategorySDKError {
		t.Fatalf("category = %s", ctx.PendingCategory())
	}
	if got := ctx.PendingReason(); got.Message != "second" || got.SDKErrorType != "Timeout" || got.SDKErrorCode != "code-1" {
		t.Fatalf("reason merge = %+v", got)
	}
	// 等强（仅 http_status，score 2 == 旧 score 2）→ category 不覆盖（Python 严格 >），
	// 但 http_status 并入 reason
	status := 429
	ctx.RecordPending(atschema.ExternalRuntimeFailureCategoryRateLimited,
		atschema.NewExternalRuntimeFailureReason("", "", "", &status))
	if ctx.PendingCategory() != atschema.ExternalRuntimeFailureCategorySDKError {
		t.Fatalf("category = %s", ctx.PendingCategory())
	}
	if ctx.PendingReason().HTTPStatus == nil || *ctx.PendingReason().HTTPStatus != 429 {
		t.Fatalf("http_status merge = %+v", ctx.PendingReason())
	}
}

// TestRuntimeReliabilityContext_FinalizedRejectsLatePending 对齐
// 已 finalize 后迟到候选丢弃（reliability.py:191-193）。
func TestRuntimeReliabilityContext_FinalizedRejectsLatePending(t *testing.T) {
	ctx, mm, _ := newTestReliabilityContext(t)
	ctx.BeginAttempt(atschema.ExternalRuntimePhaseTurn, nil)
	ctx.FinalizeFailure(atschema.ExternalRuntimeFailureCategorySDKError,
		atschema.NewExternalRuntimeFailureReason("first", "", "", nil), "failed")
	ctx.RecordPending(atschema.ExternalRuntimeFailureCategoryRateLimited,
		atschema.NewExternalRuntimeFailureReason("late", "", "", nil))
	if ctx.HasPending() {
		t.Fatal("late candidate after finalize must be dropped")
	}
	msgs, _ := mm.GetMessages(context.Background(), "leader", false, "")
	if len(msgs) != 1 {
		t.Fatalf("mailbox messages = %d, want 1", len(msgs))
	}
}

// TestRuntimeReliabilityContext_PublishRetryingBestEffort 对齐 publish_retrying：
// 无 messager 降级 + publish 失败不抛；正常路径发团队 topic 事件。
func TestRuntimeReliabilityContext_PublishRetryingBestEffort(t *testing.T) {
	// 无 messager：静默降级
	none := NewRuntimeReliabilityContext("m", "team1", "s", "claude", nil, nil, "leader", nil, nil, "")
	if err := none.PublishRetrying(atschema.ExternalRuntimeFailureCategoryRateLimited, nil, "s"); err != nil {
		t.Fatalf("no-messager publish must not error: %v", err)
	}
	// publish 失败：只记日志不抛
	failing := &fakeReliabilityMessager{fail: true}
	ctxFail := NewRuntimeReliabilityContext("m", "team1", "s", "claude", nil, failing, "leader", nil, nil, "")
	if err := ctxFail.PublishRetrying(atschema.ExternalRuntimeFailureCategoryRateLimited, nil, "s"); err != nil {
		t.Fatalf("publish failure must be logged not raised: %v", err)
	}
	// 正常路径：事件进团队 topic
	ctx, _, msgr := newTestReliabilityContext(t)
	round := 2
	ctx.BeginAttempt(atschema.ExternalRuntimePhaseTurn, &round)
	status := 429
	if err := ctx.PublishRetrying(atschema.ExternalRuntimeFailureCategoryRateLimited,
		atschema.NewExternalRuntimeFailureReason("limit", "", "", &status), "rate limited"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(msgr.topicIDs) != 1 {
		t.Fatalf("published = %d, want 1", len(msgr.topicIDs))
	}
}

// TestRuntimeReliabilityContext_MarkMemberError 对齐经回调翻 ERROR + 回调失败不抛。
func TestRuntimeReliabilityContext_MarkMemberError(t *testing.T) {
	var got atschema.MemberStatus
	ctx := NewRuntimeReliabilityContext("m", "team1", "s", "claude", nil, nil, "leader",
		func(_ context.Context, s atschema.MemberStatus) error {
			got = s
			return nil
		}, nil, "")
	if err := ctx.MarkMemberError(); err != nil {
		t.Fatalf("mark error: %v", err)
	}
	if got != atschema.MemberStatusError {
		t.Fatalf("status = %v", got)
	}
	// 回调失败照常回传（调用方 _ = 显式忽略），不吞错误信息
	ctxFail := NewRuntimeReliabilityContext("m", "team1", "s", "claude", nil, nil, "leader",
		func(context.Context, atschema.MemberStatus) error { return errors.New("db down") }, nil, "")
	if err := ctxFail.MarkMemberError(); err == nil || err.Error() != "db down" {
		t.Fatalf("status-cb failure 应回传: %v", err)
	}
}

// TestRuntimeReliabilityContext_ModelAccessors 对齐 update_model/clear_model。
func TestRuntimeReliabilityContext_ModelAccessors(t *testing.T) {
	ctx, _, _ := newTestReliabilityContext(t)
	ctx.UpdateModel(nil) // nil 忽略
	m := "  claude-3  "
	ctx.UpdateModel(&m)
	if ctx.Model() != "claude-3" {
		t.Fatalf("model = %q", ctx.Model())
	}
	ctx.ClearModel()
	if ctx.Model() != "" {
		t.Fatalf("model after clear = %q", ctx.Model())
	}
}

// TestRuntimeReliabilityContext_NoMessageManagerUndelivered 对齐
// message_manager 缺失路径：finalize 不抛、失败消息未投递（日志路径）。
func TestRuntimeReliabilityContext_NoMessageManagerUndelivered(t *testing.T) {
	ctx := NewRuntimeReliabilityContext("m", "team1", "s", "claude", nil, nil, "leader", nil, nil, "")
	ctx.BeginAttempt(atschema.ExternalRuntimePhaseStartup, nil)
	f := ctx.FinalizeFailure(atschema.ExternalRuntimeFailureCategoryProcessStartFailed,
		atschema.NewExternalRuntimeFailureReason("no cli", "StartError", "", nil), "startup failed")
	if f == nil || !f.UserActionRequired {
		t.Fatalf("process_start_failed 需要用户介入，got %+v", f)
	}
	if f.CLIPath != nil {
		t.Fatalf("空 cli_path 应为 nil（Python excluded_fields），got %q", *f.CLIPath)
	}
}

// TestRuntimeReliabilityContext_SuggestedActionI18n 对齐
// _default_suggested_action（类别键命中 i18n 文案）。
func TestRuntimeReliabilityContext_SuggestedActionI18n(t *testing.T) {
	ctx, _, _ := newTestReliabilityContext(t)
	ctx.BeginAttempt(atschema.ExternalRuntimePhaseTurn, nil)
	f := ctx.FinalizeFailure(atschema.ExternalRuntimeFailureCategoryAuthRequired,
		atschema.NewExternalRuntimeFailureReason("bad key", "", "", nil), "auth failed")
	if f == nil || !strings.Contains(f.SuggestedAction, "API key") {
		t.Fatalf("suggested_action = %q", f.SuggestedAction)
	}
}
