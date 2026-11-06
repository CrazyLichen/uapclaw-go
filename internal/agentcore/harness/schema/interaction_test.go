package schema

import (
	"context"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeOutputDriver 测试用输出驱动
type fakeOutputDriver struct {
	items       []stream.Schema
	nextCalls   int
	detached    []string
	abortValues []bool
}

// NextOutput 依次返回预置帧，取尽后返回 false
func (f *fakeOutputDriver) NextOutput(_ context.Context, _ *OutputLease) (stream.Schema, bool) {
	if f.nextCalls >= len(f.items) {
		f.nextCalls++
		return nil, false
	}
	item := f.items[f.nextCalls]
	f.nextCalls++
	return item, true
}

// DetachOutput 记录释放调用
func (f *fakeOutputDriver) DetachOutput(_ context.Context, token string, abortActiveRound bool) {
	f.detached = append(f.detached, token)
	f.abortValues = append(f.abortValues, abortActiveRound)
}

// fakeGoalCoordinator 测试用 goal 协调器（S2/S3 复用）
type fakeGoalCoordinator struct {
	active       bool
	ensureCalls  int
	beginCalls   int
	beginStarted bool
}

// HasActiveRecord 返回预置的 ACTIVE 标记
func (f *fakeGoalCoordinator) HasActiveRecord() bool { return f.active }

// EnsureActiveGoalWorkLocked 记录补排调用
func (f *fakeGoalCoordinator) EnsureActiveGoalWorkLocked() { f.ensureCalls++ }

// BeginAttempt 记录尝试调用
func (f *fakeGoalCoordinator) BeginAttempt(_ context.Context, _ string, _ int) (bool, error) {
	f.beginCalls++
	return f.beginStarted, nil
}

// ──────────────────────────── 相位与 work item ────────────────────────────

// TestInteractionPhase_值对齐Python 验证相位枚举取值
// Python: InteractionPhase (interaction.py L28)
func TestInteractionPhase_值对齐Python(t *testing.T) {
	assert.Equal(t, InteractionPhase("idle"), InteractionPhaseIdle)
	assert.Equal(t, InteractionPhase("running"), InteractionPhaseRunning)
	assert.Equal(t, InteractionPhase("terminated"), InteractionPhaseTerminated)
}

// TestInputDispatchMode_值对齐Python 验证分发模式取值
// Python: InputDispatchMode (L36)
func TestInputDispatchMode_值对齐Python(t *testing.T) {
	assert.Equal(t, InputDispatchMode("follow_up"), InputDispatchModeFollowUp)
	assert.Equal(t, InputDispatchMode("steer"), InputDispatchModeSteer)
}

// TestNewUserWorkItem_默认重置循环且深拷贝入参 验证 user 工厂
// Python: RoundWorkItem.user (L69) —— reset_loop 默认 True、inputs 深拷贝
func TestNewUserWorkItem_默认重置循环且深拷贝入参(t *testing.T) {
	inputs := map[string]any{
		"query": "修完所有 TODO",
		"meta":  map[string]any{"k": "v"},
		"list":  []any{"a"},
	}
	work := NewUserWorkItem("req-1", inputs, false, true)

	require.NotNil(t, work)
	assert.Equal(t, RoundWorkKindUser, work.Kind)
	assert.Equal(t, "req-1", work.RequestID)
	assert.False(t, work.IsFollowUp)
	assert.True(t, work.ResetLoop())
	assert.Equal(t, "", work.Context.GoalID)

	// 深拷贝：改原 map 不影响 work item
	inputs["query"] = "改掉了"
	inputs["meta"].(map[string]any)["k"] = "改掉了"
	inputs["list"].([]any)[0] = "改掉了"
	assert.Equal(t, "修完所有 TODO", work.Query())
	assert.Equal(t, "v", work.Inputs["meta"].(map[string]any)["k"])
	assert.Equal(t, "a", work.Inputs["list"].([]any)[0])
}

// TestNewUserWorkItem_FollowUp不重置循环 验证续跑语义
// Python: is_follow_up=True 时 reset_loop=False（_send_user L4153-4160）
func TestNewUserWorkItem_FollowUp不重置循环(t *testing.T) {
	work := NewUserWorkItem("req-2", map[string]any{"query": "补充"}, true, false)
	assert.True(t, work.IsFollowUp)
	assert.False(t, work.ResetLoop())
}

// TestNewGoalWorkItem_字段与重置循环 验证 goal 工厂
// Python: RoundWorkItem.goal (L87)
func TestNewGoalWorkItem_字段与重置循环(t *testing.T) {
	work := NewGoalWorkItem(map[string]any{"query": "<goal_task/>"}, "goal-1", 3, "sess-1")

	require.NotNil(t, work)
	assert.Equal(t, RoundWorkKindGoal, work.Kind)
	assert.Equal(t, "", work.RequestID)
	assert.False(t, work.IsFollowUp)
	assert.True(t, work.ResetLoop())
	assert.Equal(t, "goal-1", work.Context.GoalID)
	assert.Equal(t, 3, work.Context.Revision)
	assert.Equal(t, "sess-1", work.Context.SessionID)
}

// TestWorkItem_Query仅来自inputs 验证 query 派生规则
// Python: RoundWorkItem.query (L109)
func TestWorkItem_Query仅来自inputs(t *testing.T) {
	work := NewUserWorkItem("r", map[string]any{"query": "hi"}, false, true)
	assert.Equal(t, "hi", work.Query())

	empty := NewUserWorkItem("r", map[string]any{}, false, true)
	assert.Nil(t, empty.Query())
}

// TestActiveInteractionRound_代理属性 验证运行轮次视图
// Python: ActiveInteractionRound (L119)
func TestActiveInteractionRound_代理属性(t *testing.T) {
	work := NewGoalWorkItem(map[string]any{"query": "q"}, "goal-9", 2, "sess-9")
	round := &ActiveInteractionRound{Work: work, TaskID: "task-1"}

	assert.Equal(t, RoundWorkKindGoal, round.RunKind())
	assert.Equal(t, "goal-9", round.GoalID())
	assert.Equal(t, 2, round.Revision())
	assert.Equal(t, "sess-9", round.RunContext().SessionID)
	assert.Equal(t, "task-1", round.TaskID)
}

// ──────────────────────────── 事件与结果 ────────────────────────────

// TestInteractionEventType_值对齐Python 验证事件类型取值
func TestInteractionEventType_值对齐Python(t *testing.T) {
	assert.Equal(t, InteractionEventType("goal.updated"), InteractionEventTypeGoalUpdated)
	assert.Equal(t, InteractionEventType("execution.error"), InteractionEventTypeExecutionError)
}

// TestInteractionEvent_GoalUpdated 验证 goal.updated 事件载荷
// Python: InteractionEvent.goal_updated (L160)
func TestInteractionEvent_GoalUpdated(t *testing.T) {
	goal := map[string]any{"goal_id": "g1", "status": "active"}
	event := NewGoalUpdatedEvent(goal)
	goal["status"] = "改掉了"

	assert.Equal(t, InteractionEventType("goal.updated"), event.Type)
	payloadGoal := event.Payload["goal"].(map[string]any)
	assert.Equal(t, "active", payloadGoal["status"], "payload 必须深拷贝")

	outputSchema := event.ToOutputSchema()
	assert.Equal(t, "goal.updated", outputSchema.Type)
	assert.Equal(t, 0, outputSchema.Index)
}

// TestInteractionEvent_GoalUpdated_Nil 验证 goal 为空时的载荷形状
func TestInteractionEvent_GoalUpdated_Nil(t *testing.T) {
	event := NewGoalUpdatedEvent(nil)
	value, ok := event.Payload["goal"]
	assert.True(t, ok)
	assert.Nil(t, value)
}

// TestInteractionEvent_ExecutionError 验证 execution.error 事件载荷
// Python: InteractionEvent.execution_error (L167)
func TestInteractionEvent_ExecutionError(t *testing.T) {
	event := NewExecutionErrorEvent("round_execution_error", "boom", nil)
	assert.Equal(t, InteractionEventType("execution.error"), event.Type)
	assert.Equal(t, "round_execution_error", event.Payload["code"])
	assert.Equal(t, "boom", event.Payload["message"])
	_, hasGoal := event.Payload["goal"]
	assert.False(t, hasGoal, "goal 为 nil 时不写入 payload")

	withGoal := NewExecutionErrorEvent("c", "m", map[string]any{"goal_id": "g1"})
	assert.NotNil(t, withGoal.Payload["goal"])
}

// TestRoundOutcome_零值语义 验证结果结构
// Python: RoundOutcome (L181)
func TestRoundOutcome_零值语义(t *testing.T) {
	outcome := RoundOutcome{}
	assert.Nil(t, outcome.NextWork)
	assert.Equal(t, "", outcome.ErrorCode)
	assert.Equal(t, "", outcome.ErrorMessage)

	work := NewUserWorkItem("r", map[string]any{"query": "q"}, true, false)
	outcome.NextWork = work
	assert.Same(t, work, outcome.NextWork)
}

// 编译期断言：InteractionEvent.ToOutputSchema 返回标准流帧
var _ stream.Schema = (*stream.OutputSchema)(nil)

// ──────────────────────────── 租约与输出句柄 ────────────────────────────

// TestOutputLease_关闭幂等 验证租约关闭语义
// Python: OutputLease.closed (asyncio.Event) (L194)
func TestOutputLease_关闭幂等(t *testing.T) {
	lease := NewOutputLease("tok")
	assert.Equal(t, "tok", lease.Token)
	assert.False(t, lease.IsClosed())

	lease.Close()
	lease.Close()
	assert.True(t, lease.IsClosed())
}

// TestInteractionOutputStream_Close传递票号与abort标志 验证句柄释放语义
// Python: InteractionOutputStream.close (L226)
func TestInteractionOutputStream_Close传递票号与abort标志(t *testing.T) {
	driver := &fakeOutputDriver{}
	lease := NewOutputLease("tok-1")
	handle := NewInteractionOutputStream(driver, lease)

	handle.Close(context.Background(), true)
	assert.Equal(t, []string{"tok-1"}, driver.detached)
	assert.Equal(t, []bool{true}, driver.abortValues)

	// 重复 Close 不得再次 detach（Python: if self._closed: return）
	handle.Close(context.Background(), true)
	assert.Len(t, driver.detached, 1)
}

// TestInteractionOutputStream_Next读到nil即结束 验证读循环终止
// Python: InteractionOutputStream.__anext__ (L214)
func TestInteractionOutputStream_Next读到nil即结束(t *testing.T) {
	driver := &fakeOutputDriver{items: []stream.Schema{
		&stream.OutputSchema{Type: "chunk", Index: 0},
	}}
	handle := NewInteractionOutputStream(driver, NewOutputLease("t"))

	item, ok := handle.Next(context.Background())
	require.True(t, ok)
	assert.Equal(t, "chunk", item.(*stream.OutputSchema).Type)

	_, ok = handle.Next(context.Background())
	assert.False(t, ok)

	// 结束后再读仍返回 false（句柄已 closed）
	_, ok = handle.Next(context.Background())
	assert.False(t, ok)
}

// TestInteractionOutputStream_All遍历 验证 iter.Seq 出口
func TestInteractionOutputStream_All遍历(t *testing.T) {
	driver := &fakeOutputDriver{items: []stream.Schema{
		&stream.OutputSchema{Type: "a"},
		&stream.OutputSchema{Type: "b"},
	}}
	handle := NewInteractionOutputStream(driver, NewOutputLease("t"))

	var types []string
	for item := range handle.All(context.Background()) {
		types = append(types, item.(*stream.OutputSchema).Type)
	}
	assert.Equal(t, []string{"a", "b"}, types)
}

// 编译期断言：iter.Seq 出口形状
var _ func(context.Context) iter.Seq[stream.Schema] = (*InteractionOutputStream)(nil).All
