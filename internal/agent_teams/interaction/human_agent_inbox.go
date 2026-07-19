package interaction

import (
	"fmt"
	"sort"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HumanAgentNotEnabledError 团队未注册 human-agent 成员时抛出。
// 对齐 Python: HumanAgentNotEnabledError (openjiuwen/agent_teams/interaction/human_agent_inbox.py)
type HumanAgentNotEnabledError struct {
	// Message 错误描述
	Message string
}

// UnknownHumanAgentError 发送者不是已注册的 human-agent 成员时抛出。
// 对齐 Python: UnknownHumanAgentError (openjiuwen/agent_teams/interaction/human_agent_inbox.py)
type UnknownHumanAgentError struct {
	// Sender 尝试使用的发送者名
	Sender string
	// Registered 已注册的 human-agent 成员名列表
	Registered []string
}

// HumanAgentInbox Human-Agent 收件箱，路由 human-agent 输入。
// 对齐 Python: HumanAgentInbox (openjiuwen/agent_teams/interaction/human_agent_inbox.py)
//
// 路由规则：
//   - to == nil → 驱动 avatar 的 DeepAgent
//   - to in {"all", "*"} → 广播
//   - to == "member" → 验证目标后发送点对点消息
//
// 构造时注入：
//   - agentLookup(sender) → TeamAgent | nil — 解析活跃 human-agent 运行时
//   - onInbound(HumanAgentInboundEvent) → 团队→用户通知回调
type HumanAgentInbox struct {
	// team 团队后端
	// ⤵️ 待 9.55 回填: TeamBackend — 提供 HumanAgentNames/GetMember 方法
	team any
	// messageManager 消息管理器
	// ⤵️ 待 9.55 回填: TeamMessageManager — 提供 SendMessage/BroadcastMessage 方法
	messageManager any
	// agentLookup 解析 human-agent 成员名到活跃 TeamAgent
	agentLookup AgentLookup
	// onInbound 团队→用户通知回调
	onInbound OnInbound
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// AgentLookup 解析 human-agent 成员名到活跃 TeamAgent 运行时。
// 对齐 Python: AgentLookup = Callable[[str], Optional[TeamAgent]]
// ⤵️ 待 9.55 回填: 返回 *TeamAgent
type AgentLookup func(sender string) any

// OnInbound 团队→用户通知回调。
// 对齐 Python: OnInbound = Callable[[HumanAgentInboundEvent], Awaitable[None]]
type OnInbound func(event HumanAgentInboundEvent) error

// NewHumanAgentInbox 创建 Human-Agent 收件箱。
// 对齐 Python: HumanAgentInbox.__init__(team, message_manager, *, agent_lookup, on_inbound)
func NewHumanAgentInbox(team any, messageManager any, agentLookup AgentLookup, onInbound OnInbound) *HumanAgentInbox {
	return &HumanAgentInbox{
		team:           team,
		messageManager: messageManager,
		agentLookup:    agentLookup,
		onInbound:      onInbound,
	}
}

// Send 分发已解析的 human-agent 载荷。
// 对齐 Python: HumanAgentInbox.send(body, to, sender)
//
// Python 执行步骤：
//  1. resolved_sender = self._resolve_sender(sender)
//  2. team_logger.debug("HumanAgentInbox: sender=%s, to=%s, body_len=%d", ...)
//  3. if to is None: return await self._drive_agent(body, sender=resolved_sender)
//  4. if to in BROADCAST_TARGETS: broadcast_message → DeliverResult
//  5. return await deliver_direct(body, sender=resolved_sender, target=to, ...)
func (h *HumanAgentInbox) Send(body string, to *string, sender *string) (*DeliverResult, error) {
	// 对齐 Python 步骤 1: resolved_sender = self._resolve_sender(sender)
	resolvedSender, err := h.resolveSender(sender)
	if err != nil {
		return nil, err
	}

	// 对齐 Python 步骤 2: team_logger.debug(...)
	toStr := "<avatar>"
	if to != nil {
		toStr = *to
	}
	logger.Debug(inboxLogComponent).Str("sender", resolvedSender).
		Str("to", toStr).
		Str("body_len", fmt.Sprintf("%d", len(body))).
		Msg("HumanAgentInbox.Send")

	// 对齐 Python 步骤 3: if to is None: return await self._drive_agent(...)
	if to == nil {
		return h.driveAgent(body, resolvedSender)
	}

	// 对齐 Python 步骤 4: if to in BROADCAST_TARGETS: broadcast
	if BroadcastTargets[*to] {
		// ⤵️ 待 9.55 回填: msgID := h.messageManager.BroadcastMessage(body, resolvedSender)
		// 对齐 Python: msg_id = await self._mm.broadcast_message(content=body, from_member_name=resolved_sender)
		msgID := "stub-ha-broadcast-msg-id"
		return NewDeliverResultSuccess(&msgID), nil
	}

	// 对齐 Python 步骤 5: return await deliver_direct(body, sender=resolved_sender, target=to, ...)
	return DeliverDirect(body, resolvedSender, *to, h.messageManager, h.memberExists)
}

// GetOnInbound 返回团队→用户通知回调。
// 对齐 Python: HumanAgentInbox.on_inbound (property)
func (h *HumanAgentInbox) GetOnInbound() OnInbound {
	return h.onInbound
}

// Error 接口实现
func (e *HumanAgentNotEnabledError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "no human-agent member is registered on this team"
}

// Error 接口实现
func (e *UnknownHumanAgentError) Error() string {
	sorted := make([]string, len(e.Registered))
	copy(sorted, e.Registered)
	sort.Strings(sorted)
	return fmt.Sprintf("'%s' is not a registered human-agent member; registered members: %v",
		e.Sender, sorted)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSender 解析并验证发送者。
// 对齐 Python: HumanAgentInbox._resolve_sender(sender)
//
// Python 执行步骤：
//  1. names = self._team.human_agent_names()
//  2. if not names: raise HumanAgentNotEnabledError(...)
//  3. if sender is None:
//     a. if HUMAN_AGENT_MEMBER_NAME in names: return HUMAN_AGENT_MEMBER_NAME
//     b. return sorted(names)[0]
//  4. if sender not in names: raise UnknownHumanAgentError(...)
//  5. return sender
//
// ⤵️ 待 9.55 回填: 调用 team.HumanAgentNames() 获取已注册成员列表
func (h *HumanAgentInbox) resolveSender(sender *string) (string, error) {
	// 对齐 Python 步骤 1
	// ⤵️ 待 9.55 回填: names := h.team.HumanAgentNames()
	names := []string{agentteams.HumanAgentMemberName}

	// 对齐 Python 步骤 2: if not names: raise HumanAgentNotEnabledError
	if len(names) == 0 {
		return "", &HumanAgentNotEnabledError{}
	}

	// 对齐 Python 步骤 3: if sender is None
	if sender == nil {
		// 对齐 Python 步骤 3a: if HUMAN_AGENT_MEMBER_NAME in names: return HUMAN_AGENT_MEMBER_NAME
		for _, n := range names {
			if n == agentteams.HumanAgentMemberName {
				return n, nil
			}
		}
		// 对齐 Python 步骤 3b: return sorted(names)[0]
		sorted := make([]string, len(names))
		copy(sorted, names)
		sort.Strings(sorted)
		return sorted[0], nil
	}

	// 对齐 Python 步骤 4: if sender not in names: raise UnknownHumanAgentError
	for _, n := range names {
		if n == *sender {
			return *sender, nil
		}
	}
	return "", &UnknownHumanAgentError{Sender: *sender, Registered: names}
}

// driveAgent 驱动 avatar DeepAgent。
// 对齐 Python: HumanAgentInbox._drive_agent(body, sender)
//
// Python 执行步骤：
//  1. if self._agent_lookup is None: return DeliverResult.failure("agent_unavailable")
//  2. agent = self._agent_lookup(sender)
//  3. if agent is None: return DeliverResult.failure("agent_unavailable")
//  4. await agent.deliver_input(body)
//  5. return DeliverResult.success(None)
func (h *HumanAgentInbox) driveAgent(body string, sender string) (*DeliverResult, error) {
	// 对齐 Python 步骤 1
	if h.agentLookup == nil {
		logger.Warn(inboxLogComponent).Str("sender", sender).
			Msg("HumanAgentInbox: no agent_lookup wired; cannot deliver input")
		return NewDeliverResultFailure("agent_unavailable"), nil
	}

	// 对齐 Python 步骤 2
	agent := h.agentLookup(sender)

	// 对齐 Python 步骤 3
	if agent == nil {
		logger.Warn(inboxLogComponent).Str("sender", sender).
			Msg("HumanAgentInbox: human agent has no live runtime")
		return NewDeliverResultFailure("agent_unavailable"), nil
	}

	// 对齐 Python 步骤 4-5
	// ⤵️ 待 9.55 回填: agent.(*TeamAgent).DeliverInput(ctx, body)
	return NewDeliverResultSuccess(nil), nil
}

// memberExists 成员存在性检查。
// 对齐 Python: HumanAgentInbox._member_exists(name)
// ⤵️ 待 9.55 回填: 调用 team.GetMember(name)
func (h *HumanAgentInbox) memberExists(name string) (bool, error) {
	// ⤵️ 待 9.55 回填: member, err := h.team.GetMember(name); return member != nil, err
	return true, nil
}
