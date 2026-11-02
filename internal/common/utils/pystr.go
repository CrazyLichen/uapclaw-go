package utils

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// PyStr 对齐 Python 的 str()：
//   - None（nil）→ "None"（调用方需要空串时自己判 nil，Python 的
//     `"x" if v is not None else ""` 属于调用方语义）
//   - str → 原样（不加引号）
//   - bool → "True"/"False"
//   - 数字 → Python 记法（整数值浮点补 ".0"）
//   - dict/list/tuple → 走 PyRepr（Python 的 str(container) 等于 repr(container)）
//   - 其他 → fmt.Sprintf("%v", v)
//
// 用途：工具结果等「Python 侧会用 str(x) 落进文本」的位置。历史缺陷是直接
// fmt.Sprintf("%v", result)，Go 对 map 打印成 `map[k:v]`，前端与日志里全是
// `map[...]` 前缀，与 Python 的 `{'k': 'v'}` 完全不同。
func PyStr(v any) string {
	switch val := v.(type) {
	case nil:
		return "None"
	case string:
		return val
	case bool:
		return pyBoolStr(val)
	case map[string]any, []any:
		return PyRepr(val)
	default:
		if s, ok := pyNumberStr(v); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
}

// PyRepr 对齐 Python 的 repr()：字符串带引号、容器递归、None 输出 "None"。
//
// dict 的键按字典序遍历（Go map 无序，Python 按插入序）——输出稳定可比对，
// 但键序与 Python 的插入序不保证一致。
func PyRepr(v any) string {
	var b strings.Builder
	pyWriteRepr(&b, v)
	return b.String()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pyWriteRepr 递归写入 Python repr 形式。
func pyWriteRepr(b *strings.Builder, v any) {
	switch val := v.(type) {
	case nil:
		b.WriteString("None")
	case string:
		b.WriteString(pyStrRepr(val))
	case bool:
		b.WriteString(pyBoolStr(val))
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(pyStrRepr(k))
			b.WriteString(": ")
			pyWriteRepr(b, val[k])
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, item := range val {
			if i > 0 {
				b.WriteString(", ")
			}
			pyWriteRepr(b, item)
		}
		b.WriteByte(']')
	default:
		if s, ok := pyNumberStr(v); ok {
			b.WriteString(s)
			return
		}
		_, _ = fmt.Fprintf(b, "%v", v)
	}
}

// pyBoolStr 对齐 Python str(bool)。
func pyBoolStr(v bool) string {
	if v {
		return "True"
	}
	return "False"
}

// pyStrRepr 对齐 Python repr(str)：默认单引号，字符串内只有单引号时改用双引号。
func pyStrRepr(s string) string {
	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if byte(r) == quote && r < 0x80 {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// pyNumberStr 把 Go 数字类型转为 Python 记法文本；非数字类型返回 false。
//
// 浮点按 Python 的 f/e 记法切换（与 retrieval 侧 pyFloatStr 同源语义）：
// abs ∈ [1e-4, 1e16) 用 f 记法且整数值补 ".0"，其余 e 记法。
func pyNumberStr(v any) (string, bool) {
	switch n := v.(type) {
	case int:
		return strconv.Itoa(n), true
	case int8:
		return strconv.FormatInt(int64(n), 10), true
	case int16:
		return strconv.FormatInt(int64(n), 10), true
	case int32:
		return strconv.FormatInt(int64(n), 10), true
	case int64:
		return strconv.FormatInt(n, 10), true
	case uint:
		return strconv.FormatUint(uint64(n), 10), true
	case uint8:
		return strconv.FormatUint(uint64(n), 10), true
	case uint16:
		return strconv.FormatUint(uint64(n), 10), true
	case uint32:
		return strconv.FormatUint(uint64(n), 10), true
	case uint64:
		return strconv.FormatUint(n, 10), true
	case float32:
		return pyFloatStr(float64(n)), true
	case float64:
		return pyFloatStr(n), true
	default:
		return "", false
	}
}

// pyFloatStr 还原 Python str(float) 记法：整数值补 ".0"，nan/inf 用 Python 字面量。
func pyFloatStr(f float64) string {
	if math.IsNaN(f) {
		return "nan"
	}
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	abs := math.Abs(f)
	if abs == 0 || (abs >= 1e-4 && abs < 1e16) {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	}
	return strconv.FormatFloat(f, 'e', -1, 64)
}
