// Package subagent 提供子代理委派和验证约束的 Rail 实现。
//
// 包含三个 Rail：
//   - SubagentRail：注册 TaskTool/SessionTools，注入子代理描述 prompt section
//   - VerificationRail：验证代理工具白名单 + 每轮约束提醒 + 工作空间守卫
//   - VerificationContractRail：向父代理注入验证门控契约
//
// 文件目录：
//
//	subagent/
//	├── doc.go                        # 包文档
//	├── subagent_rail.go              # SubagentRail 子代理委派 Rail
//	├── verification_rail.go          # VerificationRail 验证代理约束 Rail
//	└── verification_contract_rail.go # VerificationContractRail 验证门控契约 Rail
//
// 对应 Python 代码：openjiuwen/harness/rails/subagent/
package subagent
