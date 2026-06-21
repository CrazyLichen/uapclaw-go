package processor

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
