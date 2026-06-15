package state

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// InMemoryStateLike StateLike 接口的内存实现
// 对应 Python: InMemoryStateLike(StateLike)
type InMemoryStateLike struct {
	// mu 并发读写锁
	mu sync.RWMutex
	// state 内部状态存储
	state map[string]any
}

// ──────────────────────────── 向后兼容别名 ────────────────────────────

// InMemoryState 保持向后兼容，后续版本移除
type InMemoryState = InMemoryStateLike

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryStateLike 创建内存状态实例
func NewInMemoryStateLike() *InMemoryStateLike {
	return &InMemoryStateLike{
		state: make(map[string]any),
	}
}

// NewInMemoryState 保持向后兼容，后续版本移除
var NewInMemoryState = NewInMemoryStateLike

// ──────────────────────────── StateLike 接口实现 ────────────────────────────

// Get 根据 key 获取状态值（深拷贝返回）
func (s *InMemoryStateLike) Get(key StateKey) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deepCopyValue(getBySchema(key, s.state))
}

// GetByPrefix 根据 key 和嵌套前缀获取状态值（深拷贝返回）
func (s *InMemoryStateLike) GetByPrefix(key StateKey, nestedPrefix string) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deepCopyValue(getBySchema(key, s.state, nestedPrefix))
}

// GetByTransformer 通过转换函数获取状态值
func (s *InMemoryStateLike) GetByTransformer(transformer Transformer) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return transformer(s)
}

// Update 更新状态数据（深拷贝输入）
func (s *InMemoryStateLike) Update(data map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	updateDict(deepCopyMap(data), s.state)
	return nil
}

// GetState 获取完整状态快照（深拷贝返回）
func (s *InMemoryStateLike) GetState() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deepCopyMap(s.state)
}

// SetState 从快照恢复状态
func (s *InMemoryStateLike) SetState(state map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state != nil {
		s.state = state
	}
}

// ──────────────────────────── SessionState 默认实现 ────────────────────────────

// GetGlobal 单存储单元无全局概念，返回 nil
func (s *InMemoryStateLike) GetGlobal(key StateKey) any { return nil }

// UpdateGlobal 单存储单元无全局概念，空操作
func (s *InMemoryStateLike) UpdateGlobal(data map[string]any) {}

// UpdateTrace 单存储单元无追踪概念，空操作
func (s *InMemoryStateLike) UpdateTrace(span any) {}

// Dump 导出完整状态，委托 GetState
func (s *InMemoryStateLike) Dump() map[string]any { return s.GetState() }
