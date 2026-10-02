package team

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// EnsureTeamSharedSkillsInitialized 确保 team 共享技能已初始化。
// 对齐 Python: TeamManager.ensure_team_shared_skills_initialized(spec)
//
// Python 步骤：
//  1. TeamManager._copy_global_skills_to_team_shared_dir(spec)
//  2. TeamManager._sync_team_shared_skills_to_agent_global(spec)
func (m *TeamManager) EnsureTeamSharedSkillsInitialized(spec any) {
	// ⤵️(#9.72) 完整实现 — 待 skill_manager / workspace 路径回填
}

// SyncTeamSkills 同步指定 session 的技能（从 workspace 到 global）。
// 对齐 Python: TeamManager.sync_team_skills(session_id)
//
// Python 步骤：
//  1. sync_info = self._team_skill_sync_targets.get(session_id)
//  2. if sync_info is None: logger.debug("no sync target"); return
//  3. source, target = sync_info
//  4. _sync_skills_dir(source, target)
func (m *TeamManager) SyncTeamSkills(sessionID string) {
	syncTarget, ok := m.teamSkillSyncTargets[sessionID]
	if !ok {
		logger.Debug(logComponent).Str("session_id", sessionID).Msg("无技能同步目标")
		return
	}
	// ⤵️(#9.72) _sync_skills_dir — 待 shutil / Path 等效实现回填
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("source", syncTarget.Source).
		Str("target", syncTarget.Target).
		Msg("同步 team 技能（待回填）")
}

// RegisterTeamSkillRail 注册 TeamSkillEvolutionRail 实例。
// 对齐 Python: TeamManager.register_team_skill_rail(session_id, rail)
func (m *TeamManager) RegisterTeamSkillRail(sessionID string, rail any) {
	m.teamSkillRails[sessionID] = rail
}

// RegisterTeamMemberSkillEvolutionRail 注册成员 SkillEvolutionRail 实例。
// 对齐 Python: TeamManager.register_team_member_skill_evolution_rail(session_id, rail)
func (m *TeamManager) RegisterTeamMemberSkillEvolutionRail(sessionID string, rail any) {
	rails := m.teamMemberSkillEvoRails[sessionID]
	for _, r := range rails {
		if r == rail {
			return // 已注册
		}
	}
	m.teamMemberSkillEvoRails[sessionID] = append(rails, rail)
}

// RegisterTeamSkillCreateRail 注册 TeamSkillCreateRail 实例。
// 对齐 Python: TeamManager.register_team_skill_create_rail(session_id, rail)
func (m *TeamManager) RegisterTeamSkillCreateRail(sessionID string, rail any) {
	m.teamSkillCreateRails[sessionID] = rail
}

// RegisterTeamRailContext 注册 TeamRailMountContext（仅 leader）。
// 对齐 Python: TeamManager.register_team_rail_context(session_id, context)
//
// Python 步骤：
//
//	if getattr(context.member_info, "role", None) == "leader":
//	    self._team_rail_contexts[session_id] = context
func (m *TeamManager) RegisterTeamRailContext(sessionID string, ctx *TeamRailMountContext) {
	// 检查是否为 leader（member_info 中 role == "leader"）
	if isLeaderRole(ctx) {
		m.teamRailContexts[sessionID] = ctx
	}
}

// GetTeamRailContext 返回指定 session 的 leader rail mount context。
// 对齐 Python: TeamManager.get_team_rail_context(session_id)
func (m *TeamManager) GetTeamRailContext(sessionID string) *TeamRailMountContext {
	return m.teamRailContexts[sessionID]
}

// RegisterTeamLiveRail 注册活跃 Rail 及其所有者。
// 对齐 Python: TeamManager.register_team_live_rail(session_id, agent, rail)
//
// Python 步骤：
//
//	rails = self._team_live_rails.setdefault(session_id, [])
//	entry = (agent, rail)
//	if entry not in rails: rails.append(entry)
func (m *TeamManager) RegisterTeamLiveRail(sessionID string, agent any, rail any) {
	rails := m.teamLiveRails[sessionID]
	entry := LiveRailEntry{Agent: agent, Rail: rail}
	for _, r := range rails {
		if r.Agent == agent && r.Rail == rail {
			return // 已注册
		}
	}
	m.teamLiveRails[sessionID] = append(rails, entry)
}

// RegisterTeamSkillSyncTarget 注册技能同步目录对。
// 对齐 Python: TeamManager.register_team_skill_sync_target(session_id, source, target)
func (m *TeamManager) RegisterTeamSkillSyncTarget(sessionID string, source string, target string) {
	m.teamSkillSyncTargets[sessionID] = SkillSyncTarget{Source: source, Target: target}
}

// HasTeamSkillSyncTarget 检查指定 session 是否有技能同步目标。
// 对齐 Python: TeamManager.has_team_skill_sync_target(session_id)
func (m *TeamManager) HasTeamSkillSyncTarget(sessionID string) bool {
	_, ok := m.teamSkillSyncTargets[sessionID]
	return ok
}

// GetTeamSkillRail 获取指定 session 的 TeamSkillEvolutionRail。
// 对齐 Python: TeamManager.get_team_skill_rail(session_id)
func (m *TeamManager) GetTeamSkillRail(sessionID string) any {
	return m.teamSkillRails[sessionID]
}

// GetTeamSkillCreateRail 获取指定 session 的 TeamSkillCreateRail。
// 对齐 Python: TeamManager.get_team_skill_create_rail(session_id)
func (m *TeamManager) GetTeamSkillCreateRail(sessionID string) any {
	return m.teamSkillCreateRails[sessionID]
}

// FindTeamSkillRailForRequest 查找拥有指定 requestID 的 TeamSkillEvolutionRail。
// 对齐 Python: TeamManager.find_team_skill_rail_for_request(request_id)
//
// Python 步骤：
//  1. for rail in self._team_skill_rails.values():
//  2. if request_id in getattr(rail, "_pending_approval_snapshots", {}): return rail
//  3. return None
func (m *TeamManager) FindTeamSkillRailForRequest(requestID string) any {
	for _, rail := range m.teamSkillRails {
		// 检查 rail 是否有 _pending_approval_snapshots 属性
		if hasPendingApproval(rail, requestID) {
			return rail
		}
	}
	return nil
}

// DrainTeamSkillEvents 排空指定 session 的 TeamSkillEvolutionRail 审批事件。
// 对齐 Python: TeamManager.drain_team_skill_events(session_id)
//
// Python 步骤：
//  1. rail = self._team_skill_rails.get(session_id)
//  2. if rail is None: return []
//  3. return await rail.drain_pending_approval_events()
func (m *TeamManager) DrainTeamSkillEvents(sessionID string) []map[string]any {
	rail, ok := m.teamSkillRails[sessionID]
	if !ok || rail == nil {
		return nil
	}
	// ⤵️(#9.72) rail.drain_pending_approval_events() — 待 Rail 完整实现后回填
	return nil
}

// UpdateEvolutionConfig 热更新 team evolution rails。
// 对齐 Python: TeamManager.update_evolution_config(config)
//
// Python 步骤：
//  1. auto_scan_enabled = get_evolution_auto_scan_enabled(config)
//  2. skill_create_enabled = get_skill_create_enabled(config)
//  3. for rails in self._team_member_skill_evolution_rails.values():
//     for rail in rails: rail.auto_scan = auto_scan_enabled
//  4. for rail in self._team_skill_rails.values():
//     rail.auto_scan = auto_scan_enabled
//  5. if not skill_create_enabled:
//     for session_id, rail in self._team_skill_create_rails.items():
//     await self._unregister_live_rail(session_id, rail)
//     self._team_skill_create_rails.pop(session_id, None)
//     return
//  6. for session_id, context in self._team_rail_contexts.items():
//     if session_id in self._team_skill_create_rails: continue
//     self._build_and_mount_member_rails_for_context(session_id, context,
//     mount_team_skill_rail=False, mount_team_skill_create_rail=True,
//     mount_skill_evolution_rail=False)
func (m *TeamManager) UpdateEvolutionConfig(config map[string]any) {
	// ⤵️(#9.72) 完整实现 — 待 Rail 类型和 evolution config 回填
	logger.Info(logComponent).Msg("更新 evolution 配置（待回填）")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isLeaderRole 检查 TeamRailMountContext 中的 member_info 是否为 leader 角色。
func isLeaderRole(ctx *TeamRailMountContext) bool {
	if ctx == nil || ctx.MemberInfo == nil {
		return false
	}
	// 尝试通过反射或接口获取 role
	type roleHolder interface {
		GetRole() string
	}
	if holder, ok := ctx.MemberInfo.(roleHolder); ok {
		return holder.GetRole() == "leader"
	}
	// 默认：如果无法判断，保守存储
	return true
}

// hasPendingApproval 检查 rail 是否有指定 requestID 的待审批快照。
func hasPendingApproval(rail any, requestID string) bool {
	// 尝试通过接口检查
	type pendingSnapshots interface {
		HasPendingApprovalSnapshot(requestID string) bool
	}
	if checker, ok := rail.(pendingSnapshots); ok {
		return checker.HasPendingApprovalSnapshot(requestID)
	}
	return false
}

// unregisterLiveRail 从 live rails 中移除并反注册指定 rail。
// 对齐 Python: TeamManager._unregister_live_rail(session_id, rail)
func (m *TeamManager) unregisterLiveRail(sessionID string, rail any) {
	liveRails, ok := m.teamLiveRails[sessionID]
	if !ok {
		return
	}
	var remaining []LiveRailEntry
	for _, entry := range liveRails {
		if entry.Rail == rail {
			// 调用 agent.unregister_rail(live_rail) — ⤵️(#9.72) 待回填
			continue
		}
		remaining = append(remaining, entry)
	}
	if len(remaining) > 0 {
		m.teamLiveRails[sessionID] = remaining
	} else {
		delete(m.teamLiveRails, sessionID)
	}
}
