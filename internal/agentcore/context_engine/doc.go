// Package context_engine 提供上下文引擎的核心抽象和数据结构。
//
// 上下文引擎负责管理 Agent 会话中的对话消息生命周期、
// 构建 LLM 推理所需的上下文窗口、以及消息压缩和卸载等处理。
// 它是 Session 和 LLM 之间的桥梁：Session 管理会话状态，
// ModelContext 管理 LLM 看到的"上下文视图"。
//
// 核心接口和数据结构定义在 interface 子包中，外部包应直接导入
// context_engine/interface 包并使用 iface.Xxx 形式引用类型。
// 本包提供处理器工厂注册表（RegisterProcessorFactory/GetProcessorFactory/ListProcessorFactories）、
// ContextEngine 门面（NewContextEngine）和预留函数 StatContextWindow。
//
// 文件目录：
//
//	context_engine/
//	├── doc.go           # 包文档
//	├── base.go          # StatContextWindow 预留函数
//	├── engine.go        # ContextEngine 门面实现（上下文池管理、处理器创建、会话状态持久化）
//	├── registry.go      # 处理器工厂注册表（Register/Get/List）
//	├── context/
//	│   ├── doc.go                     # context 子包文档
//	│   ├── session_model_context.go   # SessionModelContext 会话模型上下文
//	│   ├── session_memory_manager.go  # SessionMemoryManager 会话内存管理
//	│   ├── message_buffer.go          # MessageBuffer 消息缓冲
//	│   ├── kv_cache_manager.go        # KvCacheManager KV 缓存管理
//	│   ├── processor_state_recorder.go # ProcessorStateRecorder 处理器状态记录
//	│   └── context_utils.go           # 上下文工具函数
//	├── interface/
//	│   ├── doc.go              # Interface 子包文档
//	│   ├── types.go            # ModelContext/ContextEngine 接口 + ContextWindow/ContextStats + Option 类型 + ProcessorSpec
//	│   ├── processor.go        # ContextProcessor/ProcessorConfig 接口 + ContextEvent/ProcessorOption + Option
//	│   └── registry.go         # ProcessorFactory 工厂函数类型
//	├── schema/
//	│   ├── doc.go              # Schema 子包文档
//	│   ├── config.go           # ContextEngineConfig 上下文引擎配置
//	│   ├── context_state.go    # 压缩状态模型（ContextCompressionState + 辅助类型 + CompressionStatus/Phase）
//	│   └── offload.go          # Offload 消息模型
//	├── processor/
//	│   ├── doc.go              # Processor 子包文档
//	│   ├── base.go             # BaseProcessor 结构体 + 构造函数
//	│   ├── hooks.go            # 钩子默认实现 + ProcessorType + IsAPIRound
//	│   ├── state.go            # SaveState/LoadState 默认实现
//	│   ├── offload.go          # OffloadMessages 方法族 + offload 常量
//	│   ├── usage.go            # CompressionUsage 追踪方法族
//	│   ├── round.go            # GroupCompletedAPIRounds 函数
//	│   ├── replace.go          # Replacement 结构体 + ReplaceMessages 通用替换函数
//	│   ├── util.go             # 包级共享函数
//	│   ├── compressor/         # 压缩处理器子包
//	│   │   ├── doc.go                          # 子包文档
//	│   │   ├── dialogue_compressor.go          # DialogueCompressor 对话压缩器
//	│   │   ├── current_round_compressor.go     # CurrentRoundCompressor 当轮增量压缩器
//	│   │   ├── round_level_compressor.go       # RoundLevelCompressor 轮级渐进式压缩器
//	│   │   ├── micro_compact_processor.go      # MicroCompactProcessor 微压缩处理器
//	│   │   └── full_compact_processor.go       # FullCompactProcessor 全量压缩处理器
//	│   └── offloader/      # 卸载处理器子包
//	│       ├── doc.go                          # 子包文档
//	│       ├── message_offloader.go            # MessageOffloader 消息卸载器
//	│       ├── message_summary_offloader.go    # MessageSummaryOffloader 摘要卸载器
//	│       └── tool_result_budget_processor.go # ToolResultBudgetProcessor 工具结果预算处理器
//	└── token/
//	    ├── doc.go              # Token 子包文档
//	    ├── base.go             # TokenCounter 接口定义
//	    └── tiktoken_counter.go # TiktokenCounter 实现
//
// 对应 Python 代码：openjiuwen/core/context_engine/
package context_engine
