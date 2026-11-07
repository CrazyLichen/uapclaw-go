package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// countingDrainRail 记录 BeforeSteeringDrain 触发次数与看到的 Pending。
type countingDrainRail struct {
	interfaces.BaseRail
	// fired 触发次数
	fired int
	// seenPending 最后一次看到的排队条数
	seenPending int
	// limitTo 非 nil 时写入 Limit
	limitTo *int
	// inputsKind 触发时 cbc.inputs 的类型名（诊断用）
	inputsKind string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCallbacks 只声明 BeforeSteeringDrain。
func (r *countingDrainRail) GetCallbacks() map[interfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(interfaces.CallbackBeforeSteeringDrain, r.onDrain),
	)
}

// TestDrainSteeringBatch_空队列不发事件 验证无积压时不触发事件。
//
// spec C3：Python 在 queue is None or not has_pending_steering() 时直接返回，
// 不构造 inputs、不 fire。
func TestDrainSteeringBatch_空队列不发事件(t *testing.T) {
	rail := &countingDrainRail{}
	cbc := newDrainCallbackContext(t, rail)

	msgs, err := drainSteeringBatch(context.Background(), cbc)
	require.NoError(t, err)
	require.Empty(t, msgs)
	require.Zero(t, rail.fired, "未绑定队列不得触发事件")

	ch := make(chan string, 4)
	cbc.BindSteeringQueue(ch)
	msgs, err = drainSteeringBatch(context.Background(), cbc)
	require.NoError(t, err)
	require.Empty(t, msgs)
	require.Zero(t, rail.fired, "队列为空不得触发事件")
}

// TestDrainSteeringBatch_有积压发事件 验证事件被触发且 Pending 正确。
//
// spec C3/C7。
func TestDrainSteeringBatch_有积压发事件(t *testing.T) {
	rail := &countingDrainRail{}
	cbc := newDrainCallbackContext(t, rail)
	ch := make(chan string, 8)
	cbc.BindSteeringQueue(ch)
	for i := 0; i < 5; i++ {
		require.NoError(t, cbc.PushSteering("m"))
	}

	msgs, err := drainSteeringBatch(context.Background(), cbc)
	require.NoError(t, err)
	require.Len(t, msgs, 5)
	require.Equal(t, 1, rail.fired)
	require.Equal(t, 5, rail.seenPending)
}

// TestDrainSteeringBatch_Limit生效 验证 rail 写的上限被采纳。
//
// spec C4。
func TestDrainSteeringBatch_Limit生效(t *testing.T) {
	limit := 2
	rail := &countingDrainRail{limitTo: &limit}
	cbc := newDrainCallbackContext(t, rail)
	ch := make(chan string, 8)
	cbc.BindSteeringQueue(ch)
	for i := 0; i < 5; i++ {
		require.NoError(t, cbc.PushSteering(string(rune('a'+i))))
	}

	msgs, err := drainSteeringBatch(context.Background(), cbc)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, msgs)
	require.Equal(t, 3, cbc.SteeringPendingCount(), "剩余必须留队")
}

// TestDrainSteeringBatch_inputs还原 验证事件后 ctx.inputs 复原。
//
// spec C11 同类契约：事件输入是临时替换，必须 finally 还原。
func TestDrainSteeringBatch_inputs还原(t *testing.T) {
	rail := &countingDrainRail{}
	cbc := newDrainCallbackContext(t, rail)
	ch := make(chan string, 4)
	cbc.BindSteeringQueue(ch)
	require.NoError(t, cbc.PushSteering("a"))

	_, err := drainSteeringBatch(context.Background(), cbc)
	require.NoError(t, err)
	require.Equal(t, "steering_drain_inputs", rail.inputsKind, "事件期间 inputs 应为 SteeringDrainInputs")
	_, isInvokeInputs := cbc.Inputs().(*interfaces.InvokeInputs)
	require.True(t, isInvokeInputs, "事件结束后 inputs 必须还原为 InvokeInputs")
}

// onDrain 记录并可选写入 Limit。
func (r *countingDrainRail) onDrain(_ context.Context, railCtx any) error {
	r.fired++
	cbc, ok := railCtx.(*interfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	in, ok := cbc.Inputs().(*interfaces.SteeringDrainInputs)
	if !ok {
		r.inputsKind = "other"
		return nil
	}
	r.inputsKind = in.EventKind()
	r.seenPending = in.Pending
	if r.limitTo != nil {
		v := *r.limitTo
		in.Limit = &v
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newDrainCallbackContext 构造挂了指定 rail 的回调上下文。
func newDrainCallbackContext(t *testing.T, rail *countingDrainRail) *interfaces.AgentCallbackContext {
	t.Helper()
	agent := newTestAgent("drain-test")
	for ev, fn := range rail.GetCallbacks() {
		agent.CallbackManager().RegisterCallback(context.Background(), ev, fn)
	}
	return interfaces.NewAgentCallbackContext(agent, &interfaces.InvokeInputs{}, nil)
}
