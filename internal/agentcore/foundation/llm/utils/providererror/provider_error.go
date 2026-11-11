package providererror

import (
	"fmt"
	"regexp"
	"strconv"
)

// ──────────────────────────── 常量 ────────────────────────────

// MaxProviderErrorChars 错误体截断上限。
// Python: provider_error.py:17 MAX_PROVIDER_ERROR_CHARS = 4000
const MaxProviderErrorChars = 4000

// statusInTextRe 从错误文本推断 HTTP 状态码。
// Python: provider_error.py:23-25 _STATUS_IN_TEXT_RE
var statusInTextRe = regexp.MustCompile(`(?i)(?:error\s+code|status(?:\s+code)?|http)\D{0,16}([1-5]\d{2})\b`)

// ──────────────────────────── 导出函数 ────────────────────────────

// FormatProviderException 格式化 provider 异常，不灌 HTML/XML 响应体。
// 空消息仍回退异常类型名（对齐此前 TimeoutError/ReadError 行为，供流超时
// 与重试分类器使用）。HTTP 状态码可选：无状态的超时/传输失败保持原文。
//
// Go 无异常类型名语义，typeName 由调用方以 fmt.Sprintf("%T") 短名传入；
// statusCode 来自 HTTP 层（Python 经 exc.status_code / exc.response 提取）。
//
// Python: provider_error.py:30-44 format_provider_exception
func FormatProviderException(exc error, typeName string, statusCode int, includeExcType bool) string {
	if exc == nil {
		return ""
	}
	raw := trimSpace(exc.Error())
	if raw == "" {
		return typeName
	}
	summarized := SummarizeProviderErrorText(raw, statusCode)
	if includeExcType {
		return typeName + ": " + summarized
	}
	return summarized
}

// SummarizeProviderErrorText 返回面向用户的错误体，折叠 HTML/XML 页面。
// Python: provider_error.py:47-59 summarize_provider_error_text
func SummarizeProviderErrorText(text string, statusCode int) string {
	raw := text
	if raw == "" {
		return ""
	}
	markup, ok := markupPayload(raw)
	if ok {
		inferred := statusCode
		if inferred == 0 {
			inferred = statusFromText(raw)
		}
		return summarizeMarkup(markup, inferred)
	}
	if len(raw) > MaxProviderErrorChars {
		overflow := len(raw) - MaxProviderErrorChars
		return fmt.Sprintf("%s... [truncated %d chars]", raw[:MaxProviderErrorChars], overflow)
	}
	return raw
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// asHTTPStatus 校验值是合法 HTTP 状态码；非法返回 0（对应 Python None）。
// Python: provider_error.py:75-84 _as_http_status（bool 分支在 Go int 入参下不存在）
func asHTTPStatus(value int) int {
	if value >= 100 && value <= 599 {
		return value
	}
	return 0
}

// statusFromText 从文本推断状态码。
// Python: provider_error.py:87-92 _extract_status_from_text
func statusFromText(text string) int {
	m := statusInTextRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	code, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return asHTTPStatus(code)
}

// trimSpace 局部别名，避免 import strings 重复。
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
