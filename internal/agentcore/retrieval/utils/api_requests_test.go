package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ──────────────────────────── 成功响应 ────────────────────────────

func TestRequestWithRetry_成功响应(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()

	client := server.Client()
	result, err := RequestWithRetry(context.Background(), client, server.URL, map[string]any{"query": "test"}, map[string]string{"Content-Type": "application/json"}, RetryConfig{MaxRetries: 3})
	if err != nil {
		t.Fatalf("期望成功, 实际错误: %v", err)
	}
	if result == nil {
		t.Fatal("期望返回结果, 实际 nil")
	}
}

// ──────────────────────────── 重试测试 ────────────────────────────

func TestRequestWithRetry_重试429成功(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()

	client := server.Client()
	result, err := RequestWithRetry(context.Background(), client, server.URL, map[string]any{}, map[string]string{}, RetryConfig{MaxRetries: 3, RetryWait: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("期望重试后成功, 实际错误: %v", err)
	}
	if callCount != 2 {
		t.Errorf("期望调用 2 次, 实际 %d 次", callCount)
	}
	_ = result
}

func TestRequestWithRetry_重试500成功(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()

	client := server.Client()
	_, err := RequestWithRetry(context.Background(), client, server.URL, map[string]any{}, map[string]string{}, RetryConfig{MaxRetries: 3, RetryWait: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("期望重试后成功, 实际错误: %v", err)
	}
	if callCount != 2 {
		t.Errorf("期望调用 2 次, 实际 %d 次", callCount)
	}
}

func TestRequestWithRetry_重试503成功(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()

	client := server.Client()
	_, err := RequestWithRetry(context.Background(), client, server.URL, map[string]any{}, map[string]string{}, RetryConfig{MaxRetries: 3, RetryWait: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("期望重试后成功, 实际错误: %v", err)
	}
}

// ──────────────────────────── 400 不重试 ────────────────────────────

func TestRequestWithRetry_400不重试(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "bad request"})
	}))
	defer server.Close()

	client := server.Client()
	_, err := RequestWithRetry(context.Background(), client, server.URL, map[string]any{}, map[string]string{}, RetryConfig{MaxRetries: 3, RetryWait: 10 * time.Millisecond})
	if err == nil {
		t.Fatal("400 响应应返回错误")
	}
	// 400 不重试，应只调用 1 次
	if callCount != 1 {
		t.Errorf("期望调用 1 次, 实际 %d 次", callCount)
	}
}

func TestRequestWithRetry_400审查内容检测(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Content violates safety policy"}})
	}))
	defer server.Close()

	client := server.Client()
	_, err := RequestWithRetry(context.Background(), client, server.URL, map[string]any{}, map[string]string{}, RetryConfig{MaxRetries: 3, RetryWait: 10 * time.Millisecond})
	if err == nil {
		t.Fatal("400 响应应返回错误")
	}
	// 如果能到这里说明审查检测逻辑已执行（日志中应有 Warning，此处无法直接验证日志）
}

// ──────────────────────────── 最大重试耗尽 ────────────────────────────

func TestRequestWithRetry_最大重试耗尽(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := server.Client()
	_, err := RequestWithRetry(context.Background(), client, server.URL, map[string]any{}, map[string]string{}, RetryConfig{MaxRetries: 2, RetryWait: 10 * time.Millisecond})
	if err == nil {
		t.Fatal("最大重试耗尽应返回错误")
	}
	// 有 HTTP 响应 → RequestCallFailed
	if !strings.Contains(err.Error(), "Reranker") && !strings.Contains(err.Error(), "155600") {
		t.Errorf("错误应包含 Reranker 相关信息, 实际: %v", err)
	}
}

// ──────────────────────────── 网络错误 ────────────────────────────

func TestRequestWithRetry_网络错误(t *testing.T) {
	// 使用一个不存在的端口模拟网络错误
	client := &http.Client{Timeout: 1 * time.Second}
	_, err := RequestWithRetry(context.Background(), client, "http://127.0.0.1:1/nonexistent", map[string]any{}, map[string]string{}, RetryConfig{MaxRetries: 2, RetryWait: 10 * time.Millisecond})
	if err == nil {
		t.Fatal("网络错误应返回错误")
	}
	// 无 HTTP 响应 → UnreachableCallFailed
	if !strings.Contains(err.Error(), "Reranker") && !strings.Contains(err.Error(), "155601") {
		t.Errorf("错误应包含 UnreachableCallFailed 相关信息, 实际: %v", err)
	}
}

// ──────────────────────────── 同步版本 ────────────────────────────

func TestRequestWithRetrySync_同步调用(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()

	client := server.Client()
	result, err := RequestWithRetrySync(context.Background(), client, server.URL, map[string]any{}, map[string]string{}, RetryConfig{MaxRetries: 3})
	if err != nil {
		t.Fatalf("期望成功, 实际错误: %v", err)
	}
	if result == nil {
		t.Fatal("期望返回结果, 实际 nil")
	}
}

// ──────────────────────────── 取消上下文 ────────────────────────────

func TestRequestWithRetry_取消上下文(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError) // 始终 500 触发重试
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	client := server.Client()
	_, err := RequestWithRetry(ctx, client, server.URL, map[string]any{}, map[string]string{}, RetryConfig{MaxRetries: 10, RetryWait: 100 * time.Millisecond})
	if err == nil {
		t.Fatal("上下文取消应返回错误")
	}
}

// ──────────────────────────── 默认配置 ────────────────────────────

func TestRetryConfig_默认值(t *testing.T) {
	cfg := RetryConfig{}
	if cfg.MaxRetries != 0 {
		t.Errorf("默认 MaxRetries 应为 0（运行时使用 defaultMaxRetries）, 实际 %d", cfg.MaxRetries)
	}
	if cfg.RetryWait != 0 {
		t.Errorf("默认 RetryWait 应为 0（运行时使用 defaultRetryWait）, 实际 %v", cfg.RetryWait)
	}
	if cfg.Task != "" {
		t.Errorf("默认 Task 应为空（运行时使用 TaskReranker）, 实际 %q", cfg.Task)
	}
}

func TestTaskName_值(t *testing.T) {
	if TaskReranker != "Reranker" {
		t.Errorf("TaskReranker: 期望 Reranker, 实际 %q", TaskReranker)
	}
	if TaskEmbedding != "Embedding" {
		t.Errorf("TaskEmbedding: 期望 Embedding, 实际 %q", TaskEmbedding)
	}
}

// ──────────────────────────── 请求体和头验证 ────────────────────────────

func TestRequestWithRetry_请求体和头(t *testing.T) {
	var receivedBody map[string]any
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	client := server.Client()
	reqBody := map[string]any{"model": "test-model", "query": "hello"}
	reqHeaders := map[string]string{"Content-Type": "application/json", "Authorization": "Bearer test-key"}

	_, err := RequestWithRetry(context.Background(), client, server.URL, reqBody, reqHeaders, RetryConfig{MaxRetries: 1})
	if err != nil {
		t.Fatalf("期望成功, 实际错误: %v", err)
	}

	if receivedContentType != "application/json" {
		t.Errorf("Content-Type: 期望 application/json, 实际 %q", receivedContentType)
	}
	if receivedBody["model"] != "test-model" {
		t.Errorf("model: 期望 test-model, 实际 %v", receivedBody["model"])
	}
	if receivedBody["query"] != "hello" {
		t.Errorf("query: 期望 hello, 实际 %v", receivedBody["query"])
	}
}
