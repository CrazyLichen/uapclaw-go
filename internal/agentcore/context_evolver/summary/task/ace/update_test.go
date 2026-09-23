package ace

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	coreschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
	cepersistence "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/persistence"
	vector_store "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/vector_store"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeLLMService LLMService 的 mock 实现
type fakeLLMService struct {
	// response 模拟返回的响应
	response string
	// err 模拟返回的错误
	err error
}

func (f *fakeLLMService) Generate(_ context.Context, _ string, _ ...cecontext.GenerateOption) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

// fakeEmbeddingService EmbeddingService 的 mock 实现
type fakeEmbeddingService struct {
	embeddings [][]float64
	err        error
}

func (f *fakeEmbeddingService) Embed(_ context.Context, _ string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.embeddings) > 0 {
		return f.embeddings[0], nil
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

func (f *fakeEmbeddingService) EmbedBatch(_ context.Context, _ []string) ([][]float64, error) {
	return f.embeddings, f.err
}

// makeReflectionJSON 构造 Reflector 返回的 JSON 响应
func makeReflectionJSON() string {
	obj := map[string]any{
		"reasoning":           "The agent made a pagination error.",
		"error_identification": "Used fixed range loop.",
		"root_cause_analysis":  "Misunderstood pagination pattern.",
		"correct_approach":     "Use while True loop.",
		"key_insight":          "Always use while True for pagination.",
	}
	data, _ := json.Marshal(obj)
	return string(data)
}

// makeCurationJSON 构造 Curator 返回的 JSON 响应（含 DeltaBatch）
func makeCurationJSON() string {
	obj := map[string]any{
		"reasoning": "Need to add pagination insight.",
		"operations": []any{
			map[string]any{
				"type":    "ADD",
				"section": "strategies_and_hard_rules",
				"content": "Always use while True for pagination.",
			},
		},
	}
	data, _ := json.Marshal(obj)
	return string(data)
}

// makeCurationWithTagJSON 构造含 TAG 操作的 Curator 响应
func makeCurationWithTagJSON(bulletID string) string {
	obj := map[string]any{
		"reasoning": "Existing bullet was helpful.",
		"operations": []any{
			map[string]any{
				"type":      "TAG",
				"bullet_id": bulletID,
				"metadata":  map[string]any{"helpful": 1, "harmful": 0},
			},
		},
	}
	data, _ := json.Marshal(obj)
	return string(data)
}

// makeCurationWithRemoveJSON 构造含 REMOVE 操作的 Curator 响应
func makeCurationWithRemoveJSON(bulletID string) string {
	obj := map[string]any{
		"reasoning": "Remove outdated bullet.",
		"operations": []any{
			map[string]any{
				"type":      "REMOVE",
				"bullet_id": bulletID,
			},
		},
	}
	data, _ := json.Marshal(obj)
	return string(data)
}

// makeCurationWithUpdateJSON 构造含 UPDATE 操作的 Curator 响应
func makeCurationWithUpdateJSON(bulletID, newContent string) string {
	obj := map[string]any{
		"reasoning": "Update existing bullet.",
		"operations": []any{
			map[string]any{
				"type":      "UPDATE",
				"bullet_id": bulletID,
				"content":   newContent,
				"metadata":  map[string]any{"helpful": 1},
			},
		},
	}
	data, _ := json.Marshal(obj)
	return string(data)
}

// setupServiceContext 创建带 LLM/Embedding/VectorStore 的 ServiceContext
func setupServiceContext(llmResponse string) *cecontext.ServiceContext {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{response: llmResponse})
	sc.RegisterService("embedding_model", &fakeEmbeddingService{
		embeddings: [][]float64{{0.1, 0.2, 0.3}},
	})
	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)
	return sc
}

// ──────────────────────────── LoadPlaybookOp 测试 ────────────────────────────

// TestLoadPlaybookOp_正常加载 测试从 vector store 加载 playbook
func TestLoadPlaybookOp_正常加载(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	vs.LoadNode("ace_user1_strategies-00001", coreschema.NewVectorNode(
		"ace_user1_strategies-00001",
		"use while True for pagination",
		[]float64{0.1, 0.2, 0.3},
		map[string]any{
			"type":         "ace_memory",
			"workspace_id": "user1",
			"id":           "strategies-00001",
			"section":      "strategies",
			"content":      "use while True for pagination",
			"helpful":      2,
			"harmful":      0,
			"neutral":      1,
			"created_at":   "2025-01-01T00:00:00Z",
			"updated_at":   "2025-01-02T00:00:00Z",
		},
	))
	sc.RegisterService("vector_store", vs)

	op := NewLoadPlaybookOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	playbook, ok := rc.Get("playbook").(*Playbook)
	require.True(t, ok, "playbook 应设置到 rc")
	require.NotNil(t, playbook)
	assert.Equal(t, 1, len(playbook.Bullets()))

	bullet := playbook.GetBullet("strategies-00001")
	require.NotNil(t, bullet)
	assert.Equal(t, "use while True for pagination", bullet.Content)
	assert.Equal(t, 2, bullet.Helpful)
}

// TestLoadPlaybookOp_空Playbook 测试无已有记忆时返回空 Playbook
func TestLoadPlaybookOp_空Playbook(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	op := NewLoadPlaybookOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	playbook, ok := rc.Get("playbook").(*Playbook)
	require.True(t, ok)
	require.NotNil(t, playbook)
	assert.Equal(t, 0, len(playbook.Bullets()))
}

// TestLoadPlaybookOp_VectorStore未配置 测试 VectorStore 未注册时返回错误
func TestLoadPlaybookOp_VectorStore未配置(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewLoadPlaybookOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vector store not configured")
}

// TestLoadPlaybookOp_默认用户ID 测试 user_id 缺失时使用 "default"
func TestLoadPlaybookOp_默认用户ID(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	vs.LoadNode("ace_default_general-00001", coreschema.NewVectorNode(
		"ace_default_general-00001",
		"default content",
		[]float64{0.1, 0.2},
		map[string]any{
			"type": "ace_memory", "workspace_id": "default",
			"id": "general-00001", "section": "general", "content": "default content",
			"helpful": 0, "harmful": 0, "neutral": 0,
			"created_at": "2025-01-01T00:00:00Z", "updated_at": "2025-01-01T00:00:00Z",
		},
	))
	sc.RegisterService("vector_store", vs)

	op := NewLoadPlaybookOp(sc)
	rc := cecontext.NewRuntimeContext()
	// 不设 user_id

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	playbook, ok := rc.Get("playbook").(*Playbook)
	require.True(t, ok)
	assert.Equal(t, 1, len(playbook.Bullets()))
}

// ──────────────────────────── ReflectOp 测试 ────────────────────────────

// TestReflectOp_matts为none 测试 matts="none" 时正常执行
func TestReflectOp_matts为none(t *testing.T) {
	sc := setupServiceContext(makeReflectionJSON())

	op := NewReflectOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")
	rc.Set("trajectories", []string{"step A → step B"})
	rc.Set("ground_truth", "correct answer")
	rc.Set("feedback", []string{"test passed"})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	reflection, ok := rc.Get("reflection").(map[string]any)
	require.True(t, ok, "reflection 应设置到 rc")
	assert.Contains(t, reflection, "reasoning")
}

// TestReflectOp_matts为sequential 测试 matts="sequential" 时正常执行
func TestReflectOp_matts为sequential(t *testing.T) {
	sc := setupServiceContext(makeReflectionJSON())

	op := NewReflectOp(sc, false)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "sequential")
	rc.Set("trajectories", []string{"step A → step B"})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	reflection, ok := rc.Get("reflection").(map[string]any)
	require.True(t, ok)
	assert.Contains(t, reflection, "reasoning")
}

// TestReflectOp_matts不匹配 测试 matts 不匹配时跳过
func TestReflectOp_matts不匹配(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewReflectOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "parallel")
	rc.Set("trajectories", []string{"step A"})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	// 不匹配时 reflection 不应被设置
	_, ok := rc.Get("reflection").(map[string]any)
	assert.False(t, ok)
}

// TestReflectOp_无轨迹 测试轨迹为空时设置空 reflection
func TestReflectOp_无轨迹(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{})

	op := NewReflectOp(sc, false)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")
	rc.Set("trajectories", []string{})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	reflection, ok := rc.Get("reflection").(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 0, len(reflection))
}

// TestReflectOp_LLM未配置 测试 LLM 未注册时返回错误
func TestReflectOp_LLM未配置(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewReflectOp(sc, false)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")
	rc.Set("trajectories", []string{"step A"})

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM not configured")
}

// TestReflectOp_LLM调用失败 测试 LLM 调用失败时返回错误
func TestReflectOp_LLM调用失败(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{err: assert.AnError})

	op := NewReflectOp(sc, false)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")
	rc.Set("trajectories", []string{"step A"})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM 调用失败")
}

// TestReflectOp_无GroundTruth 测试无 ground truth 时使用 ReflectorNoGT prompt
func TestReflectOp_无GroundTruth(t *testing.T) {
	sc := setupServiceContext(makeReflectionJSON())

	op := NewReflectOp(sc, false)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")
	rc.Set("trajectories", []string{"step A"})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	reflection, ok := rc.Get("reflection").(map[string]any)
	require.True(t, ok)
	assert.Contains(t, reflection, "reasoning")
}

// ──────────────────────────── ParallelReflectOp 测试 ────────────────────────────

// TestParallelReflectOp_matts为parallel 测试 matts="parallel" 时正常执行
func TestParallelReflectOp_matts为parallel(t *testing.T) {
	sc := setupServiceContext(makeReflectionJSON())

	op := NewParallelReflectOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "parallel")
	rc.Set("trajectories", []string{"step A", "step B"})
	rc.Set("ground_truth", "correct answer")
	rc.Set("feedback", []string{"report A", "report B"})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	reflection, ok := rc.Get("reflection").(map[string]any)
	require.True(t, ok)
	assert.Contains(t, reflection, "reasoning")
}

// TestParallelReflectOp_matts为combined 测试 matts="combined" 时正常执行
func TestParallelReflectOp_matts为combined(t *testing.T) {
	sc := setupServiceContext(makeReflectionJSON())

	op := NewParallelReflectOp(sc, false)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "combined")
	rc.Set("trajectories", []string{"step A", "step B"})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	reflection, ok := rc.Get("reflection").(map[string]any)
	require.True(t, ok)
	_ = reflection // 仅验证类型断言成功
}

// TestParallelReflectOp_matts不匹配 测试 matts 不匹配时跳过
func TestParallelReflectOp_matts不匹配(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewParallelReflectOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	_, ok := rc.Get("reflection").(map[string]any)
	assert.False(t, ok)
}

// TestParallelReflectOp_轨迹不足 测试轨迹不足 2 条时设置空 reflection
func TestParallelReflectOp_轨迹不足(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{})

	op := NewParallelReflectOp(sc, true)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "parallel")
	rc.Set("trajectories", []string{"only one"})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	reflection, ok := rc.Get("reflection").(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 0, len(reflection))
}

// ──────────────────────────── CurateOp 测试 ────────────────────────────

// TestCurateOp_matts为none 测试 matts="none" 时正常执行策展
func TestCurateOp_matts为none(t *testing.T) {
	sc := setupServiceContext(makeCurationJSON())

	op := NewCurateOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")
	rc.Set("query", "test query")
	rc.Set("trajectories", []string{"step A"})
	rc.Set("reflection", map[string]any{
		"reasoning": "The agent made an error.",
	})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	delta, ok := rc.Get("delta").(*DeltaBatch)
	require.True(t, ok, "delta 应设置到 rc")
	assert.NotEmpty(t, delta.Reasoning)
	assert.Len(t, delta.Operations, 1)
	assert.Equal(t, OperationAdd, delta.Operations[0].Type)
}

// TestCurateOp_matts不匹配 测试 matts 不匹配时跳过
func TestCurateOp_matts不匹配(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewCurateOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "parallel")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	_, ok := rc.Get("delta").(*DeltaBatch)
	assert.False(t, ok)
}

// TestCurateOp_无reflection 测试 reflection 为空时设置空 DeltaBatch
func TestCurateOp_无reflection(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{})

	op := NewCurateOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")
	rc.Set("reflection", map[string]any{})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	delta, ok := rc.Get("delta").(*DeltaBatch)
	require.True(t, ok)
	assert.Empty(t, delta.Operations)
}

// TestCurateOp_LLM未配置 测试 LLM 未注册时返回错误
func TestCurateOp_LLM未配置(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewCurateOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")
	rc.Set("reflection", map[string]any{"reasoning": "test"})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM not configured")
}

// ──────────────────────────── ParallelCurateOp 测试 ────────────────────────────

// TestParallelCurateOp_matts为parallel 测试 matts="parallel" 时正常执行
func TestParallelCurateOp_matts为parallel(t *testing.T) {
	sc := setupServiceContext(makeCurationJSON())

	op := NewParallelCurateOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "parallel")
	rc.Set("query", "parallel query")
	rc.Set("trajectories", []string{"step A", "step B"})
	rc.Set("reflection", map[string]any{
		"reasoning": "Parallel reflection result.",
	})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	delta, ok := rc.Get("delta").(*DeltaBatch)
	require.True(t, ok)
	assert.Len(t, delta.Operations, 1)
}

// TestParallelCurateOp_matts为combined 测试 matts="combined" 时正常执行
func TestParallelCurateOp_matts为combined(t *testing.T) {
	sc := setupServiceContext(makeCurationJSON())

	op := NewParallelCurateOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "combined")
	rc.Set("query", "combined query")
	rc.Set("trajectories", []string{"step A", "step B"})
	rc.Set("reflection", map[string]any{"reasoning": "test"})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	delta, ok := rc.Get("delta").(*DeltaBatch)
	require.True(t, ok)
	_ = delta // 仅验证类型断言成功
}

// TestParallelCurateOp_matts不匹配 测试 matts 不匹配时跳过
func TestParallelCurateOp_matts不匹配(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewParallelCurateOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "none")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	_, ok := rc.Get("delta").(*DeltaBatch)
	assert.False(t, ok)
}

// TestParallelCurateOp_轨迹不足 测试轨迹不足 2 条时设置空 DeltaBatch
func TestParallelCurateOp_轨迹不足(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{})

	op := NewParallelCurateOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("matts", "parallel")
	rc.Set("trajectories", []string{"only one"})
	rc.Set("reflection", map[string]any{"reasoning": "test"})
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	delta, ok := rc.Get("delta").(*DeltaBatch)
	require.True(t, ok)
	assert.Empty(t, delta.Operations)
}

// ──────────────────────────── ApplyDeltaOp 测试 ────────────────────────────

// TestApplyDeltaOp_ADD操作 测试 ADD 操作正常应用
func TestApplyDeltaOp_ADD操作(t *testing.T) {
	sc := setupServiceContext("")

	op := NewApplyDeltaOp(sc, 50)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	playbook := NewPlaybook()
	rc.Set("playbook", playbook)

	delta := &DeltaBatch{
		Reasoning: "Add pagination insight",
		Operations: []DeltaOperation{
			{
				Type:    OperationAdd,
				Section: "strategies_and_hard_rules",
				Content: ptrStr("Always use while True for pagination."),
			},
		},
	}
	rc.Set("delta", delta)

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]ceschema.ACEMemory](rc, "memories")
	require.True(t, ok, "memories 应设置到 rc")
	assert.Len(t, memories, 1)
	assert.Equal(t, "Always use while True for pagination.", memories[0].Content)

	// 验证 playbook 中也添加了 bullet
	assert.Equal(t, 1, len(playbook.Bullets()))
}

// TestApplyDeltaOp_TAG操作 测试 TAG 操作正常应用
func TestApplyDeltaOp_TAG操作(t *testing.T) {
	sc := setupServiceContext("")

	playbook := NewPlaybook()
	bullet := playbook.AddBullet("strategies", "test content", nil, nil)
	require.NotNil(t, bullet)

	op := NewApplyDeltaOp(sc, 50)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")
	rc.Set("playbook", playbook)

	delta := &DeltaBatch{
		Reasoning: "Tag existing bullet",
		Operations: []DeltaOperation{
			{
				Type:     OperationTag,
				BulletID: ptrStr(bullet.ID),
				Metadata: map[string]int{"helpful": 1, "harmful": 0},
			},
		},
	}
	rc.Set("delta", delta)

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	// 验证 bullet 标记已更新
	updated := playbook.GetBullet(bullet.ID)
	require.NotNil(t, updated)
	assert.Equal(t, 1, updated.Helpful)
}

// TestApplyDeltaOp_REMOVE操作 测试 REMOVE 操作正常应用
func TestApplyDeltaOp_REMOVE操作(t *testing.T) {
	sc := setupServiceContext("")

	playbook := NewPlaybook()
	bullet := playbook.AddBullet("strategies", "to be removed", nil, nil)
	require.NotNil(t, bullet)

	op := NewApplyDeltaOp(sc, 50)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")
	rc.Set("playbook", playbook)

	delta := &DeltaBatch{
		Reasoning: "Remove outdated bullet",
		Operations: []DeltaOperation{
			{
				Type:     OperationRemove,
				BulletID: ptrStr(bullet.ID),
			},
		},
	}
	rc.Set("delta", delta)

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	// 验证 bullet 已删除
	assert.Nil(t, playbook.GetBullet(bullet.ID))
}

// TestApplyDeltaOp_UPDATE操作 测试 UPDATE 操作正常应用
func TestApplyDeltaOp_UPDATE操作(t *testing.T) {
	sc := setupServiceContext("")

	playbook := NewPlaybook()
	bullet := playbook.AddBullet("strategies", "old content", nil, nil)
	require.NotNil(t, bullet)

	op := NewApplyDeltaOp(sc, 50)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")
	rc.Set("playbook", playbook)

	newContent := "updated content"
	delta := &DeltaBatch{
		Reasoning: "Update existing bullet",
		Operations: []DeltaOperation{
			{
				Type:     OperationUpdate,
				BulletID: ptrStr(bullet.ID),
				Content:  &newContent,
				Metadata: map[string]int{"helpful": 1},
			},
		},
	}
	rc.Set("delta", delta)

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	// 验证 bullet 已更新
	updated := playbook.GetBullet(bullet.ID)
	require.NotNil(t, updated)
	assert.Equal(t, "updated content", updated.Content)
	assert.Equal(t, 1, updated.Helpful)
}

// TestApplyDeltaOp_UPDATE降级为ADD 测试 UPDATE 找不到 bullet 时降级为 ADD
func TestApplyDeltaOp_UPDATE降级为ADD(t *testing.T) {
	sc := setupServiceContext("")

	playbook := NewPlaybook()

	op := NewApplyDeltaOp(sc, 50)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")
	rc.Set("playbook", playbook)

	delta := &DeltaBatch{
		Reasoning: "UPDATE missing bullet, fallback to ADD",
		Operations: []DeltaOperation{
			{
				Type:     OperationUpdate,
				Section:  "strategies_and_hard_rules",
				BulletID: ptrStr("nonexistent-00001"),
				Content:  ptrStr("fallback content"),
			},
		},
	}
	rc.Set("delta", delta)

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	// 验证新 bullet 已创建（降级为 ADD）
	assert.Equal(t, 1, len(playbook.Bullets()))
}

// TestApplyDeltaOp_eviction驱逐 测试超过 maxBullets 时驱逐低分 bullet
func TestApplyDeltaOp_eviction驱逐(t *testing.T) {
	sc := setupServiceContext("")

	playbook := NewPlaybook()
	// 添加 3 个已有 bullet（低分）
	for i := 0; i < 3; i++ {
		bullet := playbook.AddBullet("strategies", "low score content", nil, nil)
		require.NotNil(t, bullet)
		// 设置低分
		bullet.Helpful = 0
		bullet.Harmful = 1
	}

	op := NewApplyDeltaOp(sc, 3) // maxBullets = 3
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")
	rc.Set("playbook", playbook)

	// 添加 2 个新 ADD，会超过 maxBullets
	delta := &DeltaBatch{
		Reasoning: "Add new bullets",
		Operations: []DeltaOperation{
			{Type: OperationAdd, Section: "strategies", Content: ptrStr("new 1")},
			{Type: OperationAdd, Section: "strategies", Content: ptrStr("new 2")},
		},
	}
	rc.Set("delta", delta)

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	// 总共 3(existing) + 2(add) - 2(removeCount) = 3 bullets
	assert.Equal(t, 3, len(playbook.Bullets()))
}

// TestApplyDeltaOp_无Delta 测试无 delta 时直接返回
func TestApplyDeltaOp_无Delta(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewApplyDeltaOp(sc, 50)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")
	rc.Set("playbook", NewPlaybook())

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]ceschema.ACEMemory](rc, "memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}

// TestApplyDeltaOp_VectorStore未配置 测试 VectorStore 未注册时返回错误
func TestApplyDeltaOp_VectorStore未配置(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("embedding_model", &fakeEmbeddingService{})

	op := NewApplyDeltaOp(sc, 50)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")
	rc.Set("playbook", NewPlaybook())
	rc.Set("delta", &DeltaBatch{
		Reasoning:  "test",
		Operations: []DeltaOperation{{Type: OperationAdd, Section: "s", Content: ptrStr("c")}},
	})

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vector store not configured")
}

// TestApplyDeltaOp_EmbeddingModel未配置 测试 EmbeddingModel 未注册时返回错误
func TestApplyDeltaOp_EmbeddingModel未配置(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	op := NewApplyDeltaOp(sc, 50)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")
	rc.Set("playbook", NewPlaybook())
	rc.Set("delta", &DeltaBatch{
		Reasoning:  "test",
		Operations: []DeltaOperation{{Type: OperationAdd, Section: "s", Content: ptrStr("c")}},
	})

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "embedding model not configured")
}

// TestApplyDeltaOp_默认maxBullets 测试 maxBullets<=0 时使用默认值 50
func TestApplyDeltaOp_默认maxBullets(t *testing.T) {
	op := NewApplyDeltaOp(cecontext.NewServiceContext(), 0)
	assert.Equal(t, 50, op.maxBullets)

	op2 := NewApplyDeltaOp(cecontext.NewServiceContext(), -1)
	assert.Equal(t, 50, op2.maxBullets)
}

// ──────────────────────────── PersistMemoryOp 测试 ────────────────────────────

// TestPersistMemoryOp_正常持久化 测试正常持久化流程
func TestPersistMemoryOp_正常持久化(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	// 预填充 ACE 记忆
	mem := ceschema.ACEMemory{
		BaseMemory: ceschema.BaseMemory{WorkspaceID: "user1"},
		ID:         "strategies-00001",
		Section:    "strategies",
		Content:    "test content",
		Helpful:    2,
		Harmful:    0,
		Neutral:    1,
	}
	vectorNode := mem.ToVectorNode()
	vectorNode.Embedding = []float64{0.1, 0.2}
	vs.Upsert(context.Background(), vectorNode)

	helper := cepersistence.NewMemoryPersistenceHelper(
		cepersistence.WithPersistPath(t.TempDir() + "/{algo_name}/{user_id}.json"),
	)

	op := NewPersistMemoryOp(sc, helper)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	persistCount, ok := cecontext.GetTyped[int](rc, "persist_count")
	require.True(t, ok)
	assert.Equal(t, 1, persistCount)
}

// TestPersistMemoryOp_VectorStore未配置 测试 VectorStore 未注册时返回错误
func TestPersistMemoryOp_VectorStore未配置(t *testing.T) {
	sc := cecontext.NewServiceContext()
	helper := cepersistence.NewMemoryPersistenceHelper()
	op := NewPersistMemoryOp(sc, helper)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vector store not configured")
}

// TestPersistMemoryOp_无记忆 测试无记忆时直接返回
func TestPersistMemoryOp_无记忆(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	op := NewPersistMemoryOp(sc, nil)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	persistCount, ok := cecontext.GetTyped[int](rc, "persist_count")
	require.True(t, ok)
	assert.Equal(t, 0, persistCount)
}

// TestPersistMemoryOp_无Helper 测试 helper 为 nil 时不持久化但正常完成
func TestPersistMemoryOp_无Helper(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	// 预填充
	mem := ceschema.ACEMemory{
		BaseMemory: ceschema.BaseMemory{WorkspaceID: "user1"},
		ID:         "test-00001",
		Section:    "test",
		Content:    "test",
	}
	vectorNode := mem.ToVectorNode()
	vectorNode.Embedding = []float64{0.1}
	vs.Upsert(context.Background(), vectorNode)

	op := NewPersistMemoryOp(sc, nil) // helper = nil
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	persistCount, ok := cecontext.GetTyped[int](rc, "persist_count")
	require.True(t, ok)
	assert.Equal(t, 1, persistCount) // 统计了节点但未真正持久化
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// ptrStr 返回字符串指针
func ptrStr(s string) *string {
	return &s
}
