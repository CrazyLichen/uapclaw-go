package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// agePath 把路径修改时间改到过期阈值之前。
func agePath(t *testing.T, path string, days int) {
	t.Helper()
	old := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
}

// ──────────────────────────── 用例 ────────────────────────────

// TestIsEphemeralSlug_两种模式 临时 slug 两种前缀模式。
func TestIsEphemeralSlug_两种模式(t *testing.T) {
	assert.True(t, IsEphemeralSlug("teammate-1a2b3c4d"))
	assert.True(t, IsEphemeralSlug("agent-1a2b3c4"))
	assert.False(t, IsEphemeralSlug("teammate-xyz"))
	assert.False(t, IsEphemeralSlug("owner-work"))
	assert.False(t, IsEphemeralSlug(""))
}

// TestCleanupStaleWorktrees_跳过非临时slug 非临时 slug 不参与清理。
func TestCleanupStaleWorktrees_跳过非临时slug(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	workspace := t.TempDir()
	keep := filepath.Join(WorktreesDir(workspace), "owner-work")
	require.NoError(t, os.MkdirAll(keep, 0o755))
	agePath(t, keep, 90)

	backend := &recordingBackend{removeResult: true}
	removed, err := CleanupStaleWorktrees(worktreeCtx(t, repo, workspace), NewWorktreeConfig(), backend, "")
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

// TestCleanupStaleWorktrees_跳过最近更新 未超过保留期的目录保留。
func TestCleanupStaleWorktrees_跳过最近更新(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	workspace := t.TempDir()
	fresh := filepath.Join(WorktreesDir(workspace), "teammate-1a2b3c4d")
	require.NoError(t, os.MkdirAll(fresh, 0o755))

	backend := &recordingBackend{removeResult: true}
	removed, err := CleanupStaleWorktrees(worktreeCtx(t, repo, workspace), NewWorktreeConfig(), backend, "")
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

// TestCleanupStaleWorktrees_跳过当前worktree 当前会话的 worktree 永不清理。
func TestCleanupStaleWorktrees_跳过当前worktree(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	workspace := t.TempDir()
	current := filepath.Join(WorktreesDir(workspace), "teammate-1a2b3c4d")
	require.NoError(t, os.MkdirAll(current, 0o755))
	agePath(t, current, 90)

	backend := &recordingBackend{removeResult: true}
	removed, err := CleanupStaleWorktrees(worktreeCtx(t, repo, workspace), NewWorktreeConfig(), backend, current)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

// TestCleanupStaleWorktrees_跳过未提交变更 有未提交改动时跳过（fail-closed）。
func TestCleanupStaleWorktrees_跳过未提交变更(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	workspace := t.TempDir()
	dirty := filepath.Join(WorktreesDir(workspace), "teammate-1a2b3c4d")
	require.NoError(t, os.MkdirAll(dirty, 0o755))
	initGitRepoAt(t, dirty)
	require.NoError(t, os.WriteFile(filepath.Join(dirty, "dirty.txt"), []byte("x"), 0o644))
	agePath(t, dirty, 90)

	backend := &recordingBackend{removeResult: true}
	removed, err := CleanupStaleWorktrees(worktreeCtx(t, repo, workspace), NewWorktreeConfig(), backend, "")
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

// TestCleanupStaleWorktrees_删除干净过期worktree 干净且已推送的过期目录被删除。
func TestCleanupStaleWorktrees_删除干净过期worktree(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	workspace := t.TempDir()

	// 造一个干净、已推送、过期的 worktree 目录
	origin := filepath.Join(t.TempDir(), "origin.git")
	require.NoError(t, exec.Command("git", "init", "--bare", origin).Run())
	wtPath := filepath.Join(WorktreesDir(workspace), "teammate-1a2b3c4d")
	require.NoError(t, os.MkdirAll(filepath.Dir(wtPath), 0o755))
	require.NoError(t, exec.Command("git", "clone", origin, wtPath).Run())
	_ = exec.Command("git", "-C", wtPath, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", wtPath, "config", "user.name", "Test").Run()
	require.NoError(t, os.WriteFile(filepath.Join(wtPath, "a.txt"), []byte("x"), 0o644))
	_ = exec.Command("git", "-C", wtPath, "add", ".").Run()
	_ = exec.Command("git", "-C", wtPath, "commit", "-m", "init").Run()
	require.NoError(t, exec.Command("git", "-C", wtPath, "push", "origin", "HEAD:main").Run())
	agePath(t, wtPath, 90)

	backend := &recordingBackend{removeResult: true}
	removed, err := CleanupStaleWorktrees(worktreeCtx(t, repo, workspace), NewWorktreeConfig(), backend, "")
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
	assert.Len(t, backend.removedPaths, 1)
}

// TestCleanupStaleWorktrees_无git仓库 非 git 目录直接跳过。
func TestCleanupStaleWorktrees_无git仓库(t *testing.T) {
	dir := t.TempDir()
	workspace := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(WorktreesDir(workspace), "teammate-1a2b3c4d"), 0o755))

	backend := &recordingBackend{removeResult: true}
	removed, err := CleanupStaleWorktrees(worktreeCtx(t, dir, workspace), NewWorktreeConfig(), backend, "")
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

// TestCleanupStaleWorktrees_无workspace 未设置 workspace 时跳过。
func TestCleanupStaleWorktrees_无workspace(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)

	backend := &recordingBackend{removeResult: true}
	removed, err := CleanupStaleWorktrees(worktreeCtx(t, repo, ""), NewWorktreeConfig(), backend, "")
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

// TestAutoSetupRail_执行命令 setup 命令在 worktree 目录内执行。
func TestAutoSetupRail_执行命令(t *testing.T) {
	dir := t.TempDir()
	rail := NewAutoSetupRail(WithCommands("echo ok > setup.log"))

	require.NoError(t, rail.AfterWorktreeCreate(context.Background(), nil, &WorktreeSession{WorktreePath: dir}))
	content, err := os.ReadFile(filepath.Join(dir, "setup.log"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "ok")
}

// TestAutoSetupRail_命令失败不报错 失败命令只记日志，不影响流程。
func TestAutoSetupRail_命令失败不报错(t *testing.T) {
	dir := t.TempDir()
	rail := NewAutoSetupRail(WithCommands("exit 1"))

	assert.NoError(t, rail.AfterWorktreeCreate(context.Background(), nil, &WorktreeSession{WorktreePath: dir}))
}

// TestAutoSetupRail_空实现 其余 hook 均为不干预语义。
func TestAutoSetupRail_空实现(t *testing.T) {
	rail := NewAutoSetupRail()
	ctx := context.Background()
	session := &WorktreeSession{}

	slug, err := rail.BeforeWorktreeCreate(ctx, nil, "s", "/repo")
	require.NoError(t, err)
	assert.Nil(t, slug)

	action, err := rail.BeforeWorktreeExit(ctx, nil, session, "keep")
	require.NoError(t, err)
	assert.Nil(t, action)

	assert.NoError(t, rail.AfterWorktreeExit(ctx, nil, session, "keep"))
	assert.True(t, rail.OnWorktreeFileWrite(ctx, nil, session, "f"))

	msg, err := rail.BeforeWorktreeCommit(ctx, nil, session, "msg", nil)
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "msg", *msg)

	assert.NoError(t, rail.AfterWorktreeCommit(ctx, nil, session, "sha"))
	assert.Equal(t, []string{"a"}, rail.OnWorktreeSync(ctx, nil, session, "push", []string{"a"}))
}
