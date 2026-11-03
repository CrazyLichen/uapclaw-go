package message_queue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeMessage 测试用消息
type fakeMessage struct {
	// payload 消息体
	payload []byte
}

// fakeProducer 测试用生产者
type fakeProducer struct {
	// sentPayloads 已发送的消息体
	sentPayloads []string
	// sentKeys 已发送的 partitionKey
	sentKeys []string
	// sendErr 发送错误
	sendErr error
	// closed Close 调用次数
	closed int
}

// fakeConsumer 测试用消费者
type fakeConsumer struct {
	// messages 待消费消息
	messages []pulsarMessageAPI
	// idx 当前消费位置
	idx int
	// receiveErr 接收错误
	receiveErr error
	// ackCount Ack 调用次数
	ackCount int
	// closed Close 调用次数
	closed int
}

// fakeClient 测试用客户端
type fakeClient struct {
	// producer 返回的生产者
	producer *fakeProducer
	// consumer 返回的消费者
	consumer *fakeConsumer
	// producerErr 建生产者错误
	producerErr error
	// consumerErr 建消费者错误
	consumerErr error
	// topics CreateProducer 收到的 topic 列表
	topics []string
	// subTopics Subscribe 收到的 topic 列表
	subTopics []string
	// closed Close 调用次数
	closed int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNarrowInterfaces_可被fake实现 测试三个窄接口可被测试替身满足。
func TestNarrowInterfaces_可被fake实现(t *testing.T) {
	var _ pulsarClientAPI = &fakeClient{}
	var _ pulsarConsumerAPI = &fakeConsumer{}
	var _ pulsarProducerAPI = &fakeProducer{}
	var _ pulsarMessageAPI = &fakeMessage{}

	assert.NotNil(t, &fakeClient{})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Payload 实现 pulsarMessageAPI。
func (m *fakeMessage) Payload() []byte { return m.payload }

// Send 实现 pulsarProducerAPI。
func (p *fakeProducer) Send(ctx context.Context, payload []byte, partitionKey string) error {
	if p.sendErr != nil {
		return p.sendErr
	}
	p.sentPayloads = append(p.sentPayloads, string(payload))
	p.sentKeys = append(p.sentKeys, partitionKey)
	return nil
}

// Close 实现 pulsarProducerAPI。
func (p *fakeProducer) Close() { p.closed++ }

// Receive 实现 pulsarConsumerAPI。
// 无待收消息时阻塞到 ctx 到期，模拟真实 SDK 的阻塞语义。
func (c *fakeConsumer) Receive(ctx context.Context) (pulsarMessageAPI, error) {
	if c.receiveErr != nil {
		return nil, c.receiveErr
	}
	if c.idx >= len(c.messages) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	msg := c.messages[c.idx]
	c.idx++
	return msg, nil
}

// Ack 实现 pulsarConsumerAPI。
func (c *fakeConsumer) Ack(msg pulsarMessageAPI) error {
	c.ackCount++
	return nil
}

// Close 实现 pulsarConsumerAPI。
func (c *fakeConsumer) Close() { c.closed++ }

// CreateProducer 实现 pulsarClientAPI。
func (c *fakeClient) CreateProducer(topic string) (pulsarProducerAPI, error) {
	if c.producerErr != nil {
		return nil, c.producerErr
	}
	c.topics = append(c.topics, topic)
	return c.producer, nil
}

// Subscribe 实现 pulsarClientAPI。
func (c *fakeClient) Subscribe(topic string, subscriptionName string) (pulsarConsumerAPI, error) {
	if c.consumerErr != nil {
		return nil, c.consumerErr
	}
	c.subTopics = append(c.subTopics, topic)
	return c.consumer, nil
}

// Close 实现 pulsarClientAPI。
func (c *fakeClient) Close() { c.closed++ }
