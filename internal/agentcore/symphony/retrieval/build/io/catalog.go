// catalog.go 对齐 Python build/io/catalog.py（35 行）：catalog.jsonl 读取。
package io

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/build"
)

// LoadCatalogRecords 逐行读 catalog.jsonl 为目录记录；文件不存在返回空表。
// 对齐 Python load_catalog_records（catalog.py:11-32）：worker_id 取
// worker_id/skill_id 双键回落；branch_path 逐项字符串化。
func LoadCatalogRecords(path string) ([]build.CatalogRecord, error) {
	if _, err := os.Stat(path); err != nil {
		return []build.CatalogRecord{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var records []build.CatalogRecord
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			return nil, err
		}
		var branchPath []string
		if bp, ok := payload["branch_path"].([]any); ok {
			for _, item := range bp {
				branchPath = append(branchPath, toString(item))
			}
		}
		metadata := map[string]any{}
		if md, ok := payload["metadata"].(map[string]any); ok {
			for k, v := range md {
				metadata[k] = v
			}
		}
		records = append(records, build.CatalogRecord{
			WorkerID:      toString(payload["worker_id"], payload["skill_id"]),
			CID:           toString(payload["cid"], ""),
			Name:          toString(payload["name"], ""),
			Description:   toString(payload["description"], ""),
			SkillPath:     toString(payload["skill_path"], ""),
			BranchPath:    branchPath,
			Category:      toString(payload["category"], ""),
			RetrievalText: toString(payload["retrieval_text"], ""),
			Metadata:      metadata,
		})
	}
	return records, nil
}

// toString 首个非空键的字符串化（nil 安全）
func toString(values ...any) string {
	for _, v := range values {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok {
			return s
		}
		return strings.TrimSpace(strings.Trim(strings.TrimSpace(jsonStringOf(v)), `"`))
	}
	return ""
}

// jsonStringOf 值的 JSON 文本化
func jsonStringOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
