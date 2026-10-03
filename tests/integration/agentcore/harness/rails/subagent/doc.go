//go:build integration

// Package subagent 提供 SubagentRail / VerificationRail / VerificationContractRail 集成测试。
//
// 对齐 Python: tests/unit_tests/harness/test_subagent_rail.py
// + tests/unit_tests/harness/test_verification_rail.py
//
// 文件目录：
//
//	subagent/
//	├── doc.go                  # 包文档
//	└── subagent_rail_test.go   # 3 个 Rail 测试（SubagentRail + VerificationRail + VerificationContractRail）
//
// 对应 Python 代码：openjiuwen/harness/rails/subagent/
package subagent
