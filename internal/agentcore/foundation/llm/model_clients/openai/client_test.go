package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestClientConfig 创建测试用的客户端配置（关闭 SSL 验证以避免证书要求）
func newTestClientConfig(provider, apiKey, apiBase string) *llmschema.ModelClientConfig {
	return llmschema.NewModelClientConfig(provider, apiKey, apiBase, llmschema.WithVerifySSL(false))
}

// newTestModelConfig 创建测试用的模型请求配置
func newTestModelConfig() *llmschema.ModelRequestConfig {
	return llmschema.NewModelRequestConfig(llmschema.WithModelName("gpt-4"))
}

// ──────────────────────────── NewOpenAIModelClient 测试 ────────────────────────────

func TestNewOpenAIModelClient_ValidConfig(t *testing.T) {
	// 有效配置创建客户端成功
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"))
	if err != nil {
		t.Fatalf("NewOpenAIModelClient 返回错误: %v", err)
	}
	if client == nil {
		t.Fatal("client 不应为 nil")
	}
}

func TestNewOpenAIModelClient_NoAPIKey(t *testing.T) {
	// 缺少 API Key 应失败
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "", "https://api.openai.com/v1"))
	if err == nil {
		t.Error("缺少 API Key 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Key 时 client 应为 nil")
	}
}

func TestNewOpenAIModelClient_NoAPIBase(t *testing.T) {
	// 缺少 API Base 应失败
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", ""))
	if err == nil {
		t.Error("缺少 API Base 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Base 时 client 应为 nil")
	}
}

// ──────────────────────────── 接口合规性测试 ────────────────────────────

func TestOpenAIModelClient_ImplementsBaseModelClient(t *testing.T) {
	// 验证 OpenAIModelClient 实现了 BaseModelClient 接口
	var _ model_clients.BaseModelClient = (*OpenAIModelClient)(nil)
}

// ──────────────────────────── 不支持的方法测试 ────────────────────────────

func TestOpenAIModelClient_GenerateImageReturnsError(t *testing.T) {
	// GenerateImage 应返回错误
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"))
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
}

func TestOpenAIModelClient_GenerateSpeechReturnsError(t *testing.T) {
	// GenerateSpeech 应返回错误
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"))
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
}

func TestOpenAIModelClient_GenerateVideoReturnsError(t *testing.T) {
	// GenerateVideo 应返回错误
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"))
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
}

// ──────────────────────────── Invoke 测试 ────────────────────────────

// mockOpenAICompletionResponse 构造 OpenAI Chat Completion 响应 JSON
func mockOpenAICompletionResponse(content string) string {
	return fmt.Sprintf(`{"id":"chatcmpl-123","object":"chat.completion","created":1234567890,"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, content)
}

// newTestClientWithServer 创建使用 httptest 服务器的 OpenAI 客户端
func newTestClientWithServer(server *httptest.Server, customHeaders map[string]string) *OpenAIModelClient {
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", server.URL),
	)
	if err != nil {
		panic(err)
	}
	if customHeaders != nil {
		client.baseHeaders = customHeaders
	}
	return client
}

func TestOpenAIModelClient_Invoke_成功(t *testing.T) {
	// 使用 httptest 模拟 OpenAI API 返回 200，验证 Invoke 返回正确 AssistantMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求方法和路径
		if r.Method != http.MethodPost {
			t.Errorf("请求方法 = %s, 期望 POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("请求路径 = %s, 期望 /chat/completions", r.URL.Path)
		}
		// 验证 Authorization 头
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, 期望 %q", auth, "Bearer test-key")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, mockOpenAICompletionResponse("Hello!"))
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Invoke(context.Background(), msg)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("Invoke 结果不应为 nil")
	}
	if result.Content.Text() != "Hello!" {
		t.Errorf("Content = %q, 期望 %q", result.Content.Text(), "Hello!")
	}
	if result.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, 期望 %q", result.FinishReason, "stop")
	}
}

func TestOpenAIModelClient_Invoke_HTTP错误(t *testing.T) {
	// 使用 httptest 返回 401，验证错误处理
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"Incorrect API key","type":"invalid_request_error","code":"invalid_api_key"}}`)
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Invoke(context.Background(), msg)
	if err == nil {
		t.Error("Invoke HTTP 401 应返回错误")
	}
	if result != nil {
		t.Error("Invoke HTTP 401 结果应为 nil")
	}
	// 验证错误类型
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "401") {
		t.Errorf("错误消息应包含 401, got %q", baseErr.Error())
	}
	if !strings.Contains(baseErr.Error(), "Incorrect API key") {
		t.Errorf("错误消息应包含 OpenAI 错误描述, got %q", baseErr.Error())
	}
}

func TestOpenAIModelClient_Invoke_HTTP500(t *testing.T) {
	// 使用 httptest 返回 500，验证非标准错误格式
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `internal server error`)
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Invoke(context.Background(), msg)
	if err == nil {
		t.Error("Invoke HTTP 500 应返回错误")
	}
	if result != nil {
		t.Error("Invoke HTTP 500 结果应为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "500") {
		t.Errorf("错误消息应包含 500, got %q", baseErr.Error())
	}
}

func TestOpenAIModelClient_Invoke_网络错误(t *testing.T) {
	// 使用 httptest.Server 关闭后调用，验证 wrapError
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // 立即关闭

	client := newTestClientWithServer(server, nil)
	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Invoke(context.Background(), msg)
	if err == nil {
		t.Error("Invoke 网络错误应返回错误")
	}
	if result != nil {
		t.Error("Invoke 网络错误结果应为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "invoke") {
		t.Errorf("错误消息应包含方法名 invoke, got %q", baseErr.Error())
	}
}

func TestOpenAIModelClient_Invoke_无效JSON(t *testing.T) {
	// 使用 httptest 返回非 JSON 内容，验证错误
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "this is not json")
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Invoke(context.Background(), msg)
	if err == nil {
		t.Error("Invoke 无效 JSON 应返回错误")
	}
	if result != nil {
		t.Error("Invoke 无效 JSON 结果应为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "解析响应失败") {
		t.Errorf("错误消息应包含解析响应失败, got %q", baseErr.Error())
	}
}

// ──────────────────────────── Stream 测试 ────────────────────────────

func TestOpenAIModelClient_Stream_成功(t *testing.T) {
	// 使用 httptest 模拟 SSE 流，验证 Stream 结果
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("请求方法 = %s, 期望 POST", r.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"He"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"llo"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
			`data: [DONE]`,
		}

		for _, chunk := range chunks {
			fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
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
		t.Errorf("Final Content = %q, 期望 %q", final.Content.Text(), "Hello")
	}
	if final.FinishReason != "stop" {
		t.Errorf("Final FinishReason = %q, 期望 %q", final.FinishReason, "stop")
	}
}

func TestOpenAIModelClient_Stream_HTTP错误(t *testing.T) {
	// 使用 httptest 返回非 200，验证错误
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`)
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
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

// ──────────────────────────── buildEffectiveHeaders 测试 ────────────────────────────

func TestBuildEffectiveHeaders(t *testing.T) {
	// 测试 baseHeaders 和 requestHeaders 合并
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		llmschema.NewModelClientConfig("OpenAI", "test-key", "https://api.openai.com/v1",
			llmschema.WithVerifySSL(false),
			llmschema.WithCustomHeaders(map[string]any{"X-Base": "base-val"}),
		),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	result := client.buildEffectiveHeaders(map[string]any{"X-Request": "req-val"})
	if result["X-Base"] != "base-val" {
		t.Errorf("X-Base = %q, 期望 %q", result["X-Base"], "base-val")
	}
	if result["X-Request"] != "req-val" {
		t.Errorf("X-Request = %q, 期望 %q", result["X-Request"], "req-val")
	}
}

func TestBuildEffectiveHeaders_空请求头(t *testing.T) {
	// 只有 baseHeaders
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		llmschema.NewModelClientConfig("OpenAI", "test-key", "https://api.openai.com/v1",
			llmschema.WithVerifySSL(false),
			llmschema.WithCustomHeaders(map[string]any{"X-Base": "base-val"}),
		),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	result := client.buildEffectiveHeaders(nil)
	if result["X-Base"] != "base-val" {
		t.Errorf("X-Base = %q, 期望 %q", result["X-Base"], "base-val")
	}
	if len(result) != 1 {
		t.Errorf("len(result) = %d, 期望 1", len(result))
	}
}

func TestBuildEffectiveHeaders_空配置头(t *testing.T) {
	// 只有 requestHeaders
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	result := client.buildEffectiveHeaders(map[string]any{"X-Request": "req-val"})
	if result["X-Request"] != "req-val" {
		t.Errorf("X-Request = %q, 期望 %q", result["X-Request"], "req-val")
	}
}

// ──────────────────────────── wrapError 测试 ────────────────────────────

func TestWrapError(t *testing.T) {
	// 测试错误包装
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	originalErr := fmt.Errorf("connection refused")
	wrappedErr := client.wrapError("invoke", originalErr)
	baseErr, ok := wrappedErr.(*exception.BaseError)
	if !ok {
		t.Fatalf("wrapError 应返回 BaseError, got %T", wrappedErr)
	}
	if !strings.Contains(baseErr.Error(), "invoke") {
		t.Errorf("错误消息应包含方法名 invoke, got %q", baseErr.Error())
	}
	if !strings.Contains(baseErr.Error(), "connection refused") {
		t.Errorf("错误消息应包含原始错误, got %q", baseErr.Error())
	}
}

func TestWrapError_空错误消息(t *testing.T) {
	// 测试空错误消息的特殊处理
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 创建一个 Error() 返回空字符串的错误
	originalErr := fmt.Errorf("")
	wrappedErr := client.wrapError("stream", originalErr)
	baseErr, ok := wrappedErr.(*exception.BaseError)
	if !ok {
		t.Fatalf("wrapError 应返回 BaseError, got %T", wrappedErr)
	}
	// 空错误消息时不应包含 "%T: %v" 中 %v 产生的空白
	if !strings.Contains(baseErr.Error(), "stream") {
		t.Errorf("错误消息应包含方法名 stream, got %q", baseErr.Error())
	}
}

// ──────────────────────────── handleHTTPError 测试 ────────────────────────────

func TestHandleHTTPError_OpenAI格式(t *testing.T) {
	// 测试 OpenAI 标准错误格式解析
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	resp := &http.Response{
		StatusCode: 400,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Invalid model","type":"invalid_request_error"}}`)),
	}
	result := client.handleHTTPError(resp)
	baseErr, ok := result.(*exception.BaseError)
	if !ok {
		t.Fatalf("handleHTTPError 应返回 BaseError, got %T", result)
	}
	if !strings.Contains(baseErr.Error(), "400") {
		t.Errorf("错误消息应包含 400, got %q", baseErr.Error())
	}
	if !strings.Contains(baseErr.Error(), "Invalid model") {
		t.Errorf("错误消息应包含 OpenAI 错误描述, got %q", baseErr.Error())
	}
}

func TestHandleHTTPError_非标准格式(t *testing.T) {
	// 测试非标准错误格式
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	resp := &http.Response{
		StatusCode: 502,
		Body:       io.NopCloser(strings.NewReader(`<html>Bad Gateway</html>`)),
	}
	result := client.handleHTTPError(resp)
	baseErr, ok := result.(*exception.BaseError)
	if !ok {
		t.Fatalf("handleHTTPError 应返回 BaseError, got %T", result)
	}
	if !strings.Contains(baseErr.Error(), "502") {
		t.Errorf("错误消息应包含 502, got %q", baseErr.Error())
	}
	if !strings.Contains(baseErr.Error(), "Bad Gateway") {
		t.Errorf("错误消息应包含响应体内容, got %q", baseErr.Error())
	}
}

func TestHandleHTTPError_读取失败(t *testing.T) {
	// 测试读取响应体失败的情况
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 使用一个读取即报错的 ReadCloser
	resp := &http.Response{
		StatusCode: 500,
		Body:       io.NopCloser(&errorReader{}),
	}
	result := client.handleHTTPError(resp)
	baseErr, ok := result.(*exception.BaseError)
	if !ok {
		t.Fatalf("handleHTTPError 应返回 BaseError, got %T", result)
	}
	if !strings.Contains(baseErr.Error(), "500") {
		t.Errorf("错误消息应包含 500, got %q", baseErr.Error())
	}
	if !strings.Contains(baseErr.Error(), "无法读取响应体") {
		t.Errorf("错误消息应包含无法读取响应体, got %q", baseErr.Error())
	}
}

// errorReader 总是返回错误的 io.Reader
type errorReader struct{}

func (r *errorReader) Read(_ []byte) (n int, err error) {
	return 0, fmt.Errorf("read error")
}

// ──────────────────────────── extractHTTPHeaders 测试 ────────────────────────────

func TestExtractHTTPHeaders_空(t *testing.T) {
	// 测试空 headers
	result := extractHTTPHeaders(nil)
	if result != nil {
		t.Errorf("extractHTTPHeaders(nil) = %v, 期望 nil", result)
	}
	result = extractHTTPHeaders(map[string]string{})
	if result != nil {
		t.Errorf("extractHTTPHeaders(empty) = %v, 期望 nil", result)
	}
}

func TestExtractHTTPHeaders_非空(t *testing.T) {
	// 测试非空 headers
	headers := map[string]string{
		"X-Custom": "value",
	}
	result := extractHTTPHeaders(headers)
	if result == nil {
		t.Fatal("extractHTTPHeaders 不应返回 nil")
	}
	if result["X-Custom"] != "value" {
		t.Errorf("X-Custom = %q, 期望 %q", result["X-Custom"], "value")
	}
}

// ──────────────────────────── init 注册测试 ────────────────────────────

func TestInit_注册OpenAI和OpenRouter(t *testing.T) {
	// 验证 init() 注册了 OpenAI 和 OpenRouter 到全局注册表
	registry := model_clients.GetClientRegistry()
	clients := registry.ListClients()

	// 检查 llm_OpenAI 和 llm_OpenRouter 是否存在
	hasOpenAI := false
	hasOpenRouter := false
	for _, name := range clients {
		if name == "llm_OpenAI" {
			hasOpenAI = true
		}
		if name == "llm_OpenRouter" {
			hasOpenRouter = true
		}
	}
	if !hasOpenAI {
		t.Error("init() 应注册 llm_OpenAI")
	}
	if !hasOpenRouter {
		t.Error("init() 应注册 llm_OpenRouter")
	}

	// 通过 GetClient 验证 OpenAI 工厂能正常创建客户端
	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("gpt-4"))
	cc := llmschema.NewModelClientConfig("OpenAI", "test-key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
	)
	client, err := registry.GetClient("OpenAI", "llm", mc, cc)
	if err != nil {
		t.Fatalf("GetClient(OpenAI) 报错: %v", err)
	}
	if client == nil {
		t.Error("GetClient(OpenAI) 应返回非 nil 客户端")
	}

	// 验证 OpenRouter 也能正常创建
	cc2 := llmschema.NewModelClientConfig("OpenRouter", "test-key", "https://openrouter.ai/api/v1",
		llmschema.WithVerifySSL(false),
	)
	client2, err := registry.GetClient("OpenRouter", "llm", mc, cc2)
	if err != nil {
		t.Fatalf("GetClient(OpenRouter) 报错: %v", err)
	}
	if client2 == nil {
		t.Error("GetClient(OpenRouter) 应返回非 nil 客户端")
	}
}

// ──────────────────────────── Invoke 带自定义请求头测试 ────────────────────────────

func TestOpenAIModelClient_Invoke_带自定义请求头(t *testing.T) {
	// 验证自定义请求头被正确设置到 HTTP 请求中
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, mockOpenAICompletionResponse("ok"))
	}))
	defer server.Close()

	client := newTestClientWithServer(server, map[string]string{"X-Custom": "custom-val"})
	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Invoke(context.Background(), msg,
		model_clients.WithInvokeCustomHeaders(map[string]any{"X-Request": "req-val"}),
	)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("Invoke 结果不应为 nil")
	}
	// 验证自定义请求头已设置
	if receivedHeaders.Get("X-Custom") != "custom-val" {
		t.Errorf("X-Custom = %q, 期望 %q", receivedHeaders.Get("X-Custom"), "custom-val")
	}
	if receivedHeaders.Get("X-Request") != "req-val" {
		t.Errorf("X-Request = %q, 期望 %q", receivedHeaders.Get("X-Request"), "req-val")
	}
}

// ──────────────────────────── Stream 带网络错误测试 ────────────────────────────

func TestOpenAIModelClient_Stream_网络错误(t *testing.T) {
	// 使用 httptest.Server 关闭后调用，验证 wrapError
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	client := newTestClientWithServer(server, nil)
	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Stream(context.Background(), msg)
	if err == nil {
		t.Error("Stream 网络错误应返回错误")
	}
	if result != nil {
		t.Error("Stream 网络错误结果应为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "stream") {
		t.Errorf("错误消息应包含方法名 stream, got %q", baseErr.Error())
	}
}

// ──────────────────────────── BuildHTTPRequest 补充测试 ────────────────────────────

func TestBuildHTTPRequest_带自定义请求头(t *testing.T) {
	// 测试自定义请求头被设置到 HTTP 请求中
	ctx := context.Background()
	params := map[string]any{"model": "gpt-4", "messages": []any{}, "stream": false}
	headers := map[string]string{"X-Custom": "value"}

	req, _, err := BuildHTTPRequest(ctx, "https://api.openai.com/v1", "test-key", params, headers, nil, false, "")
	if err != nil {
		t.Fatalf("BuildHTTPRequest 报错: %v", err)
	}
	if req.Header.Get("X-Custom") != "value" {
		t.Errorf("X-Custom = %q, 期望 %q", req.Header.Get("X-Custom"), "value")
	}
}

func TestBuildHTTPRequest_带超时(t *testing.T) {
	// 测试自定义超时
	ctx := context.Background()
	params := map[string]any{"model": "gpt-4", "messages": []any{}, "stream": false}
	timeout := 30.0

	_, client, err := BuildHTTPRequest(ctx, "https://api.openai.com/v1", "test-key", params, nil, &timeout, false, "")
	if err != nil {
		t.Fatalf("BuildHTTPRequest 报错: %v", err)
	}
	if client == nil {
		t.Error("client 不应为 nil")
	}
}

func TestBuildHTTPRequest_序列化失败(t *testing.T) {
	// 测试参数包含无法序列化的值
	ctx := context.Background()
	params := map[string]any{
		"model":    "gpt-4",
		"messages": []any{},
		"stream":   false,
		"invalid":  make(chan int), // chan 无法被 JSON 序列化
	}

	_, _, err := BuildHTTPRequest(ctx, "https://api.openai.com/v1", "test-key", params, nil, nil, false, "")
	if err == nil {
		t.Error("序列化失败应返回错误")
	}
}
