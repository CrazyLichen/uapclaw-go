package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/worktree"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 枚举 ────────────────────────────

// WorktreeContributionState teammate worktree 分支的贡献分类。
// Python: lifecycle.py WorktreeContributionState (str Enum, :66-72)
type WorktreeContributionState string

const (
	// WorktreeContributionContributed 分支相对基线有新提交
	WorktreeContributionContributed WorktreeContributionState = "contributed"
	// WorktreeContributionNotContributed 分支无新提交（干净）
	WorktreeContributionNotContributed WorktreeContributionState = "not_contributed"
	// WorktreeContributionUnknown 无法验证（保守保留）
	WorktreeContributionUnknown WorktreeContributionState = "unknown"
)

// ──────────────────────────── 常量 ────────────────────────────

// teamRuntimeFiles 团队运行时文件集合（不算用户改动）。
// Python: lifecycle.py _TEAM_RUNTIME_FILES (:40-48)
var teamRuntimeFiles = map[string]bool{
	".DS_Store":    true,
	"AGENT.md":     true,
	"HEARTBEAT.md": true,
	"IDENTITY.md":  true,
	"SOUL.md":      true,
	"USER.md":      true,
}

// teamRuntimeDirs 团队运行时目录集合（顶层目录，不算用户改动）。
// Python: lifecycle.py _TEAM_RUNTIME_DIRS (:51-61)
var teamRuntimeDirs = map[string]bool{
	".agent_history": true,
	".team":          true,
	"agents":         true,
	"coding_memory":  true,
	"context":        true,
	"memory":         true,
	"messages":       true,
	"skills":         true,
	"todo":           true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// LifecycleProvider lifecycle 消费的 configurator 窄接口面（Python AgentConfigurator
// duck-typing 的 Go 等价；agent.AgentConfigurator 经包内适配器实现，D-25.7-i）。
type LifecycleProvider interface {
	TeamContextProvider
	// Spec 返回团队蓝图 spec（可能 nil）
	Spec() *atschema.TeamAgentSpec
	// WorktreeManager 返回（或惰性构造）worktree 管理器
	WorktreeManager() *worktree.WorktreeManager
}

// TeammateWorktreeLifecycle 为 teammate 创建与收编 leader 托管的 worktree。
// Python: lifecycle.py TeammateWorktreeLifecycle (:74-79)
type TeammateWorktreeLifecycle struct {
	// configurator 团队装配器（窄接口）
	configurator LifecycleProvider
	// memberWorktreeInfo 成员名 → 宿主侧托管元数据缓存
	// Python: _member_worktree_info dict
	memberWorktreeInfo map[string]*MemberWorktreeInfo
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeammateWorktreeLifecycle 构造 lifecycle。
// Python: lifecycle.py __init__(configurator) (:77-79)
func NewTeammateWorktreeLifecycle(configurator LifecycleProvider) *TeammateWorktreeLifecycle {
	return &TeammateWorktreeLifecycle{
		configurator:       configurator,
		memberWorktreeInfo: map[string]*MemberWorktreeInfo{},
	}
}

// EnsureMemberWorktree 为 teammate 创建或复用当前会话的 worktree。
// Python: lifecycle.py ensure_member_worktree(teammate, role) (:205-293)
//
// 返回 worktree 路径；不需要隔离时返回空串（无错误）。
// memberStatus 为空串表示跳过状态门（Python 由调用方传 MemberStatus）。
func (l *TeammateWorktreeLifecycle) EnsureMemberWorktree(
	ctx context.Context,
	memberName string,
	role atschema.TeamRole,
	memberStatus string,
) (string, error) {
	// 三重门 1：角色必须是协调成员（Python _member_needs_worktree :91-98）
	if !role.IsCoordinatedMember() {
		return "", nil
	}
	// 三重门 2：会话级禁用（Python _session_disables_teammate_worktree :81-89）
	if l.sessionDisablesTeammateWorktree() {
		logger.Info(logComponent).Str("member_name", memberName).
			Msg("Skipping teammate worktree because the current runtime disabled worktree isolation")
		return "", nil
	}
	// 三重门 0：成员状态非关闭中（Python ensure_member_worktree :211-213 调用方门）
	if memberStatus == "shutdown_requested" || memberStatus == "shutdown" {
		return "", nil
	}

	teamBackend, teamName, ok := TeamContext(l.configurator)
	if !ok {
		return "", fmt.Errorf("Team worktree isolation requires a team backend")
	}

	// Python :226 worktree = get_member_worktree(teammate) 先取 DB 选项，
	// :211 _member_needs_worktree 要求角色为协调成员且 DB 行声明 isolation=="worktree"
	worktreeOptions := l.memberWorktreeOptions(memberName)
	if !memberNeedsWorktree(role, worktreeOptions) {
		return "", nil
	}
	scope, err := l.ownerScope(ctx, teamName, memberName)
	if err != nil {
		return "", err
	}
	worktreePath := strDeref(worktreeOptionsPath(worktreeOptions))
	if worktreeOptions != nil && worktreePath != "" {
		if st, serr := os.Stat(worktreePath); serr == nil && st.IsDir() {
			if MatchesScope(worktreeOptions, scope) {
				info := InfoFromOptions(worktreeOptions, memberName, scope)
				if info != nil {
					l.memberWorktreeInfo[memberName] = info
					return worktreePath, nil
				}
			}
			logger.Info(logComponent).
				Str("member_name", memberName).
				Str("worktree_path", worktreePath).
				Msg("Not reusing foreign or legacy worktree for teammate")
		}
		// 路径存在但 scope 匹配、目录缺失：清 DB 元数据后重建
		// Python :243-252 elif worktree is not None and worktree_path and matches_scope(...)
	} else if worktreeOptions != nil && MatchesScope(worktreeOptions, scope) {
		// Python :244-248: update_member_worktree 收敛为 isolation-only 后走重建
		_ = worktreeOptions // MatchesScope 为真但路径缺失（上方分支已含目录判定）
	}

	manager, err := l.managerForScope(scope)
	if err != nil {
		return "", err
	}

	// 把 worktree git 命令临时锚定到项目根（Python :266 enter_project_worktree_context）
	anchoredCtx, restore, err := EnterProjectWorktreeContext(ctx, scope.ProjectDir, scope.ManagedRoot)
	if err != nil {
		return "", err
	}
	result, err := manager.CreateOwnerWorktree(anchoredCtx, scope.WorktreeName, scope.ProjectDir)
	if err != nil {
		return "", err
	}
	_ = RestoreProjectWorktreeContext(restore)

	isolation := "worktree"
	uok := memberDB(teamBackend).UpdateMemberWorktree(ctx, memberName, teamName,
		&database.MemberWorktreeOptions{
			Isolation:     &isolation,
			Path:          &result.WorktreePath,
			SessionID:     &scope.SessionID,
			ProjectHash:   &scope.ProjectHash,
			ManagedRoot:   &scope.ManagedRoot,
			WorktreeBranch: strPtrOrNil(result.WorktreeBranch),
			HeadCommit:    strPtrOrNil(result.HeadCommit),
		}, "", "")
	if !uok {
		return "", fmt.Errorf("member %s not found in team %s", memberName, teamName)
	}
	l.memberWorktreeInfo[memberName] = &MemberWorktreeInfo{
		WorktreePath:   result.WorktreePath,
		WorktreeName:   scope.WorktreeName,
		WorktreeBranch: result.WorktreeBranch,
		HeadCommit:     result.HeadCommit,
		HookBased:      result.HookBased,
		SessionID:      scope.SessionID,
		ProjectHash:    scope.ProjectHash,
		ManagedRoot:    scope.ManagedRoot,
	}
	logger.Info(logComponent).
		Str("member_name", memberName).
		Str("worktree_path", result.WorktreePath).
		Str("worktree_branch", result.WorktreeBranch).
		Msg("Created worktree for teammate")
	return result.WorktreePath, nil
}

// FinalizeNonContributingMemberWorktrees 收编无分支提交的本会话 worktree。
// Python: lifecycle.py finalize_non_contributing_member_worktrees (:400-402)
func (l *TeammateWorktreeLifecycle) FinalizeNonContributingMemberWorktrees(ctx context.Context) error {
	return l.finalizeAllMemberWorktrees(ctx)
}

// FinalizeAllMemberWorktreesForTeamClean 团队永久清理前收编全部 teammate worktree。
// Python: lifecycle.py finalize_all_member_worktrees_for_team_clean (:404-406)
func (l *TeammateWorktreeLifecycle) FinalizeAllMemberWorktreesForTeamClean(ctx context.Context) error {
	return l.removeAllCurrentSessionMemberWorktrees(ctx)
}

// MemberWorktreeInfoOf 宿主缓存读取（spawn 侧取 worktree 路径用；Python 无对应
// 导出面——缓存私有 dict，Go 供 spawn_manager 查询，登记 Python 侧无对应实现）。
func (l *TeammateWorktreeLifecycle) MemberWorktreeInfoOf(memberName string) *MemberWorktreeInfo {
	return l.memberWorktreeInfo[memberName]
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sessionDisablesTeammateWorktree 当前运行时上下文是否禁用成员 worktree。
// Python: lifecycle.py _session_disables_teammate_worktree (:81-89)
//
// 活载体 disable_teammate_worktree 优先，种子键次之。
func (l *TeammateWorktreeLifecycle) sessionDisablesTeammateWorktree() bool {
	spec := l.configurator.Spec()
	if spec == nil {
		return false
	}
	if spec.BuildContext != nil {
		if bc, ok := spec.BuildContext.GetExtras()["disable_teammate_worktree"].(bool); ok && bc {
			return true
		}
	}
	if seed, ok := spec.BuildContextSeed["disable_teammate_worktree"].(bool); ok && seed {
		return true
	}
	return false
}

// memberNeedsWorktree 成员是否应跑在 worktree 里。
// Python: lifecycle.py _member_needs_worktree(teammate, role) (:91-98)
func memberNeedsWorktree(role atschema.TeamRole, worktreeOptions *database.MemberWorktreeOptions) bool {
	if !role.IsCoordinatedMember() {
		return false
	}
	if worktreeOptions == nil || strDeref(worktreeOptions.Isolation) != "worktree" {
		return false
	}
	return true
}

// ownerScopeFor Resolve 调用方的 (ctx, teamName, memberName) 闭包签名。
func (l *TeammateWorktreeLifecycle) ownerScopeFor(ctx context.Context, teamName, memberName string) (WorktreeOwnerScope, error) {
	return l.ownerScope(ctx, teamName, memberName)
}

// ownerScope 构建当前会话/项目的所有者 scope。
// Python: lifecycle.py _owner_scope(team_name, member_name) (:116-121)
func (l *TeammateWorktreeLifecycle) ownerScope(ctx context.Context, teamName, memberName string) (WorktreeOwnerScope, error) {
	return BuildWorktreeOwnerScope(ctx, teamName, memberName, l.configurator.Spec(), nil)
}

// managerForScope 返回钉在 scope.ManagedRoot 上的 manager 视图。
// Python: lifecycle.py _get_worktree_manager (:100-114) + manager_for_worktree_scope
func (l *TeammateWorktreeLifecycle) managerForScope(scope WorktreeOwnerScope) (*worktree.WorktreeManager, error) {
	manager := l.configurator.WorktreeManager()
	if manager == nil {
		// Python :112 默认 WorktreeManager(WorktreeConfig(enabled=True))
		cfg := worktree.NewWorktreeConfig()
		cfg.Enabled = true
		manager = worktree.NewWorktreeManager(cfg, nil)
	}
	return ManagerForWorktreeScope(manager, scope)
}

// memberWorktreeOptions 从成员 DB 行读 worktree 选项（无 DB 成员行时 nil）。
// Python ensure_member_worktree 经 get_member_worktree(teammate)（:226）
func (l *TeammateWorktreeLifecycle) memberWorktreeOptions(memberName string) *database.MemberWorktreeOptions {
	teamBackend, teamName, ok := TeamContext(l.configurator)
	if !ok {
		return nil
	}
	teammate, err := memberDB(teamBackend).GetMember(context.Background(), memberName, teamName)
	if err != nil || teammate == nil {
		return nil
	}
	return database.GetMemberWorktree(teammate)
}

// finalizeAllMemberWorktrees 收编当前会话拥有的全部 teammate worktree。
// Python: lifecycle.py _finalize_all_member_worktrees (:408-416)
func (l *TeammateWorktreeLifecycle) finalizeAllMemberWorktrees(ctx context.Context) error {
	teamBackend, teamName, ok := TeamContext(l.configurator)
	if !ok {
		return nil
	}
	members, err := TeammateMemberNames(ctx, teamBackend, teamName)
	if err != nil {
		return err
	}
	for _, memberName := range members {
		if ferr := l.finalizeMemberWorktree(ctx, memberName); ferr != nil {
			return ferr
		}
	}
	return nil
}

// removeAllCurrentSessionMemberWorktrees 删除本团队拥有的全部 teammate worktree。
// Python: lifecycle.py _remove_all_current_session_member_worktrees (:418-425)
func (l *TeammateWorktreeLifecycle) removeAllCurrentSessionMemberWorktrees(ctx context.Context) error {
	teamBackend, teamName, ok := TeamContext(l.configurator)
	if !ok {
		return nil
	}
	members, err := TeammateMemberNames(ctx, teamBackend, teamName)
	if err != nil {
		return err
	}
	for _, memberName := range members {
		if rerr := l.removeCurrentSessionMemberWorktree(ctx, memberName); rerr != nil {
			return rerr
		}
	}
	return l.removeOrphanedTeamSessionWorktrees(ctx, teamName)
}

// removeOrphanedTeamSessionWorktrees 删除不再有 DB 成员元数据的团队 worktree。
// Python: lifecycle.py _remove_orphaned_team_session_worktrees (:427-481)
func (l *TeammateWorktreeLifecycle) removeOrphanedTeamSessionWorktrees(ctx context.Context, teamName string) error {
	sessionsRoot := agent_teams.TeamSessionsDir(teamName)
	st, err := os.Stat(sessionsRoot)
	if err != nil || !st.IsDir() {
		return nil
	}
	sessionsRootReal := realPath(sessionsRoot)
	manager, merr := l.managerForScope(WorktreeOwnerScope{ManagedRoot: sessionsRootReal})
	if merr != nil {
		// Python 无视图失败路径；清扫退回 base manager
		manager = l.configurator.WorktreeManager()
		if manager == nil {
			cfg := worktree.NewWorktreeConfig()
			cfg.Enabled = true
			manager = worktree.NewWorktreeManager(cfg, nil)
		}
	}
	sessionEntries, err := os.ReadDir(sessionsRoot)
	if err != nil {
		return nil
	}
	for _, sessionDir := range sessionEntries {
		worktreesRoot := filepath.Join(sessionsRoot, sessionDir.Name(), "worktrees")
		wst, err := os.Stat(worktreesRoot)
		if err != nil || !wst.IsDir() {
			continue
		}
		entries, err := os.ReadDir(worktreesRoot)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			worktreePath := filepath.Join(worktreesRoot, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			info, ierr := entry.Info()
			if ierr != nil || !info.IsDir() {
				continue
			}
			worktreeReal := realPath(worktreePath)
			// 越界防护：不在 session 根下的路径保留并告警（Python :445-456）
			if !isUnderRoot(worktreeReal, sessionsRootReal) {
				logger.Warn(logComponent).Str("worktree_path", worktreePath).
					Msg("Keeping team worktree path outside session root during team clean")
				continue
			}

			repoRoot, gerr := findCanonicalRoot(ctx, worktreePath)
			if gerr != nil || repoRoot == "" {
				_ = os.RemoveAll(worktreePath)
				logger.Info(logComponent).Str("worktree_path", worktreePath).
					Msg("Removed orphaned team worktree directory during team clean")
				continue
			}

			removed := manager.RemoveWorktreeForce(ctx, worktreePath, repoRoot)
			if removed {
				logger.Info(logComponent).Str("worktree_path", worktreePath).
					Msg("Removed orphaned team worktree during team clean")
			} else {
				logger.Warn(logComponent).Str("worktree_path", worktreePath).
					Msg("Failed to remove orphaned team worktree during team clean")
			}
		}
	}
	return nil
}

// removeCurrentSessionMemberWorktree 不做贡献检查直接删除一个本会话 teammate worktree。
// Python: lifecycle.py _remove_current_session_member_worktree (:483-527)
func (l *TeammateWorktreeLifecycle) removeCurrentSessionMemberWorktree(ctx context.Context, memberName string) error {
	resolved, err := ResolveCurrentSessionMemberWorktree(ctx, l.configurator,
		l.memberWorktreeInfo, memberName, l.ownerScopeFor, false)
	if err != nil {
		return err
	}
	if resolved == nil {
		return nil
	}
	worktreeInfo := resolved.WorktreeInfo

	if st, serr := os.Stat(worktreeInfo.WorktreePath); serr != nil || !st.IsDir() {
		return ClearMemberWorktreeMetadata(ctx, memberName, resolved, l.memberWorktreeInfo)
	}
	if worktreeInfo.HookBased {
		logger.Info(logComponent).
			Str("member_name", memberName).
			Str("worktree_path", worktreeInfo.WorktreePath).
			Msg("Keeping hook-based worktree for teammate during team clean")
		return nil
	}

	repoRoot, gerr := findCanonicalRoot(ctx, worktreeInfo.WorktreePath)
	if gerr != nil || repoRoot == "" {
		_ = os.RemoveAll(worktreeInfo.WorktreePath)
		if cerr := ClearMemberWorktreeMetadata(ctx, memberName, resolved, l.memberWorktreeInfo); cerr != nil {
			return cerr
		}
		logger.Info(logComponent).
			Str("member_name", memberName).
			Str("worktree_path", worktreeInfo.WorktreePath).
			Msg("Removed current-session worktree directory for teammate during team clean because git metadata could not be resolved")
		return nil
	}

	manager, err := l.managerForScope(resolved.Scope)
	if err != nil {
		return err
	}
	removed := manager.RemoveWorktreeForce(ctx, worktreeInfo.WorktreePath, repoRoot)
	if removed {
		if cerr := ClearMemberWorktreeMetadata(ctx, memberName, resolved, l.memberWorktreeInfo); cerr != nil {
			return cerr
		}
		logger.Info(logComponent).
			Str("member_name", memberName).
			Str("worktree_path", worktreeInfo.WorktreePath).
			Msg("Removed current-session worktree for teammate during team clean")
	}
	return nil
}

// finalizeMemberWorktree 收编一个干净的本会话 teammate worktree。
// Python: lifecycle.py _finalize_member_worktree (:297-398)
func (l *TeammateWorktreeLifecycle) finalizeMemberWorktree(ctx context.Context, memberName string) error {
	resolved, err := ResolveCurrentSessionMemberWorktree(ctx, l.configurator,
		l.memberWorktreeInfo, memberName, l.ownerScopeFor, true)
	if err != nil {
		return err
	}
	if resolved == nil {
		return nil
	}
	worktreeInfo := resolved.WorktreeInfo

	if st, serr := os.Stat(worktreeInfo.WorktreePath); serr != nil || !st.IsDir() {
		if cerr := ClearMemberWorktreeMetadata(ctx, memberName, resolved, l.memberWorktreeInfo); cerr != nil {
			return cerr
		}
		logger.Info(logComponent).
			Str("member_name", memberName).
			Msg("Cleared missing current-session worktree metadata for teammate")
		return nil
	}
	if worktreeInfo.HookBased {
		logger.Info(logComponent).
			Str("member_name", memberName).
			Str("worktree_path", worktreeInfo.WorktreePath).
			Msg("Keeping hook-based worktree for teammate")
		return nil
	}

	manager, err := l.managerForScope(resolved.Scope)
	if err != nil {
		return err
	}
	repoRoot, _ := findCanonicalRoot(ctx, worktreeInfo.WorktreePath)
	session := &worktree.WorktreeSession{
		OriginalCWD:        orDefault(repoRoot, worktreeInfo.WorktreePath),
		WorktreePath:       worktreeInfo.WorktreePath,
		WorktreeName:       worktreeInfo.WorktreeName,
		WorktreeBranch:     worktreeInfo.WorktreeBranch,
		OriginalHeadCommit: worktreeInfo.HeadCommit,
		MemberName:         memberName,
		TeamName:           resolved.TeamName,
	}
	summary := manager.CountChanges(ctx, session)
	if summary == nil {
		logger.Warn(logComponent).
			Str("member_name", memberName).
			Str("worktree_path", worktreeInfo.WorktreePath).
			Msg("Keeping worktree for teammate because changes could not be verified")
		return nil
	}

	changedFiles := summary.ChangedFiles
	removeForce := false
	if changedFiles > 0 {
		// 用户改动口径：剔除团队运行时文件/目录（Python :350-353）
		changedFiles = countUserChangedFiles(ctx, worktreeInfo.WorktreePath)
		removeForce = changedFiles == 0
	}
	if changedFiles > 0 {
		logger.Info(logComponent).
			Str("member_name", memberName).
			Int("changed_files", changedFiles).
			Str("worktree_path", worktreeInfo.WorktreePath).
			Str("worktree_branch", worktreeInfo.WorktreeBranch).
			Msg("Keeping worktree for teammate with changed files")
		return nil
	}

	if repoRoot == "" {
		logger.Warn(logComponent).
			Str("member_name", memberName).
			Str("worktree_path", worktreeInfo.WorktreePath).
			Msg("Keeping worktree for teammate because repo root could not be resolved")
		return nil
	}

	state := classifyWorktreeContribution(ctx, worktreeInfo, repoRoot)
	if state == WorktreeContributionUnknown {
		logger.Warn(logComponent).
			Str("member_name", memberName).
			Str("worktree_path", worktreeInfo.WorktreePath).
			Str("worktree_branch", worktreeInfo.WorktreeBranch).
			Msg("Keeping worktree for teammate because branch commit state could not be verified")
		return nil
	}
	if state == WorktreeContributionContributed {
		logger.Info(logComponent).
			Str("member_name", memberName).
			Str("worktree_path", worktreeInfo.WorktreePath).
			Str("worktree_branch", worktreeInfo.WorktreeBranch).
			Msg("Keeping worktree for teammate with branch commits while team lifecycle is still active")
		return nil
	}

	removed := manager.RemoveWorktreeForce(ctx, worktreeInfo.WorktreePath, repoRoot)
	if removeForce {
		// force 语义已并入 RemoveWorktreeForce
	}
	if removed {
		if cerr := ClearMemberWorktreeMetadata(ctx, memberName, resolved, l.memberWorktreeInfo); cerr != nil {
			return cerr
		}
		logger.Info(logComponent).
			Str("member_name", memberName).
			Str("worktree_path", worktreeInfo.WorktreePath).
			Msg("Removed clean current-session worktree for teammate")
	}
	return nil
}

// classifyWorktreeContribution 分类 worktree 分支的提交贡献。
// Python: lifecycle.py _classify_worktree_contribution (:149-203)
//
// head_commit 锚定基线；分支领先基线 → CONTRIBUTED；分支等于基线 →
// NOT_CONTRIBUTED；无法验证 → UNKNOWN（保守保留）。
func classifyWorktreeContribution(ctx context.Context, worktreeInfo *MemberWorktreeInfo, repoRoot string) WorktreeContributionState {
	if worktreeInfo.HeadCommit != "" {
		baseHead, _ := revParseStr(ctx, worktreeInfo.HeadCommit, repoRoot)
		if baseHead != "" {
			actualHead, _ := revParseStr(ctx, "HEAD", worktreeInfo.WorktreePath)
			if actualHead != "" {
				if actualHead != baseHead {
					isActualDescendant := isRefAncestorBool(ctx, worktreeInfo.HeadCommit, "HEAD", worktreeInfo.WorktreePath)
					if isActualDescendant == nil {
						return WorktreeContributionUnknown
					}
					if *isActualDescendant {
						return WorktreeContributionContributed
					}
				} else if worktreeInfo.WorktreeBranch == "" {
					return WorktreeContributionNotContributed
				}
			}
			if worktreeInfo.WorktreeBranch == "" {
				return WorktreeContributionUnknown
			}
			branchHead, _ := revParseStr(ctx, worktreeInfo.WorktreeBranch, repoRoot)
			if branchHead == "" {
				return WorktreeContributionUnknown
			}
			if branchHead == baseHead {
				return WorktreeContributionNotContributed
			}
			isDescendant := isRefAncestorBool(ctx, worktreeInfo.HeadCommit, worktreeInfo.WorktreeBranch, repoRoot)
			if isDescendant == nil {
				return WorktreeContributionUnknown
			}
			if *isDescendant {
				return WorktreeContributionContributed
			}
			return WorktreeContributionUnknown
		}
	}
	if worktreeInfo.WorktreeBranch == "" {
		return WorktreeContributionUnknown
	}
	branchHead, _ := revParseStr(ctx, worktreeInfo.WorktreeBranch, repoRoot)
	if branchHead == "" {
		return WorktreeContributionUnknown
	}
	fallbackBase := mergeBaseStr(ctx, "HEAD", worktreeInfo.WorktreeBranch, repoRoot)
	if fallbackBase == "" {
		return WorktreeContributionUnknown
	}
	if branchHead == fallbackBase {
		return WorktreeContributionNotContributed
	}
	return WorktreeContributionContributed
}

// countUserChangedFiles 统计剔除团队运行时产物后的用户改动文件数。
// Python: lifecycle.py _count_user_changed_files (:142-147)
func countUserChangedFiles(ctx context.Context, worktreePath string) int {
	changes, err := statusPorcelainStr(ctx, worktreePath)
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range changes {
		if !isTeamRuntimeStatus(line) {
			n++
		}
	}
	return n
}

// statusPath 从 git status --porcelain 行提取路径部分。
// Python: lifecycle.py _status_path (:124-130)
func statusPath(line string) string {
	var path string
	if len(line) > 3 {
		path = strings.TrimSpace(line[3:])
	}
	if idx := strings.LastIndex(path, " -> "); idx >= 0 {
		path = strings.TrimSpace(path[idx+len(" -> "):])
	}
	path = strings.Trim(path, "\"")
	return strings.TrimRight(path, "/")
}

// isTeamRuntimeStatus 该 porcelain 行是否团队运行时产物。
// Python: lifecycle.py _is_team_runtime_status (:132-140)
func isTeamRuntimeStatus(line string) bool {
	path := statusPath(line)
	if path == "" {
		return false
	}
	if teamRuntimeFiles[path] {
		return true
	}
	root := path
	if idx := strings.Index(path, "/"); idx >= 0 {
		root = path[:idx]
	}
	return teamRuntimeDirs[root]
}

// orDefault 空串回退。
func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// realPath Python os.path.realpath 的 Go 等价。
func realPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	return abs
}

// isUnderRoot path 是否在 root 下（commonpath 语义，不兼容根返回 false）。
// Python: os.path.commonpath(...) == root（ValueError → False 由调用处吞）
func isUnderRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// strPtrOrNil 空串转 nil 的字符串指针助手。
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// findCanonicalRoot findCanonicalGitRoot 的包内简化包装（返回空串表示无仓库）。
func findCanonicalRoot(ctx context.Context, cwdPath string) (string, error) {
	root, err := worktreeFindCanonicalGitRoot(ctx, cwdPath)
	if err != nil {
		return "", err
	}
	return root, nil
}

// isRefAncestorBool isRefAncestor 的包内包装（nil=无法验证）。
func isRefAncestorBool(ctx context.Context, ancestorRef, descendantRef, cwdPath string) *bool {
	return worktree.IsRefAncestor(ctx, ancestorRef, descendantRef, cwdPath)
}

// mergeBaseStr mergeBase 的包内包装（空串=无法判定）。
func mergeBaseStr(ctx context.Context, firstRef, secondRef, cwdPath string) string {
	return worktree.MergeBase(ctx, firstRef, secondRef, cwdPath)
}

// revParseStr revParse 的包内包装（空串=无法解析）。
func revParseStr(ctx context.Context, ref, cwdPath string) (string, error) {
	return worktree.RevParse(ctx, ref, cwdPath)
}

// statusPorcelainStr statusPorcelain 的包内包装。
func statusPorcelainStr(ctx context.Context, cwdPath string) ([]string, error) {
	return worktree.StatusPorcelain(ctx, cwdPath)
}

// worktreeFindCanonicalGitRoot harness 包导出面的转调。
func worktreeFindCanonicalGitRoot(ctx context.Context, cwdPath string) (string, error) {
	return worktree.FindCanonicalGitRoot(ctx, cwdPath)
}
