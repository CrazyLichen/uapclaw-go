package handlers

import (
	"context"
	"strings"
	"testing"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	types "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderTaskAssignedToSelf_普通成员 测试普通成员任务分配渲染。
// S-附录4: 对齐 Python t("dispatcher.task_assigned_to_self", task_id=...)
func TestRenderTaskAssignedToSelf_普通成员(t *testing.T) {
	result := renderTaskAssignedToSelf(context.Background(), nil, "task-001", false)
	if !strings.Contains(result, "task-001") {
		t.Errorf("内容应包含 task_id=task-001, got %q", result)
	}
	if !strings.Contains(result, "view_task") {
		t.Errorf("普通成员模板应引导 view_task, got %q", result)
	}
}

// TestRenderTaskAssignedToSelf_人类代理 测试人类代理模板渲染
func TestRenderTaskAssignedToSelf_人类代理(t *testing.T) {
	result := renderTaskAssignedToSelf(context.Background(), nil, "task-002", true)
	if !strings.Contains(result, "task-002") {
		t.Errorf("内容应包含 task_id=task-002, got %q", result)
	}
	if !strings.Contains(result, "控制者") {
		t.Errorf("HITT 模板应是给控制者的通知, got %q", result)
	}
}

// TestRenderTaskAssignedToSelf_空任务ID 测试空 taskID 不 panic
func TestRenderTaskAssignedToSelf_空任务ID(t *testing.T) {
	result := renderTaskAssignedToSelf(context.Background(), nil, "", false)
	if result == "" {
		t.Error("不应返回空字符串")
	}
}

// ──────────────────────────── OnTaskUpdated 测试 ────────────────────────────

// newTaskBoardFixture 造一个带真实 TaskManager 的 TaskBoardHandler。
func newTaskBoardFixture(t *testing.T, memberName string) (*TaskBoardHandler, *fakeHost, *fakePollCtrl) {
	t.Helper()
	db := database.NewInMemoryTeamDatabase()
	require.NoError(t, db.Initialize(context.Background()))
	backend := tools.NewTeamBackend("t1", memberName, false, db, nil)

	host := &fakeHost{}
	bp := &fakeBP{role: schema.TeamRoleTeammate, memberName: memberName}
	infra := &handlerTestInfra{taskMgr: backend.TaskManager(), backend: backend}
	poll := &fakePollCtrl{}
	return NewTaskBoardHandler(host, bp, infra, poll), host, poll
}

// taskUpdatedEvent 造一条 TASK_UPDATED 传输事件。
func taskUpdatedEvent(memberName, taskID string) types.CoordinationEvent {
	return types.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventTaskUpdated,
			Payload:   map[string]any{"member_name": memberName, "task_id": taskID},
		},
	}
}

// TestOnTaskUpdated_自己触发announce 命中自己时恢复轮询并调 AnnounceTaskRevisions。
func TestOnTaskUpdated_自己触发announce(t *testing.T) {
	h, host, poll := newTaskBoardFixture(t, "alice")

	h.OnTaskUpdated(context.Background(), taskUpdatedEvent("alice", "t1"))

	assert.Equal(t, 1, host.announceCalls, "命中自己应触发一次 announce")
	assert.True(t, poll.resumed, "命中自己应恢复轮询")
}

// TestOnTaskUpdated_他人走任务板 他人编辑不触发 announce。
func TestOnTaskUpdated_他人走任务板(t *testing.T) {
	h, host, _ := newTaskBoardFixture(t, "alice")

	h.OnTaskUpdated(context.Background(), taskUpdatedEvent("bob", "t1"))

	assert.Equal(t, 0, host.announceCalls, "他人编辑不应触发 announce")
}

// TestOnTaskUpdated_成员名为空直接返回 未配置成员名时不动作。
func TestOnTaskUpdated_成员名为空直接返回(t *testing.T) {
	h, host, _ := newTaskBoardFixture(t, "")

	h.OnTaskUpdated(context.Background(), taskUpdatedEvent("alice", "t1"))

	assert.Equal(t, 0, host.announceCalls)
}

// TestOnTaskUpdated_taskManager为空直接返回 无任务管理器时不动作。
func TestOnTaskUpdated_taskManager为空直接返回(t *testing.T) {
	host := &fakeHost{}
	h := NewTaskBoardHandler(host,
		&fakeBP{role: schema.TeamRoleTeammate, memberName: "alice"},
		&handlerTestInfra{}, &fakePollCtrl{})

	h.OnTaskUpdated(context.Background(), taskUpdatedEvent("alice", "t1"))

	assert.Equal(t, 0, host.announceCalls, "无 task_manager 应直接返回")
}

// TestGetCallbacks_taskUpdated映射到OnTaskUpdated 注册表指向新方法。
func TestGetCallbacks_taskUpdated映射到OnTaskUpdated(t *testing.T) {
	h, _, _ := newTaskBoardFixture(t, "alice")
	cbs := h.GetCallbacks()
	require.Contains(t, cbs, events.TeamEventTaskUpdated)
	assert.NotNil(t, cbs[events.TeamEventTaskUpdated])
}

// ──────────────────────────── 25.4 七事件路由测试 ────────────────────────────

// transportEvent 造一条跨进程任务事件。
func transportEvent(eventType, memberName, taskID string, extra map[string]any) types.CoordinationEvent {
	payload := map[string]any{"member_name": memberName, "task_id": taskID}
	for k, v := range extra {
		payload[k] = v
	}
	return types.CoordinationEvent{
		Transport: &events.EventMessage{EventType: eventType, Payload: payload},
	}
}

// deliverRecorder 带投递记录的 host 假实现（复用 fakeHost 行为，多记 content）。
type deliverRecorder struct {
	fakeHost
	contents []string
}

func (f *deliverRecorder) DeliverInput(_ context.Context, content string, _ bool, _ *agentinterfaces.RunContext) error {
	f.deliverCalled = true
	f.contents = append(f.contents, content)
	return nil
}

// newBoardFixtureWithHost 指定 host/角色造 TaskBoardHandler。
func newBoardFixtureWithHost(t *testing.T, memberName string, host types.DispatcherHost,
	role schema.TeamRole) (*TaskBoardHandler, *fakePollCtrl) {
	t.Helper()
	db := database.NewInMemoryTeamDatabase()
	require.NoError(t, db.Initialize(context.Background()))
	backend := tools.NewTeamBackend("t1", memberName, role == schema.TeamRoleLeader, db, nil)
	poll := &fakePollCtrl{}
	return NewTaskBoardHandler(host, &fakeBP{role: role, memberName: memberName},
		&handlerTestInfra{taskMgr: backend.TaskManager(), backend: backend}, poll), poll
}

// TestOnTaskRevoked_自己被steer 撤回指向自己时投递 task-revoked。
func TestOnTaskRevoked_自己被steer(t *testing.T) {
	host := &deliverRecorder{}
	h, poll := newBoardFixtureWithHost(t, "alice", host, schema.TeamRoleTeammate)
	h.OnTaskRevoked(context.Background(), transportEvent(events.TeamEventTaskRevoked, "alice", "t1", nil))
	assert.True(t, host.deliverCalled, "撤回自己应投递")
	assert.True(t, poll.resumed)
	if len(host.contents) == 1 && !strings.Contains(host.contents[0], "task-revoked") {
		t.Errorf("投递内容应包 <team-event kind=\"task-revoked\">: %q", host.contents[0])
	}
}

// TestOnTaskRevoked_他人忽略 撤回他人不投递、也不走看板（reset 已发 RELEASED）。
func TestOnTaskRevoked_他人忽略(t *testing.T) {
	host := &deliverRecorder{}
	h, _ := newBoardFixtureWithHost(t, "alice", host, schema.TeamRoleTeammate)
	h.OnTaskRevoked(context.Background(), transportEvent(events.TeamEventTaskRevoked, "bob", "t1", nil))
	assert.False(t, host.deliverCalled)
}

// TestOnTaskCancelled_自己steer 取消指向自己投递 task-cancelled。
func TestOnTaskCancelled_自己steer(t *testing.T) {
	host := &deliverRecorder{}
	h, _ := newBoardFixtureWithHost(t, "alice", host, schema.TeamRoleTeammate)
	h.OnTaskCancelled(context.Background(), transportEvent(events.TeamEventTaskCancelled, "alice", "t1", nil))
	assert.True(t, host.deliverCalled)
}

// TestOnTaskSubmittedForReview_reviewer路由 reviewer 命中（[]string 与 []any 双形态）/未命中落看板。
func TestOnTaskSubmittedForReview_reviewer路由(t *testing.T) {
	host := &deliverRecorder{}
	h, poll := newBoardFixtureWithHost(t, "alice", host, schema.TeamRoleTeammate)
	h.OnTaskSubmittedForReview(context.Background(), transportEvent(
		events.TeamEventTaskSubmittedForReview, "bob", "t1", map[string]any{"reviewer": []string{"alice"}}))
	assert.True(t, host.deliverCalled, "reviewer 应被 steer")
	assert.True(t, poll.resumed)

	host2 := &deliverRecorder{}
	h2, _ := newBoardFixtureWithHost(t, "alice", host2, schema.TeamRoleTeammate)
	h2.OnTaskSubmittedForReview(context.Background(), transportEvent(
		events.TeamEventTaskSubmittedForReview, "bob", "t1", map[string]any{"reviewer": []any{"alice"}}))
	assert.True(t, host2.deliverCalled, "[]any 形态 reviewer 也应命中")

	host3 := &deliverRecorder{}
	h3, _ := newBoardFixtureWithHost(t, "carol", host3, schema.TeamRoleTeammate)
	h3.OnTaskSubmittedForReview(context.Background(), transportEvent(
		events.TeamEventTaskSubmittedForReview, "bob", "t1", map[string]any{"reviewer": []string{"alice"}}))
	assert.False(t, host3.deliverCalled, "非 reviewer 落看板（空板静默）")
}

// TestOnTaskRevisionRequested_作者命中 打回指向作者投递；他人落看板（空板静默）。
func TestOnTaskRevisionRequested_作者命中(t *testing.T) {
	host := &deliverRecorder{}
	h, _ := newBoardFixtureWithHost(t, "alice", host, schema.TeamRoleTeammate)
	h.OnTaskRevisionRequested(context.Background(), transportEvent(
		events.TeamEventTaskRevisionRequested, "alice", "t1", map[string]any{"feedback": "改"}))
	assert.True(t, host.deliverCalled)

	host2 := &deliverRecorder{}
	h2, _ := newBoardFixtureWithHost(t, "carol", host2, schema.TeamRoleTeammate)
	h2.OnTaskRevisionRequested(context.Background(), transportEvent(
		events.TeamEventTaskRevisionRequested, "alice", "t1", nil))
	assert.False(t, host2.deliverCalled)
}

// TestOnTaskVerified_作者解锁 pass 指向作者投递 task-verified。
func TestOnTaskVerified_作者解锁(t *testing.T) {
	host := &deliverRecorder{}
	h, poll := newBoardFixtureWithHost(t, "alice", host, schema.TeamRoleTeammate)
	h.OnTaskVerified(context.Background(), transportEvent(events.TeamEventTaskVerified, "alice", "t1", nil))
	assert.True(t, host.deliverCalled)
	assert.True(t, poll.resumed)
}

// TestOnTaskBoardEvent_teammateNudge集 teammate 只被 CREATED/UNBLOCKED/RELEASED 唤醒。
// 夹具预置一张 pending 无主票：nudge 被放行时必有投递（空板则静默，无法区分过滤）。
func TestOnTaskBoardEvent_teammateNudge集(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	require.NoError(t, db.Initialize(context.Background()))
	db.CreateTeam(context.Background(), "t1", "Team", "leader", "", "", "autonomous", false)
	db.CreateMember(context.Background(), "alice", "t1", "A", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	if ok, err := db.CreateTask(context.Background(), &database.TeamTaskBase{
		TaskID: "t-open", TeamName: "t1", Title: "T", Content: "C", Status: fsm.TaskStatusPending}); err != nil || !ok {
		t.Fatalf("建任务失败: %v", err)
	}
	backend := tools.NewTeamBackend("t1", "alice", false, db, nil)
	poll := &fakePollCtrl{}
	mk := func() (*TaskBoardHandler, *deliverRecorder) {
		host := &deliverRecorder{}
		return NewTaskBoardHandler(host, &fakeBP{role: schema.TeamRoleTeammate, memberName: "alice"},
			&handlerTestInfra{taskMgr: backend.TaskManager(), backend: backend}, poll), host
	}
	for _, ev := range []string{events.TeamEventTaskCreated, events.TeamEventTaskUnblocked, events.TeamEventTaskReleased} {
		h, host := mk()
		h.OnTaskBoardEvent(context.Background(), transportEvent(ev, "bob", "t1", nil))
		assert.True(t, host.deliverCalled, "%s 应唤醒 idle teammate", ev)
	}
	for _, ev := range []string{events.TeamEventTaskUpdated, events.TeamEventTaskCompleted, events.TeamEventTaskStarted} {
		h, host := mk()
		h.OnTaskBoardEvent(context.Background(), transportEvent(ev, "bob", "t1", nil))
		assert.False(t, host.deliverCalled, "%s 不应唤醒 teammate（不扩大可认领池）", ev)
	}
}

// TestOnTaskBoardEvent_teammate空板静默 三类唤醒事件在空板（无可认领任务）时静默。
func TestOnTaskBoardEvent_teammate空板静默(t *testing.T) {
	host2 := &deliverRecorder{}
	// 空板（无可认领任务）时 teammate 静默
	h2, _ := newBoardFixtureWithHost(t, "alice", host2, schema.TeamRoleTeammate)
	h2.OnTaskBoardEvent(context.Background(), transportEvent(events.TeamEventTaskCreated, "bob", "t0", nil))
	assert.False(t, host2.deliverCalled, "空板（无可认领任务）时 teammate 静默")
}

// TestOnTaskBoardEvent_leaderQuiet集 leader 不被 quiet 四事件唤醒；UNBLOCKED 按 owner 分化。
func TestOnTaskBoardEvent_leaderQuiet集(t *testing.T) {
	host := &deliverRecorder{}
	h, _ := newBoardFixtureWithHost(t, "leader", host, schema.TeamRoleLeader)
	for _, ev := range []string{events.TeamEventTaskClaimed, events.TeamEventTaskStarted,
		events.TeamEventTaskSubmittedForReview, events.TeamEventTaskRevisionRequested} {
		h.OnTaskBoardEvent(context.Background(), transportEvent(ev, "alice", "t1", nil))
		assert.False(t, host.deliverCalled, "%s 是 leader quiet 事件", ev)
	}
}

// TestOnTaskBoardEvent_leaderUnblocked按owner分化 UNBLOCKED 无主票唤醒 leader、已指派票跳过。
func TestOnTaskBoardEvent_leaderUnblocked按owner分化(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	require.NoError(t, db.Initialize(context.Background()))
	db.CreateTeam(context.Background(), "t1", "Team", "leader", "", "", "autonomous", false)
	db.CreateMember(context.Background(), "leader", "t1", "L", "{}", "ready", "leader", "", "", "build_mode", "", "")
	db.CreateMember(context.Background(), "alice", "t1", "A", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	backend := tools.NewTeamBackend("t1", "leader", true, db, nil)
	bp := &fakeBP{role: schema.TeamRoleLeader, memberName: "leader"}
	poll := &fakePollCtrl{}
	mk := func() (*TaskBoardHandler, *deliverRecorder) {
		host := &deliverRecorder{}
		return NewTaskBoardHandler(host, bp, &handlerTestInfra{taskMgr: backend.TaskManager(), backend: backend}, poll), host
	}

	owner := "alice"
	if ok, err := db.Task().CreateTask(context.Background(), &database.TeamTaskBase{
		TaskID: "t-open", TeamName: "t1", Title: "T", Content: "C", Status: fsm.TaskStatusPending}); err != nil || !ok {
		t.Fatalf("建任务失败: %v", err)
	}
	h, host := mk()
	h.OnTaskBoardEvent(context.Background(), transportEvent(events.TeamEventTaskUnblocked, "alice", "t-open", nil))
	assert.True(t, host.deliverCalled, "无主票解锁应唤醒 leader（可能需要指派）")

	if ok, err := db.Task().CreateTask(context.Background(), &database.TeamTaskBase{
		TaskID: "t-owned", TeamName: "t1", Title: "T", Content: "C", Status: fsm.TaskStatusPending, Assignee: &owner}); err != nil || !ok {
		t.Fatalf("建任务失败: %v", err)
	}
	h2, host2 := mk()
	h2.OnTaskBoardEvent(context.Background(), transportEvent(events.TeamEventTaskUnblocked, "alice", "t-owned", nil))
	assert.False(t, host2.deliverCalled, "已指派票的解锁是纯进度（F_121）")
}

// TestOnInitialTaskPoll_空板静默 空板上的 teammate 启动普查不投递（F_69 继承静默）。
func TestOnInitialTaskPoll_空板静默(t *testing.T) {
	host := &deliverRecorder{}
	h, _ := newBoardFixtureWithHost(t, "alice", host, schema.TeamRoleTeammate)
	h.OnInitialTaskPoll(context.Background(), types.CoordinationEvent{
		Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeInitialPollTask},
	})
	assert.False(t, host.deliverCalled, "空板启动普查应静默")
}

// TestOnInitialTaskPoll_有活投递 名下 pending 任务经启动普查送达（F_69）。
func TestOnInitialTaskPoll_有活投递(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	require.NoError(t, db.Initialize(context.Background()))
	db.CreateTeam(context.Background(), "t1", "Team", "leader", "", "", "autonomous", false)
	db.CreateMember(context.Background(), "alice", "t1", "A", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	backend := tools.NewTeamBackend("t1", "alice", false, db, nil)
	owner := "alice"
	if ok, err := db.Task().CreateTask(context.Background(), &database.TeamTaskBase{
		TaskID: "t-mine", TeamName: "t1", Title: "指派给我的活", Content: "内容", Status: fsm.TaskStatusPending, Assignee: &owner}); err != nil || !ok {
		t.Fatalf("建任务失败: %v", err)
	}
	host := &deliverRecorder{}
	h := NewTaskBoardHandler(host, &fakeBP{role: schema.TeamRoleTeammate, memberName: "alice"},
		&handlerTestInfra{taskMgr: backend.TaskManager(), backend: backend}, &fakePollCtrl{})
	h.OnInitialTaskPoll(context.Background(), types.CoordinationEvent{
		Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeInitialPollTask},
	})
	assert.True(t, host.deliverCalled, "名下 pending 任务应经启动普查送达")
}
