// Package rails 提供 code 模式的 harness rail 实现。
//
// 目前含 CodeTaskPlanningRail（在 agent-core 的 TaskPlanningRail 基础上改写
// todo 工具文案并追加周期性 task_reminder 附件）。
//
// # 文案助手已迁出
//
// 9.90e 收尾时，本包的两个纯字符串文案文件迁到
// `agentcore/harness/rails/planapproval`：
//
//   - confirm_message.go       → planapproval/confirm_message.go
//   - plan_approval_message.go → planapproval/message.go
//
// 原因是 `agentcore/harness/rails/interrupt` 渲染审批卡片时也要用这些函数，
// 若它们留在本包就形成 agentcore → swarm 的层级倒置。迁出后 interrupt 与
// 本包都依赖 planapproval，倒置消除。
//
// 文件目录：
//
//	rails/
//	├── doc.go                      # 包文档
//	└── code_task_planning_rail.go  # code 模式 todo rail（提醒节奏 + 附件账本恢复）
//
// 对应 Python 代码：jiuwenswarm/agents/harness/code/rails/
//   - code_task_planning_rail.py
//
// 文案助手对应的 Python 部分（code_confirm_interrupt_rail.py 与
// code_plan_approval_interrupt_rail.py）见 planapproval 包。
package rails
