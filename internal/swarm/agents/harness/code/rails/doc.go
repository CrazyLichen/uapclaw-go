// Package rails 提供 code 模式的 harness rail 实现。
// 含 CodeTaskPlanningRail（在 agent-core 的 TaskPlanningRail 基础上改写 todo
// 工具文案并追加周期性 task_reminder 附件）与计划审批文案解析助手。
// 文件目录：
//
//	rails/
//	├── doc.go                      # 包文档
//	├── code_task_planning_rail.go  # code 模式 todo rail（提醒节奏 + 附件账本恢复）
//	├── confirm_message.go          # 确认中断文案构建（switch_mode 专用文案 + 参数预览）
//	└── plan_approval_message.go    # 计划审批文案解析（标题/剥离选项/抽正文/生成选项）
//
// 对应 Python 代码：jiuwenswarm/agents/harness/code/rails/
//   - code_task_planning_rail.py
//   - code_confirm_interrupt_rail.py（仅文案构建部分）
//   - code_plan_approval_interrupt_rail.py（仅文案解析部分）
package rails
