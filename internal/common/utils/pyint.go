package utils

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// maxIntValue int 能表示的最大值（供 uint64 越界判定）。
const maxIntValue = int(^uint(0) >> 1)

// ──────────────────────────── 导出函数 ────────────────────────────

// PyInt 对齐 Python int() 的数值转换，用于解析持久化状态与外部参数里的任意值。
//
// 规则（逐条对应 CPython 行为）：
//   - 整数族与 float → 向零截断（int(-3.9) == -3）
//   - bool → 1 / 0（Python bool 是 int 子类，int(True) == 1）
//   - 字符串 → 仅接受整数文本（TrimSpace 后 Atoi，允许正负号）；
//     "3.0" 必须报错，对齐 int("3.0") 的 ValueError
//   - json.Number → 按"JSON 数字"处理：整数字面走 ParseInt；
//     小数字面在 Python 里是 float，故 Float64 后截断（int(3.0) == 3）；
//     超界整数字面报错（Go 装不下，不能退化成 float 丢精度）
//   - 其余类型 → error
func PyInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int8:
		return int(n), nil
	case int16:
		return int(n), nil
	case int32:
		return int(n), nil
	case int64:
		return int(n), nil
	case uint:
		return int(n), nil
	case uint8:
		return int(n), nil
	case uint16:
		return int(n), nil
	case uint32:
		return int(n), nil
	case uint64:
		if n > uint64(maxIntValue) {
			return 0, fmt.Errorf("not an int: %d out of range", n)
		}
		return int(n), nil
	case float32:
		return int(n), nil
	case float64:
		return int(n), nil
	case bool:
		// Python: int(True) == 1
		if n {
			return 1, nil
		}
		return 0, nil
	case json.Number:
		return pyIntFromJSONNumber(n)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0, fmt.Errorf("not an int: %q", n)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("not an int: %T", v)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pyIntFromJSONNumber 还原 json.Number 的 Python int() 语义。
func pyIntFromJSONNumber(n json.Number) (int, error) {
	literal := n.String()
	if parsed, err := strconv.ParseInt(literal, 10, 64); err == nil {
		return int(parsed), nil
	}
	// 超界整数字面量：Python 是无界 int，Go 装不下 → 报错而不是退化成 float。
	if IsPureIntegerLiteral(literal) {
		return 0, fmt.Errorf("not an int: %s out of range", literal)
	}
	asFloat, err := n.Float64()
	if err != nil {
		return 0, fmt.Errorf("not an int: %q", literal)
	}
	return int(asFloat), nil
}
