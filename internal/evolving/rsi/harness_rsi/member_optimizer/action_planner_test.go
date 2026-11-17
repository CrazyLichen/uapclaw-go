package memberoptimizer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/action_groups"
)

// mkPlan 构造合法最小方案。
func mkPlan(mut func(map[string]any)) map[string]any {
	plan := map[string]any{
		"actions": []any{},
		"action_waves": []any{},
	}
	mut(plan)
	return plan
}

func mkAction(mut func(map[string]any)) map[string]any {
	action := map[string]any{
		"action_id": "a1", "role": "solver", "action_group": "config",
		"operation": "modify", "target_path": "runtime.yaml",
		"declared_write_paths": []any{"runtime.yaml"},
		"attributed_issue_ids": []any{"i1"},
		"constraints":          map[string]any{},
	}
	mut(action)
	return action
}

func plannerTarget() []MemberOptimizationTarget {
	return []MemberOptimizationTarget{{
		Role: "solver", HarnessRefPath: "/nonexistent-harness-for-tests",
		AttributedIssueIDs: []string{"i1"},
		OptimizationSurfaces: []string{"config"},
	}}
}

func TestValidatePlanStructure_容器校验(t *testing.T) {
	// actions 非列表
	errs := ValidatePlanStructure(map[string]any{"actions": "bad"})
	require.Contains(t, errs, "actions must be a list (use [] for no actions)")
	// 元素非对象
	errs = ValidatePlanStructure(map[string]any{"actions": []any{"x"}, "action_waves": []any{}})
	require.Contains(t, errs, "actions[0] must be an object")
	// 字段类型
	errs = ValidatePlanStructure(map[string]any{"actions": []any{
		map[string]any{"action_id": 1},
	}, "action_waves": []any{}})
	require.Contains(t, errs, "actions[0].action_id must be a string")
	// waves 引用非字符串
	errs = ValidatePlanStructure(map[string]any{"actions": []any{}, "action_waves": []any{[]any{1}}})
	require.Contains(t, errs, "action_waves must be a list of lists of action IDs")
	// 合法空方案
	require.Empty(t, ValidatePlanStructure(mkPlan(func(p map[string]any) {})))
}

func TestValidatePlan_全链校验(t *testing.T) {
	targets := plannerTarget()
	roles := map[string]bool{"solver": true}
	// 合法单动作
	plan := mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {})}
		p["action_waves"] = []any{[]any{"a1"}}
	})
	require.Empty(t, ValidatePlan(plan, roles, targets, nil))
	// 重复 action_id
	plan = mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {}), mkAction(func(m map[string]any) {})}
	})
	errs := ValidatePlan(plan, roles, targets, nil)
	require.Contains(t, errs[0], "duplicate action_id: a1")
	// 波次引用未知动作
	plan = mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {})}
		p["action_waves"] = []any{[]any{"ghost"}}
	})
	errs = ValidatePlan(plan, roles, targets, nil)
	require.Contains(t, errs[0], "wave[0] references unknown action_id: ghost")
	// 波次缺动作
	plan = mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {})}
		p["action_waves"] = []any{}
	})
	errs = ValidatePlan(plan, roles, targets, nil)
	require.Contains(t, errs[0], "actions missing from waves: [a1]")
	// 束上限
	plan = mkPlan(func(p map[string]any) {
		acts := []any{}
		for i := 0; i < 4; i++ {
			id := "a" + string(rune('0'+i))
			acts = append(acts, mkAction(func(m map[string]any) { m["action_id"] = id }))
		}
		p["actions"] = acts
	})
	errs = ValidatePlan(plan, roles, targets, nil)
	require.Contains(t, errs[0], "issue i1 has 4 actions; at most 3 associated actions are allowed")
	// 依赖跨束（依赖未知）
	plan = mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) { m["depends_on"] = []any{"ghost"} })}
	})
	errs = ValidatePlan(plan, roles, targets, nil)
	require.Contains(t, errs[0], "depends on unknown action_id: ghost")
	// 优化面对齐失败（surface prompt vs config）
	plan = mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {
			m["action_group"] = "prompt"
			m["operation"] = "modify"
			m["target_path"] = "prompt_sections/files/x.md"
			m["declared_write_paths"] = []any{"prompt_sections/files/x.md", "prompt_sections/sections.yaml"}
			m["constraints"] = map[string]any{"section_name": "x"}
		})}
	})
	errs = ValidatePlan(plan, roles, targets, nil)
	require.Contains(t, errs[0], "action surface does not match diagnosed optimization_surface")
	// 目标覆盖缺失：surface config 有 target 但无动作（需非空定义集才有 supported surfaces）
	defs := []harnessrsi.ActionDefinition{{Name: "config_modify", Group: "config", Operation: "modify"}}
	emptyPlan := mkPlan(func(p map[string]any) {})
	errs = ValidatePlan(emptyPlan, roles, targets, defs)
	require.Contains(t, errs[0], "has actionable optimization_surfaces [config] but no executable action")
}

func TestValidatePlan_Issue归因(t *testing.T) {
	targets := []MemberOptimizationTarget{{
		Role: "solver", HarnessRefPath: "/nonexistent-harness-for-tests",
		AttributedIssueIDs: []string{"i1", "i2"},
		OptimizationSurfaces: []string{"config"},
	}}
	roles := map[string]bool{"solver": true}
	// 多 issue 拒绝（两 issue 均已诊断；Python unknown 检查先于多 issue 检查）
	plan := mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {
			m["attributed_issue_ids"] = []any{"i1", "i2"}
		})}
	})
	errs := ValidatePlan(plan, roles, targets, nil)
	require.Contains(t, strings.Join(errs, "; "), "each optimization action must attribute exactly one diagnosed issue")
	// 未诊断 issue 拒绝
	plan = mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {
			m["attributed_issue_ids"] = []any{"i-unknown"}
		})}
	})
	errs = ValidatePlan(plan, roles, targets, nil)
	require.Contains(t, strings.Join(errs, "; "), "attributed_issue_ids contains issues not assigned to the action role: [i-unknown]")
}

func TestValidatePlan_SkillSearch回退(t *testing.T) {
	targets := plannerTarget()
	roles := map[string]bool{"solver": true}
	plan := mkPlan(func(p map[string]any) {
		search := mkAction(func(m map[string]any) {
			m["action_id"] = "s1"
			m["action_group"] = "skill"
			m["operation"] = "search"
			m["target_path"] = "skills/"
			m["declared_write_paths"] = []any{"skills/", "skills/skills.yaml"}
			m["candidate_query"] = "find skill"
		})
		add := mkAction(func(m map[string]any) {
			m["action_id"] = "a1"
			m["action_group"] = "skill"
			m["operation"] = "add"
			m["target_path"] = "skills/new_skill/SKILL.md"
			m["declared_write_paths"] = []any{"skills/new_skill/SKILL.md", "skills/skills.yaml"}
			m["depends_on"] = []any{"s1"}
			m["run_if"] = "dependency_succeeded"
		})
		p["actions"] = []any{search, add}
	})
	errs := ValidatePlan(plan, roles, targets, nil)
	require.Contains(t, strings.Join(errs, "; "), "skill/add fallback after skill/search must set run_if=dependency_failed")
}

func TestValidatePlan_Prompt面契约(t *testing.T) {
	targets := []MemberOptimizationTarget{{
		Role: "solver", HarnessRefPath: "/nonexistent-harness-for-tests",
		AttributedIssueIDs: []string{"i1"},
		MechanismTypes:     []string{"workflow"},
		OptimizationSurfaces: []string{"identity"},
	}}
	roles := map[string]bool{"solver": true}
	// identity.md 缺 surface_scope
	plan := mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {
			m["action_group"] = "prompt"
			m["operation"] = "modify"
			m["target_path"] = "identity.md"
			m["declared_write_paths"] = []any{"identity.md"}
		})}
	})
	errs := ValidatePlan(plan, roles, targets, nil)
	require.Contains(t, errs[0], "identity.md can only be used for role identity or")
	// soul.md 程序化术语拒绝
	plan = mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {
			m["action_group"] = "prompt"
			m["operation"] = "modify"
			m["target_path"] = "soul.md"
			m["declared_write_paths"] = []any{"soul.md"}
			m["constraints"] = map[string]any{"surface_scope": "durable_operating_principle"}
			m["description"] = "update the verification procedure"
		})}
	})
	errs = ValidatePlan(plan, roles, targets, nil)
	require.Contains(t, errs[0], "specific workflows, checklists, verification procedures,")
}

func TestValidateAndRepairPlanData_波次重算(t *testing.T) {
	targets := plannerTarget()
	roles := map[string]bool{"solver": true}
	// 错误波次 → 机械修复重算后通过
	plan := mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {})}
		p["action_waves"] = []any{}
	})
	errs := ValidateAndRepairPlanData(plan, roles, targets, nil)
	require.Empty(t, errs)
	// 波次已重算
	require.NotNil(t, plan["action_waves"])
}

func TestNormalizeRequiredDeclaredPaths_机械补全(t *testing.T) {
	plan := mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {
			m["action_group"] = "prompt"
			m["operation"] = "add"
			m["target_path"] = "prompt_sections/files/new.md"
			m["declared_write_paths"] = []any{"prompt_sections/files/new.md"}
		})}
	})
	NormalizeRequiredDeclaredPaths(plan)
	actions := planActionsList(plan)
	require.Contains(t, actions[0]["declared_write_paths"], "prompt_sections/sections.yaml")
}

func TestActionOptimizationSurface(t *testing.T) {
	require.Equal(t, "config", actionOptimizationSurface(mkAction(func(m map[string]any) {})))
	require.Equal(t, "identity", actionOptimizationSurface(mkAction(func(m map[string]any) {
		m["action_group"] = "prompt"
		m["target_path"] = "identity.md"
	})))
	require.Equal(t, "soul", actionOptimizationSurface(mkAction(func(m map[string]any) {
		m["action_group"] = "prompt"
		m["target_path"] = "soul.md"
	})))
	require.Equal(t, "prompt_section", actionOptimizationSurface(mkAction(func(m map[string]any) {
		m["action_group"] = "prompt"
		m["target_path"] = "prompt_sections/files/x.md"
	})))
	require.Equal(t, "", actionOptimizationSurface(mkAction(func(m map[string]any) {
		m["action_group"] = "model"
	})))
}

func TestNormalizeOptimizationSurface(t *testing.T) {
	require.Equal(t, "prompt_section", normalizeOptimizationSurface("prompt_file"))
	require.Equal(t, "prompt_section", normalizeOptimizationSurface("prompt_section_file"))
	require.Equal(t, "config", normalizeOptimizationSurface("config"))
}

func TestValidatePlanStructure经BundleCohesion连通性(t *testing.T) {
	// 同束两动作无依赖关系 → 不连通拒绝
	plan := mkPlan(func(p map[string]any) {
		acts := []any{
			mkAction(func(m map[string]any) { m["action_id"] = "a" }),
			mkAction(func(m map[string]any) { m["action_id"] = "b" }),
		}
		p["actions"] = acts
	})
	errs := ValidateActionBundleCohesion(plan)
	require.Contains(t, errs[0], "actions are not one connected")
	// 有依赖 → 连通
	plan = mkPlan(func(p map[string]any) {
		acts := []any{
			mkAction(func(m map[string]any) { m["action_id"] = "a" }),
			mkAction(func(m map[string]any) { m["action_id"] = "b"; m["depends_on"] = []any{"a"} }),
		}
		p["actions"] = acts
	})
	require.Empty(t, ValidateActionBundleCohesion(plan))
}

func TestFilterActionablePlanningInputs(t *testing.T) {
	targets := plannerTarget()
	roleReport := &RoleAttributionReport{
		AssignedRoleIssues: []RoleIssueAttribution{{IssueID: "i1", Role: "solver"}},
	}
	mechReport := &MechanismAttributionReport{
		RoleMechanisms: map[string][]RoleMechanismAttribution{
			"solver": {
				{IssueID: "i1", MechanismType: "insufficient_role_evidence", FailureSignature: "insufficient_role_evidence"},
			},
		},
	}
	at, ar, mr, filtered := filterActionablePlanningInputs(targets, roleReport, mechReport)
	require.Len(t, filtered, 1)
	require.Equal(t, "i1", filtered[0])
	require.Empty(t, at) // 目标问题全被剔除 → 空目标
	require.Empty(t, ar.AssignedRoleIssues)
	require.Empty(t, mr.RoleMechanisms)
}

func TestPlannerSessionID(t *testing.T) {
	require.Equal(t, "member_action_planner", plannerSessionID(nil))
	got := plannerSessionID(map[string]any{"sibling_generation": map[string]any{"candidate_id": "cand 1"}})
	require.Contains(t, got, "member_action_planner_cand_1_")
	require.Len(t, strings.Split(got, "_"), 6)
}

func TestValidatePlanStructure_BundleCohesion导出一致性(t *testing.T) {
	// 导出 ValidateActionBundleCohesion 与内部实现同源
	plan := mkPlan(func(p map[string]any) {})
	require.Empty(t, ValidateActionBundleCohesion(plan))
	_ = actiongroups.SchedAction{}
}
