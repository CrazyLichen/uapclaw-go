// Package auth 提供工具认证配置与结果，以及基于策略模式的认证执行框架。
//
// 工具（RestfulApi、MCP 客户端）在发起请求前，通过回调框架触发 TOOL_AUTH 事件，
// 由注册的认证策略（SSL、HeaderQuery）完成认证并返回结果，
// 调用方从中提取 TLS 配置或认证头注入实际请求。
//
// 认证策略：
//
//	AuthStrategy 接口 — 统一抽象
//	  ├── SSLAuthStrategy — SSL/TLS 证书认证（环境变量驱动）
//	  └── HeaderQueryAuthStrategy — 自定义请求头/查询参数认证
//
// 调用流程：
//
//	工具请求前 → CallbackFramework.TriggerTool(TOOL_AUTH, {auth_config: ...})
//	    → unifiedAuthHandler → AuthStrategyRegistry.ExecuteAuth()
//	    → 返回 ToolAuthResult{AuthData: {"tls_config" | "auth_provider"}}
//	    → 调用方提取结果注入请求
//
// 文件目录：
//
//	auth/
//	├── doc.go              # 包文档
//	├── auth.go             # ToolAuthConfig + ToolAuthResult 数据模型
//	└── auth_callback.go    # AuthType + AuthStrategy + Registry + 回调注册 + HeaderQueryProvider
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/auth/
package auth
