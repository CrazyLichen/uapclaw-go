package external

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestVikingClient 创建使用 mock server 的 vikingClient
func newTestVikingClient(server *httptest.Server) *vikingClient {
	return newVikingClient(server.URL, "test-api-key", "test-account", "test-user", "test-agent")
}

func TestVikingClient_Health_成功(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	if !client.health(context.Background()) {
		t.Error("health() = false, want true")
	}
}

func TestVikingClient_Health_失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	if client.health(context.Background()) {
		t.Error("health() = true, want false (500)")
	}
}

func TestVikingClient_Health_连接失败(t *testing.T) {
	// 使用一个不存在的地址
	client := newVikingClient("http://127.0.0.1:1", "", "a", "u", "g")
	if client.health(context.Background()) {
		t.Error("health() = true, want false (连接失败)")
	}
}

func TestVikingClient_Post_成功(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/test" {
			t.Errorf("Path = %q, want /api/v1/test", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["key"] != "value" {
			t.Errorf("body[key] = %v, want value", body["key"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"result": "ok"})
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	result, err := client.post(context.Background(), "/api/v1/test", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("post() error = %v", err)
	}
	if result["result"] != "ok" {
		t.Errorf("result = %v, want ok", result["result"])
	}
}

func TestVikingClient_Post_错误状态码(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	_, err := client.post(context.Background(), "/api/v1/test", map[string]any{})
	if err == nil {
		t.Fatal("post() 应返回错误（500）")
	}
}

func TestVikingClient_Get_带参数(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Method = %q, want GET", r.Method)
		}
		if r.URL.Query().Get("uri") != "viking://test" {
			t.Errorf("uri param = %q, want 'viking://test'", r.URL.Query().Get("uri"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"result": "data"})
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	result, err := client.get(context.Background(), "/api/v1/content/read", map[string]string{"uri": "viking://test"})
	if err != nil {
		t.Fatalf("get() error = %v", err)
	}
	if result["result"] != "data" {
		t.Errorf("result = %v, want data", result["result"])
	}
}

func TestVikingClient_Get_错误状态码(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not Found"))
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	_, err := client.get(context.Background(), "/api/v1/test", nil)
	if err == nil {
		t.Fatal("get() 应返回错误（404）")
	}
}

func TestVikingClient_Close(t *testing.T) {
	client := newTestVikingClient(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})))
	// 不应 panic
	client.close()
}

func TestVikingClient_请求Header(t *testing.T) {
	var captured *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	client.health(context.Background())

	if captured == nil {
		t.Fatal("未捕获请求")
	}
	if captured.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", captured.Header.Get("Content-Type"))
	}
	if captured.Header.Get("X-OpenViking-Account") != "test-account" {
		t.Errorf("X-OpenViking-Account = %q, want test-account", captured.Header.Get("X-OpenViking-Account"))
	}
	if captured.Header.Get("X-OpenViking-User") != "test-user" {
		t.Errorf("X-OpenViking-User = %q, want test-user", captured.Header.Get("X-OpenViking-User"))
	}
	if captured.Header.Get("X-OpenViking-Agent") != "test-agent" {
		t.Errorf("X-OpenViking-Agent = %q, want test-agent", captured.Header.Get("X-OpenViking-Agent"))
	}
	if captured.Header.Get("X-API-Key") != "test-api-key" {
		t.Errorf("X-API-Key = %q, want test-api-key", captured.Header.Get("X-API-Key"))
	}
}

func TestVikingClient_请求Header_无APIKey(t *testing.T) {
	var captured *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newVikingClient(server.URL, "", "a", "u", "g")
	client.health(context.Background())

	if captured == nil {
		t.Fatal("未捕获请求")
	}
	if captured.Header.Get("X-API-Key") != "" {
		t.Errorf("X-API-Key = %q, want empty (无 apiKey)", captured.Header.Get("X-API-Key"))
	}
}
