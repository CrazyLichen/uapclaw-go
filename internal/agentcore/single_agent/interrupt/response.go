package interrupt

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 类型别名（re-export） ────────────────────────────
// 从 sa/schema 包 re-export 中断请求相关类型，保持 API 兼容。
// 类型定义已迁移至 sa/schema 包，此处仅保留类型别名和函数委托。

// InterruptRequester 中断请求接口。
type InterruptRequester = saschema.InterruptRequester

// InterruptRequest 工具中断请求。
type InterruptRequest = saschema.InterruptRequest

// ToolCallInterruptRequest 工具调用级中断请求。
type ToolCallInterruptRequest = saschema.ToolCallInterruptRequest

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolCallInterruptRequest 委托至 sa/schema 包的实现。
func NewToolCallInterruptRequest(request *InterruptRequest, toolCall *llmschema.ToolCall) *ToolCallInterruptRequest {
	return saschema.NewToolCallInterruptRequest(request, toolCall)
}
