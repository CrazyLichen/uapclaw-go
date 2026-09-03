package database

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SQLTaskDao TaskDao 的 SQL 实现。
// 对齐 Python: TaskDao (openjiuwen/agent_teams/tools/database/task_dao.py)
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

func (e *mutationFailure) Error() string { return e.reason }

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

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

// --- 5 个底层辅助函数（对齐 Python 的 _xxx_in_session 函数）---

// refreshStatusInTx 根据 unresolved deps 重算 pending/blocked 状态。
// 对齐 Python: _refresh_status_in_session(session, task_ids, now) -> List[TeamTaskBase]
func refreshStatusInTx(tx *gorm.DB, taskTable, depTable string, taskIDs []string, now int64) []*TeamTaskBase {
	if len(taskIDs) == 0 {
		return nil
	}

	// 对齐 Python: 查询候选任务（仅 pending/blocked）
	var candidates []TeamTaskBase
	tx.Table(taskTable).Where("task_id IN ? AND status IN ?", taskIDs, []string{fsm.TaskStatusPending, fsm.TaskStatusBlocked}).Find(&candidates)
	if len(candidates) == 0 {
		return nil
	}

	candidateIDs := make([]string, 0, len(candidates))
	for _, t := range candidates {
		candidateIDs = append(candidateIDs, t.TaskID)
	}

	// 对齐 Python: 查询每个候选任务的 unresolved deps 计数
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
		// 对齐 Python: pending + unresolved > 0 → blocked
		if task.Status == fsm.TaskStatusPending && unresolved > 0 {
			tx.Table(taskTable).Where("task_id = ?", task.TaskID).
				Select("status", "updated_at").
				Updates(&TeamTaskBase{Status: fsm.TaskStatusBlocked, UpdatedAt: now})
			task.Status = fsm.TaskStatusBlocked
			task.UpdatedAt = now
			refreshedTasks = append(refreshedTasks, &task)
			logger.Info(logComponent).Str("task_id", task.TaskID).Int("unresolved", unresolved).Msg("任务被阻塞")
		} else if task.Status == fsm.TaskStatusBlocked && unresolved == 0 {
			// 对齐 Python: blocked + unresolved == 0 → pending
			tx.Table(taskTable).Where("task_id = ?", task.TaskID).
				Select("status", "updated_at").
				Updates(&TeamTaskBase{Status: fsm.TaskStatusPending, UpdatedAt: now})
			task.Status = fsm.TaskStatusPending
			task.UpdatedAt = now
			refreshedTasks = append(refreshedTasks, &task)
			logger.Info(logComponent).Str("task_id", task.TaskID).Msg("任务解除阻塞")
		}
	}
	return refreshedTasks
}

// terminateTaskInTx 终止任务 + 标记依赖 resolved + 传播解除阻塞。
// 对齐 Python: _terminate_task_in_session(session, task_id, new_status, now)
// 返回 (unblocked_task_ids, error)
func terminateTaskInTx(tx *gorm.DB, taskTable, depTable, taskID, newStatus string, now int64) ([]string, error) {
	// 对齐 Python: new_status 必须是终态
	if !fsm.IsTaskTerminal(newStatus) {
		return nil, fmt.Errorf("terminateTaskInTx 期望终态，收到 %s", newStatus)
	}

	// 对齐 Python: 查任务
	var task TeamTaskBase
	result := tx.Table(taskTable).Where("task_id = ?", taskID).First(&task)
	if result.Error != nil {
		// 对齐 Python: team_logger.error("Task %s not found", task_id); return None
		return nil, nil
	}

	// 对齐 Python: 已是目标状态
	if task.Status == newStatus {
		return nil, nil
	}

	// 对齐 Python: is_valid_transition 校验
	if !fsm.IsValidTaskTransition(task.Status, newStatus) {
		logger.Error(logComponent).Str("task_id", taskID).Str("from", task.Status).Str("to", newStatus).Msg("任务状态转换不合法")
		return nil, nil
	}

	// 对齐 Python: task.status = new_status; task.updated_at = now
	tx.Table(taskTable).Where("task_id = ?", taskID).
		Select("status", "updated_at").
		Updates(&TeamTaskBase{Status: newStatus, UpdatedAt: now})
	logger.Info(logComponent).Str("task_id", taskID).Str("status", newStatus).Msg("任务终止")

	// 对齐 Python: 标记下游依赖 resolved
	depResult := tx.Table(depTable).
		Where("depends_on_task_id = ? AND resolved = 0", taskID).
		Update("resolved", 1)
	resolvedCount := int(depResult.RowsAffected)
	if resolvedCount > 0 {
		logger.Info(logComponent).Str("task_id", taskID).Int("resolved", resolvedCount).Msg("标记依赖已解决")
	}

	// 对齐 Python: 获取下游任务 ID
	var downstreamIDs []string
	tx.Table(depTable).Select("task_id").
		Where("depends_on_task_id = ?", taskID).
		Distinct().Find(&downstreamIDs)

	// 对齐 Python: 刷新下游任务状态
	refreshedTasks := refreshStatusInTx(tx, taskTable, depTable, downstreamIDs, now)
	// terminateTaskInTx 返回 unblocked task IDs（对齐 Python 返回值）
	var unblockedIDs []string
	for _, t := range refreshedTasks {
		unblockedIDs = append(unblockedIDs, t.TaskID)
	}
	return unblockedIDs, nil
}

// stageNewTasksInTx INSERT 新任务行。
// 对齐 Python: _stage_new_tasks(session, team_name, new_tasks, now)
func stageNewTasksInTx(tx *gorm.DB, taskTable, teamName string, newTasks []NewTaskSpec, now int64) error {
	if len(newTasks) == 0 {
		return nil
	}
	// 对齐 Python: seen_ids 去重
	seenIDs := make(map[string]bool, len(newTasks))
	for _, spec := range newTasks {
		if seenIDs[spec.TaskID] {
			return &mutationFailure{reason: fmt.Sprintf("Duplicate task_id %s in new_tasks", spec.TaskID)}
		}
		seenIDs[spec.TaskID] = true
		row := &TeamTaskBase{
			TaskID:    spec.TaskID,
			TeamName:  teamName,
			Title:     spec.Title,
			Content:   spec.Content,
			Status:    spec.InitialStatus,
			UpdatedAt: now,
		}
		if err := tx.Table(taskTable).Create(row).Error; err != nil {
			return &mutationFailure{reason: fmt.Sprintf("插入任务 %s 失败: %s", spec.TaskID, err.Error())}
		}
	}
	return nil
}

// loadEndpointsAndValidateInTx 解析边端点 + 校验存在性和源状态。
// 对齐 Python: _load_endpoints_and_validate(session, add_edges) -> Dict[str, TeamTaskBase]
func loadEndpointsAndValidateInTx(tx *gorm.DB, taskTable string, addEdges []EdgeSpec) (map[string]*TeamTaskBase, error) {
	if len(addEdges) == 0 {
		return nil, nil
	}

	// 对齐 Python: 收集所有端点 ID
	endpointIDs := make(map[string]bool)
	for _, e := range addEdges {
		endpointIDs[e.TaskID] = true
		endpointIDs[e.DependsOnID] = true
	}
	ids := make([]string, 0, len(endpointIDs))
	for id := range endpointIDs {
		ids = append(ids, id)
	}

	// 对齐 Python: 批量查询
	var tasks []TeamTaskBase
	tx.Table(taskTable).Where("task_id IN ?", ids).Find(&tasks)
	taskMap := make(map[string]*TeamTaskBase, len(tasks))
	for i := range tasks {
		taskMap[tasks[i].TaskID] = &tasks[i]
	}

	// 对齐 Python: TASK_DEPENDENCY_REJECT_STATUSES
	rejectStatuses := map[string]bool{
		fsm.TaskStatusCompleted: true, fsm.TaskStatusCancelled: true,
		fsm.TaskStatusClaimed: true, fsm.TaskStatusPlanApproved: true,
	}
	for _, e := range addEdges {
		if _, ok := taskMap[e.TaskID]; !ok {
			return nil, &mutationFailure{reason: fmt.Sprintf("Task %s not found", e.TaskID)}
		}
		if _, ok := taskMap[e.DependsOnID]; !ok {
			return nil, &mutationFailure{reason: fmt.Sprintf("Dependency target %s not found", e.DependsOnID)}
		}
		srcStatus := taskMap[e.TaskID].Status
		if rejectStatuses[srcStatus] {
			return nil, &mutationFailure{reason: fmt.Sprintf("Cannot add dependency to %s in terminal or executing status: %s", e.TaskID, srcStatus)}
		}
	}
	return taskMap, nil
}

// checkCycleAndComputeNewEdgesInTx 环检测 + 计算新边集。
// 对齐 Python: _check_cycle_and_compute_new_edges(session, team_name, add_edges) -> set[tuple]
func checkCycleAndComputeNewEdgesInTx(tx *gorm.DB, depTable, teamName string, addEdges []EdgeSpec, endpointTasks map[string]*TeamTaskBase) ([]TeamTaskDependencyBase, error) {
	if len(addEdges) == 0 {
		return nil, nil
	}

	// 对齐 Python: 获取现有边
	var existingRows []TeamTaskDependencyBase
	tx.Table(depTable).Where("team_name = ?", teamName).Find(&existingRows)
	existingEdgeSet := make(map[string]bool, len(existingRows))
	adjacency := make(map[string][]string)
	for _, r := range existingRows {
		key := r.TaskID + "\x00" + r.DependsOnID
		existingEdgeSet[key] = true
		adjacency[r.TaskID] = append(adjacency[r.TaskID], r.DependsOnID)
	}

	// 对齐 Python: 计算新边，去重
	var newEdges []TeamTaskDependencyBase
	newEdgeSet := make(map[string]bool)
	for _, e := range addEdges {
		key := e.TaskID + "\x00" + e.DependsOnID
		if existingEdgeSet[key] || newEdgeSet[key] {
			continue
		}
		newEdgeSet[key] = true
		adjacency[e.TaskID] = append(adjacency[e.TaskID], e.DependsOnID)

		// 对齐 Python: dep_status = endpoint_tasks[dep_id].status; initial_resolved = dep_status in TASK_TERMINAL_STATUSES
		depStatus := endpointTasks[e.DependsOnID].Status
		resolved := fsm.IsTaskTerminal(depStatus)
		newEdges = append(newEdges, TeamTaskDependencyBase{
			TaskID:      e.TaskID,
			DependsOnID: e.DependsOnID,
			TeamName:    teamName,
			Resolved:    resolved,
		})
	}

	// 对齐 Python: cycle = detect_cycle_in_adjacency(adjacency)
	if cycle := detectCycleInAdjacencySQL(adjacency); cycle != nil {
		return nil, &mutationFailure{reason: fmt.Sprintf("Circular dependency detected: %s", formatCycle(cycle))}
	}

	return newEdges, nil
}

// detectCycleInAdjacencySQL DFS 三色法检测有向图环。
// 对齐 Python: detect_cycle_in_adjacency(adjacency)
func detectCycleInAdjacencySQL(adjacency map[string][]string) []string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int)
	parent := make(map[string]string)

	var cycle []string

	var dfs func(node string) bool
	dfs = func(node string) bool {
		color[node] = gray
		for _, next := range adjacency[node] {
			if color[next] == gray {
				// 找到环：回溯路径
				cycle = []string{next}
				cur := node
				for cur != next {
					cycle = append([]string{cur}, cycle...)
					cur = parent[cur]
				}
				cycle = append([]string{next}, cycle...)
				return true
			}
			if color[next] == white {
				parent[next] = node
				if dfs(next) {
					return true
				}
			}
		}
		color[node] = black
		return false
	}

	// 按插入顺序遍历确保确定性
	for node := range adjacency {
		if color[node] == white {
			if dfs(node) {
				return cycle
			}
		}
	}
	return nil
}

// formatCycle 格式化环路路径。对齐 Python: ' -> '.join(cycle)
func formatCycle(cycle []string) string {
	return strings.Join(cycle, " -> ")
}

// --- 18 个 TaskDao 接口方法 ---

// CreateTask 创建单条任务。
// 对齐 Python: create_task(task_id, team_name, title, content, status) → bool
func (d *SQLTaskDao) CreateTask(ctx context.Context, task *TeamTaskBase) (bool, error) {
	table := d.taskTableName(ctx)
	task.UpdatedAt = GetCurrentTime()
	result := d.db.WithContext(ctx).Table(table).Create(task)
	if result.Error != nil {
		// 对齐 Python: except IntegrityError → False
		return false, nil
	}
	logger.Info(logComponent).Str("task_id", task.TaskID).Msg("任务创建成功")
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
// 对齐 Python: get_team_tasks(team_name, status=None)
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
// 对齐 Python: get_tasks_by_assignee(team_name, assignee_id, status=None)
func (d *SQLTaskDao) GetTasksByAssignee(ctx context.Context, teamName, assignee, status string) ([]*TeamTaskBase, error) {
	table := d.taskTableName(ctx)
	query := d.db.WithContext(ctx).Table(table).
		Where("team_name = ? AND assignee = ?", teamName, assignee)
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

// ClaimTask 认领任务：设置 assignee + pending→claimed FSM 校验。
// 对齐 Python: claim_task(task_id, member_name) → bool
func (d *SQLTaskDao) ClaimTask(ctx context.Context, taskID, assignee string) (bool, error) {
	table := d.taskTableName(ctx)
	var ok bool
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task TeamTaskBase
		result := tx.Table(table).Where("task_id = ?", taskID).First(&task)
		if result.Error != nil {
			// 对齐 Python: team_logger.error("Task %s not found", task_id)
			return nil // 不回滚，返回失败
		}
		// 对齐 Python: if task.assignee → warning + return False
		if task.Assignee != "" {
			logger.Warn(logComponent).Str("task_id", taskID).Str("assignee", task.Assignee).Msg("任务已被认领")
			return nil
		}
		if !fsm.IsValidTaskTransition(task.Status, fsm.TaskStatusClaimed) {
			logger.Error(logComponent).Str("task_id", taskID).Str("from", task.Status).Str("to", fsm.TaskStatusClaimed).Msg("任务状态转换不合法")
			return nil
		}
		tx.Table(table).Where("task_id = ?", taskID).
			Select("status", "assignee", "updated_at").
			Updates(&TeamTaskBase{Status: fsm.TaskStatusClaimed, Assignee: assignee, UpdatedAt: GetCurrentTime()})
		ok = true
		logger.Info(logComponent).Str("task_id", taskID).Str("assignee", assignee).Msg("任务认领成功")
		return nil
	})
	return ok, err
}

// ResetTask 重置任务：claimed→pending，清除 assignee。
// 对齐 Python: reset_task(task_id) → Optional[TeamTaskBase]
func (d *SQLTaskDao) ResetTask(ctx context.Context, taskID string) (bool, error) {
	table := d.taskTableName(ctx)
	var ok bool
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task TeamTaskBase
		result := tx.Table(table).Where("task_id = ?", taskID).First(&task)
		if result.Error != nil {
			return nil
		}
		// 对齐 Python: if task.status != claimed → error
		if task.Status != fsm.TaskStatusClaimed {
			logger.Error(logComponent).Str("task_id", taskID).Str("status", task.Status).Msg("只能重置 claimed 状态的任务")
			return nil
		}
		if !fsm.IsValidTaskTransition(task.Status, fsm.TaskStatusPending) {
			logger.Error(logComponent).Str("task_id", taskID).Str("from", task.Status).Str("to", fsm.TaskStatusPending).Msg("任务状态转换不合法")
			return nil
		}
		tx.Table(table).Where("task_id = ?", taskID).
			Select("status", "assignee", "updated_at").
			Updates(&TeamTaskBase{Status: fsm.TaskStatusPending, Assignee: "", UpdatedAt: GetCurrentTime()})
		ok = true
		logger.Info(logComponent).Str("task_id", taskID).Msg("任务重置为 pending")
		return nil
	})
	return ok, err
}

// ApprovePlanTask 计划审批：claimed→plan_approved FSM 校验。
// 对齐 Python: approve_plan_task(task_id) → Optional[TeamTaskBase]
func (d *SQLTaskDao) ApprovePlanTask(ctx context.Context, taskID string) (bool, error) {
	table := d.taskTableName(ctx)
	var ok bool
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task TeamTaskBase
		result := tx.Table(table).Where("task_id = ?", taskID).First(&task)
		if result.Error != nil {
			return nil
		}
		if !fsm.IsValidTaskTransition(task.Status, fsm.TaskStatusPlanApproved) {
			logger.Error(logComponent).Str("task_id", taskID).Str("from", task.Status).Str("to", fsm.TaskStatusPlanApproved).Msg("任务状态转换不合法")
			return nil
		}
		tx.Table(table).Where("task_id = ?", taskID).
			Select("status", "updated_at").
			Updates(&TeamTaskBase{Status: fsm.TaskStatusPlanApproved, UpdatedAt: GetCurrentTime()})
		ok = true
		logger.Info(logComponent).Str("task_id", taskID).Msg("任务计划已审批")
		return nil
	})
	return ok, err
}

// UpdateTaskStatus 更新任务状态。完成时自动解除下游依赖并刷新 blocked→pending。
// 对齐 Python: update_task_status(task_id, status) → bool
func (d *SQLTaskDao) UpdateTaskStatus(ctx context.Context, taskID, newStatus string) ([]string, error) {
	table := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)
	var refreshedIDs []string
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task TeamTaskBase
		result := tx.Table(table).Where("task_id = ?", taskID).First(&task)
		if result.Error != nil {
			return nil
		}
		if !fsm.IsValidTaskTransition(task.Status, newStatus) {
			logger.Error(logComponent).Str("task_id", taskID).Str("from", task.Status).Str("to", newStatus).Msg("任务状态转换不合法")
			return nil
		}

		now := GetCurrentTime()
		tx.Table(table).Where("task_id = ?", taskID).
			Select("status", "updated_at").
			Updates(&TeamTaskBase{Status: newStatus, UpdatedAt: now})

		// 对齐 Python: if status == completed → 标记依赖 resolved
		if newStatus == fsm.TaskStatusCompleted {
			logger.Info(logComponent).Str("task_id", taskID).Msg("任务已完成")
			depResult := tx.Table(depTable).
				Where("depends_on_task_id = ? AND resolved = 0", taskID).
				Update("resolved", 1)
			if depResult.RowsAffected > 0 {
				logger.Info(logComponent).Str("task_id", taskID).Int64("resolved", depResult.RowsAffected).Msg("标记依赖已解决")
			}
		}

		logger.Info(logComponent).Str("task_id", taskID).Str("status", newStatus).Msg("任务状态已更新")
		return nil
	})
	return refreshedIDs, err
}

// UpdateTask 更新标题/内容。claimed/plan_approved 状态下禁止编辑。
// 对齐 Python: update_task(task_id, title, content) → bool
func (d *SQLTaskDao) UpdateTask(ctx context.Context, taskID, title, content string) (bool, error) {
	table := d.taskTableName(ctx)
	var ok bool
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task TeamTaskBase
		result := tx.Table(table).Where("task_id = ?", taskID).First(&task)
		if result.Error != nil {
			return nil
		}
		// 对齐 Python: if task.status in (claimed, plan_approved) → error
		if task.Status == fsm.TaskStatusClaimed || task.Status == fsm.TaskStatusPlanApproved {
			logger.Error(logComponent).Str("task_id", taskID).Str("status", task.Status).Msg("当前状态禁止编辑任务内容")
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
			logger.Info(logComponent).Str("task_id", taskID).Msg("任务内容已更新")
		}
		ok = true
		return nil
	})
	return ok, err
}

// MutateDependencyGraph 原子图变更：5 步管线。
// 对齐 Python: mutate_dependency_graph(team_name, new_tasks, add_edges) → GraphMutationResult
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
		newEdges, err := checkCycleAndComputeNewEdgesInTx(tx, depTable, teamName, addEdges, endpointTasks)
		if err != nil {
			mutationErr = err
			return err // 回滚
		}

		// 步骤4: applyNewEdgesInTx — INSERT 依赖行
		// 对齐 Python: _apply_new_edges(session, team_name, new_edge_set, endpoint_tasks)
		for _, edge := range newEdges {
			if createErr := tx.Table(depTable).Create(&edge).Error; createErr != nil {
				mutationErr = createErr
				return createErr // rollback
			}
		}

		// 步骤5: refreshStatusInTx
		// 对齐 Python: affected_ids = new_tasks ids + new_edges task_ids
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
		// 对齐 Python: except _MutationFailure → session.rollback(); return fail
		result.Reason = mutationErr.Error()
		logger.Error(logComponent).Str("reason", result.Reason).Msg("图变更管线失败")
	} else if result.Ok {
		// 对齐 Python 日志
		logger.Info(logComponent).
			Int("new_tasks", len(newTasks)).
			Int("new_edges", len(addEdges)).
			Int("refreshed", len(result.RefreshedTasks)).
			Msg("图变更管线完成")
	}
	return result
}

// AddTaskWithBidirectionalDependencies 带双向依赖创建任务。委托 MutateDependencyGraph。
// 对齐 Python: add_task_with_bidirectional_dependencies(task_id, team_name, ...) → bool
func (d *SQLTaskDao) AddTaskWithBidirectionalDependencies(ctx context.Context, teamName string, task *TeamTaskBase, dependencies []string, dependentTaskIDs []string) GraphMutationResult {
	newTaskSpec := NewTaskSpec{
		TaskID:        task.TaskID,
		Title:         task.Title,
		Content:       task.Content,
		InitialStatus: task.Status,
	}
	// 构建双向 EdgeSpec
	var edges []EdgeSpec
	// dependencies：新任务依赖上游 → 边方向 (new_task, upstream)
	for _, depID := range dependencies {
		edges = append(edges, EdgeSpec{TaskID: task.TaskID, DependsOnID: depID})
	}
	// dependentTaskIDs：下游依赖新任务 → 边方向 (existing_task, new_task)
	for _, downstreamID := range dependentTaskIDs {
		edges = append(edges, EdgeSpec{TaskID: downstreamID, DependsOnID: task.TaskID})
	}

	result := d.MutateDependencyGraph(ctx, teamName, []NewTaskSpec{newTaskSpec}, edges)
	if !result.Ok {
		logger.Error(logComponent).Str("task_id", task.TaskID).Str("reason", result.Reason).Msg("带依赖创建任务失败")
	}
	return result
}

// GetTaskDependencies 查询任务依赖。
// 对齐 Python: get_task_dependencies(task_id) → List[TeamTaskDependencyBase]
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
// 对齐 Python: get_unresolved_dependencies_count(task_id) → int
func (d *SQLTaskDao) GetUnresolvedDependenciesCount(ctx context.Context, taskID string) (int, error) {
	depTable := d.depTableName(ctx)
	var count int64
	d.db.WithContext(ctx).Table(depTable).
		Where("task_id = ? AND resolved = 0", taskID).
		Count(&count)
	return int(count), nil
}

// GetTasksDependingOn 查询下游依赖任务（即被 taskID 阻塞的任务）。
// 对齐 Python: get_tasks_depending_on(depends_on_task_id) → List[TeamTaskBase]
func (d *SQLTaskDao) GetTasksDependingOn(ctx context.Context, taskID string) ([]*TeamTaskBase, error) {
	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)

	// 对齐 Python: 先查 deps，再查 tasks
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
// 对齐 Python: delete_task(task_id) → bool
func (d *SQLTaskDao) DeleteTask(ctx context.Context, taskID string) error {
	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)

	// 对齐 Python: session.delete(task) — 先检查存在
	result := d.db.WithContext(ctx).Table(taskTable).Where("task_id = ?", taskID).Delete(nil)
	if result.RowsAffected == 0 {
		logger.Warn(logComponent).Str("task_id", taskID).Msg("删除任务未找到")
	}
	// 同步删依赖
	d.db.WithContext(ctx).Table(depTable).
		Where("task_id = ? OR depends_on_task_id = ?", taskID, taskID).Delete(nil)
	return nil
}

// CancelTask 取消任务（原子终止传播），返回 unblocked task IDs。
// 对齐 Python: cancel_task(task_id) → {"task": ..., "unblocked_tasks": [...]}
func (d *SQLTaskDao) CancelTask(ctx context.Context, taskID string) ([]string, error) {
	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)
	var unblocked []string
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := GetCurrentTime()
		refreshed, _ := terminateTaskInTx(tx, taskTable, depTable, taskID, fsm.TaskStatusCancelled, now)
		unblocked = refreshed
		return nil
	})
	return unblocked, err
}

// CompleteTask 完成任务（原子终止传播），返回 unblocked task IDs。
// 对齐 Python: complete_task(task_id) → {"task": ..., "unblocked_tasks": [...]}
func (d *SQLTaskDao) CompleteTask(ctx context.Context, taskID string) ([]string, error) {
	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)
	var unblocked []string
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := GetCurrentTime()
		refreshed, _ := terminateTaskInTx(tx, taskTable, depTable, taskID, fsm.TaskStatusCompleted, now)
		unblocked = refreshed
		return nil
	})
	return unblocked, err
}

// CancelAllTasks 批量取消（原子终止传播），支持 skipAssignees 过滤。
// 对齐 Python: cancel_all_tasks(team_name, skip_assignees) → {"cancelled_tasks": [...], "unblocked_tasks": [...]}
func (d *SQLTaskDao) CancelAllTasks(ctx context.Context, teamName string, skipAssignees []string) (*CancelAllTasksResult, error) {
	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)
	result := &CancelAllTasksResult{}
	skipSet := make(map[string]bool, len(skipAssignees))
	for _, a := range skipAssignees {
		skipSet[a] = true
	}

	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 对齐 Python: 查询所有非终态任务
		var candidates []TeamTaskBase
		tx.Table(taskTable).
			Where("team_name = ? AND status NOT IN ?", teamName, []string{fsm.TaskStatusCompleted, fsm.TaskStatusCancelled}).
			Find(&candidates)

		if len(candidates) == 0 {
			logger.Info(logComponent).Str("team_name", teamName).Msg("无可取消的活跃任务")
			return nil
		}

		now := GetCurrentTime()
		unblockedByID := make(map[string]*TeamTaskBase)

		for _, task := range candidates {
			// 对齐 Python: if assignee in skip_assignees → continue
			if skipSet[task.Assignee] {
				logger.Debug(logComponent).Str("task_id", task.TaskID).Str("assignee", task.Assignee).Msg("跳过：assignee 在 skipAssignees 中")
				continue
			}
			refreshed, _ := terminateTaskInTx(tx, taskTable, depTable, task.TaskID, fsm.TaskStatusCancelled, now)
			result.Cancelled = append(result.Cancelled, &task)
			for _, id := range refreshed {
				if _, exists := unblockedByID[id]; !exists {
					var t TeamTaskBase
					if findErr := tx.Table(taskTable).Where("task_id = ?", id).First(&t).Error; findErr == nil {
						unblockedByID[id] = &t
					}
				}
			}
		}

		// 对齐 Python: 排除已取消的任务
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
			Msg("批量取消任务完成")
		return nil
	})
	return result, err
}

// VerifyAndFixTaskConsistency 一致性修复：扫描 blocked 任务并刷新状态。
// 对齐 Python: verify_and_fix_task_consistency(team_name) → List[TeamTaskBase]
func (d *SQLTaskDao) VerifyAndFixTaskConsistency(ctx context.Context, teamName string) ([]string, error) {
	taskTable := d.taskTableName(ctx)
	depTable := d.depTableName(ctx)

	var refreshedIDs []string
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 对齐 Python: 查所有 blocked 任务
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
