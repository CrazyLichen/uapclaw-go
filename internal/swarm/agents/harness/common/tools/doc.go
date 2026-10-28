// Package tools 提供 Swarm 侧轻量工具集（10.6.24a/24b/24c）：
// wiki 3 工具（内嵌 DeepAgent 编译知识库）、generate_image 文生图、
// mcp_* 4 工具（free/paid search、fetch webpage、exec command）、
// todo_compat 兼容层（status=deleted/delete 客户端 payload 映射）、
// send_file 全链（10.6.24b：WebFileDownloadManager HMAC 下载令牌 +
// SendFileToolkit chat.file 事件工具，push/history 经包级注入函数——
// Python 为函数体 lazy import 单例，Go 编译期成环改 agent_server 启动接线）、
// MultiSessionToolkit 并行子代理（agent.fast，10.6.24c：session_new/cancel/list
// goroutine 并发 + chat.session_result 推送 + sync.Once 汇总 chat.final，
// push/agentProvider 经包级注入函数）与 user_todos 用户待办（code 模式，
// 10.6.24c：六 action + 按 channel 隔离 markdown 持久化，channel 经 ctx 读取）。
//
// 全部工具对齐 Python jiuwenswarm/agents/harness/common/tools/ 同名文件，
// 包级 var 单例对齐 Python 模块级 @tool 实例。
//
// 文件目录：
//
//	tools/
//	├── doc.go                    # 包文档
//	├── wiki_tools.go             # wiki_ingest/wiki_query/wiki_lint + LLMWiki + sourceManifest
//	├── image_gen.go              # generate_image + invokeModelImageGeneration + ApplyImageGenModelConfigFromYAML
//	├── web_file_download.go      # WebFileDownloadManager——HMAC 下载令牌+三级密钥（10.6.24b）
//	├── send_file.go              # SendFileToolkit——chat.file 事件+双注入（10.6.24b）
//	├── multi_session_toolkits.go # MultiSessionToolkit——session_new/cancel/list（10.6.24c）
//	├── user_todo_tool.go         # user_todos——按 channel 隔离用户待办（10.6.24c）
//	├── mcp_toolkits.go           # GetMcpTools 聚合器
//	├── mcp_search.go             # mcp_free_search + mcp_paid_search + 4 provider
//	├── mcp_web_fetch.go          # mcp_fetch_webpage + Jina Reader 兜底
//	├── mcp_command.go            # mcp_exec_command + 安全检查 + 后台进程
//	├── mcp_command_windows.go    # Windows 专属 shell 探测（build tag 隔离）
//	├── mcp_command_other.go      # 非 Windows 平台桩（build tag 隔离）
//	└── todo_compat.go            # compatibleUpdateTodos + NewCompatibleTodoModifyTool
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/
package tools
