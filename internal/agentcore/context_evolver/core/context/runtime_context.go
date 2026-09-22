package context

import (
	"fmt"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeContext 操作间传递中间结果的上下文。
//
// Python 用 __getattr__/__setattr__ 动态属性访问，Go 改为 map + 方法对。
// Python: context.user_id = "alice" → Go: context.Set("user_id", "alice")
// Python: context.get("user_id") → Go: context.Get("user_id")
//
// Python: openjiuwen/extensions/context_evolver/core/context/runtime_context.py
type RuntimeContext struct {
	mu   sync.RWMutex
	data map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRuntimeContext 创建空的运行时上下文。
func NewRuntimeContext() *RuntimeContext {
	return &RuntimeContext{data: make(map[string]any)}
}

// GetTyped 泛型获取，带类型断言。
// 返回值和是否成功；类型不匹配或键不存在时 ok=false。
// Python 无此方法（Python 用动态类型直接访问），Go 需要类型安全辅助。
func GetTyped[T any](rc *RuntimeContext, key string) (T, bool) {
	rc.mu.RLock()
	v, ok := rc.data[key]
	rc.mu.RUnlock()
	if !ok {
		var zero T
		return zero, false
	}
	typed, ok := v.(T)
	if !ok {
		var zero T
		return zero, false
	}
	return typed, true
}

// ──────────────────────────── 导出方法 ────────────────────────────

// Set 设置键值。
// 对齐 Python RuntimeContext.set(key, value)。
func (rc *RuntimeContext) Set(key string, value any) {
	rc.mu.Lock()
	rc.data[key] = value
	rc.mu.Unlock()
}

// Get 获取值，不存在时返回 nil。
// 对齐 Python RuntimeContext.get(key, default=None) 的无默认值形式。
func (rc *RuntimeContext) Get(key string) any {
	rc.mu.RLock()
	v := rc.data[key]
	rc.mu.RUnlock()
	return v
}

// GetDefault 获取值，不存在时返回默认值。
// 对齐 Python RuntimeContext.get(key, default)。
func (rc *RuntimeContext) GetDefault(key string, defaultVal any) any {
	rc.mu.RLock()
	v, ok := rc.data[key]
	rc.mu.RUnlock()
	if !ok {
		return defaultVal
	}
	return v
}

// ToDict 返回数据快照副本。
// 对齐 Python RuntimeContext.to_dict()，返回浅拷贝。
func (rc *RuntimeContext) ToDict() map[string]any {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	cpy := make(map[string]any, len(rc.data))
	for k, v := range rc.data {
		cpy[k] = v
	}
	return cpy
}

// String 实现 Stringer 接口。
// 对齐 Python RuntimeContext.__repr__()。
func (rc *RuntimeContext) String() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return fmt.Sprintf("RuntimeContext(%v)", rc.data)
}
