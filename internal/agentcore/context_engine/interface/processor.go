package iface

import (
	"context"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// ProcessorConfig 处理器配置接口，所有处理器配置必须实现。
//
// 各具体处理器定义自己的 Config 结构体并实现此接口，
// 基类通过接口持有配置，子类通过类型断言获取具体配置。
//
// 对应 Python: pydantic.BaseModel（作为处理器配置基类）
type ProcessorConfig interface {
	// Validate 校验配置参数
	Validate() error
}

// ContextProcessor 上下文处理器接口，所有处理器插件必须实现。
//
// 处理器在两个生命周期点介入上下文管理：
//  1. OnAddMessages      — 消息即将被添加时
//  2. OnGetContextWindow  — 上下文窗口即将返回时
//
// 每个处理器通过 Trigger* 方法判断是否介入，仅在返回 true 时
// 才调用对应的 On* 方法执行实际处理。实现必须是无状态的，
// 或通过 SaveState/LoadState 支持跨会话恢复。
//
// 对应 Python: openjiuwen/core/context_engine/processor/base.py (ContextProcessor)
type ContextProcessor interface {
	// OnAddMessages 处理即将添加的消息，返回 ContextEvent 和变换后的消息列表。
	// 仅在 TriggerAddMessages 返回 true 时调用。
	OnAddMessages(ctx context.Context, mc ModelContext, messages []llm_schema.BaseMessage, opts ...Option) (*ContextEvent, []llm_schema.BaseMessage, error)

	// OnGetContextWindow 处理即将返回的上下文窗口，返回 ContextEvent 和变换后的窗口。
	// 仅在 TriggerGetContextWindow 返回 true 时调用。
	OnGetContextWindow(ctx context.Context, mc ModelContext, cw ContextWindow, opts ...Option) (*ContextEvent, ContextWindow, error)

	// TriggerAddMessages 判断是否需要介入消息添加。每次 AddMessages 调用均执行，必须轻量。
	TriggerAddMessages(ctx context.Context, mc ModelContext, messages []llm_schema.BaseMessage, opts ...Option) (bool, error)

	// TriggerGetContextWindow 判断是否需要介入上下文窗口获取。每次 GetContextWindow 调用均执行，必须轻量。
	TriggerGetContextWindow(ctx context.Context, mc ModelContext, cw ContextWindow, opts ...Option) (bool, error)

	// SaveState 导出处理器内部状态为可序列化的 map。
	SaveState() map[string]any

	// LoadState 从 map 恢复处理器内部状态。
	LoadState(state map[string]any)

	// ProcessorType 返回处理器类型标识字符串（Go 结构体名）。
	ProcessorType() string
}

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEvent 上下文处理器执行结果，由各 Processor 的 OnAddMessages / OnGetContextWindow 返回。
//
// 当处理器实际执行了操作时返回非 nil 的 ContextEvent，携带修改了哪些消息索引、
// 压缩摘要和压缩用量信息。Context 实例读取这些字段构建 ContextCompressionState。
// 处理器未触发（noop）时返回 nil。
//
// 对应 Python: openjiuwen/core/context_engine/processor/base.py (ContextEvent)
type ContextEvent struct {
	// EventType 处理器类型标识（如 "DialogueCompressor"、"MessageOffloader"）
	EventType string `json:"event_type"`
	// MessagesToModify 被处理器修改的消息索引列表
	MessagesToModify []int `json:"messages_to_modify"`
	// CompactSummary 压缩摘要文本
	CompactSummary string `json:"compact_summary"`
	// CompressionUsage 压缩调用用量（token 数、费用等）
	CompressionUsage map[string]any `json:"compression_usage,omitempty"`
}

// ProcessorOption 处理器可选参数，替代 Python **kwargs。
//
// 对应 Python: ContextProcessor.offload_messages(**kwargs) 中的关键字参数
type ProcessorOption struct {
	// SysOperation 系统操作接口
	// ⤵️ 9.32 回填：将 any 替换为 SysOperation 接口类型
	SysOperation any
	// OffloadHandle 卸载句柄，未指定时自动生成 UUID
	OffloadHandle string
	// OffloadType 卸载类型："filesystem" 或 "in_memory"
	OffloadType string
	// OffloadPath 卸载文件路径，未指定时自动生成
	OffloadPath string
	// Extra 额外参数
	Extra map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Option 处理器选项函数类型
type Option func(*ProcessorOption)

// WithSysOperation 设置系统操作接口
// ⤵️ 9.32 回填参数类型
func WithSysOperation(op any) Option {
	return func(o *ProcessorOption) { o.SysOperation = op }
}

// WithOffloadHandle 设置卸载句柄
func WithOffloadHandle(handle string) Option {
	return func(o *ProcessorOption) { o.OffloadHandle = handle }
}

// WithOffloadType 设置卸载类型
func WithOffloadType(offloadType string) Option {
	return func(o *ProcessorOption) { o.OffloadType = offloadType }
}

// WithOffloadPath 设置卸载文件路径
func WithOffloadPath(path string) Option {
	return func(o *ProcessorOption) { o.OffloadPath = path }
}

// WithExtra 设置额外参数
func WithExtra(key string, value any) Option {
	return func(o *ProcessorOption) {
		if o.Extra == nil {
			o.Extra = make(map[string]any)
		}
		o.Extra[key] = value
	}
}

// NewProcessorOption 从选项列表构建 ProcessorOption
func NewProcessorOption(opts ...Option) *ProcessorOption {
	po := &ProcessorOption{}
	for _, opt := range opts {
		opt(po)
	}
	return po
}

// ──────────────────────────── 非导出函数 ────────────────────────────
