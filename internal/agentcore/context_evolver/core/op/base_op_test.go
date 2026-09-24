package op

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
)

// fakeLLMService LLMService 的 mock 实现
type fakeLLMService struct {
	response string
	err      error
}

func (f *fakeLLMService) Generate(_ context.Context, _ string, _ ...cecontext.GenerateOption) (string, error) {
	return f.response, f.err
}

// fakeEmbeddingService EmbeddingService 的 mock 实现
type fakeEmbeddingService struct {
	embeddings [][]float64
	err        error
}

func (f *fakeEmbeddingService) Embed(_ context.Context, _ string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.embeddings) > 0 {
		return f.embeddings[0], nil
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

func (f *fakeEmbeddingService) EmbedBatch(_ context.Context, _ []string) ([][]float64, error) {
	return f.embeddings, f.err
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

func (m *mockVectorStore) GetAll(_ map[string]any) []*schema.VectorNode {
	var result []*schema.VectorNode
	for _, node := range m.nodes {
		result = append(result, node)
	}
	return result
}

func TestNewOpBase(t *testing.T) {
	sc := cecontext.NewServiceContext()
	b := NewOpBase(sc)
	require.NotNil(t, b)
}

func TestNewOpBase_nil(t *testing.T) {
	b := NewOpBase(nil)
	require.NotNil(t, b)
	assert.Nil(t, b.LLM())
	assert.Nil(t, b.EmbeddingModel())
	assert.Nil(t, b.VectorStore())
}

func TestOpBase_LLM(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: "hello"}
	sc.RegisterService("llm", llm)
	b := NewOpBase(sc)
	result := b.LLM()
	require.NotNil(t, result)
	resp, err := result.Generate(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, "hello", resp)
}

func TestOpBase_LLM_未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	b := NewOpBase(sc)
	assert.Nil(t, b.LLM())
}

func TestOpBase_EmbeddingModel(t *testing.T) {
	sc := cecontext.NewServiceContext()
	emb := &fakeEmbeddingService{embeddings: [][]float64{{0.5, 0.6}}}
	sc.RegisterService("embedding_model", emb)
	b := NewOpBase(sc)
	result := b.EmbeddingModel()
	require.NotNil(t, result)
	vec, err := result.Embed(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, []float64{0.5, 0.6}, vec)
}

func TestOpBase_EmbeddingModel_未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	b := NewOpBase(sc)
	assert.Nil(t, b.EmbeddingModel())
}

func TestOpBase_VectorStore(t *testing.T) {
	sc := cecontext.NewServiceContext()
	vs := newMockVectorStore()
	sc.RegisterService("vector_store", vs)
	b := NewOpBase(sc)
	result := b.VectorStore()
	require.NotNil(t, result)
	assert.Equal(t, 0, result.Count())
}

func TestOpBase_VectorStore_未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	b := NewOpBase(sc)
	assert.Nil(t, b.VectorStore())
}

// fakeAgentFlowService AgentFlowService 的 mock 实现
type fakeAgentFlowService struct {
	result *cecontext.TrajectoryResult
	err    error
}

func (f *fakeAgentFlowService) Execute(_ context.Context, _ string, _ string, _ ...cecontext.AgentFlowOption) (*cecontext.TrajectoryResult, error) {
	return f.result, f.err
}

func TestOpBase_AgentFlow_未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	b := NewOpBase(sc)
	assert.Nil(t, b.AgentFlow())
}

func TestOpBase_AgentFlow_已注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	af := &fakeAgentFlowService{result: &cecontext.TrajectoryResult{Answer: "agent answer", Success: true}}
	sc.RegisterAgentFlow(af)
	b := NewOpBase(sc)
	result := b.AgentFlow()
	require.NotNil(t, result)
	resp, err := result.Execute(context.Background(), "query", "session1")
	require.NoError(t, err)
	assert.Equal(t, "agent answer", resp.Answer)
}
