package ace

import (
	"context"
	"testing"

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

	retrieved, ok := cecontext.GetTyped[[]ceschema.ACEMemory](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) == 0 {
		t.Error("应检索到至少 1 条 ACE 记忆")
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

	retrieved, ok := cecontext.GetTyped[[]ceschema.ACEMemory](rc, "retrieved_memories")
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

	retrieved, ok := cecontext.GetTyped[[]ceschema.ACEMemory](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) != 0 {
		t.Errorf("retrieved len = %d, want 0", len(retrieved))
	}
}
