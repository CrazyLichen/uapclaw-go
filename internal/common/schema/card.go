package schema

import (
	"fmt"

	"github.com/google/uuid"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Ability 能力接口，定义能力的基本属性。
type Ability interface {
	// AbilityName 返回能力名称
	AbilityName() string
	// AbilityID 返回能力唯一标识
	AbilityID() string
	// AbilityKind 返回能力类型
	AbilityKind() AbilityKind
}

// CardInterface 卡片接口，定义身份元数据的基本属性。
type CardInterface interface {
	// GetID 返回唯一标识符
	GetID() string
	// GetName 返回名称
	GetName() string
	// GetDescription 返回描述信息
	GetDescription() string
	// String 返回简洁的身份描述
	String() string
}

// BaseCard 基础卡片，包含 ID/名称/描述等身份元数据。
type BaseCard struct {
	// ID 唯一标识符，构造时自动生成 32 位 UUID hex（无连字符）
	ID string `json:"id"`
	// Name 名称，在某个 namespace 中的唯一标识符
	Name string `json:"name"`
	// Description 功能、适用场景等描述信息，供 LLM 判断是否调用
	Description string `json:"description"`
}

// WorkflowCard 工作流卡片，扩展 BaseCard 增加版本号和输入参数。
type WorkflowCard struct {
	BaseCard
	// Version 工作流版本号
	Version string `json:"version,omitempty"`
	// InputParams 输入参数定义（JSON Schema 格式）
	InputParams map[string]any `json:"input_params,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// CardOption BaseCard 构造选项函数。
type CardOption func(*BaseCard)

// AbilityKind 能力类型枚举。
type AbilityKind int

// ──────────────────────────── 常量 ────────────────────────────

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

// ──────────────────────────── 全局变量 ────────────────────────────

// 确保 BaseCard 和 WorkflowCard 实现 CardInterface 接口。
var _ CardInterface = (*BaseCard)(nil)

var _ CardInterface = (*WorkflowCard)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// String 返回能力类型的字符串表示。
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

// WithName 设置名称选项。
func WithName(name string) CardOption {
	return func(c *BaseCard) { c.Name = name }
}

// WithDescription 设置描述选项。
func WithDescription(desc string) CardOption {
	return func(c *BaseCard) { c.Description = desc }
}

// WithID 设置 ID 选项。
func WithID(id string) CardOption {
	return func(c *BaseCard) { c.ID = id }
}

// NewBaseCard 创建基础卡片实例，自动生成 32 位 UUID hex 作为 ID。
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

// ToolInfo 返回工具信息接口，BaseCard 默认返回 nil。
func (c *BaseCard) ToolInfo() ToolInfoInterface {
	return nil
}

// GetID 返回唯一标识符。
func (c *BaseCard) GetID() string { return c.ID }

// GetName 返回名称。
func (c *BaseCard) GetName() string { return c.Name }

// GetDescription 返回描述信息。
func (c *BaseCard) GetDescription() string { return c.Description }

// String 返回简洁的身份描述。
func (c *BaseCard) String() string {
	return fmt.Sprintf("id=%s,name=%s", c.ID, c.Name)
}

// GoString 返回 Go 语法格式的描述。
func (c *BaseCard) GoString() string {
	return fmt.Sprintf("BaseCard{ID:%q, Name:%q, Description:%q}", c.ID, c.Name, c.Description)
}

// NewWorkflowCard 创建工作流卡片实例。
func NewWorkflowCard(opts ...CardOption) *WorkflowCard {
	return &WorkflowCard{
		BaseCard: *NewBaseCard(opts...),
	}
}

// ToolInfo 返回工具信息，将 InputParams 转换为 ToolInfoInterface。
func (c *WorkflowCard) ToolInfo() ToolInfoInterface {
	params := c.InputParams
	if params == nil {
		params = make(map[string]any)
	}
	return NewToolInfo(c.Name, c.Description, params)
}

// AbilityName 返回能力名称。
func (c *WorkflowCard) AbilityName() string { return c.Name }

// AbilityID 返回能力唯一标识。
func (c *WorkflowCard) AbilityID() string { return c.ID }

// AbilityKind 返回能力类型（工作流）。
func (c *WorkflowCard) AbilityKind() AbilityKind { return AbilityKindWorkflow }

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatUUIDHex 去除 UUID 中的连字符，返回 32 位 hex 字符串。
func formatUUIDHex(id string) string {
	result := make([]byte, 0, 32)
	for i := 0; i < len(id); i++ {
		if id[i] != '-' {
			result = append(result, id[i])
		}
	}
	return string(result)
}
