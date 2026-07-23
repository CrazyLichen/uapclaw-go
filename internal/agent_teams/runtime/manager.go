package runtime

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamRuntimeManager 团队运行时管理器。
// 对齐 Python: TeamRuntimeManager (openjiuwen/agent_teams/runtime/manager.py)
//
// 持有进程内 TeamRuntimePool，分发每个 run_agent_team_streaming 调用
// 到四条恢复路径之一（或拒绝）。池条目是"哪些团队当前活跃"的唯一事实来源。
type TeamRuntimeManager struct {
	// pool 活跃团队运行时池
	pool *TeamRuntimePool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// mgrLogComponent 日志组件
	mgrLogComponent = logger.ComponentChannel
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamRuntimeManager 创建运行时管理器。
func NewTeamRuntimeManager() *TeamRuntimeManager {
	return &TeamRuntimeManager{
		pool: NewTeamRuntimePool(),
	}
}

// Pool 返回运行时池。
func (m *TeamRuntimeManager) Pool() *TeamRuntimePool {
	return m.pool
}

// Interact 路由交互载荷通过活跃团队的门控。
// 对齐 Python: TeamRuntimeManager.interact(payload, *, team_name, session_id)
//
// Python 执行步骤：
//  1. entry = await self._resolve_entry(team_name=team_name, session_id=session_id)
//  2. if entry is None: return DeliverResult.failure("not_active")
//  3. if isinstance(payload, InteractiveInput):
//     a. if entry.agent.has_pending_interrupt(): await entry.agent.resume_interrupt(payload); return success
//     b. return DeliverResult.failure("unsupported_interactive_input")
//  4. if isinstance(payload, str): parsed = parse_interact_str(payload)
//  5. payloads = parsed or [GodViewMessage(body=payload)]
//  6. ticket = await entry.interact_gate.admit()
//  7. if ticket is None: return DeliverResult.failure("gate_closed")
//  8. try:
//     a. payloads = await self._resolve_recipients(entry.agent, payloads)
//     b. last_result = DeliverResult.success(None)
//     c. for entry_payload in payloads:
//     - last_result = await self._dispatch_payload(entry.agent, entry_payload)
//     - if not last_result.ok: return last_result
//     d. return last_result
//  9. finally: await entry.interact_gate.consume_done(ticket)
//
// 接受三种输入类型：
//   - *sessioninteraction.InteractiveInput → 恢复中断
//   - string → ParseInteractStr → payloads
//   - interaction.InteractPayload → 直接分发
func (m *TeamRuntimeManager) Interact(
	ctx context.Context,
	payload any,
	teamName string,
	sessionID string,
) (*interaction.DeliverResult, error) {
	// 对齐 Python 步骤 1-2
	entry := m.resolveEntry(teamName, sessionID)
	if entry == nil {
		return interaction.NewDeliverResultFailure("not_active"), nil
	}

	// 对齐 Python 步骤 3: InteractiveInput → 恢复中断
	if interactiveInput, ok := payload.(*sessioninteraction.InteractiveInput); ok {
		return m.handleInteractiveInput(entry, interactiveInput)
	}

	// 对齐 Python 步骤 4-5: 解析 payloads
	var payloads []interaction.InteractPayload
	if strPayload, ok := payload.(string); ok {
		parsed := interaction.ParseInteractStr(strPayload)
		if len(parsed) == 0 {
			payloads = []interaction.InteractPayload{interaction.NewGodViewMessage(strPayload)}
		} else {
			payloads = parsed
		}
	} else if interactPayload, ok := payload.(interaction.InteractPayload); ok {
		payloads = []interaction.InteractPayload{interactPayload}
	} else {
		return interaction.NewDeliverResultFailure("unsupported_payload_type"), nil
	}

	// 对齐 Python 步骤 6-7: admit
	ticket := entry.InteractGate.Admit()
	if ticket == nil {
		return interaction.NewDeliverResultFailure("gate_closed"), nil
	}

	// 对齐 Python 步骤 9: finally consume_done
	defer entry.InteractGate.ConsumeDone(ticket)

	// 对齐 Python 步骤 8a: resolve_recipients
	resolved, err := m.resolveRecipients(entry, payloads)
	if err != nil {
		return nil, err
	}

	// 对齐 Python 步骤 8b-8d: 逐个分发
	var lastResult *interaction.DeliverResult
	for _, p := range resolved {
		lastResult, err = m.dispatchPayload(ctx, entry, p)
		if err != nil {
			return nil, err
		}
		if !lastResult.IsOK() {
			return lastResult, nil
		}
	}
	if lastResult == nil {
		lastResult = interaction.NewDeliverResultSuccess(nil)
	}
	return lastResult, nil
}

// Activate 激活团队。
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) Activate(ctx context.Context, teamName string, sessionID string, agent any) error {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("Activate (stub)")
	// ⤵️ 待 9.62: 创建 ActiveTeam → pool.Add(entry)
	return nil
}

// Finalize 终结团队运行。
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) Finalize(ctx context.Context, teamName string, sessionID string) error {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("Finalize (stub)")
	return nil
}

// Pause 暂停团队。
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) Pause(ctx context.Context, teamName string, sessionID string) (bool, error) {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("Pause (stub)")
	return false, nil
}

// StopTeam 停止团队。
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) StopTeam(ctx context.Context, teamName string, sessionID string) (bool, error) {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("StopTeam (stub)")
	return false, nil
}

// DeleteTeam 删除团队。
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) DeleteTeam(ctx context.Context, teamName string, sessionID string) (bool, error) {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("DeleteTeam (stub)")
	return false, nil
}

// RegisterHumanAgentInbound 注册团队→用户通知回调。
// ⤵️ 待 9.55 TeamBackend 回填
func (m *TeamRuntimeManager) RegisterHumanAgentInbound(ctx context.Context, teamName string, sessionID string, memberName string, callback any) (bool, error) {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Str("member_name", memberName).Msg("RegisterHumanAgentInbound (stub)")
	return false, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveEntry 查找活跃团队条目。
// 对齐 Python: TeamRuntimeManager._resolve_entry(team_name, session_id)
func (m *TeamRuntimeManager) resolveEntry(teamName string, sessionID string) *ActiveTeam {
	entry := m.pool.Get(teamName)
	if entry == nil {
		return nil
	}
	if entry.SessionID != sessionID {
		return nil
	}
	return entry
}

// handleInteractiveInput 处理 InteractiveInput（恢复中断）。
// 对齐 Python: TeamRuntimeManager.interact() 中的 InteractiveInput 分支
//
// Python 步骤：
//  1. if entry.agent.has_pending_interrupt():
//  2. await entry.agent.resume_interrupt(payload)
//  3. return DeliverResult.success(None)
//  4. return DeliverResult.failure("unsupported_interactive_input")
func (m *TeamRuntimeManager) handleInteractiveInput(entry *ActiveTeam, input *sessioninteraction.InteractiveInput) (*interaction.DeliverResult, error) {
	// ⤵️ 待 9.55 回填: 完整实现
	// 当前 stub: 模拟成功
	logger.Debug(mgrLogComponent).Msg("handleInteractiveInput (stub)")
	return interaction.NewDeliverResultSuccess(nil), nil
}

// resolveRecipients 校验 @<member> 接收者是否在花名册中。
// 对齐 Python: TeamRuntimeManager._resolve_recipients(agent, payloads)
//
// Python 步骤：
//  1. backend = agent.team_backend
//  2. if backend is None: return payloads
//  3. async def _member_exists(name): return await backend.get_member(name) is not None
//  4. return await resolve_targets(payloads, member_exists=_member_exists)
func (m *TeamRuntimeManager) resolveRecipients(entry *ActiveTeam, payloads []interaction.InteractPayload) ([]interaction.InteractPayload, error) {
	// ⤵️ 待 9.55 回填: 从 agent.team_backend 获取 MemberExistsCheck
	// 当前 stub: 所有成员视为存在
	memberExists := func(name string) (bool, error) { return true, nil }
	return interaction.ResolveTargets(payloads, memberExists)
}

// dispatchPayload 按载荷类型分发。
// 对齐 Python: TeamRuntimeManager._dispatch_payload(agent, payload)
//
// Python 步骤：
//  1. backend = agent.team_backend
//  2. if backend is None and not isinstance(payload, GodViewMessage): return failure("no_team_backend")
//  3. if isinstance(payload, GodViewMessage):
//     return await UserInbox.deliver_to_leader(agent.deliver_input, payload.body)
//  4. if isinstance(payload, OperatorMessage):
//     a. inbox = UserInbox(backend.message_manager)
//     b. if payload.target is None: await agent.auto_start_all(); return await inbox.broadcast(payload.body)
//     c. await agent.auto_start_member(payload.target); return await inbox.direct(payload.target, payload.body)
//  5. if isinstance(payload, HumanAgentMessage):
//     a. try: inbox = HumanAgentInbox(backend, backend.message_manager, agent_lookup=agent.lookup_human_agent_runtime)
//     b. return await inbox.send(payload.body, to=payload.target, sender=payload.sender)
//     c. except HumanAgentNotEnabledError: return failure("human_agent_not_enabled")
//     d. except UnknownHumanAgentError: return failure("unknown_human_agent")
//  6. return failure(f"unknown_payload:{type(payload).__name__}")
func (m *TeamRuntimeManager) dispatchPayload(
	ctx context.Context,
	entry *ActiveTeam,
	payload interaction.InteractPayload,
) (*interaction.DeliverResult, error) {
	switch p := payload.(type) {
	case *interaction.GodViewMessage:
		// 对齐 Python 步骤 3: return await UserInbox.deliver_to_leader(agent.deliver_input, payload.body)
		deliverInput := func(ctx context.Context, content string) error {
			logger.Debug(mgrLogComponent).Str("body_len", fmt.Sprintf("%d", len(content))).
				Msg("deliverInput (stub)")
			// ⤵️ 待 9.55 回填: agent.(*TeamAgent).DeliverInput(ctx, content)
			return nil
		}
		return interaction.DeliverToLeader(deliverInput, p.Body()), nil

	case *interaction.OperatorMessage:
		// 对齐 Python 步骤 4: inbox = UserInbox(backend.message_manager)
		inbox := interaction.NewUserInbox(nil) // ⤵️ 待 9.55 回填: messageManager
		if p.Target() == nil {
			// 对齐 Python 步骤 4b: 广播前先自动启动所有未启动成员
			// ⤵️ 待 9.55 回填: agent.AutoStartAll()
			return inbox.Broadcast(p.Body())
		}
		// 对齐 Python 步骤 4c: 点对点前先启动目标成员
		// ⤵️ 待 9.55 回填: agent.AutoStartMember(*p.Target())
		return inbox.Direct(*p.Target(), p.Body())

	case *interaction.HumanAgentMessage:
		// 对齐 Python 步骤 5: inbox = HumanAgentInbox(...)
		hInbox := interaction.NewHumanAgentInbox(
			nil, // ⤵️ 待 9.55 回填: team backend
			nil, // ⤵️ 待 9.55 回填: messageManager
			nil, // ⤵️ 待 9.55 回填: agentLookup
			nil, // ⤵️ 待 9.55 回填: onInbound
		)
		result, err := hInbox.Send(p.Body(), p.Target(), strPtr(p.Sender()))
		if err != nil {
			// 对齐 Python 步骤 5c-5d
			if _, ok := err.(*interaction.HumanAgentNotEnabledError); ok {
				return interaction.NewDeliverResultFailure("human_agent_not_enabled"), nil
			}
			if _, ok := err.(*interaction.UnknownHumanAgentError); ok {
				return interaction.NewDeliverResultFailure("unknown_human_agent"), nil
			}
			return nil, err
		}
		return result, nil

	default:
		// 对齐 Python 步骤 6: return failure(f"unknown_payload:{type(payload).__name__}")
		return interaction.NewDeliverResultFailure("unknown_payload:" + payload.Kind().String()), nil
	}
}

// strPtr 返回字符串指针。
func strPtr(s string) *string { return &s }
