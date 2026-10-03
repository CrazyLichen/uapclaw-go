package team

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// teamMonitorHandlerImpl 封装 TeamMonitor 的事件收集、转换和广播。
// 对齐 Python: TeamMonitorHandler (jiwenswarm/agents/harness/team/monitor_handler.py)
//
// 提供前端事件流（channel）和团队快照查询。
// 实现 TeamMonitorHandler 接口。
type teamMonitorHandlerImpl struct {
	// monitor TeamMonitor 实例
	monitor *monitor.TeamMonitor
	// sessionID 会话 ID
	sessionID string
	// eventQueue 事件队列 channel（缓冲 256），对齐 Python _event_queue
	eventQueue chan map[string]any
	// cancel 取消事件收集 goroutine
	cancel context.CancelFunc
	// running 是否正在运行
	running bool
	// mu 保护 running 和 monitor
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// monitorHandlerEventQueueSize 事件队列缓冲大小
	monitorHandlerEventQueueSize = 256
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// newTeamMonitorHandlerImpl 创建 TeamMonitorHandler 实现。
// 对齐 Python: TeamMonitorHandler(monitor, session_id)
func newTeamMonitorHandlerImpl(monitor *monitor.TeamMonitor, sessionID string) *teamMonitorHandlerImpl {
	return &teamMonitorHandlerImpl{
		monitor:    monitor,
		sessionID:  sessionID,
		eventQueue: make(chan map[string]any, monitorHandlerEventQueueSize),
	}
}

// Start 启动监控 handler。
// 对齐 Python: TeamMonitorHandler.start()
//
// Python 步骤：
//  1. if self._running: return
//  2. if self._monitor is None: raise ValueError
//  3. await self._monitor.start()
//  4. self._running = True
//  5. self._event_task = asyncio.create_task(self._collect_events())
//  6. logger.info("Monitor 启动成功")
func (h *teamMonitorHandlerImpl) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return nil
	}
	if h.monitor == nil {
		return fmt.Errorf("TeamMonitorHandler 需要 TeamMonitor 实例")
	}
	if err := h.monitor.Start(ctx); err != nil {
		logger.Error(logComponent).Err(err).Str("session_id", h.sessionID).
			Msg("TeamMonitorHandler 启动失败：monitor.Start 失败")
		return err
	}
	h.running = true

	// 启动事件收集 goroutine
	collectCtx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go h.collectEvents(collectCtx)

	logger.Info(logComponent).Str("session_id", h.sessionID).Msg("TeamMonitorHandler 启动成功")
	return nil
}

// Stop 停止监控 handler。
// 对齐 Python: TeamMonitorHandler.stop()
//
// Python 步骤：
//  1. self._running = False
//  2. if self._event_task: cancel + await + set None
//  3. if self._monitor: try monitor.stop(); self._monitor = None
//  4. logger.info("Monitor 已停止")
func (h *teamMonitorHandlerImpl) Stop(ctx context.Context) error {
	h.mu.Lock()
	h.running = false

	// 取消事件收集 goroutine
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	h.mu.Unlock()

	// 停止 monitor（在锁外，避免死锁）
	h.mu.Lock()
	if h.monitor != nil {
		if err := h.monitor.Stop(ctx); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", h.sessionID).
				Msg("TeamMonitorHandler 停止 monitor 失败")
		}
		h.monitor = nil
	}
	h.mu.Unlock()

	logger.Info(logComponent).Str("session_id", h.sessionID).Msg("TeamMonitorHandler 已停止")
	return nil
}

// IsRunning 返回 handler 是否正在运行。
// 对齐 Python: TeamMonitorHandler.is_running property
func (h *teamMonitorHandlerImpl) IsRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

// Events 返回前端事件流 channel。
// 对齐 Python: TeamMonitorHandler.events() -> AsyncIterator[dict[str, Any]]
//
// Go 差异：Python 用 asyncio.wait_for(queue.get(), timeout=0.1) 轮询，
// Go 直接返回 channel，消费者 range 遍历。
func (h *teamMonitorHandlerImpl) Events() <-chan map[string]any {
	return h.eventQueue
}

// GetTeamSnapshot 获取团队快照（成员+任务聚合视图）。
// 对齐 Python: TeamMonitorHandler.get_team_snapshot() -> dict[str, Any] | None
//
// Python 步骤：
//  1. if self._monitor is None: return None
//  2. members = await self._monitor.get_members()
//  3. team_info = await self._monitor.get_team_info(); leader_name = team_info.leader_member_name
//  4. 过滤掉 leader
//  5. tasks = await self._monitor.get_tasks() or []
//  6. return {"members": [...], "tasks": [...], "team_id": self._monitor.team_id}
func (h *teamMonitorHandlerImpl) GetTeamSnapshot(ctx context.Context) (map[string]any, error) {
	h.mu.Lock()
	mon := h.monitor
	h.mu.Unlock()
	if mon == nil {
		return nil, nil
	}

	// 获取成员列表
	members, err := mon.GetMembers(ctx, "")
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", h.sessionID).
			Msg("GetTeamSnapshot 获取成员失败")
		return nil, nil
	}

	// 过滤掉 leader
	teamInfo, _ := mon.GetTeamInfo(ctx)
	leaderName := ""
	if teamInfo != nil {
		leaderName = teamInfo.LeaderMemberName
	}
	filteredMembers := make([]*monitor.MemberInfo, 0, len(members))
	for _, m := range members {
		if m.MemberName != leaderName {
			filteredMembers = append(filteredMembers, m)
		}
	}

	// 获取任务列表
	tasks, err := mon.GetTasks(ctx, "")
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", h.sessionID).
			Msg("GetTeamSnapshot 获取任务失败")
		tasks = nil
	}
	if tasks == nil {
		tasks = []*monitor.TaskInfo{}
	}

	// 构建成员列表
	memberList := make([]map[string]any, 0, len(filteredMembers))
	for _, m := range filteredMembers {
		entry := map[string]any{
			"member_id": m.MemberName,
			"name":      m.DisplayName,
			"status":    m.Status,
			"mode":      m.Mode,
		}
		if m.ExecutionStatus != nil {
			entry["execution_status"] = *m.ExecutionStatus
		}
		memberList = append(memberList, entry)
	}

	// 构建任务列表
	taskList := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		entry := map[string]any{
			"task_id":   t.TaskID,
			"team_name": t.TeamName,
			"title":     t.Title,
			"content":   t.Content,
			"status":    t.Status,
		}
		if t.Assignee != nil {
			entry["assignee"] = *t.Assignee
		}
		if t.UpdatedAt != nil {
			entry["updated_at"] = *t.UpdatedAt
		}
		taskList = append(taskList, entry)
	}

	return map[string]any{
		"members": memberList,
		"tasks":   taskList,
		"team_id": mon.TeamName(),
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectEvents 后台 goroutine：收集 Monitor 事件。
// 对齐 Python: TeamMonitorHandler._collect_events()
//
// Python 步骤：
//  1. if self._monitor is None: return
//  2. async for event in self._monitor.events():
//  3. if not self._running: break
//  4. event_dict = await self._convert_event_to_dict(event)
//  5. if event_dict: await self._event_queue.put(event_dict)
func (h *teamMonitorHandlerImpl) collectEvents(ctx context.Context) {
	defer logger.Info(logComponent).Str("session_id", h.sessionID).Msg("事件收集 goroutine 退出")
	logger.Info(logComponent).Str("session_id", h.sessionID).Msg("事件收集 goroutine 启动")

	// Python 步骤 1: if self._monitor is None: return
	h.mu.Lock()
	mon := h.monitor
	h.mu.Unlock()
	if mon == nil {
		return
	}
	eventCh := mon.Events()

	for evt := range eventCh {
		select {
		case <-ctx.Done():
			return
		default:
		}

		h.mu.Lock()
		isRunning := h.running
		h.mu.Unlock()
		if !isRunning {
			return
		}

		if evt == nil {
			continue
		}

		eventDict := h.convertEventToDict(evt)
		if eventDict != nil {
			select {
			case h.eventQueue <- eventDict:
			default:
				logger.Warn(logComponent).Str("session_id", h.sessionID).
					Msg("事件队列已满，丢弃事件")
			}
		}
	}
}

// convertEventToDict 将 MonitorEvent 转换为前端字典格式。
// 对齐 Python: TeamMonitorHandler._convert_event_to_dict(event) -> dict[str, Any] | None
//
// Python 步骤：
//  1. team_event_type = get_team_event_type(event.event_type); if None: return None
//  2. event_category = get_event_category(team_event_type)
//  3. event_data = {"type": team_event_type.value, "team_id": event.team_name}
//  4. if event.member_name: event_data["member_id"] = event.member_name
//  5. 按 event.event_type 分派到各 handler
//  6. return {"event_type": event_category.value, "session_id": self._session_id, "event": event_data}
func (h *teamMonitorHandlerImpl) convertEventToDict(event *monitor.MonitorEvent) map[string]any {
	if event == nil {
		return nil
	}

	// Python 步骤 1: SDK → 前端映射
	teamEventType := GetTeamEventType(event.EventType)
	if teamEventType == "" {
		return nil
	}

	// Python 步骤 2: 获取类别
	eventCategory := GetEventCategory(teamEventType)

	// Python 步骤 3: 基础事件数据
	eventData := map[string]any{
		"type":    string(teamEventType),
		"team_id": event.TeamName,
	}

	// Python 步骤 4: 成员名
	if event.MemberName != nil {
		eventData["member_id"] = *event.MemberName
	}

	// Python 步骤 5: 按 event_type 分派
	switch event.EventType {
	case monitor.MonitorEventTypeMemberSpawned:
		h.handleMemberSpawned(eventData, event)
	case monitor.MonitorEventTypeMemberStatusChanged:
		h.handleMemberStatusChanged(eventData, event)
	case monitor.MonitorEventTypeMemberExecutionChanged:
		h.handleMemberExecutionChanged(eventData, event)
	case monitor.MonitorEventTypeMemberRestarted:
		h.handleMemberRestarted(eventData, event)
	case monitor.MonitorEventTypeMemberShutdown:
		h.handleMemberShutdown(eventData, event)
	case monitor.MonitorEventTypeTaskCreated:
		h.handleTaskCreated(eventData, event)
	case monitor.MonitorEventTypeTaskClaimed:
		h.handleTaskClaimed(eventData, event)
	case monitor.MonitorEventTypeTaskCompleted:
		h.handleTaskCompleted(eventData, event)
	case monitor.MonitorEventTypeTaskCancelled:
		h.handleTaskCancelled(eventData, event)
	case monitor.MonitorEventTypeTaskUnblocked:
		h.handleTaskUnblocked(eventData, event)
	case monitor.MonitorEventTypeMessage:
		h.handleMessage(eventData, event)
	case monitor.MonitorEventTypeBroadcast:
		h.handleBroadcast(eventData, event)
	default:
		return nil
	}

	// Python 步骤 6: 包装输出
	return map[string]any{
		"event_type": string(eventCategory),
		"session_id": h.sessionID,
		"event":      eventData,
	}
}

// handleMemberSpawned 处理成员创建事件。
// 对齐 Python: TeamMonitorHandler._handle_member_spawned(base, event)
func (h *teamMonitorHandlerImpl) handleMemberSpawned(base map[string]any, event *monitor.MonitorEvent) {
	if event.MemberName != nil {
		base["member_id"] = *event.MemberName
	}
}

// handleMemberStatusChanged 处理成员状态变更事件。
// 对齐 Python: TeamMonitorHandler._handle_member_status_changed(base, event)
func (h *teamMonitorHandlerImpl) handleMemberStatusChanged(base map[string]any, event *monitor.MonitorEvent) {
	if event.MemberName != nil {
		base["member_id"] = *event.MemberName
	}
	base["old_status"] = ptrStrVal(event.OldStatus)
	base["new_status"] = ptrStrVal(event.NewStatus)
}

// handleMemberExecutionChanged 处理成员执行状态变更事件。
// 对齐 Python: TeamMonitorHandler._handle_member_execution_changed(base, event)
func (h *teamMonitorHandlerImpl) handleMemberExecutionChanged(base map[string]any, event *monitor.MonitorEvent) {
	if event.MemberName != nil {
		base["member_id"] = *event.MemberName
	}
	base["old_status"] = ptrStrVal(event.OldStatus)
	base["new_status"] = ptrStrVal(event.NewStatus)
}

// handleMemberRestarted 处理成员重启事件。
// 对齐 Python: TeamMonitorHandler._handle_member_restarted(base, event)
func (h *teamMonitorHandlerImpl) handleMemberRestarted(base map[string]any, event *monitor.MonitorEvent) {
	if event.MemberName != nil {
		base["member_id"] = *event.MemberName
	}
	base["reason"] = ptrStrVal(event.Reason)
	if event.RestartCount != nil {
		base["restart_count"] = *event.RestartCount
	}
}

// handleMemberShutdown 处理成员关闭事件。
// 对齐 Python: TeamMonitorHandler._handle_member_shutdown(base, event)
func (h *teamMonitorHandlerImpl) handleMemberShutdown(base map[string]any, event *monitor.MonitorEvent) {
	if event.MemberName != nil {
		base["member_id"] = *event.MemberName
	}
	if event.Force != nil {
		base["force"] = *event.Force
	}
}

// handleTaskCreated 处理任务创建事件。
// 对齐 Python: TeamMonitorHandler._handle_task_created(base, event)
func (h *teamMonitorHandlerImpl) handleTaskCreated(base map[string]any, event *monitor.MonitorEvent) {
	base["task_id"] = ptrStrVal(event.TaskID)
	base["status"] = ptrStrVal(event.Status)
}

// handleTaskClaimed 处理任务认领事件。
// 对齐 Python: TeamMonitorHandler._handle_task_claimed(base, event)
func (h *teamMonitorHandlerImpl) handleTaskClaimed(base map[string]any, event *monitor.MonitorEvent) {
	base["task_id"] = ptrStrVal(event.TaskID)
}

// handleTaskCompleted 处理任务完成事件。
// 对齐 Python: TeamMonitorHandler._handle_task_completed(base, event)
func (h *teamMonitorHandlerImpl) handleTaskCompleted(base map[string]any, event *monitor.MonitorEvent) {
	base["task_id"] = ptrStrVal(event.TaskID)
}

// handleTaskCancelled 处理任务取消事件。
// 对齐 Python: TeamMonitorHandler._handle_task_cancelled(base, event)
func (h *teamMonitorHandlerImpl) handleTaskCancelled(base map[string]any, event *monitor.MonitorEvent) {
	base["task_id"] = ptrStrVal(event.TaskID)
}

// handleTaskUnblocked 处理任务解除阻塞事件。
// 对齐 Python: TeamMonitorHandler._handle_task_unblocked(base, event)
func (h *teamMonitorHandlerImpl) handleTaskUnblocked(base map[string]any, event *monitor.MonitorEvent) {
	base["task_id"] = ptrStrVal(event.TaskID)
}

// handleMessage 处理点对点消息事件。
// 对齐 Python: TeamMonitorHandler._handle_message(base, event)
//
// Python 步骤：
//  1. message_content = await self._get_message_content(event.message_id)
//  2. base.update({"message_id": ..., "from_member": ..., "to_member": ..., "content": ...})
//
// Go 差异：Python 异步查询消息内容，Go 直接调用 monitor 查询。
func (h *teamMonitorHandlerImpl) handleMessage(base map[string]any, event *monitor.MonitorEvent) {
	// 获取消息内容
	messageContent := h.getMessageContent(event.MessageID)
	base["message_id"] = ptrStrVal(event.MessageID)
	base["from_member"] = ptrStrVal(event.FromMemberName)
	base["to_member"] = ptrStrVal(event.ToMemberName)
	base["content"] = messageContent
}

// handleBroadcast 处理广播消息事件。
// 对齐 Python: TeamMonitorHandler._handle_broadcast(base, event)
//
// Python 步骤：
//  1. message_content = await self._get_message_content(event.message_id)
//  2. base.update({"message_id": ..., "from_member": ..., "content": ...})
func (h *teamMonitorHandlerImpl) handleBroadcast(base map[string]any, event *monitor.MonitorEvent) {
	messageContent := h.getMessageContent(event.MessageID)
	base["message_id"] = ptrStrVal(event.MessageID)
	base["from_member"] = ptrStrVal(event.FromMemberName)
	base["content"] = messageContent
}

// getMessageContent 获取消息内容。
// 对齐 Python: TeamMonitorHandler._get_message_content(message_id) -> str
//
// Python 步骤：
//  1. if not message_id or not self._monitor: return ""
//  2. token = set_session_id(self._session_id)
//  3. try: messages = await self._monitor.get_messages(); find by message_id
//  4. finally: reset_session_id(token)
//  5. except: return ""
//
// Go 差异：Go 端 getMessages 已内置 boundSession，不需要手动设置 contextvar。
// 在事件收集 goroutine 中直接调用，无法返回 error，故使用 context.Background()。
func (h *teamMonitorHandlerImpl) getMessageContent(messageID *string) string {
	if messageID == nil || *messageID == "" {
		return ""
	}
	h.mu.Lock()
	mon := h.monitor
	h.mu.Unlock()
	if mon == nil {
		return ""
	}

	// 对齐 Python: messages = await self._monitor.get_messages()
	// 遍历所有消息按 messageID 查找
	msgs, err := mon.GetMessages(context.Background(), "", "")
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("message_id", *messageID).
			Msg("查询消息内容失败")
		return ""
	}
	for _, msg := range msgs {
		if msg.MessageID == *messageID {
			return msg.Content
		}
	}
	return ""
}

// ptrStrVal 安全提取 *string 的值，nil 时返回空字符串
func ptrStrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
