package memberoptimizer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestActionGroupLever_查表 对照 Python lever.py:62-64 _ACTION_GROUP_LEVERS。
func TestActionGroupLever_查表(t *testing.T) {
	require.Equal(t, LeverInstruction, ActionGroupLever("prompt"))
	require.Equal(t, LeverInstruction, ActionGroupLever("skill"))
	require.Equal(t, LeverAction, ActionGroupLever("tool"))
	require.Equal(t, LeverAction, ActionGroupLever("subagent"))
	require.Equal(t, LeverControl, ActionGroupLever("rail"))
	require.Equal(t, LeverControl, ActionGroupLever("processor"))
	require.Equal(t, LeverConfiguration, ActionGroupLever("config"))
	require.Equal(t, LeverConfiguration, ActionGroupLever("configuration"))
	// Python 回落 unresolved；大小写与空白归一化
	require.Equal(t, LeverUnresolved, ActionGroupLever("unknown_group"))
	require.Equal(t, LeverInstruction, ActionGroupLever("  PROMPT "))
}

// TestTargetRefLever_变量映射 对照 Python lever.py:66-69 _TARGET_VARIABLE_LEVERS。
func TestTargetRefLever_变量映射(t *testing.T) {
	require.Equal(t, LeverInstruction, TargetRefLever("identity"))
	require.Equal(t, LeverInstruction, TargetRefLever("Agent.prompt_section"))
	require.Equal(t, LeverAction, TargetRefLever("tool"))
	require.Equal(t, LeverControl, TargetRefLever("workflow"))
	require.Equal(t, LeverConfiguration, TargetRefLever("execution-budget")) // 连字符归一化
	require.Equal(t, LeverUnresolved, TargetRefLever("unknown"))
}

// TestAvailableSurfacesForLever 对照 Python lever.py:71-89。
func TestAvailableSurfacesForLever(t *testing.T) {
	// configuration 杠杆只报 config/configuration 组
	require.Equal(t, []string{"config", "configuration"},
		AvailableSurfacesForLever(LeverConfiguration, []string{"config", "configuration", "prompt"}))
	// unresolved 返回空
	require.Empty(t, AvailableSurfacesForLever(LeverUnresolved, []string{"prompt", "skill"}))
	// 其余杠杆按固定顺序输出，prompt → prompt_section
	require.Equal(t, []string{"prompt_section", "skill", "tool", "rail", "subagent", "processor"},
		AvailableSurfacesForLever(LeverInstruction,
			[]string{"processor", "subagent", "rail", "tool", "skill", "prompt"}))
}

// TestBuildHypothesisLeverPolicy 对照 Python lever.py:91-111。
func TestBuildHypothesisLeverPolicy(t *testing.T) {
	policy := BuildHypothesisLeverPolicy("Agent.prompt", []string{"c2", "c1", "c1"},
		map[string]any{"probe": "p"})
	require.Equal(t, LeverInstruction, policy["recommended_lever"])
	require.Equal(t, "Agent.prompt", policy["target_ref"])
	require.Equal(t, []string{"c1", "c2"}, policy["predicted_affected_case_ids"])
	retro := policy["retroactive_check"].(map[string]any)
	require.Equal(t, map[string]any{"probe": "p"}, retro["decisive_probe"])
	require.NotEmpty(t, retro["falsification_rule"])
	require.NotEmpty(t, policy["why_this_lever"])
}

// TestBuildActionLeverDecision 对照 Python lever.py:113-141。
func TestBuildActionLeverDecision(t *testing.T) {
	policies := []map[string]any{
		{"recommended_lever": "instruction", "why_this_lever": "w1",
			"why_not_other_levers":        map[string]any{"a": 1},
			"predicted_affected_case_ids": []any{"c1"},
			"retroactive_check":           map[string]any{"x": "y"}},
		{"recommended_lever": "instruction", "why_this_lever": "w2",
			"why_not_other_levers": "not-a-dict", // 非 dict 被过滤
			"retroactive_check":    "not-a-dict"},
	}
	decision := BuildActionLeverDecision("prompt", "prompt_section", policies)
	require.Equal(t, LeverInstruction, decision["selected_lever"])
	require.Equal(t, "prompt_section", decision["selected_surface"])
	require.Equal(t, []string{"instruction"}, decision["recommended_levers"])
	require.Equal(t, true, decision["lever_matches_diagnosis"])
	require.Equal(t, []string{"w1", "w2"}, decision["why_this_lever"])
	require.Len(t, decision["why_not_other_levers"], 1)
	require.Equal(t, []string{"c1"}, decision["predicted_affected_case_ids"])
	require.Len(t, decision["retroactive_checks"], 1)

	// 杠杆不一致 → lever_matches_diagnosis=false
	policies2 := []map[string]any{{"recommended_lever": "action"}}
	decision2 := BuildActionLeverDecision("prompt", "prompt_section", policies2)
	require.Equal(t, false, decision2["lever_matches_diagnosis"])
}

// TestLeverValuesSet LEVER_VALUES 集合完整。
func TestLeverValuesSet(t *testing.T) {
	require.Len(t, LeverValues, 5)
	for _, v := range []string{LeverConfiguration, LeverControl, LeverAction, LeverInstruction, LeverUnresolved} {
		require.Contains(t, LeverValues, v)
	}
}
