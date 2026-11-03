package message_queue

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/drunner/dmessage_queue"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// producerEntry producer LRU 链表节点载荷
type producerEntry struct {
	// topic 该 producer 对应的 topic
	topic string
	// producer 生产者窄接口
	producer pulsarProducerAPI
}

// MessageQueuePulsar Pulsar 消息队列封装。
//
// Python: MessageQueuePulsar(MessageQueueBase)
type MessageQueuePulsar struct {
	// url Pulsar 服务地址
	url string
	// maxWorkers 工作线程数（仅保存，Go 侧不消费，偏差 D1）
	maxWorkers int
	// maxProducers producer LRU 容量上限
	maxProducers int
	// client Pulsar 客户端窄接口
	client pulsarClientAPI
	// startClient 建客户端钩子，便于测试注入
	startClient func(url string) (pulsarClientAPI, error)
	// producers topic → LRU 链表节点
	producers map[string]*list.Element
	// producerOrder producer LRU 顺序（Front 最旧）
	producerOrder *list.List
	// subs topic → 订阅
	subs map[string]*PulsarSubscription
	// mu 保护 producers/producerOrder/subs（单锁，偏差 D2）
	mu sync.Mutex
	// running 是否运行中
	running atomic.Bool
}

// PulsarSubscription Pulsar topic 订阅。
//
// Python: PulsarSubscription(SubscriptionBase)
type PulsarSubscription struct {
	// topic 订阅的 topic 名
	topic string
	// consumer Pulsar 消费者窄接口
	consumer pulsarConsumerAPI
	// handler 消息处理回调
	handler func(ctx context.Context, payload map[string]any) (any, error)
	// handlerMu 保护 handler
	handlerMu sync.RWMutex
	// stateMu 保护 cancel/done
	stateMu sync.Mutex
	// cancel 消费 goroutine 取消函数
	cancel context.CancelFunc
	// done 消费 goroutine 退出信号
	done chan struct{}
	// active 是否活跃
	active atomic.Bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// MaxProducers producer LRU 容量上限。
//
// Python: MessageQueuePulsar.MAX_PRODUCERS = 10000
const MaxProducers = 10000

// DefaultSubscriptionName 默认订阅名。
//
// Python: MessageQueuePulsar.DEFAULT_SUBSCRIPTION_NAME = "default"
const DefaultSubscriptionName = "default"

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// errQueueNotRunning 消息队列未运行
var errQueueNotRunning = errors.New("pulsar message queue is not running")

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageQueuePulsar 创建 Pulsar 消息队列，不建立连接。
//
// Python: MessageQueuePulsar.__init__(pulsar_config)
func NewMessageQueuePulsar(cfg *config.PulsarConfig) *MessageQueuePulsar {
	url := ""
	maxWorkers := 8
	if cfg != nil {
		url = cfg.URL
		if cfg.MaxWorkers > 0 {
			maxWorkers = cfg.MaxWorkers
		}
	}
	return &MessageQueuePulsar{
		url:           url,
		maxWorkers:    maxWorkers,
		maxProducers:  MaxProducers,
		startClient:   newSDKClient,
		producers:     make(map[string]*list.Element),
		producerOrder: list.New(),
		subs:          make(map[string]*PulsarSubscription),
	}
}

// Start 启动消息队列。
//
// Python: MessageQueuePulsar.start() L101-107
// Go 侧 start 无返回值（接口约束），建客户端失败时记 Error 日志并保持未运行。
func (q *MessageQueuePulsar) Start() {
	if q.running.Load() {
		return
	}

	client, err := q.startClient(q.url)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "mq_pulsar_start_failed").
			Str("url", q.url).
			Err(err).
			Msg("create pulsar client failed")
		return
	}

	q.client = client
	q.running.Store(true)
	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_started").
		Str("url", q.url).
		Msg("pulsar message queue started")
}

// Stop 停止消息队列，关闭全部订阅与 producer。
//
// Python: MessageQueuePulsar.stop() L109-124
func (q *MessageQueuePulsar) Stop(ctx context.Context) error {
	if !q.running.Load() {
		return nil
	}
	q.running.Store(false)

	q.mu.Lock()
	topics := make([]string, 0, len(q.subs))
	for topic := range q.subs {
		topics = append(topics, topic)
	}
	q.mu.Unlock()

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_closing_subscriptions").
		Int("subscription_count", len(topics)).
		Msg("closing subscriptions")
	for _, topic := range topics {
		if err := q.Unsubscribe(ctx, topic); err != nil {
			logger.Warn(logComponent).
				Str("event_type", "mq_pulsar_unsubscribe_failed").
				Str("topic", topic).
				Err(err).
				Msg("unsubscribe failed during stop")
		}
	}

	q.mu.Lock()
	producerCount := q.producerOrder.Len()
	for elem := q.producerOrder.Front(); elem != nil; elem = elem.Next() {
		elem.Value.(*producerEntry).producer.Close()
	}
	q.producers = make(map[string]*list.Element)
	q.producerOrder.Init()
	client := q.client
	q.client = nil
	q.mu.Unlock()

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_closing_producers").
		Int("producer_count", producerCount).
		Msg("closing producers")

	if client != nil {
		client.Close()
	}

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_stopped").
		Msg("pulsar message queue stopped")
	return nil
}

// Subscribe 创建 topic 订阅。
//
// Python: MessageQueuePulsar.subscribe(topic) L126-139
// Python 用 try/except 包住全部步骤（含 _validate_running），统一转成订阅错误。
func (q *MessageQueuePulsar) Subscribe(topic string) (message_queue.SubscriptionBase, error) {
	if !q.running.Load() {
		return nil, exception.BuildError(
			exception.StatusMessageQueueTopicSubscriptionError,
			exception.WithCause(errQueueNotRunning),
			exception.WithParam("topic", topic),
			exception.WithParam("reason", errQueueNotRunning),
		)
	}

	q.mu.Lock()
	if sub, ok := q.subs[topic]; ok {
		q.mu.Unlock()
		return sub, nil
	}
	q.mu.Unlock()

	consumer, err := q.client.Subscribe(topic, DefaultSubscriptionName)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusMessageQueueTopicSubscriptionError,
			exception.WithCause(err),
			exception.WithParam("topic", topic),
			exception.WithParam("reason", err),
		)
	}

	sub := newPulsarSubscription(topic, consumer)
	q.mu.Lock()
	q.subs[topic] = sub
	q.mu.Unlock()

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_subscription_created").
		Str("topic", topic).
		Msg("create new subscription")
	return sub, nil
}

// Unsubscribe 取消 topic 订阅。
//
// Python: MessageQueuePulsar.unsubscribe(topic) L141-145
// 不存在的 topic 返回 nil（对齐 Python pop(topic, None) 后空判）。
func (q *MessageQueuePulsar) Unsubscribe(ctx context.Context, topic string) error {
	q.mu.Lock()
	sub, ok := q.subs[topic]
	if ok {
		delete(q.subs, topic)
	}
	q.mu.Unlock()

	if !ok {
		return nil
	}

	sub.Deactivate()
	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_unsubscribed").
		Str("topic", topic).
		Msg("unsubscribed")
	return nil
}

// Produce 发布消息到指定 topic。
//
// Python: MessageQueuePulsar.produce_message(topic, message) L151-177
// Python 用 try/except 包住全部步骤（含 _validate_running），统一转成生产错误。
func (q *MessageQueuePulsar) Produce(ctx context.Context, topic string, msg message_queue.QueueMessageBase) error {
	if err := q.produceMessage(ctx, topic, msg); err != nil {
		if _, ok := err.(*exception.BaseError); ok {
			return err
		}
		return exception.BuildError(
			exception.StatusMessageQueueTopicMessageProductionError,
			exception.WithCause(err),
			exception.WithParam("topic", topic),
			exception.WithParam("message", msg),
			exception.WithParam("reason", err),
		)
	}
	return nil
}

// SetMessageHandler 设置消息处理回调。实现 SubscriptionBase 接口。
func (s *PulsarSubscription) SetMessageHandler(handler func(ctx context.Context, payload map[string]any) (any, error)) {
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	s.handler = handler
}

// Activate 激活订阅，启动消费 goroutine（幂等）。
//
// Python: PulsarSubscription.activate() L35-39
func (s *PulsarSubscription) Activate() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if s.active.Load() {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	s.active.Store(true)

	go s.consumeLoop(ctx)

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_subscription_activated").
		Str("topic", s.topic).
		Msg("pulsar subscription activated")
}

// Deactivate 停用订阅，同步等待消费 goroutine 退出（幂等）。
//
// Python: PulsarSubscription.deactivate() L41-59
// Go 接口无 ctx/error 返回（偏差 D3），改为同步阻塞，最长等待一个 receive 超时周期。
func (s *PulsarSubscription) Deactivate() {
	s.stateMu.Lock()
	if !s.active.Load() {
		s.stateMu.Unlock()
		return
	}
	s.active.Store(false)
	cancel := s.cancel
	done := s.done
	s.stateMu.Unlock()

	if cancel != nil {
		cancel()
	}
	// 关闭消费者以解除阻塞中的 Receive
	s.consumer.Close()
	if done != nil {
		<-done
	}

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_subscription_deactivated").
		Str("topic", s.topic).
		Msg("pulsar subscription deactivated")
}

// IsActive 返回订阅是否活跃。实现 SubscriptionBase 接口。
func (s *PulsarSubscription) IsActive() bool { return s.active.Load() }

// ──────────────────────────── 非导出函数 ────────────────────────────

// newPulsarSubscription 创建订阅。
func newPulsarSubscription(topic string, consumer pulsarConsumerAPI) *PulsarSubscription {
	return &PulsarSubscription{
		topic:    topic,
		consumer: consumer,
		done:     make(chan struct{}),
	}
}

// consumeLoop 消费循环主逻辑。
//
// Python: PulsarSubscription._consume_loop() L64-83
// 完整实现在 Task 11。
func (s *PulsarSubscription) consumeLoop(ctx context.Context) {
	defer close(s.done)
	<-ctx.Done()
}

// produceMessage 发布消息的内部实现。
func (q *MessageQueuePulsar) produceMessage(ctx context.Context, topic string, msg message_queue.QueueMessageBase) error {
	if !q.running.Load() {
		return errQueueNotRunning
	}

	producer, err := q.getOrCreateProducer(topic)
	if err != nil {
		return err
	}

	content, err := dmessage_queue.SerializeMessage(msg)
	if err != nil {
		return err
	}

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_sending").
		Str("topic", topic).
		Str("message_id", msg.GetMessageID()).
		Msg("sending message")

	if err := producer.Send(ctx, content, msg.GetMessageID()); err != nil {
		return err
	}

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_sent").
		Str("topic", topic).
		Str("message_id", msg.GetMessageID()).
		Msg("message sent successfully")
	return nil
}

// getOrCreateProducer 获取或创建 topic 对应的 producer，带 LRU 淘汰。
//
// Python: MessageQueuePulsar._get_or_create_producer(topic) L179-208
// 单锁内完成全部判断与创建（偏差 D2：取消 Python 的双检快路径，
// 因其无锁读 map 在 Go 属数据竞争）。
func (q *MessageQueuePulsar) getOrCreateProducer(topic string) (pulsarProducerAPI, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if elem, ok := q.producers[topic]; ok {
		q.producerOrder.MoveToBack(elem)
		return elem.Value.(*producerEntry).producer, nil
	}

	if len(q.producers) >= q.maxProducers {
		if front := q.producerOrder.Front(); front != nil {
			evicted := front.Value.(*producerEntry)
			q.producerOrder.Remove(front)
			delete(q.producers, evicted.topic)
			evicted.producer.Close()
			logger.Debug(logComponent).
				Str("event_type", "mq_pulsar_producer_evicted").
				Str("topic", evicted.topic).
				Msg("LRU producer evicted")
		}
	}

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_producer_creating").
		Str("topic", topic).
		Msg("creating new producer")

	producer, err := q.client.CreateProducer(topic)
	if err != nil {
		return nil, err
	}
	q.producers[topic] = q.producerOrder.PushBack(&producerEntry{topic: topic, producer: producer})
	return producer, nil
}
