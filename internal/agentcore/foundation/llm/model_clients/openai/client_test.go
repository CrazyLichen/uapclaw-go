package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestClientConfig 创建测试用的客户端配置（关闭 SSL 验证以避免证书要求）
func newTestClientConfig(provider, apiKey, apiBase string) *llmschema.ModelClientConfig {
	cc, err := llmschema.NewModelClientConfig(provider, apiKey, apiBase, llmschema.WithVerifySSL(false))
	if err != nil {
		return nil
	}
	return cc
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
	// NewModelClientConfig 本身就应拒绝缺少 API Key 的配置
	_, err := llmschema.NewModelClientConfig("OpenAI", "", "https://api.openai.com/v1", llmschema.WithVerifySSL(false))
	if err == nil {
		t.Error("缺少 API Key 时 NewModelClientConfig 应返回错误")
	}
}

func TestNewOpenAIModelClient_NoAPIBase(t *testing.T) {
	// NewModelClientConfig 本身就应拒绝缺少 API Base 的配置
	_, err := llmschema.NewModelClientConfig("OpenAI", "test-key", "", llmschema.WithVerifySSL(false))
	if err == nil {
		t.Error("缺少 API Base 时 NewModelClientConfig 应返回错误")
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
		_, _ = fmt.Fprint(w, mockOpenAICompletionResponse("Hello!"))
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
		_, _ = fmt.Fprint(w, `{"error":{"message":"Incorrect API key","type":"invalid_request_error","code":"invalid_api_key"}}`)
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
		_, _ = fmt.Fprint(w, `internal server error`)
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
		_, _ = fmt.Fprint(w, "this is not json")
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
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
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
		t.Errorf("Final Content = %q, 期望 %q", final.Content.Text(), "Hello")
	}
	if final.FinishReason != "stop" {
		t.Errorf("Final FinishReason = %q, 期望 %q", final.FinishReason, "stop")
	}
}

func TestOpenAIModelClient_Stream_HTTP错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`)
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
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

// ──────────────────────────── BuildEffectiveHeaders 测试 ────────────────────────────

func TestBuildEffectiveHeaders(t *testing.T) {
	clientCfg, err := llmschema.NewModelClientConfig("OpenAI", "test-key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
		llmschema.WithCustomHeaders(map[string]string{"X-Base": "base-val"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		clientCfg,
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	result := client.BuildEffectiveHeaders(map[string]string{"X-Request": "req-val"})
	if result["X-Base"] != "base-val" {
		t.Errorf("X-Base = %q, 期望 %q", result["X-Base"], "base-val")
	}
	if result["X-Request"] != "req-val" {
		t.Errorf("X-Request = %q, 期望 %q", result["X-Request"], "req-val")
	}
}

func TestBuildEffectiveHeaders_空请求头(t *testing.T) {
	clientCfg, err := llmschema.NewModelClientConfig("OpenAI", "test-key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
		llmschema.WithCustomHeaders(map[string]string{"X-Base": "base-val"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		clientCfg,
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

	result := client.BuildEffectiveHeaders(map[string]string{"X-Request": "req-val"})
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
	cc, err := llmschema.NewModelClientConfig("OpenAI", "test-key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
	)
	if err != nil {
		t.Fatal(err)
	}
	client, err := registry.GetClient("OpenAI", "llm", mc, cc)
	if err != nil {
		t.Fatalf("GetClient(OpenAI) 报错: %v", err)
	}
	if client == nil {
		t.Error("GetClient(OpenAI) 应返回非 nil 客户端")
	}

	cc2, err := llmschema.NewModelClientConfig("OpenRouter", "test-key", "https://openrouter.ai/api/v1",
		llmschema.WithVerifySSL(false),
	)
	if err != nil {
		t.Fatal(err)
	}
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
		_, _ = fmt.Fprint(w, mockOpenAICompletionResponse("ok"))
	}))
	defer server.Close()

	client := newTestClientWithServer(server, map[string]string{"X-Custom": "custom-val"})
	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Invoke(context.Background(), msg,
		model_clients.WithInvokeCustomHeaders(map[string]string{"X-Request": "req-val"}),
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
	chunkChan, err := client.Stream(context.Background(), msg)
	if err == nil {
		t.Error("Stream 网络错误应返回错误")
	}
	if chunkChan != nil {
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

	req, _, err := BuildHTTPRequest(ctx, "https://api.openai.com/v1", "test-key", params, headers, nil, false, "", false)
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

	_, client, err := BuildHTTPRequest(ctx, "https://api.openai.com/v1", "test-key", params, nil, &timeout, false, "", false)
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

	_, _, err := BuildHTTPRequest(ctx, "https://api.openai.com/v1", "test-key", params, nil, nil, false, "", false)
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

func (p *streamOutputParser) StreamParse(chunks <-chan any) <-chan model_clients.StreamParsedResult {
	out := make(chan model_clients.StreamParsedResult)
	go func() { close(out) }()
	return out
}

// streamErrorOutputParser 总是返回错误的 mock parser
type streamErrorOutputParser struct{}

func (p *streamErrorOutputParser) Parse(_ any) (any, error) {
	return nil, fmt.Errorf("stream parse error")
}

func (p *streamErrorOutputParser) StreamParse(chunks <-chan any) <-chan model_clients.StreamParsedResult {
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
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
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
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	msg := model_clients.NewTextMessagesParam("Hi")
	chunkChan, err := client.Stream(context.Background(), msg,
		model_clients.WithStreamOutputParser(&streamOutputParser{}),
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
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	msg := model_clients.NewTextMessagesParam("Hi")
	chunkChan, err := client.Stream(context.Background(), msg,
		model_clients.WithStreamOutputParser(&streamErrorOutputParser{}),
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
			_, _ = fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
		// 不发送 [DONE]，直接关闭连接
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
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

func TestOpenAIModelClient_Stream_Context取消(t *testing.T) {
	// Stream 时 context 取消，验证触发 LLMCallError 回调
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// 持续发送数据
		for i := 0; i < 100; i++ {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}]}`)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
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

// TestOpenAIModelClient_SupportsKVCacheRelease 验证 OpenAI 客户端不支持 KV Cache 释放。
func TestOpenAIModelClient_SupportsKVCacheRelease(t *testing.T) {
	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("gpt-4"))
	cc, err := llmschema.NewModelClientConfig("OpenAI", "test-key", "https://api.openai.com/v1", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewOpenAIModelClient(mc, cc)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	if client.SupportsKVCacheRelease() {
		t.Error("OpenAI 客户端不应支持 KV Cache 释放")
	}
}

// TestWrapError_PreservesCause 防错误链回归：包装后 errors.Is 仍可命中原始错误，
// 且对用户可见的错误文案保持不变。
func TestWrapError_PreservesCause(t *testing.T) {
	c := &OpenAIModelClient{}
	wrapped := c.WrapError("stream", context.Canceled)

	if !errors.Is(wrapped, context.Canceled) {
		t.Fatal("WrapError 应保留 cause，使 errors.Is 可命中 context.Canceled")
	}
	if !strings.Contains(wrapped.Error(), "OpenAI API async stream error") {
		t.Fatalf("错误文案不得改变，实际: %s", wrapped.Error())
	}
}

// TestOpenAIModelClient_Stream_中断上抛 验证"流被切断不再被当成正常结束"：
// 服务端写完半截 SSE 就断开（没有终止 chunk），客户端必须发出带 StreamError 的
// 哨兵 chunk，让上层能按 MODEL_CALL_FAILED 处理，而不是把半截内容当完整回答。
func TestOpenAIModelClient_Stream_中断上抛(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("响应不支持 hijack，无法构造中断场景")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack 失败: %v", err)
			return
		}
		// 手工写响应头 + 一个合法 chunked 段，然后不发终止段直接断开：
		// 客户端读到的就是 "unexpected EOF" 这类读错误。
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n")
		payload := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"半截"},"finish_reason":null}]}` + "\n\n"
		_, _ = fmt.Fprintf(buf, "%x\r\n%s\r\n", len(payload), payload)
		_ = buf.Flush()
		_ = conn.Close()
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	chunkChan, err := client.Stream(context.Background(), model_clients.NewTextMessagesParam("Hi"))
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	var sawError error
	content := ""
	for chunk := range chunkChan {
		if chunk.StreamError != nil {
			sawError = chunk.StreamError
			continue
		}
		content += chunk.Content.Text()
	}
	if sawError == nil {
		t.Fatal("流中断必须通过 StreamError 上抛，实际未收到")
	}
	if !strings.Contains(sawError.Error(), "181001") {
		t.Errorf("中断错误应带 MODEL_CALL_FAILED(181001), 实际 %v", sawError)
	}
	if content != "半截" {
		t.Errorf("已收到的内容应保留, 实际 %q", content)
	}
}

// ──────────────────────────── 重试相关头部 ────────────────────────────

// TestHandleHTTPError_保留重试相关头部 验证 429 时 retry-after / x-should-retry 落入 params。
func TestHandleHTTPError_保留重试相关头部(t *testing.T) {
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	resp := &http.Response{
		StatusCode: 429,
		Header: http.Header{
			"Retry-After":    []string{"3"},
			"X-Should-Retry": []string{"false"},
		},
		Body: io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited","code":"rate_limit","type":"rate_limit_error"}}`)),
	}
	result := client.HandleHTTPError(resp)
	baseErr, ok := result.(*exception.BaseError)
	if !ok {
		t.Fatalf("应返回 BaseError，实际 %T", result)
	}
	params := baseErr.Params()
	if got := params["http_status"]; got != 429 {
		t.Fatalf("http_status 期望 429，实际 %v", got)
	}
	if got := params["retry_after"]; got != "3" {
		t.Fatalf("retry_after 期望 \"3\"，实际 %v", got)
	}
	if got := params["x_should_retry"]; got != "false" {
		t.Fatalf("x_should_retry 期望 \"false\"，实际 %v", got)
	}
}

// TestHandleHTTPError_非标准体也保留头部 验证非 OpenAI 错误体时同样带状态码与头部。
func TestHandleHTTPError_非标准体也保留头部(t *testing.T) {
	client, err := NewOpenAIModelClient(
		newTestModelConfig(),
		newTestClientConfig("OpenAI", "test-key", "https://api.openai.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	resp := &http.Response{
		StatusCode: 503,
		Header:     http.Header{"Retry-After-Ms": []string{"1500"}},
		Body:       io.NopCloser(strings.NewReader("upstream unavailable")),
	}
	result := client.HandleHTTPError(resp)
	baseErr, ok := result.(*exception.BaseError)
	if !ok {
		t.Fatalf("应返回 BaseError，实际 %T", result)
	}
	params := baseErr.Params()
	if got := params["http_status"]; got != 503 {
		t.Fatalf("http_status 期望 503，实际 %v", got)
	}
	if got := params["retry_after_ms"]; got != "1500" {
		t.Fatalf("retry_after_ms 期望 \"1500\"，实际 %v", got)
	}
}

// ──────────────────────────── 流式超时语义回归（C-A3）────────────────────────────

// TestStream_持续输出超过总超时不被切断 C-01 核心回归。
//
// 12 块 × 100ms = 1.2s 的持续吐字流，配置 timeout=0.8s：
// 流式无总超时 → 全部 12 块正常收完（旧实现会在 0.8s 处硬切断）。
func TestStream_持续输出超过总超时不被切断(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("应支持 Flush")
			return
		}
		for i := 0; i < 12; i++ {
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
			flusher.Flush()
			time.Sleep(100 * time.Millisecond)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	cc := &llmschema.ModelClientConfig{
		ClientProvider: "OpenAI", APIKey: "k", APIBase: srv.URL,
		Timeout: 0.8, VerifySSL: false,
	}
	c, err := NewOpenAIModelClient(llmschema.NewModelRequestConfig(llmschema.WithModelName("m")), cc)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := c.Stream(context.Background(), model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
	if err != nil {
		t.Fatal(err)
	}
	chunks := 0
	for chunk := range ch {
		if chunk.StreamError != nil {
			t.Fatalf("流内错误: %v", chunk.StreamError)
		}
		if chunk.Content.Text() != "" {
			chunks++
		}
	}
	if chunks != 12 {
		t.Errorf("应收满 12 块（1.2s > 0.8s 总超时），实际 %d", chunks)
	}
}

// TestInvoke_超过总超时被切断 非流式对照：总超时仍生效（Client.Timeout）。
func TestInvoke_超过总超时被切断(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(1500 * time.Millisecond) // 超过 0.8s
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	cc := &llmschema.ModelClientConfig{
		ClientProvider: "OpenAI", APIKey: "k", APIBase: srv.URL,
		Timeout: 0.8, VerifySSL: false,
	}
	c, err := NewOpenAIModelClient(llmschema.NewModelRequestConfig(llmschema.WithModelName("m")), cc)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Invoke(context.Background(), model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
	if err == nil {
		t.Fatal("超过总超时应返回错误")
	}
}

// TestInvoke_编码错误原样上抛 编码失败不应被包成 MODEL_CALL_FAILED（避免误重试）。
//
// Python: openai_model_client.py:2011 details=={"stage":"request_encoding"} 时 re-raise。
func TestInvoke_编码错误原样上抛(t *testing.T) {
	cc := &llmschema.ModelClientConfig{ClientProvider: "OpenAI", APIKey: "k",
		APIBase: "http://127.0.0.1:1", Timeout: 1, VerifySSL: false}
	c, err := NewOpenAIModelClient(llmschema.NewModelRequestConfig(llmschema.WithModelName("m")), cc)
	if err != nil {
		t.Fatal(err)
	}
	// 用 NaN 触发编码失败（经由 WithInvokeMaxTokens 之外的 Extra 通道不可行，
	// 直接构造含非法 UTF-8 的 dict 消息）
	msgs := model_clients.NewDictsMessagesParam([]map[string]any{
		{"role": "user", "content": "bad\xff"},
	})
	_, err = c.Invoke(context.Background(), msgs)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !IsRequestEncodingErr(err) {
		t.Fatalf("应保留 request_encoding 分类，实际 %v", err)
	}
	var base *exception.BaseError
	if !errors.As(err, &base) || base.Code() != 181004 {
		t.Fatalf("错误码应为 181004，实际 %v", err)
	}
}
