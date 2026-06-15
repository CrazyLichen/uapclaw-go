package state

import "github.com/uapclaw/uapclaw-go/internal/common/logger"

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowCommitState 工作流可提交状态。
//
// 在 WorkflowStateCollection 基础上增加 commit/rollback/IO 操作和节点状态创建能力。
// 实现 SessionState 接口。不实现 CommitStateLike 接口（与 Python 一致，
// CommitState 继承 StateCollection 继承 State，不是 CommitStateLike 的子类），
// GetUpdates/SetUpdates 返回聚合视图 map[string]any 而非 map[string][]map[string]any。
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
func NewWorkflowCommitState(ioState, globalState, compState, workflowState CommitStateLike, traceState map[string]any, parentID, nodeID string, workflowOnly bool) *WorkflowCommitState {
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

// ──────────────────────────── WorkflowCommitState 特有方法 ────────────────────────────

// GetWorkflowState 从工作流状态获取值。
func (s *WorkflowCommitState) GetWorkflowState(key StateKey) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.workflowState == nil || key.IsZero() {
		return nil
	}
	return s.workflowState.Get(key)
}

// UpdateAndCommitWorkflowState 立即更新并提交工作流状态。
func (s *WorkflowCommitState) UpdateAndCommitWorkflowState(data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.workflowState.UpdateByID(DefaultWorkflowID, data); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Str("action", "update_and_commit_workflow_state").Str("node_id", DefaultWorkflowID).Msg("UpdateByID 失败")
	}
	s.workflowState.Commit()
}

// SetOutputs 向 io_state 写入当前节点的输出。
// data 被包裹在 {nodeID: data} 中。
func (s *WorkflowCommitState) SetOutputs(data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ioState == nil || data == nil {
		return
	}
	if err := s.ioState.UpdateByID(s.nodeID, map[string]any{s.nodeID: data}); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Str("action", "set_outputs").Str("node_id", s.nodeID).Msg("UpdateByID 失败")
	}
}

// GetInputs 从 io_state 查询父节点的输出（即当前节点输入）。
// schema 为零值时返回当前节点全部 IO 数据；否则按 parentID 前缀查找。
func (s *WorkflowCommitState) GetInputs(schema StateKey) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ioState == nil {
		return map[string]any{}
	}
	return s.ioState.GetByTransformer(transformer)
}

// CommitUserInputs 同时写入 io_state 和 global_state 并立即提交。
// 默认节点的 io_state data 不包裹在 {nodeID: data} 中。
func (s *WorkflowCommitState) CommitUserInputs(inputs map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ioState == nil || inputs == nil {
		return
	}
	if s.nodeID != DefaultNodeID {
		if err := s.ioState.UpdateByID(s.nodeID, map[string]any{s.nodeID: inputs}); err != nil {
			logger.Error(logger.ComponentAgentCore).Err(err).Str("action", "commit_user_inputs_io").Str("node_id", s.nodeID).Msg("UpdateByID 失败")
		}
	} else {
		if err := s.ioState.UpdateByID(s.nodeID, inputs); err != nil {
			logger.Error(logger.ComponentAgentCore).Err(err).Str("action", "commit_user_inputs_io").Str("node_id", s.nodeID).Msg("UpdateByID 失败")
		}
	}
	if err := s.globalState.UpdateByID(s.nodeID, inputs); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Str("action", "commit_user_inputs_global").Str("node_id", s.nodeID).Msg("UpdateByID 失败")
	}
	// 直接调用子状态 Commit，避免通过 s.Commit() 再次获取锁导致死锁
	s.ioState.Commit()
	s.compState.Commit()
	s.globalState.Commit()
	s.workflowState.Commit()
}

// Commit 提交全部（或指定节点）的四个子状态暂存更新。
// 不传 nodeID 则提交全部；传参则提交指定节点。
func (s *WorkflowCommitState) Commit(nodeID ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ioState.Commit(nodeID...)
	s.compState.Commit(nodeID...)
	s.globalState.Commit(nodeID...)
	s.workflowState.Commit(nodeID...)
}

// Rollback 回滚指定节点的四个子状态暂存更新。
func (s *WorkflowCommitState) Rollback(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compState.Rollback(nodeID)
	s.ioState.Rollback(nodeID)
	s.globalState.Rollback(nodeID)
	s.workflowState.Rollback(nodeID)
}

// UpdateByID 委托给 compState。
func (s *WorkflowCommitState) UpdateByID(nodeID string, data map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compState.UpdateByID(nodeID, data)
}

// CreateNodeState 创建节点专属状态视图。
// 共享底层四个子状态对象，切换 nodeID/parentID。
func (s *WorkflowCommitState) CreateNodeState(nodeID, parentID string) *WorkflowCommitState {
	s.mu.RLock()
	defer s.mu.RUnlock()
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

// GetUpdates 获取所有暂存更新。
// workflowOnly 控制是否包含 global_state_updates。
// 返回格式：{key: updates_dict, ...}
// 返回类型为 map[string]any（聚合视图），与 SetUpdates 输入类型一致。
// 内部子状态使用 map[string][]map[string]any，此处通过 deepCopyUpdates 拷贝后
// 作为 any 放入结果。WorkflowCommitState 不实现 CommitStateLike，此类型差异是有意设计。
func (s *WorkflowCommitState) GetUpdates() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
// 数据可能来自 JSON 反序列化（[]map[string]any → []any），通过 convertUpdatesFromJSON 处理。
// GetUpdates 返回 map[string]any（聚合视图），SetUpdates 接收同样的 map[string]any，
// 内部子状态使用 map[string][]map[string]any。WorkflowCommitState 不实现 CommitStateLike，
// GetUpdates/SetUpdates 的类型差异是有意设计（对齐 Python CommitState 继承 StateCollection
// 而非 CommitStateLike 的设计）。
func (s *WorkflowCommitState) SetUpdates(updates map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if updates == nil {
		return
	}
	if gs, ok := updates[GlobalStateUpdatesKey]; ok && gs != nil && s.workflowOnly {
		if m, ok := gs.(map[string][]map[string]any); ok {
			s.globalState.SetUpdates(m)
		} else if m, ok := convertUpdatesFromJSON(gs); ok {
			s.globalState.SetUpdates(m)
		}
	}
	if io, ok := updates[IOStateUpdatesKey]; ok && io != nil {
		if m, ok := io.(map[string][]map[string]any); ok {
			s.ioState.SetUpdates(m)
		} else if m, ok := convertUpdatesFromJSON(io); ok {
			s.ioState.SetUpdates(m)
		}
	}
	if comp, ok := updates[CompStateUpdatesKey]; ok && comp != nil {
		if m, ok := comp.(map[string][]map[string]any); ok {
			s.compState.SetUpdates(m)
		} else if m, ok := convertUpdatesFromJSON(comp); ok {
			s.compState.SetUpdates(m)
		}
	}
	if wf, ok := updates[WorkflowStateUpdatesKey]; ok && wf != nil {
		if m, ok := wf.(map[string][]map[string]any); ok {
			s.workflowState.SetUpdates(m)
		} else if m, ok := convertUpdatesFromJSON(wf); ok {
			s.workflowState.SetUpdates(m)
		}
	}
}

// WorkflowOnly 返回是否仅工作流模式。
func (s *WorkflowCommitState) WorkflowOnly() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workflowOnly
}

// ──────────────────────────── RecoverableStateLike 覆写 ────────────────────────────

// GetState 导出状态快照（覆写 WorkflowStateCollection.GetState）。
// workflowOnly 控制是否包含 global_state。
func (s *WorkflowCommitState) GetState() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
