package agent_teams_test

import (
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/stretchr/testify/assert"
)

// TestNewTeamHarness 测试 TeamHarness 构造函数
func TestNewTeamHarness(t *testing.T) {
	rails := &agent_teams.MountedRails{
		TeamTool:   "mock_tool_rail",
		TeamPolicy: "mock_policy_rail",
	}
	h := agent_teams.NewTeamHarness(
		"mock_deep_agent",
		rails,
		string(atschema.TeamRoleLeader),
		"leader_1",
		false,
	)
	assert.NotNil(t, h)
	assert.Equal(t, "mock_deep_agent", h.InnerAgent())
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
	var capturedAgent any
	var capturedName string
	var capturedRole string
	called := false

	customizer := func(deepAgent any, memberName string, roleValue string) {
		called = true
		capturedAgent = deepAgent
		capturedName = memberName
		capturedRole = roleValue
	}

	h := agent_teams.NewTeamHarness("my_agent", nil, string(atschema.TeamRoleLeader), "leader_1", false)
	h.RunAgentCustomizer(agent_teams.AgentCustomizer(customizer))

	assert.True(t, called)
	assert.Equal(t, "my_agent", capturedAgent)
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
