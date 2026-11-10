// map_value.go 提供 map[string]any 的类型安全取值助手。
// 归属、会话元数据这类载荷在 Python 侧是 dict，Go 侧取值必须先断言类型；
// 这里统一收口，避免各包各写一份（rail / agents 曾各有一套同构实现）。
// Python 侧无对应模块：Python 直接 dict.get，Go 用本文件抵消类型系统差异。

package utils

// ──────────────────────────── 导出函数 ────────────────────────────

// MapValue 按精确类型读取 map 中的值。
// 键不存在或类型不符时返回零值与 false（等价 Python 的 dict.get + isinstance 守卫）。
func MapValue[T any](m map[string]any, key string) (T, bool) {
	var zero T
	if m == nil {
		return zero, false
	}
	v, ok := m[key].(T)
	if !ok {
		return zero, false
	}
	return v, true
}

// MapString 读取字符串值，缺失或类型不符返回空串。
func MapString(m map[string]any, key string) string {
	v, _ := MapValue[string](m, key)
	return v
}

// MapInt 读取整数值，兼容 int / int64 / float64（JSON 往返形态）。
// 缺失或类型不符返回 0。
func MapInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// MapStringSlice 读取字符串列表，兼容 []string 与 []any（JSON 往返形态）。
// 缺失或类型不符返回 nil。
func MapStringSlice(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	switch v := m[key].(type) {
	case []string:
		return append([]string{}, v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// FirstNonEmpty 返回第一个非空串，全空返回空串。
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
