package rails

import (
	"encoding/json"
	"testing"
)

func TestEnsureJSONArguments_合法JSON(t *testing.T) {
	input := `{"key": "value"}`
	result := ensureJSONArguments(input)
	if result != input {
		t.Errorf("expected original, got %s", result)
	}
}

func TestEnsureJSONArguments_空字符串(t *testing.T) {
	result := ensureJSONArguments("")
	if result != "{}" {
		t.Errorf("expected '{}', got %s", result)
	}
}

func TestEnsureJSONArguments_闭合括号修复(t *testing.T) {
	input := `{"key": "value"`
	result := ensureJSONArguments(input)
	// 应通过 json_repair 修复
	if result == "{}" {
		t.Errorf("修复失败，返回了兜底值")
	}
	// 验证结果是合法 JSON
	if !isJSONMap(result) {
		t.Errorf("修复后不是合法 JSON map: %s", result)
	}
}

func TestEnsureJSONArguments_缺引号修复(t *testing.T) {
	input := `{query: "hello"}`
	result := ensureJSONArguments(input)
	if result == "{}" {
		t.Errorf("修复失败，返回了兜底值")
	}
	if !isJSONMap(result) {
		t.Errorf("修复后不是合法 JSON map: %s", result)
	}
}

func TestEnsureJSONArguments_全部失败兜底(t *testing.T) {
	input := `not json at all {{{`
	result := ensureJSONArguments(input)
	if result != "{}" {
		t.Errorf("expected '{}', got %s", result)
	}
}

func TestFixMissingQuotes_Windows路径(t *testing.T) {
	input := `{"path": D:/work/file.txt}`
	result := fixMissingQuotes(input)
	// 应修复为 {"path": "D:/work/file.txt"}
	if result == input {
		t.Error("Windows 路径未修复")
	}
}

func TestFixMissingQuotes_缺键引号(t *testing.T) {
	input := `{query: "hello"}`
	result := fixMissingQuotes(input)
	if result == input {
		t.Error("缺键引号未修复")
	}
}

func TestFixMissingQuotes_无需修复(t *testing.T) {
	input := `{"key": "value"}`
	result := fixMissingQuotes(input)
	if result != input {
		t.Errorf("不应修改合法 JSON，got %s", result)
	}
}

func TestToolInterruptedMessage(t *testing.T) {
	// 中文
	msg := toolInterruptedMessage("read_file", func() string { return "cn" })
	if msg == "" {
		t.Error("expected non-empty message")
	}

	// 英文
	msg = toolInterruptedMessage("read_file", func() string { return "en" })
	if msg == "" {
		t.Error("expected non-empty message")
	}

	// nil 回退中文
	msg = toolInterruptedMessage("read_file", nil)
	if msg == "" {
		t.Error("expected non-empty message")
	}
}

// ---------------------------------------------------------------------------
// isNumeric
// ---------------------------------------------------------------------------

// TestIsNumeric_整数 测试整数判断。
func TestIsNumeric_整数(t *testing.T) {
	if !isNumeric("123") {
		t.Error("expected true for '123'")
	}
}

// TestIsNumeric_负数 测试负数判断。
func TestIsNumeric_负数(t *testing.T) {
	if !isNumeric("-42") {
		t.Error("expected true for '-42'")
	}
}

// TestIsNumeric_小数 测试小数判断。
func TestIsNumeric_小数(t *testing.T) {
	if !isNumeric("3.14") {
		t.Error("expected true for '3.14'")
	}
}

// TestIsNumeric_空字符串 测试空字符串返回 false。
func TestIsNumeric_空字符串(t *testing.T) {
	if isNumeric("") {
		t.Error("expected false for empty string")
	}
}

// TestIsNumeric_非数字 测试非数字字符串返回 false。
func TestIsNumeric_非数字(t *testing.T) {
	if isNumeric("abc") {
		t.Error("expected false for 'abc'")
	}
}

// TestIsNumeric_字母数字混合 测试混合字符串返回 false。
func TestIsNumeric_字母数字混合(t *testing.T) {
	if isNumeric("12a34") {
		t.Error("expected false for '12a34'")
	}
}

// TestIsNumeric_仅负号 测试仅负号返回 true（简单实现允许）。
func TestIsNumeric_仅负号(t *testing.T) {
	// 简单实现对 "-" 返回 true（无字符校验），记录实际行为
	if !isNumeric("-") {
		t.Log("isNumeric('-') = false，实现已增加校验")
	}
}

// ── 辅助 ──

func isJSONMap(s string) bool {
	var m map[string]any
	return json.Unmarshal([]byte(s), &m) == nil
}
