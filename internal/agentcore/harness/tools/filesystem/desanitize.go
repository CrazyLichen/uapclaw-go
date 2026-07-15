package filesystem

import (
	"html"
	"strings"
	"unicode"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// desanitizations 压缩标记 → 原始标记映射表。
// 对齐 Python: EditFileTool._DESANITIZATIONS (filesystem.py L1021-1040)
var desanitizations = map[string]string{
	"<fnr>":          "<function_results>",
	"<n>":            "<name>",
	"</n>":           "</name>",
	"<o>":            "<output>",
	"</o>":           "</output>",
	"<e>":            "<error>",
	"</e>":           "</error>",
	"<s>":            "<system>",
	"</s>":           "</system>",
	"<r>":            "<result>",
	"</r>":           "</result>",
	"< META_START >": "<META_START>",
	"< META_END >":   "<META_END>",
	"< EOT >":        "<EOT>",
	"< META >":       "<META>",
	"< SOS >":        "<SOS>",
	"\n\nH:":         "\n\nHuman:",
	"\n\nA:":         "\n\nAssistant:",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Desanitize 反转 HTML 实体编码 + Claude 压缩的 XML 标记。
// 对齐 Python: EditFileTool._desanitize (filesystem.py L1050-1055)
func Desanitize(value string) string {
	result := html.UnescapeString(value)
	for source, target := range desanitizations {
		result = strings.ReplaceAll(result, source, target)
	}
	return result
}

// NormalizeQuotes 将所有弯引号（curly quotes）转换为直引号。
// 对齐 Python: EditFileTool._normalize_quotes (filesystem.py L1094-1101)
func NormalizeQuotes(s string) string {
	return strings.NewReplacer(
		"\u2018", "'",
		"\u2019", "'",
		"\u201c", `"`,
		"\u201d", `"`,
	).Replace(s)
}

// ApplyCurlyDoubleQuotes 上下文感知弯双引号替换。
// 对齐 Python: EditFileTool._apply_curly_double_quotes (filesystem.py L1109-1118)
func ApplyCurlyDoubleQuotes(s string) string {
	chars := []rune(s)
	var result []rune
	for i, char := range chars {
		if char == '"' {
			if isOpeningQuoteContext(chars, i) {
				result = append(result, '\u201c') // 左弯双引号
			} else {
				result = append(result, '\u201d') // 右弯双引号
			}
		} else {
			result = append(result, char)
		}
	}
	return string(result)
}

// ApplyCurlySingleQuotes 上下文感知弯单引号替换。
// 对齐 Python: EditFileTool._apply_curly_single_quotes (filesystem.py L1120-1134)
func ApplyCurlySingleQuotes(s string) string {
	chars := []rune(s)
	var result []rune
	for i, char := range chars {
		if char != '\'' {
			result = append(result, char)
			continue
		}
		var prevChar, nextChar rune
		if i > 0 {
			prevChar = chars[i-1]
		}
		if i < len(chars)-1 {
			nextChar = chars[i+1]
		}
		if unicode.IsLetter(prevChar) && unicode.IsLetter(nextChar) {
			result = append(result, '\u2019') // 右弯单引号（撇号）
		} else if isOpeningQuoteContext(chars, i) {
			result = append(result, '\u2018') // 左弯单引号
		} else {
			result = append(result, '\u2019') // 右弯单引号
		}
	}
	return string(result)
}

// PreserveQuoteStyle 如果匹配的 actualOldStr 含弯引号，对 newStr 也应用弯引号转换。
// 对齐 Python: EditFileTool._preserve_quote_style (filesystem.py L1136-1146)
func PreserveQuoteStyle(oldStr, actualOldStr, newStr string) string {
	if oldStr == actualOldStr {
		return newStr
	}

	result := newStr
	if strings.ContainsRune(actualOldStr, '\u201c') || strings.ContainsRune(actualOldStr, '\u201d') {
		result = ApplyCurlyDoubleQuotes(result)
	}
	if strings.ContainsRune(actualOldStr, '\u2018') || strings.ContainsRune(actualOldStr, '\u2019') {
		result = ApplyCurlySingleQuotes(result)
	}
	return result
}

// TryQuoteVariants 尝试引号变体匹配。
// 对齐 Python: EditFileTool._try_quote_variants (filesystem.py L1148-1155)
// 返回 (matchedOriginalString, found)
func TryQuoteVariants(oldStr, content string) (string, bool) {
	normalizedContent := NormalizeQuotes(content)
	normalizedOld := NormalizeQuotes(oldStr)
	index := strings.Index(normalizedContent, normalizedOld)
	if index == -1 {
		return "", false
	}
	// 在原始 content 中取与 oldStr 等长的子串
	runeContent := []rune(content)
	runeStart := len([]rune(content[:index]))
	runeEnd := runeStart + len([]rune(oldStr))
	if runeEnd > len(runeContent) {
		runeEnd = len(runeContent)
	}
	return string(runeContent[runeStart:runeEnd]), true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isOpeningQuoteContext 判断给定位置的引号是否处于开引号上下文。
// 对齐 Python: EditFileTool._is_opening_quote_context (filesystem.py L1104-1107)
func isOpeningQuoteContext(chars []rune, index int) bool {
	if index == 0 {
		return true
	}
	prev := chars[index-1]
	switch prev {
	case ' ', '\t', '\n', '\r', '(', '[', '{', '\u2014', '\u2013':
		return true
	}
	return false
}
