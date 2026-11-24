// tree.go 对齐 Python build/io/tree.py（124 行）：tree_index.yaml 读写与节点排序。
package io

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// IsS3URI S3 URI 判定（对齐 Python shared/storage.is_s3_uri 的调用面语义：
// s3:// 前缀即判定；物化与读取分支未实现，见 D-16-4-1）。
func IsS3URI(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "s3://")
}

// WriteTreePreset 写 tree_index.yaml（节点排序后逐字段输出）。
// 对齐 Python write_tree_preset（tree.py:13-39）：仅 cid/type 必输出，
// description/select_when/dont_select_when/source_description/worker_id 非空才输出。
func WriteTreePreset(payload map[string]any, path string) error {
	rawNodes, _ := payload["nodes"].([]any)
	nodes := SortTreeNodes(rawNodes)
	var lines []string
	lines = append(lines, "nodes:")
	for _, nodeAny := range nodes {
		node, ok := nodeAny.(map[string]any)
		if !ok {
			continue
		}
		lines = append(lines, "  - cid: "+JSONString(strAny(node, "cid")))
		lines = append(lines, "    type: "+JSONString(strAny(node, "type")))
		for _, field := range [...]string{"description", "select_when", "dont_select_when", "source_description", "worker_id"} {
			v := strAny(node, field)
			if field == "worker_id" {
				v = strings.TrimSpace(v)
			}
			if v != "" {
				lines = append(lines, "    "+field+": "+JSONString(v))
			}
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// SortTreeNodes 节点排序：按 cid 段数升序，再按 cid 字典序；非字典项剔除。
// 对齐 Python sort_tree_nodes（tree.py:42-49）。
func SortTreeNodes(nodes []any) []any {
	type sortable struct {
		key [2]string
		val map[string]any
	}
	items := make([]sortable, 0, len(nodes))
	for _, n := range nodes {
		m, ok := n.(map[string]any)
		if !ok {
			continue
		}
		cid := strAny(m, "cid")
		segs := strings.Split(cid, ".")
		items = append(items, sortable{[2]string{strconv.Itoa(len(segs)), cid}, m})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].key[0] != items[j].key[0] {
			return items[i].key[0] < items[j].key[0]
		}
		return items[i].key[1] < items[j].key[1]
	})
	out := make([]any, len(items))
	for i, it := range items {
		out[i] = it.val
	}
	return out
}

// NormalizeItemPaths 路径规范化去重：passthrough URI 原样保留，本地路径绝对化。
// 对齐 Python normalize_item_paths（tree.py:52-68）。
func NormalizeItemPaths(itemPaths []string) []string {
	var normalized []string
	seen := map[string]bool{}
	for _, item := range itemPaths {
		raw := strings.TrimSpace(item)
		if raw == "" {
			continue
		}
		var key string
		if IsPassthroughItemURI(raw) {
			key = raw
		} else {
			expanded := raw
			if strings.HasPrefix(expanded, "~") {
				if home, err := os.UserHomeDir(); err == nil {
					expanded = filepath.Join(home, expanded[1:])
				}
			}
			abs, err := filepath.Abs(expanded)
			if err != nil {
				abs = expanded
			}
			key = abs
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		normalized = append(normalized, key)
	}
	return normalized
}

// LoadTreePreset 读 tree_index.yaml：YAML 成功则排序 nodes 返回；否则回落简单解析。
// 对齐 Python load_tree_preset（tree.py:71-84）。
func LoadTreePreset(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	var payload map[string]any
	if yerr := yaml.Unmarshal(data, &payload); yerr == nil && payload != nil {
		nodes, _ := payload["nodes"].([]any)
		payload["nodes"] = SortTreeNodes(nodes)
		return payload, nil
	}
	return ParseSimpleNodesYAML(text), nil
}

// ParseSimpleNodesYAML 简单 nodes YAML 解析（YAML 不可用时的回落路径）。
// 对齐 Python parse_simple_nodes_yaml（tree.py:87-120）：`  - ` 起新节点、
// 值先试 JSON 解析（引号/数组/对象/布尔/null/数字），失败剥引号。
func ParseSimpleNodesYAML(text string) map[string]any {
	var nodes []any
	var current map[string]any
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimRight(rawLine, "\r\n \t")
		if strings.TrimSpace(line) == "nodes:" {
			continue
		}
		if strings.HasPrefix(line, "  - ") {
			if current != nil {
				nodes = append(nodes, current)
			}
			current = map[string]any{}
			line = line[4:]
		} else if current == nil {
			continue
		} else {
			line = strings.TrimSpace(line)
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		current[strings.TrimSpace(key)] = parseSimpleNodeValue(strings.TrimSpace(value))
	}
	if current != nil {
		nodes = append(nodes, current)
	}
	return map[string]any{"nodes": SortTreeNodes(nodes)}
}

// parseSimpleNodeValue 单值解析（对齐 tree.py:109-119 分支序）
func parseSimpleNodeValue(raw string) any {
	if raw == "" {
		return ""
	}
	quote := raw[:1]
	looksStructured := quote == `"` || quote == "[" || quote == "{" || quote == "-" ||
		raw == "true" || raw == "false" || raw == "null" ||
		strings.Replace(raw, ".", "", 1) != "" && isAllDigits(strings.Replace(raw, ".", "", 1))
	if looksStructured {
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err == nil {
			return v
		}
		return strings.Trim(raw, `"`)
	}
	return raw
}

// isAllDigits 全数字判定
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// strAny 取 map 字符串字段（nil 安全，对齐 str(node.get(k, ""))）
func strAny(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

// JSONString ensure_ascii=False 的 json.dumps 等价（Go json.Marshal 默认 UTF-8 直出，
// HTML 转义关闭以对齐 ensure_ascii 语义）。
func JSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
