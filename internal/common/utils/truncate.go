package utils

import (
	"strings"
	"unicode/utf8"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TruncateRunes 按字符（rune）截断字符串到最多 maxLen 个字符。
//
// 为什么不用 s[:maxLen]：Go 的字节切片会切断多字节字符，中文被截在第 maxLen
// 字节时会产出半个 UTF-8 序列（乱码）。本仓库已多次出现同类缺陷
// （agent_teams/evolving/task_loop/viking 各有一份等价实现），新代码统一走这里。
//
// maxLen <= 0 返回空串；不足 maxLen 个字符时原样返回（不补省略号）。
func TruncateRunes(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	return string([]rune(s)[:maxLen])
}

// TruncateRunesEllipsis 截断到 maxRune 个字符并在末尾补 "..."（总长不超 maxRune）。
//
// 原为 evolving/store_projection 的私有实现，统一收敛到此处：
// maxRune <= 3 时退化为纯截断（无空间放省略号），否则保留 maxRune-3 个字符
// 并去掉尾部空格后接 "..."，避免出现 "词 ..." 这类带空格省略号。
func TruncateRunesEllipsis(s string, maxRune int) string {
	if maxRune <= 3 {
		return TruncateRunes(s, maxRune)
	}
	if utf8.RuneCountInString(s) <= maxRune {
		return s
	}
	return strings.TrimRight(TruncateRunes(s, maxRune-3), " ") + "..."
}
