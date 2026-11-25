// Package web 提供 Web 渠道的 WebSocket 服务端实现。
// WebChannel 通过 WebSocket 协议与前端浏览器通信，帧以 map 直接编解码
// （对齐上游全程 dict、无帧结构体），实现了全量 RPC 方法注册与分发。
// 两层消息架构（对齐 Python _handle_raw_message）：
//   - 第一层：normAndForward（NormalizeGatewayMessage → DeliverToMessageHandler），返回 false 不短路
//   - 第二层：本地 method handler（返回 ack 响应给客户端）
//
// 配置热重载链路（箭头表示调用先后）：
//   - 配置面板 RPC（config.get/set/save_all/validate_model、models.*、locale.*、
//     path.*、memory.forbidden.*、permissions.owner_scopes.*）经 configForwardToAgent
//     转发到 AgentServer（27.4；真相源=AgentServer 侧 control/configpanel）
//
// 文件目录：
//
//	web/
//	├── doc.go              # 包文档
//	├── web_connect.go      # WebChannel 核心（WS 服务端 + 连接管理 + 两层消息架构）
//	├── app_web_handlers.go # 全部 RPC handler + 注册 + 配置写入 + 内存采集
//	├── web_http_routes.go      # 27-A B2：/api/v1 CORE 路由 + envelope + 三分发通道（unary/stream/history）
//	├── http_routes_mapped.go   # 27-A B2：mapped 路由表全量（130 行）+ catalog + 校验
//	├── web_http_dispatch.go    # 27-A B2：HTTP 分发面（回环信任边界 + Message 构造）
//	├── web_http_timeouts.go    # 27-A B2：五档超时 env 函数
//	├── web_outbound_http.go    # 27-A B2：outbound 三符号（JSON/SSE outbound + bind_http_session）
//	├── web_delivery_registry.go# 27-A B2：请求级 outbound 注册表（B3 前置）
//	├── web_invoke.go           # 27-A B2：dispatch_web_request 共享入站管线
//	├── container_file_http_download.go # 27-A B2：verified-asset 下载半区（E2A 分块 + HEAD/Range/416）
//
// 对应 Python 代码：jiuwenswarm/gateway/channel_manager/web/
// （前端源码与静态/文件服务在 channels/web，对齐 Python channels/web/）
package web
