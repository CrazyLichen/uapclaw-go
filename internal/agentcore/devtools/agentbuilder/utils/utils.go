package utils

import (
	"regexp"
	"strings"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// jsonExtractRegexp JSON 围栏提取预编译正则（对齐 Python JSON_EXTRACT_PATTERN + re.findall 首个匹配）
	jsonExtractRegexp = regexp.MustCompile("(?s)" + JSONExtractPattern)

	// sessionIDRegexp 会话 ID 校验预编译正则（对齐 Python ^[a-zA-Z0-9_-]+$）
	sessionIDRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractJSONFromText 从文本中提取 JSON 内容。
// 对齐 Python: extract_json_from_text。
// 支持从 Markdown 代码块（```json ... ```）中提取；无围栏时返回原文；空文本原样返回。
func ExtractJSONFromText(text string) string {
	if text == "" {
		return text
	}
	if m := jsonExtractRegexp.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return text
}

// FormatDialogHistory 将对话历史格式化为字符串。
// 对齐 Python: format_dialog_history。
// 每条消息格式化为 "role: content"，缺 role 键记 "unknown"，缺 content 键记空串。
// separator 变长参数默认 "\n"。
func FormatDialogHistory(history []map[string]string, separator ...string) string {
	if len(history) == 0 {
		return ""
	}
	sep := "\n"
	if len(separator) > 0 {
		sep = separator[0]
	}
	parts := make([]string, 0, len(history))
	for _, msg := range history {
		role := "unknown"
		if v, ok := msg["role"]; ok {
			role = v
		}
		content := ""
		if v, ok := msg["content"]; ok {
			content = v
		}
		parts = append(parts, role+": "+content)
	}
	return strings.Join(parts, sep)
}

// ValidateSessionID 校验会话 ID 格式。
// 对齐 Python: validate_session_id。
// 仅允许字母、数字、下划线和连字符；空串返回 false。
func ValidateSessionID(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	return sessionIDRegexp.MatchString(sessionID)
}

// MergeDictLists 合并字典列表并按唯一键去重。
// 对齐 Python: merge_dict_lists。
// 就地追加语义（Python 原地 append 并返回同一 list，调用方 base.py 依赖此行为）；
// key 值缺失或为 falsy（空串/0/false）的条目跳过；uniqueKey 变长参数默认 "resource_id"。
func MergeDictLists(existing, newItems []map[string]any, uniqueKey ...string) []map[string]any {
	key := "resource_id"
	if len(uniqueKey) > 0 {
		key = uniqueKey[0]
	}
	if len(newItems) == 0 {
		return existing
	}

	existingKeys := make(map[any]bool)
	for _, item := range existing {
		if v, ok := item[key]; ok && isTruthy(v) {
			existingKeys[v] = true
		}
	}
	for _, item := range newItems {
		keyValue, ok := item[key]
		if !ok || !isTruthy(keyValue) {
			continue
		}
		if !existingKeys[keyValue] {
			existing = append(existing, item)
			existingKeys[keyValue] = true
		}
	}
	return existing
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isTruthy 判断值是否为 Python truthy 语义（空串/0/false 视为 falsy）。
// 对齐 Python: item[unique_key] 的隐式布尔转换。
func isTruthy(v any) bool {
	switch val := v.(type) {
	case string:
		return val != ""
	case bool:
		return val
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	case nil:
		return false
	default:
		return true
	}
}
