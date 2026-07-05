package interrupt

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolCallInterruptRequest 从 InterruptRequest 和 ToolCall 创建 ToolCallInterruptRequest。
// 委托至 saschema.NewToolCallInterruptRequest，保持 API 兼容。
func NewToolCallInterruptRequest(request *saschema.InterruptRequest, toolCall *llmschema.ToolCall) *saschema.ToolCallInterruptRequest {
	return saschema.NewToolCallInterruptRequest(request, toolCall)
}
