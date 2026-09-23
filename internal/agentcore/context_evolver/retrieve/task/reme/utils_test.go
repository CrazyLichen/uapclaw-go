package reme

import (
	"testing"
)

// TestParseJSONListResponse_JSONBlock 测试从 JSON 代码块解析列表
func TestParseJSONListResponse_JSONBlock(t *testing.T) {
	response := "Here is the ranking:\n```json\n{\"ranked_indices\": [2, 0, 4, 1, 3]}\n```\nDone."
	result := ParseJSONListResponse(response, "ranked_indices")
	if len(result) != 5 {
		t.Fatalf("期望 5 个结果，实际 %d", len(result))
	}
	expected := []int{2, 0, 4, 1, 3}
	for i, v := range expected {
		if result[i] != v {
			t.Errorf("result[%d] = %d, 期望 %d", i, result[i], v)
		}
	}
}

// TestParseJSONListResponse_PlainJSON 测试从纯 JSON 列表解析
func TestParseJSONListResponse_PlainJSON(t *testing.T) {
	response := "```json\n[5, 3, 1, 7, 9]\n```"
	result := ParseJSONListResponse(response, "ranked_indices")
	if len(result) != 5 {
		t.Fatalf("期望 5 个结果，实际 %d", len(result))
	}
	expected := []int{5, 3, 1, 7, 9}
	for i, v := range expected {
		if result[i] != v {
			t.Errorf("result[%d] = %d, 期望 %d", i, result[i], v)
		}
	}
}

// TestParseJSONListResponse_FallbackNumbers 测试回退到数字提取
func TestParseJSONListResponse_FallbackNumbers(t *testing.T) {
	response := "The relevant indices are 3, 1, and 4."
	result := ParseJSONListResponse(response, "ranked_indices")
	if len(result) != 3 {
		t.Fatalf("期望 3 个结果，实际 %d: %v", len(result), result)
	}
	expected := []int{3, 1, 4}
	for i, v := range expected {
		if result[i] != v {
			t.Errorf("result[%d] = %d, 期望 %d", i, result[i], v)
		}
	}
}

// TestParseJSONListResponse_InvalidJSON 测试无效 JSON 输入
func TestParseJSONListResponse_InvalidJSON(t *testing.T) {
	response := "This is just plain text without any JSON."
	result := ParseJSONListResponse(response, "ranked_indices")
	if len(result) != 0 {
		t.Fatalf("期望 0 个结果，实际 %d: %v", len(result), result)
	}
}

// TestParseJSONField_JSONBlock 测试从 JSON 代码块解析字段
func TestParseJSONField_JSONBlock(t *testing.T) {
	response := "Here is the result:\n```json\n{\"rewritten_context\": \"Some rewritten text here\"}\n```"
	result := ParseJSONField(response, "rewritten_context")
	if result != "Some rewritten text here" {
		t.Fatalf("期望 'Some rewritten text here'，实际 '%s'", result)
	}
}

// TestParseJSONField_PlainJSON 测试从纯 JSON 解析字段
func TestParseJSONField_PlainJSON(t *testing.T) {
	response := `{"rewritten_context": "Another rewritten text"}`
	result := ParseJSONField(response, "rewritten_context")
	if result != "Another rewritten text" {
		t.Fatalf("期望 'Another rewritten text'，实际 '%s'", result)
	}
}

// TestParseJSONField_NotFound 测试字段不存在
func TestParseJSONField_NotFound(t *testing.T) {
	response := "```json\n{\"other_field\": \"value\"}\n```"
	result := ParseJSONField(response, "rewritten_context")
	if result != "" {
		t.Fatalf("期望空字符串，实际 '%s'", result)
	}
}
