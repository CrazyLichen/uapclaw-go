package message_queue

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/drunner/dmessage_queue"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
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

// TestProduce_未启动报错 测试错误码为 MESSAGE_QUEUE_TOPIC_MESSAGE_PRODUCTION_ERROR。
func TestProduce_未启动报错(t *testing.T) {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))

	err := q.Produce(context.Background(), "topic.a", newTestRequest())
	require.Error(t, err)

	baseErr, ok := err.(*exception.BaseError)
	require.True(t, ok, "期望 *exception.BaseError，实际 %T", err)
	assert.Equal(t, "MESSAGE_QUEUE_TOPIC_MESSAGE_PRODUCTION_ERROR", baseErr.Status().Name())
}

// TestProduce_partitionKey为messageID 测试 partitionKey 传 message_id。
//
// Python: producer.send(content, partition_key=message.message_id)
func TestProduce_partitionKey为messageID(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	msg := dmessage_queue.NewDmqRequestMessage(map[string]any{"q": "hi"})
	msg.MessageID = "mid-42"

	require.NoError(t, q.Produce(context.Background(), "topic.a", msg))

	require.Len(t, client.producer.sentKeys, 1)
	assert.Equal(t, "mid-42", client.producer.sentKeys[0])
	// 发送内容必须是 wire 字节（含 __class__ 标记）
	assert.Contains(t, client.producer.sentPayloads[0], "DmqRequestMessage")
}

// TestProduce_发送失败报错 测试发送错误被包装成生产错误。
func TestProduce_发送失败报错(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{sendErr: assert.AnError}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	err := q.Produce(context.Background(), "topic.a", newTestRequest())
	require.Error(t, err)

	baseErr, ok := err.(*exception.BaseError)
	require.True(t, ok, "期望 *exception.BaseError，实际 %T", err)
	assert.Equal(t, "MESSAGE_QUEUE_TOPIC_MESSAGE_PRODUCTION_ERROR", baseErr.Status().Name())
}

// TestProduce_非Dmq类型报错 测试偏差 D6 在网络层透传。
func TestProduce_非Dmq类型报错(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	err := q.Produce(context.Background(), "topic.a", message_queue.NewQueueMessage(nil))
	require.Error(t, err)

	baseErr, ok := err.(*exception.BaseError)
	require.True(t, ok, "期望 *exception.BaseError，实际 %T", err)
	assert.Equal(t, "MESSAGE_QUEUE_TOPIC_MESSAGE_PRODUCTION_ERROR", baseErr.Status().Name())
}

// TestGetOrCreateProducer_复用同一producer 测试同 topic 不重复创建。
func TestGetOrCreateProducer_复用同一producer(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	_, err := q.getOrCreateProducer("topic.a")
	require.NoError(t, err)
	_, err = q.getOrCreateProducer("topic.a")
	require.NoError(t, err)

	assert.Len(t, client.topics, 1)
}

// TestGetOrCreateProducer_LRU淘汰最旧 测试超过容量淘汰最久未用。
//
// Python: OrderedDict.popitem(last=False) + move_to_end
func TestGetOrCreateProducer_LRU淘汰最旧(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)
	q.maxProducers = 2

	for _, topic := range []string{"t1", "t2"} {
		_, err := q.getOrCreateProducer(topic)
		require.NoError(t, err)
	}
	// 访问 t1，使其成为最近使用
	_, err := q.getOrCreateProducer("t1")
	require.NoError(t, err)
	// 创建 t3，应淘汰最久未用的 t2
	_, err = q.getOrCreateProducer("t3")
	require.NoError(t, err)

	assert.NotContains(t, q.producers, "t2")
	assert.Contains(t, q.producers, "t1")
	assert.Equal(t, 1, client.producer.closed)
}

// TestGetOrCreateProducer_建producer失败 测试建 producer 失败透传错误。
func TestGetOrCreateProducer_建producer失败(t *testing.T) {
	client := &fakeClient{producerErr: assert.AnError}
	q := newTestQueue(client)

	_, err := q.getOrCreateProducer("topic.a")
	require.Error(t, err)
}

// TestActivate_幂等 测试重复 Activate 只起一条消费 goroutine。
func TestActivate_幂等(t *testing.T) {
	consumer := &fakeConsumer{}
	sub := newPulsarSubscription("topic.a", consumer)

	sub.Activate()
	sub.Activate()

	assert.True(t, sub.IsActive())

	sub.Deactivate()
	assert.False(t, sub.IsActive())
}

// TestDeactivate_关闭consumer并退出goroutine 测试同步等待消费循环结束。
func TestDeactivate_关闭consumer并退出goroutine(t *testing.T) {
	consumer := &fakeConsumer{}
	sub := newPulsarSubscription("topic.a", consumer)

	sub.Activate()
	sub.Deactivate()

	assert.False(t, sub.IsActive())
	assert.Equal(t, 1, consumer.closed)
}

// TestDeactivate_未激活无副作用 测试幂等。
//
// Python: deactivate() 首行 if not self._active: return
func TestDeactivate_未激活无副作用(t *testing.T) {
	consumer := &fakeConsumer{}
	sub := newPulsarSubscription("topic.a", consumer)

	sub.Deactivate()

	assert.Equal(t, 0, consumer.closed)
}

// TestCallHandler_未设置返回nil 测试未注册处理器时不报错。
//
// Python: if self._handler: await self._handler(payload)
func TestCallHandler_未设置返回nil(t *testing.T) {
	sub := newPulsarSubscription("topic.a", &fakeConsumer{})

	assert.NoError(t, sub.callHandler(context.Background(), map[string]any{}))
}

// TestConsumeLoop_handler收到完整消息map 测试传整条消息而非 payload 字段（偏差 D7）。
func TestConsumeLoop_handler收到完整消息map(t *testing.T) {
	msg := dmessage_queue.NewDmqRequestMessage(map[string]any{"q": "hi"})
	msg.MessageID = "mid-1"
	msg.ReplyTopic = "reply.t"
	data, err := dmessage_queue.SerializeMessage(msg)
	require.NoError(t, err)

	consumer := &fakeConsumer{messages: []pulsarMessageAPI{&fakeMessage{payload: data}}}
	sub := newPulsarSubscription("topic.a", consumer)

	received := make(chan map[string]any, 1)
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		received <- payload
		return nil, nil
	})

	sub.Activate()
	defer sub.Deactivate()

	select {
	case payload := <-received:
		assert.Equal(t, "DmqRequestMessage", payload["__class__"])
		assert.Equal(t, "reply.t", payload["reply_topic"])
		assert.Equal(t, "mid-1", payload["message_id"])
	case <-time.After(2 * time.Second):
		t.Fatal("handler 未被调用")
	}
}

// TestConsumeLoop_成功后ack 测试处理后确认消息。
func TestConsumeLoop_成功后ack(t *testing.T) {
	msg := dmessage_queue.NewDmqRequestMessage(map[string]any{"q": "hi"})
	data, err := dmessage_queue.SerializeMessage(msg)
	require.NoError(t, err)

	consumer := &fakeConsumer{messages: []pulsarMessageAPI{&fakeMessage{payload: data}}}
	sub := newPulsarSubscription("topic.a", consumer)

	handled := make(chan struct{}, 1)
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		handled <- struct{}{}
		return nil, nil
	})

	sub.Activate()
	defer sub.Deactivate()

	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("handler 未被调用")
	}

	require.Eventually(t, func() bool { return consumer.ackCount.Load() > 0 }, 2*time.Second, 10*time.Millisecond,
		"消息未被 ack")
}

// TestConsumeLoop_handler报错不ack 测试对齐 Python 异常路径不 acknowledge。
func TestConsumeLoop_handler报错不ack(t *testing.T) {
	msg := dmessage_queue.NewDmqRequestMessage(map[string]any{"q": "hi"})
	data, err := dmessage_queue.SerializeMessage(msg)
	require.NoError(t, err)

	consumer := &fakeConsumer{messages: []pulsarMessageAPI{&fakeMessage{payload: data}}}
	sub := newPulsarSubscription("topic.a", consumer)

	handled := make(chan struct{}, 1)
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		handled <- struct{}{}
		return nil, assert.AnError
	})

	sub.Activate()
	defer sub.Deactivate()

	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("handler 未被调用")
	}

	// 留出处理窗口，确认没有 ack
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int64(0), consumer.ackCount.Load())
}

// TestConsumeLoop_反序列化失败不ack 测试坏消息不确认。
func TestConsumeLoop_反序列化失败不ack(t *testing.T) {
	consumer := &fakeConsumer{messages: []pulsarMessageAPI{&fakeMessage{payload: []byte("not-json")}}}
	sub := newPulsarSubscription("topic.a", consumer)
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		return nil, nil
	})

	sub.Activate()
	time.Sleep(100 * time.Millisecond)
	sub.Deactivate()

	assert.Equal(t, int64(0), consumer.ackCount.Load())
}

// TestConsumeLoop_接收错误继续循环 测试非超时错误不退出循环（对齐 logger.exception 后继续）。
func TestConsumeLoop_接收错误继续循环(t *testing.T) {
	consumer := &fakeConsumer{receiveErr: assert.AnError}
	sub := newPulsarSubscription("topic.a", consumer)
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		return nil, nil
	})

	sub.Activate()
	time.Sleep(100 * time.Millisecond)
	sub.Deactivate()

	assert.False(t, sub.IsActive())
	assert.Equal(t, int64(0), consumer.ackCount.Load())
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
