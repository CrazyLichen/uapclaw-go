package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SQLTaskDao TaskDao 的 SQL 实现。
// Python: TaskDao (openjiuwen/agent_teams/tools/database/task_dao.py)
// 操作动态表 team_task_{suffix} + team_task_dependency_{suffix}。
type SQLTaskDao struct {
	// db GORM 数据库实例
	db *gorm.DB
}

// mutationFailure 对齐 Python _MutationFailure。
// 管线步骤返回此 error 表示业务规则拒绝。
type mutationFailure struct {
	// reason 失败原因
	reason string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateTask 创建单条任务。
// Python: create_task(task_id, team_name, title, content, status) → bool
func (d *SQLTaskDao) CreateTask(ctx context.Context, task *TeamTaskBase) (bool, error) {
	table := d.taskTableName(ctx)
	task.UpdatedAt = GetCurrentTime()
	result := d.db.WithContext(ctx).Table(table).Create(task)
	if result.Error != nil {
		// Python: except IntegrityError → False
		return false, nil
	}
	logger.Info(logComponent).Str("task_id", task.TaskID).Msg("Task created")
	return true, nil
}

// GetTask 按 ID 查询任务。对齐 Python: get_task(task_id) → Optional[TeamTaskBase]
func (d *SQLTaskDao) GetTask(ctx context.Context, taskID string) (*TeamTaskBase, error) {
	table := d.taskTableName(ctx)
	var task TeamTaskBase
	result := d.db.WithContext(ctx).Table(table).Where("task_id = ?", taskID).First(&task)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, result.Error
	}
	return &task, nil
}

// GetTeamTasks 查询团队全部任务。status 为空字符串表示不过滤。
// Python: get_team_tasks(team_name, status=None)
func (d *SQLTaskDao) GetTeamTasks(ctx context.Context, teamName, status string) ([]*TeamTaskBase, error) {
	table := d.taskTableName(ctx)
	query := d.db.WithContext(ctx).Table(table).Where("team_name = ?", teamName)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var tasks []*TeamTaskBase
	result := query.Find(&tasks)
	if result.Error != nil {
		return nil, result.Error
	}
	return tasks, nil
}

// GetTasksByAssignee 查询成员的任务。status 为空字符串表示不过滤。
// Python: get_tasks_by_assignee(team_name, assignee_id, status=None)
func (d *SQLTaskDao) GetTasksByAssignee(ctx context.Context, teamName, assignee, status string) ([]*TeamTaskBase, error) {
	table := d.taskTableName(ctx)
	// 防御性处理：assignee 为 nullable 字段，空字符串查询应翻译为 IS NULL
	// 对齐 Python SQLAlchemy: model.assignee == None → IS NULL
	query := d.db.WithContext(ctx).Table(table).Where("team_name = ?", teamName)
	if assignee == "" {
		query = query.Where("assignee IS NULL")
	} else {
		query = query.Where("assignee = ?", assignee)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var tasks []*TeamTaskBase
	result := query.Find(&tasks)
	if result.Error != nil {
		return nil, result.Error
	}
	return tasks, nil
}

// OpenTaskCountsByAssignee 按 assignee 统计非终态任务数。
//
// Python: open_task_counts_by_assignee (task_dao.py:685-705)
func (d *SQLTaskDao) OpenTaskCountsByAssignee(ctx context.Context, teamName string) (map[string]int, error) {
	table := d.taskTableName(ctx)
	terminal := fsm.TerminalTaskStatuses()
	rows, err := d.db.WithContext(ctx).Table(table).
		Select("assignee, COUNT(*) AS cnt").
		Where("team_name = ?", teamName).
		Where("assignee IS NOT NULL").
		Where("status NOT IN ?", terminal).
		Group("assignee").
		Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[string]int)
	for rows.Next() {
		var assignee string
		var cnt int
		if scanErr := rows.Scan(&assignee, &cnt); scanErr != nil {
			return nil, scanErr
		}
		counts[assignee] = cnt
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}
	return counts, nil
}

// GetUnseenEdits 查询成员名下非终态且 revision > observed_revision 的任务。
//
// 偏差 D-5：上游用 TASK_WORKING_STATUSES（planning / in_progress / in_review），
// Go 任务状态机目前只有 6 态（pending / claimed / plan_approved / completed /
// cancelled / blocked），故用 NOT IN (completed, cancelled) 近似；
// 25.4 condition-named 状态机落地后改回精确集合。
//
// Python: get_unseen_edits (task_dao.py L763-779)
func (d *SQLTaskDao) GetUnseenEdits(ctx context.Context, teamName, assignee string) ([]*TeamTaskBase, error) {
	table := d.taskTableName(ctx)
	var tasks []*TeamTaskBase
	result := d.db.WithContext(ctx).Table(table).
		Where("team_name = ?", teamName).
		Where("assignee = ?", assignee).
		Where("status NOT IN ?", []string{fsm.TaskStatusCompleted, fsm.TaskStatusCancelled}).
		Where("revision > observed_revision").
		Find(&tasks)
	if result.Error != nil {
		return nil, result.Error
	}
	return tasks, nil
}

// AppendChangeNote 追加一条编辑说明并推进 revision。
//
// revision 推进与 change_notes 追加在同一事务内完成：单独推进 revision 会让
// 「改过但未读」成立却读不到说明，单独追加说明会让条目落在区间外被过滤掉。
//
// Python: update_task 内的 revision += 1 / change_notes 追加（F_129 / D8）
func (d *SQLTaskDao) AppendChangeNote(ctx context.Context, taskID string, revision int64, note, by string) (bool, error) {
	table := d.taskTableName(ctx)
	var ok bool
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task TeamTaskBase
		result := tx.Table(table).Where("task_id = ?", taskID).First(&task)
		if result.Error != nil {
			return nil
		}
		notes := decodeChangeNotes(task.ChangeNotes)
		rev := revision
		notes = append(notes, ChangeNote{Revision: &rev, Note: note, By: by})
		encoded, marshalErr := json.Marshal(notes)
		if marshalErr != nil {
			return marshalErr
		}
		tx.Table(table).Where("task_id = ?", taskID).Updates(map[string]any{
			"revision":     revision,
			"change_notes": string(encoded),
		})
		ok = true
		return nil
	})
	return ok, err
}

// ClaimTask 认领任务：设置 assignee + FSM 校验到 toStatus。
// Python: claim_task(task_id, member_name, to_status=IN_PROGRESS/PLANNING) → bool
// —— autonomous 自认领与 scheduled start 走 IN_PROGRESS，计划闸预留走 PLANNING。
func (d *SQLTaskDao) ClaimTask(ctx context.Context, taskID, assignee, toStatus string) (bool, error) {
	table := d.taskTableName(ctx)
	var ok bool
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task TeamTaskBase
		result := tx.Table(table).Where("task_id = ?", taskID).First(&task)
		if result.Error != nil {
			// Python: team_logger.error("Task %s not found", task_id)
			return nil // 不回滚，返回失败
		}
		// Python: if task.assignee → warning + return False
		if task.Assignee != nil && *task.Assignee != "" {
			logger.Warn(logComponent).Str("task_id", taskID).Str("assignee", *task.Assignee).Msg("Task already claimed")
			return nil
		}
		if !fsm.IsValidTaskTransition(task.Status, toStatus) {
			logger.Error(logComponent).Str("task_id", taskID).Str("from", task.Status).Str("to", toStatus).Msg("Invalid task status transition")
			return nil
		}
		tx.Table(table).Where("task_id = ?", taskID).
			Select("status", "assignee", "updated_at").
			Updates(&TeamTaskBase{Status: toStatus, Assignee: StringPtr(assignee), UpdatedAt: GetCurrentTime()})
		ok = true
		logger.Info(logComponent).Str("task_id", taskID).Str("assignee", assignee).Str("to_status", toStatus).Msg("Task claimed")
		return nil
	})
	return ok, err
}

// ResetTask 重置任务：claimed→pending，清除 assignee。
// Python: reset_task(task_id) → Optional[TeamTaskBase]
func (d *SQLTaskDao) ResetTask(ctx context.Context, taskID string) (bool, error) {
	table := d.taskTableName(ctx)
	var ok bool
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task TeamTaskBase
		result := tx.Table(table).Where("task_id = ?", taskID).First(&task)
		if result.Error != nil {
			return nil
		}
		// Python: reset 的合法来源是活跃态（IN_PROGRESS/PLANNING/IN_REVIEW，
		// condition-named 状态机后转换表各自有 → PENDING）
		if task.Status != fsm.TaskStatusInProgress && task.Status != fsm.TaskStatusPlanning && task.Status != fsm.TaskStatusInReview {
			logger.Error(logComponent).Str("task_id", taskID).Str("status", task.Status).Msg("Can only reset active tasks")
			return nil
		}
		if !fsm.IsValidTaskTransition(task.Status, fsm.TaskStatusPending) {
			logger.Error(logComponent).Str("task_id", taskID).Str("from", task.Status).Str("to", fsm.TaskStatusPending).Msg("Invalid task status transition")
			return nil
		}
		tx.Table(table).Where("task_id = ?", taskID).
			Select("status", "assignee", "updated_at").
			Updates(&TeamTaskBase{Status: fsm.TaskStatusPending, Assignee: nil, UpdatedAt: GetCurrentTime()})
		ok = true
		logger.Info(logComponent).Str("task_id", taskID).Msg("Task reset to pending")
		return nil
	})
	return ok, err
}

// ApprovePlanTask 计划审批：PLANNING→IN_PROGRESS FSM 校验。
// Python: approve_plan_task(task_id)：leader 审批放行
func (d *SQLTaskDao) ApprovePlanTask(ctx context.Context, taskID string) (bool, error) {
	table := d.taskTableName(ctx)
	var ok bool
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task TeamTaskBase
		result := tx.Table(table).Where("task_id = ?", taskID).First(&task)
		if result.Error != nil {
			return nil
		}
		if !fsm.IsValidTaskTransition(task.Status, fsm.TaskStatusInProgress) {
			logger.Error(logComponent).Str("task_id", taskID).Str("from", task.Status).Str("to", fsm.TaskStatusInProgress).Msg("Invalid task status transition")
			return nil
		}
		tx.Table(table).Where("task_id = ?", taskID).
			Select("status", "updated_at").
			Updates(&TeamTaskBase{Status: fsm.TaskStatusInProgress, UpdatedAt: GetCurrentTime()})
		ok = true
		logger.Info(logComponent).Str("task_id", taskID).Msg("Task plan approved")
		return nil
	})
	return ok, err
}

// UpdateTaskStatus 更新任务状态。完成时自动解除下游依赖并刷新 blocked→pending。
// Python: update_task_status(task_id, status) → bool
func (d *SQLTaskDao) UpdateTaskStatus(ctx context.Context, taskID, newStatus string) (bool, error) {
	table := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)
	var ok bool
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task TeamTaskBase
		result := tx.Table(table).Where("task_id = ?", taskID).First(&task)
		if result.Error != nil {
			return nil
		}
		if !fsm.IsValidTaskTransition(task.Status, newStatus) {
			logger.Error(logComponent).Str("task_id", taskID).Str("from", task.Status).Str("to", newStatus).Msg("Invalid task status transition")
			return nil
		}

		now := GetCurrentTime()
		tx.Table(table).Where("task_id = ?", taskID).
			Select("status", "updated_at").
			Updates(&TeamTaskBase{Status: newStatus, UpdatedAt: now})

		// Python: if status == completed → 标记依赖 resolved
		if newStatus == fsm.TaskStatusCompleted {
			logger.Info(logComponent).Str("task_id", taskID).Msg("Task completed")
			depResult := tx.Table(depTable).
				Where("depends_on_task_id = ? AND resolved = 0", taskID).
				Update("resolved", 1)
			if depResult.RowsAffected > 0 {
				logger.Info(logComponent).Str("task_id", taskID).Int64("resolved", depResult.RowsAffected).Msg("Dependencies marked resolved")
			}
		}

		ok = true
		logger.Info(logComponent).Str("task_id", taskID).Str("status", newStatus).Msg("Task status updated")
		return nil
	})
	return ok, err
}

// UpdateTask 更新标题/内容。claimed/plan_approved 状态下禁止编辑。
// Python: update_task(task_id, title, content) → bool
func (d *SQLTaskDao) UpdateTask(ctx context.Context, taskID, title, content string) (bool, error) {
	table := d.taskTableName(ctx)
	var ok bool
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task TeamTaskBase
		result := tx.Table(table).Where("task_id = ?", taskID).First(&task)
		if result.Error != nil {
			return nil
		}
		// Python: 仅 IN_REVIEW 冻结（PLANNING/IN_PROGRESS 可编辑，F_129）
		if task.Status == fsm.TaskStatusInReview {
			logger.Error(logComponent).Str("task_id", taskID).Str("status", task.Status).Msg("Cannot edit task in current status")
			return nil
		}

		updates := map[string]any{}
		if title != "" && task.Title != title {
			updates["title"] = title
		}
		if content != "" && task.Content != content {
			updates["content"] = content
		}
		if len(updates) > 0 {
			tx.Table(table).Where("task_id = ?", taskID).Updates(updates)
			logger.Info(logComponent).Str("task_id", taskID).Msg("Task content updated")
		}
		ok = true
		return nil
	})
	return ok, err
}

// MutateDependencyGraph 原子图变更：5 步管线。
// Python: mutate_dependency_graph(team_name, new_tasks, add_edges) → GraphMutationResult
// 管线失败时 return err 触发 rollback，对齐 Python 的 session.rollback()
func (d *SQLTaskDao) MutateDependencyGraph(ctx context.Context, teamName string, newTasks []NewTaskSpec, addEdges []EdgeSpec) GraphMutationResult {
	if len(newTasks) == 0 && len(addEdges) == 0 {
		return GraphMutationResult{Ok: true}
	}

	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)
	now := GetCurrentTime()

	var result GraphMutationResult
	var mutationErr error
	var newEdges []TeamTaskDependencyBase

	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 步骤1: stageNewTasksInTx
		if mutationErr = stageNewTasksInTx(tx, taskTable, teamName, newTasks, now); mutationErr != nil {
			return mutationErr // 回滚
		}

		// 步骤2: loadEndpointsAndValidateInTx
		endpointTasks, err := loadEndpointsAndValidateInTx(tx, taskTable, addEdges)
		if err != nil {
			mutationErr = err
			return err // 回滚
		}

		// 步骤3: checkCycleAndComputeNewEdgesInTx
		newEdges, err = checkCycleAndComputeNewEdgesInTx(tx, depTable, teamName, addEdges, endpointTasks)
		if err != nil {
			mutationErr = err
			return err // 回滚
		}

		// 步骤4: applyNewEdgesInTx — INSERT 依赖行
		// Python: _apply_new_edges(session, team_name, new_edge_set, endpoint_tasks)
		for _, edge := range newEdges {
			if createErr := tx.Table(depTable).Create(&edge).Error; createErr != nil {
				mutationErr = createErr
				return createErr // 回滚
			}
		}

		// 步骤5: refreshStatusInTx
		// Python: affected_ids = new_tasks ids + new_edges task_ids
		affectedIDs := make(map[string]bool)
		for _, spec := range newTasks {
			affectedIDs[spec.TaskID] = true
		}
		for _, edge := range newEdges {
			affectedIDs[edge.TaskID] = true
		}
		var ids []string
		for id := range affectedIDs {
			ids = append(ids, id)
		}
		refreshedTasks := refreshStatusInTx(tx, taskTable, depTable, ids, now)

		result.Ok = true
		result.RefreshedTasks = refreshedTasks
		return nil // 提交
	})

	if err != nil {
		// Python: except _MutationFailure → session.rollback(); return fail
		result.Reason = mutationErr.Error()
		logger.Error(logComponent).Str("reason", result.Reason).Msg("Dependency graph mutation pipeline failed")
	} else if result.Ok {
		// Python: 内存实现: 分支日志，使用去重后的 newEdges 计数
		if len(newTasks) > 0 {
			logger.Info(logComponent).
				Int("new_tasks", len(newTasks)).
				Int("new_edges", len(newEdges)).
				Int("refreshed", len(result.RefreshedTasks)).
				Msg("Tasks created; edges added; tasks refreshed")
		} else {
			logger.Info(logComponent).
				Int("new_edges", len(newEdges)).
				Int("refreshed", len(result.RefreshedTasks)).
				Msg("Edges added; tasks refreshed")
		}
	}
	return result
}

// AddTaskWithBidirectionalDependencies 带双向依赖创建任务。委托 MutateDependencyGraph。
// Python: add_task_with_bidirectional_dependencies(task_id, team_name, title, content, status, *, dependencies, dependent_task_ids)
func (d *SQLTaskDao) AddTaskWithBidirectionalDependencies(ctx context.Context, teamName string, task *TeamTaskBase, dependencies []string, dependentTaskIDs []string) GraphMutationResult {
	newTaskSpec := NewTaskSpec{
		TaskID:        task.TaskID,
		Title:         task.Title,
		Content:       task.Content,
		InitialStatus: task.Status,
	}
	// Python: edges = [(task_id, dep_id) for dep_id in dependencies]
	var edges []EdgeSpec
	for _, depID := range dependencies {
		edges = append(edges, EdgeSpec{TaskID: task.TaskID, DependsOnID: depID})
	}
	// Python: dep_edges = [(dep_id, task_id) for dep_id in dependent_task_ids]
	for _, depID := range dependentTaskIDs {
		edges = append(edges, EdgeSpec{TaskID: depID, DependsOnID: task.TaskID})
	}

	result := d.MutateDependencyGraph(ctx, teamName, []NewTaskSpec{newTaskSpec}, edges)
	if !result.Ok {
		logger.Error(logComponent).Str("task_id", task.TaskID).Str("reason", result.Reason).Msg("Failed to create task with dependencies")
	}
	return result
}

// GetDependentTaskIDs 返回依赖 dependsOnTaskID 的全部下游任务 id（去重）。
// 下游任务 id 就是依赖边的 task_id 列，一条 SELECT task_id 即可回答。
// Python: get_dependent_task_ids（task_dao.py L1630-1642）
func (d *SQLTaskDao) GetDependentTaskIDs(ctx context.Context, dependsOnTaskID string) ([]string, error) {
	table := d.depTableName(ctx)
	var ids []string
	err := d.db.WithContext(ctx).Table(table).
		Where("depends_on_task_id = ?", dependsOnTaskID).
		Distinct().Pluck("task_id", &ids).Error
	return ids, err
}

// GetTasksByIDs 一条 IN 查询取多行（顺序未定义；缺失 id 跳过）。
// Python: get_tasks_by_ids（task_dao.py L675-683）
func (d *SQLTaskDao) GetTasksByIDs(ctx context.Context, taskIDs []string) ([]*TeamTaskBase, error) {
	if len(taskIDs) == 0 {
		return []*TeamTaskBase{}, nil
	}
	table := d.taskTableName(ctx)
	var rows []*TeamTaskBase
	err := d.db.WithContext(ctx).Table(table).Where("task_id IN ?", taskIDs).Find(&rows).Error
	return rows, err
}

// GetRevisionSnapshot 取 (task_id, revision) 时刻的正文快照（F_129/D9）。
// ⚠️ 已知偏差：Go 无 session_file_store 溢出机制，恒返回 (nil, nil)——
// diff section 按契约优雅降级为空；回收条件 = Go 落任务正文溢出存储。
// Python: get_revision_snapshot（task_dao.py L1473-1494）
func (d *SQLTaskDao) GetRevisionSnapshot(_ context.Context, _, _ string, _ int64) (*string, error) {
	return nil, nil
}

// GetSubtasks 取直接子任务行（F_129），正文不读（简报只渲染
// id/title/assignee/status/revision；content 可能是溢出占位符）。
// Python: get_subtasks（task_dao.py L781-794）
func (d *SQLTaskDao) GetSubtasks(ctx context.Context, teamName, parentTaskID string) ([]*TeamTaskBase, error) {
	table := d.taskTableName(ctx)
	var rows []*TeamTaskBase
	err := d.db.WithContext(ctx).Table(table).
		Where("team_name = ? AND parent_task_id = ?", teamName, parentTaskID).
		Find(&rows).Error
	return rows, err
}

// GetOtherActiveTaskID 成员持有的另一条活跃任务 id，LIMIT 1 存在式探测。
// 「活跃」覆盖三个非终态持有条件——PLANNING（计划闸）、IN_PROGRESS（执行）、
// IN_REVIEW（验收闸，作者仍在岗）。
// Python: get_other_active_task_id（task_dao.py L822-852）
func (d *SQLTaskDao) GetOtherActiveTaskID(ctx context.Context, teamName, memberName, excludeTaskID string) (*string, error) {
	table := d.taskTableName(ctx)
	var id string
	err := d.db.WithContext(ctx).Table(table).
		Where(
			"team_name = ? AND assignee = ? AND task_id <> ? AND status IN (?, ?, ?)",
			teamName, memberName, excludeTaskID,
			fsm.TaskStatusPlanning, fsm.TaskStatusInProgress, fsm.TaskStatusInReview,
		).
		Limit(1).Pluck("task_id", &id).Error
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, nil
	}
	return &id, nil
}

// SubmitForReview 原子地把 IN_PROGRESS 任务推进到 IN_REVIEW（verify-gate 入口）。
// 纯状态翻转——依赖不在此处解除（任务要评审通过才算完成）。WHERE
// status='in_progress' 使 IN_PROGRESS 为唯一前驱。review_round 同语句自增
// （F_62）：开新轮与状态翻转原子，对被超越轮记的票永远不会与当前轮混淆。
// summary/note 仅在非 nil 时覆写存储的完成文本（F_119）。
//
// Python: submit_for_review (task_dao.py L1085-1127)。
func (d *SQLTaskDao) SubmitForReview(ctx context.Context, taskID, summary, note string) (bool, error) {
	table := d.taskTableName(ctx)
	updates := map[string]any{
		"status":       fsm.TaskStatusInReview,
		"review_round": gorm.Expr("review_round + 1"),
		"updated_at":   GetCurrentTime(),
	}
	if summary != "" {
		updates["summary"] = summary
	}
	if note != "" {
		updates["note"] = note
	}
	result := d.db.WithContext(ctx).Table(table).
		Where("task_id = ? AND status = ?", taskID, fsm.TaskStatusInProgress).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	submitted := result.RowsAffected == 1
	if submitted {
		logger.Info(logComponent).Str("task_id", taskID).Msg("Task submitted for review")
	} else {
		logger.Debug(logComponent).Str("task_id", taskID).
			Int64("rowcount", result.RowsAffected).
			Msg("CAS submit-for-review did not apply: missing / not in_progress")
	}
	return submitted, nil
}

// ReviseTask 原子地把 IN_REVIEW 任务退回 IN_PROGRESS（verify fail 返工边）。
// 纯状态翻转；WHERE status='in_review' 使 IN_REVIEW 为唯一前驱。
//
// Python: revise_task (task_dao.py L1130-1160)。
func (d *SQLTaskDao) ReviseTask(ctx context.Context, taskID string) (bool, error) {
	table := d.taskTableName(ctx)
	result := d.db.WithContext(ctx).Table(table).
		Where("task_id = ? AND status = ?", taskID, fsm.TaskStatusInReview).
		Update("status", fsm.TaskStatusInProgress)
	if result.Error != nil {
		return false, result.Error
	}
	revised := result.RowsAffected == 1
	if revised {
		logger.Info(logComponent).Str("task_id", taskID).
			Msg("Task sent back for revision (IN_REVIEW -> IN_PROGRESS)")
	} else {
		logger.Debug(logComponent).Str("task_id", taskID).
			Int64("rowcount", result.RowsAffected).
			Msg("CAS revise did not apply: missing / not in_review")
	}
	return revised, nil
}

// InsertReviewVote 追加一条评审票事实行（F_62）。
// 追加式设计：改票 = INSERT 新行，计票取每 reviewer 的最新行——不同 reviewer
// 的并发投票从不争用共享行，投票历史全程可审计。此处不做状态校验——调用方
// （TeamTaskManager.verify_task）在写锁外守卫状态/验收人成员资格。
//
// Python: insert_review_vote (task_dao.py L1196-1246)。
func (d *SQLTaskDao) InsertReviewVote(ctx context.Context, teamName, taskID string, reviewRound int64, reviewer, decision string, feedback *string) error {
	voteTable := "team_review_vote_" + d.reviewVoteTableName(ctx)
	row := TeamTaskReviewVoteBase{
		TeamName:    teamName,
		TaskID:      taskID,
		ReviewRound: reviewRound,
		Reviewer:    reviewer,
		Decision:    decision,
		Feedback:    feedback,
		CreatedAt:   GetCurrentTime(),
	}
	if err := d.db.WithContext(ctx).Table(voteTable).Create(&row).Error; err != nil {
		return err
	}
	logger.Info(logComponent).Str("task_id", taskID).Int64("round", reviewRound).
		Str("reviewer", reviewer).Str("decision", decision).
		Msg("Review vote recorded")
	return nil
}

// GetReviewVotes 返回一条任务某轮的全部票行，按 id 升序（最早在前）。
// 升序 id 让调用方用简单的字典覆盖得到「每 reviewer 最新票」。被超越轮的
// 行永远不会被读取——round 列把它们分区隔离。
//
// Python: get_review_votes (task_dao.py L1247-1264)。
func (d *SQLTaskDao) GetReviewVotes(ctx context.Context, taskID string, reviewRound int64) ([]*TeamTaskReviewVoteBase, error) {
	voteTable := "team_review_vote_" + d.reviewVoteTableName(ctx)
	var votes []*TeamTaskReviewVoteBase
	if err := d.db.WithContext(ctx).Table(voteTable).
		Where("task_id = ? AND review_round = ?", taskID, reviewRound).
		Order("id ASC").
		Find(&votes).Error; err != nil {
		return nil, err
	}
	return votes, nil
}

// reviewVoteTableName 获取当前 session 的评审票表名。
func (d *SQLTaskDao) reviewVoteTableName(ctx context.Context) string {
	sessionID := sessionctx.GetSessionID(ctx)
	suffix := SanitizeSessionIDForTable(sessionID)
	return "team_review_vote_" + suffix
}

// StartTaskCAS 原子地把已归属 PENDING 任务推进到 toStatus（调度器侧 CAS）。
// 谓词 WHERE assignee=member AND status=pending 使 PENDING 成为唯一合法前驱，
// 且要求任务已归属 member_name——调度器代表记录的 owner 启动任务，不（重）指派。
// resume_pending 由本次 start 消费（F_128）。
// Python: start_task（task_dao.py L855-905）
func (d *SQLTaskDao) StartTaskCAS(ctx context.Context, taskID, memberName, toStatus string) (bool, error) {
	table := d.taskTableName(ctx)
	result := d.db.WithContext(ctx).Table(table).
		Where("task_id = ? AND assignee = ? AND status = ?", taskID, memberName, fsm.TaskStatusPending).
		Updates(map[string]any{"status": toStatus, "resume_pending": false, "updated_at": GetCurrentTime()})
	if result.Error != nil {
		return false, result.Error
	}
	started := result.RowsAffected == 1
	if started {
		logger.Info(logComponent).Str("task_id", taskID).Str("member", memberName).Msg("Task started by member")
	} else {
		logger.Debug(logComponent).Str("task_id", taskID).Str("member", memberName).
			Int64("rowcount", result.RowsAffected).
			Msg("CAS start did not apply: missing / not pending / not owned by this member")
	}
	return started, nil
}

// ReassignTask 原子换人：单条条件 UPDATE——WHERE assignee=from AND status∈
// 可改派集——且 SET 只动 assignee 与交接清理字段，状态原位保留（执行中的任务
// 绝不弹回 PENDING，无伪 TASK_RELEASED、无可认领池竞态窗口）。镜像 claim_task
// 的 CAS 纪律：存在性/成员校验是调用方（锁外）的职责，本方法只做并发仲裁。
// summary/note 与换手原子清空（F_119）；resume_pending 由本次换手消费（F_128）；
// observed_revision 追平 revision（F_129）。
// Python: reassign_task (task_dao.py L971-1018)
func (d *SQLTaskDao) ReassignTask(ctx context.Context, taskID, fromAssignee, toAssignee string) (bool, error) {
	table := d.taskTableName(ctx)
	now := GetCurrentTime()
	// TASK_REASSIGNABLE_STATUSES（status.py L399-405）= PENDING/BLOCKED/IN_PROGRESS。
	// 本包不 import schema（会成环，见 models.go L282 注），可改派集按上游同一组
	// 状态字面量落在这里；一致性由 schema.TaskReassignableStatuses 的守护用例对拍。
	reassignable := []string{fsm.TaskStatusPending, fsm.TaskStatusBlocked, fsm.TaskStatusInProgress}
	result := d.db.WithContext(ctx).Table(table).
		Where("task_id = ? AND assignee = ? AND status IN ?", taskID, fromAssignee, reassignable).
		Updates(map[string]any{
			"assignee":          toAssignee,
			"summary":           nil,
			"note":              nil,
			"resume_pending":    false,
			"observed_revision": gorm.Expr("revision"),
			"updated_at":        now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	swapped := result.RowsAffected == 1
	if swapped {
		logger.Info(logComponent).Str("task_id", taskID).
			Str("from", fromAssignee).Str("to", toAssignee).
			Msg("Task reassigned")
	} else {
		logger.Debug(logComponent).Str("task_id", taskID).
			Str("from", fromAssignee).Str("to", toAssignee).
			Int64("rowcount", result.RowsAffected).
			Msg("CAS reassign did not apply")
	}
	return swapped, nil
}

// ObserveRevision 记录 assignee 被展示了 revision（F_129）。
// 谓词 assignee=member AND observed_revision<revision 使记录单调且 owner 绑定。
// Python: observe_revision（task_dao.py L1493-1511）
func (d *SQLTaskDao) ObserveRevision(ctx context.Context, taskID, memberName string, revision int64) (bool, error) {
	table := d.taskTableName(ctx)
	result := d.db.WithContext(ctx).Table(table).
		Where("task_id = ? AND assignee = ? AND observed_revision < ?", taskID, memberName, revision).
		Update("observed_revision", revision)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// GetUnseenRevisions 全队级「已指派、未终态、assignee 未见过当前 revision」窄投影。
// Python: get_unseen_revisions（task_dao.py L723-752）
func (d *SQLTaskDao) GetUnseenRevisions(ctx context.Context, teamName string) ([]UnseenRevisionRow, error) {
	table := d.taskTableName(ctx)
	var rows []struct {
		TaskID        string
		Status        string
		Assignee      *string
		ResumePending bool
		Revision      int64
	}
	err := d.db.WithContext(ctx).Table(table).
		Select("task_id, status, assignee, resume_pending, revision").
		Where(
			"team_name = ? AND assignee IS NOT NULL AND status NOT IN (?, ?) AND revision > observed_revision",
			teamName, fsm.TaskStatusCompleted, fsm.TaskStatusCancelled,
		).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]UnseenRevisionRow, 0, len(rows))
	for _, row := range rows {
		assignee := ""
		if row.Assignee != nil {
			assignee = *row.Assignee
		}
		out = append(out, UnseenRevisionRow{
			TaskID: row.TaskID, Status: row.Status, Assignee: assignee,
			ResumePending: row.ResumePending, Revision: row.Revision,
		})
	}
	return out, nil
}

// CountTasksTerminality 一条聚合查询返回 (total, non_terminal)。
// Python: count_tasks_terminality（B_09）
func (d *SQLTaskDao) CountTasksTerminality(ctx context.Context, teamName string) (int64, int64, error) {
	table := d.taskTableName(ctx)
	var total, nonTerminal int64
	if err := d.db.WithContext(ctx).Table(table).Where("team_name = ?", teamName).Count(&total).Error; err != nil {
		return 0, 0, err
	}
	if err := d.db.WithContext(ctx).Table(table).
		Where("team_name = ? AND status NOT IN (?, ?)", teamName, fsm.TaskStatusCompleted, fsm.TaskStatusCancelled).
		Count(&nonTerminal).Error; err != nil {
		return 0, 0, err
	}
	return total, nonTerminal, nil
}

// GetTaskDependencies 查询任务依赖。
// Python: get_task_dependencies(task_id) → List[TeamTaskDependencyBase]
func (d *SQLTaskDao) GetTaskDependencies(ctx context.Context, taskID string) ([]*TeamTaskDependencyBase, error) {
	depTable := d.depTableName(ctx)
	var deps []*TeamTaskDependencyBase
	result := d.db.WithContext(ctx).Table(depTable).Where("task_id = ?", taskID).Find(&deps)
	if result.Error != nil {
		return nil, result.Error
	}
	return deps, nil
}

// GetUnresolvedDependenciesCount 未解决依赖计数。
// Python: get_unresolved_dependencies_count(task_id) → int
func (d *SQLTaskDao) GetUnresolvedDependenciesCount(ctx context.Context, taskID string) (int, error) {
	depTable := d.depTableName(ctx)
	var count int64
	d.db.WithContext(ctx).Table(depTable).
		Where("task_id = ? AND resolved = 0", taskID).
		Count(&count)
	return int(count), nil
}

// GetTasksDependingOn 查询下游依赖任务（即被 taskID 阻塞的任务）。
// Python: get_tasks_depending_on(depends_on_task_id) → List[TeamTaskBase]
func (d *SQLTaskDao) GetTasksDependingOn(ctx context.Context, taskID string) ([]*TeamTaskBase, error) {
	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)

	// Python: 先查 deps，再查 tasks
	var depTaskIDs []string
	d.db.WithContext(ctx).Table(depTable).
		Select("task_id").
		Where("depends_on_task_id = ?", taskID).
		Find(&depTaskIDs)
	if len(depTaskIDs) == 0 {
		return nil, nil
	}

	var tasks []*TeamTaskBase
	result := d.db.WithContext(ctx).Table(taskTable).Where("task_id IN ?", depTaskIDs).Find(&tasks)
	if result.Error != nil {
		return nil, result.Error
	}
	return tasks, nil
}

// DeleteTask 删除任务。
// Python: delete_task(task_id) → bool
func (d *SQLTaskDao) DeleteTask(ctx context.Context, taskID string) error {
	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)

	// Python: session.delete(task) — 先检查存在
	result := d.db.WithContext(ctx).Table(taskTable).Where("task_id = ?", taskID).Delete(nil)
	if result.RowsAffected == 0 {
		logger.Warn(logComponent).Str("task_id", taskID).Msg("Task not found for deletion")
	}
	// 同步删依赖
	d.db.WithContext(ctx).Table(depTable).
		Where("task_id = ? OR depends_on_task_id = ?", taskID, taskID).Delete(nil)
	return nil
}

// CancelTask 取消任务（原子终止传播），返回被取消的任务和解除阻塞的任务列表。
// Python: cancel_task(task_id) → {"task": ..., "unblocked_tasks": [...]}
func (d *SQLTaskDao) CancelTask(ctx context.Context, taskID string) (*TeamTaskBase, []*TeamTaskBase, error) {
	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)
	var task *TeamTaskBase
	var unblocked []*TeamTaskBase
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := GetCurrentTime()
		task, unblocked, _ = terminateTaskInTx(tx, taskTable, depTable, taskID, fsm.TaskStatusCancelled, now)
		return nil
	})
	return task, unblocked, err
}

// CompleteTask 完成任务（原子终止传播），返回被完成的任务和解除阻塞的任务列表。
// Python: complete_task(task_id) → {"task": ..., "unblocked_tasks": [...]}
func (d *SQLTaskDao) CompleteTask(ctx context.Context, taskID string) (*TeamTaskBase, []*TeamTaskBase, error) {
	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)
	var task *TeamTaskBase
	var unblocked []*TeamTaskBase
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := GetCurrentTime()
		task, unblocked, _ = terminateTaskInTx(tx, taskTable, depTable, taskID, fsm.TaskStatusCompleted, now)
		return nil
	})
	return task, unblocked, err
}

// CancelAllTasks 批量取消（原子终止传播），支持 skipAssignees 过滤。
// Python: cancel_all_tasks(team_name, skip_assignees) → {"cancelled_tasks": [...], "unblocked_tasks": [...]}
func (d *SQLTaskDao) CancelAllTasks(ctx context.Context, teamName string, skipAssignees []string) (*CancelAllTasksResult, error) {
	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)
	result := &CancelAllTasksResult{}
	skipSet := make(map[string]bool, len(skipAssignees))
	for _, a := range skipAssignees {
		skipSet[a] = true
	}

	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Python: 查询所有非终态任务
		var candidates []TeamTaskBase
		tx.Table(taskTable).
			Where("team_name = ? AND status NOT IN ?", teamName, []string{fsm.TaskStatusCompleted, fsm.TaskStatusCancelled}).
			Find(&candidates)

		if len(candidates) == 0 {
			logger.Info(logComponent).Str("team_name", teamName).Msg("No active tasks to cancel")
			return nil
		}

		now := GetCurrentTime()
		unblockedByID := make(map[string]*TeamTaskBase)

		for _, task := range candidates {
			// Python: if assignee in skip_assignees → continue
			assigneeKey := ""
			if task.Assignee != nil {
				assigneeKey = *task.Assignee
			}
			if skipSet[assigneeKey] {
				assigneeStr := assigneeKey
				logger.Debug(logComponent).Str("task_id", task.TaskID).Str("assignee", assigneeStr).Msg("Skipping: assignee in skipAssignees")
				continue
			}
			_, refreshed, _ := terminateTaskInTx(tx, taskTable, depTable, task.TaskID, fsm.TaskStatusCancelled, now)
			result.Cancelled = append(result.Cancelled, &task)
			for _, t := range refreshed {
				if _, exists := unblockedByID[t.TaskID]; !exists {
					unblockedByID[t.TaskID] = t
				}
			}
		}

		// Python: 排除已取消的任务
		cancelledIDs := make(map[string]bool, len(result.Cancelled))
		for _, t := range result.Cancelled {
			cancelledIDs[t.TaskID] = true
		}
		for id, t := range unblockedByID {
			if !cancelledIDs[id] {
				result.Unblocked = append(result.Unblocked, t)
			}
		}

		logger.Info(logComponent).
			Str("team_name", teamName).
			Int("cancelled", len(result.Cancelled)).
			Int("unblocked", len(result.Unblocked)).
			Msg("batch cancel tasks completed")
		return nil
	})
	return result, err
}

// VerifyAndFixTaskConsistency 一致性修复：扫描 blocked 任务并刷新状态。
// Python: verify_and_fix_task_consistency(team_name) → List[TeamTaskBase]
func (d *SQLTaskDao) VerifyAndFixTaskConsistency(ctx context.Context, teamName string) ([]string, error) {
	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)

	var refreshedIDs []string
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Python: 查所有 blocked 任务
		var blockedIDs []string
		tx.Table(taskTable).
			Select("task_id").
			Where("team_name = ? AND status = ?", teamName, fsm.TaskStatusBlocked).
			Find(&blockedIDs)
		if len(blockedIDs) == 0 {
			return nil
		}

		now := GetCurrentTime()
		refreshedTasks := refreshStatusInTx(tx, taskTable, depTable, blockedIDs, now)
		for _, t := range refreshedTasks {
			refreshedIDs = append(refreshedIDs, t.TaskID)
		}
		return nil
	})
	return refreshedIDs, err
}

// Error 返回失败原因。
func (e *mutationFailure) Error() string { return e.reason }

// ──────────────────────────── 非导出函数 ────────────────────────────

// decodeChangeNotes 解析 change_notes JSON 列表，坏值返回空切片。
//
// 上游 unseen_change_notes 对坏 JSON 也是返回空（吞掉解析错误），
// 这里保持同样宽容：一条坏记录不该让整个追加流程失败。
//
// Python: message_template.unseen_change_notes 的 json.loads 容错（L410-421）
func decodeChangeNotes(raw *string) []ChangeNote {
	if raw == nil || *raw == "" {
		return nil
	}
	var notes []ChangeNote
	if err := json.Unmarshal([]byte(*raw), &notes); err != nil {
		return nil
	}
	return notes
}

// withTx 返回绑定指定事务的 DAO 实例。
func (d *SQLTaskDao) withTx(tx *gorm.DB) *SQLTaskDao {
	return &SQLTaskDao{db: tx}
}

// taskTableName 获取当前 session 的任务表名。
func (d *SQLTaskDao) taskTableName(ctx context.Context) string {
	sessionID := sessionctx.GetSessionID(ctx)
	suffix := SanitizeSessionIDForTable(sessionID)
	return "team_task_" + suffix
}

// depTableName 获取当前 session 的依赖表名。
func (d *SQLTaskDao) depTableName(ctx context.Context) string {
	sessionID := sessionctx.GetSessionID(ctx)
	suffix := SanitizeSessionIDForTable(sessionID)
	return "team_task_dependency_" + suffix
}

// refreshStatusInTx 根据 unresolved deps 重算 pending/blocked 状态。
// Python: _refresh_status_in_session(session, task_ids, now) -> List[TeamTaskBase]
func refreshStatusInTx(tx *gorm.DB, taskTable, depTable string, taskIDs []string, now int64) []*TeamTaskBase {
	if len(taskIDs) == 0 {
		return nil
	}

	// Python: 查询候选任务（仅 pending/blocked）
	var candidates []TeamTaskBase
	tx.Table(taskTable).Where("task_id IN ? AND status IN ?", taskIDs, []string{fsm.TaskStatusPending, fsm.TaskStatusBlocked}).Find(&candidates)
	if len(candidates) == 0 {
		return nil
	}

	candidateIDs := make([]string, 0, len(candidates))
	for _, t := range candidates {
		candidateIDs = append(candidateIDs, t.TaskID)
	}

	// Python: 查询每个候选任务的 unresolved deps 计数
	type unresolvedCount struct {
		TaskID     string
		Unresolved int
	}
	var counts []unresolvedCount
	tx.Table(depTable).
		Select("task_id, COUNT(*) as unresolved").
		Where("task_id IN ? AND resolved = 0", candidateIDs).
		Group("task_id").
		Find(&counts)

	countMap := make(map[string]int, len(counts))
	for _, c := range counts {
		countMap[c.TaskID] = c.Unresolved
	}

	var refreshedTasks []*TeamTaskBase
	for _, task := range candidates {
		unresolved := countMap[task.TaskID]
		// Python: pending + unresolved > 0 → blocked
		if task.Status == fsm.TaskStatusPending && unresolved > 0 {
			tx.Table(taskTable).Where("task_id = ?", task.TaskID).
				Select("status", "updated_at").
				Updates(&TeamTaskBase{Status: fsm.TaskStatusBlocked, UpdatedAt: now})
			task.Status = fsm.TaskStatusBlocked
			task.UpdatedAt = now
			refreshedTasks = append(refreshedTasks, &task)
			logger.Info(logComponent).Str("task_id", task.TaskID).Int("unresolved", unresolved).Msg("Task blocked")
		} else if task.Status == fsm.TaskStatusBlocked && unresolved == 0 {
			// Python: blocked + unresolved == 0 → pending
			tx.Table(taskTable).Where("task_id = ?", task.TaskID).
				Select("status", "updated_at").
				Updates(&TeamTaskBase{Status: fsm.TaskStatusPending, UpdatedAt: now})
			task.Status = fsm.TaskStatusPending
			task.UpdatedAt = now
			refreshedTasks = append(refreshedTasks, &task)
			logger.Info(logComponent).Str("task_id", task.TaskID).Msg("Task unblocked")
		}
	}
	return refreshedTasks
}

// terminateTaskInTx 终止任务 + 标记依赖 resolved + 传播解除阻塞。
// Python: _terminate_task_in_session(session, task_id, new_status, now)
// 返回 (unblocked_task_ids, error)
func terminateTaskInTx(tx *gorm.DB, taskTable, depTable, taskID, newStatus string, now int64) (*TeamTaskBase, []*TeamTaskBase, error) {
	// Python: new_status 必须是终态
	if !fsm.IsTaskTerminal(newStatus) {
		return nil, nil, fmt.Errorf("terminateTaskInTx 期望终态，收到 %s", newStatus)
	}

	// Python: 查任务
	var task TeamTaskBase
	result := tx.Table(taskTable).Where("task_id = ?", taskID).First(&task)
	if result.Error != nil {
		// Python: team_logger.error("Task %s not found", task_id); return None
		return nil, nil, nil
	}

	// Python: 已是目标状态（幂等）
	if task.Status == newStatus {
		logger.Debug(logComponent).Str("task_id", taskID).Str("status", newStatus).Msg("Task already in target status")
		return &task, []*TeamTaskBase{}, nil
	}

	// Python: is_valid_transition 校验
	if !fsm.IsValidTaskTransition(task.Status, newStatus) {
		logger.Error(logComponent).Str("task_id", taskID).Str("from", task.Status).Str("to", newStatus).Msg("Invalid task status transition")
		return nil, nil, nil
	}

	// Python: task.status = new_status; task.updated_at = now
	tx.Table(taskTable).Where("task_id = ?", taskID).
		Select("status", "updated_at").
		Updates(&TeamTaskBase{Status: newStatus, UpdatedAt: now})
	// 更新本地 task 对象状态
	task.Status = newStatus
	task.UpdatedAt = now
	logger.Info(logComponent).Str("task_id", taskID).Str("status", newStatus).Msg("Task terminated")

	// Python: 标记下游依赖 resolved
	depResult := tx.Table(depTable).
		Where("depends_on_task_id = ? AND resolved = 0", taskID).
		Update("resolved", 1)
	resolvedCount := int(depResult.RowsAffected)
	if resolvedCount > 0 {
		logger.Info(logComponent).Str("task_id", taskID).Int("resolved", resolvedCount).Msg("Dependencies marked resolved")
	}

	// Python: 获取下游任务 ID
	var downstreamIDs []string
	tx.Table(depTable).Select("task_id").
		Where("depends_on_task_id = ?", taskID).
		Distinct().Find(&downstreamIDs)

	// Python: 刷新下游任务状态
	refreshedTasks := refreshStatusInTx(tx, taskTable, depTable, downstreamIDs, now)
	return &task, refreshedTasks, nil
}

// stageNewTasksInTx INSERT 新任务行。
// Python: _stage_new_tasks(session, team_name, new_tasks, now)
func stageNewTasksInTx(tx *gorm.DB, taskTable, teamName string, newTasks []NewTaskSpec, now int64) error {
	if len(newTasks) == 0 {
		return nil
	}
	// Python: seen_ids 去重
	seenIDs := make(map[string]bool, len(newTasks))
	for _, spec := range newTasks {
		if seenIDs[spec.TaskID] {
			return &mutationFailure{reason: fmt.Sprintf("new_tasks 中存在重复 task_id: %s", spec.TaskID)}
		}
		seenIDs[spec.TaskID] = true
		row := &TeamTaskBase{
			TaskID:    spec.TaskID,
			TeamName:  teamName,
			Title:     spec.Title,
			Content:   spec.Content,
			Status:    spec.InitialStatus,
			Assignee:  spec.Assignee,
			UpdatedAt: now,
		}
		if err := tx.Table(taskTable).Create(row).Error; err != nil {
			return &mutationFailure{reason: fmt.Sprintf("插入任务 %s 失败: %s", spec.TaskID, err.Error())}
		}
	}
	return nil
}

// loadEndpointsAndValidateInTx 解析边端点 + 校验存在性和源状态。
// Python: _load_endpoints_and_validate(session, add_edges) -> Dict[str, TeamTaskBase]
func loadEndpointsAndValidateInTx(tx *gorm.DB, taskTable string, addEdges []EdgeSpec) (map[string]*TeamTaskBase, error) {
	if len(addEdges) == 0 {
		return nil, nil
	}

	// Python: 收集所有端点 ID
	endpointIDs := make(map[string]bool)
	for _, e := range addEdges {
		endpointIDs[e.TaskID] = true
		endpointIDs[e.DependsOnID] = true
	}
	ids := make([]string, 0, len(endpointIDs))
	for id := range endpointIDs {
		ids = append(ids, id)
	}

	// Python: 批量查询
	var tasks []TeamTaskBase
	tx.Table(taskTable).Where("task_id IN ?", ids).Find(&tasks)
	taskMap := make(map[string]*TeamTaskBase, len(tasks))
	for i := range tasks {
		taskMap[tasks[i].TaskID] = &tasks[i]
	}

	// Python: TASK_DEPENDENCY_REJECT_STATUSES（graph.py L21-29：终态 + 三活跃态）
	rejectStatuses := map[string]bool{
		fsm.TaskStatusCompleted: true, fsm.TaskStatusCancelled: true,
		fsm.TaskStatusPlanning: true, fsm.TaskStatusInProgress: true,
		fsm.TaskStatusInReview: true,
	}
	for _, e := range addEdges {
		if _, ok := taskMap[e.TaskID]; !ok {
			return nil, &mutationFailure{reason: fmt.Sprintf("任务 %s 不存在", e.TaskID)}
		}
		if _, ok := taskMap[e.DependsOnID]; !ok {
			return nil, &mutationFailure{reason: fmt.Sprintf("依赖目标 %s 不存在", e.DependsOnID)}
		}
		srcStatus := taskMap[e.TaskID].Status
		if rejectStatuses[srcStatus] {
			return nil, &mutationFailure{reason: fmt.Sprintf("不能为终态或执行中状态的任务 %s 添加依赖: %s", e.TaskID, srcStatus)}
		}
	}
	return taskMap, nil
}

// checkCycleAndComputeNewEdgesInTx 环检测 + 计算新边集。
// Python: _check_cycle_and_compute_new_edges(session, team_name, add_edges) -> set[tuple]
func checkCycleAndComputeNewEdgesInTx(tx *gorm.DB, depTable, teamName string, addEdges []EdgeSpec, endpointTasks map[string]*TeamTaskBase) ([]TeamTaskDependencyBase, error) {
	if len(addEdges) == 0 {
		return nil, nil
	}

	// Python: 获取现有边
	var existingRows []TeamTaskDependencyBase
	tx.Table(depTable).Where("team_name = ?", teamName).Find(&existingRows)
	existingEdgeSet := make(map[string]bool, len(existingRows))
	adjacency := make(map[string][]string)
	for _, r := range existingRows {
		key := r.TaskID + "\x00" + r.DependsOnID
		existingEdgeSet[key] = true
		adjacency[r.TaskID] = append(adjacency[r.TaskID], r.DependsOnID)
	}

	// Python: 计算新边，去重
	var newEdges []TeamTaskDependencyBase
	newEdgeSet := make(map[string]bool)
	for _, e := range addEdges {
		key := e.TaskID + "\x00" + e.DependsOnID
		if existingEdgeSet[key] || newEdgeSet[key] {
			continue
		}
		newEdgeSet[key] = true
		adjacency[e.TaskID] = append(adjacency[e.TaskID], e.DependsOnID)

		// Python: dep_status = endpoint_tasks[dep_id].status; initial_resolved = dep_status in TASK_TERMINAL_STATUSES
		depStatus := endpointTasks[e.DependsOnID].Status
		resolved := fsm.IsTaskTerminal(depStatus)
		newEdges = append(newEdges, TeamTaskDependencyBase{
			TaskID:      e.TaskID,
			DependsOnID: e.DependsOnID,
			TeamName:    teamName,
			Resolved:    resolved,
		})
	}

	// Python: cycle = detect_cycle_in_adjacency(adjacency)
	if cycle := detectCycleInAdjacencySQL(adjacency); cycle != nil {
		return nil, &mutationFailure{reason: fmt.Sprintf("检测到循环依赖: %s", formatCycle(cycle))}
	}

	return newEdges, nil
}

// detectCycleInAdjacencySQL 迭代 DFS 三色法检测有向图环。
// Python: detect_cycle_in_adjacency(adjacency)
// 使用显式栈替代递归，WHITE/GRAY/BLACK 三色标记，避免深依赖链栈溢出。
func detectCycleInAdjacencySQL(adjacency map[string][]string) []string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int)

	// 初始化所有节点为白色
	for node, deps := range adjacency {
		color[node] = white
		for _, dep := range deps {
			if _, ok := color[dep]; !ok {
				color[dep] = white
			}
		}
	}

	// 栈元素：当前节点 + 待访问的邻居列表
	type stackEntry struct {
		node     string
		children []string
	}

	for root := range color {
		if color[root] != white {
			continue
		}
		// 当前 DFS 路径
		path := []string{root}
		color[root] = gray
		stack := []stackEntry{{node: root, children: append([]string{}, adjacency[root]...)}}

		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if len(top.children) == 0 {
				// 所有邻居已访问，回溯
				color[top.node] = black
				stack = stack[:len(stack)-1]
				path = path[:len(path)-1]
				continue
			}
			// 取出下一个待访问的邻居
			nxt := top.children[len(top.children)-1]
			top.children = top.children[:len(top.children)-1]

			c := color[nxt]
			if c == gray {
				// 找到环：从路径中 nxt 出现的位置截取
				idx := -1
				for i, p := range path {
					if p == nxt {
						idx = i
						break
					}
				}
				cycle := make([]string, len(path[idx:]))
				copy(cycle, path[idx:])
				cycle = append(cycle, nxt)
				return cycle
			}
			if c == white {
				color[nxt] = gray
				path = append(path, nxt)
				stack = append(stack, stackEntry{node: nxt, children: append([]string{}, adjacency[nxt]...)})
			}
		}
	}
	return nil
}

// formatCycle 格式化环路路径。对齐 Python: ' -> '.join(cycle)
func formatCycle(cycle []string) string {
	return strings.Join(cycle, " -> ")
}
