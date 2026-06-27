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
//   - 同步发布（InvokeQueueMessage，等待处理完成）
//   - 火忘发布（QueueMessage，不等待）
//   - 订阅生命周期管理（Activate/Deactivate）
//
// 对应 Python: openjiuwen/core/runner/message_queue_inmemory.py
type MessageQueueInMemory struct {
	// maxSize 单个 topic 的 channel 缓冲大小
	maxSize int
	// timeout 消息处理超时
	timeout time.Duration
	// topics topic name → *topicSubscription
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
}

// internalMessage 内部消息封装，区分 QueueMessage 和 InvokeQueueMessage
type internalMessage struct {
	// payload 消息载荷
	payload map[string]any
	// invoke 同步消息（非 nil 时为同步发布）
	invoke *InvokeQueueMessage
}

// Subscription 导出的订阅句柄
//
// 对应 Python: MessageQueueInMemory.subscribe() 返回的 Subscription 对象
type Subscription struct {
	// ts 内部 topic 订阅实体
	ts *topicSubscription
}

// ──────────────────────────── 常量 ────────────────────────────

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

// Start 启动消息队列。
//
// 对应 Python: MessageQueueInMemory.start()
func (q *MessageQueueInMemory) Start() {
	q.running.Store(true)
	logger.Info(logger.ComponentCommon).
		Str("event_type", "message_queue_started").
		Msg("消息队列已启动")
}

// Stop 停止消息队列，取消所有订阅和消费 goroutine。
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

// Subscribe 创建或获取 topic 订阅。
//
// 对应 Python: MessageQueueInMemory.subscribe(topic)
func (q *MessageQueueInMemory) Subscribe(topic string) *Subscription {
	q.mu.Lock()
	defer q.mu.Unlock()

	if ts, exists := q.topics[topic]; exists {
		return &Subscription{ts: ts}
	}

	ts := &topicSubscription{
		topic: topic,
		ch:    make(chan *internalMessage, q.maxSize),
		done:  make(chan struct{}),
	}
	// 标记 done 已关闭（初始状态非活跃）
	close(ts.done)
	q.topics[topic] = ts

	return &Subscription{ts: ts}
}

// Unsubscribe 取消订阅，停止消费 goroutine 并移除 topic。
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

// Produce 向指定 topic 生产消息。
//
// 对应 Python: MessageQueueInMemory.produce_message(topic, queue_message)
func (q *MessageQueueInMemory) Produce(ctx context.Context, topic string, msg *QueueMessage, invoke *InvokeQueueMessage) error {
	if !q.running.Load() {
		return ErrQueueNotRunning
	}

	q.mu.RLock()
	ts, exists := q.topics[topic]
	q.mu.RUnlock()

	if !exists {
		return ErrTopicNotFound
	}

	im := &internalMessage{payload: msg.Payload, invoke: invoke}

	select {
	case ts.ch <- im:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SetMessageHandler 设置消息处理回调。
func (s *Subscription) SetMessageHandler(handler func(ctx context.Context, payload map[string]any) (any, error)) {
	s.ts.handlerMu.Lock()
	defer s.ts.handlerMu.Unlock()
	s.ts.handler = handler
}

// Activate 激活订阅，启动消费 goroutine。
//
// 对应 Python: Subscription.activate()
// timeout 参数来自 MessageQueueInMemory 的 timeout 配置。
func (s *Subscription) Activate(timeout time.Duration) {
	s.ts.activate(timeout)
}

// Deactivate 停用订阅，停止消费 goroutine。
//
// 对应 Python: Subscription.deactivate()
func (s *Subscription) Deactivate() {
	s.ts.deactivate()
}

// IsActive 返回订阅是否活跃。
func (s *Subscription) IsActive() bool {
	return s.ts.active.Load()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// activate 启动消费 goroutine。
func (ts *topicSubscription) activate(timeout time.Duration) {
	if ts.active.Load() {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	ts.cancel = cancel
	ts.done = make(chan struct{})
	ts.active.Store(true)

	go ts.consume(ctx, timeout)

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
func (ts *topicSubscription) consume(ctx context.Context, timeout time.Duration) {
	defer close(ts.done)

	for {
		select {
		case im, ok := <-ts.ch:
			if !ok {
				// channel 已关闭
				return
			}
			ts.handleMessage(ctx, im, timeout)
		case <-ctx.Done():
			return
		}
	}
}

// handleMessage 处理单条消息。
func (ts *topicSubscription) handleMessage(ctx context.Context, im *internalMessage, timeout time.Duration) {
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
		return
	}

	// 处理超时控制
	var result any
	var err error
	if timeout > 0 {
		handleCtx, cancel := context.WithTimeout(ctx, timeout)
		result, err = handler(handleCtx, im.payload)
		cancel()
	} else {
		result, err = handler(ctx, im.payload)
	}

	// 如果是同步消息，通知调用方处理完成
	if im.invoke != nil {
		im.invoke.CompleteResponse(result, err)
	}
}
