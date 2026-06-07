// Package types 提供 MCP 协议的共享类型定义，供 mcp 包和 client 子包共同引用。
//
// 将 McpServerConfig、McpToolCard、McpClient 接口、ConnectOption 等类型
// 放在独立子包中，避免 mcp ↔ client 的循环导入。
//
// 文件目录：
//
//	types/
//	├── doc.go           # 子包文档
//	└── types.go         # 共享类型定义
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/mcp/base.py + mcp_client.py
package types
