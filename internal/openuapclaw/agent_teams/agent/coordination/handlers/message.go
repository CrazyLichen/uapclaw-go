package handlers

import (
	"context"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/interaction"
	ateamprompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/prompts"
	schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageHandler 处理消息与邮箱事件：MESSAGE、BROADCAST、POLL_MAILBOX、
// 以及 MEMBER_SHUTDOWN 时的邮箱排空。
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
// 对齐 Python: MessageHandler._process_unread_messages（message.py L105-143）——
// 循环读取未读（直发+广播，时间倒序）→ 逐条格式化投递 → 标记已读；
// pending interrupt 时延迟投递直接返回。
// S-附录4: 接通已实现的 MessageManager（原 TODO(#9.63) 空壳仅日志）。
func (h *MessageHandler) processUnreadMessagesWithSteer(ctx context.Context, memberName string, useSteer bool) {
	// M-29 同款 nil 守卫：测试/降级构造可能传 nil infra
	if h.infra == nil {
		return
	}
	mm := h.infra.MessageManager()
	if mm == nil {
		return
	}
	// Python: 角色查找只做一次（is_human_agent 终身不变）
	backend := h.infra.TeamBackend()
	isHumanAgent := backend != nil && backend.IsHumanAgent(memberName)

	seenIDs := make(map[string]struct{})
	for {
		// Python: _read_all_unread（直发+广播合并，时间倒序）
		var allUnread []*database.TeamMessageBase
		if direct, err := mm.GetMessages(ctx, memberName, true, ""); err == nil {
			allUnread = append(allUnread, direct...)
		}
		if broadcasts, err := mm.GetBroadcastMessages(ctx, memberName, true, ""); err == nil {
			allUnread = append(allUnread, broadcasts...)
		}
		sort.SliceStable(allUnread, func(i, j int) bool {
			return allUnread[i].Timestamp > allUnread[j].Timestamp
		})

		var newMessages []*database.TeamMessageBase
		for _, m := range allUnread {
			if _, seen := seenIDs[m.MessageID]; !seen {
				newMessages = append(newMessages, m)
			}
		}
		if len(newMessages) == 0 {
			break
		}

		logger.Info(logComponent).
			Str("member_name", memberName).
			Int("count", len(newMessages)).
			Bool("use_steer", useSteer).
			Msg("processing unread messages")

		for _, msg := range newMessages {
			seenIDs[msg.MessageID] = struct{}{}
			// Python: pending interrupt 时延迟投递（直接 return，等中断解决后重新触发）
			if h.round.HasPendingInterrupt() {
				logger.Info(logComponent).
					Str("member_name", memberName).
					Str("message_id", msg.MessageID).
					Msg("deferring mailbox message until pending interrupt is resolved")
				return
			}
			// F_63 两阶段渲染：普通消息原样通过；框架模板消息携带 meta，
			// 文档在此按*当下*的任务行渲染——在离线成员信箱里滞留的 handoff
			// 永不投递过时的任务简报。
			// Python: expanded = await self._expand(msg)（message.py L293）
			expanded := h.expandMessage(ctx, msg)
			var text string
			if expanded.IsTemplate {
				text = expanded.Body
			} else {
				text = formatTeamMessage(msg, isHumanAgent)
			}
			logger.Debug(logComponent).
				Str("member_name", memberName).
				Str("from", msg.FromMemberName).
				Str("message_id", msg.MessageID).
				Msg("message delivered")

			if err := h.round.DeliverInput(ctx, text, useSteer, nil); err != nil {
				logger.Error(logComponent).Err(err).
					Str("message_id", msg.MessageID).
					Msg("deliver_input failed")
			}
			mm.MarkMessageRead(ctx, msg.MessageID, memberName)
			// F_129：handoff 按投递时任务现状渲染，其 revision 正是 assignee
			// 眼前的东西——记录为已见。DAO 只对任务当前 assignee 记账。
			// Python: await self._observe_shown_revision(expanded)（message.py L303-304）
			h.observeShownRevision(ctx, expanded)
		}
	}
}

// expandMessage 渲染一条消息行的投递文本（F_63 两阶段模板化）。
// 普通消息 content 原样通过；框架模板消息只携带 meta，文档在此渲染——
// 对照任务行的当前状态，因此 offline 成员信箱滞留的 handoff 永不投递过时简报。
// Python: MessageHandler._expand（message.py L352-375）
func (h *MessageHandler) expandMessage(ctx context.Context, msg *database.TeamMessageBase) ateamprompts.ExpandedMessage {
	backend := h.infra.TeamBackend()
	getters := ateamprompts.ExpandGetters{Language: h.deliveryLanguage()}
	if backend != nil {
		db := backend.DB()
		getters.TaskGetter = func(ctx context.Context, taskID string) (*database.TeamTaskBase, error) {
			return db.Task().GetTask(ctx, taskID)
		}
		getters.MemberGetter = func(ctx context.Context, name string) (*database.TeamMember, error) {
			return db.Member().GetMember(ctx, name, backend.TeamName())
		}
		getters.TaskLinksGetter = func(ctx context.Context, taskID string) ([]*database.TeamTaskBase, []*database.TeamTaskBase, error) {
			return ateamprompts.TaskLinks(ctx, db.Task(), taskID)
		}
		snapshotGetter, subtasksGetter := ateamprompts.RevisionGetters(db.Task(), backend.TeamName())
		getters.RevisionSnapshotGetter = snapshotGetter
		getters.SubtasksGetter = subtasksGetter
	}
	expanded, err := ateamprompts.ExpandMessage(ctx, msg, getters)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("message_id", msg.MessageID).
			Msg("expand_message failed")
		return ateamprompts.ExpandedMessage{Body: msg.Content}
	}
	return expanded
}

// observeShownRevision 记录已投递 handoff 展示的任务 revision 为已见（F_129）。
// handoff 按投递时的任务行渲染，其 revision 恰是 assignee 眼前的东西；
// DAO 只对任务当前 assignee 记账。
// Python: MessageHandler._observe_shown_revision（message.py L311-323）
func (h *MessageHandler) observeShownRevision(ctx context.Context, expanded ateamprompts.ExpandedMessage) {
	if !expanded.HasShownRevision {
		return
	}
	if tm := h.infra.TaskManager(); tm != nil {
		tm.ObserveRevision(ctx, expanded.ShownTaskID, expanded.ShownRevision)
	}
}

// deliveryLanguage 投递时模板渲染的团队语言。
// Python: MessageHandler._language（message.py L325-330）
func (h *MessageHandler) deliveryLanguage() string {
	if spec := h.blueprint.DispatcherSpec(); spec != nil && spec.Language != "" {
		return spec.Language
	}
	return "cn"
}

// formatTeamMessage 格式化一条 TeamMessage 为 agent 输入。
// 对齐 Python: MessageHandler._format_message（message.py L252-275）——
// human-agent 用 hitt.msg_received_for_human 模板；普通成员用
// dispatcher.msg_received（含回复引导）。
func formatTeamMessage(msg *database.TeamMessageBase, isHumanAgent bool) string {
	msgTypeKey := "dispatcher.msg_type_direct"
	if msg.Broadcast {
		msgTypeKey = "dispatcher.msg_type_broadcast"
	}
	msgType := msgTypeKey
	if rendered, err := schema.T(msgTypeKey); err == nil {
		msgType = rendered
	}
	key := "dispatcher.msg_received"
	if isHumanAgent {
		key = "hitt.msg_received_for_human"
	}
	if content, err := schema.T(key, map[string]any{
		"msg_type":   msgType,
		"message_id": msg.MessageID,
		"sender":     msg.FromMemberName,
		"content":    msg.Content,
	}); err == nil {
		return content
	}
	return key
}

// ackUserBoundMessage Leader 自动确认 teammate→user 的直接消息。
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
