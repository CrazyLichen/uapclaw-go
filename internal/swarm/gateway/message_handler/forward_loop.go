package message_handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// streamFinalState 流式任务结束状态追踪（供 defer 使用）
type streamFinalState struct {
	cancelled                bool
	hasProcessingStatusFalse bool
}

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
// 对齐 Python _forward_loop (L2163-L2558) 的 11 步骤：
//
//	步骤1:  handleChannelControl(msg)
//	步骤2:  ApplyChannelState(msg)
//	步骤3:  Gateway hook: UserPromptSubmit（预留 TODO）
//	步骤4:  CHAT_ANSWER 分支
//	步骤5:  CHAT_CANCEL 分支
//	步骤6:  Inbound Pipeline（预留 TODO）
//	步骤7:  resolveInboundReferences(msg)
//	步骤8:  prepareAgentDispatchMessage(ctx, msg)
//	步骤9:  before_chat_request hook（预留 TODO）
//	步骤10: handleChatSend(ctx, msg)
//	步骤11: 异常 → buildErrorOutMessage → publishRobotMessages
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

			// 步骤1: 处理 slash 命令（仅受控渠道）
			if mh.handleChannelControl(msg) {
				continue
			}

			// 步骤2: 注入渠道状态（session_id / mode）
			mh.ApplyChannelState(msg)

			// TODO: 步骤3 - Gateway hook: UserPromptSubmit（等 11.13 Gateway Hook 回填）
			// Python: if self._gateway_hook_handler:
			//     await self._gateway_hook_handler.on_user_prompt_submit(session_id, prompt_text)

			// 步骤4: CHAT_ANSWER 分支
			if msg.ReqMethod == schema.ReqMethodChatAnswer {
				mh.handleChatUserAnswer(ctx, msg)
				continue
			}

			// 步骤5: CHAT_CANCEL 分支
			if msg.ReqMethod == schema.ReqMethodChatCancel {
				mh.handleChatCancel(ctx, msg)
				continue
			}

			// TODO: 步骤6 - Inbound Pipeline（数字分身入站过滤）（等 11.12 IM Pipeline 回填）
			// Python: if self._inbound_pipeline is not None and msg.req_method == ReqMethod.CHAT_SEND:
			//     should_forward = await self._inbound_pipeline.apply(msg)
			//     if not should_forward: continue

			// 步骤7: Resolve @file/@agent（仅 CHAT_SEND）
			if msg.ReqMethod == schema.ReqMethodChatSend {
				mh.resolveInboundReferences(msg)
			}

			// 步骤8: 准备 Agent 派发消息
			agentMsg := mh.prepareAgentDispatchMessage(ctx, msg)

			// TODO: 步骤9 - before_chat_request hook（等 11.13 Gateway Hook 回填）
			// Python: await self._trigger_before_chat_request_hook(agent_msg)

			// 步骤10: chat.send 分发
			mh.handleChatSend(ctx, msg, agentMsg)
		}
	}
}

// handleChatSend 处理 chat.send 请求的 stream/non-stream 分发。
//
// 对齐 Python _forward_loop 步骤10 (L2496-2558)：
// 记住用户查询上下文已在 HandleMessage 入队前调过（对齐 Python handle_message），
// 此处仅做 AgentClient 连接检查 + stream/non-stream 分发。
func (mh *MessageHandler) handleChatSend(ctx context.Context, msg *schema.Message, agentMsg *schema.Message) {
	// 检查 AgentClient 可用性
	if mh.agentClient == nil || !mh.agentClient.IsConnected() {
		logger.Warn(logComponent).
			Str("event_type", "forward_no_agent_client").
			Str("msg_id", msg.ID).
			Msg("AgentClient 未连接，无法转发")
		return
	}

	env := e2a.MessageToE2AOrFallback(agentMsg)
	streamRid := env.RequestID
	if streamRid == "" {
		streamRid = msg.ID
	}

	if env.IsStream {
		// 流式分发
		emitProcessingStatus := mh.shouldEmitProcessingStatusForStream(msg)
		if emitProcessingStatus {
			mh.sendProcessingStatus(streamRid, msg.SessionID, msg.ChannelID, true)
		}
		mh.registerStreamTask(streamRid, msg.SessionID, msg.Metadata, nil)
		mh.streamEmitsProcessingStatus[streamRid] = emitProcessingStatus
		mh.streamModes[streamRid] = mh.extractModeFromParams(msg)
		go mh.processStream(ctx, msg, env, emitProcessingStatus)

		logger.Info(logComponent).
			Str("event_type", "stream_task_started").
			Str("request_id", streamRid).
			Str("channel_id", msg.ChannelID).
			Int("concurrent_tasks", len(mh.streamTasks)).
			Msg("Stream 任务已启动（后台运行）")
	} else if mh.nonStreamRPCMayRunParallel(env) {
		// 非流式并行
		go func() { _, _ = mh.processNonStreamRequest(ctx, msg, env) }()
		logger.Info(logComponent).
			Str("event_type", "non_stream_parallel").
			Str("request_id", streamRid).
			Str("method", env.Method).
			Msg("非流式 RPC 已后台执行")
	} else {
		// 非流式串行
		if _, err := mh.processNonStreamRequest(ctx, msg, env); err != nil {
			// 步骤11: 异常处理
			errMsg := mh.buildErrorOutMessage(msg, err)
			mh.PublishRobotMessages(errMsg)
		}
	}
}

// handleChatUserAnswer 处理 chat.user_answer 请求。
//
// 对齐 Python _forward_loop 步骤4 (L2200-2239)：
// 非流式处理 + evolution 审批判断。
// **不调 forwardToAgent**，方法内完整处理。
func (mh *MessageHandler) handleChatUserAnswer(ctx context.Context, msg *schema.Message) {
	// 检查 AgentClient 可用性
	if mh.agentClient == nil || !mh.agentClient.IsConnected() {
		logger.Warn(logComponent).
			Str("event_type", "chat_answer_no_agent_client").
			Str("msg_id", msg.ID).
			Msg("AgentClient 未连接，无法处理 chat.answer")
		return
	}

	agentMsg := mh.prepareAgentDispatchMessage(ctx, msg)
	env := e2a.MessageToE2AOrFallback(agentMsg)
	env.IsStream = false

	resp, _ := mh.processNonStreamRequest(ctx, msg, env)

	// 检查是否为 evolution 审批回答
	answerRequestID := ""
	if len(msg.Params) > 0 {
		var paramsMap map[string]any
		if err := json.Unmarshal(msg.Params, &paramsMap); err == nil {
			if rid, ok := paramsMap["request_id"]; ok {
				answerRequestID = fmt.Sprintf("%v", rid)
			}
		}
	}

	if IsEvolutionApprovalRequestID(answerRequestID) {
		resolved := false
		if resp != nil && resp.Payload != nil {
			if r, ok := resp.Payload["resolved"]; ok {
				if b, isBool := r.(bool); isBool && b {
					resolved = true
				}
			}
		}

		if resolved {
			queuedPayload := mh.finishEvolutionApprovalIfCurrent(msg.SessionID, answerRequestID)
			queuedInput := ""
			if queuedPayload != nil {
				if s, ok := queuedPayload["new_input"].(string); ok {
					queuedInput = strings.TrimSpace(s)
				}
			}
			var queuedAttachments []map[string]any
			if queuedPayload != nil {
				if a, ok := queuedPayload["attachments"]; ok {
					switch v := a.(type) {
					case []map[string]any:
						queuedAttachments = v
					case []any:
						for _, item := range v {
							if m, isMap := item.(map[string]any); isMap {
								queuedAttachments = append(queuedAttachments, m)
							}
						}
					}
				}
			}

			if queuedInput != "" {
				originalRequest := mh.getSessionLastUserQuery(msg.SessionID)
				queuedMsg := BuildQueuedChatSendMessage(msg, queuedInput, queuedAttachments, originalRequest)
				mh.PublishUserMessagesNowait(queuedMsg)
				logger.Info(logComponent).
					Str("event_type", "evolution_approval_dispatched").
					Str("id", queuedMsg.ID).
					Str("session_id", msg.SessionID).
					Msg("evolution approval answered (resolved), queued supplement dispatched")
			}
		} else {
			logger.Info(logComponent).
				Str("event_type", "evolution_approval_not_resolved").
				Str("id", msg.ID).
				Str("session_id", msg.SessionID).
				Str("request_id", answerRequestID).
				Msg("evolution approval answered but not resolved")
		}
	}
}

// handleChatCancel 处理 chat.cancel/interrupt 请求。
//
// 对齐 Python _forward_loop 步骤5 (L2241-2437)：
// supplement / pause / resume / cancel 三个子分支。
func (mh *MessageHandler) handleChatCancel(ctx context.Context, msg *schema.Message) {
	logger.Info(logComponent).
		Str("event_type", "interrupt_request").
		Str("id", msg.ID).
		Str("channel_id", msg.ChannelID).
		Msg("收到中断请求")

	// 解析参数
	var paramsMap map[string]any
	if len(msg.Params) > 0 {
		_ = json.Unmarshal(msg.Params, &paramsMap)
	}
	if paramsMap == nil {
		paramsMap = make(map[string]any)
	}

	newInput, _ := paramsMap["new_input"].(string)
	hasNewInput := strings.TrimSpace(newInput) != ""
	var supplementAttachments []map[string]any
	if rawAtt, ok := paramsMap["attachments"]; ok {
		switch v := rawAtt.(type) {
		case []map[string]any:
			supplementAttachments = v
		case []any:
			for _, item := range v {
				if m, isMap := item.(map[string]any); isMap {
					supplementAttachments = append(supplementAttachments, m)
				}
			}
		}
	}
	intent, _ := paramsMap["intent"].(string)
	if intent == "" {
		intent = "cancel"
	}

	if hasNewInput {
		// supplement 分支：有新输入
		mh.handleSupplement(ctx, msg, newInput, supplementAttachments, paramsMap)
		return
	}

	if intent == "cancel" {
		// cancel 分支
		mh.CancelAgentWorkForSession(ctx, msg, msg.SessionID, true)
		return
	}

	if intent == "pause" || intent == "resume" {
		// pause/resume 分支
		mh.handlePauseResume(ctx, msg, intent)
		return
	}

	// 其他 intent 按取消处理
	mh.CancelAgentWorkForSession(ctx, msg, msg.SessionID, true)
}

// handleSupplement 处理 supplement 分支（有新输入的 CHAT_CANCEL）。
//
// 对齐 Python _forward_loop L2254-2403。
func (mh *MessageHandler) handleSupplement(ctx context.Context, msg *schema.Message, newInput string, attachments []map[string]any, paramsMap map[string]any) {
	sessionID := msg.SessionID

	// 检查 evolution 状态
	if mh.isSessionEvolutionInProgress(sessionID) ||
		(sessionID != "" && func() bool {
			mh.evolutionMu.RLock()
			defer mh.evolutionMu.RUnlock()
			_, exists := mh.pendingEvolutionApproval[sessionID]
			return exists
		}()) {
		mh.queueSupplementInput(sessionID, strings.TrimSpace(newInput), attachments)
		logger.Info(logComponent).
			Str("event_type", "supplement_queued_evolution").
			Str("session_id", sessionID).
			Msg("evolution phase pending, queue supplement input")
		mh.sendInterruptResultNotification(msg.ID, msg.ChannelID, sessionID, "supplement", "已加入队列，等待演进完成", true, nil)
		return
	}

	// 1. 取消 gateway 侧当前 session 相关的流式任务
	requestIDs := mh.collectStreamTasksForSession(sessionID)
	for _, reqID := range requestIDs {
		mh.cancelStreamTask(reqID)
		logger.Info(logComponent).
			Str("event_type", "supplement_cancel_stream").
			Str("request_id", reqID).
			Str("session_id", sessionID).
			Msg("supplement: 取消流式任务")
	}

	// 2. 通知前端 supplement
	mh.sendInterruptResultNotification(msg.ID, msg.ChannelID, sessionID, "supplement", "", true, nil)

	// 3. 发送 supplement intent 到 AgentServer（取消任务但保留 todo）
	agentMsg := mh.prepareAgentDispatchMessage(ctx, msg)
	runtimeParams := mh.extractRuntimeParams(msg, paramsMap)

	supplementParams := map[string]any{
		"intent":     "supplement",
		"session_id": agentMsg.SessionID,
	}
	for k, v := range runtimeParams {
		supplementParams[k] = v
	}

	supplementEnv := e2a.E2AFromAgentFields(
		fmt.Sprintf("supplement_%x", time.Now().UnixMilli()),
		e2a.WithFieldChannelID(msg.ChannelID),
		e2a.WithFieldSessionID(agentMsg.SessionID),
		e2a.WithFieldReqMethod(string(schema.ReqMethodChatCancel)),
		e2a.WithFieldParams(supplementParams),
		e2a.WithFieldIsStream(false),
		e2a.WithFieldTimestamp(float64(time.Now().UnixMilli())/1000.0),
		e2a.WithFieldMetadata(msg.Metadata),
	)

	if mh.agentClient != nil && mh.agentClient.IsConnected() {
		resp, err := mh.agentClient.SendRequest(ctx, supplementEnv)
		if err == nil && resp != nil {
			payload := resp.Payload
			if payload == nil {
				payload = make(map[string]any)
			}
			mh.sendCancelledToolResults(msg.ChannelID, sessionID, payload, msg.Metadata)
		}
	}

	// 4. 入队新任务
	originalRequest := mh.getSessionLastUserQuery(sessionID)
	queuedMsg := BuildQueuedChatSendMessage(msg, strings.TrimSpace(newInput), attachments, originalRequest)
	// 注入 runtime_params 到新消息的 params
	if len(runtimeParams) > 0 {
		var newParams map[string]any
		if err := json.Unmarshal(queuedMsg.Params, &newParams); err == nil {
			for k, v := range runtimeParams {
				if _, exists := newParams[k]; !exists {
					newParams[k] = v
				}
			}
			if updated, err := json.Marshal(newParams); err == nil {
				queuedMsg.Params = json.RawMessage(updated)
			}
		}
	}
	// 注入 model_name
	if modelName, ok := paramsMap["model_name"]; ok && modelName != nil {
		var newParams map[string]any
		if err := json.Unmarshal(queuedMsg.Params, &newParams); err == nil {
			newParams["model_name"] = modelName
			if updated, err := json.Marshal(newParams); err == nil {
				queuedMsg.Params = json.RawMessage(updated)
			}
		}
	}

	mh.PublishUserMessagesNowait(queuedMsg)
	logger.Info(logComponent).
		Str("event_type", "supplement_new_task_enqueued").
		Str("id", queuedMsg.ID).
		Str("session_id", sessionID).
		Msg("supplement: 旧任务已取消，新任务已入队")
}

// handlePauseResume 处理 pause/resume 分支。
//
// 对齐 Python _forward_loop L2408-2435。
func (mh *MessageHandler) handlePauseResume(ctx context.Context, msg *schema.Message, intent string) {
	agentMsg := mh.prepareAgentDispatchMessage(ctx, msg)

	// 确保 mode 信息存在，否则从 channelStates 注入
	if len(agentMsg.Params) > 0 {
		var paramsMap map[string]any
		if err := json.Unmarshal(agentMsg.Params, &paramsMap); err == nil {
			if _, hasMode := paramsMap["mode"]; !hasMode {
				state := mh.GetOrCreateChannelState(msg)
				paramsMap["mode"] = ChannelModeString(state.Mode)
				if updated, err := json.Marshal(paramsMap); err == nil {
					agentMsg.Params = json.RawMessage(updated)
				}
			}
		}
	}

	_ = e2a.MessageToE2AOrFallback(agentMsg)
	// fire-and-forget：传入 msg 和 intent，sendInterruptToAgent 从 msg 提取 mode/trusted_dirs
	go mh.sendInterruptToAgent(ctx, msg, intent)

	// 检查当前 session 是否有活跃流式任务
	hasActiveTask := mh.hasActiveStreamTaskForSession(msg.SessionID)
	mh.sendInterruptResultNotification(msg.ID, msg.ChannelID, msg.SessionID, intent, "", true, &hasActiveTask)
}

// resolveInboundReferences 解析入站消息中的 @file/@agent 引用。
//
// 对齐 Python _forward_loop 步骤7 (L2450-2495)：
// 仅对 CHAT_SEND 生效。
func (mh *MessageHandler) resolveInboundReferences(msg *schema.Message) {
	if msg.ReqMethod != schema.ReqMethodChatSend || len(msg.Params) == 0 {
		return
	}

	var paramsMap map[string]any
	if err := json.Unmarshal(msg.Params, &paramsMap); err != nil {
		return
	}

	content := ""
	if q, ok := paramsMap["query"]; ok {
		if s, isStr := q.(string); isStr {
			content = s
		}
	}
	if content == "" {
		if c, ok := paramsMap["content"]; ok {
			if s, isStr := c.(string); isStr {
				content = s
			}
		}
	}
	if content == "" {
		return
	}

	// 获取 cwd
	cwd := ""
	if msg.Metadata != nil {
		if c, ok := msg.Metadata["cwd"]; ok {
			if s, isStr := c.(string); isStr {
				cwd = s
			}
		}
	}

	enriched := content

	// 解析 @file 引用
	attachments, hasAttachments := paramsMap["attachments"]
	if hasAttachments && attachments != nil {
		// 转换 attachments 为 []map[string]any
		var attList []map[string]any
		switch v := attachments.(type) {
		case []map[string]any:
			attList = v
		case []any:
			for _, item := range v {
				if m, isMap := item.(map[string]any); isMap {
					attList = append(attList, m)
				}
			}
		}
		if len(attList) > 0 {
			enriched = ResolveStructuredAttachments(content, attList, cwd)
		}
	} else if strings.Contains(content, "@") {
		enriched = ResolveAtFileReferences(content, cwd, 0)
	}

	// 解析 @agent-xxx 提及
	agentMentions := ExtractAgentMentions(content)
	if len(agentMentions) > 0 {
		var hintParts []string
		for _, agentName := range agentMentions {
			hintParts = append(hintParts,
				fmt.Sprintf("用户表达了调用智能体 \"%s\" 的意图。请按需调用该智能体，并向其传递所需的上下文。", agentName),
			)
		}
		agentHint := strings.Join(hintParts, "\n")
		enriched = enriched + "\n\n<system-reminder>\n" + agentHint + "\n</system-reminder>"
		logger.Info(logComponent).
			Str("event_type", "agent_mentions_detected").
			Strs("mentions", agentMentions).
			Msg("Agent mentions detected")
	}

	// 如果内容被修改，回写 params
	if enriched != content {
		paramsMap["query"] = enriched
		if _, hasContent := paramsMap["content"]; hasContent {
			paramsMap["content"] = enriched
		}
		if updated, err := json.Marshal(paramsMap); err == nil {
			msg.Params = json.RawMessage(updated)
		}
		logger.Info(logComponent).
			Str("event_type", "inbound_references_resolved").
			Str("id", msg.ID).
			Msg("attachments/agent-mentions resolved in chat.send")
	}
}

// processStream 流式处理：发送请求并持续读取响应 chunk。
//
// 对齐 Python process_stream (L2559-2648)：
// 新增 emitProcessingStatus 参数、hasProcessingStatusFalse 追踪、
// evolution chunk 处理、cancelled final、processing_status=false 通知。
func (mh *MessageHandler) processStream(ctx context.Context, msg *schema.Message, envelope *e2a.E2AEnvelope, emitProcessingStatus bool) {
	requestID := envelope.RequestID
	channelID := msg.ChannelID
	sessionID := msg.SessionID
	requestMetadata := msg.Metadata

	streamCtx, streamCancel := context.WithCancel(ctx)
	// 更新 streamTasks 的 cancel func
	mh.streamMu.Lock()
	mh.streamTasks[requestID] = streamCancel
	mh.streamMu.Unlock()

	// 追踪状态（供 defer 使用）
	streamState := &streamFinalState{}
	defer mh.streamFinalCleanup(requestID, sessionID, emitProcessingStatus, streamState)

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
			streamState.cancelled = true
			mh.publishStreamCancelledFinal(requestID, channelID, sessionID, requestMetadata)
			return
		case chunk, ok := <-chunkCh:
			if !ok {
				// 流正常结束
				logger.Info(logComponent).
					Str("event_type", "stream_completed").
					Str("request_id", requestID).
					Msg("Stream 正常完成")
				return
			}
			if chunk == nil {
				continue
			}

			// 跳过终止哨兵
			if IsTerminalStreamChunk(chunk) {
				logger.Debug(logComponent).
					Str("event_type", "stream_terminal_chunk").
					Str("request_id", chunk.RequestID).
					Msg("跳过终止 chunk")
				continue
			}

			// evolution chunk 处理
			mh.handleEvolutionChunk(chunk, sessionID, requestMetadata)

			// 追踪 processing_status=false
			if chunk.Payload != nil {
				if eventType, ok := chunk.Payload["event_type"].(string); ok {
					if eventType == string(schema.EventTypeChatProcessingStatus) {
						if isProcessing, ok := chunk.Payload["is_processing"].(bool); ok && !isProcessing {
							streamState.hasProcessingStatusFalse = true
						}
					}
				}
			}

			// Agent 响应块转为消息再推送到机器人消息列表
			reqMetadata := mh.getStreamMetadata(requestID)
			outMsg := ChunkToMessage(chunk, mh.getStreamSessionID(requestID), reqMetadata)
			mh.PublishRobotMessages(outMsg)
		}
	}
}

// streamFinalCleanup 流式任务结束后执行 finally 清理逻辑。
//
// 对齐 Python process_stream finally 块 (L2618-2648)。
func (mh *MessageHandler) streamFinalCleanup(requestID, sessionID string, emitProcessingStatus bool, state *streamFinalState) {
	// 清理状态
	mh.streamMu.Lock()
	delete(mh.streamTasks, requestID)
	delete(mh.streamSessions, requestID)
	delete(mh.streamMetadata, requestID)
	delete(mh.streamEmitsProcessingStatus, requestID)
	delete(mh.streamModes, requestID)
	mh.streamMu.Unlock()

	// evolution 清理：如果 session 不再有任何流式任务，清除 in_progress
	if sessionID != "" && !mh.hasActiveStreamTaskForSession(sessionID) {
		mh.clearSessionEvolutionInProgress(sessionID)
	}

	// 通知前端处理完成（只有当 AgentServer 没有发送过 processing_status=false 时才发送）
	if emitProcessingStatus && !state.cancelled && !state.hasProcessingStatusFalse {
		// 检查该 session_id 是否还有活跃的 emitsProcessingStatus 任务
		sessionHasActiveTasks := mh.hasActiveEmitProcessingStatusTaskForSession(sessionID)
		if !sessionHasActiveTasks {
			mh.sendProcessingStatus(requestID, sessionID, "", false)
			logger.Info(logComponent).
				Str("event_type", "processing_status_false").
				Str("session_id", sessionID).
				Msg("该 session 流式任务已完成，已发送 is_processing=false")
		}
	}
}

// processNonStreamRequest 非流式处理：发送请求并等待完整响应。
//
// 对齐 Python _process_non_stream_request (L2134-2159)：
// 返回值改为 (*schema.AgentResponse, error)，供 handleChatUserAnswer 使用。
func (mh *MessageHandler) processNonStreamRequest(ctx context.Context, msg *schema.Message, envelope *e2a.E2AEnvelope) (*schema.AgentResponse, error) {
	requestID := envelope.RequestID

	// 通过 AgentClient 发送非流式请求
	resp, err := mh.agentClient.SendRequest(ctx, envelope)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "non_stream_error").
			Err(err).
			Str("request_id", requestID).
			Msg("非流式请求失败")
		errMsg := mh.buildErrorOutMessage(msg, err)
		mh.PublishRobotMessages(errMsg)
		return nil, err
	}

	// Agent 响应转为消息再推送到机器人消息列表
	outMsg := ResponseToMessage(resp, msg.SessionID, msg.Metadata)
	mh.PublishRobotMessages(outMsg)

	logger.Info(logComponent).
		Str("event_type", "non_stream_response").
		Str("request_id", resp.RequestID).
		Str("channel_id", resp.ChannelID).
		Msg("Agent 响应已写入 robot_messages")

	return resp, nil
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

// hasActiveStreamTaskForSession 检查指定 session 是否有活跃流式任务
func (mh *MessageHandler) hasActiveStreamTaskForSession(sessionID string) bool {
	mh.streamMu.RLock()
	defer mh.streamMu.RUnlock()
	for _, sid := range mh.streamSessions {
		if sid == sessionID {
			return true
		}
	}
	return false
}

// hasActiveEmitProcessingStatusTaskForSession 检查指定 session 是否有活跃的 emitsProcessingStatus 任务
func (mh *MessageHandler) hasActiveEmitProcessingStatusTaskForSession(sessionID string) bool {
	mh.streamMu.RLock()
	defer mh.streamMu.RUnlock()
	for rid, sid := range mh.streamSessions {
		if sid == sessionID {
			if emits, ok := mh.streamEmitsProcessingStatus[rid]; ok && emits {
				return true
			}
		}
	}
	return false
}

// extractRuntimeParams 从消息中提取运行时参数（cwd, trusted_dirs, mode）。
//
// 供 supplement 分支构造 supplementParams 使用。
func (mh *MessageHandler) extractRuntimeParams(msg *schema.Message, paramsMap map[string]any) map[string]any {
	runtimeParams := make(map[string]any)

	for _, key := range []string{"cwd", "trusted_dirs", "mode"} {
		if value, ok := paramsMap[key]; ok && value != nil {
			runtimeParams[key] = value
		}
	}

	// 从 metadata 中补充 cwd
	if _, hasCwd := runtimeParams["cwd"]; !hasCwd {
		if msg.Metadata != nil {
			if cwd, ok := msg.Metadata["cwd"].(string); ok && strings.TrimSpace(cwd) != "" {
				runtimeParams["cwd"] = strings.TrimSpace(cwd)
			}
		}
	}

	// 从 channelStates 补充 mode
	if _, hasMode := runtimeParams["mode"]; !hasMode {
		state := mh.GetOrCreateChannelState(msg)
		runtimeParams["mode"] = ChannelModeString(state.Mode)
	}

	return runtimeParams
}
