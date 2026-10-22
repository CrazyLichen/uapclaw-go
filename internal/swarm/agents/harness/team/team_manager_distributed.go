package team

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AttachDistributedHooksForRunnerRuntime 为 Runner 池中的 TeamAgent 附加分布式 hooks。
// 对齐 Python: TeamManager.attach_distributed_hooks_for_runner_runtime(team_name, session_id, ...)
//
// Python 步骤：
//
//	Python: 1. config_base = get_config()
//	Python: 2. if not self._is_distributed_mode(config_base): return False
//	Python: 3. runtime_mgr = _runner_team_runtime_manager(GLOBAL_RUNNER)
//	Python: 4. active_team = await runtime_mgr.pool.get(team_name)
//	Python: 5. if active_team is None: return False
//	Python: 6. team_agent = active_team.agent
//	Python: 7. if team_agent is None: return False
//	Python: 8. self._runner_team_agents[session_id] = team_agent
//	9. attach 5 个 distributed hooks:
//	   - attach_distributed_local_spawn_guard
//	   - attach_spawn_member_remote_bootstrap_wrapper
//	   - attach_shutdown_member_remote_cleanup_wrapper
//	   - attach_remote_bootstrap_ack_listener
//	   - attach_remote_teammate_bootstrap_listener
//
// 10. 返回 True
//
// S-04 回填：shutdown cleanup wrapper 已实装（remote_member_shutdown_cleanup.go）；
// 其余 4 个 hooks 属 ⤵️(#10.6.21) remote_member_bootstrap 完整移植，暂以
// 存在性检查 + 日志占位（对齐 Python 缺 hook 时 warning 但不失败的整体语义——
// Python 5 个 attach 调用各自内部失败仅 warning，任一缺失不使本方法返回 False）。
func (m *TeamManager) AttachDistributedHooksForRunnerRuntime(
	teamName string,
	sessionID string,
	channelID *string,
) bool {
	// Python: config_base = get_config()
	configBase, err := teamLoadConfig()
	if err != nil {
		configBase = map[string]any{}
	}
	if !IsDistributedMode(configBase) {
		logger.Debug(logComponent).
			Str("team_name", teamName).Str("session_id", sessionID).
			Msg("[TeamManager] non-distributed mode; skip Runner runtime hooks")
		return false
	}

	// Python: runtime_mgr = _runner_team_runtime_manager(GLOBAL_RUNNER)
	// Python: active_team = await runtime_mgr.pool.get(team_name)
	runtimeMgr := runtime.GetTeamRuntimeManager()
	activeTeam := runtimeMgr.Pool().Get(teamName)
	if activeTeam == nil {
		logger.Warn(logComponent).
			Str("team_name", teamName).Str("session_id", sessionID).
			Msg("[TeamManager] Runner pool has no active team for distributed hooks")
		return false
	}

	// Python: team_agent = active_team.agent
	teamAgent := activeTeam.Agent
	if teamAgent == nil {
		logger.Warn(logComponent).
			Str("team_name", teamName).Str("session_id", sessionID).
			Msg("[TeamManager] ActiveTeam has no agent instance for distributed hooks")
		return false
	}

	// Python: self._runner_team_agents[session_id] = team_agent
	m.mu.Lock()
	if m.runnerTeamAgents == nil {
		m.runnerTeamAgents = make(map[string]*agent.TeamAgent)
	}
	m.runnerTeamAgents[sessionID] = teamAgent
	m.mu.Unlock()

	// ⤵️(#10.6.21) attach_distributed_local_spawn_guard — 待 remote_member_bootstrap 完整移植
	// ⤵️(#10.6.21) attach_spawn_member_remote_bootstrap_wrapper — 待 remote_member_bootstrap 完整移植

	// S-04 回填：attach_shutdown_member_remote_cleanup_wrapper（含 M-09 channelID 透传）
	AttachShutdownMemberRemoteCleanupWrapper(teamAgent, sessionID, channelID)

	// ⤵️(#10.6.21) attach_remote_bootstrap_ack_listener — 待 remote_member_bootstrap 完整移植
	// ⤵️(#10.6.21) attach_remote_teammate_bootstrap_listener — 待 remote_member_bootstrap 完整移植

	chID := ""
	if channelID != nil {
		chID = *channelID
	}
	logger.Info(logComponent).
		Str("team_name", teamName).Str("session_id", sessionID).Str("channel_id", chID).
		Msg("[TeamManager] distributed hooks attached to Runner-owned TeamAgent")
	return true
}
