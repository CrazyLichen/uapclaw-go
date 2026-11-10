package harness_protocol

import (
	"fmt"
	"math"
	"reflect"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FreezeJsonValue 校验并递归冻结一个 JSON 兼容值。
// 返回值不再与入参共享可变容器，调用方修改入参不影响结果。
// Python: models.py:30-48 freeze_json_value（5 个分支顺序一致）
func FreezeJsonValue(value any) (any, error) {
	// 分支 1：None（Go 的 nil）/ string / bool / 各类整数 原样返回
	// Python: models.py:33-34
	if value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return v, nil
	case float64:
		// 分支 2：浮点必须有限
		// Python: models.py:35-38
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("JSON numbers must be finite")
		}
		return v, nil
	case float32:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("JSON numbers must be finite")
		}
		return v, nil
	}

	// 分支 3：切片/数组递归（list 与 tuple 归一到同一 Go 路径）
	// Python: models.py:39-40
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			item, err := FreezeJsonValue(rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			out[i] = item
		}
		return out, nil
	}

	// 分支 4：映射递归，键必须是 string
	// Python: models.py:41-47
	if rv.Kind() == reflect.Map {
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("JSON object keys must be strings")
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			item, err := FreezeJsonValue(iter.Value().Interface())
			if err != nil {
				return nil, err
			}
			out[key] = item
		}
		return out, nil
	}

	// 分支 5：其余类型拒绝
	// Python: models.py:48
	return nil, fmt.Errorf("value of type %T is not JSON-compatible", value)
}

// FreezeJsonObject 校验、复制并递归冻结一个 JSON 对象。
// Python: models.py:51-57 freeze_json_object
func FreezeJsonObject(value any) (map[string]any, error) {
	frozen, err := FreezeJsonValue(value)
	if err != nil {
		return nil, err
	}
	if frozen == nil {
		// Python: models.py:56（isinstance(frozen, Mapping) 为 False → TypeError）
		return nil, fmt.Errorf("JSON object must be a mapping")
	}
	obj, ok := frozen.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("JSON object must be a mapping")
	}
	return obj, nil
}

// JsonValueToBuiltin 把冻结后的协议 JSON 值转回标准 JSON 容器。
// Python: models.py:60-67 json_value_to_builtin
func JsonValueToBuiltin(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = JsonValueToBuiltin(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = JsonValueToBuiltin(item)
		}
		return out
	}
	return value
}
