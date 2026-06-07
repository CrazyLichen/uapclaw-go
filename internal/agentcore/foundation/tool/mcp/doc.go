// Package mcp 提供 MCP（Model Context Protocol）协议工具的实现，
// 包括 MCPTool 工具类、McpToolCard 配置卡片、McpServerConfig 服务器配置
// 和 McpClient 客户端接口。
//
// MCPTool 是 Tool 接口的 MCP 协议实现，通过 McpClient 调用远程 MCP 服务器上的工具。
// MCPTool 不支持流式调用（Stream 返回 ErrStreamNotSupported）。
// Invoke 流程：参数格式化 → McpClient.CallTool → 提取紧凑结果 → 返回。
//
// McpClient 接口定义了与 MCP 服务器交互的标准方法（Connect/Disconnect/ListTools/CallTool 等），
// 具体实现放在 client 子包中（SseClient/StdioClient/StreamableHttpClient/OpenApiClient/PlaywrightClient）。
// NewMcpClient 工厂函数根据 McpServerConfig.ClientType 创建对应客户端。
//
// 共享类型（McpServerConfig、McpToolCard、McpClient、ConnectOption 等）定义在 types 子包中，
// mcp 包通过类型别名重导出，保持 API 向后兼容。
//
// 文件目录：
//
//	mcp/
//	├── doc.go           # 包文档
//	├── base.go          # 类型重导出 + MCPTool + ExtractMCPToolResultContent
//	├── client.go        # NewMcpClient 工厂函数
//	├── base_test.go     # 基础类型单元测试
//	├── client_test.go   # 工厂函数测试 + fakeMcpClient
//	├── types/
//	│   ├── doc.go           # 共享类型子包文档
//	│   └── types.go         # McpServerConfig + McpToolCard + McpClient + ConnectOption
//	└── client/
//	    ├── doc.go                        # 客户端子包文档
//	    ├── helpers.go                    # 共享辅助函数（结果转换、JSON Schema 解析）
//	    ├── sse_client.go                 # SseClient 实现
//	    ├── stdio_client.go               # StdioClient 实现
//	    ├── streamable_http_client.go     # StreamableHttpClient 实现
//	    ├── openapi_client.go             # OpenApiClient 实现
//	    └── playwright_client.go          # PlaywrightClient 实现
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/mcp/
package mcp
