package extraction

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// placeholderPattern 精确匹配 {{[xxx]}} 格式的占位符（对齐 Python _recursive_replace 的 lookup 语义）
var placeholderPattern = regexp.MustCompile(`\{\{\[([^\]]+)\]\}\}`)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMultilingualDescription 获取指定语言的多语言描述映射
//
// Python: MULTILINGUAL_DESCRIPTION[language]
func GetMultilingualDescription(lang string) map[string]string {
	return registry.MultilingualDescription[lang]
}

// ReplaceDescriptions 递归替换 Param 列表中的多语言 description 占位符
//
// Python: MultilingualBaseModel._replace_descriptions()
//
// 将 params 中每个 Param 的 Description 字段中的 {{[xxx]}} 占位符
// 替换为 langMap 中对应的描述文本。
func ReplaceDescriptions(params []*commonschema.Param, langMap map[string]string) []*commonschema.Param {
	result := make([]*commonschema.Param, len(params))
	for i, p := range params {
		newP := *p
		newP.Description = replacePlaceholders(p.Description, langMap)
		// 递归替换嵌套的 Properties
		if len(p.Properties) > 0 {
			newP.Properties = ReplaceDescriptions(p.Properties, langMap)
		}
		result[i] = &newP
	}
	return result
}

// ResponseFormat 将 JSON Schema 包装为 OpenAI structured output 格式
//
// Python: MultilingualBaseModel.response_format()
func ResponseFormat(name string, schemaMap map[string]any) map[string]any {
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"schema": schemaMap,
			"name":   name,
			"strict": false,
		},
	}
}

// ReadableSchema 从输出模型生成 LLM 可读的类型定义字符串。
// 对齐 Python MultilingualBaseModel.readable_schema()：按 model_fields 声明顺序输出
// `name: type` 与可选 `  # description`，并返回嵌套模型的属性表（$defs 语义）。
func ReadableSchema(modelType reflect.Type, language string) (string, map[string]any) {
	langMap := registry.MultilingualDescription[language]
	if langMap == nil {
		langMap = map[string]string{}
	}
	for modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return "", nil
	}

	refDict := make(map[string]any)
	lines := make([]string, 0, modelType.NumField())
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		if field.PkgPath != "" {
			continue // 未导出字段
		}
		name := jsonFieldName(field)
		if name == "" {
			continue
		}
		line := name + ": " + pythonTypeName(field.Type, refDict, langMap)
		if desc := fieldDescription(field, langMap); desc != "" {
			line += "  # " + desc
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), refDict
}

// StrictSchemaEnforce BFS 遍历 JSON Schema，对所有 type=object 节点设置 additionalProperties=false 和 required
//
// 对齐 Python MultilingualBaseModel.multilingual_model_json_schema(strict=True) 中的 BFS 逻辑
func StrictSchemaEnforce(schemaMap map[string]any) {
	toVisit := []map[string]any{schemaMap}
	for len(toVisit) > 0 {
		node := toVisit[0]
		toVisit = toVisit[1:]
		if node == nil {
			continue
		}
		if typeName, _ := node["type"].(string); typeName == "object" {
			if props, ok := node["properties"].(map[string]any); ok {
				node["additionalProperties"] = false
				keys := make([]string, 0, len(props))
				for k := range props {
					keys = append(keys, k)
				}
				// 对齐 Python: node.setdefault("required", list(property_field.keys()))
				if _, exists := node["required"]; !exists {
					node["required"] = keys
				}
			}
		}
		// BFS 继续遍历所有值
		for _, v := range node {
			switch child := v.(type) {
			case map[string]any:
				toVisit = append(toVisit, child)
			case []any:
				for _, item := range child {
					if m, ok := item.(map[string]any); ok {
						toVisit = append(toVisit, m)
					}
				}
			}
		}
	}
}

// BuildResponseFormat 从模型实例生成完整的 OpenAI response_format
//
// 封装 StructSchemaExtractor → ReplaceDescriptions → ToJSONSchemaMap → StrictSchemaEnforce → ResponseFormat 链路，
// 对齐 Python EntitySummary.response_format(language) 的完整调用链
func BuildResponseFormat(model any, language string) map[string]any {
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}

	langMap := registry.MultilingualDescription[language]
	if langMap == nil {
		langMap = map[string]string{}
	}

	// 1. 提取 Schema
	extractor := tool.StructSchemaExtractor{}
	params, err := extractor.Extract(modelType)
	if err != nil {
		return ResponseFormat(modelType.Name(), map[string]any{})
	}

	// 2. 替换多语言描述
	replaced := ReplaceDescriptions(params, langMap)

	// 3. 生成 JSON Schema
	schemaMap := commonschema.ToJSONSchemaMap(replaced)

	// 4. 严格模式：BFS 设置 additionalProperties=false + required
	StrictSchemaEnforce(schemaMap)

	// 5. 包装为 OpenAI response_format
	return ResponseFormat(modelType.Name(), schemaMap)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// replacePlaceholders 精确匹配替换（对齐 Python _recursive_replace 的 lookup 语义）
func replacePlaceholders(desc string, langMap map[string]string) string {
	return placeholderPattern.ReplaceAllStringFunc(desc, func(match string) string {
		if replacement, ok := langMap[match]; ok {
			return replacement
		}
		return match
	})
}

// recursiveReplace 递归替换 JSON Schema map 中的字段
// 对齐 Python: _recursive_replace(to_search, lookup, from_key, to_key=None)
func recursiveReplace(data any, lookup map[string]string, fromKey string, toKey string) bool {
	replaced := false
	switch d := data.(type) {
	case map[string]any:
		if val, exists := d[fromKey]; exists {
			if toKey == "" {
				delete(d, fromKey)
				replaced = true
			} else {
				descKey, ok := val.(string)
				if ok && lookup != nil {
					if replacement, found := lookup[descKey]; found {
						d[toKey] = replacement
					} else {
						d[toKey] = descKey
					}
				} else if ok {
					d[toKey] = descKey
				}
				if fromKey != toKey {
					delete(d, fromKey)
				}
				replaced = true
			}
		}
		for _, v := range d {
			if recursiveReplace(v, lookup, fromKey, toKey) {
				replaced = true
			}
		}
	case []any:
		for _, v := range d {
			if recursiveReplace(v, lookup, fromKey, toKey) {
				replaced = true
			}
		}
	}
	return replaced
}

// jsonFieldName 返回字段的 JSON 名；`json:"-"` 返回空串。
func jsonFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		name = field.Name
	}
	return name
}

// fieldDescription 取字段 jsonschema tag 中的描述并做多语言替换。
func fieldDescription(field reflect.StructField, langMap map[string]string) string {
	tag := field.Tag.Get("jsonschema")
	_, rest, ok := strings.Cut(tag, "description=")
	if !ok {
		return ""
	}
	return replacePlaceholders(rest, langMap)
}

// pythonTypeName 将 Go 类型映射为 Python 注解形态。
// 对齐 Python: _to_json_types(annotation)，嵌套结构体同时登记到 refDict。
func pythonTypeName(t reflect.Type, refDict map[string]any, langMap map[string]string) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "str"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.Bool:
		return "bool"
	case reflect.Slice, reflect.Array:
		return "list[" + pythonTypeName(t.Elem(), refDict, langMap) + "]"
	case reflect.Map:
		return "dict"
	case reflect.Struct:
		name := t.Name()
		if name == "" {
			return "dict"
		}
		if _, done := refDict[name]; !done {
			refDict[name] = structProperties(t, langMap) // 先占位再填充，天然防递归
		}
		return name
	default:
		return "Any"
	}
}

// structProperties 生成嵌套模型的属性表（键为 JSON 字段名）。
func structProperties(t reflect.Type, langMap map[string]string) map[string]any {
	props := make(map[string]any, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name := jsonFieldName(field)
		if name == "" {
			continue
		}
		item := map[string]any{"type": jsonSchemaType(field.Type)}
		if desc := fieldDescription(field, langMap); desc != "" {
			item["description"] = desc
		}
		props[name] = item
	}
	return props
}

// jsonSchemaType 返回 JSON Schema 类型名（用于 refDict 渲染）。
func jsonSchemaType(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Struct, reflect.Map:
		return "object"
	default:
		return "string"
	}
}
