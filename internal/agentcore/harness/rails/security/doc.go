// Package security 提供 Agent 安全护栏 Rail 实现。
//
// 包含两层安全机制：
//   - SafetyPromptRail（别名 SecurityRail）：在 LLM 调用前注入安全原则到 system prompt
//   - PermissionInterruptRail：拦截工具调用，通过分层策略评估权限
//
// BaseSecurityRail 为安全 Rail 抽象基类，定义了决策类型（Allow/Reject/Interrupt/Alert）
// 和统一的安全检查→决策应用流程（runAndApply → applySecurityDecision）。
//
// 文件目录：
//
//	security/
//	├── doc.go                   # 包文档
//	├── base_security_rail.go    # BaseSecurityRail + 决策类型 + apply 逻辑
//	├── prompt_security_rail.go  # SafetyPromptRail（别名 SecurityRail）
//	└── tool_security_rail.go    # PermissionInterruptRail
//
// 对应 Python 代码：openjiuwen/harness/rails/security/
package security
