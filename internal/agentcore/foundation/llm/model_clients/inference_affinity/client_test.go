package inferenceaffinity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestClientConfig 创建测试用的客户端配置
func newTestClientConfig(provider, apiKey, apiBase string) *llmschema.ModelClientConfig {
	return llmschema.NewModelClientConfig(provider, apiKey, apiBase, llmschema.WithVerifySSL(false))
}

// newTestModelConfig 创建测试用的模型请求配置
func newTestModelConfig() *llmschema.ModelRequestConfig {
	return llmschema.NewModelRequestConfig(llmschema.WithModelName("qwen-72b"))
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
	return fmt.Sprintf(`{"id":"chatcmpl-123","object":"chat.completion","created":1234567890,"model":"qwen-72b","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, content)
}

// ──────────────────────────── NewInferenceAffinityModelClient 测试 ────────────────────────────

func TestNewInferenceAffinityModelClient_ValidConfig(t *testing.T) {
	// 有效配置创建客户端成功
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatalf("NewInferenceAffinityModelClient 返回错误: %v", err)
	}
	if client == nil {
		t.Fatal("client 不应为 nil")
	}
	if client.GetClientName() != "InferenceAffinity client" {
		t.Errorf("ClientName = %q, want %q", client.GetClientName(), "InferenceAffinity client")
	}
}

func TestNewInferenceAffinityModelClient_NoAPIKey(t *testing.T) {
	// 缺少 API Key 应失败
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "", "https://vllm.example.com/v1"),
	)
	if err == nil {
		t.Error("缺少 API Key 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Key 时 client 应为 nil")
	}
}

func TestNewInferenceAffinityModelClient_NoAPIBase(t *testing.T) {
	// 缺少 API Base 应失败
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", ""),
	)
	if err == nil {
		t.Error("缺少 API Base 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Base 时 client 应为 nil")
	}
}

// ──────────────────────────── Invoke 测试 ────────────────────────────

func TestInvoke_BasicCall(t *testing.T) {
	// 基本调用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("请求路径 = %q, 期望 /v1/chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockCompletionResponse("你好！")))
	}))
	defer server.Close()

	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", server.URL+"/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	msg, err := client.Invoke(
		context.Background(),
		model_clients.NewTextMessagesParam("你好"),
	)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if msg.Content.Text() != "你好！" {
		t.Errorf("Content = %q, 期望 %q", msg.Content.Text(), "你好！")
	}
}

func TestInvoke_SanitizeToolCalls(t *testing.T) {
	// 验证 invoke 时 sanitize tool_calls
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody = decodeRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockCompletionResponse("ok")))
	}))
	defer server.Close()

	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", server.URL+"/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	// 构造含非标准 tool_calls 字段的 assistant 消息
	messages := model_clients.NewDictsMessagesParam([]map[string]any{
		{"role": "user", "content": "hello"},
		{
			"role":    "assistant",
			"content": nil,
			"tool_calls": []map[string]any{
				{
					"id":          "call_123",
					"type":        "invalid_type",      // 非标准 type，应被强制为 "function"
					"extra_field": "should_be_removed", // 非标准字段，应被移除
					"function": map[string]any{
						"name":      "get_weather",
						"arguments": "{\"city\":\"Beijing\"}",
					},
				},
			},
		},
		{"role": "tool", "content": "sunny", "tool_call_id": "call_123"},
	})

	_, err = client.Invoke(context.Background(), messages)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}

	// 验证请求体中的 tool_calls 已被清洗
	msgs, _ := capturedBody["messages"].([]any)
	if len(msgs) < 2 {
		t.Fatalf("消息数量 = %d, 期望 >= 2", len(msgs))
	}
	assistantMsg, _ := msgs[1].(map[string]any)
	toolCalls, _ := assistantMsg["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls 数量 = %d, 期望 1", len(toolCalls))
	}
	tc, _ := toolCalls[0].(map[string]any)

	// type 应被强制为 "function"
	if tc["type"] != "function" {
		t.Errorf("type = %v, 期望 function", tc["type"])
	}
	// 非标准字段应被移除
	if _, exists := tc["extra_field"]; exists {
		t.Error("extra_field 应被移除")
	}
	// 标准字段应保留
	if tc["id"] != "call_123" {
		t.Errorf("id = %v, 期望 call_123", tc["id"])
	}
}

func TestInvoke_WithCacheSharing(t *testing.T) {
	// 验证 cache_sharing/cache_salt 参数注入
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody = decodeRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockCompletionResponse("ok")))
	}))
	defer server.Close()

	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", server.URL+"/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Invoke(
		context.Background(),
		model_clients.NewTextMessagesParam("hello"),
		model_clients.WithInvokeExtra(map[string]any{
			"session_id":           "session-123",
			"enable_cache_sharing": true,
		}),
	)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}

	// 验证 cache 参数已注入
	if capturedBody["cache_sharing"] != true {
		t.Errorf("cache_sharing = %v, 期望 true", capturedBody["cache_sharing"])
	}
	if capturedBody["cache_salt"] != "session-123" {
		t.Errorf("cache_salt = %v, 期望 session-123", capturedBody["cache_salt"])
	}
}

func TestInvoke_HTTPError(t *testing.T) {
	// HTTP 错误处理
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", server.URL+"/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("hello"))
	if err == nil {
		t.Error("HTTP 500 应返回错误")
	}
}

// ──────────────────────────── Stream 测试 ────────────────────────────

func TestStream_BasicCall(t *testing.T) {
	// 基本流式调用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// 发送流式数据
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"qwen-72b\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你\"},\"finish_reason\":\"null\"}]}\n\n")
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"qwen-72b\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"好\"},\"finish_reason\":\"null\"}]}\n\n")
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"qwen-72b\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", server.URL+"/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.Stream(
		context.Background(),
		model_clients.NewTextMessagesParam("你好"),
	)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	// 消费所有 chunk
	var chunks []string
	for chunk := range result.Chunks {
		if chunk.Content.Text() != "" {
			chunks = append(chunks, chunk.Content.Text())
		}
	}

	if len(chunks) < 1 {
		t.Errorf("chunk 数量 = %d, 期望 >= 1", len(chunks))
	}
}

func TestStream_WithCacheSharing(t *testing.T) {
	// 验证流式调用中 cache 参数注入
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody = decodeRequestBody(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"qwen-72b\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"null\"}]}\n\n")
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", server.URL+"/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.Stream(
		context.Background(),
		model_clients.NewTextMessagesParam("hello"),
		model_clients.WithStreamExtra(map[string]any{
			"session_id":           "session-456",
			"enable_cache_sharing": true,
		}),
	)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	// 消费所有 chunk
	for range result.Chunks {
	}

	if capturedBody["cache_sharing"] != true {
		t.Errorf("cache_sharing = %v, 期望 true", capturedBody["cache_sharing"])
	}
	if capturedBody["cache_salt"] != "session-456" {
		t.Errorf("cache_salt = %v, 期望 session-456", capturedBody["cache_salt"])
	}
}

// ──────────────────────────── Release 测试 ────────────────────────────

func TestRelease_Success(t *testing.T) {
	// 成功释放 KV Cache
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/release_kv_cache" {
			t.Errorf("请求路径 = %q, 期望 /v1/release_kv_cache", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("请求方法 = %q, 期望 POST", r.Method)
		}

		body := decodeRequestBody(t, r)
		if body["cache_sharing"] != true {
			t.Errorf("cache_sharing = %v, 期望 true", body["cache_sharing"])
		}
		if body["cache_salt"] != "session-abc" {
			t.Errorf("cache_salt = %v, 期望 session-abc", body["cache_salt"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", server.URL+"/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := client.Release(
		context.Background(),
		model_clients.WithReleaseSessionID("session-abc"),
		model_clients.WithReleaseMessagesIndex(5),
	)
	if err != nil {
		t.Fatalf("Release 返回错误: %v", err)
	}
	if !ok {
		t.Error("Release 应返回 true")
	}
}

func TestRelease_HTTPError(t *testing.T) {
	// 释放失败（非 200 响应）
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", server.URL+"/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := client.Release(
		context.Background(),
		model_clients.WithReleaseSessionID("session-abc"),
		model_clients.WithReleaseMessagesIndex(3),
	)
	if err != nil {
		t.Fatalf("Release 不应返回 error，应返回 false")
	}
	if ok {
		t.Error("HTTP 500 时 Release 应返回 false")
	}
}

func TestRelease_NonJSONResponse(t *testing.T) {
	// 非 JSON 响应仍返回 true（对齐 Python）
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`OK`))
	}))
	defer server.Close()

	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", server.URL+"/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := client.Release(
		context.Background(),
		model_clients.WithReleaseSessionID("session-abc"),
		model_clients.WithReleaseMessagesIndex(0),
	)
	if err != nil {
		t.Fatalf("Release 返回错误: %v", err)
	}
	if !ok {
		t.Error("200 响应即使非 JSON 也应返回 true")
	}
}

func TestRelease_WithTools(t *testing.T) {
	// 含 tools 参数的 release
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody = decodeRequestBody(t, r)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", server.URL+"/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	toolIdx := 2
	ok, err := client.Release(
		context.Background(),
		model_clients.WithReleaseSessionID("session-tools"),
		model_clients.WithReleaseMessagesIndex(3),
		model_clients.WithReleaseTools(
			commonschema.NewToolInfo("test_tool", "测试工具", map[string]any{}, nil),
		),
		model_clients.WithReleaseToolsIndex(toolIdx),
	)
	if err != nil {
		t.Fatalf("Release 返回错误: %v", err)
	}
	if !ok {
		t.Error("Release 应返回 true")
	}

	// 验证 tools 和 tools_released_index
	if _, exists := capturedBody["tools"]; !exists {
		t.Error("请求体应包含 tools 字段")
	}
	if capturedBody["tools_released_index"] != float64(2) {
		t.Errorf("tools_released_index = %v, 期望 2", capturedBody["tools_released_index"])
	}
}

// ──────────────────────────── GenerateImage/Speech/Video 不支持测试 ────────────────────────────

func TestGenerateImage_NotSupported(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.GenerateImage(context.Background(), nil)
	if err == nil {
		t.Error("GenerateImage 应返回不支持错误")
	}
	if !strings.Contains(err.Error(), "does not support image generation") {
		t.Errorf("错误信息 = %q, 应包含 'does not support image generation'", err.Error())
	}
}

func TestGenerateSpeech_NotSupported(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.GenerateSpeech(context.Background(), nil)
	if err == nil {
		t.Error("GenerateSpeech 应返回不支持错误")
	}
	if !strings.Contains(err.Error(), "does not support speech generation") {
		t.Errorf("错误信息 = %q, 应包含 'does not support speech generation'", err.Error())
	}
}

func TestGenerateVideo_NotSupported(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.GenerateVideo(context.Background(), nil)
	if err == nil {
		t.Error("GenerateVideo 应返回不支持错误")
	}
	if !strings.Contains(err.Error(), "does not support video generation") {
		t.Errorf("错误信息 = %q, 应包含 'does not support video generation'", err.Error())
	}
}

// ──────────────────────────── 接口合规性测试 ────────────────────────────

func TestInferenceAffinityModelClient_ImplementsBaseModelClient(t *testing.T) {
	// 验证 InferenceAffinityModelClient 实现了 BaseModelClient 接口
	var _ model_clients.BaseModelClient = (*InferenceAffinityModelClient)(nil)
}

// ──────────────────────────── 注册测试 ────────────────────────────

func TestRegistryContainsInferenceAffinity(t *testing.T) {
	// 验证 InferenceAffinity 已注册
	registry := model_clients.GetClientRegistry()
	clients := registry.ListClients()
	found := false
	for _, name := range clients {
		if strings.Contains(name, "InferenceAffinity") {
			found = true
			break
		}
	}
	if !found {
		t.Error("InferenceAffinity 未在注册表中找到")
	}
}

// ──────────────────────────── ReleaseParams 测试 ────────────────────────────

func TestReleaseParams_DefaultValues(t *testing.T) {
	// 测试 ReleaseParams 的默认值
	p := model_clients.NewReleaseParams()
	if p.SessionID != "" {
		t.Error("默认 SessionID 应为空")
	}
	if p.MessagesReleasedIndex != 0 {
		t.Error("默认 MessagesReleasedIndex 应为 0")
	}
}

func TestReleaseParams_AllOpts(t *testing.T) {
	// 测试 ReleaseParams 所有选项
	p := model_clients.NewReleaseParams(
		model_clients.WithReleaseSessionID("sess-1"),
		model_clients.WithReleaseMessagesIndex(5),
		model_clients.WithReleaseModel("qwen-72b"),
	)
	if p.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, 期望 sess-1", p.SessionID)
	}
	if p.MessagesReleasedIndex != 5 {
		t.Errorf("MessagesReleasedIndex = %d, 期望 5", p.MessagesReleasedIndex)
	}
	if p.Model != "qwen-72b" {
		t.Errorf("Model = %q, 期望 qwen-72b", p.Model)
	}
}

// ──────────────────────────── sanitizeToolCalls 测试 ────────────────────────────

func TestInferenceAffinity_SanitizeToolCalls_标准字段保留(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
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
}

func TestInferenceAffinity_SanitizeToolCalls_非标准字段移除(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
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
	funcPart, _ := tc["function"].(map[string]any)
	if _, exists := funcPart["extra_func"]; exists {
		t.Error("function 内非标准字段 extra_func 应被移除")
	}
	if funcPart["name"] != "search" {
		t.Errorf("function.name = %v, 期望 search", funcPart["name"])
	}
}

func TestInferenceAffinity_SanitizeToolCalls_强制Type为Function(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
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

func TestInferenceAffinity_SanitizeToolCalls_非Assistant消息不受影响(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
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

func TestInferenceAffinity_SanitizeToolCalls_无ToolCalls不受影响(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
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

func TestInferenceAffinity_SanitizeToolCalls_保留Index(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
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

func TestInferenceAffinity_SanitizeToolCalls_FunctionNotMap(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
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

func TestInferenceAffinity_SanitizeToolCalls_ToolCallNotMap(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
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

func TestInferenceAffinity_SanitizeToolCalls_MapStringAny格式(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
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

func TestInferenceAffinity_SanitizeToolCalls_空消息列表(t *testing.T) {
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 空消息列表不应 panic
	client.sanitizeToolCalls(nil)
	client.sanitizeToolCalls([]map[string]any{})
}

// ──────────────────────────── buildReleaseRequestBody 测试 ────────────────────────────

func TestBuildReleaseRequestBody_模型名从ModelConfig获取(t *testing.T) {
	// params.Model 为空时，应从 c.ModelConfig.ModelName 获取
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Model 为空，但 ModelConfig 有 ModelName
	params := model_clients.NewReleaseParams(
		model_clients.WithReleaseSessionID("session-1"),
		model_clients.WithReleaseMessagesIndex(0),
		// 不传 WithReleaseModel，model 为空
	)

	body, err := client.buildReleaseRequestBody(params)
	if err != nil {
		t.Fatalf("buildReleaseRequestBody 返回错误: %v", err)
	}
	// 应从 ModelConfig 取到 "qwen-72b"
	if body["model"] != "qwen-72b" {
		t.Errorf("model = %v, 期望 qwen-72b", body["model"])
	}
}

func TestBuildReleaseRequestBody_MessagesParam类型(t *testing.T) {
	// params.Messages 为 MessagesParam 类型时，应正常转换
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	msgs := model_clients.NewTextMessagesParam("hello")
	params := model_clients.NewReleaseParams(
		model_clients.WithReleaseSessionID("session-1"),
		model_clients.WithReleaseMessagesIndex(0),
		model_clients.WithReleaseMessages(msgs),
	)

	body, err := client.buildReleaseRequestBody(params)
	if err != nil {
		t.Fatalf("buildReleaseRequestBody 返回错误: %v", err)
	}
	messagesDict, ok := body["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("messages 类型错误, got %T", body["messages"])
	}
	if len(messagesDict) != 1 {
		t.Errorf("messages 长度 = %d, 期望 1", len(messagesDict))
	}
}

func TestBuildReleaseRequestBody_DictsMessages类型(t *testing.T) {
	// params.Messages 为 []map[string]any 类型时，应直接使用
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	dictMsgs := []map[string]any{
		{"role": "user", "content": "hello"},
	}
	params := model_clients.NewReleaseParams(
		model_clients.WithReleaseSessionID("session-1"),
		model_clients.WithReleaseMessagesIndex(0),
		model_clients.WithReleaseMessages(dictMsgs),
	)

	body, err := client.buildReleaseRequestBody(params)
	if err != nil {
		t.Fatalf("buildReleaseRequestBody 返回错误: %v", err)
	}
	messagesDict, ok := body["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("messages 类型错误, got %T", body["messages"])
	}
	if len(messagesDict) != 1 {
		t.Errorf("messages 长度 = %d, 期望 1", len(messagesDict))
	}
}

func TestBuildReleaseRequestBody_UnsupportedMessagesType(t *testing.T) {
	// params.Messages 为不支持的类型时，应返回错误
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	params := model_clients.NewReleaseParams(
		model_clients.WithReleaseSessionID("session-1"),
		model_clients.WithReleaseMessagesIndex(0),
		model_clients.WithReleaseMessages("invalid_type"), // string 不是支持的类型
	)

	_, err = client.buildReleaseRequestBody(params)
	if err == nil {
		t.Error("不支持的 messages 类型应返回错误")
	}
}

func TestBuildReleaseRequestBody_无Messages(t *testing.T) {
	// params.Messages 为 nil 时，不应报错
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	params := model_clients.NewReleaseParams(
		model_clients.WithReleaseSessionID("session-1"),
		model_clients.WithReleaseMessagesIndex(0),
	)

	body, err := client.buildReleaseRequestBody(params)
	if err != nil {
		t.Fatalf("buildReleaseRequestBody 返回错误: %v", err)
	}
	// messages 应为空（nil 或空 slice 都算无消息）
	messagesVal := body["messages"]
	if messagesVal != nil {
		if msgs, ok := messagesVal.([]map[string]any); !ok || len(msgs) != 0 {
			t.Errorf("messages 应为空, got %v", messagesVal)
		}
	}
}

// ──────────────────────────── buildReleaseHTTPClient 测试 ────────────────────────────

func TestBuildReleaseHTTPClient_VerifySSLWithInvalidCert(t *testing.T) {
	// VerifySSL=true 且 SSLCert 指向不存在的文件，应返回错误
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		llmschema.NewModelClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1",
			llmschema.WithVerifySSL(true),
			llmschema.WithSSLCert("/nonexistent/cert.pem"),
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.buildReleaseHTTPClient(nil)
	if err == nil {
		t.Error("不存在的 SSL 证书文件应返回错误")
	}
	if !strings.Contains(err.Error(), "读取 SSL 证书失败") {
		t.Errorf("错误信息 = %q, 应包含 '读取 SSL 证书失败'", err.Error())
	}
}

func TestBuildReleaseHTTPClient_VerifySSLWithInvalidCertContent(t *testing.T) {
	// VerifySSL=true 且 SSLCert 指向无效证书内容，应返回解析错误
	// 创建临时文件写入无效 PEM 内容
	tmpFile, err := os.CreateTemp("", "cert-*.pem")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_, _ = tmpFile.WriteString("not a valid pem certificate")
	_ = tmpFile.Close()

	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		llmschema.NewModelClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1",
			llmschema.WithVerifySSL(true),
			llmschema.WithSSLCert(tmpFile.Name()),
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.buildReleaseHTTPClient(nil)
	if err == nil {
		t.Error("无效的 SSL 证书内容应返回错误")
	}
	if !strings.Contains(err.Error(), "解析 SSL 证书失败") {
		t.Errorf("错误信息 = %q, 应包含 '解析 SSL 证书失败'", err.Error())
	}
}

func TestBuildReleaseHTTPClient_ConfigTimeout(t *testing.T) {
	// ClientConfig.Timeout > 0 时应使用配置的超时时间
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		llmschema.NewModelClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1",
			llmschema.WithVerifySSL(false),
			llmschema.WithTimeout(30.0),
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	httpClient, err := client.buildReleaseHTTPClient(nil)
	if err != nil {
		t.Fatalf("buildReleaseHTTPClient 返回错误: %v", err)
	}
	// 超时时间应为 30 秒
	expectedTimeout := time.Duration(30.0 * float64(time.Second))
	if httpClient.Timeout != expectedTimeout {
		t.Errorf("Timeout = %v, 期望 %v", httpClient.Timeout, expectedTimeout)
	}
}

func TestBuildReleaseHTTPClient_CustomTimeout(t *testing.T) {
	// 传入自定义 timeout 时应覆盖配置的超时时间
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		llmschema.NewModelClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1",
			llmschema.WithVerifySSL(false),
			llmschema.WithTimeout(30.0),
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	customTimeout := 120.0
	httpClient, err := client.buildReleaseHTTPClient(&customTimeout)
	if err != nil {
		t.Fatalf("buildReleaseHTTPClient 返回错误: %v", err)
	}
	expectedTimeout := time.Duration(120.0 * float64(time.Second))
	if httpClient.Timeout != expectedTimeout {
		t.Errorf("Timeout = %v, 期望 %v", httpClient.Timeout, expectedTimeout)
	}
}

func TestBuildReleaseHTTPClient_VerifySSLTrueNoCert(t *testing.T) {
	// VerifySSL=true 且无 SSLCert 时，构造函数会要求 ssl_cert
	// 无法创建 VerifySSL=true 且无 SSLCert 的客户端（构造函数会报错）
	// 所以直接测试 buildReleaseHTTPClient 内部 VerifySSL=true + SSLCert="" 的路径
	// 需要手动构造 ClientConfig 绕过构造函数校验
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}
	// 手动设置 VerifySSL=true 但 SSLCert 为空，模拟"验证但无证书文件"路径
	// 这种情况下 buildReleaseHTTPClient 走到 else if SSLCert != "" 的 else 分支，
	// 即使用默认 TLS 配置（transport 无 TLSClientConfig）
	client.ClientConfig.VerifySSL = true
	client.ClientConfig.SSLCert = ""

	httpClient, err := client.buildReleaseHTTPClient(nil)
	if err != nil {
		t.Fatalf("buildReleaseHTTPClient 返回错误: %v", err)
	}
	if httpClient == nil {
		t.Error("httpClient 不应为 nil")
	}
}

// ──────────────────────────── sanitizeMessages 错误路径测试 ────────────────────────────

func TestSanitizeMessages_EmptyMessagesError(t *testing.T) {
	// 空消息应返回错误
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.sanitizeMessages(model_clients.MessagesParam{})
	if err == nil {
		t.Error("空消息应返回错误")
	}
}

// ──────────────────────────── Release 错误路径测试 ────────────────────────────

func TestRelease_BuildBodyError(t *testing.T) {
	// buildReleaseRequestBody 返回错误时，Release 应返回错误
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	// 传入不支持的 messages 类型，使 buildReleaseRequestBody 报错
	_, err = client.Release(
		context.Background(),
		model_clients.WithReleaseSessionID("session-1"),
		model_clients.WithReleaseMessagesIndex(0),
		model_clients.WithReleaseMessages("invalid_type"),
	)
	if err == nil {
		t.Error("buildReleaseRequestBody 错误时 Release 应返回错误")
	}
}

func TestRelease_SSLCertError(t *testing.T) {
	// buildReleaseHTTPClient 返回错误时，Release 应返回错误
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		llmschema.NewModelClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1",
			llmschema.WithVerifySSL(true),
			llmschema.WithSSLCert("/nonexistent/cert.pem"),
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Release(
		context.Background(),
		model_clients.WithReleaseSessionID("session-1"),
		model_clients.WithReleaseMessagesIndex(0),
	)
	if err == nil {
		t.Error("SSL 证书错误时 Release 应返回错误")
	}
	if !strings.Contains(err.Error(), "构建 HTTP 客户端失败") {
		t.Errorf("错误信息 = %q, 应包含 '构建 HTTP 客户端失败'", err.Error())
	}
}

// ──────────────────────────── init 注册工厂错误路径测试 ────────────────────────────

func TestInit_FactoryWithInvalidConfig(t *testing.T) {
	// 验证 init 注册的工厂在配置无效时返回 nil
	registry := model_clients.GetClientRegistry()

	// 使用缺少 API Key 的配置，工厂应返回 nil
	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("qwen-72b"))
	cc := llmschema.NewModelClientConfig("InferenceAffinity", "", "https://vllm.example.com/v1",
		llmschema.WithVerifySSL(false),
	)
	client, err := registry.GetClient("InferenceAffinity", "llm", mc, cc)
	// GetClient 调用工厂，工厂内部 NewInferenceAffinityModelClient 失败时返回 nil
	// 但 GetClient 自身可能返回不同错误形式
	_ = err // 工厂返回 nil 后 GetClient 的具体行为取决于注册表实现
	if client != nil {
		t.Error("无效配置时工厂应返回 nil 客户端")
	}
}

// ──────────────────────────── Invoke/Stream 错误路径测试 ────────────────────────────

func TestInvoke_SanitizeMessagesError(t *testing.T) {
	// sanitizeMessages 返回错误时，Invoke 应返回错误
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	// 空消息会导致 sanitizeMessages 失败
	_, err = client.Invoke(context.Background(), model_clients.MessagesParam{})
	if err == nil {
		t.Error("空消息时 Invoke 应返回错误")
	}
}

func TestStream_SanitizeMessagesError(t *testing.T) {
	// sanitizeMessages 返回错误时，Stream 应返回错误
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	// 空消息会导致 sanitizeMessages 失败
	_, err = client.Stream(context.Background(), model_clients.MessagesParam{})
	if err == nil {
		t.Error("空消息时 Stream 应返回错误")
	}
}

// ──────────────────────────── parseStreamChunk 测试 ────────────────────────────

func TestInferenceAffinity_ParseStreamChunk_基本内容(t *testing.T) {
	// 验证基本 content 解析
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	content := "你好"
	chunkResp := openai.ChatCompletionChunkResponse{
		Choices: []openai.ChunkChoice{
			{
				Delta: &openai.ChunkDelta{
					Content: &content,
				},
				FinishReason: nil,
			},
		},
	}

	chunk := client.parseStreamChunk(&chunkResp)
	if chunk == nil {
		t.Fatal("chunk 不应为 nil")
	}
	if chunk.Content.Text() != "你好" {
		t.Errorf("Content = %q, 期望 %q", chunk.Content.Text(), "你好")
	}
}

func TestInferenceAffinity_ParseStreamChunk_无Choices返回Nil(t *testing.T) {
	// 无 choices 时返回 nil（丢弃 usage-only chunk）
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	// 无 choices 但有 usage（OpenAI stream_options 场景）
	chunkResp := openai.ChatCompletionChunkResponse{
		Choices: []openai.ChunkChoice{},
		Usage: &openai.ResponseUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	chunk := client.parseStreamChunk(&chunkResp)
	if chunk != nil {
		t.Error("无 choices 时应返回 nil")
	}
}

func TestInferenceAffinity_ParseStreamChunk_空Delta返回Nil(t *testing.T) {
	// 空 content + 空 reasoning + 空 tool_calls + 无 finish_reason → nil
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	finishNull := "null"
	chunkResp := openai.ChatCompletionChunkResponse{
		Choices: []openai.ChunkChoice{
			{
				Delta:        &openai.ChunkDelta{},
				FinishReason: &finishNull,
			},
		},
	}

	chunk := client.parseStreamChunk(&chunkResp)
	if chunk != nil {
		t.Error("空 delta + finish_reason='null' 应返回 nil")
	}
}

func TestInferenceAffinity_ParseStreamChunk_有FinishReason保留(t *testing.T) {
	// 空 content 但有 finish_reason="stop" → 保留
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	finishStop := "stop"
	chunkResp := openai.ChatCompletionChunkResponse{
		Choices: []openai.ChunkChoice{
			{
				Delta:        &openai.ChunkDelta{},
				FinishReason: &finishStop,
			},
		},
	}

	chunk := client.parseStreamChunk(&chunkResp)
	if chunk == nil {
		t.Fatal("有 finish_reason='stop' 时 chunk 不应为 nil")
	}
	if chunk.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, 期望 %q", chunk.FinishReason, "stop")
	}
}

func TestInferenceAffinity_ParseStreamChunk_ReasoningContent(t *testing.T) {
	// 验证 reasoning_content 解析
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	reasoning := "让我思考一下"
	chunkResp := openai.ChatCompletionChunkResponse{
		Choices: []openai.ChunkChoice{
			{
				Delta: &openai.ChunkDelta{
					ReasoningContent: &reasoning,
				},
				FinishReason: nil,
			},
		},
	}

	chunk := client.parseStreamChunk(&chunkResp)
	if chunk == nil {
		t.Fatal("有 reasoning_content 时 chunk 不应为 nil")
	}
	if chunk.ReasoningContent != "让我思考一下" {
		t.Errorf("ReasoningContent = %q, 期望 %q", chunk.ReasoningContent, "让我思考一下")
	}
}

func TestInferenceAffinity_ParseStreamChunk_ToolCalls(t *testing.T) {
	// 验证 tool_calls 解析
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	idx := 0
	chunkResp := openai.ChatCompletionChunkResponse{
		Choices: []openai.ChunkChoice{
			{
				Delta: &openai.ChunkDelta{
					ToolCalls: []openai.ChunkToolCall{
						{
							ID:   "call_123",
							Type: "function",
							Function: openai.ChunkFunction{
								Name:      "get_weather",
								Arguments: `{"city":"Beijing"}`,
							},
							Index: &idx,
						},
					},
				},
				FinishReason: nil,
			},
		},
	}

	chunk := client.parseStreamChunk(&chunkResp)
	if chunk == nil {
		t.Fatal("有 tool_calls 时 chunk 不应为 nil")
	}
	if len(chunk.ToolCalls) != 1 {
		t.Fatalf("ToolCalls 数量 = %d, 期望 1", len(chunk.ToolCalls))
	}
	if chunk.ToolCalls[0].Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q, 期望 %q", chunk.ToolCalls[0].Name, "get_weather")
	}
}

func TestInferenceAffinity_ParseStreamChunk_Usage无费用(t *testing.T) {
	// 验证 usage 不包含费用信息（与 SiliconFlow 的区别）
	client, err := NewInferenceAffinityModelClient(
		newTestModelConfig(),
		newTestClientConfig("InferenceAffinity", "test-key", "https://vllm.example.com/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	content := "完成"
	finishStop := "stop"
	chunkResp := openai.ChatCompletionChunkResponse{
		Choices: []openai.ChunkChoice{
			{
				Delta: &openai.ChunkDelta{
					Content: &content,
				},
				FinishReason: &finishStop,
			},
		},
		Usage: &openai.ResponseUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
	}

	chunk := client.parseStreamChunk(&chunkResp)
	if chunk == nil {
		t.Fatal("chunk 不应为 nil")
	}
	if chunk.UsageMetadata == nil {
		t.Fatal("UsageMetadata 不应为 nil")
	}
	if chunk.UsageMetadata.InputTokens != 100 {
		t.Errorf("InputTokens = %d, 期望 100", chunk.UsageMetadata.InputTokens)
	}
	if chunk.UsageMetadata.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, 期望 50", chunk.UsageMetadata.OutputTokens)
	}
	// 对齐 Python InferenceAffinity: 不提取费用信息
	if chunk.UsageMetadata.InputCost != 0 || chunk.UsageMetadata.OutputCost != 0 || chunk.UsageMetadata.TotalCost != 0 {
		t.Error("InferenceAffinity usage 不应包含费用信息")
	}
}

// ──────────────────────────── buildInferenceAffinityUsageMetadata 测试 ────────────────────────────

func TestBuildInferenceAffinityUsageMetadata_基本字段(t *testing.T) {
	usage := &openai.ResponseUsage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
	}

	meta := buildInferenceAffinityUsageMetadata(usage, "qwen-72b")
	if meta.ModelName != "qwen-72b" {
		t.Errorf("ModelName = %q, 期望 %q", meta.ModelName, "qwen-72b")
	}
	if meta.InputTokens != 10 {
		t.Errorf("InputTokens = %d, 期望 10", meta.InputTokens)
	}
	if meta.OutputTokens != 20 {
		t.Errorf("OutputTokens = %d, 期望 20", meta.OutputTokens)
	}
	if meta.TotalTokens != 30 {
		t.Errorf("TotalTokens = %d, 期望 30", meta.TotalTokens)
	}
}

func TestBuildInferenceAffinityUsageMetadata_无费用(t *testing.T) {
	usage := &openai.ResponseUsage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	}

	meta := buildInferenceAffinityUsageMetadata(usage, "qwen-72b")
	if meta.InputCost != 0 {
		t.Errorf("InputCost = %f, 期望 0", meta.InputCost)
	}
	if meta.OutputCost != 0 {
		t.Errorf("OutputCost = %f, 期望 0", meta.OutputCost)
	}
	if meta.TotalCost != 0 {
		t.Errorf("TotalCost = %f, 期望 0", meta.TotalCost)
	}
}
