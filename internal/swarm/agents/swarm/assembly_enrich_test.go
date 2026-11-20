// Python 源：assembly.py enrich 段用例（T3）
//
// 场景对应（讨论点 → 用例）：
//   - mcp_configs 注入 + 尾日志真实值（c5 回收）→ TestEnrichMemberLoop
//   - 成员循环折能力 + leader 钉空 subagents → TestEnrichMemberLoop
//   - reviewer_agent 产出 → TestEnrichReviewerAgent
//   - agent_group 装配（预载包；persona 合并/predefined_members/外部成员/
//     metadata 记账/hybrid）→ TestEnrichAgentGroup
//   - scheduled 提前 return（空名册）→ TestEnrichAgentGroup_Scheduled
//   - design 模式显式未实现路径（D-15.16-e1）→ TestEnrichDesignModeGap
//   - group_members 与 agent_group 同现 ValueError（D-15.16-e2）→ TestEnrichGroupMembersGap
//   - 尾日志前缺 leader/teammate 报错 → TestEnrichAgentGroup_MissingBase

package swarm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	experts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/experts"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// enrichFixture 最小团队 spec（leader/teammate 基座 + leader 规格）。
func enrichFixture() *atschema.TeamAgentSpec {
	base := func() hschema.DeepAgentSpec {
		spec := hschema.NewDeepAgentSpec()
		spec.Rails = []hschema.RailSpec{{Type: "user.base"}}
		return spec
	}
	return &atschema.TeamAgentSpec{
		TeamName: "t1",
		Leader:   atschema.LeaderSpec{MemberName: "leader", Persona: "team prompt"},
		Agents:   map[string]hschema.DeepAgentSpec{"leader": base(), "teammate": base()},
	}
}

// enrichRun 跑一次 enrich（隔离 config 读取——legacyConfigMap 走真实文件，
// 测试环境无 config.yaml 得空 map，行为等价 Python get_config() 缺省）。
func enrichRun(t *testing.T, spec *atschema.TeamAgentSpec, mutate func(*EnrichSeedParams)) {
	t.Helper()
	p := EnrichSeedParams{SessionID: "s1", Mode: "team", ChannelID: "web"}
	if mutate != nil {
		mutate(&p)
	}
	require.NoError(t, EnrichTeamSpecForSwarmSeed(spec, p))
}

func TestEnrichMemberLoop(t *testing.T) {
	t.Run("成员折能力与seed挂载", func(t *testing.T) {
		spec := enrichFixture()
		enrichRun(t, spec, nil)
		// leader rails 已折入（含 swarm.runtime_prompt）
		assert.Contains(t, railTypes(spec.Agents["leader"].Rails), RuntimePrompt)
		assert.Contains(t, railTypes(spec.Agents["teammate"].Rails), RuntimePrompt)
		// leader 钉空 subagents
		assert.Empty(t, spec.Agents["leader"].Subagents)
		// 活载体与种子
		assert.NotNil(t, spec.BuildContext)
		assert.NotEmpty(t, spec.BuildContextSeed)
		// 尾日志字段（mcps 计数随 config：测试环境无 MCP → 0）
		assert.NotNil(t, spec.ReviewerAgent)
	})
	t.Run("无mcp配置时mcps为零", func(t *testing.T) {
		spec := enrichFixture()
		enrichRun(t, spec, nil)
		// 空快照下 BuildEnabledMCPServerConfigs 得空
		for _, member := range spec.Agents {
			assert.Empty(t, member.Mcps)
		}
	})
}

func TestEnrichReviewerAgent(t *testing.T) {
	spec := enrichFixture()
	enrichRun(t, spec, nil)
	require.NotNil(t, spec.ReviewerAgent)
	// reviewer 继承 teammate 装配后形态并收窄：1200s + 无 subagents
	assert.Equal(t, 1200.0, spec.ReviewerAgent.CompletionTimeout)
}

// fixtureAgentGroup 构造并预载一个 AgentGroup 包。
func fixtureAgentGroup(t *testing.T) *experts.LoadedAgentGroupPackage {
	t.Helper()
	dir := t.TempDir()
	name := filepath.Base(dir)
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	write("manifest.json", `{"package_type":"agent_group","name":"`+name+`","instruction":"be nice","agents":["leader","writer"]}`)
	write("agents/leader/manifest.json", `{"package_type":"agent_template","name":"Leader Display","description":"leader","persona":{"dir":"."}}`)
	write("agents/leader/AGENT.md", "# rules")
	write("agents/leader/persona.md", "leader persona")
	write("agents/writer/manifest.json", `{"package_type":"agent_template","name":"Writer Display","description":"a writer","persona":{"dir":"."}}`)
	write("agents/writer/persona.md", "writer persona")
	pkg, err := experts.LoadAgentGroupPackageBundle(dir)
	require.NoError(t, err)
	return pkg
}

func TestEnrichAgentGroup(t *testing.T) {
	t.Run("roster装配与记账", func(t *testing.T) {
		spec := enrichFixture()
		pkg := fixtureAgentGroup(t)
		enrichRun(t, spec, func(p *EnrichSeedParams) {
			p.AgentGroupName = "group-x"
			p.AgentGroupPackage = pkg
		})
		assert.Equal(t, "group-x", spec.AgentGroupName)
		assert.Equal(t, "group-x", spec.Metadata["agent_group_name"])
		assert.Equal(t, "hybrid", spec.TeamMode)
		// leader persona 合并不丢既有 team prompt
		assert.Contains(t, spec.Leader.Persona, "team prompt")
		assert.NotEmpty(t, spec.Leader.Persona)
		// 预定义名册：writer 一员（leader 排除）
		require.Len(t, spec.PredefinedMembers, 1)
		assert.Equal(t, "writer", spec.PredefinedMembers[0].MemberName)
		assert.Equal(t, "a writer", spec.PredefinedMembers[0].Desc)
		assert.NotEmpty(t, spec.PredefinedMembers[0].Prompt)
		// 成员 spec 已装配进 agents
		assert.Contains(t, railTypes(spec.Agents["writer"].Rails), RuntimePrompt)
	})
	t.Run("scheduled提前return空名册", func(t *testing.T) {
		spec := enrichFixture()
		spec.DispatchMode = "scheduled"
		pkg := fixtureAgentGroup(t)
		enrichRun(t, spec, func(p *EnrichSeedParams) {
			p.AgentGroupName = "group-x"
			p.AgentGroupPackage = pkg
		})
		assert.Empty(t, spec.PredefinedMembers)
		assert.Equal(t, "group-x", spec.AgentGroupName)
	})
	t.Run("缺leader基座报错", func(t *testing.T) {
		spec := enrichFixture()
		delete(spec.Agents, "teammate")
		pkg := fixtureAgentGroup(t)
		err := EnrichTeamSpecForSwarmSeed(spec, EnrichSeedParams{
			SessionID: "s1", Mode: "team", ChannelID: "web",
			AgentGroupName: "group-x", AgentGroupPackage: pkg,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires both base 'leader' and 'teammate' specs")
	})
}

func TestEnrichDesignModeGap(t *testing.T) {
	t.Run("design模式走显式未实现路径", func(t *testing.T) {
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			spec := enrichFixture()
			_ = EnrichTeamSpecForSwarmSeed(spec, EnrichSeedParams{
				SessionID: "s1", Mode: "team.design", ChannelID: "web",
				AgentGroupName: "group-x",
			})
		}()
		// panic 断言错误信息指向 25.12（D-15.16-e1 不静默）
		require.NotNil(t, recovered, "design branch must not silently pass")
		var msg string
		switch v := recovered.(type) {
		case error:
			msg = strings.TrimSpace(v.Error())
		case string:
			msg = strings.TrimSpace(v)
		default:
			t.Fatalf("unexpected panic value: %v", recovered)
		}
		assert.Contains(t, msg, "25.12")
		assert.Contains(t, msg, "D-15.16-e1")
	})
}

func TestEnrichGroupMembersGap(t *testing.T) {
	t.Run("group_members与agent_group同现报错", func(t *testing.T) {
		spec := enrichFixture()
		pkg := fixtureAgentGroup(t)
		err := EnrichTeamSpecForSwarmSeed(spec, EnrichSeedParams{
			SessionID: "s1", Mode: "team", ChannelID: "web",
			AgentGroupName:    "group-x",
			AgentGroupPackage: pkg,
			RequestMetadata:   map[string]any{"group_members": []any{map[string]any{"member_name": "m1"}}},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot also select a single AgentGroup")
	})
	t.Run("仅group_members走群聊留缝报错", func(t *testing.T) {
		spec := enrichFixture()
		err := EnrichTeamSpecForSwarmSeed(spec, EnrichSeedParams{
			SessionID: "s1", Mode: "team", ChannelID: "web",
			RequestMetadata: map[string]any{"group_members": []any{map[string]any{"member_name": "m1"}}},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "D-15.16-e2")
	})
}

func TestEnrichRule8ShellRemoved(t *testing.T) {
	t.Run("c3拆分壳注释与旧语义清理", func(t *testing.T) {
		data, err := os.ReadFile("assembly.go")
		require.NoError(t, err)
		src := string(data)
		// 旧壳三标记：子集形参注释 / c5 字面值 / 拆分壳偏差登记（c3 全量回收后
		// 新注释只保留"已回收"记述与 15.7/15.8 余量）。
		assert.NotContains(t, src, "⤵️15.16e 不入参")
		assert.NotContains(t, src, "按 c5 记字面值")
		assert.NotContains(t, src, "⤵️15.16e 及成员配置源装配章节")
	})
}
