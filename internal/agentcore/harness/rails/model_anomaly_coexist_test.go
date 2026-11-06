package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentrail "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// switcherFakeAgent 在既有 fakeBaseAgent 上补 SetLLM/GetLLM，模拟可切换模型的 Agent。
type switcherFakeAgent struct {
	*fakeBaseAgent
	// setLLMCalls 记录 SetLLM 调用次数
	setLLMCalls int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestControlFlowAbort_与备用模型rail共存 验证同向与反向两种交互（priority 70 vs 50，单槽覆盖后写者赢）。
func TestControlFlowAbort_与备用模型rail共存(t *testing.T) {
	anomaly, err := NewModelAnomalyDetectionRail()
	require.NoError(t, err)
	backup := agentrail.NewModelBackupRail([]*llm.Model{nil})
	agent := &switcherFakeAgent{fakeBaseAgent: newFakeBaseAgent()}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)

	// 反向：控制流中止 —— 两条 rail 都不应继续
	cbc.SetException(exception.BuildError(exception.StatusContextExecutionError,
		exception.WithMsg("Detected that the model fell into repeated tool calls; this task has been automatically aborted."),
		exception.WithDetails(map[string]any{exception.DetailsKeyControlFlowAbort: true})))
	require.NoError(t, anomaly.OnModelException(context.Background(), cbc))
	require.NoError(t, backup.OnModelException(context.Background(), cbc))
	assert.Nil(t, cbc.ConsumeRetryRequest(), "中止场景两条 rail 都不应请求重试")
	assert.Zero(t, agent.setLLMCalls, "中止场景不应切换模型")

	// 同向：瞬时失败 —— anomaly 请求退避重试，backup 切模型并覆盖为立即重试
	cbc.SetException(exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg("rate limited"),
		exception.WithParams(map[string]any{"http_status": 429})))
	require.NoError(t, anomaly.OnModelException(context.Background(), cbc))
	require.NoError(t, backup.OnModelException(context.Background(), cbc))
	assert.Equal(t, 1, agent.setLLMCalls, "瞬时失败应切换模型")
	req := cbc.ConsumeRetryRequest()
	require.NotNil(t, req, "瞬时失败应请求重试")
	assert.Zero(t, req.DelaySeconds, "备用 rail 后写，覆盖为立即重试")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetLLM 记录切换次数。
func (f *switcherFakeAgent) SetLLM(_ *llm.Model) { f.setLLMCalls++ }

// GetLLM 返回 nil（测试不关心真实模型）。
func (f *switcherFakeAgent) GetLLM() (*llm.Model, error) { return nil, nil }
