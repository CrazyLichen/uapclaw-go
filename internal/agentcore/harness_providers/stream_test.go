package harness_providers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

// 本文件覆盖 stream.py 的有界缓冲与单消费者游标。
//
// ⚠️ 上游对 BoundedEventBuffer / BufferedEventCursor **零直接测试**
// （grep -rn "BoundedEventBuffer" openjiuwen/tests/ 零命中），只经
// test_base.py:321 的单消费者租约用例间接覆盖。本文件是补测（spec §7.4），
// 重点覆盖并发状态机：背压、Close 唤醒、peek 不消费、租约。

// testHostSessionID 测试事件的宿主会话标识（NewHarnessEvent 要求非空）。
const testHostSessionID = "host-test"

// testAgentID 测试事件的 agent 标识（NewHarnessEvent 要求非空）。
const testAgentID = "agent-test"

// newTestEvent 构造一条带 OutputEvent 载荷的测试事件。
func newTestEvent(t *testing.T, sequence int) harnessprotocol.HarnessEvent {
	t.Helper()
	payload, err := harnessprotocol.NewOutputEvent(
		fmt.Sprintf("out-%d", sequence),
		harnessprotocol.OutputKindText,
		"内容",
		harnessprotocol.OutputOperationDelta,
		harnessprotocol.OutputChannelAnswer,
		0,
		"text",
		nil,
	)
	require.NoError(t, err)
	event, err := harnessprotocol.NewHarnessEvent(harnessprotocol.HarnessEventOptions{
		Sequence:      sequence,
		Timestamp:     float64(sequence),
		Event:         payload,
		HostSessionID: testHostSessionID,
		AgentID:       testAgentID,
	})
	require.NoError(t, err)
	return event
}

// newTurnEvent 构造一条带 TurnLifecycleEvent 载荷、绑定 turnID 的测试事件。
//
// 终态 kind（finished/aborted/failed）要求非 nil 的 TurnResult
// （NewTurnLifecycleEvent 的构造校验）；非终态传 nil。
func newTurnEvent(t *testing.T, sequence int, turnID string, kind harnessprotocol.TurnEventKind) harnessprotocol.HarnessEvent {
	t.Helper()
	var result *harnessprotocol.TurnResult
	if harnessprotocol.IsTerminalTurnEventKind(kind) {
		built, err := harnessprotocol.NewTurnResult(
			harnessprotocol.TurnStatusCompleted, harnessprotocol.ResultOptions{})
		require.NoError(t, err)
		result = &built
	}
	payload, err := harnessprotocol.NewTurnLifecycleEvent(kind, result)
	require.NoError(t, err)
	event, err := harnessprotocol.NewHarnessEvent(harnessprotocol.HarnessEventOptions{
		Sequence:      sequence,
		Timestamp:     float64(sequence),
		Event:         payload,
		HostSessionID: testHostSessionID,
		AgentID:       testAgentID,
		TurnID:        turnID,
	})
	require.NoError(t, err)
	return event
}

// TestBoundedEventBuffer_PutGet往返 基本 FIFO 与关闭后耗尽。
func TestBoundedEventBuffer_PutGet往返(t *testing.T) {
	buffer := NewBoundedEventBuffer(4)
	ctx := context.Background()

	require.NoError(t, buffer.Put(ctx, newTestEvent(t, 1)))
	require.NoError(t, buffer.Put(ctx, newTestEvent(t, 2)))

	first, err := buffer.Get(ctx)
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, 1, first.Sequence)

	second, err := buffer.Get(ctx)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, 2, second.Sequence)

	// 关闭后 Get 返回耗尽哨兵（对应 Python get 返回 None）
	require.NoError(t, buffer.Close(ctx))
	got, err := buffer.Get(ctx)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, harnessprotocol.ErrEventStreamDone)
}

// TestBoundedEventBuffer_Peek不消费 peek 不删除、不释放容量（stream.py:58-64）。
func TestBoundedEventBuffer_Peek不消费(t *testing.T) {
	buffer := NewBoundedEventBuffer(4)
	ctx := context.Background()
	require.NoError(t, buffer.Put(ctx, newTestEvent(t, 1)))

	peeked, err := buffer.Peek(ctx)
	require.NoError(t, err)
	require.NotNil(t, peeked)
	assert.Equal(t, 1, peeked.Sequence)

	// Peek 之后 Get 还是同一条
	got, err := buffer.Get(ctx)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 1, got.Sequence)
}

// TestBoundedEventBuffer_Peek空且关闭 返回耗尽哨兵。
func TestBoundedEventBuffer_Peek空且关闭(t *testing.T) {
	buffer := NewBoundedEventBuffer(2)
	ctx := context.Background()
	require.NoError(t, buffer.Close(ctx))

	got, err := buffer.Peek(ctx)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, harnessprotocol.ErrEventStreamDone)
}

// TestBoundedEventBuffer_Put满时阻塞_Get后解除 背压语义（stream.py:39-40 的 while wait）。
func TestBoundedEventBuffer_Put满时阻塞_Get后解除(t *testing.T) {
	buffer := NewBoundedEventBuffer(1)
	ctx := context.Background()
	require.NoError(t, buffer.Put(ctx, newTestEvent(t, 1)))

	done := make(chan error, 1)
	go func() { done <- buffer.Put(ctx, newTestEvent(t, 2)) }()

	// 缓冲满：Put 必须阻塞
	select {
	case <-done:
		t.Fatal("缓冲满时 Put 不应返回")
	case <-time.After(50 * time.Millisecond):
	}

	// Get 释放一格后 Put 应完成
	_, err := buffer.Get(ctx)
	require.NoError(t, err)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Get 释放容量后 Put 未解除阻塞")
	}
}

// TestBoundedEventBuffer_Close唤醒阻塞的Put 关闭时生产者收到哨兵（stream.py:41-42）。
func TestBoundedEventBuffer_Close唤醒阻塞的Put(t *testing.T) {
	buffer := NewBoundedEventBuffer(1)
	ctx := context.Background()
	require.NoError(t, buffer.Put(ctx, newTestEvent(t, 1)))

	done := make(chan error, 1)
	go func() { done <- buffer.Put(ctx, newTestEvent(t, 2)) }()
	time.Sleep(20 * time.Millisecond)

	require.NoError(t, buffer.Close(ctx))
	select {
	case err := <-done:
		assert.ErrorIs(t, err, ErrEventBufferClosed)
	case <-time.After(time.Second):
		t.Fatal("Close 未唤醒阻塞的 Put")
	}
}

// TestBoundedEventBuffer_Close唤醒阻塞的Get 关闭时消费者收到耗尽哨兵。
func TestBoundedEventBuffer_Close唤醒阻塞的Get(t *testing.T) {
	buffer := NewBoundedEventBuffer(2)
	ctx := context.Background()

	type result struct {
		event *harnessprotocol.HarnessEvent
		err   error
	}
	done := make(chan result, 1)
	go func() {
		event, err := buffer.Get(ctx)
		done <- result{event, err}
	}()
	time.Sleep(20 * time.Millisecond)

	require.NoError(t, buffer.Close(ctx))
	select {
	case got := <-done:
		assert.Nil(t, got.event)
		assert.ErrorIs(t, got.err, harnessprotocol.ErrEventStreamDone)
	case <-time.After(time.Second):
		t.Fatal("Close 未唤醒阻塞的 Get")
	}
}

// TestBoundedEventBuffer_Close后Put返回哨兵 对齐 stream.py:41-42。
func TestBoundedEventBuffer_Close后Put返回哨兵(t *testing.T) {
	buffer := NewBoundedEventBuffer(2)
	ctx := context.Background()
	require.NoError(t, buffer.Close(ctx))

	err := buffer.Put(ctx, newTestEvent(t, 1))
	assert.ErrorIs(t, err, ErrEventBufferClosed)
}

// TestBoundedEventBuffer_Close幂等 对齐 stream.py:66-71（无幂等守卫但语义幂等）。
func TestBoundedEventBuffer_Close幂等(t *testing.T) {
	buffer := NewBoundedEventBuffer(2)
	ctx := context.Background()
	require.NoError(t, buffer.Close(ctx))
	require.NoError(t, buffer.Close(ctx))
}

// TestBoundedEventBuffer_单消费者租约 对齐 test_base.py:321-330。
//
// 第二次取游标必须失败；释放后可再取。
func TestBoundedEventBuffer_单消费者租约(t *testing.T) {
	buffer := NewBoundedEventBuffer(4)
	ctx := context.Background()

	cursor, err := buffer.Cursor("", false)
	require.NoError(t, err)
	require.NotNil(t, cursor)

	// 上游抛 HarnessStateError（stream.py:76-77）
	second, err := buffer.Cursor("", false)
	assert.Nil(t, second)
	var stateErr *harnessprotocol.HarnessStateError
	assert.ErrorAs(t, err, &stateErr)
	assert.Contains(t, stateErr.Msg, "already has an active consumer")

	// 关闭游标释放租约后可再取
	require.NoError(t, cursor.Close(ctx))
	again, err := buffer.Cursor("", false)
	require.NoError(t, err)
	require.NotNil(t, again)
}

// TestBufferedEventCursor_周期级游标直通 perTurn=false 逐条返回。
func TestBufferedEventCursor_周期级游标直通(t *testing.T) {
	buffer := NewBoundedEventBuffer(8)
	ctx := context.Background()
	cursor, err := buffer.Cursor("", false)
	require.NoError(t, err)

	require.NoError(t, buffer.Put(ctx, newTestEvent(t, 1)))
	require.NoError(t, buffer.Put(ctx, newTestEvent(t, 2)))

	got, err := cursor.Next(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Sequence)

	got, err = cursor.Next(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, got.Sequence)
}

// TestBufferedEventCursor_单轮游标在终态自动关闭 对齐 stream.py:152-159。
func TestBufferedEventCursor_单轮游标在终态自动关闭(t *testing.T) {
	buffer := NewBoundedEventBuffer(8)
	ctx := context.Background()

	require.NoError(t, buffer.Put(ctx, newTurnEvent(t, 1, "turn-a", harnessprotocol.TurnEventKindStarted)))
	require.NoError(t, buffer.Put(ctx, newTestEvent(t, 2)))
	require.NoError(t, buffer.Put(ctx, newTurnEvent(t, 3, "turn-a", harnessprotocol.TurnEventKindFinished)))

	cursor, err := buffer.Cursor("", true)
	require.NoError(t, err)

	// STARTED èµ·å§å¸§
	got, err := cursor.Next(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Sequence)
	// 中间事件
	got, err = cursor.Next(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, got.Sequence)
	// 终态：返回后游标已关闭
	got, err = cursor.Next(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, got.Sequence)

	// 已关闭：再 Next 返回哨兵
	got, err = cursor.Next(ctx)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, harnessprotocol.ErrEventStreamDone)

	// 租约已释放
	again, err := buffer.Cursor("", false)
	require.NoError(t, err)
	require.NotNil(t, again)
}

// TestBufferedEventCursor_丢弃STARTED之前的非轮次事件 对齐 stream.py:132-136。
func TestBufferedEventCursor_丢弃STARTED之前的非轮次事件(t *testing.T) {
	buffer := NewBoundedEventBuffer(8)
	ctx := context.Background()

	// 周期级输出事件（无 TurnID），per-turn 游标应跳过
	require.NoError(t, buffer.Put(ctx, newTestEvent(t, 1)))
	require.NoError(t, buffer.Put(ctx, newTurnEvent(t, 2, "turn-b", harnessprotocol.TurnEventKindStarted)))

	cursor, err := buffer.Cursor("", true)
	require.NoError(t, err)

	got, err := cursor.Next(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, got.Sequence, "应跳过 sequence=1，直接返回 STARTED")
}

// TestBufferedEventCursor_期望轮次不匹配报错 对齐 stream.py:137-142。
func TestBufferedEventCursor_期望轮次不匹配报错(t *testing.T) {
	buffer := NewBoundedEventBuffer(8)
	ctx := context.Background()
	require.NoError(t, buffer.Put(ctx, newTurnEvent(t, 1, "turn-a", harnessprotocol.TurnEventKindStarted)))

	cursor, err := buffer.Cursor("turn-z", true)
	require.NoError(t, err)

	_, err = cursor.Next(ctx)
	var stateErr *harnessprotocol.HarnessStateError
	require.ErrorAs(t, err, &stateErr)
	assert.Contains(t, stateErr.Msg, "requested turn is not the next unconsumed turn")
}

// TestBufferedEventCursor_流中途关闭报协议错误 对齐 stream.py:122-125。
func TestBufferedEventCursor_流中途关闭报协议错误(t *testing.T) {
	buffer := NewBoundedEventBuffer(8)
	ctx := context.Background()
	require.NoError(t, buffer.Put(ctx, newTurnEvent(t, 1, "turn-a", harnessprotocol.TurnEventKindStarted)))

	cursor, err := buffer.Cursor("", true)
	require.NoError(t, err)

	// 消费 STARTED，选定轮次
	_, err = cursor.Next(ctx)
	require.NoError(t, err)

	// 轮次未终态就关流
	require.NoError(t, buffer.Close(ctx))

	_, err = cursor.Next(ctx)
	var protoErr *harnessprotocol.HarnessProtocolError
	require.ErrorAs(t, err, &protoErr)
	assert.Contains(t, protoErr.Msg, "closed before turn")
}

// TestBufferedEventCursor_Close唤醒阻塞的Next 偏差 D3。
//
// 上游 aclose 只释放租约不唤醒（stream.py:161-165），阻塞者要等 buffer.close()。
// Go 的 sync.Cond 无 asyncio task 取消兜底，悬挂 goroutine 是真实泄漏，
// 故此处 Close 额外 Broadcast。
func TestBufferedEventCursor_Close唤醒阻塞的Next(t *testing.T) {
	buffer := NewBoundedEventBuffer(2)
	ctx := context.Background()
	cursor, err := buffer.Cursor("", false)
	require.NoError(t, err)

	type result struct {
		event *harnessprotocol.HarnessEvent
		err   error
	}
	done := make(chan result, 1)
	go func() {
		event, err := cursor.Next(ctx)
		done <- result{event, err}
	}()
	time.Sleep(20 * time.Millisecond)

	// 只关游标，不关 buffer
	require.NoError(t, cursor.Close(ctx))
	select {
	case got := <-done:
		assert.Nil(t, got.event)
		assert.ErrorIs(t, got.err, harnessprotocol.ErrEventStreamDone)
	case <-time.After(time.Second):
		t.Fatal("Close 未唤醒阻塞的 Next（偏差 D3 未生效）")
	}
}

// TestBufferedEventCursor_Close幂等 重复关闭不出错。
func TestBufferedEventCursor_Close幂等(t *testing.T) {
	buffer := NewBoundedEventBuffer(2)
	ctx := context.Background()
	cursor, err := buffer.Cursor("", false)
	require.NoError(t, err)
	require.NoError(t, cursor.Close(ctx))
	require.NoError(t, cursor.Close(ctx))
}

// TestBufferedEventCursor_Iter返回自身 对齐 __aiter__（stream.py:103-104）。
func TestBufferedEventCursor_Iter返回自身(t *testing.T) {
	buffer := NewBoundedEventBuffer(2)
	cursor, err := buffer.Cursor("", false)
	require.NoError(t, err)
	assert.Equal(t, cursor, cursor.Iter())
}

// TestTurnLifecycleKind_两种形态 值形态与指针形态都必须识别。
//
// 15.2 的 NewTurnLifecycleEvent 返回值类型，但 provider 也可把指针塞进
// HarnessEventPayload（interface 承载）。只认指针会让 per-turn 游标把
// STARTED 当「非 STARTED」无限丢弃并最终阻塞（实施时实测死锁）。
func TestTurnLifecycleKind_两种形态(t *testing.T) {
	built, err := harnessprotocol.NewTurnLifecycleEvent(harnessprotocol.TurnEventKindStarted, nil)
	require.NoError(t, err)

	kind, ok := turnLifecycleKind(built)
	assert.True(t, ok)
	assert.Equal(t, harnessprotocol.TurnEventKindStarted, kind)

	kind, ok = turnLifecycleKind(&built)
	assert.True(t, ok)
	assert.Equal(t, harnessprotocol.TurnEventKindStarted, kind)
}

// TestTurnLifecycleKind_非生命周期载荷 输出事件载荷不算轮次事件。
func TestTurnLifecycleKind_非生命周期载荷(t *testing.T) {
	output, err := harnessprotocol.NewOutputEvent(
		"o1", harnessprotocol.OutputKindText, "x", harnessprotocol.OutputOperationDelta,
		harnessprotocol.OutputChannelAnswer, 0, "text", nil)
	require.NoError(t, err)
	_, ok := turnLifecycleKind(output)
	assert.False(t, ok)
}

// TestTurnLifecycleKind_空指针 指针为 nil 时按非生命周期处理。
func TestTurnLifecycleKind_空指针(t *testing.T) {
	var lifecycle *harnessprotocol.TurnLifecycleEvent
	_, ok := turnLifecycleKind(lifecycle)
	assert.False(t, ok)
}
