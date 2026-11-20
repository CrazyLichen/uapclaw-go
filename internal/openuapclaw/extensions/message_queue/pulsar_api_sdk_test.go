package message_queue

import (
	"context"
	"testing"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeSDKClient 真实 SDK 客户端的替身。
// 嵌入 nil 接口以满足 pulsar.Client，只覆写被调用的方法。
type fakeSDKClient struct {
	pulsar.Client
	// producer 返回的生产者
	producer pulsar.Producer
	// consumer 返回的消费者
	consumer pulsar.Consumer
	// producerErr 建生产者错误
	producerErr error
	// consumerErr 建消费者错误
	consumerErr error
	// gotProducerTopic CreateProducer 收到的 topic
	gotProducerTopic string
	// gotConsumerOptions Subscribe 收到的选项
	gotConsumerOptions pulsar.ConsumerOptions
	// closed Close 调用次数
	closed int
}

// fakeSDKProducer 真实 SDK 生产者的替身
type fakeSDKProducer struct {
	pulsar.Producer
	// sent 发送的消息
	sent *pulsar.ProducerMessage
	// sendErr 发送错误
	sendErr error
	// closed Close 调用次数
	closed int
}

// fakeSDKConsumer 真实 SDK 消费者的替身
type fakeSDKConsumer struct {
	pulsar.Consumer
	// msg 返回的消息
	msg pulsar.Message
	// receiveErr 接收错误
	receiveErr error
	// acked 被确认的消息
	acked pulsar.Message
	// ackErr Ack 错误
	ackErr error
	// closed Close 调用次数
	closed int
}

// fakeSDKMessage 真实 SDK 消息的替身
type fakeSDKMessage struct {
	pulsar.Message
	// payload 消息体
	payload []byte
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSDKClient_惰性创建成功 测试建客户端不立即连接、返回非 nil。
// pulsar-client-go 的 NewClient 为惰性建连（首次建 producer/consumer 才连），
// 因此无需真实 broker 即可验证包装层。
func TestNewSDKClient_惰性创建成功(t *testing.T) {
	client, err := newSDKClient("pulsar://localhost:6650")
	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()

	_, ok := client.(*sdkClient)
	assert.True(t, ok, "期望 *sdkClient，实际 %T", client)
}

// TestSDKClient_CreateProducer 测试建 producer 被包成窄接口。
func TestSDKClient_CreateProducer(t *testing.T) {
	inner := &fakeSDKProducer{}
	c := &sdkClient{client: &fakeSDKClient{producer: inner}}

	got, err := c.CreateProducer("topic.a")
	require.NoError(t, err)

	producer, ok := got.(*sdkProducer)
	require.True(t, ok, "期望 *sdkProducer，实际 %T", got)
	assert.Same(t, inner, producer.producer)
}

// TestSDKClient_CreateProducer失败 测试建 producer 失败透传错误。
func TestSDKClient_CreateProducer失败(t *testing.T) {
	c := &sdkClient{client: &fakeSDKClient{producerErr: assert.AnError}}

	_, err := c.CreateProducer("topic.a")
	assert.ErrorIs(t, err, assert.AnError)
}

// TestSDKClient_Subscribe 测试订阅使用 KeyShared 且主题名正确。
func TestSDKClient_Subscribe(t *testing.T) {
	client := &fakeSDKClient{consumer: &fakeSDKConsumer{}}
	c := &sdkClient{client: client}

	got, err := c.Subscribe("topic.a", "default")
	require.NoError(t, err)

	_, ok := got.(*sdkConsumer)
	require.True(t, ok, "期望 *sdkConsumer，实际 %T", got)
	assert.Equal(t, "topic.a", client.gotConsumerOptions.Topic)
	assert.Equal(t, "default", client.gotConsumerOptions.SubscriptionName)
	assert.Equal(t, pulsar.KeyShared, client.gotConsumerOptions.Type)
}

// TestSDKClient_Subscribe失败 测试订阅失败透传错误。
func TestSDKClient_Subscribe失败(t *testing.T) {
	c := &sdkClient{client: &fakeSDKClient{consumerErr: assert.AnError}}

	_, err := c.Subscribe("topic.a", "default")
	assert.ErrorIs(t, err, assert.AnError)
}

// TestSDKClient_Close 测试关闭客户端透传。
func TestSDKClient_Close(t *testing.T) {
	client := &fakeSDKClient{}
	c := &sdkClient{client: client}

	c.Close()

	assert.Equal(t, 1, client.closed)
}

// TestSDKConsumer_Receive 测试接收消息被包成窄接口。
func TestSDKConsumer_Receive(t *testing.T) {
	inner := &fakeSDKMessage{payload: []byte("x")}
	c := &sdkConsumer{consumer: &fakeSDKConsumer{msg: inner}}

	got, err := c.Receive(context.Background())
	require.NoError(t, err)

	msg, ok := got.(*sdkMessage)
	require.True(t, ok, "期望 *sdkMessage，实际 %T", got)
	assert.Equal(t, []byte("x"), msg.Payload())
}

// TestSDKConsumer_Receive失败 测试接收失败透传错误。
func TestSDKConsumer_Receive失败(t *testing.T) {
	c := &sdkConsumer{consumer: &fakeSDKConsumer{receiveErr: assert.AnError}}

	_, err := c.Receive(context.Background())
	assert.ErrorIs(t, err, assert.AnError)
}

// TestSDKConsumer_Ack 测试确认消息透传。
func TestSDKConsumer_Ack(t *testing.T) {
	inner := &fakeSDKMessage{}
	consumer := &fakeSDKConsumer{}
	c := &sdkConsumer{consumer: consumer}

	require.NoError(t, c.Ack(&sdkMessage{msg: inner}))

	assert.Same(t, inner, consumer.acked)
}

// TestSDKConsumer_Ack非sdkMessage 测试窄接口消息类型不符时静默跳过。
func TestSDKConsumer_Ack非sdkMessage(t *testing.T) {
	c := &sdkConsumer{consumer: &fakeSDKConsumer{}}

	assert.NoError(t, c.Ack(&fakeMessage{}))
}

// TestSDKConsumer_Ack失败 测试确认失败透传错误。
func TestSDKConsumer_Ack失败(t *testing.T) {
	c := &sdkConsumer{consumer: &fakeSDKConsumer{ackErr: assert.AnError}}

	assert.ErrorIs(t, c.Ack(&sdkMessage{}), assert.AnError)
}

// TestSDKConsumer_Close 测试关闭消费者透传。
func TestSDKConsumer_Close(t *testing.T) {
	consumer := &fakeSDKConsumer{}
	c := &sdkConsumer{consumer: consumer}

	c.Close()

	assert.Equal(t, 1, consumer.closed)
}

// TestSDKProducer_Send 测试发送把 partitionKey 写入 Key。
func TestSDKProducer_Send(t *testing.T) {
	inner := &fakeSDKProducer{}
	p := &sdkProducer{producer: inner}

	require.NoError(t, p.Send(context.Background(), []byte("body"), "mid-1"))

	require.NotNil(t, inner.sent)
	assert.Equal(t, []byte("body"), inner.sent.Payload)
	assert.Equal(t, "mid-1", inner.sent.Key)
}

// TestSDKProducer_Send失败 测试发送失败透传错误。
func TestSDKProducer_Send失败(t *testing.T) {
	p := &sdkProducer{producer: &fakeSDKProducer{sendErr: assert.AnError}}

	err := p.Send(context.Background(), []byte("body"), "mid-1")
	assert.ErrorIs(t, err, assert.AnError)
}

// TestSDKProducer_Close 测试关闭生产者透传。
func TestSDKProducer_Close(t *testing.T) {
	inner := &fakeSDKProducer{}
	p := &sdkProducer{producer: inner}

	p.Close()

	assert.Equal(t, 1, inner.closed)
}

// TestSDKMessage_Payload 测试消息体透传。
func TestSDKMessage_Payload(t *testing.T) {
	m := &sdkMessage{msg: &fakeSDKMessage{payload: []byte("body")}}

	assert.Equal(t, []byte("body"), m.Payload())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// CreateProducer 实现 pulsar.Client。
func (c *fakeSDKClient) CreateProducer(opts pulsar.ProducerOptions) (pulsar.Producer, error) {
	c.gotProducerTopic = opts.Topic
	if c.producerErr != nil {
		return nil, c.producerErr
	}
	return c.producer, nil
}

// Subscribe 实现 pulsar.Client。
func (c *fakeSDKClient) Subscribe(opts pulsar.ConsumerOptions) (pulsar.Consumer, error) {
	c.gotConsumerOptions = opts
	if c.consumerErr != nil {
		return nil, c.consumerErr
	}
	return c.consumer, nil
}

// Close 实现 pulsar.Client。
func (c *fakeSDKClient) Close() { c.closed++ }

// Send 实现 pulsar.Producer。
func (p *fakeSDKProducer) Send(ctx context.Context, msg *pulsar.ProducerMessage) (pulsar.MessageID, error) {
	p.sent = msg
	if p.sendErr != nil {
		return nil, p.sendErr
	}
	return nil, nil
}

// Close 实现 pulsar.Producer。
func (p *fakeSDKProducer) Close() { p.closed++ }

// Receive 实现 pulsar.Consumer。
func (c *fakeSDKConsumer) Receive(ctx context.Context) (pulsar.Message, error) {
	if c.receiveErr != nil {
		return nil, c.receiveErr
	}
	return c.msg, nil
}

// Ack 实现 pulsar.Consumer。
func (c *fakeSDKConsumer) Ack(msg pulsar.Message) error {
	c.acked = msg
	return c.ackErr
}

// Close 实现 pulsar.Consumer。
func (c *fakeSDKConsumer) Close() { c.closed++ }

// Payload 实现 pulsar.Message。
func (m *fakeSDKMessage) Payload() []byte { return m.payload }
