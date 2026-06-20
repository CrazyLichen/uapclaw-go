package token

import (
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TokenCounter Token 计数器抽象接口。
//
// 提供文本、消息列表和工具定义的 Token 计数能力，
// 供 ContextStats 统计和 ContextWindow 构建时使用。
//
// 对应 Python: openjiuwen/core/context_engine/token/base.py (TokenCounter)
type TokenCounter interface {
	// Count 计算文本的 Token 数量
	Count(text string, model string) int
	// CountMessages 计算消息列表的 Token 数量
	CountMessages(messages []*llm_schema.BaseMessage, model string) int
	// CountTools 计算工具定义的 Token 数量
	CountTools(tools []*schema.ToolInfo, model string) int
}
