package database

import "context"

// ──────────────────────────── 接口 ────────────────────────────

// TeamDatabase 团队数据库门面接口。⤵️ 回填: 9.65a
type TeamDatabase interface {
	// Initialize 初始化数据库
	Initialize(ctx context.Context) error
	// CreateCurSessionTables 创建当前会话动态表
	CreateCurSessionTables(ctx context.Context) error
	// DropCurSessionTables 删除当前会话动态表
	DropCurSessionTables(ctx context.Context) error
	// CleanupAllRuntimeState 清理所有运行时状态
	CleanupAllRuntimeState(ctx context.Context) (droppedTables []string, droppedDirs []string, err error)
	// DropSessionTablesByID 按 sessionID 删除动态表
	DropSessionTablesByID(ctx context.Context, sessionID string) ([]string, error)
	// Close 关闭数据库
	Close() error
	// Team 返回团队 DAO
	Team() TeamDao
	// Member 返回成员 DAO
	Member() MemberDao
	// Task 返回任务 DAO
	Task() TaskDao
	// Message 返回消息 DAO
	Message() MessageDao
}

// TeamDao 团队 DAO 接口。⤵️ 回填: 9.65a
type TeamDao interface {
	// CreateTeam 创建团队
	CreateTeam(ctx context.Context, teamName string, displayName string, leaderMemberName string, desc string) error
	// GetTeam 获取团队信息
	GetTeam(ctx context.Context, teamName string) (any, error)
	// TeamExists 团队是否存在
	TeamExists(ctx context.Context, teamName string) bool
	// DeleteTeam 删除团队
	DeleteTeam(ctx context.Context, teamName string) error
}

// MemberDao 成员 DAO 接口。⤵️ 回填: 9.65a
type MemberDao interface {
	// CreateMember 创建成员
	CreateMember(ctx context.Context, memberName string, teamName string, displayName string, agentCard string, role string, desc string) error
	// GetMember 获取成员信息
	GetMember(ctx context.Context, teamName string, memberName string) (any, error)
	// GetTeamMembers 获取团队成员列表
	GetTeamMembers(ctx context.Context, teamName string) ([]any, error)
	// UpdateMemberStatus 更新成员状态
	UpdateMemberStatus(ctx context.Context, teamName string, memberName string, status string) error
}

// TaskDao 任务 DAO 接口。⤵️ 回填: 9.65a
type TaskDao interface {
	// CreateTask 创建任务
	CreateTask(ctx context.Context, taskID string, teamName string, title string, content string, status string, assignee string) error
	// GetTask 获取任务
	GetTask(ctx context.Context, teamName string, taskID string) (any, error)
	// GetTeamTasks 获取团队任务列表
	GetTeamTasks(ctx context.Context, teamName string) ([]any, error)
	// ClaimTask 认领任务
	ClaimTask(ctx context.Context, teamName string, taskID string, assignee string) error
	// UpdateTaskStatus 更新任务状态
	UpdateTaskStatus(ctx context.Context, teamName string, taskID string, status string) error
	// CancelTask 取消任务
	CancelTask(ctx context.Context, teamName string, taskID string) error
}

// MessageDao 消息 DAO 接口。⤵️ 回填: 9.65a
type MessageDao interface {
	// CreateMessage 创建消息
	CreateMessage(ctx context.Context, messageID string, teamName string, fromMemberName string, toMemberName string, content string, broadcast bool) error
	// GetTeamMessages 获取团队所有消息
	GetTeamMessages(ctx context.Context, teamName string) ([]any, error)
	// GetMessages 获取指定成员的消息
	GetMessages(ctx context.Context, teamName string, toMemberName string, unreadOnly bool) ([]any, error)
	// MarkMessageRead 标记消息已读
	MarkMessageRead(ctx context.Context, teamName string, messageID string, memberName string) error
}
