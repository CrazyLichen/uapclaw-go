package rails

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// TestProcessCliIterationLimitRail_优先级分档 验证两档优先级。
// Python: iteration_limit.py:26-29 callback_priority
func TestProcessCliIterationLimitRail_优先级分档(t *testing.T) {
	r := NewProcessCliIterationLimitRail(5)
	require.Equal(t, -1000, r.CallbackPriorityFor(agentinterfaces.CallbackAfterReactIteration))
	require.Equal(t, 1000, r.CallbackPriorityFor(agentinterfaces.CallbackBeforeModelCall))
}

// TestProcessCliIterationLimitRail_超限请求finish 验证超上限时请求终止。
// Python: iteration_limit.py:31-34 before_model_call react_iteration > max
func TestProcessCliIterationLimitRail_超限请求finish(t *testing.T) {
	r := NewProcessCliIterationLimitRail(2)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{ReactIteration: 3}, nil)

	require.NoError(t, r.BeforeModelCall(t.Context(), cbc))
	require.True(t, cbc.HasForceFinishRequest(), "超过上限应请求终止")
}

// TestProcessCliIterationLimitRail_未超限不请求finish 验证边界。
// Python: iteration_limit.py:31-34 用 > （严格大于）
func TestProcessCliIterationLimitRail_未超限不请求finish(t *testing.T) {
	r := NewProcessCliIterationLimitRail(2)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{ReactIteration: 2}, nil)

	require.NoError(t, r.BeforeModelCall(t.Context(), cbc))
	require.False(t, cbc.HasForceFinishRequest(), "等于上限时 before_model_call 不该终止")
}

// TestProcessCliIterationLimitRail_已达上限在after请求finish 验证 after 判定用 >=。
// Python: iteration_limit.py:36-39 after_react_iteration react_iteration >= max
func TestProcessCliIterationLimitRail_已达上限在after请求finish(t *testing.T) {
	r := NewProcessCliIterationLimitRail(2)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{ReactIteration: 2}, nil)

	require.NoError(t, r.AfterReactIteration(t.Context(), cbc))
	require.True(t, cbc.HasForceFinishRequest(), "达到上限时 after_react_iteration 应终止")
}

// TestProcessCliIterationLimitRail_已有finish时不重复请求 验证不覆盖别人的请求。
// Python: iteration_limit.py:44 if exhausted and not ctx.has_force_finish_request
func TestProcessCliIterationLimitRail_已有finish时不重复请求(t *testing.T) {
	r := NewProcessCliIterationLimitRail(1)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{ReactIteration: 5}, nil)
	cbc.RequestForceFinish(map[string]any{"output": "other rail"})

	require.NoError(t, r.BeforeModelCall(t.Context(), cbc))
	consumed := cbc.ConsumeForceFinish()
	require.NotNil(t, consumed)
	require.Equal(t, "other rail", consumed.Result["output"], "不该覆盖已存在的 finish 请求")
}

// TestProcessCliIterationLimitRail_有异常时不请求 验证 exception 优先。
// Python: iteration_limit.py:44 and ctx.exception is None
func TestProcessCliIterationLimitRail_有异常时不请求(t *testing.T) {
	r := NewProcessCliIterationLimitRail(1)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{ReactIteration: 5}, nil)
	cbc.SetException(assert.AnError)

	require.NoError(t, r.BeforeModelCall(t.Context(), cbc))
	require.False(t, cbc.HasForceFinishRequest())
}

// TestProcessCliIterationLimitRail_非ModelCallInputs跳过 验证类型守卫。
// Python: iteration_limit.py:33 if isinstance(inputs, ModelCallInputs)
func TestProcessCliIterationLimitRail_非ModelCallInputs跳过(t *testing.T) {
	r := NewProcessCliIterationLimitRail(1)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)

	require.NoError(t, r.BeforeModelCall(t.Context(), cbc))
	require.False(t, cbc.HasForceFinishRequest())
}
