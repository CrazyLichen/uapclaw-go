package dataloader

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// UnknownValue 未知值占位（对齐 UNKNOWN_VALUE）
const UnknownValue = "unknown"

// ──────────────────────────── 结构体 ────────────────────────────

// DatasetProfiler 数据集画像助手（对齐 DatasetProfiler；静态方法 → 包级函数）
type DatasetProfiler struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Profile 构建确定性元数据摘要（对齐 DatasetProfiler.profile）
func (DatasetProfiler) Profile(cases []map[string]any, balanceKeys []string) map[string]any {
	summary := map[string]any{"total_cases": len(cases)}
	warnings := []any{}
	for _, key := range balanceKeys {
		counter := map[string]int{}
		for _, caseMapping := range cases {
			counter[CaseValue(caseMapping, key)]++
		}
		delete(counter, UnknownValue)
		summary[key] = sortedCounter(counter)
	}
	for _, caseMapping := range cases {
		missingFields := []string{}
		for _, key := range balanceKeys {
			if CaseValue(caseMapping, key) == UnknownValue {
				missingFields = append(missingFields, key)
			}
		}
		if len(missingFields) > 0 {
			warnings = append(warnings, map[string]any{
				"case_id":        CaseID(caseMapping),
				"missing_fields": missingFields,
			})
		}
	}
	return map[string]any{
		"summary":  summary,
		"warnings": warnings,
		"metadata": map[string]any{
			"quality": profileQuality(len(cases), len(warnings)),
		},
	}
}

// CaseValue 从顶层字段或嵌套 metadata 读取均衡值（对齐 case_value）
func CaseValue(caseMapping map[string]any, key string) string {
	value := caseMapping[key]
	if isEmptyValue(value) {
		if metadata, ok := caseMapping["metadata"].(map[string]any); ok {
			value = metadata[key]
		}
	}
	if isEmptyValue(value) {
		return UnknownValue
	}
	return fmt.Sprintf("%v", value)
}

// CaseID 返回计划与告警用的稳定用例标识（对齐 case_id）
func CaseID(caseMapping map[string]any) string {
	value := caseMapping["case_id"]
	if isEmptyValue(value) {
		value = caseMapping["id"]
	}
	if isEmptyValue(value) {
		casePath, _ := caseMapping["case_path"].(string)
		stem := strings.TrimSuffix(filepath.Base(casePath), filepath.Ext(casePath))
		if casePath == "" {
			stem = "case"
		}
		caseIndex := 0
		if raw, ok := caseMapping["case_index"].(int); ok {
			caseIndex = raw
		}
		return fmt.Sprintf("%s#%d", stem, caseIndex)
	}
	return fmt.Sprintf("%v", value)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// profileQuality 画像质量分级（对齐 _profile_quality）
func profileQuality(totalCases, warningCount int) string {
	if totalCases == 0 {
		return "empty"
	}
	if warningCount == 0 {
		return "normal"
	}
	if float64(warningCount)/float64(totalCases) >= 0.5 {
		return "low_quality_fallback"
	}
	return "partial_metadata"
}

// isEmptyValue 判断 None/空串语义（对齐 Python value in (None, "")）
func isEmptyValue(value any) bool {
	if value == nil {
		return true
	}
	str, isStr := value.(string)
	return isStr && str == ""
}

// sortedCounter 计数器按键排序输出（对齐 dict(sorted(counter.items()))）
func sortedCounter(counter map[string]int) map[string]int {
	keys := make([]string, 0, len(counter))
	for key := range counter {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]int, len(counter))
	for _, key := range keys {
		out[key] = counter[key]
	}
	return out
}
