package schema

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InterruptRequester 中断请求接口，InterruptRequest 及其子类均满足。
//
// Python: isinstance(x, InterruptRequest) — Python 中 interrupt_requests 的值类型
// 实际可存 InterruptRequest 或其子类（如 AskUserRequest, ToolCallInterruptRequest）。
// Go 中通过接口实现多态：handler 存接口值，JSON 序列化按运行时具体类型输出字段。
//
// Python: model_config = {"extra": "allow"}：子类扩展字段（如 questions）
// 通过接口多态自然保留，序列化时由具体类型决定输出内容。
type InterruptRequester interface {
	// GetMessage 返回向用户展示的确认消息
	GetMessage() string
	// GetAutoConfirmKey 返回自动确认配置键
	GetAutoConfirmKey() string
	// GetMetadata 返回中断元数据
	//
	// rail 通过它把 host 侧信息（如 tool_invocation_key）挂到中断上，
	// 队列在恢复时凭它把答案与卡片对应起来。
	GetMetadata() map[string]any
	// SetMetadata 覆盖中断元数据
	SetMetadata(metadata map[string]any)
}

// InterruptRequest 工具中断请求，携带需要用户确认的信息。
//
// Python: InterruptRequest(message, payload_schema, auto_confirm_key, ui_options,
// metadata, interrupt_id, interrupt_kind, interrupt_owner)
//
// 身份字段说明（对齐 Python docstring）：
//   - InterruptID：本次中断的唯一标识，由中断处理器在收集暂停调用时填入；
//     宿主通过 InteractiveInput.answer(interrupt_id, value) 回答它。
//   - InterruptKind：这是什么类型的问题（见 InterruptKind），供宿主渲染并决定
//     答案形状。取值是普通字符串，以便第三方 rail 声明自己的类型；空串表示
//     抛出方未声明。
//   - InterruptOwner：抛出该中断的 rail 的稳定键；答案只会投递给那个 rail。
//     答案路由**始终按 owner**，与 kind 无关。
type InterruptRequest struct {
	// Message 向用户展示的确认消息
	Message string
	// PayloadSchema 用户输入的数据结构定义 (JSON Schema)
	PayloadSchema map[string]any
	// AutoConfirmKey 自动确认的配置键
	AutoConfirmKey string
	// UIOptions UI 选项（对齐 Python ui_options: list[dict] | None）
	UIOptions []map[string]any
	// Metadata 中断元数据，承载宿主与 rail 之间传递的附加信息
	Metadata map[string]any
	// InterruptID 本次中断的唯一标识，由中断处理器填入
	InterruptID string
	// InterruptKind 中断类型（见 InterruptKind），未声明时为空串
	InterruptKind string
	// InterruptOwner 抛出该中断的 rail 的稳定键，答案按此路由
	InterruptOwner string
}

// ToolCallInterruptRequest 工具调用级中断请求，扩展 InterruptRequest。
// 用于序列化中断信息给用户输出。
//
// Python: ToolCallInterruptRequest(InterruptRequest)
// + model_config = {"extra": "allow"} 保留子类额外字段
//
// Request 字段使用 InterruptRequester 接口，支持存储 InterruptRequest 子类
// （如 AskUserRequest），JSON 序列化按运行时具体类型输出所有字段，
// Python: model_dump() + extra="allow" 的子类字段透传行为。
type ToolCallInterruptRequest struct {
	// Request 中断请求接口，可存 InterruptRequest 或其子类（如 AskUserRequest）
	Request InterruptRequester `json:"request"`
	// ToolName 工具名称
	ToolName string `json:"tool_name"`
	// ToolCallID 工具调用 ID
	ToolCallID string `json:"tool_call_id"`
	// ToolArgs 工具参数 JSON 字符串（和 ToolCall.Arguments 一致）
	ToolArgs string `json:"tool_args"`
	// Index 工具调用索引（0 表示未设置，和 ToolCall.Index 语义一致）
	Index int `json:"index"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// InterruptKind 中断类型的已知取值。
//
// kind 告诉宿主如何呈现一个中断、答案应是什么形状。取值是普通字符串
// （而非封闭枚举），以便第三方 rail 声明自己的类型；空串表示抛出方未声明。
//
// Python: class InterruptKind
const (
	// InterruptKindUndeclared 抛出方未声明类型
	InterruptKindUndeclared = ""
	// InterruptKindPermission 权限审批
	InterruptKindPermission = "permission"
	// InterruptKindConfirm 确认
	InterruptKindConfirm = "confirm"
	// InterruptKindAskUser 向用户反问
	InterruptKindAskUser = "ask_user"
	// InterruptKindSkillEvolutionApproval 技能演化审批
	InterruptKindSkillEvolutionApproval = "skill_evolution_approval"
	// InterruptKindTeamToolApproval 团队工具审批
	InterruptKindTeamToolApproval = "team_tool_approval"
	// InterruptKindClientTool 客户端工具调用
	InterruptKindClientTool = "client_tool"
)

// ──────────────────────────── 常量 ────────────────────────────

// ClientToolInterruptOwner 能力管理器为客户端工具抛出的中断的 owner 键。
//
// Python: CLIENT_TOOL_INTERRUPT_OWNER = "openjiuwen.client_tool"
const ClientToolInterruptOwner = "openjiuwen.client_tool"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// InterruptKindOf 返回中断类型，未声明时返回空串。
//
// 字段存在之前持久化的技能演化审批数据把 kind 只存在 metadata["interrupt_kind"]
// 里，此处兼容该历史形态。
//
// Python: interrupt_kind_of(request)
func InterruptKindOf(request *InterruptRequest) string {
	if request == nil {
		return InterruptKindUndeclared
	}
	if request.InterruptKind != "" {
		return request.InterruptKind
	}
	if request.Metadata != nil {
		if kind, ok := request.Metadata["interrupt_kind"].(string); ok {
			return kind
		}
	}
	return InterruptKindUndeclared
}

// NewToolCallInterruptRequest 从 InterruptRequester 和 ToolCall 创建 ToolCallInterruptRequest。
//
// Python: ToolCallInterruptRequest.from_tool_call(request, tool_call)
// request 参数接受 InterruptRequester 接口，支持传入子类（如 AskUserRequest），
// 子类扩展字段通过接口多态保留在 Request 字段中。
func NewToolCallInterruptRequest(request InterruptRequester, toolCall *llmschema.ToolCall) *ToolCallInterruptRequest {
	return &ToolCallInterruptRequest{
		Request:    request,
		ToolName:   toolCall.Name,
		ToolCallID: toolCall.ID,
		ToolArgs:   toolCall.Arguments,
		Index:      toolCall.Index,
	}
}

// GetMessage 实现 InterruptRequester 接口。
func (r *InterruptRequest) GetMessage() string { return r.Message }

// GetAutoConfirmKey 实现 InterruptRequester 接口。
func (r *InterruptRequest) GetAutoConfirmKey() string { return r.AutoConfirmKey }

// RequestInterruptKind 返回中断类型。
//
// 供 permissions 包的 InteractionRequestProvider 接口消费：root 权限队列需要
// 读 interrupt_kind 区分客户端工具中断与权限卡，但不希望依赖 schema 具体类型。
func (r *InterruptRequest) RequestInterruptKind() string { return r.InterruptKind }

// RequestMetadata 返回中断元数据。
//
// 供 permissions 包的 InteractionRequestProvider 接口消费（读 tool_invocation_key）。
func (r *InterruptRequest) RequestMetadata() map[string]any { return r.Metadata }

// GetMetadata 实现 InterruptRequester 接口。
func (r *InterruptRequest) GetMetadata() map[string]any { return r.Metadata }

// SetMetadata 实现 InterruptRequester 接口。
func (r *InterruptRequest) SetMetadata(metadata map[string]any) { r.Metadata = metadata }

// GetMessage 实现 InterruptRequester 接口，委托给 Request 字段。
func (r *ToolCallInterruptRequest) GetMessage() string {
	if r.Request != nil {
		return r.Request.GetMessage()
	}
	return ""
}

// GetAutoConfirmKey 实现 InterruptRequester 接口，委托给 Request 字段。
func (r *ToolCallInterruptRequest) GetAutoConfirmKey() string {
	if r.Request != nil {
		return r.Request.GetAutoConfirmKey()
	}
	return ""
}

// GetMetadata 实现 InterruptRequester 接口，委托给 Request 字段。
func (r *ToolCallInterruptRequest) GetMetadata() map[string]any {
	if r.Request != nil {
		return r.Request.GetMetadata()
	}
	return nil
}

// SetMetadata 实现 InterruptRequester 接口，委托给 Request 字段。
func (r *ToolCallInterruptRequest) SetMetadata(metadata map[string]any) {
	if r.Request != nil {
		r.Request.SetMetadata(metadata)
	}
}
