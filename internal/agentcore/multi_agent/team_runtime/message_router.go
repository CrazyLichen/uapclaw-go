package team_runtime

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageRouter 消息路由器，将 P2P 和 Pub-Sub 消息路由到目标 Agent。
//
// P2P 模式：触发 AgentP2PReceived 回调 → 构建 Agent 会话 → runner.RunAgent → 返回响应。
// Pub-Sub 模式：查询订阅者 → 并发触发各订阅者的 AgentPubsubReceived 回调 → 并发执行各 Agent。
//
// 对应 Python: MessageRouter (openjiuwen/core/multi_agent/team_runtime/message_router.py)
type MessageRouter struct {
	// subscriptionManager 订阅管理器
	subscriptionManager *SubscriptionManager
	// runtime 团队运行时引用
	runtime *TeamRuntime
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageRouter 创建消息路由器实例。
//
// 对应 Python: MessageRouter.__init__(subscription_manager, runtime)
func NewMessageRouter(sm *SubscriptionManager, runtime *TeamRuntime) *MessageRouter {
	return &MessageRouter{
		subscriptionManager: sm,
		runtime:             runtime,
	}
}

// RouteP2PMessage 路由 P2P 消息到目标 Agent。
//
// 流程：触发 AgentP2PReceived 回调 → 构建 Agent 会话 → runner.RunAgent → 返回响应。
//
// 对应 Python: MessageRouter.route_p2p_message(envelope)
func (r *MessageRouter) RouteP2PMessage(ctx context.Context, envelope *MessageEnvelope) (map[string]any, error) {
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

	// 构建 Agent 会话（对齐 Python: session if session is not None else session_id）
	agentSession := r.buildAgentSession(envelope.SessionID, envelope.Recipient)
	var sessionRef runner.SessionRef
	if agentSession != nil {
		sessionRef = runner.BySession(agentSession)
	} else if envelope.SessionID != "" {
		sessionRef = runner.BySessionID(envelope.SessionID)
	}

	// 执行目标 Agent（对齐 Python: Runner.run_agent(agent, inputs, session)）
	inputs := toInputsMap(envelope.Message)
	result, err := runner.RunAgent(ctx, runner.ByAgentID(envelope.Recipient), inputs, sessionRef, nil, nil)
	if err != nil {
		// 对齐 Python: raise build_error(StatusCode.RUNNER_RUN_AGENT_ERROR, agent=..., reason=...)
		return nil, exception.BuildError(
			exception.StatusRunnerRunAgentError,
			exception.WithCause(err),
			exception.WithParam("agent", envelope.Recipient),
			exception.WithParam("reason", err.Error()),
		)
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
// 使用 sync.WaitGroup 等待所有订阅者完成，对齐 Python asyncio.gather(return_exceptions=True)。
// 通过 context 取消传播，当 ctx 被取消时 goroutine 快速退出。
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

	// 并发执行各订阅者，WaitGroup 等待所有完成
	// 对齐 Python: asyncio.gather(*tasks, return_exceptions=True)
	var wg sync.WaitGroup
	for _, agentID := range subscribers {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()

			// context 取消传播：当 ctx 被取消时快速退出 goroutine
			select {
			case <-ctx.Done():
				logger.Warn(logComponent).
					Str("event_type", "PUBSUB_SUBSCRIBER_CANCELLED").
					Str("agent_id", id).
					Str("topic_id", envelope.TopicID).
					Msg("Pub-Sub 订阅者因 context 取消而退出")
				return
			default:
			}

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
			var sessionRef runner.SessionRef
			if agentSession != nil {
				sessionRef = runner.BySession(agentSession)
			} else if envelope.SessionID != "" {
				sessionRef = runner.BySessionID(envelope.SessionID)
			}
			inputs := toInputsMap(envelope.Message)
			_, err := runner.RunAgent(ctx, runner.ByAgentID(id), inputs, sessionRef, nil, nil)
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
	wg.Wait()

	logger.Info(logComponent).
		Str("event_type", "PUBSUB_ROUTE_COMPLETE").
		Str("topic_id", envelope.TopicID).
		Int("subscriber_count", len(subscribers)).
		Msg("Pub-Sub 消息路由完成")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildAgentSession 构建 Agent 子会话，复用已有的 TeamSession 创建子会话。
//
// 流程对齐 Python MessageRouter._build_agent_session:
//  1. 获取 TeamSession → 2. 获取 AgentCard → 3. 创建 Agent 子会话
//
// 对应 Python: MessageRouter._build_agent_session(session_id, agent_id)
func (r *MessageRouter) buildAgentSession(sessionID, agentID string) *session.Session {
	if sessionID == "" {
		return nil
	}
	teamSession := r.runtime.GetTeamSession(sessionID)
	if teamSession == nil {
		return nil
	}
	card, err := r.runtime.GetAgentCard(agentID)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("event_type", "BUILD_AGENT_SESSION_NO_CARD").
			Str("agent_id", agentID).
			Msg("构建 Agent 会话时获取 AgentCard 失败")
		return nil
	}
	return teamSession.CreateAgentSession(card, agentID, true)
}

// toInputsMap 将消息内容转换为 map[string]any 类型以匹配 runner.RunAgent 签名。
//
// 对齐 Python: Runner.run_agent(agent=..., inputs=envelope.message, ...) 中 inputs 的类型转换。
func toInputsMap(message any) map[string]any {
	if m, ok := message.(map[string]any); ok {
		return m
	}
	return map[string]any{"message": message}
}
