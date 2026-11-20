package responses

import (
	"reflect"
	"strings"
	"testing"
)

// TestBuildRequestBody 验证 body 基础字段与非 None 门控。
// Python: responses_utils.py:42 build_request_body
func TestBuildRequestBody(t *testing.T) {
	body, err := BuildRequestBody(BuildRequestBodyInput{
		Model:                            "gpt-5",
		Messages:                         "hi",
		IncludeReasoningEncryptedContent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["model"] != "gpt-5" || body["store"] != false || body["instructions"] != "" {
		t.Fatalf("基础字段: %v", body)
	}
	input, ok := body["input"].([]map[string]any)
	if !ok || len(input) != 1 || input[0]["role"] != "user" {
		t.Fatalf("str→单条 user: %v", body["input"])
	}
	if !reflect.DeepEqual(body["include"], []string{"reasoning.encrypted_content"}) {
		t.Fatalf("include 缺失: %v", body["include"])
	}
	for _, absent := range []string{"temperature", "top_p", "max_output_tokens", "stop", "reasoning", "tools"} {
		if _, ok := body[absent]; ok {
			t.Fatalf("None 参数 %s 不应出现在 body", absent)
		}
	}
}

// TestBuildRequestBody_空模型报错 model 为空抛 ResponsesError。
func TestBuildRequestBody_空模型报错(t *testing.T) {
	_, err := BuildRequestBody(BuildRequestBodyInput{Messages: "hi"})
	if err == nil {
		t.Fatal("应报错")
	}
	var re *ResponsesError
	if !errAs(err, &re) {
		t.Fatalf("错误类型: %T", err)
	}
}

// TestBuildRequestBody_参数进body 非空参数与工具字段。
func TestBuildRequestBody_参数进body(t *testing.T) {
	body, err := BuildRequestBody(BuildRequestBodyInput{
		Model:             "m",
		Messages:          []map[string]any{{"role": "user", "content": "hi"}},
		Temperature:       0.3,
		TopP:              0.9,
		MaxTokens:         128,
		Stop:              "END",
		Reasoning:         map[string]any{"effort": "high"},
		Tools:             []map[string]any{{"type": "function", "name": "f", "parameters": map[string]any{}}},
		ToolChoice:        "auto",
		ParallelToolCalls: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["temperature"] != 0.3 || body["top_p"] != 0.9 || body["max_output_tokens"] != 128 || body["stop"] != "END" {
		t.Fatalf("采样参数: %v", body)
	}
	if !reflect.DeepEqual(body["reasoning"], map[string]any{"effort": "high"}) {
		t.Fatalf("reasoning: %v", body["reasoning"])
	}
	if body["parallel_tool_calls"] != false || body["tool_choice"] != "auto" {
		t.Fatalf("工具选项: %v", body)
	}
	if _, ok := body["tools"]; !ok {
		t.Fatal("tools 缺失")
	}
}

// TestBuildRequestBody_ExtraBody拍平 extra_body 一层拍平到根。
func TestBuildRequestBody_ExtraBody拍平(t *testing.T) {
	body, err := BuildRequestBody(BuildRequestBodyInput{
		Model:     "m",
		Messages:  "hi",
		ExtraBody: map[string]any{"thinking": map[string]any{"type": "enabled"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body["thinking"], map[string]any{"type": "enabled"}) {
		t.Fatalf("extra_body 未拍平: %v", body["thinking"])
	}
	if _, ok := body["extra_body"]; ok {
		t.Fatal("extra_body 键不应保留在根")
	}
}

// TestExpandNestedExtraBody 外层键优先、nil → 空。
func TestExpandNestedExtraBody(t *testing.T) {
	got := ExpandNestedExtraBody(map[string]any{
		"extra_body": map[string]any{"thinking": map[string]any{"a": 1}},
		"thinking":   map[string]any{"a": 2},
	})
	if !reflect.DeepEqual(got["thinking"], map[string]any{"a": 2}) {
		t.Fatalf("外层键应优先: %v", got)
	}
	if len(ExpandNestedExtraBody(nil)) != 0 {
		t.Fatal("nil → 空")
	}
}

// TestBuildHeaders 鉴权与 session 双头。
func TestBuildHeaders(t *testing.T) {
	h, err := BuildHeaders("tok", "sess-1", map[string]string{"X-A": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if h["Authorization"] != "Bearer tok" || h["session_id"] != "sess-1" || h["x-client-request-id"] != "sess-1" {
		t.Fatalf("headers: %v", h)
	}
	if h["Accept"] != "application/json" || h["Content-Type"] != "application/json" {
		t.Fatalf("Accept/Content-Type: %v", h)
	}
	if h["X-A"] != "b" {
		t.Fatalf("自定义头: %v", h)
	}
	if _, err := BuildHeaders("", "", nil); err == nil {
		t.Fatal("空 token 应报错")
	}
}

// TestConvertMessages 四分支与 function_call 回放。
// Python: responses_utils.py:135 convert_messages
func TestConvertMessages(t *testing.T) {
	msgs := []map[string]any{
		{"role": "system", "content": "sys1"},
		{"role": "developer", "content": "dev1"},
		{"role": "user", "content": "u1"},
		{"role": "assistant", "content": "a1", "tool_calls": []any{
			map[string]any{
				"id": "call_1", "type": "function",
				"function":         map[string]any{"name": "f1", "arguments": "{}"},
				"response_item_id": "item_1",
			},
		}},
		{"role": "tool", "content": "res", "tool_call_id": "call_1"},
	}
	instructions, inputs, err := ConvertMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if instructions != "sys1\n\ndev1" {
		t.Fatalf("instructions: %q", instructions)
	}
	// assistant content item + function_call + tool output = 4 items（system/developer 走 instructions）
	if len(inputs) != 4 {
		t.Fatalf("item 数: %d (%v)", len(inputs), inputs)
	}
	if inputs[0]["role"] != "user" {
		t.Fatalf("user item: %v", inputs[0])
	}
	ac, _ := inputs[1]["content"].([]map[string]any)
	if inputs[1]["role"] != "assistant" || ac[0]["type"] != "output_text" || ac[0]["text"] != "a1" {
		t.Fatalf("assistant item: %v", inputs[1])
	}
	fc := inputs[2]
	if fc["type"] != "function_call" || fc["id"] != "item_1" || fc["call_id"] != "call_1" || fc["name"] != "f1" {
		t.Fatalf("function_call: %v", fc)
	}
	out := inputs[3]
	if out["type"] != "function_call_output" || out["call_id"] != "call_1" || out["output"] != "res" {
		t.Fatalf("function_call_output: %v", out)
	}
}

// TestConvertMessages_无ResponseItemID 兜底 id 与 uuid 兜底。
func TestConvertMessages_无ResponseItemID(t *testing.T) {
	_, inputs, err := ConvertMessages([]map[string]any{{
		"role": "assistant",
		"tool_calls": []any{map[string]any{
			"id": "call_9", "type": "function",
			"function": map[string]any{"name": "f", "arguments": "{}"},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if inputs[0]["id"] != "call_9" || inputs[0]["call_id"] != "call_9" {
		t.Fatalf("id 兜底: %v", inputs[0])
	}
	// 完全无 id → uuid 兜底（call_ 前缀 + hex）
	_, inputs2, err := ConvertMessages([]map[string]any{{
		"role": "assistant",
		"tool_calls": []any{map[string]any{
			"function": map[string]any{"name": "f", "arguments": "{}"},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := inputs2[0]["call_id"].(string)
	if !strings.HasPrefix(id, "call_") || len(id) != len("call_")+32 {
		t.Fatalf("uuid 兜底形态: %q", id)
	}
}

// TestConvertMessages_tool无CallID丢弃 与空消息报错。
func TestConvertMessages_tool无CallID丢弃(t *testing.T) {
	_, inputs, err := ConvertMessages([]map[string]any{
		{"role": "tool", "content": "res"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("无 call_id 的 tool 消息应丢弃: %v", inputs)
	}
	if _, _, err := ConvertMessages([]map[string]any{}); err == nil {
		t.Fatal("空消息应报错")
	}
}

// TestConvertTools 三形态。
func TestConvertTools(t *testing.T) {
	got := ConvertTools([]map[string]any{
		{"type": "function", "name": "f1", "description": "d1", "parameters": map[string]any{"type": "object"}},
		{"function": map[string]any{"name": "f2", "description": "d2", "parameters": map[string]any{}}},
		{"type": "function", "name": "f3", "description": "", "parameters": nil},
	})
	if len(got) != 3 {
		t.Fatalf("数量: %d", len(got))
	}
	if got[0]["name"] != "f1" {
		t.Fatalf("扁平形态: %v", got[0])
	}
	if got[1]["name"] != "f2" {
		t.Fatalf("嵌套形态: %v", got[1])
	}
	if got[2]["name"] != "f3" {
		t.Fatalf("扁平无描述形态: %v", got[2])
	}
	// parameters 兜底空 object
	p := got[2]["parameters"].(map[string]any)
	if p["type"] != "object" {
		t.Fatalf("parameters 兜底: %v", p)
	}
	if len(ConvertTools(nil)) != 0 {
		t.Fatal("nil → 空")
	}
}

// errAs 类型断言辅助（兼容 go1.22 errors.As）。
func errAs(err error, target any) bool {
	if re, ok := err.(*ResponsesError); ok {
		if p, ok := target.(**ResponsesError); ok {
			*p = re
			return true
		}
	}
	return false
}
