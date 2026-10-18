//go:build integration

// Package subagent 提供 SubagentRail / VerificationRail / VerificationContractRail / SessionToolkit 集成测试。
//
// 对齐 Python: tests/unit_tests/harness/test_subagent_rail.py
// + tests/unit_tests/harness/test_verification_rail.py
// + tests/system_tests/harness/test_deep_agent_subagent.py（cancel 部分）
//
// 文件目录：
//
//	subagent/
//	├── doc.go                            # 包文档
//	├── subagent_rail_test.go             # SubagentRail + VerificationRail + VerificationContractRail 基础测试
//	├── subagent_e2e_test.go              # SubagentRail E2E 测试
//	├── verification_rail_test.go         # VerificationRail + VerificationContractRail 完整集成测试
//	└── session_toolkit_cancel_test.go    # SessionToolkit cancel 场景 + SessionsListTool 测试
//
// 对应 Python 代码：openjiuwen/harness/rails/subagent/ + harness/tools/subagent/
package subagent
