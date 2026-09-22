package extraction

import (
	"encoding/json"
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

// FormatSchemaInfo 将 LLM 可读 Schema 拼接到提示词末尾
//
// Python: format_schema_info(output_model, indent, language)
func FormatSchemaInfo(outStr string, refDict map[string]map[string]any, indent int, language string) string {
	if outStr == "" {
		return ""
	}

	schemaInfo := registry.SchemaInfoHeader
	if len(refDict) > 0 {
		schemaInfo += fmt.Sprintf("# %s\n", registry.RefJSONObjectDef[language])
		for k, v := range refDict {
			vBytes, _ := jsonMarshalIndent(v, indent)
			schemaInfo += fmt.Sprintf("## %s\n```json\n%s\n```\n", k, string(vBytes))
		}
	}
	schemaInfo += fmt.Sprintf("---\n# %s\n```python\n%s\n```", registry.OutputFormat[language], outStr)
	return schemaInfo
}

// FormatSourceDescription 格式化数据源描述
//
// Python: format_source_description(source_description, language)
func FormatSourceDescription(sourceDescription string, language string) string {
	if sourceDescription == "" {
		return ""
	}
	tmpl, ok := registry.SourceDescription[language]
	if !ok {
		return sourceDescription
	}
	return strings.ReplaceAll(tmpl, "{source_description}", sourceDescription)
}

// FormatRelationDefinitions 格式化关系类型定义
//
// Python: format_relation_definitions(relation_types, language)
func FormatRelationDefinitions(relationTypes []RelationDef, language string) string {
	if len(relationTypes) == 0 {
		return registry.NoRelationGiven[language]
	}
	tmpl := registry.RelationFormat[language]
	var lines []string
	for _, rtype := range relationTypes {
		desc := rtype.Description[language]
		line := strings.ReplaceAll(tmpl, "{name}", rtype.Name)
		line = strings.ReplaceAll(line, "{description}", desc)
		line = strings.ReplaceAll(line, "{lhs}", rtype.LHS.Name)
		line = strings.ReplaceAll(line, "{rhs}", rtype.RHS.Name)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// FormatExistingEntities 格式化已有实体列表
//
// Python: format_existing_entities(entities, start_idx, language)
func FormatExistingEntities(entities []map[string]any, startIdx int, language string) string {
	if len(entities) == 0 {
		return ""
	}
	tmpl := registry.DisplayEntity[language]
	var lines []string
	for i, ent := range entities {
		line := strings.ReplaceAll(tmpl, "{i}", fmt.Sprintf("%d", startIdx+i))
		line = strings.ReplaceAll(line, "{name}", fmt.Sprintf("%v", ent["name"]))
		line = strings.ReplaceAll(line, "{content}", fmt.Sprintf("%v", ent["content"]))
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n\n")
}

// FormatExistingRelations 格式化已有关系列表
//
// Python: format_existing_relations(relations, start_idx, include_time)
func FormatExistingRelations(relations []map[string]any, startIdx int, includeTime bool) string {
	if len(relations) == 0 {
		return ""
	}
	tmpl := "{i}. {content}"
	var lines []string
	for i, rel := range relations {
		content := fmt.Sprintf("%v", rel["content"])
		line := strings.ReplaceAll(tmpl, "{i}", fmt.Sprintf("%d", startIdx+i))
		line = strings.ReplaceAll(line, "{content}", content)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n\n")
}

// EnsureValidLanguage 校验语言是否有效且已注册
//
// Python: ensure_valid_language(language, max_len)
func EnsureValidLanguage(language string, maxLen int) (string, error) {
	if language == "" {
		return "", fmt.Errorf("语言选项不能为空")
	}
	if _, ok := registry.RegisteredLanguage[language]; !ok {
		registered := make([]string, 0, len(registry.RegisteredLanguage))
		for lang := range registry.RegisteredLanguage {
			registered = append(registered, lang)
		}
		return "", fmt.Errorf("graph memory 不支持语言 %s，已注册: %v", language, registered)
	}
	if len(language) > maxLen {
		return "", fmt.Errorf("语言 \"%s\" 超过数据库配置的最大长度限制 (%d)", language, maxLen)
	}
	return language, nil
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

// jsonMarshalIndent 将 map 序列化为缩进 JSON
func jsonMarshalIndent(v map[string]any, indent int) ([]byte, error) {
	indentStr := strings.Repeat(" ", indent)
	return json.MarshalIndent(v, "", indentStr)
}
