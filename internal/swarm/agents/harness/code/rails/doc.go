// Package rails 提供 code 模式的 harness rail 实现。
//
// 目前仅含 CodeTaskPlanningRail：在 agent-core 的 TaskPlanningRail 基础上
// 改写 todo 工具文案（CC 对齐）并追加周期性的 task_reminder 附件。
//
// 文件目录：
//
//	rails/
//	├── doc.go                      # 包文档
//	└── code_task_planning_rail.go  # code 模式 todo rail（提醒节奏 + 附件账本恢复）
//
// 对应 Python 代码：jiuwenswarm/agents/harness/code/rails/code_task_planning_rail.py
package rails
