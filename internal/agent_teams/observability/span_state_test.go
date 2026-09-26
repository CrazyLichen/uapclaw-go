package observability

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitSpanState(t *testing.T) {
	state := InitSpanState()
	require.NotNil(t, state)
}

func TestWithSpanState_SpanStateFromCtx(t *testing.T) {
	state := InitSpanState()
	ctx := context.Background()

	// 未注入时返回 nil
	assert.Nil(t, SpanStateFromCtx(ctx))

	// 注入后可取出
	ctx = WithSpanState(ctx, state)
	got := SpanStateFromCtx(ctx)
	require.NotNil(t, got)
	assert.Equal(t, state, got)
}

func TestLlmSpanStack_PushPop(t *testing.T) {
	state := InitSpanState()
	span1 := &LlmSpanState{StartNs: 1}
	span2 := &LlmSpanState{StartNs: 2}

	state.PushLlmSpanState(span1)
	state.PushLlmSpanState(span2)

	// Pop LIFO
	popped := state.PopLlmSpanState(false)
	require.NotNil(t, popped)
	assert.Equal(t, int64(2), popped.StartNs)

	popped = state.PopLlmSpanState(false)
	require.NotNil(t, popped)
	assert.Equal(t, int64(1), popped.StartNs)

	// 空栈 pop 返回 nil
	assert.Nil(t, state.PopLlmSpanState(false))
}

func TestLlmSpanStack_Peek(t *testing.T) {
	state := InitSpanState()
	state.PushLlmSpanState(&LlmSpanState{StartNs: 1})

	// Peek 不移除
	peeked := state.PopLlmSpanState(true)
	require.NotNil(t, peeked)
	assert.Equal(t, int64(1), peeked.StartNs)

	// 栈未变
	peeked2 := state.PopLlmSpanState(true)
	require.NotNil(t, peeked2)
	assert.Equal(t, int64(1), peeked2.StartNs)
}

func TestLlmSpanState_NextChunkSeq(t *testing.T) {
	s := &LlmSpanState{}
	assert.Equal(t, 1, s.NextChunkSeq())
	assert.Equal(t, 2, s.NextChunkSeq())
	assert.Equal(t, 3, s.NextChunkSeq())
	assert.Equal(t, 3, s.ChunkCount)
}

func TestToolSpanMap_PushPop(t *testing.T) {
	state := InitSpanState()

	// 使用 nil span 测试 map 逻辑
	state.PushToolSpan("bash", nil)
	state.PushToolSpan("bash", nil)

	popped := state.PopToolSpan("bash")
	assert.Nil(t, popped) // nil span 但不报错

	popped = state.PopToolSpan("bash")
	assert.Nil(t, popped)

	// 空 map pop 返回 nil
	assert.Nil(t, state.PopToolSpan("bash"))
}

func TestAgentSpanMap_PushPop(t *testing.T) {
	state := InitSpanState()
	state.PushAgentSpan("agent1", nil)
	state.PushAgentSpan("agent1", nil)

	popped := state.PopAgentSpan("agent1")
	assert.Nil(t, popped)

	popped = state.PopAgentSpan("agent1")
	assert.Nil(t, popped)

	assert.Nil(t, state.PopAgentSpan("agent1"))
}

func TestResetAll(t *testing.T) {
	state := InitSpanState()
	state.PushLlmSpanState(&LlmSpanState{StartNs: 1})
	state.PushToolSpan("bash", nil)
	state.PushAgentSpan("agent1", nil)

	state.ResetAll()

	assert.Nil(t, state.PopLlmSpanState(false))
	assert.Nil(t, state.PopToolSpan("bash"))
	assert.Nil(t, state.PopAgentSpan("agent1"))
}

func TestOtelSpanState_并发安全(t *testing.T) {
	state := InitSpanState()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			state.PushLlmSpanState(&LlmSpanState{StartNs: int64(n)})
		}(i)
	}
	wg.Wait()
	// 栈中应有 100 个元素
	count := 0
	for state.PopLlmSpanState(false) != nil {
		count++
	}
	assert.Equal(t, 100, count)
}

func TestCurrentLLMSpanContext_空栈返回空(t *testing.T) {
	state := InitSpanState()
	sc := state.CurrentLLMSpanContext()
	assert.False(t, sc.IsValid())
}

func TestWithSpanStateAndTimeout(t *testing.T) {
	state := InitSpanState()
	ctx, cancel := WithSpanStateAndTimeout(context.Background(), state, 10*time.Second)
	defer cancel()

	// ctx 中应有 SpanState
	got := SpanStateFromCtx(ctx)
	assert.Equal(t, state, got)

	// ctx 应有 deadline
	deadline, ok := ctx.Deadline()
	assert.True(t, ok)
	assert.True(t, deadline.After(time.Now()))
}
