package handlers

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageHandler 处理消息与邮箱事件：MESSAGE、BROADCAST、POLL_MAILBOX、
// 以及 MEMBER_SHUTDOWN 时的邮箱排空。
//
// EVENT_METHOD_MAP 事件到方法映射：
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
		events.TeamEventMessage:                 h.OnMessageOrBroadcast,
		events.TeamEventBroadcast:               h.OnMessageOrBroadcast,
		string(types.InnerEventTypePollMailbox): h.OnPollMailbox,
		events.TeamEventMemberShutdown:          h.OnMemberShutdownDrain,
	}
}

// OnMessageOrBroadcast 收到 MESSAGE 或 BROADCAST 事件时处理。
// Leader 做额外工作：自动确认 teammate→user 回复、通知 human-agent 入站回调。
// 所有成员恢复轮询并排空未读邮箱。
// Python: MessageHandler.on_message_or_broadcast
func (h *MessageHandler) OnMessageOrBroadcast(ctx context.Context, event types.CoordinationEvent) {
	// 对齐 Python: if not member_name or self._infra.message_manager is None: return
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" {
		return
	}
	if h.infra == nil || h.infra.MessageManager() == nil {
		return
	}

	if event.IsInner() {
		return
	}
	em := event.Transport
	role := h.blueprint.Role()

	// Leader 额外逻辑
	// 对齐 Python: 只有 MESSAGE 时调用 _ack_user_bound_message，BROADCAST 不调用
	if role == schema.TeamRoleLeader {
		if em != nil && em.EventType == events.TeamEventMessage {
			h.ackUserBoundMessage(ctx, event)
		}
		h.notifyHumanAgentInbound(ctx, event)
	}

	// 所有成员：恢复轮询 + 排空未读邮箱
	h.poll.ResumePolls(ctx)
	h.processUnreadMessages(ctx, h.blueprint.DispatcherMemberName())

	logger.Debug(logComponent).
		Str("event_type", em.EventType).
		Str("role", string(role)).
		Msg("on_message_or_broadcast: message/broadcast event processed")
}

// OnPollMailbox 周期邮箱轮询：排空未读消息。
// Python: MessageHandler.on_poll_mailbox
func (h *MessageHandler) OnPollMailbox(ctx context.Context, _ types.CoordinationEvent) {
	// 对齐 Python: if member_name and self._infra.message_manager
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" {
		return
	}
	h.processUnreadMessages(ctx, memberName)
}

// OnMemberShutdownDrain 成员关闭时排空邮箱，确保在拆卸前所有消息到达 agent。
// 仅 teammate 处理自身关闭事件；leader 和 human-agent 使用不同的拆卸路径。
// Python: MessageHandler.on_member_shutdown_drain
func (h *MessageHandler) OnMemberShutdownDrain(ctx context.Context, event types.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	em := event.Transport
	memberName := h.blueprint.DispatcherMemberName()

	// 对齐 Python: 只关心自己的 shutdown 事件
	targetMember, _ := em.Payload["member_name"].(string)
	if targetMember != "" && targetMember != memberName {
		return
	}

	role := h.blueprint.Role()
	// 对齐 Python: if self._blueprint.role != TeamRole.TEAMMATE: return
	// 非 teammate 跳过（防御性写法，未来新增角色时自动过滤）
	if role != schema.TeamRoleTeammate {
		return
	}

	// useSteer=true 确保消息以 steer 模式投递
	h.processUnreadMessagesWithSteer(ctx, memberName, true)
	logger.Info(logComponent).
		Str("member_name", memberName).
		Msg("on_member_shutdown_drain: mailbox drained")
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
	//    c. 投递输入（deliver_input(content, use_steer=useSteer)）
	//    d. 标记已读
	_ = ctx
	_ = memberName
	_ = useSteer
	logger.Debug(logComponent).
		Str("member_name", memberName).
		Bool("use_steer", useSteer).
		Msg("processUnreadMessagesWithSteer: TODO waiting for message manager")
}

// ackUserBoundMessage Leader 自动确认 teammate→user 的直接消息。
//
// 对齐 Python: MessageHandler._ack_user_bound_message。
// user 伪成员没有轮询 agent，leader 代为确认——不标记则 unread 累积
// 反复唤醒 dispatcher。M-29: 完整实现（原空壳 TODO(#9.63)）。
func (h *MessageHandler) ackUserBoundMessage(ctx context.Context, event types.CoordinationEvent) {
	// M-29: h.infra nil 守卫（测试/降级构造可能传 nil）
	if h.infra == nil {
		return
	}
	// Python: payload: MessageEvent = event.get_payload()
	em := event.Transport
	if em == nil {
		return
	}
	// Python: if payload.to_member_name != "user": return
	toMemberName, _ := em.Payload["to_member_name"].(string)
	if toMemberName != "user" {
		return
	}
	mm := h.infra.MessageManager()
	if mm == nil {
		return
	}
	// Python: await mm.mark_message_read(payload.message_id, "user")
	messageID, _ := em.Payload["message_id"].(string)
	mm.MarkMessageRead(ctx, messageID, "user")
	fromMemberName, _ := em.Payload["from_member_name"].(string)
	logger.Debug(logComponent).
		Str("message_id", messageID).
		Str("from_member_name", fromMemberName).
		Msg("leader auto-acked user-bound message")
}

// notifyHumanAgentInbound 转发团队侧消息到 SDK human-agent 入站回调。
//
// 对齐 Python: MessageHandler._notify_human_agent_inbound。
// Leader 观察团队 topic 上的每个 MESSAGE/BROADCAST 事件：
// 直发消息接收方为 human-agent 时触发其回调；广播触发所有已注册回调
// （排除发送者——human-agent 不回显自己的广播）。消息体加载失败时
// 记 Warn 吞掉，通知故障不打断 dispatch 循环。
// M-29: 完整实现（原空壳 TODO(#9.63)）。
func (h *MessageHandler) notifyHumanAgentInbound(ctx context.Context, event types.CoordinationEvent) {
	// M-29: h.infra nil 守卫（测试/降级构造可能传 nil）
	if h.infra == nil {
		return
	}
	// Python: backend = self._infra.team_backend; mm = self._infra.message_manager
	backend := h.infra.TeamBackend()
	mm := h.infra.MessageManager()
	if backend == nil || mm == nil {
		return
	}

	em := event.Transport
	if em == nil {
		return
	}

	// Python: message_id = payload.message_id; sender = payload.from_member_name
	messageID, _ := em.Payload["message_id"].(string)
	sender, _ := em.Payload["from_member_name"].(string)
	isBroadcast := em.EventType == events.TeamEventBroadcast

	// Python: row = await mm.db.message.get_message(message_id)
	row, err := mm.MessageDao().GetMessage(ctx, messageID)
	if err != nil {
		// Python: except → warning + return（吞掉）
		logger.Warn(logComponent).
			Str("message_id", messageID).
			Err(err).
			Msg("human_agent on_inbound: failed to load message")
		return
	}
	if row == nil {
		return
	}

	// Python: body = row.content; ts = row.timestamp
	body := row.Content
	ts := row.Timestamp

	// Python: 广播→所有 human-agent（排除 sender）；直发→仅接收方为 human-agent 时
	var recipients []string
	if isBroadcast {
		for _, name := range backend.HumanAgentNames() {
			if name != sender {
				recipients = append(recipients, name)
			}
		}
	} else {
		target, _ := em.Payload["to_member_name"].(string)
		if !backend.IsHumanAgent(target) {
			return
		}
		recipients = []string{target}
	}

	// Python: 逐 recipient 取回调并触发，回调异常 Warn 吞掉
	for _, recipient := range recipients {
		callback := backend.GetHumanAgentInbound(recipient)
		if callback == nil {
			continue
		}
		evt := &interaction.HumanAgentInboundEvent{
			MemberName: recipient,
			Sender:     sender,
			Body:       body,
			Broadcast:  isBroadcast,
			MessageID:  messageID,
			Timestamp:  ts,
		}
		if err := callback(ctx, recipient, evt); err != nil {
			logger.Warn(logComponent).
				Str("recipient", recipient).
				Err(err).
				Msg("human_agent on_inbound callback raised")
		}
	}
}
