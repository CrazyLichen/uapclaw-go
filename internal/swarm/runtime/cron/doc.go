// Package cron 提供定时任务的传输无关模型与持久化，供 Agent Runtime 使用。
//
// 上游把 cron 的模型/存储/表达式从 gateway 迁移到 runtime 层，
// `jiuwenswarm/gateway/cron` 下对应文件已退化为仅 7 行的兼容别名
// （`sys.modules[__name__] = _implementation`）。本包承接该实体实现。
//
// 文件目录：
//
//	cron/
//	├── doc.go                # 包文档
//	├── cron_expr.go          # 表达式归一化/校验/下次触发/唤醒提前量收敛
//	├── models.go             # CronJob/CronTarget/CronRunState + 归一化与校验
//	├── store_base.go         # CronJobStoreBackend 协议
//	├── store.go              # 文件存储（JSON + 原子写 + CAS 修订号）
//	├── etcd_store.go         # etcd 存储（前缀 watch + CAS）
//	├── etcd_client.go        # etcd 客户端同名转发（实体在 internal/common/etcd，对齐上游兼容导入层）
//	├── cron_job_mutations.go # 任务变更（创建/更新/删除的字段归一与校验）
//	├── factory.go            # 按配置产出文件或 etcd 存储
//	└── dingtalk_routing.go   # 钉钉目标路由解析
//
// 对应 Python 代码：jiuwenswarm/runtime/cron/（10 文件 2113 行）
package cron
