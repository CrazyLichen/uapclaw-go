// Package control 的配置面板读链服务。
// 对应 Python 代码：jiuwenswarm/server/control/config_service.py（584 行）。
package control

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/version"
	rec "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	common "github.com/uapclaw/uapclaw-go/internal/swarm/common"
	"gopkg.in/yaml.v3"
)

const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 常量 ────────────────────────────

// ConfigSetEnvMap 配置面板参数键 → 环境变量键对照表
// （Python: config_service.py:29-105 _CONFIG_SET_ENV_MAP，74 条目机器抄录）。
// 面板读一律以 .env 文件为准而非进程内存（跨进程时序修复，见模块 docstring）。
var ConfigSetEnvMap = map[string]string{

	"jev_openrouter_api_key":            "OPENROUTER_API_KEY",
	"jev_typesafe_api_key":              "TYPESAFE_API_KEY",
	"jev_custom_api_key":                "JEV_CUSTOM_API_KEY",
	"model_provider":                    "MODEL_PROVIDER",
	"model":                             "MODEL_NAME",
	"api_base":                          "API_BASE",
	"api_key":                           "API_KEY",
	"endpoint_profile":                  "ENDPOINT_PROFILE",
	"video_api_base":                    "VIDEO_API_BASE",
	"video_api_key":                     "VIDEO_API_KEY",
	"video_model":                       "VIDEO_MODEL_NAME",
	"video_provider":                    "VIDEO_PROVIDER",
	"video_endpoint_profile":            "VIDEO_ENDPOINT_PROFILE",
	"video_vendor_key":                  "VIDEO_VENDOR_KEY",
	"video_plan":                        "VIDEO_PLAN",
	"video_enabled":                     "VIDEO_ENABLED",
	"video_gen_api_base":                "VIDEO_GEN_API_BASE",
	"video_gen_api_key":                 "VIDEO_GEN_API_KEY",
	"video_gen_model":                   "VIDEO_GEN_MODEL_NAME",
	"video_gen_provider":                "VIDEO_GEN_PROVIDER",
	"video_gen_protocol":                "VIDEO_GEN_PROTOCOL",
	"video_gen_enabled":                 "VIDEO_GEN_ENABLED",
	"visual_gen_api_base":               "VISUAL_GEN_API_BASE",
	"visual_gen_api_key":                "VISUAL_GEN_API_KEY",
	"visual_gen_model":                  "VISUAL_GEN_MODEL_NAME",
	"visual_gen_provider":               "VISUAL_GEN_PROVIDER",
	"visual_gen_protocol":               "VISUAL_GEN_PROTOCOL",
	"visual_gen_enabled":                "VISUAL_GEN_ENABLED",
	"audio_api_base":                    "AUDIO_API_BASE",
	"audio_api_key":                     "AUDIO_API_KEY",
	"audio_model":                       "AUDIO_MODEL_NAME",
	"audio_provider":                    "AUDIO_PROVIDER",
	"audio_endpoint_profile":            "AUDIO_ENDPOINT_PROFILE",
	"audio_vendor_key":                  "AUDIO_VENDOR_KEY",
	"audio_plan":                        "AUDIO_PLAN",
	"audio_enabled":                     "AUDIO_ENABLED",
	"vision_api_base":                   "VISION_API_BASE",
	"vision_api_key":                    "VISION_API_KEY",
	"vision_model":                      "VISION_MODEL_NAME",
	"vision_provider":                   "VISION_PROVIDER",
	"vision_endpoint_profile":           "VISION_ENDPOINT_PROFILE",
	"vision_vendor_key":                 "VISION_VENDOR_KEY",
	"vision_plan":                       "VISION_PLAN",
	"vision_enabled":                    "VISION_ENABLED",
	"email_address":                     "EMAIL_ADDRESS",
	"email_token":                       "EMAIL_TOKEN",
	"embed_api_key":                     "EMBED_API_KEY",
	"embed_api_base":                    "EMBED_API_BASE",
	"embed_model":                       "EMBED_MODEL",
	"jina_api_key":                      "JINA_API_KEY",
	"bocha_api_key":                     "BOCHA_API_KEY",
	"serper_api_key":                    "SERPER_API_KEY",
	"perplexity_api_key":                "PERPLEXITY_API_KEY",
	"github_token":                      "GITHUB_TOKEN",
	"teamskills_market_url":             "TEAM_SKILLS_HUB_BASE_URL",
	"teamskills_user_token":             "TEAM_SKILLS_HUB_USER_TOKEN",
	"teamskills_system_token":           "TEAM_SKILLS_HUB_SYSTEM_TOKEN",
	"teamskills_allowed_download_hosts": "TEAM_SKILLS_HUB_ALLOWED_DOWNLOAD_HOSTS",
	"free_search_ddg_enabled":           "FREE_SEARCH_DDG_ENABLED",
	"free_search_bing_enabled":          "FREE_SEARCH_BING_ENABLED",
	"free_search_proxy_url":             "FREE_SEARCH_PROXY_URL",
	"skills":                            "SKILLS",
	"max_iterations":                    "MAX_ITERATIONS",
	"completion_timeout":                "COMPLETION_TIMEOUT",
	"team_name":                         "TEAM_NAME",
	"lifecycle":                         "LIFECYCLE",
	"teammate_mode":                     "TEAMATE_MODE",
	"spawn_mode":                        "SPAWN_MODE",
	"member_name":                       "MEMBER_NAME",
	"display_name":                      "DISPLAY_NAME",
	"persona":                           "PERSONA",
	"agent_key":                         "AGENT_KEY",
	"role_type":                         "ROLE_TYPE",
	"prompt_hint":                       "PROMPT_HINT",
}

// swarmflow/团队调度嵌套路径常量（Python: config_service.py:107-113）。
var (
	swarmflowEnabledPath       = []string{"modes", "team", "jiuwen_team", "enable_swarmflow"}
	swarmflowBudgetPath        = []string{"modes", "team", "jiuwen_team", "swarmflow_budget"}
	teamDispatchModePath       = []string{"modes", "team", "jiuwen_team", "dispatch_mode"}
	teamTaskVerificationPath   = []string{"modes", "team", "jiuwen_team", "enable_task_verification"}
	teamMemberMessagingPath    = []string{"modes", "team", "jiuwen_team", "enable_member_messaging"}
	teamMemberSubtaskDepthPath = []string{"modes", "team", "jiuwen_team", "max_subtask_depth"}
	kvCacheConfigKey           = "kv_cache_affinity_config"
	kvCacheEnabledKey          = "enable_kv_cache_affinity"
)

// envPlaceholderPattern ${VAR:-default} 展开模式（Python: config_service.py:349）。
var envPlaceholderPattern = regexp.MustCompile(`\$\{([^:}]+)(?::-([^}]*))?\}`)

// ──────────────────────────── 非导出函数 ────────────────────────────

// readRawConfig 读 config.yaml（Python: config_service.py:124 _read_raw_config）。
// 文件不存在或解析失败回空 map。
func readRawConfig() map[string]any {
	data, err := os.ReadFile(pathutil.ConfigFile())
	if err != nil {
		return map[string]any{}
	}
	var loaded map[string]any
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[config_service] failed to read config.yaml")
		return map[string]any{}
	}
	if loaded == nil {
		return map[string]any{}
	}
	return loaded
}

// nested 沿路径取嵌套值（Python: config_service.py:137 _nested）。
// 途中非 map 或终点为 nil 回默认值。
func nested(raw map[string]any, path []string, defaultVal any) any {
	current := any(raw)
	for _, segment := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return defaultVal
		}
		current = m[segment]
	}
	if current == nil {
		return defaultVal
	}
	return current
}

// depthSwitchText 把 max_subtask_depth 渲染成成员子任务开关：
// 有布尔值恒 false；深度 > 0 才为 true（核心 F_128）
// （Python: config_service.py:146 _depth_switch_text）。
func depthSwitchText(value any) string {
	if _, isBool := value.(bool); isBool {
		return "false"
	}
	switch v := value.(type) {
	case int:
		if v > 0 {
			return "true"
		}
		return "false"
	case int64:
		if v > 0 {
			return "true"
		}
		return "false"
	case float64:
		if v > 0 {
			return "true"
		}
		return "false"
	case string:
		if trimmed := strings.TrimSpace(v); trimmed != "" && parsePositiveInt(trimmed) {
			return "true"
		}
	}
	return "false"
}

// parsePositiveInt 判断字符串能否解析为 > 0 整数（对应 Python int(value) 语义）。
func parsePositiveInt(s string) bool {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return false
	}
	return n > 0
}

// boolText 任意值 → "true"/"false" 文本（Python: config_service.py:156 _bool_text）。
func boolText(value any, defaultVal bool) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	if value == nil {
		return defaultVal
	}
	switch strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", value))) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// affinityEnabled KV 缓存亲和开关（Python: config_service.py:164 _affinity_enabled）。
// 先看顶层 kv_cache_affinity_config.enable_kv_cache_affinity，再看 react 段旧位置。
func affinityEnabled(raw map[string]any) bool {
	if canonical, ok := raw[kvCacheConfigKey].(map[string]any); ok {
		if _, has := canonical[kvCacheEnabledKey]; has {
			return boolValue(canonical[kvCacheEnabledKey])
		}
	}
	react, ok := raw["react"].(map[string]any)
	if !ok {
		return false
	}
	legacy, ok := react[kvCacheConfigKey].(map[string]any)
	if !ok {
		return false
	}
	return boolValue(legacy[kvCacheEnabledKey])
}

// boolValue 宽松转 bool（对应 Python bool()）。
func boolValue(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return v != nil && v != "" && v != float64(0) && v != int(0)
}

// flattenSymphony Symphony 段展平（Python: config_service.py:175 _flatten_symphony）。
func flattenSymphony(raw map[string]any) map[string]string {
	symphony, _ := raw["symphony"].(map[string]any)
	evolution, _ := symphony["evolution"].(map[string]any)
	flow, _ := evolution["flow"].(map[string]any)
	flowEnabled := any(flow["enabled"])
	if flowEnabled == nil {
		flowEnabled = evolution["enabled"]
	}
	retrieval, _ := symphony["skill_retrieval"].(map[string]any)
	index, _ := retrieval["index"].(map[string]any)
	discovery, _ := retrieval["discovery"].(map[string]any)
	return map[string]string{
		"symphony_enabled":                             boolTextStr(config.ResolveSymphonyEnabled(symphony["enabled"])),
		"symphony_evolution_enabled":                   boolTextStr(config.ResolveSymphonyEvolutionEnabled(flowEnabled)),
		"skill_retrieval_enabled":                      boolTextStr(boolText(retrieval["enabled"], false)),
		"skill_retrieval_index_enabled":                boolTextStr(boolText(index["enabled"], false)),
		"skill_retrieval_index_recommendation_shown":   boolTextStr(boolText(index["recommendation_shown"], false)),
		"skill_retrieval_max_results":                  strOrDefault(discovery["max_results"], 10),
		"skill_retrieval_max_output_chars":             strOrDefault(discovery["max_output_chars"], 12000),
		"skill_retrieval_max_list_entries":             strOrDefault(discovery["max_list_entries"], 40),
		"skill_retrieval_incremental_notice_max_chars": strOrDefault(discovery["incremental_notice_max_chars"], 4000),
	}
}

// flattenRecommendation 推荐两开关（Python: config_service.py:214 _flatten_recommendation
// → recommendation_switches.py:120 flatten_recommendation_for_config_panel）。
func flattenRecommendation(raw map[string]any) map[string]string {
	section, _ := raw["recommendation"].(map[string]any)
	// Python _switch_value：键存在（含显式 null）走 resolve，否则回默认。
	// 默认值 DEFAULT_TOPIC/PROMPT = false（recommendation_switches.py:25-26）。
	topicEnabled := false
	promptEnabled := false
	if section != nil {
		if _, has := section["topic_enabled"]; has {
			topicEnabled = rec.ResolveRecommendationSwitch(section["topic_enabled"], false)
		}
		if _, has := section["prompt_enabled"]; has {
			promptEnabled = rec.ResolveRecommendationSwitch(section["prompt_enabled"], false)
		}
	}
	return map[string]string{
		"topic_recommendation_enabled": boolTextStr(topicEnabled),
		"prompt_suggestion_enabled":    boolTextStr(promptEnabled),
	}
}

// flattenExternalCli 外部 CLI 段展平（Python: config_service.py:228 _flatten_external_cli）。
// 上传 CLI 路径由注册表 GetCustomAgent 提供（语义对齐 Python _uploaded_cli_path）。
func flattenExternalCli(raw map[string]any) map[string]string {
	kinds := common.ExternalCliAgentKinds()
	uploadedCLIPaths := map[string]string{}
	for _, kind := range kinds {
		if agent := common.GetCustomAgent(kind); agent != nil && agent.CLIPath != "" {
			uploadedCLIPaths[kind] = agent.CLIPath
		}
	}
	flat := common.FlattenExternalCliAgentsForConfigPanel(raw, kinds, uploadedCLIPaths)
	flat["external_cli_custom_agents"] = common.CustomAgentsConfigValue()
	return flat
}

// flattenModesTeam 团队模式段展平（Python: config_service.py:240 _flatten_modes_team）。
func flattenModesTeam(raw map[string]any) map[string]string {
	modes, _ := raw["modes"].(map[string]any)
	teamsRaw, _ := modes["team"].(map[string]any)
	if teamsRaw == nil {
		teamsRaw = map[string]any{}
	}
	flat := map[string]string{}
	// agentSpecs 保持 Python dict 插入序（Go map 无序，用有序切片承载），
	// agent_name_N 索引依赖该顺序。
	agentSpecs := map[string]map[string]any{}
	var agentSpecOrder []string
	if panelCfg, ok := raw["web_config_panel"].(map[string]any); ok {
		if registry, ok := panelCfg["agent_team_agents"].(map[string]any); ok {
			for agentKey, specAny := range registry {
				if spec, ok := specAny.(map[string]any); ok && agentKey != "" {
					if _, exists := agentSpecs[agentKey]; !exists {
						agentSpecOrder = append(agentSpecOrder, agentKey)
					}
					agentSpecs[agentKey] = spec
				}
			}
		}
	}

	// addAgent：登记 agent spec（已存在则不覆盖）（Python: :255 内嵌 add_agent）
	addAgent := func(agentKey string, spec any) string {
		if specMap, ok := spec.(map[string]any); ok && agentKey != "" {
			if _, exists := agentSpecs[agentKey]; !exists {
				agentSpecs[agentKey] = specMap
				agentSpecOrder = append(agentSpecOrder, agentKey)
			}
		}
		return agentKey
	}
	// model_name_from_spec：三层模型名解析（Python: :260 内嵌 model_name_from_spec）
	modelNameFromSpec := func(spec map[string]any) string {
		modelCfg, ok := spec["model"].(map[string]any)
		if !ok {
			return ""
		}
		if modelCfg["model"] != nil {
			return fmt.Sprintf("%v", modelCfg["model"])
		}
		if requestCfg, ok := modelCfg["model_request_config"].(map[string]any); ok && requestCfg["model"] != nil {
			return fmt.Sprintf("%v", requestCfg["model"])
		}
		if clientCfg, ok := modelCfg["model_client_config"].(map[string]any); ok && clientCfg["model_name"] != nil {
			return fmt.Sprintf("%v", clientCfg["model_name"])
		}
		return ""
	}

	// 团队遍历：前 10 个（Python: :291 enumerate + team_idx >= 10 continue）
	teamNames := sortedMapKeys(teamsRaw)
	for teamIdx, teamName := range teamNames {
		if teamIdx >= 10 {
			break
		}
		teamSpec, ok := teamsRaw[teamName].(map[string]any)
		if !ok {
			continue
		}
		prefix := fmt.Sprintf("team_%d_", teamIdx)
		flat[prefix+"name"] = strOrEmpty(teamSpec["team_name"], teamName)
		flat[prefix+"lifecycle"] = fmt.Sprintf("%v", orEmpty(teamSpec["lifecycle"]))
		flat[prefix+"teammate_mode"] = fmt.Sprintf("%v", orEmpty(teamSpec["teammate_mode"]))
		flat[prefix+"spawn_mode"] = fmt.Sprintf("%v", orEmpty(teamSpec["spawn_mode"]))
		if boolValue(teamSpec["enable_permissions"]) {
			flat[prefix+"enable_permissions"] = "true"
		} else {
			flat[prefix+"enable_permissions"] = "false"
		}
		if agents, ok := teamSpec["external_cli_agents"].([]any); ok {
			data, _ := json.Marshal(agents)
			flat[prefix+"external_cli_agents"] = string(data)
		} else {
			flat[prefix+"external_cli_agents"] = ""
		}
		agentsMap, _ := teamSpec["agents"].(map[string]any)
		if agentsMap == nil {
			agentsMap = map[string]any{}
		}
		// leader 三字段（Python: :305-307）
		if leader, ok := teamSpec["leader"].(map[string]any); ok {
			for _, key := range []string{"member_name", "display_name", "persona"} {
				flat[prefix+"leader_"+key] = fmt.Sprintf("%v", orEmpty(leader[key]))
			}
		}
		leaderKey := ""
		if leader, ok := teamSpec["leader"].(map[string]any); ok {
			leaderKey = strings.TrimSpace(fmt.Sprintf("%v", orEmpty(leader["agent_key"])))
		}
		if leaderKey == "" {
			leaderKey = teamName + "_leader"
		}
		flat[prefix+"leader_agent_key"] = addAgent(leaderKey, agentsMap["leader"])
		// teammate 分支（Python: :311-318）
		if teammateSpec, ok := agentsMap["teammate"].(map[string]any); ok {
			teammateKey := ""
			if teammate, ok := teamSpec["teammate"].(map[string]any); ok {
				teammateKey = strings.TrimSpace(fmt.Sprintf("%v", orEmpty(teammate["agent_key"])))
			}
			if teammateKey == "" {
				teammateKey = teamName + "_teammate"
			}
			flat[prefix+"teammate_agent_key"] = addAgent(teammateKey, teammateSpec)
		} else {
			flat[prefix+"teammate_agent_key"] = ""
		}
		// predefined_members（Python: :319-338）
		membersOut := []map[string]string{}
		if members, ok := teamSpec["predefined_members"].([]any); ok {
			for _, m := range members {
				member, ok := m.(map[string]any)
				if !ok {
					continue
				}
				memberName := fmt.Sprintf("%v", orEmpty(member["member_name"]))
				agentKey := fmt.Sprintf("%v", orEmpty(member["agent_key"]))
				if agentKey == "" && memberName != "" {
					agentKey = teamName + "_" + memberName
				}
				if agentKey != "" {
					addAgent(agentKey, agentsMap[memberName])
				}
				membersOut = append(membersOut, map[string]string{
					"member_name":  memberName,
					"display_name": fmt.Sprintf("%v", orEmpty(member["display_name"])),
					"persona":      fmt.Sprintf("%v", orEmpty(member["persona"])),
					"prompt_hint":  fmt.Sprintf("%v", orEmpty(member["prompt_hint"])),
					"agent_key":    agentKey,
				})
			}
		}
		data, _ := json.Marshal(membersOut)
		flat[prefix+"predefined_members"] = string(data)
	}
	// agent 索引展平（Python: :339-345，插入序前 10 个）
	for agentIdx, agentKey := range agentSpecOrder {
		if agentIdx >= 10 {
			break
		}
		spec := agentSpecs[agentKey]
		flat[fmt.Sprintf("agent_name_%d", agentIdx)] = agentKey
		flat[fmt.Sprintf("agent_model_%d", agentIdx)] = modelNameFromSpec(spec)
		if skills, ok := spec["skills"].([]any); ok {
			items := make([]string, 0, len(skills))
			for _, item := range skills {
				items = append(items, fmt.Sprintf("%v", item))
			}
			flat[fmt.Sprintf("agent_skills_%d", agentIdx)] = strings.Join(items, ",")
		} else {
			flat[fmt.Sprintf("agent_skills_%d", agentIdx)] = ""
		}
		flat[fmt.Sprintf("agent_max_iterations_%d", agentIdx)] = strOrDefault(spec["max_iterations"], 200)
		flat[fmt.Sprintf("agent_completion_timeout_%d", agentIdx)] = strOrDefault(spec["completion_timeout"], 600)
	}
	return flat
}

// resolveEnvVars 展开 ${VAR:-default}（Python: config_service.py:345 _resolve_env_vars）。
// 字符串做替换；dict/list 递归；其余原样。
func resolveEnvVars(value any) any {
	switch v := value.(type) {
	case string:
		return envPlaceholderPattern.ReplaceAllStringFunc(v, func(match string) string {
			groups := envPlaceholderPattern.FindStringSubmatch(match)
			varName := groups[1]
			def := groups[2]
			current, has := os.LookupEnv(varName)
			if def != "" || hasDef(groups) {
				if !has || current == "" {
					return def
				}
				return current
			}
			if !has {
				return ""
			}
			return current
		})
	case map[string]any:
		resolved := map[string]any{}
		for key, item := range v {
			resolved[key] = resolveEnvVars(item)
		}
		return resolved
	case []any:
		resolved := make([]any, 0, len(v))
		for _, item := range v {
			resolved = append(resolved, resolveEnvVars(item))
		}
		return resolved
	}
	return value
}

// hasDef 判断 ${VAR:-default} 语法中 default 段是否显式存在（含空 default）。
// FindStringSubmatch 对 “${VAR:-}“ 的 group(2) 是空串但正则已命中 “:-“——
// 通过整体匹配串还原判断。
func hasDef(groups []string) bool {
	return strings.Contains(groups[0], ":-")
}

// resolveBrowserPath 浏览器 chrome 路径解析（Python: config_service.py:367 _resolve_browser_path）。
func resolveBrowserPath(browser map[string]any) string {
	resolved, ok := resolveEnvVars(browser).(map[string]any)
	if !ok {
		return ""
	}
	chromePath := resolved["chrome_path"]
	if s, ok := chromePath.(string); ok {
		return strings.TrimSpace(s)
	}
	m, ok := chromePath.(map[string]any)
	if !ok {
		return ""
	}
	platformMap := map[string]string{
		"windows": "windows",
		"darwin":  "macos",
		"linux":   "linux",
	}
	keys := []string{}
	if mapped, ok := platformMap[runtimePlatformKey()]; ok {
		keys = append(keys, mapped)
	}
	keys = append(keys, "default")
	for _, key := range keys {
		value, ok := m[key].(string)
		if ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// runtimePlatformKey 当前平台键（对应 Python sys.platform 映射精简后）。
func runtimePlatformKey() string {
	switch osRuntimeGOOS() {
	case "windows":
		return "windows"
	case "darwin":
		return "darwin"
	default:
		return "linux"
	}
}

// browserCredentialEnvironment 浏览器凭据环境（Python: config_service.py:390
// _browser_credential_environment）。Runtime 在另一进程写凭据，Front 启动时的
// 环境可能过期——.env 文件值覆盖进程内 os.environ。
func browserCredentialEnvironment() map[string]string {
	environment := map[string]string{}
	for _, kv := range os.Environ() {
		if idx := strings.Index(kv, "="); idx > 0 {
			environment[kv[:idx]] = kv[idx+1:]
		}
	}
	for key, value := range dotenvValues(pathutil.EnvFile()) {
		if value != "" {
			environment[key] = value
		}
	}
	return environment
}

// dotenvValues 解析 .env 文件为键值表（Python dotenv_values 语义）：
// 跳过注释行与空行，KEY=VALUE 去引号，值可含 =；文件不存在回空表。
func dotenvValues(dotenvPath string) map[string]string {
	result := map[string]string{}
	data, err := os.ReadFile(dotenvPath)
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		idx := strings.Index(trimmed, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		value := strings.TrimSpace(trimmed[idx+1:])
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		result[key] = value
	}
	return result
}

// getPanel 配置面板 payload（Python: config_service.py:402 get_panel）。
// 步骤顺序逐条对应：env 键回读（.env 优先）→ 版本/平台 → raw 各开关 →
// 预算条件写入 → flatten 四连 → ddg/bing 缺省补 false → enable_free_models；
// 任意异常走 setdefault 兜底分支（:523-560）。
func getPanel() map[string]any {
	environment := browserCredentialEnvironment()
	payload := map[string]any{}
	for paramKey, envKey := range ConfigSetEnvMap {
		payload[paramKey] = orStrEmpty(environment[envKey])
	}
	payload["app_version"] = version.Version
	runtimePlatform := strings.ToLower(strings.TrimSpace(os.Getenv("JIUWENSWARM_RUNTIME_PLATFORM")))
	if runtimePlatform == "" {
		runtimePlatform = "default"
	}
	payload["runtime_platform"] = runtimePlatform
	if runtimePlatform == "harmony" {
		payload["external_cli_agents_supported"] = "false"
	} else {
		payload["external_cli_agents_supported"] = "true"
	}

	deferCatch := func() {
		logger.Warn(logComponent).Msg("[config_service] panel yaml merge failed")
		// Python: config_service.py:525-560 setdefault 兜底全表
		setdefaults := map[string]any{
			"context_engine_enabled":                         "false",
			"kv_cache_affinity_enabled":                      "false",
			"permissions_enabled":                            "false",
			"setup_guide_enabled":                            "true",
			"skill_evolution":                                "false",
			"memory_forbidden_enabled":                       "false",
			"memory_forbidden_description":                   "",
			"a2ui_enabled":                                   "false",
			"trajectory_ui_enabled":                          "false",
			"trajectory_ui_diagnosis_enabled":                "false",
			"team_organization_ui_enabled":                   "false",
			"comfyui_workflow_enabled":                       "false",
			"progressive_tool_optimize":                      "false",
			"swarmflow_enabled":                              "false",
			"team_scheduled_dispatch_enabled":                "false",
			"team_task_verification_enabled":                 "false",
			"team_member_messaging_enabled":                  "false",
			"team_member_subtasks_enabled":                   "false",
			"free_search_ddg_enabled":                        "false",
			"free_search_bing_enabled":                       "false",
			"proactive_recommendation_enabled":               "false",
			"topic_recommendation_enabled":                   "false",
			"prompt_suggestion_enabled":                      "false",
			"proactive_recommendation_max_recommend_per_day": "10",
			"proactive_recommendation_max_rounds_per_tick":   "20",
			"enable_free_models":                             "true",
		}
		for key, value := range setdefaults {
			if _, exists := payload[key]; !exists {
				payload[key] = value
			}
		}
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				deferCatch()
			}
		}()

		raw := readRawConfig()
		setupGuideCfg, _ := raw["setup_guide"].(map[string]any)
		payload["setup_guide_enabled"] = boolTextStr(boolText(setupGuideCfg["enabled"], true))
		reactCfg, _ := raw["react"].(map[string]any)
		ctxCfg, _ := reactCfg["context_engine_config"].(map[string]any)
		payload["context_engine_enabled"] = boolTextStr(boolText(ctxCfg["enabled"], false))
		if affinityEnabled(raw) {
			payload["kv_cache_affinity_enabled"] = "true"
		} else {
			payload["kv_cache_affinity_enabled"] = "false"
		}
		permCfg, _ := raw["permissions"].(map[string]any)
		payload["permissions_enabled"] = boolTextStr(boolText(permCfg["enabled"], false))
		evolutionCfg, _ := reactCfg["evolution"].(map[string]any)
		payload["skill_evolution"] = boolTextStr(boolText(evolutionCfg["skill_evolution"], false))
		memoryCfgAll, _ := raw["memory"].(map[string]any)
		memoryCfg, _ := memoryCfgAll["forbidden_memory_definition"].(map[string]any)
		payload["memory_forbidden_enabled"] = boolTextStr(boolText(memoryCfg["enabled"], false))
		payload["memory_forbidden_description"] = orEmpty(memoryCfg["description"])
		// a2ui 开关：异常回 "false"（Python: :460-466 try/except）
		a2uiEnabled := "false"
		if cfg, err := GetA2UIConfig(raw); err == nil && cfg.Enabled {
			a2uiEnabled = "true"
		}
		payload["a2ui_enabled"] = a2uiEnabled
		trajectoryCfg, _ := raw["trajectory_ui"].(map[string]any)
		payload["trajectory_ui_enabled"] = boolTextStr(boolText(trajectoryCfg["enabled"], false))
		payload["trajectory_ui_diagnosis_enabled"] = boolTextStr(boolText(trajectoryCfg["diagnosis_enabled"], false))
		experimentalCfg, _ := raw["experimental"].(map[string]any)
		payload["team_organization_ui_enabled"] = boolTextStr(boolText(experimentalCfg["team_organization_ui_enabled"], false))
		payload["comfyui_workflow_enabled"] = boolTextStr(boolText(experimentalCfg["comfyui_workflow_enabled"], false))
		payload["progressive_tool_optimize"] = boolTextStr(boolText(raw["progressive_tool_optimize"], false))
		payload["swarmflow_enabled"] = boolTextStr(boolText(nested(raw, swarmflowEnabledPath, false), false))
		if dispatch := nested(raw, teamDispatchModePath, "autonomous"); strings.TrimSpace(fmt.Sprintf("%v", dispatch)) == "scheduled" {
			payload["team_scheduled_dispatch_enabled"] = "true"
		} else {
			payload["team_scheduled_dispatch_enabled"] = "false"
		}
		payload["team_task_verification_enabled"] = boolTextStr(boolText(nested(raw, teamTaskVerificationPath, false), false))
		payload["team_member_messaging_enabled"] = boolTextStr(boolText(nested(raw, teamMemberMessagingPath, false), false))
		payload["team_member_subtasks_enabled"] = depthSwitchText(nested(raw, teamMemberSubtaskDepthPath, 0))
		if budget := nested(raw, swarmflowBudgetPath, nil); budget != nil {
			payload["swarmflow_budget"] = fmt.Sprintf("%v", budget)
		}
		for key, value := range flattenExternalCli(raw) {
			payload[key] = value
		}
		for key, value := range flattenSymphony(raw) {
			payload[key] = value
		}
		for key, value := range flattenRecommendation(raw) {
			payload[key] = value
		}
		if payload["free_search_ddg_enabled"] == "" || payload["free_search_ddg_enabled"] == nil {
			payload["free_search_ddg_enabled"] = "false"
		}
		if payload["free_search_bing_enabled"] == "" || payload["free_search_bing_enabled"] == nil {
			payload["free_search_bing_enabled"] = "false"
		}
		for key, value := range flattenModesTeam(raw) {
			payload[key] = value
		}
		proactiveCfg, _ := raw["proactive_recommendation"].(map[string]any)
		payload["proactive_recommendation_enabled"] = boolTextStr(boolText(proactiveCfg["enabled"], false))
		payload["proactive_recommendation_max_recommend_per_day"] = strOrDefault(proactiveCfg["max_recommend_per_day"], 10)
		payload["proactive_recommendation_max_rounds_per_tick"] = strOrDefault(proactiveCfg["max_rounds_per_tick"], 20)
		modelsCfg, _ := raw["models"].(map[string]any)
		payload["enable_free_models"] = boolTextStr(boolText(modelsCfg["enable_free_models"], true))
	}()
	return payload
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetPanel 配置面板 payload（导出入口，Python: config_service.py:402 get_panel）。
func GetPanel() map[string]any {
	return getPanel()
}

// ControlConfigError control 只读方法不支持（HTTP 语义 BAD_REQUEST，
// Python: build_error_response(request, ..., code="BAD_REQUEST")）。
type ControlConfigError struct{ Message string }

// Error 实现 error 接口。
func (e *ControlConfigError) Error() string { return e.Message }

// HandleConfigRequest 只读配置请求分派（Python: config_service.py:532
// handle_config_request）。CONFIG_GET 回面板；LOCALE_GET_CONF 读偏好语言；
// PATH_GET 回浏览器路径 + decision 投影；MEMORY_FORBIDDEN_GET 回记忆禁区段；
// 其余返回 ControlConfigError（BAD_REQUEST 语义）。
func HandleConfigRequest(reqMethod string, params map[string]any) (map[string]any, error) {
	switch reqMethod {
	case "config.get":
		return getPanel(), nil
	}
	raw := readRawConfig()
	switch reqMethod {
	case "locale.get_conf":
		lang := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", orEmpty(raw["preferred_language"]))))
		if lang != "zh" && lang != "en" {
			lang = "zh"
		}
		return map[string]any{"preferred_language": lang}, nil
	case "path.get":
		browser, _ := raw["browser"].(map[string]any)
		if browser == nil {
			browser = map[string]any{}
		}
		decision := any(browser["decision"])
		decisionMap := map[string]any{}
		if dm, ok := decision.(map[string]any); ok {
			if resolved, ok := resolveEnvVars(dm).(map[string]any); ok {
				decisionMap = resolved
			}
		}
		headless := true
		if h, ok := browser["headless"].(bool); ok {
			headless = h
		}
		payload := map[string]any{
			"chrome_path": resolveBrowserPath(browser),
			"headless":    headless,
		}
		for key, value := range common.ProjectBrowserDecision(decisionMap, browserCredentialEnvironment()) {
			payload[key] = value
		}
		return payload, nil
	case "memory_forbidden.get":
		memoryCfgAll, _ := raw["memory"].(map[string]any)
		payload, _ := memoryCfgAll["forbidden_memory_definition"].(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		return payload, nil
	}
	return nil, &ControlConfigError{Message: "unsupported control config method"}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sortedMapKeys 排序 map 键（Python dict 保持插入序，Go map 无序——展平键名依赖
// 团队索引，排序保证确定性；首次出现顺序差异属已登记形态偏差 D-2 补充说明）。
func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// osRuntimeGOOS 平台名（Go runtime.GOOS 等价；独立函数便于测试注入）。
func osRuntimeGOOS() string { return goos }

// boolTextStr bool → "true"/"false"。
func boolTextStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// strOrDefault 值非空转字符串否则用默认（Python str(value.get(k, default))）。
func strOrDefault(value any, defaultVal any) string {
	if value == nil {
		return fmt.Sprintf("%v", defaultVal)
	}
	return fmt.Sprintf("%v", value)
}

// orEmpty 值为 nil/空回空串（Python `x or ""`）。
func orEmpty(value any) any {
	if value == nil {
		return ""
	}
	return value
}

// strOrEmpty 两候选取先非空（Python `a or b or ""`）。
func strOrEmpty(a, b any) string {
	if s, ok := a.(string); ok && s != "" {
		return s
	}
	if s, ok := b.(string); ok && s != "" {
		return s
	}
	if a != nil {
		return fmt.Sprintf("%v", a)
	}
	if b != nil {
		return fmt.Sprintf("%v", b)
	}
	return ""
}

// orStrEmpty 空回空串。
func orStrEmpty(s string) string {
	if s == "" {
		return ""
	}
	return s
}

// goos 当前平台（runtime.GOOS，独立变量便于测试）。
var goos = runtime.GOOS
