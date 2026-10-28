package vector_store

import (
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
