package token

import (
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TokenCounter Token 计数器抽象接口。
//
// 提供文本、消息列表和工具定义的 Token 计数能力，
// 供 ContextStats 统计和 ContextWindow 构建时使用。
// 所有方法返回 (int, error)，调用方应检查 error 决定是否降级。
//
// 对应 Python: openjiuwen/core/context_engine/token/base.py (TokenCounter)
type TokenCounter interface {
	// Count 计算文本的 Token 数量
	Count(text string, model string) (int, error)
	// CountMessages 计算消息列表的 Token 数量
	CountMessages(messages []llm_schema.BaseMessage, model string) (int, error)
	// CountTools 计算工具定义的 Token 数量
	CountTools(tools []schema.ToolInfoInterface, model string) (int, error)
}
