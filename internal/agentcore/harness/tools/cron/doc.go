// Package cron 提供定时任务（Cron）工具集，包含统一入口工具（action 路由）
// 和可选的 7 个遗留兼容工具（cron_list_jobs/cron_get_job/cron_create_job/
// cron_update_job/cron_delete_job/cron_toggle_job/cron_preview_job）。
//
// 统一入口工具通过 action 字段路由 8 种操作：status/list/add/update/remove/run/runs/wake，
// 使用 MapFunction 直接操作 map 输入以保留 kwargs 剩余字段。
// 遗留兼容工具使用 InvokeFunction + 明确 struct 输入，参数固定无需 kwargs。
//
// 核心抽象为 CronToolBackend 接口（11 个方法），宿主提供实现；
// CronToolContext 结构体绑定 channel_id/session_id/metadata/mode，
// 计算工具作用域用于工具名和 agentID 生成。
//
// 对齐 Python: openjiuwen/harness/tools/cron.py
//
// 文件目录：
//
//	cron/
//	├── doc.go       # 包文档
//	├── backend.go   # CronToolContext 结构体 + CronToolBackend 接口 + toolScope 辅助
//	├── dispatch.go  # dispatchCronAction 路由分发器 + 辅助函数
//	└── factory.go   # CreateCronTools 工厂 + makeLegacyTool + targetSchema + 遗留输入结构体
//
// 对应 Python 代码：openjiuwen/harness/tools/cron.py
package cron
