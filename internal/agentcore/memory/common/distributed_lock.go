package common

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DistributedLock 基于 KV Store 的分布式锁。
// 通过 ExclusiveSet 原子操作实现跨进程互斥，
// UUID 防误删，CompareAndDelete 原子释放，TTL 防死锁。
// 并发约束：一个实例只代表一个逻辑持有者。lockValue 是实例级可变状态，
// 同一实例不可跨 goroutine 共享（Acquire 已加重入守卫直接报错），
// 每次使用应当新建实例（对齐 Python 每次 `async with DistributedLock(...)` 的用法）。
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
	//
	// T-05：字段读写一律经 mu 保护。原实现裸读写，同实例被并发调用时
	// （-race 可复现）会出现「判空通过 → 被覆盖 → Release 删错/漏删」。
	lockValue string
	// mu 保护 lockValue；不跨 store I/O 持锁（否则 Release 会被 Acquire 自旋饿死）
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logEventMemoryStore 对齐 Python LogEventType.MEMORY_STORE 枚举值
const logEventMemoryStore = "memory_store"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 日志组件标识 — 分布式锁属于基础设施层，使用 ComponentCommon
	logComponent = logger.ComponentCommon

	// ErrLockAlreadyAcquired 同一实例重复获取锁时返回的错误。
	// 一个 DistributedLock 实例只代表一个逻辑持有者，重入会覆盖 lockValue
	// 导致后续 Release 无法匹配，因此直接报错而非静默覆盖。
	ErrLockAlreadyAcquired = errors.New("distributed lock already acquired by this instance")

	// ErrLockNotHeld 未持有时调用 Release（T-05）。
	// 原实现以空串值去 CompareAndDelete，白跑一次存储并打误导性 Warn。
	ErrLockNotHeld = errors.New("distributed lock is not held by this instance")

	// lockReleaseTimeout 释放锁的独立超时（T-06）。
	// 释放必须脱离调用方取消状态执行，否则请求 abort 会让锁残留到 TTL。
	lockReleaseTimeout = 5 * time.Second
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDistributedLock 创建分布式锁实例。
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
// 生成 UUID 作为 lockValue，通过 ExclusiveSet 原子写入。
// 成功时 lockValue 被保存，用于 Release 时校验身份。
// 失败时等待 retryDelay 后重试。
// 一个实例只代表一个逻辑持有者：已持有时重复调用返回 ErrLockAlreadyAcquired，
// 否则 lockValue 会被覆盖，导致后续 Release 匹配失败、锁泄漏到 TTL 到期。
// Python: DistributedLock.acquire()
func (l *DistributedLock) Acquire(ctx context.Context) error {
	l.mu.Lock()
	if l.lockValue != "" {
		l.mu.Unlock()
		return ErrLockAlreadyAcquired
	}
	l.mu.Unlock()

	for {
		candidate := uuid.New().String()
		ok, err := l.store.ExclusiveSet(ctx, l.lockKey, []byte(candidate), l.ttl)
		if err != nil {
			return err
		}
		if ok {
			l.mu.Lock()
			l.lockValue = candidate
			l.mu.Unlock()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(l.retryDelay):
		}
	}
}

// Release 释放分布式锁。通过 CompareAndDelete 原子校验并删除，防止误删他人持有的锁。
// 返回 nil 不代表锁一定已被释放：key 已过期（临界区超过 TTL）或被他人持有时会跳过删除，
// 并记录 Warn 日志。这与 Python 的 try/except 静默处理一致（返回值语义不变）。
// Python: DistributedLock.release()
func (l *DistributedLock) Release(ctx context.Context) error {
	// 先取快照：下面无论成功与否都会清空 lockValue
	l.mu.Lock()
	lockValue := l.lockValue
	l.lockValue = ""
	l.mu.Unlock()
	if lockValue == "" {
		// T-05：未持有时直接返回，不再用空值查一次存储并打误导性 Warn
		return ErrLockNotHeld
	}
	// T-06：释放必须脱离调用方取消状态（WithLock 的 defer 常在 ctx 已取消时执行），
	// 但仍需有界等待，避免存储卡死拖住停机。
	relCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lockReleaseTimeout)
	defer cancel()
	released, err := l.store.CompareAndDelete(relCtx, l.lockKey, []byte(lockValue))
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("lock_key", l.lockKey).
			Str("lock_value", lockValue).
			Str("event_type", logEventMemoryStore).
			Msg("failed to release lock")
		return nil
	}
	if !released {
		logger.Warn(logComponent).
			Str("lock_key", l.lockKey).
			Str("lock_value", lockValue).
			Str("event_type", logEventMemoryStore).
			Msg("lock not released: expired or owned by another")
	}
	return nil
}

// WithLock 获取锁后执行 fn，执行完毕自动释放锁。
// 对齐 Python 的 `async with DistributedLock(store, name):` 用法。
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
