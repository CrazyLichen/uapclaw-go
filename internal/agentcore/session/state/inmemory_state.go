package state

// ──────────────────────────── 结构体 ────────────────────────────

// InMemoryState State 接口的内存实现
// 对应 Python 的 InMemoryStateLike
type InMemoryState struct {
	// state 内部状态存储
	state map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryState 创建内存状态实例
func NewInMemoryState() *InMemoryState {
	return &InMemoryState{
		state: make(map[string]any),
	}
}

// ──────────────────────────── InMemoryState 方法 ────────────────────────────

// Get 根据 key 获取状态值（深拷贝返回）
func (s *InMemoryState) Get(key StateKey) any {
	return deepCopyValue(getBySchema(key, s.state))
}

// GetByPrefix 根据 key 和嵌套前缀获取状态值（深拷贝返回）
func (s *InMemoryState) GetByPrefix(key StateKey, nestedPrefix string) any {
	return deepCopyValue(getBySchema(key, s.state, nestedPrefix))
}

// GetByTransformer 通过转换函数获取状态值
func (s *InMemoryState) GetByTransformer(transformer Transformer) any {
	return transformer(s)
}

// Update 更新状态数据（深拷贝输入）
func (s *InMemoryState) Update(data map[string]any) error {
	updateDict(deepCopyMap(data), s.state)
	return nil
}

// GetState 获取完整状态快照（深拷贝返回）
func (s *InMemoryState) GetState() map[string]any {
	return deepCopyMap(s.state)
}

// SetState 从快照恢复状态
func (s *InMemoryState) SetState(state map[string]any) {
	if state != nil {
		s.state = state
	}
}
