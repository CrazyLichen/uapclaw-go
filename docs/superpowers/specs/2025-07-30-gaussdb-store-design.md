# GaussDbStore 设计文档

> 日期：2025-07-30
> 对应 Python：`openjiuwen/extensions/store/db/gauss_db_store.py` + `gauss_dialect.py`
> 实现位置：`internal/agentcore/store/db/gaussdb/`
> 实现计划步骤：4.14

## 1. 概述

GaussDbStore 是 BaseDbStore 接口的 GaussDB 数据库实现，提供 GaussDB 特有的方言适配。
Python 侧的 GaussDbStore 本身只是一个薄包装（持有 AsyncEngine 并返回），但其配套的
`gauss_dialect.py` 包含大量 GaussDB 与 PostgreSQL 不兼容点的适配逻辑。本设计完整对标
Python 的每一个适配特性，在 GORM 框架下以自定义 Dialector 的方式实现等价功能。

## 2. 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 实现策略 | 完整自定义 Dialector，嵌入 postgres.Dialector | 完整对标 Python gauss_dialect.py，每个适配点都有明确实现 |
| 包位置 | `internal/agentcore/store/db/gaussdb/` | GaussDB 是具体厂商扩展，独立子目录避免 db/ 包职责膨胀 |
| Dialector 嵌入方式 | 值嵌入 `postgres.Dialector` | postgres.Dialector 使用值接收者，嵌入后可委托调用 |
| Migrator 嵌入方式 | 值嵌入 `postgres.Migrator` | 当前 GORM postgres Migrator 的 SQL 与 GaussDB 兼容，预留覆写点 |
| LOCKING 子句 | ClauseBuilder 注册 | 对标 Python GaussCompiler.for_update_clause()，忽略 NOWAIT/SKIP LOCKED |
| 字符串类型适配 | 自定义 SerializerInterface | 对标 Python GaussString.bind_processor()，自动转换非 string 绑定值 |
| UUID/ENUM 类型 | DataTypeOf 覆写 | 对标 Python supports_native_uuid/enum = False |
| 驱动 | 复用 pgx/v5 | GaussDB 兼容 PostgreSQL 协议，pgx 可直接连接，无需 async_gaussdb |

## 3. Python 特性对标映射

| # | Python 特性 | 作用 | Go 实现位置 | 说明 |
|---|------------|------|------------|------|
| 1 | `_patch_gaussdb_driver` | 补齐 async_gaussdb 驱动缺失的 DB-API 属性 | 不需要 | pgx 是成熟驱动，不存在此问题 |
| 2 | `_patch_gaussdb_reflection_sql` | 拦截含 pg_type.typcollation 的 SQL → 替换为 NULL | 不需要 | GORM postgres Migrator 的 SQL 不查询 typcollation 列 |
| 3 | `GaussCompiler.for_update_clause` | FOR UPDATE 简化，忽略 NOWAIT/SKIP LOCKED | `clause.go` | gaussLockingClauseBuilder 注册为 "FOR" ClauseBuilder |
| 4 | `GaussString.bind_processor` | 非 string 值自动转 string，datetime 格式化 | `serializer.go` | gaussStringSerializer 实现 SerializerInterface |
| 5 | `supports_native_enum = False` | 跳过 enum 内省 | `dialector.go` | DataTypeOf 中 ENUM 类型 → varchar |
| 6 | `supports_native_uuid = False` | 跳过 uuid 原生支持 | `dialector.go` | DataTypeOf 中 UUID 类型 → varchar(36) |
| 7 | `use_insertmanyvalues = False` | 禁用批量 INSERT 多值 | 不需要 | GORM 无此概念，CreateClauses 保持一致即可 |
| 8 | `import_dbapi` → `async_gaussdb` | 加载 GaussDB 异步驱动 | `dialector.go` | Initialize 中使用 pgx 连接，复用 postgres 驱动逻辑 |
| 9 | `_get_server_version_info` → `(9,2)` | 硬编码版本号 | 不需要 | GORM postgres Migrator 不检查数据库版本 |
| 10 | `_domain_query` / `_enum_query` → FALSE | 跳过 domain/enum 内省 | 不需要 | GORM postgres Migrator 无 domain/enum 内省查询 |
| 11 | dialect 注册 `gaussdb.async_gaussdb` | 全局注册方言 | `dialector.go` | GaussOpen() / GaussNew() 工厂函数 |
| 12 | GaussDbStore 类 | 持有 AsyncEngine 并返回 | `store.go` | GaussDbStore 结构体实现 db.BaseDbStore |
| 13 | 优雅降级（ImportError → warning） | 缺少依赖时仅警告 | `dialector.go` | Initialize 错误处理 |

## 4. 核心组件设计

### 4.1 GaussDialector（`dialector.go`）

```go
// GaussDialector GaussDB 数据库方言，基于 PostgreSQL 方言扩展。
//
// 对应 Python: openjiuwen/extensions/store/db/gauss_dialect.py (GaussDialectAsyncpg)
//
// GaussDB 与 PostgreSQL 的主要差异：
//   - 不支持 NOWAIT / SKIP LOCKED 锁选项
//   - 不支持原生 ENUM / UUID 类型
//   - 非 string 值绑定到 string 列时需要自动转换
type GaussDialector struct {
    postgres.Dialector  // 值嵌入；所有方法使用值接收者，与 postgres.Dialector 一致
}
```

**注意：** postgres.Dialector 的所有方法均使用值接收者（`func (dialector Dialector) Name() string`），
因此 GaussDialector 的覆写方法也必须使用值接收者，否则嵌入的值接收者方法会优先被调用。

**方法覆写清单：**

| 方法 | 覆写策略 | 对标 Python |
|------|---------|------------|
| `Name() string` | 返回 `"gaussdb"` | `GaussDialectAsyncpg.name = 'gaussdb'` |
| `Initialize(db *gorm.DB) error` | 委托 postgres.Initialize() + 注册 ClauseBuilder("FOR") + 注册 gauss_string Serializer | import_dbapi + GaussCompiler |
| `DataTypeOf(field *schema.Field) string` | UUID→`varchar(36)`, ENUM→`varchar`, 其余委托 postgres | supports_native_uuid/enum = False |
| `Migrator(db *gorm.DB) gorm.Migrator` | 返回 GaussMigrator | _domain_query / _enum_query |
| `Translate(err error) error` | 委托 postgres.ErrorTranslator | PG 错误码映射一致 |
| `SavePoint(tx, name)` | 委托 postgres | 无差异 |
| `RollbackTo(tx, name)` | 委托 postgres | 无差异 |
| `BindVarTo(writer, stmt, v)` | 委托 postgres | `$N` 占位符一致 |
| `QuoteTo(writer, str)` | 委托 postgres | 双引号标识符一致 |
| `Explain(sql, vars...)` | 委托 postgres | 一致 |
| `DefaultValueOf(field)` | 委托 postgres | 一致 |
| `Apply(config)` | 委托 postgres | 一致 |

**Initialize() 流程：**

```
1. 调用 postgres.Dialector.Initialize(db)
   → 完成基础 PG 初始化（回调注册、连接池创建）
2. 注册 db.ClauseBuilders["FOR"] = gaussLockingClauseBuilder
   → 覆写 LOCKING 子句生成逻辑
3. 调用 schema.RegisterSerializer("gauss_string", gaussStringSerializer{})
   → 注册 GaussDB 字符串序列化器
4. 日志记录 GaussDB 方言初始化完成
```

**DataTypeOf() 覆写逻辑：**

```
1. 如果 field.DataType 为自定义类型且包含 "uuid" → 返回 "varchar(36)"
2. 如果 field.DataType 为自定义类型且包含 "enum" → 返回 "varchar"
3. 其他 → 委托 postgres.Dialector.DataTypeOf(field)
```

**工厂函数：**

```go
// GaussOpen 使用 DSN 创建 GaussDialector。
func GaussOpen(dsn string) gorm.Dialector

// GaussNew 使用配置创建 GaussDialector。
func GaussNew(config postgres.Config) gorm.Dialector
```

### 4.2 GaussMigrator（`migrator.go`）

```go
// GaussMigrator GaussDB 迁移器，基于 PostgreSQL 迁移器扩展。
//
// 对应 Python: GaussDialectAsyncpg._domain_query / _enum_query / _get_server_version_info
//
// 当前 GORM postgres Migrator 的 SQL 不查询 pg_type.typcollation，
// 也不做 domain/enum 内省，因此 GaussDB 天然兼容。
// 本 Migrator 预留覆写点，以便未来 GORM 版本变更时快速适配。
type GaussMigrator struct {
    postgres.Migrator
}
```

**当前策略：** 所有方法直接委托 `postgres.Migrator`。如果未来 GORM 版本的 postgres Migrator
引入了 GaussDB 不兼容的 SQL（如查询 `pg_type.typcollation` 或 `pg_enum`），在此处覆写
`ColumnTypes()` 方法进行改写。

### 4.3 gaussLockingClauseBuilder（`clause.go`）

```go
// gaussLockingClauseBuilder GaussDB LOCKING 子句构建器。
//
// 对应 Python: GaussCompiler.for_update_clause()
//
// GaussDB 不支持 NOWAIT / SKIP LOCKED 锁选项，
// 此构建器忽略 Locking.Options，仅输出 "FOR <strength>"。
// 同时忽略 Locking.Table（GaussDB 不支持 OF table 语法）。
func gaussLockingClauseBuilder(c clause.Clause, builder clause.Builder)
```

**行为：**

| 输入 | postgres 默认输出 | GaussDB 输出 |
|------|------------------|-------------|
| `Locking{Strength: "UPDATE"}` | `FOR UPDATE` | `FOR UPDATE` |
| `Locking{Strength: "UPDATE", Options: "NOWAIT"}` | `FOR UPDATE NOWAIT` | `FOR UPDATE` |
| `Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}` | `FOR UPDATE SKIP LOCKED` | `FOR UPDATE` |
| `Locking{Strength: "SHARE"}` | `FOR SHARE` | `FOR SHARE` |
| `Locking{Strength: "UPDATE", Table: ...}` | `FOR UPDATE OF "table"` | `FOR UPDATE` |

### 4.4 gaussStringSerializer（`serializer.go`）

```go
// gaussStringSerializer GaussDB 字符串序列化器。
//
// 对应 Python: GaussString.bind_processor()
//
// 确保所有绑定到 string 列的非 string 值在进入驱动前被转换为 string。
// 特别处理 time.Time → "2006-01-02 15:04:05.000000" 格式。
type gaussStringSerializer struct{}
```

**Value() 转换规则：**

| Go 类型 | 转换方式 | 对标 Python |
|---------|---------|------------|
| `string` | 直接返回 | 无需转换 |
| `time.Time` | `Format("2006-01-02 15:04:05.000000")` | `strftime('%Y-%m-%d %H:%M:%S.%f')` |
| `nil` | 返回 nil | `if value is None: return None` |
| 其他 | `fmt.Sprintf("%v", v)` | `str(value)` |

**Scan() 转换规则：**

| 数据库值类型 | 转换方式 |
|-------------|---------|
| `string` | 直接设置 |
| `[]byte` | `string(v)` |
| 其他 | `fmt.Sprintf("%v", v)` |

**使用方式：** 模型字段标注 `serializer:"gauss_string"` tag：

```go
type MyModel struct {
    Name string `gorm:"serializer:gauss_string"`
}
```

### 4.5 GaussDbStore（`store.go`）

```go
// GaussDbStore GaussDB 数据库存储，实现 db.BaseDbStore 接口。
//
// 对应 Python: openjiuwen/extensions/store/db/gauss_db_store.py
//
// 本实现通过 GaussDialector 创建 *gorm.DB 实例，
// 并提供 GaussDB 特有的方言适配（LOCKING 子句简化、
// 字符串序列化、UUID/ENUM 类型映射等）。
type GaussDbStore struct {
    // db GORM 数据库实例（通过 GaussDialector 创建）
    db *gorm.DB
}
```

**构造函数：**

| 函数 | 说明 | 对标 Python |
|------|------|------------|
| `NewGaussDbStore(dsn string, opts ...gorm.Option) (*GaussDbStore, error)` | 从 DSN 创建，内部调用 `gorm.Open(GaussOpen(dsn), ...)` | `GaussDbStore(async_conn)` |
| `NewGaussDbStoreWithDB(db *gorm.DB) *GaussDbStore` | 从已有 `*gorm.DB` 创建 | 直接赋值 |

**接口实现：**

| 方法 | 说明 | 对标 Python |
|------|------|------------|
| `GetDB(ctx context.Context) *gorm.DB` | 实现 `db.BaseDbStore` 接口 | `get_async_engine() -> AsyncEngine` |
| `Close() error` | 关闭底层连接池 | Python 无此方法（由 AsyncEngine 管理） |

## 5. 文件结构

```
internal/agentcore/store/db/
├── doc.go                      # 包文档（需更新文件目录）
├── base.go                     # BaseDbStore 接口（已有）
├── default.go                  # DefaultDbStore（已有）
├── base_test.go                # （已有）
├── default_test.go             # （已有）
└── gaussdb/                    # GaussDB 数据库扩展（新增）
    ├── doc.go                  # 包文档
    ├── store.go                # GaussDbStore 结构体 + 构造函数
    ├── dialector.go            # GaussDialector 完整实现
    ├── migrator.go             # GaussMigrator 实现
    ├── serializer.go           # gaussStringSerializer 实现
    ├── clause.go               # gaussLockingClauseBuilder 实现
    ├── store_test.go           # GaussDbStore 测试
    ├── dialector_test.go       # GaussDialector 测试
    ├── migrator_test.go        # GaussMigrator 测试
    ├── serializer_test.go      # gaussStringSerializer 测试
    └── clause_test.go          # gaussLockingClauseBuilder 测试
```

## 6. 依赖关系

```
db/gaussdb/ 包导入：
  ├── internal/agentcore/store/db       # db.BaseDbStore 接口
  ├── gorm.io/driver/postgres           # postgres.Dialector, postgres.Migrator, postgres.Config
  ├── gorm.io/gorm                      # gorm.Dialector, gorm.DB, gorm.Migrator
  ├── gorm.io/gorm/clause               # clause.Locking, clause.Clause, clause.Builder
  ├── gorm.io/gorm/schema               # schema.Field, schema.SerializerInterface
  └── github.com/jackc/pgx/v5          # pgx 连接配置

db/ 包不导入 db/gaussdb/（无反向依赖）
```

## 7. 测试策略

### 7.1 单元测试（可 mock，无需真实 GaussDB）

| 测试文件 | 测试内容 |
|---------|---------|
| `dialector_test.go` | Name() 返回 "gaussdb"；DataTypeOf 对 UUID/ENUM 的映射；工厂函数 GaussOpen/GaussNew 返回正确类型 |
| `migrator_test.go` | GaussMigrator 嵌入 postgres.Migrator 的类型断言 |
| `serializer_test.go` | gaussStringSerializer.Value() 各类型转换（string/time.Time/nil/其他）；Scan() 各类型转换 |
| `clause_test.go` | gaussLockingClauseBuilder 对各种 Locking 输入的 SQL 输出 |
| `store_test.go` | NewGaussDbStoreWithDB 构造；GetDB 返回值；Close 行为；接口满足性 db.BaseDbStore |

### 7.2 集成测试（需真实 GaussDB）

```go
//go:build integration

// 测试 GaussDialector.Initialize() 真实连接 GaussDB
// 测试 GaussMigrator.ColumnTypes() 真实表结构查询
// 测试 GaussDbStore 完整 CRUD 流程
```

## 8. 与已有实现的关系

| 组件 | 关系 |
|------|------|
| `db.BaseDbStore` | GaussDbStore 实现此接口 |
| `db.DefaultDbStore` | 平级实现，DefaultDbStore 使用标准 postgres Dialector，GaussDbStore 使用 GaussDialector |
| `vector.GaussVectorStore` | 不同领域（向量存储 vs 数据库连接），GaussVectorStore 使用 pgxpool 直连，GaussDbStore 使用 GORM |
| `vector.GaussDiskANN` | 无直接关系，向量索引配置 |

## 9. 后续扩展点

1. **GaussMigrator.ColumnTypes()** — 如果 GORM 未来版本的 postgres Migrator 引入 `pg_type.typcollation` 或 `pg_enum` 查询，在 GaussMigrator 中覆写改写
2. **GaussDialector.Initialize()** — 如果 GaussDB 需要 pgx 连接级别的特殊配置（如自定义类型映射），在此处扩展
3. **更多 GaussDB 不兼容特性** — 通过增加 ClauseBuilder 或覆写 Dialector 方法实现
