package message_queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageQueueInMemory 基于 Go channel 的内存消息队列。
//
// 对齐 Python MessageQueueInMemory，支持：
//   - 按 topic 路由消息到不同订阅者
//   - 火忘发布（QueueMessage，不等待）
//   - 同步发布（InvokeQueueMessage，等待处理完成）
//   - 流式发布（StreamQueueMessage，等待流式处理结果）
//   - 订阅生命周期管理（Activate/Deactivate）
//
// 对应 Python: openjiuwen/core/runner/message_queue_inmemory.py
type MessageQueueInMemory struct {
	// maxSize 单个 topic 的 channel 缓冲大小
	maxSize int
	// timeout 消息处理超时
	timeout time.Duration
	// 主题 主题名 → *主题订阅
	topics map[string]*topicSubscription
	// mu 保护 topics map
	mu sync.RWMutex
	// running 队列是否运行中
	running atomic.Bool
}

// topicSubscription 非导出的 topic 订阅实体
type topicSubscription struct {
	// topic topic 名称
	topic string
	// ch 消息缓冲 channel
	ch chan *internalMessage
	// handler 消息处理回调
	handler func(ctx context.Context, payload map[string]any) (any, error)
	// handlerMu 保护 handler 字段
	handlerMu sync.RWMutex
	// active 消费 goroutine 是否活跃
	active atomic.Bool
	// cancel 消费 goroutine 取消函数
	cancel context.CancelFunc
	// done 消费 goroutine 退出信号
	done chan struct{}
	// timeout 消息处理超时（对齐 Python SubscriptionInMemory._timeout）
	timeout time.Duration
}

// internalMessage 内部消息封装，携带原始消息和类型断言结果
type internalMessage struct {
	// payload 消息载荷
	payload map[string]any
	// invoke 同步消息（类型断言 *InvokeQueueMessage 结果）
	invoke *InvokeQueueMessage
	// stream 流式消息（类型断言 *StreamQueueMessage 结果）
	stream *StreamQueueMessage
}

// Subscription 导出的订阅句柄，实现 SubscriptionBase 接口。
//
// 对应 Python: MessageQueueInMemory.subscribe() 返回的 Subscription 对象
type Subscription struct {
	// ts 内部 topic 订阅实体
	ts *topicSubscription
}

// ──────────────────────────── 全局变量 ────────────────────────────
var (
	// ErrQueueNotRunning 队列未运行
	ErrQueueNotRunning = errors.New("message queue is not running")
	// ErrQueueClosed 队列已关闭
	ErrQueueClosed = errors.New("message queue is closed")
	// ErrTopicNotFound topic 不存在
	ErrTopicNotFound = errors.New("topic not found")
	// ErrSubscriptionNotActive 订阅未激活
	ErrSubscriptionNotActive = errors.New("subscription is not active")
	// ErrHandlerNotSet handler 未设置
	ErrHandlerNotSet = errors.New("message handler not set")
	// ErrTopicAlreadySubscribed topic 已被订阅
	ErrTopicAlreadySubscribed = errors.New("topic is already subscribed")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageQueueInMemory 创建内存消息队列。
//
// 对应 Python: MessageQueueInMemory(queue_max_size, timeout)
func NewMessageQueueInMemory(maxSize int, timeout time.Duration) *MessageQueueInMemory {
	return &MessageQueueInMemory{
		maxSize: maxSize,
		timeout: timeout,
		topics:  make(map[string]*topicSubscription),
	}
}

// Start 启动消息队列。实现 MessageQueueBase 接口。
//
// 对应 Python: MessageQueueInMemory.start()
func (q *MessageQueueInMemory) Start() {
	q.running.Store(true)
	logger.Info(logger.ComponentCommon).
		Str("event_type", "message_queue_started").
		Msg("消息队列已启动")
}

// Stop 停止消息队列，取消所有订阅和消费 goroutine。实现 MessageQueueBase 接口。
//
// 对应 Python: MessageQueueInMemory.stop()
func (q *MessageQueueInMemory) Stop(ctx context.Context) error {
	if !q.running.Load() {
		return nil
	}
	q.running.Store(false)

	q.mu.Lock()
	defer q.mu.Unlock()

	for topic, ts := range q.topics {
		ts.deactivate()
		close(ts.ch)
		delete(q.topics, topic)
	}

	logger.Info(logger.ComponentCommon).
		Str("event_type", "message_queue_stopped").
		Msg("消息队列已停止")
	return nil
}

// Subscribe 创建 topic 订阅。实现 MessageQueueBase 接口。
// 返回 SubscriptionBase 接口，调用方可通过类型断言获取 *Subscription。
// 若 topic 已被订阅，返回 ErrTopicAlreadySubscribed 错误。
//
// 对应 Python: MessageQueueInMemory.subscribe(topic)
// Python 中重复订阅同一 topic 抛 ValueError。
func (q *MessageQueueInMemory) Subscribe(topic string) (SubscriptionBase, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, exists := q.topics[topic]; exists {
		return nil, ErrTopicAlreadySubscribed
	}

	ts := &topicSubscription{
		topic:   topic,
		ch:      make(chan *internalMessage, q.maxSize),
		done:    make(chan struct{}),
		timeout: q.timeout,
	}
	// 标记 done 已关闭（初始状态非活跃）
	close(ts.done)
	q.topics[topic] = ts

	return &Subscription{ts: ts}, nil
}

// Unsubscribe 取消订阅，停止消费 goroutine 并移除 topic。实现 MessageQueueBase 接口。
//
// 对应 Python: MessageQueueInMemory.unsubscribe(topic)
func (q *MessageQueueInMemory) Unsubscribe(ctx context.Context, topic string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	ts, exists := q.topics[topic]
	if !exists {
		return ErrTopicNotFound
	}

	ts.deactivate()
	close(ts.ch)
	delete(q.topics, topic)

	logger.Info(logger.ComponentCommon).
		Str("event_type", "message_queue_unsubscribed").
		Str("topic", topic).
		Msg("topic 订阅已取消")
	return nil
}

// Produce 发布消息到指定 topic。实现 MessageQueueBase 接口。
// 消息类型决定发布模式，对齐 Python isinstance 判断：
//   - *QueueMessage: 火忘发布，不等待处理完成
//   - *InvokeQueueMessage: 同步发布，等待处理完成
//   - *StreamQueueMessage: 流式发布，等待流式处理结果
//
// 对齐 Python: MessageQueueInMemory.produce_message(topic, queue_message)
func (q *MessageQueueInMemory) Produce(ctx context.Context, topic string, msg QueueMessageBase) error {
	if !q.running.Load() {
		return ErrQueueNotRunning
	}

	q.mu.RLock()
	ts, exists := q.topics[topic]
	q.mu.RUnlock()

	if !exists {
		return ErrTopicNotFound
	}

	// 通过类型断言判断消息类型，对齐 Python isinstance 模式
	im := &internalMessage{payload: msg.GetPayload()}
	switch m := msg.(type) {
	case *InvokeQueueMessage:
		im.invoke = m
	case *StreamQueueMessage:
		im.stream = m
	}

	select {
	case ts.ch <- im:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SetMessageHandler 设置消息处理回调。实现 SubscriptionBase 接口。
func (s *Subscription) SetMessageHandler(handler func(ctx context.Context, payload map[string]any) (any, error)) {
	s.ts.handlerMu.Lock()
	defer s.ts.handlerMu.Unlock()
	s.ts.handler = handler
}

// Activate 激活订阅，启动消费 goroutine。实现 SubscriptionBase 接口。
//
// 对齐 Python: Subscription.activate()
func (s *Subscription) Activate() {
	s.ts.activate()
}

// Deactivate 停用订阅，停止消费 goroutine。实现 SubscriptionBase 接口。
//
// 对应 Python: Subscription.deactivate()
func (s *Subscription) Deactivate() {
	s.ts.deactivate()
}

// IsActive 返回订阅是否活跃。实现 SubscriptionBase 接口。
func (s *Subscription) IsActive() bool {
	return s.ts.active.Load()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// activate 启动消费 goroutine。
func (ts *topicSubscription) activate() {
	if ts.active.Load() {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	ts.cancel = cancel
	ts.done = make(chan struct{})
	ts.active.Store(true)

	go ts.consume(ctx)

	logger.Info(logger.ComponentCommon).
		Str("event_type", "subscription_activated").
		Str("topic", ts.topic).
		Msg("topic 订阅已激活")
}

// deactivate 停止消费 goroutine。
func (ts *topicSubscription) deactivate() {
	if !ts.active.Load() {
		return
	}
	ts.active.Store(false)
	if ts.cancel != nil {
		ts.cancel()
	}
	// 等待消费 goroutine 退出
	if ts.done != nil {
		<-ts.done
	}
	logger.Info(logger.ComponentCommon).
		Str("event_type", "subscription_deactivated").
		Str("topic", ts.topic).
		Msg("topic 订阅已停用")
}

// consume 消费 goroutine 主循环。
func (ts *topicSubscription) consume(ctx context.Context) {
	defer close(ts.done)

	for {
		select {
		case im, ok := <-ts.ch:
			if !ok {
				// channel 已关闭
				return
			}
			ts.handleMessage(ctx, im)
		case <-ctx.Done():
			return
		}
	}
}

// handleMessage 处理单条消息。
// 通过 internalMessage.invoke / stream 字段判断消息类型，
// 对齐 Python SubscriptionInMemory._handle_response 的 isinstance 判断。
func (ts *topicSubscription) handleMessage(ctx context.Context, im *internalMessage) {
	ts.handlerMu.RLock()
	handler := ts.handler
	ts.handlerMu.RUnlock()

	if handler == nil {
		logger.Warn(logger.ComponentCommon).
			Str("event_type", "message_handler_not_set").
			Str("topic", ts.topic).
			Msg("消息处理回调未设置，丢弃消息")
		if im.invoke != nil {
			im.invoke.CompleteResponse(nil, ErrHandlerNotSet)
		}
		if im.stream != nil {
			im.stream.CompleteResponse(nil, ErrHandlerNotSet)
		}
		return
	}

	// 处理超时控制
	var result any
	var err error
	if ts.timeout > 0 {
		handleCtx, cancel := context.WithTimeout(ctx, ts.timeout)
		result, err = handler(handleCtx, im.payload)
		cancel()
	} else {
		result, err = handler(ctx, im.payload)
	}

	// 根据消息类型通知调用方，对齐 Python _handle_response
	if im.invoke != nil {
		im.invoke.CompleteResponse(result, err)
	}
	if im.stream != nil {
		im.stream.CompleteResponse(result, err)
	}
}
