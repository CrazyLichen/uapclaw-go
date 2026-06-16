package state

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

const (
	// regexMaxLength 正则匹配最大长度
	regexMaxLength = 1000
	// nestedPathSplit 嵌套路径分隔符
	nestedPathSplit = "."
	// nestedPathListSplit 列表索引开始符
	nestedPathListSplit = "["
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// deepCopyMap 深拷贝 map[string]any
func deepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = deepCopyValue(v)
	}
	return dst
}

// deepCopySlice 深拷贝 []any
func deepCopySlice(src []any) []any {
	if src == nil {
		return nil
	}
	dst := make([]any, len(src))
	for i, v := range src {
		dst[i] = deepCopyValue(v)
	}
	return dst
}

// deepCopyValue 深拷贝任意值（map/slice/原始值）
func deepCopyValue(val any) any {
	switch v := val.(type) {
	case map[string]any:
		return deepCopyMap(v)
	case []any:
		return deepCopySlice(v)
	default:
		return v // string/int/float/bool/nil 等原始值直接返回
	}
}

// splitNestedPath 拆分嵌套路径
// 例: "a_1.b.c[1].d" → ["a_1", "b", "c", 1, "d"]
func splitNestedPath(nestedKey string) []any {
	if nestedKey == "" {
		return nil
	}
	if !containsChar(nestedKey, nestedPathSplit) &&
		!containsChar(nestedKey, nestedPathListSplit) &&
		!containsChar(nestedKey, "['") {
		return nil
	}

	var result []any
	parts := splitString(nestedKey, nestedPathSplit)
	for _, part := range parts {
		if containsChar(part, nestedPathListSplit) {
			baseAndIndexes := parseListIndexes(part)
			result = append(result, baseAndIndexes...)
		} else {
			result = append(result, part)
		}
	}
	return result
}

// isRefPath 判断是否为引用路径，如 "${start123.p2}"
func isRefPath(path string) bool {
	return len(path) > 3 && len(path) <= regexMaxLength &&
		path[:2] == "${" && path[len(path)-1] == '}'
}

// extractOriginKey 从引用路径中提取原始 key
// 例: "${start123.p2}" → "start123.p2"
func extractOriginKey(key string) string {
	if !containsChar(key, "$") {
		return key
	}
	start := -1
	for i := 0; i < len(key) && i < regexMaxLength; i++ {
		if i+1 < len(key) && key[i] == '$' && key[i+1] == '{' {
			start = i + 2
			break
		}
	}
	if start == -1 {
		return key
	}
	for i := start; i < len(key); i++ {
		if key[i] == '}' {
			return key[start:i]
		}
	}
	return key
}

// updateDict 用 update 字典更新 source 字典
// source 是扁平结构，update 的 key 支持嵌套路径
// 如果 value 为 nil 则删除对应 key
func updateDict(update map[string]any, source map[string]any) {
	type removal struct {
		key       any
		container any // map[string]any 或 []any
	}
	var removed []removal

	for key, value := range update {
		currentKey, currentContainer := rootToPath(key, source, true)
		if value == nil {
			removed = append(removed, removal{key: currentKey, container: currentContainer})
		} else {
			updateByKey(currentKey, value, currentContainer)
		}
	}
	for _, r := range removed {
		deleteByKey(r.key, r.container)
	}
}

// getBySchema 根据 schema 从 data 中获取值
// schema 可以是 string（路径）、map[string]any（批量映射）、[]any（列表映射）
// isRoot 表示是否为根层调用：根层时字符串 schema 视为数据路径，非根层时非引用路径的字符串视为默认值
func getBySchema(schema StateKey, data map[string]any, isRootOrNestedPath ...any) any {
	isRoot := true
	var nestedPath string
	for _, arg := range isRootOrNestedPath {
		switch v := arg.(type) {
		case string:
			nestedPath = v
		case bool:
			isRoot = v
		}
	}

	if nestedPath != "" {
		data = getValueByNestedPathMap(nestedPath, data)
	}

	if data == nil {
		return nil
	}

	switch schema.Type() {
	case StateKeyString:
		originKey := extractOriginKey(schema.String())
		// 非根层 + 非引用路径 → 字符串本身就是值，不从 data 中查找
		if originKey == schema.String() && !isRoot {
			return schema.String()
		}
		return getValueByNestedPath(originKey, data)
	case StateKeyMap:
		return getBySchemaMap(schema.Map(), data)
	case StateKeyList:
		return getBySchemaList(schema.List(), data)
	default:
		return nil
	}
}

// getValueByNestedPath 根据嵌套路径从 source 获取值
// 例: "a.b[0].c" → source["a"]["b"][0]["c"]
func getValueByNestedPath(nestedKey string, source map[string]any) any {
	paths := splitNestedPath(nestedKey)
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

// safeExtendContainer 安全地扩展列表容器到 targetIndex 位置。
// 对齐 Python _safe_extend_container。
// 中间位置用 nil 填充，目标位置放空字典（isFinal=true）或空列表（isFinal=false）。
// 有上限保护（索引 [0,10000]、扩展量 ≤ 10000）。
func safeExtendContainer(container []any, targetIndex int, isFinal bool) ([]any, bool) {
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

// rootToIndex 通过纯索引路径导航嵌套列表结构。
// 对齐 Python root_to_index。
// 返回 (调整后的最终索引, 最终容器列表)。
// 嵌套深度上限 10，索引范围 [0,10000]，支持负索引自动调整。
func rootToIndex(indexes []int, source []any, createIfAbsent bool) (int, []any) {
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
			current, ok = safeExtendContainer(current, idx, false)
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
		current, ok = safeExtendContainer(current, finalIdx, true)
		if !ok {
			return -1, nil
		}
	}
	if finalIdx < 0 || finalIdx >= len(current) {
		return -1, nil
	}
	return finalIdx, current
}

// rootToPath 沿嵌套路径导航到最终容器
// 返回 (最终key, 最终容器)
// 最终容器可能是 map[string]any 或 []any，对应最终 key 为 string 或 int
// createIfAbsent 为 true 时自动创建缺失的中间节点
func rootToPath(nestedPath string, source map[string]any, createIfAbsent ...bool) (any, any) {
	create := len(createIfAbsent) > 0 && createIfAbsent[0]
	paths := splitNestedPath(nestedPath)
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
				list, ok2 = safeExtendContainer(list, idx, isLast)
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

// expandNestedStructure 将嵌套 key 的字典展开为嵌套结构
// 例: {"a.b": 1} → {"a": {"b": 1}}
func expandNestedStructure(data any) any {
	switch v := data.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, value := range v {
			currentKey, currentContainer := rootToPath(key, result, true)
			if currentKey == nil {
				continue
			}
			switch kk := currentKey.(type) {
			case string:
				if m, ok := currentContainer.(map[string]any); ok {
					m[kk] = expandNestedStructure(value)
				}
			case int:
				if list, ok := currentContainer.([]any); ok && kk < len(list) {
					list[kk] = expandNestedStructure(value)
				}
			}
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = expandNestedStructure(item)
		}
		return result
	default:
		return data
	}
}

// updateByKey 在 source 中按 key 更新值
// source 可以是 map[string]any 或 []any，key 对应为 string 或 int
func updateByKey(key any, newValue any, source any) {
	switch k := key.(type) {
	case string:
		m, ok := source.(map[string]any)
		if !ok {
			return
		}
		if _, exists := m[k]; !exists {
			m[k] = expandNestedStructure(newValue)
			return
		}
		if existing, ok := m[k].(map[string]any); ok {
			if newMap, ok := newValue.(map[string]any); ok {
				updateDict(newMap, existing)
				return
			}
		}
		m[k] = expandNestedStructure(newValue)
	case int:
		list, ok := source.([]any)
		if !ok {
			return
		}
		if k >= 0 && k < len(list) {
			list[k] = expandNestedStructure(newValue)
		}
	}
}

// deleteByKey 在 source 中按 key 删除
// source 可以是 map[string]any 或 []any
func deleteByKey(key any, source any) {
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

// getValueByNestedPathMap 与 getValueByNestedPath 类似，但返回 map[string]any
// 用于 getBySchema 中根据前缀定位
func getValueByNestedPathMap(nestedKey string, source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	result := getValueByNestedPath(nestedKey, source)
	if m, ok := result.(map[string]any); ok {
		return m
	}
	return nil
}

// getBySchemaMap 处理 map schema 的递归读取
// 对应 Python: get_by_schema 中 dict 分支
// 只有引用路径（${...}）才从 data 取值，普通字符串保留为默认值
func getBySchemaMap(schema map[string]any, data map[string]any) map[string]any {
	result := map[string]any{}
	for targetKey, targetSchema := range schema {
		switch s := targetSchema.(type) {
		case []any:
			result[targetKey] = getBySchema(ListKey(s), data, false)
		case map[string]any:
			result[targetKey] = getBySchema(SchemaKey(s), data, false)
		case string:
			if isRefPath(s) {
				// 引用路径 → 从 data 取值
				result[targetKey] = getBySchema(StringKey(s), data, false)
			} else {
				// 普通字符串 → 保留为默认值
				result[targetKey] = s
			}
		default:
			result[targetKey] = targetSchema
		}
	}
	return result
}

// getBySchemaList 处理 list schema 的递归读取
// 对应 Python: get_by_schema 中 list 分支
func getBySchemaList(schema []any, data map[string]any) []any {
	result := make([]any, len(schema))
	for i, item := range schema {
		switch s := item.(type) {
		case string:
			if isRefPath(s) {
				result[i] = getBySchema(StringKey(s), data, false)
			} else {
				result[i] = s
			}
		case map[string]any:
			result[i] = getBySchema(SchemaKey(s), data, false)
		case []any:
			result[i] = getBySchema(ListKey(s), data, false)
		default:
			result[i] = item
		}
	}
	return result
}

// containsChar 检查字符串是否包含指定字符/子串
func containsChar(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstring(s, substr)
}

// containsSubstring 检查字符串是否包含子串
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// splitString 按分隔符拆分字符串
func splitString(s, sep string) []string {
	if sep == "" {
		return []string{s}
	}
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

// parseListIndexes 解析包含数组索引的部分
// 例: "c[1]" → ["c", 1], "[1]" → [1], "a[-1]" → ["a", -1]
func parseListIndexes(part string) []any {
	var result []any
	bracketIdx := -1
	for i := 0; i < len(part); i++ {
		if part[i] == '[' {
			bracketIdx = i
			break
		}
	}
	if bracketIdx == -1 {
		return []any{part}
	}

	base := part[:bracketIdx]
	if base != "" {
		result = append(result, base)
	}

	remaining := part[bracketIdx:]
	for len(remaining) > 0 {
		if remaining[0] != '[' {
			break
		}
		end := -1
		for i := 1; i < len(remaining); i++ {
			if remaining[i] == ']' {
				end = i
				break
			}
		}
		if end == -1 {
			break
		}
		indexStr := remaining[1:end]
		var idx int
		isNeg := false
		parseStart := 0
		if len(indexStr) > 0 && indexStr[0] == '-' {
			isNeg = true
			parseStart = 1
		}
		isInt := true
		if parseStart >= len(indexStr) {
			isInt = false
		} else {
			parsed := 0
			for i := parseStart; i < len(indexStr); i++ {
				if indexStr[i] < '0' || indexStr[i] > '9' {
					isInt = false
					break
				}
				parsed = parsed*10 + int(indexStr[i]-'0')
			}
			if isInt {
				if isNeg {
					idx = -parsed
				} else {
					idx = parsed
				}
			}
		}
		if isInt {
			result = append(result, idx)
		} else {
			result = append(result, indexStr)
		}
		remaining = remaining[end+1:]
	}
	return result
}

// deepCopyUpdates 深拷贝暂存更新数据
func deepCopyUpdates(updates map[string][]map[string]any) map[string][]map[string]any {
	if updates == nil {
		return nil
	}
	result := make(map[string][]map[string]any, len(updates))
	for key, list := range updates {
		copied := make([]map[string]any, len(list))
		for i, u := range list {
			copied[i] = deepCopyMap(u)
		}
		result[key] = copied
	}
	return result
}

// convertUpdatesFromJSON 将 JSON 反序列化后的 updates 数据转换为 map[string][]map[string]any。
//
// JSON 反序列化会将 []map[string]any 变为 []any（每个元素是 map[string]any），
// 导致类型断言 gs.(map[string][]map[string]any) 失败。此函数递归处理，
// 将 map[string]any 中值为 []any 的字段转换为 []map[string]any。
func convertUpdatesFromJSON(raw any) (map[string][]map[string]any, bool) {
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
