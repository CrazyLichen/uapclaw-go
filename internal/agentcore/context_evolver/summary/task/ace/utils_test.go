package ace

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSafeJSONLoads_直接JSON(t *testing.T) {
	input := `{"reasoning": "test", "operations": []}`
	got, err := SafeJSONLoads(input)
	if err != nil {
		t.Fatalf("SafeJSONLoads 失败: %v", err)
	}
	if got["reasoning"] != "test" {
		t.Errorf("reasoning = %v, want %q", got["reasoning"], "test")
	}
}

func TestSafeJSONLoads_MarkdownCodeBlock(t *testing.T) {
	input := "```json\n{\"reasoning\": \"from block\"}\n```"
	got, err := SafeJSONLoads(input)
	if err != nil {
		t.Fatalf("SafeJSONLoads 失败: %v", err)
	}
	if got["reasoning"] != "from block" {
		t.Errorf("reasoning = %v, want %q", got["reasoning"], "from block")
	}
}

func TestSafeJSONLoads_MarkdownCodeBlock无语言标记(t *testing.T) {
	input := "```\n{\"reasoning\": \"no lang\"}\n```"
	got, err := SafeJSONLoads(input)
	if err != nil {
		t.Fatalf("SafeJSONLoads 失败: %v", err)
	}
	if got["reasoning"] != "no lang" {
		t.Errorf("reasoning = %v, want %q", got["reasoning"], "no lang")
	}
}

func TestSafeJSONLoads_任意JSON对象(t *testing.T) {
	input := "Some text before {\"key\": \"value\"} some text after"
	got, err := SafeJSONLoads(input)
	if err != nil {
		t.Fatalf("SafeJSONLoads 失败: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("key = %v, want %q", got["key"], "value")
	}
}

func TestSafeJSONLoads_解析失败(t *testing.T) {
	input := "no json here at all"
	_, err := SafeJSONLoads(input)
	if err == nil {
		t.Error("期望返回错误，实际返回 nil")
	}
}

func TestSafeJSONLoads_嵌套JSON(t *testing.T) {
	input := `{"reasoning": "test", "operations": [{"type": "ADD", "section": "s1", "content": "c1"}]}`
	got, err := SafeJSONLoads(input)
	if err != nil {
		t.Fatalf("SafeJSONLoads 失败: %v", err)
	}
	ops, ok := got["operations"].([]any)
	if !ok || len(ops) != 1 {
		t.Fatalf("operations 解析失败")
	}
}

func TestSafeJSONLoads_空对象(t *testing.T) {
	input := "{}"
	got, err := SafeJSONLoads(input)
	if err != nil {
		t.Fatalf("SafeJSONLoads 失败: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("空 JSON 对象应有 0 个字段，实际 %d", len(got))
	}
}
