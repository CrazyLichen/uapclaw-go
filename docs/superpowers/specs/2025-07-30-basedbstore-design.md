# BaseDbStore 设计文档

> 日期：2025-07-30
> 对应 Python：`openjiuwen/core/foundation/store/base_db_store.py`
> 实现位置：`internal/agentcore/store/db/`

## 1. 概述

BaseDbStore 是 SQL 数据库的连接抽象接口，为记忆系统（SqlDbStore、SqlMessageStore 等）提供
数据库引擎的依赖注入点。它本身不提供任何数据存储能力，唯一的职责是暴露 `*gorm.DB` 实例，
让上层组件通过接口获取数据库连接，而非直接依赖具体引擎。

## 2. 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 接口方法 | `GetDB(ctx) *gorm.DB` | 对等 Python 的 `get_async_engine() -> AsyncEngine`，`*gorm.DB` 是 Go 中多后端统一的数据库引擎抽象 |
| 包位置 | `internal/agentcore/store/db/` | 与 `kv/`、`vector/` 平行，保持 store 层组织一致性 |
| GaussDbStore 位置 | 同在 `db/` 包下 | 与 GaussVectorStore/ESVectorStore 放 vector/ 包的模式一致，Go 项目无 extensions 目录 |
| 方言注册 | 暂不实现 | Go 中 GORM 通过 pgx 驱动连接 GaussDB 基本能工作，与 GaussVectorStore 使用 pgxpool 的方式一致 |
| 构造方式 | 4.13/4.14 中实现 | 4.12 只定义接口，具体 DefaultDbStore/GaussDbStore 的构造方式在后续步骤中设计 |

## 3. Python 对比

### Python 的 BaseDbStore

```python
class BaseDbStore(ABC):
    @abstractmethod
    def get_async_engine(self) -> AsyncEngine:
        pass
```

### Python 中的消费者

BaseDbStore **仅供记忆系统使用**，调用链：

```
LongTermMemory.register_store(kv_store, vector_store, db_store=BaseDbStore)
  ├── create_tables(db_store)         → 建表
  ├── SqlDbStore(db_store)            → 通用 CRUD 包装器
  │     └── SqlMessageStore           → 消息专用操作
  └── SQLMigrator(sql_db_store)       → Schema 迁移
```

### 与 KV/Vector 的关系

三者是**平级的、互不包含**：

| 存储 | 职责 | 被 LongTermMemory 怎么用 |
|------|------|------------------------|
| BaseKVStore | 存键值对 | 直接用；与 vector_store 组合成 SimpleMemoryIndex |
| BaseVectorStore | 存向量+检索 | 直接用；与 kv_store 组合成索引 |
| BaseDbStore | 提供数据库连接 | 用来创建 SqlDbStore → SqlMessageStore，做消息持久化 |

- BaseDbStore **不包装** BaseKVStore 或 BaseVectorStore
- DbBasedKVStore 也**不依赖** BaseDbStore，它直接接收 AsyncEngine

## 4. Go 接口定义

```go
// BaseDbStore SQL 数据库抽象，提供数据库引擎访问
//
// 本接口是数据库连接的依赖注入点，让上层组件（SqlDbStore、SqlMessageStore 等）
// 通过接口获取数据库连接，而非直接依赖具体引擎。
// 对应 Python：openjiuwen/core/foundation/store/base_db_store.py
type BaseDbStore interface {
    // GetDB 返回 GORM 数据库实例，调用者可使用返回值执行数据库操作
    GetDB(ctx context.Context) *gorm.DB
}
```

## 5. 后续实现（4.13 / 4.14）

| 步骤 | 类型 | 说明 |
|------|------|------|
| 4.13 | DefaultDbStore | 持有 `*gorm.DB`，`GetDB()` 直接返回；对等 Python DefaultDbStore |
| 4.14 | GaussDbStore | 持有 `*gorm.DB`，`GetDB()` 直接返回；对等 Python GaussDbStore，暂不实现方言注册 |

## 6. 文件结构

```
internal/agentcore/store/db/
├── doc.go              # 包文档
└── base.go             # BaseDbStore 接口定义
```

4.13/4.14 实现后追加：
```
├── default.go          # DefaultDbStore
└── gauss.go            # GaussDbStore
```

## 7. 测试策略

4.12 只定义接口，测试验证：
- 接口方法签名正确
- 可以被 mock（用于后续 SqlDbStore / SqlMessageStore 的测试）

4.13/4.14 实现时再补充具体测试用例。

## 8. 依赖变更

4.12 无新增依赖（`*gorm.DB` 已在 `kv/db_based.go` 中使用）。
