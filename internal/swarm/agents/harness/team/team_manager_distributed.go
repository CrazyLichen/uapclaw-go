package team

// ──────────────────────────── 导出函数 ────────────────────────────

// AttachDistributedHooksForRunnerRuntime 为 Runner 池中的 TeamAgent 附加分布式 hooks。
// 对齐 Python: TeamManager.attach_distributed_hooks_for_runner_runtime(team_name, session_id, ...)
//
// Python 步骤：
//  1. config_base = get_config()
//  2. if not self._is_distributed_mode(config_base): return False
//  3. runtime_mgr = _runner_team_runtime_manager(GLOBAL_RUNNER)
//  4. active_team = await runtime_mgr.pool.get(team_name)
//  5. if active_team is None: return False
//  6. team_agent = active_team.agent
//  7. if team_agent is None: return False
//  8. self._runner_team_agents[session_id] = team_agent
//  9. attach 5 个 distributed hooks:
//     - attach_distributed_local_spawn_guard
//     - attach_spawn_member_remote_bootstrap_wrapper
//     - attach_shutdown_member_remote_cleanup_wrapper
//     - attach_remote_bootstrap_ack_listener
//     - attach_remote_teammate_bootstrap_listener
// 10. return True
func (m *TeamManager) AttachDistributedHooksForRunnerRuntime(
	teamName string,
	sessionID string,
	channelID *string,
) bool {
	// ⤵️(#9.72) 完整实现 — 待 distributed_runtime / remote_member_bootstrap 回填
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────
