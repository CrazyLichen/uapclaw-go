package state

// ──────────────────────────── 深拷贝 ────────────────────────────

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
		container map[string]any
	}
	var removed []removal

	for key, value := range update {
		currentKey, current := rootToPath(key, source, true)
		if value == nil {
			removed = append(removed, removal{key: currentKey, container: current})
		} else {
			updateByKey(currentKey, value, current)
		}
	}
	for _, r := range removed {
		deleteByKey(r.key, r.container)
	}
}

// getBySchema 根据 schema 从 data 中获取值
// schema 可以是 string（路径）、map[string]any（批量映射）、[]any（列表映射）
func getBySchema(schema StateKey, data map[string]any, nestedPath ...string) any {
	if len(nestedPath) > 0 && nestedPath[0] != "" {
		data = getValueByNestedPathMap(nestedPath[0], data)
	}

	if data == nil {
		return nil
	}

	switch schema.Type() {
	case StateKeyString:
		originKey := extractOriginKey(schema.String())
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
			if !ok || p < 0 || p >= len(list) {
				return nil
			}
			if isLast {
				return list[p]
			}
			current = list[p]
		}
	}
	return nil
}

// rootToPath 沿嵌套路径导航到最终容器
// 返回 (最终key, 最终容器map)
// 注意：仅支持 string key 最终导航到 map[string]any 容器
// 对于 list 索引路径暂不支持直接导航（updateDict 中嵌套路径一般为 string key）
// createIfAbsent 为 true 时自动创建缺失的中间节点
func rootToPath(nestedPath string, source map[string]any, createIfAbsent ...bool) (any, map[string]any) {
	create := len(createIfAbsent) > 0 && createIfAbsent[0]
	paths := splitNestedPath(nestedPath)
	if len(paths) == 0 {
		return nestedPath, source
	}

	current := source
	for i, path := range paths {
		isLast := i == len(paths)-1
		switch p := path.(type) {
		case string:
			if _, exists := current[p]; !exists {
				if !create {
					return nil, nil
				}
				if !isLast && i+1 < len(paths) {
					if _, isInt := paths[i+1].(int); isInt {
						current[p] = []any{}
					} else {
						current[p] = map[string]any{}
					}
				} else {
					current[p] = map[string]any{}
				}
			}
			if isLast {
				return p, current
			}
			next, ok := current[p].(map[string]any)
			if !ok {
				if !create {
					return nil, nil
				}
				next = map[string]any{}
				current[p] = next
			}
			current = next
		case int:
			// 对于 list 索引，需要先找到包含 list 的 key
			// rootToPath 主要用于 updateDict，其中 key 都是 string 类型
			// list 索引在 updateDict 场景下不常见，返回 nil
			return nil, nil
		}
	}
	return nil, nil
}

// expandNestedStructure 将嵌套 key 的字典展开为嵌套结构
// 例: {"a.b": 1} → {"a": {"b": 1}}
func expandNestedStructure(data any) any {
	switch v := data.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, value := range v {
			currentKey, current := rootToPath(key, result, true)
			current[currentKey.(string)] = expandNestedStructure(value)
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
func updateByKey(key any, newValue any, source map[string]any) {
	keyStr, ok := key.(string)
	if !ok {
		return
	}
	if _, exists := source[keyStr]; !exists {
		source[keyStr] = expandNestedStructure(newValue)
		return
	}
	if existing, ok := source[keyStr].(map[string]any); ok {
		if newMap, ok := newValue.(map[string]any); ok {
			updateDict(newMap, existing)
			return
		}
	}
	source[keyStr] = expandNestedStructure(newValue)
}

// deleteByKey 在 source 中按 key 删除
func deleteByKey(key any, source map[string]any) {
	keyStr, ok := key.(string)
	if !ok {
		return
	}
	delete(source, keyStr)
}

// ──────────────────────────── 内部辅助函数 ────────────────────────────

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
func getBySchemaMap(schema map[string]any, data map[string]any) map[string]any {
	result := map[string]any{}
	for targetKey, targetSchema := range schema {
		switch s := targetSchema.(type) {
		case []any:
			result[targetKey] = getBySchema(ListKey(s), data)
		case map[string]any:
			result[targetKey] = getBySchema(SchemaKey(s), data)
		case string:
			// 在 schema 的值中，字符串始终被视为路径引用（非根层行为）
			result[targetKey] = getBySchema(StringKey(s), data)
		default:
			result[targetKey] = targetSchema
		}
	}
	return result
}

// getBySchemaList 处理 list schema 的递归读取
func getBySchemaList(schema []any, data map[string]any) []any {
	result := make([]any, len(schema))
	for i, item := range schema {
		switch s := item.(type) {
		case string:
			result[i] = getBySchema(StringKey(s), data)
		case map[string]any:
			result[i] = getBySchema(SchemaKey(s), data)
		case []any:
			result[i] = getBySchema(ListKey(s), data)
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
