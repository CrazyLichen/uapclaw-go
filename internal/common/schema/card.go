package schema

import (
	"fmt"

	"github.com/google/uuid"
)

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

// ──────────────────────────── 结构体 ────────────────────────────

// BaseCard 数字名片基类，所有 Card 类型均嵌入此结构体。
//
// 子类包括：ToolCard、AgentCard、WorkflowCard、TeamCard、SysOperationCard。
// BaseCard 提供统一身份标识和 LLM function calling 所需的元信息。
//
// 对应 Python: openjiuwen/core/common/schema/card.py (BaseCard)
type BaseCard struct {
	// ID 唯一标识符，构造时自动生成 32 位 UUID hex（无连字符）
	ID string `json:"id"`
	// Name 名称，在某个 namespace 中的唯一标识符
	Name string `json:"name"`
	// Description 功能、适用场景等描述信息，供 LLM 判断是否调用
	Description string `json:"description"`
}

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

// ──────────────────────────── 导出函数 ────────────────────────────

// CardOption BaseCard 构造选项函数。
type CardOption func(*BaseCard)

// WithName 设置 Card 名称。
func WithName(name string) CardOption {
	return func(c *BaseCard) { c.Name = name }
}

// WithDescription 设置 Card 描述。
func WithDescription(desc string) CardOption {
	return func(c *BaseCard) { c.Description = desc }
}

// WithID 设置 Card ID（覆盖自动生成的 UUID）。
func WithID(id string) CardOption {
	return func(c *BaseCard) { c.ID = id }
}

// NewBaseCard 创建 BaseCard 实例，默认自动生成 32 位 UUID hex 作为 ID。
//
// 对应 Python: BaseCard(id=uuid4().hex, name="", description="")
func NewBaseCard(opts ...CardOption) *BaseCard {
	card := &BaseCard{
		ID: uuid.New().String(), // 生成 UUID（含连字符，下面去除）
	}
	// 去除连字符，与 Python uuid4().hex 行为一致（32 位 hex）
	card.ID = formatUUIDHex(card.ID)

	for _, opt := range opts {
		opt(card)
	}
	return card
}

// ToolInfo 返回工具描述信息，供 LLM function calling 消费。
// BaseCard 默认返回 nil，子类（如 ToolCard、AgentCard）应覆写此方法。
//
// 对应 Python: BaseCard.tool_info() — Python 中为空实现（...），子类各自覆写
func (c *BaseCard) ToolInfo() *ToolInfo {
	return nil
}

// String 实现 fmt.Stringer 接口，返回简洁的身份描述。
//
// 对应 Python: BaseCard.to_str()
func (c *BaseCard) String() string {
	return fmt.Sprintf("id=%s,name=%s", c.ID, c.Name)
}

// GoString 实现 fmt.GoStringer 接口，用于 %#v 格式化。
func (c *BaseCard) GoString() string {
	return fmt.Sprintf("BaseCard{ID:%q, Name:%q, Description:%q}", c.ID, c.Name, c.Description)
}

// NewWorkflowCard 创建 WorkflowCard 实例。
//
// 对应 Python: WorkflowCard(name=..., description=..., version=..., input_params=...)
func NewWorkflowCard(opts ...CardOption) *WorkflowCard {
	return &WorkflowCard{
		BaseCard: *NewBaseCard(opts...),
	}
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

// AbilityName 实现 Ability 接口。
func (c *WorkflowCard) AbilityName() string { return c.Name }

// AbilityID 实现 Ability 接口。
func (c *WorkflowCard) AbilityID() string { return c.ID }

// AbilityKind 实现 Ability 接口。
func (c *WorkflowCard) AbilityKind() AbilityKind { return AbilityKindWorkflow }

// NewAgentCard 创建 AgentCard 实例。
//
// 对应 Python: AgentCard(name=..., description=..., input_params=..., output_params=..., interface_url=...)
func NewAgentCard(opts ...CardOption) *AgentCard {
	return &AgentCard{
		BaseCard: *NewBaseCard(opts...),
	}
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

// AbilityName 实现 Ability 接口。
func (c *AgentCard) AbilityName() string { return c.Name }

// AbilityID 实现 Ability 接口。
func (c *AgentCard) AbilityID() string { return c.ID }

// AbilityKind 实现 Ability 接口。
func (c *AgentCard) AbilityKind() AbilityKind { return AbilityKindAgent }

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatUUIDHex 将 UUID 字符串中的连字符去除，返回 32 位 hex。
// 例如 "550e8400-e29b-41d4-a716-446655440000" → "550e8400e29b41d4a716446655440000"
func formatUUIDHex(id string) string {
	result := make([]byte, 0, 32)
	for i := 0; i < len(id); i++ {
		if id[i] != '-' {
			result = append(result, id[i])
		}
	}
	return string(result)
}
