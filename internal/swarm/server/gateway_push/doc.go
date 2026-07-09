// Package gateway_push 提供 Gateway ↔ AgentServer 的进程内传输实现。
//
// 本包提供 ChannelTransport（基于 Go channel），对齐 Python WebSocket 单连接模型。
// 传输接口定义在 swarm/transport 包（AgentTransport），本包仅提供进程内实现。
// Wire 编码工具（BuildServerPushWire、BuildConnectionAckFrame、WireRequestIDKey）
// 也已迁移到 swarm/transport 包。
//
// 文件目录：
//
//	gateway_push/
//	├── doc.go                 # 包文档
//	├── transport.go           # 接口合规声明（ChannelTransport 实现 transport.AgentTransport）
//	├── channel_transport.go   # ChannelTransport 进程内实现
//	└── channel_transport_test.go # ChannelTransport 测试
//
// 对应 Python 代码：jiuwenswarm/server/gateway_push/transport.py (进程内路径)
package gateway_push
