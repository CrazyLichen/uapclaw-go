// Package mcp 提供 MCP 资源浏览工具集，包含 ListMcpResourcesTool（列出 MCP 服务器资源）
// 和 ReadMcpResourceTool（读取 MCP 服务器资源内容）。
//
// 两个工具供 McpRail 在 Init 阶段注册到 ResourceMgr + AbilityManager，
// 使 LLM 能够发现和读取已注册 MCP 服务器上的资源。
//
// 对齐 Python: openjiuwen/harness/tools/mcp_tools.py
//
// 文件目录：
//
//	mcp/
//	├── doc.go              # 包文档
//	└── mcp_resources.go    # ListMcpResourcesTool + ReadMcpResourceTool
//
// 对应 Python 代码：openjiuwen/harness/tools/mcp_tools.py
package mcp
