package reme

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

// fakeLLMService LLMService 的 mock 实现
type fakeLLMService struct {
	response string
	err      error
}

func (f *fakeLLMService) Generate(_ context.Context, _ string, _ ...cecontext.GenerateOption) (string, error) {
	return f.response, f.err
}

// fakeEmbeddingService EmbeddingService 的 mock 实现
type fakeEmbeddingService struct {
	embeddings [][]float64
	err        error
	callCount  int
}

func (f *fakeEmbeddingService) Embed(_ context.Context, _ string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.embeddings) > 0 {
		idx := f.callCount
		f.callCount++
		if idx < len(f.embeddings) {
			return f.embeddings[idx], nil
		}
		return f.embeddings[0], nil
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

func (f *fakeEmbeddingService) EmbedBatch(_ context.Context, _ []string) ([][]float64, error) {
	return f.embeddings, f.err
}

// ──────────────────────────── TrajectoryPreprocessOp 测试 ────────────────────────────

// TestTrajectoryPreprocessOp_正常分组 测试轨迹按分数正常分组
func TestTrajectoryPreprocessOp_正常分组(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewTrajectoryPreprocessOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("trajectories", []string{"step1", "step2", "step3"})
	rc.Set("score", []float64{1.0, 0.5, 1.5})
	rc.Set("threshold", 1.0)

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	success, _ := cecontext.GetTyped[[]string](rc, "success_trajectories")
	failure, _ := cecontext.GetTyped[[]string](rc, "failure_trajectories")
	all, _ := cecontext.GetTyped[[]string](rc, "all_trajectories")

	assert.Equal(t, []string{"step1", "step3"}, success)
	assert.Equal(t, []string{"step2"}, failure)
	assert.Equal(t, []string{"step1", "step2", "step3"}, all)
}

// TestTrajectoryPreprocessOp_空轨迹 测试空轨迹输入
func TestTrajectoryPreprocessOp_空轨迹(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewTrajectoryPreprocessOp(sc)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	success, _ := cecontext.GetTyped[[]string](rc, "success_trajectories")
	failure, _ := cecontext.GetTyped[[]string](rc, "failure_trajectories")
	all, _ := cecontext.GetTyped[[]string](rc, "all_trajectories")

	assert.Equal(t, []string{}, success)
	assert.Equal(t, []string{}, failure)
	assert.Equal(t, []string{}, all)
}

// TestTrajectoryPreprocessOp_默认阈值 测试无阈值时默认为 1
func TestTrajectoryPreprocessOp_默认阈值(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewTrajectoryPreprocessOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("trajectories", []string{"step1", "step2"})
	rc.Set("score", []float64{1.0, 0.5})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	success, _ := cecontext.GetTyped[[]string](rc, "success_trajectories")
	assert.Equal(t, []string{"step1"}, success)
}

// ──────────────────────────── SuccessExtractionOp 测试 ────────────────────────────

// TestSuccessExtractionOp_正常提取 测试成功轨迹正常提取经验
func TestSuccessExtractionOp_正常提取(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n[{\"when_to_use\": \"test when\", \"experience\": \"test exp\", \"tags\": [\"tag1\"], \"step_type\": \"action\", \"tools_used\": [\"tool1\"], \"confidence\": 0.8}]\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewSuccessExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("success_trajectories", []string{"step A → step B"})
	rc.Set("query", "test query")
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "success_memories")
	require.Len(t, memories, 1)
	assert.Equal(t, "test when", memories[0].WhenToUse)
	assert.Equal(t, "test exp", memories[0].Content)
	assert.Equal(t, "user1", memories[0].WorkspaceID)
	assert.Equal(t, []string{"tag1"}, memories[0].Metadata.Tags)
	assert.Equal(t, "action", memories[0].Metadata.StepType)
	assert.Equal(t, []string{"tool1"}, memories[0].Metadata.ToolsUsed)
	assert.InDelta(t, 0.8, memories[0].Metadata.Confidence, 0.01)
}

// TestSuccessExtractionOp_跳过提取 测试 useExtraction=false 时跳过
func TestSuccessExtractionOp_跳过提取(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSuccessExtractionOp(sc, false)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "success_memories")
	assert.Equal(t, []*ceschema.ReMeMemory{}, memories)
}

// TestSuccessExtractionOp_空成功轨迹 测试无成功轨迹时返回空
func TestSuccessExtractionOp_空成功轨迹(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSuccessExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("success_trajectories", []string{})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "success_memories")
	assert.Equal(t, []*ceschema.ReMeMemory{}, memories)
}

// TestSuccessExtractionOp_LLM未注册 测试 LLM 未注册时返回错误
func TestSuccessExtractionOp_LLM未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSuccessExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("success_trajectories", []string{"step1"})

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM not configured")
}

// ──────────────────────────── FailureExtractionOp 测试 ────────────────────────────

// TestFailureExtractionOp_正常提取 测试失败轨迹正常提取经验
func TestFailureExtractionOp_正常提取(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n[{\"when_to_use\": \"avoid when\", \"experience\": \"avoid exp\", \"tags\": [\"fail\"], \"step_type\": \"reasoning\", \"tools_used\": [], \"confidence\": 0.6}]\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewFailureExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("failure_trajectories", []string{"bad step A"})
	rc.Set("query", "test query")
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "failure_memories")
	require.Len(t, memories, 1)
	assert.Equal(t, "avoid when", memories[0].WhenToUse)
	assert.Equal(t, "avoid exp", memories[0].Content)
}

// TestFailureExtractionOp_跳过提取 测试 useExtraction=false 时跳过
func TestFailureExtractionOp_跳过提取(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewFailureExtractionOp(sc, false)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "failure_memories")
	assert.Equal(t, []*ceschema.ReMeMemory{}, memories)
}

// TestFailureExtractionOp_LLM未注册 测试 LLM 未注册时返回错误
func TestFailureExtractionOp_LLM未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewFailureExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("failure_trajectories", []string{"bad step"})

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
}

// ──────────────────────────── ComparativeExtractionOp 测试 ────────────────────────────

// TestComparativeExtractionOp_正常对比 测试高低分轨迹正常对比
func TestComparativeExtractionOp_正常对比(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n[{\"when_to_use\": \"compare when\", \"experience\": \"compare exp\", \"tags\": [\"comp\"], \"step_type\": \"decision\", \"tools_used\": [\"tool2\"], \"confidence\": 0.9}]\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewComparativeExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("all_trajectories", []string{"high steps", "low steps"})
	rc.Set("score", []float64{0.9, 0.3})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")
	require.Len(t, memories, 1)
	assert.Equal(t, "compare when", memories[0].WhenToUse)
}

// TestComparativeExtractionOp_跳过 测试 useExtraction=false 时跳过
func TestComparativeExtractionOp_跳过(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewComparativeExtractionOp(sc, false)
	rc := cecontext.NewRuntimeContext()

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")
	assert.Equal(t, []*ceschema.ReMeMemory{}, memories)
}

// TestComparativeExtractionOp_轨迹不足 测试轨迹不足时返回空
func TestComparativeExtractionOp_轨迹不足(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewComparativeExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("all_trajectories", []string{"only one"})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")
	assert.Equal(t, []*ceschema.ReMeMemory{}, memories)
}

// TestComparativeExtractionOp_分数相同 测试所有分数相同时返回空
func TestComparativeExtractionOp_分数相同(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewComparativeExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("all_trajectories", []string{"step A", "step B"})
	rc.Set("score", []float64{0.5, 0.5})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")
	assert.Equal(t, []*ceschema.ReMeMemory{}, memories)
}

// ──────────────────────────── ComparativeAllExtractionOp 测试 ────────────────────────────

// TestComparativeAllExtractionOp_正常对比 测试全量对比正常
func TestComparativeAllExtractionOp_正常对比(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n[{\"when_to_use\": \"all when\", \"experience\": \"all exp\", \"tags\": [\"all\"], \"step_type\": \"action\", \"tools_used\": [], \"confidence\": 0.7}]\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewComparativeAllExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("all_trajectories", []string{"step A", "step B"})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")
	require.Len(t, memories, 1)
	assert.Equal(t, "all when", memories[0].WhenToUse)
}

// TestComparativeAllExtractionOp_轨迹不足 测试轨迹不足时返回空
func TestComparativeAllExtractionOp_轨迹不足(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewComparativeAllExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("all_trajectories", []string{"only one"})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")
	assert.Equal(t, []*ceschema.ReMeMemory{}, memories)
}

// TestComparativeAllExtractionOp_LLM未注册 测试 LLM 未注册时返回错误
func TestComparativeAllExtractionOp_LLM未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewComparativeAllExtractionOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("all_trajectories", []string{"step A", "step B"})

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM not configured")
}

// ──────────────────────────── MemoryValidationOp 测试 ────────────────────────────

// TestMemoryValidationOp_正常校验 测试正常校验流程
func TestMemoryValidationOp_正常校验(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n{\"is_valid\": true, \"score\": 0.9, \"feedback\": \"Good\", \"reason\": \"\"}\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewMemoryValidationOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("success_memories", []*ceschema.ReMeMemory{
		{WhenToUse: "when1", Content: "content1"},
	})
	rc.Set("failure_memories", []*ceschema.ReMeMemory{})
	rc.Set("comparative_memories", []*ceschema.ReMeMemory{})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	validated, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "validated_memories")
	require.Len(t, validated, 1)
	assert.InDelta(t, 0.9, validated[0].Score, 0.01)
}

// TestMemoryValidationOp_跳过校验 测试 useValidation=false 时直接通过
func TestMemoryValidationOp_跳过校验(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewMemoryValidationOp(sc, false)
	rc := cecontext.NewRuntimeContext()
	input := []*ceschema.ReMeMemory{{WhenToUse: "w", Content: "c"}}
	rc.Set("success_memories", input)
	rc.Set("failure_memories", []*ceschema.ReMeMemory{})
	rc.Set("comparative_memories", []*ceschema.ReMeMemory{})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	validated, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "validated_memories")
	assert.Len(t, validated, 1)
}

// TestMemoryValidationOp_空记忆 测试无记忆时返回空
func TestMemoryValidationOp_空记忆(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewMemoryValidationOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("success_memories", []*ceschema.ReMeMemory{})
	rc.Set("failure_memories", []*ceschema.ReMeMemory{})
	rc.Set("comparative_memories", []*ceschema.ReMeMemory{})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	validated, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "validated_memories")
	assert.Equal(t, []*ceschema.ReMeMemory{}, validated)
}

// TestMemoryValidationOp_校验不通过 测试记忆校验不通过时被过滤
func TestMemoryValidationOp_校验不通过(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{
		response: "```json\n{\"is_valid\": false, \"score\": 0.2, \"feedback\": \"Poor quality\", \"reason\": \"too vague\"}\n```",
	}
	sc.RegisterService("llm", llm)

	op := NewMemoryValidationOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("success_memories", []*ceschema.ReMeMemory{
		{WhenToUse: "when1", Content: "bad content"},
	})
	rc.Set("failure_memories", []*ceschema.ReMeMemory{})
	rc.Set("comparative_memories", []*ceschema.ReMeMemory{})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	validated, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "validated_memories")
	assert.Len(t, validated, 0)
}

// ──────────────────────────── MemoryDeduplicationOp 测试 ────────────────────────────

// TestMemoryDeduplicationOp_正常去重 测试正常去重流程
func TestMemoryDeduplicationOp_正常去重(t *testing.T) {
	sc := cecontext.NewServiceContext()
	emb := &fakeEmbeddingService{
		embeddings: [][]float64{
			{1.0, 0.0, 0.0},
			{0.0, 1.0, 0.0},
		},
	}
	sc.RegisterService("embedding_model", emb)

	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	op := NewMemoryDeduplicationOp(sc, true, 0.9)
	rc := cecontext.NewRuntimeContext()
	rc.Set("validated_memories", []*ceschema.ReMeMemory{
		{WhenToUse: "when1", Content: "content1"},
		{WhenToUse: "when2", Content: "content2"},
	})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	deduped, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "deduplicated_memories")
	dupCount, _ := cecontext.GetTyped[int](rc, "duplicate_count")
	assert.Len(t, deduped, 2)
	assert.Equal(t, 0, dupCount)
}

// TestMemoryDeduplicationOp_跳过去重 测试 useDeduplication=false 时跳过
func TestMemoryDeduplicationOp_跳过去重(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewMemoryDeduplicationOp(sc, false, 0.9)
	rc := cecontext.NewRuntimeContext()
	input := []*ceschema.ReMeMemory{{WhenToUse: "w", Content: "c"}}
	rc.Set("validated_memories", input)
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	deduped, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "deduplicated_memories")
	assert.Len(t, deduped, 1)
}

// TestMemoryDeduplicationOp_空记忆 测试无记忆时返回空
func TestMemoryDeduplicationOp_空记忆(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewMemoryDeduplicationOp(sc, true, 0.9)
	rc := cecontext.NewRuntimeContext()
	rc.Set("validated_memories", []*ceschema.ReMeMemory{})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	deduped, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "deduplicated_memories")
	assert.Equal(t, []*ceschema.ReMeMemory{}, deduped)
	dupCount, _ := cecontext.GetTyped[int](rc, "duplicate_count")
	assert.Equal(t, 0, dupCount)
}

// TestMemoryDeduplicationOp_检测重复 测试检测到重复记忆
func TestMemoryDeduplicationOp_检测重复(t *testing.T) {
	sc := cecontext.NewServiceContext()
	// 两个记忆生成相同的 embedding → 余弦相似度 = 1.0 → 重复
	emb := &fakeEmbeddingService{
		embeddings: [][]float64{
			{1.0, 0.0, 0.0},
			{1.0, 0.0, 0.0},
		},
	}
	sc.RegisterService("embedding_model", emb)

	op := NewMemoryDeduplicationOp(sc, true, 0.9)
	rc := cecontext.NewRuntimeContext()
	rc.Set("validated_memories", []*ceschema.ReMeMemory{
		{WhenToUse: "when1", Content: "content1"},
		{WhenToUse: "when2", Content: "content2"},
	})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	deduped, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "deduplicated_memories")
	dupCount, _ := cecontext.GetTyped[int](rc, "duplicate_count")
	assert.Len(t, deduped, 1)
	assert.Equal(t, 1, dupCount)
}

// TestMemoryDeduplicationOp_EmbeddingModel未注册 测试 EmbeddingModel 未注册时返回错误
func TestMemoryDeduplicationOp_EmbeddingModel未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	// 不注册 embedding_model
	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	op := NewMemoryDeduplicationOp(sc, true, 0.9)
	rc := cecontext.NewRuntimeContext()
	rc.Set("validated_memories", []*ceschema.ReMeMemory{
		{WhenToUse: "when1", Content: "content1"},
	})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EmbeddingModel not configured")
}

// ──────────────────────────── UpdateVectorStoreOp 测试 ────────────────────────────

// TestUpdateVectorStoreOp_正常存储 测试正常存储流程
func TestUpdateVectorStoreOp_正常存储(t *testing.T) {
	sc := cecontext.NewServiceContext()
	emb := &fakeEmbeddingService{
		embeddings: [][]float64{{0.1, 0.2, 0.3}},
	}
	sc.RegisterService("embedding_model", emb)

	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	op := NewUpdateVectorStoreOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("deduplicated_memories", []*ceschema.ReMeMemory{
		{WhenToUse: "when1", Content: "content1"},
	})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	storedCount, _ := cecontext.GetTyped[int](rc, "stored_count")
	memoryIDs, _ := cecontext.GetTyped[[]string](rc, "memory_ids")
	assert.Equal(t, 1, storedCount)
	assert.Len(t, memoryIDs, 1)
}

// TestUpdateVectorStoreOp_空记忆 测试无记忆时返回空
func TestUpdateVectorStoreOp_空记忆(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewUpdateVectorStoreOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("deduplicated_memories", []*ceschema.ReMeMemory{})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	storedCount, _ := cecontext.GetTyped[int](rc, "stored_count")
	assert.Equal(t, 0, storedCount)
}

// TestUpdateVectorStoreOp_Embedding未注册 测试 EmbeddingModel 未注册时返回错误
func TestUpdateVectorStoreOp_Embedding未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewUpdateVectorStoreOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("deduplicated_memories", []*ceschema.ReMeMemory{{WhenToUse: "w", Content: "c"}})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "EmbeddingModel not configured")
}

// TestUpdateVectorStoreOp_VectorStore未注册 测试 VectorStore 未注册时返回错误
func TestUpdateVectorStoreOp_VectorStore未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	emb := &fakeEmbeddingService{embeddings: [][]float64{{0.1}}}
	sc.RegisterService("embedding_model", emb)

	op := NewUpdateVectorStoreOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("deduplicated_memories", []*ceschema.ReMeMemory{{WhenToUse: "w", Content: "c"}})
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "VectorStore not configured")
}

// ──────────────────────────── PersistMemoryOp 测试 ────────────────────────────

// TestPersistMemoryOp_正常持久化 测试正常持久化流程
func TestPersistMemoryOp_正常持久化(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	// 先写入一些节点
	node := &ceschema.ReMeMemory{BaseMemory: ceschema.BaseMemory{WorkspaceID: "user1"}, WhenToUse: "when1", Content: "content1"}
	vectorNode := node.ToVectorNode()
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

// TestPersistMemoryOp_空记忆 测试无记忆时返回空
func TestPersistMemoryOp_空记忆(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	helper := persistence.NewMemoryPersistenceHelper()
	op := NewPersistMemoryOp(sc, helper)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	persistCount, _ := cecontext.GetTyped[int](rc, "persist_count")
	assert.Equal(t, 0, persistCount)
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

// ──────────────────────────── 辅助函数测试 ────────────────────────────

// TestGetStringFromMap 测试 getStringFromMap 各种场景
func TestGetStringFromMap(t *testing.T) {
	m := map[string]any{"key": "value", "num": 42}

	assert.Equal(t, "value", getStringFromMap(m, "key", "default"))
	assert.Equal(t, "default", getStringFromMap(m, "missing", "default"))
	assert.Equal(t, "default", getStringFromMap(m, "num", "default"))
}

// TestGetFloatFromMap 测试 getFloatFromMap 各种场景
func TestGetFloatFromMap(t *testing.T) {
	m := map[string]any{"float": 3.14, "int": 42, "str": "hello"}

	assert.InDelta(t, 3.14, getFloatFromMap(m, "float", 0), 0.01)
	assert.InDelta(t, 42.0, getFloatFromMap(m, "int", 0), 0.01)
	assert.InDelta(t, 0.0, getFloatFromMap(m, "missing", 0), 0.01)
	assert.InDelta(t, 0.0, getFloatFromMap(m, "str", 0), 0.01)
}

// TestGetStringSliceFromMap 测试 getStringSliceFromMap 各种场景
func TestGetStringSliceFromMap(t *testing.T) {
	m := map[string]any{
		"str_slice": []string{"a", "b"},
		"any_slice": []any{"x", "y"},
		"num":       42,
	}

	assert.Equal(t, []string{"a", "b"}, getStringSliceFromMap(m, "str_slice"))
	assert.Equal(t, []string{"x", "y"}, getStringSliceFromMap(m, "any_slice"))
	assert.Nil(t, getStringSliceFromMap(m, "missing"))
	assert.Nil(t, getStringSliceFromMap(m, "num"))
}
