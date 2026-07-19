package tool_call

import "testing"

// TestParseJSON_无Header 测试不带 header 的 JSON 解析
func TestParseJSON_无Header(t *testing.T) {
	output := `Some text before {"key": "value", "num": 42} some text after`
	result := ParseJSON(output)
	if result["key"] != "value" {
		t.Errorf("key = %v, want value", result["key"])
	}
	if num, ok := result["num"].(float64); !ok || num != 42 {
		t.Errorf("num = %v, want 42", result["num"])
	}
}

// TestParseJSON_带Header 测试带 header 的 JSON 解析
func TestParseJSON_带Header(t *testing.T) {
	output := `blah blah {"description": {"name": "test"}} more blah`
	result := ParseJSON(output, "description")
	inner, ok := result["description"].(map[string]any)
	if !ok {
		t.Fatalf("description type = %T, want map[string]any", result["description"])
	}
	if inner["name"] != "test" {
		t.Errorf("name = %v, want test", inner["name"])
	}
}

// TestParseJSON_带Header换行 测试带换行的 header 查找
func TestParseJSON_带Header换行(t *testing.T) {
	output := `blah blah {
"description": {"name": "test"}} more blah`
	result := ParseJSON(output, "description")
	inner, ok := result["description"].(map[string]any)
	if !ok {
		t.Fatalf("description type = %T, want map[string]any", result["description"])
	}
	if inner["name"] != "test" {
		t.Errorf("name = %v, want test", inner["name"])
	}
}

// TestParseJSON_无JSON 测试无 JSON 内容
func TestParseJSON_无JSON(t *testing.T) {
	output := "no json here"
	result := ParseJSON(output)
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

// TestParseJSON_单引号修复 测试单引号 JSON 修复
func TestParseJSON_单引号修复(t *testing.T) {
	output := `{'key': 'value'}`
	result := ParseJSON(output)
	if result["key"] != "value" {
		t.Errorf("key = %v, want value", result["key"])
	}
}

// TestFormatPromptLlama 测试 Llama 提示词格式化
func TestFormatPromptLlama(t *testing.T) {
	result := FormatPromptLlama("sys", "user")
	if result != "sysuser" {
		t.Errorf("got %q, want %q", result, "sysuser")
	}
}

// TestFormatPromptLlama_空SystemPrompt 测试空系统提示词
func TestFormatPromptLlama_空SystemPrompt(t *testing.T) {
	result := FormatPromptLlama("", "user prompt")
	if result != "user prompt" {
		t.Errorf("got %q, want %q", result, "user prompt")
	}
}
