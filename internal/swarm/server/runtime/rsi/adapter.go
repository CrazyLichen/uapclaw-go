package rsi

// RSI adapter 公共助手（对齐 Python adapter.py 182 行的 HARNESS 相关面）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateScenario 按 web §3.5 语义校验 scenario/artifact_type 组合。
// 非法抛 SCENARIO_NOT_SUPPORTED；ARTIFACT 缺 artifact_type 且必填时抛
// BAD_REQUEST；harness 场景不接受 artifact_type。供 create/list/validate 复用。
func ValidateScenario(scenario string, artifactType *string, artifactTypeRequired bool) (*Scenario, *ArtifactType, error) {
	scenarioValue := upper(strings.TrimSpace(scenario))
	var scenarioEnum Scenario
	switch scenarioValue {
	case "HARNESS":
		scenarioEnum = ScenarioHarness
	case "ARTIFACT":
		scenarioEnum = ScenarioArtifact
	case "":
		scenarioEnum = ""
	default:
		return nil, nil, NewScenarioNotSupported(fmt.Sprintf("scenario 非法: %s", scenario))
	}
	var artifactEnum ArtifactType
	if artifactType != nil && *artifactType != "" {
		value := upper(strings.TrimSpace(*artifactType))
		switch value {
		case "PAPER":
			artifactEnum = ArtifactTypePaper
		case "PROGRAM":
			artifactEnum = ArtifactTypeProgram
		default:
			return nil, nil, NewScenarioNotSupported(fmt.Sprintf("artifact_type 非法: %s", *artifactType))
		}
	}
	if scenarioEnum == ScenarioArtifact && artifactTypeRequired && artifactEnum == "" {
		return nil, nil, NewBadRequest("artifact 场景必填 artifact_type", nil)
	}
	if scenarioEnum == ScenarioHarness && artifactEnum != "" {
		return nil, nil, NewBadRequest("harness 场景不接受 artifact_type", nil)
	}
	return &scenarioEnum, &artifactEnum, nil
}

// DefaultValidateInput 边界版数据集校验（adapter 无 validate_input 时的兜底）：
// 路径存在 + JSON 顶层形状；格式语义归引擎侧，sample_count 形状可用。
func DefaultValidateInput(path string) (RsiDatasetResult, error) {
	p := expandUser(path)
	if !fileExists(p) {
		return RsiDatasetResult{}, &RsiError{Code: "PATH_INVALID", Message: fmt.Sprintf("本地路径不存在: %s", p)}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return RsiDatasetResult{Valid: false,
			Errors: []map[string]string{{"reason": fmt.Sprintf("数据集 JSON 解析失败: %v", err), "code": "DATASET_INVALID"}}}, nil
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return RsiDatasetResult{Valid: false,
			Errors: []map[string]string{{"reason": fmt.Sprintf("数据集 JSON 解析失败: %v", err), "code": "DATASET_INVALID"}}}, nil
	}
	var cases []any
	var sampleCount *int
	switch v := raw.(type) {
	case map[string]any:
		if list, ok := v["cases"].([]any); ok {
			cases = list
			n := len(list)
			sampleCount = &n
		} else {
			n := 1
			sampleCount = &n
		}
	case []any:
		cases = v
		n := len(v)
		sampleCount = &n
	default:
		return RsiDatasetResult{Valid: false, SampleCount: sampleCount,
			Errors: []map[string]string{{"reason": "数据集顶层必须是 object 或 array", "code": "DATASET_INVALID"}}}, nil
	}
	_ = cases
	return RsiDatasetResult{Valid: true, SampleCount: sampleCount, Errors: []map[string]string{}}, nil
}

// sameFile 任务路径 basename 校验助手（materializer 复用）。
func safeFilename(name, def string) string {
	candidate := filepath.Base(name)
	if strings.ContainsAny(candidate, "/\\") || candidate == "." || candidate == ".." || candidate == "" {
		return def
	}
	return candidate
}
