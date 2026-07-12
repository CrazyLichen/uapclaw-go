package message_handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// PublishRobotMessages 将 Agent 响应写入出站 channel。
//
// 非阻塞写入，channel 满时丢弃并记录警告。
// 对齐 Python: MessageHandler.publish_robot_messages()
func (mh *MessageHandler) PublishRobotMessages(msg *schema.Message) {
	select {
	case mh.robotMessages <- msg:
	default:
		logger.Warn(logComponent).
			Str("event_type", "outbound_queue_full").
			Str("msg_id", msg.ID).
			Msg("出站消息队列已满，丢弃消息")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// forwardLoop 入站转发主循环
//
// 从 userMessages channel 持续读取消息，依次处理：
//  1. _handle_channel_control(msg) → 如果是 slash 命令则处理，跳过后续
//  2. _apply_channel_state(msg) → 注入 session_id / mode
//  3. 根据 req_method 分支转发到 AgentServer
//
// 对齐 Python _forward_loop (L2163-L2556)
func (mh *MessageHandler) forwardLoop(ctx context.Context) {
	logger.Info(logComponent).Msg("入站转发循环已启动")
	defer logger.Info(logComponent).Msg("入站转发循环已退出")

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-mh.userMessages:
			if msg == nil {
				continue
			}
			mh.processInbound(ctx, msg)
		}
	}
}

// processInbound 处理单条入站消息
func (mh *MessageHandler) processInbound(ctx context.Context, msg *schema.Message) {
	// 1. 尝试处理 slash 命令（仅受控渠道）
	if mh.handleChannelControl(msg) {
		return // 已作为控制指令处理
	}

	// 2. 注入渠道状态（session_id / mode）
	mh.ApplyChannelState(msg)

	// 3. 根据请求方法转发
	switch msg.ReqMethod {
	case schema.ReqMethodChatSend:
		mh.handleChatSend(ctx, msg)
	case schema.ReqMethodChatCancel:
		mh.handleChatCancel(ctx, msg)
	case schema.ReqMethodChatResume:
		mh.handleChatResume(ctx, msg)
	case schema.ReqMethodChatAnswer:
		mh.handleChatUserAnswer(ctx, msg)
	default:
		// 其他方法直接转发
		mh.forwardToAgent(ctx, msg)
	}
}

// handleChatSend 处理 chat.send 请求
//
// 对齐 Python _forward_loop 中 CHAT_SEND 的 @file 解析 + @agent-xxx 提及逻辑
func (mh *MessageHandler) handleChatSend(ctx context.Context, msg *schema.Message) {
	// 解析 @file 引用和 @agent-xxx 提及（对齐 Python L2449-L2495）
	if len(msg.Params) > 0 {
		params := make(map[string]any)
		if json.Unmarshal(msg.Params, &params) == nil {
			// 提取 content（对齐 Python: msg.params.get("query") or msg.params.get("content") or ""）
			var content string
			if q, ok := params["query"].(string); ok && q != "" {
				content = q
			} else if c, ok := params["content"].(string); ok && c != "" {
				content = c
			}

			// 提取 cwd（对齐 Python: msg.metadata.get("cwd")）
			var cwd string
			if msg.Metadata != nil {
				if c, ok := msg.Metadata["cwd"].(string); ok {
					cwd = c
				}
			}

			enriched := content

			// 解析 structured attachments 或 @file 引用
			if attachments, ok := params["attachments"]; ok && attachments != nil {
				enriched = ResolveStructuredAttachments(content, attachments, cwd)
			} else if content != "" && strings.Contains(content, "@") {
				enriched = ResolveAtFileReferences(content, cwd, 0)
			}

			// 解析 @agent-xxx 提及
			agentMentions := ExtractAgentMentions(content)
			if len(agentMentions) > 0 {
				hintParts := make([]string, 0, len(agentMentions))
				for _, agentName := range agentMentions {
					hintParts = append(hintParts,
						"用户表达了调用智能体 \""+agentName+"\" 的意图。请按需调用该智能体，并向其传递所需的上下文。",
					)
				}
				agentHint := strings.Join(hintParts, "\n")
				enriched = enriched + "\n\n<system-reminder>\n" + agentHint + "\n</system-reminder>"
				logger.Info(logComponent).
					Str("event_type", "agent_mentions_detected").
					Strs("agent_mentions", agentMentions).
					Msg("检测到 Agent 提及")
			}

			// 如果内容被修改，回写 params
			if enriched != content {
				params["query"] = enriched
				if _, hasContent := params["content"]; hasContent {
					params["content"] = enriched
				}
				if updatedParams, err := json.Marshal(params); err == nil {
					msg.Params = updatedParams
				}
				logger.Info(logComponent).
					Str("event_type", "at_file_or_mention_resolved").
					Str("msg_id", msg.ID).
					Msg("chat.send 中 @file 引用或 @agent 提及已解析")
			}
		}
	}

	mh.forwardToAgent(ctx, msg)
}

// handleChatCancel 处理 chat.cancel/interrupt 请求
//
// 根据 intent 参数分为四路分支（对齐 Python L2241-L2437）：
//  1. supplement（有 new_input）：取消旧流式任务 → 发 supplement 通知 → 发 supplement intent → 入队新 chat.send
//  2. cancel：取消当前任务（CancelAgentWorkForSession）
//  3. pause/resume：转发到 AgentServer → 发 interrupt_result 通知
//  4. 默认：与 cancel 同
func (mh *MessageHandler) handleChatCancel(ctx context.Context, msg *schema.Message) {
	// 解析 params
	params := make(map[string]any)
	if len(msg.Params) > 0 {
		_ = json.Unmarshal(msg.Params, &params)
	}

	newInput, _ := params["new_input"].(string)
	hasNewInput := strings.TrimSpace(newInput) != ""
	intent, _ := params["intent"].(string)
	if intent == "" {
		intent = "cancel"
	}

	if hasNewInput {
		// supplement 路径
		mh.handleSupplement(ctx, msg, newInput, params)
		return
	}

	switch intent {
	case "cancel":
		mh.CancelAgentWorkForSession(ctx, msg.SessionID, "cancel")
	case "pause", "resume":
		mh.handlePauseResume(ctx, msg, intent)
	default:
		mh.CancelAgentWorkForSession(ctx, msg.SessionID, intent)
	}
}

// handleChatResume 处理 chat.resume 请求
func (mh *MessageHandler) handleChatResume(ctx context.Context, msg *schema.Message) {
	mh.forwardToAgent(ctx, msg)
}

// handleChatUserAnswer 处理 chat.user_answer 请求
func (mh *MessageHandler) handleChatUserAnswer(ctx context.Context, msg *schema.Message) {
	mh.forwardToAgent(ctx, msg)
}

// forwardToAgent 转发消息到 AgentServer
func (mh *MessageHandler) forwardToAgent(ctx context.Context, msg *schema.Message) {
	if mh.agentClient == nil {
		logger.Warn(logComponent).
			Str("event_type", "forward_no_agent_client").
			Str("msg_id", msg.ID).
			Msg("AgentClient 为空，无法转发")
		return
	}

	if !mh.agentClient.IsConnected() {
		logger.Warn(logComponent).
			Str("event_type", "forward_agent_client_not_connected").
			Str("msg_id", msg.ID).
			Msg("AgentClient 未连接，无法转发")
		return
	}

	// 消息转为 E2A 信封
	envelope := e2a.MessageToE2AOrFallback(msg)

	// 根据是否流式选择处理方式
	if msg.IsStream {
		go mh.processStream(ctx, msg, envelope)
	} else {
		go mh.processNonStreamRequest(ctx, msg, envelope)
	}
}

// processStream 流式处理：发送请求并持续读取响应 chunk
//
// 对齐 Python process_stream
func (mh *MessageHandler) processStream(ctx context.Context, msg *schema.Message, envelope *e2a.E2AEnvelope) {
	requestID := envelope.RequestID
	streamCtx, streamCancel := context.WithCancel(ctx)
	mh.registerStreamTask(requestID, msg.SessionID, msg.Metadata, streamCancel)
	defer mh.unregisterStreamTask(requestID)

	// 通过 AgentClient 发送流式请求
	chunkCh, err := mh.agentClient.SendRequestStream(streamCtx, envelope)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "stream_error").
			Err(err).
			Str("request_id", requestID).
			Msg("流式请求失败")
		return
	}

	// 持续读取响应 chunk
	for {
		select {
		case <-streamCtx.Done():
			return
		case chunk, ok := <-chunkCh:
			if !ok {
				return
			}
			if chunk == nil {
				continue
			}

			// 跳过终止哨兵
			if IsTerminalStreamChunk(chunk) {
				return
			}

			// Agent 响应块转为消息再推送到机器人消息列表
			reqMetadata := mh.getStreamMetadata(requestID)
			outMsg := ChunkToMessage(chunk, mh.getStreamSessionID(requestID), reqMetadata)
			mh.PublishRobotMessages(outMsg)
		}
	}
}

// processNonStreamRequest 非流式处理：发送请求并等待完整响应
//
// 对齐 Python _process_non_stream_request
func (mh *MessageHandler) processNonStreamRequest(ctx context.Context, msg *schema.Message, envelope *e2a.E2AEnvelope) {
	requestID := envelope.RequestID

	// 通过 AgentClient 发送非流式请求
	resp, err := mh.agentClient.SendRequest(ctx, envelope)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "non_stream_error").
			Err(err).
			Str("request_id", requestID).
			Msg("非流式请求失败")
		return
	}

	// Agent 响应转为消息再推送到机器人消息列表
	outMsg := ResponseToMessage(resp, msg.SessionID, msg.Metadata)
	mh.PublishRobotMessages(outMsg)
}

// registerStreamTask 注册流式任务
func (mh *MessageHandler) registerStreamTask(requestID, sessionID string, metadata map[string]any, cancel context.CancelFunc) {
	mh.streamMu.Lock()
	defer mh.streamMu.Unlock()
	mh.streamTasks[requestID] = cancel
	mh.streamSessions[requestID] = sessionID
	if metadata != nil {
		mh.streamMetadata[requestID] = metadata
	}
}

// unregisterStreamTask 注销流式任务
func (mh *MessageHandler) unregisterStreamTask(requestID string) {
	mh.streamMu.Lock()
	defer mh.streamMu.Unlock()
	delete(mh.streamTasks, requestID)
	delete(mh.streamSessions, requestID)
	delete(mh.streamMetadata, requestID)
	delete(mh.streamModes, requestID)
}

// getStreamMetadata 获取流式任务的请求 metadata
func (mh *MessageHandler) getStreamMetadata(requestID string) map[string]any {
	mh.streamMu.RLock()
	defer mh.streamMu.RUnlock()
	return mh.streamMetadata[requestID]
}

// getStreamSessionID 获取流式任务的 sessionID
func (mh *MessageHandler) getStreamSessionID(requestID string) string {
	mh.streamMu.RLock()
	defer mh.streamMu.RUnlock()
	return mh.streamSessions[requestID]
}

// handleSupplement 处理 supplement 路径（有 new_input）。
//
// 对齐 Python L2281-L2399：
// 1. 取消该 session 关联的流式任务
// 2. 发送 supplement interrupt_result 通知
// 3. 发送 supplement intent 到 AgentServer（取消任务但保留 todo）
// 4. 入队新的 chat.send 消息
func (mh *MessageHandler) handleSupplement(ctx context.Context, msg *schema.Message, newInput string, params map[string]any) {
	sessionID := msg.SessionID

	// 1. 取消该 session 关联的流式任务
	requestIDs := mh.collectStreamTasksForSession(sessionID)
	for _, reqID := range requestIDs {
		mh.cancelStreamTask(reqID)
	}

	// 2. 通知前端 supplement（对齐 Python _send_interrupt_result_notification）
	mh.sendInterruptResultNotification(sessionID, "supplement")

	// 3. 发送 supplement intent 到 AgentServer
	if mh.agentClient != nil && mh.agentClient.IsConnected() {
		// 提取 runtime_params（cwd, trusted_dirs, mode）
		runtimeParams := make(map[string]any)
		for _, key := range []string{"cwd", "trusted_dirs", "mode"} {
			if v, ok := params[key]; ok && v != nil {
				runtimeParams[key] = v
			}
		}
		// 从 metadata 补充 cwd
		if _, hasCwd := runtimeParams["cwd"]; !hasCwd && msg.Metadata != nil {
			if cwd, ok := msg.Metadata["cwd"].(string); ok && cwd != "" {
				runtimeParams["cwd"] = cwd
			}
		}
		// 注入 mode 信息
		if _, hasMode := runtimeParams["mode"]; !hasMode {
			mode := mh.getChannelStateMode(msg.ChannelID, sessionID)
			if mode != "" {
				runtimeParams["mode"] = mode
			}
		}

		supplementParams := map[string]any{
			"intent":     "supplement",
			"session_id": sessionID,
		}
		for k, v := range runtimeParams {
			supplementParams[k] = v
		}

		supplementParamsJSON, _ := json.Marshal(supplementParams)
		supplementMsg := schema.NewReqMessage(msg.ChannelID, sessionID, schema.ReqMethodChatCancel,
			supplementParamsJSON,
			schema.WithSessionID(sessionID),
		)
		envelope := e2a.MessageToE2AOrFallback(supplementMsg)
		envelope.IsStream = false

		// 非流式发送 supplement intent，等响应后丢弃
		go func() {
			resp, err := mh.agentClient.SendRequest(ctx, envelope)
			if err != nil {
				logger.Warn(logComponent).
					Str("event_type", "supplement_send_error").
					Err(err).
					Str("session_id", sessionID).
					Msg("supplement intent 发送失败(忽略)")
				return
			}
			logger.Info(logComponent).
				Str("event_type", "supplement_response_discarded").
				Str("request_id", resp.RequestID).
				Bool("ok", resp.OK).
				Msg("supplement intent 响应(已丢弃)")
		}()
	}

	// 4. 入队新的 chat.send 消息
	originalRequest := mh.GetSessionLastUserQuery(sessionID)

	// 构建 supplement continuation query
	continuationQuery := buildSupplementContinuationQuery(newInput, originalRequest)

	supInput := strings.TrimSpace(newInput)
	newParams := map[string]any{
		"query":            continuationQuery,
		"supplement_input": supInput,
		"original_request": originalRequest,
		"session_id":       sessionID,
		"is_supplement":    true,
	}
	// 注入 model_name（如有）
	if modelName, ok := params["model_name"].(string); ok && modelName != "" {
		newParams["model_name"] = modelName
	}
	// 注入 attachments（如有）
	if attachments, ok := params["attachments"]; ok && attachments != nil {
		newParams["attachments"] = attachments
	}
	newParamsJSON, _ := json.Marshal(newParams)

	newMsg := schema.NewReqMessage(msg.ChannelID, sessionID, schema.ReqMethodChatSend,
		newParamsJSON,
		schema.WithSessionID(sessionID),
	)
	newMsg.IsStream = true
	if msg.Metadata != nil {
		newMsg.Metadata = msg.Metadata
	}

	// 入队
	mh.PublishUserMessagesNowait(newMsg)
	logger.Info(logComponent).
		Str("event_type", "supplement_new_task_queued").
		Str("new_msg_id", newMsg.ID).
		Str("session_id", sessionID).
		Msg("supplement: 旧任务已取消，新任务已入队")
}

// handlePauseResume 处理 pause/resume 路径。
//
// 对齐 Python L2408-L2435：
// 1. 转发到 AgentServer（不取消流式任务）
// 2. 发送 interrupt_result 通知
func (mh *MessageHandler) handlePauseResume(ctx context.Context, msg *schema.Message, intent string) {
	// 转发到 AgentServer
	mh.forwardToAgent(ctx, msg)

	// 检查当前 session 是否有活跃的流式任务
	hasActiveTask := false
	requestIDs := mh.collectStreamTasksForSession(msg.SessionID)
	if len(requestIDs) > 0 {
		hasActiveTask = true
	}

	// 发送 interrupt_result 通知（对齐 Python _send_interrupt_result_notification）
	mh.sendInterruptResultNotification(msg.SessionID, intent)

	if hasActiveTask {
		logger.Debug(logComponent).
			Str("event_type", "pause_resume_with_active_task").
			Str("intent", intent).
			Str("session_id", msg.SessionID).
			Msg("pause/resume: session 有活跃的流式任务")
	}
}

// buildSupplementContinuationQuery 构建补充请求的 continuation query。
//
// 一比一对照 Python _build_supplement_continuation_query。
func buildSupplementContinuationQuery(newInput, originalRequest string) string {
	trimmed := strings.TrimSpace(newInput)
	original := strings.TrimSpace(originalRequest)

	var originalSection string
	if original != "" {
		if len(original) > 8000 {
			original = original[:8000]
		}
		originalSection = fmt.Sprintf("\n\n原始任务请求如下，请以它作为继续执行 todo 时的上下文，尤其要保留其中的文件路径、目录、约束和目标：\n%s", original)
	}

	return "用户在当前任务执行中追加了补充/调整请求：\n" +
		fmt.Sprintf("%s\n\n", trimmed) +
		"请先处理这个补充/调整请求，然后检查并继续执行当前会话 todo 列表中仍未完成的 " +
		"in_progress 或 pending 任务。不要因为补充请求本身处理完成就询问用户下一步；" +
		"只有在确认 todo 列表没有未完成任务时，才可以总结或询问后续方向。\n\n" +
		"注意：追加补充请求会中断上一轮流式输出，用户界面上上一轮正在输出的任务结果可能只展示了一部分。" +
		"如果补充请求发生时某个 todo 正在输出结果，或者 todo 状态已经前进但该任务结果可能没有完整展示，" +
		"继续执行时请先补全或简要重述这个被中断任务的完整结果，再推进后续 todo；" +
		"不要仅因为 todo 状态已经变为 completed 就跳过用户尚未完整看到的任务结果。" +
		originalSection
}

// getChannelStateMode 从渠道状态中获取当前 mode。
func (mh *MessageHandler) getChannelStateMode(channelID, sessionID string) string {
	mh.statesMu.RLock()
	defer mh.statesMu.RUnlock()

	// 先尝试 channelKey（channelID + sessionID 组合键）
	key := channelID + ":" + sessionID
	if state, ok := mh.channelStates[key]; ok && state != nil {
		return ChannelModeString(state.Mode)
	}
	// 回退到 channelID 键
	if state, ok := mh.channelStates[channelID]; ok && state != nil {
		return ChannelModeString(state.Mode)
	}
	return ""
}
