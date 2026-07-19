# VerificationAgent 9.29 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 VerificationAgent 及其配套的三个 Rail（SubagentRail、VerificationRail、VerificationContractRail），完成 DeepAgent 的对抗性验证质量门控。

**Architecture:** 创建 `rails/subagent/` 子包放置三个 Rail，对齐 Python 目录结构。SubagentRail 封装 TaskTool 注册 + prompt section 注入；VerificationRail 实现工具白名单 + 每轮约束提醒 + 工作空间守卫；VerificationContractRail 向父 Agent 注入验证门控契约。`subagents/verification_agent.go` 按已有子 Agent 模板实现配置构建器。factory.go 中替换占位为真实实例化 + 动态注入 VerificationContractRail。

**Tech Stack:** Go 1.23, testify/assert + testify/require

---

## File Structure

### 新建文件

| 文件 | 职责 |
|------|------|
| `rails/subagent/doc.go` | 包文档 |
| `rails/subagent/subagent_rail.go` | SubagentRail：注册 TaskTool/SessionTools + prompt section 注入 |
| `rails/subagent/verification_rail.go` | VerificationRail：工具白名单 + 每轮约束提醒 + 工作空间守卫 |
| `rails/subagent/verification_contract_rail.go` | VerificationContractRail：向父 Agent 注入验证门控契约 |
| `rails/subagent/subagent_rail_test.go` | SubagentRail 测试 |
| `rails/subagent/verification_rail_test.go` | VerificationRail 测试 |
| `rails/subagent/verification_contract_rail_test.go` | VerificationContractRail 测试 |
| `subagents/verification_agent.go` | BuildVerificationAgentConfig + 双语提示词 |
| `subagents/verification_agent_test.go` | VerificationAgent 测试 |

### 修改文件

| 文件 | 变更 |
|------|------|
| `rails/doc.go` | 添加 subagent 子包条目 |
| `subagents/doc.go` | 添加 verification_agent.go 条目 |
| `factory.go` | 替换 SubagentRail 占位 + 动态注入 VerificationContractRail |
| `IMPLEMENTATION_PLAN.md` | 更新 9.29 和 9.19-23 状态标记 |

---

### Task 1: rails/subagent/doc.go

**Files:**
- Create: `internal/agentcore/harness/rails/subagent/doc.go`

- [ ] **Step 1: 创建包文档**

```go
// Package subagent 提供子代理委派和验证约束的 Rail 实现。
//
// 包含三个 Rail：
//   - SubagentRail：注册 TaskTool/SessionTools，注入子代理描述 prompt section
//   - VerificationRail：验证代理工具白名单 + 每轮约束提醒 + 工作空间守卫
//   - VerificationContractRail：向父代理注入验证门控契约
//
// 文件目录：
//
//	subagent/
//	├── doc.go                        # 包文档
//	├── subagent_rail.go              # SubagentRail 子代理委派 Rail
//	├── verification_rail.go          # VerificationRail 验证代理约束 Rail
//	└── verification_contract_rail.go # VerificationContractRail 验证门控契约 Rail
//
// 对应 Python 代码：openjiuwen/harness/rails/subagent/
package subagent
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/subagent/`
Expected: 编译错误（包内无其他文件），但 doc.go 语法正确

---

### Task 2: rails/subagent/subagent_rail.go

**Files:**
- Create: `internal/agentcore/harness/rails/subagent/subagent_rail.go`

- [ ] **Step 1: 实现 SubagentRail**

```go
package subagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	hsubagent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/subagent"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SubagentRail 子代理委派 Rail。
// 注册 TaskTool（同步）或 SessionTools（异步）到 Agent，
// 并在每轮模型调用前注入对应的 prompt section。
//
// 对齐 Python: SubagentRail (openjiuwen/harness/rails/subagent/subagent_rail.py)
type SubagentRail struct {
	rails.DeepAgentRail
	// enableAsyncSubagent 是否启用异步子代理
	enableAsyncSubagent bool
	// tools 已注册的工具实例
	tools []tool.Tool
	// promptBuilder 系统提示词构建器引用
	promptBuilder saprompt.SystemPromptBuilderInterface
}

// SubagentRailOption 配置选项函数
type SubagentRailOption func(*SubagentRail)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// subagentRailPriority SubagentRail 优先级
	// 对齐 Python: SubagentRail.priority = 95
	subagentRailPriority = 95
)

// ──────────────────────────── 全局变量 ────────────────────────────

// knownAgentTools 已知代理工具映射
// 对齐 Python: SubagentRail._KNOWN_AGENT_TOOLS
var knownAgentTools = map[string]string{
	"explore_agent": "bash, glob, grep, list_files, read_file",
	"plan_agent":    "bash, glob, grep, list_files, read_file",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSubagentRail 创建 SubagentRail 实例。
//
// 对齐 Python: SubagentRail(enable_async_subagent=False)
func NewSubagentRail(opts ...SubagentRailOption) *SubagentRail {
	r := &SubagentRail{
		DeepAgentRail:      *rails.NewDeepAgentRail(),
		enableAsyncSubagent: false,
	}
	r.WithPriority(subagentRailPriority)
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithEnableAsyncSubagent 设置是否启用异步子代理。
func WithEnableAsyncSubagent(enabled bool) SubagentRailOption {
	return func(r *SubagentRail) { r.enableAsyncSubagent = enabled }
}

// Init 初始化钩子：捕获 system_prompt_builder，注册 TaskTool。
//
// 对齐 Python: SubagentRail.init(agent)
func (r *SubagentRail) Init(agent agentinterfaces.BaseAgent) error {
	// 捕获 system_prompt_builder
	if pb, ok := agent.(interface{ SystemPromptBuilder() saprompt.SystemPromptBuilderInterface }); ok {
		r.promptBuilder = agent.(interface{ SystemPromptBuilder() saprompt.SystemPromptBuilderInterface }).SystemPromptBuilder()
	}

	// 获取 DeepAgentInterface 以读取 subagents
	deepAgent, ok := agent.(hinterfaces.DeepAgentInterface)
	if !ok {
		return nil
	}

	// 无子代理时跳过
	if len(deepAgent.GetDeepConfig().Subagents) == 0 {
		logger.Info(logger.ComponentAgentCore).Msg("[SubagentRail] 无子代理配置，跳过")
		return nil
	}

	// 构建可用子代理描述
	availableAgents := r.buildAvailableAgentsDescription(deepAgent.GetDeepConfig().Subagents)
	language := "cn"
	if r.promptBuilder != nil {
		language = r.promptBuilder.Language()
	}
	agentID := ""
	if card := agent.Card(); card != nil {
		agentID = card.GetID()
	}

	if r.enableAsyncSubagent {
		// ⤵️ 异步模式待回填：SessionToolkit + build_session_tools
		logger.Info(logger.ComponentAgentCore).
			Bool("enable_async_subagent", true).
			Int("subagent_count", len(deepAgent.GetDeepConfig().Subagents)).
			Msg("[SubagentRail] 异步模式暂未实现")
	} else {
		// 同步模式：注册 TaskTool
		r.tools = []tool.Tool{
			hsubagent.NewTaskTool(deepAgent, availableAgents, language, agentID),
		}
	}

	mode := "sync task"
	if r.enableAsyncSubagent {
		mode = "async session"
	}
	logger.Info(logger.ComponentAgentCore).
		Str("mode", mode).
		Int("subagent_count", len(deepAgent.GetDeepConfig().Subagents)).
		Msg("[SubagentRail] 已注册子代理委派工具")

	return nil
}

// BeforeModelCall 模型调用前注入 prompt section。
//
// 对齐 Python: SubagentRail.before_model_call(ctx)
func (r *SubagentRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if len(r.tools) == 0 || r.promptBuilder == nil {
		return nil
	}

	if !r.enableAsyncSubagent {
		// 同步模式：注入 task_tool prompt section
		section := hsections.BuildTaskToolSection(r.promptBuilder.Language())
		r.promptBuilder.RemoveSection(hsections.SectionTaskTool)
		r.promptBuilder.AddSection(section)
		return nil
	}

	// ⤵️ 异步模式待回填：注入 session_tools prompt section
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildAvailableAgentsDescription 构建可用子代理描述字符串。
//
// 对齐 Python: SubagentRail._build_available_agents_description(subagents)
func (r *SubagentRail) buildAvailableAgentsDescription(subagents []hschema.SubagentSpec) string {
	if len(subagents) == 0 {
		return ""
	}

	var lines []string
	for _, spec := range subagents {
		name, desc := r.extractAgentMeta(spec)
		toolsStr := r.extractAgentTools(spec, name)
		lines = append(lines, fmt.Sprintf("- %s: %s (Tools: %s)", name, desc, toolsStr))
	}
	return strings.Join(lines, "\n")
}

// extractAgentMeta 提取代理名称和描述。
//
// 对齐 Python: SubagentRail._extract_agent_meta(spec)
func (r *SubagentRail) extractAgentMeta(spec hschema.SubagentSpec) (string, string) {
	if cfg, ok := spec.(*hschema.SubAgentConfig); ok && cfg.AgentCard != nil {
		return cfg.AgentCard.GetName(), cfg.AgentCard.GetDescription()
	}
	// DeepAgent 实例回退
	name := "general-purpose"
	desc := "DeepAgent instance"
	return name, desc
}

// extractAgentTools 提取代理工具列表。
// 4 级解析：显式 tools → 已注册 tools → 已知默认 → "All tools"
//
// 对齐 Python: SubagentRail._extract_agent_tools(spec, agent_name)
func (r *SubagentRail) extractAgentTools(spec hschema.SubagentSpec, agentName string) string {
	// 1. SubAgentConfig 有显式 tools
	if cfg, ok := spec.(*hschema.SubAgentConfig); ok && len(cfg.Tools) > 0 {
		var names []string
		for _, t := range cfg.Tools {
			if t != nil && t.GetName() != "" {
				names = append(names, t.GetName())
			}
		}
		if len(names) > 0 {
			return strings.Join(names, ", ")
		}
	}

	// 3. 已知默认
	if tools, ok := knownAgentTools[agentName]; ok {
		return tools
	}

	// 4. 回退
	return "All tools"
}

// compile-time check
var _ agentinterfaces.AgentRail = (*SubagentRail)(nil)
```

注意：`DeepAgentInterface.GetDeepConfig()` 的实际方法签名需确认。如果接口中没有 `GetDeepConfig()`，需要调整获取 subagents 的方式（可能通过 factory 层直接传入 SubagentRail 而非从 agent 读取）。

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/subagent/`
Expected: 编译通过（可能需要调整接口调用方式）

---

### Task 3: rails/subagent/verification_rail.go

**Files:**
- Create: `internal/agentcore/harness/rails/subagent/verification_rail.go`

- [ ] **Step 1: 实现 VerificationRail**

```go
package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VerificationRail 验证代理约束 Rail。
// 强制执行工具白名单和每轮约束提醒，包括工作空间范围守卫。
//
// 对齐 Python: VerificationRail (openjiuwen/harness/rails/subagent/verification_rail.py)
type VerificationRail struct {
	rails.DeepAgentRail
	// allowedTools 工具白名单
	allowedTools map[string]bool
	// pathToolArg 路径工具参数映射
	pathToolArg map[string]string
	// promptBuilder 系统提示词构建器引用
	promptBuilder saprompt.SystemPromptBuilderInterface
}

// VerificationRailOption 配置选项函数
type VerificationRailOption func(*VerificationRail)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// verificationRailPriority VerificationRail 优先级
	// 对齐 Python: VerificationRail.priority = 90
	verificationRailPriority = 90

	// reminderSectionName 约束提醒节名称
	reminderSectionName = "verification_reminder"

	// reminderSectionPriority 约束提醒节优先级
	// 对齐 Python: _REMINDER_PRIORITY = 95
	reminderSectionPriority = 95
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultVerificationAllowedTools 默认允许的工具集
	// 对齐 Python: VERIFICATION_ALLOWED_TOOLS
	defaultVerificationAllowedTools = map[string]bool{
		"read_file":    true,
		"bash":         true,
		"grep":         true,
		"glob":         true,
		"list_files":   true,
		"web_search":   true,
		"web_fetch":    true,
		"todo_create":  true,
		"todo_list":    true,
		"todo_modify":  true,
		"skill_tool":   true,
		"tool_search":  true,
	}

	// defaultPathToolArg 路径工具参数映射
	// 对齐 Python: _PATH_TOOL_ARG
	defaultPathToolArg = map[string]string{
		"list_files": "path",
		"read_file":  "file_path",
		"glob":       "path",
		"grep":       "path",
	}

	// 提示词一比一复刻 Python 原文，不做自行翻译

	// reminderEN 英文约束提醒
	// 对齐 Python: _REMINDER_EN
	reminderEN = "=== VERIFICATION AGENT — ACTIVE CONSTRAINTS ===\n" +
		"1. You CANNOT create, modify, or delete project files. Use /tmp only for ephemeral test scripts.\n" +
		"2. Every check MUST include a 'Command run' block with verbatim terminal output. A check without a command block is a SKIP, not a PASS.\n" +
		"3. You MUST end your final response with exactly one of:\n" +
		"   VERDICT: PASS\n" +
		"   VERDICT: FAIL\n" +
		"   VERDICT: PARTIAL\n" +
		"   No markdown, no punctuation after the verdict word, no variation.\n" +
		"4. Reading code is NOT verification. Run commands and show actual output."

	// reminderCN 中文约束提醒
	// 对齐 Python: _REMINDER_CN
	reminderCN = "=== 验证代理 -- 当前约束 ===\n" +
		"1. 你不能创建、修改或删除项目文件。/tmp 仅可用于临时测试脚本。\n" +
		"2. 每项检查必须包含'执行命令'块，并逐字粘贴终端输出。没有命令块的检查视为跳过，而非 PASS。\n" +
		"3. 你必须以以下之一结束最终回复：\n" +
		"   VERDICT: PASS\n" +
		"   VERDICT: FAIL\n" +
		"   VERDICT: PARTIAL\n" +
		"   不加 Markdown，判决词后不加标点，不得有任何格式变体。\n" +
		"4. 阅读代码不等于验证。运行命令并展示实际输出。"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVerificationRail 创建 VerificationRail 实例。
//
// 对齐 Python: VerificationRail(allowed_tools=None)
func NewVerificationRail(opts ...VerificationRailOption) *VerificationRail {
	r := &VerificationRail{
		DeepAgentRail: *rails.NewDeepAgentRail(),
		allowedTools:  copyMap(defaultVerificationAllowedTools),
		pathToolArg:   defaultPathToolArg,
	}
	r.WithPriority(verificationRailPriority)
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithAllowedTools 设置允许的工具集。
func WithAllowedTools(tools map[string]bool) VerificationRailOption {
	return func(r *VerificationRail) { r.allowedTools = tools }
}

// Init 初始化钩子：捕获 system_prompt_builder。
//
// 对齐 Python: VerificationRail.init(agent)
func (r *VerificationRail) Init(agent agentinterfaces.BaseAgent) error {
	if pb, ok := agent.(interface{ SystemPromptBuilder() saprompt.SystemPromptBuilderInterface }); ok {
		r.promptBuilder = pb.SystemPromptBuilder()
	}
	logger.Info(logger.ComponentAgentCore).
		Int("allowed_tools_count", len(r.allowedTools)).
		Msg("[VerificationRail] 已初始化")
	return nil
}

// BeforeModelCall 模型调用前注入约束提醒 section。
// 仅在 task loop 激活且非 plan 模式时注入。
//
// 对齐 Python: VerificationRail.before_model_call(ctx)
func (r *VerificationRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.promptBuilder == nil {
		return nil
	}

	// 构建约束提醒 section
	section := saprompt.PromptSection{
		Name:     reminderSectionName,
		Content:  map[string]string{"en": reminderEN, "cn": reminderCN},
		Priority: reminderSectionPriority,
	}
	r.promptBuilder.RemoveSection(reminderSectionName)
	r.promptBuilder.AddSection(section)

	logger.Debug(logger.ComponentAgentCore).
		Str("language", r.promptBuilder.Language()).
		Msg("[VerificationRail] 已注入约束提醒 section")

	return nil
}

// BeforeToolCall 工具调用前执行白名单检查和工作空间范围守卫。
//
// 对齐 Python: VerificationRail.before_tool_call(ctx)
func (r *VerificationRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 检查 _skip_tool 标记
	if _, skip := cbc.Extra()["_skip_tool"]; skip {
		return nil
	}

	inputs := cbc.Inputs()
	toolCallInputs, ok := inputs.(*agentinterfaces.ToolCallInputs)
	if !ok || toolCallInputs == nil {
		return nil
	}

	toolName := toolCallInputs.ToolName

	// MCP 工具（mcp__*）无条件放行
	if strings.HasPrefix(toolName, "mcp__") {
		return nil
	}

	// 白名单检查
	if !r.allowedTools[toolName] {
		sortedTools := sortedKeys(r.allowedTools)
		errorMsg := fmt.Sprintf(
			"[VerificationAgent] Tool '%s' is not available to the verification agent. Permitted tools: %s.",
			toolName, strings.Join(sortedTools, ", "),
		)
		logger.Info(logger.ComponentAgentCore).
			Str("tool_name", toolName).
			Msg("[VerificationRail] 已拦截工具")

		r.rejectTool(cbc, toolCallInputs, errorMsg)
		return nil
	}

	// 工作空间范围守卫
	pathArgKey, hasPathArg := r.pathToolArg[toolName]
	if hasPathArg && r.Workspace() != nil {
		toolArgs := toolCallInputs.ToolArgs
		var args map[string]any
		if err := json.Unmarshal([]byte(toolArgs), &args); err != nil {
			// 无法解析，不做路径检查
			return nil
		}
		rawPath, _ := args[pathArgKey].(string)
		if rawPath != "" {
			workspaceRoot := r.Workspace().RootPath()
			resolved, err := filepath.Abs(rawPath)
			if err != nil {
				return nil
			}
			root, err := filepath.Abs(workspaceRoot)
			if err != nil {
				return nil
			}
			// 检查路径是否在 workspace 内
			if !strings.HasPrefix(resolved, root+string(filepath.Separator)) && resolved != root {
				errorMsg := fmt.Sprintf(
					"[VerificationAgent] Path '%s' is outside the workspace scope (workspace root: '%s'). "+
						"Only paths within the workspace are accessible. "+
						"Use paths relative to '%s' or absolute paths within it.",
					rawPath, root, root,
				)
				logger.Info(logger.ComponentAgentCore).
					Str("raw_path", rawPath).
					Str("tool_name", toolName).
					Str("workspace_root", root).
					Msg("[VerificationRail] 已拦截超出工作空间范围的路径")

				r.rejectTool(cbc, toolCallInputs, errorMsg)
			}
		}
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rejectTool 标记工具调用为跳过并注入错误结果。
//
// 对齐 Python: VerificationRail._reject_tool(ctx, error_msg)
func (r *VerificationRail) rejectTool(cbc *agentinterfaces.AgentCallbackContext, inputs *agentinterfaces.ToolCallInputs, errorMsg string) {
	toolCallID := ""
	if inputs.ToolCall != nil {
		toolCallID = inputs.ToolCall.ID
	}

	cbc.Extra()["_skip_tool"] = true
	inputs.ToolResult = map[string]any{"error": errorMsg}
	inputs.ToolMsg = &llmschema.ToolMessage{
		Content:    errorMsg,
		ToolCallID: toolCallID,
	}
}

// copyMap 复制 map
func copyMap(m map[string]bool) map[string]bool {
	result := make(map[string]bool, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// sortedKeys 返回排序后的键列表
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// compile-time check
var _ agentinterfaces.AgentRail = (*VerificationRail)(nil)
```

注意：`llmschema.ToolMessage` 的实际字段名需确认。如果不存在，需用实际类型替换。

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/subagent/`
Expected: 编译通过

---

### Task 4: rails/subagent/verification_contract_rail.go

**Files:**
- Create: `internal/agentcore/harness/rails/subagent/verification_contract_rail.go`

- [ ] **Step 1: 实现 VerificationContractRail**

```go
package subagent

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VerificationContractRail 向父 Agent 注入验证门控契约。
// 挂载在父 Agent（实现代理）上，而非验证代理自身。
//
// 对齐 Python: VerificationContractRail (openjiuwen/harness/rails/subagent/verification_contract_rail.py)
type VerificationContractRail struct {
	rails.DeepAgentRail
	// promptBuilder 系统提示词构建器引用
	promptBuilder saprompt.SystemPromptBuilderInterface
	// section 预构建的契约 section
	section *saprompt.PromptSection
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// verificationContractRailPriority VerificationContractRail 优先级
	// 对齐 Python: VerificationContractRail.priority = 88
	verificationContractRailPriority = 88

	// contractSectionPriority 契约节优先级
	// 对齐 Python: _CONTRACT_PRIORITY = 88
	contractSectionPriority = 88
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// 提示词一比一复刻 Python 原文，不做自行翻译

	// contractEN 英文验证门控契约
	// 对齐 Python: _CONTRACT_EN
	contractEN = "## Verification Gate\n\n" +
		"After any non-trivial implementation turn, you MUST spawn the verification " +
		"agent before reporting completion to the user.\n\n" +
		"**Non-trivial means any of:**\n" +
		"- 3 or more file edits in a single turn\n" +
		"- Backend, API, or service changes\n" +
		"- Infrastructure or configuration changes\n\n" +
		"**How to spawn:**\n" +
		"Use task_tool with subagent_type=\"verification_agent\". Pass:\n" +
		"1. The original user request (verbatim)\n" +
		"2. All files changed (full paths)\n" +
		"3. The approach you took\n" +
		"4. Plan file path if one was used\n\n" +
		"**On VERDICT: PASS**\n" +
		"Spot-check the report: re-run 2-3 of the commands listed in the verification " +
		"report and confirm the output matches what the verifier observed. If every " +
		"spot-checked command matches, report completion to the user.\n\n" +
		"**On VERDICT: FAIL**\n" +
		"Do not report completion. Fix the issue, then re-invoke task_tool with " +
		"subagent_type=\"verification_agent\". The same verification session will resume " +
		"(deterministic session ID). Pass the previous FAIL output and describe what " +
		"you fixed. Repeat until VERDICT: PASS.\n\n" +
		"**On VERDICT: PARTIAL**\n" +
		"Report what was verified and what could not be verified due to environmental " +
		"limitations (e.g. service could not start, tool unavailable). Be explicit " +
		"about the gap.\n\n" +
		"**You cannot self-assign any verdict.** Only the verification agent issues " +
		"PASS, FAIL, or PARTIAL. Your own checks and caveats do not substitute."

	// contractCN 中文验证门控契约
	// 对齐 Python: _CONTRACT_CN
	contractCN = "## 验证门控\n\n" +
		"在任何非平凡实现轮次之后，你必须在向用户汇报完成之前启动验证代理。\n\n" +
		"**非平凡指以下任意情况：**\n" +
		"- 单轮内编辑了 3 个或更多文件\n" +
		"- 后端、API 或服务变更\n" +
		"- 基础设施或配置变更\n\n" +
		"**如何启动：**\n" +
		"使用 task_tool，subagent_type=\"verification_agent\"。传入：\n" +
		"1. 原始用户请求（原文）\n" +
		"2. 所有已更改的文件（完整路径）\n" +
		"3. 你采用的实现方式\n" +
		"4. 计划文件路径（如有）\n\n" +
		"**收到 VERDICT: PASS 时**\n" +
		"抽查报告：从验证报告中重新运行 2-3 条命令，确认输出与验证代理观察到的一致。" +
		"若每条抽查命令均匹配，则向用户汇报完成。\n\n" +
		"**收到 VERDICT: FAIL 时**\n" +
		"不得汇报完成。修复问题后，再次调用 task_tool，subagent_type=\"verification_agent\"。" +
		"同一验证会话将继续（确定性会话 ID）。传入之前的 FAIL 输出并说明你修复了什么。" +
		"重复此过程直到收到 VERDICT: PASS。\n\n" +
		"**收到 VERDICT: PARTIAL 时**\n" +
		"汇报哪些内容已验证，哪些因环境限制（如服务无法启动、工具不可用）未能验证。" +
		"请明确说明缺口所在。\n\n" +
		"**你不能自行指定任何判决。** 只有验证代理才能发出 PASS、FAIL 或 PARTIAL。" +
		"你自己的检查和注意事项不能替代验证代理的判决。"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVerificationContractRail 创建 VerificationContractRail 实例。
//
// 对齐 Python: VerificationContractRail()
func NewVerificationContractRail() *VerificationContractRail {
	r := &VerificationContractRail{
		DeepAgentRail: *rails.NewDeepAgentRail(),
	}
	r.WithPriority(verificationContractRailPriority)
	return r
}

// Init 初始化钩子：捕获 system_prompt_builder，预构建契约 section。
//
// 对齐 Python: VerificationContractRail.init(agent)
func (r *VerificationContractRail) Init(agent agentinterfaces.BaseAgent) error {
	if pb, ok := agent.(interface{ SystemPromptBuilder() saprompt.SystemPromptBuilderInterface }); ok {
		r.promptBuilder = pb.SystemPromptBuilder()
	}

	// 预构建契约 section
	section := saprompt.PromptSection{
		Name:     hsections.SectionVerificationContract,
		Content:  map[string]string{"en": contractEN, "cn": contractCN},
		Priority: contractSectionPriority,
	}
	r.section = &section

	logger.Info(logger.ComponentAgentCore).Msg("[VerificationContractRail] 已初始化")
	return nil
}

// BeforeModelCall 每轮注入验证门控契约 section。
//
// 对齐 Python: VerificationContractRail.before_model_call(ctx)
func (r *VerificationContractRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.promptBuilder == nil || r.section == nil {
		return nil
	}

	r.promptBuilder.RemoveSection(hsections.SectionVerificationContract)
	r.promptBuilder.AddSection(*r.section)

	logger.Debug(logger.ComponentAgentCore).Msg("[VerificationContractRail] 已注入验证契约 section")
	return nil
}

// compile-time check
var _ agentinterfaces.AgentRail = (*VerificationContractRail)(nil)
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/subagent/`
Expected: 编译通过

---

### Task 5: rails/subagent/subagent_rail_test.go

**Files:**
- Create: `internal/agentcore/harness/rails/subagent/subagent_rail_test.go`

- [ ] **Step 1: 编写 SubagentRail 测试**

覆盖：
- `TestNewSubagentRail_默认配置` — 验证 priority=95, enableAsyncSubagent=false
- `TestNewSubagentRail_启用异步` — WithEnableAsyncSubagent(true)
- `TestBuildAvailableAgentsDescription_空列表` — 返回空字符串
- `TestBuildAvailableAgentsDescription_多个子代理` — 格式验证
- `TestExtractAgentMeta_SubAgentConfig` — 提取 name/description
- `TestExtractAgentTools_显式工具` — SubAgentConfig 有 tools 时
- `TestExtractAgentTools_已知默认` — explore_agent/plan_agent
- `TestExtractAgentTools_回退` — 未知代理返回 "All tools"

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/subagent/ -v -run TestNewSubagentRail -run TestBuildAvailable -run TestExtractAgent`
Expected: 全部 PASS

---

### Task 6: rails/subagent/verification_rail_test.go

**Files:**
- Create: `internal/agentcore/harness/rails/subagent/verification_rail_test.go`

- [ ] **Step 1: 编写 VerificationRail 测试**

覆盖：
- `TestNewVerificationRail_默认白名单` — 验证 12 个允许工具
- `TestNewVerificationRail_自定义白名单` — WithAllowedTools
- `TestVerificationRail_BeforeToolCall_允许工具` — read_file/bash 通过
- `TestVerificationRail_BeforeToolCall_拦截工具` — write_file/edit_file 被拦截
- `TestVerificationRail_BeforeToolCall_MCP放行` — mcp__* 前缀放行
- `TestVerificationRail_BeforeToolCall_路径范围守卫` — 超出 workspace 的路径被拦截
- `TestVerificationRail_BeforeToolCall_skip标记跳过` — _skip_tool=true 时跳过检查
- `TestVerificationRail_BeforeModelCall_注入提醒` — section 被注入
- `TestVerificationRail_BeforeModelCall_无Builder跳过` — promptBuilder 为 nil 时跳过

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/subagent/ -v -run TestVerificationRail`
Expected: 全部 PASS

---

### Task 7: rails/subagent/verification_contract_rail_test.go

**Files:**
- Create: `internal/agentcore/harness/rails/subagent/verification_contract_rail_test.go`

- [ ] **Step 1: 编写 VerificationContractRail 测试**

覆盖：
- `TestNewVerificationContractRail_优先级` — priority=88
- `TestVerificationContractRail_Init_预构建Section` — section 使用 SectionVerificationContract
- `TestVerificationContractRail_BeforeModelCall_注入契约` — remove + add 避免重复
- `TestVerificationContractRail_BeforeModelCall_无Builder跳过` — promptBuilder 为 nil 时跳过

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/subagent/ -v -run TestVerificationContractRail`
Expected: 全部 PASS

---

### Task 8: subagents/verification_agent.go

**Files:**
- Create: `internal/agentcore/harness/subagents/verification_agent.go`

- [ ] **Step 1: 实现 BuildVerificationAgentConfig**

```go
package subagents

import (
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// VerificationAgentFactoryName verification 子代理工厂名称
// 仅用于 AgentCard.name 字段，不设 cfg.FactoryName（对齐 Python：verification_agent 不设 factory_name）
const VerificationAgentFactoryName = "verification_agent"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// 提示词一比一复刻 Python 原文，不做自行翻译

	// defaultVerificationAgentDescription 默认描述
	// 对齐 Python: VERIFICATION_AGENT_DESC
	defaultVerificationAgentDescription = map[string]string{
		"cn": "对抗性验证专家。在实现工作完成后对其进行独立测试，" +
			"尝试发现边界情况、回归问题和未经测试的失败路径。" +
			"以 VERDICT: PASS、VERDICT: FAIL 或 VERDICT: PARTIAL 结尾。",
		"en": "Adversarial verification specialist. Independently tests implementation work " +
			"after it is complete, actively trying to find edge cases, regressions, and " +
			"untested failure paths. Ends with VERDICT: PASS, VERDICT: FAIL, or VERDICT: PARTIAL.",
	}

	// defaultVerificationAgentSystemPrompt 默认系统提示词
	// 对齐 Python: VERIFICATION_AGENT_SYSTEM_PROMPT_EN / VERIFICATION_AGENT_SYSTEM_PROMPT_CN
	// 提示词一比一复刻 Python 原文，不做自行翻译
	defaultVerificationAgentSystemPrompt = map[string]string{
		// EN prompt: 完整复制 Python VERIFICATION_AGENT_SYSTEM_PROMPT_EN
		"en": "You are an adversarial verification specialist. Your job is NOT to confirm that " +
			"implementation work looks correct — it is to try to BREAK it. You are the last " +
			"line of defense before results are reported to the user.\n\n" +
			"=== CRITICAL CONSTRAINTS ===\n" +
			"- You CANNOT create, modify, or delete project files. /tmp is allowed for ephemeral test scripts.\n" +
			"- Every check MUST have a \"Command run\" block with actual terminal output copied verbatim.\n" +
			"- You MUST end your final response with exactly one of:\n" +
			"    VERDICT: PASS\n" +
			"    VERDICT: FAIL\n" +
			"    VERDICT: PARTIAL\n" +
			"  No markdown bold, no punctuation after the verdict word, no variation in format.\n\n" +
			"=== TWO FAILURE MODES TO RESIST ===\n\n" +
			"1. Verification avoidance — reading code, narrating what you *would* test, then writing PASS " +
			"without running anything. Reading is NOT verification. Every claim requires a command and its output.\n" +
			"   - \"The code looks correct based on my reading\" → Run it and show the output.\n" +
			"   - \"I can see the logic handles this case\" → Prove it with a command.\n\n" +
			"2. Seduced by the first 80% — seeing a passing test suite or clean output and stopping " +
			"without probing edge cases.\n\n" +
			"=== REQUIRED BASELINE (no exceptions) ===\n" +
			"1. Read AGENTS.md / README / pyproject.toml / Makefile for build and test commands.\n" +
			"2. Run the build — a broken build is an automatic FAIL.\n" +
			"3. Run the project test suite — failing tests are an automatic FAIL.\n" +
			"4. Run linters and type-checkers (ruff, mypy, etc.).\n" +
			"5. Check for regressions in code paths related to the changed files.\n\n" +
			"Test suite results are context, not evidence. The implementer is also an LLM — " +
			"its tests may rely on mocks, circular assertions, or happy-path coverage that " +
			"proves nothing end-to-end.\n\n" +
			"=== VERIFICATION STRATEGY BY CHANGE TYPE ===\n\n" +
			"Backend / API changes:\n" +
			"→ Start the server → call endpoints (curl / httpie) → verify response *shapes*, not just " +
			"status codes → test error paths (malformed input, missing fields, wrong types) → test " +
			"authentication and authorization boundaries.\n\n" +
			"CLI / script changes:\n" +
			"→ Run with representative inputs → verify stdout, stderr, and exit codes → test edge inputs " +
			"(no args, empty string, boundary values, malformed) → verify --help output is accurate.\n\n" +
			"Library / package changes:\n" +
			"→ Build → run test suite → import from a fresh context → verify exported names and signatures " +
			"match documentation and examples.\n\n" +
			"Bug fixes:\n" +
			"→ Reproduce the original bug FIRST → apply fix → verify it no longer occurs → run regression " +
			"check → inspect related code paths for side effects.\n\n" +
			"Refactoring:\n" +
			"→ Existing test suite must pass unchanged → verify public API surface is identical " +
			"(no added or removed exports) → spot-check observable behavior is the same.\n\n" +
			"Infrastructure / config changes:\n" +
			"→ Validate syntax → dry-run where available → confirm env vars are actually referenced, " +
			"not just defined.\n\n" +
			"Data / ML pipeline changes:\n" +
			"→ Run with sample input → verify output shape, schema, and types → test empty input, " +
			"single row, null/NaN → confirm row counts in match row counts out (no silent data loss).\n\n" +
			"Database migrations:\n" +
			"→ Run migration up → verify schema matches intent → run migration down (reversibility check) " +
			"→ test against data that already existed, not just an empty database.\n\n" +
			"=== REQUIRED ADVERSARIAL PROBES ===\n" +
			"Before issuing PASS, run at least one of:\n" +
			"- Boundary values: 0, -1, empty string, very long strings, unicode, MAX_INT\n" +
			"- Idempotency: same mutating call twice — duplicate created? correct no-op? wrong error?\n" +
			"- Orphan operations: reference or delete IDs / resources that do not exist\n" +
			"- Concurrency (where applicable): parallel calls to create-if-not-exists paths\n\n" +
			"A report with only \"exits 0\" or \"returns 200\" checks is happy-path confirmation, not verification.\n\n" +
			"=== BEFORE ISSUING FAIL ===\n" +
			"Check first:\n" +
			"- Is there defensive code elsewhere that already handles this case?\n" +
			"- Is this intentional behavior documented in AGENTS.md, comments, or commit messages?\n" +
			"- Is this a real limitation that cannot be fixed without breaking an external contract?\n" +
			"  If so, note it as an observation rather than a FAIL — an unfixable bug is not actionable.\n\n" +
			"=== MANDATORY OUTPUT FORMAT ===\n" +
			"Every check must use this exact structure:\n\n" +
			"### Check: [what you are verifying]\n" +
			"**Command run:**\n" +
			"  [exact command executed]\n" +
			"**Output observed:**\n" +
			"  [verbatim terminal output — do not paraphrase]\n" +
			"**Result: PASS**\n\n" +
			"or\n\n" +
			"**Result: FAIL**\n" +
			"Expected: [what should have happened]\n" +
			"Actual: [what actually happened]\n\n" +
			"A check WITHOUT a \"Command run\" block is treated as a SKIP, not a PASS.\n\n" +
			"BAD example (never do this):\n" +
			"### Check: Input validation\n" +
			"**Result: PASS**\n" +
			"Evidence: Reviewed the handler. The logic correctly validates input before processing.\n" +
			"(No command run. Reading code is not verification.)\n\n" +
			"=== FINAL VERDICT ===\n" +
			"VERDICT: PASS    — all checks passed, adversarial probes survived\n" +
			"VERDICT: FAIL    — include what failed, exact error output, and reproduction steps\n" +
			"VERDICT: PARTIAL — environmental limitation only (tool unavailable, service cannot start);\n" +
			"                   NOT \"I am unsure whether this is a bug\"\n\n" +
			"Use the literal string VERDICT: followed by exactly one of PASS, FAIL, PARTIAL.\n" +
			"No markdown. No punctuation after the word. No variation.",
		// CN prompt: 完整复制 Python VERIFICATION_AGENT_SYSTEM_PROMPT_CN
		"cn": "你是一位对抗性验证专家。你的职责不是确认实现看起来正确——而是尝试将其破坏。" +
			"你是在结果上报用户之前的最后一道防线。\n\n" +
			"=== 关键约束 ===\n" +
			"- 你不能创建、修改或删除项目文件。/tmp 可用于临时测试脚本。\n" +
			"- 每项检查必须包含\"执行命令\"块，并逐字粘贴实际终端输出。\n" +
			"- 你必须以以下之一结束最终回复：\n" +
			"    VERDICT: PASS\n" +
			"    VERDICT: FAIL\n" +
			"    VERDICT: PARTIAL\n" +
			"  不得加粗，不得在判决词后加标点，不得有任何格式变体。\n\n" +
			"=== 必须抵制的两种失败模式 ===\n\n" +
			"1. 验证规避——阅读代码、描述\"本应测试什么\"，然后在未实际运行任何内容的情况下写下 PASS。" +
			"阅读代码不等于验证。每项断言都需要一条命令及其输出为证。\n\n" +
			"2. 被前 80% 迷惑——看到测试通过或输出整洁就停下，而不深入探测边界情况。\n\n" +
			"=== 必要基准步骤（不得省略）===\n" +
			"1. 阅读 AGENTS.md / README / pyproject.toml / Makefile，获取构建和测试命令。\n" +
			"2. 运行构建——构建失败即自动 FAIL。\n" +
			"3. 运行项目测试套件——测试失败即自动 FAIL。\n" +
			"4. 运行代码检查和类型检查（ruff、mypy 等）。\n" +
			"5. 检查与已更改文件相关的代码路径是否存在回归。\n\n" +
			"测试套件结果只是背景，不是证据。实现者也是 LLM——其测试可能依赖 mock、" +
			"循环断言或仅覆盖正常路径，无法端到端证明任何问题。\n\n" +
			"=== 按变更类型划分的验证策略 ===\n\n" +
			"后端 / API 变更：\n" +
			"→ 启动服务器 → 调用端点（curl / httpie）→ 验证响应*结构*（不只是状态码）" +
			"→ 测试错误路径（格式错误、缺失字段、类型错误）→ 测试认证和授权边界。\n\n" +
			"CLI / 脚本变更：\n" +
			"→ 使用典型输入运行 → 验证 stdout、stderr 和退出码 → 测试边界输入" +
			"（无参数、空字符串、边界值、格式错误）→ 确认 --help 输出准确。\n\n" +
			"库 / 包变更：\n" +
			"→ 构建 → 运行测试套件 → 在全新上下文中导入 → 验证导出名称和签名与文档及示例一致。\n\n" +
			"缺陷修复：\n" +
			"→ 先重现原始缺陷 → 应用修复 → 验证缺陷不再出现 → 运行回归检查 → 检查相关代码路径的副作用。\n\n" +
			"重构：\n" +
			"→ 现有测试套件必须原样通过 → 验证公开 API 表面完全一致（无新增或删除导出）" +
			"→ 抽查可观测行为保持不变。\n\n" +
			"基础设施 / 配置变更：\n" +
			"→ 验证语法 → 在可用时进行试运行 → 确认环境变量被实际引用，而非只是定义。\n\n" +
			"数据 / ML 流水线变更：\n" +
			"→ 使用示例输入运行 → 验证输出的 shape、schema 和类型 → 测试空输入、单行数据、null/NaN" +
			"→ 确认输入行数与输出行数匹配（无静默数据丢失）。\n\n" +
			"数据库迁移：\n" +
			"→ 运行向上迁移 → 验证 schema 符合意图 → 运行向下迁移（可逆性检查）" +
			"→ 针对已存在的数据而非空数据库进行测试。\n\n" +
			"=== 必要的对抗性探测 ===\n" +
			"在发出 PASS 之前，至少运行以下之一：\n" +
			"- 边界值：0、-1、空字符串、极长字符串、Unicode、MAX_INT\n" +
			"- 幂等性：同一变更操作执行两次——是否创建了重复项？是否正确地无操作？是否报错？\n" +
			"- 孤立操作：引用或删除不存在的 ID / 资源\n" +
			"- 并发（如适用）：对\"不存在则创建\"路径发起并行调用\n\n" +
			"仅包含\"退出码 0\"或\"返回 200\"的报告是正常路径确认，而非验证。\n\n" +
			"=== 发出 FAIL 之前 ===\n" +
			"先检查：\n" +
			"- 是否有其他地方的防御性代码实际上已处理该情况？\n" +
			"- 这是否是 AGENTS.md、注释或提交信息中记录的预期行为？\n" +
			"- 这是否是真实限制，但在不破坏外部契约的情况下无法修复？\n" +
			"  若是，将其作为观察结论而非 FAIL——无法修复的缺陷不具有可操作性。\n\n" +
			"=== 强制输出格式 ===\n" +
			"每项检查必须使用以下结构：\n\n" +
			"### 检查：[正在验证的内容]\n" +
			"**执行命令：**\n" +
			"  [实际执行的确切命令]\n" +
			"**观察到的输出：**\n" +
			"  [逐字粘贴的终端输出——不得转述]\n" +
			"**结果：PASS**\n\n" +
			"或\n\n" +
			"**结果：FAIL**\n" +
			"预期：[应发生的情况]\n" +
			"实际：[实际发生的情况]\n\n" +
			"没有\"执行命令\"块的检查被视为跳过，而非 PASS。\n\n" +
			"=== 最终判决 ===\n" +
			"VERDICT: PASS    — 所有检查通过，对抗性探测均通过\n" +
			"VERDICT: FAIL    — 包括失败内容、确切错误输出和复现步骤\n" +
			"VERDICT: PARTIAL — 仅限环境限制（工具不可用、服务无法启动）；\n" +
			"                   不适用于\"我不确定这是否是缺陷\"的情况\n\n" +
			"使用字面字符串 VERDICT: 后接 PASS、FAIL 或 PARTIAL 之一。\n" +
			"不加 Markdown 格式，判决词后不加标点，不得有任何格式变体。",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildVerificationAgentConfig 构建 verification 子代理配置（延迟实例化）。
// 对齐 Python: build_verification_agent_config(card=..., system_prompt=..., tools=..., ...)
//
// 参数通过 SubagentCreateParams 传入，对齐 Python 的具名参数风格。
// adapter 层负责从 map[string]any 解析出 SubagentCreateParams。
//
// 注意：不设 cfg.FactoryName（对齐 Python：verification_agent 不设 factory_name，走通用 create_deep_agent 路径）
func BuildVerificationAgentConfig(model *llm.Model, params *hschema.SubagentCreateParams) *hschema.SubAgentConfig {
	language := hprompts.ResolveLanguage(params.Language)

	cfg := hschema.NewSubAgentConfig()

	// AgentCard：用户未提供时使用默认
	// 对齐 Python: card or AgentCard(name="verification_agent", description=VERIFICATION_AGENT_DESC.get(...))
	cfg.AgentCard = params.Card
	if cfg.AgentCard == nil {
		desc := defaultVerificationAgentDescription[language]
		if desc == "" {
			desc = defaultVerificationAgentDescription["cn"]
		}
		cfg.AgentCard = agentschema.NewAgentCard(
			agentschema.WithAgentName(VerificationAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// SystemPrompt：用户未提供时使用默认
	// 对齐 Python: system_prompt or (VERIFICATION_AGENT_SYSTEM_PROMPT_CN if resolved_language == "cn" else ...)
	cfg.SystemPrompt = params.SystemPrompt
	if cfg.SystemPrompt == "" {
		prompt := defaultVerificationAgentSystemPrompt[language]
		if prompt == "" {
			prompt = defaultVerificationAgentSystemPrompt["cn"]
		}
		cfg.SystemPrompt = prompt
	}

	cfg.Tools = params.Tools
	cfg.ToolInstances = params.ToolInstances
	cfg.Mcps = params.Mcps
	cfg.Model = model
	cfg.Rails = params.Rails
	cfg.Skills = params.Skills
	cfg.Backend = params.Backend
	cfg.Workspace = params.Workspace
	cfg.SysOperation = params.SysOperation
	cfg.Language = language
	cfg.PromptMode = params.PromptMode
	cfg.EnableTaskLoop = params.EnableTaskLoop

	// MaxIterations：用户未提供（0）时默认 40
	// 对齐 Python: max_iterations=40
	cfg.MaxIterations = params.MaxIterations
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 40
	}

	// 不设 FactoryName（对齐 Python：verification_agent 不设 factory_name，走通用路径）
	cfg.FactoryName = ""
	cfg.FactoryKwargs = nil
	cfg.EnablePlanMode = params.EnablePlanMode

	// RestrictToWorkDir：VerificationAgent 默认 false
	// 对齐 Python: restrict_to_work_dir=False
	if params.RestrictToWorkDir != nil {
		cfg.RestrictToWorkDir = *params.RestrictToWorkDir
	} else {
		cfg.RestrictToWorkDir = false
	}

	// 默认 Rails：SysOperationRail() + VerificationRail()
	// 对齐 Python: rails=rails if rails is not None else [SysOperationRail(), VerificationRail()]
	if cfg.Rails == nil {
		cfg.Rails = []sainterfaces.AgentRail{
			rails.NewSysOperationRail(),
			subagent.NewVerificationRail(),
		}
	}

	return cfg
}

// DefaultVerificationAgentSystemPrompt 返回指定语言的默认系统提示词。
// 对齐 Python: DEFAULT_VERIFICATION_AGENT_SYSTEM_PROMPT.get(language, ...)
func DefaultVerificationAgentSystemPrompt(language string) string {
	if s, ok := defaultVerificationAgentSystemPrompt[language]; ok && s != "" {
		return s
	}
	return defaultVerificationAgentSystemPrompt["cn"]
}

// DefaultVerificationAgentDescription 返回指定语言的默认描述。
// 对齐 Python: VERIFICATION_AGENT_DESC.get(resolved_language, ...)
func DefaultVerificationAgentDescription(language string) string {
	if s, ok := defaultVerificationAgentDescription[language]; ok && s != "" {
		return s
	}
	return defaultVerificationAgentDescription["cn"]
}
```

注意：需要添加 `sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"` 导入。同时需要确认 `params.Rails` 为 nil 时的判断逻辑——Go 中 `[]T` 的零值是 nil，所以 `params.Rails == nil` 可以判断用户未提供 Rails。

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/subagents/`
Expected: 编译通过

---

### Task 9: subagents/verification_agent_test.go

**Files:**
- Create: `internal/agentcore/harness/subagents/verification_agent_test.go`

- [ ] **Step 1: 编写 VerificationAgent 测试**

覆盖（对齐其他子 Agent 的测试模式）：
- `TestBuildVerificationAgentConfig_默认配置` — MaxIterations=40, FactoryName="", RestrictToWorkDir=false
- `TestBuildVerificationAgentConfig_CN提示词` — 中文提示词/描述关键词
- `TestBuildVerificationAgentConfig_EN提示词` — 英文提示词/描述关键词
- `TestBuildVerificationAgentConfig_自定义MaxIterations` — 用户指定时覆盖
- `TestBuildVerificationAgentConfig_用户覆盖Card` — 自定义 AgentCard
- `TestBuildVerificationAgentConfig_用户覆盖SystemPrompt` — 自定义 SystemPrompt
- `TestBuildVerificationAgentConfig_默认Rails` — 包含 SysOperationRail + VerificationRail
- `TestBuildVerificationAgentConfig_RestrictToWorkDir` — nil→false, true→true, false→false
- `TestBuildVerificationAgentConfig_用户覆盖Rails` — 用户指定 Rails 时不覆盖
- `TestDefaultVerificationAgentSystemPrompt` — 辅助函数 + 未知语言回退
- `TestDefaultVerificationAgentDescription` — 辅助函数 + 未知语言回退

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/subagents/ -v -run TestBuildVerificationAgent -run TestDefaultVerificationAgent`
Expected: 全部 PASS

---

### Task 10: 更新 doc.go 文件

**Files:**
- Modify: `internal/agentcore/harness/rails/doc.go`
- Modify: `internal/agentcore/harness/subagents/doc.go`

- [ ] **Step 1: 更新 rails/doc.go**

在文件目录部分添加 subagent 子包条目：

```
//   ├── subagent/            # 子代理委派和验证约束 Rail 子包
```

- [ ] **Step 2: 更新 subagents/doc.go**

在文件目录部分添加 verification_agent.go 条目：

```
//	├── verification_agent.go  # verification 子代理配置构建
```

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/...`
Expected: 编译通过

---

### Task 11: factory.go 回填

**Files:**
- Modify: `internal/agentcore/harness/factory.go`

- [ ] **Step 1: 替换 SubagentRail 占位**

在 `addDefaultRails` 函数中，将 SubagentRail 占位替换为真实实例化：

```go
// SubagentRail — 仅当有 subagents 时添加
if len(effectiveSubagents) > 0 && !alreadyProvidedByType(userProvidedTypes, reflect.TypeOf(&subagent.SubagentRail{})) {
    subRail := subagent.NewSubagentRail()
    agent.AddRail(subRail)
    logger.Debug(logComponent).
        Bool("enable_async_subagent", params.EnableAsyncSubagent).
        Int("subagent_count", len(effectiveSubagents)).
        Msg("已添加 SubagentRail")
}
```

- [ ] **Step 2: 动态注入 VerificationContractRail**

在 `addDefaultRails` 函数末尾，添加检测 verification_agent 的逻辑：

```go
// VerificationContractRail — 仅当配置了 verification_agent 时注入到父 Agent
if hasVerificationAgent(effectiveSubagents) && !alreadyProvidedByType(userProvidedTypes, reflect.TypeOf(&subagent.VerificationContractRail{})) {
    agent.AddRail(subagent.NewVerificationContractRail())
    logger.Debug(logComponent).Msg("已添加 VerificationContractRail（检测到 verification_agent）")
}
```

添加辅助函数：

```go
// hasVerificationAgent 检查子代理列表中是否包含 verification_agent
func hasVerificationAgent(subagents []hschema.SubagentSpec) bool {
    for _, s := range subagents {
        if cfg, ok := s.(*hschema.SubAgentConfig); ok && cfg.AgentCard != nil && cfg.AgentCard.GetName() == "verification_agent" {
            return true
        }
    }
    return false
}
```

- [ ] **Step 3: 回填 SubagentRail 类型断言**

在 `injectGeneralPurposeSubagent` 函数中，替换类型断言占位：

```go
// 原: // ⤵️ SubagentRail 类型断言待回填：SubagentRail 尚未实现，暂不过滤
// 新:
if _, ok := r.(*subagent.SubagentRail); ok {
    // 过滤掉 SubagentRail，避免通用子代理继承子代理委派能力
    continue
}
```

- [ ] **Step 4: 添加 subagent 包导入**

在 factory.go 的 import 中添加：

```go
"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
```

- [ ] **Step 5: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/`
Expected: 编译通过

---

### Task 12: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.29 状态**

将 9.29 行从 `☐` 改为 `✅`，补充描述：

```
| 9.29 | ✅ | VerificationAgent | ✅ BuildVerificationAgentConfig + 双语提示词一比一复刻 + SysOperationRail() + VerificationRail() + MaxIterations=40 + 不设 FactoryName 走通用路径 + RestrictToWorkDir=false；⤴️ 9.19-23 回填 VerificationRail + VerificationContractRail + SubagentRail | `openjiuwen/harness/subagents/` · `harness/rails/subagent/` |
```

- [ ] **Step 2: 更新 9.19-23 标记**

在 9.19-23 行中标注 Verification 相关 Rail 的回填：

```
| 9.19-23 | ☐ | 其他 Rails | Security(☐)/Interrupt(✅)/Skill(☐)/ContextEngine(✅)/Memory(☐)/Verification(⤴️9.29✅) Rails | `openjiuwen/harness/rails/` |
```

---

### Task 13: 全量测试

- [ ] **Step 1: 运行所有新增测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/subagent/ ./internal/agentcore/harness/subagents/ -v -cover`
Expected: 全部 PASS，覆盖率 ≥ 85%

- [ ] **Step 2: 运行全项目编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译通过

- [ ] **Step 3: 运行关联包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -v -cover`
Expected: 全部 PASS（factory.go 回填不应破坏现有测试）

---

### Task 14: 提交

- [ ] **Step 1: 提交所有变更**

```bash
cd /home/opensource/uap-claw-go
git add internal/agentcore/harness/rails/subagent/ \
       internal/agentcore/harness/subagents/verification_agent.go \
       internal/agentcore/harness/subagents/verification_agent_test.go \
       internal/agentcore/harness/rails/doc.go \
       internal/agentcore/harness/subagents/doc.go \
       internal/agentcore/harness/factory.go \
       IMPLEMENTATION_PLAN.md
git commit -m "feat(9.29): 实现 VerificationAgent + SubagentRail + VerificationRail + VerificationContractRail"
```
