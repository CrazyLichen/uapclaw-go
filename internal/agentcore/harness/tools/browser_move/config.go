package browser_move

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BrowserRunGuardrails 浏览器运行守护护栏配置。
//
// 对齐 Python: openjiuwen/harness/tools/browser_move/playwright_runtime/config.py (BrowserRunGuardrails)
type BrowserRunGuardrails struct {
	// MaxSteps 最大步数
	MaxSteps int
	// MaxFailures 最大失败数
	MaxFailures int
	// TimeoutS 超时时间（秒）
	TimeoutS int
	// RetryOnce 是否重试一次
	RetryOnce bool
	// ResumeOnMaxIterations 达到最大迭代次数后是否继续
	ResumeOnMaxIterations bool
}

// RuntimeSettings 运行时配置。
//
// 对齐 Python: openjiuwen/harness/tools/browser_move/playwright_runtime/config.py (RuntimeSettings)
type RuntimeSettings struct {
	// Provider 模型提供者
	Provider string
	// APIKey API 密钥
	APIKey string
	// APIBase API 基础 URL
	APIBase string
	// ModelName 模型名称
	ModelName string
	// MCPCfg MCP 服务器配置
	MCPCfg *mcptypes.McpServerConfig
	// Guardrails 浏览器运行守护护栏
	Guardrails *BrowserRunGuardrails
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildBrowserGuardrails 构建浏览器运行守护护栏配置。
//
// 对齐 Python: build_browser_guardrails()
func BuildBrowserGuardrails() *BrowserRunGuardrails {
	minOne := 1
	minZero := 0
	return &BrowserRunGuardrails{
		MaxSteps:              ResolveIntEnv([]string{"BROWSER_GUARDRAIL_MAX_STEPS"}, DefaultGuardrailMaxSteps, &minOne),
		MaxFailures:           ResolveIntEnv([]string{"BROWSER_GUARDRAIL_MAX_FAILURES"}, DefaultGuardrailMaxFailures, &minZero),
		TimeoutS:              ResolveBrowserTimeoutS(),
		RetryOnce:             ResolveBoolEnv([]string{"BROWSER_GUARDRAIL_RETRY_ONCE"}, DefaultGuardrailRetryOnce),
		ResumeOnMaxIterations: ResolveBoolEnv([]string{"BROWSER_GUARDRAIL_RESUME_ON_MAX_ITERATIONS"}, false),
	}
}

// BuildPlaywrightMCPConfig 构建 Playwright MCP 服务器配置。
//
// 对齐 Python: build_playwright_mcp_config()
func BuildPlaywrightMCPConfig() *mcptypes.McpServerConfig {
	command := strings.TrimSpace(os.Getenv("PLAYWRIGHT_MCP_COMMAND"))
	if command == "" {
		command = DefaultPlaywrightMCPCommand
	}

	args := ParseCommandArgs(FirstNonEmptyEnv("PLAYWRIGHT_MCP_ARGS"))
	if len(args) == 0 {
		args = ParseCommandArgs(DefaultPlaywrightMCPArgs)
	}

	cwd := ResolvePlaywrightMCPCwd()

	driverMode := strings.ToLower(strings.TrimSpace(os.Getenv("BROWSER_DRIVER")))
	extensionMode := driverMode == "extension" || IsTruthyEnv(os.Getenv("PLAYWRIGHT_MCP_EXTENSION"))

	minOne := 1
	timeoutS := ResolveIntEnv(
		[]string{"PLAYWRIGHT_MCP_TIMEOUT_S", "BROWSER_TIMEOUT_S"},
		DefaultBrowserTimeoutS,
		&minOne,
	)

	// 收集环境变量映射
	envMap := map[string]string{}
	for _, key := range []string{
		"PLAYWRIGHT_BROWSERS_PATH",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
	} {
		if value := os.Getenv(key); value != "" {
			envMap[key] = value
		}
	}

	// 解析额外环境变量 JSON
	extraEnvJSON := strings.TrimSpace(os.Getenv("PLAYWRIGHT_MCP_ENV_JSON"))
	if extraEnvJSON != "" {
		var extra map[string]any
		if err := json.Unmarshal([]byte(extraEnvJSON), &extra); err != nil {
			panic(fmt.Sprintf("Invalid PLAYWRIGHT_MCP_ENV_JSON: %v", err))
		}
		for k, v := range extra {
			envMap[k] = fmt.Sprintf("%v", v)
		}
	}

	if extensionMode {
		// extension 模式
		envMap["PLAYWRIGHT_MCP_EXTENSION"] = "true"
		extensionToken := FirstNonEmptyEnv("PLAYWRIGHT_MCP_EXTENSION_TOKEN")
		if extensionToken != "" {
			envMap["PLAYWRIGHT_MCP_EXTENSION_TOKEN"] = extensionToken
		}
		hasExtensionFlag := false
		for _, arg := range args {
			if arg == "--extension" {
				hasExtensionFlag = true
				break
			}
		}
		if !hasExtensionFlag {
			args = append(args, "--extension")
		}
	} else {
		// CDP 模式
		cdpEndpoint := FirstNonEmptyEnv("PLAYWRIGHT_MCP_CDP_ENDPOINT", "PLAYWRIGHT_CDP_URL")
		cdpHeaders := FirstNonEmptyEnv("PLAYWRIGHT_MCP_CDP_HEADERS", "PLAYWRIGHT_CDP_HEADERS")
		cdpTimeout := FirstNonEmptyEnv("PLAYWRIGHT_MCP_CDP_TIMEOUT", "PLAYWRIGHT_CDP_TIMEOUT_MS")
		browserName := FirstNonEmptyEnv("PLAYWRIGHT_MCP_BROWSER")
		deviceName := FirstNonEmptyEnv("PLAYWRIGHT_MCP_DEVICE")

		if cdpEndpoint != "" {
			if deviceName != "" {
				panic("PLAYWRIGHT_MCP_DEVICE is not supported with CDP endpoint mode.")
			}
			envMap["PLAYWRIGHT_MCP_CDP_ENDPOINT"] = cdpEndpoint
			if browserName == "" {
				// CDP 模式仅支持 Chromium
				envMap["PLAYWRIGHT_MCP_BROWSER"] = "chrome"
			}
		}
		if cdpHeaders != "" {
			envMap["PLAYWRIGHT_MCP_CDP_HEADERS"] = cdpHeaders
		}
		if cdpTimeout != "" {
			envMap["PLAYWRIGHT_MCP_CDP_TIMEOUT"] = cdpTimeout
		}
	}

	// 构建 params
	params := map[string]any{
		"command": command,
		"args":    args,
		"cwd":     cwd,
	}
	if timeoutS > 0 {
		params["timeout_s"] = timeoutS
	}
	if len(envMap) > 0 {
		params["env"] = envMap
	}

	return mcptypes.NewMcpServerConfig(
		"playwright-official",
		"stdio://playwright",
		"stdio",
		mcptypes.WithServerID("playwright_official_stdio"),
		mcptypes.WithParams(params),
	)
}

// BuildRuntimeSettings 构建运行时配置。
//
// 对齐 Python: build_runtime_settings()
func BuildRuntimeSettings() *RuntimeSettings {
	provider, apiKey, apiBase := ResolveModelSettings()
	return &RuntimeSettings{
		Provider:   provider,
		APIKey:     apiKey,
		APIBase:    apiBase,
		ModelName:  ResolveModelName(),
		MCPCfg:     BuildPlaywrightMCPConfig(),
		Guardrails: BuildBrowserGuardrails(),
	}
}

// ResolveRuntimeSettings 解析运行时配置，优先使用传入的 settings，
// 否则从 Model 的 ModelClientConfig/ModelConfig 推导。
//
// 对齐 Python: _resolve_runtime_settings(model, settings)
func ResolveRuntimeSettings(model *llm.Model, settings *RuntimeSettings) *RuntimeSettings {
	if settings != nil {
		return settings
	}
	if model != nil && model.ClientConfig != nil {
		cc := model.ClientConfig
		requestModelName := ""
		if model.ModelConfig != nil {
			requestModelName = model.ModelConfig.ModelName
		}
		return &RuntimeSettings{
			Provider:   cc.ClientProvider,
			APIKey:     cc.APIKey,
			APIBase:    cc.APIBase,
			ModelName:  requestModelName,
			MCPCfg:     BuildPlaywrightMCPConfig(),
			Guardrails: BuildBrowserGuardrails(),
		}
	}
	return BuildRuntimeSettings()
}

// ResolvePlaywrightMCPCwd 解析 MCP 工作目录，支持重定位默认值。
//
// 对齐 Python: resolve_playwright_mcp_cwd()
func ResolvePlaywrightMCPCwd() string {
	configured := FirstNonEmptyEnv(
		"PLAYWRIGHT_RUNTIME_MCP_CWD",
		"BROWSER_RUNTIME_MCP_CWD",
		"PLAYWRIGHT_RUNTIME_WORKDIR",
		"BROWSER_RUNTIME_WORKDIR",
	)
	if configured != "" {
		expanded, err := filepath.Abs(filepath.Clean(configured))
		if err != nil {
			return configured
		}
		return expanded
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

// ──────────────────────────── 非导出函数 ────────────────────────────
