package database

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InMemoryTeamDatabase 内存数据库替代实现。
// 对齐 Python: InMemoryTeamDatabase (openjiuwen/agent_teams/tools/memory_database.py)
//
// 单体结构体同时实现 TeamDatabase + TeamDao + MemberDao + TaskDao 接口，
// 对齐 Python 的 self.team = self / self.member = self / self.task = self 自引用设计。
// MessageDao 接口方法由 ⤵️ 9.65a-3 回填。
type InMemoryTeamDatabase struct {
	// teams 团队数据，key=teamName
	teams map[string]*Team
	// members 成员数据，key=memberName+"\x00"+teamName（复合主键编码）
	members map[string]*TeamMember
	// tasks 任务数据，key=taskID
	tasks map[string]*TeamTaskBase
	// deps 依赖边数据，key=taskID+"\x00"+dependsOnID（复合主键编码）
	deps map[string]*TeamTaskDependencyBase
	// initialized 是否已初始化
	initialized bool
	// mu 保护并发访问
	mu sync.Mutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryTeamDatabase 创建内存数据库实例。
func NewInMemoryTeamDatabase() *InMemoryTeamDatabase {
	return &InMemoryTeamDatabase{
		teams:   make(map[string]*Team),
		members: make(map[string]*TeamMember),
		tasks:   make(map[string]*TeamTaskBase),
		deps:    make(map[string]*TeamTaskDependencyBase),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// memberKey 构造复合主键 key。
func memberKey(memberName, teamName string) string {
	return memberName + "\x00" + teamName
}

// depKey 构造依赖边复合主键 key。
func depKey(taskID, dependsOnID string) string {
	return taskID + "\x00" + dependsOnID
}

// ──────────────────────────── TeamDatabase 接口实现 ────────────────────────────

// Initialize 初始化（InMemory 无需操作，直接标记已初始化）。
func (db *InMemoryTeamDatabase) Initialize(_ context.Context) error {
	db.mu.Lock()
	db.initialized = true
	db.mu.Unlock()
	return nil
}

// CreateCurSessionTables InMemory 模式下为 no-op（无动态表）。
func (db *InMemoryTeamDatabase) CreateCurSessionTables(_ context.Context) error { return nil }

// DropCurSessionTables InMemory 模式下为 no-op。
func (db *InMemoryTeamDatabase) DropCurSessionTables(_ context.Context) error { return nil }

// CleanupAllRuntimeState 清空所有 map（对齐 Python 清空所有 dict）。
func (db *InMemoryTeamDatabase) CleanupAllRuntimeState(_ context.Context) ([]string, []string, error) {
	db.mu.Lock()
	db.teams = make(map[string]*Team)
	db.members = make(map[string]*TeamMember)
	db.tasks = make(map[string]*TeamTaskBase)
	db.deps = make(map[string]*TeamTaskDependencyBase)
	db.mu.Unlock()
	return nil, nil, nil
}

// DropSessionTablesByID InMemory 模式下为 no-op。
func (db *InMemoryTeamDatabase) DropSessionTablesByID(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// ForceDeleteTeamSession 跨表拆卸：删 team 行 + 删该 team 下所有成员。
// 对齐 Python: force_delete_team_session(team_name)
func (db *InMemoryTeamDatabase) ForceDeleteTeamSession(_ context.Context, teamName string) bool {
	db.mu.Lock()
	_, exists := db.teams[teamName]
	delete(db.teams, teamName)
	// 删除该 team 下所有成员（对齐 Python CASCADE on delete）
	for key, member := range db.members {
		if member.TeamName == teamName {
			delete(db.members, key)
		}
	}
	// 删除该 team 下所有任务
	for id, task := range db.tasks {
		if task.TeamName == teamName {
			delete(db.tasks, id)
		}
	}
	// 删除该 team 下所有依赖边
	for key, dep := range db.deps {
		if dep.TeamName == teamName {
			delete(db.deps, key)
		}
	}
	db.mu.Unlock()
	return exists
}

// Close 关闭数据库（清空所有数据）。
func (db *InMemoryTeamDatabase) Close() error {
	db.mu.Lock()
	db.teams = nil
	db.members = nil
	db.tasks = nil
	db.deps = nil
	db.initialized = false
	db.mu.Unlock()
	return nil
}

// Team 返回 TeamDao（自引用：self.team = self）。
func (db *InMemoryTeamDatabase) Team() TeamDao { return db }

// Member 返回 MemberDao（自引用：self.member = self）。
func (db *InMemoryTeamDatabase) Member() MemberDao { return db }

// Task 返回 TaskDao（自引用：self.task = self）。
func (db *InMemoryTeamDatabase) Task() TaskDao { return db }

// Message 返回 MessageDao（⤵️ 9.65a-3 回填后返回 db）。
func (db *InMemoryTeamDatabase) Message() MessageDao { return db }

// ──────────────────────────── TeamDao 接口实现 ────────────────────────────

// CreateTeam 创建团队。对齐 Python: TeamDao.create_team()
// 成功返回 true，团队已存在返回 false（对齐 Python IntegrityError → False）
func (db *InMemoryTeamDatabase) CreateTeam(_ context.Context, teamName, displayName, leaderMemberName, desc, prompt string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, exists := db.teams[teamName]; exists {
		return false // 对齐 Python IntegrityError → False
	}
	ts := GetCurrentTime()
	db.teams[teamName] = &Team{
		TeamName:         teamName,
		DisplayName:      displayName,
		LeaderMemberName: leaderMemberName,
		Desc:             desc,
		Prompt:           prompt,
		Created:          ts,
		UpdatedAt:        ts,
	}
	return true
}

// GetTeam 获取团队信息。对齐 Python: TeamDao.get_team()
func (db *InMemoryTeamDatabase) GetTeam(_ context.Context, teamName string) (*Team, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	team, exists := db.teams[teamName]
	if !exists {
		return nil, nil // 对齐 Python Optional[Team] → None
	}
	return team, nil
}

// TeamExists 团队是否存在。对齐 Python: TeamDao.team_exists()
func (db *InMemoryTeamDatabase) TeamExists(_ context.Context, teamName string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, exists := db.teams[teamName]
	return exists
}

// DeleteTeam 删除团队（级联删除成员）。对齐 Python: TeamDao.delete_team()
func (db *InMemoryTeamDatabase) DeleteTeam(_ context.Context, teamName string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, exists := db.teams[teamName]
	if !exists {
		return false // 对齐 Python: team not found → False
	}
	delete(db.teams, teamName)
	// 级联删除成员（对齐 Python CASCADE on delete）
	for key, member := range db.members {
		if member.TeamName == teamName {
			delete(db.members, key)
		}
	}
	return true
}

// GetTeamUpdatedAt 获取团队 updated_at 毫秒时间戳。对齐 Python: TeamDao.get_team_updated_at()
func (db *InMemoryTeamDatabase) GetTeamUpdatedAt(_ context.Context, teamName string) int64 {
	db.mu.Lock()
	defer db.mu.Unlock()
	team, exists := db.teams[teamName]
	if !exists || team.UpdatedAt == 0 {
		return 0 // 对齐 Python: missing → 0
	}
	return team.UpdatedAt
}

// ──────────────────────────── MemberDao 接口实现 ────────────────────────────

// CreateMember 创建成员。对齐 Python: MemberDao.create_member()
// 成功返回 true，成员已存在返回 false（对齐 Python IntegrityError → False）
func (db *InMemoryTeamDatabase) CreateMember(_ context.Context, memberName, teamName, displayName, agentCard, status, role, desc, executionStatus, mode, prompt, modelRefJSON string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	key := memberKey(memberName, teamName)
	if _, exists := db.members[key]; exists {
		return false // 对齐 Python IntegrityError → False
	}
	db.members[key] = &TeamMember{
		MemberName:      memberName,
		TeamName:        teamName,
		DisplayName:     displayName,
		Desc:            desc,
		AgentCard:       agentCard,
		Status:          status,
		ExecutionStatus: executionStatus,
		Mode:            mode,
		Role:            role,
		Prompt:          prompt,
		ModelRefJSON:    modelRefJSON,
		UpdatedAt:       GetCurrentTime(), // 对齐 Python: updated_at = get_current_time()
	}
	return true
}

// GetMember 获取成员信息。对齐 Python: MemberDao.get_member()
func (db *InMemoryTeamDatabase) GetMember(_ context.Context, memberName, teamName string) (*TeamMember, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	member, exists := db.members[memberKey(memberName, teamName)]
	if !exists {
		return nil, nil // 对齐 Python Optional[TeamMember] → None
	}
	return member, nil
}

// GetTeamMembers 获取团队成员列表，可选按 status 过滤。对齐 Python: MemberDao.get_team_members(team, status=None)
func (db *InMemoryTeamDatabase) GetTeamMembers(_ context.Context, teamName string, status string) ([]*TeamMember, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var result []*TeamMember
	for _, member := range db.members {
		if member.TeamName != teamName {
			continue
		}
		if status != "" && member.Status != status {
			continue // 对齐 Python: status 过滤
		}
		result = append(result, member)
	}
	return result, nil
}

// UpdateMemberStatus 更新成员状态（含 FSM 校验）。对齐 Python: MemberDao.update_member_status()
// 返回 true 表示成功，false 表示成员不存在或状态转换不合法
func (db *InMemoryTeamDatabase) UpdateMemberStatus(_ context.Context, memberName, teamName, status string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	key := memberKey(memberName, teamName)
	member, exists := db.members[key]
	if !exists {
		return false // 对齐 Python: member not found → False
	}
	// FSM 校验（对齐 Python: is_valid_transition）
	if !IsValidMemberTransition(member.Status, status) {
		return false // 对齐 Python: invalid transition → False
	}
	member.Status = status
	return true
}

// TryTransitionMemberStatus CAS 原子状态转换。对齐 Python: MemberDao.try_transition_member_status()
// 仅当当前状态 == fromStatus 时才更新为 toStatus，否则返回 false
func (db *InMemoryTeamDatabase) TryTransitionMemberStatus(_ context.Context, memberName, teamName, fromStatus, toStatus string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	key := memberKey(memberName, teamName)
	member, exists := db.members[key]
	if !exists {
		return false
	}
	if member.Status != fromStatus {
		return false // 对齐 Python: rowcount == 0 → False (CAS 失败)
	}
	member.Status = toStatus
	return true // 对齐 Python: rowcount == 1 → True (CAS 成功)
}

// ListHumanAgentNames 获取 human_agent 角色的成员名列表。对齐 Python: MemberDao.list_human_agent_names()
func (db *InMemoryTeamDatabase) ListHumanAgentNames(_ context.Context, teamName string) ([]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var result []string
	for _, member := range db.members {
		if member.TeamName == teamName && member.Role == "human_agent" {
			result = append(result, member.MemberName)
		}
	}
	return result, nil
}

// GetMembersMaxUpdatedAt 获取 MAX(updated_at)。对齐 Python: MemberDao.get_members_max_updated_at()
func (db *InMemoryTeamDatabase) GetMembersMaxUpdatedAt(_ context.Context, teamName string) int64 {
	db.mu.Lock()
	defer db.mu.Unlock()
	var maxVal int64
	for _, member := range db.members {
		if member.TeamName == teamName && member.UpdatedAt > maxVal {
			maxVal = member.UpdatedAt
		}
	}
	return maxVal // 对齐 Python: 无数据返回 0
}

// UpdateMemberExecutionStatus 更新执行状态（含 FSM 校验）。
// 对齐 Python: MemberDao.update_member_execution_status()
func (db *InMemoryTeamDatabase) UpdateMemberExecutionStatus(_ context.Context, memberName, teamName, executionStatus string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	key := memberKey(memberName, teamName)
	member, exists := db.members[key]
	if !exists {
		return false
	}
	if !IsValidExecutionTransition(member.ExecutionStatus, executionStatus) {
		return false
	}
	member.ExecutionStatus = executionStatus
	return true
}

// ──────────────────────────── TaskDao 接口实现 ────────────────────────────

// CreateTask 创建单条任务。对齐 Python: TaskDao.create_task()
// 成功返回 true，task_id 冲突返回 false（对齐 Python IntegrityError → False）
func (db *InMemoryTeamDatabase) CreateTask(_ context.Context, task *TeamTaskBase) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, exists := db.tasks[task.TaskID]; exists {
		return false, nil // 对齐 Python IntegrityError → False
	}
	task.UpdatedAt = GetCurrentTime()
	db.tasks[task.TaskID] = task
	return true, nil
}

// GetTask 按 ID 查询任务。对齐 Python: TaskDao.get_task()
func (db *InMemoryTeamDatabase) GetTask(_ context.Context, taskID string) (*TeamTaskBase, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, exists := db.tasks[taskID]
	if !exists {
		return nil, nil // 对齐 Python Optional[TeamTaskBase] → None
	}
	return task, nil
}

// GetTeamTasks 查询团队全部任务。对齐 Python: TaskDao.get_team_tasks()
// status 为空字符串表示不过滤。
func (db *InMemoryTeamDatabase) GetTeamTasks(_ context.Context, teamName, status string) ([]*TeamTaskBase, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var result []*TeamTaskBase
	for _, task := range db.tasks {
		if task.TeamName != teamName {
			continue
		}
		if status != "" && task.Status != status {
			continue
		}
		result = append(result, task)
	}
	return result, nil
}

// GetTasksByAssignee 查询成员的任务。对齐 Python: TaskDao.get_tasks_by_assignee()
// status 为空字符串表示不过滤。
func (db *InMemoryTeamDatabase) GetTasksByAssignee(_ context.Context, teamName, assignee, status string) ([]*TeamTaskBase, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var result []*TeamTaskBase
	for _, task := range db.tasks {
		if task.TeamName != teamName || task.Assignee != assignee {
			continue
		}
		if status != "" && task.Status != status {
			continue
		}
		result = append(result, task)
	}
	return result, nil
}

// ClaimTask 认领任务：设置 assignee + PENDING→CLAIMED FSM 校验。
// 对齐 Python: TaskDao.claim_task()
func (db *InMemoryTeamDatabase) ClaimTask(_ context.Context, taskID, assignee string) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, exists := db.tasks[taskID]
	if !exists {
		return false, nil
	}
	if !IsValidTaskTransition(task.Status, fsm.TaskStatusClaimed) {
		return false, nil // 对齐 Python: invalid transition → False
	}
	task.Status = fsm.TaskStatusClaimed
	task.Assignee = assignee
	task.UpdatedAt = GetCurrentTime()
	return true, nil
}

// ResetTask 重置任务：CLAIMED→PENDING，清除 assignee。
// 对齐 Python: TaskDao.reset_task()
func (db *InMemoryTeamDatabase) ResetTask(_ context.Context, taskID string) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, exists := db.tasks[taskID]
	if !exists {
		return false, nil
	}
	if !IsValidTaskTransition(task.Status, fsm.TaskStatusPending) {
		return false, nil
	}
	task.Status = fsm.TaskStatusPending
	task.Assignee = ""
	task.UpdatedAt = GetCurrentTime()
	return true, nil
}

// ApprovePlanTask 计划审批：CLAIMED→PLAN_APPROVED FSM 校验。
// 对齐 Python: TaskDao.approve_plan_task()
func (db *InMemoryTeamDatabase) ApprovePlanTask(_ context.Context, taskID string) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, exists := db.tasks[taskID]
	if !exists {
		return false, nil
	}
	if !IsValidTaskTransition(task.Status, fsm.TaskStatusPlanApproved) {
		return false, nil
	}
	task.Status = fsm.TaskStatusPlanApproved
	task.UpdatedAt = GetCurrentTime()
	return true, nil
}

// UpdateTask 更新标题/内容。CLAIMED/PLAN_APPROVED 状态下禁止编辑。
// 对齐 Python: TaskDao.update_task()
func (db *InMemoryTeamDatabase) UpdateTask(_ context.Context, taskID, title, content string) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, exists := db.tasks[taskID]
	if !exists {
		return false, nil
	}
	// 对齐 Python: PENDING/BLOCKED 状态才允许编辑；CLAIMED/PLAN_APPROVED 禁止
	if task.Status == fsm.TaskStatusClaimed || task.Status == fsm.TaskStatusPlanApproved {
		return false, nil
	}
	task.Title = title
	task.Content = content
	task.UpdatedAt = GetCurrentTime()
	return true, nil
}

// DeleteTask 删除任务（级联删除依赖边）。对齐 Python: TaskDao.delete_task()
func (db *InMemoryTeamDatabase) DeleteTask(_ context.Context, taskID string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.tasks, taskID)
	// 级联删除涉及此任务的依赖边（作为上游或下游）
	for key, dep := range db.deps {
		if dep.TaskID == taskID || dep.DependsOnID == taskID {
			delete(db.deps, key)
		}
	}
	return nil
}

// GetTaskDependencies 查询任务依赖。对齐 Python: TaskDao.get_task_dependencies()
func (db *InMemoryTeamDatabase) GetTaskDependencies(_ context.Context, taskID string) ([]*TeamTaskDependencyBase, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var result []*TeamTaskDependencyBase
	for _, dep := range db.deps {
		if dep.TaskID == taskID {
			result = append(result, dep)
		}
	}
	return result, nil
}

// GetUnresolvedDependenciesCount 未解决依赖计数。对齐 Python: TaskDao.get_unresolved_dependencies_count()
func (db *InMemoryTeamDatabase) GetUnresolvedDependenciesCount(_ context.Context, taskID string) (int, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	count := 0
	for _, dep := range db.deps {
		if dep.TaskID == taskID && !dep.Resolved {
			count++
		}
	}
	return count, nil
}

// GetTasksDependingOn 查询下游依赖任务。对齐 Python: TaskDao.get_tasks_depending_on()
// 返回 taskID 阻塞的任务列表（即 deps 中 depends_on_task_id == taskID 的下游任务）
func (db *InMemoryTeamDatabase) GetTasksDependingOn(_ context.Context, taskID string) ([]*TeamTaskBase, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var result []*TeamTaskBase
	for _, dep := range db.deps {
		if dep.DependsOnID == taskID {
			task, exists := db.tasks[dep.TaskID]
			if exists {
				result = append(result, task)
			}
		}
	}
	return result, nil
}

// UpdateTaskStatus 更新任务状态。完成时自动解除下游依赖并刷新 BLOCKED→PENDING。
// 对齐 Python: TaskDao.update_task_status()
func (db *InMemoryTeamDatabase) UpdateTaskStatus(_ context.Context, taskID, newStatus string) ([]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	task, exists := db.tasks[taskID]
	if !exists {
		return nil, nil
	}
	if !IsValidTaskTransition(task.Status, newStatus) {
		return nil, nil
	}

	// 如果是终态（COMPLETED/CANCELLED），执行终止传播
	if newStatus == fsm.TaskStatusCompleted || newStatus == fsm.TaskStatusCancelled {
		return db.terminateTaskInSession(taskID, newStatus)
	}

	// 非终态转换：直接更新状态
	task.Status = newStatus
	task.UpdatedAt = GetCurrentTime()
	return nil, nil
}

// CancelTask 取消任务（原子终止传播）。对齐 Python: TaskDao.cancel_task()
func (db *InMemoryTeamDatabase) CancelTask(_ context.Context, taskID string) ([]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.terminateTaskInSession(taskID, fsm.TaskStatusCancelled)
}

// CompleteTask 完成任务（原子终止传播）。对齐 Python: TaskDao.complete_task()
func (db *InMemoryTeamDatabase) CompleteTask(_ context.Context, taskID string) ([]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.terminateTaskInSession(taskID, fsm.TaskStatusCompleted)
}

// CancelAllTasks 批量取消（原子终止传播）。对齐 Python: TaskDao.cancel_all_tasks()
// skipAssignees 列表中的 assignee 对应的任务不会被取消。
func (db *InMemoryTeamDatabase) CancelAllTasks(_ context.Context, teamName string, skipAssignees []string) ([]*TeamTaskBase, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	skipSet := make(map[string]bool, len(skipAssignees))
	for _, a := range skipAssignees {
		skipSet[a] = true
	}

	var cancelled []*TeamTaskBase
	for _, task := range db.tasks {
		if task.TeamName != teamName {
			continue
		}
		// 终态跳过
		if task.Status == fsm.TaskStatusCompleted || task.Status == fsm.TaskStatusCancelled {
			continue
		}
		// skipAssignees 跳过（对齐 Python: skip tasks assigned to specified members）
		if skipSet[task.Assignee] {
			continue
		}
		db.terminateTaskInSession(task.TaskID, fsm.TaskStatusCancelled)
		cancelled = append(cancelled, task)
	}
	return cancelled, nil
}

// VerifyAndFixTaskConsistency 一致性修复：扫描 BLOCKED 任务并刷新状态。
// 对齐 Python: TaskDao.verify_and_fix_task_consistency()
func (db *InMemoryTeamDatabase) VerifyAndFixTaskConsistency(_ context.Context, teamName string) ([]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.refreshTaskStatuses(teamName), nil
}

// MutateDependencyGraph 原子图变更：5 步管线。
// 对齐 Python: mutate_dependency_graph()
func (db *InMemoryTeamDatabase) MutateDependencyGraph(_ context.Context, teamName string, newTasks []NewTaskSpec, addEdges []EdgeSpec) GraphMutationResult {
	db.mu.Lock()
	defer db.mu.Unlock()

	mc := &mutationContext{db: db, teamName: teamName, newTasks: newTasks, addEdges: addEdges}

	// 步骤1：插入新任务行，检测 task_id 重复
	mc.stageNewTasks()
	if mc.failReason != "" {
		return GraphMutationResult{Ok: false, Reason: mc.failReason}
	}

	// 步骤2：加载边端点，拒绝缺失/终态/已执行源
	mc.loadEndpointsAndValidate()
	if mc.failReason != "" {
		return GraphMutationResult{Ok: false, Reason: mc.failReason}
	}

	// 步骤3：构建后变更邻接表，检测环路
	mc.checkCycleAndComputeNewEdges()
	if mc.failReason != "" {
		return GraphMutationResult{Ok: false, Reason: mc.failReason}
	}

	// 步骤4：插入依赖边行
	mc.applyNewEdges()

	// 步骤5：刷新 PENDING↔BLOCKED 状态
	mc.refreshStatus()

	return GraphMutationResult{Ok: true, RefreshedTasks: mc.refreshedIDs}
}

// AddTaskWithBidirectionalDependencies 带双向依赖创建任务。
// 对齐 Python: add_task_with_bidirectional_dependencies()
func (db *InMemoryTeamDatabase) AddTaskWithBidirectionalDependencies(_ context.Context, teamName string, task *TeamTaskBase, dependsOnIDs []string) GraphMutationResult {
	// 构建 NewTaskSpec
	newTaskSpec := NewTaskSpec{
		TaskID:        task.TaskID,
		Title:         task.Title,
		Content:       task.Content,
		InitialStatus: task.Status,
	}
	if newTaskSpec.InitialStatus == "" {
		newTaskSpec.InitialStatus = fsm.TaskStatusPending
	}

	// 构建 EdgeSpec：task 依赖 dependsOnIDs 中的每个上游任务
	edges := make([]EdgeSpec, 0, len(dependsOnIDs))
	for _, upstreamID := range dependsOnIDs {
		edges = append(edges, EdgeSpec{TaskID: task.TaskID, DependsOnID: upstreamID})
	}

	return db.MutateDependencyGraph(context.Background(), teamName, []NewTaskSpec{newTaskSpec}, edges)
}

// ──────────────────────────── 依赖图管线 ────────────────────────────

// mutationContext 依赖图变更管线共享上下文。
// 替代 Python _MutationFailure 异常信号，使用 failReason flag 模式。
type mutationContext struct {
	db       *InMemoryTeamDatabase
	teamName string
	newTasks []NewTaskSpec
	addEdges []EdgeSpec

	// 步骤间共享数据（闭包操作）
	stagedTasks   map[string]*TeamTaskBase   // 步骤1产出：已插入的新任务
	endpointTasks map[string]*TeamTaskBase   // 步骤2产出：边端点对应的任务
	newEdgeRows   []TeamTaskDependencyBase   // 步骤4产出：待插入的依赖边行
	refreshedIDs  []string                   // 步骤5产出：状态刷新的 task IDs

	// 失败标记（替代 Python _MutationFailure）
	failReason string
}

// stageNewTasks 步骤1：插入新任务行，检测 task_id 重复。
// 对齐 Python: _stage_new_tasks()
func (mc *mutationContext) stageNewTasks() {
	mc.stagedTasks = make(map[string]*TeamTaskBase)
	for _, spec := range mc.newTasks {
		if _, exists := mc.db.tasks[spec.TaskID]; exists {
			mc.failReason = "task_id already exists: " + spec.TaskID
			return
		}
		// 检查已 staged 的新任务中是否也有重复
		if _, exists := mc.stagedTasks[spec.TaskID]; exists {
			mc.failReason = "duplicate new task_id in spec: " + spec.TaskID
			return
		}
		status := spec.InitialStatus
		if status == "" {
			status = fsm.TaskStatusPending // 默认 PENDING
		}
		task := &TeamTaskBase{
			TaskID:    spec.TaskID,
			TeamName:  mc.teamName,
			Title:     spec.Title,
			Content:   spec.Content,
			Status:    status,
			UpdatedAt: GetCurrentTime(),
		}
		mc.db.tasks[spec.TaskID] = task
		mc.stagedTasks[spec.TaskID] = task
	}
}

// loadEndpointsAndValidate 步骤2：加载边端点，拒绝缺失/终态目标。
// 对齐 Python: _load_endpoints_and_validate()
func (mc *mutationContext) loadEndpointsAndValidate() {
	mc.endpointTasks = make(map[string]*TeamTaskBase)
	for _, edge := range mc.addEdges {
		// 加载下游任务（被阻塞的）
		downstream, downExists := mc.db.tasks[edge.TaskID]
		if !downExists {
			mc.failReason = "edge endpoint not found: " + edge.TaskID
			mc.rollbackStagedTasks()
			return
		}
		mc.endpointTasks[edge.TaskID] = downstream

		// 加载上游任务（阻塞源）
		upstream, upExists := mc.db.tasks[edge.DependsOnID]
		if !upExists {
			mc.failReason = "edge endpoint not found: " + edge.DependsOnID
			mc.rollbackStagedTasks()
			return
		}
		mc.endpointTasks[edge.DependsOnID] = upstream

		// 拒绝终态目标：上游是 COMPLETED/CANCELLED 时不允许添加依赖
		if upstream.Status == fsm.TaskStatusCompleted || upstream.Status == fsm.TaskStatusCancelled {
			mc.failReason = "cannot add dependency on terminal task: " + edge.DependsOnID
			mc.rollbackStagedTasks()
			return
		}
	}
}

// checkCycleAndComputeNewEdges 步骤3：构建后变更邻接表，检测环路。
// 对齐 Python: _check_cycle_and_compute_new_edges()
func (mc *mutationContext) checkCycleAndComputeNewEdges() {
	// 构建后变更邻接表：downstream → [upstream1, upstream2, ...]
	adj := make(map[string][]string)
	// 加载已有边
	for _, dep := range mc.db.deps {
		if dep.TeamName == mc.teamName {
			adj[dep.TaskID] = append(adj[dep.TaskID], dep.DependsOnID)
		}
	}
	// 加载新边
	for _, edge := range mc.addEdges {
		adj[edge.TaskID] = append(adj[edge.TaskID], edge.DependsOnID)
	}

	// 检测环路：DFS
	visited := make(map[string]int) // 0=未访问, 1=正在访问, 2=已完成
	for node := range adj {
		if mc.hasCycleDFS(node, adj, visited) {
			mc.failReason = "dependency cycle detected involving: " + node
			mc.rollbackStagedTasks()
			return
		}
	}

	// 计算新边行
	mc.newEdgeRows = make([]TeamTaskDependencyBase, 0, len(mc.addEdges))
	for _, edge := range mc.addEdges {
		upstream := mc.endpointTasks[edge.DependsOnID]
		// 终态依赖初始 resolved=True（对齐 Python）
		resolved := upstream.Status == fsm.TaskStatusCompleted || upstream.Status == fsm.TaskStatusCancelled
		mc.newEdgeRows = append(mc.newEdgeRows, TeamTaskDependencyBase{
			TaskID:      edge.TaskID,
			DependsOnID: edge.DependsOnID,
			TeamName:    mc.teamName,
			Resolved:    resolved,
		})
	}
}

// hasCycleDFS DFS 检测环路。
func (mc *mutationContext) hasCycleDFS(node string, adj map[string][]string, visited map[string]int) bool {
	if visited[node] == 1 {
		return true // 环路
	}
	if visited[node] == 2 {
		return false // 已完成
	}
	visited[node] = 1 // 正在访问
	for _, neighbor := range adj[node] {
		if mc.hasCycleDFS(neighbor, adj, visited) {
			return true
		}
	}
	visited[node] = 2 // 已完成
	return false
}

// applyNewEdges 步骤4：插入依赖边行。
func (mc *mutationContext) applyNewEdges() {
	for i := range mc.newEdgeRows {
		edgeRow := mc.newEdgeRows[i] // 复制到新变量，避免循环变量地址共享
		mc.db.deps[depKey(edgeRow.TaskID, edgeRow.DependsOnID)] = &edgeRow
	}
}

// refreshStatus 步骤5：刷新 PENDING↔BLOCKED 状态。
// 对齐 Python: _refresh_status_in_session()
func (mc *mutationContext) refreshStatus() {
	mc.refreshedIDs = mc.db.refreshTaskStatuses(mc.teamName)
}

// rollbackStagedTasks 回滚步骤1插入的新任务。
func (mc *mutationContext) rollbackStagedTasks() {
	for id := range mc.stagedTasks {
		delete(mc.db.tasks, id)
	}
}

// ──────────────────────────── TaskDao 内部方法 ────────────────────────────

// terminateTaskInSession 原子终止传播：终止任务 + 标记下游 resolved + 刷新状态。
// 对齐 Python: _terminate_task_in_session()
// 一次 Lock 内完成所有操作（由调用方持锁，此方法不加锁）。
func (db *InMemoryTeamDatabase) terminateTaskInSession(taskID, terminalStatus string) ([]string, error) {
	task, exists := db.tasks[taskID]
	if !exists {
		return nil, nil
	}
	if !IsValidTaskTransition(task.Status, terminalStatus) {
		return nil, nil
	}

	// 1. 将任务设为终态
	task.Status = terminalStatus
	task.UpdatedAt = GetCurrentTime()

	// 2. 批量标记下游依赖为 resolved=True
	for key, dep := range db.deps {
		if dep.DependsOnID == taskID && !dep.Resolved {
			dep.Resolved = true
			db.deps[key] = dep
		}
	}

	// 3. 对所有下游任务执行状态刷新（可能解除阻塞）
	// 收集下游任务 ID
	var downstreamIDs []string
	for _, dep := range db.deps {
		if dep.DependsOnID == taskID {
			downstreamIDs = append(downstreamIDs, dep.TaskID)
		}
	}

	// 刷新每个下游任务的状态
	var refreshed []string
	for _, downID := range downstreamIDs {
		downTask, downExists := db.tasks[downID]
		if !downExists {
			continue
		}
		// 重新计算下游任务的未解决依赖数
		if downTask.Status == fsm.TaskStatusBlocked {
			unresolved := 0
			for _, dep := range db.deps {
				if dep.TaskID == downID && !dep.Resolved {
					unresolved++
				}
			}
			if unresolved == 0 {
				downTask.Status = fsm.TaskStatusPending
				downTask.UpdatedAt = GetCurrentTime()
				refreshed = append(refreshed, downID)
			}
		}
	}

	return refreshed, nil
}

// refreshTaskStatuses 刷新团队内所有任务的 PENDING↔BLOCKED 状态。
// 对齐 Python: _refresh_status_in_session()
// PENDING + 有未解决依赖 → BLOCKED
// BLOCKED + 无未解决依赖 → PENDING
func (db *InMemoryTeamDatabase) refreshTaskStatuses(teamName string) []string {
	var refreshed []string
	for _, task := range db.tasks {
		if task.TeamName != teamName {
			continue
		}
		unresolvedCount := 0
		for _, dep := range db.deps {
			if dep.TaskID == task.TaskID && dep.TeamName == teamName && !dep.Resolved {
				unresolvedCount++
			}
		}

		oldStatus := task.Status
		if task.Status == fsm.TaskStatusPending && unresolvedCount > 0 {
			task.Status = fsm.TaskStatusBlocked
		} else if task.Status == fsm.TaskStatusBlocked && unresolvedCount == 0 {
			task.Status = fsm.TaskStatusPending
		}
		if oldStatus != task.Status {
			task.UpdatedAt = GetCurrentTime()
			refreshed = append(refreshed, task.TaskID)
		}
	}
	return refreshed
}

// ──────────────────────────── 编译期接口断言 ────────────────────────────

var (
	_ TeamDatabase = (*InMemoryTeamDatabase)(nil) // InMemoryTeamDatabase 必须满足 TeamDatabase 接口
	_ TeamDao      = (*InMemoryTeamDatabase)(nil) // InMemoryTeamDatabase 必须满足 TeamDao 接口
	_ MemberDao    = (*InMemoryTeamDatabase)(nil) // InMemoryTeamDatabase 必须满足 MemberDao 接口
	_ TaskDao      = (*InMemoryTeamDatabase)(nil) // InMemoryTeamDatabase 必须满足 TaskDao 接口
)
