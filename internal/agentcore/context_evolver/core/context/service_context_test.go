package context

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
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

// mockVectorStore VectorStoreService 的 mock 实现
type mockVectorStore struct {
	nodes map[string]*schema.VectorNode
}

func newMockVectorStore() *mockVectorStore {
	return &mockVectorStore{nodes: make(map[string]*schema.VectorNode)}
}

func (m *mockVectorStore) Upsert(_ context.Context, node *schema.VectorNode) error {
	m.nodes[node.ID] = node
	return nil
}

func (m *mockVectorStore) Search(_ context.Context, _ []float64, _ int, _ map[string]any) ([]*schema.VectorNode, error) {
	return nil, nil
}

func (m *mockVectorStore) Delete(_ context.Context, nodeID string) (bool, error) {
	_, ok := m.nodes[nodeID]
	if ok {
		delete(m.nodes, nodeID)
	}
	return ok, nil
}

func (m *mockVectorStore) Clear() {
	m.nodes = make(map[string]*schema.VectorNode)
}

func (m *mockVectorStore) Count() int {
	return len(m.nodes)
}

func TestServiceContext_VectorStore(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.VectorStore())

	// 注册实现 VectorStoreService 接口的 mock
	vs := newMockVectorStore()
	sc.RegisterService("vector_store", vs)
	result := sc.VectorStore()
	require.NotNil(t, result)
	assert.Equal(t, 0, result.Count())

	// 注册未实现接口的值，返回 nil
	sc2 := NewServiceContext()
	sc2.RegisterService("vector_store", "not-a-vector-store")
	assert.Nil(t, sc2.VectorStore())
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

func TestServiceContext_String(t *testing.T) {
	sc := NewServiceContext()
	sc.RegisterService("llm", "my-llm")
	s := sc.String()
	assert.Contains(t, s, "ServiceContext")
	assert.Contains(t, s, "llm")
}
