# BaseDbStore 接口实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Go 版 BaseDbStore 接口，对照 Python 的 `openjiuwen/core/foundation/store/base_db_store.py`，为后续记忆系统（SqlDbStore、SqlMessageStore 等）提供数据库连接的依赖注入点。

**Architecture:** 单文件接口方案。`internal/agentcore/store/db/base.go` 定义 `BaseDbStore` 接口（唯一方法 `GetDB(ctx) *gorm.DB`），加上 `doc.go` 包文档和 `base_test.go` 接口验证测试。与 kv/base.go、vector/base.go 的模式完全一致。

**Tech Stack:** Go 1.22+, context.Context, gorm.io/gorm

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/agentcore/store/db/doc.go` | 包文档 |
| 创建 | `internal/agentcore/store/db/base.go` | BaseDbStore 接口定义 |
| 创建 | `internal/agentcore/store/db/base_test.go` | 接口编译验证 + mock 测试 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 4.12 状态 ☐ → ✅ |

---

### Task 1: 创建 db 包目录和 doc.go

**Files:**
- Create: `internal/agentcore/store/db/doc.go`

- [ ] **Step 1: 创建包目录**

Run: `mkdir -p internal/agentcore/store/db`

- [ ] **Step 2: 编写 doc.go**

```go
// Package db 提供 SQL 数据库的抽象接口定义。
//
// 本包定义了数据库连接的依赖注入接口 BaseDbStore，
// 让上层组件（SqlDbStore、SqlMessageStore 等）通过接口获取数据库连接，
// 而非直接依赖具体引擎。BaseDbStore 本身不提供任何数据存储能力，
// 唯一的职责是暴露 *gorm.DB 实例。
//
// 文件目录：
//
//	db/
//	├── doc.go    # 包文档
//	└── base.go   # BaseDbStore 接口定义
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/base_db_store.py
//
// 核心类型/接口索引：
//
//	BaseDbStore — SQL 数据库连接抽象接口，通过 GetDB 返回 *gorm.DB 实例
package db
```

- [ ] **Step 3: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/db/...`
Expected: 编译成功，无错误

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/db/doc.go
git commit -m "feat(store): 添加 db 包 doc.go 包文档"
```

---

### Task 2: 编写 base.go 接口定义（先写测试）

**Files:**
- Create: `internal/agentcore/store/db/base_test.go`
- Create: `internal/agentcore/store/db/base.go`

- [ ] **Step 1: 编写接口编译验证测试（fake 实现）**

创建 `internal/agentcore/store/db/base_test.go`，写一个 fake 实现来验证接口定义可编译、方法签名正确：

```go
package db

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeDbStore 用于验证 BaseDbStore 接口可被实现。
type fakeDbStore struct {
	db *gorm.DB
}

func (f *fakeDbStore) GetDB(_ context.Context) *gorm.DB {
	return f.db
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──── 接口编译验证测试 ────

// TestBaseDbStore_接口满足 验证 fakeDbStore 满足 BaseDbStore 接口。
func TestBaseDbStore_接口满足(t *testing.T) {
	var _ BaseDbStore = (*fakeDbStore)(nil)
}

// TestFakeDbStore_GetDB 验证 fakeDbStore.GetDB 返回预期的 *gorm.DB 实例。
func TestFakeDbStore_GetDB(t *testing.T) {
	ctx := context.Background()
	store := &fakeDbStore{db: nil}
	result := store.GetDB(ctx)
	if result != nil {
		t.Errorf("GetDB 返回 %v, 期望 nil", result)
	}
}

// TestBaseDbStore_接口方法签名 验证接口方法签名与预期一致。
func TestBaseDbStore_接口方法签名(t *testing.T) {
	// 通过 fake 实现调用，确保 GetDB 接受 context.Context 参数并返回 *gorm.DB
	store := &fakeDbStore{db: nil}
	ctx := context.Background()
	db := store.GetDB(ctx)
	_ = db // db 为 nil，仅验证方法签名正确
}
```

- [ ] **Step 2: 运行测试，验证编译失败（接口尚未定义）**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/...`
Expected: FAIL — BaseDbStore 未定义

- [ ] **Step 3: 编写 base.go 接口定义**

创建 `internal/agentcore/store/db/base.go`：

```go
package db

import (
	"context"

	"gorm.io/gorm"
)

// ──────────────────────────── 接口 ────────────────────────────

// BaseDbStore SQL 数据库抽象接口，提供数据库引擎访问。
//
// 本接口是数据库连接的依赖注入点，让上层组件（SqlDbStore、SqlMessageStore 等）
// 通过接口获取数据库连接，而非直接依赖具体引擎。
// BaseDbStore 与 BaseKVStore、BaseVectorStore 是平级关系，互不包含：
//   - BaseKVStore 负责键值对存储
//   - BaseVectorStore 负责向量存储与检索
//   - BaseDbStore 负责提供数据库连接
//
// 对应 Python: openjiuwen/core/foundation/store/base_db_store.py (BaseDbStore)
type BaseDbStore interface {
	// GetDB 返回 GORM 数据库实例，调用者可使用返回值执行数据库操作。
	//
	// 对应 Python: BaseDbStore.get_async_engine() -> AsyncEngine
	GetDB(ctx context.Context) *gorm.DB
}
```

- [ ] **Step 4: 运行测试，验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/...`
Expected: PASS — fake 实现满足接口，编译通过

- [ ] **Step 5: 运行测试，验证测试通过**

Run: `cd /home/opensource/uap-claw-go && go test -v ./internal/agentcore/store/db/...`
Expected: 所有测试 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/store/db/base.go internal/agentcore/store/db/base_test.go
git commit -m "feat(store): 实现 BaseDbStore 接口定义"
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

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 中 4.12 状态**

将 `| 4.12 | ☐ | BaseDbStore 接口 | SQL 数据库抽象 |` 改为 `| 4.12 | ✅ | BaseDbStore 接口 | SQL 数据库抽象 |`

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 4.12 BaseDbStore 接口状态为已完成"
```
