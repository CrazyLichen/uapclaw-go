# DbBasedKVStore 设计文档

> 日期：2025-07-29
> 对应 Python：`openjiuwen/core/foundation/store/kv/db_based_kv_store.py`
> 实现位置：`internal/agentcore/store/kv/db_based.go`

## 1. 概述

DbBasedKVStore 是基于 GORM 的数据库 KV 存储实现，支持 SQLite、MySQL、PostgreSQL 三种方言。
它实现了 `BaseKVStore` 接口，与已有的 InMemoryKVStore、FileKVStore 并列，为需要持久化且
支持多数据库后端的场景提供统一 KV 存储能力。

## 2. 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| ORM | GORM | 功能完整、方言自动适配、社区成熟 |
| 数据库方言 | SQLite + MySQL + PostgreSQL | 对齐 Python 完整支持 |
| SQLite 驱动 | gorm.io/driver/sqlite + modernc（纯 Go） | 零 CGO，交叉编译无障碍 |
| 包结构 | 全放 kv 包，GORM 隐式依赖 | 代码简单，与 InMemory/File 同级 |
| 构造函数入参 | `*gorm.DB` | 对齐 Python 传入 AsyncEngine，职责分离 |
| 值存储 | BLOB 列，无 `__BYTES:` 前缀 | Go 统一 []byte，无需区分 string/bytes |
| ExclusiveSet | 依赖数据库主键唯一性 | key 是主键，并发写只有一个成功，无需应用层锁 |
| 表结构 | key VARCHAR(255) PK + value BLOB | 对齐 Python kv_store 表 |
| 表名 | 硬编码 "kv_store" | 对齐 Python，多组件通过 key 前缀区分命名空间 |
| 惰性建表 | atomic.Bool 快速路径 + sync.Once 慢路径 | 高性能无锁快速路径 + 安全的建表保证 |

## 3. GORM 模型

```go
// KVStoreRow kv_store 表的 GORM 模型
type KVStoreRow struct {
    // Key 键，主键
    Key string `gorm:"column:key;type:varchar(255);primaryKey"`
    // Value 值，BLOB 类型
    Value []byte `gorm:"column:value;type:blob;not null"`
}

// TableName 返回表名
func (KVStoreRow) TableName() string { return "kv_store" }
```

## 4. 核心结构体

```go
// DbBasedKVStore 基于 GORM 的数据库 KV 存储实现
type DbBasedKVStore struct {
    // db GORM 数据库实例
    db *gorm.DB
    // tableCreated 建表标志，atomic.Bool 做快速路径
    tableCreated atomic.Bool
    // tableOnce 慢路径建表，保证只执行一次
    tableOnce sync.Once
}

// dbBasedPipeline 数据库 Pipeline 实现
type dbBasedPipeline struct {
    // ops 待执行的操作列表
    ops []operation
    // store 关联的 DbBasedKVStore 实例
    store *DbBasedKVStore
}
```

## 5. 构造函数

```go
// NewDbBasedKVStore 创建基于 GORM 的数据库 KV 存储
// db: 已初始化的 GORM 数据库实例（调用方负责配置方言和连接池）
func NewDbBasedKVStore(db *gorm.DB) *DbBasedKVStore
```

调用方示例：
```go
import (
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
)

db, _ := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
store := kv.NewDbBasedKVStore(db)
```

## 6. 惰性建表机制

```go
func (s *DbBasedKVStore) ensureTable() {
    if s.tableCreated.Load() {
        return  // 快速路径：无锁原子读
    }
    s.tableOnce.Do(func() {
        s.db.AutoMigrate(&KVStoreRow{})
        s.tableCreated.Store(true)
    })
}
```

每个方法（Set/Get/Delete/...）入口调用 `ensureTable()`。

## 7. ExclusiveSet 值格式

### 写入格式（JSON 包装）

当调用 ExclusiveSet 时，value 用 JSON 包装后存入 BLOB 列：

```json
{
    "exclusive_value": "<base64 编码的原始 []byte>",
    "exclusive_expiry": 1700000000
}
```

- `exclusive_value`：Base64 编码（JSON 不能直接存二进制）
- `exclusive_expiry`：Unix 时间戳（秒），0 表示不过期

### 读取逻辑（Get 解包）

Get 读取时：
1. 尝试 JSON 解析 BLOB 内容
2. 如果含 `exclusive_expiry` 字段 → 返回 `exclusive_value` 的 Base64 解码结果
3. 如果 JSON 解析失败或不含 `exclusive_expiry` → 直接返回 BLOB 原始字节

这与 Python 的行为对齐：普通 Set 存原始值，ExclusiveSet 存 JSON 包装，Get 自动解包。

## 8. Upsert 策略

GORM 的 `Clauses(clause.OnConflict{...})` 自动处理方言差异：
- SQLite → `ON CONFLICT DO UPDATE`
- MySQL → `ON DUPLICATE KEY UPDATE`
- PostgreSQL → `ON CONFLICT DO UPDATE`

无需手动写方言分支（Python 需要手动区分 mysql_insert / sqlite_insert）。

## 9. Pipeline 实现

对齐 Python：操作按类型分组，在一个事务内批量执行。

```go
func (p *dbBasedPipeline) Execute(ctx context.Context) ([]PipelineResult, error) {
    // 拷贝操作列表并清空，允许 Pipeline 复用
    ops := p.ops
    p.ops = nil

    // 在一个事务内执行
    // 1. 分组：set_ops / get_keys / exists_keys
    // 2. 批量 set：逐条 upsert（GORM 无原生批量 upsert）
    // 3. 批量 get：WHERE key IN (...)
    // 4. 批量 exists：WHERE key IN (...)
    // 5. 按原始操作顺序组装结果
}
```

## 10. 方法对照表

| Go 方法 | Python 方法 | 差异说明 |
|---------|------------|---------|
| `Set(ctx, key, value)` | `set(key, value)` | value 统一 []byte，BLOB 直接存 |
| `ExclusiveSet(ctx, key, value, expiry)` | `exclusive_set(key, value, expiry)` | 依赖主键唯一性，不加行锁 |
| `Get(ctx, key)` | `get(key)` | 解包 exclusive JSON，否则返回原始 BLOB |
| `Exists(ctx, key)` | `exists(key)` | 无差异 |
| `Delete(ctx, key)` | `delete(key)` | 无差异 |
| `GetByPrefix(ctx, prefix)` | `get_by_prefix(prefix)` | GORM Where("key LIKE ?", prefix+"%") |
| `DeleteByPrefix(ctx, prefix, batchSize)` | `delete_by_prefix(prefix, batch_size)` | batchSize=0 一次性删除，>0 分批 |
| `MGet(ctx, keys)` | `mget(keys)` | WHERE key IN (...)，按输入顺序返回 |
| `BatchDelete(ctx, keys, batchSize)` | `batch_delete(keys, batch_size)` | 返回删除行数 |
| `Pipeline(ctx)` | `pipeline()` | 返回 dbBasedPipeline |

## 11. 文件结构

```
kv/
├── doc.go              # 包文档（需更新文件目录和 DbBasedKVStore 说明）
├── base.go             # BaseKVStore 接口 + KVPipeline 接口
├── in_memory.go        # InMemoryKVStore
├── file.go             # FileKVStore (bbolt)
├── db_based.go         # DbBasedKVStore + dbBasedPipeline（新增）
├── base_test.go
├── in_memory_test.go
├── file_test.go
└── db_based_test.go    # 新增（SQLite 内存数据库测试）
```

## 12. 测试策略

- 使用 SQLite `:memory:` 内存数据库做单元测试，不需要 `integration` build tag
- 测试覆盖所有 BaseKVStore 接口方法
- 重点测试 ExclusiveSet 的并发语义和过期逻辑
- 重点测试 Get 对 exclusive JSON 的解包逻辑
- 重点测试 Pipeline 的操作分组和结果顺序
- 重点测试 GetByPrefix/DeleteByPrefix 的前缀匹配
- 重点测试 MGet/BatchDelete 的批量操作和 batchSize 分批
- 不测试真实 MySQL/PG（用 `integration` build tag 隔离）

## 13. 依赖变更

go.mod 新增：
```
gorm.io/gorm
gorm.io/driver/sqlite    // modernc 纯 Go（需 gormsqlite_nocgo build tag）
gorm.io/driver/mysql
gorm.io/driver/postgres
```
