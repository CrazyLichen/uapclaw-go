// Package gateway_push 提供 AgentServer → Gateway 的下行推送抽象与实现。
//
// 定义 GatewayPushTransport 接口——所有 server_push 场景的统一推送入口，
// 以及 ChannelPushTransport 进程内实现（通过 AgentServer 单例发送）。
// 将来跨进程模式使用 WebSocketPushTransport（也在本包中实现）。
//
// 所有 server_push 场景（evolution 状态/cron 触发/文件推送/多会话工具等）
// 统一通过 GatewayPushTransport.SendPush 推送，不直接操作底层 Transport。
//
// 文件目录：
//
//	gateway_push/
//	├── doc.go            # 包文档
//	└── transport.go      # GatewayPushTransport 接口 + ChannelPushTransport 实现
//
// 对应 Python 代码：jiuwenswarm/server/gateway_push/
package gateway_push
