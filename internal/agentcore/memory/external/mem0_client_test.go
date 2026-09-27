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

// newTestMem0Client 创建使用 mock server 的 mem0HTTPClient
func newTestMem0Client(server *httptest.Server) *mem0HTTPClient {
	return &mem0HTTPClient{
		apiKey:     "test-api-key",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func TestNewMem0HTTPClient_默认URL(t *testing.T) {
	client := newMem0HTTPClient("key", "")
	if client.baseURL != mem0DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", client.baseURL, mem0DefaultBaseURL)
	}
}

func TestNewMem0HTTPClient_自定义URL(t *testing.T) {
	client := newMem0HTTPClient("key", "https://custom.mem0.ai")
	if client.baseURL != "https://custom.mem0.ai" {
		t.Errorf("baseURL = %q, want custom URL", client.baseURL)
	}
}

func TestUnwrapResults_dict格式(t *testing.T) {
	data := []byte(`{"results": [{"memory": "偏好中文", "score": 0.9}]}`)
	items, err := unwrapResults(data)
	if err != nil {
		t.Fatalf("unwrapResults() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Memory != "偏好中文" {
		t.Errorf("Memory = %q, want %q", items[0].Memory, "偏好中文")
	}
	if items[0].Score != 0.9 {
		t.Errorf("Score = %f, want 0.9", items[0].Score)
	}
}

func TestUnwrapResults_list格式(t *testing.T) {
	data := []byte(`[{"memory": "偏好英文", "score": 0.8}]`)
	items, err := unwrapResults(data)
	if err != nil {
		t.Fatalf("unwrapResults() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Memory != "偏好英文" {
		t.Errorf("Memory = %q, want %q", items[0].Memory, "偏好英文")
	}
}

func TestUnwrapResults_空结果(t *testing.T) {
	data := []byte(`[]`)
	items, err := unwrapResults(data)
	if err != nil {
		t.Fatalf("unwrapResults() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestUnwrapResults_dict空结果(t *testing.T) {
	data := []byte(`{"results": []}`)
	items, err := unwrapResults(data)
	if err != nil {
		t.Fatalf("unwrapResults() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestMem0HTTPClient_Search(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/memories/search/" {
			t.Errorf("Path = %q, want /v1/memories/search/", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Token test-api-key" {
			t.Errorf("Authorization = %q, want 'Token test-api-key'", r.Header.Get("Authorization"))
		}

		var req mem0SearchRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Query != "测试查询" {
			t.Errorf("Query = %q, want %q", req.Query, "测试查询")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"memory": "用户偏好Go语言", "score": 0.95},
				{"memory": "用户使用VSCode", "score": 0.8},
			},
		})
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	items, err := client.search(context.Background(), "测试查询", map[string]any{"user_id": "u1"}, true, 10)
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Memory != "用户偏好Go语言" {
		t.Errorf("Memory = %q, want %q", items[0].Memory, "用户偏好Go语言")
	}
}

func TestMem0HTTPClient_Search_空结果(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	items, err := client.search(context.Background(), "不存在", nil, false, 5)
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestMem0HTTPClient_GetAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/v1/memories/" {
			t.Errorf("Path = %q, want /v1/memories/", r.URL.Path)
		}
		uid := r.URL.Query().Get("user_id")
		if uid != "u1" {
			t.Errorf("user_id = %q, want %q", uid, "u1")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"memory": "偏好暗色主题"},
			{"memory": "使用zsh"},
		})
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	items, err := client.getAll(context.Background(), map[string]any{"user_id": "u1"})
	if err != nil {
		t.Fatalf("getAll() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
}

func TestMem0HTTPClient_GetAll_带agentID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := r.URL.Query().Get("user_id")
		aid := r.URL.Query().Get("agent_id")
		if uid != "u1" {
			t.Errorf("user_id = %q, want %q", uid, "u1")
		}
		if aid != "a1" {
			t.Errorf("agent_id = %q, want %q", aid, "a1")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	items, err := client.getAll(context.Background(), map[string]any{"user_id": "u1", "agent_id": "a1"})
	if err != nil {
		t.Fatalf("getAll() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestMem0HTTPClient_Add(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/memories/" {
			t.Errorf("Path = %q, want /v1/memories/", r.URL.Path)
		}

		var req mem0AddRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) != 2 {
			t.Errorf("len(Messages) = %d, want 2", len(req.Messages))
		}
		if req.Messages[0].Role != "user" {
			t.Errorf("Messages[0].Role = %q, want %q", req.Messages[0].Role, "user")
		}
		if req.Infer != nil {
			t.Errorf("Infer = %v, want nil (未设置)", *req.Infer)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "mem-123"})
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	msgs := []mem0Message{
		{Role: "user", Content: "你好"},
		{Role: "assistant", Content: "你好！"},
	}
	err := client.add(context.Background(), msgs, map[string]any{"user_id": "u1", "agent_id": "a1"}, nil)
	if err != nil {
		t.Fatalf("add() error = %v", err)
	}
}

func TestMem0HTTPClient_Add_Conclude(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mem0AddRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Infer == nil || *req.Infer != false {
			t.Errorf("Infer = %v, want false", req.Infer)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			t.Errorf("Messages = %v, want single user message", req.Messages)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "mem-456"})
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	inferFalse := false
	msgs := []mem0Message{{Role: "user", Content: "我喜欢Go语言"}}
	err := client.add(context.Background(), msgs, map[string]any{"user_id": "u1", "agent_id": "a1"}, &inferFalse)
	if err != nil {
		t.Fatalf("add() error = %v", err)
	}
}

func TestMem0HTTPClient_认证失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"detail": "Invalid API key"})
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	_, err := client.search(context.Background(), "test", nil, false, 5)
	if err == nil {
		t.Fatal("search() 应返回认证错误")
	}
}

func TestMem0HTTPClient_服务器错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	_, err := client.search(context.Background(), "test", nil, false, 5)
	if err == nil {
		t.Fatal("search() 应返回服务器错误")
	}
}

func TestMem0HTTPClient_请求超时(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
	}))
	defer server.Close()

	// 用短超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	client := newTestMem0Client(server)
	_, err := client.search(ctx, "test", nil, false, 5)
	if err == nil {
		t.Fatal("search() 应返回超时错误")
	}
}
