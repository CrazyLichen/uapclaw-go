// Package modules 提供 Controller 领域的核心逻辑组件。
//
// 本包包含 Controller 事件驱动系统的四大核心组件：
//   - TaskManager (6.20)：任务 CRUD、状态管理、优先级/层级索引
//   - EventQueue (6.20)：基于 MessageQueueInMemory 的事件发布订阅
//   - TaskScheduler (6.21)：后台调度循环、并发执行、暂停/取消
//   - EventHandler (6.21)：事件处理器接口 + 默认基类
//
// 组件间依赖关系：
//
//	EventQueue ← EventHandler（订阅时绑定 handler 回调）
//	TaskScheduler ← TaskManager + EventQueue + TaskExecutor
//	TaskManager.SetOnTaskSubmitted → TaskScheduler.NotifyTaskSubmitted
//
// 文件目录：
//
//	modules/
//	├── doc.go              # 包文档
//	├── event_handler.go    # EventHandler 接口 + EventHandlerBase + EventHandlerInput
//	├── task_manager.go     # TaskManager + TaskFilter + TaskManagerState
//	├── event_queue.go      # EventQueue
//	├── task_executor.go    # TaskExecutor 接口 + TaskExecutorDependencies + TaskExecutorRegistry
//	└── task_scheduler.go   # TaskScheduler
//
// 对应 Python 代码：openjiuwen/core/controller/modules/
package modules
