// 本包共用的小工具函数。
// Python 侧这些是各文件内联的字面操作（str()/dict() 拷贝/or 短路），Go 侧集中实现。
package common

import (
	"encoding/json"
	"fmt"
	"strings"
)

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

// parseFiniteFloat 解析十进制浮点（Python float(str) 语义）。
func parseFiniteFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%g", &f)
	if err != nil {
		return 0, err
	}
	return f, nil
}
