package team_workspace

import "time"

// ──────────────────────────── 结构体 ────────────────────────────

// TeamWorkspaceConfig 团队共享工作空间配置。
// 对齐 Python: TeamWorkspaceConfig
type TeamWorkspaceConfig struct {
	// Enabled 是否启用团队工作空间
	Enabled bool `json:"enabled"`
	// RootPath 工作空间根路径
	RootPath string `json:"root_path,omitempty"`
	// ArtifactDirs 产物目录列表
	ArtifactDirs []string `json:"artifact_dirs"`
	// VersionControl 是否启用版本控制
	VersionControl bool `json:"version_control"`
	// ConflictStrategy 并发修改冲突策略
	ConflictStrategy ConflictStrategy `json:"conflict_strategy"`
	// RemoteURL 远程仓库 URL
	RemoteURL string `json:"remote_url,omitempty"`
}

// WorkspaceFileLock 文件级锁条目。
// 对齐 Python: WorkspaceFileLock
type WorkspaceFileLock struct {
	// FilePath 文件路径
	FilePath string `json:"file_path"`
	// HolderID 持有者标识
	HolderID string `json:"holder_id"`
	// HolderName 持有者名称
	HolderName string `json:"holder_name"`
	// AcquiredAt 获取时间（RFC3339 格式）
	AcquiredAt string `json:"acquired_at"`
	// TimeoutSeconds 超时秒数
	TimeoutSeconds int `json:"timeout_seconds"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// WorkspaceMode 工作空间操作模式。
// 对齐 Python: WorkspaceMode
type WorkspaceMode string

const (
	// WorkspaceModeLocal 本地模式
	WorkspaceModeLocal WorkspaceMode = "local"
	// WorkspaceModeDistributed 分布式模式
	WorkspaceModeDistributed WorkspaceMode = "distributed"
)

// ConflictStrategy 并发修改冲突策略。
// 对齐 Python: ConflictStrategy
type ConflictStrategy string

const (
	// ConflictStrategyLock 文件级锁
	ConflictStrategyLock ConflictStrategy = "lock"
	// ConflictStrategyMerge Git 合并
	ConflictStrategyMerge ConflictStrategy = "merge"
	// ConflictStrategyLastWriteWins 后写覆盖
	ConflictStrategyLastWriteWins ConflictStrategy = "last_write_wins"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamWorkspaceConfig 创建默认 TeamWorkspaceConfig。
// 对齐 Python: TeamWorkspaceConfig()
func NewTeamWorkspaceConfig() TeamWorkspaceConfig {
	return TeamWorkspaceConfig{
		ArtifactDirs: []string{
			"artifacts/code",
			"artifacts/docs",
			"artifacts/reports",
			"trajectories",
		},
		VersionControl:   true,
		ConflictStrategy: ConflictStrategyLock,
	}
}

// NewWorkspaceFileLock 创建默认 WorkspaceFileLock。
func NewWorkspaceFileLock(filePath, holderID, holderName string) WorkspaceFileLock {
	return WorkspaceFileLock{
		FilePath:       filePath,
		HolderID:       holderID,
		HolderName:     holderName,
		AcquiredAt:     time.Now().UTC().Format(time.RFC3339),
		TimeoutSeconds: 300,
	}
}

// IsExpired 检查锁是否已超时。
// 对齐 Python: WorkspaceFileLock.is_expired()
func (l WorkspaceFileLock) IsExpired() bool {
	acquired, err := time.Parse(time.RFC3339, l.AcquiredAt)
	if err != nil {
		return true
	}
	return time.Now().UTC().After(acquired.Add(time.Duration(l.TimeoutSeconds) * time.Second))
}
