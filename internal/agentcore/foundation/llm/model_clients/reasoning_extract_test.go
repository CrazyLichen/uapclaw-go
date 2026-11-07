package model_clients

import "testing"

// TestExtractReasoningContent_四级回退 对齐 openai_model_client.py:2751-2763 _extract_reasoning_content。
// 顺序：reasoning_details[0].text → reasoning_content → reasoning（OpenRouter）→ reasoning_token_text
func TestExtractReasoningContent_四级回退(t *testing.T) {
	cases := []struct {
		name   string
		fields map[string]any
		want   string
	}{
		{"reasoning_details[0].text", map[string]any{
			"reasoning_details": []any{map[string]any{"text": "think"}}}, "think"},
		{"reasoning_content", map[string]any{"reasoning_content": "a"}, "a"},
		{"reasoning(OpenRouter)", map[string]any{"reasoning": "b"}, "b"},
		{"reasoning_token_text", map[string]any{"reasoning_token_text": "c"}, "c"},
		{"优先级 details>content", map[string]any{
			"reasoning_details": []any{map[string]any{"text": "first"}},
			"reasoning_content": "second"}, "first"},
		{"优先级 content>reasoning", map[string]any{
			"reasoning_content": "second", "reasoning": "third"}, "second"},
		{"空串跳过走下一级", map[string]any{"reasoning_content": "", "reasoning": "b"}, "b"},
		{"全缺", map[string]any{}, ""},
	}
	for _, tc := range cases {
		if got := ExtractReasoningContent(tc.fields); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

// TestExtractReasoningContent_明细非法形态 details 非列表/首元素非 map/无 text 时回落。
func TestExtractReasoningContent_明细非法形态(t *testing.T) {
	cases := []struct {
		name   string
		fields map[string]any
		want   string
	}{
		{"details非列表", map[string]any{"reasoning_details": "not-a-list", "reasoning": "b"}, "b"},
		{"details空列表", map[string]any{"reasoning_details": []any{}, "reasoning": "b"}, "b"},
		{"首元素非map", map[string]any{"reasoning_details": []any{"str"}, "reasoning": "b"}, "b"},
		{"text非字符串", map[string]any{"reasoning_details": []any{map[string]any{"text": 1}}, "reasoning": "b"}, "b"},
		{"text空串", map[string]any{"reasoning_details": []any{map[string]any{"text": ""}}, "reasoning": "b"}, "b"},
	}
	for _, tc := range cases {
		if got := ExtractReasoningContent(tc.fields); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}
