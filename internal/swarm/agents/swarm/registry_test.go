package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
)

// TestSwarmContextFactoryCoversDefault 覆盖注册后 MaterializeBuildContext
// 走新工厂（对齐 registry.py:198 注册语义 + blueprint.go:363 消费链）。
func TestSwarmContextFactoryCoversDefault(t *testing.T) {
	RegisterSwarmProviders()
	// 幂等：二次调用安全
	RegisterSwarmProviders()

	spec := &atschema.TeamAgentSpec{}
	spec.BuildContextSeed = map[string]any{
		"session_id": "s1", "team_id": "t1", "project_dir": "/proj", "mode": "team",
		"team_outputs_dir": "/artifacts/outputs",
	}
	if err := spec.MaterializeBuildContext(); err != nil {
		t.Fatal(err)
	}
	bc, ok := spec.BuildContext.(*buildctx.SwarmBuildContext)
	if !ok {
		t.Fatalf("应重建为平台子类，got %T", spec.BuildContext)
	}
	if bc.TeamID != "t1" || bc.ProjectDir != "/proj" || bc.Mode != "team" {
		t.Fatalf("字段丢失: %+v", bc)
	}
	if bc.TeamOutputsDir != "/artifacts/outputs" {
		t.Fatalf("team_outputs_dir 丢失: %q", bc.TeamOutputsDir)
	}
}

// TestRegisterSwarmProviders_幂等且全集注册 重复调用幂等；常量层全集对拍
// provider 注册表（防显式清单遗漏——F6 偏差的兜底测试）。
func TestRegisterSwarmProviders_幂等且全集注册(t *testing.T) {
	RegisterSwarmProviders()
	RegisterSwarmProviders() // 幂等

	// rail 常量层全集对拍：registry.go 常量块声明的每个 swarm.* rail 名
	// 必须可查（允许 15.20/15.21 缺口批未覆盖的名字在此显式豁免并注明）。
	exemptRails := map[string]bool{
		// 15.21：evolution 段（swarm.team_skill_evolution/team_skill_create/
		// member_skill_evolution/evolution_interrupt/symphony_graph_evolution）
		"swarm.team_skill_evolution":      true,
		"swarm.team_skill_create":         true,
		"swarm.member_skill_evolution":    true,
		"swarm.evolution_interrupt":       true,
		"swarm.symphony_graph_evolution":  true,
	}
	for name := range SwarmRailNamesForTest() {
		if exemptRails[name] {
			continue
		}
		_, ok := hschema.LookupRailProvider(name)
		require.True(t, ok, "rail provider 未注册: %s", name)
	}
	// tool 抽样
	for _, name := range []string{SwarmSkillToolkit, SwarmUserTodos, SessionMessagingTools} {
		_, ok := hschema.LookupToolProvider(name)
		require.True(t, ok, "tool provider 未注册: %s", name)
	}
	// subagent 抽样
	_, ok := hschema.LookupSubagentProvider(SwarmBrowserAgent)
	require.True(t, ok, "subagent provider 未注册: swarm.browser_agent")
	// 群聊三件必须不在（F2）
	_, ok = hschema.LookupRailProvider("swarm.group_member_template")
	assert.False(t, ok, "group_chat 三件 ⤴️25.6，不得在本批注册")
}
