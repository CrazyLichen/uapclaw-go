package single_agent

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestRepairToolArgumentsJSON_正常JSON(t *testing.T) {
	input := `{"key": "value"}`
	result := RepairToolArgumentsJSON(input)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if *result != input {
		t.Errorf("result = %q, want %q", *result, input)
	}
}

func TestRepairToolArgumentsJSON_缺失尾部大括号(t *testing.T) {
	input := `{"key": "value"`
	result := RepairToolArgumentsJSON(input)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	expected := `{"key": "value"}`
	if *result != expected {
		t.Errorf("result = %q, want %q", *result, expected)
	}
}

func TestRepairToolArgumentsJSON_缺失尾部中括号(t *testing.T) {
	input := `[1, 2, 3`
	result := RepairToolArgumentsJSON(input)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	expected := `[1, 2, 3]`
	if *result != expected {
		t.Errorf("result = %q, want %q", *result, expected)
	}
}

func TestRepairToolArgumentsJSON_嵌套缺失(t *testing.T) {
	input := `{"arr": [1, 2`
	result := RepairToolArgumentsJSON(input)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	expected := `{"arr": [1, 2]}`
	if *result != expected {
		t.Errorf("result = %q, want %q", *result, expected)
	}
}

func TestRepairToolArgumentsJSON_字符串内括号不计入(t *testing.T) {
	input := `{"text": "hello {world}"}`
	result := RepairToolArgumentsJSON(input)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if *result != input {
		t.Errorf("result = %q, want %q", *result, input)
	}
}

func TestRepairToolArgumentsJSON_转义引号(t *testing.T) {
	input := `{"text": "say \"hello\""}`
	result := RepairToolArgumentsJSON(input)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if *result != input {
		t.Errorf("result = %q, want %q", *result, input)
	}
}

func TestRepairToolArgumentsJSON_仍在字符串内(t *testing.T) {
	input := `{"key": "unterminated string`
	result := RepairToolArgumentsJSON(input)
	if result != nil {
		t.Errorf("未闭合字符串应返回 nil，实际 %q", *result)
	}
}

func TestRepairToolArgumentsJSON_空字符串(t *testing.T) {
	result := RepairToolArgumentsJSON("")
	if result != nil {
		t.Errorf("空字符串应返回 nil，实际 %q", *result)
	}
}

func TestRepairToolArgumentsJSON_不匹配的闭合(t *testing.T) {
	input := `{"key": "value"}]`
	result := RepairToolArgumentsJSON(input)
	if result != nil {
		t.Errorf("不匹配的闭合括号应返回 nil，实际 %q", *result)
	}
}

func TestParseToolArguments_正常解析(t *testing.T) {
	input := `{"city": "北京", "days": 3}`
	result, err := ParseToolArguments(input)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if result["city"] != "北京" {
		t.Errorf("city = %v, want 北京", result["city"])
	}
}

func TestParseToolArguments_修复后解析(t *testing.T) {
	input := `{"city": "北京"`
	result, err := ParseToolArguments(input)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if result["city"] != "北京" {
		t.Errorf("city = %v, want 北京", result["city"])
	}
}

func TestParseToolArguments_无法修复(t *testing.T) {
	input := `not json at all`
	_, err := ParseToolArguments(input)
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestParseToolArguments_空字符串(t *testing.T) {
	result, err := ParseToolArguments("")
	if err != nil {
		t.Fatalf("空字符串不应返回错误: %v", err)
	}
	if result != nil {
		t.Errorf("空字符串应返回 nil，实际 %v", result)
	}
}
