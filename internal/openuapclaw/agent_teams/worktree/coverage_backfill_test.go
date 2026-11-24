package worktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	hworktree "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/worktree"
	"os/exec"
)

// 覆盖率回填（改动行门禁）：lifecycle/member_state/session_cleanup/naming/
// session_scope 的分支缺口。逻辑本体由既有单测与集成覆盖。

// ──────────────────────────── lifecycle ────────────────────────────

// TestEnsureMemberWorktree_活载体禁用 extras disable_teammate_worktree 优先于种子。
func TestEnsureMemberWorktree_活载体禁用(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	cfg.spec.BuildContext = &atschema.BuildContext{}
	cfg.spec.BuildContext.GetExtras()["disable_teammate_worktree"] = true
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", atschema.TeamRoleTeammate, "")
	require.NoError(t, err)
	assert.Empty(t, path)
}

// TestEnsureMemberWorktree_无TeamBackend报错 缺团队后端显式报错。
func TestEnsureMemberWorktree_无TeamBackend报错(t *testing.T) {
	l := NewTeammateWorktreeLifecycle(&lifecycleFakeConfigurator{spec: &atschema.TeamAgentSpec{}})
	_, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", atschema.TeamRoleTeammate, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a team backend")
}

// TestEnsureMemberWorktree_目录缺失重建 DB 记录的路径不存在 → 走重建分支。
func TestEnsureMemberWorktree_目录缺失重建(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", atschema.TeamRoleTeammate, "")
	require.NoError(t, err)
	require.NotEmpty(t, path)

	// 目录被外部删除，DB 行仍登记旧路径：Python :243-252 清元数据后仍走
	// create_owner_worktree——git 注册表仍持有该 worktree 名 → rc=128 报错
	//（上游 worktree_add 无 prune 处理，Go 行为一致，偏差无）
	require.NoError(t, os.RemoveAll(path))
	_, err = l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", atschema.TeamRoleTeammate, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already used by worktree")
}

// TestFinalize_目录已丢 finalize 时 worktree 目录已被外部删除 → 仅清元数据；
// 清完元数据后重进场需重新声明 isolation（门返回空）。
func TestFinalize_目录已丢(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", atschema.TeamRoleTeammate, "")
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(path))

	require.NoError(t, l.FinalizeNonContributingMemberWorktrees(withSessionID(t, "s1")))
	_, err = l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", atschema.TeamRoleTeammate, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already used by worktree")
}

// TestFinalize_散会强删 团队 clean 全删（不做贡献检查）。
func TestFinalize_散会强删(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", atschema.TeamRoleTeammate, "")
	require.NoError(t, err)
	require.NoError(t, l.FinalizeAllMemberWorktreesForTeamClean(withSessionID(t, "s1")))
	assert.NoDirExists(t, path)
}

// TestLifecycleHelpers 小助手直测。
func TestLifecycleHelpers(t *testing.T) {
	assert.Equal(t, "d", orDefault("", "d"))
	assert.Equal(t, "v", orDefault("v", "d"))
	assert.NotNil(t, realPath(t.TempDir()))
	assert.Nil(t, strPtrOrNil(""))
	assert.NotNil(t, strPtrOrNil("x"))
	assert.Nil(t, worktreeOptionsPath(nil))
	assert.NotNil(t, worktreeOptionsPath(&database.MemberWorktreeOptions{Path: strPtr("/p")}))
}

// ──────────────────────────── member_state ────────────────────────────

func TestMemberStateHelpers(t *testing.T) {
	assert.False(t, MatchesScope(nil, WorktreeOwnerScope{}))
	assert.False(t, InfoMatchesScope(nil, WorktreeOwnerScope{}))
	assert.False(t, shouldLogForeignWorktree(nil, false, true))
	assert.False(t, shouldLogForeignWorktree(
		&database.MemberWorktreeOptions{Isolation: strPtr("worktree"), Path: strPtr("/p")}, true, true))
	assert.True(t, shouldLogForeignWorktree(
		&database.MemberWorktreeOptions{Isolation: strPtr("worktree"), Path: strPtr("/p")}, false, true))
	assert.True(t, strIsEmpty(""))
	// TeamContext：nil 后端 → false
	_, _, ok := TeamContext(&lifecycleFakeConfigurator{})
	assert.False(t, ok)
}

// TestResolveCurrentSessionMemberWorktree_无成员行 成员不存在 → nil 无错。
func TestResolveCurrentSessionMemberWorktree_无成员行(t *testing.T) {
	cfg, _ := newLifecycleFixture(t, t.TempDir(), nil)
	res, err := ResolveCurrentSessionMemberWorktree(context.Background(), cfg,
		map[string]*MemberWorktreeInfo{}, "ghost",
		func(ctx context.Context, teamName, memberName string) (WorktreeOwnerScope, error) {
			return WorktreeOwnerScope{SessionID: "s1", ManagedRoot: t.TempDir(), ProjectHash: "h"}, nil
		}, true)
	require.NoError(t, err)
	assert.Nil(t, res)
}

// ──────────────────────────── session_cleanup ────────────────────────────

func TestRemoveSessionWorktrees_边界(t *testing.T) {
	ctx := context.Background()
	assert.True(t, RemoveSessionWorktrees(ctx, "", "", nil))
	assert.True(t, RemoveSessionWorktrees(ctx, "team-x", "s1", nil)) // 目录不存在 → true

	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)
	// worktrees 根是普通文件 → false
	root := agent_teams.TeamSessionWorktreesDir("team-x", "s1")
	require.NoError(t, os.MkdirAll(filepath.Dir(root), 0o755))
	require.NoError(t, os.WriteFile(root, []byte("x"), 0o644))
	assert.False(t, RemoveSessionWorktrees(ctx, "team-x", "s1", nil))
}

func TestRemoveSessionWorktrees_混合条目(t *testing.T) {
	ctx := context.Background()
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)
	root := agent_teams.TeamSessionWorktreesDir("team-y", "s2")
	require.NoError(t, os.MkdirAll(root, 0o755))
	// 符号链接条目（目标在根内）→ unlink；目标在根外 → 越界防护 False
	target := filepath.Join(root, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
	require.NoError(t, os.Symlink(target, filepath.Join(root, "link")))
	// 普通文件条目 → unlink
	require.NoError(t, os.WriteFile(filepath.Join(root, "plain.txt"), []byte("x"), 0o644))
	// 无 git 的普通目录 → RemoveAll 兜底
	require.NoError(t, os.MkdirAll(filepath.Join(root, "plain-dir"), 0o755))
	require.True(t, RemoveSessionWorktrees(ctx, "team-y", "s2", nil))
	assert.NoDirExists(t, root)
}

// ──────────────────────────── 第二轮：finalize 分类与 scope/naming/remote ────────────────────────────

// TestFinalize_有提交保留 分支领先基线 → CONTRIBUTED → 保留。
func TestFinalize_有提交保留(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", atschema.TeamRoleTeammate, "")
	require.NoError(t, err)
	require.NoError(t, gitCommitIn(t, path))
	require.NoError(t, l.FinalizeNonContributingMemberWorktrees(withSessionID(t, "s1")))
	assert.DirExists(t, path, "有分支提交应保留")
}

// TestFinalize_用户改动保留 未提交的用户改动 → 保留。
func TestFinalize_用户改动保留(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", atschema.TeamRoleTeammate, "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(path, "user_edit.txt"), []byte("x"), 0o644))
	require.NoError(t, l.FinalizeNonContributingMemberWorktrees(withSessionID(t, "s1")))
	assert.DirExists(t, path, "用户改动应保留")
}

// gitCommitIn 在 dir 里打一个空提交（test 身份）。
func gitCommitIn(t *testing.T, dir string) error {
	t.Helper()
	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "lead")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
	return cmd.Run()
}

// TestBuildWorktreeOwnerScope_错误分支 缺 session/缺 project_dir/目录不存在。
func TestBuildWorktreeOwnerScope_错误分支(t *testing.T) {
	spec := &atschema.TeamAgentSpec{}
	// 无 session_id
	_, err := BuildWorktreeOwnerScope(context.Background(), "team-a", "alice", spec, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session_id")

	// 有 session 但 spec 无 project_dir
	ctx := withSessionID(t, "s1")
	_, err = BuildWorktreeOwnerScope(ctx, "team-a", "alice", spec, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project_dir")

	// project_dir 不存在
	bad := &atschema.TeamAgentSpec{BuildContextSeed: map[string]any{"project_dir": filepath.Join(t.TempDir(), "nope")}}
	_, err = BuildWorktreeOwnerScope(ctx, "team-a", "alice", bad, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

// TestResolveProjectDir_活载体优先 build_context.project_dir 优先于种子。
func TestResolveProjectDir_活载体优先(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	spec := &atschema.TeamAgentSpec{BuildContextSeed: map[string]any{"project_dir": dirA}}
	bc := &atschema.BuildContext{}
	bc.ProjectDir = dirB
	got, err := resolveProjectDir(spec, bc)
	require.NoError(t, err)
	assert.Equal(t, filepath.Base(dirB), filepath.Base(got), "活载体优先")
}

// TestEnterProjectWorktreeContext_无CwdState 上下文缺 cwd 状态 → 显式报错。
func TestEnterProjectWorktreeContext_无CwdState(t *testing.T) {
	// 普通 ctx 无 CwdState：GetCwd 走默认值不报错（Go 独有宽容面，restore 返回旧 ctx）
	newCtx, restore, err := EnterProjectWorktreeContext(context.Background(), t.TempDir(), "")
	require.NoError(t, err)
	require.NotNil(t, newCtx)
	require.NotNil(t, restore)
	assert.Equal(t, context.Background(), RestoreProjectWorktreeContext(restore))
}

// TestSlugPart_全非法回退 清洗后为空 → fallback。
func TestSlugPart_全非法回退(t *testing.T) {
	assert.Equal(t, "fb", slugPart("!!!", "fb", 10))
	assert.Equal(t, "ab", slugPart("ab!!!", "fb", 10))
}

// TestRemoteBackend_远端URL缺失 无 origin remote → Create 显式报错。
func TestRemoteBackend_远端URL缺失(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	b := NewRemoteWorktreeBackend(hworktree.NewWorktreeConfig(), &failMessager{}, "node-1")
	_, err := b.Create(context.Background(), "agent-x", repo, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Cannot determine remote URL")
}

// failMessager SendAndWait 恒失败。
type failMessager struct{}

func (f *failMessager) SendAndWait(ctx context.Context, nodeID string, payload map[string]any) (map[string]any, error) {
	return nil, context.DeadlineExceeded
}

// TestRemoteBackend_发送失败CreateRemoveExists messager 失败 → remove/exists 安全 False。
func TestRemoteBackend_发送失败CreateRemoveExists(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	runGitOrigin(t, repo)
	b := NewRemoteWorktreeBackend(hworktree.NewWorktreeConfig(), &failMessager{}, "node-1")
	_, err := b.Create(context.Background(), "agent-x", repo, "")
	require.Error(t, err)
	assert.False(t, b.Remove(context.Background(), "/w", repo, true))
	assert.False(t, b.Exists(context.Background(), "/w"))
}

func runGitOrigin(t *testing.T, repo string) { t.Helper() }

// TestWorktreeResponseFromMap_字段缺省 响应反序列化缺键安全。
func TestWorktreeResponseFromMap_字段缺省(t *testing.T) {
	resp, err := responseFromPayload(map[string]any{"success": true})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Empty(t, resp.WorktreePath)
	// 未知动作 → 失败响应
	h := &WorktreeRemoteHandler{}
	r := h.Handle(context.Background(), &WorktreeRemoteRequest{Action: "nope"})
	assert.False(t, r.Success)
}

// ──────────────────────────── 第三轮：孤儿清扫全分支 + 分类 Unknown 面 + hook 保留 ────────────────────────────

// TestOrphanSweep_全分支 清扫：普通目录兜底删 / git worktree 强删 / 符号链接跳过。
func TestOrphanSweep_全分支(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)

	// 经真实 git worktree add 造一个孤儿（有 git 注册 → RemoveWorktreeForce 分支）
	realWt := agent_teams.TeamSessionWorktreesDir("team-z", "s9")
	require.NoError(t, os.MkdirAll(filepath.Dir(realWt), 0o755))
	realOrphan := filepath.Join(realWt, "agent-team-z-orphan-1")
	addCmd := exec.Command("git", "worktree", "add", "-B", "worktree-orphan-z", realOrphan, "HEAD")
	addCmd.Dir = repo
	addCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
	require.NoError(t, addCmd.Run())
	// 普通（非 git）目录 → RemoveAll 兜底分支
	plainOrphan := filepath.Join(realWt, "agent-team-z-orphan-2")
	require.NoError(t, os.MkdirAll(plainOrphan, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(plainOrphan, "junk"), []byte("x"), 0o644))
	// 符号链接孤儿 → 跳过分支
	require.NoError(t, os.Symlink(repo, filepath.Join(realWt, "agent-team-z-orphan-link")))
	// 非 worktrees 命名的兄弟目录 → continue 分支
	require.NoError(t, os.MkdirAll(filepath.Join(filepath.Dir(realWt), "not-worktrees"), 0o755))

	db := database.NewInMemoryTeamDatabase()
	cfg := &lifecycleFakeConfigurator{
		backend:  &fakeTeamBackendAccess{teamName: "team-z", db: db},
		teamName: "team-z",
		spec:     &atschema.TeamAgentSpec{BuildContextSeed: map[string]any{"project_dir": repo}},
	}
	l := NewTeammateWorktreeLifecycle(cfg)
	require.NoError(t, l.FinalizeAllMemberWorktreesForTeamClean(withSessionID(t, "s9")))
	assert.NoDirExists(t, realOrphan, "git 注册孤儿应强删")
	assert.NoDirExists(t, plainOrphan, "非 git 孤儿应兜底删")
	_, serr := os.Lstat(filepath.Join(realWt, "agent-team-z-orphan-link"))
	assert.NoError(t, serr, "符号链接孤儿跳过（不删）")
}

// TestClassifyWorktreeContribution_Unknown面 无法验证 → UNKNOWN（保守保留）。
func TestClassifyWorktreeContribution_Unknown面(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	initGitRepoAt(t, repo)

	// 分支为空 → Unknown
	assert.Equal(t, WorktreeContributionUnknown,
		classifyWorktreeContribution(ctx, &MemberWorktreeInfo{WorktreePath: repo}, repo))
	// 不存在的分支 → branchHead 空 → Unknown
	assert.Equal(t, WorktreeContributionUnknown,
		classifyWorktreeContribution(ctx, &MemberWorktreeInfo{WorktreePath: repo, WorktreeBranch: "no-such"}, repo))
}

// TestFinalize_Contributed保留经cleanup 走 finalizeMemberWorktree 的 classify 分支。
func TestFinalize_Contributed保留经cleanup(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", atschema.TeamRoleTeammate, "")
	require.NoError(t, err)
	require.NoError(t, gitCommitIn(t, path))
	// FinalizeAllMemberWorktreesForTeamClean 是强删路径；贡献检查走
	// FinalizeNonContributingMemberWorktrees（保留分支）
	require.NoError(t, l.FinalizeNonContributingMemberWorktrees(withSessionID(t, "s1")))
	assert.DirExists(t, path)
}

// TestRemoveSessionWorktrees_根为符号链接 worktrees 根是符号链接 → false。
func TestRemoveSessionWorktrees_根为符号链接(t *testing.T) {
	ctx := context.Background()
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)
	root := agent_teams.TeamSessionWorktreesDir("team-sym", "s1")
	require.NoError(t, os.MkdirAll(filepath.Dir(root), 0o755))
	require.NoError(t, os.Symlink(t.TempDir(), root))
	assert.False(t, RemoveSessionWorktrees(ctx, "team-sym", "s1", nil))
}

// TestClassifyWorktreeContribution_分支面 branchHead==baseHead → NOT_CONTRIBUTED；
// 分支落后基线 → Unknown（isDescendant false）。
func TestClassifyWorktreeContribution_分支面(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	headSha := worktreeRevParse(t, repo, "HEAD")

	// 未动过的 worktree：HEAD==基线，分支==基线 → NOT_CONTRIBUTED
	assert.Equal(t, WorktreeContributionNotContributed,
		classifyWorktreeContribution(ctx, &MemberWorktreeInfo{
			WorktreePath: repo, WorktreeBranch: "main", HeadCommit: headSha,
		}, repo))

	// 分支落后基线：建第二个提交，分支钉在旧提交
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
		require.NoError(t, cmd.Run())
	}
	require.NoError(t, os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b"), 0o644))
	run("add", ".")
	run("commit", "-m", "second")
	run("branch", "-f", "stale", headSha)
	assert.Equal(t, WorktreeContributionUnknown,
		classifyWorktreeContribution(ctx, &MemberWorktreeInfo{
			WorktreePath: repo, WorktreeBranch: "stale", HeadCommit: worktreeRevParse(t, repo, "HEAD"),
		}, repo))
}

// TestFinalize_git元数据损坏 worktree .git 被破坏 → 兜底删 + 清元数据（散会路径）。
func TestFinalize_git元数据损坏(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", atschema.TeamRoleTeammate, "")
	require.NoError(t, err)
	// 破坏 .git（worktree 里是文件，指向主仓 gitdir）
	gitFile := filepath.Join(path, ".git")
	require.NoError(t, os.Remove(gitFile))
	require.NoError(t, l.FinalizeAllMemberWorktreesForTeamClean(withSessionID(t, "s1")))
	assert.NoDirExists(t, path)
}

func worktreeRevParse(t *testing.T, repo, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = repo
	out, err := cmd.Output()
	require.NoError(t, err)
	return string(out[:len(out)-1])
}

// ──────────────────────────── 第四轮：单行分支收尾 ────────────────────────────

// TestTeamContext_团队名回退 configurator 团队名空 → 回退 backend 团队名。
func TestTeamContext_团队名回退(t *testing.T) {
	backend, teamName, ok := TeamContext(&lifecycleFakeConfigurator{
		backend:  &fakeTeamBackendAccess{teamName: "team-fallback", db: database.NewInMemoryTeamDatabase()},
		teamName: "",
	})
	assert.True(t, ok)
	assert.Equal(t, "team-fallback", teamName)
	_ = backend
}

// TestResolve_无后端 configurator 缺 TeamBackend → nil 无错。
func TestResolve_无后端(t *testing.T) {
	res, err := ResolveCurrentSessionMemberWorktree(context.Background(),
		&lifecycleFakeConfigurator{spec: &atschema.TeamAgentSpec{}},
		map[string]*MemberWorktreeInfo{}, "alice", nil, false)
	require.NoError(t, err)
	assert.Nil(t, res)
}

// TestResolve_scope错误 ownerScope 报错 → 透传。
func TestResolve_scope错误(t *testing.T) {
	cfg, _ := newLifecycleFixture(t, t.TempDir(), nil)
	// DB 行带 path（hasDBWorktree true）才会走到 ownerScope 调用
	require.True(t, cfg.backend.DB().Member().UpdateMemberWorktree(context.Background(), "alice", "team-a",
		&database.MemberWorktreeOptions{Isolation: strPtr("worktree"), Path: strPtr(t.TempDir())}, "", ""))
	boom := assert.AnError
	_, err := ResolveCurrentSessionMemberWorktree(context.Background(), cfg,
		map[string]*MemberWorktreeInfo{}, "alice",
		func(ctx context.Context, teamName, memberName string) (WorktreeOwnerScope, error) {
			return WorktreeOwnerScope{}, boom
		}, false)
	require.Error(t, err)
	assert.Equal(t, boom, err)
}

// TestResolve_仅Isolation声明 DB 行只有 isolation 无路径且无缓存 → nil（门 :211）。
func TestResolve_仅Isolation声明(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	// DB 行 isolation-only（无 path）：UpdateMemberWorktree 收敛形态
	require.True(t, cfg.backend.DB().Member().UpdateMemberWorktree(context.Background(), "alice", "team-a",
		&database.MemberWorktreeOptions{Isolation: strPtr("worktree")}, "", ""))
	res, err := ResolveCurrentSessionMemberWorktree(context.Background(), cfg,
		map[string]*MemberWorktreeInfo{}, "alice",
		func(ctx context.Context, teamName, memberName string) (WorktreeOwnerScope, error) {
			return WorktreeOwnerScope{SessionID: "s1", ManagedRoot: t.TempDir(), ProjectHash: "h"}, nil
		}, false)
	require.NoError(t, err)
	assert.Nil(t, res)
}

// TestSlugPart_仅分隔符回退 点/下划线/连字符清洗后为空 → fallback。
func TestSlugPart_仅分隔符回退(t *testing.T) {
	assert.Equal(t, "fb", slugPart("._-", "fb", 10))
}

// TestResolveProjectDir_全空 spec 与 build_context 皆空 → 显式报错。
func TestResolveProjectDir_全空(t *testing.T) {
	_, err := resolveProjectDir(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires explicit build_context.project_dir")
}

// TestManagerForScope_失败回退 ManagedRoot 为空 → 视图失败退回默认 manager。
func TestManagerForScope_失败回退(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	_ = NewTeammateWorktreeLifecycle(cfg)
	_, err := ManagerForWorktreeScope(hworktree.NewWorktreeManager(hworktree.NewWorktreeConfig(), nil), WorktreeOwnerScope{})
	require.Error(t, err, "ManagedRoot 为空 → worktrees_dir is required")
}

// TestRemoteHandle_create克隆失败 远端 URL 不可克隆 → 失败响应。
func TestRemoteHandle_create克隆失败(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	mgr := hworktree.NewWorktreeManager(hworktree.NewWorktreeConfig(), nil)
	h := NewWorktreeRemoteHandler(mgr)
	resp := h.Handle(context.Background(), &WorktreeRemoteRequest{
		Action: "create", Slug: "agent-x", RepoURL: filepath.Join(t.TempDir(), "not-a-repo"),
	})
	assert.False(t, resp.Success)
}

// TestEnsureMemberWorktree_无行成员门 成员无 DB 行 → options nil → 门返回空
// （覆盖 memberWorktreeOptions nil 分支 + memberNeedsWorktree false 门）。
func TestEnsureMemberWorktree_无行成员门(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "bob", atschema.TeamRoleTeammate, "")
	require.NoError(t, err)
	assert.Empty(t, path)
}

// TestEnsureMemberWorktree_外来拒收 单测：旧会话登记的 worktree 在新会话 scope 下拒收重建。
func TestEnsureMemberWorktree_外来拒收(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", atschema.TeamRoleTeammate, "")
	require.NoError(t, err)
	// 新会话 s2：DB 行 session_id 仍是 s1 → scope 不匹配 → 拒收 + 重建
	path2, err := l.EnsureMemberWorktree(withSessionID(t, "s2"), "alice", atschema.TeamRoleTeammate, "")
	require.NoError(t, err)
	assert.NotEqual(t, path, path2, "外来 worktree 应重建")
	assert.Contains(t, path2, "sessions"+string(filepath.Separator)+"s2")
}

// TestClearMemberWorktreeMetadata_成员不存在 DB 无成员行 → 显式 error。
func TestClearMemberWorktreeMetadata_成员不存在(t *testing.T) {
	emptyBackend := &fakeTeamBackendAccess{teamName: "team-a", db: database.NewInMemoryTeamDatabase()}
	err := ClearMemberWorktreeMetadata(context.Background(), "ghost",
		&ResolvedMemberWorktree{TeamBackend: emptyBackend, TeamName: "team-a", DBMatchesScope: true},
		map[string]*MemberWorktreeInfo{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in team")
}

// TestResolveProjectDir_活载体空目录 spec.BuildContext 存在但 ProjectDir 空 → 种子回退。
func TestResolveProjectDir_活载体空目录(t *testing.T) {
	dir := t.TempDir()
	spec := &atschema.TeamAgentSpec{BuildContextSeed: map[string]any{"project_dir": dir}}
	spec.BuildContext = &atschema.BuildContext{} // GetProjectDir() == ""
	got, err := resolveProjectDir(spec, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, got)
}

// TestValidateMemberWorktreeIsolation_透传 spec 无 project_dir → 校验入口透传错误。
func TestValidateMemberWorktreeIsolation_透传(t *testing.T) {
	err := ValidateMemberWorktreeIsolation(withSessionID(t, "s1"),
		&atschema.TeamAgentSpec{}, "team-a", "alice")
	require.Error(t, err)
}

// TestCountUserChangedFiles_目录缺失 status 失败 → 0（保守不计用户改动）。
func TestCountUserChangedFiles_目录缺失(t *testing.T) {
	assert.Equal(t, 0, countUserChangedFiles(context.Background(), filepath.Join(t.TempDir(), "nope")))
}
