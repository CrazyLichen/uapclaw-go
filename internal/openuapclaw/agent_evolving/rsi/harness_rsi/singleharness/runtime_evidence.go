// 把「缺失能力观察」与「已被证实的未使用」区分开（runtime_evidence.py 一比一对齐）。
package singleharness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InconclusiveCapabilityCases 返回负激活声明缺少完整时序证据的用例（Python inconclusive_capability_cases）。
func InconclusiveCapabilityCases(evalRefPath string, missingInvocations []map[string]string) map[string]bool {
	required := map[string]bool{}
	for _, item := range missingInvocations {
		target := ""
		if item != nil {
			target = item["target_case_id"]
		}
		required[target] = true
	}
	if len(required) == 0 {
		return map[string]bool{}
	}
	ref := filepath.Clean(evalRefPath)
	raw, err := os.ReadFile(ref)
	if err != nil {
		return copyKeys(required)
	}
	var payload map[string]any
	if err := yaml.Unmarshal(raw, &payload); err != nil {
		return copyKeys(required)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	rawCases, ok := payload["cases"].([]any)
	if !ok {
		return copyKeys(required)
	}
	inconclusive := map[string]bool{}
	for _, rawCase := range rawCases {
		item, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		caseID := ""
		if raw, present := item["case_id"]; present {
			caseID = toString(raw)
		}
		if !required[caseID] {
			continue
		}
		resultPath := filepath.Clean(toString(item["result_path"]))
		if resultPath == "" {
			resultPath = "."
		}
		if !filepath.IsAbs(resultPath) {
			resultPath = filepath.Join(filepath.Dir(ref), resultPath)
		}
		resultRaw, readErr := os.ReadFile(resultPath)
		if readErr != nil {
			inconclusive[caseID] = true
			continue
		}
		var result map[string]any
		if jsonErr := json.Unmarshal(resultRaw, &result); jsonErr != nil {
			inconclusive[caseID] = true
			continue
		}
		metadata, _ := result["metadata"].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
		}
		execution, _ := metadata["execution"].(map[string]any)
		if execution == nil {
			execution = map[string]any{}
		}
		quality, hasQuality := execution["execution_evidence"]
		statusComplete := false
		timingComplete := false
		if qualityMap, isMap := quality.(map[string]any); isMap {
			status, _ := qualityMap["status"].(string)
			statusComplete = status == "complete"
			timing, _ := qualityMap["timing_complete"].(bool)
			timingComplete = timing
		}
		_ = hasQuality
		if !statusComplete || !timingComplete {
			inconclusive[caseID] = true
		}
	}
	return inconclusive
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// copyKeys 复制键集合。
func copyKeys(source map[string]bool) map[string]bool {
	copied := make(map[string]bool, len(source))
	for key := range source {
		copied[key] = true
	}
	return copied
}

// toString 任意值转字符串（Python str() 语义）。
func toString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	if value == nil {
		return ""
	}
	return jsonNumberToString(value)
}

// jsonNumberToString 数值的字符串化。
func jsonNumberToString(value any) string {
	switch v := value.(type) {
	case int:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
	return ""
}
