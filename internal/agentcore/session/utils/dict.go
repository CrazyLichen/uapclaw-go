package utils

// ──────────────────────────── 导出函数 ────────────────────────────

// UpdateDict 用 update 字典更新 source 字典
// source 是扁平结构，update 的 key 支持嵌套路径
// 如果 value 为 nil 则删除对应 key
func UpdateDict(update map[string]any, source map[string]any) {
	type removal struct {
		key       any
		container any // map[string]any 或 []any
	}
	var removed []removal

	for key, value := range update {
		currentKey, currentContainer := RootToPath(key, source, true)
		if value == nil {
			removed = append(removed, removal{key: currentKey, container: currentContainer})
		} else {
			UpdateByKey(currentKey, value, currentContainer)
		}
	}
	for _, r := range removed {
		DeleteByKey(r.key, r.container)
	}
}

// UpdateByKey 在 source 中按 key 更新值
// source 可以是 map[string]any 或 []any，key 对应为 string 或 int
func UpdateByKey(key any, newValue any, source any) {
	switch k := key.(type) {
	case string:
		m, ok := source.(map[string]any)
		if !ok {
			return
		}
		if _, exists := m[k]; !exists {
			m[k] = ExpandNestedStructure(newValue)
			return
		}
		if existing, ok := m[k].(map[string]any); ok {
			if newMap, ok := newValue.(map[string]any); ok {
				UpdateDict(newMap, existing)
				return
			}
		}
		m[k] = ExpandNestedStructure(newValue)
	case int:
		list, ok := source.([]any)
		if !ok {
			return
		}
		if k >= 0 && k < len(list) {
			list[k] = ExpandNestedStructure(newValue)
		}
	}
}

// DeleteByKey 在 source 中按 key 删除
// source 可以是 map[string]any 或 []any
func DeleteByKey(key any, source any) {
	switch k := key.(type) {
	case string:
		if m, ok := source.(map[string]any); ok {
			delete(m, k)
		}
	case int:
		if list, ok := source.([]any); ok {
			if k >= 0 && k < len(list) {
				list[k] = nil
			}
		}
	}
}

// ExpandNestedStructure 将嵌套 key 的字典展开为嵌套结构
// 例: {"a.b": 1} → {"a": {"b": 1}}
func ExpandNestedStructure(data any) any {
	switch v := data.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, value := range v {
			currentKey, currentContainer := RootToPath(key, result, true)
			if currentKey == nil {
				continue
			}
			switch kk := currentKey.(type) {
			case string:
				if m, ok := currentContainer.(map[string]any); ok {
					m[kk] = ExpandNestedStructure(value)
				}
			case int:
				if list, ok := currentContainer.([]any); ok && kk < len(list) {
					list[kk] = ExpandNestedStructure(value)
				}
			}
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = ExpandNestedStructure(item)
		}
		return result
	default:
		return data
	}
}
