package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskCreateSpec 批量创建任务的输入规范。
type TaskCreateSpec struct {
	Title   string
	Content string
}

// TaskDetail 任务详细视图（含阻塞关系）。
type TaskDetail struct {
	Task      *database.TeamTaskBase
	BlockedBy []*database.TeamTaskBase
	Blocks    []*database.TeamTaskBase
}

// TaskSummary 任务摘要视图（含阻塞信息）。
type TaskSummary struct {
	TaskID    string
	Title     string
	Status    string
	Assignee  string
	BlockedBy []string
}

// PlanRecord 计划记录（index.json 中的一条）。
// 对齐 Python: PlanRecord (openjiuwen/agent_teams/tools/task_manager.py)
type PlanRecord struct {
	PlanID     string `json:"plan_id"`
	TaskID     string `json:"task_id"`
	MemberName string `json:"member_name"`
	Status     string `json:"status"`
	Decision   string `json:"decision"`
	Feedback   string `json:"feedback,omitempty"`
	CreatedAt  int64  `json:"created_at"`
}

// PlanIndex 计划索引（index.json 结构）。
type PlanIndex struct {
	Tasks     map[string]*TaskPlanIndex `json:"tasks"`
	TaskPlans map[string]*PlanRecord    `json:"task_plans"`
}

// TaskPlanIndex 每任务的计划索引。
type TaskPlanIndex struct {
	PlanIDs      []string `json:"plan_ids"`
	LatestPlanID string   `json:"latest_plan_id"`
	Status       string   `json:"status"`
}

// TeamTaskManager 团队任务管理器。
// 对齐 Python: TeamTaskManager (openjiuwen/agent_teams/tools/task_manager.py)
type TeamTaskManager struct {
	// db 团队数据库实例（内含 TaskDao）
	db database.TeamDatabase
	// teamName 团队标识
	teamName string
	// memberName 当前成员标识
	memberName string
	// messager 事件发布器
	messager messager.Messager
	// sessionID 会话标识（用于构建 topic）
	sessionID string
	// plansDir 计划文件存储目录
	plansDir string
	// teamPlanID 团队级计划标识
	teamPlanID string
	// leaderMemberName Leader 成员名（用于通知计划审批）
	leaderMemberName string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamTaskManager 创建任务管理器。
func NewTeamTaskManager(db database.TeamDatabase, teamName, memberName string, messager messager.Messager, plansDir, teamPlanID, leaderMemberName string, sessionID string) *TeamTaskManager {
	return &TeamTaskManager{
		db:               db,
		teamName:         teamName,
		memberName:       memberName,
		messager:         messager,
		plansDir:         plansDir,
		teamPlanID:       teamPlanID,
		leaderMemberName: leaderMemberName,
		sessionID:        sessionID,
	}
}

// Add 创建单条任务。对齐 Python: TeamTaskManager.add()
func (tm *TeamTaskManager) Add(ctx context.Context, title, content string) (*database.TeamTaskBase, error) {
	taskID := fmt.Sprintf("task_%s_%d_%d", tm.teamName, time.Now().UnixMilli(), time.Now().UnixNano()%1000)
	task := &database.TeamTaskBase{
		TaskID:   taskID,
		TeamName: tm.teamName,
		Title:    title,
		Content:  content,
		Status:   fsm.TaskStatusPending,
	}
	ok, err := tm.db.Task().CreateTask(ctx, task)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("创建任务失败: task_id 冲突 %s", taskID)
	}
	tm.publishTaskEvent(ctx, eventTaskCreated, map[string]any{
		"team_name": tm.teamName, "task_id": task.TaskID, "status": task.Status,
	})
	return task, nil
}

// AddBatch 批量创建任务。对齐 Python: TeamTaskManager.add_batch()
func (tm *TeamTaskManager) AddBatch(ctx context.Context, specs []TaskCreateSpec) ([]*database.TeamTaskBase, error) {
	var tasks []*database.TeamTaskBase
	for _, spec := range specs {
		task, err := tm.Add(ctx, spec.Title, spec.Content)
		if err != nil {
			return tasks, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// Get 按 ID 查任务。对齐 Python: TeamTaskManager.get()
func (tm *TeamTaskManager) Get(ctx context.Context, taskID string) (*database.TeamTaskBase, error) {
	return tm.db.Task().GetTask(ctx, taskID)
}

// ListTasks 列出团队任务。对齐 Python: TeamTaskManager.list_tasks()
func (tm *TeamTaskManager) ListTasks(ctx context.Context, status string) ([]*database.TeamTaskBase, error) {
	return tm.db.Task().GetTeamTasks(ctx, tm.teamName, status)
}

// GetClaimableTasks 获取可认领任务（PENDING + 无未解决依赖）。
// 对齐 Python: TeamTaskManager.get_claimable_tasks()
func (tm *TeamTaskManager) GetClaimableTasks(ctx context.Context) ([]*database.TeamTaskBase, error) {
	pendingTasks, err := tm.db.Task().GetTeamTasks(ctx, tm.teamName, fsm.TaskStatusPending)
	if err != nil {
		return nil, err
	}
	var result []*database.TeamTaskBase
	for _, task := range pendingTasks {
		count, _ := tm.db.Task().GetUnresolvedDependenciesCount(ctx, task.TaskID)
		if count == 0 {
			result = append(result, task)
		}
	}
	return result, nil
}

// GetTasksByAssignee 按成员查任务。对齐 Python: TeamTaskManager.get_tasks_by_assignee()
func (tm *TeamTaskManager) GetTasksByAssignee(ctx context.Context, memberName, status string) ([]*database.TeamTaskBase, error) {
	return tm.db.Task().GetTasksByAssignee(ctx, tm.teamName, memberName, status)
}

// Claim 成员自认领。对齐 Python: TeamTaskManager.claim()
func (tm *TeamTaskManager) Claim(ctx context.Context, taskID string) error {
	ok, err := tm.db.Task().ClaimTask(ctx, taskID, tm.memberName)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("认领任务失败: 任务不存在或状态不允许认领 %s", taskID)
	}
	tm.publishTaskEvent(ctx, eventTaskClaimed, map[string]any{
		"team_name": tm.teamName, "task_id": taskID,
	})
	return nil
}

// Assign Leader 分配。对齐 Python: TeamTaskManager.assign()
func (tm *TeamTaskManager) Assign(ctx context.Context, taskID, assignee string) error {
	ok, err := tm.db.Task().ClaimTask(ctx, taskID, assignee)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("分配任务失败: 任务不存在或状态不允许分配 %s", taskID)
	}
	tm.publishTaskEvent(ctx, eventTaskClaimed, map[string]any{
		"team_name": tm.teamName, "task_id": taskID,
	})
	return nil
}

// Complete 完成任务。对齐 Python: TeamTaskManager.complete()
func (tm *TeamTaskManager) Complete(ctx context.Context, taskID string) ([]string, error) {
	refreshed, err := tm.db.Task().CompleteTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	// terminateTaskInSession: nil=nil 表示不存在/FSM不合法，非 nil 表示成功（含幂等）
	if refreshed == nil {
		return nil, fmt.Errorf("完成任务失败: 任务不存在或状态不允许完成 %s", taskID)
	}
	tm.publishTaskEvent(ctx, eventTaskCompleted, map[string]any{
		"team_name": tm.teamName, "task_id": taskID,
	})
	tm.publishUnblockedEvents(ctx, refreshed)
	tm.maybePublishTaskListDrained(ctx)
	return refreshed, nil
}

// Cancel 取消单条任务。对齐 Python: TeamTaskManager.cancel()
func (tm *TeamTaskManager) Cancel(ctx context.Context, taskID string) ([]string, error) {
	refreshed, err := tm.db.Task().CancelTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	// terminateTaskInSession: nil=nil 表示不存在/FSM不合法，非 nil 表示成功（含幂等）
	if refreshed == nil {
		return nil, fmt.Errorf("取消任务失败: 任务不存在或状态不允许取消 %s", taskID)
	}
	tm.publishTaskEvent(ctx, eventTaskCancelled, map[string]any{
		"team_name": tm.teamName, "task_id": taskID,
	})
	tm.publishUnblockedEvents(ctx, refreshed)
	tm.maybePublishTaskListDrained(ctx)
	return refreshed, nil
}

// CancelAllTasks 批量取消。对齐 Python: TeamTaskManager.cancel_all_tasks()
func (tm *TeamTaskManager) CancelAllTasks(ctx context.Context, skipAssignees []string) ([]*database.TeamTaskBase, error) {
	cancelled, err := tm.db.Task().CancelAllTasks(ctx, tm.teamName, skipAssignees)
	if err != nil {
		return nil, err
	}
	for _, task := range cancelled {
		tm.publishTaskEvent(ctx, eventTaskCancelled, map[string]any{
			"team_name": tm.teamName, "task_id": task.TaskID,
		})
	}
	tm.maybePublishTaskListDrained(ctx)
	return cancelled, nil
}

// Reset 重置任务（CLAIMED→PENDING）。对齐 Python: TeamTaskManager.reset()
func (tm *TeamTaskManager) Reset(ctx context.Context, taskID string) error {
	ok, err := tm.db.Task().ResetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("重置任务失败: 任务不存在或状态不允许重置 %s", taskID)
	}
	return nil
}

// UpdateTask 更新标题/内容。对齐 Python: TeamTaskManager.update_task()
func (tm *TeamTaskManager) UpdateTask(ctx context.Context, taskID, title, content string) error {
	ok, err := tm.db.Task().UpdateTask(ctx, taskID, title, content)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("更新任务失败: 任务不存在或状态不允许编辑 %s", taskID)
	}
	tm.publishTaskEvent(ctx, eventTaskUpdated, map[string]any{
		"team_name": tm.teamName, "task_id": taskID,
	})
	return nil
}

// AddWithPriority 带双向依赖创建任务。对齐 Python: TeamTaskManager.add_with_priority()
func (tm *TeamTaskManager) AddWithPriority(ctx context.Context, taskID, title, content string, dependsOnIDs []string, newTasksSpec []database.NewTaskSpec) (database.GraphMutationResult, error) {
	task := &database.TeamTaskBase{
		TaskID:   taskID,
		TeamName: tm.teamName,
		Title:    title,
		Content:  content,
		Status:   fsm.TaskStatusPending,
	}
	result := tm.db.Task().AddTaskWithBidirectionalDependencies(ctx, tm.teamName, task, dependsOnIDs)
	if !result.Ok {
		return result, fmt.Errorf("带依赖创建任务失败: %s", result.Reason)
	}
	tm.publishTaskEvent(ctx, eventTaskCreated, map[string]any{
		"team_name": tm.teamName, "task_id": taskID, "status": fsm.TaskStatusPending,
	})
	return result, nil
}

// AddAsTopPriority 最高优先级插入。对齐 Python: TeamTaskManager.add_as_top_priority()
func (tm *TeamTaskManager) AddAsTopPriority(ctx context.Context, title, content string) (*database.TeamTaskBase, error) {
	taskID := fmt.Sprintf("task_%s_%d_%d_priority", tm.teamName, time.Now().UnixMilli(), time.Now().UnixNano()%1000)
	task := &database.TeamTaskBase{
		TaskID:   taskID,
		TeamName: tm.teamName,
		Title:    title,
		Content:  content,
		Status:   fsm.TaskStatusPending,
	}
	ok, err := tm.db.Task().CreateTask(ctx, task)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("创建最高优先级任务失败: task_id 冲突 %s", taskID)
	}

	// 阻塞所有 PENDING 任务：每个 PENDING 任务依赖新任务
	pendingTasks, _ := tm.db.Task().GetTeamTasks(ctx, tm.teamName, fsm.TaskStatusPending)
	var edges []database.EdgeSpec
	for _, existing := range pendingTasks {
		if existing.TaskID == taskID {
			continue // 跳过自己
		}
		edges = append(edges, database.EdgeSpec{TaskID: existing.TaskID, DependsOnID: taskID})
	}

	if len(edges) > 0 {
		result := tm.db.Task().MutateDependencyGraph(ctx, tm.teamName, nil, edges)
		if !result.Ok {
			return task, fmt.Errorf("添加阻塞依赖失败: %s", result.Reason)
		}
	}

	tm.publishTaskEvent(ctx, eventTaskCreated, map[string]any{
		"team_name": tm.teamName, "task_id": taskID, "status": fsm.TaskStatusPending,
	})
	return task, nil
}

// AddDependencies 向已有任务添加依赖。对齐 Python: TeamTaskManager.add_dependencies()
func (tm *TeamTaskManager) AddDependencies(ctx context.Context, taskID string, dependsOnIDs []string) (database.GraphMutationResult, error) {
	var edges []database.EdgeSpec
	for _, upstreamID := range dependsOnIDs {
		edges = append(edges, database.EdgeSpec{TaskID: taskID, DependsOnID: upstreamID})
	}
	result := tm.db.Task().MutateDependencyGraph(ctx, tm.teamName, nil, edges)
	if !result.Ok {
		return result, fmt.Errorf("添加依赖失败: %s", result.Reason)
	}
	return result, nil
}

// GetTaskDetail 详细视图（含 blocked_by + blocks）。
// 对齐 Python: TeamTaskManager.get_task_detail()
func (tm *TeamTaskManager) GetTaskDetail(ctx context.Context, taskID string) (*TaskDetail, error) {
	task, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, nil
	}

	// blocked_by：task 的上游依赖
	deps, _ := tm.db.Task().GetTaskDependencies(ctx, taskID)
	var blockedBy []*database.TeamTaskBase
	for _, dep := range deps {
		upstream, _ := tm.db.Task().GetTask(ctx, dep.DependsOnID)
		if upstream != nil {
			blockedBy = append(blockedBy, upstream)
		}
	}

	// blocks：task 的下游依赖
	downstream, _ := tm.db.Task().GetTasksDependingOn(ctx, taskID)
	return &TaskDetail{
		Task:      task,
		BlockedBy: blockedBy,
		Blocks:    downstream,
	}, nil
}

// ListTasksWithDeps 摘要视图。对齐 Python: TeamTaskManager.list_tasks_with_deps()
func (tm *TeamTaskManager) ListTasksWithDeps(ctx context.Context) ([]*TaskSummary, error) {
	tasks, err := tm.db.Task().GetTeamTasks(ctx, tm.teamName, "")
	if err != nil {
		return nil, err
	}
	var result []*TaskSummary
	for _, task := range tasks {
		deps, _ := tm.db.Task().GetTaskDependencies(ctx, task.TaskID)
		var blockedBy []string
		for _, dep := range deps {
			blockedBy = append(blockedBy, dep.DependsOnID)
		}
		result = append(result, &TaskSummary{
			TaskID:    task.TaskID,
			Title:     task.Title,
			Status:    task.Status,
			Assignee:  task.Assignee,
			BlockedBy: blockedBy,
		})
	}
	return result, nil
}

// SubmitPlan PLAN_MODE 提交计划。对齐 Python: TeamTaskManager.submit_plan()
func (tm *TeamTaskManager) SubmitPlan(ctx context.Context, taskID, planFilePath, toolCallID string) (*PlanRecord, error) {
	// 1. 校验成员是否为 PLAN_MODE
	member, _ := tm.db.Member().GetMember(ctx, tm.memberName, tm.teamName)
	if member == nil || member.Mode != "plan_mode" {
		return nil, fmt.Errorf("成员 %s 不是 PLAN_MODE", tm.memberName)
	}

	// 2. 校验任务状态
	task, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("任务不存在: %s", taskID)
	}
	if task.Status != fsm.TaskStatusPending && task.Status != fsm.TaskStatusClaimed {
		return nil, fmt.Errorf("任务状态不允许提交计划: %s (当前: %s)", taskID, task.Status)
	}
	if task.Status == fsm.TaskStatusClaimed && task.Assignee != tm.memberName {
		return nil, fmt.Errorf("任务 %s 已被 %s 认领，当前成员无法提交计划", taskID, task.Assignee)
	}

	// 3. 如果 PENDING → 先 claim
	if task.Status == fsm.TaskStatusPending {
		ok, _ := tm.db.Task().ClaimTask(ctx, taskID, tm.memberName)
		if !ok {
			return nil, fmt.Errorf("提交计划前认领任务失败: %s", taskID)
		}
	}

	// 4. 生成 plan_id（加入纳秒避免同一毫秒内重复提交时 ID 冲突）
	planID := fmt.Sprintf("plan_%s_%d_%d", taskID, time.Now().UnixMilli(), time.Now().UnixNano()%1000)

	// 5. 拷贝 plan 文件到 plansDir
	planDir := filepath.Join(tm.plansDir, tm.teamPlanID, "tasks", taskID, "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建计划目录失败: %v", err)
	}
	destPath := filepath.Join(planDir, planID+".md")
	if planFilePath != "" {
		content, err := os.ReadFile(planFilePath)
		if err != nil {
			return nil, fmt.Errorf("读取计划文件失败: %v", err)
		}
		if err := os.WriteFile(destPath, content, 0o644); err != nil {
			return nil, fmt.Errorf("写入计划文件失败: %v", err)
		}
	}

	// 6. 写入 index.json
	record := &PlanRecord{
		PlanID:     planID,
		TaskID:     taskID,
		MemberName: tm.memberName,
		Status:     fsm.TaskStatusClaimed,
		Decision:   "pending",
		CreatedAt:  time.Now().UnixMilli(),
	}
	if err := tm.writePlanIndex(record); err != nil {
		return nil, fmt.Errorf("写入计划索引失败: %v", err)
	}

	// 7. 发布事件
	tm.publishTaskEvent(ctx, eventTaskPlanRequest, map[string]any{
		"team_name":      tm.teamName,
		"member_name":    tm.memberName,
		"task_id":        taskID,
		"status":         fsm.TaskStatusClaimed,
		"plan_id":        planID,
		"member_plan_md": destPath,
		"tool_call_id":   toolCallID,
	})

	return record, nil
}

// ApprovePlan PLAN_MODE 审批/拒绝计划。对齐 Python: TeamTaskManager.approve_plan()
func (tm *TeamTaskManager) ApprovePlan(ctx context.Context, planID string, approved bool, feedback string) error {
	// 1. 加载 index.json
	index, err := tm.loadPlanIndex()
	if err != nil {
		return fmt.Errorf("加载计划索引失败: %v", err)
	}

	// 2. 校验 planID 存在
	planRecord, planExists := index.TaskPlans[planID]
	if !planExists {
		return fmt.Errorf("计划不存在: %s", planID)
	}

	// 3. 校验 task 状态
	task, _ := tm.db.Task().GetTask(ctx, planRecord.TaskID)
	if task == nil {
		return fmt.Errorf("任务不存在: %s", planRecord.TaskID)
	}
	if task.Status != fsm.TaskStatusClaimed {
		return fmt.Errorf("任务状态应为 CLAIMED: 当前 %s", task.Status)
	}

	// 4. 校验 planID 是 latest_plan_id
	taskPlanIdx, idxExists := index.Tasks[planRecord.TaskID]
	if idxExists && taskPlanIdx.LatestPlanID != planID {
		return fmt.Errorf("不能审批过期计划: latest=%s, current=%s", taskPlanIdx.LatestPlanID, planID)
	}

	// 5. 校验 decision 为 pending
	if planRecord.Decision != "pending" {
		return fmt.Errorf("计划已审批: decision=%s", planRecord.Decision)
	}

	// 6. 校验 plan 文件物理存在
	planDir := filepath.Join(tm.plansDir, tm.teamPlanID, "tasks", planRecord.TaskID, "plans")
	planFile := filepath.Join(planDir, planID+".md")
	if _, err := os.Stat(planFile); os.IsNotExist(err) {
		return fmt.Errorf("计划文件不存在: %s", planFile)
	}

	if approved {
		// 审批通过：CLAIMED→PLAN_APPROVED
		ok, _ := tm.db.Task().ApprovePlanTask(ctx, planRecord.TaskID)
		if !ok {
			return fmt.Errorf("审批 FSM 转换失败: %s", planRecord.TaskID)
		}
		planRecord.Decision = "approve"
		planRecord.Status = fsm.TaskStatusPlanApproved
	} else {
		// 审批拒绝：任务保持 CLAIMED
		planRecord.Decision = "reject"
		planRecord.Feedback = feedback
		planRecord.Status = fsm.TaskStatusClaimed
	}

	// 更新 index.json
	if err := tm.updatePlanIndex(planID, planRecord); err != nil {
		return fmt.Errorf("更新计划索引失败: %v", err)
	}

	// 发布事件
	tm.publishTaskEvent(ctx, eventTaskPlanResponse, map[string]any{
		"team_name":   tm.teamName,
		"member_name": tm.memberName,
		"task_id":     planRecord.TaskID,
		"approved":    approved,
		"status":      planRecord.Status,
		"plan_id":     planID,
		"feedback":    feedback,
	})

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// publishTaskEvent 发布任务事件到 TeamTopic。
// 对齐 Python: TeamTaskManager._publish_task_event()
func (tm *TeamTaskManager) publishTaskEvent(ctx context.Context, eventType string, payload map[string]any) {
	if tm.messager == nil {
		return
	}
	topicID := buildTaskTopic(tm.sessionID, tm.teamName)
	msg := map[string]any{
		"event_type": eventType,
		"payload":    payload,
	}
	if err := tm.messager.Publish(ctx, topicID, msg); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).
			Str("event_type", eventType).
			Msg("发布任务事件失败")
	}
}

// publishUnblockedEvents 逐条发布 TaskUnblockedEvent。
// 对齐 Python: TeamTaskManager._publish_unblocked_events()
func (tm *TeamTaskManager) publishUnblockedEvents(ctx context.Context, unblockedIDs []string) {
	for _, id := range unblockedIDs {
		tm.publishTaskEvent(ctx, eventTaskUnblocked, map[string]any{
			"team_name": tm.teamName, "task_id": id,
		})
	}
}

// maybePublishTaskListDrained 检查是否所有任务已终态，如果是则发布 TaskListDrainedEvent。
// 对齐 Python: TeamTaskManager._maybe_publish_task_list_drained()
func (tm *TeamTaskManager) maybePublishTaskListDrained(ctx context.Context) {
	tasks, err := tm.db.Task().GetTeamTasks(ctx, tm.teamName, "")
	if err != nil || len(tasks) == 0 {
		return
	}
	allTerminal := true
	for _, task := range tasks {
		if task.Status != fsm.TaskStatusCompleted && task.Status != fsm.TaskStatusCancelled {
			allTerminal = false
			break
		}
	}
	if allTerminal {
		tm.publishTaskEvent(ctx, eventTaskListDrained, map[string]any{
			"team_name":  tm.teamName,
			"task_count": len(tasks),
		})
	}
}

// checkTaskListDrained 检查所有任务是否均处于终态。
// 对齐 Python: TeamTaskManager._check_task_list_drained()
func (tm *TeamTaskManager) checkTaskListDrained(ctx context.Context) bool {
	tasks, err := tm.db.Task().GetTeamTasks(ctx, tm.teamName, "")
	if err != nil || len(tasks) == 0 {
		return false
	}
	for _, t := range tasks {
		if t.Status != fsm.TaskStatusCompleted && t.Status != fsm.TaskStatusCancelled {
			return false
		}
	}
	return true
}

// writePlanIndex 写入或创建 index.json。
func (tm *TeamTaskManager) writePlanIndex(record *PlanRecord) error {
	indexPath := filepath.Join(tm.plansDir, tm.teamPlanID, "index.json")
	index, err := tm.loadPlanIndex()
	if err != nil {
		index = &PlanIndex{
			Tasks:     make(map[string]*TaskPlanIndex),
			TaskPlans: make(map[string]*PlanRecord),
		}
	}

	// 更新 task_plans
	index.TaskPlans[record.PlanID] = record

	// 更新 tasks
	taskIdx, exists := index.Tasks[record.TaskID]
	if !exists {
		taskIdx = &TaskPlanIndex{PlanIDs: []string{}, Status: record.Status}
		index.Tasks[record.TaskID] = taskIdx
	}
	taskIdx.PlanIDs = append(taskIdx.PlanIDs, record.PlanID)
	taskIdx.LatestPlanID = record.PlanID
	taskIdx.Status = record.Status

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath, data, 0o644)
}

// updatePlanIndex 更新已有 plan 记录。
func (tm *TeamTaskManager) updatePlanIndex(planID string, record *PlanRecord) error {
	indexPath := filepath.Join(tm.plansDir, tm.teamPlanID, "index.json")
	index, err := tm.loadPlanIndex()
	if err != nil {
		return err
	}

	// 更新 task_plans
	index.TaskPlans[planID] = record

	// 更新 tasks 的 status
	taskIdx, exists := index.Tasks[record.TaskID]
	if exists {
		taskIdx.Status = record.Status
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath, data, 0o644)
}

// loadPlanIndex 加载 index.json。
func (tm *TeamTaskManager) loadPlanIndex() (*PlanIndex, error) {
	indexPath := filepath.Join(tm.plansDir, tm.teamPlanID, "index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}
	var index PlanIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}
	return &index, nil
}
