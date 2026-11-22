package rsi

// Evo-Bench 验证集归一化（供单 harness 引擎）。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/validation_dataset.py（66 行）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NormalizeValidationSuite 返回引擎数据集载荷；标准数据集返回 nil。
// 公开 GDPVAL 套件把任务存在 validation 键下并以 id 标识；引擎装载器则期望
// cases 数组（case_id + input 字段）。
func NormalizeValidationSuite(sourcePath string) (map[string]any, error) {
	source, err := filepath.Abs(strings.ReplaceAll(sourcePath, "~", userHomeDir()))
	if err != nil {
		source = sourcePath
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, NewDatasetInvalid(fmt.Sprintf("训练集不是有效 JSON: %v", err), nil)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, NewDatasetInvalid(fmt.Sprintf("训练集不是有效 JSON: %v", err), nil)
	}
	if payload == nil {
		return nil, nil
	}
	rawValidation, isList := payload["validation"].([]any)
	if !isList {
		return nil, nil
	}
	if _, hasCases := payload["cases"].([]any); hasCases {
		return nil, nil
	}
	if len(rawValidation) == 0 {
		return nil, NewDatasetInvalid("Evo-Bench 训练集 validation 不能为空", nil)
	}
	cases := []map[string]any{}
	seenCaseIDs := map[string]bool{}
	for index, item := range rawValidation {
		task, ok := item.(map[string]any)
		if !ok {
			return nil, NewDatasetInvalid(fmt.Sprintf("第 %d 条训练样本必须是对象", index+1), nil)
		}
		caseID := strings.TrimSpace(valueToString(task["id"]))
		domain := strings.ToLower(valueToString(task["domain"]))
		sourceName := caseID
		if i := strings.Index(caseID, "-"); i >= 0 {
			sourceName = caseID[:i]
		}
		prompt := valueToString(task["prompt"])
		if caseID == "" {
			return nil, NewDatasetInvalid(fmt.Sprintf("第 %d 条训练样本缺少 id", index+1), nil)
		}
		if seenCaseIDs[caseID] {
			return nil, NewDatasetInvalid(fmt.Sprintf("训练样本 id 重复: %s", caseID), nil)
		}
		if domain != "general" && domain != "office" {
			return nil, NewDatasetInvalid(fmt.Sprintf("训练样本 %s 仅支持 general/office 领域", caseID), nil)
		}
		if sourceName != "claw" && sourceName != "gdpval" && sourceName != "apex" {
			return nil, NewDatasetInvalid(fmt.Sprintf("训练样本 %s 不支持来源: %s", caseID, sourceName), nil)
		}
		if strings.TrimSpace(prompt) == "" {
			return nil, NewDatasetInvalid(fmt.Sprintf("训练样本 %s 缺少 prompt", caseID), nil)
		}
		seenCaseIDs[caseID] = true
		// 对齐 Python {**task, ...}：原字段全保留，派生键覆盖。
		copied := map[string]any{}
		for k, v := range task {
			copied[k] = v
		}
		copied["case_id"] = caseID
		copied["task_id"] = caseID
		copied["input"] = prompt
		copied["domain"] = domain
		copied["source"] = sourceName
		taskType := domain
		if metadata, ok := task["metadata"].(map[string]any); ok && valueToString(metadata["task_type"]) != "" {
			taskType = valueToString(metadata["task_type"])
		}
		copied["task_type"] = taskType
		cases = append(cases, copied)
	}
	return map[string]any{"dataset_id": "evobench_local_no_key_validation", "cases": cases}, nil
}

// userHomeDir HOME 环境变量回退（~ 展开）。
func userHomeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	return "."
}
