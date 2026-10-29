// Package acp 提供 ACP stdio 通道（11.25）。
//
// AcpChannel 是 ACP 协议的 CLI/IDE 侧通道：stdin 每行一个 JSON
// （E2A envelope 或 ACP JSON-RPC 请求/响应三路由），stdout 每行一个
// JSON-RPC 帧（jsonrpc 模式）或 E2AResponse 帧（e2a 模式）。
// 可选 WS 客户端把入站消息转发到 Gateway（模式二）。
//
// 与 gateway 包 AcpGatewayBridge 的分工：桥挂在 GatewayServer /acp 路由上
// 服务 WS IDE；本通道是独立进程形态（CLI 直连 / 连网关）。
//
// 文件目录：
//
//	acp/
//	├── doc.go           # 包文档
//	└── acp_channel.go   # AcpChannel 本体（三路由 + jsonrpc 六方法 + E2A 双模式
//	                     #   + pending/finalize 家族 + 测试钩子；WS 客户端族 11.25 Task 9）
//
// 对应 Python 代码：jiuwenswarm/gateway/channel_manager/protocol/acp/acp_connect.py
// （AcpChannel L663-1738；常量 L47-52；配置 L55-63；上下文 L65-81）。
package acp
