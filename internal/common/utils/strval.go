package utils

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常数 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// StrVal 将任意值转为字符串。
// 对齐 Python: str(value) — nil 返回空串，string 返回原值，[]byte 转字符串，其他用 fmt.Sprintf("%v")。
func StrVal(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// StrValFromMap 从 map 中按键提取字符串值。
// key 不存在或值为 nil 时返回空串，string 直接返回，其他类型用 fmt.Sprintf("%v") 转换。
func StrValFromMap(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return StrVal(v)
}

// StrValDefault 将任意值转为字符串，值为 nil 或空串时返回默认值。
func StrValDefault(v any, defaultVal string) string {
	s := StrVal(v)
	if s == "" {
		return defaultVal
	}
	return s
}

// ──────────────────────────── 非导出函数 ────────────────────────────
