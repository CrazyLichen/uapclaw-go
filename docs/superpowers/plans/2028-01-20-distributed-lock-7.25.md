# 7.25 Memory Common — DistributedLock 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `memory/common` 包中实现 `DistributedLock`，完成 7.25 Memory Common 章节，使 7.27 LongTermMemory 可直接使用。

**Architecture:** 1:1 复刻 Python `DistributedLock`，基于 `kv.BaseKVStore.ExclusiveSet` 原子操作实现跨进程互斥锁。自旋获取（固定 10ms 重试）、UUID 防误删、TTL 防死锁。Go 特有补充：ctx 取消支持 + `WithLock` 便捷方法（对齐 Python async context manager）。

**Tech Stack:** Go, `kv.BaseKVStore` 接口, `github.com/google/uuid`, 项目内部 `logger` 包

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 新增 | `internal/agentcore/memory/common/distributed_lock.go` | DistributedLock 结构体 + Acquire/Release/WithLock |
| 新增 | `internal/agentcore/memory/common/distributed_lock_test.go` | 单元测试（9 个用例） |
| 修改 | `internal/agentcore/memory/common/doc.go` | 文件目录新增 distributed_lock.go 条目 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 7.25 状态 ☐ → ✅ |

---

### Task 1: distributed_lock.go — 结构体与构造函数

**Files:**
- Create: `internal/agentcore/memory/common/distributed_lock.go`

- [ ] **Step 1: 创建 distributed_lock.go，写入结构体与构造函数**

```go
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
		store:       store,
		lockKey:     "_lock/" + lockName,
		ttl:         10,
		retryDelay:  10 * time.Millisecond,
		lockValue:   "",
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/common/`
Expected: 编译成功，无错误

---

### Task 2: distributed_lock.go — Acquire 方法

**Files:**
- Modify: `internal/agentcore/memory/common/distributed_lock.go`

- [ ] **Step 1: 在导出函数区块添加 Acquire 方法**

在 `NewDistributedLock` 函数之后添加：

```go
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
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/common/`
Expected: 编译成功

---

### Task 3: distributed_lock.go — Release 方法

**Files:**
- Modify: `internal/agentcore/memory/common/distributed_lock.go`

- [ ] **Step 1: 在 Acquire 方法之后添加 Release 方法**

```go
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
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/common/`
Expected: 编译成功

---

### Task 4: distributed_lock.go — WithLock 便捷方法

**Files:**
- Modify: `internal/agentcore/memory/common/distributed_lock.go`

- [ ] **Step 1: 在 Release 方法之后添加 WithLock 方法**

```go
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
	defer l.Release(ctx)
	return fn(ctx)
}
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/common/`
Expected: 编译成功

---

### Task 5: distributed_lock_test.go — 基础测试

**Files:**
- Create: `internal/agentcore/memory/common/distributed_lock_test.go`

- [ ] **Step 1: 创建测试文件，写入基础测试用例**

```go
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
```

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -tags=test -run 'TestNewDistributedLock|TestDistributedLock_Acquire$|TestDistributedLock_Release$|TestDistributedLock_Release_非持有者不删|TestDistributedLock_Release_锁不存在' ./internal/agentcore/memory/common/ -v`
Expected: 5 个测试全部 PASS

---

### Task 6: distributed_lock_test.go — 竞争与取消测试

**Files:**
- Modify: `internal/agentcore/memory/common/distributed_lock_test.go`

- [ ] **Step 1: 在测试文件末尾添加竞争和取消测试**

```go
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
```

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -tags=test -run 'TestDistributedLock_Acquire_竞争自旋|TestDistributedLock_Acquire_上下文取消' ./internal/agentcore/memory/common/ -v`
Expected: 2 个测试全部 PASS

---

### Task 7: distributed_lock_test.go — WithLock 测试

**Files:**
- Modify: `internal/agentcore/memory/common/distributed_lock_test.go`

- [ ] **Step 1: 在测试文件末尾添加 WithLock 测试**

```go
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
```

- [ ] **Step 2: 运行全部测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -tags=test ./internal/agentcore/memory/common/ -v`
Expected: 全部 9 个 DistributedLock 测试 + 已有的 base/kv_prefix_registry 测试全部 PASS

---

### Task 8: 更新 doc.go

**Files:**
- Modify: `internal/agentcore/memory/common/doc.go`

- [ ] **Step 1: 读取当前 doc.go 内容**

Run: `cat /home/opensource/uapclaw-gateway/internal/agentcore/memory/common/doc.go`

- [ ] **Step 2: 在文件目录中添加 distributed_lock.go 条目**

将文件目录部分从：

```
//	common/
//	├── doc.go                  # 包文档
//	├── kv_prefix_registry.go   # KV 前缀注册表
//	└── base.go                 # 记忆索引名称生成/解析 + 命中结果解析
```

改为：

```
//	common/
//	├── doc.go                  # 包文档
//	├── base.go                 # 记忆索引名称生成/解析 + 命中结果解析
//	├── kv_prefix_registry.go   # KV 前缀注册表
//	└── distributed_lock.go     # 基于 KV Store 的分布式锁
```

同时更新包功能概述，在现有描述后补充 DistributedLock 说明：

```
// Package common 提供记忆系统的公共工具。
//
// 本包包含向量索引名称生成/解析、记忆命中结果解析、KV 前缀注册表、
// 以及基于 KV Store 的分布式锁等跨模块共享的基础设施组件，
// 供记忆管理器和迁移器使用。
```

- [ ] **Step 3: 验证编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/common/`
Expected: 编译成功

---

### Task 9: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 7.25 状态从 ☐ 改为 ✅**

查找：
```
| 7.25 | ☐ | Memory Common | 记忆公共工具 | `openjiuwen/core/memory/common/` |
```

替换为：
```
| 7.25 | ✅ | Memory Common | 记忆公共工具（base.go + KvPrefixRegistry 已在 7.7/7.9 回填 ✅；DistributedLock ✅） | `openjiuwen/core/memory/common/` |
```

- [ ] **Step 2: 验证文件修改正确**

Run: `grep '7.25' /home/opensource/uapclaw-gateway/IMPLEMENTATION_PLAN.md`
Expected: 只有一行，状态为 ✅

---

### Task 10: 全量编译与测试验证

- [ ] **Step 1: 检查是否有残留 go 编译进程**

Run: `pgrep -f 'go (build|test)' || echo "无残留进程"`
Expected: "无残留进程"（如有则 kill）

- [ ] **Step 2: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 3: 运行 memory/common 包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test -tags=test -cover ./internal/agentcore/memory/common/ -v`
Expected: 全部测试 PASS，覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/memory/common/distributed_lock.go
git add internal/agentcore/memory/common/distributed_lock_test.go
git add internal/agentcore/memory/common/doc.go
git add IMPLEMENTATION_PLAN.md
git commit -m "feat(memory): 实现 7.25 Memory Common — DistributedLock 分布式锁

1:1 复刻 Python DistributedLock：
- 基于 kv.BaseKVStore.ExclusiveSet 原子操作
- 自旋获取（固定 10ms 重试）+ UUID 防误删 + TTL(10s) 防死锁
- Go 特有：ctx 取消支持 + WithLock 便捷方法（对齐 Python async context manager）
- Release 失败时 Error 日志 + 静默返回 nil（对齐 Python try/except）
- 9 个单元测试覆盖：构造/获取/释放/竞争自旋/ctx取消/非持有者/锁不存在/WithLock/fn出错
- doc.go 文件目录更新 + IMPLEMENTATION_PLAN.md 7.25 ☐→✅"
```
