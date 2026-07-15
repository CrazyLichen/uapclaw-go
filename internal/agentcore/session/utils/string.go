package utils

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ContainsChar 检查字符串是否包含指定字符/子串
func ContainsChar(s, substr string) bool {
	return len(s) >= len(substr) && ContainsSubstring(s, substr)
}

// ContainsSubstring 检查字符串是否包含子串
func ContainsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// SplitString 按分隔符拆分字符串
func SplitString(s, sep string) []string {
	if sep == "" {
		return []string{s}
	}
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

// ParseListIndexes 解析包含数组索引的部分
// 例: "c[1]" → ["c", 1], "[1]" → [1], "a[-1]" → ["a", -1]
func ParseListIndexes(part string) []any {
	var result []any
	bracketIdx := -1
	for i := 0; i < len(part); i++ {
		if part[i] == '[' {
			bracketIdx = i
			break
		}
	}
	if bracketIdx == -1 {
		return []any{part}
	}

	base := part[:bracketIdx]
	if base != "" {
		result = append(result, base)
	}

	remaining := part[bracketIdx:]
	for len(remaining) > 0 {
		if remaining[0] != '[' {
			break
		}
		end := -1
		for i := 1; i < len(remaining); i++ {
			if remaining[i] == ']' {
				end = i
				break
			}
		}
		if end == -1 {
			break
		}
		indexStr := remaining[1:end]
		var idx int
		isNeg := false
		parseStart := 0
		if len(indexStr) > 0 && indexStr[0] == '-' {
			isNeg = true
			parseStart = 1
		}
		isInt := true
		if parseStart >= len(indexStr) {
			isInt = false
		} else {
			parsed := 0
			for i := parseStart; i < len(indexStr); i++ {
				if indexStr[i] < '0' || indexStr[i] > '9' {
					isInt = false
					break
				}
				parsed = parsed*10 + int(indexStr[i]-'0')
			}
			if isInt {
				if isNeg {
					idx = -parsed
				} else {
					idx = parsed
				}
			}
		}
		if isInt {
			result = append(result, idx)
		} else {
			result = append(result, indexStr)
		}
		remaining = remaining[end+1:]
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────
