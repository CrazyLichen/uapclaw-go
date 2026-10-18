//go:build integration

// Package controller 提供事件驱动任务编排控制器端到端集成测试。
//
// 对齐 Python: tests/system_tests/agent/controller_agent/test_deepsearch.py
// 测试 Controller 核心管道：TaskManager + EventHandler + TaskScheduler + EventQueue
//
// 测试范围：
//   - 三阶段任务链式执行（DataCollect → DataAnalysis → ReportGenerate）
//   - EventHandler 回调触发（handle_input + handle_task_completion）
//   - 优先级驱动的阶段激活
//   - 任务生命周期（Submitted → Running → Completed）
//   - EventQueue 异步事件分发
//   - TaskExecutor 注册与执行
//
// 文件目录：
//
//	controller/
//	├── doc.go                      # 包文档
//	└── deepsearch_e2e_test.go      # DeepSearch 三阶段管道 E2E 测试
//
// 对应 Python 代码：tests/system_tests/agent/controller_agent/test_deepsearch.py
package controller
