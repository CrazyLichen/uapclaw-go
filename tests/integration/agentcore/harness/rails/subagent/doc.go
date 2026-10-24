//go:build integration

// Package subagent 提供 SubagentRail / VerificationRail / VerificationContractRail / SessionToolkit 集成测试。
//
// 对齐 Python: tests/unit_tests/harness/test_subagent_rail.py
// + tests/unit_tests/harness/test_verification_rail.py
// + tests/system_tests/harness/test_deep_agent_subagent.py（cancel + spawn 完成部分）
//
// 文件目录：
//
//	subagent/
//	├── doc.go                            # 包文档
//	├── subagent_rail_test.go             # SubagentRail + VerificationRail + VerificationContractRail 基础测试
//	├── subagent_e2e_test.go              # SubagentRail E2E 测试
//	├── verification_rail_test.go         # VerificationRail + VerificationContractRail 完整集成测试
//	├── session_toolkit_cancel_test.go    # SessionToolkit cancel 场景 + SessionsListTool 测试
//	├── session_cancel_e2e_test.go        # 异步子代理端到端取消 6 场景 + fakeSubAgent/steeringObserver 替身
//	                                     # （对齐 Python 场景 1-6；替身与测试同文件以满足
//	                                     #  integration-audit 每文件 suite.Run 检查）
//	└── session_spawn_e2e_test.go         # 异步子代理端到端完成 3 场景（auto-invoke 总结 /
//	                                     #  q2 不阻塞且 steering 不可见 / 多 spawn 去重调度，
//	                                     #  对齐 Python TestDeepAgentSessionRail 场景 1/2/4）
//
// 对应 Python 代码：openjiuwen/harness/rails/subagent/ + harness/tools/subagent/
package subagent
