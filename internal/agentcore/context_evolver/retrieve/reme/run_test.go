package reme

import (
	"context"
	"testing"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	coreschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
	vector_store "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/vector_store"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeLLMService 模拟 LLM 服务
type fakeLLMService struct {
	response string
	err      error
}

// fakeEmbeddingService 模拟 Embedding 服务
type fakeEmbeddingService struct {
	embeddings [][]float64
	err        error
}

// ──────────────────────────── 导出方法 ────────────────────────────

func (f *fakeLLMService) Generate(_ context.Context, _ string, _ ...cecontext.GenerateOption) (string, error) {
	return f.response, f.err
}

func (f *fakeEmbeddingService) Embed(_ context.Context, _ string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.embeddings) > 0 {
		return f.embeddings[0], nil
	}
	return []float64{1.0, 0.0, 0.0}, nil
}

func (f *fakeEmbeddingService) EmbedBatch(_ context.Context, _ []string) ([][]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.embeddings, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestServiceContext 创建包含 fake 服务的 ServiceContext
func newTestServiceContext() *cecontext.ServiceContext {
	sc := cecontext.NewServiceContext()

	// 注册 Embedding 服务
	sc.RegisterService("embedding_model", &fakeEmbeddingService{
		embeddings: [][]float64{{1.0, 0.0, 0.0}},
	})

	// 注册 VectorStore
	vs := vector_store.NewMemoryVectorStore()
	// 预填充测试数据
	vs.LoadNode("reme_default_test1", coreschema.NewVectorNode(
		"reme_default_test1",
		"when to use python",
		[]float64{0.9, 0.1, 0.0},
		map[string]any{
			"type":         "reme_memory",
			"workspace_id": "default",
			"when_to_use":  "when to use python",
			"content":      "Use Python for data analysis",
		},
	))
	vs.LoadNode("reme_default_test2", coreschema.NewVectorNode(
		"reme_default_test2",
		"when to use Go",
		[]float64{0.1, 0.9, 0.0},
		map[string]any{
			"type":         "reme_memory",
			"workspace_id": "default",
			"when_to_use":  "when to use Go",
			"content":      "Use Go for backend services",
		},
	))
	vs.LoadNode("reme_default_test3", coreschema.NewVectorNode(
		"reme_default_test3",
		"when to use Rust",
		[]float64{0.0, 0.1, 0.9},
		map[string]any{
			"type":         "reme_memory",
			"workspace_id": "default",
			"when_to_use":  "when to use Rust",
			"content":      "Use Rust for system programming",
		},
	))
	sc.RegisterService("vector_store", vs)

	return sc
}

// newTestServiceContextWithLLM 创建包含 fake LLM 的 ServiceContext
func newTestServiceContextWithLLM(llmResponse string) *cecontext.ServiceContext {
	sc := newTestServiceContext()
	sc.RegisterService("llm", &fakeLLMService{response: llmResponse})
	return sc
}

// newEmptyServiceContext 创建不含任何服务的 ServiceContext
func newEmptyServiceContext() *cecontext.ServiceContext {
	return cecontext.NewServiceContext()
}

// ──────────────────────────── RecallMemoryOp 测试 ────────────────────────────

// TestRecallMemoryOp_正常检索 测试正常向量检索
func TestRecallMemoryOp_正常检索(t *testing.T) {
	sc := newTestServiceContext()
	op := NewRecallMemoryOp(sc, 3)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "python data analysis")
	rc.Set("user_id", "default")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	retrieved, ok := cecontext.GetTyped[[]ceschema.ReMeRetrievedMemory](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) == 0 {
		t.Fatal("应检索到记忆")
	}
}

// TestRecallMemoryOp_Embedding未注册 测试 Embedding 服务未注册
func TestRecallMemoryOp_Embedding未注册(t *testing.T) {
	sc := newEmptyServiceContext()
	op := NewRecallMemoryOp(sc, 3)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test")

	err := op.Execute(context.Background(), rc)
	if err == nil {
		t.Fatal("期望返回错误")
	}
	if err.Error() != "embedding model not configured in ServiceContext" {
		t.Fatalf("错误消息不匹配: %v", err)
	}
}

// TestRecallMemoryOp_VectorStore未注册 测试 VectorStore 服务未注册
func TestRecallMemoryOp_VectorStore未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("embedding_model", &fakeEmbeddingService{})
	op := NewRecallMemoryOp(sc, 3)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test")

	err := op.Execute(context.Background(), rc)
	if err == nil {
		t.Fatal("期望返回错误")
	}
	if err.Error() != "vector store not configured in ServiceContext" {
		t.Fatalf("错误消息不匹配: %v", err)
	}
}

// ──────────────────────────── RerankMemoryOp 测试 ────────────────────────────

// TestRerankMemoryOp_正常重排序 测试正常 LLM 重排序
func TestRerankMemoryOp_正常重排序(t *testing.T) {
	llmResponse := "```json\n{\"ranked_indices\": [1, 0, 2], \"reasoning\": \"test\"}\n```"
	sc := newTestServiceContextWithLLM(llmResponse)
	op := NewRerankMemoryOp(sc, true, 2)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when A", Content: "content A"},
		{WhenToUse: "when B", Content: "content B"},
		{WhenToUse: "when C", Content: "content C"},
	})

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	retrieved, ok := cecontext.GetTyped[[]ceschema.ReMeRetrievedMemory](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	// 重排序后应只有 topKRerank=2 个
	if len(retrieved) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d", len(retrieved))
	}
	// 第一个应为原索引 1（when B）
	if retrieved[0].WhenToUse != "when B" {
		t.Fatalf("第一个应为 'when B'，实际 '%s'", retrieved[0].WhenToUse)
	}
	// 第二个应为原索引 0（when A）
	if retrieved[1].WhenToUse != "when A" {
		t.Fatalf("第二个应为 'when A'，实际 '%s'", retrieved[1].WhenToUse)
	}
}

// TestRerankMemoryOp_跳过重排序 测试 llmRerank=false 时跳过
func TestRerankMemoryOp_跳过重排序(t *testing.T) {
	sc := newTestServiceContext()
	op := NewRerankMemoryOp(sc, false, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when A", Content: "content A"},
	})

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	// 应保持原顺序
	retrieved, ok := cecontext.GetTyped[[]ceschema.ReMeRetrievedMemory](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) != 1 || retrieved[0].WhenToUse != "when A" {
		t.Fatal("跳过重排序时应保持原数据")
	}
}

// TestRerankMemoryOp_无记忆 测试无记忆时跳过
func TestRerankMemoryOp_无记忆(t *testing.T) {
	sc := newTestServiceContextWithLLM("any response")
	op := NewRerankMemoryOp(sc, true, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	// 不设置 retrieved_memories

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
}

// TestRerankMemoryOp_LLM未注册 测试 LLM 未注册
func TestRerankMemoryOp_LLM未注册(t *testing.T) {
	sc := newTestServiceContext() // 不注册 LLM
	op := NewRerankMemoryOp(sc, true, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when A", Content: "content A"},
	})

	err := op.Execute(context.Background(), rc)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// TestRerankMemoryOp_解析失败用原顺序 测试 LLM 响应解析失败时保留原顺序
func TestRerankMemoryOp_解析失败用原顺序(t *testing.T) {
	llmResponse := "This is not valid JSON for reranking"
	sc := newTestServiceContextWithLLM(llmResponse)
	op := NewRerankMemoryOp(sc, true, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when A", Content: "content A"},
		{WhenToUse: "when B", Content: "content B"},
	})

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	// 解析失败时应保留原顺序
	retrieved, ok := cecontext.GetTyped[[]ceschema.ReMeRetrievedMemory](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d", len(retrieved))
	}
	if retrieved[0].WhenToUse != "when A" || retrieved[1].WhenToUse != "when B" {
		t.Fatal("解析失败时应保留原顺序")
	}
}

// ──────────────────────────── RewriteMemoryOp 测试 ────────────────────────────

// TestRewriteMemoryOp_正常改写 测试正常 LLM 改写
func TestRewriteMemoryOp_正常改写(t *testing.T) {
	llmResponse := "```json\n{\"rewritten_context\": \"A cohesive rewritten guidance\"}\n```"
	sc := newTestServiceContextWithLLM(llmResponse)
	op := NewRewriteMemoryOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when A", Content: "content A"},
		{WhenToUse: "when B", Content: "content B"},
	})

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	memStr, ok := cecontext.GetTyped[string](rc, "memory_string")
	if !ok {
		t.Fatal("memory_string 未设置")
	}
	if memStr != "A cohesive rewritten guidance" {
		t.Fatalf("期望 'A cohesive rewritten guidance'，实际 '%s'", memStr)
	}
}

// TestRewriteMemoryOp_跳过改写 测试 llmRewrite=false 时用格式化原文
func TestRewriteMemoryOp_跳过改写(t *testing.T) {
	sc := newTestServiceContext()
	op := NewRewriteMemoryOp(sc, false)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when A", Content: "content A"},
	})

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	memStr, ok := cecontext.GetTyped[string](rc, "memory_string")
	if !ok {
		t.Fatal("memory_string 未设置")
	}
	if memStr == "" {
		t.Fatal("应设置格式化原文")
	}
	if memStr != "When to use: when A\nContent: content A" {
		t.Fatalf("格式化原文不匹配，实际 '%s'", memStr)
	}
}

// TestRewriteMemoryOp_无记忆 测试无记忆时设置空字符串
func TestRewriteMemoryOp_无记忆(t *testing.T) {
	sc := newTestServiceContextWithLLM("any response")
	op := NewRewriteMemoryOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	// 不设置 retrieved_memories

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	memStr, ok := cecontext.GetTyped[string](rc, "memory_string")
	if !ok {
		t.Fatal("memory_string 未设置")
	}
	if memStr != "" {
		t.Fatalf("无记忆时应为空字符串，实际 '%s'", memStr)
	}
}

// TestRewriteMemoryOp_解析失败用原文 测试 LLM 响应解析失败时降级为格式化原文
func TestRewriteMemoryOp_解析失败用原文(t *testing.T) {
	llmResponse := "This is not valid JSON for rewriting"
	sc := newTestServiceContextWithLLM(llmResponse)
	op := NewRewriteMemoryOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when A", Content: "content A"},
	})

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	memStr, ok := cecontext.GetTyped[string](rc, "memory_string")
	if !ok {
		t.Fatal("memory_string 未设置")
	}
	// 解析失败时应降级为格式化原文
	expected := "When to use: when A\nContent: content A"
	if memStr != expected {
		t.Fatalf("期望 '%s'，实际 '%s'", expected, memStr)
	}
}

// TestRewriteMemoryOp_LLM未注册 测试 LLM 未注册时降级为格式化原文
func TestRewriteMemoryOp_LLM未注册(t *testing.T) {
	sc := newTestServiceContext() // 不注册 LLM
	op := NewRewriteMemoryOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "when A", Content: "content A"},
	})

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	memStr, ok := cecontext.GetTyped[string](rc, "memory_string")
	if !ok {
		t.Fatal("memory_string 未设置")
	}
	expected := "When to use: when A\nContent: content A"
	if memStr != expected {
		t.Fatalf("期望 '%s'，实际 '%s'", expected, memStr)
	}
}
