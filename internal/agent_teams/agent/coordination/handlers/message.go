package handlers

import (
	"context"

	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageHandler 处理消息与邮箱事件：MESSAGE、BROADCAST、POLL_MAILBOX、
// 以及 MEMBER_SHUTDOWN 时的邮箱排空。
//
// EVENT_METHOD_MAP:
//   - message                → onMessageOrBroadcast
//   - broadcast              → onMessageOrBroadcast
//   - coordination_poll_mailbox → onPollMailbox
//   - member_shutdown        → onMemberShutdownDrain
//
// Python: MessageHandler
type MessageHandler struct {
	BaseCoordinationHandler
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageHandler 创建 MessageHandler 实例。
// Python: MessageHandler.__init__
func NewMessageHandler(
	host types.DispatcherHost,
	bp types.DispatcherBlueprint,
	inf types.DispatcherInfra,
	pollCtrl types.PollController,
) *MessageHandler {
	return &MessageHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
	}
}

// GetCallbacks 返回 event_key → 回调方法注册表。
// Python: MessageHandler.get_callbacks
func (h *MessageHandler) GetCallbacks() map[string]types.EventCallbackFunc {
	return map[string]types.EventCallbackFunc{
		events.TeamEventMessage:                    h.OnMessageOrBroadcast,
		events.TeamEventBroadcast:                  h.OnMessageOrBroadcast,
		string(types.InnerEventTypePollMailbox): h.OnPollMailbox,
		events.TeamEventMemberShutdown:             h.OnMemberShutdownDrain,
	}
}

// OnMessageOrBroadcast 收到 MESSAGE 或 BROADCAST 事件时处理。
// Leader 做额外工作：自动确认 teammate→user 回复、通知 human-agent 入站回调。
// 所有成员恢复轮询并排空未读邮箱。
// Python: MessageHandler.on_message_or_broadcast
func (h *MessageHandler) OnMessageOrBroadcast(ctx context.Context, event types.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	em := event.Transport
	role := h.blueprint.Role()

	// Leader 额外逻辑
	if role == schema.TeamRoleLeader {
		h.ackUserBoundMessage(event)
		h.notifyHumanAgentInbound(event)
	}

	// 所有成员：恢复轮询 + 排空未读邮箱
	h.poll.ResumePolls()
	h.processUnreadMessages(ctx, h.blueprint.MemberName())

	logger.Debug(logComponent).
		Str("event_type", em.EventType).
		Str("role", string(role)).
		Msg("on_message_or_broadcast: 已处理消息/广播事件")
}

// OnPollMailbox 周期邮箱轮询：排空未读消息。
// Python: MessageHandler.on_poll_mailbox
func (h *MessageHandler) OnPollMailbox(ctx context.Context, _ types.CoordinationEvent) {
	h.processUnreadMessages(ctx, h.blueprint.MemberName())
}

// OnMemberShutdownDrain 成员关闭时排空邮箱，确保在拆卸前所有消息到达 agent。
// 仅 teammate 处理自身关闭事件；leader 和 human-agent 使用不同的拆卸路径。
// Python: MessageHandler.on_member_shutdown_drain
func (h *MessageHandler) OnMemberShutdownDrain(ctx context.Context, event types.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	em := event.Transport
	memberName := h.blueprint.MemberName()

	// 对齐 Python: 只关心自己的 shutdown 事件
	targetMember, _ := em.Payload["member_name"].(string)
	if targetMember != "" && targetMember != memberName {
		return
	}

	role := h.blueprint.Role()
	// Leader 和 human-agent 跳过（使用不同拆卸路径）
	if role == schema.TeamRoleLeader || role == schema.TeamRoleHumanAgent {
		return
	}

	// useSteer=true 确保消息以 steer 模式投递
	h.processUnreadMessagesWithSteer(ctx, memberName, true)
	logger.Info(logComponent).
		Str("member_name", memberName).
		Msg("on_member_shutdown_drain: 已排空邮箱")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// processUnreadMessages 排空未读邮箱消息并投递到 agent。
// 对齐 Python: MessageHandler._process_unread_messages(use_steer=False)
func (h *MessageHandler) processUnreadMessages(ctx context.Context, memberName string) {
	h.processUnreadMessagesWithSteer(ctx, memberName, false)
}

// processUnreadMessagesWithSteer 排空未读邮箱消息。
// 对齐 Python: MessageHandler._process_unread_messages
// TODO(#9.63): 等消息管理器接口就绪后补充完整的读取→格式化→投递→标记已读逻辑
func (h *MessageHandler) processUnreadMessagesWithSteer(ctx context.Context, memberName string, useSteer bool) {
	// 对齐 Python 步骤：
	// 1. 读取所有未读消息（_read_all_unread）
	// 2. 循环直到无新消息
	// 3. 对每条消息：
	//    a. 如果有 pending interrupt，延迟投递
	//    b. 格式化消息（_format_message）
	//    c. deliver_input(content, use_steer=useSteer)
	//    d. 标记已读
	_ = ctx
	_ = memberName
	_ = useSteer
	logger.Debug(logComponent).
		Str("member_name", memberName).
		Bool("use_steer", useSteer).
		Msg("processUnreadMessagesWithSteer: TODO 等消息管理器就绪")
}

// ackUserBoundMessage Leader 自动确认 teammate→user 的直接消息。
// 对齐 Python: MessageHandler._ack_user_bound_message
// TODO(#9.63): 等消息管理器接口就绪后补充
func (h *MessageHandler) ackUserBoundMessage(_ types.CoordinationEvent) {
	// Leader 标记 teammate→user 直接消息为已读
	// Python: 对 user 伪成员没有轮询 agent，leader 代为确认
}

// notifyHumanAgentInbound 转发团队侧消息到 SDK human-agent 入站回调。
// 对齐 Python: MessageHandler._notify_human_agent_inbound
// TODO(#9.63): 等交互层接口就绪后补充
func (h *MessageHandler) notifyHumanAgentInbound(_ types.CoordinationEvent) {
	// 对于广播：触发所有已注册回调（排除发送者）
	// 对于直接消息：仅在接收方为 human-agent 时触发
}
