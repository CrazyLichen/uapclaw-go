# StructuredAskUserRail (10.6.3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 StructuredAskUserRail，对齐 Python 的结构化选项问答功能，在 CodeAdapter 中注册使用。

**Architecture:** 新建 `swarm/server/rails/` 包，StructuredAskUserRail 嵌入 `interrupt.AskUserRail`，覆盖 Init/Uninit/resolveInterruptFn。导出 interrupt 包中必要的字段和类型（`ResolveInterruptFn`、`BuildAskRequest`）以支持跨包访问。

**Tech Stack:** Go 1.23+, testify/assert, testify/require

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `internal/agentcore/harness/rails/interrupt/interrupt_base.go` | Modify | 导出 `ResolveInterruptFn` 字段和类型 |
| `internal/agentcore/harness/rails/interrupt/ask_user_rail.go` | Modify | 导出 `BuildAskRequest` 方法 |
| `internal/agentcore/harness/rails/interrupt/confirm_rail.go` | Modify | 引用更新 `resolveInterruptFn` → `ResolveInterruptFn` |
| `internal/agentcore/harness/rails/interrupt/interrupt_base_test.go` | Modify | 引用更新 `resolveInterruptFn` → `ResolveInterruptFn` |
| `internal/swarm/server/rails/doc.go` | Create | 包文档 |
| `internal/swarm/server/rails/structured_ask_user_tool.go` | Create | StructuredAskUserTool + 扩展 schema |
| `internal/swarm/server/rails/structured_ask_user_tool_test.go` | Create | StructuredAskUserTool 测试 |
| `internal/swarm/server/rails/structured_ask_user_rail.go` | Create | StructuredAskUserRail + StructuredAskUserPayload |
| `internal/swarm/server/rails/structured_ask_user_rail_test.go` | Create | StructuredAskUserRail 测试 |
| `internal/swarm/server/adapter/code_adapter.go` | Modify | 回填 `buildStructuredAskUserRail()` |
| `IMPLEMENTATION_PLAN.md` | Modify | 10.6.3 状态更新 |

---

### Task 1: 导出 interrupt 包的 ResolveInterruptFn 和 BuildAskRequest

**Files:**
- Modify: `internal/agentcore/harness/rails/interrupt/interrupt_base.go`
- Modify: `internal/agentcore/harness/rails/interrupt/ask_user_rail.go`
- Modify: `internal/agentcore/harness/rails/interrupt/confirm_rail.go`
- Modify: `internal/agentcore/harness/rails/interrupt/interrupt_base_test.go`

- [ ] **Step 1: 修改 interrupt_base.go — 导出 ResolveInterruptFn 字段和类型**

将 `resolveInterruptFn` 字段改为 `ResolveInterruptFn`，类型 `resolveInterruptFn` 改为 `ResolveInterruptFn`，所有内部引用同步更新。

在 `interrupt_base.go` 中，替换：

```go
// resolveInterruptFn 中断解析函数，子类设置。默认：无输入→中断，有输入→允许。
resolveInterruptFn resolveInterruptFn
```

为：

```go
// ResolveInterruptFn 中断解析函数，子类设置。默认：无输入→中断，有输入→允许。
ResolveInterruptFn ResolveInterruptFn
```

将类型定义：

```go
// resolveInterruptFn 中断解析函数类型，子类设置。默认：无输入→中断，有输入→允许。
type resolveInterruptFn func(
```

改为：

```go
// ResolveInterruptFn 中断解析函数类型，子类设置。默认：无输入→中断，有输入→允许。
type ResolveInterruptFn func(
```

将 `NewBaseInterruptRail` 中的：

```go
r.resolveInterruptFn = r.defaultResolveInterrupt
```

改为：

```go
r.ResolveInterruptFn = r.defaultResolveInterrupt
```

将 `BeforeToolCall` 中的：

```go
decision := r.resolveInterruptFn(ctx, cbc, toolInputs.ToolCall, userInput, autoConfirmConfig)
```

改为：

```go
decision := r.ResolveInterruptFn(ctx, cbc, toolInputs.ToolCall, userInput, autoConfirmConfig)
```

- [ ] **Step 2: 修改 ask_user_rail.go — 导出 BuildAskRequest，更新 ResolveInterruptFn 引用**

将 `buildAskRequest` 方法改为 `BuildAskRequest`：

```go
// BuildAskRequest 构建 AskUserRequest。
// 返回 *AskUserRequest（InterruptRequester 接口实现），携带 questions 字段。
// JSON 序列化时，questions 自然出现在输出中（对齐 Python model_dump + extra="allow"）。
//
// 对齐 Python: AskUserRail._build_ask_request(tool_call)
func (r *AskUserRail) BuildAskRequest(toolCall *llmschema.ToolCall) *AskUserRequest {
```

将 `resolveAskUserInterrupt` 中对 `buildAskRequest` 的调用改为 `BuildAskRequest`：

```go
decision = r.Interrupt(r.BuildAskRequest(toolCall))
```

```go
return r.Interrupt(r.BuildAskRequest(toolCall))
```

将 `NewAskUserRail` 中的：

```go
r.resolveInterruptFn = r.resolveAskUserInterrupt
```

改为：

```go
r.ResolveInterruptFn = r.resolveAskUserInterrupt
```

- [ ] **Step 3: 修改 confirm_rail.go — 更新 ResolveInterruptFn 引用**

将 `NewConfirmInterruptRail` 中的：

```go
r.resolveInterruptFn = r.resolveConfirmInterrupt
```

改为：

```go
r.ResolveInterruptFn = r.resolveConfirmInterrupt
```

- [ ] **Step 4: 修改 interrupt_base_test.go — 更新 ResolveInterruptFn 引用**

将测试中 `r.resolveInterruptFn = ...` 改为 `r.ResolveInterruptFn = ...`：

```go
r.ResolveInterruptFn = func(_ context.Context, _ *agentinterfaces.AgentCallbackContext, _ *llmschema.ToolCall, _ any, _ map[string]any) InterruptDecision {
```

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/harness/rails/interrupt/...`
Expected: 编译成功，无错误

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/rails/interrupt/... -v -count=1`
Expected: 所有测试通过

- [ ] **Step 7: 提交**

```bash
git add -A
git commit -m "refactor: export ResolveInterruptFn and BuildAskRequest for cross-package access"
```

---

### Task 2: 创建 StructuredAskUserTool

**Files:**
- Create: `internal/swarm/server/rails/doc.go`
- Create: `internal/swarm/server/rails/structured_ask_user_tool.go`
- Create: `internal/swarm/server/rails/structured_ask_user_tool_test.go`

- [ ] **Step 1: 创建包目录**

Run: `mkdir -p /home/opensource/uapclaw-gateway/internal/swarm/server/rails`

- [ ] **Step 2: 创建 doc.go**

```go
// Package rails 提供 Swarm 侧的 Rail 扩展实现。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/ 下的 Rail 实现，
// 在 agentcore 的通用 Rail 基础上增加 Swarm 专属逻辑。
//
// 文件目录：
//
//	rails/
//	├── doc.go                        # 包文档
//	├── structured_ask_user_rail.go    # StructuredAskUserRail + StructuredAskUserPayload
//	└── structured_ask_user_tool.go    # StructuredAskUserTool + 扩展 schema
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/ask_user_rail.py
package rails
```

- [ ] **Step 3: 创建 structured_ask_user_tool.go**

```go
package rails

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStructuredAskUserTool 创建支持结构化选项的 AskUserTool 空壳实例。
// 使用 BuildToolCard 从注册表获取 schema（AskUserMetadataProvider 已有 questions 参数），
// 然后覆盖描述文本为结构化版本（对齐 Python EXTENDED_DESCRIPTION_EN/CN）。
//
// 对齐 Python: StructuredAskUserTool(Tool) — jiuwenswarm/agents/harness/common/rails/ask_user_rail.py
func NewStructuredAskUserTool(language, agentID string) (tool.Tool, error) {
	// 使用 BuildToolCard 获取 schema（AskUserMetadataProvider 已注册 questions 参数）
	card, err := hprompts.BuildToolCard("ask_user", "ask_user", language, nil, agentID)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "structured_ask_user_tool_create").
			Err(err).
			Msg("构建 StructuredAskUserTool ToolCard 失败")
		return nil, fmt.Errorf("构建 StructuredAskUserTool ToolCard 失败: %w", err)
	}

	// 覆盖描述文本为结构化版本（对齐 Python EXTENDED_DESCRIPTION）
	card.Description = getStructuredDescription(language)

	// 空壳 invoke：返回空 map（对齐 Python StructuredAskUserTool.invoke → return {}）
	askUserTool, err := tool.NewMapFunction(
		card,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{}, nil
		},
		nil,
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "structured_ask_user_tool_create").
			Err(err).
			Msg("创建 StructuredAskUserTool 失败")
		return nil, fmt.Errorf("创建 StructuredAskUserTool 失败: %w", err)
	}

	logger.Info(logComponent).
		Str("event_type", "structured_ask_user_tool_create").
		Str("tool_id", card.ID).
		Msg("StructuredAskUserTool 创建成功")

	return askUserTool, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getStructuredDescription 返回结构化 ask_user 工具描述。
// 对齐 Python: EXTENDED_DESCRIPTION_EN / EXTENDED_DESCRIPTION_CN
func getStructuredDescription(language string) string {
	if language == "en" {
		return "Interrupts execution and requests input from the user. " +
			"Supports two modes:\n" +
			"1. Plain query (free-text): pass only `query` — the user types their answer.\n" +
			"2. Structured questions (multi-choice): pass `query` + `questions` — " +
			"the user selects from predefined options. " +
			"Use `questions` when you want the user to choose between specific options " +
			"(e.g., 'Apply update' vs 'Skip'). Each question can have 2-4 options."
	}
	return "中断执行并向用户请求输入。支持两种模式：\n" +
		"1. 纯文本查询：只传 `query` —— 用户自由输入回答。\n" +
		"2. 结构化选项：传 `query` + `questions` —— 用户从预定义选项中选择。" +
		"当你希望用户在特定选项间做选择时（如「应用更新」vs「跳过」）使用 `questions`。" +
		"每个问题可提供 2-4 个选项。"
}
```

- [ ] **Step 4: 创建 structured_ask_user_tool_test.go**

```go
package rails

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── NewStructuredAskUserTool ────────────────────────────

// TestNewStructuredAskUserTool_中文 验证中文语言创建
func TestNewStructuredAskUserTool_中文(t *testing.T) {
	tool, err := NewStructuredAskUserTool("cn", "agent1")
	require.NoError(t, err)
	require.NotNil(t, tool)

	card := tool.Card()
	assert.Equal(t, "ask_user_agent1", card.ID)
	assert.Equal(t, "ask_user", card.Name)
	assert.Contains(t, card.Description, "结构化选项")
}

// TestNewStructuredAskUserTool_英文 验证英文语言创建
func TestNewStructuredAskUserTool_英文(t *testing.T) {
	tool, err := NewStructuredAskUserTool("en", "agent1")
	require.NoError(t, err)
	require.NotNil(t, tool)

	card := tool.Card()
	assert.Contains(t, card.Description, "Structured questions")
}

// TestNewStructuredAskUserTool_无AgentID 验证无 agentID 时使用 UUID
func TestNewStructuredAskUserTool_无AgentID(t *testing.T) {
	tool, err := NewStructuredAskUserTool("cn", "")
	require.NoError(t, err)
	require.NotNil(t, tool)

	card := tool.Card()
	// BuildToolCard 生成 ID 格式: "ask_user_<uuid8>"
	assert.Contains(t, card.ID, "ask_user_")
	assert.Greater(t, len(card.ID), len("ask_user_"))
}

// TestNewStructuredAskUserTool_默认语言 验证非 cn/en 语言回退到中文
func TestNewStructuredAskUserTool_默认语言(t *testing.T) {
	tool, err := NewStructuredAskUserTool("fr", "agent1")
	require.NoError(t, err)
	require.NotNil(t, tool)

	card := tool.Card()
	// 非中英文语言，GetAskUserMetadataProviderInputParams 回退到中文
	// 但描述文本因 getStructuredDescription 逻辑也回退到中文
	assert.Contains(t, card.Description, "结构化选项")
}

// TestNewStructuredAskUserTool_InputParams 验证 ToolCard 包含 questions 参数
func TestNewStructuredAskUserTool_InputParams(t *testing.T) {
	tool, err := NewStructuredAskUserTool("cn", "agent1")
	require.NoError(t, err)
	require.NotNil(t, tool)

	card := tool.Card()
	// InputParams 是 []*schema.Param，至少应包含 questions 参数
	assert.NotEmpty(t, card.InputParams)
	// 验证存在名为 questions 的参数
	found := false
	for _, p := range card.InputParams {
		if p.Name == "questions" {
			found = true
			break
		}
	}
	assert.True(t, found, "应包含 questions 参数")
}

// ──────────────────────────── getStructuredDescription ────────────────────────────

// TestGetStructuredDescription_中文 验证中文描述
func TestGetStructuredDescription_中文(t *testing.T) {
	desc := getStructuredDescription("cn")
	assert.Contains(t, desc, "结构化选项")
	assert.Contains(t, desc, "纯文本查询")
}

// TestGetStructuredDescription_英文 验证英文描述
func TestGetStructuredDescription_英文(t *testing.T) {
	desc := getStructuredDescription("en")
	assert.Contains(t, desc, "Structured questions")
	assert.Contains(t, desc, "Plain query")
}
```

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/rails/...`
Expected: 编译成功

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/rails/... -v -count=1`
Expected: 所有测试通过

- [ ] **Step 7: 提交**

```bash
git add -A
git commit -m "feat: add StructuredAskUserTool with extended schema for structured questions"
```

---

### Task 3: 创建 StructuredAskUserRail

**Files:**
- Create: `internal/swarm/server/rails/structured_ask_user_rail.go`
- Create: `internal/swarm/server/rails/structured_ask_user_rail_test.go`

- [ ] **Step 1: 创建 structured_ask_user_rail.go**

```go
package rails

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StructuredAskUserPayload 结构化用户回答载荷。
// 问题文本到选择选项标签的映射。
//
// 对齐 Python: StructuredAskUserPayload
type StructuredAskUserPayload struct {
	// Answers 问题文本到选择选项标签的映射
	Answers map[string]string `json:"answers"`
}

// StructuredAskUserRail 扩展 AskUserRail，支持结构化选项问答。
// 当 LLM 调用 ask_user 工具时携带 questions 参数（包含 header/options/multi_select），
// 前端可渲染为可点击选项而非纯文本输入框。
//
// 机制：ToolCallInterruptRequest.tool_args 保留原始工具调用参数（包括 questions），
// interrupt_helpers 的 _extract_questions_from_value() 检查 tool_args 中的 questions
// 字段并转换为前端格式。
//
// 对齐 Python: StructuredAskUserRail(AskUserRail) — jiuwenswarm/agents/harness/common/rails/ask_user_rail.py
type StructuredAskUserRail struct {
	interrupt.AskUserRail
	// structuredTools 已注册的 StructuredAskUserTool 引用，供 Uninit 注销
	structuredTools []tool.Tool
	// language 语言设置
	language string
	// parentResolve 保存父类 resolve 函数，用于非结构化路径回退
	parentResolve interrupt.ResolveInterruptFn
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 StructuredAskUserRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*StructuredAskUserRail)(nil)

// structuredAskUserRailLogComponent 日志组件标识
var structuredAskUserRailLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStructuredAskUserRail 创建 StructuredAskUserRail 实例。
//
// 对齐 Python: StructuredAskUserRail.__init__(tool_names=None, language=None)
func NewStructuredAskUserRail(language string) *StructuredAskUserRail {
	r := &StructuredAskUserRail{
		AskUserRail: *interrupt.NewAskUserRail(),
		language:    language,
	}
	// 保存父类 resolve 函数（对齐 Python super().resolve_interrupt()）
	r.parentResolve = r.AskUserRail.BaseInterruptRail.ResolveInterruptFn
	// 覆盖为自己的 resolve 函数
	r.AskUserRail.BaseInterruptRail.ResolveInterruptFn = r.resolveStructuredInterrupt
	return r
}

// Init 注册 StructuredAskUserTool 到 ResourceMgr + AbilityManager。
// 覆盖父类 AskUserRail 的 Init，注册扩展版工具。
//
// 对齐 Python: StructuredAskUserRail.init(agent)
func (r *StructuredAskUserRail) Init(agent agentinterfaces.BaseAgent) error {
	// 确定语言（对齐 Python: self._language or resolve_language()）
	language := r.language
	if language == "" {
		if sb := agent.SystemPromptBuilder(); sb != nil {
			language = sb.Language()
		}
		if language == "" {
			language = "cn"
		}
	}

	// 获取 agentID
	var agentID string
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}

	// 创建 StructuredAskUserTool（对齐 Python: StructuredAskUserTool(language=language, agent_id=agent_id)）
	askUserTool, err := NewStructuredAskUserTool(language, agentID)
	if err != nil {
		logger.Warn(structuredAskUserRailLogComponent).
			Str("event_type", "structured_ask_user_rail_init").
			Err(err).
			Msg("创建 StructuredAskUserTool 失败")
		return fmt.Errorf("创建 StructuredAskUserTool 失败: %w", err)
	}
	r.structuredTools = []tool.Tool{askUserTool}

	// 注册到 AbilityManager + ResourceMgr
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.structuredTools {
		if am != nil {
			am.Add(t.Card())
		}
		if resourceMgr != nil {
			_ = resourceMgr.AddTool(t)
		}
	}

	logger.Info(structuredAskUserRailLogComponent).
		Str("event_type", "structured_ask_user_rail_init").
		Str("language", language).
		Msg("StructuredAskUserRail 已注册 structured ask_user 工具")

	return nil
}

// Uninit 从 AbilityManager + ResourceMgr 注销 StructuredAskUserTool。
//
// 对齐 Python: StructuredAskUserRail.uninit(agent)
func (r *StructuredAskUserRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if len(r.structuredTools) == 0 {
		return nil
	}

	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.structuredTools {
		func(t tool.Tool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(structuredAskUserRailLogComponent).
						Str("event_type", "structured_ask_user_rail_uninit").
						Str("tool_name", t.Card().Name).
						Msgf("注销工具失败: %v", rec)
				}
			}()
			if am != nil {
				am.Remove(t.Card().Name)
			}
			if resourceMgr != nil {
				_, _ = resourceMgr.RemoveTool([]string{t.Card().ID})
			}
		}(t)
	}
	r.structuredTools = nil

	logger.Info(structuredAskUserRailLogComponent).
		Str("event_type", "structured_ask_user_rail_uninit").
		Msg("StructuredAskUserRail 注销完成")

	return nil
}

// GetStructuredTools 返回已注册的结构化工具列表。
//
// 对齐 Python: StructuredAskUserRail.get_structured_tools()
func (r *StructuredAskUserRail) GetStructuredTools() []tool.Tool {
	return r.structuredTools
}

// ExtractQuestions 从工具调用参数中提取 questions 列表。
//
// 对齐 Python: StructuredAskUserRail.extract_questions(tool_call)
func (r *StructuredAskUserRail) ExtractQuestions(toolCall *llmschema.ToolCall) []map[string]any {
	if toolCall == nil {
		return nil
	}

	args := parseToolArgsJSON(toolCall.Arguments)
	questionsRaw, ok := args["questions"].([]any)
	if !ok || len(questionsRaw) == 0 {
		return nil
	}

	questions := make([]map[string]any, 0, len(questionsRaw))
	for _, q := range questionsRaw {
		if qMap, ok := q.(map[string]any); ok {
			questions = append(questions, qMap)
		}
	}
	if len(questions) == 0 {
		return nil
	}
	return questions
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveStructuredInterrupt StructuredAskUserRail 的中断解析逻辑。
//
// 结构化路径：解析为 StructuredAskUserPayload，格式化为 "question: answer" 文本，Reject。
// 非结构化路径：回退父类 resolve 函数（对齐 Python super().resolve_interrupt()）。
//
// 对齐 Python: StructuredAskUserRail.resolve_interrupt(ctx, tool_call, user_input, auto_confirm_config)
func (r *StructuredAskUserRail) resolveStructuredInterrupt(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	userInput any,
	autoConfirmConfig map[string]any,
) (decision interrupt.InterruptDecision) {
	// 对齐 Python try/except Exception：异常时回退到 interrupt
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(structuredAskUserRailLogComponent).
				Str("event_type", "structured_ask_user_rail_resolve").
				Msgf("解析结构化输入异常，回退到 interrupt: %v", rec)
			decision = r.AskUserRail.Interrupt(r.AskUserRail.BuildAskRequest(toolCall))
		}
	}()

	// 无用户输入 → 中断
	if userInput == nil {
		return r.AskUserRail.Interrupt(r.AskUserRail.BuildAskRequest(toolCall))
	}

	// 检测是否为结构化问答（对齐 Python: questions_data = self.extract_questions(tool_call)）
	questionsData := r.ExtractQuestions(toolCall)
	isStructured := len(questionsData) > 0

	if isStructured {
		// 结构化路径
		payload, ok := r.parseStructuredInput(userInput)
		if !ok || len(payload.Answers) == 0 {
			return r.AskUserRail.Interrupt(r.AskUserRail.BuildAskRequest(toolCall))
		}

		// 格式化回答文本（对齐 Python: answer_parts = [f"{q_text}: {selected}" ...]）
		answerParts := make([]string, 0, len(payload.Answers))
		for qText, selected := range payload.Answers {
			if qText == "__free_text__" {
				answerParts = append(answerParts, selected)
			} else {
				answerParts = append(answerParts, fmt.Sprintf("%s: %s", qText, selected))
			}
		}
		answerText := strings.Join(answerParts, "\n")

		logger.Info(structuredAskUserRailLogComponent).
			Str("event_type", "structured_ask_user_rail_resolve").
			Str("answer_text", answerText).
			Msg("Resolved structured answer")

		return r.AskUserRail.BaseInterruptRail.Reject(answerText)
	}

	// 非结构化路径 — 回退父类（对齐 Python: await super().resolve_interrupt(...)）
	return r.parentResolve(ctx, cbc, toolCall, userInput, autoConfirmConfig)
}

// parseStructuredInput 解析用户输入为 StructuredAskUserPayload。
// 支持 StructuredAskUserPayload / map[string]any / interrupt.AskUserPayload / string 四种格式。
//
// 对齐 Python: StructuredAskUserRail.resolve_interrupt 中的解析逻辑
func (r *StructuredAskUserRail) parseStructuredInput(userInput any) (*StructuredAskUserPayload, bool) {
	switch input := userInput.(type) {
	case *StructuredAskUserPayload:
		return input, true
	case *interrupt.AskUserPayload:
		// 对齐 Python: AskUserPayload 转换（answer → answers）
		freeText := input.Answers
		if len(freeText) > 0 {
			return &StructuredAskUserPayload{Answers: freeText}, true
		}
		return &StructuredAskUserPayload{}, true
	case map[string]any:
		// 对齐 Python: dict with "answers" key
		if answersVal, ok := input["answers"]; ok {
			if answersMap, ok := answersVal.(map[string]any); ok {
				answers := make(map[string]string, len(answersMap))
				for k, v := range answersMap {
					if s, ok := v.(string); ok {
						answers[k] = s
					}
				}
				return &StructuredAskUserPayload{Answers: answers}, true
			}
		}
		// 对齐 Python: Frontend sends answers as {question: selected_option}
		answers := make(map[string]string, len(input))
		for k, v := range input {
			if s, ok := v.(string); ok {
				answers[k] = s
			}
		}
		return &StructuredAskUserPayload{Answers: answers}, true
	case string:
		// 对齐 Python: string input → {"__free_text__": user_input}
		if input == "" {
			return &StructuredAskUserPayload{}, true
		}
		return &StructuredAskUserPayload{Answers: map[string]string{"__free_text__": input}}, true
	default:
		return nil, false
	}
}

// parseToolArgsJSON 解析 ToolCall.Arguments JSON 字符串为 map。
// 本包本地实现，不依赖 interrupt 包的未导出函数。
func parseToolArgsJSON(arguments string) map[string]any {
	if arguments == "" {
		return map[string]any{}
	}
	args := make(map[string]any)
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return map[string]any{}
	}
	return args
}
```

- [ ] **Step 2: 创建 structured_ask_user_rail_test.go**

```go
package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── NewStructuredAskUserRail ────────────────────────────

// TestNewStructuredAskUserRail 验证构造函数
func TestNewStructuredAskUserRail(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	assert.NotNil(t, r)
	assert.Equal(t, "cn", r.language)
	// 验证 ResolveInterruptFn 已被覆盖
	assert.NotNil(t, r.AskUserRail.BaseInterruptRail.ResolveInterruptFn)
	// 验证 parentResolve 已保存
	assert.NotNil(t, r.parentResolve)
}

// TestNewStructuredAskUserRail_空语言 验证空语言参数
func TestNewStructuredAskUserRail_空语言(t *testing.T) {
	r := NewStructuredAskUserRail("")
	assert.NotNil(t, r)
	assert.Equal(t, "", r.language)
}

// ──────────────────────────── ExtractQuestions ────────────────────────────

// TestExtractQuestions_有questions 验证提取 questions 列表
func TestExtractQuestions_有questions(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:   "tc1",
		Name: "ask_user",
		Arguments: `{"questions": [{"question": "选择方案", "header": "方案", "options": [{"label": "A"}, {"label": "B"}]}]}`,
	}

	questions := r.ExtractQuestions(toolCall)
	require.Len(t, questions, 1)
	assert.Equal(t, "选择方案", questions[0]["question"])
	assert.Equal(t, "方案", questions[0]["header"])
}

// TestExtractQuestions_无questions 验证无 questions 参数返回 nil
func TestExtractQuestions_无questions(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"query": "你好"}`,
	}

	questions := r.ExtractQuestions(toolCall)
	assert.Nil(t, questions)
}

// TestExtractQuestions_nilToolCall 验证 nil ToolCall 返回 nil
func TestExtractQuestions_nilToolCall(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	questions := r.ExtractQuestions(nil)
	assert.Nil(t, questions)
}

// TestExtractQuestions_空questions 验证空 questions 列表返回 nil
func TestExtractQuestions_空questions(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": []}`,
	}

	questions := r.ExtractQuestions(toolCall)
	assert.Nil(t, questions)
}

// ──────────────────────────── resolveStructuredInterrupt ────────────────────────────

// TestResolveStructuredInterrupt_无输入中断 验证无用户输入返回中断
func TestResolveStructuredInterrupt_无输入中断(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "Q1", "header": "H1"}]}`,
	}

	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, nil, nil,
	)

	interruptResult, ok := decision.(*interrupt.InterruptResult)
	require.True(t, ok)
	assert.NotNil(t, interruptResult.Request)
}

// TestResolveStructuredInterrupt_结构化路径 验证结构化输入返回 Reject
func TestResolveStructuredInterrupt_结构化路径(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "选择方案", "header": "方案"}]}`,
	}

	// 用户输入：结构化回答
	userInput := map[string]any{
		"answers": map[string]any{
			"选择方案": "方案A",
		},
	}

	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, userInput, nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok)
	assert.Equal(t, "选择方案: 方案A", rejectResult.ToolResult)
}

// TestResolveStructuredInterrupt_结构化路径自由文本 验证 __free_text__ 键
func TestResolveStructuredInterrupt_结构化路径自由文本(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "Q1", "header": "H1"}]}`,
	}

	// 字符串输入 → __free_text__
	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, "用户自由输入", nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok)
	assert.Equal(t, "用户自由输入", rejectResult.ToolResult)
}

// TestResolveStructuredInterrupt_非结构化路径回退 验证无 questions 时回退父类
func TestResolveStructuredInterrupt_非结构化路径回退(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"query": "你好"}`,
	}

	// 无 questions → 回退父类，无用户输入 → 中断
	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, nil, nil,
	)

	_, ok := decision.(*interrupt.InterruptResult)
	assert.True(t, ok, "非结构化无输入应回退父类返回 InterruptResult")
}

// TestResolveStructuredInterrupt_非结构化路径有输入 验证无 questions 时回退父类（有输入）
func TestResolveStructuredInterrupt_非结构化路径有输入(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"query": "你好"}`,
	}

	// 无 questions + 有输入 → 回退父类（默认行为：有输入 → Approve）
	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, "用户回答", nil,
	)

	_, ok := decision.(*interrupt.ApproveResult)
	assert.True(t, ok, "非结构化有输入应回退父类返回 ApproveResult")
}

// TestResolveStructuredInterrupt_dict输入无answers键 验证 dict 输入无 answers 键
func TestResolveStructuredInterrupt_dict输入无answers键(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "Q1", "header": "H1"}]}`,
	}

	// Frontend sends answers as {question: selected_option}
	userInput := map[string]any{
		"Q1": "选项A",
	}

	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, userInput, nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok)
	assert.Equal(t, "Q1: 选项A", rejectResult.ToolResult)
}

// TestResolveStructuredInterrupt_AskUserPayload输入 验证 AskUserPayload 输入
func TestResolveStructuredInterrupt_AskUserPayload输入(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	toolCall := &llmschema.ToolCall{
		ID:        "tc1",
		Name:      "ask_user",
		Arguments: `{"questions": [{"question": "Q1", "header": "H1"}]}`,
	}

	userInput := &interrupt.AskUserPayload{
		Answers: map[string]string{"Q1": "回答1"},
	}

	decision := r.resolveStructuredInterrupt(
		context.Background(), nil, toolCall, userInput, nil,
	)

	rejectResult, ok := decision.(*interrupt.RejectResult)
	require.True(t, ok)
	assert.Contains(t, rejectResult.ToolResult, "Q1: 回答1")
}

// ──────────────────────────── parseStructuredInput ────────────────────────────

// TestParseStructuredInput_StructuredAskUserPayload 验证 StructuredAskUserPayload 输入
func TestParseStructuredInput_StructuredAskUserPayload(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	payload := &StructuredAskUserPayload{Answers: map[string]string{"Q1": "A1"}}
	result, ok := r.parseStructuredInput(payload)
	assert.True(t, ok)
	assert.Equal(t, "A1", result.Answers["Q1"])
}

// TestParseStructuredInput_DictWithAnswers 验证 dict with answers 输入
func TestParseStructuredInput_DictWithAnswers(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	input := map[string]any{
		"answers": map[string]any{"Q1": "A1"},
	}
	result, ok := r.parseStructuredInput(input)
	assert.True(t, ok)
	assert.Equal(t, "A1", result.Answers["Q1"])
}

// TestParseStructuredInput_DictWithoutAnswers 验证 dict without answers 输入
func TestParseStructuredInput_DictWithoutAnswers(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	input := map[string]any{"Q1": "选项A"}
	result, ok := r.parseStructuredInput(input)
	assert.True(t, ok)
	assert.Equal(t, "选项A", result.Answers["Q1"])
}

// TestParseStructuredInput_String 验证字符串输入
func TestParseStructuredInput_String(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	result, ok := r.parseStructuredInput("自由文本")
	assert.True(t, ok)
	assert.Equal(t, "自由文本", result.Answers["__free_text__"])
}

// TestParseStructuredInput_EmptyString 验证空字符串输入
func TestParseStructuredInput_EmptyString(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	result, ok := r.parseStructuredInput("")
	assert.True(t, ok)
	assert.Empty(t, result.Answers)
}

// TestParseStructuredInput_InvalidType 验证无效类型返回 false
func TestParseStructuredInput_InvalidType(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	result, ok := r.parseStructuredInput(42)
	assert.False(t, ok)
	assert.Nil(t, result)
}

// ──────────────────────────── parseToolArgsJSON ────────────────────────────

// TestParseToolArgsJSON_正常JSON 验证正常 JSON 解析
func TestParseToolArgsJSON_正常JSON(t *testing.T) {
	args := parseToolArgsJSON(`{"key": "value", "num": 1}`)
	assert.Equal(t, "value", args["key"])
}

// TestParseToolArgsJSON_空字符串 验证空字符串
func TestParseToolArgsJSON_空字符串(t *testing.T) {
	args := parseToolArgsJSON("")
	assert.Empty(t, args)
}

// TestParseToolArgsJSON_无效JSON 验证无效 JSON
func TestParseToolArgsJSON_无效JSON(t *testing.T) {
	args := parseToolArgsJSON("not json")
	assert.Empty(t, args)
}

// ──────────────────────────── 编译时接口验证 ────────────────────────────

// TestStructuredAskUserRail_AgentRail接口 验证满足 AgentRail 接口
func TestStructuredAskUserRail_AgentRail接口(t *testing.T) {
	var r agentinterfaces.AgentRail = NewStructuredAskUserRail("cn")
	assert.NotNil(t, r)
}

// TestStructuredAskUserRail_Priority 验证继承的优先级
func TestStructuredAskUserRail_Priority(t *testing.T) {
	r := NewStructuredAskUserRail("cn")
	assert.Equal(t, 90, r.Priority())
}

// ──────────────────────────── StructuredAskUserPayload ────────────────────────────

// TestStructuredAskUserPayload_JSON序列化 验证 JSON 序列化
func TestStructuredAskUserPayload_JSON序列化(t *testing.T) {
	payload := &StructuredAskUserPayload{
		Answers: map[string]string{"问题1": "选项A"},
	}
	// 验证字段存在
	assert.Equal(t, "选项A", payload.Answers["问题1"])
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/rails/...`
Expected: 编译成功

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/rails/... -v -count=1`
Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat: add StructuredAskUserRail with structured question support"
```

---

### Task 4: CodeAdapter 回填 + IMPLEMENTATION_PLAN.md 更新

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 修改 code_adapter.go — 回填 buildStructuredAskUserRail**

在 `code_adapter.go` 的 import 块中添加：

```go
serverrails "github.com/uapclaw/uapclaw-go/internal/swarm/server/rails"
```

替换 `buildStructuredAskUserRail` 方法：

```go
// buildStructuredAskUserRail 构建结构化询问护栏。
// 对齐 Python: JiuwenClawCodeAdapter._build_structured_ask_user_rail() (interface_code.py)
// ✅ 10.6.3: StructuredAskUserRail 已实现
func (c *CodeAdapter) buildStructuredAskUserRail() sainterfaces.AgentRail {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).Any("panic", r).
				Msg("StructuredAskUserRail 创建失败")
		}
	}()
	rail := serverrails.NewStructuredAskUserRail(c.deep.resolveRuntimeLanguage())
	logger.Info(logComponent).Msg("StructuredAskUserRail 创建成功")
	return rail
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/...`
Expected: 编译成功

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 10.6.3-10 行中的 `☐` 改为 `🔄`（10.6.3 已完成，其余还未），并标注 10.6.3 已完成：

将：

```
| 10.6.3-10 | ☐ | Swarm Rails | AskUser/Avatar/Permissions/Interrupt/ProjectMemory/ResponsePrompt/RuntimePrompt/StreamEvent | `jiuwenswarm/agents/harness/common/rails/` |
```

改为：

```
| 10.6.3-10 | 🔄 | Swarm Rails | AskUser✅/Avatar/Permissions/Interrupt/ProjectMemory/ResponsePrompt/RuntimePrompt/StreamEvent | `jiuwenswarm/agents/harness/common/rails/` |
```

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "feat: wire StructuredAskUserRail in CodeAdapter and update IMPLEMENTATION_PLAN"
```

---

### Task 5: 全量编译和测试验证

**Files:**
- No new files

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功，无错误

- [ ] **Step 2: 运行 interrupt 包测试（确保重构未破坏）**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/rails/interrupt/... -v -count=1`
Expected: 所有测试通过

- [ ] **Step 3: 运行新包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/rails/... -v -count=1`
Expected: 所有测试通过

- [ ] **Step 4: 运行 adapter 包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/... -v -count=1`
Expected: 所有测试通过

- [ ] **Step 5: 最终提交（如有修复）**

```bash
git add -A
git commit -m "test: verify all tests pass after StructuredAskUserRail implementation"
```
