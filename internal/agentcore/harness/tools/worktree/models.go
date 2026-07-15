package worktree

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeConfig Worktree 隔离配置。
// 对齐 Python: WorktreeConfig
type WorktreeConfig struct {
	// Enabled 是否启用 Worktree 隔离
	Enabled bool `json:"enabled"`
	// BaseDir Worktree 基础目录
	BaseDir string `json:"base_dir,omitempty"`
	// SparsePaths 稀疏检出路径列表
	SparsePaths []string `json:"sparse_paths,omitempty"`
	// SymlinkDirectories 需要创建符号链接的目录列表
	SymlinkDirectories []string `json:"symlink_directories,omitempty"`
	// IncludePatterns 文件包含模式列表
	IncludePatterns []string `json:"include_patterns,omitempty"`
	// CleanupAfterDays 多少天后自动清理
	CleanupAfterDays int `json:"cleanup_after_days"`
	// AutoCleanupOnShutdown 关闭时是否自动清理
	AutoCleanupOnShutdown bool `json:"auto_cleanup_on_shutdown"`
	// LifecyclePolicy 生命周期策略
	LifecyclePolicy WorktreeLifecyclePolicy `json:"lifecycle_policy"`
}

// WorktreeSession Worktree 运行时状态。
// 对齐 Python: WorktreeSession
type WorktreeSession struct {
	// OriginalCWD 原始工作目录
	OriginalCWD string `json:"original_cwd"`
	// WorktreePath Worktree 路径
	WorktreePath string `json:"worktree_path"`
	// WorktreeName Worktree 名称
	WorktreeName string `json:"worktree_name"`
	// WorktreeBranch Worktree 分支
	WorktreeBranch string `json:"worktree_branch,omitempty"`
	// OriginalBranch 原始分支
	OriginalBranch string `json:"original_branch,omitempty"`
	// OriginalHeadCommit 原始 HEAD 提交
	OriginalHeadCommit string `json:"original_head_commit,omitempty"`
	// MemberName 成员名称
	MemberName string `json:"member_name,omitempty"`
	// TeamName 团队名称
	TeamName string `json:"team_name,omitempty"`
	// HookBased 是否基于钩子
	HookBased bool `json:"hook_based"`
	// LifecyclePolicy 生命周期策略
	LifecyclePolicy WorktreeLifecyclePolicy `json:"lifecycle_policy"`
	// TeamLifecycle 团队生命周期
	TeamLifecycle string `json:"team_lifecycle,omitempty"`
	// CreationDurationMs 创建耗时（毫秒）
	CreationDurationMs float64 `json:"creation_duration_ms,omitempty"`
	// UsedSparsePaths 是否使用了稀疏路径
	UsedSparsePaths bool `json:"used_sparse_paths"`
}

// WorktreeCreateResult Worktree 创建结果。
// 对齐 Python: WorktreeCreateResult
type WorktreeCreateResult struct {
	// WorktreePath Worktree 路径
	WorktreePath string `json:"worktree_path"`
	// WorktreeBranch Worktree 分支
	WorktreeBranch string `json:"worktree_branch,omitempty"`
	// HeadCommit HEAD 提交
	HeadCommit string `json:"head_commit,omitempty"`
	// BaseBranch 基础分支
	BaseBranch string `json:"base_branch,omitempty"`
	// Existed 是否已存在
	Existed bool `json:"existed"`
	// HookBased 是否基于钩子
	HookBased bool `json:"hook_based"`
}

// WorktreeChangeSummary Worktree 变更摘要。
// 对齐 Python: WorktreeChangeSummary
type WorktreeChangeSummary struct {
	// ChangedFiles 变更文件数
	ChangedFiles int `json:"changed_files"`
	// Commits 提交数
	Commits int `json:"commits"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// WorktreeLifecyclePolicy Worktree 生命周期策略。
// 对齐 Python: WorktreeLifecyclePolicy
type WorktreeLifecyclePolicy string

const (
	// WorktreeLifecyclePolicyAuto 自动推断
	WorktreeLifecyclePolicyAuto WorktreeLifecyclePolicy = "auto"
	// WorktreeLifecyclePolicyEphemeral 临时，自动清理
	WorktreeLifecyclePolicyEphemeral WorktreeLifecyclePolicy = "ephemeral"
	// WorktreeLifecyclePolicyDurable 持久，跨会话保留
	WorktreeLifecyclePolicyDurable WorktreeLifecyclePolicy = "durable"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorktreeConfig 创建默认 WorktreeConfig。
// 对齐 Python: WorktreeConfig()
func NewWorktreeConfig() WorktreeConfig {
	return WorktreeConfig{
		CleanupAfterDays:      30,
		AutoCleanupOnShutdown: true,
		LifecyclePolicy:       WorktreeLifecyclePolicyAuto,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
