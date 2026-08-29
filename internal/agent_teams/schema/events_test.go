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

// ──────────────────────────── ToPayload 测试 ────────────────────────────

// TestToPayload_团队生命周期事件 测试团队生命周期事件的 ToPayload 方法
func TestToPayload_团队生命周期事件(t *testing.T) {
	t.Run("TeamCreatedEvent", func(t *testing.T) {
		e := TeamCreatedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, DisplayName: "显示名", LeaderMemberName: "leader1", Created: 12345}
		p := e.ToPayload()
		if p["team_name"] != "t1" {
			t.Errorf("team_name = %v, want t1", p["team_name"])
		}
		if p["display_name"] != "显示名" {
			t.Errorf("display_name = %v, want 显示名", p["display_name"])
		}
		if p["leader_member_name"] != "leader1" {
			t.Errorf("leader_member_name = %v, want leader1", p["leader_member_name"])
		}
		if p["created"] != int64(12345) {
			t.Errorf("created = %v, want 12345", p["created"])
		}
	})

	t.Run("TeamCleanedEvent", func(t *testing.T) {
		e := TeamCleanedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t2"}}
		p := e.ToPayload()
		if p["team_name"] != "t2" {
			t.Errorf("team_name = %v, want t2", p["team_name"])
		}
		if len(p) != 1 {
			t.Errorf("payload 长度 = %d, want 1", len(p))
		}
	})

	t.Run("TeamStandbyEvent", func(t *testing.T) {
		e := TeamStandbyEvent{BaseEventMessage: BaseEventMessage{TeamName: "t3"}}
		p := e.ToPayload()
		if p["team_name"] != "t3" {
			t.Errorf("team_name = %v, want t3", p["team_name"])
		}
	})

	t.Run("TeamCompletedEvent", func(t *testing.T) {
		e := TeamCompletedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t4"}, MemberCount: 3, TaskCount: 5}
		p := e.ToPayload()
		if p["team_name"] != "t4" {
			t.Errorf("team_name = %v, want t4", p["team_name"])
		}
		if p["member_count"] != 3 {
			t.Errorf("member_count = %v, want 3", p["member_count"])
		}
		if p["task_count"] != 5 {
			t.Errorf("task_count = %v, want 5", p["task_count"])
		}
	})
}

// TestToPayload_成员生命周期事件 测试成员生命周期事件的 ToPayload 方法
func TestToPayload_成员生命周期事件(t *testing.T) {
	t.Run("MemberSpawnedEvent", func(t *testing.T) {
		e := MemberSpawnedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1", MemberName: "m1"}}
		p := e.ToPayload()
		if p["team_name"] != "t1" {
			t.Errorf("team_name = %v, want t1", p["team_name"])
		}
		if p["member_name"] != "m1" {
			t.Errorf("member_name = %v, want m1", p["member_name"])
		}
	})

	t.Run("MemberRestartedEvent", func(t *testing.T) {
		e := MemberRestartedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1", MemberName: "m1"}, Reason: "crash", RestartCount: 2}
		p := e.ToPayload()
		if p["reason"] != "crash" {
			t.Errorf("reason = %v, want crash", p["reason"])
		}
		if p["restart_count"] != 2 {
			t.Errorf("restart_count = %v, want 2", p["restart_count"])
		}
	})

	t.Run("MemberStatusChangedEvent", func(t *testing.T) {
		e := MemberStatusChangedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1", MemberName: "m1"}, OldStatus: "idle", NewStatus: "running"}
		p := e.ToPayload()
		if p["old_status"] != "idle" {
			t.Errorf("old_status = %v, want idle", p["old_status"])
		}
		if p["new_status"] != "running" {
			t.Errorf("new_status = %v, want running", p["new_status"])
		}
	})

	t.Run("MemberExecutionChangedEvent", func(t *testing.T) {
		e := MemberExecutionChangedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1", MemberName: "m1"}, OldStatus: "busy", NewStatus: "free"}
		p := e.ToPayload()
		if p["old_status"] != "busy" {
			t.Errorf("old_status = %v, want busy", p["old_status"])
		}
		if p["new_status"] != "free" {
			t.Errorf("new_status = %v, want free", p["new_status"])
		}
	})

	t.Run("MemberShutdownEvent", func(t *testing.T) {
		e := MemberShutdownEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1", MemberName: "m1"}, Force: true}
		p := e.ToPayload()
		if p["force"] != true {
			t.Errorf("force = %v, want true", p["force"])
		}
	})

	t.Run("MemberCanceledEvent", func(t *testing.T) {
		e := MemberCanceledEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1", MemberName: "m1"}}
		p := e.ToPayload()
		if p["member_name"] != "m1" {
			t.Errorf("member_name = %v, want m1", p["member_name"])
		}
	})
}

// TestToPayload_协作事件 测试协作事件的 ToPayload 方法
func TestToPayload_协作事件(t *testing.T) {
	t.Run("PlanApprovalEvent", func(t *testing.T) {
		e := PlanApprovalEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1", MemberName: "m1"}, Approved: true}
		p := e.ToPayload()
		if p["approved"] != true {
			t.Errorf("approved = %v, want true", p["approved"])
		}
	})

	t.Run("ToolApprovalResultEvent", func(t *testing.T) {
		e := ToolApprovalResultEvent{
			BaseEventMessage: BaseEventMessage{TeamName: "t1", MemberName: "m1"},
			ToolCallID:       "tc1", Approved: false, Feedback: "retry", AutoConfirm: true,
		}
		p := e.ToPayload()
		if p["tool_call_id"] != "tc1" {
			t.Errorf("tool_call_id = %v, want tc1", p["tool_call_id"])
		}
		if p["approved"] != false {
			t.Errorf("approved = %v, want false", p["approved"])
		}
		if p["feedback"] != "retry" {
			t.Errorf("feedback = %v, want retry", p["feedback"])
		}
		if p["auto_confirm"] != true {
			t.Errorf("auto_confirm = %v, want true", p["auto_confirm"])
		}
	})
}

// TestToPayload_消息事件 测试消息事件的 ToPayload 方法
func TestToPayload_消息事件(t *testing.T) {
	t.Run("MessageEvent", func(t *testing.T) {
		e := MessageEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, MessageID: "mid1", FromMemberName: "alice", ToMemberName: "bob"}
		p := e.ToPayload()
		if p["message_id"] != "mid1" {
			t.Errorf("message_id = %v, want mid1", p["message_id"])
		}
		if p["from_member_name"] != "alice" {
			t.Errorf("from_member_name = %v, want alice", p["from_member_name"])
		}
		if p["to_member_name"] != "bob" {
			t.Errorf("to_member_name = %v, want bob", p["to_member_name"])
		}
	})

	t.Run("BroadcastEvent", func(t *testing.T) {
		e := BroadcastEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, MessageID: "mid2", FromMemberName: "leader"}
		p := e.ToPayload()
		if p["from_member_name"] != "leader" {
			t.Errorf("from_member_name = %v, want leader", p["from_member_name"])
		}
	})
}

// TestToPayload_任务事件 测试任务事件的 ToPayload 方法
func TestToPayload_任务事件(t *testing.T) {
	t.Run("TaskCreatedEvent", func(t *testing.T) {
		e := TaskCreatedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, TaskID: "task1", Status: "pending"}
		p := e.ToPayload()
		if p["task_id"] != "task1" {
			t.Errorf("task_id = %v, want task1", p["task_id"])
		}
		if p["status"] != "pending" {
			t.Errorf("status = %v, want pending", p["status"])
		}
	})

	t.Run("TaskPlanRequestEvent", func(t *testing.T) {
		e := TaskPlanRequestEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, TaskID: "task1", Status: "planning", PlanID: "plan1", MemberPlanMD: "plan.md", ToolCallID: "tc1"}
		p := e.ToPayload()
		if p["plan_id"] != "plan1" {
			t.Errorf("plan_id = %v, want plan1", p["plan_id"])
		}
		if p["member_plan_md"] != "plan.md" {
			t.Errorf("member_plan_md = %v, want plan.md", p["member_plan_md"])
		}
		if p["tool_call_id"] != "tc1" {
			t.Errorf("tool_call_id = %v, want tc1", p["tool_call_id"])
		}
	})

	t.Run("TaskPlanResponseEvent", func(t *testing.T) {
		e := TaskPlanResponseEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, TaskID: "task1", Approved: true, Status: "approved", PlanID: "plan1", Feedback: "ok", ToolCallID: "tc2"}
		p := e.ToPayload()
		if p["approved"] != true {
			t.Errorf("approved = %v, want true", p["approved"])
		}
		if p["feedback"] != "ok" {
			t.Errorf("feedback = %v, want ok", p["feedback"])
		}
	})

	t.Run("TaskUpdatedEvent", func(t *testing.T) {
		e := TaskUpdatedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, TaskID: "task2"}
		p := e.ToPayload()
		if p["task_id"] != "task2" {
			t.Errorf("task_id = %v, want task2", p["task_id"])
		}
	})

	t.Run("TaskClaimedEvent", func(t *testing.T) {
		e := TaskClaimedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, TaskID: "task3"}
		p := e.ToPayload()
		if p["task_id"] != "task3" {
			t.Errorf("task_id = %v, want task3", p["task_id"])
		}
	})

	t.Run("TaskCompletedEvent", func(t *testing.T) {
		e := TaskCompletedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, TaskID: "task4"}
		p := e.ToPayload()
		if p["task_id"] != "task4" {
			t.Errorf("task_id = %v, want task4", p["task_id"])
		}
	})

	t.Run("TaskCancelledEvent", func(t *testing.T) {
		e := TaskCancelledEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, TaskID: "task5"}
		p := e.ToPayload()
		if p["task_id"] != "task5" {
			t.Errorf("task_id = %v, want task5", p["task_id"])
		}
	})

	t.Run("TaskUnblockedEvent", func(t *testing.T) {
		e := TaskUnblockedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, TaskID: "task6"}
		p := e.ToPayload()
		if p["task_id"] != "task6" {
			t.Errorf("task_id = %v, want task6", p["task_id"])
		}
	})

	t.Run("TaskListDrainedEvent", func(t *testing.T) {
		e := TaskListDrainedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, TaskCount: 7}
		p := e.ToPayload()
		if p["task_count"] != 7 {
			t.Errorf("task_count = %v, want 7", p["task_count"])
		}
	})
}

// TestToPayload_Worktree事件 测试 Worktree 事件的 ToPayload 方法
func TestToPayload_Worktree事件(t *testing.T) {
	t.Run("WorktreeCreatedEvent", func(t *testing.T) {
		e := WorktreeCreatedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, WorktreeName: "wt1", WorktreePath: "/tmp/wt1", Existed: true}
		p := e.ToPayload()
		if p["worktree_name"] != "wt1" {
			t.Errorf("worktree_name = %v, want wt1", p["worktree_name"])
		}
		if p["worktree_path"] != "/tmp/wt1" {
			t.Errorf("worktree_path = %v, want /tmp/wt1", p["worktree_path"])
		}
		if p["existed"] != true {
			t.Errorf("existed = %v, want true", p["existed"])
		}
	})

	t.Run("WorktreeRemovedEvent", func(t *testing.T) {
		e := WorktreeRemovedEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, WorktreeName: "wt2", WorktreePath: "/tmp/wt2"}
		p := e.ToPayload()
		if p["worktree_name"] != "wt2" {
			t.Errorf("worktree_name = %v, want wt2", p["worktree_name"])
		}
		if p["worktree_path"] != "/tmp/wt2" {
			t.Errorf("worktree_path = %v, want /tmp/wt2", p["worktree_path"])
		}
	})
}

// TestToPayload_Workspace事件 测试 Workspace 事件的 ToPayload 方法
func TestToPayload_Workspace事件(t *testing.T) {
	t.Run("WorkspaceArtifactEvent", func(t *testing.T) {
		e := WorkspaceArtifactEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, ArtifactPath: "a.go", CommitSHA: "abc123"}
		p := e.ToPayload()
		if p["artifact_path"] != "a.go" {
			t.Errorf("artifact_path = %v, want a.go", p["artifact_path"])
		}
		if p["commit_sha"] != "abc123" {
			t.Errorf("commit_sha = %v, want abc123", p["commit_sha"])
		}
	})

	t.Run("WorkspaceConflictEvent", func(t *testing.T) {
		e := WorkspaceConflictEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, FilePath: "f.go", ConflictingCommit: "def456"}
		p := e.ToPayload()
		if p["file_path"] != "f.go" {
			t.Errorf("file_path = %v, want f.go", p["file_path"])
		}
		if p["conflicting_commit"] != "def456" {
			t.Errorf("conflicting_commit = %v, want def456", p["conflicting_commit"])
		}
	})

	t.Run("WorkspaceLockRequestEvent", func(t *testing.T) {
		e := WorkspaceLockRequestEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, Action: "acquire", FilePath: "g.go", HolderName: "m1", TimeoutSeconds: 30}
		p := e.ToPayload()
		if p["action"] != "acquire" {
			t.Errorf("action = %v, want acquire", p["action"])
		}
		if p["file_path"] != "g.go" {
			t.Errorf("file_path = %v, want g.go", p["file_path"])
		}
		if p["holder_name"] != "m1" {
			t.Errorf("holder_name = %v, want m1", p["holder_name"])
		}
		if p["timeout_seconds"] != 30 {
			t.Errorf("timeout_seconds = %v, want 30", p["timeout_seconds"])
		}
	})

	t.Run("WorkspaceLockResponseEvent", func(t *testing.T) {
		holder := map[string]any{"name": "m1"}
		e := WorkspaceLockResponseEvent{BaseEventMessage: BaseEventMessage{TeamName: "t1"}, FilePath: "g.go", Granted: false, Holder: holder}
		p := e.ToPayload()
		if p["granted"] != false {
			t.Errorf("granted = %v, want false", p["granted"])
		}
		if p["file_path"] != "g.go" {
			t.Errorf("file_path = %v, want g.go", p["file_path"])
		}
	})
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
