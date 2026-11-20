package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskCreateSpec 批量创建任务的输入规范。
// Python: add_batch(tasks: List[dict]) 中每个 dict 的字段。
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
	// taskID 可选自定义任务 ID
	taskID string
	// dependencies 可选依赖列表
	dependencies []string
	// assignee 可选任务所有者（调度式创建时在写图阶段就落上）
	assignee *string
}

// TaskAddWithPriorityOption AddWithPriority 的可选参数。
type TaskAddWithPriorityOption func(*taskAddWithPriorityConfig)

// taskAddWithPriorityConfig AddWithPriority 的可选配置
type taskAddWithPriorityConfig struct {
	// taskID 可选自定义任务 ID
	taskID string
	// dependencies 依赖列表
	dependencies []string
	// dependentTaskIDs 被依赖任务 ID 列表
	dependentTaskIDs []string
}

// TaskCreateResult 任务创建结果，对齐 Python TaskCreateResult。
// Python: openjiuwen/agent_teams/schema/task.py (TaskCreateResult)
type TaskCreateResult struct {
	// Task 成功时为创建的任务，失败时为 nil
	Task *database.TeamTaskBase
	// Reason 失败原因
	Reason string
}

// TaskDetail 任务详细视图（含阻塞关系）。
type TaskDetail struct {
	// Task 任务基本信息
	Task *database.TeamTaskBase
	// BlockedBy 阻塞该任务的任务列表
	BlockedBy []*database.TeamTaskBase
	// Blocks 被该任务阻塞的任务列表
	Blocks []*database.TeamTaskBase
}

// TaskSummary 任务摘要视图（含阻塞信息）。
type TaskSummary struct {
	// TaskID 任务标识
	TaskID string
	// Title 任务标题
	Title string
	// Status 任务状态
	Status string
	// Assignee 指派成员
	Assignee string
	// BlockedBy 阻塞该任务的任务 ID 列表
	BlockedBy []string
}

// PlanRecord 计划记录（index.json 中的一条）。
// Python: _write_task_plan_index 写入的完整字段集
type PlanRecord struct {
	// PlanID 计划唯一标识
	PlanID string `json:"plan_id"`
	// TaskID 关联的任务标识
	TaskID string `json:"task_id"`
	// TeamPlanID 团队级计划标识
	TeamPlanID string `json:"team_plan_id,omitempty"`
	// MemberName 提交计划的成员名
	MemberName string `json:"member_name"`
	// Status 计划状态
	Status string `json:"status"`
	// LatestPlanID 最新计划标识
	LatestPlanID string `json:"latest_plan_id,omitempty"`
	// MemberPlanMD 成员计划 Markdown 文件路径
	MemberPlanMD string `json:"member_plan_md,omitempty"`
	// SourcePlanPath 源计划文件路径
	SourcePlanPath string `json:"source_plan_path,omitempty"`
	// ToolCallID 触发计划提交的工具调用 ID
	ToolCallID string `json:"tool_call_id,omitempty"`
	// LeaderMessageID Leader 审批消息 ID
	LeaderMessageID string `json:"leader_message_id,omitempty"`
	// LeaderName Leader 成员名
	LeaderName string `json:"leader_name,omitempty"`
	// Decision 审批决定（pending/approve/reject）
	Decision string `json:"decision"`
	// Feedback 审批反馈
	Feedback string `json:"feedback,omitempty"`
	// SubmittedAt 提交时间
	SubmittedAt string `json:"submitted_at,omitempty"`
	// DecidedAt 审批时间
	DecidedAt string `json:"decided_at,omitempty"`
	// CompletedAt 完成时间
	CompletedAt string `json:"completed_at,omitempty"`
	// UpdatedAt 更新时间
	UpdatedAt string `json:"updated_at,omitempty"`
}

// PlanIndex 计划索引（index.json 结构）。
type PlanIndex struct {
	// Tasks 任务ID → 每任务的计划索引
	Tasks map[string]*TaskPlanIndex `json:"tasks"`
	// TaskPlans 计划ID → 计划记录
	TaskPlans map[string]*PlanRecord `json:"task_plans"`
}

// TaskPlanIndex 每任务的计划索引。
type TaskPlanIndex struct {
	// PlanIDs 计划ID列表（按提交顺序）
	PlanIDs []string `json:"plan_ids"`
	// LatestPlanID 最新计划ID
	LatestPlanID string `json:"latest_plan_id"`
	// Status 任务状态
	Status string `json:"status"`
}

// TeamTaskManager 团队任务管理器。
// Python: TeamTaskManager (openjiuwen/agent_teams/tools/task_manager.py)
type TeamTaskManager struct {
	// db 团队数据库实例（内含 TaskDao）
	db database.TeamDatabase
	// dispatchMode 团队任务下发模式（autonomous/scheduled）。
	// Python: self._dispatch_mode（verify_task 判定策略开关）。
	dispatchMode string
	// teamName 团队标识
	teamName string
	// memberName 当前成员标识
	memberName string
	// messager 事件发布器
	messager messager.Messager
	// messageManager 消息管理器（用于 notifyLeaderOfPlan 持久化消息到 DB）
	// Python: _notify_leader_of_plan 中创建 TeamMessageManager 实例
	messageManager *TeamMessageManager
	// plansDir 计划文件存储目录
	plansDir string
	// teamPlanID 团队级计划标识
	teamPlanID string
	// leaderMemberName Leader 成员名（用于通知计划审批）
	leaderMemberName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Ok 返回创建是否成功。
func (r *TaskCreateResult) Ok() bool {
	return r.Task != nil
}

// WithTaskID 设置自定义任务 ID，对齐 Python: add(task_id=...)
func WithTaskID(taskID string) TaskAddOption {
	return func(c *taskAddConfig) {
		c.taskID = taskID
	}
}

// WithTaskAssignee 指定任务所有者。
//
// 调度式创建（create_task 带 member/assignee）用它在写图阶段就把 owner 落上：
// 任务停在 PENDING（有依赖则 BLOCKED）并带着 owner，由调度器交接启动。
//
// Python: NewTaskSpec.assignee（add_graph 时一并写入）
func WithTaskAssignee(assignee string) TaskAddOption {
	return func(c *taskAddConfig) { c.assignee = &assignee }
}

// WithDependencies 指定新任务依赖的任务标识列表。
//
// 有依赖的任务创建后进入 BLOCKED，等上游任务全部完成才转 PENDING。
// Python: NewTaskSpec.dependencies（add_graph 时一并写入）
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

// MemberName 返回当前成员标识。
// Python: TeamTaskManager.member_name
func (tm *TeamTaskManager) MemberName() string { return tm.memberName }

// NewTeamTaskManager 创建团队任务管理器。
// sessionID 从 context 中获取（schema.GetSessionID(ctx)），不再作为构造参数。
// Python: TeamTaskManager.__init__(db, team_name, member_name, messager, message_manager, plans_dir, team_plan_id, leader_member_name)
func NewTeamTaskManager(db database.TeamDatabase, teamName, memberName string, msg messager.Messager, messageManager *TeamMessageManager, plansDir, teamPlanID, leaderMemberName string) *TeamTaskManager {
	tm := &TeamTaskManager{
		db:               db,
		teamName:         teamName,
		memberName:       memberName,
		messager:         msg,
		messageManager:   messageManager,
		plansDir:         plansDir,
		teamPlanID:       teamPlanID,
		leaderMemberName: leaderMemberName,
	}
	// 25.2b F_62：从团队行读 dispatch mode（Python 构造参数传入；
	// Go 团队行已持久化该值，读库等价）。读不到（测试替身的 nil DAO、
	// 读库报错）即 autonomous 缺省。
	tm.dispatchMode = readDispatchMode(db, teamName)
	return tm
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
			Assignee:      cfg.assignee,
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
			tm.publishTaskEvent(ctx, events.TaskCreatedEvent{
				BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
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
		Assignee: cfg.assignee,
	}
	ok, err := tm.db.Task().CreateTask(ctx, task)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("创建任务失败: task_id 冲突 %s", taskID)
	}
	tm.publishTaskEvent(ctx, events.TaskCreatedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           task.TaskID,
		Status:           task.Status,
	})
	return task, nil
}

// AddBatch 批量创建任务。对齐 Python: TeamTaskManager.add_batch()
// 跳过无效规格（缺 title/content）和创建失败的任务，返回 TaskCreateResult 列表。
// Python: created_tasks 遇错不中断，继续处理后续规格。
func (tm *TeamTaskManager) AddBatch(ctx context.Context, specs []TaskCreateSpec) ([]*TaskCreateResult, error) {
	var results []*TaskCreateResult
	for _, spec := range specs {
		// Python: if not title or not content → skip
		if spec.Title == "" || spec.Content == "" {
			logger.Warn(logComponent).Str("spec", fmt.Sprintf("%+v", spec)).Msg("Batch create skipping invalid spec")
			results = append(results, &TaskCreateResult{Reason: "无效规格：缺少标题或内容"})
			continue
		}
		task, err := tm.Add(ctx, spec.Title, spec.Content,
			WithTaskID(spec.TaskID),
			WithDependencies(spec.Dependencies),
		)
		if err != nil {
			// Python: if not result.ok → warning + skip
			logger.Warn(logComponent).Err(err).Str("title", spec.Title).Msg("Batch create skipping failed task")
			results = append(results, &TaskCreateResult{Reason: err.Error()})
			continue
		}
		results = append(results, &TaskCreateResult{Task: task})
	}
	// Python: team_logger.info(f"Batch added {len(created_tasks)} tasks")
	created := 0
	for _, r := range results {
		if r.Ok() {
			created++
		}
	}
	logger.Info(logComponent).Int("count", created).Int("total", len(results)).Msg("Batch create completed")
	return results, nil
}

// Get 按 ID 查任务。对齐 Python: TeamTaskManager.get()
func (tm *TeamTaskManager) Get(ctx context.Context, taskID string) (*database.TeamTaskBase, error) {
	return tm.db.Task().GetTask(ctx, taskID)
}

// ListTasks 列出团队任务。对齐 Python: TeamTaskManager.list_tasks()
func (tm *TeamTaskManager) ListTasks(ctx context.Context, status string) ([]*database.TeamTaskBase, error) {
	return tm.db.Task().GetTeamTasks(ctx, tm.teamName, status)
}

// OpenTaskCountsByAssignee 按 assignee 统计非终态任务数（缺席 = 没有开放任务）。
// Python: TeamTaskManager.open_task_counts_by_assignee (task_manager.py:1552-1554)
func (tm *TeamTaskManager) OpenTaskCountsByAssignee(ctx context.Context) (map[string]int, error) {
	return tm.db.Task().OpenTaskCountsByAssignee(ctx, tm.teamName)
}

// BoardCounts 一条聚合查询返回 (total, non_terminal) 板面计数（B_09）。
// Python: TeamTaskManager.board_counts (task_manager.py L1556-1558)
func (tm *TeamTaskManager) BoardCounts(ctx context.Context) (int64, int64, error) {
	return tm.db.Task().CountTasksTerminality(ctx, tm.teamName)
}

// HasOpenTasks 板面是否仍有非终态任务（B_09）。
// Python: TeamTaskManager.has_open_tasks (task_manager.py L1560-1563)
func (tm *TeamTaskManager) HasOpenTasks(ctx context.Context) (bool, error) {
	_, nonTerminal, err := tm.BoardCounts(ctx)
	if err != nil {
		return false, err
	}
	return nonTerminal > 0, nil
}

// UnseenRevisions 全队级「已指派、未终态、assignee 未见过当前 revision」窄投影
// （F_129）——调度器 revision scan 每次板面唤醒都跑，不读正文。
// Python: TeamTaskManager.unseen_revisions (task_manager.py L1744-1746)
func (tm *TeamTaskManager) UnseenRevisions(ctx context.Context) ([]database.UnseenRevisionRow, error) {
	return tm.db.Task().GetUnseenRevisions(ctx, tm.teamName)
}

// ObserveRevision 记录本成员（任务 assignee）被展示了 revision（F_129）。
// 框架把任务内容交给 assignee 的地方调用：模板化 handoff 投递、自认领通知、
// 自己的 view_task(get)。尽力而为：失败只意味着 assignee 会再多被告知一次。
// Python: TeamTaskManager.observe_revision（task_manager.py L1731-1742）
func (tm *TeamTaskManager) ObserveRevision(ctx context.Context, taskID string, revision int64) {
	if _, err := tm.db.Task().ObserveRevision(ctx, taskID, tm.memberName, revision); err != nil {
		logger.Error(logComponent).
			Str("task_id", taskID).Int64("revision", revision).Err(err).
			Msgf("Failed to record observed revision %d of task %s", revision, taskID)
	}
}

// GetOtherActiveTaskID 成员持有的另一条活跃任务（三活跃态），one-active-task
// 不变量的存在式探测。
// Python: TeamTaskManager.get_other_active_task_id (task_manager.py L1768-1790)
func (tm *TeamTaskManager) GetOtherActiveTaskID(ctx context.Context, memberName, excludeTaskID string) (string, error) {
	id, err := tm.db.Task().GetOtherActiveTaskID(ctx, tm.teamName, memberName, excludeTaskID)
	if err != nil {
		return "", err
	}
	if id == nil {
		return "", nil
	}
	return *id, nil
}

// StartTask 把 scheduled 已指派任务从 PENDING 推进到执行（F_62）。
// 指派（建队时）与执行开始是两个独立事件。入队门镜像 assign：BUILD_MODE owner
// 直接执行（IN_PROGRESS），PLAN_MODE owner 进计划闸（PLANNING，F_59 scheduled·plan
// 边）。CAS 前强制 one-active-task 不变量：owner 不得已持有另一条活跃任务。
// Python: TeamTaskManager.start_task (task_manager.py L1791-1853)
func (tm *TeamTaskManager) StartTask(ctx context.Context, taskID string) (schema.TaskOpResult, error) {
	// 1. 任务存在（Python: task = await self.get(task_id)）
	task, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if task == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("Task %s not found", taskID)), nil
	}
	// 2. 无 assignee 拒绝
	memberName := ""
	if task.Assignee != nil {
		memberName = *task.Assignee
	}
	if memberName == "" {
		return schema.TaskOpResult{}.Fail("Task " + taskID + " has no assignee; cannot start an unassigned task"), nil
	}
	// 3. 防御性 owner 校验（陈旧/手工行的护栏；常规 create/assign 路径更早校验）。
	// Python: owner_result = await self._validate_task_owner(member_name)
	ownerResult, err := tm.validateTaskOwner(ctx, memberName)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if !ownerResult.OK {
		return ownerResult, nil
	}
	// 3.1 plan gate 需要成员 mode（validateTaskOwner 只回答存在性/leader 排除）。
	// Python: member = await self.db.member.get_member(member_name, self.team_name)
	member, err := tm.db.Member().GetMember(ctx, memberName, tm.teamName)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if member == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("Member %s is not on team %s", memberName, tm.teamName)), nil
	}
	// 4. 入队门：plan-mode owner 落计划闸；F_128 恢复的父任务首轮已审批过
	planGate := member.Mode == "plan_mode" && !task.ResumePending
	entryStatus := fsm.TaskStatusInProgress
	if planGate {
		entryStatus = fsm.TaskStatusPlanning
	}
	// 5. 幂等重启动：已在其 owner 的入队门 → no-op
	if task.Status == entryStatus {
		logger.Debug(logComponent).Str("task_id", taskID).Str("member", memberName).
			Msg("Task already started by member; no-op")
		return schema.TaskOpResult{}.Success(), nil
	}
	// 6. 每成员一条活跃任务（PLANNING / IN_PROGRESS / IN_REVIEW）
	busyTaskID, err := tm.GetOtherActiveTaskID(ctx, memberName, taskID)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if busyTaskID != "" {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf(
			"Member %s already has an active task %s; finish it before starting %s",
			memberName, busyTaskID, taskID)), nil
	}
	// 7. CAS 启动
	started, err := tm.db.Task().StartTaskCAS(ctx, taskID, memberName, entryStatus)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if !started {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf(
			"Task %s cannot be started from status '%s' (only a pending task assigned to %s can start)",
			taskID, task.Status, memberName)), nil
	}
	// 8. 发布 TaskStartedEvent
	tm.publishTaskEvent(ctx, events.TaskStartedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           taskID,
		MemberName:       memberName,
	})
	return schema.TaskOpResult{}.Success(), nil
}

// validateTaskOwner 校验任务 owner 存在且不是 team leader。
// Python: TeamTaskManager._validate_task_owner (task_manager.py L167-181)
func (tm *TeamTaskManager) validateTaskOwner(ctx context.Context, memberName string) (schema.TaskOpResult, error) {
	owner := strings.TrimSpace(memberName)
	if owner == "" {
		return schema.TaskOpResult{}.Success(), nil
	}
	leaderMemberName := tm.resolveLeaderMemberName(ctx)
	if leaderMemberName != "" && owner == leaderMemberName {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf(
			"Member %s is the team leader and cannot be assigned team tasks; "+
				"assign the task to a non-leader member", owner)), nil
	}
	member, err := tm.db.Member().GetMember(ctx, owner, tm.teamName)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if member == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("Member %s not found in team %s", owner, tm.teamName)), nil
	}
	return schema.TaskOpResult{}.Success(), nil
}

// Reassign 原子换手：把已归属任务从当前 owner 移交给另一成员。
// **状态原位保留**——绝不弹回 PENDING，因此不产生伪 TASK_RELEASED、
// 不给 idle teammate 留可认领池竞态窗口。跨可改派三态（PENDING/BLOCKED/
// IN_PROGRESS）工作；尚未启动的已指派票（scheduled 模式的静止态）恰是
// 换手成本最低的时刻——没有在飞的推理可丢。恰好发两个定向事件：
// TASK_REVOKED 让前任 owner 从陌生任务上 steer 开，TASK_CLAIMED 告诉新
// owner 票已是他的。前任的其他认领与在飞轮次不受影响——只动这一张票。
// 新成员在换手**之前**校验，坏目标不会扰动任务本身。
// Python: TeamTaskManager.reassign (task_manager.py L1858-1920)
func (tm *TeamTaskManager) Reassign(ctx context.Context, taskID, newAssignee string) (schema.TaskOpResult, error) {
	// 1. 任务存在（Python: task = await self.get(task_id)）
	task, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if task == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("Task %s not found", taskID)), nil
	}

	// 2. 新 owner 校验（Python: owner_result = await self._validate_task_owner(new_assignee)）
	ownerResult, err := tm.validateTaskOwner(ctx, newAssignee)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if !ownerResult.OK {
		return ownerResult, nil
	}

	// 3. 无旧 owner 拒绝（Python: L1894）
	oldAssignee := ""
	if task.Assignee != nil {
		oldAssignee = *task.Assignee
	}
	if oldAssignee == "" {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("Task %s has no current assignee to reassign from", taskID)), nil
	}

	// 4. F_82：等子任务的 BLOCKED 挂起父任务算半成品，工作在当前 owner 手上
	//（F_128）——与两道闸不可换手同理。普通 BLOCKED 从未启动，可换手。
	// Python: L1898-1902（open_subtask_ids 非空则拒）
	if task.Status == fsm.TaskStatusBlocked {
		openSubtask, subErr := tm.hasOpenSubtask(ctx, taskID)
		if subErr != nil {
			return schema.TaskOpResult{}, subErr
		}
		if openSubtask {
			return schema.TaskOpResult{}.Fail(fmt.Sprintf(
				"Task %s is suspended while its subtasks run under %s; "+
					"wait for the subtasks to finish, or cancel them first", taskID, oldAssignee)), nil
		}
	}

	// 5. CAS 换手（Python: swapped = await self.db.task.reassign_task(...)）
	swapped, err := tm.db.Task().ReassignTask(ctx, taskID, oldAssignee, newAssignee)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if !swapped {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf(
			"Task %s could not be reassigned; it is either no longer held by "+
				"%s, or its status (%s) does not allow handing it over", taskID, oldAssignee, task.Status)), nil
	}

	// 6. 定向事件序：先 REVOKED(前任) 再 CLAIMED(新任)
	//（Python: L1910-1916 两连发）
	tm.publishTaskEvent(ctx, events.TaskRevokedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName, MemberName: oldAssignee},
		TaskID:           taskID,
	})
	tm.publishTaskEvent(ctx, events.TaskClaimedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName, MemberName: newAssignee},
		TaskID:           taskID,
	})
	return schema.TaskOpResult{}.Success(), nil
}

// hasOpenSubtask 是否存在非终态直接子任务（F_128）。
// Python: TeamTaskManager.open_subtask_ids (task_manager.py L1753-1756) 的存在式收窄。
func (tm *TeamTaskManager) hasOpenSubtask(ctx context.Context, taskID string) (bool, error) {
	subtasks, err := tm.db.Task().GetSubtasks(ctx, tm.teamName, taskID)
	if err != nil {
		return false, err
	}
	for _, sub := range subtasks {
		if !fsm.IsTaskTerminal(sub.Status) {
			return true, nil
		}
	}
	return false, nil
}

// GetClaimableTasks 获取可认领任务（PENDING + 无未解决依赖）。
// Python: TeamTaskManager.get_claimable_tasks()
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

// UnseenEdits 成员名下「改过但未读」的任务（F_129）。
// Python: unseen_edits(member_name) (task_manager.py L1748-1750)
func (tm *TeamTaskManager) UnseenEdits(ctx context.Context, memberName string) ([]*database.TeamTaskBase, error) {
	return tm.db.Task().GetUnseenEdits(ctx, tm.teamName, memberName)
}

// Claim 成员自认领。对齐 Python: TeamTaskManager.claim()
func (tm *TeamTaskManager) Claim(ctx context.Context, taskID string) (schema.TaskOpResult, error) {
	// 1. 检查任务是否存在（对齐 Python: task = await self.get(task_id)）
	task, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("查询任务失败: %s", err.Error())), nil
	}
	if task == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("任务 %s 不存在", taskID)), nil
	}

	// 2. 检查成员是否存在（对齐 Python: member = await self.db.member.get_member(...)）
	member, err := tm.db.Member().GetMember(ctx, tm.memberName, tm.teamName)
	if err != nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("查询成员失败: %s", err.Error())), nil
	}
	if member == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("成员 %s 在团队 %s 中不存在", tm.memberName, tm.teamName)), nil
	}

	// 3. PLAN_MODE 检查（对齐 Python: if member.mode == MemberMode.PLAN_MODE.value）
	if member.Mode == "plan_mode" {
		return schema.TaskOpResult{}.Fail("PLAN_MODE 成员必须先调用 submit_plan，leader 审批后任务从 planning 变为 in_progress"), nil
	}

	// 4. 幂等性检查（对齐 Python: if task.assignee == member_name and task.status == IN_PROGRESS）
	if task.Assignee != nil && *task.Assignee == tm.memberName && task.Status == fsm.TaskStatusInProgress {
		return schema.TaskOpResult{}.Success(), nil // 已认领，幂等返回成功
	}

	// 5. 已被他人认领检查（对齐 Python: if task.assignee）
	if task.Assignee != nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("任务 %s 已被 %s 认领，%s 无法认领", taskID, *task.Assignee, tm.memberName)), nil
	}

	// 6. FSM 状态转换合法性检查（对齐 Python: is_valid_transition(task.status, IN_PROGRESS, TASK_TRANSITIONS)）
	if !database.IsValidTaskTransition(task.Status, fsm.TaskStatusInProgress) {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("任务 %s 无法从状态 '%s' 认领（只有 pending 任务可认领）", taskID, task.Status)), nil
	}

	// 7. 执行认领（对齐 Python: success = await self.db.task.claim_task(task_id, member_name)）
	ok, err := tm.db.Task().ClaimTask(ctx, taskID, tm.memberName, fsm.TaskStatusInProgress)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if !ok {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("认领任务失败: 数据库拒绝认领 %s（可能存在并发认领竞争）", taskID)), nil
	}

	// 8. 事件发布（对齐 Python: await self.messager.publish(TaskClaimedEvent(...))）
	tm.publishTaskEvent(ctx, events.TaskClaimedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName, MemberName: tm.memberName},
		TaskID:           taskID,
	})
	return schema.TaskOpResult{}.Success(), nil
}

// Assign Leader 分配。对齐 Python: TeamTaskManager.assign()
func (tm *TeamTaskManager) Assign(ctx context.Context, taskID, assignee string) (schema.TaskOpResult, error) {
	// 1. 检查任务是否存在（对齐 Python: task = await self.get(task_id)）
	task, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("查询任务失败: %s", err.Error())), nil
	}
	if task == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("任务 %s 不存在", taskID)), nil
	}

	// 2. owner 校验（Python: owner_result = await self._validate_task_owner(assignee)，
	// task_manager.py L483——leader 不可被指派团队任务）
	ownerResult, err := tm.validateTaskOwner(ctx, assignee)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if !ownerResult.OK {
		return ownerResult, nil
	}

	// 3. 检查被分配者是否是团队成员（对齐 Python: member = await self.db.member.get_member(assignee, ...)）
	member, err := tm.db.Member().GetMember(ctx, assignee, tm.teamName)
	if err != nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("查询成员失败: %s", err.Error())), nil
	}
	if member == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("成员 %s 在团队 %s 中不存在", assignee, tm.teamName)), nil
	}

	// 3. 幂等性检查（对齐 Python: if task.assignee == assignee and task.status == IN_PROGRESS）
	if task.Assignee != nil && *task.Assignee == assignee && task.Status == fsm.TaskStatusInProgress {
		return schema.TaskOpResult{}.Success(), nil // 已分配给同一成员，幂等返回成功
	}

	// 4. 已被他人认领检查（对齐 Python: if task.assignee and task.assignee != assignee）
	if task.Assignee != nil && *task.Assignee != assignee {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("任务 %s 已被 %s 认领，需先 reset 再分配给 %s", taskID, *task.Assignee, assignee)), nil
	}

	// 5. 执行分配（对齐 Python: success = await self.db.task.claim_task(task_id, assignee)）
	ok, err := tm.db.Task().ClaimTask(ctx, taskID, assignee, fsm.TaskStatusInProgress)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if !ok {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("分配任务失败: 数据库拒绝分配 %s（状态转换无效: %s）", taskID, task.Status)), nil
	}

	// 6. 事件发布（对齐 Python: await self.messager.publish(TaskClaimedEvent(...))）
	tm.publishTaskEvent(ctx, events.TaskClaimedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName, MemberName: assignee},
		TaskID:           taskID,
	})
	return schema.TaskOpResult{}.Success(), nil
}

// Complete 完成任务。对齐 Python: TeamTaskManager.complete()
func (tm *TeamTaskManager) Complete(ctx context.Context, taskID string) (schema.TaskOpResult, error) {
	// 1. 检查成员是否存在（对齐 Python: member = await self.db.member.get_member(...)）
	member, err := tm.db.Member().GetMember(ctx, tm.memberName, tm.teamName)
	if err != nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("查询成员失败: %s", err.Error())), nil
	}
	if member == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("成员 %s 在团队 %s 中不存在", tm.memberName, tm.teamName)), nil
	}

	// 2. PLAN_MODE 状态检查（对齐 Python: if member.mode == MemberMode.PLAN_MODE.value）
	// 仅验证状态，plan index 写入移到 CompleteTask 之后（对齐 Python 执行顺序）
	var preCompleteTask *database.TeamTaskBase
	if member.Mode == "plan_mode" {
		preCompleteTask, err = tm.db.Task().GetTask(ctx, taskID)
		if err != nil {
			return schema.TaskOpResult{}.Fail(fmt.Sprintf("查询任务失败: %s", err.Error())), nil
		}
		if preCompleteTask == nil {
			return schema.TaskOpResult{}.Fail(fmt.Sprintf("任务 %s 不存在", taskID)), nil
		}
		// PLAN_MODE 成员只能完成 IN_PROGRESS 任务（对齐 Python：审批已把任务
		// 从 planning 推进到 in_progress；完成统一只从 in_progress 出发）
		if preCompleteTask.Status != fsm.TaskStatusInProgress {
			return schema.TaskOpResult{}.Fail(fmt.Sprintf("PLAN_MODE 成员无法完成状态为 '%s' 的任务 %s（只能完成 in_progress 任务）", preCompleteTask.Status, taskID)), nil
		}
	}

	// 2.5 verify-gate 入口（F_62）：完成的任务带验收人 → 进 IN_REVIEW 而非完成。
	// Python: complete_task 在任务带 reviewers 时走 submit_for_review 边并发布
	// TaskSubmittedForReviewEvent (events.py L403-411)。
	{
		gateTask, gateErr := tm.db.Task().GetTask(ctx, taskID)
		if gateErr == nil && gateTask != nil && gateTask.HasReviewers() {
			summary := ""
			note := ""
			submitted, submitErr := tm.db.Task().SubmitForReview(ctx, taskID, summary, note)
			if submitErr != nil {
				return schema.TaskOpResult{}, submitErr
			}
			if !submitted {
				return schema.TaskOpResult{}.Fail(fmt.Sprintf("任务 %s 提交评审失败（状态不允许）", taskID)), nil
			}
			logger.Info(logComponent).Str("task_id", taskID).
				Msg("Task submitted for review (has reviewers)")
			tm.publishTaskEvent(ctx, events.TaskSubmittedForReviewEvent{
				BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName, MemberName: tm.memberName},
				TaskID:           taskID,
				Reviewer:         gateTask.Reviewers(),
			})
			return schema.TaskOpResult{}.Success(), nil
		}
	}

	// 3. 执行完成（对齐 Python: result = await self.db.task.complete_task(task_id)）
	task, unblockedTasks, err := tm.db.Task().CompleteTask(ctx, taskID)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	// terminateTaskInSession: task=nil 表示不存在/FSM不合法，非 nil 表示成功（含幂等）
	if task == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("完成任务失败: 任务不存在或状态不允许完成 %s", taskID)), nil
	}

	// 4. PLAN_MODE 下更新 plan index 的完成状态（移到 CompleteTask 之后，对齐 Python 执行顺序）
	if member.Mode == "plan_mode" {
		planIndex, err := tm.loadPlanIndex()
		if err == nil && planIndex != nil {
			taskIdx, ok := planIndex.Tasks[taskID]
			if ok && taskIdx != nil {
				latestPlanID := ""
				if len(taskIdx.PlanIDs) > 0 {
					latestPlanID = taskIdx.PlanIDs[len(taskIdx.PlanIDs)-1]
				}
				nowISO := time.Now().Format(time.RFC3339)
				// Python: _write_task_plan_index(task_id, {task_id, plan_id, team_plan_id, member_name, status, completed_at, updated_at})
				assigneeName := ""
				if task.Assignee != nil {
					assigneeName = *task.Assignee
				}
				planRecord := &PlanRecord{
					PlanID:      latestPlanID,
					TaskID:      taskID,
					TeamPlanID:  tm.teamPlanID,
					MemberName:  assigneeName,
					Status:      string(fsm.TaskStatusCompleted),
					CompletedAt: nowISO,
					UpdatedAt:   nowISO,
				}
				if err := tm.writePlanIndex(planRecord); err != nil {
					logger.Warn(logComponent).Err(err).Str("task_id", taskID).Msg("PLAN_MODE completed: failed to update plan index")
				}
			}
		}
	}

	// 5. 事件发布（对齐 Python: await self._publish_task_event + _publish_unblocked_events + _maybe_publish_task_list_drained）
	// Python: TaskCompletedEvent(member_name=completed_task.assignee)
	assigneeName := ""
	if task.Assignee != nil {
		assigneeName = *task.Assignee
	}
	tm.publishTaskEvent(ctx, events.TaskCompletedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName, MemberName: assigneeName},
		TaskID:           taskID,
	})
	// unblockedTasks 是 []*TeamTaskBase，直接发布事件
	tm.publishUnblockedEvents(ctx, unblockedTasks)
	tm.maybePublishTaskListDrained(ctx)
	return schema.TaskOpResult{}.Success(), nil
}

// Cancel 取消单条任务。对齐 Python: TeamTaskManager.cancel()
func (tm *TeamTaskManager) Cancel(ctx context.Context, taskID string) ([]string, error) {
	task, unblockedTasks, err := tm.db.Task().CancelTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	// terminateTaskInSession: task=nil 表示不存在/FSM不合法，非 nil 表示成功（含幂等）
	if task == nil {
		return nil, fmt.Errorf("取消任务失败: 任务不存在或状态不允许取消 %s", taskID)
	}
	tm.publishTaskEvent(ctx, events.TaskCancelledEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           taskID,
	})
	// unblockedTasks 是 []*TeamTaskBase，直接发布事件
	tm.publishUnblockedEvents(ctx, unblockedTasks)
	tm.maybePublishTaskListDrained(ctx)
	// 返回 unblocked task ID 列表
	var refreshed []string
	for _, t := range unblockedTasks {
		refreshed = append(refreshed, t.TaskID)
	}
	return refreshed, nil
}

// CancelAllTasks 批量取消。对齐 Python: TeamTaskManager.cancel_all_tasks()
func (tm *TeamTaskManager) CancelAllTasks(ctx context.Context, skipAssignees []string) ([]*database.TeamTaskBase, error) {
	result, err := tm.db.Task().CancelAllTasks(ctx, tm.teamName, skipAssignees)
	if err != nil {
		return nil, err
	}
	for _, task := range result.Cancelled {
		tm.publishTaskEvent(ctx, events.TaskCancelledEvent{
			BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
			TaskID:           task.TaskID,
		})
	}
	// Python: await self._publish_unblocked_events(unblocked_tasks)
	tm.publishUnblockedEvents(ctx, result.Unblocked)
	tm.maybePublishTaskListDrained(ctx)
	return result.Cancelled, nil
}

// Reset 重置任务（CLAIMED→PENDING）。对齐 Python: TeamTaskManager.reset()
func (tm *TeamTaskManager) Reset(ctx context.Context, taskID string) (schema.TaskOpResult, error) {
	// Python: existing = await self.db.task.get_task(task_id)
	existing, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if existing == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("Task %s not found", taskID)), nil
	}

	ok, err := tm.db.Task().ResetTask(ctx, taskID)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if !ok {
		// Python: L1716-1719（只有 planning / in-progress / in-review 可 reset）
		return schema.TaskOpResult{}.Fail(fmt.Sprintf(
			"Task %s cannot be reset from status '%s'; "+
				"only planning / in-progress / in-review tasks can be reset", taskID, existing.Status)), nil
	}

	// 发布 TASK_RELEASED：idle teammate 得知票重新可认领（与 TASK_UNBLOCKED
	// 同款下游 nudge）。（Python: L1724-1727）
	logger.Info(logComponent).Str("task_id", taskID).Msg("Task reset successfully")
	tm.publishTaskEvent(ctx, events.TaskReleasedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           taskID,
	})
	return schema.TaskOpResult{}.Success(), nil
}

// UpdateTask 更新标题/内容并推进 revision。
//
// 对齐 Python: TeamTaskManager.update_task()——编辑使 revision += 1 并追加
// change_notes（F_129 / D8），让「改过但未读」成为可判定的常驻信号
// （revision > observed_revision），无需单独的未读标志。
//
// 上游 update_task 把 note 作为显式参数由调用方给出；Go 的 UpdateTask 签名
// 没有该参数，故从实际变更的字段推断（见 buildRevisionNote）——change_notes
// 至少有一行内容，下游 TaskRevisionTracker 才能渲染出 `- rN (by): note`。
func (tm *TeamTaskManager) UpdateTask(ctx context.Context, taskID, title, content string) error {
	ok, err := tm.db.Task().UpdateTask(ctx, taskID, title, content)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("更新任务失败: 任务不存在或状态不允许编辑 %s", taskID)
	}

	// 步骤：推进 revision + 追加 change_notes（上游在 update_task 内原子完成）
	old, getErr := tm.db.Task().GetTask(ctx, taskID)
	if getErr == nil && old != nil {
		note := buildRevisionNote(title, content)
		if _, bumpErr := tm.db.Task().AppendChangeNote(ctx, taskID, old.Revision+1, note, tm.memberName); bumpErr != nil {
			// 编辑本身已成功，bump 失败只告警：不能因为通知基线写失败把一次
			// 成功的编辑报成失败（上游 update_task 在同事务内，Go 的 DAO 拆分
			// 导致此处非原子，故降级为告警）。
			logger.Warn(logComponent).Err(bumpErr).Str("task_id", taskID).
				Msg("append change note failed")
		}
	}

	tm.publishTaskEvent(ctx, events.TaskUpdatedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           taskID,
	})
	return nil
}

// buildRevisionNote 从未被改动的字段生成一行编辑说明。
//
// 上游 update_task 的 note 是调用方显式传入的（工具层带 note 参数）；Go 的
// UpdateTask 没有该参数，故从实际变更的字段推断，保证 change_notes 至少
// 有一行可渲染内容——否则 TaskRevisionTracker 的通知只剩任务行、没有说明。
func buildRevisionNote(title, content string) string {
	switch {
	case title != "" && content != "":
		return "更新标题与正文"
	case title != "":
		return "更新标题"
	case content != "":
		return "更新正文"
	default:
		return "更新任务"
	}
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
		tm.publishTaskEvent(ctx, events.TaskCreatedEvent{
			BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
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
	tm.publishTaskEvent(ctx, events.TaskCreatedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
		TaskID:           created.TaskID,
		Status:           created.Status,
	})
	return created, nil
}

// AddDependencies 向已有任务添加依赖。对齐 Python: TeamTaskManager.add_dependencies()
func (tm *TeamTaskManager) AddDependencies(ctx context.Context, taskID string, dependsOnIDs []string) (schema.TaskOpResult, error) {
	var edges []database.EdgeSpec
	for _, upstreamID := range dependsOnIDs {
		edges = append(edges, database.EdgeSpec{TaskID: taskID, DependsOnID: upstreamID})
	}
	result := tm.db.Task().MutateDependencyGraph(ctx, tm.teamName, nil, edges)
	if !result.Ok {
		return schema.TaskOpResult{}.Fail(result.Reason), nil
	}
	return schema.TaskOpResult{}.Success(), nil
}

// GetTaskDetail 详细视图（含 blocked_by + blocks）。
// Python: TeamTaskManager.get_task_detail()
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
func (tm *TeamTaskManager) ListTasksWithDeps(ctx context.Context, status ...string) ([]*TaskSummary, error) {
	statusFilter := ""
	if len(status) > 0 {
		statusFilter = status[0]
	}
	tasks, err := tm.db.Task().GetTeamTasks(ctx, tm.teamName, statusFilter)
	if err != nil {
		return nil, err
	}
	var result []*TaskSummary
	for _, task := range tasks {
		deps, _ := tm.db.Task().GetTaskDependencies(ctx, task.TaskID)
		var blockedBy []string
		for _, dep := range deps {
			// Python: [d.depends_on_task_id for d in deps if not d.resolved]
			if !dep.Resolved {
				blockedBy = append(blockedBy, dep.DependsOnID)
			}
		}
		assignee := ""
		if task.Assignee != nil {
			assignee = *task.Assignee
		}
		result = append(result, &TaskSummary{
			TaskID:    task.TaskID,
			Title:     task.Title,
			Status:    task.Status,
			Assignee:  assignee,
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
	// 对齐 Python: PENDING（首次提交，下方预留进 PLANNING）/ PLANNING（拒绝后
	// 重提，保持 PLANNING）/ IN_PROGRESS（scheduled owner 对已开始任务补交计划）
	if task.Status != fsm.TaskStatusPending && task.Status != fsm.TaskStatusPlanning && task.Status != fsm.TaskStatusInProgress {
		return nil, fmt.Errorf("任务状态不允许提交计划: %s (当前: %s)", taskID, task.Status)
	}
	if task.Status != fsm.TaskStatusPending && (task.Assignee == nil || *task.Assignee != tm.memberName) {
		assigneeName := ""
		if task.Assignee != nil {
			assigneeName = *task.Assignee
		}
		return nil, fmt.Errorf("任务 %s 已被 %s 认领，当前成员无法提交计划", taskID, assigneeName)
	}

	// 3. 如果 PENDING → 预留进 PLANNING（对齐 Python: claim_task(..., to_status=PLANNING)）
	if task.Status == fsm.TaskStatusPending {
		ok, _ := tm.db.Task().ClaimTask(ctx, taskID, tm.memberName, fsm.TaskStatusPlanning)
		if !ok {
			return nil, fmt.Errorf("提交计划前认领任务失败: %s", taskID)
		}
	}

	// 4. 生成 plan_id（加入纳秒避免同一毫秒内重复提交时 ID 冲突）
	planID := fmt.Sprintf("plan_%s_%d_%d", taskID, time.Now().UnixMilli(), time.Now().UnixNano()%1000)

	// 5. 检查 plan_id 是否已存在（对齐 Python L938: if self._read_plan_index(plan_id): return failure）
	planIndex, _ := tm.loadPlanIndex()
	if planIndex != nil {
		if _, exists := planIndex.TaskPlans[planID]; exists {
			return nil, fmt.Errorf("计划已存在: %s", planID)
		}
	}

	// 6. 拷贝 plan 文件到 plansDir
	planDir := filepath.Join(tm.plansDir, tm.teamPlanID, "tasks", taskID, "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建计划目录失败: %v", err)
	}
	destPath := filepath.Join(planDir, planID+".md")
	if planFilePath != "" {
		// M-08: 路径验证对齐 Python: _resolve_submitted_plan_path
		resolvedPath, err := resolveSubmittedPlanPath(planFilePath)
		if err != nil {
			return nil, fmt.Errorf("解析计划文件路径失败: %v", err)
		}
		// 跳过源和目标相同的拷贝（对齐 Python: if submitted_plan_path != member_plan_path.resolve()）
		absDest, _ := filepath.Abs(destPath)
		if resolvedPath == absDest {
			// 源和目标相同，跳过拷贝
		} else if err := copyFileWithMeta(resolvedPath, destPath); err != nil {
			return nil, fmt.Errorf("拷贝计划文件失败: %v", err)
		}
	}

	// 7. 写入 index.json
	nowISO := time.Now().Format(time.RFC3339)
	record := &PlanRecord{
		PlanID:       planID,
		TaskID:       taskID,
		TeamPlanID:   tm.teamPlanID,
		MemberName:   tm.memberName,
		Status:       fsm.TaskStatusPlanning,
		MemberPlanMD: destPath,
		Decision:     "pending",
		SubmittedAt:  nowISO,
		UpdatedAt:    nowISO,
	}
	// M-08: 记录源计划文件路径（对齐 Python: source_plan_path）
	if planFilePath != "" {
		if resolvedPath, err := resolveSubmittedPlanPath(planFilePath); err == nil {
			record.SourcePlanPath = resolvedPath
		}
	}
	if err := tm.writePlanIndex(record); err != nil {
		return nil, fmt.Errorf("写入计划索引失败: %v", err)
	}

	// 8. 发布事件
	tm.publishTaskEvent(ctx, events.TaskPlanRequestEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName, MemberName: tm.memberName},
		TaskID:           taskID,
		Status:           fsm.TaskStatusPlanning,
		PlanID:           planID,
		MemberPlanMD:     destPath,
		ToolCallID:       toolCallID,
	})

	// 9. 对齐 Python: _notify_leader_of_plan — 通过 TeamMessageManager 通知 leader
	leaderMessageID := tm.notifyLeaderOfPlan(ctx, record, destPath, toolCallID)
	if leaderMessageID != "" {
		// Python: _write_task_plan_index(task_id, {"plan_id": ..., "leader_message_id": ..., "updated_at": ...})
		record.LeaderMessageID = leaderMessageID
		_ = tm.writePlanIndex(record)
	}

	return record, nil
}

// ApprovePlan PLAN_MODE 审批/拒绝计划。对齐 Python: TeamTaskManager.approve_plan()
func (tm *TeamTaskManager) ApprovePlan(ctx context.Context, planID string, approved bool, feedback string) (schema.TaskOpResult, error) {
	// 1. 加载 index.json
	index, err := tm.loadPlanIndex()
	if err != nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("加载计划索引失败: %v", err)), nil
	}

	// 2. 校验 planID 存在
	planRecord, planExists := index.TaskPlans[planID]
	if !planExists {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("计划不存在: %s", planID)), nil
	}

	// 3. 校验 task 状态
	task, _ := tm.db.Task().GetTask(ctx, planRecord.TaskID)
	if task == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("任务不存在: %s", planRecord.TaskID)), nil
	}
	if task.Status != fsm.TaskStatusPlanning {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("任务状态应为 PLANNING: 当前 %s", task.Status)), nil
	}
	// Python: if not existing.assignee: return TaskOpResult.fail(f"Task {task_id} has no assignee")
	if task.Assignee == nil || *task.Assignee == "" {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("任务 %s 没有 assignee", planRecord.TaskID)), nil
	}

	// 4. 校验 planID 是 latest_plan_id
	taskPlanIdx, idxExists := index.Tasks[planRecord.TaskID]
	if idxExists && taskPlanIdx.LatestPlanID != planID {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("不能审批过期计划: latest=%s, current=%s", taskPlanIdx.LatestPlanID, planID)), nil
	}

	// 5. 校验 decision 为 pending
	if planRecord.Decision != "pending" {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("计划已审批: decision=%s", planRecord.Decision)), nil
	}

	// 6. 校验 plan 文件物理存在
	planDir := filepath.Join(tm.plansDir, tm.teamPlanID, "tasks", planRecord.TaskID, "plans")
	planFile := filepath.Join(planDir, planID+".md")
	if _, err := os.Stat(planFile); os.IsNotExist(err) {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("计划文件不存在: %s", planFile)), nil
	}

	if approved {
		// 审批通过：PLANNING→IN_PROGRESS
		ok, _ := tm.db.Task().ApprovePlanTask(ctx, planRecord.TaskID)
		if !ok {
			return schema.TaskOpResult{}.Fail(fmt.Sprintf("审批 FSM 转换失败: %s", planRecord.TaskID)), nil
		}
		planRecord.Decision = "approve"
		planRecord.Status = fsm.TaskStatusInProgress
	} else {
		// 审批拒绝：任务保持 PLANNING（返工自环）
		planRecord.Decision = "reject"
		planRecord.Feedback = feedback
		planRecord.Status = fsm.TaskStatusPlanning
	}
	planRecord.DecidedAt = time.Now().Format(time.RFC3339)
	planRecord.UpdatedAt = time.Now().Format(time.RFC3339)

	// 更新 index.json
	if err := tm.updatePlanIndex(planID, planRecord); err != nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("更新计划索引失败: %v", err)), nil
	}

	// 发布事件
	// S-附录4: 对齐 Python member_name=task.assignee（被审批成员）——
	// 原误用 tm.memberName（审批发起人 leader 自己），导致接收方
	// OnTaskPlanDecision 判定"不是给自己的"而错误转发
	tm.publishTaskEvent(ctx, events.TaskPlanResponseEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName, MemberName: *task.Assignee},
		TaskID:           planRecord.TaskID,
		Approved:         approved,
		Status:           planRecord.Status,
		PlanID:           planID,
		Feedback:         feedback,
	})

	return schema.TaskOpResult{}.Success(), nil
}

// notifyLeaderOfPlan 通过 TeamMessageManager 通知 leader 审批计划。
// 返回 leader_message_id（对齐 Python: _notify_leader_of_plan → leader_message_id）。
// Python: TeamTaskManager._notify_leader_of_plan()
func (tm *TeamTaskManager) notifyLeaderOfPlan(ctx context.Context, record *PlanRecord, planFilePath string, toolCallID string) string {
	leaderName := tm.resolveLeaderMemberName(ctx)
	if leaderName == "" {
		logger.Warn(logComponent).
			Str("team", tm.teamName).
			Str("task_id", record.TaskID).
			Str("plan_id", record.PlanID).
			Msg("notifyLeaderOfPlan: cannot notify leader, leader_member_name is empty")
		return ""
	}
	// Python: leader_member_name == self.member_name → 跳过
	if leaderName == tm.memberName {
		return ""
	}

	content := renderPlanReviewMessage(record.MemberName, record.TaskID, record.PlanID, planFilePath, toolCallID)

	// M-09: 改用 TeamMessageManager.SendMessage 对齐 Python
	// Python: message_manager = TeamMessageManager(db, team_name, member_name, messager)
	// Python: message_id = await message_manager.send_message(content=..., to_member_name=...)
	if tm.messageManager != nil {
		messageID, err := tm.messageManager.SendMessage(ctx, content, leaderName, tm.memberName)
		if err != nil {
			logger.Error(logComponent).
				Str("leader", leaderName).
				Str("task_id", record.TaskID).
				Str("plan_id", record.PlanID).
				Err(err).
				Msg("notifyLeaderOfPlan: SendMessage failed")
			return ""
		}
		logger.Info(logComponent).
			Str("leader", leaderName).
			Str("task_id", record.TaskID).
			Str("plan_id", record.PlanID).
			Str("message_id", messageID).
			Msg("notifyLeaderOfPlan: leader notified to review plan")
		return messageID
	}

	// 降级：messageManager 不可用时回退到 messager.Send（保持向后兼容）
	if tm.messager == nil {
		return ""
	}
	logger.Warn(logComponent).Msg("notifyLeaderOfPlan: messageManager unavailable, falling back to messager.Send")
	msg := events.EventMessageFromEvent(events.MessageEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
		MessageID:        fmt.Sprintf("plan_notify_%s_%d", record.PlanID, time.Now().UnixMilli()),
		FromMemberName:   tm.memberName,
		ToMemberName:     leaderName,
	})
	if msg.Payload == nil {
		msg.Payload = make(map[string]any)
	}
	msg.Payload["content"] = content
	if err := tm.messager.Send(ctx, leaderName, msg); err != nil {
		logger.Warn(logComponent).
			Str("leader", leaderName).
			Err(err).
			Msg("notifyLeaderOfPlan: messager.Send failed")
	}
	return ""
}

// resolveLeaderMemberName 解析 leader 成员名。
// Python: TeamTaskManager._resolve_leader_member_name()
func (tm *TeamTaskManager) resolveLeaderMemberName(ctx context.Context) string {
	if tm.leaderMemberName != "" {
		return tm.leaderMemberName
	}
	// Python: 从 db.team.get_team 获取 leader_member_name
	team, err := tm.db.Team().GetTeam(ctx, tm.teamName)
	if err != nil || team == nil {
		return ""
	}
	name := team.LeaderMemberName
	if name != "" {
		tm.leaderMemberName = name
	}
	return name
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// renderPlanReviewMessage 渲染计划审批消息。
// Python: TeamTaskManager._render_plan_review_message()
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

// resolveSubmittedPlanPath 解析并验证提交的计划文件路径。
// 对齐 Python: TeamTaskManager._resolve_submitted_plan_path(plan_path)
// 执行：ExpandHome → filepath.Abs → filepath.EvalSymlinks → os.Stat 文件存在性检查。
func resolveSubmittedPlanPath(planPath string) (string, error) {
	rawPath := strings.TrimSpace(planPath)
	if rawPath == "" {
		return "", fmt.Errorf("submit_plan requires plan_path")
	}

	// 1. ExpandHome（对齐 Python: Path(raw_path).expanduser()）
	expanded := pathutil.ExpandHome(rawPath)

	// 2. 绝对化 + 解析符号链接（对齐 Python: submitted_path.resolve() 的 strict=False 语义；
	//    公共助手内部已含 Abs，无需再单独 Abs 一次）
	resolved := pathutil.ResolveSymlinksLenient(expanded)

	// 3. 文件存在性检查（对齐 Python: if not submitted_path.is_file()）
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("计划文件不存在: %s", resolved)
		}
		return "", fmt.Errorf("访问计划文件失败: %v", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("计划路径是目录而非文件: %s", resolved)
	}

	return resolved, nil
}

// copyFileWithMeta 拷贝文件并保留原始权限和修改时间。
// 对齐 Python: shutil.copy2(src, dst) — 保留权限 + mtime + atime。
func copyFileWithMeta(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("获取源文件信息失败: %v", err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %v", err)
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %v", err)
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("拷贝文件内容失败: %v", err)
	}

	// 恢复修改时间（对齐 Python: shutil.copystat → os.utime）
	if err := os.Chtimes(dst, info.ModTime(), info.ModTime()); err != nil {
		// 修改时间恢复失败不阻断流程，仅记录日志
		logger.Warn(logComponent).
			Str("dst", dst).
			Err(err).
			Msg("copyFileWithMeta: failed to restore modification time")
	}

	return nil
}

// publishTaskEvent 发布任务事件到 TeamTopic。
// Python: TeamTaskManager._publish_task_event()
// sessionID 从 context 中获取（schema.GetSessionID(ctx)），对齐 Python: get_session_id()。
func (tm *TeamTaskManager) publishTaskEvent(ctx context.Context, event events.TypedEvent) {
	if tm.messager == nil {
		return
	}
	msg := events.EventMessageFromEvent(event)
	topicID := events.TeamTopicTask.Build(schema.GetSessionID(ctx), tm.teamName)
	if err := tm.messager.Publish(ctx, topicID, msg); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", event.EventTypeName()).
			Msg("failed to publish task event")
	}
}

// publishUnblockedEvents 逐条发布 TaskUnblockedEvent。
// Python: TeamTaskManager._publish_unblocked_events()
func (tm *TeamTaskManager) publishUnblockedEvents(ctx context.Context, unblockedTasks []*database.TeamTaskBase) {
	for _, t := range unblockedTasks {
		tm.publishTaskEvent(ctx, events.TaskUnblockedEvent{
			BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
			TaskID:           t.TaskID,
		})
	}
}

// maybePublishTaskListDrained 检查是否所有任务已终态，如果是则发布 TaskListDrainedEvent。
// Python: TeamTaskManager._maybe_publish_task_list_drained()
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
		tm.publishTaskEvent(ctx, events.TaskListDrainedEvent{
			BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
			TaskCount:        len(tasks),
		})
	}
}

// checkTaskListDrained 检查所有任务是否均处于终态。
// Python: TeamTaskManager._check_task_list_drained()
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
// Python: for refreshed in mutation.refreshed_tasks: if refreshed.task_id == task_id
func findRefreshedTask(refreshed []*database.TeamTaskBase, taskID string) *database.TeamTaskBase {
	for _, t := range refreshed {
		if t.TaskID == taskID {
			return t
		}
	}
	return nil
}

// generateTaskID 生成任务 ID，对齐 Python: uuid.uuid4()。
func generateTaskID(teamName string, suffixes ...string) string {
	base := "task_" + teamName + "_" + uuid.New().String()
	if len(suffixes) > 0 {
		base += "_" + strings.Join(suffixes, "_")
	}
	return base
}

// ──────────────────────────── review 面（25.2b F_62） ────────────────────────────

// ResolveReviewerType 从任务的结构化验收人列表查一人的类型。
// Python: TeamTaskManager._resolve_reviewer_type（task_manager.py L872-879）。
func ResolveReviewerType(task *database.TeamTaskBase, reviewerName string) string {
	for _, detail := range task.ReviewerDetails() {
		if detail["reviewer_id"] == reviewerName {
			if typ := detail["type"]; typ != "" {
				return typ
			}
			return "verifier"
		}
	}
	return "verifier"
}

// VerifyTask 验收门任务的评审人判定（reviewer only）。
// 判定策略取决于团队生效的 dispatch mode：
//   - autonomous：首个判定生效——pass 完成任务（IN_REVIEW→COMPLETED，解除
//     下游阻塞），fail 退回作者（IN_REVIEW→IN_PROGRESS 返工回路）——没有
//     judge 组件计票。
//   - scheduled（F_62）：只记录票事实（追加行）并发布 TASK_REVIEW_VOTE；
//     任务保持 IN_REVIEW，leader 侧调度器计票并用 settle_review 结算。
//     同一 reviewer 重复调用替换其前票（最新行胜出）。
//
// 两种模式守卫相同：任务 IN_REVIEW 且调用者在其验收人列表（非作者）。
//
// Python: TeamTaskManager.verify_task (task_manager.py L769-857)。
func (tm *TeamTaskManager) VerifyTask(ctx context.Context, taskID, decision, feedback string) (schema.TaskOpResult, error) {
	normalized := strings.TrimSpace(strings.ToLower(decision))

	task, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if task == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("Task %s not found", taskID)), nil
	}
	if task.Status != fsm.TaskStatusInReview {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("Task %s is not under review (status '%s'); nothing to verify", taskID, task.Status)), nil
	}
	reviewers := task.Reviewers()
	isReviewer := false
	for _, name := range reviewers {
		if name == tm.memberName {
			isReviewer = true
			break
		}
	}
	if !isReviewer {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("%s is not a reviewer of task %s; cannot verify it", tm.memberName, taskID)), nil
	}
	if tm.memberName == derefString(task.Assignee) {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("%s cannot verify their own task %s", tm.memberName, taskID)), nil
	}

	// 从任务的结构化验收人列表解析调用者类型：inspector 票必须校验为浮点分。
	rt := ResolveReviewerType(task, tm.memberName)
	if rt == "inspector" {
		score, convErr := strconv.ParseFloat(normalized, 64)
		if convErr != nil {
			return schema.TaskOpResult{}.Fail(fmt.Sprintf("verify_task decision must be a number for inspector reviewer, got '%s'", decision)), nil
		}
		if score < 0.0 || score > 1.0 {
			return schema.TaskOpResult{}.Fail(fmt.Sprintf("verify_task decision must be a number in [0.0, 1.0], got '%s'", decision)), nil
		}
		normalized = fmt.Sprintf("%.2f", score)
	} else if normalized != "pass" && normalized != "fail" {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("verify_task decision must be 'pass' or 'fail', got '%s'", decision)), nil
	}

	logger.Info(logComponent).Str("reviewer", tm.memberName).Str("task", taskID).
		Str("decision", normalized).Msg("[verify_task]")

	if tm.dispatchMode == "scheduled" {
		return tm.recordReviewVote(ctx, task, normalized, feedback)
	}
	if normalized == "pass" {
		return tm.verifyPass(ctx, task)
	}
	return tm.verifyFail(ctx, task, feedback)
}

// recordReviewVote 持久化一条票并发布计票快照（scheduled dispatch）。
// 票事实按任务当前 review_round 追加；计票取每 reviewer 最新行——并发 reviewer
// 从不争用，改票即普通插入。此处不做状态迁移——计票决出后由调度器结算。
//
// Python: _record_review_vote (task_manager.py L859-884)。
func (tm *TeamTaskManager) recordReviewVote(ctx context.Context, task *database.TeamTaskBase, decision, feedback string) (schema.TaskOpResult, error) {
	var feedbackPtr *string
	if feedback != "" {
		feedbackPtr = &feedback
	}
	if err := tm.db.Task().InsertReviewVote(ctx, tm.teamName, task.TaskID, task.ReviewRound, tm.memberName, decision, feedbackPtr); err != nil {
		return schema.TaskOpResult{}, err
	}
	tally := tm.GetReviewTally(task)
	assignee := derefString(task.Assignee)
	tm.publishTaskEvent(ctx, events.TaskReviewVoteEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName, MemberName: assignee},
		TaskID:           task.TaskID,
		Reviewer:         tm.memberName,
		Decision:         decision,
		ReviewRound:      task.ReviewRound,
		PassCount:        tally["pass_count"].(int),
		FailCount:        tally["fail_count"].(int),
		ReviewerCount:    tally["reviewer_count"].(int),
	})
	logger.Info(logComponent).Str("reviewer", tm.memberName).Str("task", task.TaskID).
		Str("decision", decision).Int64("round", task.ReviewRound).
		Int("pass", tally["pass_count"].(int)).Int("fail", tally["fail_count"].(int)).
		Int("of", tally["reviewer_count"].(int)).
		Msg("[verify_vote]")
	return schema.TaskOpResult{}.SuccessWithData(map[string]any(tally)), nil
}

// GetReviewTally 一条 IN_REVIEW 任务当前轮的票计快照。
// 按类型分组：verifier 和 challenger 构成二元（pass/fail）池；inspector 票是
// 0~1 分数。每 reviewer 最新票胜出；不在验收人列表内的票被忽略。
//
// Python: get_review_tally (task_manager.py L908-993)。
func (tm *TeamTaskManager) GetReviewTally(task *database.TeamTaskBase) map[string]any {
	votes, err := tm.db.Task().GetReviewVotes(context.Background(), task.TaskID, task.ReviewRound)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("task_id", task.TaskID).Msg("review votes query failed")
		votes = nil
	}
	reviewers := task.Reviewers()
	// 结构化验收人列表 → reviewer_id→type 映射。
	typeMap := map[string]string{}
	for _, detail := range task.ReviewerDetails() {
		rid := detail["reviewer_id"]
		if rid != "" {
			typ := detail["type"]
			if typ == "" {
				typ = "verifier"
			}
			typeMap[rid] = typ
		}
	}

	latestDecision := map[string]string{}
	latestFeedback := map[string]string{}
	for _, vote := range votes {
		inList := false
		for _, name := range reviewers {
			if name == vote.Reviewer {
				inList = true
				break
			}
		}
		if !inList {
			continue
		}
		// 升序遍历 + 覆盖 = 每 reviewer 最新票。
		latestDecision[vote.Reviewer] = vote.Decision
		if vote.Feedback != nil {
			latestFeedback[vote.Reviewer] = *vote.Feedback
		} else {
			latestFeedback[vote.Reviewer] = ""
		}
	}

	// 二元池：verifier + challenger（pass/fail 票）。
	verdictPass, verdictFail, verdictTotal, verdictVoted := 0, 0, 0, 0
	failFeedback := map[string]string{}
	// inspector 池：分数票。
	inspectorScores := map[string]float64{}
	inspectorCount, inspectorVoted := 0, 0

	for _, name := range reviewers {
		rt := typeMap[name]
		if rt == "" {
			rt = "verifier"
		}
		if rt == "inspector" {
			inspectorCount++
			if decision, voted := latestDecision[name]; voted {
				inspectorVoted++
				if score, convErr := strconv.ParseFloat(decision, 64); convErr == nil {
					inspectorScores[name] = score
				}
			}
		} else {
			verdictTotal++
			if decision, voted := latestDecision[name]; voted {
				verdictVoted++
				if decision == "pass" {
					verdictPass++
				} else if decision == "fail" {
					verdictFail++
					if feedback := latestFeedback[name]; feedback != "" {
						failFeedback[name] = feedback
					}
				}
			}
		}
	}

	var inspectorAvg any
	if len(inspectorScores) > 0 {
		sum := 0.0
		for _, score := range inspectorScores {
			sum += score
		}
		inspectorAvg = sum / float64(len(inspectorScores))
	}

	return map[string]any{
		"task_id":            task.TaskID,
		"review_round":       task.ReviewRound,
		"pass_count":         verdictPass,
		"fail_count":         verdictFail,
		"reviewer_count":     len(reviewers),
		"voted":              sortedKeysOf(latestDecision),
		"fail_feedback":      failFeedback,
		"verdict_pass_count": verdictPass,
		"verdict_fail_count": verdictFail,
		"verdict_total":      verdictTotal,
		"verdict_voted":      verdictVoted,
		"inspector_count":    inspectorCount,
		"inspector_voted":    inspectorVoted,
		"inspector_scores":   inspectorScores,
		"inspector_avg":      inspectorAvg,
	}
}

// SettleReview 框架侧结算一评委轮（F_62）。
// 由 leader 侧调度器在票计决出后调用：pass 完成任务（IN_REVIEW→COMPLETED，
// 解除依赖、发布 TASK_VERIFIED），fail 退回返工（IN_REVIEW→IN_PROGRESS，
// 发布带汇总反馈的 TASK_REVISION_REQUESTED）。与 verify_task 不同——没有
// 验收人成员守卫：调用方是调度运行时，不是成员。DAO 状态 CAS 防止重复结算。
//
// Python: settle_review (task_manager.py L995-1080)。
func (tm *TeamTaskManager) SettleReview(ctx context.Context, taskID, decision, feedback string) (schema.TaskOpResult, error) {
	normalized := strings.TrimSpace(strings.ToLower(decision))
	if normalized != "pass" && normalized != "fail" {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("settle_review decision must be 'pass' or 'fail', got '%s'", decision)), nil
	}

	task, err := tm.db.Task().GetTask(ctx, taskID)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if task == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("Task %s not found", taskID)), nil
	}
	if task.Status != fsm.TaskStatusInReview {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("Task %s is not under review (status '%s'); nothing to settle", taskID, task.Status)), nil
	}

	if normalized == "pass" {
		return tm.verifyPass(ctx, task)
	}
	return tm.verifyFail(ctx, task, feedback)
}

// verifyPass 判定通过：IN_REVIEW → COMPLETED，解除下游阻塞。
// 复用 CompleteTask（含依赖解除 + 级联）——IN_REVIEW→COMPLETED 是合法终态边，
// 源状态已由 verify_task/settle_review 守卫为 IN_REVIEW。
//
// Python: _verify_pass (task_manager.py L1021-1048)。
func (tm *TeamTaskManager) verifyPass(ctx context.Context, task *database.TeamTaskBase) (schema.TaskOpResult, error) {
	completedTask, unblockedTasks, err := tm.db.Task().CompleteTask(ctx, task.TaskID)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if completedTask == nil {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("Task %s could not be completed from review", task.TaskID)), nil
	}
	logger.Info(logComponent).Str("task_id", task.TaskID).Msg("Task verified (passed) and completed")
	assignee := derefString(completedTask.Assignee)
	tm.publishTaskEvent(ctx, events.TaskVerifiedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName, MemberName: assignee},
		TaskID:           task.TaskID,
	})
	tm.publishUnblockedEvents(ctx, unblockedTasks)
	tm.maybePublishTaskListDrained(ctx)
	return schema.TaskOpResult{}.Success(), nil
}

// verifyFail 判定不通过：IN_REVIEW → IN_PROGRESS，返工回路到作者。
//
// Python: _verify_fail (task_manager.py L1050-1071)。
func (tm *TeamTaskManager) verifyFail(ctx context.Context, task *database.TeamTaskBase, feedback string) (schema.TaskOpResult, error) {
	revised, err := tm.db.Task().ReviseTask(ctx, task.TaskID)
	if err != nil {
		return schema.TaskOpResult{}, err
	}
	if !revised {
		return schema.TaskOpResult{}.Fail(fmt.Sprintf("Task %s could not be sent back for revision", task.TaskID)), nil
	}
	logger.Info(logComponent).Str("task_id", task.TaskID).Str("assignee", derefString(task.Assignee)).
		Msg("Task verification failed; sent back for revision")
	tm.publishTaskEvent(ctx, events.TaskRevisionRequestedEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName, MemberName: derefString(task.Assignee)},
		TaskID:           task.TaskID,
		Feedback:         feedback,
	})
	return schema.TaskOpResult{}.Success(), nil
}

// GetPreviousRoundVote 返回某 reviewer 在当前轮之前一轮的最新票。
// 临时 reviewer 每轮都以全新上下文运行——这张票是它对上一轮要求返工内容的
// 唯一记忆。首轮或该 reviewer 未投时返回 nil。
//
// Python: get_previous_round_vote (task_manager.py L885-906)。
func (tm *TeamTaskManager) GetPreviousRoundVote(ctx context.Context, task *database.TeamTaskBase, reviewer string) (*database.TeamTaskReviewVoteBase, error) {
	if task.ReviewRound <= 1 {
		return nil, nil
	}
	votes, err := tm.db.Task().GetReviewVotes(ctx, task.TaskID, task.ReviewRound-1)
	if err != nil {
		return nil, err
	}
	var latest *database.TeamTaskReviewVoteBase
	for _, vote := range votes {
		if vote.Reviewer == reviewer {
			latest = vote
		}
	}
	return latest, nil
}

// GetReviewTasks 返回验收人列表包含 reviewer_name 的 IN_REVIEW 任务。
// 取状态索引的 IN_REVIEW 行再在内存过滤验收人成员资格（v1 验收是团队级，
// 规范化的 reviewer 连接表为时过早，见 F_59）。任务作者不会把任务返给自己审。
//
// Python: get_review_tasks (task_manager.py L1073-1080)。
func (tm *TeamTaskManager) GetReviewTasks(ctx context.Context, reviewerName string) ([]*database.TeamTaskBase, error) {
	inReview, err := tm.db.Task().GetTeamTasks(ctx, tm.teamName, fsm.TaskStatusInReview)
	if err != nil {
		return nil, err
	}
	var result []*database.TeamTaskBase
	for _, task := range inReview {
		for _, name := range task.Reviewers() {
			if name == reviewerName && derefString(task.Assignee) != reviewerName {
				result = append(result, task)
				break
			}
		}
	}
	return result, nil
}

// readDispatchMode 从团队行读 dispatch mode；任何失败（测试替身 nil DAO、
// 读库报错）静默回落空串 = autonomous。
func readDispatchMode(db database.TeamDatabase, teamName string) string {
	defer func() {
		if r := recover(); r != nil {
			// 测试替身的嵌入 nil 接口调用会 panic——按缺省处理。
			_ = r
		}
	}()
	if db == nil {
		return ""
	}
	teamDao := db.Team()
	if teamDao == nil {
		return ""
	}
	team, err := teamDao.GetTeam(context.Background(), teamName)
	if err != nil || team == nil {
		return ""
	}
	return team.DispatchMode
}

// sortedKeysOf 键排序助手（voted 列表确定性输出）。
func sortedKeysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// derefString 字符串指针解引用（nil → ""）。
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// TaskDB 返回任务 DAO（scheduling 半区读取 task_links / upstream 简报）。
func (tm *TeamTaskManager) TaskDB() database.TaskDao { return tm.db.Task() }
