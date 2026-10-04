//go:build integration

// Package harness 提供 Harness 模块集成测试的目录结构文档。
//
// 实际测试代码以 harness_test 包（外部测试包）编写。
// 对齐 Python: tests/system_tests/harness/test_deep_agent_e2e.py
//
// 文件目录：
//
//	harness/
//	├── doc.go               # 包文档
//	├── code_agent_test.go   # CodeAgent E2E 集成测试
//	├── deep_agent_test.go   # DeepAgent E2E 测试
//	├── force_finish_test.go # ForceFinish 机制集成测试
//	├── outer_loop_test.go   # 外层循环（TaskLoop）集成测试
//	├── steer_test.go        # Steering 内循环集成测试
//	└── rails/               # Rail 子系统测试
package harness
