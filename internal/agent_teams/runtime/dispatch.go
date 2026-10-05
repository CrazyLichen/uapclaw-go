package runtime

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RunAction 调度决策结果。
// Python: RunAction (openjiuwen/agent_teams/runtime/dispatch.py)
type RunAction struct {
	// Kind 调度动作
	Kind RunActionKind
	// RequireSpec 是否需要 TeamAgentSpec
	RequireSpec bool
	// Reason 拒绝原因（空字符串表示无原因）
	Reason string
}

// ──────────────────────────── 枚举 ────────────────────────────

// RunActionKind 调度动作枚举。
// Python: RunActionKind (openjiuwen/agent_teams/runtime/dispatch.py)
type RunActionKind string

const (
	// RunActionKindCreate 创建新团队
	RunActionKindCreate RunActionKind = "create"
	// RunActionKindNewTeamInSession 在新 session 上启动已有团队
	RunActionKindNewTeamInSession RunActionKind = "new_team_in_session"
	// RunActionKindColdRecover 冷恢复（从 session checkpoint 恢复）
	RunActionKindColdRecover RunActionKind = "cold_recover"
	// RunActionKindResumeFromPause 从暂停恢复
	RunActionKindResumeFromPause RunActionKind = "resume_from_pause"
	// RunActionKindRejectRunning 拒绝：团队已在运行
	RunActionKindRejectRunning RunActionKind = "reject_running"
	// RunActionKindRejectOrphaned 拒绝：session 有桶但 DB 无行
	RunActionKindRejectOrphaned RunActionKind = "reject_orphaned"
	// RunActionKindRejectInconsistent 拒绝：pool 有条目但 DB 无行
	RunActionKindRejectInconsistent RunActionKind = "reject_inconsistent"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// IsTeamRejectKind 判断是否为拒绝类调度动作。
// Python: _is_team_reject_kind(kind) (team_runner.py L107-112)
func IsTeamRejectKind(kind RunActionKind) bool {
	return kind == RunActionKindRejectRunning ||
		kind == RunActionKindRejectOrphaned ||
		kind == RunActionKindRejectInconsistent
}

// DecideRunAction 纯调度决策（无副作用）。
// Python: decide_run_action(team_in_db, team_in_session, pool_entry, target_session_id, target_team_name, team_db_state)
//
// 决策真值表：
//
//	teamInDB  teamInSession  poolEntry   结果
//	false     false          nil         CREATE
//	false     false          present     REJECT_INCONSISTENT
//	false     true           —           REJECT_ORPHANED (pending_create/cleaned → CREATE)
//	true      false          nil         NEW_TEAM_IN_SESSION
//	true      true           nil         COLD_RECOVER
//	true      true           RUNNING     REJECT_RUNNING
//	true      true           PAUSED      RESUME_FROM_PAUSE
func DecideRunAction(
	teamInDB bool,
	teamInSession bool,
	poolEntry *ActiveTeam,
	targetSessionID string,
	targetTeamName string,
	teamDBState string,
) RunAction {
	// Python 步骤 1: 可重建 — session 桶中的 pending_create / cleaned 状态
	// 此检查必须在 REJECT_INCONSISTENT 之前，因为 pending_create/cleaned 状态
	// 的 pool 条目应被允许重建，而非被拒绝为不一致。
	if !teamInDB && teamInSession {
		if teamDBState == metadata.TeamDBStatePendingCreate ||
			teamDBState == metadata.TeamDBStateCleaned {
			return RunAction{Kind: RunActionKindCreate, RequireSpec: true}
		}
		return RunAction{
			Kind:        RunActionKindRejectOrphaned,
			RequireSpec: false,
			Reason: fmt.Sprintf(
				"team %q not in DB but session bucket exists for %q",
				targetTeamName, targetSessionID,
			),
		}
	}

	// Python 步骤 2: 不一致 — pool 有条目但 DB 无行
	// 仅在 team_db_state 非 pending_create/cleaned 时可达
	if !teamInDB && poolEntry != nil {
		return RunAction{
			Kind:        RunActionKindRejectInconsistent,
			RequireSpec: false,
			Reason: fmt.Sprintf(
				"team %q present in pool but missing from DB",
				targetTeamName,
			),
		}
	}

	// Python 步骤 3: 全新团队
	if !teamInDB {
		return RunAction{Kind: RunActionKindCreate, RequireSpec: true}
	}

	// Python 步骤 4: 冷路径（无 pool 条目，DB 有团队）
	if poolEntry == nil {
		if teamInSession {
			return RunAction{Kind: RunActionKindColdRecover, RequireSpec: false}
		}
		return RunAction{Kind: RunActionKindNewTeamInSession, RequireSpec: false}
	}

	// Python 步骤 5: Pool 条目存在。
	// Manager.activate 保证条目属于 target_session_id —
	// 跨 session 条目在 dispatch 之前已拆除，
	// 因此到达此分支时 session 不匹配是契约违反。
	if poolEntry.SessionID != targetSessionID {
		panic(fmt.Sprintf(
			"dispatch invariant violated: pool entry for %q on session %q must be torn down before dispatching to session %q",
			targetTeamName, poolEntry.SessionID, targetSessionID,
		))
	}
	if poolEntry.State == RuntimeStatePaused {
		return RunAction{Kind: RunActionKindResumeFromPause, RequireSpec: false}
	}
	return RunAction{
		Kind:        RunActionKindRejectRunning,
		RequireSpec: false,
		Reason: fmt.Sprintf(
			"team %q already running on session %q; use interact",
			targetTeamName, targetSessionID,
		),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
