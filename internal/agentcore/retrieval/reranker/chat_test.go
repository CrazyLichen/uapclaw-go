package reranker

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	reranker "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// ──────────────────────────── 构造函数测试 ────────────────────────────

func TestNewChatReranker_YesNoIDs缺失报错(t *testing.T) {
	config := reranker.RerankerConfig{
		APIBase: "https://api.example.com",
	}
	_, err := NewChatReranker(config)
	if err == nil {
		t.Fatal("YesNoIDs 缺失时应返回错误")
	}
}

func TestNewChatReranker_正常创建(t *testing.T) {
	config := reranker.RerankerConfig{
		APIBase:   "https://api.example.com",
		YesNoIDs:  [2]int{1234, 5678},
		ModelName: "chat-model",
	}
	c, err := NewChatReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if c.yesNoIDs != [2]int{1234, 5678} {
		t.Errorf("yesNoIDs: 期望 [1234 5678], 实际 %v", c.yesNoIDs)
	}
	if c.endPoint != chatEndPoint {
		t.Errorf("endPoint: 期望 %q, 实际 %q", chatEndPoint, c.endPoint)
	}
}

// ──────────────────────────── 接口约束测试 ────────────────────────────

func TestChatReranker_接口约束(t *testing.T) {
	var _ reranker.BaseReranker = (*ChatReranker)(nil)
}

// ──────────────────────────── Rerank 测试 ────────────────────────────

// makeChatCompletionResponse 构造 chat completion 响应
func makeChatCompletionResponse(topLogprobs []map[string]any) map[string]any {
	return map[string]any{
		"choices": []any{
			map[string]any{
				"logprobs": map[string]any{
					"content": []any{
						map[string]any{
							"top_logprobs": topLogprobs,
						},
					},
				},
			},
		},
	}
}

func TestChatReranker_Rerank_正常解析(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("请求路径: 期望 /chat/completions, 实际 %s", r.URL.Path)
		}

		resp := makeChatCompletionResponse([]map[string]any{
			{"token": "Yes", "logprob": -0.1},
			{"token": "No", "logprob": -2.3},
		})
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		YesNoIDs:  [2]int{1234, 5678},
		ModelName: "chat-model",
	}
	c, _ := NewChatReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	result, err := c.Rerank(context.Background(), "查询", []string{"文档"})
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}

	// 计算: confidence = exp(-0.1) ≈ 0.905, noScore = exp(-2.3) ≈ 0.100
	// score = 0.905 / (0.905 + 0.100) ≈ 0.900
	yesProb := math.Exp(-0.1)
	noProb := math.Exp(-2.3)
	expected := yesProb / (yesProb + noProb)

	if len(result) != 1 {
		t.Fatalf("结果数量: 期望 1, 实际 %d", len(result))
	}
	for _, score := range result {
		if math.Abs(score-expected) > 0.01 {
			t.Errorf("分数: 期望 %.4f, 实际 %.4f", expected, score)
		}
	}
}

func TestChatReranker_Rerank_yes概率计算(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// yes 的 logprob 很高（接近 0），no 的 logprob 很低
		resp := makeChatCompletionResponse([]map[string]any{
			{"token": "yes", "logprob": -0.01}, // exp(-0.01) ≈ 0.990
			{"token": "no", "logprob": -5.0},   // exp(-5.0) ≈ 0.0067
		})
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		YesNoIDs:  [2]int{1234, 5678},
		ModelName: "chat-model",
	}
	c, _ := NewChatReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	result, err := c.Rerank(context.Background(), "查询", []string{"文档"})
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}

	for _, score := range result {
		// yes 概率很高，score 应接近 1.0
		if score < 0.99 {
			t.Errorf("期望高分（接近 1.0）, 实际 %.4f", score)
		}
	}
}

func TestChatReranker_Rerank_多文档报错(t *testing.T) {
	config := reranker.RerankerConfig{
		APIBase:   "https://api.example.com",
		YesNoIDs:  [2]int{1234, 5678},
		ModelName: "chat-model",
	}
	c, _ := NewChatReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	_, err := c.Rerank(context.Background(), "查询", []string{"文档1", "文档2"})
	if err == nil {
		t.Fatal("多文档时应返回错误")
	}
}

func TestChatReranker_Rerank_logprobs不支持(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 返回无 logprobs 的响应
		resp := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "yes",
					},
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		YesNoIDs:  [2]int{1234, 5678},
		ModelName: "chat-model",
	}
	c, _ := NewChatReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	result, err := c.Rerank(context.Background(), "查询", []string{"文档"})
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}
	// 无 logprobs 时返回 0.0
	for _, score := range result {
		if score != 0.0 {
			t.Errorf("无 logprobs 时分数应为 0, 实际 %f", score)
		}
	}
}

func TestChatReranker_Rerank_总概率为零(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 返回不匹配 yes/no 的 token
		resp := makeChatCompletionResponse([]map[string]any{
			{"token": "maybe", "logprob": -0.5},
			{"token": "perhaps", "logprob": -1.0},
		})
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		YesNoIDs:  [2]int{1234, 5678},
		ModelName: "chat-model",
	}
	c, _ := NewChatReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	result, err := c.Rerank(context.Background(), "查询", []string{"文档"})
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}
	for _, score := range result {
		if score != 0.0 {
			t.Errorf("总概率为零时分数应为 0, 实际 %f", score)
		}
	}
}

func TestChatReranker_RerankSync_同步调用(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := makeChatCompletionResponse([]map[string]any{
			{"token": "yes", "logprob": -0.2},
			{"token": "no", "logprob": -1.5},
		})
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		YesNoIDs:  [2]int{1234, 5678},
		ModelName: "chat-model",
	}
	c, _ := NewChatReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	result, err := c.RerankSync(context.Background(), "查询", []string{"文档"})
	if err != nil {
		t.Fatalf("RerankSync 失败: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("结果数量: 期望 1, 实际 %d", len(result))
	}
}

// ──────────────────────────── TestCompatibility 测试 ────────────────────────────

func TestChatReranker_TestCompatibility_成功(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := makeChatCompletionResponse([]map[string]any{
			{"token": "yes", "logprob": -0.1},
		})
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		YesNoIDs:  [2]int{1234, 5678},
		ModelName: "chat-model",
	}
	c, _ := NewChatReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	ok, err := c.TestCompatibility(context.Background())
	if err != nil {
		t.Fatalf("TestCompatibility 失败: %v", err)
	}
	if !ok {
		t.Error("服务支持时应返回 true")
	}
}

func TestChatReranker_TestCompatibility_失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		YesNoIDs:  [2]int{1234, 5678},
		ModelName: "chat-model",
	}
	c, _ := NewChatReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	ok, _ := c.TestCompatibility(context.Background())
	if ok {
		t.Error("服务不可用时应返回 false")
	}
}

// ──────────────────────────── Document 输入测试 ────────────────────────────

func TestChatReranker_RerankDocs_Document输入(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := makeChatCompletionResponse([]map[string]any{
			{"token": "yes", "logprob": -0.1},
		})
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := reranker.RerankerConfig{
		APIBase:   server.URL,
		YesNoIDs:  [2]int{1234, 5678},
		ModelName: "chat-model",
	}
	c, _ := NewChatReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	doc := reranker.NewDocument("文档内容")
	result, err := c.RerankDocs(context.Background(), "查询", []*reranker.Document{doc})
	if err != nil {
		t.Fatalf("RerankDocs 失败: %v", err)
	}
	if _, ok := result[doc.ID]; !ok {
		t.Error("结果应包含文档 ID")
	}
}

func TestChatReranker_RerankDocs_多文档报错(t *testing.T) {
	config := reranker.RerankerConfig{
		APIBase:   "https://api.example.com",
		YesNoIDs:  [2]int{1234, 5678},
		ModelName: "chat-model",
	}
	c, _ := NewChatReranker(config, WithMaxRetries(1), WithRetryWait(10*time.Millisecond))

	doc1 := reranker.NewDocument("文档1")
	doc2 := reranker.NewDocument("文档2")
	_, err := c.RerankDocs(context.Background(), "查询", []*reranker.Document{doc1, doc2})
	if err == nil {
		t.Fatal("多文档时应返回错误")
	}
}

// ──────────────────────────── 辅助函数测试 ────────────────────────────

func TestMaxFloat(t *testing.T) {
	if maxFloat([]float64{}) != 0 {
		t.Error("空切片应返回 0")
	}
	if maxFloat([]float64{3.0}) != 3.0 {
		t.Error("单元素应返回该元素")
	}
	if maxFloat([]float64{1.0, 3.0, 2.0}) != 3.0 {
		t.Error("应返回最大值")
	}
}

func TestFirstDocID(t *testing.T) {
	if firstDocID(nil) != "" {
		t.Error("nil 应返回空字符串")
	}
	if firstDocID([]string{}) != "" {
		t.Error("空切片应返回空字符串")
	}
	if firstDocID([]string{"doc-1"}) != "doc-1" {
		t.Error("应返回第一个元素")
	}
}
