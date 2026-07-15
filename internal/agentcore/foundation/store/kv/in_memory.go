package kv

import (
	"context"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// entry 内存存储条目，保存值和过期时间戳。
type entry struct {
	// value 存储的值
	value []byte
	// expiryTs 过期时间戳（Unix 秒），0 表示不过期
	expiryTs int64
}

// operation Pipeline 操作记录。
type operation struct {
	// op 操作类型："set"、"get"、"exists"
	op string
	// key 操作的键
	key string
	// value Set 操作的值，仅 op 为 "set" 时有效
	value []byte
	// expiry Set 操作的过期秒数，0 表示不过期
	// 对应 Python: BasedKVStorePipeline.set(key, value, ttl=None)
	expiry int
}

// InMemoryKVStore 基于内存的键值存储实现。
//
// 对应 Python: openjiuwen/core/foundation/store/kv/in_memory_kv_store.py
type InMemoryKVStore struct {
	// mu 读写锁，保证并发安全
	mu sync.RWMutex
	// store 内部存储映射
	store map[string]entry
}

// inMemoryPipeline 内存 Pipeline 实现。
type inMemoryPipeline struct {
	// ops 待执行的操作列表
	ops []operation
	// exec 执行函数闭包，在 Pipeline() 方法中创建
	exec func(ops []operation) ([]PipelineResult, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryKVStore 创建新的内存键值存储实例。
func NewInMemoryKVStore() *InMemoryKVStore {
	return &InMemoryKVStore{
		store: make(map[string]entry),
	}
}

// Set 存储或覆盖一个键值对。
func (s *InMemoryKVStore) Set(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[key] = entry{value: value, expiryTs: 0}
	return nil
}

// Get 根据 key 获取值，key 不存在或已过期时返回 nil。
func (s *InMemoryKVStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getWithoutLock(key), nil
}

// Exists 检查 key 是否存在且未过期。
func (s *InMemoryKVStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getWithoutLock(key) != nil, nil
}

// Delete 删除指定 key。key 不存在时返回 nil（不报错），与 Python 行为一致。
func (s *InMemoryKVStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.store, key)
	return nil
}

// ExclusiveSet 原子性地设置键值对，仅当 key 不存在或已过期时成功。
// expiry 为过期秒数，0 表示不过期。
// 返回 true 表示设置成功，false 表示 key 已存在且未过期。
func (s *InMemoryKVStore) ExclusiveSet(_ context.Context, key string, value []byte, expiry int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	if e, ok := s.store[key]; ok {
		if e.expiryTs != 0 && now > e.expiryTs {
			// 已过期：允许覆盖（对齐 Python: current_time > expiry_ts）
		} else {
			// 未过期：拒绝
			return false, nil
		}
	}
	var expiryTs int64
	if expiry > 0 {
		expiryTs = now + int64(expiry)
	}
	s.store[key] = entry{value: value, expiryTs: expiryTs}
	return true, nil
}

// GetByPrefix 获取所有以 prefix 开头的键值对，已过期的 key 不包含在结果中。
func (s *InMemoryKVStore) GetByPrefix(_ context.Context, prefix string) (map[string][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string][]byte)
	for k := range s.store {
		if strings.HasPrefix(k, prefix) {
			if v := s.getWithoutLock(k); v != nil {
				result[k] = v
			}
		}
	}
	return result, nil
}

// DeleteByPrefix 删除所有以 prefix 开头的键值对。
// batchSize 为每批删除的数量，0 或负数表示一次性删除（等价于 Python batch_size <= 0）。
func (s *InMemoryKVStore) DeleteByPrefix(_ context.Context, prefix string, batchSize int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	toDel := make([]string, 0)
	for k := range s.store {
		if strings.HasPrefix(k, prefix) {
			toDel = append(toDel, k)
		}
	}
	if batchSize <= 0 {
		for _, k := range toDel {
			delete(s.store, k)
		}
		return nil
	}
	for i := 0; i < len(toDel); i += batchSize {
		end := i + batchSize
		if end > len(toDel) {
			end = len(toDel)
		}
		for _, k := range toDel[i:end] {
			delete(s.store, k)
		}
	}
	return nil
}

// MGet 批量获取多个 key 的值。
// 返回值与输入 keys 顺序对应，不存在或已过期的 key 对应位置为 nil。
func (s *InMemoryKVStore) MGet(_ context.Context, keys []string) ([][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([][]byte, len(keys))
	for i, k := range keys {
		result[i] = s.getWithoutLock(k)
	}
	return result, nil
}

// BatchDelete 批量删除多个 key，返回成功删除的数量。
// batchSize 为每批删除的数量，0 或负数表示一次性删除（等价于 Python batch_size <= 0）。
func (s *InMemoryKVStore) BatchDelete(_ context.Context, keys []string, batchSize int) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	if batchSize <= 0 {
		for _, k := range keys {
			if _, ok := s.store[k]; ok {
				delete(s.store, k)
				deleted++
			}
		}
		return deleted, nil
	}
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		for _, k := range keys[i:end] {
			if _, ok := s.store[k]; ok {
				delete(s.store, k)
				deleted++
			}
		}
	}
	return deleted, nil
}

// Pipeline 创建批量操作管道。
// 闭包捕获 store 引用，Execute() 时加写锁批量执行。
func (s *InMemoryKVStore) Pipeline(_ context.Context) KVPipeline {
	return &inMemoryPipeline{
		ops: make([]operation, 0),
		exec: func(ops []operation) ([]PipelineResult, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			now := time.Now().Unix()
			results := make([]PipelineResult, 0, len(ops))
			for _, op := range ops {
				switch op.op {
				case "set":
					var expiryTs int64
					if op.expiry > 0 {
						expiryTs = now + int64(op.expiry)
					}
					s.store[op.key] = entry{value: op.value, expiryTs: expiryTs}
					results = append(results, PipelineResult{Op: "set", Key: op.key})
				case "get":
					v := s.getWithoutLock(op.key)
					results = append(results, PipelineResult{Op: "get", Key: op.key, Value: v})
				case "exists":
					v := s.getWithoutLock(op.key)
					results = append(results, PipelineResult{Op: "exists", Key: op.key, Exists: v != nil})
				}
			}
			return results, nil
		},
	}
}

// Set 向管道中添加一个 Set 操作（仅记录，不立即执行）。
// expiry 为过期秒数，0 表示不过期。
func (p *inMemoryPipeline) Set(_ context.Context, key string, value []byte, expiry int) error {
	p.ops = append(p.ops, operation{op: "set", key: key, value: value, expiry: expiry})
	return nil
}

// Get 向管道中添加一个 Get 操作（仅记录，不立即执行）。
func (p *inMemoryPipeline) Get(_ context.Context, key string) error {
	p.ops = append(p.ops, operation{op: "get", key: key})
	return nil
}

// Exists 向管道中添加一个 Exists 操作（仅记录，不立即执行）。
func (p *inMemoryPipeline) Exists(_ context.Context, key string) error {
	p.ops = append(p.ops, operation{op: "exists", key: key})
	return nil
}

// Execute 提交并执行管道中的所有操作，返回各操作的结果。
// 执行后管道被清空，可复用。
func (p *inMemoryPipeline) Execute(_ context.Context) ([]PipelineResult, error) {
	results, err := p.exec(p.ops)
	p.ops = nil // 清空操作列表，允许 Pipeline 复用
	return results, err
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getWithoutLock 在无锁状态下读取值并检查过期。
// 与 Python 的 _get_without_lock 对齐。
// key 不存在返回 nil，已过期返回 nil（但不删除）。
func (s *InMemoryKVStore) getWithoutLock(key string) []byte {
	e, ok := s.store[key]
	if !ok {
		return nil
	}
	if e.expiryTs != 0 && time.Now().Unix() > e.expiryTs {
		// 已过期：返回 nil，但不删除（允许 ExclusiveSet 覆盖）
		// 对齐 Python: current_time > expiry_ts（严格大于才算过期）
		return nil
	}
	return e.value
}
