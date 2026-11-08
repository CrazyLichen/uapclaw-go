package rails

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// TestNewTeamPolicyRail_默认值 测试默认构造
func TestNewTeamPolicyRail_默认值(t *testing.T) {
	r := mustNewPolicyRail(t)
	if r == nil {
		t.Fatal("NewTeamPolicyRail returned nil")
	}
}

// TestNewTeamPolicyRail_WithOptions 测试选项构造
func TestNewTeamPolicyRail_WithOptions(t *testing.T) {
	r := mustNewPolicyRail(t,
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyMemberName("alice"),
		WithPolicyLifecycle("temporary"),
		WithPolicyTeammateMode("build_mode"),
		WithPolicyLanguage("cn"),
		WithPolicyTeamMode("default"),
	)
	if r.role != atschema.TeamRoleLeader {
		t.Fatalf("expected role=leader, got %s", r.role)
	}
}

// TestTeamPolicyRail_静态Sections_Leader 测试 Leader 静态 Section 数量
func TestTeamPolicyRail_静态Sections_Leader(t *testing.T) {
	r := mustNewPolicyRail(t,
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyMemberName("alice"),
		WithPolicyTeammateMode("build_mode"),
		WithPolicyLifecycle("temporary"),
		WithPolicyLanguage("cn"),
		WithPolicyTeamMode("default"),
	)
	// Leader 应有：Role(P:11) + Workflow(P:13) + Lifecycle(P:14) + Persona(P:15) = 4 个
	if len(r.staticSections) < 3 {
		t.Fatalf("expected at least 3 static sections for leader, got %d", len(r.staticSections))
	}
	// 第一个 section 应为 team_role
	if r.staticSections[0].Name != string(prompts.SectionRole) {
		t.Fatalf("expected first section name=%s, got %s", prompts.SectionRole, r.staticSections[0].Name)
	}
}

// TestTeamPolicyRail_静态Sections_Teammate 测试 Teammate 静态 Section
func TestTeamPolicyRail_静态Sections_Teammate(t *testing.T) {
	r := mustNewPolicyRail(t,
		WithPolicyRole(atschema.TeamRoleTeammate),
		WithPolicyMemberName("bob"),
		WithPolicyTeammateMode("build_mode"),
		WithPolicyLifecycle("temporary"),
		WithPolicyLanguage("cn"),
		WithPolicyTeamMode("default"),
	)
	// Teammate 应有：Role(P:11) = 至少 1 个（无 workflow/lifecycle）
	if len(r.staticSections) < 1 {
		t.Fatalf("expected at least 1 static section for teammate, got %d", len(r.staticSections))
	}
}

// TestTeamPolicyRail_无TeamBackend 测试无 TeamBackend 时 tracker 仍构造且名册通道关闭
func TestTeamPolicyRail_无TeamBackend(t *testing.T) {
	r := mustNewPolicyRail(t,
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
	)
	if r.tracker == nil {
		t.Fatal("tracker 应始终构造（无 backend 时退化为只有身份通道）")
	}
	if text, ok := r.tracker.SnapshotText(); ok && text != "" {
		t.Fatalf("无 backend 时不应有名册/团队信息内容, got %q", text)
	}
}

// TestTeamPolicyRail_Init 测试 Init 缓存 prompt builder
func TestTeamPolicyRail_Init(t *testing.T) {
	r := mustNewPolicyRail(t,
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
	)
	agent := newFakeBaseAgentTeam()
	err := r.Init(context.Background(), agent)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if r.systemPromptBuilder == nil {
		t.Fatal("expected non-nil systemPromptBuilder after Init")
	}
}

// TestTeamPolicyRail_Uninit 测试 Uninit 清理
func TestTeamPolicyRail_Uninit(t *testing.T) {
	r := mustNewPolicyRail(t,
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
	)
	agent := newFakeBaseAgentTeam()
	_ = r.Init(context.Background(), agent)
	_ = r.Uninit(agent)
	if r.systemPromptBuilder != nil {
		t.Fatal("expected nil systemPromptBuilder after Uninit")
	}
}

// TestTeamPolicyRail_StaticSections 测试 StaticSections 访问器
func TestTeamPolicyRail_StaticSections(t *testing.T) {
	r := mustNewPolicyRail(t,
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
	)
	sections := r.StaticSections()
	if sections == nil {
		t.Fatal("expected non-nil static sections")
	}
}

// TestTeamPolicyRail_ExtraSection 测试 basePrompt 非空时包含 Extra Section
func TestTeamPolicyRail_ExtraSection(t *testing.T) {
	r := mustNewPolicyRail(t,
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
		WithPolicyBasePrompt("custom instructions"),
	)
	found := false
	for _, s := range r.staticSections {
		if s.Name == string(prompts.SectionExtra) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected team_extra section when basePrompt is set")
	}
}

// TestTeamPolicyRail_BeforeModelCall 测试 BeforeModelCall 注入 section
func TestTeamPolicyRail_BeforeModelCall(t *testing.T) {
	r := mustNewPolicyRail(t,
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyMemberName("alice"),
		WithPolicyLanguage("cn"),
	)
	agent := newFakeBaseAgentTeam()
	_ = r.Init(context.Background(), agent)
	err := r.BeforeModelCall(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeforeModelCall returned error: %v", err)
	}
}

// TestTeamPolicyRail_BeforeModelCall_无Builder 测试无 builder 时不 panic
func TestTeamPolicyRail_BeforeModelCall_无Builder(t *testing.T) {
	r := mustNewPolicyRail(t,
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
	)
	// 不调用 Init，systemPromptBuilder 为 nil
	err := r.BeforeModelCall(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeforeModelCall should not fail with nil builder: %v", err)
	}
}

// TestWithPolicyTeamWorkspaceMount 验证 WithPolicyTeamWorkspaceMount 选项。
func TestWithPolicyTeamWorkspaceMount(t *testing.T) {
	r := &TeamPolicyRail{}
	WithPolicyTeamWorkspaceMount("/mnt/ws")(r)
	assert.Equal(t, "/mnt/ws", r.teamWorkspaceMount)
}

// TestWithPolicyTeamWorkspacePath 验证 WithPolicyTeamWorkspacePath 选项。
func TestWithPolicyTeamWorkspacePath(t *testing.T) {
	r := &TeamPolicyRail{}
	WithPolicyTeamWorkspacePath("/tmp/team-ws")(r)
	assert.Equal(t, "/tmp/team-ws", r.teamWorkspacePath)
}

// TestWithPolicyTeamBackend 验证 WithPolicyTeamBackend 选项。
func TestWithPolicyTeamBackend(t *testing.T) {
	tb := newRailsTestTeamBackend()
	r := &TeamPolicyRail{}
	WithPolicyTeamBackend(tb)(r)
	assert.Equal(t, tb, r.teamBackend)
}

// TestWithPolicyExposeHumanAgents 验证 WithPolicyExposeHumanAgents 选项。
func TestWithPolicyExposeHumanAgents(t *testing.T) {
	r := &TeamPolicyRail{}
	WithPolicyExposeHumanAgents(true)(r)
	assert.True(t, r.exposeHumanAgentsToTeammates)
}

// mustNewPolicyRail 断言 NewTeamPolicyRail 无错误。
func mustNewPolicyRail(t *testing.T, opts ...TeamPolicyRailOption) *TeamPolicyRail {
	t.Helper()
	rail, err := NewTeamPolicyRail(opts...)
	if err != nil {
		t.Fatalf("NewTeamPolicyRail 返回错误: %v", err)
	}
	return rail
}

// TestNewTeamPolicyRail_模板缺失返回错误 构造期模板缺失必须报错（对齐 Python __init__ 抛 FileNotFoundError）
func TestNewTeamPolicyRail_模板缺失返回错误(t *testing.T) {
	_, err := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("zz"),
	)
	if err == nil {
		t.Fatal("模板缺失时 NewTeamPolicyRail 应返回错误")
	}
}

// ──────────────────────────── 25.0c 新增参数 ────────────────────────────

// TestNewTeamPolicyRail_新参数就位 测试 25.0c 新增构造参数被正确保存
func TestNewTeamPolicyRail_新参数就位(t *testing.T) {
	rail, err := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleTeammate),
		WithPolicyDisplayName("Alice"),
		WithPolicyMemberWorkspacePath("/ws/alice"),
		WithPolicyMemberPrompt("私有约定"),
		WithPolicyTeamOutputsDir("/team/outputs"),
		WithPolicyForkSource("bob"),
		WithPolicySteerBatchSize(3),
		WithPolicyDispatchMode("scheduled"),
	)
	if err != nil {
		t.Fatalf("NewTeamPolicyRail: %v", err)
	}
	if rail.displayName != "Alice" {
		t.Fatalf("displayName = %q", rail.displayName)
	}
	if rail.memberWorkspacePath != "/ws/alice" {
		t.Fatalf("memberWorkspacePath = %q", rail.memberWorkspacePath)
	}
	if rail.memberPrompt != "私有约定" {
		t.Fatalf("memberPrompt = %q", rail.memberPrompt)
	}
	if rail.teamOutputsDir != "/team/outputs" {
		t.Fatalf("teamOutputsDir = %q", rail.teamOutputsDir)
	}
	if rail.forkSource != "bob" {
		t.Fatalf("forkSource = %q", rail.forkSource)
	}
	if rail.steerBatchSize != 3 {
		t.Fatalf("steerBatchSize = %d", rail.steerBatchSize)
	}
	if rail.dispatchMode != "scheduled" {
		t.Fatalf("dispatchMode = %q", rail.dispatchMode)
	}
	if rail.tracker == nil {
		t.Fatal("tracker 未构造")
	}
}

// TestNewTeamPolicyRail_默认steerBatchSize 测试缺省批大小为 2
//
// Python: steer_batch_size: int = 2 (team_policy_rail.py L147)
func TestNewTeamPolicyRail_默认steerBatchSize(t *testing.T) {
	rail, err := NewTeamPolicyRail(WithPolicyRole(atschema.TeamRoleTeammate))
	if err != nil {
		t.Fatalf("NewTeamPolicyRail: %v", err)
	}
	if rail.steerBatchSize != defaultSteerBatchSize {
		t.Fatalf("steerBatchSize = %d, want %d", rail.steerBatchSize, defaultSteerBatchSize)
	}
	if defaultSteerBatchSize != 2 {
		t.Fatalf("defaultSteerBatchSize = %d, want 2（对齐上游）", defaultSteerBatchSize)
	}
}

// TestNewTeamPolicyRail_leader清空任务通知器 测试 leader 不持有任务通知
//
// Python: task_event_tracker if role != LEADER else None (team_policy_rail.py L243-245)
func TestNewTeamPolicyRail_leader清空任务通知器(t *testing.T) {
	fake := &fakeTaskTracker{}
	rail, err := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyTaskEventTracker(fake),
	)
	if err != nil {
		t.Fatalf("NewTeamPolicyRail: %v", err)
	}
	if rail.taskEventTracker != nil {
		t.Fatal("leader 不应持有任务通知器")
	}
}

// TestNewTeamPolicyRail_teammate保留任务通知器 测试非 leader 保留注入的通知器
func TestNewTeamPolicyRail_teammate保留任务通知器(t *testing.T) {
	fake := &fakeTaskTracker{}
	rail, err := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleTeammate),
		WithPolicyTaskEventTracker(fake),
	)
	if err != nil {
		t.Fatalf("NewTeamPolicyRail: %v", err)
	}
	if rail.taskEventTracker == nil {
		t.Fatal("teammate 应保留任务通知器")
	}
}

// ──────────────────────────── 25.0c 钩子 ────────────────────────────

// TestGetCallbacks_声明三条钩子 测试 OnUserMessage / BeforeSteeringDrain / BeforeModelCall 都注册
func TestGetCallbacks_声明三条钩子(t *testing.T) {
	r := mustNewPolicyRail(t, WithPolicyRole(atschema.TeamRoleTeammate))
	cbs := r.GetCallbacks()
	for _, event := range []agentinterfaces.AgentCallbackEvent{
		agentinterfaces.CallbackOnUserMessage,
		agentinterfaces.CallbackBeforeSteeringDrain,
		agentinterfaces.CallbackBeforeModelCall,
	} {
		if _, ok := cbs[event]; !ok {
			t.Fatalf("缺少回调声明: %s", event)
		}
	}
}

// TestOnUserMessage_非leader剪枝被取代的任务板 测试只留最新一份快照 + 时间锚点
//
// Python: on_user_message (team_policy_rail.py L268-319)
func TestOnUserMessage_非leader剪枝被取代的任务板(t *testing.T) {
	r := mustNewPolicyRail(t, WithPolicyRole(atschema.TeamRoleTeammate))
	// 任务板快照是 <team-event kind="task-board">（snapshotEventKinds 唯一成员）
	parts := []string{
		`<team-event kind="task-board">旧板</team-event>`,
		`<team-event kind="task-board">新板</team-event>`,
	}
	cbc := newTestCallbackContext(&agentinterfaces.UserMessageInputs{Parts: parts})
	if err := r.OnUserMessage(context.Background(), cbc); err != nil {
		t.Fatalf("OnUserMessage: %v", err)
	}
	got := cbc.Inputs().(*agentinterfaces.UserMessageInputs).Parts
	// 1 份存活快照 + 1 个 clock；无 session 故不再折入状态
	if len(got) != 2 {
		t.Fatalf("parts = %d, want 2: %v", len(got), got)
	}
	if !strings.Contains(got[0], "<team-clock ") {
		t.Fatalf("parts[0] 应是 clock 锚点: %q", got[0])
	}
	if !strings.Contains(got[1], "新板") {
		t.Fatalf("应保留最新的一份板: %q", got[1])
	}
}

// TestOnUserMessage_leader不剪枝 测试 leader 的板序列被完整保留
//
// Python: _drop_superseded 的 leader 豁免（team_policy_rail.py L368-369）
func TestOnUserMessage_leader不剪枝(t *testing.T) {
	r := mustNewPolicyRail(t, WithPolicyRole(atschema.TeamRoleLeader))
	parts := []string{
		`<team-event kind="task-board">旧板</team-event>`,
		`<team-event kind="task-board">新板</team-event>`,
	}
	cbc := newTestCallbackContext(&agentinterfaces.UserMessageInputs{Parts: parts})
	if err := r.OnUserMessage(context.Background(), cbc); err != nil {
		t.Fatalf("OnUserMessage: %v", err)
	}
	got := cbc.Inputs().(*agentinterfaces.UserMessageInputs).Parts
	// 2 份板 + 1 个 clock
	if len(got) != 3 {
		t.Fatalf("parts = %d, want 3: %v", len(got), got)
	}
}

// TestOnUserMessage_任务通知前插 测试 tracker 的 notice 排在 clock 之前
//
// Python: _fold_task_outcomes (team_policy_rail.py L321-328)
func TestOnUserMessage_任务通知前插(t *testing.T) {
	fake := &fakeTaskTracker{notice: &tracker.ClaimedNotice{
		Text:    `<team-event kind="task-revised">x</team-event>`,
		Entries: []string{"t-1@2"},
	}}
	r := mustNewPolicyRail(t,
		WithPolicyRole(atschema.TeamRoleTeammate),
		WithPolicyTaskEventTracker(fake),
	)
	cbc := newTestCallbackContextWithSession(
		&agentinterfaces.UserMessageInputs{Parts: []string{"用户输入"}},
		newFakeSessionFacade("s1"),
	)
	if err := r.OnUserMessage(context.Background(), cbc); err != nil {
		t.Fatalf("OnUserMessage: %v", err)
	}
	got := cbc.Inputs().(*agentinterfaces.UserMessageInputs).Parts
	if !strings.Contains(got[0], "task-revised") {
		t.Fatalf("parts[0] 应是任务通知: %q", got[0])
	}
	if fake.persistCount != 1 {
		t.Fatalf("persist 次数 = %d, want 1", fake.persistCount)
	}
}

// TestOnUserMessage_非UserMessageInputs不动 测试输入类型不符时安全返回
func TestOnUserMessage_非UserMessageInputs不动(t *testing.T) {
	r := mustNewPolicyRail(t, WithPolicyRole(atschema.TeamRoleTeammate))
	cbc := newTestCallbackContext(&agentinterfaces.InvokeInputs{})
	if err := r.OnUserMessage(context.Background(), cbc); err != nil {
		t.Fatalf("OnUserMessage 不应报错: %v", err)
	}
}

// TestBeforeSteeringDrain_非leader设上限 测试批上限来自构造参数
//
// Python: before_steering_drain (team_policy_rail.py L330-350)
func TestBeforeSteeringDrain_非leader设上限(t *testing.T) {
	r := mustNewPolicyRail(t,
		WithPolicyRole(atschema.TeamRoleTeammate),
		WithPolicySteerBatchSize(3),
	)
	in := &agentinterfaces.SteeringDrainInputs{}
	cbc := newTestCallbackContext(in)
	if err := r.BeforeSteeringDrain(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeSteeringDrain: %v", err)
	}
	if in.Limit == nil || *in.Limit != 3 {
		t.Fatalf("Limit = %v, want 3", in.Limit)
	}
}

// TestBeforeSteeringDrain_leader不设上限 测试 leader 豁免（批不设限）
func TestBeforeSteeringDrain_leader不设上限(t *testing.T) {
	r := mustNewPolicyRail(t, WithPolicyRole(atschema.TeamRoleLeader))
	in := &agentinterfaces.SteeringDrainInputs{}
	cbc := newTestCallbackContext(in)
	if err := r.BeforeSteeringDrain(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeSteeringDrain: %v", err)
	}
	if in.Limit != nil {
		t.Fatalf("Limit = %v, want nil（leader 豁免）", in.Limit)
	}
}

// TestQueueTeamContextAttachment_无附件管理器走兜底 测试无 manager 时返回 false
//
// Python: _queue_team_context_attachment 的 manager is None → False（L413-416）
func TestQueueTeamContextAttachment_无附件管理器走兜底(t *testing.T) {
	r := mustNewPolicyRail(t, WithPolicyRole(atschema.TeamRoleTeammate))
	sess := newFakeSessionFacade("s1")
	cbc := newTestCallbackContextWithSession(&agentinterfaces.InvokeInputs{}, sess)
	if got := r.queueTeamContextAttachment(context.Background(), cbc, sess); got {
		t.Fatal("无附件管理器时应返回 false（交回兜底车道）")
	}
}
