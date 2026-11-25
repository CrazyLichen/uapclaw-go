// Package pythonsem 提供复刻 Python 动态类型语义的公共助手函数。
//
// 背景：Go 对齐 Python 上游时，大量校验/比较/错误消息依赖 Python 的动态行为——
// bool 先于 int 的类型判定、跨类型数值相等（1 == 1.0）、repr 的展示格式
// （True/None/单引号字符串/整值 float 带 .0）等。这些助手此前散落为各对齐包的
// 私有实现（member_optimizer/py_helpers.go、foundation/llm/routing/failures.go、
// intellirouter/router/reliable_router.go、improver_evolution/python_semantics.go），
// 且个别实现语义有偏差（如 %f 截断的 float repr 丢失精度）。本包以 Python 语义
// 正确的规范实现收口，供各对齐包统一引用。
//
// 文件目录：
//
//	pythonsem/
//	├── doc.go       # 包文档
//	├── helpers.go   # 全部 Python 语义助手
//	└── helpers_test.go
//
// 对应 Python 代码：无独立对应物（语言级语义适配层）。
package pythonsem

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsBool 判断值是否为布尔（Python isinstance(x, bool)）。
func IsBool(value any) bool {
	_, ok := value.(bool)
	return ok
}

// IsInt 判断值是否为整数（Python isinstance(x, int)：排除布尔与浮点）。
func IsInt(value any) bool {
	switch value.(type) {
	case int, int64:
		return true
	default:
		return false
	}
}

// IsNumber 判断值是否为有限数值（Python _is_number：排除布尔、含 int/float、须有限）。
func IsNumber(value any) bool {
	if IsBool(value) {
		return false
	}
	switch v := value.(type) {
	case int:
		return true
	case int64:
		return true
	case float64:
		return !isInfOrNaN(v)
	default:
		return false
	}
}

// PyInt 取整数值（调用方需先以 IsInt 保证类型；非整型返回 0）。
func PyInt(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	default:
		return 0
	}
}

// PyIntConvert 复刻 Python int(x) 宽转换（8.21 ToolComp int(code) 先例）：
// bool→0/1、int/int64 直取、float64 截断取整、十进制字符串解析；
// 其余类型（None/dict/list 等）转换失败 → (0, false)，≙ Python ValueError/TypeError。
func PyIntConvert(value any) (int64, bool) {
	switch v := value.(type) {
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// PyFloat 数值转 float64（非数值返回 NaN，调用方以 IsNumber/IsBool 先行判定）。
func PyFloat(value any) float64 {
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

// PyEqual 复刻 Python `==` 的数值跨类型相等语义（1 == 1.0 为 True；布尔不参与数值比较）。
func PyEqual(left, right any) bool {
	if IsNumber(left) && IsNumber(right) {
		return PyFloat(left) == PyFloat(right)
	}
	return reflect.DeepEqual(left, right)
}

// PyRepr 复刻 Python repr() 的展示格式：
// None / True / False / 单引号字符串 / 数值字面量 / 列表与字典的递归 repr。
func PyRepr(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case bool:
		if v {
			return "True"
		}
		return "False"
	case string:
		return "'" + v + "'"
	case int:
		return intRepr(int64(v))
	case int64:
		return intRepr(v)
	case float64:
		return PyFloatRepr(v)
	case []any:
		out := "["
		for i, item := range v {
			if i > 0 {
				out += ", "
			}
			out += PyRepr(item)
		}
		return out + "]"
	case []string:
		out := "["
		for i, item := range v {
			if i > 0 {
				out += ", "
			}
			out += "'" + item + "'"
		}
		return out + "]"
	case map[string]any:
		// Python dict repr：键带单引号、冒号后无空格、项间 ", "、按键排序展示
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := "{"
		for i, key := range keys {
			if i > 0 {
				out += ", "
			}
			out += "'" + key + "': " + PyRepr(v[key])
		}
		return out + "}"
	default:
		// Python str(value) 的近似：交由 fmt 默认格式化
		return fmt.Sprintf("%v", value)
	}
}

// PyStr 复刻 Python str() 的字符串化语义：str 原样（不加引号，与 PyRepr 的
// repr 语义区分）、bool True/False、None→None、数值十进制、容器回退 repr
// （Python str(list/dict) 即其 repr）。8.22 HTTP 组件占位符替换 str(value) 先例。
func PyStr(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case string:
		return v
	case bool:
		if v {
			return "True"
		}
		return "False"
	case int:
		return intRepr(int64(v))
	case int64:
		return intRepr(v)
	case float64:
		return PyFloatRepr(v)
	case []any, []string, map[string]any:
		return PyRepr(value)
	default:
		return fmt.Sprintf("%v", value)
	}
}

// PyFloatRepr 复刻 Python float repr：整值带 .0 后缀（2.0 → "2.0"），
// 其余用最短 round-trip 表示（Python repr 语义），如 0.1 → "0.1"、1e-12 → "1e-12"。
func PyFloatRepr(f float64) string {
	text := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(text, ".eE") && !isInfOrNaN(f) {
		return text + ".0"
	}
	return text
}

// PyDeepEqual 深度相等比较，数值跨类型相等（复刻 Python == 的递归语义），
// 用于 Go map[string]any 与 Python 黄金样例 JSON 的对齐断言。
func PyDeepEqual(left, right any) bool {
	if IsNumber(left) && IsNumber(right) {
		return PyFloat(left) == PyFloat(right)
	}
	if IsBool(left) != IsBool(right) {
		return false
	}
	switch l := left.(type) {
	case nil:
		return right == nil
	case string:
		r, ok := right.(string)
		return ok && l == r
	case bool:
		r, ok := right.(bool)
		return ok && l == r
	case map[string]any:
		return deepEqualMap(reflect.ValueOf(l), reflect.ValueOf(right))
	}
	lv, rv := reflect.ValueOf(left), reflect.ValueOf(right)
	if lv.Kind() == reflect.Map && rv.Kind() == reflect.Map {
		return deepEqualMap(lv, rv)
	}
	if lv.Kind() != reflect.Slice && lv.Kind() != reflect.Array {
		return reflect.DeepEqual(left, right)
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return false
	}
	if lv.Len() != rv.Len() {
		return false
	}
	for i := 0; i < lv.Len(); i++ {
		if !PyDeepEqual(lv.Index(i).Interface(), rv.Index(i).Interface()) {
			return false
		}
	}
	return true
}

// MapValue 对应 Python dict.get(key)：缺键与值为 nil 同样返回 nil。
func MapValue(mapping map[string]any, key string) any {
	return mapping[key]
}

// ValidateAndConvertType 按 Python 期望类型校验并转换值（Python questioner_comp.py
// validate_and_convert_type :309-377 一比一；结构偏差 D-24-2：Python 定义在组件内，
// 2026-11-24 拍板②迁 pythonsem 共享，8.26 等后续抽取类组件复用）。返回 (转换值,
// 是否有效)；失败 (nil, false)，≙ Python (None, False)。
func ValidateAndConvertType(value any, expectedType string) (any, bool) {
	if value == nil {
		return nil, false
	}
	switch expectedType {
	case "string":
		// String type: always convert to str(value)
		return PyStr(value), true
	case "integer":
		// Integer type: bool 先于 int 排除（True=1/False=0 陷阱）；float 仅整值
		// 接受；str 尝试 int(cleaned)
		if IsBool(value) {
			return nil, false
		}
		if IsInt(value) {
			return PyInt(value), true
		}
		if f, ok := value.(float64); ok {
			if f == float64(int64(f)) {
				return int64(f), true
			}
			return nil, false
		}
		if s, ok := value.(string); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil {
				return nil, false
			}
			return n, true
		}
		return nil, false
	case "number":
		// Number type: 转 float(value)；bool 排除；str 尝试 float(cleaned)
		if IsBool(value) {
			return nil, false
		}
		if IsNumber(value) {
			return PyFloat(value), true
		}
		if s, ok := value.(string); ok {
			f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return nil, false
			}
			return f, true
		}
		return nil, false
	case "boolean":
		// Boolean type: 仅接受 bool 或 "true"/"false"（忽略大小写与首尾空白）
		if b, ok := value.(bool); ok {
			return b, true
		}
		if s, ok := value.(string); ok {
			switch strings.ToLower(strings.TrimSpace(s)) {
			case "true":
				return true, true
			case "false":
				return false, true
			}
			return nil, false
		}
		return nil, false
	default:
		// Unknown type: treat as string (backward compatible)
		return PyStr(value), true
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// intRepr 整数十进制字面量
func intRepr(v int64) string {
	return strconv.FormatInt(v, 10)
}

// isInfOrNaN 判断浮点是否为无穷或 NaN
func isInfOrNaN(f float64) bool {
	return math.IsInf(f, 0) || math.IsNaN(f)
}

// deepEqualMap 泛化 map 深度相等（键为可比较类型，值递归 PyDeepEqual）
func deepEqualMap(lv, rv reflect.Value) bool {
	if lv.Len() != rv.Len() {
		return false
	}
	for _, key := range lv.MapKeys() {
		rvv := rv.MapIndex(key)
		if !rvv.IsValid() {
			return false
		}
		if !PyDeepEqual(lv.MapIndex(key).Interface(), rvv.Interface()) {
			return false
		}
	}
	return true
}
