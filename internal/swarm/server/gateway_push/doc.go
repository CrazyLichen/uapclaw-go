// Package gateway_push 提供 Gateway ↔ AgentServer 的传输抽象与实现。
//
// 本包定义 AgentTransport 接口（Send/Recv/Close），对齐 Python WebSocket 单连接模型：
// Send 发送 JSON 字节（对齐 ws.send(json_str)），Recv 返回 JSON 字节接收通道（对齐 ws.recv()）。
// 不感知 E2A 协议语义，所有协议逻辑在 AgentClient 侧。
// 进程内实现 ChannelTransport（基于 Go channel），用于 chat/serve/acp/app 等单进程模式。
// 跨进程模式（WebSocketTransport）将在后续领域实现。
//
// 文件目录：
//
//	gateway_push/
//	├── doc.go                 # 包文档
//	├── transport.go           # AgentTransport 接口定义
//	├── channel_transport.go   # ChannelTransport 进程内实现
//	├── wire.go                # server_push wire 编码（对齐 Python gateway_push/wire.py）
//	└── wire_test.go           # wire 编码测试
//
// 对应 Python 代码：jiuwenswarm/server/gateway_push/
package gateway_push
