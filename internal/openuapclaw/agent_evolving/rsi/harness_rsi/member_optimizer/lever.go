// lever.go 提供规划与实验跟踪共享的优化杠杆策略。
// Python: openjiuwen/rsi/harness_rsi/member_optimizer/lever.py（全量对齐，164 行）。

package memberoptimizer

import (
	"sort"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// LeverConfiguration 杠杆：配置
const LeverConfiguration = "configuration"

// LeverControl 杠杆：控制
const LeverControl = "control"

// LeverAction 杠杆：动作
const LeverAction = "action"

// LeverInstruction 杠杆：叮嘱
const LeverInstruction = "instruction"

// LeverUnresolved 杠杆：未决
const LeverUnresolved = "unresolved"

// LeverValues 全部合法杠杆值（Python LEVER_VALUES frozenset）。
var LeverValues = []string{
	LeverConfiguration,
	LeverControl,
	LeverAction,
	LeverInstruction,
	LeverUnresolved,
}

// actionGroupLevers 手术分组 → 杠杆（Python _ACTION_GROUP_LEVERS）。
var actionGroupLevers = map[string]string{
	"prompt":        LeverInstruction,
	"skill":         LeverInstruction,
	"tool":          LeverAction,
	"subagent":      LeverAction,
	"rail":          LeverControl,
	"processor":     LeverControl,
	"config":        LeverConfiguration,
	"configuration": LeverConfiguration,
}

// targetVariableLevers 目标变量 → 杠杆（Python _TARGET_VARIABLE_LEVERS）。
var targetVariableLevers = map[string]string{
	"prompt":           LeverInstruction,
	"prompt_section":   LeverInstruction,
	"identity":         LeverInstruction,
	"soul":             LeverInstruction,
	"skill":            LeverInstruction,
	"instruction":      LeverInstruction,
	"tool":             LeverAction,
	"subagent":         LeverAction,
	"action":           LeverAction,
	"workflow":         LeverControl,
	"context":          LeverControl,
	"memory":           LeverControl,
	"processor":        LeverControl,
	"rail":             LeverControl,
	"control":          LeverControl,
	"execution_budget": LeverConfiguration,
	"budget":           LeverConfiguration,
	"config":           LeverConfiguration,
	"configuration":    LeverConfiguration,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ActionGroupLever 返回手术分组实现的 HarnessX 风格杠杆。
// Python: lever.py:62-64 action_group_lever。
func ActionGroupLever(actionGroup string) string {
	return lookupLever(actionGroupLevers, actionGroup)
}

// TargetRefLever 把分析器目标变量映射到修改杠杆。
// Python: lever.py:66-69 target_ref_lever。
func TargetRefLever(targetRef string) string {
	variable := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(targetRef), "-", "_"))
	variable = lastSegment(variable, ".")
	return lookupLever(targetVariableLevers, variable)
}

// AvailableSurfacesForLever 返回杠杆支持的表面；上游标签是建议不是沙箱。
// Python: lever.py:71-89 available_surfaces_for_lever。
func AvailableSurfacesForLever(lever string, allowedActionGroups []string) []string {
	groups := make(map[string]bool, len(allowedActionGroups))
	for _, group := range allowedActionGroups {
		groups[strings.ToLower(strings.TrimSpace(group))] = true
	}
	// Python: if lever == LEVER_CONFIGURATION: return [g for g in ("config","configuration") if g in groups]
	if lever == LeverConfiguration {
		out := []string{}
		for _, group := range []string{"config", "configuration"} {
			if groups[group] {
				out = append(out, group)
			}
		}
		return out
	}
	// Python: if lever == LEVER_UNRESOLVED: return []
	if lever == LeverUnresolved {
		return []string{}
	}
	// Python: ["prompt_section" if group == "prompt" else group for group in (...) if group in groups]
	out := []string{}
	for _, group := range []string{"prompt", "skill", "tool", "rail", "subagent", "processor"} {
		if groups[group] {
			if group == "prompt" {
				group = "prompt_section"
			}
			out = append(out, group)
		}
	}
	return out
}

// BuildHypothesisLeverPolicy 构建仅供优化器使用的、与表面无关的杠杆决策策略。
// Python: lever.py:91-111 build_hypothesis_lever_policy。
func BuildHypothesisLeverPolicy(targetRef string, targetCaseIDs []string, decisiveProbe map[string]any) map[string]any {
	lever := TargetRefLever(targetRef)
	return map[string]any{
		"recommended_lever":           lever,
		"target_ref":                  strings.TrimSpace(targetRef),
		"why_this_lever":              "Advisory classification from target_ref; select components using the observed operation.",
		"why_not_other_levers":        map[string]any{},
		"predicted_affected_case_ids": sortedUniqueStrings(targetCaseIDs),
		"retroactive_check": map[string]any{
			"decisive_probe": copyStringAnyMap(decisiveProbe),
			"falsification_rule": "Check the predicted behavior against paired evidence independently of task score. " +
				"A score change alone neither confirms nor refutes the cause.",
		},
	}
}

// BuildActionLeverDecision 把具体手术绑定到其不可变的优化器杠杆策略。
// Python: lever.py:113-141 build_action_lever_decision。
func BuildActionLeverDecision(actionGroup string, selectedSurface string, policies []map[string]any) map[string]any {
	recommendedSet := map[string]bool{}
	var recommended []string
	for _, policy := range policies {
		lever := stringOrUnresolved(policy["recommended_lever"])
		if !recommendedSet[lever] {
			recommendedSet[lever] = true
			recommended = append(recommended, lever)
		}
	}
	actual := ActionGroupLever(actionGroup)
	whyThis := make([]string, 0, len(policies))
	for _, policy := range policies {
		whyThis = append(whyThis, stringOrEmpty(policy["why_this_lever"]))
	}
	whyNot := []map[string]any{}
	for _, policy := range policies {
		if m, ok := policy["why_not_other_levers"].(map[string]any); ok {
			whyNot = append(whyNot, copyStringAnyMap(m))
		}
	}
	retro := []map[string]any{}
	for _, policy := range policies {
		if m, ok := policy["retroactive_check"].(map[string]any); ok {
			retro = append(retro, copyStringAnyMap(m))
		}
	}
	matches := len(recommended) > 0 && len(recommended) == 1 && recommended[0] == actual
	return map[string]any{
		"selected_lever":              actual,
		"selected_surface":            selectedSurface,
		"recommended_levers":          recommended,
		"lever_matches_diagnosis":     matches,
		"why_this_lever":              whyThis,
		"why_not_other_levers":        whyNot,
		"predicted_affected_case_ids": predictedAffectedCaseIDs(policies),
		"retroactive_checks":          retro,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// lookupLever 归一化后查表，未命中回落 unresolved（Python .get(..., LEVER_UNRESOLVED)）。
func lookupLever(table map[string]string, key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if lever, ok := table[normalized]; ok {
		return lever
	}
	return LeverUnresolved
}

// lastSegment 取字符串按分隔符切分后的最后一段。
func lastSegment(value string, sep string) string {
	if idx := strings.LastIndex(value, sep); idx >= 0 {
		return value[idx+len(sep):]
	}
	return value
}

// sortedUniqueStrings 去重并排序（Python sorted({...})）。
func sortedUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		normalized := v
		if normalized == "" {
			continue
		}
		if !seen[normalized] {
			seen[normalized] = true
			out = append(out, normalized)
		}
	}
	sort.Strings(out)
	return out
}

// predictedAffectedCaseIDs 汇总策略中的受影响案例 ID（Python _predicted_affected_case_ids）。
func predictedAffectedCaseIDs(policies []map[string]any) []string {
	caseIDs := map[string]bool{}
	for _, policy := range policies {
		raw, ok := policy["predicted_affected_case_ids"].([]any)
		if !ok {
			continue
		}
		for _, item := range raw {
			normalized, _ := item.(string)
			if normalized != "" {
				caseIDs[normalized] = true
			}
		}
	}
	out := make([]string, 0, len(caseIDs))
	for id := range caseIDs {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// stringOrUnresolved 取字符串值，空回落 unresolved（Python `or LEVER_UNRESOLVED`）。
func stringOrUnresolved(value any) string {
	if s, ok := value.(string); ok && s != "" {
		return s
	}
	return LeverUnresolved
}

// stringOrEmpty 取字符串值，空回落空串（Python `or ""`）。
func stringOrEmpty(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// copyStringAnyMap 拷贝 map[string]any（Python dict(...)）。
func copyStringAnyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
