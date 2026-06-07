package tool

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCard 工具配置卡片，嵌入 BaseCard，增加输入参数定义和扩展属性。
//
// 对应 Python: openjiuwen/core/foundation/tool/base.py (ToolCard)
type ToolCard struct {
	schema.BaseCard
	// InputParams 输入参数定义，用于校验和生成 ToolInfo 传给 LLM
	InputParams []*schema.Param
	// Properties 扩展属性
	Properties map[string]any
}

// ToolCallOptions 工具调用的扩展选项。
//
// 对应 Python: Tool.invoke/Stream 中的 **kwargs 参数集合
type ToolCallOptions struct {
	// SkipNoneValue 是否跳过 None 值（LocalFunction 使用）
	SkipNoneValue bool
	// SkipInputsValidate 是否跳过输入校验（LocalFunction 使用）
	SkipInputsValidate bool
	// Timeout 超时时间，单位秒（RestfulApi 使用）
	Timeout float64
	// MaxResponseBytes 最大响应字节数（RestfulApi 使用）
	MaxResponseBytes int
	// RaiseForStatus HTTP 错误是否抛异常（RestfulApi 使用）
	RaiseForStatus bool
}

// StreamChunk 流式执行的返回块。
//
// 消费者通过读取 channel 中的 StreamChunk 获取流式数据：
//   - Data 非 nil 且 Done=false：正常数据块
//   - Done=true：流正常结束
//   - Error 非 nil：流出错
type StreamChunk struct {
	// Data 本块数据
	Data map[string]any
	// Error 非 nil 表示流结束且出错
	Error error
	// Done true 表示流正常结束（Data 为空）
	Done bool
}

// Tool 工具接口，所有工具类型（LocalFunction/MCPTool/RestfulApi）的统一抽象。
//
// Tool 接口只定义纯业务方法，生命周期回调由 LifecycleTool 包装器处理。
//
// 对应 Python: openjiuwen/core/foundation/tool/base.py (Tool)
type Tool interface {
	// Card 返回工具的配置卡片
	Card() *ToolCard
	// Invoke 一次性执行工具，返回完整结果。
	// 不支持 Stream 的工具在 Stream 方法中返回 ErrStreamNotSupported。
	Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error)
	// Stream 流式执行工具，逐步返回结果块。
	// 不支持 Stream 的工具返回 ErrStreamNotSupported 错误。
	Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error)
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrStreamNotSupported 工具不支持流式调用时返回的错误。
//
// 对应 Python: TOOL_STREAM_NOT_SUPPORTED (182010)
var ErrStreamNotSupported = exception.BuildError(
	exception.StatusToolStreamNotSupported,
	exception.WithParam("card", ""),
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ToolOption 工具调用选项函数。
type ToolOption func(*ToolCallOptions)

// WithSkipNoneValue 设置是否跳过 None 值。
func WithSkipNoneValue(skip bool) ToolOption {
	return func(o *ToolCallOptions) { o.SkipNoneValue = skip }
}

// WithSkipInputsValidate 设置是否跳过输入校验。
func WithSkipInputsValidate(skip bool) ToolOption {
	return func(o *ToolCallOptions) { o.SkipInputsValidate = skip }
}

// WithTimeout 设置超时时间（秒）。
func WithTimeout(d float64) ToolOption {
	return func(o *ToolCallOptions) { o.Timeout = d }
}

// WithMaxResponseBytes 设置最大响应字节数。
func WithMaxResponseBytes(n int) ToolOption {
	return func(o *ToolCallOptions) { o.MaxResponseBytes = n }
}

// WithRaiseForStatus 设置 HTTP 错误是否抛异常。
func WithRaiseForStatus(raise bool) ToolOption {
	return func(o *ToolCallOptions) { o.RaiseForStatus = raise }
}

// NewToolCallOptions 从选项列表构造 ToolCallOptions。
func NewToolCallOptions(opts ...ToolOption) *ToolCallOptions {
	o := &ToolCallOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// NewToolCard 创建 ToolCard 实例，自动生成 BaseCard。
//
// 对应 Python: ToolCard(input_params=..., properties=...)
func NewToolCard(name, description string, inputParams []*schema.Param, properties map[string]any) *ToolCard {
	card := &ToolCard{
		BaseCard:    *schema.NewBaseCard(schema.WithName(name), schema.WithDescription(description)),
		InputParams: inputParams,
		Properties:  properties,
	}
	if card.Properties == nil {
		card.Properties = make(map[string]any)
	}
	return card
}

// NewErrStreamNotSupported 创建带 card 信息的 Stream 不支持错误。
func NewErrStreamNotSupported(card string) *exception.BaseError {
	return exception.BuildError(
		exception.StatusToolStreamNotSupported,
		exception.WithParam("card", card),
	)
}

// ValidateToolCard 校验 ToolCard 的合法性。
//
// 规则：
//   - card 不能为 nil
//   - card.ID 不能为空
//
// 对应 Python: Tool.__init__ 中的 card 校验
func ValidateToolCard(card *ToolCard) error {
	if card == nil {
		return exception.BuildError(
			exception.StatusToolCardInvalid,
			exception.WithParam("card", "nil"),
			exception.WithParam("reason", "card is None"),
		)
	}
	if card.ID == "" {
		return exception.BuildError(
			exception.StatusToolCardInvalid,
			exception.WithParam("card", card.String()),
			exception.WithParam("reason", "card id is empty"),
		)
	}
	return nil
}

// String 实现 fmt.Stringer 接口，返回 ToolCard 的简洁描述。
func (c *ToolCard) String() string {
	return fmt.Sprintf("id=%s,name=%s", c.ID, c.Name)
}

// ToolInfo 从 ToolCard 生成工具描述信息，供 LLM function calling 消费。
//
// 将 InputParams ([]*Param) 转换为 JSON Schema map，构造 ToolInfo 返回。
//
// 对应 Python: ToolCard.tool_info() -> ToolInfo(name=..., description=..., parameters=...)
func (c *ToolCard) ToolInfo() *schema.ToolInfo {
	parameters := schema.ToJSONSchemaMap(c.InputParams)
	return schema.NewToolInfo(c.Name, c.Description, parameters)
}

// AbilityName 实现 schema.Ability 接口。
func (c *ToolCard) AbilityName() string { return c.Name }

// AbilityID 实现 schema.Ability 接口。
func (c *ToolCard) AbilityID() string { return c.ID }

// AbilityKind 实现 schema.Ability 接口。
func (c *ToolCard) AbilityKind() schema.AbilityKind { return schema.AbilityKindTool }
