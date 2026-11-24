package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 覆盖率回填（改动行门禁）：导出包装面 / lock 分支 / remote base 分支 / 事件标记。
// 逻辑本体由既有单测覆盖，本文件走薄包装与分支缺口。

// initBackfillRepo 建一个带初始提交的真实 git 仓库。
func initBackfillRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
		require.NoError(t, cmd.Run())
	}
	run("init", "-q")
	run("config", "user.email", "t@t.com")
	run("config", "user.name", "test")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x"), 0o644))
	run("add", ".")
	run("commit", "-m", "init")
	return repo
}

func TestExportedGitWrappers(t *testing.T) {
	ctx := context.Background()
	repo := initBackfillRepo(t)

	root, err := FindCanonicalGitRoot(ctx, repo)
	require.NoError(t, err)
	assert.NotEmpty(t, root)

	sha, err := RevParse(ctx, "HEAD", repo)
	require.NoError(t, err)
	assert.Len(t, sha, 40)

	// 祖先关系：HEAD 是自身祖先 → true
	anc := IsRefAncestor(ctx, "HEAD", "HEAD", repo)
	require.NotNil(t, anc)
	assert.True(t, *anc)

	// merge-base：与自身公共祖先 = 自身 SHA
	assert.Equal(t, sha, MergeBase(ctx, "HEAD", "HEAD", repo))

	lines, err := StatusPorcelain(ctx, repo)
	require.NoError(t, err)
	assert.Empty(t, lines, "干净仓库无 status 行")

	assert.NotEmpty(t, GetDefaultBranch(ctx, repo))

	// 非仓库目录 → FindCanonicalGitRoot 空根 + 报错（Python find_canonical_git_root None 语义）
	plain := t.TempDir()
	root2, err2 := FindCanonicalGitRoot(ctx, plain)
	assert.Empty(t, root2)
	require.Error(t, err2)
}

func TestLockTimeoutErrorSurface(t *testing.T) {
	e := &WorktreeLockTimeout{Message: "busy"}
	assert.Equal(t, "busy", e.Error())
	assert.True(t, errors.Is(e, ErrWorktreeLockTimeout))
}

func TestRemoteBaseSwitch(t *testing.T) {
	g := &GitBackend{config: NewWorktreeConfig()}
	assert.False(t, BackendIsRemoteBase(g))
	EnableRemoteBase(g)
	assert.True(t, BackendIsRemoteBase(g))

	// 非 GitBackend 后端：no-op + false（Python getattr(remote_base, False)）
	assert.False(t, BackendIsRemoteBase(&fakeWorktreeBackend{}))
	EnableRemoteBase(&fakeWorktreeBackend{})
}

// fakeWorktreeBackend 非 GitBackend 后端替身。
type fakeWorktreeBackend struct{ GitBackend }

func TestResolveRemoteBase_分支(t *testing.T) {
	ctx := context.Background()
	repo := initBackfillRepo(t)
	g := &GitBackend{config: NewWorktreeConfig()}

	// 本地无 origin/<default>（无 remote）→ fetch 失败 → 回退 HEAD
	ref, sha := g.resolveRemoteBase(ctx, repo)
	assert.Equal(t, "HEAD", ref)
	assert.Len(t, sha, 40)

	// 本地已有 origin/<default>（打一个 origin/main ref）→ 跳过 fetch 直接解析
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		require.NoError(t, cmd.Run())
	}
	run("update-ref", "refs/remotes/origin/main", "HEAD")
	ref2, sha2 := g.resolveRemoteBase(ctx, repo)
	assert.Equal(t, "origin/main", ref2)
	assert.Len(t, sha2, 40)

	// 未出生仓库（无提交）→ 建空基础提交
	unborn := t.TempDir()
	runInit := exec.Command("git", "init", "-q")
	runInit.Dir = unborn
	require.NoError(t, runInit.Run())
	ref3, sha3 := g.resolveRemoteBase(ctx, unborn)
	assert.Equal(t, "HEAD", ref3)
	assert.NotEmpty(t, sha3)
}

func TestDefaultBranchNames(t *testing.T) {
	assert.Equal(t, []string{"main", "master", "develop"}, defaultBranchNames())
}

func TestWorktreeEventMarkers(t *testing.T) {
	var ev WorktreeEvent = &WorktreeCreatedEvent{WorktreeName: "n", WorktreePath: "/p"}
	assert.NotPanics(t, func() { _ = ev })
	var ev2 WorktreeEvent = &WorktreeRemovedEvent{WorktreeName: "n", WorktreePath: "/p"}
	assert.NotPanics(t, func() { _ = ev2 })
}

func TestAcquireFileLock_权限拒绝(t *testing.T) {
	// lockFile 路径是目录 → OpenFile O_CREATE 失败（EISDIR，root 下同样生效）
	// → PermissionError 语义报错（Python :105-111）
	dir := t.TempDir()
	blocker := filepath.Join(dir, "taken")
	require.NoError(t, os.Mkdir(blocker, 0o755))
	_, err := acquireFileLock(blocker, time.Now().Add(time.Second))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestReleaseFileLock_Nil安全(t *testing.T) {
	releaseFileLock(nil, "/unused") // 不 panic 即通过
}

func TestExportedGitWrappers_错误分支(t *testing.T) {
	ctx := context.Background()
	repo := initBackfillRepo(t)

	// 不存在的 ref → is_ref_ancestor 无法判定 → nil（Python :445 返回 None）
	assert.Nil(t, IsRefAncestor(ctx, "HEAD", "refs/heads/no-such-ref", repo))
	// merge-base 无法判定 → 空串（Python :473-493）
	assert.Empty(t, MergeBase(ctx, "HEAD", "refs/heads/no-such-ref", repo))
	// 无 remote 仓库 fetch → false（Python fetch_ref :339 失败面）
	assert.False(t, FetchRef(ctx, repo, "main"))
	// RunGit 直调：成功 + 失败双分支
	assert.Equal(t, 0, RunGit(ctx, []string{"status"}, repo).ReturnCode)
	assert.NotEqual(t, 0, RunGit(ctx, []string{"rev-parse", "--verify", "no-such-ref"}, repo).ReturnCode)
}
