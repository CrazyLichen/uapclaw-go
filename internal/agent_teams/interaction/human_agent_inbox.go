package interaction

import (
	"context"
	"fmt"
	"sort"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentLookup 解析 human-agent 成员名到活跃 TeamAgent 运行时。
// 对齐 Python: AgentLookup = Callable[[str], Optional[TeamAgent]]
type AgentLookup func(sender string) *agent.TeamAgent

// OnInbound 团队→用户通知回调。
// 对齐 Python: OnInbound = Callable[[HumanAgentInboundEvent], Awaitable[None]]
type OnInbound func(event HumanAgentInboundEvent) error

// HumanAgentNotEnabledError 团队未注册 human-agent 成员时抛出。
// 对齐 Python: HumanAgentNotEnabledError (openjiuwen/agent_teams/interaction/human_agent_inbox.py)
type HumanAgentNotEnabledError struct {
	// Message 错误描述
	Message string
}

// UnknownHumanAgentError 发送者不是已注册的 human-agent 成员时抛出。
// 对齐 Python: UnknownHumanAgentError (openjiuwen/agent_teams/interaction/human_agent_inbox.py)
// 类型已提升到 schema 包，此处保留别名以兼容现有调用方。
type UnknownHumanAgentError = schema.UnknownHumanAgentError

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
	team *tools.TeamBackend
	// messageManager 消息管理器
	messageManager *tools.TeamMessageManager
	// agentLookup 解析 human-agent 成员名到活跃 TeamAgent
	agentLookup AgentLookup
	// onInbound 团队→用户通知回调
	onInbound OnInbound
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHumanAgentInbox 创建 Human-Agent 收件箱。
// 对齐 Python: HumanAgentInbox.__init__(team, message_manager, *, agent_lookup, on_inbound)
func NewHumanAgentInbox(team *tools.TeamBackend, messageManager *tools.TeamMessageManager, agentLookup AgentLookup, onInbound OnInbound) *HumanAgentInbox {
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
//  2. team_logger.debug: HumanAgentInbox 发送者、目标、消息体长度
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
		Msg("HumanAgentInbox: 发送")

	// 对齐 Python 步骤 3: if to is None: return await self._drive_agent(...)
	if to == nil {
		return h.driveAgent(body, resolvedSender)
	}

	// 对齐 Python 步骤 4: if to in BROADCAST_TARGETS: broadcast
	if BroadcastTargets[*to] {
		// 对齐 Python: msg_id = await self._mm.broadcast_message(content=body, from_member_name=resolved_sender)
		ctx := context.Background()
		msgID, err := h.messageManager.BroadcastMessage(ctx, body, resolvedSender)
		if err != nil {
			return NewDeliverResultFailure("broadcast_failed:" + err.Error()), nil
		}
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

// Error 实现 error 接口（HumanAgentNotEnabledError）
func (e *HumanAgentNotEnabledError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "该团队未注册 human-agent 成员"
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
	// 对齐 Python 步骤 1: names = self._team.human_agent_names()
	names := h.team.HumanAgentNames()

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
			Msg("HumanAgentInbox: 未配置 agent_lookup，无法投递输入")
		return NewDeliverResultFailure("agent_unavailable"), nil
	}

	// 对齐 Python 步骤 2
	agent := h.agentLookup(sender)

	// 对齐 Python 步骤 3
	if agent == nil {
		logger.Warn(inboxLogComponent).Str("sender", sender).
			Msg("HumanAgentInbox: human agent 无活跃运行时")
		return NewDeliverResultFailure("agent_unavailable"), nil
	}

	// 对齐 Python 步骤 4-5
	// ⤵️ 待 9.55 回填: agent.DeliverInput(ctx, body)
	_ = agent // 避免未使用变量编译错误
	return NewDeliverResultSuccess(nil), nil
}

// memberExists 成员存在性检查。
// 对齐 Python: HumanAgentInbox._member_exists(name)
func (h *HumanAgentInbox) memberExists(name string) (bool, error) {
	ctx := context.Background()
	member, _ := h.team.GetMember(ctx, name)
	return member != nil, nil
}
