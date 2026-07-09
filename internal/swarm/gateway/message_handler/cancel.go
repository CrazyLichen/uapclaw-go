package message_handler

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CancelAgentWorkForSession 取消指定会话的所有流式任务并发送中断请求到 AgentServer
//
// 对齐 Python _cancel_agent_work_for_session (L381-L528)
func (mh *MessageHandler) CancelAgentWorkForSession(ctx context.Context, sessionID string, intent string) {
	if sessionID == "" {
		return
	}

	// 1. 收集该 session 关联的流式任务
	requestIDs := mh.collectStreamTasksForSession(sessionID)

	// 2. 构造 cancel 请求 → _send_interrupt_to_agent（对齐 Python）
	if mh.agentClient != nil && mh.agentClient.IsConnected() && len(requestIDs) > 0 {
		for _, reqID := range requestIDs {
			mh.sendInterruptToAgent(ctx, reqID, sessionID, intent)
		}
	}

	// 3. 取消流式任务
	for _, reqID := range requestIDs {
		mh.cancelStreamTask(reqID)
	}

	// 4. 发送 interrupt_result 通知
	mh.sendInterruptResultNotification(sessionID, intent)
}

// SendProcessingStatus 发送 processing_status 事件消息
//
// 对齐 Python _send_processing_status
func (mh *MessageHandler) SendProcessingStatus(sessionID string, isProcessing bool) {
	msg := schema.NewEventMessage("", sessionID, schema.EventTypeChatProcessingStatus,
		map[string]any{
			"is_processing": isProcessing,
			"session_id":    sessionID,
		},
	)
	mh.enqueueOutbound(msg)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectStreamTasksForSession 收集指定 session 的所有流式任务 requestID
func (mh *MessageHandler) collectStreamTasksForSession(sessionID string) []string {
	mh.streamMu.RLock()
	defer mh.streamMu.RUnlock()

	var requestIDs []string
	for reqID, sid := range mh.streamSessions {
		if sid == sessionID {
			requestIDs = append(requestIDs, reqID)
		}
	}
	return requestIDs
}

// cancelStreamTask 取消单个流式任务
func (mh *MessageHandler) cancelStreamTask(requestID string) {
	mh.streamMu.Lock()
	cancel, exists := mh.streamTasks[requestID]
	if exists {
		delete(mh.streamTasks, requestID)
		delete(mh.streamSessions, requestID)
		delete(mh.streamMetadata, requestID)
		delete(mh.streamModes, requestID)
	}
	mh.streamMu.Unlock()

	if exists && cancel != nil {
		cancel()
		logger.Debug(logComponent).
			Str("event_type", "stream_task_cancelled").
			Str("request_id", requestID).
			Msg("流式任务已取消")
	}
}

// sendInterruptToAgent 发送中断请求到 AgentServer，等响应后丢弃。
//
// 对齐 Python: _send_interrupt_to_agent — 调 send_request 等响应后丢弃。
func (mh *MessageHandler) sendInterruptToAgent(ctx context.Context, requestID, sessionID, intent string) {
	// 构造 chat.cancel 消息
	msg := schema.NewReqMessage("", sessionID, schema.ReqMethodChatCancel,
		nil,
		schema.WithSessionID(sessionID),
	)

	envelope := e2a.MessageToE2AOrFallback(msg)
	envelope.IsStream = false

	resp, err := mh.agentClient.SendRequest(ctx, envelope)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "cancel_send_error").
			Err(err).
			Str("request_id", requestID).
			Msg("AgentServer 中断请求失败(忽略)")
		return
	}
	logger.Info(logComponent).
		Str("event_type", "cancel_response_discarded").
		Str("request_id", resp.RequestID).
		Bool("ok", resp.OK).
		Msg("AgentServer 中断响应(已丢弃)")
}

// sendInterruptResultNotification 发送 interrupt_result 事件通知
func (mh *MessageHandler) sendInterruptResultNotification(sessionID, intent string) {
	msg := schema.NewEventMessage("", sessionID, schema.EventTypeChatInterruptResult,
		map[string]any{
			"intent":     intent,
			"session_id": sessionID,
		},
	)
	mh.enqueueOutbound(msg)
}

// sendStreamCancelledNotification 发送流式取消通知
func (mh *MessageHandler) sendStreamCancelledNotification(sessionID string) {
	mh.SendProcessingStatus(sessionID, false)
}

// publishStreamCancelledFinal 发布流式取消的最终消息
func (mh *MessageHandler) publishStreamCancelledFinal(sessionID string) {
	msg := schema.NewEventMessage("", sessionID, schema.EventTypeChatFinal,
		map[string]any{
			"content":      "",
			"is_cancelled": true,
			"session_id":   sessionID,
		},
	)
	mh.enqueueOutbound(msg)
}
