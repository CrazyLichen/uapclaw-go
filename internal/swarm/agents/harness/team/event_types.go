package team

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// TeamEventCategory 前端事件类别。
// 对齐 Python: TeamEventCategory(str, Enum) (jiwenswarm/agents/harness/team/event_types.py)
type TeamEventCategory string

const (
	// EventCategoryMember 成员事件区域
	EventCategoryMember TeamEventCategory = "team.member"
	// EventCategoryTask 任务事件区域
	EventCategoryTask TeamEventCategory = "team.task"
	// EventCategoryMessage 消息事件区域
	EventCategoryMessage TeamEventCategory = "team.message"
)

// TeamEventType 前端事件类型，命名约定 team.{category}.{action}。
// 对齐 Python: TeamEventType(str, Enum) (jiwenswarm/agents/harness/team/event_types.py)
type TeamEventType string

const (
	// ── 成员事件 ──
	// TeamEventMemberSpawned 成员已启动
	TeamEventMemberSpawned TeamEventType = "team.member.spawned"
	// TeamEventMemberStatusChanged 成员状态变更
	TeamEventMemberStatusChanged TeamEventType = "team.member.status_changed"
	// TeamEventMemberExecutionChanged 成员执行状态变更
	TeamEventMemberExecutionChanged TeamEventType = "team.member.execution_changed"
	// TeamEventMemberRestarted 成员已重启
	TeamEventMemberRestarted TeamEventType = "team.member.restarted"
	// TeamEventMemberShutdown 成员已关闭
	TeamEventMemberShutdown TeamEventType = "team.member.shutdown"

	// ── 任务事件 ──
	// TeamEventTaskCreated 任务已创建
	TeamEventTaskCreated TeamEventType = "team.task.created"
	// TeamEventTaskClaimed 任务已认领
	TeamEventTaskClaimed TeamEventType = "team.task.claimed"
	// TeamEventTaskCompleted 任务已完成
	TeamEventTaskCompleted TeamEventType = "team.task.completed"
	// TeamEventTaskCancelled 任务已取消
	TeamEventTaskCancelled TeamEventType = "team.task.cancelled"
	// TeamEventTaskUnblocked 任务已解除阻塞
	TeamEventTaskUnblocked TeamEventType = "team.task.unblocked"

	// ── 消息事件 ──
	// TeamEventMessageP2P 点对点消息
	TeamEventMessageP2P TeamEventType = "team.message.p2p"
	// TeamEventMessageBroadcast 广播消息
	TeamEventMessageBroadcast TeamEventType = "team.message.broadcast"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// eventTypeToCategory TeamEventType → TeamEventCategory 映射。
	// 对齐 Python: EVENT_TYPE_TO_CATEGORY
	eventTypeToCategory = map[TeamEventType]TeamEventCategory{
		TeamEventMemberSpawned:          EventCategoryMember,
		TeamEventMemberStatusChanged:    EventCategoryMember,
		TeamEventMemberExecutionChanged: EventCategoryMember,
		TeamEventMemberRestarted:        EventCategoryMember,
		TeamEventMemberShutdown:         EventCategoryMember,
		TeamEventTaskCreated:            EventCategoryTask,
		TeamEventTaskClaimed:            EventCategoryTask,
		TeamEventTaskCompleted:          EventCategoryTask,
		TeamEventTaskCancelled:          EventCategoryTask,
		TeamEventTaskUnblocked:          EventCategoryTask,
		TeamEventMessageP2P:             EventCategoryMessage,
		TeamEventMessageBroadcast:       EventCategoryMessage,
	}

	// sdkToTeamEventMap SDK MonitorEventType → 前端 TeamEventType 映射。
	// 对齐 Python: SDK_TO_TEAM_EVENT_MAP
	// 未包含的 SDK 类型（TEAM_CREATED/MEMBER_CANCELED/TASK_PLAN_*/TASK_UPDATED 等）
	// 在转换时返回零值，静默丢弃。
	sdkToTeamEventMap = map[monitor.MonitorEventType]TeamEventType{
		monitor.MonitorEventTypeMemberSpawned:          TeamEventMemberSpawned,
		monitor.MonitorEventTypeMemberStatusChanged:    TeamEventMemberStatusChanged,
		monitor.MonitorEventTypeMemberExecutionChanged: TeamEventMemberExecutionChanged,
		monitor.MonitorEventTypeMemberRestarted:        TeamEventMemberRestarted,
		monitor.MonitorEventTypeMemberShutdown:         TeamEventMemberShutdown,
		monitor.MonitorEventTypeTaskCreated:            TeamEventTaskCreated,
		monitor.MonitorEventTypeTaskClaimed:            TeamEventTaskClaimed,
		monitor.MonitorEventTypeTaskCompleted:          TeamEventTaskCompleted,
		monitor.MonitorEventTypeTaskCancelled:          TeamEventTaskCancelled,
		monitor.MonitorEventTypeTaskUnblocked:          TeamEventTaskUnblocked,
		monitor.MonitorEventTypeMessage:                TeamEventMessageP2P,
		monitor.MonitorEventTypeBroadcast:              TeamEventMessageBroadcast,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetTeamEventType 将 SDK MonitorEventType 映射为前端 TeamEventType。
// 对齐 Python: get_team_event_type(sdk_event_type: MonitorEventType) -> TeamEventType | None
// 返回零值（空字符串）表示未映射，调用者应跳过该事件。
func GetTeamEventType(sdk monitor.MonitorEventType) TeamEventType {
	return sdkToTeamEventMap[sdk]
}

// GetEventCategory 返回前端事件类别。
// 对齐 Python: get_event_category(event_type: TeamEventType) -> TeamEventCategory
// 默认返回 EventCategoryMember。
func GetEventCategory(et TeamEventType) TeamEventCategory {
	if cat, ok := eventTypeToCategory[et]; ok {
		return cat
	}
	return EventCategoryMember
}

// IsMessageEvent 判断事件类型是否为消息类别。
// 对齐 Python: is_message_event(event_type: TeamEventType) -> bool
func IsMessageEvent(et TeamEventType) bool {
	return GetEventCategory(et) == EventCategoryMessage
}

// ──────────────────────────── 非导出函数 ────────────────────────────
