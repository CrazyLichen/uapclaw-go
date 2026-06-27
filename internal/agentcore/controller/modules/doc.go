// Package modules 提供 Controller 领域的核心逻辑组件。
//
// 本包包含 Controller 事件驱动系统的核心组件：
//   - TaskManager (6.20)：任务 CRUD、状态管理、优先级/层级索引
//   - EventQueue (6.20)：基于 MessageQueueInMemory 的事件发布订阅
//   - TaskScheduler (6.21)：后台调度循环、并发执行、暂停/取消
//   - EventHandler (6.21)：事件处理器接口 + 默认基类
//   - IntentToolkits (6.21)：意图识别工具集，8 个 OpenAI Tool Schema
//   - IntentRecognizer (6.21)：意图识别器骨架（⤵️ 6.23 回填 LLM 调用）
//   - EventHandlerWithIntentRecognition (6.21)：基于意图识别的事件处理器
//
// 组件间依赖关系：
//
//	EventQueue ← EventHandler（订阅时绑定 handler 回调）
//	TaskScheduler ← TaskManager + EventQueue + TaskExecutor
//	TaskManager.SetOnTaskSubmitted → TaskScheduler.NotifyTaskSubmitted
//	IntentRecognizer ← ModelProvider（⤵️ 6.23 回填）
//	EventHandlerWithIntentRecognition ← IntentRecognizer + EventHandlerBase
//
// 文件目录：
//
//	modules/
//	├── doc.go              # 包文档
//	├── event_handler.go    # EventHandler 接口 + EventHandlerBase + EventHandlerInput
//	├── intent_recognizer.go # IntentRecognizer 骨架 + EventHandlerWithIntentRecognition
//	├── intent_toolkits.go  # IntentToolkits 意图工具集
//	├── task_manager.go     # TaskManager + TaskFilter + TaskManagerState
//	├── event_queue.go      # EventQueue
//	├── task_executor.go    # TaskExecutor 接口 + TaskExecutorDependencies + TaskExecutorRegistry
//	└── task_scheduler.go   # TaskScheduler
//
// 对应 Python 代码：openjiuwen/core/controller/modules/
package modules
