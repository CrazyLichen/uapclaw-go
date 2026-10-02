# 7.22+7.23 迁移操作注册表 + 迁移器 设计文档

## 概述

本设计将 Python 项目中 `openjiuwen/core/memory/migration/` 的 **7.22 Migration Operations** 和 **7.23 Migration Migrators** 合并为一次实现。

7.22 的类型定义（OperationMetadata / BaseOperation / Operation 接口 / 13 种 Operation 类型 / OperationRegistry）已在 7.21 中提前完成。本次实现的核心内容是：

1. **5 个 Migrator 执行器**（SQL / KV / Vector / Message / IndexVersion）
2. **run_migrations 编排函数**
3. **4 个 VectorStore 的 UpdateSchema 回填**
4. **共享工具函数**（ComputeNewSchema / BuildTransformFunc）
5. **接口变更**（UpdateSchema 签名 []any → []operation.Operation + 新增 RenameCollection）

## Python 对应源码

| Go 路径 | Python 路径 |
|---------|------------|
| `migration/migrator/sql_migrator.go` | `openjiuwen/core/memory/migration/migrator/sql_migrator.py` |
| `migration/migrator/kv_migrator.go` | `openjiuwen/core/memory/migration/migrator/kv_migrator.py` |
| `migration/migrator/vector_migrator.go` | `openjiuwen/core/memory/migration/migrator/vector_migrator.py` |
| `migration/migrator/message_migrator.go` | `openjiuwen/core/memory/migration/migrator/message_migrator.py` |
| `migration/migrator/index_version_migrator.go` | `openjiuwen/core/memory/migration/migrator/index_version_migrator.py` |
| `migration/run_migrations.go` | `openjiuwen/core/memory/migration/run_migrations.py` |
| `foundation/store/vector/schema_utils.go` | `openjiuwen/core/foundation/store/vector/utils.py` |
| 4 个 VectorStore UpdateSchema | 各 VectorStore 的 `update_schema` 方法 |

---

## 一、接口变更

### 1.1 UpdateSchema 签名变更

**现状**（`foundation/store/vector/base.go`）：
```go
UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error
```

**变更后**：
```go
UpdateSchema(ctx context.Context, collectionName string, operations []operation.Operation, opts ...Option) error
```

**影响范围**：
- `BaseVectorStore` 接口定义
- `MilvusVectorStore` / `ChromaVectorStore` / `GaussVectorStore` / `ESVectorStore` 实现
- `base_test.go` 中的 `fakeVectorStore`
- `index/simple_test.go` 中的 fake
- `memory/manage/mem_model/semantic_store_test.go` 中的 fake

### 1.2 新增 RenameCollection 接口方法

**Python**：Milvus SDK 原生支持 `rename_collection`，GaussDB 支持 `ALTER TABLE RENAME`。ChromaDB 和 ES 不支持，但 Python `BaseVectorStore` 接口定义了此方法。

**Go 变更**（`foundation/store/vector/base.go`）：
```go
// RenameCollection 重命名集合。
// 不支持的后端返回 StatusStoreVectorNotSupported 错误。
//
// Python: BaseVectorStore.rename_collection(old_name, new_name)
RenameCollection(ctx context.Context, oldName string, newName string, opts ...Option) error
```

**各后端实现策略**：

| 后端 | 实现方式 | 备注 |
|------|---------|------|
| Milvus | `client.RenameCollection(ctx, oldName, newName)` | SDK 原生支持 |
| GaussDB | `db.Exec("ALTER TABLE ? RENAME TO ?", oldName, newName)` | PostgreSQL 兼容 |
| ChromaDB | 返回 `StatusStoreVectorNotSupported` 错误 | 不支持 rename |
| ES | 返回 `StatusStoreVectorNotSupported` 错误 | 不支持 rename |

---

## 二、共享工具函数（`foundation/store/vector/schema_utils.go`）

对齐 Python `openjiuwen/core/foundation/store/vector/utils.py` 中的两个函数。

### 2.1 ComputeNewSchema

```go
// ComputeNewSchema 根据 operations 计算目标 schema。
// 深拷贝 oldSchema 后逐个应用 operation，返回新 schema。
//
// Python: compute_new_schema(old_schema, operations)
func ComputeNewSchema(oldSchema *CollectionSchema, operations []operation.Operation) (*CollectionSchema, error)
```

**分发逻辑**：
- `AddScalarFieldOperation` → `computeSchemaAddField`
- `RenameScalarFieldOperation` → `computeSchemaRenameField`
- `UpdateScalarFieldTypeOperation` → `computeSchemaUpdateFieldType`
- `UpdateEmbeddingDimensionOperation` → `computeSchemaUpdateVectorDim`
- 其他类型 → 返回 `StatusStoreVectorSchemaInvalid` 错误

**4 个子函数对齐 Python `_compute_schema_add_field` 等的校验逻辑**：
- AddField：用 `AddField()` 追加字段
- RenameField：校验 old 存在、new 不存在，通过 `ToDict()`/`CollectionFromDict()` 深拷贝后修改字段名
- UpdateFieldType：校验字段存在且非向量字段，修改 type
- UpdateVectorDim：校验字段存在且是向量字段，修改 dim

### 2.2 BuildTransformFunc

```go
// BuildTransformFunc 构建统一的文档变换函数，顺序应用所有 operation。
//
// Python: build_transform_func_for_operations(operations)
func BuildTransformFunc(operations []operation.Operation) func(map[string]any) map[string]any
```

**返回闭包**对每个 doc 逐个应用 `applyOperationToDoc`：
- `AddScalarFieldOperation`：字段不存在且 DefaultValue 非零时设置
- `RenameScalarFieldOperation`：`doc[newName] = doc[oldName]; delete(doc, oldName)`
- `UpdateScalarFieldTypeOperation`：no-op（对齐 Python，让 Milvus 处理类型转换）
- `UpdateEmbeddingDimensionOperation`：调用 `RecomputeEmbeddingFunc(doc)`，无则用零向量 `[0.0]*newDimension`；校验返回向量长度

---

## 三、5 个 Migrator

所有 Migrator 位于 `internal/agentcore/memory/migration/migrator/` 子包。

### 3.1 SQLMigrator

**Python 对齐**：`openjiuwen/core/memory/migration/migrator/sql_migrator.py`

**Go 实现**：用 GORM Migrator 接口替代 Python 的 Alembic。

```go
// SQLMigrator SQL 表迁移执行器。
// 使用 GORM Migrator 接口执行 DDL 变更，版本跟踪通过 MemoryMetaManager。
//
// Python: openjiuwen/core/memory/migration/migrator/sql_migrator.py (SQLMigrator)
type SQLMigrator struct {
    // sqlDb SqlDbStore 实例
    sqlDb *model.SqlDbStore
    // metaManager 版本跟踪管理器
    metaManager *MemoryMetaManager
    // db GORM 数据库实例（从 sqlDb 获取）
    db *gorm.DB
}
```

**核心方法**：

| 方法 | Python | Go |
|------|--------|-----|
| `TryMigrate(entityKey, operations)` | `try_migrate` | 获取当前版本→过滤待执行操作→事务内执行→更新版本 |
| `migrateAddColumn(op)` | `_migrate_add_column` | `db.Migrator().AddColumn(&tableModel, op.ColumnName)` |
| `migrateRenameColumn(op)` | `_migrate_rename_column` | `db.Migrator().RenameColumn(&tableModel, op.OldColumnName, op.NewColumnName)` |
| `migrateUpdateColumnType(op)` | `_migrate_update_column_type` | `db.Migrator().AlterColumn(&tableModel, op.ColumnName)` |

**GORM Migrator 的局限性**：
- `AddColumn` 需要传入 GORM 模型结构体，无法直接用 table+column 字符串
- **解决方案**：用 `db.Table(tableName).Migrator()` + 动态构造 `gorm.ColumnType`；或用 `db.Exec(rawSQL)` 作为 fallback
- **推荐**：GORM Migrator 做不了的用原生 SQL fallback（与 Python 中 SQLite 的 `_alter_column_type_sqlite` 思路一致）

**版本跟踪**：通过 `MemoryMetaManager` 读写 `memory_meta` 表，与 Python 完全对齐。

**差异说明**：
- Python 用 Alembic `MigrationContext` + `Operations`，Go 用 GORM Migrator + 原生 SQL fallback
- Python 有 `_validate_table` 校验表名合法性，Go 中跳过（Registry 已保证 entity_key 合法）
- Python 的 `batch_migrate` 方法，Go 不实现（`run_migrations` 已逐表调用 `TryMigrate`）

### 3.2 KVMigrator

**Python 对齐**：`openjiuwen/core/memory/migration/migrator/kv_migrator.py`

```go
// KVMigrator KV 存储迁移执行器。
// 版本跟踪通过 KV_STORE 自身的 key MEMORY_MIGRATION_KV_SCHEMA_VERSION。
// 迁移前创建备份，失败时从备份恢复。
//
// Python: openjiuwen/core/memory/migration/migrator/kv_migrator.py (KVMigrator)
type KVMigrator struct {
    // kvStore KV 存储实例
    kvStore kv.BaseKVStore
}
```

**核心方法对齐 Python**：

| 方法 | Python | Go | 说明 |
|------|--------|-----|------|
| `TryMigrate` | `try_migrate` | 完全对齐 | 校验 entity_key + 版本比较 + 备份+执行+恢复 |
| `getCurrentVersion` | `_get_current_version` | 完全对齐 | 读取 `MEMORY_MIGRATION_KV_SCHEMA_VERSION` key |
| `hasMemoryModuleData` | `_has_memory_module_data` | 完全对齐 | 遍历 `KvPrefixRegistry.GetAllPrefixes()` |
| `updateVersion` | `_update_version` | 完全对齐 | `kvStore.Set(KVSchemaVersionKey, []byte(strconv.Itoa(version)))` |
| `executeOperation` | `_execute_operation` | 完全对齐 | type switch `UpdateKVOperation` → 调用 `UpdateFunc` |
| `validateOperationsOrder` | `_validate_operations_order` | 完全对齐 | 检查 schema_version 严格递增 |
| `createBackup` | `_create_backup` | 完全对齐 | JSON 序列化所有 prefix 下数据 |
| `restoreFromBackup` | `_restore_from_backup` | 完全对齐 | 删除旧数据+反序列化+逐 key 恢复 |
| `cleanupBackup` | `_cleanup_backup` | 完全对齐 | 删除备份 key |

**Go 与 Python 的类型差异**：
- Python `kv_store.get()` 返回 dict/string，Go `kvStore.Get()` 返回 `[]byte`
- 备份：Go 用 `json.Marshal`/`json.Unmarshal`，值统一为 `[]byte`
- 版本：Go 用 `strconv.Atoi`/`strconv.Itoa` 转换

### 3.3 VectorMigrator

**Python 对齐**：`openjiuwen/core/memory/migration/migrator/vector_migrator.py`

```go
// VectorMigrator 向量集合迁移执行器。
// 按 entity_key 发现目标集合，调用 VectorStore.UpdateSchema 执行迁移。
//
// Python: openjiuwen/core/memory/migration/migrator/vector_migrator.py (VectorMigrator)
type VectorMigrator struct {
    // vectorStore 向量存储实例
    vectorStore vector.BaseVectorStore
}
```

**核心方法对齐 Python**：

| 方法 | Python | Go | 说明 |
|------|--------|-----|------|
| `TryMigrate` | `try_migrate` | 完全对齐 | 发现集合→迁移集合 |
| `migrateCollections` | `_migrate_collections` | 完全对齐 | 逐集合过滤待执行操作→调用 UpdateSchema→更新 metadata |
| `findCollections` | `_find_collections` | 完全对齐 | 去掉 `vector_` 前缀→SupportMemoryType 校验→ListCollectionNames 过滤 |

### 3.4 MessageMigrator

**Python 对齐**：`openjiuwen/core/memory/migration/migrator/message_migrator.py`

```go
// MessageMigrator 消息存储迁移执行器。
// 版本跟踪通过 BaseMessageStore.GetSchemaVersion/SetSchemaVersion。
// 迁移前创建备份，失败时从备份恢复。
//
// Python: openjiuwen/core/memory/migration/migrator/message_migrator.py (MessageMigrator)
type MessageMigrator struct {
    // messageStore 消息存储实例
    messageStore db.BaseMessageStore
}
```

**核心方法对齐 Python**：

| 方法 | Python | Go | 说明 |
|------|--------|-----|------|
| `TryMigrate` | `try_migrate` | 完全对齐 | 校验 entity_key + 版本比较 + 备份+执行+恢复 |
| `executeOperation` | `_execute_operation` | 完全对齐 | type switch `UpdateMessageOperation` → 调用 `UpdateFunc` |
| `validateOperationsOrder` | `_validate_operations_order` | 完全对齐 | 同 KVMigrator |
| `createBackup` | `_create_backup` | 完全对齐 | `GetMessages` 全量读取 → 序列化为 backup slice |
| `restoreFromBackup` | `_restore_from_backup` | 完全对齐 | `DeleteMessages` 全量删除 → 反序列化 → 逐条 `AddMessage` |

**Go 与 Python 的差异**：
- Python `message_store.get_messages(limit=1000)` 分页，Go `GetMessages` 也有 limit 参数
- Python `BaseMessage` 有 `content`/`role`，Go 的 `schema.BaseMessage` 和 `MessageAdd` 结构对应
- Python 的 `metadata.model_dump(mode="json")`，Go 中 `MessageMetadata` 是结构体，序列化用 `json.Marshal`

### 3.5 IndexVersionMigrator

**Python 对齐**：`openjiuwen/core/memory/migration/migrator/index_version_migrator.py`

```go
// IndexVersionMigrator 记忆索引版本迁移执行器。
// 在同一 BaseMemoryIndex 实例内做 MemoryDoc 字段变换。
// 迁移前创建备份，失败时恢复。
//
// Python: openjiuwen/core/memory/migration/migrator/index_version_migrator.py (IndexVersionMigrator)
type IndexVersionMigrator struct {}
```

**核心方法对齐 Python**：

| 方法 | Python | Go | 说明 |
|------|--------|-----|------|
| `TryMigrate` | `try_migrate` | 完全对齐 | 获取当前版本→过滤→备份→逐个 apply→更新版本→清理备份 |
| `applyOperation` | `_apply_operation` | 完全对齐 | type switch 分发到 4 个子方法 |
| `applyRenameField` | `_apply_rename_field` | 完全对齐 | ListUserScopes → 逐 scope 批量 ListMemories → rename key → DeleteMemories + AddMemories |
| `applyTransformField` | `_apply_transform_field` | 完全对齐 | 同上，但调用 TransformFunc |
| `applyAddField` | `_apply_add_field` | 完全对齐 | DefaultValueOrFunc 运行时判断类型 |
| `applyRemoveField` | `_apply_remove_field` | 完全对齐 | delete doc.Fields[fieldName] |

**注意**：Python 中 IndexVersionMigrator 的 `TryMigrate` 签名是 `try_migrate(self, index, operations)`，不接收 `entity_key`。Go 中 `runIndexVersionMigrations` 需要特殊处理（与 Python 的 `run_index_version_migrations` 一致，不使用 `_run_migrations_with_registry`）。

---

## 四、run_migrations 编排（`migration/run_migrations.go`）

**Python 对齐**：`openjiuwen/core/memory/migration/run_migrations.py`

### 4.1 通用助手函数

所有 Migrator 统一实现 `Migrator` 接口，使 `runMigrationsWithRegistry` 能通用编排：

```go
// Migrator 通用迁移器接口，所有具体 Migrator 必须实现。
type Migrator interface {
    // TryMigrate 尝试执行迁移。
    // entityKey 为实体标识（表名/KV 键/记忆类型等），operations 为待执行的操作列表。
    // 返回 nil 表示成功，非 nil 表示失败。
    TryMigrate(ctx context.Context, entityKey string, operations []operation.Operation) error
}
```

**注意**：`IndexVersionMigrator` 不实现此接口（其 TryMigrate 签名不接受 entityKey），因此 `RunIndexVersionMigrations` 独立实现。

```go
// runMigrationsWithRegistry 通用迁移运行器，处理日志和错误处理。
//
// Python: _run_migrations_with_registry(registry, migrator_factory, store_name)
func runMigrationsWithRegistry(
    ctx context.Context,
    registry *operation.OperationRegistry,
    migrator Migrator,
    storeName string,
) error
```

**逻辑对齐 Python**：
1. `registry.GetAllOperations()` 获取映射
2. 空映射时 Info 日志 + return
3. 逐 entity_key 调用 `migrator.TryMigrate(ctx, entityKey, operations)`
4. 失败时 Error 日志 + 返回 `StatusMemoryMigrateMemoryExecutionError`

**Go 与 Python 差异**：Python 是 `async` 函数，Go 传 `ctx context.Context`；Python 抛异常，Go 返回 `error`。

### 4.2 5 个公开函数

```go
// RunVectorMigrations 执行所有已注册的向量迁移。
// Python: run_vector_migrations(vector_store)
func RunVectorMigrations(ctx context.Context, vectorStore vector.BaseVectorStore) error

// RunKVMigrations 执行所有已注册的 KV 迁移。
// Python: run_kv_migrations(kv_store)
func RunKVMigrations(ctx context.Context, kvStore kv.BaseKVStore) error

// RunSQLMigrations 执行所有已注册的 SQL 迁移。
// Python: run_sql_migrations(sql_db_store)
func RunSQLMigrations(ctx context.Context, sqlDbStore *model.SqlDbStore) error

// RunMessageMigrations 执行所有已注册的消息迁移。
// Python: run_message_migrations(message_store)
func RunMessageMigrations(ctx context.Context, messageStore db.BaseMessageStore) error

// RunIndexVersionMigrations 执行所有已注册的索引版本迁移。
// Python: run_index_version_migrations(index)
func RunIndexVersionMigrations(ctx context.Context, index index.BaseMemoryIndex) error
```

**特殊处理**：`RunIndexVersionMigrations` 不使用 `runMigrationsWithRegistry`（Python 也是独立的实现），因为 `IndexVersionMigrator.TryMigrate` 的签名不接受 `entityKey`，而是直接接收 `index` 和 `operations`。

---

## 五、VectorStore UpdateSchema 回填

### 5.1 通用模式

4 个 VectorStore 的 `UpdateSchema` 实现遵循相同模式（对齐 Python）：

```
1. operations 为空 → 直接返回
2. 获取当前 schema: GetSchema(collectionName)
3. 计算目标 schema: ComputeNewSchema(oldSchema, operations)
4. 构建变换函数: BuildTransformFunc(operations)
5. 获取集合元数据: GetCollectionMetadata(collectionName)
6. 执行迁移: executeMigration(collectionName, newSchema, transformFunc, metadata)
7. 失败时清理临时集合
```

### 5.2 MilvusVectorStore.UpdateSchema

**迁移策略**：创建临时集合 → 流式读旧数据 → 批量写临时 → 删旧 → rename

```
1. 创建临时集合: {collectionName}_migration_{timestamp}
2. 加载旧集合并流式读取文档（Milvus query_iterator 或 GetDocsByFilters 分页）
3. 批量（batch=100）对文档应用 transformFunc，写入临时集合
4. 释放旧集合内存
5. 删除旧集合
6. RenameCollection 临时集合 → 原名
7. 清除 collection_metadata 缓存
8. 失败: 清理临时集合
```

**对齐 Python `MilvusVectorStore._execute_migration`**。

### 5.3 ChromaVectorStore.UpdateSchema

**迁移策略**：创建临时集合 → 全量读 → 批量写临时 → 删旧 → 双重拷贝

```
1. 创建临时集合: {collectionName}_migration_{timestamp}
2. 全量读取旧集合文档
3. 对文档应用 transformFunc，写入临时集合
4. 删除旧集合
5. [ChromaDB 不支持 rename] 双重拷贝:
   a. 全量读取临时集合文档
   b. 创建原名称新集合
   c. 写入文档
   d. 删除临时集合
6. 失败: 清理临时集合
```

**对齐 Python `ChromaVectorStore._execute_migration`**。

### 5.4 GaussVectorStore.UpdateSchema

**迁移策略**：创建临时集合 → SQL cursor 读 → 批量写临时 → 删旧 → ALTER TABLE RENAME

```
1. 创建临时集合: {collectionName}_migration_{timestamp}
2. SQL cursor: SELECT * FROM {collectionName}
3. 批量（batch=100）对行应用 transformFunc，写入临时集合
4. 删除旧集合
5. ALTER TABLE {tempName} RENAME TO {collectionName}
6. 清除 collection_metadata 缓存
7. 失败: 清理临时集合
```

**对齐 Python `GaussVectorStore.update_schema`**。

### 5.5 ESVectorStore.UpdateSchema

**迁移策略**：创建临时索引 → search 读 → 批量写临时 → 删旧 → 双重拷贝

```
1. 创建临时索引: {collectionName}_migration_{timestamp}
2. Search 读取旧索引文档（排除 _metadata_doc_id，size=10000）
3. 对文档应用 transformFunc，写入临时索引
4. 删除旧索引
5. [ES 不支持 rename] 双重拷贝:
   a. Search 读取临时索引文档
   b. 创建原名称新索引
   c. 写入文档
   d. 删除临时索引
6. 失败: 清理临时索引
```

**对齐 Python `ESVectorStore.update_schema`**。

---

## 六、日志同步

按项目日志规则，对照 Python 中所有 `memory_logger` 调用，在 Go 中等价位置使用结构化日志。

| Python | Go |
|--------|-----|
| `memory_logger.info/warning/error` | `logger.Info/Warn/Error(logComponent).Str(...).Msg(...)` |
| `event_type=LogEventType.MEMORY_INIT` | `logger.ComponentAgentCore` + 自定义事件字段 |
| `exception=str(e)` | `.Err(err)` |
| `exc_info=True` | `.Err(err)` + Error 级别 |

**关键日志点（不可省略）**：

- `runMigrationsWithRegistry`：跳过迁移（无注册操作）/ 迁移失败
- `KVMigrator`：版本比较、待执行操作数量、备份创建/恢复/清理、每个操作执行结果
- `MessageMigrator`：同 KVMigrator 的日志点
- `SQLMigrator`：迁移开始/成功/失败、版本更新
- `IndexVersionMigrator`：版本比较、备份创建/恢复/清理
- 4 个 VectorStore UpdateSchema：迁移开始/成功/失败、临时集合创建/删除
- `VectorMigrator.findCollections`：不支持的 memory type 错误

---

## 七、测试策略

### 7.1 Migrator 单元测试（可 mock，不使用 build tag）

| 测试 | 方式 |
|------|------|
| SQLMigrator | 用 `sqlite` 内存数据库 + GORM（`db, _ := gorm.Open(sqlite.Open(":memory:"))`），创建测试表 + MemoryMetaManager |
| KVMigrator | 用 `InMemoryKVStore` fake，注册 prefix，模拟版本变更 |
| VectorMigrator | 用 `fakeVectorStore`（实现 `BaseVectorStore` 接口），验证 findCollections/migrateCollections 调用 |
| MessageMigrator | 用 `fakeMessageStore`（实现 `BaseMessageStore` 接口），验证备份恢复逻辑 |
| IndexVersionMigrator | 用 `SimpleMemoryIndex`（内存实现），验证字段变换 |

### 7.2 run_migrations 单元测试

- 测试 `runMigrationsWithRegistry`：空 Registry → 跳过、有操作 → 成功/失败
- 测试 5 个 `RunXxxMigrations` 函数的编排逻辑

### 7.3 ComputeNewSchema / BuildTransformFunc 单元测试

- 每种操作类型的正向测试
- 校验失败测试（字段不存在、重名字段、向量字段类型更新）
- BuildTransformFunc：AddField 默认值、RenameField、UpdateEmbeddingDimension 零向量 fallback

### 7.4 VectorStore UpdateSchema 测试

- **Milvus**：`//go:build integration` 标签（需真实 Milvus 实例）
- **ChromaDB**：`//go:build integration` 标签（需真实 Chroma 实例）
- **GaussDB**：`//go:build integration` 标签（需真实 GaussDB 实例）
- **ES**：`//go:build integration` 标签（需真实 ES 实例）
- 分发逻辑（不依赖外部服务的部分）可通过 fake 测试

### 7.5 RenameCollection 测试

- Milvus / GaussDB：`//go:build integration` 标签
- ChromaDB / ES：单元测试验证返回 `StatusStoreVectorNotSupported` 错误

---

## 八、回填标记更新

实现完成后，需更新 `IMPLEMENTATION_PLAN.md` 中以下标记：

| 条目 | 变更 |
|------|------|
| 7.21 | ⤵️ 标记移除（Migrator + run_migrations 已回填） |
| 7.22 | ☐ → ✅（类型已在 7.21 完成，⤴️ 回填 UpdateSchema 已完成） |
| 7.23 | ☐ → ✅（Migrator + run_migrations + UpdateSchema 回填全部完成） |
| 4.8 | UpdateSchema 待 7.22/7.23 回填 → 移除"待回填"说明 |
| 4.9 | UpdateSchema 待 7.22/7.23 回填 → 移除"待回填"说明 |
| 4.10 | UpdateSchema 待 7.22/7.23 回填 → 移除"待回填"说明 |
| 4.11 | UpdateSchema 待 7.22/7.23 回填 → 移除"待回填"说明 |

---

## 九、文件清单

### 新增文件

| 文件 | 内容 | 预估行数 |
|------|------|---------|
| `foundation/store/vector/schema_utils.go` | ComputeNewSchema + BuildTransformFunc + 4 个 compute 子函数 + applyOperationToDoc | ~200 |
| `foundation/store/vector/schema_utils_test.go` | 上述函数的单元测试 | ~300 |
| `migration/migrator/sql_migrator.go` | SQLMigrator | ~200 |
| `migration/migrator/sql_migrator_test.go` | SQLMigrator 单元测试 | ~200 |
| `migration/migrator/kv_migrator.go` | KVMigrator | ~250 |
| `migration/migrator/kv_migrator_test.go` | KVMigrator 单元测试 | ~250 |
| `migration/migrator/vector_migrator.go` | VectorMigrator | ~100 |
| `migration/migrator/vector_migrator_test.go` | VectorMigrator 单元测试 | ~100 |
| `migration/migrator/message_migrator.go` | MessageMigrator | ~250 |
| `migration/migrator/message_migrator_test.go` | MessageMigrator 单元测试 | ~250 |
| `migration/migrator/index_version_migrator.go` | IndexVersionMigrator | ~250 |
| `migration/migrator/index_version_migrator_test.go` | IndexVersionMigrator 单元测试 | ~250 |
| `migration/run_migrations.go` | runMigrationsWithRegistry + 5 个 RunXxx 函数 | ~120 |
| `migration/run_migrations_test.go` | 编排逻辑单元测试 | ~150 |

### 修改文件

| 文件 | 变更内容 |
|------|---------|
| `foundation/store/vector/base.go` | UpdateSchema 签名 `[]any` → `[]operation.Operation` + 新增 RenameCollection 接口方法 + 新增 `ErrNotSupported` 便捷变量 |
| `foundation/store/vector/milvus.go` | UpdateSchema 回填 + RenameCollection 实现 |
| `foundation/store/vector/chroma.go` | UpdateSchema 回填 + RenameCollection 返回 not supported |
| `foundation/store/vector/gauss.go` | UpdateSchema 回填 + RenameCollection 实现（ALTER TABLE RENAME） |
| `foundation/store/vector/es.go` | UpdateSchema 回填 + RenameCollection 返回 not supported |
| `foundation/store/vector/base_test.go` | fakeVectorStore 更新 |
| `foundation/store/vector/milvus_test.go` | UpdateSchema stub 测试更新 + RenameCollection 测试 |
| `foundation/store/vector/chroma_test.go` | UpdateSchema stub 测试更新 + RenameCollection 测试 |
| `foundation/store/vector/gauss_test.go` | UpdateSchema stub 测试更新 + RenameCollection 测试 |
| `foundation/store/vector/es_test.go` | UpdateSchema stub 测试更新 + RenameCollection 测试 |
| `foundation/store/index/simple_test.go` | fake 更新 |
| `memory/manage/mem_model/semantic_store_test.go` | fake 更新 |
| `migration/migrator/doc.go` | 更新文件目录 |
| `migration/doc.go` | 更新文件目录 |
