package message_handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler/command_parser"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleChannelControl 处理 slash 命令，返回 true 表示已处理
//
// 对齐 Python _handle_channel_control (L615-883)：
// 调用 command_parser.ParseChannelControlText，按 action 分发处理。
// Web 渠道不在受控类型中，直接返回 false。
func (mh *MessageHandler) handleChannelControl(msg *schema.Message) bool {
	// 仅处理请求消息
	if msg.Type != schema.MessageTypeReq {
		return false
	}

	// 仅处理 chat.send 方法
	if msg.ReqMethod != schema.ReqMethodChatSend {
		return false
	}

	// 从 params 提取文本内容
	text := extractTextFromParams(msg.Params)
	if text == "" {
		return false
	}

	// 解析控制指令
	parsed := command_parser.ParseChannelControlText(text)
	if parsed.Action == command_parser.ActionNone {
		return false
	}

	// 获取渠道类型
	channelType := mh.resolveControlChannelType(msg)
	if !controlChannelTypes[string(channelType)] {
		// 非受控渠道不处理 slash 命令
		return false
	}

	// 按 action 分发
	switch parsed.Action {
	case command_parser.ActionNewSessionOK:
		mh.newSessionCancelAndNotice(msg, parsed)
	case command_parser.ActionNewSessionBad:
		mh.sendChannelNotice(msg, map[string]any{"content": "非法指令"})
	case command_parser.ActionModeOK:
		mh.modeChangeCancelAndNotice(msg, parsed)
	case command_parser.ActionModeBad:
		mh.sendChannelNotice(msg, map[string]any{"content": "非法指令"})
	case command_parser.ActionSwitchOK:
		mh.modeChangeCancelAndNotice(msg, parsed)
	case command_parser.ActionSwitchBad:
		mh.sendChannelNotice(msg, map[string]any{"content": "非法指令"})
	case command_parser.ActionSkillsOK:
		mh.skillsSlashNotice(msg)
	case command_parser.ActionBranchOK:
		mh.branchSlashNotice(msg, parsed)
	case command_parser.ActionRewindOK:
		mh.rewindSlashConfirmPrompt(msg, parsed)
	case command_parser.ActionRewindBad:
		mh.sendChannelNotice(msg, map[string]any{"content": "非法指令，/rewind 须带正整数轮次编号，如 /rewind 2"})
	case command_parser.ActionRewindConfirm:
		mh.rewindSlashNotice(msg, parsed)
	case command_parser.ActionRewindCancel:
		mh.sendChannelNotice(msg, map[string]any{"content": "[收到 /rewind cancel] 已取消回退操作。"})
	default:
		return false
	}

	logger.Info(logComponent).
		Str("event_type", "channel_control_handled").
		Int("action", int(parsed.Action)).
		Str("channel_id", msg.ChannelID).
		Msg("slash 命令已处理")
	return true
}

// newSessionCancelAndNotice 处理 /new_session 命令。
//
// 对齐 Python _new_session_cancel_and_notice (L575-591)：
// 先更新 state 再异步取消旧会话（publishInterruptResult=false），最后发通知。
func (mh *MessageHandler) newSessionCancelAndNotice(msg *schema.Message, _ command_parser.ParsedChannelControl) {
	state := mh.GetOrCreateChannelState(msg)
	oldSID := state.SessionID

	// 生成新 session_id
	newSID := GenerateChannelSessionID(msg.ChannelID)

	// 先更新 state（对齐 Python：先更新 state.session_id 再 cancel）
	state.SessionID = newSID

	// TODO: SessionMap 集成 + triggerSessionStartHook（等 11.7 + 11.13 回填）

	// 异步取消旧会话（静默模式：publishInterruptResult=false）
	if oldSID != "" {
		go mh.CancelAgentWorkForSession(context.Background(), msg, oldSID, false)
	}

	mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 CLI 指令], session_id 已变更为 %s", newSID)})
}

// modeChangeCancelAndNotice 处理 /mode 和 /switch 命令。
//
// 对齐 Python _mode_change_cancel_and_notice (L593-613)：
// 先取消当前会话任务（publishInterruptResult=false），再下发 mode 已变更提示。
func (mh *MessageHandler) modeChangeCancelAndNotice(msg *schema.Message, parsed command_parser.ParsedChannelControl) {
	// 提前获取渠道状态（/switch 需要读取当前 mode）
	state := mh.GetOrCreateChannelState(msg)

	// 确定新模式
	var newMode ChannelMode
	switch parsed.Action {
	case command_parser.ActionModeOK:
		newMode = ParseChannelMode(parsed.ModeSubcommand)
	case command_parser.ActionSwitchOK:
		// 对齐 Python：根据当前 state.Mode 判断模式家族，再决定目标模式
		currentMode := state.Mode
		switch parsed.SwitchSubcommand {
		case "plan":
			switch currentMode {
			case ChannelModeAgentPlan, ChannelModeAgentFast:
				newMode = ChannelModeAgentPlan
			case ChannelModeCodePlan, ChannelModeCodeNormal, ChannelModeCodeTeam:
				newMode = ChannelModeCodePlan
			default:
				// 当前模式不在任何支持 /switch plan 的家族中，发送"非法指令"通知
				mh.sendChannelNotice(msg, map[string]any{"content": "非法指令"})
				return
			}
		case "fast":
			switch currentMode {
			case ChannelModeAgentPlan, ChannelModeAgentFast:
				newMode = ChannelModeAgentFast
			default:
				mh.sendChannelNotice(msg, map[string]any{"content": "非法指令"})
				return
			}
		case "normal":
			switch currentMode {
			case ChannelModeCodePlan, ChannelModeCodeNormal, ChannelModeCodeTeam:
				newMode = ChannelModeCodeNormal
			default:
				mh.sendChannelNotice(msg, map[string]any{"content": "非法指令"})
				return
			}
		case "team":
			switch currentMode {
			case ChannelModeCodePlan, ChannelModeCodeNormal, ChannelModeCodeTeam:
				newMode = ChannelModeCodeTeam
			default:
				mh.sendChannelNotice(msg, map[string]any{"content": "非法指令"})
				return
			}
		default:
			mh.sendChannelNotice(msg, map[string]any{"content": "非法指令"})
			return
		}
	default:
		return
	}

	// 更新渠道状态
	oldMode := state.Mode
	oldSID := state.SessionID
	state.Mode = newMode

	modeLabel := ChannelModeString(newMode)

	if oldMode != newMode {
		// 模式确实变更：先取消当前会话任务（静默模式）
		if oldSID != "" && mh.hasActiveStreamTaskForSession(oldSID) {
			go mh.CancelAgentWorkForSession(context.Background(), msg, oldSID, false)
		}
		mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 CLI 指令], mode 已变更为 %s", modeLabel)})
	} else {
		// 模式未变更：仅通知
		mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 CLI 指令], mode 已变更为 %s", modeLabel)})
	}
}

// sendChannelNotice 发送渠道通知消息。
//
// 对齐 Python _send_channel_notice (L347-379)：
// event_type 为 CHAT_FINAL，payload 确保补齐 is_complete: true。
// 传 string 时调用方应包装为 {"content": text}，此处仅接受 map[string]any。
func (mh *MessageHandler) sendChannelNotice(msg *schema.Message, payload map[string]any) {
	// 确保 is_complete 字段存在
	if _, has := payload["is_complete"]; !has {
		payload["is_complete"] = true
	}

	noticeMsg := &schema.Message{
		ID:              msg.ID,
		Type:            schema.MessageTypeEvent,
		ChannelID:       msg.ChannelID,
		SessionID:       msg.SessionID,
		Timestamp:       schema.NowTimestamp(),
		OK:              true,
		EventType:       schema.EventTypeChatFinal,
		Payload:         payload,
		Metadata:        msg.Metadata,
		EnableStreaming: true,
	}
	mh.PublishRobotMessages(noticeMsg)
}

// skillsSlashNotice 处理 /skills list 命令。
//
// 对齐 Python _skills_slash_notice (L885-937)：
// 构造 SKILLS_LIST E2A → AgentClient.SendRequest → 格式化通知。
func (mh *MessageHandler) skillsSlashNotice(msg *schema.Message) {
	if mh.agentClient == nil || !mh.agentClient.IsConnected() {
		mh.sendChannelNotice(msg, map[string]any{"error": "获取技能列表失败：AgentClient 未连接"})
		return
	}

	reqID := fmt.Sprintf("skills_slash_%x", time.Now().UnixMilli())
	skillsReq := &schema.Message{
		ID:        reqID,
		Type:      schema.MessageTypeReq,
		ChannelID: msg.ChannelID,
		SessionID: msg.SessionID,
		Params:    json.RawMessage(`{}`),
		Timestamp: float64(time.Now().UnixMilli()) / 1000.0,
		OK:        true,
		ReqMethod: schema.ReqMethodSkillsList,
		IsStream:  false,
		Metadata:  msg.Metadata,
		Provider:  msg.Provider,
		ChatID:    msg.ChatID,
		UserID:    msg.UserID,
		BotID:     msg.BotID,
	}

	env := e2a.MessageToE2AOrFallback(skillsReq)
	env.IsStream = false

	resp, err := mh.agentClient.SendRequest(context.Background(), env)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "skills_slash_error").
			Err(err).
			Msg("/skills list 请求失败")
		mh.sendChannelNotice(msg, map[string]any{"error": fmt.Sprintf("获取技能列表失败：%v", err)})
		return
	}

	if resp.OK {
		var noticePayload map[string]any
		if resp.Payload != nil {
			noticePayload = make(map[string]any, len(resp.Payload))
			for k, v := range resp.Payload {
				noticePayload[k] = v
			}
		} else {
			noticePayload = map[string]any{"data": nil}
		}
		mh.sendChannelNotice(msg, noticePayload)
	} else {
		errMsg := ""
		if resp.Payload != nil {
			if e, ok := resp.Payload["error"].(string); ok {
				errMsg = e
			}
		}
		if errMsg != "" {
			mh.sendChannelNotice(msg, map[string]any{"error": fmt.Sprintf("获取技能列表失败: %s", errMsg)})
		} else {
			mh.sendChannelNotice(msg, map[string]any{"error": "获取技能列表失败"})
		}
	}
}

// branchSlashNotice 处理 /branch 命令。
//
// 对齐 Python _branch_slash_notice (L939-1015)：
// 构造 SESSION_FORK E2A → AgentClient.SendRequest → 格式化通知。
func (mh *MessageHandler) branchSlashNotice(msg *schema.Message, parsed command_parser.ParsedChannelControl) {
	state := mh.GetOrCreateChannelState(msg)
	sourceSID := state.SessionID
	if sourceSID == "" {
		mh.sendChannelNotice(msg, map[string]any{"error": "当前无活跃会话，无法分叉"})
		return
	}

	branchName := parsed.BranchName
	if branchName == "" {
		branchName = "Branched conversation"
	}

	if mh.agentClient == nil || !mh.agentClient.IsConnected() {
		mh.sendChannelNotice(msg, map[string]any{"error": "分叉失败：AgentClient 未连接"})
		return
	}

	newSID := GenerateChannelSessionID(msg.ChannelID)

	env := e2a.E2AFromAgentFields(
		fmt.Sprintf("branch-%x", time.Now().UnixMilli()),
		e2a.WithFieldChannelID(msg.ChannelID),
		e2a.WithFieldSessionID(sourceSID),
		e2a.WithFieldReqMethod(string(schema.ReqMethodSessionFork)),
		e2a.WithFieldParams(map[string]any{
			"source_session_id": sourceSID,
			"target_session_id": newSID,
			"title":             branchName,
		}),
		e2a.WithFieldIsStream(false),
		e2a.WithFieldTimestamp(float64(time.Now().UnixMilli())/1000.0),
	)

	resp, err := mh.agentClient.SendRequest(context.Background(), env)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "branch_slash_error").
			Err(err).
			Msg("/branch 请求失败")
		mh.sendChannelNotice(msg, map[string]any{"error": fmt.Sprintf("分叉失败：%v", err)})
		return
	}

	if !resp.OK {
		errMsg := "session.fork failed"
		if resp.Payload != nil {
			if e, ok := resp.Payload["error"].(string); ok && e != "" {
				errMsg = e
			}
		}
		mh.sendChannelNotice(msg, map[string]any{"error": fmt.Sprintf("分叉失败：%s", errMsg)})
		return
	}

	forkSID := newSID
	forkTitle := branchName
	if resp.Payload != nil {
		if sid, ok := resp.Payload["session_id"].(string); ok && sid != "" {
			forkSID = sid
		}
		if t, ok := resp.Payload["title"].(string); ok && t != "" {
			forkTitle = t
		}
	}

	oldSID := state.SessionID
	state.SessionID = forkSID

	// 取消旧会话
	if oldSID != "" {
		go mh.CancelAgentWorkForSession(context.Background(), msg, oldSID, true)
	}

	mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 /branch 指令] 已分叉会话「%s」，当前已切换到新会话。", forkTitle)})
}

// rewindSlashConfirmPrompt 发送 /rewind 确认提示。
//
// 对齐 Python _rewind_slash_confirm_prompt (L1017-1045)：
// 两步确认：先发送确认提示，不立即执行。
func (mh *MessageHandler) rewindSlashConfirmPrompt(msg *schema.Message, parsed command_parser.ParsedChannelControl) {
	turn := parsed.RewindTurn
	if turn == 0 {
		turn = 1
	}
	mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 /rewind %d] 确认回退到第 %d 轮？发送 /rewind confirm 确认，/rewind cancel 取消。", turn, turn)})
}

// rewindSlashNotice 处理 /rewind 命令（用户已确认）。
//
// 对齐 Python _rewind_slash_notice (L1047-1142)：
// E2A-first + fallback：构造 SESSION_REWIND E2A → AgentClient.SendRequest。
func (mh *MessageHandler) rewindSlashNotice(msg *schema.Message, parsed command_parser.ParsedChannelControl) {
	state := mh.GetOrCreateChannelState(msg)
	targetSID := state.SessionID
	if targetSID == "" {
		mh.sendChannelNotice(msg, map[string]any{"error": "当前无活跃会话，无法回退"})
		return
	}

	turn := parsed.RewindTurn
	if turn == 0 {
		turn = 1
	}

	// 先取消当前任务
	mh.CancelAgentWorkForSession(context.Background(), msg, targetSID, false)

	if mh.agentClient == nil || !mh.agentClient.IsConnected() {
		mh.sendChannelNotice(msg, map[string]any{"error": "回退失败：AgentClient 未连接"})
		return
	}

	// E2A-first：转发到 AgentServer
	env := e2a.E2AFromAgentFields(
		fmt.Sprintf("rewind-%x", time.Now().UnixMilli()),
		e2a.WithFieldChannelID(msg.ChannelID),
		e2a.WithFieldSessionID(targetSID),
		e2a.WithFieldReqMethod(string(schema.ReqMethodSessionRewind)),
		e2a.WithFieldParams(map[string]any{
			"session_id": targetSID,
			"turn_index": turn,
		}),
		e2a.WithFieldIsStream(false),
		e2a.WithFieldTimestamp(float64(time.Now().UnixMilli())/1000.0),
	)

	resp, err := mh.agentClient.SendRequest(context.Background(), env)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "rewind_e2a_error").
			Err(err).
			Msg("/rewind E2A 请求失败")
		mh.sendChannelNotice(msg, map[string]any{"error": fmt.Sprintf("回退失败：%v", err)})
		return
	}

	if resp.OK {
		preview := ""
		remaining := 0
		removed := 0
		if resp.Payload != nil {
			if p, ok := resp.Payload["content_preview"].(string); ok {
				if len(p) > 50 {
					preview = p[:50]
				} else {
					preview = p
				}
			}
			if r, ok := resp.Payload["remaining_records"]; ok {
				if n, isNum := r.(int); isNum {
					remaining = n
				}
			}
			if r, ok := resp.Payload["removed_records"]; ok {
				if n, isNum := r.(int); isNum {
					removed = n
				}
			}
		}
		mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 /rewind 指令] 已回退到第 %d 轮（\"%s\"），删除 %d 条记录，剩余 %d 条。", turn, preview, removed, remaining)})
	} else {
		errMsg := "session.rewind failed"
		if resp.Payload != nil {
			if e, ok := resp.Payload["error"].(string); ok && e != "" {
				errMsg = e
			}
		}
		mh.sendChannelNotice(msg, map[string]any{"error": fmt.Sprintf("回退失败：%s", errMsg)})
	}
}

// extractTextFromParams 从消息 params 中提取文本内容。
//
// 对齐 Python：params.get("query") or params.get("content") or ""
func extractTextFromParams(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var paramsMap map[string]any
	if err := json.Unmarshal(params, &paramsMap); err != nil {
		return ""
	}
	// 先检查 query 字段，再回退 content 字段
	if query, ok := paramsMap["query"]; ok {
		if s, isStr := query.(string); isStr && s != "" {
			return s
		}
	}
	if content, ok := paramsMap["content"]; ok {
		if s, isStr := content.(string); isStr {
			return s
		}
	}
	return ""
}
