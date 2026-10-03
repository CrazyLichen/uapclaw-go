//go:build integration

// Package harness 提供 Harness 模块集成测试的目录结构文档。
//
// 实际测试代码以 harness_test 包（外部测试包）编写，
// 位于 deep_agent_test.go 中。
// 对齐 Python: tests/system_tests/harness/test_deep_agent_e2e.py
//
// 文件目录：
//
//	harness/
//	├── doc.go               # 包文档
//	├── deep_agent_test.go   # DeepAgent E2E 测试
//	└── rails/               # Rail 子系统测试
package harness
