// Package cron 提供定时任务（Cron）调度服务：表达式归一化、任务持久化、
// 事件堆调度（wake/push/push_update 三分支）、API 门面。
//
// 核心组件：
//   - CronJob/CronRunState：数据模型（models.go）
//   - NormalizeCronExpr/NextFireTime：cron 表达式处理（cron_expr.go，go-quartz 驱动，
//     含 croniter→Quartz dow 约定变换）
//   - CronJobStore：JSON 持久化（store.go，tmp+rename 原子写）
//   - CronSchedulerService：调度器（scheduler.go，事件堆 + 进程级注册表 + MessagePublisher 接口）
//   - CronController：API 门面（controller.go，WebChannel handlers 用）
//
// 方案 B 委托降级单调度器：Gateway 启动时 SetProcessScheduler 注册；
// Agent 侧 CronTools.ensureScheduler 检查注册表，已注册则委托 Reload
// 而非启动内嵌 fallback（修正 Python 单进程双跑缺陷）。
//
// 不移植：cron_json_convert.py（CLI 工具非运行时）；
// controller.py get_tools()（工具集已由 9.38 agentcore/harness/tools/cron 实现）。
//
// 文件目录：
//
//	cron/
//	├── doc.go           # 包文档
//	├── models.go        # CronJob/CronRunState 模型 + 渠道校验归一化
//	├── cron_expr.go     # 表达式归一化 + dow 变换 + NextFireTime
//	├── store.go         # CronJobStore JSON 持久化
//	├── scheduler.go     # CronSchedulerService 事件堆调度器
//	└── controller.go    # CronController API 门面
//
// 对应 Python 代码：jiuwenswarm/gateway/cron/
package cron
