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

// TestRecursiveReplace_删除字段 测试 toKey 为空时删除 fromKey
func TestRecursiveReplace_删除字段(t *testing.T) {
	data := map[string]any{
		"title": "MyTitle",
		"type":  "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":  "string",
				"title": "NameTitle",
			},
		},
	}
	replaced := recursiveReplace(data, nil, "title", "")
	if !replaced {
		t.Error("应返回 true 表示执行了替换")
	}
	if _, ok := data["title"]; ok {
		t.Error("顶层 title 应被删除")
	}
	props := data["properties"].(map[string]any)
	nameProp := props["name"].(map[string]any)
	if _, ok := nameProp["title"]; ok {
		t.Error("嵌套 title 应被删除")
	}
}

// TestRecursiveReplace_替换字段 测试 fromKey → toKey 替换
func TestRecursiveReplace_替换字段(t *testing.T) {
	lookup := map[string]string{
		"#/$defs/Foo": "Foo",
	}
	data := map[string]any{
		"$ref": "#/$defs/Foo",
	}
	replaced := recursiveReplace(data, lookup, "$ref", "type")
	if !replaced {
		t.Error("应返回 true")
	}
	if data["type"] != "Foo" {
		t.Errorf("type 期望 Foo，实际 %v", data["type"])
	}
	if _, ok := data["$ref"]; ok {
		t.Error("$ref 应被删除")
	}
}

// TestRecursiveReplace_lookup未命中 测试 lookup 未命中时使用原值
func TestRecursiveReplace_lookup未命中(t *testing.T) {
	lookup := map[string]string{}
	data := map[string]any{
		"$ref": "#/$defs/Bar",
	}
	replaced := recursiveReplace(data, lookup, "$ref", "type")
	if !replaced {
		t.Error("应返回 true")
	}
	if data["type"] != "#/$defs/Bar" {
		t.Errorf("type 期望 #/$defs/Bar，实际 %v", data["type"])
	}
}

// TestRecursiveReplace_fromKey等于toKey 测试 fromKey == toKey 时不删除
func TestRecursiveReplace_fromKey等于toKey(t *testing.T) {
	lookup := map[string]string{
		"old": "new",
	}
	data := map[string]any{
		"x": "old",
	}
	replaced := recursiveReplace(data, lookup, "x", "x")
	if !replaced {
		t.Error("应返回 true")
	}
	if data["x"] != "new" {
		t.Errorf("x 期望 new，实际 %v", data["x"])
	}
}

// TestRecursiveReplace_数组内替换 测试 []any 中的递归替换
func TestRecursiveReplace_数组内替换(t *testing.T) {
	data := map[string]any{
		"items": []any{
			map[string]any{"title": "A"},
			map[string]any{"title": "B"},
		},
	}
	replaced := recursiveReplace(data, nil, "title", "")
	if !replaced {
		t.Error("应返回 true")
	}
	items := data["items"].([]any)
	for _, item := range items {
		m := item.(map[string]any)
		if _, ok := m["title"]; ok {
			t.Error("数组内 title 应被删除")
		}
	}
}

// TestRecursiveReplace_无匹配 测试无匹配时返回 false
func TestRecursiveReplace_无匹配(t *testing.T) {
	data := map[string]any{
		"type": "string",
	}
	replaced := recursiveReplace(data, nil, "title", "")
	if replaced {
		t.Error("无匹配时应返回 false")
	}
}
