package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
)

// newTestSqlDBWithSession 创建带指定 session ID 的测试数据库。
func newTestSqlDBWithSession(t *testing.T, sessionID string) *SqlTeamDatabase {
	t.Helper()
	config := DatabaseConfig{DBType: DatabaseTypeSQLite, ConnectionString: ":memory:"}
	db := NewSqlTeamDatabase(config)
	ctx := newTestCtx(sessionID)
	require.NoError(t, db.Initialize(ctx))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSQLTaskDao_CreateAndGet(t *testing.T) {
	db := newTestSqlDBWithSession(t, "task-test-1")
	ctx := newTestCtx("task-test-1")

	dao := db.Task()
	task := &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "Task 1", Content: "Content", Status: fsm.TaskStatusPending}
	ok, err := dao.CreateTask(ctx, task)
	require.NoError(t, err)
	assert.True(t, ok)

	got, err := dao.GetTask(ctx, "t1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Task 1", got.Title)
}

func TestSQLTaskDao_GetTeamTasks(t *testing.T) {
	db := newTestSqlDBWithSession(t, "task-list-test")
	ctx := newTestCtx("task-list-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusPending})
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "team1", Title: "T2", Status: fsm.TaskStatusClaimed})

	tasks, err := dao.GetTeamTasks(ctx, "team1", "")
	require.NoError(t, err)
	assert.Equal(t, 2, len(tasks))

	claimed, err := dao.GetTeamTasks(ctx, "team1", fsm.TaskStatusClaimed)
	require.NoError(t, err)
	assert.Equal(t, 1, len(claimed))
}

func TestSQLTaskDao_GetTasksByAssignee(t *testing.T) {
	db := newTestSqlDBWithSession(t, "assignee-test")
	ctx := newTestCtx("assignee-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusClaimed, Assignee: "m1"})
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "team1", Title: "T2", Status: fsm.TaskStatusPending, Assignee: "m2"})

	// 查询 m1 的任务
	tasks, err := dao.GetTasksByAssignee(ctx, "team1", "m1", "")
	require.NoError(t, err)
	require.Equal(t, 1, len(tasks))
	assert.Equal(t, "t1", tasks[0].TaskID)

	// 按状态过滤
	claimedTasks, err := dao.GetTasksByAssignee(ctx, "team1", "m1", fsm.TaskStatusClaimed)
	require.NoError(t, err)
	assert.Equal(t, 1, len(claimedTasks))
}

func TestSQLTaskDao_ClaimTask(t *testing.T) {
	db := newTestSqlDBWithSession(t, "claim-test")
	ctx := newTestCtx("claim-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusPending})

	// 对齐 Python: pending → claimed
	ok, err := dao.ClaimTask(ctx, "t1", "member1")
	require.NoError(t, err)
	assert.True(t, ok)

	got, _ := dao.GetTask(ctx, "t1")
	assert.Equal(t, fsm.TaskStatusClaimed, got.Status)
	assert.Equal(t, "member1", got.Assignee)
}

func TestSQLTaskDao_ResetTask(t *testing.T) {
	db := newTestSqlDBWithSession(t, "reset-test")
	ctx := newTestCtx("reset-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusPending})

	// 先认领
	dao.ClaimTask(ctx, "t1", "member1")

	// 对齐 Python: claimed → pending
	ok, err := dao.ResetTask(ctx, "t1")
	require.NoError(t, err)
	assert.True(t, ok)

	got, _ := dao.GetTask(ctx, "t1")
	assert.Equal(t, fsm.TaskStatusPending, got.Status)
	assert.Equal(t, "", got.Assignee)
}

func TestSQLTaskDao_ApprovePlanTask(t *testing.T) {
	db := newTestSqlDBWithSession(t, "approve-test")
	ctx := newTestCtx("approve-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusPending})
	dao.ClaimTask(ctx, "t1", "member1")

	// 对齐 Python: claimed → plan_approved
	ok, err := dao.ApprovePlanTask(ctx, "t1")
	require.NoError(t, err)
	assert.True(t, ok)

	got, _ := dao.GetTask(ctx, "t1")
	assert.Equal(t, fsm.TaskStatusPlanApproved, got.Status)
}

func TestSQLTaskDao_UpdateTaskStatus(t *testing.T) {
	db := newTestSqlDBWithSession(t, "update-status-test")
	ctx := newTestCtx("update-status-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusClaimed})

	// 对齐 Python: claimed → completed
	refreshed, err := dao.UpdateTaskStatus(ctx, "t1", fsm.TaskStatusCompleted)
	require.NoError(t, err)
	_ = refreshed

	got, _ := dao.GetTask(ctx, "t1")
	assert.Equal(t, fsm.TaskStatusCompleted, got.Status)
}

func TestSQLTaskDao_UpdateTask(t *testing.T) {
	db := newTestSqlDBWithSession(t, "update-task-test")
	ctx := newTestCtx("update-task-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Content: "C1", Status: fsm.TaskStatusPending})

	// 对齐 Python: pending 状态下可编辑
	ok, err := dao.UpdateTask(ctx, "t1", "New Title", "New Content")
	require.NoError(t, err)
	assert.True(t, ok)

	got, _ := dao.GetTask(ctx, "t1")
	assert.Equal(t, "New Title", got.Title)
	assert.Equal(t, "New Content", got.Content)

	// 对齐 Python: claimed 状态下禁止编辑
	dao.ClaimTask(ctx, "t1", "m1")
	ok, err = dao.UpdateTask(ctx, "t1", "Should Fail", "")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSQLTaskDao_MutateDependencyGraph_成功(t *testing.T) {
	db := newTestSqlDBWithSession(t, "mutate-test")
	ctx := newTestCtx("mutate-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusPending})
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "team1", Title: "T2", Status: fsm.TaskStatusPending})

	// 对齐 Python: t1 依赖 t2
	result := dao.MutateDependencyGraph(ctx, "team1", nil, []EdgeSpec{{TaskID: "t1", DependsOnID: "t2"}})
	assert.True(t, result.Ok)

	deps, _ := dao.GetTaskDependencies(ctx, "t1")
	assert.Equal(t, 1, len(deps))
}

func TestSQLTaskDao_MutateDependencyGraph_环检测(t *testing.T) {
	db := newTestSqlDBWithSession(t, "cycle-test")
	ctx := newTestCtx("cycle-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "a", TeamName: "team1", Title: "A", Status: fsm.TaskStatusPending})
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "b", TeamName: "team1", Title: "B", Status: fsm.TaskStatusPending})
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "c", TeamName: "team1", Title: "C", Status: fsm.TaskStatusPending})

	dao.MutateDependencyGraph(ctx, "team1", nil, []EdgeSpec{{TaskID: "a", DependsOnID: "b"}})
	dao.MutateDependencyGraph(ctx, "team1", nil, []EdgeSpec{{TaskID: "b", DependsOnID: "c"}})

	// 对齐 Python: a→b, b→c, c→a 构成环
	result := dao.MutateDependencyGraph(ctx, "team1", nil, []EdgeSpec{{TaskID: "c", DependsOnID: "a"}})
	assert.False(t, result.Ok)
	assert.Contains(t, result.Reason, "循环依赖")
}

func TestSQLTaskDao_MutateDependencyGraph_新增任务(t *testing.T) {
	db := newTestSqlDBWithSession(t, "new-tasks-test")
	ctx := newTestCtx("new-tasks-test")

	dao := db.Task()

	// 对齐 Python: mutate_dependency_graph(team_name, new_tasks=[...], add_edges=[...])
	result := dao.MutateDependencyGraph(ctx, "team1",
		[]NewTaskSpec{
			{TaskID: "t1", Title: "T1", Content: "C1", InitialStatus: fsm.TaskStatusPending},
			{TaskID: "t2", Title: "T2", Content: "C2", InitialStatus: fsm.TaskStatusPending},
		},
		[]EdgeSpec{{TaskID: "t1", DependsOnID: "t2"}},
	)
	assert.True(t, result.Ok)

	got, _ := dao.GetTask(ctx, "t1")
	require.NotNil(t, got)
	assert.Equal(t, fsm.TaskStatusBlocked, got.Status) // t1 有未解决依赖 → blocked
}

func TestSQLTaskDao_AddTaskWithBidirectionalDependencies(t *testing.T) {
	db := newTestSqlDBWithSession(t, "bidir-test")
	ctx := newTestCtx("bidir-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusPending})

	// 对齐 Python: 带依赖创建任务
	result := dao.AddTaskWithBidirectionalDependencies(ctx, "team1",
		&TeamTaskBase{TaskID: "t2", TeamName: "team1", Title: "T2", Status: fsm.TaskStatusPending},
		[]string{"t1"},
		nil,
	)
	assert.True(t, result.Ok)

	got, _ := dao.GetTask(ctx, "t2")
	require.NotNil(t, got)
	assert.Equal(t, fsm.TaskStatusBlocked, got.Status) // t2 依赖 t1 → blocked
}

func TestSQLTaskDao_CancelTask_终止传播(t *testing.T) {
	db := newTestSqlDBWithSession(t, "cancel-test")
	ctx := newTestCtx("cancel-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusPending})
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "team1", Title: "T2", Status: fsm.TaskStatusPending})

	// t2 依赖 t1
	dao.MutateDependencyGraph(ctx, "team1", nil, []EdgeSpec{{TaskID: "t2", DependsOnID: "t1"}})

	// 对齐 Python: cancel_task(t1) → t2 解除阻塞
	task, unblocked, err := dao.CancelTask(ctx, "t1")
	require.NoError(t, err)
	require.NotNil(t, task)
	assert.Equal(t, fsm.TaskStatusCancelled, task.Status)
	// 检查 unblocked 中是否包含 t2
	var unblockedIDs []string
	for _, t := range unblocked {
		unblockedIDs = append(unblockedIDs, t.TaskID)
	}
	assert.Contains(t, unblockedIDs, "t2")

	got, _ := dao.GetTask(ctx, "t2")
	require.NotNil(t, got)
	assert.Equal(t, fsm.TaskStatusPending, got.Status) // blocked → pending
}

func TestSQLTaskDao_CompleteTask(t *testing.T) {
	db := newTestSqlDBWithSession(t, "complete-test")
	ctx := newTestCtx("complete-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusClaimed})

	_, unblocked, err := dao.CompleteTask(ctx, "t1")
	require.NoError(t, err)
	_ = unblocked

	got, _ := dao.GetTask(ctx, "t1")
	require.NotNil(t, got)
	assert.Equal(t, fsm.TaskStatusCompleted, got.Status)
}

func TestSQLTaskDao_CancelAllTasks(t *testing.T) {
	db := newTestSqlDBWithSession(t, "cancel-all-test")
	ctx := newTestCtx("cancel-all-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusPending, Assignee: "m1"})
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "team1", Title: "T2", Status: fsm.TaskStatusClaimed, Assignee: "m2"})

	// 对齐 Python: cancel_all_tasks(team_name, skip_assignees=[])
	result, err := dao.CancelAllTasks(ctx, "team1", nil)
	require.NoError(t, err)
	assert.Equal(t, 2, len(result.Cancelled))

	// 验证 t1, t2 已取消
	got1, _ := dao.GetTask(ctx, "t1")
	assert.Equal(t, fsm.TaskStatusCancelled, got1.Status)

	// 对齐 Python: skip_assignees 过滤
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t3", TeamName: "team1", Title: "T3", Status: fsm.TaskStatusPending, Assignee: "m3"})
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t4", TeamName: "team1", Title: "T4", Status: fsm.TaskStatusPending, Assignee: "m4"})
	result2, err := dao.CancelAllTasks(ctx, "team1", []string{"m3"})
	require.NoError(t, err)
	assert.Equal(t, 1, len(result2.Cancelled)) // 只取消 t4，跳过 t3

	got3, _ := dao.GetTask(ctx, "t3")
	assert.Equal(t, fsm.TaskStatusPending, got3.Status) // t3 未被取消
	got4, _ := dao.GetTask(ctx, "t4")
	assert.Equal(t, fsm.TaskStatusCancelled, got4.Status) // t4 被取消
}

func TestSQLTaskDao_DeleteTask(t *testing.T) {
	db := newTestSqlDBWithSession(t, "delete-test")
	ctx := newTestCtx("delete-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusPending})

	err := dao.DeleteTask(ctx, "t1")
	require.NoError(t, err)

	got, _ := dao.GetTask(ctx, "t1")
	assert.Nil(t, got)
}

func TestSQLTaskDao_GetUnresolvedDependenciesCount(t *testing.T) {
	db := newTestSqlDBWithSession(t, "unresolved-test")
	ctx := newTestCtx("unresolved-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusPending})
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "team1", Title: "T2", Status: fsm.TaskStatusPending})
	dao.MutateDependencyGraph(ctx, "team1", nil, []EdgeSpec{{TaskID: "t1", DependsOnID: "t2"}})

	// t1 有 1 个未解决依赖
	count, err := dao.GetUnresolvedDependenciesCount(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestSQLTaskDao_GetTasksDependingOn(t *testing.T) {
	db := newTestSqlDBWithSession(t, "depending-on-test")
	ctx := newTestCtx("depending-on-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusPending})
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "team1", Title: "T2", Status: fsm.TaskStatusPending})
	dao.MutateDependencyGraph(ctx, "team1", nil, []EdgeSpec{{TaskID: "t2", DependsOnID: "t1"}})

	// t1 被谁依赖？→ t2
	tasks, err := dao.GetTasksDependingOn(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, 1, len(tasks))
	assert.Equal(t, "t2", tasks[0].TaskID)
}

func TestSQLTaskDao_VerifyAndFixTaskConsistency(t *testing.T) {
	db := newTestSqlDBWithSession(t, "verify-test")
	ctx := newTestCtx("verify-test")

	dao := db.Task()
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "team1", Title: "T1", Status: fsm.TaskStatusPending})
	dao.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "team1", Title: "T2", Status: fsm.TaskStatusPending})

	// t2 依赖 t1
	dao.MutateDependencyGraph(ctx, "team1", nil, []EdgeSpec{{TaskID: "t2", DependsOnID: "t1"}})

	// 对齐 Python: verify_and_fix_task_consistency
	refreshed, err := dao.VerifyAndFixTaskConsistency(ctx, "team1")
	require.NoError(t, err)
	_ = refreshed
}
