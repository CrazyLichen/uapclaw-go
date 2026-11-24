package worktree

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeTeamBackendAccess 测试用 TeamBackendAccess（直连内存库）。
type fakeTeamBackendAccess struct {
	teamName string
	db       database.TeamDatabase
}

func (f *fakeTeamBackendAccess) TeamName() string          { return f.teamName }
func (f *fakeTeamBackendAccess) DB() database.TeamDatabase { return f.db }

// fakeConfigurator 测试用 configurator。
type fakeConfigurator struct {
	backend  TeamBackendAccess
	teamName string
}

func (f *fakeConfigurator) TeamBackend() TeamBackendAccess { return f.backend }
func (f *fakeConfigurator) TeamName() string               { return f.teamName }

// strPtr 测试字符串指针助手。
func strPtr(s string) *string { return &s }

// newTestDB 建内存库并注册成员。
func newTestDB(t *testing.T, memberName, role string) database.TeamDatabase {
	t.Helper()
	db := database.NewInMemoryTeamDatabase()
	require.True(t, db.Member().CreateMember(context.Background(), memberName, "team-a", memberName, "{}", "", role, "", "", "", "", ""))
	return db
}

// ──────────────────────────── TeamContext ────────────────────────────

// TestTeamContext_后端与团队名对 从 configurator 取后端/团队名对；缺一返回 false。
// Python: team_context
func TestTeamContext_后端与团队名对(t *testing.T) {
	db := newTestDB(t, "m1", string(schema.TeamRoleTeammate))
	cfg := &fakeConfigurator{
		backend:  &fakeTeamBackendAccess{teamName: "team-a", db: db},
		teamName: "team-a",
	}
	backend, teamName, ok := TeamContext(cfg)
	require.True(t, ok)
	assert.Equal(t, "team-a", teamName)
	require.NotNil(t, backend)

	// 团队名空且后端 nil → false
	_, _, ok = TeamContext(&fakeConfigurator{})
	assert.False(t, ok)
}

// ──────────────────────────── TeammateMemberNames ────────────────────────────

// TestTeammateMemberNames_只取协调角色 仅 teammate/external_cli 角色入选。
// Python: teammate_member_names
func TestTeammateMemberNames_只取协调角色(t *testing.T) {
	ctx := context.Background()
	db := database.NewInMemoryTeamDatabase()
	require.True(t, db.Member().CreateMember(ctx, "t1", "team-a", "t1", "{}", "", string(schema.TeamRoleTeammate), "", "", "", "", ""))
	require.True(t, db.Member().CreateMember(ctx, "e1", "team-a", "e1", "{}", "", string(schema.TeamRoleExternalCLI), "", "", "", "", ""))
	require.True(t, db.Member().CreateMember(ctx, "l1", "team-a", "l1", "{}", "", string(schema.TeamRoleLeader), "", "", "", "", ""))
	require.True(t, db.Member().CreateMember(ctx, "h1", "team-a", "h1", "{}", "", string(schema.TeamRoleHumanAgent), "", "", "", "", ""))

	backend := &fakeTeamBackendAccess{teamName: "team-a", db: db}
	names, err := TeammateMemberNames(ctx, backend, "team-a")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"t1", "e1"}, names)
}

// ──────────────────────────── scope 匹配 ────────────────────────────

// TestMatchesScope_三键 DB 元数据三键（session/project_hash/managed_root）齐同才算。
// Python: matches_scope
func TestMatchesScope_三键(t *testing.T) {
	scope := WorktreeOwnerScope{SessionID: "s1", ProjectHash: "ph", ManagedRoot: "/root"}
	opts := &database.MemberWorktreeOptions{
		SessionID:   strPtr("s1"),
		ProjectHash: strPtr("ph"),
		ManagedRoot: strPtr("/root"),
	}
	assert.True(t, MatchesScope(opts, scope))

	opts.SessionID = strPtr("s2")
	assert.False(t, MatchesScope(opts, scope))
	assert.False(t, MatchesScope(nil, scope))
}

// TestInfoMatchesScope_含路径非空 缓存元数据还要求路径非空。
// Python: info_matches_scope
func TestInfoMatchesScope_含路径非空(t *testing.T) {
	scope := WorktreeOwnerScope{SessionID: "s1", ProjectHash: "ph", ManagedRoot: "/root"}
	info := &MemberWorktreeInfo{SessionID: "s1", ProjectHash: "ph", ManagedRoot: "/root", WorktreePath: "/wt"}
	assert.True(t, InfoMatchesScope(info, scope))

	info.WorktreePath = ""
	assert.False(t, InfoMatchesScope(info, scope))
}

// ──────────────────────────── InfoFromOptions ────────────────────────────

// TestInfoFromOptions_元数据不全告警返回nil 任一键缺失返回 nil（保守保留）。
// Python: info_from_options :95-102
func TestInfoFromOptions_元数据不全告警返回nil(t *testing.T) {
	scope := WorktreeOwnerScope{SessionID: "s1", ProjectHash: "ph", ManagedRoot: "/root", WorktreeName: "wt"}
	opts := &database.MemberWorktreeOptions{
		Path:        strPtr("/wt"),
		SessionID:   strPtr("s1"),
		ProjectHash: nil, // 缺
		ManagedRoot: strPtr("/root"),
	}
	assert.Nil(t, InfoFromOptions(opts, "m1", scope))
}

// TestInfoFromOptions_scope不匹配返回nil scope 不匹配返回 nil。
// Python: info_from_options :104-106
func TestInfoFromOptions_scope不匹配返回nil(t *testing.T) {
	scope := WorktreeOwnerScope{SessionID: "s1", ProjectHash: "ph", ManagedRoot: "/root", WorktreeName: "wt"}
	opts := &database.MemberWorktreeOptions{
		Path:        strPtr("/wt"),
		SessionID:   strPtr("other"),
		ProjectHash: strPtr("ph"),
		ManagedRoot: strPtr("/root"),
	}
	assert.Nil(t, InfoFromOptions(opts, "m1", scope))
}

// TestInfoFromOptions_全键命中 全键命中构建完整托管元数据。
func TestInfoFromOptions_全键命中(t *testing.T) {
	scope := WorktreeOwnerScope{SessionID: "s1", ProjectHash: "ph", ManagedRoot: "/root", WorktreeName: "wt"}
	opts := &database.MemberWorktreeOptions{
		Path:           strPtr("/wt"),
		SessionID:      strPtr("s1"),
		ProjectHash:    strPtr("ph"),
		ManagedRoot:    strPtr("/root"),
		WorktreeBranch: strPtr("worktree-wt"),
		HeadCommit:     strPtr("abc"),
	}
	info := InfoFromOptions(opts, "m1", scope)
	require.NotNil(t, info)
	assert.Equal(t, "/wt", info.WorktreePath)
	assert.Equal(t, "wt", info.WorktreeName)
	assert.Equal(t, "worktree-wt", info.WorktreeBranch)
	assert.Equal(t, "abc", info.HeadCommit)
	assert.Equal(t, "s1", info.SessionID)
}

// ──────────────────────────── Resolve / Clear ────────────────────────────

// TestResolveCurrentSessionMemberWorktree_无DB无缓存返回nil 成员无 worktree 且缓存空 → nil。
// Python: resolve_current_session_member_worktree :152-156
func TestResolveCurrentSessionMemberWorktree_无DB无缓存返回nil(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t, "m1", string(schema.TeamRoleTeammate))
	cfg := &fakeConfigurator{backend: &fakeTeamBackendAccess{teamName: "team-a", db: db}, teamName: "team-a"}

	resolved, err := ResolveCurrentSessionMemberWorktree(ctx, cfg,
		map[string]*MemberWorktreeInfo{}, "m1",
		func(ctx context.Context, teamName, memberName string) (WorktreeOwnerScope, error) {
			return WorktreeOwnerScope{SessionID: "s1"}, nil
		}, false)
	require.NoError(t, err)
	assert.Nil(t, resolved)
}

// TestResolveCurrentSessionMemberWorktree_DB命中构建 DB worktree 匹配 scope 时从 DB 构建。
// Python: :162-176 info_from_options 分支
func TestResolveCurrentSessionMemberWorktree_DB命中构建(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t, "m1", string(schema.TeamRoleTeammate))
	scope := WorktreeOwnerScope{
		SessionID: "s1", ProjectHash: "ph", ManagedRoot: "/root",
		TeamName: "team-a", MemberName: "m1", WorktreeName: "wt",
	}
	require.True(t, db.Member().UpdateMemberWorktree(ctx, "m1", "team-a",
		&database.MemberWorktreeOptions{
			Isolation:   strPtr("worktree"),
			Path:        strPtr("/wt"),
			SessionID:   strPtr("s1"),
			ProjectHash: strPtr("ph"),
			ManagedRoot: strPtr("/root"),
		}, "", ""))
	cfg := &fakeConfigurator{backend: &fakeTeamBackendAccess{teamName: "team-a", db: db}, teamName: "team-a"}

	resolved, err := ResolveCurrentSessionMemberWorktree(ctx, cfg,
		map[string]*MemberWorktreeInfo{}, "m1",
		func(ctx context.Context, teamName, memberName string) (WorktreeOwnerScope, error) { return scope, nil }, false)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	assert.True(t, resolved.DBMatchesScope)
	assert.Equal(t, "/wt", resolved.WorktreeInfo.WorktreePath)
}

// TestResolveCurrentSessionMemberWorktree_外来不日志条件 logForeign=false 时不产生
// 外来日志路径返回 nil（外来 worktree 的 DB 不匹配 → 返回 nil）。
// Python: :178-186
func TestResolveCurrentSessionMemberWorktree_外来不日志条件(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t, "m1", string(schema.TeamRoleTeammate))
	// DB 里的 session 与 scope 不同 → 外来
	require.True(t, db.Member().UpdateMemberWorktree(ctx, "m1", "team-a",
		&database.MemberWorktreeOptions{
			Isolation:   strPtr("worktree"),
			Path:        strPtr("/foreign"),
			SessionID:   strPtr("other-session"),
			ProjectHash: strPtr("ph"),
			ManagedRoot: strPtr("/root"),
		}, "", ""))
	cfg := &fakeConfigurator{backend: &fakeTeamBackendAccess{teamName: "team-a", db: db}, teamName: "team-a"}

	resolved, err := ResolveCurrentSessionMemberWorktree(ctx, cfg,
		map[string]*MemberWorktreeInfo{}, "m1",
		func(ctx context.Context, teamName, memberName string) (WorktreeOwnerScope, error) {
			return WorktreeOwnerScope{SessionID: "s1", ProjectHash: "ph", ManagedRoot: "/root"}, nil
		}, true)
	require.NoError(t, err)
	assert.Nil(t, resolved, "外来 worktree 不归本会话，返回 nil（重建交由 lifecycle）")
}

// TestClearMemberWorktreeMetadata_收敛为isolationOnly DB 匹配时收敛为 isolation-only，
// 缓存条目弹出。
// Python: clear_member_worktree_metadata
func TestClearMemberWorktreeMetadata_收敛为isolationOnly(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t, "m1", string(schema.TeamRoleTeammate))
	require.True(t, db.Member().UpdateMemberWorktree(ctx, "m1", "team-a",
		&database.MemberWorktreeOptions{
			Isolation:   strPtr("worktree"),
			Path:        strPtr("/wt"),
			SessionID:   strPtr("s1"),
			ProjectHash: strPtr("ph"),
			ManagedRoot: strPtr("/root"),
		}, "", ""))

	cache := map[string]*MemberWorktreeInfo{"m1": {WorktreePath: "/wt"}}
	resolved := &ResolvedMemberWorktree{
		TeamBackend:    &fakeTeamBackendAccess{teamName: "team-a", db: db},
		TeamName:       "team-a",
		DBMatchesScope: true,
	}
	require.NoError(t, ClearMemberWorktreeMetadata(ctx, "m1", resolved, cache))

	_, ok := cache["m1"]
	assert.False(t, ok, "宿主缓存应弹出")

	// DB 收敛：isolation 保留 worktree，其余清空
	teammate, err := db.Member().GetMember(ctx, "m1", "team-a")
	require.NoError(t, err)
	opts := database.GetMemberWorktree(teammate)
	require.NotNil(t, opts)
	assert.Equal(t, "worktree", strDeref(opts.Isolation))
	assert.Nil(t, opts.Path)
}
