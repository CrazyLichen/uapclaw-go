package monitor

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EventListenerRegistrar 事件监听器注册接口。
// 避免 monitor → agent 循环依赖。*agent.TeamAgent 已实现此接口。
type EventListenerRegistrar interface {
	// AddEventListener 注册事件监听器，返回句柄用于后续移除。
	AddEventListener(handler messager.MessagerHandler) *messager.EventListenerHandle
	// RemoveEventListener 移除事件监听器。
	RemoveEventListener(handle *messager.EventListenerHandle)
}

// TeamMonitor 观察一个 leader TeamAgent，用于状态查询和实时事件。
// 对齐 Python: TeamMonitor (openjiuwen/agent_teams/monitor/team_monitor.py)
//
// 生命周期：
//
//	monitor := CreateMonitor(teamAgent, ...)
//	monitor.Start(ctx)             // 开始监听
//	for evt := range monitor.Events() { ... }  // 消费事件
//	monitor.Stop(ctx)              // 清理
type TeamMonitor struct {
	// teamName 团队标识
	teamName string
	// sessionID 会话标识（用于动态表路由）
	sessionID string
	// db TeamDatabase 实例（用于状态查询）
	db database.TeamDatabase
	// teamAgent 事件监听器注册接口
	teamAgent EventListenerRegistrar
	// hideDM 是否隐藏直发消息
	hideDM bool
	// eventCh 事件 channel（缓冲 256），替代 Python asyncio.Queue
	eventCh chan *MonitorEvent
	// eventMu 保护 started 和 listenerHandle
	eventMu sync.Mutex
	// started 是否已启动
	started bool
	// listenerHandle 事件监听器句柄
	listenerHandle *messager.EventListenerHandle
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// monitorEventChSize 事件 channel 缓冲大小
	monitorEventChSize = 256
	// logComponentMon 日志组件
	logComponentMon = logger.ComponentChannel
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamMonitor 创建 TeamMonitor。
// 对齐 Python: TeamMonitor.__init__(team_name, session_id, db, team_agent, *, hide_dm=False)
func NewTeamMonitor(teamName, sessionID string, db database.TeamDatabase, teamAgent EventListenerRegistrar, hideDM bool) *TeamMonitor {
	return &TeamMonitor{
		teamName:  teamName,
		sessionID: sessionID,
		db:        db,
		teamAgent: teamAgent,
		hideDM:    hideDM,
		eventCh:   make(chan *MonitorEvent, monitorEventChSize),
	}
}

// CreateMonitor 创建绑定到 leader TeamAgent 的 TeamMonitor。
// 对齐 Python: create_monitor(team_agent, *, hide_dm=False)
//
// Python 步骤：
//  1. if team_agent.role != TeamRole.LEADER: raise ValueError
//  2. backend = team_agent.team_backend
//  3. if backend is None: raise ValueError("TeamAgent has no team_backend configured")
//  4. return TeamMonitor(team_name=backend.team_name, session_id=get_session_id(), db=backend.db, ...)
//
// Go 差异：Python create_monitor 从 team_agent 提取 team_name/session_id/db，
// Go 端调用者需显式传入这些参数（因为 Go 的 TeamAgent 角色查询方法尚未稳定）。
// 校验 teamAgent 非 nil。
func CreateMonitor(teamAgent EventListenerRegistrar, db database.TeamDatabase, teamName, sessionID string, hideDM bool) (*TeamMonitor, error) {
	if teamAgent == nil {
		return nil, fmt.Errorf("team_agent 不能为 nil")
	}
	return NewTeamMonitor(teamName, sessionID, db, teamAgent, hideDM), nil
}

// TeamName 返回团队名称。
// 对齐 Python: TeamMonitor.team_name property
func (m *TeamMonitor) TeamName() string {
	return m.teamName
}

// SessionID 返回会话 ID。
// 对齐 Python: TeamMonitor.session_id property
func (m *TeamMonitor) SessionID() string {
	return m.sessionID
}

// Start 开始监控，注册事件监听器。幂等。
// 对齐 Python: TeamMonitor.start()
//
// Python 步骤：
//  1. if self._started: return
//  2. self._team_agent.add_event_listener(self._on_event)
//  3. self._started = True
//  4. team_logger.info("TeamMonitor started for team {}", self._team_name)
func (m *TeamMonitor) Start(ctx context.Context) error {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()
	if m.started {
		return nil
	}
	handle := m.teamAgent.AddEventListener(m.onEvent)
	m.listenerHandle = handle
	m.started = true
	logger.Info(logComponentMon).Str("team_name", m.teamName).Msg("TeamMonitor 已启动")
	return nil
}

// Stop 停止监控，取消注册，终止事件流。幂等。
// 对齐 Python: TeamMonitor.stop()
//
// Python 步骤：
//  1. if not self._started: return
//  2. self._team_agent.remove_event_listener(self._on_event)
//  3. self._started = False
//  4. self._event_queue.put_nowait(None)  // sentinel
//  5. team_logger.info("TeamMonitor stopped for team {}", self._team_name)
func (m *TeamMonitor) Stop(ctx context.Context) error {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()
	if !m.started {
		return nil
	}
	if m.listenerHandle != nil {
		m.teamAgent.RemoveEventListener(m.listenerHandle)
		m.listenerHandle = nil
	}
	m.started = false
	// 发送 nil sentinel 到 eventCh，终止 Events() 迭代
	select {
	case m.eventCh <- nil:
	default:
		// channel 满时丢弃 sentinel（消费者已经退出）
	}
	// 关闭 channel，终止 range 遍历
	close(m.eventCh)
	logger.Info(logComponentMon).Str("team_name", m.teamName).Msg("TeamMonitor 已停止")
	return nil
}

// Events 返回只读事件 channel。
// 对齐 Python: TeamMonitor.events() -> AsyncIterator[MonitorEvent]
//
// Go 差异：Python 返回 AsyncIterator，Go 返回 <-chan *MonitorEvent。
// 消费者通过 `for evt := range monitor.Events()` 遍历。
// channel 关闭（Stop() 调用后）时迭代终止。
func (m *TeamMonitor) Events() <-chan *MonitorEvent {
	return m.eventCh
}

// GetTeamInfo 查询团队基本信息。
// 对齐 Python: TeamMonitor.get_team_info() -> TeamInfo | None
//
// Python 步骤：
//  1. with self._bound_session(): team = await self._db.team.get_team(self._team_name)
//  2. if team is None: return None
//  3. return TeamInfo.from_internal(team)
func (m *TeamMonitor) GetTeamInfo(ctx context.Context) (*TeamInfo, error) {
	ctx = m.boundSession(ctx)
	team, err := m.db.Team().GetTeam(ctx, m.teamName)
	if err != nil {
		return nil, err
	}
	if team == nil {
		return nil, nil
	}
	info := TeamInfo{}.FromInternal(team)
	return info, nil
}

// GetMembers 查询团队成员列表。
// 对齐 Python: TeamMonitor.get_members(status=None) -> list[MemberInfo]
//
// Python 步骤：
//  1. with self._bound_session(): members = await self._db.member.get_team_members(self._team_name, status=status)
//  2. return [MemberInfo.from_internal(m) for m in members]
func (m *TeamMonitor) GetMembers(ctx context.Context, status string) ([]*MemberInfo, error) {
	ctx = m.boundSession(ctx)
	members, err := m.db.Member().GetTeamMembers(ctx, m.teamName, status)
	if err != nil {
		return nil, err
	}
	result := make([]*MemberInfo, 0, len(members))
	for _, member := range members {
		result = append(result, MemberInfo{}.FromInternal(member))
	}
	return result, nil
}

// GetMember 查询单个成员。
// 对齐 Python: TeamMonitor.get_member(member_name) -> MemberInfo | None
//
// Python 步骤：
//  1. with self._bound_session(): member = await self._db.member.get_member(member_name, self._team_name)
//  2. if member is None: return None
//  3. return MemberInfo.from_internal(member)
func (m *TeamMonitor) GetMember(ctx context.Context, memberName string) (*MemberInfo, error) {
	ctx = m.boundSession(ctx)
	member, err := m.db.Member().GetMember(ctx, memberName, m.teamName)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, nil
	}
	info := MemberInfo{}.FromInternal(member)
	return info, nil
}

// GetTasks 查询任务列表。
// 对齐 Python: TeamMonitor.get_tasks(status=None) -> list[TaskInfo]
//
// Python 步骤：
//  1. with self._bound_session(): tasks = await self._db.task.get_team_tasks(self._team_name, status=status)
//  2. return [TaskInfo.from_internal(t) for t in tasks]
func (m *TeamMonitor) GetTasks(ctx context.Context, status string) ([]*TaskInfo, error) {
	ctx = m.boundSession(ctx)
	tasks, err := m.db.Task().GetTeamTasks(ctx, m.teamName, status)
	if err != nil {
		return nil, err
	}
	result := make([]*TaskInfo, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, TaskInfo{}.FromInternal(task))
	}
	return result, nil
}

// GetMessages 查询邮箱消息。
// 对齐 Python: TeamMonitor.get_messages(*, to_member_name=None, from_member_name=None) -> list[MessageInfo]
//
// Python 步骤：
//  1. if self._hide_dm and to_member_name is not None: return []
//  2. with self._bound_session():
//     a. if to_member_name is not None: rows = db.message.get_messages(team_name, to_member_name, from_member_name)
//     b. else: broadcast_filter = True if self._hide_dm else None; rows = db.message.get_team_messages(team_name, broadcast=broadcast_filter)
//  3. return [MessageInfo.from_internal(r) for r in rows]
func (m *TeamMonitor) GetMessages(ctx context.Context, toMemberName, fromMemberName string) ([]*MessageInfo, error) {
	// Python 步骤 1: hideDM + toMemberName → 强制返回空列表
	if m.hideDM && toMemberName != "" {
		return nil, nil
	}
	ctx = m.boundSession(ctx)
	var rows []*database.TeamMessageBase
	var err error
	if toMemberName != "" {
		// Python 步骤 2a: 点对点消息
		rows, err = m.db.Message().GetMessages(ctx, m.teamName, toMemberName, false, fromMemberName)
	} else {
		// Python 步骤 2b: 全团队消息，hideDM 时只取广播
		broadcastFilter := ""
		if m.hideDM {
			broadcastFilter = "true"
		}
		rows, err = m.db.Message().GetTeamMessages(ctx, m.teamName, broadcastFilter)
	}
	if err != nil {
		return nil, err
	}
	result := make([]*MessageInfo, 0, len(rows))
	for _, r := range rows {
		result = append(result, MessageInfo{}.FromInternal(r))
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// onEvent Messager 事件回调，注册到 TeamAgent 的事件监听器。
// 对齐 Python: TeamMonitor._on_event(event: EventMessage)
//
// Python 步骤：
//  1. monitor_event = MonitorEvent.from_event_message(event)
//  2. if monitor_event is None: return
//  3. if self._hide_dm and monitor_event.event_type == MonitorEventType.MESSAGE: return
//  4. self._event_queue.put_nowait(monitor_event)
func (m *TeamMonitor) onEvent(ctx context.Context, msg *events.EventMessage) error {
	monitorEvent := FromEventMessage(msg)
	if monitorEvent == nil {
		return nil
	}
	// hideDM 过滤：丢弃 MESSAGE 类型事件，保留 BROADCAST
	if m.hideDM && monitorEvent.EventType == MonitorEventTypeMessage {
		return nil
	}
	// 非阻塞写入 channel
	select {
	case m.eventCh <- monitorEvent:
	default:
		// channel 满时丢弃事件（避免阻塞 Messager 事件分发管线）
		logger.Warn(logComponentMon).Str("event_type", string(monitorEvent.EventType)).
			Msg("事件 channel 已满，丢弃事件")
	}
	return nil
}

// boundSession 绑定 session_id 到 context，用于动态表路由。
// 对齐 Python: TeamMonitor._bound_session() context manager
//
// Python 步骤：
//  1. if self._session_id and get_session_id() != self._session_id:
//     token = set_session_id(self._session_id)
//  2. yield (执行查询)
//  3. finally: if token: reset_session_id(token)
//
// Go 差异：Python 用 contextvar 在 async 调用链中隐式传播 session_id，
// Go 用 context.Value 显式传播。本方法创建/更新 SessionState 注入到 ctx 中。
func (m *TeamMonitor) boundSession(ctx context.Context) context.Context {
	if m.sessionID == "" {
		return ctx
	}
	state := sessionctx.SessionStateFromCtx(ctx)
	if state != nil {
		// 已有 SessionState，直接设置 sessionID
		state.SetSessionID(m.sessionID)
		return ctx
	}
	// 无 SessionState，创建新的并注入 context
	newState := sessionctx.InitSessionState()
	newState.SetSessionID(m.sessionID)
	return sessionctx.WithSessionState(ctx, newState)
}
