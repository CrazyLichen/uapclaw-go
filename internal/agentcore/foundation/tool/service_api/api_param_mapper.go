package service_api

import (
	"strings"
)

// ──────────────────────────── 枚举 ────────────────────────────

// APIParamLocation API 参数位置枚举，基于 OpenAPI 规范定义参数在 HTTP 请求中的位置。
//
// 对应 Python: openjiuwen/core/foundation/tool/service_api/api_param_mapper.py (APIParamLocation)
type APIParamLocation int

const (
	// APIParamLocationQuery 查询参数，URL 中 ?key=value
	APIParamLocationQuery APIParamLocation = iota
	// APIParamLocationPath 路径参数，URL 中 /users/{id}
	APIParamLocationPath
	// APIParamLocationBody 请求体参数
	APIParamLocationBody
	// APIParamLocationHeader HTTP 请求头参数
	APIParamLocationHeader
	// APIParamLocationForm 表单数据参数
	APIParamLocationForm
)

// String 返回 APIParamLocation 的字符串表示。
func (l APIParamLocation) String() string {
	switch l {
	case APIParamLocationQuery:
		return "query"
	case APIParamLocationPath:
		return "path"
	case APIParamLocationBody:
		return "body"
	case APIParamLocationHeader:
		return "header"
	case APIParamLocationForm:
		return "form"
	default:
		return "unknown"
	}
}

// ──────────────────────────── 结构体 ────────────────────────────

// APIParamMapper 将输入参数映射到 HTTP 请求的各位置（query/path/body/header/form）。
//
// 根据 JSON Schema 中每个参数的 location 字段决定其目标位置，
// 并合并 Card 上预设的默认参数（queries/headers/paths）。
//
// 对应 Python: openjiuwen/core/foundation/tool/service_api/api_param_mapper.py (APIParamMapper)
type APIParamMapper struct {
	// schema 原始 JSON Schema map（从 RestfulApiCard.InputSchema 传入）
	schema map[string]any
	// defaultQueries 默认查询参数
	defaultQueries map[string]any
	// defaultHeaders 默认请求头参数
	defaultHeaders map[string]any
	// defaultPaths 默认路径参数
	defaultPaths map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// locationKey JSON Schema 中参数位置字段的键名
	locationKey = "location"
	// formHandlerTypeKey JSON Schema 中表单处理器类型字段的键名
	formHandlerTypeKey = "form_handler_type"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAPIParamMapper 创建参数映射器。
//
// 参数：
//   - schema: 原始 JSON Schema map，properties 中可含 location 字段
//   - defaultQueries: Card 预设的默认查询参数
//   - defaultHeaders: Card 预设的默认请求头
//   - defaultPaths: Card 预设的默认路径参数
func NewAPIParamMapper(
	schema map[string]any,
	defaultQueries map[string]any,
	defaultHeaders map[string]any,
	defaultPaths map[string]any,
) *APIParamMapper {
	return &APIParamMapper{
		schema:         schema,
		defaultQueries: defaultQueries,
		defaultHeaders: defaultHeaders,
		defaultPaths:   defaultPaths,
	}
}

// Map 将输入参数映射到各 API 位置。
//
// 流程：
//  1. 遍历 schema.properties，读取每个参数的 location 字段
//  2. 有显式 location → 按指定位置；无 → 用 defaultLocation
//  3. FORM 类型参数 → 存储 {form_handler_type, value}
//  4. 合并 defaults：defaults 为基础，inputs 中非 nil/非空字符串的值覆盖 defaults
//
// 对应 Python: APIParamMapper.map()
func (m *APIParamMapper) Map(inputs map[string]any, defaultLocation APIParamLocation) map[APIParamLocation]map[string]any {
	// 初始化各位置的结果桶
	result := map[APIParamLocation]map[string]any{
		APIParamLocationQuery:  {},
		APIParamLocationPath:   {},
		APIParamLocationBody:   {},
		APIParamLocationHeader: {},
		APIParamLocationForm:   {},
	}

	// 如果没有 schema，所有输入放到 defaultLocation
	if m.schema == nil {
		result[defaultLocation] = inputs
		return result
	}

	properties, _ := m.schema["properties"].(map[string]any)

	for paramName, paramSchema := range properties {
		value, exists := inputs[paramName]
		if !exists {
			continue
		}

		paramMap, _ := paramSchema.(map[string]any)
		location := defaultLocation

		// 读取 location 字段
		if locRaw, ok := paramMap[locationKey]; ok {
			if locStr, ok := locRaw.(string); ok {
				if parsed, err := parseAPIParamLocation(locStr); err == nil {
					location = parsed
				}
			}
		}

		// FORM 类型特殊处理：存储 {form_handler_type, value}
		if location == APIParamLocationForm {
			formHandlerType := "default"
			if fht, ok := paramMap[formHandlerTypeKey]; ok {
				if s, ok := fht.(string); ok {
					formHandlerType = s
				}
			}
			if value != nil {
				result[APIParamLocationForm][paramName] = map[string]any{
					"form_handler_type": formHandlerType,
					"value":             value,
				}
			}
			continue
		}

		result[location][paramName] = value
	}

	// 合并 defaults（inputs 中非 nil/非空字符串的值覆盖 defaults）
	for _, location := range []APIParamLocation{APIParamLocationPath, APIParamLocationQuery, APIParamLocationHeader} {
		var defaults map[string]any
		switch location {
		case APIParamLocationPath:
			defaults = m.defaultPaths
		case APIParamLocationQuery:
			defaults = m.defaultQueries
		case APIParamLocationHeader:
			defaults = m.defaultHeaders
		}
		merged := make(map[string]any)
		// 先填充 defaults
		for k, v := range defaults {
			merged[k] = v
		}
		// inputs 中非 nil/非空字符串的值覆盖 defaults
		for k, v := range result[location] {
			if v != nil && v != "" {
				merged[k] = v
			}
		}
		result[location] = merged
	}

	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseAPIParamLocation 将字符串解析为 APIParamLocation。
func parseAPIParamLocation(s string) (APIParamLocation, error) {
	switch strings.ToLower(s) {
	case "query":
		return APIParamLocationQuery, nil
	case "path":
		return APIParamLocationPath, nil
	case "body":
		return APIParamLocationBody, nil
	case "header":
		return APIParamLocationHeader, nil
	case "form":
		return APIParamLocationForm, nil
	default:
		return APIParamLocationBody, nil // 未知值 fallback 到 body
	}
}
