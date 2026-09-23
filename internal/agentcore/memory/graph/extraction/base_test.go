package extraction

import (
	"testing"

	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/prompts/cn"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/prompts/en"
)

// TestStrictSchemaEnforce_设置additionalProperties 测试 BFS 设置 additionalProperties=false
func TestStrictSchemaEnforce_设置additionalProperties(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{"type": "string"},
			"attributes": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
				"properties": map[string]any{
					"key": map[string]any{"type": "string"},
				},
			},
		},
	}
	StrictSchemaEnforce(schema)

	// 顶层 object 应设置 additionalProperties=false
	ap, ok := schema["additionalProperties"].(bool)
	if !ok || ap {
		t.Errorf("顶层 additionalProperties 期望 false，实际 %v", schema["additionalProperties"])
	}
	// 顶层 required 应包含所有 key
	req, ok := schema["required"].([]string)
	if !ok || len(req) != 2 {
		t.Errorf("顶层 required 期望长度 2，实际 %v", schema["required"])
	}
	// 嵌套 object 也应设置
	attrs := schema["properties"].(map[string]any)["attributes"].(map[string]any)
	ap2, ok := attrs["additionalProperties"].(bool)
	if !ok || ap2 {
		t.Errorf("嵌套 additionalProperties 期望 false，实际 %v", attrs["additionalProperties"])
	}
}

// TestStrictSchemaEnforce_数组内嵌对象 测试 array items 中的 object 也被处理
func TestStrictSchemaEnforce_数组内嵌对象(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	StrictSchemaEnforce(schema)

	arrayItems := schema["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)
	ap, ok := arrayItems["additionalProperties"].(bool)
	if !ok || ap {
		t.Errorf("数组内对象 additionalProperties 期望 false，实际 %v", arrayItems["additionalProperties"])
	}
}

// TestStrictSchemaEnforce_空Schema 测试空 Schema 不 panic
func TestStrictSchemaEnforce_空Schema(t *testing.T) {
	schema := map[string]any{"type": "string"}
	StrictSchemaEnforce(schema)
	if _, ok := schema["additionalProperties"]; ok {
		t.Error("非 object 类型不应设置 additionalProperties")
	}
}

// TestBuildResponseFormat_EntitySummary 测试 BuildResponseFormat 生成完整 Schema
func TestBuildResponseFormat_EntitySummary(t *testing.T) {
	result := BuildResponseFormat(EntitySummary{}, "cn")

	rf, ok := result["json_schema"].(map[string]any)
	if !ok {
		t.Fatal("缺少 json_schema 字段")
	}
	if rf["name"] != "EntitySummary" {
		t.Errorf("name 期望 EntitySummary，实际 %v", rf["name"])
	}
	schema, ok := rf["schema"].(map[string]any)
	if !ok {
		t.Fatal("缺少 schema 字段")
	}
	if schema["type"] != "object" {
		t.Errorf("schema type 期望 object，实际 %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		t.Errorf("schema properties 不应为空")
	}
	if _, ok := props["summary"]; !ok {
		t.Error("缺少 summary 属性")
	}
	if _, ok := props["attributes"]; !ok {
		t.Error("缺少 attributes 属性")
	}
	// strict 模式：object 应有 additionalProperties=false
	if ap, ok := schema["additionalProperties"].(bool); !ok || ap {
		t.Errorf("Schema additionalProperties 期望 false，实际 %v", schema["additionalProperties"])
	}
}

// TestBuildResponseFormat_EntityDuplication 测试 EntityDuplication
func TestBuildResponseFormat_EntityDuplication(t *testing.T) {
	result := BuildResponseFormat(EntityDuplication{}, "cn")
	rf := result["json_schema"].(map[string]any)
	if rf["name"] != "EntityDuplication" {
		t.Errorf("name 期望 EntityDuplication，实际 %v", rf["name"])
	}
}

// TestBuildResponseFormat_MergeRelations 测试 MergeRelations
func TestBuildResponseFormat_MergeRelations(t *testing.T) {
	result := BuildResponseFormat(MergeRelations{}, "cn")
	rf := result["json_schema"].(map[string]any)
	if rf["name"] != "MergeRelations" {
		t.Errorf("name 期望 MergeRelations，实际 %v", rf["name"])
	}
}

// TestBuildResponseFormat_RelevantFacts 测试 RelevantFacts
func TestBuildResponseFormat_RelevantFacts(t *testing.T) {
	result := BuildResponseFormat(RelevantFacts{}, "cn")
	rf := result["json_schema"].(map[string]any)
	if rf["name"] != "RelevantFacts" {
		t.Errorf("name 期望 RelevantFacts，实际 %v", rf["name"])
	}
}
