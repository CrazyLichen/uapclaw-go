// Package tool_discovery 提供渐进式工具发现的元工具集与 BM25 检索适配层。
// 两个元工具通过回调函数与 ProgressiveToolRail 协作：
//   - ToolSearchTool（tool_search）在 deferred 工具目录里做 BM25 检索，
//     返回匹配工具的完整 parameters schema，并把命中结果授权给当前会话
//   - ToolCallTool（tool_call）是固定的执行包装器：把搜索结果按准确名称与参数
//     交给 AbilityManager 嵌套执行，从而保留目标工具自身的 rail 生命周期
//
// 文件目录：
//
//	tool_discovery/
//	├── doc.go          # 包文档
//	├── bm25.go         # BM25ToolIndex：工具 schema 的 BM25 索引与指纹
//	├── tool_search.go  # ToolSearchTool 延迟工具搜索元工具
//	└── tool_call.go    # ToolCallTool 延迟工具执行包装器
//
// 对应 Python 代码：openjiuwen/harness/tools/tool_discovery/
package tool_discovery
