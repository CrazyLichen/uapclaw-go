package message_handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/sessionops"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler/command_parser"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// userInfos 控制指令回包所需的路由身份（Python: _handle_channel_control 顶部就地构造的 dict）。
type userInfos struct {
	// ID 消息 ID（Python user_infos['id'] = msg.id）
	ID string
	// MetaData 消息元数据（Python user_infos['meta_data'] = msg.metadata）
	MetaData map[string]any
	// AppID 应用标识（Python app_id or bot_id or ""）
	AppID string
	// BotID 机器人标识（Python bot_id or ""）
	BotID string
}

// modeSwitchParams /mode 分发所需的具名参数（Python: handle_mode_switch 的关键字参数）。
type modeSwitchParams struct {
	// State 渠道控制状态（就地落定 mode）
	State *ChannelControlState
	// UserInfos 回包路由身份
	UserInfos userInfos
	// ChannelID 渠道标识
	ChannelID string
	// ReplySessionID 回包会话
	ReplySessionID string
	// Msg 入站消息（取消链路需要）
	Msg *schema.Message
}

// modeChangeCancelParams mode 切换取消并发通知所需的具名参数。
// Python: ModeChangeCancelParams（message_handler.py:297-305）
type modeChangeCancelParams struct {
	// UserInfos 回包路由身份
	UserInfos userInfos
	// ChannelID 渠道标识
	ChannelID string
	// ReplySessionID 回包会话
	ReplySessionID string
	// OldSID 旧会话（用于取消）
	OldSID string
	// NewModeLabel 新模式标签
	NewModeLabel string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// validModeInputs /mode 合法输入集合（25 项）。
// Python: _VALID_MODE_INPUTS = NEW_CANONICAL_MODES | DEPRECATION_MAP.keys() | MODE_ALIASES.keys()
// （message_handler.py:99-101）。逐项列全，避免与上游漂移。
var validModeInputs = map[string]bool{
	// NEW_CANONICAL_MODES（9）
	"agent.work.normal": true, "agent.work.plan": true,
	"agent.code.normal": true, "agent.code.plan": true,
	"team.work.normal": true, "team.work.plan": true,
	"team.code.normal": true, "team.code.plan": true,
	"team.design": true,
	// DEPRECATION_MAP（10）
	"agent": true, "agent.plan": true, "agent.fast": true,
	"code": true, "code.normal": true, "code.plan": true, "code.team": true,
	"team": true, "team.plan.normal": true, "team.plan.code": true,
	// MODE_ALIASES（6）
	"team.plan": true, "team.code": true, "team.work": true,
	"team.normal": true, "agent.work": true, "agent.code": true,
}

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
// S-02：各分支的 RPC/通知尾巴一律经 runSlashAsync 甩到独立协程（对齐 Python 每个
// 分支的 asyncio.create_task）。forwardLoop 是唯一消费 goroutine，同步执行会让
// /branch、/rewind、/skills 的 600s unary RPC 阻塞全渠道入站派发。
func (mh *MessageHandler) handleChannelControl(ctx context.Context, msg *schema.Message) bool {
	// Python: user_infos 在函数最顶部就地构造（L1987-1992）
	appID := msg.AppID
	if appID == "" {
		appID = msg.BotID
	}
	infos := userInfos{
		ID:       msg.ID,
		MetaData: msg.Metadata,
		AppID:    appID,
		BotID:    msg.BotID,
	}

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
		// Python: asyncio.create_task(_skills_slash_notice)
		mh.runSlashAsync(ctx, "skills", func(ctx context.Context) {
			mh.skillsSlashNotice(ctx, infos, msg)
		})
		logControlHandled(msg, channelType, parsed)
		return true
	}

	// Python: L652 之后才创建 channel state
	state := mh.GetOrCreateChannelState(msg)

	// 按 action 分发
	switch parsed.Action {
	case command_parser.ActionNewSessionOK:
		// Python: L2028-2060——先清空 session_id，再经 session.create 分配真实会话；
		// 失败时回滚 session_id 并下发「创建新会话失败」，不转发给 Agent。
		oldSID := state.SessionID
		state.SessionID = ""
		newSID, allocErr := mh.allocateChannelSession(ctx, msg, state, false)
		if allocErr != nil {
			state.SessionID = oldSID
			logger.Error(logComponent).
				Err(allocErr).
				Str("channel_id", msg.ChannelID).
				Str("msg_id", msg.ID).
				Msg("create new session failed")
			mh.runSlashAsync(ctx, "new_session_bad", func(context.Context) {
				mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, map[string]any{"error": "创建新会话失败，请稍后重试"})
			})
			logControlHandled(msg, channelType, parsed)
			return true
		}
		// 触发 SessionStart hook（fire-and-forget）
		// Python: if self._gateway_hook_handler: asyncio.create_task(on_session_start(new_sid, source=channel_type))
		mh.TriggerSessionStartHook(newSID, string(channelType))
		// 收尾：异步取消旧会话 + 发变更通知（Python: create_task）
		mh.runSlashAsync(ctx, "new_session", func(ctx context.Context) {
			mh.newSessionCancelAndNotice(ctx, infos, msg, oldSID, newSID)
		})
	case command_parser.ActionNewSessionBad:
		mh.runSlashAsync(ctx, "new_session_bad", func(context.Context) {
			mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, "非法指令")
		})
	case command_parser.ActionModeOK:
		// Python: return self.handle_mode_switch(...)（同步就地落定状态）
		mh.handleModeSwitch(ctx, parsed.ModeSubcommand, modeSwitchParams{
			State:          state,
			UserInfos:      infos,
			ChannelID:      msg.ChannelID,
			ReplySessionID: msg.SessionID,
			Msg:            msg,
		})
	case command_parser.ActionModeBad:
		mh.runSlashAsync(ctx, "mode_bad", func(context.Context) {
			mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, "非法指令")
		})
	case command_parser.ActionSwitchOK:
		// Python: SWITCH_OK 内联在 _handle_channel_control（L2163-2210）
		var targetMode ChannelMode
		switch parsed.SwitchSubcommand {
		case "plan":
			// agent 下 plan / fast 已合并：/switch plan 保持 agent.work.normal
			if agentWorkModes[string(state.Mode)] {
				targetMode = ChannelModeAgentWorkNormal
			} else if codeModes[string(state.Mode)] {
				targetMode = ChannelModeAgentCodePlan
			}
		case "fast":
			if agentWorkModes[string(state.Mode)] {
				targetMode = ChannelModeAgentWorkNormal
			}
		case "normal":
			if codeModes[string(state.Mode)] {
				targetMode = ChannelModeAgentCodeNormal
			}
		case "team":
			if codeModes[string(state.Mode)] {
				targetMode = ChannelModeTeamCodeNormal
			}
		}
		if targetMode == "" {
			mh.runSlashAsync(ctx, "switch_bad", func(context.Context) {
				mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, "非法指令")
			})
			logControlHandled(msg, channelType, parsed)
			return true
		}
		prevMode := state.Mode
		prevSID := state.SessionID
		state.Mode = targetMode
		label := string(state.Mode)
		if prevMode != state.Mode {
			mh.runSlashAsync(ctx, "switch", func(ctx context.Context) {
				mh.modeChangeCancelAndNotice(ctx, modeChangeCancelParams{
					UserInfos:      infos,
					ChannelID:      msg.ChannelID,
					ReplySessionID: msg.SessionID,
					OldSID:         prevSID,
					NewModeLabel:   label,
				}, msg)
			})
		} else {
			mh.runSlashAsync(ctx, "switch_noop", func(context.Context) {
				mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, buildModeChangeNoticeText(label))
			})
		}
	case command_parser.ActionSwitchBad:
		mh.runSlashAsync(ctx, "switch_bad", func(context.Context) {
			mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, "非法指令")
		})
	case command_parser.ActionBranchOK:
		mh.runSlashAsync(ctx, "branch", func(ctx context.Context) {
			mh.branchSlashNotice(ctx, infos, msg, parsed)
		})
	case command_parser.ActionRewindOK:
		// 单纯发确认提示（无 RPC），Python 侧同为 create_task，保持一致
		mh.runSlashAsync(ctx, "rewind_confirm_prompt", func(context.Context) {
			mh.rewindSlashConfirmPrompt(infos, msg, parsed)
		})
	case command_parser.ActionRewindBad:
		mh.runSlashAsync(ctx, "rewind_bad", func(context.Context) {
			mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, "非法指令，/rewind 须带正整数轮次编号，如 /rewind 2")
		})
	case command_parser.ActionRewindConfirm:
		mh.runSlashAsync(ctx, "rewind", func(ctx context.Context) {
			mh.rewindSlashNotice(ctx, infos, msg, parsed)
		})
	case command_parser.ActionRewindCancel:
		mh.runSlashAsync(ctx, "rewind_cancel", func(context.Context) {
			mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, "[收到 /rewind cancel] 已取消回退操作。")
		})
	default:
		return false
	}

	logControlHandled(msg, channelType, parsed)
	return true
}

// newSessionCancelAndNotice /new_session 收尾：取消旧会话 + 发变更通知。
// Python: _new_session_cancel_and_notice (L1862-1878)：先取消再通知。
// 状态更新（session.create 分配/hook）已上提至 handleChannelControl（对齐 L2028-2060）。
func (mh *MessageHandler) newSessionCancelAndNotice(ctx context.Context, infos userInfos, msg *schema.Message, oldSID, newSID string) {
	// 取消旧会话（静默模式：publishInterruptResult=false）
	if oldSID != "" {
		mh.CancelAgentWorkForSession(ctx, msg, oldSID, false)
	}

	mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID,
		fmt.Sprintf("[收到 CLI 指令], session_id 已变更为 %s", newSID))
}

// handleModeSwitch 校验并应用 /mode 指令切换运行模式。
// Python: handle_mode_switch (L1902-1982)，步骤逐条对齐：
//  1. 白名单校验（_VALID_MODE_INPUTS，25 项）：未命中 → Warn + 「非法指令」+ 返回
//  2. 记录旧状态 → state.mode = channel_mode_from_str(mode_str)
//  3. Info 日志（channel、old→new、old_sid）
//  4. 状态变化 → 取消旧会话 + 变更通知；未变化 → 仅变更通知
func (mh *MessageHandler) handleModeSwitch(ctx context.Context, modeStr string, params modeSwitchParams) {
	raw := strings.TrimSpace(strings.ToLower(modeStr))
	if !isValidModeInput(raw) {
		logger.Warn(logComponent).
			Str("mode_str", modeStr).
			Str("channel_id", params.ChannelID).
			Str("session_id", params.ReplySessionID).
			Msg("handle_mode_switch: invalid directive")
		mh.sendChannelNotice(params.UserInfos, params.ChannelID, params.ReplySessionID, "非法指令")
		return
	}
	oldMode := params.State.Mode
	oldSID := params.State.SessionID
	params.State.Mode = channelModeFromStr(modeStr)
	newLabel := string(params.State.Mode)
	logger.Info(logComponent).
		Str("channel_id", params.ChannelID).
		Str("mode_str", modeStr).
		Str("new_mode", newLabel).
		Str("old_sid", oldSID).
		Msg("handle_mode_switch: mode switched")
	if oldMode != params.State.Mode {
		mh.runSlashAsync(ctx, "mode", func(ctx context.Context) {
			mh.modeChangeCancelAndNotice(ctx, modeChangeCancelParams{
				UserInfos:      params.UserInfos,
				ChannelID:      params.ChannelID,
				ReplySessionID: params.ReplySessionID,
				OldSID:         oldSID,
				NewModeLabel:   newLabel,
			}, params.Msg)
		})
		return
	}
	mh.runSlashAsync(ctx, "mode_noop", func(context.Context) {
		mh.sendChannelNotice(params.UserInfos, params.ChannelID, params.ReplySessionID,
			buildModeChangeNoticeText(newLabel))
	})
}

// modeChangeCancelAndNotice 取消当前会话任务并下发 mode 变更提示。
// Python: _mode_change_cancel_and_notice (L1880-1896)：
// 先取消当前会话任务（publishInterruptResult=false），再下发 mode 已变更提示。
func (mh *MessageHandler) modeChangeCancelAndNotice(ctx context.Context, params modeChangeCancelParams, msg *schema.Message) {
	// M-01: Python 无 hasActiveStreamTaskForSession 守卫，只要 old_sid 非空就取消
	// （AgentServer 侧可能有网关不可见的派发工作）。
	if params.OldSID != "" {
		mh.CancelAgentWorkForSession(ctx, msg, params.OldSID, false)
	}
	mh.sendChannelNotice(params.UserInfos, params.ChannelID, params.ReplySessionID,
		buildModeChangeNoticeText(params.NewModeLabel))
}

// buildModeChangeNoticeText 构造 mode 变更提示文案。
// Python: _build_mode_change_notice_text (L1898-1900)
func buildModeChangeNoticeText(modeLabel string) string {
	return fmt.Sprintf("[收到 CLI 指令], mode 已变更为 %s", modeLabel)
}

// logControlHandled 记录 channel_control_handled 兼容事件。
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
// Python: _send_channel_notice (L347-379)：
// event_type 为 CHAT_FINAL，payload 确保补齐 is_complete: true。
// 传 string 时调用方应包装为 {"content": text}，此处仅接受 map[string]any。
func (mh *MessageHandler) sendChannelNotice(infos userInfos, channelID, sessionID string, textOrPayload any) {
	// Python: str → {"content": text, "is_complete": True}；dict → 副本 + setdefault
	var payload map[string]any
	switch v := textOrPayload.(type) {
	case map[string]any:
		payload = make(map[string]any, len(v)+1)
		for k, val := range v {
			payload[k] = val
		}
	case string:
		payload = map[string]any{"content": v}
	default:
		payload = map[string]any{"content": fmt.Sprint(v)}
	}
	if _, has := payload["is_complete"]; !has {
		payload["is_complete"] = true
	}

	noticeMsg := &schema.Message{
		ID:              infos.ID,
		Type:            schema.MessageTypeEvent,
		ChannelID:       channelID,
		SessionID:       sessionID,
		Params:          json.RawMessage(`{}`),
		Timestamp:       schema.NowTimestamp(),
		OK:              true,
		EventType:       schema.EventTypeChatFinal,
		Payload:         payload,
		Metadata:        infos.MetaData,
		AppID:           infos.AppID,
		EnableStreaming: true,
	}
	mh.PublishRobotMessages(noticeMsg)
}

// isValidModeInput 判断 /mode 入参是否在白名单内。
// Python: `if mode_str not in _VALID_MODE_INPUTS`（message_handler.py:1931）
func isValidModeInput(modeStr string) bool {
	return validModeInputs[modeStr]
}

// skillsSlashNotice 处理 /skills list 命令。
// Python: _skills_slash_notice (L885-937)：
// 构造 SKILLS_LIST E2A → AgentClient.SendRequest → 格式化通知。
func (mh *MessageHandler) skillsSlashNotice(ctx context.Context, infos userInfos, msg *schema.Message) {
	if mh.agentClient == nil || !mh.agentClient.IsConnected() {
		mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, map[string]any{"error": "获取技能列表失败：AgentClient 未连接"})
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

	resp, err := mh.agentClient.SendRequest(ctx, env)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "skills_slash_error").
			Err(err).
			Msg("/skills list request failed")
		mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, map[string]any{"error": fmt.Sprintf("获取技能列表失败：%v", err)})
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
		mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, noticePayload)
	} else {
		errMsg := ""
		if resp.Payload != nil {
			if e, ok := resp.Payload["error"].(string); ok {
				errMsg = e
			}
		}
		if errMsg != "" {
			mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, map[string]any{"error": fmt.Sprintf("获取技能列表失败: %s", errMsg)})
		} else {
			mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, map[string]any{"error": "获取技能列表失败"})
		}
	}
}

// branchSlashNotice 处理 /branch 命令。
// Python: _branch_slash_notice (L939-1015)：
// 构造 SESSION_FORK E2A → AgentClient.SendRequest → 格式化通知。
func (mh *MessageHandler) branchSlashNotice(ctx context.Context, infos userInfos, msg *schema.Message, parsed command_parser.ParsedChannelControl) {
	state := mh.GetOrCreateChannelState(msg)
	sourceSID := state.SessionID
	if sourceSID == "" {
		mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, map[string]any{"error": "当前无活跃会话，无法分叉"})
		return
	}

	// M-07: 请求侧保持用户原始输入（可为空串），默认标题只在通知展示时回退。
	// Python: branch_name = parsed.branch_name or ""（空串照发），
	// 服务端 session_ops 的规则是「title 非空直接用，否则由源会话标题派生 <标题> (Branch)」。
	// 原实现预填 "Branched conversation" 会让服务端派生规则失效、多个无名分叉互相重名。
	branchName := parsed.BranchName

	if mh.agentClient == nil || !mh.agentClient.IsConnected() {
		mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, map[string]any{"error": "分叉失败：AgentClient 未连接"})
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

	resp, err := mh.agentClient.SendRequest(ctx, env)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "branch_slash_error").
			Err(err).
			Msg("/branch request failed")
		mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, map[string]any{"error": fmt.Sprintf("分叉失败：%v", err)})
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
		mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, map[string]any{"error": fmt.Sprintf("分叉失败：%s", errMsg)})
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
	// M-07: 展示层回退（Python: branch_name or "Branched conversation"），
	// 请求参数与状态更新都不使用该默认值。
	displayTitle := forkTitle
	if displayTitle == "" {
		displayTitle = "Branched conversation"
	}

	oldSID := state.SessionID
	state.SessionID = forkSID

	// A1: Python L994 `await self._cancel_agent_work_for_session(msg, old_sid)`
	// （publish_interrupt_result 取默认 True）。原实现用 go 异步，通知会先于
	// 取消完成发出，与 Python 顺序相反（M-05 同类问题，/branch 当初漏改）。
	if oldSID != "" {
		mh.CancelAgentWorkForSession(ctx, msg, oldSID, true)
	}

	// A2: Python L1001-1004 成功日志
	logger.Info(logComponent).
		Str("event_type", "branch_slash_done").
		Str("source_session_id", sourceSID).
		Str("fork_session_id", forkSID).
		Str("title", forkTitle).
		Msg("/branch completed")

	mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, fmt.Sprintf("[收到 /branch 指令] 已分叉会话「%s」，当前已切换到新会话。", displayTitle))
}

// rewindSlashConfirmPrompt 发送 /rewind 确认提示。
// Python: _rewind_slash_confirm_prompt (L1017-1045)：
// 两步确认：先发送确认提示，不立即执行。
func (mh *MessageHandler) rewindSlashConfirmPrompt(infos userInfos, msg *schema.Message, parsed command_parser.ParsedChannelControl) {
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
	mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, content)
}

// rewindSlashNotice 处理 /rewind 命令（用户已确认）。
// Python: _rewind_slash_notice (L1047-1142)：
// E2A-first + fallback：构造 SESSION_REWIND E2A → AgentClient.SendRequest；
// 失败/错误时 fallback 本地 sessionops.RewindSession 截断 history.json，
// 通知带「（注意：上下文未同步截断）」。
func (mh *MessageHandler) rewindSlashNotice(ctx context.Context, infos userInfos, msg *schema.Message, parsed command_parser.ParsedChannelControl) {
	state := mh.GetOrCreateChannelState(msg)
	targetSID := state.SessionID
	if targetSID == "" {
		mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, map[string]any{"error": "当前无活跃会话，无法回退"})
		return
	}

	turn := parsed.RewindTurn
	if turn == 0 {
		turn = 1
	}

	// 先取消当前任务（Python: await 同步调用；publish_interrupt_result 取默认 True）
	// M-06：原实现传 false，用户看不到中断结果与 cancelled_tools，与 /branch 自相矛盾。
	mh.CancelAgentWorkForSession(ctx, msg, targetSID, true)

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

		resp, err := mh.agentClient.SendRequest(ctx, env)
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
			mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, fmt.Sprintf("[收到 /rewind 指令] 已回退到第 %d 轮（\"%s\"），删除 %d 条记录，剩余 %d 条。", turn, preview, removed, remaining))
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
		mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, map[string]any{"error": fmt.Sprintf("回退失败：%v", err)})
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
	mh.sendChannelNotice(infos, msg.ChannelID, msg.SessionID, map[string]any{"content": fmt.Sprintf(
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

// runSlashAsync 以独立协程执行斜杠命令的 RPC/通知尾巴。
// S-02：对齐 Python 各分支的 asyncio.create_task。forwardLoop 是唯一消费
// mh.userMessages 的 goroutine，同步执行会让 /branch、/rewind、/skills list 的
// unary RPC（超时 600s，见 routing/agent_client.go）阻塞全渠道入站派发。
// ctx 使用 forwardLoop 的 ctx，停机时可取消；panic 一律 recover，不带崩进程。
func (mh *MessageHandler) runSlashAsync(ctx context.Context, action string, fn func(context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error(logComponent).
					Any("panic", r).
					Str("event_type", "slash_command_panic").
					Str("action", action).
					Msg("slash command goroutine panic recovered")
			}
		}()
		fn(ctx)
	}()
}
