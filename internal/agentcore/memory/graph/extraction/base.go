package extraction

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

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

// ReadableSchema 从输出模型生成 LLM 可读的类型定义字符串
//
// Python: MultilingualBaseModel.readable_schema()
func ReadableSchema(modelType reflect.Type, language string) (string, map[string]map[string]any) {
	langMap := registry.MultilingualDescription[language]
	if langMap == nil {
		langMap = map[string]string{}
	}

	// 1. 提取 Schema
	extractor := tool.StructSchemaExtractor{}
	params, err := extractor.Extract(modelType)
	if err != nil {
		return "", nil
	}

	// 2. 替换多语言描述
	replaced := ReplaceDescriptions(params, langMap)

	// 3. 生成 JSON Schema
	schemaMap := commonschema.ToJSONSchemaMap(replaced)

	// 4. 生成可读字符串（对齐 Python readable_schema 格式）
	outStr := formatReadableSchema(schemaMap, "")
	refDict := extractRefDict(schemaMap)

	return outStr, refDict
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
			if props, ok := node["properties"].(map[string]any); ok && len(props) > 0 {
				node["additionalProperties"] = false
				keys := make([]string, 0, len(props))
				for k := range props {
					keys = append(keys, k)
				}
				node["required"] = keys
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
	if modelType.Kind() == reflect.Ptr {
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

// replacePlaceholders 替换字符串中的 {{[xxx]}} 占位符
func replacePlaceholders(desc string, langMap map[string]string) string {
	for placeholder, replacement := range langMap {
		desc = strings.ReplaceAll(desc, placeholder, replacement)
	}
	return desc
}

// formatReadableSchema 递归生成可读 Schema 字符串
func formatReadableSchema(schema map[string]any, indent string) string {
	var sb strings.Builder

	typeName, _ := schema["type"].(string)

	switch typeName {
	case "object":
		sb.WriteString(indent + "class Output:\n")
		props, _ := schema["properties"].(map[string]any)
		for propName, propVal := range props {
			propMap, ok := propVal.(map[string]any)
			if !ok {
				continue
			}
			desc, _ := propMap["description"].(string)
			propType := inferPythonType(propMap)
			fmt.Fprintf(&sb, "%s    %s: %s  # %s\n", indent, propName, propType, desc)
			if propType == "dict" || propType == "list[dict]" {
				nestedStr := formatReadableSchema(propMap, indent+"    ")
				if nestedStr != "" {
					sb.WriteString(nestedStr)
				}
			}
		}
	case "array":
		items, _ := schema["items"].(map[string]any)
		if items != nil {
			nestedStr := formatReadableSchema(items, indent+"    ")
			if nestedStr != "" {
				sb.WriteString(nestedStr)
			}
		}
	}

	return sb.String()
}

// inferPythonType 从 JSON Schema 属性推断 Python 类型名
func inferPythonType(prop map[string]any) string {
	typeName, _ := prop["type"].(string)
	switch typeName {
	case "string":
		return "str"
	case "integer":
		return "int"
	case "number":
		return "float"
	case "boolean":
		return "bool"
	case "array":
		items, _ := prop["items"].(map[string]any)
		if items != nil {
			itemType := inferPythonType(items)
			return "list[" + itemType + "]"
		}
		return "list"
	case "object":
		return "dict"
	default:
		return "Any"
	}
}

// extractRefDict 从 Schema 中提取引用定义
func extractRefDict(schema map[string]any) map[string]map[string]any {
	refDict := map[string]map[string]any{}
	extractRefDictRecursive(schema, refDict)
	return refDict
}

func extractRefDictRecursive(schema map[string]any, refDict map[string]map[string]any) {
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		return
	}
	for _, propVal := range props {
		propMap, ok := propVal.(map[string]any)
		if !ok {
			continue
		}
		defs, _ := propMap["$defs"].(map[string]any)
		for defName, defVal := range defs {
			defMap, ok := defVal.(map[string]any)
			if ok {
				refDict[defName] = defMap
			}
		}
		extractRefDictRecursive(propMap, refDict)
	}
}
