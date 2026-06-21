// Package processor 提供上下文处理器插件体系。
//
// 处理器在两个生命周期点介入上下文管理：
//  1. OnAddMessages     — 消息即将被添加时
//  2. OnGetContextWindow — 上下文窗口即将返回时
//
// 文件目录：
//
//	processor/
//	├── doc.go    # 包文档
//	└── base.go   # ContextEvent 处理器结果类型
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/
package processor
