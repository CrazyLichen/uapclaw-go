package reranker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	reranker "github.com/uapclaw/uapclaw-go/internal/agentcore/store/reranker"
)

// ──────────────────────────── 构造函数测试 ────────────────────────────

func TestNewStandardReranker_配置校验(t *testing.T) {
	config := reranker.RerankerConfig{APIBase: ""}
	_, err := NewStandardReranker(config)
	if err == nil {
		t.Fatal("APIBase 缺失时应返回错误")
	}
}

func TestNewStandardReranker_APIBase去除rerank后缀(t *testing.T) {
	config := reranker.RerankerConfig{
		APIBase: "https://api.example.com/rerank",
	}
	r, err := NewStandardReranker(config)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if r.Config().APIBase != "https://api.example.com" {
		t.Errorf("APIBase: 期望 https://api.example.com, 实际 %q", r.Config().APIBase)
	}
}

func TestNewStandardReranker_APIBase无后缀(t *testing.T) {
	config := reranker.RerankerConfig{
		APIBase: "https://api.example.com",
	}
	r, err := NewStandardReranker(config)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if r.Config().APIBase != "https://api.example.com" {
		t.Errorf("APIBase: 期望 https://api.example.com, 实际 %q", r.Config().APIBase)
	}
}

// ──────────────────────────── 接口约束测试 ────────────────────────────

func TestStandardReranker_接口约束(t *testing.T) {
	var _ reranker.BaseReranker = (*StandardReranker)(nil)
}

// ──────────────────────────── Rerank 测试 ────────────────────────────

func TestStandardReranker_Rerank_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("请求方法: 期望 POST, 实际 %s", r.Method)
		}
		if r.URL.Path != "/rerank" {
			t.Errorf("请求路径: 期望 /rerank, 实际 %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{"index": float64(0), "relevance_score": 0.95},
				map[string]any{"index": float64(1), "relevance_score": 0.5},
			},
		})
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		ModelName: "rerank-model",
	}
	r, err := NewStandardReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	result, err := r.Rerank(context.Background(), "查询", []string{"文档1", "文档2"})
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}
	if result["文档1"] != 0.95 {
		t.Errorf("文档1 分数: 期望 0.95, 实际 %f", result["文档1"])
	}
	if result["文档2"] != 0.5 {
		t.Errorf("文档2 分数: 期望 0.5, 实际 %f", result["文档2"])
	}
}

func TestStandardReranker_RerankDocs_Document输入(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{"index": float64(0), "relevance_score": 0.88},
			},
		})
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		ModelName: "rerank-model",
	}
	r, _ := NewStandardReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	doc := reranker.NewDocument("文档内容")
	result, err := r.RerankDocs(context.Background(), "查询", []*reranker.Document{doc})
	if err != nil {
		t.Fatalf("RerankDocs 失败: %v", err)
	}
	if result[doc.ID] != 0.88 {
		t.Errorf("文档分数: 期望 0.88, 实际 %f", result[doc.ID])
	}
}

func TestStandardReranker_Rerank_Instruct选项(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{"index": float64(0), "relevance_score": 0.9},
			},
		})
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		ModelName: "rerank-model",
	}
	r, _ := NewStandardReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	// 禁用 instruct
	disabled := false
	_, err := r.Rerank(context.Background(), "测试查询", []string{"文档"}, reranker.RerankOption{InstructEnabled: &disabled})
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}

	query, _ := receivedBody["query"].(string)
	if query != "测试查询" {
		t.Errorf("禁用 instruct 时 query 应为原始值, 实际: %q", query)
	}
}

func TestStandardReranker_Rerank_ExtraBody合并(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{"index": float64(0), "relevance_score": 0.9},
			},
		})
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:    server.URL,
		ModelName:  "rerank-model",
		ExtraBody:  map[string]any{"custom_field": "custom_value"},
	}
	r, _ := NewStandardReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	_, err := r.Rerank(context.Background(), "查询", []string{"文档"})
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}

	if receivedBody["custom_field"] != "custom_value" {
		t.Errorf("custom_field: 期望 custom_value, 实际 %v", receivedBody["custom_field"])
	}
}

func TestStandardReranker_Rerank_API调用失败重试(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{"index": float64(0), "relevance_score": 0.9},
			},
		})
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		ModelName: "rerank-model",
	}
	r, _ := NewStandardReranker(config, WithMaxRetries(3), WithRetryWait(10*time.Millisecond))

	result, err := r.Rerank(context.Background(), "查询", []string{"文档"})
	if err != nil {
		t.Fatalf("重试后应成功, 实际错误: %v", err)
	}
	if result["文档"] != 0.9 {
		t.Errorf("文档分数: 期望 0.9, 实际 %f", result["文档"])
	}
}

func TestStandardReranker_RerankSync_同步调用(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{"index": float64(0), "relevance_score": 0.8},
			},
		})
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		ModelName: "rerank-model",
	}
	r, _ := NewStandardReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	result, err := r.RerankSync(context.Background(), "查询", []string{"文档"})
	if err != nil {
		t.Fatalf("RerankSync 失败: %v", err)
	}
	if result["文档"] != 0.8 {
		t.Errorf("文档分数: 期望 0.8, 实际 %f", result["文档"])
	}
}

func TestStandardReranker_Rerank_空结果(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"results": []any{},
		})
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		ModelName: "rerank-model",
	}
	r, _ := NewStandardReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	result, err := r.Rerank(context.Background(), "查询", []string{"文档1"})
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}
	// 空结果时文档分数为默认 0
	if result["文档1"] != 0.0 {
		t.Errorf("空结果时分数应为 0, 实际 %f", result["文档1"])
	}
}

func TestStandardReranker_ExtraHeaders(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{"index": float64(0), "relevance_score": 0.9},
			},
		})
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		APIKey:    "my-api-key",
		ModelName: "rerank-model",
	}
	r, _ := NewStandardReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	_, err := r.Rerank(context.Background(), "查询", []string{"文档"})
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}

	if receivedAuth != "Bearer my-api-key" {
		t.Errorf("Authorization: 期望 Bearer my-api-key, 实际 %q", receivedAuth)
	}
}

func TestNewStandardReranker_自定义HTTPClient(t *testing.T) {
	customClient := &http.Client{Timeout: 30 * time.Second}
	config := reranker.RerankerConfig{
		APIBase: "https://api.example.com",
	}
	r, err := NewStandardReranker(config, WithHTTPClient(customClient))
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if r.httpClient != customClient {
		t.Error("应使用自定义 HTTP 客户端")
	}
}
