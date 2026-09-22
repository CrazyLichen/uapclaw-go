package worktree

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GitError Git 命令执行错误。
// Python: GitError(Exception)
type GitError struct {
	// Command 失败的命令
	Command string
	// ReturnCode 退出码
	ReturnCode int
	// Stderr 标准错误输出
	Stderr string
}

// GitResult Git 命令执行结果。
// Python: GitResult(frozen dataclass)
type GitResult struct {
	// ReturnCode 退出码
	ReturnCode int
	// Stdout 标准输出
	Stdout string
	// Stderr 标准错误
	Stderr string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// gitCommandTimeout Git 命令默认超时时间。
const gitCommandTimeout = 30 * time.Second

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// findGitRoot 查找 git 仓库根目录。
// Python: find_git_root(cwd)
func findGitRoot(ctx context.Context, cwd string) (string, error) {
	r := runGit(ctx, []string{"rev-parse", "--show-toplevel"}, cwd)
	if !r.OK() {
		return "", fmt.Errorf("不在 git 仓库中: %s", r.Stderr)
	}
	return r.Stdout, nil
}

// findCanonicalGitRoot 从 worktree 中查找主仓库根目录。
// Python: find_canonical_git_root(cwd)
//
// 如果 cwd 在 worktree 内，返回父仓库根目录。
func findCanonicalGitRoot(ctx context.Context, cwd string) (string, error) {
	// 先尝试通过 resolveGitDir 获取 .git 目录
	gitDir, err := resolveGitDir(ctx, cwd)
	if err != nil || gitDir == "" {
		// 降级为 findGitRoot
		return findGitRoot(ctx, cwd)
	}

	// 检查是否为 worktree（有 commondir 文件）
	commondirPath := filepath.Join(gitDir, "commondir")
	if info, err := os.Stat(commondirPath); err == nil && !info.IsDir() {
		data, err := os.ReadFile(commondirPath)
		if err != nil {
			return findGitRoot(ctx, cwd)
		}
		common := strings.TrimSpace(string(data))
		commonAbs := filepath.Clean(filepath.Join(gitDir, common))
		// commondir 指向共享 .git 目录
		if filepath.Base(commonAbs) == ".git" {
			return filepath.Dir(commonAbs), nil
		}
		return commonAbs, nil
	}

	// 普通仓库：gitDir 是 <root>/.git
	return findGitRoot(ctx, cwd)
}

// getCurrentBranch 获取当前分支名。
// Python: get_current_branch(cwd)
func getCurrentBranch(ctx context.Context, cwd string) (string, error) {
	r := runGit(ctx, []string{"rev-parse", "--abbrev-ref", "HEAD"}, cwd)
	if !r.OK() || r.Stdout == "HEAD" {
		return "", fmt.Errorf("HEAD 处于分离状态或不在仓库中")
	}
	return r.Stdout, nil
}

// getDefaultBranch 检测默认分支（main/master）。
// Python: get_default_branch(cwd)
//
// 尝试 symbolic-ref，然后回退探测常见名称，最终回退 "main"。
func getDefaultBranch(ctx context.Context, cwd string) string {
	r := runGit(ctx, []string{"symbolic-ref", "refs/remotes/origin/HEAD", "--short"}, cwd)
	if r.OK() {
		// "origin/main" -> "main"
		parts := strings.SplitN(r.Stdout, "/", 2)
		if len(parts) > 1 {
			return parts[1]
		}
		return r.Stdout
	}
	// 回退：尝试常见名称
	for _, name := range []string{"main", "master"} {
		check := runGit(ctx, []string{"rev-parse", "--verify", "origin/" + name}, cwd)
		if check.OK() {
			return name
		}
	}
	return "main"
}

// revParse 解析 ref 到 SHA。
// Python: rev_parse(ref, cwd)
func revParse(ctx context.Context, ref, cwd string) (string, error) {
	r := runGit(ctx, []string{"rev-parse", ref}, cwd)
	if !r.OK() {
		return "", fmt.Errorf("rev-parse %s 失败: %s", ref, r.Stderr)
	}
	return r.Stdout, nil
}

// worktreeAdd 创建新 git worktree。
// Python: worktree_add(repo_root, worktree_path, branch_name, base_ref, *, no_checkout=False)
func worktreeAdd(ctx context.Context, repoRoot, wtPath, branch, baseRef string, noCheckout bool) error {
	args := []string{"worktree", "add"}
	if noCheckout {
		args = append(args, "--no-checkout")
	}
	args = append(args, "-B", branch, wtPath, baseRef)
	r := runGit(ctx, args, repoRoot)
	if !r.OK() {
		return &GitError{Command: "worktree add", ReturnCode: r.ReturnCode, Stderr: r.Stderr}
	}
	return nil
}

// worktreeRemove 删除 git worktree。
// Python: worktree_remove(worktree_path, *, repo_root, force=False)
func worktreeRemove(ctx context.Context, wtPath, repoRoot string, force bool) bool {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, wtPath)
	r := runGit(ctx, args, repoRoot)
	return r.OK()
}

// worktreePrune 清理过期的 worktree 引用。
// Python: worktree_prune(repo_root)
func worktreePrune(ctx context.Context, repoRoot string) error {
	runGit(ctx, []string{"worktree", "prune"}, repoRoot)
	return nil
}

// branchDelete 删除本地 git 分支。
// Python: branch_delete(branch, repo_root)
func branchDelete(ctx context.Context, branch, repoRoot string) bool {
	r := runGit(ctx, []string{"branch", "-D", branch}, repoRoot)
	return r.OK()
}

// fetchRef 从远程获取指定 ref。
// Python: fetch_ref(repo_root, ref, *, remote="origin")
func fetchRef(ctx context.Context, repoRoot, ref string, remote ...string) bool {
	remoteName := "origin"
	if len(remote) > 0 && remote[0] != "" {
		remoteName = remote[0]
	}
	r := runGit(ctx, []string{"fetch", remoteName, ref}, repoRoot)
	return r.OK()
}

// sparseCheckoutSet 配置稀疏检出（cone 模式）。
// Python: sparse_checkout_set(worktree_path, paths)
func sparseCheckoutSet(ctx context.Context, wtPath string, paths []string) error {
	args := []string{"sparse-checkout", "set", "--cone", "--"}
	args = append(args, paths...)
	r := runGit(ctx, args, wtPath)
	if !r.OK() {
		return &GitError{Command: "sparse-checkout set", ReturnCode: r.ReturnCode, Stderr: r.Stderr}
	}
	// 执行 checkout HEAD
	checkout := runGit(ctx, []string{"checkout", "HEAD"}, wtPath)
	if !checkout.OK() {
		return &GitError{Command: "checkout HEAD", ReturnCode: checkout.ReturnCode, Stderr: checkout.Stderr}
	}
	return nil
}

// statusPorcelain 获取未提交变更文件列表。
// Python: status_porcelain(cwd)
func statusPorcelain(ctx context.Context, cwd string) ([]string, error) {
	r := runGit(ctx, []string{"status", "--porcelain"}, cwd)
	if !r.OK() {
		return nil, fmt.Errorf("git status 失败: %s", r.Stderr)
	}
	var result []string
	for _, line := range strings.Split(r.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result, nil
}

// countCommitsSince 计算 base_commit 以来的提交数。
// Python: count_commits_since(base_commit, cwd)
//
// 返回 nil 表示无法确定（fail-closed）。
func countCommitsSince(ctx context.Context, baseCommit, cwd string) *int {
	r := runGit(ctx, []string{"rev-list", "--count", baseCommit + "..HEAD"}, cwd)
	if !r.OK() {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(r.Stdout))
	if err != nil {
		return nil
	}
	return &n
}

// hasUnpushedCommits 检查是否有未推送的提交。
// Python: has_unpushed_commits(cwd)
//
// 返回 nil 表示检查失败（fail-closed：调用方应视为有变更）。
func hasUnpushedCommits(ctx context.Context, cwd string) *bool {
	r := runGit(ctx, []string{"rev-list", "--max-count=1", "HEAD", "--not", "--remotes"}, cwd)
	if !r.OK() {
		return nil
	}
	result := len(strings.TrimSpace(r.Stdout)) > 0
	return &result
}

// readWorktreeHeadSHA 快速路径：不调 git 子进程，直接读 HEAD SHA。
// Python: read_worktree_head_sha(worktree_path)
//
// 读取 .git 文件 → gitdir → HEAD → resolve ref。~0.5ms vs ~15ms for git rev-parse HEAD。
func readWorktreeHeadSHA(wtPath string) (string, error) {
	gitFile := filepath.Join(wtPath, ".git")
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir:") {
		return "", fmt.Errorf("无效的 .git 文件格式: %s", wtPath)
	}

	gitDir := filepath.Clean(filepath.Join(wtPath, strings.TrimSpace(strings.TrimPrefix(content, "gitdir:"))))
	headFile := filepath.Join(gitDir, "HEAD")
	headData, err := os.ReadFile(headFile)
	if err != nil {
		return "", err
	}
	head := strings.TrimSpace(string(headData))

	// Detached HEAD：直接是 SHA
	if !strings.HasPrefix(head, "ref:") {
		if len(head) == 40 {
			return head, nil
		}
		return "", fmt.Errorf("无效的 HEAD SHA 长度: %s", headFile)
	}

	// 分支引用：解析到 SHA
	refPath := strings.TrimSpace(strings.TrimPrefix(head, "ref:"))

	// 先尝试 worktree 本地 refs
	fullRef := filepath.Join(gitDir, refPath)
	if sha, err := os.ReadFile(fullRef); err == nil {
		return strings.TrimSpace(string(sha)), nil
	}

	// 从 commondir 解析
	commondirFile := filepath.Join(gitDir, "commondir")
	commonData, err := os.ReadFile(commondirFile)
	if err != nil {
		return "", err
	}
	common := strings.TrimSpace(string(commonData))
	commonAbs := filepath.Clean(filepath.Join(gitDir, common))
	fullRef = filepath.Join(commonAbs, refPath)
	sha, err := os.ReadFile(fullRef)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(sha)), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// OK 检查 GitResult 是否成功。
func (r GitResult) OK() bool { return r.ReturnCode == 0 }

// Error 返回 GitError 的错误描述。
func (e *GitError) Error() string {
	return fmt.Sprintf("git %s 失败 (rc=%d): %s", e.Command, e.ReturnCode, e.Stderr)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runGit 核心 Git 命令执行器。
// Python: _run_git(args, *, cwd, check)
//
// 使用 os/exec.CommandContext + 30s 超时，设置 GIT_TERMINAL_PROMPT=0。
func runGit(ctx context.Context, args []string, cwd string) GitResult {
	// 30s 超时上下文
	execCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "git", args...)
	cmd.Dir = cwd
	cmd.Env = gitEnv()
	cmd.Stdin = nil

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	returnCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			returnCode = exitErr.ExitCode()
		} else {
			returnCode = -1
		}
	}

	return GitResult{
		ReturnCode: returnCode,
		Stdout:     strings.TrimSpace(stdout.String()),
		Stderr:     strings.TrimSpace(stderr.String()),
	}
}

// gitEnv 构建抑制交互式提示的环境变量。
// Python: _git_env() — dict 会覆盖同名 key，Go 用 append 会追加重复 key，
// 因此先移除已有的 GIT_ASKPASS 条目再追加，确保宿主环境的值被覆盖
func gitEnv() []string {
	env := os.Environ()
	// 先移除已有的 GIT_ASKPASS，避免 append 后出现重复 key
	filtered := make([]string, 0, len(env)+2)
	for _, e := range env {
		if !strings.HasPrefix(e, "GIT_ASKPASS=") {
			filtered = append(filtered, e)
		}
	}
	filtered = append(filtered, "GIT_TERMINAL_PROMPT=0")
	filtered = append(filtered, "GIT_ASKPASS=")
	return filtered
}

// resolveGitDir 获取 .git 目录路径（worktree 也适用）。
// Python: resolve_git_dir(cwd)
func resolveGitDir(ctx context.Context, cwd string) (string, error) {
	r := runGit(ctx, []string{"rev-parse", "--git-dir"}, cwd)
	if !r.OK() {
		return "", fmt.Errorf("不在 git 仓库中")
	}
	gitDir := r.Stdout
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(cwd, gitDir)
	}
	return filepath.Clean(gitDir), nil
}
