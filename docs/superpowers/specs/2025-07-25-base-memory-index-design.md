# BaseMemoryIndex 接口设计

## 概述

本文档描述 `internal/agentcore/store/index/` 包的实现设计，对应实现计划步骤 4.17 — BaseMemoryIndex 接口。

该包提供记忆索引的抽象接口和数据模型，支持记忆文档的 CRUD、语义搜索、备份恢复等操作。记忆文档以 `user_id` 和 `scope_id` 隔离，支持多租户和多场景的记忆管理。

**Python 参考路径**：`openjiuwen/core/foundation/store/base_memory_index.py`

## 设计决策

| 决策项 | 选择 | 原因 |
|--------|------|------|
| 接口分层策略 | 单接口 + 嵌入结构体 | 1:1 对齐 Python ABC + 默认实现，实现者嵌入 MemoryIndexBase 获得默认行为 |
| 可选参数表示 | nil/空切片 = 不过滤 | 与 Python `None`/`[]` 统一处理一致，Go 惯用法 |
| 搜索结果类型 | `MemorySearchResult` 具名结构体 | 类型安全、可扩展，优于平行切片 |
| StorageCodec 位置 | 同文件 `index/base.go` | 与 Python 一致，index 包本身不大 |
| MemoryIndexBase 位置 | 同文件 `index/base.go` | 与 Python 一致，方便查阅 |
| set_storage_codec | 放入接口强制实现 | 与 Python `@abstractmethod` 一致 |

## 文件布局

```
internal/agentcore/store/index/
├── doc.go           # 包文档
├── base.go          # StorageCodec + MemoryDoc + MemorySearchResult + UserScope + BaseMemoryIndex + MemoryIndexBase
└── base_test.go     # 单元测试
```

## 核心数据类型

### StorageCodec 接口

存储编解码器接口，用于对记忆文本进行加解密。

```go
// StorageCodec 存储编解码器接口，用于对记忆文本进行加解密。
//
// 对应 Python: openjiuwen/core/foundation/store/base_memory_index.py (StorageCodec)
type StorageCodec interface {
    // Encode 对文本进行编码（如加密）
    Encode(text string) string
    // Decode 对数据进行解码（如解密）
    Decode(data string) string
}
```

### MemoryDoc 结构体

记忆文档，表示一条存储的记忆条目。

```go
// MemoryDoc 记忆文档，表示一条存储的记忆条目。
//
// 对应 Python: openjiuwen/core/foundation/store/base_memory_index.py (MemoryDoc)
type MemoryDoc struct {
    // ID 唯一标识
    ID string `json:"id"`
    // Text 文本内容
    Text string `json:"text"`
    // Type 类型/分类
    Type string `json:"type"`
    // Timestamp 时间戳
    Timestamp time.Time `json:"timestamp"`
    // Fields 扩展字段
    Fields map[string]any `json:"fields,omitempty"`
}
```

与 Python 对齐说明：
- Python `default=""` / `default_factory=dict` → Go 零值
- Python `timestamp` 默认 `datetime.now(timezone.utc).astimezone()` → Go 中由构造函数或 AddMemories 时设置

### MemorySearchResult 结构体

记忆搜索结果，包含匹配文档和相关度分数。

```go
// MemorySearchResult 记忆搜索结果，包含匹配文档和相关度分数。
//
// 对应 Python: search 方法返回的 tuple[MemoryDoc, float]
type MemorySearchResult struct {
    // Doc 匹配的记忆文档
    Doc *MemoryDoc
    // Score 相关度分数，范围 [0, 1]，越高越相关
    Score float64
}
```

### UserScope 结构体

用户-作用域对，用于 ListUserScopes 返回值。

```go
// UserScope 用户-作用域对，用于 ListUserScopes 返回值。
//
// 对应 Python: list_user_scopes 返回的 tuple[str, str]
type UserScope struct {
    // UserID 用户标识
    UserID string
    // ScopeID 作用域标识
    ScopeID string
}
```

## BaseMemoryIndex 接口

```go
// BaseMemoryIndex 记忆索引抽象接口，定义记忆文档的存储和检索操作。
//
// 所有记忆索引实现必须实现此接口。记忆文档以 user_id 和 scope_id 隔离，
// 支持多租户和多场景的记忆管理。
//
// 对应 Python: openjiuwen/core/foundation/store/base_memory_index.py (BaseMemoryIndex)
type BaseMemoryIndex interface {
    // ─── 抽象方法（实现者必须实现） ───

    // SetStorageCodec 设置存储编解码器。
    SetStorageCodec(codec StorageCodec)

    // AddMemories 添加新的记忆文档。
    AddMemories(ctx context.Context, userID string, scopeID string, memories []*MemoryDoc) error

    // UpdateMemories 更新记忆文档。
    UpdateMemories(ctx context.Context, userID string, scopeID string, memories []*MemoryDoc) error

    // DeleteMemories 按 ID 删除记忆文档。
    DeleteMemories(ctx context.Context, userID string, scopeID string, ids []string) error

    // DeleteByUser 删除指定用户的所有记忆（跨所有 scope）。
    DeleteByUser(ctx context.Context, userID string) error

    // DeleteByScope 删除指定 scope 的所有记忆（跨所有 user）。
    DeleteByScope(ctx context.Context, scopeID string) error

    // DeleteByUserAndScope 删除指定用户和 scope 组合的所有记忆。
    DeleteByUserAndScope(ctx context.Context, userID string, scopeID string) error

    // Search 语义搜索记忆文档，返回最相关的结果及相关度分数。
    // memTypes 为 nil 或空切片时搜索所有类型；topK 为 0 时使用默认值 10。
    Search(ctx context.Context, userID string, scopeID string, query string, memTypes []string, topK int) ([]*MemorySearchResult, error)

    // GetByID 按 ID 获取单条记忆文档，不存在时返回 nil, nil。
    GetByID(ctx context.Context, userID string, scopeID string, memID string) (*MemoryDoc, error)

    // ─── 有默认实现的方法（通过嵌入 MemoryIndexBase 获得） ───

    // ListMemories 分页获取记忆文档列表。
    // memTypes 为 nil 或空切片时返回所有类型；多个 memType 时按 memType 顺序排列。
    ListMemories(ctx context.Context, userID string, scopeID string, offset int, limit int, memTypes []string) ([]*MemoryDoc, error)

    // GetSchemaVersion 获取当前 schema 版本号，未设置时返回 0。
    GetSchemaVersion() int

    // UpdateSchemaVersion 更新 schema 版本号。
    UpdateSchemaVersion(version int)

    // CreateBackup 创建当前数据的备份，返回备份标识。
    CreateBackup(ctx context.Context) (string, error)

    // RestoreBackup 从备份恢复数据。
    RestoreBackup(ctx context.Context, backupID string) error

    // CleanupBackup 清理备份。
    CleanupBackup(ctx context.Context, backupID string) error

    // ListUserScopes 列出索引中所有 (userID, scopeID) 对。
    ListUserScopes(ctx context.Context) ([]UserScope, error)
}
```

### Python 方法映射表

| Python 方法 | Go 方法 | 变化说明 |
|------------|---------|---------|
| `search → list[tuple[MemoryDoc, float]]` | `Search → []*MemorySearchResult` | 具名结构体替代元组 |
| `list_user_scopes → list[tuple[str, str]]` | `ListUserScopes → []UserScope` | 具名结构体替代元组 |
| `mem_types: list[str] \| None` | `memTypes []string` | nil/空切片 = 不过滤 |
| `top_k: int = 10` | `topK int` | 0 时使用默认值 10 |
| `get_by_id → MemoryDoc \| None` | `GetByID → (*MemoryDoc, error)` | nil 表示不存在 |

## MemoryIndexBase 默认实现

嵌入此结构体后，实现类只需实现核心抽象方法即可满足 BaseMemoryIndex 接口。

```go
// MemoryIndexBase 记忆索引的默认实现基类。
//
// 嵌入此结构体后，实现类只需实现核心抽象方法即可满足 BaseMemoryIndex 接口。
// 默认提供 ListMemories / GetSchemaVersion / UpdateSchemaVersion /
// CreateBackup / RestoreBackup / CleanupBackup / ListUserScopes 的通用行为。
//
// 对应 Python: BaseMemoryIndex 中的非抽象方法默认实现
type MemoryIndexBase struct {
    // schemaVersion schema 版本号
    schemaVersion int
    // backups 备份数据（内存中的简单实现）
    backups map[string]*backupData
}

// backupData 备份数据
type backupData struct {
    // SchemaVersion 备份时的 schema 版本
    SchemaVersion int
}
```

### 各方法默认实现逻辑

| 方法 | 默认行为 | 对齐 Python |
|------|---------|------------|
| `ListMemories` | 返回 nil, nil | Python 默认 `pass` 返回 None |
| `GetSchemaVersion` | 返回 schemaVersion 字段 | Python: `return self._schema_version` |
| `UpdateSchemaVersion` | 设置 schemaVersion 字段 | Python: `self._schema_version = version` |
| `CreateBackup` | 生成 UUID，存入 backups map，返回 ID | Python: `uuid.uuid4()` + `self._backups[bid]` |
| `RestoreBackup` | 从 backups map 恢复 schemaVersion；不存在时返回 StatusMemoryBackupNotFound | Python: `raise ValueError(f"Backup {backup_id} not found")` |
| `CleanupBackup` | 从 backups map 删除 | Python: `self._backups.pop(backup_id, None)` |
| `ListUserScopes` | 返回 nil, nil | Python 默认 `pass` |

### 构造函数

```go
// NewMemoryIndexBase 创建记忆索引基类实例。
func NewMemoryIndexBase() *MemoryIndexBase {
    return &MemoryIndexBase{
        backups: make(map[string]*backupData),
    }
}
```

## 错误处理

### 新增错误码

在 `internal/common/exception/codes_context.go` 的 Memory Engine 区间（158000-159999）新增：

| 错误码 | 编号 | 含义 | 对齐 Python |
|--------|------|------|------------|
| `StatusMemoryBackupNotFound` | 158011 | 备份不存在 | Python: `ValueError(f"Backup {backup_id} not found")` |

### 复用已有错误码

以下错误码已存在，后续 SimpleMemoryIndex（步骤 4.18）可直接复用：

| 错误码 | 编号 | 用途 |
|--------|------|------|
| `StatusMemoryAddMemoryExecutionError` | 158002 | 添加记忆失败 |
| `StatusMemoryDeleteMemoryExecutionError` | 158003 | 删除记忆失败 |
| `StatusMemoryUpdateMemoryExecutionError` | 158004 | 更新记忆失败 |
| `StatusMemoryGetMemoryExecutionError` | 158005 | 获取记忆失败 |

## 日志

使用项目规范中的 `logger` 体系，组件常量使用 `ComponentCommon`（store 属于基础设施层）。

```go
const logComponent = logger.ComponentCommon
```

对齐 Python 中的 `memory_logger` 调用：
- `memory_logger.error(...)` → `logger.Error(logComponent).Err(err).Msg(...)`
- 异常路径必须包含 `event_type`、`method`、上下文字段

## 依赖

| 依赖 | 用途 | 项目已有 |
|------|------|---------|
| `context` | 标准库，方法首参 | 是 |
| `time` | 标准库，MemoryDoc.Timestamp | 是 |
| `github.com/google/uuid` | CreateBackup 生成 UUID | 是（v1.6.0） |
| `github.com/uapclaw/uapclaw-go/internal/common/exception` | 错误体系 | 是 |
| `github.com/uapclaw/uapclaw-go/internal/common/logger` | 日志体系 | 是 |

## 测试策略

### 测试文件：`index/base_test.go`

#### MemoryDoc 测试
- JSON 序列化/反序列化字段完整性
- Fields 为 nil 时的 `omitempty` 行为
- Timestamp 零值处理

#### MemorySearchResult 测试
- 结构体字段访问

#### UserScope 测试
- 结构体字段访问

#### StorageCodec 测试
- 使用 fake 实现验证接口约束

#### MemoryIndexBase 默认实现测试

| 测试用例 | 验证内容 |
|---------|---------|
| `TestNewMemoryIndexBase` | 构造函数初始化 backups map |
| `TestGetSchemaVersion_默认值` | 初始版本号为 0 |
| `TestUpdateSchemaVersion` | 设置后 GetSchemaVersion 返回新值 |
| `TestCreateBackup` | 返回非空 ID，backups map 中有记录 |
| `TestRestoreBackup_存在` | 恢复后 schemaVersion 正确 |
| `TestRestoreBackup_不存在` | 返回 `StatusMemoryBackupNotFound` 错误 |
| `TestCleanupBackup` | 清理后 backups map 中无该记录 |
| `TestListMemories_默认` | 返回 nil, nil |
| `TestListUserScopes_默认` | 返回 nil, nil |

#### 覆盖率目标
- `base.go` 覆盖率 ≥ 85%
- 无需 build tag（不依赖外部环境）

### 不在本步骤测试的内容
- `SimpleMemoryIndex`（步骤 4.18）
- 真实 Embedding/VectorStore 调用
