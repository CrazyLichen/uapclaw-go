package schema

import (
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

// ContextCompressionMetric 上下文压缩前后指标快照。
//
// 对应 Python: openjiuwen/core/context_engine/schema/context_state.py (ContextCompressionMetric)
type ContextCompressionMetric struct {
	// Time 观测时间（ISO 8601 毫秒精度），空串表示未记录
	Time string `json:"time,omitempty"`
	// Messages 消息数量
	Messages int `json:"messages"`
	// Tokens Token 数量
	Tokens int `json:"tokens"`
	// ContextPercent 上下文使用百分比（0-100），0 且 omitempty 省略表示无上限
	ContextPercent int `json:"context_percent,omitempty"`
}

// ContextCompressionSaved 上下文压缩节省量。
//
// 对应 Python: openjiuwen/core/context_engine/schema/context_state.py (ContextCompressionSaved)
type ContextCompressionSaved struct {
	// Messages 节省的消息数
	Messages int `json:"messages"`
	// Tokens 节省的 Token 数
	Tokens int `json:"tokens"`
	// Percent 节省百分比
	Percent float64 `json:"percent"`
}

// ContextCompressionUsage 上下文压缩 LLM 调用用量。
//
// 对应 Python: openjiuwen/core/context_engine/schema/context_state.py (ContextCompressionUsage)
type ContextCompressionUsage struct {
	// Calls LLM 调用次数
	Calls int `json:"calls"`
	// InputTokens 输入 Token 数
	InputTokens int `json:"input_tokens"`
	// OutputTokens 输出 Token 数
	OutputTokens int `json:"output_tokens"`
	// TotalTokens 总 Token 数
	TotalTokens int `json:"total_tokens"`
	// CacheTokens 缓存 Token 数
	CacheTokens int `json:"cache_tokens"`
	// InputCost 输入费用
	InputCost float64 `json:"input_cost"`
	// OutputCost 输出费用
	OutputCost float64 `json:"output_cost"`
	// TotalCost 总费用
	TotalCost float64 `json:"total_cost"`
	// ModelName 模型名称
	ModelName string `json:"model_name"`
	// Details 每次 LLM 调用的原始用量详情
	Details []map[string]any `json:"details,omitempty"`
}

// ContextCompressionState 上下文压缩状态完整快照。
//
// 由 ProcessorStateRecorder.BuildState() 构建，记录一次压缩操作的完整生命周期。
// 通过回调框架和 session stream 发射，供外部系统观测上下文引擎行为。
//
// 对应 Python: openjiuwen/core/context_engine/schema/context_state.py (ContextCompressionState)
type ContextCompressionState struct {
	// Type 事件类型标识，固定为 ContextCompressionStateType
	Type string `json:"type"`
	// OperationID 操作唯一标识
	OperationID string `json:"operation_id"`
	// Status 操作状态
	Status CompressionStatus `json:"status"`
	// Phase 操作阶段
	Phase CompressionPhase `json:"phase"`
	// Processor 处理器类型名称
	Processor string `json:"processor"`
	// Model 使用的 LLM 模型名称
	Model string `json:"model"`
	// Before 压缩前指标
	Before ContextCompressionMetric `json:"before"`
	// After 压缩后指标，nil 表示操作未完成或被跳过
	After *ContextCompressionMetric `json:"after,omitempty"`
	// Statistic 上下文统计快照
	Statistic iface.ContextStats `json:"statistic"`
	// Saved 压缩节省量，nil 表示无节省（操作未完成）
	Saved *ContextCompressionSaved `json:"saved,omitempty"`
	// CompressionUsage LLM 调用用量，nil 表示未调用 LLM
	CompressionUsage *ContextCompressionUsage `json:"compression_usage,omitempty"`
	// DurationMs 操作耗时（毫秒），0 且 omitempty 省略表示未完成
	DurationMs int `json:"duration_ms,omitempty"`
	// ContextMax 上下文窗口 Token 上限，0 且 omitempty 省略表示无上限
	ContextMax int `json:"context_max,omitempty"`
	// Summary 人类可读的操作摘要
	Summary string `json:"summary"`
	// CompactSummary 紧凑摘要（供流式输出）
	CompactSummary string `json:"compact_summary"`
	// Error 错误信息，空串表示无错误
	Error string `json:"error,omitempty"`
}

// CompressionStatus 压缩操作状态字面量类型。
//
// 对应 Python: Literal["started", "completed", "noop", "skipped", "failed"]
type CompressionStatus string

// CompressionPhase 压缩操作阶段字面量类型。
//
// 对应 Python: Literal["add_messages", "get_context_window", "active_compress"]
type CompressionPhase string

// ──────────────────────────── 常量 ────────────────────────────
const (
	// CompressionStarted 压缩操作已启动
	CompressionStarted CompressionStatus = "started"
	// CompressionCompleted 压缩操作已完成
	CompressionCompleted CompressionStatus = "completed"
	// CompressionNoop 压缩操作无变更
	CompressionNoop CompressionStatus = "noop"
	// CompressionSkipped 压缩操作已跳过
	CompressionSkipped CompressionStatus = "skipped"
	// CompressionFailed 压缩操作已失败
	CompressionFailed CompressionStatus = "failed"
)

const (
	// PhaseAddMessages 添加消息阶段
	PhaseAddMessages CompressionPhase = "add_messages"
	// PhaseGetContextWindow 获取上下文窗口阶段
	PhaseGetContextWindow CompressionPhase = "get_context_window"
	// PhaseActiveCompress 主动压缩阶段
	PhaseActiveCompress CompressionPhase = "active_compress"
)

// ContextCompressionStateType 压缩状态事件类型标识。
// 用于回调事件名和 session stream 的 OutputSchema.Type 字段。
//
// 对应 Python: CONTEXT_COMPRESSION_STATE_TYPE = "context.compression_state"
const ContextCompressionStateType = "context.compression_state"
