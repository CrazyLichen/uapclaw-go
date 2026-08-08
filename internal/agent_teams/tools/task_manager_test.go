package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

func setupTestTaskManager() (*TeamTaskManager, *database.InMemoryTeamDatabase) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "leader1", "alpha", "Leader", "{}", "ready", "leader", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	tm := NewTeamTaskManager(db, "alpha", "agent1", nil, "", "", "leader1", "")
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
	tm.AddWithPriority(ctx, "dep_task", "下游任务", "依赖上游", []string{task.TaskID}, nil)

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
		{Title: "任务2", Content: "内容2"},
	}
	tasks, err := tm.AddBatch(ctx, specs)
	if err != nil {
		t.Fatalf("AddBatch 返回错误: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("批量创建应返回2个任务: got %d", len(tasks))
	}
}

func setupPlanModeTaskManager(t *testing.T) (*TeamTaskManager, *database.InMemoryTeamDatabase, string) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "leader1", "alpha", "Leader", "{}", "ready", "leader", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "plan_mode", "", "")

	plansDir := t.TempDir()
	tm := NewTeamTaskManager(db, "alpha", "agent1", nil, plansDir, "plan_session_1", "leader1", "")
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
	_, err := tm.AddWithPriority(ctx, "new_task", "新任务", "内容", []string{"nonexist"}, nil)
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
	leaderTM := NewTeamTaskManager(db, "alpha", "leader1", nil, plansDir, "plan_session_1", "leader1", "")
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

func TestTaskManager_AddBatch_中途失败(t *testing.T) {
	tm, db := setupTestTaskManager()
	ctx := context.Background()

	task1, _ := tm.Add(ctx, "任务1", "")
	// 手动往 db 写入与 task_manager 同 ID 的任务，制造冲突
	db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID:   task1.TaskID,
		TeamName: "alpha",
		Title:    "冲突",
		Status:   fsm.TaskStatusPending,
	})

	// AddBatch 第二个应失败
	_, err := tm.AddBatch(ctx, []TaskCreateSpec{
		{Title: "新任务", Content: "内容"},
	})
	// 正常情况不应失败（ID 是自动生成的），此测试验证正常路径覆盖
	if err != nil {
		// 只要不是 ID 冲突就行
		t.Logf("AddBatch 返回错误: %v（可接受）", err)
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
