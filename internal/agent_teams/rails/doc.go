// Package rails 提供团队级 Rails 实现。
// 本包包含：
//   - FirstIterationGate：首次迭代信号门（channel 替代 Python asyncio.Event）
//   - TeamToolRail：团队协调工具注册（priority=90）
//   - TeamPolicyRail：团队策略提示注入（静态 PromptSection，priority=12）
//     两条车道：静态 section 进 system prompt 前缀；团队状态（身份/团队元数据/
//     名册）由 agent_teams/tracker 决策、本 rail 投进对话历史
//   - TeamToolApprovalRail：teammate 工具调用审批（继承 BaseInterruptRail）
//   - TeamPlanModeRail：team.plan leader 提示词叠加（priority=84）
//
// Python: openjiuwen/agent_teams/rails/
//
// 文件目录：
//
//	rails/
//	├── doc.go                  # 包文档
//	├── team_context.go      # TeamHandleKey 17 键 + InjectTeamHandles + 17 个 getter
//	├── first_iteration_gate.go # 首次迭代信号门
//	├── team_tool_rail.go       # TeamToolRail (priority=90)
//	├── team_policy_rail.go     # TeamPolicyRail (priority=12)
//	├── tool_approval_rail.go   # TeamToolApprovalRail (priority=90)
//	└── team_plan_mode_rail.go  # TeamPlanModeRail (priority=84)
//
// 对应 Python 代码：openjiuwen/agent_teams/rails/
package rails
