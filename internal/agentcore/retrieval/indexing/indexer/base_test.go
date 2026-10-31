package indexer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// fakeIndexer 用于测试的 Indexer mock 实现
type fakeIndexer struct {
	buildIndexCalled   bool
	updateIndexCalled  bool
	deleteIndexCalled  bool
	indexExistsCalled  bool
	getIndexInfoCalled bool
}

var _ Indexer = (*fakeIndexer)(nil)

func (f *fakeIndexer) Close() error { return nil }

func (f *fakeIndexer) BuildIndex(_ context.Context, _ []common.TextChunk, _ common.IndexConfig, _ embedding.BaseEmbedding, _ ...IndexOption) (bool, error) {
	f.buildIndexCalled = true
	return true, nil
}

func (f *fakeIndexer) UpdateIndex(_ context.Context, _ []common.TextChunk, _ string, _ common.IndexConfig, _ embedding.BaseEmbedding, _ ...IndexOption) (bool, error) {
	f.updateIndexCalled = true
	return true, nil
}

func (f *fakeIndexer) DeleteIndex(_ context.Context, _ string, _ string) (bool, error) {
	f.deleteIndexCalled = true
	return true, nil
}

func (f *fakeIndexer) IndexExists(_ context.Context, _ string) (bool, error) {
	f.indexExistsCalled = true
	return true, nil
}

func (f *fakeIndexer) GetIndexInfo(_ context.Context, _ string) (map[string]any, error) {
	f.getIndexInfoCalled = true
	return map[string]any{"count": 0}, nil
}

func TestIndexer_接口满足性(t *testing.T) {
	var _ Indexer = (*fakeIndexer)(nil)
}

func TestIndexer_fakeIndexer调用(t *testing.T) {
	f := &fakeIndexer{}
	ctx := context.Background()

	ok, err := f.BuildIndex(ctx, nil, common.IndexConfig{}, nil)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.True(t, f.buildIndexCalled)

	ok, err = f.DeleteIndex(ctx, "doc1", "idx1")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.True(t, f.deleteIndexCalled)

	exists, err := f.IndexExists(ctx, "idx1")
	assert.True(t, exists)
	assert.NoError(t, err)
	assert.True(t, f.indexExistsCalled)

	info, err := f.GetIndexInfo(ctx, "idx1")
	assert.NoError(t, err)
	assert.Equal(t, 0, info["count"])
	assert.True(t, f.getIndexInfoCalled)
}

func TestNewIndexOptions(t *testing.T) {
	opts := NewIndexOptions()
	assert.NotNil(t, opts)
	assert.Nil(t, opts.Extra)
}

func TestWithIndexExtra(t *testing.T) {
	opts := NewIndexOptions(WithIndexExtra("key", "value"))
	assert.Equal(t, "value", opts.Extra["key"])
}

// TestChromaIndexer_属性访问器回填 13.26 新增 6 getter（validate_index 消费）。
func TestChromaIndexer_属性访问器回填(t *testing.T) {
	ci := &ChromaIndexer{
		textField:         "content",
		sparseVectorField: "sparse_vector",
		metadataField:     "metadata",
		docIDField:        "document_id",
		databaseName:      "test_db",
	}
	assert.Nil(t, ci.VectorField())
	assert.Equal(t, "content", ci.TextField())
	assert.Equal(t, "sparse_vector", ci.SparseVectorField())
	assert.Equal(t, "metadata", ci.MetadataField())
	assert.Equal(t, "document_id", ci.DocIDField())
	assert.Equal(t, "test_db", ci.DatabaseName())
}

// TestMilvusIndexer_属性访问器回填 13.26 新增 6 getter。
func TestMilvusIndexer_属性访问器回填(t *testing.T) {
	mi := &MilvusIndexer{
		textField:         "content",
		sparseVectorField: "sparse_vector",
		metadataField:     "metadata",
		docIDField:        "document_id",
		databaseName:      "test_db",
	}
	assert.Nil(t, mi.VectorField())
	assert.Equal(t, "content", mi.TextField())
	assert.Equal(t, "sparse_vector", mi.SparseVectorField())
	assert.Equal(t, "metadata", mi.MetadataField())
	assert.Equal(t, "document_id", mi.DocIDField())
	assert.Equal(t, "test_db", mi.DatabaseName())
}
