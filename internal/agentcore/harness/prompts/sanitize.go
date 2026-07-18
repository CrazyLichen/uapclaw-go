package prompts

import "regexp"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// injectionPattern 提示词注入危险字符正则
	// 对应 Python: _INJECTION_PATTERN = re.compile(r"[<>\{\}\[\]`\$]|\.{3,}|\\n|\\r")
	// Go 中反引号无法出现在原始字符串内，使用解释型字符串
	injectionPattern = regexp.MustCompile(`[<>\{\}\[\]` + "`" + `\$]|\.{3,}|\\n|\\r`)
)

// SanitizePath 清洗用户可控的路径字符串。
//
// 移除可能用于提示词注入的特殊字符，保留正常路径分隔符。
//
// 对应 Python: sanitize_path(path)
// ──────────────────────────── 导出函数 ────────────────────────────

func SanitizePath(path string) string {
	return injectionPattern.ReplaceAllString(path, "")
}

// SanitizeUserContent 移除用户内容中的注入危险字符并截断长度。
//
// 对应 Python: sanitize_user_content(content, max_len)
func SanitizeUserContent(content string, maxLen int) string {
	safeText := injectionPattern.ReplaceAllString(content, "")
	if len(safeText) > maxLen {
		safeText = safeText[:maxLen]
	}
	return safeText
}
