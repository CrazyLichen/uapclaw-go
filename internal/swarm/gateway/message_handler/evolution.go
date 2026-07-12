package message_handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsEvolutionApprovalRequestID 判断 requestID 是否为 evolution 审批请求 ID。
//
// 对齐 Python _is_evolution_approval_request_id (L1891-1898)：
// 支持 skill evolution (skill_evolve_*) 和 team skill evolution (team_skill_evolve_*)。
func IsEvolutionApprovalRequestID(requestID string) bool {
	return strings.HasPrefix(requestID, "skill_evolve_") || strings.HasPrefix(requestID, "team_skill_evolve_")
}

// BuildSupplementContinuationQuery 构造补充续接查询文本。
//
// 对齐 Python _build_supplement_continuation_query (L2076-2098)：
// 将用户补充输入和原始任务请求组合为续接提示词。
func BuildSupplementContinuationQuery(newInput, originalRequest string) string {
	trimmed := strings.TrimSpace(newInput)
	original := strings.TrimSpace(originalRequest)

	originalSection := ""
	if original != "" {
		// 截取前 8000 字符，对齐 Python original[:8000]
		runes := []rune(original)
		if len(runes) > 8000 {
			original = string(runes[:8000])
		}
		originalSection = fmt.Sprintf(
			"\n\n原始任务请求如下，请以它作为继续执行 todo 时的上下文，尤其要保留其中的文件路径、目录、约束和目标：\n%s",
			original,
		)
	}

	return fmt.Sprintf(
		"用户在当前任务执行中追加了补充/调整请求：\n%s\n\n"+
			"请先处理这个补充/调整请求，然后检查并继续执行当前会话 todo 列表中仍未完成的 "+
			"in_progress 或 pending 任务。不要因为补充请求本身处理完成就询问用户下一步；"+
			"只有在确认 todo 列表没有未完成任务时，才可以总结或询问后续方向。\n\n"+
			"注意：追加补充请求会中断上一轮流式输出，用户界面上上一轮正在输出的任务结果可能只展示了一部分。"+
			"如果补充请求发生时某个 todo 正在输出结果，或者 todo 状态已经前进但该任务结果可能没有完整展示，"+
			"继续执行时请先补全或简要重述这个被中断任务的完整结果，再推进后续 todo；"+
			"不要仅因为 todo 状态已经变为 completed 就跳过用户尚未完整看到的任务结果。%s",
		trimmed,
		originalSection,
	)
}

// BuildQueuedChatSendMessage 构造排队的 chat.send 消息。
//
// 对齐 Python _build_queued_chat_send_message (L2101-2132)：
// 构造 supplement 续接的 chat.send 请求消息。
func BuildQueuedChatSendMessage(msg *schema.Message, newInput string, attachments []map[string]any, originalRequest string) *schema.Message {
	newReqID := fmt.Sprintf("req_%x_%s", time.Now().UnixMilli(), msg.ID)

	query := BuildSupplementContinuationQuery(newInput, originalRequest)
	params := map[string]any{
		"query":            query,
		"supplement_input": newInput,
		"original_request": originalRequest,
		"session_id":       msg.SessionID,
		"is_supplement":    true,
	}
	if len(attachments) > 0 {
		params["attachments"] = attachments
	}

	paramsJSON, _ := json.Marshal(params)

	return &schema.Message{
		ID:        newReqID,
		Type:      schema.MessageTypeReq,
		ChannelID: msg.ChannelID,
		SessionID: msg.SessionID,
		Params:    json.RawMessage(paramsJSON),
		Timestamp: float64(time.Now().UnixMilli()) / 1000.0,
		OK:        true,
		ReqMethod: schema.ReqMethodChatSend,
		IsStream:  true,
		Metadata:  msg.Metadata,
		Provider:  msg.Provider,
		ChatID:    msg.ChatID,
		UserID:    msg.UserID,
		BotID:     msg.BotID,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// queueSupplementInput 排队补充输入。
//
// 对齐 Python _queue_supplement_input (L1900-1911)：
// 将用户的补充输入存入队列，等待 evolution 审批完成后重新发送。
func (mh *MessageHandler) queueSupplementInput(sessionID, newInput string, attachments []map[string]any) {
	if sessionID == "" {
		return
	}
	payload := map[string]any{"new_input": newInput}
	if len(attachments) > 0 {
		payload["attachments"] = attachments
	}
	mh.evolutionMu.Lock()
	mh.queuedSupplementInput[sessionID] = payload
	mh.evolutionMu.Unlock()
}

// popQueuedSupplementInput 取出并删除排队的补充输入。
//
// 对齐 Python _pop_queued_supplement_input (L1913-1916)。
func (mh *MessageHandler) popQueuedSupplementInput(sessionID string) map[string]any {
	if sessionID == "" {
		return nil
	}
	mh.evolutionMu.Lock()
	defer mh.evolutionMu.Unlock()
	payload, exists := mh.queuedSupplementInput[sessionID]
	if exists {
		delete(mh.queuedSupplementInput, sessionID)
	}
	return payload
}

// markPendingEvolutionApproval 标记待审批的 evolution 请求。
//
// 对齐 Python _mark_pending_evolution_approval (L1918-1922)。
func (mh *MessageHandler) markPendingEvolutionApproval(sessionID, requestID string) {
	if sessionID == "" {
		return
	}
	if IsEvolutionApprovalRequestID(requestID) {
		mh.evolutionMu.Lock()
		mh.pendingEvolutionApproval[sessionID] = requestID
		mh.evolutionMu.Unlock()
	}
}

// clearPendingEvolutionApproval 清除待审批的 evolution 请求。
//
// 对齐 Python _clear_pending_evolution_approval (L1980-1983)。
func (mh *MessageHandler) clearPendingEvolutionApproval(sessionID string) {
	if sessionID == "" {
		return
	}
	mh.evolutionMu.Lock()
	delete(mh.pendingEvolutionApproval, sessionID)
	mh.evolutionMu.Unlock()
}

// finishEvolutionApprovalIfCurrent 完成当前 evolution 审批并返回排队输入。
//
// 对齐 Python _finish_evolution_approval_if_current (L1998-2019)：
// 如果 answered_request_id 与当前 pending 的 request_id 一致，
// 则清除 pending + in_progress 状态，返回排队的补充输入。
func (mh *MessageHandler) finishEvolutionApprovalIfCurrent(sessionID, answeredRequestID string) map[string]any {
	if sessionID == "" || answeredRequestID == "" {
		return nil
	}

	mh.evolutionMu.RLock()
	currentRequestID, exists := mh.pendingEvolutionApproval[sessionID]
	mh.evolutionMu.RUnlock()

	if !exists || currentRequestID != answeredRequestID {
		logger.Info(logComponent).
			Str("event_type", "evolution_approval_stale").
			Str("session_id", sessionID).
			Str("answered_rid", answeredRequestID).
			Str("current_rid", currentRequestID).
			Msg("过时的 evolution 审批已解决，保留当前 pending")
		return nil
	}

	mh.clearPendingEvolutionApproval(sessionID)
	mh.clearSessionEvolutionInProgress(sessionID)
	return mh.popQueuedSupplementInput(sessionID)
}

// markSessionEvolutionInProgress 标记 session 正在进行 evolution 审批。
//
// 对齐 Python _mark_session_evolution_in_progress (L1985-1988)。
func (mh *MessageHandler) markSessionEvolutionInProgress(sessionID string) {
	if sessionID == "" {
		return
	}
	mh.evolutionMu.Lock()
	mh.sessionEvolutionInProgress[sessionID] = true
	mh.evolutionMu.Unlock()
}

// clearSessionEvolutionInProgress 清除 session 的 evolution 进行中标记。
//
// 对齐 Python _clear_session_evolution_in_progress (L1990-1993)。
func (mh *MessageHandler) clearSessionEvolutionInProgress(sessionID string) {
	if sessionID == "" {
		return
	}
	mh.evolutionMu.Lock()
	delete(mh.sessionEvolutionInProgress, sessionID)
	mh.evolutionMu.Unlock()
}

// isSessionEvolutionInProgress 判断 session 是否正在进行 evolution。
//
// 对齐 Python _is_session_evolution_in_progress (L1995-1996)。
func (mh *MessageHandler) isSessionEvolutionInProgress(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	mh.evolutionMu.RLock()
	defer mh.evolutionMu.RUnlock()
	return mh.sessionEvolutionInProgress[sessionID]
}

// clearSessionEvolutionStates 清除 session 的所有 evolution 状态。
//
// 对齐 Python _clear_session_evolution_states (L2070-2073)。
func (mh *MessageHandler) clearSessionEvolutionStates(sessionID string) {
	mh.clearSessionEvolutionInProgress(sessionID)
	mh.clearPendingEvolutionApproval(sessionID)
	mh.popQueuedSupplementInput(sessionID)
}

// handleEvolutionChunk 处理 chunk 中的演进状态和审批事件，更新 Gateway 状态机。
//
// 对齐 Python _handle_evolution_chunk (L2021-2068)：
// 在 process_stream 和 handleAgentServerPush 两条路径中复用。
func (mh *MessageHandler) handleEvolutionChunk(chunk *schema.AgentResponseChunk, sessionID string, requestMetadata map[string]any) {
	if chunk.Payload == nil {
		return
	}

	eventType, _ := chunk.Payload["event_type"].(string)

	// 处理 evolution_status 事件
	if eventType == string(schema.EventTypeChatEvolutionStatus) {
		status, _ := chunk.Payload["status"].(string)
		status = strings.TrimSpace(strings.ToLower(status))
		switch status {
		case "start":
			mh.markSessionEvolutionInProgress(sessionID)
			logger.Info(logComponent).
				Str("event_type", "evolution_status_start").
				Str("session_id", sessionID).
				Str("request_id", chunk.RequestID).
				Msg("evolution status start")
		case "end":
			mh.clearSessionEvolutionInProgress(sessionID)
			logger.Info(logComponent).
				Str("event_type", "evolution_status_end").
				Str("session_id", sessionID).
				Str("request_id", chunk.RequestID).
				Msg("evolution status end")
		}
	}

	// 处理 ask_user_question 中的 evolution 审批
	approvalRequestID, _ := chunk.Payload["request_id"].(string)
	if eventType == string(schema.EventTypeChatAskUserQuestion) && IsEvolutionApprovalRequestID(approvalRequestID) {
		mh.maybeAutoAcceptReplacedEvolutionApproval(sessionID, approvalRequestID, chunk.ChannelID, requestMetadata)
		mh.markPendingEvolutionApproval(sessionID, approvalRequestID)
		logger.Info(logComponent).
			Str("event_type", "evolution_approval_detected").
			Str("session_id", sessionID).
			Str("request_id", approvalRequestID).
			Msg("evolution approval detected")
	}
}

// buildAutoAcceptEvolutionAnswer 构造自动接受 evolution 审批的回答消息。
//
// 对齐 Python _build_auto_accept_evolution_answer (L1924-1949)。
func (mh *MessageHandler) buildAutoAcceptEvolutionAnswer(channelID, sessionID, requestID string, metadata map[string]any) *schema.Message {
	id := fmt.Sprintf("auto_evolve_answer_%x_%s", time.Now().UnixMilli(), generateRandomHex(3))
	params := map[string]any{
		"request_id": requestID,
		"answers":    []map[string]any{{"selected_options": []string{"接收"}}},
		"source":     "auto_accept",
	}
	paramsJSON, _ := json.Marshal(params)

	return &schema.Message{
		ID:        id,
		Type:      schema.MessageTypeReq,
		ChannelID: channelID,
		SessionID: sessionID,
		Params:    json.RawMessage(paramsJSON),
		Timestamp: float64(time.Now().UnixMilli()) / 1000.0,
		OK:        true,
		ReqMethod: schema.ReqMethodChatAnswer,
		IsStream:  false,
		Metadata:  metadata,
	}
}

// maybeAutoAcceptReplacedEvolutionApproval 如果新的 evolution 审批替换了旧的，自动接受旧的。
//
// 对齐 Python _maybe_auto_accept_replaced_evolution_approval (L1951-1978)：
// 当同一 session 有新的 evolution 审批进来时，自动接受之前 pending 的旧审批。
func (mh *MessageHandler) maybeAutoAcceptReplacedEvolutionApproval(sessionID, incomingRequestID, channelID string, metadata map[string]any) {
	if sessionID == "" || incomingRequestID == "" {
		return
	}

	mh.evolutionMu.RLock()
	previousRequestID, exists := mh.pendingEvolutionApproval[sessionID]
	mh.evolutionMu.RUnlock()

	if !exists || previousRequestID == incomingRequestID {
		return
	}

	autoAnswer := mh.buildAutoAcceptEvolutionAnswer(channelID, sessionID, previousRequestID, metadata)
	mh.PublishUserMessagesNowait(autoAnswer)
	logger.Info(logComponent).
		Str("event_type", "auto_accept_superseded_evolution").
		Str("session_id", sessionID).
		Str("old_request_id", previousRequestID).
		Str("new_request_id", incomingRequestID).
		Msg("自动接受被替换的 evolution 审批")
}
