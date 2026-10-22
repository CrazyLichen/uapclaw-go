// Package hooks 提供 Gateway 层钩子处理器。
//
// GatewayHookHandler 在 Gateway 层执行 session / 生命周期类 hooks：
// 同步串行执行（保证 session 生命周期顺序）、超时默认 10s（短于 AgentServer 30s，
// 避免阻塞消息转发）、失败永不阻塞用户请求（只做日志记录）。
//
// 文件目录：
//
//	hooks/
//	├── doc.go     # 包文档
//	└── handler.go # GatewayHookHandler 4 事件方法
//
// 对应 Python 代码：jiuwenswarm/gateway/hooks/handler.py
package hooks
