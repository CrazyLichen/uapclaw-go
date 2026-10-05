package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/registry"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	runnerspawn "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamRuntimeManager 团队运行时管理器。
// Python: TeamRuntimeManager (openjiuwen/agent_teams/runtime/manager.py)
//
// 持有进程内 TeamRuntimePool，分发每个 run_agent_team_streaming 调用
// 到四条恢复路径之一（或拒绝）。池条目是"哪些团队当前活跃"的唯一事实来源。
type TeamRuntimeManager struct {
	// pool 活跃团队运行时池
	pool *TeamRuntimePool
}

// TeamRuntimeActivation 激活结果。
// Python: TeamRuntimeActivation (openjiuwen/agent_teams/runtime/manager.py)
type TeamRuntimeActivation struct {
	// Agent 激活的 TeamAgent 实例
	Agent *agent.TeamAgent
	// Session 绑定的 AgentTeamSession
	Session *session.AgentTeamSession
	// Action 调度决策结果
	Action RunAction
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// mgrLogComponent 日志组件
	mgrLogComponent = logger.ComponentChannel
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// globalTeamRuntimeManager 全局 TeamRuntimeManager 单例
	// 对齐 Python: _runner_team_runtime_manager(runner)
	globalTeamRuntimeManager *TeamRuntimeManager
	// globalTeamRuntimeMu 保护全局单例
	globalTeamRuntimeMu sync.Mutex
	// memberFinalizedStatuses 成员已终结状态集合
	// Python: _MEMBER_FINALIZED_STATUSES = frozenset({STOPPED, PAUSED, SHUTDOWN})
	memberFinalizedStatuses = map[atschema.MemberStatus]bool{
		atschema.MemberStatusStopped:  true,
		atschema.MemberStatusPaused:   true,
		atschema.MemberStatusShutdown: true,
	}
)

// 编译期检查：确保 TeamRuntimeManager 满足 registry.PoolAccessor
var _ registry.PoolAccessor = (*TeamRuntimeManager)(nil)

// 编译期检查：确保 TeamRuntimeManager 满足 registry.SessionReleaser
var _ registry.SessionReleaser = (*TeamRuntimeManager)(nil)

// 编译期检查：确保 TeamRuntimeManager 满足 registry.TeamSessionChecker
var _ registry.TeamSessionChecker = (*TeamRuntimeManager)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetTeamRuntimeManager 获取全局 TeamRuntimeManager（懒创建）。
// 对齐 Python _runner_team_runtime_manager(runner)：首次访问时创建并绑定到 Runner。
// 由于循环依赖，Runner 中存储为 any 类型，本函数负责创建和类型安全的获取。
func GetTeamRuntimeManager() *TeamRuntimeManager {
	globalTeamRuntimeMu.Lock()
	defer globalTeamRuntimeMu.Unlock()
	if globalTeamRuntimeManager == nil {
		globalTeamRuntimeManager = NewTeamRuntimeManager()
		// 同步到 Runner.teamRuntimeManager（any 类型）
		runner.SetTeamRuntimeManager(globalTeamRuntimeManager)
		// 注入 RunTeamMember 函数到 spawn 包（打破 spawn ↔ runtime 循环依赖）
		spawn.SetRunTeamMemberFunc(RunTeamMember)
		// 注入 TeamRunner 函数到 runner/spawn 包（打破循环依赖）
		runnerspawn.SetTeamRunnerFunc(RunAgentTeam)
	}
	return globalTeamRuntimeManager
}

// NewTeamRuntimeManager 创建运行时管理器。
func NewTeamRuntimeManager() *TeamRuntimeManager {
	return &TeamRuntimeManager{
		pool: NewTeamRuntimePool(),
	}
}

// Pool 返回运行时池（具体类型）。
func (m *TeamRuntimeManager) Pool() *TeamRuntimePool {
	return m.pool
}

// PoolReader 返回运行时池的最小读取+移除接口（满足 registry.PoolAccessor）。
func (m *TeamRuntimeManager) PoolReader() registry.PoolReader {
	return m.pool
}

// Interact 路由交互载荷通过活跃团队的门控。
// Python: TeamRuntimeManager.interact(payload, *, team_name, session_id)
//
// Python 执行步骤：
//  1. entry = await self._resolve_entry(team_name=team_name, session_id=session_id)
//  2. if entry is None: return DeliverResult.failure("not_active")
//  3. if isinstance(payload, InteractiveInput):
//     a. if entry.agent.has_pending_interrupt(): await entry.agent.resume_interrupt(payload); return success
//     b. return DeliverResult.failure("unsupported_interactive_input")
//  4. if isinstance(payload, str): parsed = parse_interact_str(payload)
//  5. payloads = parsed or [GodViewMessage(body=payload)]
//  6. ticket = await entry.interact_gate.admit()
//  7. if ticket is None: return DeliverResult.failure("gate_closed")
//  8. try:
//     a. payloads = await self._resolve_recipients(entry.agent, payloads)
//     b. last_result = DeliverResult.success(None)
//     c. for entry_payload in payloads:
//     - last_result = await self._dispatch_payload(entry.agent, entry_payload)
//     - if not last_result.ok: return last_result
//     d. return last_result
//  9. finally: await entry.interact_gate.consume_done(ticket)
//
// 接受三种输入类型：
//   - *sessioninteraction.InteractiveInput → 恢复中断
//   - string → ParseInteractStr → payloads
//   - interaction.InteractPayload → 直接分发
func (m *TeamRuntimeManager) Interact(
	ctx context.Context,
	payload any,
	teamName string,
	sessionID string,
) (*interaction.DeliverResult, error) {
	// Python 步骤 1-2
	entry := m.resolveEntry(teamName, sessionID)
	if entry == nil {
		return interaction.NewDeliverResultFailure("not_active"), nil
	}

	// Python 步骤 3: InteractiveInput → 恢复中断
	if interactiveInput, ok := payload.(*sessioninteraction.InteractiveInput); ok {
		return m.handleInteractiveInput(ctx, entry, interactiveInput)
	}

	// Python 步骤 4-5: 解析 payloads
	var payloads []interaction.InteractPayload
	if strPayload, ok := payload.(string); ok {
		parsed := interaction.ParseInteractStr(strPayload)
		if len(parsed) == 0 {
			payloads = []interaction.InteractPayload{interaction.NewGodViewMessage(strPayload)}
		} else {
			payloads = parsed
		}
	} else if interactPayload, ok := payload.(interaction.InteractPayload); ok {
		payloads = []interaction.InteractPayload{interactPayload}
	} else {
		return interaction.NewDeliverResultFailure("unsupported_payload_type"), nil
	}

	// Python 步骤 6-7: admit
	ticket := entry.InteractGate.Admit()
	if ticket == nil {
		return interaction.NewDeliverResultFailure("gate_closed"), nil
	}

	// Python 步骤 9: finally consume_done
	defer entry.InteractGate.ConsumeDone(ticket)

	// Python 步骤 8a: resolve_recipients
	resolved, err := m.resolveRecipients(ctx, entry, payloads)
	if err != nil {
		return nil, err
	}

	// Python 步骤 8b-8d: 逐个分发
	var lastResult *interaction.DeliverResult
	for _, p := range resolved {
		lastResult, err = m.dispatchPayload(ctx, entry, p)
		if err != nil {
			return nil, err
		}
		if !lastResult.IsOK() {
			return lastResult, nil
		}
	}
	if lastResult == nil {
		lastResult = interaction.NewDeliverResultSuccess(nil)
	}
	return lastResult, nil
}

// Activate 激活团队（完整实现，内联回填 #9.62）。
// Python: TeamRuntimeManager.activate(spec, session, inputs)
//
// 执行步骤（对齐 Python manager.py L108-169）：
//  1. buildTeamSession → 创建 AgentTeamSession
//  2. pool.Get → 查看是否有活跃池条目
//  3. 跨 session 策略：若池条目 session 不匹配，stop_team + 清空 poolEntry
//  4. inspectSession → 检查 DB 和 session 中团队状态
//  5. DecideRunAction → 纯调度决策
//  6. applyAction → 执行决策副作用
func (m *TeamRuntimeManager) Activate(
	ctx context.Context,
	spec *atschema.TeamAgentSpec,
	sess any,
	inputs any,
) (*TeamRuntimeActivation, error) {
	// Python 步骤 1: _build_session(spec, session)
	teamSession := buildTeamSession(spec, sess)
	targetSessionID := teamSession.GetSessionID()
	teamName := spec.TeamName

	// Python 步骤 2: pool_entry = await self._pool.get(team_name)
	poolEntry := m.pool.Get(teamName)

	// Python 步骤 3: 跨 session 策略
	// Python: L130-137 — 任何不同 session 的池条目在 dispatch 前被拆除，
	// 折叠旧的 WARM_RECOVER / NEW_TEAM_IN_SESSION_WARM 路径为 stop+remove+cold-rebuild。
	if poolEntry != nil && poolEntry.SessionID != targetSessionID {
		logger.Info(mgrLogComponent).
			Str("team_name", teamName).
			Str("old_session_id", poolEntry.SessionID).
			Str("new_session_id", targetSessionID).
			Msg("activate: 池条目 session 不匹配，stop+remove 后重建")
		_, _ = m.StopTeam(ctx, teamName, poolEntry.SessionID)
		poolEntry = nil
	}

	// Python 步骤 4: _inspect_session(spec, team_session, team_name)
	teamInSession, teamInDB, teamDBState, err := m.inspectSession(ctx, spec, teamSession, teamName)
	if err != nil {
		return nil, err
	}

	// Python 步骤 5: decide_run_action(...)
	action := DecideRunAction(
		teamInDB, teamInSession,
		poolEntry,
		targetSessionID, teamName,
		teamDBState,
	)

	// Python: L152-158 — 日志记录调度结果
	logger.Info(mgrLogComponent).
		Str("team_name", teamName).
		Str("session_id", targetSessionID).
		Str("action_kind", string(action.Kind)).
		Bool("in_db", teamInDB).
		Bool("in_session", teamInSession).
		Str("db_state", teamDBState).
		Bool("pooled", poolEntry != nil).
		Msg("activate: 调度决策完成")

	// Python 步骤 6: _apply_action(action, spec, team_session, pool_entry, inputs)
	return m.applyAction(ctx, action, spec, teamSession, poolEntry, inputs)
}

// Finalize 终结团队运行：选择暂停或停止。
// Python: TeamRuntimeManager.finalize(team_name, session_id)
//
// 决策规则：
//   - shutdown_requested（teammate 明确请求离开）→ 停止
//   - agent.lifecycle != "persistent" → 停止
//   - 否则 → 暂停
func (m *TeamRuntimeManager) Finalize(ctx context.Context, teamName string, sessionID string) error {
	entry := m.resolveEntry(teamName, sessionID)
	if entry == nil {
		logger.Debug(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
			Msg("finalize: 无池条目，无操作")
		return nil
	}
	ag := entry.Agent
	shutdownRequested, err := ag.IsShutdownRequested(ctx)
	if err != nil {
		logger.Warn(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
			Err(err).Msg("finalize: 检查 shutdown_requested 失败，默认暂停")
	}
	if shutdownRequested || ag.Lifecycle() != "persistent" {
		logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
			Bool("shutdown_requested", shutdownRequested).
			Str("lifecycle", ag.Lifecycle()).
			Msg("finalize: 停止团队")
		if stopErr := ag.StopCoordination(ctx); stopErr != nil {
			logger.Warn(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
				Err(stopErr).Msg("finalize: StopCoordination 失败")
		}
		m.pool.Remove(teamName)
	} else {
		logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
			Msg("finalize: 暂停持久化团队")
		if pauseErr := ag.PauseCoordination(ctx); pauseErr != nil {
			logger.Warn(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
				Err(pauseErr).Msg("finalize: PauseCoordination 失败")
		}
		entry.State = RuntimeStatePaused
	}
	return nil
}

// Pause 暂停团队。
// Python: TeamRuntimeManager.pause(team_name, session_id)
//
// 返回 false 表示池中无匹配条目，否则暂停成功（幂等：已暂停的条目再次暂停是 no-op）。
func (m *TeamRuntimeManager) Pause(ctx context.Context, teamName string, sessionID string) (bool, error) {
	entry := m.resolveEntry(teamName, sessionID)
	if entry == nil {
		logger.Debug(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
			Msg("pause: 无池条目，无操作")
		return false, nil
	}
	if err := entry.Agent.PauseCoordination(ctx); err != nil {
		return false, err
	}
	entry.State = RuntimeStatePaused
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("pause: 团队已暂停")
	return true, nil
}

// StopTeam 停止团队，保留持久化数据。
// Python: TeamRuntimeManager.stop_team(team_name, session_id)
//
// StopCoordination 失败时仅记录警告（best-effort），仍从池中移除。
func (m *TeamRuntimeManager) StopTeam(ctx context.Context, teamName string, sessionID string) (bool, error) {
	entry := m.resolveEntry(teamName, sessionID)
	if entry == nil {
		logger.Debug(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
			Msg("stop_team: 无池条目，无操作")
		return false, nil
	}
	// Python: try: await entry.agent.stop_coordination() except: logger.warning(...)
	if err := entry.Agent.StopCoordination(ctx); err != nil {
		logger.Warn(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
			Err(err).Msg("stop_team: 停止协调失败，继续从池移除")
	}
	m.pool.Remove(teamName)
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("stop_team: 团队已停止并从池移除")
	return true, nil
}

// DeleteTeam 删除团队运行时状态、checkpoint、持久化元数据和文件系统目录。
// Python: TeamRuntimeManager.delete_team(team_name, session_ids, force=False)
//
// 清理步骤（按顺序）：
//  1. force=False 时拒绝活跃条目；force=True 时先 stop_team
//  2. 检查 session checkpoint 是否存在
//  3. 从 session bucket 解析 db_config
//  4. Drop session 动态表
//  5. Release session checkpoint
//  6. Delete team_info 行（级联删 members/tasks/messages）
//  7. Remove team_home 目录
func (m *TeamRuntimeManager) DeleteTeam(ctx context.Context, teamName string, sessionIDs []string, force bool) (bool, error) {
	// 步骤 1: force 守卫
	if m.pool.HasActive(teamName) {
		entry := m.pool.Get(teamName)
		activeSession := ""
		if entry != nil {
			activeSession = entry.SessionID
		}
		if !force {
			return false, fmt.Errorf("team %s has an active runtime on session %s; stop_team before delete_team or pass force=true", teamName, activeSession)
		}
		logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", activeSession).
			Msg("delete_team(force=true): 先停止活跃运行时")
		if entry != nil {
			_, _ = m.StopTeam(ctx, teamName, activeSession)
		}
	}

	// 步骤 2: 检查 session checkpoint 是否存在
	cp := getCheckpointer()
	var existingSessionIDs []string
	if len(sessionIDs) > 0 {
		for _, sid := range sessionIDs {
			if sessionExists(ctx, sid) {
				existingSessionIDs = append(existingSessionIDs, sid)
			}
		}
		if len(existingSessionIDs) == 0 {
			logger.Info(mgrLogComponent).Str("team_name", teamName).Strs("session_ids", sessionIDs).
				Msg("delete_team: 提供的 session 均已释放，直接返回")
			return true, nil
		}
	}

	// 步骤 3: 解析 db_config
	var dbConfig *database.DatabaseConfig
	if len(existingSessionIDs) > 0 {
		releaseInfo := resolveAnyTeamSessionReleaseInfo(existingSessionIDs)
		if releaseInfo == nil {
			return false, fmt.Errorf("cannot resolve team session release info for any supplied sessions: %v, aborting delete_team", existingSessionIDs)
		}
		dbConfig = &releaseInfo.DBConfig
	} else if len(sessionIDs) == 0 {
		// 无 sessionIDs 时使用默认 db_config
		defaultCfg := database.NewDatabaseConfig()
		dbConfig = &defaultCfg
	}

	// 步骤 4: Drop session 动态表
	if dbConfig != nil && len(sessionIDs) > 0 {
		db := spawn.GetSharedDB(*dbConfig)
		if db != nil {
			if initErr := db.Initialize(ctx); initErr != nil {
				logger.Warn(mgrLogComponent).Err(initErr).Str("team_name", teamName).
					Msg("delete_team: 数据库初始化失败")
			} else {
				for _, sid := range sessionIDs {
					if _, dropErr := db.DropSessionTablesByID(ctx, sid); dropErr != nil {
						logger.Warn(mgrLogComponent).Str("session_id", sid).Err(dropErr).
							Msg("delete_team: 删除会话动态表失败")
					}
				}
			}
		}
	}

	// 步骤 5: Release session checkpoint
	if cp != nil {
		for _, sid := range sessionIDs {
			if relErr := cp.Release(ctx, sid); relErr != nil {
				logger.Warn(mgrLogComponent).Str("session_id", sid).Err(relErr).
					Msg("delete_team: 释放 checkpoint 失败")
			}
		}
	}

	// 步骤 6: Delete team_info 行（级联删 members/tasks/messages）
	var deleted bool
	if dbConfig != nil {
		deleted = deleteTeamDB(ctx, *dbConfig, teamName)
	}

	// 步骤 7: Remove team_home 目录
	removeTeamDir(teamName)

	logger.Info(mgrLogComponent).Str("team_name", teamName).Bool("deleted", deleted).
		Msg("delete_team: 完成")
	return deleted, nil
}

// ReleaseSession 释放指定 session 的动态表。
// Python: TeamRuntimeManager.release_session(session_id, force=False)
//
// force=False 时拒绝有活跃团队的 session；force=True 时先停止再清理。
// 注意：ReleaseSession 不释放 checkpoint，也不删除 team_info 行和 FS 目录。
func (m *TeamRuntimeManager) ReleaseSession(ctx context.Context, sessionID string, force bool) error {
	if sessionID == "" {
		return nil
	}

	// force 守卫
	activeTeams := m.pool.TeamsForSession(sessionID)
	if len(activeTeams) > 0 {
		if !force {
			var blockedNames []string
			for _, t := range activeTeams {
				blockedNames = append(blockedNames, t.TeamName)
			}
			return fmt.Errorf("team(s) [%s] active on session %s; stop_team or pass force=true", strings.Join(blockedNames, ", "), sessionID)
		}
		for _, team := range activeTeams {
			logger.Info(mgrLogComponent).Str("team_name", team.TeamName).Str("session_id", sessionID).
				Msg("release_session(force=true): 停止活跃团队")
			_, _ = m.StopTeam(ctx, team.TeamName, sessionID)
		}
	}

	// 解析 db_config
	releaseInfo, err := ResolveTeamSessionReleaseInfo(sessionID)
	if err != nil {
		return fmt.Errorf("cannot resolve team session release info for %s: %w", sessionID, err)
	}
	if releaseInfo == nil {
		return fmt.Errorf("cannot resolve team session release info for %s: no team bucket found", sessionID)
	}

	// Drop session 动态表
	return dropSessionTablesForSession(ctx, sessionID, releaseInfo.DBConfig)
}

// IsTeamSession 判断指定 session 是否为团队会话（包含持久化的团队 bucket）。
// Python: TeamRuntimeManager.resolve_team_session_release_info(session_id) is not None
// 实现 registry.TeamSessionChecker 接口，供 runner 包调用。
func (m *TeamRuntimeManager) IsTeamSession(sessionID string) bool {
	info, err := ResolveTeamSessionReleaseInfo(sessionID)
	if err != nil {
		return false
	}
	return info != nil
}

// FinalizeMember 终结 teammate/human-agent（非 leader）。
// Python: TeamRuntimeManager.finalize_member(agent) (manager.py L244-309)
//
// 决策规则（对齐 Python）：
//   - 已终结（STOPPED/PAUSED/SHUTDOWN）→ 仅关闭 kernel
//   - SHUTDOWN_REQUESTED → stop + mark SHUTDOWN
//   - 默认 → pause + mark READY（下次可被分配）
func FinalizeMember(ctx context.Context, ag *agent.TeamAgent) error {
	member := ag.TeamMemberHandle()
	memberName := ag.MemberName()

	// Python: try/except 外层
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Debug(mgrLogComponent).
					Str("member_name", memberName).
					Any("recover", r).
					Msg("finalize_member: 读取状态异常")
			}
		}()

		// Python: L268-274 — 读取 member status
		var currentStatus atschema.MemberStatus
		var hasStatus bool
		if member != nil {
			status, err := member.Status(ctx)
			if err == nil {
				currentStatus = status
				hasStatus = true
			} else {
				// Python: L272-274 — logger.debug("Failed to read...")
				logger.Debug(mgrLogComponent).
					Str("member_name", memberName).
					Err(err).
					Msg("finalize_member: 读取 member 状态失败")
			}
		}

		// Python: L275-277 — already_finalized
		if hasStatus && memberFinalizedStatuses[currentStatus] {
			logger.Info(mgrLogComponent).
				Str("member_name", memberName).
				Str("status", string(currentStatus)).
				Msg("finalize_member: 已终结，仅关闭 kernel")
			_ = ag.StopCoordination(ctx)
			return
		}

		// Python: L282-286 — SHUTDOWN_REQUESTED
		if hasStatus && currentStatus == atschema.MemberStatusShutdownRequested {
			logger.Info(mgrLogComponent).
				Str("member_name", memberName).
				Msg("finalize_member: 按请求关闭")
			_ = ag.StopCoordination(ctx)
			if member != nil {
				_, _ = member.UpdateStatus(ctx, atschema.MemberStatusShutdown)
			}
			return
		}

		// Python: L287-291 — 默认 pause + mark READY
		logger.Info(mgrLogComponent).
			Str("member_name", memberName).
			Msg("finalize_member: 暂停并标记 READY")
		_ = ag.PauseCoordination(ctx)
		if member != nil {
			_, _ = member.UpdateStatus(ctx, atschema.MemberStatusReady)
		}
	}()

	return nil
}

// GetMonitor 获取活跃团队的 TeamMonitor。
// Python: TeamRuntimeManager.get_monitor(team_name, session_id, hide_dm) (manager.py L545-560)
func (m *TeamRuntimeManager) GetMonitor(
	ctx context.Context,
	teamName string,
	sessionID string,
	hideDM bool,
) (*monitor.TeamMonitor, error) {
	// Python: L557-558
	entry := m.resolveEntry(teamName, sessionID)
	if entry == nil {
		return nil, nil
	}
	// Python: create_monitor(entry.agent, hide_dm=hide_dm)
	// Go 差异：CreateMonitor 需要显式传入 db/teamName/sessionID
	backend := getTeamBackend(entry.Agent)
	if backend == nil {
		return nil, nil
	}
	return monitor.CreateMonitor(entry.Agent, backend.DB(), teamName, sessionID, hideDM)
}

// ListActiveTeams 列出所有活跃团队。
// Python: TeamRuntimeManager.list_active_teams() (manager.py L562-568)
//
// 返回只读快照，排除活跃 TeamAgent 和 InteractGate 引用，
// 使 SDK/CLI 调用者无法通过结果修改运行时状态。
func (m *TeamRuntimeManager) ListActiveTeams() []ActiveTeamInfo {
	return m.pool.ListAllInfo()
}

// RegisterHumanAgentInbound 注册团队→用户通知回调。
// Python: team_backend.register_human_agent_inbound(member_name, callback)
func (m *TeamRuntimeManager) RegisterHumanAgentInbound(ctx context.Context, teamName string, sessionID string, memberName string, callback any) (bool, error) {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Str("member_name", memberName).Msg("注册 HumanAgent 入站")
	entry := m.resolveEntry(teamName, sessionID)
	if entry == nil {
		return false, nil
	}
	// 获取 TeamBackend（entry.Agent 为 *agent.TeamAgent，直接调用 TeamBackend()）
	backend := getTeamBackend(entry.Agent)
	if backend == nil {
		return false, nil
	}
	// 将 callback 转换为 OnInbound 类型
	onInbound, ok := callback.(tools.OnInbound)
	if !ok {
		logger.Warn(mgrLogComponent).Str("member_name", memberName).Msg("callback 类型不匹配 tools.OnInbound")
		return false, nil
	}
	if err := backend.RegisterHumanAgentInbound(ctx, memberName, onInbound); err != nil {
		logger.Warn(mgrLogComponent).Str("member_name", memberName).Err(err).Msg("注册 HumanAgent 入站失败")
		return false, err
	}
	return true, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveEntry 查找活跃团队条目。
// Python: TeamRuntimeManager._resolve_entry(team_name, session_id)
func (m *TeamRuntimeManager) resolveEntry(teamName string, sessionID string) *ActiveTeam {
	entry := m.pool.Get(teamName)
	if entry == nil {
		return nil
	}
	if entry.SessionID != sessionID {
		return nil
	}
	return entry
}

// handleInteractiveInput 处理 InteractiveInput（恢复中断）。
// Python: TeamRuntimeManager.interact() 中的 InteractiveInput 分支
//
// Python 步骤：
//  1. if entry.agent.has_pending_interrupt():
//  2. await entry.agent.resume_interrupt(payload)
//  3. return DeliverResult.success(None)
//  4. return DeliverResult.failure("unsupported_interactive_input")
func (m *TeamRuntimeManager) handleInteractiveInput(ctx context.Context, entry *ActiveTeam, input *sessioninteraction.InteractiveInput) (*interaction.DeliverResult, error) {
	// 防御：Agent 为 nil 时无法处理交互输入
	if entry.Agent == nil {
		return interaction.NewDeliverResultFailure("agent_unavailable"), nil
	}
	// Python: if entry.agent.has_pending_interrupt(): await entry.agent.resume_interrupt(payload); return success
	if entry.Agent.HasPendingInterrupt() {
		err := entry.Agent.ResumeInterrupt(ctx, input)
		if err != nil {
			return interaction.NewDeliverResultFailure("resume_interrupt_failed"), nil
		}
		return interaction.NewDeliverResultSuccess(nil), nil
	}
	// Python: return DeliverResult.failure("unsupported_interactive_input")
	return interaction.NewDeliverResultFailure("unsupported_interactive_input"), nil
}

// resolveRecipients 校验 @<member> 接收者是否在花名册中。
// Python: TeamRuntimeManager._resolve_recipients(agent, payloads)
//
// Python 步骤：
//  1. backend = agent.team_backend
//  2. if backend is None: return payloads
//  3. async def _member_exists(name): return await backend.get_member(name) is not None
//  4. return await resolve_targets(payloads, member_exists=_member_exists)
func (m *TeamRuntimeManager) resolveRecipients(ctx context.Context, entry *ActiveTeam, payloads []interaction.InteractPayload) ([]interaction.InteractPayload, error) {
	// Python 步骤 1-2: backend = agent.team_backend; if backend is None: return payloads
	backend := getTeamBackend(entry.Agent)
	if backend == nil {
		return payloads, nil
	}
	// Python 步骤 3: async def _member_exists(name): return await backend.get_member(name) is not None
	memberExists := func(name string) (bool, error) {
		member, _ := backend.GetMember(ctx, name)
		return member != nil, nil
	}
	// Python 步骤 4: return await resolve_targets(payloads, member_exists=_member_exists)
	return interaction.ResolveTargets(payloads, memberExists)
}

// dispatchPayload 按载荷类型分发。
// Python: TeamRuntimeManager._dispatch_payload(agent, payload)
//
// Python 步骤：
//  1. backend = agent.team_backend
//  2. if backend is None and not isinstance(payload, GodViewMessage): return failure("no_team_backend")
//  3. if isinstance(payload, GodViewMessage):
//     return await UserInbox.deliver_to_leader(agent.deliver_input, payload.body)
//  4. if isinstance(payload, OperatorMessage):
//     a. inbox = UserInbox(backend.message_manager)
//     b. if payload.target is None: await agent.auto_start_all(); return await inbox.broadcast(payload.body)
//     c. await agent.auto_start_member(payload.target); return await inbox.direct(payload.target, payload.body)
//  5. if isinstance(payload, HumanAgentMessage):
//     a. try: inbox = HumanAgentInbox(backend, backend.message_manager, agent_lookup=agent.lookup_human_agent_runtime)
//     b. return await inbox.send(payload.body, to=payload.target, sender=payload.sender)
//     c. except HumanAgentNotEnabledError: return failure("human_agent_not_enabled")
//     d. except UnknownHumanAgentError: return failure("unknown_human_agent")
//  6. return failure(f"unknown_payload:{type(payload).__name__}")
func (m *TeamRuntimeManager) dispatchPayload(
	ctx context.Context,
	entry *ActiveTeam,
	payload interaction.InteractPayload,
) (*interaction.DeliverResult, error) {
	switch p := payload.(type) {
	case *interaction.GodViewMessage:
		// Python 步骤 3: return await UserInbox.deliver_to_leader(agent.deliver_input, payload.body)
		if entry.Agent == nil {
			return interaction.NewDeliverResultFailure("agent_unavailable"), nil
		}
		deliverInput := func(ctx context.Context, content string) error {
			return entry.Agent.DeliverInput(ctx, content, true)
		}
		return interaction.DeliverToLeader(deliverInput, p.Body()), nil

	case *interaction.OperatorMessage:
		// Python 步骤 4: inbox = UserInbox(backend.message_manager)
		backend := getTeamBackend(entry.Agent)
		var msgManager *tools.TeamMessageManager
		if backend != nil {
			msgManager = backend.MessageManager()
		}
		if msgManager == nil {
			return interaction.NewDeliverResultFailure("no_team_backend"), nil
		}
		inbox := interaction.NewUserInbox(msgManager)
		if p.Target() == nil {
			// Python 步骤 4b: 广播前先自动启动所有未启动成员
			if entry.Agent != nil {
				entry.Agent.AutoStartAll(ctx)
			}
			return inbox.Broadcast(p.Body())
		}
		// Python 步骤 4c: 点对点前先启动目标成员
		if entry.Agent != nil {
			entry.Agent.AutoStartMember(ctx, *p.Target())
		}
		return inbox.Direct(*p.Target(), p.Body())

	case *interaction.HumanAgentMessage:
		// Python 步骤 5: inbox = HumanAgentInbox(...)
		backend := getTeamBackend(entry.Agent)
		if backend == nil {
			return interaction.NewDeliverResultFailure("no_team_backend"), nil
		}
		// 包装 agentLookup：*agent.TeamAgent → interaction.DeliverInputer（隐式满足）
		var agentLookup interaction.AgentLookup
		if entry.Agent != nil {
			agentLookup = func(sender string) interaction.DeliverInputer {
				return entry.Agent.LookupHumanAgentRuntime(sender)
			}
		}
		hInbox := interaction.NewHumanAgentInbox(
			backend,
			backend.MessageManager(),
			agentLookup,
			nil, // onInbound 为 nil 时 HumanAgentInbox 内部跳过入站通知；
			// 实际回调需由 server 层通过 RegisterHumanAgentInbound 注入
		)
		result, err := hInbox.Send(ctx, p.Body(), p.Target(), strPtr(p.Sender()))
		if err != nil {
			// Python 步骤 5c-5d
			if _, ok := err.(*interaction.HumanAgentNotEnabledError); ok {
				return interaction.NewDeliverResultFailure("human_agent_not_enabled"), nil
			}
			if _, ok := err.(*interaction.UnknownHumanAgentError); ok {
				return interaction.NewDeliverResultFailure("unknown_human_agent"), nil
			}
			return nil, err
		}
		return result, nil

	default:
		// Python 步骤 6: return failure(f"unknown_payload:{type(payload).__name__}")
		return interaction.NewDeliverResultFailure("unknown_payload:" + payload.Kind().String()), nil
	}
}

// getTeamBackend 从 Agent 中提取 TeamBackend。
func getTeamBackend(a *agent.TeamAgent) *tools.TeamBackend {
	if a == nil {
		return nil
	}
	return a.TeamBackend()
}

// strPtr 返回字符串指针。
func strPtr(s string) *string { return &s }

// inspectSession 检查 DB 和 session 中的团队状态。
// Python: TeamRuntimeManager._inspect_session(spec, team_session, team_name) (manager.py L820-850)
//
// team_in_session 来自 checkpoint 桶；team_in_db 来自静态团队表。
// 两者都是 dispatch 真值表所需 — 团队表查询区分 CREATE 和 NEW_TEAM_IN_SESSION。
func (m *TeamRuntimeManager) inspectSession(
	ctx context.Context,
	spec *atschema.TeamAgentSpec,
	teamSession *session.AgentTeamSession,
	teamName string,
) (teamInSession bool, teamInDB bool, teamDBState string, err error) {
	// Python: L822-823
	cp := getCheckpointer()
	sessionExists := false
	if cp != nil {
		sessionExists, _ = cp.SessionExists(ctx, teamSession.GetSessionID())
	}

	// Python: L824
	if preErr := teamSession.PreRun(ctx); preErr != nil {
		// Python 中 pre_run 失败不影响后续流程，Go 中记录警告
		logger.Warn(mgrLogComponent).Err(preErr).
			Str("session_id", teamSession.GetSessionID()).
			Msg("inspectSession: PreRun 失败")
	}

	// Python: L825-830
	if !sessionExists {
		teamInSession = false
		teamDBState = ""
	} else {
		// Python: L828-829
		bucket := metadata.ReadTeamNamespace(teamSession, teamName)
		teamInSession = bucket != nil
		teamDBState = metadata.ReadTeamDBState(teamSession, teamName)
	}

	// Python: L832-834
	// spec.ResolveDBConfig() 返回 any，需要类型断言为 database.DBConfigProvider
	dbCfgAny := spec.ResolveDBConfig()
	dbCfgProvider, ok := dbCfgAny.(database.DBConfigProvider)
	if !ok && dbCfgAny != nil {
		logger.Warn(mgrLogComponent).
			Str("team_name", teamName).
			Any("db_config_type", fmt.Sprintf("%T", dbCfgAny)).
			Msg("inspectSession: ResolveDBConfig 返回类型非 DBConfigProvider")
	}
	db := spawn.GetSharedDB(dbCfgProvider)
	if db != nil {
		if initErr := db.Initialize(ctx); initErr != nil {
			logger.Warn(mgrLogComponent).Err(initErr).
				Str("team_name", teamName).
				Msg("inspectSession: 数据库初始化失败")
		} else {
			// Python: L834: team_in_db = await db.team.team_exists(team_name)
			teamInDB = db.Team().TeamExists(ctx, teamName)
		}
	}

	return teamInSession, teamInDB, teamDBState, nil
}

// applyAction 执行调度决策的副作用。
// Python: TeamRuntimeManager._apply_action(action, spec, team_session, pool_entry, inputs) (manager.py L852-921)
func (m *TeamRuntimeManager) applyAction(
	ctx context.Context,
	action RunAction,
	spec *atschema.TeamAgentSpec,
	teamSession *session.AgentTeamSession,
	poolEntry *ActiveTeam,
	inputs any,
) (*TeamRuntimeActivation, error) {
	teamName := spec.TeamName
	sessionID := teamSession.GetSessionID()
	kind := action.Kind

	// Python: L861-870 — 拒绝类：不执行副作用
	if IsTeamRejectKind(kind) {
		var ag *agent.TeamAgent
		if poolEntry != nil {
			ag = poolEntry.Agent
		}
		logger.Warn(mgrLogComponent).
			Str("team_name", teamName).
			Str("session_id", sessionID).
			Str("reason", action.Reason).
			Str("kind", string(kind)).
			Msg("run_agent_team rejected")
		return &TeamRuntimeActivation{Agent: ag, Session: teamSession, Action: action}, nil
	}

	// Python: L872-879 — RESUME_FROM_PAUSE
	if kind == RunActionKindResumeFromPause {
		if poolEntry == nil {
			return nil, fmt.Errorf("%s requires an active pool entry", kind)
		}
		if err := preRunWithInputs(ctx, teamSession, inputs); err != nil {
			return nil, err
		}
		poolEntry.State = RuntimeStateRunning
		poolEntry.InteractGate.Reset()
		return &TeamRuntimeActivation{Agent: poolEntry.Agent, Session: teamSession, Action: action}, nil
	}

	// Python: L881-915 — 冷路径（无 pool 条目）
	var ag *agent.TeamAgent
	var buildErr error

	switch kind {
	case RunActionKindColdRecover:
		// Python: L884-886
		ag, buildErr = agent.RecoverFromSession(ctx, teamSession, teamName, spec)
		if buildErr != nil {
			return nil, buildErr
		}
		if _, recErr := ag.RecoverTeam(ctx); recErr != nil {
			return nil, recErr
		}

	case RunActionKindNewTeamInSession:
		// Python: L888-895
		if err := preRunWithInputs(ctx, teamSession, inputs); err != nil {
			return nil, err
		}
		ag, buildErr = buildTeamAgent(ctx, spec)
		if buildErr != nil {
			return nil, buildErr
		}
		if _, resumeErr := ag.ResumeForNewSession(ctx, teamSession); resumeErr != nil {
			return nil, resumeErr
		}
		// Python: L893-895 — team_in_db 为 True，可能有待恢复的 DB 成员
		if _, recErr := ag.RecoverTeam(ctx); recErr != nil {
			return nil, recErr
		}
		if flushErr := flushTeamManifest(ctx, ag, teamSession); flushErr != nil {
			return nil, flushErr
		}

	case RunActionKindCreate:
		// Python: L897-900
		if err := preRunWithInputs(ctx, teamSession, inputs); err != nil {
			return nil, err
		}
		ag, buildErr = buildTeamAgent(ctx, spec)
		if buildErr != nil {
			return nil, buildErr
		}
		if flushErr := flushTeamManifest(ctx, ag, teamSession); flushErr != nil {
			return nil, flushErr
		}

	default:
		return nil, fmt.Errorf("unhandled RunActionKind: %s", kind)
	}

	// Python: L917-919 — 加入池
	m.pool.Add(&ActiveTeam{
		TeamName:     teamName,
		Agent:        ag,
		SessionID:    sessionID,
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	})

	return &TeamRuntimeActivation{Agent: ag, Session: teamSession, Action: action}, nil
}

// buildTeamSession 从 spec+session 参数创建 AgentTeamSession。
// Python: TeamRuntimeManager._build_session(spec, session) (manager.py L923-932)
func buildTeamSession(spec *atschema.TeamAgentSpec, sess any) *session.AgentTeamSession {
	switch s := sess.(type) {
	case *session.AgentTeamSession:
		// Python: L926 — isinstance(session, AgentTeamSession)
		return s
	case string:
		// Python: L927 — isinstance(session, str)
		return session.NewAgentTeamSession(session.WithAgentTeamSessionID(s))
	default:
		// Python: L928 — return create_agent_team_session()
		return session.NewAgentTeamSession()
	}
}

// preRunWithInputs 调用 session.pre_run(inputs)。
// Python: TeamRuntimeManager._pre_run_with_inputs(session, inputs) (manager.py L934-937)
//
// 仅在 inputs 为 dict 时转发，否则传 nil（对齐 Python: isinstance(inputs, dict)）。
func preRunWithInputs(ctx context.Context, sess *session.AgentTeamSession, inputs any) error {
	inputsDict, _ := inputs.(map[string]any)
	return sess.PreRun(ctx, inputsDict)
}

// flushTeamManifest 持久化团队清单 + 刷检查点。
// Python: TeamRuntimeManager._flush_team_manifest(agent, session) (manager.py L939-943)
//
// 在暴露 runtime_ready 之前，持久化最小团队清单。
func flushTeamManifest(ctx context.Context, ag *agent.TeamAgent, sess *session.AgentTeamSession) error {
	// Python: L941 — agent.persist_session_manifest(session)
	ag.PersistSessionManifest(sess)
	// Python: L942 — await session.flush_checkpoint()
	return sess.FlushCheckpoint(ctx)
}

// buildTeamAgent 从 TeamAgentSpec 构建 TeamAgent（对齐 Python spec.build()）。
// Python: TeamAgentSpec.build() → TeamAgent (blueprint.py L370-446)
//
// 因 Go 循环依赖，schema 包无法导入 agent 包，因此 spec.Build() 仅做校验。
// 本函数在 runtime 包中完成 Python spec.build() 的完整构造流程：
//  1. spec.Build() 校验
//  2. 构建 TeamSpec（含 model_pool/model_pool_strategy）
//  3. 解析 transport/messager_config
//  4. 构建 ModelAllocator + 预分配 leader 模型
//  5. 构建 TeamRuntimeContext
//  6. NewTeamAgent(card) + AttachModelAllocator + Configure
func buildTeamAgent(ctx context.Context, spec *atschema.TeamAgentSpec) (*agent.TeamAgent, error) {
	// Python: L372-373 — self._validate_reserved_names() / self._validate_hitt_consistency()
	if _, err := spec.Build(); err != nil {
		return nil, fmt.Errorf("buildTeamAgent: spec 校验失败: %w", err)
	}

	// Python: L389-399 — 构建 TeamSpec
	var teamPool []models.ModelPoolEntry
	var teamStrategy string
	if spec.ModelRouter != nil {
		teamPool = spec.ModelRouter.ToPoolEntries()
		teamStrategy = "router"
	} else {
		teamPool = spec.ModelPool
		teamStrategy = spec.ModelPoolStrategy
	}

	teamSpec := &atschema.TeamSpec{
		TeamName:          spec.TeamName,
		DisplayName:       spec.TeamName,
		LeaderMemberName:  spec.Leader.MemberName,
		Language:          spec.Language,
		ModelPool:         teamPool,
		ModelPoolStrategy: teamStrategy,
	}

	// Python: L402 — messager_config = self.transport.build() if self.transport else None
	var messagerConfig *atschema.MessagerTransportConfig
	if spec.Transport != nil {
		if mc, err := spec.Transport.Build(); err == nil {
			messagerConfig = &mc
		}
	}

	// Python: L403 — db_config = self.resolve_db_config()
	dbCfgAny := spec.ResolveDBConfig()
	dbCfgProvider, ok := dbCfgAny.(database.DBConfigProvider)
	if !ok && dbCfgAny != nil {
		logger.Warn(mgrLogComponent).
			Str("team_name", spec.TeamName).
			Any("db_config_type", fmt.Sprintf("%T", dbCfgAny)).
			Msg("buildTeamAgent: ResolveDBConfig 返回类型非 DBConfigProvider")
	}

	// Python: L419-425 — build model allocator + leader allocation
	var modelAllocator models.ModelAllocator
	var leaderMemberModel *models.TeamModelConfig
	if len(teamPool) > 0 {
		modelAllocator = models.BuildModelAllocatorForPool(teamPool, teamStrategy, spec.TeamName)
		if modelAllocator != nil {
			leaderAlloc := modelAllocator.Allocate(spec.Leader.ModelName)
			if leaderAlloc != nil {
				cfg := leaderAlloc.ToTeamModelConfig()
				leaderMemberModel = &cfg
			}
		}
	}

	// Python: L426 — self._validate_leader_model_resolved(...)
	if err := atschema.ValidateLeaderModelResolved(*spec, leaderMemberModel, *teamSpec); err != nil {
		return nil, fmt.Errorf("buildTeamAgent: leader 模型校验失败: %w", err)
	}

	// Python: L428-436 — 构建 TeamRuntimeContext
	runtimeCtx := atschema.TeamRuntimeContext{
		Role:          atschema.TeamRoleLeader,
		MemberName:    spec.Leader.MemberName,
		Persona:       spec.Leader.Persona,
		TeamSpec:      teamSpec,
		MessagerConfig: messagerConfig,
		DBConfig:      dbCfgProvider,
		MemberModel:   leaderMemberModel,
	}

	// Python: L405-410 — 构建 leader_card
	leaderCardID := fmt.Sprintf("%s_%s", spec.TeamName, spec.Leader.MemberName)
	leaderCard := agentschema.NewAgentCard(
		agentschema.WithAgentID(leaderCardID),
		agentschema.WithAgentName(spec.Leader.DisplayName),
		agentschema.WithAgentDescription(fmt.Sprintf("Leader of team %s", spec.TeamName)),
	)

	// Python: L438 — agent = _TeamAgent(leader_card)
	ag := agent.NewTeamAgent(leaderCard)

	// Python: L444 — agent.attach_model_allocator(model_allocator, leader_allocation=leader_allocation)
	if modelAllocator != nil {
		var leaderAlloc *models.Allocation
		if len(teamPool) > 0 {
			alloc := modelAllocator.Allocate(spec.Leader.ModelName)
			leaderAlloc = alloc
		}
		ag.AttachModelAllocator(modelAllocator, leaderAlloc)
	}

	// Python: L445 — agent.configure(self, context)
	ag.Configure(ctx, *spec, runtimeCtx)

	return ag, nil
}
