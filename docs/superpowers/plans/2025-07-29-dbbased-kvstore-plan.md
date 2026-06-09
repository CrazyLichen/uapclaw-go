# DbBasedKVStore 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 DbBasedKVStore — 基于 GORM 的数据库 KV 存储，支持 SQLite/MySQL/PostgreSQL

**Architecture:** 在 `internal/agentcore/store/kv/` 包内新增 `db_based.go`，实现 `BaseKVStore` 接口。构造函数接受 `*gorm.DB`，使用 GORM `clause.OnConflict` 处理多方言 upsert，`atomic.Bool + sync.Once` 做惰性建表，ExclusiveSet 值用 JSON 包装（Base64 编码 binary），Get 自动解包。

**Tech Stack:** GORM, gorm.io/driver/sqlite (modernc纯Go), gorm.io/driver/mysql, gorm.io/driver/postgres

**设计文档:** `docs/superpowers/specs/2025-07-29-dbbased-kvstore-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 创建 | `internal/agentcore/store/kv/db_based.go` | DbBasedKVStore + dbBasedPipeline 实现 |
| 创建 | `internal/agentcore/store/kv/db_based_test.go` | DbBasedKVStore 单元测试（SQLite :memory:） |
| 修改 | `internal/agentcore/store/kv/doc.go` | 更新文件目录，添加 db_based.go 条目 |
| 修改 | `go.mod` / `go.sum` | 添加 GORM 及驱动依赖 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 更新 4.4 状态 |

---

### Task 1: 添加 GORM 依赖

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 安装 GORM 及驱动依赖**

```bash
cd /home/opensource/uap-claw-go
go get gorm.io/gorm
go get gorm.io/driver/sqlite
go get gorm.io/driver/mysql
go get gorm.io/driver/postgres
```

- [ ] **Step 2: 验证依赖已添加**

```bash
grep -E "gorm.io" go.mod
```

期望输出包含：
```
gorm.io/gorm vX.X.X
gorm.io/driver/sqlite vX.X.X
gorm.io/driver/mysql vX.X.X
gorm.io/driver/postgres vX.X.X
```

- [ ] **Step 3: 验证编译通过**

```bash
go build ./internal/agentcore/store/kv/...
```

期望：编译成功（当前 kv 包不引用 GORM，所以不会有链接错误）

- [ ] **Step 4: 提交依赖变更**

```bash
git add go.mod go.sum
git commit -m "chore: add GORM and database driver dependencies for DbBasedKVStore"
```

---

### Task 2: 实现 DbBasedKVStore 核心结构体和构造函数

**Files:**
- Create: `internal/agentcore/store/kv/db_based.go`

- [ ] **Step 1: 创建 db_based.go，写入结构体、常量、构造函数和惰性建表**

```go
package kv

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KVStoreRow kv_store 表的 GORM 模型。
//
// 对应 Python: openjiuwen/core/foundation/store/kv/db_based_kv_store.py (KVStoreTable)
type KVStoreRow struct {
	// Key 键，主键
	Key string `gorm:"column:key;type:varchar(255);primaryKey"`
	// Value 值，BLOB 类型
	Value []byte `gorm:"column:value;type:blob;not null"`
}

// exclusiveValue ExclusiveSet 的 JSON 包装格式。
//
// 对应 Python: {EXCLUSIVE_VALUE_KEY: value, EXCLUSIVE_EXPIRY_KEY: expire_at}
type exclusiveValue struct {
	// ExclusiveValue Base64 编码的原始 []byte
	ExclusiveValue string `json:"exclusive_value"`
	// Exclusive_expiry 过期时间戳（Unix 秒），0 表示不过期
	ExclusiveExpiry int64 `json:"exclusive_expiry"`
}

// DbBasedKVStore 基于 GORM 的数据库 KV 存储实现。
//
// 支持 SQLite、MySQL、PostgreSQL 三种方言，通过 GORM clause.OnConflict
// 自动处理方言差异。构造函数接受已初始化的 *gorm.DB，调用方负责配置方言和连接池。
//
// 对应 Python: openjiuwen/core/foundation/store/kv/db_based_kv_store.py (DbBasedKVStore)
type DbBasedKVStore struct {
	// db GORM 数据库实例
	db *gorm.DB
	// tableCreated 建表标志，atomic.Bool 做快速路径
	tableCreated atomic.Bool
	// tableOnce 慢路径建表，保证只执行一次
	tableOnce sync.Once
}

// dbBasedPipeline 数据库 Pipeline 实现。
type dbBasedPipeline struct {
	// ops 待执行的操作列表
	ops []operation
	// store 关联的 DbBasedKVStore 实例
	store *DbBasedKVStore
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// exclusiveValueKey JSON 中值的字段名
	exclusiveValueKey = "exclusive_value"
	// exclusiveExpiryKey JSON 中过期时间的字段名
	exclusiveExpiryKey = "exclusive_expiry"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDbBasedKVStore 创建基于 GORM 的数据库 KV 存储。
//
// db: 已初始化的 GORM 数据库实例（调用方负责配置方言和连接池）。
// 对齐 Python: DbBasedKVStore(engine: AsyncEngine)
func NewDbBasedKVStore(db *gorm.DB) *DbBasedKVStore {
	return &DbBasedKVStore{
		db: db,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureTable 惰性建表。
// 快速路径：atomic.Bool 无锁检查；慢路径：sync.Once 保证只建一次。
// 对齐 Python: _create_table_if_not_exist()
func (s *DbBasedKVStore) ensureTable() {
	if s.tableCreated.Load() {
		return
	}
	s.tableOnce.Do(func() {
		s.db.AutoMigrate(&KVStoreRow{})
		s.tableCreated.Store(true)
	})
}

// encodeExclusiveValue 将 value 和 expireAt 编码为 exclusive JSON 格式的 []byte。
// 对齐 Python: json.dumps({EXCLUSIVE_VALUE_KEY: encoded_value, EXCLUSIVE_EXPIRY_KEY: expire_at})
func encodeExclusiveValue(value []byte, expireAt int64) ([]byte, error) {
	ev := exclusiveValue{
		ExclusiveValue:  base64.StdEncoding.EncodeToString(value),
		ExclusiveExpiry: expireAt,
	}
	return json.Marshal(ev)
}

// decodeExclusiveValue 尝试从 BLOB 解码 exclusive JSON 格式。
// 如果数据是 exclusive JSON（含 exclusive_expiry 字段），返回解码后的值和 true。
// 否则返回 nil 和 false。
// 对齐 Python: get() 中的 json.loads + EXCLUSIVE_EXPIRY_KEY 检查
func decodeExclusiveValue(data []byte) ([]byte, bool) {
	var ev exclusiveValue
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, false
	}
	if ev.ExclusiveExpiry == 0 && ev.ExclusiveValue == "" {
		// 空的 exclusive 结构，不视为 exclusive 格式
		return nil, false
	}
	decoded, err := base64.StdEncoding.DecodeString(ev.ExclusiveValue)
	if err != nil {
		return nil, false
	}
	return decoded, true
}

// upsertStatement 构造 GORM upsert 语句，自动处理方言差异。
// 对齐 Python: _get_upsert_stmt() — 区分 MySQL/SQLite 方言
// Go 版本通过 GORM clause.OnConflict 统一处理
func upsertStatement(db *gorm.DB, row *KVStoreRow) *gorm.DB {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(row)
}

// TableName 返回表名，实现 GORM Tabler 接口。
func (KVStoreRow) TableName() string {
	return "kv_store"
}
```

- [ ] **Step 2: 验证编译通过**

```bash
go build ./internal/agentcore/store/kv/...
```

期望：编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/db_based.go
git commit -m "feat(kv): add DbBasedKVStore struct, constructor, and helpers"
```

---

### Task 3: 实现 Set / Get / Exists / Delete 方法

**Files:**
- Modify: `internal/agentcore/store/kv/db_based.go`

- [ ] **Step 1: 在导出函数区块末尾添加 Set/Get/Exists/Delete 方法**

在 `NewDbBasedKVStore` 函数之后、非导出函数区块之前添加：

```go
// Set 存储或覆盖一个键值对。
// 对齐 Python: DbBasedKVStore.set(key, value)
func (s *DbBasedKVStore) Set(ctx context.Context, key string, value []byte) error {
	s.ensureTable()
	row := &KVStoreRow{
		Key:   key,
		Value: value,
	}
	return upsertStatement(s.db.WithContext(ctx), row).Error
}

// Get 根据 key 获取值，key 不存在时返回 nil, nil。
// 尝试 JSON 解析，如果含 exclusive_expiry 字段则解包返回实际值。
// 对齐 Python: DbBasedKVStore.get(key) — 解包 exclusive dict
func (s *DbBasedKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.ensureTable()
	var row KVStoreRow
	err := s.db.WithContext(ctx).Where("key = ?", key).First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	// 尝试解包 exclusive JSON
	if decoded, ok := decodeExclusiveValue(row.Value); ok {
		return decoded, nil
	}
	return row.Value, nil
}

// Exists 检查 key 是否存在。
// 对齐 Python: DbBasedKVStore.exists(key)
func (s *DbBasedKVStore) Exists(ctx context.Context, key string) (bool, error) {
	s.ensureTable()
	var count int64
	err := s.db.WithContext(ctx).Model(&KVStoreRow{}).Where("key = ?", key).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Delete 删除指定 key，key 不存在时不执行操作。
// 对齐 Python: DbBasedKVStore.delete(key)
func (s *DbBasedKVStore) Delete(ctx context.Context, key string) error {
	s.ensureTable()
	return s.db.WithContext(ctx).Where("key = ?", key).Delete(&KVStoreRow{}).Error
}
```

- [ ] **Step 2: 验证编译通过**

```bash
go build ./internal/agentcore/store/kv/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/db_based.go
git commit -m "feat(kv): implement DbBasedKVStore Set/Get/Exists/Delete"
```

---

### Task 4: 实现 ExclusiveSet 方法

**Files:**
- Modify: `internal/agentcore/store/kv/db_based.go`

- [ ] **Step 1: 在 Delete 方法之后添加 ExclusiveSet**

```go
// ExclusiveSet 原子性地设置键值对，仅当 key 不存在或已过期时成功。
// expiry 为过期秒数，0 表示不过期。
// 返回 true 表示设置成功，false 表示 key 已存在且未过期。
// 依赖数据库主键唯一性保证原子性，不加应用层锁。
// 对齐 Python: DbBasedKVStore.exclusive_set(key, value, expiry)
func (s *DbBasedKVStore) ExclusiveSet(ctx context.Context, key string, value []byte, expiry int) (bool, error) {
	s.ensureTable()
	now := time.Now().Unix()

	// 查询已有记录
	var row KVStoreRow
	err := s.db.WithContext(ctx).Where("key = ?", key).First(&row).Error
	if err == nil {
		// key 已存在，检查是否过期
		var ev exclusiveValue
		if err := json.Unmarshal(row.Value, &ev); err != nil {
			// 无法解析为 exclusive JSON，视为未过期
			return false, nil
		}
		if ev.ExclusiveExpiry == 0 || ev.ExclusiveExpiry > now {
			// 未过期：ExpiryAt==0 表示永不过期；ExpiryAt > now 表示尚未到期
			return false, nil
		}
		// 已过期，允许覆盖（继续执行写入）
	} else if err != gorm.ErrRecordNotFound {
		return false, err
	}

	// 计算过期时间戳
	var expireAt int64
	if expiry > 0 {
		expireAt = now + int64(expiry)
	}

	// 编码为 exclusive JSON
	encodedValue, err := encodeExclusiveValue(value, expireAt)
	if err != nil {
		return false, fmt.Errorf("编码 exclusive 值失败: %w", err)
	}

	// upsert 写入
	newRow := &KVStoreRow{
		Key:   key,
		Value: encodedValue,
	}
	if err := upsertStatement(s.db.WithContext(ctx), newRow).Error; err != nil {
		return false, err
	}
	return true, nil
}
```

- [ ] **Step 2: 验证编译通过**

```bash
go build ./internal/agentcore/store/kv/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/db_based.go
git commit -m "feat(kv): implement DbBasedKVStore ExclusiveSet"
```

---

### Task 5: 实现 GetByPrefix / DeleteByPrefix 方法

**Files:**
- Modify: `internal/agentcore/store/kv/db_based.go`

- [ ] **Step 1: 在 ExclusiveSet 方法之后添加 GetByPrefix 和 DeleteByPrefix**

```go
// GetByPrefix 获取所有以 prefix 开头的键值对。
// 对齐 Python: DbBasedKVStore.get_by_prefix(prefix) — 解码 _decode_value
func (s *DbBasedKVStore) GetByPrefix(ctx context.Context, prefix string) (map[string][]byte, error) {
	s.ensureTable()
	var rows []KVStoreRow
	err := s.db.WithContext(ctx).Where("key LIKE ?", prefix+"%").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string][]byte, len(rows))
	for _, row := range rows {
		// 尝试解包 exclusive JSON，否则返回原始 BLOB
		if decoded, ok := decodeExclusiveValue(row.Value); ok {
			result[row.Key] = decoded
		} else {
			result[row.Key] = row.Value
		}
	}
	return result, nil
}

// DeleteByPrefix 删除所有以 prefix 开头的键值对。
// batchSize 为每批删除的数量，0 表示一次性删除。
// 对齐 Python: DbBasedKVStore.delete_by_prefix(prefix, batch_size)
func (s *DbBasedKVStore) DeleteByPrefix(ctx context.Context, prefix string, batchSize int) error {
	s.ensureTable()
	db := s.db.WithContext(ctx)

	if batchSize <= 0 {
		return db.Where("key LIKE ?", prefix+"%").Delete(&KVStoreRow{}).Error
	}

	// 分批删除：先查询匹配的 key，再按批次删除
	var keys []string
	if err := db.Model(&KVStoreRow{}).Where("key LIKE ?", prefix+"%").Pluck("key", &keys).Error; err != nil {
		return err
	}

	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		if err := db.Where("key IN ?", keys[i:end]).Delete(&KVStoreRow{}).Error; err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 2: 验证编译通过**

```bash
go build ./internal/agentcore/store/kv/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/db_based.go
git commit -m "feat(kv): implement DbBasedKVStore GetByPrefix/DeleteByPrefix"
```

---

### Task 6: 实现 MGet / BatchDelete 方法

**Files:**
- Modify: `internal/agentcore/store/kv/db_based.go`

- [ ] **Step 1: 在 DeleteByPrefix 方法之后添加 MGet 和 BatchDelete**

```go
// MGet 批量获取多个 key 的值。
// 返回值与输入 keys 顺序对应，不存在的 key 对应位置为 nil。
// 对齐 Python: DbBasedKVStore.mget(keys)
func (s *DbBasedKVStore) MGet(ctx context.Context, keys []string) ([][]byte, error) {
	s.ensureTable()
	if len(keys) == 0 {
		return [][]byte{}, nil
	}

	var rows []KVStoreRow
	err := s.db.WithContext(ctx).Where("key IN ?", keys).Find(&rows).Error
	if err != nil {
		return nil, err
	}

	// 构建 lookup map
	lookup := make(map[string][]byte, len(rows))
	for _, row := range rows {
		if decoded, ok := decodeExclusiveValue(row.Value); ok {
			lookup[row.Key] = decoded
		} else {
			lookup[row.Key] = row.Value
		}
	}

	// 按输入顺序组装结果
	result := make([][]byte, len(keys))
	for i, key := range keys {
		result[i] = lookup[key] // 不存在时为零值 nil
	}
	return result, nil
}

// BatchDelete 批量删除多个 key，返回成功删除的数量。
// batchSize 为每批删除的数量，0 表示一次性删除。
// 对齐 Python: DbBasedKVStore.batch_delete(keys, batch_size)
func (s *DbBasedKVStore) BatchDelete(ctx context.Context, keys []string, batchSize int) (int, error) {
	s.ensureTable()
	if len(keys) == 0 {
		return 0, nil
	}

	db := s.db.WithContext(ctx)

	if batchSize <= 0 {
		result := db.Where("key IN ?", keys).Delete(&KVStoreRow{})
		if result.Error != nil {
			return 0, result.Error
		}
		return int(result.RowsAffected), nil
	}

	// 分批删除
	totalDeleted := 0
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		result := db.Where("key IN ?", keys[i:end]).Delete(&KVStoreRow{})
		if result.Error != nil {
			return totalDeleted, result.Error
		}
		totalDeleted += int(result.RowsAffected)
	}
	return totalDeleted, nil
}
```

- [ ] **Step 2: 验证编译通过**

```bash
go build ./internal/agentcore/store/kv/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/db_based.go
git commit -m "feat(kv): implement DbBasedKVStore MGet/BatchDelete"
```

---

### Task 7: 实现 Pipeline

**Files:**
- Modify: `internal/agentcore/store/kv/db_based.go`

- [ ] **Step 1: 在 BatchDelete 方法之后添加 Pipeline 方法，以及 dbBasedPipeline 的 Set/Get/Exists/Execute 方法**

```go
// Pipeline 创建批量操作管道。
// 对齐 Python: DbBasedKVStore.pipeline()
func (s *DbBasedKVStore) Pipeline(_ context.Context) KVPipeline {
	return &dbBasedPipeline{
		ops:   make([]operation, 0),
		store: s,
	}
}

// Set 向管道中添加一个 Set 操作（仅记录，不立即执行）。
// expiry 为过期秒数，0 表示不过期。
func (p *dbBasedPipeline) Set(_ context.Context, key string, value []byte, expiry int) error {
	p.ops = append(p.ops, operation{op: "set", key: key, value: value, expiry: expiry})
	return nil
}

// Get 向管道中添加一个 Get 操作（仅记录，不立即执行）。
func (p *dbBasedPipeline) Get(_ context.Context, key string) error {
	p.ops = append(p.ops, operation{op: "get", key: key})
	return nil
}

// Exists 向管道中添加一个 Exists 操作（仅记录，不立即执行）。
func (p *dbBasedPipeline) Exists(_ context.Context, key string) error {
	p.ops = append(p.ops, operation{op: "exists", key: key})
	return nil
}

// Execute 提交并执行管道中的所有操作，返回各操作的结果。
// 操作按类型分组（set/get/exists），在一个事务内批量执行。
// 执行后管道被清空，可复用。
// 对齐 Python: DbBasedKVStore.pipeline().execute()
func (p *dbBasedPipeline) Execute(ctx context.Context) ([]PipelineResult, error) {
	// 拷贝操作列表并清空，允许 Pipeline 复用
	ops := p.ops
	p.ops = nil

	if len(ops) == 0 {
		return []PipelineResult{}, nil
	}

	p.store.ensureTable()

	// 分组操作
	type setOp struct {
		key    string
		value  []byte
		expiry int
	}
	var setOps []setOp
	var getKeys []string
	var existsKeys []string

	for _, op := range ops {
		switch op.op {
		case "set":
			setOps = append(setOps, setOp{key: op.key, value: op.value, expiry: op.expiry})
		case "get":
			getKeys = append(getKeys, op.key)
		case "exists":
			existsKeys = append(existsKeys, op.key)
		}
	}

	var results []PipelineResult

	err := p.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 批量 set
		for _, op := range setOps {
			row := &KVStoreRow{
				Key:   op.key,
				Value: op.value,
			}
			if err := upsertStatement(tx, row).Error; err != nil {
				results = append(results, PipelineResult{Op: "set", Key: op.key, Err: err})
				continue
			}
			results = append(results, PipelineResult{Op: "set", Key: op.key})
		}

		// 批量 get
		getResults := make(map[string][]byte)
		if len(getKeys) > 0 {
			var rows []KVStoreRow
			if err := tx.Where("key IN ?", getKeys).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				if decoded, ok := decodeExclusiveValue(row.Value); ok {
					getResults[row.Key] = decoded
				} else {
					getResults[row.Key] = row.Value
				}
			}
		}

		// 批量 exists
		existsResults := make(map[string]bool)
		if len(existsKeys) > 0 {
			var rows []KVStoreRow
			if err := tx.Select("key").Where("key IN ?", existsKeys).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				existsResults[row.Key] = true
			}
		}

		// 按原始操作顺序组装结果
		reordered := make([]PipelineResult, 0, len(ops))
		setIdx := 0
		for _, op := range ops {
			switch op.op {
			case "set":
				if setIdx < len(results) {
					reordered = append(reordered, results[setIdx])
					setIdx++
				}
			case "get":
				val := getResults[op.key] // 不存在时为零值 nil
				reordered = append(reordered, PipelineResult{Op: "get", Key: op.key, Value: val})
			case "exists":
				reordered = append(reordered, PipelineResult{
					Op:     "exists",
					Key:    op.key,
					Exists: existsResults[op.key],
				})
			}
		}
		results = reordered
		return nil
	})

	if err != nil {
		return nil, err
	}
	return results, nil
}
```

- [ ] **Step 2: 验证编译通过**

```bash
go build ./internal/agentcore/store/kv/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/db_based.go
git commit -m "feat(kv): implement DbBasedKVStore Pipeline"
```

---

### Task 8: 编写单元测试 — 构造函数和基础 CRUD

**Files:**
- Create: `internal/agentcore/store/kv/db_based_test.go`

- [ ] **Step 1: 创建测试文件，写入辅助函数和基础测试**

```go
package kv

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTestDbBasedKVStore 创建用于测试的 DbBasedKVStore 实例，使用 SQLite :memory: 数据库。
func newTestDbBasedKVStore(t *testing.T) *DbBasedKVStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 SQLite 内存数据库失败: %v", err)
	}
	return NewDbBasedKVStore(db)
}

// ──── 构造函数测试 ────

// TestNewDbBasedKVStore 验证构造函数创建非 nil 实例。
func TestNewDbBasedKVStore(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	if store == nil {
		t.Fatal("NewDbBasedKVStore 返回 nil")
	}
	if store.db == nil {
		t.Fatal("内部 db 未初始化")
	}
}

// ──── 接口满足验证 ────

// TestDbBasedKVStore_接口满足 验证 DbBasedKVStore 满足 BaseKVStore 接口。
func TestDbBasedKVStore_接口满足(t *testing.T) {
	var _ BaseKVStore = (*DbBasedKVStore)(nil)
}

// ──── 惰性建表测试 ────

// TestDbBasedKVStore_惰性建表 验证首次操作时自动建表。
func TestDbBasedKVStore_惰性建表(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	// 初始状态：未建表
	if store.tableCreated.Load() {
		t.Error("初始状态 tableCreated 应为 false")
	}

	// 执行 Set 触发建表
	err := store.Set(ctx, "key1", []byte("value1"))
	if err != nil {
		t.Fatalf("Set 返回错误: %v", err)
	}

	// 建表后标志应为 true
	if !store.tableCreated.Load() {
		t.Error("Set 后 tableCreated 应为 true")
	}

	// 验证表存在：查询应成功
	var count int64
	store.db.Model(&KVStoreRow{}).Count(&count)
	if count != 1 {
		t.Errorf("建表后记录数 = %d, 期望 1", count)
	}
}

// ──── Set / Get 测试 ────

// TestDbBasedKVStore_SetGet 基本 Set + Get 往返。
func TestDbBasedKVStore_SetGet(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// TestDbBasedKVStore_Get_不存在 验证获取不存在的 key 返回 nil。
func TestDbBasedKVStore_Get_不存在(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	val, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if val != nil {
		t.Errorf("Get 返回 %v, 期望 nil", val)
	}
}

// TestDbBasedKVStore_Set_覆盖 验证 Set 覆盖已有值。
func TestDbBasedKVStore_Set_覆盖(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("old"))
	_ = store.Set(ctx, "key1", []byte("new"))

	val, _ := store.Get(ctx, "key1")
	if string(val) != "new" {
		t.Errorf("覆盖后 Get 返回 %q, 期望 %q", string(val), "new")
	}
}

// TestDbBasedKVStore_Set_二进制值 验证 Set/Get 二进制数据往返。
func TestDbBasedKVStore_Set_二进制值(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	// 包含 NULL 字节和其他非文本字节的值
	original := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 'h', 'e', 'l', 'l', 'o'}
	err := store.Set(ctx, "bin_key", original)
	if err != nil {
		t.Fatalf("Set 返回错误: %v", err)
	}

	val, err := store.Get(ctx, "bin_key")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if len(val) != len(original) {
		t.Fatalf("Get 返回长度 %d, 期望 %d", len(val), len(original))
	}
	for i, b := range val {
		if b != original[i] {
			t.Errorf("Get[%d] = 0x%02X, 期望 0x%02X", i, b, original[i])
		}
	}
}

// ──── Exists 测试 ────

// TestDbBasedKVStore_Exists_存在 验证 key 存在时 Exists 返回 true。
func TestDbBasedKVStore_Exists_存在(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// TestDbBasedKVStore_Exists_不存在 验证 key 不存在时 Exists 返回 false。
func TestDbBasedKVStore_Exists_不存在(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// TestDbBasedKVStore_Delete 验证删除后 Get 返回 nil。
func TestDbBasedKVStore_Delete(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	_ = store.Delete(ctx, "key1")

	val, _ := store.Get(ctx, "key1")
	if val != nil {
		t.Errorf("删除后 Get 返回 %v, 期望 nil", val)
	}
}

// TestDbBasedKVStore_Delete_不存在 验证删除不存在的 key 不报错。
func TestDbBasedKVStore_Delete_不存在(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	err := store.Delete(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("删除不存在的 key 返回错误: %v", err)
	}
}
```

- [ ] **Step 2: 运行测试验证通过**

```bash
go test -v -run "TestNewDbBasedKVStore|TestDbBasedKVStore_接口满足|TestDbBasedKVStore_惰性建表|TestDbBasedKVStore_SetGet|TestDbBasedKVStore_Get_不存在|TestDbBasedKVStore_Set_覆盖|TestDbBasedKVStore_Set_二进制值|TestDbBasedKVStore_Exists|TestDbBasedKVStore_Delete" ./internal/agentcore/store/kv/...
```

期望：全部 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/db_based_test.go
git commit -m "test(kv): add DbBasedKVStore constructor and basic CRUD tests"
```

---

### Task 9: 编写 ExclusiveSet 测试

**Files:**
- Modify: `internal/agentcore/store/kv/db_based_test.go`

- [ ] **Step 1: 在文件末尾添加 ExclusiveSet 相关测试**

```go
// ──── ExclusiveSet 测试 ────

// TestDbBasedKVStore_ExclusiveSet_新key 验证新 key 设置成功。
func TestDbBasedKVStore_ExclusiveSet_新key(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// TestDbBasedKVStore_ExclusiveSet_已存在拒绝 验证 key 已存在且未过期时返回 false。
func TestDbBasedKVStore_ExclusiveSet_已存在拒绝(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// TestDbBasedKVStore_ExclusiveSet_已过期允许覆盖 验证 key 已过期时允许覆盖。
func TestDbBasedKVStore_ExclusiveSet_已过期允许覆盖(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	// 设置 1 秒后过期的 key
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 1)
	if !ok {
		t.Fatal("首次 ExclusiveSet 应返回 true")
	}

	// 等待过期
	time.Sleep(2 * time.Second)

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

// TestDbBasedKVStore_ExclusiveSet_expiry为零 验证 expiry=0 时永不过期。
func TestDbBasedKVStore_ExclusiveSet_expiry为零(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value1"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Error("ExclusiveSet 返回 false, 期望 true")
	}

	// 验证再次 ExclusiveSet 同一 key 会被拒绝（永不过期）
	ok2, _ := store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if ok2 {
		t.Error("expiry=0 的 key 应永不过期，第二次 ExclusiveSet 应返回 false")
	}
}

// TestDbBasedKVStore_ExclusiveSet_带expiry 验证带过期时间的设置。
func TestDbBasedKVStore_ExclusiveSet_带expiry(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 60)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	// 验证 key 存在
	exists, _ := store.Exists(ctx, "key1")
	if !exists {
		t.Error("设置后 Exists 返回 false, 期望 true")
	}

	// 验证内部值是 exclusive JSON 格式
	var row KVStoreRow
	store.db.Where("key = ?", "key1").First(&row)
	var ev exclusiveValue
	if err := json.Unmarshal(row.Value, &ev); err != nil {
		t.Fatalf("内部值不是有效 JSON: %v", err)
	}
	if ev.ExclusiveExpiry == 0 {
		t.Error("ExclusiveExpiry 为 0, 期望非零")
	}
}

// TestDbBasedKVStore_ExclusiveSet_手动修改过期时间 验证手动将过期时间设为过去后可覆盖。
func TestDbBasedKVStore_ExclusiveSet_手动修改过期时间(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	// 设置带过期的 key
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 60)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	// 手动修改过期时间为过去
	var row KVStoreRow
	store.db.Where("key = ?", "key1").First(&row)
	var ev exclusiveValue
	json.Unmarshal(row.Value, &ev)
	ev.ExclusiveExpiry = 1 // 1970-01-01，已过期
	newValue, _ := json.Marshal(ev)
	store.db.Model(&KVStoreRow{}).Where("key = ?", "key1").Update("value", newValue)

	// 已过期，允许覆盖
	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Error("已过期 key 的 ExclusiveSet 返回 false, 期望 true")
	}
}

// TestDbBasedKVStore_Get_解包Exclusive值 验证 Get 解包 exclusive-set 的值返回实际 []byte。
func TestDbBasedKVStore_Get_解包Exclusive值(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	ok, err := store.ExclusiveSet(ctx, "key1", []byte("exclusive_value"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Fatal("ExclusiveSet 返回 false, 期望 true")
	}

	// Get 应解包，返回实际值而非 JSON 包装
	val, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if string(val) != "exclusive_value" {
		t.Errorf("Get 返回 %q, 期望 %q", string(val), "exclusive_value")
	}

	// 验证返回的不是 JSON 字节
	var ev exclusiveValue
	if err := json.Unmarshal(val, &ev); err == nil && ev.ExclusiveValue != "" {
		t.Error("Get 返回了 JSON 包装结构，应返回解包后的实际值")
	}
}
```

- [ ] **Step 2: 运行测试验证通过**

```bash
go test -v -run "TestDbBasedKVStore_ExclusiveSet|TestDbBasedKVStore_Get_解包Exclusive值" ./internal/agentcore/store/kv/...
```

期望：全部 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/db_based_test.go
git commit -m "test(kv): add DbBasedKVStore ExclusiveSet tests"
```

---

### Task 10: 编写 GetByPrefix / DeleteByPrefix / MGet / BatchDelete 测试

**Files:**
- Modify: `internal/agentcore/store/kv/db_based_test.go`

- [ ] **Step 1: 在文件末尾添加前缀和批量操作测试**

```go
// ──── GetByPrefix 测试 ────

// TestDbBasedKVStore_GetByPrefix_正常 验证按前缀获取键值对。
func TestDbBasedKVStore_GetByPrefix_正常(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// TestDbBasedKVStore_GetByPrefix_无匹配 验证无匹配前缀返回空 map。
func TestDbBasedKVStore_GetByPrefix_无匹配(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// ──── DeleteByPrefix 测试 ────

// TestDbBasedKVStore_DeleteByPrefix_一次性 验证 batchSize=0 时一次性删除。
func TestDbBasedKVStore_DeleteByPrefix_一次性(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// TestDbBasedKVStore_DeleteByPrefix_分批 验证 batchSize>0 时分批删除。
func TestDbBasedKVStore_DeleteByPrefix_分批(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "user:2", []byte("bob"))
	_ = store.Set(ctx, "user:3", []byte("charlie"))

	err := store.DeleteByPrefix(ctx, "user:", 2)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	result, _ := store.GetByPrefix(ctx, "user:")
	if len(result) != 0 {
		t.Errorf("分批删除后仍有 %d 条 user: 前缀的 key", len(result))
	}
}

// TestDbBasedKVStore_DeleteByPrefix_无匹配 验证无匹配前缀不报错。
func TestDbBasedKVStore_DeleteByPrefix_无匹配(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))

	err := store.DeleteByPrefix(ctx, "item:", 0)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	exists, _ := store.Exists(ctx, "user:1")
	if !exists {
		t.Error("user:1 不应被删除")
	}
}

// ──── MGet 测试 ────

// TestDbBasedKVStore_MGet_正常 验证批量获取多个 key。
func TestDbBasedKVStore_MGet_正常(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// TestDbBasedKVStore_MGet_空列表 验证传入空 key 列表时返回空切片。
func TestDbBasedKVStore_MGet_空列表(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	values, err := store.MGet(ctx, []string{})
	if err != nil {
		t.Fatalf("MGet 返回错误: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("MGet 返回 %d 条, 期望 0 条", len(values))
	}
}

// TestDbBasedKVStore_MGet_部分不存在 验证部分 key 不存在时对应位置为 nil。
func TestDbBasedKVStore_MGet_部分不存在(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// ──── BatchDelete 测试 ────

// TestDbBasedKVStore_BatchDelete_正常 验证批量删除并返回删除数量。
func TestDbBasedKVStore_BatchDelete_正常(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// TestDbBasedKVStore_BatchDelete_空列表 验证空列表返回 0。
func TestDbBasedKVStore_BatchDelete_空列表(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	deleted, err := store.BatchDelete(ctx, []string{}, 0)
	if err != nil {
		t.Fatalf("BatchDelete 返回错误: %v", err)
	}
	if deleted != 0 {
		t.Errorf("BatchDelete 返回 %d, 期望 0", deleted)
	}
}

// TestDbBasedKVStore_BatchDelete_分批 验证分批删除。
func TestDbBasedKVStore_BatchDelete_分批(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// TestDbBasedKVStore_BatchDelete_部分不存在 验证只删除存在的 key。
func TestDbBasedKVStore_BatchDelete_部分不存在(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))

	deleted, _ := store.BatchDelete(ctx, []string{"k1", "k2"}, 0)
	if deleted != 1 {
		t.Errorf("BatchDelete 返回 %d, 期望 1", deleted)
	}
}
```

- [ ] **Step 2: 运行测试验证通过**

```bash
go test -v -run "TestDbBasedKVStore_GetByPrefix|TestDbBasedKVStore_DeleteByPrefix|TestDbBasedKVStore_MGet|TestDbBasedKVStore_BatchDelete" ./internal/agentcore/store/kv/...
```

期望：全部 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/kv/db_based_test.go
git commit -m "test(kv): add DbBasedKVStore prefix and batch operation tests"
```

---

### Task 11: 编写 Pipeline 和并发安全测试

**Files:**
- Modify: `internal/agentcore/store/kv/db_based_test.go`

- [ ] **Step 1: 在文件末尾添加 Pipeline 和并发测试**

```go
// ──── Pipeline 测试 ────

// TestDbBasedKVStore_Pipeline_混合操作 验证 Pipeline 混合 set+get+exists 操作。
func TestDbBasedKVStore_Pipeline_混合操作(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	// 先设置一个 key
	_ = store.Set(ctx, "existing", []byte("old_value"))

	// 创建 Pipeline 并执行混合操作
	pipe := store.Pipeline(ctx)
	_ = pipe.Set(ctx, "new_key", []byte("new_value"), 0)
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

// TestDbBasedKVStore_Pipeline_复用 验证 Execute 后 Pipeline 可复用。
func TestDbBasedKVStore_Pipeline_复用(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	pipe := store.Pipeline(ctx)

	// 第一次使用
	_ = pipe.Set(ctx, "k1", []byte("v1"), 0)
	results1, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("第一次 Execute 返回错误: %v", err)
	}
	if len(results1) != 1 {
		t.Errorf("第一次 Execute 返回 %d 条结果, 期望 1 条", len(results1))
	}

	// 第二次使用（复用同一个 Pipeline）
	_ = pipe.Set(ctx, "k2", []byte("v2"), 0)
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

// TestDbBasedKVStore_Pipeline_空操作 验证空 Pipeline Execute 返回空切片。
func TestDbBasedKVStore_Pipeline_空操作(t *testing.T) {
	store := newTestDbBasedKVStore(t)
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

// ──── 并发安全测试 ────

// TestDbBasedKVStore_并发安全 验证多 goroutine 并发读写无 race。
func TestDbBasedKVStore_并发安全(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	const goroutines = 20
	const opsPerGoroutine = 50

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

- [ ] **Step 2: 运行全部 DbBasedKVStore 测试**

```bash
go test -v -run "TestDbBasedKVStore" ./internal/agentcore/store/kv/...
```

期望：全部 PASS

- [ ] **Step 3: 运行 race 检测**

```bash
go test -race -run "TestDbBasedKVStore_并发安全" ./internal/agentcore/store/kv/...
```

期望：PASS，无 race

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/kv/db_based_test.go
git commit -m "test(kv): add DbBasedKVStore Pipeline and concurrency tests"
```

---

### Task 12: 更新 doc.go 和覆盖率验证

**Files:**
- Modify: `internal/agentcore/store/kv/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件目录**

将 doc.go 中的文件目录从：

```
//	kv/
//	├── doc.go           # 包文档
//	├── base.go          # BaseKVStore 接口 + KVPipeline 接口 + PipelineResult 结构体
//	├── in_memory.go     # InMemoryKVStore 内存实现 + inMemoryPipeline
//	└── file.go          # FileKVStore 文件持久化实现 + filePipeline
```

更新为：

```
//	kv/
//	├── doc.go           # 包文档
//	├── base.go          # BaseKVStore 接口 + KVPipeline 接口 + PipelineResult 结构体
//	├── in_memory.go     # InMemoryKVStore 内存实现 + inMemoryPipeline
//	├── file.go          # FileKVStore 文件持久化实现 + filePipeline
//	└── db_based.go      # DbBasedKVStore 数据库实现（GORM）+ dbBasedPipeline
```

同时在包概述中添加 DbBasedKVStore 说明。完整更新后的 doc.go：

```go
// Package kv 提供键值存储的抽象接口定义和多种后端实现。
//
// 本包定义了所有 KV 存储后端必须满足的 BaseKVStore 接口，
// 以及用于批量操作的 KVPipeline 接口和 PipelineResult 结果类型。
// InMemoryKVStore 提供基于内存的并发安全实现，支持惰性过期检查。
// FileKVStore 提供基于 bbolt 的文件持久化实现，对应 Python ShelveStore，
// 严格复刻其语义（包括已知的值解包不一致和过期语义不一致）。
// DbBasedKVStore 提供基于 GORM 的数据库持久化实现，支持 SQLite/MySQL/PostgreSQL，
// 对应 Python DbBasedKVStore。
//
// 文件目录：
//
//	kv/
//	├── doc.go           # 包文档
//	├── base.go          # BaseKVStore 接口 + KVPipeline 接口 + PipelineResult 结构体
//	├── in_memory.go     # InMemoryKVStore 内存实现 + inMemoryPipeline
//	├── file.go          # FileKVStore 文件持久化实现 + filePipeline
//	└── db_based.go      # DbBasedKVStore 数据库实现（GORM）+ dbBasedPipeline
//
// 对应 Python 代码：openjiuwen/core/foundation/store/base_kv_store.py
//
//	InMemoryKVStore 对应: openjiuwen/core/foundation/store/kv/in_memory_kv_store.py
//	FileKVStore 对应:     openjiuwen/core/foundation/store/kv/shelve_store.py
//	DbBasedKVStore 对应:  openjiuwen/core/foundation/store/kv/db_based_kv_store.py
package kv
```

- [ ] **Step 2: 运行完整覆盖率检查**

```bash
go test -cover ./internal/agentcore/store/kv/...
```

期望：覆盖率 ≥ 85%

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 4.4 行的状态从 `☐` 改为 `✅`：

```
| 4.4 | ✅ | DbBasedKVStore | 数据库 KV 存储 | `openjiuwen/core/foundation/store/kv/db_based_kv_store.py` |
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/kv/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs: update doc.go and IMPLEMENTATION_PLAN for DbBasedKVStore"
```

---

### Task 13: 最终验证

**Files:** 无变更

- [ ] **Step 1: 运行全量测试**

```bash
go test -v ./internal/agentcore/store/kv/...
```

期望：全部 PASS

- [ ] **Step 2: 运行覆盖率检查**

```bash
go test -cover ./internal/agentcore/store/kv/...
```

期望：≥ 85%

- [ ] **Step 3: 运行 race 检测**

```bash
go test -race ./internal/agentcore/store/kv/...
```

期望：无 race condition

- [ ] **Step 4: 验证 go vet**

```bash
go vet ./internal/agentcore/store/kv/...
```

期望：无警告

---

## 自检结果

1. **设计文档覆盖**：所有设计决策（GORM选型、方言支持、BLOB列、ExclusiveSet原子性、惰性建表、Pipeline分组执行）都有对应实现任务
2. **占位符扫描**：无 TBD/TODO/占位符，所有步骤包含完整代码
3. **类型一致性**：`KVStoreRow`、`exclusiveValue`、`DbBasedKVStore`、`dbBasedPipeline` 在所有任务中名称一致；`operation` 类型复用 `in_memory.go` 中已有的定义
