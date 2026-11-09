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

	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
)

// ──────────────────────────── 测试用结构体 ────────────────────────────

// recordingBackend 记录调用参数的 WorktreeBackend 替身。
type recordingBackend struct {
	// createResult Create 返回的结果
	createResult *WorktreeCreateResult
	// createErr Create 返回的错误
	createErr error
	// removeResult Remove 返回值
	removeResult bool
	// createdSlug 最近一次创建使用的 slug
	createdSlug string
	// removedPaths 已删除的路径
	removedPaths []string
}

// Create 记录并返回预设结果。
func (b *recordingBackend) Create(_ context.Context, slug, _, _ string) (*WorktreeCreateResult, error) {
	b.createdSlug = slug
	if b.createErr != nil {
		return nil, b.createErr
	}
	return b.createResult, nil
}

// Remove 记录删除路径。
func (b *recordingBackend) Remove(_ context.Context, wtPath, _ string) bool {
	b.removedPaths = append(b.removedPaths, wtPath)
	return b.removeResult
}

// Exists 固定返回 false。
func (b *recordingBackend) Exists(_ context.Context, _ string) bool { return false }

// fakeLifecycleRail 记录 hook 调用的生命周期轨替身。
type fakeLifecycleRail struct {
	// slugResult BeforeWorktreeCreate 返回值
	slugResult *string
	// createErr BeforeWorktreeCreate 返回的错误
	createErr error
	// actionResult BeforeWorktreeExit 返回值
	actionResult *string
	// allowWrite OnWorktreeFileWrite 返回值
	allowWrite bool
	// commitResult BeforeWorktreeCommit 返回值
	commitResult *string
	// syncFiles OnWorktreeSync 返回的过滤结果
	syncFiles []string
	// afterCreateCalls AfterWorktreeCreate 调用次数
	afterCreateCalls int
	// afterExitCalls AfterWorktreeExit 调用次数
	afterExitCalls int
	// afterCommitCalls AfterWorktreeCommit 调用次数
	afterCommitCalls int
}

// BeforeWorktreeCreate 返回预设 slug。
func (f *fakeLifecycleRail) BeforeWorktreeCreate(_ context.Context, _ *interfaces.AgentCallbackContext, _, _ string) (*string, error) {
	return f.slugResult, f.createErr
}

// AfterWorktreeCreate 记录调用。
func (f *fakeLifecycleRail) AfterWorktreeCreate(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession) error {
	f.afterCreateCalls++
	return nil
}

// BeforeWorktreeExit 返回预设 action。
func (f *fakeLifecycleRail) BeforeWorktreeExit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) (*string, error) {
	return f.actionResult, nil
}

// AfterWorktreeExit 记录调用。
func (f *fakeLifecycleRail) AfterWorktreeExit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) error {
	f.afterExitCalls++
	return nil
}

// OnWorktreeFileWrite 返回预设放行结果。
func (f *fakeLifecycleRail) OnWorktreeFileWrite(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) bool {
	return f.allowWrite
}

// BeforeWorktreeCommit 返回预设提交消息。
func (f *fakeLifecycleRail) BeforeWorktreeCommit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string, _ []string) (*string, error) {
	return f.commitResult, nil
}

// AfterWorktreeCommit 记录调用。
func (f *fakeLifecycleRail) AfterWorktreeCommit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) error {
	f.afterCommitCalls++
	return nil
}

// OnWorktreeSync 返回预设文件列表。
func (f *fakeLifecycleRail) OnWorktreeSync(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string, _ []string) []string {
	return f.syncFiles
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// worktreeCtx 构造带 cwd/workspace 的 context。
func worktreeCtx(t *testing.T, repoRoot, workspace string) context.Context {
	t.Helper()
	state := cwd.InitCwd(repoRoot, cwd.WithWorkspace(workspace))
	return cwd.WithCwdState(context.Background(), state)
}

// initGitRepoAt 在指定目录初始化带初始提交的 git 仓库。
func initGitRepoAt(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, exec.Command("git", "init", dir).Run())
	_ = exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# t"), 0o644))
	_ = exec.Command("git", "-C", dir, "add", ".").Run()
	_ = exec.Command("git", "-C", dir, "commit", "-m", "init").Run()
}

// ──────────────────────────── 用例 ────────────────────────────

// TestCreateOwnerWorktree_成功 后端创建成功后返回结果并跑后置设置。
func TestCreateOwnerWorktree_成功(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	workspace := t.TempDir()
	target := WorktreePathFor(workspace, "owner-1")
	require.NoError(t, os.MkdirAll(target, 0o755))

	backend := &recordingBackend{createResult: &WorktreeCreateResult{
		WorktreePath: target,
		Existed:      false,
	}}
	mgr := NewWorktreeManager(NewWorktreeConfig(), backend)

	result, err := mgr.CreateOwnerWorktree(worktreeCtx(t, repo, workspace), "owner-1")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "owner-1", backend.createdSlug)
}

// TestCreateOwnerWorktree_已存在刷新mtime 已存在时不跑后置设置但仍成功。
func TestCreateOwnerWorktree_已存在刷新mtime(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	workspace := t.TempDir()
	target := WorktreePathFor(workspace, "owner-2")
	require.NoError(t, os.MkdirAll(target, 0o755))

	backend := &recordingBackend{createResult: &WorktreeCreateResult{
		WorktreePath: target,
		Existed:      true,
	}}
	mgr := NewWorktreeManager(NewWorktreeConfig(), backend)

	result, err := mgr.CreateOwnerWorktree(worktreeCtx(t, repo, workspace), "owner-2")
	require.NoError(t, err)
	assert.True(t, result.Existed)
}

// TestCreateOwnerWorktree_非法slug 非法 slug 在解析阶段被拒。
func TestCreateOwnerWorktree_非法slug(t *testing.T) {
	mgr := NewWorktreeManager(NewWorktreeConfig(), &recordingBackend{})
	_, err := mgr.CreateOwnerWorktree(context.Background(), "../escape")
	require.Error(t, err)
}

// TestCreateOwnerWorktree_不在git仓库 非 git 目录直接报错。
func TestCreateOwnerWorktree_不在git仓库(t *testing.T) {
	dir := t.TempDir()
	mgr := NewWorktreeManager(NewWorktreeConfig(), &recordingBackend{})
	_, err := mgr.CreateOwnerWorktree(worktreeCtx(t, dir, t.TempDir()), "owner-3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不在 git 仓库中")
}

// TestCreateOwnerWorktree_后端失败 后端报错原样传播。
func TestCreateOwnerWorktree_后端失败(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	workspace := t.TempDir()
	mgr := NewWorktreeManager(NewWorktreeConfig(), &recordingBackend{createErr: errors.New("boom")})

	_, err := mgr.CreateOwnerWorktree(worktreeCtx(t, repo, workspace), "owner-4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

// TestRecoverWorktreeForOwner_成功 已有 worktree 时恢复出会话。
func TestRecoverWorktreeForOwner_成功(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	workspace := t.TempDir()
	slug := ownerSlug("owner-abc")
	target := WorktreePathFor(workspace, slug)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, exec.Command("git", "-C", repo, "worktree", "add", target, "-b", "recover-branch").Run())

	mgr := NewWorktreeManager(NewWorktreeConfig(), &recordingBackend{})
	session, err := mgr.RecoverWorktreeForOwner(worktreeCtx(t, repo, workspace), "owner-abc", "team-1")
	require.NoError(t, err)
	require.NotNil(t, session)
	assert.Equal(t, slug, session.WorktreeName)
	assert.Equal(t, "team-1", session.TeamName)
	assert.NotEmpty(t, session.OriginalHeadCommit)
}

// TestRecoverWorktreeForOwner_无worktree 目标目录不是 git 仓库时返回 nil。
func TestRecoverWorktreeForOwner_无worktree(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	workspace := t.TempDir()

	mgr := NewWorktreeManager(NewWorktreeConfig(), &recordingBackend{})
	session, err := mgr.RecoverWorktreeForOwner(worktreeCtx(t, repo, workspace), "owner-none", "")
	require.NoError(t, err)
	assert.Nil(t, session)
}

// TestRemoveWorktree_委托后端 删除单个 worktree 走后端。
func TestRemoveWorktree_委托后端(t *testing.T) {
	backend := &recordingBackend{removeResult: true}
	mgr := NewWorktreeManager(NewWorktreeConfig(), backend)

	assert.True(t, mgr.RemoveWorktree(context.Background(), "/tmp/wt", "/tmp/repo"))
	assert.Equal(t, []string{"/tmp/wt"}, backend.removedPaths)
}

// TestCleanupWorktreesByPrefix_按前缀清理 只删除前缀匹配的目录。
func TestCleanupWorktreesByPrefix_按前缀清理(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	workspace := t.TempDir()
	wtDir := WorktreesDir(workspace)
	require.NoError(t, os.MkdirAll(filepath.Join(wtDir, "teammate-a"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(wtDir, "other-b"), 0o755))

	backend := &recordingBackend{removeResult: true}
	mgr := NewWorktreeManager(NewWorktreeConfig(), backend)

	removed, err := mgr.CleanupTeamWorktrees(worktreeCtx(t, repo, workspace), "team-1", true)
	require.NoError(t, err)
	require.Len(t, removed, 1)
	assert.Contains(t, removed[0], "teammate-a")
}

// TestCleanupWorktreesByPrefix_持久策略跳过 durable 策略且非强制时整体跳过。
func TestCleanupWorktreesByPrefix_持久策略跳过(t *testing.T) {
	cfg := NewWorktreeConfig()
	cfg.LifecyclePolicy = WorktreeLifecyclePolicyDurable
	mgr := NewWorktreeManager(cfg, &recordingBackend{removeResult: true})

	removed, err := mgr.CleanupTeamWorktrees(context.Background(), "team-1", false)
	require.NoError(t, err)
	assert.Empty(t, removed)
}

// TestCleanupWorktreesByPrefix_无workspace 未设置 workspace 时跳过。
func TestCleanupWorktreesByPrefix_无workspace(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	mgr := NewWorktreeManager(NewWorktreeConfig(), &recordingBackend{removeResult: true})

	removed, err := mgr.CleanupWorktreesByPrefix(worktreeCtx(t, repo, ""), "teammate-", true)
	require.NoError(t, err)
	assert.Empty(t, removed)
}

// TestCheckChanges_统计未提交变更 有改动时返回变更文件数。
func TestCheckChanges_统计未提交变更(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o644))

	mgr := NewWorktreeManager(NewWorktreeConfig(), &recordingBackend{})
	summary := mgr.checkChanges(context.Background(), repo)
	require.NotNil(t, summary)
	assert.Equal(t, 1, summary.ChangedFiles)

	assert.Nil(t, mgr.checkChanges(context.Background(), filepath.Join(repo, "missing")), "非 git 目录返回 nil")
}

// TestFireLifecycleHooks_全链路 八个 hook 都转发到生命周期轨。
func TestFireLifecycleHooks_全链路(t *testing.T) {
	slugOverride := "overridden"
	actionOverride := "keep"
	commitOverride := "msg2"
	rail := &fakeLifecycleRail{
		slugResult:   &slugOverride,
		actionResult: &actionOverride,
		allowWrite:   false,
		commitResult: &commitOverride,
		syncFiles:    []string{"a.go"},
	}
	mgr := NewWorktreeManager(NewWorktreeConfig(), &recordingBackend{}, WithLifecycleRails(rail))
	ctx := context.Background()
	session := &WorktreeSession{WorktreeName: "w"}

	slug, err := mgr.fireBeforeCreate(ctx, nil, "s", "/repo")
	require.NoError(t, err)
	assert.Equal(t, "overridden", *slug)

	mgr.fireAfterCreate(ctx, nil, session)
	assert.Equal(t, 1, rail.afterCreateCalls)

	action, err := mgr.fireBeforeExit(ctx, nil, session, "commit")
	require.NoError(t, err)
	assert.Equal(t, "keep", *action)

	mgr.fireAfterExit(ctx, nil, session, "keep")
	assert.Equal(t, 1, rail.afterExitCalls)

	assert.False(t, mgr.fireOnFileWrite(ctx, nil, session, "a.go"), "轨拒绝时必须阻止写入")

	msg, err := mgr.fireBeforeCommit(ctx, nil, session, "msg", []string{"a.go"})
	require.NoError(t, err)
	assert.Equal(t, "msg2", *msg)

	mgr.fireAfterCommit(ctx, nil, session, "sha")
	assert.Equal(t, 1, rail.afterCommitCalls)

	assert.Equal(t, []string{"a.go"}, mgr.fireOnSync(ctx, nil, session, "push", []string{"b.go"}))
}

// TestFireBeforeCreate_轨报错被吞 单轨报错不影响其他轨，返回 nil 不干预。
func TestFireBeforeCreate_轨报错被吞(t *testing.T) {
	rail := &fakeLifecycleRail{createErr: errors.New("hook boom")}
	mgr := NewWorktreeManager(NewWorktreeConfig(), &recordingBackend{}, WithLifecycleRails(rail))

	slug, err := mgr.fireBeforeCreate(context.Background(), nil, "s", "/repo")
	require.NoError(t, err)
	assert.Nil(t, slug)
}

// TestResolveTargetPath_workspace未设置 workspace 为空时报错。
func TestResolveTargetPath_workspace未设置(t *testing.T) {
	mgr := NewWorktreeManager(NewWorktreeConfig(), &recordingBackend{})
	_, err := mgr.resolveTargetPath(context.Background(), "slug")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace 未设置")
}

// TestResolveTargetPath_非法slug 非法 slug 返回错误。
func TestResolveTargetPath_非法slug(t *testing.T) {
	workspace := t.TempDir()
	mgr := NewWorktreeManager(NewWorktreeConfig(), &recordingBackend{})
	_, err := mgr.resolveTargetPath(worktreeCtx(t, workspace, workspace), "../bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "无效的 worktree 名称")
}

// TestEnter_workspace未设置 缺少 workspace 时拒绝进入。
func TestEnter_workspace未设置(t *testing.T) {
	mgr := NewWorktreeManager(NewWorktreeConfig(), &recordingBackend{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := mgr.Enter(ctx, "slug", "member", "team")
	require.Error(t, err)
}
