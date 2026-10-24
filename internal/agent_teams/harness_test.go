package agent_teams_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
)

// mockDeepAgent 实现 DeepAgentInterface 的测试桩。
type mockDeepAgent struct {
	hinterfaces.DeepAgentInterface // 嵌入接口以部分满足，未调用的方法会 panic
}

// TestNewTeamHarness 测试 TeamHarness 构造函数
func TestNewTeamHarness(t *testing.T) {
	rails := &agent_teams.MountedRails{
		TeamTool:   nil, // ⤴️ 9.68 回填后 TeamTool 类型为 *rails.TeamToolRail
		TeamPolicy: nil, // ⤴️ 9.68 回填后 TeamPolicy 类型为 *rails.TeamPolicyRail
	}
	var agent hinterfaces.DeepAgentInterface // nil 接口
	h := agent_teams.NewTeamHarness(
		agent,
		rails,
		string(atschema.TeamRoleLeader),
		"leader_1",
		false,
	)
	assert.NotNil(t, h)
	assert.Nil(t, h.InnerAgent())
	assert.Equal(t, rails, h.Rails())
	assert.Equal(t, string(atschema.TeamRoleLeader), h.Role())
	assert.Equal(t, "leader_1", h.MemberName())
}

// TestNewTeamHarness_NilRails 测试 Rails 为 nil 时不 panic
func TestNewTeamHarness_NilRails(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleTeammate), "t1", true)
	assert.NotNil(t, h)
	assert.Nil(t, h.Rails())
}

// TestTeamHarness_Rails 返回 Rails 句柄
func TestTeamHarness_Rails(t *testing.T) {
	rails := &agent_teams.MountedRails{TeamTool: nil}
	h := agent_teams.NewTeamHarness(nil, rails, string(atschema.TeamRoleLeader), "", false)
	assert.Equal(t, rails, h.Rails())
}

// TestTeamHarness_RunAgentCustomizer 测试自定义钩子调用
// S-01: 统一签名 ctx + error
func TestTeamHarness_RunAgentCustomizer(t *testing.T) {
	var capturedAgent hinterfaces.DeepAgentInterface
	var capturedName string
	var capturedRole string
	called := false

	customizer := func(ctx context.Context, deepAgent hinterfaces.DeepAgentInterface, memberName string, roleValue string) error {
		called = true
		capturedAgent = deepAgent
		capturedName = memberName
		capturedRole = roleValue
		return nil
	}

	agent := &mockDeepAgent{}
	h := agent_teams.NewTeamHarness(agent, nil, string(atschema.TeamRoleLeader), "leader_1", false)
	h.RunAgentCustomizer(context.Background(), customizer)

	assert.True(t, called)
	assert.NotNil(t, capturedAgent)
	assert.Equal(t, "leader_1", capturedName)
	assert.Equal(t, "leader", capturedRole)
}

// TestTeamHarness_RunAgentCustomizer_NilAgent 测试 deepAgent 为 nil 时跳过钩子（S-06 守卫）
func TestTeamHarness_RunAgentCustomizer_NilAgent(t *testing.T) {
	called := false
	customizer := func(ctx context.Context, deepAgent hinterfaces.DeepAgentInterface, memberName string, roleValue string) error {
		called = true
		return nil
	}
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "leader_1", false)
	h.RunAgentCustomizer(context.Background(), customizer)
	assert.False(t, called) // nil agent → 跳过
}

// TestTeamHarness_RunAgentCustomizer_Error吞掉 测试 customizer 返回 error 时只记日志不 panic
func TestTeamHarness_RunAgentCustomizer_Error吞掉(t *testing.T) {
	customizer := func(ctx context.Context, deepAgent hinterfaces.DeepAgentInterface, memberName string, roleValue string) error {
		return errors.New("customizer failed")
	}
	agent := &mockDeepAgent{}
	h := agent_teams.NewTeamHarness(agent, nil, string(atschema.TeamRoleLeader), "leader_1", false)
	assert.NotPanics(t, func() {
		h.RunAgentCustomizer(context.Background(), customizer)
	})
}

// TestTeamHarness_RunAgentCustomizer_Nil 测试 nil 自定义器不 panic
func TestTeamHarness_RunAgentCustomizer_Nil(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.NotPanics(t, func() {
		h.RunAgentCustomizer(context.Background(), nil)
	})
}

// TestBuildTeamHarness 测试 Build 函数
func TestBuildTeamHarness(t *testing.T) {
	h := agent_teams.BuildTeamHarness(
		atschema.DeepAgentSpec{}, // agentSpec
		string(atschema.TeamRoleTeammate),
		"teammate_1",
		nil, // teamToolRail — ⤴️ 9.68 回填后类型为 *rails.TeamToolRail
		nil, // teamPolicyRail — ⤴️ 9.68 回填后类型为 *rails.TeamPolicyRail
		nil, // firstIterGate
		nil, // teamWorkspaceRail
		nil, // toolApprovalRail
		nil, // teamPlanModeRail
		true,
	)
	assert.NotNil(t, h)
	assert.Equal(t, string(atschema.TeamRoleTeammate), h.Role())
	assert.Equal(t, "teammate_1", h.MemberName())
	assert.NotNil(t, h.Rails())
	assert.Nil(t, h.Rails().TeamTool)
	assert.Nil(t, h.Rails().TeamPolicy)
	assert.Nil(t, h.Rails().FirstIterGate)
}

// TestTeamHarness_StubMethods 测试 TODO 占位方法返回零值
func TestTeamHarness_StubMethods(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.Nil(t, h.DeepConfig())
	assert.Nil(t, h.Workspace())
	assert.Nil(t, h.SysOperation())
	assert.Nil(t, h.Model())
	assert.False(t, h.HasPendingInterrupt())
	assert.Nil(t, h.FindRails(nil))
}

// TestTeamHarness_IsPendingInterruptResumeValid 测试中断恢复验证占位返回 false
func TestTeamHarness_IsPendingInterruptResumeValid(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.False(t, h.IsPendingInterruptResumeValid(nil))
	assert.False(t, h.IsPendingInterruptResumeValid("some input"))
}

// TestTeamHarness_PersistDBState 测试持久化占位方法不 panic
func TestTeamHarness_PersistDBState(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.NotPanics(t, func() {
		h.PersistTeamDBState()
		h.MarkTeamCleaned()
		h.MarkTeamBuilt()
		h.RequestCompletionPoll()
		h.WakeMailboxIfInterruptCleared()
		h.InitCwdForRound()
	})
}

// TestTeamHarness_Steer 测试 Steer 占位返回 nil
func TestTeamHarness_Steer(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.Steer(context.TODO(), "转向"))
}

// TestTeamHarness_FollowUp 测试 FollowUp 占位返回 nil
func TestTeamHarness_FollowUp(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.FollowUp(context.TODO(), "追加"))
}

// TestTeamHarness_Abort 测试 Abort 占位返回 nil
func TestTeamHarness_Abort(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.Abort(context.TODO()))
}

// TestTeamHarness_RunStreaming 测试 RunStreaming 占位返回关闭的通道
func TestTeamHarness_RunStreaming(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	ch, err := h.RunStreaming(context.TODO(), nil, "", nil)
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	// 通道应已关闭
	_, ok := <-ch
	assert.False(t, ok)
}

// TestTeamHarness_RegisterRail 测试 RegisterRail 占位返回 nil
func TestTeamHarness_RegisterRail(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.RegisterRail(context.TODO(), nil))
}

// TestTeamHarness_UnregisterRail 测试 UnregisterRail 占位返回 nil
func TestTeamHarness_UnregisterRail(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.UnregisterRail(context.TODO(), nil))
}

// TestTeamHarness_RegisterMemberTools_Nil 测试 nil memoryManager 不 panic
func TestTeamHarness_RegisterMemberTools_Nil(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.NotPanics(t, func() {
		h.RegisterMemberTools(nil)
	})
}

// TestTeamHarness_InjectMemberMemory_Nil 测试 nil memoryManager 返回 nil
func TestTeamHarness_InjectMemberMemory_Nil(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.InjectMemberMemory(context.TODO(), nil, ""))
}

// TestTeamHarness_RunStreaming_非BaseAgent降级空流 M-17 回归测试。
// deepAgent 非 nil 但未实现 BaseAgent 时应降级为关闭的空流（Warn 日志路径），
// 而非 panic 或阻塞。
func TestTeamHarness_RunStreaming_非BaseAgent降级空流(t *testing.T) {
	// mockDeepAgent 只嵌入接口，不实现 BaseAgent 的完整方法集 → 断言失败路径
	h := agent_teams.NewTeamHarness(&mockDeepAgent{}, nil, string(atschema.TeamRoleLeader), "m1", false)
	ch, err := h.RunStreaming(context.TODO(), map[string]any{"q": "hi"}, "sess-1", nil)
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	// 通道应立即关闭（降级空流）
	_, ok := <-ch
	assert.False(t, ok)
}

// TestTeamHarness_RunStreaming_teamSession占位分支 M-17 回归测试。
// teamSession 非 nil 走分支 2 占位（⤵️ plan-mode 种子化回填前返回关闭空流）。
func TestTeamHarness_RunStreaming_teamSession占位分支(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "m1", false)
	ch, err := h.RunStreaming(context.TODO(), nil, "sess-1", map[string]any{"team": "state"})
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	_, ok := <-ch
	assert.False(t, ok)
}
