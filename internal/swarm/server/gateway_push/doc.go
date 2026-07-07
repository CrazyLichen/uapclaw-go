// Package gateway_push 提供 Gateway → AgentServer 的传输抽象和实现。
//
// Transport 抽象支持两种传输模式：
// - ChannelTransport：进程内 Go channel 传输（单进程模式）
// - WebSocketTransport：跨进程 WebSocket 传输（后续实现）
//
// 文件目录：
//
//	gateway_push/
//	├── doc.go              # 包文档
//	├── transport.go        # AgentTransport 接口
//	└── channel_transport.go # ChannelTransport 实现
//
// 对应 Python 代码：jiuwenswarm/server/gateway_push/
package gateway_push
