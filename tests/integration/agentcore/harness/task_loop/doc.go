//go:build integration

// Package task_loop 提供 TaskLoop 模块的集成测试。
//
// 覆盖 LoopCoordinator 状态机、LoopEvent schema、LoopQueues 优先级队列。
//
// 对应 Python 代码：
//   - tests/unit_tests/harness/test_loop_coordinator.py（协调器）
//   - tests/unit_tests/harness/test_loop_event_schema.py（事件 schema）
//   - tests/unit_tests/harness/test_loop_queues.py（优先级队列）
package task_loop
