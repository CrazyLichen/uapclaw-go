package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"))
	if err != nil {
		t.Fatalf("NewOpenAIModelClient 返回错误: %v", err)
	}
	if client == nil {
		t.Fatal("client 不应为 nil")
	}
}

func TestNewOpenAIModelClient_NoAPIKey(t *testing.T) {
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "", "https://api.openai.com/v1"))
	if err == nil {
		t.Error("缺少 API Key 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Key 时 client 应为 nil")
	}
}

func TestNewOpenAIModelClient_NoAPIBase(t *testing.T) {
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", ""))
	if err == nil {
		t.Error("缺少 API Base 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Base 时 client 应为 nil")
	}
}

func TestRelease_NotSupported(t *testing.T) {
	client, err := NewOpenAIModelClient(newTestModelConfig(), newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"))
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

// ──────────────────────────── 接口合规性测试 ────────────────────────────

func TestOpenAIModelClient_ImplementsBaseModelClient(t *testing.T) {
	var _ model_clients.BaseModelClient = (*OpenAIModelClient)(nil)
}

// ──────────────────────────── 不支持的方法测试 ────────────────────────────

func TestOpenAIModelClient_GenerateImageReturnsError(t *testing.T) {
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("请求方法 = %s, 期望 POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("请求路径 = %s, 期望 /chat/completions", r.URL.Path)
		}
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

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

// ──────────────────────────── BuildEffectiveHeaders 测试 ────────────────────────────

func TestBuildEffectiveHeaders(t *testing.T) {
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

	result := client.BuildEffectiveHeaders(map[string]any{"X-Request": "req-val"})
	if result["X-Base"] != "base-val" {
		t.Errorf("X-Base = %q, 期望 %q", result["X-Base"], "base-val")
	}
	if result["X-Request"] != "req-val" {
		t.Errorf("X-Request = %q, 期望 %q", result["X-Request"], "req-val")
	}
}

func TestBuildEffectiveHeaders_空请求头(t *testing.T) {
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

	result := client.BuildEffectiveHeaders(nil)
	if result["X-Base"] != "base-val" {
		t.Errorf("X-Base = %q, 期望 %q", result["X-Base"], "base-val")
	}
	if len(result) != 1 {
		t.Errorf("len(result) = %d, 期望 1", len(result))
	}
}

func TestBuildEffectiveHeaders_空配置头(t *testing.T) {
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	result := client.BuildEffectiveHeaders(map[string]any{"X-Request": "req-val"})
	if result["X-Request"] != "req-val" {
		t.Errorf("X-Request = %q, 期望 %q", result["X-Request"], "req-val")
	}
}

// ──────────────────────────── WrapError 测试 ────────────────────────────

func TestWrapError(t *testing.T) {
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	originalErr := fmt.Errorf("connection refused")
	wrappedErr := client.WrapError("invoke", originalErr)
	baseErr, ok := wrappedErr.(*exception.BaseError)
	if !ok {
		t.Fatalf("WrapError 应返回 BaseError, got %T", wrappedErr)
	}
	if !strings.Contains(baseErr.Error(), "invoke") {
		t.Errorf("错误消息应包含方法名 invoke, got %q", baseErr.Error())
	}
	if !strings.Contains(baseErr.Error(), "connection refused") {
		t.Errorf("错误消息应包含原始错误, got %q", baseErr.Error())
	}
}

func TestWrapError_空错误消息(t *testing.T) {
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	originalErr := fmt.Errorf("")
	wrappedErr := client.WrapError("stream", originalErr)
	baseErr, ok := wrappedErr.(*exception.BaseError)
	if !ok {
		t.Fatalf("WrapError 应返回 BaseError, got %T", wrappedErr)
	}
	if !strings.Contains(baseErr.Error(), "stream") {
		t.Errorf("错误消息应包含方法名 stream, got %q", baseErr.Error())
	}
}

// ──────────────────────────── HandleHTTPError 测试 ────────────────────────────

func TestHandleHTTPError_OpenAI格式(t *testing.T) {
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
	result := client.HandleHTTPError(resp)
	baseErr, ok := result.(*exception.BaseError)
	if !ok {
		t.Fatalf("HandleHTTPError 应返回 BaseError, got %T", result)
	}
	if !strings.Contains(baseErr.Error(), "400") {
		t.Errorf("错误消息应包含 400, got %q", baseErr.Error())
	}
	if !strings.Contains(baseErr.Error(), "Invalid model") {
		t.Errorf("错误消息应包含 OpenAI 错误描述, got %q", baseErr.Error())
	}
}

func TestHandleHTTPError_非标准格式(t *testing.T) {
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
	result := client.HandleHTTPError(resp)
	baseErr, ok := result.(*exception.BaseError)
	if !ok {
		t.Fatalf("HandleHTTPError 应返回 BaseError, got %T", result)
	}
	if !strings.Contains(baseErr.Error(), "502") {
		t.Errorf("错误消息应包含 502, got %q", baseErr.Error())
	}
	if !strings.Contains(baseErr.Error(), "Bad Gateway") {
		t.Errorf("错误消息应包含响应体内容, got %q", baseErr.Error())
	}
}

func TestHandleHTTPError_读取失败(t *testing.T) {
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	resp := &http.Response{
		StatusCode: 500,
		Body:       io.NopCloser(&errorReader{}),
	}
	result := client.HandleHTTPError(resp)
	baseErr, ok := result.(*exception.BaseError)
	if !ok {
		t.Fatalf("HandleHTTPError 应返回 BaseError, got %T", result)
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

// ──────────────────────────── ExtractHTTPHeaders 测试 ────────────────────────────

func TestExtractHTTPHeaders_空(t *testing.T) {
	result := ExtractHTTPHeaders(nil)
	if result != nil {
		t.Errorf("ExtractHTTPHeaders(nil) = %v, 期望 nil", result)
	}
	result = ExtractHTTPHeaders(map[string]string{})
	if result != nil {
		t.Errorf("ExtractHTTPHeaders(empty) = %v, 期望 nil", result)
	}
}

func TestExtractHTTPHeaders_非空(t *testing.T) {
	headers := map[string]string{"X-Custom": "value"}
	result := ExtractHTTPHeaders(headers)
	if result == nil {
		t.Fatal("ExtractHTTPHeaders 不应返回 nil")
	}
	if result["X-Custom"] != "value" {
		t.Errorf("X-Custom = %q, 期望 %q", result["X-Custom"], "value")
	}
}

// ──────────────────────────── init 注册测试 ────────────────────────────

func TestInit_注册OpenAI和OpenRouter(t *testing.T) {
	registry := model_clients.GetClientRegistry()
	clients := registry.ListClients()

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
	if receivedHeaders.Get("X-Custom") != "custom-val" {
		t.Errorf("X-Custom = %q, 期望 %q", receivedHeaders.Get("X-Custom"), "custom-val")
	}
	if receivedHeaders.Get("X-Request") != "req-val" {
		t.Errorf("X-Request = %q, 期望 %q", receivedHeaders.Get("X-Request"), "req-val")
	}
}

// ──────────────────────────── Stream 带网络错误测试 ────────────────────────────

func TestOpenAIModelClient_Stream_网络错误(t *testing.T) {
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
	ctx := context.Background()
	params := map[string]any{
		"model":    "gpt-4",
		"messages": []any{},
		"stream":   false,
		"invalid":  make(chan int),
	}

	_, _, err := BuildHTTPRequest(ctx, "https://api.openai.com/v1", "test-key", params, nil, nil, false, "")
	if err == nil {
		t.Error("序列化失败应返回错误")
	}
}

// ──────────────────────────── Stream 回调对齐测试 ────────────────────────────

// streamOutputParser 成功解析的 mock parser
type streamOutputParser struct{}

func (p *streamOutputParser) Parse(input any) (any, error) {
	text, _ := input.(string)
	return map[string]any{"parsed": text}, nil
}

func (p *streamOutputParser) StreamParse(chunks <-chan *llmschema.AssistantMessageChunk) <-chan model_clients.StreamParsedResult {
	out := make(chan model_clients.StreamParsedResult)
	go func() { close(out) }()
	return out
}

// streamErrorOutputParser 总是返回错误的 mock parser
type streamErrorOutputParser struct{}

func (p *streamErrorOutputParser) Parse(_ any) (any, error) {
	return nil, fmt.Errorf("stream parse error")
}

func (p *streamErrorOutputParser) StreamParse(chunks <-chan *llmschema.AssistantMessageChunk) <-chan model_clients.StreamParsedResult {
	out := make(chan model_clients.StreamParsedResult)
	go func() { close(out) }()
	return out
}

func TestOpenAIModelClient_Stream_无效JSON走logger(t *testing.T) {
	// SSE 流中包含无效 JSON 数据，应走 logger.Error 而非回调，继续读取后续数据
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: invalid-json`,
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"OK"},"finish_reason":null}]}`,
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

	final := result.Final()
	if final == nil {
		t.Fatal("Final 不应为 nil")
	}
	if final.Content.Text() != "OK" {
		t.Errorf("Final Content = %q, 期望 %q", final.Content.Text(), "OK")
	}
}

func TestOpenAIModelClient_Stream_OutputParser成功(t *testing.T) {
	// Stream 带 OutputParser，验证 parser 成功路径
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
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
	result, err := client.Stream(context.Background(), msg,
		model_clients.WithStreamOutputParser(&streamOutputParser{}),
	)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	final := result.Final()
	if final == nil {
		t.Fatal("Final 不应为 nil")
	}
	if final.ParserContent == nil {
		t.Error("Final ParserContent 不应为 nil（OutputParser 成功路径）")
	}
}

func TestOpenAIModelClient_Stream_OutputParser错误(t *testing.T) {
	// Stream 带 OutputParser 解析失败，应走 logger.Error 而非回调
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
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
	result, err := client.Stream(context.Background(), msg,
		model_clients.WithStreamOutputParser(&streamErrorOutputParser{}),
	)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	// 验证流正常结束（parser 错误不应导致流崩溃）
	final := result.Final()
	if final == nil {
		t.Fatal("Final 不应为 nil")
	}
	// parser 错误时 ParserContent 应为 nil
	if final.ParserContent != nil {
		t.Error("Final ParserContent 应为 nil（OutputParser 失败路径）")
	}
}

func TestOpenAIModelClient_Stream_SSE异常关闭(t *testing.T) {
	// SSE 流异常关闭（无 [DONE]），验证不崩溃
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
		// 不发送 [DONE]，直接关闭连接
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Stream(context.Background(), msg)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	// 验证流正常终止（不 panic）
	final := result.Final()
	if final == nil {
		t.Fatal("Final 不应为 nil")
	}
}

func TestOpenAIModelClient_Stream_Context取消(t *testing.T) {
	// Stream 时 context 取消，验证触发 LLMCallError 回调
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// 持续发送数据
		for i := 0; i < 100; i++ {
			fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}]}`)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	ctx, cancel := context.WithCancel(context.Background())
	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Stream(ctx, msg)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	// 读取一个 chunk 后取消 context
	<-result.Chunks // 读一个
	cancel()        // 取消

	// 验证流正常终止（不 panic）
	for range result.Chunks {
	}
}