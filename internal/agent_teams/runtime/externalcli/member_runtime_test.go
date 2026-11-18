package externalcli

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// 编译期接口满足断言：三种成员运行时实现 agent.MemberRuntime
// （Python MemberRuntime Protocol 的 Go 承载）。agent 包不反向 import
// externalcli（生产零反向依赖），断言置于测试侧。
var (
	_ agent.MemberRuntime = (*ExternalCliRuntime)(nil)
	_ agent.MemberRuntime = (*ReinvokeCliRuntime)(nil)
	_ agent.MemberRuntime = (*ExternalHarnessMemberRuntime)(nil)

	_ agent.TaskNoticeAwareRuntime = (*ExternalCliRuntime)(nil)
	_ agent.TaskNoticeAwareRuntime = (*ReinvokeCliRuntime)(nil)
	_ agent.TaskNoticeAwareRuntime = (*ExternalHarnessMemberRuntime)(nil)
)

// ──────────────────────────── 测试替身 ────────────────────────────

// fakeMemberSession member_runtime 测试用最小 SessionFacade 实现。
type fakeMemberSession struct {
	states      map[string]any
	committed   int
	postRunDone bool
}

func newFakeMemberSession() *fakeMemberSession {
	return &fakeMemberSession{states: map[string]any{}}
}

func (s *fakeMemberSession) GetSessionID() string { return "member-sess" }
func (s *fakeMemberSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{}
}
func (s *fakeMemberSession) GetParentSessionID() string { return "" }
func (s *fakeMemberSession) UpdateState(data map[string]any) {
	for k, v := range data {
		s.states[k] = v
	}
}
func (s *fakeMemberSession) GetState(key state.StateKey) (any, error) {
	v, ok := s.states[key.String()]
	if !ok {
		return nil, nil
	}
	return v, nil
}
func (s *fakeMemberSession) DumpState() map[string]any                  { return s.states }
func (s *fakeMemberSession) WriteStream(_ context.Context, _ any) error { return nil }
func (s *fakeMemberSession) WriteCustomStream(_ context.Context, _ any) error {
	return nil
}
func (s *fakeMemberSession) GetEnv(_ string, _ ...any) any { return nil }
func (s *fakeMemberSession) Interact(_ context.Context, _ any) error {
	return nil
}
func (s *fakeMemberSession) ClearSession(_ context.Context) {}
func (s *fakeMemberSession) Commit(_ context.Context) error {
	s.committed++
	return nil
}
func (s *fakeMemberSession) PostRun(_ context.Context) error {
	s.postRunDone = true
	return nil
}

// 编译期确认 fake 满足 facade 与 commit 面。
var (
	_ sessioninterfaces.SessionFacade = (*fakeMemberSession)(nil)
	_ memberSessionCommitter          = (*fakeMemberSession)(nil)
)

// fakeHarnessProtocol member_runtime 构造测试用的最小 HarnessProtocol。
type fakeHarnessProtocol struct{}

func (h *fakeHarnessProtocol) Card() harness_protocol.HarnessCard {
	return harness_protocol.HarnessCard{Name: "fake-provider"}
}
func (h *fakeHarnessProtocol) State() harness_protocol.HarnessState {
	return harness_protocol.HarnessStateIdle
}
func (h *fakeHarnessProtocol) ProviderSessionID() *string { return nil }
func (h *fakeHarnessProtocol) EventBufferConfig() harness_protocol.EventBufferConfig {
	return harness_protocol.EventBufferConfig{}
}
func (h *fakeHarnessProtocol) Start(_ context.Context, _ harness_protocol.HarnessContext) error {
	return nil
}
func (h *fakeHarnessProtocol) Stop(_ context.Context) error { return nil }
func (h *fakeHarnessProtocol) Events() harness_protocol.HarnessEventCursor {
	return nil
}
func (h *fakeHarnessProtocol) TurnEvents(_ *string) harness_protocol.HarnessEventCursor {
	return nil
}
func (h *fakeHarnessProtocol) Send(_ context.Context, _ harness_protocol.HarnessInput,
	_ harness_protocol.DeliveryMode) (harness_protocol.SendReceipt, error) {
	return harness_protocol.SendReceipt{}, nil
}
func (h *fakeHarnessProtocol) Abort(_ context.Context, _ harness_protocol.AbortMode) error {
	return nil
}
func (h *fakeHarnessProtocol) Pause(_ context.Context) error { return nil }
func (h *fakeHarnessProtocol) Resume(_ context.Context, _ *harness_protocol.HarnessInput) error {
	return nil
}
func (h *fakeHarnessProtocol) ExportCheckpoint(_ context.Context) (*harness_protocol.HarnessCheckpoint, error) {
	return nil, nil
}

var _ harness_protocol.HarnessProtocol = (*fakeHarnessProtocol)(nil)

// ──────────────────────────── 测试 ────────────────────────────

func TestCheckpointToDict_往返(t *testing.T) {
	rev := "r1"
	providerSession := "ps-1"
	cp := &harness_protocol.HarnessCheckpoint{
		Provider:          "codex",
		SchemaVersion:     "1",
		AgentID:           "a1",
		HostSessionID:     "h1",
		CheckpointID:      "c1",
		Sequence:          3,
		Data:              map[string]any{"k": "v"},
		ProviderSessionID: &providerSession,
		Revision:          &rev,
	}
	dict := CheckpointToDict(cp)
	if dict["checkpoint_id"] != "c1" || dict["sequence"] != 3 {
		t.Fatalf("序列化字段不符: %v", dict)
	}
	back := CheckpointFromDict(dict)
	if back == nil || back.Provider != "codex" || back.Sequence != 3 || back.CheckpointID != "c1" {
		t.Fatalf("反序列化漂移: %+v", back)
	}
	if back.ProviderSessionID == nil || *back.ProviderSessionID != "ps-1" {
		t.Fatalf("provider_session_id 丢失: %+v", back)
	}
}

func TestCheckpointFromDict_非法形状返回nil(t *testing.T) {
	if got := CheckpointFromDict("not-a-map"); got != nil {
		t.Fatalf("非 dict 输入应返回 nil，got %v", got)
	}
	if got := CheckpointFromDict(map[string]any{"provider": 42}); got != nil {
		t.Fatalf("坏 payload 应返回 nil，got %v", got)
	}
}

func TestReadMemberCheckpoint_后端不匹配返回nil(t *testing.T) {
	sess := newFakeMemberSession()
	sess.UpdateState(map[string]any{
		externalRuntimeStateKey: map[string]any{
			externalBackendKey:    "codex",
			externalCheckpointKey: CheckpointToDict(&harness_protocol.HarnessCheckpoint{CheckpointID: "c1"}),
		},
	})
	if got := ReadMemberCheckpoint(sess, "claude"); got != nil {
		t.Fatalf("后端不匹配应返回 nil，got %+v", got)
	}
	got := ReadMemberCheckpoint(sess, "codex")
	if got == nil || got.CheckpointID != "c1" {
		t.Fatalf("同后端应读回 checkpoint，got %+v", got)
	}
}

func TestMemberSessionCheckpointSink_Save写状态并提交(t *testing.T) {
	sess := newFakeMemberSession()
	sink := NewMemberSessionCheckpointSink(sess, "codex")
	receipt, err := sink.Save(&harness_protocol.HarnessCheckpoint{
		CheckpointID: "cp-1",
		Sequence:     7,
	}, harness_protocol.CheckpointReasonSessionActivated, "")
	if err != nil {
		t.Fatalf("Save 报错: %v", err)
	}
	if receipt.CheckpointID != "cp-1" || receipt.Sequence != 7 || receipt.StorageRevision != "cp-1" {
		t.Fatalf("回执漂移: %+v", receipt)
	}
	if sess.committed != 1 {
		t.Fatalf("save 必须提交会话，committed=%d", sess.committed)
	}
	if ReadMemberCheckpoint(sess, "codex") == nil {
		t.Fatal("checkpoint 应已写入成员会话状态")
	}
}

func TestNewExternalHarnessMemberRuntime_缺上下文工厂报错(t *testing.T) {
	if _, err := NewExternalHarnessMemberRuntime(&fakeHarnessProtocol{}, nil,
		ExternalHarnessMemberRuntimeOptions{}); err == nil {
		t.Fatal("contextSource 为 nil 必须报错")
	}
}

func TestExternalHarnessMemberRuntime_构造默认值与只读面(t *testing.T) {
	r, err := NewExternalHarnessMemberRuntime(&fakeHarnessProtocol{},
		func(_ any) (*harness_protocol.HarnessContext, error) {
			return &harness_protocol.HarnessContext{}, nil
		},
		ExternalHarnessMemberRuntimeOptions{AgentKind: "codex"})
	if err != nil {
		t.Fatalf("构造报错: %v", err)
	}
	if r.MCPServerName != TeamMCPServerName {
		t.Fatalf("MCPServerName 默认应为 %q，got %q", TeamMCPServerName, r.MCPServerName)
	}
	if r.ProviderName() != "fake-provider" || r.memberName != "fake-provider" {
		t.Fatalf("成员名应兜底取卡片名")
	}
	if r.ReliabilityAgentKind() != "codex" || r.BackgroundTaskController() != nil ||
		r.Workspace() != nil || r.SysOperation() != nil {
		t.Fatalf("只读面漂移")
	}
}

func TestExternalHarnessMemberRuntime_绑定槽位(t *testing.T) {
	r, _ := NewExternalHarnessMemberRuntime(&fakeHarnessProtocol{},
		func(_ any) (*harness_protocol.HarnessContext, error) {
			return &harness_protocol.HarnessContext{}, nil
		}, ExternalHarnessMemberRuntimeOptions{})
	// tracker 槽位
	r.BindTeamContextTracker(nil)
	r.BindTaskOutcomeTracker(nil)
	// MCP / tools / fallback / activity / trajectory / teardown
	r.BindMCPServers([]harness_protocol.McpServerConfig{{Name: "team"}})
	r.BindTools(nil)
	r.BindFallbackPromotion(func(_ context.Context) (bool, error) { return true, nil })
	sinkCalled := 0
	r.BindActivitySink(func(_ *AgentActivityEvent) { sinkCalled++ }, "a1", "phase", "label", "corr")
	var recorder *harnessproviders.HarnessTrajectoryRecorder
	r.BindTrajectoryRecorder(recorder)
	r.AddTeardownHook(func(_ context.Context) error { return nil })
	if len(r.extraMCPServers) != 1 {
		t.Fatal("BindMCPServers 必须追加进 extraMCPServers 槽位")
	}
	if r.promoteFallbackModel == nil || r.activitySink == nil || len(r.teardownHooks) != 1 {
		t.Fatal("绑定槽位未落位")
	}
	// agent_kind 为空时 BindReliabilityContext 为 no-op
	r.BindReliabilityContext("s", nil, "leader", nil, nil)
	if r.reliabilityCtx != nil {
		t.Fatal("agent_kind 为空不得构造可靠性上下文")
	}
}

// TestBindReliabilityContext_真构造 对齐 Python bind_reliability_context
// （member_runtime.py:398-421）：agent_kind 非空时构造并注入上下文；
// team_backend 为 nil 时 team_name=""、message_manager=nil。
func TestBindReliabilityContext_真构造(t *testing.T) {
	r2, _ := NewExternalHarnessMemberRuntime(&fakeHarnessProtocol{},
		func(_ any) (*harness_protocol.HarnessContext, error) {
			return &harness_protocol.HarnessContext{}, nil
		}, ExternalHarnessMemberRuntimeOptions{AgentKind: "claude", CLIPath: "/bin/fake-cli"})
	r2.BindReliabilityContext("sess-1", nil, "leader", nil, nil)
	ctx := r2.reliabilityCtxSnap()
	if ctx == nil {
		t.Fatal("agent_kind 非空必须构造可靠性上下文")
	}
	if ctx.cliPath != "/bin/fake-cli" {
		t.Fatalf("cliPath = %q", ctx.cliPath)
	}
}

func TestExternalHarnessMemberRuntime_成员会话助手(t *testing.T) {
	r, _ := NewExternalHarnessMemberRuntime(&fakeHarnessProtocol{},
		func(_ any) (*harness_protocol.HarnessContext, error) {
			return &harness_protocol.HarnessContext{}, nil
		}, ExternalHarnessMemberRuntimeOptions{})
	// pending/commit：tracker 与会话为 nil 时 no-op
	r.pendingTeamContext()
	r.commitTeamContext()
	// ensureMemberSession：无团队会话 → nil
	sess, err := r.ensureMemberSession(nil)
	if err != nil || sess != nil {
		t.Fatalf("无团队会话应返回 nil 会话: %v %v", sess, err)
	}
	// finalize：无会话 no-op
	if err := r.finalizeMemberSession(context.Background()); err != nil {
		t.Fatalf("finalize 无会话应 no-op: %v", err)
	}
}

// 鸭子路径对真实团队会话命中：ensureMemberSession 的接口形状必须与
// AgentTeamSession.CreateAgentSession（agent_team.go:371）逐参对齐，否则该
// 路径对真实会话恒静默返回 nil（Task 10 集成测试实施中发现的缺陷）。
// Python: member_runtime.py:947 hasattr + create_agent_session。
func TestExternalHarnessMemberRuntime_成员会话鸭子路径命中真实会话(t *testing.T) {
	r, _ := NewExternalHarnessMemberRuntime(&fakeHarnessProtocol{},
		func(_ any) (*harness_protocol.HarnessContext, error) {
			return &harness_protocol.HarnessContext{}, nil
		}, ExternalHarnessMemberRuntimeOptions{})
	r.memberAgentID = "pay_beta"
	teamSession := session.CreateAgentTeamSession("duck-sess", nil, "pay")
	sess, err := r.ensureMemberSession(teamSession)
	if err != nil {
		t.Fatalf("ensureMemberSession: %v", err)
	}
	if sess == nil {
		t.Fatal("真实团队会话必须命中鸭子路径并创建成员子会话")
	}
	if r.memberSession == nil {
		t.Fatal("成员会话槽位必须落位")
	}
	// 幂等：二次调用返回同一会话
	sess2, err := r.ensureMemberSession(teamSession)
	if err != nil || sess2 != sess {
		t.Fatalf("ensureMemberSession 必须幂等: %v %v", sess2, err)
	}
	// 收尾清槽位
	if err := r.finalizeMemberSession(context.Background()); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if r.memberSession != nil {
		t.Fatal("finalize 后成员会话槽位必须清空")
	}
}

func TestMemberRoundKind_全映射(t *testing.T) {
	cases := map[harness_protocol.TurnEventKind]string{
		harness_protocol.TurnEventKindStarted:  "started",
		harness_protocol.TurnEventKindPaused:   "paused",
		harness_protocol.TurnEventKindResumed:  "started",
		harness_protocol.TurnEventKindFinished: "finished",
		harness_protocol.TurnEventKindAborted:  "aborted",
		harness_protocol.TurnEventKindFailed:   "failed",
	}
	for kind, want := range cases {
		if got := memberRoundKind(kind); got != want {
			t.Fatalf("kind %s → %s, want %s", kind, got, want)
		}
	}
}

func TestActivityName_与Preview(t *testing.T) {
	if activityName("bash") != "bash" || activityName(42) != "" || activityName("") != "" {
		t.Fatal("activityName 漂移")
	}
	if activityPreview(nil) != "" || activityPreview("raw") != "raw" {
		t.Fatal("activityPreview 字符串分支漂移")
	}
	got := activityPreview(map[string]any{"a": 1})
	if !strings.Contains(got, `"a":1`) {
		t.Fatalf("结构化载荷应单行紧凑 dump，got %q", got)
	}
}

func TestPrependContext_三形状(t *testing.T) {
	// 字符串内容
	out := prependContext(mustHarnessInput(t, "hello"), "CLOCK")
	if s, ok := out.Content.(string); !ok || s != "CLOCK\n\nhello" {
		t.Fatalf("字符串分支漂移: %#v", out.Content)
	}
	// 空字符串内容
	out = prependContext(mustHarnessInput(t, ""), "CLOCK")
	if s, ok := out.Content.(string); !ok || s != "CLOCK" {
		t.Fatalf("空内容分支漂移: %#v", out.Content)
	}
	// 全 dict 列表 → 头部前置文本块
	blocks := []any{map[string]any{"type": "text", "text": "body"}}
	out = prependContext(mustHarnessInput(t, blocks), "CLOCK")
	list, ok := out.Content.([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("列表分支应前置一个文本块: %#v", out.Content)
	}
	first, _ := list[0].(map[string]any)
	if first["text"] != "CLOCK" {
		t.Fatalf("前置块漂移: %#v", first)
	}
}

// mustHarnessInput 构造测试输入。
func mustHarnessInput(t *testing.T, content any) harness_protocol.HarnessInput {
	t.Helper()
	in, err := harness_protocol.NewHarnessInput(content, nil)
	if err != nil {
		t.Fatalf("构造 HarnessInput 失败: %v", err)
	}
	return in
}
