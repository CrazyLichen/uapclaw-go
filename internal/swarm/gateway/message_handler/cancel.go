package message_handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CancelAgentWorkForSession 取消指定会话的所有流式任务并发送中断请求到 AgentServer。
//
// 对齐 Python _cancel_agent_work_for_session (L381-L528)：
//  1. 清除 evolution 状态
//  2. 收集该 session 关联的流式任务
//  3. 构造 cancel 请求（注入 mode + trusted_dirs）
//  4. 发送中断请求到 AgentServer 并等待响应
//  5. 取消 gateway 侧流式任务
//  6. 根据响应决定通知方式
func (mh *MessageHandler) CancelAgentWorkForSession(ctx context.Context, msg *schema.Message, oldSessionID string, publishInterruptResult bool) {
	if oldSessionID == "" {
		return
	}

	// 1. 清除 evolution 状态
	mh.clearSessionEvolutionStates(oldSessionID)

	// 2. 收集该 session 关联的流式任务
	requestIDs := mh.collectStreamTasksForSession(oldSessionID)

	// 3. 构造 cancel 请求并注入 mode + trusted_dirs
	cancelMsg := mh.buildCancelMessage(msg, oldSessionID)

	// 4. 发送中断请求到 AgentServer，等待响应
	// 对齐 Python: 即使网关侧已无活跃流式拉取任务，也必须通知 AgentServer
	// 否则仅断开 CLI WebSocket 无法停止已派发的工作
	var resp *schema.AgentResponse
	var respErr error
	if mh.agentClient != nil && mh.agentClient.IsConnected() {
		cancelEnv := e2a.MessageToE2AOrFallback(cancelMsg)
		cancelEnv.IsStream = false
		resp, respErr = mh.agentClient.SendRequest(ctx, cancelEnv)
		if respErr != nil {
			logger.Warn(logComponent).
				Str("event_type", "cancel_send_error").
				Err(respErr).
				Str("session_id", oldSessionID).
				Msg("AgentServer 中断请求失败")

			// 对齐 Python: SendRequest 异常时发送 success=false 并返回
			if publishInterruptResult {
				hasActiveTask := len(requestIDs) > 0
				mh.sendInterruptResultNotification(
					msg.ID, msg.ChannelID, oldSessionID,
					"cancel", fmt.Sprintf("任务终止失败: %s", respErr.Error()),
					false, hasActiveTask,
				)
			}
			return
		}
	}

	// 5. 取消 gateway 侧流式任务
	for _, reqID := range requestIDs {
		mh.cancelStreamTask(reqID)
	}

	// 6. 根据响应决定通知方式
	if resp != nil && resp.Payload != nil {
		eventType, _ := resp.Payload["event_type"].(string)
		if eventType == string(schema.EventTypeChatInterruptResult) && publishInterruptResult {
			// 将 AgentServer 响应转为消息并发布
			outMsg := ResponseToMessage(resp, oldSessionID, msg.Metadata)
			mh.PublishRobotMessages(outMsg)
			// 发送已取消的工具结果
			mh.sendCancelledToolResults(msg.ChannelID, oldSessionID, resp.Payload, msg.Metadata)
		} else if !publishInterruptResult {
			// 静默模式，仅记录日志
			logger.Debug(logComponent).
				Str("event_type", "cancel_silent").
				Str("session_id", oldSessionID).
				Msg("取消完成，静默模式不发布 interrupt_result")
		} else {
			// 非预期响应，发送失败通知
			// 对齐 Python: 从 payload 中提取 error 或 message 字段
			errorMsg := "任务终止失败"
			if errFromPayload, ok := resp.Payload["error"]; ok {
				if s, isStr := errFromPayload.(string); isStr && s != "" {
					errorMsg = s
				}
			}
			if errorMsg == "任务终止失败" {
				if msgFromPayload, ok := resp.Payload["message"]; ok {
					if s, isStr := msgFromPayload.(string); isStr && s != "" {
						errorMsg = s
					}
				}
			}
			mh.sendInterruptResultNotification(msg.ID, msg.ChannelID, oldSessionID, "cancel", errorMsg, false, false)
		}
	} else if publishInterruptResult {
		// 无响应或响应无 payload，发送成功通知
		hasActiveTask := len(requestIDs) > 0
		mh.sendInterruptResultNotification(msg.ID, msg.ChannelID, oldSessionID, "cancel", "任务已取消", true, hasActiveTask)
	}
}

// SendProcessingStatus 发送 processing_status 事件消息（向后兼容包装）。
//
// 对齐 Python _send_processing_status，保留旧签名供过渡期使用。
func (mh *MessageHandler) SendProcessingStatus(sessionID string, isProcessing bool) {
	mh.sendProcessingStatus("", sessionID, "", isProcessing)
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
	entry, exists := mh.streamTasks[requestID]
	if exists {
		delete(mh.streamTasks, requestID)
		delete(mh.streamSessions, requestID)
		delete(mh.streamMetadata, requestID)
		delete(mh.streamModes, requestID)
	}
	mh.streamMu.Unlock()

	if exists && entry != nil && entry.cancel != nil {
		entry.cancel()
		// 对齐 Python: await asyncio.gather(*tasks) — 等待 goroutine 完全退出
		entry.wg.Wait()
		logger.Debug(logComponent).
			Str("event_type", "stream_task_cancelled").
			Str("request_id", requestID).
			Msg("流式任务已取消并等待退出")
	}
}

// buildCancelMessage 构造 chat.cancel 请求消息，注入 mode 和 trusted_dirs。
//
// 对齐 Python _cancel_agent_work_for_session 中构造 cancel 请求的逻辑：
// 从 msg.Params 或 channelStates 中提取 mode 和 trusted_dirs 注入 params。
func (mh *MessageHandler) buildCancelMessage(msg *schema.Message, sessionID string) *schema.Message {
	// 基础参数
	params := map[string]any{
		"session_id": sessionID,
	}

	// 从 msg.Params 中提取 mode
	if len(msg.Params) > 0 {
		var msgParams map[string]any
		if err := json.Unmarshal(msg.Params, &msgParams); err == nil {
			if mode, ok := msgParams["mode"]; ok {
				params["mode"] = mode
			}
			if trustedDirs, ok := msgParams["trusted_dirs"]; ok {
				params["trusted_dirs"] = trustedDirs
			}
		}
	}

	// 如果 params 中没有 mode，从 channelStates 补充
	if _, hasMode := params["mode"]; !hasMode {
		state := mh.GetOrCreateChannelState(msg)
		params["mode"] = ChannelModeString(state.Mode)
	}

	paramsJSON, _ := json.Marshal(params)

	return &schema.Message{
		ID:        msg.ID,
		Type:      schema.MessageTypeReq,
		ChannelID: msg.ChannelID,
		SessionID: sessionID,
		Params:    json.RawMessage(paramsJSON),
		Timestamp: schema.NowTimestamp(),
		OK:        true,
		ReqMethod: schema.ReqMethodChatCancel,
		IsStream:  false,
		Metadata:  msg.Metadata,
		Provider:  msg.Provider,
		ChatID:    msg.ChatID,
		UserID:    msg.UserID,
		BotID:     msg.BotID,
	}
}

// sendInterruptToAgent 发送中断请求到 AgentServer，等响应后丢弃。
//
// 对齐 Python _send_interrupt_to_agent (L2654-2691)：
// 从 msg 中提取 mode/trusted_dirs 注入 cancel 请求参数。
func (mh *MessageHandler) sendInterruptToAgent(ctx context.Context, msg *schema.Message, intent string) {
	// 构造 chat.cancel 消息，注入 mode + trusted_dirs
	cancelMsg := mh.buildCancelMessage(msg, msg.SessionID)

	// 在 params 中注入 intent
	if len(cancelMsg.Params) > 0 {
		var params map[string]any
		if err := json.Unmarshal(cancelMsg.Params, &params); err == nil {
			params["intent"] = intent
			if updated, err := json.Marshal(params); err == nil {
				cancelMsg.Params = json.RawMessage(updated)
			}
		}
	}

	envelope := e2a.MessageToE2AOrFallback(cancelMsg)
	envelope.IsStream = false

	resp, err := mh.agentClient.SendRequest(ctx, envelope)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "cancel_send_error").
			Err(err).
			Str("session_id", msg.SessionID).
			Msg("AgentServer 中断请求失败(忽略)")
		return
	}
	logger.Info(logComponent).
		Str("event_type", "cancel_response_discarded").
		Str("request_id", resp.RequestID).
		Bool("ok", resp.OK).
		Msg("AgentServer 中断响应(已丢弃)")
}

// sendInterruptToAgentWithEnvelope 使用预构建的 E2A 信封发送中断请求。
// 对齐 Python: _send_interrupt_to_agent(env_interrupt) — 接收已包含 mode 注入的信封
func (mh *MessageHandler) sendInterruptToAgentWithEnvelope(ctx context.Context, envelope *e2a.E2AEnvelope, intent string) {
	// 在 params 中注入 intent
	if envelope.Params != nil {
		envelope.Params["intent"] = intent
	}

	envelope.IsStream = false

	resp, err := mh.agentClient.SendRequest(ctx, envelope)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "cancel_send_error").
			Err(err).
			Str("session_id", envelope.SessionID).
			Msg("AgentServer 中断请求失败(忽略)")
		return
	}
	logger.Info(logComponent).
		Str("event_type", "cancel_response_discarded").
		Str("request_id", resp.RequestID).
		Bool("ok", resp.OK).
		Msg("AgentServer 中断响应(已丢弃)")
}

// sendInterruptResultNotification 发送 interrupt_result 事件通知。
//
// 对齐 Python _send_interrupt_result_notification (L2693-L2748)：
// 根据 intent 和 hasActiveTask 选择成功/失败消息模板，
// 构造 CHAT_INTERRUPT_RESULT 事件发送到客户端。
func (mh *MessageHandler) sendInterruptResultNotification(requestID, channelID, sessionID, intent string, message string, success bool, hasActiveTask bool) {
	payload := map[string]any{
		"event_type":      string(schema.EventTypeChatInterruptResult),
		"intent":          intent,
		"success":         success,
		"message":         message,
		"session_id":      sessionID,
		"has_active_task": hasActiveTask,
	}

	msg := &schema.Message{
		ID:              requestID,
		Type:            schema.MessageTypeEvent,
		ChannelID:       channelID,
		SessionID:       sessionID,
		Timestamp:       schema.NowTimestamp(),
		OK:              true,
		EventType:       schema.EventTypeChatInterruptResult,
		Payload:         payload,
		EnableStreaming: true,
	}
	mh.PublishRobotMessages(msg)
}

// sendProcessingStatus 发送 processing_status 事件消息。
//
// 对齐 Python _send_processing_status：
// payload 包含 is_processing 和 is_complete，消息 ID 使用 requestID。
func (mh *MessageHandler) sendProcessingStatus(requestID, sessionID, channelID string, isProcessing bool) {
	msg := &schema.Message{
		ID:        requestID,
		Type:      schema.MessageTypeEvent,
		ChannelID: channelID,
		SessionID: sessionID,
		Timestamp: schema.NowTimestamp(),
		OK:        true,
		EventType: schema.EventTypeChatProcessingStatus,
		Payload: map[string]any{
			"is_processing": isProcessing,
			"is_complete":   !isProcessing,
			"session_id":    sessionID,
		},
		EnableStreaming: true,
	}
	mh.PublishRobotMessages(msg)
}

// sendStreamCancelledNotification 发送流式取消通知。
//
// 对齐 Python: 构造 CHAT_INTERRUPT_RESULT 事件，payload 含 intent=cancel, success=true。
func (mh *MessageHandler) sendStreamCancelledNotification(requestID, channelID, sessionID string) {
	msg := &schema.Message{
		ID:        requestID,
		Type:      schema.MessageTypeEvent,
		ChannelID: channelID,
		SessionID: sessionID,
		Timestamp: schema.NowTimestamp(),
		OK:        true,
		EventType: schema.EventTypeChatInterruptResult,
		Payload: map[string]any{
			"event_type": string(schema.EventTypeChatInterruptResult),
			"intent":     "cancel",
			"success":    true,
			"message":    "任务已取消",
			"session_id": sessionID,
		},
		EnableStreaming: true,
	}
	mh.PublishRobotMessages(msg)
}

// publishStreamCancelledFinal 发布流式取消的最终消息。
//
// 对齐 Python: type=event, payload 包含 event_type=chat.final + is_complete=True
func (mh *MessageHandler) publishStreamCancelledFinal(requestID, channelID, sessionID string, requestMetadata map[string]any) {
	msg := &schema.Message{
		ID:        requestID,
		Type:      schema.MessageTypeEvent,
		ChannelID: channelID,
		SessionID: sessionID,
		Timestamp: schema.NowTimestamp(),
		OK:        true,
		EventType: schema.EventTypeChatFinal,
		Payload: map[string]any{
			"event_type":  string(schema.EventTypeChatFinal),
			"content":     "",
			"is_complete": true,
		},
		Metadata:        requestMetadata,
		EnableStreaming: true,
	}
	mh.PublishRobotMessages(msg)
}

// buildErrorOutMessage 构造错误响应消息。
//
// 对齐 Python: 使用原始消息的 ID/ChannelID/SessionID/Metadata 构造错误响应。
func (mh *MessageHandler) buildErrorOutMessage(msg *schema.Message, err error) *schema.Message {
	return &schema.Message{
		ID:              msg.ID,
		Type:            schema.MessageTypeRes,
		ChannelID:       msg.ChannelID,
		SessionID:       msg.SessionID,
		Timestamp:       schema.NowTimestamp(),
		OK:              false,
		Payload:         map[string]any{"error": err.Error()},
		Metadata:        msg.Metadata,
		EnableStreaming: true,
	}
}

// buildToolResultMessage 构造工具结果消息。
//
// 对齐 Python _build_tool_result_message (L2790-L2818)：
// id 格式: tool_result_{timestamp:x}_{random_hex}，
// type=event, event_type=CHAT_TOOL_RESULT，
// payload 含 tool_result 字典（tool_name/tool_call_id/result/status）。
func (mh *MessageHandler) buildToolResultMessage(channelID, sessionID string, toolInfo map[string]any, metadata map[string]any) *schema.Message {
	id := fmt.Sprintf("tool_result_%x_%s", time.Now().UnixMilli(), generateRandomHex(3))

	// 构建 tool_result 字典
	toolResult := map[string]any{}
	if toolName, ok := toolInfo["tool_name"]; ok {
		toolResult["tool_name"] = toolName
	}
	if toolCallID, ok := toolInfo["tool_call_id"]; ok {
		toolResult["tool_call_id"] = toolCallID
	}
	if result, ok := toolInfo["result"]; ok {
		toolResult["result"] = result
	}
	if status, ok := toolInfo["status"]; ok {
		toolResult["status"] = status
	}

	return &schema.Message{
		ID:              id,
		Type:            schema.MessageTypeEvent,
		ChannelID:       channelID,
		SessionID:       sessionID,
		Timestamp:       schema.NowTimestamp(),
		OK:              true,
		EventType:       schema.EventTypeChatToolResult,
		Payload:         map[string]any{"tool_result": toolResult},
		Metadata:        metadata,
		EnableStreaming: true,
	}
}

// sendCancelledToolResults 发送已取消的工具结果消息。
//
// 对齐 Python _send_cancelled_tool_results (L2820-L2841)：
// 从 payload 中提取 cancelled_tools 列表，
// 为每个工具构造 tool_result 消息并发布。
func (mh *MessageHandler) sendCancelledToolResults(channelID, sessionID string, payload map[string]any, metadata map[string]any) {
	if payload == nil {
		return
	}

	cancelledToolsRaw, ok := payload["cancelled_tools"]
	if !ok || cancelledToolsRaw == nil {
		return
	}

	// cancelled_tools 应为列表
	var cancelledTools []map[string]any
	switch v := cancelledToolsRaw.(type) {
	case []any:
		for _, item := range v {
			if m, isMap := item.(map[string]any); isMap {
				cancelledTools = append(cancelledTools, m)
			}
		}
	case []map[string]any:
		cancelledTools = v
	}

	for _, toolInfo := range cancelledTools {
		msg := mh.buildToolResultMessage(channelID, sessionID, toolInfo, metadata)
		mh.PublishRobotMessages(msg)
	}
}
