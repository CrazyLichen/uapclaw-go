package team

import (
	"context"

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
func (m *TeamManager) GetMonitor(sessionID string) any {
	return m.teamMonitors[sessionID]
}

// RegisterMonitor 注册监控 handler。
// 对齐 Python: TeamManager.register_monitor(session_id, handler)
func (m *TeamManager) RegisterMonitor(sessionID string, handler any) {
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
	// Go 差异：TeamMonitorHandler 在 Go 中尚未完全对齐，先移除引用
	if _, ok := m.teamMonitors[sessionID]; ok {
		delete(m.teamMonitors, sessionID)
		logger.Info(logComponent).Str("session_id", sessionID).Msg("监控 handler 已移除")
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
