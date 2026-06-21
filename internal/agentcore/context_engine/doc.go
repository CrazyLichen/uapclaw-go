// Package context_engine 提供上下文引擎的核心抽象和数据结构。
//
// 上下文引擎负责管理 Agent 会话中的对话消息生命周期、
// 构建 LLM 推理所需的上下文窗口、以及消息压缩和卸载等处理。
// 它是 Session 和 LLM 之间的桥梁：Session 管理会话状态，
// ModelContext 管理 LLM 看到的"上下文视图"。
//
// 文件目录：
//
//	context_engine/
//	├── doc.go           # 包文档
//	├── base.go          # ModelContext 接口 + ContextStats + ContextWindow + ContextEngine 接口
//	│                   # NewContextWindow 构造函数 + StatMessages/StatTools/StatContextWindow 预留方法
//	│                   # ContextEngine 方法直接使用 *session.Session（循环依赖已通过 single_agent/interfaces 解决）
//	│                   # 消息类型使用 []llm_schema.BaseMessage（接口类型，非指针切片）
//	│                   # ⤵️ StatMessages/StatTools/StatContextWindow 实际逻辑待 5.31 回填
//	├── schema/
//	│   ├── doc.go              # Schema 子包文档
//	│   ├── config.go           # ContextEngineConfig 上下文引擎配置
//	│   ├── context_state.go    # 压缩状态模型（ContextCompressionState + 辅助类型 + CompressionStatus/Phase）
//	│   └── offload.go          # Offload 消息模型
//	├── processor/
//	│   ├── doc.go              # Processor 子包文档
//	│   ├── base.go             # ContextProcessor 接口 + ProcessorConfig 接口 + ContextEvent + BaseProcessor
//	│   ├── hooks.go            # 钩子默认实现 + ProcessorType + IsAPIRound
//	│   ├── state.go            # SaveState/LoadState 默认实现
//	│   ├── offload.go          # OffloadMessages 方法族 + offload 常量
//	│   ├── usage.go            # CompressionUsage 追踪方法族
//	│   └── round.go            # GroupCompletedAPIRounds 函数
//	└── token/
//	    ├── doc.go              # Token 子包文档
//	    └── base.go             # TokenCounter 接口定义
//
// 对应 Python 代码：openjiuwen/core/context_engine/
package context_engine
