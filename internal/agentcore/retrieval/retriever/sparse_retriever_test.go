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

// newSparseFixture 构建装配好的 Sparse 桩（Python mock_vector_store fixture 对应物）
func newSparseFixture() (*SparseRetriever, *fakeVectorStore) {
	store := &fakeVectorStore{
		sparseResult: []common.SearchResult{
			{ID: "chunk_pk_1", Text: "Result 1", Score: 0.95, Metadata: map[string]any{"doc_id": "doc_1"}},
			{ID: "chunk_pk_2", Text: "Result 2", Score: 0.85, Metadata: map[string]any{"doc_id": "doc_2"}},
		},
	}
	r := NewSparseRetriever(store)
	return r, store
}

// TestSparseRetriever_Retrieve_成功 对齐 test_retrieve_success
func TestSparseRetriever_Retrieve_成功(t *testing.T) {
	r, store := newSparseFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "Result 1", results[0].Text)
	assert.Equal(t, 0.95, results[0].Score)
	assert.Equal(t, 1, store.sparseCalls)
}

// TestSparseRetriever_Retrieve_模式不支持 对齐 test_retrieve_invalid_mode
func TestSparseRetriever_Retrieve_模式不支持(t *testing.T) {
	r, _ := newSparseFixture()
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeVector)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155200, be.Code())
	assert.Contains(t, be.Message(), "only supports 'sparse' mode")
}

// TestSparseRetriever_BatchRetrieve 对齐 test_batch_retrieve
func TestSparseRetriever_BatchRetrieve(t *testing.T) {
	r, _ := newSparseFixture()
	results, err := r.BatchRetrieve(context.Background(), []string{"query 1", "query 2"}, 5)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Len(t, results[0], 2)
	assert.Len(t, results[1], 2)
}

// TestSparseRetriever_Retrieve_chunkID取主键 G-1：Sparse 归一化 chunk_id 取 result.ID
// （Python 三作者不一致点：Vector 取 metadata["chunk_id"]，Sparse/Hybrid 取 result.id——钉死）
func TestSparseRetriever_Retrieve_chunkID取主键(t *testing.T) {
	r, _ := newSparseFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Equal(t, "chunk_pk_1", results[0].ChunkID)
}

// TestSparseRetriever_Retrieve_阈值静默忽略 G-6：Python Sparse 无 threshold 校验无过滤（spec §8-5）
func TestSparseRetriever_Retrieve_阈值静默忽略(t *testing.T) {
	r, _ := newSparseFixture()
	threshold := 0.9
	results, err := r.Retrieve(context.Background(), "test query", 5, &threshold, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Len(t, results, 2) // 不过滤，2 条全保留
}

// TestSparseRetriever_RetrieveSearchResults_模式校验 G-2：Python Sparse.rsr 有 mode 校验（与 Vector 不对称）
func TestSparseRetriever_RetrieveSearchResults_模式校验(t *testing.T) {
	r, _ := newSparseFixture()
	_, err := r.RetrieveSearchResults(context.Background(), "test query", 5, RetrievalModeVector)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155200, be.Code())
}

// TestSparseRetriever_RetrieveSearchResults_原始结果 G-2：返回 SearchResult 不归一化
func TestSparseRetriever_RetrieveSearchResults_原始结果(t *testing.T) {
	r, _ := newSparseFixture()
	results, err := r.RetrieveSearchResults(context.Background(), "test query", 5, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "chunk_pk_1", results[0].ID) // 原始 ID，非归一化
}

// TestSparseRetriever_Retrieve_错误透传 G-7
func TestSparseRetriever_Retrieve_错误透传(t *testing.T) {
	sentinel := errors.New("bm25 backend down")
	store := &fakeVectorStore{sparseErr: sentinel}
	r := NewSparseRetriever(store)
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeSparse)
	require.Equal(t, sentinel, err) // 原样冒泡不包装
}

// TestSparseRetriever_Close_关闭底层 G-3（sparse 侧）：对齐 Python close() 判空关底层
func TestSparseRetriever_Close_关闭底层(t *testing.T) {
	r, store := newSparseFixture()
	r.Close()
	assert.Equal(t, 1, store.closeCalls)
}

// TestSparseRetriever_Close_nilStore不panic G-3 边界（sparse 侧）
func TestSparseRetriever_Close_nilStore不panic(t *testing.T) {
	r := NewSparseRetriever(nil)
	assert.NotPanics(t, r.Close)
}

// TestSparseRetriever_RetrieveSearchResults_无嵌入模型可检索 BM25 不需要嵌入模型
// （Python Sparse 无 embed_model 字段，Vector.rsr 的 155202 在此不存在——不对称钉点）
func TestSparseRetriever_RetrieveSearchResults_无嵌入模型可检索(t *testing.T) {
	r, store := newSparseFixture()
	results, err := r.RetrieveSearchResults(context.Background(), "test query", 5, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, 1, store.sparseCalls)
}

// TestSparseRetriever_BatchRetrieve_错误透传 G-5（sparse 侧）：裸 gather 首错传播
func TestSparseRetriever_BatchRetrieve_错误透传(t *testing.T) {
	sentinel := errors.New("bm25 down")
	store := &fakeVectorStore{sparseErr: sentinel}
	r := NewSparseRetriever(store)
	_, err := r.BatchRetrieve(context.Background(), []string{"q1", "q2"}, 5)
	require.Error(t, err)
	require.Equal(t, sentinel, err)
}
