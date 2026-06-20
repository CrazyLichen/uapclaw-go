package context_engine

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelContext 上下文引擎的核心抽象接口，管理对话消息和上下文窗口。
//
// 职责：
//   - 消息生命周期管理（增删改查）
//   - 上下文窗口构建（供 LLM 推理使用）
//   - 统计与监控（消息数/Token数/对话轮次）
//
// 对应 Python: openjiuwen/core/context_engine/base.py (ModelContext)
type ModelContext interface {
	// Len 返回上下文消息数量
	Len() int
	// GetMessages 获取消息列表
	// size 限制返回数量，nil 表示不限制
	// withHistory 控制是否包含历史消息
	GetMessages(size *int, withHistory bool) []llm_schema.BaseMessage
	// SetMessages 替换消息列表
	// withHistory 控制是否替换历史消息
	SetMessages(messages []llm_schema.BaseMessage, withHistory bool)
	// PopMessages 从尾部弹出消息
	// withHistory 控制是否从历史消息中弹出
	PopMessages(size int, withHistory bool) []llm_schema.BaseMessage
	// ClearMessages 清空消息
	// withHistory 控制是否清空历史消息
	ClearMessages(ctx context.Context, withHistory bool) error
	// AddMessages 添加消息
	// message 接受 *BaseMessage（单条）或 []*BaseMessage（列表）
	AddMessages(ctx context.Context, message any) ([]llm_schema.BaseMessage, error)
	// GetContextWindow 构建上下文窗口供模型推理使用
	GetContextWindow(ctx context.Context, systemMessages []llm_schema.BaseMessage,
		tools []*schema.ToolInfo, windowSize *int, dialogueRound *int) (*ContextWindow, error)
	// Statistic 计算上下文统计信息
	Statistic() *ContextStats
	// SessionID 返回会话 ID
	SessionID() string
	// ContextID 返回上下文 ID
	ContextID() string
	// TokenCounter 返回 Token 计数器
	TokenCounter() token.TokenCounter
	// ReloaderTool 返回重载卸载消息的工具
	ReloaderTool() tool.Tool
}

// ContextEngine 上下文引擎门面接口。
//
// 管理上下文池、处理器注册、会话状态持久化。
// 属于 Agent 级别组件（不在 Session 中），通过 agent.contextEngine 访问。
//
// 对应 Python: openjiuwen/core/context_engine/context_engine.py (ContextEngine)
type ContextEngine interface {
	// CreateContext 创建上下文
	CreateContext(ctx context.Context, contextID string, sess *session.Session) (ModelContext, error)
	// GetContext 获取上下文
	GetContext(contextID string, sessionID string) ModelContext
	// CompressContext 压缩上下文
	CompressContext(ctx context.Context, contextID string, sess *session.Session) (string, error)
	// ClearContext 清空上下文
	ClearContext(ctx context.Context, contextID string, sessionID string) error
	// SaveContexts 保存上下文状态
	SaveContexts(ctx context.Context, sess *session.Session, contextIDs []string) error
	// RegisterProcessor 注册处理器
	RegisterProcessor(processorType string, processor any)
}

// ContextStats 上下文统计快照，记录消息数量、Token 数量和对话轮次。
//
// 对应 Python: openjiuwen/core/context_engine/base.py (ContextStats)
type ContextStats struct {
	// TotalMessages 消息总数
	TotalMessages int `json:"total_messages"`
	// TotalTokens Token 总数
	TotalTokens int `json:"total_tokens"`
	// TotalDialogues 对话轮次数
	TotalDialogues int `json:"total_dialogues"`
	// SystemMessages 系统消息数
	SystemMessages int `json:"system_messages"`
	// UserMessages 用户消息数
	UserMessages int `json:"user_messages"`
	// AssistantMessages 助手消息数
	AssistantMessages int `json:"assistant_messages"`
	// ToolMessages 工具消息数
	ToolMessages int `json:"tool_messages"`
	// Tools 注入工具数
	Tools int `json:"tools"`
	// SystemMessageTokens 系统消息 Token 数
	SystemMessageTokens int `json:"system_message_tokens"`
	// UserMessageTokens 用户消息 Token 数
	UserMessageTokens int `json:"user_message_tokens"`
	// AssistantMessageTokens 助手消息 Token 数
	AssistantMessageTokens int `json:"assistant_message_tokens"`
	// ToolMessageTokens 工具消息 Token 数
	ToolMessageTokens int `json:"tool_message_tokens"`
	// ToolTokens 工具定义 Token 数
	ToolTokens int `json:"tool_tokens"`
}

// ContextWindow LLM 推理上下文窗口快照，包含系统消息、上下文消息和工具定义。
//
// 对应 Python: openjiuwen/core/context_engine/base.py (ContextWindow)
type ContextWindow struct {
	// SystemMessages 系统消息
	SystemMessages []llm_schema.BaseMessage `json:"system_messages"`
	// ContextMessages 上下文消息
	ContextMessages []llm_schema.BaseMessage `json:"context_messages"`
	// Tools 工具定义
	Tools []*schema.ToolInfo `json:"tools"`
	// Statistic 统计信息（值类型，零值始终可用，与 Python ContextStats() 默认实例对齐）
	Statistic ContextStats `json:"statistic"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMessages 合并系统消息和上下文消息，返回完整消息列表。
//
// 对应 Python: ContextWindow.get_messages()
func (w *ContextWindow) GetMessages() []llm_schema.BaseMessage {
	result := make([]llm_schema.BaseMessage, 0, len(w.SystemMessages)+len(w.ContextMessages))
	result = append(result, w.SystemMessages...)
	result = append(result, w.ContextMessages...)
	return result
}

// GetTools 返回工具列表。
//
// 对应 Python: ContextWindow.get_tools()
func (w *ContextWindow) GetTools() []*schema.ToolInfo {
	return w.Tools
}

// NewContextWindow 创建上下文窗口实例，所有字段初始化为零值。
//
// Statistic 字段初始化为 ContextStats 零值（与 Python ContextStats() 默认实例对齐），
// 消息和工具切片初始化为空切片（避免 JSON 序列化为 null）。
//
// 对应 Python: ContextWindow() 默认构造
func NewContextWindow() *ContextWindow {
	return &ContextWindow{
		SystemMessages:  make([]llm_schema.BaseMessage, 0),
		ContextMessages: make([]llm_schema.BaseMessage, 0),
		Tools:           make([]*schema.ToolInfo, 0),
		Statistic:       ContextStats{},
	}
}

// StatMessages 统计消息数量和 token 数，填充 ContextStats 各字段。
//
// 统计逻辑：
//   - 按角色计数消息数（system/user/assistant/tool）
//   - 优先使用最后一条 AssistantMessage 的 usage_metadata.total_tokens 作为 total_tokens
//   - 若无 usage_metadata，则逐条计算 token（TiktokenCounter 或 fallback 字符串长度/4）
//
// 对应 Python: Context._stat_messages(stat, messages)
//
// ⤵️ 待 5.31 Context 具体实现时回填实际统计逻辑
func (s *ContextStats) StatMessages(messages []llm_schema.BaseMessage, tokenCounter token.TokenCounter) {
	// ⤵️ 待 5.31 回填：按角色计数 + token 计算
	// 参见 Python: openjiuwen/core/context_engine/context/context.py (_stat_messages)
	//
	// 实现要点：
	//   1. s.TotalMessages = len(messages)
	//   2. 按角色计数：s.SystemMessages / s.UserMessages / s.AssistantMessages / s.ToolMessages
	//   3. token 计算优先级：
	//      a) 最后一条 AssistantMessage 的 usage_metadata.total_tokens → 直接赋值 s.TotalTokens 并返回
	//      b) 逐条调用 tokenCounter.CountMessages 或 fallback len(content)/4
	//   4. s.TotalDialogues 由 StatContextWindow 统一计算（依赖 ContextUtils.find_all_dialogue_round）
}

// StatTools 统计工具数量和 token 数，填充 ContextStats 的 Tools/ToolTokens 字段。
//
// 对应 Python: Context._stat_tools(stat, tools)
//
// ⤵️ 待 5.31 Context 具体实现时回填实际统计逻辑
func (s *ContextStats) StatTools(tools []*schema.ToolInfo, tokenCounter token.TokenCounter) {
	// ⤵️ 待 5.31 回填：工具计数 + token 计算
	// 参见 Python: openjiuwen/core/context_engine/context/context.py (_stat_tools)
	//
	// 实现要点：
	//   1. s.Tools = len(tools)
	//   2. 逐条计算工具 token：tokenCounter.CountTools 或 fallback len(name+description+parameters)/4
	//   3. s.ToolTokens = 各工具 token 之和
	//   4. s.TotalTokens += s.ToolTokens
}

// StatContextWindow 统计 ContextWindow 的完整统计信息。
//
// 内部调用 StatMessages + StatTools + 计算对话轮次，填充 window.Statistic 各字段。
// 对应 Python: Context._stat_context_window(window)
//
// ⤵️ 待 5.31 Context 具体实现时回填实际统计逻辑
func StatContextWindow(window *ContextWindow, tokenCounter token.TokenCounter) {
	// ⤵️ 待 5.31 回填：统计窗口消息 + 工具 + 对话轮次
	// 参见 Python: openjiuwen/core/context_engine/context/context.py (_stat_context_window)
	//
	// 实现要点：
	//   1. window.Statistic.StatMessages(window.GetMessages(), tokenCounter)
	//   2. window.Statistic.StatTools(window.GetTools(), tokenCounter)
	//   3. window.Statistic.TotalDialogues = 计算对话轮次（依赖 ContextUtils.FindAllDialogueRound）
}

// ──────────────────────────── 非导出函数 ────────────────────────────
