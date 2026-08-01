package lite

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemoryToolContext 编程记忆工具上下文。⤵️ 回填: 7.3
type CodingMemoryToolContext struct {
	LiteMemoryToolContextBase
	// CodingMemoryDir 编程记忆目录路径
	CodingMemoryDir string
	// NodeName 节点名称，固定 "coding_memory"
	NodeName string
}
