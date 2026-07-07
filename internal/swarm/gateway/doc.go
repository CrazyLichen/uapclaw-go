// Package gateway 提供 Gateway 服务器，负责接收外部请求并路由到 AgentServer。
//
// Gateway 是 uapclaw 的统一入口，同时提供 WebSocket RPC 端点、
// 静态文件服务和文件操作 HTTP API。通过 Transport 抽象与 AgentServer 通信。
//
// 文件目录：
//
//	gateway/
//	├── doc.go              # 包文档
//	├── app_gateway.go      # GatewayServer 启动入口，chi router 组装
//	├── embed.go            # go:embed 前端静态资源
//	├── file_api.go         # /file-api/* HTTP 路由处理
//	├── channel_manager/    # 渠道管理
//	├── message_handler/    # 消息处理器
//	└── routing/            # 路由与 AgentServer 客户端
//
// 对应 Python 代码：jiuwenswarm/gateway/
package gateway
