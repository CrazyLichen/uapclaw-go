// Package configpanel 配置面板写入层（27.4）。
// 对齐 Python jiuwenswarm/common/config_panel/config_set_handlers.py 的写链：
// ConfigChangeSet 变更记账与 reload_scopes、config.set/save_all 参数落盘
// （.env + config.yaml）与重载通知。
package configpanel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

const logComponentConfigPanel = logger.ComponentAgentServer

// ──────────────────────────── 结构体 ────────────────────────────

// ConfigPanelBadRequest config 面板入参非法（Python: config_set_handlers.py:68
// ConfigPanelBadRequest，HTTP 语义 400）。
type ConfigPanelBadRequest struct {
	Message string
}

// Error 实现 error 接口。
func (e *ConfigPanelBadRequest) Error() string { return e.Message }

// ConfigPanelInternalError config 域写盘/内部失败（Python :139，HTTP 语义 500）。
type ConfigPanelInternalError struct {
	Message string
}

// Error 实现 error 接口。
func (e *ConfigPanelInternalError) Error() string { return e.Message }

// ConfigChangeSet 变更集（Python :201 ConfigChangeSet，frozen dataclass）。
type ConfigChangeSet struct {
	// EnvUpdates 本次变更的环境变量增量
	EnvUpdates map[string]string
	// YAMLUpdated 本次变更的 config.yaml 键列表
	YAMLUpdated []string
	// Force 强制触发重载（save_all 语义）
	Force bool
}

// Changed 是否有实际变更（Python :207 changed）。
func (c ConfigChangeSet) Changed() bool {
	return c.Force || len(c.EnvUpdates) > 0 || len(c.YAMLUpdated) > 0
}

// UpdatedKeys 变更键集合（Python :211 updated_keys）。
func (c ConfigChangeSet) UpdatedKeys() []string {
	seen := map[string]bool{}
	for k := range c.EnvUpdates {
		seen[k] = true
	}
	for _, k := range c.YAMLUpdated {
		seen[k] = true
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ReloadScopes 热更新 scope 推导（Python :215 reload_scopes，逐分支对齐）。
func (c ConfigChangeSet) ReloadScopes() []string {
	scopes := map[string]bool{}
	envKeys := make(map[string]bool, len(c.EnvUpdates))
	for k := range c.EnvUpdates {
		envKeys[k] = true
	}
	if intersectBool(envKeys, SearchReloadEnvKeys) {
		scopes["search"] = true
	}
	if intersectBool(envKeys, ModelReloadEnvKeys) {
		scopes["model"] = true
	}
	if intersectBool(envKeys, MultimodalReloadEnvKeys) {
		scopes["multimodal"] = true
	}
	if intersectBool(envKeys, ASREnvKeys) {
		scopes["web_ui"] = true
	}
	if intersectBool(envKeys, SearchReloadEnvKeys) {
		scopes["search"] = true
	}
	for _, key := range c.YAMLUpdated {
		keyText := key
		// Python: MOA_CONFIG_KEYS 命中 → web_ui（MOA 缺口 ⤵️：Go 侧 moa_config
		// 未落地，键集暂空，命中分支随 MOA 域回填）。
		if moaConfigKeys[keyText] {
			scopes["web_ui"] = true
			continue
		}
		switch {
		case keyText == "models.defaults" || strings.HasPrefix(keyText, "models."):
			scopes["model"] = true
		case containsAny(keyText, []string{"modes.team", "agents", "team"}) || teamDispatchConfigKeys[keyText]:
			scopes["team"] = true
		case strings.HasPrefix(keyText, "permissions"):
			scopes["permissions"] = true
		case strings.HasPrefix(keyText, "proactive_recommendation"):
			scopes["proactive"] = true
		case strings.HasPrefix(keyText, "symphony") || strings.HasPrefix(keyText, "skill_retrieval"):
			scopes["agent_runtime"] = true
		case keyText == "trajectory_ui_diagnosis_enabled":
			scopes["agent_runtime"] = true
			scopes["web_ui"] = true
		case webUIYAMLKeys[keyText] || strings.HasPrefix(keyText, "a2ui_"):
			scopes["web_ui"] = true
		default:
			scopes["agent_runtime"] = true
		}
	}
	// Python: force 且无 scope 且非「仅 MOA yaml」→ agent_runtime
	if c.Force && len(scopes) == 0 {
		scopes["agent_runtime"] = true
	}
	out := make([]string, 0, len(scopes))
	for k := range scopes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ConfigApplyResult 落盘结果（Python :264 ConfigApplyResult）。
type ConfigApplyResult struct {
	// EnvUpdates 环境变量增量
	EnvUpdates map[string]string
	// YAMLUpdated yaml 键列表
	YAMLUpdated []string
	// CanonicalConfig 权限画像规范化键值（permissions_profile 分支）
	CanonicalConfig map[string]string
	// PendingPermissionProfile 待提交权限画像（空串=无）
	PendingPermissionProfile string
	// PendingPermissionKey 待提交键名
	PendingPermissionKey string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// 热更新 scope 常量（Python :156-195）。
var (
	// SearchReloadEnvKeys 搜索类 env 键（Python SEARCH_RELOAD_ENV_KEYS）
	SearchReloadEnvKeys = map[string]bool{
		"BOCHA_API_KEY": true, "PERPLEXITY_API_KEY": true,
		"SERPER_API_KEY": true, "JINA_API_KEY": true,
	}
	// ModelReloadEnvKeys 模型 env 键（Python MODEL_RELOAD_ENV_KEYS）
	ModelReloadEnvKeys = map[string]bool{
		"MODEL_PROVIDER": true, "MODEL_NAME": true,
		"API_BASE": true, "API_KEY": true,
	}
	// MultimodalReloadEnvKeys 多模态 env 键（Python MULTIMODAL_RELOAD_ENV_KEYS）
	MultimodalReloadEnvKeys = map[string]bool{
		"VIDEO_PROVIDER": true, "VIDEO_MODEL_NAME": true, "VIDEO_API_BASE": true,
		"VIDEO_API_KEY": true, "VIDEO_ENDPOINT_PROFILE": true, "VIDEO_CONTEXT_WINDOW_TOKENS": true,
		"AUDIO_PROVIDER": true, "AUDIO_MODEL_NAME": true, "AUDIO_API_BASE": true,
		"AUDIO_API_KEY": true, "AUDIO_ENDPOINT_PROFILE": true, "AUDIO_CONTEXT_WINDOW_TOKENS": true,
		"VISION_PROVIDER": true, "VISION_MODEL_NAME": true, "VISION_API_BASE": true,
		"VISION_API_KEY": true, "VISION_ENDPOINT_PROFILE": true, "VISION_CONTEXT_WINDOW_TOKENS": true,
		"VISION_ENABLED": true, "AUDIO_ENABLED": true, "VIDEO_ENABLED": true,
		"VIDEO_GEN_ENABLED": true, "VIDEO_GEN_API_BASE": true, "VIDEO_GEN_API_KEY": true,
		"VIDEO_GEN_MODEL_NAME": true, "VIDEO_GEN_PROVIDER": true, "VIDEO_GEN_PROTOCOL": true,
		"VIDEO_GEN_CONTEXT_WINDOW_TOKENS": true,
		"VISUAL_GEN_ENABLED":              true, "VISUAL_GEN_API_BASE": true, "VISUAL_GEN_API_KEY": true,
		"VISUAL_GEN_MODEL_NAME": true, "VISUAL_GEN_PROVIDER": true, "VISUAL_GEN_PROTOCOL": true,
		"VISUAL_GEN_CONTEXT_WINDOW_TOKENS": true,
	}
	// ASREnvKeys 语音识别 env 键（Python ASR_ENV_KEYS）
	ASREnvKeys = map[string]bool{
		"ASR_API_BASE": true, "ASR_API_KEY": true, "ASR_MODEL_NAME": true,
	}
)

// WebConfigReloadChannelID 热更新目标通道（Python :155 WEB_CONFIG_RELOAD_CHANNEL_ID）。
const WebConfigReloadChannelID = "web"

var (
	// TeamDispatchConfigKeys 调度模式键族（Python :424）
	teamDispatchConfigKeys = map[string]bool{
		"team_scheduled_dispatch_enabled": true,
		"team_task_verification_enabled":  true,
		"team_member_messaging_enabled":   true,
		"team_member_subtasks_enabled":    true,
	}
	// RecommendationUIConfigKeys 推荐面板 UI 键（Python :430）
	recommendationUIConfigKeys = map[string]bool{
		"topic_recommendation_enabled": true,
		"prompt_suggestion_enabled":    true,
	}
	// WebUIYAMLKeys 仅改 yaml 的 UI 开关（Python :435）
	webUIYAMLKeys = map[string]bool{
		"task_full_duplex_enabled":     true,
		"team_organization_ui_enabled": true,
		"comfyui_workflow_enabled":     true,
		"setup_guide_enabled":          true,
		"topic_recommendation_enabled": true,
		"prompt_suggestion_enabled":    true,
	}
	// AgentReloadScopes 必须等 AgentServer 应用的 scope（Python :443）
	AgentReloadScopes = map[string]bool{
		"model": true, "multimodal": true, "search": true, "team": true,
		"permissions": true, "agent_runtime": true, "proactive": true,
	}
	// PermissionsProfiles 合法权限画像（Python :476）
	PermissionsProfiles = map[string]bool{
		"default": true, "automatic": true, "full_access": true,
	}
	// moaConfigKeys MOA 配置键集（Python moa_config.MOA_CONFIG_KEYS；
	// Go 侧 moa_config 未落地 ⤵️ 待回填，暂空集）
	moaConfigKeys = map[string]bool{}
)

var proactiveIntPattern = regexp.MustCompile(`^[0-9]+$`)

// yamlSwitchTable yaml 开关写回表：param 键 → yaml 嵌套路径。
// 各路径逐条取自 Python common/config.py 对应 update_*_in_config 与
// config_service.get_panel 的读取路径（读/写路径必须一致）。
var yamlSwitchTable = map[string][]string{
	"context_engine_enabled":                         {"react", "context_engine_config", "enabled"},
	"setup_guide_enabled":                            {"setup_guide", "enabled"},
	"rsi_enabled":                                    {"rsi", "enabled"},
	"enable_free_models":                             {"models", "enable_free_models"},
	"skill_evolution":                                {"react", "evolution", "skill_evolution"},
	"ttse_enabled":                                   {"react", "ttse", "enabled"},
	"memory_forbidden_enabled":                       {"memory", "forbidden_memory_definition", "enabled"},
	"swarmflow_enabled":                              {"modes", "team", "jiuwen_team", "enable_swarmflow"},
	"swarmflow_budget":                               {"modes", "team", "jiuwen_team", "swarmflow_budget"},
	"trajectory_ui_enabled":                          {"trajectory_ui", "enabled"},
	"trajectory_ui_diagnosis_enabled":                {"trajectory_ui", "diagnosis_enabled"},
	"team_organization_ui_enabled":                   {"experimental", "team_organization_ui_enabled"},
	"comfyui_workflow_enabled":                       {"experimental", "comfyui_workflow_enabled"},
	"task_full_duplex_enabled":                       {"experimental", "task_full_duplex_enabled"},
	"task_asr_enabled":                               {"experimental", "task_asr_enabled"},
	"progressive_tool_optimize":                      {"progressive_tool_optimize"},
	"proactive_recommendation_enabled":               {"proactive_recommendation", "enabled"},
	"proactive_recommendation_max_recommend_per_day": {"proactive_recommendation", "max_recommend_per_day"},
	"proactive_recommendation_max_rounds_per_tick":   {"proactive_recommendation", "max_rounds_per_tick"},
	"team_scheduled_dispatch_enabled":                {"modes", "team", "jiuwen_team", "dispatch_mode"},
	"team_task_verification_enabled":                 {"modes", "team", "jiuwen_team", "enable_task_verification"},
	"team_member_messaging_enabled":                  {"modes", "team", "jiuwen_team", "enable_member_messaging"},
	"team_member_subtasks_enabled":                   {"modes", "team", "jiuwen_team", "max_subtask_depth"},
	"a2ui_enabled":                                   {"a2ui", "enabled"},
}

// 联动专用路径（非 param 键，直接引用）。
var (
	teamDispatchModePath     = []string{"modes", "team", "jiuwen_team", "dispatch_mode"}
	teamTaskVerificationPath = []string{"modes", "team", "jiuwen_team", "enable_task_verification"}
	teamMemberMessagingPath  = []string{"modes", "team", "jiuwen_team", "enable_member_messaging"}
	teamMemberSubtasksPath   = []string{"modes", "team", "jiuwen_team", "max_subtask_depth"}
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NeedsAgentReload 本保存是否必须等 AgentServer 应用（Python :454 needs_agent_reload）。
func NeedsAgentReload(scopes []string) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, s := range scopes {
		if AgentReloadScopes[s] {
			return true
		}
	}
	return false
}

// PermissionProfile 权限画像推导（Python :479 permission_profile）。
// permissions 段缺失或 enabled 非 true → full_access。
func PermissionProfile(permissionConfig any) string {
	cfg, ok := permissionConfig.(map[string]any)
	if !ok {
		return "full_access"
	}
	if enabled, isBool := cfg["enabled"].(bool); !isBool || !enabled {
		return "full_access"
	}
	// Python: is_auto_permission_mode → resolve_permission_runtime_mode == AUTO_PERMISSION_MODE。
	// runtime mode 解析器属权限域既有实现（internal 层 permission profile），
	// 这里按 mode 字段等价判定；复杂回落随权限域回填 ⤵️。
	if mode, ok := cfg["mode"].(string); ok && mode == "auto" {
		return "automatic"
	}
	return "default"
}

// CanonicalPermissionFacade 画像规范化键值（Python :485 canonical_permission_facade）。
func CanonicalPermissionFacade(profile string) map[string]string {
	enabled := "true"
	if profile == "full_access" {
		enabled = "false"
	}
	return map[string]string{
		"permissions_profile": profile,
		"permissions_enabled": enabled,
	}
}

// CoerceConfigPanelValue 面板取值强转（Python :529 coerce_config_panel_value）。
// value_type: bool/int/non_negative_int/raw_int/float/ratio/output_chars/其余回 string。
func CoerceConfigPanelValue(value any, valueType string, defaultVal any) (any, error) {
	switch valueType {
	case "bool":
		s := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", value)))
		switch s {
		case "true", "1", "yes", "on", "enabled":
			return true, nil
		}
		return false, nil
	case "int":
		n, ok := coerceInt(value)
		if !ok {
			return defaultVal, nil
		}
		return maxInt(1, n), nil
	case "non_negative_int":
		n, ok := coerceInt(value)
		if !ok {
			return defaultVal, nil
		}
		return maxInt(0, n), nil
	case "raw_int":
		n, ok := coerceInt(value)
		if !ok {
			return defaultVal, nil
		}
		return n, nil
	case "float":
		f, ok := coerceFloat(value)
		if !ok {
			return defaultVal, nil
		}
		return maxFloat(0.0, f), nil
	case "ratio":
		f, ok := coerceFloat(value)
		if !ok {
			return defaultVal, nil
		}
		if f > 0.0 && f <= 1.0 {
			return f, nil
		}
		return defaultVal, nil
	case "output_chars":
		n, ok := coerceInt(value)
		if !ok {
			return nil, &ConfigPanelBadRequest{Message: "max_output_chars must be an integer from 512 to 48000"}
		}
		if n < 512 || n > 48000 {
			return nil, &ConfigPanelBadRequest{Message: "max_output_chars must be from 512 to 48000"}
		}
		return n, nil
	}
	if value == nil {
		return fmt.Sprintf("%v", defaultVal), nil
	}
	return fmt.Sprintf("%v", value), nil
}

// SetNestedConfigValue 沿路径写嵌套键（Python :569 set_nested_config_value）。
func SetNestedConfigValue(target map[string]any, path []string, value any) {
	current := target
	for i, seg := range path {
		if i == len(path)-1 {
			current[seg] = value
			return
		}
		next, ok := current[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[seg] = next
		}
		current = next
	}
}

// GetNestedConfigValue 沿路径读嵌套键（Python :580 get_nested_config_value）。
func GetNestedConfigValue(source map[string]any, path []string, defaultVal any) any {
	current := source
	for i, seg := range path {
		v, ok := current[seg]
		if !ok {
			return defaultVal
		}
		if i == len(path)-1 {
			return v
		}
		next, ok := v.(map[string]any)
		if !ok {
			return defaultVal
		}
		current = next
	}
	return defaultVal
}

// ParseConfigBool 面板布尔解析（Python :1346 parse_config_bool → parse_kvc_bool）。
func ParseConfigBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "true" || s == "1" || s == "yes" || s == "on" || s == "enabled"
	case float64:
		return v != 0
	}
	return false
}

// EncryptConfigParams 敏感键加密（Python :1397 encrypt_config_params）。
// key 含 api_key/token（不含 _context_window_tokens 后缀）时加密；crypto 为 nil
// 或加密失败保留原值（AgentServer 无扩展时明文落库，spec D-3）。
func EncryptConfigParams(ctx context.Context, params map[string]any, cryptoProv crypto.CryptoProvider) map[string]any {
	out := make(map[string]any, len(params))
	for k, v := range params {
		out[k] = v
	}
	if cryptoProv == nil {
		return out
	}
	for key, val := range out {
		if strings.HasSuffix(key, "_context_window_tokens") {
			continue
		}
		kl := strings.ToLower(key)
		if strings.Contains(kl, "api_key") || strings.Contains(kl, "token") {
			if strVal, ok := val.(string); ok && strVal != "" {
				if enc, err := cryptoProv.Encrypt(ctx, strVal); err == nil {
					out[key] = enc
				} else {
					logger.Warn(logComponentConfigPanel).Str("key", key).Err(err).
						Msg("encrypt config param failed, keep plain value")
				}
			}
		}
	}
	return out
}

// ApplyConfigSet 将 config.set 风格参数落到 .env/config.yaml（不触发重载）。
// Python: config_set_handlers.py:1452 apply_config_payload。步骤逐段对齐；
// MOA 参数校验/保存、team 模板实体保留、codex/claude 依赖安装、外部 CLI 开关块、
// KV 亲和复合校验、推荐面板持久化六处缺口按 27.4 行 ⤵️ 登记待回填，命中时
// 跳过对应写盘并 Warn（不改变其余键的落盘结果）。
func ApplyConfigSet(ctx context.Context, params map[string]any, cryptoProv crypto.CryptoProvider) (*ConfigApplyResult, error) {
	if _, ok := params["permissions_mode"]; ok {
		return nil, &ConfigPanelBadRequest{Message: "permissions_mode is not writable"}
	}
	if _, hasProfile := params["permissions_profile"]; hasProfile {
		if _, hasEnabled := params["permissions_enabled"]; hasEnabled {
			return nil, &ConfigPanelBadRequest{Message: "permissions_profile and permissions_enabled are mutually exclusive"}
		}
	}
	// Python: moa_params 过滤 + build_moa_config_from_frontend 校验（⤵️ 缺口，
	// MOA 键集暂空 → moa_params 恒空，行为等价于「无 MOA 键」分支）。
	// Python: encrypt_config_params（排除 MOA 键——键集空即全量加密）
	params = EncryptConfigParams(ctx, params, cryptoProv)

	envUpdates := map[string]string{}
	yamlUpdated := []string{}
	canonicalConfig := map[string]string(nil)
	pendingProfile := ""
	pendingKey := ""

	// Python: permissions_profile / permissions_enabled 画像分支（:1520-1536）
	if raw, hasProfile := params["permissions_profile"]; hasProfile {
		pendingProfile = strings.TrimSpace(fmt.Sprintf("%v", raw))
		if !PermissionsProfiles[pendingProfile] {
			return nil, &ConfigPanelBadRequest{Message: "invalid permissions_profile"}
		}
		canonicalConfig = CanonicalPermissionFacade(pendingProfile)
	} else if raw, hasEnabled := params["permissions_enabled"]; hasEnabled {
		if ParseConfigBool(raw) {
			pendingProfile = "default"
		} else {
			pendingProfile = "full_access"
		}
		canonicalConfig = CanonicalPermissionFacade(pendingProfile)
		pendingKey = "permissions_enabled"
	}
	if _, hasProfile := params["permissions_profile"]; hasProfile {
		pendingKey = "permissions_profile"
	}

	// Python: CONFIG_SET_ENV_MAP 循环（:1539-1560）——上下文窗口解析 + provider 校验
	for paramKey, envKey := range configSetEnvMap {
		raw, ok := params[paramKey]
		if !ok {
			continue
		}
		val := raw
		if strings.HasSuffix(paramKey, "_context_window_tokens") && val != nil && fmt.Sprintf("%v", val) != "" {
			parsed := ParsePositiveInt(val)
			if parsed == 0 {
				return nil, &ConfigPanelBadRequest{
					Message: fmt.Sprintf("%s must be a positive integer or a value such as 256K or 1M", paramKey),
				}
			}
			val = fmt.Sprintf("%d", parsed)
		}
		if strings.HasSuffix(paramKey, "_provider") {
			strVal := strings.TrimSpace(fmt.Sprintf("%v", val))
			if strVal != "" && strVal != "<nil>" && !IsAvailableProvider(strVal) {
				return nil, &ConfigPanelBadRequest{
					Message: fmt.Sprintf("Model provider must in: %v", AvailableModelProviders()),
				}
			}
		}
		if val == nil {
			envUpdates[envKey] = ""
		} else {
			envUpdates[envKey] = strings.TrimSpace(fmt.Sprintf("%v", val))
		}
	}

	// Python: agents/team → replace_teams_in_config（⤵️ 缺口：team 模板实体保留
	// preserve_deleted_team_entities 依赖 team_binding_store，暂由调用侧既有
	// replaceTeamsInConfig 落盘替代；缺口随团队模板域回填）
	for _, teamKey := range []string{"agents", "team"} {
		if _, ok := params[teamKey]; ok {
			if err := ReplaceTeamsInConfig(params); err != nil {
				logger.Warn(logComponentConfigPanel).Err(err).Msg("failed to write back modes.team")
				return nil, &ConfigPanelInternalError{Message: "failed to update modes.team"}
			}
			yamlUpdated = append(yamlUpdated, "modes.team")
			break
		}
	}

	// Python: CONFIG_YAML_KEYS + custom_external_cli_config_keys 循环（:1541+）。
	// 外部 CLI 开关块保留 Gateway 本地实现（计划明示），此处处理其余开关。
	preferredLang := "zh"
	raw := ReadRawConfig()
	if lang, ok := raw["preferred_language"].(string); ok && strings.TrimSpace(lang) != "" {
		preferredLang = strings.TrimSpace(lang)
	}
	scheduled := teamDispatchScheduled(raw, params)

	for paramKey, val := range params {
		path, isSwitch := yamlSwitchTable[paramKey]
		if !isSwitch && paramKey != "memory_forbidden_description" {
			continue
		}
		parsed := ParseConfigBool(val)
		switch paramKey {
		case "context_engine_enabled", "setup_guide_enabled", "rsi_enabled",
			"enable_free_models", "skill_evolution", "ttse_enabled",
			"memory_forbidden_enabled", "trajectory_ui_enabled",
			"team_organization_ui_enabled", "comfyui_workflow_enabled",
			"task_full_duplex_enabled", "task_asr_enabled":
			writeYAMLPath(path, parsed)
		case "progressive_tool_optimize":
			writeYAMLPath(path, parsed)
		case "memory_forbidden_description":
			desc := strings.TrimSpace(fmt.Sprintf("%v", val))
			writeYAMLPath([]string{"memory", "forbidden_memory_definition", "description"},
				map[string]any{preferredLang: desc})
		case "swarmflow_enabled":
			writeYAMLPath(path, parsed)
		case "swarmflow_budget":
			budget := strings.TrimSpace(fmt.Sprintf("%v", val))
			if budget == "" || strings.EqualFold(budget, "none") || strings.EqualFold(budget, "null") {
				deleteYAMLPath(path)
			} else {
				writeYAMLPath(path, budget)
			}
		case "team_scheduled_dispatch_enabled":
			writeYAMLPath(teamDispatchModePath, dispatchModeText(parsed))
			if !parsed {
				// Python: 关闭调度模式时一并关闭验证者/成员通信/子任务（:1594-1601）
				writeYAMLPath(teamTaskVerificationPath, false)
				yamlUpdated = append(yamlUpdated, "team_task_verification_enabled")
				writeYAMLPath(teamMemberMessagingPath, false)
				yamlUpdated = append(yamlUpdated, "team_member_messaging_enabled")
				writeYAMLPath(teamMemberSubtasksPath, 0)
				yamlUpdated = append(yamlUpdated, "team_member_subtasks_enabled")
			}
		case "team_task_verification_enabled":
			writeYAMLPath(teamTaskVerificationPath, parsed && scheduled)
		case "team_member_messaging_enabled":
			writeYAMLPath(teamMemberMessagingPath, parsed && scheduled)
		case "team_member_subtasks_enabled":
			writeYAMLPath(teamMemberSubtasksPath, subtaskDepth(parsed && scheduled, val))
		case "trajectory_ui_diagnosis_enabled":
			writeYAMLPath(path, parsed)
		case "proactive_recommendation_enabled":
			writeYAMLPath(path, parsed)
		case "proactive_recommendation_max_recommend_per_day":
			n, err := ValidateProactiveInt(val, "每日推荐上限(max_recommend_per_day)")
			if err != nil {
				return nil, err
			}
			writeYAMLPath(path, n)
		case "proactive_recommendation_max_rounds_per_tick":
			n, err := ValidateProactiveInt(val, "每次检查对话轮数(max_rounds_per_tick)")
			if err != nil {
				return nil, err
			}
			writeYAMLPath(path, n)
		case "a2ui_enabled":
			// Python: provider("a2ui") 未注册时开关仍可写 yaml（:1656-1663 fallback）
			writeYAMLPath([]string{"a2ui", "enabled"}, parsed)
		case "kv_cache_affinity_enabled":
			// Python: update_kv_cache_affinity_enabled_in_config + 复合校验（⤵️ 缺口）
			logger.Warn(logComponentConfigPanel).Str("key", paramKey).
				Msg("kv_cache_affinity write skipped, pending backfill")
			continue
		case "topic_recommendation_enabled", "prompt_suggestion_enabled":
			// Python: save_recommendation_switches（⤵️ 缺口：推荐面板持久化随 10.3.31 回填）
			logger.Warn(logComponentConfigPanel).Str("key", paramKey).
				Msg("recommendation switch persist skipped, pending backfill")
			continue
		default:
			continue
		}
		yamlUpdated = append(yamlUpdated, paramKey)
	}

	// Python: ASCEND_AFFINITY provider → models.default_provider（:1700-1707）
	if provider, ok := params["model_provider"].(string); ok && provider == AscendAffinityProvider {
		if writeYAMLPath([]string{"models", "default_provider"}, provider) {
			yamlUpdated = append(yamlUpdated, "models.default_provider")
		}
	}

	// Python: symphony / skill_retrieval / moa_params 保存段（⤵️ 缺口登记，
	// moa/symphony 键集空 → 分支不进入，行为等价）。

	// Python: env 写入进程内 + persist_env_updates（:1717-1723）
	for envKey, value := range envUpdates {
		_ = os.Setenv(envKey, value)
	}
	if len(envUpdates) > 0 {
		PersistEnvUpdates(envUpdates)
		logger.Info(logComponentConfigPanel).Strs("keys", sortedKeysStr(envUpdates)).Msg(".env updated")
	}
	if len(yamlUpdated) > 0 {
		logger.Info(logComponentConfigPanel).Strs("keys", yamlUpdated).Msg("config.yaml updated")
	}

	// Python: KVC_CONFIG_CHANGED 复合校验（⤵️ 缺口随 kv_cache 域回填）。

	return &ConfigApplyResult{
		EnvUpdates:               envUpdates,
		YAMLUpdated:              yamlUpdated,
		CanonicalConfig:          canonicalConfig,
		PendingPermissionProfile: pendingProfile,
		PendingPermissionKey:     pendingKey,
	}, nil
}

// CommitPendingPermissionProfile 提交待定权限画像（Python :1764
// commit_pending_permission_profile）。写 permissions.enabled + mode，
// 并把键名追加进 yaml_updated。
func CommitPendingPermissionProfile(result *ConfigApplyResult) error {
	if result == nil || result.PendingPermissionProfile == "" {
		return nil
	}
	profile := result.PendingPermissionProfile
	writeYAMLPath([]string{"permissions", "enabled"}, profile != "full_access")
	if profile == "automatic" {
		writeYAMLPath([]string{"permissions", "mode"}, "auto")
	}
	result.YAMLUpdated = append(result.YAMLUpdated, result.PendingPermissionKey)
	return nil
}

// PersistEnvUpdates 把已更新的环境变量写回 .env（Python :1293 persist_env_updates：
// 仅覆盖或追加对应 KEY=value 行，带引号）。
func PersistEnvUpdates(updates map[string]string) {
	if len(updates) == 0 {
		return
	}
	envPath := pathutil.EnvFile()
	data, err := os.ReadFile(envPath)
	var lines []string
	if err == nil {
		lines = strings.Split(string(data), "\n")
	}
	newLines := make([]string, 0, len(lines)+len(updates))
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		replaced := false
		for envKey, value := range updates {
			if strings.HasPrefix(stripped, envKey+"=") {
				newLines = append(newLines, envLine(envKey, value))
				replaced = true
				break
			}
		}
		if !replaced && line != "" {
			newLines = append(newLines, line)
		}
	}
	for envKey, value := range updates {
		found := false
		for _, s := range newLines {
			if strings.HasPrefix(strings.TrimSpace(s), envKey+"=") {
				found = true
				break
			}
		}
		if !found {
			newLines = append(newLines, envLine(envKey, value))
		}
	}
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		logger.Warn(logComponentConfigPanel).Err(err).Msg("mkdir config dir failed")
		return
	}
	if err := os.WriteFile(envPath, []byte(strings.Join(newLines, "\n")+"\n"), 0o644); err != nil {
		logger.Warn(logComponentConfigPanel).Err(err).Msg("write .env failed")
	}
}

// ValidateProactiveInt 校验 proactive 数值（Python :1324 validate_proactive_int，
// 默认 1-50 正整数，挡浮点/负数/科学计数/空值）。
func ValidateProactiveInt(val any, name string) (int, error) {
	raw := strings.TrimSpace(fmt.Sprintf("%v", val))
	if val == nil || raw == "" || raw == "<nil>" {
		return 0, &ConfigPanelBadRequest{Message: fmt.Sprintf("%s 不能为空，需为 1-50 的正整数", name)}
	}
	if !proactiveIntPattern.MatchString(raw) {
		return 0, &ConfigPanelBadRequest{Message: fmt.Sprintf("%s 必须是正整数（1-50），当前值无效：%q", name, raw)}
	}
	n := 0
	for _, ch := range raw {
		n = n*10 + int(ch-'0')
	}
	if n < 1 || n > 50 {
		return 0, &ConfigPanelBadRequest{Message: fmt.Sprintf("%s 需为 1-50 的正整数，当前：%d", name, n)}
	}
	return n, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// envLine 生成 .env 行（Python: f'{env_key}="{value}"\n' if value else f'{env_key}=\n'）。
func envLine(envKey, value string) string {
	if value == "" {
		return envKey + "="
	}
	return envKey + `="` + value + `"`
}

// writeYAMLPath 读改写 config.yaml 的嵌套路径（Python _set_config_path 语义）。
func writeYAMLPath(path []string, value any) bool {
	return mutateYAMLFile(func(root map[string]any) {
		SetNestedConfigValue(root, path, value)
	})
}

// deleteYAMLPath 删除 config.yaml 嵌套键（Python update_swarmflow_budget 的
// none/null 清除语义）。
func deleteYAMLPath(path []string) bool {
	return mutateYAMLFile(func(root map[string]any) {
		parent := GetNestedConfigValue(root, path[:len(path)-1], nil)
		if m, ok := parent.(map[string]any); ok {
			delete(m, path[len(path)-1])
		}
	})
}

// dispatchModeText 调度模式文案（Python: "scheduled" if enabled else "autonomous"）。
func dispatchModeText(enabled bool) string {
	if enabled {
		return "scheduled"
	}
	return "autonomous"
}

// teamDispatchScheduled 计算调度模式现值（Python :1605-1612：params 带开关用
// params，否则用 raw 展平值）。
func teamDispatchScheduled(raw map[string]any, params map[string]any) bool {
	if v, ok := params["team_scheduled_dispatch_enabled"]; ok {
		return ParseConfigBool(v)
	}
	enabled := GetNestedConfigValue(raw, []string{"modes", "team", "jiuwen_team", "dispatch_mode"}, "autonomous")
	return strings.TrimSpace(fmt.Sprintf("%v", enabled)) == "scheduled"
}

// subtaskDepth 子任务深度值（Python coerce_subtask_depth：开→max(1,val)，关→0）。
func subtaskDepth(enabled bool, val any) int {
	if !enabled {
		return 0
	}
	n, ok := coerceInt(val)
	if !ok || n < 1 {
		return 1
	}
	return n
}

// intersectBool 键集相交。
func intersectBool(a map[string]bool, b map[string]bool) bool {
	for k := range a {
		if b[k] {
			return true
		}
	}
	return false
}

// containsAny 前缀族命中。
func containsAny(key string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// sortedKeysStr 排序 map 键。
func sortedKeysStr(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
