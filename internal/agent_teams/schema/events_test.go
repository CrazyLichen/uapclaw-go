package schema

import "testing"

// ──────────────────────────── TeamTopic.Build 测试 ────────────────────────────

// TestTeamTopic_Build_团队事件 测试 TeamTopicTeam 构建 topic
func TestTeamTopic_Build_团队事件(t *testing.T) {
	got := TeamTopicTeam.Build("sess123", "myteam")
	want := "session:sess123:team:myteam:team"
	if got != want {
		t.Errorf("期望 %q, 实际 %q", want, got)
	}
}

// TestTeamTopic_Build_任务事件 测试 TeamTopicTask 构建 topic
func TestTeamTopic_Build_任务事件(t *testing.T) {
	got := TeamTopicTask.Build("sess456", "team_a")
	want := "session:sess456:team:team_a:task"
	if got != want {
		t.Errorf("期望 %q, 实际 %q", want, got)
	}
}

// TestTeamTopic_Build_消息事件 测试 TeamTopicMessage 构建 topic
func TestTeamTopic_Build_消息事件(t *testing.T) {
	got := TeamTopicMessage.Build("abc", "xyz")
	want := "session:abc:team:xyz:message"
	if got != want {
		t.Errorf("期望 %q, 实际 %q", want, got)
	}
}

// TestTeamTopic_Build_空字符串 测试空 sessionID 和 teamName
func TestTeamTopic_Build_空字符串(t *testing.T) {
	got := TeamTopicTeam.Build("", "")
	want := "session::team::team"
	if got != want {
		t.Errorf("期望 %q, 实际 %q", want, got)
	}
}

// TestTeamTopic_Build_自定义Topic 测试自定义 TeamTopic 值
func TestTeamTopic_Build_自定义Topic(t *testing.T) {
	custom := TeamTopic("custom")
	got := custom.Build("s1", "t1")
	want := "session:s1:team:t1:custom"
	if got != want {
		t.Errorf("期望 %q, 实际 %q", want, got)
	}
}

// ──────────────────────────── NewEventMessage 测试 ────────────────────────────

// TestNewEventMessage_基本 测试基本创建
func TestNewEventMessage_基本(t *testing.T) {
	payload := map[string]any{"team_name": "test_team"}
	msg := NewEventMessage(TeamEventCreated, payload, "node1")
	if msg.EventType != TeamEventCreated {
		t.Errorf("期望 EventType=%q, 实际=%q", TeamEventCreated, msg.EventType)
	}
	if msg.SenderID != "node1" {
		t.Errorf("期望 SenderID='node1', 实际=%q", msg.SenderID)
	}
	if msg.Payload["team_name"] != "test_team" {
		t.Errorf("期望 Payload[team_name]='test_team', 实际=%v", msg.Payload["team_name"])
	}
}

// TestNewEventMessage_空Payload 测试空 payload
func TestNewEventMessage_空Payload(t *testing.T) {
	msg := NewEventMessage(TeamEventCleaned, nil, "node2")
	if msg.EventType != TeamEventCleaned {
		t.Errorf("期望 EventType=%q, 实际=%q", TeamEventCleaned, msg.EventType)
	}
	if msg.Payload != nil {
		t.Errorf("期望 Payload=nil, 实际=%v", msg.Payload)
	}
	if msg.SenderID != "node2" {
		t.Errorf("期望 SenderID='node2', 实际=%q", msg.SenderID)
	}
}

// TestNewEventMessage_嵌套Payload 测试嵌套 payload
func TestNewEventMessage_嵌套Payload(t *testing.T) {
	payload := map[string]any{
		"team_name": "team1",
		"details": map[string]any{
			"member_count": 5,
			"active":       true,
		},
	}
	msg := NewEventMessage(TeamEventTeamCompleted, payload, "node3")
	if msg.EventType != TeamEventTeamCompleted {
		t.Errorf("期望 EventType=%q, 实际=%q", TeamEventTeamCompleted, msg.EventType)
	}
	details, ok := msg.Payload["details"].(map[string]any)
	if !ok {
		t.Fatal("期望 details 为 map[string]any")
	}
	if details["member_count"] != 5 {
		t.Errorf("期望 member_count=5, 实际=%v", details["member_count"])
	}
	if details["active"] != true {
		t.Errorf("期望 active=true, 实际=%v", details["active"])
	}
}

// TestNewEventMessage_空SenderID 测试空发送者
func TestNewEventMessage_空SenderID(t *testing.T) {
	msg := NewEventMessage(TeamEventMemberSpawned, map[string]any{}, "")
	if msg.SenderID != "" {
		t.Errorf("期望 SenderID 为空, 实际=%q", msg.SenderID)
	}
}

// TestNewEventMessage_各事件类型 测试不同事件类型常量
func TestNewEventMessage_各事件类型(t *testing.T) {
	events := []string{
		TeamEventCreated, TeamEventCleaned, TeamEventStandby, TeamEventTeamCompleted,
		TeamEventMemberSpawned, TeamEventMemberRestarted, TeamEventMemberStatusChanged,
		TeamEventMessage, TeamEventBroadcast,
		TeamEventTaskCreated, TeamEventTaskClaimed, TeamEventTaskCompleted, TeamEventTaskCancelled,
	}
	for _, eventType := range events {
		msg := NewEventMessage(eventType, nil, "")
		if msg.EventType != eventType {
			t.Errorf("期望 EventType=%q, 实际=%q", eventType, msg.EventType)
		}
	}
}

// ──────────────────────────── TypedEvent 测试 ────────────────────────────

// TestTypedEvent_EventTypeName 测试各事件的 EventTypeName 方法
func TestTypedEvent_EventTypeName(t *testing.T) {
	tests := []struct {
		event TypedEvent
		want  string
	}{
		{TaskCreatedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventTaskCreated},
		{TaskClaimedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventTaskClaimed},
		{TaskCompletedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventTaskCompleted},
		{TaskCancelledEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventTaskCancelled},
		{TaskUpdatedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventTaskUpdated},
		{TaskUnblockedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventTaskUnblocked},
		{TaskListDrainedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventTaskListDrained},
		{TaskPlanRequestEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventTaskPlanRequest},
		{TaskPlanResponseEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventTaskPlanResponse},
		{MessageEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventMessage},
		{BroadcastEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventBroadcast},
		{TeamCreatedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventCreated},
		{TeamCleanedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventCleaned},
		{TeamStandbyEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventStandby},
		{TeamCompletedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventTeamCompleted},
		{MemberSpawnedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventMemberSpawned},
		{MemberRestartedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventMemberRestarted},
		{MemberStatusChangedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventMemberStatusChanged},
		{MemberExecutionChangedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventMemberExecutionChanged},
		{MemberShutdownEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventMemberShutdown},
		{MemberCanceledEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventMemberCanceled},
		{PlanApprovalEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventPlanApproval},
		{ToolApprovalResultEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventToolApprovalResult},
		{WorktreeCreatedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventWorktreeCreated},
		{WorktreeRemovedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventWorktreeRemoved},
		{WorkspaceArtifactEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventWorkspaceArtifactUpdated},
		{WorkspaceConflictEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventWorkspaceConflict},
		{WorkspaceLockRequestEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventWorkspaceLockRequest},
		{WorkspaceLockResponseEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}}, TeamEventWorkspaceLockResponse},
	}
	for _, tt := range tests {
		if got := tt.event.EventTypeName(); got != tt.want {
			t.Errorf("%T.EventTypeName() = %q, want %q", tt.event, got, tt.want)
		}
	}
}

// TestEventMessageFromEvent 测试从具体事件创建 EventMessage
func TestEventMessageFromEvent(t *testing.T) {
	e := TaskCreatedEvent{BaseEventMessage: BaseEventMessage{TeamName: "team1"}, TaskID: "task_1", Status: "pending"}
	msg := EventMessageFromEvent(e)
	if msg.EventType != TeamEventTaskCreated {
		t.Errorf("EventType = %q, want %q", msg.EventType, TeamEventTaskCreated)
	}
	if msg.Payload["task_id"] != "task_1" {
		t.Errorf("Payload[task_id] = %v, want task_1", msg.Payload["task_id"])
	}
	if msg.Payload["team_name"] != "team1" {
		t.Errorf("Payload[team_name] = %v, want team1", msg.Payload["team_name"])
	}
}

// TestEventMessageFromEvent_消息事件 测试 MessageEvent 转换
func TestEventMessageFromEvent_消息事件(t *testing.T) {
	e := MessageEvent{
		BaseEventMessage: BaseEventMessage{TeamName: "team1", MemberName: "alice"},
		MessageID:        "msg_1",
		FromMemberName:   "alice",
		ToMemberName:     "bob",
	}
	msg := EventMessageFromEvent(e)
	if msg.EventType != TeamEventMessage {
		t.Errorf("EventType = %q, want %q", msg.EventType, TeamEventMessage)
	}
	if msg.Payload["message_id"] != "msg_1" {
		t.Errorf("Payload[message_id] = %v, want msg_1", msg.Payload["message_id"])
	}
	if msg.Payload["from_member_name"] != "alice" {
		t.Errorf("Payload[from_member_name] = %v, want alice", msg.Payload["from_member_name"])
	}
	if msg.Payload["to_member_name"] != "bob" {
		t.Errorf("Payload[to_member_name] = %v, want bob", msg.Payload["to_member_name"])
	}
}

// TestEventMessageFromEvent_广播事件 测试 BroadcastEvent 转换
func TestEventMessageFromEvent_广播事件(t *testing.T) {
	e := BroadcastEvent{
		BaseEventMessage: BaseEventMessage{TeamName: "team1", MemberName: "leader"},
		MessageID:        "msg_2",
		FromMemberName:   "leader",
	}
	msg := EventMessageFromEvent(e)
	if msg.EventType != TeamEventBroadcast {
		t.Errorf("EventType = %q, want %q", msg.EventType, TeamEventBroadcast)
	}
	if msg.Payload["from_member_name"] != "leader" {
		t.Errorf("Payload[from_member_name] = %v, want leader", msg.Payload["from_member_name"])
	}
}
