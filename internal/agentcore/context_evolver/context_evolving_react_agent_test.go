package contextevolver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/config"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/service"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockMemoryServiceForAgent 用于测试的 TaskMemoryService mock。
type mockMemoryServiceForAgent struct {
	retrieveFn     func(ctx context.Context, userID string, query string) (*service.RetrieveResult, error)
	loadMemoriesFn func(ctx context.Context, userID string) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// Retrieve 实现 Retrieve 方法。
func (m *mockMemoryServiceForAgent) Retrieve(ctx context.Context, userID string, query string) (*service.RetrieveResult, error) {
	if m.retrieveFn != nil {
		return m.retrieveFn(ctx, userID, query)
	}
	return &service.RetrieveResult{
		MemoryString:    "test memory content",
		RetrievedMemory: []ceschema.MemoryItem{ceschema.ReMeRetrievedMemory{WhenToUse: "test", Content: "mem1"}},
	}, nil
}

// LoadMemories 实现 LoadMemories 方法。
func (m *mockMemoryServiceForAgent) LoadMemories(_ context.Context, _ string) error {
	if m.loadMemoriesFn != nil {
		return m.loadMemoriesFn(nil, "")
	}
	return nil
}

// TestNewContextEvolvingReActAgent_基本构造 验证构造不报错。
func TestNewContextEvolvingReActAgent_基本构造(t *testing.T) {
	agent := &ContextEvolvingReActAgent{
		userID:                  "test-user",
		injectMemoriesInContext: true,
	}
	assert.NotNil(t, agent)
	assert.Equal(t, "test-user", agent.userID)
	assert.True(t, agent.injectMemoriesInContext)
}

// TestContextEvolvingReActAgent_Execute_实现AgentFlowService 验证 Execute 方法签名。
func TestContextEvolvingReActAgent_Execute_实现AgentFlowService(t *testing.T) {
	var _ cecontext.AgentFlowService = (*ContextEvolvingReActAgent)(nil)
}

// TestCopyMap 验证 copyMap 辅助函数。
func TestCopyMap(t *testing.T) {
	original := map[string]any{
		"query":   "hello",
		"matts_k": 3,
		"nested":  map[string]any{"key": "value"},
	}

	copied := copyMap(original)

	assert.Equal(t, original["query"], copied["query"])
	assert.Equal(t, original["matts_k"], copied["matts_k"])

	copied["query"] = "world"
	assert.Equal(t, "hello", original["query"])
}

// TestFormatTrajectoryFromResult 验证轨迹格式化。
func TestFormatTrajectoryFromResult(t *testing.T) {
	result := map[string]any{
		"output": "The answer is 42",
	}
	trajectory := formatTrajectoryFromResult(result)
	assert.Contains(t, trajectory, "The answer is 42")
}

// TestFormatTrajectoryFromResult_空结果 验证空结果返回空字符串。
func TestFormatTrajectoryFromResult_空结果(t *testing.T) {
	trajectory := formatTrajectoryFromResult(nil)
	assert.Equal(t, "", trajectory)

	trajectory = formatTrajectoryFromResult(map[string]any{})
	assert.Equal(t, "", trajectory)
}

// TestContextEvolvingReActAgent_MemoryCache 验证记忆缓存逻辑。
func TestContextEvolvingReActAgent_MemoryCache(t *testing.T) {
	agent := &ContextEvolvingReActAgent{
		userID:             "test-user",
		lastRetrievedQuery: "test-query",
		lastRetrievalResult: &service.RetrieveResult{
			MemoryString: "cached memory",
			RetrievedMemory: []ceschema.MemoryItem{
				ceschema.ReMeRetrievedMemory{WhenToUse: "w1", Content: "mem1"},
				ceschema.ReMeRetrievedMemory{WhenToUse: "w2", Content: "mem2"},
			},
		},
	}
	assert.Equal(t, "test-query", agent.lastRetrievedQuery)
	assert.NotNil(t, agent.lastRetrievalResult)
	assert.Equal(t, 2, len(agent.lastRetrievalResult.RetrievedMemory))
}

// TestMemoryAgentConfigInput 验证配置输入结构体。
func TestMemoryAgentConfigInput(t *testing.T) {
	cfg := MemoryAgentConfigInput{
		ModelProvider: "OpenAI",
		APIKey:        "test-key",
		APIBase:       "https://api.openai.com/v1",
		ModelName:     "gpt-5.2",
		MaxIterations: 10,
	}
	assert.Equal(t, "OpenAI", cfg.ModelProvider)
	assert.Equal(t, "gpt-5.2", cfg.ModelName)
}

// TestContextEvolvingReActAgent_GetMemoryService 验证 GetMemoryService。
func TestContextEvolvingReActAgent_GetMemoryService(t *testing.T) {
	agent := &ContextEvolvingReActAgent{}
	assert.Nil(t, agent.GetMemoryService())

	svc := &service.TaskMemoryService{}
	agent.memoryService = svc
	assert.Equal(t, svc, agent.GetMemoryService())
}

// TestContextEvolvingReActAgent_AutoConfigure_无APIKey 验证 API_KEY 缺失时静默跳过（对齐 Python）。
func TestContextEvolvingReActAgent_AutoConfigure_无APIKey(t *testing.T) {
	ceconfig.Delete("API_KEY")
	agent := &ContextEvolvingReActAgent{}
	err := agent.AutoConfigure(context.Background())
	// 对齐 Python: if not api_key: return（静默跳过，不返回错误）
	require.NoError(t, err)
}

// TestContextEvolvingReActAgent_AutoConfigure_有APIKey 验证有 API_KEY 时成功配置。
func TestContextEvolvingReActAgent_AutoConfigure_有APIKey(t *testing.T) {
	ceconfig.Set("API_KEY", "test-key-auto")
	defer ceconfig.Delete("API_KEY")

	agent := &ContextEvolvingReActAgent{
		ReActAgent: agents.NewReActAgent(&agentschema.AgentCard{}, saconfig.NewReActAgentConfig()),
	}
	err := agent.AutoConfigure(context.Background())
	require.NoError(t, err)
}

// TestAutoConfigure_不改动记忆服务 验证 AutoConfigure 只配置 Agent，不重建服务。
func TestAutoConfigure_不改动记忆服务(t *testing.T) {
	ceconfig.Set("API_KEY", "k")
	ceconfig.Set("MODEL_NAME", "gpt-4o")
	defer ceconfig.Delete("API_KEY")
	defer ceconfig.Delete("MODEL_NAME")

	before := &service.TaskMemoryService{}
	agent := &ContextEvolvingReActAgent{
		ReActAgent:    agents.NewReActAgent(&agentschema.AgentCard{}, saconfig.NewReActAgentConfig()),
		memoryService: before,
	}
	require.NoError(t, agent.AutoConfigure(context.Background()))
	assert.Same(t, before, agent.memoryService, "AutoConfigure 不应重建 memoryService")

	cfg := agent.ReActAgent.Config().(*saconfig.ReActAgentConfig)
	assert.Equal(t, "gpt-4o", cfg.ModelNameVal)
	assert.Equal(t, 5, cfg.MaxIterations)
}

// TestAutoConfigure_默认模型为gpt4 验证未配置 MODEL_NAME 时用 Python 的默认值 gpt-4。
func TestAutoConfigure_默认模型为gpt4(t *testing.T) {
	ceconfig.Set("API_KEY", "k")
	ceconfig.Delete("MODEL_NAME")
	defer ceconfig.Delete("API_KEY")

	agent := &ContextEvolvingReActAgent{
		ReActAgent: agents.NewReActAgent(&agentschema.AgentCard{}, saconfig.NewReActAgentConfig()),
	}
	require.NoError(t, agent.AutoConfigure(context.Background()))

	cfg := agent.ReActAgent.Config().(*saconfig.ReActAgentConfig)
	assert.Equal(t, "gpt-4", cfg.ModelNameVal)
}

// TestNewContextEvolvingReActAgent_双nil也创建服务 验证 service 为 nil 即创建（对齐 Python）。
func TestNewContextEvolvingReActAgent_双nil也创建服务(t *testing.T) {
	ceconfig.Set("API_KEY", "test-key")
	defer ceconfig.Delete("API_KEY")

	agent, err := NewContextEvolvingReActAgent(context.Background(), &agentschema.AgentCard{}, "u1",
		nil, true, false, "none", nil, "", "", 0, "")
	require.NoError(t, err)
	assert.NotNil(t, agent.GetMemoryService(), "memoryService 不应为 nil")
}

// TestNewContextEvolvingReActAgent_无APIKey报错 验证无密钥时构造失败（对齐 Python 抛错）。
func TestNewContextEvolvingReActAgent_无APIKey报错(t *testing.T) {
	t.Setenv("API_KEY", "")
	ceconfig.Delete("API_KEY")

	_, err := NewContextEvolvingReActAgent(context.Background(), &agentschema.AgentCard{}, "u1",
		nil, true, false, "none", nil, "", "", 0, "")
	assert.Error(t, err)
}
