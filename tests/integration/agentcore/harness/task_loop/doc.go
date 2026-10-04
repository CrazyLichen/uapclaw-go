//go:build integration

// Package task_loop 提供 TaskLoop 模块的集成测试。
//
// 覆盖 LoopCoordinator 状态机、LoopEvent schema、LoopQueues 优先级队列。
//
// 对应 Python: tests/unit_tests/harness/test_loop_coordinator.py,
// tests/unit_tests/harness/test_loop_event_schema.py,
// tests/unit_tests/harness/test_loop_queues.py
package task_loop
