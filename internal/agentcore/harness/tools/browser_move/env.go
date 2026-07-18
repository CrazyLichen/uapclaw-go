package browser_move

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultModelName 默认模型名称
	// 对齐 Python: DEFAULT_MODEL_NAME
	DefaultModelName = "anthropic/claude-sonnet-4.5"
	// DefaultBrowserTimeoutS 默认浏览器超时（秒）
	// 对齐 Python: DEFAULT_BROWSER_TIMEOUT_S
	DefaultBrowserTimeoutS = 180
	// DefaultGuardrailMaxSteps 默认守护护栏最大步数
	// 对齐 Python: DEFAULT_GUARDRAIL_MAX_STEPS
	DefaultGuardrailMaxSteps = 20
	// DefaultGuardrailMaxFailures 默认守护护栏最大失败数
	// 对齐 Python: DEFAULT_GUARDRAIL_MAX_FAILURES
	DefaultGuardrailMaxFailures = 2
	// DefaultPlaywrightMCPCommand 默认 Playwright MCP 命令
	// 对齐 Python: DEFAULT_PLAYWRIGHT_MCP_COMMAND
	DefaultPlaywrightMCPCommand = "npx"
	// DefaultPlaywrightMCPArgs 默认 Playwright MCP 参数
	// 对齐 Python: DEFAULT_PLAYWRIGHT_MCP_ARGS
	DefaultPlaywrightMCPArgs = "-y @playwright/mcp@latest"
	// MissingAPIKeyMessage 缺少 API Key 的提示信息
	// 对齐 Python: MISSING_API_KEY_MESSAGE
	MissingAPIKeyMessage = "Missing API key. Set API_KEY (or OPENROUTER_API_KEY / SILICONFLOW_API_KEY / OPENAI_API_KEY / DASHSCOPE_API_KEY)."
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// supportedModelProviders 支持的模型提供者
	// 对齐 Python: SUPPORTED_MODEL_PROVIDERS
	supportedModelProviders = map[string]bool{
		"openai":      true,
		"openrouter":  true,
		"siliconflow": true,
		"dashscope":   true,
	}
	// truthyEnvValues 真值环境变量值
	// 对齐 Python: TRUTHY_ENV_VALUES
	truthyEnvValues = map[string]bool{
		"1": true, "true": true, "yes": true, "on": true,
	}
	// falsyEnvValues 假值环境变量值
	// 对齐 Python: FALSY_ENV_VALUES
	falsyEnvValues = map[string]bool{
		"0": true, "false": true, "no": true, "off": true,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FirstNonEmptyEnv 返回第一个非空的环境变量值。
// 对齐 Python: first_non_empty_env(*keys)
func FirstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}
	return ""
}

// NormalizeProvider 标准化模型提供者名称。
// 对齐 Python: normalize_provider(provider)
func NormalizeProvider(provider string) string {
	raw := strings.TrimSpace(provider)
	lowered := strings.ToLower(raw)
	if supportedModelProviders[lowered] {
		return lowered
	}
	switch lowered {
	case "alibaba", "aliyun":
		return "dashscope"
	case "silicon-flow", "silicon_flow":
		return "siliconflow"
	}
	return raw
}

// IsTruthyEnv 判断环境变量值是否为真。
// 对齐 Python: is_truthy_env(value)
func IsTruthyEnv(value string) bool {
	lowered := strings.TrimSpace(strings.ToLower(value))
	return truthyEnvValues[lowered]
}

// IsFalsyEnv 判断环境变量值是否为假。
// 对齐 Python: is_falsy_env(value)
func IsFalsyEnv(value string) bool {
	lowered := strings.TrimSpace(strings.ToLower(value))
	return falsyEnvValues[lowered]
}

// ResolveIntEnv 从环境变量解析整数。
// 对齐 Python: resolve_int_env(*keys, default=..., minimum=...)
func ResolveIntEnv(keys []string, defaultVal int, minimum *int) int {
	for _, key := range keys {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			continue
		}
		if minimum != nil && value < *minimum {
			continue
		}
		return value
	}
	return defaultVal
}

// ResolveBoolEnv 从环境变量解析布尔值。
// 对齐 Python: resolve_bool_env(*keys, default=...)
func ResolveBoolEnv(keys []string, defaultVal bool) bool {
	for _, key := range keys {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		if IsTruthyEnv(raw) {
			return true
		}
		if IsFalsyEnv(raw) {
			return false
		}
	}
	return defaultVal
}

// ResolveModelName 解析模型名称。
// 对齐 Python: resolve_model_name()
func ResolveModelName() string {
	if v := FirstNonEmptyEnv("MODEL_NAME"); v != "" {
		return v
	}
	return DefaultModelName
}

// ResolveBrowserTimeoutS 解析浏览器超时时间。
// 对齐 Python: resolve_browser_timeout_s()
func ResolveBrowserTimeoutS() int {
	min := 1
	return ResolveIntEnv([]string{"BROWSER_TIMEOUT_S", "PLAYWRIGHT_TOOL_TIMEOUT_S"}, DefaultBrowserTimeoutS, &min)
}

// InferProviderFromAPIBase 从 API Base URL 推断 provider。
// 对齐 Python: infer_provider_from_api_base(api_base)
func InferProviderFromAPIBase(apiBase string) string {
	base := strings.TrimSpace(strings.ToLower(apiBase))
	if base == "" {
		return ""
	}
	if strings.Contains(base, "openrouter.ai") {
		return "openrouter"
	}
	if strings.Contains(base, "siliconflow") {
		return "siliconflow"
	}
	if strings.Contains(base, "dashscope") {
		return "dashscope"
	}
	return "openai"
}

// ParseCommandArgs 解析命令行参数字符串。
// 对齐 Python: parse_command_args(value)
func ParseCommandArgs(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	// 尝试 JSON 数组解析
	if strings.HasPrefix(value, "[") {
		var parsed []string
		if err := json.Unmarshal([]byte(value), &parsed); err == nil {
			return parsed
		}
	}
	// 简单空格分割（对齐 Python shlex.split 简化版）
	return splitArgs(value)
}

// ResolveModelSettings 解析模型配置（provider, api_key, api_base）。
// 对齐 Python: resolve_model_settings()
func ResolveModelSettings() (provider, apiKey, apiBase string) {
	providerMode := NormalizeProvider(FirstNonEmptyEnv("MODEL_PROVIDER", "MODEL_CLIENT_PROVIDER"))
	if providerMode != "" && !supportedModelProviders[providerMode] {
		// Python 侧抛出 ValueError，Go 侧降级为默认 openai
		providerMode = ""
	}

	explicitAPIBase := FirstNonEmptyEnv("API_BASE", "MODEL_API_BASE")

	if providerMode != "" {
		provider = providerMode
	} else {
		baseHint := explicitAPIBase
		if baseHint == "" {
			baseHint = FirstNonEmptyEnv(
				"OPENROUTER_BASE_URL", "OPENROUTER_API_BASE",
				"SILICONFLOW_BASE_URL", "SILICONFLOW_API_BASE",
				"DASHSCOPE_BASE_URL", "DASHSCOPE_API_BASE",
				"OPENAI_BASE_URL", "OPENAI_API_BASE",
			)
		}
		provider = InferProviderFromAPIBase(baseHint)
		if provider == "" {
			hasOpenRouterKey := FirstNonEmptyEnv("OPENROUTER_API_KEY") != ""
			hasSiliconFlowKey := FirstNonEmptyEnv("SILICONFLOW_API_KEY") != ""
			hasDashScopeKey := FirstNonEmptyEnv("DASHSCOPE_API_KEY") != ""
			switch {
			case hasOpenRouterKey:
				provider = "openrouter"
			case hasSiliconFlowKey:
				provider = "siliconflow"
			case hasDashScopeKey:
				provider = "dashscope"
			default:
				provider = "openai"
			}
		}
	}

	// 根据 provider 决定 key/base 的优先级链
	switch provider {
	case "openrouter":
		apiKey = FirstNonEmptyEnv("API_KEY", "MODEL_API_KEY", "OPENROUTER_API_KEY", "OPENAI_API_KEY")
		apiBase = FirstNonEmptyEnv("API_BASE", "MODEL_API_BASE", "OPENROUTER_BASE_URL", "OPENROUTER_API_BASE")
		if apiBase == "" {
			apiBase = "https://openrouter.ai/api/v1"
		}
	case "siliconflow":
		apiKey = FirstNonEmptyEnv("API_KEY", "MODEL_API_KEY", "SILICONFLOW_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY")
		apiBase = FirstNonEmptyEnv("API_BASE", "MODEL_API_BASE", "SILICONFLOW_BASE_URL", "SILICONFLOW_API_BASE")
		if apiBase == "" {
			apiBase = "https://api.siliconflow.cn/v1"
		}
	case "dashscope":
		apiKey = FirstNonEmptyEnv("API_KEY", "MODEL_API_KEY", "DASHSCOPE_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY")
		apiBase = FirstNonEmptyEnv("API_BASE", "MODEL_API_BASE", "DASHSCOPE_BASE_URL", "DASHSCOPE_API_BASE")
		if apiBase == "" {
			apiBase = "https://dashscope.aliyuncs.com/compatible-mode/v1"
		}
	default: // openai
		apiKey = FirstNonEmptyEnv("API_KEY", "MODEL_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY")
		apiBase = FirstNonEmptyEnv("API_BASE", "MODEL_API_BASE", "OPENAI_BASE_URL", "OPENAI_API_BASE")
		if apiBase == "" {
			apiBase = "https://api.openai.com/v1"
		}
	}
	return provider, apiKey, apiBase
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// splitArgs 简单命令行参数分割，支持引号
func splitArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(c)
			}
		} else {
			switch c {
			case '"', '\'':
				inQuote = true
				quoteChar = c
			case ' ', '\t':
				if current.Len() > 0 {
					args = append(args, current.String())
					current.Reset()
				}
			default:
				current.WriteByte(c)
			}
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
