// Package task_loop 提供 DeepAgent 外层任务循环的运行时组件。
//
// 包含任务循环控制器（TaskLoopController）、双队列缓冲（LoopQueues）、
// 循环协调器（LoopCoordinator）、停止条件评估器（StopConditionEvaluator）
// 及其内置实现，用于控制 DeepAgent 多轮任务循环的生命周期。
//
// TaskLoopController 嵌入 Controller 基类，扩展轮次管理（提交/等待/完成）
// 和 follow-up 队列操作，是 DeepAgent 外层循环的"方向盘"。
// LoopCoordinator 是"刹车"——追踪迭代/token/耗时/中止，通过评估器链决定是否继续。
// LoopQueues 提供双队列缓冲（steering + follow_up），桥接 EventHandler 与 Executor/Loop。
//
// 文件目录：
//
//	task_loop/
//	├── doc.go                   # 包文档
//	├── controller.go            # TaskLoopController（嵌入 Controller + 轮次管理扩展）
//	├── loop_queues.go           # LoopQueues 双队列缓冲
//	├── stop_condition.go        # StopConditionEvaluator 接口 + 5 个评估器实现
//	├── loop_coordinator.go      # LoopCoordinator + LoopCoordinatorState
//	├── loop_coordinator_test.go # LoopCoordinator 测试
//	├── stop_condition_test.go   # 评估器测试
//	├── loop_queues_test.go     # LoopQueues 测试
//	└── controller_test.go      # TaskLoopController 测试
//
// 对应 Python 代码：openjiuwen/harness/task_loop/ + openjiuwen/harness/schema/stop_condition.py
package task_loop
