package tools

import "context"

// ──────────────────────────── 接口 ────────────────────────────

// TeamTaskManager 团队任务管理器接口。⤵️ 回填: 9.65a
type TeamTaskManager interface {
	// Add 添加任务
	Add(ctx context.Context, title string, content string) (any, error)
	// Get 获取任务
	Get(ctx context.Context, taskID string) (any, error)
	// ListTasks 列出任务
	ListTasks(ctx context.Context, status string) ([]any, error)
	// Assign 分配任务
	Assign(ctx context.Context, taskID string, assignee string) error
	// Claim 认领任务
	Claim(ctx context.Context, taskID string) error
	// Complete 完成任务
	Complete(ctx context.Context, taskID string) error
	// Cancel 取消任务
	Cancel(ctx context.Context, taskID string) (any, error)
	// CancelAllTasks 批量取消任务
	CancelAllTasks(ctx context.Context, skipAssignees []string) ([]any, error)
	// GetClaimableTasks 获取可认领任务
	GetClaimableTasks(ctx context.Context) ([]any, error)
	// GetTasksByAssignee 按分配人查任务
	GetTasksByAssignee(ctx context.Context, memberName string, status string) ([]any, error)
}
