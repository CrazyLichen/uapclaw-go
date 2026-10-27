// Package tools 提供 Swarm 侧轻量工具集（10.6.24a）：
// wiki 3 工具（内嵌 DeepAgent 编译知识库）、generate_image 文生图、
// mcp_* 4 工具（free/paid search、fetch webpage、exec command）、
// todo_compat 兼容层（status=deleted/delete 客户端 payload 映射）。
//
// 全部工具对齐 Python jiuwenswarm/agents/harness/common/tools/ 同名文件，
// 包级 var 单例对齐 Python 模块级 @tool 实例。
//
// 文件目录：
//
//	tools/
//	├── doc.go           # 包文档
//	├── wiki_tools.go    # wiki_ingest/wiki_query/wiki_lint + LLMWiki + sourceManifest
//	├── image_gen.go     # generate_image + invokeModelImageGeneration + ApplyImageGenModelConfigFromYAML
//	├── mcp_toolkits.go  # GetMcpTools 聚合器
//	├── mcp_search.go    # mcp_free_search + mcp_paid_search + 4 provider
//	├── mcp_web_fetch.go # mcp_fetch_webpage + Jina Reader 兜底
//	├── mcp_command.go   # mcp_exec_command + 安全检查 + 后台进程
//	└── todo_compat.go   # compatibleUpdateTodos + NewCompatibleTodoModifyTool
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/
package tools
