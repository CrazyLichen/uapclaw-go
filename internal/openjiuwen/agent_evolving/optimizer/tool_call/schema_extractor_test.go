package tool_call

import "testing"

// TestExtractSchema_嵌套字典 测试递归处理嵌套字典
func TestExtractSchema_嵌套字典(t *testing.T) {
	input := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "名称",
			},
		},
		"required": []any{"name"},
	}
	result := ExtractSchema(input)
	// 顶层 type 应变为空字符串
	if result["type"] != "" {
		t.Errorf("type = %v, want empty string", result["type"])
	}
	// required 列表应保留
	if req, ok := result["required"].([]any); !ok || len(req) != 1 {
		t.Errorf("required = %v, want [name]", result["required"])
	}
	// properties 应递归处理
	props, ok := result["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T", result["properties"])
	}
	nameProp, ok := props["name"].(map[string]any)
	if !ok {
		t.Fatalf("name type = %T", props["name"])
	}
	if nameProp["type"] != "" {
		t.Errorf("name.type = %v, want empty string", nameProp["type"])
	}
	if nameProp["description"] != "" {
		t.Errorf("name.description = %v, want empty string", nameProp["description"])
	}
}

// TestExtractSchema_空字典 测试空输入
func TestExtractSchema_空字典(t *testing.T) {
	result := ExtractSchema(map[string]any{})
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

// TestExtractSchema_深层嵌套 测试多层嵌套
func TestExtractSchema_深层嵌套(t *testing.T) {
	input := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"level3": "deep_value",
			},
		},
	}
	result := ExtractSchema(input)
	l1 := result["level1"].(map[string]any)
	l2 := l1["level2"].(map[string]any)
	if l2["level3"] != "" {
		t.Errorf("level3 = %v, want empty string", l2["level3"])
	}
}

// TestExtractSchemaFromJSON_从字符串解析 测试从 JSON 字符串提取
func TestExtractSchemaFromJSON_从字符串解析(t *testing.T) {
	jsonStr := `{"type": "object", "properties": {"id": {"type": "number"}}}`
	result := ExtractSchemaFromJSON(jsonStr)
	if result["type"] != "" {
		t.Errorf("type = %v, want empty string", result["type"])
	}
}

// TestExtractSchemaFromJSON_无效JSON 测试无效 JSON
func TestExtractSchemaFromJSON_无效JSON(t *testing.T) {
	result := ExtractSchemaFromJSON("not json")
	if len(result) != 0 {
		t.Errorf("expected empty map for invalid JSON, got %v", result)
	}
}
