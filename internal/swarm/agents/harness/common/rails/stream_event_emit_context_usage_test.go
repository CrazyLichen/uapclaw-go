package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
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

// usageStubModelContext ModelContext 桩：emitContextUsage 只做 nil 判断。
type usageStubModelContext struct {
	ceiface.ModelContext
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEmitContextUsage_分母取ceConfig分子取总量 验证 context.usage 的口径：
// 分母来自 ContextEngineConfig.ModelContextWindowTokens，分子取 UsageMetadata.TotalTokens。
func TestEmitContextUsage_分母取ceConfig分子取总量(t *testing.T) {
	reactCfg := saconfig.NewReActAgentConfig()
	reactCfg.ModelNameVal = "mock-model"
	reactCfg.ContextEngineConfig.ModelContextWindowTokens = map[string]int{"mock-model": 32000}

	response := llmschema.NewAssistantMessage("ok")
	response.UsageMetadata = &llmschema.UsageMetadata{InputTokens: 8000, OutputTokens: 200, TotalTokens: 8200}
	inputs := &sainterfaces.ModelCallInputs{Response: response}

	sess := &fakeSession{}
	cbCtx := sainterfaces.NewAgentCallbackContext(&usageStubAgent{cfg: reactCfg}, inputs, sess)
	cbCtx.SetModelContext(&usageStubModelContext{})

	emitContextUsage(context.Background(), cbCtx)

	require.Len(t, sess.written, 1, "应写入一条 context.usage 事件")
	out, ok := sess.written[0].(*stream.OutputSchema)
	require.True(t, ok, "写入类型应为 *stream.OutputSchema，实际 %T", sess.written[0])
	assert.Equal(t, "context.usage", out.Type)
	payload, ok := out.Payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 32000, payload["context_max"], "分母应取模型窗口映射")
	assert.Equal(t, 8200, payload["tokens_used"], "分子应取 total_tokens（输入+输出）")
	assert.InDelta(t, 25.625, payload["rate"], 1e-6)
}
