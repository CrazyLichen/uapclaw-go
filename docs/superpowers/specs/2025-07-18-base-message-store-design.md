# BaseMessageStore 消息持久化设计

> 领域四 4.15 小节实现设计

## 1. 概述

本文描述 `BaseMessageStore` 接口及其 SQL 实现在 Go 项目中的设计方案。消息持久化是记忆系统的基础能力，负责对话消息的 CRUD 操作、加密存储、过滤查询和 schema 版本管理。

### 1.1 对齐策略

- **接口签名对齐 Python**，不新增接口方法
- **修正 Python 已知缺陷**：`count_messages` 用 SQL COUNT、`get_messages` 支持 `start_time`/`end_time` 过滤、`add_messages` 真正批量写入
- **存储字段对齐 Python**：只存储 `role` 和 `content`，不保存 `name`、`metadata` 及子类扩展字段

### 1.2 Python 已知缺陷及 Go 修正

| 缺陷 | Python 行为 | Go 修正 |
|------|------------|---------|
| `count_messages` 性能差 | 取回全部数据后 `len()` | 使用 SQL COUNT |
| `get_messages` 过滤不完整 | `start_time`/`end_time` 未实现 | 实现 `StartTime`/`EndTime` 范围查询 |
| `add_messages` 非批量 | 循环调用 `add_message` | 一次 GORM `Create` 批量插入 |
| `BaseMessage` 字段丢失 | `name`/`metadata` 不存储 | 与 Python 对齐，不存储 |

## 2. 包结构

完全对齐 Python 目录映射：

| Python 路径 | Go 路径 | 内容 |
|------------|---------|------|
| `foundation/store/base_message_store.py` | `store/db/base_message_store.go` | `BaseMessageStore` 接口 + 辅助类型 |
| `foundation/store/db/default_db_store.py` | `store/db/` (已有) | `BaseDbStore`、`DefaultDbStore` |
| `memory/manage/mem_model/sql_db_store.py` | `memory/manage/model/sql_db_store.go` | `SqlDbStore` 通用 CRUD 层 |
| `memory/manage/mem_model/sql_message_store.py` | `memory/manage/model/sql_message_store.go` | `SqlMessageStore` 实现 |
| `memory/manage/mem_model/message_manager.py` | `memory/manage/model/message_manager.go` | `MessageManager` 上层管理 |
| `memory/manage/mem_model/db_model.py` | `memory/manage/model/db_model.go` | 数据库模型 |

### 2.1 文件目录树

```
store/
├── db/
│   ├── base.go                       # 已有：BaseDbStore 接口
│   ├── default_db_store.go           # 已有：DefaultDbStore
│   ├── gaussdb/                      # 已有：GaussDbStore
│   ├── base_message_store.go         # 【新增】BaseMessageStore 接口 + 辅助类型
│   └── base_message_store_test.go    # 【新增】接口测试
├── kv/                               # 已有
├── vector/                           # 已有
└── vector_fields/                    # 已有

memory/                               # 【新增】
└── manage/
    └── model/
        ├── doc.go                    # 包文档
        ├── sql_db_store.go           # SqlDbStore 通用 CRUD
        ├── sql_db_store_test.go
        ├── sql_message_store.go      # SqlMessageStore 实现
        ├── sql_message_store_test.go
        ├── db_model.go              # 数据库模型（GORM Model）
        ├── db_model_test.go
        ├── message_manager.go       # MessageManager
        └── message_manager_test.go
```

## 3. 接口设计

### 3.1 BaseMessageStore 接口

```go
// BaseMessageStore 消息持久化接口
// 对应 Python: openjiuwen/core/foundation/store/base_message_store.py
type BaseMessageStore interface {
    // AddMessage 添加单条消息，返回 message_id
    AddMessage(ctx context.Context, messageAdd *MessageAdd) (string, error)

    // AddMessages 批量添加消息，返回 ID 列表
    // 修正：真正批量写入，而非循环调用 AddMessage
    AddMessages(ctx context.Context, messageAdds []*MessageAdd) ([]string, error)

    // GetMessageByID 按 ID 获取消息，不存在时返回错误
    GetMessageByID(ctx context.Context, messageID string) (*BaseMessage, *MessageMetadata, error)

    // GetMessages 按条件过滤查询消息
    // 修正：实现 StartTime/EndTime 过滤（跳过 MessageType）
    GetMessages(ctx context.Context, filter *MessageFilter, limit int, orderBy string, orderDirection string) ([]*MessageAndMeta, error)

    // UpdateMessage 更新消息内容
    UpdateMessage(ctx context.Context, messageID string, content MessageContent) error

    // DeleteMessageByID 按 ID 删除单条消息
    DeleteMessageByID(ctx context.Context, messageID string) error

    // DeleteMessages 按条件删除消息，返回删除数量
    DeleteMessages(ctx context.Context, filter *MessageFilter) (int64, error)

    // CountMessages 统计匹配消息数量
    // 修正：使用 SQL COUNT，而非取回全部数据后 len()
    CountMessages(ctx context.Context, filter *MessageFilter) (int64, error)

    // GetSchemaVersion 获取当前 schema 版本号
    GetSchemaVersion(ctx context.Context) (int32, error)

    // SetSchemaVersion 设置 schema 版本号
    SetSchemaVersion(ctx context.Context, version int32) error
}
```

### 3.2 辅助类型

```go
// MessageMetadata 消息元数据
// 对应 Python: MessageMetadata
type MessageMetadata struct {
    MessageID   string    // 消息唯一标识
    UserID      string    // 用户 ID
    ScopeID     string    // 作用域 ID
    SessionID   string    // 会话 ID
    Timestamp   time.Time // 时间戳（数据库存 string，Go 用 time.Time，GORM Serializer 自动转换）
    MessageType string    // 消息类型
}

// MessageAdd 添加消息的入参
// 对应 Python: message_add 字典
type MessageAdd struct {
    Message   *BaseMessage // 消息对象
    UserID    string       // 用户 ID
    ScopeID   string       // 作用域 ID
    SessionID string       // 会话 ID
    Timestamp time.Time    // 时间戳（零值时自动生成当前时间）
}

// MessageFilter 消息查询过滤条件
// 对应 Python: message_filter 字典
// 修正：实现 StartTime/EndTime 过滤，跳过 MessageType（数据库表无对应列）
type MessageFilter struct {
    UserID    string     // 用户 ID
    ScopeID   string     // 作用域 ID
    SessionID string     // 会话 ID
    StartTime *time.Time // 起始时间（nil 表示不限制）
    EndTime   *time.Time // 结束时间（nil 表示不限制）
}

// MessageAndMeta 消息+元数据组合（用于 GetMessages 返回）
type MessageAndMeta struct {
    Message  *BaseMessage
    Metadata *MessageMetadata
}
```

### 3.3 与 Python 接口的差异汇总

| 方法 | Python 签名 | Go 签名 | 差异说明 |
|------|------------|---------|---------|
| `add_message` | `async def add_message(self, message_add: Dict) -> str` | `AddMessage(ctx, *MessageAdd) (string, error)` | Dict → 强类型 MessageAdd |
| `add_messages` | `async def add_messages(self, message_adds: List[Dict]) -> List[str]` | `AddMessages(ctx, []*MessageAdd) ([]string, error)` | 修正为真正批量写入 |
| `get_message_by_id` | `async def get_message_by_id(self, message_id: str) -> Tuple[BaseMessage, MessageMetadata]` | `GetMessageByID(ctx, string) (*BaseMessage, *MessageMetadata, error)` | 元组 → 多返回值 |
| `get_messages` | `async def get_messages(self, message_filter: Dict, ...) -> List[Tuple[...]]` | `GetMessages(ctx, *MessageFilter, int, string, string) ([]*MessageAndMeta, error)` | 实现 StartTime/EndTime |
| `update_message` | `async def update_message(self, message_id: str, content: Union[str, List]) -> bool` | `UpdateMessage(ctx, string, MessageContent) error` | 返回 error 而非 bool |
| `delete_message_by_id` | `async def delete_message_by_id(self, message_id: str) -> bool` | `DeleteMessageByID(ctx, string) error` | 返回 error 而非 bool |
| `delete_messages` | `async def delete_messages(self, message_filter: Dict) -> int` | `DeleteMessages(ctx, *MessageFilter) (int64, error)` | int → int64 |
| `count_messages` | `async def count_messages(self, message_filter: Dict) -> int` | `CountMessages(ctx, *MessageFilter) (int64, error)` | 用 SQL COUNT |
| `get_schema_version` | `async def get_schema_version(self) -> int \| None` | `GetSchemaVersion(ctx) (int32, error)` | None → error |
| `set_schema_version` | `async def set_schema_version(self, version: int) -> None` | `SetSchemaVersion(ctx, int32) error` | 无返回 → error |

## 4. 实现层设计

### 4.1 SqlDbStore — 通用 SQL CRUD 层

```go
// SqlDbStore 基于 BaseDbStore 的通用 SQL CRUD 封装
// 对应 Python: openjiuwen/core/memory/manage/mem_model/sql_db_store.py
type SqlDbStore struct {
    dbStore BaseDbStore
    db      *gorm.DB
}
```

方法对齐 Python 的 `SqlDbStore`，参数用 `map[string]any`：

| 方法 | 签名 | 说明 |
|------|------|------|
| `NewSqlDbStore` | `func NewSqlDbStore(dbStore BaseDbStore) *SqlDbStore` | 构造，从 BaseDbStore 获取 `*gorm.DB` |
| `Write` | `func (s *SqlDbStore) Write(ctx context.Context, table string, data map[string]any) error` | 插入一行 |
| `ConditionGet` | `func (s *SqlDbStore) ConditionGet(ctx context.Context, table string, conditions map[string]any, columns []string) ([]map[string]any, error)` | 条件查询（IN 子句） |
| `GetWithSort` | `func (s *SqlDbStore) GetWithSort(ctx context.Context, table string, filters map[string]any, sortBy string, order string, limit int) ([]map[string]any, error)` | 过滤+排序+分页 |
| `Update` | `func (s *SqlDbStore) Update(ctx context.Context, table string, conditions map[string]any, data map[string]any) error` | 条件更新 |
| `Delete` | `func (s *SqlDbStore) Delete(ctx context.Context, table string, conditions map[string]any) error` | 条件删除 |
| `Exist` | `func (s *SqlDbStore) Exist(ctx context.Context, table string, conditions map[string]any) (bool, error)` | 存在性检查 |
| `Count` | `func (s *SqlDbStore) Count(ctx context.Context, table string, conditions map[string]any) (int64, error)` | SQL COUNT（Python 无，Go 新增） |

**Python 无但 Go 新增的方法**：`Count` — Python 的 `count_messages` 用 `GetWithSort` 取回全部后 `len()`，Go 在 `SqlDbStore` 层直接提供 SQL COUNT 能力。

### 4.2 数据库模型

```go
// UserMessage 用户消息表模型
// 对应 Python: UserMessage (MessageMixin + Base)
type UserMessage struct {
    MessageID string `gorm:"primaryKey;size:64"`  // 消息唯一标识
    UserID    string `gorm:"size:64;not null"`     // 用户 ID
    ScopeID   string `gorm:"size:64;not null"`     // 作用域 ID
    Content   string `gorm:"size:4096;not null"`   // 消息内容（AES 加密后存储）
    SessionID string `gorm:"size:64"`              // 会话 ID
    Role      string `gorm:"size:32"`              // 消息角色
    Timestamp string `gorm:"size:32"`              // 时间戳（ISO 字符串，对齐 Python）
}

// TableName 指定表名
func (UserMessage) TableName() string { return "user_message" }
```

与 Python 完全对齐：7 列，类型和约束一一对应。`Timestamp` 仍是 `string` 存 ISO 格式，读取时转换为 `time.Time`。

### 4.3 SqlMessageStore

```go
// SqlMessageStore SQL 消息存储实现
// 对应 Python: openjiuwen/core/memory/manage/mem_model/sql_message_store.py
type SqlMessageStore struct {
    cryptoKey   []byte            // AES 加密密钥（可选，为空时不加密）
    sqlDbStore  *SqlDbStore       // CRUD 层
    tableName   string            // 默认 "user_message"
    codec       *AesStorageCodec  // 内容编解码器
}
```

**关键实现逻辑**：

| 方法 | 实现要点 |
|------|---------|
| `AddMessage` | 生成 message_id（SHA-256 hash）→ 加密 content → 组装行数据 → `sqlDbStore.Write` |
| `AddMessages` | 真正批量写入：构造多条行数据，一次 GORM `Create` 插入 |
| `GetMessageByID` | `sqlDbStore.ConditionGet` → 不存在抛 `StatusStoreMessageNotFound` → 解密 content → 构造 `BaseMessage` + `MessageMetadata` |
| `GetMessages` | `sqlDbStore.GetWithSort`（含 StartTime/EndTime 范围查询）→ 逐条解密 → 构造返回列表 |
| `UpdateMessage` | 加密新 content → `sqlDbStore.Update` |
| `DeleteMessageByID` | `sqlDbStore.Delete` |
| `DeleteMessages` | 先 `CountMessages` 获取数量 → `sqlDbStore.Delete` → 返回删除数量 |
| `CountMessages` | `sqlDbStore.Count`（SQL COUNT，不取回数据） |
| `GetSchemaVersion` / `SetSchemaVersion` | 通过 `MemoryMetaManager` 操作（4.16 范围，本次预留接口） |

**消息 ID 生成**：

```go
// generateMessageID 基于 content + timestamp 生成消息 ID
// 对应 Python: _generate_message_id
// 格式: msg_{sha256(content_json+timestamp)[:16]}_{timestamp_ms}
func generateMessageID(content string, timestamp time.Time) string
```

**加密/解密**：复用已有的 `crypto.AesGcmProvider`，仅对 `content` 字段加密存储。`cryptoKey` 为空时 passthrough 不加密。

### 4.4 MessageManager

```go
// MessageManager 消息管理器，BaseMessageStore 的上层封装
// 对应 Python: openjiuwen/core/memory/manage/mem_model/message_manager.py
type MessageManager struct {
    store BaseMessageStore
}

// MessageAddRequest 添加消息请求
type MessageAddRequest struct {
    UserID    string
    ScopeID   string
    Content   string
    Role      string
    SessionID string
    Timestamp time.Time
}
```

| 方法 | 签名 | 说明 |
|------|------|------|
| `NewMessageManager` | `func NewMessageManager(store BaseMessageStore) *MessageManager` | 构造 |
| `Add` | `func (m *MessageManager) Add(ctx context.Context, req *MessageAddRequest) (string, error)` | 验证必填字段后调用 store.AddMessage |
| `Get` | `func (m *MessageManager) Get(ctx context.Context, userID, scopeID, sessionID string, messageLen int) ([]*MessageAndMeta, error)` | 倒序获取后反转 |
| `GetByID` | `func (m *MessageManager) GetByID(ctx context.Context, msgID string) (*MessageAndMeta, error)` | 按 ID 获取，不存在返回 nil |
| `DeleteByUserAndScope` | `func (m *MessageManager) DeleteByUserAndScope(ctx context.Context, userID, scopeID string) (int64, error)` | 删除指定用户+作用域的所有消息 |

## 5. 错误处理

### 5.1 错误码

在 `exception/codes_framework.go` 中新增：

```go
StatusStoreMessageGetExecutionError    = 187000  // 消息获取执行错误
StatusStoreMessageAddExecutionError    = 187001  // 消息添加执行错误
StatusStoreMessageUpdateExecutionError = 187002  // 消息更新执行错误
StatusStoreMessageDeleteExecutionError = 187003  // 消息删除执行错误
StatusStoreMessageNotFound             = 187004  // 消息不存在
StatusStoreMessageCountExecutionError  = 187005  // 消息计数执行错误
```

### 5.2 日志规则

- 每个 error 返回分支添加 `logger.Error` 日志，包含 `event_type=STORE_MESSAGE_ERROR`、`method`、`message_id` 等上下文字段
- `GetMessageByID` 不存在时抛 `StatusStoreMessageNotFound`
- `SqlDbStore` 层 SQL 执行失败用 `exception.BuildError` 包装，不暴露底层 GORM 错误
- 使用组件常量 `logger.ComponentAgentCore`

## 6. 测试策略

| 层级 | 文件 | 测试方式 | 覆盖目标 |
|------|------|---------|---------|
| `BaseMessageStore` 接口 | `store/db/base_message_store_test.go` | 定义 mock 实现验证接口契约 | 接口签名正确性 |
| `SqlDbStore` | `memory/manage/model/sql_db_store_test.go` | SQLite 内存数据库 | 全部 8 方法 |
| `SqlMessageStore` | `memory/manage/model/sql_message_store_test.go` | SQLite 内存数据库 | 全部 9 方法 + 加密/解密 |
| `MessageManager` | `memory/manage/model/message_manager_test.go` | mock `BaseMessageStore` | 验证/转发逻辑 |
| `db_model` | `memory/manage/model/db_model_test.go` | GORM AutoMigrate + 字段验证 | 表结构正确性 |

**测试要点**：
- 不用 build tag：全部用 SQLite 内存数据库（`:memory:`）通过 GORM 测试
- 加密测试：`cryptoKey` 为空时 passthrough，有 key 时验证加解密往返
- 消息 ID 生成测试：验证确定性和格式正确性
- 过滤测试：重点验证 `StartTime`/`EndTime` 范围查询
- CountMessages 测试：验证用 SQL COUNT
- AddMessages 测试：验证真正批量写入

## 7. 依赖关系

```
BaseMessageStore (store/db/base_message_store.go)
        ▲
        │ implements
        │
SqlMessageStore (memory/manage/model/sql_message_store.go)
        │
        ├──→ SqlDbStore (memory/manage/model/sql_db_store.go)
        │         │
        │         └──→ BaseDbStore (store/db/base.go) → *gorm.DB
        │
        ├──→ crypto.AesGcmProvider (internal/common/crypto/)
        │
        └──→ schema.BaseMessage (internal/agentcore/foundation/llm/schema/)

MessageManager (memory/manage/model/message_manager.go)
        │
        └──→ BaseMessageStore (依赖接口，不依赖实现)
```

## 8. 实现范围

本次 4.15 小节的实现范围：

1. `store/db/base_message_store.go` — `BaseMessageStore` 接口 + `MessageMetadata` + `MessageAdd` + `MessageFilter` + `MessageAndMeta`
2. `memory/manage/model/sql_db_store.go` — `SqlDbStore` 通用 CRUD 层
3. `memory/manage/model/sql_message_store.go` — `SqlMessageStore` 实现
4. `memory/manage/model/db_model.go` — `UserMessage` GORM 模型
5. `memory/manage/model/message_manager.go` — `MessageManager` 上层管理
6. 各文件对应的 `_test.go` 测试文件
7. 各包的 `doc.go` 文档文件
8. `exception/codes_framework.go` 新增错误码
9. `store/db/doc.go` 更新文件目录
