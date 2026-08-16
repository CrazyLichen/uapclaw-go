// Package coding_memory 提供编程记忆（CodingMemory）工具集，包含读取（CodingMemoryReadTool）、
// 写入（CodingMemoryWriteTool）和编辑（CodingMemoryEditTool）三个工具。
//
// 编程记忆工具用于在 coding_memory/ 目录下管理 Markdown 格式的记忆文件，
// 通过 lite.CodingMemoryToolContext 提供上下文（工作空间、系统操作、索引管理器等），
// 底层调用 lite.CodingMemoryReadWithContext / CodingMemoryWriteWithContext / CodingMemoryEditWithContext。
//
// 对齐 Python: openjiuwen/harness/tools/coding_memory.py
//
// 文件目录：
//
//	coding_memory/
//	├── doc.go                    # 包文档
//	└── coding_memory_tool.go     # 三个 Tool 结构体 + CreateCodingMemoryTools 工厂函数
//
// 对应 Python 代码：openjiuwen/harness/tools/coding_memory.py
package coding_memory
