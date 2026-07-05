package interrupt

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 枚举 ────────────────────────────

// 从 sa/schema 包 re-export 中断请求相关类型，保持 API 兼容。
// 类型定义已迁移至 sa/schema 包，此处仅保留类型别名和函数委托。

// InterruptRequester 中断请求接口。
// TODO: 考虑移除 reexport，让调用者直接使用 saschema 包
type InterruptRequester = saschema.InterruptRequester

// InterruptRequest 工具中断请求。
// TODO: 考虑移除 reexport，让调用者直接使用 saschema 包
type InterruptRequest = saschema.InterruptRequest

// ToolCallInterruptRequest 工具调用级中断请求。
// TODO: 考虑移除 reexport，让调用者直接使用 saschema 包
type ToolCallInterruptRequest = saschema.ToolCallInterruptRequest

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolCallInterruptRequest 从 InterruptRequest 和 ToolCall 创建 ToolCallInterruptRequest。
// 委托至 saschema.NewToolCallInterruptRequest，保持 API 兼容。
func NewToolCallInterruptRequest(request *InterruptRequest, toolCall *llmschema.ToolCall) *ToolCallInterruptRequest {
	return saschema.NewToolCallInterruptRequest(request, toolCall)
}
