# 4.16 SqlMessageStore 全链路改进设计

## 概述

对领域四第 4.16 步骤（SqlMessageStore）进行全面改进，覆盖 SqlMessageStore 内部缺陷修复、依赖层 SqlDbStore 扩展、AesStorageCodec 独立化、MemoryMetaManager 独立化，与 Python 实现全链路对齐。

## 问题清单

### A. AddMessages 非真正批量写入

当前代码注释称"真正批量写入"，但实际循环调用 `sqlDbStore.Write()`（每条一次 INSERT）。需改为 GORM 批量 Create。

### B. 加解密策略不够严谨

- 加解密逻辑内嵌在 SqlMessageStore 中（encodeContent/decodeContent），应委托给独立的 AesStorageCodec
- 加密失败时静默返回原文（容错设计），应改为严格模式：key 为空 passthrough，key 非空加密失败返回 error
- 构造时 `cryptoKey` 长度未校验

### C. memoryMetaManager 未独立

Python 中 MemoryMetaManager 是独立类文件，Go 中以非导出类型内嵌在 sql_message_store.go 里。且 Go 缺少 `DeleteByTableName` 方法。

### D. rowToMessageAndMeta 类型断言不安全

大量使用 `,_` 忽略类型断言失败，可能导致数据丢失。

### E. Content 序列化不支持多模态

当前用 `Content.String()` 只取文本，多模态消息（图片+文本列表）会丢失结构信息。应改用 `json.Marshal/Unmarshal` 序列化 `MessageContent`，对齐 Python 的 `Union[str, List[Union[str, dict]]]` 支持。

### F. SqlDbStore 方法对齐度不足

Python 有 `batch_get`、`delete_table`、`invalidate_table_cache`、`get_table`（表反射缓存）、`get`（按主键查询单条）等方法，Go 中缺失。`ConditionGet` 缺少类型校验，`GetWithSort` 缺少排序列校验。

### G. 缺少 AesStorageCodec 独立包

Python 有 `openjiuwen/core/memory/codec/aes_storage_codec.py` 作为独立编解码器，供 SqlMessageStore 和未来 MemoryIndex 共用。Go 中缺失。

## 设计

### 1. AesStorageCodec 独立包

**包路径**：`internal/agentcore/memory/codec/`

**文件结构**：

```
codec/
├── doc.go
├── aes_storage_codec.go
└── aes_storage_codec_test.go
```

**核心类型**：

```go
type AesStorageCodec struct {
    key []byte
}

func NewAesStorageCodec(key []byte) (*AesStorageCodec, error)
func (c *AesStorageCodec) Encode(plaintext string) (string, error)
func (c *AesStorageCodec) Decode(ciphertext string) (string, error)
```

**行为规则**：

- `key` 为 nil/空 → passthrough 模式，Encode/Decode 原样返回
- `key` 非空 → 构造时校验必须为 32 字节，否则返回 error
- key 非空时加密/解密失败 → 返回 error（严格模式，与 Python 容错模式不同）
- 底层委托给 `crypto.AesGcmProvider`

### 2. MemoryMetaManager 独立包

**包路径**：`internal/agentcore/memory/migration/migrator/`

**文件结构**：

```
migration/
└── migrator/
    ├── doc.go
    ├── memory_meta_manager.go
    └── memory_meta_manager_test.go
```

**核心类型**：

```go
type MemoryMetaManager struct {
    sqlDbStore *model.SqlDbStore
    metaTable  string  // 固定 "memory_meta"
}

func NewMemoryMetaManager(sqlDbStore *model.SqlDbStore) *MemoryMetaManager
func (m *MemoryMetaManager) Add(ctx context.Context, tableName string, schemaVersion string) error
func (m *MemoryMetaManager) GetByTableName(ctx context.Context, tableName string) ([]map[string]any, error)
func (m *MemoryMetaManager) DeleteByTableName(ctx context.Context, tableName string) error  // 新增，对齐 Python
```

**行为规则**：

- `Add`：tableName 或 schemaVersion 为空时静默返回 nil；先 Exist 检查，已存在则跳过（幂等）
- `DeleteByTableName`：补齐 Python 中存在但 Go 缺失的方法
- Python 返回 `bool`/`list|None`，Go 返回 `error`/`[]map[string]any`（项目惯例）

**包依赖方向**：

```
migration/migrator → memory/manage/model (SqlDbStore)
```

无循环依赖。

### 3. SqlDbStore 依赖层扩展

#### 新增方法

| 方法 | 签名 | 对齐 Python | 说明 |
|------|------|-------------|------|
| `CreateBatch` | `(ctx, table, rows []map[string]any) error` | 新增 | 批量插入，供 AddMessages 使用 |
| `BatchGet` | `(ctx, table, conditionsList []map[string]any) ([]map[string]any, error)` | `batch_get` | 多组 OR 条件查询 |
| `Get` | `(ctx, table, conditions map[string]any, columns []string) (map[string]any, error)` | `get` | 按条件查询单条（limit 1） |
| `DeleteTable` | `(ctx, tableName string) error` | `delete_table` | 整表 DROP |
| `GetTable` | `(ctx, tableName string) ([]string, error)` | `get_table` | 获取表列名列表（带缓存） |
| `InvalidateTableCache` | `(tableName string)` | `invalidate_table_cache` | 清除列名缓存 |

#### 已有方法改进

| 方法 | 改进 |
|------|------|
| `ConditionGet` | 增加 values 类型校验：必须为切片类型，否则返回 error（对齐 Python 严格校验） |
| `GetWithSort` | 增加 sortBy 列存在性校验：若列不存在返回 error（对齐 Python 的 build_error 逻辑） |

#### 表列名缓存设计

```go
type SqlDbStore struct {
    dbStore    db.BaseDbStore
    db         *gorm.DB
    tableCache sync.Map  // 表名 → []string（列名列表）
}
```

- `GetTable` 返回 `[]string`（列名列表），而非 Python 的 SQLAlchemy Table 对象（GORM 不需要）
- 缓存用于 `GetWithSort` 排序列校验和 `ConditionGet` 过滤列校验
- `InvalidateTableCache` 清除指定表的缓存

#### CreateBatch 实现

使用 GORM `db.Table(table).Create(rows)` 一次性批量 INSERT，rows 为空时直接返回 nil。

#### Get 方法

Python 硬编码 `WHERE id = record_id`，但 Go 的 UserMessage 主键是 `message_id`。改为通用的 conditions 参数 + limit 1，避免硬编码主键列名。

### 4. SqlMessageStore 核心改进

#### 4.1 构造函数变化

```go
type SqlMessageStore struct {
    cryptoKey  []byte
    codec      *codec.AesStorageCodec       // 新增：替代 encodeContent/decodeContent
    sqlDbStore *SqlDbStore
    tableName  string
    metaMgr    *migrator.MemoryMetaManager  // 新增：替代内嵌 memoryMetaManager
}

func NewSqlMessageStore(cryptoKey []byte, sqlDbStore *SqlDbStore, tableName string) (*SqlMessageStore, error)
```

- 返回值从 `*SqlMessageStore` 变为 `(*SqlMessageStore, error)`
- 内部构造 `AesStorageCodec`（key 校验在 codec 内部）
- 内部构造 `MemoryMetaManager`
- 所有调用方需适配（测试文件、MessageManager 等）

#### 4.2 AddMessages 批量写入

```go
func (s *SqlMessageStore) AddMessages(ctx context.Context, messageAdds []*storedb.MessageAdd) ([]string, error) {
    // 遍历：生成 messageID、序列化 content、加密、组装 data
    // 批量写入：s.sqlDbStore.CreateBatch(ctx, s.tableName, rows)
}
```

#### 4.3 Content 序列化（支持多模态）

**写入时**：

```go
contentBytes, err := json.Marshal(message.Content)
if err != nil { return "", err }
encrypted, err := s.codec.Encode(string(contentBytes))
if err != nil { return "", err }
```

- 纯文本 → `"hello"`（JSON string）
- 多模态 → `[{"type":"text","text":"hello"},{"type":"image_url",...}]`（JSON array）

**读取时**：

```go
decrypted, err := s.codec.Decode(contentStr)
if err != nil { return nil, nil, err }
var content schema.MessageContent
if err := json.Unmarshal([]byte(decrypted), &content); err != nil {
    return nil, nil, err
}
```

- `schema.MessageContent` 已实现 `MarshalJSON/UnmarshalJSON`，天然支持
- 兼容旧数据：之前 `Content.String()` 写入的纯文本就是 `"hello"`，`json.Unmarshal("hello")` 可被 `MessageContent.UnmarshalJSON` 处理

**generateMessageID 同步**：

```go
contentBytes, _ := json.Marshal(message.Content)
messageHash := sha256.Sum256([]byte(fmt.Sprintf("%s%v", string(contentBytes), timestamp)))
```

**UpdateMessage 同步**：

```go
contentBytes, err := json.Marshal(content)
if err != nil { return err }
encrypted, err := s.codec.Encode(string(contentBytes))
if err != nil { return err }
```

#### 4.4 安全类型断言

```go
func (s *SqlMessageStore) rowToMessageAndMeta(row map[string]any) (*schema.BaseMessage, *storedb.MessageMetadata, error) {
    getStr := func(key string) (string, error) {
        v, ok := row[key]
        if !ok {
            return "", fmt.Errorf("行数据缺少字段 %q", key)
        }
        s, ok := v.(string)
        if !ok {
            return "", fmt.Errorf("字段 %q 类型错误: 期望 string, 实际 %T", key, v)
        }
        return s, nil
    }
    // 所有字段使用 getStr 安全断言
}
```

#### 4.5 GetSchemaVersion / SetSchemaVersion

改用 `s.metaMgr`（migrator 包的 MemoryMetaManager），逻辑不变。

### 5. 文件组织

```
internal/agentcore/memory/
├── codec/                              # 新增包
│   ├── doc.go
│   ├── aes_storage_codec.go
│   └── aes_storage_codec_test.go
├── migration/                          # 新增包
│   └── migrator/
│       ├── doc.go
│       ├── memory_meta_manager.go
│       └── memory_meta_manager_test.go
└── manage/
    └── model/
        ├── doc.go                      # 更新：移除 memoryMetaManager，新增引用说明
        ├── db_model.go                 # 不变
        ├── db_model_test.go            # 不变
        ├── sql_db_store.go             # 扩展：新增 6 方法 + 校验 + 缓存
        ├── sql_db_store_test.go        # 补充新方法测试
        ├── sql_message_store.go        # 重构
        ├── sql_message_store_test.go   # 适配改造
        ├── message_manager.go          # 不变
        └── message_manager_test.go     # 不变
```

### 6. 依赖方向图

```
schema.BaseMessage / MessageContent
        ↑
BaseMessageStore (接口, store/db 包)
        ↑
SqlMessageStore ──→ AesStorageCodec (memory/codec 包)
        │               └──→ crypto.AesGcmProvider
        │
        ├──→ SqlDbStore (同包)
        │       └──→ db.BaseDbStore → *gorm.DB
        │
        └──→ MemoryMetaManager (migration/migrator 包)
                └──→ SqlDbStore (manage/model 包)
```

无循环依赖。

### 7. 测试策略

| 文件 | 测试方式 | 关键用例 |
|------|----------|----------|
| `codec/aes_storage_codec_test.go` | 纯单元测试 | 空 key passthrough、32 字节 key 正常、key 长度错误、加密往返、解密失败返回 error、多模态内容加解密 |
| `migrator/memory_meta_manager_test.go` | SQLite 内存 DB | Add 幂等、空参数静默、GetByTableName、DeleteByTableName |
| `model/sql_db_store_test.go` | SQLite 内存 DB | CreateBatch、BatchGet、Get、DeleteTable、ConditionGet 类型校验、GetWithSort 排序列校验 |
| `model/sql_message_store_test.go` | SQLite 内存 DB | 构造函数 key 校验、多模态消息存储/更新、AddMessages 批量写入、已有测试适配 |

覆盖率目标：≥ 85%。

## 与 Python 的差异总结

| 项目 | Python | Go（改进后） |
|------|--------|-------------|
| 加解密策略 | 容错：加密失败返回原文 | 严格：key 为空 passthrough，key 非空失败返回 error |
| AesStorageCodec | 独立包 `memory/codec/` | 独立包 `memory/codec/` ✅ 对齐 |
| MemoryMetaManager | 独立包 `migration/migrator/` | 独立包 `migration/migrator/` ✅ 对齐 |
| Content 序列化 | 隐式 str/list → 加密 | 显式 json.Marshal → 加密（更严谨） |
| count_messages | get_with_sort + len() | SQL COUNT ✅ 已改进 |
| 时间范围查询 | 定义了未实现 | GetWithSortAndTimeRange ✅ 已实现 |
| ConditionGet 类型校验 | values 必须为 list | values 必须为切片 ✅ 对齐 |
| GetWithSort 排序列校验 | 校验列存在性 | 校验列存在性 ✅ 对齐 |
| SqlDbStore 表缓存 | SQLAlchemy 表反射缓存 | 列名列表缓存（GORM 不需反射） |
