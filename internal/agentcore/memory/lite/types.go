package lite

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryChunk 记忆分块。对齐 Python MemoryChunk
type MemoryChunk struct {
	// Text 分块文本内容
	Text string
	// StartLine 起始行号
	StartLine int
	// EndLine 结束行号
	EndLine int
}
