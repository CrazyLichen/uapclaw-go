package rb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/persistence"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/vector_store"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeEmbeddingService EmbeddingService 的 mock 实现
type fakeEmbeddingService struct {
	embeddings [][]float64
	err        error
}

func (f *fakeEmbeddingService) Embed(_ context.Context, _ string) ([]float64, error) {
	if len(f.embeddings) > 0 {
		return f.embeddings[0], nil
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

func (f *fakeEmbeddingService) EmbedBatch(_ context.Context, _ []string) ([][]float64, error) {
	return f.embeddings, f.err
}

// ──────────────────────────── SummarizeMemoryOp 测试 ────────────────────────────

// TestSummarizeMemoryOp_matts为none 测试 matts="none" 时正常提取
func TestSummarizeMemoryOp_matts为none(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "# Memory Item 1\n## Title Test Strategy\n## Description A test description\n## Content Test content for the strategy",
	}
	sc.RegisterService("llm", llm)

	op := NewSummarizeMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")
	rc.Set("query", "test query")
	rc.Set("trajectories", []string{"step A → step B → step C"})
	rc.Set("label", []bool{true})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "memories")
	require.NotEmpty(t, memories)
	assert.Equal(t, "test query", memories[0].Query)
	assert.NotNil(t, memories[0].Label)
	assert.True(t, *memories[0].Label)
}

// TestSummarizeMemoryOp_matts为sequential 测试 matts="sequential" 时正常提取
func TestSummarizeMemoryOp_matts为sequential(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "# Memory Item 1\n## Title Seq Strategy\n## Description Seq description\n## Content Seq content",
	}
	sc.RegisterService("llm", llm)

	op := NewSummarizeMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "sequential")
	rc.Set("query", "seq query")
	rc.Set("trajectories", []string{"step 1 → step 2"})
	rc.Set("label", []bool{false})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "memories")
	require.NotEmpty(t, memories)
	assert.NotNil(t, memories[0].Label)
	assert.False(t, *memories[0].Label)
}

// TestSummarizeMemoryOp_matts不匹配 测试 matts 不匹配时返回 nil
func TestSummarizeMemoryOp_matts不匹配(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSummarizeMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "parallel")
	rc.Set("query", "test query")
	rc.Set("trajectories", []string{"step1"})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	// 不匹配时不设置 memories
	_, ok := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "memories")
	assert.False(t, ok)
}

// TestSummarizeMemoryOp_label从rc获取 测试 label 从 rc 中获取
func TestSummarizeMemoryOp_label从rc获取(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "# Memory Item 1\n## Title From RC\n## Description From RC label\n## Content RC content",
	}
	sc.RegisterService("llm", llm)

	op := NewSummarizeMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")
	rc.Set("query", "test query")
	rc.Set("trajectories", []string{"step A"})
	rc.Set("label", []bool{true})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "memories")
	require.NotEmpty(t, memories)
	assert.True(t, *memories[0].Label)
}

// TestSummarizeMemoryOp_label通过LLM判断 测试 label 不在 rc 中时通过 LLM 判断
func TestSummarizeMemoryOp_label通过LLM判断(t *testing.T) {
	sc := cecontext.NewServiceContext()
	// LLM 先被 LabelDeterminator 调用判定 label，再被 SummarizeMemoryOp 调用提取
	// 用一个计数器模拟两次不同响应
	callCount := 0
	llm := &fakeLLMServiceWithCount{
		responses: []string{
			"Thoughts: looks good\nStatus: success",   // LabelDeterminator 调用
			"# Memory Item 1\n## Title LLM Label\n## Description LLM determined\n## Content LLM content", // 提取调用
		},
		callCount: &callCount,
	}
	sc.RegisterService("llm", llm)

	op := NewSummarizeMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")
	rc.Set("query", "test query")
	rc.Set("trajectories", []string{"step A"})
	// 不设置 label，触发 LabelDeterminator

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "memories")
	require.NotEmpty(t, memories)
	assert.True(t, *memories[0].Label)
}

// fakeLLMServiceWithCount 支持多次调用返回不同响应的 mock LLM
type fakeLLMServiceWithCount struct {
	responses []string
	callCount *int
	err       error
}

func (f *fakeLLMServiceWithCount) Generate(_ context.Context, _ string, _ ...cecontext.GenerateOption) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	idx := *f.callCount
	*f.callCount++
	if idx < len(f.responses) {
		return f.responses[idx], nil
	}
	return f.responses[len(f.responses)-1], nil
}

// ──────────────────────────── SummarizeMemoryParallelOp 测试 ────────────────────────────

// TestSummarizeMemoryParallelOp_matts为parallel 测试 matts="parallel" 时正常提取
func TestSummarizeMemoryParallelOp_matts为parallel(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "# Memory Item 1\n## Title Parallel Strategy\n## Description Parallel desc\n## Content Parallel content",
	}
	sc.RegisterService("llm", llm)

	op := NewSummarizeMemoryParallelOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "parallel")
	rc.Set("query", "parallel query")
	rc.Set("trajectories", []string{"step A", "step B"})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "memories")
	require.NotEmpty(t, memories)
	assert.Equal(t, "parallel query", memories[0].Query)
	assert.Nil(t, memories[0].Label) // parallel 模式 label=nil
}

// TestSummarizeMemoryParallelOp_matts为combined 测试 matts="combined" 时正常提取
func TestSummarizeMemoryParallelOp_matts为combined(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "# Memory Item 1\n## Title Combined Strategy\n## Description Combined desc\n## Content Combined content",
	}
	sc.RegisterService("llm", llm)

	op := NewSummarizeMemoryParallelOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "combined")
	rc.Set("query", "combined query")
	rc.Set("trajectories", []string{"step A", "step B"})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "memories")
	require.NotEmpty(t, memories)
	assert.Nil(t, memories[0].Label)
}

// TestSummarizeMemoryParallelOp_轨迹不足 测试轨迹不足 2 条时返回 nil
func TestSummarizeMemoryParallelOp_轨迹不足(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSummarizeMemoryParallelOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "parallel")
	rc.Set("query", "test query")
	rc.Set("trajectories", []string{"only one"})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	_, ok := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "memories")
	assert.False(t, ok)
}

// TestSummarizeMemoryParallelOp_matts不匹配 测试 matts 不匹配时返回 nil
func TestSummarizeMemoryParallelOp_matts不匹配(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSummarizeMemoryParallelOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")
	rc.Set("query", "test query")
	rc.Set("trajectories", []string{"step A", "step B"})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	_, ok := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "memories")
	assert.False(t, ok)
}

// ──────────────────────────── UpdateVectorStoreOp 测试 ────────────────────────────

// TestUpdateVectorStoreOp_正常更新 测试正常向量存储更新
func TestUpdateVectorStoreOp_正常更新(t *testing.T) {
	sc := cecontext.NewServiceContext()
	emb := &fakeEmbeddingService{
		embeddings: [][]float64{{0.1, 0.2, 0.3}},
	}
	sc.RegisterService("embedding_model", emb)

	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	op := NewUpdateVectorStoreOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("memories", []*ceschema.ReasoningBankMemory{
		{
			Query:  "test query",
			Memory: []ceschema.ReasoningBankMemoryItem{{Title: "t", Description: "d", Content: "c"}},
		},
	})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	storedCount, _ := cecontext.GetTyped[int](rc, "stored_count")
	memoryIDs, _ := cecontext.GetTyped[[]string](rc, "memory_ids")
	assert.Equal(t, 1, storedCount)
	assert.Len(t, memoryIDs, 1)
}

// TestUpdateVectorStoreOp_服务未注册 测试 EmbeddingModel 或 VectorStore 未注册时返回错误
func TestUpdateVectorStoreOp_服务未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	// 不注册任何服务

	op := NewUpdateVectorStoreOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("memories", []*ceschema.ReasoningBankMemory{
		{Query: "q", Memory: []ceschema.ReasoningBankMemoryItem{{Title: "t", Description: "d", Content: "c"}}},
	})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "EmbeddingModel not configured")
}

// TestUpdateVectorStoreOp_空记忆 测试无记忆时返回空
func TestUpdateVectorStoreOp_空记忆(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewUpdateVectorStoreOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("memories", []*ceschema.ReasoningBankMemory{})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	storedCount, _ := cecontext.GetTyped[int](rc, "stored_count")
	assert.Equal(t, 0, storedCount)
}

// ──────────────────────────── PersistMemoryOp 测试 ────────────────────────────

// TestPersistMemoryOp_正常持久化 测试正常持久化流程
func TestPersistMemoryOp_正常持久化(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	// 先写入一些 reasoning_bank_memory 节点
	mem := &ceschema.ReasoningBankMemory{
		BaseMemory: ceschema.BaseMemory{WorkspaceID: "user1"},
		Query:      "test query",
		Memory:     []ceschema.ReasoningBankMemoryItem{{Title: "t", Description: "d", Content: "c"}},
	}
	vectorNode := mem.ToVectorNode()
	vectorNode.Embedding = []float64{0.1, 0.2}
	vs.Upsert(context.Background(), vectorNode)

	helper := persistence.NewMemoryPersistenceHelper(
		persistence.WithPersistPath(t.TempDir() + "/{algo_name}/{user_id}.json"),
	)

	op := NewPersistMemoryOp(sc, helper)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	persistCount, _ := cecontext.GetTyped[int](rc, "persist_count")
	assert.Equal(t, 1, persistCount)
}

// TestPersistMemoryOp_VectorStore未注册 测试 VectorStore 未注册时返回错误
func TestPersistMemoryOp_VectorStore未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	helper := persistence.NewMemoryPersistenceHelper()
	op := NewPersistMemoryOp(sc, helper)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "VectorStore not configured")
}
