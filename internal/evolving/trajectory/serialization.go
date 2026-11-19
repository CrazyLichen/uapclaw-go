package trajectory

import (
	"encoding/json"
	"fmt"
	"sort"
)

// serialization.go 轨迹 payload 的 JSON 兼容值转换。
// 对齐 Python: agent_evolving/trajectory/serialization.py（全文）。
// Python 的 dataclass/model_dump/to_dict 回退链在 Go 侧收窄为 json.Marshal
// 回退：任意未知类型先走标准 JSON 编码，失败回落 fmt.Sprint 字符串化
//（Python 末级同样是 str(value)，语义一致）。

// toJsonCompatible 返回 value 的脱离式 JSON 兼容表示。
// Python: serialization.py to_json_compatible。
func toJsonCompatible(value any) any {
	switch v := value.(type) {
	case nil, string, bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, float32, float64:
		return v
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[key] = toJsonCompatible(item)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = toJsonCompatible(item)
		}
		return result
	case json.Marshaler:
		// 具备 MarshalJSON 的类型（如 otel attribute.Value）走标准编码。
		data, err := json.Marshal(v)
		if err == nil {
			var decoded any
			if json.Unmarshal(data, &decoded) == nil {
				return toJsonCompatible(decoded)
			}
		}
		return fmt.Sprint(v)
	default:
		// 未知结构体：标准 JSON 编码（对齐 Python dataclass/model_dump 回退）。
		data, err := json.Marshal(v)
		if err == nil {
			var decoded any
			if json.Unmarshal(data, &decoded) == nil {
				return toJsonCompatible(decoded)
			}
		}
		return fmt.Sprint(v)
	}
}

// sortedKeys 返回 map 键的确定性排序（供 attributes_from_map 的 sorted 语义）。
// Python: sorted(attributes)。
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// jsonEncodeString 紧凑 JSON 编码（ensure_ascii=False 语义：Go 原生不转义
// 非 ASCII），供消息属性写入。
// Python: json.dumps(value, ensure_ascii=False, default=str)。
func jsonEncodeString(value any) string {
	compatible := toJsonCompatible(value)
	data, err := json.Marshal(compatible)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}
