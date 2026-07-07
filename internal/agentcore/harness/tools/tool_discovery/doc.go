// Package tool_discovery 提供渐进式工具发现元工具集，包含 SearchToolsTool（搜索候选工具）
// 和 LoadToolsTool（加载真实工具到当前会话可见集合）。
// 两个元工具通过回调函数与 ProgressiveToolRail 协作：
//   - SearchToolsTool 调用 searchToolsFn 执行搜索，再调用 appendTraceFn 记录搜索轨迹
//   - LoadToolsTool 调用 loadToolsFn 执行工具加载
//
// 对齐 Python: openjiuwen/harness/tools/tool_discovery/
//
// 文件目录：
//
//	tool_discovery/
//	├── doc.go              # 包文档
//	├── search_tools.go     # SearchToolsTool 搜索候选工具元工具
//	└── load_tools.go       # LoadToolsTool 加载工具元工具
//
// 对应 Python 代码：openjiuwen/harness/tools/tool_discovery/
package tool_discovery
