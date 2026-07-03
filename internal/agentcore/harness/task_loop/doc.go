// Package task_loop 提供 DeepAgent 外层任务循环的运行时组件。
//
// 包含循环协调器（LoopCoordinator）、停止条件评估器（StopConditionEvaluator）
// 及其内置实现，用于控制 DeepAgent 多轮任务循环的生命周期。
//
// LoopCoordinator 追踪迭代次数、token 用量、耗时和中止标记，
// 每轮迭代前通过评估器链（OR 语义）决定是否继续循环。
//
// 文件目录：
//
//	task_loop/
//	├── doc.go                   # 包文档
//	├── stop_condition.go        # StopConditionEvaluator 接口 + 5 个评估器实现
//	├── stop_condition_test.go   # 评估器测试
//	├── loop_coordinator.go      # LoopCoordinator + LoopCoordinatorState
//	└── loop_coordinator_test.go # LoopCoordinator 测试
//
// 对应 Python 代码：openjiuwen/harness/task_loop/ + openjiuwen/harness/schema/stop_condition.py
package task_loop
