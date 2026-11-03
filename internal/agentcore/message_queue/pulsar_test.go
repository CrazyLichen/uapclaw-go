package message_queue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/drunner/dmessage_queue"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewMessageQueuePulsar_不建client 测试构造函数不建立连接。
//
// Python: __init__ 只保存 url/max_workers，client 在 start() 才建
func TestNewMessageQueuePulsar_不建client(t *testing.T) {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))

	assert.Nil(t, q.client)
	assert.Equal(t, 8, q.maxWorkers)
	assert.Equal(t, MaxProducers, q.maxProducers)
	assert.Equal(t, "pulsar://localhost:6650", q.url)
}

// TestNewMessageQueuePulsar_空配置 测试 cfg 为 nil 时不 panic。
func TestNewMessageQueuePulsar_空配置(t *testing.T) {
	q := NewMessageQueuePulsar(nil)

	assert.NotNil(t, q)
	assert.Equal(t, 8, q.maxWorkers)
}

// TestStart_幂等 测试重复 Start 只建一次 client。
func TestStart_幂等(t *testing.T) {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))
	calls := 0
	q.startClient = func(url string) (pulsarClientAPI, error) {
		calls++
		return &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}, nil
	}

	q.Start()
	first := q.client
	q.Start()

	assert.Equal(t, 1, calls)
	assert.NotNil(t, first)
	assert.Same(t, first, q.client)
	assert.True(t, q.running.Load())
}

// TestStart_建client失败不置running 测试 SDK 建连失败的处理。
//
// Go 侧 Start 无返回值（接口约束），失败只记日志并保持未运行。
func TestStart_建client失败不置running(t *testing.T) {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))
	q.startClient = func(url string) (pulsarClientAPI, error) {
		return nil, assert.AnError
	}

	q.Start()

	assert.False(t, q.running.Load())
	assert.Nil(t, q.client)
}

// TestStop_未启动 测试未运行时 Stop 无副作用。
func TestStop_未启动(t *testing.T) {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))

	assert.NoError(t, q.Stop(context.Background()))
}

// TestStop_关闭client与producer 测试 Stop 释放全部资源。
func TestStop_关闭client与producer(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	_, err := q.Subscribe("topic.a")
	require.NoError(t, err)
	require.NoError(t, q.Produce(context.Background(), "topic.a", newTestRequest()))

	require.NoError(t, q.Stop(context.Background()))

	assert.Equal(t, 1, client.closed)
	assert.Equal(t, 1, client.producer.closed)
	assert.False(t, q.running.Load())
}

// TestSubscribe_未启动报错 测试错误码为 MESSAGE_QUEUE_TOPIC_SUBSCRIPTION_ERROR。
//
// Python: try/except 包住含 _validate_running 的全部步骤，统一转订阅错误
func TestSubscribe_未启动报错(t *testing.T) {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))

	_, err := q.Subscribe("topic.a")
	require.Error(t, err)

	baseErr, ok := err.(*exception.BaseError)
	require.True(t, ok, "期望 *exception.BaseError，实际 %T", err)
	assert.Equal(t, "MESSAGE_QUEUE_TOPIC_SUBSCRIPTION_ERROR", baseErr.Status().Name())
}

// TestSubscribe_同topic复用 测试重复订阅返回同一实例且只建一次 consumer。
func TestSubscribe_同topic复用(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	first, err := q.Subscribe("topic.a")
	require.NoError(t, err)
	second, err := q.Subscribe("topic.a")
	require.NoError(t, err)

	assert.Same(t, first, second)
	assert.Len(t, client.subTopics, 1)
}

// TestSubscribe_建consumer失败报错 测试建 consumer 失败被包装。
func TestSubscribe_建consumer失败报错(t *testing.T) {
	client := &fakeClient{consumerErr: assert.AnError}
	q := newTestQueue(client)

	_, err := q.Subscribe("topic.a")
	require.Error(t, err)

	baseErr, ok := err.(*exception.BaseError)
	require.True(t, ok, "期望 *exception.BaseError，实际 %T", err)
	assert.Equal(t, "MESSAGE_QUEUE_TOPIC_SUBSCRIPTION_ERROR", baseErr.Status().Name())
}

// TestUnsubscribe_不存在topic不报错 测试对齐 Python pop 后空判。
func TestUnsubscribe_不存在topic不报错(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	assert.NoError(t, q.Unsubscribe(context.Background(), "no.such.topic"))
}

// TestUnsubscribe_关闭consumer 测试取消订阅会关闭已激活的消费者。
func TestUnsubscribe_关闭consumer(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	sub, err := q.Subscribe("topic.a")
	require.NoError(t, err)
	// Python deactivate() 对未激活订阅直接返回（不关 consumer），故须先激活
	sub.Activate()

	require.NoError(t, q.Unsubscribe(context.Background(), "topic.a"))

	assert.Equal(t, 1, client.consumer.closed)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestQueue 构造一个注入了 fake client 的队列（跳过 Start 的真实建连）。
func newTestQueue(client pulsarClientAPI) *MessageQueuePulsar {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))
	q.client = client
	q.running.Store(true)
	return q
}

// newTestRequest 构造一条最小请求消息。
func newTestRequest() *dmessage_queue.DmqRequestMessage {
	return dmessage_queue.NewDmqRequestMessage(map[string]any{"q": "hi"})
}
