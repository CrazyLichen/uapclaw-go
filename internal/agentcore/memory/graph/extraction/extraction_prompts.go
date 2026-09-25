package extraction

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	graphobj "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/prompts/entity_extraction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// formattingConfig 格式化配置
type formattingConfig struct {
	sourceDescription string
	outputModel       reflect.Type
	outputIndent      int
	history           string
	content           string
	language          string
}

// ──────────────────────────── 枚举 ────────────────────────────

// formattingOption 格式化参数选项
type formattingOption func(*formattingConfig)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractEntityDeclaration 组装实体声明（名称）抽取提示词
//
// Python: extract_entity_declaration(src_type, content, history, description, entity_types, language, extras, indent)
func ExtractEntityDeclaration(
	srcType config.EpisodeType,
	content string,
	history string,
	description string,
	entityTypes []*registry.EntityDef,
	language string,
	extras map[string]any,
	indent int,
) (map[string]any, *prompt.PromptTemplate, map[string]any) {
	operation := strings.ToLower(srcType.String())
	templateName := fmt.Sprintf("entity_extraction_%s_%s", operation, language)

	kwargs := getFormattingKwargs(
		withSourceDescription(description),
		withOutputModel(reflect.TypeOf(EntityExtraction{})),
		withOutputIndent(indent),
		withContent(content),
		withHistory(history),
		withLanguage(language),
	)

	for k, v := range extras {
		kwargs[k] = v
	}

	if entityTypes == nil {
		entityTypes = []*registry.EntityDef{registry.DefaultEntity}
	}

	var typeLines []string
	for i, ent := range entityTypes {
		typeLines = append(typeLines, fmt.Sprintf("%d. %s%s", i, ent.Name, ent.Description[language]))
	}
	kwargs["entity_types"] = strings.Join(typeLines, "\n")

	return kwargs, prompts.GetTemplateManager().Get(templateName), multilingualResponseFormat(reflect.TypeOf(EntityExtraction{}), language)
}

// ExtractEntityAttributes 组装实体摘要与属性抽取提示词
//
// Python: extract_entity_attributes(entity, content, history, language, extras, indent)
func ExtractEntityAttributes(
	entity *graphobj.Entity,
	content string,
	history string,
	language string,
	extras map[string]any,
	indent int,
) (map[string]any, *prompt.PromptTemplate, map[string]any) {
	operation := "summary_create"
	templateName := fmt.Sprintf("entity_extraction_%s_%s", operation, language)

	kwargs := getFormattingKwargs(
		withOutputModel(reflect.TypeOf(EntitySummary{})),
		withOutputIndent(indent),
		withContent(content),
		withHistory(history),
		withLanguage(language),
	)

	kwargs["entity_name"] = entity.Name
	if entity.Content == "" {
		kwargs["entity_summary"] = ""
	} else {
		kwargs["entity_summary"] = entity.Content
	}

	if len(entity.Attributes) > 0 {
		attrBytes, _ := json.MarshalIndent(entity.Attributes, "", strings.Repeat(" ", indent))
		kwargs["entity_attribute"] = string(attrBytes)
	}

	for k, v := range extras {
		kwargs[k] = v
	}

	// Python: entity.obj_type.casefold() == "human" and "summary_target" in kwargs
	if strings.ToLower(entity.ObjType) == "human" {
		if st, ok := kwargs["summary_target"]; ok {
			switch v := st.(type) {
			case int:
				kwargs["summary_target"] = v * 2
			case float64:
				kwargs["summary_target"] = int(v) * 2
			case string:
				// Python: isinstance(summary_target, str) and summary_target.isdigit()
				var intVal int
				if _, err := fmt.Sscanf(v, "%d", &intVal); err == nil {
					kwargs["summary_target"] = intVal * 2
				}
			}
		}
	}

	return kwargs, prompts.GetTemplateManager().Get(templateName), multilingualResponseFormat(reflect.TypeOf(EntitySummary{}), language)
}

// ExtractRelationDeclaration 组装关系抽取提示词
//
// Python: extract_relation_declaration(relation_types, entities, reference_time, tz_info, content, history, entity_types, description, language, indent)
func ExtractRelationDeclaration(
	relationTypes []registry.RelationDef,
	entities []EntityDeclaration,
	referenceTime int64,
	tzInfo any,
	content string,
	history string,
	entityTypes []*registry.EntityDef,
	description string,
	language string,
	indent int,
) (map[string]any, *prompt.PromptTemplate, map[string]any) {
	operation := "relation"
	templateName := fmt.Sprintf("entity_extraction_%s_%s", operation, language)

	kwargs := getFormattingKwargs(
		withSourceDescription(description),
		withOutputModel(reflect.TypeOf(RelationExtraction{})),
		withOutputIndent(indent),
		withHistory(history),
		withContent(content),
		withLanguage(language),
	)

	// Python: isinstance(tz_info, (dict, list)) → json.dumps; else str()
	switch v := tzInfo.(type) {
	case map[string]any, []any:
		tzBytes, _ := json.MarshalIndent(v, "", strings.Repeat(" ", indent))
		kwargs["tz_info"] = string(tzBytes)
	default:
		kwargs["tz_info"] = fmt.Sprintf("%v", tzInfo)
	}

	kwargs["entities"] = formatNewEntities(entities, entityTypes, 1, language)
	kwargs["relation_types"] = entity_extraction.FormatRelationDefinitions(relationTypes, language)
	kwargs["reference_time"] = time.Unix(referenceTime, 0).Format("2006-01-02T15:04:05Z07:00")
	kwargs["id_range"] = fmt.Sprintf("1-%d", len(entities))

	return kwargs, prompts.GetTemplateManager().Get(templateName), multilingualResponseFormat(reflect.TypeOf(RelationExtraction{}), language)
}

// ExtractTimezone 组装时区抽取提示词
//
// Python: extract_timezone(content, history, description, language, indent)
func ExtractTimezone(
	content string,
	history string,
	description string,
	language string,
	indent int,
) (map[string]any, *prompt.PromptTemplate, map[string]any) {
	operation := "timezone"
	templateName := fmt.Sprintf("entity_extraction_%s_%s", operation, language)

	kwargs := getFormattingKwargs(
		withSourceDescription(description),
		withOutputModel(reflect.TypeOf(TimezonePredictions{})),
		withOutputIndent(indent),
		withHistory(history),
		withContent(content),
		withLanguage(language),
	)

	return kwargs, prompts.GetTemplateManager().Get(templateName), multilingualResponseFormat(reflect.TypeOf(TimezonePredictions{}), language)
}

// MergeExistingEntities 组装实体合并提示词
//
// Python: merge_existing_entities(target, sources, language, extras, indent)
func MergeExistingEntities(
	target *graphobj.Entity,
	sources []*graphobj.Entity,
	language string,
	extras map[string]any,
	indent int,
) (map[string]any, *prompt.PromptTemplate, map[string]any) {
	operation := "entity_merge"
	templateName := fmt.Sprintf("entity_extraction_%s_%s", operation, language)

	kwargs := getFormattingKwargs(
		withOutputModel(reflect.TypeOf(EntitySummary{})),
		withOutputIndent(indent),
		withLanguage(language),
	)

	kwargs["entity_name"] = target.Name
	if target.Content == "" {
		kwargs["entity_summary"] = ""
	} else {
		kwargs["entity_summary"] = target.Content
	}

	if len(target.Attributes) > 0 {
		attrBytes, _ := json.MarshalIndent(target.Attributes, "", strings.Repeat(" ", indent))
		kwargs["entity_attribute"] = string(attrBytes)
	}

	// Python: format_existing_entities([e.model_dump() for e in sources], language=language)
	sourceMaps := make([]map[string]any, len(sources))
	for i, s := range sources {
		sourceMaps[i] = s.ToMap()
	}
	kwargs["entities_to_merge"] = entity_extraction.FormatExistingEntities(sourceMaps, 1, language)

	for k, v := range extras {
		kwargs[k] = v
	}

	return kwargs, prompts.GetTemplateManager().Get(templateName), multilingualResponseFormat(reflect.TypeOf(EntitySummary{}), language)
}

// FilterRelationsForMerge 组装关系过滤提示词
//
// Python: filter_relations_for_merge(target, relations, language, extras, indent)
func FilterRelationsForMerge(
	target *graphobj.Entity,
	relations []*graphobj.Relation,
	language string,
	extras map[string]any,
	indent int,
) (map[string]any, *prompt.PromptTemplate, map[string]any) {
	operation := "relation_filter"
	templateName := fmt.Sprintf("entity_extraction_%s_%s", operation, language)

	// Python: relations = [r.model_dump() if isinstance(r, Relation) else r for r in relations]
	relMaps := make([]map[string]any, len(relations))
	for i, r := range relations {
		relMaps[i] = r.ToMap()
	}

	kwargs := getFormattingKwargs(
		withOutputModel(reflect.TypeOf(RelevantFacts{})),
		withOutputIndent(indent),
		withLanguage(language),
	)

	kwargs["entity_name"] = target.Name
	if target.Content == "" {
		kwargs["entity_summary"] = ""
	} else {
		kwargs["entity_summary"] = target.Content
	}

	if len(target.Attributes) > 0 {
		attrBytes, _ := json.MarshalIndent(target.Attributes, "", strings.Repeat(" ", indent))
		kwargs["entity_attribute"] = string(attrBytes)
	}

	// Python: format_existing_relations(relations, include_time=False)
	kwargs["existing_relations"] = entity_extraction.FormatExistingRelations(relMaps, 1, false)

	for k, v := range extras {
		kwargs[k] = v
	}

	return kwargs, prompts.GetTemplateManager().Get(templateName), multilingualResponseFormat(reflect.TypeOf(RelevantFacts{}), language)
}

// DedupeEntityList 组装实体去重提示词
//
// Python: dedupe_entity_list(content, candidate_entities, existing_entities, entity_types, history, description, language, indent)
func DedupeEntityList(
	content string,
	candidateEntities []EntityDeclaration,
	existingEntities []map[string]any,
	entityTypes []*registry.EntityDef,
	history string,
	description string,
	language string,
	indent int,
) (map[string]any, *prompt.PromptTemplate, map[string]any) {
	operation := "dedupe_entity"
	templateName := fmt.Sprintf("entity_extraction_%s_%s", operation, language)

	kwargs := getFormattingKwargs(
		withSourceDescription(description),
		withOutputModel(reflect.TypeOf(EntityDuplication{})),
		withOutputIndent(indent),
		withHistory(history),
		withContent(content),
		withLanguage(language),
	)

	kwargs["entities"] = entity_extraction.FormatExistingEntities(existingEntities, 1, language)
	kwargs["candidate_entities"] = formatNewEntities(
		candidateEntities, entityTypes, len(existingEntities)+1, language,
	)

	return kwargs, prompts.GetTemplateManager().Get(templateName), multilingualResponseFormat(reflect.TypeOf(EntityDuplication{}), language)
}

// DedupeRelationList 组装关系去重提示词
//
// Python: dedupe_relation_list(content, relation, existing_relations, existing_entities, history, description, language, indent)
func DedupeRelationList(
	content string,
	relation *graphobj.Relation,
	existingRelations []map[string]any,
	existingEntities []*graphobj.Entity,
	history string,
	description string,
	language string,
	indent int,
) (map[string]any, *prompt.PromptTemplate, map[string]any) {
	operation := "dedupe_relation"
	templateName := fmt.Sprintf("entity_extraction_%s_%s", operation, language)

	kwargs := getFormattingKwargs(
		withSourceDescription(description),
		withOutputModel(reflect.TypeOf(MergeRelations{})),
		withOutputIndent(indent),
		withHistory(history),
		withContent(content),
		withLanguage(language),
	)

	// Python: format_existing_entities(existing_entities, 1, language)
	entityMaps := make([]map[string]any, len(existingEntities))
	for i, e := range existingEntities {
		entityMaps[i] = e.ToMap()
	}
	kwargs["entities"] = entity_extraction.FormatExistingEntities(entityMaps, 1, language)

	kwargs["existing_relations"] = entity_extraction.FormatExistingRelations(existingRelations, 1, true)

	// Python: format_existing_relations([relation.model_dump()], 0).removeprefix("0. ")
	newRelStr := entity_extraction.FormatExistingRelations([]map[string]any{relation.ToMap()}, 0, true)
	kwargs["new_relation"] = strings.TrimPrefix(newRelStr, "0. ")

	return kwargs, prompts.GetTemplateManager().Get(templateName), multilingualResponseFormat(reflect.TypeOf(MergeRelations{}), language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// withSourceDescription 设置数据源描述
func withSourceDescription(desc string) formattingOption {
	return func(c *formattingConfig) { c.sourceDescription = desc }
}

// withOutputModel 设置输出模型类型
func withOutputModel(t reflect.Type) formattingOption {
	return func(c *formattingConfig) { c.outputModel = t }
}

// withOutputIndent 设置输出缩进
func withOutputIndent(indent int) formattingOption {
	return func(c *formattingConfig) { c.outputIndent = indent }
}

// withHistory 设置历史消息
func withHistory(history string) formattingOption {
	return func(c *formattingConfig) { c.history = history }
}

// withContent 设置当前内容
func withContent(content string) formattingOption {
	return func(c *formattingConfig) { c.content = content }
}

// withLanguage 设置语言
func withLanguage(language string) formattingOption {
	return func(c *formattingConfig) { c.language = language }
}

// getFormattingKwargs 组装提示词格式化关键字参数
//
// Python: get_formatting_kwargs(source_description, output_model, output_indent, history, content, language)
//
// 对齐 Python prompts/entity_extraction/base.py 中的同名函数。
// 将各种格式化参数整理为 kwargs map，供后续模板替换使用。
func getFormattingKwargs(opts ...formattingOption) map[string]any {
	cfg := &formattingConfig{
		outputIndent: 2,
		language:     "cn",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// 组装 context：Python 中 MARK_HISTORY_MSG + MARK_CURRENT_MSG
	var context strings.Builder
	if cfg.history != "" {
		tmpl, ok := registry.MarkHistoryMsg[cfg.language]
		if ok {
			context.WriteString(strings.ReplaceAll(tmpl, "{history}", cfg.history))
		}
	}
	if cfg.content != "" {
		tmpl, ok := registry.MarkCurrentMsg[cfg.language]
		if ok {
			context.WriteString(strings.ReplaceAll(tmpl, "{content}", cfg.content))
		}
	}

	// format_schema_info：通过 ReadableSchema 生成
	var extraMessage string
	if cfg.outputModel != nil {
		outStr, refDict := ReadableSchema(cfg.outputModel, cfg.language)
		extraMessage = entity_extraction.FormatSchemaInfo(outStr, refDict, cfg.outputIndent, cfg.language)
	}

	return map[string]any{
		"source_description": entity_extraction.FormatSourceDescription(cfg.sourceDescription, cfg.language),
		"extra_message":      extraMessage,
		"context":            context.String(),
	}
}

// formatNewEntities 格式化新提取的候选实体列表
//
// Python: format_new_entities(entities, entity_types, start_idx, language)
//
// 对齐 Python extraction_prompts.py 中的同名函数。
// 如果提供 entity_types，先展示类型定义 + "---"，再列出带类型的实体；
// 否则只列出编号+名称。
func formatNewEntities(
	entities []EntityDeclaration,
	entityTypes []*registry.EntityDef,
	startIdx int,
	language string,
) string {
	if len(entities) == 0 {
		return ""
	}

	sep := registry.MultilingualDescription[language][":"]

	if len(entityTypes) > 0 {
		// Python: 引入 entity_types
		typeIDSet := make(map[int]struct{})
		for _, e := range entities {
			typeIDSet[e.EntityTypeID] = struct{}{}
		}
		sortedIDs := make([]int, 0, len(typeIDSet))
		for id := range typeIDSet {
			sortedIDs = append(sortedIDs, id)
		}
		sort.Ints(sortedIDs)

		var lines []string
		for _, typeID := range sortedIDs {
			if typeID < len(entityTypes) {
				ent := entityTypes[typeID]
				lines = append(lines, fmt.Sprintf("%s%s%s", ent.Name, sep, ent.Description[language]))
			}
		}
		lines = append(lines, "---")

		for i, entity := range entities {
			var typeName string
			if entity.EntityTypeID < len(entityTypes) {
				typeName = entityTypes[entity.EntityTypeID].Name
			}
			lines = append(lines, fmt.Sprintf("%d. %s (%s)", startIdx+i, entity.Name, typeName))
		}
		return strings.Join(lines, "\n")
	}

	// 无 entity_types：只列出编号+名称
	var lines []string
	for i, entity := range entities {
		lines = append(lines, fmt.Sprintf("%d. %s", startIdx+i, entity.Name))
	}
	return strings.Join(lines, "\n")
}

// multilingualResponseFormat 将输出模型转为多语言 response_format 映射
//
// Python: MultilingualBaseModel.response_format(language)
//
// 逻辑：
//  1. 使用 StructSchemaExtractor 提取 struct 的 Param 列表
//  2. 用 MultilingualDescription 替换 description 占位符
//  3. 用 ToJSONSchemaMap 生成完整 JSON Schema
//  4. 用 ResponseFormat 包装为 OpenAI structured output 格式
func multilingualResponseFormat(modelType reflect.Type, language string) map[string]any {
	langMap := registry.MultilingualDescription[language]
	if langMap == nil {
		langMap = map[string]string{}
	}

	extractor := tool.StructSchemaExtractor{}
	params, err := extractor.Extract(modelType)
	if err != nil || len(params) == 0 {
		// 降级：空 schema
		return ResponseFormat(modelType.Name(), map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		})
	}

	replaced := ReplaceDescriptions(params, langMap)
	schemaMap := commonschema.ToJSONSchemaMap(replaced)
	StrictSchemaEnforce(schemaMap)
	return ResponseFormat(modelType.Name(), schemaMap)
}
