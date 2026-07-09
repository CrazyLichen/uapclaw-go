// Package transport 提供 Gateway ↔ AgentServer 的传输抽象与 Wire 编码工具。
//
// 本包定义 AgentTransport 接口（Send/Recv/Close），对齐 Python WebSocket 单连接模型，
// 以及 E2A Wire 编码工具函数（WireRequestIDKey、BuildConnectionAckFrame、BuildServerPushWire）。
// 不包含传输实现，实现由 server/gateway_push（ChannelTransport）等包提供。
//
// 文件目录：
//
//	transport/
//	├── doc.go           # 包文档
//	├── interface.go     # AgentTransport 接口定义
//	├── wire.go          # Wire 编码工具（对齐 Python gateway_push/wire.py + agent_client.py）
//	└── wire_test.go     # Wire 编码测试
//
// 对应 Python 代码：jiuwenswarm/server/gateway_push/wire.py + jiuwenswarm/gateway/routing/agent_client.py
package transport
