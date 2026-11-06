package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	usage_snapshot "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/usage"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// usageStubAgent BaseAgent 桩：只实现 Config()，其余方法经嵌入接口转发（不会被调用）。
type usageStubAgent struct {
	sainterfaces.BaseAgent
	cfg sainterfaces.AgentConfig
}

// Config 返回被注入的 AgentConfig。
func (a *usageStubAgent) Config() sainterfaces.AgentConfig { return a.cfg }

// usageStubModelContext ModelContext 桩：emitContextUsage 需要 Statistic 做分类拆分。
type usageStubModelContext struct {
	ceiface.ModelContext
	// stats 返回的上下文统计
	stats ceiface.ContextStats
}

// Statistic 返回注入的统计快照。
func (m *usageStubModelContext) Statistic() *ceiface.ContextStats { return &m.stats }

// TokenCounter 返回 nil：分类计数走字符估算，避免测试依赖 tiktoken 资源。
func (m *usageStubModelContext) TokenCounter() token.TokenCounter { return nil }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEmitContextUsage_分母取ceConfig分子取总量 验证 context.usage 的口径：
// 分母来自 ContextEngineConfig.ModelContextWindowTokens，分子取 UsageMetadata.TotalTokens。
func TestEmitContextUsage_分母取ceConfig分子取总量(t *testing.T) {
	reactCfg := saconfig.NewReActAgentConfig()
	reactCfg.ModelNameVal = "mock-model"
	reactCfg.ContextEngineConfig.ModelContextWindowTokens = map[string]int{"mock-model": 32000}

	response := llmschema.NewAssistantMessage("ok")
	response.UsageMetadata = &llmschema.UsageMetadata{InputTokens: 8000, OutputTokens: 200, TotalTokens: 8200}
	inputs := &sainterfaces.ModelCallInputs{
		Response: response,
		Messages: []llmschema.BaseMessage{
			llmschema.NewSystemMessage("你是助手"),
			llmschema.NewUserMessage("问题"),
			llmschema.NewAssistantMessage("答复"),
		},
	}

	sess := &fakeSession{}
	cbCtx := sainterfaces.NewAgentCallbackContext(&usageStubAgent{cfg: reactCfg}, inputs, sess)
	cbCtx.SetModelContext(&usageStubModelContext{})

	emitContextUsage(context.Background(), cbCtx)

	require.Len(t, sess.written, 1, "应写入一条 context.usage 事件")
	out, ok := sess.written[0].(*stream.OutputSchema)
	require.True(t, ok, "写入类型应为 *stream.OutputSchema，实际 %T", sess.written[0])
	assert.Equal(t, "context.usage", out.Type)
	snapshot, ok := out.Payload.(usage_snapshot.ContextUsageSnapshot)
	require.True(t, ok, "载荷应为 context-usage.v1 快照，实际 %T", out.Payload)
	assert.Equal(t, usage_snapshot.EventTypeContextUsage, snapshot.EventType)
	assert.Equal(t, usage_snapshot.SchemaVersion, snapshot.SchemaVersion)
	assert.Equal(t, usage_snapshot.PhasePostCall, snapshot.Phase)
	require.NotEmpty(t, snapshot.RequestID, "request_id 必填（前端空值会丢弃整条快照）")
	require.NotEmpty(t, snapshot.ProductSessionID, "product_session_id 必填")
	require.NotNil(t, snapshot.ContextWindow.LimitTokens)
	assert.Equal(t, 32000, *snapshot.ContextWindow.LimitTokens, "窗口应取模型映射")
	require.NotNil(t, snapshot.ContextWindow.InputTokens)
	assert.Equal(t, 8000, *snapshot.ContextWindow.InputTokens, "输入量应取 provider 上报的 input_tokens")
	require.NotNil(t, snapshot.ContextWindow.OccupancyRate)
	assert.InDelta(t, 0.25, *snapshot.ContextWindow.OccupancyRate, 1e-6)
	assert.Len(t, snapshot.Parts, 4, "四类用量都应出现")
	assert.Positive(t, snapshot.Parts["system_prompt"].Tokens, "系统提示词分类应有非零值")
	assert.Positive(t, snapshot.Parts["messages"].Tokens, "消息分类应有非零值")
}
