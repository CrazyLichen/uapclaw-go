package worktree

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent 日志组件（包级，与 lifecycle/session_cleanup 共用）
var logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// TeamContextProvider configurator 的窄接口面（Python duck-typing 的 Go 等价，
// 偏差 D-25.7-i：Python configurator: Any → Go 结构化窄接口；agent.AgentConfigurator
// 结构性实现本接口，本包 import tools/schema 不 import agent 包避环）。
// Python: team_context(configurator) 读 configurator.team_backend / team_name。
type TeamContextProvider interface {
	// TeamBackend 返回团队后端访问缝（可能 nil；agent.AgentConfigurator 经
	// 包内适配器实现——其 TeamBackend() 返回 *tools.TeamBackend，结构性满足
	// TeamBackendAccess，由 agent 侧适配器转换）
	TeamBackend() TeamBackendAccess
	// TeamName 返回团队名
	TeamName() string
}

// TeamBackendAccess worktree 层消费的团队后端窄接口（*tools.TeamBackend 结构性
// 实现：TeamName() + DB()）。
type TeamBackendAccess interface {
	// TeamName 返回团队名
	TeamName() string
	// DB 返回团队数据库门面
	DB() database.TeamDatabase
}

// memberDB 取成员 DAO 的便捷函数（Python team_backend.db.member 等价）。
func memberDB(tb TeamBackendAccess) database.MemberDao {
	return tb.DB().Member()
}

// MemberWorktreeInfo 一个 teammate worktree 的 leader 侧托管元数据。
// Python: member_state.py MemberWorktreeInfo (BaseModel, :23-32)
type MemberWorktreeInfo struct {
	// WorktreePath worktree 绝对路径
	WorktreePath string
	// WorktreeName worktree slug
	WorktreeName string
	// WorktreeBranch worktree 分支名（可能空）
	WorktreeBranch string
	// HeadCommit 创建时的 HEAD 提交（可能空）
	HeadCommit string
	// HookBased 是否成员经 enter_worktree 钩子自建
	HookBased bool
	// SessionID 所属会话
	SessionID string
	// ProjectHash 项目哈希段
	ProjectHash string
	// ManagedRoot 会话 worktrees 根目录
	ManagedRoot string
}

// ResolvedMemberWorktree 本会话拥有的 teammate worktree 的 DB/缓存解析结果。
// Python: member_state.py ResolvedMemberWorktree (dataclass, :35-41)
type ResolvedMemberWorktree struct {
	// TeamBackend 团队后端
	TeamBackend TeamBackendAccess
	// TeamName 团队名
	TeamName string
	// Scope 所有者 scope
	Scope WorktreeOwnerScope
	// DBMatchesScope DB 元数据是否属于本 scope
	DBMatchesScope bool
	// WorktreeInfo 托管元数据
	WorktreeInfo *MemberWorktreeInfo
}

// ──────────────────────────── 枚举 ────────────────────────────

// （无枚举）

// ──────────────────────────── 常量 ────────────────────────────

// （无包级常量）

// ──────────────────────────── 全局变量 ────────────────────────────

// （无包级变量）

// ──────────────────────────── 导出函数 ────────────────────────────

// TeamContext 从 configurator 取团队后端/团队名对。
// Python: member_state.py team_context(configurator) (:47-55)
//
// configurator 团队名为空时回退 team_backend 的团队名；二者缺一返回 nil。
func TeamContext(configurator TeamContextProvider) (TeamBackendAccess, string, bool) {
	teamBackend := configurator.TeamBackend()
	teamName := configurator.TeamName()
	if teamName == "" && teamBackend != nil {
		teamName = teamBackend.TeamName()
	}
	if teamBackend == nil || teamName == "" {
		return nil, "", false
	}
	return teamBackend, teamName, true
}

// TeammateMemberNames 返回团队的 teammate 成员名列表。
// Python: member_state.py teammate_member_names(team_backend, team_name) (:58-63)
//
// 仅协调角色：teammate / external_cli。
func TeammateMemberNames(ctx context.Context, teamBackend TeamBackendAccess, teamName string) ([]string, error) {
	members, err := memberDB(teamBackend).GetTeamMembers(ctx, teamName, "")
	if err != nil {
		return nil, err
	}
	coordinated := map[string]bool{
		string(schema.TeamRoleTeammate):     true,
		string(schema.TeamRoleExternalCLI):  true,
	}
	names := make([]string, 0, len(members))
	for _, m := range members {
		if coordinated[m.Role] {
			names = append(names, m.MemberName)
		}
	}
	return names, nil
}

// MatchesScope DB worktree 元数据是否属于所有者 scope。
// Python: member_state.py matches_scope(worktree, scope) (:65-75)
func MatchesScope(worktreeOptions *database.MemberWorktreeOptions, scope WorktreeOwnerScope) bool {
	if worktreeOptions == nil {
		return false
	}
	return strEq(strDeref(worktreeOptions.SessionID), scope.SessionID) &&
		strEq(strDeref(worktreeOptions.ProjectHash), scope.ProjectHash) &&
		strEq(strDeref(worktreeOptions.ManagedRoot), scope.ManagedRoot)
}

// InfoMatchesScope 缓存的托管元数据是否属于所有者 scope。
// Python: member_state.py info_matches_scope(worktree_info, scope) (:77-88)
func InfoMatchesScope(worktreeInfo *MemberWorktreeInfo, scope WorktreeOwnerScope) bool {
	if worktreeInfo == nil {
		return false
	}
	return worktreeInfo.SessionID == scope.SessionID &&
		worktreeInfo.ProjectHash == scope.ProjectHash &&
		worktreeInfo.ManagedRoot == scope.ManagedRoot &&
		worktreeInfo.WorktreePath != ""
}

// InfoFromOptions 从持久化的 DB 成员选项构建托管元数据。
// Python: member_state.py info_from_options(worktree, *, member_name, scope) (:90-116)
//
// 所有权元数据不完整时告警并返回 nil（保守保留路径不重建）；scope 不匹配
// 返回 nil（外来/遗留 worktree 交由调用方日志并重建）。
func InfoFromOptions(worktreeOptions *database.MemberWorktreeOptions, memberName string, scope WorktreeOwnerScope) *MemberWorktreeInfo {
	if worktreeOptions == nil ||
		strIsEmpty(strDeref(worktreeOptions.Path)) ||
		strIsEmpty(strDeref(worktreeOptions.SessionID)) ||
		strIsEmpty(strDeref(worktreeOptions.ProjectHash)) ||
		strIsEmpty(strDeref(worktreeOptions.ManagedRoot)) {
		logger.Warn(logComponent).
			Str("member_name", memberName).
			Str("worktree_path", strDeref(worktreeOptionsPath(worktreeOptions))).
			Msg("Keeping worktree for teammate because worktree ownership metadata is incomplete")
		return nil
	}
	if !MatchesScope(worktreeOptions, scope) {
		return nil
	}
	return &MemberWorktreeInfo{
		WorktreePath:   strDeref(worktreeOptions.Path),
		WorktreeName:   scope.WorktreeName,
		WorktreeBranch: strDeref(worktreeOptions.WorktreeBranch),
		HeadCommit:     strDeref(worktreeOptions.HeadCommit),
		SessionID:      strDeref(worktreeOptions.SessionID),
		ProjectHash:    strDeref(worktreeOptions.ProjectHash),
		ManagedRoot:    strDeref(worktreeOptions.ManagedRoot),
	}
}

// ResolveCurrentSessionMemberWorktree 从 DB 选项与宿主缓存解析本会话的 teammate worktree。
// Python: member_state.py resolve_current_session_member_worktree(configurator,
// member_worktree_info, member_name, owner_scope, *, log_foreign) (:134-192)
//
// ownerScope 闭包签名 (teamName, memberName) → scope（Python Callable 语义）。
// memberWorktreeInfo 由 lifecycle 持有传入（Python MutableMapping 参数语义）。
func ResolveCurrentSessionMemberWorktree(
	ctx context.Context,
	configurator TeamContextProvider,
	memberWorktreeInfo map[string]*MemberWorktreeInfo,
	memberName string,
	// ownerScope 闭包携带 ctx（偏差 D-25.7-e：Python contextvar 全局态 → Go 显式传参）
	ownerScope func(ctx context.Context, teamName, memberName string) (WorktreeOwnerScope, error),
	logForeign bool,
) (*ResolvedMemberWorktree, error) {
	teamBackend, teamName, ok := TeamContext(configurator)
	if !ok {
		return nil, nil
	}

	teammate, err := memberDB(teamBackend).GetMember(ctx, memberName, teamName)
	if err != nil {
		return nil, err
	}
	if teammate == nil {
		return nil, nil
	}
	worktreeOptions := database.GetMemberWorktree(teammate)
	worktreeInfo := memberWorktreeInfo[memberName]
	hasDBWorktree := worktreeOptions != nil &&
		strDeref(worktreeOptions.Isolation) == "worktree" &&
		strDeref(worktreeOptions.Path) != ""
	if worktreeInfo == nil && !hasDBWorktree {
		return nil, nil
	}

	scope, err := ownerScope(ctx, teamName, memberName)
	if err != nil {
		return nil, err
	}

	dbMatchesScope := worktreeOptions != nil && hasDBWorktree && MatchesScope(worktreeOptions, scope)
	if worktreeInfo != nil && !InfoMatchesScope(worktreeInfo, scope) {
		worktreeInfo = nil
	}

	if shouldLogForeignWorktree(worktreeOptions, dbMatchesScope, logForeign) {
		logger.Info(logComponent).
			Str("member_name", memberName).
			Str("worktree_path", strDeref(worktreeOptionsPath(worktreeOptions))).
			Msg("Keeping worktree for teammate because it is not owned by the current session")
	}

	if worktreeInfo == nil {
		if !dbMatchesScope || worktreeOptions == nil {
			return nil, nil
		}
		worktreeInfo = InfoFromOptions(worktreeOptions, memberName, scope)
		if worktreeInfo == nil {
			return nil, nil
		}
	}

	return &ResolvedMemberWorktree{
		TeamBackend:    teamBackend,
		TeamName:       teamName,
		Scope:          scope,
		DBMatchesScope: dbMatchesScope,
		WorktreeInfo:   worktreeInfo,
	}, nil
}

// ClearMemberWorktreeMetadata 清除 DB 与宿主缓存里的本会话 worktree 元数据。
// Python: member_state.py clear_member_worktree_metadata(member_name, resolved,
// member_worktree_info) (:194-203)
//
// DB 匹配 scope 时把 worktree 段收敛为 isolation-only（保留隔离声明、清路径，
// 对齐 MemberWorktreeOptions(isolation="worktree")）；宿主缓存无条件弹出。
func ClearMemberWorktreeMetadata(
	ctx context.Context,
	memberName string,
	resolved *ResolvedMemberWorktree,
	memberWorktreeInfo map[string]*MemberWorktreeInfo,
) error {
	if resolved.DBMatchesScope {
		isolation := "worktree"
		ok := memberDB(resolved.TeamBackend).UpdateMemberWorktree(ctx, memberName, resolved.TeamName,
			&database.MemberWorktreeOptions{Isolation: &isolation}, "", "")
		if !ok {
			return fmt.Errorf("member %s not found in team %s", memberName, resolved.TeamName)
		}
	}
	delete(memberWorktreeInfo, memberName)
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// shouldLogForeignWorktree 是否记录外来 worktree 日志。
// Python: member_state.py _should_log_foreign_worktree (:122-132)
func shouldLogForeignWorktree(worktreeOptions *database.MemberWorktreeOptions, dbMatchesScope, logForeign bool) bool {
	if !logForeign || worktreeOptions == nil || dbMatchesScope {
		return false
	}
	return strDeref(worktreeOptions.Isolation) == "worktree" && strDeref(worktreeOptions.Path) != ""
}

// strDeref nil 安全解引用。
func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// strEq nil 与空串等价的字符串比较（Python None == None 语义）。
func strEq(a, b string) bool { return a == b }

// strIsEmpty 空串判定。
func strIsEmpty(s string) bool { return s == "" }

// worktreeOptionsPath nil 安全取 Path。
func worktreeOptionsPath(o *database.MemberWorktreeOptions) *string {
	if o == nil {
		return nil
	}
	return o.Path
}
