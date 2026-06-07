// Package client 提供 MCP 客户端各传输协议的具体实现。
//
// 每种客户端实现 mcp 包中定义的 McpClient 接口：
//   - SseClient — SSE (Server-Sent Events) 传输
//   - StdioClient — Stdio 子进程传输
//   - StreamableHttpClient — Streamable HTTP 传输
//   - OpenApiClient — OpenAPI 规格解析客户端
//   - PlaywrightClient — Playwright 浏览器工具客户端（SSE/stdio 双传输）
//
// 使用 mcp.NewMcpClient 工厂函数根据 McpServerConfig.ClientType 创建对应客户端。
//
// 文件目录：
//
//	client/
//	├── doc.go                        # 子包文档
//	├── sse_client.go                 # SseClient 实现
//	├── stdio_client.go               # StdioClient 实现
//	├── streamable_http_client.go     # StreamableHttpClient 实现
//	├── openapi_client.go             # OpenApiClient 实现
//	└── playwright_client.go          # PlaywrightClient 实现
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/mcp/client/
package client
