package extraction

import (
	"reflect"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// TestEntityExtraction_Schema生成 测试 EntityExtraction 的 Schema 生成
func TestEntityExtraction_Schema生成(t *testing.T) {
	params, err := tool.StructSchemaExtractor{}.Extract(reflect.TypeOf(EntityExtraction{}))
	if err != nil {
		t.Fatalf("Extract 报错: %v", err)
	}
	if len(params) == 0 {
		t.Fatal("params 不应为空")
	}
	schemaMap := commonschema.ToJSONSchemaMap(params)
	if schemaMap["type"] != "object" {
		t.Errorf("顶层 type 应为 object，实际 %v", schemaMap["type"])
	}
	props, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties 应为 map")
	}
	if _, ok := props["extracted_entities"]; !ok {
		t.Error("应包含 extracted_entities 属性")
	}
}

// TestReplaceDescriptions_中文替换 测试多语言描述替换
func TestReplaceDescriptions_中文替换(t *testing.T) {
	params, err := tool.StructSchemaExtractor{}.Extract(reflect.TypeOf(EntitySummary{}))
	if err != nil {
		t.Fatalf("Extract 报错: %v", err)
	}

	// 打印所有 Param 信息
	for _, p := range params {
		t.Logf("Param: Name=%q Description=%q", p.Name, p.Description)
	}

	// 查找 summary 字段
	var summaryParam *commonschema.Param
	for _, p := range params {
		if p.Name == "summary" {
			summaryParam = p
			break
		}
	}
	if summaryParam == nil {
		// 可能 StructSchemaExtractor 使用 Go 字段名而非 json tag 名
		for _, p := range params {
			t.Logf("Fallback Param: Name=%q Description=%q", p.Name, p.Description)
		}
		t.Fatal("未找到 summary 字段")
	}
	if !strings.Contains(summaryParam.Description, "{{[ent_summary]}}") {
		t.Errorf("summary description 应包含占位符，实际: %q", summaryParam.Description)
	}

	// 执行替换
	cnMap := GetMultilingualDescription("cn")
	if cnMap == nil {
		t.Fatal("cnMap 不应为 nil")
	}
	replaced := ReplaceDescriptions(params, cnMap)

	// 验证替换后的 description
	for _, p := range replaced {
		if p.Name == "summary" {
			if strings.Contains(p.Description, "{{[ent_summary]}}") {
				t.Errorf("替换后 description 不应仍为占位符，实际: %q", p.Description)
			}
			if p.Description == "" {
				t.Error("替换后 description 不应为空")
			}
			break
		}
	}
}

// TestResponseFormat_包装 测试 ResponseFormat 包装函数
func TestResponseFormat_包装(t *testing.T) {
	schemaMap := map[string]any{"type": "object", "properties": map[string]any{}}
	rf := ResponseFormat("EntityExtraction", schemaMap)
	if rf["type"] != "json_schema" {
		t.Errorf("type 应为 json_schema，实际 %v", rf["type"])
	}
	js, ok := rf["json_schema"].(map[string]any)
	if !ok {
		t.Fatal("json_schema 应为 map")
	}
	if js["name"] != "EntityExtraction" {
		t.Errorf("name 应为 EntityExtraction，实际 %v", js["name"])
	}
	if js["strict"] != false {
		t.Error("strict 应为 false")
	}
}

// TestEntityDeclaration_Schema生成 测试 EntityDeclaration 的 Schema 生成
func TestEntityDeclaration_Schema生成(t *testing.T) {
	params, err := tool.StructSchemaExtractor{}.Extract(reflect.TypeOf(EntityDeclaration{}))
	if err != nil {
		t.Fatalf("Extract 报错: %v", err)
	}
	schemaMap := commonschema.ToJSONSchemaMap(params)
	props, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties 应为 map")
	}
	if _, ok := props["name"]; !ok {
		t.Error("应包含 name 属性")
	}
	if _, ok := props["entity_type_id"]; !ok {
		t.Error("应包含 entity_type_id 属性")
	}
}

// TestRelationExtraction_Schema生成 测试 RelationExtraction 的 Schema 生成
func TestRelationExtraction_Schema生成(t *testing.T) {
	params, err := tool.StructSchemaExtractor{}.Extract(reflect.TypeOf(RelationExtraction{}))
	if err != nil {
		t.Fatalf("Extract 报错: %v", err)
	}
	schemaMap := commonschema.ToJSONSchemaMap(params)
	props, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties 应为 map")
	}
	if _, ok := props["extracted_relations"]; !ok {
		t.Error("应包含 extracted_relations 属性")
	}
}

// TestMergeRelations_Schema生成 测试 MergeRelations 的 Schema 生成
func TestMergeRelations_Schema生成(t *testing.T) {
	params, err := tool.StructSchemaExtractor{}.Extract(reflect.TypeOf(MergeRelations{}))
	if err != nil {
		t.Fatalf("Extract 报错: %v", err)
	}
	schemaMap := commonschema.ToJSONSchemaMap(params)
	props, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties 应为 map")
	}
	if _, ok := props["need_merging"]; !ok {
		t.Error("应包含 need_merging 属性")
	}
}
