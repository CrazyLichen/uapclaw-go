package utils

import (
	"encoding/json"
	"fmt"
	"strconv"
)

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

// IntVal 将任意值转为 int。
// 对齐 Python: int(value) — nil 返回 0，数值类型直接转换，string 尝试 strconv.Atoi，其他返回 0。
func IntVal(v any) int {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case int8:
		return int(val)
	case int16:
		return int(val)
	case int32:
		return int(val)
	case int64:
		return int(val)
	case uint:
		return int(val)
	case uint8:
		return int(val)
	case uint16:
		return int(val)
	case uint32:
		return int(val)
	case uint64:
		return int(val)
	case float64:
		return int(val)
	case float32:
		return int(val)
	case string:
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
		return 0
	case json.Number:
		if n, err := val.Int64(); err == nil {
			return int(n)
		}
		return 0
	default:
		return 0
	}
}

// IntValDefault 将任意值转为 int，转换失败或为零值时返回默认值。
func IntValDefault(v any, defaultVal int) int {
	n := IntVal(v)
	if n == 0 && v != nil {
		// v 非 nil 但 IntVal 返回 0，可能是 string 解析失败
		if _, ok := v.(string); ok {
			return defaultVal
		}
	}
	if n == 0 {
		return defaultVal
	}
	return n
}

// ──────────────────────────── 非导出函数 ────────────────────────────
