package database

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamDatabase 团队数据库门面接口。
// Python: TeamDatabase (openjiuwen/agent_teams/tools/database/__init__.py)
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
	// Python: force_delete_team_session(team_name)
	ForceDeleteTeamSession(ctx context.Context, teamName string) bool
	// Close 关闭数据库引擎并释放连接。
	Close() error
	// Team 返回 TeamDao（对齐 Python self.team = TeamDao(...)）
	Team() TeamDao
	// Member 返回 MemberDao（对齐 Python self.member = MemberDao(...)）
	Member() MemberDao
	// Task 返回 TaskDao。
	Task() TaskDao
	// Message 返回 MessageDao。
	Message() MessageDao
}

// TeamDao 团队 DAO 接口。
// Python: TeamDao (openjiuwen/agent_teams/tools/database/team_dao.py)
type TeamDao interface {
	// CreateTeam 创建团队。返回 true 表示成功，false 表示团队已存在（对齐 Python IntegrityError → False）。
	// 参数 prompt 为新增字段（Python 原有）。
	// dispatchMode 与 enableTaskVerification 记录本次实例的能力取值
	// （Python team_dao.py L31-32 同序）。
	CreateTeam(ctx context.Context, teamName, displayName, leaderMemberName, desc, prompt string,
		dispatchMode string, enableTaskVerification bool) bool
	// GetTeam 获取团队信息。返回 nil 表示团队不存在（对齐 Python Optional[Team]）。
	GetTeam(ctx context.Context, teamName string) (*Team, error)
	// TeamExists 团队是否存在。对齐 Python: team_exists()
	TeamExists(ctx context.Context, teamName string) bool
	// DeleteTeam 删除团队（级联删除成员）。返回 true 表示删除成功，false 表示团队不存在。
	// Python: delete_team() → bool
	DeleteTeam(ctx context.Context, teamName string) bool
	// GetTeamUpdatedAt 获取团队 updated_at 毫秒时间戳（用于变更检测）。
	// Python: get_team_updated_at() → int
	GetTeamUpdatedAt(ctx context.Context, teamName string) int64
}

// MemberDao 成员 DAO 接口。
// Python: MemberDao (openjiuwen/agent_teams/tools/database/member_dao.py)
type MemberDao interface {
	// CreateMember 创建成员。返回 true 表示成功，false 表示成员已存在（对齐 Python IntegrityError → False）。
	// Python: create_member 的全部 11 参数。
	CreateMember(ctx context.Context, memberName, teamName, displayName, agentCard, status, role, desc, executionStatus, mode, prompt, options string) bool
	// GetMember 获取成员信息。返回 nil 表示成员不存在（对齐 Python Optional[TeamMember]）。
	// 参数顺序 (memberName, teamName) 对齐 Python。
	GetMember(ctx context.Context, memberName, teamName string) (*TeamMember, error)
	// GetTeamMembers 获取团队成员列表，可选按 status 过滤。
	// Python: get_team_members(team, status=None)，空字符串 status 表示不过滤。
	GetTeamMembers(ctx context.Context, teamName string, status string) ([]*TeamMember, error)
	// GetMemberStatus 窄投影：只取成员当前持久化状态，未知返回 ("", false)。
	//
	// 协调层的 harness 输入门卫只需要状态，不需要拉整行（agent_card /
	// prompt / options）。
	//
	// Python: get_member_status (member_dao.py)
	GetMemberStatus(ctx context.Context, teamName, memberName string) (string, bool)
	// DeleteUnstartedMember 删除一条尚未被触碰的成员行（status = UNSTARTED）。
	//
	// DAO 里唯一的成员删除路径，且带状态守卫：存在的意义是回滚 create_task
	// 内联成员——任务图没落地时把刚写的成员行撤掉。离开 UNSTARTED 的行已经绑了
	// 运行时/邮箱/任务，绝不在这里删。
	//
	// Python: delete_unstarted_member (member_dao.py:397-415)
	DeleteUnstartedMember(ctx context.Context, memberName, teamName string) bool

	// UpdateMemberStatus 更新成员状态（含 FSM 校验）。返回 true 表示成功，false 表示成员不存在或转换不合法。
	// Python: update_member_status() → bool
	UpdateMemberStatus(ctx context.Context, memberName, teamName, status string) bool
	// TryTransitionMemberStatus CAS 原子状态转换（对齐 Python try_transition_member_status）。
	// 仅当当前状态 == fromStatus 时才更新为 toStatus，否则返回 false。
	// 使用 string 参数避免 database→schema 循环依赖。
	TryTransitionMemberStatus(ctx context.Context, memberName, teamName, fromStatus, toStatus string) bool
	// ListRetirableMembers 投影携带退役策略且空闲（READY）的成员
	// (member_name, status, lifetime)。供调度器 retirement scan（F_118）。
	// 只取 READY：BUSY 成员在轮中，退役请求落到那里会让它正在说的话的回复
	// 变成搁浅邮件（轮结束、流关闭、没人再唤醒它）。READY 状态事件在轮结束
	// 瞬间重新触发扫描。status 过滤在 SQL；lifetime 在 options JSON 里，
	// 策略过滤在收窄后的行集上做。
	// Python: list_retirable_members（member_dao.py L338-367）
	ListRetirableMembers(ctx context.Context, teamName string) ([]RetirableMemberRow, error)
	// ListShutdownMembers 投影 SHUTDOWN 成员名集合。供调度器每次唤醒的
	// 搁浅任务检查，只读名字——永不读序列化卡片或私有提示词。
	// Python: list_shutdown_members（member_dao.py L369-381）
	ListShutdownMembers(ctx context.Context, teamName string) (map[string]bool, error)
	// ListHumanAgentNames 获取 human_agent 角色的成员名列表（HITT 名册重建）。
	// Python: list_human_agent_names()
	ListHumanAgentNames(ctx context.Context, teamName string) ([]string, error)
	// UpdateMemberServedModel 记录模型组把成员路由到 servedModel。
	// 仅当模型与最新一条不同才追加（AppendMemberServedModel 语义）。
	// 返回行是否存在（false 让调用方下次路由再试——leader 的首次路由可能
	// 发生在 build_team 写入成员行之前）。
	// Python: update_member_served_model (member_dao.py:807)
	UpdateMemberServedModel(ctx context.Context, memberName, teamName, servedModel string) bool
	// PromoteMemberFallbackModel 把成员持久化的兜底模型提为当前模型引用。
	// Python: MemberDao.promote_member_fallback_model (member_dao.py:844-868)
	PromoteMemberFallbackModel(ctx context.Context, memberName, teamName string) bool
	// GetMembersMaxUpdatedAt 获取 MAX(updated_at)（成员变更检测）。
	// Python: get_members_max_updated_at() → int
	GetMembersMaxUpdatedAt(ctx context.Context, teamName string) int64
	// UpdateMemberExecutionStatus 更新执行状态（含 FSM 校验）。
	// Python: update_member_execution_status() → bool
	UpdateMemberExecutionStatus(ctx context.Context, memberName, teamName, executionStatus string) bool
	// ResetPausedMemberExecutionStatus 批量把已暂停成员的非 IDLE 执行状态复位为 IDLE。
	//
	// 只用于生命周期收尾（暂停后成员运行时已停），不走轮次驱动的 FSM 校验；
	// 与 Python reset_paused_member_execution_status 的条件一致：
	// status=PAUSED 且 execution_status != IDLE。
	//
	// Python: reset_paused_member_execution_status (member_dao.py:649-681)
	ResetPausedMemberExecutionStatus(ctx context.Context, teamName string, memberNames []string) int
}

// TaskDao 任务 DAO 接口。
// Python: TaskDao (openjiuwen/agent_teams/tools/database/task_dao.py)
type TaskDao interface {
	// CreateTask 创建单条任务。返回 true 表示成功，false 表示 task_id 冲突（对齐 Python IntegrityError → False）。
	CreateTask(ctx context.Context, task *TeamTaskBase) (bool, error)
	// GetTask 按 ID 查询任务。返回 nil 表示不存在（对齐 Python Optional[TeamTaskBase]）。
	GetTask(ctx context.Context, taskID string) (*TeamTaskBase, error)
	// GetTeamTasks 查询团队全部任务。status 为空字符串表示不过滤。
	GetTeamTasks(ctx context.Context, teamName, status string) ([]*TeamTaskBase, error)
	// GetTasksByAssignee 查询成员的任务。status 为空字符串表示不过滤。
	GetTasksByAssignee(ctx context.Context, teamName, assignee, status string) ([]*TeamTaskBase, error)
	// OpenTaskCountsByAssignee 按 assignee 统计非终态任务数（一条分组查询）。
	//
	// 非终态 = TASK_TERMINAL_STATUSES 之外的全部状态（PENDING/BLOCKED/PLANNING/
	// IN_PROGRESS/IN_REVIEW 都算）：任务在评审或依赖队列里，成员名下就还有活。
	// 没有开放任务的成员不会出现在结果里。
	//
	// Python: open_task_counts_by_assignee (task_dao.py:685-705)
	OpenTaskCountsByAssignee(ctx context.Context, teamName string) (map[string]int, error)
	// GetUnseenEdits 查询成员名下「改过但未读」的任务（revision > observed_revision）。
	// Python: get_unseen_edits(team_name, assignee) (task_dao.py L763-779)
	// 只查非终态任务；正文可能被溢出占位符替代，调用方不应读 Content。
	GetUnseenEdits(ctx context.Context, teamName, assignee string) ([]*TeamTaskBase, error)
	// AppendChangeNote 追加一条编辑说明并把 revision 推进到 revision（F_129 / D8）。
	// Python: update_task 内 revision += 1 + change_notes 追加
	// note 为编辑说明正文，by 为修改人（空串表示未知）。
	AppendChangeNote(ctx context.Context, taskID string, revision int64, note, by string) (bool, error)
	// ClaimTask 认领任务：设置 assignee + FSM 校验到 toStatus。
	// Python: claim_task(task_id, member_name, to_status=IN_PROGRESS/PLANNING) → bool
	ClaimTask(ctx context.Context, taskID, assignee, toStatus string) (bool, error)
	// ResetTask 重置任务：活跃态→PENDING，清除 assignee。
	// Python: reset_task() → bool
	ResetTask(ctx context.Context, taskID string) (bool, error)
	// ApprovePlanTask 计划审批：CLAIMED→PLAN_APPROVED FSM 校验。
	// Python: approve_plan_task() → bool
	ApprovePlanTask(ctx context.Context, taskID string) (bool, error)
	// UpdateTaskStatus 更新任务状态。完成时自动解除下游依赖并刷新 BLOCKED→PENDING。
	// 返回 true 表示成功，false 表示任务不存在或 FSM 不合法。
	UpdateTaskStatus(ctx context.Context, taskID, newStatus string) (bool, error)
	// UpdateTask 更新标题/内容。CLAIMED/PLAN_APPROVED 状态下禁止编辑。
	// Python: update_task() → bool
	UpdateTask(ctx context.Context, taskID, title, content string) (bool, error)
	// MutateDependencyGraph 原子图变更：5 步管线。
	// Python: mutate_dependency_graph()
	MutateDependencyGraph(ctx context.Context, teamName string, newTasks []NewTaskSpec, addEdges []EdgeSpec) GraphMutationResult
	// AddTaskWithBidirectionalDependencies 带双向依赖创建任务。委托 MutateDependencyGraph。
	// Python: add_task_with_bidirectional_dependencies(task_id, team_name, title, content, status, *, dependencies, dependent_task_ids)
	AddTaskWithBidirectionalDependencies(ctx context.Context, teamName string, task *TeamTaskBase, dependencies []string, dependentTaskIDs []string) GraphMutationResult
	// GetDependentTaskIDs 返回依赖 dependsOnTaskID 的全部下游任务 id（去重）。
	// Python: get_dependent_task_ids（task_dao.py L1630-1642）
	GetDependentTaskIDs(ctx context.Context, dependsOnTaskID string) ([]string, error)
	// GetTasksByIDs 一条 IN 查询取多行（顺序未定义；缺失 id 跳过）。
	// Python: get_tasks_by_ids（task_dao.py L675-683）
	GetTasksByIDs(ctx context.Context, taskIDs []string) ([]*TeamTaskBase, error)
	// GetRevisionSnapshot 取 (task_id, revision) 时刻的正文快照（F_129/D9）。
	// ⚠️ 已知偏差：Go 侧无 session_file_store 溢出机制（Python 快照写
	// KIND_TASK_REVISION FileAddress），当前恒返回 (nil, nil)——diff section
	// 按契约优雅降级为空；回收条件 = Go 落任务正文溢出存储。
	// Python: get_revision_snapshot（task_dao.py L1473-1494）
	GetRevisionSnapshot(ctx context.Context, teamName, taskID string, revision int64) (*string, error)
	// GetSubtasks 取直接子任务行（F_129），正文不读。
	// Python: get_subtasks（task_dao.py L781-794）
	GetSubtasks(ctx context.Context, teamName, parentTaskID string) ([]*TeamTaskBase, error)
	// GetOtherActiveTaskID 成员持有的另一条活跃任务（三活跃态 PLANNING/
	// IN_PROGRESS/IN_REVIEW），LIMIT 1 存在式探测（one-active-task 不变量）。
	// Python: get_other_active_task_id（task_dao.py L822-852）
	GetOtherActiveTaskID(ctx context.Context, teamName, memberName, excludeTaskID string) (*string, error)
	// StartTaskCAS 原子地把**已归属** PENDING 任务推进到 toStatus：
	// WHERE assignee=member AND status=pending 单语句 CAS——调度器代表记录的
	// owner 启动任务，不（重）指派。resume_pending 由本次 start 消费（F_128）。
	// Python: start_task（task_dao.py L855-905）
	StartTaskCAS(ctx context.Context, taskID, memberName, toStatus string) (bool, error)
	// ObserveRevision 记录 member（任务 assignee）被展示了 revision（F_129）。
	// 单调（永不回退）且绑定 owner：投给不再持有任务的人不记账。
	// Python: observe_revision（task_dao.py L1493-1511）
	ObserveRevision(ctx context.Context, taskID, memberName string, revision int64) (bool, error)
	// GetUnseenRevisions 全队级「已指派、未终态、assignee 未见过当前 revision」
	// 的 (task_id, status, assignee, resume_pending, revision) 窄投影——调度器
	// revision scan 每次板面唤醒都跑，不读正文。
	// Python: get_unseen_revisions（task_dao.py L723-752）
	GetUnseenRevisions(ctx context.Context, teamName string) ([]UnseenRevisionRow, error)
	// CountTasksTerminality 一条聚合查询返回 (total, non_terminal)（B_09）。
	// Python: count_tasks_terminality（task_manager.py L1556 消费）
	CountTasksTerminality(ctx context.Context, teamName string) (int64, int64, error)
	// GetTaskDependencies 查询任务依赖。
	GetTaskDependencies(ctx context.Context, taskID string) ([]*TeamTaskDependencyBase, error)
	// GetUnresolvedDependenciesCount 未解决依赖计数。
	GetUnresolvedDependenciesCount(ctx context.Context, taskID string) (int, error)
	// GetTasksDependingOn 查询下游依赖任务（即被 taskID 阻塞的任务）。
	GetTasksDependingOn(ctx context.Context, taskID string) ([]*TeamTaskBase, error)
	// DeleteTask 删除任务。
	DeleteTask(ctx context.Context, taskID string) error
	// CancelTask 取消任务（原子终止传播），返回被取消的任务和解除阻塞的任务列表。
	// Python: cancel_task(task_id) → {"task": ..., "unblocked_tasks": [...]}
	CancelTask(ctx context.Context, taskID string) (*TeamTaskBase, []*TeamTaskBase, error)
	// CancelAllTasks 批量取消（原子终止传播），支持 skipAssignees 过滤。
	// Python: cancel_all_tasks() → {"cancelled_tasks": [...], "unblocked_tasks": [...]}
	CancelAllTasks(ctx context.Context, teamName string, skipAssignees []string) (*CancelAllTasksResult, error)
	// CompleteTask 完成任务（原子终止传播），返回被完成的任务和解除阻塞的任务列表。
	// Python: complete_task(task_id) → {"task": ..., "unblocked_tasks": [...]}
	CompleteTask(ctx context.Context, taskID string) (*TeamTaskBase, []*TeamTaskBase, error)
	// VerifyAndFixTaskConsistency 一致性修复：扫描 BLOCKED 任务并刷新状态。
	VerifyAndFixTaskConsistency(ctx context.Context, teamName string) ([]string, error)
}

// MessageDao 消息 DAO 接口。
// Python: MessageDao (openjiuwen/agent_teams/tools/database/message_dao.py)
type MessageDao interface {
	// GetMessage 按 ID 查消息。返回 nil 表示不存在。
	GetMessage(ctx context.Context, messageID string) (*TeamMessageBase, error)
	// CreateMessage 创建消息。返回 true 表示成功，false 表示 messageID 冲突。
	// 广播消息时 IsRead 必须为 nil；直发消息时 IsRead 为 BoolPtr(false/true)。
	CreateMessage(ctx context.Context, msg *TeamMessageBase) bool
	// GetMessages 获取直发消息（非广播）。
	// fromMemberName 为空字符串表示不过滤。unreadOnly=true 时仅返回未读。
	GetMessages(ctx context.Context, teamName, toMemberName string, unreadOnly bool, fromMemberName string) ([]*TeamMessageBase, error)
	// GetBroadcastMessages 获取广播消息（排除自己发送的）。
	// unreadOnly=true 时通过 read_status watermark 过滤。
	GetBroadcastMessages(ctx context.Context, teamName, memberName string, unreadOnly bool, fromMemberName string) ([]*TeamMessageBase, error)
	// GetTeamMessages 获取团队所有消息。broadcast 为空字符串表示不过滤。
	GetTeamMessages(ctx context.Context, teamName string, broadcast string) ([]*TeamMessageBase, error)
	// HasUnreadMessages 是否有未读消息。
	// 直发：检查 is_read=false；广播：检查 per-member watermark。
	HasUnreadMessages(ctx context.Context, teamName string, includeBroadcast bool) bool
	// MembersWithUnread 返回 memberNames 中仍有未读输入的子集：与
	// has_unread_messages 相同的两个读取器（直发行，再广播水位），收窄到指名
	// 成员。供调度器 retirement scan 与休眠 settle 门使用（F_118）：还没读
	// 收到内容的成员不算完事，哪怕板子空了。
	// Python: members_with_unread（message_dao.py L483-497）
	MembersWithUnread(ctx context.Context, teamName string, memberNames []string) (map[string]bool, error)
	// MarkMessageRead 标记已读。直发设 is_read=true；广播更新 read_status watermark。
	MarkMessageRead(ctx context.Context, messageID, memberName string) bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamDatabase 根据配置创建数据库实例。
// Python: 根据 DatabaseConfig.db_type 选择后端。
// memory 类型映射为 InMemoryTeamDatabase，sqlite/postgresql/mysql 类型映射为 SqlTeamDatabase。
// SQL 初始化失败时降级为 InMemoryTeamDatabase。
func NewTeamDatabase(ctx context.Context, config DBConfigProvider) TeamDatabase {
	switch config.GetDBType() {
	case DatabaseTypeMemory:
		return NewInMemoryTeamDatabase()
	default:
		sqlDB := NewSqlTeamDatabase(config.(DatabaseConfig))
		if err := sqlDB.Initialize(ctx); err != nil {
			logger.Error(logComponent).Err(err).Msg("SQL database init failed, falling back to InMemory")
			return NewInMemoryTeamDatabase()
		}
		return sqlDB
	}
}
