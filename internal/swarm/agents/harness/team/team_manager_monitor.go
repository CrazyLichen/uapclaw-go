package team

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// HasStreamTask 检查指定 session 是否有活跃的流任务。
// 对齐 Python: TeamManager.has_stream_task(session_id)
func (m *TeamManager) HasStreamTask(sessionID string) bool {
	_, ok := m.streamTasks[sessionID]
	return ok
}

// PopStreamTask 弹出指定 session 的流任务 cancel 函数。
// 对齐 Python: TeamManager.pop_stream_task(session_id)
// Go 差异：Python 返回 asyncio.Task，Go 返回 context.CancelFunc
func (m *TeamManager) PopStreamTask(sessionID string) context.CancelFunc {
	cancel, ok := m.streamTasks[sessionID]
	if !ok {
		return nil
	}
	delete(m.streamTasks, sessionID)
	return cancel
}

// RegisterStreamTask 注册流任务的 cancel 函数。
// 对齐 Python: TeamManager.register_stream_task(session_id, task)
// Go 差异：Python 注册 asyncio.Task，Go 注册 context.CancelFunc
func (m *TeamManager) RegisterStreamTask(sessionID string, cancel context.CancelFunc) {
	m.streamTasks[sessionID] = cancel
}

// CancelAllStreamTasks 取消所有流任务。
// 对齐 Python: TeamManager.cancel_all_stream_tasks(reason)
//
// Python 步骤：
//  1. async with self._lock: pending = list(self._stream_tasks.items())
//  2. for session_id, task in pending: if task.done(): continue; task.cancel()
//  3. for session_id, task in pending: if task.done(): continue; await task (catch CancelledError)
//  4. async with self._lock: self._stream_tasks.clear()
//
// Go 差异：用 cancel() 替代 task.cancel()，无需 await
func (m *TeamManager) CancelAllStreamTasks(reason string) {
	m.mu.Lock()
	pending := make(map[string]context.CancelFunc, len(m.streamTasks))
	for k, v := range m.streamTasks {
		pending[k] = v
	}
	m.mu.Unlock()

	for sessionID, cancel := range pending {
		if cancel == nil {
			continue
		}
		logger.Info(logComponent).
			Str("reason", reason).
			Str("session_id", sessionID).
			Msg("取消流任务")
		cancel()
	}

	m.mu.Lock()
	m.streamTasks = make(map[string]context.CancelFunc)
	m.mu.Unlock()
}

// GetMonitor 获取指定 session 的监控 handler。
// 对齐 Python: TeamManager.get_monitor(session_id)
func (m *TeamManager) GetMonitor(sessionID string) TeamMonitorHandler {
	return m.teamMonitors[sessionID]
}

// RegisterMonitor 注册监控 handler。
// 对齐 Python: TeamManager.register_monitor(session_id, handler)
func (m *TeamManager) RegisterMonitor(sessionID string, handler TeamMonitorHandler) {
	m.teamMonitors[sessionID] = handler
}

// GetTeamEvolutionWatcher 获取指定 session 的演进监控 cancel 函数。
// 对齐 Python: TeamManager.get_team_evolution_watcher(session_id)
// Go 差异：Python 返回 asyncio.Task，Go 返回 context.CancelFunc
func (m *TeamManager) GetTeamEvolutionWatcher(sessionID string) context.CancelFunc {
	return m.teamEvolutionWatchers[sessionID]
}

// RegisterTeamEvolutionWatcher 注册演进监控 cancel 函数。
// 对齐 Python: TeamManager.register_team_evolution_watcher(session_id, task)
// Go 差异：Python 注册 asyncio.Task，Go 注册 context.CancelFunc
func (m *TeamManager) RegisterTeamEvolutionWatcher(sessionID string, cancel context.CancelFunc) {
	m.teamEvolutionWatchers[sessionID] = cancel
}

// PopTeamEvolutionWatcher 弹出指定 session 的演进监控 cancel 函数。
// 对齐 Python: TeamManager.pop_team_evolution_watcher(session_id)
func (m *TeamManager) PopTeamEvolutionWatcher(sessionID string) context.CancelFunc {
	cancel, ok := m.teamEvolutionWatchers[sessionID]
	if !ok {
		return nil
	}
	delete(m.teamEvolutionWatchers, sessionID)
	return cancel
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasLocalTeamRuntime 判断 session 是否使用内存中的 TeamAgent 路径。
// 对齐 Python: TeamManager._has_local_team_runtime(session_id)
func (m *TeamManager) hasLocalTeamRuntime(sessionID string) bool {
	_, ok := m.teamAgents[sessionID]
	return ok
}

// cleanupRuntimeLocals 清理指定 session 的本地运行时状态。
// 对齐 Python: TeamManager._cleanup_runtime_locals(session_id)
//
// Python 步骤：
//  1. watcher_task = self._team_evolution_watchers.pop(session_id)
//     if watcher_task and not watcher_task.done(): watcher_task.cancel(); await watcher_task
//  2. stream_task = self._stream_tasks.pop(session_id)
//     if stream_task and not stream_task.done(): stream_task.cancel(); await stream_task
//  3. monitor_handler = self._team_monitors.pop(session_id)
//     if monitor_handler: await monitor_handler.stop()
//  4. self._clear_team_rail_registries(session_id)
func (m *TeamManager) cleanupRuntimeLocals(sessionID string) {
	// 步骤 1: 取消演进监控
	if cancel, ok := m.teamEvolutionWatchers[sessionID]; ok && cancel != nil {
		cancel()
		delete(m.teamEvolutionWatchers, sessionID)
		logger.Info(logComponent).Str("session_id", sessionID).Msg("演进监控已取消")
	}

	// 步骤 2: 取消流任务
	if cancel, ok := m.streamTasks[sessionID]; ok && cancel != nil {
		cancel()
		delete(m.streamTasks, sessionID)
		logger.Info(logComponent).Str("session_id", sessionID).Msg("流任务已取消")
	}

	// 步骤 3: 停止监控 handler
	// Python: await monitor_handler.stop()
	if handler, ok := m.teamMonitors[sessionID]; ok {
		if err := handler.Stop(context.Background()); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("停止监控 handler 失败")
		}
		delete(m.teamMonitors, sessionID)
		logger.Info(logComponent).Str("session_id", sessionID).Msg("监控 handler 已停止并移除")
	}

	// 步骤 4: 清理 Rail 注册
	m.clearTeamRailRegistries(sessionID)
}

// clearTeamRailRegistries 清理指定 session 的 Rail 注册。
// 对齐 Python: TeamManager._clear_team_rail_registries(session_id)
func (m *TeamManager) clearTeamRailRegistries(sessionID string) {
	delete(m.teamSkillRails, sessionID)
	delete(m.teamMemberSkillEvoRails, sessionID)
	delete(m.teamSkillCreateRails, sessionID)
	delete(m.teamRailContexts, sessionID)
	delete(m.teamLiveRails, sessionID)
	delete(m.teamSkillSyncTargets, sessionID)
}

// EnsureMonitor 为指定 session 创建并启动监控 handler。
// 对齐 Python: ensure_monitor_for_active_runtime(channel_id, session_id, team_name, hide_dm)
//
// Python 步骤：
//  1. tm = get_team_manager(channel_id)
//  2. existing = tm.get_monitor(session_id); if existing and existing.is_running: return
//  3. monitor = await Runner.get_agent_team_monitor(team_name=, session_id=, hide_dm=)
//  4. if monitor is None: logger.warning; return
//  5. monitor_handler = TeamMonitorHandler(monitor, session_id)
//  6. await monitor_handler.start()
//  7. tm.register_monitor(session_id, monitor_handler)
//  8. if monitor_handler.is_running: asyncio.create_task(_consume_monitor_events(...))
//
// Go 差异：Python 通过 Runner.get_agent_team_monitor 获取 TeamMonitor，
// Go 端直接通过 TeamAgent 创建 TeamMonitor（省略 Runner 桥接层）。
func (m *TeamManager) EnsureMonitor(ctx context.Context, sessionID string, teamAgent *agent.TeamAgent, hideDM bool) error {
	// Python 步骤 2: 检查是否已有 handler 且 is_running → 提前返回
	m.mu.Lock()
	existing := m.teamMonitors[sessionID]
	m.mu.Unlock()
	if existing != nil && existing.IsRunning() {
		return nil
	}

	// Python 步骤 3-4: 从 TeamAgent 获取 TeamBackend → db → 创建 TeamMonitor
	backend := teamAgent.TeamBackend()
	if backend == nil {
		return fmt.Errorf("team_agent 没有 team_backend")
	}
	db := backend.DB()
	teamName := backend.TeamName()

	// Python 步骤 4: 创建 TeamMonitor
	mon, err := monitor.CreateMonitor(teamAgent, db, teamName, sessionID, hideDM)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Str("team_name", teamName).
			Msg("创建 TeamMonitor 失败")
		return fmt.Errorf("创建 TeamMonitor 失败: %w", err)
	}

	// Python 步骤 5: 创建 handler
	handler := newTeamMonitorHandlerImpl(mon, sessionID)

	// Python 步骤 6: 启动 handler
	if err := handler.Start(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("TeamMonitorHandler 启动失败")
		return fmt.Errorf("TeamMonitorHandler 启动失败: %w", err)
	}

	// Python 步骤 7: 注册到 teamMonitors
	m.RegisterMonitor(sessionID, handler)
	logger.Info(logComponent).Str("session_id", sessionID).Str("team_name", teamName).
		Msg("TeamMonitorHandler 启动成功")

	// Python 步骤 8: 启动事件消费 goroutine
	if handler.IsRunning() {
		consumeCtx, cancel := context.WithCancel(context.Background())
		m.RegisterStreamTask(sessionID+"_monitor", cancel)
		go m.consumeMonitorEvents(consumeCtx, sessionID, handler)
	}

	return nil
}

// consumeMonitorEvents 消费监控事件并广播到 channel waiters。
// 对齐 Python: _consume_monitor_events(channel_id, session_id, monitor_handler)
//
// Python 步骤：
//  1. logger.info("monitor event loop started")
//  2. async for event in monitor_handler.events(): _broadcast_event(channel_id, session_id, event)
//  3. logger.info("monitor event loop ended")
//  4. except CancelledError: raise
//  5. except Exception: logger.error
//
// Go 差异：Python 的 _broadcast_event 查找 _pending_waiters 并放入每个 waiter 的 queue。
// Go 端 pending_waiters 机制待 #9.85 实现，当前仅记录日志。
func (m *TeamManager) consumeMonitorEvents(ctx context.Context, sessionID string, handler TeamMonitorHandler) {
	defer logger.Info(logComponent).Str("session_id", sessionID).Msg("监控事件消费 goroutine 退出")
	logger.Info(logComponent).Str("session_id", sessionID).Msg("监控事件消费 goroutine 启动")

	for evt := range handler.Events() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// TODO(#9.85): 广播到 channel waiters（需要 _pending_waiters 机制）
		// 当前仅记录日志，待前端通道层实现后回填
		if evt != nil {
			logger.Debug(logComponent).
				Any("event_type", evt["event_type"]).
				Str("session_id", sessionID).
				Msg("监控事件")
		}
	}
}
