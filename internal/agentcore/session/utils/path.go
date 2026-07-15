package utils

// ──────────────────────────── 结构体 ────────────────────────────

// parentEntry 父容器追踪条目，用于列表 append 后回写。
type parentEntry struct {
	// m 父 map（如果父容器是 map）
	m map[string]any
	// mKey 在父 map 中的键
	mKey string
	// l 父 list（如果父容器是 list）
	l []any
	// lIdx 在父 list 中的索引
	lIdx int
	// isMap 父容器是 map 还是 list
	isMap bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// SplitNestedPath 拆分嵌套路径
// 例: "a_1.b.c[1].d" → ["a_1", "b", "c", 1, "d"]
func SplitNestedPath(nestedKey string) []any {
	if nestedKey == "" {
		return nil
	}
	if !ContainsChar(nestedKey, NestedPathSplit) &&
		!ContainsChar(nestedKey, NestedPathListSplit) &&
		!ContainsChar(nestedKey, "['") {
		return nil
	}

	var result []any
	parts := SplitString(nestedKey, NestedPathSplit)
	for _, part := range parts {
		if ContainsChar(part, NestedPathListSplit) {
			baseAndIndexes := ParseListIndexes(part)
			result = append(result, baseAndIndexes...)
		} else {
			result = append(result, part)
		}
	}
	return result
}

// GetValueByNestedPath 根据嵌套路径从 source 获取值
// 例: "a.b[0].c" → source["a"]["b"][0]["c"]
func GetValueByNestedPath(nestedKey string, source map[string]any) any {
	paths := SplitNestedPath(nestedKey)
	if len(paths) == 0 {
		return source[nestedKey]
	}

	var current any = source
	for i, path := range paths {
		isLast := i == len(paths)-1
		switch p := path.(type) {
		case string:
			m, ok := current.(map[string]any)
			if !ok {
				return nil
			}
			val, exists := m[p]
			if !exists {
				return nil
			}
			if isLast {
				return val
			}
			current = val
		case int:
			list, ok := current.([]any)
			if !ok {
				return nil
			}
			idx := p
			if idx < 0 {
				idx = len(list) + idx
				if idx < 0 {
					return nil
				}
			}
			if idx >= len(list) {
				return nil
			}
			if isLast {
				return list[idx]
			}
			current = list[idx]
		}
	}
	return nil
}

// RootToPath 沿嵌套路径导航到最终容器
// 返回 (最终key, 最终容器)
// 最终容器可能是 map[string]any 或 []any，对应最终 key 为 string 或 int
// createIfAbsent 为 true 时自动创建缺失的中间节点
func RootToPath(nestedPath string, source map[string]any, createIfAbsent ...bool) (any, any) {
	create := len(createIfAbsent) > 0 && createIfAbsent[0]
	paths := SplitNestedPath(nestedPath)
	if len(paths) == 0 {
		return nestedPath, source
	}

	var current any = source
	// 父容器追踪栈，用于列表 append 后回写
	parents := make([]parentEntry, 0, len(paths))

	for i, path := range paths {
		isLast := i == len(paths)-1
		switch p := path.(type) {
		case string:
			m, ok := current.(map[string]any)
			if !ok {
				return nil, nil
			}
			if _, exists := m[p]; !exists {
				if !create {
					return nil, nil
				}
				if !isLast && i+1 < len(paths) {
					if _, isInt := paths[i+1].(int); isInt {
						m[p] = []any{}
					} else {
						m[p] = map[string]any{}
					}
				} else {
					m[p] = map[string]any{}
				}
			}
			if isLast {
				return p, m
			}
			// 支持中间节点为 map 或 list
			switch next := m[p].(type) {
			case map[string]any:
				parents = append(parents, parentEntry{m: m, mKey: p, isMap: true})
				current = next
			case []any:
				parents = append(parents, parentEntry{m: m, mKey: p, isMap: true})
				current = next
			default:
				if !create {
					return nil, nil
				}
				next = map[string]any{}
				m[p] = next
				parents = append(parents, parentEntry{m: m, mKey: p, isMap: true})
				current = next
			}
		case int:
			list, ok := current.([]any)
			if !ok {
				return nil, nil
			}
			idx := p
			if idx < 0 {
				idx = len(list) + idx
				if idx < 0 {
					return nil, nil
				}
			}
			// 自动扩展列表
			if idx >= len(list) {
				if !create {
					return nil, nil
				}
				var ok2 bool
				list, ok2 = SafeExtendContainer(list, idx, isLast)
				if !ok2 {
					return nil, nil
				}
				// 回写到父容器（append 可能换了底层数组）
				writeBackList(parents, list)
			}
			if isLast {
				return idx, list
			}
			if idx >= len(list) {
				return nil, nil
			}
			parents = append(parents, parentEntry{l: list, lIdx: idx, isMap: false})
			current = list[idx]
		}
	}
	return nil, nil
}

// RootToIndex 通过纯索引路径导航嵌套列表结构。
// 对齐 Python root_to_index。
// 返回 (调整后的最终索引, 最终容器列表)。
// 嵌套深度上限 10，索引范围 [0,10000]，支持负索引自动调整。
func RootToIndex(indexes []int, source []any, createIfAbsent bool) (int, []any) {
	if source == nil || len(indexes) == 0 {
		return -1, nil
	}
	if len(indexes) > 10 {
		return -1, nil
	}

	current := source

	// 处理中间索引
	for i := 0; i < len(indexes)-1; i++ {
		idx := indexes[i]
		// 处理负索引
		if idx < 0 {
			idx = len(current) + idx
			if idx < 0 {
				return -1, nil
			}
		} else if idx > 10000 {
			return -1, nil
		}
		// 越界扩展
		if idx >= len(current) {
			if !createIfAbsent {
				return -1, nil
			}
			var ok bool
			current, ok = SafeExtendContainer(current, idx, false)
			if !ok {
				return -1, nil
			}
		}
		// 安全访问
		if idx >= len(current) {
			return -1, nil
		}
		next, ok := current[idx].([]any)
		if !ok {
			if current[idx] != nil {
				return -1, nil
			}
			// nil 位置自动创建列表
			if !createIfAbsent {
				return -1, nil
			}
			next = []any{}
			current[idx] = next
		}
		current = next
	}

	// 处理最终索引
	finalIdx := indexes[len(indexes)-1]
	if finalIdx < 0 {
		finalIdx = len(current) + finalIdx
		if finalIdx < 0 {
			return -1, nil
		}
	} else if finalIdx > 10000 {
		return -1, nil
	}
	if finalIdx >= len(current) {
		if !createIfAbsent {
			return -1, nil
		}
		var ok bool
		current, ok = SafeExtendContainer(current, finalIdx, true)
		if !ok {
			return -1, nil
		}
	}
	if finalIdx < 0 || finalIdx >= len(current) {
		return -1, nil
	}
	return finalIdx, current
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeBackList 将 append 后可能更换底层数组的 list 回写到父容器。
func writeBackList(parents []parentEntry, list []any) {
	if len(parents) == 0 {
		return
	}
	parent := parents[len(parents)-1]
	if parent.isMap {
		parent.m[parent.mKey] = list
	} else {
		parent.l[parent.lIdx] = list
	}
}
