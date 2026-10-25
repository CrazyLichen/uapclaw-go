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

// newVectorFixture 构建装配好的 fake 桩与检索器（Python fixture 对应物）
func newVectorFixture() (*VectorRetriever, *fakeVectorStore, *fakeEmbedModel) {
	store := &fakeVectorStore{
		searchResult: []common.SearchResult{
			{ID: "1", Text: "Result 1", Score: 0.95, Metadata: map[string]any{"doc_id": "doc_1", "chunk_id": "chunk_1"}},
			{ID: "2", Text: "Result 2", Score: 0.85, Metadata: map[string]any{"doc_id": "doc_2", "chunk_id": "chunk_2"}},
		},
		sparseResult: []common.SearchResult{},
	}
	embed := &fakeEmbedModel{queryVec: []float64{0.1}}
	r := NewVectorRetriever(store, embed)
	return r, store, embed
}

// TestVectorRetriever_Retrieve_成功 对齐 test_retrieve_success
func TestVectorRetriever_Retrieve_成功(t *testing.T) {
	r, store, embed := newVectorFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeVector)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "Result 1", results[0].Text)
	assert.Equal(t, 0.95, results[0].Score)
	assert.Equal(t, 1, embed.embedQueryCalls)
	assert.Equal(t, 1, store.searchCalls)
}

// TestVectorRetriever_Retrieve_分数阈值 对齐 test_retrieve_with_score_threshold
func TestVectorRetriever_Retrieve_分数阈值(t *testing.T) {
	r, _, _ := newVectorFixture()
	threshold := 0.9
	results, err := r.Retrieve(context.Background(), "test query", 5, &threshold, RetrievalModeVector)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.GreaterOrEqual(t, results[0].Score, 0.9)
}

// TestVectorRetriever_Retrieve_BM25兜底 对齐 test_retrieve_fallback_to_sparse
func TestVectorRetriever_Retrieve_BM25兜底(t *testing.T) {
	store := &fakeVectorStore{
		searchResult: []common.SearchResult{},
		sparseResult: []common.SearchResult{
			{ID: "1", Text: "Sparse result", Score: 0.8, Metadata: map[string]any{}},
		},
	}
	r := NewVectorRetriever(store, &fakeEmbedModel{queryVec: []float64{0.1}})
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeVector)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Sparse result", results[0].Text)
	assert.Equal(t, 1, store.sparseCalls)
}

// TestVectorRetriever_Retrieve_模式不支持 对齐 test_retrieve_invalid_mode
func TestVectorRetriever_Retrieve_模式不支持(t *testing.T) {
	r, _, _ := newVectorFixture()
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeSparse)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155200, be.Code())
	assert.Contains(t, be.Message(), "only supports 'vector' mode")
}

// TestVectorRetriever_Retrieve_无嵌入模型 对齐 test_retrieve_without_embed_model
func TestVectorRetriever_Retrieve_无嵌入模型(t *testing.T) {
	rNil := NewVectorRetriever(&fakeVectorStore{}, nil)
	_, err := rNil.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeVector)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155202, be.Code())
	assert.Contains(t, be.Message(), "embed_model is required")
}

// TestVectorRetriever_BatchRetrieve 对齐 test_batch_retrieve
func TestVectorRetriever_BatchRetrieve(t *testing.T) {
	r, _, _ := newVectorFixture()
	results, err := r.BatchRetrieve(context.Background(), []string{"query 1", "query 2"}, 5)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Len(t, results[0], 2)
	assert.Len(t, results[1], 2)
}

// TestVectorRetriever_Retrieve_chunkID取Metadata G-1：Vector 归一化 chunk_id 取 metadata["chunk_id"]
// （Python 不一致点钉死：Vector=metadata 来源，Sparse=result.id 来源）
func TestVectorRetriever_Retrieve_chunkID取Metadata(t *testing.T) {
	r, _, _ := newVectorFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeVector)
	require.NoError(t, err)
	assert.Equal(t, "chunk_1", results[0].ChunkID)
	assert.Equal(t, "doc_1", results[0].DocID)
}

// TestVectorRetriever_Retrieve_metadata非字符串 G-1 边界：断言失败→空串（spec §8-3）
func TestVectorRetriever_Retrieve_metadata非字符串(t *testing.T) {
	store := &fakeVectorStore{
		searchResult: []common.SearchResult{
			{ID: "1", Text: "Result 1", Score: 0.9, Metadata: map[string]any{"doc_id": 123, "chunk_id": true}},
		},
	}
	r := NewVectorRetriever(store, &fakeEmbedModel{queryVec: []float64{0.1}})
	results, err := r.Retrieve(context.Background(), "q", 5, nil, RetrievalModeVector)
	require.NoError(t, err)
	assert.Equal(t, "", results[0].DocID) // Python pydantic lax 强转 "123"，Go 空串（归档偏差）
	assert.Equal(t, "", results[0].ChunkID)
}

// TestVectorRetriever_RetrieveSearchResults_原始结果 G-2：返回 SearchResult 不归一化
func TestVectorRetriever_RetrieveSearchResults_原始结果(t *testing.T) {
	r, _, _ := newVectorFixture()
	results, err := r.RetrieveSearchResults(context.Background(), "test query", 5, RetrievalModeVector)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "1", results[0].ID) // 原始 ID
}

// TestVectorRetriever_RetrieveSearchResults_无mode校验 G-2：传 sparse 不报错静默走 vector（spec §8-8）
func TestVectorRetriever_RetrieveSearchResults_无mode校验(t *testing.T) {
	r, store, _ := newVectorFixture()
	results, err := r.RetrieveSearchResults(context.Background(), "test query", 5, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, 1, store.searchCalls) // 走的是 vector 检索
}

// TestVectorRetriever_Close_关闭底层 G-3
func TestVectorRetriever_Close_关闭底层(t *testing.T) {
	r, store, _ := newVectorFixture()
	r.Close()
	assert.Equal(t, 1, store.closeCalls)
}

// TestVectorRetriever_Close_nilStore不panic G-3 边界
func TestVectorRetriever_Close_nilStore不panic(t *testing.T) {
	r := NewVectorRetriever(nil, nil)
	assert.NotPanics(t, r.Close)
}

// TestVectorRetriever_Retrieve_mode零值回落 G-4："" 等同未设走子类默认（spec §8-1 钉死）
func TestVectorRetriever_Retrieve_mode零值回落(t *testing.T) {
	r, _, _ := newVectorFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// TestSparseRetriever_Retrieve_mode零值回落 G-4（sparse 侧）
func TestSparseRetriever_Retrieve_mode零值回落(t *testing.T) {
	r, _ := newSparseFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// TestVectorRetriever_BatchRetrieve_首错即返回 G-5：q1 成功 q2 失败 → 整体 error，
// 成功结果丢弃（裸 gather 语义：首错传播）。
// fakeVectorStore.searchErrAfter=1 → 第 1 次 Search 成功、之后报错。
func TestVectorRetriever_BatchRetrieve_首错即返回(t *testing.T) {
	store := &fakeVectorStore{
		searchResult: []common.SearchResult{
			{ID: "1", Text: "q1 result", Score: 0.9, Metadata: map[string]any{}},
		},
		searchErr:      errors.New("vector backend down"),
		searchErrAfter: 1, // 第 1 次 search 成功，之后报错
	}
	r := NewVectorRetriever(store, &fakeEmbedModel{queryVec: []float64{0.1}})
	results, err := r.BatchRetrieve(context.Background(), []string{"q1", "q2"}, 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vector backend down")
	assert.Nil(t, results) // 首错时成功结果丢弃（裸 gather 语义）
}

// TestVectorRetriever_BatchRetrieve_空queries G-8：空切片非 nil
func TestVectorRetriever_BatchRetrieve_空queries(t *testing.T) {
	r, _, _ := newVectorFixture()
	results, err := r.BatchRetrieve(context.Background(), []string{}, 5)
	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NotNil(t, results) // 非 nil 空切片，对齐 Python []
}

// TestVectorRetriever_Retrieve_EmbedQuery错误透传 G-7
func TestVectorRetriever_Retrieve_EmbedQuery错误透传(t *testing.T) {
	sentinel := errors.New("embed backend down")
	r := NewVectorRetriever(&fakeVectorStore{}, &fakeEmbedModel{err: sentinel})
	_, err := r.Retrieve(context.Background(), "q", 5, nil, RetrievalModeVector)
	require.Equal(t, sentinel, err)
}

// TestVectorRetriever_Retrieve_Search错误透传 G-7（store 侧）
func TestVectorRetriever_Retrieve_Search错误透传(t *testing.T) {
	sentinel := errors.New("vector backend down")
	store := &fakeVectorStore{searchErr: sentinel}
	r := NewVectorRetriever(store, &fakeEmbedModel{queryVec: []float64{0.1}})
	_, err := r.Retrieve(context.Background(), "q", 5, nil, RetrievalModeVector)
	require.Equal(t, sentinel, err)
}

// TestVectorRetriever_BM25兜底_阈值仍生效 BM25 兜底路径的阈值过滤（V2 与 V3 交叉验证）
func TestVectorRetriever_BM25兜底_阈值仍生效(t *testing.T) {
	store := &fakeVectorStore{
		searchResult: []common.SearchResult{},
		sparseResult: []common.SearchResult{
			{ID: "1", Text: "Sparse high", Score: 0.95, Metadata: map[string]any{}},
			{ID: "2", Text: "Sparse low", Score: 0.5, Metadata: map[string]any{}},
		},
	}
	r := NewVectorRetriever(store, &fakeEmbedModel{queryVec: []float64{0.1}})
	threshold := 0.9
	results, err := r.Retrieve(context.Background(), "q", 5, &threshold, RetrievalModeVector)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Sparse high", results[0].Text)
}
