# 10.3.23 Hooks（executor + user_hook_rail）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现用户配置的 hooks 执行引擎（配置模型 + 执行器 + UserHookRail），对齐 Python jiuwenswarm/common/hooks_config.py + server/hooks/executor.py + server/hooks/user_hook_rail.py

**Architecture:** 配置模型放 internal/common/hooks/（全局共享），执行器和 UserHookRail 放 internal/swarm/server/hooks/（AgentServer 侧）。HookExecutor 支持 command（子进程）和 prompt（LLM 审核）两种 hook 类型。UserHookRail(embed DeepAgentRail) 覆盖 BeforeToolCall/AfterToolCall/OnToolException/AfterInvoke 4 个钩子方法。

**Tech Stack:** Go 1.x + os/exec（command hook）+ agentcore/llm Model（prompt hook）+ regexp（HookMatcher）

---

## File Structure

### 创建文件

| 文件 | 包 | 职责 |
|------|-----|------|
| `internal/common/hooks/doc.go` | hooks | 包文档 |
| `internal/common/hooks/types.go` | hooks | HookType 枚举 + HookEvent 17 个常量 + AgentRailEvents/GatewayEvents 分组 + IsRailEvent/IsGatewayEvent |
| `internal/common/hooks/types_test.go` | hooks | HookType/HookEvent 常量值 + AgentRailEvents/GatewayEvents 判断 |
| `internal/common/hooks/config.go` | hooks | CommandHookConfig/PromptHookConfig/HookMatcher/HooksConfig/LoadHooksConfig |
| `internal/common/hooks/config_test.go` | hooks | HookMatcher.Matches 各模式 + HooksConfig.Match + GetEventSummary + LoadHooksConfig |
| `internal/swarm/server/hooks/doc.go` | hooks (server) | 包文档 |
| `internal/swarm/server/hooks/executor.go` | hooks (server) | HookOutcome/HookResult/LLMConfig + HookExecutor(RunAll/runCommandHook/runPromptHook/queryLLM) + ParseCommandOutput + ExtractJSONFromResponse |
| `internal/swarm/server/hooks/executor_test.go` | hooks (server) | ParseCommandOutput + ExtractJSONFromResponse + HookExecutor.RunAll + command hook 各退出码 + prompt hook mock |
| `internal/swarm/server/hooks/user_hook_rail.go` | hooks (server) | UserHookRail(embed DeepAgentRail) + BeforeToolCall/AfterToolCall/OnToolException/AfterInvoke |
| `internal/swarm/server/hooks/user_hook_rail_test.go` | hooks (server) | 4 个钩子方法的 blocking/modifiedInput/additionalContext 行为 |

### 修改文件

| 文件 | 修改内容 |
|------|---------|
| `internal/swarm/server/adapter/deep_adapter_rails.go` | 在 buildAgentRails 中添加步骤 21: UserHookRail 注册 |
| `internal/swarm/server/handle_extensions.go` | handleHooksList 从空列表改为 LoadHooksConfig + GetEventSummary |
| `IMPLEMENTATION_PLAN.md` | 10.3.23-26 拆分为 10.3.23.1~5 + 10.3.24~26 |

---

### Task 1: common/hooks — HookType + HookEvent 常量

**Files:**
- Create: `internal/common/hooks/doc.go`
- Create: `internal/common/hooks/types.go`
- Test: `internal/common/hooks/types_test.go`

- [ ] **Step 1: 写失败的测试**

```go
package hooks

import "testing"

// TestHookType_常量值 测试 HookType 对齐 Python HookType(COMMAND/PROMPT)
func TestHookType_常量值(t *testing.T) {
	if HookTypeCommand != "command" {
		t.Errorf("HookTypeCommand = %q, want %q", HookTypeCommand, "command")
	}
	if HookTypePrompt != "prompt" {
		t.Errorf("HookTypePrompt = %q, want %q", HookTypePrompt, "prompt")
	}
}

// TestHookEvent_常量值 测试 17 个 HookEvent 常量对齐 Python HookEvent
func TestHookEvent_常量值(t *testing.T) {
	want := map[string]string{
		"PreToolUse":            HookEventPreToolUse,
		"PostToolUse":           HookEventPostToolUse,
		"PostToolUseFailure":    HookEventPostToolUseFailure,
		"Stop":                  HookEventStop,
		"UserPromptSubmit":      HookEventUserPromptSubmit,
		"SessionStart":          HookEventSessionStart,
		"SessionEnd":            HookEventSessionEnd,
		"Notification":          HookEventNotification,
		"PermissionRequest":     HookEventPermissionRequest,
		"PermissionDenied":      HookEventPermissionDenied,
		"SubagentStart":         HookEventSubagentStart,
		"SubagentStop":          HookEventSubagentStop,
		"ConfigChange":          HookEventConfigChange,
		"InstructionsLoaded":    HookEventInstructionsLoaded,
		"Setup":                 HookEventSetup,
		"BeforeModelCall":       HookEventBeforeModelCall,
		"AfterModelCall":        HookEventAfterModelCall,
	}
	for pythonName, goConst := range want {
		if goConst != pythonName {
			t.Errorf("Go constant = %q, want Python value %q", goConst, pythonName)
		}
	}
}

// TestIsRailEvent 测试 AgentServer Rail 层事件判断
func TestIsRailEvent(t *testing.T) {
	railEvents := []string{
		HookEventPreToolUse, HookEventPostToolUse, HookEventPostToolUseFailure,
		HookEventStop, HookEventPermissionRequest, HookEventPermissionDenied,
		HookEventSubagentStart, HookEventSubagentStop,
		HookEventBeforeModelCall, HookEventAfterModelCall,
	}
	for _, e := range railEvents {
		if !IsRailEvent(e) {
			t.Errorf("IsRailEvent(%q) = false, want true", e)
		}
	}
	// Gateway 事件应返回 false
	if IsRailEvent(HookEventUserPromptSubmit) {
		t.Errorf("IsRailEvent(%q) = true, want false", HookEventUserPromptSubmit)
	}
}

// TestIsGatewayEvent 测试 Gateway 层事件判断
func TestIsGatewayEvent(t *testing.T) {
	gatewayEvents := []string{
		HookEventUserPromptSubmit, HookEventSessionStart, HookEventSessionEnd,
		HookEventNotification, HookEventConfigChange, HookEventInstructionsLoaded,
		HookEventSetup,
	}
	for _, e := range gatewayEvents {
		if !IsGatewayEvent(e) {
			t.Errorf("IsGatewayEvent(%q) = false, want true", e)
		}
	}
	// Rail 事件应返回 false
	if IsGatewayEvent(HookEventPreToolUse) {
		t.Errorf("IsGatewayEvent(%q) = true, want false", HookEventPreToolUse)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -tags=!integration,!llm,!e2e ./internal/common/hooks/... -v 2>&1 | head -20`
Expected: FAIL（包不存在）

- [ ] **Step 3: 写 doc.go + types.go 实现**

`internal/common/hooks/doc.go`:
```go
// Package hooks 提供 Hooks 配置模型和事件定义，对齐 Python jiuwenswarm/common/hooks_config.py。
//
// 本包定义 HookType/HookEvent 枚举常量、CommandHookConfig/PromptHookConfig 配置模型、
// HookMatcher 匹配逻辑、HooksConfig 聚合与加载函数。
// Gateway 和 AgentServer 均可引用此包（全局共享，不依赖 server/ 或 agentcore/）。
//
// 文件目录：
//
//	hooks/
//	├── doc.go    # 包文档
//	├── types.go  # HookType 枚举 + HookEvent 常量 + AgentRailEvents/GatewayEvents 分组
//	└── config.go # CommandHookConfig/PromptHookConfig/HookMatcher/HooksConfig/LoadHooksConfig
//
// 对应 Python 代码：jiuwenswarm/common/hooks_config.py
package hooks
```

`internal/common/hooks/types.go`:
```go
package hooks

// ──────────────────────────── 枚举 ────────────────────────────

// HookType hook 类型枚举，对齐 Python HookType(COMMAND/PROMPT)
type HookType string

const (
	// HookTypeCommand command 类型 hook（子进程执行），对齐 Python HookType.COMMAND
	HookTypeCommand HookType = "command"
	// HookTypePrompt prompt 类型 hook（LLM 审核），对齐 Python HookType.PROMPT
	HookTypePrompt HookType = "prompt"
)

// ──────────────────────────── 常量 ────────────────────────────

// HookEvent hook 事件常量，对齐 Python HookEvent(17 个)
// 统一为字符串常量，格式与 Python 一致（如 "PreToolUse"）
const (
	// HookEventPreToolUse 工具调用前，对齐 Python PRE_TOOL_USE
	HookEventPreToolUse = "PreToolUse"
	// HookEventPostToolUse 工具调用后，对齐 Python POST_TOOL_USE
	HookEventPostToolUse = "PostToolUse"
	// HookEventPostToolUseFailure 工具调用异常后，对齐 Python POST_TOOL_USE_FAILURE
	HookEventPostToolUseFailure = "PostToolUseFailure"
	// HookEventStop Agent 停止，对齐 Python STOP
	HookEventStop = "Stop"
	// HookEventUserPromptSubmit 用户提交 prompt，对齐 Python USER_PROMPT_SUBMIT
	HookEventUserPromptSubmit = "UserPromptSubmit"
	// HookEventSessionStart 会话开始，对齐 Python SESSION_START
	HookEventSessionStart = "SessionStart"
	// HookEventSessionEnd 会话结束，对齐 Python SESSION_END
	HookEventSessionEnd = "SessionEnd"
	// HookEventNotification 通知，对齐 Python NOTIFICATION
	HookEventNotification = "Notification"
	// HookEventPermissionRequest 权限请求，对齐 Python PERMISSION_REQUEST
	HookEventPermissionRequest = "PermissionRequest"
	// HookEventPermissionDenied 权限拒绝，对齐 Python PERMISSION_DENIED
	HookEventPermissionDenied = "PermissionDenied"
	// HookEventSubagentStart 子 Agent 启动，对齐 Python SUBAGENT_START
	HookEventSubagentStart = "SubagentStart"
	// HookEventSubagentStop 子 Agent 停止，对齐 Python SUBAGENT_STOP
	HookEventSubagentStop = "SubagentStop"
	// HookEventConfigChange 配置变更，对齐 Python CONFIG_CHANGE
	HookEventConfigChange = "ConfigChange"
	// HookEventInstructionsLoaded 指令加载，对齐 Python INSTRUCTIONS_LOADED
	HookEventInstructionsLoaded = "InstructionsLoaded"
	// HookEventSetup 安装，对齐 Python SETUP
	HookEventSetup = "Setup"
	// HookEventBeforeModelCall 模型调用前，对齐 Python BEFORE_MODEL_CALL
	HookEventBeforeModelCall = "BeforeModelCall"
	// HookEventAfterModelCall 模型调用后，对齐 Python AFTER_MODEL_CALL
	HookEventAfterModelCall = "AfterModelCall"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// AgentRailEvents 需要在 AgentServer Rail 层执行的事件，对齐 Python _AGENT_RAIL_EVENTS
var AgentRailEvents = map[string]bool{
	HookEventPreToolUse:         true,
	HookEventPostToolUse:        true,
	HookEventPostToolUseFailure: true,
	HookEventStop:               true,
	HookEventPermissionRequest:  true,
	HookEventPermissionDenied:   true,
	HookEventSubagentStart:      true,
	HookEventSubagentStop:       true,
	HookEventBeforeModelCall:    true,
	HookEventAfterModelCall:     true,
}

// GatewayEvents 需要在 Gateway 层执行的事件，对齐 Python _GATEWAY_EVENTS
var GatewayEvents = map[string]bool{
	HookEventUserPromptSubmit:   true,
	HookEventSessionStart:       true,
	HookEventSessionEnd:         true,
	HookEventNotification:       true,
	HookEventConfigChange:       true,
	HookEventInstructionsLoaded: true,
	HookEventSetup:              true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsRailEvent 判断事件是否属于 AgentServer Rail 层，对齐 Python is_rail_event()
func IsRailEvent(event string) bool { return AgentRailEvents[event] }

// IsGatewayEvent 判断事件是否属于 Gateway 层，对齐 Python is_gateway_event()
func IsGatewayEvent(event string) bool { return GatewayEvents[event] }
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -tags=!integration,!llm,!e2e ./internal/common/hooks/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/common/hooks/doc.go internal/common/hooks/types.go internal/common/hooks/types_test.go
git commit -m "feat(10.3.23.1): 添加 HookType/HookEvent 常量 + AgentRailEvents/GatewayEvents 分组"
```

---

### Task 2: common/hooks — 配置模型 + HookMatcher + HooksConfig + LoadHooksConfig

**Files:**
- Create: `internal/common/hooks/config.go`
- Test: `internal/common/hooks/config_test.go`

- [ ] **Step 1: 写失败的测试**

```go
package hooks

import "testing"

// TestCommandHookConfig_默认值 测试 CommandHookConfig 字段
func TestCommandHookConfig_默认值(t *testing.T) {
	cfg := CommandHookConfig{}
	if cfg.Type != "" {
		t.Errorf("Type = %q, want empty (defaults set at usage)", cfg.Type)
	}
	if cfg.Timeout != 0 {
		t.Errorf("Timeout = %d, want 0 (defaults set at usage)", cfg.Timeout)
	}
}

// TestPromptHookConfig_默认值 测试 PromptHookConfig 字段
func TestPromptHookConfig_默认值(t *testing.T) {
	cfg := PromptHookConfig{}
	if cfg.Type != "" {
		t.Errorf("Type = %q, want empty", cfg.Type)
	}
	if cfg.Timeout != 0 {
		t.Errorf("Timeout = %d, want 0", cfg.Timeout)
	}
}

// TestHookMatcher_Matches_通配符 测试 * 匹配所有
func TestHookMatcher_Matches_通配符(t *testing.T) {
	m := HookMatcher{Matcher: "*"}
	if !m.Matches("any_tool") {
		t.Error("Matches('any_tool') with matcher=* should be true")
	}
	if !m.Matches("") {
		t.Error("Matches('') with matcher=* should be true")
	}
}

// TestHookMatcher_Matches_空字符串 测试空 matcher 匹配所有
func TestHookMatcher_Matches_空字符串(t *testing.T) {
	m := HookMatcher{Matcher: ""}
	if !m.Matches("anything") {
		t.Error("Matches('anything') with matcher='' should be true")
	}
}

// TestHookMatcher_Matches_精确匹配 测试精确字符串匹配
func TestHookMatcher_Matches_精确匹配(t *testing.T) {
	m := HookMatcher{Matcher: "read_file"}
	if !m.Matches("read_file") {
		t.Error("Matches('read_file') should be true")
	}
	if m.Matches("write_file") {
		t.Error("Matches('write_file') should be false")
	}
}

// TestHookMatcher_Matches_OR 匹配 测试 | 分隔的 OR 匹配
func TestHookMatcher_Matches_OR匹配(t *testing.T) {
	m := HookMatcher{Matcher: "read_file|write_file"}
	if !m.Matches("read_file") {
		t.Error("Matches('read_file') with OR matcher should be true")
	}
	if !m.Matches("write_file") {
		t.Error("Matches('write_file') with OR matcher should be true")
	}
	if m.Matches("delete_file") {
		t.Error("Matches('delete_file') with OR matcher should be false")
	}
}

// TestHookMatcher_Matches_正则匹配 测试正则匹配
func TestHookMatcher_Matches_正则匹配(t *testing.T) {
	m := HookMatcher{Matcher: "^read_.*"}
	if !m.Matches("read_file") {
		t.Error("Matches('read_file') with regex ^read_.* should be true")
	}
	if m.Matches("write_file") {
		t.Error("Matches('write_file') with regex ^read_.* should be false")
	}
	// 以 $ 结尾
	m2 := HookMatcher{Matcher: "_file$"}
	if !m2.Matches("read_file") {
		t.Error("Matches('read_file') with regex _file$ should be true")
	}
	// 含 .* 的正则
	m3 := HookMatcher{Matcher: "tool.*name"}
	if !m3.Matches("tool_xyz_name") {
		t.Error("Matches('tool_xyz_name') with regex tool.*name should be true")
	}
}

// TestHooksConfig_Match 测试 Match 获取匹配的 hook 配置
func TestHooksConfig_Match(t *testing.T) {
	cfg := HooksConfig{
		Events: map[string][]HookMatcher{
			HookEventPreToolUse: {
				{
					Matcher: "read_file",
					Hooks:   []map[string]any{{"type": "command", "command": "check_file"}},
				},
				{
					Matcher: "*",
					Hooks:   []map[string]any{{"type": "prompt", "prompt": "review"}},
				},
			},
		},
	}
	hooks := cfg.Match(HookEventPreToolUse, "read_file")
	if len(hooks) != 2 {
		t.Errorf("Match(PreToolUse, read_file) = %d hooks, want 2", len(hooks))
	}
	// 不匹配的事件应返回空
	hooks2 := cfg.Match(HookEventPostToolUse, "read_file")
	if len(hooks2) != 0 {
		t.Errorf("Match(PostToolUse, read_file) = %d hooks, want 0", len(hooks2))
	}
}

// TestHooksConfig_Match_禁用 测试 DisableAllHooks 时返回空
func TestHooksConfig_Match_禁用(t *testing.T) {
	cfg := HooksConfig{
		Events:          map[string][]HookMatcher{HookEventPreToolUse: {{Matcher: "*"}}},
		DisableAllHooks: true,
	}
	hooks := cfg.Match(HookEventPreToolUse, "read_file")
	if len(hooks) != 0 {
		t.Errorf("Match with DisableAllHooks should return 0 hooks, got %d", len(hooks))
	}
}

// TestHooksConfig_GetEventSummary 测试摘要生成
func TestHooksConfig_GetEventSummary(t *testing.T) {
	cfg := HooksConfig{
		Events: map[string][]HookMatcher{
			HookEventPreToolUse: {
				{Matcher: "*", Hooks: []map[string]any{{"type": "command"}}},
			},
		},
	}
	summary := cfg.GetEventSummary()
	if len(summary) != 17 {
		t.Errorf("GetEventSummary() len = %d, want 17 (all HookEvents)", len(summary))
	}
}

// TestLoadHooksConfig_空配置 测试空配置返回默认值
func TestLoadHooksConfig_空配置(t *testing.T) {
	cfg := LoadHooksConfig(nil)
	if cfg == nil {
		t.Error("LoadHooksConfig(nil) should return non-nil")
	}
	if len(cfg.Events) != 0 {
		t.Errorf("Events = %d, want 0 for nil config", len(cfg.Events))
	}
}

// TestLoadHooksConfig_有效配置 测试有效 hooks 配置加载
func TestLoadHooksConfig_有效配置(t *testing.T) {
	configBase := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "read_file|write_file",
					"hooks":   []any{map[string]any{"type": "command", "command": "check"}},
				},
			},
		},
	}
	cfg := LoadHooksConfig(configBase)
	if len(cfg.Events) != 1 {
		t.Errorf("Events count = %d, want 1", len(cfg.Events))
	}
	matchers := cfg.Events[HookEventPreToolUse]
	if len(matchers) != 1 {
		t.Errorf("PreToolUse matchers = %d, want 1", len(matchers))
	}
	if matchers[0].Matcher != "read_file|write_file" {
		t.Errorf("Matcher = %q, want %q", matchers[0].Matcher, "read_file|write_file")
	}
}

// TestLoadHooksConfig_禁用 测试 disable_all_hooks 加载
func TestLoadHooksConfig_禁用(t *testing.T) {
	configBase := map[string]any{
		"hooks": map[string]any{
			"disable_all_hooks": true,
		},
	}
	cfg := LoadHooksConfig(configBase)
	if !cfg.DisableAllHooks {
		t.Error("DisableAllHooks should be true")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -tags=!integration,!llm,!e2e ./internal/common/hooks/... -v -run "TestCommand|TestPrompt|TestHookMatcher|TestHooksConfig_Match|TestHooksConfig_Get|TestLoad" 2>&1 | head -30`
Expected: FAIL（类型未定义）

- [ ] **Step 3: 写 config.go 实现**

`internal/common/hooks/config.go`:
```go
package hooks

import (
	"fmt"
	"regexp"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CommandHookConfig command 类型 hook 配置，对齐 Python CommandHookConfig dataclass
type CommandHookConfig struct {
	// Type 类型标识，默认 "command"
	Type string `json:"type"`
	// Command shell 命令
	Command string `json:"command"`
	// Timeout 超时秒数，默认 30
	Timeout int `json:"timeout"`
	// Shell 执行器，默认 "bash"
	Shell string `json:"shell"`
	// StatusMessage 状态消息
	StatusMessage string `json:"status_message"`
}

// PromptHookConfig prompt 类型 hook 配置，对齐 Python PromptHookConfig dataclass
type PromptHookConfig struct {
	// Type 类型标识，默认 "prompt"
	Type string `json:"type"`
	// Prompt 模板字符串
	Prompt string `json:"prompt"`
	// Timeout 超时秒数，默认 15
	Timeout int `json:"timeout"`
	// Model LLM 模型名
	Model string `json:"model"`
	// StatusMessage 状态消息
	StatusMessage string `json:"status_message"`
}

// HookMatcher hook 匹配器，对齐 Python HookMatcher dataclass
type HookMatcher struct {
	// Matcher 匹配表达式，默认 "*"（通配/OR/正则/精确）
	Matcher string `json:"matcher"`
	// Hooks hook 配置列表
	Hooks []map[string]any `json:"hooks"`
}

// HooksConfig hooks 配置聚合，对齐 Python HooksConfig dataclass
type HooksConfig struct {
	// Events 事件 → matcher 列表
	Events map[string][]HookMatcher `json:"events"`
	// DisableAllHooks 禁用所有 hooks
	DisableAllHooks bool `json:"disable_all_hooks"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Matches 检查 query 是否匹配此 matcher，对齐 Python HookMatcher.matches()
// 支持：*（匹配所有）| |分隔的 OR 匹配 | 正则匹配 | 精确匹配
func (m *HookMatcher) Matches(query string) bool {
	pattern := m.Matcher
	if pattern == "" {
		pattern = "*"
	}
	if pattern == "*" {
		return true
	}
	// "|" 分隔的 OR 匹配（不以 ^ 开头时才走 OR 逻辑，对齐 Python）
	if containsPipe(pattern) && !startsWithAnchor(pattern) {
		parts := splitByPipe(pattern)
		for _, p := range parts {
			if matchSingle(trimSpace(p), query) {
				return true
			}
		}
		return false
	}
	return matchSingle(pattern, query)
}

// Match 获取匹配该事件 + query 的所有 hook 配置，对齐 Python HooksConfig.match()
func (c *HooksConfig) Match(event, query string) []map[string]any {
	if c.DisableAllHooks {
		return nil
	}
	matchers := c.Events[event]
	var result []map[string]any
	for _, m := range matchers {
		if m.Matches(query) {
			result = append(result, m.Hooks...)
		}
	}
	return result
}

// GetEventSummary 返回各事件的 hook 数量摘要，对齐 Python HooksConfig.get_event_summary()
func (c *HooksConfig) GetEventSummary() []map[string]any {
	allEvents := allHookEventValues()
	summaries := make([]map[string]any, 0, len(allEvents))
	for _, eventName := range allEvents {
		matchers := c.Events[eventName]
		totalHooks := 0
		matcherDetails := make([]map[string]any, 0, len(matchers))
		for _, m := range matchers {
			totalHooks += len(m.Hooks)
			matcherDetails = append(matcherDetails, map[string]any{
				"matcher":    m.Matcher,
				"hook_count": len(m.Hooks),
				"hooks":      m.Hooks,
			})
		}
		summaries = append(summaries, map[string]any{
			"name":         eventName,
			"total_hooks":  totalHooks,
			"matchers":     matcherDetails,
		})
	}
	return summaries
}

// LoadHooksConfig 从 config.yaml 的 hooks 段加载配置，对齐 Python load_hooks_config()
func LoadHooksConfig(configBase map[string]any) *HooksConfig {
	if configBase == nil {
		return &HooksConfig{}
	}
	hooksSection, ok := configBase["hooks"]
	if !ok {
		return &HooksConfig{}
	}
	hooksMap, ok := hooksSection.(map[string]any)
	if !ok {
		return &HooksConfig{}
	}

	disableAll := false
	if v, ok := hooksMap["disable_all_hooks"]; ok {
		disableAll = toBool(v)
	}

	events := make(map[string][]HookMatcher)
	for _, eventName := range allHookEventValues() {
		eventConfigs, ok := hooksMap[eventName]
		if !ok {
			continue
		}
		configsList, ok := eventConfigs.([]any)
		if !ok {
			logger.Warn(logger.ComponentCommon).
				Str("event", eventName).
				Msg("hooks 配置：event 段期望 []any 类型")
			continue
		}
		var matchers []HookMatcher
		for _, entry := range configsList {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				logger.Warn(logger.ComponentCommon).
					Str("event", eventName).
					Msg("hooks 配置：entry 期望 map[string]any 类型")
				continue
			}
			matcherStr := "*"
			if v, ok := entryMap["matcher"]; ok {
				if s, ok := v.(string); ok {
					matcherStr = s
				}
			}
			var hooksList []map[string]any
			if v, ok := entryMap["hooks"]; ok {
				if list, ok := v.([]any); ok {
					for _, h := range list {
						if hm, ok := h.(map[string]any); ok {
							hooksList = append(hooksList, hm)
						}
					}
				}
			}
			matchers = append(matchers, HookMatcher{
				Matcher: matcherStr,
				Hooks:   hooksList,
			})
		}
		if len(matchers) > 0 {
			events[eventName] = matchers
		}
	}
	return &HooksConfig{Events: events, DisableAllHooks: disableAll}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// matchSingle 单个模式匹配，对齐 Python HookMatcher._match_single()
func matchSingle(pattern, query string) bool {
	if pattern == query {
		return true
	}
	// 正则匹配：pattern 以 ^ 开头或以 $ 结尾或含 .*
	if startsWithAnchor(pattern) || endsWithAnchor(pattern) || containsDotStar(pattern) {
		matched, err := regexp.MatchString(pattern, query)
		if err != nil {
			return false
		}
		return matched
	}
	return false
}

// allHookEventValues 返回所有 HookEvent 常量值的有序列表
func allHookEventValues() []string {
	return []string{
		HookEventPreToolUse, HookEventPostToolUse, HookEventPostToolUseFailure,
		HookEventStop, HookEventUserPromptSubmit, HookEventSessionStart,
		HookEventSessionEnd, HookEventNotification, HookEventPermissionRequest,
		HookEventPermissionDenied, HookEventSubagentStart, HookEventSubagentStop,
		HookEventConfigChange, HookEventInstructionsLoaded, HookEventSetup,
		HookEventBeforeModelCall, HookEventAfterModelCall,
	}
}

// containsPipe 检查字符串是否包含 |
func containsPipe(s string) bool { return containsChar(s, '|') }

// startsWithAnchor 检查字符串是否以 ^ 开头
func startsWithAnchor(s string) bool { return len(s) > 0 && s[0] == '^' }

// endsWithAnchor 检查字符串是否以 $ 结尾
func endsWithAnchor(s string) bool { return len(s) > 0 && s[len(s)-1] == '$' }

// containsDotStar 检查字符串是否含 .*
func containsDotStar(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '.' && s[i+1] == '*' {
			return true
		}
	}
	return false
}

// containsChar 检查字符串是否包含指定字符
func containsChar(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

// splitByPipe 按 | 分割字符串
func splitByPipe(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '|' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// trimSpace 去除两端空白
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && s[start] == ' ' {
		start++
	}
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}

// toBool 将 any 转为 bool
func toBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// fmtHookMatcher 用于调试的格式化辅助
var _ = fmt.Sprintf // 确保 fmt import 不被移除
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -tags=!integration,!llm,!e2e ./internal/common/hooks/... -v`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 5: 提交**

```bash
git add internal/common/hooks/config.go internal/common/hooks/config_test.go
git commit -m "feat(10.3.23.2): 添加 CommandHookConfig/PromptHookConfig/HookMatcher/HooksConfig/LoadHooksConfig"
```

---

### Task 3: server/hooks — HookExecutor（command/prompt）+ ParseCommandOutput + ExtractJSONFromResponse

**Files:**
- Create: `internal/swarm/server/hooks/doc.go`
- Create: `internal/swarm/server/hooks/executor.go`
- Test: `internal/swarm/server/hooks/executor_test.go`

- [ ] **Step 1: 写失败的测试**

`internal/swarm/server/hooks/executor_test.go` — 测试 ParseCommandOutput + ExtractJSONFromResponse + HookExecutor:

```go
package hooks

import (
	"context"
	"testing"
	"time"
)

// TestHookOutcome_常量值 测试 HookOutcome 对齐 Python
func TestHookOutcome_常量值(t *testing.T) {
	if HookOutcomeSuccess != "success" {
		t.Errorf("HookOutcomeSuccess = %q, want %q", HookOutcomeSuccess, "success")
	}
	if HookOutcomeBlocking != "blocking" {
		t.Errorf("HookOutcomeBlocking = %q, want %q", HookOutcomeBlocking, "blocking")
	}
	if HookOutcomeNonBlockingError != "non_blocking_error" {
		t.Errorf("HookOutcomeNonBlockingError = %q, want %q", HookOutcomeNonBlockingError, "non_blocking_error")
	}
}

// TestParseCommandOutput_空输出 测试 stdout 空时返回 SUCCESS
func TestParseCommandOutput_空输出(t *testing.T) {
	result := ParseCommandOutput("")
	if result.Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", result.Outcome, HookOutcomeSuccess)
	}
}

// TestParseCommandOutput_有效JSON_阻塞 测试 decision=block
func TestParseCommandOutput_有效JSON_阻塞(t *testing.T) {
	result := ParseCommandOutput(`{"decision": "block", "reason": "dangerous tool"}`)
	if result.Outcome != HookOutcomeBlocking {
		t.Errorf("Outcome = %q, want %q", result.Outcome, HookOutcomeBlocking)
	}
	if result.Error != "dangerous tool" {
		t.Errorf("Error = %q, want %q", result.Error, "dangerous tool")
	}
	if !result.ShowToModel {
		t.Error("ShowToModel should be true for blocking")
	}
}

// TestParseCommandOutput_有效JSON_修改输入 测试 modifiedInput + additionalContext
func TestParseCommandOutput_有效JSON_修改输入(t *testing.T) {
	result := ParseCommandOutput(`{"decision": "allow", "modifiedInput": {"path": "/safe"}, "additionalContext": "context info"}`)
	if result.Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", result.Outcome, HookOutcomeSuccess)
	}
	if result.ModifiedInput["path"] != "/safe" {
		t.Errorf("ModifiedInput[path] = %v, want %q", result.ModifiedInput["path"], "/safe")
	}
	if result.AdditionalContext != "context info" {
		t.Errorf("AdditionalContext = %q, want %q", result.AdditionalContext, "context info")
	}
}

// TestParseCommandOutput_有效JSON_reason 测试 reason 在 non-block 时存入 additionalContext
func TestParseCommandOutput_有效JSON_reason(t *testing.T) {
	result := ParseCommandOutput(`{"decision": "allow", "reason": "approved"}`)
	if result.AdditionalContext != "approved" {
		t.Errorf("AdditionalContext = %q, want %q", result.AdditionalContext, "approved")
	}
}

// TestParseCommandOutput_非JSON 测试非 JSON 输出返回 SUCCESS
func TestParseCommandOutput_非JSON(t *testing.T) {
	result := ParseCommandOutput("just some text output")
	if result.Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q for non-JSON", result.Outcome, HookOutcomeSuccess)
	}
}

// TestParseCommandOutput_非dictJSON 测试非 dict JSON 返回 SUCCESS
func TestParseCommandOutput_非dictJSON(t *testing.T) {
	result := ParseCommandOutput(`[1, 2, 3]`)
	if result.Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q for non-dict JSON", result.Outcome, HookOutcomeSuccess)
	}
}

// TestExtractJSONFromResponse_直接JSON 测试直接 JSON 解析
func TestExtractJSONFromResponse_直接JSON(t *testing.T) {
	data := ExtractJSONFromResponse(`{"decision": "block", "reason": "test"}`)
	if data["decision"] != "block" {
		t.Errorf("decision = %v, want %q", data["decision"], "block")
	}
}

// TestExtractJSONFromResponse_markdownFence 测试 markdown fence 提取
func TestExtractJSONFromResponse_markdownFence(t *testing.T) {
	text := "Here is the result:\n```json\n{\"decision\": \"allow\"}\n```"
	data := ExtractJSONFromResponse(text)
	if data["decision"] != "allow" {
		t.Errorf("decision = %v, want %q", data["decision"], "allow")
	}
}

// TestExtractJSONFromResponse_嵌入式JSON 测试嵌入式 { } 提取
func TestExtractJSONFromResponse_嵌入式JSON(t *testing.T) {
	text := "The LLM responded with {\"decision\": \"block\"} as the answer."
	data := ExtractJSONFromResponse(text)
	if data["decision"] != "block" {
		t.Errorf("decision = %v, want %q", data["decision"], "block")
	}
}

// TestExtractJSONFromResponse_空文本 测试空文本返回空 map
func TestExtractJSONFromResponse_空文本(t *testing.T) {
	data := ExtractJSONFromResponse("")
	if len(data) != 0 {
		t.Errorf("empty text should return empty map, got %d keys", len(data))
	}
}

// TestExtractJSONFromResponse_无JSON 测试不含 JSON 返回空 map
func TestExtractJSONFromResponse_无JSON(t *testing.T) {
	data := ExtractJSONFromResponse("just plain text, no json here")
	if len(data) != 0 {
		t.Errorf("no JSON text should return empty map, got %d keys", len(data))
	}
}

// TestHookExecutor_RunAll_空配置 测试空 hook 配置返回空列表
func TestHookExecutor_RunAll_空配置(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	results := exec.RunAll(context.Background(), nil, map[string]any{}, "")
	if len(results) != 0 {
		t.Errorf("RunAll(nil) = %d results, want 0", len(results))
	}
}

// TestHookExecutor_RunAll_command成功 测试 command hook exit 0
func TestHookExecutor_RunAll_command成功(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "command", "command": "echo '{\"decision\": \"allow\"}'", "timeout": 10},
	}
	hookInput := map[string]any{"tool_name": "test_tool"}
	results := exec.RunAll(context.Background(), hookConfigs, hookInput, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeSuccess)
	}
}

// TestHookExecutor_RunAll_command阻塞 测试 command hook exit 2（阻塞）
func TestHookExecutor_RunAll_command阻塞(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	// exit 2 的命令
	hookConfigs := []map[string]any{
		{"type": "command", "command": "echo '{\"decision\": \"block\", \"reason\": \"blocked\"}' && exit 2", "timeout": 10},
	}
	hookInput := map[string]any{"tool_name": "test_tool"}
	results := exec.RunAll(context.Background(), hookConfigs, hookInput, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeBlocking {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeBlocking)
	}
}

// TestHookExecutor_RunAll_command空命令 测试空 command 返回 NON_BLOCKING_ERROR
func TestHookExecutor_RunAll_command空命令(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "command", "command": "", "timeout": 10},
	}
	results := exec.RunAll(context.Background(), hookConfigs, map[string]any{}, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeNonBlockingError {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeNonBlockingError)
	}
}

// TestHookExecutor_RunAll_command超时 测试超时返回 NON_BLOCKING_ERROR
func TestHookExecutor_RunAll_command超时(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "command", "command": "sleep 30", "timeout": 1},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := exec.RunAll(ctx, hookConfigs, map[string]any{}, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeNonBlockingError {
		t.Errorf("Outcome = %q, want %q (timeout)", results[0].Outcome, HookOutcomeNonBlockingError)
	}
}

// TestHookExecutor_RunAll_prompt空模板 测试空 prompt 返回 NON_BLOCKING_ERROR
func TestHookExecutor_RunAll_prompt空模板(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "prompt", "prompt": "", "timeout": 10},
	}
	results := exec.RunAll(context.Background(), hookConfigs, map[string]any{}, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeNonBlockingError {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeNonBlockingError)
	}
}

// TestHookExecutor_RunAll_未知类型 测试未知 hook 类型跳过
func TestHookExecutor_RunAll_未知类型(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "unknown", "command": "echo test"},
	}
	results := exec.RunAll(context.Background(), hookConfigs, map[string]any{}, "")
	if len(results) != 0 {
		t.Errorf("unknown type should produce 0 results, got %d", len(results))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -tags=!integration,!llm,!e2e ./internal/swarm/server/hooks/... -v 2>&1 | head -20`
Expected: FAIL（包不存在）

- [ ] **Step 3: 写 doc.go + executor.go 实现**

`internal/swarm/server/hooks/doc.go`:
```go
// Package hooks（server/hooks）提供 Hook 执行引擎和 UserHookRail，对齐 Python jiuwenswarm/server/hooks/。
//
// 本包包含：
//   - HookExecutor：统一调度 command/prompt 两类 hook，返回 HookResult
//   - UserHookRail：以 Rail 形态拦截工具调用和 Agent 生命周期
//
// 配置模型定义在上层 common/hooks 包（HookType/HookEvent/HooksConfig/LoadHooksConfig）。
//
// 文件目录：
//
//	hooks/
//	├── doc.go            # 包文档
//	├── executor.go       # HookOutcome/HookResult/LLMConfig + HookExecutor + ParseCommandOutput + ExtractJSONFromResponse
//	└── user_hook_rail.go # UserHookRail(embed DeepAgentRail) 4 个钩子方法
//
// 对应 Python 代码：jiuwenswarm/server/hooks/executor.py + user_hook_rail.py
package hooks
```

`internal/swarm/server/hooks/executor.go` — 完整实现（对齐 Python executor.py，约 250 行）：

```go
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// HookOutcome hook 执行结果类型，对齐 Python HookOutcome
const (
	// HookOutcomeSuccess 执行成功，对齐 Python HookOutcome.SUCCESS
	HookOutcomeSuccess = "success"
	// HookOutcomeBlocking 阻塞执行，对齐 Python HookOutcome.BLOCKING
	HookOutcomeBlocking = "blocking"
	// HookOutcomeNonBlockingError 非阻塞错误，对齐 Python HookOutcome.NON_BLOCKING_ERROR
	HookOutcomeNonBlockingError = "non_blocking_error"
)

// logComponent 日志组件标识
const logComponent = logger.ComponentCommon

// ──────────────────────────── 结构体 ────────────────────────────

// HookResult hook 执行结果，对齐 Python HookResult dataclass
type HookResult struct {
	// Outcome 执行结果类型（success/blocking/non_blocking_error）
	Outcome string
	// Error 错误/拦截原因
	Error string
	// ShowToModel 是否展示给模型（blocking 时为 true）
	ShowToModel bool
	// ModifiedInput 修改后的输入（由 hook 修改）
	ModifiedInput map[string]any
	// AdditionalContext 附加上下文
	AdditionalContext string
}

// LLMConfig prompt hook 使用的 LLM 配置
// 对齐 Python _query_llm 中从 config 提取的 APIKey/APIBase/ClientProvider/DefaultModel
type LLMConfig struct {
	// APIKey LLM API 密钥
	APIKey string
	// APIBase LLM API 地址
	APIBase string
	// ClientProvider LLM 客户端提供者
	ClientProvider string
	// DefaultModel 默认模型名
	DefaultModel string
}

// HookExecutor hook 执行器，对齐 Python HookExecutor
// 统一调度 command/prompt 两类 hook，返回 HookResult 列表
type HookExecutor struct {
	// llmConfig prompt hook 使用的 LLM 配置
	llmConfig LLMConfig
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHookExecutor 创建 HookExecutor，对齐 Python HookExecutor()
func NewHookExecutor(llmConfig LLMConfig) *HookExecutor {
	return &HookExecutor{llmConfig: llmConfig}
}

// RunAll 并行执行同一 matcher 下的所有 hooks，对齐 Python HookExecutor.run_all()
// Go 中使用 goroutine + WaitGroup 实现并发（等价 asyncio.gather）
func (e *HookExecutor) RunAll(ctx context.Context, hookConfigs []map[string]any, hookInput map[string]any, sessionID string) []HookResult {
	if len(hookConfigs) == 0 {
		return nil
	}

	results := make([]HookResult, len(hookConfigs))
	var wg sync.WaitGroup

	for i, cfg := range hookConfigs {
		wg.Add(1)
		go func(idx int, c map[string]any) {
			defer wg.Done()
			hookType, _ := c["type"].(string)
			if hookType == string(hookscfg.HookTypeCommand) || hookType == "" {
				// 默认类型为 command
				results[idx] = e.runCommandHook(ctx, c, hookInput)
			} else if hookType == string(hookscfg.HookTypePrompt) {
				results[idx] = e.runPromptHook(ctx, c, hookInput)
			}
			// 未知类型：不设置 results[idx]，保持零值 HookResult{Outcome:""}
		}(i, cfg)
	}
	wg.Wait()

	// 将异常结果（outcome 为空）替换为 NON_BLOCKING_ERROR
	for i, r := range results {
		if r.Outcome == "" {
			results[i] = HookResult{Outcome: HookOutcomeNonBlockingError, Error: "unknown hook type"}
		}
	}
	return results
}

// ParseCommandOutput 解析 command hook 的 stdout JSON 协议
// 对齐 Python HookExecutor.parse_command_output（静态方法）
func ParseCommandOutput(stdout string) HookResult {
	if strings.TrimSpace(stdout) == "" {
		return HookResult{Outcome: HookOutcomeSuccess}
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &data); err != nil {
		return HookResult{Outcome: HookOutcomeSuccess}
	}

	if _, ok := data["decision"]; !ok {
		// 非 dict 或无 decision 字段 → SUCCESS
		return HookResult{Outcome: HookOutcomeSuccess}
	}

	decision, _ := data["decision"].(string)
	if decision == "block" {
		reason := "blocked by hook"
		if v, ok := data["reason"].(string); ok && v != "" {
			reason = v
		}
		return HookResult{
			Outcome:     HookOutcomeBlocking,
			Error:       reason,
			ShowToModel: true,
		}
	}

	result := HookResult{Outcome: HookOutcomeSuccess}
	if v, ok := data["modifiedInput"]; ok {
		if m, ok := v.(map[string]any); ok {
			result.ModifiedInput = m
		}
	}
	if v, ok := data["additionalContext"]; ok {
		if s, ok := v.(string); ok {
			result.AdditionalContext = s
		}
	}
	if v, ok := data["reason"].(string); ok && decision != "block" {
		if result.AdditionalContext == "" {
			result.AdditionalContext = v
		}
	}
	return result
}

// ExtractJSONFromResponse 从 LLM 响应中提取 JSON 对象
// 对齐 Python HookExecutor.extract_json_from_response（静态方法）
func ExtractJSONFromResponse(text string) map[string]any {
	if text == "" {
		return map[string]any{}
	}
	text = strings.TrimSpace(text)

	// 1. 直接 JSON 解析
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err == nil {
		return data
	}

	// 2. markdown fence ```json``` 提取
	re := regexp.MustCompile("```(?:json)?\\s*([\\s\\S]*?)```")
	if match := re.FindStringSubmatch(text); len(match) > 1 {
		var fenceData map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(match[1])), &fenceData); err == nil {
			return fenceData
		}
	}

	// 3. 嵌入式 { ... } 提取
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		var embedData map[string]any
		if err := json.Unmarshal([]byte(text[start:end+1]), &embedData); err == nil {
			return embedData
		}
	}

	return map[string]any{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runCommandHook 执行 command 类型 hook（子进程），对齐 Python _run_command_hook
func (e *HookExecutor) runCommandHook(ctx context.Context, config map[string]any, hookInput map[string]any) HookResult {
	command, _ := config["command"].(string)
	if command == "" {
		return HookResult{Outcome: HookOutcomeNonBlockingError, Error: "empty command"}
	}

	// 默认值对齐 Python
	timeout := 30
	if v, ok := config["timeout"]; ok {
		switch n := v.(type) {
		case int:
			timeout = n
		case float64:
			timeout = int(n)
		}
	}
	shell := "bash"
	if v, ok := config["shell"].(string); ok && v != "" {
		shell = v
	}

	hookInputJSON, err := json.Marshal(hookInput)
	if err != nil {
		return HookResult{Outcome: HookOutcomeNonBlockingError, Error: fmt.Sprintf("serialize hook input: %v", err)}
	}
	toolName, _ := hookInput["tool_name"].(string)

	// 设置环境变量，对齐 Python: env["ARGUMENTS"] = hook_input_json; env["TOOL_NAME"] = tool_name
	env := os.Environ()
	env = append(env, fmt.Sprintf("ARGUMENTS=%s", string(hookInputJSON)))
	env = append(env, fmt.Sprintf("TOOL_NAME=%s", toolName))

	// 创建子进程，对齐 Python: asyncio.create_subprocess_exec(shell, "-c", command, ...)
	cmd := exec.Command(shell, "-c", command)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(string(hookInputJSON))

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// 带超时执行
	doneCh := make(chan error, 1)
	go func() { doneCh <- cmd.Run() }()

	select {
	case <-time.After(time.Duration(timeout) * time.Second):
		// 超时 → kill 进程，对齐 Python: proc.kill() + proc.wait()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		logger.Debug(logComponent).Int("timeout", timeout).Str("command", command).Msg("hook 子进程超时，已 kill")
		return HookResult{Outcome: HookOutcomeNonBlockingError, Error: fmt.Sprintf("hook timeout after %ds", timeout)}
	case runErr := <-doneCh:
		if runErr != nil {
			// 进程启动失败等异常
			if cmd.ProcessState == nil {
				return HookResult{Outcome: HookOutcomeNonBlockingError, Error: runErr.Error()}
			}
		}
	}

	returnCode := -1
	if cmd.ProcessState != nil {
		returnCode = cmd.ProcessState.ExitCode()
	}

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	if returnCode == 0 {
		return ParseCommandOutput(stdout)
	}

	if returnCode == 2 {
		// exit 2 = blocking，对齐 Python
		parsed := ParseCommandOutput(stdout)
		reason := ""
		if parsed.Outcome == HookOutcomeBlocking {
			reason = parsed.Error
		} else if parsed.AdditionalContext != "" {
			reason = parsed.AdditionalContext
		}
		if reason == "" {
			reason = strings.TrimSpace(stderr)
			if reason == "" {
				reason = "hook blocked execution"
			}
		}
		return HookResult{
			Outcome:     HookOutcomeBlocking,
			Error:       reason,
			ShowToModel: true,
		}
	}

	// 其他退出码 → non_blocking_error，对齐 Python
	errMsg := strings.TrimSpace(stderr)
	if errMsg == "" {
		errMsg = fmt.Sprintf("exit code %d", returnCode)
	}
	return HookResult{Outcome: HookOutcomeNonBlockingError, Error: errMsg}
}

// runPromptHook 执行 prompt 类型 hook（LLM 审核），对齐 Python _run_prompt_hook
func (e *HookExecutor) runPromptHook(ctx context.Context, config map[string]any, hookInput map[string]any) HookResult {
	promptTemplate, _ := config["prompt"].(string)
	if promptTemplate == "" {
		return HookResult{Outcome: HookOutcomeNonBlockingError, Error: "empty prompt"}
	}

	timeout := 15
	if v, ok := config["timeout"]; ok {
		switch n := v.(type) {
		case int:
			timeout = n
		case float64:
			timeout = int(n)
		}
	}
	modelName, _ := config["model"].(string)

	// 模板替换，对齐 Python: $ARGUMENTS + $TOOL_NAME
	hookInputJSON, _ := json.Marshal(hookInput)
	toolName, _ := hookInput["tool_name"].(string)
	finalPrompt := strings.ReplaceAll(promptTemplate, "$ARGUMENTS", string(hookInputJSON))
	finalPrompt = strings.ReplaceAll(finalPrompt, "$TOOL_NAME", toolName)

	// 带超时调用 LLM，对齐 Python: asyncio.wait_for(self._query_llm(prompt, model), timeout=timeout)
	type llmResult struct {
		text string
		err  error
	}
	resultCh := make(chan llmResult, 1)
	go func() {
		text, err := e.queryLLM(ctx, finalPrompt, modelName)
		resultCh <- llmResult{text, err}
	}()

	var result llmResult
	select {
	case <-time.After(time.Duration(timeout) * time.Second):
		return HookResult{Outcome: HookOutcomeNonBlockingError, Error: fmt.Sprintf("prompt hook timeout after %ds", timeout)}
	case result = <-resultCh:
		if result.err != nil {
			return HookResult{Outcome: HookOutcomeNonBlockingError, Error: result.err.Error()}
		}
	}

	data := ExtractJSONFromResponse(result.text)
	decision, _ := data["decision"].(string)
	if decision == "" {
		decision = "allow" // 默认允许，对齐 Python
	}

	if decision == "block" {
		reason, _ := data["reason"].(string)
		if reason == "" {
			reason = "blocked by prompt hook"
		}
		return HookResult{
			Outcome:     HookOutcomeBlocking,
			Error:       reason,
			ShowToModel: true,
		}
	}

	r := HookResult{Outcome: HookOutcomeSuccess}
	if v, ok := data["modifiedInput"]; ok {
		if m, ok := v.(map[string]any); ok {
			r.ModifiedInput = m
		}
	}
	if v, ok := data["additionalContext"]; ok {
		if s, ok := v.(string); ok {
			r.AdditionalContext = s
		}
	}
	return r
}

// queryLLM 调用 LLM 执行 hook 审查，对齐 Python _query_llm
// 内部用 LLMConfig 创建 Model 实例（对齐 Python 动态 import config + 创建 Model）
func (e *HookExecutor) queryLLM(ctx context.Context, prompt, modelName string) (string, error) {
	clientConfig := llmschema.NewModelClientConfig(e.llmConfig.ClientProvider, e.llmConfig.APIKey, e.llmConfig.APIBase)
	model, err := llm.NewModel(clientConfig, nil)
	if err != nil {
		return "", fmt.Errorf("创建 Model 失败: %w", err)
	}

	effectiveModel := modelName
	if effectiveModel == "" {
		effectiveModel = e.llmConfig.DefaultModel
	}

	messages := []llmschema.BaseMessage{
		llmschema.NewUserMessage(prompt),
	}
	opts := []llm.InvokeOption{
		llm.WithTemperature(0.0),
		llm.WithMaxTokens(1024),
		llm.WithModel(effectiveModel),
	}

	response, err := model.Invoke(ctx, messages, opts...)
	if err != nil {
		return "", fmt.Errorf("LLM Invoke 失败: %w", err)
	}

	// 对齐 Python: 处理 response.content（str 或 list）
	content := response.Content
	if contentStr, ok := content.(string); ok {
		return contentStr, nil
	}
	if contentList, ok := content.([]any); ok {
		var parts []string
		for _, block := range contentList {
			if blockMap, ok := block.(map[string]any); ok {
				if text, ok := blockMap["text"].(string); ok {
					parts = append(parts, text)
				}
			} else if text, ok := block.(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n"), nil
	}
	return fmt.Sprintf("%v", content), nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -tags=!integration,!llm,!e2e ./internal/swarm/server/hooks/... -v`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/hooks/doc.go internal/swarm/server/hooks/executor.go internal/swarm/server/hooks/executor_test.go
git commit -m "feat(10.3.23.3): 添加 HookOutcome/HookResult + HookExecutor(command/prompt) + ParseCommandOutput + ExtractJSONFromResponse"
```

---

### Task 4: server/hooks — UserHookRail

**Files:**
- Create: `internal/swarm/server/hooks/user_hook_rail.go`
- Test: `internal/swarm/server/hooks/user_hook_rail_test.go`

- [ ] **Step 1: 写失败的测试**

```go
package hooks

import (
	"context"
	"testing"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// TestNewUserHookRail 测试构造
func TestNewUserHookRail(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPreToolUse: {{Matcher: "*"}},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)
	if rail == nil {
		t.Error("NewUserHookRail() = nil, want non-nil")
	}
}

// TestUserHookRail_BeforeToolCall_阻塞 测试 blocking 设置 _skip_tool + _hook_feedback
func TestUserHookRail_BeforeToolCall_阻塞(t *testing.T) {
	// 配置一个返回 blocking 的 command hook
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPreToolUse: {
			{
				Matcher: "*",
				Hooks:   []map[string]any{{"type": "command", "command": "echo '{\"decision\": \"block\", \"reason\": \"dangerous\"}' && exit 0"}},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil, nil)
	// AgentCallbackContext.Extra() 返回 map[string]any，可直接写入
	// Inputs 通过 SetInputs 设置

	err := rail.BeforeToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeToolCall error: %v", err)
	}
	if cbc.Extra()["_skip_tool"] != true {
		t.Error("_skip_tool should be true after blocking")
	}
	if cbc.Extra()["_hook_feedback"] != "dangerous" {
		t.Errorf("_hook_feedback = %v, want %q", cbc.Extra()["_hook_feedback"], "dangerous")
	}
}

// TestUserHookRail_BeforeToolCall_无匹配 测试无匹配事件时直接返回 nil
func TestUserHookRail_BeforeToolCall_无匹配(t *testing.T) {
	cfg := hookscfg.HooksConfig{} // 空 Events
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil, nil)
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{ToolName: "any_tool", ToolArgs: "{}"})

	err := rail.BeforeToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeToolCall with no hooks should return nil, got: %v", err)
	}
}

// TestUserHookRail_AfterToolCall_附加上下文 测试 additionalContext 拼接到 ToolResult
func TestUserHookRail_AfterToolCall_附加上下文(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPostToolUse: {
			{
				Matcher: "*",
				Hooks:   []map[string]any{{"type": "command", "command": "echo '{\"decision\": \"allow\", \"additionalContext\": \"extra info\"}'"}},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil, nil)
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: "{}", ToolResult: "original result"})

	err := rail.AfterToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("AfterToolCall error: %v", err)
	}
	inputs := cbc.Inputs.(*agentinterfaces.ToolCallInputs)
	if !strings.Contains(inputs.ToolResult, "extra info") {
		t.Errorf("ToolResult should contain 'extra info', got: %q", inputs.ToolResult)
	}
}
```

注意：`AgentCallbackContext` 的 `SetExtraMap` 和 `SetInputs` 方法需要在实际实现中确认是否存在。根据探索结果，`cbc.extra` 是 `map[string]any` 字段，`cbc.inputs` 是 `EventInputs` 接口。测试中需要通过正确的方式设置这些字段。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -tags=!integration,!llm,!e2e ./internal/swarm/server/hooks/... -v -run "TestUserHookRail" 2>&1 | head -20`
Expected: FAIL

- [ ] **Step 3: 写 user_hook_rail.go 实现**

```go
package hooks

import (
	"context"
	"fmt"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UserHookRail 用户配置的 hooks 执行引擎，对齐 Python UserHookRail(DeepAgentRail, priority=60)
// 将用户配置的 hooks 以 Rail 形态注册到 DeepAgent，拦截工具调用和 Agent 生命周期
// Priority=60: 在 SecurityRail(80) 之后，JiuClawStreamEventRail(50) 之前
type UserHookRail struct {
	rails.DeepAgentRail
	// config hooks 配置
	config hookscfg.HooksConfig
	// executor hook 执行器
	executor *HookExecutor
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewUserHookRail 创建 UserHookRail，对齐 Python UserHookRail.__init__(hooks_config)
func NewUserHookRail(config hookscfg.HooksConfig, executor *HookExecutor) *UserHookRail {
	r := &UserHookRail{
		config:   config,
		executor: executor,
	}
	r.DeepAgentRail = *rails.NewDeepAgentRail().WithPriority(60)
	return r
}

// BeforeToolCall 对齐 Python before_tool_call → HookEvent.PRE_TOOL_USE
// blocking → cbc.Extra["_skip_tool"]=true, cbc.Extra["_hook_feedback"]=error
// modifiedInput → 修改 cbc.Inputs.ToolArgs/ToolName
// additionalContext → cbc.Extra["_hook_additional_context"] 追加
func (r *UserHookRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	toolInputs := cbc.Inputs.(*agentinterfaces.ToolCallInputs)
	toolName := toolInputs.ToolName

	hookConfigs := r.config.Match(hookscfg.HookEventPreToolUse, toolName)
	if len(hookConfigs) == 0 {
		return nil
	}

	sessionID := getSessionID(cbc)
	hookInput := map[string]any{
		"event":      "PreToolUse",
		"tool_name":  toolName,
		"tool_input": toolInputs.ToolArgs,
		"session_id": sessionID,
	}

	results := r.executor.RunAll(ctx, hookConfigs, hookInput, sessionID)

	for _, result := range results {
		if result.Outcome == HookOutcomeBlocking {
			cbc.Extra()["_skip_tool"] = true
			cbc.Extra()["_hook_feedback"] = result.Error
			logger.Info(logComponent).Str("tool_name", toolName).Str("reason", result.Error).Msg("UserHookRail: PreToolUse BLOCKED")
			return nil
		}
		if result.ModifiedInput != nil {
			// 对齐 Python: ctx.inputs.tool_args = r.modified_input
			if modifiedArgs, ok := result.ModifiedInput["tool_args"]; ok {
				if s, ok := modifiedArgs.(string); ok {
					toolInputs.ToolArgs = s
				}
			}
			if newName, ok := result.ModifiedInput["_tool_name"]; ok {
				if s, ok := newName.(string); ok && s != "" {
					toolInputs.ToolName = s
					logger.Info(logComponent).Str("tool_name", toolName).Str("new_name", s).Msg("UserHookRail: PreToolUse modified tool name")
				}
			}
			logger.Info(logComponent).Str("tool_name", toolName).Msg("UserHookRail: PreToolUse modified input")
		}
		if result.AdditionalContext != "" {
			existing, _ := cbc.Extra()["_hook_additional_context"].(string)
			if existing != "" {
				cbc.Extra()["_hook_additional_context"] = existing + "\n" + result.AdditionalContext
			} else {
				cbc.Extra()["_hook_additional_context"] = result.AdditionalContext
			}
		}
	}
	return nil
}

// AfterToolCall 对齐 Python after_tool_call → HookEvent.POST_TOOL_USE
// blocking → cbc.Extra["_post_tool_hook_feedback"]=error
// additionalContext → 拼接到 cbc.Inputs.ToolResult
func (r *UserHookRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	toolInputs := cbc.Inputs.(*agentinterfaces.ToolCallInputs)
	toolName := toolInputs.ToolName

	hookConfigs := r.config.Match(hookscfg.HookEventPostToolUse, toolName)
	if len(hookConfigs) == 0 {
		return nil
	}

	sessionID := getSessionID(cbc)
	hookInput := map[string]any{
		"event":       "PostToolUse",
		"tool_name":   toolName,
		"tool_input":  toolInputs.ToolArgs,
		"tool_result": toolInputs.ToolResult,
		"session_id":  sessionID,
	}

	results := r.executor.RunAll(ctx, hookConfigs, hookInput, sessionID)

	for _, result := range results {
		if result.Outcome == HookOutcomeBlocking {
			cbc.Extra()["_post_tool_hook_feedback"] = result.Error
			logger.Info(logComponent).Str("tool_name", toolName).Str("reason", result.Error).Msg("UserHookRail: PostToolUse BLOCKED")
		}
		if result.AdditionalContext != "" {
			current := fmt.Sprintf("%v", toolInputs.ToolResult)
			if current != "" {
				toolInputs.ToolResult = current + "\n[Hook 发现]: " + result.AdditionalContext
			} else {
				toolInputs.ToolResult = "[Hook 发现]: " + result.AdditionalContext
			}
		}
	}
	return nil
}

// OnToolException 对齐 Python on_tool_exception → HookEvent.POST_TOOL_USE_FAILURE
// 仅通知收集（不改变处理流程）
func (r *UserHookRail) OnToolException(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	toolInputs := cbc.Inputs.(*agentinterfaces.ToolCallInputs)
	toolName := toolInputs.ToolName

	hookConfigs := r.config.Match(hookscfg.HookEventPostToolUseFailure, toolName)
	if len(hookConfigs) == 0 {
		return nil
	}

	sessionID := getSessionID(cbc)
	hookInput := map[string]any{
		"event":      "PostToolUseFailure",
		"tool_name":  toolName,
		"tool_input": toolInputs.ToolArgs,
		"error":      fmt.Sprintf("%v", cbc.Exception()),
		"session_id": sessionID,
	}

	// 仅执行，不改变流程，对齐 Python
	r.executor.RunAll(ctx, hookConfigs, hookInput, sessionID)
	return nil
}

// AfterInvoke 对齐 Python after_invoke → HookEvent.STOP
// blocking → cbc.Extra["_stop_hook_feedback"]=error
func (r *UserHookRail) AfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	hookConfigs := r.config.Match(hookscfg.HookEventStop, "")
	if len(hookConfigs) == 0 {
		return nil
	}

	sessionID := getSessionID(cbc)
	var finalResponse any
	if invokeInputs, ok := cbc.Inputs.(*agentinterfaces.InvokeInputs); ok {
		finalResponse = invokeInputs.Result
	}

	hookInput := map[string]any{
		"event":         "Stop",
		"final_response": finalResponse,
		"session_id":    sessionID,
	}

	results := r.executor.RunAll(ctx, hookConfigs, hookInput, sessionID)

	for _, result := range results {
		if result.Outcome == HookOutcomeBlocking {
			cbc.Extra()["_stop_hook_feedback"] = result.Error
			reason := result.Error
			if len(reason) > 200 {
				reason = reason[:200]
			}
			logger.Info(logComponent).Str("reason", reason).Msg("UserHookRail: Stop hook feedback")
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getSessionID 从 cbc 获取 session_id
func getSessionID(cbc *agentinterfaces.AgentCallbackContext) string {
	if cbc.Session() != nil {
		return cbc.Session().GetSessionID()
	}
	return ""
}
```

注意：`SetPriority` 方法需要确认在 BaseRail/DeepAgentRail 中是否存在。如果不存在，UserHookRail 需要通过其他方式设置 priority=60。`cbc.Exception()` 方法名也需要确认。`cbc.Session()` 方法也需要确认。

实际实现时需要根据 `AgentCallbackContext` 的真实方法名调整。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -tags=!integration,!llm,!e2e ./internal/swarm/server/hooks/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/hooks/user_hook_rail.go internal/swarm/server/hooks/user_hook_rail_test.go
git commit -m "feat(10.3.23.4): 添加 UserHookRail(embed DeepAgentRail) BeforeToolCall/AfterToolCall/OnToolException/AfterInvoke"
```

---

### Task 5: 回填 — DeepAdapter 注册 + handleHooksList

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`
- Modify: `internal/swarm/server/handle_extensions.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 在 deep_adapter_rails.go 中添加 UserHookRail 注册步骤**

在 `buildAgentRails` 方法末尾（步骤 20 permissionRail 之后）添加步骤 21：

```go
// 步骤 21: userHookRail — 用户配置的 hooks，对齐 Python interface_deep.py L2200-2211
hooksCfg := hookscfg.LoadHooksConfig(configBase)
if len(hooksCfg.Events) > 0 {
    // 从 configBase 提取 LLM 配置
    modelsCfg, _ := configBase["models"].(map[string]any)
    defaultCfg, _ := modelsCfg["default"].(map[string]any)
    clientCfg, _ := defaultCfg["model_client_config"].(map[string]any)
    llmCfg := hookscfg.LLMConfig{
        APIKey:         strFromMap(clientCfg, "api_key"),
        APIBase:        strFromMap(clientCfg, "api_base"),
        ClientProvider: strFromMap(clientCfg, "client_provider"),
        DefaultModel:   strFromMap(clientCfg, "model_name"),
    }
    hookExec := serverhooks.NewHookExecutor(llmCfg)
    userHookRail := serverhooks.NewUserHookRail(*hooksCfg, hookExec)
    railsList = append(railsList, userHookRail)
    logger.Info(logComponent).Int("event_types", len(hooksCfg.Events)).Msg("UserHookRail 加载完成")
}
```

需要在 deep_adapter_rails.go 添加 import：
- `hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"`
- `serverhooks "github.com/uapclaw/uapclaw-go/internal/swarm/server/hooks"`

也需要在 deep_adapter_rails.go 或其辅助文件中添加 `strFromMap` 工具函数（如果不存在）。

- [ ] **Step 2: 更新 handleHooksList**

将 `handle_extensions.go` 的 `handleHooksList` 从空列表改为真实实现：

```go
func (s *AgentServer) handleHooksList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
    configBase, _ := s.config.Load()
    hooksCfg := hookscfg.LoadHooksConfig(configBase)
    summary := hooksCfg.GetEventSummary()
    return schema.NewAgentResponse(request.RequestID, request.ChannelID,
        schema.WithPayload(map[string]any{"hooks": summary}),
    ), nil
}
```

需要在 handle_extensions.go 添加 import `hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"`。

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将行 678 的 `10.3.23-26 ☐ 服务端辅助` 替换为：

```
| 10.3.23.1 | ✅ | HookType/HookEvent 常量 | AgentRailEvents/GatewayEvents | `hooks_config.py` |
| 10.3.23.2 | ✅ | 配置模型 | CommandHookConfig/PromptHookConfig/HookMatcher/HooksConfig/LoadHooksConfig | `hooks_config.py` |
| 10.3.23.3 | ✅ | HookExecutor | command/prompt 两类 hook + ParseCommandOutput + ExtractJSONFromResponse | `executor.py` |
| 10.3.23.4 | ✅ | UserHookRail | embed DeepAgentRail + BeforeToolCall/AfterToolCall/OnToolException/AfterInvoke | `user_hook_rail.py` |
| 10.3.23.5 | ✅ | 回填注册 | DeepAdapter 注册 UserHookRail + handleHooksList 真实实现 | `interface_deep.py` |
| 10.3.24 | ☐ | Sandbox | 延后 | `sandbox/` |
| 10.3.25 | ☐ | Utils | 延后 | `utils/` |
| 10.3.26 | ☐ | 入口 | 延后 | `app_agentserver.py` |
```

- [ ] **Step 4: 运行整体构建和测试确认无破坏**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./... && go test -tags=!integration,!llm,!e2e ./internal/common/hooks/... ./internal/swarm/server/hooks/... -cover`
Expected: 构建成功 + 测试通过 + 覆盖率 ≥ 85%

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/handle_extensions.go IMPLEMENTATION_PLAN.md
git commit -m "feat(10.3.23.5): 回填 DeepAdapter 注册 UserHookRail + handleHooksList 真实实现 + IMPLEMENTATION_PLAN 拆分"
```

---

### Task 6: 最终验证

- [ ] **Step 1: 运行整体测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -cover -tags=!integration,!llm,!e2e ./internal/common/hooks/... ./internal/swarm/server/hooks/... -v`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 2: 运行整体项目构建**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 成功

- [ ] **Step 3: 检查 IMPLEMENTATION_PLAN.md 状态标记正确**

确认 10.3.23.1~5 为 ✅，10.3.24~26 为 ☐
