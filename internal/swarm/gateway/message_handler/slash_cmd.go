package message_handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler/command_parser"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleChannelControl 处理 slash 命令，返回 true 表示已处理
//
// 对齐 Python _handle_channel_control (L615-L870)：
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
		mh.sendChannelNotice(msg, "/new_session 不接受额外参数")
	case command_parser.ActionModeOK:
		mh.modeChangeCancelAndNotice(msg, parsed)
	case command_parser.ActionModeBad:
		mh.sendChannelNotice(msg, "无效的 /mode 参数")
	case command_parser.ActionSwitchOK:
		mh.modeChangeCancelAndNotice(msg, parsed)
	case command_parser.ActionSwitchBad:
		mh.sendChannelNotice(msg, "无效的 /switch 参数")
	case command_parser.ActionSkillsOK:
		mh.skillsSlashNotice(msg)
	case command_parser.ActionBranchOK:
		mh.branchSlashNotice(msg, parsed)
	case command_parser.ActionRewindOK:
		mh.rewindSlashNotice(msg, parsed)
	case command_parser.ActionRewindBad:
		mh.sendChannelNotice(msg, "/rewind 需要正整数轮次编号")
	case command_parser.ActionRewindConfirm:
		mh.rewindSlashNotice(msg, parsed)
	case command_parser.ActionRewindCancel:
		mh.sendChannelNotice(msg, "已取消回退操作")
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

// newSessionCancelAndNotice 处理 /new_session 命令
func (mh *MessageHandler) newSessionCancelAndNotice(msg *schema.Message, _ command_parser.ParsedChannelControl) {
	sessionID := msg.SessionID
	if sessionID != "" {
		// 取消旧会话的流式任务
		mh.cancelAgentWorkForSessionNoCtx(sessionID, "new_session")
	}

	// 生成新 session_id
	newSID := GenerateChannelSessionID(msg.ChannelID)

	// 更新渠道状态
	state := mh.GetOrCreateChannelState(msg)
	state.SessionID = newSID

	mh.sendChannelNotice(msg, fmt.Sprintf("已创建新会话：%s", newSID))
}

// modeChangeCancelAndNotice 处理 /mode 和 /switch 命令
func (mh *MessageHandler) modeChangeCancelAndNotice(msg *schema.Message, parsed command_parser.ParsedChannelControl) {
	// 确定新模式
	var newMode ChannelMode
	switch parsed.Action {
	case command_parser.ActionModeOK:
		newMode = ParseChannelMode(parsed.ModeSubcommand)
	case command_parser.ActionSwitchOK:
		switch parsed.SwitchSubcommand {
		case "plan":
			newMode = ChannelModeAgentPlan
		case "fast":
			newMode = ChannelModeAgentFast
		case "normal":
			newMode = ChannelModeCodeNormal
		case "team":
			newMode = ChannelModeTeam
		default:
			newMode = ChannelModeAgentPlan
		}
	default:
		return
	}

	// 更新渠道状态
	state := mh.GetOrCreateChannelState(msg)
	state.Mode = newMode

	modeName := ChannelModeString(newMode)
	mh.sendChannelNotice(msg, fmt.Sprintf("模式已切换为：%s", modeName))
}

// sendChannelNotice 发送渠道通知消息
func (mh *MessageHandler) sendChannelNotice(msg *schema.Message, notice string) {
	noticeMsg := schema.NewEventMessage(msg.ChannelID, msg.SessionID, schema.EventTypeChatProcessingStatus,
		map[string]any{
			"notice":     notice,
			"session_id": msg.SessionID,
		},
	)
	mh.enqueueOutbound(noticeMsg)
}

// skillsSlashNotice 处理 /skills list 命令
func (mh *MessageHandler) skillsSlashNotice(msg *schema.Message) {
	mh.sendChannelNotice(msg, "技能列表功能待实现")
}

// branchSlashNotice 处理 /branch 命令
func (mh *MessageHandler) branchSlashNotice(msg *schema.Message, parsed command_parser.ParsedChannelControl) {
	branchName := parsed.BranchName
	if branchName == "" {
		branchName = "default"
	}
	mh.sendChannelNotice(msg, fmt.Sprintf("分支功能待实现（分支名：%s）", branchName))
}

// rewindSlashNotice 处理 /rewind 命令
func (mh *MessageHandler) rewindSlashNotice(msg *schema.Message, parsed command_parser.ParsedChannelControl) {
	turn := parsed.RewindTurn
	if turn == 0 {
		mh.sendChannelNotice(msg, "回退功能待实现")
		return
	}
	mh.sendChannelNotice(msg, fmt.Sprintf("回退到第 %d 轮功能待实现", turn))
}

// extractTextFromParams 从消息 params 中提取文本内容
func extractTextFromParams(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var paramsMap map[string]any
	if err := json.Unmarshal(params, &paramsMap); err != nil {
		return ""
	}
	if content, ok := paramsMap["content"]; ok {
		if s, isStr := content.(string); isStr {
			return s
		}
	}
	return ""
}

// cancelAgentWorkForSessionNoCtx 无 context 版本的取消
func (mh *MessageHandler) cancelAgentWorkForSessionNoCtx(sessionID, intent string) {
	ctx := context.Background()
	mh.CancelAgentWorkForSession(ctx, sessionID, intent)
}
