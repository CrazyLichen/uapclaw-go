package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestIsTeamRejectKind 判断拒绝类调度动作
func TestIsTeamRejectKind(t *testing.T) {
	assert.True(t, IsTeamRejectKind(RunActionKindRejectRunning))
	assert.True(t, IsTeamRejectKind(RunActionKindRejectOrphaned))
	assert.True(t, IsTeamRejectKind(RunActionKindRejectInconsistent))
	assert.False(t, IsTeamRejectKind(RunActionKindCreate))
	assert.False(t, IsTeamRejectKind(RunActionKindNewTeamInSession))
	assert.False(t, IsTeamRejectKind(RunActionKindColdRecover))
	assert.False(t, IsTeamRejectKind(RunActionKindResumeFromPause))
}

// TestDecideRunAction 全部决策路径
// Python: dispatch.py decide_run_action 真值表
func TestDecideRunAction(t *testing.T) {
	tests := []struct {
		name            string
		teamInDB        bool
		teamInSession   bool
		poolEntry       *ActiveTeam
		sessionID       string
		teamName        string
		teamDBState     string
		wantKind        RunActionKind
		wantRequireSpec bool
		wantReason      bool // 是否期望有 reason
		wantPanic       bool
	}{
		{
			name:     "全新团队_无DB无Session无Pool",
			teamInDB: false, teamInSession: false, poolEntry: nil,
			sessionID: "sess-1", teamName: "team-a", teamDBState: "",
			wantKind: RunActionKindCreate, wantRequireSpec: true, wantReason: false,
		},
		{
			name:     "不一致_Pool有条目但DB无行",
			teamInDB: false, teamInSession: false,
			poolEntry: &ActiveTeam{TeamName: "team-a", SessionID: "sess-1", State: RuntimeStateRunning},
			sessionID: "sess-1", teamName: "team-a", teamDBState: "",
			wantKind: RunActionKindRejectInconsistent, wantRequireSpec: false, wantReason: true,
		},
		{
			name:     "孤立_Session有桶但DB无行",
			teamInDB: false, teamInSession: true, poolEntry: nil,
			sessionID: "sess-1", teamName: "team-a", teamDBState: "",
			wantKind: RunActionKindRejectOrphaned, wantRequireSpec: false, wantReason: true,
		},
		{
			name:     "可重建_PendingCreate状态",
			teamInDB: false, teamInSession: true, poolEntry: nil,
			sessionID: "sess-1", teamName: "team-a", teamDBState: "pending_create",
			wantKind: RunActionKindCreate, wantRequireSpec: true, wantReason: false,
		},
		{
			name:     "可重建_Cleaned状态",
			teamInDB: false, teamInSession: true, poolEntry: nil,
			sessionID: "sess-1", teamName: "team-a", teamDBState: "cleaned",
			wantKind: RunActionKindCreate, wantRequireSpec: true, wantReason: false,
		},
		{
			name:     "新Session上的已有团队_DB有行无Session无Pool",
			teamInDB: true, teamInSession: false, poolEntry: nil,
			sessionID: "sess-1", teamName: "team-a", teamDBState: "",
			wantKind: RunActionKindNewTeamInSession, wantRequireSpec: false, wantReason: false,
		},
		{
			name:     "冷恢复_DB有行Session有桶无Pool",
			teamInDB: true, teamInSession: true, poolEntry: nil,
			sessionID: "sess-1", teamName: "team-a", teamDBState: "",
			wantKind: RunActionKindColdRecover, wantRequireSpec: false, wantReason: false,
		},
		{
			name:     "拒绝运行_Pool有条目且Running",
			teamInDB: true, teamInSession: true,
			poolEntry: &ActiveTeam{TeamName: "team-a", SessionID: "sess-1", State: RuntimeStateRunning},
			sessionID: "sess-1", teamName: "team-a", teamDBState: "",
			wantKind: RunActionKindRejectRunning, wantRequireSpec: false, wantReason: true,
		},
		{
			name:     "从暂停恢复_Pool有条目且Paused",
			teamInDB: true, teamInSession: true,
			poolEntry: &ActiveTeam{TeamName: "team-a", SessionID: "sess-1", State: RuntimeStatePaused},
			sessionID: "sess-1", teamName: "team-a", teamDBState: "",
			wantKind: RunActionKindResumeFromPause, wantRequireSpec: false, wantReason: false,
		},
		{
			name:     "契约违反_Pool条目Session不匹配",
			teamInDB: true, teamInSession: true,
			poolEntry: &ActiveTeam{TeamName: "team-a", SessionID: "sess-old", State: RuntimeStateRunning},
			sessionID: "sess-new", teamName: "team-a", teamDBState: "",
			wantPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				assert.Panics(t, func() {
					DecideRunAction(tt.teamInDB, tt.teamInSession, tt.poolEntry, tt.sessionID, tt.teamName, tt.teamDBState)
				})
				return
			}
			got := DecideRunAction(tt.teamInDB, tt.teamInSession, tt.poolEntry, tt.sessionID, tt.teamName, tt.teamDBState)
			assert.Equal(t, tt.wantKind, got.Kind)
			assert.Equal(t, tt.wantRequireSpec, got.RequireSpec)
			if tt.wantReason {
				assert.NotEmpty(t, got.Reason)
			}
		})
	}
}

// TestDecideRunAction_可重建优先于不一致 验证 pending_create 状态的 pool 条目走 CREATE 而非 REJECT_INCONSISTENT
// Python: dispatch.py 注释 — "This check must come BEFORE the REJECT_INCONSISTENT check"
func TestDecideRunAction_可重建优先于不一致(t *testing.T) {
	// teamInDB=false, teamInSession=true, teamDBState="pending_create" → CREATE（不是 REJECT_ORPHANED）
	got := DecideRunAction(false, true, nil, "sess-1", "team-a", "pending_create")
	assert.Equal(t, RunActionKindCreate, got.Kind)
	assert.True(t, got.RequireSpec)

	// teamInDB=false, teamInSession=true, teamDBState="cleaned" → CREATE
	got = DecideRunAction(false, true, nil, "sess-1", "team-a", "cleaned")
	assert.Equal(t, RunActionKindCreate, got.Kind)
	assert.True(t, got.RequireSpec)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
