package reranker

import (
	"context"
	"encoding/json"
	"testing"
)

// ──────────────────────────── Document 测试 ────────────────────────────

func TestDocument_JSON序列化(t *testing.T) {
	doc := &Document{
		ID:       "doc-001",
		Text:     "测试文档内容",
		Metadata: map[string]any{"source": "test"},
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored Document
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.ID != doc.ID {
		t.Errorf("ID: 期望 %q, 实际 %q", doc.ID, restored.ID)
	}
	if restored.Text != doc.Text {
		t.Errorf("Text: 期望 %q, 实际 %q", doc.Text, restored.Text)
	}
	if restored.Metadata["source"] != "test" {
		t.Errorf("Metadata[source]: 期望 test, 实际 %v", restored.Metadata["source"])
	}
}

func TestDocument_Metadata为nil时序列化(t *testing.T) {
	doc := &Document{
		ID:   "doc-002",
		Text: "无元数据",
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("反序列化到 map 失败: %v", err)
	}
	if _, ok := raw["metadata"]; !ok {
		t.Error("Metadata 为 nil 时 JSON 中应包含 metadata 字段")
	}
}

func TestDocument_零值序列化(t *testing.T) {
	doc := &Document{}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored Document
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.ID != "" {
		t.Errorf("ID: 期望空字符串, 实际 %q", restored.ID)
	}
	if restored.Text != "" {
		t.Errorf("Text: 期望空字符串, 实际 %q", restored.Text)
	}
}

// ──────────────────────────── NewDocument 测试 ────────────────────────────

func TestNewDocument_自动生成UUID(t *testing.T) {
	doc := NewDocument("测试文本")
	if doc.ID == "" {
		t.Error("NewDocument 应自动生成 UUID 作为 ID")
	}
	if doc.Text != "测试文本" {
		t.Errorf("Text: 期望 %q, 实际 %q", "测试文本", doc.Text)
	}
	if doc.Metadata != nil {
		t.Errorf("无 metadata 参数时 Metadata 应为 nil, 实际 %v", doc.Metadata)
	}
}

func TestNewDocument_带Metadata(t *testing.T) {
	meta := map[string]any{"key": "value"}
	doc := NewDocument("文本", meta)
	if doc.Metadata == nil {
		t.Error("带 metadata 参数时 Metadata 不应为 nil")
	}
	if doc.Metadata["key"] != "value" {
		t.Errorf("Metadata[key]: 期望 value, 实际 %v", doc.Metadata["key"])
	}
}

func TestNewDocument_ID唯一(t *testing.T) {
	doc1 := NewDocument("a")
	doc2 := NewDocument("b")
	if doc1.ID == doc2.ID {
		t.Error("两次调用 NewDocument 生成的 ID 应不同")
	}
}

// ──────────────────────────── DocID 测试 ────────────────────────────

func TestDocID_字符串输入(t *testing.T) {
	result := DocID("hello")
	if result != "hello" {
		t.Errorf("字符串输入: 期望 %q, 实际 %q", "hello", result)
	}
}

func TestDocID_Document输入(t *testing.T) {
	doc := &Document{ID: "doc-123", Text: "内容"}
	result := DocID(doc)
	if result != "doc-123" {
		t.Errorf("Document 输入: 期望 %q, 实际 %q", "doc-123", result)
	}
}

func TestDocID_其他类型(t *testing.T) {
	result := DocID(123)
	if result != "" {
		t.Errorf("不支持的类型应返回空字符串, 实际 %q", result)
	}
}

// ──────────────────────────── RerankerConfig 测试 ────────────────────────────

func TestRerankerConfig_校验通过(t *testing.T) {
	config := &RerankerConfig{APIBase: "https://api.example.com"}
	if err := ValidateConfig(config); err != nil {
		t.Errorf("有效配置校验应通过, 实际错误: %v", err)
	}
}

func TestRerankerConfig_APIBase必填(t *testing.T) {
	config := &RerankerConfig{APIBase: ""}
	err := ValidateConfig(config)
	if err == nil {
		t.Fatal("APIBase 为空时应返回错误")
	}
}

func TestRerankerConfig_Timeout为负数(t *testing.T) {
	config := &RerankerConfig{APIBase: "https://api.example.com", Timeout: -1}
	err := ValidateConfig(config)
	if err == nil {
		t.Fatal("Timeout 为负数时应返回错误")
	}
}

// ──────────────────────────── RerankOption 测试 ────────────────────────────

func TestRerankOption_默认指令(t *testing.T) {
	query := ResolveInstruct("搜索查询", nil)
	if query == "搜索查询" {
		t.Error("默认应使用指令模板，查询不应等于原始值")
	}
	if !contains(query, defaultInstruct) {
		t.Errorf("查询应包含默认指令 %q, 实际: %q", defaultInstruct, query)
	}
	if !contains(query, "搜索查询") {
		t.Errorf("查询应包含原始查询文本, 实际: %q", query)
	}
}

func TestRerankOption_自定义指令(t *testing.T) {
	customInstruct := "自定义指令内容"
	opt := &RerankOption{CustomInstruct: customInstruct}
	query := ResolveInstruct("搜索查询", opt)
	if !contains(query, customInstruct) {
		t.Errorf("查询应包含自定义指令 %q, 实际: %q", customInstruct, query)
	}
	if contains(query, defaultInstruct) {
		t.Error("使用自定义指令时不应包含默认指令")
	}
}

func TestRerankOption_禁用指令(t *testing.T) {
	disabled := false
	opt := &RerankOption{InstructEnabled: &disabled}
	query := ResolveInstruct("搜索查询", opt)
	if query != "搜索查询" {
		t.Errorf("禁用指令时查询应等于原始值, 实际: %q", query)
	}
}

func TestRerankOption_启用指令无自定义(t *testing.T) {
	enabled := true
	opt := &RerankOption{InstructEnabled: &enabled}
	query := ResolveInstruct("搜索查询", opt)
	if !contains(query, defaultInstruct) {
		t.Errorf("启用指令但无自定义时应使用默认指令, 实际: %q", query)
	}
}

func TestRerankOption_启用指令带自定义(t *testing.T) {
	enabled := true
	opt := &RerankOption{InstructEnabled: &enabled, CustomInstruct: "我的指令"}
	query := ResolveInstruct("搜索查询", opt)
	if !contains(query, "我的指令") {
		t.Errorf("应包含自定义指令, 实际: %q", query)
	}
}

// ──────────────────────────── BaseReranker 接口约束测试 ────────────────────────────

// fakeReranker 用于测试的模拟重排序模型
type fakeReranker struct{}

func (f *fakeReranker) Rerank(_ context.Context, _ string, docs []string, _ ...RerankOption) (map[string]float64, error) {
	result := make(map[string]float64, len(docs))
	for _, doc := range docs {
		result[doc] = 1.0
	}
	return result, nil
}

func (f *fakeReranker) RerankDocs(_ context.Context, _ string, docs []*Document, _ ...RerankOption) (map[string]float64, error) {
	result := make(map[string]float64, len(docs))
	for _, doc := range docs {
		result[doc.ID] = 1.0
	}
	return result, nil
}

func (f *fakeReranker) RerankSync(_ context.Context, _ string, docs []string, _ ...RerankOption) (map[string]float64, error) {
	result := make(map[string]float64, len(docs))
	for _, doc := range docs {
		result[doc] = 1.0
	}
	return result, nil
}

func (f *fakeReranker) RerankDocsSync(_ context.Context, _ string, docs []*Document, _ ...RerankOption) (map[string]float64, error) {
	result := make(map[string]float64, len(docs))
	for _, doc := range docs {
		result[doc.ID] = 1.0
	}
	return result, nil
}

func TestBaseReranker_接口约束(t *testing.T) {
	// 验证 fakeReranker 满足 BaseReranker 接口
	var _ BaseReranker = &fakeReranker{}
}

func TestBaseReranker_FakeRerank(t *testing.T) {
	reranker := &fakeReranker{}
	result, err := reranker.Rerank(context.Background(), "查询", []string{"文档1", "文档2"})
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("结果数量: 期望 2, 实际 %d", len(result))
	}
	if result["文档1"] != 1.0 {
		t.Errorf("文档1 分数: 期望 1.0, 实际 %f", result["文档1"])
	}
}

func TestBaseReranker_FakeRerankDocs(t *testing.T) {
	r := &fakeReranker{}
	doc1 := NewDocument("文档1")
	doc2 := NewDocument("文档2")
	result, err := r.RerankDocs(context.Background(), "查询", []*Document{doc1, doc2})
	if err != nil {
		t.Fatalf("RerankDocs 失败: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("结果数量: 期望 2, 实际 %d", len(result))
	}
	if result[doc1.ID] != 1.0 {
		t.Errorf("doc1 分数: 期望 1.0, 实际 %f", result[doc1.ID])
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// contains 检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
