package lite

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryToolContext 通用记忆工具上下文。对齐 Python MemoryToolContext
type MemoryToolContext struct {
	LiteMemoryToolContextBase
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryToolContext 创建通用记忆工具上下文
func NewMemoryToolContext() *MemoryToolContext {
	return &MemoryToolContext{
		LiteMemoryToolContextBase: LiteMemoryToolContextBase{
			NodeName: "memory",
		},
	}
}
