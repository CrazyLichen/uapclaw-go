# 9.26 BrowserAgent 全栈实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整实现 9.26 BrowserAgent，包括配置构建、运行时内核、进度追踪 Rail、7 个运行时工具、探测系统、控制器和 Worker Agent 构建器，对齐 Python `openjiuwen/harness/subagents/browser_agent.py` 及 `openjiuwen/harness/tools/browser_move/`。

**Architecture:** 三层递进——Layer 1 配置层（env/config/parsing/browser_agent.go）→ Layer 2 运行时核心层（progress/service/runtime/browser_rail/runtime_tools/factory）→ Layer 3 探测与控制层（probes/controllers/agents/profiles/site_profiles）。其他章节依赖用 `any` 占位 + `⤵️ 9.38-49` 注释标记。

**Tech Stack:** Go, 现有 harness 包（SubAgentConfig, SubagentCreateParams, CreateDeepAgent, BaseRail, PromptSection, Tool/ToolCard/NewTool, McpServerConfig, Runner/ResourceMgr, BaseKVStore）

---

## Layer 1 — 配置层

### Task 1: 创建 browser_move 包骨架（doc.go）

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/doc.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package browser_move 提供浏览器运行时工具集，支持 Playwright MCP 浏览器自动化。
//
// 本包实现 BrowserAgent 的运行时内核、进度追踪 Rail、
// 紧凑探测工具、自定义动作控制器和 Worker Agent 构建器。
//
// 文件目录：
//
//	browser_move/
//	├── doc.go              # 包文档
//	├── env.go              # 环境变量解析工具
//	├── config.go           # RuntimeSettings + BrowserRunGuardrails + MCP 配置工厂
//	├── parsing.go          # JSON 解析工具
//	├── progress.go         # BrowserTaskProgressState 进度状态
//	├── service.go          # BrowserService 后端服务
//	├── runtime.go          # BrowserAgentRuntime 运行时内核
//	├── browser_rail.go     # BrowserRuntimeRail 进度追踪 Rail
//	├── runtime_tools.go    # 7 个运行时辅助工具
//	├── probes.go           # JavaScript 探测代码生成
//	├── controllers.go      # BaseController + ActionController
//	├── agents.go           # Worker Agent 构建器
//	├── profiles.go         # BrowserProfile + BrowserProfileStore
//	└── site_profiles.go    # 站点配置文件和选择器缓存
//
// 对应 Python 代码：openjiuwen/harness/tools/browser_move/
package browser_move
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/tools/browser_move/...`
Expected: 编译通过（空包）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/doc.go
git commit -m "feat(browser_move): 创建 browser_move 包骨架 doc.go"
```

---

### Task 2: 实现 env.go — 环境变量解析工具

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/env.go`
- Create: `internal/agentcore/harness/tools/browser_move/env_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/utils/env.py`

- [ ] **Step 1: 写 env_test.go 测试**

```go
package browser_move

import (
	"os"
	"testing"
)

func TestFirstNonEmptyEnv(t *testing.T) {
	os.Setenv("TEST_BM_KEY1", "")
	os.Setenv("TEST_BM_KEY2", "value2")
	defer os.Unsetenv("TEST_BM_KEY1")
	defer os.Unsetenv("TEST_BM_KEY2")
	result := FirstNonEmptyEnv("TEST_BM_KEY1", "TEST_BM_KEY2")
	if result != "value2" {
		t.Errorf("期望 value2, 得到 %s", result)
	}
}

func TestFirstNonEmptyEnv_全部为空(t *testing.T) {
	result := FirstNonEmptyEnv("TEST_BM_NONEXISTENT1", "TEST_BM_NONEXISTENT2")
	if result != "" {
		t.Errorf("期望空字符串, 得到 %s", result)
	}
}

func TestNormalizeProvider(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"openai", "openai"},
		{"OpenAI", "openai"},
		{"alibaba", "dashscope"},
		{"aliyun", "dashscope"},
		{"silicon-flow", "siliconflow"},
		{"silicon_flow", "siliconflow"},
		{"unknown_provider", "unknown_provider"},
	}
	for _, tt := range tests {
		result := NormalizeProvider(tt.input)
		if result != tt.expected {
			t.Errorf("NormalizeProvider(%q) = %q, 期望 %q", tt.input, result, tt.expected)
		}
	}
}

func TestIsTruthyEnv(t *testing.T) {
	if !IsTruthyEnv("1") || !IsTruthyEnv("true") || !IsTruthyEnv("yes") || !IsTruthyEnv("on") {
		t.Error("期望真值判断正确")
	}
	if IsTruthyEnv("0") || IsTruthyEnv("false") || IsTruthyEnv("") || IsTruthyEnv("no") {
		t.Error("期望假值判断正确")
	}
}

func TestResolveIntEnv(t *testing.T) {
	os.Setenv("TEST_BM_INT", "42")
	defer os.Unsetenv("TEST_BM_INT")
	min := 1
	result := ResolveIntEnv([]string{"TEST_BM_INT"}, 10, &min)
	if result != 42 {
		t.Errorf("期望 42, 得到 %d", result)
	}
}

func TestResolveIntEnv_低于最小值(t *testing.T) {
	os.Setenv("TEST_BM_INT_LOW", "0")
	defer os.Unsetenv("TEST_BM_INT_LOW")
	min := 1
	result := ResolveIntEnv([]string{"TEST_BM_INT_LOW"}, 10, &min)
	if result != 10 {
		t.Errorf("期望回退默认值 10, 得到 %d", result)
	}
}

func TestResolveBoolEnv(t *testing.T) {
	os.Setenv("TEST_BM_BOOL", "true")
	defer os.Unsetenv("TEST_BM_BOOL")
	result := ResolveBoolEnv([]string{"TEST_BM_BOOL"}, false)
	if result != true {
		t.Errorf("期望 true, 得到 %v", result)
	}
}

func TestParseCommandArgs(t *testing.T) {
	result := ParseCommandArgs("-y @playwright/mcp@latest")
	if len(result) != 2 || result[0] != "-y" || result[1] != "@playwright/mcp@latest" {
		t.Errorf("ParseCommandArgs 结果不正确: %v", result)
	}
}

func TestParseCommandArgs_JSON数组(t *testing.T) {
	result := ParseCommandArgs(`["-y", "@playwright/mcp@latest"]`)
	if len(result) != 2 || result[0] != "-y" || result[1] != "@playwright/mcp@latest" {
		t.Errorf("ParseCommandArgs JSON 结果不正确: %v", result)
	}
}

func TestInferProviderFromAPIBase(t *testing.T) {
	tests := []struct {
		apiBase  string
		expected string
	}{
		{"https://openrouter.ai/api/v1", "openrouter"},
		{"https://api.siliconflow.cn/v1", "siliconflow"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "dashscope"},
		{"https://api.openai.com/v1", "openai"},
		{"", ""},
	}
	for _, tt := range tests {
		result := InferProviderFromAPIBase(tt.apiBase)
		if result != tt.expected {
			t.Errorf("InferProviderFromAPIBase(%q) = %q, 期望 %q", tt.apiBase, result, tt.expected)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/browser_move/... -run "TestFirstNonEmptyEnv|TestNormalizeProvider|TestIsTruthyEnv|TestResolveIntEnv|TestResolveBoolEnv|TestParseCommandArgs|TestInferProviderFromAPIBase" -v`
Expected: 编译失败（函数未定义）

- [ ] **Step 3: 写 env.go 实现**

```go
package browser_move

import (
	"encoding/json"
	"os"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// 对齐 Python: env.py 中的常量定义
const (
	// DefaultModelName 默认模型名称
	DefaultModelName = "anthropic/claude-sonnet-4.5"
	// DefaultBrowserTimeoutS 默认浏览器超时（秒）
	DefaultBrowserTimeoutS = 180
	// DefaultGuardrailMaxSteps 默认守护护栏最大步数
	DefaultGuardrailMaxSteps = 20
	// DefaultGuardrailMaxFailures 默认守护护栏最大失败数
	DefaultGuardrailMaxFailures = 2
	// DefaultPlaywrightMCPCommand 默认 Playwright MCP 命令
	DefaultPlaywrightMCPCommand = "npx"
	// DefaultPlaywrightMCPArgs 默认 Playwright MCP 参数
	DefaultPlaywrightMCPArgs = "-y @playwright/mcp@latest"
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
// 对齐 Python: first_non_empty_env
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
// 对齐 Python: normalize_provider
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
// 对齐 Python: is_truthy_env
func IsTruthyEnv(value string) bool {
	lowered := strings.TrimSpace(strings.ToLower(value))
	return truthyEnvValues[lowered]
}

// IsFalsyEnv 判断环境变量值是否为假。
// 对齐 Python: is_falsy_env
func IsFalsyEnv(value string) bool {
	lowered := strings.TrimSpace(strings.ToLower(value))
	return falsyEnvValues[lowered]
}

// ResolveIntEnv 从环境变量解析整数。
// 对齐 Python: resolve_int_env
func ResolveIntEnv(keys []string, defaultVal int, minimum *int) int {
	for _, key := range keys {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		var value int
		if _, err := parseSimpleInt(raw, &value); err != nil {
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
// 对齐 Python: resolve_bool_env
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
// 对齐 Python: resolve_model_name
func ResolveModelName() string {
	if v := FirstNonEmptyEnv("MODEL_NAME"); v != "" {
		return v
	}
	return DefaultModelName
}

// ResolveBrowserTimeoutS 解析浏览器超时时间。
// 对齐 Python: resolve_browser_timeout_s
func ResolveBrowserTimeoutS() int {
	min := 1
	return ResolveIntEnv([]string{"BROWSER_TIMEOUT_S", "PLAYWRIGHT_TOOL_TIMEOUT_S"}, DefaultBrowserTimeoutS, &min)
}

// InferProviderFromAPIBase 从 API Base URL 推断 provider。
// 对齐 Python: infer_provider_from_api_base
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
// 对齐 Python: parse_command_args
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseSimpleInt 简单整数解析辅助
func parseSimpleInt(s string, out *int) (bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return false, nil
	}
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	if s == "" {
		return false, nil
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return false, nil
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		n = -n
	}
	*out = n
	return true, nil
}

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
```

**注意：** 以上即为完整实现代码，无伪代码。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/browser_move/... -v`
Expected: PASS

- [ ] **Step 5: 补充 ResolveModelSettings 函数和测试**

在 env.go 中添加 `ResolveModelSettings()` 函数（对齐 Python `resolve_model_settings()`，~100 行），并在 env_test.go 中添加测试用例。此函数支持 openai/openrouter/siliconflow/dashscope 四种 provider 的环境变量优先级链。

- [ ] **Step 6: 运行全部测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/browser_move/... -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/env.go internal/agentcore/harness/tools/browser_move/env_test.go
git commit -m "feat(browser_move): 实现环境变量解析工具 env.go"
```

---

### Task 3: 实现 parsing.go — JSON 解析工具

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/parsing.go`
- Create: `internal/agentcore/harness/tools/browser_move/parsing_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/utils/parsing.py`

- [ ] **Step 1: 写 parsing_test.go 测试**

测试用例覆盖：
- `ExtractJSONObject`：空输入、dict 输入、直接 JSON 解析、```json 代码块提取、首尾花括号匹配、Playwright `### Result` 标记处理
- `SanitizeJSONSchema`：折叠 anyOf nullable、移除 $schema/$id/$defs、null type → "object"

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/agentcore/harness/tools/browser_move/... -run "TestExtractJSONObject|TestSanitizeJSONSchema" -v`

- [ ] **Step 3: 写 parsing.go 实现**

实现 `ExtractJSONObject(text any) map[string]any` 和 `SanitizeJSONSchema(schema any) any`，一比一对齐 Python `extract_json_object` 和 `sanitize_json_schema` 的逻辑。

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/parsing.go internal/agentcore/harness/tools/browser_move/parsing_test.go
git commit -m "feat(browser_move): 实现 JSON 解析工具 parsing.go"
```

---

### Task 4: 实现 config.go — 配置数据类与工厂函数

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/config.go`
- Create: `internal/agentcore/harness/tools/browser_move/config_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/playwright_runtime/config.py`

- [ ] **Step 1: 写 config_test.go 测试**

测试用例覆盖：
- `BuildBrowserGuardrails`：默认值、环境变量覆盖
- `BuildPlaywrightMCPConfig`：默认 MCP 配置、extension 模式、CDP endpoint、环境变量映射
- `BuildRuntimeSettings`：默认配置
- `ResolveRuntimeSettings`：传入 settings 直接返回、从 Model 推导
- `ResolvePlaywrightMCPCwd`：环境变量覆盖、默认 cwd

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写 config.go 实现**

核心结构体：
- `BrowserRunGuardrails`：MaxSteps/MaxFailures/TimeoutS/RetryOnce/ResumeOnMaxIterations
- `RuntimeSettings`：Provider/APIKey/APIBase/ModelName/MCPCfg/Guardrails

核心工厂函数：
- `BuildBrowserGuardrails() *BrowserRunGuardrails`
- `BuildPlaywrightMCPConfig() *mcptypes.McpServerConfig` — 支持 extension/CDP/env 映射
- `BuildRuntimeSettings() *RuntimeSettings`
- `ResolveRuntimeSettings(model *llm.Model, settings *RuntimeSettings) *RuntimeSettings` — 优先用传入 settings，否则从 Model 推导
- `ResolvePlaywrightMCPCwd() string`

**注意：** `BuildPlaywrightMCPConfig` 需要 import `mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"` 和 `llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"`。需要使用 `mcptypes.NewMcpServerConfig()` 构造。

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/config.go internal/agentcore/harness/tools/browser_move/config_test.go
git commit -m "feat(browser_move): 实现配置数据类与工厂函数 config.go"
```

---

### Task 5: 改写 browser_agent.go — BuildBrowserAgentConfig

**Files:**
- Modify: `internal/agentcore/harness/subagents/browser_agent.go`
- Modify: `internal/agentcore/harness/subagents/browser_agent_test.go`（如果不存在则创建）

**Python 参考:** `openjiuwen/harness/subagents/browser_agent.py:1-192`

- [ ] **Step 1: 写 browser_agent_test.go 测试**

测试用例覆盖：
- `BuildBrowserAgentConfig` 默认值（AgentCard/SystemPrompt/MaxIterations=25/FactoryName/FactoryKwargs）
- 语言选择（cn/en）
- 用户参数覆盖
- `DefaultBrowserAgentSystemPrompt` / `DefaultBrowserAgentDescription` 辅助函数

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 改写 browser_agent.go**

将现有 19 行骨架替换为完整实现：

关键改动：
1. 参数签名升级：`BuildBrowserAgentConfig(model *llm.Model, params *hschema.SubagentCreateParams)`
2. 新增常量 `BrowserAgentFactoryName = "browser_agent"`
3. 新增 `defaultBrowserAgentSystemPrompt` 和 `defaultBrowserAgentDescription`（中/英双语，一比一复刻 Python 原文）
4. 完整设置 AgentCard/SystemPrompt/Tools/Mcps/Model/Rails/MaxIterations=25/FactoryName/FactoryKwargs（含 `"settings"` key → `*RuntimeSettings`）
5. 导出辅助函数 `DefaultBrowserAgentSystemPrompt(language)` 和 `DefaultBrowserAgentDescription(language)`

**提示词一比一复刻**（直接从 Python 源码复制）：
- `DEFAULT_BROWSER_AGENT_SYSTEM_PROMPT_CN`：`browser_agent.py:78-101`
- `DEFAULT_BROWSER_AGENT_SYSTEM_PROMPT_EN`：`browser_agent.py:43-76`
- `DEFAULT_BROWSER_AGENT_DESCRIPTION_CN`：`browser_agent.py:112`
- `DEFAULT_BROWSER_AGENT_DESCRIPTION_EN`：`browser_agent.py:109-111`

**import 路径：**
- `bm "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/browser_move"` — 用于 `bm.ResolveRuntimeSettings` 和 `bm.RuntimeSettings`

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/agentcore/harness/subagents/... -v`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/subagents/browser_agent.go internal/agentcore/harness/subagents/browser_agent_test.go
git commit -m "feat(subagents): 完善 BuildBrowserAgentConfig，对齐 Python browser_agent.py"
```

---

### Task 6: Layer 1 编译检查与回填 doc.go

**Files:**
- Modify: `internal/agentcore/harness/subagents/doc.go` — 添加 browser_agent.go 条目

- [ ] **Step 1: 全量编译检查**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/...`
Expected: 编译通过

- [ ] **Step 2: 更新 subagents/doc.go 文件目录**

在 doc.go 中将 `browser_agent.go` 的描述从 `# browser 子代理配置构建` 改为 `# browser 子代理配置构建（完整配置 + 工厂名称 + 默认提示词）`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/subagents/doc.go
git commit -m "docs(subagents): 更新 doc.go 中 browser_agent.go 描述"
```

---

## Layer 2 — 运行时核心层

### Task 7: 实现 progress.go — BrowserTaskProgressState

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/progress.go`
- Create: `internal/agentcore/harness/tools/browser_move/progress_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/playwright_runtime/service.py:44-116`

- [ ] **Step 1: 写 progress_test.go 测试**

测试用例覆盖：
- `NewBrowserTaskProgressStateFromDict`：正常数据、空数据、last_page 子对象解析
- `IsEmpty`：初始状态为空、有 completed_steps 非空
- `ToDict`：往返一致性

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写 progress.go 实现**

核心结构体 `BrowserTaskProgressState` 含 12 个字段（RequestID/Status/CompletedSteps/RemainingSteps/NextStep/CompletionEvidence/MissingRequirements/RecentToolSteps/LastPageURL/LastPageTitle/LastScreenshot/LastWorkerFinal），实现 `FromDict`/`ToDict`/`IsEmpty`。

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/progress.go internal/agentcore/harness/tools/browser_move/progress_test.go
git commit -m "feat(browser_move): 实现 BrowserTaskProgressState 进度状态"
```

---

### Task 8: 实现 service.go — BrowserService（第 1 部分：核心结构与取消逻辑）

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/service.go`
- Create: `internal/agentcore/harness/tools/browser_move/service_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/playwright_runtime/service.py:118-570`

由于 service.go 约 800 行，分两个 Task 实现。本 Task 实现核心结构和取消/会话逻辑。

- [ ] **Step 1: 写 service_test.go 测试（第 1 部分）**

测试用例覆盖：
- `NewBrowserService` 构造
- `SessionNew` 会话创建
- `RequestCancel` / `ClearCancel` / `IsCancelled` 取消逻辑
- `_cancelKey` / `_inflightKey` 键生成

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写 service.go（第 1 部分）**

实现：
- `BrowserService` 结构体（Provider/APIKey/APIBase/ModelName/MCPCfg/Guardrails + 内部状态字段：started/sessions/locks/cancelStore/progressBySession/failureContextBySession 等）
- `NewBrowserService()` 构造函数
- `SessionNew()` / `RequestCancel()` / `ClearCancel()` / `IsCancelled()`
- `_cancelKey()` / `_inflightKey()` / `_registerInflightTask()` / `_unregisterInflightTask()`

**占位标记：**
```go
// managedDriver 托管浏览器驱动，⤵️ 9.38-49 回填
managedDriver any

// browserAgent Worker Agent 实例，Layer 3 回填
browserAgent any
```

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/service.go internal/agentcore/harness/tools/browser_move/service_test.go
git commit -m "feat(browser_move): 实现 BrowserService 核心结构与取消逻辑"
```

---

### Task 9: 实现 service.go — BrowserService（第 2 部分：进度状态与 RunTask）

**Files:**
- Modify: `internal/agentcore/harness/tools/browser_move/service.go`
- Modify: `internal/agentcore/harness/tools/browser_move/service_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/playwright_runtime/service.py:570-1494`

- [ ] **Step 1: 写 service_test.go 测试（第 2 部分）**

测试用例覆盖：
- `RecordToolProgress` / `RecordWorkerProgress` / `GetProgressState` / `ExportProgressState` / `SetProgressState` / `ClearProgressState`
- `BuildProgressContext`：空状态、有 completed_steps/remaining_steps
- `BuildFailureSummary`：完整摘要构建
- `ShouldTreatAsCompleted`：status=completed + 有 evidence → true；status=partial → false
- `NormalizeProgressStatus`：complete/completed/done → completed；in_progress → partial

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 添加 service.go 第 2 部分**

实现：
- 进度状态管理方法（`RecordToolProgress`/`RecordWorkerProgress`/`GetProgressState`/`ExportProgressState`/`SetProgressState`/`ClearProgressState`/`BuildProgressContext`/`BuildFailureSummary`/`ShouldTreatAsCompleted`）
- `NormalizeScreenshotValue`：本地路径 → data URL
- `RunTask` 方法：完整重试链（retry_once + resume_on_max_iterations + transport error 重启）

**占位标记：**
```go
// EnsureRuntimeReady 中 ManagedBrowserDriver 逻辑 ⤵️ 9.38-49 回填
// _checkConnection 中 MCP client ping ⤵️ 9.38-49 回填
// _restart 中 _restartBrowserRuntime 调用 ManagedBrowserDriver ⤵️ 9.38-49 回填
```

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/service.go internal/agentcore/harness/tools/browser_move/service_test.go
git commit -m "feat(browser_move): 实现 BrowserService 进度状态与 RunTask"
```

---

### Task 10: 实现 runtime.go — BrowserAgentRuntime

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/runtime.go`
- Create: `internal/agentcore/harness/tools/browser_move/runtime_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/playwright_runtime/runtime.py:56-537`

- [ ] **Step 1: 写 runtime_test.go 测试**

测试用例覆盖：
- `NewBrowserAgentRuntime` 构造
- `CancelRun` / `ClearCancel`：委托到 BrowserService
- `ProbeInteractives` / `ProbeCards`：mock code executor（Layer 3 回填，本 Task 用占位测试）
- `ListActions`：返回 controller 注册的动作列表
- `RuntimeHealth`：返回 service 状态
- `_unwrapMCPTextResult`：dict{content:[{type:"text",text:"..."}]} → 文本提取

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写 runtime.go 实现**

核心结构体 `BrowserAgentRuntime`，方法包括：
- `NewBrowserAgentRuntime(provider, apiKey, apiBase, modelName string, mcpCfg *mcptypes.McpServerConfig, guardrails *BrowserRunGuardrails) *BrowserAgentRuntime`
- `EnsureRuntimeReady(ctx)` — 委托到 service + 初始化 code executor + controller
- `EnsureStarted(ctx)` — 委托到 service + 注册 runtime tools
- `CancelRun(ctx, sessionID, requestID)` / `ClearCancel(ctx, sessionID, requestID)`
- `ProbeInteractives(ctx, maxItems, viewportOnly, query)` / `ProbeCards(ctx, maxCards, viewportOnly, includeButtons, query)` — 调用 code executor 执行 JS 探测代码
- `RunCustomAction(ctx, action, sessionID, requestID, params)` / `ListActions(ctx)`
- `RuntimeHealth(ctx)` / `Shutdown(ctx)`

**占位标记：**
```go
// ensureBrowserRuntimeClientPatch ⤵️ 9.38-49 回填
// _getPlaywrightMCPTool ⤵️ 9.38-49 回填（需要 Runner.resource_mgr）
// _callPlaywrightRunCodeUnsafe ⤵️ 9.38-49 回填（需要 MCP 工具调用）
```

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/runtime.go internal/agentcore/harness/tools/browser_move/runtime_test.go
git commit -m "feat(browser_move): 实现 BrowserAgentRuntime 运行时内核"
```

---

### Task 11: 实现 browser_rail.go — BrowserRuntimeRail

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/browser_rail.go`
- Create: `internal/agentcore/harness/tools/browser_move/browser_rail_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/playwright_runtime/runtime.py:539-808`

- [ ] **Step 1: 写 browser_rail_test.go 测试**

测试用例覆盖：
- `NewBrowserRuntimeRail` 构造
- `Priority` 返回值
- `_extractProgressPayload`：包含 `<browser_progress>` 标签的文本提取
- `_buildProgressResult`：status=completed → ok=true
- `_isBrowserProgressTool`：browser_ 前缀判断
- `BeforeInvoke`：mock session 恢复进度
- `AfterInvoke`：提取 progress payload + 判断完成

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写 browser_rail.go 实现**

`BrowserRuntimeRail` 嵌入 `interfaces.BaseRail`，实现 4 个关键钩子：
- `BeforeInvoke`：调用 runtime.EnsureRuntimeReady()；注入 MCP ability；从 session 恢复进度；记录任务文本
- `BeforeModelCall`：注入 `browser_progress_format` 和 `browser_progress_continuation` PromptSection
- `AfterToolCall`：对 browser_ 前缀工具记录进度
- `AfterInvoke`：提取 `<browser_progress>` payload；判断完成/失败；持久化进度

进度标签正则：
```go
var browserProgressTagRE = regexp.MustCompile(`<browser_progress>\s*(\{.*?\})\s*</browser_progress>`)
```

进度状态键名对齐 Python：
```go
const (
	browserProgressStateKey          = "__browser_subagent_progress_state__"
	browserProgressTaskKey           = "__browser_subagent_last_task__"
	browserProgressSectionName       = "browser_progress_continuation"
	browserProgressFormatSectionName = "browser_progress_format"
)
```

**import 路径：**
- `sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"`
- `prompts "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"`

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/browser_rail.go internal/agentcore/harness/tools/browser_move/browser_rail_test.go
git commit -m "feat(browser_move): 实现 BrowserRuntimeRail 进度追踪 Rail"
```

---

### Task 12: 实现 runtime_tools.go — 7 个运行时工具

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/runtime_tools.go`
- Create: `internal/agentcore/harness/tools/browser_move/runtime_tools_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/playwright_runtime/runtime_tools.py`

- [ ] **Step 1: 写 runtime_tools_test.go 测试**

测试用例覆盖（使用 mock BrowserAgentRuntime）：
- `BrowserCancelTool.Invoke`：成功取消
- `BrowserClearCancelTool.Invoke`：成功清除
- `BrowserCustomActionTool.Invoke`：成功运行动作
- `BrowserListActionsTool.Invoke`：返回动作列表
- `BrowserProbeInteractivesTool.Invoke`：参数解析（max_items/viewport_only/query）
- `BrowserProbeCardsTool.Invoke`：参数解析（max_cards/viewport_only/include_buttons/query）
- `BrowserRuntimeHealthTool.Invoke`：返回健康状态
- `BuildBrowserRuntimeTools`：返回 7 个工具

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写 runtime_tools.go 实现**

7 个 Tool 实现，每个：
- 定义 struct 嵌入 `*tool.ToolCard`
- 实现 `tool.Tool` 接口（Card/Invoke/Stream）
- 描述和参数 schema 一比一复刻 Python 的 `_XXX_DESC` 和 `_XXX_PARAMS`

工厂函数：
```go
func BuildBrowserRuntimeTools(runtime *BrowserAgentRuntime, language string) []tool.Tool
```

**Tool 创建方式**：使用自定义 struct 实现 `tool.Tool` 接口（而非 `tool.NewTool` 泛型函数），因为每个 Tool 需要持有 `*BrowserAgentRuntime` 引用。

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/runtime_tools.go internal/agentcore/harness/tools/browser_move/runtime_tools_test.go
git commit -m "feat(browser_move): 实现 7 个运行时辅助工具"
```

---

### Task 13: 实现 browser_agent_factory.go — CreateBrowserAgent 工厂函数

**Files:**
- Create: `internal/agentcore/harness/browser_agent_factory.go`
- Create: `internal/agentcore/harness/browser_agent_factory_test.go`
- Modify: `internal/agentcore/harness/deep_agent.go` — 修改 switch 分支

**Python 参考:** `openjiuwen/harness/subagents/browser_agent.py:195-262`

- [ ] **Step 1: 写 browser_agent_factory_test.go 测试**

测试用例覆盖：
- `CreateBrowserAgent`：基本创建（mock Model/Runner）
- 合并用户 tools + 注入的 runtime tools
- 合并用户 rails + BrowserRuntimeRail
- FactoryKwargs 中的 settings 传递

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写 browser_agent_factory.go**

```go
package harness

// CreateBrowserAgent 创建并配置 BrowserAgent DeepAgent 实例。
// 对齐 Python: create_browser_agent(model, card=..., system_prompt=..., ...)
func CreateBrowserAgent(ctx context.Context, params *hschema.SubagentCreateParams) (*DeepAgent, error) {
    language := hprompts.ResolveLanguage(params.Language)
    resolvedSettings := bm.ResolveRuntimeSettings(params.Model, nil)
    // ... 从 FactoryKwargs 提取 settings
    // ... 创建 BrowserAgentRuntime
    // ... 注入 runtime tools + BrowserRuntimeRail
    // ... 调用 CreateDeepAgent
}
```

- [ ] **Step 4: 修改 deep_agent.go switch 分支**

将 `case "browser_agent", "browser_runtime":` 从返回 error 改为调用 `CreateBrowserAgent(ctx, kwargs)`。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/agentcore/harness/... -v`

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/browser_agent_factory.go internal/agentcore/harness/browser_agent_factory_test.go internal/agentcore/harness/deep_agent.go
git commit -m "feat(harness): 实现 CreateBrowserAgent 工厂函数并集成到 CreateSubagent"
```

---

### Task 14: Layer 2 编译检查

- [ ] **Step 1: 全量编译检查**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/...`
Expected: 编译通过

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -v -count=1`
Expected: 全部 PASS

- [ ] **Step 3: 提交（如有修复）**

```bash
git add -A && git commit -m "fix(browser_move): Layer 2 编译修复"
```

---

## Layer 3 — 探测与控制层

### Task 15: 实现 profiles.go — BrowserProfile + BrowserProfileStore

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/profiles.go`
- Create: `internal/agentcore/harness/tools/browser_move/profiles_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/playwright_runtime/profiles.py`

- [ ] **Step 1: 写 profiles_test.go 测试**

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写 profiles.go 实现**

`BrowserProfile` 结构体 + `BrowserProfileStore`（JSON 文件存储），一比一对齐 Python。

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/profiles.go internal/agentcore/harness/tools/browser_move/profiles_test.go
git commit -m "feat(browser_move): 实现 BrowserProfile 和 BrowserProfileStore"
```

---

### Task 16: 实现 site_profiles.go — 站点配置与选择器缓存

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/site_profiles.go`
- Create: `internal/agentcore/harness/tools/browser_move/site_profiles_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/playwright_runtime/site_profiles.py`

- [ ] **Step 1: 写 site_profiles_test.go 测试**

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写 site_profiles.go 实现**

`SiteProfile` 结构体 + `SelectorCache` + `BuiltinSiteProfiles()` + `GetSelectorCache()`。

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/site_profiles.go internal/agentcore/harness/tools/browser_move/site_profiles_test.go
git commit -m "feat(browser_move): 实现站点配置文件和选择器缓存"
```

---

### Task 17: 实现 probes.go — JavaScript 探测代码生成

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/probes.go`
- Create: `internal/agentcore/harness/tools/browser_move/probes_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/playwright_runtime/probes.py`

- [ ] **Step 1: 写 probes_test.go 测试**

测试用例覆盖：
- `BuildInteractiveProbeJS`：返回有效 JavaScript 字符串，包含 maxItems/viewportOnly/query 参数
- `BuildCardProbeJS`：返回有效 JavaScript 字符串，包含 maxCards/siteProfiles/selectorCache 参数

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写 probes.go 实现**

`BuildInteractiveProbeJS(maxItems int, viewportOnly bool, query string) string` 和 `BuildCardProbeJS(maxCards int, viewportOnly bool, includeButtons bool, query string, siteProfiles []SiteProfile, selectorCacheRecords []SelectorCacheRecord) string`。

JavaScript 代码以 Go raw string literal 存储，逻辑一比一复刻 Python 的 `build_interactive_probe_js` 和 `build_card_probe_js`。

**注意：** probes.py 有 ~1250 行，大部分是 JavaScript 模板代码。Go 侧需要将 Python 中的 f-string 模板转换为 Go 的 `fmt.Sprintf` 或字符串拼接。

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/probes.go internal/agentcore/harness/tools/browser_move/probes_test.go
git commit -m "feat(browser_move): 实现 JavaScript 探测代码生成"
```

---

### Task 18: 实现 controllers.go — BaseController + ActionController

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/controllers.go`
- Create: `internal/agentcore/harness/tools/browser_move/controllers_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/controllers/base.py` + `controllers/action.py`

- [ ] **Step 1: 写 controllers_test.go 测试**

测试用例覆盖：
- `ActionController` 构造
- `RegisterAction` / `ListActions` / `DescribeActions`
- `RunAction`：执行已注册动作
- `BindCodeExecutor`：绑定代码执行器

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写 controllers.go 实现**

`BaseController` 接口 + `ActionController` struct 实现。`RegisterBuiltinActions` 方法标记 `⤵️ 9.38-49` 占位。

**占位标记：**
```go
// RegisterBuiltinActions 注册内置浏览器动作（拖拽、坐标解析等）
// ⤵️ 9.38-49: 具体动作的执行逻辑依赖 Playwright code executor，待回填
func (c *ActionController) RegisterBuiltinActions() {
	// TODO: ⤵️ 9.38-49 回填 drag_drop / resolve_coordinates 等内置动作
}
```

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/controllers.go internal/agentcore/harness/tools/browser_move/controllers_test.go
git commit -m "feat(browser_move): 实现 BaseController 和 ActionController"
```

---

### Task 19: 实现 agents.go — Worker Agent 构建器

**Files:**
- Create: `internal/agentcore/harness/tools/browser_move/agents.go`
- Create: `internal/agentcore/harness/tools/browser_move/agents_test.go`

**Python 参考:** `openjiuwen/harness/tools/browser_move/playwright_runtime/agents.py`

- [ ] **Step 1: 写 agents_test.go 测试**

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写 agents.go 实现**

`BuildBrowserWorkerAgent` 函数：创建 ReActAgent，配置 model client + system prompt + MCP tools + max_steps + temperature/top_p。

`BuildBrowserWorkerSystemPrompt` 函数：一比一复刻 Python `build_browser_worker_system_prompt()`。

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/agents.go internal/agentcore/harness/tools/browser_move/agents_test.go
git commit -m "feat(browser_move): 实现 Worker Agent 构建器"
```

---

### Task 20: Layer 3 编译检查 + 回填 runtime.go 中的 controller

**Files:**
- Modify: `internal/agentcore/harness/tools/browser_move/runtime.go` — 回填 controller 字段

- [ ] **Step 1: 回填 runtime.go 中的 controller**

将 `controller any` 占位字段改为 `controller *ActionController`，并在 `EnsureRuntimeReady` 中调用 `controller.BindCodeExecutor` 和 `controller.RegisterBuiltinActions`。

- [ ] **Step 2: 回填 service.go 中的 browserAgent**

将 `browserAgent any` 改为 `browserAgent *react_agent.ReActAgent`，在 `EnsureStarted` 中调用 `BuildBrowserWorkerAgent`。

- [ ] **Step 3: 全量编译检查**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/...`
Expected: 编译通过

- [ ] **Step 4: 全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -v -count=1`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/browser_move/runtime.go internal/agentcore/harness/tools/browser_move/service.go
git commit -m "feat(browser_move): 回填 controller 和 browserAgent 字段类型"
```

---

### Task 21: 更新 IMPLEMENTATION_PLAN.md + doc.go

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`
- Modify: `internal/agentcore/harness/tools/browser_move/doc.go`
- Modify: `internal/agentcore/harness/subagents/doc.go`

- [ ] **Step 1: 更新 IMPLEMENTATION_PLAN.md**

将 9.26 的 `🔄` 改为 `✅`：
```
| 9.26 | ✅ | BrowserAgent | 浏览器子 Agent（骨架已建，⤵️ 9.38-49） | `openjiuwen/harness/subagents/` |
```

改为：
```
| 9.26 | ✅ | BrowserAgent | 浏览器子 Agent | `openjiuwen/harness/subagents/` |
```

（移除 `⤵️ 9.38-49` 标记，因为代码中已有 `⤵️` 注释）

- [ ] **Step 2: 更新 doc.go 文件目录**

确保 `browser_move/doc.go` 和 `subagents/doc.go` 的文件目录是最新的。

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md internal/agentcore/harness/tools/browser_move/doc.go internal/agentcore/harness/subagents/doc.go
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 中 9.26 状态为 ✅"
```

---

### Task 22: 最终全量编译与测试

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译通过

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags=test ./internal/agentcore/... -count=1`
Expected: 全部 PASS

- [ ] **Step 3: 覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -tags=test -cover ./internal/agentcore/harness/tools/browser_move/... ./internal/agentcore/harness/subagents/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 提交最终状态**

```bash
git add -A && git commit -m "feat(9.26): BrowserAgent 全栈实现完成"
```
