//go:build integration

// Package security 提供 Security/Guardrail 模块的集成测试。
//
// 测试覆盖：
//   - BaseSecurityRail 全生命周期：SecurityReject/Allow/Interrupt/MultiEvent/Priority
//   - SafetyPromptRail 自动注册、安全节注入/移除
//   - PermissionInterruptRail ALLOW/DENY/ASK 决策流
//
// 文件目录：
//
//	security/
//	├── doc.go                      # 包文档
//	├── base_security_rail_test.go  # BaseSecurityRail 集成测试
//	├── safety_prompt_test.go       # SafetyPromptRail 集成测试
//	└── permission_test.go          # PermissionInterruptRail 集成测试
//
// 对应 Python 代码：openjiuwen/harness/rails/security/ + tests/system_tests/rail/test_base_security_rail_integration.py
package security
