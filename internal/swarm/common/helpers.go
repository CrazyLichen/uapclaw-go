// 本包共用的小工具函数。
// Python 侧这些是各文件内联的字面操作（str()/dict() 拷贝/or 短路），Go 侧集中实现。
package common

import "encoding/json"

// jsonNumber 类型别名：map[string]any 中 YAML/JSON 数字的可能形态。
type jsonNumber = json.Number

// asString 把 any 安全转字符串；nil 返回空串。
// 对齐 Python str(x or "") 的宽松语义（None/缺失 → ""）。
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	// Python str(123) 语义：非字符串标量转字符串仅用于宽松比较场景，
	// 本包调用点均为配置字段读取，非字符串一律按空处理并注释。
	return ""
}

// copyMap 浅拷贝 map；nil 返回空 map。
// Python: dict(x) 语义（reasoning_injector.py:65 _runtime_config_copy 等）。
func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// firstNonEmpty 返回第一个非空字符串。
// Python or 短路语义。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
