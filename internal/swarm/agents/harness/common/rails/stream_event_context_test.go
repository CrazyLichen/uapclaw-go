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

// ── 辅助 ──

func isJSONMap(s string) bool {
	var m map[string]any
	return json.Unmarshal([]byte(s), &m) == nil
}
