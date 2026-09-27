//go:build test

package common

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
)

// TestNewDistributedLock 测试构造函数参数正确
func TestNewDistributedLock(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	lock := NewDistributedLock(store, "user/123")
	if lock.lockKey != "_lock/user/123" {
		t.Errorf("lockKey = %q, want %q", lock.lockKey, "_lock/user/123")
	}
	if lock.ttl != 10 {
		t.Errorf("ttl = %d, want 10", lock.ttl)
	}
	if lock.retryDelay != 10*time.Millisecond {
		t.Errorf("retryDelay = %v, want 10ms", lock.retryDelay)
	}
	if lock.lockValue != "" {
		t.Errorf("lockValue = %q, want empty", lock.lockValue)
	}
}

// TestDistributedLock_Acquire 测试无竞争时一次获取成功
func TestDistributedLock_Acquire(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	lock := NewDistributedLock(store, "test-lock")
	err := lock.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire 返回 error: %v", err)
	}
	if lock.lockValue == "" {
		t.Error("Acquire 后 lockValue 不应为空")
	}
	// 验证 KV 中写入了锁
	val, err := store.Get(context.Background(), "_lock/test-lock")
	if err != nil {
		t.Fatalf("Get 返回 error: %v", err)
	}
	if string(val) != lock.lockValue {
		t.Errorf("KV 中的值 = %q, lockValue = %q, 应一致", string(val), lock.lockValue)
	}
}

// TestDistributedLock_Release 测试正常释放后其他锁可获取
func TestDistributedLock_Release(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	lock1 := NewDistributedLock(store, "test-lock")
	if err := lock1.Acquire(context.Background()); err != nil {
		t.Fatalf("lock1 Acquire 失败: %v", err)
	}
	// 释放
	if err := lock1.Release(context.Background()); err != nil {
		t.Fatalf("lock1 Release 失败: %v", err)
	}
	// 另一个锁应能获取
	lock2 := NewDistributedLock(store, "test-lock")
	if err := lock2.Acquire(context.Background()); err != nil {
		t.Fatalf("lock2 Acquire 失败（释放后应可获取）: %v", err)
	}
}

// TestDistributedLock_Release_非持有者不删 测试 lockValue 不匹配时不删除
func TestDistributedLock_Release_非持有者不删(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	lock1 := NewDistributedLock(store, "test-lock")
	if err := lock1.Acquire(context.Background()); err != nil {
		t.Fatalf("lock1 Acquire 失败: %v", err)
	}
	// 另一个锁实例尝试释放（lockValue 不同，不应删除）
	lock2 := NewDistributedLock(store, "test-lock")
	// lock2 未 Acquire，lockValue 为空，Release 不应删除 lock1 的锁
	if err := lock2.Release(context.Background()); err != nil {
		t.Fatalf("lock2 Release 返回 error: %v", err)
	}
	// lock1 的锁仍应存在
	val, _ := store.Get(context.Background(), "_lock/test-lock")
	if string(val) != lock1.lockValue {
		t.Error("非持有者 Release 后锁被误删")
	}
}

// TestDistributedLock_Release_锁不存在 测试 key 不存在时 Release 不报错
func TestDistributedLock_Release_锁不存在(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	lock := NewDistributedLock(store, "nonexistent")
	lock.lockValue = "fake-uuid"
	err := lock.Release(context.Background())
	if err != nil {
		t.Errorf("锁不存在时 Release 应返回 nil，实际: %v", err)
	}
}

// TestDistributedLock_Acquire_竞争自旋 测试两个锁争同一 key
func TestDistributedLock_Acquire_竞争自旋(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	lock1 := NewDistributedLock(store, "contended-lock")
	lock2 := NewDistributedLock(store, "contended-lock")

	// lock1 先获取
	if err := lock1.Acquire(context.Background()); err != nil {
		t.Fatalf("lock1 Acquire 失败: %v", err)
	}

	// 用 goroutine 让 lock2 在后台等待，lock1 释放后 lock2 应能获取
	lock2Acquired := make(chan error, 1)
	go func() {
		// 使用短超时 ctx 防止测试挂死
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		lock2Acquired <- lock2.Acquire(ctx)
	}()

	// 等一小段时间确保 lock2 进入自旋
	time.Sleep(50 * time.Millisecond)

	// lock1 释放
	if err := lock1.Release(context.Background()); err != nil {
		t.Fatalf("lock1 Release 失败: %v", err)
	}

	// lock2 应能获取
	select {
	case err := <-lock2Acquired:
		if err != nil {
			t.Fatalf("lock2 Acquire 失败: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lock2 Acquire 超时")
	}
}

// TestDistributedLock_Acquire_上下文取消 测试 ctx 取消后 Acquire 立即返回
func TestDistributedLock_Acquire_上下文取消(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	lock1 := NewDistributedLock(store, "cancel-lock")
	lock2 := NewDistributedLock(store, "cancel-lock")

	// lock1 先获取
	if err := lock1.Acquire(context.Background()); err != nil {
		t.Fatalf("lock1 Acquire 失败: %v", err)
	}

	// lock2 尝试获取，但 ctx 很快取消
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := lock2.Acquire(ctx)
	if err == nil {
		t.Error("ctx 取消后 Acquire 应返回 error")
		// 如果意外获取了锁，释放它避免影响其他测试
		_ = lock2.Release(context.Background())
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error 类型 = %v, want context.DeadlineExceeded", err)
	}

	// 清理
	_ = lock1.Release(context.Background())
}

// TestDistributedLock_WithLock 测试 WithLock 正常获取/释放
func TestDistributedLock_WithLock(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	lock := NewDistributedLock(store, "withlock-test")
	executed := false

	err := lock.WithLock(context.Background(), func(ctx context.Context) error {
		executed = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithLock 返回 error: %v", err)
	}
	if !executed {
		t.Error("fn 未被执行")
	}
	// 释放后锁应不存在
	val, _ := store.Get(context.Background(), "_lock/withlock-test")
	if val != nil {
		t.Error("WithLock 执行后锁未被释放")
	}
}

// TestDistributedLock_WithLock_fn出错也释放 测试 fn 返回 error 时锁仍被释放
func TestDistributedLock_WithLock_fn出错也释放(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	lock := NewDistributedLock(store, "withlock-error-test")

	fnErr := errors.New("业务逻辑错误")
	err := lock.WithLock(context.Background(), func(ctx context.Context) error {
		return fnErr
	})
	if !errors.Is(err, fnErr) {
		t.Errorf("WithLock 应返回 fn 的 error，实际: %v", err)
	}
	// 即使 fn 出错，锁仍应被释放
	val, _ := store.Get(context.Background(), "_lock/withlock-error-test")
	if val != nil {
		t.Error("fn 出错后锁未被释放")
	}
}
