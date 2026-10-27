package retriever

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// newHybridFixture 构建装配好的 Hybrid 桩与检索器（Python mock fixture 对应物）
func newHybridFixture() (*HybridRetriever, *fakeVectorStore, *fakeEmbedModel) {
	store := &fakeVectorStore{
		hybridResult: []common.SearchResult{
			{ID: "1", Text: "Hybrid result 1", Score: 0.95, Metadata: map[string]any{"doc_id": "doc_1"}},
			{ID: "2", Text: "Hybrid result 2", Score: 0.85, Metadata: map[string]any{"doc_id": "doc_2"}},
		},
		searchResult: []common.SearchResult{
			{ID: "1", Text: "Vector result", Score: 0.9, Metadata: map[string]any{}},
		},
		sparseResult: []common.SearchResult{
			{ID: "1", Text: "Sparse result", Score: 0.8, Metadata: map[string]any{}},
		},
	}
	// 对齐 Python [0.1] * 384（mock_embed_model fixture）
	queryVec := make([]float64, 384)
	for i := range queryVec {
		queryVec[i] = 0.1
	}
	embed := &fakeEmbedModel{queryVec: queryVec}
	r := NewHybridRetriever(store, embed)
	return r, store, embed
}

// newHybridFixture0 指定构造 alpha 的 Hybrid 桩（alpha 覆写/回落用例）
func newHybridFixture0(alpha float64) (*HybridRetriever, *fakeVectorStore, *fakeEmbedModel) {
	store := &fakeVectorStore{
		hybridResult: []common.SearchResult{
			{ID: "1", Text: "Hybrid result 1", Score: 0.95, Metadata: map[string]any{"doc_id": "doc_1"}},
		},
	}
	embed := &fakeEmbedModel{queryVec: []float64{0.1}}
	r := NewHybridRetriever(store, embed, WithHybridAlpha(alpha))
	return r, store, embed
}

// TestHybridRetriever_Retrieve_hybrid模式 对齐 test_retrieve_hybrid_mode
func TestHybridRetriever_Retrieve_hybrid模式(t *testing.T) {
	r, store, embed := newHybridFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, 1, store.hybridCalls)
	assert.Equal(t, 1, embed.embedQueryCalls)
}

// TestHybridRetriever_Retrieve_vector模式 对齐 test_retrieve_vector_mode
func TestHybridRetriever_Retrieve_vector模式(t *testing.T) {
	r, store, _ := newHybridFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeVector)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 1, store.searchCalls)
}

// TestHybridRetriever_Retrieve_sparse模式 对齐 test_retrieve_sparse_mode（无 embed 构造）
func TestHybridRetriever_Retrieve_sparse模式(t *testing.T) {
	store := &fakeVectorStore{
		sparseResult: []common.SearchResult{
			{ID: "1", Text: "Sparse result", Score: 0.8, Metadata: map[string]any{}},
		},
	}
	r := NewHybridRetriever(store, nil)
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 1, store.sparseCalls)
}

// TestHybridRetriever_Retrieve_alpha覆写 对齐 test_retrieve_with_custom_alpha
// （构造 0.7 + 调用 0.8 → store 收到 0.8：调用覆写优先于构造默认）
func TestHybridRetriever_Retrieve_alpha覆写(t *testing.T) {
	r, store, _ := newHybridFixture0(0.7)
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeHybrid, WithRetrieveAlpha(0.8))
	require.NoError(t, err)
	assert.Equal(t, 0.8, store.lastAlpha)
}

// TestHybridRetriever_Retrieve_vector模式无embed 对齐 test_retrieve_vector_mode_without_embed_model
func TestHybridRetriever_Retrieve_vector模式无embed(t *testing.T) {
	r := NewHybridRetriever(&fakeVectorStore{}, nil)
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeVector)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155202, be.Code())
	assert.Contains(t, be.Message(), "embed_model is required")
}

// TestHybridRetriever_Retrieve_阈值过滤 对齐 test_retrieve_with_score_threshold
// （vector 模式 0.95/0.85 阈值 0.85 → 全保留，>= 边界）
func TestHybridRetriever_Retrieve_阈值过滤(t *testing.T) {
	store := &fakeVectorStore{
		searchResult: []common.SearchResult{
			{ID: "1", Text: "High", Score: 0.95, Metadata: map[string]any{}},
			{ID: "2", Text: "Low", Score: 0.85, Metadata: map[string]any{}},
		},
	}
	r := NewHybridRetriever(store, &fakeEmbedModel{queryVec: []float64{0.1}})
	threshold := 0.85
	results, err := r.Retrieve(context.Background(), "test query", 5, &threshold, RetrievalModeVector)
	require.NoError(t, err)
	for _, result := range results {
		assert.GreaterOrEqual(t, result.Score, 0.85)
	}
	assert.Len(t, results, 2) // >= 边界全保留
}

// TestHybridRetriever_Retrieve_阈值非vector模式 对齐 test_retrieve_score_threshold_invalid_mode
// （hybrid+threshold → 155201，先于 mode 分支的前置校验）
func TestHybridRetriever_Retrieve_阈值非vector模式(t *testing.T) {
	r, _, _ := newHybridFixture()
	threshold := 0.5
	_, err := r.Retrieve(context.Background(), "test query", 5, &threshold, RetrievalModeHybrid)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155201, be.Code())
	assert.Contains(t, be.Message(), "score_threshold is only supported")
}

// ── Go 增强（钉 Python 盲区）──

// TestHybridRetriever_Retrieve_mode零值回落 G-1："" → hybrid（spec §8-1 钉死）
func TestHybridRetriever_Retrieve_mode零值回落(t *testing.T) {
	r, store, _ := newHybridFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, 1, store.hybridCalls)
}

// TestHybridRetriever_Retrieve_embed缺省纯稀疏 G-2：hybrid 模式无 embed →
// HybridSearch 收到 nil queryVector（store 层纯稀疏路径）+ embed_query 零调用
func TestHybridRetriever_Retrieve_embed缺省纯稀疏(t *testing.T) {
	store := &fakeVectorStore{
		hybridResult: []common.SearchResult{
			{ID: "1", Text: "Sparse-only", Score: 0.8, Metadata: map[string]any{}},
		},
	}
	r := NewHybridRetriever(store, nil)
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Nil(t, store.lastHybridVec)
}

// TestHybridRetriever_Retrieve_alpha未设回落构造值 G-3：构造 0.7 不传 opts → 0.7
func TestHybridRetriever_Retrieve_alpha未设回落构造值(t *testing.T) {
	r, store, _ := newHybridFixture0(0.7)
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Equal(t, 0.7, store.lastAlpha)
}

// TestHybridRetriever_Retrieve_模式不支持文案 G-4：逐字符断言
// （与 Vector/Sparse 的 "only supports" 文案不同——Python 三作者不一致，spec §8-6）
func TestHybridRetriever_Retrieve_模式不支持文案(t *testing.T) {
	r, _, _ := newHybridFixture()
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalMode("foo"))
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155200, be.Code())
	assert.Contains(t, be.Message(), "Unsupported mode: foo")
}

// TestHybridRetriever_Retrieve_阈值下界过滤 G-5：阈值 0.9 → 0.85 被过滤（H6 只测全保留边界）
func TestHybridRetriever_Retrieve_阈值下界过滤(t *testing.T) {
	store := &fakeVectorStore{
		searchResult: []common.SearchResult{
			{ID: "1", Text: "High", Score: 0.95, Metadata: map[string]any{}},
			{ID: "2", Text: "Low", Score: 0.85, Metadata: map[string]any{}},
		},
	}
	r := NewHybridRetriever(store, &fakeEmbedModel{queryVec: []float64{0.1}})
	threshold := 0.9
	results, err := r.Retrieve(context.Background(), "test query", 5, &threshold, RetrievalModeVector)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "High", results[0].Text)
}

// TestHybridRetriever_Retrieve_HybridSearch错误透传 G-6：不包装原样返回
func TestHybridRetriever_Retrieve_HybridSearch错误透传(t *testing.T) {
	sentinel := errors.New("hybrid backend down")
	store := &fakeVectorStore{hybridErr: sentinel}
	r := NewHybridRetriever(store, &fakeEmbedModel{queryVec: []float64{0.1}})
	_, err := r.Retrieve(context.Background(), "q", 5, nil, RetrievalModeHybrid)
	require.Equal(t, sentinel, err)
}

// TestHybridRetriever_Retrieve_chunkID取主键 G-7：归一化 chunk_id 取 result.ID
// （Python 三作者不一致点钉死：Hybrid 同 Sparse 取主键，异于 Vector 的 metadata）
func TestHybridRetriever_Retrieve_chunkID取主键(t *testing.T) {
	r, _, _ := newHybridFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Equal(t, "1", results[0].ChunkID)
	assert.Equal(t, "doc_1", results[0].DocID)
}

// TestHybridRetriever_Retrieve_默认alpha05 G-8：无构造无调用 → 0.5（Python 默认参数）
func TestHybridRetriever_Retrieve_默认alpha05(t *testing.T) {
	r, store, _ := newHybridFixture()
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Equal(t, 0.5, store.lastAlpha)
}
