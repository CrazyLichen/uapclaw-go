package rail

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestModelBackupRail_控制流中止不切换 rail 主动中止（bailout）不是模型故障：
// 备用 rail 必须不切模型、不请求重试。
func TestModelBackupRail_控制流中止不切换(t *testing.T) {
	t.Parallel()
	r := NewModelBackupRail([]*llm.Model{})
	agent := newFakeSwitcherAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
	cbc.SetException(controlFlowAbortErr())

	require.NoError(t, r.OnModelException(context.Background(), cbc))
	assert.Empty(t, agent.setLLMCalls, "控制流中止不应切换模型")
	assert.Zero(t, r.index, "备用索引不应推进")
	assert.Nil(t, cbc.ConsumeRetryRequest(), "控制流中止不应请求重试")
}

// TestModelBackupRail_普通故障照常切换 反向对照：没有标记的故障仍切模型 + 请求重试。
func TestModelBackupRail_普通故障照常切换(t *testing.T) {
	t.Parallel()
	backup := newTestModel("backup-cf")
	r := NewModelBackupRail([]*llm.Model{backup})
	agent := newFakeSwitcherAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
	cbc.SetException(exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg("provider 500"),
		exception.WithParams(map[string]any{"http_status": 500})))

	require.NoError(t, r.OnModelException(context.Background(), cbc))
	require.Len(t, agent.setLLMCalls, 1, "普通故障应切换模型")
	assert.Equal(t, 1, r.index)
	require.NotNil(t, cbc.ConsumeRetryRequest(), "普通故障应请求重试")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// controlFlowAbortErr 构造带控制流中止标记的异常（等价 bailout 的 cause）。
func controlFlowAbortErr() error {
	return exception.BuildError(exception.StatusContextExecutionError,
		exception.WithMsg("Detected that the model fell into repeated tool calls; this task has been automatically aborted."),
		exception.WithDetails(map[string]any{exception.DetailsKeyControlFlowAbort: true}))
}
