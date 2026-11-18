package team

import (
	"context"
	"fmt"
	"strings"

	agentteamsmodels "github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	agentcron "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/cron"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	resourcesmanager "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	swarm "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	cronbridge "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/cron"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentCustomizer Agent 定制器函数类型。
// S-01: 全项目唯一类型定义在 agent_teams/schema 包（依赖最底层）。
type AgentCustomizer = atschema.AgentCustomizer

// ──────────────────────────── 导出函数 ────────────────────────────

// ApplyTeamPlanMode 应用 team plan mode。
// 对齐 Python: TeamManager.apply_team_plan_mode(spec, *, request_metadata)
//
// Python 步骤：
//
//	Python: 1. mode = str((request_metadata or {}).get("mode") or "").strip().lower()
//	Python: 2. if mode == "team.plan": spec.enable_team_plan = True
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

// GetSwarmEnrichedTeamSpec 基于 provider 装配构建团队 spec（无父 DeepAgent）。
// 对齐 Python: TeamManager.get_swarm_enriched_team_spec (team_manager.py:1958-2077)
//
// 成员能力全部来自共享配置源（enrich_team_spec_for_swarm），不再继承
// 预构建的单 agent。偏差 D-15.16-c8 收尾：本方法是 Go 侧 seed 生产与
// enrich 的唯一接线点（旧 GetEnrichedTeamSpec 路径已随本批删除——上游
// get_enriched_team_spec 已被 provider 装配路径整体替代，build_agent_customizer
// 链上游同删）。
//
// Python 步骤：
//
//	Python: 1. config_base = get_config()（:2009）
//	Python: 2. self._team_evolution_enabled[session_id] = get_skill_evolution_enabled(config_base)（:2013）
//	Python: 3. await self._ensure_postgresql_for_leader(config_base)（:2014）
//	Python: 4. spec, has_binding = self._load_session_team_spec(...)（:2015）
//	Python: 5. if not has_binding: self._apply_session_scoped_team_name(spec, session_id)（:2018-2019）
//	Python: 6. self.apply_team_plan_mode(spec, request_metadata=)（:2021）
//	Python: 7. enrich_team_spec_for_swarm(spec, ...)（:2023-2033）
//	Python: 8. swarmflow_config 三段（:2035-2052）⤴️swarmflow 章节挂账
//	Python: 9. dispatch_mode 段（:2053-2059）
//	Python: 10. bootstrap_mode 段 + framework/scheduled 组合校验（:2060-2076）
//
// 挂账（IMPLEMENTATION_PLAN 15.16c 行登记）：
//   - 步骤 4 的 binding store / session model selection 恢复段 ⤴️server runtime 章节
//     （Go 暂按 has_binding=false 语义：恒 applySessionScopedTeamName）
//   - 步骤 8 swarmflow_config 段 ⤴️swarmflow 章节
//   - agent_group_name 形参 ⤴️15.16e
//   - dispatch_mode/bootstrap_mode 的 session 级 resolver（_resolve_session_team_*）
//     ⤴️25.2 调度模式章节（当前由调用方显式传参，空串=None 语义）
func (m *TeamManager) GetSwarmEnrichedTeamSpec(
	ctx context.Context,
	sessionID string,
	mode string,
	projectDir string,
	trustedDirs []string,
	requestID string,
	userID string,
	channelID string,
	requestMetadata map[string]any,
	dispatchMode string,
	bootstrapMode string,
) (*atschema.TeamAgentSpec, error) {
	// 步骤 1-2: 配置加载 + 演进开关记账（:2009/:2013）
	configBase, err := teamLoadConfig()
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("session_id", sessionID).
			Msg("GetSwarmEnrichedTeamSpec: load config failed")
		configBase = nil
	}
	m.mu.Lock()
	m.teamEvolutionEnabled[sessionID] = config.SkillEvolutionEnabled(configBase)
	m.mu.Unlock()

	// 步骤 3: leader PG 编排（:2014）
	EnsurePostgresqlForLeader(ctx, configBase)

	// 步骤 4-5: spec 加载 + session 作用域 team name（:2015-2019）。
	// binding/model_selection 恢复段 ⤴️server runtime 章节，暂按无绑定语义。
	spec, specErr := loadTeamSpec(sessionID, configBase)
	if specErr != nil {
		// 对齐 Python raise 沿调用链传播（_load_team_spec 无 try/except）
		logger.Error(logComponent).Str("session_id", sessionID).Err(specErr).
			Msg("GetSwarmEnrichedTeamSpec: loadTeamSpec failed")
		return nil, specErr
	}
	if spec == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).
			Msg("loadTeamSpec returned nil, using default spec")
		s := atschema.NewTeamAgentSpec()
		spec = &s
	}
	m.applySessionScopedTeamName(spec, sessionID)

	// 步骤 6: team plan mode（:2021）
	m.ApplyTeamPlanMode(spec, requestMetadata)

	// 步骤 7: provider 装配 enrich——context 构造段（:2023-2033；assembly.py:456-541）。
	// rails 目录注册 ⤵️15.7/15.8、成员 spec 重写/mcp/agent_group ⤵️15.16e（D-15.16-c3）。
	params := swarm.SeedParamsFromRequestMetadata(sessionID, requestMetadata)
	params.Mode = mode
	if projectDir != "" {
		params.ProjectDir = projectDir
	}
	params.TrustedDirs = trustedDirs
	params.RequestID = requestID
	params.UserID = userID
	params.ChannelID = channelID
	if enrichErr := swarm.EnrichTeamSpecForSwarmSeed(spec, params); enrichErr != nil {
		logger.Error(logComponent).Str("session_id", sessionID).Err(enrichErr).
			Msg("GetSwarmEnrichedTeamSpec: enrich seed failed")
		return nil, enrichErr
	}

	// 步骤 8: swarmflow_config 三段（:2035-2052）⤴️swarmflow 章节挂账

	// 步骤 9: dispatch_mode 段（:2053-2059）：非空才覆盖；非 scheduled 时
	// 任务验证、成员互信消息与子任务拆分全部关闭。
	if dispatchMode != "" {
		spec.DispatchMode = dispatchMode
		if dispatchMode != "scheduled" {
			spec.EnableTaskVerification = false
			spec.EnableMemberMessaging = false
			spec.MaxSubtaskDepth = 0
		}
	}

	// 步骤 10: bootstrap_mode 段（:2060-2076）：非空才覆盖；framework
	// 必须搭配 scheduled dispatch，组合非法在请求边界直接拒绝（:2071-2076）。
	if bootstrapMode != "" {
		spec.BootstrapMode = bootstrapMode
	}
	if spec.BootstrapMode == "framework" && spec.DispatchMode != "scheduled" {
		// 对齐 Python :2071-2076 内联 raise（消息形状见 Python 原文；
		// spec 构造期同语义校验见 blueprint.validateBootstrapMode）
		return nil, fmt.Errorf(
			"team_bootstrap_mode='framework' requires scheduled dispatch (effective dispatch_mode=%q)",
			spec.DispatchMode)
	}

	return spec, nil
}

// RegisterMemberRuntimeTools 注册成员运行时工具（CronRuntimeBridge + SendFileToolkit）。
// 对齐 Python: TeamManager.register_member_runtime_tools(agent, *, session_id, ...)
//
// Python 步骤：
//
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
	agentID := ""
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}
	// 11.10 已回填：CronRuntimeBridge 工具注册
	// Python: cron_runtime = CronRuntimeBridge() + build_tools(context=..., agent_id=..., language=...)
	cronRuntime := cronbridge.NewCronRuntimeBridge()
	// Python: channel_id=channel_id or "web"
	channel := ""
	if channelID != nil {
		channel = strings.TrimSpace(*channelID)
	}
	if channel == "" {
		channel = "web"
	}
	// Python: language=getattr(agent.deep_config, "language", "cn")
	language := "cn"
	if dc := agent.DeepConfig(); dc != nil && strings.TrimSpace(dc.Language) != "" {
		language = strings.TrimSpace(dc.Language)
	}
	// Python L497-503: SimpleNamespace(tool_scope=f"team_member_{agent_id or 'unknown'}", ...)
	// metadata 直接传 request_metadata（不注入 request_id，与 deep_adapter 步骤 9 的绑定不同）
	scopeAgentID := agentID
	if scopeAgentID == "" {
		scopeAgentID = "unknown"
	}
	cronCtx := &agentcron.CronToolContext{
		ChannelID:         channel,
		SessionID:         sessionID,
		Metadata:          copyMetadataMap(requestMetadata),
		Mode:              "team",
		ToolScopeOverride: "team_member_" + scopeAgentID,
	}
	// Python L505-517: try/except 包裹 build_tools + 注册循环
	tools := cronRuntime.BuildTools(cronCtx, agentID, language)
	registeredCount := 0
	resourceMgr := runner.GetResourceMgr()
	abilityMgr := agent.AbilityManager()
	for _, cronTool := range tools {
		card := cronTool.Card()
		// Python L512: if not Runner.resource_mgr.get_tool(cron_tool.card.id): add_tool
		if existing, gerr := resourceMgr.GetTool([]string{card.ID}); gerr != nil || len(existing) == 0 {
			if err := resourceMgr.AddTool(cronTool); err != nil {
				logger.Warn(logComponent).Err(err).Str("tool", card.Name).
					Msg("[TeamManager] cron tool add to resource manager failed")
				continue
			}
		}
		abilityMgr.Add(card)
		registeredCount++
	}
	logger.Info(logComponent).
		Str("agent_id", agentID).
		Int("registered", registeredCount).
		Msg("[TeamManager] Registered cron tools for member")

	if requestID == nil || *requestID == "" || channelID == nil || *channelID == "" {
		logger.Info(logComponent).
			Msg("[TeamManager] SendFileToolkit skipped: missing request_id or channel_id")
		return
	}
	// ── 步骤 f-send_file：SendFileToolkit 注册（10.6.24b 已回填）──
	// Python: register_member_runtime_tools L519-555（整块 try/except 兜底 → Go recover）
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("panic", r).
					Msg("[TeamManager] SendFileToolkit registration failed")
			}
		}()
		// Python L524-532: 三态开关（None → web 默认 true，其余默认 false）
		var cfgData map[string]any
		// 对齐 Python get_config()：normalize 一并应用（2026-10-28 审查接线）。
		// 注：normalize 补 web 缺省 true 后，三态的 None→web 分支仍保留——
		// 显式 send_file_allowed: false 时 sendFileConfigured=true 不触发兜底
		cfg, cfgErr := config.New("", config.WithNormalize(config.NormalizeConfig))
		if cfgErr != nil {
			logger.Warn(logComponent).Err(cfgErr).
				Msg("[TeamManager] SendFileToolkit config init failed")
			return
		}
		cfgData, loadErr := cfg.Load()
		if loadErr != nil || cfgData == nil {
			logger.Warn(logComponent).Err(loadErr).
				Msg("[TeamManager] SendFileToolkit config load failed")
			return
		}
		sendFileEnabled := false
		sendFileConfigured := false
		if channelsRaw, ok := cfgData["channels"]; ok {
			if channelsMap, ok := channelsRaw.(map[string]any); ok {
				if chRaw, ok := channelsMap[*channelID]; ok {
					if chMap, ok := chRaw.(map[string]any); ok {
						if b, ok := chMap["send_file_allowed"].(bool); ok {
							sendFileEnabled = b
							sendFileConfigured = true
						}
					}
				}
			}
		}
		// Python: if send_file_enabled is None: send_file_enabled = (channel_id == "web")
		if !sendFileConfigured {
			sendFileEnabled = *channelID == "web"
		}
		if !sendFileEnabled {
			logger.Info(logComponent).
				Str("channel_id", *channelID).
				Msg("[TeamManager] SendFileToolkit skipped: send_file_allowed=False for channel")
			return
		}

		// Python L540-542: ability 前缀扫描移除旧卡
		abilityMgr := agent.AbilityManager()
		for _, existing := range abilityMgr.List() {
			if strings.HasPrefix(existing.AbilityName(), "send_file_to_user") {
				abilityMgr.Remove(existing.AbilityName())
			}
		}

		// Python L544-552: 新建 toolkit 注册
		// 有意偏差（spec 偏差 #2）：WithRefresh() 显式替换旧实例——
		// Python add_tool 重复注册静默丢弃异常（旧 toolkit 残留 ResourceMgr），
		// Go 侧修正为显式替换，语义对齐"每次请求重新注册"
		toolkit := swarmtools.NewSendFileToolkit(*requestID, sessionID, *channelID, swarmtools.SendFileRuntimeContext{Metadata: requestMetadata})
		resourceMgr := runner.GetResourceMgr()
		for _, sfTool := range toolkit.GetTools() {
			if err := resourceMgr.AddTool(sfTool, resourcesmanager.WithRefresh()); err != nil {
				logger.Warn(logComponent).Err(err).
					Str("tool", sfTool.Card().Name).
					Msg("[TeamManager] send_file tool add failed")
			}
			abilityMgr.Add(sfTool.Card())
		}
		logger.Info(logComponent).
			Str("channel_id", *channelID).
			Msg("[TeamManager] SendFileToolkit registered")
	}()
}

// copyMetadataMap 浅拷贝 metadata（nil 保留 nil，对齐 Python metadata 原样传递）。
func copyMetadataMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// NormalizeDistributedTransportFields 公共包装：分布式传输字段标准化。
// 对齐 Python: TeamManager.normalize_distributed_transport_fields(config_base, team_cfg)
//
// 委托 distributed_runtime.go 纯函数实现。
// M-04: 签名加 error（对齐 Python parse_port raise 传播）。
func (m *TeamManager) NormalizeDistributedTransportFields(configBase map[string]any, teamCfg map[string]any) (map[string]any, error) {
	return NormalizeDistributedTransportFields(configBase, teamCfg)
}

// ParsePort 解析并验证端口值。
// 对齐 Python: TeamManager.parse_port(value, default, field_name)
//
// 委托 distributed_runtime.go 纯函数实现。
// M-04: 签名加 error（对齐 Python raise ValueError）。
func (m *TeamManager) ParsePort(value any, defaultPort int, fieldName string) (int, error) {
	return ParsePort(value, defaultPort, fieldName)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSessionScopedTeamName 构建 session 作用域的 team name。
// 对齐 Python: TeamManager._build_session_scoped_team_name(team_name, session_id)
//
// Python 步骤：
//
//	Python: 1. base_name = str(team_name or "").strip() or "team"
//	Python: 2. session_suffix = re.sub(r"[^A-Za-z0-9_.-]+", "_", str(session_id or "").strip())
//	Python: 3. session_suffix = session_suffix.strip("._-")
//	Python: 4. if not session_suffix: return base_name
//	Python: 5. if base_name.endswith(f"_{session_suffix}"): return base_name
//	Python: 6. return f"{base_name}_{session_suffix}"
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

// recoverStep 步骤级 panic 恢复（对齐 Python 各步骤独立 try/except 吞错）。
func recoverStep(stepName string) {
	if r := recover(); r != nil {
		logger.Warn(logComponent).Str("step", stepName).Any("panic", r).
			Msg("[TeamManager] customizer step failed")
	}
}

// firstNonEmpty 返回首个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// loadTeamSpec 加载团队配置并构建 TeamAgentSpec。
// 对齐 Python: TeamManager._load_team_spec(session_id) (L291-352)
//
// Python 步骤：
//
//	Python: 1. config_base = get_config()
//	2. 分布式依赖检查（缺依赖回退 local）
//	Python: 3. spec_dict = load_team_spec_dict(config_base)
//	Python: 4. spec_dict = normalize_team_identity_fields(spec_dict)
//	5. 分布式传输字段标准化
//	6. models.defaults > 1 条时注入 model_pool + model_pool_strategy
//	Python: 7. return TeamAgentSpec(**spec_dict)
//
// Go 差异：Python 使用全局 get_config()，Go 通过参数注入 configBase。
// M-38: 无参调用时 config.New("").Load() 兜底（对齐 Python get_config()，
// harness 邻包已有三处同款 sweeper.go/memoryrpc）；加载失败才回退默认 spec
// （测试可控）。model_pool 注入对齐 Python L320-350。
// M-04: 端口非法等配置错误返回 error（对齐 Python _load_team_spec 无 try/except，
// raise 沿调用链传播到 get_enriched_team_spec 调用方）。
func loadTeamSpec(sessionID string, configBase ...map[string]any) (*atschema.TeamAgentSpec, error) {
	var cb map[string]any
	if len(configBase) > 0 {
		cb = configBase[0]
	}
	if cb == nil {
		// M-38: 对齐 Python config_base = get_config()——无参调用走全局配置加载
		// 对齐 Python get_config()：normalize 一并应用（2026-10-28 审查接线）
		cfg, cfgErr := config.New("", config.WithNormalize(config.NormalizeConfig))
		if cfgErr != nil {
			logger.Warn(logComponent).Str("session_id", sessionID).Err(cfgErr).
				Msg("loadTeamSpec: config init failed, using default spec")
			s := atschema.NewTeamAgentSpec()
			return &s, nil
		}
		cfgData, loadErr := cfg.Load()
		if loadErr != nil || cfgData == nil {
			logger.Warn(logComponent).Str("session_id", sessionID).Err(loadErr).
				Msg("loadTeamSpec: config load failed, using default spec")
			s := atschema.NewTeamAgentSpec()
			return &s, nil
		}
		cb = cfgData
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
	// M-04: 端口非法时 error 上抛（对齐 Python parse_port raise 无 except 传播）
	if IsDistributedMode(cb) {
		normalized, err := NormalizeDistributedTransportFields(cb, specDict)
		if err != nil {
			logger.Error(logComponent).Str("session_id", sessionID).Err(err).
				Msg("loadTeamSpec: distributed transport fields invalid")
			return nil, err
		}
		specDict = normalized
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

	return spec, nil
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
