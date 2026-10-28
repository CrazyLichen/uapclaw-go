package retriever

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestVectorRetriever_能力接口回填 embed_model/vector_store/index_type 三态
func TestVectorRetriever_能力接口回填(t *testing.T) {
	store := &fakeVectorStore{}
	var r Retriever = NewVectorRetriever(store, nil)

	// EmbedModelProvider：实现且返回 nil（Python 属性存在值为 None）
	ep, ok := r.(EmbedModelProvider)
	assert.True(t, ok)
	assert.Nil(t, ep.EmbedModel())

	// VectorStoreAccessor
	vs, ok := r.(VectorStoreAccessor)
	assert.True(t, ok)
	assert.Equal(t, store, vs.VectorStore())

	// IndexTypeCarrier：零值 "" = Python None 未注入
	ic, ok := r.(IndexTypeCarrier)
	assert.True(t, ok)
	assert.Equal(t, "", ic.IndexType())
	ic.SetIndexType("hybrid")
	assert.Equal(t, "hybrid", ic.IndexType())
}

// TestSparseRetriever_能力接口回填 无 embed_model 属性（hasattr False 同构）
func TestSparseRetriever_能力接口回填(t *testing.T) {
	var r Retriever = NewSparseRetriever(&fakeVectorStore{})

	_, ok := r.(EmbedModelProvider)
	assert.False(t, ok) // Python SparseRetriever 无 embed_model 属性

	_, ok = r.(VectorStoreAccessor)
	assert.True(t, ok)

	_, ok = r.(IndexTypeCarrier)
	assert.True(t, ok)
}

// TestHybridRetriever_能力接口回填
func TestHybridRetriever_能力接口回填(t *testing.T) {
	var r Retriever = NewHybridRetriever(&fakeVectorStore{}, nil)

	ep, ok := r.(EmbedModelProvider)
	assert.True(t, ok)
	assert.Nil(t, ep.EmbedModel())

	_, ok = r.(VectorStoreAccessor)
	assert.True(t, ok)

	_, ok = r.(IndexTypeCarrier)
	assert.True(t, ok)
}

// Test三实现SetIndexType回读 三实现注入后回读一致
func Test三实现SetIndexType回读(t *testing.T) {
	for name, r := range map[string]Retriever{
		"vector": NewVectorRetriever(&fakeVectorStore{}, nil),
		"sparse": NewSparseRetriever(&fakeVectorStore{}),
		"hybrid": NewHybridRetriever(&fakeVectorStore{}, nil),
	} {
		carrier := r.(IndexTypeCarrier)
		carrier.SetIndexType("bm25")
		assert.Equal(t, "bm25", carrier.IndexType(), name)
	}
}
