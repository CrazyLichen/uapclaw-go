// Package reliability 测试用原子值容器（rail_hooks_test 专用）。
package reliability

import "sync"

// atomicValue 泛型原子值（测试捕获最近一条消息字段用）。
type atomicValue[T any] struct {
	mu sync.Mutex
	v  T
}

// Store 存值。
func (a *atomicValue[T]) Store(v T) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.v = v
}

// Load 取值。
func (a *atomicValue[T]) Load() T {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v
}
