# 48h 逻辑审查报告（2026-10-05）

> 审查范围：48小时内提交记录覆盖的实现计划章节  
> 审查方法：逐方法对比 Python 参考实现，检查步骤缺失、占位未实现、错误处理偏差  
> 分类标准：严重=功能问题/数据损坏/安全漏洞，一般=行为偏差/缺失防御，提示=日志/风格/已知差异

---

## 审查范围

48h 内完成的实现计划章节：

| 章节 | 内容 | Python 参考路径 |
|------|------|-----------------|
| 7.16 | OpenJiuwenMemoryProvider | `openjiuwen/core/memory/external/openjiuwen_memory_provider.py` |
| 7.17 | AgentArtsMemoryProvider | `openjiuwen/core/memory/external/agentarts_memory_provider.py` |
| 7.19a/c | MemoryAnalyzer / Generator | `openjiuwen/core/memory/process/extract/` |
| 7.20 | Dreaming Orchestrator + Sweeper | `openjiuwen/core/memory/dreaming/` + `jiuwenswarm/agents/harness/common/memory/dreaming/` |
| 7.22-7.23 | Migration Operations + Migrators | `openjiuwen/core/memory/migration/` |
| 7.27 | LongTermMemory | `openjiuwen/core/memory/long_term_memory.py` |
| monitor | MonitorEventType/TeamMonitor/MonitorHandler/StreamLogger/event_types | `jiuwenswarm/agents/harness/team/monitor/` |
| team | 应用层 TeamManager + InteractInput | `jiuwenswarm/agents/harness/team/team_manager.py` |
| agent-teams | 14 TeamTool 子类 MapResult | `openjiuwen/core/multi_agent/teams/` |
| coordination | CoordinationKernel S-01~S-34 修复验证 | `openjiuwen/agent_teams/agent/coordination/kernel.py` |

---

## 问题汇总

| 严重级别 | 数量 |
|----------|------|
| **严重 (S)** | 21 |
| **一般 (M)** | 45 |
| **提示 (T)** | 24 |
| **总计** | 90 |

---

## 一、7.16 OpenJiuwenMemoryProvider（3S / 5M / 3T）

### S-01: handleSearch threshold=0 被零值守卫错误回退到 0.3

Go 的 `handleSearch` 中 `if v := floatVal(t); v != 0` 把用户显式传入的 `threshold=0` 误判为"未提供"，回退到 0.3。

**Python** (`openjiuwen_memory_provider.py` L302-308):
```python
results = await self._ltm.search_user_mem(
    query=args.get("query", ""),
    num=args.get("num", 5),
    user_id=self._user_id,
    scope_id=self._scope_id,
    threshold=args.get("threshold", 0.3),  # 只要提供了就用，即使是 0
)
```

**Go** (`openjiuwen_provider.go` L537-542):
```go
threshold := 0.3
if t, ok := args["threshold"]; ok {
    if v := floatVal(t); v != 0 {  // BUG: threshold=0 被回退
        threshold = v
    }
}
```

**修复方案**: 删除 `v != 0` 守卫，直接使用 args 中的值：
```go
threshold := 0.3
if t, ok := args["threshold"]; ok {
    threshold = floatVal(t)
}
```

---

### S-02: Initialize store 创建失败返回 nil 而非 error

Go 的 `Initialize` 在 store 创建失败时 `return nil`，调用方依赖 `err == nil` 判断成功，但 `initialized` 仍为 false。

**Python** (`openjiuwen_memory_provider.py` L114-116):
```python
if not self._kv_store or not self._vector_store or not self._db_store:
    logger.error("[OpenJiuwenMemoryProvider] Store creation failed")
    return  # Python 是 void 返回，语义正确
```

**Go** (`openjiuwen_provider.go` L280-284):
```go
if p.kvStore == nil || p.vectorStore == nil || p.dbStore == nil {
    logger.Error(ojLogComponent).Msg("Store 创建失败")
    return nil  // BUG: Go 约定应返回 error
}
```

**修复方案**:
```go
return fmt.Errorf("store creation failed: kv=%v vector=%v db=%v",
    p.kvStore != nil, p.vectorStore != nil, p.dbStore != nil)
```

---

### S-03: SyncTurn 中 add_messages 错误被匿名函数吞掉

匿名函数中 `AddMessages` 的 error 仅 log，外层永远 `return nil`。Go 接口约定返回 error，调用方无法感知失败。

**Python** (`openjiuwen_memory_provider.py` L216-225):
```python
try:
    await self._ltm.add_messages(...)
except Exception as e:
    logger.warning(f"sync_turn add_messages failed: {e}")
# Python 是 void 返回，吞错是 OK 的
```

**Go** (`openjiuwen_provider.go` L489-508):
```go
func() {
    defer func() { ... }()
    _, err := ltmInstance.AddMessages(...)
    if err != nil {
        logger.Warn(ojLogComponent).Err(err).Msg("sync_turn add_messages 失败")
        // error 仅在匿名函数内，不传播
    }
}()
return nil  // 永远 nil
```

**修复方案**: 使用 named return 或 err 变量捕获：
```go
var syncErr error
func() {
    defer func() { if r := recover(); r != nil { ... } }()
    _, err := ltmInstance.AddMessages(...)
    if err != nil {
        logger.Warn(ojLogComponent).Err(err).Msg("sync_turn add_messages 失败")
        syncErr = err  // 传播错误
    }
}()
return syncErr
```

---

### M-01: buildOpenJiuwenProviderConfig KV 默认后端 "shelve" vs Python "memory"

**Go** (`deep_adapter_config.go`): `kvBackend := strOr(strVal(ojCfg["kv_type"]), "shelve")`  
**Python**: `_DEFAULT_KV_BACKEND = "memory"`

**修复方案**: 改为 `strOr(strVal(ojCfg["kv_type"]), defaultKVBackend)` // "memory"

---

### M-02: buildOpenJiuwenProviderConfig 缺少 scope_config 传递

Go 构建的 config dict 没有 `scope_config` 键，通过 `buildOpenJiuwenProviderConfig` 创建的 Provider 永远不会解析 scope_config。

**修复方案**: 在 `buildOpenJiuwenProviderConfig` 中添加 scope_config 的传递。

---

### M-03: Prefetch parts 拼接产生多余换行

Go 的 `"\n## Related History Summaries"` 自带换行符，又通过 `for i, part := range parts` 在 i>0 时追加 "\n"，导致和 Python 的 `"\n".join(parts)` 输出不同。

**修复方案**: 使用 `strings.Join(parts, "\n")` 替代手动拼接，或在 history summary 标题中移除前导 "\n"。

---

### M-04: HandleToolCall 合并两个工具到同一 case 分支

`case "ltm_search", "ltm_search_summary":` 合并后可维护性降低，如果 `dispatchToolCall` 出错无法区分哪个工具。

**修复方案**: 拆分为两个独立 case（不紧急，逻辑正确）。

---

### M-05: createDBStore 同步 vs Python 异步

Go 的 `gorm.Open` 是同步 API，Python 的 `_create_db_store` 是 `async`。已知差异，无需修改。

---

### T-01: OnSessionEnd 依赖基类空实现（对齐 Python）

### T-02: Shutdown 不影响 LTM 全局单例（设计差异，非 bug）

### T-03: createVectorStore 缺少 panic 保护

Go 的 `NewChromaVectorStore` 没有 error 返回，也没有 `defer/recover`。如果内部 panic 不会被捕获。

---

## 二、7.20 Dreaming Orchestrator + Sweeper（5S / 8M / 4T）

### S-01: RunSweep Scan 阶段异常未捕获

Python 用 `try/except` 包裹 `scan_new_sessions`，失败时 log + return early。Go 无任何保护。

**Python** (`sweeper.py` L128-134):
```python
try:
    sessions = await asyncio.get_running_loop().run_in_executor(
        None, self.scan_new_sessions,
    )
except Exception:
    logger.exception("[Sweeper] Scan stage failed")
    return
```

**Go** (`sweeper.go` L278):
```go
sessions := s.ScanNewSessions()  // 无异常保护
```

**修复方案**: 在 `RunSweep` 中为 `ScanNewSessions` 添加 `defer recover()` 保护，或让其返回 error 并处理。

---

### S-02: TryStopDreaming 缺少异常保护

Python `try_stop_dreaming` 用 `try/except` 包裹 `stop_dreaming()`。Go 直接调用 `swarmdreaming.StopDreaming()` 无保护。

**Python** (`interface_deep.py` L5959-5965):
```python
try:
    await stop_dreaming(mode=mode)
    self._dreaming_started = False
except Exception as exc:
    logger.warning("[JiuWenClawDeepAdapter] stop_dreaming failed: %s", exc)
```

**Go** (`deep_adapter_dreaming.go` L118-135):
```go
d.dreamingStarted = false
swarmdreaming.StopDreaming(ctx, d.dreamingMode)  // 无 try/except / recover
```

**修复方案**: 添加 `recover()` 保护或将 `StopDreaming` 改为返回 error 并 log。

---

### S-03: TryStartDreaming 异常日志级别不匹配

Python 用 `logger.warning`，Go 用 `logger.Error`。改变运维告警行为。

**修复方案**: 将 `logger.Error` 改为 `logger.Warn`，对齐 Python。

---

### S-04: TryStartDreaming 先设 dreamingStarted=true 再调 start — 步骤顺序错误

Go 在调用 `StartDreaming` 前就设 `d.dreamingStarted = true`，创建一个"标记运行但实际无 orchestrator"的窗口。Python 仅在成功后设 `self._dreaming_started = orch is not None`。

**Python** (`interface_deep.py` L5936-5954):
```python
if self._dreaming_started:
    return
try:
    orch = await start_dreaming(...)
    self._dreaming_started = orch is not None  # 仅在成功后设置
except Exception as exc:
    logger.warning(...)
    # _dreaming_started 保持 False
```

**Go** (`deep_adapter_dreaming.go` L51-97):
```go
d.dreamingStarted = true  // Step 4: 调用前就设 — 错误
orch, err := swarmdreaming.StartDreaming(...)
if err != nil {
    d.dreamingStarted = false  // 回退
    return err
}
d.dreamingStarted = orch != nil  // Step 5: 成功后又设 — 冗余
```

**修复方案**: 删除步骤 4 的 `d.dreamingStarted = true`，仅在成功后设 `d.dreamingStarted = orch != nil`。

---

### S-05: TryStopDreaming 异常后不回退 dreamingStarted

Go 在调用 `StopDreaming` 前就设 `d.dreamingStarted = false`。如果 StopDreaming 失败，状态不一致（标记停止但 orchestrator 仍在运行）。

**Python** (`interface_deep.py` L5962-5963):
```python
await stop_dreaming(mode=mode)
self._dreaming_started = False  # 仅在成功后清除
```

**修复方案**: 将 `d.dreamingStarted = false` 移到 `StopDreaming` 调用之后，或在失败时不清除。

---

### M-01: Init 加载 checkpoint 未处理 Python 的 list→dict 转换逻辑

**修复方案**: 在 `loadCheckpoint` 中增加 fallback：如果 `ScannedSessions` 为空但原始 JSON 是 list，转为 map。

### M-02: Orchestrator.tick 中 context.Canceled 仅 return 不传播，loop 继续

**修复方案**: tick 检测 `context.Canceled` 后应让 loop 退出（返回 sentinel error 或 loop 检查 ctx）。

### M-03: extractContentStr 对 dict 类型处理差异

Go 使用 `MessageContent.Parts()` 类型化处理，Python 用 `isinstance(item, dict)` + `item.get("text")`。Go 的类型系统更安全，但需确保覆盖所有 LLM 返回格式。

### M-04: StartDreaming TOCTOU 竞态条件

`RLock` 检查后释放，再 `Lock` 写入，中间可能另一个 goroutine 也通过检查。

**修复方案**: 使用单个 `Lock` 或 `sync.Map.LoadOrStore` 或双重检查。

### M-05: TryStartDreaming/TryStopDreaming 使用 context.Background() 丢弃调用链上下文

**修复方案**: 传入 server-lifetime context 替代 `context.Background()`。

### M-06: Orchestrator.loop 在 ctx.Done 后未设置 running=false

**修复方案**: 在 `ctx.Done()` 分支中添加 `o.running = false`。

### M-07: tick 中 context.Canceled 不传播（与 M-02 相关）

### M-08: ScanNewSessions 使用 ModTime().Unix() 丢失浮点精度

Python 用 `st_mtime`（float，含亚秒），Go 用 `Unix()`（整数秒）。

**修复方案**: 改用 `float64(info.ModTime().UnixNano()) / 1e9`。

### T-01: RunSweep 返回 error 但从不返回非 nil（与 Python 一致）

### T-02: LoadExistingSummary 未按文件名排序

**修复方案**: 在 `os.ReadDir` 后排序 entries。

### T-03: sortedKeys 使用冒泡排序

**修复方案**: 改用 `sort.Strings(keys)` 或 `slices.Sort(keys)`。

### T-04: promoteAgent 写入 DREAMING.md 拼接格式细微差异

---

## 三、7.22-7.23 Migration（5S / 6M / 3T）

### S-01: SQLMigrator 缺少 _validate_table 校验

Python 校验表名是否在 MEMORY_TABLES_CONFIG 支持列表，Go 完全缺失。

**Python** (`sql_migrator.py` L24-35):
```python
@staticmethod
def _validate_table(table_name: str):
    if table_name not in [table_config["table"].name for table_config in MEMORY_TABLES_CONFIG]:
        raise ValueError(f"Unsupported table name: {table_name}")
```

**修复方案**: 在每个 `migrateXxx` 方法入口添加表名校验。

---

### S-02: SQLMigrator.updateVersion 逻辑与 Python 不一致 — 可能产生重复记录

Python 用"先 UPDATE，rowcount==0 再 INSERT"的 upsert 模式。Go 先 Add 再 Update，当 schema_version 不同时 Add 会插入新行，导致 memory_meta 表中同一 table_name 出现多条记录。

**Python** (`sql_migrator.py` L220-238):
```python
update_stmt = update(memory_meta_table).where(
    memory_meta_table.c.table_name == table_name
).values(schema_version=target_version)
result = sync_conn.execute(update_stmt)
if result.rowcount == 0:
    insert_stmt = insert(memory_meta_table).values(
        table_name=table_name, schema_version=target_version
    )
    sync_conn.execute(insert_stmt)
```

**Go** (`sql_migrator.go` L128-147):
```go
if err := m.metaManager.Add(ctx, tableName, versionStr); err != nil {
    return err  // Add 检查 table_name+schema_version 联合唯一性
}
result := m.db.WithContext(ctx).Table("memory_meta").
    Where("table_name = ?", tableName).
    Update("schema_version", versionStr)  // 无条件 Update
```

**修复方案**: 改为先 Update，检查 `RowsAffected==0` 时再 Insert，对齐 Python upsert。

---

### S-03: MessageMigrator.createBackup 未实现分页 — 只取前 1000 条

Go 单次调用 `GetMessages(ctx, nil, backupPageSize, "timestamp", "asc")` limit=1000，超过部分丢失。

**修复方案**: 实现分页循环，按 offset/limit 反复调用直到返回空列表。

---

### S-04: SQLMigrator.migrateAddColumn 原生 SQL 存在 SQL 注入风险

Go 用 `fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s %s", ...)` 直接拼接表名列名。

**Python**: 使用 Alembic 的 `op.add_column` API，内部参数化。

**修复方案**: 使用 GORM 的 `Migrator().AddColumn()` 或校验表名/列名不含特殊字符。

---

### S-05: SQLMigrator.migrateUpdateColumnType 原生 SQL 语法不兼容 MySQL

Go 对非 SQLite 方言使用 `ALTER TABLE %s ALTER COLUMN %s TYPE %s`（PostgreSQL 语法），MySQL 应为 `MODIFY COLUMN`。

**修复方案**: 检测方言名，MySQL 用 `MODIFY COLUMN`，PostgreSQL 用 `ALTER COLUMN TYPE`。

---

### M-01: SQLMigrator 缺少 BatchMigrate 方法

Python 有 `batch_migrate` 公共 API，Go 未实现。

**修复方案**: 添加 `func (m *SQLMigrator) BatchMigrate(ctx context.Context, migrations []BatchMigration) map[string]bool`。

### M-02: KVMigrator 备份数据类型与 Python 不一致

Go 将所有 value 转为 `string(v)`（`[]byte → string`），非字符串值（嵌套 dict/list）可能被损坏为 base64。

**修复方案**: 保留原始 `[]byte`，或用 JSON 序列化每个 value。

### M-03: IndexVersionMigrator 对 DefaultValueOrFunc 的类型判断不完整

Python 用 `callable()` 对任意可调用对象生效，Go 只匹配 `func() any` 签名。

**修复方案**: 使用 `reflect` 判断函数类型并调用。

### M-04: RunMigrations 中错误链未包含原始 cause

Python 用 `raise build_error(..., cause=e) from e`，Go 的 `exception.BuildError` 不含原始 error cause 链。

**修复方案**: 使用 `%w` 动词包装或确保 BuildError 支持 cause。

### M-05: MessageMigrator.restoreFromBackup 版本重置失败中断恢复

Python 将版本重置放在独立 try/except，失败不影响主流程。Go 在 SetSchemaVersion 失败时 `return err` 中断。

**修复方案**: 版本重置失败时仅 log 不 return，对齐 Python 防御性处理。

### M-06: AddColumnOperation.Nullable 默认值 false vs Python True

Go 零值为 false，Python 默认 `nullable: bool = True`。

**修复方案**: 已有 `NewAddColumnOperation` 设置 true，但直接构造 struct 时需注意。

### T-01: KVMigrator TryMigrate 中 cleanupBackup 错误被忽略但已记录（与 Python 一致）

### T-02: VectorMigrator.findCollections supportedTypes 传入方式差异（行为等价）

### T-03: KVMigrator _get_current_version 处理 int 类型值（Go KV store 统一 []byte，当前足够）

---

## 四、7.27 LongTermMemory（2S / 6M / 5T）

### S-01: UpdateVariables 吞掉错误 — 循环中 err 仅记录不返回

**Python** (`long_term_memory.py` L1268-1274):
```python
for name, value in variables.items():
    await self.variable_manager.update_user_variable(...)  # 异常直接传播
```

**Go** (`update_ops.go` L72-77):
```go
for name, value := range variables {
    if err := m.variableManager.UpdateUserVariable(ctx, p.UserID, p.ScopeID, name, value); err != nil {
        logger.Error(logComponent).Err(err).Str("name", name).Msg("更新变量失败")
        // 未 return err！继续循环
    }
}
```

**修复方案**: 在 `if err != nil` 分支中 `return err`，与 Python 异常传播语义一致。

---

### S-02: RegisterStore 缺少 vector_store/db_store/message_store 的 nil 校验

Python 对三个可选参数做 `isinstance` 校验。Go 的验证函数全部空实现（`return nil`），且 RegisterStore 中只调了 `validateKVStore`，未调其余三个。nil 接口值可通过编译但运行时 panic。

**修复方案**: 对可选参数调用 nil 检查（`if params.vectorStore != nil { validateVectorStore(params.vectorStore) }`）。

---

### M-01: validateID 的 scopeID 长度校验使用 byte 长度而非 rune 长度

Python `len(scope_id)` 返回字符数，Go `len(scopeID)` 返回字节数。CJK 字符占 3 字节，128 字节限制实际只允许约 42 个 CJK 字符。

**修复方案**: 改为 `utf8.RuneCountInString(scopeID) > 128`。

### M-02: searchUserMemImpl 缺少 AttributeError 分支的 Debug 日志

Python 有三个 except 分支（AttributeError→debug、ValueError→warning、Exception→warning），Go 只区分了 Validation 和其他。

**修复方案**: 增加 AttributeError 等价分支，使用 `logger.Debug` 级别。

### M-03: AddMessages 缺少 ValueError 异常分支处理

Python 对 `write_manager.add_memories` 有 `except ValueError` 专门捕获，Go 统一处理所有 error。

**修复方案**: 增加 ValueError 等价分类。

### M-04: GetUserMemByPage 返回 nil 而非空切片

JSON 序列化时 `nil → null`，Python `[] → []`。

**修复方案**: 改为 `return []*MemInfo{}, nil`。

### M-05: SetConfig 中 SqlMessageStore 的 codec 回填方式与 Python 不同

Python 只在 `crypto_key is None` 时回填，Go 每次都覆盖。

**修复方案**: 增加 `if sqlMsg.GetStorageCodec() == nil` 条件判断。

### M-06: getScopeEmbeddingModel 缺少 APIEmbedding 实例化异常捕获

Python 有 `try/except Exception`，Go 直接调用 `NewAPIEmbedding` 无保护。

**修复方案**: 添加 `defer recover()` 保护。

### T-01: GetVariables 中 Python 支持 str 单值查询（API 形态差异，功能等价）

### T-02: GetVariables 缺少 Python 的类型错误（Go 静态类型已保证）

### T-03: _check_messages 截断 rune vs Python 字符切片（行为一致）

### T-04: add_messages session_id 默认值一致

### T-05: GetRecentMessages 缺少 Python 的 messageManager==nil warning 日志（Go 更防御性）

---

## 五、Monitor 模块（2S / 6M / 6T）

### S-01: TeamMonitor.Stop() 关闭 channel 后无法重新 Start

Python `stop()` 只发送 sentinel `None`，不关闭 Queue。Go `close(m.eventCh)` 后再 Start 写入已关闭 channel 会 panic。

**Python** (`team_monitor.py` L108-115):
```python
async def stop(self) -> None:
    if not self._started:
        return
    self._team_agent.remove_event_listener(self._on_event)
    self._started = False
    self._event_queue.put_nowait(None)  # sentinel, 不关闭 Queue
```

**Go** (`team_monitor.go` L145-166):
```go
close(m.eventCh)  // 关闭 channel，不可恢复
```

**修复方案**: 移除 `close(m.eventCh)`，改为发送 nil sentinel。Start 中如果 channel 已关闭则重新创建。

---

### S-02: TeamMonitorHandler.getMessageContent 使用 context.Background() 丢失 session context

**Go** (`monitor_handler.go` L498):
```go
msgs, err := mon.GetMessages(context.Background(), "", "")
```

**Python** (`monitor_handler.py` L223-253):
```python
token = set_session_id(self._session_id)
try:
    messages = await self._monitor.get_messages()
```

**修复方案**: 将 `collectEvents` 的 ctx 传递到 `getMessageContent` 中。

---

### M-01: capStr 按字节截断会破坏 CJK 多字节字符

Go `len(text)` 返回字节数，Python `len(text)` 返回字符数。

**修复方案**: 改用 `utf8.RuneCountInString` 和 `[]rune(text)[:limit]`。

### M-02: stream_logger 时间格式缺少时区信息

Python 用 `datetime.now().astimezone()`，Go 用 `time.Now().Format("2006-01-02 15:04:05.000")` 不含时区。

**修复方案**: 使用 `time.Now().Format("2006-01-02 15:04:05.000 MST")`。

### M-03: Flush 关闭文件前未 Sync

Python 每次写后 `self._file.flush()`，Go 无 Sync 调用。

**修复方案**: 在 `Flush()` 关闭前调用 `l.file.Sync()`。

### M-04: safeWrite 部分调用路径缺少时间戳

`emit` 自行添加时间戳后调 safeWrite，但 Feed 的错误恢复路径直接调 safeWrite 无时间戳。

**修复方案**: 在 `safeWrite` 中统一添加时间戳前缀（对齐 Python `_safe_write`）。

### M-05: GetTeamSnapshot 吞没 GetMembers 错误

`GetMembers` 失败时返回 `(nil, nil)`，丢失错误信息。

**修复方案**: 返回 `(nil, err)` 或降级为空列表。

### M-06: TeamMonitorHandler 接口缺少 TeamID() 方法

Python 有 `team_id` 属性，Go 接口未定义。

**修复方案**: 在 `TeamMonitorHandler` 接口中增加 `TeamID() string`。

### T-01: _feed 跳过非 TeamOutputSchema 类型检查（Go 用 map[string]any 无法做 instanceof）

### T-02: _render_role 返回值类型差异（None vs ""，功能等价）

### T-03: MonitorEventTypeMemberCanceled 未映射（与 Python 一致）

### T-04: autoStartEvolutionWatcher 占位代码（⤵️ 待回填）

### T-05: consumeMonitorEvents 广播功能为空壳（TODO #9.85）

### T-06: FromEventMessage 时间戳使用 time.Now()（与 Python 一致）

---

## 六、TeamManager 应用层 + TeamTools（4S / 10M / 1T）

### S-01: Interact 缺少顶层异常捕获

Python 的 `interact` 有 `try/except Exception` 始终返回 False。Go 只处理 `mgr.Interact` 返回的 error，但方法整体无 panic 保护。

**Python** (`team_manager.py` L1156-1183):
```python
async def interact(self, session_id: str, user_input: Any) -> bool:
    try:
        ...
        return success
    except Exception as exc:
        logger.error("[TeamManager] interact failed: session_id=%s, error=%s", session_id, exc)
        return False
```

**修复方案**: 添加 `defer recover()` 或用 Go 的 `(bool, error)` 返回值确保 panic 时返回 `(false, err)`。

---

### S-02: hasLocalTeamRuntime 缺少分布式模式前置检查

Python 检查 `_is_distributed_mode(get_config()) and session_id in self._team_agents`，Go 直接检查 `sessionID in teamAgents`。非分布式模式下语义错误，影响 terminate/stop/cancel/pause 的分支判断。

**Python** (`team_manager.py` L1453-1455):
```python
def _has_local_team_runtime(self, session_id: str) -> bool:
    return self._is_distributed_mode(get_config()) and session_id in self._team_agents
```

**Go** (`team_manager_monitor.go` L152-156):
```go
func (m *TeamManager) hasLocalTeamRuntime(sessionID string) bool {
    _, ok := m.teamAgents[sessionID]
    return ok  // 缺少分布式模式前置条件
}
```

**修复方案**: 添加 `isDistributedMode()` 检查。

---

### S-03: cleanupRuntimeLocals 使用 context.Background() 停止监控

丢弃原始 context，可能导致资源泄漏或竞态。

**修复方案**: `cleanupRuntimeLocals` 改为接受 `context.Context` 参数，传递给 `handler.Stop()`。影响 TerminateSessionRuntime/CancelSessionRuntime/StopSessionRuntime/PauseSessionRuntime/destroyTeam 等调用。

---

### S-04: destroyTeam 缺少 set_session_id/reset_session_id 上下文管理

Python 在 `_destroy_team` 前设置 `set_session_id(session_id)`，finally 中 `reset_session_id(token)`。Go 完全缺失。

**修复方案**: 通过 ctx 传播 sessionID 或在 DestroyTeam 调用前设置 session 上下文。

---

### M-01: CreateTeam 缺少 _ensure_postgresql_for_leader 调用

Python 在构建 spec 前调用，Go 完全跳过且未标注待回填。

### M-02: consumeMonitorEvents 缺少事件广播（前端无法接收实时事件）

Python 每个事件调用 `_broadcast_event`，Go 只记日志，标注 TODO(#9.85)。

### M-03: BuildAgentCustomizer 闭包体 5 个核心步骤为占位

步骤 a-e 全部标注 ⤵️ 待回填，仅步骤 f（本身也是空壳）有代码。

### M-04: RegisterMemberRuntimeTools 为空壳

Python 注册 CronRuntimeBridge + SendFileToolkit，Go 为空。

### M-05: EnsureTeamSharedSkillsInitialized 为空壳

Python 复制全局技能到团队共享目录，Go 为空。

### M-06: UpdateEvolutionConfig 为空壳

Python 执行热更新，Go 为空。

### M-07: DrainTeamSkillEvents 返回空切片

Python 调用 `rail.drain_pending_approval_events()`，Go 始终返回空。

### M-08: autoStartEvolutionWatcher 为占位 goroutine

Python 订阅事件并推送 server_push，Go 只等 context 取消。

### M-09: unregisterLiveRail 缺少 agent.unregister_rail 调用

### M-10: workspace_meta 工具在 CreateTeamTools 的 allTools 中缺失

设计上由 TeamToolRail 单独添加，但需确认过滤逻辑不因此产生问题。

### T-01: Interact 方法签名差异 (bool, error) vs Python 的 bool

---

## 七、CoordinationKernel S 级修复验证（0S / 4M / 2T）

所有 17 项 S 级修复（S-01~S-34）均已验证正确。

### M-01: Setup bp/inf nil 时继续执行而非返回错误

Python raise RuntimeError，Go 只 log warning 后继续。

**修复方案**: 返回 error 或从 host 自动提取 bp/inf。

### M-02: EnqueueUserInput 只接受 string，Python 接受 dict 或 string

**修复方案**: 改为 `EnqueueUserInput(inputs any)` 内部提取 query。

### M-03: persistTeamLifecycle 忽略 ctx 参数

**修复方案**: 传递 ctx 到 I/O 操作。

### M-04: TeamCompletionHandler.OnPollTask 是 TODO stub (#9.63)

### T-01: TeamTopic 迭代硬编码三个值

### T-02: EnqueueMailboxAfterFirstIteration 同步阻塞

---

## 关键修复优先级

### P0 — 必须立即修复（功能/数据/安全）

| ID | 章节 | 描述 |
|----|------|------|
| S-04 | 7.20 | TryStartDreaming 先设 flag 再调 start，步骤顺序错误 |
| S-05 | 7.20 | TryStopDreaming 先清 flag 再调 stop，失败后状态不一致 |
| S-01 | 7.16 | handleSearch threshold=0 被零值守卫回退 |
| S-02 | 7.23 | SQLMigrator updateVersion 可能产生重复记录 |
| S-03 | 7.23 | MessageMigrator 备份只取 1000 条，超过部分丢失 |
| S-04 | 7.23 | SQL 注入风险 |
| S-01 | 7.27 | UpdateVariables 吞掉错误不返回 |
| S-01 | monitor | TeamMonitor.Stop() 关闭 channel 后无法重新 Start |
| S-02 | team | hasLocalTeamRuntime 缺少分布式模式检查 |
| S-03 | team | cleanupRuntimeLocals 使用 context.Background() |

### P1 — 应尽快修复（行为偏差/缺失防御）

| ID | 章节 | 描述 |
|----|------|------|
| S-02 | 7.16 | Initialize store 失败返回 nil 而非 error |
| S-03 | 7.16 | SyncTurn add_messages 错误被吞掉 |
| S-01 | 7.20 | RunSweep Scan 阶段无异常保护 |
| S-02 | 7.20 | TryStopDreaming 无异常保护 |
| S-03 | 7.20 | TryStartDreaming 日志级别 Error 应为 Warn |
| S-01 | 7.22 | SQLMigrator 缺少表名校验 |
| S-05 | 7.23 | migrateUpdateColumnType MySQL 语法不兼容 |
| S-02 | 7.27 | RegisterStore 缺少 nil 校验 |
| S-02 | monitor | getMessageContent 使用 context.Background() |
| S-01 | team | Interact 缺少顶层异常捕获 |
| S-04 | team | destroyTeam 缺少 session context 管理 |

### P2 — 建议修复（占位/日志/性能）

所有 M 级问题及以下，按实际影响评估。重点关注：
- monitor M-01: capStr CJK 截断破坏
- monitor M-04: safeWrite 时间戳不一致
- 7.20 M-08: mtime 精度丢失
- 7.27 M-01: validateID 字节 vs 字符
- team M-03: BuildAgentCustomizer 占位（核心功能缺失）
