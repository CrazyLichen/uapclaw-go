package external

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestAgentArtsClient 创建使用 mock server 的 agentartsClient。
func newTestAgentArtsClient(server *httptest.Server) *agentartsClient {
	return &agentartsClient{
		baseURL:    server.URL,
		apiKey:     "test-api-key",
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func TestNewAgentArtsClient_默认URL(t *testing.T) {
	client := newAgentArtsClient("", "key")
	if client.baseURL != agentartsDefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", client.baseURL, agentartsDefaultBaseURL)
	}
}

func TestNewAgentArtsClient_自定义URL(t *testing.T) {
	client := newAgentArtsClient("https://custom.example.com", "key")
	if client.baseURL != "https://custom.example.com" {
		t.Errorf("baseURL = %q, want custom URL", client.baseURL)
	}
}

func TestNewAgentArtsClient_尾部斜杠去除(t *testing.T) {
	client := newAgentArtsClient("https://custom.example.com/", "key")
	if client.baseURL != "https://custom.example.com" {
		t.Errorf("baseURL = %q, want no trailing slash", client.baseURL)
	}
}

func TestAgentArtsClient_SearchMemories_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/core/spaces/space1/memories/search" {
			t.Errorf("Path = %q, want /v1/core/spaces/space1/memories/search", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("Authorization = %q, want 'Bearer test-api-key'", r.Header.Get("Authorization"))
		}

		var filter memorySearchFilter
		json.NewDecoder(r.Body).Decode(&filter)
		if filter.Query != "测试查询" {
			t.Errorf("Query = %q, want %q", filter.Query, "测试查询")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []map[string]any{
				{
					"record": map[string]any{"id": "m1", "content": "用户偏好Go语言", "strategy_type": "semantic"},
					"score":   0.95,
				},
				{
					"record": map[string]any{"id": "m2", "content": "用户使用VSCode", "strategy_type": "semantic"},
					"score":   0.8,
				},
			},
			"total": 2,
			"query": "测试查询",
		})
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	items, err := client.searchMemories(context.Background(), "space1", memorySearchFilter{
		Query:    "测试查询",
		TopK:     10,
		MinScore: 0.5,
	})
	if err != nil {
		t.Fatalf("searchMemories() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Memory != "用户偏好Go语言" {
		t.Errorf("Memory = %q, want %q", items[0].Memory, "用户偏好Go语言")
	}
	if items[0].Score != 0.95 {
		t.Errorf("Score = %f, want 0.95", items[0].Score)
	}
}

func TestAgentArtsClient_SearchMemories_空结果(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []any{},
			"total":   0,
			"query":   "不存在",
		})
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	items, err := client.searchMemories(context.Background(), "space1", memorySearchFilter{Query: "不存在"})
	if err != nil {
		t.Fatalf("searchMemories() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestAgentArtsClient_SearchMemories_过滤空content(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []map[string]any{
				{"record": map[string]any{"id": "m1", "content": "", "strategy_type": "semantic"}, "score": 0.9},
				{"record": map[string]any{"id": "m2", "content": "有效内容", "strategy_type": "semantic"}, "score": 0.8},
			},
			"total": 2,
		})
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	items, err := client.searchMemories(context.Background(), "space1", memorySearchFilter{Query: "test"})
	if err != nil {
		t.Fatalf("searchMemories() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1（空 content 应被过滤）", len(items))
	}
	if items[0].Memory != "有效内容" {
		t.Errorf("Memory = %q, want %q", items[0].Memory, "有效内容")
	}
}

func TestAgentArtsClient_CreateMemorySession_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/core/spaces/space1/sessions" {
			t.Errorf("Path = %q, want /v1/core/spaces/space1/sessions", r.URL.Path)
		}

		var req sessionCreateRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.ActorID != "actor1" {
			t.Errorf("ActorID = %q, want %q", req.ActorID, "actor1")
		}
		if req.AssistantID != "asst1" {
			t.Errorf("AssistantID = %q, want %q", req.AssistantID, "asst1")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "session-uuid-123",
			"space_id":     "space1",
			"actor_id":     "actor1",
			"assistant_id": "asst1",
		})
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	info, err := client.createMemorySession(context.Background(), "space1", sessionCreateRequest{
		ActorID:     "actor1",
		AssistantID: "asst1",
	})
	if err != nil {
		t.Fatalf("createMemorySession() error = %v", err)
	}
	if info.ID != "session-uuid-123" {
		t.Errorf("ID = %q, want %q", info.ID, "session-uuid-123")
	}
}

func TestAgentArtsClient_CreateMemorySession_失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal"}`))
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	_, err := client.createMemorySession(context.Background(), "space1", sessionCreateRequest{})
	if err == nil {
		t.Fatal("createMemorySession() 应返回错误")
	}
}

func TestAgentArtsClient_AddMessages_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		expectedPath := "/v1/core/spaces/space1/sessions/sess1/messages"
		if r.URL.Path != expectedPath {
			t.Errorf("Path = %q, want %q", r.URL.Path, expectedPath)
		}

		var req addMessagesRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) != 2 {
			t.Errorf("len(Messages) = %d, want 2", len(req.Messages))
		}
		if req.Messages[0].Role != "user" {
			t.Errorf("Messages[0].Role = %q, want %q", req.Messages[0].Role, "user")
		}
		if len(req.Messages[0].Parts) != 1 || req.Messages[0].Parts[0].Type != "text" {
			t.Errorf("Messages[0].Parts 格式不正确")
		}
		if req.Messages[0].Parts[0].Text != "你好" {
			t.Errorf("Messages[0].Parts[0].Text = %q, want %q", req.Messages[0].Parts[0].Text, "你好")
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"messages": []any{}})
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	msgs := []textMessage{
		buildTextMessage("user", "你好", "actor1", "asst1"),
		buildTextMessage("assistant", "你好！", "actor1", "asst1"),
	}
	err := client.addMessages(context.Background(), "space1", "sess1", msgs)
	if err != nil {
		t.Fatalf("addMessages() error = %v", err)
	}
}

func TestAgentArtsClient_AddMessages_失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "session not found"}`))
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	err := client.addMessages(context.Background(), "space1", "invalid-session", nil)
	if err == nil {
		t.Fatal("addMessages() 应返回错误")
	}
}

func TestAgentArtsClient_认证失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid api key"}`))
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	_, err := client.searchMemories(context.Background(), "space1", memorySearchFilter{Query: "test"})
	if err == nil {
		t.Fatal("searchMemories() 应返回认证错误")
	}
}

func TestAgentArtsClient_请求超时(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	client := newTestAgentArtsClient(server)
	_, err := client.searchMemories(ctx, "space1", memorySearchFilter{Query: "test"})
	if err == nil {
		t.Fatal("searchMemories() 应返回超时错误")
	}
}

func TestBuildTextMessage_无actor(t *testing.T) {
	msg := buildTextMessage("user", "hello", "", "")
	if msg.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Role, "user")
	}
	if len(msg.Parts) != 1 || msg.Parts[0].Text != "hello" {
		t.Errorf("Parts 格式不正确")
	}
	if msg.ActorID != "" {
		t.Errorf("ActorID 应为空，got %q", msg.ActorID)
	}
	if msg.AssistantID != "" {
		t.Errorf("AssistantID 应为空，got %q", msg.AssistantID)
	}
}

func TestBuildTextMessage_带actor(t *testing.T) {
	msg := buildTextMessage("assistant", "hi", "actor1", "asst1")
	if msg.ActorID != "actor1" {
		t.Errorf("ActorID = %q, want %q", msg.ActorID, "actor1")
	}
	if msg.AssistantID != "asst1" {
		t.Errorf("AssistantID = %q, want %q", msg.AssistantID, "asst1")
	}
}
