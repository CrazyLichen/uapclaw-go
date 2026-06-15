package state

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryWorkflowState 创建基于内存的工作流状态实例。
//
// globalState 可从外部传入（与 AgentSession 共享），未提供则创建新的 InMemoryCommitState。
// workflowOnly 取决于 globalState 是否为 nil：
//   - 传入 globalState → workflowOnly=false（global_state 由外部管理）
//   - 未传 globalState → workflowOnly=true（所有状态独立）
//
// 对应 Python: openjiuwen/core/session/state/workflow_state.py (InMemoryState)
func NewInMemoryWorkflowState(globalState ...CommitStateLike) *WorkflowCommitState {
	var gs CommitStateLike
	if len(globalState) > 0 && globalState[0] != nil {
		gs = globalState[0]
	} else {
		gs = NewInMemoryCommitState()
	}
	workflowOnly := len(globalState) == 0 || globalState[0] == nil
	return NewWorkflowCommitState(
		NewInMemoryCommitState(), // ioState
		gs,                       // globalState
		NewInMemoryCommitState(), // compState
		NewInMemoryCommitState(), // workflowState
		make(map[string]any),     // traceState
		"",                       // parentID
		DefaultNodeID,            // nodeID
		workflowOnly,
	)
}
