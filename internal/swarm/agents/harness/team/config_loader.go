package team

import (
	"fmt"
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
	defaultMaxIterations = 200
	// defaultCompletionTimeout 默认完成超时时间（秒）
	defaultCompletionTimeout = 600.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadTeamSpecDict 从 configBase 构建完整的 team 配置字典。
// 对齐 Python: load_team_spec_dict(config_base)
//
// Python 步骤：
//  1. team_raw = select_first_modes_team(config_base)
//  2. storage = resolve_storage_config(team_raw.get("storage_raw", {}))
//  3. default_model = build_default_model_dict(config_base)
//  4. default_workspace, max_iter, timeout = build_agent_defaults()
//  5. agents = build_agents_config(team_raw, config_base)
//  6. 汇总 team_cfg
//  7. normalize_team_identity_fields(team_cfg)
func LoadTeamSpecDict(configBase map[string]any) map[string]any {
	if configBase == nil {
		configBase = make(map[string]any)
	}

	teamRaw := selectFirstModesTeam(configBase)

	storage := resolveStorageConfig(resolveMapField(teamRaw, "storage_raw"))

	defaultModel := buildDefaultModelDict(configBase)

	defaultWorkspace, maxIter, timeout := buildAgentDefaults()

	agents := buildAgentsConfig(teamRaw, configBase)

	// 汇总 team 配置
	teamCfg := make(map[string]any)
	for k, v := range teamRaw {
		teamCfg[k] = v
	}
	teamCfg["storage"] = storage
	teamCfg["default_model"] = defaultModel
	teamCfg["default_workspace"] = defaultWorkspace
	teamCfg["max_iterations"] = maxIter
	teamCfg["completion_timeout"] = timeout
	teamCfg["agents"] = agents
	teamCfg["workspace"] = buildWorkspaceSpec(teamRaw)
	teamCfg["transport"] = buildTransportSpec(teamRaw)
	teamCfg["leader"] = buildLeaderSpec(teamRaw)
	teamCfg["predefined_members"] = buildPredefinedMembers(teamRaw)

	teamCfg = normalizeTeamIdentityFields(teamCfg)

	logger.Debug(logComponent).
		Str("team_name", strField(teamCfg, "team_name")).
		Msg("LoadTeamSpecDict 完成")

	return teamCfg
}

// ResolveTeamSqliteDbPath 解析团队 SQLite 数据库路径。
// 对齐 Python: resolve_team_sqlite_db_path(config_base)
//
// Python 步骤：
//  1. team_raw = resolve_team_raw_for_storage(config_base)
//  2. storage = resolve_storage_config(team_raw.get("storage_raw", {}))
//  3. 返回 storage 中的 connection_string
func ResolveTeamSqliteDbPath(configBase map[string]any) string {
	if configBase == nil {
		return ""
	}
	teamRaw := resolveTeamRawForStorage(configBase)
	storage := resolveStorageConfig(resolveMapField(teamRaw, "storage_raw"))
	if cs, ok := storage["connection_string"].(string); ok {
		return cs
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// selectFirstModesTeam 从 configBase["modes"]["team"] 中选择第一个 team 配置。
// 对齐 Python: _select_first_modes_team(config_base)
func selectFirstModesTeam(configBase map[string]any) map[string]any {
	modes, ok := configBase["modes"].(map[string]any)
	if !ok {
		logger.Debug(logComponent).Msg("config_base 中无 modes 配置，使用空 team 配置")
		return make(map[string]any)
	}
	teamList, ok := modes["team"].([]any)
	if !ok || len(teamList) == 0 {
		logger.Debug(logComponent).Msg("modes.team 为空或非列表，使用空 team 配置")
		return make(map[string]any)
	}
	first, ok := teamList[0].(map[string]any)
	if !ok {
		logger.Warn(logComponent).Msg("modes.team[0] 不是 map，使用空 team 配置")
		return make(map[string]any)
	}
	logger.Debug(logComponent).
		Str("team_name", strField(first, "team_name")).
		Msg("选中第一个 modes.team 配置")
	return first
}

// resolveTeamRawForStorage 解析用于存储路径的 team 原始配置。
// 对齐 Python: _resolve_team_raw_for_storage(config_base)
func resolveTeamRawForStorage(configBase map[string]any) map[string]any {
	teamRaw := selectFirstModesTeam(configBase)
	if teamRaw == nil {
		return make(map[string]any)
	}
	return teamRaw
}

// resolveDefaultModelConfig 解析默认模型配置。
// 对齐 Python: _resolve_default_model_config(config_base)
//
// 从 configBase["models"]["defaults"] 获取默认模型配置。
func resolveDefaultModelConfig(configBase map[string]any) map[string]any {
	models, ok := configBase["models"].(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	defaults, ok := models["defaults"].(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	return defaults
}

// buildDefaultModelDict 构建默认模型字典，包含 model_client_config 和 model_request_config。
// 对齐 Python: _build_default_model_dict(config_base)
func buildDefaultModelDict(configBase map[string]any) map[string]any {
	defaults := resolveDefaultModelConfig(configBase)
	result := make(map[string]any)

	// model_client_config
	if mcc, ok := defaults["model_client_config"].(map[string]any); ok {
		result["model_client_config"] = utils.DeepCopyMap(mcc)
	} else {
		result["model_client_config"] = make(map[string]any)
	}

	// model_request_config
	if mrc, ok := defaults["model_request_config"].(map[string]any); ok {
		result["model_request_config"] = utils.DeepCopyMap(mrc)
	} else {
		result["model_request_config"] = make(map[string]any)
	}

	logger.Debug(logComponent).Msg("构建默认模型配置完成")
	return result
}

// resolveStorageConfig 深拷贝 storage_raw 并解析 SQLite 连接字符串。
// 对齐 Python: _resolve_storage_config(storage_raw)
//
// Python 步骤：
//  1. 深拷贝 storage_raw
//  2. 如果 db_type 为 sqlite 且 connection_string 为空，自动填充
func resolveStorageConfig(storageRaw map[string]any) map[string]any {
	result := utils.DeepCopyMap(storageRaw)
	if result == nil {
		result = make(map[string]any)
	}

	// 解析 SQLite 连接字符串
	dbType := ""
	if dt, ok := result["db_type"].(string); ok {
		dbType = dt
	}
	connStr := ""
	if cs, ok := result["connection_string"].(string); ok {
		connStr = cs
	}

	if strings.EqualFold(dbType, "sqlite") && connStr == "" {
		// 使用 AgentTeamsHomeDir 作为默认目录
		defaultPath := filepath.Join(workspace.AgentTeamsHomeDir(), "team.db")
		result["connection_string"] = defaultPath
		logger.Debug(logComponent).
			Str("connection_string", defaultPath).
			Msg("SQLite 连接字符串为空，使用默认路径")
	}

	return result
}

// buildAgentDefaults 返回默认 workspace、max_iterations 和 completion_timeout。
// 对齐 Python: _build_agent_defaults()
func buildAgentDefaults() (map[string]any, int, float64) {
	defaultWorkspace := map[string]any{
		"root_path": "",
		"language":  "",
	}
	return defaultWorkspace, defaultMaxIterations, defaultCompletionTimeout
}

// buildAgentSpecDict 合并 agent_config 和默认值，构建单个 agent 规格。
// 对齐 Python: _build_agent_spec_dict(agent_config, default_model, default_workspace, max_iterations, completion_timeout)
func buildAgentSpecDict(
	agentConfig map[string]any,
	defaultModel map[string]any,
	defaultWorkspace map[string]any,
	maxIterations int,
	completionTimeout float64,
) map[string]any {
	result := make(map[string]any)

	// 先填入默认值
	result["workspace"] = utils.DeepCopyMap(defaultWorkspace)
	result["max_iterations"] = maxIterations
	result["completion_timeout"] = completionTimeout
	result["model"] = utils.DeepCopyMap(defaultModel)

	// 用 agent_config 覆盖
	for k, v := range agentConfig {
		result[k] = v
	}

	return result
}

// buildAgentsConfig 构建 agents 配置字典，解析 $ref 引用，确保 "leader" 键存在。
// 对齐 Python: _build_agents_config(team_raw, config_base)
//
// Python 步骤：
//  1. 遍历 team_raw["agents"] 或 team_raw["members"]
//  2. 如果值是 {"$ref": "..."}，从 config_base["models"]["agents"] 中查找
//  3. 用 default_model 和 defaults 合并每个 agent 配置
//  4. 确保 "leader" 键存在
func buildAgentsConfig(teamRaw map[string]any, configBase map[string]any) map[string]any {
	agents := make(map[string]any)

	// 获取 agents 配置源：优先 team_raw["agents"]，其次 team_raw["members"]
	var agentsSrc map[string]any
	if a, ok := teamRaw["agents"].(map[string]any); ok && len(a) > 0 {
		agentsSrc = a
	} else if m, ok := teamRaw["members"].(map[string]any); ok && len(m) > 0 {
		agentsSrc = m
	}

	// 获取 config_base 中的模型代理定义
	var configBaseAgents map[string]any
	if models, ok := configBase["models"].(map[string]any); ok {
		if ca, ok := models["agents"].(map[string]any); ok {
			configBaseAgents = ca
		}
	}

	defaultModel := buildDefaultModelDict(configBase)
	defaultWorkspace, maxIter, timeout := buildAgentDefaults()

	for name, spec := range agentsSrc {
		var agentConfig map[string]any
		// 检查 $ref 引用
		if specMap, ok := spec.(map[string]any); ok {
			if ref, ok := specMap["$ref"].(string); ok && ref != "" {
				// 从 config_base["models"]["agents"] 中查找引用
				if configBaseAgents != nil {
					if refConfig, ok := configBaseAgents[ref].(map[string]any); ok {
						agentConfig = utils.DeepCopyMap(refConfig)
						logger.Debug(logComponent).
							Str("agent_name", name).
							Str("ref", ref).
							Msg("解析 $ref 引用成功")
					} else {
						logger.Warn(logComponent).
							Str("agent_name", name).
							Str("ref", ref).
							Msg("$ref 引用在 config_base.models.agents 中未找到")
						agentConfig = make(map[string]any)
					}
				} else {
					logger.Warn(logComponent).
						Str("agent_name", name).
						Str("ref", ref).
						Msg("$ref 引用但 config_base.models.agents 不存在")
					agentConfig = make(map[string]any)
				}
			} else {
				// 直接配置
				agentConfig = utils.DeepCopyMap(specMap)
			}
		} else {
			agentConfig = make(map[string]any)
		}

		agents[name] = buildAgentSpecDict(agentConfig, defaultModel, defaultWorkspace, maxIter, timeout)
	}

	// 确保 "leader" 键存在
	if _, hasLeader := agents["leader"]; !hasLeader {
		agents["leader"] = buildAgentSpecDict(make(map[string]any), defaultModel, defaultWorkspace, maxIter, timeout)
		logger.Debug(logComponent).Msg("agents 中无 leader 配置，使用默认值")
	}

	return agents
}

// buildWorkspaceSpec 构建工作空间规格。
// 对齐 Python: _build_workspace_spec(team_raw)
func buildWorkspaceSpec(teamRaw map[string]any) map[string]any {
	ws, ok := teamRaw["workspace"].(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	return utils.DeepCopyMap(ws)
}

// buildTransportSpec 构建传输层规格。
// 对齐 Python: _build_transport_spec(team_raw)
func buildTransportSpec(teamRaw map[string]any) map[string]any {
	tp, ok := teamRaw["transport"].(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	return utils.DeepCopyMap(tp)
}

// buildLeaderSpec 构建 Leader 规格。
// 对齐 Python: _build_leader_spec(team_raw)
func buildLeaderSpec(teamRaw map[string]any) map[string]any {
	leader, ok := teamRaw["leader"].(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	return utils.DeepCopyMap(leader)
}

// buildPredefinedMembers 构建预定义成员列表。
// 对齐 Python: _build_predefined_members(team_raw)
func buildPredefinedMembers(teamRaw map[string]any) []map[string]any {
	raw, ok := teamRaw["predefined_members"].([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(raw))
	for i, item := range raw {
		if m, ok := item.(map[string]any); ok {
			result = append(result, utils.DeepCopyMap(m))
		} else {
			logger.Warn(logComponent).
				Int("index", i).
				Msg("predefined_members 项不是 map，跳过")
		}
	}
	return result
}

// normalizeTeamIdentityFields 标准化团队身份字段。
// 对齐 Python: normalize_team_identity_fields(team_cfg)
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
			Msg("display_name 有值但 name 为空，设 name = display_name")
	} else if name != "" && displayName == "" {
		m["display_name"] = name
		logger.Debug(logComponent).
			Str("context", context).
			Str("display_name", name).
			Msg("name 有值但 display_name 为空，设 display_name = name")
	}
}

// resolveMapField 从 map 中安全获取嵌套 map 字段。
func resolveMapField(m map[string]any, key string) map[string]any {
	if m == nil {
		return make(map[string]any)
	}
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return make(map[string]any)
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
