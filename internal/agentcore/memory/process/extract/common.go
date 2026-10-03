package extract

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExtractMemoryParams 长期记忆提取参数。
//
// Python: openjiuwen/core/memory/process/extract/common.py (ExtractMemoryParams)
type ExtractMemoryParams struct {
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
	// Messages 当前轮次消息列表
	Messages []schema.BaseMessage
	// HistoryMessages 历史消息列表
	HistoryMessages []schema.BaseMessage
	// BaseChatModel 基础聊天模型（对齐 Python base_chat_model）
	BaseChatModel *llm.Model
}

// MemoryOperationParams 记忆操作参数（UPDATE/DELETE 语义验证时使用）。
//
// 本参数在 7.18 中不被直接使用，但属于 Python common.py 的一部分，
// 后续 7.19+ Generator 的 _handle_memory_with_instruct 需要此参数。
//
// Python: openjiuwen/core/memory/process/extract/common.py (MemoryOperationParams)
type MemoryOperationParams struct {
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
	// MessageMemID 关联消息 ID
	MessageMemID string
	// Timestamp 时间戳
	Timestamp string
	// BaseModel 基础聊天模型
	BaseModel *llm.Model
	// SemanticStore 语义存储（用于搜索旧记忆做语义验证）
	// TODO(#7.27): 回填为具体类型——当前 Go 版 SearchManager.Search 不再接收 semantic_store 参数（已内置），
	// 此字段保留供 7.27 回填时使用。
	SemanticStore any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
