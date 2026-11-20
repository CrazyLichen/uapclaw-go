package dataloader

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AdaptReference 把显式声明的私有引用翻译成既有 verifier 输入
// （对齐 adapt_reference；后端选择必须显式，绝不从文件名或 ID 推断）
func AdaptReference(caseMapping map[string]any, datasetPath string) (map[string]any, error) {
	declarations := []map[string]any{}
	privatePaths := []string{}
	pairs, paths, err := ReferencedFiles(caseMapping, filepath.Dir(datasetPath))
	if err != nil {
		return nil, err
	}
	for index, pair := range pairs {
		if pair[0] != "reference" {
			continue
		}
		path := paths[index]
		privatePaths = append(privatePaths, path)
		if strings.ToLower(filepath.Ext(path)) != ".json" {
			continue
		}
		raw, err := osReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("invalid reference JSON: %s", filepath.Base(path))
		}
		var payload any
		if jsonErr := json.Unmarshal(raw, &payload); jsonErr != nil {
			return nil, fmt.Errorf("invalid reference JSON: %s", filepath.Base(path))
		}
		if mapping, ok := payload.(map[string]any); ok {
			if _, hasAdapter := mapping["adapter"]; hasAdapter {
				declarations = append(declarations, mapping)
			}
		}
	}
	if len(declarations) == 0 {
		return caseMapping, nil
	}
	if len(declarations) != 1 {
		return nil, fmt.Errorf("reference.files must declare exactly one verifier adapter")
	}
	declaration := declarations[0]
	adapter, _ := declaration["adapter"].(string)
	if adapter != "swebench_official" {
		return nil, fmt.Errorf("unsupported reference adapter: %s", adapter)
	}
	if _, hasLegacy := caseMapping["swebench"]; hasLegacy {
		return nil, fmt.Errorf("do not combine legacy swebench configuration with a reference adapter")
	}
	rawConfig, ok := declaration["config"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("swebench_official reference adapter requires a config object")
	}
	config := copyMap(rawConfig)
	instanceID, isStr := config["instance_id"].(string)
	if !isStr || instanceID != fmt.Sprintf("%v", caseMapping["case_id"]) {
		return nil, fmt.Errorf("reference instance_id must match case_id")
	}
	rawPath, isStr := config["official_dataset_path"].(string)
	if !isStr {
		return nil, fmt.Errorf("official_dataset_path must reference a declared private dataset file")
	}
	officialPath, err := ResolveDatasetFile(filepath.Dir(datasetPath), rawPath)
	if err != nil {
		return nil, err
	}
	declared := false
	for _, path := range privatePaths {
		if path == officialPath {
			declared = true
			break
		}
	}
	if !declared {
		return nil, fmt.Errorf("official_dataset_path must be included in reference.files")
	}
	raw, err := osReadFile(officialPath)
	if err != nil {
		return nil, err
	}
	var records []any
	if jsonErr := json.Unmarshal(raw, &records); jsonErr != nil {
		return nil, jsonErr
	}
	if records == nil {
		return nil, fmt.Errorf("official SWE dataset must be a list")
	}
	selected := []map[string]any{}
	for _, record := range records {
		if mapping, ok := record.(map[string]any); ok && fmt.Sprintf("%v", mapping["instance_id"]) == instanceID {
			selected = append(selected, mapping)
		}
	}
	if len(selected) != 1 {
		return nil, fmt.Errorf("official dataset must contain exactly one matching instance_id")
	}
	for _, key := range []string{"repo", "base_commit", "version", "test_patch"} {
		if declaredValue, exists := config[key]; exists {
			if fmt.Sprintf("%v", declaredValue) != fmt.Sprintf("%v", selected[0][key]) {
				return nil, fmt.Errorf("reference config conflicts with official dataset: %s", key)
			}
		}
	}
	config["official_dataset_path"] = officialPath
	merged := copyMap(caseMapping)
	merged["swebench"] = config
	return merged, nil
}
