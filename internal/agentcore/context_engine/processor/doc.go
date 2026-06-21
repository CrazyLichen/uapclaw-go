// Package processor 提供上下文处理器插件体系。
//
// 处理器在两个生命周期点介入上下文管理：
//  1. OnAddMessages     — 消息即将被添加时
//  2. OnGetContextWindow — 上下文窗口即将返回时
//
// 每个处理器通过 Trigger* 方法判断是否介入，仅在返回 true 时
// 才调用对应的 On* 方法执行实际处理。
//
// 文件目录：
//
//	processor/
//	├── doc.go       # 包文档
//	├── base.go      # ContextProcessor 接口 + ProcessorConfig 接口 + ContextEvent 结构体
//	│               # + BaseProcessor 结构体 + ProcessorOption/Option + 构造函数
//	├── hooks.go     # BaseProcessor 钩子默认实现 + ProcessorType + IsAPIRound
//	├── state.go     # BaseProcessor SaveState/LoadState 默认实现
//	├── offload.go   # OffloadMessages 方法族 + offload 常量 + GenerateOffloadPath
//	├── usage.go     # CompressionUsage 追踪方法族（ExtractUsageMetadata/MergeCompressionUsage 等）
//	└── round.go     # GroupCompletedAPIRounds 包级导出函数
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/
package processor
