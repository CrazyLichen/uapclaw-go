// Package context 提供上下文引擎的具体上下文实例实现。
//
// 本包实现 ModelContext 接口的核心类 SessionModelContext，
// 以及其依赖的子组件：消息缓冲区、KV 缓存管理器、压缩状态记录器、
// 上下文工具方法和会话记忆管理器。
//
// 文件目录：
//
//	context/
//	├── doc.go                        # 包文档
//	├── session_model_context.go      # SessionModelContext 核心类
//	├── message_buffer.go             # ContextMessageBuffer + OffloadMessageBuffer
//	├── kv_cache_manager.go           # KVCacheManager
//	├── processor_state_recorder.go   # ProcessorStateRecorder + ProcessorStateInput
//	├── context_utils.go              # 补充工具方法 + 模型映射表
//	└── session_memory_manager.go     # SessionMemoryManager + UpdateAgent
//
// 对应 Python 代码：openjiuwen/core/context_engine/context/
package context
