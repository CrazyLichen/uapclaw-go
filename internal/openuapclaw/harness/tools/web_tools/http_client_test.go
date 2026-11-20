package web_tools

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestHTTPRequest_BasicGet 基础 GET 请求
func TestHTTPRequest_BasicGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("期望 GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello"))
	}))
	defer server.Close()

	resp, err := httpRequest("GET", server.URL, withTimeout(5))
	if err != nil {
		t.Fatalf("httpRequest 失败: %v", err)
	}
	if resp.statusCode != 200 {
		t.Errorf("statusCode = %d, want 200", resp.statusCode)
	}
	if resp.text != "hello" {
		t.Errorf("text = %q, want %q", resp.text, "hello")
	}
}

// TestHTTPRequest_Post 请求
func TestHTTPRequest_Post(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("期望 POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	resp, err := httpRequest("POST", server.URL,
		withHeaders(map[string]string{"Content-Type": "application/json"}),
		withBody(`{"query":"test"}`),
		withTimeout(5),
	)
	if err != nil {
		t.Fatalf("httpRequest 失败: %v", err)
	}
	if resp.statusCode != 200 {
		t.Errorf("statusCode = %d, want 200", resp.statusCode)
	}
}

// TestRaiseForStatusWithBody_HTTP错误包含响应体
func TestRaiseForStatusWithBody(t *testing.T) {
	// 2xx 不报错
	resp := &httpResponse{statusCode: 200, status: "200 OK", text: "ok"}
	if err := raiseForStatusWithBody(resp); err != nil {
		t.Errorf("2xx 不应报错, got %v", err)
	}

	// 4xx 报错含 body
	resp = &httpResponse{statusCode: 400, status: "400 Bad Request", text: "invalid query"}
	err := raiseForStatusWithBody(resp)
	if err == nil {
		t.Error("4xx 应报错")
	}
}

// TestShouldBypassFreeSearchProxy 代理绕过逻辑
func TestShouldBypassFreeSearchProxy(t *testing.T) {
	// 无代理设置时绕过
	os.Unsetenv(freeSearchProxyURLEnv)
	if !shouldBypassFreeSearchProxy("http://example.com") {
		t.Error("无代理时应绕过")
	}
}
