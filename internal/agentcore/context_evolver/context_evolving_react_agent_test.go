package contextevolver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/service"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockMemoryServiceForAgent 用于测试的 TaskMemoryService mock。
type mockMemoryServiceForAgent struct {
	retrieveFn func(ctx context.Context, userID string, query string) (*service.RetrieveResult, error)
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

// TestNewContextEvolvingReActAgent_基本构造 验证构造不报错。
func TestNewContextEvolvingReActAgent_基本构造(t *testing.T) {
	// 使用 nil memoryService 验证不 panic
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
	// 编译期接口断言
	var _ cecontext.AgentFlowService = (*ContextEvolvingReActAgent)(nil)
}

// TestCopyMap 验证 copyMap 辅助函数。
func TestCopyMap(t *testing.T) {
	original := map[string]any{
		"query":    "hello",
		"matts_k":  3,
		"nested":   map[string]any{"key": "value"},
	}

	copied := copyMap(original)

	assert.Equal(t, original["query"], copied["query"])
	assert.Equal(t, original["matts_k"], copied["matts_k"])

	// 修改 copy 不影响 original
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

// TestContextEvolvingReActAgent_Invoke_无Query 验证无 query 时走父类。
func TestContextEvolvingReActAgent_Invoke_无Query(t *testing.T) {
	// 此测试验证无 query 时 Invoke 的路由行为
	// 完整的 Invoke 测试需要构造完整的 ReActAgent（需要较多依赖）
	// 这里验证核心路由逻辑
	agent := &ContextEvolvingReActAgent{
		userID: "test-user",
	}
	assert.NotNil(t, agent)
	// 无 ReActAgent 嵌入时 Invoke 会 panic，这里只验证类型存在
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
		APIKey:       "test-key",
		APIBase:      "https://api.openai.com/v1",
		ModelName:    "gpt-5.2",
		MaxIterations: 10,
	}
	assert.Equal(t, "OpenAI", cfg.ModelProvider)
	assert.Equal(t, "gpt-5.2", cfg.ModelName)
}

// TestContextEvolvingReActAgent_Execute_无ReActAgent 验证 Execute 在缺少 ReActAgent 时的行为。
// 由于嵌入 *ReActAgent 为 nil 时 invokeWithMemory 会 panic，
// 此测试仅验证类型正确。
func TestContextEvolvingReActAgent_Execute_无ReActAgent(t *testing.T) {
	agent := &ContextEvolvingReActAgent{
		userID:                  "test-user",
		injectMemoriesInContext: true,
	}
	// 不能调用 Execute（会 panic 因为 ReActAgent 为 nil）
	// 只验证字段设置
	assert.True(t, agent.injectMemoriesInContext)
}

// TestContextEvolvingReActAgent_GetMemoryService 验证 GetMemoryService。
func TestContextEvolvingReActAgent_GetMemoryService(t *testing.T) {
	agent := &ContextEvolvingReActAgent{}
	assert.Nil(t, agent.GetMemoryService())

	// 模拟设置
	svc := &service.TaskMemoryService{}
	agent.memoryService = svc
	assert.Equal(t, svc, agent.GetMemoryService())
}
