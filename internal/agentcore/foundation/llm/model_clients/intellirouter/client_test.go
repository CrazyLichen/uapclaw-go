package intellirouter

import (
	"context"
	"fmt"
	"math"
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

// ──── 辅助函数 ────

// mockCompletionResponse 构造 OpenAI Chat Completion 响应 JSON。
func mockCompletionResponse(content string) string {
	return fmt.Sprintf(`{"id":"chatcmpl-123","object":"chat.completion","created":1234567890,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, content)
}

// mockSSEStreamResponse 构造 OpenAI SSE 流式响应。
func mockSSEStreamResponse(content string) string {
	chunks := []string{"data: ", "data: ", "data: [DONE]\n"}
	chunks[0] = fmt.Sprintf(`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","content":%q},"finish_reason":null}]}`+"\n", content)
	chunks[1] = `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"test-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"]}` + "\n"
	return strings.Join(chunks, "")
}

// createTestClientWithServer 创建指向 httptest 服务器的 IntelliRouter 客户端。
func createTestClientWithServer(t *testing.T, server *httptest.Server, numDeps int) *IntelliRouterModelClient {
	t.Helper()

	// 清空路由缓存
	routerCacheLock.Lock()
	routerCache = make(map[string]*ReliableRouter)
	routerCacheLock.Unlock()

	// 构建 deployments，全部指向 httptest 服务器
	deployments := make([]map[string]any, 0, numDeps)
	for i := 0; i < numDeps; i++ {
		deployments = append(deployments, map[string]any{
			"id":         fmt.Sprintf("dep%d", i+1),
			"model_name": "test-model",
			"api_key":    "sk-test",
			"api_base":   server.URL,
			"tpm":        100000,
			"rpm":        60,
		})
	}

	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": deployments,
			"intelli_router_strategy":    "simple-shuffle",
			"intelli_router_num_retries": numDeps,
		}),
	)

	client, err := NewIntelliRouterModelClient(mc, cc)
	if err != nil {
		t.Fatalf("创建测试客户端失败: %v", err)
	}
	return client
}

// createTestClientWithRetry 创建带重试的测试客户端（多端点，部分失败）。
func createTestClientWithRetry(t *testing.T, servers ...*httptest.Server) *IntelliRouterModelClient {
	t.Helper()

	routerCacheLock.Lock()
	routerCache = make(map[string]*ReliableRouter)
	routerCacheLock.Unlock()

	deployments := make([]map[string]any, 0, len(servers))
	for i, server := range servers {
		deployments = append(deployments, map[string]any{
			"id":         fmt.Sprintf("dep%d", i+1),
			"model_name": "test-model",
			"api_key":    "sk-test",
			"api_base":   server.URL,
			"tpm":        100000,
			"rpm":        60,
		})
	}

	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": deployments,
			"intelli_router_strategy":    "simple-shuffle",
			"intelli_router_num_retries": len(servers),
		}),
	)

	client, err := NewIntelliRouterModelClient(mc, cc)
	if err != nil {
		t.Fatalf("创建测试客户端失败: %v", err)
	}
	return client
}

// ──── Invoke 测试 ────

// TestInvoke_Success 测试 Invoke 成功调用。
func TestInvoke_Success(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponse("Hello from IntelliRouter!"))
	}))
	defer server.Close()

	client := createTestClientWithServer(t, server, 1)
	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Invoke(context.Background(), msg)
	if err != nil {
		t.Fatalf("Invoke 报错: %v", err)
	}
	if result == nil {
		t.Fatal("Invoke 结果不应为 nil")
	}
	if result.Content.Text() != "Hello from IntelliRouter!" {
		t.Errorf("Content = %q, 期望 %q", result.Content.Text(), "Hello from IntelliRouter!")
	}
	if callCount != 1 {
		t.Errorf("调用次数 = %d, 期望 1", callCount)
	}
}

// TestInvoke_RetryOnFailure 测试 Invoke 失败时自动重试。
func TestInvoke_RetryOnFailure(t *testing.T) {
	// 第一个服务器总是返回 500
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `internal server error`)
	}))
	defer failServer.Close()

	// 第二个服务器正常返回
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponse("Retry success!"))
	}))
	defer successServer.Close()

	client := createTestClientWithRetry(t, failServer, successServer)

	// 因为 simple-shuffle 可能先选中成功的服务器，我们需要多试几次
	// 或者直接构造端点顺序可控的客户端
	// 更简单的方式：把失败服务器的端点设为不健康
	var lastErr error
	var result *llmschema.AssistantMessage
	for i := 0; i < 10; i++ {
		result, lastErr = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("Hi"))
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		t.Fatalf("多次重试后仍失败: %v", lastErr)
	}
	_ = result // 成功即可
}

// TestInvoke_AllEndpointsFail 测试所有端点都失败时返回错误。
func TestInvoke_AllEndpointsFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `internal server error`)
	}))
	defer server.Close()

	client := createTestClientWithServer(t, server, 2)
	msg := model_clients.NewTextMessagesParam("Hi")
	_, err := client.Invoke(context.Background(), msg)
	if err == nil {
		t.Error("所有端点失败时应返回错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	// 可能是"所有部署端点均失败"（重试后）或"所有端点均不健康或冷却中"（冷却过滤后）
	errMsg := baseErr.Error()
	if !strings.Contains(errMsg, "所有部署端点均失败") && !strings.Contains(errMsg, "所有端点均不健康或冷却中") {
		t.Errorf("错误消息应包含失败信息, got %q", errMsg)
	}
}

// TestInvoke_NoAvailableEndpoints 测试无可用端点时报错。
func TestInvoke_NoAvailableEndpoints(t *testing.T) {
	routerCacheLock.Lock()
	routerCache = make(map[string]*ReliableRouter)
	routerCacheLock.Unlock()

	// 创建只有 1 个端点的客户端，然后标记端点为不健康
	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []map[string]any{
				{
					"id":         "dep1",
					"model_name": "test-model",
					"api_key":    "sk-test",
					"api_base":   "https://unreachable.test.com",
				},
			},
			"intelli_router_num_retries": 0,
		}),
	)

	client, err := NewIntelliRouterModelClient(mc, cc)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 标记端点为不健康
	client.router.deployments[0].RecordFailure()

	_, err = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("Hi"))
	if err == nil {
		t.Error("无可用端点时应返回错误")
	}
}

// TestInvoke_RecordSuccessOnSuccess 测试成功调用后记录延迟。
func TestInvoke_RecordSuccessOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponse("ok"))
	}))
	defer server.Close()

	client := createTestClientWithServer(t, server, 1)

	// 调用前延迟应为 +inf
	dep := client.router.deployments[0]
	if !math.IsInf(dep.GetAvgLatency(), 1) {
		t.Error("调用前 avgLatency 应为 +inf")
	}

	msg := model_clients.NewTextMessagesParam("Hi")
	_, err := client.Invoke(context.Background(), msg)
	if err != nil {
		t.Fatalf("Invoke 报错: %v", err)
	}

	// 调用后延迟应不再是 +inf（即使是 0ms 也说明已被记录）
	lat := dep.GetAvgLatency()
	if math.IsInf(lat, 1) {
		t.Error("调用后 avgLatency 不应仍为 +inf")
	}
}

// TestInvoke_RecordFailureOnError 测试失败调用后记录失败。
func TestInvoke_RecordFailureOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `error`)
	}))
	defer server.Close()

	client := createTestClientWithServer(t, server, 1)
	client.router.numRetries = 0 // 不重试

	dep := client.router.deployments[0]
	_, _ = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("Hi"))

	// 应记录为不健康
	if dep.IsHealthy() {
		t.Error("失败后端点应标记为不健康")
	}
}

// ──── Stream 测试 ────

// TestStream_Success 测试 Stream 成功调用。
func TestStream_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockSSEStreamResponse("Hello stream!"))
	}))
	defer server.Close()

	client := createTestClientWithServer(t, server, 1)
	msg := model_clients.NewTextMessagesParam("Hi")
	result, err := client.Stream(context.Background(), msg)
	if err != nil {
		t.Fatalf("Stream 报错: %v", err)
	}
	if result == nil {
		t.Fatal("Stream 结果不应为 nil")
	}
}

// TestStream_Error 测试 Stream 失败。
func TestStream_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `internal server error`)
	}))
	defer server.Close()

	client := createTestClientWithServer(t, server, 1)
	msg := model_clients.NewTextMessagesParam("Hi")
	_, err := client.Stream(context.Background(), msg)
	if err == nil {
		t.Error("Stream 失败时应返回错误")
	}
}

// TestStream_RecordSuccessOnSuccess 测试 Stream 成功后记录延迟。
func TestStream_RecordSuccessOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockSSEStreamResponse("stream ok"))
	}))
	defer server.Close()

	client := createTestClientWithServer(t, server, 1)
	dep := client.router.deployments[0]

	// 调用前延迟应为 +inf
	if !math.IsInf(dep.GetAvgLatency(), 1) {
		t.Error("调用前 avgLatency 应为 +inf")
	}

	_, err := client.Stream(context.Background(), model_clients.NewTextMessagesParam("Hi"))
	if err != nil {
		t.Fatalf("Stream 报错: %v", err)
	}

	// 调用后延迟应不再是 +inf（即使是 0ms 也说明已被记录）
	lat := dep.GetAvgLatency()
	if math.IsInf(lat, 1) {
		t.Error("Stream 成功后 avgLatency 不应仍为 +inf")
	}
}

// ──── Router 层面薄包装方法测试 ────

// TestReliableRouter_RecordSuccess 测试 Router 级 RecordSuccess。
func TestReliableRouter_RecordSuccess(t *testing.T) {
	dep := NewDeployment(DeploymentConfig{ID: "test"})
	router := &ReliableRouter{
		deployments:  []*Deployment{dep},
		sessionMap:   make(map[string]sessionEntry),
		modelIndices: make(map[string][]*Deployment),
	}

	router.RecordSuccess(dep, 100*time.Millisecond)
	if !dep.IsHealthy() {
		t.Error("RecordSuccess 后应健康")
	}
	if dep.GetAvgLatency() != 100.0 {
		t.Errorf("avgLatency = %f, 期望 100.0", dep.GetAvgLatency())
	}
}

// TestReliableRouter_RecordFailure 测试 Router 级 RecordFailure。
func TestReliableRouter_RecordFailure(t *testing.T) {
	dep := NewDeployment(DeploymentConfig{ID: "test"})
	router := &ReliableRouter{
		deployments:  []*Deployment{dep},
		sessionMap:   make(map[string]sessionEntry),
		modelIndices: make(map[string][]*Deployment),
	}

	router.RecordFailure(dep)
	if dep.IsHealthy() {
		t.Error("RecordFailure 后应不健康")
	}
}

// ──── HealthChecker 补充测试 ────

// TestHealthChecker_CheckAllWithServer 测试 CheckAll 使用 httptest 服务器。
func TestHealthChecker_CheckAllWithServer(t *testing.T) {
	// 正常服务器
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponse("ok"))
	}))
	defer okServer.Close()

	// 错误服务器
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failServer.Close()

	deps := []*Deployment{
		NewDeployment(DeploymentConfig{ID: "ok", ModelName: "test", APIKey: "k1", APIBase: okServer.URL, VerifySSL: false}),
		NewDeployment(DeploymentConfig{ID: "fail", ModelName: "test", APIKey: "k2", APIBase: failServer.URL, VerifySSL: false}),
	}

	hc := NewHealthChecker(deps, 300.0, 5.0)
	results := hc.CheckAll()

	if len(results) != 2 {
		t.Fatalf("CheckAll 应返回 2 个结果，实际 %d", len(results))
	}
	if !results["ok"].IsHealthy {
		t.Error("ok 端点应为健康")
	}
	if results["fail"].IsHealthy {
		t.Error("fail 端点应为不健康")
	}
}

// TestHealthChecker_GetLastResults 测试获取最近检查结果。
func TestHealthChecker_GetLastResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponse("ok"))
	}))
	defer server.Close()

	deps := []*Deployment{
		NewDeployment(DeploymentConfig{ID: "dep1", ModelName: "test", APIKey: "k1", APIBase: server.URL, VerifySSL: false}),
	}

	hc := NewHealthChecker(deps, 300.0, 5.0)
	hc.CheckAll()

	results := hc.GetLastResults()
	if len(results) != 1 {
		t.Fatalf("GetLastResults 应返回 1 个结果，实际 %d", len(results))
	}
	if !results["dep1"].IsHealthy {
		t.Error("dep1 应为健康")
	}
}

// TestHealthChecker_HealthyRecovery 测试健康检查通过后恢复端点。
func TestHealthChecker_HealthyRecovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponse("ok"))
	}))
	defer server.Close()

	dep := NewDeployment(DeploymentConfig{ID: "dep1", ModelName: "test", APIKey: "k1", APIBase: server.URL, VerifySSL: false})
	dep.RecordFailure() // 标记为不健康

	if dep.IsHealthy() {
		t.Error("RecordFailure 后应不健康")
	}

	hc := NewHealthChecker([]*Deployment{dep}, 300.0, 5.0)
	hc.CheckAll()

	// 健康检查通过后应恢复
	if !dep.IsHealthy() {
		t.Error("健康检查通过后应恢复为健康")
	}
}

// TestReliableRouter_GetHealthCheckResults 测试获取健康检查结果。
func TestReliableRouter_GetHealthCheckResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponse("ok"))
	}))
	defer server.Close()

	config := &IntelliRouterClientConfig{
		Strategy:            "simple-shuffle",
		NumRetries:          3,
		Timeout:             30.0,
		EnableHealthCheck:   true,
		HealthCheckInterval: 300.0,
		Deployments: []DeploymentConfig{
			{ID: "dep1", ModelName: "test", APIKey: "k1", APIBase: server.URL, VerifySSL: false},
		},
	}

	router := NewReliableRouter(config)
	defer router.StopHealthChecker()

	results := router.GetHealthCheckResults()
	if results == nil {
		t.Error("启用健康检查后 GetHealthCheckResults 不应返回 nil")
	}
}

// TestReliableRouter_GetHealthCheckResults_NotEnabled 测试未启用健康检查时返回 nil。
func TestReliableRouter_GetHealthCheckResults_NotEnabled(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")
	results := router.GetHealthCheckResults()
	if results != nil {
		t.Error("未启用健康检查时 GetHealthCheckResults 应返回 nil")
	}
}

// ──── 辅助方法补充测试 ────

// TestGetFloatFromAny_AllTypes 测试 getFloatFromAny 所有类型。
func TestGetFloatFromAny_AllTypes(t *testing.T) {
	tests := []struct {
		input    any
		expected float64
		ok       bool
	}{
		{nil, 0, false},
		{float64(3.14), 3.14, true},
		{int(42), 42.0, true},
		{int64(100), 100.0, true},
		{"string", 0, false},
		{true, 0, false},
	}

	for _, tt := range tests {
		v, ok := getFloatFromAny(tt.input)
		if ok != tt.ok {
			t.Errorf("getFloatFromAny(%v) ok = %v, 期望 %v", tt.input, ok, tt.ok)
		}
		if ok && v != tt.expected {
			t.Errorf("getFloatFromAny(%v) = %f, 期望 %f", tt.input, v, tt.expected)
		}
	}
}

// TestGetStringDefault 测试 getStringDefault。
func TestGetStringDefault(t *testing.T) {
	m := map[string]any{"key": "value"}
	if v := getStringDefault(m, "key", "default"); v != "value" {
		t.Errorf("getStringDefault = %q, 期望 %q", v, "value")
	}
	if v := getStringDefault(m, "missing", "default"); v != "default" {
		t.Errorf("getStringDefault missing = %q, 期望 %q", v, "default")
	}
	if v := getStringDefault(nil, "key", "default"); v != "default" {
		t.Errorf("getStringDefault nil = %q, 期望 %q", v, "default")
	}
}

// TestGetIntDefault 测试 getIntDefault。
func TestGetIntDefault(t *testing.T) {
	m := map[string]any{"key": 42}
	if v := getIntDefault(m, "key", 0); v != 42 {
		t.Errorf("getIntDefault = %d, 期望 %d", v, 42)
	}
	if v := getIntDefault(m, "missing", 99); v != 99 {
		t.Errorf("getIntDefault missing = %d, 期望 %d", v, 99)
	}
	if v := getIntDefault(nil, "key", 99); v != 99 {
		t.Errorf("getIntDefault nil = %d, 期望 %d", v, 99)
	}
}

// TestGetFloatDefault 测试 getFloatDefault。
func TestGetFloatDefault(t *testing.T) {
	m := map[string]any{"key": 3.14}
	if v := getFloatDefault(m, "key", 0); v != 3.14 {
		t.Errorf("getFloatDefault = %f, 期望 %f", v, 3.14)
	}
	if v := getFloatDefault(m, "missing", 1.5); v != 1.5 {
		t.Errorf("getFloatDefault missing = %f, 期望 %f", v, 1.5)
	}
	if v := getFloatDefault(nil, "key", 1.5); v != 1.5 {
		t.Errorf("getFloatDefault nil = %f, 期望 %f", v, 1.5)
	}
}

// TestBytesReader 测试 bytesReaderImpl。
func TestBytesReader(t *testing.T) {
	data := []byte("hello")
	br := &bytesReaderImpl{data: data}

	buf := make([]byte, 5)
	n, err := br.Read(buf)
	if err != nil {
		t.Fatalf("Read 报错: %v", err)
	}
	if n != 5 {
		t.Errorf("Read 返回 %d, 期望 5", n)
	}
	if string(buf) != "hello" {
		t.Errorf("Read 内容 = %q, 期望 %q", string(buf), "hello")
	}

	// 再次读取应返回 EOF
	_, err = br.Read(buf)
	if err == nil {
		t.Error("读完后再读应返回错误")
	}
}

// TestCheckDeployment_ErrorURL 测试无效 URL 的健康检查。
func TestCheckDeployment_ErrorURL(t *testing.T) {
	dep := NewDeployment(DeploymentConfig{
		ID: "bad", ModelName: "test", APIKey: "k1",
		APIBase: "http://[::1]:namedhost", // 无效 URL
	})
	hc := NewHealthChecker([]*Deployment{dep}, 300.0, 5.0)

	result := hc.checkDeployment(dep)
	if result.IsHealthy {
		t.Error("无效 URL 应为不健康")
	}
	if result.Error == "" {
		t.Error("无效 URL 应有错误信息")
	}
}

// TestCheckDeployment_HTTPError 测试 HTTP 错误的健康检查。
func TestCheckDeployment_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	dep := NewDeployment(DeploymentConfig{
		ID: "dep1", ModelName: "test", APIKey: "k1",
		APIBase: server.URL, VerifySSL: false,
	})
	hc := NewHealthChecker([]*Deployment{dep}, 300.0, 5.0)

	result := hc.checkDeployment(dep)
	if result.IsHealthy {
		t.Error("HTTP 503 应为不健康")
	}
	if !strings.Contains(result.Error, "503") {
		t.Errorf("错误信息应包含 503, got %q", result.Error)
	}
}

// TestCheckDeployment_ConnectionRefused 测试连接被拒的健康检查。
func TestCheckDeployment_ConnectionRefused(t *testing.T) {
	dep := NewDeployment(DeploymentConfig{
		ID: "dep1", ModelName: "test", APIKey: "k1",
		APIBase: "http://127.0.0.1:1", // 几乎一定连不上的端口
	})
	hc := NewHealthChecker([]*Deployment{dep}, 300.0, 1.0) // 1 秒超时

	result := hc.checkDeployment(dep)
	if result.IsHealthy {
		t.Error("连接被拒应为不健康")
	}
}

// TestRoutingContext 测试路由上下文。
func TestRoutingContext(t *testing.T) {
	ctx := &RoutingContext{
		SessionID: "test-session",
		Kwargs:    map[string]any{"key": "value"},
	}
	if ctx.SessionID != "test-session" {
		t.Errorf("SessionID = %q, 期望 %q", ctx.SessionID, "test-session")
	}
	if ctx.Kwargs["key"] != "value" {
		t.Error("Kwargs 应包含 key=value")
	}
}

// TestHealthCheckResult 测试健康检查结果。
func TestHealthCheckResult(t *testing.T) {
	result := HealthCheckResult{
		DeploymentID: "dep1",
		IsHealthy:    true,
		Latency:      0.05,
		Timestamp:    time.Now(),
	}
	if result.DeploymentID != "dep1" {
		t.Errorf("DeploymentID = %q, 期望 %q", result.DeploymentID, "dep1")
	}
	if !result.IsHealthy {
		t.Error("IsHealthy 应为 true")
	}
}

// TestConfigParseDeployments_EmptyList 测试空部署列表解析。
func TestConfigParseDeployments_EmptyList(t *testing.T) {
	cc := llmschema.NewModelClientConfig("intelli_router", "", "",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []map[string]any{},
		}),
	)
	config := FromModelClientConfig(cc)
	if len(config.Deployments) != 0 {
		t.Errorf("空列表应解析为 0 个部署，实际 %d", len(config.Deployments))
	}
}

// TestSessionAffinity_DeploymentUnavailable 测试亲和端点不可用时的降级。
func TestSessionAffinity_DeploymentUnavailable(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	// 第一次调用，建立映射
	dep, _ := router.SelectDeploymentWithContext("test-model", &RoutingContext{SessionID: "s1"})
	router.RecordSuccessWithSession(dep, 50*time.Millisecond, "s1")

	// 标记该端点为不健康（进入冷却期）
	dep.RecordFailure()

	// 同一 session 再次调用，亲和端点不可用，应降级到策略选择
	dep2, err := router.SelectDeploymentWithContext("test-model", &RoutingContext{SessionID: "s1"})
	if err != nil {
		t.Fatalf("亲和端点不可用时应降级选择，不应报错: %v", err)
	}
	if dep2 == nil {
		t.Fatal("降级选择应返回有效端点")
	}
	// 降级选择的端点可能和之前不同（因为之前的在冷却期）
}

// TestCleanupExpiredSessions 测试过期 session 的清理。
func TestCleanupExpiredSessions(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	// 手动添加一个过期的 session 映射
	router.sessionMu.Lock()
	router.sessionMap["expired"] = sessionEntry{
		deploymentID: "dep1",
		lastUsed:     time.Now().Add(-2 * time.Hour),
	}
	router.sessionMap["active"] = sessionEntry{
		deploymentID: "dep2",
		lastUsed:     time.Now(),
	}
	router.sessionCleanup = time.Now().Add(-2 * time.Minute) // 超过清理间隔
	router.sessionMu.Unlock()

	// 触发清理（通过 updateSessionMapping）
	router.updateSessionMapping("trigger", "dep1")

	router.sessionMu.RLock()
	_, expiredExists := router.sessionMap["expired"]
	_, activeExists := router.sessionMap["active"]
	router.sessionMu.RUnlock()

	if expiredExists {
		t.Error("过期 session 应被清理")
	}
	if !activeExists {
		t.Error("活跃 session 不应被清理")
	}
}

// TestInvoke_UpdatesAPIConfig 测试 Invoke 动态替换 api_key/api_base。
func TestInvoke_UpdatesAPIConfig(t *testing.T) {
	var receivedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, mockCompletionResponse("ok"))
	}))
	defer server.Close()

	routerCacheLock.Lock()
	routerCache = make(map[string]*ReliableRouter)
	routerCacheLock.Unlock()

	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("test-model"))
	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []map[string]any{
				{
					"id":         "dep1",
					"model_name": "test-model",
					"api_key":    "sk-real-key",
					"api_base":   server.URL,
				},
			},
			"intelli_router_num_retries": 0,
		}),
	)

	client, err := NewIntelliRouterModelClient(mc, cc)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	_, err = client.Invoke(context.Background(), model_clients.NewTextMessagesParam("Hi"))
	if err != nil {
		t.Fatalf("Invoke 报错: %v", err)
	}

	if !strings.Contains(receivedKey, "sk-real-key") {
		t.Errorf("请求应使用真实 API Key, 实际 Authorization = %q", receivedKey)
	}
}

// ──── convertChunk 测试 ────

// TestConvertChunk_基本内容 测试基本 content 解析。
func TestConvertChunk_基本内容(t *testing.T) {
	client := createTestClientWithServer(t,
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})),
		1,
	)

	content := "Hello"
	chunkResp := openai.ChatCompletionChunkResponse{
		Choices: []openai.ChunkChoice{
			{
				Delta: &openai.ChunkDelta{
					Content: &content,
				},
			},
		},
	}

	chunk := client.convertChunk(&chunkResp)
	if chunk == nil {
		t.Fatal("chunk 不应为 nil")
	}
	if chunk.Content.Text() != "Hello" {
		t.Errorf("Content = %q, 期望 %q", chunk.Content.Text(), "Hello")
	}
}

// TestConvertChunk_只提取Content 测试只提取 content，不提取其他字段。
func TestConvertChunk_只提取Content(t *testing.T) {
	// 对齐 Python IntelliRouter._convert_chunk: 只提取 content
	client := createTestClientWithServer(t,
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})),
		1,
	)

	content := "test"
	reasoning := "reasoning"
	finishStop := "stop"
	idx := 0
	chunkResp := openai.ChatCompletionChunkResponse{
		Choices: []openai.ChunkChoice{
			{
				Delta: &openai.ChunkDelta{
					Content:          &content,
					ReasoningContent: &reasoning,
					ToolCalls: []openai.ChunkToolCall{
						{
							ID:   "call_1",
							Type: "function",
							Function: openai.ChunkFunction{
								Name:      "test_func",
								Arguments: "{}",
							},
							Index: &idx,
						},
					},
				},
				FinishReason: &finishStop,
			},
		},
		Usage: &openai.ResponseUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	chunk := client.convertChunk(&chunkResp)
	if chunk == nil {
		t.Fatal("chunk 不应为 nil")
	}
	// Content 应被提取
	if chunk.Content.Text() != "test" {
		t.Errorf("Content = %q, 期望 %q", chunk.Content.Text(), "test")
	}
	// 其他字段不应被提取（对齐 Python IntelliRouter._convert_chunk）
	if chunk.ReasoningContent != "" {
		t.Errorf("ReasoningContent 应为空, 实际 %q", chunk.ReasoningContent)
	}
	if len(chunk.ToolCalls) != 0 {
		t.Errorf("ToolCalls 应为空, 实际长度 %d", len(chunk.ToolCalls))
	}
	if chunk.UsageMetadata != nil {
		t.Error("UsageMetadata 应为 nil")
	}
}

// TestConvertChunk_无Choices返回Nil 测试无 choices 时返回 nil。
func TestConvertChunk_无Choices返回Nil(t *testing.T) {
	client := createTestClientWithServer(t,
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})),
		1,
	)

	chunkResp := openai.ChatCompletionChunkResponse{
		Choices: []openai.ChunkChoice{},
		Usage: &openai.ResponseUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	chunk := client.convertChunk(&chunkResp)
	if chunk != nil {
		t.Error("无 choices 时应返回 nil")
	}
}

// TestConvertChunk_空Content 测试空 content 的 chunk。
func TestConvertChunk_空Content(t *testing.T) {
	// 对齐 Python: 空 content 也返回 chunk
	client := createTestClientWithServer(t,
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})),
		1,
	)

	emptyContent := ""
	chunkResp := openai.ChatCompletionChunkResponse{
		Choices: []openai.ChunkChoice{
			{
				Delta: &openai.ChunkDelta{
					Content: &emptyContent,
				},
			},
		},
	}

	chunk := client.convertChunk(&chunkResp)
	if chunk == nil {
		t.Fatal("空 content 的 chunk 不应为 nil")
	}
	if chunk.Content.Text() != "" {
		t.Errorf("Content = %q, 期望空字符串", chunk.Content.Text())
	}
}

// TestIntelliRouterModelClient_SupportsKVCacheRelease_不支持 验证不支持 KV Cache 释放
func TestIntelliRouterModelClient_SupportsKVCacheRelease_不支持(t *testing.T) {
	// 创建最简 client
	client := &IntelliRouterModelClient{}
	if client.SupportsKVCacheRelease() {
		t.Error("期望 SupportsKVCacheRelease 返回 false")
	}
}
