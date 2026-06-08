# BaseKVStore 接口实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Go 版 BaseKVStore 接口和 KVPipeline 接口，对照 Python 的 `openjiuwen/core/foundation/store/base_kv_store.py`

**Architecture:** 单文件单接口方案。`internal/agentcore/store/kv/base.go` 定义 `PipelineResult` 结构体、`BaseKVStore` 接口（10 方法）、`KVPipeline` 接口（4 方法），加上 `doc.go` 包文档和 `base_test.go` 接口验证测试。

**Tech Stack:** Go 1.22+, context.Context, []byte 统一值类型

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/agentcore/store/kv/doc.go` | 包文档 |
| 创建 | `internal/agentcore/store/kv/base.go` | BaseKVStore + KVPipeline 接口 + PipelineResult 结构体 |
| 创建 | `internal/agentcore/store/kv/base_test.go` | 接口编译验证 + mock 测试 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 4.1 状态 ☐ → ✅ |

---

### Task 1: 创建 kv 包目录和 doc.go

**Files:**
- Create: `internal/agentcore/store/kv/doc.go`

- [ ] **Step 1: 创建包目录**

Run: `mkdir -p internal/agentcore/store/kv`

- [ ] **Step 2: 编写 doc.go**

```go
// Package kv 提供键值存储的抽象接口定义和批量操作管道。
//
// 本包定义了所有 KV 存储后端必须满足的 BaseKVStore 接口，
// 以及用于批量操作的 KVPipeline 接口和 PipelineResult 结果类型。
// 具体实现（内存、文件、数据库、Redis 等）在各子文件中提供。
//
// 文件目录：
//
//	kv/
//	├── doc.go           # 包文档
//	└── base.go          # BaseKVStore 接口 + KVPipeline 接口 + PipelineResult 结构体
//
// 对应 Python 代码：openjiuwen/core/foundation/store/base_kv_store.py
package kv
```

- [ ] **Step 3: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...`
Expected: 编译成功，无错误

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/kv/doc.go
git commit -m "feat(store): 添加 kv 包 doc.go 包文档"
```

---

### Task 2: 编写 base.go 接口定义（先写测试）

**Files:**
- Create: `internal/agentcore/store/kv/base_test.go`
- Create: `internal/agentcore/store/kv/base.go`

- [ ] **Step 1: 编写接口编译验证测试（fake 实现）**

创建 `internal/agentcore/store/kv/base_test.go`，写一个 fake 实现来验证接口定义可编译、方法签名正确：

```go
package kv

import (
	"context"
)

// fakeKVStore 用于验证 BaseKVStore 接口可被实现。
type fakeKVStore struct{}

func (f *fakeKVStore) Set(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (f *fakeKVStore) ExclusiveSet(_ context.Context, _ string, _ []byte, _ int) (bool, error) {
	return false, nil
}

func (f *fakeKVStore) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func (f *fakeKVStore) Exists(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (f *fakeKVStore) Delete(_ context.Context, _ string) error {
	return nil
}

func (f *fakeKVStore) GetByPrefix(_ context.Context, _ string) (map[string][]byte, error) {
	return nil, nil
}

func (f *fakeKVStore) DeleteByPrefix(_ context.Context, _ string, _ int) error {
	return nil
}

func (f *fakeKVStore) MGet(_ context.Context, _ []string) ([][]byte, error) {
	return nil, nil
}

func (f *fakeKVStore) BatchDelete(_ context.Context, _ []string, _ int) (int, error) {
	return 0, nil
}

func (f *fakeKVStore) Pipeline(_ context.Context) KVPipeline {
	return &fakeKVPipeline{}
}

// fakeKVPipeline 用于验证 KVPipeline 接口可被实现。
type fakeKVPipeline struct{}

func (f *fakeKVPipeline) Set(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (f *fakeKVPipeline) Get(_ context.Context, _ string) error {
	return nil
}

func (f *fakeKVPipeline) Exists(_ context.Context, _ string) error {
	return nil
}

func (f *fakeKVPipeline) Execute(_ context.Context) ([]PipelineResult, error) {
	return nil, nil
}
```

- [ ] **Step 2: 运行测试，验证编译失败（接口尚未定义）**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/kv/...`
Expected: FAIL — BaseKVStore、KVPipeline、PipelineResult 未定义

- [ ] **Step 3: 编写 base.go 接口定义**

创建 `internal/agentcore/store/kv/base.go`：

```go
package kv

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// PipelineResult Pipeline 操作的执行结果。
//
// 调用者根据 Op 字段判断应访问哪个字段：
//   - Op 为 "set"：仅检查 Err 是否为 nil
//   - Op 为 "get"：通过 Value 获取返回值（key 不存在时 Value 为 nil）
//   - Op 为 "exists"：通过 Exists 获取布尔结果
type PipelineResult struct {
	// Op 操作类型："set"、"get"、"exists"
	Op string
	// Key 操作的键
	Key string
	// Value Get 操作返回的值，仅 Op 为 "get" 时有效
	Value []byte
	// Exists Exists 操作的结果，仅 Op 为 "exists" 时有效
	Exists bool
	// Err 操作执行错误，nil 表示成功
	Err error
}

// ──────────────────────────── 接口 ────────────────────────────

// BaseKVStore 键值存储后端的抽象接口。
//
// 所有 KV 存储后端（内存、文件、数据库、Redis 等）必须实现此接口。
// 插件开发者可直接实现此接口，调用方通过直接导入和实例化使用。
//
// 对应 Python: openjiuwen/core/foundation/store/base_kv_store.py (BaseKVStore)
type BaseKVStore interface {
	// Set 存储或覆盖一个键值对。
	Set(ctx context.Context, key string, value []byte) error

	// ExclusiveSet 原子性地设置键值对，仅当 key 不存在时成功。
	// expiry 为过期秒数，0 表示不过期。
	// 返回 true 表示设置成功，false 表示 key 已存在。
	ExclusiveSet(ctx context.Context, key string, value []byte, expiry int) (bool, error)

	// Get 根据 key 获取值，key 不存在时返回 nil, nil。
	Get(ctx context.Context, key string) ([]byte, error)

	// Exists 检查 key 是否存在。
	Exists(ctx context.Context, key string) (bool, error)

	// Delete 删除指定 key，key 不存在时不执行操作。
	Delete(ctx context.Context, key string) error

	// GetByPrefix 获取所有以 prefix 开头的键值对。
	GetByPrefix(ctx context.Context, prefix string) (map[string][]byte, error)

	// DeleteByPrefix 删除所有以 prefix 开头的键值对。
	// batchSize 为每批删除的数量，0 表示一次性删除。
	DeleteByPrefix(ctx context.Context, prefix string, batchSize int) error

	// MGet 批量获取多个 key 的值。
	// 返回值与输入 keys 顺序对应，不存在的 key 对应位置为 nil。
	MGet(ctx context.Context, keys []string) ([][]byte, error)

	// BatchDelete 批量删除多个 key，返回成功删除的数量。
	// batchSize 为每批删除的数量，0 表示一次性删除。
	BatchDelete(ctx context.Context, keys []string, batchSize int) (int, error)

	// Pipeline 创建批量操作管道，用于减少网络往返。
	Pipeline(ctx context.Context) KVPipeline
}

// KVPipeline 批量操作管道接口。
//
// 用于收集多个操作后一次性提交执行，减少网络往返。
// 使用方式：
//
//	p := store.Pipeline(ctx)
//	p.Set(ctx, "k1", []byte("v1"))
//	p.Get(ctx, "k2")
//	p.Exists(ctx, "k3")
//	results, err := p.Execute(ctx)
//
// 对应 Python: openjiuwen/core/foundation/store/base_kv_store.py (BasedKVStorePipeline)
type KVPipeline interface {
	// Set 向管道中添加一个 Set 操作（仅记录，不立即执行）。
	Set(ctx context.Context, key string, value []byte) error

	// Get 向管道中添加一个 Get 操作（仅记录，不立即执行）。
	Get(ctx context.Context, key string) error

	// Exists 向管道中添加一个 Exists 操作（仅记录，不立即执行）。
	Exists(ctx context.Context, key string) error

	// Execute 提交并执行管道中的所有操作，返回各操作的结果。
	// 执行后管道被清空，可复用。
	Execute(ctx context.Context) ([]PipelineResult, error)
}
```

- [ ] **Step 4: 运行测试，验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/kv/...`
Expected: PASS — fake 实现满足接口，编译通过

- [ ] **Step 5: 补充接口行为验证测试**

在 `base_test.go` 末尾追加：

```go
// ──── 接口编译验证测试 ────

// TestBaseKVStore_接口满足 验证 fakeKVStore 满足 BaseKVStore 接口。
func TestBaseKVStore_接口满足(t *testing.T) {
	var _ BaseKVStore = (*fakeKVStore)(nil)
}

// TestKVPipeline_接口满足 验证 fakeKVPipeline 满足 KVPipeline 接口。
func TestKVPipeline_接口满足(t *testing.T) {
	var _ KVPipeline = (*fakeKVPipeline)(nil)
}

// TestPipelineResult_字段 验证 PipelineResult 结构体字段可赋值。
func TestPipelineResult_字段(t *testing.T) {
	result := PipelineResult{
		Op:     "get",
		Key:    "test_key",
		Value:  []byte("test_value"),
		Exists: false,
		Err:    nil,
	}
	if result.Op != "get" {
		t.Errorf("Op = %q, 期望 %q", result.Op, "get")
	}
	if result.Key != "test_key" {
		t.Errorf("Key = %q, 期望 %q", result.Key, "test_key")
	}
	if string(result.Value) != "test_value" {
		t.Errorf("Value = %q, 期望 %q", string(result.Value), "test_value")
	}
	if result.Exists != false {
		t.Errorf("Exists = %v, 期望 false", result.Exists)
	}
	if result.Err != nil {
		t.Errorf("Err = %v, 期望 nil", result.Err)
	}
}

// TestPipelineResult_Set操作 验证 Set 操作的结果结构。
func TestPipelineResult_Set操作(t *testing.T) {
	result := PipelineResult{
		Op:  "set",
		Key: "mykey",
		Err: nil,
	}
	if result.Op != "set" {
		t.Errorf("Op = %q, 期望 %q", result.Op, "set")
	}
}

// TestPipelineResult_Exists操作 验证 Exists 操作的结果结构。
func TestPipelineResult_Exists操作(t *testing.T) {
	result := PipelineResult{
		Op:     "exists",
		Key:    "mykey",
		Exists: true,
		Err:    nil,
	}
	if !result.Exists {
		t.Error("Exists 应为 true")
	}
}

// TestPipelineResult_错误结果 验证带错误的 PipelineResult。
func TestPipelineResult_错误结果(t *testing.T) {
	result := PipelineResult{
		Op:  "get",
		Key: "missing_key",
		Err: context.Canceled,
	}
	if result.Err == nil {
		t.Error("Err 不应为 nil")
	}
}
```

- [ ] **Step 6: 运行全部测试**

Run: `cd /home/opensource/uap-claw-go && go test -v ./internal/agentcore/store/kv/...`
Expected: 所有测试 PASS

- [ ] **Step 7: 检查测试覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/kv/...`
Expected: 覆盖率达标（本包为接口定义，测试验证接口编译 + 结构体字段，覆盖率合理即可）

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/store/kv/base.go internal/agentcore/store/kv/base_test.go
git commit -m "feat(store): 实现 BaseKVStore 和 KVPipeline 接口定义"
```

---

### Task 3: 全量回归测试 + 更新实现计划

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 全量编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 全量测试回归**

Run: `cd /home/opensource/uap-claw-go && make test`
Expected: 所有测试 PASS

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 中 4.1 状态**

将 `4.1 | ☐` 改为 `4.1 | ✅`

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 4.1 BaseKVStore 接口状态为已完成"
```
