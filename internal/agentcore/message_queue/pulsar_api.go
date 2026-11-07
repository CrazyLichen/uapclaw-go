package message_queue

import (
	"context"

	"github.com/apache/pulsar-client-go/pulsar"
)

// ──────────────────────────── 结构体 ────────────────────────────

// pulsarClientAPI Pulsar 客户端窄接口
type pulsarClientAPI interface {
	// CreateProducer 创建指定 topic 的 producer
	CreateProducer(topic string) (pulsarProducerAPI, error)
	// Subscribe 订阅 topic，返回 KeyShared 消费者
	Subscribe(topic string, subscriptionName string) (pulsarConsumerAPI, error)
	// Close 关闭客户端
	Close()
}

// pulsarConsumerAPI Pulsar 消费者窄接口
type pulsarConsumerAPI interface {
	// Receive 阻塞接收一条消息，ctx 到期返回错误
	Receive(ctx context.Context) (pulsarMessageAPI, error)
	// Ack 确认消息
	Ack(msg pulsarMessageAPI) error
	// Close 关闭消费者
	Close()
}

// pulsarProducerAPI Pulsar 生产者窄接口
type pulsarProducerAPI interface {
	// Send 发送消息，partitionKey 决定分区与顺序
	Send(ctx context.Context, payload []byte, partitionKey string) error
	// Close 关闭生产者
	Close()
}

// pulsarMessageAPI Pulsar 消息窄接口
type pulsarMessageAPI interface {
	// Payload 返回消息体
	Payload() []byte
}

// sdkClient apache/pulsar-client-go 客户端适配器
type sdkClient struct {
	// client 真实 SDK 客户端
	client pulsar.Client
}

// sdkConsumer 真实消费者适配器
type sdkConsumer struct {
	// consumer 真实 SDK 消费者
	consumer pulsar.Consumer
}

// sdkProducer 真实生产者适配器
type sdkProducer struct {
	// producer 真实 SDK 生产者
	producer pulsar.Producer
}

// sdkMessage 真实消息适配器
type sdkMessage struct {
	// msg 真实 SDK 消息
	msg pulsar.Message
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newSDKClient 创建真实 Pulsar 客户端。
// Python: pulsar.Client(self._url)
// Go SDK 的 NewClient 返回 error，调用方需处理（Start 无返回值，只能记日志）。
func newSDKClient(url string) (pulsarClientAPI, error) {
	client, err := pulsar.NewClient(pulsar.ClientOptions{URL: url})
	if err != nil {
		return nil, err
	}
	return &sdkClient{client: client}, nil
}

// CreateProducer 创建 producer 并包成窄接口。
func (c *sdkClient) CreateProducer(topic string) (pulsarProducerAPI, error) {
	producer, err := c.client.CreateProducer(pulsar.ProducerOptions{Topic: topic})
	if err != nil {
		return nil, err
	}
	return &sdkProducer{producer: producer}, nil
}

// Subscribe 创建 KeyShared 消费者并包成窄接口。
func (c *sdkClient) Subscribe(topic string, subscriptionName string) (pulsarConsumerAPI, error) {
	consumer, err := c.client.Subscribe(pulsar.ConsumerOptions{
		Topic:            topic,
		SubscriptionName: subscriptionName,
		Type:             pulsar.KeyShared,
	})
	if err != nil {
		return nil, err
	}
	return &sdkConsumer{consumer: consumer}, nil
}

// Close 关闭客户端。
func (c *sdkClient) Close() { c.client.Close() }

// Receive 接收一条消息并包成窄接口。
func (c *sdkConsumer) Receive(ctx context.Context) (pulsarMessageAPI, error) {
	msg, err := c.consumer.Receive(ctx)
	if err != nil {
		return nil, err
	}
	return &sdkMessage{msg: msg}, nil
}

// Ack 确认消息。
func (c *sdkConsumer) Ack(msg pulsarMessageAPI) error {
	sdkMsg, ok := msg.(*sdkMessage)
	if !ok {
		return nil
	}
	return c.consumer.Ack(sdkMsg.msg)
}

// Close 关闭消费者。
func (c *sdkConsumer) Close() { c.consumer.Close() }

// Send 发送消息，partitionKey 写入 ProducerMessage.Key。
// Python: producer.send(content, partition_key=message.message_id)
func (p *sdkProducer) Send(ctx context.Context, payload []byte, partitionKey string) error {
	_, err := p.producer.Send(ctx, &pulsar.ProducerMessage{
		Payload: payload,
		Key:     partitionKey,
	})
	return err
}

// Close 关闭生产者。
func (p *sdkProducer) Close() { p.producer.Close() }

// Payload 返回消息体。
func (m *sdkMessage) Payload() []byte { return m.msg.Payload() }
