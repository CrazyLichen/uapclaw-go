package team

import (
	"context"
	"strings"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentCustomizer Agent 定制器函数类型。
// 对齐 Python: build_agent_customizer 返回的 customizer(agent, member_name, role) 闭包
type AgentCustomizer func(ctx context.Context, agent interfaces.DeepAgentInterface, memberName string, role string) error

// ──────────────────────────── 导出函数 ────────────────────────────

// GetEnrichedTeamSpec 获取增强的 TeamAgentSpec。
// 对齐 Python: TeamManager.get_enriched_team_spec(session_id, deep_agent, ...)
//
// Python 步骤：
//  1. config_base = get_config()
//  2. await self._ensure_postgresql_for_leader(config_base)
//  3. spec = self._load_team_spec(session_id)
//  4. self._apply_session_scoped_team_name(spec, session_id=session_id)
//  5. self.apply_team_plan_mode(spec, request_metadata=)
//  6. spec.agent_customizer = self.build_agent_customizer(...)
//  7. return spec
func (m *TeamManager) GetEnrichedTeamSpec(
	ctx context.Context,
	sessionID string,
	deepAgent interfaces.DeepAgentInterface,
	requestID string,
	channelID string,
	requestMetadata map[string]any,
) *atschema.TeamAgentSpec {
	// 步骤 1-2: 加载 spec（Go 差异：configBase 由 loadTeamSpec 内部获取或调用方注入）
	spec := loadTeamSpec(sessionID)
	if spec == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("loadTeamSpec 返回 nil，使用默认 spec")
		s := atschema.NewTeamAgentSpec()
		spec = &s
	}

	// 步骤 3: 应用 session 作用域 team name
	m.applySessionScopedTeamName(spec, sessionID)

	// 步骤 4: 应用 team plan mode
	m.ApplyTeamPlanMode(spec, requestMetadata)

	// 步骤 5: 构建 agent customizer 并设置到 spec
	customizer := m.BuildAgentCustomizer(spec, deepAgent, sessionID, requestID, channelID, requestMetadata)
	if customizer != nil {
		spec.AgentCustomizer = customizer
	}

	// 步骤 6: 同步 team identity metadata
	m.SyncTeamIdentityMetadata(ctx, sessionID, spec.TeamName)

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("team_name", spec.TeamName).
		Bool("has_customizer", spec.AgentCustomizer != nil).
		Msg("GetEnrichedTeamSpec 完成")

	return spec
}

// ApplyTeamPlanMode 应用 team plan mode。
// 对齐 Python: TeamManager.apply_team_plan_mode(spec, *, request_metadata)
//
// Python 步骤：
//  1. mode = str((request_metadata or {}).get("mode") or "").strip().lower()
//  2. if mode == "team.plan": spec.enable_team_plan = True
func (m *TeamManager) ApplyTeamPlanMode(spec *atschema.TeamAgentSpec, requestMetadata map[string]any) {
	mode := ""
	if requestMetadata != nil {
		if raw, ok := requestMetadata["mode"].(string); ok {
			mode = strings.TrimSpace(raw)
		}
	}
	if strings.EqualFold(mode, "team.plan") {
		spec.EnableTeamPlan = true
		logger.Info(logComponent).Msg("ApplyTeamPlanMode: 启用 team.plan 模式")
	}
}

// SyncTeamIdentityMetadata 将 team_name 写入 session metadata。
// 对齐 Python: TeamManager.sync_team_identity_metadata(session_id, team_name)
func (m *TeamManager) SyncTeamIdentityMetadata(ctx context.Context, sessionID string, teamName string) {
	if sessionID == "" || teamName == "" {
		return
	}
	teamNamePtr := &teamName
	session.UpdateSessionMetadata(session.SessionMetadataUpdate{
		SessionID: sessionID,
		TeamName:  teamNamePtr,
	})
	logger.Debug(logComponent).Str("session_id", sessionID).Str("team_name", teamName).
		Msg("同步 team identity metadata")
}

// BuildAgentCustomizer 构建 agent customizer 闭包。
// 对齐 Python: TeamManager.build_agent_customizer(spec, deep_agent, session_id, ...)
//
// Python 步骤（极长闭包，约 200 行）：
//  1. 解析 global_skills_dir, team_ws_skills_dir
//  2. 定义 resolve_member_spec, resolve_member_skills, copy_member_configured_skills,
//     build_member_skill_state, write_member_skill_state 等内部函数
//  3. 定义 customizer(agent, member_name, role) 闭包：
//     a. 继承能力卡
//     b. 技能同步（copy member skills + sync team skills + write skills_state）
//     c. code adapter 配置
//     d. build_member_rails + 注册 TeamSkillEvolutionRail/SkillCreateRail
//     e. 注册 TeamRailContext（leader）
//     f. 注册 member runtime tools（CronRuntimeBridge + SendFileToolkit）
//  4. return customizer
//
// Go 差异：Python 的 customizer 是一个闭包，Go 中改为 AgentCustomizer 函数类型。
// 当前实现骨架，内部步骤待后续回填。
func (m *TeamManager) BuildAgentCustomizer(
	spec *atschema.TeamAgentSpec,
	deepAgent interfaces.DeepAgentInterface,
	sessionID string,
	requestID string,
	channelID string,
	requestMetadata map[string]any,
) AgentCustomizer {
	if spec == nil || deepAgent == nil {
		return nil
	}

	// 捕获闭包变量
	capturedSpec := spec
	capturedSessionID := sessionID
	capturedRequestID := requestID
	capturedChannelID := channelID
	capturedRequestMetadata := requestMetadata
	mgr := m

	// 对齐 Python: 返回 customizer(agent, member_name, role) 闭包
	return func(ctx context.Context, agent interfaces.DeepAgentInterface, memberName string, role string) error {
		// 步骤 a: 继承能力卡 — ⤵️ 待回填
		_ = capturedSpec

		// 步骤 b: 技能同步（copy member skills + sync team skills + write skills_state）— ⤵️ 待回填

		// 步骤 c: code adapter 配置 — ⤵️ 待回填

		// 步骤 d: build_member_rails + 注册 TeamSkillEvolutionRail/SkillCreateRail — ⤵️ 待回填

		// 步骤 e: 注册 TeamRailContext（leader）— ⤵️ 待回填

		// 步骤 f: 注册 member runtime tools
		mgr.RegisterMemberRuntimeTools(agent, capturedSessionID, &capturedRequestID, &capturedChannelID, capturedRequestMetadata)

		logger.Info(logComponent).
			Str("session_id", capturedSessionID).
			Str("member_name", memberName).
			Str("role", role).
			Msg("AgentCustomizer 闭包执行（骨架，内部步骤待回填）")

		return nil
	}
}

// RegisterMemberRuntimeTools 注册成员运行时工具（CronRuntimeBridge + SendFileToolkit）。
// 对齐 Python: TeamManager.register_member_runtime_tools(agent, *, session_id, ...)
//
// Python 步骤：
//  1. 创建 CronRuntimeBridge → cron_runtime.build_tools → Runner.resource_mgr.add_tool → agent.ability_manager.add
//  2. 检查 send_file_allowed 配置
//  3. 创建 SendFileToolkit → 注册到 Runner.resource_mgr + agent.ability_manager
func (m *TeamManager) RegisterMemberRuntimeTools(
	agent interfaces.DeepAgentInterface,
	sessionID string,
	requestID *string,
	channelID *string,
	requestMetadata map[string]any,
) {
	// ⤵️(#9.72) 完整实现 —待 CronRuntimeBridge / SendFileToolkit 回填
}

// NormalizeDistributedTransportFields 公共包装：分布式传输字段标准化。
// 对齐 Python: TeamManager.normalize_distributed_transport_fields(config_base, team_cfg)
func (m *TeamManager) NormalizeDistributedTransportFields(configBase map[string]any, teamCfg map[string]any) map[string]any {
	// ⤵️(#9.72) 完整实现 — 待 distributed_runtime 回填
	return teamCfg
}

// ParsePort 解析并验证端口值。
// 对齐 Python: TeamManager.parse_port(value, default, field_name)
func (m *TeamManager) ParsePort(value any, defaultPort int, fieldName string) int {
	// ⤵️(#9.72) 完整实现 — 待 distributed_runtime.parse_port 回填
	return defaultPort
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSessionScopedTeamName 构建 session 作用域的 team name。
// 对齐 Python: TeamManager._build_session_scoped_team_name(team_name, session_id)
//
// Python 步骤：
//  1. base_name = str(team_name or "").strip() or "team"
//  2. session_suffix = re.sub(r"[^A-Za-z0-9_.-]+", "_", str(session_id or "").strip())
//  3. session_suffix = session_suffix.strip("._-")
//  4. if not session_suffix: return base_name
//  5. if base_name.endswith(f"_{session_suffix}"): return base_name
//  6. return f"{base_name}_{session_suffix}"
func buildSessionScopedTeamName(teamName string, sessionID string) string {
	baseName := teamName
	if baseName == "" {
		baseName = "team"
	}
	if sessionID == "" {
		return baseName
	}
	// 简化正则替换：将非字母数字字符替换为下划线
	suffix := sanitizeSessionSuffix(sessionID)
	if suffix == "" {
		return baseName
	}
	suffix = trimDotsUnderscores(suffix)
	if suffix == "" {
		return baseName
	}
	if len(baseName) > len(suffix)+1 && baseName[len(baseName)-len(suffix)-1:] == "_"+suffix {
		return baseName
	}
	return baseName + "_" + suffix
}

// sanitizeSessionSuffix 将 sessionID 中非字母数字的字符替换为下划线。
func sanitizeSessionSuffix(s string) string {
	var result []byte
	for _, c := range s {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-' {
			result = append(result, byte(c))
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}

// trimDotsUnderscores 去除首尾的点号和下划线。
func trimDotsUnderscores(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == '.' || s[start] == '_' || s[start] == '-') {
		start++
	}
	for end > start && (s[end-1] == '.' || s[end-1] == '_' || s[end-1] == '-') {
		end--
	}
	return s[start:end]
}

// loadTeamSpec 加载团队配置并构建 TeamAgentSpec。
// 对齐 Python: TeamManager._load_team_spec(session_id)
//
// Python 步骤：
//  1. config_base = get_config()
//  2. spec_dict = load_team_spec_dict(config_base)
//  3. spec_dict = normalize_team_identity_fields(spec_dict)
//  4. 分布式模式标准化 — ⤵️ 待回填
//  5. return TeamAgentSpec(**spec_dict)
//
// Go 差异：Python 使用全局 get_config()，Go 通过参数注入 configBase。
// 当 configBase 为 nil 时，返回默认 spec。
func loadTeamSpec(sessionID string, configBase ...map[string]any) *atschema.TeamAgentSpec {
	var cb map[string]any
	if len(configBase) > 0 {
		cb = configBase[0]
	}
	if cb == nil {
		// Go 差异：无 configBase 时返回默认 spec
		logger.Warn(logComponent).Str("session_id", sessionID).
			Msg("loadTeamSpec: configBase 为 nil，使用默认 spec")
		s := atschema.NewTeamAgentSpec()
		return &s
	}

	// 步骤 2: 构建 spec 字典
	specDict := LoadTeamSpecDict(cb)

	// 步骤 3: 标准化身份字段
	specDict = normalizeTeamIdentityFields(specDict)

	// 步骤 4: 分布式模式标准化 — ⤵️ 待回填

	// 步骤 5: 从 dict 构建 TeamAgentSpec
	spec := atschema.NewTeamAgentSpecFromDict(specDict)

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("team_name", spec.TeamName).
		Msg("loadTeamSpec 完成")

	return spec
}

// applySessionScopedTeamName 应用 session 作用域的 team name 到 spec。
// 对齐 Python: TeamManager._apply_session_scoped_team_name(spec, session_id=session_id)
func (m *TeamManager) applySessionScopedTeamName(spec *atschema.TeamAgentSpec, sessionID string) {
	if spec == nil {
		return
	}
	scopedName := buildSessionScopedTeamName(spec.TeamName, sessionID)
	if scopedName != spec.TeamName {
		logger.Info(logComponent).
			Str("session_id", sessionID).
			Str("original_name", spec.TeamName).
			Str("scoped_name", scopedName).
			Msg("应用 session 作用域 team name")
		spec.TeamName = scopedName
	}
}
