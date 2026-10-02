package team

import (
	"context"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
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
//
// Go 差异：此方法涉及大量 Python 专属逻辑（PostgreSQL、config 加载、customizer 闭包），
// 当前作为编排入口，具体步骤由调用方或后续回填完成。
func (m *TeamManager) GetEnrichedTeamSpec(
	sessionID string,
	deepAgent interfaces.DeepAgentInterface,
	requestID *string,
	channelID *string,
	requestMetadata map[string]any,
) *atschema.TeamAgentSpec {
	// ⤵️(#9.72) 完整实现 — 待 config_loader / distributed_runtime / team_runtime_inheritance 回填
	return nil
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
		if m, ok := requestMetadata["mode"].(string); ok {
			mode = m
		}
	}
	_ = mode
	// ⤵️(#9.72) 设置 spec.EnableTeamPlan = true — 待 TeamAgentSpec 回填
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
// 当前返回 nil，待后续回填。
func (m *TeamManager) BuildAgentCustomizer(
	spec *atschema.TeamAgentSpec,
	deepAgent interfaces.DeepAgentInterface,
	sessionID string,
	requestID *string,
	channelID *string,
	requestMetadata map[string]any,
) AgentCustomizer {
	// ⤵️(#9.72) 完整实现 — 待 team_runtime_inheritance / rail_manager / skill_manager 回填
	return nil
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
func loadTeamSpec(sessionID string) *atschema.TeamAgentSpec {
	// ⤵️(#9.72) 完整实现 — 待 config_loader / distributed_runtime 回填
	return nil
}
