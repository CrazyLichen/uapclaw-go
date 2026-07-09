package gateway

import "os"

// ──────────────────────────── 全局变量 ────────────────────────────

// configSetEnvMap 配置键到环境变量的映射。
// 对齐 Python: _CONFIG_SET_ENV_MAP (app_web_handlers.py L310-367)
// 启动时和热重载时，收集这些环境变量的当前值传给 AgentServer。
var configSetEnvMap = map[string]string{
	// 默认模型（主对话）
	"model_provider": "MODEL_PROVIDER",
	"model":          "MODEL_NAME",
	"api_base":       "API_BASE",
	"api_key":        "API_KEY",
	// video 模型
	"video_api_base": "VIDEO_API_BASE",
	"video_api_key":  "VIDEO_API_KEY",
	"video_model":    "VIDEO_MODEL_NAME",
	"video_provider": "VIDEO_PROVIDER",
	// audio 模型
	"audio_api_base": "AUDIO_API_BASE",
	"audio_api_key":  "AUDIO_API_KEY",
	"audio_model":    "AUDIO_MODEL_NAME",
	"audio_provider": "AUDIO_PROVIDER",
	// vision 模型
	"vision_api_base": "VISION_API_BASE",
	"vision_api_key":  "VISION_API_KEY",
	"vision_model":    "VISION_MODEL_NAME",
	"vision_provider": "VISION_PROVIDER",
	// 其他
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
	"evolution_auto_scan":               "EVOLUTION_AUTO_SCAN",
	"skill_create":                      "SKILL_CREATE",
	"teamskills_market_url":             "TEAM_SKILLS_HUB_BASE_URL",
	"teamskills_user_token":             "TEAM_SKILLS_HUB_USER_TOKEN",
	"teamskills_system_token":           "TEAM_SKILLS_HUB_SYSTEM_TOKEN",
	"teamskills_allowed_download_hosts": "TEAM_SKILLS_HUB_ALLOWED_DOWNLOAD_HOSTS",
	"free_search_ddg_enabled":           "FREE_SEARCH_DDG_ENABLED",
	"free_search_bing_enabled":          "FREE_SEARCH_BING_ENABLED",
	"free_search_proxy_url":             "FREE_SEARCH_PROXY_URL",
	// agents
	"skills":             "SKILLS",
	"max_iterations":     "MAX_ITERATIONS",
	"completion_timeout": "COMPLETION_TIMEOUT",
	// team
	"team_name":     "TEAM_NAME",
	"lifecycle":     "LIFECYCLE",
	"teammate_mode": "TEAMATE_MODE",
	"spawn_mode":    "SPAWN_MODE",
	"member_name":   "MEMBER_NAME",
	"display_name":  "DISPLAY_NAME",
	"persona":       "PERSONA",
	"agent_key":     "AGENT_KEY",
	"role_type":     "ROLE_TYPE",
	"prompt_hint":   "PROMPT_HINT",
}

// browserRuntimeKeys 触发 browser.runtime_restart 的环境变量集合。
// 对齐 Python: browser_runtime_keys (app_gateway.py L920-935)。
var browserRuntimeKeys = map[string]bool{
	"MODEL_PROVIDER":    true,
	"MODEL_NAME":        true,
	"API_BASE":          true,
	"API_KEY":           true,
	"VIDEO_PROVIDER":    true,
	"VIDEO_MODEL_NAME":  true,
	"VIDEO_API_BASE":    true,
	"VIDEO_API_KEY":     true,
	"AUDIO_PROVIDER":    true,
	"AUDIO_MODEL_NAME":  true,
	"AUDIO_API_BASE":    true,
	"AUDIO_API_KEY":     true,
	"VISION_PROVIDER":   true,
	"VISION_MODEL_NAME": true,
	"VISION_API_BASE":   true,
	"VISION_API_KEY":    true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildEnvMap 收集 configSetEnvMap 中各环境变量的当前值。
// 对齐 Python: {env_key: os.getenv(env_key) for env_key in _CONFIG_SET_ENV_MAP.values()}
func BuildEnvMap() map[string]any {
	env := make(map[string]any, len(configSetEnvMap))
	for _, envKey := range configSetEnvMap {
		env[envKey] = os.Getenv(envKey)
	}
	return env
}

// ShouldBrowserRestart 判断变更的环境变量是否需要触发 browser.runtime_restart。
// 对齐 Python: browser_runtime_keys & set(updated_env_keys)。
func ShouldBrowserRestart(updatedKeys []string) bool {
	for _, k := range updatedKeys {
		if browserRuntimeKeys[k] {
			return true
		}
	}
	return false
}
