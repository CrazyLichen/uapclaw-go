package utils

// ──────────────────────────── 导出函数 ────────────────────────────

// IsRefPath 判断是否为引用路径，如 "${start123.p2}"
func IsRefPath(path string) bool {
	return len(path) > 3 && len(path) <= RegexMaxLength &&
		path[:2] == "${" && path[len(path)-1] == '}'
}

// ExtractOriginKey 从引用路径中提取原始 key
// 例: "${start123.p2}" → "start123.p2"
func ExtractOriginKey(key string) string {
	if !ContainsChar(key, "$") {
		return key
	}
	start := -1
	for i := 0; i < len(key) && i < RegexMaxLength; i++ {
		if i+1 < len(key) && key[i] == '$' && key[i+1] == '{' {
			start = i + 2
			break
		}
	}
	if start == -1 {
		return key
	}
	for i := start; i < len(key); i++ {
		if key[i] == '}' {
			return key[start:i]
		}
	}
	return key
}
