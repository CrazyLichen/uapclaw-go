package context

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewServiceContext(t *testing.T) {
	sc := NewServiceContext()
	assert.NotNil(t, sc)
}

func TestServiceContext_RegisterService_GetService(t *testing.T) {
	sc := NewServiceContext()
	sc.RegisterService("llm", "mock-llm-client")
	assert.Equal(t, "mock-llm-client", sc.GetService("llm"))
	assert.Nil(t, sc.GetService("nonexistent"))
}

func TestServiceContext_LLM(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.LLM())
	sc.RegisterService("llm", "my-llm")
	assert.Equal(t, "my-llm", sc.LLM())
}

func TestServiceContext_EmbeddingModel(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.EmbeddingModel())
	sc.RegisterService("embedding_model", "my-embedding")
	assert.Equal(t, "my-embedding", sc.EmbeddingModel())
}

func TestServiceContext_VectorStore(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.VectorStore())
	sc.RegisterService("vector_store", "my-vs")
	assert.Equal(t, "my-vs", sc.VectorStore())
}

func TestServiceContext_Clear(t *testing.T) {
	sc := NewServiceContext()
	sc.RegisterService("llm", "my-llm")
	sc.Clear()
	assert.Nil(t, sc.GetService("llm"))
}

func TestServiceContext_覆盖注册(t *testing.T) {
	sc := NewServiceContext()
	sc.RegisterService("llm", "v1")
	sc.RegisterService("llm", "v2")
	assert.Equal(t, "v2", sc.GetService("llm"))
}
