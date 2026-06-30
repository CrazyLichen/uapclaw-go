package team_runtime

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentExecutor Agent 执行器接口，解决 team_runtime → runner 循环依赖。
//
// Runner 实现此接口后注入 MessageRouter，使路由器能调用 Agent 而不直接依赖 runner 包。
type AgentExecutor interface {
	// RunAgent 执行指定 Agent 并返回结果。
	RunAgent(ctx context.Context, agentID string, inputs any, sess any) (any, error)
}

// MessageRouter 消息路由器，将 P2P 和 Pub-Sub 消息路由到目标 Agent。
//
// P2P 模式：触发 AgentP2PReceived 回调 → 构建 Agent 会话 → 执行目标 Agent → 返回响应。
// Pub-Sub 模式：查询订阅者 → 并发触发各订阅者的 AgentPubsubReceived 回调 → 并发执行各 Agent。
//
// 对应 Python: MessageRouter (openjiuwen/core/multi_agent/team_runtime/message_router.py)
type MessageRouter struct {
	// subscriptionManager 订阅管理器
	subscriptionManager *SubscriptionManager
	// runtime 团队运行时引用
	runtime *TeamRuntime
	// agentExecutor Agent 执行器，避免循环依赖
	agentExecutor AgentExecutor
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageRouter 创建消息路由器实例。
//
// 对应 Python: MessageRouter.__init__(subscription_manager, runtime)
func NewMessageRouter(sm *SubscriptionManager, runtime *TeamRuntime, executor AgentExecutor) *MessageRouter {
	return &MessageRouter{
		subscriptionManager: sm,
		runtime:             runtime,
		agentExecutor:       executor,
	}
}

// RouteP2PMessage 路由 P2P 消息到目标 Agent。
//
// 流程：触发 AgentP2PReceived 回调 → 构建 Agent 会话 → agentExecutor.RunAgent → 返回响应。
//
// 对应 Python: MessageRouter.route_p2p_message(envelope)
func (r *MessageRouter) RouteP2PMessage(ctx context.Context, envelope *MessageEnvelope) (any, error) {
	// 触发 AgentP2PReceived 回调
	callback.GetCallbackFramework().TriggerAgentTeam(ctx, &callback.AgentTeamEventData{
		Event:   callback.AgentP2PReceived,
		AgentID: envelope.Recipient,
		Message: envelope.Message,
		Extra: map[string]any{
			"sender":     envelope.Sender,
			"session_id": envelope.SessionID,
			"message_id": envelope.MessageID,
		},
	})

	// 构建 Agent 会话
	agentSession := r.buildAgentSession(envelope.SessionID, envelope.Recipient)

	// 执行目标 Agent
	result, err := r.agentExecutor.RunAgent(ctx, envelope.Recipient, envelope.Message, agentSession)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "P2P_ROUTE_ERROR").
			Str("sender", envelope.Sender).
			Str("recipient", envelope.Recipient).
			Str("message_id", envelope.MessageID).
			Msg("P2P 消息路由执行失败")
		return nil, err
	}

	logger.Info(logComponent).
		Str("event_type", "P2P_ROUTE_SUCCESS").
		Str("sender", envelope.Sender).
		Str("recipient", envelope.Recipient).
		Str("message_id", envelope.MessageID).
		Msg("P2P 消息路由成功")

	return result, nil
}

// RoutePubsubMessage 路由 Pub-Sub 消息到所有订阅者 Agent。
//
// 流程：查询订阅者 → 并发触发 AgentPubsubReceived 回调 → 并发执行各 Agent。
// 单个订阅者失败仅记录日志，不影响其他订阅者。
//
// 对应 Python: MessageRouter.route_pubsub_message(envelope)
func (r *MessageRouter) RoutePubsubMessage(ctx context.Context, envelope *MessageEnvelope) error {
	subscribers := r.subscriptionManager.GetSubscribers(envelope.TopicID)
	if len(subscribers) == 0 {
		logger.Warn(logComponent).
			Str("event_type", "PUBSUB_NO_SUBSCRIBERS").
			Str("topic_id", envelope.TopicID).
			Str("sender", envelope.Sender).
			Msg("Pub-Sub 消息无订阅者")
		return nil
	}

	logger.Info(logComponent).
		Str("event_type", "PUBSUB_ROUTE_START").
		Str("topic_id", envelope.TopicID).
		Str("sender", envelope.Sender).
		Int("subscriber_count", len(subscribers)).
		Msg("开始路由 Pub-Sub 消息到订阅者")

	// 并发执行各订阅者
	for _, agentID := range subscribers {
		go func(id string) {
			// 触发 AgentPubsubReceived 回调
			callback.GetCallbackFramework().TriggerAgentTeam(ctx, &callback.AgentTeamEventData{
				Event:   callback.AgentPubsubReceived,
				AgentID: id,
				Message: envelope.Message,
				Extra: map[string]any{
					"sender":     envelope.Sender,
					"topic_id":   envelope.TopicID,
					"session_id": envelope.SessionID,
					"message_id": envelope.MessageID,
				},
			})

			agentSession := r.buildAgentSession(envelope.SessionID, id)
			_, err := r.agentExecutor.RunAgent(ctx, id, envelope.Message, agentSession)
			if err != nil {
				logger.Error(logComponent).Err(err).
					Str("event_type", "PUBSUB_SUBSCRIBER_ERROR").
					Str("agent_id", id).
					Str("topic_id", envelope.TopicID).
					Str("message_id", envelope.MessageID).
					Msg("Pub-Sub 订阅者执行失败")
				return
			}

			logger.Debug(logComponent).
				Str("event_type", "PUBSUB_SUBSCRIBER_SUCCESS").
				Str("agent_id", id).
				Str("topic_id", envelope.TopicID).
				Msg("Pub-Sub 订阅者执行成功")
		}(agentID)
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildAgentSession 构建 Agent 会话，复用已有的 TeamSession 或创建新的。
//
// 对应 Python: MessageRouter._build_agent_session(session_id, agent_id)
func (r *MessageRouter) buildAgentSession(sessionID, agentID string) any {
	if sessionID != "" {
		if sess := r.runtime.GetTeamSession(sessionID); sess != nil {
			return sess
		}
	}
	// 无有效会话，返回 nil
	return nil
}
