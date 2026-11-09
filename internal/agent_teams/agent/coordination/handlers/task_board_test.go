package handlers

import (
	"context"
	"strings"
	"testing"

	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"

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
