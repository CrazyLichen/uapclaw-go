package database

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// TeamDatabase 团队数据库门面接口。
// 对齐 Python: TeamDatabase (openjiuwen/agent_teams/tools/database/__init__.py)
// 拥有引擎生命周期和跨表事务，单表操作通过 DAO 属性（team/member/task/message）调用。
type TeamDatabase interface {
	// Initialize 初始化数据库引擎、创建表、接入 DAO。
	Initialize(ctx context.Context) error
	// CreateCurSessionTables 创建当前会话动态表。
	CreateCurSessionTables(ctx context.Context) error
	// DropCurSessionTables 删除当前会话动态表。
	DropCurSessionTables(ctx context.Context) error
	// CleanupAllRuntimeState 清理所有运行时状态（删除动态表 + 清空静态表）。
	CleanupAllRuntimeState(ctx context.Context) (droppedTables []string, droppedDirs []string, err error)
	// DropSessionTablesByID 按 sessionID 删除动态表。
	DropSessionTablesByID(ctx context.Context, sessionID string) ([]string, error)
	// ForceDeleteTeamSession 跨表拆卸：删 team_info 行 + 删该 team 下所有成员 + drop 会话动态表。
	// 对齐 Python: force_delete_team_session(team_name)
	ForceDeleteTeamSession(ctx context.Context, teamName string) bool
	// Close 关闭数据库引擎并释放连接。
	Close() error
	// Team 返回 TeamDao（对齐 Python self.team = TeamDao(...)）
	Team() TeamDao
	// Member 返回 MemberDao（对齐 Python self.member = MemberDao(...)）
	Member() MemberDao
	// Task 返回 TaskDao。⤵️ 9.65a-2 回填具体方法
	Task() TaskDao
	// Message 返回 MessageDao。⤵️ 9.65a-3 回填具体方法
	Message() MessageDao
}

// TeamDao 团队 DAO 接口。
// 对齐 Python: TeamDao (openjiuwen/agent_teams/tools/database/team_dao.py)
type TeamDao interface {
	// CreateTeam 创建团队。返回 true 表示成功，false 表示团队已存在（对齐 Python IntegrityError → False）。
	// 参数 prompt 为新增字段（Python 原有）。
	CreateTeam(ctx context.Context, teamName, displayName, leaderMemberName, desc, prompt string) bool
	// GetTeam 获取团队信息。返回 nil 表示团队不存在（对齐 Python Optional[Team]）。
	GetTeam(ctx context.Context, teamName string) (*Team, error)
	// TeamExists 团队是否存在。对齐 Python: team_exists()
	TeamExists(ctx context.Context, teamName string) bool
	// DeleteTeam 删除团队（级联删除成员）。返回 true 表示删除成功，false 表示团队不存在。
	// 对齐 Python: delete_team() → bool
	DeleteTeam(ctx context.Context, teamName string) bool
	// GetTeamUpdatedAt 获取团队 updated_at 毫秒时间戳（用于变更检测）。
	// 对齐 Python: get_team_updated_at() → int
	GetTeamUpdatedAt(ctx context.Context, teamName string) int64
}

// MemberDao 成员 DAO 接口。
// 对齐 Python: MemberDao (openjiuwen/agent_teams/tools/database/member_dao.py)
type MemberDao interface {
	// CreateMember 创建成员。返回 true 表示成功，false 表示成员已存在（对齐 Python IntegrityError → False）。
	// 对齐 Python create_member 的全部 11 参数。
	CreateMember(ctx context.Context, memberName, teamName, displayName, agentCard, status, role, desc, executionStatus, mode, prompt, modelRefJSON string) bool
	// GetMember 获取成员信息。返回 nil 表示成员不存在（对齐 Python Optional[TeamMember]）。
	// 参数顺序 (memberName, teamName) 对齐 Python。
	GetMember(ctx context.Context, memberName, teamName string) (*TeamMember, error)
	// GetTeamMembers 获取团队成员列表，可选按 status 过滤。
	// 对齐 Python: get_team_members(team, status=None)，空字符串 status 表示不过滤。
	GetTeamMembers(ctx context.Context, teamName string, status string) ([]*TeamMember, error)
	// UpdateMemberStatus 更新成员状态（含 FSM 校验）。返回 true 表示成功，false 表示成员不存在或转换不合法。
	// 对齐 Python: update_member_status() → bool
	UpdateMemberStatus(ctx context.Context, memberName, teamName, status string) bool
	// TryTransitionMemberStatus CAS 原子状态转换（对齐 Python try_transition_member_status）。
	// 仅当当前状态 == fromStatus 时才更新为 toStatus，否则返回 false。
	// 使用 string 参数避免 database→schema 循环依赖。
	TryTransitionMemberStatus(ctx context.Context, memberName, teamName, fromStatus, toStatus string) bool
	// ListHumanAgentNames 获取 human_agent 角色的成员名列表（HITT 名册重建）。
	// 对齐 Python: list_human_agent_names()
	ListHumanAgentNames(ctx context.Context, teamName string) ([]string, error)
	// GetMembersMaxUpdatedAt 获取 MAX(updated_at)（成员变更检测）。
	// 对齐 Python: get_members_max_updated_at() → int
	GetMembersMaxUpdatedAt(ctx context.Context, teamName string) int64
	// UpdateMemberExecutionStatus 更新执行状态（含 FSM 校验）。
	// 对齐 Python: update_member_execution_status() → bool
	UpdateMemberExecutionStatus(ctx context.Context, memberName, teamName, executionStatus string) bool
}

// TaskDao 任务 DAO 接口。
// 对齐 Python: TaskDao (openjiuwen/agent_teams/tools/database/task_dao.py)
type TaskDao interface {
	// CreateTask 创建单条任务。返回 true 表示成功，false 表示 task_id 冲突（对齐 Python IntegrityError → False）。
	CreateTask(ctx context.Context, task *TeamTaskBase) (bool, error)
	// GetTask 按 ID 查询任务。返回 nil 表示不存在（对齐 Python Optional[TeamTaskBase]）。
	GetTask(ctx context.Context, taskID string) (*TeamTaskBase, error)
	// GetTeamTasks 查询团队全部任务。status 为空字符串表示不过滤。
	GetTeamTasks(ctx context.Context, teamName, status string) ([]*TeamTaskBase, error)
	// GetTasksByAssignee 查询成员的任务。status 为空字符串表示不过滤。
	GetTasksByAssignee(ctx context.Context, teamName, assignee, status string) ([]*TeamTaskBase, error)
	// ClaimTask 认领任务：设置 assignee + PENDING→CLAIMED FSM 校验。
	// 对齐 Python: claim_task() → bool
	ClaimTask(ctx context.Context, taskID, assignee string) (bool, error)
	// ResetTask 重置任务：CLAIMED→PENDING，清除 assignee。
	// 对齐 Python: reset_task() → bool
	ResetTask(ctx context.Context, taskID string) (bool, error)
	// ApprovePlanTask 计划审批：CLAIMED→PLAN_APPROVED FSM 校验。
	// 对齐 Python: approve_plan_task() → bool
	ApprovePlanTask(ctx context.Context, taskID string) (bool, error)
	// UpdateTaskStatus 更新任务状态。完成时自动解除下游依赖并刷新 BLOCKED→PENDING。
	// 返回刷新的 task ID 列表。
	UpdateTaskStatus(ctx context.Context, taskID, newStatus string) ([]string, error)
	// UpdateTask 更新标题/内容。CLAIMED/PLAN_APPROVED 状态下禁止编辑。
	// 对齐 Python: update_task() → bool
	UpdateTask(ctx context.Context, taskID, title, content string) (bool, error)
	// MutateDependencyGraph 原子图变更：5 步管线。
	// 对齐 Python: mutate_dependency_graph()
	MutateDependencyGraph(ctx context.Context, teamName string, newTasks []NewTaskSpec, addEdges []EdgeSpec) GraphMutationResult
	// AddTaskWithBidirectionalDependencies 带双向依赖创建任务。委托 MutateDependencyGraph。
	// 对齐 Python: add_task_with_bidirectional_dependencies()
	AddTaskWithBidirectionalDependencies(ctx context.Context, teamName string, task *TeamTaskBase, dependsOnIDs []string) GraphMutationResult
	// GetTaskDependencies 查询任务依赖。
	GetTaskDependencies(ctx context.Context, taskID string) ([]*TeamTaskDependencyBase, error)
	// GetUnresolvedDependenciesCount 未解决依赖计数。
	GetUnresolvedDependenciesCount(ctx context.Context, taskID string) (int, error)
	// GetTasksDependingOn 查询下游依赖任务（即被 taskID 阻塞的任务）。
	GetTasksDependingOn(ctx context.Context, taskID string) ([]*TeamTaskBase, error)
	// DeleteTask 删除任务。
	DeleteTask(ctx context.Context, taskID string) error
	// CancelTask 取消任务（原子终止传播），返回 unblocked task IDs。
	CancelTask(ctx context.Context, taskID string) ([]string, error)
	// CancelAllTasks 批量取消（原子终止传播），支持 skipAssignees 过滤。
	CancelAllTasks(ctx context.Context, teamName string, skipAssignees []string) ([]*TeamTaskBase, error)
	// CompleteTask 完成任务（原子终止传播），返回 unblocked task IDs。
	CompleteTask(ctx context.Context, taskID string) ([]string, error)
	// VerifyAndFixTaskConsistency 一致性修复：扫描 BLOCKED 任务并刷新状态。
	VerifyAndFixTaskConsistency(ctx context.Context, teamName string) ([]string, error)
}

// MessageDao 消息 DAO 接口。⤵️ 9.65a-3 回填具体方法签名
type MessageDao interface{}
