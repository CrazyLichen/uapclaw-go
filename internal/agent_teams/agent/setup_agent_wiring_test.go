package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	teamrails "github.com/uapclaw/uapclaw-go/internal/agent_teams/rails"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestSetupAgent_注册团队railProvider 对齐上游 agent_configurator.py:711。
//
// SetupAgent 必须调 EnsureHarnessElementsRegistered，否则 8 个 team rail
// provider 全是死代码，spec 里声明的 RailSpec.Build 会走"未知 type"分支
// 被静默跳过——团队 rail 全部丢失且不报错。
func TestSetupAgent_注册团队railProvider(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	teamrails.ResetRegistrationForTest()
	t.Cleanup(func() {
		hschema.ResetRailRegistryForTest()
		teamrails.ResetRegistrationForTest()
	})

	_, before := hschema.LookupRailProvider(teamrails.TeamPolicy)
	assert.False(t, before, "前置：注册表应为空")

	c := NewAgentConfigurator(agentschema.NewAgentCard())
	_, err := c.SetupAgent(atschema.TeamAgentSpec{}, defaultRuntimeContextForWiring())

	require.NoError(t, err)
	_, after := hschema.LookupRailProvider(teamrails.TeamPolicy)
	assert.True(t, after, "SetupAgent 后 team.policy provider 必须已注册")
}

// TestSetupAgent_团队rail折进specRails 对齐上游 :930-940。
//
// 上游注释：「Fold the team rails into the spec rails (after the user rails,
// to keep the init order consistent with the legacy mount order)」。
// 用 provider 调用计数验证：team.policy 的 provider 至少被调用一次，
// 说明声明真的进了 spec.rails 且经 RailSpec.Build 解析。
func TestSetupAgent_团队rail折进specRails(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	teamrails.ResetRegistrationForTest()
	teamrails.EnsureHarnessElementsRegistered()
	t.Cleanup(func() {
		hschema.ResetRailRegistryForTest()
		teamrails.ResetRegistrationForTest()
	})

	called := 0
	hschema.RegisterRailProvider(teamrails.TeamPolicy, func(
		map[string]any, hschema.BuildContextInterface,
	) (any, error) {
		called++
		return nil, nil
	})

	c := NewAgentConfigurator(agentschema.NewAgentCard())
	_, err := c.SetupAgent(atschema.TeamAgentSpec{}, defaultRuntimeContextForWiring())

	require.NoError(t, err)
	assert.GreaterOrEqual(t, called, 1,
		"team.policy 的 RailSpec 必须被折进 spec.rails 并经 provider 解析")
}

// defaultRuntimeContextForWiring 构造最小 TeamRuntimeContext。
func defaultRuntimeContextForWiring() atschema.TeamRuntimeContext {
	return atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleLeader,
		MemberName: "l1",
	}
}
