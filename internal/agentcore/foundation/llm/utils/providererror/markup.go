package providererror

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// markupRe 错误页 markup 开头判定。
// Python: provider_error.py:19 _MARKUP_RE
var markupRe = regexp.MustCompile(`(?i)<!doctype\s+html|<html(?:\s|>)|<\?xml`)

// titleRe / h1Re / tagRe / headingStatusRe。
// Python: provider_error.py:20-22, 26-27
var (
	titleRe         = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	h1Re            = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	tagRe           = regexp.MustCompile(`<[^>]+>`)
	headingStatusRe = regexp.MustCompile(`\b([1-5]\d{2})\b`)
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// markupPayload 判定文本是否为错误页 markup，返回从标记起点起的子串。
// Python: provider_error.py:94-105 _markup_payload
func markupPayload(text string) (string, bool) {
	loc := markupRe.FindStringIndex(text)
	if loc == nil {
		return "", false
	}
	start := loc[0]
	if start > 160 {
		return "", false
	}
	markup := text[start:]
	if !isErrorPage(markup) {
		return "", false
	}
	return markup, true
}

// isErrorPage 判定 markup 是否确为错误页（而非恰好含标签的 JSON 片段）。
// Python: provider_error.py:107-117 _is_error_page
func isErrorPage(markup string) bool {
	sample := strings.ToLower(trimSpace(markup))
	if len(sample) > 8000 {
		sample = sample[:8000]
	}
	if strings.HasPrefix(sample, "<?xml") {
		return strings.Contains(sample, "<html") || strings.Contains(sample, "<fault") || len(markup) >= 512
	}
	if !strings.HasPrefix(sample, "<!doctype html") && !strings.HasPrefix(sample, "<html") {
		return false
	}
	for _, token := range []string{"<head", "<body", "</html>", "<title", "<h1"} {
		if strings.Contains(sample, token) {
			return true
		}
	}
	return len(markup) >= 256
}

// summarizeMarkup 把错误页折叠为一行摘要。
// Python: provider_error.py:119-134 _summarize_markup
func summarizeMarkup(markup string, statusCode int) string {
	heading := extractHeading(markup)
	inferred := statusCode
	if inferred == 0 {
		inferred = statusFromHeading(heading)
	}
	kind := "XML"
	if looksLikeHTML(markup) {
		kind = "HTML"
	}
	page := fmt.Sprintf("%s error page", kind)
	if heading != "" {
		page = fmt.Sprintf("%s error page %q", kind, heading)
	}
	parts := make([]string, 0, 2)
	if inferred != 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", inferred))
	}
	parts = append(parts, fmt.Sprintf("%s (%d chars)", page, len(markup)))
	return fmt.Sprintf("%s; response body is an HTML/XML document, not an API error payload",
		strings.Join(parts, ", "))
}

// looksLikeHTML 判断 markup 是否为 HTML（而非 XML）。
// Python: provider_error.py:136-139 _looks_like_html
func looksLikeHTML(markup string) bool {
	sample := strings.ToLower(trimSpace(markup))
	if len(sample) > 64 {
		sample = sample[:64]
	}
	return strings.HasPrefix(sample, "<!doctype html") || strings.HasPrefix(sample, "<html")
}

// extractHeading 提取首个 title / h1 文本（截断 120 字符）。
// Python: provider_error.py:141-150 _extract_heading
func extractHeading(markup string) string {
	for _, pattern := range []*regexp.Regexp{titleRe, h1Re} {
		m := pattern.FindStringSubmatch(markup)
		if m == nil {
			continue
		}
		heading := plainText(m[1])
		if heading != "" {
			if len([]rune(heading)) > 120 {
				heading = string([]rune(heading)[:120])
			}
			return heading
		}
	}
	return ""
}

// statusFromHeading 从 heading（如 "502 Bad Gateway"）推断状态码。
// Python: provider_error.py:152-159 _status_from_heading
func statusFromHeading(heading string) int {
	if heading == "" {
		return 0
	}
	m := headingStatusRe.FindStringSubmatch(heading)
	if m == nil {
		return 0
	}
	code, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return asHTTPStatus(code)
}

// plainText 剥标签 + HTML 反转义 + 压空白成单行 + 双引号换单引号。
// Python: provider_error.py:161-163 _plain_text
func plainText(value string) string {
	text := tagRe.ReplaceAllString(value, " ")
	text = html.UnescapeString(text)
	return strings.ReplaceAll(strings.Join(strings.Fields(text), " "), `"`, "'")
}
