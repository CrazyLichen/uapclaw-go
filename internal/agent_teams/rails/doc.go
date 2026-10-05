// Package rails 提供团队级 Rails 实现。
//
// 本包包含：
//   - FirstIterationGate：首次迭代信号门（channel 替代 Python asyncio.Event）
//   - TeamToolRail：团队协调工具注册（priority=90）
//   - TeamPolicyRail：团队策略提示注入（6 个静态 + 2 个动态 PromptSection，priority=12）
//   - TeamToolApprovalRail：teammate 工具调用审批（继承 BaseInterruptRail）
//   - TeamPlanModeRail：team.plan leader 提示词叠加（priority=84）
//
// Python: openjiuwen/agent_teams/rails/
//
// 文件目录：
//
//	rails/
//	├── doc.go                  # 包文档
//	├── first_iteration_gate.go # 首次迭代信号门
//	├── team_tool_rail.go       # TeamToolRail (priority=90)
//	├── team_policy_rail.go     # TeamPolicyRail (priority=12)
//	├── tool_approval_rail.go   # TeamToolApprovalRail (priority=90)
//	└── team_plan_mode_rail.go  # TeamPlanModeRail (priority=84)
//
// 对应 Python 代码：openjiuwen/agent_teams/rails/
package rails
