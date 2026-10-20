package team

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMaxIterations 默认最大迭代次数
	// 对齐 Python: _DEFAULT_MAX_ITERATIONS
	defaultMaxIterations = 200
	// defaultCompletionTimeout 默认完成超时时间（秒）
	// 对齐 Python: _DEFAULT_COMPLETION_TIMEOUT
	defaultCompletionTimeout = 600.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultAgentWorkspace agent 级默认 workspace。
	// 对齐 Python: _DEFAULT_AGENT_WORKSPACE = {"stable_base": True}
	defaultAgentWorkspace = map[string]any{"stable_base": true}
	// defaultTeamWorkspace 团队级默认 workspace。
	// 对齐 Python: _DEFAULT_TEAM_WORKSPACE = {"enabled": True}
	defaultTeamWorkspace = map[string]any{"enabled": true}
	// defaultTransport 默认传输层规格。
	// 对齐 Python: _DEFAULT_TRANSPORT = {"type": "inprocess"}
	defaultTransport = map[string]any{"type": "inprocess"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadTeamSpecDict 从 configBase 构建完整的 team 配置字典。
// 对齐 Python: load_team_spec_dict(config_base) (L300-346)
//
// Python 步骤：
//  1. team_raw = _select_first_modes_team(config_base)；为空时 warning
//  2. agents = _build_agents_config(team_raw, config_base)
//  3. spec_dict = deepcopy(team_raw)，pop("enable_team_plan")
//  4. 顶层字段：team_name/lifecycle/teammate_mode/spawn_mode/enable_hitt/leader/
//     agents/language（preferred_language 默认 "zh"）
//  5. workspace_spec 非空时设置；transport 设置
//  6. predefined_members 非空时设置、空时 pop
//  7. storage_raw = team_raw.get("storage")；非空时经 _resolve_storage_config 设置
//  8. 完成日志（team_name/lifecycle/agents keys/predefined_members member_name）
func LoadTeamSpecDict(configBase map[string]any) map[string]any {
	if configBase == nil {
		configBase = make(map[string]any)
	}

	teamRaw := selectFirstModesTeam(configBase)
	if len(teamRaw) == 0 {
		logger.Warn(logComponent).Msg("no modes.team config found, using defaults")
		teamRaw = make(map[string]any)
	}

	agents := buildAgentsConfig(teamRaw, configBase)

	// 汇总 team 配置：深拷贝 team_raw 后逐项覆写（对齐 Python deepcopy + 顶层赋值）
	teamCfg := utils.DeepCopyMap(teamRaw)
	if teamCfg == nil {
		teamCfg = make(map[string]any)
	}
	// 对齐 Python: spec_dict.pop("enable_team_plan", None)
	delete(teamCfg, "enable_team_plan")

	// 顶层字段默认值（对齐 Python L314-318）
	teamName := strings.TrimSpace(strField(teamRaw, "team_name"))
	if teamName == "" {
		teamName = "team"
	}
	teamCfg["team_name"] = teamName
	teamCfg["lifecycle"] = strFieldOr(teamRaw, "lifecycle", "persistent")
	teamCfg["teammate_mode"] = strFieldOr(teamRaw, "teammate_mode", "build_mode")
	teamCfg["spawn_mode"] = strFieldOr(teamRaw, "spawn_mode", "inprocess")
	teamCfg["enable_hitt"] = boolFieldOr(teamRaw, "enable_hitt", true)
	teamCfg["leader"] = buildLeaderSpec(teamRaw)
	teamCfg["agents"] = agents
	// 对齐 Python L321: str(config_base.get("preferred_language", "zh")).strip().lower()
	language := strings.TrimSpace(strField(configBase, "preferred_language"))
	if language == "" {
		language = "zh"
	}
	teamCfg["language"] = strings.ToLower(language)

	// workspace：非 dict 时返回默认 {"enabled": true, "version_control": false}，
	// 恒非 nil（Python 返回 Optional 但两分支都返回 dict，等价恒设置）
	teamCfg["workspace"] = buildWorkspaceSpec(teamRaw)
	teamCfg["transport"] = buildTransportSpec(teamRaw)

	// predefined_members：非空时设置，空时移除（对齐 Python L329-333）
	predefinedMembers := buildPredefinedMembers(teamRaw)
	if len(predefinedMembers) > 0 {
		teamCfg["predefined_members"] = predefinedMembers
	} else {
		delete(teamCfg, "predefined_members")
	}

	// storage：读 "storage" 键（对齐 Python L335-337），仅非空时设置
	if storageRaw, ok := teamRaw["storage"].(map[string]any); ok && len(storageRaw) > 0 {
		teamCfg["storage"] = resolveStorageConfig(storageRaw)
	} else {
		// Python 不设置该键（保持 nil 语义），Go 侧同步不设置
		delete(teamCfg, "storage")
	}

	// 完成日志（对齐 Python L339-345，T-02 字段补齐）
	memberNames := make([]string, 0, len(predefinedMembers))
	for _, member := range predefinedMembers {
		memberNames = append(memberNames, strField(member, "member_name"))
	}
	agentKeys := make([]string, 0, len(agents))
	for k := range agents {
		agentKeys = append(agentKeys, k)
	}
	sortStrings(agentKeys)
	logger.Info(logComponent).
		Str("team_name", teamName).
		Str("lifecycle", strField(teamCfg, "lifecycle")).
		Strs("agents", agentKeys).
		Strs("predefined_members", memberNames).
		Msg("team config loaded")

	return teamCfg
}

// ResolveTeamSqliteDbPath 解析团队 SQLite 数据库路径。
// 对齐 Python: resolve_team_sqlite_db_path(config_base) (L58-87)
//
// Python 步骤：
//  1. team_raw = _resolve_team_raw_for_storage(config_base)
//  2. storage_raw = team_raw.get("storage", {})；非 dict 返回 None
//  3. storage_type = storage_raw.get("type")；非空且非 sqlite 返回 None
//  4. conn_str = storage_raw.params.connection_string；为空返回 AgentTeamsHome/team.db
//  5. expanduser；绝对路径直接返回；相对路径以 AgentTeamsHome 为基准
func ResolveTeamSqliteDbPath(configBase map[string]any) string {
	if configBase == nil {
		return ""
	}
	teamRaw := resolveTeamRawForStorage(configBase)

	storageRaw, ok := teamRaw["storage"].(map[string]any)
	if !ok {
		return ""
	}

	// 对齐 Python: storage_type and storage_type != "sqlite" → None
	storageType := strings.ToLower(strings.TrimSpace(strField(storageRaw, "type")))
	if storageType != "" && storageType != "sqlite" {
		return ""
	}

	storageParams, _ := storageRaw["params"].(map[string]any)
	if storageParams == nil {
		storageParams = map[string]any{}
	}

	connStr := strings.TrimSpace(strField(storageParams, "connection_string"))
	if connStr == "" {
		// 对齐 Python: get_agent_teams_home() / "team.db"
		return filepath.Join(workspace.AgentTeamsHomeDir(), "team.db")
	}

	// 对齐 Python: Path(conn_str).expanduser()（~ 展开）
	dbPath := expandHomePath(connStr)
	if filepath.IsAbs(dbPath) {
		return dbPath
	}
	// 对齐 Python: 相对路径以 AgentTeamsHome 为基准
	return filepath.Join(workspace.AgentTeamsHomeDir(), dbPath)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// selectFirstModesTeam 从 configBase["modes"]["team"] 中选择第一个 team 配置。
// 对齐 Python: _select_first_modes_team(config_base) (L26-40)
//
// modes.team 为 dict（team_name → team_spec），按 key 遍历取第一个 dict 值。
// S-01: 旧实现误断言为 []any list，生产 dict 格式恒失败导致全链路退化为默认值。
func selectFirstModesTeam(configBase map[string]any) map[string]any {
	modes, ok := configBase["modes"].(map[string]any)
	if !ok {
		logger.Debug(logComponent).Msg("no modes config in config_base, using empty team config")
		return make(map[string]any)
	}
	// 对齐 Python: teams_raw = modes_raw.get("team", {})；非 dict 返回 {}
	teams, ok := modes["team"].(map[string]any)
	if !ok || len(teams) == 0 {
		logger.Debug(logComponent).Msg("modes.team is empty or non-dict, using empty team config")
		return make(map[string]any)
	}
	for _, teamRaw := range teams {
		if first, ok := teamRaw.(map[string]any); ok {
			logger.Debug(logComponent).
				Str("team_name", strField(first, "team_name")).
				Msg("selected team from modes.team")
			return first
		}
	}
	return make(map[string]any)
}

// resolveTeamRawForStorage 解析用于存储路径的 team 原始配置。
// 对齐 Python: _resolve_team_raw_for_storage(config_base) (L43-55)
func resolveTeamRawForStorage(configBase map[string]any) map[string]any {
	selected := selectFirstModesTeam(configBase)
	if len(selected) > 0 {
		return selected
	}

	// 对齐 Python L48-50: legacy_team = config_base.get("team", {})
	if legacyTeam, ok := configBase["team"].(map[string]any); ok && len(legacyTeam) > 0 {
		return legacyTeam
	}

	// 对齐 Python L52-53: 任一身份键存在则视 config_base 自身为 team_raw
	for _, key := range []string{"team_name", "leader", "agents", "storage", "predefined_members"} {
		if _, exists := configBase[key]; exists {
			return configBase
		}
	}

	return make(map[string]any)
}

// resolveDefaultModelConfig 解析默认模型配置。
// 对齐 Python: _resolve_default_model_config(config_base) (L90-105)
//
// models.defaults 为 list 时取第一个 dict 条目；否则回退 models.default（旧格式单对象）。
// S-03: 旧实现误断言 defaults 为 map，生产 list 格式恒失败且无旧格式回退。
func resolveDefaultModelConfig(configBase map[string]any) map[string]any {
	models, ok := configBase["models"].(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	// 对齐 Python L96-99: defaults 为 list 时取第一个 dict 条目
	if defaults, ok := models["defaults"].([]any); ok {
		for _, item := range defaults {
			if m, ok := item.(map[string]any); ok {
				return m
			}
		}
	}
	// 对齐 Python L101-103: 旧格式回退 default 单对象
	if def, ok := models["default"].(map[string]any); ok {
		return def
	}
	return make(map[string]any)
}

// buildDefaultModelDict 构建默认模型字典，包含 model_client_config 和 model_request_config。
// 对齐 Python: _build_default_model_dict(config_base) (L108-125)
func buildDefaultModelDict(configBase map[string]any) map[string]any {
	defaults := resolveDefaultModelConfig(configBase)

	// 对齐 Python L110-111: model_client_config / model_config_obj（S-03 键名修正）
	mcc, _ := defaults["model_client_config"].(map[string]any)
	if mcc == nil {
		mcc = map[string]any{}
	}
	mco, _ := defaults["model_config_obj"].(map[string]any)
	if mco == nil {
		mco = map[string]any{}
	}
	mccCopy := utils.DeepCopyMap(mcc)
	if mccCopy == nil {
		mccCopy = map[string]any{}
	}
	mcoCopy := utils.DeepCopyMap(mco)
	if mcoCopy == nil {
		mcoCopy = map[string]any{}
	}

	// 对齐 Python L113-115: model_name 注入 model_request_config["model"]
	modelName := strField(mcc, "model_name")
	if modelName != "" {
		if _, exists := mcoCopy["model"]; !exists {
			mcoCopy["model"] = modelName
		}
	}

	// 对齐 Python L117-121: 完成日志（model_name + provider）
	provider := strField(mcc, "client_provider")
	if provider == "" {
		provider = "unknown"
	}
	logger.Info(logComponent).
		Str("model_name", modelName).
		Str("provider", provider).
		Msg("model config loaded")

	return map[string]any{
		"model_client_config": mccCopy,
		"model_request_config": mcoCopy,
	}
}

// resolveStorageConfig 解析 storage 配置：params.connection_string 存在时
// 展开路径并确保父目录存在。
// 对齐 Python: _resolve_storage_config(storage_raw) (L128-145)
//
// S-02d: 旧实现读顶层 db_type/connection_string 并把解析结果写回顶层；
// Python 读 type/params.connection_string 且仅当该键存在时解析，结果写回 params。
func resolveStorageConfig(storageRaw map[string]any) map[string]any {
	result := utils.DeepCopyMap(storageRaw)
	if result == nil {
		result = make(map[string]any)
	}

	// 对齐 Python L130-132: params 中无 connection_string 键时原样返回
	storageParams, _ := result["params"].(map[string]any)
	if storageParams == nil {
		return result
	}
	if _, exists := storageParams["connection_string"]; !exists {
		return result
	}

	// 对齐 Python L134-137: resolve_team_sqlite_db_path({"storage": storage_dict})
	dbPath := resolveTeamSqliteDbPathForStorage(result)
	if dbPath == "" {
		return result
	}
	storageParams["connection_string"] = dbPath

	// 对齐 Python L139-143: 父目录不存在时创建
	dbDir := filepath.Dir(dbPath)
	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dbDir, 0o755); err != nil {
			logger.Warn(logComponent).Str("db_dir", dbDir).Err(err).
				Msg("failed to create database directory")
		} else {
			logger.Info(logComponent).Str("db_dir", dbDir).
				Msg("created database directory")
		}
	}

	return result
}

// resolveTeamSqliteDbPathForStorage 从 storage 配置解析 SQLite 路径（内部重载）。
// 对齐 Python: resolve_team_sqlite_db_path({"storage": storage_dict}) 调用形式——
// 将 storage dict 包装为临时 configBase 复用 ResolveTeamSqliteDbPath 逻辑。
func resolveTeamSqliteDbPathForStorage(storageDict map[string]any) string {
	return ResolveTeamSqliteDbPath(map[string]any{"storage": storageDict})
}

// buildAgentDefaults 返回默认 workspace、max_iterations 和 completion_timeout。
// 对齐 Python: _build_agent_defaults() (L148-153)
//
// S-02 新发现：默认 workspace 应为 {"stable_base": true}（Python _DEFAULT_AGENT_WORKSPACE），
// 旧实现误写为 {"root_path": "", "language": ""}。
func buildAgentDefaults() (map[string]any, int, float64) {
	return utils.DeepCopyMap(defaultAgentWorkspace), defaultMaxIterations, defaultCompletionTimeout
}

// buildAgentSpecDict 合并 agent_config 和默认值，构建单个 agent 规格。
// 对齐 Python: _build_agent_spec_dict(...) (L156-169)
//
// Python 用 setdefault 语义（已显式配置的键不覆盖）；Go 用"先填默认再覆盖"等价实现。
func buildAgentSpecDict(
	agentConfig map[string]any,
	defaultModel map[string]any,
	defaultWorkspace map[string]any,
	maxIterations int,
	completionTimeout float64,
) map[string]any {
	result := make(map[string]any)

	// 先填入默认值（等价 setdefault）
	result["workspace"] = utils.DeepCopyMap(defaultWorkspace)
	result["max_iterations"] = maxIterations
	result["completion_timeout"] = completionTimeout
	result["model"] = utils.DeepCopyMap(defaultModel)

	// 用 agent_config 覆盖（显式配置优先）
	for k, v := range agentConfig {
		result[k] = v
	}

	return result
}

// buildAgentsConfig 构建 agents 配置字典，解析 "$name" 字符串引用，确保 leader/teammate 键存在。
// 对齐 Python: _build_agents_config(team_raw, config_base) (L172-225)
//
// Python 步骤：
//  1. agents_raw = team_raw["agents"]；为空时注入 {"leader": {}, "teammate": {}}
//  2. 顶层 top_agents = config_base["agents"]（查找路径为顶层，非 models.agents）
//  3. 值为 "$name" 字符串时从 top_agents 解引用；失败 warning + 空配置
//  4. 每个 agent 经 _build_agent_spec_dict 合并默认值
//  5. 确保 "leader" 键存在
//
// M-01: 旧实现只识别 {"$ref": name} dict 形式（Python 不存在此形式）且查
// models.agents（Python 查顶层 agents）。现已严格对齐：仅字符串引用 + 顶层查找。
func buildAgentsConfig(teamRaw map[string]any, configBase map[string]any) map[string]any {
	defaultModel := buildDefaultModelDict(configBase)
	defaultWorkspace, maxIter, timeout := buildAgentDefaults()

	// 获取 agents 配置源：优先 team_raw["agents"]，其次 team_raw["members"]
	var agentsSrc map[string]any
	if a, ok := teamRaw["agents"].(map[string]any); ok && len(a) > 0 {
		agentsSrc = a
	} else if m, ok := teamRaw["members"].(map[string]any); ok && len(m) > 0 {
		agentsSrc = m
	}

	// 对齐 Python L177-179: 空 agents 配置时注入 leader/teammate 两键
	if len(agentsSrc) == 0 {
		logger.Warn(logComponent).Msg("agents config is empty, using default leader/teammate")
		agentsSrc = map[string]any{"leader": map[string]any{}, "teammate": map[string]any{}}
	}

	// 对齐 Python L181-183: top_agents = config_base["agents"]（顶层）
	topAgents, _ := configBase["agents"].(map[string]any)
	if topAgents == nil {
		topAgents = map[string]any{}
	}

	agents := make(map[string]any)
	for name, spec := range agentsSrc {
		var agentConfig map[string]any
		// 对齐 Python L187-201: "$name" 字符串引用形式
		if refStr, ok := spec.(string); ok && strings.HasPrefix(refStr, "$") {
			refName := refStr[1:]
			if refConfig, exists := topAgents[refName]; exists {
				agentConfig = utils.DeepCopyMap(toMapAny(refConfig))
				logger.Debug(logComponent).
					Str("agent_name", name).
					Str("ref", refName).
					Msg("resolved agent reference to top-level agents")
			} else {
				logger.Warn(logComponent).
					Str("agent_name", name).
					Str("ref", refName).
					Msg("agent reference not found in top-level agents, using defaults")
				agentConfig = make(map[string]any)
			}
		} else {
			// 对齐 Python L203: dict 直接配置，非 dict 空 map
			agentConfig = toMapAny(spec)
			if agentConfig == nil {
				agentConfig = make(map[string]any)
			} else {
				agentConfig = utils.DeepCopyMap(agentConfig)
			}
		}

		agents[name] = buildAgentSpecDict(agentConfig, defaultModel, defaultWorkspace, maxIter, timeout)
	}

	// 对齐 Python L216-223: 确保 "leader" 键存在
	if _, hasLeader := agents["leader"]; !hasLeader {
		agents["leader"] = buildAgentSpecDict(make(map[string]any), defaultModel, defaultWorkspace, maxIter, timeout)
	}

	return agents
}

// buildWorkspaceSpec 构建工作空间规格。
// 对齐 Python: _build_workspace_spec(team_raw) (L228-238)
//
// S-02a: 非 dict 时返回默认 {"enabled": true, "version_control": false}；
// dict 时深拷贝 + setdefault enabled=true/version_control=false。
func buildWorkspaceSpec(teamRaw map[string]any) map[string]any {
	ws, ok := teamRaw["workspace"].(map[string]any)
	if !ok {
		result := utils.DeepCopyMap(defaultTeamWorkspace)
		result["version_control"] = false
		return result
	}
	result := utils.DeepCopyMap(ws)
	if _, exists := result["enabled"]; !exists {
		result["enabled"] = true
	}
	if _, exists := result["version_control"]; !exists {
		result["version_control"] = false
	}
	return result
}

// buildTransportSpec 构建传输层规格。
// 对齐 Python: _build_transport_spec(team_raw) (L241-248)
//
// S-02b: 非 dict 时返回默认 {"type": "inprocess"}；dict 时深拷贝 + setdefault type=inprocess。
func buildTransportSpec(teamRaw map[string]any) map[string]any {
	tp, ok := teamRaw["transport"].(map[string]any)
	if !ok {
		return utils.DeepCopyMap(defaultTransport)
	}
	result := utils.DeepCopyMap(tp)
	if _, exists := result["type"]; !exists {
		result["type"] = "inprocess"
	}
	return result
}

// buildLeaderSpec 构建 Leader 规格。
// 对齐 Python: _build_leader_spec(team_raw) (L251-263)
//
// S-02c: 非 dict 时返回完整默认四字段；dict 时字段级默认值回退
// （member_name="team_leader" / display_name="Team Leader" / name 三级回退 / persona 默认）。
func buildLeaderSpec(teamRaw map[string]any) map[string]any {
	leader, _ := teamRaw["leader"].(map[string]any)
	if leader == nil {
		leader = map[string]any{}
	}

	// 对齐 Python L253-257: name 三级回退（name → display_name → "TeamLeader"）
	leaderName := strings.TrimSpace(strField(leader, "name"))
	if leaderName == "" {
		leaderName = strings.TrimSpace(strField(leader, "display_name"))
	}
	if leaderName == "" {
		leaderName = "TeamLeader"
	}

	// 对齐 Python L258-263: 字段级默认值
	memberName := strField(leader, "member_name")
	if memberName == "" {
		memberName = "team_leader"
	}
	displayName := strField(leader, "display_name")
	if displayName == "" {
		displayName = "Team Leader"
	}
	persona := strField(leader, "persona")
	if persona == "" {
		persona = "天才项目管理专家"
	}

	return map[string]any{
		"member_name": memberName,
		"display_name": displayName,
		"name":         leaderName,
		"persona":      persona,
	}
}

// buildPredefinedMembers 构建预定义成员列表。
// 对齐 Python: _build_predefined_members(team_raw) (L266-297)
//
// S-02 新发现：补齐 Python 完整过滤链——
//  1. 非 dict 条目跳过；member_name 为空跳过（warning）
//  2. name/display_name 均无有效值跳过（warning）
//  3. member_name 规范化、display_name = identity_name、persona 兜底 ""
func buildPredefinedMembers(teamRaw map[string]any) []map[string]any {
	raw, ok := teamRaw["predefined_members"].([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		// 对齐 Python L277-280: member_name 为空跳过
		memberName := strings.TrimSpace(strField(m, "member_name"))
		if memberName == "" {
			logger.Warn(logComponent).
				Int("index", i).
				Msg("skipped predefined member without member_name")
			continue
		}

		// 对齐 Python L282-288: identity_name = name or display_name，均无有效值跳过
		identityName := strField(m, "name")
		if strings.TrimSpace(identityName) == "" {
			identityName = strField(m, "display_name")
		}
		if strings.TrimSpace(identityName) == "" {
			logger.Warn(logComponent).
				Int("index", i).
				Msg("skipped predefined member without name/display_name")
			continue
		}

		// 对齐 Python L290-293: 深拷贝 + 字段规范化
		memberSpec := utils.DeepCopyMap(m)
		memberSpec["member_name"] = memberName
		memberSpec["display_name"] = strings.TrimSpace(identityName)
		if persona, exists := memberSpec["persona"]; !exists || persona == nil {
			memberSpec["persona"] = ""
		}

		result = append(result, memberSpec)
	}
	return result
}

// toMapAny 将 any 安全转换为 map[string]any（非 map 返回 nil）。
func toMapAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// strFieldOr 从 map 中读取字符串字段，为空时返回默认值。
func strFieldOr(m map[string]any, key string, def string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return def
}

// boolFieldOr 从 map 中读取布尔字段，键不存在时返回默认值。
func boolFieldOr(m map[string]any, key string, def bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return def
}

// expandHomePath 展开 ~ 前缀路径。
// 对齐 Python: Path(conn_str).expanduser()
func expandHomePath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// sortStrings 原地字符串切片排序（日志字段确定性输出）。
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// strField 从 map 中安全获取字符串字段。
func strField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// normalizeTeamIdentityFields 标准化团队身份字段。
// 对齐 Python: TeamManager._normalize_team_identity_fields(team_cfg) (team_manager.py L245)
//
// 规则：
//   - 深拷贝输入
//   - 对 leader 和每个 predefined_member：
//     如果 display_name 有值但 name 为空，name = display_name
//     如果 name 有值但 display_name 为空，display_name = name
func normalizeTeamIdentityFields(teamCfg map[string]any) map[string]any {
	result := utils.DeepCopyMap(teamCfg)

	// 标准化 leader
	if leader, ok := result["leader"].(map[string]any); ok {
		normalizeNameFields(leader, "leader")
	}

	// 标准化 predefined_members
	if members, ok := result["predefined_members"].([]any); ok {
		for i, item := range members {
			if m, ok := item.(map[string]any); ok {
				normalizeNameFields(m, fmt.Sprintf("predefined_members[%d]", i))
			}
		}
	}

	return result
}

// normalizeNameFields 标准化 name/display_name 互填。
// 如果 display_name 有值但 name 为空，设 name = display_name；
// 如果 name 有值但 display_name 为空，设 display_name = name。
func normalizeNameFields(m map[string]any, context string) {
	name, _ := m["name"].(string)
	displayName, _ := m["display_name"].(string)

	if displayName != "" && name == "" {
		m["name"] = displayName
		logger.Debug(logComponent).
			Str("context", context).
			Str("name", displayName).
			Msg("display_name set but name empty, setting name = display_name")
	} else if name != "" && displayName == "" {
		m["display_name"] = name
		logger.Debug(logComponent).
			Str("context", context).
			Str("display_name", name).
			Msg("name set but display_name empty, setting display_name = name")
	}
}
