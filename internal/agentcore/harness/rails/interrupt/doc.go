// Package interrupt 提供中断-恢复（HITL）Rail 实现。
//
// 在 BeforeToolCall 钩子中拦截特定工具调用，暂停 ReAct 循环
// 等待用户输入（Human-in-the-loop），然后恢复执行。
// 是 Agent 人工审批、用户交互的核心机制。
//
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
//	└── confirm_rail.go     # ConfirmInterruptRail + ConfirmPayload/ConfirmRequest
//
// 对应 Python 代码：openjiuwen/harness/rails/interrupt/
package interrupt
