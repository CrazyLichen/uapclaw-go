package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// newResponsesTestClient 构造指向 httptest 服务端的 Responses 模式客户端。
func newResponsesTestClient(t *testing.T, serverURL string) *OpenAIModelClient {
	t.Helper()
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("gpt-5"), llmschema.WithTemperatureValue(0.5))
	clientConfig := &llmschema.ModelClientConfig{
		ClientProvider: "OpenAI",
		APIBase:        serverURL,
		APIKey:         "sk-test",
		APIMode:        llmschema.APIModeResponses,
	}
	c, err := NewOpenAIModelClient(modelConfig, clientConfig)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestUsesResponsesAPI 分流判定。
func TestUsesResponsesAPI(t *testing.T) {
	modelConfig := llmschema.NewModelRequestConfig(llmschema.WithModelName("m"))
	for _, tc := range []struct {
		apiMode llmschema.APIMode
		want    bool
	}{
		{llmschema.APIModeResponses, true},
		{llmschema.APIMode(""), false},
		{llmschema.APIModeChatCompletions, false},
		{llmschema.APIModeAnthropicMessages, false},
	} {
		c, err := NewOpenAIModelClient(modelConfig, &llmschema.ModelClientConfig{
			ClientProvider: "OpenAI", APIMode: tc.apiMode, APIKey: "sk-test",
			APIBase: "https://api.openai.com/v1",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := c.usesResponsesAPI(); got != tc.want {
			t.Fatalf("api_mode=%q got=%v want=%v", tc.apiMode, got, tc.want)
		}
	}
}

// TestBuildResponsesRequestBody 开关弹出与 model_dump 余量。
func TestBuildResponsesRequestBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := newResponsesTestClient(t, srv.URL)

	params := &model_clients.InvokeParams{
		Temperature: floatPtr(0.7),
		TopP:        floatPtr(0.9),
		MaxTokens:   newIntPtr(64),

		Extra: map[string]any{
			// 开关关闭：temperature/top_p 不进 body
			"send_sampling_params":   false,
			"send_max_output_tokens": false,
			// 弹出参数
			"reasoning":                           map[string]any{"effort": "high"},
			"tool_choice":                         "auto",
			"parallel_tool_calls":                 true,
			"include_reasoning_encrypted_content": true,
			// 无关 kwargs：应留在 Extra
			"custom_key": "v",
		},
	}
	body, err := c.buildResponsesRequestBody([]map[string]any{{"role": "user", "content": "hi"}}, params)
	if err != nil {
		t.Fatal(err)
	}
	if body["model"] != "gpt-5" {
		t.Fatalf("model: %v", body["model"])
	}
	for _, absent := range []string{"temperature", "top_p", "max_output_tokens"} {
		if _, ok := body[absent]; ok {
			t.Fatalf("开关关闭时 %s 不应进 body", absent)
		}
	}
	if !reflectDeepEqual(body["reasoning"], map[string]any{"effort": "high"}) {
		t.Fatalf("reasoning: %v", body["reasoning"])
	}
	if !reflectDeepEqual(body["include"], []string{"reasoning.encrypted_content"}) {
		t.Fatalf("include: %v", body["include"])
	}
	// 无关 kwargs 留在 Extra（Python kwargs 语义）
	if params.Extra["custom_key"] != "v" {
		t.Fatal("无关 kwargs 应保留")
	}
	// 弹出键已删除
	if _, ok := params.Extra["reasoning"]; ok {
		t.Fatal("reasoning 应已弹出")
	}

	// 开关开启：采样参数进 body
	params2 := &model_clients.InvokeParams{
		Temperature: floatPtr(0.7),
		Extra:       map[string]any{"send_sampling_params": true, "send_max_output_tokens": true},
	}
	body2, err := c.buildResponsesRequestBody([]map[string]any{{"role": "user", "content": "hi"}}, params2)
	if err != nil {
		t.Fatal(err)
	}
	if body2["temperature"] != 0.7 {
		t.Fatalf("开关开启 temperature: %v", body2["temperature"])
	}
	// max_tokens 双侧均缺省 → 不进 body（Python: configured_max_tokens 为 None）
	if _, ok := body2["max_output_tokens"]; ok {
		t.Fatalf("max_output_tokens 双侧缺省不应进 body: %v", body2["max_output_tokens"])
	}
}

// TestInvokeResponsesAPI httptest SSE → 非流式聚合。
func TestInvokeResponsesAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("auth: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"type":"response.output_text.delta","delta":"hey"}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"type":"response.completed","response":{"id":"r1","model":"gpt-5","usage":{"input_tokens":2,"output_tokens":4,"output_tokens_details":{"reasoning_tokens":3}}}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "data: [DONE]")
		fmt.Fprintln(w)
	}))
	defer srv.Close()
	c := newResponsesTestClient(t, srv.URL)

	msg, err := c.Invoke(context.Background(), model_clients.NewTextMessagesParam("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content.String() != "hey" {
		t.Fatalf("content: %q", msg.Content.String())
	}
	if msg.ResponseID != "r1" {
		t.Fatalf("response_id: %q", msg.ResponseID)
	}
	if msg.UsageMetadata == nil || msg.UsageMetadata.ReasoningTokens == nil || *msg.UsageMetadata.ReasoningTokens != 3 {
		t.Fatalf("reasoning_tokens: %+v", msg.UsageMetadata)
	}
}

// TestInvokeResponsesAPI_HTTPError 500 → MODEL_CALL_FAILED 包装。
func TestInvokeResponsesAPI_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"message":"kaboom"}}`)
	}))
	defer srv.Close()
	c := newResponsesTestClient(t, srv.URL)

	_, err := c.Invoke(context.Background(), model_clients.NewTextMessagesParam("hi"))
	if err == nil {
		t.Fatal("应报错")
	}
	if !isModelCallFailed(err) {
		t.Fatalf("应包装为 MODEL_CALL_FAILED: %v", err)
	}
	if !jsonContains(err.Error(), "Responses API invoke error") {
		t.Fatalf("文案: %v", err)
	}
}

// TestStreamResponsesAPI_FailedEvent 流内 failed 事件 → StreamError chunk。
func TestStreamResponsesAPI_FailedEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"type":"response.output_text.delta","delta":"x"}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"type":"response.failed","message":"inner boom"}`)
		fmt.Fprintln(w)
	}))
	defer srv.Close()
	c := newResponsesTestClient(t, srv.URL)

	ch, err := c.Stream(context.Background(), model_clients.NewTextMessagesParam("hi"))
	if err != nil {
		t.Fatal(err)
	}
	var sawErr error
	var first string
	for chunk := range ch {
		if chunk.StreamError != nil {
			sawErr = chunk.StreamError
		} else if chunk.Content.Text() != "" {
			first = chunk.Content.Text()
		}
	}
	if first != "x" {
		t.Fatalf("首帧: %q", first)
	}
	if sawErr == nil || !jsonContains(sawErr.Error(), "Responses API stream error") {
		t.Fatalf("StreamError: %v", sawErr)
	}
}

// TestEmitResponsesError error_message 仅在无消息时填充。
func TestEmitResponsesError(t *testing.T) {
	c := newResponsesTestClient(t, "http://127.0.0.1:1")
	// 有消息的错误：error_message 不填充
	plain := fmt.Errorf("plain failure")
	data := llmCallEventDataOf(map[string]any{"model": "m"}, "OpenAI", false, plain, "")
	if data.Extra != nil {
		t.Fatal("有消息时 error_message 不应填充")
	}
	// 空白消息：填充 detail
	blank := fmt.Errorf("  ")
	data2 := llmCallEventDataOf(map[string]any{"model": "m"}, "OpenAI", false, blank, blank.Error())
	if data2.Extra == nil {
		t.Fatal("空白消息应填充 error_message")
	}
	_ = c
}

// ──────────────────────────── 测试辅助 ────────────────────────────

func floatPtr(v float64) *float64 { return &v }

func reflectDeepEqual(a, b any) bool { return jsonEqual(a, b) }

// jsonEqual JSON 序列化后比较（避免引入 reflect 深比较的 map 顺序差异）。
func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// jsonContains 断言字符串包含子串。
func jsonContains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// isModelCallFailed 判断是否 MODEL_CALL_FAILED 包装。
func isModelCallFailed(err error) bool {
	if err == nil {
		return false
	}
	// exception.BaseError 的 StatusCode 判断走文本兜底
	return jsonContains(err.Error(), "MODEL_CALL_FAILED") ||
		jsonContains(fmt.Sprintf("%T", err), "BaseError")
}

// newIntPtr 测试辅助 int 指针（避开既有 intPtr 命名）。
func newIntPtr(v int) *int { return &v }
