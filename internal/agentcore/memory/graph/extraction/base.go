package extraction

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

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

// ReadableSchema 从输出模型生成 LLM 可读的类型定义字符串
//
// Python: MultilingualBaseModel.readable_schema()
func ReadableSchema(modelType reflect.Type, language string) (string, map[string]any) {
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

	// 4. 递归替换：删除 title、删除 required、$ref → type 内联
	recursiveReplace(schemaMap, nil, "title", "")
	recursiveReplace(schemaMap, nil, "required", "")
	// 在删除 $defs 之前提取 refDict
	refDict := extractRefDict(schemaMap)
	if defs, ok := schemaMap["$defs"].(map[string]any); ok {
		refLookup := make(map[string]string, len(defs))
		for key := range defs {
			refLookup["#/$defs/"+key] = key
		}
		recursiveReplace(schemaMap, refLookup, "$ref", "type")
		delete(schemaMap, "$defs")
	}

	// 5. 生成可读字符串（对齐 Python readable_schema 格式）
	outStr := formatReadableSchema(schemaMap, "")

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
			if props, ok := node["properties"].(map[string]any); ok {
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

// formatReadableSchema 递归生成可读 Schema 字符串
func formatReadableSchema(schema map[string]any, indent string) string {
	var sb strings.Builder

	typeName, _ := schema["type"].(string)

	switch typeName {
	case "object":
		props, _ := schema["properties"].(map[string]any)
		for propName, propVal := range props {
			propMap, ok := propVal.(map[string]any)
			if !ok {
				continue
			}
			desc, _ := propMap["description"].(string)
			propType := inferPythonType(propMap)
			fmt.Fprintf(&sb, "%s%s: %s  # %s\n", indent, propName, propType, desc)
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
func extractRefDict(schema map[string]any) map[string]any {
	refDict := map[string]any{}
	extractRefDictRecursive(schema, refDict)
	return refDict
}

func extractRefDictRecursive(schema map[string]any, refDict map[string]any) {
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
				// 对齐 Python: refDict = {key: val["properties"] for key, val in refs.items()}
				if propsOnly, ok := defMap["properties"].(map[string]any); ok {
					refDict[defName] = propsOnly
				}
			}
		}
		extractRefDictRecursive(propMap, refDict)
	}
}
