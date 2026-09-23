package rb

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

// fakeEmbeddingService 模拟 Embedding 服务
type fakeEmbeddingService struct {
	embeddings [][]float64
	err        error
}

// ──────────────────────────── 导出方法 ────────────────────────────

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
	vs.LoadNode("rb_default_test1", coreschema.NewVectorNode(
		"rb_default_test1",
		"test query",
		[]float64{0.9, 0.1, 0.0},
		map[string]any{
			"type":         "reasoning_bank_memory",
			"workspace_id": "default",
			"query":        "test query",
			"memory": []any{
				map[string]any{
					"title":       "Strategy A",
					"description": "A good strategy",
					"content":     "Do X then Y",
				},
			},
		},
	))
	vs.LoadNode("rb_default_test2", coreschema.NewVectorNode(
		"rb_default_test2",
		"another query",
		[]float64{0.1, 0.9, 0.0},
		map[string]any{
			"type":         "reasoning_bank_memory",
			"workspace_id": "default",
			"query":        "another query",
			"memory": []any{
				map[string]any{
					"title":       "Strategy B",
					"description": "Another strategy",
					"content":     "Do Z then W",
				},
			},
		},
	))
	vs.LoadNode("rb_other_test", coreschema.NewVectorNode(
		"rb_other_test",
		"other query",
		[]float64{0.0, 0.1, 0.9},
		map[string]any{
			"type":         "reasoning_bank_memory",
			"workspace_id": "other_user",
			"query":        "other query",
			"memory": []any{
				map[string]any{
					"title":       "Strategy C",
					"description": "Other user strategy",
					"content":     "Do P then Q",
				},
			},
		},
	))
	sc.RegisterService("vector_store", vs)

	return sc
}

// newEmptyServiceContext 创建不含任何服务的 ServiceContext
func newEmptyServiceContext() *cecontext.ServiceContext {
	return cecontext.NewServiceContext()
}

// ──────────────────────────── RBRecallMemoryOp 测试 ────────────────────────────

// TestRBRecallMemoryOp_正常检索 测试正常 ReasoningBank 记忆检索
func TestRBRecallMemoryOp_正常检索(t *testing.T) {
	sc := newTestServiceContext()
	op := NewRBRecallMemoryOp(sc, 3)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("user_id", "default")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	retrieved, ok := cecontext.GetTyped[[]ceschema.ReasoningBankRetrievedMemory](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) == 0 {
		t.Fatal("应检索到记忆")
	}
	// 验证第一条记录内容
	if retrieved[0].Title != "Strategy A" {
		t.Fatalf("期望 Title='Strategy A'，实际 '%s'", retrieved[0].Title)
	}
	if retrieved[0].Description != "A good strategy" {
		t.Fatalf("期望 Description='A good strategy'，实际 '%s'", retrieved[0].Description)
	}
	if retrieved[0].Content != "Do X then Y" {
		t.Fatalf("期望 Content='Do X then Y'，实际 '%s'", retrieved[0].Content)
	}
}

// TestRBRecallMemoryOp_Embedding未注册 测试 Embedding 服务未注册
func TestRBRecallMemoryOp_Embedding未注册(t *testing.T) {
	sc := newEmptyServiceContext()
	op := NewRBRecallMemoryOp(sc, 1)
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

// TestRBRecallMemoryOp_VectorStore未注册 测试 VectorStore 服务未注册
func TestRBRecallMemoryOp_VectorStore未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("embedding_model", &fakeEmbeddingService{})
	op := NewRBRecallMemoryOp(sc, 1)
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

// TestRBRecallMemoryOp_默认UserID 测试 user_id 为空时使用 "default"
func TestRBRecallMemoryOp_默认UserID(t *testing.T) {
	sc := newTestServiceContext()
	op := NewRBRecallMemoryOp(sc, 3)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	// 不设置 user_id，应使用 "default"

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	retrieved, ok := cecontext.GetTyped[[]ceschema.ReasoningBankRetrievedMemory](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) == 0 {
		t.Fatal("默认 user_id='default' 时应检索到记忆")
	}
	// 应检索到 workspace_id="default" 的记忆，而非 "other_user" 的
	for _, mem := range retrieved {
		if mem.Title == "Strategy C" {
			t.Fatal("不应检索到 other_user 的记忆")
		}
	}
}

// TestRBRecallMemoryOp_向量节点转换失败 测试向量节点转换失败时 Warn 日志跳过
func TestRBRecallMemoryOp_向量节点转换失败(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("embedding_model", &fakeEmbeddingService{
		embeddings: [][]float64{{1.0, 0.0, 0.0}},
	})

	vs := vector_store.NewMemoryVectorStore()
	// 正常节点
	vs.LoadNode("rb_ok_node", coreschema.NewVectorNode(
		"rb_ok_node",
		"ok query",
		[]float64{0.9, 0.1, 0.0},
		map[string]any{
			"type":         "reasoning_bank_memory",
			"workspace_id": "default",
			"query":        "ok query",
			"memory": []any{
				map[string]any{
					"title":       "Good Strategy",
					"description": "Good desc",
					"content":     "Good content",
				},
			},
		},
	))
	// 空 memory 节点（转换后 Memory 为空，应跳过）
	vs.LoadNode("rb_empty_memory", coreschema.NewVectorNode(
		"rb_empty_memory",
		"empty query",
		[]float64{0.8, 0.2, 0.0},
		map[string]any{
			"type":         "reasoning_bank_memory",
			"workspace_id": "default",
			"query":        "empty query",
			// 不含 memory 字段
		},
	))
	sc.RegisterService("vector_store", vs)

	op := NewRBRecallMemoryOp(sc, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("user_id", "default")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	retrieved, ok := cecontext.GetTyped[[]ceschema.ReasoningBankRetrievedMemory](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	// 应只包含正常节点的记忆，空 memory 节点被跳过
	if len(retrieved) != 1 {
		t.Fatalf("期望 1 条结果，实际 %d", len(retrieved))
	}
	if retrieved[0].Title != "Good Strategy" {
		t.Fatalf("期望 Title='Good Strategy'，实际 '%s'", retrieved[0].Title)
	}
}

// TestNewRBRecallMemoryOp_默认TopK 测试 topK <= 0 时默认为 1
func TestNewRBRecallMemoryOp_默认TopK(t *testing.T) {
	sc := newTestServiceContext()
	op := NewRBRecallMemoryOp(sc, 0)
	if op.topK != 1 {
		t.Fatalf("期望 topK=1，实际 %d", op.topK)
	}

	op2 := NewRBRecallMemoryOp(sc, -5)
	if op2.topK != 1 {
		t.Fatalf("期望 topK=1，实际 %d", op2.topK)
	}

	op3 := NewRBRecallMemoryOp(sc, 5)
	if op3.topK != 5 {
		t.Fatalf("期望 topK=5，实际 %d", op3.topK)
	}
}

// TestRBRecallMemoryOp_多记忆条目 测试单个 VectorNode 包含多个 MemoryItem
func TestRBRecallMemoryOp_多记忆条目(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("embedding_model", &fakeEmbeddingService{
		embeddings: [][]float64{{1.0, 0.0, 0.0}},
	})

	vs := vector_store.NewMemoryVectorStore()
	vs.LoadNode("rb_multi_memory", coreschema.NewVectorNode(
		"rb_multi_memory",
		"multi query",
		[]float64{0.9, 0.1, 0.0},
		map[string]any{
			"type":         "reasoning_bank_memory",
			"workspace_id": "default",
			"query":        "multi query",
			"memory": []any{
				map[string]any{
					"title":       "Item 1",
					"description": "First item",
					"content":     "Content 1",
				},
				map[string]any{
					"title":       "Item 2",
					"description": "Second item",
					"content":     "Content 2",
				},
			},
		},
	))
	sc.RegisterService("vector_store", vs)

	op := NewRBRecallMemoryOp(sc, 5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "multi query")
	rc.Set("user_id", "default")

	err := op.Execute(context.Background(), rc)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	retrieved, ok := cecontext.GetTyped[[]ceschema.ReasoningBankRetrievedMemory](rc, "retrieved_memories")
	if !ok {
		t.Fatal("retrieved_memories 未设置")
	}
	if len(retrieved) != 2 {
		t.Fatalf("期望 2 条结果，实际 %d", len(retrieved))
	}
	if retrieved[0].Title != "Item 1" {
		t.Fatalf("期望 Title='Item 1'，实际 '%s'", retrieved[0].Title)
	}
	if retrieved[1].Title != "Item 2" {
		t.Fatalf("期望 Title='Item 2'，实际 '%s'", retrieved[1].Title)
	}
}

// TestRBRecallMemoryOp_Embedding失败 测试 Embedding 调用失败时返回错误
func TestRBRecallMemoryOp_Embedding失败(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("embedding_model", &fakeEmbeddingService{
		err: fmt.Errorf("embedding error"),
	})
	vs := vector_store.NewMemoryVectorStore()
	sc.RegisterService("vector_store", vs)

	op := NewRBRecallMemoryOp(sc, 1)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test")

	err := op.Execute(context.Background(), rc)
	if err == nil {
		t.Fatal("期望返回错误")
	}
	if err.Error() != "embedding query failed: embedding error" {
		t.Fatalf("错误消息不匹配: %v", err)
	}
}
