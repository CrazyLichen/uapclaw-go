package monitor

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// TestMonitorEventType_值对齐 验证枚举字符串值与 Python 一致
func TestMonitorEventType_值对齐(t *testing.T) {
	tests := []struct {
		got  MonitorEventType
		want string
	}{
		{MonitorEventTypeTeamCreated, "team_created"},
		{MonitorEventTypeTeamCleaned, "team_cleaned"},
		{MonitorEventTypeTeamStandby, "team_standby"},
		{MonitorEventTypeMemberSpawned, "member_spawned"},
		{MonitorEventTypeMemberRestarted, "member_restarted"},
		{MonitorEventTypeMemberStatusChanged, "member_status_changed"},
		{MonitorEventTypeMemberExecutionChanged, "member_execution_changed"},
		{MonitorEventTypeMemberShutdown, "member_shutdown"},
		{MonitorEventTypeMemberCanceled, "member_canceled"},
		{MonitorEventTypeTaskCreated, "task_created"},
		{MonitorEventTypeTaskPlanRequest, "task_plan_request"},
		{MonitorEventTypeTaskPlanResponse, "task_plan_response"},
		{MonitorEventTypeTaskUpdated, "task_updated"},
		{MonitorEventTypeTaskClaimed, "task_claimed"},
		{MonitorEventTypeTaskCompleted, "task_completed"},
		{MonitorEventTypeTaskCancelled, "task_cancelled"},
		{MonitorEventTypeTaskUnblocked, "task_unblocked"},
		{MonitorEventTypeMessage, "message"},
		{MonitorEventTypeBroadcast, "broadcast"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("MonitorEventType = %q, want %q", tt.got, tt.want)
		}
	}
}

// TestMonitorEventValues_完整性 验证 19 个枚举值都在集合中
func TestMonitorEventValues_完整性(t *testing.T) {
	if len(monitorEventValues) != 19 {
		t.Errorf("monitorEventValues 有 %d 项, want 19", len(monitorEventValues))
	}
	for _, v := range []MonitorEventType{
		MonitorEventTypeTeamCreated, MonitorEventTypeMemberSpawned, MonitorEventTypeTaskCreated,
		MonitorEventTypeMessage, MonitorEventTypeBroadcast,
	} {
		if !monitorEventValues[string(v)] {
			t.Errorf("monitorEventValues[%q] = false, want true", v)
		}
	}
}

// TestTeamInfo_FromInternal 验证 database.Team → TeamInfo 转换
func TestTeamInfo_FromInternal(t *testing.T) {
	team := &database.Team{
		TeamName:         "test-team",
		DisplayName:      "测试团队",
		LeaderMemberName: "leader",
		Desc:             "描述",
		Created:          1700000000000,
	}
	info := TeamInfo{}.FromInternal(team)
	if info.TeamName != "test-team" {
		t.Errorf("TeamName = %q, want %q", info.TeamName, "test-team")
	}
	if info.Desc == nil || *info.Desc != "描述" {
		t.Errorf("Desc = %v, want '描述'", info.Desc)
	}
	if info.Created != 1700000000000 {
		t.Errorf("Created = %d, want 1700000000000", info.Created)
	}

	// Desc 为空时为 nil
	team2 := &database.Team{TeamName: "t", DisplayName: "d", LeaderMemberName: "l"}
	info2 := TeamInfo{}.FromInternal(team2)
	if info2.Desc != nil {
		t.Errorf("Desc = %v, want nil", info2.Desc)
	}
}

// TestMemberInfo_FromInternal 验证 database.TeamMember → MemberInfo 转换
func TestMemberInfo_FromInternal(t *testing.T) {
	member := &database.TeamMember{
		MemberName:      "m1",
		TeamName:        "team",
		DisplayName:     "成员1",
		Status:          "active",
		ExecutionStatus: "idle",
		Mode:            "build_mode",
	}
	info := MemberInfo{}.FromInternal(member)
	if info.MemberName != "m1" {
		t.Errorf("MemberName = %q, want %q", info.MemberName, "m1")
	}
	if info.ExecutionStatus == nil || *info.ExecutionStatus != "idle" {
		t.Errorf("ExecutionStatus = %v, want 'idle'", info.ExecutionStatus)
	}

	// ExecutionStatus 为空时为 nil
	member2 := &database.TeamMember{MemberName: "m2", TeamName: "t", Status: "active", Mode: "build_mode"}
	info2 := MemberInfo{}.FromInternal(member2)
	if info2.ExecutionStatus != nil {
		t.Errorf("ExecutionStatus = %v, want nil", info2.ExecutionStatus)
	}
}

// TestTaskInfo_FromInternal 验证 database.TeamTaskBase → TaskInfo 转换
func TestTaskInfo_FromInternal(t *testing.T) {
	assignee := "m1"
	updatedAt := int64(1700000000000)
	task := &database.TeamTaskBase{
		TaskID:    "task-1",
		TeamName:  "team",
		Title:     "标题",
		Content:   "内容",
		Status:    "claimed",
		Assignee:  &assignee,
		UpdatedAt: updatedAt,
	}
	info := TaskInfo{}.FromInternal(task)
	if info.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", info.TaskID, "task-1")
	}
	if info.Assignee == nil || *info.Assignee != "m1" {
		t.Errorf("Assignee = %v, want 'm1'", info.Assignee)
	}
	if info.UpdatedAt == nil || *info.UpdatedAt != updatedAt {
		t.Errorf("UpdatedAt = %v, want %d", info.UpdatedAt, updatedAt)
	}

	// 零值UpdatedAt → nil
	task2 := &database.TeamTaskBase{TaskID: "t2", Status: "pending"}
	info2 := TaskInfo{}.FromInternal(task2)
	if info2.UpdatedAt != nil {
		t.Errorf("UpdatedAt = %v, want nil", info2.UpdatedAt)
	}
}

// TestMessageInfo_FromInternal 验证 database.TeamMessageBase → MessageInfo 转换
func TestMessageInfo_FromInternal(t *testing.T) {
	isRead := false
	msg := &database.TeamMessageBase{
		MessageID:      "msg-1",
		TeamName:       "team",
		FromMemberName: "m1",
		ToMemberName:   nil,
		Content:        "hello",
		Timestamp:      1700000000000,
		Broadcast:      true,
		IsRead:         nil, // 广播消息 IsRead 为 nil
	}
	info := MessageInfo{}.FromInternal(msg)
	if info.MessageID != "msg-1" {
		t.Errorf("MessageID = %q, want %q", info.MessageID, "msg-1")
	}
	if info.Broadcast != true {
		t.Errorf("Broadcast = %v, want true", info.Broadcast)
	}
	if info.IsRead != false {
		t.Errorf("IsRead = %v, want false (nil→false)", info.IsRead)
	}

	// 直发消息 IsRead 有值
	msg2 := &database.TeamMessageBase{
		MessageID: "msg-2", FromMemberName: "m1",
		IsRead: &isRead, Broadcast: false,
	}
	info2 := MessageInfo{}.FromInternal(msg2)
	if info2.IsRead != false {
		t.Errorf("IsRead = %v, want false", info2.IsRead)
	}
}

// TestFromEventMessage_有效事件 验证有效事件类型的转换
func TestFromEventMessage_有效事件(t *testing.T) {
	msg := &events.EventMessage{
		EventType: "member_spawned",
		Payload: map[string]any{
			"team_name":   "team1",
			"member_name": "m1",
		},
	}
	evt := FromEventMessage(msg)
	if evt == nil {
		t.Fatal("FromEventMessage 返回 nil, want non-nil")
	}
	if evt.EventType != MonitorEventTypeMemberSpawned {
		t.Errorf("EventType = %q, want %q", evt.EventType, MonitorEventTypeMemberSpawned)
	}
	if evt.TeamName != "team1" {
		t.Errorf("TeamName = %q, want %q", evt.TeamName, "team1")
	}
	if evt.MemberName == nil || *evt.MemberName != "m1" {
		t.Errorf("MemberName = %v, want 'm1'", evt.MemberName)
	}
	if evt.Timestamp == 0 {
		t.Error("Timestamp = 0, want non-zero")
	}
}

// TestFromEventMessage_无效事件 验证非监控事件类型返回 nil
func TestFromEventMessage_无效事件(t *testing.T) {
	msg := &events.EventMessage{
		EventType: "plan_approval", // 不在 monitorEventValues 中
		Payload:   map[string]any{},
	}
	evt := FromEventMessage(msg)
	if evt != nil {
		t.Errorf("FromEventMessage 返回 non-nil, want nil for non-monitor event")
	}
}

// TestFromEventMessage_Nil 验证 nil 输入返回 nil
func TestFromEventMessage_Nil(t *testing.T) {
	evt := FromEventMessage(nil)
	if evt != nil {
		t.Errorf("FromEventMessage(nil) = %v, want nil", evt)
	}
}

// TestFromEventMessage_成员事件 验证 member_status_changed 的 OldStatus/NewStatus 映射
func TestFromEventMessage_成员事件(t *testing.T) {
	msg := &events.EventMessage{
		EventType: "member_status_changed",
		Payload: map[string]any{
			"team_name":   "team1",
			"member_name": "m1",
			"old_status":  "idle",
			"new_status":  "active",
		},
	}
	evt := FromEventMessage(msg)
	if evt == nil {
		t.Fatal("FromEventMessage 返回 nil")
	}
	if evt.OldStatus == nil || *evt.OldStatus != "idle" {
		t.Errorf("OldStatus = %v, want 'idle'", evt.OldStatus)
	}
	if evt.NewStatus == nil || *evt.NewStatus != "active" {
		t.Errorf("NewStatus = %v, want 'active'", evt.NewStatus)
	}
}

// TestFromEventMessage_任务事件 验证 task_created 的 TaskID/Status 映射
func TestFromEventMessage_任务事件(t *testing.T) {
	msg := &events.EventMessage{
		EventType: "task_created",
		Payload: map[string]any{
			"team_name": "team1",
			"task_id":   "task-1",
			"status":    "pending",
		},
	}
	evt := FromEventMessage(msg)
	if evt == nil {
		t.Fatal("FromEventMessage 返回 nil")
	}
	if evt.TaskID == nil || *evt.TaskID != "task-1" {
		t.Errorf("TaskID = %v, want 'task-1'", evt.TaskID)
	}
	if evt.Status == nil || *evt.Status != "pending" {
		t.Errorf("Status = %v, want 'pending'", evt.Status)
	}
}

// TestFromEventMessage_消息事件 验证 message 的 MessageID/FromMemberName 映射
func TestFromEventMessage_消息事件(t *testing.T) {
	msg := &events.EventMessage{
		EventType: "message",
		Payload: map[string]any{
			"team_name":       "team1",
			"message_id":      "msg-1",
			"from_member_name": "m1",
			"to_member_name":   "m2",
		},
	}
	evt := FromEventMessage(msg)
	if evt == nil {
		t.Fatal("FromEventMessage 返回 nil")
	}
	if evt.MessageID == nil || *evt.MessageID != "msg-1" {
		t.Errorf("MessageID = %v, want 'msg-1'", evt.MessageID)
	}
	if evt.FromMemberName == nil || *evt.FromMemberName != "m1" {
		t.Errorf("FromMemberName = %v, want 'm1'", evt.FromMemberName)
	}
	if evt.ToMemberName == nil || *evt.ToMemberName != "m2" {
		t.Errorf("ToMemberName = %v, want 'm2'", evt.ToMemberName)
	}
}
