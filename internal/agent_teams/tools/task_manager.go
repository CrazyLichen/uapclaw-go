package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskCreateSpec 批量创建任务的输入规范。
// 对齐 Python: add_batch(tasks: List[dict]) 中每个 dict 的字段。
type TaskCreateSpec struct {
	// TaskID 可选自定义任务 ID，空则自动生成。对齐 Python: task_spec.get("task_id")
	TaskID string
	// Title 任务标题
	Title string
	// Content 任务内容
	Content string
	// Dependencies 可选依赖列表。对齐 Python: task_spec.get("dependencies")
	Dependencies []string
}

// TaskAddOption 添加任务的可选参数，对齐 Python: add(task_id, dependencies) 可选参数
type TaskAddOption func(*taskAddConfig)

// taskAddConfig Add 方法的可选配置
type taskAddConfig struct {
	taskID       string
	dependencies []string
}

// TaskAddWithPriorityOption AddWithPriority 的可选参数。
type TaskAddWithPriorityOption func(*taskAddWithPriorityConfig)

// taskAddWithPriorityConfig AddWithPriority 的可选配置
type taskAddWithPriorityConfig struct {
	taskID           string
	dependencies     []string // 对齐 Python: dependencies
	dependentTaskIDs []string // 对齐 Python: dependent_task_ids
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
	// plansDir 计划文件存储目录
	plansDir string
	// teamPlanID 团队级计划标识
	teamPlanID string
	// leaderMemberName Leader 成员名（用于通知计划审批）
	leaderMemberName string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponentChannel 日志组件标识
const logComponentChannel = logger.ComponentChannel

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithTaskID 设置自定义任务 ID，对齐 Python: add(task_id=...)
func WithTaskID(taskID string) TaskAddOption {
	return func(c *taskAddConfig) {
		c.taskID = taskID
	}
}

// WithDependencies 设置任务依赖，对齐 Python: add(dependencies=...)
func WithDependencies(deps []string) TaskAddOption {
	return func(c *taskAddConfig) {
		c.dependencies = deps
	}
}

// WithPriorityTaskID 设置自定义任务 ID。对齐 Python: add_with_priority(task_id=...)
func WithPriorityTaskID(taskID string) TaskAddWithPriorityOption {
	return func(c *taskAddWithPriorityConfig) {
		c.taskID = taskID
	}
}

// WithPriorityDependencies 设置新任务依赖的上游任务。对齐 Python: add_with_priority(dependencies=...)
func WithPriorityDependencies(deps []string) TaskAddWithPriorityOption {
	return func(c *taskAddWithPriorityConfig) {
		c.dependencies = deps
	}
}

// WithPriorityDependentTaskIDs 设置依赖新任务的下游任务。对齐 Python: add_with_priority(dependent_task_ids=...)
func WithPriorityDependentTaskIDs(ids []string) TaskAddWithPriorityOption {
	return func(c *taskAddWithPriorityConfig) {
		c.dependentTaskIDs = ids
	}
}

// NewTeamTaskManager 创建任务管理器。
// sessionID 从 context 中获取（schema.GetSessionID(ctx)），不再作为构造参数。
func NewTeamTaskManager(db database.TeamDatabase, teamName, memberName string, messager messager.Messager, plansDir, teamPlanID, leaderMemberName string) *TeamTaskManager {
	return &TeamTaskManager{
		db:               db,
		teamName:         teamName,
		memberName:       memberName,
		messager:         messager,
		plansDir:         plansDir,
		teamPlanID:       teamPlanID,
		leaderMemberName: leaderMemberName,
	}
}

// Add 创建单条任务。对齐 Python: TeamTaskManager.add()
// 支持可选参数 WithTaskID、WithDependencies，对齐 Python: add(task_id, dependencies)
// 有依赖时走 MutateDependencyGraph 原子路径（环检测+状态刷新），无依赖时走 CreateTask。
func (tm *TeamTaskManager) Add(ctx context.Context, title, content string, opts ...TaskAddOption) (*database.TeamTaskBase, error) {
	cfg := &taskAddConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	taskID := cfg.taskID
	if taskID == "" {
		taskID = generateTaskID(tm.teamName)
	}

	if len(cfg.dependencies) > 0 {
		// 有依赖：走 MutateDependencyGraph 原子路径（对齐 Python: mutate_dependency_graph）
		newTaskSpec := database.NewTaskSpec{
			TaskID:        taskID,
			Title:         title,
			Content:       content,
			InitialStatus: fsm.TaskStatusPending,
		}
		edges := make([]database.EdgeSpec, 0, len(cfg.dependencies))
		for _, depID := range cfg.dependencies {
			edges = append(edges, database.EdgeSpec{TaskID: taskID, DependsOnID: depID})
		}
		result := tm.db.Task().MutateDependencyGraph(ctx, tm.teamName, []database.NewTaskSpec{newTaskSpec}, edges)
		if !result.Ok {
			return nil, fmt.Errorf("创建任务失败: %s", result.Reason)
		}
		// 从 RefreshedTasks 中提取新任务的最终状态（可能 PENDING→BLOCKED）
		task := findRefreshedTask(result.RefreshedTasks, taskID)
		if task == nil {
			task, _ = tm.db.Task().GetTask(ctx, taskID)
		}
		if task != nil {
			tm.publishTaskEvent(ctx, schema.TaskCreatedEvent{
				BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
				TaskID:           task.TaskID,
				Status:           task.Status,
			})
		}
		return task, nil
	}

	// 无依赖：走 CreateTask
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
	tm.publishTaskEvent(ctx, schema.TaskCreatedEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           task.TaskID,
		Status:           task.Status,
	})
	return task, nil
}

// AddBatch 批量创建任务。对齐 Python: TeamTaskManager.add_batch()
// 跳过无效规格（缺 title/content）和创建失败的任务，返回成功创建的列表。
// 对齐 Python: created_tasks 遇错不中断，继续处理后续规格。
func (tm *TeamTaskManager) AddBatch(ctx context.Context, specs []TaskCreateSpec) ([]*database.TeamTaskBase, error) {
	var tasks []*database.TeamTaskBase
	for _, spec := range specs {
		// 对齐 Python: if not title or not content → skip
		if spec.Title == "" || spec.Content == "" {
			logger.Warn(logComponentChannel).Str("spec", fmt.Sprintf("%+v", spec)).Msg("批量创建跳过无效规格")
			continue
		}
		task, err := tm.Add(ctx, spec.Title, spec.Content,
			WithTaskID(spec.TaskID),
			WithDependencies(spec.Dependencies),
		)
		if err != nil {
			// 对齐 Python: if not result.ok → warning + skip
			logger.Warn(logComponentChannel).Err(err).Str("title", spec.Title).Msg("批量创建跳过失败任务")
			continue
		}
		tasks = append(tasks, task)
	}
	// 对齐 Python: team_logger.info(f"Batch added {len(created_tasks)} tasks")
	logger.Info(logComponentChannel).Int("count", len(tasks)).Msg("批量创建完成")
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
	// 1. 检查任务是否存在（对齐 Python: task = await self.get(task_id)）
	task, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("查询任务失败: %w", err)
	}
	if task == nil {
		return fmt.Errorf("任务 %s 不存在", taskID)
	}

	// 2. 检查成员是否存在（对齐 Python: member = await self.db.member.get_member(...)）
	member, err := tm.db.Member().GetMember(ctx, tm.memberName, tm.teamName)
	if err != nil {
		return fmt.Errorf("查询成员失败: %w", err)
	}
	if member == nil {
		return fmt.Errorf("成员 %s 在团队 %s 中不存在", tm.memberName, tm.teamName)
	}

	// 3. PLAN_MODE 检查（对齐 Python: if member.mode == MemberMode.PLAN_MODE.value）
	if member.Mode == "plan_mode" {
		return fmt.Errorf("PLAN_MODE 成员必须先调用 submit_plan，leader 审批后任务从 claimed 变为 plan_approved")
	}

	// 4. 幂等性检查（对齐 Python: if task.assignee == member_name and task.status == CLAIMED）
	if task.Assignee == tm.memberName && task.Status == fsm.TaskStatusClaimed {
		return nil // 已认领，幂等返回成功
	}

	// 5. 已被他人认领检查（对齐 Python: if task.assignee）
	if task.Assignee != "" {
		return fmt.Errorf("任务 %s 已被 %s 认领，%s 无法认领", taskID, task.Assignee, tm.memberName)
	}

	// 6. FSM 状态转换合法性检查（对齐 Python: is_valid_transition(task.status, CLAIMED, TASK_TRANSITIONS)）
	if !database.IsValidTaskTransition(task.Status, fsm.TaskStatusClaimed) {
		return fmt.Errorf("任务 %s 无法从状态 '%s' 认领（只有 pending 任务可认领）", taskID, task.Status)
	}

	// 7. 执行认领（对齐 Python: success = await self.db.task.claim_task(task_id, member_name)）
	ok, err := tm.db.Task().ClaimTask(ctx, taskID, tm.memberName)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("认领任务失败: 数据库拒绝认领 %s（可能存在并发认领竞争）", taskID)
	}

	// 8. 事件发布（对齐 Python: await self.messager.publish(TaskClaimedEvent(...))）
	tm.publishTaskEvent(ctx, schema.TaskClaimedEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName, MemberName: tm.memberName},
		TaskID:           taskID,
	})
	return nil
}

// Assign Leader 分配。对齐 Python: TeamTaskManager.assign()
func (tm *TeamTaskManager) Assign(ctx context.Context, taskID, assignee string) error {
	// 1. 检查任务是否存在（对齐 Python: task = await self.get(task_id)）
	task, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("查询任务失败: %w", err)
	}
	if task == nil {
		return fmt.Errorf("任务 %s 不存在", taskID)
	}

	// 2. 检查被分配者是否是团队成员（对齐 Python: member = await self.db.member.get_member(assignee, ...)）
	member, err := tm.db.Member().GetMember(ctx, assignee, tm.teamName)
	if err != nil {
		return fmt.Errorf("查询成员失败: %w", err)
	}
	if member == nil {
		return fmt.Errorf("成员 %s 在团队 %s 中不存在", assignee, tm.teamName)
	}

	// 3. 幂等性检查（对齐 Python: if task.assignee == assignee and task.status == CLAIMED）
	if task.Assignee == assignee && task.Status == fsm.TaskStatusClaimed {
		return nil // 已分配给同一成员，幂等返回成功
	}

	// 4. 已被他人认领检查（对齐 Python: if task.assignee and task.assignee != assignee）
	if task.Assignee != "" && task.Assignee != assignee {
		return fmt.Errorf("任务 %s 已被 %s 认领，需先 reset 再分配给 %s", taskID, task.Assignee, assignee)
	}

	// 5. 执行分配（对齐 Python: success = await self.db.task.claim_task(task_id, assignee)）
	ok, err := tm.db.Task().ClaimTask(ctx, taskID, assignee)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("分配任务失败: 数据库拒绝分配 %s（状态转换无效: %s）", taskID, task.Status)
	}

	// 6. 事件发布（对齐 Python: await self.messager.publish(TaskClaimedEvent(...))）
	tm.publishTaskEvent(ctx, schema.TaskClaimedEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName, MemberName: assignee},
		TaskID:           taskID,
	})
	return nil
}

// Complete 完成任务。对齐 Python: TeamTaskManager.complete()
func (tm *TeamTaskManager) Complete(ctx context.Context, taskID string) ([]string, error) {
	// 1. 检查成员是否存在（对齐 Python: member = await self.db.member.get_member(...)）
	member, err := tm.db.Member().GetMember(ctx, tm.memberName, tm.teamName)
	if err != nil {
		return nil, fmt.Errorf("查询成员失败: %w", err)
	}
	if member == nil {
		return nil, fmt.Errorf("成员 %s 在团队 %s 中不存在", tm.memberName, tm.teamName)
	}

	// 2. PLAN_MODE 检查（对齐 Python: if member.mode == MemberMode.PLAN_MODE.value）
	if member.Mode == "plan_mode" {
		task, err := tm.db.Task().GetTask(ctx, taskID)
		if err != nil {
			return nil, fmt.Errorf("查询任务失败: %w", err)
		}
		if task == nil {
			return nil, fmt.Errorf("任务 %s 不存在", taskID)
		}
		// PLAN_MODE 成员只能完成 PLAN_APPROVED 状态的任务
		if task.Status != fsm.TaskStatusPlanApproved {
			return nil, fmt.Errorf("PLAN_MODE 成员无法完成状态为 '%s' 的任务 %s（只能完成 plan_approved 任务）", task.Status, taskID)
		}

		// 对齐 Python: PLAN_MODE 下更新 plan index 的完成状态
		planIndex, err := tm.loadPlanIndex()
		if err == nil && planIndex != nil {
			taskIdx, ok := planIndex.Tasks[taskID]
			if ok && taskIdx != nil {
				latestPlanID := ""
				if len(taskIdx.PlanIDs) > 0 {
					latestPlanID = taskIdx.PlanIDs[len(taskIdx.PlanIDs)-1]
				}
				planRecord := &PlanRecord{
					PlanID:     latestPlanID,
					TaskID:     taskID,
					MemberName: task.Assignee,
					Status:     string(fsm.TaskStatusCompleted),
					Decision:   "completed",
					CreatedAt:  time.Now().UnixMilli(),
				}
				if err := tm.updatePlanIndex(latestPlanID, planRecord); err != nil {
					logger.Warn(logComponent).Err(err).Str("task_id", taskID).Msg("PLAN_MODE 完成：更新 plan index 失败")
				}
			}
		}
	}

	// 3. 执行完成（对齐 Python: result = await self.db.task.complete_task(task_id)）
	refreshed, err := tm.db.Task().CompleteTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	// terminateTaskInSession: nil=nil 表示不存在/FSM不合法，非 nil 表示成功（含幂等）
	if refreshed == nil {
		return nil, fmt.Errorf("完成任务失败: 任务不存在或状态不允许完成 %s", taskID)
	}

	// 4. 事件发布（对齐 Python: await self._publish_task_event + _publish_unblocked_events + _maybe_publish_task_list_drained）
	tm.publishTaskEvent(ctx, schema.TaskCompletedEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           taskID,
	})
	// refreshed 是 []string（unblocked task ID 列表），转换为 []*TeamTaskBase
	var unblockedTasks []*database.TeamTaskBase
	for _, id := range refreshed {
		if t, _ := tm.db.Task().GetTask(ctx, id); t != nil {
			unblockedTasks = append(unblockedTasks, t)
		}
	}
	tm.publishUnblockedEvents(ctx, unblockedTasks)
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
	tm.publishTaskEvent(ctx, schema.TaskCancelledEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           taskID,
	})
	// refreshed 是 []string（unblocked task ID 列表），转换为 []*TeamTaskBase
	var unblockedTasks []*database.TeamTaskBase
	for _, id := range refreshed {
		if t, _ := tm.db.Task().GetTask(ctx, id); t != nil {
			unblockedTasks = append(unblockedTasks, t)
		}
	}
	tm.publishUnblockedEvents(ctx, unblockedTasks)
	tm.maybePublishTaskListDrained(ctx)
	return refreshed, nil
}

// CancelAllTasks 批量取消。对齐 Python: TeamTaskManager.cancel_all_tasks()
func (tm *TeamTaskManager) CancelAllTasks(ctx context.Context, skipAssignees []string) ([]*database.TeamTaskBase, error) {
	result, err := tm.db.Task().CancelAllTasks(ctx, tm.teamName, skipAssignees)
	if err != nil {
		return nil, err
	}
	for _, task := range result.Cancelled {
		tm.publishTaskEvent(ctx, schema.TaskCancelledEvent{
			BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
			TaskID:           task.TaskID,
		})
	}
	// 对齐 Python: await self._publish_unblocked_events(unblocked_tasks)
	tm.publishUnblockedEvents(ctx, result.Unblocked)
	tm.maybePublishTaskListDrained(ctx)
	return result.Cancelled, nil
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
	tm.publishTaskEvent(ctx, schema.TaskUpdatedEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           taskID,
	})
	return nil
}

// AddWithPriority 带双向依赖创建任务。对齐 Python: TeamTaskManager.add_with_priority()
// 支持双向依赖：dependencies（新任务依赖谁）+ dependentTaskIDs（谁依赖新任务）。
// 有 dependencies 时初始状态为 BLOCKED，否则为 PENDING。
func (tm *TeamTaskManager) AddWithPriority(ctx context.Context, title, content string, opts ...TaskAddWithPriorityOption) (*database.TeamTaskBase, error) {
	cfg := &taskAddWithPriorityConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	taskID := cfg.taskID
	if taskID == "" {
		taskID = generateTaskID(tm.teamName)
	}

	// 初始状态逻辑（对齐 Python: 有 dependencies → BLOCKED，否则 PENDING）
	initialStatus := fsm.TaskStatusPending
	if len(cfg.dependencies) > 0 {
		initialStatus = fsm.TaskStatusBlocked
	}

	task := &database.TeamTaskBase{
		TaskID:   taskID,
		TeamName: tm.teamName,
		Title:    title,
		Content:  content,
		Status:   initialStatus,
	}
	result := tm.db.Task().AddTaskWithBidirectionalDependencies(ctx, tm.teamName, task, cfg.dependencies, cfg.dependentTaskIDs)
	if !result.Ok {
		return nil, fmt.Errorf("带依赖创建任务失败: %s", result.Reason)
	}

	// 从 RefreshedTasks 中取最终状态
	created := findRefreshedTask(result.RefreshedTasks, taskID)
	if created == nil {
		created, _ = tm.db.Task().GetTask(ctx, taskID)
	}
	if created != nil {
		tm.publishTaskEvent(ctx, schema.TaskCreatedEvent{
			BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
			TaskID:           created.TaskID,
			Status:           created.Status,
		})
	}
	return created, nil
}

// AddAsTopPriority 最高优先级插入。对齐 Python: TeamTaskManager.add_as_top_priority()
// 让所有 PENDING 任务依赖新任务，保证新任务最先执行。
func (tm *TeamTaskManager) AddAsTopPriority(ctx context.Context, title, content string, opts ...TaskAddOption) (*database.TeamTaskBase, error) {
	cfg := &taskAddConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	taskID := cfg.taskID
	if taskID == "" {
		taskID = generateTaskID(tm.teamName, "priority")
	}

	// 查询所有 PENDING 任务作为 dependentTaskIDs
	pendingTasks, _ := tm.db.Task().GetTeamTasks(ctx, tm.teamName, fsm.TaskStatusPending)
	var dependentTaskIDs []string
	for _, t := range pendingTasks {
		if t.TaskID != taskID {
			dependentTaskIDs = append(dependentTaskIDs, t.TaskID)
		}
	}

	task := &database.TeamTaskBase{
		TaskID:   taskID,
		TeamName: tm.teamName,
		Title:    title,
		Content:  content,
		Status:   fsm.TaskStatusPending,
	}
	// 一步原子：通过 AddTaskWithBidirectionalDependencies（对齐 Python）
	result := tm.db.Task().AddTaskWithBidirectionalDependencies(ctx, tm.teamName, task, nil, dependentTaskIDs)
	if !result.Ok {
		return nil, fmt.Errorf("创建最高优先级任务失败: %s", result.Reason)
	}

	created := findRefreshedTask(result.RefreshedTasks, taskID)
	if created == nil {
		created = task
	}
	tm.publishTaskEvent(ctx, schema.TaskCreatedEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           created.TaskID,
		Status:           created.Status,
	})
	return created, nil
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
	tm.publishTaskEvent(ctx, schema.TaskPlanRequestEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName, MemberName: tm.memberName},
		TaskID:           taskID,
		Status:           fsm.TaskStatusClaimed,
		PlanID:           planID,
		MemberPlanMD:     destPath,
		ToolCallID:       toolCallID,
	})

	// 8. 对齐 Python: _notify_leader_of_plan — 通过 P2P 消息直接通知 leader
	tm.notifyLeaderOfPlan(ctx, record, destPath, toolCallID)

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
	tm.publishTaskEvent(ctx, schema.TaskPlanResponseEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName, MemberName: tm.memberName},
		TaskID:           planRecord.TaskID,
		Approved:         approved,
		Status:           planRecord.Status,
		PlanID:           planID,
		Feedback:         feedback,
	})

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// notifyLeaderOfPlan 通过 P2P 消息直接通知 leader 审批计划。
// 对齐 Python: TeamTaskManager._notify_leader_of_plan()
func (tm *TeamTaskManager) notifyLeaderOfPlan(ctx context.Context, record *PlanRecord, planFilePath string, toolCallID string) {
	if tm.messager == nil {
		return
	}
	leaderName := tm.resolveLeaderMemberName()
	if leaderName == "" {
		logger.Warn(logComponentChannel).
			Str("team", tm.teamName).
			Str("task_id", record.TaskID).
			Str("plan_id", record.PlanID).
			Msg("notifyLeaderOfPlan: 无法通知 leader，leader_member_name 为空")
		return
	}
	// 对齐 Python: leader_member_name == self.member_name → 跳过
	if leaderName == tm.memberName {
		return
	}

	content := renderPlanReviewMessage(record.MemberName, record.TaskID, record.PlanID, planFilePath, toolCallID)
	msg := schema.EventMessageFromEvent(schema.MessageEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
		MessageID:        fmt.Sprintf("plan_notify_%s_%d", record.PlanID, time.Now().UnixMilli()),
		FromMemberName:   tm.memberName,
		ToMemberName:     leaderName,
	})
	// 在 payload 中附加内容信息，供 leader 处理
	if msg.Payload == nil {
		msg.Payload = make(map[string]any)
	}
	msg.Payload["content"] = content
	if err := tm.messager.Send(ctx, leaderName, msg); err != nil {
		logger.Warn(logComponentChannel).
			Str("leader", leaderName).
			Str("task_id", record.TaskID).
			Str("plan_id", record.PlanID).
			Err(err).
			Msg("notifyLeaderOfPlan: 发送 P2P 消息失败")
	}
}

// resolveLeaderMemberName 解析 leader 成员名。
// 对齐 Python: TeamTaskManager._resolve_leader_member_name()
func (tm *TeamTaskManager) resolveLeaderMemberName() string {
	if tm.leaderMemberName != "" {
		return tm.leaderMemberName
	}
	// 对齐 Python: 从 db.team.get_team 获取 leader_member_name
	team, err := tm.db.Team().GetTeam(context.Background(), tm.teamName)
	if err != nil || team == nil {
		return ""
	}
	name := team.LeaderMemberName
	if name != "" {
		tm.leaderMemberName = name
	}
	return name
}

// renderPlanReviewMessage 渲染计划审批消息。
// 对齐 Python: TeamTaskManager._render_plan_review_message()
func renderPlanReviewMessage(memberName, taskID, planID, planFilePath, toolCallID string) string {
	lines := []string{
		"Member task plan approval request.",
		fmt.Sprintf("Member: %s", memberName),
		fmt.Sprintf("Task ID: %s", taskID),
		fmt.Sprintf("Plan ID: %s", planID),
		fmt.Sprintf("Plan file: %s", planFilePath),
	}
	if toolCallID != "" {
		lines = append(lines, fmt.Sprintf("Tool Call ID: %s", toolCallID))
	}
	lines = append(lines, "", "Please review the plan file and call approve_plan with this plan_id.")
	return strings.Join(lines, "\n")
}

// publishTaskEvent 发布任务事件到 TeamTopic。
// 对齐 Python: TeamTaskManager._publish_task_event()
// sessionID 从 context 中获取（schema.GetSessionID(ctx)），对齐 Python: get_session_id()。
func (tm *TeamTaskManager) publishTaskEvent(ctx context.Context, event schema.TypedEvent) {
	if tm.messager == nil {
		return
	}
	msg := schema.EventMessageFromEvent(event)
	topicID := schema.TeamTopicTask.Build(schema.GetSessionID(ctx), tm.teamName)
	if err := tm.messager.Publish(ctx, topicID, msg); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", event.EventTypeName()).
			Msg("发布任务事件失败")
	}
}

// publishUnblockedEvents 逐条发布 TaskUnblockedEvent。
// 对齐 Python: TeamTaskManager._publish_unblocked_events()
func (tm *TeamTaskManager) publishUnblockedEvents(ctx context.Context, unblockedTasks []*database.TeamTaskBase) {
	for _, t := range unblockedTasks {
		tm.publishTaskEvent(ctx, schema.TaskUnblockedEvent{
			BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
			TaskID:           t.TaskID,
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
		tm.publishTaskEvent(ctx, schema.TaskListDrainedEvent{
			BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
			TaskCount:        len(tasks),
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

// findRefreshedTask 从 refreshedTasks 列表中按 taskID 查找任务。
// 对齐 Python: for refreshed in mutation.refreshed_tasks: if refreshed.task_id == task_id
func findRefreshedTask(refreshed []*database.TeamTaskBase, taskID string) *database.TeamTaskBase {
	for _, t := range refreshed {
		if t.TaskID == taskID {
			return t
		}
	}
	return nil
}

// generateTaskID 生成任务 ID，对齐 Python: uuid.uuid4() 的等价逻辑。
func generateTaskID(teamName string, suffixes ...string) string {
	base := fmt.Sprintf("task_%s_%d_%d", teamName, time.Now().UnixMilli(), time.Now().UnixNano()%1000)
	if len(suffixes) > 0 {
		base += "_" + strings.Join(suffixes, "_")
	}
	return base
}
