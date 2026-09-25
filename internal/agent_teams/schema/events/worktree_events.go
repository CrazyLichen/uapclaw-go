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
	// OwnerID 持有者标识（如 team member name）
	OwnerID string
	// Tag 分组标签（如 team name）
	Tag string
}

// WorktreeRemovedEvent Worktree 移除事件。
// Python: WorktreeRemovedEvent
type WorktreeRemovedEvent struct {
	BaseEventMessage
	// WorktreeName worktree 名
	WorktreeName string
	// WorktreePath 绝对路径
	WorktreePath string
	// OwnerID 持有者标识
	OwnerID string
	// Tag 分组标签
	Tag string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// EventTypeName 返回 worktree 创建事件类型名。
func (e WorktreeCreatedEvent) EventTypeName() string { return TeamEventWorktreeCreated }

// ToPayload 转换为事件载荷。
func (e WorktreeCreatedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "worktree_name": e.WorktreeName, "worktree_path": e.WorktreePath, "existed": e.Existed, "owner_id": e.OwnerID, "tag": e.Tag}
}

// EventTypeName 返回 worktree 移除事件类型名。
func (e WorktreeRemovedEvent) EventTypeName() string { return TeamEventWorktreeRemoved }

// ToPayload 转换为事件载荷。
func (e WorktreeRemovedEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "worktree_name": e.WorktreeName, "worktree_path": e.WorktreePath, "owner_id": e.OwnerID, "tag": e.Tag}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
