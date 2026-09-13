package worktree

import (
	"context"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeCreatedEvent worktree 创建/恢复事件。
// Python: WorktreeCreatedEvent
type WorktreeCreatedEvent struct {
	// WorktreeName worktree 名称
	WorktreeName string `json:"worktree_name"`
	// WorktreePath worktree 绝对路径
	WorktreePath string `json:"worktree_path"`
	// OwnerID 持有者标识（如 team member name）
	OwnerID string `json:"owner_id,omitempty"`
	// Tag 分组标签（如 team name）
	Tag string `json:"tag,omitempty"`
	// Existed 是否从已有 worktree 恢复
	Existed bool `json:"existed"`
}

// WorktreeRemovedEvent worktree 移除事件。
// Python: WorktreeRemovedEvent
type WorktreeRemovedEvent struct {
	// WorktreeName worktree 名称
	WorktreeName string `json:"worktree_name"`
	// WorktreePath worktree 绝对路径
	WorktreePath string `json:"worktree_path"`
	// OwnerID 持有者标识
	OwnerID string `json:"owner_id,omitempty"`
	// Tag 分组标签
	Tag string `json:"tag,omitempty"`
}

// WorktreeEvent worktree 事件联合类型。
// Python: WorktreeEvent = Union[WorktreeCreatedEvent, WorktreeRemovedEvent]
type WorktreeEvent interface {
	isWorktreeEvent()
}

// WorktreeEventHandler worktree 生命周期事件回调。
// Python: WorktreeEventHandler = Callable[[WorktreeEvent], Awaitable[None]]
type WorktreeEventHandler func(ctx context.Context, event WorktreeEvent) error

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// isWorktreeEvent 实现 WorktreeEvent 接口
func (*WorktreeCreatedEvent) isWorktreeEvent() {}
func (*WorktreeRemovedEvent) isWorktreeEvent() {}
