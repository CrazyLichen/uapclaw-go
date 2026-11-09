package schema

import (
	"context"
	"iter"
	"testing"
	"time"

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

// ──────────────────────────── 租约管理器 ────────────────────────────

// TestOutputLeaseManager_单消费者 验证同一时刻只有一张票
// Python: OutputLeaseManager.attach (L259)
func TestOutputLeaseManager_单消费者(t *testing.T) {
	m := NewOutputLeaseManager()

	first := m.Attach()
	require.NotNil(t, first)
	assert.True(t, m.HasConsumer())
	assert.Equal(t, first.Token, m.CurrentToken())
	assert.Len(t, first.Token, 32, "票号对齐 uuid4().hex 的 32 位")

	second := m.Attach()
	assert.Nil(t, second, "已有消费者时不得再发第二张票")
	assert.Equal(t, first.Token, m.CurrentToken())
}

// TestOutputLeaseManager_陈旧票无法撤销新租约 验证代沟保护
// Python: OutputLeaseManager.detach (L267)
func TestOutputLeaseManager_陈旧票无法撤销新租约(t *testing.T) {
	m := NewOutputLeaseManager()

	old := m.Attach()
	require.NotNil(t, old)
	assert.True(t, m.Detach(old.Token, true))
	assert.Nil(t, m.CurrentLease())

	fresh := m.Attach()
	require.NotNil(t, fresh)

	assert.False(t, m.Detach(old.Token, true), "旧票号不得撤销新租约")
	assert.Equal(t, fresh.Token, m.CurrentToken())
	assert.False(t, fresh.IsClosed())
}

// TestOutputLeaseManager_Shutdown 验证关闭语义
// Python: OutputLeaseManager.shutdown (L340)
func TestOutputLeaseManager_Shutdown(t *testing.T) {
	m := NewOutputLeaseManager()
	lease := m.Attach()
	require.NotNil(t, lease)

	m.Shutdown()
	assert.True(t, lease.IsClosed())
	assert.Nil(t, m.CurrentLease())
	assert.False(t, m.HasConsumer())
	assert.Nil(t, m.Attach(), "关闭后不得再发票")
}

// TestOutputLeaseManager_Detach后读不到 验证幽灵读者不会偷下一条
// Python: next_item 的 closed 分支 (L297-338)：脱离的 queue.get 任务不得
// 存活并偷走下一个租约的第一条输出
func TestOutputLeaseManager_Detach后读不到(t *testing.T) {
	m := NewOutputLeaseManager()
	lease := m.Attach()
	require.NotNil(t, lease)

	done := make(chan struct{})
	var got bool
	go func() {
		defer close(done)
		_, got = m.NextItem(context.Background(), lease)
	}()

	assert.True(t, m.Detach(lease.Token, false))
	<-done
	assert.False(t, got, "租约作废后挂起的读取必须返回 false")
}

// TestOutputLeaseManager_幽灵读者不偷新租约首帧 验证代际隔离
func TestOutputLeaseManager_幽灵读者不偷新租约首帧(t *testing.T) {
	m := NewOutputLeaseManager()
	dead := m.Attach()
	require.NotNil(t, dead)

	halt := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		<-halt
		_, _ = m.NextItem(context.Background(), dead)
	}()

	assert.True(t, m.Detach(dead.Token, true))
	close(halt)
	<-readerDone

	fresh := m.Attach()
	require.NotNil(t, fresh)
	m.Emit(&stream.OutputSchema{Type: "first"}, "")

	item, ok := m.NextItem(context.Background(), fresh)
	require.True(t, ok)
	assert.Equal(t, "first", item.(*stream.OutputSchema).Type)
}

// TestOutputLeaseManager_无票Emit丢弃 验证无消费者时不入队
// Python: emit 的 not has_consumer 分支 (L289)
func TestOutputLeaseManager_无票Emit丢弃(t *testing.T) {
	m := NewOutputLeaseManager()
	m.Emit(&stream.OutputSchema{Type: "chunk"}, "")

	lease := m.Attach()
	require.NotNil(t, lease)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, ok := m.NextItem(ctx, lease)
	assert.False(t, ok, "无消费者期间 emit 的帧不得被新租约读到")
}

// TestOutputLeaseManager_ExpectedToken不匹配则丢弃 验证延迟事件防错发
// Python: emit 的 expected_token 分支 (L291)
func TestOutputLeaseManager_ExpectedToken不匹配则丢弃(t *testing.T) {
	m := NewOutputLeaseManager()
	lease := m.Attach()
	require.NotNil(t, lease)

	m.Emit(&stream.OutputSchema{Type: "stale"}, "other-token")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, ok := m.NextItem(ctx, lease)
	assert.False(t, ok)
}

// TestOutputLeaseManager_Finishing后Emit丢弃 验证收尾后不再收新数据
// Python: emit 的 finishing 分支 (L293)
func TestOutputLeaseManager_Finishing后Emit丢弃(t *testing.T) {
	m := NewOutputLeaseManager()
	lease := m.Attach()
	require.NotNil(t, lease)

	m.FinishCurrent()
	m.Emit(&stream.OutputSchema{Type: "late"}, "")

	item, ok := m.NextItem(context.Background(), lease)
	assert.False(t, ok)
	assert.Nil(t, item)
}

// TestOutputLeaseManager_FinishCurrent顺序 验证哨兵排在已入队帧之后
// Python: finish_current 的 put_nowait(_OUTPUT_END) (L285)
func TestOutputLeaseManager_FinishCurrent顺序(t *testing.T) {
	m := NewOutputLeaseManager()
	lease := m.Attach()
	require.NotNil(t, lease)

	for _, typ := range []string{"a", "b", "c"} {
		m.Emit(&stream.OutputSchema{Type: typ}, "")
	}
	m.FinishCurrent()

	var got []string
	for {
		item, ok := m.NextItem(context.Background(), lease)
		if !ok {
			break
		}
		got = append(got, item.(*stream.OutputSchema).Type)
	}
	assert.Equal(t, []string{"a", "b", "c"}, got)
	assert.Nil(t, m.CurrentLease(), "读到哨兵后租约应被清空")
	assert.True(t, lease.IsClosed())
}

// TestOutputLeaseManager_FinishCurrent队列满不丢哨兵 验证结束信号优先
func TestOutputLeaseManager_FinishCurrent队列满不丢哨兵(t *testing.T) {
	m := NewOutputLeaseManager()
	lease := m.Attach()
	require.NotNil(t, lease)

	for i := 0; i < outputQueueSize; i++ {
		m.Emit(&stream.OutputSchema{Type: "chunk"}, "")
	}
	m.FinishCurrent()

	count := 0
	for {
		_, ok := m.NextItem(context.Background(), lease)
		if !ok {
			break
		}
		count++
	}
	assert.Equal(t, outputQueueSize-1, count, "队首一帧被丢弃，其余保留，哨兵最终生效")
}

// TestOutputLeaseManager_队列满丢新帧 验证有界丢弃策略
func TestOutputLeaseManager_队列满丢新帧(t *testing.T) {
	m := NewOutputLeaseManager()
	lease := m.Attach()
	require.NotNil(t, lease)

	for i := 0; i < outputQueueSize; i++ {
		m.Emit(&stream.OutputSchema{Type: "chunk", Index: i}, "")
	}
	m.Emit(&stream.OutputSchema{Type: "overflow"}, "")

	assert.Equal(t, int64(1), m.DroppedCount())
	assert.Equal(t, outputQueueSize, len(m.queue))

	item, ok := m.NextItem(context.Background(), lease)
	require.True(t, ok)
	assert.Equal(t, 0, item.(*stream.OutputSchema).Index, "丢的是新帧，队首仍是 index=0")
}

// TestOutputLeaseManager_丢帧日志限流 验证 1000 次丢弃只写 1 条日志
func TestOutputLeaseManager_丢帧日志限流(t *testing.T) {
	m := NewOutputLeaseManager()
	lease := m.Attach()
	require.NotNil(t, lease)
	m.dropLogInterval = time.Hour

	for i := 0; i < outputQueueSize; i++ {
		m.Emit(&stream.OutputSchema{Type: "chunk"}, "")
	}
	for i := 0; i < 1000; i++ {
		m.Emit(&stream.OutputSchema{Type: "overflow"}, "")
	}

	assert.Equal(t, int64(1000), m.dropped.Load())
	assert.Equal(t, int64(1), m.dropLogCount.Load(), "限流窗口内只写一条日志")
}

// TestOutputLeaseManager_透传非OutputSchema帧 验证队列类型是 stream.Schema
// spec U49：收成 *OutputSchema 会静默丢 trace 帧
func TestOutputLeaseManager_透传非OutputSchema帧(t *testing.T) {
	m := NewOutputLeaseManager()
	lease := m.Attach()
	require.NotNil(t, lease)

	trace := &stream.TraceSchema{}
	m.Emit(trace, "")

	item, ok := m.NextItem(context.Background(), lease)
	require.True(t, ok)
	assert.Same(t, trace, item)
}

// TestOutputLeaseManager_NextItem后Detach为noop 验证读到哨兵后 close 无害
// Python: detach 在 lease 为 None 时返回 False (L270)
func TestOutputLeaseManager_NextItem后Detach为noop(t *testing.T) {
	m := NewOutputLeaseManager()
	lease := m.Attach()
	require.NotNil(t, lease)

	m.FinishCurrent()
	_, ok := m.NextItem(context.Background(), lease)
	require.False(t, ok)

	assert.False(t, m.Detach(lease.Token, true), "租约已因哨兵清空，detach 应为 noop")
}

// ──────────────────────────── 边界与空值 ────────────────────────────

// TestInteractionOutputStream_Lease与提前退出 验证访问器与 All 提前中断
func TestInteractionOutputStream_Lease与提前退出(t *testing.T) {
	driver := &fakeOutputDriver{items: []stream.Schema{
		&stream.OutputSchema{Type: "a"},
		&stream.OutputSchema{Type: "b"},
	}}
	lease := NewOutputLease("tok")
	handle := NewInteractionOutputStream(driver, lease)
	assert.Same(t, lease, handle.Lease())

	count := 0
	for range handle.All(context.Background()) {
		count++
		break
	}
	assert.Equal(t, 1, count, "yield 返回 false 时提前结束")
}

// TestWorkItem_空接收者语义 验证 nil 安全
// Python: reset_loop 的 get("reset_loop", True) 默认值
func TestWorkItem_空接收者语义(t *testing.T) {
	var work *RoundWorkItem
	assert.Nil(t, work.Query())
	assert.True(t, work.ResetLoop(), "nil 接收者按 Python 默认 reset_loop=True")
}

// TestOutputLeaseManager_CurrentToken无租约 验证空票号
func TestOutputLeaseManager_CurrentToken无租约(t *testing.T) {
	m := NewOutputLeaseManager()
	assert.Equal(t, "", m.CurrentToken())
}

// TestNextItem_ctx取消释放租约槽位 Go 侧独有的取消路径必须清槽位。
//
// Python 的 next_output(self._lease) 不接受 ctx，不存在"消费者被取消但槽位
// 仍被占"的状态；Go 给 NextItem 加了 ctx.Done() 早退，若不在此清掉 m.lease，
// 句柄会被 Next 标成 closed 使后续 Close 变 no-op，于是租约永远指向死句柄、
// AttachOutput 从此一律返回 nil（会话被永久锁死，supplement 打断后的新一轮
// 就是这么丢的）。
func TestNextItem_ctx取消释放租约槽位(t *testing.T) {
	m := NewOutputLeaseManager()

	first := m.Attach()
	require.NotNil(t, first, "空闲时第一次 attach 必须成功")
	require.True(t, m.HasConsumer())

	// 消费者被取消（模拟网关断连 / 请求 ctx 结束）
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, ok := m.NextItem(ctx, first)
	require.False(t, ok, "ctx 取消后必须返回 false")

	// 槽位必须已释放：下一位消费者能 attach
	require.False(t, m.HasConsumer(), "取消后不得再认为有消费者")
	second := m.Attach()
	require.NotNil(t, second, "取消后下一位消费者必须能 attach（槽位已释放）")
	require.True(t, m.HasConsumer())

	m.Detach(second.Token, true)
}
