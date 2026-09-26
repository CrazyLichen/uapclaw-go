package agent

import (
	"context"
	"encoding/json"
	"fmt"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LiveTeammate 活跃队友快照（成员名 + 当前状态）。
// Python: collect_live_teammates_for_session_switch 返回的 list[tuple[str, MemberStatus]]
type LiveTeammate struct {
	// MemberName 成员名
	MemberName string
	// Status 当前 DB 状态
	Status atschema.MemberStatus
}

// RecoveryManager 团队恢复管理器。
// 负责：团队恢复、成员状态转换、Leader 配置持久化、Allocator 状态持久化。
//
// Python: openjiuwen/agent_teams/agent/recovery_manager.py
type RecoveryManager struct {
	// configurator Agent 配置器
	configurator *AgentConfigurator
	// spawnManager 子进程管理器
	spawnManager *SpawnManager
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// rmLogComponent 日志组件
	rmLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRecoveryManager 创建恢复管理器实例。
// Python: RecoveryManager.__init__(configurator, spawn_manager)
func NewRecoveryManager(configurator *AgentConfigurator, spawnManager *SpawnManager) *RecoveryManager {
	return &RecoveryManager{
		configurator: configurator,
		spawnManager: spawnManager,
	}
}

// RecoverTeam 从数据库状态恢复团队，返回成功重启的成员名列表。
// Python: async recover_team(self) -> list[str]
func (m *RecoveryManager) RecoverTeam(ctx context.Context) []string {
	// Python: team_backend = self._configurator.team_backend; if not team_backend: return []
	teamBackend := m.configurator.TeamBackend()
	if teamBackend == nil {
		return []string{}
	}

	memberName := m.configurator.MemberName()
	if memberName == "" {
		memberName = "?"
	}
	// Python: team_logger.info("[{}] recovering team", member_name or "?")
	logger.Info(rmLogComponent).
		Str("member_name", memberName).
		Msg("recovering team")

	// 重建 HITT 名册缓存（冷启动 Leader 后缓存为空）
	// Python 注释：其他 sync HITT 消费者在冷启动后仍读缓存，需在 restart_teammate 扇出前重建
	// Python: await team_backend.refresh_human_agent_roster()
	teamBackend.RefreshHumanAgentRoster(ctx)

	// 获取所有成员
	// Python: all_members = await team_backend.list_members()
	allMembers, err := teamBackend.ListMembers(ctx)
	if err != nil {
		logger.Error(rmLogComponent).Err(err).Msg("recover_team: list_members 失败")
		return []string{}
	}

	restarted := []string{}
	teamName := m.configurator.TeamName()

	for _, member := range allMembers {
		// Python: if member.member_name == member_name: continue
		if member.MemberName == m.configurator.MemberName() {
			continue
		}

		// Python: if team_name: await team_backend.db.member.update_member_status(
		//     member.member_name, team_name, MemberStatus.RESTARTING.value)
		if teamName != "" {
			teamBackend.DB().Member().UpdateMemberStatus(
				ctx, member.MemberName, teamName, string(atschema.MemberStatusRestarting),
			)
		}

		// Python: if await self._spawn_manager.restart_teammate(member.member_name):
		//     restarted.append(member.member_name)
		if err := m.spawnManager.RestartTeammate(ctx, member.MemberName, defaultMaxRetries); err == nil {
			restarted = append(restarted, member.MemberName)
		}
	}

	return restarted
}

// PersistLeaderConfig 持久化 Leader 配置到 team namespace。
// Python: persist_leader_config(self, session) -> None
func (m *RecoveryManager) PersistLeaderConfig(sess interfaces.SessionFacade) {
	spec := m.configurator.Spec()
	runtimeCtx := m.configurator.RuntimeContext()
	teamName := m.configurator.TeamName()

	// Python: if spec is None or ctx is None or team_name is None: return
	if spec == nil || runtimeCtx == nil || teamName == "" {
		return
	}

	// 构建 payload
	// Python: payload = {"spec": spec.model_dump(mode="json"), "context": ctx.model_dump(mode="json"), ...}
	specBytes, _ := json.Marshal(spec)
	ctxBytes, _ := json.Marshal(runtimeCtx)

	var specMap map[string]any
	var ctxMap map[string]any
	_ = json.Unmarshal(specBytes, &specMap)
	_ = json.Unmarshal(ctxBytes, &ctxMap)

	payload := map[string]any{
		"spec":    specMap,
		"context": ctxMap,
	}

	// Python: TEAM_DB_STATE_KEY: read_team_db_state(session, team_name) or TEAM_DB_STATE_PENDING_CREATE
	dbState := metadata.ReadTeamDBState(sess, teamName)
	if dbState == "" {
		dbState = metadata.TeamDBStatePendingCreate
	}
	payload[metadata.TeamDBStateKey] = dbState

	// Python: if allocator is not None: payload["model_allocator_state"] = allocator.state_dict()
	allocator := m.configurator.ModelAllocator()
	if allocator != nil {
		payload["model_allocator_state"] = allocator.StateDict()
	}

	// Python: write_team_namespace(session, team_name, payload)
	metadata.WriteTeamNamespace(sess, teamName, payload)
}

// MarkTeammateRestartingForSessionSwitch 将活跃队友的状态转为 RESTARTING。
// READY/BUSY/SHUTDOWN_REQUESTED/UNSTARTED 需先经 ERROR 中转。
//
// Python: _mark_teammate_restarting_for_session_switch(self, member_name, current_status) -> bool
// Go 中导出以便同包测试（agent_test 包）访问。
func (m *RecoveryManager) MarkTeammateRestartingForSessionSwitch(
	ctx context.Context,
	memberName string,
	currentStatus atschema.MemberStatus,
) bool {
	// Python: team_backend = self._configurator.team_backend; if not team_backend: return False
	teamBackend := m.configurator.TeamBackend()
	if teamBackend == nil {
		return false
	}
	// Python: team_name = self._configurator.team_name; if team_name is None: return False
	teamName := m.configurator.TeamName()
	if teamName == "" {
		return false
	}
	db := teamBackend.DB()

	// Python: if current_status == MemberStatus.RESTARTING: return True
	if currentStatus == atschema.MemberStatusRestarting {
		return true
	}

	// 可直接转换到 RESTARTING 的状态集合
	// Python: directly_restartable = {PAUSED, STOPPED, ERROR, SHUTDOWN}
	directlyRestartable := map[atschema.MemberStatus]bool{
		atschema.MemberStatusPaused:   true,
		atschema.MemberStatusStopped:  true,
		atschema.MemberStatusError:    true,
		atschema.MemberStatusShutdown: true,
	}

	// READY/BUSY/SHUTDOWN_REQUESTED/UNSTARTED 需先经 ERROR 中转
	// Python: if current_status not in directly_restartable:
	if !directlyRestartable[currentStatus] {
		// Python: updated = await db.member.update_member_status(member_name, team_name, MemberStatus.ERROR.value)
		updated := db.Member().UpdateMemberStatus(
			ctx, memberName, teamName, string(atschema.MemberStatusError),
		)
		if !updated {
			// Python: team_logger.warning("Failed to move teammate {} from {} to ERROR before session rebind", ...)
			logger.Warn(rmLogComponent).
				Str("member_name", memberName).
				Str("current_status", string(currentStatus)).
				Msg("Failed to move teammate from current_status to ERROR before session rebind")
			return false
		}
		currentStatus = atschema.MemberStatusError
	}

	// Python: if current_status == MemberStatus.RESTARTING: return True
	if currentStatus == atschema.MemberStatusRestarting {
		return true
	}

	// Python: updated = await db.member.update_member_status(member_name, team_name, MemberStatus.RESTARTING.value)
	updated := db.Member().UpdateMemberStatus(
		ctx, memberName, teamName, string(atschema.MemberStatusRestarting),
	)
	if !updated {
		// Python: team_logger.warning("Failed to move teammate {} into RESTARTING during session rebind", member_name)
		logger.Warn(rmLogComponent).
			Str("member_name", memberName).
			Msg("Failed to move teammate into RESTARTING during session rebind")
		return false
	}
	return true
}

// CollectLiveTeammatesForSessionSwitch 快照需要 session 切换时重新绑定的活跃队友。
// 仅 Leader 角色且 teamBackend 存在时执行。
// Python: collect_live_teammates_for_session_switch(self) -> list[tuple[str, MemberStatus]]
func (m *RecoveryManager) CollectLiveTeammatesForSessionSwitch(ctx context.Context) []LiveTeammate {
	// Python: if self._configurator.role != TeamRole.LEADER or not team_backend: return []
	if m.configurator.Role() != atschema.TeamRoleLeader || m.configurator.TeamBackend() == nil {
		return nil
	}

	teamBackend := m.configurator.TeamBackend()
	// Python: members = await team_backend.list_members()
	members, err := teamBackend.ListMembers(ctx)
	if err != nil {
		logger.Error(rmLogComponent).Err(err).Msg("collect_live_teammates: list_members 失败")
		return nil
	}

	leaderMemberName := m.configurator.MemberName()
	// Python: spawned = self._spawn_manager.spawned_handles
	spawned := m.spawnManager.SpawnedHandles()

	// 构建 live 集合：有运行时句柄 + 非 Leader
	// Python: live_teammates = {name for name, handle in spawned.items() if name != leader and handle is not None}
	liveTeammates := make(map[string]struct{})
	for name, handle := range spawned {
		if name != leaderMemberName && handle != nil {
			liveTeammates[name] = struct{}{}
		}
	}

	result := []LiveTeammate{}
	// 排除终态
	// Python: if member_status in {UNSTARTED, SHUTDOWN, STOPPED}: continue
	excludedStatuses := map[atschema.MemberStatus]bool{
		atschema.MemberStatusUnstarted: true,
		atschema.MemberStatusShutdown:  true,
		atschema.MemberStatusStopped:   true,
	}

	for _, member := range members {
		if _, ok := liveTeammates[member.MemberName]; !ok {
			continue
		}
		// Python: member_status = MemberStatus(member.status)
		memberStatus := atschema.MemberStatus(member.Status)
		if excludedStatuses[memberStatus] {
			continue
		}
		result = append(result, LiveTeammate{
			MemberName: member.MemberName,
			Status:     memberStatus,
		})
	}

	return result
}

// RestartForSessionSwitch 在 session 切换后重启指定队友。
// cleanupFirst=true 时先清理旧 handle 再重启；false 表示协调层已清理。
// Python: restart_for_session_switch(self, recoverable_members, *, cleanup_first) -> None
func (m *RecoveryManager) RestartForSessionSwitch(
	ctx context.Context,
	recoverableMembers []LiveTeammate,
	cleanupFirst bool,
) {
	for _, lt := range recoverableMembers {
		// Python: if cleanup_first: await self._spawn_manager.cleanup_teammate(member_name)
		if cleanupFirst {
			m.spawnManager.CleanupTeammate(ctx, lt.MemberName)
		}

		// Python: if not await self._mark_teammate_restarting_for_session_switch(member_name, member_status): continue
		if !m.MarkTeammateRestartingForSessionSwitch(ctx, lt.MemberName, lt.Status) {
			continue
		}

		// Python: await self._spawn_manager.restart_teammate(member_name)
		// 对齐 Python：不抛异常，忽略失败
		_ = m.spawnManager.RestartTeammate(ctx, lt.MemberName, defaultMaxRetries)
	}
}

// PersistAllocatorState 持久化 allocator 状态到 team namespace。
// Python: persist_allocator_state(self, team_session) -> None
func (m *RecoveryManager) PersistAllocatorState(sess interfaces.SessionFacade) {
	allocator := m.configurator.ModelAllocator()
	teamName := m.configurator.TeamName()
	memberName := m.configurator.MemberName()
	if memberName == "" {
		memberName = "?"
	}

	// Python: if team_session is None or allocator is None or team_name is None: return
	if sess == nil || allocator == nil || teamName == "" {
		return
	}

	// Python: try: merge_team_namespace(session, team_name, {"model_allocator_state": allocator.state_dict()})
	// Python: except Exception as e: team_logger.error("[{}] failed to persist allocator state: {}", ...)
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error(rmLogComponent).
					Str("member_name", memberName).
					Str("error", fmt.Sprintf("%v", r)).
					Msg("failed to persist allocator state")
			}
		}()
		metadata.MergeTeamNamespace(sess, teamName, map[string]any{
			"model_allocator_state": allocator.StateDict(),
		})
	}()
}

// ──────────────────────────── 非导出函数 ────────────────────────────
