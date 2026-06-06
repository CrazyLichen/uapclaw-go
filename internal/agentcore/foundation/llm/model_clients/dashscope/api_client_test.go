package dashscope

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ──────────────────────────── ResolveDashScopeBaseURL 测试 ────────────────────────────

func TestResolveDashScopeBaseURL_CompatibleMode(t *testing.T) {
	// OpenAI 兼容模式 URL 应去掉 /compatible-mode/ 部分
	result := ResolveDashScopeBaseURL("https://dashscope.aliyuncs.com/compatible-mode/v1")
	if result != "https://dashscope.aliyuncs.com" {
		t.Errorf("result = %q, want %q", result, "https://dashscope.aliyuncs.com")
	}
}

func TestResolveDashScopeBaseURL_CompatibleMode_TrailingPath(t *testing.T) {
	// 兼容模式 URL 后有额外路径
	result := ResolveDashScopeBaseURL("https://dashscope.aliyuncs.com/compatible-mode/v1/")
	if result != "https://dashscope.aliyuncs.com" {
		t.Errorf("result = %q, want %q", result, "https://dashscope.aliyuncs.com")
	}
}

func TestResolveDashScopeBaseURL_NativeMode(t *testing.T) {
	// 原生模式 URL 直接使用
	result := ResolveDashScopeBaseURL("https://dashscope.aliyuncs.com")
	if result != "https://dashscope.aliyuncs.com" {
		t.Errorf("result = %q, want %q", result, "https://dashscope.aliyuncs.com")
	}
}

func TestResolveDashScopeBaseURL_CustomProxy(t *testing.T) {
	// 自定义代理 URL 直接使用
	result := ResolveDashScopeBaseURL("https://custom-proxy.example.com")
	if result != "https://custom-proxy.example.com" {
		t.Errorf("result = %q, want %q", result, "https://custom-proxy.example.com")
	}
}

func TestResolveDashScopeBaseURL_TrailingSlash(t *testing.T) {
	// 末尾有斜杠应去掉
	result := ResolveDashScopeBaseURL("https://dashscope.aliyuncs.com/")
	if result != "https://dashscope.aliyuncs.com" {
		t.Errorf("result = %q, want %q", result, "https://dashscope.aliyuncs.com")
	}
}

// ──────────────────────────── CallDashScopeAPI 测试 ────────────────────────────

func TestCallDashScopeAPI_Success(t *testing.T) {
	// 成功 API 调用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求方法
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		// 验证认证头
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("Authorization = %q, want %q", r.Header.Get("Authorization"), "Bearer test-api-key")
		}
		// 验证 Content-Type
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}

		// 返回成功响应
		resp := DashScopeResponse{
			StatusCode: 200,
			RequestID:  "req-123",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	reqBody := map[string]any{
		"model": "wanx-v1",
		"input": map[string]any{},
	}

	resp, err := CallDashScopeAPI(
		context.Background(),
		server.URL,
		"test-api-key",
		multiModalPath,
		reqBody,
		nil,
		false,
		"",
	)
	if err != nil {
		t.Fatalf("CallDashScopeAPI 返回错误: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if resp.RequestID != "req-123" {
		t.Errorf("RequestID = %q, want %q", resp.RequestID, "req-123")
	}
}

func TestCallDashScopeAPI_ErrorResponse(t *testing.T) {
	// API 返回错误
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DashScopeResponse{
			StatusCode: 500,
			Code:       "InternalError",
			Message:    "服务内部错误",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	reqBody := map[string]any{"model": "test"}
	resp, err := CallDashScopeAPI(
		context.Background(),
		server.URL,
		"test-api-key",
		multiModalPath,
		reqBody,
		nil,
		false,
		"",
	)
	if err == nil {
		t.Error("API 错误时应返回错误")
	}
	if resp != nil {
		t.Error("错误时响应应为 nil")
	}
}

func TestCallDashScopeAPI_CompatibleModeBaseURL(t *testing.T) {
	// 验证兼容模式 URL 自动推导
	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-001"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// 构建一个兼容模式的 URL
	compatibleURL := server.URL + "/compatible-mode/v1"

	reqBody := map[string]any{"model": "test"}
	_, err := CallDashScopeAPI(
		context.Background(),
		compatibleURL,
		"test-api-key",
		multiModalPath,
		reqBody,
		nil,
		false,
		"",
	)
	if err != nil {
		t.Fatalf("CallDashScopeAPI 返回错误: %v", err)
	}

	// 验证请求路径是原生 API 路径（不含 /compatible-mode/）
	expectedPath := "/api/v1/services/aigc/multimodal-generation/generation"
	if receivedPath != expectedPath {
		t.Errorf("请求路径 = %q, want %q", receivedPath, expectedPath)
	}
}

func TestCallDashScopeAPI_ContextCancelled(t *testing.T) {
	// context 取消
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 不应到达
		t.Error("请求不应到达服务器")
	}))
	defer server.Close()

	reqBody := map[string]any{"model": "test"}
	_, err := CallDashScopeAPI(
		ctx,
		server.URL,
		"test-api-key",
		multiModalPath,
		reqBody,
		nil,
		false,
		"",
	)
	if err == nil {
		t.Error("context 取消时应返回错误")
	}
}
