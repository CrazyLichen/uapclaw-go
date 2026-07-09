// Package web 提供 Web 渠道的 WebSocket 服务端实现。
//
// WebChannel 通过 WebSocket 协议与前端浏览器通信，
// 支持 req/res/event 三种帧类型，实现了全量 RPC 方法注册与分发。
//
// 配置热重载链路：
//   - config.set → ApplyConfigPayload → NotifyConfigSavedOnce → onConfigSaved
//   - config.save_all → ApplyConfigPayload + models + team → NotifyConfigSavedOnce → onConfigSaved
//
// 文件目录：
//
//	web/
//	├── doc.go              # 包文档
//	├── frame.go            # 帧协议类型定义和编解码
//	├── config_apply.go     # 配置写入逻辑（ApplyConfigPayload、NotifyConfigSavedOnce）
//	├── web_connect.go      # WebChannel 核心（WS 服务端 + 连接管理）
//	├── web_handlers.go     # RPC 分发器和方法处理器（RegisterWebHandlers）
//	└── frontend/           # React 前端源码和构建产物
//
// 对应 Python 代码：jiuwenswarm/gateway/channel_manager/web/
package web
