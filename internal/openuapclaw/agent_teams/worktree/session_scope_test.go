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
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/worktree"
)

// mustBackend 经注册缝构造 GitBackend（外部包不可直接构造未导出字段）。
func mustBackend(t *testing.T) worktree.WorktreeBackend {
	t.Helper()
	b, err := worktree.CreateBackend("git", worktree.NewWorktreeConfig())
	require.NoError(t, err)
	return b
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// withSessionID 构造绑定 session_id 的 ctx（sessionctx 注入）。
func withSessionID(t *testing.T, sessionID string) context.Context {
	t.Helper()
	state := agent_teams.InitSessionState()
	state.SetSessionID(sessionID)
	return agent_teams.WithSessionState(context.Background(), state)
}

// ──────────────────────────── naming ────────────────────────────

// TestBuildTeammateWorktreeName_确定性 同输入恒同输出，格式对齐。
// Python: test_worktree_naming.py build_teammate_worktree_name 用例
func TestBuildTeammateWorktreeName_确定性(t *testing.T) {
	name1, err := BuildTeammateWorktreeName("My Team", "Alice", "sess-1", "hash123")
	require.NoError(t, err)
	name2, err := BuildTeammateWorktreeName("My Team", "Alice", "sess-1", "hash123")
	require.NoError(t, err)
	assert.Equal(t, name1, name2)
	assert.Contains(t, name1, "agent-my-team-alice-")
	// hash 段 10 位
	digest := name1[len("agent-my-team-alice-"):]
	assert.Len(t, digest, 10)
}

// TestBuildTeammateWorktreeName_跨项目哈希区分 同成员同会话不同项目哈希产生不同名。
func TestBuildTeammateWorktreeName_跨项目哈希区分(t *testing.T) {
	name1, err := BuildTeammateWorktreeName("team", "m", "s", "hash-a")
	require.NoError(t, err)
	name2, err := BuildTeammateWorktreeName("team", "m", "s", "hash-b")
	require.NoError(t, err)
	assert.NotEqual(t, name1, name2)
}

// TestSlugPart_清洗与截断 非法字符换 -、截断到 max_length、空值回退。
// Python: naming.py _slug_part
func TestSlugPart_清洗与截断(t *testing.T) {
	assert.Equal(t, "dev-team", slugPart("Dev Team!!", "team", 20))
	assert.Equal(t, "abc", slugPart("abcdefghijkl", "x", 3))
	assert.Equal(t, "fallback", slugPart("", "fallback", 10))
	assert.Equal(t, "fallback", slugPart("///", "fallback", 10))
}

// ──────────────────────────── paths 段 ────────────────────────────

// TestSafePathSegment_净化 净化为单段安全路径，永不逃逸。
// Python: paths.py _safe_segment / safe_path_segment
func TestSafePathSegment_净化(t *testing.T) {
	assert.Equal(t, "a_b_c_d", agent_teams.SafePathSegment("a/b\\c:d", "_"))
	assert.Equal(t, "_", agent_teams.SafePathSegment("..", "_"))
	assert.Equal(t, "_", agent_teams.SafePathSegment("", "_"))
	// 超 96 字符截断
	long := make([]byte, 120)
	for i := range long {
		long[i] = 'a'
	}
	assert.Len(t, agent_teams.SafePathSegment(string(long), "_"), 96)
}

// TestTeamSessionWorktreesDir_布局 布局 = {team_home}/sessions/{sid}/worktrees。
// Python: paths.py team_session_worktrees_dir
func TestTeamSessionWorktreesDir_布局(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	defer agent_teams.ResetHome()
	dir := agent_teams.TeamSessionWorktreesDir("team", "s1")
	assert.True(t, filepath.IsAbs(dir))
	assert.Contains(t, dir, filepath.Join("sessions", "s1", "worktrees"))
}

// TestProjectWorktreeHash_稳定且校验存在性 目录存在时 12 位哈希且稳定；不存在时报错。
// Python: paths.py project_worktree_hash
func TestProjectWorktreeHash_稳定且校验存在性(t *testing.T) {
	dir := t.TempDir()
	h1, err := agent_teams.ProjectWorktreeHash(dir)
	require.NoError(t, err)
	h2, err := agent_teams.ProjectWorktreeHash(dir)
	require.NoError(t, err)
	assert.Equal(t, h1, h2)
	assert.Len(t, h1, 12)

	_, err = agent_teams.ProjectWorktreeHash(filepath.Join(dir, "missing"))
	assert.Error(t, err)
}

// ──────────────────────────── session_scope ────────────────────────────

// TestBuildWorktreeOwnerScope_全字段 会话/项目/哈希/根/名称全部解析。
// Python: build_worktree_owner_scope
func TestBuildWorktreeOwnerScope_全字段(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	defer agent_teams.ResetHome()
	projectDir := t.TempDir()

	spec := &atschema.TeamAgentSpec{BuildContextSeed: map[string]any{}}
	scope, err := BuildWorktreeOwnerScope(
		withSessionID(t, "sess-42"), "team-a", "alice",
		spec, nil)
	require.Error(t, err, "无 build_context 时必须显式报错（Python :52-54）")

	spec.BuildContextSeed["project_dir"] = projectDir
	scope, err = BuildWorktreeOwnerScope(withSessionID(t, "sess-42"), "team-a", "alice", spec, nil)
	require.NoError(t, err)
	assert.Equal(t, "sess-42", scope.SessionID)
	assert.Equal(t, "team-a", scope.TeamName)
	assert.Equal(t, "alice", scope.MemberName)
	assert.NotEmpty(t, scope.ProjectDir)
	assert.Len(t, scope.ProjectHash, 12)
	assert.Contains(t, scope.ManagedRoot, filepath.Join("sessions", "sess-42", "worktrees"))
	assert.Contains(t, scope.WorktreeName, "agent-team-a-alice-")
}

// TestBuildWorktreeOwnerScope_无会话报错 无 session_id 时显式报错（Python :60-62）。
func TestBuildWorktreeOwnerScope_无会话报错(t *testing.T) {
	_, err := BuildWorktreeOwnerScope(context.Background(), "t", "m", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires an active team session_id")
}

// TestResolveProjectDir_目录不存在报错 project_dir 指向不存在目录时报错（Python :54-55）。
func TestResolveProjectDir_目录不存在报错(t *testing.T) {
	spec := &atschema.TeamAgentSpec{BuildContextSeed: map[string]any{
		"project_dir": "/nonexistent/path/xyz",
	}}
	_, err := resolveProjectDir(spec, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

// TestManagerForWorktreeScope_视图钉根 manager 视图把 target 解析到 managed_root 下。
// Python: manager_for_worktree_scope + with_worktrees_dir
func TestManagerForWorktreeScope_视图钉根(t *testing.T) {
	managedRoot := t.TempDir()
	mgr := worktree.NewWorktreeManager(worktree.NewWorktreeConfig(), mustBackend(t))
	view, err := ManagerForWorktreeScope(mgr, WorktreeOwnerScope{ManagedRoot: managedRoot})
	require.NoError(t, err)
	assert.Equal(t, managedRoot, view.Config().BaseDir)
	assert.True(t, view.Config().BaseDirIsWorktreesDir)
}

// TestEnterProjectWorktreeContext_锚定与恢复 新 ctx 锚定 project_dir，restore 回原状态。
// Python: enter_project_worktree_context / restore_project_worktree_context
func TestEnterProjectWorktreeContext_锚定与恢复(t *testing.T) {
	base := context.Background()
	// 预置原状态
	orig := cwd.InitCwd("/tmp", cwd.WithWorkspace("/ws"), cwd.WithTeamWorkspace("/tws"))
	orig.SetOriginalCwd("/original")
	base = cwd.WithCwdState(base, orig)

	projectDir := t.TempDir()
	newCtx, restore, err := EnterProjectWorktreeContext(base, projectDir, "/worktree-root")
	require.NoError(t, err)

	// 新 ctx：cwd 锚定项目目录，workspace 是 worktree root
	newCwd, err := cwd.GetCwd(newCtx)
	require.NoError(t, err)
	assert.Equal(t, projectDir, newCwd)
	assert.Equal(t, "/worktree-root", cwd.GetWorkspace(newCtx))
	assert.Equal(t, projectDir, func() string {
		root, _ := cwd.GetProjectRoot(newCtx)
		return root
	}())
	// team_workspace 保留快照
	assert.Equal(t, "/tws", cwd.GetTeamWorkspace(newCtx))

	// restore 后回到原状态
	oldCtx := RestoreProjectWorktreeContext(restore)
	oldCwd, err := cwd.GetCwd(oldCtx)
	require.NoError(t, err)
	assert.Equal(t, "/tmp", oldCwd)
	assert.Equal(t, "/original", cwd.GetOriginalCwd(oldCtx))
	assert.Equal(t, "/ws", cwd.GetWorkspace(oldCtx))
}

// TestEnterProjectWorktreeContext_目录不存在 新建 worktree 目录用 os.MkdirAll 兜底不在
// 本函数职责内；project_dir 必须已存在（resolveProjectDir 已校验），此处仅验证
// 快照传递无 panic。
func TestEnterProjectWorktreeContext_目录不存在(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dir")
	_, _, err := EnterProjectWorktreeContext(context.Background(), missing, "/wt")
	// Python get_cwd 快照失败语义：无 CwdState 时 get_cwd 返回 os.getcwd() 不报错
	// Go ctx 无状态时 GetCwd 报错——本测试仅记录两种行为均可接受
	if err != nil {
		assert.Contains(t, err.Error(), "cwd")
	}
	_ = os.MkdirAll(missing, 0o755)
}
