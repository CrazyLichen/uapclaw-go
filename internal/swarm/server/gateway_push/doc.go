// Package gateway_push 提供 Gateway → AgentServer 的传输抽象与实现。
//
// 本包定义 AgentTransport 接口（Send/Recv/Close），并实现进程内 ChannelTransport
// （基于 Go channel），用于 chat/serve/acp/app 等单进程模式下 Gateway 与 AgentServer 通信。
// 跨进程模式（WebSocketTransport）将在后续领域实现。
//
// 文件目录：
//
//	gateway_push/
//	├── doc.go                 # 包文档
//	├── transport.go           # AgentTransport 接口定义
//	└── channel_transport.go   # ChannelTransport 进程内实现
//
// 对应 Python 代码：jiuwenswarm/server/gateway_push/
package gateway_push
