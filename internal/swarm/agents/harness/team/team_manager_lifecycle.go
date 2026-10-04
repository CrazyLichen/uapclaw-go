package team

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	runner "github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CommitRuntimeReady 提交运行时就绪：设置 active + 清 pending。
// 对齐 Python: TeamManager.commit_runtime_ready(session_id, team_name)
//
// Python 步骤：
//  1. self._active_session_id = session_id
//  2. self._active_team_name = team_name
//  3. if self._pending_session_id == session_id: clear pending
//  4. logger.info("[TeamManager] commit_runtime_ready ...")
//  5. ensure_monitor_for_active_runtime(...) — 自动启动监控
//
// Go 差异：Python 在 commit_runtime_ready 中直接调用 ensure_monitor，
// Go 侧因持有 mu 锁，不能在锁内调用 EnsureMonitor（可能触发其他锁操作），
// 因此 CommitRuntimeReady 仅更新状态，调用方应在锁释放后调用 OnRuntimeReady。
func (m *TeamManager) CommitRuntimeReady(sessionID string, teamName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeSessionID = sessionID
	m.activeTeamName = teamName
	if m.pendingSessionID != "" && m.pendingSessionID == sessionID {
		m.pendingSessionID = ""
		m.pendingTeamName = ""
	}
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("team_name", teamName).
		Str("active_session_id", ptrToStr(m.activeSessionID)).
		Str("pending_session_id", ptrToStr(m.pendingSessionID)).
		Msg("commit_runtime_ready")
}

// OnRuntimeReady 运行时就绪后的自动启动逻辑。
// 对齐 Python: commit_runtime_ready 中调用 ensure_monitor_for_active_runtime
//
// 调用方在 CommitRuntimeReady 之后调用此方法，自动启动监控和演进 watcher。
// 此方法不在 CommitRuntimeReady 内部调用，避免死锁。
func (m *TeamManager) OnRuntimeReady(ctx context.Context, sessionID string, teamAgent *agent.TeamAgent, hideDM bool) {
	if teamAgent == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("OnRuntimeReady: teamAgent 为 nil，跳过监控启动")
		return
	}

	// 对齐 Python: ensure_monitor_for_active_runtime(channel_id, session_id, team_name, hide_dm)
	if err := m.EnsureMonitor(ctx, sessionID, teamAgent, hideDM); err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("OnRuntimeReady: EnsureMonitor 失败")
	}

	// 自动启动 evolution watcher — ⤵️ 待回填
	// Python: 当 runtime_ready 事件触发时，start_team_evolution_watcher 自动启动
	// Go 差异：event bus 完整实现后，由 runtime_ready 事件自动触发
	logger.Debug(logComponent).Str("session_id", sessionID).
		Msg("OnRuntimeReady: evolution watcher 自动启动待回填")
}

// ClearPendingRuntime 清除指定 session 的 pending 状态。
// 对齐 Python: TeamManager.clear_pending_runtime(session_id)
func (m *TeamManager) ClearPendingRuntime(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pendingSessionID != "" && m.pendingSessionID == sessionID {
		m.pendingSessionID = ""
		m.pendingTeamName = ""
	}
}

// ClearActiveRuntime 清除指定 session 的 active 状态。
// 对齐 Python: TeamManager.clear_active_runtime(session_id)
func (m *TeamManager) ClearActiveRuntime(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeSessionID != "" && m.activeSessionID == sessionID {
		m.activeSessionID = ""
		m.activeTeamName = ""
	}
}

// PrepareRuntimeActivation 准备运行时激活：停止非目标 session → 设置 pending。
// 对齐 Python: TeamManager.prepare_runtime_activation(session_id, team_name)
//
// Python 步骤：
//  1. await self.prepare_session_switch(session_id, reason="switch runtime: ")
//  2. async with self._lock: self._pending_session_id = session_id; self._pending_team_name = team_name
func (m *TeamManager) PrepareRuntimeActivation(ctx context.Context, sessionID string, teamName string) error {
	if err := m.PrepareSessionSwitch(ctx, sessionID, "switch runtime: "); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingSessionID = sessionID
	m.pendingTeamName = teamName
	return nil
}

// PrepareSessionSwitch 停止其他活跃/等待的 team runtime 后切换 session。
// 对齐 Python: TeamManager.prepare_session_switch(target_session_id, reason)
//
// Python 步骤：
//  1. async with self._lock: 收集 stale_sessions (active != target, pending != target)
//  2. logger.info("prepare_session_switch target=%s active=%s pending=%s stale=%s", ...)
//  3. for stale_session_id in dict.fromkeys(stale_sessions):
//     await self.stop_session_runtime(stale_session_id, reason=reason)
func (m *TeamManager) PrepareSessionSwitch(ctx context.Context, targetSessionID string, reason string) error {
	m.mu.Lock()
	var staleSessions []string
	if m.activeSessionID != "" && m.activeSessionID != targetSessionID {
		staleSessions = append(staleSessions, m.activeSessionID)
	}
	if m.pendingSessionID != "" && m.pendingSessionID != targetSessionID {
		staleSessions = append(staleSessions, m.pendingSessionID)
	}
	logger.Info(logComponent).
		Str("reason", reason).
		Str("target_session_id", targetSessionID).
		Str("active_session_id", ptrToStr(m.activeSessionID)).
		Str("pending_session_id", ptrToStr(m.pendingSessionID)).
		Strs("stale_sessions", staleSessions).
		Msg("prepare_session_switch")
	m.mu.Unlock()

	// 去重后逐个停止
	seen := make(map[string]bool)
	for _, sid := range staleSessions {
		if seen[sid] {
			continue
		}
		seen[sid] = true
		m.StopSessionRuntime(ctx, sid, reason) //nolint:errcheck // 清理操作，错误不可操作
	}
	return nil
}

// CreateTeam 创建 TeamAgent 实例。
// 对齐 Python: TeamManager.create_team(session_id, deep_agent, ...)
//
// Python 步骤：
//  1. config_base = get_config()
//  2. await self._ensure_postgresql_for_leader(config_base)
//  3. spec = self._load_team_spec(session_id)
//  4. self._apply_session_scoped_team_name(spec, session_id=session_id)
//  5. spec.agent_customizer = self.build_agent_customizer(...)
//  6. token = set_session_id(session_id)
//  7. try: team_agent = spec.build(); self._team_agents[session_id] = team_agent
//  8. self.ensure_team_shared_skills_initialized(spec)
//  9. if distributed: attach distributed hooks
//
// 10. finally: reset_session_id(token)
//
// Go 差异：Python 的 _ensure_postgresql、distributed hooks 等在 Go 中尚未完全实现，
// 方法签名保留参数但部分步骤暂时跳过，待后续回填。
func (m *TeamManager) CreateTeam(
	ctx context.Context,
	sessionID string,
	deepAgent interfaces.DeepAgentInterface,
	spec *atschema.TeamAgentSpec,
) (*agent.TeamAgent, error) {
	logger.Info(logComponent).Str("session_id", sessionID).Msg("创建 TeamAgent（应用层）")

	// 步骤 3-4: 加载 spec + session 作用域 team_name — 由调用方提前完成
	// 步骤 5: agent_customizer — 由调用方提前完成

	// 步骤 7: spec.build() — 对齐 Python: team_agent = spec.build(); self._team_agents[session_id] = team_agent
	// Go 差异：因循环依赖，spec.Build() 不能直接返回 *agent.TeamAgent，
	// 因此在 CreateTeam 中完成 NewTeamAgent + Configure 构建。
	if spec == nil {
		return nil, fmt.Errorf("spec 不能为 nil")
	}
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("spec 校验失败: %w", err)
	}

	// 构造 AgentCard
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID(fmt.Sprintf("%s_leader", spec.TeamName)),
		agentschema.WithAgentName(spec.Leader.MemberName),
		agentschema.WithAgentDescription(spec.Leader.Persona),
	)

	// NewTeamAgent + Configure
	teamAgent := agent.NewTeamAgent(card)
	runtimeCtx := atschema.TeamRuntimeContext{
		MemberName: spec.Leader.MemberName,
		Persona:    spec.Leader.Persona,
		Role:       atschema.TeamRoleLeader,
	}
	teamAgent.Configure(ctx, *spec, runtimeCtx)

	// 注册到 teamAgents
	m.mu.Lock()
	m.teamAgents[sessionID] = teamAgent
	m.mu.Unlock()

	// 步骤 8: ensure_team_shared_skills_initialized — ⤵️(#9.72) 待回填
	// 步骤 9: attach distributed hooks — ⤵️(#9.72) 待回填

	logger.Info(logComponent).Str("session_id", sessionID).Str("team_name", spec.TeamName).
		Msg("Team 已创建")
	return teamAgent, nil
}

// GetOrCreateTeam 获取或创建 TeamAgent 实例。
// 对齐 Python: TeamManager.get_or_create_team(session_id, deep_agent, ...)
//
// Python 步骤：
//  1. async with self._lock:
//  2. team_agent = self._team_agents.get(session_id)
//  3. if team_agent is not None: return team_agent
//  4. await self._destroy_other_sessions(session_id)
//  5. return await self.create_team(...)
func (m *TeamManager) GetOrCreateTeam(
	ctx context.Context,
	sessionID string,
	deepAgent interfaces.DeepAgentInterface,
	spec *atschema.TeamAgentSpec,
) (*agent.TeamAgent, error) {
	m.mu.Lock()
	teamAgent, ok := m.teamAgents[sessionID]
	if ok && teamAgent != nil {
		m.mu.Unlock()
		return teamAgent, nil
	}
	m.mu.Unlock()

	m.destroyOtherSessions(ctx, sessionID)
	return m.CreateTeam(ctx, sessionID, deepAgent, spec)
}

// DestroyTeam 销毁指定 session 的 TeamAgent。
// 对齐 Python: TeamManager.destroy_team(session_id)
//
// Python 步骤：
//  1. async with self._lock: return await self._destroy_team(session_id)
func (m *TeamManager) DestroyTeam(ctx context.Context, sessionID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.destroyTeam(ctx, sessionID)
}

// CleanupAll 清理所有 TeamAgent。
// 对齐 Python: TeamManager.cleanup_all()
//
// Python 步骤：
//  1. async with self._lock: session_ids = list(self._team_agents.keys())
//  2. for session_id in session_ids: await self._destroy_team(session_id)
func (m *TeamManager) CleanupAll(ctx context.Context) {
	m.mu.Lock()
	sessionIDs := make([]string, 0, len(m.teamAgents))
	for sid := range m.teamAgents {
		sessionIDs = append(sessionIDs, sid)
	}
	m.mu.Unlock()

	for _, sid := range sessionIDs {
		m.destroyTeam(ctx, sid) //nolint:errcheck // 清理操作，错误不可操作
	}
	logger.Info(logComponent).Msg("所有 team 已清理")
}

// TerminateSessionRuntime 终结指定 session 的运行时。
// 对齐 Python: TeamManager.terminate_session_runtime(session_id, reason)
//
// Python 步骤：
//  1. async with self._lock: 检查 has_stream_task / has_team_runtime
//  2. team_name = self._resolve_session_team_name(session_id)
//  3. if team_name: await Runner.stop_agent_team(team_name=, session_id=)
//  4. if has_local_team_runtime: cleaned = await self._destroy_team(session_id)
//  5. await self._cleanup_runtime_locals(session_id)
//  6. self.clear_active_runtime(session_id); self.clear_pending_runtime(session_id)
func (m *TeamManager) TerminateSessionRuntime(ctx context.Context, sessionID string, reason string) (bool, error) {
	m.mu.Lock()
	hasStreamTask := m.HasStreamTask(sessionID)
	hasLocalTeamRuntime := m.hasLocalTeamRuntime(sessionID)
	hasTeamRuntime := hasLocalTeamRuntime || m.hasMonitor(sessionID) ||
		(m.activeSessionID != "" && m.activeSessionID == sessionID) ||
		(m.pendingSessionID != "" && m.pendingSessionID == sessionID)
	if !hasStreamTask && !hasTeamRuntime {
		m.mu.Unlock()
		return false, nil
	}
	logger.Info(logComponent).Str("reason", reason).Str("session_id", sessionID).
		Msg("终结 team session 运行时")

	// 解析 team_name
	teamName := m.resolveSessionTeamName(sessionID)
	m.mu.Unlock()

	// 停止 Runner-owned runtime
	cleaned := false
	if teamName != "" {
		// Python: await Runner.stop_agent_team(team_name=team_name, session_id=session_id)
		if stopped, err := runner.StopAgentTeam(ctx, teamName, sessionID); err != nil {
			logger.Warn(logComponent).Str("team_name", teamName).Str("session_id", sessionID).
				Err(err).Msg("停止 Runner-owned runtime 失败")
		} else {
			logger.Info(logComponent).Str("team_name", teamName).Str("session_id", sessionID).
				Bool("stopped", stopped).Msg("已停止 Runner-owned runtime")
		}
	}

	if hasLocalTeamRuntime {
		c, _ := m.destroyTeam(ctx, sessionID)
		cleaned = c
	}

	m.cleanupRuntimeLocals(ctx, sessionID)
	m.ClearActiveRuntime(sessionID)
	m.ClearPendingRuntime(sessionID)

	logger.Info(logComponent).Str("reason", reason).Str("session_id", sessionID).
		Bool("cleaned", cleaned).Msg("team session 已终结")
	return true, nil
}

// CancelSessionRuntime 取消指定 session 的运行时（从 Runner pool 移除）。
// 对齐 Python: TeamManager.cancel_session_runtime(session_id, reason)
//
// Python 步骤：
//  1. async with self._lock: 检查 has_stream_task / has_team_runtime
//  2. team_name = self._resolve_session_team_name(session_id)
//  3. if team_name: runner_stopped = await Runner.stop_agent_team(...)
//  4. await self._cleanup_runtime_locals(session_id)
//  5. self.clear_active_runtime(session_id); self.clear_pending_runtime(session_id)
func (m *TeamManager) CancelSessionRuntime(ctx context.Context, sessionID string, reason string) (bool, error) {
	m.mu.Lock()
	hasStreamTask := m.HasStreamTask(sessionID)
	hasLocalTeamRuntime := m.hasLocalTeamRuntime(sessionID)
	hasTeamRuntime := hasLocalTeamRuntime || m.hasMonitor(sessionID) ||
		(m.activeSessionID != "" && m.activeSessionID == sessionID) ||
		(m.pendingSessionID != "" && m.pendingSessionID == sessionID)
	if !hasStreamTask && !hasTeamRuntime {
		m.mu.Unlock()
		return false, nil
	}
	logger.Info(logComponent).Str("reason", reason).Str("session_id", sessionID).
		Msg("取消 team session 运行时")

	teamName := m.resolveSessionTeamName(sessionID)
	m.mu.Unlock()

	// 停止 Runner-owned runtime
	runnerStopped := false
	if teamName != "" {
		// Python: await Runner.stop_agent_team(team_name=team_name, session_id=session_id)
		if stopped, err := runner.StopAgentTeam(ctx, teamName, sessionID); err != nil {
			logger.Warn(logComponent).Str("team_name", teamName).Str("session_id", sessionID).
				Err(err).Msg("停止 Runner-owned runtime 失败")
		} else {
			runnerStopped = stopped
		}
	}

	m.cleanupRuntimeLocals(ctx, sessionID)
	m.ClearActiveRuntime(sessionID)
	m.ClearPendingRuntime(sessionID)

	logger.Info(logComponent).Str("reason", reason).Str("session_id", sessionID).
		Bool("runner_stopped", runnerStopped).Msg("team session 已取消")
	return true, nil
}

// StopSessionRuntime 停止指定 session 的运行时（保留持久化数据）。
// 对齐 Python: TeamManager.stop_session_runtime(session_id, reason)
//
// Python 步骤：
//  1. async with self._lock: 检查 has_stream_task / has_team_runtime
//  2. team_agent = self._team_agents.pop(session_id) if has_local else None
//  3. await self._cleanup_runtime_locals(session_id)
//  4. if has_local and team_agent: stopped = await self._stop_local_team_runtime(session_id, team_agent)
//  5. team_name = self._resolve_session_team_name(session_id)
//  6. if team_name: runner_stopped = await Runner.stop_agent_team(...)
//  7. if not has_local: release a2x + stop runner transport
//  8. self.clear_active_runtime(session_id); self.clear_pending_runtime(session_id)
func (m *TeamManager) StopSessionRuntime(ctx context.Context, sessionID string, reason string) (bool, error) {
	m.mu.Lock()
	hasStreamTask := m.HasStreamTask(sessionID)
	hasLocalTeamRuntime := m.hasLocalTeamRuntime(sessionID)
	hasTeamRuntime := hasLocalTeamRuntime || m.hasRunnerTeamAgent(sessionID) || m.hasMonitor(sessionID) ||
		(m.activeSessionID != "" && m.activeSessionID == sessionID) ||
		(m.pendingSessionID != "" && m.pendingSessionID == sessionID)
	if !hasStreamTask && !hasTeamRuntime {
		m.mu.Unlock()
		return false, nil
	}

	logger.Info(logComponent).Str("reason", reason).Str("session_id", sessionID).
		Msg("停止 team session 运行时")

	var teamAgent *agent.TeamAgent
	if hasLocalTeamRuntime {
		teamAgent = m.teamAgents[sessionID]
		delete(m.teamAgents, sessionID)
	}
	m.mu.Unlock()

	m.cleanupRuntimeLocals(ctx, sessionID)

	stopped := false
	if hasLocalTeamRuntime && teamAgent != nil {
		stopped = m.stopLocalTeamRuntime(ctx, sessionID, teamAgent)
	}

	teamName := m.resolveSessionTeamName(sessionID)
	if teamName != "" {
		// Python: await Runner.stop_agent_team(team_name=team_name, session_id=session_id)
		if _, err := runner.StopAgentTeam(ctx, teamName, sessionID); err != nil {
			logger.Warn(logComponent).Str("team_name", teamName).Str("session_id", sessionID).
				Err(err).Msg("停止 Runner-owned runtime 失败")
		}
	}

	if !hasLocalTeamRuntime {
		// 释放 A2X 资源 + 停止 Runner transport — ⤵️(#9.72) 待回填
		m.mu.Lock()
		delete(m.runnerTeamAgents, sessionID)
		m.mu.Unlock()
	}

	m.ClearActiveRuntime(sessionID)
	m.ClearPendingRuntime(sessionID)

	logger.Info(logComponent).Str("reason", reason).Str("session_id", sessionID).
		Bool("stopped", stopped).Msg("team session 已停止")
	return true, nil
}

// PauseSessionRuntime 暂停指定 session 的运行时。
// 对齐 Python: TeamManager.pause_session_runtime(session_id, reason)
//
// Python 步骤：
//  1. async with self._lock: 检查 has_stream_task / has_team_runtime
//  2. team_name = self._resolve_session_team_name(session_id)
//  3. if team_name: runner_paused = await Runner.pause_agent_team(...)
//  4. await self._cleanup_runtime_locals(session_id)
//  5. self.clear_active_runtime(session_id); self.clear_pending_runtime(session_id)
func (m *TeamManager) PauseSessionRuntime(ctx context.Context, sessionID string, reason string) (bool, error) {
	m.mu.Lock()
	hasStreamTask := m.HasStreamTask(sessionID)
	hasLocalTeamRuntime := m.hasLocalTeamRuntime(sessionID)
	hasTeamRuntime := hasLocalTeamRuntime || m.hasMonitor(sessionID) ||
		(m.activeSessionID != "" && m.activeSessionID == sessionID) ||
		(m.pendingSessionID != "" && m.pendingSessionID == sessionID)
	if !hasStreamTask && !hasTeamRuntime {
		m.mu.Unlock()
		return false, nil
	}

	logger.Info(logComponent).Str("reason", reason).Str("session_id", sessionID).
		Msg("暂停 team session 运行时")

	teamName := m.resolveSessionTeamName(sessionID)
	m.mu.Unlock()

	runnerPaused := false
	if teamName != "" {
		// Python: await Runner.pause_agent_team(team_name=team_name, session_id=session_id)
		if paused, err := runner.PauseAgentTeam(ctx, teamName, sessionID); err != nil {
			logger.Warn(logComponent).Str("team_name", teamName).Str("session_id", sessionID).
				Err(err).Msg("暂停 Runner-owned runtime 失败")
		} else {
			runnerPaused = paused
		}
	}

	m.cleanupRuntimeLocals(ctx, sessionID)
	m.ClearActiveRuntime(sessionID)
	m.ClearPendingRuntime(sessionID)

	logger.Info(logComponent).Str("reason", reason).Str("session_id", sessionID).
		Bool("runner_paused", runnerPaused).Msg("team session 已暂停")
	return true, nil
}

// DeleteSessionRuntime 删除指定 session 的运行时及其持久化数据。
// 对齐 Python: TeamManager.delete_session_runtime(session_id, reason)
//
// Python 步骤：
//  1. team_name = self._resolve_delete_session_team_name(session_id)
//  2. await self.stop_session_runtime(session_id, reason=reason)
//  3. if team_name: await Runner.delete_agent_team(team_name=, session_ids=, force=True)
//  4. else: await Runner.release(session_id)
func (m *TeamManager) DeleteSessionRuntime(ctx context.Context, sessionID string, reason string) (bool, error) {
	teamName := m.resolveDeleteSessionTeamName(sessionID)

	m.StopSessionRuntime(ctx, sessionID, reason) //nolint:errcheck // 清理操作，错误不可操作

	if teamName != "" {
		// Python: await Runner.delete_agent_team(team_name=team_name, session_ids=[session_id], force=True)
		if _, err := runner.DeleteAgentTeam(ctx, teamName, []string{sessionID}, true); err != nil {
			logger.Warn(logComponent).Str("team_name", teamName).Str("session_id", sessionID).
				Err(err).Msg("删除 Runner-owned team 失败")
		}
	} else {
		logger.Warn(logComponent).Str("session_id", sessionID).
			Msg("无法解析 team_name，回退到 session release")
		// Python: await Runner.release(session_id)
		if err := runner.Release(ctx, sessionID, false); err != nil {
			logger.Warn(logComponent).Str("session_id", sessionID).Err(err).
				Msg("回退 session release 失败")
		}
	}

	logger.Info(logComponent).Str("reason", reason).Str("session_id", sessionID).
		Str("team_name", teamName).Msg("team session 已删除")
	return true, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSessionTeamName 解析指定 session 的 team_name。
// 对齐 Python: TeamManager._resolve_session_team_name(session_id)
//
// Python 步骤：
//  1. if active_session_id == session_id and active_team_name: return active_team_name
//  2. if pending_session_id == session_id and pending_team_name: return pending_team_name
//  3. metadata = get_session_metadata(session_id); team_name = metadata.get("team_name")
//  4. if team_name: return team_name
//  5. logger.warning("failed to resolve team_name"); return None
func (m *TeamManager) resolveSessionTeamName(sessionID string) string {
	if m.activeSessionID != "" && m.activeSessionID == sessionID && m.activeTeamName != "" {
		return m.activeTeamName
	}
	if m.pendingSessionID != "" && m.pendingSessionID == sessionID && m.pendingTeamName != "" {
		return m.pendingTeamName
	}
	// Step 3: 从 session metadata 获取 team_name
	// 对齐 Python: metadata = get_session_metadata(session_id); team_name = str(metadata.get("team_name") or "").strip()
	if metadata := session.GetSessionMetadata(sessionID); metadata != nil {
		if teamName, ok := metadata["team_name"].(string); ok && teamName != "" {
			return teamName
		}
	}
	logger.Warn(logComponent).Str("session_id", sessionID).
		Msg("无法从 active/pending/metadata 解析 team_name")
	return ""
}

// resolveDeleteSessionTeamName 从 session metadata 解析用于删除的 team_name。
// 对齐 Python: TeamManager._resolve_delete_session_team_name(session_id)
//
// Python 步骤：
//  1. metadata = get_session_metadata(session_id)
//  2. team_name = str(metadata.get("team_name") or "").strip()
//  3. if team_name: return team_name
//  4. logger.warning("failed to resolve delete team_name"); return None
func (m *TeamManager) resolveDeleteSessionTeamName(sessionID string) string {
	if metadata := session.GetSessionMetadata(sessionID); metadata != nil {
		if teamName, ok := metadata["team_name"].(string); ok && teamName != "" {
			return teamName
		}
	}
	logger.Warn(logComponent).Str("session_id", sessionID).
		Msg("无法从 metadata 解析 delete team_name")
	return ""
}

// destroyOtherSessions 销毁非当前 session 的所有 TeamAgent。
// 对齐 Python: TeamManager._destroy_other_sessions(current_session_id)
func (m *TeamManager) destroyOtherSessions(ctx context.Context, currentSessionID string) {
	m.mu.Lock()
	var staleIDs []string
	for sid := range m.teamAgents {
		if sid != currentSessionID {
			staleIDs = append(staleIDs, sid)
		}
	}
	m.mu.Unlock()

	for _, sid := range staleIDs {
		m.destroyTeam(ctx, sid) //nolint:errcheck // 清理操作，错误不可操作
	}
}

// destroyTeam 销毁指定 session 的 TeamAgent（内部实现，不加锁）。
// 对齐 Python: TeamManager._destroy_team(session_id)
//
// Python 步骤：
//  1. await self._cleanup_runtime_locals(session_id)
//  2. team_agent = self._team_agents.pop(session_id)
//  3. if team_agent is None: return False
//  4. cleaned = await team_agent.destroy_team(force=True)
//  5. await release_a2x_reservations_for_session(session_id, team_agent=)
//  6. await _stop_team_messager(team_agent, session_id=)
//  7. return cleaned
func (m *TeamManager) destroyTeam(ctx context.Context, sessionID string) (bool, error) {
	m.cleanupRuntimeLocals(ctx, sessionID)

	m.mu.Lock()
	teamAgent, ok := m.teamAgents[sessionID]
	if ok {
		delete(m.teamAgents, sessionID)
	}
	m.mu.Unlock()

	if !ok || teamAgent == nil {
		logger.Info(logComponent).Str("session_id", sessionID).Msg("内存中无 team 实例")
		return false, nil
	}

	// Python: cleaned = await team_agent.destroy_team(force=True)
	// S-04: 对齐 Python，注入 session_id 到 context（Python 用 set_session_id）
	cleaned := false
	if teamAgent != nil {
		// 注入 sessionID 到 ctx，对齐 Python: token = set_session_id(session_id)
		ss := sessionctx.InitSessionState()
		ss.SetSessionID(sessionID)
		destroyCtx := sessionctx.WithSessionState(ctx, ss)
		c, err := teamAgent.DestroyTeam(destroyCtx, true)
		if err != nil {
			logger.Error(logComponent).Str("session_id", sessionID).Err(err).Msg("DestroyTeam 失败")
		}
		cleaned = c
	}

	// Python: await release_a2x_reservations_for_session + await _stop_team_messager
	// ⤵️(#9.72) 待回填

	logger.Info(logComponent).Str("session_id", sessionID).Bool("cleaned", cleaned).
		Msg("team 已通过核心 API 清理")
	return cleaned, nil
}

// stopLocalTeamRuntime 停止本地 team runtime。
// 对齐 Python: TeamManager._stop_local_team_runtime(session_id, team_agent)
func (m *TeamManager) stopLocalTeamRuntime(ctx context.Context, sessionID string, teamAgent *agent.TeamAgent) bool {
	stopped := false
	// Python: stop_coordination = getattr(team_agent, "stop_coordination", None)
	// Python: if callable(stop_coordination): await stop_coordination(); stopped = True
	if teamAgent != nil {
		if err := teamAgent.StopCoordination(ctx); err != nil {
			logger.Warn(logComponent).Str("session_id", sessionID).Err(err).Msg("StopCoordination 失败")
		} else {
			stopped = true
		}
	}

	// Python: await release_a2x_reservations_for_session(session_id, team_agent=)
	// Python: await _stop_team_messager(team_agent, session_id=session_id)
	// ⤵️(#9.72) 待回填

	logger.Info(logComponent).Str("session_id", sessionID).Bool("stopped", stopped).
		Msg("停止本地 team runtime")
	return stopped
}

// hasMonitor 检查指定 session 是否有监控 handler。
func (m *TeamManager) hasMonitor(sessionID string) bool {
	_, ok := m.teamMonitors[sessionID]
	return ok
}

// hasRunnerTeamAgent 检查指定 session 是否有 Runner 池引用。
func (m *TeamManager) hasRunnerTeamAgent(sessionID string) bool {
	_, ok := m.runnerTeamAgents[sessionID]
	return ok
}
