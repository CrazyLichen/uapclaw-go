package schema

import (
	"fmt"

	"github.com/google/uuid"
)

// ──────────────────────────── 结构体 ────────────────────────────

type Ability interface {
	// AbilityName 返回能力名称
	AbilityName() string
	// AbilityID 返回能力唯一标识
	AbilityID() string
	// AbilityKind 返回能力类型
	AbilityKind() AbilityKind
}

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

type BaseCard struct {
	// ID 唯一标识符，构造时自动生成 32 位 UUID hex（无连字符）
	ID string `json:"id"`
	// Name 名称，在某个 namespace 中的唯一标识符
	Name string `json:"name"`
	// Description 功能、适用场景等描述信息，供 LLM 判断是否调用
	Description string `json:"description"`
}

type WorkflowCard struct {
	BaseCard
	// Version 工作流版本号
	Version string `json:"version,omitempty"`
	// InputParams 输入参数定义（JSON Schema 格式）
	InputParams map[string]any `json:"input_params,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

type AbilityKind int

type CardOption func(*BaseCard)

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

var _ CardInterface = (*BaseCard)(nil)

var _ CardInterface = (*WorkflowCard)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

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

func WithName(name string) CardOption {
	return func(c *BaseCard) { c.Name = name }
}

func WithDescription(desc string) CardOption {
	return func(c *BaseCard) { c.Description = desc }
}

func WithID(id string) CardOption {
	return func(c *BaseCard) { c.ID = id }
}

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

func (c *BaseCard) ToolInfo() ToolInfoInterface {
	return nil
}

func (c *BaseCard) GetID() string { return c.ID }

func (c *BaseCard) GetName() string { return c.Name }

func (c *BaseCard) GetDescription() string { return c.Description }

func (c *BaseCard) String() string {
	return fmt.Sprintf("id=%s,name=%s", c.ID, c.Name)
}

func (c *BaseCard) GoString() string {
	return fmt.Sprintf("BaseCard{ID:%q, Name:%q, Description:%q}", c.ID, c.Name, c.Description)
}

func NewWorkflowCard(opts ...CardOption) *WorkflowCard {
	return &WorkflowCard{
		BaseCard: *NewBaseCard(opts...),
	}
}

func (c *WorkflowCard) ToolInfo() ToolInfoInterface {
	params := c.InputParams
	if params == nil {
		params = make(map[string]any)
	}
	return NewToolInfo(c.Name, c.Description, params)
}

func (c *WorkflowCard) AbilityName() string { return c.Name }

func (c *WorkflowCard) AbilityID() string { return c.ID }

func (c *WorkflowCard) AbilityKind() AbilityKind { return AbilityKindWorkflow }

// ──────────────────────────── 非导出函数 ────────────────────────────

func formatUUIDHex(id string) string {
	result := make([]byte, 0, 32)
	for i := 0; i < len(id); i++ {
		if id[i] != '-' {
			result = append(result, id[i])
		}
	}
	return string(result)
}
