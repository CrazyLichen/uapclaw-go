package iface

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
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
	// size ≤ 0 表示不限制；size < 0 返回错误；withHistory 控制是否包含历史消息
	GetMessages(size int, withHistory bool) ([]llm_schema.BaseMessage, error)
	// SetMessages 替换消息列表
	// withHistory 控制是否替换历史消息
	SetMessages(messages []llm_schema.BaseMessage, withHistory bool)
	// PopMessages 从尾部弹出消息
	// withHistory 控制是否从历史消息中弹出
	PopMessages(size int, withHistory bool) []llm_schema.BaseMessage
	// ClearMessages 清空消息
	// withHistory 控制是否清空历史消息
	// opts 透传给处理器，对齐 Python: clear_messages(**kwargs)
	ClearMessages(ctx context.Context, withHistory bool, opts ...Option) error
	// AddMessages 添加消息
	// message 接受 BaseMessage（单条）或 []BaseMessage（列表）
	// opts 透传给处理器，对齐 Python: add_messages(messages, **kwargs)
	AddMessages(ctx context.Context, message llm_schema.BaseMessage, opts ...Option) ([]llm_schema.BaseMessage, error)
	// GetContextWindow 构建上下文窗口供模型推理使用
	// windowSize ≤ 0 使用默认值；dialogueRound ≤ 0 使用默认值
	// opts 透传给处理器，对齐 Python: get_context_window(..., **kwargs)
	GetContextWindow(ctx context.Context, systemMessages []llm_schema.BaseMessage,
		tools []schema.ToolInfoInterface, windowSize int, dialogueRound int, opts ...Option) (*ContextWindow, error)
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
	// WorkspaceDir 返回工作目录路径
	//
	// 对应 Python: SessionModelContext.workspace_dir()
	WorkspaceDir() string
	// SetSessionRef 设置会话引用
	//
	// 对应 Python: SessionModelContext.set_session_ref()
	SetSessionRef(sess sessioninterfaces.SessionFacade)
	// GetSessionRef 获取会话引用
	//
	// 对应 Python: SessionModelContext.get_session_ref()
	GetSessionRef() sessioninterfaces.SessionFacade
	// OffloadMessages 将消息卸载到内存缓冲区
	//
	// 对应 Python: SessionModelContext.offload_messages()
	OffloadMessages(handle string, messages []llm_schema.BaseMessage)
	// SaveState 保存上下文状态为 map
	//
	// 对应 Python: SessionModelContext.save_state()
	SaveState() map[string]any
	// LoadState 从 map 恢复上下文状态
	//
	// 对应 Python: SessionModelContext.load_state()
	LoadState(state map[string]any)
	// CompressContext 主动压缩上下文
	//
	// 返回 "busy"/"compressed"/"noop"。
	// 对应 Python: SessionModelContext.compress_context()
	CompressContext(ctx context.Context, opts ...CompressContextOption) (string, error)
}

// ContextEngine 上下文引擎门面接口。
//
// 管理上下文池、处理器注册、会话状态持久化。
// 属于 Agent 级别组件（不在 Session 中），通过 agent.contextEngine 访问。
//
// 对应 Python: openjiuwen/core/context_engine/context_engine.py (ContextEngine)
type ContextEngine interface {
	// CreateContext 创建或获取上下文
	CreateContext(ctx context.Context, contextID string, sess sessioninterfaces.SessionFacade, opts ...CreateContextOption) (ModelContext, error)
	// GetContext 获取上下文（不存在返回 nil）
	GetContext(contextID string, sessionID string) ModelContext
	// CompressContext 主动压缩上下文，返回 "busy"/"compressed"/"noop"
	CompressContext(ctx context.Context, contextID string, sess sessioninterfaces.SessionFacade, opts ...CompressContextOption) (string, error)
	// ClearContext 清空上下文（三种粒度：全清/按session/按context+session）
	ClearContext(ctx context.Context, opts ...ClearContextOption) error
	// SaveContexts 批量持久化上下文状态，返回 contextID → state 映射
	SaveContexts(ctx context.Context, sess sessioninterfaces.SessionFacade, contextIDs []string) (map[string]any, error)
}

// ProcessorSpec 处理器规格，指定类型和配置。
//
// 对应 Python: (processor_type, processor_config) 元组。
// ConfigOverrides 支持 dict 级别的部分覆盖（对齐 Python 中 (key, dict) 形式的 override），
// 合并时将 dict 中的字段覆盖到 preset config 的对应字段上。
type ProcessorSpec struct {
	// Type 处理器类型标识
	Type string
	// Config 处理器配置
	Config ProcessorConfig
	// ConfigOverrides dict 级别的部分配置覆盖（snake_case 键名）
	// 对齐 Python: _merge_config_with_overrides(base_config, overrides)
	ConfigOverrides map[string]any
}

// ContextEngineOptions ContextEngine 构造器可选项
type ContextEngineOptions struct {
	// Workspace 工作空间
	Workspace *hworkspace.Workspace
	// SysOperation 系统操作接口
	SysOperation sysop.SysOperation
}

// CreateContextOptions CreateContext 方法可选项
type CreateContextOptions struct {
	// Processors 处理器规格列表
	Processors []ProcessorSpec
	// HistoryMessages 历史消息
	HistoryMessages []llm_schema.BaseMessage
	// TokenCounter Token 计数器
	TokenCounter token.TokenCounter
}

// CompressContextOptions CompressContext 方法可选项
type CompressContextOptions struct {
	// ProcessorTypes 压缩处理器类型过滤列表
	ProcessorTypes []string
	// SysOperation 系统操作接口，由 ContextEngine 透传
	SysOperation sysop.SysOperation
	// ModelName 模型名称，用于 resolve_context_max
	ModelName string
	// SessionID 显式 session_id fallback，对齐 Python compress_context(session_id=...)
	SessionID string
}

// ClearContextOptions ClearContext 方法可选项
type ClearContextOptions struct {
	// SessionID 会话 ID
	SessionID string
	// ContextID 上下文 ID
	ContextID string
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
	Tools []schema.ToolInfoInterface `json:"tools"`
	// Statistic 统计信息（值类型，零值始终可用，与 Python ContextStats() 默认实例对齐）
	Statistic ContextStats `json:"statistic"`
}

// ContextEngineOption ContextEngine 构造器选项函数
type ContextEngineOption func(*ContextEngineOptions)

// CreateContextOption CreateContext 方法选项函数
type CreateContextOption func(*CreateContextOptions)

// CompressContextOption CompressContext 方法选项函数
type CompressContextOption func(*CompressContextOptions)

// ClearContextOption ClearContext 方法选项函数
type ClearContextOption func(*ClearContextOptions)

// ──────────────────────────── 全局变量 ────────────────────────────

// 确保 contextID 参数可用（避免未使用导入编译错误）
var _ = fmt.Sprintf

// ──────────────────────────── 导出函数 ────────────────────────────

// WithWorkspace 设置工作空间
func WithWorkspace(w *hworkspace.Workspace) ContextEngineOption {
	return func(o *ContextEngineOptions) { o.Workspace = w }
}

// WithEngineSysOperation 设置上下文引擎的系统操作接口
func WithEngineSysOperation(op sysop.SysOperation) ContextEngineOption {
	return func(o *ContextEngineOptions) { o.SysOperation = op }
}

// WithProcessors 设置处理器规格列表
func WithProcessors(specs []ProcessorSpec) CreateContextOption {
	return func(o *CreateContextOptions) { o.Processors = specs }
}

// WithHistoryMessages 设置历史消息
func WithHistoryMessages(msgs []llm_schema.BaseMessage) CreateContextOption {
	return func(o *CreateContextOptions) { o.HistoryMessages = msgs }
}

// WithTokenCounter 设置 Token 计数器
func WithTokenCounter(tc token.TokenCounter) CreateContextOption {
	return func(o *CreateContextOptions) { o.TokenCounter = tc }
}

// WithProcessorTypes 设置压缩处理器类型过滤列表
func WithProcessorTypes(types []string) CompressContextOption {
	return func(o *CompressContextOptions) { o.ProcessorTypes = types }
}

// WithCompressSysOperation 设置压缩时的系统操作接口
func WithCompressSysOperation(op sysop.SysOperation) CompressContextOption {
	return func(o *CompressContextOptions) { o.SysOperation = op }
}

// WithModelName 设置模型名称，用于 resolve_context_max
func WithModelName(name string) CompressContextOption {
	return func(o *CompressContextOptions) { o.ModelName = name }
}

// WithCompressSessionID 设置显式 session_id fallback，对齐 Python compress_context(session_id=...)
func WithCompressSessionID(sid string) CompressContextOption {
	return func(o *CompressContextOptions) { o.SessionID = sid }
}

// WithSessionID 设置会话 ID（用于 ClearContext）
func WithSessionID(sid string) ClearContextOption {
	return func(o *ClearContextOptions) { o.SessionID = sid }
}

// WithContextID 设置上下文 ID（用于 ClearContext，需配合 WithSessionID）
func WithContextID(cid string) ClearContextOption {
	return func(o *ClearContextOptions) { o.ContextID = cid }
}

// NewContextEngineOptions 从选项列表构建 ContextEngineOptions
func NewContextEngineOptions(opts ...ContextEngineOption) *ContextEngineOptions {
	o := &ContextEngineOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// NewCreateContextOptions 从选项列表构建 CreateContextOptions
func NewCreateContextOptions(opts ...CreateContextOption) *CreateContextOptions {
	o := &CreateContextOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// NewCompressContextOptions 从选项列表构建 CompressContextOptions
func NewCompressContextOptions(opts ...CompressContextOption) *CompressContextOptions {
	o := &CompressContextOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// NewClearContextOptions 从选项列表构建 ClearContextOptions
func NewClearContextOptions(opts ...ClearContextOption) *ClearContextOptions {
	o := &ClearContextOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

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
func (w *ContextWindow) GetTools() []schema.ToolInfoInterface {
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
		Tools:           make([]schema.ToolInfoInterface, 0),
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
func (s *ContextStats) StatMessages(messages []llm_schema.BaseMessage, tokenCounter token.TokenCounter) {
	s.TotalMessages = len(messages)

	// 优先使用最后一条 AssistantMessage 的 usage_metadata.total_tokens
	if usageTokens := getLastAssistantUsageTokens(messages); usageTokens > 0 {
		s.TotalTokens = usageTokens
		return
	}

	// 逐条计算 token，按角色累加
	for _, msg := range messages {
		tokens := countSingleMessageTokens(msg, tokenCounter)
		switch msg.GetRole() {
		case llm_schema.RoleTypeSystem:
			s.SystemMessages++
			s.SystemMessageTokens += tokens
		case llm_schema.RoleTypeUser:
			s.UserMessages++
			s.UserMessageTokens += tokens
		case llm_schema.RoleTypeAssistant:
			s.AssistantMessages++
			s.AssistantMessageTokens += tokens
		case llm_schema.RoleTypeTool:
			s.ToolMessages++
			s.ToolMessageTokens += tokens
		}
	}
	s.TotalTokens = s.SystemMessageTokens + s.UserMessageTokens + s.AssistantMessageTokens + s.ToolMessageTokens
}

// StatTools 统计工具数量和 token 数，填充 ContextStats 的 Tools/ToolTokens 字段。
//
// 对应 Python: Context._stat_tools(stat, tools)
func (s *ContextStats) StatTools(tools []schema.ToolInfoInterface, tokenCounter token.TokenCounter) {
	s.Tools = len(tools)
	for _, t := range tools {
		s.ToolTokens += countToolTokens(t, tokenCounter)
	}
	s.TotalTokens += s.ToolTokens
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getLastAssistantUsageTokens 从消息列表中获取最后一条 AssistantMessage 的 usage_metadata.total_tokens。
// 返回 0 表示未找到有效的 usage_metadata。
func getLastAssistantUsageTokens(messages []llm_schema.BaseMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		am, ok := messages[i].(*llm_schema.AssistantMessage)
		if !ok {
			continue
		}
		if am.UsageMetadata != nil && am.UsageMetadata.TotalTokens > 0 {
			return am.UsageMetadata.TotalTokens
		}
	}
	return 0
}

// countSingleMessageTokens 计算单条消息的 token 数。
// 优先使用 tokenCounter.CountMessages，失败时 fallback 到 len(content)/4 向下取整。
// 对齐 Python: SessionModelContext._count_single_message_tokens()
func countSingleMessageTokens(msg llm_schema.BaseMessage, tokenCounter token.TokenCounter) int {
	if tokenCounter != nil {
		count, err := tokenCounter.CountMessages([]llm_schema.BaseMessage{msg}, "")
		if err == nil {
			return count
		}
	}
	// fallback：字符串长度 / 4 向下取整，对齐 Python len//4
	return len(msg.GetContent().Text()) / 4
}

// countToolTokens 计算单个工具定义的 token 数。
// 优先使用 tokenCounter.CountTools，失败时 fallback 到 len(name+description+parameters)/4 向下取整。
// 对齐 Python: SessionModelContext._count_tool_tokens()
func countToolTokens(toolInfo schema.ToolInfoInterface, tokenCounter token.TokenCounter) int {
	if tokenCounter != nil {
		count, err := tokenCounter.CountTools([]schema.ToolInfoInterface{toolInfo}, "")
		if err == nil {
			return count
		}
	}
	// fallback：拼接 name + description + parameters JSON，长度 / 4 向下取整，对齐 Python len//4
	text := toolInfo.GetName()
	if toolInfo.GetDescription() != "" {
		text += toolInfo.GetDescription()
	}
	if toolInfo.GetParameters() != nil {
		if data, err := json.Marshal(toolInfo.GetParameters()); err == nil {
			text += string(data)
		}
	}
	return len(text) / 4
}
