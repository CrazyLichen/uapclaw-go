package external

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// ToolSchema 工具 Schema 定义，对应 Python get_tool_schemas() 返回的 dict。
//
// Python: provider.get_tool_schemas() -> list[dict[str, Any]]
type ToolSchema struct {
	// Name 工具名称
	Name string `json:"name"`
	// Description 工具描述
	Description string `json:"description"`
	// Parameters 工具参数 JSON Schema
	Parameters map[string]any `json:"parameters"`
}

// ProviderOption Provider 可选参数的 functional option。
type ProviderOption func(*ProviderOptions)

// ProviderOptions Provider 可选参数集合。
//
// 对齐 Python initialize(**kwargs)/prefetch(query, **kwargs)/
// sync_turn(user_msg, assistant_msg, **kwargs) 中的 kwargs。
type ProviderOptions struct {
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
	// SessionID 会话标识
	SessionID string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithUserID 设置用户标识。
func WithUserID(id string) ProviderOption {
	return func(opts *ProviderOptions) { opts.UserID = id }
}

// WithScopeID 设置作用域标识。
func WithScopeID(id string) ProviderOption {
	return func(opts *ProviderOptions) { opts.ScopeID = id }
}

// WithSessionID 设置会话标识。
func WithSessionID(id string) ProviderOption {
	return func(opts *ProviderOptions) { opts.SessionID = id }
}

// MemoryProvider 外部记忆提供者接口。
// 对齐 Python: MemoryProvider(ABC) (openjiuwen/core/memory/external/provider.py)
//
// 所有方法对齐 Python ABC:
//   - 必须实现: Name, IsAvailable, Initialize, GetToolSchemas,
//     HandleToolCall, Prefetch, SyncTurn
//   - 可选覆盖: SystemPromptBlock, Shutdown, OnSessionEnd, IsInitialized
type MemoryProvider interface {
	// Name 返回 Provider 唯一名称。
	// Python: @property name -> str
	Name() string

	// IsAvailable 检查 Provider 是否已配置且就绪（无网络调用）。
	// Python: is_available() -> bool
	IsAvailable() bool

	// Initialize 初始化 Provider。
	// Python: async def initialize(self, **kwargs) -> None
	Initialize(ctx context.Context, opts ...ProviderOption) error

	// GetToolSchemas 返回 Provider 提供的工具 Schema 列表。
	// Python: get_tool_schemas() -> list[dict[str, Any]]
	GetToolSchemas() []ToolSchema

	// HandleToolCall 处理工具调用并返回结果字符串。
	// Python: async def handle_tool_call(self, tool_name: str, args: dict) -> str
	HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error)

	// Prefetch 根据查询预取记忆上下文。
	// Python: async def prefetch(self, query: str, **kwargs) -> str
	Prefetch(ctx context.Context, query string, opts ...ProviderOption) (string, error)

	// SyncTurn 同步一轮对话到外部记忆。
	// Python: async def sync_turn(self, user_msg: str, assistant_msg: str, **kwargs) -> None
	SyncTurn(ctx context.Context, userMsg, assistantMsg string, opts ...ProviderOption) error

	// SystemPromptBlock 返回 Provider 的系统提示词引导块。
	// Python: system_prompt_block() -> str（默认 ""）
	SystemPromptBlock() string

	// Shutdown 关闭 Provider 释放资源。
	// Python: async def shutdown() -> None（默认 no-op）
	Shutdown(ctx context.Context) error

	// OnSessionEnd 会话结束时回调。
	// Python: async def on_session_end(messages) -> None（默认 no-op）
	OnSessionEnd(ctx context.Context, messages []map[string]any) error

	// IsInitialized 返回 Provider 是否已初始化。
	// Python: @property is_initialized -> bool（默认 False）
	IsInitialized() bool
}

// BaseMemoryProvider MemoryProvider 可选方法的默认实现。
// 具体 Provider 嵌入此结构体后只需覆盖关心的方法。
// 对齐 Python: MemoryProvider 中的非 @abstractmethod 方法。
type BaseMemoryProvider struct{}

// SystemPromptBlock 默认返回空字符串。
// Python: def system_prompt_block(self) -> str: return ""
func (BaseMemoryProvider) SystemPromptBlock() string { return "" }

// Shutdown 默认无操作。
// Python: async def shutdown(self) -> None: pass
func (BaseMemoryProvider) Shutdown(_ context.Context) error { return nil }

// OnSessionEnd 默认无操作。
// Python: async def on_session_end(self, messages) -> None: pass
func (BaseMemoryProvider) OnSessionEnd(_ context.Context, _ []map[string]any) error { return nil }

// IsInitialized 默认返回 false。
// Python: @property is_initialized -> False
func (BaseMemoryProvider) IsInitialized() bool { return false }

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyOptions 应用 functional options 并返回 ProviderOptions。
func applyOptions(opts ...ProviderOption) ProviderOptions {
	var po ProviderOptions
	for _, o := range opts {
		o(&po)
	}
	return po
}
