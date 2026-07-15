package message_queue

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LocalMessageQueue 本地消息队列 no-op 实现。
// start/stop 均返回成功，不做任何实际操作。
//
// 对应 Python: LocalMessageQueue
// Python 中 LocalMessageQueue 是 Runner._message_queue 的默认实现，
// 仅提供 start()/stop() 返回 True 的空操作，使得 Runner.start 在非分布式模式下正常启动。
type LocalMessageQueue struct{}

// LocalSubscription 本地订阅 no-op 实现。
// 所有方法均为空操作，满足 SubscriptionBase 接口。
//
// 对应 Python: SubscriptionBase 的 no-op 默认实现
type LocalSubscription struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Start 启动本地消息队列（no-op，返回 nil 表示成功）。
//
// 对应 Python: LocalMessageQueue.start() → True
func (q *LocalMessageQueue) Start() {
	logger.Info(logger.ComponentCommon).
		Str("event_type", "local_message_queue_started").
		Msg("本地消息队列已启动（no-op）")
}

// Stop 停止本地消息队列（no-op，返回 nil 表示成功）。
//
// 对应 Python: LocalMessageQueue.stop() → True
func (q *LocalMessageQueue) Stop(_ context.Context) error {
	logger.Info(logger.ComponentCommon).
		Str("event_type", "local_message_queue_stopped").
		Msg("本地消息队列已停止（no-op）")
	return nil
}

// Subscribe 创建 topic 订阅（返回 no-op LocalSubscription）。
//
// 对应 Python: MessageQueueBase.subscribe() 的本地 no-op 实现
func (q *LocalMessageQueue) Subscribe(topic string) (SubscriptionBase, error) {
	logger.Info(logger.ComponentCommon).
		Str("event_type", "local_message_queue_subscribed").
		Str("topic", topic).
		Msg("本地消息队列订阅已创建（no-op）")
	return &LocalSubscription{}, nil
}

// Unsubscribe 取消 topic 订阅（no-op）。
//
// 对应 Python: MessageQueueBase.unsubscribe() 的本地 no-op 实现
func (q *LocalMessageQueue) Unsubscribe(_ context.Context, topic string) error {
	logger.Info(logger.ComponentCommon).
		Str("event_type", "local_message_queue_unsubscribed").
		Str("topic", topic).
		Msg("本地消息队列订阅已取消（no-op）")
	return nil
}

// Produce 发布消息（no-op，消息被丢弃）。
// 接受 QueueMessageBase 接口，对齐 Python produce_message(topic, message) 签名。
//
// 对应 Python: MessageQueueBase.produce_message() 的本地 no-op 实现
func (q *LocalMessageQueue) Produce(_ context.Context, topic string, _ QueueMessageBase) error {
	logger.Debug(logger.ComponentCommon).
		Str("event_type", "local_message_queue_produce").
		Str("topic", topic).
		Msg("本地消息队列发布消息（no-op，消息丢弃）")
	return nil
}

// SetMessageHandler 设置消息处理回调（no-op）。
func (s *LocalSubscription) SetMessageHandler(_ func(ctx context.Context, payload map[string]any) (any, error)) {
	// 空操作
}

// Activate 激活订阅（no-op）。
func (s *LocalSubscription) Activate() {
	// 空操作
}

// Deactivate 停用订阅（no-op）。
func (s *LocalSubscription) Deactivate() {
	// 空操作
}

// IsActive 返回 false（本地订阅永远不活跃）。
func (s *LocalSubscription) IsActive() bool {
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────
