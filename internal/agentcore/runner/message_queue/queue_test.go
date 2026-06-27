package message_queue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMessageQueueInMemory_启动停止 测试队列启动和停止生命周期。
func TestMessageQueueInMemory_启动停止(t *testing.T) {
	q := NewMessageQueueInMemory(100, 10*time.Second)

	// 未启动时 Produce 应返回错误
	err := q.Produce(context.Background(), "test", NewQueueMessage(nil))
	assert.ErrorIs(t, err, ErrQueueNotRunning)

	// 启动
	q.Start()
	assert.True(t, q.running.Load())

	// 重复启动不报错
	q.Start()

	// 停止
	err = q.Stop(context.Background())
	assert.NoError(t, err)
	assert.False(t, q.running.Load())
}

// TestMessageQueueInMemory_生产消费 测试消息的生产和消费。
func TestMessageQueueInMemory_生产消费(t *testing.T) {
	q := NewMessageQueueInMemory(100, 10*time.Second)
	q.Start()
	defer func() { _ = q.Stop(context.Background()) }()

	var received atomic.Int32
	sub := q.Subscribe("test_topic")

	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		received.Add(1)
		return nil, nil
	})
	sub.Activate()

	err := q.Produce(context.Background(), "test_topic", NewQueueMessage(map[string]any{"key": "value"}))
	require.NoError(t, err)

	// 等待消息被消费
	assert.Eventually(t, func() bool { return received.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
}

// TestMessageQueueInMemory_同步发布等待 测试 InvokeQueueMessage 的同步等待语义。
func TestMessageQueueInMemory_同步发布等待(t *testing.T) {
	q := NewMessageQueueInMemory(100, 10*time.Second)
	q.Start()
	defer func() { _ = q.Stop(context.Background()) }()

	sub := q.Subscribe("sync_topic")
	var handlerCalled atomic.Bool

	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		handlerCalled.Store(true)
		return "handler_result", nil
	})
	sub.Activate()

	invoke := NewInvokeQueueMessage(map[string]any{"key": "value"})
	err := q.ProduceSync(context.Background(), "sync_topic", invoke)
	require.NoError(t, err)

	// WaitResponse 应阻塞直到 handler 处理完成
	result, err := invoke.WaitResponse(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "handler_result", result)
	assert.True(t, handlerCalled.Load())
}

// TestMessageQueueInMemory_火忘发布 测试 QueueMessage 不等待处理完成。
func TestMessageQueueInMemory_火忘发布(t *testing.T) {
	q := NewMessageQueueInMemory(100, 10*time.Second)
	q.Start()
	defer func() { _ = q.Stop(context.Background()) }()

	var received atomic.Int32
	sub := q.Subscribe("async_topic")
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		time.Sleep(100 * time.Millisecond) // 模拟处理耗时
		received.Add(1)
		return nil, nil
	})
	sub.Activate()

	// 火忘发布，不传 InvokeQueueMessage
	err := q.Produce(context.Background(), "async_topic", NewQueueMessage(map[string]any{"key": "value"}))
	require.NoError(t, err)

	// Produce 立即返回，handler 可能还没处理完
	// 但最终会处理
	assert.Eventually(t, func() bool { return received.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
}

// TestMessageQueueInMemory_多Topic路由 测试不同 topic 消息路由到不同 handler。
func TestMessageQueueInMemory_多Topic路由(t *testing.T) {
	q := NewMessageQueueInMemory(100, 10*time.Second)
	q.Start()
	defer func() { _ = q.Stop(context.Background()) }()

	var topic1Received, topic2Received atomic.Int32

	sub1 := q.Subscribe("topic_1")
	sub1.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		topic1Received.Add(1)
		return nil, nil
	})
	sub1.Activate()

	sub2 := q.Subscribe("topic_2")
	sub2.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		topic2Received.Add(1)
		return nil, nil
	})
	sub2.Activate()

	_ = q.Produce(context.Background(), "topic_1", NewQueueMessage(map[string]any{"key": "1"}))
	_ = q.Produce(context.Background(), "topic_2", NewQueueMessage(map[string]any{"key": "2"}))

	assert.Eventually(t, func() bool { return topic1Received.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Eventually(t, func() bool { return topic2Received.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
}

// TestMessageQueueInMemory_订阅取消 测试取消订阅后 Produce 返回错误。
func TestMessageQueueInMemory_订阅取消(t *testing.T) {
	q := NewMessageQueueInMemory(100, 10*time.Second)
	q.Start()
	defer func() { _ = q.Stop(context.Background()) }()

	q.Subscribe("cancel_topic")
	err := q.Unsubscribe(context.Background(), "cancel_topic")
	require.NoError(t, err)

	err = q.Produce(context.Background(), "cancel_topic", NewQueueMessage(nil))
	assert.ErrorIs(t, err, ErrTopicNotFound)
}

// TestMessageQueueInMemory_激活停用 测试 Activate/Deactivate 控制消费。
func TestMessageQueueInMemory_激活停用(t *testing.T) {
	q := NewMessageQueueInMemory(100, 10*time.Second)
	q.Start()
	defer func() { _ = q.Stop(context.Background()) }()

	var received atomic.Int32
	sub := q.Subscribe("toggle_topic")
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		received.Add(1)
		return nil, nil
	})

	// 激活
	sub.Activate()
	assert.True(t, sub.IsActive())

	_ = q.Produce(context.Background(), "toggle_topic", NewQueueMessage(map[string]any{"key": "1"}))
	assert.Eventually(t, func() bool { return received.Load() == 1 }, 2*time.Second, 10*time.Millisecond)

	// 停用
	sub.Deactivate()
	assert.False(t, sub.IsActive())
}

// TestMessageQueueInMemory_同步发布Handler错误 测试 handler 返回错误时 WaitResponse 收到错误。
func TestMessageQueueInMemory_同步发布Handler错误(t *testing.T) {
	q := NewMessageQueueInMemory(100, 10*time.Second)
	q.Start()
	defer func() { _ = q.Stop(context.Background()) }()

	sub := q.Subscribe("error_topic")
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		return nil, errors.New("handler error")
	})
	sub.Activate()

	invoke := NewInvokeQueueMessage(map[string]any{"key": "value"})
	_ = q.ProduceSync(context.Background(), "error_topic", invoke)

	result, err := invoke.WaitResponse(context.Background())
	assert.Error(t, err)
	assert.EqualError(t, err, "handler error")
	assert.Nil(t, result)
}

// TestMessageQueueInMemory_Handler未设置 测试 handler 未设置时同步消息返回错误。
func TestMessageQueueInMemory_Handler未设置(t *testing.T) {
	q := NewMessageQueueInMemory(100, 10*time.Second)
	q.Start()
	defer func() { _ = q.Stop(context.Background()) }()

	sub := q.Subscribe("no_handler_topic")
	// 不设置 handler
	sub.Activate()

	invoke := NewInvokeQueueMessage(map[string]any{"key": "value"})
	_ = q.ProduceSync(context.Background(), "no_handler_topic", invoke)

	_, err := invoke.WaitResponse(context.Background())
	assert.ErrorIs(t, err, ErrHandlerNotSet)
}

// TestMessageQueueInMemory_重复订阅同Topic 测试对同一 topic 多次 Subscribe 返回同一订阅。
func TestMessageQueueInMemory_重复订阅同Topic(t *testing.T) {
	q := NewMessageQueueInMemory(100, 10*time.Second)
	q.Start()
	defer func() { _ = q.Stop(context.Background()) }()

	sub1 := q.Subscribe("same_topic")
	sub2 := q.Subscribe("same_topic")
	assert.Equal(t, sub1.ts, sub2.ts)
}
