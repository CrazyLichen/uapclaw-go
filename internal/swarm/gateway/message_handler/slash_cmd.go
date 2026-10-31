package message_handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/sessionops"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler/command_parser"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleChannelControl 处理 slash 命令，返回 true 表示已处理
//
// Python: _handle_channel_control (L615-883)，顺序严格对齐：
//
//	L626 渠道白名单 → L630 取 text → L634 解析 → L639 记 info
//	→ L645 SKILLS_OK 提前 return（不建 state）→ L652 建 state → 分发
//
// D1（有意偏差）：保留 Type/ReqMethod 双重前置守卫。Python 的三个发布方
// （web publish_user_messages、cron、evolution autoAnswer）均发 type=req，
// slash 文本只出现在 query/content，守卫等价且省一次解析。
func (mh *MessageHandler) handleChannelControl(msg *schema.Message) bool {
	// 仅处理请求消息
	if msg.Type != schema.MessageTypeReq {
		return false
	}

	// 仅处理 chat.send 方法
	if msg.ReqMethod != schema.ReqMethodChatSend {
		return false
	}

	// Python: L626 先判渠道白名单，非受控渠道不解析
	channelType := mh.resolveControlChannelType(msg)
	if !controlChannelTypes[string(channelType)] {
		return false
	}

	// Python: L630 params.get("query") or params.get("content") or ""
	text := extractTextFromParams(msg.Params)
	if text == "" {
		return false
	}

	// Python: L634 解析
	parsed := command_parser.ParseChannelControlText(text)
	if parsed.Action == command_parser.ActionNone {
		return false
	}

	// Python: L637-642 解析成功后立即记录（分发之前）
	logger.Info(logComponent).
		Str("event_type", "channel_control_parsed").
		Str("channel_type", string(channelType)).
		Str("text", text).
		Str("action", command_parser.ParsedControlActionString(parsed.Action)).
		Msg("slash command parsed")

	// Python: L645-648 /skills list 走独立路径，且不创建 channel state
	if parsed.Action == command_parser.ActionSkillsOK {
		mh.skillsSlashNotice(msg)
		logControlHandled(msg, channelType, parsed)
		return true
	}

	// Python: L652 之后才创建 channel state
	state := mh.GetOrCreateChannelState(msg)

	// 按 action 分发
	switch parsed.Action {
	case command_parser.ActionNewSessionOK:
		// 11.7+11.13：SessionMap 轮转 + state 更新 + SessionStart hook
		// Python: L654-667（全部在入口函数体内，收尾甩子函数）
		oldSID := state.SessionID
		var newSID string
		if p, c, b, u, ok := extractIdentityTuple(msg); ok && channelIDMatchesSessionMapTypes(msg.ChannelID) {
			newSID = mh.sessionMap.GetSessionID(p, c, b, u, true) // rotate=true
		} else {
			newSID = GenerateChannelSessionID(string(channelType))
		}
		state.SessionID = newSID
		// 触发 SessionStart hook（fire-and-forget）
		// Python: if self._gateway_hook_handler: asyncio.create_task(on_session_start(new_sid, source=channel_type))
		mh.TriggerSessionStartHook(newSID, string(channelType))
		// 收尾：异步取消旧会话 + 发变更通知
		mh.newSessionCancelAndNotice(msg, oldSID, newSID)
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

	logControlHandled(msg, channelType, parsed)
	return true
}

// newSessionCancelAndNotice /new_session 收尾：取消旧会话 + 发变更通知。
//
// Python: _new_session_cancel_and_notice (L575-591)：await 先取消完再发通知。
// M-05: Go 原实现 go 异步导致取消未完成通知先到（顺序丢失），改同步对齐。
// 状态更新（SessionMap 轮转/state.SessionID/hook）已上提至 handleChannelControl（D5 对齐）。
func (mh *MessageHandler) newSessionCancelAndNotice(msg *schema.Message, oldSID, newSID string) {
	// 取消旧会话（静默模式：publishInterruptResult=false）
	if oldSID != "" {
		mh.CancelAgentWorkForSession(context.Background(), msg, oldSID, false)
	}

	mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 CLI 指令], session_id 已变更为 %s", newSID)})
}

// modeChangeCancelAndNotice 处理 /mode 和 /switch 命令。
//
// Python: _mode_change_cancel_and_notice (L593-613)：
// 先取消当前会话任务（publishInterruptResult=false），再下发 mode 已变更提示。
func (mh *MessageHandler) modeChangeCancelAndNotice(msg *schema.Message, parsed command_parser.ParsedChannelControl) {
	// 提前获取渠道状态（/switch 需要读取当前 mode）
	state := mh.GetOrCreateChannelState(msg)

	// 确定新模式
	var newMode ChannelMode
	switch parsed.Action {
	case command_parser.ActionModeOK:
		// S-01: 对齐 Python L715-730 的 8 路一级映射。
		// 原实现直接 ParseChannelMode(mode_subcommand)，但 "agent"/"code"/"team"
		// 三个一级值不在 ParseChannelMode 词表内会回落 AgentPlan，导致
		// /mode code 误映射 AGENT_PLAN、/mode team 误映射 AGENT_PLAN。
		switch parsed.ModeSubcommand {
		case "agent":
			newMode = ChannelModeAgentPlan
		case "code":
			newMode = ChannelModeCodeNormal
		case "team":
			newMode = ChannelModeTeam
		default:
			// 二级值（agent.plan/agent.fast/code.plan/code.normal/code.team）
			newMode = ParseChannelMode(parsed.ModeSubcommand)
		}
	case command_parser.ActionSwitchOK:
		// Python: 根据当前 state.Mode 判断模式家族，再决定目标模式
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
		// M-01: 对齐 Python L734-743——_mode_change_cancel_and_notice 内部
		// 无 hasActiveStreamTaskForSession 守卫，只要 old_sid 非空就取消
		// （AgentServer 侧可能有网关不可见的派发工作）。
		// M-05: Python 为 await 同步调用（先取消完再发通知）；Go 原实现 go 异步
		// 导致取消未完成通知先到。CancelAgentWorkForSession 内部 SendRequest
		// 有超时不会永久阻塞 forwardLoop。
		if oldSID != "" {
			mh.CancelAgentWorkForSession(context.Background(), msg, oldSID, false)
		}
		mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 CLI 指令], mode 已变更为 %s", modeLabel)})
	} else {
		// 模式未变更：仅通知
		mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 CLI 指令], mode 已变更为 %s", modeLabel)})
	}
}

// logControlHandled 记录 channel_control_handled 兼容事件。
//
// Python 无此事件（Python 只在解析后记一条 info）。Go 侧历史上用该事件名做
// 排障检索，故保留：字段与旧实现一致（action 整型 + channel_id），另补
// action_name/channel_type 便于阅读。删掉它会断外部日志看板。
func logControlHandled(msg *schema.Message, channelType channel_manager.ChannelType, parsed command_parser.ParsedChannelControl) {
	logger.Info(logComponent).
		Str("event_type", "channel_control_handled").
		Int("action", int(parsed.Action)).
		Str("action_name", command_parser.ParsedControlActionString(parsed.Action)).
		Str("channel_id", msg.ChannelID).
		Str("channel_type", string(channelType)).
		Msg("slash command handled")
}

// sendChannelNotice 发送渠道通知消息。
//
// Python: _send_channel_notice (L347-379)：
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
		Params:          json.RawMessage(`{}`),
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
// Python: _skills_slash_notice (L885-937)：
// 构造 SKILLS_LIST E2A → AgentClient.SendRequest → 格式化通知。
func (mh *MessageHandler) skillsSlashNotice(msg *schema.Message) {
	if mh.agentClient == nil || !mh.agentClient.IsConnected() {
		mh.sendChannelNotice(msg, map[string]any{"error": "获取技能列表失败：AgentClient 未连接"})
		return
	}

	// T-08: 对齐 Python _skills_slash_notice——req_id 带 _{token_hex(3)}
	// 随机后缀（避免毫秒内并发请求 req_id 冲突）
	reqID := fmt.Sprintf("skills_slash_%x_%s", time.Now().UnixMilli(), generateRandomHex(3))
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
			Msg("/skills list request failed")
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
// Python: _branch_slash_notice (L939-1015)：
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

	// M-02: 对齐 Python L962-963——先用 _resolve_control_channel_type 解析渠道类型，
	// 再以此生成 new_sid（原实现直接用 msg.ChannelID 裸字符串）
	channelType := mh.resolveControlChannelType(msg)
	newSID := GenerateChannelSessionID(string(channelType))

	env := e2a.E2AFromAgentFields(
		fmt.Sprintf("branch-%x-%s", time.Now().UnixMilli(), generateRandomHex(3)),
		// M-02: Python L971 channel_id=channel_type（非裸 channel_id）
		e2a.WithFieldChannelID(string(channelType)),
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
			Msg("/branch request failed")
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
		// A2: Python L1005-1009 的 except ValueError 分支（发通知 + 无 exception 日志）
		logger.Warn(logComponent).
			Str("event_type", "branch_slash_rejected").
			Str("session_id", sourceSID).
			Msgf("/branch rejected: %s", errMsg)
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

	// A1: Python L994 `await self._cancel_agent_work_for_session(msg, old_sid)`
	// （publish_interrupt_result 取默认 True）。原实现用 go 异步，通知会先于
	// 取消完成发出，与 Python 顺序相反（M-05 同类问题，/branch 当初漏改）。
	if oldSID != "" {
		mh.CancelAgentWorkForSession(context.Background(), msg, oldSID, true)
	}

	// A2: Python L1001-1004 成功日志
	logger.Info(logComponent).
		Str("event_type", "branch_slash_done").
		Str("source_session_id", sourceSID).
		Str("fork_session_id", forkSID).
		Str("title", forkTitle).
		Msg("/branch completed")

	mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 /branch 指令] 已分叉会话「%s」，当前已切换到新会话。", forkTitle)})
}

// rewindSlashConfirmPrompt 发送 /rewind 确认提示。
//
// Python: _rewind_slash_confirm_prompt (L1017-1045)：
// 两步确认：先发送确认提示，不立即执行。
func (mh *MessageHandler) rewindSlashConfirmPrompt(msg *schema.Message, parsed command_parser.ParsedChannelControl) {
	turn := parsed.RewindTurn
	if turn == 0 {
		turn = 1
	}
	// M-03: 逐字复制 Python L1046-1050 四行模板（不可逆警告/删除范围/
	// confirm 格式/bash 文件影响说明），原实现压缩成单行丢失关键信息
	content := fmt.Sprintf(
		"[收到 /rewind %d 指令] 确认要回退到第 %d 轮吗？\n"+
			"此操作不可逆，将删除第 %d 轮及之后的所有对话。\n"+
			"请回复 /rewind confirm %d 确认，或 /rewind cancel 取消。\n"+
			"注意：回退不影响手动编辑的文件或通过 bash 执行的命令。",
		turn, turn, turn, turn)
	mh.sendChannelNotice(msg, map[string]any{"content": content})
}

// rewindSlashNotice 处理 /rewind 命令（用户已确认）。
//
// Python: _rewind_slash_notice (L1047-1142)：
// E2A-first + fallback：构造 SESSION_REWIND E2A → AgentClient.SendRequest；
// 失败/错误时 fallback 本地 sessionops.RewindSession 截断 history.json，
// 通知带「（注意：上下文未同步截断）」。
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

	// 先取消当前任务（Python: await 同步调用）
	mh.CancelAgentWorkForSession(context.Background(), msg, targetSID, false)

	// E2A-first：转发到 AgentServer（原子性截断 history + context + checkpointer）
	// AgentClient 未连接时直接走本地 fallback
	if mh.agentClient != nil && mh.agentClient.IsConnected() {
		env := e2a.E2AFromAgentFields(
			fmt.Sprintf("rewind-%x-%s", time.Now().UnixMilli(), generateRandomHex(3)),
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
			// Python: logger.warning + fall through to local fallback
			logger.Warn(logComponent).
				Str("event_type", "rewind_e2a_error").
				Err(err).
				Str("session_id", targetSID).
				Msg("/rewind E2A failed, fallback to local rewind")
		} else if resp.OK {
			// E2A 成功路径
			preview := ""
			remaining := 0
			removed := 0
			rewindContext := false
			if resp.Payload != nil {
				if p, ok := resp.Payload["content_preview"].(string); ok {
					// M-07: Python preview[:50] 按字符切，Go 按 rune 对齐口径，
					// 否则 CJK 多字节字符在字节边界截断产生非法 UTF-8
					preview = truncRunes(p, 50)
				}
				remaining = payloadToInt(resp.Payload["remaining_records"])
				removed = payloadToInt(resp.Payload["removed_records"])
				// A6: Python L1096 读取 rewind_context（仅用于日志）
				rewindContext, _ = resp.Payload["rewind_context"].(bool)
			}
			// A6: Python L1104-1107 成功日志
			logger.Info(logComponent).
				Str("event_type", "rewind_slash_done_e2a").
				Str("session_id", targetSID).
				Int("turn_index", turn).
				Bool("rewind_context", rewindContext).
				Msg("/rewind completed via E2A")
			mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 /rewind 指令] 已回退到第 %d 轮（\"%s\"），删除 %d 条记录，剩余 %d 条。", turn, preview, removed, remaining)})
			return
		} else {
			// AgentServer 返回错误 — fall through to local fallback
			logger.Warn(logComponent).
				Str("event_type", "rewind_e2a_rejected").
				Str("session_id", targetSID).
				Msgf("/rewind E2A failed, fallback to local rewind: %v", resp.Payload)
		}
	}

	// M-04: Fallback——本地截断 history.json（对齐 Python rewind_session 本地兜底）
	result, err := sessionops.RewindSession(sessionops.RewindSessionParams{
		SessionID: targetSID,
		TurnIndex: turn,
	})
	if err != nil {
		mh.sendChannelNotice(msg, map[string]any{"error": fmt.Sprintf("回退失败：%v", err)})
		return
	}
	// A5: Python L1117 只取 result["content_preview"]，无兜底。
	// 原实现在为空时回填 result.Content，属自造行为（比 Python 更"友好"但不对齐），已删除。
	preview := result.ContentPreview
	// A6: Python L1128-1131 fallback 成功日志
	logger.Info(logComponent).
		Str("event_type", "rewind_slash_done_local").
		Str("session_id", targetSID).
		Int("turn_index", turn).
		Int("remaining_records", result.RemainingRecords).
		Int("removed_records", result.RemovedRecords).
		Msg("/rewind completed via local fallback")
	mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf(
		"[收到 /rewind 指令] 已回退到第 %d 轮（\"%s\"），删除 %d 条记录，剩余 %d 条。（注意：上下文未同步截断）",
		turn, preview, result.RemovedRecords, result.RemainingRecords)})
}

// truncRunes 按字符（rune）截断字符串。
// Python s[:n] 语义对齐：CJK 多字节字符不会在字节中间截断。
func truncRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// payloadToInt 从 JSON 反序列化 payload 中宽容提取 int。
// S-06: 跨进程 JSON 数值必为 float64（agent_client.go json.Unmarshal），
// 直接 .(int) 断言永不命中导致字段恒为 0。
func payloadToInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
		if f, err := n.Float64(); err == nil {
			return int(f)
		}
	}
	return 0
}

// extractTextFromParams 从消息 params 中提取文本内容。
//
// Python: params.get("query") or params.get("content") or ""
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
