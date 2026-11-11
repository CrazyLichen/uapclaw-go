package processcli

import (
	"strings"
	"unicode"
)

// 本文件是 channels/process_cli/ui.py 的显示宽度工具部分（P-4）。
//
// Python: channels/process_cli/ui.py:28-70（_display_width / _truncate / _wrap_display）
//
// Go 标准库没有 unicodedata.east_asian_width（偏差 E-3），此处按
// Unicode EastAsianWidth.txt 的 Wide / Fullwidth 主要区段自建判定。

// ──────────────────────────── 导出函数 ────────────────────────────

// displayWidth 显示宽度：East-Asian W/F 记 2，组合字符记 0，其余 1。
// Python: ui.py:28-35 _display_width
func displayWidth(value string) int {
	width := 0
	for _, r := range value {
		width += runeDisplayWidth(r)
	}
	return width
}

// truncate 按显示宽度截断，超宽加省略号。
// Python: ui.py:38-54 _truncate
func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(value) <= width {
		return value
	}
	marker := ""
	if width > 1 {
		marker = "…"
	}
	target := width - displayWidth(marker)
	var builder strings.Builder
	used := 0
	for _, r := range value {
		runeWidth := runeDisplayWidth(r)
		if used+runeWidth > target {
			break
		}
		builder.WriteRune(r)
		used += runeWidth
	}
	return builder.String() + marker
}

// wrapDisplay 按显示宽度折行。
// Python: ui.py:57-70 _wrap_display
func wrapDisplay(value string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	lines := make([]string, 0, 2)
	var current strings.Builder
	used := 0
	for _, r := range value {
		runeWidth := runeDisplayWidth(r)
		if current.Len() > 0 && used+runeWidth > width {
			lines = append(lines, current.String())
			current.Reset()
			used = 0
		}
		current.WriteRune(r)
		used += runeWidth
	}
	lines = append(lines, current.String())
	return lines
}

// shortSession 会话 ID 展示用短形式（超过 12 字符截断加省略号）。
// Python: ui.py:325-328 ProcessCliUI.short_session
func shortSession(sessionID string) string {
	if len(sessionID) <= 12 {
		return sessionID
	}
	return sessionID[:12] + "…"
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runeDisplayWidth 单字符显示宽度。
func runeDisplayWidth(r rune) int {
	if isCombiningRune(r) {
		return 0
	}
	if isEastAsianWide(r) {
		return 2
	}
	return 1
}

// isCombiningRune 是否组合字符（宽度 0）。
func isCombiningRune(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r)
}

// isEastAsianWide 是否 East-Asian Wide/Fullwidth（宽度 2）。
//
// 区段来自 Unicode EastAsianWidth.txt 的 W/F 主要块（偏差 E-3：Go 无
// unicodedata.east_asian_width 等价函数）。
func isEastAsianWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F: // 谚文字母
		return true
	case r >= 0x2E80 && r <= 0x303E: // CJK 部首 + 符号
		return true
	case r >= 0x3041 && r <= 0x33FF: // 平假名/片假名/注音/CJK 兼容
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK 扩展 A
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // CJK 统一表意
		return true
	case r >= 0xA000 && r <= 0xA4CF: // 彝文
		return true
	case r >= 0xA960 && r <= 0xA97F: // 谚文扩展 A
		return true
	case r >= 0xAC00 && r <= 0xD7A3: // 谚文音节
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK 兼容表意
		return true
	case r >= 0xFE10 && r <= 0xFE19: // 竖排形式
		return true
	case r >= 0xFE30 && r <= 0xFE6F: // CJK 兼容形式
		return true
	case r >= 0xFF01 && r <= 0xFF60: // 全角形式
		return true
	case r >= 0xFFE0 && r <= 0xFFE6: // 全角符号
		return true
	case r >= 0x1F300 && r <= 0x1F64F: // 表情
		return true
	case r >= 0x1F900 && r <= 0x1F9FF: // 补充表情
		return true
	case r >= 0x20000 && r <= 0x3FFFD: // CJK 扩展 B 及以上
		return true
	}
	return false
}
