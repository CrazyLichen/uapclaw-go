//go:build integration

// Package context_engineer 提供 ContextAssembleRail / ContextProcessorRail 集成测试。
//
// 对齐 Python: tests/unit_tests/harness/test_context_assemble_rail.py
// + tests/unit_tests/harness/test_context_processor_rail.py
// + tests/system_tests/harness/test_context_processor_rail_system.py
//
// 文件目录：
//
//	context_engineer/
//	├── doc.go                              # 包文档
//	├── context_engineer_rail_test.go       # 2 个 Rail 基础测试（8 测试）
//	└── context_processor_callback_test.go  # 回调生命周期深度测试（15 测试）
//
// 对应 Python 代码：openjiuwen/harness/rails/context_engineer/
package context_engineer
