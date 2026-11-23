// Package configpanel 数值强转助手（27.4）。
// 对齐 Python coerce_config_panel_value 内联的 int/float 转换语义。
package configpanel

import (
	"fmt"
	"strings"
)

// coerceInt 任意值转 int（Python int(value) 宽容语义；失败回 false）。
func coerceInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err == nil {
			return n, true
		}
		// Python int("3.5") 失败；但 int("3") 成功。浮点字符串按失败处理。
		return 0, false
	}
	// Python int("abc") 抛 ValueError → 失败
	n, ok := coerceFloat(value)
	if !ok {
		return 0, false
	}
	return int(n), true
}

// coerceFloat 任意值转 float64。
func coerceFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%g", &f); err == nil {
			return f, true
		}
		return 0, false
	}
	return 0, false
}

// maxInt 取较大整数。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// maxFloat 取较大浮点。
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
