package schema

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// InterruptRequester 中断请求接口，InterruptRequest 及其子类均满足。
//
// 对齐 Python: isinstance(x, InterruptRequest) — Python 中 interrupt_requests 的值类型
// 实际可存 InterruptRequest 或其子类（如 AskUserRequest, ToolCallInterruptRequest）。
// Go 中通过接口实现多态：handler 存接口值，JSON 序列化按运行时具体类型输出字段。
//
// 对齐 Python model_config = {"extra": "allow"}：子类扩展字段（如 questions）
// 通过接口多态自然保留，序列化时由具体类型决定输出内容。
type InterruptRequester interface {
	// GetMessage 返回向用户展示的确认消息
	GetMessage() string
	// GetAutoConfirmKey 返回自动确认配置键
	GetAutoConfirmKey() string
}

// InterruptRequest 工具中断请求，携带需要用户确认的信息。
//
// 对应 Python: InterruptRequest(message, payload_schema, auto_confirm_key, ui_options)
type InterruptRequest struct {
	// Message 向用户展示的确认消息
	Message string
	// PayloadSchema 用户输入的数据结构定义 (JSON Schema)
	PayloadSchema map[string]any
	// AutoConfirmKey 自动确认的配置键
	AutoConfirmKey string
	// UIOptions UI 选项（对齐 Python ui_options: list[dict] | None）
	UIOptions []map[string]any
}

// ToolCallInterruptRequest 工具调用级中断请求，扩展 InterruptRequest。
// 用于序列化中断信息给用户输出。
//
// 对应 Python: ToolCallInterruptRequest(InterruptRequest)
// + model_config = {"extra": "allow"} 保留子类额外字段
//
// Request 字段使用 InterruptRequester 接口，支持存储 InterruptRequest 子类
// （如 AskUserRequest），JSON 序列化按运行时具体类型输出所有字段，
// 对齐 Python model_dump() + extra="allow" 的子类字段透传行为。
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

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolCallInterruptRequest 从 InterruptRequester 和 ToolCall 创建 ToolCallInterruptRequest。
//
// 对应 Python: ToolCallInterruptRequest.from_tool_call(request, tool_call)
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
