// 本文件是 improver_evolution 包内两个对齐文件共用的 Python 语义适配助手，
// 无独立 Python 对应物（Python 的动态类型行为内嵌于两源文件中）：
// 布尔排除、int/float 二分、跨类型数值相等、repr 错误消息格式。
package improver_evolution

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// errNew 构造对齐 Python ValueError 语义的错误
func errNew(msg string) error {
	return errors.New(msg)
}

// isBool 判断值是否为布尔（Python isinstance(x, bool)）
func isBool(value any) bool {
	_, ok := value.(bool)
	return ok
}

// isInt 判断值是否为整数（Python isinstance(x, int)，排除布尔与浮点）
func isInt(value any) bool {
	switch value.(type) {
	case int, int64:
		return true
	default:
		return false
	}
}

// pyInt 取整数值（调用方需先以 isInt 保证类型）
func pyInt(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	default:
		return 0
	}
}

// pyEqual 复刻 Python `==` 的数值跨类型相等语义（1 == 1.0 为 True；布尔不参与数值比较）
func pyEqual(left, right any) bool {
	if isNumber(left) && isNumber(right) {
		return pyFloat(left) == pyFloat(right)
	}
	return reflect.DeepEqual(left, right)
}

// pyFloat 数值转 float64
func pyFloat(value any) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	default:
		return math.NaN()
	}
}

// pyRepr 复刻 Python repr() 的错误消息展示格式（'字符串' / True / None / 数值字面量）
func pyRepr(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case bool:
		if v {
			return "True"
		}
		return "False"
	case string:
		return fmt.Sprintf("'%s'", v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return pyFloatRepr(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// pyFloatRepr 复刻 Python float repr：整值带 .0 后缀（2.0 → "2.0"），其余 %g
func pyFloatRepr(f float64) string {
	text := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(text, ".eE") && !math.IsInf(f, 0) && !math.IsNaN(f) {
		return text + ".0"
	}
	return text
}

// mapValue 对应 Python dict.get(key)（缺键与 None 同为 nil）
func mapValue(mapping map[string]any, key string) any {
	return mapping[key]
}
