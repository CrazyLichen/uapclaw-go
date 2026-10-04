package monitor

import (
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamInfo 团队基本信息。
// 对齐 Python: TeamInfo(BaseModel) (openjiuwen/agent_teams/monitor/models.py)
type TeamInfo struct {
	// TeamName 团队名称
	TeamName string `json:"team_name"`
	// DisplayName 显示名称
	DisplayName string `json:"display_name"`
	// LeaderMemberName Leader 成员名
	LeaderMemberName string `json:"leader_member_name"`
	// Desc 团队描述
	Desc *string `json:"desc,omitempty"`
	// Created 创建时间戳（毫秒）
	Created int64 `json:"created"`
}

// MemberInfo 团队成员信息。
// 对齐 Python: MemberInfo(BaseModel) (openjiuwen/agent_teams/monitor/models.py)
type MemberInfo struct {
	// MemberName 成员名称
	MemberName string `json:"member_name"`
	// TeamName 团队名称
	TeamName string `json:"team_name"`
	// DisplayName 显示名称
	DisplayName string `json:"display_name"`
	// Desc 成员描述
	Desc *string `json:"desc,omitempty"`
	// Status 成员状态（MemberStatus 值）
	Status string `json:"status"`
	// ExecutionStatus 执行状态（ExecutionStatus 值）
	ExecutionStatus *string `json:"execution_status,omitempty"`
	// Mode 成员模式（MemberMode 值）
	Mode string `json:"mode"`
}

// TaskInfo 任务信息。
// 对齐 Python: TaskInfo(BaseModel) (openjiuwen/agent_teams/monitor/models.py)
type TaskInfo struct {
	// TaskID 任务唯一标识
	TaskID string `json:"task_id"`
	// TeamName 团队名称
	TeamName string `json:"team_name"`
	// Title 任务标题
	Title string `json:"title"`
	// Content 任务内容
	Content string `json:"content"`
	// Status 任务状态（TaskStatus 值）
	Status string `json:"status"`
	// Assignee 认领人/分配人
	Assignee *string `json:"assignee,omitempty"`
	// UpdatedAt 最新状态转换的毫秒时间戳
	UpdatedAt *int64 `json:"updated_at,omitempty"`
}

// MessageInfo 邮箱消息信息。
// 对齐 Python: MessageInfo(BaseModel) (openjiuwen/agent_teams/monitor/models.py)
type MessageInfo struct {
	// MessageID 消息唯一标识
	MessageID string `json:"message_id"`
	// TeamName 团队名称
	TeamName string `json:"team_name"`
	// FromMemberName 发送者
	FromMemberName string `json:"from_member_name"`
	// ToMemberName 接收者（nil 表示广播）
	ToMemberName *string `json:"to_member_name,omitempty"`
	// Content 消息内容
	Content string `json:"content"`
	// Timestamp 毫秒时间戳
	Timestamp int64 `json:"timestamp"`
	// Broadcast 是否广播消息
	Broadcast bool `json:"broadcast"`
	// IsRead 是否已读
	IsRead bool `json:"is_read"`
}

// MonitorEvent 监控发出的实时事件。所有载荷字段扁平化。
// 对齐 Python: MonitorEvent(BaseModel) (openjiuwen/agent_teams/monitor/models.py)
type MonitorEvent struct {
	// EventType 事件类型
	EventType MonitorEventType `json:"event_type"`
	// TeamName 团队名
	TeamName string `json:"team_name"`
	// MemberName 成员名
	MemberName *string `json:"member_name,omitempty"`
	// Timestamp 监控接收时间（毫秒）
	Timestamp int64 `json:"timestamp"`

	// ── 团队字段 ──
	// DisplayName 显示名称
	DisplayName *string `json:"display_name,omitempty"`
	// LeaderMemberName Leader 成员名
	LeaderMemberName *string `json:"leader_member_name,omitempty"`
	// Created 创建时间戳（毫秒）
	Created *int64 `json:"created,omitempty"`

	// ── 成员字段 ──
	// OldStatus 旧状态
	OldStatus *string `json:"old_status,omitempty"`
	// NewStatus 新状态
	NewStatus *string `json:"new_status,omitempty"`
	// Reason 原因
	Reason *string `json:"reason,omitempty"`
	// RestartCount 重启次数
	RestartCount *int `json:"restart_count,omitempty"`
	// Force 是否强制
	Force *bool `json:"force,omitempty"`

	// ── 任务字段 ──
	// TaskID 任务 ID
	TaskID *string `json:"task_id,omitempty"`
	// Status 任务状态
	Status *string `json:"status,omitempty"`
	// PlanID 计划 ID
	PlanID *string `json:"plan_id,omitempty"`
	// MemberPlanMD 成员计划 Markdown
	MemberPlanMD *string `json:"member_plan_md,omitempty"`
	// Approved 是否已审批
	Approved *bool `json:"approved,omitempty"`

	// ── 消息字段 ──
	// MessageID 消息 ID
	MessageID *string `json:"message_id,omitempty"`
	// FromMemberName 发送者
	FromMemberName *string `json:"from_member_name,omitempty"`
	// ToMemberName 接收者
	ToMemberName *string `json:"to_member_name,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// MonitorEventType 可观察的事件类型枚举。
// 对齐 Python: MonitorEventType(str, Enum) (openjiuwen/agent_teams/monitor/models.py)
//
// 仅包括团队、成员、任务和消息事件。
// 内部事件（计划审批、工具审批、工作树、workspace 锁等）被排除。
type MonitorEventType string

const (
	// ── 团队生命周期 ──
	// MonitorEventTypeTeamCreated 团队已创建
	MonitorEventTypeTeamCreated MonitorEventType = "team_created"
	// MonitorEventTypeTeamCleaned 团队已清理
	MonitorEventTypeTeamCleaned MonitorEventType = "team_cleaned"
	// MonitorEventTypeTeamStandby 团队待机
	MonitorEventTypeTeamStandby MonitorEventType = "team_standby"

	// ── 成员生命周期 ──
	// MonitorEventTypeMemberSpawned 成员已启动
	MonitorEventTypeMemberSpawned MonitorEventType = "member_spawned"
	// MonitorEventTypeMemberRestarted 成员已重启
	MonitorEventTypeMemberRestarted MonitorEventType = "member_restarted"
	// MonitorEventTypeMemberStatusChanged 成员状态变更
	MonitorEventTypeMemberStatusChanged MonitorEventType = "member_status_changed"
	// MonitorEventTypeMemberExecutionChanged 成员执行状态变更
	MonitorEventTypeMemberExecutionChanged MonitorEventType = "member_execution_changed"
	// MonitorEventTypeMemberShutdown 成员已关闭
	MonitorEventTypeMemberShutdown MonitorEventType = "member_shutdown"
	// MonitorEventTypeMemberCanceled 成员已取消
	MonitorEventTypeMemberCanceled MonitorEventType = "member_canceled"

	// ── 任务 ──
	// MonitorEventTypeTaskCreated 任务已创建
	MonitorEventTypeTaskCreated MonitorEventType = "task_created"
	// MonitorEventTypeTaskPlanRequest 任务计划请求
	MonitorEventTypeTaskPlanRequest MonitorEventType = "task_plan_request"
	// MonitorEventTypeTaskPlanResponse 任务计划响应
	MonitorEventTypeTaskPlanResponse MonitorEventType = "task_plan_response"
	// MonitorEventTypeTaskUpdated 任务已更新
	MonitorEventTypeTaskUpdated MonitorEventType = "task_updated"
	// MonitorEventTypeTaskClaimed 任务已认领
	MonitorEventTypeTaskClaimed MonitorEventType = "task_claimed"
	// MonitorEventTypeTaskCompleted 任务已完成
	MonitorEventTypeTaskCompleted MonitorEventType = "task_completed"
	// MonitorEventTypeTaskCancelled 任务已取消
	MonitorEventTypeTaskCancelled MonitorEventType = "task_cancelled"
	// MonitorEventTypeTaskUnblocked 任务已解除阻塞
	MonitorEventTypeTaskUnblocked MonitorEventType = "task_unblocked"

	// ── 消息 ──
	// MonitorEventTypeMessage 点对点消息
	MonitorEventTypeMessage MonitorEventType = "message"
	// MonitorEventTypeBroadcast 广播消息
	MonitorEventTypeBroadcast MonitorEventType = "broadcast"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// monitorEventValues MonitorEventType 所有值的集合。
	// 对齐 Python: _MONITOR_EVENT_VALUES = frozenset(e.value for e in MonitorEventType)
	// 用于 FromEventMessage 中过滤非监控事件类型。
	monitorEventValues map[string]bool
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FromInternal 从 database.Team 转换为 TeamInfo。
// 对齐 Python: TeamInfo.from_internal(team)
func (TeamInfo) FromInternal(team *database.Team) *TeamInfo {
	var desc *string
	if team.Desc != "" {
		desc = &team.Desc
	}
	return &TeamInfo{
		TeamName:         team.TeamName,
		DisplayName:      team.DisplayName,
		LeaderMemberName: team.LeaderMemberName,
		Desc:             desc,
		Created:          team.Created,
	}
}

// FromInternal 从 database.TeamMember 转换为 MemberInfo。
// 对齐 Python: MemberInfo.from_internal(member)
func (MemberInfo) FromInternal(member *database.TeamMember) *MemberInfo {
	var desc *string
	if member.Desc != "" {
		desc = &member.Desc
	}
	var executionStatus *string
	if member.ExecutionStatus != "" {
		executionStatus = &member.ExecutionStatus
	}
	return &MemberInfo{
		MemberName:      member.MemberName,
		TeamName:        member.TeamName,
		DisplayName:     member.DisplayName,
		Desc:            desc,
		Status:          member.Status,
		ExecutionStatus: executionStatus,
		Mode:            member.Mode,
	}
}

// FromInternal 从 database.TeamTaskBase 转换为 TaskInfo。
// 对齐 Python: TaskInfo.from_internal(task)
func (TaskInfo) FromInternal(task *database.TeamTaskBase) *TaskInfo {
	var updatedAt *int64
	if task.UpdatedAt != 0 {
		updatedAt = &task.UpdatedAt
	}
	return &TaskInfo{
		TaskID:    task.TaskID,
		TeamName:  task.TeamName,
		Title:     task.Title,
		Content:   task.Content,
		Status:    task.Status,
		Assignee:  task.Assignee,
		UpdatedAt: updatedAt,
	}
}

// FromInternal 从 database.TeamMessageBase 转换为 MessageInfo。
// 对齐 Python: MessageInfo.from_internal(msg)
func (MessageInfo) FromInternal(msg *database.TeamMessageBase) *MessageInfo {
	isRead := false
	if msg.IsRead != nil {
		isRead = *msg.IsRead
	}
	return &MessageInfo{
		MessageID:      msg.MessageID,
		TeamName:       msg.TeamName,
		FromMemberName: msg.FromMemberName,
		ToMemberName:   msg.ToMemberName,
		Content:        msg.Content,
		Timestamp:      msg.Timestamp,
		Broadcast:      msg.Broadcast,
		IsRead:         isRead,
	}
}

// FromEventMessage 从内部 EventMessage 构建 MonitorEvent。
// 对齐 Python: MonitorEvent.from_event_message(event_message) -> MonitorEvent | None
//
// Python 步骤：
//  1. raw_type = event_message.event_type
//  2. if raw_type not in _MONITOR_EVENT_VALUES: return None
//  3. return cls.model_validate({**event_message.payload, "event_type": raw_type, "timestamp": int(time*1000)})
//
// Go 差异：合并 payload 字段到 MonitorEvent 扁平字段，用辅助函数提取类型化指针。
func FromEventMessage(msg *events.EventMessage) *MonitorEvent {
	if msg == nil {
		return nil
	}
	rawType := msg.EventType
	if !monitorEventValues[rawType] {
		return nil
	}
	p := msg.Payload
	if p == nil {
		p = make(map[string]any)
	}

	evt := &MonitorEvent{
		EventType: MonitorEventType(rawType),
		TeamName:  utils.StrValFromMap(p, "team_name"),
		Timestamp: time.Now().UnixMilli(),
	}

	// 成员名
	if v := strPtrFromMap(p, "member_name"); v != nil {
		evt.MemberName = v
	}

	// 团队字段
	evt.DisplayName = strPtrFromMap(p, "display_name")
	evt.LeaderMemberName = strPtrFromMap(p, "leader_member_name")
	evt.Created = int64PtrFromMap(p, "created")

	// 成员字段
	evt.OldStatus = strPtrFromMap(p, "old_status")
	evt.NewStatus = strPtrFromMap(p, "new_status")
	evt.Reason = strPtrFromMap(p, "reason")
	evt.RestartCount = intPtrFromMap(p, "restart_count")
	evt.Force = boolPtrFromMap(p, "force")

	// 任务字段
	evt.TaskID = strPtrFromMap(p, "task_id")
	evt.Status = strPtrFromMap(p, "status")
	evt.PlanID = strPtrFromMap(p, "plan_id")
	evt.MemberPlanMD = strPtrFromMap(p, "member_plan_md")
	evt.Approved = boolPtrFromMap(p, "approved")

	// 消息字段
	evt.MessageID = strPtrFromMap(p, "message_id")
	evt.FromMemberName = strPtrFromMap(p, "from_member_name")
	evt.ToMemberName = strPtrFromMap(p, "to_member_name")

	return evt
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 初始化 monitorEventValues，对齐 Python _MONITOR_EVENT_VALUES
	monitorEventValues = make(map[string]bool, 19)
	for _, v := range []MonitorEventType{
		MonitorEventTypeTeamCreated,
		MonitorEventTypeTeamCleaned,
		MonitorEventTypeTeamStandby,
		MonitorEventTypeMemberSpawned,
		MonitorEventTypeMemberRestarted,
		MonitorEventTypeMemberStatusChanged,
		MonitorEventTypeMemberExecutionChanged,
		MonitorEventTypeMemberShutdown,
		MonitorEventTypeMemberCanceled,
		MonitorEventTypeTaskCreated,
		MonitorEventTypeTaskPlanRequest,
		MonitorEventTypeTaskPlanResponse,
		MonitorEventTypeTaskUpdated,
		MonitorEventTypeTaskClaimed,
		MonitorEventTypeTaskCompleted,
		MonitorEventTypeTaskCancelled,
		MonitorEventTypeTaskUnblocked,
		MonitorEventTypeMessage,
		MonitorEventTypeBroadcast,
	} {
		monitorEventValues[string(v)] = true
	}
}

// strPtrFromMap 从 map 中提取 *string，不存在或为空时返回 nil。
func strPtrFromMap(m map[string]any, key string) *string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
}

// intPtrFromMap 从 map 中提取 *int。
func intPtrFromMap(m map[string]any, key string) *int {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch n := v.(type) {
	case int:
		return &n
	case int64:
		i := int(n)
		return &i
	case float64:
		i := int(n)
		return &i
	}
	return nil
}

// int64PtrFromMap 从 map 中提取 *int64。
func int64PtrFromMap(m map[string]any, key string) *int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch n := v.(type) {
	case int:
		i := int64(n)
		return &i
	case int64:
		return &n
	case float64:
		i := int64(n)
		return &i
	}
	return nil
}

// boolPtrFromMap 从 map 中提取 *bool。
func boolPtrFromMap(m map[string]any, key string) *bool {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	b, ok := v.(bool)
	if !ok {
		return nil
	}
	return &b
}
