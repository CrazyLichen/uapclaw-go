// Package workflowbase 工作流域基础类型叶包（零依赖）。
//
// 承载 WorkflowOutput / WorkflowExecutionState 及其枚举常量。
// 抽取动机（8.26 收窄回填，用户拍板 2026-11-25）：single_agent/schema 的
// WorkflowInterruptEntry.WorkflowExecutionState 需要强类型承载，而 schema 直接
// import workflow 根包会成环（workflow(root) → single_agent/schema 已存在）——
// 叶包化后 schema → workflowbase 合法，全链断言消除。
// 规则 9 注记：Python 归属 openjiuwen/core/workflow/base.py（WorkflowOutput
// / WorkflowExecutionState），叶包化仅为破环物理搬迁，语义零变化。
//
// 文件目录：
//
//	workflowbase/
//	└── workflowbase.go   # WorkflowOutput + WorkflowExecutionState 枚举
//
// 对应 Python 代码：openjiuwen/core/workflow/base.py
package workflowbase

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowOutput 工作流执行结果。
// Python: openjiuwen/core/workflow/base.py WorkflowOutput(BaseModel)
type WorkflowOutput struct {
	// Result 输出数据
	Result any
	// State 执行状态
	State WorkflowExecutionState
}

// ──────────────────────────── 枚举 ────────────────────────────

// WorkflowExecutionState 工作流执行状态。
// Python: base.py WorkflowExecutionState(str, Enum)
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
