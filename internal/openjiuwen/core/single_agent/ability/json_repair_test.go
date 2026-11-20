package ability

import (
	"strings"
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

// 截断（模型输出到上限/流被切断）是"写长文件"场景最常见的参数失败形态：
// 报错必须带出 json 的语法诊断，否则只剩一坨原文，无法判断根因。
func TestParseToolArguments_截断参数带语法诊断(t *testing.T) {
	input := `{"file_path": "C:\\Users\\Administrator\\index.html", "content": "<!DOCTYPE html>`
	_, err := ParseToolArguments(input)
	if err == nil {
		t.Fatal("应返回错误")
	}
	msg := err.Error()
	if !strings.Contains(msg, "无效的工具调用参数 JSON") {
		t.Errorf("错误信息缺少前缀, 实际 %q", msg)
	}
	if !strings.Contains(msg, "unexpected end of JSON input") {
		t.Errorf("错误信息缺少 json 语法诊断, 实际 %q", msg)
	}
	if !strings.Contains(msg, "原始参数") {
		t.Errorf("错误信息缺少原始参数, 实际 %q", msg)
	}
}

// 非法转义同样是模型侧常见问题，诊断需指出具体位置。
func TestParseToolArguments_非法转义带语法诊断(t *testing.T) {
	input := `{"file_path": "C:\Users\a.html"}`
	_, err := ParseToolArguments(input)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !strings.Contains(err.Error(), "invalid character") {
		t.Errorf("错误信息缺少 json 语法诊断, 实际 %q", err.Error())
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
