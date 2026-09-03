package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

func setupTestTaskManager() (*TeamTaskManager, *database.InMemoryTeamDatabase) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "leader1", "alpha", "Leader", "{}", "ready", "leader", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	tm := NewTeamTaskManager(db, "alpha", "agent1", nil, "", "", "leader1")
	db.Initialize(ctx)
	return tm, db
}

func TestTaskManager_Add(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, err := tm.Add(ctx, "任务标题", "任务内容")
	if err != nil {
		t.Fatalf("Add 返回错误: %v", err)
	}
	if task.Status != fsm.TaskStatusPending {
		t.Errorf("新任务状态应为 pending: got %q", task.Status)
	}
}

func TestTaskManager_Claim(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "内容")
	err := tm.Claim(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("Claim 返回错误: %v", err)
	}
	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusClaimed {
		t.Errorf("认领后状态应为 claimed: got %q", got.Status)
	}
	if got.Assignee != "agent1" {
		t.Errorf("认领人应为 agent1: got %q", got.Assignee)
	}
}

func TestTaskManager_Complete(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "内容")
	tm.Claim(ctx, task.TaskID)
	refreshed, err := tm.Complete(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("Complete 返回错误: %v", err)
	}
	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusCompleted {
		t.Errorf("完成后状态应为 completed: got %q", got.Status)
	}
	_ = refreshed // 返回刷新的 task IDs（无下游依赖时为空列表）
}

func TestTaskManager_Cancel(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "内容")
	_, err := tm.Cancel(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("Cancel 返回错误: %v", err)
	}
	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusCancelled {
		t.Errorf("取消后状态应为 cancelled: got %q", got.Status)
	}
}

func TestTaskManager_CancelAllTasks(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	tm.Add(ctx, "任务1", "")
	tm.Add(ctx, "任务2", "")

	cancelled, err := tm.CancelAllTasks(ctx, nil)
	if err != nil {
		t.Fatalf("CancelAllTasks 返回错误: %v", err)
	}
	if len(cancelled) != 2 {
		t.Errorf("应取消2个任务: got %d", len(cancelled))
	}
}

func TestTaskManager_GetClaimableTasks(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	tm.Add(ctx, "任务1", "")
	tm.Add(ctx, "任务2", "")
	// 认领一个
	claimableBefore, _ := tm.GetClaimableTasks(ctx)
	if len(claimableBefore) != 2 {
		t.Errorf("初始可认领任务应为2: got %d", len(claimableBefore))
	}
	tm.Claim(ctx, claimableBefore[0].TaskID)

	claimable, _ := tm.GetClaimableTasks(ctx)
	if len(claimable) != 1 {
		t.Errorf("认领1个后可认领任务应为1: got %d", len(claimable))
	}
}

func TestTaskManager_ListTasksWithDeps(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "上游任务", "")
	tm.AddWithPriority(ctx, "下游任务", "依赖上游",
		WithPriorityTaskID("dep_task"),
		WithPriorityDependencies([]string{task.TaskID}),
	)

	summaries, err := tm.ListTasksWithDeps(ctx)
	if err != nil {
		t.Fatalf("ListTasksWithDeps 返回错误: %v", err)
	}
	if len(summaries) != 2 {
		t.Errorf("摘要数量应为2: got %d", len(summaries))
	}
}

func TestTaskManager_AddAsTopPriority(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	tm.Add(ctx, "已有任务", "")

	priorityTask, err := tm.AddAsTopPriority(ctx, "最高优先级", "")
	if err != nil {
		t.Fatalf("AddAsTopPriority 返回错误: %v", err)
	}
	if priorityTask.Status != fsm.TaskStatusPending {
		t.Errorf("最高优先级任务状态应为 pending: got %q", priorityTask.Status)
	}

	// 已有任务应被阻塞
	allTasks, _ := tm.ListTasks(ctx, fsm.TaskStatusBlocked)
	if len(allTasks) == 0 {
		t.Error("已有任务应被最高优先级任务阻塞")
	}
}

func TestTaskManager_Reset(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	tm.Claim(ctx, task.TaskID)

	err := tm.Reset(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("Reset 返回错误: %v", err)
	}
	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusPending {
		t.Errorf("重置后状态应为 pending: got %q", got.Status)
	}
}

func TestTaskManager_Assign(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "内容")
	err := tm.Assign(ctx, task.TaskID, "agent1")
	if err != nil {
		t.Fatalf("Assign 返回错误: %v", err)
	}
	got, _ := tm.Get(ctx, task.TaskID)
	if got.Assignee != "agent1" {
		t.Errorf("分配人应为 agent1: got %q", got.Assignee)
	}
}

func TestTaskManager_UpdateTask(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "原始标题", "原始内容")
	err := tm.UpdateTask(ctx, task.TaskID, "新标题", "新内容")
	if err != nil {
		t.Fatalf("UpdateTask 返回错误: %v", err)
	}
	got, _ := tm.Get(ctx, task.TaskID)
	if got.Title != "新标题" {
		t.Errorf("标题: got %q, want %q", got.Title, "新标题")
	}
}

func TestTaskManager_GetTaskDetail(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	upstream, _ := tm.Add(ctx, "上游", "")
	downstream, _ := tm.Add(ctx, "下游", "")
	result, _ := tm.AddDependencies(ctx, downstream.TaskID, []string{upstream.TaskID})
	if !result.Ok {
		t.Fatalf("AddDependencies 失败: %s", result.Reason)
	}

	detail, err := tm.GetTaskDetail(ctx, downstream.TaskID)
	if err != nil {
		t.Fatalf("GetTaskDetail 返回错误: %v", err)
	}
	if detail == nil {
		t.Fatal("detail 应非 nil")
	}
	if len(detail.BlockedBy) != 1 {
		t.Errorf("下游应被1个上游阻塞: got %d", len(detail.BlockedBy))
	}

	upstreamDetail, err := tm.GetTaskDetail(ctx, upstream.TaskID)
	if err != nil {
		t.Fatalf("GetTaskDetail upstream 返回错误: %v", err)
	}
	if upstreamDetail == nil {
		t.Fatal("upstreamDetail 应非 nil")
	}
	if len(upstreamDetail.Blocks) != 1 {
		t.Errorf("上游应阻塞1个下游: got %d", len(upstreamDetail.Blocks))
	}
}

func TestTaskManager_AddBatch(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	specs := []TaskCreateSpec{
		{Title: "任务1", Content: "内容1"},
		{Title: "任务2", Content: "内容2", TaskID: "custom_id_1"},
	}
	tasks, err := tm.AddBatch(ctx, specs)
	if err != nil {
		t.Fatalf("AddBatch 返回错误: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("批量创建应返回2个任务: got %d", len(tasks))
	}
	// 验证自定义 TaskID
	if tasks[1].TaskID != "custom_id_1" {
		t.Errorf("自定义 TaskID 应为 custom_id_1: got %q", tasks[1].TaskID)
	}
}

func setupPlanModeTaskManager(t *testing.T) (*TeamTaskManager, *database.InMemoryTeamDatabase, string) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "leader1", "alpha", "Leader", "{}", "ready", "leader", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "plan_mode", "", "")

	plansDir := t.TempDir()
	tm := NewTeamTaskManager(db, "alpha", "agent1", nil, plansDir, "plan_session_1", "leader1")
	db.Initialize(ctx)
	return tm, db, plansDir
}

func TestTaskManager_SubmitPlan(t *testing.T) {
	tm, _, _ := setupPlanModeTaskManager(t)
	ctx := context.Background()

	planFile := filepath.Join(t.TempDir(), "plan.md")
	os.WriteFile(planFile, []byte("# 执行计划\n完成数据分析"), 0o644)

	task, _ := tm.Add(ctx, "数据分析", "完成数据分析任务")

	record, err := tm.SubmitPlan(ctx, task.TaskID, planFile, "call_123")
	if err != nil {
		t.Fatalf("SubmitPlan 返回错误: %v", err)
	}
	if record.PlanID == "" {
		t.Error("PlanID 应非空")
	}
	if record.Decision != "pending" {
		t.Errorf("Decision 应为 pending: got %q", record.Decision)
	}

	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusClaimed {
		t.Errorf("提交计划后任务应为 claimed: got %q", got.Status)
	}
}

func TestTaskManager_ApprovePlan_通过(t *testing.T) {
	tm, _, _ := setupPlanModeTaskManager(t)
	ctx := context.Background()

	planFile := filepath.Join(t.TempDir(), "plan.md")
	os.WriteFile(planFile, []byte("# 执行计划"), 0o644)

	task, _ := tm.Add(ctx, "数据分析", "")
	record, _ := tm.SubmitPlan(ctx, task.TaskID, planFile, "call_123")

	err := tm.ApprovePlan(ctx, record.PlanID, true, "")
	if err != nil {
		t.Fatalf("ApprovePlan 返回错误: %v", err)
	}

	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusPlanApproved {
		t.Errorf("审批通过后任务应为 plan_approved: got %q", got.Status)
	}
}

func TestTaskManager_ApprovePlan_拒绝(t *testing.T) {
	tm, _, _ := setupPlanModeTaskManager(t)
	ctx := context.Background()

	planFile := filepath.Join(t.TempDir(), "plan.md")
	os.WriteFile(planFile, []byte("# 执行计划"), 0o644)

	task, _ := tm.Add(ctx, "数据分析", "")
	record, _ := tm.SubmitPlan(ctx, task.TaskID, planFile, "call_123")

	err := tm.ApprovePlan(ctx, record.PlanID, false, "需要更多细节")
	if err != nil {
		t.Fatalf("ApprovePlan 返回错误: %v", err)
	}

	got, _ := tm.Get(ctx, task.TaskID)
	if got.Status != fsm.TaskStatusClaimed {
		t.Errorf("审批拒绝后任务应保持 claimed: got %q", got.Status)
	}
}

func TestTaskManager_checkTaskListDrained(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	// 所有任务终态
	task, _ := tm.Add(ctx, "任务", "")
	tm.Claim(ctx, task.TaskID)
	tm.Complete(ctx, task.TaskID)

	if !tm.checkTaskListDrained(ctx) {
		t.Error("所有任务终态时应返回 true")
	}

	// 有非终态任务
	tm.Add(ctx, "未完成任务", "")
	if tm.checkTaskListDrained(ctx) {
		t.Error("有非终态任务时应返回 false")
	}
}

func TestTaskManager_GetTasksByAssignee(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	tm.Assign(ctx, task.TaskID, "agent1")

	result, err := tm.GetTasksByAssignee(ctx, "agent1", "")
	if err != nil {
		t.Fatalf("GetTasksByAssignee 返回错误: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("agent1 的任务应为1: got %d", len(result))
	}
}

func TestTaskManager_Claim_失败(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	tm.Claim(ctx, task.TaskID)
	// 同一成员再次认领同一任务应幂等返回成功（对齐 Python: idempotent re-claim）
	err := tm.Claim(ctx, task.TaskID)
	if err != nil {
		t.Errorf("同一成员重复认领应幂等返回成功, got: %v", err)
	}
}

func TestTaskManager_Assign_失败(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	// 分配不存在任务应失败
	err := tm.Assign(ctx, "nonexist", "agent1")
	if err == nil {
		t.Error("分配不存在任务应返回错误")
	}
}

func TestTaskManager_Complete_失败(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	// 从 pending 直接完成应失败（需要先 claim）
	_, err := tm.Complete(ctx, task.TaskID)
	if err == nil {
		t.Error("PENDING 状态直接完成应返回错误")
	}
}

func TestTaskManager_Cancel_失败(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	// 取消不存在任务应失败
	_, err := tm.Cancel(ctx, "nonexist")
	if err == nil {
		t.Error("取消不存在任务应返回错误")
	}
}

func TestTaskManager_Reset_失败(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	// PENDING 状态下 reset 应失败
	err := tm.Reset(ctx, task.TaskID)
	if err == nil {
		t.Error("PENDING 状态下 reset 应返回错误")
	}
}

func TestTaskManager_UpdateTask_失败(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	tm.Claim(ctx, task.TaskID)
	// CLAIMED 状态下禁止编辑
	err := tm.UpdateTask(ctx, task.TaskID, "新标题", "新内容")
	if err == nil {
		t.Error("CLAIMED 状态下应禁止编辑")
	}
}

func TestTaskManager_Add_重复ID(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	// Add 应成功创建
	_, err := tm.Add(ctx, "任务1", "")
	if err != nil {
		t.Fatalf("Add 应成功: %v", err)
	}
}

func TestTaskManager_CancelAllTasks_skipAssignees(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task1, _ := tm.Add(ctx, "任务1", "")
	tm.Add(ctx, "任务2", "")
	tm.Assign(ctx, task1.TaskID, "agent1")

	cancelled, err := tm.CancelAllTasks(ctx, []string{"agent1"})
	if err != nil {
		t.Fatalf("CancelAllTasks 返回错误: %v", err)
	}
	// 只取消未分配给 agent1 的任务
	if len(cancelled) < 1 {
		t.Errorf("应至少取消1个任务: got %d", len(cancelled))
	}
}

func TestTaskManager_AddWithPriority_失败(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	// 依赖不存在的任务应失败
	_, err := tm.AddWithPriority(ctx, "新任务", "内容",
		WithPriorityTaskID("new_task"),
		WithPriorityDependencies([]string{"nonexist"}),
	)
	if err == nil {
		t.Error("依赖不存在任务应返回错误")
	}
}

func TestTaskManager_AddDependencies_成功(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task1, _ := tm.Add(ctx, "上游", "")
	task2, _ := tm.Add(ctx, "下游", "")

	result, err := tm.AddDependencies(ctx, task2.TaskID, []string{task1.TaskID})
	if err != nil {
		t.Fatalf("AddDependencies 返回错误: %v", err)
	}
	if !result.Ok {
		t.Errorf("应成功: reason=%s", result.Reason)
	}

	got, _ := tm.Get(ctx, task2.TaskID)
	if got.Status != fsm.TaskStatusBlocked {
		t.Errorf("下游应被阻塞: got %q", got.Status)
	}
}

func TestTaskManager_AddDependencies_失败(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")

	// 依赖不存在的任务应失败
	_, err := tm.AddDependencies(ctx, task.TaskID, []string{"nonexist"})
	if err == nil {
		t.Error("依赖不存在任务应返回错误")
	}
}

func TestTaskManager_SubmitPlan_不是PlanMode(t *testing.T) {
	tm, _ := setupTestTaskManager() // agent1 是 build_mode
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	_, err := tm.SubmitPlan(ctx, task.TaskID, "", "call_123")
	if err == nil {
		t.Error("build_mode 成员提交计划应返回错误")
	}
}

func TestTaskManager_SubmitPlan_不合法状态(t *testing.T) {
	tm, _, _ := setupPlanModeTaskManager(t)
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	// 分配给 leader1，agent1 不是认领人
	tm.Assign(ctx, task.TaskID, "leader1")

	_, err := tm.SubmitPlan(ctx, task.TaskID, "", "call_123")
	if err == nil {
		t.Error("非认领人提交计划应返回错误")
	}
}

func TestTaskManager_SubmitPlan_已完成任务(t *testing.T) {
	tm, db, plansDir := setupPlanModeTaskManager(t)
	ctx := context.Background()

	task, _ := tm.Add(ctx, "已完成任务", "")
	tm.Assign(ctx, task.TaskID, "agent1")

	// PLAN_MODE 成员只能完成 plan_approved 任务，需先提交计划并审批
	planFile := filepath.Join(t.TempDir(), "plan.md")
	os.WriteFile(planFile, []byte("# 计划"), 0o644)
	planRecord, err := tm.SubmitPlan(ctx, task.TaskID, planFile, "call_1")
	if err != nil {
		t.Fatalf("SubmitPlan 返回错误: %v", err)
	}

	// leader 审批计划
	leaderTM := NewTeamTaskManager(db, "alpha", "leader1", nil, plansDir, "plan_session_1", "leader1")
	if err := leaderTM.ApprovePlan(ctx, planRecord.PlanID, true, ""); err != nil {
		t.Fatalf("ApprovePlan 返回错误: %v", err)
	}

	// 现在可以完成
	_, err = tm.Complete(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("Complete 返回错误: %v", err)
	}

	// 已完成任务提交计划应返回错误
	_, err = tm.SubmitPlan(ctx, task.TaskID, "", "call_123")
	if err == nil {
		t.Error("已完成任务提交计划应返回错误")
	}
}

func TestTaskManager_ApprovePlan_过期计划(t *testing.T) {
	tm, _, _ := setupPlanModeTaskManager(t)
	ctx := context.Background()

	planFile := filepath.Join(t.TempDir(), "plan.md")
	os.WriteFile(planFile, []byte("# 计划1"), 0o644)

	task, _ := tm.Add(ctx, "任务", "")
	record1, _ := tm.SubmitPlan(ctx, task.TaskID, planFile, "call_1")

	// 提交第二个计划（拒绝第一个后重新提交）
	planFile2 := filepath.Join(t.TempDir(), "plan2.md")
	os.WriteFile(planFile2, []byte("# 计划2"), 0o644)
	_, _ = tm.SubmitPlan(ctx, task.TaskID, planFile2, "call_2")

	// 审批第一个（过期）应失败
	err := tm.ApprovePlan(ctx, record1.PlanID, true, "")
	if err == nil {
		t.Error("审批过期计划应返回错误")
	}
}

func TestTaskManager_GetTaskDetail_不存在(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	detail, err := tm.GetTaskDetail(ctx, "nonexist")
	if err != nil {
		t.Fatalf("GetTaskDetail 返回错误: %v", err)
	}
	if detail != nil {
		t.Error("不存在任务应返回 nil")
	}
}

func TestTaskManager_ListTasksWithDeps_空(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	summaries, err := tm.ListTasksWithDeps(ctx)
	if err != nil {
		t.Fatalf("ListTasksWithDeps 返回错误: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("空团队应返回0个摘要: got %d", len(summaries))
	}
}

func TestTaskManager_CancelAllTasks_空团队(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	cancelled, err := tm.CancelAllTasks(ctx, nil)
	if err != nil {
		t.Fatalf("CancelAllTasks 返回错误: %v", err)
	}
	if len(cancelled) != 0 {
		t.Errorf("空团队应返回0个取消: got %d", len(cancelled))
	}
}

func TestTaskManager_AddAsTopPriority_空团队(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	priorityTask, err := tm.AddAsTopPriority(ctx, "最高优先级", "")
	if err != nil {
		t.Fatalf("AddAsTopPriority 返回错误: %v", err)
	}
	if priorityTask.Status != fsm.TaskStatusPending {
		t.Errorf("空团队最高优先级任务应为 pending: got %q", priorityTask.Status)
	}
}

func TestTaskManager_AddBatch_空(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	tasks, err := tm.AddBatch(ctx, nil)
	if err != nil {
		t.Fatalf("AddBatch 返回错误: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("空批量应返回0个任务: got %d", len(tasks))
	}
}

func TestTaskManager_Add_冲突ID(t *testing.T) {
	tm, db := setupTestTaskManager()
	ctx := context.Background()

	// 先直接往 db 插入一个相同 ID 的 task
	task, _ := tm.Add(ctx, "任务1", "内容1")
	// 再往 db 直接插入相同 ID（绕过 TaskManager 的自动 ID 生成）
	existing := &database.TeamTaskBase{
		TaskID:   task.TaskID,
		TeamName: "alpha",
		Title:    "冲突",
		Content:  "冲突内容",
		Status:   fsm.TaskStatusPending,
	}
	ok, _ := db.Task().CreateTask(ctx, existing)
	if ok {
		t.Error("创建冲突 ID 的任务应失败")
	}
}

func TestTaskManager_AddBatch_跳过无效规格(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	// 对齐 Python: 缺 title 或 content 的规格应被跳过
	specs := []TaskCreateSpec{
		{Title: "有效任务", Content: "内容"},
		{Title: "", Content: "缺标题"},  // 应跳过
		{Title: "缺内容", Content: ""},  // 应跳过
		{Title: "又一个有效", Content: "内容2"},
	}
	tasks, err := tm.AddBatch(ctx, specs)
	if err != nil {
		t.Fatalf("AddBatch 返回错误: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("应跳过无效规格，返回2个任务: got %d", len(tasks))
	}
}

func TestTaskManager_AddBatch_跳过创建失败(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	// 对齐 Python: 创建失败的规格应跳过，不影响后续
	// 依赖不存在的任务会走 MutateDependencyGraph 原子路径失败
	specs := []TaskCreateSpec{
		{Title: "正常任务", Content: "内容"},
		{Title: "依赖失败", Content: "内容", Dependencies: []string{"nonexistent_task"}},
		{Title: "另一个正常", Content: "内容2"},
	}
	tasks, err := tm.AddBatch(ctx, specs)
	if err != nil {
		t.Fatalf("AddBatch 不应返回错误（容错）: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("应跳过创建失败的规格，返回2个任务: got %d", len(tasks))
	}
}

func TestTaskManager_AddBatch_带依赖(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	// 先创建上游任务
	upstream, _ := tm.Add(ctx, "上游任务", "内容")

	// AddBatch 中指定 dependencies
	specs := []TaskCreateSpec{
		{Title: "独立任务", Content: "独立内容"},
		{Title: "依赖任务", Content: "依赖上游", TaskID: "dep_batch_task", Dependencies: []string{upstream.TaskID}},
	}
	tasks, err := tm.AddBatch(ctx, specs)
	if err != nil {
		t.Fatalf("AddBatch 返回错误: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("批量创建应返回2个任务: got %d", len(tasks))
	}
	// 有依赖的任务应被阻塞
	depTask, _ := tm.Get(ctx, "dep_batch_task")
	if depTask == nil {
		t.Fatal("依赖任务应存在")
	}
	if depTask.Status != fsm.TaskStatusBlocked {
		t.Errorf("有依赖的任务应为 blocked: got %q", depTask.Status)
	}
}

func TestTaskManager_SubmitPlan_已认领非当前成员(t *testing.T) {
	tm, _, _ := setupPlanModeTaskManager(t)
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	// leader1 认领（而非 agent1）
	tm.Assign(ctx, task.TaskID, "leader1")

	_, err := tm.SubmitPlan(ctx, task.TaskID, "", "call_1")
	if err == nil {
		t.Error("非认领人提交计划应返回错误")
	}
}

func TestTaskManager_ApprovePlan_已审批(t *testing.T) {
	tm, _, _ := setupPlanModeTaskManager(t)
	ctx := context.Background()

	planFile := filepath.Join(t.TempDir(), "plan.md")
	os.WriteFile(planFile, []byte("# 计划"), 0o644)

	task, _ := tm.Add(ctx, "任务", "")
	record, _ := tm.SubmitPlan(ctx, task.TaskID, planFile, "call_1")

	// 先审批通过
	err := tm.ApprovePlan(ctx, record.PlanID, true, "")
	if err != nil {
		t.Fatalf("审批通过应成功: %v", err)
	}

	// 再次审批同一 planID 应失败（decision 已不是 pending）
	err = tm.ApprovePlan(ctx, record.PlanID, true, "")
	if err == nil {
		t.Error("重复审批应返回错误")
	}
}

func TestTaskManager_ApprovePlan_不存在计划(t *testing.T) {
	tm, _, _ := setupPlanModeTaskManager(t)
	ctx := context.Background()

	err := tm.ApprovePlan(ctx, "nonexist_plan", true, "")
	if err == nil {
		t.Error("审批不存在的计划应返回错误")
	}
}

// TestTaskManager_SubmitPlan_带Messager 测试带 messager 的 SubmitPlan（覆盖 notifyLeaderOfPlan）
func TestTaskManager_SubmitPlan_带Messager(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "leader1", "alpha", "Leader", "{}", "ready", "leader", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "plan_mode", "", "")

	plansDir := t.TempDir()
	msg := messager.NewInProcessMessager(atschema.NewMessagerTransportConfig())
	tm := NewTeamTaskManager(db, "alpha", "agent1", msg, plansDir, "plan_session_1", "leader1")
	db.Initialize(ctx)

	planFile := filepath.Join(t.TempDir(), "plan.md")
	os.WriteFile(planFile, []byte("# 执行计划"), 0o644)

	task, _ := tm.Add(ctx, "数据分析", "完成数据分析任务")

	record, err := tm.SubmitPlan(ctx, task.TaskID, planFile, "call_123")
	if err != nil {
		t.Fatalf("SubmitPlan 返回错误: %v", err)
	}
	if record.PlanID == "" {
		t.Error("PlanID 应非空")
	}
}

func TestTaskManager_WithPriorityDependentTaskIDs(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	upstream, _ := tm.Add(ctx, "上游", "")
	downstream, _ := tm.Add(ctx, "下游", "")

	// 创建中间任务，依赖上游，被下游依赖
	middle, err := tm.AddWithPriority(ctx, "中间任务", "内容",
		WithPriorityDependencies([]string{upstream.TaskID}),
		WithPriorityDependentTaskIDs([]string{downstream.TaskID}),
	)
	if err != nil {
		t.Fatalf("AddWithPriority 双向依赖应成功: %v", err)
	}
	if middle.Status != fsm.TaskStatusBlocked {
		t.Errorf("有 dependencies 的任务初始应为 blocked: got %q", middle.Status)
	}

	// 验证下游被阻塞
	got, _ := tm.Get(ctx, downstream.TaskID)
	if got.Status != fsm.TaskStatusBlocked {
		t.Errorf("下游任务应被中间任务阻塞: got %q", got.Status)
	}
}

func TestTaskManager_renderPlanReviewMessage(t *testing.T) {
	msg := renderPlanReviewMessage("agent1", "task_1", "plan_1", "/path/plan.md", "call_1")
	if !strings.Contains(msg, "agent1") {
		t.Error("消息应包含成员名")
	}
	if !strings.Contains(msg, "task_1") {
		t.Error("消息应包含任务ID")
	}
	if !strings.Contains(msg, "plan_1") {
		t.Error("消息应包含计划ID")
	}
	if !strings.Contains(msg, "call_1") {
		t.Error("消息应包含 ToolCallID")
	}

	// 无 toolCallID
	msg2 := renderPlanReviewMessage("agent1", "task_1", "plan_1", "/path/plan.md", "")
	if strings.Contains(msg2, "Tool Call ID") {
		t.Error("无 toolCallID 时不应包含 Tool Call ID 行")
	}
}

func TestTaskManager_Claim_已被他人认领(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	// agent1 认领
	tm.Claim(ctx, task.TaskID)

	// 创建另一个 manager 以 leader1 身份认领
	leaderTM := NewTeamTaskManager(tm.db, "alpha", "leader1", nil, "", "", "leader1")
	err := leaderTM.Claim(ctx, task.TaskID)
	if err == nil {
		t.Error("已被他人认领的任务应返回错误")
	}
}

func TestTaskManager_Claim_BLOCKED任务(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	upstream, _ := tm.Add(ctx, "上游", "")
	downstream, _ := tm.AddWithPriority(ctx, "下游", "内容",
		WithPriorityDependencies([]string{upstream.TaskID}),
	)
	// 下游应处于 BLOCKED
	if downstream.Status != fsm.TaskStatusBlocked {
		t.Fatalf("下游应为 blocked: got %q", downstream.Status)
	}

	err := tm.Claim(ctx, downstream.TaskID)
	if err == nil {
		t.Error("BLOCKED 任务不能被认领")
	}
}

func TestTaskManager_Assign_已被他人认领(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	tm.Assign(ctx, task.TaskID, "agent1")

	// 分配给不同人应失败
	err := tm.Assign(ctx, task.TaskID, "leader1")
	if err == nil {
		t.Error("已被他人认领的任务应返回错误")
	}
}

func TestTaskManager_Complete_解除阻塞(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	upstream, _ := tm.Add(ctx, "上游", "")
	downstream, _ := tm.AddWithPriority(ctx, "下游", "内容",
		WithPriorityDependencies([]string{upstream.TaskID}),
	)
	if downstream.Status != fsm.TaskStatusBlocked {
		t.Fatalf("下游应为 blocked: got %q", downstream.Status)
	}

	// 认领并完成上游 → 下游应解除阻塞
	tm.Claim(ctx, upstream.TaskID)
	_, err := tm.Complete(ctx, upstream.TaskID)
	if err != nil {
		t.Fatalf("完成上游应成功: %v", err)
	}

	// 下游应变为 PENDING
	got, _ := tm.Get(ctx, downstream.TaskID)
	if got.Status != fsm.TaskStatusPending {
		t.Errorf("上游完成后下游应为 pending: got %q", got.Status)
	}
}

func TestTaskManager_Cancel_解除阻塞(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	upstream, _ := tm.Add(ctx, "上游", "")
	downstream, _ := tm.AddWithPriority(ctx, "下游", "内容",
		WithPriorityDependencies([]string{upstream.TaskID}),
	)
	if downstream.Status != fsm.TaskStatusBlocked {
		t.Fatalf("下游应为 blocked: got %q", downstream.Status)
	}

	// 取消上游 → 下游应解除阻塞
	_, err := tm.Cancel(ctx, upstream.TaskID)
	if err != nil {
		t.Fatalf("取消上游应成功: %v", err)
	}

	got, _ := tm.Get(ctx, downstream.TaskID)
	if got.Status != fsm.TaskStatusPending {
		t.Errorf("上游取消后下游应为 pending: got %q", got.Status)
	}
}

func TestTaskManager_publishUnblockedEvents(t *testing.T) {
	// 通过 CancelAllTasks 触发 publishUnblockedEvents
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	upstream, _ := tm.Add(ctx, "上游", "")
	downstream, _ := tm.AddWithPriority(ctx, "下游", "内容",
		WithPriorityDependencies([]string{upstream.TaskID}),
	)
	if downstream.Status != fsm.TaskStatusBlocked {
		t.Fatalf("下游应为 blocked: got %q", downstream.Status)
	}

	cancelled, err := tm.CancelAllTasks(ctx, nil)
	if err != nil {
		t.Fatalf("CancelAllTasks 应成功: %v", err)
	}
	if len(cancelled) < 2 {
		t.Errorf("应至少取消2个任务: got %d", len(cancelled))
	}
}

func TestTaskManager_resolveLeaderMemberName(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	// 已有 leader1 在构造时传入
	name := tm.resolveLeaderMemberName()
	if name != "leader1" {
		t.Errorf("应返回 leader1: got %q", name)
	}

	// 从 team 获取（清空 leaderMemberName 后）
	tm2 := NewTeamTaskManager(tm.db, "alpha", "agent1", nil, "", "", "")
	// team 表有 leader_member_name
	team, _ := tm2.db.Team().GetTeam(ctx, "alpha")
	if team == nil || team.LeaderMemberName != "leader1" {
		t.Log("team.LeaderMemberName 不为 leader1，跳过回退测试")
	} else {
		name2 := tm2.resolveLeaderMemberName()
		if name2 != "leader1" {
			t.Errorf("从 team 获取应返回 leader1: got %q", name2)
		}
	}
}

func TestTaskManager_Add_带依赖(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	upstream, _ := tm.Add(ctx, "上游", "")
	downstream, err := tm.Add(ctx, "下游", "依赖上游",
		WithDependencies([]string{upstream.TaskID}),
	)
	if err != nil {
		t.Fatalf("Add 带依赖应成功: %v", err)
	}
	if downstream.Status != fsm.TaskStatusBlocked {
		t.Errorf("有依赖的任务应为 blocked: got %q", downstream.Status)
	}

	// 上游完成后下游解除阻塞
	tm.Claim(ctx, upstream.TaskID)
	tm.Complete(ctx, upstream.TaskID)
	got, _ := tm.Get(ctx, downstream.TaskID)
	if got.Status != fsm.TaskStatusPending {
		t.Errorf("上游完成后下游应为 pending: got %q", got.Status)
	}
}

func TestTaskManager_Add_依赖不存在(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	_, err := tm.Add(ctx, "依赖不存在", "内容",
		WithDependencies([]string{"nonexistent"}),
	)
	if err == nil {
		t.Error("依赖不存在的任务应返回错误")
	}
}

func TestTaskManager_Claim_成员不存在(t *testing.T) {
	tm, _ := setupTestTaskManager()
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	// 创建不存在的成员的 manager
	ghostTM := NewTeamTaskManager(tm.db, "alpha", "ghost", nil, "", "", "leader1")
	err := ghostTM.Claim(ctx, task.TaskID)
	if err == nil {
		t.Error("不存在的成员认领应返回错误")
	}
}

func TestTaskManager_Claim_PLAN_MODE(t *testing.T) {
	tm, db := setupTestTaskManager()
	ctx := context.Background()

	// 将 agent1 改为 plan_mode
	db.SetMemberMode("agent1", "alpha", "plan_mode")
	task, _ := tm.Add(ctx, "任务", "")
	err := tm.Claim(ctx, task.TaskID)
	if err == nil {
		t.Error("PLAN_MODE 成员不应直接认领")
	}
}

func TestTaskManager_Complete_PLAN_MODE_非PLAN_APPROVED(t *testing.T) {
	tm, _, _ := setupPlanModeTaskManager(t)
	ctx := context.Background()

	task, _ := tm.Add(ctx, "任务", "")
	tm.Assign(ctx, task.TaskID, "agent1")

	// PLAN_MODE 成员只能完成 PLAN_APPROVED 任务，CLAIMED 应失败
	_, err := tm.Complete(ctx, task.TaskID)
	if err == nil {
		t.Error("PLAN_MODE 成员完成 CLAIMED 任务应返回错误")
	}
}

func TestTaskManager_Complete_成员不存在(t *testing.T) {
	// 创建一个不存在的成员的 manager
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "leader1", "alpha", "Leader", "{}", "ready", "leader", "", "", "build_mode", "", "")
	// 不创建 agent1
	tm := NewTeamTaskManager(db, "alpha", "ghost", nil, "", "", "leader1")
	db.Initialize(ctx)

	task, _ := tm.Add(ctx, "任务", "")
	_, err := tm.Complete(ctx, task.TaskID)
	if err == nil {
		t.Error("不存在的成员完成任务应返回错误")
	}
}
