package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newGitBackend 构造默认配置的 git 后端。
func newGitBackend(t *testing.T) *GitBackend {
	t.Helper()
	cfg := NewWorktreeConfig()
	return &GitBackend{config: cfg}
}

// ──────────────────────────── 用例 ────────────────────────────

// TestGitBackend_Create_创建worktree 新建 worktree 并返回分支与基准。
func TestGitBackend_Create_创建worktree(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	target := filepath.Join(t.TempDir(), "wt")

	backend := newGitBackend(t)
	result, err := backend.Create(context.Background(), "feature-a", repo, target)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Existed)
	assert.Equal(t, target, result.WorktreePath)
	assert.Equal(t, "worktree-feature-a", result.WorktreeBranch)
	assert.NotEmpty(t, result.HeadCommit)
	assert.DirExists(t, target)
}

// TestGitBackend_Create_已存在时快速恢复 已有 worktree 直接返回 Existed=true。
func TestGitBackend_Create_已存在时快速恢复(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	target := filepath.Join(t.TempDir(), "wt")

	backend := newGitBackend(t)
	_, err := backend.Create(context.Background(), "feature-b", repo, target)
	require.NoError(t, err)

	again, err := backend.Create(context.Background(), "feature-b", repo, target)
	require.NoError(t, err)
	assert.True(t, again.Existed)
	assert.NotEmpty(t, again.HeadCommit)
}

// TestGitBackend_Create_稀疏检出 配置稀疏路径时只检出指定目录。
func TestGitBackend_Create_稀疏检出(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "src", "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "docs.md"), []byte("docs"), 0o644))
	_ = exec.Command("git", "-C", repo, "add", ".").Run()
	_ = exec.Command("git", "-C", repo, "commit", "-m", "add src").Run()

	cfg := NewWorktreeConfig()
	cfg.SparsePaths = []string{"src"}
	backend := &GitBackend{config: cfg}
	target := filepath.Join(t.TempDir(), "wt")

	result, err := backend.Create(context.Background(), "feature-c", repo, target)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.FileExists(t, filepath.Join(target, "src", "main.go"))
}

// TestGitBackend_Remove_删除worktree与分支 删除后目录与 worktree 分支消失。
func TestGitBackend_Remove_删除worktree与分支(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	target := filepath.Join(t.TempDir(), "wt")

	backend := newGitBackend(t)
	_, err := backend.Create(context.Background(), "feature-d", repo, target)
	require.NoError(t, err)

	assert.True(t, backend.Remove(context.Background(), target, repo, true))
	assert.NoDirExists(t, target)

	branches := exec.Command("git", "-C", repo, "branch", "--list", "worktree-feature-d")
	out, _ := branches.Output()
	assert.Empty(t, string(out), "worktree 分支应被删除")
}

// TestGitBackend_Exists_存在性检查 依据 .git 文件判定。
func TestGitBackend_Exists_存在性检查(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	target := filepath.Join(t.TempDir(), "wt")

	backend := newGitBackend(t)
	assert.False(t, backend.Exists(context.Background(), target))

	_, err := backend.Create(context.Background(), "feature-e", repo, target)
	require.NoError(t, err)
	assert.True(t, backend.Exists(context.Background(), target))
}

// TestGitBackend_ResolveBase_本地分支优先 本地存在检出的分支时优先用本地分支名。
// Python: _resolve_base "local state first"（backend.py:225-262），本地分支
// 优先于 origin ref（含未推送提交）；无 origin 时也不做 fetch。
func TestGitBackend_ResolveBase_本地分支优先(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)

	backend := newGitBackend(t)
	branch, sha := backend.resolveBase(context.Background(), repo)
	assert.NotEmpty(t, sha)
	// 本地检出的分支名（refs/heads/<branch>^commit 命中），不再回退 "HEAD"
	assert.NotEqual(t, "HEAD", branch)
	current, err := getCurrentBranch(context.Background(), repo)
	require.NoError(t, err)
	assert.Equal(t, current, branch)
}

// TestGitBackend_ResolveBase_有origin引用仍本地优先 存在 origin/<default> 时
// 本地检出的分支仍优先（Python _resolve_base 候选序：refs/heads 先于 refs/remotes）。
// fetch 只发生在 RemoteBase=true 的远端节点路径。
func TestGitBackend_ResolveBase_有origin引用仍本地优先(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	require.NoError(t, exec.Command("git", "init", "--bare", origin).Run())

	repo := filepath.Join(t.TempDir(), "repo")
	require.NoError(t, exec.Command("git", "clone", origin, repo).Run())
	_ = exec.Command("git", "-C", repo, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", repo, "config", "user.name", "Test").Run()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.txt"), []byte("x"), 0o644))
	_ = exec.Command("git", "-C", repo, "add", ".").Run()
	_ = exec.Command("git", "-C", repo, "commit", "-m", "init").Run()
	require.NoError(t, exec.Command("git", "-C", repo, "push", "origin", "HEAD:main").Run())
	require.NoError(t, exec.Command("git", "-C", repo, "fetch", "origin").Run())

	backend := newGitBackend(t)
	branch, sha := backend.resolveBase(context.Background(), repo)
	assert.NotEmpty(t, sha)
	assert.NotEqual(t, "HEAD", branch)
	current, err := getCurrentBranch(context.Background(), repo)
	require.NoError(t, err)
	assert.Equal(t, current, branch)
}

// TestGitBackend_ResolveBase_远端模式用origin RemoteBase=true 时走远端节点路径：
// origin-first + fetch，命中 origin/<default>。
// Python: _resolve_remote_base（backend.py:264-301）
func TestGitBackend_ResolveBase_远端模式用origin(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	require.NoError(t, exec.Command("git", "init", "--bare", origin).Run())

	repo := filepath.Join(t.TempDir(), "repo")
	require.NoError(t, exec.Command("git", "clone", origin, repo).Run())
	_ = exec.Command("git", "-C", repo, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", repo, "config", "user.name", "Test").Run()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.txt"), []byte("x"), 0o644))
	_ = exec.Command("git", "-C", repo, "add", ".").Run()
	_ = exec.Command("git", "-C", repo, "commit", "-m", "init").Run()
	require.NoError(t, exec.Command("git", "-C", repo, "push", "origin", "HEAD:main").Run())
	require.NoError(t, exec.Command("git", "-C", repo, "fetch", "origin").Run())

	backend := newGitBackend(t)
	backend.RemoteBase = true
	branch, sha := backend.resolveBase(context.Background(), repo)
	assert.NotEmpty(t, sha)
	assert.Equal(t, "origin/main", branch)
}

// TestGitBackend_ResolveBase_未出生仓库建空初始提交 未出生仓库（无任何提交）
// 时应经 plumbing 建空初始提交，不吞用户暂存文件。
// Python: backend.py:258-262 create_empty_initial_commit
func TestGitBackend_ResolveBase_未出生仓库建空初始提交(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, exec.Command("git", "init", repo).Run())
	_ = exec.Command("git", "-C", repo, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", repo, "config", "user.name", "Test").Run()
	// 用户已暂存但未提交的文件——空初始提交不得吞掉
	require.NoError(t, os.WriteFile(filepath.Join(repo, "staged.txt"), []byte("x"), 0o644))
	require.NoError(t, exec.Command("git", "-C", repo, "add", ".").Run())

	backend := newGitBackend(t)
	branch, sha := backend.resolveBase(context.Background(), repo)
	assert.Equal(t, "HEAD", branch)
	assert.NotEmpty(t, sha)
	// HEAD 已指向空初始提交
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	assert.Equal(t, sha, strings.TrimSpace(string(out)))
	// 暂存文件仍在
	_, err = os.Stat(filepath.Join(repo, "staged.txt"))
	require.NoError(t, err)
}
