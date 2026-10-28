// Package acp 提供 E2A 与 ACP 协议互转的工具级纯函数（10.6.24d）。
//
// 本包对齐 Python jiuwenswarm/common/e2a/acp/：构造 ACP toolCall descriptor
// （toolCallId/title/kind/status/rawInput/locations/rawOutput）与 session/update
// 工具事件。BuildAcpToolDescriptor 由权限确认桥消费；protocol.go 四函数由
// AgentManager ACP 分支与 session_updates.py（11.25）消费。
//
// 文件目录：
//
//	acp/
//	├── doc.go                # 包文档
//	├── acp_tool_updates.go   # descriptor 构造与 tool kind 推断
//	└── protocol.go           # initialize/session_new/session_list/prompt 响应构造
//
// 对应 Python 代码：jiuwenswarm/common/e2a/acp/acp_tool_updates.py · protocol.py
package acp
