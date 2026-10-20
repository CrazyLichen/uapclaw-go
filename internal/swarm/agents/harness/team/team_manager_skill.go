package team

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// EnsureTeamSharedSkillsInitialized 确保 team 共享技能已初始化。
// 对齐 Python: TeamManager.ensure_team_shared_skills_initialized(spec) (L1043-1047)
func (m *TeamManager) EnsureTeamSharedSkillsInitialized(spec *atschema.TeamAgentSpec) {
	if spec == nil {
		return
	}
	m.copyGlobalSkillsToTeamSharedDir(spec)
	m.syncTeamSharedSkillsToAgentGlobal(spec)
}

// SyncTeamSkills 同步指定 session 的技能（从 workspace 到 global）。
// 对齐 Python: TeamManager.sync_team_skills(session_id)
//
// Python 步骤：
//
//	Python: 1. sync_info = self._team_skill_sync_targets.get(session_id)
//	Python: 2. if sync_info is None: logger.debug("no sync target"); return
//	Python: 3. source, target = sync_info
//	Python: 4. _sync_skills_dir(source, target)
func (m *TeamManager) SyncTeamSkills(sessionID string) {
	syncTarget, ok := m.teamSkillSyncTargets[sessionID]
	if !ok {
		logger.Debug(logComponent).Str("session_id", sessionID).Msg("no skillsynctarget")
		return
	}
	syncSkillsDir(syncTarget.Source, syncTarget.Target)
}

// RegisterTeamSkillRail 注册 TeamSkillEvolutionRail 实例。
// 对齐 Python: TeamManager.register_team_skill_rail(session_id, rail)
func (m *TeamManager) RegisterTeamSkillRail(sessionID string, rail *evolution.TeamSkillEvolutionRail) {
	m.teamSkillRails[sessionID] = rail
}

// RegisterTeamMemberSkillEvolutionRail 注册成员 SkillEvolutionRail 实例。
// 对齐 Python: TeamManager.register_team_member_skill_evolution_rail(session_id, rail)
func (m *TeamManager) RegisterTeamMemberSkillEvolutionRail(sessionID string, rail *evolution.SkillEvolutionRail) {
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
func (m *TeamManager) RegisterTeamSkillCreateRail(sessionID string, rail *evolution.TeamSkillCreateRail) {
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
		return
	}
	// T-03: 非 leader context 静默丢弃时输出 debug 日志，便于排查
	// rail 未挂载问题（Python 同样静默丢弃，此处仅增加可观测性）
	logger.Debug(logComponent).
		Str("session_id", sessionID).
		Msg("RegisterTeamRailContext: non-leader context discarded")
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
func (m *TeamManager) RegisterTeamLiveRail(sessionID string, agent interfaces.DeepAgentInterface, rail agentinterfaces.AgentRail) {
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
func (m *TeamManager) GetTeamSkillRail(sessionID string) *evolution.TeamSkillEvolutionRail {
	return m.teamSkillRails[sessionID]
}

// GetTeamSkillCreateRail 获取指定 session 的 TeamSkillCreateRail。
// 对齐 Python: TeamManager.get_team_skill_create_rail(session_id)
func (m *TeamManager) GetTeamSkillCreateRail(sessionID string) *evolution.TeamSkillCreateRail {
	return m.teamSkillCreateRails[sessionID]
}

// FindTeamSkillRailForRequest 查找拥有指定 requestID 的 TeamSkillEvolutionRail。
// 对齐 Python: TeamManager.find_team_skill_rail_for_request(request_id)
//
// Python 步骤：
//
//	Python: 1. for rail in self._team_skill_rails.values():
//	Python: 2. if request_id in getattr(rail, "_pending_approval_snapshots", {}): return rail
//	Python: 3. return None
func (m *TeamManager) FindTeamSkillRailForRequest(requestID string) *evolution.TeamSkillEvolutionRail {
	for _, rail := range m.teamSkillRails {
		if rail.HasPendingApprovalSnapshot(requestID) {
			return rail
		}
	}
	return nil
}

// DrainTeamSkillEvents 排空指定 session 的 TeamSkillEvolutionRail 审批事件。
// 对齐 Python: TeamManager.drain_team_skill_events(session_id)
//
// Python 步骤：
//
//	Python: 1. rail = self._team_skill_rails.get(session_id)
//	Python: 2. if rail is None: return []
//	Python: 3. return await rail.drain_pending_approval_events()
func (m *TeamManager) DrainTeamSkillEvents(sessionID string) []map[string]any {
	rail, ok := m.teamSkillRails[sessionID]
	if !ok || rail == nil {
		return []map[string]any{}
	}
	// 对齐 Python: await rail.drain_pending_approval_events()
	// DrainPendingApprovalEvents 返回 []*stream.OutputSchema，转为 []map[string]any
	events := rail.DrainPendingApprovalEvents(true, nil)
	if len(events) == 0 {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(events))
	for _, e := range events {
		if e == nil {
			continue
		}
		if payload, ok := e.Payload.(map[string]any); ok {
			result = append(result, payload)
		}
	}
	return result
}

// UpdateEvolutionConfig 热更新 team evolution rails。
// 对齐 Python: TeamManager.update_evolution_config(config)
//
// Python 步骤：
//
//	Python: 1. auto_scan_enabled = get_evolution_auto_scan_enabled(config)
//	Python: 2. skill_create_enabled = get_skill_create_enabled(config)
//	Python: 3. for rails in self._team_member_skill_evolution_rails.values():
//	   for rail in rails: rail.auto_scan = auto_scan_enabled
//	Python: 4. for rail in self._team_skill_rails.values():
//	   rail.auto_scan = auto_scan_enabled
//	Python: 5. if not skill_create_enabled:
//	   for session_id, rail in self._team_skill_create_rails.items():
//	   await self._unregister_live_rail(session_id, rail)
//	   self._team_skill_create_rails.pop(session_id, None)
//	   return
//	Python: 6. for session_id, context in self._team_rail_contexts.items():
//	   if session_id in self._team_skill_create_rails: continue
//	   self._build_and_mount_member_rails_for_context(session_id, context,
//	   mount_team_skill_rail=False, mount_team_skill_create_rail=True,
//	   mount_skill_evolution_rail=False)
func (m *TeamManager) UpdateEvolutionConfig(config map[string]any) {
	autoScanEnabled := GetEvolutionAutoScanEnabled(config)
	skillCreateEnabled := GetSkillCreateEnabled(config)

	// 步骤 3: 更新所有成员 SkillEvolutionRail 的 autoScan
	for _, rails := range m.teamMemberSkillEvoRails {
		for _, rail := range rails {
			rail.SetAutoScan(autoScanEnabled)
		}
	}

	// 步骤 4: 更新所有 TeamSkillEvolutionRail 的 autoScan
	for _, rail := range m.teamSkillRails {
		rail.SetAutoScan(autoScanEnabled)
	}

	// 步骤 5: 若 skill_create 未启用，反注册所有 skill create rails
	if !skillCreateEnabled {
		for sessionID, rail := range m.teamSkillCreateRails {
			m.unregisterLiveRail(sessionID, rail)
			delete(m.teamSkillCreateRails, sessionID)
		}
		return
	}

	// 步骤 6: 若 skill_create 启用，为缺少 skill create rail 的 session 挂载
	for sessionID, context := range m.teamRailContexts {
		if _, ok := m.teamSkillCreateRails[sessionID]; ok {
			continue
		}
		m.buildAndMountMemberRailsForContext(sessionID, context,
			false /* mountTeamSkillRail */, true /* mountTeamSkillCreateRail */, false /* mountSkillEvolutionRail */)
	}

	logger.Info(logComponent).
		Bool("auto_scan_enabled", autoScanEnabled).
		Bool("skill_create_enabled", skillCreateEnabled).
		Msg("evolution config update completed")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isLeaderRole 检查 TeamRailMountContext 中的 member_info 是否为 leader 角色。
// 对齐 Python: getattr(context.member_info, "role", None) == "leader"
func isLeaderRole(ctx *TeamRailMountContext) bool {
	if ctx == nil || ctx.MemberInfo == nil {
		return false
	}
	if ctx.MemberInfo.Role != nil {
		return *ctx.MemberInfo.Role == "leader"
	}
	// S-26: 对齐 Python，Role 为 nil 时返回 false（Python: getattr(ctx.member_info, "role", None) == "leader" → False）
	return false
}

// buildAndMountMemberRailsForContext 用已存储的挂载上下文重建 team rails。
// 对齐 Python: TeamManager._build_and_mount_member_rails_for_context (team_manager.py L1285-1327)
//
// Python 步骤：
//
//	Python: 1. latest_config = get_config(); context.team_workspace.config = latest_config
//	Python: 2. member_rails = build_member_rails(member_info, runtime, team_workspace)
//	3. 遍历 rails，按 mount 开关挂载：
//	   - TeamSkillEvolutionRail + mount_team_skill_rail → agent.add_rail + register_team_live_rail
//	   - SkillEvolutionRail + mount_skill_evolution_rail → agent.add_rail + register_team_member_skill_evolution_rail
//	   - TeamSkillCreateRail + mount_team_skill_create_rail → agent.add_rail + register_team_live_rail
//	4. team_skill_rail 非 nil → register_team_skill_rail；
//	   skills_dir 非空 → register_team_skill_sync_target(skills_dir, get_agent_skills_dir())
//	5. team_skill_create_rail 非 nil → register_team_skill_create_rail
func (m *TeamManager) buildAndMountMemberRailsForContext(
	sessionID string,
	ctx *TeamRailMountContext,
	mountTeamSkillRail bool,
	mountTeamSkillCreateRail bool,
	mountSkillEvolutionRail bool,
) (*evolution.TeamSkillEvolutionRail, *evolution.TeamSkillCreateRail) {
	// Python: get_config() 加载失败时抛异常中断；Go 侧对齐语义：
	// 加载失败记日志后返回 nil（不中断调用方热更新循环）。
	latestConfig, err := teamLoadConfig()
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("session_id", sessionID).
			Msg("[TeamManager] load config failed for rail rebuild")
		return nil, nil
	}
	if ctx.TeamWorkspace != nil {
		ctx.TeamWorkspace.Config = latestConfig
	}

	memberRails := BuildMemberRails(ctx.MemberInfo, ctx.Runtime, ctx.TeamWorkspace)

	var teamSkillRail *evolution.TeamSkillEvolutionRail
	var teamSkillCreateRail *evolution.TeamSkillCreateRail
	for _, rail := range memberRails {
		if tsr, ok := rail.(*evolution.TeamSkillEvolutionRail); ok && mountTeamSkillRail {
			ctx.Agent.AddRail(tsr)
			m.RegisterTeamLiveRail(sessionID, ctx.Agent, tsr)
			teamSkillRail = tsr
		} else if ser, ok := rail.(*evolution.SkillEvolutionRail); ok && mountSkillEvolutionRail {
			ctx.Agent.AddRail(ser)
			m.RegisterTeamMemberSkillEvolutionRail(sessionID, ser)
		} else if scr, ok := rail.(*evolution.TeamSkillCreateRail); ok && mountTeamSkillCreateRail {
			ctx.Agent.AddRail(scr)
			m.RegisterTeamLiveRail(sessionID, ctx.Agent, scr)
			teamSkillCreateRail = scr
		}
	}

	if teamSkillRail != nil {
		m.RegisterTeamSkillRail(sessionID, teamSkillRail)
		if ctx.TeamWorkspace != nil && ctx.TeamWorkspace.SkillsDir != nil && *ctx.TeamWorkspace.SkillsDir != "" {
			m.RegisterTeamSkillSyncTarget(sessionID, *ctx.TeamWorkspace.SkillsDir, workspace.AgentSkillsDir())
		}
	}
	if teamSkillCreateRail != nil {
		m.RegisterTeamSkillCreateRail(sessionID, teamSkillCreateRail)
	}
	return teamSkillRail, teamSkillCreateRail
}

// unregisterLiveRail 从 live rails 中移除并反注册指定 rail。
// 对齐 Python: TeamManager._unregister_live_rail(session_id, rail)
func (m *TeamManager) unregisterLiveRail(sessionID string, rail agentinterfaces.AgentRail) {
	liveRails, ok := m.teamLiveRails[sessionID]
	if !ok {
		return
	}
	var remaining []LiveRailEntry
	for _, entry := range liveRails {
		if entry.Rail == rail {
			// 对齐 Python: agent.unregister_rail(live_rail)
			if entry.Agent != nil {
				_ = entry.Agent.UnregisterRail(context.Background(), rail)
			}
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

// copyGlobalSkillsToTeamSharedDir 复制全局技能到团队共享目录（team build 后执行一次）。
// 对齐 Python: TeamManager._copy_global_skills_to_team_shared_dir(spec) (L998-1026)
//
// marker 文件 .team_skills_copied 幂等：存在即跳过。
func (m *TeamManager) copyGlobalSkillsToTeamSharedDir(spec *atschema.TeamAgentSpec) {
	globalSkillsDir := workspace.AgentSkillsDir()
	if info, err := os.Stat(globalSkillsDir); err != nil || !info.IsDir() {
		logger.Warn(logComponent).Str("global_skills_dir", globalSkillsDir).
			Msg("[TeamManager] global_skills_dir does not exist")
		return
	}

	teamSharedSkillsDir := resolveTeamSharedSkillsDir(spec)

	// marker 幂等检查
	copiedMarker := filepath.Join(teamSharedSkillsDir, ".team_skills_copied")
	if _, err := os.Stat(copiedMarker); err == nil {
		logger.Info(logComponent).Msg("[TeamManager] Team shared skills already copied, skipping")
		return
	}

	// 复制整个技能目录（含 skills_state.json），目标存在则合并覆盖（dirs_exist_ok=True 等效）
	if err := copyTreeMerge(globalSkillsDir, teamSharedSkillsDir); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("global_skills_dir", globalSkillsDir).
			Str("team_shared_skills_dir", teamSharedSkillsDir).
			Msg("[TeamManager] copy global skills to team shared failed")
		return
	}

	// 写 marker 文件
	if err := os.WriteFile(copiedMarker, []byte(""), 0o644); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamManager] write team_skills_copied marker failed")
		return
	}
	logger.Info(logComponent).Str("team_shared_skills_dir", teamSharedSkillsDir).
		Msg("[TeamManager] Copied global skills dir to team shared")
}

// resolveTeamSharedSkillsDir 解析团队共享技能目录。
// 对齐 Python: TeamManager._resolve_team_shared_skills_dir(spec) (L1028-1034)
//
// spec.Workspace.RootPath 优先，回退 TeamHome(teamName)/team-workspace。
func resolveTeamSharedSkillsDir(spec *atschema.TeamAgentSpec) string {
	wsPath := ""
	if spec.Workspace != nil && spec.Workspace.RootPath != "" {
		wsPath = spec.Workspace.RootPath
	}
	if wsPath == "" {
		wsPath = filepath.Join(agentteams.TeamHome(spec.TeamName), "team-workspace")
	}
	return filepath.Join(wsPath, "skills")
}

// syncTeamSharedSkillsToAgentGlobal 将团队共享技能镜像到本地 agent skills 目录（供 SkillUseRail）。
// 对齐 Python: TeamManager._sync_team_shared_skills_to_agent_global(spec) (L1036-1041)
func (m *TeamManager) syncTeamSharedSkillsToAgentGlobal(spec *atschema.TeamAgentSpec) {
	teamSharedSkillsDir := resolveTeamSharedSkillsDir(spec)
	globalSkillsDir := workspace.AgentSkillsDir()
	syncSkillsDir(teamSharedSkillsDir, globalSkillsDir)
}

// syncSkillsDir 将 source 中每个有效技能目录复制到 target（最新版本胜出）。
// 对齐 Python: _sync_skills_dir(source, target) (L87-107)
//
// 有效技能 = 含 SKILL.md 的子目录；target 已有同名目录先删除再复制（rmtree+copytree）。
// source 非目录直接返回。synced>0 时输出 info 日志。
func syncSkillsDir(source string, target string) {
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		logger.Warn(logComponent).Err(err).Str("target", target).
			Msg("[TeamManager] syncSkillsDir mkdir failed")
		return
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("source", source).
			Msg("[TeamManager] syncSkillsDir readdir failed")
		return
	}
	synced := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillMD := filepath.Join(source, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillMD); err != nil {
			continue
		}
		dest := filepath.Join(target, entry.Name())
		if _, err := os.Stat(dest); err == nil {
			if err := os.RemoveAll(dest); err != nil {
				logger.Warn(logComponent).Err(err).Str("dest", dest).
					Msg("[TeamManager] syncSkillsDir rmtree failed")
				continue
			}
		}
		if err := copyTreeMerge(filepath.Join(source, entry.Name()), dest); err != nil {
			logger.Warn(logComponent).Err(err).Str("dest", dest).
				Msg("[TeamManager] syncSkillsDir copytree failed")
			continue
		}
		synced++
	}
	if synced > 0 {
		logger.Info(logComponent).
			Int("count", synced).
			Str("source", source).
			Str("target", target).
			Msg("[TeamManager] synced skills")
	}
}

// copyTreeMerge 递归复制目录树，目标存在则逐项合并覆盖（shutil.copytree(dirs_exist_ok=True) 等效）。
func copyTreeMerge(source string, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("mkdir target: %w", err)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("readdir source: %w", err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(source, entry.Name())
		dstPath := filepath.Join(target, entry.Name())
		if entry.IsDir() {
			if err := copyTreeMerge(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", srcPath, err)
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dstPath, err)
		}
	}
	return nil
}
