package schema

import (
	"reflect"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// AgentCard Agent 配置卡片，嵌入 BaseCard，增加输入/输出参数和接口 URL。
//
// InputParams/OutputParams 使用 []*schema.Param 定义，与 ToolCard 一致，
// ToolInfo() 时通过 ToJSONSchemaMap 转为 JSON Schema dict 供 LLM 消费。
// 可通过 WithInputParams[I]() / WithOutputParams[O]() 泛型 Option
// 从 Go struct 自动反射提取参数定义（对齐 Python 的 Type[BaseModel] 路径）。
//
// 对应 Python: openjiuwen/core/single_agent/schema/agent_card.py (AgentCard)
type AgentCard struct {
	schema.BaseCard
	// InputParams 输入参数定义，与 ToolCard 一致使用 []*schema.Param
	InputParams []*schema.Param `json:"input_params,omitempty"`
	// OutputParams 输出参数定义
	OutputParams []*schema.Param `json:"output_params,omitempty"`
	// InterfaceURL A2A JSON-RPC 基础 URL
	InterfaceURL string `json:"interface_url,omitempty"`
}

// AgentCardOption AgentCard 构造选项函数。
type AgentCardOption func(*AgentCard)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 AgentCard 满足 schema.CardInterface。
var _ schema.CardInterface = (*AgentCard)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithInputParams 从 Go struct 类型自动反射提取输入参数定义。
// 对齐 Python AgentCard(input_params=Type[BaseModel]) 的 model_json_schema() 路径。
//
// 用法：
//
//	type MyInput struct {
//	    Query string `json:"query" jsonschema:"description=搜索关键词,required"`
//	}
//	card := NewAgentCard(WithAgentName("my_agent"), WithInputParams[MyInput]())
func WithInputParams[I any]() AgentCardOption {
	return func(c *AgentCard) {
		typ := reflect.TypeOf((*I)(nil)).Elem()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		params, err := tool.StructSchemaExtractor{}.Extract(typ)
		if err == nil && len(params) > 0 {
			c.InputParams = params
		}
	}
}

// WithOutputParams 从 Go struct 类型自动反射提取输出参数定义。
// 对齐 Python AgentCard(output_params=Type[BaseModel]) 的 model_json_schema() 路径。
func WithOutputParams[O any]() AgentCardOption {
	return func(c *AgentCard) {
		typ := reflect.TypeOf((*O)(nil)).Elem()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		params, err := tool.StructSchemaExtractor{}.Extract(typ)
		if err == nil && len(params) > 0 {
			c.OutputParams = params
		}
	}
}

// WithAgentName 设置 Agent 名称。
func WithAgentName(name string) AgentCardOption {
	return func(c *AgentCard) { c.Name = name }
}

// WithAgentDescription 设置 Agent 描述。
func WithAgentDescription(desc string) AgentCardOption {
	return func(c *AgentCard) { c.Description = desc }
}

// WithAgentID 设置 Agent 唯一标识（覆盖自动生成的 UUID）。
func WithAgentID(id string) AgentCardOption {
	return func(c *AgentCard) { c.ID = id }
}

// WithInterfaceURL 设置 A2A JSON-RPC 基础 URL。
func WithInterfaceURL(url string) AgentCardOption {
	return func(c *AgentCard) { c.InterfaceURL = url }
}

// WithOutputParamsDirect 直接设置输出参数定义。
func WithOutputParamsDirect(params []*schema.Param) AgentCardOption {
	return func(c *AgentCard) { c.OutputParams = params }
}

// NewAgentCard 创建 AgentCard 实例，编译时类型安全。
// 所有选项均为 AgentCardOption 类型，消除运行时类型断言。
//
// 对应 Python: AgentCard(name=..., description=..., input_params=..., output_params=..., interface_url=...)
func NewAgentCard(opts ...AgentCardOption) *AgentCard {
	card := &AgentCard{
		BaseCard: *schema.NewBaseCard(),
	}
	for _, opt := range opts {
		opt(card)
	}
	return card
}

// ToolInfo 返回工具描述信息，供 LLM function calling 消费。
// 将 InputParams ([]*Param) 转换为 JSON Schema map，与 ToolCard.ToolInfo() 一致。
//
// 对应 Python: AgentCard.tool_info() / AbilityManager.list_tool_info() 中 AgentCard 分支
func (c *AgentCard) ToolInfo() schema.ToolInfoInterface {
	parameters := schema.ToJSONSchemaMap(c.InputParams)
	return schema.NewToolInfo(c.Name, c.Description, parameters)
}

// AbilityName 实现 Ability 接口。
func (c *AgentCard) AbilityName() string { return c.Name }

// AbilityID 实现 Ability 接口。
func (c *AgentCard) AbilityID() string { return c.ID }

// AbilityKind 实现 Ability 接口。
func (c *AgentCard) AbilityKind() schema.AbilityKind { return schema.AbilityKindAgent }
