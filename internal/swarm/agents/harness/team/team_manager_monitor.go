package team

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// streamTaskEntry 流任务条目，包含 cancel 函数和可选的 WaitGroup。
// M-23: 对齐 Python 的 await task 语义，cancel 后等待 goroutine 退出。
type streamTaskEntry struct {
	// cancel 取消函数（context.WithCancel 返回的 cancel）
	cancel context.CancelFunc
	// wg 可选的 WaitGroup，goroutine 启动时 wg.Add(1)，退出时 wg.Done()
	// 调用方通过 SetStreamTaskWaitGroup 注入
	wg *sync.WaitGroup
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// HasStreamTask 检查指定 session 是否有活跃的流任务。
// 对齐 Python: TeamManager.has_stream_task(session_id)
//
// S-04: Python 依赖 asyncio 单线程 + GIL 无锁安全；Go map 并发读写是 fatal error，
// 导出访问器统一加锁。lifecycle 持锁路径改用锁内直读（见 terminate/stop/cancel/pause）。
func (m *TeamManager) HasStreamTask(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.streamTasks[sessionID]
	return ok
}

// PopStreamTask 弹出指定 session 的流任务 cancel 函数。
// 对齐 Python: TeamManager.pop_stream_task(session_id)
// Go 差异：Python 返回 asyncio.Task，Go 返回 context.CancelFunc
func (m *TeamManager) PopStreamTask(sessionID string) context.CancelFunc {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.streamTasks[sessionID]
	if !ok {
		return nil
	}
	delete(m.streamTasks, sessionID)
	if entry != nil {
		return entry.cancel
	}
	return nil
}

// RegisterStreamTask 注册流任务的 cancel 函数。
// 对齐 Python: TeamManager.register_stream_task(session_id, task)
// Go 差异：Python 注册 asyncio.Task，Go 注册 context.CancelFunc + WaitGroup
func (m *TeamManager) RegisterStreamTask(sessionID string, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streamTasks[sessionID] = &streamTaskEntry{cancel: cancel}
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
// M-23: cancel 后等待 goroutine 退出（对齐 Python 的 await task）
func (m *TeamManager) CancelAllStreamTasks(reason string) {
	m.mu.Lock()
	pending := make(map[string]*streamTaskEntry, len(m.streamTasks))
	for k, v := range m.streamTasks {
		pending[k] = v
	}
	m.mu.Unlock()

	for sessionID, entry := range pending {
		if entry == nil || entry.cancel == nil {
			continue
		}
		logger.Info(logComponent).
			Str("reason", reason).
			Str("session_id", sessionID).
			Msg("cancelling stream task")
		entry.cancel()
	}

	// M-23: 等待所有 goroutine 退出（对齐 Python 的 await task）
	for sessionID, entry := range pending {
		if entry != nil && entry.wg != nil {
			entry.wg.Wait()
			logger.Debug(logComponent).Str("session_id", sessionID).
				Msg("stream task goroutine exited")
		}
	}

	m.mu.Lock()
	m.streamTasks = make(map[string]*streamTaskEntry)
	m.mu.Unlock()
}

// GetMonitor 获取指定 session 的监控 handler。
// 对齐 Python: TeamManager.get_monitor(session_id)
//
// S-04: 导出访问器加锁（Go map 并发读写是 fatal error）。
func (m *TeamManager) GetMonitor(sessionID string) TeamMonitorHandler {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.teamMonitors[sessionID]
}

// RegisterMonitor 注册监控 handler。
// 对齐 Python: TeamManager.register_monitor(session_id, handler)
//
// S-04: 导出访问器加锁。EnsureMonitor 锁内注册已改为直写避免重入。
func (m *TeamManager) RegisterMonitor(sessionID string, handler TeamMonitorHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.teamMonitors[sessionID] = handler
}

// GetTeamEvolutionWatcher 获取指定 session 的演进监控 cancel 函数。
// 对齐 Python: TeamManager.get_team_evolution_watcher(session_id)
// Go 差异：Python 返回 asyncio.Task，Go 返回 context.CancelFunc
//
// S-04: 导出访问器加锁（Go map 并发读写是 fatal error）。
func (m *TeamManager) GetTeamEvolutionWatcher(sessionID string) context.CancelFunc {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.teamEvolutionWatchers[sessionID]
}

// RegisterTeamEvolutionWatcher 注册演进监控 cancel 函数。
// 对齐 Python: TeamManager.register_team_evolution_watcher(session_id, task)
// Go 差异：Python 注册 asyncio.Task，Go 注册 context.CancelFunc
//
// S-04: 导出访问器加锁（Go map 并发读写是 fatal error）。
func (m *TeamManager) RegisterTeamEvolutionWatcher(sessionID string, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.teamEvolutionWatchers[sessionID] = cancel
}

// PopTeamEvolutionWatcher 弹出指定 session 的演进监控 cancel 函数。
// 对齐 Python: TeamManager.pop_team_evolution_watcher(session_id)
//
// S-04: 导出访问器加锁（Go map 并发读写是 fatal error）。
func (m *TeamManager) PopTeamEvolutionWatcher(sessionID string) context.CancelFunc {
	m.mu.Lock()
	defer m.mu.Unlock()
	cancel, ok := m.teamEvolutionWatchers[sessionID]
	if !ok {
		return nil
	}
	delete(m.teamEvolutionWatchers, sessionID)
	return cancel
}

// SetStreamTaskWaitGroup 为指定 session 的流任务设置 WaitGroup。
// goroutine 启动时调用 wg.Add(1)，退出时调用 wg.Done()。
//
// S-04: 该方法读写 streamTasks map，同步加锁（报告未列的第 9 处）。
func (m *TeamManager) SetStreamTaskWaitGroup(sessionID string, wg *sync.WaitGroup) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.streamTasks[sessionID]; ok && entry != nil {
		entry.wg = wg
	}
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
			Msg("failed to create TeamMonitor")
		return fmt.Errorf("创建 TeamMonitor 失败: %w", err)
	}

	// Python 步骤 5: 创建 handler
	handler := newTeamMonitorHandlerImpl(mon, sessionID)

	// Python 步骤 6: 启动 handler
	if err := handler.Start(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("TeamMonitorHandler start failed")
		return fmt.Errorf("TeamMonitorHandler 启动失败: %w", err)
	}

	// Python 步骤 7: 注册到 teamMonitors
	// S-04: RegisterMonitor 加锁后此处改锁内直写（sync.Mutex 不可重入）
	m.mu.Lock()
	m.teamMonitors[sessionID] = handler
	m.mu.Unlock()
	logger.Info(logComponent).Str("session_id", sessionID).Str("team_name", teamName).
		Msg("TeamMonitorHandler started successfully")

	// Python 步骤 8: 启动事件消费 goroutine
	// M-05 方案 A: 对齐 Python——_consume_monitor_events 是 asyncio.create_task 的
	// detached 任务，不注册进 _stream_tasks（靠 monitor stop close event queue 自然退出）。
	// T-08: 消费者生命周期与请求 ctx 解耦（对齐 Python detached 语义），传 Background ctx。
	if handler.IsRunning() {
		go m.consumeMonitorEvents(context.Background(), sessionID, handler)
	}

	// 自动启动 evolution watcher
	// 对齐 Python: ensure_monitor_for_active_runtime 中，监控启动后自动触发 evolution watcher
	// Python: asyncio.create_task(start_team_evolution_watcher(channel_id, session_id, team_name))
	m.autoStartEvolutionWatcher(ctx, sessionID, teamName)

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasLocalTeamRuntime 判断 session 是否使用内存中的 TeamAgent 路径。
// 对齐 Python: TeamManager._has_local_team_runtime(session_id)
// S-02: Python 返回 self._is_distributed_mode(get_config()) and session_id in self._team_agents
func (m *TeamManager) hasLocalTeamRuntime(sessionID string) bool {
	// S-02: 分布式模式前置检查，对齐 Python _is_distributed_mode(get_config())
	cfg := config.GetRunnerConfig()
	if cfg == nil || !cfg.DistributedMode {
		return false
	}
	_, ok := m.teamAgents[sessionID]
	return ok
}

// cleanupRuntimeLocals 清理指定 session 的本地运行时状态。
// 对齐 Python: TeamManager._cleanup_runtime_locals(session_id)
// S-03: 增加 ctx 参数，传播 context 到 handler.Stop
//
// Python 步骤：
//  1. watcher_task = self._team_evolution_watchers.pop(session_id)
//     if watcher_task and not watcher_task.done(): watcher_task.cancel(); await watcher_task
//  2. stream_task = self._stream_tasks.pop(session_id)
//     if stream_task and not stream_task.done(): stream_task.cancel(); await stream_task
//  3. monitor_handler = self._team_monitors.pop(session_id)
//     if monitor_handler: await monitor_handler.stop()
//  4. self._clear_team_rail_registries(session_id)
func (m *TeamManager) cleanupRuntimeLocals(ctx context.Context, sessionID string) {
	// 步骤 1: 取消演进监控（锁内 pop，cancel 在锁外调用——对齐 Python pop→cancel 结构）
	var watcherCancel context.CancelFunc
	m.mu.Lock()
	if cancel, ok := m.teamEvolutionWatchers[sessionID]; ok {
		watcherCancel = cancel
		delete(m.teamEvolutionWatchers, sessionID)
	}
	m.mu.Unlock()
	if watcherCancel != nil {
		watcherCancel()
		logger.Info(logComponent).Str("session_id", sessionID).Msg("evolution monitor cancelled")
	}

	// 步骤 2: 取消流任务（锁内 pop，cancel/Wait 在锁外——Wait 可能阻塞，
	// 持锁等待会与消费端访问器互相饥饿）
	var streamEntry *streamTaskEntry
	m.mu.Lock()
	if entry, ok := m.streamTasks[sessionID]; ok {
		streamEntry = entry
		delete(m.streamTasks, sessionID)
	}
	m.mu.Unlock()
	if streamEntry != nil {
		if streamEntry.cancel != nil {
			streamEntry.cancel()
		}
		// M-23: 等待 goroutine 退出
		if streamEntry.wg != nil {
			streamEntry.wg.Wait()
		}
		logger.Info(logComponent).Str("session_id", sessionID).Msg("stream task cancelled")
	}

	// 步骤 3: 停止监控 handler（锁内 pop，Stop 在锁外——对齐 Python pop→await stop）
	var monitorHandler TeamMonitorHandler
	m.mu.Lock()
	if handler, ok := m.teamMonitors[sessionID]; ok {
		monitorHandler = handler
		delete(m.teamMonitors, sessionID)
	}
	m.mu.Unlock()
	// Python: await monitor_handler.stop()
	// S-03: 使用传入的 ctx 替代 context.Background()
	if monitorHandler != nil {
		if err := monitorHandler.Stop(ctx); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("failed to stop monitor handler")
		}
		logger.Info(logComponent).Str("session_id", sessionID).Msg("monitor handler stopped and removed")
	}

	// 步骤 4: 清理 Rail 注册（锁内批量 delete）
	m.mu.Lock()
	m.clearTeamRailRegistries(sessionID)
	m.mu.Unlock()
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
// Go 差异：Python 通过 _broadcast_event 查找 _pending_waiters 并放入每个 waiter 的 queue。
// Go 端通过 onEventBroadcast 回调实现，由 adapter 层在初始化时注入。
func (m *TeamManager) consumeMonitorEvents(ctx context.Context, sessionID string, handler TeamMonitorHandler) {
	defer logger.Info(logComponent).Str("session_id", sessionID).Msg("monitor event consumer goroutine exited")
	logger.Info(logComponent).Str("session_id", sessionID).Msg("monitor event consumer goroutine started")

	for evt := range handler.Events() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// 对齐 Python: _broadcast_event(channel_id, session_id, event)
		if evt != nil {
			if m.onEventBroadcast != nil {
				m.onEventBroadcast(m.channelID, sessionID, evt)
			} else {
				logger.Debug(logComponent).
					Any("event_type", evt["event_type"]).
					Str("session_id", sessionID).
					Msg("monitor event (no broadcast callback, log only)")
			}
		}
	}
}

// autoStartEvolutionWatcher 自动启动 evolution watcher 的占位入口。
// 对齐 Python: start_team_evolution_watcher(channel_id, session_id, team_name)
//
// Python 步骤：
//  1. 检查是否已有 watcher 在运行
//  2. 创建 asyncio task 运行 evolution watcher 循环（_watch_team_evolution_and_push）
//  3. 注册到 _team_evolution_watchers
//
// M-06: Python 的 evolution watcher 消费的是 TeamSkillEvolutionRail 的审批事件
// （drain_pending_approval_events），不是 monitor 的事件流。Go 端完整实现在
// adapter 层 ensureTeamEvolutionWatcher（watchTeamEvolutionAndPushTeam 每轮
// rail.DrainPendingApprovalEvents），此处不再重复消费 handler.Events()——
// Go channel 单事件只投递给一个消费者，双消费会与 consumeMonitorEvents 竞争，
// 各自只能看到一半事件。
// 本方法保留为日志入口 + watcher 注册占位，实际 watch 由 adapter 层驱动。
func (m *TeamManager) autoStartEvolutionWatcher(ctx context.Context, sessionID string, teamName string) {
	// 检查是否已有 watcher
	if existing := m.GetTeamEvolutionWatcher(sessionID); existing != nil {
		logger.Debug(logComponent).Str("session_id", sessionID).
			Msg("evolution watcher already exists, skipping auto-start")
		return
	}

	logger.Info(logComponent).Str("session_id", sessionID).Str("team_name", teamName).
		Msg("auto-starting evolution watcher (delegated to adapter layer)")
}
