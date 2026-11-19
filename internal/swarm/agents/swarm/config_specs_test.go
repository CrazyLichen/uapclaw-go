// Python 源：config_specs.py 用例 1:1 转 Go + 本批场景化补充（T2b）
//
// 场景对应（讨论点 → 用例）：
//   - team profile rails/tools → TestBuildTeamCapabilitySpecs
//   - code profile（code.team / team.plan.code）→ TestBuildCodeCapabilitySpecs
//   - design profile 声明层 → TestBuildDesignCapabilitySpecs
//   - permissions 开关传递 → 同上三组内的 teammate/leader 分支
//   - mcp_configs 注入 + 去重 → TestBuildMemberDeepAgentSpec_MCP
//   - _with_project_cwd → TestWithProjectCwd
//   - reviewer 薄壳收窄 → TestBuildReviewerDeepAgentSpec
//   - Skill rail 收敛（_collapse_skill_use_rails）→ TestCollapseSkillUseRails

package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/manifest"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// railTypes 提取 rail 类型名列表。
func railTypes(specs []hschema.RailSpec) []string {
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.Type)
	}
	return out
}

// toolTypes 提取工具类型名列表。
func toolTypes(specs []hschema.BuiltinToolSpec) []string {
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.Type)
	}
	return out
}

// baseSpecFixture 最小基础 spec。
func baseSpecFixture() *hschema.DeepAgentSpec {
	return &hschema.DeepAgentSpec{
		MaxIterations:       15,
		AutoCreateWorkspace: true,
		Rails:               []hschema.RailSpec{{Type: "user.base"}},
	}
}

// ──────────────────────────── 测试 ────────────────────────────

func TestBuildTeamCapabilitySpecs(t *testing.T) {
	cfg := map[string]any{}
	rails_, tools_ := BuildMemberCapabilitySpecs(cfg, "team", "teammate", false)

	t.Run("teammate含common rails+skill rail+toolkit", func(t *testing.T) {
		types := railTypes(rails_)
		assert.Contains(t, types, RuntimePrompt)
		assert.Contains(t, types, manifest.SysOperation)
		assert.Contains(t, types, manifest.TaskPlanning) // teammate 保留 todo planning
		assert.Contains(t, types, rails.TeamSkillUse)
		assert.Contains(t, types, MemberSkillToolkit)
		// leader 专属 rails 不出现
		assert.NotContains(t, types, StructuredAskUser)
		assert.NotContains(t, types, SessionMessagingRoute)
	})
	t.Run("teammate不含演进rail_开关关", func(t *testing.T) {
		assert.NotContains(t, railTypes(rails_), TeamSkillEvolution)
	})
	t.Run("tools含web族+send_file", func(t *testing.T) {
		tt := toolTypes(tools_)
		assert.Contains(t, tt, manifest.WebSearch)
		assert.Contains(t, tt, manifest.Vision)
		assert.Contains(t, tt, SendFileTool)
		assert.NotContains(t, tt, SwarmA4PIntentAuthorization) // leader 专属
	})
	t.Run("leader去掉todo_planning带结构化ask与消息路由", func(t *testing.T) {
		lr, lt := BuildMemberCapabilitySpecs(cfg, "team", "leader", false)
		assert.NotContains(t, railTypes(lr), manifest.TaskPlanning)
		assert.Contains(t, railTypes(lr), StructuredAskUser)
		assert.Contains(t, railTypes(lr), SessionMessagingRoute)
		assert.Contains(t, toolTypes(lt), SwarmA4PIntentAuthorization)
		assert.Contains(t, toolTypes(lt), SessionMessagingTools)
	})
	t.Run("permissions开启时teammate带TeamPermission", func(t *testing.T) {
		pr, _ := BuildMemberCapabilitySpecs(cfg, "team", "teammate", true)
		assert.Contains(t, railTypes(pr), TeamPermission)
	})
	t.Run("permissions开启时leader带TeamPermissionPolicy", func(t *testing.T) {
		pr, _ := BuildMemberCapabilitySpecs(cfg, "team", "leader", true)
		assert.Contains(t, railTypes(pr), TeamPermissionPolicy)
	})
}

func TestBuildCodeCapabilitySpecs(t *testing.T) {
	t.Run("code.team teammate含code rails族", func(t *testing.T) {
		r, _ := BuildMemberCapabilitySpecs(map[string]any{}, "code.team", "teammate", false)
		types := railTypes(r)
		assert.Contains(t, types, CodeRuntimePrompt)
		assert.Contains(t, types, CodeProjectMemory)
		assert.Contains(t, types, CodeAgentMD)
		assert.Contains(t, types, CodeAgentMode)
		// PERMISSION_INTERRUPT 对全部团队成员排除
		assert.NotContains(t, types, PermissionInterrupt)
		// 非 plan leader 无审批闸
		assert.NotContains(t, types, TeamPlanApproval)
		// code confirm interrupt（core.confirm_interrupt）带 tool_names 参数
		found := false
		for _, spec := range r {
			if spec.Type == manifest.ConfirmInterrupt {
				found = true
				assert.Equal(t, []string{"switch_mode", "exit_plan_mode"}, spec.Params["tool_names"])
			}
		}
		assert.True(t, found)
	})
	t.Run("plan_code leader带审批闸且无confirm_interrupt", func(t *testing.T) {
		r, _ := BuildMemberCapabilitySpecs(map[string]any{}, "team.plan.code", "leader", false)
		types := railTypes(r)
		assert.Contains(t, types, TeamPlanApproval)
		assert.NotContains(t, types, manifest.ConfirmInterrupt)
		assert.NotContains(t, types, CodeTaskPlanning) // leader 去 code todo planning
	})
	t.Run("code模式tools含code_extra_tools", func(t *testing.T) {
		_, tt := BuildMemberCapabilitySpecs(map[string]any{}, "code.team", "teammate", false)
		assert.Contains(t, toolTypes(tt), CodeExtraTools)
	})
}

func TestBuildDesignCapabilitySpecs(t *testing.T) {
	t.Run("leader含designer三rail声明", func(t *testing.T) {
		r, tt := BuildMemberCapabilitySpecs(map[string]any{}, "team.design", "leader", false)
		types := railTypes(r)
		assert.Contains(t, types, DesignerAuthoring)
		assert.Contains(t, types, DesignerExecution)
		assert.Contains(t, types, DesignerSurfaceSanitizer)
		assert.Contains(t, types, StructuredAskUser)
		// design 工具面：vision/audio + leader web 三件 + graph tools
		ttypes := toolTypes(tt)
		assert.Contains(t, ttypes, DesignerGraphTools)
		assert.Contains(t, ttypes, manifest.WebSearch)
		// profile 无 skill/symphony/task-planning rails
		assert.NotContains(t, types, rails.TeamSkillUse)
		assert.NotContains(t, types, MemberSkillToolkit)
	})
	t.Run("teammate含sys_operation无designer rail", func(t *testing.T) {
		r, tt := BuildMemberCapabilitySpecs(map[string]any{}, "team.design", "teammate", false)
		assert.Contains(t, railTypes(r), manifest.SysOperation)
		assert.NotContains(t, railTypes(r), DesignerAuthoring)
		assert.NotContains(t, toolTypes(tt), DesignerGraphTools)
	})
}

func TestBuildMemberSubagentSpecs(t *testing.T) {
	t.Run("非code模式只有statusline_setup缺省开", func(t *testing.T) {
		specs := BuildMemberSubagentSpecs(map[string]any{}, "team", "teammate")
		require.Len(t, specs, 1)
		assert.Equal(t, StatuslineSetupAgent, specs[0].FactoryName)
	})
	t.Run("code模式含explore与plan缺省开", func(t *testing.T) {
		specs := BuildMemberSubagentSpecs(map[string]any{}, "code.team", "teammate")
		names := []string{}
		for _, spec := range specs {
			names = append(names, spec.AgentCard.BaseCard.Name)
		}
		assert.Contains(t, names, "explore_agent")
		assert.Contains(t, names, "plan_agent")
		assert.Contains(t, names, "statusline-setup")
	})
	t.Run("显式false剔除缺省开子代理", func(t *testing.T) {
		cfg := map[string]any{"react": map[string]any{
			"subagents": map[string]any{"explore_agent": map[string]any{"enabled": false}},
		}}
		specs := BuildMemberSubagentSpecs(cfg, "code.team", "teammate")
		for _, spec := range specs {
			assert.NotEqual(t, rails.ExploreAgent, spec.FactoryName)
		}
	})
	t.Run("code与cua需显式true", func(t *testing.T) {
		cfg := map[string]any{"react": map[string]any{
			"subagents": map[string]any{
				"code_agent":    map[string]any{"enabled": true},
				"cua_agent":     map[string]any{"enabled": true, "snapshot_keep_last_k": "2", "pause_on_user_input": true},
				"browser_agent": map[string]any{"enabled": true},
			},
		}}
		specs := BuildMemberSubagentSpecs(cfg, "code.team", "teammate")
		factories := []string{}
		for _, spec := range specs {
			factories = append(factories, spec.FactoryName)
		}
		assert.Contains(t, factories, SwarmCodeAgent)
		assert.Contains(t, factories, SwarmCuaAgent)
		assert.Contains(t, factories, SwarmBrowserAgent)
		// cua factory_kwargs 去前缀
		for _, spec := range specs {
			if spec.FactoryName == SwarmCuaAgent {
				assert.Equal(t, 2, spec.FactoryKwargs["snapshot_keep_last_k"])
				assert.Equal(t, true, spec.FactoryKwargs["pause_on_user_input"])
			}
		}
	})
}

func TestBuildMemberDeepAgentSpec(t *testing.T) {
	t.Run("rails合并且基础spec不被修改", func(t *testing.T) {
		base := baseSpecFixture()
		merged := BuildMemberDeepAgentSpec(map[string]any{}, "team", "teammate", base, false, nil)
		assert.Equal(t, []hschema.RailSpec{{Type: "user.base"}}, base.Rails)
		assert.Equal(t, "user.base", merged.Rails[0].Type)
		assert.Greater(t, len(merged.Rails), 1)
	})
	t.Run("leader钉空subagents且task_planning关", func(t *testing.T) {
		base := baseSpecFixture()
		base.Subagents = []hschema.SubAgentSpec{{SystemPrompt: "x"}}
		merged := BuildMemberDeepAgentSpec(map[string]any{}, "team", "leader", base, false, nil)
		assert.Empty(t, merged.Subagents)
		assert.False(t, merged.EnableTaskPlanning)
	})
	t.Run("mcp合并按id与name去重", func(t *testing.T) {
		base := baseSpecFixture()
		base.Mcps = []*types.McpServerConfig{{ServerID: "id1", ServerName: "shared"}}
		configMcps := []*types.McpServerConfig{
			{ServerID: "id1", ServerName: "dup-id"},
			{ServerID: "id2", ServerName: "shared"},
			{ServerID: "id3", ServerName: "new"},
		}
		merged := BuildMemberDeepAgentSpec(map[string]any{}, "team", "teammate", base, false, configMcps)
		require.Len(t, merged.Mcps, 2)
		assert.Equal(t, "id1", merged.Mcps[0].ServerID)
		assert.Equal(t, "id3", merged.Mcps[1].ServerID)
	})
	t.Run("非code模式skill_discovery随检索翻转", func(t *testing.T) {
		merged := BuildMemberDeepAgentSpec(map[string]any{}, "team", "teammate", baseSpecFixture(), false, nil)
		assert.True(t, merged.EnableSkillDiscovery) // 检索未开
	})
	t.Run("code模式system_prompt为code提示词", func(t *testing.T) {
		merged := BuildMemberDeepAgentSpec(map[string]any{}, "code.team", "teammate", baseSpecFixture(), false, nil)
		assert.NotEmpty(t, merged.SystemPrompt)
	})
	t.Run("teammate子代理合并剔除基础explore", func(t *testing.T) {
		base := baseSpecFixture()
		base.Subagents = []hschema.SubAgentSpec{
			{FactoryName: rails.ExploreAgent},
			{SystemPrompt: "other"},
		}
		merged := BuildMemberDeepAgentSpec(map[string]any{}, "code.team", "teammate", base, false, nil)
		count := 0
		for _, spec := range merged.Subagents {
			if spec.FactoryName == rails.ExploreAgent {
				count++
			}
		}
		assert.Equal(t, 1, count)                                  // 基础的被剔除，只剩声明式那个
		assert.Equal(t, "other", merged.Subagents[0].SystemPrompt) // 保留在首位
	})
}

func TestWithProjectCwd(t *testing.T) {
	base := baseSpecFixture()
	t.Run("空目录原样返回", func(t *testing.T) {
		assert.Same(t, base, WithProjectCwd(base, ""))
		assert.Same(t, base, WithProjectCwd(base, "   "))
	})
	t.Run("非空覆写cwd与project_root", func(t *testing.T) {
		merged := WithProjectCwd(base, "/repo")
		assert.Equal(t, "/repo", merged.Cwd)
		assert.Equal(t, "/repo", merged.ProjectRoot)
		assert.Empty(t, base.Cwd) // base 未被修改
	})
}

func TestBuildReviewerDeepAgentSpec(t *testing.T) {
	t.Run("team模式reviewer白名单", func(t *testing.T) {
		base := baseSpecFixture()
		base.Rails = append(base.Rails,
			hschema.RailSpec{Type: RuntimePrompt},
			hschema.RailSpec{Type: rails.TeamSkillUse},
			hschema.RailSpec{Type: MemberSkillToolkit},
			hschema.RailSpec{Type: TeamPermission},
		)
		base.BuiltinTools = []hschema.BuiltinToolSpec{
			{Type: manifest.WebSearch},
			{Type: SendFileTool},
		}
		reviewer := BuildReviewerDeepAgentSpec("team", base)
		types := railTypes(reviewer.Rails)
		assert.Contains(t, types, RuntimePrompt)
		assert.Contains(t, types, rails.TeamSkillUse) // team skill rail 保留
		assert.NotContains(t, types, MemberSkillToolkit)
		assert.NotContains(t, types, TeamPermission)
		tt := toolTypes(reviewer.BuiltinTools)
		assert.Contains(t, tt, manifest.WebSearch)
		assert.NotContains(t, tt, SendFileTool)
		assert.Equal(t, 1200.0, reviewer.CompletionTimeout)
	})
	t.Run("code模式reviewer保留LSP与AGENT_MD", func(t *testing.T) {
		base := baseSpecFixture()
		base.Rails = append(base.Rails, hschema.RailSpec{Type: CodeRuntimePrompt}, hschema.RailSpec{Type: CodeAgentMD})
		reviewer := BuildReviewerDeepAgentSpec("code.team", base)
		types := railTypes(reviewer.Rails)
		assert.Contains(t, types, CodeRuntimePrompt)
		assert.Contains(t, types, CodeAgentMD)
	})
}

func TestCollapseSkillUseRails(t *testing.T) {
	t.Run("多条Skill rail只留首条", func(t *testing.T) {
		rails_ := []hschema.RailSpec{
			{Type: "user.base"},
			{Type: rails.TeamSkillUse},
			{Type: manifest.SkillUse},
			{Type: "core.security"},
		}
		out := collapseSkillUseRails(rails_, false)
		require.Len(t, out, 3)
		assert.Equal(t, rails.TeamSkillUse, out[1].Type)
	})
	t.Run("检索开启时钉auto_list并关include_tools", func(t *testing.T) {
		rails_ := []hschema.RailSpec{
			{Type: rails.TeamSkillUse, Params: map[string]any{"skill_mode": "all"}},
		}
		out := collapseSkillUseRails(rails_, true)
		require.Len(t, out, 1)
		assert.Equal(t, "auto_list", out[0].Params["skill_mode"])
		assert.Equal(t, false, out[0].Params["include_tools"])
	})
}

func TestResolveMemberSkills(t *testing.T) {
	cfg := map[string]any{"agents": map[string]any{
		"leader": map[string]any{"skills": []any{"  a ", "", "b"}},
	}}
	assert.Equal(t, []string{"a", "b"}, resolveMemberSkills(cfg, "leader"))
	assert.Empty(t, resolveMemberSkills(map[string]any{}, "leader"))
}

func TestSkillMode(t *testing.T) {
	assert.Equal(t, "all", skillMode(map[string]any{}))
	assert.Equal(t, "auto_list", skillMode(map[string]any{
		"symphony": map[string]any{"skill_retrieval": map[string]any{"enabled": true}},
	}))
	assert.Equal(t, "auto_list", skillMode(map[string]any{
		"react": map[string]any{"skill_mode": "auto_list"},
	}))
	assert.Equal(t, "all", skillMode(map[string]any{
		"react": map[string]any{"skill_mode": "bogus"},
	}))
}

func TestKvCacheAffinityConfigBake(t *testing.T) {
	t.Run("默认关闭", func(t *testing.T) {
		assert.False(t, kvCacheAffinityConfig(map[string]any{}).EnableKVCacheAffinity)
	})
	t.Run("显式开启但provider无能力时fail-closed", func(t *testing.T) {
		cfg := map[string]any{"kv_cache_affinity_config": map[string]any{"enable_kv_cache_affinity": true}}
		assert.False(t, kvCacheAffinityConfig(cfg).EnableKVCacheAffinity)
	})
}
