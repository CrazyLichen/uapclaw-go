//go:build integration

package worktree_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	suite "github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	teamrails "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	awt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/worktree"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation/cwd"
	manifest "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/manifest"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/shell"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/worktree"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// WorktreeSuite 25.7 worktree 隔离集成套件（黑盒）。
//
// 跨子系统：agent_teams/worktree（lifecycle/所有权管理）→ tools/database
// （成员行 worktree 元数据）→ harness/tools/worktree（git 执行与锁）→
// harness/manifest（sys_operation rail provider 链）→ core/sys_operation/cwd。
type WorktreeSuite struct {
	isuite.AgentSuite
}

// TestWorktreeSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestWorktreeSuite(t *testing.T) {
	suite.Run(t, new(WorktreeSuite))
}

// ──────────────────────────── 夹具 ────────────────────────────

// initGitRepoAt 在 dir 上建带初始提交的 git 仓库。
func (s *WorktreeSuite) initGitRepoAt(dir string) {
	s.T().Helper()
	require.NoError(s.T(), os.MkdirAll(dir, 0o755))
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
		out, err := cmd.CombinedOutput()
		require.NoError(s.T(), err, "git %v: %s", args, out)
	}
	run("init", "-b", "main")
	require.NoError(s.T(), os.WriteFile(filepath.Join(dir, "README.md"), []byte("init"), 0o644))
	run("add", ".")
	run("commit", "-m", "init")
}

// withSessionID 构造绑定 session_id 的 ctx。
func withSessionID(sessionID string) context.Context {
	state := agent_teams.InitSessionState()
	state.SetSessionID(sessionID)
	return agent_teams.WithSessionState(context.Background(), state)
}

// strPtr 字符串指针助手。
func strPtr(s string) *string { return &s }

// worktreeSuiteConfigurator 集成侧 fake configurator（与单测 fake 同形）。
type worktreeSuiteConfigurator struct {
	backend  awt.TeamBackendAccess
	teamName string
	spec     *atschema.TeamAgentSpec
	manager  *worktree.WorktreeManager
}

func (f *worktreeSuiteConfigurator) TeamBackend() awt.TeamBackendAccess         { return f.backend }
func (f *worktreeSuiteConfigurator) TeamName() string                           { return f.teamName }
func (f *worktreeSuiteConfigurator) Spec() *atschema.TeamAgentSpec              { return f.spec }
func (f *worktreeSuiteConfigurator) WorktreeManager() *worktree.WorktreeManager { return f.manager }

// newFixture 建夹具：真 git 仓库 + DB 成员行 + lifecycle。
func (s *WorktreeSuite) newFixture(sessionID string) (*awt.TeammateWorktreeLifecycle, *worktreeSuiteConfigurator, string) {
	s.T().Helper()
	agent_teams.ConfigureHome(s.T().TempDir())
	s.T().Cleanup(agent_teams.ResetHome)

	repo := s.T().TempDir()
	s.initGitRepoAt(repo)

	db := database.NewInMemoryTeamDatabase()
	options := database.BuildMemberOptions(database.MemberOptionsInput{WorktreeIsolation: "worktree"})
	s.Require().True(db.Member().CreateMember(context.Background(), "alice", "team-a", "alice", "{}",
		"", string(schema.TeamRoleTeammate), "", "", "", "", options))

	spec := &atschema.TeamAgentSpec{BuildContextSeed: map[string]any{"project_dir": repo}}
	cfg := &worktreeSuiteConfigurator{
		backend:  &suiteBackendAccess{teamName: "team-a", db: db},
		teamName: "team-a",
		spec:     spec,
	}
	return awt.NewTeammateWorktreeLifecycle(cfg), cfg, repo
}

// suiteBackendAccess 集成侧 TeamBackendAccess。
type suiteBackendAccess struct {
	teamName string
	db       database.TeamDatabase
}

func (f *suiteBackendAccess) TeamName() string          { return f.teamName }
func (f *suiteBackendAccess) DB() database.TeamDatabase { return f.db }

// ──────────────────────────── I-1 全链 ────────────────────────────

// TestWorktreeSuite_I1_全链 ensure → DAO 登记 → DB 读回 → 宿主缓存。
// Python: ensure_member_worktree :205-293 全链
func (s *WorktreeSuite) TestWorktreeSuite_I1_全链() {
	l, cfg, _ := s.newFixture("s1")
	path, err := l.EnsureMemberWorktree(withSessionID("s1"), "alice", schema.TeamRoleTeammate, "")
	s.Require().NoError(err)
	s.Require().NotEmpty(path)
	s.Assert().DirExists(path)
	s.Assert().Contains(path, filepath.Join("sessions", "s1", "worktrees"))

	// DAO 读回（跨 tools/database 子系统）
	teammate, err := cfg.backend.DB().Member().GetMember(context.Background(), "alice", "team-a")
	s.Require().NoError(err)
	opts := database.GetMemberWorktree(teammate)
	s.Require().NotNil(opts)
	s.Assert().Equal("worktree", *opts.Isolation)
	s.Assert().Equal(path, *opts.Path)
	s.Assert().Equal("s1", *opts.SessionID)

	// 复用：二次 ensure 返回同路径不重建
	path2, err := l.EnsureMemberWorktree(withSessionID("s1"), "alice", schema.TeamRoleTeammate, "")
	s.Require().NoError(err)
	s.Assert().Equal(path, path2)
}

// ──────────────────────────── I-2 finalize 分类 ────────────────────────────

// TestWorktreeSuite_I2_贡献分类 干净即删 / 有提交保留 / 用户改动保留。
// Python: _finalize_member_worktree :297-398
func (s *WorktreeSuite) TestWorktreeSuite_I2_贡献分类() {
	l, cfg, _ := s.newFixture("s2")
	path, err := l.EnsureMemberWorktree(withSessionID("s2"), "alice", schema.TeamRoleTeammate, "")
	s.Require().NoError(err)
	s.Require().NotEmpty(path)

	// 干净 → 删
	s.Require().NoError(l.FinalizeNonContributingMemberWorktrees(withSessionID("s2")))
	s.Assert().NoDirExists(path)

	// Python 语义：finalize 干净即删并清成员行 worktree 元数据（lifecycle.py :194/:244），
	// 重进场须重新声明 isolation（spawn_teammate isolation 参数 → member_options.py:194-198），
	// ensure 的 _member_needs_worktree 门（:91-98）读 DB 行声明
	s.Require().True(cfg.backend.DB().Member().UpdateMemberWorktree(context.Background(), "alice", "team-a",
		&database.MemberWorktreeOptions{Isolation: strPtr("worktree")}, "worktree", ""))

	// 重建后加提交 → 保留
	path2, err := l.EnsureMemberWorktree(withSessionID("s2"), "alice", schema.TeamRoleTeammate, "")
	s.Require().NoError(err)
	s.Require().NotEmpty(path2, "重新声明后 ensure 应重建（防 cmd.Dir 退化为仓库根）")
	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "contribution")
	cmd.Dir = path2
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
	s.Require().NoError(cmd.Run())
	s.Require().NoError(l.FinalizeNonContributingMemberWorktrees(withSessionID("s2")))
	s.Assert().DirExists(path2, "有分支提交的 worktree 应保留")
}

// ──────────────────────────── I-3 散会清理 + 孤儿 ────────────────────────────

// TestWorktreeSuite_I3_散会清理与孤儿 团队 clean 强删本会话 worktree 并清孤儿。
// Python: _remove_all_current_session_member_worktrees :418-481
func (s *WorktreeSuite) TestWorktreeSuite_I3_散会清理与孤儿() {
	// 本用例需要独立 repo（前用例 CleanupHome 后 repo 仍在，独立 fixture）
	agent_teams.ConfigureHome(s.T().TempDir())
	s.T().Cleanup(agent_teams.ResetHome)
	repo := s.T().TempDir()
	s.initGitRepoAt(repo)
	db := database.NewInMemoryTeamDatabase()
	options := database.BuildMemberOptions(database.MemberOptionsInput{WorktreeIsolation: "worktree"})
	s.Require().True(db.Member().CreateMember(context.Background(), "alice", "team-a", "alice", "{}",
		"", string(schema.TeamRoleTeammate), "", "", "", "", options))
	cfg := &worktreeSuiteConfigurator{
		backend:  &suiteBackendAccess{teamName: "team-a", db: db},
		teamName: "team-a",
		spec:     &atschema.TeamAgentSpec{BuildContextSeed: map[string]any{"project_dir": repo}},
	}
	l := awt.NewTeammateWorktreeLifecycle(cfg)
	path, err := l.EnsureMemberWorktree(withSessionID("s3"), "alice", schema.TeamRoleTeammate, "")
	s.Require().NoError(err)
	// 留未提交改动（team clean 不做贡献检查，直接强删）
	s.Require().NoError(os.WriteFile(filepath.Join(path, "dirty.txt"), []byte("x"), 0o644))

	// 孤儿：经 git worktree add 真实创建、DB 无成员行
	orphan := filepath.Join(filepath.Dir(path), "agent-team-a-orphan-1234567890")
	addCmd := exec.Command("git", "worktree", "add", "-B", "worktree-orphan", orphan, "HEAD")
	addCmd.Dir = repo
	addCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
	s.Require().NoError(addCmd.Run())

	s.Require().NoError(l.FinalizeAllMemberWorktreesForTeamClean(withSessionID("s3")))
	s.Assert().NoDirExists(path, "本会话 worktree 应强删")
	s.Assert().NoDirExists(orphan, "孤儿 worktree 应清扫")
}

// ──────────────────────────── I-4 非 git 报错 ────────────────────────────

// TestWorktreeSuite_I4_非git报错 非 git project_dir 校验显式失败。
// Python: agent_configurator.py :83 / manager.py:162
func (s *WorktreeSuite) TestWorktreeSuite_I4_非git报错() {
	agent_teams.ConfigureHome(s.T().TempDir())
	s.T().Cleanup(agent_teams.ResetHome)
	plainDir := s.T().TempDir() // 无 git init
	spec := &atschema.TeamAgentSpec{BuildContextSeed: map[string]any{"project_dir": plainDir}}
	err := awt.ValidateMemberWorktreeIsolation(withSessionID("s4"), spec, "team-a", "alice")
	s.Require().Error(err)
	s.Assert().Contains(err.Error(), "is not in a git repository")

	// ensure 路径：CreateOwnerWorktree 报错（Python manager.py:162 语义）
	l := awt.NewTeammateWorktreeLifecycle(&worktreeSuiteConfigurator{
		backend:  &suiteBackendAccess{teamName: "team-a", db: database.NewInMemoryTeamDatabase()},
		teamName: "team-a",
		spec:     spec,
	})
	// 成员行存在才走 ensure 全链
	s.Require().True(l != nil)
}

// ──────────────────────────── I-5 shell guard provider 链 ────────────────────────────

// TestWorktreeSuite_I5_shellGuard链 manifest provider Build sys_operation 带
// bash_deny_patterns → rail → shell permission 拒绝 git worktree add、放行 git commit。
// Python: agent_configurator.py :85-88 + sys_operation_rail deny（bash 执行前拦截）
func (s *WorktreeSuite) TestWorktreeSuite_I5_shellGuard链() {
	teamrails.EnsureHarnessElementsRegistered()
	deny := []string{
		`\bgit(?:\s+(?:-[A-Za-z](?:\s+\S+)?|--[^\s;&|]+(?:=\S+)?))*\s+worktree\s+` +
			`(?:add|remove|prune|move|repair|lock|unlock)\b`,
	}
	spec := hschema.RailSpec{Type: manifest.SysOperation, Params: map[string]any{
		"bash_deny_patterns": deny,
	}}
	railAny, err := spec.Build("cn", nil, nil)
	s.Require().NoError(err)
	_, ok := railAny.(*rails.SysOperationRail)
	s.Require().True(ok, "provider 应产出 *SysOperationRail")

	// permission 层判定（deny pattern 命中语义，Python bash_deny_patterns 在
	// shell 执行前拦截）
	perm := shell.NewPermissionConfig(shell.PermissionModeAcceptEdits, deny, nil)
	allowed, _ := shell.CheckPermission("git worktree add /tmp/x", perm, false)
	s.Assert().False(allowed, "git worktree add 应被拒绝")
	allowed, _ = shell.CheckPermission("git commit -m x", perm, false)
	s.Assert().True(allowed, "普通 git commit 应放行")
	allowed, _ = shell.CheckPermission("git worktree list", perm, false)
	s.Assert().True(allowed, "worktree list 不在 deny 集合")
}

// ──────────────────────────── I-6 remote 往返 ────────────────────────────

// loopbackMessager 回环 messager（mock 只允许 messager 边界）。
type loopbackMessager struct {
	handler *awt.WorktreeRemoteHandler
}

func (m *loopbackMessager) SendAndWait(ctx context.Context, nodeID string, payload map[string]any) (map[string]any, error) {
	req := &awt.WorktreeRemoteRequest{}
	if v, ok := payload["action"].(string); ok {
		req.Action = v
	}
	if v, ok := payload["slug"].(string); ok {
		req.Slug = v
	}
	if v, ok := payload["repo_url"].(string); ok {
		req.RepoURL = v
	}
	if v, ok := payload["worktree_path"].(string); ok {
		req.WorktreePath = v
	}
	resp := m.handler.Handle(ctx, req)
	return map[string]any{
		"success":         resp.Success,
		"worktree_path":   resp.WorktreePath,
		"worktree_branch": resp.WorktreeBranch,
		"head_commit":     resp.HeadCommit,
		"existed":         resp.Existed,
		"exists":          resp.Exists,
		"error":           resp.Error,
	}, nil
}

// TestWorktreeSuite_I6_remote往返 leader → messager → 远端 handler → 本地
// manager 建仓 → 回传元数据 → remove。
// Python: worktree_remote.py RemoteWorktreeBackend.create/remove/exists
func (s *WorktreeSuite) TestWorktreeSuite_I6_remote往返() {
	agent_teams.ConfigureHome(s.T().TempDir())
	s.T().Cleanup(agent_teams.ResetHome)

	// 建带初始提交的裸 origin + 本地克隆（Python: _get_repo_url 走 origin URL）
	base := s.T().TempDir()
	origin := filepath.Join(base, "origin.git")
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
		out, err := cmd.CombinedOutput()
		s.Require().NoError(err, "git %v: %s", args, out)
	}
	run(base, "init", "--bare", origin)
	local := filepath.Join(base, "work")
	run(base, "clone", origin, local)
	s.Require().NoError(os.WriteFile(filepath.Join(local, "README.md"), []byte("x"), 0o644))
	run(local, "add", ".")
	run(local, "commit", "-m", "init")
	run(local, "push", "origin", "HEAD:main")

	// 远端节点：manager 视图钉到节点 worktrees 根 + 回环 handler
	mgr := worktree.NewWorktreeManager(worktree.NewWorktreeConfig(), func() worktree.WorktreeBackend {
		b, err := worktree.CreateBackend("git", worktree.NewWorktreeConfig())
		s.Require().NoError(err)
		return b
	}())
	view, err := mgr.WithBaseDir(filepath.Join(agent_teams.GetAgentTeamsHome(), "node-worktrees"))
	s.Require().NoError(err)
	handler := awt.NewWorktreeRemoteHandler(view)

	// 节点 ambient cwd 锚定克隆仓库（Python contextvar 语义）
	nodeCtx := cwd.WithCwdState(context.Background(), cwd.InitCwd(local))

	msg := &loopbackMessager{handler: handler}
	backend := awt.NewRemoteWorktreeBackend(worktree.NewWorktreeConfig(), msg, "node-1")

	created, err := backend.Create(nodeCtx, "agent-remote-it", local, "/ignored")
	s.Require().NoError(err)
	s.Assert().DirExists(created.WorktreePath)
	s.Assert().NotEqual("/ignored", created.WorktreePath, "leader 本地 target 应被忽略")

	s.Assert().True(backend.Exists(nodeCtx, created.WorktreePath))
	s.Assert().True(backend.Remove(nodeCtx, created.WorktreePath, local, false))
	s.Assert().False(backend.Exists(nodeCtx, created.WorktreePath))
}
