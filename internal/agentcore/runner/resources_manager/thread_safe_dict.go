package resources_manager

import (
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ThreadSafeDict 泛型线程安全字典，使用 sync.RWMutex 保护 map 并发访问。
//
// 对应 Python: ThreadSafeDict (openjiuwen/core/runner/resources_manager/thread_safe_dict.py)
// Python 使用 threading.RLock（可重入锁），Go 使用 sync.RWMutex（读写分离锁）。
type ThreadSafeDict[K comparable, V any] struct {
	// mu 读写锁
	mu sync.RWMutex
	// data 底层 map
	data map[K]V
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewThreadSafeDict 创建线程安全字典。
//
// 对应 Python: ThreadSafeDict(initial_data=None)
func NewThreadSafeDict[K comparable, V any]() *ThreadSafeDict[K, V] {
	return &ThreadSafeDict[K, V]{
		data: make(map[K]V),
	}
}

// NewThreadSafeDictWithInitial 创建带初始数据的线程安全字典。
func NewThreadSafeDictWithInitial[K comparable, V any](initial map[K]V) *ThreadSafeDict[K, V] {
	if initial == nil {
		initial = make(map[K]V)
	}
	return &ThreadSafeDict[K, V]{
		data: initial,
	}
}

// Get 获取值，不存在返回零值。
//
// 对应 Python: ThreadSafeDict.get(key, default=None)
func (d *ThreadSafeDict[K, V]) Get(key K) V {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.data[key]
}

// Set 设置键值对。
//
// 对应 Python: ThreadSafeDict.__setitem__(key, value)
func (d *ThreadSafeDict[K, V]) Set(key K, value V) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.data[key] = value
}

// Delete 删除键值对，不存在时 panic。
//
// 对应 Python: ThreadSafeDict.__delitem__(key)
func (d *ThreadSafeDict[K, V]) Delete(key K) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.data, key)
}

// Len 返回元素数量。
//
// 对应 Python: ThreadSafeDict.__len__()
func (d *ThreadSafeDict[K, V]) Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.data)
}

// Contains 检查键是否存在。
//
// 对应 Python: ThreadSafeDict.__contains__(key)
func (d *ThreadSafeDict[K, V]) Contains(key K) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.data[key]
	return ok
}

// GetOrSet 获取值，不存在则设置默认值并返回。
//
// 对应 Python: ThreadSafeDict.get_or_set(key, default=None)
func (d *ThreadSafeDict[K, V]) GetOrSet(key K, defaultValue V) V {
	d.mu.Lock()
	defer d.mu.Unlock()
	if v, ok := d.data[key]; ok {
		return v
	}
	d.data[key] = defaultValue
	return defaultValue
}

// GetOrCreate 获取值，不存在则调用 creator 创建并存储。
//
// 对应 Python: ThreadSafeDict.get_or_create(key, creator, *args, **kwargs)
func (d *ThreadSafeDict[K, V]) GetOrCreate(key K, creator func() V) V {
	d.mu.Lock()
	defer d.mu.Unlock()
	if v, ok := d.data[key]; ok {
		return v
	}
	v := creator()
	d.data[key] = v
	return v
}

// Pop 移除并返回值，不存在返回零值。
//
// 对应 Python: ThreadSafeDict.pop(key, default=None)
func (d *ThreadSafeDict[K, V]) Pop(key K) V {
	d.mu.Lock()
	defer d.mu.Unlock()
	v := d.data[key]
	delete(d.data, key)
	return v
}

// SetDefault 如果键不存在则设置默认值，返回实际值。
//
// 对应 Python: ThreadSafeDict.setdefault(key, default=None)
func (d *ThreadSafeDict[K, V]) SetDefault(key K, defaultValue V) V {
	d.mu.Lock()
	defer d.mu.Unlock()
	if v, ok := d.data[key]; ok {
		return v
	}
	d.data[key] = defaultValue
	return defaultValue
}

// Update 批量更新键值对。
//
// 对应 Python: ThreadSafeDict.update(m, /, **kwargs)
func (d *ThreadSafeDict[K, V]) Update(m map[K]V) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, v := range m {
		d.data[k] = v
	}
}

// Clear 清空字典。
//
// 对应 Python: ThreadSafeDict.clear()
func (d *ThreadSafeDict[K, V]) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.data = make(map[K]V)
}

// Keys 返回所有键的切片。
//
// 对应 Python: ThreadSafeDict.keys()
func (d *ThreadSafeDict[K, V]) Keys() []K {
	d.mu.RLock()
	defer d.mu.RUnlock()
	keys := make([]K, 0, len(d.data))
	for k := range d.data {
		keys = append(keys, k)
	}
	return keys
}

// Values 返回所有值的切片。
//
// 对应 Python: ThreadSafeDict.values()
func (d *ThreadSafeDict[K, V]) Values() []V {
	d.mu.RLock()
	defer d.mu.RUnlock()
	values := make([]V, 0, len(d.data))
	for _, v := range d.data {
		values = append(values, v)
	}
	return values
}

// Items 返回所有键值对的切片。
//
// 对应 Python: ThreadSafeDict.items()
func (d *ThreadSafeDict[K, V]) Items() []struct {
	Key   K
	Value V
} {
	d.mu.RLock()
	defer d.mu.RUnlock()
	items := make([]struct {
		Key   K
		Value V
	}, 0, len(d.data))
	for k, v := range d.data {
		items = append(items, struct {
			Key   K
			Value V
		}{Key: k, Value: v})
	}
	return items
}
