package schema

import (
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolInterruptException 工具中断异常，当工具需要用户确认时抛出。
//
// 实现 error 接口，使得：
//   - tool.Invoke() 可通过 (any, error) 签名返回
//   - before hook 可通过 Fire() 返回 error
//   - railedExecuteSingleToolCall 通过 errors.As 识别
//
// 对应 Python: ToolInterruptException(AgentInterrupt)
//
// Request 字段使用 InterruptRequester 接口，支持存储 InterruptRequest 子类
// （如 AskUserRequest），对齐 Python request: InterruptRequest 的多态语义。
type ToolInterruptException struct {
	// Request 中断请求接口，可存 InterruptRequest 或其子类
	Request InterruptRequester
	// ToolCall 关联的 ToolCall（由 hook 层通过 ctx 赋值，D3）
	ToolCall *llmschema.ToolCall
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
// 对齐 Python: super().__init__(str(request.message))
func (e *ToolInterruptException) Error() string {
	if e.Request != nil {
		return e.Request.GetMessage()
	}
	return "tool interrupt"
}

// String 返回人类可读的描述（含 ToolCall 信息）。
func (e *ToolInterruptException) String() string {
	if e.Request != nil {
		if e.ToolCall != nil {
			return fmt.Sprintf("tool interrupt: %s (tool=%s, call_id=%s)",
				e.Request.GetMessage(), e.ToolCall.Name, e.ToolCall.ID)
		}
		return fmt.Sprintf("tool interrupt: %s", e.Request.GetMessage())
	}
	return "tool interrupt"
}
