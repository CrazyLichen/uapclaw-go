package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeLifecycleRail worktree 生命周期 hook 基类。
// Python: WorktreeLifecycleRail (rails.py)
//
// 完整实现在 rails.go 中，此处仅定义接口供 ManagerOption 引用。
// 子类覆盖关心的 hook 方法，WorktreeManager.fireRail 直接调用。
type WorktreeLifecycleRail interface {
	// BeforeWorktreeCreate 创建前 hook。返回修改后的 slug，nil 不干预。
	BeforeWorktreeCreate(ctx context.Context, slug, repoRoot string) (string, error)
	// AfterWorktreeCreate 创建后 hook。
	AfterWorktreeCreate(ctx context.Context, session *WorktreeSession) error
	// BeforeWorktreeExit 退出前 hook。返回修改后的 action，nil 不干预。
	BeforeWorktreeExit(ctx context.Context, session *WorktreeSession, action string) (string, error)
	// AfterWorktreeExit 退出后 hook。
	AfterWorktreeExit(ctx context.Context, session *WorktreeSession, action string) error
}

// WorktreeBackend worktree 后端接口。
// Python: WorktreeBackend(Protocol)
type WorktreeBackend interface {
	// Create 创建或恢复 worktree。
	// Python: WorktreeBackend.create(slug, repo_root, target_path)
	Create(ctx context.Context, slug, repoRoot, targetPath string) (*WorktreeCreateResult, error)
	// Remove 删除 worktree。
	// Python: WorktreeBackend.remove(worktree_path, repo_root)
	Remove(ctx context.Context, worktreePath, repoRoot string) bool
	// Exists 检查 worktree 是否存在。
	// Python: WorktreeBackend.exists(worktree_path)
	Exists(ctx context.Context, worktreePath string) bool
}

// GitBackend 原生 git worktree 后端。
// Python: GitBackend
//
// 实现完整的创建流程：
// 1. 快速恢复检查（readWorktreeHeadSHA，不调 git 子进程）
// 2. 条件 fetch（跳过本地已存在的 origin ref）
// 3. git worktree add -B
// 4. 可选稀疏检出
type GitBackend struct {
	// config Worktree 配置
	config WorktreeConfig
}

// managerOptions WorktreeManager 内部构造选项。
type managerOptions struct {
	eventHandler   WorktreeEventHandler
	lifecycleRails []WorktreeLifecycleRail
}

// ──────────────────────────── 枚举 ────────────────────────────

// ManagerOption WorktreeManager 构造选项。
type ManagerOption func(*managerOptions)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// backendRegistry 后端注册表。
// Python: _BACKEND_REGISTRY
var backendRegistry = struct {
	sync.RWMutex
	m map[string]func(WorktreeConfig) WorktreeBackend
}{
	m: map[string]func(WorktreeConfig) WorktreeBackend{
		"git": func(cfg WorktreeConfig) WorktreeBackend {
			return &GitBackend{config: cfg}
		},
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterWorktreeBackend 注册自定义 worktree 后端。
// Python: register_worktree_backend(name, factory)
func RegisterWorktreeBackend(name string, factory func(WorktreeConfig) WorktreeBackend) {
	backendRegistry.Lock()
	defer backendRegistry.Unlock()
	backendRegistry.m[name] = factory
}

// CreateBackend 按名称创建 worktree 后端。
// Python: create_backend(name, config)
func CreateBackend(name string, config WorktreeConfig) (WorktreeBackend, error) {
	backendRegistry.RLock()
	defer backendRegistry.RUnlock()
	factory, ok := backendRegistry.m[name]
	if !ok {
		return nil, fmt.Errorf("未知的 worktree 后端 '%s'，可用: %v", name, availableBackends())
	}
	return factory(config), nil
}

// WithEventHandler 设置事件处理器。
func WithEventHandler(handler WorktreeEventHandler) ManagerOption {
	return func(o *managerOptions) { o.eventHandler = handler }
}

// WithLifecycleRails 设置生命周期 rail。
func WithLifecycleRails(rails ...WorktreeLifecycleRail) ManagerOption {
	return func(o *managerOptions) { o.lifecycleRails = rails }
}

// Create 创建或恢复 worktree。
// Python: GitBackend.create(slug, repo_root, target_path)
//
// 四阶段创建流程：
// 1. 快速恢复：readWorktreeHeadSHA 读 HEAD，已有 worktree 直接返回
// 2. 基准解析：_resolveBase — 先尝试本地 origin/<default> 引用（省 6-8s），fetch 失败再回退 HEAD
// 3. 创建 worktree：worktreeAdd -B
// 4. 稀疏检出（可选）：sparseCheckoutSet，失败回滚
func (g *GitBackend) Create(ctx context.Context, slug, repoRoot, targetPath string) (*WorktreeCreateResult, error) {
	wtBranch := WorktreeBranchName(slug)

	// 阶段 1: 快速恢复 —— worktree 已存在
	existingHead, err := ReadWorktreeHeadSHA(targetPath)
	if err == nil && existingHead != "" {
		return &WorktreeCreateResult{
			WorktreePath:   targetPath,
			WorktreeBranch: wtBranch,
			HeadCommit:     existingHead,
			Existed:        true,
		}, nil
	}

	// 阶段 2: 解析基准分支
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return nil, fmt.Errorf("创建 worktree 父目录失败: %w", err)
	}
	baseBranch, baseSHA := g.resolveBase(ctx, repoRoot)

	// 阶段 3: 创建 worktree
	sparse := g.config.SparsePaths
	if err := WorktreeAdd(ctx, repoRoot, targetPath, wtBranch, baseBranch, len(sparse) > 0); err != nil {
		return nil, err
	}

	// 阶段 4: 稀疏检出（可选，失败回滚）
	if len(sparse) > 0 {
		if err := SparseCheckoutSet(ctx, targetPath, sparse); err != nil {
			// 回滚：移除刚创建的 worktree
			_ = WorktreeRemove(ctx, targetPath, repoRoot, true)
			return nil, fmt.Errorf("稀疏检出失败，worktree 已清理: %w", err)
		}
	}

	if baseSHA == "" {
		baseSHA, _ = RevParse(ctx, "HEAD", targetPath)
	}

	return &WorktreeCreateResult{
		WorktreePath:   targetPath,
		WorktreeBranch: wtBranch,
		HeadCommit:     baseSHA,
		BaseBranch:     baseBranch,
		Existed:        false,
	}, nil
}

// Remove 删除 worktree 及其分支。
// Python: GitBackend.remove(worktree_path, repo_root)
func (g *GitBackend) Remove(ctx context.Context, worktreePath, repoRoot string) bool {
	branch, _ := GetCurrentBranch(ctx, worktreePath)
	ok := WorktreeRemove(ctx, worktreePath, repoRoot, true)
	if ok && branch != "" && strings.HasPrefix(branch, "worktree-") {
		_ = BranchDelete(ctx, branch, repoRoot)
	}
	return ok
}

// Exists 通过快速 HEAD 读取检查 worktree 是否存在。
// Python: GitBackend.exists(worktree_path)
func (g *GitBackend) Exists(_ context.Context, worktreePath string) bool {
	sha, err := ReadWorktreeHeadSHA(worktreePath)
	return err == nil && sha != ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveBase 解析新 worktree 的基准分支和 SHA。
// Python: GitBackend._resolve_base(repo_root)
//
// 优化：跳过本地已存在的 origin ref，节省 6-8s。
func (g *GitBackend) resolveBase(ctx context.Context, repoRoot string) (string, string) {
	defaultBranch := GetDefaultBranch(ctx, repoRoot)
	originRef := "origin/" + defaultBranch

	// 先尝试本地解析
	if sha, err := RevParse(ctx, originRef, repoRoot); err == nil && sha != "" {
		return originRef, sha
	}

	// 从远程 fetch
	if FetchRef(ctx, repoRoot, defaultBranch) {
		if sha, err := RevParse(ctx, originRef, repoRoot); err == nil && sha != "" {
			return originRef, sha
		}
	}

	// 最后回退：使用当前 HEAD
	sha, _ := RevParse(ctx, "HEAD", repoRoot)
	return "HEAD", sha
}

// availableBackends 返回所有已注册后端名称。
func availableBackends() []string {
	backendRegistry.RLock()
	defer backendRegistry.RUnlock()
	names := make([]string, 0, len(backendRegistry.m))
	for name := range backendRegistry.m {
		names = append(names, name)
	}
	return names
}
