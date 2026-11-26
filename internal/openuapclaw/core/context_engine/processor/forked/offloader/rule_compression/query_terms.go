// 相关性词项抽取（规则压缩打分用）。
// 对应 Python 代码：offloader/rule_compression/query_terms.py（93 行）
package rulecompression

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// termRe 稳定词项模式
// Python: query_terms.py:13 (_TERM_RE)
var termRe = regexp.MustCompile(`[A-Za-z0-9_]{3,}`)

// pathArgumentKeys 路径类参数键（值不入词项）
// Python: query_terms.py:14 (_PATH_ARGUMENT_KEYS)
var pathArgumentKeys = map[string]bool{
	"file": true, "file_path": true, "filepath": true,
	"filename": true, "path": true, "paths": true,
}

// nonUserInputArgumentKeys 非用户输入参数键（值不入词项）
// Python: query_terms.py:23 (_NON_USER_INPUT_ARGUMENT_KEYS)
var nonUserInputArgumentKeys = map[string]bool{
	"trusted_dirs": true, "preferred_response_language": true,
	"files_updated_by_user": true, "timezone": true, "timestamp": true,
	"encoding": true, "offset": true, "limit": true, "start": true,
	"end": true, "channel": true, "channels": true, "frontend": true,
	"session_id": true, "context_id": true, "request_id": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractQueryTerms 抽取稳定小写词项集合（用户内容 + 工具名 + 参数值，路径/非用户键跳过）。
// Python: query_terms.py:36 (extract_query_terms)
func ExtractQueryTerms(userContent string, toolName string, toolArguments any) map[string]bool {
	terms := map[string]bool{}
	addTerms(terms, userContent)
	if toolName != "" {
		addTerms(terms, toolName)
	}
	addTermsFromValue(terms, parseJSONIfPossible(toolArguments))
	return terms
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// addTerms 词项收集（路径分隔符前缀跳过）。
// Python: query_terms.py:51 (_add_terms)
func addTerms(terms map[string]bool, value string) {
	for _, loc := range termRe.FindAllStringIndex(value, -1) {
		start := loc[0]
		if start > 0 && (value[start-1] == '/' || value[start-1] == '\\') {
			continue
		}
		terms[strings.ToLower(value[loc[0]:loc[1]])] = true
	}
}

// addTermsFromValue 递归收集：字符串直收；map 收键 + 跳过路径/非用户键；切片递归；其余转字符串。
// Python: query_terms.py:58 (_add_terms_from_value)
func addTermsFromValue(terms map[string]bool, value any) {
	switch v := value.(type) {
	case nil:
		return
	case string:
		addTerms(terms, v)
		return
	case map[string]any:
		for key, item := range v {
			addTermsFromValue(terms, key)
			lower := strings.ToLower(key)
			if pathArgumentKeys[lower] || nonUserInputArgumentKeys[lower] {
				continue
			}
			addTermsFromValue(terms, item)
		}
		return
	case []any:
		for _, item := range v {
			addTermsFromValue(terms, item)
		}
		return
	default:
		addTerms(terms, toStringValue(value))
	}
}

// parseJSONIfPossible 字符串尝试 JSON 解析（失败原样返回）。
// Python: query_terms.py:78 (_parse_json_if_possible)
func parseJSONIfPossible(value any) any {
	text, ok := value.(string)
	if !ok {
		return value
	}
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return value
	}
	return parsed
}

// toStringValue Python str(value) 的 Go 等价（数值/布尔等 %v 形态足够词项抽取）。
func toStringValue(value any) string {
	return fmt.Sprint(value)
}
