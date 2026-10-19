package team

import (
	"context"
	"strings"

	agentteamsmodels "github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentCustomizer Agent 定制器函数类型。
// S-01: 全项目唯一类型定义在 agent_teams/schema 包（依赖最底层），
// 此处保留类型别名供 team 包内使用，签名一致：ctx + error + memberName string。
type AgentCustomizer = atschema.AgentCustomizer

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// GetEnrichedTeamSpec 获取增强的 TeamAgentSpec。
// 对齐 Python: TeamManager.get_enriched_team_spec(session_id, deep_agent, ...) (team_manager.py L354-381)
//
// Python 步骤：
//  1. config_base = get_config()
//  2. await self._ensure_postgresql_for_leader(config_base)
//  3. spec = self._load_team_spec(session_id)
//  4. self._apply_session_scoped_team_name(spec, session_id=session_id)
//  5. self.apply_team_plan_mode(spec, request_metadata=)
//  6. spec.agent_customizer = self.build_agent_customizer(...)
//
// S-03: 对齐 Python——get_enriched_team_spec 不调用 sync_team_identity_metadata；
// 身份同步仅在 runtime_ready 处理路径（adapter 层 SyncTeamIdentityMetadata，
// 含 activationKind 守卫）执行，原此处的无守卫调用已删除。
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
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("loadTeamSpec returned nil, using default spec")
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

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("team_name", spec.TeamName).
		Bool("has_customizer", spec.AgentCustomizer != nil).
		Msg("GetEnrichedTeamSpec completed")

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
		logger.Info(logComponent).Msg("ApplyTeamPlanMode: enabling team.plan mode")
	}
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
			Msg("AgentCustomizer closure executed (skeleton, internal steps pending backfill)")

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
//
// 委托 distributed_runtime.go 纯函数实现。
func (m *TeamManager) NormalizeDistributedTransportFields(configBase map[string]any, teamCfg map[string]any) map[string]any {
	return NormalizeDistributedTransportFields(configBase, teamCfg)
}

// ParsePort 解析并验证端口值。
// 对齐 Python: TeamManager.parse_port(value, default, field_name)
//
// 委托 distributed_runtime.go 纯函数实现。
func (m *TeamManager) ParsePort(value any, defaultPort int, fieldName string) int {
	return ParsePort(value, defaultPort, fieldName)
}

// ──────────────────────────── 常量 ────────────────────────────
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
	// M-22: 使用 strings.HasSuffix，对齐 Python base_name.endswith(f"_{session_suffix}")
	if strings.HasSuffix(baseName, "_"+suffix) {
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

// poolEntryToMap 将 ModelPoolEntry 转为 map（JSON 桥接）。
// 对齐 Python: pool_entry.model_dump()
func poolEntryToMap(entry *agentteamsmodels.ModelPoolEntry) map[string]any {
	return map[string]any{
		"model_name":   entry.ModelName,
		"api_key":      entry.APIKey,
		"api_base_url": entry.APIBaseURL,
		"api_provider": entry.APIProvider,
		"description":  entry.Description,
		"model_id":     entry.ModelID,
		"metadata":     entry.Metadata,
	}
}

// loadTeamSpec 加载团队配置并构建 TeamAgentSpec。
// 对齐 Python: TeamManager._load_team_spec(session_id) (L291-352)
//
// Python 步骤：
//  1. config_base = get_config()
//  2. 分布式依赖检查（缺依赖回退 local）
//  3. spec_dict = load_team_spec_dict(config_base)
//  4. spec_dict = normalize_team_identity_fields(spec_dict)
//  5. 分布式传输字段标准化
//  6. models.defaults > 1 条时注入 model_pool + model_pool_strategy
//  7. return TeamAgentSpec(**spec_dict)
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
			Msg("loadTeamSpec: configBase is nil, using default spec")
		s := atschema.NewTeamAgentSpec()
		return &s
	}

	// 步骤 2: 分布式模式依赖检查（缺依赖回退 local）
	// 对齐 Python: TeamManager._load_team_spec L296-318
	if IsDistributedMode(cb) {
		missing := MissingDistributedDependencies(cb)
		if len(missing) > 0 {
			missingList := strings.Join(missing, ", ")
			logger.Warn(logComponent).Str("missing", missingList).
				Msg("[TeamManager][MISSING_DISTRIBUTE_DEPS]")
			logger.Error(logComponent).
				Msg("[TeamManager][FALLBACK_TO_LOCAL] distributed runtime is not available; downgraded to local mode for current process")
			logger.Warn(logComponent).
				Msg("[TeamManager][ACTION] install via: pip install -e \".[distribute]\" or uv sync --extra distribute")
			cb = FallbackDistributedToLocal(cb)
		}
	}

	// 步骤 3: 构建 spec 字典
	specDict := LoadTeamSpecDict(cb)

	// 步骤 4: 标准化身份字段
	specDict = normalizeTeamIdentityFields(specDict)

	// 步骤 5: 分布式传输字段标准化（对齐 Python L317-318）
	if IsDistributedMode(cb) {
		specDict = NormalizeDistributedTransportFields(cb, specDict)
	}

	// 步骤 6: model_pool 注入
	// 对齐 Python: TeamManager._load_team_spec L320-350
	// models.defaults 多于一条时填充 model_pool 并设置 model_pool_strategy，
	// 使团队成员可从池中分配不同模型端点。
	defaultModels := config.GetDefaultModels(cb)
	if len(defaultModels) > 1 {
		poolEntries := make([]any, 0, len(defaultModels))
		for _, entry := range defaultModels {
			mcc, _ := entry["model_client_config"].(map[string]any)
			if mcc == nil {
				continue
			}
			modelName, _ := mcc["model_name"].(string)
			if modelName == "" {
				continue
			}
			apiKey, _ := mcc["api_key"].(string)
			apiBase, _ := mcc["api_base"].(string)
			apiProvider, _ := mcc["client_provider"].(string)

			// metadata.client = mcc 排除四键且值非 nil；metadata.request = mco 浅拷贝（恒非 nil）
			clientMeta := map[string]any{}
			excluded := map[string]bool{
				"model_name": true, "api_key": true, "api_base": true, "client_provider": true,
			}
			for k, v := range mcc {
				if excluded[k] || v == nil {
					continue
				}
				clientMeta[k] = v
			}
			mco, _ := entry["model_config_obj"].(map[string]any)
			if mco == nil {
				mco = map[string]any{}
			}
			requestMeta := make(map[string]any, len(mco))
			for k, v := range mco {
				requestMeta[k] = v
			}

			poolEntry := agentteamsmodels.NewModelPoolEntry(
				modelName, apiKey, apiBase, apiProvider,
				agentteamsmodels.WithMetadata(map[string]any{
					"client":  clientMeta,
					"request": requestMeta,
				}),
			)
			// JSON 桥接：ModelPoolEntry → map（specDict 经 NewTeamAgentSpecFromDict 反序列化）
			poolEntries = append(poolEntries, poolEntryToMap(poolEntry))
		}
		if len(poolEntries) > 0 {
			specDict["model_pool"] = poolEntries
			specDict["model_pool_strategy"] = "by_model_name"
		}
	}

	// 步骤 7: 从 dict 构建 TeamAgentSpec
	spec := atschema.NewTeamAgentSpecFromDict(specDict)

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("team_name", spec.TeamName).
		Msg("loadTeamSpec completed")

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
			Msg("applying session-scoped team name")
		spec.TeamName = scopedName
	}
}

// ──────────────────────────── 全局变量 ────────────────────────────
