package workflow

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowOutput 工作流执行结果。
//
// 对应 Python: openjiuwen/core/workflow/base.py WorkflowOutput(BaseModel)
type WorkflowOutput struct {
	// Result 输出数据
	Result any
	// State 执行状态
	State WorkflowExecutionState
}

// ──────────────────────────── 枚 ────────────────────────────

// WorkflowExecutionState 工作流执行状态。
//
// 对应 Python: openjiuwen/core/workflow/base.py WorkflowExecutionState(str, Enum)
type WorkflowExecutionState string

// ──────────────────────────── 常量 ────────────────────────────
const (
	// WorkflowExecutionStateCompleted 执行完成
	WorkflowExecutionStateCompleted WorkflowExecutionState = "COMPLETED"
	// WorkflowExecutionStateInputRequired 需要用户输入（中断）
	WorkflowExecutionStateInputRequired WorkflowExecutionState = "INPUT_REQUIRED"
	// WorkflowExecutionStateError 执行出错
	WorkflowExecutionStateError WorkflowExecutionState = "ERROR"
)
