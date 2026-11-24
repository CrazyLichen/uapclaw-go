package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/worktree"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// lifecycleFakeConfigurator lifecycle 用的完整 fake configurator。
type lifecycleFakeConfigurator struct {
	backend  TeamBackendAccess
	teamName string
	spec     *atschema.TeamAgentSpec
	manager  *worktree.WorktreeManager
}

func (f *lifecycleFakeConfigurator) TeamBackend() TeamBackendAccess             { return f.backend }
func (f *lifecycleFakeConfigurator) TeamName() string                           { return f.teamName }
func (f *lifecycleFakeConfigurator) Spec() *atschema.TeamAgentSpec              { return f.spec }
func (f *lifecycleFakeConfigurator) WorktreeManager() *worktree.WorktreeManager { return f.manager }

// initGitRepoAt 在 dir 上建带初始提交的 git 仓库。
func initGitRepoAt(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("init"), 0o644))
	run("add", ".")
	run("commit", "-m", "init")
}

// newLifecycleFixture 建 fake configurator + 成员行 + git 项目目录。
func newLifecycleFixture(t *testing.T, projectDir string, seedOverrides map[string]any) (*lifecycleFakeConfigurator, string) {
	t.Helper()
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)

	db := database.NewInMemoryTeamDatabase()
	// Python 语义：spawn_teammate isolation 参数先写成员行 options（member_options.py:194-198），
	// ensure 的 _member_needs_worktree 门读取该声明
	options := database.BuildMemberOptions(database.MemberOptionsInput{WorktreeIsolation: "worktree"})
	require.True(t, db.Member().CreateMember(context.Background(), "alice", "team-a", "alice", "{}",
		"", string(schema.TeamRoleTeammate), "", "", "", "", options))

	seed := map[string]any{"project_dir": projectDir}
	for k, v := range seedOverrides {
		seed[k] = v
	}
	spec := &atschema.TeamAgentSpec{BuildContextSeed: seed}
	cfg := &lifecycleFakeConfigurator{
		backend:  &fakeTeamBackendAccess{teamName: "team-a", db: db},
		teamName: "team-a",
		spec:     spec,
	}
	return cfg, projectDir
}

// ──────────────────────────── 三重门 ────────────────────────────

// TestEnsureMemberWorktree_非协调角色返回空 human_agent 等角色不进 worktree。
// Python: _member_needs_worktree :91-98
func TestEnsureMemberWorktree_非协调角色返回空(t *testing.T) {
	cfg, _ := newLifecycleFixture(t, t.TempDir(), nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", schema.TeamRoleHumanAgent, "")
	require.NoError(t, err)
	assert.Empty(t, path)
}

// TestEnsureMemberWorktree_会话禁用 种子声明 disable_teammate_worktree 时跳过。
// Python: _session_disables_teammate_worktree :81-89
func TestEnsureMemberWorktree_会话禁用(t *testing.T) {
	cfg, _ := newLifecycleFixture(t, t.TempDir(), map[string]any{"disable_teammate_worktree": true})
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", schema.TeamRoleTeammate, "")
	require.NoError(t, err)
	assert.Empty(t, path)
}

// TestEnsureMemberWorktree_状态门 shutdown 成员不建 worktree。
// Python: ensure_member_worktree :211-213（调用方门）
func TestEnsureMemberWorktree_状态门(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", schema.TeamRoleTeammate, "shutdown")
	require.NoError(t, err)
	assert.Empty(t, path)
}

// ──────────────────────────── 新建 / 复用 ────────────────────────────

// TestEnsureMemberWorktree_新建全链 创建 worktree + DB 登记 + 落 session worktrees 目录。
// Python: ensure_member_worktree :220-293
func TestEnsureMemberWorktree_新建全链(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)

	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", schema.TeamRoleTeammate, "")
	require.NoError(t, err)
	require.NotEmpty(t, path)
	assert.DirExists(t, path)
	assert.Contains(t, path, filepath.Join("sessions", "s1", "worktrees"))
	assert.Contains(t, filepath.Base(path), "agent-team-a-alice-")

	// DB 行已登记 worktree 元数据
	teammate, err := cfg.backend.DB().Member().GetMember(context.Background(), "alice", "team-a")
	require.NoError(t, err)
	opts := database.GetMemberWorktree(teammate)
	require.NotNil(t, opts)
	assert.Equal(t, "worktree", strDeref(opts.Isolation))
	assert.Equal(t, path, strDeref(opts.Path))

	// 宿主缓存入账
	assert.NotNil(t, l.MemberWorktreeInfoOf("alice"))
}

// TestEnsureMemberWorktree_复用 二次 ensure 复用同一 worktree 不重建。
// Python: :228-237
func TestEnsureMemberWorktree_复用(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)

	path1, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", schema.TeamRoleTeammate, "")
	require.NoError(t, err)
	path2, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", schema.TeamRoleTeammate, "")
	require.NoError(t, err)
	assert.Equal(t, path1, path2)
	assert.DirExists(t, path1)
}

// ──────────────────────────── finalize 分类 ────────────────────────────

// TestFinalizeMemberWorktree_干净即删 无变更的 worktree 收编时删除。
// Python: _finalize_member_worktree :321-398
func TestFinalizeMemberWorktree_干净即删(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", schema.TeamRoleTeammate, "")
	require.NoError(t, err)

	require.NoError(t, l.FinalizeNonContributingMemberWorktrees(withSessionID(t, "s1")))
	assert.NoDirExists(t, path, "干净 worktree 应被删除")
	// DB 收敛为 isolation-only
	teammate, err := cfg.backend.DB().Member().GetMember(context.Background(), "alice", "team-a")
	require.NoError(t, err)
	opts := database.GetMemberWorktree(teammate)
	require.NotNil(t, opts)
	assert.Nil(t, opts.Path)
}

// TestFinalizeMemberWorktree_有提交保留 分支有新提交时保留给 leader 收编。
// Python: :378-386 CONTRIBUTED 分支
func TestFinalizeMemberWorktree_有提交保留(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", schema.TeamRoleTeammate, "")
	require.NoError(t, err)

	// 在 worktree 里做一次提交
	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "contribution")
	cmd.Dir = path
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
	require.NoError(t, cmd.Run())

	require.NoError(t, l.FinalizeNonContributingMemberWorktrees(withSessionID(t, "s1")))
	assert.DirExists(t, path, "有提交的 worktree 应保留")
}

// TestFinalizeMemberWorktree_用户改动保留 团队运行时文件外的用户改动保留 worktree。
// Python: :350-363 changed_files > 0 分支
func TestFinalizeMemberWorktree_用户改动保留(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", schema.TeamRoleTeammate, "")
	require.NoError(t, err)

	// 用户改一个非运行时文件（未提交）
	require.NoError(t, os.WriteFile(filepath.Join(path, "user_edit.txt"), []byte("x"), 0o644))

	require.NoError(t, l.FinalizeNonContributingMemberWorktrees(withSessionID(t, "s1")))
	assert.DirExists(t, path, "有用户改动的 worktree 应保留")
}

// TestFinalizeMemberWorktree_仅运行时产物即删 只有团队运行时产物（未被 .gitignore
// 覆盖时算 porcelain 行，但属运行时集合）视为干净删除。
// Python: _is_team_runtime_status :132-140
func TestFinalizeMemberWorktree_仅运行时产物即删(t *testing.T) {
	assert.True(t, isTeamRuntimeStatus("?? .team/"))
	assert.True(t, isTeamRuntimeStatus("?? memory/"))
	assert.True(t, isTeamRuntimeStatus("?? AGENT.md"))
	assert.False(t, isTeamRuntimeStatus("?? user_edit.txt"))
	assert.False(t, isTeamRuntimeStatus(""))
}

// ──────────────────────────── 散会清理 ────────────────────────────

// TestRemoveAllCurrentSessionMemberWorktrees_强制删除与孤儿清扫 团队 clean 路径：
// 本会话 worktree 强删 + 无成员行的孤儿目录清掉。
// Python: _remove_all_current_session_member_worktrees :418-425 + 孤儿清扫 :427-481
func TestRemoveAllCurrentSessionMemberWorktrees_强制删除与孤儿清扫(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)
	cfg, _ := newLifecycleFixture(t, repo, nil)
	l := NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID(t, "s1"), "alice", schema.TeamRoleTeammate, "")
	require.NoError(t, err)
	// 在 worktree 里留未提交改动（team clean 不做贡献检查，直接强删）
	require.NoError(t, os.WriteFile(filepath.Join(path, "dirty.txt"), []byte("x"), 0o644))

	// 造一个孤儿：经 git worktree add 真实创建、但 DB 里没有成员行的"野生" worktree
	// （独立仓库对 git worktree remove 本就失败，Python 同语义，故必须真 worktree）
	orphan := filepath.Join(filepath.Dir(path), "agent-team-a-orphan-1234567890")
	addCmd := exec.Command("git", "worktree", "add", "-B", "worktree-orphan", orphan, "HEAD")
	addCmd.Dir = repo
	addCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
	require.NoError(t, addCmd.Run())

	require.NoError(t, l.FinalizeAllMemberWorktreesForTeamClean(withSessionID(t, "s1")))
	assert.NoDirExists(t, path, "本会话 worktree 应强删")
	assert.NoDirExists(t, orphan, "孤儿 worktree 应清扫")
}

// ──────────────────────────── session_cleanup ────────────────────────────

// TestRemoveSessionWorktrees_全清 会话 worktrees 目录整体清除。
// Python: remove_session_worktrees :24-111
func TestRemoveSessionWorktrees_全清(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)

	repo := t.TempDir()
	initGitRepoAt(t, repo)
	wtRoot := agent_teams.TeamSessionWorktreesDir("team", "s1")
	require.NoError(t, os.MkdirAll(filepath.Join(wtRoot, "wt-one"), 0o755))

	ok := RemoveSessionWorktrees(context.Background(), "team", "s1", nil)
	assert.True(t, ok)
	assert.NoDirExists(t, wtRoot, "worktrees 根目录应删除")
}

// TestRemoveSessionWorktrees_空参数直通 空团队名/会话直接成功。
// Python: :26-28
func TestRemoveSessionWorktrees_空参数直通(t *testing.T) {
	assert.True(t, RemoveSessionWorktrees(context.Background(), "", "", nil))
}

// TestRemoveSessionWorktrees_目录不存在直通 不存在返回 True。
// Python: :31-33
func TestRemoveSessionWorktrees_目录不存在直通(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)
	assert.True(t, RemoveSessionWorktrees(context.Background(), "team", "no-such-session", nil))
}

// TestRemoveSessionWorktrees_越界防护 symlink 逃逸的 worktrees 根保留不删。
// Python: :34-46 + :48-60
func TestRemoveSessionWorktrees_越界防护(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)

	// 建 worktrees 根为指向外部的符号链接
	external := t.TempDir()
	wtRoot := agent_teams.TeamSessionWorktreesDir("team", "s1")
	require.NoError(t, os.MkdirAll(filepath.Dir(wtRoot), 0o755))
	require.NoError(t, os.Symlink(external, wtRoot))

	ok := RemoveSessionWorktrees(context.Background(), "team", "s1", nil)
	assert.False(t, ok, "symlink worktrees 根应拒绝并返回 False")
	assert.DirExists(t, external, "外部目录不应被触碰")
}

// TestStatusPath_重命名行 porcelain 重命名行取 " -> " 后段。
// Python: _status_path :124-130
func TestStatusPath_重命名行(t *testing.T) {
	assert.Equal(t, "new.txt", statusPath(`R  old.txt -> new.txt`))
	assert.Equal(t, "plain.txt", statusPath("?? plain.txt"))
	assert.Equal(t, "dir", statusPath("?? dir/"))
}

// TestClassifyWorktreeContribution_无HeadCommit回退mergeBase 无 head_commit 时
// 走 merge_base 回退分支：分支领先 HEAD → CONTRIBUTED；等于 → NOT_CONTRIBUTED。
// Python: _classify_worktree_contribution :184-203
func TestClassifyWorktreeContribution_无HeadCommit回退mergeBase(t *testing.T) {
	repo := t.TempDir()
	initGitRepoAt(t, repo)

	// 分支领先 HEAD：建 worktree 并提交
	wtPath := filepath.Join(t.TempDir(), "wt")
	addCmd := exec.Command("git", "worktree", "add", "-B", "worktree-lead", wtPath, "HEAD")
	addCmd.Dir = repo
	addCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
	require.NoError(t, addCmd.Run())
	commitCmd := exec.Command("git", "commit", "--allow-empty", "-m", "lead")
	commitCmd.Dir = wtPath
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
	require.NoError(t, commitCmd.Run())

	state := classifyWorktreeContribution(context.Background(), &MemberWorktreeInfo{
		WorktreePath:   wtPath,
		WorktreeBranch: "worktree-lead",
		// HeadCommit 为空 → merge_base 回退分支
	}, repo)
	assert.Equal(t, WorktreeContributionContributed, state)

	// 分支等于 HEAD（基线分支）：NOT_CONTRIBUTED
	state2 := classifyWorktreeContribution(context.Background(), &MemberWorktreeInfo{
		WorktreePath:   repo,
		WorktreeBranch: "main",
	}, repo)
	assert.Equal(t, WorktreeContributionNotContributed, state2)
}

// TestMemberNeedsWorktree_门禁 非协调角色或未声明 isolation 均返回 false。
// Python: _member_needs_worktree :91-98
func TestMemberNeedsWorktree_门禁(t *testing.T) {
	assert.False(t, memberNeedsWorktree(schema.TeamRoleHumanAgent, &database.MemberWorktreeOptions{Isolation: strPtr("worktree")}))
	assert.False(t, memberNeedsWorktree(schema.TeamRoleTeammate, nil))
	assert.False(t, memberNeedsWorktree(schema.TeamRoleTeammate, &database.MemberWorktreeOptions{Isolation: strPtr("none")}))
	assert.True(t, memberNeedsWorktree(schema.TeamRoleTeammate, &database.MemberWorktreeOptions{Isolation: strPtr("worktree")}))
}
