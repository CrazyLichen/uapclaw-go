package service

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/config"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	cepersistence "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/persistence"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
	vector_store "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/vector_store"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockVectorStoreForTMS 用于 TaskMemoryService 测试的 VectorStoreService mock。
type mockVectorStoreForTMS struct {
	nodes   map[string]any
	count   int
	upserts int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

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

// newTestTaskMemoryService 创建测试用 TaskMemoryService。
// 使用 mock client 注入 OpenAILLMWrapper/OpenAIEmbeddingWrapper + MemoryVectorStore。
func newTestTaskMemoryService(cfg *TaskMemoryServiceConfig) (*TaskMemoryService, *mockVectorStoreForTMS, error) {
	cfg = applyConfigDefaults(cfg)

	sc := cecontext.NewServiceContext()
	llm := &OpenAILLMWrapper{
		modelName:    cfg.LLMModel,
		temperature:  0.7,
		maxTokens:    2000,
		isNewerModel: false,
		client:       &mockLLMClient{},
	}
	emb := NewOpenAIEmbeddingWrapperWithClient(cfg.EmbeddingModel, &mockEmbeddingClient{})
	vs := newMockVectorStore()

	sc.RegisterService("llm", llm)
	sc.RegisterService("embedding_model", emb)
	sc.RegisterService("vector_store", vs)

	svc, err := newTaskMemoryServiceWithServices(sc, llm, emb, vs, cfg)
	return svc, vs, err
}

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

// TestNewTaskMemoryService_基本构造 验证 ACE 算法的基本构造。
func TestNewTaskMemoryService_基本构造(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
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

// TestNewTaskMemoryService_RB算法 验证 ReasoningBank 算法构造。
func TestNewTaskMemoryService_RB算法(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		RetrievalAlgo: "RB",
		SummaryAlgo:   "RB",
	})
	require.NoError(t, err)
	assert.Equal(t, "ReasoningBank", svc.retrievalAlgorithm)
	assert.Equal(t, "ReasoningBank", svc.summaryAlgorithm)
}

// TestNewTaskMemoryService_ReMe算法 验证 ReMe 算法构造。
func TestNewTaskMemoryService_ReMe算法(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		RetrievalAlgo: "ReMe",
		SummaryAlgo:   "ReMe",
	})
	require.NoError(t, err)
	assert.Equal(t, "ReMe", svc.retrievalAlgorithm)
	assert.Equal(t, "ReMe", svc.summaryAlgorithm)
}

// TestNewTaskMemoryService_RefCon算法 验证 RefCon 算法构造。
func TestNewTaskMemoryService_RefCon算法(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		RetrievalAlgo: "RefCon",
		SummaryAlgo:   "RefCon",
	})
	require.NoError(t, err)
	assert.Equal(t, "RefCon", svc.retrievalAlgorithm)
}

// TestNewTaskMemoryService_DivCon算法 验证 DivCon 算法构造。
func TestNewTaskMemoryService_DivCon算法(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		RetrievalAlgo: "DivCon",
		SummaryAlgo:   "DivCon",
	})
	require.NoError(t, err)
	assert.Equal(t, "DivCon", svc.retrievalAlgorithm)
}

// TestNewTaskMemoryService_默认值 验证默认算法为 ACE。
func TestNewTaskMemoryService_默认值(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(nil)
	require.NoError(t, err)
	assert.Equal(t, "ACE", svc.retrievalAlgorithm)
	assert.Equal(t, "ACE", svc.summaryAlgorithm)
}

// TestNewTaskMemoryService_非法算法 验证非法算法返回错误。
func TestNewTaskMemoryService_非法算法(t *testing.T) {
	_, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		RetrievalAlgo: "invalid",
	})
	require.Error(t, err)
}

// TestTaskMemoryService_AddMemory_ACE 验证 ACE 算法的添加记忆。
func TestTaskMemoryService_AddMemory_ACE(t *testing.T) {
	svc, vs, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		SummaryAlgo: "ACE",
	})
	require.NoError(t, err)

	result, err := svc.AddMemory(context.Background(), "user1", AddMemoryRequest{
		Content: "This is a test memory",
		Section: "general",
	})
	require.NoError(t, err)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, "user1", result.UserID)
	assert.Equal(t, "ACE", result.Algorithm)
	assert.Equal(t, 1, vs.upserts)
}

// TestTaskMemoryService_AddMemory_ACE_缺少内容 验证 ACE 输入校验。
func TestTaskMemoryService_AddMemory_ACE_缺少内容(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		SummaryAlgo: "ACE",
	})
	require.NoError(t, err)

	_, err = svc.AddMemory(context.Background(), "user1", AddMemoryRequest{
		Section: "general",
		// Content 为空
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content and section")
}

// TestTaskMemoryService_AddMemory_ReMe 验证 ReMe 算法的添加记忆。
func TestTaskMemoryService_AddMemory_ReMe(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		SummaryAlgo: "ReMe",
	})
	require.NoError(t, err)

	whenToUse := "when debugging"
	result, err := svc.AddMemory(context.Background(), "user1", AddMemoryRequest{
		Content:   "This is a ReMe memory",
		WhenToUse: &whenToUse,
	})
	require.NoError(t, err)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, "ReMe", result.Algorithm)
}

// TestTaskMemoryService_AddMemory_ReMe_缺少WhenToUse 验证 ReMe 输入校验。
func TestTaskMemoryService_AddMemory_ReMe_缺少WhenToUse(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		SummaryAlgo: "ReMe",
	})
	require.NoError(t, err)

	_, err = svc.AddMemory(context.Background(), "user1", AddMemoryRequest{
		Content: "This is a ReMe memory",
		// WhenToUse 为空
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content and when_to_use")
}

// TestTaskMemoryService_AddMemory_ReasoningBank 验证 RB 算法的添加记忆。
func TestTaskMemoryService_AddMemory_ReasoningBank(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
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
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, "ReasoningBank", result.Algorithm)
}

// TestTaskMemoryService_AddMemory_RB_缺少Title 验证 RB 输入校验。
func TestTaskMemoryService_AddMemory_RB_缺少Title(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		SummaryAlgo: "RB",
	})
	require.NoError(t, err)

	desc := "test description"
	_, err = svc.AddMemory(context.Background(), "user1", AddMemoryRequest{
		Content:     "This is an RB memory",
		Description: &desc,
		// Title 为空
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content, title, and description")
}

// TestTaskMemoryService_AddMemory_RB_缺少Description 验证 RB 输入校验。
func TestTaskMemoryService_AddMemory_RB_缺少Description(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		SummaryAlgo: "RB",
	})
	require.NoError(t, err)

	title := "test title"
	_, err = svc.AddMemory(context.Background(), "user1", AddMemoryRequest{
		Content: "This is an RB memory",
		Title:   &title,
		// Description 为空
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content, title, and description")
}

// TestTaskMemoryService_AddMemory_不支持的算法 验证不支持的算法返回错误。
func TestTaskMemoryService_AddMemory_不支持的算法(t *testing.T) {
	svc := &TaskMemoryService{
		summaryAlgorithm: "Unknown",
		embedding:        NewOpenAIEmbeddingWrapperWithClient("test", &mockEmbeddingClient{}),
	}

	_, err := svc.AddMemory(context.Background(), "user1", AddMemoryRequest{
		Content: "test",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unsupported algorithm")
}

// TestTaskMemoryService_Reconfigure 验证重新配置算法。
func TestTaskMemoryService_Reconfigure(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
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
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
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
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		RetrievalAlgo: "ACE",
		SummaryAlgo:   "ACE",
	})
	require.NoError(t, err)
	assert.Nil(t, svc.PersistenceHelper())

	err = svc.LoadMemories(context.Background(), "user1")
	require.NoError(t, err)
}

// TestTaskMemoryService_LoadMemories_有Persistence 验证从持久化加载记忆。
func TestTaskMemoryService_LoadMemories_有Persistence(t *testing.T) {
	vs := newMockVectorStore()
	ph := cepersistence.NewMemoryPersistenceHelper()

	// 预先保存一些数据
	nodesData := map[string]any{
		"ace_user1_m1": map[string]any{
			"id":        "ace_user1_m1",
			"content":   "test memory",
			"metadata":  map[string]any{"workspace_id": "user1"},
			"embedding": []float64{0.1, 0.2},
		},
	}
	err := ph.Save("user1", "ace", nodesData)
	require.NoError(t, err)

	svc := &TaskMemoryService{
		summaryAlgorithm:  "ACE",
		vectorStore:       vs,
		persistenceHelper: ph,
	}

	err = svc.LoadMemories(context.Background(), "user1")
	require.NoError(t, err)
	assert.Equal(t, 1, vs.count)
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
		{"unknown", "unknown"}, // 默认分支：ToLower
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
}

// TestApplyConfigDefaults_自定义值 验证自定义值不被覆盖。
func TestApplyConfigDefaults_自定义值(t *testing.T) {
	cfg := applyConfigDefaults(&TaskMemoryServiceConfig{
		LLMModel:       "custom-model",
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

// TestTaskMemoryService_GetPlaybook_非ACE 验证非 ACE 算法返回空。
func TestTaskMemoryService_GetPlaybook_非ACE(t *testing.T) {
	svc, _, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		SummaryAlgo: "ReMe",
	})
	require.NoError(t, err)

	nodes, err := svc.GetPlaybook(context.Background(), "user1")
	require.NoError(t, err)
	assert.Nil(t, nodes)
}

// TestTaskMemoryService_ClearPlaybook_非ACE 验证非 ACE 算法返回 nil。
func TestTaskMemoryService_ClearPlaybook_非ACE(t *testing.T) {
	svc, vs, err := newTestTaskMemoryService(&TaskMemoryServiceConfig{
		SummaryAlgo: "ReMe",
	})
	require.NoError(t, err)

	err = svc.ClearPlaybook(context.Background(), "user1")
	require.NoError(t, err)
	assert.Equal(t, 0, vs.count) // 不应清除
}

// TestTaskMemoryService_GetPlaybook_ACE 验证 ACE 算法返回 playbook。
func TestTaskMemoryService_GetPlaybook_ACE(t *testing.T) {
	vs := vector_store.NewMemoryVectorStore()
	vs.LoadNode("ace_user1_test1", schema.NewVectorNode(
		"ace_user1_test1", "content", []float64{0.1},
		map[string]any{"workspace_id": "user1", "type": "ace_memory"},
	))

	svc := &TaskMemoryService{
		vectorStore:      vs,
		summaryAlgorithm: "ACE",
	}

	nodes, err := svc.GetPlaybook(context.Background(), "user1")
	require.NoError(t, err)
	assert.NotEmpty(t, nodes)
}

// TestTaskMemoryService_ClearPlaybook_ACE 验证 ACE 算法清除 playbook。
func TestTaskMemoryService_ClearPlaybook_ACE(t *testing.T) {
	vs := vector_store.NewMemoryVectorStore()
	vs.LoadNode("ace_user1_test1", schema.NewVectorNode(
		"ace_user1_test1", "content", []float64{0.1},
		map[string]any{"workspace_id": "user1", "type": "ace_memory"},
	))

	svc := &TaskMemoryService{
		vectorStore:      vs,
		summaryAlgorithm: "ACE",
	}

	err := svc.ClearPlaybook(context.Background(), "user1")
	require.NoError(t, err)
	assert.Equal(t, 0, vs.Count())
}

// TestFormatMemoryItems_空切片 验证空输入返回空字符串。
func TestFormatMemoryItems_空切片(t *testing.T) {
	assert.Equal(t, "", formatMemoryItems(nil))
	assert.Equal(t, "", formatMemoryItems([]ceschema.MemoryItem{}))
}

// TestFormatMemoryItems_单条 验证单条记忆格式化。
func TestFormatMemoryItems_单条(t *testing.T) {
	items := []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{Content: "test content", WhenToUse: "when testing"},
	}
	result := formatMemoryItems(items)
	assert.Contains(t, result, "test content")
	assert.Contains(t, result, "when testing")
}

// TestFormatMemoryItems_多条 验证多条记忆用换行拼接。
func TestFormatMemoryItems_多条(t *testing.T) {
	items := []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{Content: "content1", WhenToUse: "use1"},
		ceschema.ReMeRetrievedMemory{Content: "content2", WhenToUse: "use2"},
	}
	result := formatMemoryItems(items)
	assert.Contains(t, result, "content1")
	assert.Contains(t, result, "content2")
	assert.Contains(t, result, "\n")
}

// TestFormatMemoryItems_混合类型 验证不同 MemoryItem 类型混合格式化。
func TestFormatMemoryItems_混合类型(t *testing.T) {
	items := []ceschema.MemoryItem{
		ceschema.ReMeRetrievedMemory{Content: "reme content", WhenToUse: "reme use"},
		ceschema.ReasoningBankRetrievedMemory{Title: "rb title", Description: "rb desc", Content: "rb content"},
	}
	result := formatMemoryItems(items)
	assert.Contains(t, result, "reme content")
	assert.Contains(t, result, "rb title")
}

// TestTaskMemoryService_属性访问器 验证 PersistType/PersistPath/Milvus 属性。
func TestTaskMemoryService_属性访问器(t *testing.T) {
	svc := &TaskMemoryService{
		persistType:      strPtr("milvus"),
		persistPath:      "/tmp/memories",
		milvusHost:       "localhost",
		milvusPort:       19530,
		milvusCollection: "vector_nodes",
	}
	assert.Equal(t, "milvus", *svc.PersistType())
	assert.Equal(t, "/tmp/memories", svc.PersistPath())
	assert.Equal(t, "localhost", svc.MilvusHost())
	assert.Equal(t, 19530, svc.MilvusPort())
	assert.Equal(t, "vector_nodes", svc.MilvusCollection())
}

// TestTaskMemoryService_属性访问器_默认值 验证未设置时的默认值。
func TestTaskMemoryService_属性访问器_默认值(t *testing.T) {
	svc := &TaskMemoryService{}
	assert.Nil(t, svc.PersistType())
	assert.Equal(t, "", svc.PersistPath())
	assert.Equal(t, "", svc.MilvusHost())
	assert.Equal(t, 0, svc.MilvusPort())
	assert.Equal(t, "", svc.MilvusCollection())
}

// TestNewOpenAIEmbeddingWrapper_CeconfigFallback 验证 APIKey 空时从 ceconfig 兜底。
func TestNewOpenAIEmbeddingWrapper_CeconfigFallback(t *testing.T) {
	// 设置 ceconfig 中的 API_KEY
	ceconfig.Set("API_KEY", "test-ceconfig-key")
	defer ceconfig.Delete("API_KEY")

	wrapper, err := NewOpenAIEmbeddingWrapper("text-embedding-3-small", "", "https://api.openai.com/v1")
	require.NoError(t, err)
	assert.NotNil(t, wrapper)
}

// TestNewOpenAIEmbeddingWrapper_CeconfigFallback_无Key 验证 ceconfig 无 key 时返回错误。
func TestNewOpenAIEmbeddingWrapper_CeconfigFallback_无Key(t *testing.T) {
	// 清除 ceconfig 中的 API_KEY
	ceconfig.Delete("API_KEY")
	// 清除 os.Getenv 中的 API_KEY（以防环境变量存在）
	origEnv := os.Getenv("API_KEY")
	os.Unsetenv("API_KEY")
	defer func() {
		if origEnv != "" {
			os.Setenv("API_KEY", origEnv)
		}
	}()

	_, err := NewOpenAIEmbeddingWrapper("text-embedding-3-small", "", "https://api.openai.com/v1")
	require.Error(t, err)
}

// TestNewTaskMemoryService_需APIKey 验证 APIKey 缺失时返回错误。
func TestNewTaskMemoryService_需APIKey(t *testing.T) {
	ceconfig.Delete("API_KEY")
	origEnv := os.Getenv("API_KEY")
	os.Unsetenv("API_KEY")
	defer func() {
		if origEnv != "" {
			os.Setenv("API_KEY", origEnv)
		}
	}()

	_, err := NewTaskMemoryService(&TaskMemoryServiceConfig{
		RetrievalAlgo: "ACE",
		SummaryAlgo:   "ACE",
	})
	require.Error(t, err)
}

// TestNewTaskMemoryService_通过Ceconfig 验证通过 ceconfig 提供 APIKey 可成功构造。
func TestNewTaskMemoryService_通过Ceconfig(t *testing.T) {
	ceconfig.Set("API_KEY", "test-api-key-for-tms")
	defer ceconfig.Delete("API_KEY")

	svc, err := NewTaskMemoryService(&TaskMemoryServiceConfig{
		RetrievalAlgo: "ACE",
		SummaryAlgo:   "ACE",
	})
	require.NoError(t, err)
	assert.NotNil(t, svc)
	assert.Equal(t, "ACE", svc.retrievalAlgorithm)
}

// mockFlowOp 设置 RuntimeContext 后返回成功的 BaseOp mock。
type mockFlowOp struct {
	// setupFn 在 Execute 中调用来设置 RuntimeContext
	setupFn func(rc *cecontext.RuntimeContext)
	// err Execute 返回的错误
	err error
}

func (m *mockFlowOp) Execute(_ context.Context, rc *cecontext.RuntimeContext) error {
	if m.setupFn != nil {
		m.setupFn(rc)
	}
	return m.err
}

// TestTaskMemoryService_Retrieve_ACE 验证 ACE 算法 Retrieve 结果格式化。
func TestTaskMemoryService_Retrieve_ACE(t *testing.T) {
	svc := &TaskMemoryService{
		retrievalAlgorithm: "ACE",
		retrieveFlow: &mockFlowOp{
			setupFn: func(rc *cecontext.RuntimeContext) {
				rc.Set("retrieved_memories", []ceschema.MemoryItem{
					ceschema.ACEMemory{ID: "1", Content: "ace content", Section: "debug"},
				})
			},
		},
	}

	result, err := svc.Retrieve(context.Background(), "user1", "test query")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "user1", result.UserID)
	assert.Equal(t, "test query", result.Query)
	assert.Equal(t, "ACE", result.Algorithm)
	assert.Len(t, result.RetrievedMemory, 1)
	assert.Contains(t, result.MemoryString, "ace content")
}

// TestTaskMemoryService_Retrieve_ReMe 验证 ReMe 算法 Retrieve 使用 memory_string。
func TestTaskMemoryService_Retrieve_ReMe(t *testing.T) {
	svc := &TaskMemoryService{
		retrievalAlgorithm: "ReMe",
		retrieveFlow: &mockFlowOp{
			setupFn: func(rc *cecontext.RuntimeContext) {
				rc.Set("memory_string", "reme formatted memory")
				rc.Set("retrieved_memories", []ceschema.MemoryItem{
					ceschema.ReMeRetrievedMemory{Content: "reme content", WhenToUse: "when testing"},
				})
			},
		},
	}

	result, err := svc.Retrieve(context.Background(), "user1", "test query")
	require.NoError(t, err)
	assert.Equal(t, "reme formatted memory", result.MemoryString)
	assert.Equal(t, "ReMe", result.Algorithm)
}

// TestTaskMemoryService_Retrieve_执行失败 验证 flow 执行失败时返回错误。
func TestTaskMemoryService_Retrieve_执行失败(t *testing.T) {
	svc := &TaskMemoryService{
		retrievalAlgorithm: "ACE",
		retrieveFlow: &mockFlowOp{
			err: fmt.Errorf("flow execution failed"),
		},
	}

	_, err := svc.Retrieve(context.Background(), "user1", "test query")
	require.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestTaskMemoryService_Summarize_ACE 验证 ACE 算法 Summarize 结果。
func TestTaskMemoryService_Summarize_ACE(t *testing.T) {
	svc := &TaskMemoryService{
		summaryAlgorithm: "ACE",
		summaryFlow: &mockFlowOp{
			setupFn: func(rc *cecontext.RuntimeContext) {
				rc.Set("memories", []*schema.VectorNode{
					schema.NewVectorNode("id1", "content", []float64{0.1}, nil),
				})
			},
		},
	}

	result, err := svc.Summarize(context.Background(), "user1", "matts", "query", []string{"traj1"})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "user1", result.UserID)
	assert.Equal(t, "ACE", result.Algorithm)
	assert.Len(t, result.Memories, 1)
}

// TestTaskMemoryService_Summarize_执行失败 验证 flow 执行失败时返回错误。
func TestTaskMemoryService_Summarize_执行失败(t *testing.T) {
	svc := &TaskMemoryService{
		summaryAlgorithm: "ACE",
		summaryFlow: &mockFlowOp{
			err: fmt.Errorf("summarize flow failed"),
		},
	}

	_, err := svc.Summarize(context.Background(), "user1", "matts", "query", []string{"traj1"})
	require.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// strPtr 辅助函数，返回字符串指针。
func strPtr(s string) *string { return &s }
