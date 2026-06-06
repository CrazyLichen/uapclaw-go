package inferenceaffinity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
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

// mockCompletionResponseWithToolCalls 构造含 tool_calls 的响应
func mockCompletionResponseWithToolCalls() string {
	return `{"id":"chatcmpl-123","object":"chat.completion","created":1234567890,"model":"qwen-72b","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_abc","type":"function","index":0,"function":{"name":"get_weather","arguments":"{\"city\":\"Beijing\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
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
		w.Write([]byte(mockCompletionResponse("你好！")))
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
		w.Write([]byte(mockCompletionResponse("ok")))
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
			"role": "assistant",
			"content": nil,
			"tool_calls": []map[string]any{
				{
					"id":            "call_123",
					"type":          "invalid_type", // 非标准 type，应被强制为 "function"
					"extra_field":   "should_be_removed", // 非标准字段，应被移除
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
		w.Write([]byte(mockCompletionResponse("ok")))
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
		w.Write([]byte(`{"error": "internal server error"}`))
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
		fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"qwen-72b\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你\"},\"finish_reason\":\"null\"}]}\n\n")
		fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"qwen-72b\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"好\"},\"finish_reason\":\"null\"}]}\n\n")
		fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"qwen-72b\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
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
		fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"qwen-72b\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"null\"}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
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
		w.Write([]byte(`{"status": "ok"}`))
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
		w.Write([]byte(`{"error": "internal error"}`))
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
		w.Write([]byte(`OK`))
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
		w.Write([]byte(`{"status": "ok"}`))
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
			commonschema.NewToolInfo("test_tool", "测试工具", map[string]any{}),
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

// ──────────────────────────── sanitizeToolCalls 测试 ────────────────────────────

func TestSanitizeToolCalls_StandardFields(t *testing.T) {
	// 保留标准字段
	messages := []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{
					"id":   "call_123",
					"type": "function",
					"function": map[string]any{
						"name":      "get_weather",
						"arguments": `{"city":"Beijing"}`,
					},
				},
			},
		},
	}

	sanitizeToolCalls(messages)

	tc, _ := messages[0]["tool_calls"].([]map[string]any)[0]["function"].(map[string]any)
	if tc["name"] != "get_weather" {
		t.Errorf("name = %v, 期望 get_weather", tc["name"])
	}
}

func TestSanitizeToolCalls_ForceType(t *testing.T) {
	// 强制 type="function"
	messages := []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{
					"id":   "call_123",
					"type": "invalid_type",
					"function": map[string]any{
						"name":      "test",
						"arguments": "{}",
					},
				},
			},
		},
	}

	sanitizeToolCalls(messages)

	tc := messages[0]["tool_calls"].([]map[string]any)[0]
	if tc["type"] != "function" {
		t.Errorf("type = %v, 期望 function", tc["type"])
	}
}

func TestSanitizeToolCalls_RemoveExtraFields(t *testing.T) {
	// 移除非标准字段
	messages := []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{
					"id":          "call_123",
					"type":        "function",
					"extra_field": "should_be_removed",
					"function": map[string]any{
						"name":      "test",
						"arguments": "{}",
						"extra":     "also_removed",
					},
				},
			},
		},
	}

	sanitizeToolCalls(messages)

	tc := messages[0]["tool_calls"].([]map[string]any)[0]
	if _, exists := tc["extra_field"]; exists {
		t.Error("extra_field 应被移除")
	}
	funcPart, _ := tc["function"].(map[string]any)
	if _, exists := funcPart["extra"]; exists {
		t.Error("function.extra 应被移除")
	}
}

func TestSanitizeToolCalls_NonAssistantMessages(t *testing.T) {
	// 跳过非 assistant 消息
	messages := []map[string]any{
		{"role": "user", "content": "hello"},
		{"role": "system", "content": "you are helpful"},
	}

	sanitizeToolCalls(messages)

	// 非 assistant 消息不应被修改
	if messages[0]["role"] != "user" {
		t.Error("user 消息不应被修改")
	}
	if messages[1]["role"] != "system" {
		t.Error("system 消息不应被修改")
	}
}

func TestSanitizeToolCalls_IndexPreserved(t *testing.T) {
	// index 字段应保留
	messages := []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []map[string]any{
				{
					"id":    "call_123",
					"type":  "function",
					"index": 0,
					"function": map[string]any{
						"name":      "test",
						"arguments": "{}",
					},
				},
			},
		},
	}

	sanitizeToolCalls(messages)

	tc := messages[0]["tool_calls"].([]map[string]any)[0]
	if tc["index"] != 0 {
		t.Errorf("index = %v, 期望 0", tc["index"])
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
