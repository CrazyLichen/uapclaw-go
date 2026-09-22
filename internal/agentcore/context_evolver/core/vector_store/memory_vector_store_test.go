package vector_store

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
)

func TestNewMemoryVectorStore(t *testing.T) {
	s := NewMemoryVectorStore()
	assert.NotNil(t, s)
	assert.Equal(t, 0, s.Count())
}

func TestMemoryVectorStore_Upsert_Search(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()

	// 插入两个节点
	n1 := schema.NewVectorNode("1", "hello world", []float64{1, 0, 0}, map[string]any{"ws": "a"})
	n2 := schema.NewVectorNode("2", "goodbye world", []float64{0, 1, 0}, map[string]any{"ws": "a"})

	require.NoError(t, s.Upsert(ctx, n1))
	require.NoError(t, s.Upsert(ctx, n2))
	assert.Equal(t, 2, s.Count())

	// 搜索与 [1,0,0] 最相似的 → 应该返回 n1
	results, err := s.Search(ctx, []float64{1, 0, 0}, 1, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "1", results[0].ID)
}

func TestMemoryVectorStore_Search_metadataFilter(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()

	n1 := schema.NewVectorNode("1", "a", []float64{1, 0}, map[string]any{"ws": "alice"})
	n2 := schema.NewVectorNode("2", "b", []float64{0, 1}, map[string]any{"ws": "bob"})

	require.NoError(t, s.Upsert(ctx, n1))
	require.NoError(t, s.Upsert(ctx, n2))

	// 只搜索 ws=alice
	results, err := s.Search(ctx, []float64{0, 1}, 10, map[string]any{"ws": "alice"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "1", results[0].ID)
}

func TestMemoryVectorStore_Search_空库(t *testing.T) {
	s := NewMemoryVectorStore()
	results, err := s.Search(context.Background(), []float64{1, 0}, 5, nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestMemoryVectorStore_Upsert_无embedding报错(t *testing.T) {
	s := NewMemoryVectorStore()
	n := schema.NewVectorNode("1", "text", nil, nil)
	err := s.Upsert(context.Background(), n)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no embedding")
}

func TestMemoryVectorStore_Upsert_覆盖(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()

	n1 := schema.NewVectorNode("1", "old", []float64{1, 0}, nil)
	require.NoError(t, s.Upsert(ctx, n1))
	n2 := schema.NewVectorNode("1", "new", []float64{0, 1}, nil)
	require.NoError(t, s.Upsert(ctx, n2))

	assert.Equal(t, 1, s.Count())
	all := s.GetAll(nil)
	assert.Equal(t, "new", all[0].Content)
}

func TestMemoryVectorStore_Delete(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()

	n := schema.NewVectorNode("1", "text", []float64{1}, nil)
	require.NoError(t, s.Upsert(ctx, n))

	deleted, err := s.Delete(ctx, "1")
	require.NoError(t, err)
	assert.True(t, deleted)
	assert.Equal(t, 0, s.Count())

	deleted, err = s.Delete(ctx, "nonexistent")
	require.NoError(t, err)
	assert.False(t, deleted)
}

func TestMemoryVectorStore_Clear(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("1", "a", []float64{1}, nil)))
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("2", "b", []float64{2}, nil)))
	s.Clear()
	assert.Equal(t, 0, s.Count())
}

func TestMemoryVectorStore_GetAll(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("1", "a", []float64{1}, map[string]any{"ws": "x"})))
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("2", "b", []float64{2}, map[string]any{"ws": "y"})))

	all := s.GetAll(nil)
	assert.Len(t, all, 2)

	filtered := s.GetAll(map[string]any{"ws": "x"})
	assert.Len(t, filtered, 1)
	assert.Equal(t, "1", filtered[0].ID)
}

func TestMemoryVectorStore_LoadNode(t *testing.T) {
	s := NewMemoryVectorStore()
	// load_node 不校验 embedding，对齐 Python
	n := schema.NewVectorNode("1", "text", nil, nil)
	s.LoadNode("1", n)
	assert.Equal(t, 1, s.Count())
}

func TestMemoryVectorStore_LoadFromDict(t *testing.T) {
	s := NewMemoryVectorStore()
	data := map[string]map[string]any{
		"1": {"id": "1", "content": "hello", "embedding": []float64{1, 0}, "metadata": map[string]any{}},
		"2": {"id": "2", "content": "skip", "metadata": map[string]any{}}, // 无 embedding，应跳过
	}
	err := s.LoadFromDict(data)
	require.NoError(t, err)
	assert.Equal(t, 1, s.Count()) // 只有节点1被加载
}

func TestMemoryVectorStore_余弦相似度计算(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()

	// 三个向量：[1,0] [0,1] [0.707,0.707]
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("a", "a", []float64{1, 0}, nil)))
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("b", "b", []float64{0, 1}, nil)))
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("c", "c", []float64{1 / math.Sqrt2, 1 / math.Sqrt2}, nil)))

	// 查询 [1,0]，top2 应返回 a 和 c（c 余弦 ≈ 0.707）
	results, err := s.Search(ctx, []float64{1, 0}, 2, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "a", results[0].ID)
	assert.Equal(t, "c", results[1].ID)
}

func TestMemoryVectorStore_Search_零范数向量(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()

	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("zero", "zero", []float64{0, 0}, nil)))
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("normal", "normal", []float64{1, 0}, nil)))

	results, err := s.Search(ctx, []float64{1, 0}, 5, nil)
	require.NoError(t, err)
	// 零范数向量的相似度为 0，normal 应排前面
	require.True(t, len(results) >= 1)
	assert.Equal(t, "normal", results[0].ID)
}

func TestMemoryVectorStore_String(t *testing.T) {
	s := NewMemoryVectorStore()
	s.LoadNode("1", schema.NewVectorNode("1", "a", []float64{1}, nil))
	assert.Contains(t, s.String(), "1")
}
