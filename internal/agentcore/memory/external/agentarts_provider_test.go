package external

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestAgentArtsProvider 创建使用 mock server 的 AgentArtsProvider。
// 直接注入 client，绕过 sync.Once 懒加载。
func newTestAgentArtsProvider(server *httptest.Server) *AgentArtsProvider {
	p := NewAgentArtsProvider(server.URL, "test-key", "test-space", "default-actor", "default-asst", nil)
	// 直接注入 client，绕过 sync.Once
	p.clientOnce = sync.Once{}
	p.client = newAgentArtsClient(server.URL, "test-key")
	return p
}

func TestNewAgentArtsProvider_默认KVStore(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	if p.sessionMappingStore == nil {
		t.Error("sessionMappingStore 不应为 nil")
	}
}

func TestNewAgentArtsProvider_自定义KVStore(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	p := NewAgentArtsProvider("", "key", "space", "", "", store)
	if p.sessionMappingStore != store {
		t.Error("sessionMappingStore 应为自定义 store")
	}
}

func TestAgentArtsProvider_Name(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	if p.Name() != "agentarts" {
		t.Errorf("Name() = %q, want %q", p.Name(), "agentarts")
	}
}

func TestAgentArtsProvider_IsAvailable_有key有space(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	if !p.IsAvailable() {
		t.Error("IsAvailable() 应返回 true")
	}
}

func TestAgentArtsProvider_IsAvailable_无key(t *testing.T) {
	p := NewAgentArtsProvider("", "", "space", "", "", nil)
	if p.IsAvailable() {
		t.Error("IsAvailable() 应返回 false（无 api_key）")
	}
}

func TestAgentArtsProvider_IsAvailable_无space(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "", "", "", nil)
	if p.IsAvailable() {
		t.Error("IsAvailable() 应返回 false（无 space_id）")
	}
}

func TestAgentArtsProvider_IsInitialized(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	if p.IsInitialized() {
		t.Error("IsInitialized() 初始应为 false")
	}
}

func TestAgentArtsProvider_GetToolSchemas(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	schemas := p.GetToolSchemas()
	if len(schemas) != 1 {
		t.Fatalf("len(schemas) = %d, want 1", len(schemas))
	}
	if schemas[0].Name != "external_memory_search" {
		t.Errorf("Name = %q, want %q", schemas[0].Name, "external_memory_search")
	}
}

func TestAgentArtsProvider_SystemPromptBlock(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	block := p.SystemPromptBlock()
	if block == "" {
		t.Error("SystemPromptBlock() 不应为空")
	}
	if len(block) < 17 || block[:17] != "# External Memory" {
		t.Errorf("SystemPromptBlock() 应以 '# External Memory' 开头, got %q", block)
	}
}

func TestAgentArtsProvider_Initialize_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// createMemorySession 响应
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "ms-uuid-123",
			"space_id":     "test-space",
			"actor_id":     "user1",
			"assistant_id": "scope1",
		})
	}))
	defer server.Close()

	p := NewAgentArtsProvider(server.URL, "test-key", "test-space", "default-actor", "default-asst", nil)
	p.client = newAgentArtsClient(server.URL, "test-key")

	err := p.Initialize(context.Background(),
		WithUserID("user1"),
		WithScopeID("scope1"),
		WithSessionID("sess1"),
	)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !p.IsInitialized() {
		t.Error("Initialize() 后 IsInitialized() 应为 true")
	}
	if p.actorID != "user1" {
		t.Errorf("actorID = %q, want %q", p.actorID, "user1")
	}
	if p.assistantID != "scope1" {
		t.Errorf("assistantID = %q, want %q", p.assistantID, "scope1")
	}
}

func TestAgentArtsProvider_Initialize_无sessionID(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	err := p.Initialize(context.Background(), WithUserID("user1"))
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !p.IsInitialized() {
		t.Error("Initialize() 后 IsInitialized() 应为 true")
	}
}

func TestAgentArtsProvider_Initialize_已有映射(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	// 预存映射
	store.Set(context.Background(), "agentarts/session_mapping/sess2", []byte("existing-ms-id"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 不应该被调用
		t.Error("createMemorySession 不应被调用")
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	p := NewAgentArtsProvider(server.URL, "test-key", "test-space", "default-actor", "default-asst", store)
	p.client = newAgentArtsClient(server.URL, "test-key")

	err := p.Initialize(context.Background(), WithSessionID("sess2"))
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !p.IsInitialized() {
		t.Error("Initialize() 后 IsInitialized() 应为 true")
	}
}

func TestAgentArtsProvider_Prefetch_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []map[string]any{
				{"record": map[string]any{"id": "m1", "content": "用户偏好中文", "strategy_type": "semantic"}, "score": 0.9},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	result, err := p.Prefetch(context.Background(), "用户偏好")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result == "" {
		t.Fatal("Prefetch() 结果不应为空")
	}
	if len(result) < 18 || result[:18] != "## External Memory" {
		t.Errorf("Prefetch() 应以 '## External Memory' 开头, got %q", result)
	}
}

func TestAgentArtsProvider_Prefetch_空查询(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	result, err := p.Prefetch(context.Background(), "")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() 空查询应返回空字符串, got %q", result)
	}
}

func TestAgentArtsProvider_Prefetch_搜索失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	result, err := p.Prefetch(context.Background(), "test")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() 搜索失败应返回空字符串, got %q", result)
	}
	if p.consecutiveFailures != 1 {
		t.Errorf("consecutiveFailures = %d, want 1", p.consecutiveFailures)
	}
}

func TestAgentArtsProvider_HandleToolCall_正常搜索(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []map[string]any{
				{"record": map[string]any{"id": "m1", "content": "记忆内容", "strategy_type": "semantic"}, "score": 0.95},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	result, err := p.HandleToolCall(context.Background(), "external_memory_search", map[string]any{
		"query": "test",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if _, ok := parsed["results"]; !ok {
		t.Errorf("HandleToolCall() 结果应包含 'results', got %v", parsed)
	}
}

func TestAgentArtsProvider_HandleToolCall_未知工具(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	result, err := p.HandleToolCall(context.Background(), "unknown_tool", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]string
	json.Unmarshal([]byte(result), &parsed)
	if parsed["error"] != "Unknown tool: unknown_tool" {
		t.Errorf("error = %q, want 'Unknown tool: unknown_tool'", parsed["error"])
	}
}

func TestAgentArtsProvider_HandleToolCall_缺少query(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	result, err := p.HandleToolCall(context.Background(), "external_memory_search", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]string
	json.Unmarshal([]byte(result), &parsed)
	if parsed["error"] != "Missing required parameter: query" {
		t.Errorf("error = %q, want 'Missing required parameter: query'", parsed["error"])
	}
}

func TestAgentArtsProvider_HandleToolCall_搜索失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	result, err := p.HandleToolCall(context.Background(), "external_memory_search", map[string]any{
		"query": "test",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]string
	json.Unmarshal([]byte(result), &parsed)
	if _, ok := parsed["error"]; !ok {
		t.Errorf("搜索失败应返回 error, got %v", parsed)
	}
	if p.consecutiveFailures != 1 {
		t.Errorf("consecutiveFailures = %d, want 1", p.consecutiveFailures)
	}
}

func TestAgentArtsProvider_SyncTurn_正常(t *testing.T) {
	sessionCreated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/core/spaces/test-space/sessions" {
			sessionCreated = true
			json.NewEncoder(w).Encode(map[string]any{
				"id": "ms-uuid-456", "space_id": "test-space",
			})
			return
		}
		// addMessages 响应
		json.NewEncoder(w).Encode(map[string]any{"messages": []any{}})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	err := p.SyncTurn(context.Background(), "你好", "你好！", WithSessionID("sess1"))
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
	if !sessionCreated {
		t.Error("SyncTurn 应创建 memory session")
	}
	if p.consecutiveFailures != 0 {
		t.Errorf("consecutiveFailures = %d, want 0", p.consecutiveFailures)
	}
}

func TestAgentArtsProvider_SyncTurn_空消息跳过(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	err := p.SyncTurn(context.Background(), "", "assistant msg")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
	err = p.SyncTurn(context.Background(), "user msg", "")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
}

func TestAgentArtsProvider_SyncTurn_错误吞掉(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	err := p.SyncTurn(context.Background(), "user msg", "assistant msg", WithSessionID("sess1"))
	if err != nil {
		t.Fatalf("SyncTurn() 应吞掉错误返回 nil, got %v", err)
	}
	if p.consecutiveFailures != 1 {
		t.Errorf("consecutiveFailures = %d, want 1", p.consecutiveFailures)
	}
}

func TestAgentArtsProvider_Shutdown(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	p.initialized = true
	p.client = newAgentArtsClient("http://example.com", "key")

	err := p.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if p.IsInitialized() {
		t.Error("Shutdown() 后 IsInitialized() 应为 false")
	}
	if p.client != nil {
		t.Error("Shutdown() 后 client 应为 nil")
	}
}

func TestAgentArtsProvider_ConsecutiveFailures_计数(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)

	p.recordFailure()
	p.recordFailure()
	if p.consecutiveFailures != 2 {
		t.Errorf("consecutiveFailures = %d, want 2", p.consecutiveFailures)
	}

	p.recordSuccess()
	if p.consecutiveFailures != 0 {
		t.Errorf("consecutiveFailures = %d, want 0 after recordSuccess", p.consecutiveFailures)
	}
}

func TestAgentArtsProvider_RuntimeActorID_优先级(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "default-actor", "", nil)
	p.actorID = "runtime-actor"

	// 1. args["user_id"] 优先
	args := map[string]any{"user_id": "args-actor"}
	if got := p.runtimeActorID(args); got != "args-actor" {
		t.Errorf("runtimeActorID() = %q, want %q", got, "args-actor")
	}

	// 2. p.actorID 其次
	if got := p.runtimeActorID(map[string]any{}); got != "runtime-actor" {
		t.Errorf("runtimeActorID() = %q, want %q", got, "runtime-actor")
	}

	// 3. p.defaultActorID 最后
	p.actorID = ""
	if got := p.runtimeActorID(map[string]any{}); got != "default-actor" {
		t.Errorf("runtimeActorID() = %q, want %q", got, "default-actor")
	}
}

func TestAgentArtsProvider_RuntimeAssistantID_优先级(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "default-asst", nil)
	p.assistantID = "runtime-asst"

	// 1. args["assistant_id"] 优先
	args := map[string]any{"assistant_id": "args-asst"}
	if got := p.runtimeAssistantID(args); got != "args-asst" {
		t.Errorf("runtimeAssistantID() = %q, want %q", got, "args-asst")
	}

	// 2. args["scope_id"] 其次
	args = map[string]any{"scope_id": "scope-asst"}
	if got := p.runtimeAssistantID(args); got != "scope-asst" {
		t.Errorf("runtimeAssistantID() = %q, want %q", got, "scope-asst")
	}

	// 3. p.assistantID 再次
	if got := p.runtimeAssistantID(map[string]any{}); got != "runtime-asst" {
		t.Errorf("runtimeAssistantID() = %q, want %q", got, "runtime-asst")
	}

	// 4. p.defaultAssistantID 最后
	p.assistantID = ""
	if got := p.runtimeAssistantID(map[string]any{}); got != "default-asst" {
		t.Errorf("runtimeAssistantID() = %q, want %q", got, "default-asst")
	}
}

func TestAgentArtsProvider_SessionMappingKey(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	key := p.sessionMappingKey("sess1")
	expected := "agentarts/session_mapping/sess1"
	if key != expected {
		t.Errorf("sessionMappingKey() = %q, want %q", key, expected)
	}
}

// ──────────────────────────── 补充覆盖率测试 ────────────────────────────

func TestAgentArtsProvider_EnsureMemorySession_无sessionID回退到实例存储(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	store.Set(context.Background(), "agentarts/session_mapping/stored-sess", []byte("ms-stored"))

	p := NewAgentArtsProvider("", "key", "space", "", "", store)
	// 不传 sessionID，但 p.sessionID 有值 → 应回退到 p.sessionID
	p.sessionID = "stored-sess"

	id, err := p.ensureMemorySession(context.Background(), "", "actor1", "asst1")
	if err != nil {
		t.Fatalf("ensureMemorySession() 应回退到 p.sessionID, error = %v", err)
	}
	if id != "ms-stored" {
		t.Errorf("id = %q, want %q", id, "ms-stored")
	}
}

func TestAgentArtsProvider_EnsureMemorySession_无sessionID且无实例存储报错(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	_, err := p.ensureMemorySession(context.Background(), "", "actor1", "asst1")
	if err == nil {
		t.Fatal("ensureMemorySession() 空 sessionID 且无实例存储应返回错误")
	}
}

func TestAgentArtsProvider_EnsureMemorySession_创建失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	_, err := p.ensureMemorySession(context.Background(), "sess1", "actor1", "asst1")
	if err == nil {
		t.Fatal("ensureMemorySession() 创建失败应返回错误")
	}
}

func TestAgentArtsProvider_EnsureMemorySession_返回空ID报错(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "", "space_id": "test-space",
		})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	_, err := p.ensureMemorySession(context.Background(), "sess1", "actor1", "asst1")
	if err == nil {
		t.Fatal("ensureMemorySession() 空 ID 应返回错误")
	}
}

func TestAgentArtsProvider_EnsureMemorySession_actorFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req sessionCreateRequest
		json.NewDecoder(r.Body).Decode(&req)
		// 验证 fallback 到 defaultActorID
		if req.ActorID != "default-actor" {
			t.Errorf("ActorID = %q, want %q (fallback)", req.ActorID, "default-actor")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "ms-uuid-fallback", "space_id": "test-space",
		})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	// 不传 actorID 和 assistantID，应 fallback 到 default 值
	id, err := p.ensureMemorySession(context.Background(), "sess-fallback", "", "")
	if err != nil {
		t.Fatalf("ensureMemorySession() error = %v", err)
	}
	if id != "ms-uuid-fallback" {
		t.Errorf("id = %q, want %q", id, "ms-uuid-fallback")
	}
}

func TestAgentArtsProvider_Search_带strategyType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var filter memorySearchFilter
		json.NewDecoder(r.Body).Decode(&filter)
		if filter.StrategyType != "episodic" {
			t.Errorf("StrategyType = %q, want %q", filter.StrategyType, "episodic")
		}
		if filter.ActorID != "default-actor" {
			t.Errorf("ActorID = %q, want %q", filter.ActorID, "default-actor")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []map[string]any{
				{"record": map[string]any{"id": "m1", "content": "记忆1", "strategy_type": "episodic"}, "score": 0.9},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	items, err := p.search(context.Background(), "test", map[string]any{
		"strategy_type": "episodic",
	})
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("len(items) = %d, want 1", len(items))
	}
}

func TestAgentArtsProvider_Search_搜索失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	_, err := p.search(context.Background(), "test", map[string]any{})
	if err == nil {
		t.Fatal("search() 失败应返回错误")
	}
}

func TestAgentArtsProvider_GetClient_懒加载(t *testing.T) {
	p := NewAgentArtsProvider("http://example.com", "key", "space", "", "", nil)
	if p.client != nil {
		t.Error("初始 client 应为 nil")
	}
	c := p.getClient()
	if c == nil {
		t.Error("getClient() 应返回非 nil 客户端")
	}
	// 第二次调用应返回同一实例
	c2 := p.getClient()
	if c != c2 {
		t.Error("getClient() 应返回同一实例")
	}
}

func TestAgentArtsClient_DoRequest_无body(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	var result map[string]any
	err := client.doRequest(context.Background(), http.MethodGet, "/test", nil, &result)
	if err != nil {
		t.Fatalf("doRequest() error = %v", err)
	}
}

func TestAgentArtsClient_DoRequest_无result(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": "test"}`))
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	err := client.doRequest(context.Background(), http.MethodPost, "/test", map[string]any{"key": "val"}, nil)
	if err != nil {
		t.Fatalf("doRequest() error = %v", err)
	}
}

func TestAgentArtsClient_DoRequest_无效JSON响应(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not json`))
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	var result map[string]any
	err := client.doRequest(context.Background(), http.MethodPost, "/test", map[string]any{}, &result)
	if err == nil {
		t.Fatal("doRequest() 无效 JSON 应返回错误")
	}
}

func TestAgentArtsProvider_SyncTurn_使用已有Session(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	// 预存映射
	store.Set(context.Background(), "agentarts/session_mapping/existing-sess", []byte("existing-ms-id"))

	addMsgCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/core/spaces/test-space/sessions" {
			t.Error("createMemorySession 不应被调用")
		}
		if r.URL.Path == "/v1/core/spaces/test-space/sessions/existing-ms-id/messages" {
			addMsgCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"messages": []any{}})
	}))
	defer server.Close()

	p := NewAgentArtsProvider(server.URL, "test-key", "test-space", "default-actor", "default-asst", store)
	p.clientOnce = sync.Once{}
	p.client = newAgentArtsClient(server.URL, "test-key")

	err := p.SyncTurn(context.Background(), "你好", "你好！", WithSessionID("existing-sess"))
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
	if !addMsgCalled {
		t.Error("addMessages 应被调用")
	}
}

func TestAgentArtsProvider_HandleToolCall_空结果(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []any{},
			"total":   0,
		})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	result, err := p.HandleToolCall(context.Background(), "external_memory_search", map[string]any{
		"query": "不存在",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}
	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["result"] != "No relevant memories found." {
		t.Errorf("result = %v, want 'No relevant memories found.'", parsed["result"])
	}
}

func TestAgentArtsProvider_Prefetch_成功归零(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	p.consecutiveFailures = 3

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []map[string]any{
				{"record": map[string]any{"id": "m1", "content": "内容", "strategy_type": "semantic"}, "score": 0.9},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	p.clientOnce = sync.Once{}
	p.client = newAgentArtsClient(server.URL, "test-key")

	_, _ = p.Prefetch(context.Background(), "test")
	if p.consecutiveFailures != 0 {
		t.Errorf("consecutiveFailures = %d, want 0 after success", p.consecutiveFailures)
	}
}

func TestAgentArtsClient_SearchMemories_带actorID和strategyType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var filter memorySearchFilter
		json.NewDecoder(r.Body).Decode(&filter)
		if filter.ActorID != "user1" {
			t.Errorf("ActorID = %q, want %q", filter.ActorID, "user1")
		}
		if filter.StrategyType != "user_preference" {
			t.Errorf("StrategyType = %q, want %q", filter.StrategyType, "user_preference")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []any{},
			"total":   0,
		})
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	_, err := client.searchMemories(context.Background(), "space1", memorySearchFilter{
		Query:        "test",
		ActorID:      "user1",
		StrategyType: "user_preference",
	})
	if err != nil {
		t.Fatalf("searchMemories() error = %v", err)
	}
}

func TestAgentArtsProvider_Search_topK截断(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var filter memorySearchFilter
		json.NewDecoder(r.Body).Decode(&filter)
		// top_k 超过 maxTopK 应被截断
		if filter.TopK != 100 {
			t.Errorf("TopK = %d, want 100 (truncated from 200)", filter.TopK)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"records": []any{}, "total": 0})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	_, err := p.search(context.Background(), "test", map[string]any{
		"top_k": float64(200), // 超过 maxTopK=100
	})
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
}

func TestAgentArtsProvider_Search_minScoreInt类型(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var filter memorySearchFilter
		json.NewDecoder(r.Body).Decode(&filter)
		if filter.MinScore != 1.0 {
			t.Errorf("MinScore = %f, want 1.0", filter.MinScore)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"records": []any{}, "total": 0})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	_, err := p.search(context.Background(), "test", map[string]any{
		"min_score": int(1), // int 类型分支
	})
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
}

func TestAgentArtsProvider_Search_topKInt类型(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var filter memorySearchFilter
		json.NewDecoder(r.Body).Decode(&filter)
		if filter.TopK != 5 {
			t.Errorf("TopK = %d, want 5", filter.TopK)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"records": []any{}, "total": 0})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	_, err := p.search(context.Background(), "test", map[string]any{
		"top_k": int(5), // int 类型分支
	})
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
}

func TestAgentArtsProvider_Prefetch_所有记忆为空content(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []map[string]any{
				{"record": map[string]any{"id": "m1", "content": "", "strategy_type": "semantic"}, "score": 0.9},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	result, err := p.Prefetch(context.Background(), "test")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() 空 content 应返回空字符串, got %q", result)
	}
}

func TestAgentArtsProvider_Initialize_ensureMemorySession失败传播错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := NewAgentArtsProvider(server.URL, "test-key", "test-space", "default-actor", "default-asst", nil)
	p.client = newAgentArtsClient(server.URL, "test-key")

	// 对齐 Python: ensureMemorySession 失败时异常传播，initialized 不设为 true
	err := p.Initialize(context.Background(), WithSessionID("sess-fail"))
	if err == nil {
		t.Fatal("Initialize() 应返回错误（ensureMemorySession 失败）")
	}
	if p.IsInitialized() {
		t.Error("Initialize() ensureMemorySession 失败时 IsInitialized() 不应为 true")
	}
}

func TestAgentArtsClient_DoRequest_序列化失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()
	client := newTestAgentArtsClient(server)
	// 无法序列化的类型
	err := client.doRequest(context.Background(), http.MethodPost, "/test", make(chan int), nil)
	if err == nil {
		t.Fatal("doRequest() 应返回序列化错误")
	}
}

func TestAgentArtsProvider_SyncTurn_KVStore读取失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r, ok := w.(http.ResponseWriter); ok && r != nil {
			_ = r
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": "ms-uuid-kvfail", "space_id": "test-space",
		})
	}))
	defer server.Close()

	// 使用一个读取会报错的 mock store
	p := NewAgentArtsProvider(server.URL, "test-key", "test-space", "default-actor", "default-asst",
		&errorKVStore{getErr: fmt.Errorf("kv read error")})
	p.clientOnce = sync.Once{}
	p.client = newAgentArtsClient(server.URL, "test-key")

	// KVStore 读取失败，但应 fallback 到创建新 session
	err := p.SyncTurn(context.Background(), "你好", "你好！", WithSessionID("sess1"))
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
}

// errorKVStore 用于测试 KVStore 读取失败的场景
type errorKVStore struct {
	getErr error
}

func (s *errorKVStore) Set(_ context.Context, _ string, _ []byte) error { return nil }
func (s *errorKVStore) ExclusiveSet(_ context.Context, _ string, _ []byte, _ int) (bool, error) {
	return false, nil
}
func (s *errorKVStore) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, s.getErr
}
func (s *errorKVStore) Exists(_ context.Context, _ string) (bool, error)    { return false, nil }
func (s *errorKVStore) Delete(_ context.Context, _ string) error             { return nil }
func (s *errorKVStore) GetByPrefix(_ context.Context, _ string) (map[string][]byte, error) {
	return nil, nil
}
func (s *errorKVStore) DeleteByPrefix(_ context.Context, _ string, _ int) error { return nil }
func (s *errorKVStore) MGet(_ context.Context, _ []string) ([][]byte, error)    { return nil, nil }
func (s *errorKVStore) BatchDelete(_ context.Context, _ []string, _ int) (int, error) {
	return 0, nil
}
func (s *errorKVStore) Pipeline(_ context.Context) kv.KVPipeline { return nil }
