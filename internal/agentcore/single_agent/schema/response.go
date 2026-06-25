package schema

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// InterruptRequester 中断请求接口，InterruptRequest 和 ToolCallInterruptRequest 均满足。
//
// 对齐 Python: isinstance(x, InterruptRequest) — Python 中 interrupt_requests 的值类型
// 实际可存 InterruptRequest 或其子类 ToolCallInterruptRequest（多态）。
// Go 中通过接口实现多态：handleToolInterruptException 存 *InterruptRequest，
// handleSubAgentInterrupt 存 *ToolCallInterruptRequest，均满足此接口。
type InterruptRequester interface {
	// GetMessage 返回向用户展示的确认消息
	GetMessage() string
	// GetAutoConfirmKey 返回自动确认配置键
	GetAutoConfirmKey() string
}

// ──────────────────────────── 结构体 ────────────────────────────

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
type ToolCallInterruptRequest struct {
	// InterruptRequest 嵌入基础中断请求
	InterruptRequest
	// ToolName 工具名称
	ToolName string
	// ToolCallID 工具调用 ID
	ToolCallID string
	// ToolArgs 工具参数 JSON 字符串（和 ToolCall.Arguments 一致）
	ToolArgs string
	// Index 工具调用索引（0 表示未设置，和 ToolCall.Index 语义一致）
	Index int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolCallInterruptRequest 从 InterruptRequest 和 ToolCall 创建 ToolCallInterruptRequest。
//
// 对应 Python: ToolCallInterruptRequest.from_tool_call(request, tool_call)
func NewToolCallInterruptRequest(request *InterruptRequest, toolCall *llmschema.ToolCall) *ToolCallInterruptRequest {
	return &ToolCallInterruptRequest{
		InterruptRequest: *request,
		ToolName:         toolCall.Name,
		ToolCallID:       toolCall.ID,
		ToolArgs:         toolCall.Arguments,
		Index:            toolCall.Index,
	}
}

// GetMessage 实现 InterruptRequester 接口。
func (r *InterruptRequest) GetMessage() string { return r.Message }

// GetAutoConfirmKey 实现 InterruptRequester 接口。
func (r *InterruptRequest) GetAutoConfirmKey() string { return r.AutoConfirmKey }

// GetMessage 实现 InterruptRequester 接口（覆盖嵌入实现，返回相同值）。
func (r *ToolCallInterruptRequest) GetMessage() string { return r.Message }

// GetAutoConfirmKey 实现 InterruptRequester 接口（覆盖嵌入实现，返回相同值）。
func (r *ToolCallInterruptRequest) GetAutoConfirmKey() string { return r.AutoConfirmKey }
