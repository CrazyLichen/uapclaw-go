package evaluator

import (
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/judger"
)

// init 向 judger 包注册需求结果契约实现（依赖反转缝，解除 evaluator→judger 反向包环；
// Python score_judge_output 直调本包 requirement_results_contract，步骤归属不变）。
func init() {
	judger.RegisterRequirementResultsContract(RequirementResultsContract)
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// RequirementResultsSchemaVersion 需求结果契约版本。
	RequirementResultsSchemaVersion = 1
	// RequirementGroup 通用需求组。
	RequirementGroup = "requirement"
	// AtomicCheckGroup 原子检查组（WorkBuddy legacy）。
	AtomicCheckGroup = "atomic_check"
	// FailToPassGroup 失败转通过组（SWE-Bench legacy）。
	FailToPassGroup = "fail_to_pass"
	// PassToPassGroup 通过保持组（SWE-Bench legacy）。
	PassToPassGroup = "pass_to_pass"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RequirementKey 需求身份键（Python: (group, requirement_id) 元组）。
type RequirementKey struct {
	// Group 组别
	Group string
	// RequirementID 需求标识
	RequirementID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RequirementResultsContract 构建可序列化的带版本需求结果契约。
// Python: requirement_results.py requirement_results_contract。
func RequirementResultsContract(items []map[string]any) map[string]any {
	return map[string]any{
		"schema_version": RequirementResultsSchemaVersion,
		"items":          NormalizeRequirementResults(items),
	}
}

// RequirementResultsFromJudgeCriteria 适配 [0,1] 区间的准则分。
// Python: requirement_results.py requirement_results_from_judge_criteria。
func RequirementResultsFromJudgeCriteria(criteria any) map[string]any {
	return RequirementResultsFromJudgeCriteriaWithSource(criteria, "judge_detail.criteria")
}

// RequirementResultsFromJudgeCriteriaWithSource 带来源的准则分适配。
func RequirementResultsFromJudgeCriteriaWithSource(criteria any, source string) map[string]any {
	items := []map[string]any{}
	rawList, ok := criteria.([]any)
	if !ok {
		return RequirementResultsContract(items)
	}
	for index, raw := range rawList {
		rawMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		requirementID := trimSpaces(defaultCriterionID(rawMap, index+1))
		if requirementID == "" {
			continue
		}
		score := finiteFloat(rawMap["score"])
		var passed *bool
		if explicit, isBool := rawMap["passed"].(bool); isBool {
			passed = &explicit
		}
		if passed == nil && score != nil {
			value := *score >= 1.0
			passed = &value
		}
		evidence := toSignalString(rawMap["rationale"])
		if evidence == "" {
			evidence = toSignalString(rawMap["evidence"])
		}
		itemSource := toSignalString(rawMap["source"])
		if itemSource == "" {
			itemSource = source
		}
		item := map[string]any{
			"requirement_id": requirementID,
			"group":          RequirementGroup,
			"passed":         boolPtrToAny(passed),
			"score":          floatPtrToAny(score),
			"evidence":       evidence,
			"status":         toSignalString(rawMap["status"]),
			"source":         itemSource,
		}
		items = append(items, item)
	}
	return RequirementResultsContract(items)
}

// EvaluationRequirementResults 读通用契约，回退到 legacy 评估元数据。
// Python: requirement_results.py evaluation_requirement_results。
func EvaluationRequirementResults(metadata map[string]any, caseID string) []map[string]any {
	if _, present := metadata["requirement_results"]; present {
		return NormalizeRequirementResults(metadata["requirement_results"])
	}
	items := legacyAtomicCheckResults(metadata["atomic_checks"])
	items = append(items, legacySWETestResults(metadata["instance_report"], caseID)...)
	return NormalizeRequirementResults(items)
}

// NormalizeRequirementResults 归一化带版本契约或裸条目列表。
// Python: normalize_requirement_results——非法条目忽略；身份为 (group, requirement_id)
// 二元组，后出现者胜出（适配器可确定性地替换较早的不完整观察）。
func NormalizeRequirementResults(value any) []map[string]any {
	var rawItems []any
	switch typed := value.(type) {
	case map[string]any:
		rawItems, _ = typed["items"].([]any)
	case []any:
		rawItems = typed
	case []map[string]any:
		rawItems = make([]any, 0, len(typed))
		for _, item := range typed {
			rawItems = append(rawItems, item)
		}
	}
	if rawItems == nil {
		return []map[string]any{}
	}

	byIdentity := map[RequirementKey]map[string]any{}
	for _, raw := range rawItems {
		rawMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		requirementID := trimSpaces(firstNonEmptyString(
			toSignalString(rawMap["requirement_id"]),
			toSignalString(rawMap["name"]),
			toSignalString(rawMap["id"]),
		))
		if requirementID == "" {
			continue
		}
		group := trimSpaces(toSignalString(rawMap["group"]))
		if group == "" {
			group = RequirementGroup
		}
		var passed any
		if p, isBool := rawMap["passed"].(bool); isBool {
			passed = p
		}
		score := finiteFloat(rawMap["score"])
		evidence := firstNonEmptyString(
			toSignalString(rawMap["evidence"]),
			toSignalString(rawMap["detail"]),
			toSignalString(rawMap["rationale"]),
		)
		byIdentity[RequirementKey{Group: group, RequirementID: requirementID}] = map[string]any{
			"requirement_id": requirementID,
			"group":          group,
			"passed":         passed,
			"score":          floatPtrToAny(score),
			"evidence":       evidence,
			"status":         toSignalString(rawMap["status"]),
			"source":         toSignalString(rawMap["source"]),
		}
	}
	keys := make([]RequirementKey, 0, len(byIdentity))
	for key := range byIdentity {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Group != keys[j].Group {
			return keys[i].Group < keys[j].Group
		}
		return keys[i].RequirementID < keys[j].RequirementID
	})
	result := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		result = append(result, byIdentity[key])
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// legacyAtomicCheckResults WorkBuddy atomic_checks legacy 形态。
// Python: _legacy_atomic_check_results——缺席不是失败证据，保留未知判定。
func legacyAtomicCheckResults(value any) []map[string]any {
	rawList, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	items := []map[string]any{}
	for _, raw := range rawList {
		rawMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		requirementID := trimSpaces(firstNonEmptyString(
			toSignalString(rawMap["name"]),
			toSignalString(rawMap["requirement_id"]),
		))
		if requirementID == "" {
			continue
		}
		var passed any
		if p, isBool := rawMap["passed"].(bool); isBool {
			passed = p
		}
		evidence := firstNonEmptyString(
			toSignalString(rawMap["detail"]),
			toSignalString(rawMap["evidence"]),
		)
		items = append(items, map[string]any{
			"requirement_id": requirementID,
			"group":          AtomicCheckGroup,
			"passed":         passed,
			"score":          floatPtrToAny(finiteFloat(rawMap["score"])),
			"evidence":       evidence,
			"status":         toSignalString(rawMap["status"]),
			"source":         "evaluation.metadata.atomic_checks",
		})
	}
	return items
}

// legacySWETestResults SWE-Bench instance_report legacy 形态。
// Python: _legacy_swe_test_results。
func legacySWETestResults(value any, caseID string) []map[string]any {
	reportMap, ok := value.(map[string]any)
	if !ok {
		return []map[string]any{}
	}
	var report any
	if caseID != "" {
		report = reportMap[caseID]
	}
	if reportMapOrNil(report) == nil && len(reportMap) == 1 {
		for _, only := range reportMap {
			report = only
			break
		}
	}
	reportM := reportMapOrNil(report)
	if reportM == nil {
		return []map[string]any{}
	}
	testsStatus, ok := reportM["tests_status"].(map[string]any)
	if !ok {
		return []map[string]any{}
	}

	items := []map[string]any{}
	legacyGroups := [][2]string{
		{"FAIL_TO_PASS", FailToPassGroup},
		{"PASS_TO_PASS", PassToPassGroup},
	}
	for _, pair := range legacyGroups {
		legacyGroup, group := pair[0], pair[1]
		groupStatus, ok := testsStatus[legacyGroup].(map[string]any)
		if !ok {
			continue
		}
		outcomes := [][3]any{
			{"success", true, 1.0},
			{"failure", false, 0.0},
		}
		for _, outcome := range outcomes {
			outcomeName := outcome[0].(string)
			passed := outcome[1].(bool)
			score := outcome[2].(float64)
			rawIDs, ok := groupStatus[outcomeName].([]any)
			if !ok {
				continue
			}
			for _, rawID := range rawIDs {
				requirementID := trimSpaces(toSignalString(rawID))
				if requirementID == "" {
					continue
				}
				items = append(items, map[string]any{
					"requirement_id": requirementID,
					"group":          group,
					"passed":         passed,
					"score":          score,
					"evidence":       "",
					"status":         outcomeName,
					"source":         "evaluation.metadata.instance_report.tests_status." + legacyGroup,
				})
			}
		}
	}
	return items
}

// defaultCriterionID 准则 ID 缺省（Python: criterion_id or verifier_id or f"criterion_{index}"）。
func defaultCriterionID(raw map[string]any, index int) string {
	if id := toSignalString(raw["criterion_id"]); id != "" {
		return id
	}
	if id := toSignalString(raw["verifier_id"]); id != "" {
		return id
	}
	return "criterion_" + trimSpaces(toSignalString(index))
}

// firstNonEmptyString 首个非空字符串。
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// reportMapOrNil 映射类型守卫。
func reportMapOrNil(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return nil
}

// boolPtrToAny bool 指针转 any（nil 保持 nil——Python bool | None）。
func boolPtrToAny(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

// floatPtrToAny float 指针转 any（nil 保持 nil——Python float | None）。
func floatPtrToAny(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
