// utils 包提供通用工具函数。
//
// dict.go 实现字典操作工具和 JSON Schema 参数校验。
// 对应 Python：
//   - openjiuwen/core/common/utils/dict_utils.py（嵌套字典操作）
//   - openjiuwen/core/common/utils/schema_utils.py → remove_none_values（零值清理）
//   - uapclaw-main/pkg/tools/validate.go → validateToolArgs（参数校验）

package utils

import (
	"fmt"
	"math"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LeafNode 叶子节点，表示嵌套结构中的一个末端值及其路径。
type LeafNode struct {
	Path  []string // 从根到叶子的路径（列表索引格式为 "[0]"）
	Value any      // 叶子节点的值
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateNestedDict 从点分路径创建嵌套字典。
//
// 对应 Python: create_nested_dict(path, value, separator)
// 将点分路径拆分为多层键，在叶子位置放置 value。
// 空路径时直接返回 value。
//
// 示例：
//
//	CreateNestedDict("a.b", 1) → {"a": {"b": 1}}
func CreateNestedDict(path string, value any, separator ...string) map[string]any {
	sep := "."
	if len(separator) > 0 {
		sep = separator[0]
	}
	if path == "" {
		// 空路径，返回包含 value 的单层 map 不合理，
		// 与 Python 行为一致：直接返回 value（但 Go 返回类型是 map[string]any，
		// 这里返回 nil，由调用方处理）
		return nil
	}

	keys := strings.Split(path, sep)
	result := map[string]any{}
	current := result

	for i, key := range keys {
		if i == len(keys)-1 {
			current[key] = value
		} else {
			current[key] = map[string]any{}
			current = current[key].(map[string]any)
		}
	}

	return result
}

// FlattenDict 将嵌套字典展平为点分路径键值对。
//
// 对应 Python: flatten_dict(data)
// 内部调用 ExtractLeafNodes 提取所有叶子节点，
// 再将路径列表格式化为点分字符串键。
func FlattenDict(data map[string]any) map[string]any {
	nodes := ExtractLeafNodes(data)
	result := make(map[string]any, len(nodes))
	for _, node := range nodes {
		result[formatPath(node.Path)] = node.Value
	}
	return result
}

// ExtractLeafNodes 提取嵌套结构中的所有叶子节点。
//
// 对应 Python: extract_leaf_nodes(data, current_path)
// 递归遍历 map 和 slice，收集所有非集合类型的末端值。
// 列表索引格式化为 "[0]"、"[1]" 等。
//
// 示例：
//
//	ExtractLeafNodes({"a": [1, {"b": 2}]})
//	→ [{Path: ["a", "[0]"], Value: 1}, {Path: ["a", "[1]", "b"], Value: 2}]
func ExtractLeafNodes(data any, currentPath ...string) []LeafNode {
	if data == nil {
		return nil
	}

	path := []string{}
	if len(currentPath) > 0 {
		path = currentPath
	}

	var results []LeafNode

	switch v := data.(type) {
	case map[string]any:
		for key, val := range v {
			newPath := make([]string, len(path), len(path)+1)
			copy(newPath, path)
			newPath = append(newPath, key)
			results = append(results, ExtractLeafNodes(val, newPath...)...)
		}
	case []any:
		for i, item := range v {
			newPath := make([]string, len(path), len(path)+1)
			copy(newPath, path)
			newPath = append(newPath, fmt.Sprintf("[%d]", i))
			results = append(results, ExtractLeafNodes(item, newPath...)...)
		}
	default:
		results = append(results, LeafNode{Path: path, Value: data})
	}

	return results
}

// RebuildDict 从叶子节点列表重建嵌套字典。
//
// 对应 Python: rebuild_dict(path_value_pairs)
// 支持列表索引路径元素（如 "[0]"），遇到索引时创建 slice。
// 简化版路径重建，不处理 list 元素的 dict.go 复杂情况，
// 对于纯 dict 路径直接调用 rebuildDictFromPaths。
func RebuildDict(pairs []LeafNode) map[string]any {
	result := map[string]any{}

	for _, node := range pairs {
		current := result
		path := node.Path

		// 遍历到倒数第二个键
		for i := 0; i < len(path)-1; i++ {
			key := path[i]
			if _, ok := current[key]; !ok {
				// 预判下一个键是否为列表索引
				nextKey := path[i+1]
				if strings.HasPrefix(nextKey, "[") && strings.HasSuffix(nextKey, "]") {
					current[key] = []any{}
				} else {
					current[key] = map[string]any{}
				}
			}
			current = current[key].(map[string]any)
		}

		// 设置叶子值
		if len(path) > 0 {
			lastKey := path[len(path)-1]
			current[lastKey] = node.Value
		}
	}

	return result
}

// RemoveZeroValues 递归移除 map[string]any 中的零值。
//
// 对应 Python: remove_none_values()
// 参考场景：uapclaw-main 工具调用参数清理——LLM 返回的参数中可能包含
// 未填写的零值字段，需要在提交前清理。
//
// 移除规则（Go 零值判定）：
//   - nil
//   - 空字符串 ""
//   - 整数 0
//   - 浮点数 0.0
//   - 布尔 false
//   - 空切片 []any{}
//   - 空 map map[string]any{}
//
// 嵌套的 map[string]any 会递归清理，递归后为空的 map 也会被移除。
func RemoveZeroValues(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}

	result := make(map[string]any, len(data))
	for key, val := range data {
		if isZeroValue(val) {
			continue
		}
		// 递归处理嵌套 map
		if sub, ok := val.(map[string]any); ok {
			cleaned := RemoveZeroValues(sub)
			if len(cleaned) == 0 {
				continue
			}
			result[key] = cleaned
		} else {
			result[key] = val
		}
	}
	return result
}

// ValidateArgs 根据 JSON Schema 校验参数 map，返回校验错误。
//
// 参考 uapclaw-main: validateToolArgs(schema, args)
// 支持 JSON Schema 的子集校验：
//   - required：必需字段检查
//   - type：类型检查（string/integer/number/boolean/array/object）
//   - enum：枚举值检查
//   - 嵌套 object/array 递归校验
//   - additionalProperties：额外属性控制
//
// 不做 Pydantic 校验，纯 map 操作。
// 对于 LLM 返回的动态工具调用参数校验足够使用。
func ValidateArgs(schema map[string]any, args map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	if args == nil {
		args = map[string]any{}
	}

	if err := checkRequired(schema, args); err != nil {
		return err
	}

	propsRaw, ok := schema["properties"]
	if !ok {
		return nil
	}

	props, ok := propsRaw.(map[string]any)
	if !ok {
		return nil
	}

	additional := allowsAdditional(schema)

	for key, val := range args {
		propSchemaRaw, known := props[key]
		if !known {
			if !additional {
				return fmt.Errorf("unexpected property %q", key)
			}
			continue
		}
		propSchema, ok := propSchemaRaw.(map[string]any)
		if !ok {
			continue
		}
		if err := checkType(key, val, propSchema); err != nil {
			return err
		}
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatPath 将路径列表格式化为点分字符串。
// 列表索引（如 "[0]"）直接拼接到前一个元素后，不加点分隔符。
func formatPath(path []string) string {
	var buf strings.Builder
	for i, key := range path {
		if i == 0 || strings.HasPrefix(key, "[") {
			buf.WriteString(key)
		} else {
			buf.WriteByte('.')
			buf.WriteString(key)
		}
	}
	return buf.String()
}

// isZeroValue 判断值是否为 Go 零值。
func isZeroValue(val any) bool {
	if val == nil {
		return true
	}
	switch v := val.(type) {
	case string:
		return v == ""
	case int:
		return v == 0
	case int64:
		return v == 0
	case float64:
		return v == 0.0
	case bool:
		return !v
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}
	return false
}

// checkRequired 检查必需字段。
func checkRequired(schema map[string]any, args map[string]any) error {
	reqRaw, ok := schema["required"]
	if !ok {
		return nil
	}

	var required []string
	switch r := reqRaw.(type) {
	case []string:
		required = r
	case []any:
		for _, v := range r {
			if s, ok := v.(string); ok {
				required = append(required, s)
			}
		}
	default:
		return nil
	}

	for _, field := range required {
		if _, present := args[field]; !present {
			return fmt.Errorf("missing required property %q", field)
		}
	}
	return nil
}

// allowsAdditional 判断是否允许额外属性。
func allowsAdditional(schema map[string]any) bool {
	v, ok := schema["additionalProperties"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// checkType 校验值是否匹配 JSON Schema 类型声明。
func checkType(key string, val any, propSchema map[string]any) error {
	typeRaw, ok := propSchema["type"]
	if !ok {
		return nil
	}
	typeName, ok := typeRaw.(string)
	if !ok {
		return nil
	}

	switch typeName {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("property %q: expected string, got %T", key, val)
		}
	case "integer":
		switch v := val.(type) {
		case float64:
			if v != math.Trunc(v) {
				return fmt.Errorf("property %q: expected integer, got float64 with fractional part", key)
			}
		case int, int64:
			// ok
		default:
			return fmt.Errorf("property %q: expected integer, got %T", key, val)
		}
	case "number":
		switch val.(type) {
		case float64, int, int64:
			// ok
		default:
			return fmt.Errorf("property %q: expected number, got %T", key, val)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("property %q: expected boolean, got %T", key, val)
		}
	case "array":
		arr, ok := val.([]any)
		if !ok {
			return fmt.Errorf("property %q: expected array, got %T", key, val)
		}
		if err := checkArrayItems(key, arr, propSchema); err != nil {
			return err
		}
	case "object":
		obj, ok := val.(map[string]any)
		if !ok {
			return fmt.Errorf("property %q: expected object, got %T", key, val)
		}
		if err := ValidateArgs(propSchema, obj); err != nil {
			return fmt.Errorf("property %q: %w", key, err)
		}
	}

	if err := checkEnum(key, val, propSchema); err != nil {
		return err
	}

	return nil
}

// checkArrayItems 校验数组元素。
func checkArrayItems(key string, arr []any, propSchema map[string]any) error {
	itemsRaw, ok := propSchema["items"]
	if !ok {
		return nil
	}
	itemSchema, ok := itemsRaw.(map[string]any)
	if !ok {
		return nil
	}
	for i, elem := range arr {
		elemKey := fmt.Sprintf("%s[%d]", key, i)
		if err := checkType(elemKey, elem, itemSchema); err != nil {
			return err
		}
	}
	return nil
}

// checkEnum 校验枚举值。
func checkEnum(key string, val any, propSchema map[string]any) error {
	enumRaw, ok := propSchema["enum"]
	if !ok {
		return nil
	}

	switch ev := enumRaw.(type) {
	case []any:
		for _, allowed := range ev {
			if val == allowed {
				return nil
			}
		}
	case []string:
		if s, ok := val.(string); ok {
			for _, allowed := range ev {
				if s == allowed {
					return nil
				}
			}
		}
	default:
		return nil
	}

	return fmt.Errorf("property %q: value %v is not in enum", key, val)
}
