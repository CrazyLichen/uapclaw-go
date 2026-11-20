package vector_store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestChromaVectorStore_CollectionName回填 getter/setter + 缓存失效
func TestChromaVectorStore_CollectionName回填(t *testing.T) {
	s := &ChromaVectorStore{collectionName: "old"}
	assert.Equal(t, "old", s.CollectionName())
	s.SetCollectionName("kb_1_triples")
	assert.Equal(t, "kb_1_triples", s.CollectionName())
	// 缓存失效：Set 后 getCollection 必须重新按名取（nil 缓存被清）
	assert.Nil(t, s.collection)
}

// TestMilvusVectorStore_CollectionName回填
func TestMilvusVectorStore_CollectionName回填(t *testing.T) {
	s := &MilvusVectorStore{collectionName: "old"}
	assert.Equal(t, "old", s.CollectionName())
	s.SetCollectionName("kb_1_chunks")
	assert.Equal(t, "kb_1_chunks", s.CollectionName())
}

// TestChromaVectorStore_属性访问器回填 13.26 新增 6 getter（validate_index 消费）。
func TestChromaVectorStore_属性访问器回填(t *testing.T) {
	s := &ChromaVectorStore{
		textField:         "content",
		sparseVectorField: "sparse_vector",
		metadataField:     "metadata",
		docIDField:        "document_id",
		databaseName:      "test_db",
	}
	// vectorField nil 时 VectorField() 返回 nil（any 装箱 nil 结构体指针）
	assert.Nil(t, s.VectorField())
	assert.Equal(t, "content", s.TextField())
	assert.Equal(t, "sparse_vector", s.SparseVectorField())
	assert.Equal(t, "metadata", s.MetadataField())
	assert.Equal(t, "document_id", s.DocIDField())
	assert.Equal(t, "test_db", s.DatabaseName())
}

// TestMilvusVectorStore_属性访问器回填 13.26 新增 6 getter + EnsureLoaded 转发。
func TestMilvusVectorStore_属性访问器回填(t *testing.T) {
	s := &MilvusVectorStore{
		textField:         "content",
		sparseVectorField: "sparse_vector",
		metadataField:     "metadata",
		docIDField:        "document_id",
		databaseName:      "test_db",
	}
	assert.Nil(t, s.VectorField())
	assert.Equal(t, "content", s.TextField())
	assert.Equal(t, "sparse_vector", s.SparseVectorField())
	assert.Equal(t, "metadata", s.MetadataField())
	assert.Equal(t, "document_id", s.DocIDField())
	assert.Equal(t, "test_db", s.DatabaseName())
	// EnsureLoaded：collectionLoaded=true 时短路返回 nil（不触网络）
	s.mu.Lock()
	s.collectionLoaded = true
	s.mu.Unlock()
	assert.NoError(t, s.EnsureLoaded(context.Background()))
}
