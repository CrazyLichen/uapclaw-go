package infra

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// slugifyIssueRe issue 号识别正则（对齐 _slugify 的 issue_match）
var slugifyIssueRe = regexp.MustCompile(`(?i)(?:fix-issue-|issue-|\bissue\s*#?|#)\s*(\d+)`)

// slugifyNonAlnumRe 非字母数字替换正则（对齐 _slugify 的 re.sub）
var slugifyNonAlnumRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeManager 为每个 task 创建和清理 git worktree。
//
// 工作流（对齐 class WorktreeManager）：
//   - 有 LocalRepo → fetch + worktree add
//   - 无 LocalRepo → 确保 clone 缓存 → fetch + worktree add
type WorktreeManager struct {
	// configFn 返回配置快照（避免依赖 autoharness 包类型造成环；由上层注入取值函数）
	configFn func() WorktreeConfigSnapshot
	// runner 命令执行注入缝
	runner CommandRunner
	// gitEnv 认证环境
	gitEnv map[string]string
	// mu 串行化 git 操作（对齐 asyncio.Lock）
	mu sync.Mutex
	// ctx 命令执行上下文
	ctx context.Context
}

// WorktreeConfigSnapshot WorktreeManager 消费的配置快照
// （字段语义对齐 AutoHarnessConfig 的对应字段）
type WorktreeConfigSnapshot struct {
	LocalRepo     string
	CacheRepoDir  string
	WorktreesDir  string
	GitBaseBranch string
	RepoURL       string
	GitUserName   string
	GitUserEmail  string
	GitRemote     string
	ForkOwner     string
	UpstreamRepo  string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorktreeManager 构造 worktree 管理器（对齐 __init__）
func NewWorktreeManager(ctx context.Context, runner CommandRunner, configFn func() WorktreeConfigSnapshot, username, token string) *WorktreeManager {
	return &WorktreeManager{
		ctx:      ctx,
		runner:   runner,
		configFn: configFn,
		gitEnv:   BuildGitAuthEnv(username, token),
	}
}

// Prepare 为 task 创建隔离的 worktree（对齐 prepare）。
//
// topic 用于生成目录名和分支名；返回 worktree 绝对路径。
func (w *WorktreeManager) Prepare(topic string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	cfg := w.configFn()
	base, err := w.ensureBaseRepo()
	if err != nil {
		return "", err
	}

	slug := slugify(topic)
	ts := time.Now().Unix()
	wtName := fmt.Sprintf("%d-%s", ts, slug)
	branchName := fmt.Sprintf("auto-harness/%s", slug)

	wtRoot := cfg.WorktreesDir
	if err := os.MkdirAll(wtRoot, 0o755); err != nil {
		return "", fmt.Errorf("worktree root mkdir failed: %v", err)
	}
	wtPath := filepath.Join(wtRoot, wtName)

	baseBranch := cfg.GitBaseBranch
	if baseBranch == "" {
		baseBranch = "develop"
	}

	if err := w.dropExistingBranch(base, branchName); err != nil {
		return "", err
	}

	code, out := w.runGit([]string{
		"worktree", "add", "-b", branchName, wtPath,
		fmt.Sprintf("origin/%s", baseBranch),
	}, base)
	if code != 0 {
		return "", fmt.Errorf("worktree add failed: %s", out)
	}

	logger.Info(logComponent).
		Str("worktree", wtPath).
		Str("branch", branchName).
		Msg("Created worktree")

	// 配置 git user（worktree 级别）
	if cfg.GitUserName != "" {
		w.runGit([]string{"config", "user.name", cfg.GitUserName}, wtPath)
	}
	if cfg.GitUserEmail != "" {
		w.runGit([]string{"config", "user.email", cfg.GitUserEmail}, wtPath)
	}

	// 配置 fork remote（如果指定）
	if cfg.GitRemote != "" {
		// 检查 remote 是否已存在
		rc, _ := w.runGit([]string{"remote", "get-url", cfg.GitRemote}, wtPath)
		if rc != 0 {
			// worktree 共享 remote，在 base 上添加
			forkURL := fmt.Sprintf("https://gitcode.com/%s/%s.git", cfg.ForkOwner, cfg.UpstreamRepo)
			w.runGit([]string{"remote", "add", cfg.GitRemote, forkURL}, base)
		}
	}

	return wtPath, nil
}

// PrepareReadonlySnapshot 从 origin/base 创建 detached 只读快照
// （对齐 prepare_readonly_snapshot；assess/plan 阶段使用，
// 分析始终指向最新 fetch 的远程 base 分支而非用户可变的本地检出）。
func (w *WorktreeManager) PrepareReadonlySnapshot(label string) (string, error) {
	w.mu.Lock()
	cfg := w.configFn()
	base, err := w.ensureBaseRepo()
	if err != nil {
		w.mu.Unlock()
		return "", err
	}
	ts := time.Now().Unix()
	wtRoot := cfg.WorktreesDir
	if err := os.MkdirAll(wtRoot, 0o755); err != nil {
		w.mu.Unlock()
		return "", fmt.Errorf("worktree root mkdir failed: %v", err)
	}
	wtPath := filepath.Join(wtRoot, fmt.Sprintf("%d-%s", ts, label))
	baseBranch := cfg.GitBaseBranch
	if baseBranch == "" {
		baseBranch = "develop"
	}

	code, out := w.runGit([]string{
		"worktree", "add", "--detach", wtPath,
		fmt.Sprintf("origin/%s", baseBranch),
	}, base)
	w.mu.Unlock()
	if code != 0 {
		return "", fmt.Errorf("readonly worktree add failed: %s", out)
	}

	logger.Info(logComponent).
		Str("worktree", wtPath).
		Msg("Created readonly worktree")
	return wtPath, nil
}

// Cleanup 清理 worktree（对齐 cleanup；失败仅 Warn 等待人工清理）
func (w *WorktreeManager) Cleanup(worktreePath string) {
	w.mu.Lock()
	base := w.baseRepo()
	wt := worktreePath

	if !pathExists(wt) {
		w.mu.Unlock()
		return
	}

	code, out := w.runGit([]string{"worktree", "remove", "--force", wt}, base)
	w.mu.Unlock()
	if code != 0 {
		logger.Warn(logComponent).
			Str("output", out).
			Msg("worktree remove failed (manual cleanup needed)")
	} else {
		logger.Info(logComponent).
			Str("worktree", worktreePath).
			Msg("Cleaned up worktree")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// baseRepo 返回基础仓库路径（对齐 _base_repo：local_repo 优先，否则 clone 缓存）
func (w *WorktreeManager) baseRepo() string {
	cfg := w.configFn()
	if cfg.LocalRepo != "" {
		if abs, err := filepath.Abs(cfg.LocalRepo); err == nil {
			return abs
		}
	}
	return cfg.CacheRepoDir
}

// isManagedWorktreePath 判断 worktree 是否位于 auto-harness 根之下
// （对齐 _is_managed_worktree_path）
func (w *WorktreeManager) isManagedWorktreePath(worktreePath string) bool {
	cfg := w.configFn()
	root, _ := filepath.Abs(cfg.WorktreesDir)
	candidate, _ := filepath.Abs(worktreePath)
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// listWorktrees 解析 git worktree list --porcelain 条目（对齐 _list_worktrees）
func (w *WorktreeManager) listWorktrees(base string) ([]map[string]string, error) {
	code, out := w.runGit([]string{"worktree", "list", "--porcelain"}, base)
	if code != 0 {
		return nil, fmt.Errorf("worktree list failed: %s", out)
	}

	var entries []map[string]string
	current := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			if len(current) > 0 {
				entries = append(entries, current)
				current = map[string]string{}
			}
			continue
		}
		if strings.HasPrefix(line, "worktree ") {
			if len(current) > 0 {
				entries = append(entries, current)
			}
			current = map[string]string{"path": strings.TrimPrefix(line, "worktree ")}
			continue
		}
		if strings.HasPrefix(line, "branch ") {
			current["branch"] = strings.TrimPrefix(line, "branch ")
		}
	}
	if len(current) > 0 {
		entries = append(entries, current)
	}
	return entries, nil
}

// dropExistingBranch 重建前删除陈旧的 auto-harness 分支/worktree
// （对齐 _drop_existing_branch）
func (w *WorktreeManager) dropExistingBranch(base, branchName string) error {
	code, out := w.runGit([]string{"worktree", "prune"}, base)
	if code != 0 {
		return fmt.Errorf("worktree prune failed: %s", out)
	}

	code, _ = w.runGit([]string{
		"show-ref", "--verify", "--quiet",
		fmt.Sprintf("refs/heads/%s", branchName),
	}, base)
	if code != 0 {
		return nil
	}

	branchRef := fmt.Sprintf("refs/heads/%s", branchName)
	entries, err := w.listWorktrees(base)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry["branch"] != branchRef {
			continue
		}
		worktreePath := entry["path"]
		if worktreePath == "" {
			continue
		}
		if !w.isManagedWorktreePath(worktreePath) {
			return fmt.Errorf(
				"existing auto-harness branch is checked out in unmanaged worktree: %s",
				worktreePath,
			)
		}
		code, out := w.runGit([]string{"worktree", "remove", "--force", worktreePath}, base)
		if code != 0 {
			return fmt.Errorf("failed to remove existing managed worktree for branch %s: %s", branchName, out)
		}
		logger.Info(logComponent).
			Str("branch", branchName).
			Str("worktree", worktreePath).
			Msg("Removed stale worktree for branch")
	}

	code, out = w.runGit([]string{"branch", "-D", branchName}, base)
	if code != 0 {
		return fmt.Errorf("failed to delete existing branch %s: %s", branchName, out)
	}
	logger.Info(logComponent).
		Str("branch", branchName).
		Msg("Deleted stale auto-harness branch")
	return nil
}

// ensureBaseRepo 确保基础仓库存在并已 fetch（对齐 _ensure_base_repo）。
//
// 三分支：本地仓库 fetch / 缓存已有 fetch / 首次 clone。
func (w *WorktreeManager) ensureBaseRepo() (string, error) {
	cfg := w.configFn()
	base := w.baseRepo()

	if cfg.LocalRepo != "" {
		// 本地仓库：只 fetch
		if !pathExists(base) {
			return "", fmt.Errorf("local_repo not found: %s", base)
		}
		code, out := w.runGit([]string{"fetch", "origin"}, base)
		if code != 0 {
			logger.Warn(logComponent).
				Str("output", out).
				Msg("fetch failed in local_repo (continuing)")
		}
		return base, nil
	}

	// 无本地仓库：确保 clone 缓存（.git 目录或裸仓库 HEAD 文件）
	if isDir(filepath.Join(base, ".git")) || (isDir(base) && isRegularFile(filepath.Join(base, "HEAD"))) {
		// 已有缓存，fetch
		code, out := w.runGit([]string{"fetch", "origin"}, base)
		if code != 0 {
			logger.Warn(logComponent).
				Str("output", out).
				Msg("fetch failed in cache repo (continuing)")
		}
		return base, nil
	}

	// 首次 clone
	baseBranch := cfg.GitBaseBranch
	if baseBranch == "" {
		baseBranch = "develop"
	}
	parent := filepath.Dir(base)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("git clone failed: %v", err)
	}
	code, out := w.runGit([]string{
		"clone", "-b", baseBranch, cfg.RepoURL, base,
	}, parent)
	if code != 0 {
		return "", fmt.Errorf("git clone failed: %s", out)
	}
	logger.Info(logComponent).
		Str("repo", base).
		Msg("Cloned repo")
	return base, nil
}

// runGit 执行 git 命令（对齐 _run_git；.strip() 语义）
func (w *WorktreeManager) runGit(args []string, cwd string) (int, string) {
	result, err := w.runner.Run(w.ctx, cwd, envMapToSlice(w.gitEnv), "git", args...)
	if err != nil {
		return 1, result.Output + err.Error()
	}
	return result.ExitCode, strings.TrimSpace(result.Output)
}

// slugify 将 topic 转为文件系统安全的 slug（对齐 _slugify）
func slugify(topic string) string {
	if m := slugifyIssueRe.FindStringSubmatch(topic); m != nil {
		return fmt.Sprintf("fix-issue-%s", m[1])
	}
	slug := slugifyNonAlnumRe.ReplaceAllString(topic, "-")
	slug = strings.ToLower(strings.Trim(slug, "-"))
	if len([]rune(slug)) > 40 {
		slug = slug[:40]
	}
	if slug == "" {
		return "task"
	}
	return slug
}
