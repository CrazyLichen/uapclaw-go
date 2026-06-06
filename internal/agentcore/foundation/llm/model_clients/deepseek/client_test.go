package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
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
	return llmschema.NewModelRequestConfig(llmschema.WithModelName("deepseek-chat"))
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
	return fmt.Sprintf(`{"id":"chatcmpl-123","object":"chat.completion","created":1234567890,"model":"deepseek-chat","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, content)
}

// mockCompletionResponseWithReasoning 构造含 reasoning_content 的响应
func mockCompletionResponseWithReasoning(content, reasoningContent string) string {
	return fmt.Sprintf(`{"id":"chatcmpl-123","object":"chat.completion","created":1234567890,"model":"deepseek-chat","choices":[{"index":0,"message":{"role":"assistant","content":%q,"reasoning_content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, content, reasoningContent)
}

// ──────────────────────────── NewDeepSeekModelClient 测试 ────────────────────────────

func TestNewDeepSeekModelClient_ValidConfig(t *testing.T) {
	// 有效配置创建客户端成功
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
	)
	if err != nil {
		t.Fatalf("NewDeepSeekModelClient 返回错误: %v", err)
	}
	if client == nil {
		t.Fatal("client 不应为 nil")
	}
	if client.GetClientName() != "DeepSeek client" {
		t.Errorf("ClientName = %q, want %q", client.GetClientName(), "DeepSeek client")
	}
}

func TestNewDeepSeekModelClient_NoAPIKey(t *testing.T) {
	// 缺少 API Key 应失败
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "", "https://api.deepseek.com/v1"),
	)
	if err == nil {
		t.Error("缺少 API Key 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Key 时 client 应为 nil")
	}
}

func TestNewDeepSeekModelClient_NoAPIBase(t *testing.T) {
	// 缺少 API Base 应失败
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", ""),
	)
	if err == nil {
		t.Error("缺少 API Base 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Base 时 client 应为 nil")
	}
}

// ──────────────────────────── 接口合规性测试 ────────────────────────────

func TestDeepSeekModelClient_ImplementsBaseModelClient(t *testing.T) {
	// 验证 DeepSeekModelClient 实现了 BaseModelClient 接口
	var _ model_clients.BaseModelClient = (*DeepSeekModelClient)(nil)
}

// ──────────────────────────── enrichMessagesWithReasoningContent 测试 ────────────────────────────

func TestEnrichMessages_AssistantWithoutReasoningContent(t *testing.T) {
	// assistant 消息没有 reasoning_content → 应补充空字符串
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msgs := model_clients.NewMessagesParam(
		llmschema.NewUserMessage("你好"),
		llmschema.NewAssistantMessage("你好！"),
	)
	enrichedMsgs, err := client.enrichMessagesWithReasoningContent(msgs)
	if err != nil {
		t.Fatalf("enrichMessagesWithReasoningContent 失败: %v", err)
	}

	// 转换为 dict 验证
	dicts := enrichedMsgs.Dicts()
	if len(dicts) != 2 {
		t.Fatalf("dicts 长度 = %d, want 2", len(dicts))
	}

	// 第二条是 assistant 消息
	assistantMsg := dicts[1]
	if role, _ := assistantMsg["role"].(string); role != "assistant" {
		t.Errorf("role = %q, want assistant", role)
	}
	if rc, exists := assistantMsg["reasoning_content"]; !exists {
		t.Error("assistant 消息应包含 reasoning_content 字段")
	} else if rc != "" {
		t.Errorf("reasoning_content = %q, want 空字符串", rc)
	}
}

func TestEnrichMessages_AssistantWithToolCalls(t *testing.T) {
	// 带 tool_calls 的 assistant 消息也应补 reasoning_content
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
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
	)
	enrichedMsgs, err := client.enrichMessagesWithReasoningContent(msgs)
	if err != nil {
		t.Fatalf("enrichMessagesWithReasoningContent 失败: %v", err)
	}

	dicts := enrichedMsgs.Dicts()
	assistantDict := dicts[1]

	// 应有 tool_calls
	if _, exists := assistantDict["tool_calls"]; !exists {
		t.Error("assistant 消息应包含 tool_calls 字段")
	}
	// 应有 reasoning_content
	if rc, exists := assistantDict["reasoning_content"]; !exists {
		t.Error("带 tool_calls 的 assistant 消息也应包含 reasoning_content")
	} else if rc != "" {
		t.Errorf("reasoning_content = %q, want 空字符串", rc)
	}
}

func TestEnrichMessages_AssistantWithExistingReasoningContent(t *testing.T) {
	// 已有 reasoning_content 的 assistant 消息不应被覆盖
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	assistantMsg := llmschema.NewAssistantMessage(
		"答案是 42",
		llmschema.WithReasoningContent("我需要思考一下..."),
	)

	msgs := model_clients.NewMessagesParam(
		llmschema.NewUserMessage("终极问题"),
		assistantMsg,
	)
	enrichedMsgs, err := client.enrichMessagesWithReasoningContent(msgs)
	if err != nil {
		t.Fatalf("enrichMessagesWithReasoningContent 失败: %v", err)
	}

	dicts := enrichedMsgs.Dicts()
	assistantDict := dicts[1]

	rc, _ := assistantDict["reasoning_content"].(string)
	if rc != "我需要思考一下..." {
		t.Errorf("reasoning_content = %q, want %q", rc, "我需要思考一下...")
	}
}

func TestEnrichMessages_UserMessageNoChange(t *testing.T) {
	// 非 assistant 消息不应被修改
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msgs := model_clients.NewMessagesParam(
		llmschema.NewUserMessage("你好"),
		llmschema.NewSystemMessage("你是助手"),
	)
	enrichedMsgs, err := client.enrichMessagesWithReasoningContent(msgs)
	if err != nil {
		t.Fatalf("enrichMessagesWithReasoningContent 失败: %v", err)
	}

	dicts := enrichedMsgs.Dicts()
	// user 和 system 消息都不应有 reasoning_content
	for _, msg := range dicts {
		if role, _ := msg["role"].(string); role != "assistant" {
			if _, exists := msg["reasoning_content"]; exists {
				t.Errorf("%s 消息不应包含 reasoning_content 字段", role)
			}
		}
	}
}

func TestEnrichMessages_MixedMessages(t *testing.T) {
	// 混合消息列表，只有 assistant 消息被补充 reasoning_content
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msgs := model_clients.NewMessagesParam(
		llmschema.NewSystemMessage("你是助手"),
		llmschema.NewUserMessage("你好"),
		llmschema.NewAssistantMessage("你好！"),
		llmschema.NewUserMessage("1+1=?"),
		llmschema.NewAssistantMessage("2"),
	)
	enrichedMsgs, err := client.enrichMessagesWithReasoningContent(msgs)
	if err != nil {
		t.Fatalf("enrichMessagesWithReasoningContent 失败: %v", err)
	}

	dicts := enrichedMsgs.Dicts()
	// 统计 assistant 消息含 reasoning_content 的数量
	assistantCount := 0
	assistantWithRC := 0
	for _, msg := range dicts {
		if role, _ := msg["role"].(string); role == "assistant" {
			assistantCount++
			if _, exists := msg["reasoning_content"]; exists {
				assistantWithRC++
			}
		}
	}
	if assistantCount != 2 {
		t.Errorf("assistant 消息数 = %d, want 2", assistantCount)
	}
	if assistantWithRC != 2 {
		t.Errorf("含 reasoning_content 的 assistant 消息数 = %d, want 2", assistantWithRC)
	}
}

func TestEnrichMessages_DictsPassthrough(t *testing.T) {
	// Dicts 格式消息也能正确处理
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	dictMsgs := []map[string]any{
		{"role": "user", "content": "你好"},
		{"role": "assistant", "content": "你好！"},
	}
	msgs := model_clients.NewDictsMessagesParam(dictMsgs)
	enrichedMsgs, err := client.enrichMessagesWithReasoningContent(msgs)
	if err != nil {
		t.Fatalf("enrichMessagesWithReasoningContent 失败: %v", err)
	}

	dicts := enrichedMsgs.Dicts()
	assistantDict := dicts[1]
	if _, exists := assistantDict["reasoning_content"]; !exists {
		t.Error("Dicts 格式的 assistant 消息也应补充 reasoning_content")
	}
}

func TestEnrichMessages_TextInput(t *testing.T) {
	// 纯文本输入也能正确处理（自动包装为 UserMessage，没有 assistant 消息不需要补）
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msgs := model_clients.NewTextMessagesParam("你好")
	enrichedMsgs, err := client.enrichMessagesWithReasoningContent(msgs)
	if err != nil {
		t.Fatalf("enrichMessagesWithReasoningContent 失败: %v", err)
	}

	dicts := enrichedMsgs.Dicts()
	if len(dicts) != 1 {
		t.Fatalf("dicts 长度 = %d, want 1", len(dicts))
	}
	if dicts[0]["role"] != "user" {
		t.Errorf("role = %q, want user", dicts[0]["role"])
	}
}

func TestEnrichMessages_EmptyMessages(t *testing.T) {
	// 空消息应返回错误
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msgs := model_clients.MessagesParam{}
	_, err = client.enrichMessagesWithReasoningContent(msgs)
	if err == nil {
		t.Error("空消息应返回错误")
	}
}

// ──────────────────────────── Invoke 集成测试 ────────────────────────────

func TestDeepSeekModelClient_Invoke_成功(t *testing.T) {
	// 使用 httptest 模拟 DeepSeek API 返回 200
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
		_, _ = fmt.Fprint(w, mockCompletionResponse("你好！我是 DeepSeek"))
	}))
	defer server.Close()

	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", server.URL),
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
	if result.Content.Text() != "你好！我是 DeepSeek" {
		t.Errorf("Content = %q, want %q", result.Content.Text(), "你好！我是 DeepSeek")
	}
}

func TestDeepSeekModelClient_Invoke_请求体含ReasoningContent(t *testing.T) {
	// 验证多轮对话中 assistant 消息的 reasoning_content 被正确传给 API
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponse("好的"))
	}))
	defer server.Close()

	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 模拟多轮对话：user → assistant(无 reasoning_content) → user
	msgs := model_clients.NewMessagesParam(
		llmschema.NewUserMessage("你好"),
		llmschema.NewAssistantMessage("你好！"),
		llmschema.NewUserMessage("1+1=?"),
	)
	result, err := client.Invoke(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("Invoke 结果不应为 nil")
	}

	// 验证请求体中 assistant 消息包含 reasoning_content
	messages, _ := receivedBody["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages 长度 = %d, want 3", len(messages))
	}

	// 第二条是 assistant 消息，应有 reasoning_content
	assistantMsg, _ := messages[1].(map[string]any)
	if role, _ := assistantMsg["role"].(string); role != "assistant" {
		t.Errorf("messages[1].role = %q, want assistant", role)
	}
	if rc, exists := assistantMsg["reasoning_content"]; !exists {
		t.Error("请求体中 assistant 消息应包含 reasoning_content")
	} else if rc != "" {
		t.Errorf("reasoning_content = %q, want 空字符串", rc)
	}
}

func TestDeepSeekModelClient_Invoke_保留已有ReasoningContent(t *testing.T) {
	// 已有 reasoning_content 的 assistant 消息不应被覆盖
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponse("继续"))
	}))
	defer server.Close()

	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	assistantMsg := llmschema.NewAssistantMessage(
		"你好！",
		llmschema.WithReasoningContent("让我想想..."),
	)
	msgs := model_clients.NewMessagesParam(
		llmschema.NewUserMessage("你好"),
		assistantMsg,
		llmschema.NewUserMessage("继续"),
	)
	result, err := client.Invoke(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("Invoke 结果不应为 nil")
	}

	// 验证请求体中 reasoning_content 保留原值
	messages, _ := receivedBody["messages"].([]any)
	assistantDict, _ := messages[1].(map[string]any)
	if rc, _ := assistantDict["reasoning_content"].(string); rc != "让我想想..." {
		t.Errorf("reasoning_content = %q, want %q", rc, "让我想想...")
	}
}

func TestDeepSeekModelClient_Invoke_响应含ReasoningContent(t *testing.T) {
	// 验证 DeepSeek 响应中的 reasoning_content 被正确解析
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponseWithReasoning("答案是 42", "我需要计算一下..."))
	}))
	defer server.Close()

	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", server.URL),
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

func TestDeepSeekModelClient_Invoke_HTTP错误(t *testing.T) {
	// 使用 httptest 返回 401，验证错误处理
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":{"message":"Incorrect API key","type":"invalid_request_error","code":"invalid_api_key"}}`)
	}))
	defer server.Close()

	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", server.URL),
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

func TestDeepSeekModelClient_Stream_成功(t *testing.T) {
	// 使用 httptest 模拟 SSE 流，验证 Stream 结果
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("请求方法 = %s, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"deepseek-chat","choices":[{"index":0,"delta":{"role":"assistant","content":"He"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"deepseek-chat","choices":[{"index":0,"delta":{"content":"llo"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"deepseek-chat","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
			`data: [DONE]`,
		}

		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Stream(context.Background(), msg)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("Stream 结果不应为 nil")
	}

	// Final() 会阻塞等待流结束，然后返回合并结果
	final := result.Final()
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

func TestDeepSeekModelClient_Stream_请求体含ReasoningContent(t *testing.T) {
	// 验证流式调用中 assistant 消息的 reasoning_content 也被正确处理
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"deepseek-chat","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 多轮对话包含 assistant 消息
	msgs := model_clients.NewMessagesParam(
		llmschema.NewUserMessage("你好"),
		llmschema.NewAssistantMessage("你好！"),
		llmschema.NewUserMessage("继续"),
	)
	_, err = client.Stream(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	// 验证请求体中 assistant 消息包含 reasoning_content
	messages, _ := receivedBody["messages"].([]any)
	assistantDict, _ := messages[1].(map[string]any)
	if _, exists := assistantDict["reasoning_content"]; !exists {
		t.Error("流式请求体中 assistant 消息应包含 reasoning_content")
	}
}

func TestDeepSeekModelClient_Stream_HTTP错误(t *testing.T) {
	// 使用 httptest 返回非 200，验证错误处理
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`)
	}))
	defer server.Close()

	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Stream(context.Background(), msg)
	if err == nil {
		t.Error("Stream HTTP 429 应返回错误")
	}
	if result != nil {
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
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
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

func TestDeepSeekModelClient_GenerateImage_NotSupported(t *testing.T) {
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
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

func TestDeepSeekModelClient_GenerateSpeech_NotSupported(t *testing.T) {
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
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

func TestDeepSeekModelClient_GenerateVideo_NotSupported(t *testing.T) {
	client, err := NewDeepSeekModelClient(
		newTestModelConfig(),
		newTestClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1"),
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

func TestInit_注册DeepSeek(t *testing.T) {
	// 验证 init() 注册了 DeepSeek 到全局注册表
	registry := model_clients.GetClientRegistry()
	clients := registry.ListClients()

	hasDeepSeek := false
	for _, name := range clients {
		if name == "llm_DeepSeek" {
			hasDeepSeek = true
		}
	}
	if !hasDeepSeek {
		t.Error("init() 应注册 llm_DeepSeek")
	}

	// 通过 GetClient 验证 DeepSeek 工厂能正常创建客户端
	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("deepseek-chat"))
	cc := llmschema.NewModelClientConfig("DeepSeek", "test-key", "https://api.deepseek.com/v1",
		llmschema.WithVerifySSL(false),
	)
	client, err := registry.GetClient("DeepSeek", "llm", mc, cc)
	if err != nil {
		t.Fatalf("GetClient(DeepSeek) 报错: %v", err)
	}
	if client == nil {
		t.Error("GetClient(DeepSeek) 应返回非 nil 客户端")
	}

	// 验证创建的客户端是 DeepSeekModelClient 类型
	deepseekClient, ok := client.(*DeepSeekModelClient)
	if !ok {
		t.Fatalf("GetClient(DeepSeek) 应返回 *DeepSeekModelClient, got %T", client)
	}
	if deepseekClient.GetClientName() != "DeepSeek client" {
		t.Errorf("ClientName = %q, want %q", deepseekClient.GetClientName(), "DeepSeek client")
	}
}
