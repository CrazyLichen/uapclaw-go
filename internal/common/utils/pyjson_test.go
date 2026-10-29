package utils

import (
	"encoding/json"
	"testing"
)

// TestMarshalNoEscape HTML 字符不转义 + 非 ASCII 原样
func TestMarshalNoEscape(t *testing.T) {
	data := map[string]any{
		"html":  "<bash-stdout>content</bash-stdout>",
		"cjk":   "中文内容",
		"amp":   "a&b",
		"quote": "he said \"hi\"",
	}
	out, err := MarshalNoEscape(data)
	if err != nil {
		t.Fatalf("MarshalNoEscape: %v", err)
	}
	s := string(out)
	// HTML 字符原样（json.Marshal 默认会转义为 \u003c 等）
	if !json.Valid(out) {
		t.Errorf("output not valid JSON: %s", s)
	}
	for _, want := range []string{"<bash-stdout>", "</bash-stdout>", "中文内容", "a&b", `\"hi\"`} {
		if !contains(s, want) {
			t.Errorf("output should contain %q, got %s", want, s)
		}
	}
	for _, banned := range []string{`\u003c`, `\u003e`, `\u0026`, `\u4e2d`} {
		if contains(s, banned) {
			t.Errorf("output should not contain escape %q, got %s", banned, s)
		}
	}
	// 无尾换行（对齐 json.dumps）
	if s[len(s)-1] == '\n' {
		t.Errorf("output should not have trailing newline")
	}
}

// TestMarshalIndentNoEscape 缩进 + 不转义 + 无尾换行
func TestMarshalIndentNoEscape(t *testing.T) {
	data := map[string]any{"key": "<val>", "n": 1}
	out, err := MarshalIndentNoEscape(data)
	if err != nil {
		t.Fatalf("MarshalIndentNoEscape: %v", err)
	}
	s := string(out)
	if !json.Valid(out) {
		t.Errorf("output not valid JSON: %s", s)
	}
	if !contains(s, "<val>") {
		t.Errorf("output should contain raw <val>, got %s", s)
	}
	if !contains(s, "\n  \"key\"") {
		t.Errorf("output should be 2-space indented, got %s", s)
	}
	if s[len(s)-1] == '\n' {
		t.Errorf("output should not have trailing newline")
	}
}

// TestMarshalNoEscape_错误传播 不可序列化输入（channel）返回错误
func TestMarshalNoEscape_错误传播(t *testing.T) {
	_, err := MarshalNoEscape(make(chan int))
	if err == nil {
		t.Error("channel should fail to marshal")
	}
	_, err = MarshalIndentNoEscape(make(chan int))
	if err == nil {
		t.Error("channel should fail to marshal (indent)")
	}
}

// contains 子串判断（避免引入 testify 断言风格混用）。
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

// indexOf 朴素子串查找。
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
