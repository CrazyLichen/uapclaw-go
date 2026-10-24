// Package cronbridge 提供 Agent 侧 cron 工具桥接（11.10）：
// CronTools（本地 cron_jobs.json 双写 + E2A 转发 Gateway）+
// CronRuntimeBridge（解析宿主 backend，适配 agentcore CronToolBackend 11 方法接口）+
// 委托降级单调度器（方案 B：Gateway 调度器运行中则委托 Reload，否则启动内嵌 fallback）。
//
// 路由上下文用 ctx values 传递（对齐 Python 4 个 ContextVar 的 CronToolRoute 单值），
// PushCronRoute 绑定后整轮 Agent 执行共享同一路由。
//
// 全局注入：SetGlobalGatewayPushFunc/SetGlobalUserPublisher/SetGlobalConfigLoader/
// SetGlobalAgentClient 由 app_gateway 装配时调用（Go 无 MessageHandler 单例，
// 以全局注入替代 Python MessageHandler.get_instance()）。
//
// 文件目录：
//
//	cron/
//	├── doc.go            # 包文档
//	├── cron_tools.go     # CronTools 本地双写 + E2A 转发 + ensureScheduler 方案 B 四分支
//	└── cron_runtime.go   # CronRuntimeBridge + cronToolsBackend 11 方法 + extractLegacyParams
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/cron/
package cronbridge
