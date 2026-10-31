// Package a2a 提供 A2A（Agent2Agent）协议入站通道。
//
// 外部 A2A 客户端通过 HTTP JSON-RPC（含 SSE 流式）把本网关当作一个 A2A Agent 调用：
// 先读 /.well-known/agent-card.json 获取能力，再 SendMessage / SendStreamingMessage 取结果。
// 协议栈（JSON-RPC 分派、任务状态机、SSE、TaskStore、Agent Card 路由）由官方 Go SDK
// github.com/a2aproject/a2a-go/v2 的 a2asrv 提供；本包只实现「网关桥」：把 A2A 请求
// 翻译为内部 chat.send 消息交给 MessageHandler，再把回包翻译回 A2A 事件。
//
// 与 ACP/Web/TUI 通道并列，但自带 HTTP 监听端口（默认 127.0.0.1:19100），
// 不注册 GatewayServer 路由。出站 A2A（Agent 主动调用外部 A2A）不属于本包，
// 见领域九 9.81。
//
// 文件目录：
//
//	a2a/
//	├── doc.go           # 包文档
//	├── a2a_channel.go   # A2AChannelConfig + A2AChannel（pending/生命周期/HTTP 三路由）
//	├── a2a_executor.go  # a2aExecutor（a2asrv.AgentExecutor）事件序列
//	├── a2a_parts.go     # A2A parts ⇄ 内部 Message 映射（纯函数）
//	└── a2a_card.go      # AgentCard 构造
//
// 对应 Python 代码：jiuwenswarm/gateway/channel_manager/protocol/a2a/a2a_connect.py
// 设计文档：docs/superpowers/specs/2026-10-31-a2a-channel-11.26-design.md
package a2a
