package team

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
)

// TestGetTeamEventType_已映射 验证已映射的 SDK 事件类型
func TestGetTeamEventType_已映射(t *testing.T) {
	tests := []struct {
		sdk  monitor.MonitorEventType
		want TeamEventType
	}{
		{monitor.MonitorEventTypeMemberSpawned, TeamEventMemberSpawned},
		{monitor.MonitorEventTypeMemberStatusChanged, TeamEventMemberStatusChanged},
		{monitor.MonitorEventTypeMemberExecutionChanged, TeamEventMemberExecutionChanged},
		{monitor.MonitorEventTypeMemberRestarted, TeamEventMemberRestarted},
		{monitor.MonitorEventTypeMemberShutdown, TeamEventMemberShutdown},
		{monitor.MonitorEventTypeTaskCreated, TeamEventTaskCreated},
		{monitor.MonitorEventTypeTaskClaimed, TeamEventTaskClaimed},
		{monitor.MonitorEventTypeTaskCompleted, TeamEventTaskCompleted},
		{monitor.MonitorEventTypeTaskCancelled, TeamEventTaskCancelled},
		{monitor.MonitorEventTypeTaskUnblocked, TeamEventTaskUnblocked},
		{monitor.MonitorEventTypeMessage, TeamEventMessageP2P},
		{monitor.MonitorEventTypeBroadcast, TeamEventMessageBroadcast},
	}
	for _, tt := range tests {
		got := GetTeamEventType(tt.sdk)
		if got != tt.want {
			t.Errorf("GetTeamEventType(%q) = %q, want %q", tt.sdk, got, tt.want)
		}
	}
}

// TestGetTeamEventType_未映射 验证未映射的 SDK 类型返回零值
func TestGetTeamEventType_未映射(t *testing.T) {
	unmapped := []monitor.MonitorEventType{
		monitor.MonitorEventTypeTeamCreated,
		monitor.MonitorEventTypeTeamCleaned,
		monitor.MonitorEventTypeTeamStandby,
		monitor.MonitorEventTypeMemberCanceled,
		monitor.MonitorEventTypeTaskPlanRequest,
		monitor.MonitorEventTypeTaskPlanResponse,
		monitor.MonitorEventTypeTaskUpdated,
	}
	for _, sdk := range unmapped {
		got := GetTeamEventType(sdk)
		if got != "" {
			t.Errorf("GetTeamEventType(%q) = %q, want 空字符串", sdk, got)
		}
	}
}

// TestGetEventCategory 校验事件类别映射
func TestGetEventCategory(t *testing.T) {
	tests := []struct {
		et   TeamEventType
		want TeamEventCategory
	}{
		{TeamEventMemberSpawned, EventCategoryMember},
		{TeamEventMemberStatusChanged, EventCategoryMember},
		{TeamEventMemberShutdown, EventCategoryMember},
		{TeamEventTaskCreated, EventCategoryTask},
		{TeamEventTaskCompleted, EventCategoryTask},
		{TeamEventTaskUnblocked, EventCategoryTask},
		{TeamEventMessageP2P, EventCategoryMessage},
		{TeamEventMessageBroadcast, EventCategoryMessage},
	}
	for _, tt := range tests {
		got := GetEventCategory(tt.et)
		if got != tt.want {
			t.Errorf("GetEventCategory(%q) = %q, want %q", tt.et, got, tt.want)
		}
	}
}

// TestGetEventCategory_默认 无效类型默认返回 EventCategoryMember
func TestGetEventCategory_默认(t *testing.T) {
	got := GetEventCategory("invalid_type")
	if got != EventCategoryMember {
		t.Errorf("GetEventCategory(无效) = %q, want %q", got, EventCategoryMember)
	}
}

// TestIsMessageEvent 校验消息事件判断
func TestIsMessageEvent(t *testing.T) {
	if !IsMessageEvent(TeamEventMessageP2P) {
		t.Error("IsMessageEvent(team.message.p2p) 应为 true")
	}
	if !IsMessageEvent(TeamEventMessageBroadcast) {
		t.Error("IsMessageEvent(team.message.broadcast) 应为 true")
	}
	if IsMessageEvent(TeamEventTaskCreated) {
		t.Error("IsMessageEvent(team.task.created) 应为 false")
	}
	if IsMessageEvent(TeamEventMemberSpawned) {
		t.Error("IsMessageEvent(team.member.spawned) 应为 false")
	}
}
