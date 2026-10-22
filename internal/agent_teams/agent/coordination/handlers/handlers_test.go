package handlers

import (
	"context"
	"fmt"
	"testing"

	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── AgentLifecycleHandler 测试 ────────────────────────────

func TestAgentLifecycleHandler_GetCallbacks_事件映射(t *testing.T) {
	h := NewAgentLifecycleHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})
	cb := h.GetCallbacks()

	expected := map[string]string{
		string(types.InnerEventTypeUserInput): "OnUserInput",
		events.TeamEventStandby:               "OnStandby",
		events.TeamEventCleaned:               "OnCleaned",
		events.TeamEventToolApprovalResult:    "OnToolApprovalResult",
		events.TeamEventTaskPlanResponse:      "OnTaskPlanResponse",
	}

	if len(cb) != len(expected) {
		t.Errorf("GetCallbacks 返回 %d 项，期望 %d 项", len(cb), len(expected))
	}

	for key := range expected {
		if _, ok := cb[key]; !ok {
			t.Errorf("缺少 event_key=%q", key)
		}
	}
}

func TestAgentLifecycleHandler_OnStandby_暂停轮询(t *testing.T) {
	pollCtrl := &fakePollCtrl{}
	h := NewAgentLifecycleHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, pollCtrl)

	h.OnStandby(context.Background(), types.CoordinationEvent{
		Transport: &events.EventMessage{EventType: events.TeamEventStandby},
	})

	if !pollCtrl.paused {
		t.Error("OnStandby 应暂停轮询")
	}
}

func TestAgentLifecycleHandler_OnCleaned_Leader不关闭(t *testing.T) {
	host := &fakeHost{}
	h := NewAgentLifecycleHandler(host, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})

	h.OnCleaned(context.Background(), types.CoordinationEvent{
		Transport: &events.EventMessage{EventType: events.TeamEventCleaned},
	})

	// Leader 不调用 shutdown_self
	if host.shutdownCalled {
		t.Error("Leader 收到 team_cleaned 不应调用 shutdown_self")
	}
}

func TestAgentLifecycleHandler_OnCleaned_Teammate关闭(t *testing.T) {
	host := &fakeHost{}
	h := NewAgentLifecycleHandler(host, &fakeBP{role: schema.TeamRoleTeammate}, nil, &fakePollCtrl{})

	h.OnCleaned(context.Background(), types.CoordinationEvent{
		Transport: &events.EventMessage{EventType: events.TeamEventCleaned},
	})

	if !host.shutdownCalled {
		t.Error("Teammate 收到 team_cleaned 应调用 shutdown_self")
	}
}

// ──────────────────────────── MemberHandler 测试 ────────────────────────────

func TestMemberHandler_GetCallbacks_事件映射(t *testing.T) {
	h := NewMemberHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{}, nil)
	cb := h.GetCallbacks()

	expected := []string{
		events.TeamEventMemberSpawned,
		events.TeamEventMemberRestarted,
		events.TeamEventMemberStatusChanged,
		events.TeamEventMemberExecutionChanged,
		events.TeamEventMemberShutdown,
		events.TeamEventMemberCanceled,
	}

	if len(cb) != len(expected) {
		t.Errorf("GetCallbacks 返回 %d 项，期望 %d 项", len(cb), len(expected))
	}

	for _, key := range expected {
		if _, ok := cb[key]; !ok {
			t.Errorf("缺少 event_key=%q", key)
		}
	}
}

func TestMemberHandler_OnMemberEvent_Teammate取消(t *testing.T) {
	host := &fakeHost{}
	h := NewMemberHandler(host, &fakeBP{role: schema.TeamRoleTeammate, memberName: "worker1"}, nil, &fakePollCtrl{}, nil)

	h.OnMemberEvent(context.Background(), types.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventMemberCanceled,
			Payload:   map[string]any{"member_name": "worker1"},
		},
	})

	if !host.cancelCalled {
		t.Error("MEMBER_CANCELED 应调用 cancel_agent")
	}
}

func TestMemberHandler_OnMemberEvent_非自身事件跳过(t *testing.T) {
	host := &fakeHost{}
	h := NewMemberHandler(host, &fakeBP{role: schema.TeamRoleTeammate, memberName: "worker1"}, nil, &fakePollCtrl{}, nil)

	h.OnMemberEvent(context.Background(), types.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventMemberCanceled,
			Payload:   map[string]any{"member_name": "worker2"},
		},
	})

	if host.cancelCalled {
		t.Error("非自身 MEMBER_CANCELED 不应调用 cancel_agent")
	}
}

// ──────────────────────────── MessageHandler 测试 ────────────────────────────

func TestMessageHandler_GetCallbacks_事件映射(t *testing.T) {
	h := NewMessageHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})
	cb := h.GetCallbacks()

	expected := map[string]bool{
		events.TeamEventMessage:                 true,
		events.TeamEventBroadcast:               true,
		string(types.InnerEventTypePollMailbox): true,
		events.TeamEventMemberShutdown:          true,
	}

	if len(cb) != len(expected) {
		t.Errorf("GetCallbacks 返回 %d 项，期望 %d 项", len(cb), len(expected))
	}

	for key := range expected {
		if _, ok := cb[key]; !ok {
			t.Errorf("缺少 event_key=%q", key)
		}
	}
}

func TestMessageHandler_OnMemberShutdownDrain_Leader跳过(t *testing.T) {
	pollCtrl := &fakePollCtrl{}
	h := NewMessageHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader, memberName: "leader"}, nil, pollCtrl)

	// Leader 应跳过，不应调用任何消息处理
	h.OnMemberShutdownDrain(context.Background(), types.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventMemberShutdown,
			Payload:   map[string]any{"member_name": "leader"},
		},
	})
	// 无 panic 即通过
}

// ──────────────────────────── TaskBoardHandler 测试 ────────────────────────────

func TestTaskBoardHandler_GetCallbacks_事件映射(t *testing.T) {
	h := NewTaskBoardHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})
	cb := h.GetCallbacks()

	expected := map[string]bool{
		events.TeamEventTaskClaimed:      true,
		events.TeamEventTaskCreated:      true,
		events.TeamEventTaskPlanRequest:  true,
		events.TeamEventTaskPlanResponse: true,
		events.TeamEventTaskUpdated:      true,
		events.TeamEventTaskCompleted:    true,
		events.TeamEventTaskCancelled:    true,
		events.TeamEventTaskUnblocked:    true,
	}

	if len(cb) != len(expected) {
		t.Errorf("GetCallbacks 返回 %d 项，期望 %d 项", len(cb), len(expected))
	}

	for key := range expected {
		if _, ok := cb[key]; !ok {
			t.Errorf("缺少 event_key=%q", key)
		}
	}
}

func TestTaskBoardHandler_OnTaskPlanDecision_有ToolCallID跳过(t *testing.T) {
	host := &fakeHost{}
	// S-附录4: infra 提供 TaskManager 使前置守卫通过，
	// 真正验证 tool_call_id 分支（原 nil infra 版本被守卫短路退化成恒真）
	db := database.NewInMemoryTeamDatabase()
	if err := db.Initialize(context.Background()); err != nil {
		t.Fatalf("db init 失败: %v", err)
	}
	taskMgr := tools.NewTeamTaskManager(db, "t1", "w1", nil, nil, t.TempDir(), "plan-1", "leader")
	infra := &handlerTestInfra{taskMgr: taskMgr}
	h := NewTaskBoardHandler(host, &fakeBP{role: schema.TeamRoleTeammate, memberName: "w1"}, infra, &fakePollCtrl{})

	h.OnTaskPlanDecision(context.Background(), types.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventTaskPlanResponse,
			Payload: map[string]any{
				"member_name":  "w1",
				"tool_call_id": "tc-123",
				"approved":     true,
			},
		},
	})

	if host.deliverCalled {
		t.Error("有 tool_call_id 时应跳过额外 deliver_input")
	}
}

// TestTaskBoardHandler_OnTaskPlanDecision_批准投递模板 S-附录4: 给自己的审批
// （member_name 匹配 + 无 tool_call_id）→ 投递 task_plan_approved_to_self
func TestTaskBoardHandler_OnTaskPlanDecision_批准投递模板(t *testing.T) {
	host := &fakeHost{}
	db := database.NewInMemoryTeamDatabase()
	if err := db.Initialize(context.Background()); err != nil {
		t.Fatalf("db init 失败: %v", err)
	}
	taskMgr := tools.NewTeamTaskManager(db, "t1", "w1", nil, nil, t.TempDir(), "plan-1", "leader")
	infra := &handlerTestInfra{taskMgr: taskMgr}
	h := NewTaskBoardHandler(host, &fakeBP{role: schema.TeamRoleTeammate, memberName: "w1"}, infra, &fakePollCtrl{})

	h.OnTaskPlanDecision(context.Background(), types.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventTaskPlanResponse,
			Payload: map[string]any{
				"member_name": "w1",
				"task_id":     "task-9",
				"approved":    true,
				"feedback":    "good",
			},
		},
	})

	if !host.deliverCalled {
		t.Fatal("给自己的审批（无 tool_call_id）应投递模板内容")
	}
}

// TestTaskBoardHandler_OnTaskPlanDecision_他人审批转发 S-附录4:
// member_name 不匹配 → 转发 OnTaskBoardEvent（恢复轮询，不走计划模板路径）
func TestTaskBoardHandler_OnTaskPlanDecision_他人审批转发(t *testing.T) {
	host := &fakeHost{}
	db := database.NewInMemoryTeamDatabase()
	if err := db.Initialize(context.Background()); err != nil {
		t.Fatalf("db init 失败: %v", err)
	}
	taskMgr := tools.NewTeamTaskManager(db, "t1", "w2", nil, nil, t.TempDir(), "plan-1", "leader")
	infra := &handlerTestInfra{taskMgr: taskMgr}
	pollCtrl := &fakePollCtrl{}
	h := NewTaskBoardHandler(host, &fakeBP{role: schema.TeamRoleTeammate, memberName: "w1"}, infra, pollCtrl)

	h.OnTaskPlanDecision(context.Background(), types.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventTaskPlanResponse,
			Payload: map[string]any{
				"member_name": "other-member",
				"task_id":     "task-1",
				"approved":    true,
			},
		},
	})

	// 转发路径会走 OnTaskBoardEvent → resume_polls
	if !pollCtrl.resumed {
		t.Error("他人审批应转发 OnTaskBoardEvent 并恢复轮询")
	}
}

// ──────────────────────────── StaleTaskHandler 测试 ────────────────────────────

func TestStaleTaskHandler_GetCallbacks_事件映射(t *testing.T) {
	h := NewStaleTaskHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{}, nil)
	cb := h.GetCallbacks()

	if len(cb) != 1 {
		t.Errorf("GetCallbacks 返回 %d 项，期望 1 项", len(cb))
	}
	if _, ok := cb[string(types.InnerEventTypePollTask)]; !ok {
		t.Error("缺少 coordination_poll_task 事件键")
	}
}

func TestStaleTaskHandler_共享节流映射(t *testing.T) {
	throttle := make(map[string]float64)
	h1 := NewMemberHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{}, throttle)
	h2 := NewStaleTaskHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{}, throttle)

	// 两个 handler 应共享同一个节流映射
	throttle["task-1"] = 123.0
	if h1.staleClaimThrottle["task-1"] != 123.0 {
		t.Error("MemberHandler 应共享 staleClaimThrottle 引用")
	}
	if h2.staleClaimThrottle["task-1"] != 123.0 {
		t.Error("StaleTaskHandler 应共享 staleClaimThrottle 引用")
	}
}

// ──────────────────────────── TeamCompletionHandler 测试 ────────────────────────────

func TestTeamCompletionHandler_GetCallbacks_事件映射(t *testing.T) {
	h := NewTeamCompletionHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})
	cb := h.GetCallbacks()

	expected := map[string]bool{
		string(types.InnerEventTypePollTask): true,
		events.TeamEventTaskListDrained:      true,
		events.TeamEventTeamCompleted:        true,
	}

	if len(cb) != len(expected) {
		t.Errorf("GetCallbacks 返回 %d 项，期望 %d 项", len(cb), len(expected))
	}

	for key := range expected {
		if _, ok := cb[key]; !ok {
			t.Errorf("缺少 event_key=%q", key)
		}
	}
}

func TestTeamCompletionHandler_Rearm(t *testing.T) {
	h := NewTeamCompletionHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})
	h.teamCompletedEmitted = true

	h.Rearm()

	if h.teamCompletedEmitted {
		t.Error("Rearm 后 teamCompletedEmitted 应为 false")
	}
}

func TestTeamCompletionHandler_OnTaskListDrained_触发回调(t *testing.T) {
	h := NewTeamCompletionHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})

	called1 := false
	called2 := false
	h.RegisterCompletionCallback(func(_ context.Context) error {
		called1 = true
		return nil
	})
	h.RegisterCompletionCallback(func(_ context.Context) error {
		called2 = true
		return nil
	})

	h.OnTaskListDrained(context.Background(), types.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventTaskListDrained,
			Payload:   map[string]any{},
		},
	})

	if !called1 || !called2 {
		t.Error("OnTaskListDrained 应触发所有注册的完成回调")
	}
}

func TestTeamCompletionHandler_OnTaskListDrained_回调失败不阻断(t *testing.T) {
	h := NewTeamCompletionHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})

	called2 := false
	h.RegisterCompletionCallback(func(_ context.Context) error {
		return fmt.Errorf("模拟失败")
	})
	h.RegisterCompletionCallback(func(_ context.Context) error {
		called2 = true
		return nil
	})

	h.OnTaskListDrained(context.Background(), types.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventTaskListDrained,
			Payload:   map[string]any{},
		},
	})

	if !called2 {
		t.Error("第一个回调失败不应阻断后续回调")
	}
}

func TestTeamCompletionHandler_OnPollTask_已发出时跳过(t *testing.T) {
	h := NewTeamCompletionHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})
	h.teamCompletedEmitted = true

	// 应直接返回，不执行任何逻辑
	h.OnPollTask(context.Background(), types.CoordinationEvent{
		Inner: &types.InnerEventMessage{EventType: types.InnerEventTypePollTask},
	})
	// 无 panic 即通过
}

func TestTeamCompletionHandler_OnPollTask_非Leader跳过(t *testing.T) {
	h := NewTeamCompletionHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleTeammate}, nil, &fakePollCtrl{})

	h.OnPollTask(context.Background(), types.CoordinationEvent{
		Inner: &types.InnerEventMessage{EventType: types.InnerEventTypePollTask},
	})
	// 无 panic 即通过
}
