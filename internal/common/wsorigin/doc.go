// Package wsorigin 提供 WebSocket Origin 安全校验。
//
// 用于验证 WebSocket 连接的 Origin 头，防止跨站 WebSocket 劫持（CSWSH）。
//
// 本包包含三层抽象：
//   - 核心层：OriginChecker 结构体 + IsEnabled/IsAllowed/GetAllowedHosts/ForbiddenResponse 方法
//     纯逻辑，不依赖任何 WebSocket 框架。
//   - gorilla/websocket 适配层：GorillaCheckOrigin() 返回 Upgrader.CheckOrigin 函数
//   - net/http 适配层：HTTPMiddleware() 返回中间件，拦截 WebSocket 升级请求
//
// 配置方式（环境变量驱动）：
//
//	UAPCLAW_ENABLE_ORIGIN_CHECK=1                     # 启用校验（默认关闭）
//	UAPCLAW_WS_ALLOWED_ORIGIN_HOSTS=example.com,localhost  # 白名单（逗号分隔）
//
// 白名单特殊值 "none"：允许无 Origin 头的请求（如非浏览器客户端）。
//
// 文件目录：
//
//	wsorigin/
//	├── doc.go           # 包文档
//	├── origin.go        # 核心校验逻辑（OriginChecker 结构体 + 方法）
//	├── gorilla.go       # gorilla/websocket Upgrader.CheckOrigin 适配器
//	└── nethttp.go       # net/http 中间件适配器
//
// 对应 Python 代码：jiuwenswarm/common/security/ws_origin.py
package wsorigin
