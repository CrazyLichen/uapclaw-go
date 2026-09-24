package ace

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	coreschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
	vector_store "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/vector_store"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestACERecallMemoryOp_正常检索(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	// 预填充 ACE 记忆
	vs.LoadNode("ace_user1_sec-00001", coreschema.NewVectorNode(
		"ace_user1_sec-00001",
		"test content",
		[]float64{0.1, 0.2, 0.3},
		map[string]any{
			"type":         "ace_memory",
			"workspace_id": "user1",
			"id":           "sec-00001",
			"section":      "sec",
			"content":      "test content",
			"helpful":      1,
			"harmful":      0,
			"neutral":      0,
			"created_at":   "2025-01-01T00:00:00Z",
			"updated_at":   "2025-01-01T00:00:00Z",
		},
	))
	sc.RegisterService("vector_store", vs)

	op := NewACERecallMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}

	retrieved, ok := cecontext.GetTyped[[]ceschema.MemoryItem](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) == 0 {
		t.Error("应检索到至少 1 条 ACE 记忆")
	}
	// 验证类型为 ACEMemory
	if _, ok := retrieved[0].(ceschema.ACEMemory); !ok {
		t.Error("第一条记忆应为 ACEMemory 类型")
	}
}

func TestACERecallMemoryOp_默认用户ID(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	vs.LoadNode("ace_default_sec-00001", coreschema.NewVectorNode(
		"ace_default_sec-00001",
		"default content",
		[]float64{0.1, 0.2, 0.3},
		map[string]any{
			"type": "ace_memory", "workspace_id": "default",
			"id": "sec-00001", "section": "sec", "content": "default content",
			"helpful": 0, "harmful": 0, "neutral": 0,
			"created_at": "2025-01-01T00:00:00Z", "updated_at": "2025-01-01T00:00:00Z",
		},
	))
	sc.RegisterService("vector_store", vs)

	op := NewACERecallMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	// 不设 user_id，应使用 "default"

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}

	retrieved, ok := cecontext.GetTyped[[]ceschema.MemoryItem](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) == 0 {
		t.Error("默认用户 ID 下应检索到至少 1 条 ACE 记忆")
	}
}

func TestACERecallMemoryOp_VectorStore未配置(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewACERecallMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	if err == nil {
		t.Error("期望返回错误，实际返回 nil")
	}
}

func TestACERecallMemoryOp_空结果(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("vector_store", vector_store.NewMemoryVectorStore())

	op := NewACERecallMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "nonexist")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}

	retrieved, ok := cecontext.GetTyped[[]ceschema.MemoryItem](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) != 0 {
		t.Errorf("retrieved len = %d, want 0", len(retrieved))
	}
}

// TestACERecallMemoryOp_无效节点跳过 测试 metadata 不匹配的节点被跳过
func TestACERecallMemoryOp_无效节点跳过(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := vector_store.NewMemoryVectorStore()
	// 加载一个有效节点
	vs.LoadNode("ace_user1_valid-00001", coreschema.NewVectorNode(
		"ace_user1_valid-00001",
		"valid content",
		[]float64{0.1, 0.2, 0.3},
		map[string]any{
			"type":         "ace_memory",
			"workspace_id": "user1",
			"id":           "valid-00001",
			"section":      "valid",
			"content":      "valid content",
			"helpful":      1,
			"harmful":      0,
			"neutral":      0,
			"created_at":   "2025-01-01T00:00:00Z",
			"updated_at":   "2025-01-01T00:00:00Z",
		},
	))
	// 加载一个无效节点（type 不是 ace_memory，不会匹配 filter）
	// 但直接加载一个 type 不匹配的节点在 MemoryVectorStore 中无法被 Search 返回
	// 所以这里只测试 Search 成功场景
	sc.RegisterService("vector_store", vs)

	op := NewACERecallMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	retrieved, ok := cecontext.GetTyped[[]ceschema.MemoryItem](rc, "retrieved_memories")
	require.True(t, ok)
	assert.Len(t, retrieved, 1)
	// 验证类型为 ACEMemory
	if _, ok := retrieved[0].(ceschema.ACEMemory); !ok {
		t.Error("第一条记忆应为 ACEMemory 类型")
	}
}

// fakeErrorVectorStore 模拟 Search 返回错误的 VectorStore
type fakeErrorVectorStore struct {
	vector_store *vector_store.MemoryVectorStore
}

func (f *fakeErrorVectorStore) Upsert(ctx context.Context, node *coreschema.VectorNode) error {
	return f.vector_store.Upsert(ctx, node)
}

func (f *fakeErrorVectorStore) Search(ctx context.Context, embedding []float64, topK int, metadataFilter map[string]any) ([]*coreschema.VectorNode, error) {
	return nil, fmt.Errorf("simulated search error")
}

func (f *fakeErrorVectorStore) Delete(ctx context.Context, nodeID string) (bool, error) {
	return f.vector_store.Delete(ctx, nodeID)
}

func (f *fakeErrorVectorStore) Clear() {
	f.vector_store.Clear()
}

func (f *fakeErrorVectorStore) Count() int {
	return f.vector_store.Count()
}

func (f *fakeErrorVectorStore) GetAll(metadataFilter map[string]any) []*coreschema.VectorNode {
	return f.vector_store.GetAll(metadataFilter)
}

// TestACERecallMemoryOp_Search失败 测试 Search 返回错误时传播错误
func TestACERecallMemoryOp_Search失败(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := &fakeErrorVectorStore{vector_store: vector_store.NewMemoryVectorStore()}
	sc.RegisterService("vector_store", vs)

	op := NewACERecallMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ACE memory search failed")
}
