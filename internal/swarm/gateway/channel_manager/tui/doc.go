// Package tui 提供 /tui CLI 通道的路由装配：转发方法集合定义与本地 handler 注册。
//
// 本包是 channel_manager 下的通道实现之一，与 web 包平级：web 负责 Web 渠道的
// WebSocket 服务端，本包负责 TUI（CLI）渠道的路由 binding 与 5 个本地 ack handler。
//
// 依赖方向（重要）：本包**不 import gateway 包**。Install 阶段需要往 GatewayServer
// 注册 handler、handler 需要发送 res 帧，两者都以接口表达（RouteServer /
// ResponseSender），由装配点 app_gateway.go 传入 gateway.GatewayServer 实例。
// 这样既避免了 gateway → tui（装配）与 tui → gateway（能力）互相引用成环，也
// 与 Python「register_cli_handlers(channel=gateway_server) 注入 channel」的写法同构。
//
// 文件目录：
//
//	tui/
//	├── doc.go                   # 包文档
//	├── tui_memory_handlers.go   # 记忆 RPC 本地 handler（memory.list/edit/status/toggle/open，薄封装 memoryrpc）
//	└── tui_connect.go           # RouteParams/ResponseSender/RouteServer + 两个转发集合 + BuildRouteBinding + 本地 handler 注册
//
// 对应 Python 代码：jiuwenswarm/gateway/channel_manager/tui/tui_connect.py
package tui
