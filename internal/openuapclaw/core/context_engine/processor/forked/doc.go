// Package forked 提供 forked 上下文处理器家族的共享地基。
//
// Python 在 processor/forked/ 下维护一套与旧链（processor/compressor/）并行的
// 处理器家族，是上游产品路径的默认 preset；本包对应其 base 层：
// ContextEvent 事件、ContextProcessor 基类（trigger/on 四协程点、状态持久化、
// compression usage 记账、offload 家族）。其余家族成员随 24.15 15b 批补齐。
//
// 文件目录：
//
//	forked/
//	├── doc.go           # 包文档
//	└── base.go         # ContextEvent + ContextProcessor 基类 + offload 家族
//	└── compressor/      # forked 压缩器家族（24.2/24.15 分批迁入）
//	    ├── session_memory_compressor.go   # 会话记忆物化压缩器（24.2）
//	    ├── support/                        # 计数/调试原语（24.2）
//	    └── recall/                         # 归档与召回（bm25/archive/retriever，24.2）
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/
package forked
