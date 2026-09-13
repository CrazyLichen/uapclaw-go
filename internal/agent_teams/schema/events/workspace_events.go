package events

// ──────────────────────────── 结构体 ────────────────────────────

// WorkspaceArtifactEvent 工件创建/更新事件。
// Python: WorkspaceArtifactEvent
type WorkspaceArtifactEvent struct {
	BaseEventMessage
	// ArtifactPath 工件在工作空间内的相对路径
	ArtifactPath string
	// CommitSHA Git 提交 SHA（如已版本化）
	CommitSHA string
}

// WorkspaceConflictEvent 合并冲突/推送失败事件。
// Python: WorkspaceConflictEvent
type WorkspaceConflictEvent struct {
	BaseEventMessage
	// FilePath 冲突文件路径
	FilePath string
	// ConflictingCommit 冲突提交 SHA
	ConflictingCommit string
}

// WorkspaceLockRequestEvent 锁请求事件。
// Python: WorkspaceLockRequestEvent
type WorkspaceLockRequestEvent struct {
	BaseEventMessage
	// Action 锁操作：acquire 或 release
	Action string
	// FilePath 锁定/解锁的文件
	FilePath string
	// HolderName 锁请求者名称
	HolderName string
	// TimeoutSeconds 锁超时秒数
	TimeoutSeconds int
}

// WorkspaceLockResponseEvent 锁响应事件。
// Python: WorkspaceLockResponseEvent
type WorkspaceLockResponseEvent struct {
	BaseEventMessage
	// FilePath 锁定/解锁的文件
	FilePath string
	// Granted 是否授予
	Granted bool
	// Holder 当前锁持有者信息（未授予时）
	Holder map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func (e WorkspaceArtifactEvent) EventTypeName() string { return TeamEventWorkspaceArtifactUpdated }
func (e WorkspaceArtifactEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "artifact_path": e.ArtifactPath, "commit_sha": e.CommitSHA}
}

func (e WorkspaceConflictEvent) EventTypeName() string { return TeamEventWorkspaceConflict }
func (e WorkspaceConflictEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "file_path": e.FilePath, "conflicting_commit": e.ConflictingCommit}
}

func (e WorkspaceLockRequestEvent) EventTypeName() string { return TeamEventWorkspaceLockRequest }
func (e WorkspaceLockRequestEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "action": e.Action, "file_path": e.FilePath, "holder_name": e.HolderName, "timeout_seconds": e.TimeoutSeconds}
}

func (e WorkspaceLockResponseEvent) EventTypeName() string { return TeamEventWorkspaceLockResponse }
func (e WorkspaceLockResponseEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "file_path": e.FilePath, "granted": e.Granted, "holder": e.Holder}
}
