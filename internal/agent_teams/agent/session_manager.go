package agent

import (
	"context"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionManager 管理团队会话生命周期和持久化。
// 对齐 Python: SessionManager (openjiuwen/agent_teams/agent/session_manager.py)
//
// 三态模型：
//
//	Unbound     — sessionID="" teamSession=nil  从未绑定或显式解绑
//	Half-bound  — sessionID="X" teamSession=nil 会话释放但 ID 保留用于日志
//	Fully-bound — sessionID="X" teamSession=ses 活跃会话，可读写
//
// 禁止状态：sessionID="" + teamSession!=nil（悬空状态）
//
// 对齐 Python SessionManager 的关键差异：
//   - Python 使用 contextvars.Token 机制管理 session_id 的设置/重置
//   - Go 使用 SessionState 可变容器 + context.Value 传播指针（同 CwdState 模式）
//   - Python bind_session 中 set_session_id() 返回 Token 并持有
//   - Go BindSession 中直接 SessionState.SetSessionID() 原地修改，不需要 Token
//   - Python release_session 中 reset_session_id(token) 恢复旧值
//   - Go ReleaseSession 中直接 SetSessionID("") 清空
type SessionManager struct {
	// state 可变运行时状态
	state *TeamAgentState
	// configurator Agent 配置器
	configurator *AgentConfigurator
	// recoveryManager 恢复管理器
	// TODO(#9.61): 替换 any 为 RecoveryManager 类型
	recoveryManager any
	// sessionState session_id 可变容器（通过 context 传播）
	sessionState *agentteams.SessionState
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionMgrLogComponent 日志组件
	sessionMgrLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionManager 创建新的 SessionManager。
// 对齐 Python: SessionManager.__init__(state, configurator, recovery_manager)
func NewSessionManager(
	state *TeamAgentState,
	configurator *AgentConfigurator,
	recoveryManager any, // TODO(#9.61): RecoveryManager 类型
) *SessionManager {
	return &SessionManager{
		state:           state,
		configurator:    configurator,
		recoveryManager: recoveryManager,
		sessionState:    agentteams.InitSessionState(),
	}
}

// TeamSession 返回当前团队会话。
// 对齐 Python: SessionManager.team_session (property getter)
func (m *SessionManager) TeamSession() any {
	return m.state.TeamSession
}

// SetTeamSession 设置团队会话。
// 对齐 Python: SessionManager.team_session (property setter)
func (m *SessionManager) SetTeamSession(session any) {
	m.state.TeamSession = session
}

// BindSession 完整绑定到指定会话。
// 对齐 Python: SessionManager.bind_session(session)
//
// Python 执行步骤：
//  1. _reset_session_id_token()        — 重置上一次的 Token
//  2. _session_id_token = set_session_id(session.get_session_id())  — 设置 session_id 到 contextvar
//  3. state.team_session = session     — 存储到可变状态
//  4. team_backend.db.create_cur_session_tables()  — 创建 DB 表（幂等）
//  5. recovery_manager.persist_leader_config(session) — Leader 侧持久化
//
// Go 对应步骤：
//  1. 不需要 Token reset — 直接 SetSessionID 覆盖旧值
//  2. sessionState.SetSessionID(sessionID) — 原地修改，共享指针立即可见
//  3. state.TeamSession = session — 存储到可变状态
//  4. TODO(#9.61): teamBackend.DB().CreateCurSessionTables()
//  5. TODO(#9.61): recoveryManager.PersistLeaderConfig(session)
//
// 返回新的 context.Context（含 SessionState），调用方必须用于后续传播。
func (m *SessionManager) BindSession(
	ctx context.Context,
	session any, // TODO(#9.59): AgentTeamSession 接口
) (context.Context, error) {
	sessionID := extractSessionID(session)

	// 步骤 1-2: 设置 sessionID 到 SessionState（原地修改，共享指针立即可见）
	// 对齐 Python: _reset_session_id_token() + set_session_id(session.get_session_id())
	m.sessionState.SetSessionID(sessionID)

	// 注入 SessionState 到 context（如果尚未注入）
	// 对齐 Python: contextvars 自动生效
	// Go: 需要显式 WithSessionState 返回新 ctx
	newCtx := ctx
	if agentteams.SessionStateFromCtx(ctx) == nil {
		newCtx = agentteams.WithSessionState(ctx, m.sessionState)
	}

	// 步骤 3: 存储 session 到 state
	// 对齐 Python: state.team_session = session if isinstance(session, AgentTeamSession) else None
	m.state.TeamSession = session

	// 步骤 4: 创建 DB 表（幂等）
	// 对齐 Python: if team_backend: await team_backend.db.create_cur_session_tables()
	// TODO(#9.61): if teamBackend := m.configurator.TeamBackend(); teamBackend != nil { ... }

	// 步骤 5: Leader 侧持久化
	// 对齐 Python: if spec and role == TeamRole.LEADER: recovery_manager.persist_leader_config(session)
	// TODO(#9.61): if m.configurator.Role() == TeamRoleLeader && m.configurator.Spec() != nil { ... }

	logger.Info(sessionMgrLogComponent).
		Str("session_id", sessionID).
		Msg("SessionManager.BindSession")

	return newCtx, nil
}

// ReleaseSession 从当前会话解绑。
// 对齐 Python: SessionManager.release_session()
//
// Python 执行步骤：
//  1. _reset_session_id_token()        — 重置 contextvar Token
//  2. state.team_session = None         — 清空会话引用
//
// Go 对应步骤：
//  1. sessionState.SetSessionID("")     — 清空 sessionID（不需要 Token reset）
//  2. state.TeamSession = nil           — 清空会话引用
//
// 清空后 sessionID=""，teamSession=nil → Unbound 状态。
func (m *SessionManager) ReleaseSession() {
	// 步骤 1: 清空 sessionID
	// 对齐 Python: _reset_session_id_token()
	m.sessionState.SetSessionID("")

	// 步骤 2: 清空 teamSession
	// 对齐 Python: state.team_session = None
	m.state.TeamSession = nil

	logger.Info(sessionMgrLogComponent).
		Msg("SessionManager.ReleaseSession")
}

// ResumeForNewSession 切换到新会话并重新绑定活着的 teammate 运行时。
// 对齐 Python: SessionManager.resume_for_new_session(session)
//
// Python 执行步骤：
//  1. recoverable_members = await recovery_manager.collect_live_teammates_for_session_switch()
//  2. await self.bind_session(session)
//  3. if role != LEADER or not team_backend: return
//  4. await recovery_manager.restart_for_session_switch(recoverable_members, cleanup_first=True)
//
// Go 对应步骤：
//  1. TODO(#9.61): 收集活着的 teammate
//  2. BindSession — 已实现
//  3. TODO(#9.61): Leader 判断
//  4. TODO(#9.61): 重启 teammate
//
// 顺序重要：先快照再 bind，这样新 session_id 通过 spawn 载荷传播到重启的 teammate。
func (m *SessionManager) ResumeForNewSession(
	ctx context.Context,
	session any,
) (context.Context, error) {
	// 步骤 1: 收集活着的 teammate（快照）
	// 对齐 Python: recoverable_members = await self._recovery_manager.collect_live_teammates_for_session_switch()
	// TODO(#9.61): recoverableMembers := m.recoveryManager.CollectLiveTeammatesForSessionSwitch()

	// 步骤 2: 绑定新会话
	// 对齐 Python: await self.bind_session(session)
	newCtx, err := m.BindSession(ctx, session)
	if err != nil {
		return newCtx, err
	}

	// 步骤 3-4: Leader 侧重启 teammate
	// 对齐 Python:
	//   if self._configurator.role != TeamRole.LEADER or not team_backend: return
	//   await self._recovery_manager.restart_for_session_switch(recoverable_members, cleanup_first=True)
	// TODO(#9.61): if m.configurator.Role() != Leader || m.configurator.TeamBackend() == nil → return
	// TODO(#9.61): m.recoveryManager.RestartForSessionSwitch(recoverableMembers, true)

	return newCtx, nil
}

// RecoverForExistingSession 绑定到检查点恢复的会话（不清理）。
// 对齐 Python: SessionManager.recover_for_existing_session(session)
//
// Python 执行步骤（与 resume_for_new_session 相同，但 cleanup_first=False）：
//  1. recoverable_members = await recovery_manager.collect_live_teammates_for_session_switch()
//  2. await self.bind_session(session)
//  3. if role != LEADER or not team_backend: return
//  4. await recovery_manager.restart_for_session_switch(recoverable_members, cleanup_first=False)
//
// 调用方必须已经拆卸协调（清除了活跃句柄）并验证了检查点。
func (m *SessionManager) RecoverForExistingSession(
	ctx context.Context,
	session any,
) (context.Context, error) {
	// 步骤 1: 收集活着的 teammate（快照）
	// 对齐 Python: recoverable_members = await self._recovery_manager.collect_live_teammates_for_session_switch()
	// TODO(#9.61): recoverableMembers := m.recoveryManager.CollectLiveTeammatesForSessionSwitch()

	// 步骤 2: 绑定会话
	// 对齐 Python: await self.bind_session(session)
	newCtx, err := m.BindSession(ctx, session)
	if err != nil {
		return newCtx, err
	}

	// 步骤 3-4: Leader 侧重启 teammate（cleanup_first=False，不清理）
	// 对齐 Python:
	//   if self._configurator.role != TeamRole.LEADER or not team_backend: return
	//   await self._recovery_manager.restart_for_session_switch(recoverable_members, cleanup_first=False)
	// TODO(#9.61): if m.configurator.Role() != Leader || m.configurator.TeamBackend() == nil → return
	// TODO(#9.61): m.recoveryManager.RestartForSessionSwitch(recoverableMembers, false)

	return newCtx, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractSessionID 从 session 对象中提取 session_id。
// 对齐 Python: session.get_session_id()
// TODO(#9.59): 定义 AgentTeamSession 接口后替换为接口方法调用
func extractSessionID(session any) string {
	if session == nil {
		return ""
	}
	// 尝试类型断言到含 GetSessionID() 方法的接口
	type sessionIDer interface {
		GetSessionID() string
	}
	if s, ok := session.(sessionIDer); ok {
		return s.GetSessionID()
	}
	return ""
}
