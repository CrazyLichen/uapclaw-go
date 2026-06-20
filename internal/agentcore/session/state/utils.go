package state

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

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
		originKey := utils.ExtractOriginKey(schema.String())
		// 非根层 + 非引用路径 → 字符串本身就是值，不从 data 中查找
		if originKey == schema.String() && !isRoot {
			return schema.String()
		}
		return utils.GetValueByNestedPath(originKey, data)
	case StateKeyMap:
		return getBySchemaMap(schema.Map(), data)
	case StateKeyList:
		return getBySchemaList(schema.List(), data)
	default:
		return nil
	}
}

// getValueByNestedPathMap 与 GetValueByNestedPath 类似，但返回 map[string]any
// 用于 getBySchema 中根据前缀定位
func getValueByNestedPathMap(nestedKey string, source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	result := utils.GetValueByNestedPath(nestedKey, source)
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
			if utils.IsRefPath(s) {
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
			if utils.IsRefPath(s) {
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
