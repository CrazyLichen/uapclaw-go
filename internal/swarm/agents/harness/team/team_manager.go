package team

import (
	"context"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMonitorHandler 团队监控 handler 接口。
// 对齐 Python: TeamMonitorHandler (jiwenswarm/agents/harness/team/monitor_handler.py)
//
// 提供团队事件流的生命周期管理、事件消费和快照查询。
type TeamMonitorHandler interface {
	// Stop 停止监控，取消后台事件收集，清理资源。
	// 对齐 Python: TeamMonitorHandler.stop()
	Stop(ctx context.Context) error
	// IsRunning 返回 handler 是否正在运行。
	// 对齐 Python: TeamMonitorHandler.is_running
	IsRunning() bool
	// Events 返回前端事件流 channel，供消费者 range 遍历。
	// 对齐 Python: TeamMonitorHandler.events() -> AsyncIterator[dict[str, Any]]
	Events() <-chan map[string]any
	// GetTeamSnapshot 获取团队快照（成员+任务聚合视图）。
	// 对齐 Python: TeamMonitorHandler.get_team_snapshot() -> dict[str, Any] | None
	GetTeamSnapshot(ctx context.Context) (map[string]any, error)
	// TeamID 返回团队标识。
	// M-06: 对齐 Python: TeamMonitorHandler.team_id property
	TeamID() string
}

// LiveRailEntry 活跃 Rail 实例及其所有者。
// 对齐 Python: TeamManager._team_live_rails 条目 (agent, rail) 二元组
type LiveRailEntry struct {
	// Agent Rail 所属的 Agent
	Agent interfaces.DeepAgentInterface
	// Rail Rail 实例
	Rail agentinterfaces.AgentRail
}

// SkillSyncTarget 技能同步目录对。
// 对齐 Python: TeamManager._team_skill_sync_targets 条目 (source: Path, target: Path)
type SkillSyncTarget struct {
	// Source 源目录
	Source string
	// Target 目标目录
	Target string
}

// TeamManager 管理一个 channel 下的所有 TeamAgent 运行时。
// 对齐 Python: jiuwenswarm/agents/harness/team/team_manager.py
//
// 每个 channel 拥有独立的 TeamManager 实例，通过 GetTeamManager(channelID) 获取。
// TeamManager 管理 session 级的活跃状态、Rail 生命周期、技能同步、流任务等应用层职责，
// 通过 TeamRuntimeManager（核心层）间接访问 Pool 执行实际的 interact/activate/finalize 操作。
type TeamManager struct {
	mu sync.Mutex

	// channelID 本 Manager 所属的 channel 标识
	channelID string

	// team agent 引用
	// 对齐 Python: _team_agents, _runner_team_agents
	teamAgents       map[string]*agent.TeamAgent // sessionID → TeamAgent（内存持有）
	runnerTeamAgents map[string]*agent.TeamAgent // sessionID → Runner 池引用

	// 活跃状态（单活跃 session 语义）
	// 对齐 Python: _active_session_id, _active_team_name, _pending_session_id, _pending_team_name
	activeSessionID  string
	activeTeamName   string
	pendingSessionID string
	pendingTeamName  string

	// 监控
	// 对齐 Python: _team_monitors
	teamMonitors map[string]TeamMonitorHandler // sessionID → TeamMonitorHandler

	// 流任务
	// 对齐 Python: _stream_tasks（Go 用 context.CancelFunc 替代 asyncio.Task）
	streamTasks map[string]*streamTaskEntry // sessionID → stream task entry

	// Rail 管理
	// 对齐 Python: _team_skill_rails, _team_member_skill_evolution_rails, _team_skill_create_rails,
	//             _team_rail_contexts, _team_live_rails, _team_skill_sync_targets
	teamSkillRails          map[string]*evolution.TeamSkillEvolutionRail // sessionID → TeamSkillEvolutionRail
	teamMemberSkillEvoRails map[string][]*evolution.SkillEvolutionRail   // sessionID → []SkillEvolutionRail
	teamSkillCreateRails    map[string]*evolution.TeamSkillCreateRail    // sessionID → TeamSkillCreateRail
	teamRailContexts        map[string]*TeamRailMountContext             // sessionID → TeamRailMountContext
	teamLiveRails           map[string][]LiveRailEntry                   // sessionID → []LiveRailEntry
	teamSkillSyncTargets    map[string]SkillSyncTarget                   // sessionID → SkillSyncTarget

	// 演进监控
	// 对齐 Python: _team_evolution_watchers
	teamEvolutionWatchers map[string]context.CancelFunc

	// 事件广播回调，由 adapter 层注入（将事件广播到 channel waiters）
	// 对齐 Python: _broadcast_event(channel_id, session_id, event)
	onEventBroadcast func(channelID, sessionID string, event map[string]any)

	// code member 配置回调，由 adapter 层注入（code 模式 team member 配置）
	// 对齐 Python: customizer 内 from interface_code import configure_code_team_member_agent（延迟 import）
	codeMemberConfigurer CodeMemberConfigurer
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件（TeamManager 使用 ComponentTeam 区分于 channel 层）
	logComponent = logger.ComponentTeam
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// teamManagers 全局 TeamManager 实例字典。
	// 对齐 Python: _team_managers: dict[str, TeamManager]
	teamManagers   = make(map[string]*TeamManager)
	teamManagersMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamManager 创建新的 TeamManager 实例。
// 对齐 Python: TeamManager.__init__
func NewTeamManager() *TeamManager {
	return &TeamManager{
		teamAgents:              make(map[string]*agent.TeamAgent),
		runnerTeamAgents:        make(map[string]*agent.TeamAgent),
		teamMonitors:            make(map[string]TeamMonitorHandler),
		streamTasks:             make(map[string]*streamTaskEntry),
		teamSkillRails:          make(map[string]*evolution.TeamSkillEvolutionRail),
		teamMemberSkillEvoRails: make(map[string][]*evolution.SkillEvolutionRail),
		teamSkillCreateRails:    make(map[string]*evolution.TeamSkillCreateRail),
		teamRailContexts:        make(map[string]*TeamRailMountContext),
		teamLiveRails:           make(map[string][]LiveRailEntry),
		teamSkillSyncTargets:    make(map[string]SkillSyncTarget),
		teamEvolutionWatchers:   make(map[string]context.CancelFunc),
	}
}

// SetOnEventBroadcast 设置事件广播回调（由 adapter 层注入）。
// 回调将 monitor/evolution 事件广播到 channel waiters。
// 对齐 Python: _broadcast_event(channel_id, session_id, event)
func (m *TeamManager) SetOnEventBroadcast(fn func(channelID, sessionID string, event map[string]any)) {
	m.onEventBroadcast = fn
}

// SetCodeMemberConfigurer 设置 code member 配置回调（由 adapter 层注入）。
// 对齐 Python: build_agent_customizer 内延迟 import configure_code_team_member_agent——
// Go 因循环依赖（team → adapter → team）改为注入回调，回调 nil 时 warning 跳过
// （对齐 Python import 失败 except 跳过语义）。
func (m *TeamManager) SetCodeMemberConfigurer(fn CodeMemberConfigurer) {
	m.codeMemberConfigurer = fn
}

// GetTeamManager 获取指定 channel 的 TeamManager 实例（懒创建）。
// 对齐 Python: get_team_manager(channel_id)
func GetTeamManager(channelID string) *TeamManager {
	resolved := strings.TrimSpace(channelID)
	if resolved == "" {
		resolved = "default"
	}
	teamManagersMu.RLock()
	mgr, ok := teamManagers[resolved]
	teamManagersMu.RUnlock()
	if ok {
		return mgr
	}
	teamManagersMu.Lock()
	defer teamManagersMu.Unlock()
	// 二次检查（double-check locking 模式）
	if mgr, ok = teamManagers[resolved]; ok {
		return mgr
	}
	mgr = NewTeamManager()
	mgr.channelID = resolved
	teamManagers[resolved] = mgr
	return mgr
}

// FindTeamSkillRailAcrossManagers 在所有 channel 的 TeamManager 中查找拥有指定 requestID 的 TeamSkillEvolutionRail。
// 对齐 Python: find_team_skill_rail_across_managers(request_id)
func FindTeamSkillRailAcrossManagers(requestID string) *evolution.TeamSkillEvolutionRail {
	teamManagersMu.RLock()
	defer teamManagersMu.RUnlock()
	for _, mgr := range teamManagers {
		rail := mgr.FindTeamSkillRailForRequest(requestID)
		if rail != nil {
			return rail
		}
	}
	return nil
}

// SyncTeamSkillsAcrossManagers 在所有 channel 的 TeamManager 中同步指定 session 的技能。
// 对齐 Python: sync_team_skills_across_managers(session_id)
func SyncTeamSkillsAcrossManagers(sessionID string) bool {
	teamManagersMu.RLock()
	defer teamManagersMu.RUnlock()
	for _, mgr := range teamManagers {
		if mgr.HasTeamSkillSyncTarget(sessionID) {
			mgr.SyncTeamSkills(sessionID)
			return true
		}
	}
	return false
}

// CancelAllTeamStreamTasksAcrossManagers 取消所有 channel 的流任务。
// 对齐 Python: cancel_all_team_stream_tasks_across_managers(reason)
func CancelAllTeamStreamTasksAcrossManagers(reason string) {
	teamManagersMu.RLock()
	managers := make([]*TeamManager, 0, len(teamManagers))
	for _, mgr := range teamManagers {
		managers = append(managers, mgr)
	}
	teamManagersMu.RUnlock()
	for _, mgr := range managers {
		mgr.CancelAllStreamTasks(reason)
	}
}

// StopTeamSessionRuntimeAcrossManagers 在所有 channel 中停止指定 session 的运行时。
// 对齐 Python: stop_team_session_runtime_across_managers(session_id, reason)
func StopTeamSessionRuntimeAcrossManagers(ctx context.Context, sessionID string, reason string) bool {
	teamManagersMu.RLock()
	managers := make([]*TeamManager, 0, len(teamManagers))
	for _, mgr := range teamManagers {
		managers = append(managers, mgr)
	}
	teamManagersMu.RUnlock()
	stopped := false
	for _, mgr := range managers {
		if s, _ := mgr.StopSessionRuntime(ctx, sessionID, reason); s {
			stopped = true
		}
	}
	return stopped
}

// GetAllTeamManagers 返回所有 channel 的 TeamManager 快照。
// 对齐 Python: get_all_team_managers()
func GetAllTeamManagers() []*TeamManager {
	teamManagersMu.RLock()
	defer teamManagersMu.RUnlock()
	result := make([]*TeamManager, 0, len(teamManagers))
	for _, mgr := range teamManagers {
		result = append(result, mgr)
	}
	return result
}

// ResetTeamManager 重置指定 channel 的 TeamManager，channelID 为空时清空全部。
// 对齐 Python: reset_team_manager(channel_id)
func ResetTeamManager(channelID string) {
	teamManagersMu.Lock()
	defer teamManagersMu.Unlock()
	if channelID == "" {
		teamManagers = make(map[string]*TeamManager)
		return
	}
	resolved := strings.TrimSpace(channelID)
	if resolved == "" {
		resolved = "default"
	}
	delete(teamManagers, resolved)
}

// ActiveSessionID 返回当前活跃的 session ID。
// 对齐 Python: TeamManager.active_session_id property
func (m *TeamManager) ActiveSessionID() string {
	return m.activeSessionID
}

// ActiveTeamName 返回当前活跃的 team 名称。
// 对齐 Python: TeamManager.active_team_name property
func (m *TeamManager) ActiveTeamName() string {
	return m.activeTeamName
}

// PendingSessionID 返回等待中的 session ID。
// 对齐 Python: TeamManager.pending_session_id property
func (m *TeamManager) PendingSessionID() string {
	return m.pendingSessionID
}

// PendingTeamName 返回等待中的 team 名称。
// 对齐 Python: TeamManager.pending_team_name property
func (m *TeamManager) PendingTeamName() string {
	return m.pendingTeamName
}

// GetTeamAgent 获取内存中的 TeamAgent 实例。
// 对齐 Python: TeamManager.get_team_agent(session_id)
//
// S-04: Python 依赖 asyncio 单线程 + GIL 无锁安全；Go map 并发读写是 fatal error，
// 导出访问器统一加锁。
func (m *TeamManager) GetTeamAgent(sessionID string) *agent.TeamAgent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.teamAgents[sessionID]
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// displayStr 将 string 转为日志展示字符串（空字符串时返回 "<nil>"）。
// T-02: 原名 ptrToStr 误导（参数是 string 而非 *string），重命名以准确反映语义。
// 对齐 Python 日志 %s 输出 None 时的 "<nil>" 观感。
func displayStr(s string) string {
	if s == "" {
		return "<nil>"
	}
	return s
}
