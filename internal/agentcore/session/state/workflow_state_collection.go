package state

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowStateCollection 工作流场景的状态集合。
//
// 组合 io_state/global_state/comp_state/workflow_state 四个可提交区域，
// 提供三级回退查询（global_state → io_state[parentID] → io_state[nodeID]）。
// 实现 SessionState 接口。
//
// 对应 Python: openjiuwen/core/session/state/workflow_state.py (StateCollection)
type WorkflowStateCollection struct {
	// mu 并发读写锁
	mu sync.RWMutex
	// ioState 输入输出状态
	ioState CommitStateLike
	// globalState 全局状态（从 AgentSession 共享）
	globalState CommitStateLike
	// compState 组件状态
	compState CommitStateLike
	// workflowState 工作流状态
	workflowState CommitStateLike
	// traceState 追踪状态（按 nodeID 存 span）
	traceState map[string]any
	// parentID 父节点 ID
	parentID string
	// nodeID 当前节点 ID
	nodeID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowStateCollection 创建工作流状态集合实例。
func NewWorkflowStateCollection(ioState, globalState, compState, workflowState CommitStateLike, traceState map[string]any, parentID, nodeID string) *WorkflowStateCollection {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_workflow_state_collection").
		Str("parent_id", parentID).
		Str("node_id", nodeID).
		Msg("创建工作流状态集合")
	if traceState == nil {
		traceState = make(map[string]any)
	}
	return &WorkflowStateCollection{
		ioState:       ioState,
		globalState:   globalState,
		compState:     compState,
		workflowState: workflowState,
		traceState:    traceState,
		parentID:      parentID,
		nodeID:        nodeID,
	}
}

// GetGlobal 从全局状态获取值。
// 三级回退查询：globalState → ioState[parentID] → ioState[nodeID]。
// G-02 修复：key 为 AllStateKey 时返回 nil（Workflow 层无"获取全部"语义，对齐 Python），
// key 为零值时也返回 nil（不再触发"获取全部"逻辑）。
func (s *WorkflowStateCollection) GetGlobal(key StateKey) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.globalState == nil || key.IsAll() || key.IsZero() {
		return nil
	}
	result := s.globalState.Get(key)
	if result != nil {
		return result
	}
	result = s.ioState.GetByPrefix(key, s.parentID)
	if result != nil {
		return result
	}
	return s.ioState.GetByPrefix(key, s.nodeID)
}

// UpdateGlobal 更新全局状态，以当前 nodeID 为键暂存更新。
func (s *WorkflowStateCollection) UpdateGlobal(data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.globalState == nil || data == nil {
		return
	}
	if err := s.globalState.UpdateByID(s.nodeID, data); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Str("action", "update_global").Str("node_id", s.nodeID).Msg("UpdateByID 失败")
	}
}

// SetGlobal 从快照恢复全局状态。
// 对齐 Python: session.state().global_state.set_state(state)
func (s *WorkflowStateCollection) SetGlobal(st map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.globalState == nil || st == nil {
		return
	}
	s.globalState.SetState(st)
}

// UpdateTrace 更新追踪状态。
func (s *WorkflowStateCollection) UpdateTrace(span any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traceState[s.nodeID] = span
}

// CommitCmp 提交当前节点的 comp_state 和 io_state。
func (s *WorkflowStateCollection) CommitCmp() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compState.Commit(s.nodeID)
	s.ioState.Commit(s.nodeID)
}

// Dump 导出完整状态快照。
func (s *WorkflowStateCollection) Dump() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		IOStateKey:              s.ioState.GetState(),
		IOStateUpdatesKey:       s.ioState.GetUpdates(),
		GlobalStateKey:          s.globalState.GetState(),
		GlobalStateUpdatesKey:   s.globalState.GetUpdates(),
		CompStateKey:            s.compState.GetState(),
		CompStateUpdatesKey:     s.compState.GetUpdates(),
		WorkflowStateKey:        s.workflowState.GetState(),
		WorkflowStateUpdatesKey: s.workflowState.GetUpdates(),
		TraceStateKey:           s.traceState,
	}
}

// Get 根据 StateKey 获取组件状态值。
// key 为 nil 时返回当前节点的全部 comp_state；否则按 nodeID 前缀查找。
func (s *WorkflowStateCollection) Get(key StateKey) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.compState == nil {
		return nil
	}
	if key.IsZero() {
		return s.compState.Get(StringKey(s.nodeID))
	}
	return s.compState.GetByPrefix(key, s.nodeID)
}

// GetByPrefix 根据 key 和嵌套前缀获取组件状态值。
func (s *WorkflowStateCollection) GetByPrefix(key StateKey, nestedPrefix string) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.compState == nil {
		return nil
	}
	return s.compState.GetByPrefix(key, nestedPrefix)
}

// GetByTransformer 通过转换函数获取组件状态值。
func (s *WorkflowStateCollection) GetByTransformer(transformer Transformer) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.compState == nil {
		return nil
	}
	return s.compState.GetByTransformer(transformer)
}

// Update 更新组件状态，以当前 nodeID 为键暂存更新。
// data 被包裹在 {nodeID: data} 中。
func (s *WorkflowStateCollection) Update(data map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.compState == nil {
		return nil
	}
	if err := s.compState.UpdateByID(s.nodeID, map[string]any{s.nodeID: data}); err != nil {
		return err
	}
	return nil
}

// GetState / SetState 不在此实现（Python 的 StateCollection 也不覆写这两个方法）。
// 由 WorkflowCommitState 覆写，因为 CommitState 需要按 workflowOnly 条件控制 global_state 的包含。
