package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// newTestTeamHarnessWithNative 构造一个带 native 的 TeamHarness（复用无模型 NativeHarness）。
// 25.1a 把 TeamHarness 重构为"转发 native supervisor"，协议面方法（Start/Stop/Send/...）
// 全部转调 native；这些方法在 0 覆盖时被改动行门禁拦下，本文件补齐。
func newTestTeamHarnessWithNative(t *testing.T) *TeamHarness {
	t.Helper()
	native := newTestNativeHarness(t)
	return newTeamHarnessWithNative(
		native,
		native.DeepAgent(),
		string(atschema.TeamRoleLeader),
		"leader-1",
		false,
		nil, // taskEventTracker
	)
}

// TestTeamHarness_State无native返回Idle 验证 State 的 nil native 分支。
func TestTeamHarness_State无native返回Idle(t *testing.T) {
	h := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.Equal(t, HarnessStateIdle, h.State())
}

// TestTeamHarness_State转发native 验证 State 转调 native.State()。
func TestTeamHarness_State转发native(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)
	assert.Equal(t, HarnessStateIdle, h.State())
}

// TestTeamHarness_SessionID无native返回空 验证 SessionID 的 nil native 分支。
func TestTeamHarness_SessionID无native返回空(t *testing.T) {
	h := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.Equal(t, "", h.SessionID())
}

// TestTeamHarness_Native返回底层 验证 Native 返回底层 NativeHarness。
func TestTeamHarness_Native返回底层(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)
	assert.NotNil(t, h.Native())
}

// TestTeamHarness_Outputs无native返回Nil 验证 Outputs 的 nil native 分支。
func TestTeamHarness_Outputs无native返回Nil(t *testing.T) {
	h := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.Nil(t, h.Outputs())
}

// TestTeamHarness_Outputs转发native 验证 Outputs 转调 native.Outputs()。
func TestTeamHarness_Outputs转发native(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)
	assert.NotNil(t, h.Outputs())
}

// TestTeamHarness_Start无native报错 验证 Start 的 nil native 守卫。
func TestTeamHarness_Start无native报错(t *testing.T) {
	h := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.Error(t, h.Start(context.Background(), nil))
}

// TestTeamHarness_StartStop完整周期 验证 Start 绑定子会话并启动 native、Stop 收尾。
// 用 *AgentTeamSession 作为 teamSession：同时覆盖 makeChildSession 的
// 团队会话分支（子会话继承团队会话 ID）与 SessionID 的转发分支（非空）。
func TestTeamHarness_StartStop完整周期(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)
	teamSess := session.NewAgentTeamSession(session.WithAgentTeamSessionID("team-sess"))
	require.NotNil(t, teamSess)

	require.NoError(t, h.Start(context.Background(), teamSess))
	// Start 后 native 相位保持 idle（对齐上游：第一条 send 才开轮）
	assert.Equal(t, HarnessStateIdle, h.State())
	// nativeSessionID 记录团队会话标识
	assert.Equal(t, "team-sess", h.nativeSessionID)
	// SessionID 转发 native.SessionID()（已绑定子会话，继承团队会话 ID）
	assert.Equal(t, "team-sess", h.SessionID())

	require.NoError(t, h.Stop(context.Background()))
	assert.Equal(t, HarnessStateTerminated, h.State())
	// Stop 清空 nativeSessionID
	assert.Equal(t, "", h.nativeSessionID)
}

// TestTeamHarness_Stop无native不报错 验证 Stop 的 nil native 分支。
func TestTeamHarness_Stop无native不报错(t *testing.T) {
	h := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.Stop(context.Background()))
}

// TestTeamHarness_Send无native报错 验证 Send 的 nil native 守卫。
func TestTeamHarness_Send无native报错(t *testing.T) {
	h := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.Error(t, h.Send(context.Background(), "x", false))
}

// TestTeamHarness_Send转发native 验证 Send 转调 native.Send()。
func TestTeamHarness_Send转发native(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)
	teamSess := session.CreateAgentSession("team-sess", h.Native().DeepAgent().Card(), nil)
	require.NoError(t, h.Start(context.Background(), teamSess))
	defer func() { _ = h.Stop(context.Background()) }()

	require.NoError(t, h.Send(context.Background(), "第一条", false))
}

// TestTeamHarness_Abort无活跃会话NoOp 验证 Abort 在无 native/无活跃会话时 no-op。
func TestTeamHarness_Abort无活跃会话NoOp(t *testing.T) {
	h := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.Abort(context.Background(), false))
}

// TestTeamHarness_Abort转发native 验证 Abort 在有活跃会话时转调 native.Abort()。
func TestTeamHarness_Abort转发native(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)
	teamSess := session.CreateAgentSession("team-sess", h.Native().DeepAgent().Card(), nil)
	require.NoError(t, h.Start(context.Background(), teamSess))
	defer func() { _ = h.Stop(context.Background()) }()

	// Start 已 setActiveSession(child)，Abort 走转发分支
	require.NoError(t, h.Abort(context.Background(), false))
}

// TestTeamHarness_PauseResume转发native 验证 Pause/Resume 在活跃会话下转调 native。
func TestTeamHarness_PauseResume转发native(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)
	teamSess := session.CreateAgentSession("team-sess", h.Native().DeepAgent().Card(), nil)
	require.NoError(t, h.Start(context.Background(), teamSess))
	defer func() { _ = h.Stop(context.Background()) }()

	_ = h.Pause(context.Background())
	_ = h.Resume(context.Background(), "继续")
}

// TestTeamHarness_Pause无活跃会话NoOp 验证 Pause 无活跃会话时 no-op。
func TestTeamHarness_Pause无活跃会话NoOp(t *testing.T) {
	h := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.Pause(context.Background()))
	assert.NoError(t, h.Resume(context.Background(), nil))
}

// TestTeamHarness_Subscribe转发native 验证 Subscribe 转调 native.Subscribe()。
func TestTeamHarness_Subscribe转发native(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)
	err := h.Subscribe(context.Background(), func(context.Context, HarnessStateEvent) {}, func(context.Context, HarnessRoundEvent) {})
	assert.NoError(t, err)
}

// TestTeamHarness_Subscribe无nativeNoOp 验证 Subscribe 的 nil native 分支。
func TestTeamHarness_Subscribe无nativeNoOp(t *testing.T) {
	h := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.Subscribe(context.Background(), nil, nil))
}

// TestTeamHarness_RunOnce无native报错 验证 RunOnce 的 nil native 守卫。
func TestTeamHarness_RunOnce无native报错(t *testing.T) {
	h := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	_, err := h.RunOnce(context.Background(), "x", nil)
	assert.Error(t, err)
}

// TestTeamHarness_RunOnce不启动supervisor 验证未 Start 时 RunOnce 走非流式调用。
func TestTeamHarness_RunOnce不启动supervisor(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)
	teamSess := session.CreateAgentSession("team-sess", h.Native().DeepAgent().Card(), nil)
	require.NotNil(t, teamSess)
	// 无模型环境：Invoke 可能返回 error，但不得 panic；结束后 activeSession 已清空
	_, _ = h.RunOnce(context.Background(), "单次任务", teamSess)
	assert.Nil(t, h.getActiveSession())
}

// TestTeamHarness_Dispose无nativeNoOp 验证 Dispose 的 nil native 分支。
func TestTeamHarness_Dispose无nativeNoOp(t *testing.T) {
	h := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.Dispose(context.Background()))
}

// TestTeamHarness_Dispose转发native 验证 Dispose 转调 native.Dispose()。
func TestTeamHarness_Dispose转发native(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)
	teamSess := session.CreateAgentSession("team-sess", h.Native().DeepAgent().Card(), nil)
	require.NoError(t, h.Start(context.Background(), teamSess))
	require.NoError(t, h.Dispose(context.Background()))
	assert.Equal(t, HarnessStateTerminated, h.State())
}

// TestSessionIDOf 验证 sessionIDOf 从团队会话提取标识。
func TestSessionIDOf(t *testing.T) {
	assert.Equal(t, "", sessionIDOf(nil))

	teamSess := session.CreateAgentSession("s-123", nil, nil)
	assert.Equal(t, "s-123", sessionIDOf(teamSess))

	// 非 GetSessionID 接口类型 → 返回空串
	assert.Equal(t, "", sessionIDOf("plain string"))
}

// TestTeamHarness_makeChildSession 验证建子会话（普通会话与团队会话两分支）。
func TestTeamHarness_makeChildSession(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)

	// 普通会话（非 *AgentTeamSession）→ CreateAgentSession("", card, nil)
	child := h.makeChildSession(nil)
	require.NotNil(t, child)

	// nil teamSession 同样走普通分支
	child2 := h.makeChildSession("not-a-team-session")
	require.NotNil(t, child2)

	// *AgentTeamSession 分支 → ts.CreateAgentSession(card, "", false)
	teamSess := session.NewAgentTeamSession()
	require.NotNil(t, teamSess)
	child3 := h.makeChildSession(teamSess)
	require.NotNil(t, child3)
}

// TestTeamHarness_card 验证 card 返回 native 的 card。
func TestTeamHarness_card(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)
	card := h.card()
	require.NotNil(t, card)
	assert.Equal(t, "test-native", card.GetName())

	// 无 native 有 deepAgent 分支
	native := newTestNativeHarness(t)
	h2 := NewTeamHarness(native.DeepAgent(), string(atschema.TeamRoleLeader), "", false)
	assert.NotNil(t, h2.card())

	// 无 native 无 deepAgent 分支
	h3 := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.Nil(t, h3.card())
}

// TestTeamHarness_isInitialTeamPlanLeader 验证 plan 模式判定。
func TestTeamHarness_isInitialTeamPlanLeader(t *testing.T) {
	h := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", true)
	assert.True(t, h.isInitialTeamPlanLeader())

	h2 := NewTeamHarness(nil, string(atschema.TeamRoleTeammate), "", true)
	assert.False(t, h2.isInitialTeamPlanLeader())

	h3 := NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.False(t, h3.isInitialTeamPlanLeader())
}

// TestTeamHarness_ensureInitialPlanMode 验证非 Leader 时不种子化（直接返回）。
func TestTeamHarness_ensureInitialPlanMode(t *testing.T) {
	h := newTestTeamHarnessWithNative(t)
	// initialPlanMode=false → isInitialTeamPlanLeader=false → 直接返回不 panic
	child := session.CreateAgentSession("", h.card(), nil)
	require.NotNil(t, child)
	assert.NotPanics(t, func() {
		h.ensureInitialPlanMode(child)
	})
	assert.False(t, h.initialPlanModeSeeded)
}

// TestTeamHarness_ensureInitialPlanMode_Leader种子化 验证 Leader+plan 时切 plan 模式并置位。
func TestTeamHarness_ensureInitialPlanMode_Leader种子化(t *testing.T) {
	native := newTestNativeHarness(t)
	h := newTeamHarnessWithNative(
		native,
		native.DeepAgent(),
		string(atschema.TeamRoleLeader),
		"leader-1",
		true, // initialPlanMode=true
		nil,  // taskEventTracker
	)
	child := session.CreateAgentSession("", h.card(), nil)
	require.NotNil(t, child)
	assert.NotPanics(t, func() {
		h.ensureInitialPlanMode(child)
	})
	// 种子化已置位：第二次调用应幂等跳过
	assert.True(t, h.initialPlanModeSeeded)
	assert.NotPanics(t, func() {
		h.ensureInitialPlanMode(child)
	})
}
