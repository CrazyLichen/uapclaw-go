package lite

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemoryToolContext 编程记忆工具上下文。对齐 Python CodingMemoryToolContext
type CodingMemoryToolContext struct {
	LiteMemoryToolContextBase
	// CodingMemoryDir 编程记忆目录路径
	CodingMemoryDir string
}

// NewCodingMemoryToolContext 创建编程记忆工具上下文
func NewCodingMemoryToolContext() *CodingMemoryToolContext {
	return &CodingMemoryToolContext{
		LiteMemoryToolContextBase: LiteMemoryToolContextBase{
			NodeName: "coding_memory",
		},
	}
}
