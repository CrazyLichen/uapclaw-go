package database

import "context"

// ──────────────────────────── 接口 ────────────────────────────

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

// TaskDao 任务 DAO 接口。⤵️ 9.65a-2 回填具体方法签名
type TaskDao interface{}

// MessageDao 消息 DAO 接口。⤵️ 9.65a-3 回填具体方法签名
type MessageDao interface{}
