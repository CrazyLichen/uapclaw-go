package state

import "github.com/uapclaw/uapclaw-go/internal/common/logger"

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowCommitState 工作流可提交状态。
//
// 在 WorkflowStateCollection 基础上增加 commit/rollback/IO 操作和节点状态创建能力。
// 实现 State 接口。
//
// 注意：由于 Go 接口方法签名冲突（CommitState.Commit(nodeID...string) 与 WorkflowCommitState.Commit() 无参），
// WorkflowCommitState 不直接实现 CommitState 接口，而是提供 CommitCommitState/RollbackNode 方法。
// 这与 Python 设计一致：Python 中 CommitState 继承 StateCollection 继承 State。
//
// 对应 Python: openjiuwen/core/session/state/workflow_state.py (CommitState)
type WorkflowCommitState struct {
	// WorkflowStateCollection 嵌入基础四区状态
	WorkflowStateCollection
	// workflowOnly 是否仅工作流模式（无共享 globalState 时为 true）
	workflowOnly bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowCommitState 创建工作流可提交状态实例。
func NewWorkflowCommitState(ioState, globalState, compState, workflowState CommitState, traceState map[string]any, parentID, nodeID string, workflowOnly bool) *WorkflowCommitState {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_workflow_commit_state").
		Str("parent_id", parentID).
		Str("node_id", nodeID).
		Bool("workflow_only", workflowOnly).
		Msg("创建工作流可提交状态")
	return &WorkflowCommitState{
		WorkflowStateCollection: *NewWorkflowStateCollection(ioState, globalState, compState, workflowState, traceState, parentID, nodeID),
		workflowOnly:            workflowOnly,
	}
}

// ──────────────────────────── WorkflowCommitState 方法 ────────────────────────────

// GetWorkflowState 从工作流状态获取值。
func (s *WorkflowCommitState) GetWorkflowState(key StateKey) any {
	if s.workflowState == nil || key.IsZero() {
		return nil
	}
	return s.workflowState.Get(key)
}

// UpdateAndCommitWorkflowState 立即更新并提交工作流状态。
func (s *WorkflowCommitState) UpdateAndCommitWorkflowState(data map[string]any) {
	s.workflowState.UpdateByID(DefaultWorkflowID, data)
	s.workflowState.Commit()
}

// SetOutputs 向 io_state 写入当前节点的输出。
// data 被包裹在 {nodeID: data} 中。
func (s *WorkflowCommitState) SetOutputs(data map[string]any) {
	if s.ioState == nil || data == nil {
		return
	}
	s.ioState.UpdateByID(s.nodeID, map[string]any{s.nodeID: data})
}

// GetInputs 从 io_state 查询父节点的输出（即当前节点输入）。
// schema 为零值时返回当前节点全部 IO 数据；否则按 parentID 前缀查找。
func (s *WorkflowCommitState) GetInputs(schema StateKey) any {
	if s.ioState == nil {
		return nil
	}
	if schema.IsZero() {
		return s.ioState.Get(StringKey(s.nodeID))
	}
	return s.ioState.GetByPrefix(schema, s.parentID)
}

// GetOutputs 从 io_state 查询指定节点的输出。
// nodeID 为空时使用当前 nodeID。
func (s *WorkflowCommitState) GetOutputs(nodeID ...string) any {
	if s.ioState == nil {
		return nil
	}
	effectiveNodeID := s.nodeID
	if len(nodeID) > 0 && nodeID[0] != "" {
		effectiveNodeID = nodeID[0]
	}
	return s.ioState.GetByPrefix(StringKey(effectiveNodeID), s.parentID)
}

// GetInputsByTransformer 通过转换函数获取输入。
func (s *WorkflowCommitState) GetInputsByTransformer(transformer Transformer) any {
	if s.ioState == nil {
		return map[string]any{}
	}
	return s.ioState.GetByTransformer(transformer)
}

// CommitUserInputs 同时写入 io_state 和 global_state 并立即提交。
// 默认节点的 io_state data 不包裹在 {nodeID: data} 中。
func (s *WorkflowCommitState) CommitUserInputs(inputs map[string]any) {
	if s.ioState == nil || inputs == nil {
		return
	}
	if s.nodeID != DefaultNodeID {
		s.ioState.UpdateByID(s.nodeID, map[string]any{s.nodeID: inputs})
	} else {
		s.ioState.UpdateByID(s.nodeID, inputs)
	}
	s.globalState.UpdateByID(s.nodeID, inputs)
	s.Commit()
}

// Commit 提交全部四个子状态。
func (s *WorkflowCommitState) Commit() {
	s.ioState.Commit()
	s.compState.Commit()
	s.globalState.Commit()
	s.workflowState.Commit()
}

// Rollback 回滚全部四个子状态的当前节点更新。
func (s *WorkflowCommitState) Rollback() {
	s.compState.Rollback(s.nodeID)
	s.ioState.Rollback(s.nodeID)
	s.globalState.Rollback(s.nodeID)
	s.workflowState.Rollback(s.nodeID)
}

// CreateNodeState 创建节点专属状态视图。
// 共享底层四个子状态对象，切换 nodeID/parentID。
func (s *WorkflowCommitState) CreateNodeState(nodeID, parentID string) *WorkflowCommitState {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "create_node_state").
		Str("node_id", nodeID).
		Str("parent_id", parentID).
		Msg("创建节点专属状态视图")
	return NewWorkflowCommitState(
		s.ioState,
		s.globalState,
		s.compState,
		s.workflowState,
		s.traceState,
		parentID,
		nodeID,
		true, // create_node_state 总是 workflowOnly=true
	)
}

// ──────────────────────────── CommitState 兼容方法 ────────────────────────────

// UpdateByID 委托给 compState。
func (s *WorkflowCommitState) UpdateByID(nodeID string, data map[string]any) {
	s.compState.UpdateByID(nodeID, data)
}

// CommitCommitState 提交指定节点的暂存更新。
// 注意：此方法与 WorkflowCommitState.Commit() 不同，后者提交全部四个子状态。
func (s *WorkflowCommitState) CommitCommitState(nodeID ...string) {
	s.ioState.Commit(nodeID...)
	s.compState.Commit(nodeID...)
	s.globalState.Commit(nodeID...)
	s.workflowState.Commit(nodeID...)
}

// RollbackNode 回滚指定节点的暂存更新。
func (s *WorkflowCommitState) RollbackNode(nodeID string) {
	s.compState.Rollback(nodeID)
	s.ioState.Rollback(nodeID)
	s.globalState.Rollback(nodeID)
	s.workflowState.Rollback(nodeID)
}

// GetUpdates 获取所有暂存更新。
// workflowOnly 控制是否包含 global_state_updates。
// 返回格式：{key: {node_id: [update_dict_1, ...], ...}, ...}
func (s *WorkflowCommitState) GetUpdates() map[string]any {
	result := map[string]any{
		IOStateUpdatesKey:       deepCopyUpdates(s.ioState.GetUpdates()),
		CompStateUpdatesKey:     deepCopyUpdates(s.compState.GetUpdates()),
		WorkflowStateUpdatesKey: deepCopyUpdates(s.workflowState.GetUpdates()),
	}
	if s.workflowOnly {
		result[GlobalStateUpdatesKey] = deepCopyUpdates(s.globalState.GetUpdates())
	} else {
		result[GlobalStateUpdatesKey] = nil
	}
	return result
}

// SetUpdates 设置暂存更新。
func (s *WorkflowCommitState) SetUpdates(updates map[string]any) {
	if updates == nil {
		return
	}
	if gs, ok := updates[GlobalStateUpdatesKey]; ok && gs != nil && s.workflowOnly {
		if m, ok := gs.(map[string][]map[string]any); ok {
			s.globalState.SetUpdates(m)
		}
	}
	if io, ok := updates[IOStateUpdatesKey]; ok && io != nil {
		if m, ok := io.(map[string][]map[string]any); ok {
			s.ioState.SetUpdates(m)
		}
	}
	if comp, ok := updates[CompStateUpdatesKey]; ok && comp != nil {
		if m, ok := comp.(map[string][]map[string]any); ok {
			s.compState.SetUpdates(m)
		}
	}
	if wf, ok := updates[WorkflowStateUpdatesKey]; ok && wf != nil {
		if m, ok := wf.(map[string][]map[string]any); ok {
			s.workflowState.SetUpdates(m)
		}
	}
}

// WorkflowOnly 返回是否仅工作流模式。
func (s *WorkflowCommitState) WorkflowOnly() bool {
	return s.workflowOnly
}

// ──────────────────────────── RecoverableState 覆写 ────────────────────────────

// GetState 导出状态快照（覆写 WorkflowStateCollection.GetState）。
// workflowOnly 控制是否包含 global_state。
func (s *WorkflowCommitState) GetState() map[string]any {
	result := map[string]any{
		IOStateKey:       s.ioState.GetState(),
		CompStateKey:     s.compState.GetState(),
		WorkflowStateKey: s.workflowState.GetState(),
	}
	if s.workflowOnly {
		result[GlobalStateKey] = s.globalState.GetState()
	} else {
		result[GlobalStateKey] = nil
	}
	return result
}

// SetState 从快照恢复状态（覆写 WorkflowStateCollection.SetState）。
func (s *WorkflowCommitState) SetState(st map[string]any) {
	if st == nil {
		return
	}
	if gs, ok := st[GlobalStateKey]; ok && gs != nil {
		if m, ok := gs.(map[string]any); ok {
			s.globalState.SetState(m)
		}
	}
	if io, ok := st[IOStateKey]; ok && io != nil {
		if m, ok := io.(map[string]any); ok {
			s.ioState.SetState(m)
		}
	}
	if comp, ok := st[CompStateKey]; ok && comp != nil {
		if m, ok := comp.(map[string]any); ok {
			s.compState.SetState(m)
		}
	}
	if wf, ok := st[WorkflowStateKey]; ok && wf != nil {
		if m, ok := wf.(map[string]any); ok {
			s.workflowState.SetState(m)
		}
	}
}
