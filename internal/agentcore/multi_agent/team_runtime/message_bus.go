package team_runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	message_queue "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

// MessageBusInterface 消息总线接口，TeamRuntime 通过此接口解耦对 MessageBus 的直接依赖。
// MessageBus 结构体和测试 mock 均实现此接口。
type MessageBusInterface interface {
	// Start 启动消息总线
	Start(ctx context.Context) error
	// Stop 停止消息总线
	Stop(ctx context.Context) error
	// CleanupSession 清理会话
	CleanupSession(ctx context.Context, sessionID string) error
	// Send P2P 发送消息
	Send(ctx context.Context, message any, recipient string, sender string, sessionID string, timeout float64) (any, error)
	// Publish Pub-Sub 发布消息
	Publish(ctx context.Context, message any, topicID string, sender string, sessionID string) error
	// AddSubscription 添加订阅
	AddSubscription(agentID, topic string)
	// RemoveSubscription 移除订阅
	RemoveSubscription(agentID, topic string)
	// RemoveAllSubscriptions 移除所有订阅
	RemoveAllSubscriptions(agentID string)
	// ListSubscriptions 列出订阅
	ListSubscriptions(agentID string) any
	// GetSubscriptionCount 获取订阅数
	GetSubscriptionCount() int
}

// MessageBusConfig 消息总线配置。
//
// 对应 Python: MessageBusConfig (openjiuwen/core/multi_agent/team_runtime/message_bus.py)
type MessageBusConfig struct {
	// MaxQueueSize 单个 topic 的 channel 缓冲大小，默认 1000
	MaxQueueSize int
	// ProcessTimeout 消息处理超时秒数，默认 1800.0
	ProcessTimeout float64
	// TeamID 团队标识
	TeamID string
}

// MessageBus 消息总线，基于 MessageQueueInMemory 实现 P2P 和 Pub-Sub 消息收发。
//
// 每个会话（sessionID）对应一对 topic：
//   - P2P topic:  {teamID}_{sessionID}__p2p__（无 sessionID 时为 {teamID}__p2p__）
//   - Pub-Sub topic: {teamID}_{sessionID}__pubsub__（无 sessionID 时为 {teamID}__pubsub__）
//
// 通过 SubscriptionManager 维护订阅关系，通过 MessageRouter 路由消息到目标 Agent。
//
// 对应 Python: MessageBus (openjiuwen/core/multi_agent/team_runtime/message_bus.py)
type MessageBus struct {
	// config 消息总线配置
	config MessageBusConfig
	// teamID 团队标识
	teamID string
	// mq 内存消息队列
	mq *message_queue.MessageQueueInMemory
	// activeSubscriptions 活跃订阅映射，topic → SubscriptionBase
	activeSubscriptions map[string]message_queue.SubscriptionBase
	// subscriptionLock 订阅操作读写互斥锁（双检锁：RLock 快速路径 + Lock 慢速路径）
	subscriptionLock sync.RWMutex
	// subscriptionManager 订阅管理器
	subscriptionManager *SubscriptionManager
	// router 消息路由器
	router *MessageRouter
	// running 是否运行中（使用 atomic.Bool 保证并发安全）
	running atomic.Bool
}

// MessageBusConfigOption 消息总线配置选项函数类型
type MessageBusConfigOption func(*MessageBusConfig)

// ──────────────────────────── 常量 ────────────────────────────
const (
	// p2pTopicSuffix P2P topic 后缀
	p2pTopicSuffix = "__p2p__"
	// pubsubTopicSuffix Pub-Sub topic 后缀
	pubsubTopicSuffix = "__pubsub__"
	// defaultMaxQueueSize 默认队列大小
	defaultMaxQueueSize = 1000
	// defaultProcessTimeout 默认处理超时秒数
	defaultProcessTimeout = 1800.0
	// defaultTeamID 默认团队标识，对齐 Python: self._team_id = self._config.team_id or "default"
	defaultTeamID = "default"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 MessageBus 满足 MessageBusInterface 接口
var _ MessageBusInterface = (*MessageBus)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageBusConfig 创建消息总线配置，设置默认值。
func NewMessageBusConfig(opts ...MessageBusConfigOption) *MessageBusConfig {
	cfg := &MessageBusConfig{
		MaxQueueSize:   defaultMaxQueueSize,
		ProcessTimeout: defaultProcessTimeout,
		TeamID:         defaultTeamID,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// WithMaxQueueSize 设置队列大小选项。
func WithMaxQueueSize(size int) MessageBusConfigOption {
	return func(c *MessageBusConfig) { c.MaxQueueSize = size }
}

// WithProcessTimeout 设置处理超时选项。
func WithProcessTimeout(timeout float64) MessageBusConfigOption {
	return func(c *MessageBusConfig) { c.ProcessTimeout = timeout }
}

// WithTeamID 设置团队标识选项。
func WithTeamID(teamID string) MessageBusConfigOption {
	return func(c *MessageBusConfig) { c.TeamID = teamID }
}

// NewMessageBus 创建消息总线实例。
//
// 对应 Python: MessageBus.__init__(config, runtime, message_router)
// Python 在初始化失败时 raise build_error(StatusCode.MESSAGE_QUEUE_INITIATION_ERROR, ...)，
// Go 通过返回 error 对齐此语义。当前 MessageQueueInMemory 构造不返回 error，
// 后续若支持则回填 StatusMessageQueueInitiationError。
func NewMessageBus(config MessageBusConfig, runtime *TeamRuntime) (*MessageBus, error) {
	sm := NewSubscriptionManager()
	router := NewMessageRouter(sm, runtime)

	return &MessageBus{
		config:              config,
		teamID:              config.TeamID,
		mq:                  message_queue.NewMessageQueueInMemory(config.MaxQueueSize, time.Duration(config.ProcessTimeout*float64(time.Second))),
		activeSubscriptions: make(map[string]message_queue.SubscriptionBase),
		subscriptionManager: sm,
		router:              router,
	}, nil
}

// Start 启动消息总线。
//
// 对应 Python: MessageBus.start()
func (mb *MessageBus) Start(ctx context.Context) error {
	if mb.running.Load() {
		logger.Warn(logComponent).
			Str("event_type", "MESSAGE_BUS_ALREADY_RUNNING").
			Str("team_id", mb.teamID).
			Msg("消息总线已在运行中")
		return nil
	}

	mb.mq.Start()
	mb.running.Store(true)

	logger.Info(logComponent).
		Str("event_type", "MESSAGE_BUS_STARTED").
		Str("team_id", mb.teamID).
		Msg("消息总线已启动")

	return nil
}

// Stop 停止消息总线。
//
// 对应 Python: MessageBus.stop()
func (mb *MessageBus) Stop(ctx context.Context) error {
	if !mb.running.Load() {
		return nil
	}

	// 停用所有活跃订阅
	mb.subscriptionLock.Lock()
	for topic, sub := range mb.activeSubscriptions {
		sub.Deactivate()
		delete(mb.activeSubscriptions, topic)
	}
	mb.subscriptionLock.Unlock()

	// 停止消息队列
	if err := mb.mq.Stop(ctx); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "MESSAGE_BUS_STOP_ERROR").
			Str("team_id", mb.teamID).
			Msg("消息总线停止失败")
		return exception.BuildError(
			exception.StatusMessageQueueInitiationError,
			exception.WithCause(err),
			exception.WithParam("type", "MessageQueueInMemory"),
			exception.WithParam("reason", fmt.Sprintf("[shutdown phase] %s", err.Error())),
		)
	}

	mb.running.Store(false)

	logger.Info(logComponent).
		Str("event_type", "MESSAGE_BUS_STOPPED").
		Str("team_id", mb.teamID).
		Msg("消息总线已停止")

	return nil
}

// CleanupSession 清理会话相关的订阅。
//
// 对应 Python: MessageBus.cleanup_session(session_id)
func (mb *MessageBus) CleanupSession(ctx context.Context, sessionID string) error {
	p2pTopic := mb.getP2PTopic(sessionID)
	pubsubTopic := mb.getPubsubTopic(sessionID)

	mb.subscriptionLock.Lock()
	defer mb.subscriptionLock.Unlock()

	for _, topic := range []string{p2pTopic, pubsubTopic} {
		if sub, ok := mb.activeSubscriptions[topic]; ok {
			sub.Deactivate()
			delete(mb.activeSubscriptions, topic)
			if err := mb.mq.Unsubscribe(ctx, topic); err != nil {
				logger.Warn(logComponent).Err(err).
					Str("event_type", "MESSAGE_BUS_CLEANUP_ERROR").
					Str("topic", topic).
					Str("session_id", sessionID).
					Msg("清理会话订阅失败")
			}
		}
	}

	logger.Info(logComponent).
		Str("event_type", "MESSAGE_BUS_SESSION_CLEANED").
		Str("session_id", sessionID).
		Msg("消息总会话清理完成")

	return nil
}

// Send P2P 发送消息到指定接收者，等待响应。
//
// 流程：构建信封 → 确保 P2P topic 订阅 → 发布 InvokeQueueMessage → 等待响应。
//
// 对应 Python: MessageBus.send(message, recipient, sender, session_id, timeout)
func (mb *MessageBus) Send(ctx context.Context, message any, recipient string, sender string, sessionID string, timeout float64) (any, error) {
	if !mb.running.Load() {
		return nil, exception.BuildError(exception.StatusMessageQueueInitiationError,
			exception.WithParam("reason", "消息总线未启动，无法发送 P2P 消息"),
		)
	}

	// 构建消息信封
	envelope := NewMessageEnvelope(
		uuid.New().String(),
		message,
		sender,
		WithRecipient(recipient),
		WithSessionID(sessionID),
	)

	// 确保 P2P topic 已订阅
	p2pTopic := mb.getP2PTopic(sessionID)
	if err := mb.ensureSubscription(ctx, p2pTopic); err != nil {
		return nil, exception.BuildError(exception.StatusMessageQueueTopicSubscriptionError,
			exception.WithCause(err),
			exception.WithParam("topic", p2pTopic),
			exception.WithParam("reason", fmt.Sprintf("确保 P2P 订阅失败: %s", err.Error())),
		)
	}

	// 构建 payload
	payload := mb.buildEnvelopePayload(envelope)

	// 发布同步消息，等待响应
	invokeMsg := message_queue.NewInvokeQueueMessage(payload)
	if err := mb.mq.Produce(ctx, p2pTopic, invokeMsg); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "P2P_SEND_ERROR").
			Str("sender", sender).
			Str("recipient", recipient).
			Str("message_id", envelope.MessageID).
			Msg("P2P 消息发送失败")
		return nil, exception.BuildError(
			exception.StatusMessageQueueTopicMessageProductionError,
			exception.WithCause(err),
			exception.WithParam("topic", p2pTopic),
			exception.WithParam("message", envelope.String()),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 等待响应，带超时
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout*float64(time.Second)))
		defer cancel()
	}

	result, err := invokeMsg.WaitResponse(ctx)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "P2P_RESPONSE_ERROR").
			Str("sender", sender).
			Str("recipient", recipient).
			Str("message_id", envelope.MessageID).
			Msg("P2P 消息响应等待失败")
		return nil, exception.BuildError(
			exception.StatusMessageQueueMessageProcessExecutionError,
			exception.WithCause(err),
			exception.WithParam("reason", err.Error()),
		)
	}

	logger.Info(logComponent).
		Str("event_type", "P2P_SEND_SUCCESS").
		Str("sender", sender).
		Str("recipient", recipient).
		Str("message_id", envelope.MessageID).
		Msg("P2P 消息发送并收到响应")

	return result, nil
}

// Publish Pub-Sub 发布消息到指定主题，发后即忘。
//
// 流程：构建信封 → 确保 Pub-Sub topic 订阅 → 发布 QueueMessage（火忘）。
//
// 对应 Python: MessageBus.publish(message, topic_id, sender, session_id)
func (mb *MessageBus) Publish(ctx context.Context, message any, topicID string, sender string, sessionID string) error {
	if !mb.running.Load() {
		return exception.BuildError(exception.StatusMessageQueueInitiationError,
			exception.WithParam("reason", "消息总线未启动，无法发布 Pub-Sub 消息"),
		)
	}

	// 构建消息信封
	envelope := NewMessageEnvelope(
		uuid.New().String(),
		message,
		sender,
		WithTopicID(topicID),
		WithSessionID(sessionID),
	)

	// 确保 Pub-Sub topic 已订阅
	pubsubTopic := mb.getPubsubTopic(sessionID)
	if err := mb.ensureSubscription(ctx, pubsubTopic); err != nil {
		return exception.BuildError(exception.StatusMessageQueueTopicSubscriptionError,
			exception.WithCause(err),
			exception.WithParam("topic", pubsubTopic),
			exception.WithParam("reason", fmt.Sprintf("确保 Pub-Sub 订阅失败: %s", err.Error())),
		)
	}

	// 构建 payload
	payload := mb.buildEnvelopePayload(envelope)

	// 发布火忘消息
	fireAndForgetMsg := message_queue.NewQueueMessage(payload)
	if err := mb.mq.Produce(ctx, pubsubTopic, fireAndForgetMsg); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "PUBSUB_PUBLISH_ERROR").
			Str("sender", sender).
			Str("topic_id", topicID).
			Str("message_id", envelope.MessageID).
			Msg("Pub-Sub 消息发布失败")
		return exception.BuildError(
			exception.StatusMessageQueueTopicMessageProductionError,
			exception.WithCause(err),
			exception.WithParam("topic", pubsubTopic),
			exception.WithParam("message", envelope.String()),
			exception.WithParam("reason", err.Error()),
		)
	}

	logger.Info(logComponent).
		Str("event_type", "PUBSUB_PUBLISH_SUCCESS").
		Str("sender", sender).
		Str("topic_id", topicID).
		Str("message_id", envelope.MessageID).
		Msg("Pub-Sub 消息发布成功")

	return nil
}

// AddSubscription 添加订阅关系。
//
// 对应 Python: MessageBus.add_subscription(agent_id, topic)
func (mb *MessageBus) AddSubscription(agentID, topic string) {
	mb.subscriptionManager.Subscribe(agentID, topic)
}

// RemoveSubscription 移除订阅关系。
//
// 对应 Python: MessageBus.remove_subscription(agent_id, topic)
func (mb *MessageBus) RemoveSubscription(agentID, topic string) {
	mb.subscriptionManager.Unsubscribe(agentID, topic)
}

// RemoveAllSubscriptions 移除 Agent 的所有订阅。
//
// 对应 Python: MessageBus.remove_all_subscriptions(agent_id)
func (mb *MessageBus) RemoveAllSubscriptions(agentID string) {
	mb.subscriptionManager.UnsubscribeAll(agentID)
}

// ListSubscriptions 列出订阅信息。
//
// 对应 Python: MessageBus.list_subscriptions(agent_id)
func (mb *MessageBus) ListSubscriptions(agentID string) any {
	return mb.subscriptionManager.ListSubscriptions(agentID)
}

// GetSubscriptionCount 获取总订阅数。
//
// 对应 Python: MessageBus.get_subscription_count()
func (mb *MessageBus) GetSubscriptionCount() int {
	return mb.subscriptionManager.GetSubscriptionCount()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// getP2PTopic 获取 P2P topic 名称。
//
// 格式对齐 Python：
//   - 有 sessionID: {teamID}_{sessionID}__p2p__
//   - 无 sessionID: {teamID}__p2p__
func (mb *MessageBus) getP2PTopic(sessionID string) string {
	if sessionID != "" {
		return mb.teamID + "_" + sessionID + p2pTopicSuffix
	}
	return mb.teamID + p2pTopicSuffix
}

// getPubsubTopic 获取 Pub-Sub topic 名称。
//
// 格式对齐 Python：
//   - 有 sessionID: {teamID}_{sessionID}__pubsub__
//   - 无 sessionID: {teamID}__pubsub__
func (mb *MessageBus) getPubsubTopic(sessionID string) string {
	if sessionID != "" {
		return mb.teamID + "_" + sessionID + pubsubTopicSuffix
	}
	return mb.teamID + pubsubTopicSuffix
}

// ensureSubscription 确保指定 topic 的订阅已激活（双检锁）。
//
// 快速路径：RLock 检查是否已订阅，避免大多数情况下的写锁竞争。
// 慢速路径：Lock 创建订阅，再次检查防止重复创建。
//
// 对应 Python: MessageBus._ensure_subscription(topic)
func (mb *MessageBus) ensureSubscription(ctx context.Context, topic string) error {
	// 快速路径：读锁检查是否已订阅
	mb.subscriptionLock.RLock()
	if sub, ok := mb.activeSubscriptions[topic]; ok && sub.IsActive() {
		mb.subscriptionLock.RUnlock()
		return nil
	}
	mb.subscriptionLock.RUnlock()

	// 慢速路径：写锁创建订阅
	mb.subscriptionLock.Lock()
	defer mb.subscriptionLock.Unlock()

	// 双检：再次检查是否已订阅（可能在获取写锁前被其他 goroutine 创建）
	if sub, ok := mb.activeSubscriptions[topic]; ok && sub.IsActive() {
		return nil
	}

	// 创建订阅
	sub, err := mb.mq.Subscribe(topic)
	if err != nil {
		// topic 已存在但不活跃，尝试取消后重新订阅
		if err == message_queue.ErrTopicAlreadySubscribed {
			_ = mb.mq.Unsubscribe(ctx, topic)
			sub, err = mb.mq.Subscribe(topic)
			if err != nil {
				return fmt.Errorf("重新订阅 topic %s 失败: %w", topic, err)
			}
		} else {
			return fmt.Errorf("订阅 topic %s 失败: %w", topic, err)
		}
	}

	// 设置消息处理回调，根据 topic 中的标记判断 P2P 或 Pub-Sub
	if containsP2PMarker(topic) {
		sub.SetMessageHandler(mb.handleP2PMessage)
	} else {
		sub.SetMessageHandler(mb.handlePubsubMessage)
	}
	sub.Activate()

	mb.activeSubscriptions[topic] = sub

	logger.Info(logComponent).
		Str("event_type", "MESSAGE_BUS_SUBSCRIPTION_ENSURED").
		Str("topic", topic).
		Msg("消息总线订阅已确保激活")

	return nil
}

// handleP2PMessage 处理 P2P 消息，提取信封并路由。
//
// 对应 Python: MessageBus._handle_p2p_message(payload)
// Python 在 ValueError/Exception 时 raise build_error(StatusCode.MESSAGE_QUEUE_MESSAGE_PROCESS_EXECUTION_ERROR, ...)，
// Go 通过返回 BuildError 对齐此语义。
func (mb *MessageBus) handleP2PMessage(ctx context.Context, payload map[string]any) (any, error) {
	envelope, err := mb.extractEnvelopeFromPayload(payload)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "P2P_HANDLE_ERROR").
			Msg("提取 P2P 消息信封失败")
		return nil, exception.BuildError(
			exception.StatusMessageQueueMessageProcessExecutionError,
			exception.WithCause(err),
			exception.WithParam("reason", fmt.Sprintf("Invalid P2P message payload: %s", err.Error())),
		)
	}

	return mb.router.RouteP2PMessage(ctx, envelope)
}

// handlePubsubMessage 处理 Pub-Sub 消息，提取信封并路由。
//
// 对应 Python: MessageBus._handle_pubsub_message(payload)
// Python 中所有异常仅记录日志不抛出（火忘语义），Go 对齐此行为：
// 即使信封提取失败或路由失败，也仅记录日志，返回 (nil, nil)。
func (mb *MessageBus) handlePubsubMessage(ctx context.Context, payload map[string]any) (any, error) {
	envelope, err := mb.extractEnvelopeFromPayload(payload)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "PUBSUB_HANDLE_ERROR").
			Msg("提取 Pub-Sub 消息信封失败")
		// 火忘语义：只记日志，吞掉错误
		return nil, nil
	}

	if err := mb.router.RoutePubsubMessage(ctx, envelope); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "PUBSUB_ROUTE_ERROR").
			Str("topic_id", envelope.TopicID).
			Str("message_id", envelope.MessageID).
			Msg("Pub-Sub 消息路由失败")
		// 火忘语义：只记日志，吞掉错误
		return nil, nil
	}
	return nil, nil
}

// extractEnvelopeFromPayload 从 payload 中提取消息信封。
//
// 对应 Python: MessageBus._extract_envelope(payload)
func (mb *MessageBus) extractEnvelopeFromPayload(payload map[string]any) (*MessageEnvelope, error) {
	envelopeAny, ok := payload["envelope"]
	if !ok {
		return nil, fmt.Errorf("payload 中缺少 envelope 字段")
	}
	envelope, ok := envelopeAny.(*MessageEnvelope)
	if !ok {
		return nil, fmt.Errorf("envelope 类型断言失败，期望 *MessageEnvelope，实际 %T", envelopeAny)
	}
	return envelope, nil
}

// buildEnvelopePayload 构建包含信封的 payload。
func (mb *MessageBus) buildEnvelopePayload(envelope *MessageEnvelope) map[string]any {
	return map[string]any{
		"envelope": envelope,
	}
}

// containsP2PMarker 判断 topic 是否包含 P2P 标记。
func containsP2PMarker(topic string) bool {
	return strings.Contains(topic, p2pTopicSuffix)
}
