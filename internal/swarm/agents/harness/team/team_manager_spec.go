package team

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	agentteamsmodels "github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentcron "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/cron"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	cronbridge "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/cron"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentCustomizer Agent 定制器函数类型。
// S-01: 全项目唯一类型定义在 agent_teams/schema 包（依赖最底层），
// 此处保留类型别名供 team 包内使用，签名一致：ctx + error + memberName string。
type AgentCustomizer = atschema.AgentCustomizer

// CodeMemberConfigurer code 模式 team member 配置回调（由 adapter 层注入）。
// 对齐 Python: configure_code_team_member_agent(agent, *, parent_agent, skill_manager,
// member_name, role, session_id, channel_id, project_dir, runtime_language,
// force_english_runtime_prompt) (interface_code.py L1301)
//
// skillManager 为成员独立 SkillManager（步骤 b 构造），可能为 nil。
// runtimeLanguage 为空串表示 None（Python 语义：非 team-plan leader 不传语言）。
type CodeMemberConfigurer func(
	ctx context.Context,
	agent interfaces.DeepAgentInterface,
	parentAgent interfaces.DeepAgentInterface,
	skillManager *skillruntime.SkillManager,
	memberName, role, sessionID, channelID, projectDir, runtimeLanguage string,
	forceEnglishRuntimePrompt bool,
)

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// GetEnrichedTeamSpec 获取增强的 TeamAgentSpec。
// 对齐 Python: TeamManager.get_enriched_team_spec(session_id, deep_agent, ...) (team_manager.py L354-381)
//
// Python 步骤：
//
//	Python: 1. config_base = get_config()
//	Python: 2. await self._ensure_postgresql_for_leader(config_base)
//	Python: 3. spec = self._load_team_spec(session_id)
//	Python: 4. self._apply_session_scoped_team_name(spec, session_id=session_id)
//	Python: 5. self.apply_team_plan_mode(spec, request_metadata=)
//	Python: 6. spec.agent_customizer = self.build_agent_customizer(...)
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
	// 步骤 1-2: 加载 spec + PG 编排
	// Go 差异：configBase 由 loadTeamSpec 内部获取或调用方注入
	// 步骤 2: _ensure_postgresql_for_leader
	// 对齐 Python: config_base = get_config(); await self._ensure_postgresql_for_leader(config_base)
	if cfgMap, err := teamLoadConfig(); err == nil {
		EnsurePostgresqlForLeader(ctx, cfgMap)
	} else {
		logger.Warn(logComponent).Err(err).
			Msg("GetEnrichedTeamSpec: load config for PG check failed")
	}
	spec, specErr := loadTeamSpec(sessionID)
	if specErr != nil {
		// M-04: 对齐 Python raise 传播——配置错误（如非法端口）显式暴露给调用方
		logger.Error(logComponent).Str("session_id", sessionID).Err(specErr).
			Msg("GetEnrichedTeamSpec: loadTeamSpec failed")
		return nil
	}
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

// BuildAgentCustomizer 构建 agent customizer 闭包。
// 对齐 Python: TeamManager.build_agent_customizer(spec, deep_agent, session_id, ...) (L557-967)
//
// 闭包六步（各步骤独立 panic 恢复 + warning，对齐 Python 各步骤独立 try/except）：
//
//	a. 继承能力卡
//	b. 技能同步（copy member skills + sync team skills + write skills_state）
//	c. code adapter 配置（parent adapter mode == "code" 时注入回调）
//	d. build_member_rails + 白名单 AddRail + 三类 rail 注册
//	e. 注册 TeamRailContext（leader）
//	f. 注册 member runtime tools（cron 已随 11.10 回填；SendFile ⤵️(10.6.24)）
//
// 差异说明：Python L948-959 的 rail_manager 扩展 rail 循环，Go 无 get_rail_manager
// 对应物（扩展 rail 加载机制属 10.5.7 ExtensionLoader 延后），空操作。
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

	// ── 前置解析（对齐 Python build_agent_customizer 闭包捕获变量 L567-579）──
	globalSkillsDir := workspace.AgentSkillsDir()
	globalSkillsStatePath := filepath.Join(globalSkillsDir, "skills_state.json")
	resolvedChannel := channelID
	if resolvedChannel == "" {
		resolvedChannel = "default"
	}
	resolvedModelName := GetDefaultModelName(nil)

	// team shared workspace（spec.Workspace.RootPath 回退 TeamHome/team-workspace）
	teamWsRoot := ""
	if spec.Workspace != nil && spec.Workspace.RootPath != "" {
		teamWsRoot = spec.Workspace.RootPath
	} else {
		teamWsRoot = filepath.Join(agentteams.TeamHome(spec.TeamName), "team-workspace")
	}
	teamWsSkillsDir := filepath.Join(teamWsRoot, "skills")
	teamTrajectoryRegistry := trajectory.NewInMemoryTrajectoryRegistry()

	capturedRequestID := requestID
	capturedRequestMetadata := requestMetadata
	mgr := m

	// ── customizer 闭包（对齐 Python L727-965）──
	return func(ctx context.Context, agent interfaces.DeepAgentInterface, memberName string, role string) error {
		logger.Info(logComponent).
			Str("channel", resolvedChannel).
			Str("member_name", memberName).
			Str("role", role).
			Msg("[TeamManager] customizer called")

		// member workspace 解析（对齐 L738-750）
		memberDeepConfig := agent.DeepConfig()
		var agentWsRootPath string
		if memberDeepConfig != nil && memberDeepConfig.Workspace != nil {
			agentWsRootPath = memberDeepConfig.Workspace.RootPath
			logger.Debug(logComponent).Str("workspace_root_path", agentWsRootPath).
				Msg("[TeamManager] member workspace resolved")
		} else {
			logger.Warn(logComponent).
				Msg("[TeamManager] agent deep_config.workspace is None")
		}

		// parent 项目目录解析（对齐 L751-768）
		parentAdapterMode := strings.ToLower(deepAgent.UapswarmAdapterMode())
		parentProjectDir := firstNonEmpty(
			deepAgent.UapswarmProjectDir(),
			deepAgent.UapswarmCodeProjectDir(),
			parentWorkspaceRootPath(deepAgent),
		)
		if parentProjectDir != "" && memberDeepConfig != nil {
			agent.SetUapswarmProjectDir(parentProjectDir)
		}

		// ── 步骤 a：能力卡继承（对齐 L772-786）──
		stepAInheritAbilities(agent, deepAgent)

		// ── 步骤 b：技能同步（对齐 L788-827）──
		memberSkillManager := stepBSyncSkills(spec, agent, memberName, role,
			agentWsRootPath, globalSkillsDir, globalSkillsStatePath, teamWsSkillsDir)

		// ── 步骤 c：code adapter 配置（对齐 L829-868）──
		if parentAdapterMode == "code" {
			stepCConfigureCodeMember(mgr, ctx, agent, deepAgent, memberSkillManager,
				memberName, role, sessionID, resolvedChannel, parentProjectDir,
				spec, capturedRequestMetadata)
		}

		// ── 步骤 d：member rails 构造与注册（对齐 L870-947）──
		stepDMemberRails(mgr, agent, spec, memberName, role,
			resolvedChannel, resolvedModelName, teamWsRoot, teamWsSkillsDir,
			teamTrajectoryRegistry, globalSkillsDir, sessionID)

		// ── 步骤 e：TeamRailContext（leader）（对齐 L932-945）──
		stepERegisterRailContext(mgr, agent, spec, memberName, role,
			resolvedChannel, resolvedModelName, teamWsRoot, teamWsSkillsDir,
			teamTrajectoryRegistry, sessionID)

		// Python L948-959 rail_manager 扩展 rail 循环：Go 无对应机制（10.5.7 延后），空操作

		// ── 步骤 f：member runtime tools（cron 已随 11.10 回填；SendFile ⤵️(10.6.24)）──
		mgr.RegisterMemberRuntimeTools(agent, sessionID, &capturedRequestID, &resolvedChannel, capturedRequestMetadata)

		return nil
	}
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
	// ⤵️(10.6.24) SendFileToolkit 注册 — 待 SendFile 工具集实现回填
	// Python: send_file_allowed 三态（None 时 web 默认 True，其余 False）→ SendFileToolkit(...).get_tools() 注册
	logger.Info(logComponent).
		Str("channel_id", *channelID).
		Msg("[TeamManager] SendFileToolkit registration pending (10.6.24)")
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

// ──────────────────────────── 常量 ────────────────────────────
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

// stepAInheritAbilities 步骤 a：能力卡继承。
// 对齐 Python L772-786：FilterInheritableAbilityCards → existingAbilityIds 去重 → Add。
func stepAInheritAbilities(agent, deepAgent interfaces.DeepAgentInterface) {
	defer recoverStep("inherit abilities")
	inheritableCards := FilterInheritableAbilityCards(deepAgent)
	existingAbilityIDs := map[string]bool{}
	for _, ability := range agent.AbilityManager().List() {
		existingAbilityIDs[ability.AbilityID()] = true
	}
	addedCount := 0
	for _, card := range inheritableCards {
		if existingAbilityIDs[card.ID] {
			logger.Debug(logComponent).Str("ability", card.Name).
				Msg("[TeamManager] Ability already exists, skipped")
			continue
		}
		agent.AbilityManager().Add(card)
		existingAbilityIDs[card.ID] = true
		addedCount++
	}
	logger.Info(logComponent).
		Int("added", addedCount).
		Int("total", len(existingAbilityIDs)).
		Msg("[TeamManager] Added inheritable abilities")
}

// stepBSyncSkills 步骤 b：技能同步（五函数主流程）。
// 对齐 Python L788-827。返回成员 SkillManager（可能 nil）。
func stepBSyncSkills(spec *atschema.TeamAgentSpec, agent interfaces.DeepAgentInterface,
	memberName, role, agentWsRootPath, globalSkillsDir, globalSkillsStatePath, teamWsSkillsDir string) *skillruntime.SkillManager {

	var memberSkillManager *skillruntime.SkillManager
	if agentWsRootPath == "" {
		// 对齐 Python：member_workspace 为 None 时跳过步骤 b 剩余部分
		logger.Warn(logComponent).
			Str("member_name", memberName).
			Msg("[TeamManager] member workspace is None, skipping skill sync")
		return nil
	}
	defer recoverStep("skill sync")

	memberWorkspaceRoot := agentWsRootPath
	memberSkillsDir := filepath.Join(memberWorkspaceRoot, "skills")

	// 解析 skills_configured / selected_skills
	skillsConfigured, selectedSkills := resolveMemberSkills(spec, memberName, role)

	// 技能复制主流程（对齐 L799-808 try/except）
	func() {
		defer recoverStep("skill copy")
		if err := os.MkdirAll(memberSkillsDir, 0o755); err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[TeamManager] skill copy failed")
			return
		}
		if skillsConfigured && len(selectedSkills) > 0 {
			copyMemberConfiguredSkills(memberSkillsDir, selectedSkills, globalSkillsDir)
		}
		syncSkillsDir(teamWsSkillsDir, memberSkillsDir)
		writeMemberSkillState(memberSkillsDir, globalSkillsStatePath)
	}()

	// 成员 SkillManager（对齐 L810-813）
	func() {
		defer recoverStep("member SkillManager setup")
		memberSkillManager = skillruntime.NewSkillManager(memberWorkspaceRoot)
	}()

	// MemberSkillToolkitRail 挂载（对齐 L815-827）
	func() {
		defer recoverStep("MemberSkillToolkitRail setup")
		agent.AddRail(NewMemberSkillToolkitRail(memberWorkspaceRoot))
		logger.Info(logComponent).Str("workspace", memberWorkspaceRoot).
			Msg("[TeamManager] MemberSkillToolkitRail queued for member workspace")
	}()
	return memberSkillManager
}

// stepCConfigureCodeMember 步骤 c：code adapter 配置（注入回调）。
// 对齐 Python L829-868：延迟 import configure_code_team_member_agent + except 跳过。
func stepCConfigureCodeMember(mgr *TeamManager, ctx context.Context,
	agent, deepAgent interfaces.DeepAgentInterface, memberSkillManager *skillruntime.SkillManager,
	memberName, role, sessionID, resolvedChannel, parentProjectDir string,
	spec *atschema.TeamAgentSpec, requestMetadata map[string]any) {

	defer recoverStep("code team member adapter setup")
	configurer := mgr.codeMemberConfigurer
	if configurer == nil {
		// 对齐 Python: except Exception → warning 跳过
		logger.Warn(logComponent).
			Str("member_name", memberName).
			Str("role", role).
			Msg("[TeamManager] code member configurer not injected, skipping code team member adapter setup")
		return
	}

	// team_plan 判定（对齐 L835-848）
	requestMode := ""
	if requestMetadata != nil {
		if raw, ok := requestMetadata["mode"].(string); ok {
			requestMode = strings.TrimSpace(strings.ToLower(raw))
		}
	}
	roleName := strings.TrimSpace(strings.ToLower(role))
	teamPlanEnabled := requestMode == "team.plan" || spec.EnableTeamPlan
	isTeamPlanLeader := teamPlanEnabled && roleName == "leader"
	teamPlanRuntimeLanguage := ""
	if isTeamPlanLeader {
		// spec.Language 回退 config preferred_language 默认 "zh"
		lang := spec.Language
		if lang == "" {
			lang = configPreferredLanguage()
		}
		teamPlanRuntimeLanguage = lang
	}

	configurer(ctx, agent, deepAgent, memberSkillManager,
		memberName, role, sessionID, resolvedChannel, parentProjectDir,
		teamPlanRuntimeLanguage, !isTeamPlanLeader)
}

// stepDMemberRails 步骤 d：member rails 构造与注册。
// 对齐 Python L870-947：BuildMemberRails → 白名单过滤 AddRail → 三类注册。
func stepDMemberRails(mgr *TeamManager, agent interfaces.DeepAgentInterface,
	spec *atschema.TeamAgentSpec, memberName, role, resolvedChannel, resolvedModelName,
	teamWsRoot, teamWsSkillsDir string, teamTrajectoryRegistry trajectory.TrajectorySource,
	globalSkillsDir, sessionID string) {

	defer recoverStep("build_member_rails")

	teamID := spec.TeamName
	teamWorkspace := &TeamWorkspaceInfo{
		RootDir:            &teamWsRoot,
		SkillsDir:          &teamWsSkillsDir,
		TeamID:             &teamID,
		Config:             loadConfigForRails(),
		TrajectoryRegistry: teamTrajectoryRegistry,
	}

	memberRails := BuildMemberRails(
		&MemberInfo{
			AgentName: agentCardName(agent, "team_member"),
			ModelName: resolvedModelName,
			Role:      &role,
		},
		&RuntimeInfo{Channel: resolvedChannel},
		teamWorkspace,
	)

	var teamSkillRail *evolution.TeamSkillEvolutionRail
	var teamSkillCreateRail *evolution.TeamSkillCreateRail
	for _, rail := range memberRails {
		railTypeName := reflect.TypeOf(rail).Elem().Name()
		if railWhitelist[railTypeName] {
			agent.AddRail(rail)
			// TeamSkillEvolutionRail/TeamSkillCreateRail 需注册 live rail（热更新反注册用）
			switch r := rail.(type) {
			case *evolution.TeamSkillEvolutionRail:
				GetTeamManager(resolvedChannel).RegisterTeamLiveRail(sessionID, agent, r)
			case *evolution.TeamSkillCreateRail:
				GetTeamManager(resolvedChannel).RegisterTeamLiveRail(sessionID, agent, r)
			}
		} else {
			logger.Debug(logComponent).Str("rail", railTypeName).
				Msg("[TeamManager] Skipping non-whitelisted rail")
		}
		switch r := rail.(type) {
		case *evolution.TeamSkillEvolutionRail:
			teamSkillRail = r
		case *evolution.SkillEvolutionRail:
			GetTeamManager(resolvedChannel).RegisterTeamMemberSkillEvolutionRail(sessionID, r)
		case *evolution.TeamSkillCreateRail:
			teamSkillCreateRail = r
		}
	}
	logger.Info(logComponent).Int("count", len(memberRails)).
		Msg("[TeamManager] Added rails for team member")

	// TeamSkillEvolutionRail 注册（审批/同步用，对齐 L912-926）
	if teamSkillRail != nil {
		tm := GetTeamManager(resolvedChannel)
		tm.RegisterTeamSkillRail(sessionID, teamSkillRail)
		tm.RegisterTeamSkillSyncTarget(sessionID, teamWsSkillsDir, globalSkillsDir)
		logger.Info(logComponent).
			Str("skills_dir", teamWsSkillsDir).
			Str("sync_target", globalSkillsDir).
			Msg("[TeamManager] TeamSkillEvolutionRail mounted on leader")
	}
	if teamSkillCreateRail != nil {
		GetTeamManager(resolvedChannel).RegisterTeamSkillCreateRail(sessionID, teamSkillCreateRail)
	}
}

// stepERegisterRailContext 步骤 e：注册 TeamRailContext（仅 leader）。
// 对齐 Python L932-945。
func stepERegisterRailContext(mgr *TeamManager, agent interfaces.DeepAgentInterface,
	spec *atschema.TeamAgentSpec, memberName, role, resolvedChannel, resolvedModelName,
	teamWsRoot, teamWsSkillsDir string, teamTrajectoryRegistry trajectory.TrajectorySource,
	sessionID string) {

	if role != "leader" {
		return
	}
	defer recoverStep("register_team_rail_context")

	teamID := spec.TeamName
	teamWorkspace := &TeamWorkspaceInfo{
		RootDir:            &teamWsRoot,
		SkillsDir:          &teamWsSkillsDir,
		TeamID:             &teamID,
		Config:             loadConfigForRails(),
		TrajectoryRegistry: teamTrajectoryRegistry,
	}
	GetTeamManager(resolvedChannel).RegisterTeamRailContext(sessionID, &TeamRailMountContext{
		Agent: agent,
		MemberInfo: &MemberInfo{
			AgentName: agentCardName(agent, "team_member"),
			ModelName: resolvedModelName,
			Role:      &role,
		},
		Runtime:       &RuntimeInfo{Channel: resolvedChannel},
		TeamWorkspace: teamWorkspace,
	})
}

// resolveMemberSkills 解析成员配置的技能列表。
// 对齐 Python resolve_member_skills (L591-603)：
// member_name → role → leader 回退查找 spec.Agents；Skills 为空时返回 (false, [])。
func resolveMemberSkills(spec *atschema.TeamAgentSpec, memberName, role string) (bool, []string) {
	memberSpec := resolveMemberSpec(spec, memberName, role)
	if memberSpec == nil {
		return false, []string{}
	}
	if memberSpec.Skills == nil {
		return false, []string{}
	}
	selected := make([]string, 0, len(memberSpec.Skills))
	for _, s := range memberSpec.Skills {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			selected = append(selected, trimmed)
		}
	}
	return true, selected
}

// resolveMemberSpec 解析成员规格。
// 对齐 Python resolve_member_spec (L581-589)：
// member_name 命中 → role 命中 → leader 回退。
func resolveMemberSpec(spec *atschema.TeamAgentSpec, memberName, role string) *atschema.DeepAgentSpec {
	if spec.Agents == nil {
		return nil
	}
	if memberName != "" {
		if s, ok := spec.Agents[memberName]; ok {
			return &s
		}
	}
	if role != "" {
		if s, ok := spec.Agents[role]; ok {
			return &s
		}
	}
	if s, ok := spec.Agents["leader"]; ok {
		return &s
	}
	return nil
}

// copyMemberConfiguredSkills 复制成员配置的技能到成员技能目录。
// 对齐 Python copy_member_configured_skills (L605-638)：
// global 目录中 SKILL.md 有效且在 selected 集合 → 复制（dest 已存在跳过）→ missing warning。
func copyMemberConfiguredSkills(memberSkillsDir string, selectedSkills []string, globalSkillsDir string) {
	if info, err := os.Stat(globalSkillsDir); err != nil || !info.IsDir() {
		logger.Warn(logComponent).Str("global_skills_dir", globalSkillsDir).
			Msg("[TeamManager] global_skills_dir does not exist")
		return
	}
	selectedSet := map[string]bool{}
	for _, s := range selectedSkills {
		selectedSet[s] = true
	}
	if err := os.MkdirAll(memberSkillsDir, 0o755); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamManager] mkdir member_skills_dir failed")
		return
	}
	copiedCount := 0
	entries, err := os.ReadDir(globalSkillsDir)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamManager] readdir global_skills_dir failed")
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillMD := filepath.Join(globalSkillsDir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillMD); err != nil {
			continue
		}
		if !selectedSet[entry.Name()] {
			continue
		}
		dest := filepath.Join(memberSkillsDir, entry.Name())
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		if err := copyTreeMerge(filepath.Join(globalSkillsDir, entry.Name()), dest); err != nil {
			logger.Warn(logComponent).Err(err).Str("skill", entry.Name()).
				Msg("[TeamManager] copy skill to member workspace failed")
			continue
		}
		copiedCount++
		logger.Info(logComponent).Str("skill", entry.Name()).
			Msg("[TeamManager] Copied skill to member workspace")
	}

	// missing 集合 warning
	existingNames := map[string]bool{}
	if entries, err := os.ReadDir(memberSkillsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				existingNames[e.Name()] = true
			}
		}
	}
	missing := make([]string, 0)
	for name := range selectedSet {
		if !existingNames[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		logger.Warn(logComponent).Strs("missing", missing).
			Msg("[TeamManager] configured skills not found in global dir")
	}
	logger.Info(logComponent).Int("count", copiedCount).
		Msg("[TeamManager] Total configured skills copied to member")
}

// buildMemberSkillState 构建成员技能状态（对齐 Python build_member_skill_state L640-716）。
//
// 读全局 skills_state.json → NormalizeMarketplaces → installed_plugins/local_skills
// 按 member 目录实际 SKILL.md 过滤 + 补全（project source 条目）。
func buildMemberSkillState(memberSkillsDir, globalSkillsStatePath string) map[string]any {
	state := map[string]any{
		"marketplaces":      []any{},
		"installed_plugins": []any{},
		"local_skills":      []any{},
	}
	if data, err := os.ReadFile(globalSkillsStatePath); err == nil {
		var loaded map[string]any
		if jsonErr := json.Unmarshal(data, &loaded); jsonErr == nil {
			for k, v := range loaded {
				state[k] = v
			}
		}
	} else {
		logger.Warn(logComponent).Err(err).
			Msg("[TeamManager] failed to load global skills_state.json")
	}

	if raw, ok := state["marketplaces"].([]any); ok {
		normalized := skillruntime.NormalizeMarketplaces(raw)
		converted := make([]any, 0, len(normalized))
		for _, m := range normalized {
			converted = append(converted, m)
		}
		state["marketplaces"] = converted
	}

	// member 目录实际技能名（SKILL.md 有效）
	actualSkillNames := make([]string, 0)
	entries, err := os.ReadDir(memberSkillsDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(memberSkillsDir, e.Name(), "SKILL.md")); err == nil {
				actualSkillNames = append(actualSkillNames, e.Name())
			}
		}
	}
	sort.Strings(actualSkillNames)
	actualSkillSet := map[string]bool{}
	for _, n := range actualSkillNames {
		actualSkillSet[n] = true
	}

	// installed_plugins 过滤
	installedPlugins := make([]any, 0)
	if rawList, ok := state["installed_plugins"].([]any); ok {
		for _, item := range rawList {
			plugin, ok := item.(map[string]any)
			if !ok {
				continue
			}
			pluginName := strings.TrimSpace(mapStrField(plugin, "name"))
			if pluginName == "" || !actualSkillSet[pluginName] {
				continue
			}
			installedPlugins = append(installedPlugins, plugin)
		}
	}

	// local_skills 过滤
	localSkills := make([]any, 0)
	if rawList, ok := state["local_skills"].([]any); ok {
		for _, item := range rawList {
			localSkill, ok := item.(map[string]any)
			if !ok {
				continue
			}
			skillName := strings.TrimSpace(mapStrField(localSkill, "name"))
			if skillName == "" || !actualSkillSet[skillName] {
				continue
			}
			localSkills = append(localSkills, localSkill)
		}
	}

	// 补全缺失（project source 条目）
	existingPluginNames := map[string]bool{}
	for _, item := range installedPlugins {
		if plugin, ok := item.(map[string]any); ok {
			existingPluginNames[strings.TrimSpace(mapStrField(plugin, "name"))] = true
		}
	}
	existingLocalNames := map[string]bool{}
	for _, item := range localSkills {
		if localSkill, ok := item.(map[string]any); ok {
			existingLocalNames[strings.TrimSpace(mapStrField(localSkill, "name"))] = true
		}
	}
	for _, skillName := range actualSkillNames {
		if !existingPluginNames[skillName] {
			installedPlugins = append(installedPlugins, map[string]any{
				"name": skillName, "marketplace": "", "version": "",
				"commit": "", "source": "project", "installed_at": "",
			})
		}
		if !existingLocalNames[skillName] {
			localSkills = append(localSkills, map[string]any{
				"name":   skillName,
				"origin": filepath.Join(memberSkillsDir, skillName),
				"source": "project",
			})
		}
	}

	state["installed_plugins"] = installedPlugins
	state["local_skills"] = localSkills
	return state
}

// writeMemberSkillState 写成员 skills_state.json。
// 对齐 Python write_member_skill_state (L718-725)。
func writeMemberSkillState(memberSkillsDir, globalSkillsStatePath string) {
	stateFile := filepath.Join(memberSkillsDir, "skills_state.json")
	state := buildMemberSkillState(memberSkillsDir, globalSkillsStatePath)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamManager] marshal member skills_state failed")
		return
	}
	if err := os.WriteFile(stateFile, data, 0o644); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamManager] write member skills_state failed")
		return
	}
	logger.Info(logComponent).Str("state_file", stateFile).
		Msg("[TeamManager] Wrote member skills_state.json")
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

// parentWorkspaceRootPath 解析 parent agent 的 workspace 根路径（可能为空）。
// 对齐 Python L754-767 的 parent_workspace.root_path 回退。
func parentWorkspaceRootPath(deepAgent interfaces.DeepAgentInterface) string {
	cfg := deepAgent.DeepConfig()
	if cfg == nil || cfg.Workspace == nil {
		return ""
	}
	return cfg.Workspace.RootPath
}

// configPreferredLanguage 读取 config preferred_language（默认 "zh"）。
// 对齐 Python: get_config().get("preferred_language", "zh")
func configPreferredLanguage() string {
	cfgMap, err := teamLoadConfig()
	if err != nil {
		return "zh"
	}
	if lang, ok := cfgMap["preferred_language"].(string); ok && lang != "" {
		return lang
	}
	return "zh"
}

// loadConfigForRails 为 rails 构造加载完整 config（对齐 Python get_config()）。
func loadConfigForRails() map[string]any {
	cfgMap, err := teamLoadConfig()
	if err != nil {
		return map[string]any{}
	}
	return cfgMap
}

// agentCardName 读取 agent card name（回退默认值）。
// 对齐 Python: getattr(agent.card, "name", "team_member")
func agentCardName(agent interfaces.DeepAgentInterface, fallback string) string {
	card := agent.Card()
	if card == nil || card.Name == "" {
		return fallback
	}
	return card.Name
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
		cfg, cfgErr := config.New("")
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
