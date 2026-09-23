package extraction

import (
	"reflect"
	"strings"
	"testing"

	graphobj "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestExtractEntityDeclaration_返回值 测试实体声明抽取提示词组装
func TestExtractEntityDeclaration_返回值(t *testing.T) {
	kwargs, tmpl, outputFmt := ExtractEntityDeclaration(
		config.EpisodeTypeConversation,
		"用户说：我叫张三",
		"",
		"",
		nil,
		"cn",
		nil,
		2,
	)

	// 验证 kwargs 基本字段
	if kwargs["context"] == "" {
		t.Error("context 不应为空")
	}
	if _, ok := kwargs["entity_types"]; !ok {
		t.Error("kwargs 应包含 entity_types")
	}
	if _, ok := kwargs["extra_message"]; !ok {
		t.Error("kwargs 应包含 extra_message")
	}

	// 验证模板
	if tmpl == nil {
		t.Fatal("模板不应为 nil")
	}
	if tmpl.Name != "entity_extraction_conversation_cn" {
		t.Errorf("模板名称应为 entity_extraction_conversation_cn，实际 %s", tmpl.Name)
	}

	// 验证 outputFmt
	verifyResponseFormat(t, outputFmt, "EntityExtraction")
}

// TestExtractEntityDeclaration_文档类型 测试文档类型的实体声明抽取
func TestExtractEntityDeclaration_文档类型(t *testing.T) {
	kwargs, tmpl, _ := ExtractEntityDeclaration(
		config.EpisodeTypeDocument,
		"这是一份技术文档",
		"",
		"技术文档描述",
		nil,
		"cn",
		nil,
		2,
	)

	if tmpl.Name != "entity_extraction_document_cn" {
		t.Errorf("文档类型模板名称应为 entity_extraction_document_cn，实际 %s", tmpl.Name)
	}

	// 验证 description 被格式化到 source_description 中
	srcDesc, ok := kwargs["source_description"].(string)
	if !ok || srcDesc == "" {
		t.Error("source_description 不应为空（传入了 description）")
	}
}

// TestExtractEntityDeclaration_自定义实体类型 测试自定义实体类型列表
func TestExtractEntityDeclaration_自定义实体类型(t *testing.T) {
	entityTypes := []*registry.EntityDef{
		registry.DefaultEntity,
		registry.HumanEntity,
	}
	kwargs, _, _ := ExtractEntityDeclaration(
		config.EpisodeTypeConversation,
		"内容",
		"",
		"",
		entityTypes,
		"cn",
		nil,
		2,
	)

	entityTypesStr, ok := kwargs["entity_types"].(string)
	if !ok {
		t.Fatal("entity_types 应为 string")
	}
	if !strings.Contains(entityTypesStr, "Entity") {
		t.Error("entity_types 应包含 Entity 类型")
	}
	if !strings.Contains(entityTypesStr, "Human") {
		t.Error("entity_types 应包含 Human 类型")
	}
}

// TestExtractEntityDeclaration_英文 测试英文语言
func TestExtractEntityDeclaration_英文(t *testing.T) {
	_, tmpl, _ := ExtractEntityDeclaration(
		config.EpisodeTypeConversation,
		"Hello world",
		"",
		"",
		nil,
		"en",
		nil,
		2,
	)

	if tmpl.Name != "entity_extraction_conversation_en" {
		t.Errorf("英文模板名称应为 entity_extraction_conversation_en，实际 %s", tmpl.Name)
	}
}

// TestExtractRelationDeclaration_返回值 测试关系抽取提示词组装
func TestExtractRelationDeclaration_返回值(t *testing.T) {
	entities := []EntityDeclaration{
		{Name: "张三", EntityTypeID: 0},
		{Name: "华为", EntityTypeID: 0},
	}
	relationTypes := []registry.RelationDef{*registry.DefaultRelation}

	kwargs, tmpl, outputFmt := ExtractRelationDeclaration(
		relationTypes,
		entities,
		1700000000,
		"Asia/Shanghai",
		"张三在华为工作",
		"",
		[]*registry.EntityDef{registry.DefaultEntity},
		"",
		"cn",
		2,
	)

	// 验证 kwargs 关键字段
	if _, ok := kwargs["tz_info"]; !ok {
		t.Error("kwargs 应包含 tz_info")
	}
	if _, ok := kwargs["entities"]; !ok {
		t.Error("kwargs 应包含 entities")
	}
	if _, ok := kwargs["relation_types"]; !ok {
		t.Error("kwargs 应包含 relation_types")
	}
	if _, ok := kwargs["reference_time"]; !ok {
		t.Error("kwargs 应包含 reference_time")
	}
	if kwargs["id_range"] != "1-2" {
		t.Errorf("id_range 应为 1-2，实际 %v", kwargs["id_range"])
	}

	// 验证模板
	if tmpl == nil {
		t.Fatal("模板不应为 nil")
	}
	if tmpl.Name != "entity_extraction_relation_cn" {
		t.Errorf("模板名称应为 entity_extraction_relation_cn，实际 %s", tmpl.Name)
	}

	// 验证 outputFmt
	verifyResponseFormat(t, outputFmt, "RelationExtraction")
}

// TestDedupeEntityList_返回值 测试实体去重提示词组装
func TestDedupeEntityList_返回值(t *testing.T) {
	candidates := []EntityDeclaration{
		{Name: "张三", EntityTypeID: 0},
	}
	existing := []map[string]any{
		{"name": "李四", "content": "工程师"},
	}

	kwargs, tmpl, outputFmt := DedupeEntityList(
		"对话内容",
		candidates,
		existing,
		nil,
		"",
		"",
		"cn",
		2,
	)

	// 验证 kwargs
	if _, ok := kwargs["entities"]; !ok {
		t.Error("kwargs 应包含 entities")
	}
	if _, ok := kwargs["candidate_entities"]; !ok {
		t.Error("kwargs 应包含 candidate_entities")
	}

	// 验证模板
	if tmpl == nil {
		t.Fatal("模板不应为 nil")
	}
	if tmpl.Name != "entity_extraction_dedupe_entity_cn" {
		t.Errorf("模板名称应为 entity_extraction_dedupe_entity_cn，实际 %s", tmpl.Name)
	}

	// 验证 outputFmt
	verifyResponseFormat(t, outputFmt, "EntityDuplication")
}

// TestDedupeRelationList_返回值 测试关系去重提示词组装
func TestDedupeRelationList_返回值(t *testing.T) {
	rel := graphobj.NewRelation()
	rel.Name = "工作于"
	rel.Content = "张三在华为工作"

	existingRels := []map[string]any{
		{"content": "李四在腾讯工作"},
	}
	existingEntities := []*graphobj.Entity{
		graphobj.NewEntity(),
	}

	kwargs, tmpl, outputFmt := DedupeRelationList(
		"对话内容",
		rel,
		existingRels,
		existingEntities,
		"",
		"",
		"cn",
		2,
	)

	// 验证 kwargs
	if _, ok := kwargs["entities"]; !ok {
		t.Error("kwargs 应包含 entities")
	}
	if _, ok := kwargs["existing_relations"]; !ok {
		t.Error("kwargs 应包含 existing_relations")
	}
	if _, ok := kwargs["new_relation"]; !ok {
		t.Error("kwargs 应包含 new_relation")
	}
	// new_relation 不应以 "0. " 开头（Python: .removeprefix("0. ")）
	newRel, ok := kwargs["new_relation"].(string)
	if ok && strings.HasPrefix(newRel, "0. ") {
		t.Error("new_relation 不应以 '0. ' 开头")
	}

	// 验证模板
	if tmpl == nil {
		t.Fatal("模板不应为 nil")
	}
	if tmpl.Name != "entity_extraction_dedupe_relation_cn" {
		t.Errorf("模板名称应为 entity_extraction_dedupe_relation_cn，实际 %s", tmpl.Name)
	}

	// 验证 outputFmt
	verifyResponseFormat(t, outputFmt, "MergeRelations")
}

// TestExtractTimezone_返回值 测试时区抽取提示词组装
func TestExtractTimezone_返回值(t *testing.T) {
	kwargs, tmpl, outputFmt := ExtractTimezone(
		"用户在上海工作",
		"",
		"",
		"cn",
		2,
	)

	// 验证 kwargs
	if kwargs["context"] == "" {
		t.Error("context 不应为空（传入了 content）")
	}

	// 验证模板
	if tmpl == nil {
		t.Fatal("模板不应为 nil")
	}
	if tmpl.Name != "entity_extraction_timezone_cn" {
		t.Errorf("模板名称应为 entity_extraction_timezone_cn，实际 %s", tmpl.Name)
	}

	// 验证 outputFmt
	verifyResponseFormat(t, outputFmt, "TimezonePredictions")
}

// TestExtractEntityAttributes_返回值 测试实体属性抽取提示词组装
func TestExtractEntityAttributes_返回值(t *testing.T) {
	entity := graphobj.NewEntity()
	entity.Name = "张三"
	entity.Content = "工程师"
	entity.Attributes = map[string]any{"department": "研发"}

	kwargs, tmpl, outputFmt := ExtractEntityAttributes(
		entity,
		"新增信息",
		"",
		"cn",
		nil,
		2,
	)

	// 验证 kwargs
	if kwargs["entity_name"] != "张三" {
		t.Errorf("entity_name 应为 张三，实际 %v", kwargs["entity_name"])
	}
	if kwargs["entity_summary"] != "工程师" {
		t.Errorf("entity_summary 应为 工程师，实际 %v", kwargs["entity_summary"])
	}
	if _, ok := kwargs["entity_attribute"]; !ok {
		t.Error("kwargs 应包含 entity_attribute（有 attributes）")
	}

	// 验证模板
	if tmpl == nil {
		t.Fatal("模板不应为 nil")
	}
	if tmpl.Name != "entity_extraction_summary_create_cn" {
		t.Errorf("模板名称应为 entity_extraction_summary_create_cn，实际 %s", tmpl.Name)
	}

	verifyResponseFormat(t, outputFmt, "EntitySummary")
}

// TestExtractEntityAttributes_人类类型SummaryTarget翻倍 测试 Human 类型 summary_target 翻倍
func TestExtractEntityAttributes_人类类型SummaryTarget翻倍(t *testing.T) {
	entity := graphobj.NewEntity()
	entity.ObjType = "Human"
	entity.Name = "用户"
	entity.Content = "摘要"

	extras := map[string]any{"summary_target": 250}

	kwargs, _, _ := ExtractEntityAttributes(
		entity,
		"内容",
		"",
		"cn",
		extras,
		2,
	)

	st := kwargs["summary_target"]
	if st != 500 {
		t.Errorf("Human 类型 summary_target 应翻倍为 500，实际 %v", st)
	}
}

// TestMergeExistingEntities_返回值 测试实体合并提示词组装
func TestMergeExistingEntities_返回值(t *testing.T) {
	target := graphobj.NewEntity()
	target.Name = "张三"
	target.Content = "合并后的摘要"

	source1 := graphobj.NewEntity()
	source1.Name = "张三"
	source1.Content = "工程师"
	source2 := graphobj.NewEntity()
	source2.Name = "张三"
	source2.Content = "在华为工作"

	kwargs, tmpl, outputFmt := MergeExistingEntities(
		target,
		[]*graphobj.Entity{source1, source2},
		"cn",
		nil,
		2,
	)

	// 验证 kwargs
	if kwargs["entity_name"] != "张三" {
		t.Errorf("entity_name 应为 张三，实际 %v", kwargs["entity_name"])
	}
	if _, ok := kwargs["entities_to_merge"]; !ok {
		t.Error("kwargs 应包含 entities_to_merge")
	}

	// 验证模板
	if tmpl == nil {
		t.Fatal("模板不应为 nil")
	}
	if tmpl.Name != "entity_extraction_entity_merge_cn" {
		t.Errorf("模板名称应为 entity_extraction_entity_merge_cn，实际 %s", tmpl.Name)
	}

	verifyResponseFormat(t, outputFmt, "EntitySummary")
}

// TestFilterRelationsForMerge_返回值 测试关系过滤提示词组装
func TestFilterRelationsForMerge_返回值(t *testing.T) {
	target := graphobj.NewEntity()
	target.Name = "张三"
	target.Content = "工程师"

	rel1 := graphobj.NewRelation()
	rel1.Content = "张三在华为工作"

	kwargs, tmpl, outputFmt := FilterRelationsForMerge(
		target,
		[]*graphobj.Relation{rel1},
		"cn",
		nil,
		2,
	)

	// 验证 kwargs
	if kwargs["entity_name"] != "张三" {
		t.Errorf("entity_name 应为 张三，实际 %v", kwargs["entity_name"])
	}
	if _, ok := kwargs["existing_relations"]; !ok {
		t.Error("kwargs 应包含 existing_relations")
	}

	// 验证模板
	if tmpl == nil {
		t.Fatal("模板不应为 nil")
	}
	if tmpl.Name != "entity_extraction_relation_filter_cn" {
		t.Errorf("模板名称应为 entity_extraction_relation_filter_cn，实际 %s", tmpl.Name)
	}

	verifyResponseFormat(t, outputFmt, "RelevantFacts")
}

// TestGetFormattingKwargs_基本 测试格式化参数组装基本功能
func TestGetFormattingKwargs_基本(t *testing.T) {
	kwargs := getFormattingKwargs(
		withContent("测试内容"),
		withHistory("历史内容"),
		withLanguage("cn"),
	)

	// context 应包含历史和当前内容
	context, ok := kwargs["context"].(string)
	if !ok {
		t.Fatal("context 应为 string")
	}
	if !strings.Contains(context, "历史内容") {
		t.Error("context 应包含历史内容")
	}
	if !strings.Contains(context, "测试内容") {
		t.Error("context 应包含当前内容")
	}
}

// TestGetFormattingKwargs_空参数 测试空参数时返回空值
func TestGetFormattingKwargs_空参数(t *testing.T) {
	kwargs := getFormattingKwargs(withLanguage("cn"))

	if kwargs["context"] != "" {
		t.Error("空参数时 context 应为空")
	}
	if kwargs["source_description"] != "" {
		t.Error("空参数时 source_description 应为空")
	}
}

// TestFormatNewEntities_有类型 测试有实体类型时的格式化
func TestFormatNewEntities_有类型(t *testing.T) {
	entities := []EntityDeclaration{
		{Name: "张三", EntityTypeID: 0},
		{Name: "华为", EntityTypeID: 1},
	}
	entityTypes := []*registry.EntityDef{
		registry.DefaultEntity,
		registry.HumanEntity,
	}

	result := formatNewEntities(entities, entityTypes, 1, "cn")

	if !strings.Contains(result, "Entity") {
		t.Error("结果应包含 Entity 类型定义")
	}
	if !strings.Contains(result, "Human") {
		t.Error("结果应包含 Human 类型定义")
	}
	if !strings.Contains(result, "---") {
		t.Error("结果应包含类型分隔符 ---")
	}
	if !strings.Contains(result, "张三 (Entity)") {
		t.Error("结果应包含 '张三 (Entity)'")
	}
	if !strings.Contains(result, "华为 (Human)") {
		t.Error("结果应包含 '华为 (Human)'")
	}
}

// TestFormatNewEntities_无类型 测试无实体类型时的格式化
func TestFormatNewEntities_无类型(t *testing.T) {
	entities := []EntityDeclaration{
		{Name: "张三", EntityTypeID: 0},
		{Name: "华为", EntityTypeID: 0},
	}

	result := formatNewEntities(entities, nil, 1, "cn")

	if !strings.Contains(result, "1. 张三") {
		t.Error("结果应包含 '1. 张三'")
	}
	if !strings.Contains(result, "2. 华为") {
		t.Error("结果应包含 '2. 华为'")
	}
	if strings.Contains(result, "---") {
		t.Error("无类型时不应包含 --- 分隔符")
	}
}

// TestFormatNewEntities_空列表 测试空实体列表
func TestFormatNewEntities_空列表(t *testing.T) {
	result := formatNewEntities(nil, nil, 1, "cn")
	if result != "" {
		t.Error("空列表应返回空字符串")
	}
}

// TestFormatNewEntities_起始索引 测试自定义起始索引
func TestFormatNewEntities_起始索引(t *testing.T) {
	entities := []EntityDeclaration{
		{Name: "张三", EntityTypeID: 0},
	}

	result := formatNewEntities(entities, nil, 5, "cn")
	if !strings.Contains(result, "5. 张三") {
		t.Errorf("起始索引 5 时应包含 '5. 张三'，实际 %s", result)
	}
}

// TestMultilingualResponseFormat_基本 测试多语言响应格式生成
func TestMultilingualResponseFormat_基本(t *testing.T) {
	import_reflect := reflect.TypeOf(EntityExtraction{})
	outputFmt := multilingualResponseFormat(import_reflect, "cn")

	verifyResponseFormat(t, outputFmt, "EntityExtraction")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// verifyResponseFormat 验证 response_format 结构
func verifyResponseFormat(t *testing.T, outputFmt map[string]any, expectedName string) {
	t.Helper()

	if outputFmt["type"] != "json_schema" {
		t.Errorf("type 应为 json_schema，实际 %v", outputFmt["type"])
	}
	js, ok := outputFmt["json_schema"].(map[string]any)
	if !ok {
		t.Fatal("json_schema 应为 map")
	}
	if js["name"] != expectedName {
		t.Errorf("name 应为 %s，实际 %v", expectedName, js["name"])
	}
	if js["strict"] != false {
		t.Error("strict 应为 false")
	}
	schema, ok := js["schema"].(map[string]any)
	if !ok {
		t.Fatal("schema 应为 map")
	}
	if schema["type"] != "object" {
		t.Errorf("schema.type 应为 object，实际 %v", schema["type"])
	}
}
