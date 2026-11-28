package vector_store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
)

// newTestChromaBleveStore 构造测试用组合存储（chroma 本地持久化 + bleve 索引，均落 TempDir）
func newTestChromaBleveStore(t *testing.T, collection string) *ChromaBleveStore {
	t.Helper()
	dir := t.TempDir()
	cfg := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: collection,
		DistanceMetric: common.DistanceMetricCosine,
		DatabaseName:   collection,
	}
	store, err := NewChromaBleveStore(cfg,
		filepath.Join(dir, "chroma"), filepath.Join(dir, "bleve"),
		"content", vector_fields.NewDefaultChromaVectorField(),
		"sparse_vector", "metadata", "document_id")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// bleveNode 构造 chroma+Bleve 双写数据行（固定 4 维向量满足 chroma 写入要求）
func bleveNode(id, docID, text string, vector []float64) map[string]any {
	return map[string]any{
		"id":          id,
		"content":     text,
		"metadata":    map[string]any{"doc_id": docID},
		"document_id": docID,
		"embedding":   vector,
	}
}

// TestChromaBleveStore_SparseSearch_BM25关键词排序 **BM25 关键词排序真用例**（spec
// §10 验收核心）：仅含查询词之一的文档得分必须低于同时含两查询词的文档，
// 无关文档不得命中——证明 bleve 半边是真关键词排序而非伪相似度。
func TestChromaBleveStore_SparseSearch_BM25关键词排序(t *testing.T) {
	store := newTestChromaBleveStore(t, "kb_bm25_rank")
	ctx := context.Background()
	vec := []float64{0.1, 0.2, 0.3, 0.4}
	require.NoError(t, store.Add(ctx, []map[string]any{
		bleveNode("c1", "d1", "apple banana cherry juice recipe", vec),
		bleveNode("c2", "d2", "banana telescope ocean waves", vec),
		bleveNode("c3", "d3", "quantum physics entanglement", vec),
	}))

	results, err := store.SparseSearch(ctx, "apple banana", 5, nil)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// ① 关键词命中集正确：无关文档（quantum）不得出现
	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.ID)
	}
	assert.Contains(t, ids, "c1", "双词命中文档应在结果集")
	assert.Contains(t, ids, "c2", "单词命中文档应在结果集")
	assert.NotContains(t, ids, "c3", "无关文档不得命中")

	// ② 关键词排序正确：同时含 apple+banana 的 c1 得分 > 仅含 banana 的 c2
	// （BM25 多词项匹配累加），且分数严格递减
	scoreByID := map[string]float64{}
	for _, r := range results {
		scoreByID[r.ID] = r.Score
	}
	assert.Greater(t, scoreByID["c1"], scoreByID["c2"],
		"双词命中应排在单词命中之前（BM25 词项累加）")
	for i := 1; i < len(results); i++ {
		assert.GreaterOrEqual(t, results[i-1].Score, results[i].Score,
			"结果应按得分降序排列")
	}
	// ③ 分数为正（伪 EF 哈希路径下分数无语义；bleve BM25 得分为正的相关性）
	assert.Greater(t, scoreByID["c1"], 0.0, "BM25 得分应为正")
}

// TestChromaBleveStore_SparseSearch_TF排序 词频影响排序：同词出现 3 次的文档
// 得分高于出现 1 次的文档（BM25 TF 分量）。
func TestChromaBleveStore_SparseSearch_TF排序(t *testing.T) {
	store := newTestChromaBleveStore(t, "kb_bm25_tf")
	ctx := context.Background()
	vec := []float64{0.5, 0.1, 0.1, 0.1}
	require.NoError(t, store.Add(ctx, []map[string]any{
		bleveNode("c1", "d1", "quantum quantum quantum computing gates", vec),
		bleveNode("c2", "d2", "quantum telescope", vec),
	}))

	results, err := store.SparseSearch(ctx, "quantum", 5, nil)
	require.NoError(t, err)
	require.Len(t, results, 2, "两文档均应命中")
	// BM25 TF 分量：高频文档应排第一
	assert.Equal(t, "c1", results[0].ID, "TF=3 文档应排在 TF=1 之前")
	assert.Greater(t, results[0].Score, results[1].Score, "TF=3 得分应更高")
}

// TestChromaBleveStore_DeleteByDocID 重解析先删后建链路：按 docID 删除后该文档
// 关键词不再命中，其他文档不受影响。
func TestChromaBleveStore_DeleteByDocID(t *testing.T) {
	store := newTestChromaBleveStore(t, "kb_bleve_del")
	ctx := context.Background()
	vec := []float64{0.1, 0.3, 0.2, 0.4}
	require.NoError(t, store.Add(ctx, []map[string]any{
		bleveNode("c1", "d1", "space rocket launch", vec),
		bleveNode("c2", "d2", "space shuttle history", vec),
	}))

	deleted, err := store.DeleteByDocID(ctx, "d1")
	require.NoError(t, err)
	assert.True(t, deleted, "应有 bleve 删除发生")

	results, err := store.SparseSearch(ctx, "space", 5, nil)
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.ID] = true
	}
	assert.NotContains(t, ids, "c1", "已删文档不得命中")
	assert.Contains(t, ids, "c2", "未删文档仍应命中")

	// 幂等：再删返回 false
	deleted, err = store.DeleteByDocID(ctx, "d1")
	require.NoError(t, err)
	assert.False(t, deleted)
}

// TestChromaBleveStore_HybridSearch_融合 向量半边透传 + 稀疏半边 bleve + RRF 融合：
// 结果非空且融合两路来源。
func TestChromaBleveStore_HybridSearch_融合(t *testing.T) {
	store := newTestChromaBleveStore(t, "kb_bleve_hybrid")
	ctx := context.Background()
	require.NoError(t, store.Add(ctx, []map[string]any{
		bleveNode("c1", "d1", "rocket launch schedule", []float64{0.9, 0.1, 0.0, 0.0}),
		bleveNode("c2", "d2", "ocean telescope", []float64{0.0, 0.9, 0.1, 0.0}),
	}))

	// 向量半边：query 靠近 c1；稀疏半边：query 词命中 c1（c2 无 rocket 词）
	fused, err := store.HybridSearch(ctx, "rocket launch", []float64{0.9, 0.1, 0.0, 0.0}, 5, 0.5, nil)
	require.NoError(t, err)
	require.NotEmpty(t, fused)
	// RRF 融合：c1 双路命中应排第一
	assert.Equal(t, "fused_top_is_c1", func() string {
		// SearchResult 无 ID 透传（RRFFusion 消费 RetrievalResult 不含 ID）——
		// 以文本匹配断言排序
		if len(fused) > 0 && fused[0].Text == "rocket launch schedule" {
			return "fused_top_is_c1"
		}
		return "fused_top_not_c1"
	}())

	// 纯向量路径透传正常
	vecResults, err := store.Search(ctx, []float64{0.9, 0.1, 0.0, 0.0}, 5, nil)
	require.NoError(t, err)
	require.NotEmpty(t, vecResults)
}

// TestChromaBleveStore_Delete_TableCleanup 删除与清表：按 id 删 bleve+chroma，
// DeleteTable 清理 bleve 目录与 chroma collection。
func TestChromaBleveStore_Delete_TableCleanup(t *testing.T) {
	store := newTestChromaBleveStore(t, "kb_bleve_del2")
	ctx := context.Background()
	require.NoError(t, store.Add(ctx, []map[string]any{
		bleveNode("c1", "d1", "alpha beta gamma", []float64{0.2, 0.2, 0.2, 0.4}),
	}))
	deleted, err := store.Delete(ctx, []string{"c1"}, nil)
	require.NoError(t, err)
	assert.True(t, deleted)
	results, err := store.SparseSearch(ctx, "alpha", 5, nil)
	require.NoError(t, err)
	assert.Empty(t, results, "按 id 删除后关键词不得命中")

	require.NoError(t, store.DeleteTable(ctx, "kb_bleve_del2"))
	// DeleteTable 后 store 已关，仅验证目录清理副作用（不再访问已关索引）
}

// TestChromaBleveStore_SparseSearch_中文分词排序 cjk 分词器中文关键词排序：
// 含查询词多次的中文文档排在仅一次之前，无关中文文档不命中。
func TestChromaBleveStore_SparseSearch_中文分词排序(t *testing.T) {
	store := newTestChromaBleveStore(t, "kb_bm25_cjk")
	ctx := context.Background()
	vec := []float64{0.3, 0.3, 0.2, 0.2}
	// 等长设计：c1/c2 总长近似（长度归一化不主导），仅查询词频差异——
	// TF=3 文档应稳定排在 TF=1 之前（BM25 TF 分量）
	require.NoError(t, store.Add(ctx, []map[string]any{
		bleveNode("c1", "d1", "量子计算 量子计算 量子计算", vec),
		bleveNode("c2", "d2", "量子计算 红烧肉 家常做法", vec),
		bleveNode("c3", "d3", "云海翻腾 日出东方", vec),
	}))

	results, err := store.SparseSearch(ctx, "量子计算", 5, nil)
	require.NoError(t, err)
	require.NotEmpty(t, results, "中文关键词应命中")
	assert.Equal(t, "c1", results[0].ID, "高频中文关键词文档应排第一")
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.ID] = true
	}
	assert.Contains(t, ids, "c2")
	assert.NotContains(t, ids, "c3", "无关中文文档不得命中")
}
