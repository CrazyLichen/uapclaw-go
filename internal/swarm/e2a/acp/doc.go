// Package acp 提供 E2A 与 ACP 协议互转的工具级纯函数（10.6.24d）。
//
// 本包对齐 Python jiuwenswarm/common/e2a/acp/：构造 ACP toolCall descriptor
// （toolCallId/title/kind/status/rawInput/locations/rawOutput）与 session/update
// 工具事件。24d 仅消费 BuildAcpToolDescriptor（权限确认桥）；其余四个函数
// 是 session_updates.py（11.25）的依赖，随包一并交付（零调用保留，⤴️ 11.25 消费）。
//
// 文件目录：
//
//	acp/
//	├── doc.go                # 包文档
//	└── acp_tool_updates.go   # descriptor 构造与 tool kind 推断
//
// 对应 Python 代码：jiuwenswarm/common/e2a/acp/acp_tool_updates.py
package acp
