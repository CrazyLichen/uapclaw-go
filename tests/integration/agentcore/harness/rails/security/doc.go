//go:build integration

// Package security 提供 Security/Guardrail 模块的集成测试。
//
// 测试覆盖：
//   - BaseSecurityRail 全生命周期：SecurityReject/Allow/Interrupt/MultiEvent/Priority
//   - SafetyPromptRail 自动注册、安全节注入/移除
//   - PermissionInterruptRail ALLOW/DENY/ASK 决策流
//   - SecurityDecision 全路径决策流：链式决策、Interrupt→Resume 闭环、混合结果
//
// 文件目录：
//
//	security/
//	├── doc.go                        # 包文档
//	├── base_security_rail_test.go    # BaseSecurityRail 集成测试
//	├── safety_prompt_test.go         # SafetyPromptRail 集成测试
//	├── prompt_security_rail_test.go  # PromptSecurityRail 集成测试
//	├── tool_permission_rail_test.go  # ToolPermissionRail 集成测试
//	├── guardrail_content_filter_test.go # Guardrail 内容过滤集成测试
//	├── permission_test.go            # PermissionInterruptRail 中断恢复测试
//	└── decision_flow_test.go         # SecurityDecision 全路径决策流测试
//
// 对应 Python 代码：openjiuwen/harness/rails/security/ + tests/system_tests/rail/test_base_security_rail_integration.py
package security
