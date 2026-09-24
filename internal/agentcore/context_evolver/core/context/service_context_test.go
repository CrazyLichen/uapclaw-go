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

// fakeLLMService LLMService 的 mock 实现
type fakeLLMService struct {
	response string
	err      error
}

func (f *fakeLLMService) Generate(_ context.Context, _ string, _ ...GenerateOption) (string, error) {
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

func TestServiceContext_LLM(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.LLM())

	// 注册实现 LLMService 接口的 mock
	llm := &fakeLLMService{response: "test-response"}
	sc.RegisterService("llm", llm)
	result := sc.LLM()
	require.NotNil(t, result)

	// 注册未实现接口的值，返回 nil
	sc2 := NewServiceContext()
	sc2.RegisterService("llm", "not-a-llm")
	assert.Nil(t, sc2.LLM())
}

func TestServiceContext_EmbeddingModel(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.EmbeddingModel())

	// 注册实现 EmbeddingService 接口的 mock
	emb := &fakeEmbeddingService{embeddings: [][]float64{{0.1, 0.2}}}
	sc.RegisterService("embedding_model", emb)
	result := sc.EmbeddingModel()
	require.NotNil(t, result)

	// 注册未实现接口的值，返回 nil
	sc2 := NewServiceContext()
	sc2.RegisterService("embedding_model", "not-an-embedding")
	assert.Nil(t, sc2.EmbeddingModel())
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

func TestServiceContext_AgentFlow_未注册(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.AgentFlow())
}

func TestServiceContext_AgentFlow_已注册(t *testing.T) {
	sc := NewServiceContext()
	af := &fakeAgentFlowService{result: &TrajectoryResult{Answer: "test answer", Success: true}}
	sc.RegisterAgentFlow(af)
	result := sc.AgentFlow()
	require.NotNil(t, result)
	resp, err := result.Execute(context.Background(), "query", "session1")
	require.NoError(t, err)
	assert.Equal(t, "test answer", resp.Answer)
}

func TestServiceContext_AgentFlow_类型不匹配(t *testing.T) {
	sc := NewServiceContext()
	sc.RegisterService("agent_flow", "not-an-agent-flow")
	assert.Nil(t, sc.AgentFlow())
}

func TestServiceContext_RegisterAgentFlow(t *testing.T) {
	sc := NewServiceContext()
	af := &fakeAgentFlowService{result: &TrajectoryResult{Answer: "registered"}}
	sc.RegisterAgentFlow(af)
	require.NotNil(t, sc.AgentFlow())
}

// fakeAgentFlowService AgentFlowService 的 mock 实现
type fakeAgentFlowService struct {
	result *TrajectoryResult
	err    error
}

func (f *fakeAgentFlowService) Execute(_ context.Context, _ string, _ string, _ ...AgentFlowOption) (*TrajectoryResult, error) {
	return f.result, f.err
}

func TestServiceContext_Clear(t *testing.T) {
	sc := NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{})
	sc.Clear()
	assert.Nil(t, sc.GetService("llm"))
}

func TestServiceContext_覆盖注册(t *testing.T) {
	sc := NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{response: "v1"})
	sc.RegisterService("llm", &fakeLLMService{response: "v2"})
	llm := sc.LLM()
	require.NotNil(t, llm)
}

func TestServiceContext_String(t *testing.T) {
	sc := NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{})
	s := sc.String()
	assert.Contains(t, s, "ServiceContext")
	assert.Contains(t, s, "llm")
}

func TestGenerateOption(t *testing.T) {
	c := &GenerateConfig{}
	WithSystemPrompt("sys")(c)
	assert.Equal(t, "sys", c.SystemPrompt)
	WithTemperature(0.7)(c)
	assert.Equal(t, 0.7, c.Temperature)
	WithMaxTokens(100)(c)
	assert.Equal(t, 100, c.MaxTokens)
}
