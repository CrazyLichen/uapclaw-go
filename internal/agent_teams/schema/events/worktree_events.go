package events

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeCreatedEvent Worktree 创建/恢复事件。
// Python: WorktreeCreatedEvent
type WorktreeCreatedEvent struct {
	BaseEventMessage
	// WorktreeName worktree 名
	WorktreeName string
	// WorktreePath 绝对路径
	WorktreePath string
	// Existed 是否从已有 worktree 恢复
	Existed bool
}

// WorktreeRemovedEvent Worktree 移除事件。
// Python: WorktreeRemovedEvent
type WorktreeRemovedEvent struct {
	BaseEventMessage
	// WorktreeName worktree 名
	WorktreeName string
	// WorktreePath 绝对路径
	WorktreePath string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func (e WorktreeCreatedEvent) EventTypeName() string { return TeamEventWorktreeCreated }
func (e WorktreeCreatedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "worktree_name": e.WorktreeName, "worktree_path": e.WorktreePath, "existed": e.Existed}
}

func (e WorktreeRemovedEvent) EventTypeName() string { return TeamEventWorktreeRemoved }
func (e WorktreeRemovedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "worktree_name": e.WorktreeName, "worktree_path": e.WorktreePath}
}
