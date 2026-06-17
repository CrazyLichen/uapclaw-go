package state

import (
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InMemoryCommitState CommitStateLike 接口的内存实现
// 对应 Python: InMemoryCommitState(CommitStateLike)
type InMemoryCommitState struct {
	// mu 并发读写锁
	mu sync.RWMutex
	// state 底层状态（默认 InMemoryStateLike）
	state StateLike
	// updates 按 nodeID 缓存的待提交更新
	updates map[string][]map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryCommitState 创建内存事务状态实例
// 可选传入底层 StateLike，默认创建新的 InMemoryStateLike
func NewInMemoryCommitState(state ...StateLike) *InMemoryCommitState {
	var s StateLike
	if len(state) > 0 && state[0] != nil {
		s = state[0]
	} else {
		s = NewInMemoryStateLike()
	}
	return &InMemoryCommitState{
		state:   s,
		updates: make(map[string][]map[string]any),
	}
}

// Get 委托给底层 state
func (s *InMemoryCommitState) Get(key StateKey) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Get(key)
}

// GetByPrefix 委托给底层 state
func (s *InMemoryCommitState) GetByPrefix(key StateKey, nestedPrefix string) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.GetByPrefix(key, nestedPrefix)
}

// GetByTransformer 委托给底层 state
func (s *InMemoryCommitState) GetByTransformer(transformer Transformer) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.GetByTransformer(transformer)
}

// GetState 委托给底层 state
func (s *InMemoryCommitState) GetState() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.GetState()
}

// SetState 委托给底层 state
func (s *InMemoryCommitState) SetState(state map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.SetState(state)
}

// Update 禁止直接调用，必须使用 UpdateByID
// 对应 Python: raise build_error(StatusCode.ERROR, msg="commit state update must support node_id")
func (s *InMemoryCommitState) Update(data map[string]any) error {
	return fmt.Errorf("commit state update must support node_id")
}

// UpdateByID 按节点 ID 暂存更新（深拷贝 data）
// nodeID 为空字符串时返回 error，对齐 Python 中 node_id is None 抛异常的行为
func (s *InMemoryCommitState) UpdateByID(nodeID string, data map[string]any) error {
	if nodeID == "" {
		return fmt.Errorf("不能使用空 nodeID 更新状态")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates[nodeID] = append(s.updates[nodeID], deepCopyMap(data))
	return nil
}

// Commit 提交暂存更新到底层 state
// 不传 nodeID 则提交所有节点的暂存
func (s *InMemoryCommitState) Commit(nodeID ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(nodeID) == 0 {
		// 提交全部：逐个应用更新到底层 state，然后清空整个暂存
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
			s.updates[id] = make([]map[string]any, 0)
		}
	}
}

// Rollback 丢弃指定节点的暂存更新
func (s *InMemoryCommitState) Rollback(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates[nodeID] = make([]map[string]any, 0)
}

// GetUpdates 获取所有暂存更新（深拷贝返回，对齐 Python get_updates 返回完整 key 集合）
func (s *InMemoryCommitState) GetUpdates() map[string][]map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string][]map[string]any, len(s.updates))
	for key, updates := range s.updates {
		copied := make([]map[string]any, len(updates))
		for i, u := range updates {
			copied[i] = deepCopyMap(u)
		}
		result[key] = copied
	}
	return result
}

// SetUpdates 设置暂存更新（深拷贝传入数据，与 GetUpdates 深拷贝返回对称）
// G-06 修复：加深拷贝，防止调用方保留引用修改内部状态。
// Python 不深拷贝是因为单线程 asyncio 无竞态，Go 有 goroutine 并发风险。
func (s *InMemoryCommitState) SetUpdates(updates map[string][]map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if updates != nil {
		s.updates = deepCopyUpdates(updates)
	}
}
