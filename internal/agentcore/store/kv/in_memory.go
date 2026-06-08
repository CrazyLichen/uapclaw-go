package kv

import (
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
}

// InMemoryKVStore 基于 内存的键值存储实现。
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

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryKVStore 创建新的内存键值存储实例。
func NewInMemoryKVStore() *InMemoryKVStore {
	return &InMemoryKVStore{
		store: make(map[string]entry),
	}
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
		return nil
	}
	return e.value
}
