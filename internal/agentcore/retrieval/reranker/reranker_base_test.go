package reranker

import (
	"testing"

	reranker "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// ──────────────────────────── NewRerankerBase 测试 ────────────────────────────

func TestNewRerankerBase(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:  "test-key",
		APIBase: "https://api.example.com",
	}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	if base == nil {
		t.Fatal("NewRerankerBase 返回 nil")
	}
	if base.MaxRetries() != 3 {
		t.Errorf("maxRetries: 期望 3, 实际 %d", base.MaxRetries())
	}
}

func TestNewRerankerBaseWithDefaults(t *testing.T) {
	config := reranker.RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBaseWithDefaults(config)

	if base == nil {
		t.Fatal("NewRerankerBaseWithDefaults 返回 nil")
	}
	if base.MaxRetries() != defaultMaxRetries {
		t.Errorf("maxRetries: 期望 %d, 实际 %d", defaultMaxRetries, base.MaxRetries())
	}
	if base.RetryWait() != defaultRetryWait {
		t.Errorf("retryWait: 期望 %v, 实际 %v", defaultRetryWait, base.RetryWait())
	}
}

// ──────────────────────────── RequestHeaders 测试 ────────────────────────────

func TestRerankerBase_RequestHeaders_有APIKey(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:  "test-key",
		APIBase: "https://api.example.com",
	}
	base := NewRerankerBase(config, 3, defaultRetryWait)
	headers := base.requestHeaders()

	if headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type: 期望 application/json, 实际 %q", headers["Content-Type"])
	}
	if headers["Authorization"] != "Bearer test-key" {
		t.Errorf("Authorization: 期望 Bearer test-key, 实际 %q", headers["Authorization"])
	}
}

func TestRerankerBase_RequestHeaders_无APIKey(t *testing.T) {
	config := reranker.RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)
	headers := base.requestHeaders()

	if _, ok := headers["Authorization"]; ok {
		t.Error("无 APIKey 时不应包含 Authorization 头")
	}
}

// ──────────────────────────── ParseResponse 测试 ────────────────────────────

func TestRerankerBase_ParseResponse_标准格式(t *testing.T) {
	config := reranker.RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	responseData := map[string]any{
		"results": []any{
			map[string]any{"index": float64(0), "relevance_score": 0.95},
			map[string]any{"index": float64(1), "relevance_score": 0.5},
		},
	}
	docIDs := []string{"doc-0", "doc-1"}

	result := base.parseResponse(responseData, docIDs)
	if result["doc-0"] != 0.95 {
		t.Errorf("doc-0 分数: 期望 0.95, 实际 %f", result["doc-0"])
	}
	if result["doc-1"] != 0.5 {
		t.Errorf("doc-1 分数: 期望 0.5, 实际 %f", result["doc-1"])
	}
}

func TestRerankerBase_ParseResponse_嵌套output格式(t *testing.T) {
	config := reranker.RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	responseData := map[string]any{
		"output": map[string]any{
			"results": []any{
				map[string]any{"index": float64(0), "relevance_score": 0.8},
			},
		},
	}
	docIDs := []string{"doc-0"}

	result := base.parseResponse(responseData, docIDs)
	if result["doc-0"] != 0.8 {
		t.Errorf("doc-0 分数: 期望 0.8, 实际 %f", result["doc-0"])
	}
}

func TestRerankerBase_ParseResponse_无匹配结果(t *testing.T) {
	config := reranker.RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	responseData := map[string]any{}
	docIDs := []string{"doc-0", "doc-1"}

	result := base.parseResponse(responseData, docIDs)
	if result["doc-0"] != 0.0 {
		t.Errorf("无匹配结果时分数应为 0, 实际 %f", result["doc-0"])
	}
	if result["doc-1"] != 0.0 {
		t.Errorf("无匹配结果时分数应为 0, 实际 %f", result["doc-1"])
	}
}

func TestRerankerBase_ParseResponse_越界索引(t *testing.T) {
	config := reranker.RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	responseData := map[string]any{
		"results": []any{
			map[string]any{"index": float64(99), "relevance_score": 0.9},
		},
	}
	docIDs := []string{"doc-0"}

	result := base.parseResponse(responseData, docIDs)
	// 越界索引应被忽略，doc-0 保持默认值 0
	if result["doc-0"] != 0.0 {
		t.Errorf("越界索引应被忽略, doc-0 分数应为 0, 实际 %f", result["doc-0"])
	}
}

// ──────────────────────────── ExtractDocIDs 测试 ────────────────────────────

func TestRerankerBase_ExtractDocIDs(t *testing.T) {
	config := reranker.RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	docs := []any{
		"plain-text",
		&reranker.Document{ID: "doc-123", Text: "内容"},
	}
	ids := base.extractDocIDs(docs)

	if ids[0] != "plain-text" {
		t.Errorf("字符串文档 ID: 期望 plain-text, 实际 %q", ids[0])
	}
	if ids[1] != "doc-123" {
		t.Errorf("Document ID: 期望 doc-123, 实际 %q", ids[1])
	}
}

// ──────────────────────────── ExtractTexts 测试 ────────────────────────────

func TestRerankerBase_ExtractTexts(t *testing.T) {
	config := reranker.RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	docs := []any{
		"plain-text",
		&reranker.Document{ID: "doc-123", Text: "文档内容"},
	}
	texts := base.extractTexts(docs)

	if texts[0] != "plain-text" {
		t.Errorf("字符串文本: 期望 plain-text, 实际 %q", texts[0])
	}
	if texts[1] != "文档内容" {
		t.Errorf("Document 文本: 期望 文档内容, 实际 %q", texts[1])
	}
}

// ──────────────────────────── ResolveTopN 测试 ────────────────────────────

func TestRerankerBase_ResolveTopN_设置值(t *testing.T) {
	config := reranker.RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	opt := &reranker.RerankOption{TopN: 5}
	topN := base.resolveTopN(opt, 10)
	if topN != 5 {
		t.Errorf("TopN: 期望 5, 实际 %d", topN)
	}
}

func TestRerankerBase_ResolveTopN_未设置(t *testing.T) {
	config := reranker.RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	topN := base.resolveTopN(nil, 10)
	if topN != 10 {
		t.Errorf("未设置 TopN 时应使用文档总数, 期望 10, 实际 %d", topN)
	}
}

func TestRerankerBase_ResolveTopN_为零值(t *testing.T) {
	config := reranker.RerankerConfig{APIBase: "https://api.example.com"}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	opt := &reranker.RerankOption{TopN: 0}
	topN := base.resolveTopN(opt, 10)
	if topN != 10 {
		t.Errorf("TopN 为 0 时应使用文档总数, 期望 10, 实际 %d", topN)
	}
}

// ──────────────────────────── RequestParams 测试 ────────────────────────────

func TestRerankerBase_RequestParams(t *testing.T) {
	config := reranker.RerankerConfig{
		APIBase:   "https://api.example.com",
		ModelName: "rerank-model",
		ExtraBody: map[string]any{"custom_field": "custom_value"},
	}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	opt := &reranker.RerankOption{ExtraParams: map[string]any{"extra_key": "extra_val"}}
	params := base.requestParams("查询文本", []string{"文档1", "文档2"}, 2, opt)

	if params["model"] != "rerank-model" {
		t.Errorf("model: 期望 rerank-model, 实际 %v", params["model"])
	}
	if params["return_documents"] != false {
		t.Errorf("return_documents: 期望 false, 实际 %v", params["return_documents"])
	}
	if params["custom_field"] != "custom_value" {
		t.Errorf("custom_field: 期望 custom_value, 实际 %v", params["custom_field"])
	}
	if params["extra_key"] != "extra_val" {
		t.Errorf("extra_key: 期望 extra_val, 实际 %v", params["extra_key"])
	}
}

// ──────────────────────────── AssembleParams 测试 ────────────────────────────

func TestRerankerBase_AssembleParams(t *testing.T) {
	config := reranker.RerankerConfig{
		APIBase:   "https://api.example.com",
		ModelName: "rerank-model",
	}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	docs := []any{"文档1", "文档2"}
	headers, params, docIDs := base.assembleParams("搜索查询", docs, nil)

	if headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type: 期望 application/json, 实际 %q", headers["Content-Type"])
	}
	if params["model"] != "rerank-model" {
		t.Errorf("model: 期望 rerank-model, 实际 %v", params["model"])
	}
	if len(docIDs) != 2 {
		t.Errorf("docIDs 长度: 期望 2, 实际 %d", len(docIDs))
	}
	if docIDs[0] != "文档1" {
		t.Errorf("docIDs[0]: 期望 文档1, 实际 %q", docIDs[0])
	}
	if docIDs[1] != "文档2" {
		t.Errorf("docIDs[1]: 期望 文档2, 实际 %q", docIDs[1])
	}
}

// ──────────────────────────── Config 访问器测试 ────────────────────────────

func TestRerankerBase_Config(t *testing.T) {
	config := reranker.RerankerConfig{
		APIBase:   "https://api.example.com",
		ModelName: "test-model",
	}
	base := NewRerankerBase(config, 3, defaultRetryWait)

	if base.Config().ModelName != "test-model" {
		t.Errorf("Config().ModelName: 期望 test-model, 实际 %q", base.Config().ModelName)
	}
	if base.Config().APIBase != "https://api.example.com" {
		t.Errorf("Config().APIBase: 期望 https://api.example.com, 实际 %q", base.Config().APIBase)
	}
}
