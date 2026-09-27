package common

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DistributedLock 基于 KV Store 的分布式锁。
// 通过 ExclusiveSet 原子操作实现跨进程互斥，
// UUID 防误删，TTL 防死锁。
//
// Python: openjiuwen/core/memory/common/distributed_lock.py (DistributedLock)
type DistributedLock struct {
	// store KV 存储后端（Redis/InMemory 等）
	store kv.BaseKVStore
	// lockKey 锁键名，格式 "_lock/{lockName}"
	lockKey string
	// ttl 锁过期秒数，防止死锁
	ttl int
	// retryDelay 获取失败后的重试间隔
	retryDelay time.Duration
	// lockValue 当前锁持有者的唯一标识（UUID），防止误删
	lockValue string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDistributedLock 创建分布式锁实例。
//
// Python: DistributedLock(store, lock_name)
func NewDistributedLock(store kv.BaseKVStore, lockName string) *DistributedLock {
	return &DistributedLock{
		store:      store,
		lockKey:    "_lock/" + lockName,
		ttl:        10,
		retryDelay: 10 * time.Millisecond,
		lockValue:  "",
	}
}

// Acquire 获取分布式锁。自旋调用 ExclusiveSet 直到成功或 ctx 被取消。
//
// 每次尝试生成新的 UUID 作为 lockValue，通过 ExclusiveSet 原子写入。
// 成功时 lockValue 被保存，用于 Release 时校验身份。
// 失败时等待 retryDelay 后重试。
//
// Python: DistributedLock.acquire()
func (l *DistributedLock) Acquire(ctx context.Context) error {
	l.lockValue = uuid.New().String()
	for {
		ok, err := l.store.ExclusiveSet(ctx, l.lockKey, []byte(l.lockValue), l.ttl)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(l.retryDelay):
		}
	}
}

// Release 释放分布式锁。校验 lockValue 一致后才删除，防止误删他人持有的锁。
// 释放失败时记录 Error 日志但不返回 error（对齐 Python 的 try/except 静默处理）。
//
// Python: DistributedLock.release()
func (l *DistributedLock) Release(ctx context.Context) error {
	val, err := l.store.Get(ctx, l.lockKey)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "MEMORY_STORE").
			Msg("释放锁失败")
		return nil
	}
	if string(val) == l.lockValue {
		if delErr := l.store.Delete(ctx, l.lockKey); delErr != nil {
			logger.Error(logComponent).Err(delErr).
				Str("event_type", "MEMORY_STORE").
				Msg("释放锁失败")
		}
	}
	return nil
}

// WithLock 获取锁后执行 fn，执行完毕自动释放锁。
// 对齐 Python 的 `async with DistributedLock(store, name):` 用法。
//
// 用法:
//
//	err := lock.WithLock(ctx, func(ctx context.Context) error {
//	    // 临界区操作
//	    return nil
//	})
func (l *DistributedLock) WithLock(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := l.Acquire(ctx); err != nil {
		return err
	}
	defer func() { _ = l.Release(ctx) }()
	return fn(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
