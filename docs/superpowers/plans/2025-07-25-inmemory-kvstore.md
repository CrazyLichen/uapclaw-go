# InMemoryKVStore 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 InMemoryKVStore 内存键值存储，满足 BaseKVStore + KVPipeline 接口，对照 Python 原版 in_memory_kv_store.py。

**Architecture:** 使用 `sync.RWMutex` + `map[string]entry` 实现并发安全的内存 KV 存储，惰性过期检查（不主动删除过期 key），Pipeline 使用闭包模式批量执行操作。

**Tech Stack:** Go 1.25, sync.RWMutex, time.Now().Unix() 过期判断

---

## 文件结构

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/agentcore/store/kv/in_memory.go` | 新建 | InMemoryKVStore + inMemoryPipeline + entry + operation |
| `internal/agentcore/store/kv/in_memory_test.go` | 新建 | InMemoryKVStore 全量单元测试 |
| `internal/agentcore/store/kv/doc.go` | 修改 | 添加 in_memory.go 和 in_memory_test.go 到文件目录 |

---

### Task 1: 创建 in_memory.go 骨架 — 结构体 + 构造函数 + getWithoutLock

**Files:**
- Create: `internal/agentcore/store/kv/in_memory.go`

- [ ] **Step 1: 编写结构体定义、构造函数和 getWithoutLock 辅助方法**

```go
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
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...`
Expected: 编译成功，无错误

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/in_memory.go
git commit -m "feat(kv): 添加 InMemoryKVStore 骨架 — 结构体、构造函数和 getWithoutLock"
```

---

### Task 2: 实现核心 CRUD 方法 — Set / Get / Exists / Delete

**Files:**
- Modify: `internal/agentcore/store/kv/in_memory.go`

- [ ] **Step 1: 在 `// ──────────────────────────── 导出函数 ────────────────────────────` 区块内 `NewInMemoryKVStore` 之后添加核心 CRUD 方法**

```go
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

// Delete 删除指定 key，key 不存在时不执行操作。
func (s *InMemoryKVStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.store, key)
	return nil
}
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...`
Expected: 编译成功

- [ ] **Step 3: 编写核心 CRUD 方法的测试**

创建文件 `internal/agentcore/store/kv/in_memory_test.go`：

```go
package kv

import (
	"context"
	"testing"
)

// ──── 构造函数测试 ────

// TestNewInMemoryKVStore 验证构造函数创建非 nil 实例。
func TestNewInMemoryKVStore(t *testing.T) {
	store := NewInMemoryKVStore()
	if store == nil {
		t.Fatal("NewInMemoryKVStore 返回 nil")
	}
	if store.store == nil {
		t.Fatal("内部 store 映射未初始化")
	}
}

// ──── Set / Get 测试 ────

// TestInMemoryKVStore_SetGet 基本 Set + Get 往返。
func TestInMemoryKVStore_SetGet(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	err := store.Set(ctx, "key1", []byte("value1"))
	if err != nil {
		t.Fatalf("Set 返回错误: %v", err)
	}

	val, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("Get 返回 %q, 期望 %q", string(val), "value1")
	}
}

// TestInMemoryKVStore_Get_不存在 验证获取不存在的 key 返回 nil。
func TestInMemoryKVStore_Get_不存在(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	val, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if val != nil {
		t.Errorf("Get 返回 %v, 期望 nil", val)
	}
}

// TestInMemoryKVStore_Set_覆盖 验证 Set 覆盖已有值。
func TestInMemoryKVStore_Set_覆盖(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("old"))
	_ = store.Set(ctx, "key1", []byte("new"))

	val, _ := store.Get(ctx, "key1")
	if string(val) != "new" {
		t.Errorf("覆盖后 Get 返回 %q, 期望 %q", string(val), "new")
	}
}

// ──── Exists 测试 ────

// TestInMemoryKVStore_Exists_存在 验证 key 存在时 Exists 返回 true。
func TestInMemoryKVStore_Exists_存在(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))

	exists, err := store.Exists(ctx, "key1")
	if err != nil {
		t.Fatalf("Exists 返回错误: %v", err)
	}
	if !exists {
		t.Error("Exists 返回 false, 期望 true")
	}
}

// TestInMemoryKVStore_Exists_不存在 验证 key 不存在时 Exists 返回 false。
func TestInMemoryKVStore_Exists_不存在(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	exists, err := store.Exists(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Exists 返回错误: %v", err)
	}
	if exists {
		t.Error("Exists 返回 true, 期望 false")
	}
}

// ──── Delete 测试 ────

// TestInMemoryKVStore_Delete 验证删除后 Get 返回 nil。
func TestInMemoryKVStore_Delete(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	_ = store.Delete(ctx, "key1")

	val, _ := store.Get(ctx, "key1")
	if val != nil {
		t.Errorf("删除后 Get 返回 %v, 期望 nil", val)
	}
}

// TestInMemoryKVStore_Delete_不存在 验证删除不存在的 key 不报错。
func TestInMemoryKVStore_Delete_不存在(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	err := store.Delete(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("删除不存在的 key 返回错误: %v", err)
	}
}

// ──── 接口满足验证 ────

// TestInMemoryKVStore_接口满足 验证 InMemoryKVStore 满足 BaseKVStore 接口。
func TestInMemoryKVStore_接口满足(t *testing.T) {
	var _ BaseKVStore = (*InMemoryKVStore)(nil)
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test -v -race ./internal/agentcore/store/kv/... -run "TestNewInMemoryKVStore|TestInMemoryKVStore_Set|TestInMemoryKVStore_Get|TestInMemoryKVStore_Exists|TestInMemoryKVStore_Delete|TestInMemoryKVStore_接口满足"`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/kv/in_memory.go internal/agentcore/store/kv/in_memory_test.go
git commit -m "feat(kv): 实现 InMemoryKVStore 核心 CRUD 方法 (Set/Get/Exists/Delete)"
```

---

### Task 3: 实现 ExclusiveSet 方法

**Files:**
- Modify: `internal/agentcore/store/kv/in_memory.go`
- Modify: `internal/agentcore/store/kv/in_memory_test.go`

- [ ] **Step 1: 在导出函数区块内 Delete 之后添加 ExclusiveSet 方法**

```go
// ExclusiveSet 原子性地设置键值对，仅当 key 不存在或已过期时成功。
// expiry 为过期秒数，0 表示不过期。
// 返回 true 表示设置成功，false 表示 key 已存在且未过期。
func (s *InMemoryKVStore) ExclusiveSet(_ context.Context, key string, value []byte, expiry int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	if e, ok := s.store[key]; ok {
		if e.expiryTs != 0 && now > e.expiryTs {
			// 已过期：允许覆盖
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
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...`
Expected: 编译成功

- [ ] **Step 3: 在 in_memory_test.go 中添加 ExclusiveSet 测试**

在文件末尾（`TestInMemoryKVStore_接口满足` 之前）添加：

```go
// ──── ExclusiveSet 测试 ────

// TestInMemoryKVStore_ExclusiveSet_新key 验证新 key 设置成功。
func TestInMemoryKVStore_ExclusiveSet_新key(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value1"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Error("ExclusiveSet 返回 false, 期望 true")
	}

	val, _ := store.Get(ctx, "key1")
	if string(val) != "value1" {
		t.Errorf("ExclusiveSet 后 Get 返回 %q, 期望 %q", string(val), "value1")
	}
}

// TestInMemoryKVStore_ExclusiveSet_已存在拒绝 验证 key 已存在且未过期时返回 false。
func TestInMemoryKVStore_ExclusiveSet_已存在拒绝(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if ok {
		t.Error("ExclusiveSet 返回 true, 期望 false")
	}

	// 原值不变
	val, _ := store.Get(ctx, "key1")
	if string(val) != "value1" {
		t.Errorf("拒绝后 Get 返回 %q, 期望 %q", string(val), "value1")
	}
}

// TestInMemoryKVStore_ExclusiveSet_已过期允许覆盖 验证 key 已过期时允许覆盖。
func TestInMemoryKVStore_ExclusiveSet_已过期允许覆盖(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	// 设置 1 秒后过期的 key
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 1)
	if !ok {
		t.Fatal("首次 ExclusiveSet 应返回 true")
	}

	// 等待过期
	time.Sleep(1100 * time.Millisecond)

	// 已过期的 key 允许覆盖
	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Error("已过期 key 的 ExclusiveSet 返回 false, 期望 true")
	}

	val, _ := store.Get(ctx, "key1")
	if string(val) != "value2" {
		t.Errorf("覆盖后 Get 返回 %q, 期望 %q", string(val), "value2")
	}
}

// TestInMemoryKVStore_ExclusiveSet_带expiry 验证带过期时间的设置。
func TestInMemoryKVStore_ExclusiveSet_带expiry(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 10)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	// 验证 key 存在
	exists, _ := store.Exists(ctx, "key1")
	if !exists {
		t.Error("设置后 Exists 返回 false, 期望 true")
	}

	// 验证内部过期时间戳已设置（通过读取 store 内部状态）
	store.mu.RLock()
	e, ok := store.store["key1"]
	store.mu.RUnlock()
	if !ok {
		t.Fatal("store 中未找到 key1")
	}
	if e.expiryTs == 0 {
		t.Error("expiryTs 为 0, 期望非零")
	}
}

// TestInMemoryKVStore_Exists_已过期 验证已过期的 key Exists 返回 false。
func TestInMemoryKVStore_Exists_已过期(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 1)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	time.Sleep(1100 * time.Millisecond)

	exists, _ := store.Exists(ctx, "key1")
	if exists {
		t.Error("过期后 Exists 返回 true, 期望 false")
	}
}

// TestInMemoryKVStore_Get_已过期 验证已过期的 key Get 返回 nil。
func TestInMemoryKVStore_Get_已过期(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 1)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	time.Sleep(1100 * time.Millisecond)

	val, _ := store.Get(ctx, "key1")
	if val != nil {
		t.Errorf("过期后 Get 返回 %v, 期望 nil", val)
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test -v -race ./internal/agentcore/store/kv/... -run "TestInMemoryKVStore_ExclusiveSet|TestInMemoryKVStore_Exists_已过期|TestInMemoryKVStore_Get_已过期"`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/kv/in_memory.go internal/agentcore/store/kv/in_memory_test.go
git commit -m "feat(kv): 实现 InMemoryKVStore ExclusiveSet 方法及过期测试"
```

---

### Task 4: 实现批量操作方法 — GetByPrefix / DeleteByPrefix / MGet / BatchDelete

**Files:**
- Modify: `internal/agentcore/store/kv/in_memory.go`
- Modify: `internal/agentcore/store/kv/in_memory_test.go`

- [ ] **Step 1: 在导出函数区块内 ExclusiveSet 之后添加批量操作方法**

```go
// GetByPrefix 获取所有以 prefix 开头的键值对，已过期的 key 返回 nil 值但不包含在结果中。
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
// batchSize 为每批删除的数量，0 表示一次性删除。
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
// batchSize 为每批删除的数量，0 表示一次性删除。
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
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...`
Expected: 编译成功

- [ ] **Step 3: 在 in_memory_test.go 中添加批量操作测试**

在文件末尾（`TestInMemoryKVStore_接口满足` 之前）添加：

```go
// ──── GetByPrefix 测试 ────

// TestInMemoryKVStore_GetByPrefix_正常 验证按前缀获取键值对。
func TestInMemoryKVStore_GetByPrefix_正常(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "user:2", []byte("bob"))
	_ = store.Set(ctx, "item:1", []byte("book"))

	result, err := store.GetByPrefix(ctx, "user:")
	if err != nil {
		t.Fatalf("GetByPrefix 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("GetByPrefix 返回 %d 条, 期望 2 条", len(result))
	}
	if string(result["user:1"]) != "alice" {
		t.Errorf("user:1 = %q, 期望 %q", string(result["user:1"]), "alice")
	}
	if string(result["user:2"]) != "bob" {
		t.Errorf("user:2 = %q, 期望 %q", string(result["user:2"]), "bob")
	}
}

// TestInMemoryKVStore_GetByPrefix_无匹配 验证无匹配前缀返回空 map。
func TestInMemoryKVStore_GetByPrefix_无匹配(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))

	result, err := store.GetByPrefix(ctx, "item:")
	if err != nil {
		t.Fatalf("GetByPrefix 返回错误: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("GetByPrefix 返回 %d 条, 期望 0 条", len(result))
	}
}

// TestInMemoryKVStore_GetByPrefix_含过期key 验证过期的 key 不包含在结果中。
func TestInMemoryKVStore_GetByPrefix_含过期key(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	ok, _ := store.ExclusiveSet(ctx, "user:2", []byte("bob"), 1)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	time.Sleep(1100 * time.Millisecond)

	result, _ := store.GetByPrefix(ctx, "user:")
	if len(result) != 1 {
		t.Errorf("GetByPrefix 返回 %d 条, 期望 1 条（过期 key 被过滤）", len(result))
	}
	if string(result["user:1"]) != "alice" {
		t.Errorf("user:1 = %q, 期望 %q", string(result["user:1"]), "alice")
	}
}

// ──── DeleteByPrefix 测试 ────

// TestInMemoryKVStore_DeleteByPrefix_一次性 验证 batchSize=0 时一次性删除。
func TestInMemoryKVStore_DeleteByPrefix_一次性(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "user:2", []byte("bob"))
	_ = store.Set(ctx, "item:1", []byte("book"))

	err := store.DeleteByPrefix(ctx, "user:", 0)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	exists, _ := store.Exists(ctx, "user:1")
	if exists {
		t.Error("删除后 user:1 仍存在")
	}
	exists, _ = store.Exists(ctx, "item:1")
	if !exists {
		t.Error("item:1 不应被删除")
	}
}

// TestInMemoryKVStore_DeleteByPrefix_分批 验证 batchSize>0 时分批删除。
func TestInMemoryKVStore_DeleteByPrefix_分批(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "user:2", []byte("bob"))
	_ = store.Set(ctx, "user:3", []byte("charlie"))

	err := store.DeleteByPrefix(ctx, "user:", 2)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	// 所有 user: 前缀的 key 都应被删除
	result, _ := store.GetByPrefix(ctx, "user:")
	if len(result) != 0 {
		t.Errorf("分批删除后仍有 %d 条 user: 前缀的 key", len(result))
	}
}

// ──── MGet 测试 ────

// TestInMemoryKVStore_MGet_正常 验证批量获取多个 key。
func TestInMemoryKVStore_MGet_正常(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))
	_ = store.Set(ctx, "k2", []byte("v2"))

	values, err := store.MGet(ctx, []string{"k1", "k2"})
	if err != nil {
		t.Fatalf("MGet 返回错误: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("MGet 返回 %d 条, 期望 2 条", len(values))
	}
	if string(values[0]) != "v1" {
		t.Errorf("values[0] = %q, 期望 %q", string(values[0]), "v1")
	}
	if string(values[1]) != "v2" {
		t.Errorf("values[1] = %q, 期望 %q", string(values[1]), "v2")
	}
}

// TestInMemoryKVStore_MGet_部分不存在 验证部分 key 不存在时对应位置为 nil。
func TestInMemoryKVStore_MGet_部分不存在(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))

	values, _ := store.MGet(ctx, []string{"k1", "k2"})
	if len(values) != 2 {
		t.Fatalf("MGet 返回 %d 条, 期望 2 条", len(values))
	}
	if string(values[0]) != "v1" {
		t.Errorf("values[0] = %q, 期望 %q", string(values[0]), "v1")
	}
	if values[1] != nil {
		t.Errorf("values[1] = %v, 期望 nil", values[1])
	}
}

// TestInMemoryKVStore_MGet_含过期 验证过期 key 对应位置为 nil。
func TestInMemoryKVStore_MGet_含过期(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))
	ok, _ := store.ExclusiveSet(ctx, "k2", []byte("v2"), 1)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	time.Sleep(1100 * time.Millisecond)

	values, _ := store.MGet(ctx, []string{"k1", "k2"})
	if string(values[0]) != "v1" {
		t.Errorf("values[0] = %q, 期望 %q", string(values[0]), "v1")
	}
	if values[1] != nil {
		t.Errorf("过期 key values[1] = %v, 期望 nil", values[1])
	}
}

// ──── BatchDelete 测试 ────

// TestInMemoryKVStore_BatchDelete_正常 验证批量删除并返回删除数量。
func TestInMemoryKVStore_BatchDelete_正常(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))
	_ = store.Set(ctx, "k2", []byte("v2"))
	_ = store.Set(ctx, "k3", []byte("v3"))

	deleted, err := store.BatchDelete(ctx, []string{"k1", "k3"}, 0)
	if err != nil {
		t.Fatalf("BatchDelete 返回错误: %v", err)
	}
	if deleted != 2 {
		t.Errorf("BatchDelete 返回 %d, 期望 2", deleted)
	}

	exists, _ := store.Exists(ctx, "k1")
	if exists {
		t.Error("k1 删除后仍存在")
	}
	exists, _ = store.Exists(ctx, "k2")
	if !exists {
		t.Error("k2 不应被删除")
	}
}

// TestInMemoryKVStore_BatchDelete_空列表 验证空列表返回 0。
func TestInMemoryKVStore_BatchDelete_空列表(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	deleted, err := store.BatchDelete(ctx, []string{}, 0)
	if err != nil {
		t.Fatalf("BatchDelete 返回错误: %v", err)
	}
	if deleted != 0 {
		t.Errorf("BatchDelete 返回 %d, 期望 0", deleted)
	}
}

// TestInMemoryKVStore_BatchDelete_分批 验证分批删除。
func TestInMemoryKVStore_BatchDelete_分批(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))
	_ = store.Set(ctx, "k2", []byte("v2"))
	_ = store.Set(ctx, "k3", []byte("v3"))

	deleted, _ := store.BatchDelete(ctx, []string{"k1", "k2", "k3"}, 2)
	if deleted != 3 {
		t.Errorf("BatchDelete 返回 %d, 期望 3", deleted)
	}

	result, _ := store.GetByPrefix(ctx, "k")
	if len(result) != 0 {
		t.Errorf("分批删除后仍有 %d 条 key", len(result))
	}
}

// TestInMemoryKVStore_BatchDelete_部分不存在 验证只删除存在的 key。
func TestInMemoryKVStore_BatchDelete_部分不存在(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))

	deleted, _ := store.BatchDelete(ctx, []string{"k1", "k2"}, 0)
	if deleted != 1 {
		t.Errorf("BatchDelete 返回 %d, 期望 1", deleted)
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test -v -race ./internal/agentcore/store/kv/... -run "TestInMemoryKVStore_GetByPrefix|TestInMemoryKVStore_DeleteByPrefix|TestInMemoryKVStore_MGet|TestInMemoryKVStore_BatchDelete"`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/kv/in_memory.go internal/agentcore/store/kv/in_memory_test.go
git commit -m "feat(kv): 实现 InMemoryKVStore 批量操作方法 (GetByPrefix/DeleteByPrefix/MGet/BatchDelete)"
```

---

### Task 5: 实现 Pipeline 方法 — Pipeline() + inMemoryPipeline 接口实现

**Files:**
- Modify: `internal/agentcore/store/kv/in_memory.go`
- Modify: `internal/agentcore/store/kv/in_memory_test.go`

- [ ] **Step 1: 在导出函数区块内 BatchDelete 之后添加 Pipeline 方法，在非导出函数区块内添加 inMemoryPipeline 的接口方法**

在导出函数区块添加：

```go
// Pipeline 创建批量操作管道。
// 闭包捕获 store 引用，Execute() 时加写锁批量执行。
func (s *InMemoryKVStore) Pipeline(_ context.Context) KVPipeline {
	return &inMemoryPipeline{
		ops: make([]operation, 0),
		exec: func(ops []operation) ([]PipelineResult, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			results := make([]PipelineResult, 0, len(ops))
			for _, op := range ops {
				switch op.op {
				case "set":
					s.store[op.key] = entry{value: op.value, expiryTs: 0}
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
```

在非导出函数区块（`getWithoutLock` 之后）添加：

```go
// Set 向管道中添加一个 Set 操作（仅记录，不立即执行）。
func (p *inMemoryPipeline) Set(_ context.Context, key string, value []byte) error {
	p.ops = append(p.ops, operation{op: "set", key: key, value: value})
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
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...`
Expected: 编译成功

- [ ] **Step 3: 在 in_memory_test.go 中添加 Pipeline 测试**

在文件末尾（`TestInMemoryKVStore_接口满足` 之前）添加：

```go
// ──── Pipeline 测试 ────

// TestInMemoryKVStore_Pipeline_混合操作 验证 Pipeline 混合 set+get+exists 操作。
func TestInMemoryKVStore_Pipeline_混合操作(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	// 先设置一个 key
	_ = store.Set(ctx, "existing", []byte("old_value"))

	// 创建 Pipeline 并执行混合操作
	pipe := store.Pipeline(ctx)
	_ = pipe.Set(ctx, "new_key", []byte("new_value"))
	_ = pipe.Get(ctx, "existing")
	_ = pipe.Exists(ctx, "nonexistent")

	results, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("Pipeline Execute 返回错误: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Pipeline 返回 %d 条结果, 期望 3 条", len(results))
	}

	// 验证 Set 结果
	if results[0].Op != "set" || results[0].Key != "new_key" {
		t.Errorf("results[0] = {Op: %q, Key: %q}, 期望 {Op: %q, Key: %q}", results[0].Op, results[0].Key, "set", "new_key")
	}

	// 验证 Get 结果
	if results[1].Op != "get" || string(results[1].Value) != "old_value" {
		t.Errorf("results[1] = {Op: %q, Value: %q}, 期望 {Op: %q, Value: %q}", results[1].Op, string(results[1].Value), "get", "old_value")
	}

	// 验证 Exists 结果
	if results[2].Op != "exists" || results[2].Exists {
		t.Errorf("results[2] = {Op: %q, Exists: %v}, 期望 {Op: %q, Exists: false}", results[2].Op, results[2].Exists, "exists")
	}

	// 验证 Pipeline Set 实际写入了 store
	val, _ := store.Get(ctx, "new_key")
	if string(val) != "new_value" {
		t.Errorf("Pipeline Set 后 Get 返回 %q, 期望 %q", string(val), "new_value")
	}
}

// TestInMemoryKVStore_Pipeline_复用 验证 Execute 后 Pipeline 可复用。
func TestInMemoryKVStore_Pipeline_复用(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	pipe := store.Pipeline(ctx)

	// 第一次使用
	_ = pipe.Set(ctx, "k1", []byte("v1"))
	results1, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("第一次 Execute 返回错误: %v", err)
	}
	if len(results1) != 1 {
		t.Errorf("第一次 Execute 返回 %d 条结果, 期望 1 条", len(results1))
	}

	// 第二次使用（复用同一个 Pipeline）
	_ = pipe.Set(ctx, "k2", []byte("v2"))
	_ = pipe.Get(ctx, "k1")
	results2, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("第二次 Execute 返回错误: %v", err)
	}
	if len(results2) != 2 {
		t.Errorf("第二次 Execute 返回 %d 条结果, 期望 2 条", len(results2))
	}

	// 验证第二次的 Get 结果
	if results2[1].Op != "get" || string(results2[1].Value) != "v1" {
		t.Errorf("复用 Pipeline Get 结果 = {Op: %q, Value: %q}, 期望 {Op: %q, Value: %q}", results2[1].Op, string(results2[1].Value), "get", "v1")
	}
}

// TestInMemoryKVStore_Pipeline_空操作 验证空 Pipeline Execute 返回空切片。
func TestInMemoryKVStore_Pipeline_空操作(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	pipe := store.Pipeline(ctx)
	results, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("空 Pipeline Execute 返回错误: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("空 Pipeline Execute 返回 %d 条结果, 期望 0 条", len(results))
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test -v -race ./internal/agentcore/store/kv/... -run "TestInMemoryKVStore_Pipeline"`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/kv/in_memory.go internal/agentcore/store/kv/in_memory_test.go
git commit -m "feat(kv): 实现 InMemoryKVStore Pipeline 方法和 inMemoryPipeline 接口"
```

---

### Task 6: 添加并发安全测试

**Files:**
- Modify: `internal/agentcore/store/kv/in_memory_test.go`

- [ ] **Step 1: 在 in_memory_test.go 中添加并发安全测试**

在文件末尾（`TestInMemoryKVStore_接口满足` 之前）添加：

```go
// ──── 并发安全测试 ────

// TestInMemoryKVStore_并发安全 验证多 goroutine 并发读写无 race。
func TestInMemoryKVStore_并发安全(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 4) // 4 组 goroutine: Set, Get, Delete, ExclusiveSet

	// 并发 Set
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("set:%d:%d", id, j)
				_ = store.Set(ctx, key, []byte("value"))
			}
		}(i)
	}

	// 并发 Get
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("set:%d:%d", id, j)
				_, _ = store.Get(ctx, key)
			}
		}(i)
	}

	// 并发 Delete
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("set:%d:%d", id, j)
				_ = store.Delete(ctx, key)
			}
		}(i)
	}

	// 并发 ExclusiveSet
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("exclusive:%d:%d", id, j)
				_, _ = store.ExclusiveSet(ctx, key, []byte("value"), 0)
			}
		}(i)
	}

	wg.Wait()
}
```

注意：需要在 in_memory_test.go 的 import 块中添加 `"fmt"` 和 `"sync"`。

完整的 import 块应为：

```go
import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)
```

- [ ] **Step 2: 运行测试验证通过（带 -race 检测）**

Run: `cd /home/opensource/uap-claw-go && go test -v -race ./internal/agentcore/store/kv/... -run "TestInMemoryKVStore_并发安全"`
Expected: PASS，无 race 检测报警

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/in_memory_test.go
git commit -m "test(kv): 添加 InMemoryKVStore 并发安全测试"
```

---

### Task 7: 更新 doc.go 包文档

**Files:**
- Modify: `internal/agentcore/store/kv/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录和包功能概述**

将 doc.go 内容替换为：

```go
// Package kv 提供键值存储的抽象接口定义和内存实现。
//
// 本包定义了所有 KV 存储后端必须满足的 BaseKVStore 接口，
// 以及用于批量操作的 KVPipeline 接口和 PipelineResult 结果类型。
// InMemoryKVStore 提供基于内存的并发安全实现，支持惰性过期检查。
// 其他后端实现（文件、数据库、Redis 等）将在后续版本中提供。
//
// 文件目录：
//
//	kv/
//	├── doc.go           # 包文档
//	├── base.go          # BaseKVStore 接口 + KVPipeline 接口 + PipelineResult 结构体
//	└── in_memory.go     # InMemoryKVStore 内存实现 + inMemoryPipeline
//
// 对应 Python 代码：openjiuwen/core/foundation/store/base_kv_store.py
//
//	InMemoryKVStore 对应: openjiuwen/core/foundation/store/kv/in_memory_kv_store.py
package kv
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/doc.go
git commit -m "docs(kv): 更新 doc.go 添加 InMemoryKVStore 文件条目"
```

---

### Task 8: 全量测试 + 覆盖率检查 + IMPLEMENTATION_PLAN.md 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 运行全量测试（带 race 检测）**

Run: `cd /home/opensource/uap-claw-go && go test -v -race ./internal/agentcore/store/kv/...`
Expected: 全部 PASS，无 race 报警

- [ ] **Step 2: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/kv/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 中步骤 4.2 的状态**

将步骤 4.2 的状态从 `☐` 改为 `✅`

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore: 更新 IMPLEMENTATION_PLAN.md 4.2 InMemoryKVStore 状态为已完成"
```
