package reme

import (
	"context"
	"fmt"
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

	retrieved, ok := cecontext.GetTyped[[]ceschema.MemoryItem](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) == 0 {
		t.Fatal("应检索到记忆")
	}
	// 验证类型为 ReMeRetrievedMemory
	if _, ok := retrieved[0].(ceschema.ReMeRetrievedMemory); !ok {
		t.Error("第一条记忆应为 ReMeRetrievedMemory 类型")
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
	rc.Set("retrieved_memories", []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{WhenToUse: "when A", Content: "content A"},
		ceschema.ReMeRetrievedMemory{WhenToUse: "when B", Content: "content B"},
		ceschema.ReMeRetrievedMemory{WhenToUse: "when C", Content: "content C"},
	})

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	retrieved, ok := cecontext.GetTyped[[]ceschema.MemoryItem](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	// 重排序后应只有 topKRerank=2 个
	if len(retrieved) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d", len(retrieved))
	}
	// 验证并检查排序
	first, ok1 := retrieved[0].(ceschema.ReMeRetrievedMemory)
	second, ok2 := retrieved[1].(ceschema.ReMeRetrievedMemory)
	if !ok1 || !ok2 {
		t.Fatal("记忆应为 ReMeRetrievedMemory 类型")
	}
	// 第一个应为原索引 1（when B）
	if first.WhenToUse != "when B" {
		t.Fatalf("第一个应为 'when B'，实际 '%s'", first.WhenToUse)
	}
	// 第二个应为原索引 0（when A）
	if second.WhenToUse != "when A" {
		t.Fatalf("第二个应为 'when A'，实际 '%s'", second.WhenToUse)
	}
}

// TestRerankMemoryOp_跳过重排序 测试 llmRerank=false 时跳过
func TestRerankMemoryOp_跳过重排序(t *testing.T) {
	sc := newTestServiceContext()
	op := NewRerankMemoryOp(sc, false, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{WhenToUse: "when A", Content: "content A"},
	})

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	// 应保持原顺序
	retrieved, ok := cecontext.GetTyped[[]ceschema.MemoryItem](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(retrieved))
	}
	first, ok := retrieved[0].(ceschema.ReMeRetrievedMemory)
	if !ok || first.WhenToUse != "when A" {
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

// TestRerankMemoryOp_LLM未注册 测试 LLM 未注册时返回错误
func TestRerankMemoryOp_LLM未注册(t *testing.T) {
	sc := newTestServiceContext() // 不注册 LLM
	op := NewRerankMemoryOp(sc, true, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{WhenToUse: "when A", Content: "content A"},
	})

	err := op.Execute(context.Background(), rc)
	if err == nil {
		t.Fatal("期望返回错误")
	}
	if err.Error() != "LLM not configured in ServiceContext" {
		t.Fatalf("错误消息不匹配: %v", err)
	}
}

// TestRerankMemoryOp_LLM调用失败 测试 LLM 调用失败时返回错误
func TestRerankMemoryOp_LLM调用失败(t *testing.T) {
	sc := newTestServiceContext()
	sc.RegisterService("llm", &fakeLLMService{err: fmt.Errorf("LLM error")})
	op := NewRerankMemoryOp(sc, true, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{WhenToUse: "when A", Content: "content A"},
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
	rc.Set("retrieved_memories", []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{WhenToUse: "when A", Content: "content A"},
		ceschema.ReMeRetrievedMemory{WhenToUse: "when B", Content: "content B"},
	})

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	// 解析失败时应保留原顺序
	retrieved, ok := cecontext.GetTyped[[]ceschema.MemoryItem](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d", len(retrieved))
	}
	first, ok1 := retrieved[0].(ceschema.ReMeRetrievedMemory)
	second, ok2 := retrieved[1].(ceschema.ReMeRetrievedMemory)
	if !ok1 || !ok2 {
		t.Fatal("记忆应为 ReMeRetrievedMemory 类型")
	}
	if first.WhenToUse != "when A" || second.WhenToUse != "when B" {
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
	rc.Set("retrieved_memories", []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{WhenToUse: "when A", Content: "content A"},
		ceschema.ReMeRetrievedMemory{WhenToUse: "when B", Content: "content B"},
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
	rc.Set("retrieved_memories", []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{WhenToUse: "when A", Content: "content A"},
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
	// 对齐 Python 格式: Memory {i}:\n  When to use: ...\n  Content: ...\n
	expected := "Memory 1:\n  When to use: when A\n  Content: content A\n"
	if memStr != expected {
		t.Fatalf("格式化原文不匹配，期望 '%s'，实际 '%s'", expected, memStr)
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
	rc.Set("retrieved_memories", []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{WhenToUse: "when A", Content: "content A"},
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
	expected := "Memory 1:\n  When to use: when A\n  Content: content A\n"
	if memStr != expected {
		t.Fatalf("期望 '%s'，实际 '%s'", expected, memStr)
	}
}

// TestRewriteMemoryOp_LLM未注册 测试 LLM 未注册时返回错误
func TestRewriteMemoryOp_LLM未注册(t *testing.T) {
	sc := newTestServiceContext() // 不注册 LLM
	op := NewRewriteMemoryOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{WhenToUse: "when A", Content: "content A"},
	})

	err := op.Execute(context.Background(), rc)
	if err == nil {
		t.Fatal("期望返回错误")
	}
	if err.Error() != "LLM not configured in ServiceContext" {
		t.Fatalf("错误消息不匹配: %v", err)
	}
}

// TestRewriteMemoryOp_LLM调用失败 测试 LLM 调用失败时返回错误
func TestRewriteMemoryOp_LLM调用失败(t *testing.T) {
	sc := newTestServiceContext()
	sc.RegisterService("llm", &fakeLLMService{err: fmt.Errorf("LLM error")})
	op := NewRewriteMemoryOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("retrieved_memories", []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{WhenToUse: "when A", Content: "content A"},
	})

	err := op.Execute(context.Background(), rc)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// ──────────────────────────── 格式化函数测试 ────────────────────────────

// TestFormatCandidatesForRerank_格式 测试对齐 Python 的候选格式
func TestFormatCandidatesForRerank_格式(t *testing.T) {
	candidates := []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "cond A", Content: "exp A"},
		{WhenToUse: "cond B", Content: "exp B"},
	}
	result := formatCandidatesForRerank(candidates)

	// 对齐 Python: Candidate {i}:\nCondition: {cond}\nExperience: {content}\n
	if !contains(result, "Candidate 0:") {
		t.Fatal("应包含 'Candidate 0:'")
	}
	if !contains(result, "Condition: cond A") {
		t.Fatal("应包含 'Condition: cond A'")
	}
	if !contains(result, "Experience: exp A") {
		t.Fatal("应包含 'Experience: exp A'")
	}
	if !contains(result, "Candidate 1:") {
		t.Fatal("应包含 'Candidate 1:'")
	}
	// 候选项之间用 "\n---\n" 连接
	if !contains(result, "\n---\n") {
		t.Fatal("候选项之间应用 \\n---\\n 连接")
	}
}

// TestFormatMemoriesForContext_格式 测试对齐 Python 的记忆格式
func TestFormatMemoriesForContext_格式(t *testing.T) {
	memories := []ceschema.ReMeRetrievedMemory{
		{WhenToUse: "cond A", Content: "content A"},
	}
	result := formatMemoriesForContext(memories)

	// 对齐 Python: Memory {i}:\n  When to use: {cond}\n  Content: {content}\n
	if !contains(result, "Memory 1:") {
		t.Fatal("应包含 'Memory 1:'")
	}
	if !contains(result, "When to use: cond A") {
		t.Fatal("应包含 'When to use: cond A'")
	}
	if !contains(result, "Content: content A") {
		t.Fatal("应包含 'Content: content A'")
	}
}

// contains 简单字符串包含检查
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && findSubstring(s, sub)))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
