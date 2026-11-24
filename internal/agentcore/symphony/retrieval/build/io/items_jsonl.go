// items_jsonl.go 对齐 Python build/io/items_jsonl.py（138 行）：JSONL 读取与解析。
// S3/HTTP 远端分支仅保留 URI 判定（D-16-4-1：S3 读文本返回登记错误；HTTP 读为
// discovery 未消费路径，按偏差一并裁剪——discovery 调用面仅覆盖 is_passthrough_item_uri）。
package io

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsHTTPURI http(s) URI 判定（对齐 items_jsonl.py:15-19）
func IsHTTPURI(value string) bool {
	v := strings.TrimSpace(value)
	lower := strings.ToLower(v)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// IsPassthroughItemURI passthrough 判定：S3/http/jsonl:// 均不做本地化。
// 对齐 Python is_passthrough_item_uri（items_jsonl.py:22-27）。
func IsPassthroughItemURI(value string) bool {
	raw := strings.TrimSpace(value)
	lower := strings.ToLower(raw)
	return IsS3URI(raw) || IsHTTPURI(raw) || strings.HasPrefix(lower, "jsonl://")
}

// ErrS3NotImplemented S3 分支未实现（偏差 D-16-4-1）
var ErrS3NotImplemented = errors.New("s3 item source not implemented in Go (deviation D-16-4-1); use local path")

// ErrHTTPNotImplemented HTTP 分支未实现（discovery 未消费，偏差登记同 D-16-4-1 缝）
var ErrHTTPNotImplemented = errors.New("http item source not implemented in Go (deviation D-16-4-1)")

// LoadItemsJSONLText 读 items JSONL 文本（本地分支；S3/HTTP 返回登记错误）。
// 对齐 Python load_items_jsonl_text（items_jsonl.py:33-45）。
func LoadItemsJSONLText(itemJSONLPath string) (string, error) {
	rawPath := strings.TrimSpace(itemJSONLPath)
	if rawPath == "" {
		return "", nil
	}
	if IsS3URI(rawPath) {
		return "", ErrS3NotImplemented
	}
	if IsHTTPURI(rawPath) {
		return "", ErrHTTPNotImplemented
	}
	expanded := rawPath
	if strings.HasPrefix(expanded, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = filepath.Join(home, expanded[1:])
		}
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		abs = expanded
	}
	if _, serr := os.Stat(abs); serr != nil {
		return "", fmt.Errorf("JSONL path not found: %s", abs)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParseJSONLScannedItems 解析 JSONL 内容为 (scanned by skillId, ordered jsonl:// paths)。
// 对齐 Python parse_jsonl_scanned_items（items_jsonl.py:48-138）：
// raw_decode 逐对象（空白/逗号分隔）、坏项跳至下一 "{" 重同步、缺
// contentExtendParam/skillId 剔除、skillId 重复忽略、字段多键回落。
func ParseJSONLScannedItems(jsonlContent string) (map[string]map[string]any, []string) {
	scanned := map[string]map[string]any{}
	var orderedPaths []string
	seenPaths := map[string]bool{}
	itemNo := 0
	text := strings.TrimPrefix(jsonlContent, "\ufeff") // BOM 剥离（对齐 :57 lstrip("\ufeff")）
	index := 0
	for index < len(text) {
		// 跳过空白与逗号分隔（对齐 Python :59-62）
		for index < len(jsonlContent) && (isSpaceByte(jsonlContent[index]) || jsonlContent[index] == ',') {
			index++
		}
		if index >= len(jsonlContent) {
			break
		}
		// raw_decode 语义：从当前 offset 解一个 JSON 对象
		payload, nextIndex, derr := decodeJSONObjectAt(text, index)
		if derr != nil {
			// 坏项：跳到下一个 "{" 重同步（对齐 Python :75-80）
			nextBrace := strings.IndexByte(text[index+1:], '{')
			if nextBrace == -1 {
				break
			}
			index = index + 1 + nextBrace
			continue
		}
		index = nextIndex
		itemNo++
		if payload == nil {
			continue
		}
		contentExtend, ok := payload["contentExtendParam"].(map[string]any)
		if !ok {
			continue // Invalid JSON item: missing object field 'contentExtendParam'
		}
		skillID := strings.TrimSpace(strOr(contentExtend, "skillId"))
		if skillID == "" {
			continue
		}
		if _, dup := scanned[skillID]; dup {
			continue
		}
		skillName := strings.TrimSpace(strOr(contentExtend, "skillName"))
		if skillName == "" {
			skillName = skillID
		}
		skillDesc := strings.TrimSpace(strOr(contentExtend, "skillDesc"))
		sourcePath := fmt.Sprintf("jsonl://skill/%s", skillID)
		skillPath := strings.TrimSpace(firstNonEmpty(contentExtend, "skillPath", "skill_path", "path", sourcePath))
		skillContent := strings.TrimSpace(firstNonEmpty(contentExtend, "skillContent", "skill_content", "content", ""))
		description := skillDesc
		if description == "" {
			description = skillName
		}
		stars := 0
		if v, ok := contentExtend["stars"]; ok && v != nil {
			if n, ok := v.(json.Number); ok {
				if iv, err := n.Int64(); err == nil {
					stars = int(iv)
				}
			}
		}
		isOfficial, _ := contentExtend["isOfficial"].(bool)
		if !isOfficial {
			isOfficial, _ = contentExtend["is_official"].(bool)
		}
		metaCopy := map[string]any{}
		for k, v := range contentExtend {
			metaCopy[k] = v
		}
		scanned[skillID] = map[string]any{
			"id":                   skillID,
			"name":                 skillName,
			"description":          description,
			"skill_path":           skillPath,
			"path":                 skillPath,
			"content":              skillContent,
			"github_url":           firstNonEmpty(contentExtend, "githubUrl", "github_url", ""),
			"stars":                stars,
			"is_official":          isOfficial,
			"author":               strOr(contentExtend, "author"),
			"content_hash":         firstNonEmpty(contentExtend, "contentHash", "content_hash", ""),
			"content_extend_param": metaCopy,
		}
		if !seenPaths[sourcePath] {
			seenPaths[sourcePath] = true
			orderedPaths = append(orderedPaths, sourcePath)
		}
	}
	return scanned, orderedPaths
}

// isSpaceByte JSON 空白字节判定
func isSpaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

// decodeJSONObjectAt 从 offset 起解一个 JSON 对象，返回 (payload, next offset, err)。
// 等价 Python JSONDecoder.raw_decode。
func decodeJSONObjectAt(text string, start int) (map[string]any, int, error) {
	dec := json.NewDecoder(strings.NewReader(text[start:]))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		return nil, 0, err
	}
	// 计算 next offset：Decode 后 Decoder.InputOffset 为已消费字节数
	return payload, start + int(dec.InputOffset()), nil
}

// strOr 字符串字段（nil/类型不符回落空串）
func strOr(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// firstNonEmpty 依次取第一个非空字符串键
func firstNonEmpty(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strOr(m, k); s != "" {
			return s
		}
	}
	return ""
}
