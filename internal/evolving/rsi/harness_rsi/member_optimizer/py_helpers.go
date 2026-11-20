// py_helpers.go Python 语义辅助函数（f-string/str()/json.dumps 的 Go 等价）。
// 供 member_optimizer 各对齐文件复用；语义逐条注释对应 Python 表达式。

package memberoptimizer

import (
	"fmt"
	"github.com/uapclaw/uapclaw-go/internal/common/pythonsem"
)

// pyStr 等价 Python str(value)：None → ""、float 整数值保留 .0 语义按 %v。
// Python: f"{value}" / str(value)。
func pyStr(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

// pyStrOrEmpty 等价 Python raw.get("key", "")：非字符串按 str()。
func pyStrOrEmpty(value any) string {
	return pyStr(value)
}

// pyListStr 等价 Python f"{list}"：列表的 repr 形式（单引号）。
func pyListStr(value any) string {
	if value == nil {
		return "None"
	}
	switch v := value.(type) {
	case []string:
		if len(v) == 0 {
			return "[]"
		}
		out := "["
		for i, item := range v {
			if i > 0 {
				out += ", "
			}
			out += "'" + item + "'"
		}
		return out + "]"
	case []any:
		if len(v) == 0 {
			return "[]"
		}
		out := "["
		for i, item := range v {
			if i > 0 {
				out += ", "
			}
			out += pythonsem.PyRepr(item)
		}
		return out + "]"
	case []map[string]any:
		if len(v) == 0 {
			return "[]"
		}
		out := "["
		for i, item := range v {
			if i > 0 {
				out += ", "
			}
			out += pythonsem.PyRepr(item)
		}
		return out + "]"
	default:
		return pythonsem.PyRepr(value)
	}
}

// pyAnyList 断言 []any，nil 安全（Python `value or []`）。
func pyAnyList(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return nil
}

// pyOrNone 等价 Python：值为 nil 时输出 None（JSON/YAML null）。
func pyOrNone(value any) any {
	if value == nil {
		return nil
	}
	return value
}

// pyOrNoneList 空列表序列化为 [] 而非 null（Python 空列表字段默认 []）。
func pyOrNoneList(list []map[string]any) any {
	if list == nil {
		return []any{}
	}
	return list
}

// pyOrNoneTraceRefs 空轨迹引用序列化为 []。
func pyOrNoneTraceRefs(list []map[string]string) any {
	if list == nil {
		return []any{}
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		converted := map[string]any{}
		for key, value := range item {
			converted[key] = value
		}
		out = append(out, converted)
	}
	return out
}

// pyEvidenceFirst5 等价 Python issue.evidence[:5]。
func pyEvidenceFirst5(evidence []map[string]any) []any {
	out := make([]any, 0, len(evidence))
	for _, item := range evidence {
		out = append(out, item)
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// pyFloatOrZero 等价 Python float(raw.get("confidence", 0.0))。
func pyFloatOrZero(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		var f float64
		_, err := fmt.Sscanf(v, "%g", &f)
		if err != nil {
			return 0.0
		}
		return f
	default:
		return 0.0
	}
}

// pyMin 整数下界。
func pyMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// pyMinFloat 浮点下界。
func pyMinFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// pyMaxFloat 浮点上界。
func pyMaxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// pyStrList []string 转 any 列表（nil → []）。
func pyStrList(list []string) any {
	out := make([]any, 0, len(list))
	for _, item := range list {
		out = append(out, item)
	}
	return out
}
