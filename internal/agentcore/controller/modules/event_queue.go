package modules

import (
	"context"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EventQueue 事件队列，基于 MessageQueueInMemory 实现事件发布订阅。
//
// 对齐 Python: openjiuwen/core/controller/modules/event_queue.py (EventQueue)
// 为每个 agentID+sessionID 组合的 5 种 EventType 创建独立 topic，
// 订阅时绑定对应 EventHandler 方法，发布时按 EventType 路由到正确 handler。
type EventQueue struct {
	// config 配置
	config *config.ControllerConfig
	// queue 内存消息队列
	queue *message_queue.MessageQueueInMemory
	// eventHandler 事件处理器
	eventHandler EventHandler
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEventQueue 创建事件队列实例。
//
// 对齐 Python: EventQueue.__init__(config)
func NewEventQueue(cfg *config.ControllerConfig) *EventQueue {
	timeout := time.Duration(cfg.EventTimeout * float64(time.Second))
	q := message_queue.NewMessageQueueInMemory(cfg.EventQueueSize, timeout)
	return &EventQueue{
		config: cfg,
		queue:  q,
	}
}

// SetConfig 更新配置。
//
// 对齐 Python: EventQueue.set_config(config)
func (eq *EventQueue) SetConfig(cfg *config.ControllerConfig) {
	eq.config = cfg
}

// SetEventHandler 设置事件处理器。
//
// 对齐 Python: EventQueue.set_event_handler(event_handler)
func (eq *EventQueue) SetEventHandler(handler EventHandler) {
	eq.eventHandler = handler
}

// Start 启动事件队列。
//
// 对齐 Python: EventQueue.start()
func (eq *EventQueue) Start() {
	eq.queue.Start()
	logger.Info(logComponent).
		Str("event_type", "event_queue_started").
		Msg("事件队列已启动")
}

// Stop 停止事件队列。
//
// 对齐 Python: EventQueue.stop()
func (eq *EventQueue) Stop(ctx context.Context) error {
	err := eq.queue.Stop(ctx)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Err(err).
			Msg("事件队列停止失败")
		return exception.NewBaseError(exception.StatusAgentControllerEventQueueError,
			exception.WithMsg(fmt.Sprintf("事件队列停止失败: %v", err)),
			exception.WithCause(err))
	}
	logger.Info(logComponent).
		Str("event_type", "event_queue_stopped").
		Msg("事件队列已停止")
	return nil
}

// Subscribe 为指定 agentID+sessionID 的所有事件类型创建订阅。
//
// 对齐 Python: EventQueue.subscribe(agent_id, session_id)
// 为 5 种 EventType 创建 topic，每个订阅绑定对应 EventHandler 方法并激活。
func (eq *EventQueue) Subscribe(ctx context.Context, agentID, sessionID string) error {
	if eq.eventHandler == nil {
		err := fmt.Errorf("事件处理器未设置，无法订阅: agent_id=%s, session_id=%s", agentID, sessionID)
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("agent_id", agentID).
			Str("session_id", sessionID).
			Msg("事件处理器未设置，无法订阅")
		return exception.NewBaseError(exception.StatusAgentControllerEventQueueError,
			exception.WithMsg(err.Error()))
	}

	timeout := time.Duration(eq.config.EventTimeout * float64(time.Second))
	eventTypes := []schema.EventType{
		schema.EventInput,
		schema.EventTaskInteraction,
		schema.EventTaskCompletion,
		schema.EventTaskFailed,
		schema.EventFollowUp,
	}

	for _, eventType := range eventTypes {
		topic := eq.buildTopic(agentID, sessionID, eventType)
		sub := eq.queue.Subscribe(topic)

		// 捕获当前 eventType 和 handler，避免闭包引用循环变量
		handler := eq.makeEventHandler(eventType)
		sub.SetMessageHandler(handler)
		sub.Activate(timeout)

		logger.Info(logComponent).
			Str("event_type", "event_queue_subscribed").
			Str("topic", topic).
			Str("agent_id", agentID).
			Str("session_id", sessionID).
			Str("event_type_name", string(eventType)).
			Msg("事件订阅已创建并激活")
	}

	return nil
}

// Unsubscribe 取消指定 agentID+sessionID 的所有事件订阅。
//
// 对齐 Python: EventQueue.unsubscribe(agent_id, session_id)
func (eq *EventQueue) Unsubscribe(ctx context.Context, agentID, sessionID string) error {
	eventTypes := []schema.EventType{
		schema.EventInput,
		schema.EventTaskInteraction,
		schema.EventTaskCompletion,
		schema.EventTaskFailed,
		schema.EventFollowUp,
	}

	for _, eventType := range eventTypes {
		topic := eq.buildTopic(agentID, sessionID, eventType)
		err := eq.queue.Unsubscribe(ctx, topic)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("topic", topic).
				Str("agent_id", agentID).
				Str("session_id", sessionID).
				Err(err).
				Msg("取消事件订阅失败")
			return exception.NewBaseError(exception.StatusAgentControllerEventQueueError,
				exception.WithMsg(fmt.Sprintf("取消事件订阅失败: topic=%s, err=%v", topic, err)),
				exception.WithCause(err))
		}
		logger.Info(logComponent).
			Str("event_type", "event_queue_unsubscribed").
			Str("topic", topic).
			Str("agent_id", agentID).
			Str("session_id", sessionID).
			Msg("事件订阅已取消")
	}

	return nil
}

// PublishEvent 同步发布事件，等待处理完成。
//
// 对齐 Python: EventQueue.publish_event(agent_id, session, event)
func (eq *EventQueue) PublishEvent(ctx context.Context, agentID string, sess sessioninterfaces.SessionFacade, event schema.Event) error {
	eventType := event.GetEventType()
	sessionID := sess.GetSessionID()
	topic := eq.buildTopic(agentID, sessionID, eventType)

	payload := map[string]any{
		"event":   event,
		"session": sess,
	}

	invoke := message_queue.NewInvokeQueueMessage(payload)
	err := eq.queue.Produce(ctx, topic, message_queue.NewQueueMessage(payload), invoke)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("topic", topic).
			Str("agent_id", agentID).
			Str("session_id", sessionID).
			Str("event_type_name", string(eventType)).
			Err(err).
			Msg("同步发布事件失败")
		return exception.NewBaseError(exception.StatusAgentControllerEventQueueError,
			exception.WithMsg(fmt.Sprintf("同步发布事件失败: topic=%s, err=%v", topic, err)),
			exception.WithCause(err))
	}

	// 等待处理完成
	_, err = invoke.WaitResponse(ctx)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("topic", topic).
			Str("agent_id", agentID).
			Str("session_id", sessionID).
			Err(err).
			Msg("同步事件处理失败")
		return exception.NewBaseError(exception.StatusAgentControllerEventQueueError,
			exception.WithMsg(fmt.Sprintf("同步事件处理失败: topic=%s, err=%v", topic, err)),
			exception.WithCause(err))
	}

	logger.Info(logComponent).
		Str("event_type", "event_published_sync").
		Str("topic", topic).
		Str("agent_id", agentID).
		Str("session_id", sessionID).
		Str("event_type_name", string(eventType)).
		Msg("同步事件发布并处理完成")
	return nil
}

// PublishEventAsync 火忘发布事件，不等待处理完成。
//
// 对齐 Python: EventQueue.publish_event_async(agent_id, session, event)
func (eq *EventQueue) PublishEventAsync(ctx context.Context, agentID string, sess sessioninterfaces.SessionFacade, event schema.Event) error {
	eventType := event.GetEventType()
	sessionID := sess.GetSessionID()
	topic := eq.buildTopic(agentID, sessionID, eventType)

	payload := map[string]any{
		"event":   event,
		"session": sess,
	}

	err := eq.queue.Produce(ctx, topic, message_queue.NewQueueMessage(payload), nil)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("topic", topic).
			Str("agent_id", agentID).
			Str("session_id", sessionID).
			Str("event_type_name", string(eventType)).
			Err(err).
			Msg("火忘发布事件失败")
		return exception.NewBaseError(exception.StatusAgentControllerEventQueueError,
			exception.WithMsg(fmt.Sprintf("火忘发布事件失败: topic=%s, err=%v", topic, err)),
			exception.WithCause(err))
	}

	logger.Info(logComponent).
		Str("event_type", "event_published_async").
		Str("topic", topic).
		Str("agent_id", agentID).
		Str("session_id", sessionID).
		Str("event_type_name", string(eventType)).
		Msg("火忘事件已发布")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildTopic 构建 topic 名称，格式为 "{agentID}_{sessionID}_{eventType}"。
//
// 对齐 Python: EventQueue._build_topic(agent_id, session_id, event_type)
func (eq *EventQueue) buildTopic(agentID, sessionID string, eventType schema.EventType) string {
	return fmt.Sprintf("%s_%s_%s", agentID, sessionID, string(eventType))
}

// makeEventHandler 根据事件类型创建对应的消息处理回调。
//
// 对齐 Python: EventQueue._subscribe_event 中的 event_handle_wrapper
func (eq *EventQueue) makeEventHandler(eventType schema.EventType) func(ctx context.Context, payload map[string]any) (any, error) {
	handler := eq.eventHandler
	return func(ctx context.Context, payload map[string]any) (any, error) {
		event, ok := payload["event"].(schema.Event)
		if !ok {
			err := fmt.Errorf("payload 中缺少 event 字段或类型不正确")
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("event_type_name", string(eventType)).
				Msg(err.Error())
			return nil, err
		}
		sess, ok := payload["session"].(sessioninterfaces.SessionFacade)
		if !ok {
			err := fmt.Errorf("payload 中缺少 session 字段或类型不正确")
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("event_type_name", string(eventType)).
				Msg(err.Error())
			return nil, err
		}

		input := &EventHandlerInput{Event: event, Session: sess}

		switch eventType {
		case schema.EventInput:
			return handler.HandleInput(ctx, input)
		case schema.EventTaskInteraction:
			return handler.HandleTaskInteraction(ctx, input)
		case schema.EventTaskCompletion:
			return handler.HandleTaskCompletion(ctx, input)
		case schema.EventTaskFailed:
			return handler.HandleTaskFailed(ctx, input)
		case schema.EventFollowUp:
			return handler.HandleFollowUp(ctx, input)
		default:
			err := fmt.Errorf("未知事件类型: %s", eventType)
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("event_type_name", string(eventType)).
				Msg("未知事件类型")
			return nil, err
		}
	}
}
