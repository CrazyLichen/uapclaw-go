// Package acp 提供 E2A 与 ACP 协议互转的工具级纯函数（10.6.24d）。
//
// 本包对齐 Python jiuwenswarm/common/e2a/acp/：构造 ACP toolCall descriptor
// （toolCallId/title/kind/status/rawInput/locations/rawOutput）与 session/update
// 工具事件。BuildAcpToolDescriptor 由权限确认桥消费；protocol.go 四函数由
// AgentManager ACP 分支与 ACP 桥/stdio 通道（11.25）消费。
// session_updates.go 提供会话更新构造（agent/thought chunk 增量三态、
// usage、plan/todo/tool 派发、processing/idle 状态、final text 三态），
// 由 AcpGatewayBridge（gateway 包）与 AcpChannel（protocol/acp 包）消费。
//
// 文件目录：
//
//	acp/
//	├── doc.go                # 包文档
//	├── acp_tool_updates.go   # descriptor 构造与 tool kind 推断（11.25 起由桥/通道消费，转正）
//	├── idle_timer.go         # 空闲收尾定时器槽（桥与 stdio 通道共用，§9-4 引用比对）
//	├── protocol.go           # initialize/session_new/session_list/prompt 响应构造 + id/标量归一
//	└── session_updates.go    # session/update 构造与增量三态（11.25，session_updates.py 1:1）
//
// 对应 Python 代码：jiuwenswarm/common/e2a/acp/acp_tool_updates.py · protocol.py · session_updates.py
package acp
