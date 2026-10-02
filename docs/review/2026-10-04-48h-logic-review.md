# 48 小时逻辑审查报告（2026-10-04）

> 审查范围：2026-10-02 ~ 2026-10-04 48小时内提交的代码
> 对应章节：7.27 LongTermMemory、7.22+7.23 迁移操作注册表+迁移器、TeamManager 应用层、S 级修复验证
> 审查方式：逐方法对比 Python 参考源码与 Go 实现（3 个 Agent 并行 + 人工复核）

## 审查统计

| 分类 | 数量 | 说明 |
|------|------|------|
| 严重 (S) | 39 | 功能缺失/逻辑错误/并发竞态/数据丢失 |
| 一般 (M) | 30 | 行为偏差/错误处理缺失/锁策略不一致 |
| 提示 (T) | 16 | 日志/命名/风格/测试 |
| **总计** | **85** | |

---

## 一、7.27 LongTermMemory（12 严重 / 12 一般 / 7 提示）

### S-01 [严重] SetConfig 缺少 SqlMessageStore crypto_key 注入

Python 在 `set_config()` 中为 `SqlMessageStore` 设置 `crypto_key`，Go 的 `SetConfig` 完全遗漏了这一步骤。`RegisterStore` 自动创建 SqlMessageStore 时传了 `nil` 作为 cryptoKey，而 SetConfig 又没有回填，导致加密消息无法正确编解码。

**Python 样例** (`long_term_memory.py:306-308`):
```python
if self.message_store:
    if isinstance(self.message_store, SqlMessageStore) and self.message_store.crypto_key is None:
        self.message_store.crypto_key = config.crypto_key
    self.message_manager = MessageManager(store=self.message_store)
```

**Go 问题代码** (`config_ops.go:78-80`):
```go
if m.messageStore != nil {
    m.messageManager = mem_model.NewMessageManager(m.messageStore)
}
```

**修复方案**：在 SetConfig 的 messageStore 初始化处，检查 `m.messageStore` 是否为 `*mem_model.SqlMessageStore` 且 `CryptoKey` 为空：
```go
if m.messageStore != nil {
    if sqlMsg, ok := m.messageStore.(*mem_model.SqlMessageStore); ok && sqlMsg.CryptoKey() == "" {
        sqlMsg.SetCryptoKey(cfg.CryptoKey)
    }
    m.messageManager = mem_model.NewMessageManager(m.messageStore)
}
```

---

### S-02 [严重] 多处错误被静默吞没——Python 异常传播 vs Go continue/空值

Go 实现中大量使用 `continue` 或返回空值来跳过错误，而 Python 中这些操作失败后异常自然向上传播。以下是完整清单：

| 方法 | Go 行为 | Python 行为 |
|------|---------|------------|
| `addMessagesImpl` → messageManager.Add | `continue` 跳过 | 异常传播 |
| `addMessagesImpl` → scopeUserMappingManager.Add | 仅记日志 | 异常传播 |
| `DeleteMemByScope` → writeManager.DeleteMemByUserID | 仅记日志 | 异常传播 |
| `DeleteVariables` → variableManager.DeleteUserVariable | 仅记日志 | 异常传播 |
| `UpdateVariables` → variableManager.UpdateUserVariable | 仅记日志 | 异常传播 |
| `GetVariables` → searchManager.GetUserVariable | `ret[name] = ""` | 异常传播 |

**Python 样例** (`long_term_memory.py:584-594`):
```python
msg_id = await self.message_manager.add(add_req)
# 无 try/except — 异常自然传播
```

**Go 问题代码** (`add_messages.go:146-153`):
```go
id, addErr := m.messageManager.Add(ctx, addReq)
if addErr != nil {
    logger.Error(logComponent).Err(addErr).Msg("添加消息失败")
    continue  // ← 静默跳过，与 Python 行为不一致
}
```

**修复方案**：对齐 Python 行为——删除失败应返回错误，而非 continue。对于 `addMessagesImpl` 中的消息添加失败，`msg_id` 会保留旧值 `"-1"`，后续 `gen_all_memory` 使用错误的 msgID 关联记忆，导致数据不一致。

---

### S-03 [严重] SearchUserMem/SearchUserHistorySummary 回调缺少 result_count 和 search_type

**Python 样例** (`long_term_memory.py:977-979`):
```python
await trigger(MemoryEvents.MEMORY_SEARCH_FINISHED,
    scope_id=scope_id, user_id=user_id, query=query,
    result_count=len(mem_results), search_type="user_mem")
```

**Go 问题**：`triggerMemoryAfter` 传入的 `MemoryEventData` 缺少 `ResultCount` 和 `SearchType` 字段。

**修复方案**：在 `MemoryEventData` 中增加 `ResultCount int` 和 `SearchType string` 字段，并在搜索回调中填充。

---

### S-04 [严重] SearchUserMem/SearchUserHistorySummary 缺少异常分类处理

Python 对 `AttributeError`/`ValueError`/`Exception` 分别以 Debug/Warning/Warning 级别记录并包装不同错误消息。Go 统一为一个 error 处理，丢失了异常分类信息。

**Python 样例** (`long_term_memory.py:981-1025`):
```python
except AttributeError as e:
    memory_logger.debug("Search user mem has attribute exception.", ...)
    raise build_error(...)
except ValueError as e:
    memory_logger.warning("Search user mem has value exception.", ...)
    raise build_error(...)
```

**修复方案**：对 `searchManager.Search` 返回的错误进行类型判断，对不同类型记录不同级别日志。

---

### S-05 [严重] AddMessages 回调 event_data 缺少 MemoryType/SessionID

**Python 样例**：`@_fw.emit_before(MemoryEvents.MEMORY_ADDED)` 装饰器自动传递函数参数。

**Go 问题代码** (`add_messages.go:40-44`):
```go
triggerMemoryBefore(ctx, callback.MemoryAdded, &callback.MemoryEventData{
    Event:   callback.MemoryAdded,
    UserID:  p.UserID,
    ScopeID: p.ScopeID,
    // 缺少 SessionID / MemoryType
})
```

**修复方案**：补充 `SessionID: p.SessionID` 和 `MemoryType: "all"` 字段。

---

### S-06 [严重] LongTermMemory 的 scopeConfig/scopeEmbedding map 无并发保护

Python 依赖 asyncio 单线程，无并发问题。Go 中 `SetScopeConfig` 写、`getScopeConfig` 读、`applyScopeEmbedding` 读写 `scopeEmbedding`，多 goroutine 并发会 panic。

**修复方案**：为 `scopeConfig` 和 `scopeEmbedding` 增加 `sync.RWMutex` 保护，或改用 `sync.Map`。

---

### S-07 [严重] sync.Once 单例 Reset 后并发竞态

`ResetLongTermMemory()` 重置 `ltmOnce` 和 `ltmInstance` 后，如果并发调用 `GetLongTermMemory()`，一个 goroutine 可能看到 `ltmInstance` 为 nil 而另一个正在初始化。

**修复方案**：`ResetLongTermMemory` 和 `GetLongTermMemory` 需要互斥保护，或用 `sync.Mutex` 替代 `sync.Once`。

---

### S-08 [严重] checkMessages/getHistoryMessages 截断使用字节长度而非字符长度

Go 中 `len(text) > inputMsgMaxLen` 使用字节长度，Python 使用字符（Unicode 码点）长度。对 CJK 文本，1 个中文字符占 3 字节，Go 的截断会提前触发且可能截断到 UTF-8 中间字节。

**Python 样例** (`long_term_memory.py:1462`):
```python
msg.content = msg.content[:self._sys_mem_config.input_msg_max_len]
```

**Go 问题代码** (`long_term_memory.go:184-185`):
```go
if len(text) > inputMsgMaxLen {  // ← 字节长度
    msg.SetContent(llmschema.NewTextContent(text[:inputMsgMaxLen]))  // ← 可能截断到 UTF-8 中间
}
```

**修复方案**：
```go
if utf8.RuneCountInString(text) > inputMsgMaxLen {
    runes := []rune(text)
    msg.SetContent(llmschema.NewTextContent(string(runes[:inputMsgMaxLen])))
}
```

---

### S-09 [严重] GetRecentMessages 缺少 session_id 过滤参数

Python 支持 `session_id` 参数过滤消息，Go 的 `GetRecentMessages` 虽然接受 `UserScopeOption` 但没有 `WithSessionID` 选项，sessionID 始终传空字符串。

**Python 样例** (`long_term_memory.py:663-668`):
```python
async def get_recent_messages(self, user_id=..., scope_id=..., session_id=..., num=10):
```

**修复方案**：在 `UserScopeOption` 中增加 `WithSessionID` 选项。

---

### S-10 [严重] GetUserMemByPage/MemResult 中 MemInfo 缺少 Timestamp 字段

Python 中 `MemInfo` 包含 `timestamp` 字段，Go 构造 `MemInfo` 时未从搜索结果中提取 `Timestamp`，永远是 nil。

**Python 样例** (`long_term_memory.py:967-972`):
```python
MemInfo(mem_id=item["id"], content=item["mem"], type=item.get("mem_type"),
    timestamp=item.get("timestamp"))
```

**Go 问题代码** (`query_ops.go:172-177`):
```go
memResults = append(memResults, &MemInfo{
    MemID:   item.Doc.ID,
    Content: item.Doc.Text,
    Type:    memType,
    // Timestamp 未赋值
})
```

**修复方案**：从 `searchData` 中提取 Timestamp 并赋值。

---

### S-11 [严重] getHistoryMessages 当 messageManager 为 nil 时返回 nil 而非空切片

**Python 样例** (`long_term_memory.py:1474-1475`):
```python
if not self.message_manager:
    return []
```

**Go 问题代码** (`long_term_memory.go:196`):
```go
return nil, nil  // ← 应返回 []llmschema.BaseMessage{}, nil
```

**修复方案**：返回 `[]llmschema.BaseMessage{}, nil` 保持与 Python 语义一致（nil 在 JSON 序列化时为 `null`，空切片为 `[]`）。

---

### S-12 [严重] RegisterStore 中 RegisterPlugin 绕过统一调用路径

Python 中 `register_store` 的 Step 3 通过调用 `register_plugin(name='semantic_index', cls=SimpleMemoryIndex, params=...)` 注册 SimpleMemoryIndex。Go 中 `RegisterStore` 直接设置 `m.memoryIndex = simpleIndex`，绕过了 `RegisterPlugin` 方法。

**修复方案**：统一调用 `m.RegisterPlugin(simpleIndex)` 路径。

---

### M-01 [一般] deepCopyScopeConfig fallback 浅拷贝不完整

当 JSON 序列化失败时，fallback 只复制 3 个 Definition 字段，遗漏 `ModelCfg`/`ModelClientCfg`/`EmbeddingCfg`。

**修复方案**：补充 fallback 中缺失的字段，并记录 Warn 日志。

---

### M-02 [一般] SetConfig 中 SqlDbStore 重复创建

`SetConfig` 每次调用都创建新的 `SqlDbStore`，而 `RegisterStore` 中也创建了一次。Python 中 `SqlDbStore(self.db_store)` 只创建一次。

**修复方案**：将 `sqlDbStore` 缓存到 `LongTermMemory` 结构体中。

---

### M-03 [一般] GetVariables 获取失败返回空字符串掩盖错误

Go 中 `GetVariables` 遍历 names 时，获取失败设置 `ret[name] = ""`，掩盖了变量不存在的事实。Python 中 `get_user_variable` 失败直接异常退出。

**修复方案**：获取变量失败应返回错误。

---

### M-04 [一般] SearchUserMem/SearchUserHistorySummary 的 triggerMemoryBefore 缺少 search_type 区分

两个搜索方法都触发 `MemorySearchStarted`，但回调数据中没有区分是搜索 `user_mem` 还是 `history_summary`。

**修复方案**：在 MemoryEventData 中标记 search_type。

---

### M-05 [一般] DeleteMemByScope 中 GetByScopeID 返回类型使用 map[string]any 而非结构体

`map[string]any` 提取 `user_id` 需要类型断言，存在运行时 panic 风险。

**修复方案**：`GetByScopeID` 应返回具体结构体。

---

### M-06 [一般] RegisterStore 中 SqlMessageStore 构造参数 cryptoKey 传 nil

`NewSqlMessageStore(nil, sqlDbStore, "")` 的 cryptoKey 传 nil，与 S-01 联动。

**修复方案**：在 `SetConfig` 中回填 crypto_key。

---

### M-07 [一般] triggerMemoryBefore/After 的 event 参数冗余

两个函数接收 `event callback.MemoryEventType` 参数，但实际通过 `data.Event` 传递事件类型，event 参数未被使用。

**修复方案**：确认 `fw.TriggerMemory` 行为，清理冗余参数。

---

### M-08 [一般] DeleteMessagesByUserAndScope 使用 MEMORY_RETRIEVE 事件类型做 validateID

这是 Python 原始代码的语义错误（删除操作用 RETRIEVE 事件类型），Go 照搬了。

**修复方案**：标注为已知对齐 Python 的问题。

---

### M-09 [一般] RegisterStore 中 sql_migrations 传参类型不匹配

Go 的 `RunSQLMigrations` 接收 `*gorm.DB` + `MemoryMetaManager`，Python 接收 `SqlDbStore`。

**修复方案**：`RunSQLMigrations` 应接受 `*SqlDbStore`，与 Python 对齐。

---

### M-10 [一般] RegisterPlugin 方法签名与 Python 不一致

Go 直接接受已实例化的 `BaseMemoryIndex`，跳过了 Python 的 `name`/`cls`/`params` 动态实例化。

**修复方案**：标注为语言差异，可接受。但 `RegisterStore` 中应统一调用 `RegisterPlugin` 路径。

---

### M-11 [一般] VectorMigrator 构造函数额外参数 supportedTypes

Python 的 `VectorMigrator(vector_store)` 只接受 vector store，Go 的 `NewVectorMigrator(vectorStore, supportedTypes)` 额外接受 `supportedTypes`。

**修复方案**：如果可以从 vector_store 或注册表推断，应移除此参数。

---

### M-12 [一般] KVMigrator 构造函数需要 kvRegistry 参数

Python 的 `KVMigrator(kv_store)` 不需要注册表，Go 额外传入 `kvRegistry`。

**修复方案**：将 `kvRegistry` 改为包级变量引用，减少耦合。

---

### T-01 [提示] validateID 事件类型使用字符串而非枚举

Python 使用 `LogEventType.MEMORY_STORE` 枚举，Go 使用 `"MEMORY_STORE"` 硬编码字符串。

**修复方案**：定义 `eventType` 常量替代硬编码字符串。

---

### T-02 [提示] runMigration 日志缺少 event_type 字段

Python 日志包含 `event_type=LogEventType.MEMORY_INIT`，Go 缺少。

**修复方案**：增加 `.Str("event_type", "MEMORY_INIT")`。

---

### T-03 [提示] MigrateBetweenIndices 日志缺少 event_type

同 T-02。

---

### T-04 [提示] GetUserMemByPage/DrainTeamSkillEvents 返回 nil 而非空切片

Go 中 `nil` 和空切片在 JSON 序列化时行为不同（`null` vs `[]`）。

**修复方案**：返回空切片。

---

### T-05 [提示] runMigration 日志使用 fmt.Sprintf 而非纯结构化日志

Msg 中重复了 store_type 信息。

**修复方案**：Msg 改为常量字符串。

---

### T-06 [提示] getScopeEmbeddingModel 缺少 APIEmbedding 实例化失败的捕获日志

Python 有外层 try/except 捕获实例化失败，Go 缺少。

**修复方案**：在 `apiembedding.NewAPIEmbedding` 调用外包错误捕获。

---

### T-07 [提示] checkMessages 中 inputMsgMaxLen 的硬编码默认值 2000

Python 不设默认值（依赖 sysMemConfig），Go 硬编码 2000。

**修复方案**：保留防御性默认值，但应与 MemoryEngineConfig 的默认值一致。

---

## 二、7.22+7.23 迁移系统（4 严重 / 4 一般 / 3 提示）

### S-13 [严重] RegisterStore 缺少 run_index_version_migrations 调用

Go 的 `RunIndexVersionMigrations` 存在但从未在 `RegisterStore` 中被调用，索引版本迁移被完全跳过。

**Python 样例** (`run_migrations.py:128-162`):
```python
async def run_index_version_migrations(index: BaseMemoryIndex) -> None:
    registry_map = index_registry.get_all_operations()
    ...
```

**修复方案**：在 `RegisterStore` 迁移步骤中增加：
```go
if m.memoryIndex != nil {
    if err := runMigration(ctx, func(ctx context.Context) error {
        return migration.RunIndexVersionMigrations(ctx, m.memoryIndex)
    }, "index version"); err != nil {
        return err
    }
}
```

---

### S-14 [严重] RunVectorMigrations 的 supportedTypes 传 nil 可能导致迁移静默跳过

`RegisterStore` 调用 `RunVectorMigrations(ctx, m.vectorStore, nil)` 时 `supportedTypes` 为 nil。如果 `VectorMigrator` 在 `supportedTypes` 为 nil 时不执行任何迁移，则向量存储的 schema 升级将被静默跳过。

**Python 样例** (`run_migrations.py:61-76`):
```python
async def run_vector_migrations(vector_store: BaseVectorStore) -> None:
    await _run_migrations_with_registry(
        registry=vector_registry,
        migrator_factory=lambda: VectorMigrator(vector_store),
        store_name="vector store"
    )
```

**修复方案**：传入 `m.fragmentType`（在 `SetConfig` 之后可用），或 `VectorMigrator` 在 nil 时使用默认列表。

---

### S-15 [严重] RunSQLMigrations 签名与 Python 不一致

Python 接受 `SqlDbStore`，Go 接受 `*gorm.DB` + `*MemoryMetaManager`。`RegisterStore` 中需要创建两次 `SqlDbStore`（一次给 `MemoryMetaManager`，一次给 `SetConfig`）。

**修复方案**：`RunSQLMigrations` 应接受 `*SqlDbStore` 或 `BaseDbStore`，与 Python 对齐。

---

### S-16 [严重] KVMigrator.createBackup 空备份不写入但 restoreFromBackup 静默跳过

`createBackup` 当 `backupData` 为空时不写入 KV store，但 `restoreFromBackup` 读到 nil 后直接返回 nil。迁移失败时恢复逻辑被静默跳过，导致数据不一致。

**修复方案**：空备份写入标记（如 `{"__empty__": true}`），或 `restoreFromBackup` 在备份不存在时记录 Warn。

---

### M-13 [一般] Migrator 接口与 IndexVersionMigrator 签名不一致未在 doc.go 说明

**修复方案**：在 `doc.go` 中说明此设计决策。

---

### M-14 [一般] SQLMigrator/MessageMigrator 缺少备份/恢复说明

KVMigrator 有备份/恢复，SQL/Message 没有。Python 中 SQL 依赖事务保证。

**修复方案**：在 `doc.go` 中说明。

---

### M-15 [一般] Operation 基类 TypeName() 与 Python type 属性映射

**修复方案**：审查所有 Operation 子类的 `TypeName()` 返回值对齐 Python。

---

### M-16 [一般] OperationRegistry.GetOperations 范围查询缺少日志提示

`fromVersion > toVersion` 时无日志提示。

**修复方案**：记录 Debug 日志。

---

### T-08 [提示] run_migrations.go 日志中 store_name 和 entity_key 混用

**修复方案**：对齐 Python 日志策略。

---

### T-09 [提示] migrator 包 logComponent 与 ltm 包使用相同值

**修复方案**：考虑区分来源。

---

### T-10 [提示] Operation 接口缺少 Validate() 方法

Python 的 `BaseOperation` 有 `validate` 方法。

**修复方案**：考虑在 `Operation` 接口增加 `Validate() error`。

---

## 三、TeamManager 应用层（18 严重 / 10 一般 / 4 提示）

### S-17 [严重] CreateTeam 方法体为空，返回 nil, nil

**Python 样例** (`team_helpers.py`):
```python
async def create_team(self, session_id, deep_agent, ...):
    spec = self._load_team_spec(session_id)
    self._apply_session_scoped_team_name(spec, session_id=session_id)
    spec.agent_customizer = self.build_agent_customizer(...)
    token = set_session_id(session_id)
    try:
        team_agent = spec.build()
        self._team_agents[session_id] = team_agent  # ← 注册到内存
        return team_agent
    finally:
        reset_session_id(token)
```

**Go 问题代码** (`team_manager_lifecycle.go:132-149`):
```go
func (m *TeamManager) CreateTeam(...) (*agent.TeamAgent, error) {
    // 全部 ⤵️ 委托给调用方
    return nil, nil  // ← 返回空！
}
```

**修复方案**：至少实现核心步骤——将 TeamAgent 注册到 `m.teamAgents[sessionID]` 并返回。

---

### S-18 [严重] 生命周期方法存在系统性锁竞态

Python 中 `terminate_session_runtime`/`cancel_session_runtime`/`stop_session_runtime`/`pause_session_runtime` 的所有操作在 `async with self._lock` 内完成。Go 版本在 `m.mu.Unlock()` 后才调用 `cleanupRuntimeLocals`、`ClearActiveRuntime`、`ClearPendingRuntime`，这些方法又各自加 `m.mu.Lock()`。在解锁到重新加锁之间存在竞态窗口，另一个 goroutine 可修改状态。

**Python 样例** (`team_helpers.py`):
```python
async def stop_session_runtime(self, session_id, reason):
    async with self._lock:
        # 所有操作在锁内
        await self._cleanup_runtime_locals(session_id)
        self.clear_active_runtime(session_id)
        self.clear_pending_runtime(session_id)
```

**Go 问题代码** (`team_manager_lifecycle.go:336-364`):
```go
m.mu.Unlock()
// ← 竞态窗口
m.cleanupRuntimeLocals(sessionID)  // 内部不加锁
m.ClearActiveRuntime(sessionID)    // 内部加 m.mu.Lock()
m.ClearPendingRuntime(sessionID)   // 内部加 m.mu.Lock()
```

**修复方案**：将 `cleanupRuntimeLocals`/`ClearActiveRuntime`/`ClearPendingRuntime` 拆分为加锁公开版和不加锁内部版。已持有锁的调用者使用内部版。

---

### S-19 [严重] cleanupRuntimeLocals 不持锁却修改多个 map

Python 中 `_cleanup_runtime_locals` 始终在 `_lock` 保护下调用。Go 版本直接操作 `teamEvolutionWatchers`、`streamTasks`、`teamMonitors` 等多个 map 不加锁。

**修复方案**：`cleanupRuntimeLocals` 应加 `m.mu.Lock()`，或保证所有调用者持锁。

---

### S-20 [严重] resolveSessionTeamName/resolveDeleteSessionTeamName 缺少 session metadata 回退

Python 中这两个方法最后从 `get_session_metadata(session_id)` 读取 `team_name`，Go 中标记为 `⤵️` 占位。active/pending 状态被清除后永远返回空字符串。

**Python 样例** (`team_helpers.py`):
```python
metadata = get_session_metadata(session_id)
team_name = metadata.get("team_name")
if team_name:
    return team_name
```

**修复方案**：实现从 `session.Manager.GetSessionMetadata(sessionID)` 中读取 `team_name`。也需实现 `sync_team_identity_metadata` 在 `team.runtime_ready` 事件时持久化 team identity 到 session metadata。

---

### S-21 [严重] destroyTeam/stopLocalTeamRuntime 中 TeamAgent.DestroyTeam 和 StopCoordination 未调用

**Python 样例** (`team_helpers.py`):
```python
cleaned = await team_agent.destroy_team(force=True)
await release_a2x_reservations_for_session(session_id, team_agent=team_agent)
await _stop_team_messager(team_agent, session_id=session_id)
```

**Go 问题**：全部 `⤵️` 占位，`cleaned` 始终为 false。

**修复方案**：实现 `teamAgent.DestroyTeam(ctx, true)` 调用。TeamAgent 已有此方法（9.55 已完成）。

---

### S-22 [严重] GetEnrichedTeamSpec/BuildAgentCustomizer/loadTeamSpec 全部返回 nil

`BuildAgentCustomizer` 是 Python 约 200 行的核心闭包，涉及技能同步、Rail 注册、代码适配器配置。缺失意味着 Team 运行时无法正确配置成员。

**修复方案**：分阶段实现——先 `loadTeamSpec`，再 `GetEnrichedTeamSpec`，最后 `BuildAgentCustomizer`。

---

### S-23 [严重] processTeamMessageStream 首次请求路径完全空壳

Python 的 `process_team_message_stream` 包含 `_extract_query_directives`、`_handle_team_slash_command`、`get_enriched_team_spec`、`prepare_runtime_activation`、`_consume_stream_with_query` 等核心逻辑。Go 版首次请求路径直接 `return`。

**修复方案**：至少实现核心分流——GetEnrichedTeamSpec + PrepareRuntimeActivation + stream goroutine 启动。

---

### S-24 [严重] 缺少 sync_team_identity_metadata 等价实现

Python 在 `team.runtime_ready` 事件中调用 `sync_team_identity_metadata` 持久化 team identity，确保 session metadata 中有 team_name。缺失导致 S-20 的回退路径永远走不通。

**修复方案**：实现 `SyncTeamIdentityMetadata` 函数。

---

### S-25 [严重] 缺少 ensure_monitor_for_active_runtime / ensure_team_evolution_watcher 等价实现

Python 在 `team.runtime_ready` 后自动启动 TeamMonitorHandler 和 evolution watcher。Go 的 `teamMonitors`/`teamEvolutionWatchers` map 从未被自动填充。

**修复方案**：实现 `EnsureMonitorForActiveRuntime` 和 `EnsureTeamEvolutionWatcher`。

---

### S-26 [严重] isLeaderRole 默认返回 true——违反 Python 语义

Python 中 `register_team_rail_context` 只在 `role == "leader"` 时存储。Go 默认 true 意味着非 leader 的 context 也会被存储，可能覆盖 leader 的 context。

**Python 样例**:
```python
if getattr(context.member_info, "role", None) == "leader":
    self._team_rail_contexts[session_id] = context
```

**Go 问题代码** (`team_manager_skill.go:202`):
```go
return true  // ← 非 leader 也会被存储
```

**修复方案**：默认返回 `false`。

---

### S-27 [严重] unregisterLiveRail 不调用 agent.unregister_rail

**Python 样例**:
```python
unregister = getattr(agent, "unregister_rail", None)
if callable(unregister):
    await unregister(live_rail)
```

**Go 问题**：`⤵️(#9.72) 待回填`，rail 反注册不完整。

**修复方案**：实现 agent.unregister_rail 调用。

---

### S-28 [严重] UpdateEvolutionConfig 完全空壳

Python 中约 40 行实现，包含 auto_scan 更新、skill_create_rail 反注册、重建缺失 rail 等逻辑。

**修复方案**：实现 auto_scan 更新 + skill_create rail 反注册/重建逻辑。

---

### S-29 [严重] EnsureTeamSharedSkillsInitialized 完全空壳

Python 中 `_copy_global_skills_to_team_shared_dir` + `_sync_team_shared_skills_to_agent_global`。Go 方法体为空。

**修复方案**：实现共享技能目录初始化。

---

### S-30 [严重] SyncTeamSkills 只打日志不执行同步

**Python 样例**:
```python
_sync_skills_dir(source, target)
```

**Go 问题**：只记录 Source/Target 日志，不调用同步函数。

**修复方案**：实现 Go 版 `_sync_skills_dir`（用 `filepath.Walk` + `os`/`io` 复制）。

---

### S-31 [严重] DeleteSessionRuntime 无条件返回 true

即使 `StopSessionRuntime` 返回 false（session 不存在），也返回 `true, nil`。

**Python 样例**:
```python
await self.stop_session_runtime(session_id, reason=reason)
if team_name:
    await Runner.delete_agent_team(team_name=, session_ids=[session_id], force=True)
```

**修复方案**：检查 `StopSessionRuntime` 的返回值。

---

### S-32 [严重] AttachDistributedHooksForRunnerRuntime 始终返回 false

非分布式模式下应返回 `true`（表示"无需附加分布式钩子，默认行为 OK"）。

**修复方案**：默认模式返回 `true`。

---

### S-33 [严重] FindTeamSkillRailForRequest 依赖未实现的接口断言

`hasPendingApproval` 通过接口断言 `rail.(pendingSnapshots)` 检查。如果 `TeamSkillEvolutionRail` 未实现 `HasPendingApprovalSnapshot` 方法，永远返回 nil。

**修复方案**：确认 `TeamSkillEvolutionRail` 实现了该方法。

---

### S-34 [严重] CommitRuntimeReady 对 *string 赋值使用函数参数地址

`m.activeSessionID = &sessionID`，`sessionID` 是函数参数（栈变量）。虽然 Go 编译器会逃逸到堆，但这是容易出错的模式。

**修复方案**：考虑改用值类型 `string` 配合空字符串表示 nil 语义。

---

### M-17 [一般] GetOrCreateTeam 持锁后解锁调 destroyOtherSessions 存在竞态窗口

Python 的 `get_or_create_team` 整个方法在 `_lock` 保护下。Go 版本在 unlock 和 destroyOtherSessions 之间有竞态。

**修复方案**：使用内部不加锁版本的 `destroyOtherSessions`。

---

### M-18 [一般] hasLocalTeamRuntime 缺少分布式模式检查

Python 中 `_has_local_team_runtime` 只在分布式模式下返回 True。Go 版本始终检查 `session_id in teamAgents`。

**修复方案**：加入 `_is_distributed_mode` 检查，或文档说明差异。

---

### M-19 [一般] Runner.stop_agent_team/pause_agent_team/delete_agent_team 等操作待回填

多个生命周期方法中 Runner 相关操作都是 `⤵️` 占位。

**修复方案**：实现 `runtime.GetTeamRuntimeManager().Stop/Pause/Delete` 调用。

---

### M-20 [一般] PrepareSessionSwitch 中 StopSessionRuntime 错误被忽略

**修复方案**：记录 Warn 日志。

---

### M-21 [一般] ApplyTeamPlanMode 获取 mode 但未使用

`_ = mode`，不设置 `spec.enable_team_plan`。

**修复方案**：一旦 TeamAgentSpec 有 EnableTeamPlan 字段就设置。

---

### M-22 [一般] buildSessionScopedTeamName 应使用 strings.HasSuffix

当前使用切片比较 `baseName[len(baseName)-len(suffix)-1:] == "_"+suffix`，应改用 `strings.HasSuffix(baseName, "_"+suffix)`。

---

### M-23 [一般] CancelAllStreamTasks 不等待 goroutine 完成

Python 中 `cancel_all_stream_tasks` 在 cancel 后还 `await task`。

**修复方案**：使用 `sync.WaitGroup` 或 done channel 等待 goroutine 退出。

---

### M-24 [一般] GetMonitorHandler 和 GetMonitor 语义重复

**修复方案**：保留 `GetMonitor`（对齐 Python），删除 `GetMonitorHandler`。

---

### M-25 [一般] _resolve_channel_id 逻辑分散且与 Python 不一致

空格字符串在 Python 中 `str("   " or "default")` → `"   "`，Go 中 `strings.TrimSpace("   ")` → `""` → `"default"`。

**修复方案**：统一 `_resolveChannelID` 函数。

---

### M-26 [一般] RegisterMemberRuntimeTools/NormalizeDistributedTransportFields/ParsePort 完全空壳

**修复方案**：待相关组件实现后回填。

---

### T-11 [提示] TeamManager 多处 any 类型

**修复方案**：待类型确定后替换为具体类型。

---

### T-12 [提示] 测试直接访问私有字段

**修复方案**：提供仅测试用的 setter 方法。

---

### T-13 [提示] logComponent 应区分 TeamManager/TeamHelpers

**修复方案**：定义两个组件常量。

---

### T-14 [提示] DrainTeamSkillEvents 返回 nil 而非空切片

**修复方案**：返回 `[]map[string]any{}`。

---

## 四、S 级修复验证（3 严重 / 2 一般 / 2 提示）

### S-35 [严重] Invoke/Stream 缺少 FinalizeRound 的 defer/finally 语义

Go 中 `FinalizeRound` 不在 `defer` 中执行，panic 或 error 时不会被调用。

**Python 样例**:
```python
try:
    # queue reading loop
    return last_result
finally:
    await self._coordination.finalize_round()  # ← 始终执行
```

**修复方案**：
```go
func (a *TeamAgent) Invoke(...) (map[string]any, error) {
    defer a.coordination.FinalizeRound()
    // ... queue reading loop
}
```

---

### S-36 [严重] AfterInvoke 的 context.WithoutCancel 可能导致 goroutine 永不退出

父 context 取消后，goroutine 将永远运行。

**修复方案**：设置超时 context：
```go
afterCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
defer cancel()
```

---

### S-37 [严重] CJK 截断问题仅修复了 autoRecall，其他位置仍存在

M-08/S-08 中发现的 `checkMessages`/`getHistoryMessages` 截断问题仍然存在。

**修复方案**：统一修复所有 CJK 截断。

---

### M-27 [一般] S-13 LookupHumanAgentRuntime 返回 nil 时语义模糊

无法区分"不是 human_agent"和"查找失败"。

**修复方案**：返回 `(*HumanAgentRuntime, error)`。

---

### M-28 [一般] S-20 CodeAdapter.CreateInstance 设置 instance 属性可能不完整

需确认 DeepAgent 有 instance 字段或 setter 方法。

---

### T-15 [提示] S-31 CancelMember 调用仅记 Warn 日志

Python 中 cancel_member 错误传播给调用方，Go 仅记录日志。

**修复方案**：关键错误路径应向上传播。

---

### T-16 [提示] S-37 CJK 截断修复仅局部

同 S-37。

---

## 修复优先级建议

| 优先级 | 问题编号 | 说明 |
|--------|---------|------|
| P0 (立即修复) | S-01, S-02, S-06, S-07, S-08, S-17, S-18, S-19, S-21, S-35 | crypto_key/并发竞态/错误吞没/Team空壳 |
| P1 (本轮修复) | S-03~S-05, S-09~S-16, S-20, S-22~S-34, S-36, S-37 | 回调缺失/迁移跳过/Team功能缺失 |
| P2 (下轮修复) | M-01~M-28 | 行为偏差/错误处理 |
| P3 (后续优化) | T-01~T-16 | 日志/命名/风格 |
