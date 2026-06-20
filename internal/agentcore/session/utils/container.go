package utils

// ──────────────────────────── 导出函数 ────────────────────────────

// SafeExtendContainer 安全地扩展列表容器到 targetIndex 位置。
// 对齐 Python _safe_extend_container。
// 中间位置用 nil 填充，目标位置放空字典（isFinal=true）或空列表（isFinal=false）。
// 有上限保护（索引 [0,10000]、扩展量 ≤ 10000）。
func SafeExtendContainer(container []any, targetIndex int, isFinal bool) ([]any, bool) {
	if targetIndex < 0 || targetIndex > 10000 {
		return container, false
	}
	currentLen := len(container)
	if targetIndex < currentLen {
		return container, true
	}
	expansionNeeded := targetIndex - currentLen + 1
	if expansionNeeded > 10000 {
		return container, false
	}
	// 填充中间位置
	for i := currentLen; i < targetIndex; i++ {
		container = append(container, nil)
	}
	// 目标位置
	if isFinal {
		container = append(container, map[string]any{})
	} else {
		container = append(container, []any{})
	}
	return container, true
}

// DeepCopyMap 深拷贝 map[string]any
func DeepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = DeepCopyValue(v)
	}
	return dst
}

// DeepCopySlice 深拷贝 []any
func DeepCopySlice(src []any) []any {
	if src == nil {
		return nil
	}
	dst := make([]any, len(src))
	for i, v := range src {
		dst[i] = DeepCopyValue(v)
	}
	return dst
}

// DeepCopyValue 深拷贝任意值（map/slice/原始值）
func DeepCopyValue(val any) any {
	switch v := val.(type) {
	case map[string]any:
		return DeepCopyMap(v)
	case []any:
		return DeepCopySlice(v)
	default:
		return v // string/int/float/bool/nil 等原始值直接返回
	}
}

// DeepCopyUpdates 深拷贝暂存更新数据
func DeepCopyUpdates(updates map[string][]map[string]any) map[string][]map[string]any {
	if updates == nil {
		return nil
	}
	result := make(map[string][]map[string]any, len(updates))
	for key, list := range updates {
		copied := make([]map[string]any, len(list))
		for i, u := range list {
			copied[i] = DeepCopyMap(u)
		}
		result[key] = copied
	}
	return result
}

// ConvertUpdatesFromJSON 将 JSON 反序列化后的 updates 数据转换为 map[string][]map[string]any。
//
// JSON 反序列化会将 []map[string]any 变为 []any（每个元素是 map[string]any），
// 导致类型断言 gs.(map[string][]map[string]any) 失败。此函数递归处理，
// 将 map[string]any 中值为 []any 的字段转换为 []map[string]any。
func ConvertUpdatesFromJSON(raw any) (map[string][]map[string]any, bool) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	result := make(map[string][]map[string]any, len(m))
	for key, val := range m {
		slice, ok := val.([]any)
		if !ok {
			return nil, false
		}
		maps := make([]map[string]any, len(slice))
		for i, item := range slice {
			itemMap, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			maps[i] = itemMap
		}
		result[key] = maps
	}
	return result, true
}
