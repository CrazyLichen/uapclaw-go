package worktree

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeConfig Worktree 隔离配置。
// Python: WorktreeConfig
type WorktreeConfig struct {
	// Enabled 是否启用 Worktree 隔离
	Enabled bool `json:"enabled"`
	// BaseDir Worktree 基础目录
	BaseDir string `json:"base_dir,omitempty"`
	// BaseDirIsWorktreesDir 把 BaseDir 视作 worktrees 父目录本身。
	// 为 false 时 worktree 落在 {BaseDir}/.worktrees 下（单 agent 通用路径）；
	// 团队拥有的 worktree 传 true，使会话根为 sessions/{id}/worktrees/{slug}。
	// Python: WorktreeConfig.base_dir_is_worktrees_dir (models.py:74)
	BaseDirIsWorktreesDir bool `json:"base_dir_is_worktrees_dir,omitempty"`
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
	// RepoLock 是否按 repo_root 串行化 worktree 创建/删除关键段。
	// 为 true 时 WorktreeManager 在 enter / create_owner_worktree / _remove_worktree
	// 外层包进程内 + 跨进程双锁（见 lock.go RepoLock）。
	// 自行管理隔离的远端后端可禁用。
	// Python: WorktreeConfig.repo_lock (models.py:87)
	RepoLock bool `json:"repo_lock"`
	// RepoLockTimeout 仓库锁等待秒数，超时显式报错（不静默降级）。
	// 覆盖最坏情况的串行链（如 5 个 worker 各自 fetch + worktree add）。
	// Python: WorktreeConfig.repo_lock_timeout (models.py:96)
	RepoLockTimeout float64 `json:"repo_lock_timeout"`
	// LongpathsSupport 创建 worktree 前在 repo_root 上设 core.longpaths=true（Windows）。
	// Windows 上 git worktree add 检出超 260 字符路径会 rc=128 失败；core.longpaths
	// 使 git 使用扩展长度路径前缀绕过 Win32 限制，不改变源仓库目录结构。
	// 默认开启（rc=128 是主要失败形态）；源仓库禁止 git config 写入时关闭。
	// Python: WorktreeConfig.longpaths_support (models.py:99)
	LongpathsSupport bool `json:"longpaths_support"`
}

// WorktreeSession Worktree 运行时状态。
// Python: WorktreeSession
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
// Python: WorktreeCreateResult
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
// Python: WorktreeChangeSummary
type WorktreeChangeSummary struct {
	// ChangedFiles 变更文件数
	ChangedFiles int `json:"changed_files"`
	// Commits 提交数
	Commits int `json:"commits"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// WorktreeLifecyclePolicy Worktree 生命周期策略。
// Python: WorktreeLifecyclePolicy
type WorktreeLifecyclePolicy string

const (
	// WorktreeLifecyclePolicyAuto 自动推断
	WorktreeLifecyclePolicyAuto WorktreeLifecyclePolicy = "auto"
	// WorktreeLifecyclePolicyEphemeral 临时，自动清理
	WorktreeLifecyclePolicyEphemeral WorktreeLifecyclePolicy = "ephemeral"
	// WorktreeLifecyclePolicyDurable 持久，跨会话保留
	WorktreeLifecyclePolicyDurable WorktreeLifecyclePolicy = "durable"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorktreeConfig 创建默认 WorktreeConfig。
// Python: WorktreeConfig()
func NewWorktreeConfig() WorktreeConfig {
	return WorktreeConfig{
		CleanupAfterDays:      30,
		AutoCleanupOnShutdown: true,
		LifecyclePolicy:       WorktreeLifecyclePolicyAuto,
		RepoLock:              true,
		RepoLockTimeout:       120.0,
		LongpathsSupport:      true,
	}
}
