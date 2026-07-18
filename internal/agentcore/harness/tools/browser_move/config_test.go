package browser_move

import (
	"os"
	"testing"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// helper: 清理环境变量
func unsetEnvKeys(keys ...string) {
	for _, k := range keys {
		os.Unsetenv(k)
	}
}

// ──────────────────────────── BuildBrowserGuardrails 测试 ────────────────────────────

func TestBuildBrowserGuardrails_默认值(t *testing.T) {
	// 清除可能影响的环境变量
	unsetEnvKeys(
		"BROWSER_GUARDRAIL_MAX_STEPS",
		"BROWSER_GUARDRAIL_MAX_FAILURES",
		"BROWSER_GUARDRAIL_RETRY_ONCE",
		"BROWSER_GUARDRAIL_RESUME_ON_MAX_ITERATIONS",
		"BROWSER_TIMEOUT_S",
		"PLAYWRIGHT_TOOL_TIMEOUT_S",
	)

	g := BuildBrowserGuardrails()
	if g.MaxSteps != DefaultGuardrailMaxSteps {
		t.Errorf("MaxSteps 期望 %d, 得到 %d", DefaultGuardrailMaxSteps, g.MaxSteps)
	}
	if g.MaxFailures != DefaultGuardrailMaxFailures {
		t.Errorf("MaxFailures 期望 %d, 得到 %d", DefaultGuardrailMaxFailures, g.MaxFailures)
	}
	if g.TimeoutS != DefaultBrowserTimeoutS {
		t.Errorf("TimeoutS 期望 %d, 得到 %d", DefaultBrowserTimeoutS, g.TimeoutS)
	}
	if g.RetryOnce != DefaultGuardrailRetryOnce {
		t.Errorf("RetryOnce 期望 %v, 得到 %v", DefaultGuardrailRetryOnce, g.RetryOnce)
	}
	if g.ResumeOnMaxIterations != false {
		t.Errorf("ResumeOnMaxIterations 期望 false, 得到 %v", g.ResumeOnMaxIterations)
	}
}

func TestBuildBrowserGuardrails_环境变量覆盖(t *testing.T) {
	os.Setenv("BROWSER_GUARDRAIL_MAX_STEPS", "30")
	os.Setenv("BROWSER_GUARDRAIL_MAX_FAILURES", "5")
	os.Setenv("BROWSER_GUARDRAIL_RETRY_ONCE", "false")
	os.Setenv("BROWSER_GUARDRAIL_RESUME_ON_MAX_ITERATIONS", "true")
	os.Setenv("BROWSER_TIMEOUT_S", "300")
	defer unsetEnvKeys(
		"BROWSER_GUARDRAIL_MAX_STEPS",
		"BROWSER_GUARDRAIL_MAX_FAILURES",
		"BROWSER_GUARDRAIL_RETRY_ONCE",
		"BROWSER_GUARDRAIL_RESUME_ON_MAX_ITERATIONS",
		"BROWSER_TIMEOUT_S",
	)

	g := BuildBrowserGuardrails()
	if g.MaxSteps != 30 {
		t.Errorf("MaxSteps 期望 30, 得到 %d", g.MaxSteps)
	}
	if g.MaxFailures != 5 {
		t.Errorf("MaxFailures 期望 5, 得到 %d", g.MaxFailures)
	}
	if g.TimeoutS != 300 {
		t.Errorf("TimeoutS 期望 300, 得到 %d", g.TimeoutS)
	}
	if g.RetryOnce != false {
		t.Errorf("RetryOnce 期望 false, 得到 %v", g.RetryOnce)
	}
	if g.ResumeOnMaxIterations != true {
		t.Errorf("ResumeOnMaxIterations 期望 true, 得到 %v", g.ResumeOnMaxIterations)
	}
}

// ──────────────────────────── BuildPlaywrightMCPConfig 测试 ────────────────────────────

func TestBuildPlaywrightMCPConfig_默认值(t *testing.T) {
	// 清除可能干扰的环境变量
	unsetEnvKeys(
		"PLAYWRIGHT_MCP_COMMAND",
		"PLAYWRIGHT_MCP_ARGS",
		"PLAYWRIGHT_MCP_TIMEOUT_S",
		"BROWSER_TIMEOUT_S",
		"BROWSER_DRIVER",
		"PLAYWRIGHT_MCP_EXTENSION",
		"PLAYWRIGHT_MCP_CDP_ENDPOINT",
		"PLAYWRIGHT_CDP_URL",
		"PLAYWRIGHT_MCP_CDP_HEADERS",
		"PLAYWRIGHT_CDP_HEADERS",
		"PLAYWRIGHT_MCP_CDP_TIMEOUT",
		"PLAYWRIGHT_CDP_TIMEOUT_MS",
		"PLAYWRIGHT_MCP_BROWSER",
		"PLAYWRIGHT_MCP_DEVICE",
		"PLAYWRIGHT_MCP_ENV_JSON",
		"PLAYWRIGHT_MCP_EXTENSION_TOKEN",
		"PLAYWRIGHT_RUNTIME_MCP_CWD",
		"BROWSER_RUNTIME_MCP_CWD",
		"PLAYWRIGHT_RUNTIME_WORKDIR",
		"BROWSER_RUNTIME_WORKDIR",
	)

	cfg := BuildPlaywrightMCPConfig()
	if cfg.ServerID != "playwright_official_stdio" {
		t.Errorf("ServerID 期望 playwright_official_stdio, 得到 %s", cfg.ServerID)
	}
	if cfg.ServerName != "playwright-official" {
		t.Errorf("ServerName 期望 playwright-official, 得到 %s", cfg.ServerName)
	}
	if cfg.ServerPath != "stdio://playwright" {
		t.Errorf("ServerPath 期望 stdio://playwright, 得到 %s", cfg.ServerPath)
	}
	if cfg.ClientType != "stdio" {
		t.Errorf("ClientType 期望 stdio, 得到 %s", cfg.ClientType)
	}

	params := cfg.Params
	if params == nil {
		t.Fatal("Params 不应为 nil")
	}
	if params["command"] != DefaultPlaywrightMCPCommand {
		t.Errorf("command 期望 %s, 得到 %v", DefaultPlaywrightMCPCommand, params["command"])
	}
	args, ok := params["args"].([]string)
	if !ok {
		t.Fatalf("args 类型不正确: %T", params["args"])
	}
	if len(args) == 0 || args[0] != "-y" {
		t.Errorf("args 期望 ['-y', '@playwright/mcp@latest'], 得到 %v", args)
	}
	if timeoutS, ok := params["timeout_s"]; ok {
		if timeoutS != DefaultBrowserTimeoutS {
			t.Errorf("timeout_s 期望 %d, 得到 %v", DefaultBrowserTimeoutS, timeoutS)
		}
	}
}

func TestBuildPlaywrightMCPConfig_extension模式(t *testing.T) {
	os.Setenv("BROWSER_DRIVER", "extension")
	os.Setenv("PLAYWRIGHT_MCP_EXTENSION_TOKEN", "test-token")
	defer unsetEnvKeys("BROWSER_DRIVER", "PLAYWRIGHT_MCP_EXTENSION_TOKEN")

	cfg := BuildPlaywrightMCPConfig()
	params := cfg.Params
	envMap, ok := params["env"].(map[string]string)
	if !ok {
		t.Fatalf("env 类型不正确: %T", params["env"])
	}
	if envMap["PLAYWRIGHT_MCP_EXTENSION"] != "true" {
		t.Errorf("期望 PLAYWRIGHT_MCP_EXTENSION=true, 得到 %s", envMap["PLAYWRIGHT_MCP_EXTENSION"])
	}
	if envMap["PLAYWRIGHT_MCP_EXTENSION_TOKEN"] != "test-token" {
		t.Errorf("期望 PLAYWRIGHT_MCP_EXTENSION_TOKEN=test-token, 得到 %s", envMap["PLAYWRIGHT_MCP_EXTENSION_TOKEN"])
	}
	args, _ := params["args"].([]string)
	hasExtension := false
	for _, arg := range args {
		if arg == "--extension" {
			hasExtension = true
			break
		}
	}
	if !hasExtension {
		t.Error("extension 模式下 args 应包含 --extension")
	}
}

func TestBuildPlaywrightMCPConfig_CDP模式(t *testing.T) {
	os.Setenv("PLAYWRIGHT_MCP_CDP_ENDPOINT", "ws://localhost:9222")
	defer unsetEnvKeys("PLAYWRIGHT_MCP_CDP_ENDPOINT")

	cfg := BuildPlaywrightMCPConfig()
	params := cfg.Params
	envMap, ok := params["env"].(map[string]string)
	if !ok {
		t.Fatalf("env 类型不正确: %T", params["env"])
	}
	if envMap["PLAYWRIGHT_MCP_CDP_ENDPOINT"] != "ws://localhost:9222" {
		t.Errorf("期望 PLAYWRIGHT_MCP_CDP_ENDPOINT=ws://localhost:9222, 得到 %s", envMap["PLAYWRIGHT_MCP_CDP_ENDPOINT"])
	}
	// CDP 模式下未指定 browser 时默认 chrome
	if envMap["PLAYWRIGHT_MCP_BROWSER"] != "chrome" {
		t.Errorf("期望 PLAYWRIGHT_MCP_BROWSER=chrome, 得到 %s", envMap["PLAYWRIGHT_MCP_BROWSER"])
	}
}

// ──────────────────────────── ResolvePlaywrightMCPCwd 测试 ────────────────────────────

func TestResolvePlaywrightMCPCwd_环境变量设置(t *testing.T) {
	os.Setenv("PLAYWRIGHT_RUNTIME_MCP_CWD", "/tmp/test_cwd")
	defer unsetEnvKeys("PLAYWRIGHT_RUNTIME_MCP_CWD")

	cwd := ResolvePlaywrightMCPCwd()
	if cwd != "/tmp/test_cwd" {
		t.Errorf("期望 /tmp/test_cwd, 得到 %s", cwd)
	}
}

func TestResolvePlaywrightMCPCwd_回退到当前目录(t *testing.T) {
	unsetEnvKeys(
		"PLAYWRIGHT_RUNTIME_MCP_CWD",
		"BROWSER_RUNTIME_MCP_CWD",
		"PLAYWRIGHT_RUNTIME_WORKDIR",
		"BROWSER_RUNTIME_WORKDIR",
	)

	cwd := ResolvePlaywrightMCPCwd()
	if cwd == "" {
		t.Error("期望非空工作目录, 得到空字符串")
	}
}

// ──────────────────────────── BuildRuntimeSettings 测试 ────────────────────────────

func TestBuildRuntimeSettings(t *testing.T) {
	// 清除可能干扰的环境变量
	unsetEnvKeys(
		"MODEL_PROVIDER",
		"MODEL_CLIENT_PROVIDER",
		"API_KEY",
		"MODEL_API_KEY",
		"OPENAI_API_KEY",
		"OPENROUTER_API_KEY",
		"API_BASE",
		"MODEL_API_BASE",
		"MODEL_NAME",
		"OPENAI_BASE_URL",
		"OPENAI_API_BASE",
	)

	// 设置最小环境变量以便测试
	os.Setenv("OPENAI_API_KEY", "test-key")
	defer unsetEnvKeys("OPENAI_API_KEY")

	s := BuildRuntimeSettings()
	if s == nil {
		t.Fatal("BuildRuntimeSettings 不应返回 nil")
	}
	if s.MCPCfg == nil {
		t.Error("MCPCfg 不应为 nil")
	}
	if s.Guardrails == nil {
		t.Error("Guardrails 不应为 nil")
	}
}

// ──────────────────────────── ResolveRuntimeSettings 测试 ────────────────────────────

func TestResolveRuntimeSettings_传入settings优先(t *testing.T) {
	settings := &RuntimeSettings{
		Provider:  "openrouter",
		APIKey:    "or-key",
		APIBase:   "https://openrouter.ai/api/v1",
		ModelName: "test-model",
		MCPCfg:    mcptypes.NewMcpServerConfig("test", "stdio://test", "stdio"),
		Guardrails: &BrowserRunGuardrails{
			MaxSteps:              10,
			MaxFailures:           1,
			TimeoutS:              60,
			RetryOnce:             true,
			ResumeOnMaxIterations: false,
		},
	}

	result := ResolveRuntimeSettings(nil, settings)
	if result != settings {
		t.Error("传入 settings 时应直接返回该 settings")
	}
	if result.Provider != "openrouter" {
		t.Errorf("Provider 期望 openrouter, 得到 %s", result.Provider)
	}
}

func TestResolveRuntimeSettings_从Model推导(t *testing.T) {
	unsetEnvKeys(
		"MODEL_PROVIDER",
		"MODEL_CLIENT_PROVIDER",
		"API_KEY",
		"MODEL_API_KEY",
		"OPENAI_API_KEY",
		"OPENROUTER_API_KEY",
		"API_BASE",
		"MODEL_API_BASE",
		"MODEL_NAME",
	)

	// 注意：NewModelClientConfig.Validate() 会将 provider 标准化为枚举形式
	// "openrouter" → "OpenRouter"，所以断言时用标准化后的值
	// 使用已注册的 provider 名称 "OpenRouter"（首字母大写）
	model, err := llm.NewModel(
		llmschema.NewModelClientConfig("OpenRouter", "or-key", "https://openrouter.ai/api/v1"),
		llmschema.NewModelRequestConfig(llmschema.WithModelName("qwen-max")),
	)
	if err != nil {
		t.Fatalf("NewModel 失败: %v", err)
	}

	result := ResolveRuntimeSettings(model, nil)
	if result.Provider != "OpenRouter" {
		t.Errorf("Provider 期望 OpenRouter, 得到 %s", result.Provider)
	}
	if result.APIKey != "or-key" {
		t.Errorf("APIKey 期望 or-key, 得到 %s", result.APIKey)
	}
	if result.APIBase != "https://openrouter.ai/api/v1" {
		t.Errorf("APIBase 期望 https://openrouter.ai/api/v1, 得到 %s", result.APIBase)
	}
	if result.ModelName != "qwen-max" {
		t.Errorf("ModelName 期望 qwen-max, 得到 %s", result.ModelName)
	}
	if result.MCPCfg == nil {
		t.Error("MCPCfg 不应为 nil")
	}
	if result.Guardrails == nil {
		t.Error("Guardrails 不应为 nil")
	}
}

func TestResolveRuntimeSettings_Model为nil时回退(t *testing.T) {
	unsetEnvKeys(
		"MODEL_PROVIDER",
		"MODEL_CLIENT_PROVIDER",
		"API_KEY",
		"MODEL_API_KEY",
		"OPENAI_API_KEY",
		"OPENROUTER_API_KEY",
		"API_BASE",
		"MODEL_API_BASE",
		"MODEL_NAME",
	)

	os.Setenv("OPENAI_API_KEY", "fallback-key")
	defer unsetEnvKeys("OPENAI_API_KEY")

	result := ResolveRuntimeSettings(nil, nil)
	if result == nil {
		t.Fatal("ResolveRuntimeSettings 不应返回 nil")
	}
	if result.Provider != "openai" {
		t.Errorf("Provider 期望 openai, 得到 %s", result.Provider)
	}
}
