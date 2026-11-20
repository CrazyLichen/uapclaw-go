package message_queue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestLocalMessageQueue_实现MessageQueueBase接口 测试 LocalMessageQueue 满足 MessageQueueBase 接口。
func TestLocalMessageQueue_实现MessageQueueBase接口(t *testing.T) {
	var _ MessageQueueBase = &LocalMessageQueue{}
}

// TestLocalSubscription_实现SubscriptionBase接口 测试 LocalSubscription 满足 SubscriptionBase 接口。
func TestLocalSubscription_实现SubscriptionBase接口(t *testing.T) {
	var _ SubscriptionBase = &LocalSubscription{}
}

// TestLocalMessageQueue_启动停止 测试 no-op 启动停止不报错。
func TestLocalMessageQueue_启动停止(t *testing.T) {
	q := &LocalMessageQueue{}
	q.Start()
	err := q.Stop(context.Background())
	assert.NoError(t, err)
}

// TestLocalMessageQueue_订阅取消 测试 no-op 订阅取消不报错。
func TestLocalMessageQueue_订阅取消(t *testing.T) {
	q := &LocalMessageQueue{}
	sub, err := q.Subscribe("test_topic")
	assert.NoError(t, err)
	assert.NotNil(t, sub)

	err = q.Unsubscribe(context.Background(), "test_topic")
	assert.NoError(t, err)
}

// TestLocalMessageQueue_发布 测试 no-op 发布不报错。
func TestLocalMessageQueue_发布(t *testing.T) {
	q := &LocalMessageQueue{}
	err := q.Produce(context.Background(), "test_topic", NewQueueMessage(map[string]any{"key": "value"}))
	assert.NoError(t, err)
}

// TestLocalSubscription_所有方法 测试 no-op 订阅所有方法。
func TestLocalSubscription_所有方法(t *testing.T) {
	s := &LocalSubscription{}
	s.SetMessageHandler(nil)
	s.Activate()
	s.Deactivate()
	assert.False(t, s.IsActive())
}
