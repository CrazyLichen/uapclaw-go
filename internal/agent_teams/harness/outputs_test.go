package harness

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// TestOutputIterator_读到END后返回false 验证终止语义：END 之后恒 false。
func TestOutputIterator_读到END后返回false(t *testing.T) {
	q := make(chan stream.Schema, 2)
	it := newOutputIterator(q)

	q <- &stream.OutputSchema{Type: "text", Payload: map[string]any{"content": "hello"}}
	q <- endSentinel
	close(q)

	chunk, ok := it.Next(context.Background())
	require.True(t, ok)
	assert.NotNil(t, chunk)

	_, ok = it.Next(context.Background())
	assert.False(t, ok)

	// 终止后再次调用仍为 false（幂等终止，C17）
	_, ok = it.Next(context.Background())
	assert.False(t, ok)
}

// TestOutputIterator_单消费者 验证第二个迭代器读不到数据（C17）。
func TestOutputIterator_单消费者(t *testing.T) {
	q := make(chan stream.Schema, 1)
	first := newOutputIterator(q)
	second := newOutputIterator(q)

	q <- endSentinel
	close(q)

	_, ok := first.Next(context.Background())
	assert.False(t, ok)

	// 第二个迭代器也被标记为不可用：不抢同一个 channel
	_, ok = second.Next(context.Background())
	assert.False(t, ok)
}

// TestOutputIterator_上下文取消可退出 验证阻塞读可被打断。
func TestOutputIterator_上下文取消可退出(t *testing.T) {
	q := make(chan stream.Schema)
	it := newOutputIterator(q)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, ok := it.Next(ctx)
	assert.False(t, ok)
	assert.Less(t, time.Since(start), time.Second)
}
