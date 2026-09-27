package external

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestMem0Provider 创建使用 mock server 的 Mem0Provider
func newTestMem0Provider(server *httptest.Server) *Mem0Provider {
	p := NewMem0Provider("test-key", "user-1", "agent-1", false)
	// 替换 client 指向 mock server
	p.mu.Lock()
	p.client = newMem0HTTPClient("test-key", server.URL)
	p.initialized = true
	p.mu.Unlock()
	return p
}

func TestMem0Provider_Name(t *testing.T) {
	p := NewMem0Provider("key", "u1", "a1", false)
	if p.Name() != "mem0" {
		t.Errorf("Name() = %q, want %q", p.Name(), "mem0")
	}
}

func TestMem0Provider_IsAvailable(t *testing.T) {
	p1 := NewMem0Provider("key", "u1", "", false)
	if !p1.IsAvailable() {
		t.Error("IsAvailable() = false, want true (有 apiKey)")
	}
	p2 := NewMem0Provider("", "u1", "", false)
	if p2.IsAvailable() {
		t.Error("IsAvailable() = true, want false (无 apiKey)")
	}
}

func TestMem0Provider_Initialize(t *testing.T) {
	p := NewMem0Provider("key", "", "", false)
	err := p.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !p.IsInitialized() {
		t.Error("IsInitialized() = false, want true")
	}
	if p.client == nil {
		t.Error("client 应已初始化")
	}
}

func TestMem0Provider_Initialize_覆盖参数(t *testing.T) {
	p := NewMem0Provider("key", "old-user", "old-agent", false)
	err := p.Initialize(context.Background(),
		WithUserID("new-user"),
		WithScopeID("new-agent"),
	)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if p.userID != "new-user" {
		t.Errorf("userID = %q, want %q", p.userID, "new-user")
	}
	if p.agentID != "new-agent" {
		t.Errorf("agentID = %q, want %q", p.agentID, "new-agent")
	}
}

func TestMem0Provider_Initialize_apiKey缺失(t *testing.T) {
	p := NewMem0Provider("", "", "", false)
	err := p.Initialize(context.Background())
	if err == nil {
		t.Fatal("Initialize() 应返回错误（apiKey 为空）")
	}
}

func TestMem0Provider_GetToolSchemas(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	schemas := p.GetToolSchemas()
	if len(schemas) != 3 {
		t.Fatalf("len(GetToolSchemas()) = %d, want 3", len(schemas))
	}
	names := []string{schemas[0].Name, schemas[1].Name, schemas[2].Name}
	expected := []string{"mem0_profile", "mem0_search", "mem0_conclude"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("schemas[%d].Name = %q, want %q", i, name, expected[i])
		}
	}
}

func TestMem0Provider_SystemPromptBlock(t *testing.T) {
	p := NewMem0Provider("key", "user-42", "", false)
	block := p.SystemPromptBlock()
	if block == "" {
		t.Fatal("SystemPromptBlock() 返回空串")
	}
	if !strings.Contains(block, "user-42") {
		t.Errorf("SystemPromptBlock() = %q, 应包含 userID", block)
	}
}

func TestMem0Provider_Prefetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"memory": "偏好Go", "score": 0.9},
				{"memory": "使用vim", "score": 0.7},
			},
		})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.Prefetch(context.Background(), "测试", WithUserID("user-1"))
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if !strings.Contains(result, "## Mem0 Memory") {
		t.Errorf("Prefetch() = %q, 应包含 '## Mem0 Memory'", result)
	}
	if !strings.Contains(result, "偏好Go") {
		t.Errorf("Prefetch() = %q, 应包含 '偏好Go'", result)
	}
}

func TestMem0Provider_Prefetch_空结果(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.Prefetch(context.Background(), "不存在")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() = %q, want empty", result)
	}
}

func TestMem0Provider_Prefetch_空query(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	result, err := p.Prefetch(context.Background(), "")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() = %q, want empty (空 query)", result)
	}
}

func TestMem0Provider_Prefetch_API失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.Prefetch(context.Background(), "测试")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() = %q, want empty (API 失败)", result)
	}
	if p.consecutiveFailures != 1 {
		t.Errorf("consecutiveFailures = %d, want 1", p.consecutiveFailures)
	}
}

func TestMem0Provider_SyncTurn(t *testing.T) {
	var called int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "mem-1"})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	err := p.SyncTurn(context.Background(), "你好", "你好！")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("API 调用次数 = %d, want 1", atomic.LoadInt32(&called))
	}
}

func TestMem0Provider_SyncTurn_空消息(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	err := p.SyncTurn(context.Background(), "", "assistant")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
	err = p.SyncTurn(context.Background(), "user", "")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
}

func TestMem0Provider_SyncTurn_API失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	err := p.SyncTurn(context.Background(), "你好", "你好！")
	if err == nil {
		t.Fatal("SyncTurn() 应返回错误")
	}
	if p.consecutiveFailures != 1 {
		t.Errorf("consecutiveFailures = %d, want 1", p.consecutiveFailures)
	}
}

func TestMem0Provider_SyncTurn_熔断器开启(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	for i := 0; i < 5; i++ {
		p.recordFailure()
	}
	err := p.SyncTurn(context.Background(), "你好", "你好！")
	if err != nil {
		t.Fatalf("SyncTurn() 熔断器开启时应返回 nil, got %v", err)
	}
}

func TestMem0Provider_HandleToolCall_Profile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"memory": "偏好暗色主题"},
			{"memory": "使用zsh"},
		})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.HandleToolCall(context.Background(), "mem0_profile", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["result"] == nil {
		t.Error("result 应有值")
	}
	if count, ok := parsed["count"].(float64); !ok || int(count) != 2 {
		t.Errorf("count = %v, want 2", parsed["count"])
	}
}

func TestMem0Provider_HandleToolCall_Profile_空结果(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.HandleToolCall(context.Background(), "mem0_profile", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["result"] != "No memories stored yet." {
		t.Errorf("result = %v, want 'No memories stored yet.'", parsed["result"])
	}
}

func TestMem0Provider_HandleToolCall_Search(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"memory": "Go语言", "score": 0.95},
			},
		})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.HandleToolCall(context.Background(), "mem0_search", map[string]any{
		"query": "语言偏好",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["results"] == nil {
		t.Error("results 应有值")
	}
}

func TestMem0Provider_HandleToolCall_Search_缺query(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	result, err := p.HandleToolCall(context.Background(), "mem0_search", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	errMsg, _ := parsed["error"].(string)
	if !strings.Contains(errMsg, "query") {
		t.Errorf("error = %q, 应提及 query", errMsg)
	}
}

func TestMem0Provider_HandleToolCall_Conclude(t *testing.T) {
	var called int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		// 验证 infer=false
		var req mem0AddRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Infer == nil || *req.Infer != false {
			t.Errorf("infer = %v, want false", req.Infer)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "mem-1"})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.HandleToolCall(context.Background(), "mem0_conclude", map[string]any{
		"conclusion": "我喜欢Go语言",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["result"] != "Fact stored." {
		t.Errorf("result = %v, want 'Fact stored.'", parsed["result"])
	}
}

func TestMem0Provider_HandleToolCall_未知工具(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	p.initialized = true
	result, err := p.HandleToolCall(context.Background(), "unknown_tool", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	errMsg, _ := parsed["error"].(string)
	if !strings.Contains(errMsg, "Unknown tool") {
		t.Errorf("error = %q, 应提及 Unknown tool", errMsg)
	}
}

func TestMem0Provider_熔断器_开启与恢复(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	p.initialized = true

	// 连续失败 5 次
	for i := 0; i < 5; i++ {
		p.recordFailure()
	}
	if !p.isBreakerOpen() {
		t.Error("熔断器应已开启")
	}

	// 冷却期后应自动恢复
	p.breakerOpenUntil = time.Now().Add(-1 * time.Second) // 模拟冷却期已过
	if p.isBreakerOpen() {
		t.Error("冷却期过后熔断器应关闭")
	}

	// 成功后重置
	p.consecutiveFailures = 4
	p.recordSuccess()
	if p.consecutiveFailures != 0 {
		t.Errorf("consecutiveFailures = %d, want 0 (成功后重置)", p.consecutiveFailures)
	}
}

func TestMem0Provider_HandleToolCall_熔断器开启(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	p.initialized = true

	// 触发熔断
	for i := 0; i < 5; i++ {
		p.recordFailure()
	}

	result, err := p.HandleToolCall(context.Background(), "mem0_search", map[string]any{"query": "test"})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	errMsg, _ := parsed["error"].(string)
	if !strings.Contains(errMsg, "temporarily unavailable") {
		t.Errorf("error = %q, 应提及 temporarily unavailable", errMsg)
	}
}

func TestMem0Provider_Prefetch_熔断器开启(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	p.initialized = true

	for i := 0; i < 5; i++ {
		p.recordFailure()
	}

	result, err := p.Prefetch(context.Background(), "测试")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() = %q, want empty (熔断器开启)", result)
	}
}

func TestMem0Provider_QueuePrefetch(t *testing.T) {
	var called int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	p.QueuePrefetch(context.Background(), "预热查询")

	// 等待 goroutine 完成
	p.prefetchWg.Wait()

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("API 调用次数 = %d, want 1", atomic.LoadInt32(&called))
	}
}

func TestMem0Provider_QueuePrefetch_API失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	p.QueuePrefetch(context.Background(), "预热查询")

	// 等待 goroutine 完成
	p.prefetchWg.Wait()

	if p.consecutiveFailures != 1 {
		t.Errorf("consecutiveFailures = %d, want 1", p.consecutiveFailures)
	}
}

func TestMem0Provider_QueuePrefetch_熔断器开启(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	p.initialized = true

	for i := 0; i < 5; i++ {
		p.recordFailure()
	}

	// 不应启动 goroutine
	p.QueuePrefetch(context.Background(), "测试")
	// 无 panic 即可
}

func TestMem0Provider_Shutdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	p.QueuePrefetch(context.Background(), "预热")

	err := p.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if p.IsInitialized() {
		t.Error("Shutdown 后 IsInitialized() 应为 false")
	}
	if p.client != nil {
		t.Error("Shutdown 后 client 应为 nil")
	}
}

func TestMem0Provider_IsInitialized(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	if p.IsInitialized() {
		t.Error("初始化前 IsInitialized() 应为 false")
	}
	p.Initialize(context.Background())
	if !p.IsInitialized() {
		t.Error("初始化后 IsInitialized() 应为 true")
	}
}

func TestMem0Provider_接口满足(t *testing.T) {
	// 编译时验证 Mem0Provider 满足 MemoryProvider 接口
	var _ MemoryProvider = (*Mem0Provider)(nil)
}

func TestMem0Provider_readFilters_无agentID(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	filters := p.readFilters()
	if filters["user_id"] != "u1" {
		t.Errorf("user_id = %v, want u1", filters["user_id"])
	}
	if _, ok := filters["agent_id"]; ok {
		t.Error("agent_id 不应存在（为空时省略）")
	}
}

func TestMem0Provider_readFilters_有agentID(t *testing.T) {
	p := NewMem0Provider("key", "u1", "a1", false)
	filters := p.readFilters()
	if filters["agent_id"] != "a1" {
		t.Errorf("agent_id = %v, want a1", filters["agent_id"])
	}
}

func TestMem0Provider_writeFilters(t *testing.T) {
	p := NewMem0Provider("key", "u1", "a1", false)
	filters := p.writeFilters()
	if filters["user_id"] != "u1" {
		t.Errorf("user_id = %v, want u1", filters["user_id"])
	}
	if filters["agent_id"] != "a1" {
		t.Errorf("agent_id = %v, want a1", filters["agent_id"])
	}
}

func TestMem0Provider_getClient_延迟初始化(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	if p.client != nil {
		t.Error("初始化前 client 应为 nil")
	}
	client := p.getClient()
	if client == nil {
		t.Error("getClient() 应返回非 nil 客户端")
	}
	if p.client == nil {
		t.Error("getClient() 后 client 应被缓存")
	}
}
