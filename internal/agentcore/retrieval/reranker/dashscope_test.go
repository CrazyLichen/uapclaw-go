package reranker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	reranker "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// dashScopeTestResponse DashScope API 测试响应
const dashScopeTestResponse = `{
	"output": {
		"results": [
			{"index": 0, "relevance_score": 0.95},
			{"index": 1, "relevance_score": 0.75},
			{"index": 2, "relevance_score": 0.50}
		]
	}
}`

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestDashScopeServer 创建模拟 DashScope API 的测试服务器。
func newTestDashScopeServer(responseBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求方法和路径
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != dashScopeEndPoint {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// 验证 Content-Type
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseBody))
	}))
}

// newTestDashScopeReranker 创建测试用 DashScopeReranker 实例。
func newTestDashScopeReranker(serverURL string) *DashScopeReranker {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   serverURL,
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)
	return r
}

func TestNewDashScopeReranker(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, err := NewDashScopeReranker(config)
	if err != nil {
		t.Fatalf("NewDashScopeReranker 返回错误: %v", err)
	}
	if r.endPoint != dashScopeEndPoint {
		t.Errorf("endPoint 期望 %s, 实际 %s", dashScopeEndPoint, r.endPoint)
	}
	if r.config.ModelName != "test-model" {
		t.Errorf("ModelName 期望 test-model, 实际 %s", r.config.ModelName)
	}
}

func TestNewDashScopeReranker_配置缺失时返回错误(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	_, err := NewDashScopeReranker(config)
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
}

func TestNewDashScopeReranker_去除端点后缀(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com/services/rerank/text-rerank/text-rerank",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, err := NewDashScopeReranker(config)
	if err != nil {
		t.Fatalf("NewDashScopeReranker 返回错误: %v", err)
	}
	if r.config.APIBase != "https://dashscope.aliyuncs.com" {
		t.Errorf("APIBase 期望 https://dashscope.aliyuncs.com, 实际 %s", r.config.APIBase)
	}
}

func TestDashScopeReranker_Rerank(t *testing.T) {
	server := newTestDashScopeServer(dashScopeTestResponse)
	defer server.Close()

	r := newTestDashScopeReranker(server.URL)
	result, err := r.Rerank(context.Background(), "测试查询", []string{"文档1", "文档2", "文档3"})
	if err != nil {
		t.Fatalf("Rerank 返回错误: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("结果数量期望 3, 实际 %d", len(result))
	}
	if result["文档1"] != 0.95 {
		t.Errorf("文档1 分数期望 0.95, 实际 %f", result["文档1"])
	}
	if result["文档2"] != 0.75 {
		t.Errorf("文档2 分数期望 0.75, 实际 %f", result["文档2"])
	}
}

func TestDashScopeReranker_RerankDocs(t *testing.T) {
	server := newTestDashScopeServer(dashScopeTestResponse)
	defer server.Close()

	r := newTestDashScopeReranker(server.URL)
	docs := []*reranker.Document{
		reranker.NewDocument("文档1"),
		reranker.NewDocument("文档2"),
	}
	result, err := r.RerankDocs(context.Background(), "测试查询", docs)
	if err != nil {
		t.Fatalf("RerankDocs 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("结果数量期望 2, 实际 %d", len(result))
	}
}

func TestDashScopeReranker_RerankMultimodal(t *testing.T) {
	server := newTestDashScopeServer(dashScopeTestResponse)
	defer server.Close()

	r := newTestDashScopeReranker(server.URL)
	docs := []*common.MultimodalDocument{
		common.NewMultimodalDocument().AddField(common.ModalityText, "多模态文档1"),
		common.NewMultimodalDocument().
			AddField(common.ModalityText, "图文文档").
			AddField(common.ModalityImage, "https://example.com/img.png"),
	}
	result, err := r.RerankMultimodal(context.Background(), "测试查询", docs)
	if err != nil {
		t.Fatalf("RerankMultimodal 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("结果数量期望 2, 实际 %d", len(result))
	}
}

func TestDashScopeReranker_RerankSync(t *testing.T) {
	server := newTestDashScopeServer(dashScopeTestResponse)
	defer server.Close()

	r := newTestDashScopeReranker(server.URL)
	result, err := r.RerankSync(context.Background(), "测试查询", []string{"文档1", "文档2"})
	if err != nil {
		t.Fatalf("RerankSync 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("结果数量期望 2, 实际 %d", len(result))
	}
}

func TestDashScopeReranker_Rerank_请求失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	r := newTestDashScopeReranker(server.URL)
	_, err := r.Rerank(context.Background(), "测试查询", []string{"文档1"})
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
}

func TestDashScopeReranker_requestParams(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)

	params := r.requestParams("测试查询", []string{"文档1", "文档2"}, 2, nil)

	// 验证顶层结构
	if params["model"] != "test-model" {
		t.Errorf("model 期望 test-model, 实际 %v", params["model"])
	}

	// 验证 input 结构
	input, ok := params["input"].(map[string]any)
	if !ok {
		t.Fatal("input 不是 map[string]any 类型")
	}
	if input["query"] != "测试查询" {
		t.Errorf("input.query 期望 测试查询, 实际 %v", input["query"])
	}

	// 验证 parameters 结构
	parameters, ok := params["parameters"].(map[string]any)
	if !ok {
		t.Fatal("parameters 不是 map[string]any 类型")
	}
	if parameters["return_documents"] != false {
		t.Errorf("parameters.return_documents 期望 false, 实际 %v", parameters["return_documents"])
	}
	if parameters["top_n"] != 2 {
		t.Errorf("parameters.top_n 期望 2, 实际 %v", parameters["top_n"])
	}
	// 默认不传 instruct
	if _, exists := parameters["instruct"]; exists {
		t.Error("parameters.instruct 不应存在")
	}
}

func TestDashScopeReranker_requestParams_自定义Instruct(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)

	customInstruct := "自定义指令"
	opt := reranker.RerankOption{CustomInstruct: customInstruct}
	params := r.requestParams("测试查询", []string{"文档1"}, 1, &opt)

	parameters := params["parameters"].(map[string]any)
	if parameters["instruct"] != customInstruct {
		t.Errorf("parameters.instruct 期望 %s, 实际 %v", customInstruct, parameters["instruct"])
	}
}

func TestDashScopeReranker_requestParams_无Instruct(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)

	params := r.requestParams("测试查询", []string{"文档1"}, 1, nil)

	parameters := params["parameters"].(map[string]any)
	if _, exists := parameters["instruct"]; exists {
		t.Error("parameters.instruct 不应存在")
	}
}

func TestDashScopeReranker_assembleParams_混合文档类型(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)

	mmDoc := common.NewMultimodalDocument().
		AddField(common.ModalityText, "多模态文档").
		AddField(common.ModalityImage, "https://example.com/img.png")
	plainDoc := reranker.NewDocument("纯文本文档")

	docs := []any{"字符串文档", plainDoc, mmDoc}
	_, params, docIDs, err := r.assembleParams("测试查询", docs, nil)
	if err != nil {
		t.Fatalf("assembleParams 返回错误: %v", err)
	}

	// 验证 docIDs
	if len(docIDs) != 3 {
		t.Fatalf("docIDs 长度期望 3, 实际 %d", len(docIDs))
	}
	if docIDs[0] != "字符串文档" {
		t.Errorf("docIDs[0] 期望 字符串文档, 实际 %s", docIDs[0])
	}
	if docIDs[1] != plainDoc.ID {
		t.Errorf("docIDs[1] 期望 %s, 实际 %s", plainDoc.ID, docIDs[1])
	}

	// 验证 documents 是 []map[string]any 格式（因为有多模态）
	input := params["input"].(map[string]any)
	documents := input["documents"]
	docList, ok := documents.([]map[string]any)
	if !ok {
		t.Fatalf("documents 不是 []map[string]any 类型（多模态模式下应为 map 列表），实际类型: %T", documents)
	}
	if len(docList) != 3 {
		t.Fatalf("documents 长度期望 3, 实际 %d", len(docList))
	}
}

func TestDashScopeReranker_assembleParams_不支持的类型(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)

	docs := []any{123} // int 类型不支持
	_, _, _, err := r.assembleParams("测试查询", docs, nil)
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
}

func TestDashScopeReranker_parseResponse(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, _ := NewDashScopeReranker(config)

	var responseData map[string]any
	_ = json.Unmarshal([]byte(dashScopeTestResponse), &responseData)

	docIDs := []string{"文档1", "文档2", "文档3"}
	result := r.parseResponse(responseData, docIDs)

	if len(result) != 3 {
		t.Fatalf("结果数量期望 3, 实际 %d", len(result))
	}
	if result["文档1"] != 0.95 {
		t.Errorf("文档1 分数期望 0.95, 实际 %f", result["文档1"])
	}
	if result["文档2"] != 0.75 {
		t.Errorf("文档2 分数期望 0.75, 实际 %f", result["文档2"])
	}
	if result["文档3"] != 0.50 {
		t.Errorf("文档3 分数期望 0.50, 实际 %f", result["文档3"])
	}
}

func TestDashScopeReranker_WithOption(t *testing.T) {
	config := reranker.RerankerConfig{
		APIKey:    "test-key",
		APIBase:   "https://dashscope.aliyuncs.com",
		ModelName: "test-model",
		Timeout:   10,
		ExtraBody: map[string]any{},
	}
	r, err := NewDashScopeReranker(config,
		WithDashScopeMaxRetries(5),
		WithDashScopeRetryWait(200*time.Millisecond),
		WithDashScopeExtraHeaders(map[string]string{"X-Custom": "value"}),
	)
	if err != nil {
		t.Fatalf("NewDashScopeReranker 返回错误: %v", err)
	}
	if r.maxRetries != 5 {
		t.Errorf("maxRetries 期望 5, 实际 %d", r.maxRetries)
	}
	if r.retryWait != 200*time.Millisecond {
		t.Errorf("retryWait 期望 200ms, 实际 %v", r.retryWait)
	}
	if r.headers["X-Custom"] != "value" {
		t.Error("额外请求头未设置")
	}
}
