package entity_extraction

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

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
func FormatRelationDefinitions(relationTypes []registry.RelationDef, language string) string {
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

// jsonMarshalIndent 将 map 序列化为缩进 JSON
func jsonMarshalIndent(v map[string]any, indent int) ([]byte, error) {
	indentStr := strings.Repeat(" ", indent)
	return json.MarshalIndent(v, "", indentStr)
}
