package agent_teams_test

import (
	"context"
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
		TeamTool:   "mock_tool_rail",
		TeamPolicy: "mock_policy_rail",
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
	rails := &agent_teams.MountedRails{TeamTool: "x"}
	h := agent_teams.NewTeamHarness(nil, rails, string(atschema.TeamRoleLeader), "", false)
	assert.Equal(t, rails, h.Rails())
}

// TestTeamHarness_RunAgentCustomizer 测试自定义钩子调用
func TestTeamHarness_RunAgentCustomizer(t *testing.T) {
	var capturedAgent hinterfaces.DeepAgentInterface
	var capturedName string
	var capturedRole string
	called := false

	customizer := func(deepAgent hinterfaces.DeepAgentInterface, memberName string, roleValue string) {
		called = true
		capturedAgent = deepAgent
		capturedName = memberName
		capturedRole = roleValue
	}

	var agent hinterfaces.DeepAgentInterface
	h := agent_teams.NewTeamHarness(agent, nil, string(atschema.TeamRoleLeader), "leader_1", false)
	h.RunAgentCustomizer(customizer)

	assert.True(t, called)
	assert.Nil(t, capturedAgent)
	assert.Equal(t, "leader_1", capturedName)
	assert.Equal(t, "leader", capturedRole)
}

// TestTeamHarness_RunAgentCustomizer_Nil 测试 nil 自定义器不 panic
func TestTeamHarness_RunAgentCustomizer_Nil(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.NotPanics(t, func() {
		h.RunAgentCustomizer(nil)
	})
}

// TestBuildTeamHarness 测试 Build 函数
func TestBuildTeamHarness(t *testing.T) {
	h := agent_teams.BuildTeamHarness(
		nil, // agentSpec
		string(atschema.TeamRoleTeammate),
		"teammate_1",
		"tool_rail",   // teamToolRail
		"policy_rail", // teamPolicyRail
		nil,           // firstIterGate
		nil,           // teamWorkspaceRail
		nil,           // toolApprovalRail
		nil,           // teamPlanModeRail
		true,
	)
	assert.NotNil(t, h)
	assert.Equal(t, string(atschema.TeamRoleTeammate), h.Role())
	assert.Equal(t, "teammate_1", h.MemberName())
	assert.NotNil(t, h.Rails())
	assert.Equal(t, "tool_rail", h.Rails().TeamTool)
	assert.Equal(t, "policy_rail", h.Rails().TeamPolicy)
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
	ch, err := h.RunStreaming(nil, nil, "", nil)
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	// 通道应已关闭
	_, ok := <-ch
	assert.False(t, ok)
}

// TestTeamHarness_RegisterRail 测试 RegisterRail 占位返回 nil
func TestTeamHarness_RegisterRail(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.RegisterRail(nil, nil))
}

// TestTeamHarness_UnregisterRail 测试 UnregisterRail 占位返回 nil
func TestTeamHarness_UnregisterRail(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.UnregisterRail(nil, nil))
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
	assert.NoError(t, h.InjectMemberMemory(nil, nil, ""))
}
