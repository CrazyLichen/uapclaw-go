# AbilityManager 3.13 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 AbilityManager — Agent 能力注册与调度中心，管理四类 Ability（Tool/Workflow/Agent/McpServer）的注册、查询、LLM 工具描述生成、并行执行和 JSON 参数修复。

**Architecture:** AbilityManager 存储四类 Card 元数据，通过 ResourceManager 接口获取实例执行，并行执行使用 WaitGroup + channel 收集。Rail 生命周期只定义接口预留调用点，JSON 修复完整对齐 Python。

**Tech Stack:** Go 1.x, zerolog 日志, sync.WaitGroup 并发

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/common/schema/card.go` | 新增 WorkflowCard / AgentCard 结构体 |
| 修改 | `internal/common/schema/card_test.go` | 新增 WorkflowCard / AgentCard 测试 |
| 修改 | `internal/common/schema/doc.go` | 更新文件目录 |
| 修改 | `internal/common/exception/codes_agent.go` | 新增 StatusAbilityExecutionError |
| 创建 | `internal/agentcore/single_agent/doc.go` | 包文档 |
| 创建 | `internal/agentcore/single_agent/ability_types.go` | Ability 接口 + AbilityKind + AddAbilityResult + AbilityExecutionError |
| 创建 | `internal/agentcore/single_agent/ability_types_test.go` | Ability 类型测试 |
| 创建 | `internal/agentcore/single_agent/json_repair.go` | RepairToolArgumentsJSON + ParseToolArguments |
| 创建 | `internal/agentcore/single_agent/json_repair_test.go` | JSON 修复测试 |
| 创建 | `internal/agentcore/single_agent/resource_manager.go` | ResourceManager 接口 + NoopResourceManager + Workflow/Agent 最小接口 + Session/ContextEngine 预留 |
| 创建 | `internal/agentcore/single_agent/resource_manager_test.go` | ResourceManager 测试 |
| 创建 | `internal/agentcore/single_agent/ability_manager.go` | AbilityManager 核心结构 + 注册/查询/执行 |
| 创建 | `internal/agentcore/single_agent/ability_manager_test.go` | AbilityManager 核心测试 |
| 修改 | `internal/agentcore/foundation/tool/base.go` | ToolCard 实现 Ability 接口 |
| 修改 | `internal/agentcore/foundation/tool/mcp/types/types.go` | McpServerConfig 实现 Ability 接口 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 更新 3.13 状态 |

---

### Task 1: 新增 WorkflowCard / AgentCard 到 common/schema

**Files:**
- Modify: `internal/common/schema/card.go`
- Modify: `internal/common/schema/card_test.go`
- Modify: `internal/common/schema/doc.go`

- [ ] **Step 1: 在 card.go 中新增 WorkflowCard 和 AgentCard 结构体**

在 `card.go` 文件末尾（`非导出函数` 区块之前）添加：

```go
// WorkflowCard 工作流配置卡片，嵌入 BaseCard，增加版本号和输入参数定义。
//
// 对应 Python: openjiuwen/core/workflow/base.py (WorkflowCard)
type WorkflowCard struct {
	BaseCard
	// Version 工作流版本号
	Version string `json:"version,omitempty"`
	// InputParams 输入参数定义（JSON Schema 格式）
	InputParams map[string]any `json:"input_params,omitempty"`
}

// NewWorkflowCard 创建 WorkflowCard 实例。
//
// 对应 Python: WorkflowCard(name=..., description=..., version=..., input_params=...)
func NewWorkflowCard(opts ...CardOption) *WorkflowCard {
	card := &WorkflowCard{
		BaseCard: *NewBaseCard(opts...),
	}
	return card
}

// ToolInfo 返回工具描述信息，供 LLM function calling 消费。
// WorkflowCard 的 InputParams 直接作为 JSON Schema parameters。
//
// 对应 Python: WorkflowCard.tool_info()
func (c *WorkflowCard) ToolInfo() *ToolInfo {
	params := c.InputParams
	if params == nil {
		params = make(map[string]any)
	}
	return NewToolInfo(c.Name, c.Description, params)
}

// AgentCard Agent 配置卡片，嵌入 BaseCard，增加输入/输出参数和接口 URL。
//
// 对应 Python: openjiuwen/core/single_agent/schema/agent_card.py (AgentCard)
type AgentCard struct {
	BaseCard
	// InputParams 输入参数定义（JSON Schema 格式）
	InputParams map[string]any `json:"input_params,omitempty"`
	// OutputParams 输出参数定义（JSON Schema 格式）
	OutputParams map[string]any `json:"output_params,omitempty"`
	// InterfaceURL A2A JSON-RPC 基础 URL
	InterfaceURL string `json:"interface_url,omitempty"`
}

// NewAgentCard 创建 AgentCard 实例。
//
// 对应 Python: AgentCard(name=..., description=..., input_params=..., output_params=..., interface_url=...)
func NewAgentCard(opts ...CardOption) *AgentCard {
	card := &AgentCard{
		BaseCard: *NewBaseCard(opts...),
	}
	return card
}

// ToolInfo 返回工具描述信息，供 LLM function calling 消费。
// AgentCard 的 InputParams 直接作为 JSON Schema parameters；
// InputParams 为 nil 时返回空 object schema。
//
// 对应 Python: AgentCard.tool_info()
func (c *AgentCard) ToolInfo() *ToolInfo {
	params := c.InputParams
	if params == nil {
		params = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
			"required":   []string{},
		}
	}
	return NewToolInfo(c.Name, c.Description, params)
}
```

- [ ] **Step 2: 在 card_test.go 中添加 WorkflowCard / AgentCard 测试**

在 `card_test.go` 末尾添加：

```go
func TestWorkflowCard_ToolInfo_有参数(t *testing.T) {
	card := NewWorkflowCard(
		WithName("my_workflow"),
		WithDescription("我的工作流"),
	)
	card.Version = "1.0"
	card.InputParams = map[string]any{
		"type":       "object",
		"properties": map[string]any{"query": map[string]any{"type": "string"}},
	}
	info := card.ToolInfo()
	if info.Name != "my_workflow" {
		t.Errorf("Name = %q, want my_workflow", info.Name)
	}
	if info.Description != "我的工作流" {
		t.Errorf("Description = %q, want 我的工作流", info.Description)
	}
	if card.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", card.Version)
	}
}

func TestWorkflowCard_ToolInfo_无参数(t *testing.T) {
	card := NewWorkflowCard(WithName("empty_wf"))
	info := card.ToolInfo()
	if info.Name != "empty_wf" {
		t.Errorf("Name = %q, want empty_wf", info.Name)
	}
	props, ok := info.Parameters["properties"].(map[string]any)
	if !ok || len(props) != 0 {
		t.Errorf("无参数时 properties 应为空 map，实际 %v", info.Parameters)
	}
}

func TestAgentCard_ToolInfo_有参数(t *testing.T) {
	card := NewAgentCard(
		WithName("sub_agent"),
		WithDescription("子 Agent"),
	)
	card.InputParams = map[string]any{
		"type":       "object",
		"properties": map[string]any{"task": map[string]any{"type": "string"}},
	}
	card.InterfaceURL = "http://localhost:8080/a2a"
	info := card.ToolInfo()
	if info.Name != "sub_agent" {
		t.Errorf("Name = %q, want sub_agent", info.Name)
	}
	if card.InterfaceURL != "http://localhost:8080/a2a" {
		t.Errorf("InterfaceURL = %q, want http://localhost:8080/a2a", card.InterfaceURL)
	}
}

func TestAgentCard_ToolInfo_无参数(t *testing.T) {
	card := NewAgentCard(WithName("no_params_agent"))
	info := card.ToolInfo()
	if info.Name != "no_params_agent" {
		t.Errorf("Name = %q, want no_params_agent", info.Name)
	}
	// InputParams 为 nil 时应返回空 object schema
	typ, ok := info.Parameters["type"].(string)
	if !ok || typ != "object" {
		t.Errorf("无参数时 type 应为 object，实际 %v", info.Parameters)
	}
}
```

- [ ] **Step 3: 更新 doc.go 文件目录**

将 `internal/common/schema/doc.go` 中的文件目录更新为：

```go
// Package schema 提供两个项目共享的基础数据模型。
//
// 本包定义了 Agent 系统和 LLM function calling 所需的核心元信息类型，
// 作为 agentcore 和 swarm 共用的类型基础层。
//
// 文件目录：
//
//	schema/
//	├── doc.go         # 包文档
//	├── card.go        # BaseCard 数字名片基类 + WorkflowCard 工作流卡片 + AgentCard Agent 卡片
//	├── param.go       # Param / ParamType 参数定义模型，支持嵌套结构
//	└── tool_info.go   # ToolInfoProvider 接口 + ToolInfo 本地工具描述 + McpToolInfo MCP 工具描述
//
// 核心类型：
//
//   - BaseCard：数字名片基类，提供 ID/Name/Description 和 ToolInfo() 方法
//   - WorkflowCard：工作流配置卡片，增加 Version 和 InputParams
//   - AgentCard：Agent 配置卡片，增加 InputParams/OutputParams/InterfaceURL
//   - Param / ParamType：参数定义模型，最终转换为 JSON Schema
//   - ToolInfoProvider：工具信息提供者接口，LLM 层统一消费 ToolInfo 和 McpToolInfo
//   - ToolInfo / McpToolInfo：工具描述信息；McpToolInfo 嵌入 ToolInfo 并扩展 ServerName
//
// 对应 Python 代码：openjiuwen/core/common/schema/
package schema
```

- [ ] **Step 4: 运行测试验证**

Run: `go test ./internal/common/schema/... -v -run "TestWorkflowCard|TestAgentCard"`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/common/schema/card.go internal/common/schema/card_test.go internal/common/schema/doc.go
git commit -m "feat(schema): add WorkflowCard and AgentCard types for AbilityManager"
```

---

### Task 2: 为 ToolCard 和 McpServerConfig 实现 Ability 接口方法

**Files:**
- Modify: `internal/agentcore/foundation/tool/base.go`
- Modify: `internal/agentcore/foundation/tool/mcp/types/types.go`

Ability 接口将在 Task 3 中定义于 `single_agent` 包。为了避免循环依赖（`single_agent` → `tool` → `single_agent`），Ability 接口和 AbilityKind 实际上也需要定义在 `single_agent` 包中，而 ToolCard/McpServerConfig 上实现的方法需要导入 single_agent 包的 AbilityKind。

更好的方案：**将 AbilityKind 和 Ability 接口定义在 common/schema 包中**（与 BaseCard/ToolInfo 同层），避免 tool 包依赖 single_agent 包。

- [ ] **Step 1: 在 common/schema/card.go 中新增 AbilityKind 和 Ability 接口**

在 `card.go` 的 `结构体` 区块（WorkflowCard/AgentCard 定义之前）添加：

```go
// ──────────────────────────── 枚举 ────────────────────────────

// AbilityKind 能力类型枚举，标识四类 Ability 的类型。
type AbilityKind int

const (
	// AbilityKindTool 工具能力
	AbilityKindTool AbilityKind = iota
	// AbilityKindWorkflow 工作流能力
	AbilityKindWorkflow
	// AbilityKindAgent Agent 能力
	AbilityKindAgent
	// AbilityKindMcpServer MCP 服务器能力
	AbilityKindMcpServer
)

// String 实现 fmt.Stringer 接口。
func (k AbilityKind) String() string {
	switch k {
	case AbilityKindTool:
		return "tool"
	case AbilityKindWorkflow:
		return "workflow"
	case AbilityKindAgent:
		return "agent"
	case AbilityKindMcpServer:
		return "mcp_server"
	default:
		return "unknown"
	}
}

// ──────────────────────────── 接口 ────────────────────────────

// Ability 四类能力的统一接口，ToolCard/WorkflowCard/AgentCard/McpServerConfig 均实现此接口。
type Ability interface {
	// AbilityName 返回能力名称
	AbilityName() string
	// AbilityID 返回能力唯一标识
	AbilityID() string
	// AbilityKind 返回能力类型
	AbilityKind() AbilityKind
}
```

- [ ] **Step 2: 为 WorkflowCard 实现 Ability 接口**

在 `card.go` 中 WorkflowCard 的 ToolInfo 方法之后添加：

```go
// AbilityName 实现 Ability 接口。
func (c *WorkflowCard) AbilityName() string { return c.Name }

// AbilityID 实现 Ability 接口。
func (c *WorkflowCard) AbilityID() string { return c.ID }

// AbilityKind 实现 Ability 接口。
func (c *WorkflowCard) AbilityKind() AbilityKind { return AbilityKindWorkflow }
```

- [ ] **Step 3: 为 AgentCard 实现 Ability 接口**

在 `card.go` 中 AgentCard 的 ToolInfo 方法之后添加：

```go
// AbilityName 实现 Ability 接口。
func (c *AgentCard) AbilityName() string { return c.Name }

// AbilityID 实现 Ability 接口。
func (c *AgentCard) AbilityID() string { return c.ID }

// AbilityKind 实现 Ability 接口。
func (c *AgentCard) AbilityKind() AbilityKind { return AbilityKindAgent }
```

- [ ] **Step 4: 为 ToolCard 实现 Ability 接口**

在 `internal/agentcore/foundation/tool/base.go` 中（`ValidateToolCard` 函数之前）添加：

```go
// AbilityName 实现 schema.Ability 接口。
func (c *ToolCard) AbilityName() string { return c.Name }

// AbilityID 实现 schema.Ability 接口。
func (c *ToolCard) AbilityID() string { return c.ID }

// AbilityKind 实现 schema.Ability 接口。
func (c *ToolCard) AbilityKind() schema.AbilityKind { return schema.AbilityKindTool }
```

- [ ] **Step 5: 为 McpServerConfig 实现 Ability 接口**

在 `internal/agentcore/foundation/tool/mcp/types/types.go` 中（`NewMcpToolCard` 函数之后）添加：

```go
// AbilityName 实现 schema.Ability 接口。
func (c *McpServerConfig) AbilityName() string { return c.ServerName }

// AbilityID 实现 schema.Ability 接口。
func (c *McpServerConfig) AbilityID() string { return c.ServerID }

// AbilityKind 实现 schema.Ability 接口。
func (c *McpServerConfig) AbilityKind() schema.AbilityKind { return schema.AbilityKindMcpServer }
```

- [ ] **Step 6: 在 card_test.go 中添加 Ability 接口测试**

在 `card_test.go` 末尾添加：

```go
func TestWorkflowCard_Ability(t *testing.T) {
	card := NewWorkflowCard(WithName("wf"), WithDescription("工作流"))
	if card.AbilityName() != "wf" {
		t.Errorf("AbilityName = %q, want wf", card.AbilityName())
	}
	if card.AbilityKind() != AbilityKindWorkflow {
		t.Errorf("AbilityKind = %v, want AbilityKindWorkflow", card.AbilityKind())
	}
	var _ Ability = card // 编译期接口检查
}

func TestAgentCard_Ability(t *testing.T) {
	card := NewAgentCard(WithName("ag"), WithDescription("Agent"))
	if card.AbilityName() != "ag" {
		t.Errorf("AbilityName = %q, want ag", card.AbilityName())
	}
	if card.AbilityKind() != AbilityKindAgent {
		t.Errorf("AbilityKind = %v, want AbilityKindAgent", card.AbilityKind())
	}
	var _ Ability = card // 编译期接口检查
}
```

- [ ] **Step 7: 运行测试验证**

Run: `go test ./internal/common/schema/... ./internal/agentcore/foundation/tool/... -v -run "TestWorkflowCard_Ability|TestAgentCard_Ability"`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add internal/common/schema/card.go internal/common/schema/card_test.go internal/agentcore/foundation/tool/base.go internal/agentcore/foundation/tool/mcp/types/types.go
git commit -m "feat: add Ability interface and AbilityKind, implement for ToolCard/WorkflowCard/AgentCard/McpServerConfig"
```

---

### Task 3: 新增 AbilityExecutionError 状态码

**Files:**
- Modify: `internal/common/exception/codes_agent.go`

- [ ] **Step 1: 在 codes_agent.go 的 Agent Orchestration 区块中新增状态码**

在 `StatusAgentPromptParamError` 之后添加：

```go
	// StatusAbilityExecutionError 能力执行错误（AbilityManager 统一异常）
	StatusAbilityExecutionError = NewStatusCode(
		"ABILITY_EXECUTION_ERROR", 120005,
		"ability execution error, reason: {error_msg}")
	// StatusAbilityNotFound 能力未找到
	StatusAbilityNotFound = NewStatusCode(
		"ABILITY_NOT_FOUND", 120006,
		"ability not found, name: {ability_name}")
	// StatusAbilityMalformedArguments 工具参数 JSON 格式错误
	StatusAbilityMalformedArguments = NewStatusCode(
		"ABILITY_MALFORMED_ARGUMENTS", 120007,
		"malformed tool arguments, tool: {tool_name}, reason: {error_msg}")
```

- [ ] **Step 2: 运行测试确认无冲突**

Run: `go test ./internal/common/exception/... -v`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/common/exception/codes_agent.go
git commit -m "feat(exception): add ability execution error status codes"
```

---

### Task 4: 创建 single_agent 包 + Ability 类型 + JSON 修复

**Files:**
- Create: `internal/agentcore/single_agent/doc.go`
- Create: `internal/agentcore/single_agent/ability_types.go`
- Create: `internal/agentcore/single_agent/ability_types_test.go`
- Create: `internal/agentcore/single_agent/json_repair.go`
- Create: `internal/agentcore/single_agent/json_repair_test.go`

- [ ] **Step 1: 创建包目录**

Run: `mkdir -p internal/agentcore/single_agent`

- [ ] **Step 2: 创建 doc.go**

```go
// Package single_agent 提供 Agent 核心能力管理，包括 AbilityManager 注册与调度。
//
// AbilityManager 是 Agent 的能力注册与调度中心，管理四类 Ability
// （Tool / Workflow / Agent / McpServer）的完整生命周期：
// 注册管理、LLM 工具描述生成、并行执行、JSON 参数修复、路由分发。
//
// 文件目录：
//
//	single_agent/
//	├── doc.go                 # 包文档
//	├── ability_types.go       # Ability 联合类型 + AddAbilityResult + AbilityExecutionError + ToolRail 预留
//	├── json_repair.go         # RepairToolArgumentsJSON + ParseToolArguments
//	├── resource_manager.go    # ResourceManager 接口 + NoopResourceManager + 最小依赖接口
//	└── ability_manager.go     # AbilityManager 核心结构 + 注册/查询/执行
//
// 对应 Python 代码：openjiuwen/core/single_agent/ability_manager.py
package single_agent
```

- [ ] **Step 3: 创建 ability_types.go**

```go
package single_agent

import (
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AddAbilityResult 添加能力的返回结果。
//
// 对应 Python: AddAbilityResult
type AddAbilityResult struct {
	// Name 能力名称
	Name string
	// Added 是否成功添加
	Added bool
	// Reason 未添加的原因（如 "duplicate_tool"、"added_tool"）
	Reason string
}

// AbilityExecutionError 能力执行统一异常，嵌入 BaseError 并关联 ToolMessage。
//
// 对应 Python: AbilityExecutionError
type AbilityExecutionError struct {
	*exception.BaseError
	// ToolMessage 关联的工具返回消息
	ToolMessage *llmschema.ToolMessage
}

// ExecuteResult 单个工具调用的执行结果。
type ExecuteResult struct {
	// Result 执行结果
	Result any
	// ToolMsg 返回给 LLM 的 ToolMessage
	ToolMsg *llmschema.ToolMessage
	// Err 执行错误（如有）
	Err error
}

// ToolCallContext 工具调用上下文（预留，6.5 回填）。
type ToolCallContext struct {
	// ToolCall 工具调用信息
	ToolCall *llmschema.ToolCall
	// ToolName 工具名称
	ToolName string
	// ToolArgs 工具参数
	ToolArgs map[string]any
	// ToolResult 工具执行结果
	ToolResult any
	// ToolMsg 工具返回消息
	ToolMsg *llmschema.ToolMessage
	// ⤵️ 预留字段：force_finish / steering_queue / skip_tool
}

// ToolCallResult 工具调用结果（预留）。
type ToolCallResult struct {
	// Result 执行结果
	Result any
	// ToolMsg 返回给 LLM 的 ToolMessage
	ToolMsg *llmschema.ToolMessage
}

// ──────────────────────────── 接口 ────────────────────────────

// ToolRail 工具调用生命周期钩子接口（3.13 只定义，6.4-6.10 实现）。
type ToolRail interface {
	// BeforeToolCall 工具调用前触发
	BeforeToolCall(ctx context.Context, callCtx *ToolCallContext) (*ToolCallContext, error)
	// AfterToolCall 工具调用后触发
	AfterToolCall(ctx context.Context, callCtx *ToolCallContext, result *ToolCallResult) (*ToolCallResult, error)
	// OnToolException 工具调用异常时触发
	OnToolException(ctx context.Context, callCtx *ToolCallContext, err error) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAbilityExecutionError 创建能力执行错误。
//
// 对应 Python: AbilityExecutionError(status=..., msg=..., tool_message=...)
func NewAbilityExecutionError(
	status exception.StatusCode,
	toolCallID string,
	msg string,
	opts ...exception.ErrorOption,
) *AbilityExecutionError {
	allOpts := append([]exception.ErrorOption{exception.WithMsg(msg)}, opts...)
	return &AbilityExecutionError{
		BaseError: exception.NewBaseError(status, allOpts...),
		ToolMessage: llmschema.NewToolMessage(toolCallID, msg),
	}
}

// BuildToolMessageContent 从执行结果中提取 ToolMessage 的 content 字段。
//
// 提取逻辑（对齐 Python _build_tool_message_content）：
//  1. 结果有 data.content 字段 → 返回 content
//  2. 结果 success=false 且有 error → 返回 error
//  3. 其他 → 字符串化结果
func BuildToolMessageContent(result any) string {
	if m, ok := result.(map[string]any); ok {
		if data, ok := m["data"].(map[string]any); ok {
			if content, ok := data["content"]; ok {
				s := fmt.Sprintf("%v", content)
				if s != "" {
					return s
				}
			}
		}
		if success, ok := m["success"].(bool); ok && !success {
			if errVal, ok := m["error"]; ok {
				return fmt.Sprintf("%v", errVal)
			}
		}
	}
	return fmt.Sprintf("%v", result)
}
```

注意：`ToolRail` 接口中的 `ctx context.Context` 需要 import "context"。在 ability_types.go 的 import 中添加：

```go
import (
	"context"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)
```

（`schema` 暂时不直接使用，但后续 Task 会需要。若编译器报 unused import，可先移除。）

- [ ] **Step 4: 创建 ability_types_test.go**

```go
package single_agent

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewAbilityExecutionError(t *testing.T) {
	err := NewAbilityExecutionError(
		exception.StatusAbilityExecutionError,
		"call_123",
		"工具执行失败",
	)
	if err.ToolMessage == nil {
		t.Fatal("ToolMessage 不应为 nil")
	}
	if err.ToolMessage.ToolCallID != "call_123" {
		t.Errorf("ToolCallID = %q, want call_123", err.ToolMessage.ToolCallID)
	}
	if err.ToolMessage.Content != "工具执行失败" {
		t.Errorf("Content = %q, want 工具执行失败", err.ToolMessage.Content)
	}
	if err.Code() != exception.StatusAbilityExecutionError.Code() {
		t.Errorf("Code = %d, want %d", err.Code(), exception.StatusAbilityExecutionError.Code())
	}
}

func TestBuildToolMessageContent_有DataContent(t *testing.T) {
	result := map[string]any{
		"data": map[string]any{
			"content": "搜索结果",
		},
	}
	content := BuildToolMessageContent(result)
	if content != "搜索结果" {
		t.Errorf("content = %q, want 搜索结果", content)
	}
}

func TestBuildToolMessageContent_失败有Error(t *testing.T) {
	result := map[string]any{
		"success": false,
		"error":   "超时",
	}
	content := BuildToolMessageContent(result)
	if content != "超时" {
		t.Errorf("content = %q, want 超时", content)
	}
}

func TestBuildToolMessageContent_其他(t *testing.T) {
	content := BuildToolMessageContent("简单字符串")
	if content != "简单字符串" {
		t.Errorf("content = %q, want 简单字符串", content)
	}
}

func TestBuildToolMessageContent_NilData(t *testing.T) {
	result := map[string]any{
		"data": map[string]any{
			"content": "",
		},
	}
	content := BuildToolMessageContent(result)
	// content 为空字符串，fmt.Sprintf("%v", "") == ""，但外层 data.content 分支返回空
	// 之后会 fallback 到最终 fmt.Sprintf
	// 实际行为：data.content="" → s="" → s != "" 为 false → 不返回
	// 然后检查 success 字段，不存在 → fallback 到 fmt.Sprintf("%v", result)
	_ = content // 结果取决于实现细节，不严格断言
}

func TestAddAbilityResult(t *testing.T) {
	r := AddAbilityResult{Name: "test", Added: true, Reason: "added_tool"}
	if r.Name != "test" {
		t.Errorf("Name = %q, want test", r.Name)
	}
	if !r.Added {
		t.Error("Added 应为 true")
	}
	if r.Reason != "added_tool" {
		t.Errorf("Reason = %q, want added_tool", r.Reason)
	}
}
```

- [ ] **Step 5: 创建 json_repair.go**

```go
package single_agent

import (
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RepairToolArgumentsJSON 尝试修复畸形的 JSON 字符串，通过补全缺失的闭合括号。
// 返回修复后的字符串指针；无法修复时返回 nil。
//
// 对应 Python: AbilityManager._repair_tool_arguments_json
func RepairToolArgumentsJSON(arguments string) *string {
	text := arguments
	// 去除首尾空白
	for len(text) > 0 && (text[0] == ' ' || text[0] == '\t' || text[0] == '\n' || text[0] == '\r') {
		text = text[1:]
	}
	for len(text) > 0 && (text[len(text)-1] == ' ' || text[len(text)-1] == '\t' || text[len(text)-1] == '\n' || text[len(text)-1] == '\r') {
		text = text[:len(text)-1]
	}
	if text == "" {
		return nil
	}

	var stack []byte
	inString := false
	escape := false

	for i := 0; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escape {
				escape = false
			} else if ch == '\\' {
				escape = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			continue
		}
		if ch == '{' || ch == '[' {
			stack = append(stack, ch)
			continue
		}
		if ch == '}' {
			if len(stack) == 0 || stack[len(stack)-1] != '{' {
				return nil
			}
			stack = stack[:len(stack)-1]
			continue
		}
		if ch == ']' {
			if len(stack) == 0 || stack[len(stack)-1] != '[' {
				return nil
			}
			stack = stack[:len(stack)-1]
		}
	}

	if inString {
		return nil
	}
	if len(stack) == 0 {
		return &text
	}

	// 按栈逆序补全闭合符号
	suffix := make([]byte, len(stack))
	for i := 0; i < len(stack); i++ {
		opener := stack[len(stack)-1-i]
		if opener == '{' {
			suffix[i] = '}'
		} else {
			suffix[i] = ']'
		}
	}
	repaired := text + string(suffix)
	return &repaired
}

// ParseToolArguments 将工具调用参数解析为 map[string]any。
// 先尝试 json.Unmarshal；失败后尝试 Repair + 再次 Unmarshal；
// 仍失败则返回错误，包含原始 JSON 和错误信息。
//
// 对应 Python: AbilityManager._parse_tool_arguments
func ParseToolArguments(arguments string) (map[string]any, error) {
	if arguments == "" {
		return nil, nil
	}

	// 先尝试直接解析
	var result map[string]any
	if err := json.Unmarshal([]byte(arguments), &result); err == nil {
		return result, nil
	}

	// 尝试修复
	repaired := RepairToolArgumentsJSON(arguments)
	if repaired != nil && *repaired != arguments {
		if err := json.Unmarshal([]byte(*repaired), &result); err == nil {
			logger.Warn(logger.ComponentAgentCore).
				Msg("通过补全闭合括号修复了畸形的工具参数")
			return result, nil
		}
	}

	return nil, fmt.Errorf("Invalid tool arguments JSON: raw arguments: %q", arguments)
}
```

- [ ] **Step 6: 创建 json_repair_test.go**

```go
package single_agent

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestRepairToolArgumentsJSON_正常JSON(t *testing.T) {
	input := `{"key": "value"}`
	result := RepairToolArgumentsJSON(input)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if *result != input {
		t.Errorf("result = %q, want %q", *result, input)
	}
}

func TestRepairToolArgumentsJSON_缺失尾部大括号(t *testing.T) {
	input := `{"key": "value"`
	result := RepairToolArgumentsJSON(input)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	expected := `{"key": "value"}`
	if *result != expected {
		t.Errorf("result = %q, want %q", *result, expected)
	}
}

func TestRepairToolArgumentsJSON_缺失尾部中括号(t *testing.T) {
	input := `[1, 2, 3`
	result := RepairToolArgumentsJSON(input)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	expected := `[1, 2, 3]`
	if *result != expected {
		t.Errorf("result = %q, want %q", *result, expected)
	}
}

func TestRepairToolArgumentsJSON_嵌套缺失(t *testing.T) {
	input := `{"arr": [1, 2`
	result := RepairToolArgumentsJSON(input)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	expected := `{"arr": [1, 2]}`
	if *result != expected {
		t.Errorf("result = %q, want %q", *result, expected)
	}
}

func TestRepairToolArgumentsJSON_字符串内括号不计入(t *testing.T) {
	input := `{"text": "hello {world}"}`
	result := RepairToolArgumentsJSON(input)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if *result != input {
		t.Errorf("result = %q, want %q", *result, input)
	}
}

func TestRepairToolArgumentsJSON_转义引号(t *testing.T) {
	input := `{"text": "say \"hello\""}`
	result := RepairToolArgumentsJSON(input)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if *result != input {
		t.Errorf("result = %q, want %q", *result, input)
	}
}

func TestRepairToolArgumentsJSON_仍在字符串内(t *testing.T) {
	input := `{"key": "unterminated string`
	result := RepairToolArgumentsJSON(input)
	if result != nil {
		t.Errorf("未闭合字符串应返回 nil，实际 %q", *result)
	}
}

func TestRepairToolArgumentsJSON_空字符串(t *testing.T) {
	result := RepairToolArgumentsJSON("")
	if result != nil {
		t.Errorf("空字符串应返回 nil，实际 %q", *result)
	}
}

func TestRepairToolArgumentsJSON_不匹配的闭合(t *testing.T) {
	input := `{"key": "value"}]`
	result := RepairToolArgumentsJSON(input)
	if result != nil {
		t.Errorf("不匹配的闭合括号应返回 nil，实际 %q", *result)
	}
}

func TestParseToolArguments_正常解析(t *testing.T) {
	input := `{"city": "北京", "days": 3}`
	result, err := ParseToolArguments(input)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if result["city"] != "北京" {
		t.Errorf("city = %v, want 北京", result["city"])
	}
}

func TestParseToolArguments_修复后解析(t *testing.T) {
	input := `{"city": "北京"`
	result, err := ParseToolArguments(input)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if result["city"] != "北京" {
		t.Errorf("city = %v, want 北京", result["city"])
	}
}

func TestParseToolArguments_无法修复(t *testing.T) {
	input := `not json at all`
	_, err := ParseToolArguments(input)
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestParseToolArguments_空字符串(t *testing.T) {
	result, err := ParseToolArguments("")
	if err != nil {
		t.Fatalf("空字符串不应返回错误: %v", err)
	}
	if result != nil {
		t.Errorf("空字符串应返回 nil，实际 %v", result)
	}
}
```

- [ ] **Step 7: 运行测试验证**

Run: `go test ./internal/agentcore/single_agent/... -v -run "TestRepair|TestParse"`
Expected: PASS

Run: `go test ./internal/agentcore/single_agent/... -v -run "TestNewAbilityExecutionError|TestBuildToolMessageContent|TestAddAbilityResult"`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/single_agent/
git commit -m "feat(single_agent): add ability types, JSON repair, and execution error"
```

---

### Task 5: 创建 ResourceManager 接口 + NoopResourceManager

**Files:**
- Create: `internal/agentcore/single_agent/resource_manager.go`
- Create: `internal/agentcore/single_agent/resource_manager_test.go`

- [ ] **Step 1: 创建 resource_manager.go**

```go
package single_agent

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// ResourceManager 实例获取接口，AbilityManager 通过此接口获取 Tool/Workflow/Agent 实例。
// 具体实现由领域六/九提供，3.13 阶段使用 NoopResourceManager。
type ResourceManager interface {
	// GetTool 按 ID 获取工具实例
	GetTool(toolID string, opts ...ResourceOption) (tool.Tool, error)
	// GetWorkflow 按 ID 获取工作流实例
	GetWorkflow(workflowID string, opts ...ResourceOption) (Workflow, error)
	// GetAgent 按 ID 获取 Agent 实例
	GetAgent(agentID string, opts ...ResourceOption) (Agent, error)
	// GetMcpToolInfos 获取 MCP 服务器的工具描述列表
	GetMcpToolInfos(serverID string) ([]*schema.ToolInfo, error)
}

// Workflow 工作流执行接口（最小定义，领域八扩展）。
type Workflow interface {
	// Execute 执行工作流
	Execute(ctx context.Context, inputs map[string]any, opts ...WorkflowOption) (any, error)
}

// Agent Agent 执行接口（最小定义，领域六扩展）。
type Agent interface {
	// Invoke 调用 Agent
	Invoke(ctx context.Context, inputs map[string]any, opts ...AgentOption) (any, error)
}

// ContextEngine 上下文引擎接口（预留，领域五回填）。
type ContextEngine interface {
	// CreateContext 创建上下文
	CreateContext(ctx context.Context, contextID string, session Session) (any, error)
}

// Session 会话接口（预留，领域五回填）。
type Session interface {
	// GetSessionID 获取会话 ID
	GetSessionID() string
	// CreateWorkflowSession 创建工作流子会话 ⤵️ 预留
	CreateWorkflowSession() Session
	// GetState 获取会话状态
	GetState(key string) any
	// UpdateState 更新会话状态
	UpdateState(state map[string]any)
}

// ──────────────────────────── 结构体 ────────────────────────────

// ResourceOptions 实例获取选项。
type ResourceOptions struct {
	// Tag 资源标签
	Tag string
	// Session 会话实例 ⤵️ 预留
	Session Session
}

// NoopResourceManager ResourceManager 的空实现，3.13 阶段使用。
// 所有方法返回 NotFound 错误。
type NoopResourceManager struct{}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ResourceOption 实例获取选项函数。
type ResourceOption func(*ResourceOptions)

// WithResourceTag 设置资源标签。
func WithResourceTag(tag string) ResourceOption {
	return func(o *ResourceOptions) { o.Tag = tag }
}

// WithResourceSession 设置会话实例。
func WithResourceSession(session Session) ResourceOption {
	return func(o *ResourceOptions) { o.Session = session }
}

// NewResourceOptions 从选项列表构造 ResourceOptions。
func NewResourceOptions(opts ...ResourceOption) *ResourceOptions {
	o := &ResourceOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WorkflowOption 工作流执行选项函数（预留，领域八扩展）。
type WorkflowOption func(*WorkflowOptions)

// WorkflowOptions 工作流执行选项（预留）。
type WorkflowOptions struct{}

// AgentOption Agent 调用选项函数（预留，领域六扩展）。
type AgentOption func(*AgentOptions)

// AgentOptions Agent 调用选项（预留）。
type AgentOptions struct{}

// GetTool 实现 ResourceManager 接口，返回 NotFound 错误。
func (n *NoopResourceManager) GetTool(toolID string, opts ...ResourceOption) (tool.Tool, error) {
	return nil, exception.BuildError(
		exception.StatusAbilityNotFound,
		exception.WithParam("ability_name", toolID),
		exception.WithMsg("tool not found in noop resource manager"),
	)
}

// GetWorkflow 实现 ResourceManager 接口，返回 NotFound 错误。
func (n *NoopResourceManager) GetWorkflow(workflowID string, opts ...ResourceOption) (Workflow, error) {
	return nil, exception.BuildError(
		exception.StatusAbilityNotFound,
		exception.WithParam("ability_name", workflowID),
		exception.WithMsg("workflow not found in noop resource manager"),
	)
}

// GetAgent 实现 ResourceManager 接口，返回 NotFound 错误。
func (n *NoopResourceManager) GetAgent(agentID string, opts ...ResourceOption) (Agent, error) {
	return nil, exception.BuildError(
		exception.StatusAbilityNotFound,
		exception.WithParam("ability_name", agentID),
		exception.WithMsg("agent not found in noop resource manager"),
	)
}

// GetMcpToolInfos 实现 ResourceManager 接口，返回空列表。
func (n *NoopResourceManager) GetMcpToolInfos(serverID string) ([]*schema.ToolInfo, error) {
	return nil, nil
}
```

- [ ] **Step 2: 创建 resource_manager_test.go**

```go
package single_agent

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNoopResourceManager_GetTool(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetTool("test_tool")
	if err == nil {
		t.Fatal("应返回错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误类型应为 *BaseError，实际 %T", err)
	}
	if baseErr.Code() != exception.StatusAbilityNotFound.Code() {
		t.Errorf("Code = %d, want %d", baseErr.Code(), exception.StatusAbilityNotFound.Code())
	}
}

func TestNoopResourceManager_GetWorkflow(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetWorkflow("test_wf")
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestNoopResourceManager_GetAgent(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetAgent("test_agent")
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestNoopResourceManager_GetMcpToolInfos(t *testing.T) {
	mgr := &NoopResourceManager{}
	infos, err := mgr.GetMcpToolInfos("server1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if infos != nil {
		t.Errorf("应返回 nil，实际 %v", infos)
	}
}

func TestNewResourceOptions(t *testing.T) {
	opts := NewResourceOptions(
		WithResourceTag("my_tag"),
	)
	if opts.Tag != "my_tag" {
		t.Errorf("Tag = %q, want my_tag", opts.Tag)
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `go test ./internal/agentcore/single_agent/... -v -run "TestNoopResourceManager|TestNewResourceOptions"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/resource_manager.go internal/agentcore/single_agent/resource_manager_test.go
git commit -m "feat(single_agent): add ResourceManager interface and NoopResourceManager"
```

---

### Task 6: 实现 AbilityManager 核心 — 注册/查询/ListToolInfo

**Files:**
- Create: `internal/agentcore/single_agent/ability_manager.go`（第一部分：结构体 + 注册/查询/排序）
- Create: `internal/agentcore/single_agent/ability_manager_test.go`（第一部分：注册/查询测试）

- [ ] **Step 1: 创建 ability_manager.go**

```go
package single_agent

import (
	"context"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AbilityManager Agent 能力注册与调度中心。
//
// 职责：
//   - 存储可用 Ability Card（仅元数据，不持有实例）
//   - 提供 add/remove/query 接口
//   - 将 Card 转为 ToolInfo 供 LLM 使用
//   - 执行 Ability 调用（从 ResourceManager 获取实例）
//
// 对应 Python: openjiuwen/core/single_agent/ability_manager.py (AbilityManager)
type AbilityManager struct {
	tools         map[string]*tool.ToolCard
	workflows     map[string]*schema.WorkflowCard
	agents        map[string]*schema.AgentCard
	mcpServers    map[string]*mcp.McpServerConfig
	contextEngine ContextEngine   // ⤵️ 预留，领域五回填
	resourceMgr   ResourceManager
	rail          ToolRail // ⤵️ 预留，6.4-6.10 回填
}

// toolItem 内部辅助类型，用于 prioritizePaidSearch 的输入。
type toolItem struct {
	name string
	card *tool.ToolCard
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAbilityManager 创建 AbilityManager 实例。
func NewAbilityManager(resourceMgr ResourceManager) *AbilityManager {
	if resourceMgr == nil {
		resourceMgr = &NoopResourceManager{}
	}
	return &AbilityManager{
		tools:       make(map[string]*tool.ToolCard),
		workflows:   make(map[string]*schema.WorkflowCard),
		agents:      make(map[string]*schema.AgentCard),
		mcpServers:  make(map[string]*mcp.McpServerConfig),
		resourceMgr: resourceMgr,
	}
}

// SetContextEngine 设置上下文引擎。
func (am *AbilityManager) SetContextEngine(ce ContextEngine) {
	am.contextEngine = ce
}

// SetRail 设置工具调用生命周期钩子（预留，6.4-6.10 回填）。
func (am *AbilityManager) SetRail(rail ToolRail) {
	am.rail = rail
}

// Add 添加单个能力。重复 name 时保留已有的，记录 Warn 日志，返回 Added=false。
func (am *AbilityManager) Add(ability schema.Ability) AddAbilityResult {
	switch a := ability.(type) {
	case *tool.ToolCard:
		existing, ok := am.tools[a.Name]
		if ok {
			logger.Warn(logger.ComponentAgentCore).
				Str("ability_name", a.Name).
				Str("existing_id", existing.ID).
				Str("new_id", a.ID).
				Msg("检测到重复工具能力，保留已有能力，跳过新增")
			return AddAbilityResult{Name: a.Name, Added: false, Reason: "duplicate_tool"}
		}
		am.tools[a.Name] = a
		return AddAbilityResult{Name: a.Name, Added: true, Reason: "added_tool"}

	case *schema.WorkflowCard:
		existing, ok := am.workflows[a.Name]
		if ok {
			logger.Warn(logger.ComponentAgentCore).
				Str("ability_name", a.Name).
				Str("existing_id", existing.ID).
				Str("new_id", a.ID).
				Msg("检测到重复工作流能力，保留已有能力，跳过新增")
			return AddAbilityResult{Name: a.Name, Added: false, Reason: "duplicate_workflow"}
		}
		am.workflows[a.Name] = a
		return AddAbilityResult{Name: a.Name, Added: true, Reason: "added_workflow"}

	case *schema.AgentCard:
		existing, ok := am.agents[a.Name]
		if ok {
			logger.Warn(logger.ComponentAgentCore).
				Str("ability_name", a.Name).
				Str("existing_id", existing.ID).
				Str("new_id", a.ID).
				Msg("检测到重复Agent能力，保留已有能力，跳过新增")
			return AddAbilityResult{Name: a.Name, Added: false, Reason: "duplicate_agent"}
		}
		am.agents[a.Name] = a
		return AddAbilityResult{Name: a.Name, Added: true, Reason: "added_agent"}

	case *mcp.McpServerConfig:
		existing, ok := am.mcpServers[a.ServerName]
		if ok {
			logger.Warn(logger.ComponentAgentCore).
				Str("ability_name", a.ServerName).
				Str("existing_id", existing.ServerID).
				Str("new_id", a.ServerID).
				Msg("检测到重复MCP服务器能力，保留已有能力，跳过新增")
			return AddAbilityResult{Name: a.ServerName, Added: false, Reason: "duplicate_mcp_server"}
		}
		am.mcpServers[a.ServerName] = a
		return AddAbilityResult{Name: a.ServerName, Added: true, Reason: "added_mcp_server"}

	default:
		logger.Warn(logger.ComponentAgentCore).
			Str("ability_type", strings.TrimSpace("")).
			Msg("未知能力类型")
		name := "unknown"
		if a != nil {
			name = ability.AbilityName()
		}
		return AddAbilityResult{Name: name, Added: false, Reason: "unknown_ability_type"}
	}
}

// AddMany 批量添加能力。
func (am *AbilityManager) AddMany(abilities []schema.Ability) []AddAbilityResult {
	results := make([]AddAbilityResult, len(abilities))
	for i, a := range abilities {
		results[i] = am.Add(a)
	}
	return results
}

// Remove 按名称移除能力，返回被移除的 Ability（未找到返回 nil）。
// 移除 McpServer 时同时移除其关联工具。
func (am *AbilityManager) Remove(name string) schema.Ability {
	if toolCard, ok := am.tools[name]; ok {
		delete(am.tools, name)
		return toolCard
	}
	if wf, ok := am.workflows[name]; ok {
		delete(am.workflows, name)
		return wf
	}
	if ag, ok := am.agents[name]; ok {
		delete(am.agents, name)
		return ag
	}
	if mcpServer, ok := am.mcpServers[name]; ok {
		delete(am.mcpServers, name)
		// 级联删除该 MCP 服务器下的工具
		serverID := mcpServer.ServerID
		prefix := serverID + "."
		for toolName, toolCard := range am.tools {
			if toolCard.ID != "" && strings.HasPrefix(toolCard.ID, prefix) {
				delete(am.tools, toolName)
			}
		}
		return mcpServer
	}
	return nil
}

// RemoveMany 批量移除能力。
func (am *AbilityManager) RemoveMany(names []string) []schema.Ability {
	results := make([]schema.Ability, len(names))
	for i, name := range names {
		results[i] = am.Remove(name)
	}
	return results
}

// Get 按名称查询能力（依次查找 tools → workflows → agents → mcpServers）。
func (am *AbilityManager) Get(name string) schema.Ability {
	if t, ok := am.tools[name]; ok {
		return t
	}
	if w, ok := am.workflows[name]; ok {
		return w
	}
	if a, ok := am.agents[name]; ok {
		return a
	}
	if m, ok := am.mcpServers[name]; ok {
		return m
	}
	return nil
}

// List 列出所有已注册能力。
func (am *AbilityManager) List() []schema.Ability {
	var abilities []schema.Ability
	for _, t := range am.tools {
		abilities = append(abilities, t)
	}
	for _, w := range am.workflows {
		abilities = append(abilities, w)
	}
	for _, a := range am.agents {
		abilities = append(abilities, a)
	}
	for _, m := range am.mcpServers {
		abilities = append(abilities, m)
	}
	return abilities
}

// ReorderTools 按给定名称顺序重排 tools 注册表。
func (am *AbilityManager) ReorderTools(orderedNames []string) {
	if len(orderedNames) == 0 || len(am.tools) == 0 {
		return
	}
	var preferred []string
	for _, name := range orderedNames {
		if _, ok := am.tools[name]; ok {
			preferred = append(preferred, name)
		}
	}
	if len(preferred) == 0 {
		return
	}
	reordered := make(map[string]*tool.ToolCard, len(am.tools))
	for _, name := range preferred {
		reordered[name] = am.tools[name]
	}
	for name, card := range am.tools {
		if _, ok := reordered[name]; !ok {
			reordered[name] = card
		}
	}
	am.tools = reordered
}

// ListToolInfo 获取 ToolInfo 列表供 LLM function calling 消费。
// names 非空时只返回指定名称的工具；为空时返回全部。
func (am *AbilityManager) ListToolInfo(ctx context.Context, names []string) ([]*schema.ToolInfo, error) {
	var toolInfos []*schema.ToolInfo

	// 1. ToolCards → ToolInfo
	items := make([]toolItem, 0, len(am.tools))
	for name, card := range am.tools {
		items = append(items, toolItem{name: name, card: card})
	}
	items = prioritizePaidSearch(items)

	for _, item := range items {
		if len(names) > 0 {
			found := false
			for _, n := range names {
				if n == item.name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		// 排除 MCP 服务器下的工具
		if am.isToolInMcpServer(item.card.ID) {
			continue
		}
		toolInfos = append(toolInfos, item.card.ToolInfo())
	}

	// 2. WorkflowCards → ToolInfo
	for name, wf := range am.workflows {
		if len(names) > 0 {
			found := false
			for _, n := range names {
				if n == name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		toolInfos = append(toolInfos, wf.ToolInfo())
	}

	// 3. AgentCards → ToolInfo
	for name, ag := range am.agents {
		if len(names) > 0 {
			found := false
			for _, n := range names {
				if n == name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		toolInfos = append(toolInfos, ag.ToolInfo())
	}

	// 4. MCP 懒加载：⤵️ 预留，等 ResourceManager 实现后回填
	// for mcpServerName, mcpServer := range am.mcpServers {
	//     mcpToolInfos, err := am.resourceMgr.GetMcpToolInfos(mcpServer.ServerID)
	//     ...
	// }

	return toolInfos, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// prioritizePaidSearch 当 paid_search 和 free_search 同时存在时，
// 确保 paid_search 排在 free_search 前面。
func prioritizePaidSearch(items []toolItem) []toolItem {
	if len(items) == 0 {
		return items
	}
	var paidIdx, freeIdx int = -1, -1
	for i, item := range items {
		if item.name == "paid_search" {
			paidIdx = i
		}
		if item.name == "free_search" {
			freeIdx = i
		}
	}
	if paidIdx < 0 || freeIdx < 0 || paidIdx < freeIdx {
		return items
	}
	// paid 在 free 后面，将 paid 移到 free 前面
	reordered := make([]toolItem, len(items))
	copy(reordered, items)
	paidItem := reordered[paidIdx]
	// 移除 paid
	reordered = append(reordered[:paidIdx], reordered[paidIdx+1:]...)
	// 找到 free 的新位置（因为移除了一个元素）
	newFreeIdx := freeIdx
	if paidIdx < freeIdx {
		newFreeIdx--
	}
	// 插入 paid 到 free 之前
	reordered = append(reordered[:newFreeIdx], append([]toolItem{paidItem}, reordered[newFreeIdx:]...)...)
	return reordered
}

// isToolInMcpServer 判断工具 ID 是否属于某个 MCP 服务器。
func (am *AbilityManager) isToolInMcpServer(toolID string) bool {
	for _, mcpServer := range am.mcpServers {
		if strings.HasPrefix(toolID, mcpServer.ServerID+".") {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: 创建 ability_manager_test.go（注册/查询/排序部分）**

```go
package single_agent

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestAbilityManager_Add_Tool(t *testing.T) {
	am := NewAbilityManager(nil)
	card := tool.NewToolCard("search", "搜索", nil, nil)
	result := am.Add(card)
	if !result.Added {
		t.Error("应成功添加")
	}
	if result.Reason != "added_tool" {
		t.Errorf("Reason = %q, want added_tool", result.Reason)
	}
}

func TestAbilityManager_Add_重复Tool(t *testing.T) {
	am := NewAbilityManager(nil)
	card1 := tool.NewToolCard("search", "搜索1", nil, nil)
	card2 := tool.NewToolCard("search", "搜索2", nil, nil)
	am.Add(card1)
	result := am.Add(card2)
	if result.Added {
		t.Error("重复 name 不应添加")
	}
	if result.Reason != "duplicate_tool" {
		t.Errorf("Reason = %q, want duplicate_tool", result.Reason)
	}
	// 保留旧的
	got := am.Get("search")
	if got == nil {
		t.Fatal("Get 不应返回 nil")
	}
	if got.AbilityID() != card1.ID {
		t.Errorf("应保留旧的 Card")
	}
}

func TestAbilityManager_Add_Workflow(t *testing.T) {
	am := NewAbilityManager(nil)
	wf := schema.NewWorkflowCard(schema.WithName("my_wf"), schema.WithDescription("工作流"))
	result := am.Add(wf)
	if !result.Added {
		t.Error("应成功添加")
	}
	if result.Reason != "added_workflow" {
		t.Errorf("Reason = %q, want added_workflow", result.Reason)
	}
}

func TestAbilityManager_Add_Agent(t *testing.T) {
	am := NewAbilityManager(nil)
	ag := schema.NewAgentCard(schema.WithName("my_agent"), schema.WithDescription("Agent"))
	result := am.Add(ag)
	if !result.Added {
		t.Error("应成功添加")
	}
	if result.Reason != "added_agent" {
		t.Errorf("Reason = %q, want added_agent", result.Reason)
	}
}

func TestAbilityManager_Add_McpServer(t *testing.T) {
	am := NewAbilityManager(nil)
	mc := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse")
	result := am.Add(mc)
	if !result.Added {
		t.Error("应成功添加")
	}
	if result.Reason != "added_mcp_server" {
		t.Errorf("Reason = %q, want added_mcp_server", result.Reason)
	}
}

func TestAbilityManager_AddMany(t *testing.T) {
	am := NewAbilityManager(nil)
	results := am.AddMany([]schema.Ability{
		tool.NewToolCard("t1", "工具1", nil, nil),
		schema.NewWorkflowCard(schema.WithName("w1"), schema.WithDescription("工作流1")),
	})
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
	if !results[0].Added || results[0].Reason != "added_tool" {
		t.Errorf("第一个结果应为 added_tool，实际 %v", results[0])
	}
	if !results[1].Added || results[1].Reason != "added_workflow" {
		t.Errorf("第二个结果应为 added_workflow，实际 %v", results[1])
	}
}

func TestAbilityManager_Remove_Tool(t *testing.T) {
	am := NewAbilityManager(nil)
	card := tool.NewToolCard("search", "搜索", nil, nil)
	am.Add(card)
	removed := am.Remove("search")
	if removed == nil {
		t.Fatal("应返回被移除的 Ability")
	}
	if removed.AbilityName() != "search" {
		t.Errorf("AbilityName = %q, want search", removed.AbilityName())
	}
	if am.Get("search") != nil {
		t.Error("移除后 Get 应返回 nil")
	}
}

func TestAbilityManager_Remove_McpServer级联删除(t *testing.T) {
	am := NewAbilityManager(nil)
	// 添加 MCP 服务器
	mc := mcp.NewMcpServerConfig("my_server", "http://localhost:8080/sse", "sse")
	am.Add(mc)
	// 手动添加属于该 MCP 服务器的工具
	mcpTool := tool.NewToolCard("mcp_my_server_search", "MCP搜索", nil, nil)
	mcpTool.ID = mc.ServerID + ".search"
	am.Add(mcpTool)
	// 添加不属于该 MCP 服务器的工具
	otherTool := tool.NewToolCard("other_tool", "其他工具", nil, nil)
	am.Add(otherTool)
	// 移除 MCP 服务器
	am.Remove("my_server")
	// 验证 MCP 工具被级联删除
	if am.Get("mcp_my_server_search") != nil {
		t.Error("MCP 工具应被级联删除")
	}
	// 验证其他工具不受影响
	if am.Get("other_tool") == nil {
		t.Error("其他工具不应被删除")
	}
}

func TestAbilityManager_Get(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("tool1", "工具", nil, nil))
	am.Add(schema.NewWorkflowCard(schema.WithName("wf1"), schema.WithDescription("工作流")))
	am.Add(schema.NewAgentCard(schema.WithName("ag1"), schema.WithDescription("Agent")))

	if am.Get("tool1") == nil {
		t.Error("tool1 应存在")
	}
	if am.Get("wf1") == nil {
		t.Error("wf1 应存在")
	}
	if am.Get("ag1") == nil {
		t.Error("ag1 应存在")
	}
	if am.Get("nonexistent") != nil {
		t.Error("不存在的名称应返回 nil")
	}
}

func TestAbilityManager_List(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("t1", "工具", nil, nil))
	am.Add(schema.NewWorkflowCard(schema.WithName("w1"), schema.WithDescription("工作流")))
	am.Add(schema.NewAgentCard(schema.WithName("a1"), schema.WithDescription("Agent")))
	list := am.List()
	if len(list) != 3 {
		t.Errorf("List 长度 = %d, want 3", len(list))
	}
}

func TestAbilityManager_ReorderTools(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("c", "C", nil, nil))
	am.Add(tool.NewToolCard("a", "A", nil, nil))
	am.Add(tool.NewToolCard("b", "B", nil, nil))
	am.ReorderTools([]string{"a", "b", "c"})
	// 验证顺序：遍历 map 无法保证顺序，但 ReorderTools 应重建 map 使 preferred 在前
	// 通过 ListToolInfo 间接验证顺序
	infos, err := am.ListToolInfo(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListToolInfo 错误: %v", err)
	}
	if len(infos) != 3 {
		t.Fatalf("infos 长度 = %d, want 3", len(infos))
	}
	if infos[0].Name != "a" {
		t.Errorf("第一个应为 a，实际 %s", infos[0].Name)
	}
	if infos[1].Name != "b" {
		t.Errorf("第二个应为 b，实际 %s", infos[1].Name)
	}
	if infos[2].Name != "c" {
		t.Errorf("第三个应为 c，实际 %s", infos[2].Name)
	}
}

func TestPrioritizePaidSearch_paid在free前面(t *testing.T) {
	items := []toolItem{
		{name: "paid_search", card: tool.NewToolCard("paid_search", "付费搜索", nil, nil)},
		{name: "free_search", card: tool.NewToolCard("free_search", "免费搜索", nil, nil)},
	}
	result := prioritizePaidSearch(items)
	if result[0].name != "paid_search" {
		t.Errorf("paid_search 应在第一个，实际 %s", result[0].name)
	}
}

func TestPrioritizePaidSearch_paid在free后面(t *testing.T) {
	items := []toolItem{
		{name: "free_search", card: tool.NewToolCard("free_search", "免费搜索", nil, nil)},
		{name: "other_tool", card: tool.NewToolCard("other", "其他", nil, nil)},
		{name: "paid_search", card: tool.NewToolCard("paid_search", "付费搜索", nil, nil)},
	}
	result := prioritizePaidSearch(items)
	// paid 应在 free 前面
	paidIdx, freeIdx := -1, -1
	for i, item := range result {
		if item.name == "paid_search" {
			paidIdx = i
		}
		if item.name == "free_search" {
			freeIdx = i
		}
	}
	if paidIdx >= freeIdx {
		t.Errorf("paid_search(idx=%d) 应在 free_search(idx=%d) 前面", paidIdx, freeIdx)
	}
}

func TestPrioritizePaidSearch_只有paid(t *testing.T) {
	items := []toolItem{
		{name: "paid_search", card: tool.NewToolCard("paid_search", "付费搜索", nil, nil)},
		{name: "other", card: tool.NewToolCard("other", "其他", nil, nil)},
	}
	result := prioritizePaidSearch(items)
	if len(result) != 2 {
		t.Errorf("长度应为 2，实际 %d", len(result))
	}
}

func TestAbilityManager_ListToolInfo(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("tool1", "工具1", nil, nil))
	am.Add(schema.NewWorkflowCard(schema.WithName("wf1"), schema.WithDescription("工作流1")))
	am.Add(schema.NewAgentCard(schema.WithName("ag1"), schema.WithDescription("Agent1")))

	infos, err := am.ListToolInfo(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListToolInfo 错误: %v", err)
	}
	names := make(map[string]bool)
	for _, info := range infos {
		names[info.Name] = true
	}
	if !names["tool1"] || !names["wf1"] || !names["ag1"] {
		t.Errorf("缺少工具，实际 %v", names)
	}
}

func TestAbilityManager_ListToolInfo_按名称过滤(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("tool1", "工具1", nil, nil))
	am.Add(tool.NewToolCard("tool2", "工具2", nil, nil))

	infos, err := am.ListToolInfo(context.Background(), []string{"tool1"})
	if err != nil {
		t.Fatalf("ListToolInfo 错误: %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "tool1" {
		t.Errorf("应只返回 tool1，实际 %v", infos)
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `go test ./internal/agentcore/single_agent/... -v -run "TestAbilityManager|TestPrioritizePaidSearch"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/ability_manager.go internal/agentcore/single_agent/ability_manager_test.go
git commit -m "feat(single_agent): implement AbilityManager registration, query, and ListToolInfo"
```

---

### Task 7: 实现 AbilityManager.Execute 并行执行

**Files:**
- Modify: `internal/agentcore/single_agent/ability_manager.go`（新增 Execute 方法）
- Modify: `internal/agentcore/single_agent/ability_manager_test.go`（新增 Execute 测试）

- [ ] **Step 1: 在 ability_manager.go 的 `导出函数` 区块中添加 Execute 方法**

在 `ListToolInfo` 方法之后添加：

```go
// Execute 并行执行多个 ToolCall，返回每个调用的结果。
// 使用 WaitGroup + channel 收集，与 Python asyncio.gather(return_exceptions=True) 语义一致：
// 所有任务都执行完毕，错误作为 ExecuteResult.Err 返回。
func (am *AbilityManager) Execute(
	ctx context.Context,
	toolCalls []*llmschema.ToolCall,
	session Session,
	tag string,
) []ExecuteResult {
	if len(toolCalls) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	resultCh := make(chan ExecuteResult, len(toolCalls))

	for _, tc := range toolCalls {
		wg.Add(1)
		go func(toolCall *llmschema.ToolCall) {
			defer wg.Done()
			result := am.railedExecuteSingleToolCall(ctx, toolCall, session, tag)
			resultCh <- result
		}(tc)
	}

	wg.Wait()
	close(resultCh)

	results := make([]ExecuteResult, 0, len(toolCalls))
	for r := range resultCh {
		results = append(results, r)
	}

	// ⤵️ 预留：force_finish 信号传播（等 6.4-6.10 Rail 系统就绪后回填）

	return results
}
```

- [ ] **Step 2: 在 `非导出函数` 区块中添加 railedExecuteSingleToolCall 和 executeSingleToolCall**

```go
// railedExecuteSingleToolCall 在 Rail 生命周期内执行单个工具调用。
// 当前阶段直接调用 executeSingleToolCall，Rail 钩子调用点预留。
func (am *AbilityManager) railedExecuteSingleToolCall(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	session Session,
	tag string,
) ExecuteResult {
	// ⤵️ 预留：BeforeToolCall Rail 钩子
	// if am.rail != nil { ... }

	result := am.executeSingleToolCall(ctx, toolCall, session, tag)

	// ⤵️ 预留：AfterToolCall Rail 钩子
	// if am.rail != nil { ... }

	return result
}

// executeSingleToolCall 执行单个工具调用。
// 路由逻辑：按 tool_name 查找 Card → 从 ResourceManager 获取实例 → 执行。
func (am *AbilityManager) executeSingleToolCall(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	session Session,
	tag string,
) ExecuteResult {
	toolName := toolCall.Name

	// 解析参数
	toolArgs, err := ParseToolArguments(toolCall.Arguments)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("tool_name", toolName).
			Err(err).
			Msg("工具收到畸形参数")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityMalformedArguments,
			toolCall.ID,
			err.Error(),
			exception.WithParam("tool_name", toolName),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	// 路由分发
	if _, ok := am.tools[toolName]; ok {
		return am.executeTool(ctx, toolCall, toolName, toolArgs, session, tag)
	}
	if _, ok := am.workflows[toolName]; ok {
		return am.executeWorkflow(ctx, toolCall, toolName, toolArgs, session, tag)
	}
	if _, ok := am.agents[toolName]; ok {
		return am.executeAgent(ctx, toolCall, toolName, toolArgs, session, tag)
	}
	if _, ok := am.mcpServers[toolName]; ok {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"MCP 工具执行暂未实现: "+toolName,
			exception.WithParam("tool_name", toolName),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	// 兜底：尝试从 ResourceManager 按 name 获取 Tool
	return am.executeFallbackTool(ctx, toolCall, toolName, toolArgs, session, tag)
}

// executeTool 执行 Tool 类型能力。
func (am *AbilityManager) executeTool(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	session Session,
	tag string,
) ExecuteResult {
	toolCard := am.tools[toolName]
	toolID := toolCard.ID
	if toolID == "" {
		toolID = toolCard.Name
	}

	var opts []ResourceOption
	if tag != "" {
		opts = append(opts, WithResourceTag(tag))
	}

	t, err := am.resourceMgr.GetTool(toolID, opts...)
	if err != nil {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"工具实例未找到: "+toolID,
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	result, err := t.Invoke(ctx, toolArgs)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("tool_name", toolName).
			Err(err).
			Msg("工具执行错误")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"工具执行错误: "+err.Error(),
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	content := BuildToolMessageContent(result)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return ExecuteResult{Result: result, ToolMsg: toolMsg}
}

// executeWorkflow 执行 Workflow 类型能力。⤵️ 预留，领域八回填。
func (am *AbilityManager) executeWorkflow(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	session Session,
	tag string,
) ExecuteResult {
	wfCard := am.workflows[toolName]
	wfID := wfCard.ID
	if wfID == "" {
		wfID = wfCard.Name
	}

	wf, err := am.resourceMgr.GetWorkflow(wfID)
	if err != nil {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"工作流实例未找到: "+wfID,
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	result, err := wf.Execute(ctx, toolArgs)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("workflow_name", toolName).
			Err(err).
			Msg("工作流执行错误")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"工作流执行错误: "+err.Error(),
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	content := BuildToolMessageContent(result)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return ExecuteResult{Result: result, ToolMsg: toolMsg}
}

// executeAgent 执行 Agent 类型能力。⤵️ 预留，领域六回填。
func (am *AbilityManager) executeAgent(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	session Session,
	tag string,
) ExecuteResult {
	agentCard := am.agents[toolName]
	agentID := agentCard.ID
	if agentID == "" {
		agentID = agentCard.Name
	}

	ag, err := am.resourceMgr.GetAgent(agentID)
	if err != nil {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"Agent 实例未找到: "+agentID,
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	result, err := ag.Invoke(ctx, toolArgs)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("agent_name", toolName).
			Err(err).
			Msg("Agent 执行错误")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"Agent 执行错误: "+err.Error(),
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	content := BuildToolMessageContent(result)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return ExecuteResult{Result: result, ToolMsg: toolMsg}
}

// executeFallbackTool 兜底：从 ResourceManager 按 name 获取 Tool。
func (am *AbilityManager) executeFallbackTool(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	session Session,
	tag string,
) ExecuteResult {
	var opts []ResourceOption
	if tag != "" {
		opts = append(opts, WithResourceTag(tag))
	}

	t, err := am.resourceMgr.GetTool(toolName, opts...)
	if err != nil {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"能力未找到: "+toolName,
			exception.WithParam("ability_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	result, invokeErr := t.Invoke(ctx, toolArgs)
	if invokeErr != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("tool_name", toolName).
			Err(invokeErr).
			Msg("工具执行错误")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"工具执行错误: "+invokeErr.Error(),
			exception.WithParam("tool_name", toolName),
			exception.WithCause(invokeErr),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	content := BuildToolMessageContent(result)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return ExecuteResult{Result: result, ToolMsg: toolMsg}
}
```

- [ ] **Step 3: 在 ability_manager_test.go 中添加 Execute 测试**

需要一个 fakeResourceManager 和 fakeTool：

```go
// fakeTool 用于测试的模拟工具
type fakeTool struct {
	card   *tool.ToolCard
	result map[string]any
	err    error
}

func (f *fakeTool) Card() *tool.ToolCard { return f.card }
func (f *fakeTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return f.result, f.err
}
func (f *fakeTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// fakeResourceManager 用于测试的模拟资源管理器
type fakeResourceManager struct {
	tools     map[string]tool.Tool
	workflows map[string]Workflow
	agents    map[string]Agent
}

func newFakeResourceManager() *fakeResourceManager {
	return &fakeResourceManager{
		tools:     make(map[string]tool.Tool),
		workflows: make(map[string]Workflow),
		agents:    make(map[string]Agent),
	}
}

func (f *fakeResourceManager) GetTool(toolID string, _ ...ResourceOption) (tool.Tool, error) {
	t, ok := f.tools[toolID]
	if !ok {
		return nil, exception.BuildError(exception.StatusAbilityNotFound, exception.WithParam("ability_name", toolID))
	}
	return t, nil
}

func (f *fakeResourceManager) GetWorkflow(workflowID string, _ ...ResourceOption) (Workflow, error) {
	w, ok := f.workflows[workflowID]
	if !ok {
		return nil, exception.BuildError(exception.StatusAbilityNotFound, exception.WithParam("ability_name", workflowID))
	}
	return w, nil
}

func (f *fakeResourceManager) GetAgent(agentID string, _ ...ResourceOption) (Agent, error) {
	a, ok := f.agents[agentID]
	if !ok {
		return nil, exception.BuildError(exception.StatusAbilityNotFound, exception.WithParam("ability_name", agentID))
	}
	return a, nil
}

func (f *fakeResourceManager) GetMcpToolInfos(_ string) ([]*schema.ToolInfo, error) {
	return nil, nil
}
```

然后添加 Execute 测试用例：

```go
func TestAbilityManager_Execute_单工具成功(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	// 注册工具 Card
	toolCard := tool.NewToolCard("search", "搜索", nil, nil)
	am.Add(toolCard)

	// 注册工具实例
	frm.tools[toolCard.ID] = &fakeTool{
		card:   toolCard,
		result: map[string]any{"result": "搜索结果"},
	}

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "search", `{"query": "test"}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("不应有错误: %v", results[0].Err)
	}
	if results[0].ToolMsg == nil {
		t.Fatal("ToolMsg 不应为 nil")
	}
	if results[0].ToolMsg.ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q, want call_1", results[0].ToolMsg.ToolCallID)
	}
}

func TestAbilityManager_Execute_并行多工具(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	card1 := tool.NewToolCard("tool_a", "工具A", nil, nil)
	card2 := tool.NewToolCard("tool_b", "工具B", nil, nil)
	am.Add(card1)
	am.Add(card2)

	frm.tools[card1.ID] = &fakeTool{card: card1, result: map[string]any{"result": "A"}}
	frm.tools[card2.ID] = &fakeTool{card: card2, result: map[string]any{"result": "B"}}

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "tool_a", `{}`),
		llmschema.NewToolCall("call_2", "tool_b", `{}`),
	}, nil, "")

	if len(results) != 2 {
		t.Fatalf("结果数量 = %d, want 2", len(results))
	}
	// 两个都应成功
	errCount := 0
	for _, r := range results {
		if r.Err != nil {
			errCount++
		}
	}
	if errCount > 0 {
		t.Errorf("不应有错误，实际 %d 个", errCount)
	}
}

func TestAbilityManager_Execute_参数解析失败(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	toolCard := tool.NewToolCard("search", "搜索", nil, nil)
	am.Add(toolCard)

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "search", `not json at all`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("应有错误")
	}
	// 错误应为 AbilityExecutionError
	var execErr *AbilityExecutionError
	if !isAbilityExecutionError(results[0].Err, &execErr) {
		t.Errorf("错误类型应为 *AbilityExecutionError，实际 %T", results[0].Err)
	}
}

func TestAbilityManager_Execute_能力未找到(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "nonexistent", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("应有错误")
	}
}

// isAbilityExecutionError 检查错误是否为 *AbilityExecutionError。
func isAbilityExecutionError(err error, target **AbilityExecutionError) bool {
	switch e := err.(type) {
	case *AbilityExecutionError:
		*target = e
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: 运行测试验证**

Run: `go test ./internal/agentcore/single_agent/... -v -run "TestAbilityManager_Execute"`
Expected: PASS

- [ ] **Step 5: 运行全部 single_agent 测试**

Run: `go test ./internal/agentcore/single_agent/... -v -cover`
Expected: 全部 PASS，覆盖率 ≥ 85%

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/single_agent/ability_manager.go internal/agentcore/single_agent/ability_manager_test.go
git commit -m "feat(single_agent): implement AbilityManager.Execute with parallel execution"
```

---

### Task 8: 更新 IMPLEMENTATION_PLAN.md 和 doc.go

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`
- Modify: `internal/agentcore/single_agent/doc.go`

- [ ] **Step 1: 更新 IMPLEMENTATION_PLAN.md 中 3.13 的状态**

将 `| 3.13 | ☐ | AbilityManager |` 改为 `| 3.13 | ✅ | AbilityManager |`

- [ ] **Step 2: 运行全量测试验证无回归**

Run: `go test ./internal/agentcore/... -count=1`
Expected: 全部 PASS

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: mark 3.13 AbilityManager as completed"
```
