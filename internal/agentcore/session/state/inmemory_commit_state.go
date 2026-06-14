package state

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InMemoryCommitState CommitState 接口的内存实现
// 对应 Python 的 InMemoryCommitState
type InMemoryCommitState struct {
	// state 底层状态（默认 InMemoryState）
	state State
	// updates 按 nodeID 缓存的待提交更新
	updates map[string][]map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryCommitState 创建内存事务状态实例
// 可选传入底层 State，默认创建新的 InMemoryState
func NewInMemoryCommitState(state ...State) *InMemoryCommitState {
	var s State
	if len(state) > 0 && state[0] != nil {
		s = state[0]
	} else {
		s = NewInMemoryState()
	}
	return &InMemoryCommitState{
		state:   s,
		updates: make(map[string][]map[string]any),
	}
}

// ──────────────────────────── InMemoryCommitState 方法 ────────────────────────────

// Get 委托给底层 state
func (s *InMemoryCommitState) Get(key StateKey) any {
	return s.state.Get(key)
}

// GetByPrefix 委托给底层 state
func (s *InMemoryCommitState) GetByPrefix(key StateKey, nestedPrefix string) any {
	return s.state.GetByPrefix(key, nestedPrefix)
}

// GetByTransformer 委托给底层 state
func (s *InMemoryCommitState) GetByTransformer(transformer Transformer) any {
	return s.state.GetByTransformer(transformer)
}

// GetState 委托给底层 state
func (s *InMemoryCommitState) GetState() map[string]any {
	return s.state.GetState()
}

// SetState 委托给底层 state
func (s *InMemoryCommitState) SetState(state map[string]any) {
	s.state.SetState(state)
}

// Update 禁止直接调用，必须使用 UpdateByID
// 对应 Python: raise build_error(StatusCode.ERROR, msg="commit state update must support node_id")
func (s *InMemoryCommitState) Update(data map[string]any) error {
	return fmt.Errorf("commit state update must support node_id")
}

// UpdateByID 按节点 ID 暂存更新（深拷贝 data）
func (s *InMemoryCommitState) UpdateByID(nodeID string, data map[string]any) {
	if nodeID == "" {
		return
	}
	s.updates[nodeID] = append(s.updates[nodeID], deepCopyMap(data))
}

// Commit 提交暂存更新到底层 state
// 不传 nodeID 则提交所有节点的暂存
func (s *InMemoryCommitState) Commit(nodeID ...string) {
	if len(nodeID) == 0 {
		// 提交全部
		for key, updates := range s.updates {
			for _, update := range updates {
				if err := s.state.Update(update); err != nil {
					logger.Error(logger.ComponentAgentCore).
						Err(err).
						Str("action", "commit").
						Str("node_id", key).
						Msg("提交更新到底层 state 失败")
				}
			}
			s.updates[key] = nil
		}
		s.updates = make(map[string][]map[string]any)
	} else {
		// 提交指定节点
		for _, id := range nodeID {
			nodeUpdates, exists := s.updates[id]
			if !exists || len(nodeUpdates) == 0 {
				continue
			}
			for _, update := range nodeUpdates {
				if err := s.state.Update(update); err != nil {
					logger.Error(logger.ComponentAgentCore).
						Err(err).
						Str("action", "commit").
						Str("node_id", id).
						Msg("提交更新到底层 state 失败")
				}
			}
			s.updates[id] = nil
		}
	}
}

// Rollback 丢弃指定节点的暂存更新
func (s *InMemoryCommitState) Rollback(nodeID string) {
	s.updates[nodeID] = nil
}

// GetUpdates 获取所有暂存更新
func (s *InMemoryCommitState) GetUpdates() map[string][]map[string]any {
	result := make(map[string][]map[string]any, len(s.updates))
	for key, updates := range s.updates {
		if len(updates) > 0 {
			copied := make([]map[string]any, len(updates))
			for i, u := range updates {
				copied[i] = deepCopyMap(u)
			}
			result[key] = copied
		}
	}
	return result
}

// SetUpdates 设置暂存更新
func (s *InMemoryCommitState) SetUpdates(updates map[string][]map[string]any) {
	if updates != nil {
		s.updates = updates
	}
}
