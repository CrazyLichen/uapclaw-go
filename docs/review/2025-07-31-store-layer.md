# 代码审查报告 — 2025-07-31

> 审查范围：最近24小时提交（24个commit），涉及 **领域四（存储层 4.11-4.17）** 和 **领域七（记忆系统 codec/migrator/model）**

---

## 📊 审查概览

| 领域 | 章节 | 涉及文件数 | 严重 | 一般 | 提示 |
|------|------|-----------|------|------|------|
| 领域四：存储层 | 4.11 ESVectorStore | 3 | 3 | 4 | 3 |
| 领域四：存储层 | 4.12 BaseDbStore 接口 | 2 | 0 | 1 | 1 |
| 领域四：存储层 | 4.13 DefaultDbStore | 2 | 0 | 0 | 1 |
| 领域四：存储层 | 4.14 GaussDbStore | 6 | 2 | 2 | 5 |
| 领域四：存储层 | 4.15 BaseMessageStore | 2 | 1 | 2 | 1 |
| 领域四：存储层 | 4.16 SqlMessageStore | 8 | 4 | 5 | 3 |
| 领域四：存储层 | 4.17 BaseMemoryIndex | 2 | 0 | 2 | 4 |
| 领域七：记忆系统 | AesStorageCodec | 2 | 1 | 0 | 0 |
| 领域七：记忆系统 | MemoryMetaManager | 2 | 0 | 1 | 1 |
| **合计** | | **29** | **11** | **17** | **19** |

---

## 🔴 严重问题（11项）

### S1. ESVectorStore：`esClientWrapper.Do()` 忽略 context

- **文件**：`internal/agentcore/store/vector/es.go:793`
- **问题**：`esClientWrapper.Do()` 使用 `context.Background()` 而非传入的 `ctx`，导致请求级 context（超时、取消信号）被完全忽略
- **影响**：所有 ES 操作无法被上层 context 控制，超时/取消机制失效，可能导致请求永久阻塞
- **Python 参考**：Python 使用 `async with` 上下文管理器，天然支持超时和取消
- **修复建议**：将 `req.Do(ctx, w.inner)` 的第一个参数改为传入的 `ctx`

### S2. ESVectorStore：`search` 分数归一化与 Python 不一致

- **文件**：`internal/agentcore/store/vector/es.go:536`
- **问题**：Go 实现调用了 `esNormalizeScore()` 对搜索分数做归一化，而 Python 直接返回 ES 原始 `_score`
- **影响**：调用方拿到的分数值与 Python 不一致，可能导致下游逻辑（如排序、阈值判断、相关性过滤）行为差异
- **Python 参考**：`float(hit.get("_score", 0.0))` 直接使用原始分数
- **修复建议**：移除 `esNormalizeScore()` 调用，直接使用 ES 返回的原始分数，与 Python 对齐

### S3. ESVectorStore：`create_collection` 未处理并发创建异常

- **文件**：`internal/agentcore/store/vector/es.go`（CreateCollection 方法）
- **问题**：Go 只做 `HEAD` 前置检查存在则返回，但 `PUT` 创建时可能因并发已存在而返回 `resource_already_exists_exception`，Go 会报错而非静默返回
- **影响**：并发创建同一集合时可能报错，Python 通过捕获 `resource_already_exists_exception` 静默返回
- **Python 参考**：`except resource_already_exists_exception: return` — 捕获异常静默返回
- **修复建议**：在 `CreateCollection` 的 PUT 请求错误处理中，检测 `resource_already_exists_exception` 并静默返回

### S4. GaussDbStore：缺少 `pg_type.typcollation` SQL 拦截

- **文件**：`internal/agentcore/store/db/gaussdb/migrator.go`
- **问题**：Python 通过 `_patch_gaussdb_reflection_sql` 事件监听器拦截含 `pg_type.typcollation` 的子查询并替换为 NULL。GaussDB 的 `pg_type` 视图没有 `typcollation` 列。当前 GaussMigrator 仅为空壳，未覆写任何方法
- **影响**：当 GORM postgres Migrator 执行 `ColumnTypes()` 等内省 SQL 时，查询包含 `pg_type.typcollation` 会导致 GaussDB 报错
- **Python 参考**：`_patch_gaussdb_reflection_sql` 拦截并替换不兼容 SQL
- **修复建议**：覆写 GaussMigrator 的 `ColumnTypes()` 方法，处理 `typcollation` 兼容性

### S5. GaussDbStore：缺少 `_domain_query` / `_enum_query` 替换

- **文件**：`internal/agentcore/store/db/gaussdb/migrator.go`
- **问题**：Python 将 domain/enum 内省查询替换为 `SELECT 1 WHERE FALSE`（永远返回空结果集），因为 GaussDB 不支持 PostgreSQL 的 domain/enum 类型系统。GaussMigrator 未覆写相关方法
- **影响**：如果 GORM postgres Migrator 执行 domain/enum 查询，GaussDB 不支持会报错
- **Python 参考**：`_domain_query(schema) → SELECT 1 WHERE FALSE`，`_enum_query(schema) → SELECT 1 WHERE FALSE`
- **修复建议**：在 GaussMigrator 中覆写 domain/enum 相关查询方法

### S6. SqlMessageStore：`generateMessageID` 与 Python 不兼容

- **文件**：`internal/agentcore/memory/manage/model/sql_message_store.go`
- **问题**：Go 使用 `fmt.Sprintf("%s%v", content, timestamp)` 拼接哈希输入，其中 `%v` 对 `time.Time` 的格式为 `2025-01-01 00:00:00 +0800 CST`；Python 使用 `f"{content_str}{timestamp}"`，其中 `timestamp.__str__()` 格式为 `2025-01-01 00:00:00+08:00`。两者格式不同
- **影响**：相同数据在 Go/Python 生成不同的 message_id，**破坏了跨语言兼容性**
- **Python 参考**：`hashlib.sha256(f"{content_str}{timestamp}".encode()).hexdigest()`
- **修复建议**：使用与 Python 兼容的时间格式，如 `timestamp.Format("2006-01-02 15:04:05-07:00")`

### S7. AesStorageCodec 严格模式破坏数据互操作

- **文件**：`internal/agentcore/memory/codec/aes_storage_codec.go`
- **问题**：Python 的 AesStorageCodec 是容错模式——加密/解密失败时返回原文，保证数据不丢失。Go 是严格模式——失败时返回 error
- **影响**：
  1. Go 无法读取 Python 写入的未加密（明文）数据——Decode 会对明文调用 AES-GCM 解密失败
  2. Python 写入的密文，如果密钥不匹配，Python 容错返回原文，Go 报错
  3. **破坏了 Go/Python 数据互操作性**
- **Python 参考**：`except Exception: return text`（加密失败返回原文），`except Exception: return data`（解密失败返回原文）
- **修复建议**：在 `Decode` 方法中添加容错逻辑——解密失败时返回原文并记录 Warn 日志，与 Python 行为对齐

### S8. SqlMessageStore：`CountMessages` 忽略时间范围过滤

- **文件**：`internal/agentcore/memory/manage/model/sql_message_store.go`
- **问题**：`GetMessages` 通过 `GetWithSortAndTimeRange` 支持时间范围过滤，但 `CountMessages` 使用的 `Count` 方法只接受等值条件，不支持时间范围
- **影响**：当 `MessageFilter` 含 `StartTime`/`EndTime` 时，`CountMessages` 会忽略时间范围，返回不正确的计数
- **Python 参考**：Python 的 `count_messages` 也不支持时间范围（使用 `get_with_sort` + `len()`），但 Python 没有 `StartTime`/`EndTime` 字段
- **修复建议**：新增 `CountWithTimeRange` 方法，或在 `CountMessages` 中检测时间范围条件并走 `GetWithSortAndTimeRange` + `len()` 路径

### S9. SqlDbStore：`DeleteTable` SQL 注入风险

- **文件**：`internal/agentcore/memory/manage/model/sql_db_store.go:448`
- **问题**：使用 `fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)` 直接拼接表名，存在 SQL 注入风险
- **影响**：如果 tableName 来自不可信输入，可能导致任意 SQL 执行
- **Python 参考**：Python 使用 SQLAlchemy ORM 的 `t.drop(checkfirst=True)`，天然防注入
- **修复建议**：使用 GORM 的 `Migrator().DropTable(tableName)` 或对表名进行白名单校验

### S10. BaseMessageStore：`GetSchemaVersion` 无法表达"未设置"语义

- **文件**：`internal/agentcore/store/db/base_message_store.go:63`
- **问题**：Python 返回 `int | None`，`None` 表示版本未设置；Go 返回 `(int32, error)`，`0` 可能是合法版本号也可能是"未设置"
- **影响**：调用方无法区分"版本号为 0"和"版本未设置"两种语义，可能导致误判
- **Python 参考**：`get_schema_version() -> int | None`
- **修复建议**：返回 `(*int32, error)`，`nil` 表示未设置；或返回 `(int32, bool, error)`，bool 表示是否找到

### S11. SqlMessageStore：timestamp 解析错误被静默忽略

- **文件**：`internal/agentcore/memory/manage/model/sql_message_store.go:440`
- **问题**：`timestamp, _ := time.Parse(time.RFC3339, timestampStr)` — 解析失败被静默忽略，timestamp 会是零值
- **影响**：数据库中格式不正确的 timestamp 会被静默置为零值（`0001-01-01T00:00:00Z`），而非报错或记录警告，可能导致数据不一致
- **修复建议**：解析失败时至少记录 Warn 日志，或返回 error

---

## 🟡 一般问题（17项）

### G1. ESVectorStore：`esMapTypeToOurType` 精度丢失

- **文件**：`internal/agentcore/store/vector/es.go:924-925`
- **问题**：Python 区分 `"short"→INT16, "byte"→INT8`，Go 全部映射为 `INT32`
- **影响**：Schema 反射时整数精度信息丢失
- **修复建议**：添加 `INT16` 和 `INT8` 类型映射

### G2. ESVectorStore：`delete_docs_by_ids` 不分批

- **文件**：`internal/agentcore/store/vector/es.go:588`
- **问题**：Go 一次性构建所有 delete 动作的 NDJSON，不分批；Python 按 `batch_size`（默认500）分批
- **影响**：大批量删除可能导致 ES 请求体过大
- **修复建议**：添加分批删除逻辑

### G3. ESVectorStore：`search` 的 `output_fields` 行为差异

- **文件**：`internal/agentcore/store/vector/es.go`
- **问题**：Python 不设 `_source.includes`，只设 `excludes=["_meta"]`，返回所有字段（排除 `_meta`）；Go 设 `_source.includes` 限定返回字段
- **影响**：Python 返回更多字段，Go 只返回 `output_fields` 指定的字段，语义不同
- **修复建议**：与 Python 对齐——有 `output_fields` 时只设 `excludes=["_meta"]`，不设 `includes`

### G4. ESVectorStore：错误日志 `event_type` 使用 `LLM_CALL_ERROR`

- **文件**：`internal/agentcore/store/vector/es.go`（多处：192, 205, 274, 287, 517, 519, 657, 744, 1257）
- **问题**：存储操作错误日志使用 `LLM_CALL_ERROR` event_type，不符合日志同步规范，应为 `STORE_*` 类型
- **修复建议**：统一改为 `STORE_*` 系列 event_type

### G5. GaussDbStore：LOCKING 子句输出差异

- **文件**：`internal/agentcore/store/db/gaussdb/clause.go`
- **问题**：Python 硬编码 `" FOR UPDATE"`，Go 输出 `"FOR " + locking.Strength`（可输出 FOR SHARE 等）
- **影响**：Go 更精确但与 Python 行为不一致。如果 GaussDB 不支持 FOR SHARE 等锁类型，Go 可能生成无效 SQL
- **修复建议**：确认 GaussDB 支持的锁类型，如仅支持 FOR UPDATE 则硬编码

### G6. GaussDbStore：GaussStringSerializer 缺少 parent_processor 回调

- **文件**：`internal/agentcore/store/db/gaussdb/serializer.go`
- **问题**：Python 在类型转换后还会调用 `String.bind_processor` 的返回值（parent_processor），Go 跳过了此步
- **影响**：在 PostgreSQL 方言下 parent_processor 是空操作，当前无实际影响，但未来方言行为变化可能产生差异
- **修复建议**：在注释中说明跳过原因

### G7. BaseMessageStore：`MessageFilter` 缺少 `MessageType` 字段

- **文件**：`internal/agentcore/store/db/base_message_store.go:109`
- **问题**：Python `message_filter` 支持 `message_type` 字段，Go 跳过了
- **影响**：虽然 Python 实际 SQL 表也无此列（未做过滤），但接口定义了此字段。如果未来数据库表增加 `message_type` 列，需要补回
- **修复建议**：在接口层保留 `MessageType` 字段并加注释说明暂未实现过滤

### G8. BaseMessageStore：`UpdateMessage` / `DeleteMessageByID` 丢失"不存在"语义

- **文件**：`internal/agentcore/store/db/base_message_store.go:42,47`
- **问题**：Python 返回 `bool`（False 表示不存在/未执行），Go 返回 `error`（无法区分"不存在"和"真正的错误"）
- **影响**：调用方无法区分"消息不存在但不算错误"和"数据库访问异常"
- **修复建议**：定义 `ErrMessageNotFound` 错误，在消息不存在时返回此错误供调用者区分

### G9. SqlMessageStore：`GetByID` 错误处理不正确

- **文件**：`internal/agentcore/memory/manage/model/message_manager.go`
- **问题**：Go 在 GetByID 中任何 error 都返回 `(nil, nil)`（吞掉错误），而 Python 实际是 bug（捕获 ValueError 但抛出 BaseError）
- **影响**：消息查询失败时调用方不知道出错
- **修复建议**：正确传播 error，只在"未找到"时返回 `(nil, nil)`

### G10. SqlMessageStore：`ScopeUserMappingManager` 完全缺失

- **文件**：缺失
- **问题**：Python 有 `ScopeUserMappingManager` 管理 `scope_user_mapping` 表的 CRUD，Go 侧只有 DB Model，无 Manager
- **影响**：无法对 `scope_user_mapping` 表进行业务层操作
- **修复建议**：实现 `ScopeUserMappingManager`

### G11. SqlMessageStore：`DataIdManager` 完全缺失

- **文件**：缺失
- **问题**：Python 有 `DataIdManager` 生成唯一 ID，Go 未实现
- **影响**：缺少唯一 ID 生成能力
- **修复建议**：实现 `DataIdManager`

### G12. SqlMessageStore：`create_tables` 缺少旧表迁移和版本初始化逻辑

- **文件**：`internal/agentcore/memory/manage/model/db_model.go`
- **问题**：Python `create_tables` 有三个关键逻辑——检查旧 `group_id` 列并 DROP 重建、记录新建表、对新建表写入 `schema_version`。Go 的 `CreateTables` 只是简单 `AutoMigrate`
- **影响**：从旧版 Python 数据库迁移到 Go 时可能失败；新建表无版本号
- **修复建议**：补充旧表迁移检测和版本初始化逻辑

### G13. SqlDbStore：列名参数的 SQL 注入风险

- **文件**：`internal/agentcore/memory/manage/model/sql_db_store.go`（多处：87, 137-138, 145, 173-174, 178, 200, 228, 249, 274, 384, 423）
- **问题**：所有 `fmt.Sprintf("%s = ?", col)` 和 `fmt.Sprintf("%s IN ?", col)` 中的列名来自参数，存在注入风险
- **影响**：Python 使用 SQLAlchemy Table 对象的列引用（`t.c[col]`），天然防注入；Go 直接拼接
- **修复建议**：对列名进行白名单校验，或使用 GORM 的 Column 类型安全引用

### G14. BaseMemoryIndex：`cleanup_backup` 和 `list_user_scopes` 抽象性不一致

- **文件**：`internal/agentcore/store/index/base.go`
- **问题**：Python 中 `cleanup_backup` 和 `list_user_scopes` 是 `@abstractmethod`（子类必须实现），Go 提供了默认空实现
- **影响**：Go 的实现类通过嵌入 `MemoryIndexBase` 自动"满足"了这两个方法，绕过了强制实现的要求。如果实际实现用外部存储（如 S3），Go 的默认实现只清理了内存 map，不会清理外部数据
- **修复建议**：在注释中标注"Python 中为抽象方法，Go 提供了默认空实现，子类应覆盖"，或从 MemoryIndexBase 移除默认实现

### G15. BaseMemoryIndex：`MemoryIndexBase` 并发安全问题

- **文件**：`internal/agentcore/store/index/base.go`
- **问题**：`backups` map 和 `schemaVersion` 字段没有并发保护（无 mutex）
- **影响**：如果实现类被并发访问，可能导致 data race
- **修复建议**：添加 `sync.RWMutex` 保护，或在注释中明确标注非并发安全

### G16. MemoryMetaManager：`GetByTableName` 返回值语义差异

- **文件**：`internal/agentcore/memory/migration/migrator/memory_meta_manager.go`
- **问题**：Python 无结果时返回 `None`，Go 返回 `(nil, nil)` 或 `([]map[string]any{}, nil)`
- **影响**：调用方需注意区分"空结果"和"未找到"
- **修复建议**：统一返回语义——无结果时返回 `(nil, nil)` 或定义特定错误

### G17. SqlMessageStore：`GetMessages` 中 session_id 过滤行为差异

- **文件**：`internal/agentcore/memory/manage/model/sql_message_store.go`
- **问题**：Python 即使 `session_id` 为 `None` 也传给 SQL 查询（WHERE session_id IS NULL），Go 空字符串时不加过滤条件
- **影响**：行为差异——Python 可以查找 session_id 为 NULL 的记录，Go 无法区分"不加过滤"和"查找 NULL 值"
- **修复建议**：使用 `*string` 指针类型，`nil` 表示查找 NULL 值，空字符串表示不加过滤

---

## 🔵 提示问题（19项）

### T1. ESVectorStore：`get_schema` 从 mapping 反射时不标记主键字段

- **文件**：`internal/agentcore/store/vector/es.go`
- **说明**：Python 通过 `kwargs.get("primary_key_field", "id")` 参数标记主键，Go 的 `esBuildSchemaFromMapping` 不处理主键字段标记

### T2. ESVectorStore：`get_schema` 从 mapping 反射时不设置 description

- **文件**：`internal/agentcore/store/vector/es.go`
- **说明**：Python 设置 `description=f"Collection '{collection_name}'"`，Go 不设置

### T3. ESVectorStore：`list_collection_names` 异常处理差异

- **文件**：`internal/agentcore/store/vector/es.go`
- **说明**：Python 异常时返回空列表 `[]`，Go 返回 error。Go 更严格但行为不同

### T4. GaussDbStore：`GaussDialector` 缺少 driver 声明注释

- **文件**：`internal/agentcore/store/db/gaussdb/dialector.go`
- **说明**：Python 显式声明了 `driver = 'async_gaussdb'`，Go 使用 pgx 驱动。应在注释中说明 Go 使用 pgx 替代

### T5. GaussDbStore：`supports_statement_cache` / `use_insertmanyvalues` 未映射

- **文件**：`internal/agentcore/store/db/gaussdb/dialector.go`
- **说明**：GORM 无对应概念，但应在注释中说明跳过原因

### T6. GaussDbStore：`_get_server_version_info` 未映射

- **文件**：`internal/agentcore/store/db/gaussdb/dialector.go`
- **说明**：GORM 无版本检测机制，但应在注释中说明跳过原因

### T7. GaussDbStore：GaussMigrator 预留覆写点缺少 TODO 标记

- **文件**：`internal/agentcore/store/db/gaussdb/migrator.go`
- **说明**：按项目规范，预留功能应有 `// TODO:` 标记

### T8. GaussDbStore：LOCKING 子句缺少前导空格差异说明

- **文件**：`internal/agentcore/store/db/gaussdb/clause.go`
- **说明**：Python 返回 `" FOR UPDATE"`（前有空格），Go 返回 `"FOR UPDATE"`（前无空格）。GORM ClauseBuilder 调用前自行添加空格，所以不影响，但应注释说明

### T9. DefaultDbStore：传入 `nil *gorm.DB` 不报错

- **文件**：`internal/agentcore/store/db/default.go:26-28`
- **说明**：`NewDefaultDbStore(db)` 在 `db == nil` 时不报错，后续 `GetDB` 返回 nil 引发难以排查的问题

### T10. BaseMessageStore：`GetMessages` 参数无默认值约定

- **文件**：`internal/agentcore/store/db/base_message_store.go:37`
- **说明**：Python 有 `limit=10, order_by="timestamp", order_direction="desc"` 默认值，Go 无。建议定义常量或注释说明零值行为

### T11. BaseMemoryIndex：`MemoryDoc.Timestamp` 默认值差异

- **文件**：`internal/agentcore/store/index/base.go`
- **说明**：Python 创建时自动填充当前时间，Go 的 `time.Time` 零值为 `0001-01-01`。建议注释说明"零值表示未设置，由实现层填充"

### T12. BaseMemoryIndex：`MemoryDoc.Fields` 零值差异

- **文件**：`internal/agentcore/store/index/base.go`
- **说明**：Python 默认 `{}`（空字典），Go 零值为 `nil`。加了 `omitempty` 后 JSON 序列化行为不同

### T13. BaseMemoryIndex：`defaultTopK` 常量未使用

- **文件**：`internal/agentcore/store/index/base.go`
- **说明**：定义了 `defaultTopK = 10` 但没有任何代码引用它

### T14. BaseMemoryIndex：`logComponent` 变量未使用

- **文件**：`internal/agentcore/store/index/base.go`
- **说明**：定义了 `logComponent = logger.ComponentCommon` 但 base.go 中没有任何日志调用

### T15. BaseMemoryIndex：`RestoreBackup` 错误分支缺少日志

- **文件**：`internal/agentcore/store/index/base.go`
- **说明**：按项目日志规范，`RestoreBackup` 不存在时应记录 Error 日志

### T16. MemoryMetaManager：`Add` 方法缺少 `**kwargs` 支持

- **文件**：`internal/agentcore/memory/migration/migrator/memory_meta_manager.go`
- **说明**：Python 支持 `**kwargs`，Go 不支持。但 Python 实际也未使用 kwargs

### T17. SqlMessageStore：`add_messages` 事务粒度差异

- **文件**：`internal/agentcore/memory/manage/model/sql_message_store.go`
- **说明**：Python 逐条调用 `add_message`（每条独立事务），Go 一次 `CreateBatch`（一个事务）。Go 更优但行为不同——部分成功部分失败时，Go 要么全写入要么全不写

### T18. SqlMessageStore：AesStorageCodec key 校验差异

- **文件**：`internal/agentcore/memory/codec/aes_storage_codec.go`
- **说明**：Python 不校验 key 长度，Go 校验 key 必须 32 字节。Go 更安全但行为不同

### T19. SqlDbStore：`Get` 方法签名变更

- **文件**：`internal/agentcore/memory/manage/model/sql_db_store.go`
- **说明**：Python 硬编码 `WHERE id = record_id`，Go 改为通用 `conditions` 参数。功能上 Go 更灵活，但调用方需显式传 `{"id": ...}`

---

## 📋 按模块分类汇总

### ESVectorStore（4.11）

| 级别 | 编号 | 问题摘要 |
|------|------|---------|
| 🔴 | S1 | `esClientWrapper.Do()` 忽略 context |
| 🔴 | S2 | `search` 分数归一化与 Python 不一致 |
| 🔴 | S3 | `create_collection` 未处理并发创建异常 |
| 🟡 | G1 | `esMapTypeToOurType` 精度丢失（short/byte→INT32） |
| 🟡 | G2 | `delete_docs_by_ids` 不分批 |
| 🟡 | G3 | `search` 的 `output_fields` 行为差异 |
| 🟡 | G4 | 错误日志 event_type 使用 LLM_CALL_ERROR |
| 🔵 | T1 | `get_schema` 不标记主键字段 |
| 🔵 | T2 | `get_schema` 不设置 description |
| 🔵 | T3 | `list_collection_names` 异常处理差异 |

### GaussDbStore（4.14）

| 级别 | 编号 | 问题摘要 |
|------|------|---------|
| 🔴 | S4 | 缺少 `pg_type.typcollation` SQL 拦截 |
| 🔴 | S5 | 缺少 `_domain_query` / `_enum_query` 替换 |
| 🟡 | G5 | LOCKING 子句输出差异 |
| 🟡 | G6 | GaussStringSerializer 缺少 parent_processor |
| 🔵 | T4 | GaussDialector 缺少 driver 声明注释 |
| 🔵 | T5 | `supports_statement_cache` / `use_insertmanyvalues` 未映射 |
| 🔵 | T6 | `_get_server_version_info` 未映射 |
| 🔵 | T7 | GaussMigrator 预留覆写点缺少 TODO 标记 |
| 🔵 | T8 | LOCKING 子句前导空格差异说明 |

### BaseMessageStore（4.15）

| 级别 | 编号 | 问题摘要 |
|------|------|---------|
| 🔴 | S10 | `GetSchemaVersion` 无法表达"未设置"语义 |
| 🟡 | G7 | `MessageFilter` 缺少 `MessageType` 字段 |
| 🟡 | G8 | `UpdateMessage`/`DeleteMessageByID` 丢失"不存在"语义 |
| 🔵 | T10 | `GetMessages` 参数无默认值约定 |

### SqlMessageStore（4.16）

| 级别 | 编号 | 问题摘要 |
|------|------|---------|
| 🔴 | S6 | `generateMessageID` 与 Python 不兼容 |
| 🔴 | S8 | `CountMessages` 忽略时间范围过滤 |
| 🔴 | S9 | `DeleteTable` SQL 注入风险 |
| 🔴 | S11 | timestamp 解析错误被静默忽略 |
| 🟡 | G9 | `GetByID` 错误处理不正确 |
| 🟡 | G10 | `ScopeUserMappingManager` 完全缺失 |
| 🟡 | G11 | `DataIdManager` 完全缺失 |
| 🟡 | G12 | `create_tables` 缺少旧表迁移和版本初始化 |
| 🟡 | G13 | 列名参数 SQL 注入风险 |
| 🟡 | G17 | `GetMessages` session_id 过滤行为差异 |
| 🔵 | T17 | `add_messages` 事务粒度差异 |
| 🔵 | T18 | AesStorageCodec key 校验差异 |
| 🔵 | T19 | `Get` 方法签名变更 |

### AesStorageCodec（7.24 预实现）

| 级别 | 编号 | 问题摘要 |
|------|------|---------|
| 🔴 | S7 | 严格模式破坏数据互操作 |

### BaseMemoryIndex（4.17）

| 级别 | 编号 | 问题摘要 |
|------|------|---------|
| 🟡 | G14 | `cleanup_backup`/`list_user_scopes` 抽象性不一致 |
| 🟡 | G15 | `MemoryIndexBase` 并发安全问题 |
| 🔵 | T11 | `MemoryDoc.Timestamp` 默认值差异 |
| 🔵 | T12 | `MemoryDoc.Fields` 零值差异 |
| 🔵 | T13 | `defaultTopK` 常量未使用 |
| 🔵 | T14 | `logComponent` 变量未使用 |
| 🔵 | T15 | `RestoreBackup` 错误分支缺少日志 |

### DefaultDbStore（4.13）

| 级别 | 编号 | 问题摘要 |
|------|------|---------|
| 🔵 | T9 | 传入 `nil *gorm.DB` 不报错 |

### MemoryMetaManager（7.23 预实现）

| 级别 | 编号 | 问题摘要 |
|------|------|---------|
| 🟡 | G16 | `GetByTableName` 返回值语义差异 |
| 🔵 | T16 | `Add` 方法缺少 `**kwargs` 支持 |

---

## 🎯 修复优先级建议

### 立即修复（影响正确性/安全性）

1. **S1** — `esClientWrapper.Do()` context 丢失（1行修改）
2. **S6** — `generateMessageID` 时间格式不兼容（1行修改）
3. **S7** — AesStorageCodec 添加容错逻辑（关键互操作性问题）
4. **S9** — `DeleteTable` SQL 注入风险
5. **S11** — timestamp 解析错误被静默忽略

### 尽快修复（影响功能完整性）

6. **S2** — ESVectorStore search 分数归一化差异
7. **S3** — ESVectorStore 并发创建异常处理
8. **S4/S5** — GaussDbStore 缺少 SQL 兼容性处理
9. **S8** — CountMessages 忽略时间范围
10. **S10** — GetSchemaVersion None 语义丢失

### 计划修复（功能缺失）

11. **G1** — ES 类型映射精度
12. **G2** — 分批删除
13. **G10/G11** — ScopeUserMappingManager / DataIdManager 缺失
14. **G12** — 旧表迁移和版本初始化
15. **G13** — 列名 SQL 注入风险
