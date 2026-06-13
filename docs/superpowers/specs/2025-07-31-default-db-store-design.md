# DefaultDbStore 设计文档

## 概述

DefaultDbStore 是 BaseDbStore 接口的默认实现，持有并暴露 `*gorm.DB` 实例，适用于所有 GORM 支持的数据库（SQLite、PostgreSQL、MySQL 等）。

对应 Python：`openjiuwen/core/foundation/store/db/default_db_store.py`

## 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 构造方式 | 直接注入 `*gorm.DB` | 与 Python 对等，职责最单一，调用方管理连接生命周期 |
| Close() 方法 | 不提供 | 与 Python 对等，AsyncEngine 由外部管理 |
| 文件组织 | 同包内新增 `default.go` | 接口与实现同包，自然且无跨包引用复杂度 |
| 构造函数签名 | `NewDefaultDbStore(db *gorm.DB) *DefaultDbStore` | 简单直接，不做 nil 校验，与 Python 对等 |
| 日志 | 无 | Python 端 DefaultDbStore 自身无日志 |

## Python 对照

```python
# Python — 极其精简，仅持有 AsyncEngine
class DefaultDbStore(BaseDbStore):
    def __init__(self, async_conn: AsyncEngine):
        self.async_conn = async_conn

    def get_async_engine(self) -> AsyncEngine:
        return self.async_conn
```

| Python | Go |
|--------|-----|
| `AsyncEngine` | `*gorm.DB` |
| `__init__(self, async_conn)` | `NewDefaultDbStore(db *gorm.DB)` |
| `get_async_engine()` | `GetDB(ctx context.Context)` |

## 文件结构

```
internal/agentcore/store/db/
├── doc.go           # 包文档（更新文件目录）
├── base.go          # BaseDbStore 接口（已有）
├── base_test.go     # 接口验证测试（已有）
├── default.go       # DefaultDbStore 实现（新增）
└── default_test.go  # DefaultDbStore 测试（新增）
```

## 接口实现

```go
type DefaultDbStore struct {
    db *gorm.DB
}

func NewDefaultDbStore(db *gorm.DB) *DefaultDbStore {
    return &DefaultDbStore{db: db}
}

func (s *DefaultDbStore) GetDB(_ context.Context) *gorm.DB {
    return s.db
}
```

## 测试计划

| 测试用例 | 说明 |
|---------|------|
| 编译期接口满足 | `var _ BaseDbStore = (*DefaultDbStore)(nil)` |
| NewDefaultDbStore 构造 | 验证返回非 nil 实例 |
| GetDB 返回正确的 *gorm.DB | 使用 sqlite 创建真实实例，验证 GetDB 返回值 |
| GetDB 忽略 context | 传入不同 context，返回值不变 |
