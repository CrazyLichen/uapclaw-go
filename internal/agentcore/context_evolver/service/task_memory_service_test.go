package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockEmbeddingServiceForTMS 用于 TaskMemoryService 测试的 EmbeddingService mock。
type mockEmbeddingServiceForTMS struct {
	embedFn      func(ctx context.Context, text string) ([]float64, error)
	embedBatchFn func(ctx context.Context, texts []string) ([][]float64, error)
}

func (m *mockEmbeddingServiceForTMS) Embed(ctx context.Context, text string) ([]float64, error) {
	if m.embedFn != nil {
		return m.embedFn(ctx, text)
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

func (m *mockEmbeddingServiceForTMS) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	if m.embedBatchFn != nil {
		return m.embedBatchFn(ctx, texts)
	}
	result := make([][]float64, len(texts))
	for i := range texts {
		result[i] = []float64{0.1, 0.2, 0.3}
	}
	return result, nil
}

// mockLLMServiceForTMS 用于 TaskMemoryService 测试的 LLMService mock。
type mockLLMServiceForTMS struct {
	generateFn func(ctx context.Context, prompt string, opts ...cecontext.GenerateOption) (string, error)
}

func (m *mockLLMServiceForTMS) Generate(ctx context.Context, prompt string, opts ...cecontext.GenerateOption) (string, error) {
	if m.generateFn != nil {
		return m.generateFn(ctx, prompt, opts...)
	}
	return "mock llm response", nil
}

// mockVectorStoreForTMS 用于 TaskMemoryService 测试的 VectorStoreService mock。
type mockVectorStoreForTMS struct {
	nodes   map[string]any
	count   int
	upserts int
}

func newMockVectorStore() *mockVectorStoreForTMS {
	return &mockVectorStoreForTMS{nodes: make(map[string]any)}
}

func (m *mockVectorStoreForTMS) Upsert(_ context.Context, node *schema.VectorNode) error {
	m.upserts++
	m.nodes[node.ID] = node
	m.count = len(m.nodes)
	return nil
}

func (m *mockVectorStoreForTMS) Search(_ context.Context, _ []float64, _ int, _ map[string]any) ([]*schema.VectorNode, error) {
	return nil, nil
}

func (m *mockVectorStoreForTMS) Delete(_ context.Context, nodeID string) (bool, error) {
	if _, ok := m.nodes[nodeID]; ok {
		delete(m.nodes, nodeID)
		m.count = len(m.nodes)
		return true, nil
	}
	return false, nil
}

func (m *mockVectorStoreForTMS) Clear() {
	m.nodes = make(map[string]any)
	m.count = 0
}

func (m *mockVectorStoreForTMS) Count() int {
	return m.count
}

func (m *mockVectorStoreForTMS) GetAll(_ map[string]any) []*schema.VectorNode {
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestNormalizeAlgoName_正常值 验证算法名规范化。
func TestNormalizeAlgoName_正常值(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ACE", "ACE"},
		{"ace", "ACE"},
		{"RB", "ReasoningBank"},
		{"rb", "ReasoningBank"},
		{"REASONINGBANK", "ReasoningBank"},
		{"REME", "ReMe"},
		{"reme", "ReMe"},
		{"REFCON", "RefCon"},
		{"DIVCON", "DivCon"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result, err := NormalizeAlgoName(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestNormalizeAlgoName_非法值 验证未知算法名返回错误。
func TestNormalizeAlgoName_非法值(t *testing.T) {
	_, err := NormalizeAlgoName("unknown")
	require.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestNewTaskMemoryServiceWithServices_基本构造 验证 ACE 算法的基本构造。
func TestNewTaskMemoryServiceWithServices_基本构造(t *testing.T) {
	llm := &mockLLMServiceForTMS{}
	emb := &mockEmbeddingServiceForTMS{}
	vs := newMockVectorStore()

	svc, err := NewTaskMemoryServiceWithServices(llm, emb, vs, &TaskMemoryServiceConfig{
		RetrievalAlgo: "ACE",
		SummaryAlgo:   "ACE",
	})
	require.NoError(t, err)
	assert.NotNil(t, svc)
	assert.Equal(t, "ACE", svc.retrievalAlgorithm)
	assert.Equal(t, "ACE", svc.summaryAlgorithm)
	assert.NotNil(t, svc.retrieveFlow)
	assert.NotNil(t, svc.summaryFlow)
}

// TestNewTaskMemoryServiceWithServices_RB算法 验证 ReasoningBank 算法构造。
func TestNewTaskMemoryServiceWithServices_RB算法(t *testing.T) {
	llm := &mockLLMServiceForTMS{}
	emb := &mockEmbeddingServiceForTMS{}
	vs := newMockVectorStore()

	svc, err := NewTaskMemoryServiceWithServices(llm, emb, vs, &TaskMemoryServiceConfig{
		RetrievalAlgo: "RB",
		SummaryAlgo:   "RB",
	})
	require.NoError(t, err)
	assert.Equal(t, "ReasoningBank", svc.retrievalAlgorithm)
	assert.Equal(t, "ReasoningBank", svc.summaryAlgorithm)
}

// TestNewTaskMemoryServiceWithServices_默认值 验证默认算法为 ACE。
func TestNewTaskMemoryServiceWithServices_默认值(t *testing.T) {
	llm := &mockLLMServiceForTMS{}
	emb := &mockEmbeddingServiceForTMS{}
	vs := newMockVectorStore()

	svc, err := NewTaskMemoryServiceWithServices(llm, emb, vs, nil)
	require.NoError(t, err)
	assert.Equal(t, "ACE", svc.retrievalAlgorithm)
	assert.Equal(t, "ACE", svc.summaryAlgorithm)
}

// TestNewTaskMemoryServiceWithServices_非法算法 验证非法算法返回错误。
func TestNewTaskMemoryServiceWithServices_非法算法(t *testing.T) {
	llm := &mockLLMServiceForTMS{}
	emb := &mockEmbeddingServiceForTMS{}
	vs := newMockVectorStore()

	_, err := NewTaskMemoryServiceWithServices(llm, emb, vs, &TaskMemoryServiceConfig{
		RetrievalAlgo: "invalid",
	})
	require.Error(t, err)
}

// TestTaskMemoryService_AddMemory_ACE 验证 ACE 算法的添加记忆。
func TestTaskMemoryService_AddMemory_ACE(t *testing.T) {
	llm := &mockLLMServiceForTMS{}
	emb := &mockEmbeddingServiceForTMS{}
	vs := newMockVectorStore()

	svc, err := NewTaskMemoryServiceWithServices(llm, emb, vs, &TaskMemoryServiceConfig{
		SummaryAlgo: "ACE",
	})
	require.NoError(t, err)

	result, err := svc.AddMemory(context.Background(), "user1", AddMemoryRequest{
		Content: "This is a test memory",
		Section: "general",
	})
	require.NoError(t, err)
	assert.Equal(t, "success", result["status"])
	assert.Equal(t, "user1", result["user_id"])
	assert.Equal(t, "ACE", result["algorithm"])
	assert.Equal(t, 1, vs.upserts)
}

// TestTaskMemoryService_AddMemory_ReMe 验证 ReMe 算法的添加记忆。
func TestTaskMemoryService_AddMemory_ReMe(t *testing.T) {
	llm := &mockLLMServiceForTMS{}
	emb := &mockEmbeddingServiceForTMS{}
	vs := newMockVectorStore()

	svc, err := NewTaskMemoryServiceWithServices(llm, emb, vs, &TaskMemoryServiceConfig{
		SummaryAlgo: "ReMe",
	})
	require.NoError(t, err)

	whenToUse := "when debugging"
	result, err := svc.AddMemory(context.Background(), "user1", AddMemoryRequest{
		Content:   "This is a ReMe memory",
		WhenToUse: &whenToUse,
	})
	require.NoError(t, err)
	assert.Equal(t, "success", result["status"])
	assert.Equal(t, "ReMe", result["algorithm"])
}

// TestTaskMemoryService_AddMemory_ReasoningBank 验证 RB 算法的添加记忆。
func TestTaskMemoryService_AddMemory_ReasoningBank(t *testing.T) {
	llm := &mockLLMServiceForTMS{}
	emb := &mockEmbeddingServiceForTMS{}
	vs := newMockVectorStore()

	svc, err := NewTaskMemoryServiceWithServices(llm, emb, vs, &TaskMemoryServiceConfig{
		SummaryAlgo: "RB",
	})
	require.NoError(t, err)

	title := "test title"
	desc := "test description"
	result, err := svc.AddMemory(context.Background(), "user1", AddMemoryRequest{
		Content:     "This is an RB memory",
		Title:       &title,
		Description: &desc,
	})
	require.NoError(t, err)
	assert.Equal(t, "success", result["status"])
	assert.Equal(t, "ReasoningBank", result["algorithm"])
}

// TestTaskMemoryService_Reconfigure 验证重新配置算法。
func TestTaskMemoryService_Reconfigure(t *testing.T) {
	llm := &mockLLMServiceForTMS{}
	emb := &mockEmbeddingServiceForTMS{}
	vs := newMockVectorStore()

	svc, err := NewTaskMemoryServiceWithServices(llm, emb, vs, &TaskMemoryServiceConfig{
		RetrievalAlgo: "ACE",
		SummaryAlgo:   "ACE",
	})
	require.NoError(t, err)

	err = svc.Reconfigure("RB")
	require.NoError(t, err)
	assert.Equal(t, "ReasoningBank", svc.retrievalAlgorithm)
	assert.Equal(t, "ReasoningBank", svc.summaryAlgorithm)
}

// TestTaskMemoryService_Reconfigure_非法算法 验证非法算法名重新配置返回错误。
func TestTaskMemoryService_Reconfigure_非法算法(t *testing.T) {
	llm := &mockLLMServiceForTMS{}
	emb := &mockEmbeddingServiceForTMS{}
	vs := newMockVectorStore()

	svc, err := NewTaskMemoryServiceWithServices(llm, emb, vs, &TaskMemoryServiceConfig{
		RetrievalAlgo: "ACE",
		SummaryAlgo:   "ACE",
	})
	require.NoError(t, err)

	err = svc.Reconfigure("invalid")
	require.Error(t, err)
	// 原算法不变
	assert.Equal(t, "ACE", svc.retrievalAlgorithm)
}

// TestTaskMemoryService_LoadMemories_无Persistence 验证无持久化时为 no-op。
func TestTaskMemoryService_LoadMemories_无Persistence(t *testing.T) {
	llm := &mockLLMServiceForTMS{}
	emb := &mockEmbeddingServiceForTMS{}
	vs := newMockVectorStore()

	svc, err := NewTaskMemoryServiceWithServices(llm, emb, vs, &TaskMemoryServiceConfig{
		RetrievalAlgo: "ACE",
		SummaryAlgo:   "ACE",
	})
	require.NoError(t, err)
	assert.Nil(t, svc.PersistenceHelper())

	err = svc.LoadMemories(context.Background(), "user1")
	require.NoError(t, err)
}

// TestAlgoToPersistName 验证算法名到持久化名的映射。
func TestAlgoToPersistName(t *testing.T) {
	tests := []struct {
		algo     string
		expected string
	}{
		{"ACE", "ace"},
		{"ReasoningBank", "rb"},
		{"ReMe", "reme"},
		{"RefCon", "reme"},
		{"DivCon", "reme"},
	}
	for _, tc := range tests {
		t.Run(tc.algo, func(t *testing.T) {
			assert.Equal(t, tc.expected, algoToPersistName(tc.algo))
		})
	}
}

// TestApplyConfigDefaults 验证默认值应用。
func TestApplyConfigDefaults(t *testing.T) {
	cfg := applyConfigDefaults(nil)
	assert.Equal(t, "gpt-5.2", cfg.LLMModel)
	assert.Equal(t, "text-embedding-3-small", cfg.EmbeddingModel)
	assert.Equal(t, "ACE", cfg.RetrievalAlgo)
	assert.Equal(t, "ACE", cfg.SummaryAlgo)
	assert.Equal(t, 10, cfg.TopKRetrieval)
	assert.Equal(t, 5, cfg.TopKRerank)
	assert.Equal(t, 1, cfg.TopKQuery)
	assert.Equal(t, 50, cfg.MaxPlaybookSize)
}

// TestApplyConfigDefaults_自定义值 验证自定义值不被覆盖。
func TestApplyConfigDefaults_自定义值(t *testing.T) {
	cfg := applyConfigDefaults(&TaskMemoryServiceConfig{
		LLMModel:      "custom-model",
		EmbeddingModel: "custom-emb",
		RetrievalAlgo:  "RB",
		SummaryAlgo:    "ReMe",
	})
	assert.Equal(t, "custom-model", cfg.LLMModel)
	assert.Equal(t, "custom-emb", cfg.EmbeddingModel)
	assert.Equal(t, "RB", cfg.RetrievalAlgo)
	assert.Equal(t, "ReMe", cfg.SummaryAlgo)
}

// TestNewOpenAIEmbeddingWrapperWithClient 验证 WithClient 构造。
func TestNewOpenAIEmbeddingWrapperWithClient(t *testing.T) {
	mock := &mockEmbeddingClient{}
	wrapper := NewOpenAIEmbeddingWrapperWithClient("test-model", mock)
	assert.Equal(t, "test-model", wrapper.modelName)
	assert.Equal(t, mock, wrapper.client)
}

// mockBaseEmbeddingForTMS 简单 BaseEmbedding mock（用于 NewOpenAIEmbeddingWrapperWithClient）
// 这里复用 embedding_wrapper_test.go 中的 mockEmbeddingClient 即可

// TestNewOpenAILLMWrapperWithMockClient 验证内部构造注入。
func TestNewOpenAILLMWrapperWithMockClient(t *testing.T) {
	mock := &mockLLMClient{}
	wrapper := &OpenAILLMWrapper{
		modelName:    "gpt-3.5-turbo",
		temperature:  0.7,
		maxTokens:    2000,
		isNewerModel: false,
		client:       mock,
	}
	assert.NotNil(t, wrapper)
	assert.Equal(t, "gpt-3.5-turbo", wrapper.modelName)
}
