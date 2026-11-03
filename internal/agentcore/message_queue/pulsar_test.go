package message_queue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/drunner/dmessage_queue"
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
