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
