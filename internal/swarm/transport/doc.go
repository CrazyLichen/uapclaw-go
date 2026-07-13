// Package transport 提供 Gateway ↔ AgentServer 的传输抽象、Wire 编码工具与进程内传输实现。
//
// 本包定义 AgentTransport 接口（Send/Recv/Close），对齐 Python WebSocket 单连接模型，
// E2A Wire 编码工具函数（WireRequestIDKey、BuildConnectionAckFrame、BuildServerPushWire），
// 以及进程内传输实现 ChannelTransport（基于 Go channel）。
// 将来跨进程传输实现 WebSocketTransport 也在本包中。
//
// 文件目录：
//
//	transport/
//	├── doc.go                 # 包文档
//	├── interface.go           # AgentTransport 接口定义
//	├── channel_transport.go   # ChannelTransport 进程内实现
//	├── wire.go                # Wire 编码工具（对齐 Python gateway_push/wire.py + agent_client.py）
//	├── wire_test.go           # Wire 编码测试
//	└── channel_transport_test.go # ChannelTransport 测试
//
// 对应 Python 代码：jiuwenswarm/server/gateway_push/transport.py (进程内路径) + wire.py + agent_client.py
package transport
