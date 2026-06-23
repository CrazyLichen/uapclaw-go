package siliconflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestClientConfig 创建测试用的客户端配置
func newTestClientConfig(provider, apiKey, apiBase string) *llmschema.ModelClientConfig {
	return llmschema.NewModelClientConfig(provider, apiKey, apiBase, llmschema.WithVerifySSL(false))
}

// newTestModelConfig 创建测试用的模型请求配置
func newTestModelConfig() *llmschema.ModelRequestConfig {
	return llmschema.NewModelRequestConfig(llmschema.WithModelName("Qwen/Qwen2.5-7B-Instruct"))
}

// decodeRequestBody 从 HTTP 请求解码 JSON 请求体
func decodeRequestBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("读取请求体失败: %v", err)
	}
	var reqBody map[string]any
	if err := json.Unmarshal(body, &reqBody); err != nil {
		t.Fatalf("解析请求体失败: %v", err)
	}
	return reqBody
}

// mockCompletionResponse 构造 OpenAI Chat Completion 响应 JSON
func mockCompletionResponse(content string) string {
	return fmt.Sprintf(`{"id":"chatcmpl-123","object":"chat.completion","created":1234567890,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, content)
}

// mockCompletionResponseWithReasoning 构造含 reasoning_content 的响应
func mockCompletionResponseWithReasoning(content, reasoningContent string) string {
	return fmt.Sprintf(`{"id":"chatcmpl-123","object":"chat.completion","created":1234567890,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"message":{"role":"assistant","content":%q,"reasoning_content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, content, reasoningContent)
}

// ──────────────────────────── NewSiliconFlowModelClient 测试 ────────────────────────────

func TestNewSiliconFlowModelClient_ValidConfig(t *testing.T) {
	// 有效配置创建客户端成功
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("NewSiliconFlowModelClient 返回错误: %v", err)
	}
	if client == nil {
		t.Fatal("client 不应为 nil")
	}
	if client.GetClientName() != "SiliconFlow client" {
		t.Errorf("ClientName = %q, want %q", client.GetClientName(), "SiliconFlow client")
	}
}

func TestNewSiliconFlowModelClient_NoAPIKey(t *testing.T) {
	// 缺少 API Key 应失败
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "", "https://api.siliconflow.cn/v1"),
	)
	if err == nil {
		t.Error("缺少 API Key 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Key 时 client 应为 nil")
	}
}

func TestNewSiliconFlowModelClient_NoAPIBase(t *testing.T) {
	// 缺少 API Base 应失败
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", ""),
	)
	if err == nil {
		t.Error("缺少 API Base 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Base 时 client 应为 nil")
	}
}

// ──────────────────────────── 接口合规性测试 ────────────────────────────

func TestSiliconFlowModelClient_ImplementsBaseModelClient(t *testing.T) {
	// 验证 SiliconFlowModelClient 实现了 BaseModelClient 接口
	var _ model_clients.BaseModelClient = (*SiliconFlowModelClient)(nil)
}

// ──────────────────────────── sanitizeMessages 测试 ────────────────────────────

func TestSanitizeMessages_DictsPassthrough(t *testing.T) {
	// Dicts 格式消息也能正确处理
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	dictMsgs := []map[string]any{
		{"role": "user", "content": "你好"},
		{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{
				"id":    "call-1",
				"type":  "function",
				"extra": "should_be_removed",
				"function": map[string]any{
					"name":       "test",
					"arguments":  "{}",
					"extra_func": "should_be_removed",
				},
			},
		}},
	}
	msgs := model_clients.NewDictsMessagesParam(dictMsgs)
	sanitizedMsgs, err := client.sanitizeMessages(msgs)
	if err != nil {
		t.Fatalf("sanitizeMessages 失败: %v", err)
	}

	dicts := sanitizedMsgs.Dicts()
	assistantDict := dicts[1]
	toolCalls, _ := assistantDict["tool_calls"].([]map[string]any)
	tc := toolCalls[0]

	// 非标准字段应被移除
	if _, exists := tc["extra"]; exists {
		t.Error("Dicts 格式的 tool_calls 非标准字段 extra 应被移除")
	}
	funcPart, _ := tc["function"].(map[string]any)
	if _, exists := funcPart["extra_func"]; exists {
		t.Error("Dicts 格式的 function 内非标准字段 extra_func 应被移除")
	}
	// 标准字段应保留
	if tc["type"] != "function" {
		t.Errorf("type = %v, want function", tc["type"])
	}
}

func TestSanitizeMessages_TextInput(t *testing.T) {
	// 纯文本输入也能正确处理（自动包装为 UserMessage，没有 assistant 消息不需要清洗）
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msgs := model_clients.NewTextMessagesParam("你好")
	sanitizedMsgs, err := client.sanitizeMessages(msgs)
	if err != nil {
		t.Fatalf("sanitizeMessages 失败: %v", err)
	}

	dicts := sanitizedMsgs.Dicts()
	if len(dicts) != 1 {
		t.Fatalf("dicts 长度 = %d, want 1", len(dicts))
	}
	if dicts[0]["role"] != "user" {
		t.Errorf("role = %q, want user", dicts[0]["role"])
	}
}

func TestSanitizeMessages_MessagesInput(t *testing.T) {
	// 消息列表格式也能正确处理
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	toolCalls := []*llmschema.ToolCall{
		llmschema.NewToolCall("call-123", "get_weather", `{"city":"Beijing"}`),
	}
	assistantMsg := llmschema.NewAssistantMessage(
		"",
		llmschema.WithToolCalls(toolCalls),
	)

	msgs := model_clients.NewMessagesParam(
		llmschema.NewUserMessage("北京天气"),
		assistantMsg,
		llmschema.NewToolMessage("晴天，25°C", "call-123"),
	)
	sanitizedMsgs, err := client.sanitizeMessages(msgs)
	if err != nil {
		t.Fatalf("sanitizeMessages 失败: %v", err)
	}

	dicts := sanitizedMsgs.Dicts()
	if len(dicts) != 3 {
		t.Fatalf("dicts 长度 = %d, want 3", len(dicts))
	}

	// assistant 消息的 tool_calls 应被清洗
	assistantDict := dicts[1]
	if role, _ := assistantDict["role"].(string); role != "assistant" {
		t.Errorf("messages[1].role = %q, want assistant", role)
	}
	toolCallsResult, _ := assistantDict["tool_calls"].([]map[string]any)
	if len(toolCallsResult) != 1 {
		t.Fatalf("tool_calls 长度 = %d, want 1", len(toolCallsResult))
	}
	tc := toolCallsResult[0]
	if tc["type"] != "function" {
		t.Errorf("type = %v, want function", tc["type"])
	}
}

func TestSanitizeMessages_EmptyMessages(t *testing.T) {
	// 空消息应返回错误
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msgs := model_clients.MessagesParam{}
	_, err = client.sanitizeMessages(msgs)
	if err == nil {
		t.Error("空消息应返回错误")
	}
}

func TestInvoke_SanitizeMessagesError(t *testing.T) {
	// sanitizeMessages 返回错误时，Invoke 应返回错误
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 空消息会导致 sanitizeMessages 失败
	msgs := model_clients.MessagesParam{}
	_, err = client.Invoke(context.Background(), msgs)
	if err == nil {
		t.Error("空消息时 Invoke 应返回错误")
	}
}

func TestStream_SanitizeMessagesError(t *testing.T) {
	// sanitizeMessages 返回错误时，Stream 应返回错误
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 空消息会导致 sanitizeMessages 失败
	msgs := model_clients.MessagesParam{}
	_, err = client.Stream(context.Background(), msgs)
	if err == nil {
		t.Error("空消息时 Stream 应返回错误")
	}
}

// ──────────────────────────── Invoke 集成测试 ────────────────────────────

func TestSiliconFlowModelClient_Invoke_成功(t *testing.T) {
	// 使用 httptest 模拟 SiliconFlow API 返回 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求方法和路径
		if r.Method != http.MethodPost {
			t.Errorf("请求方法 = %s, want POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("请求路径 = %s, want /chat/completions", r.URL.Path)
		}
		// 验证 Authorization 头
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-key")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponse("你好！我是 SiliconFlow"))
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("你好")
	result, err := client.Invoke(context.Background(), msg)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("Invoke 结果不应为 nil")
	}
	if result.Content.Text() != "你好！我是 SiliconFlow" {
		t.Errorf("Content = %q, want %q", result.Content.Text(), "你好！我是 SiliconFlow")
	}
}

func TestSiliconFlowModelClient_Invoke_请求体含SanitizedToolCalls(t *testing.T) {
	// 验证多轮对话中 tool_calls 被正确清洗后传给 API
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponse("好的"))
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 用 Dicts 格式传入含非标准字段的 tool_calls
	dictMsgs := []map[string]any{
		{"role": "user", "content": "你好"},
		{
			"role":    "assistant",
			"content": "",
			"tool_calls": []any{
				map[string]any{
					"id":    "call-1",
					"type":  "", // 非标准 type，应被强制为 "function"
					"extra": "should_be_removed",
					"function": map[string]any{
						"name":       "test_func",
						"arguments":  "{}",
						"extra_func": "should_be_removed",
					},
				},
			},
		},
		{"role": "user", "content": "继续"},
	}
	msgs := model_clients.NewDictsMessagesParam(dictMsgs)

	result, err := client.Invoke(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("Invoke 结果不应为 nil")
	}

	// 验证请求体中 tool_calls 被清洗
	messages, _ := receivedBody["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages 长度 = %d, want 3", len(messages))
	}

	// 第二条是 assistant 消息
	assistantMsg, _ := messages[1].(map[string]any)
	if role, _ := assistantMsg["role"].(string); role != "assistant" {
		t.Errorf("messages[1].role = %q, want assistant", role)
	}

	toolCalls, _ := assistantMsg["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls 长度 = %d, want 1", len(toolCalls))
	}

	tc, _ := toolCalls[0].(map[string]any)
	// type 应被强制为 "function"
	if tc["type"] != "function" {
		t.Errorf("tool_calls[0].type = %v, want function", tc["type"])
	}
	// 非标准字段应被移除
	if _, exists := tc["extra"]; exists {
		t.Error("请求体中 tool_calls 的非标准字段 extra 应被移除")
	}
	// function 内非标准字段应被移除
	funcPart, _ := tc["function"].(map[string]any)
	if _, exists := funcPart["extra_func"]; exists {
		t.Error("请求体中 function 的非标准字段 extra_func 应被移除")
	}
	// 标准字段应保留
	if funcPart["name"] != "test_func" {
		t.Errorf("function.name = %v, want test_func", funcPart["name"])
	}
}

func TestSiliconFlowModelClient_Invoke_响应含ReasoningContent(t *testing.T) {
	// 验证 SiliconFlow 响应中的 reasoning_content 被正确解析
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponseWithReasoning("答案是 42", "我需要计算一下..."))
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("1+1=?")
	result, err := client.Invoke(context.Background(), msg)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result.ReasoningContent != "我需要计算一下..." {
		t.Errorf("ReasoningContent = %q, want %q", result.ReasoningContent, "我需要计算一下...")
	}
}

func TestSiliconFlowModelClient_Invoke_HTTP错误(t *testing.T) {
	// 使用 httptest 返回 401，验证错误处理
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":{"message":"Incorrect API key","type":"invalid_request_error","code":"invalid_api_key"}}`)
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Invoke(context.Background(), msg)
	if err == nil {
		t.Error("Invoke HTTP 401 应返回错误")
	}
	if result != nil {
		t.Error("Invoke HTTP 401 结果应为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "401") {
		t.Errorf("错误消息应包含 401, got %q", baseErr.Error())
	}
}

// ──────────────────────────── Stream 集成测试 ────────────────────────────

func TestSiliconFlowModelClient_Stream_成功(t *testing.T) {
	// 使用 httptest 模拟 SSE 流，验证 Stream 结果
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("请求方法 = %s, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{"role":"assistant","content":"He"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{"content":"llo"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
			`data: [DONE]`,
		}

		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("Hi")
	chunkChan, err := client.Stream(context.Background(), msg)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	// 从 channel 读取并累积为最终结果
	var final *llmschema.AssistantMessageChunk
	for chunk := range chunkChan {
		if final == nil {
			final = chunk
		} else {
			final = final.Merge(chunk)
		}
	}
	if final == nil {
		t.Fatal("Final 不应为 nil")
	}
	if final.Content.Text() != "Hello" {
		t.Errorf("Final Content = %q, want %q", final.Content.Text(), "Hello")
	}
	if final.FinishReason != "stop" {
		t.Errorf("Final FinishReason = %q, want %q", final.FinishReason, "stop")
	}
}

func TestSiliconFlowModelClient_Stream_请求体含SanitizedToolCalls(t *testing.T) {
	// 验证流式调用中 tool_calls 也被正确清洗
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 多轮对话包含带非标准字段的 tool_calls
	dictMsgs := []map[string]any{
		{"role": "user", "content": "你好"},
		{
			"role":    "assistant",
			"content": "",
			"tool_calls": []any{
				map[string]any{
					"id":    "call-1",
					"type":  "function",
					"extra": "should_be_removed",
					"function": map[string]any{
						"name":       "test",
						"arguments":  "{}",
						"extra_func": "should_be_removed",
					},
				},
			},
		},
		{"role": "user", "content": "继续"},
	}
	msgs := model_clients.NewDictsMessagesParam(dictMsgs)

	_, err = client.Stream(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	// 验证请求体中 tool_calls 被清洗
	messages, _ := receivedBody["messages"].([]any)
	assistantDict, _ := messages[1].(map[string]any)
	toolCalls, _ := assistantDict["tool_calls"].([]any)
	tc, _ := toolCalls[0].(map[string]any)

	// 非标准字段应被移除
	if _, exists := tc["extra"]; exists {
		t.Error("流式请求体中 tool_calls 的非标准字段 extra 应被移除")
	}
	// type 应为 "function"
	if tc["type"] != "function" {
		t.Errorf("流式请求体中 tool_calls type = %v, want function", tc["type"])
	}
}

func TestSiliconFlowModelClient_Stream_HTTP错误(t *testing.T) {
	// 使用 httptest 返回非 200，验证错误处理
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`)
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("Hi")
	chunkChan, err := client.Stream(context.Background(), msg)
	if err == nil {
		t.Error("Stream HTTP 429 应返回错误")
	}
	if chunkChan != nil {
		t.Error("Stream HTTP 429 结果应为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "429") {
		t.Errorf("错误消息应包含 429, got %q", baseErr.Error())
	}
}

func TestRelease_NotSupported(t *testing.T) {
	// Release 应返回 false 和不支持错误
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	ok, err := client.Release(context.Background())
	if ok {
		t.Error("Release 应返回 false")
	}
	if err == nil {
		t.Fatal("Release 应返回错误")
	}
	if !strings.Contains(err.Error(), "does not support KV cache release") {
		t.Errorf("错误消息应包含 'does not support KV cache release', got %q", err.Error())
	}
}

// ──────────────────────────── 多模态不支持测试 ────────────────────────────

func TestSiliconFlowModelClient_GenerateImage_NotSupported(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	result, err := client.GenerateImage(context.Background(), nil)
	if err == nil {
		t.Error("GenerateImage 应返回错误")
	}
	if result != nil {
		t.Error("GenerateImage 结果应为 nil")
	}
	if !strings.Contains(err.Error(), "does not support image generation") {
		t.Errorf("错误消息应包含不支持图片生成, got %q", err.Error())
	}
}

func TestSiliconFlowModelClient_GenerateSpeech_NotSupported(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	result, err := client.GenerateSpeech(context.Background(), nil)
	if err == nil {
		t.Error("GenerateSpeech 应返回错误")
	}
	if result != nil {
		t.Error("GenerateSpeech 结果应为 nil")
	}
	if !strings.Contains(err.Error(), "does not support speech generation") {
		t.Errorf("错误消息应包含不支持语音生成, got %q", err.Error())
	}
}

func TestSiliconFlowModelClient_GenerateVideo_NotSupported(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	result, err := client.GenerateVideo(context.Background(), nil)
	if err == nil {
		t.Error("GenerateVideo 应返回错误")
	}
	if result != nil {
		t.Error("GenerateVideo 结果应为 nil")
	}
	if !strings.Contains(err.Error(), "does not support video generation") {
		t.Errorf("错误消息应包含不支持视频生成, got %q", err.Error())
	}
}

// ──────────────────────────── init 注册测试 ────────────────────────────

func TestInit_注册SiliconFlow(t *testing.T) {
	// 验证 init() 注册了 SiliconFlow 到全局注册表
	registry := model_clients.GetClientRegistry()
	clients := registry.ListClients()

	hasSiliconFlow := false
	for _, name := range clients {
		if name == "llm_SiliconFlow" {
			hasSiliconFlow = true
		}
	}
	if !hasSiliconFlow {
		t.Error("init() 应注册 llm_SiliconFlow")
	}

	// 通过 GetClient 验证 SiliconFlow 工厂能正常创建客户端
	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("Qwen/Qwen2.5-7B-Instruct"))
	cc := llmschema.NewModelClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1",
		llmschema.WithVerifySSL(false),
	)
	client, err := registry.GetClient("SiliconFlow", "llm", mc, cc)
	if err != nil {
		t.Fatalf("GetClient(SiliconFlow) 报错: %v", err)
	}
	if client == nil {
		t.Error("GetClient(SiliconFlow) 应返回非 nil 客户端")
	}

	// 验证创建的客户端是 SiliconFlowModelClient 类型
	siliconFlowClient, ok := client.(*SiliconFlowModelClient)
	if !ok {
		t.Fatalf("GetClient(SiliconFlow) 应返回 *SiliconFlowModelClient, got %T", client)
	}
	if siliconFlowClient.GetClientName() != "SiliconFlow client" {
		t.Errorf("ClientName = %q, want %q", siliconFlowClient.GetClientName(), "SiliconFlow client")
	}
}

// ──────────────────────────── sanitizeToolCalls 测试 ────────────────────────────

func TestSanitizeToolCalls_标准字段保留(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	messages := []map[string]any{
		{
			"role":    "assistant",
			"content": "",
			"tool_calls": []any{
				map[string]any{
					"id":   "call-123",
					"type": "function",
					"function": map[string]any{
						"name":      "get_weather",
						"arguments": `{"city":"Beijing"}`,
					},
				},
			},
		},
	}

	client.sanitizeToolCalls(messages)

	toolCalls, _ := messages[0]["tool_calls"].([]map[string]any)
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls 长度 = %d, 期望 1", len(toolCalls))
	}

	tc := toolCalls[0]
	if tc["id"] != "call-123" {
		t.Errorf("id = %v, 期望 call-123", tc["id"])
	}
	if tc["type"] != "function" {
		t.Errorf("type = %v, 期望 function", tc["type"])
	}
	funcPart, _ := tc["function"].(map[string]any)
	if funcPart["name"] != "get_weather" {
		t.Errorf("function.name = %v, 期望 get_weather", funcPart["name"])
	}
	if funcPart["arguments"] != `{"city":"Beijing"}` {
		t.Errorf("function.arguments = %v, 期望 {\"city\":\"Beijing\"}", funcPart["arguments"])
	}
}

func TestSanitizeToolCalls_非标准字段移除(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	messages := []map[string]any{
		{
			"role":    "assistant",
			"content": "",
			"tool_calls": []any{
				map[string]any{
					"id":          "call-456",
					"type":        "function",
					"extra_field": "should_be_removed",
					"debug_info":  "should_be_removed_2",
					"function": map[string]any{
						"name":       "search",
						"arguments":  `{"q":"test"}`,
						"extra_func": "should_be_removed",
					},
				},
			},
		},
	}

	client.sanitizeToolCalls(messages)

	toolCalls, _ := messages[0]["tool_calls"].([]map[string]any)
	tc := toolCalls[0]

	if _, exists := tc["extra_field"]; exists {
		t.Error("tool_calls 顶层非标准字段 extra_field 应被移除")
	}
	if _, exists := tc["debug_info"]; exists {
		t.Error("tool_calls 顶层非标准字段 debug_info 应被移除")
	}
	funcPart, _ := tc["function"].(map[string]any)
	if _, exists := funcPart["extra_func"]; exists {
		t.Error("function 内非标准字段 extra_func 应被移除")
	}
	if funcPart["name"] != "search" {
		t.Errorf("function.name = %v, 期望 search", funcPart["name"])
	}
}

func TestSanitizeToolCalls_强制Type为Function(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	testCases := []struct {
		name      string
		inputType string
	}{
		{"空字符串", ""},
		{"其他值", "tool"},
		{"已正确", "function"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			messages := []map[string]any{
				{
					"role":    "assistant",
					"content": "",
					"tool_calls": []any{
						map[string]any{
							"id":   "call-1",
							"type": tc.inputType,
							"function": map[string]any{
								"name":      "test_func",
								"arguments": "{}",
							},
						},
					},
				},
			}

			client.sanitizeToolCalls(messages)

			toolCalls, _ := messages[0]["tool_calls"].([]map[string]any)
			result := toolCalls[0]
			if result["type"] != "function" {
				t.Errorf("type 输入 %q 后 = %v, 期望 function", tc.inputType, result["type"])
			}
		})
	}
}

func TestSanitizeToolCalls_非Assistant消息不受影响(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	messages := []map[string]any{
		{"role": "user", "content": "你好"},
		{"role": "system", "content": "你是助手"},
	}

	origUser := messages[0]["content"]
	origSystem := messages[1]["content"]

	client.sanitizeToolCalls(messages)

	if messages[0]["content"] != origUser {
		t.Error("user 消息不应被修改")
	}
	if messages[1]["content"] != origSystem {
		t.Error("system 消息不应被修改")
	}
	if _, exists := messages[0]["tool_calls"]; exists {
		t.Error("user 消息不应被添加 tool_calls 字段")
	}
}

func TestSanitizeToolCalls_无ToolCalls不受影响(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	messages := []map[string]any{
		{"role": "assistant", "content": "你好！"},
	}

	client.sanitizeToolCalls(messages)

	if _, exists := messages[0]["tool_calls"]; exists {
		t.Error("没有 tool_calls 的 assistant 消息不应被添加 tool_calls 字段")
	}
}

func TestSanitizeToolCalls_保留Index(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	messages := []map[string]any{
		{
			"role":    "assistant",
			"content": "",
			"tool_calls": []any{
				map[string]any{
					"id":    "call-1",
					"type":  "function",
					"index": float64(0),
					"function": map[string]any{
						"name":      "func_a",
						"arguments": "{}",
					},
				},
				map[string]any{
					"id":    "call-2",
					"type":  "function",
					"index": float64(1),
					"function": map[string]any{
						"name":      "func_b",
						"arguments": "{}",
					},
				},
			},
		},
	}

	client.sanitizeToolCalls(messages)

	toolCalls, _ := messages[0]["tool_calls"].([]map[string]any)
	if len(toolCalls) != 2 {
		t.Fatalf("tool_calls 长度 = %d, 期望 2", len(toolCalls))
	}
	if idx, ok := toolCalls[0]["index"]; !ok {
		t.Error("第一个 tool_call 的 index 应被保留")
	} else if idx != float64(0) {
		t.Errorf("第一个 tool_call index = %v, 期望 0", idx)
	}
	if idx, ok := toolCalls[1]["index"]; !ok {
		t.Error("第二个 tool_call 的 index 应被保留")
	} else if idx != float64(1) {
		t.Errorf("第二个 tool_call index = %v, 期望 1", idx)
	}
}

func TestSanitizeToolCalls_FunctionNotMap(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	messages := []map[string]any{
		{
			"role":    "assistant",
			"content": "",
			"tool_calls": []map[string]any{
				{
					"id":   "call-1",
					"type": "function",
					// function 缺失
				},
			},
		},
	}

	client.sanitizeToolCalls(messages)

	toolCalls, _ := messages[0]["tool_calls"].([]map[string]any)
	tc := toolCalls[0]
	funcPart, _ := tc["function"].(map[string]any)
	if funcPart["name"] != "" {
		t.Errorf("function.name = %v, 期望空字符串", funcPart["name"])
	}
	if funcPart["arguments"] != "" {
		t.Errorf("function.arguments = %v, 期望空字符串", funcPart["arguments"])
	}
}

func TestSanitizeToolCalls_ToolCallNotMap(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	messages := []map[string]any{
		{
			"role":    "assistant",
			"content": "",
			"tool_calls": []any{
				"invalid_tool_call", // 非 map 类型，应被跳过
				map[string]any{
					"id":   "call-1",
					"type": "function",
					"function": map[string]any{
						"name":      "valid_func",
						"arguments": "{}",
					},
				},
			},
		},
	}

	client.sanitizeToolCalls(messages)

	toolCalls, _ := messages[0]["tool_calls"].([]map[string]any)
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls 长度 = %d, 期望 1", len(toolCalls))
	}
	if funcPart, _ := toolCalls[0]["function"].(map[string]any); funcPart["name"] != "valid_func" {
		t.Errorf("function.name = %v, 期望 valid_func", funcPart["name"])
	}
}

func TestSanitizeToolCalls_MapStringAny格式(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	messages := []map[string]any{
		{
			"role":    "assistant",
			"content": "",
			"tool_calls": []map[string]any{
				{
					"id":   "call-789",
					"type": "function",
					"function": map[string]any{
						"name":      "calc",
						"arguments": `{"x":1}`,
					},
				},
			},
		},
	}

	client.sanitizeToolCalls(messages)

	toolCalls, _ := messages[0]["tool_calls"].([]map[string]any)
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls 长度 = %d, 期望 1", len(toolCalls))
	}
	if toolCalls[0]["id"] != "call-789" {
		t.Errorf("id = %v, 期望 call-789", toolCalls[0]["id"])
	}
}

func TestSanitizeToolCalls_空消息列表(t *testing.T) {
	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", "https://api.siliconflow.cn/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 空消息列表不应 panic
	client.sanitizeToolCalls(nil)
	client.sanitizeToolCalls([]map[string]any{})
}

// ──────────────────────────── Stream 回调对齐测试 ────────────────────────────

// sfStreamOutputParser 成功解析的 mock parser
type sfStreamOutputParser struct{}

func (p *sfStreamOutputParser) Parse(input any) (any, error) {
	text, _ := input.(string)
	return map[string]any{"parsed": text}, nil
}

func (p *sfStreamOutputParser) StreamParse(chunks <-chan any) <-chan model_clients.StreamParsedResult {
	out := make(chan model_clients.StreamParsedResult)
	go func() { close(out) }()
	return out
}

// sfStreamErrorOutputParser 总是返回错误的 mock parser
type sfStreamErrorOutputParser struct{}

func (p *sfStreamErrorOutputParser) Parse(_ any) (any, error) {
	return nil, fmt.Errorf("stream parse error")
}

func (p *sfStreamErrorOutputParser) StreamParse(chunks <-chan any) <-chan model_clients.StreamParsedResult {
	out := make(chan model_clients.StreamParsedResult)
	go func() { close(out) }()
	return out
}

func TestSiliconFlowModelClient_Stream_无效JSON走logger(t *testing.T) {
	// SSE 流中包含无效 JSON 数据，应走 logger.Error 而非回调，继续读取后续数据
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: invalid-json`,
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{"content":"OK"},"finish_reason":null}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("Hi")
	chunkChan, err := client.Stream(context.Background(), msg)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	var final *llmschema.AssistantMessageChunk
	for chunk := range chunkChan {
		if final == nil {
			final = chunk
		} else {
			final = final.Merge(chunk)
		}
	}
	if final == nil {
		t.Fatal("Final 不应为 nil")
	}
	if final.Content.Text() != "OK" {
		t.Errorf("Final Content = %q, 期望 %q", final.Content.Text(), "OK")
	}
}

func TestSiliconFlowModelClient_Stream_OutputParser成功(t *testing.T) {
	// Stream 带 OutputParser，验证 parser 成功路径
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("Hi")
	chunkChan, err := client.Stream(context.Background(), msg,
		model_clients.WithStreamOutputParser(&sfStreamOutputParser{}),
	)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	var final *llmschema.AssistantMessageChunk
	for chunk := range chunkChan {
		if final == nil {
			final = chunk
		} else {
			final = final.Merge(chunk)
		}
	}
	if final == nil {
		t.Fatal("Final 不应为 nil")
	}
	if final.ParserContent == nil {
		t.Error("Final ParserContent 不应为 nil（OutputParser 成功路径）")
	}
}

func TestSiliconFlowModelClient_Stream_OutputParser错误(t *testing.T) {
	// Stream 带 OutputParser 解析失败，应走 logger.Error 而非回调
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("Hi")
	chunkChan, err := client.Stream(context.Background(), msg,
		model_clients.WithStreamOutputParser(&sfStreamErrorOutputParser{}),
	)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	// 验证流正常结束（parser 错误不应导致流崩溃）
	var final *llmschema.AssistantMessageChunk
	for chunk := range chunkChan {
		if final == nil {
			final = chunk
		} else {
			final = final.Merge(chunk)
		}
	}
	if final == nil {
		t.Fatal("Final 不应为 nil")
	}
	// parser 错误时 ParserContent 应为 nil
	if final.ParserContent != nil {
		t.Error("Final ParserContent 应为 nil（OutputParser 失败路径）")
	}
}

func TestSiliconFlowModelClient_Stream_SSE异常关闭(t *testing.T) {
	// SSE 流异常关闭（无 [DONE]），验证不崩溃
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`,
		}
		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
		// 不发送 [DONE]，直接关闭连接
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("Hi")
	chunkChan, err := client.Stream(context.Background(), msg)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	// 验证流正常终止（不 panic）
	var final *llmschema.AssistantMessageChunk
	for chunk := range chunkChan {
		if final == nil {
			final = chunk
		} else {
			final = final.Merge(chunk)
		}
	}
	if final == nil {
		t.Fatal("Final 不应为 nil")
	}
}

func TestSiliconFlowModelClient_Stream_Context取消(t *testing.T) {
	// Stream 时 context 取消，验证触发 LLMCallError 回调
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// 持续发送数据
		for i := 0; i < 100; i++ {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}]}`)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	msg := model_clients.NewTextMessagesParam("Hi")
	chunkChan, err := client.Stream(ctx, msg)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	// 读取一个 chunk 后取消 context
	<-chunkChan // 读一个
	cancel()    // 取消

	// 验证流正常终止（不 panic）
	for range chunkChan {
	}
}

// ──────────────────────────── parseStreamChunk 测试 ────────────────────────────

func TestSiliconFlowModelClient_Stream_ReasoningContent(t *testing.T) {
	// 流式响应包含 reasoning_content，验证正确解析
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{"role":"assistant","content":"答案","reasoning_content":"思考过程"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("Hi")
	chunkChan, err := client.Stream(context.Background(), msg)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	var final *llmschema.AssistantMessageChunk
	for chunk := range chunkChan {
		if final == nil {
			final = chunk
		} else {
			final = final.Merge(chunk)
		}
	}
	if final == nil {
		t.Fatal("Final 不应为 nil")
	}
	if final.ReasoningContent != "思考过程" {
		t.Errorf("ReasoningContent = %q, 期望 %q", final.ReasoningContent, "思考过程")
	}
}

func TestSiliconFlowModelClient_Stream_ToolCallsDelta(t *testing.T) {
	// 流式响应包含 tool_calls delta，验证正确解析
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Beijing\"}"}}]},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("北京天气")
	chunkChan, err := client.Stream(context.Background(), msg)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	var final *llmschema.AssistantMessageChunk
	for chunk := range chunkChan {
		if final == nil {
			final = chunk
		} else {
			final = final.Merge(chunk)
		}
	}
	if final == nil {
		t.Fatal("Final 不应为 nil")
	}
	if len(final.ToolCalls) == 0 {
		t.Fatal("Final ToolCalls 不应为空")
	}
	if final.ToolCalls[0].Name != "get_weather" {
		t.Errorf("ToolCalls[0].Name = %q, 期望 %q", final.ToolCalls[0].Name, "get_weather")
	}
}

func TestSiliconFlowModelClient_Stream_UsageOnlyChunk被丢弃(t *testing.T) {
	// 流中包含无 choices 的 usage-only chunk（部分 API 实现），应被静默丢弃
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`,
			// 无 choices 的 usage-only chunk
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`,
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"Qwen/Qwen2.5-7B-Instruct","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewSiliconFlowModelClient(
		newTestModelConfig(),
		newTestClientConfig("SiliconFlow", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("Hi")
	chunkChan, err := client.Stream(context.Background(), msg)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	var final *llmschema.AssistantMessageChunk
	for chunk := range chunkChan {
		if final == nil {
			final = chunk
		} else {
			final = final.Merge(chunk)
		}
	}
	if final == nil {
		t.Fatal("Final 不应为 nil")
	}
	if final.Content.Text() != "Hi" {
		t.Errorf("Content = %q, 期望 %q", final.Content.Text(), "Hi")
	}
}

// ──────────────────────────── extractCostFromUsage 测试 ────────────────────────────

func TestExtractCostFromUsage_含费用信息(t *testing.T) {
	// usage 包含 cost 字段时正确提取
	usage := &openai.ResponseUsage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	}
	// 通过 json 注入 cost 字段（ExtractCostInfo 查找 "cost" 键）
	data, _ := json.Marshal(usage)
	var usageMap map[string]any
	_ = json.Unmarshal(data, &usageMap)
	usageMap["cost"] = map[string]any{
		"input_cost":  0.001,
		"output_cost": 0.002,
		"total_cost":  0.003,
	}

	inputCost, outputCost, totalCost := model_clients.ExtractCostInfo(usageMap)
	if totalCost <= 0 {
		t.Errorf("totalCost 应大于 0, 实际 %f (inputCost=%f, outputCost=%f)", totalCost, inputCost, outputCost)
	}
}

func TestExtractCostFromUsage_无费用信息(t *testing.T) {
	// usage 不含 cost 字段时返回 0
	usage := &openai.ResponseUsage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	}
	data, _ := json.Marshal(usage)
	var usageMap map[string]any
	_ = json.Unmarshal(data, &usageMap)

	inputCost, outputCost, totalCost := model_clients.ExtractCostInfo(usageMap)
	if inputCost != 0 || outputCost != 0 || totalCost != 0 {
		t.Errorf("无 cost 字段时应返回 0, 实际 inputCost=%f, outputCost=%f, totalCost=%f", inputCost, outputCost, totalCost)
	}
}
