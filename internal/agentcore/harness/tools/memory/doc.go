// Package memory 提供记忆工具集（5 个 Tool），封装 lite 包的 WithContext 操作函数。
//
// 每个 Tool 持有 *lite.MemoryToolContext，实现 tool.Tool 接口：
//   - MemorySearchTool：语义搜索记忆
//   - MemoryGetTool：按行号切片读取记忆文件
//   - ReadMemoryTool：按 offset/limit 读取记忆文件
//   - WriteMemoryTool：写入/追加记忆文件
//   - EditMemoryTool：精确字符串替换记忆文件
//
// CreateMemoryTools 工厂函数统一创建 5 个 Tool 实例，
// 对齐 Python openjiuwen/harness/tools/memory.py 的 create_memory_tools。
//
// 文件目录：
//
//	memory/
//	├── doc.go              # 包文档
//	└── memory_tool.go      # 5 个 Tool 结构体 + CreateMemoryTools 工厂函数
//
// 对应 Python 代码：openjiuwen/harness/tools/memory.py
package memory
