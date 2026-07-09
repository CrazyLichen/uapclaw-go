// Package gateway 提供 Gateway 服务器，负责接收外部请求并路由到 AgentServer。
//
// Gateway 是 uapclaw 的统一入口，同时提供 WebSocket RPC 端点、
// 静态文件服务和文件操作 HTTP API。通过 Transport 抽象与 AgentServer 通信。
//
// 配置推送（agent.reload_config）在 Gateway 层实现：
//   - 启动时推送初始配置给 AgentServer（PushInitialConfig）
//   - 配置变更回调（OnConfigSaved），由 WebHandler 的 config.set/config.save_all 触发
//   - 条件触发 browser.runtime_restart
//
// 对齐 Python: jiuwenswarm/gateway/app_gateway.py (_on_config_saved)
//
// 文件目录：
//
//	gateway/
//	├── doc.go              # 包文档
//	├── app_gateway.go      # GatewayServer 启动入口，chi router 组装
//	├── config_push.go      # 配置推送（onConfigSaved、PushInitialConfig）
//	├── config_env_map.go   # 配置键→环境变量映射 + browserRuntimeKeys
//	├── embed.go            # go:embed 前端静态资源
//	├── file_api.go         # /file-api/* HTTP 路由处理
//	├── channel_manager/    # 渠道管理
//	├── message_handler/    # 消息处理器
//	└── routing/            # 路由与 AgentServer 客户端
//
// 对应 Python 代码：jiuwenswarm/gateway/
package gateway
