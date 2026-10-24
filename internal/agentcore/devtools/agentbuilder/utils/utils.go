package utils

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent 日志组件常量
const logComponent = logger.ComponentAgentCore

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

// SafeJSONLoads 安全解析 JSON 字符串。
// 对齐 Python: utils.safe_json_loads + JsonUtils.safe_json_loads 三层语义：
//  1. 空文本短路返回 default
//  2. def 未传（nil）→ 解析失败返回 BuildError(StatusCommonJsonInputProcessError)
//  3. def 已传 → 解析失败记录 Error 日志后返回 def
//
// 变长参数承载"是否传了 default"的信息（nil 无法区分"没传"与"传了 nil"）。
func SafeJSONLoads(text string, def ...any) (any, error) {
	var defaultValue any
	hasDefault := len(def) > 0
	if hasDefault {
		defaultValue = def[0]
	}

	if text == "" {
		return defaultValue, nil
	}

	var result any
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		if !hasDefault {
			return nil, exception.BuildError(
				exception.StatusCommonJsonInputProcessError,
				exception.WithMsg("JSON decode error"),
			)
		}
		logger.Error(logComponent).
			Str("error", err.Error()).
			Msg("JSON decode error")
		return defaultValue, nil
	}
	return result, nil
}

// LoadJSONFile 加载 JSON 文件。
// 对齐 Python: load_json_file。
// 不存在 → 包裹 fs.ErrNotExist 的错误 + Error 日志；空文件 → 空 map + Warn 日志；
// 顶层非 dict / 解析异常 → BuildError(StatusContextMessageInvalid) + cause 链
// （Python 的 ValueError(顶层非 dict) 在 try 块内被 except 包装为 ValidationError，Go 同路径）。
func LoadJSONFile(filePath string) (map[string]any, error) {
	if _, err := os.Stat(filePath); err != nil {
		logger.Error(logComponent).
			Str("file_path", filePath).
			Msg("File not found")
		return nil, fmt.Errorf("File not found: %s: %w", filePath, fs.ErrNotExist)
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, wrapJSONFileError(filePath, err)
	}

	if len(strings.TrimSpace(string(raw))) == 0 {
		logger.Warn(logComponent).
			Str("file_path", filePath).
			Msg("JSON file is empty")
		return map[string]any{}, nil
	}

	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, wrapJSONFileError(filePath, err)
	}

	m, ok := data.(map[string]any)
	if !ok {
		return nil, wrapJSONFileError(filePath, fmt.Errorf("JSON top level must be object/dict, got: %T", data))
	}
	return m, nil
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

// wrapJSONFileError 包装 JSON 文件读取/解析错误。
// 对齐 Python: load_json_file 的 ValidationError(CONTEXT_MESSAGE_INVALID) + cause 链。
func wrapJSONFileError(filePath string, cause error) error {
	logger.Error(logComponent).
		Str("file_path", filePath).
		Str("error", cause.Error()).
		Msg("JSON parse failed")
	return exception.BuildError(
		exception.StatusContextMessageInvalid,
		exception.WithMsg(fmt.Sprintf("JSON parse error: %s", cause)),
		exception.WithDetails(map[string]any{"file_path": filePath, "error": cause.Error()}),
		exception.WithCause(cause),
	)
}
