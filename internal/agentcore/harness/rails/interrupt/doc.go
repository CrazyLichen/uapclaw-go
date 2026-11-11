// Package interrupt 提供中断-恢复（HITL）Rail 实现。
// 在 BeforeToolCall 钩子中拦截特定工具调用，暂停 ReAct 循环
// 等待用户输入（Human-in-the-loop），然后恢复执行。
// 是 Agent 人工审批、用户交互的核心机制。
// 三种决策类型：
//   - ApproveResult：放行工具执行（可修改参数）
//   - RejectResult：跳过工具执行（预设返回结果）
//   - InterruptResult：中断等待用户输入
//
// 文件目录：
//
//	interrupt/
//	├── doc.go              # 包文档
//	├── interrupt_base.go   # BaseInterruptRail + 决策类型
//	├── ask_user_rail.go    # AskUserRail + AskUserPayload/AskUserRequest
//	├── confirm_rail.go     # ConfirmInterruptRail + ConfirmPayload/ConfirmRequest
//	├── helpers.go          # ConvertInteractionsToAskUserQuestion + 6 helper 函数
//	├── edition.go          # 产品版本判定（零依赖版本探测，对齐 edition.py）
//	├── kinds.go             # 中断类型判定与交互过滤
//	├── options.go           # 审批选项构建与归一化
//	├── permission_option_values.go # 权限/确认审批选项统一词表
//	├── question_card.go     # 审批问题卡片渲染
//	├── question_options.go  # 审批选项选择（plan approval）
//	└── reviewer_ui_metadata.go # 审查器 UI 元数据投影
//
// 对应 Python 代码：openjiuwen/harness/rails/interrupt/ + jiuwenswarm/agents/harness/common/rails/interrupt/interrupt_helpers.py
package interrupt
