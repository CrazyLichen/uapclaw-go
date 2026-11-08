# E 组（记忆）缺陷对齐设计（2026-11-08）

> 上游基线：agent-core `5a35c39f`、jiuwenswarm `6c6434e8e`
> 缺陷来源：[`docs/sync/defect-plan-2026-11-04.md`](../../../docs/sync/defect-plan-2026-11-04.md) §3.E（13 行，核验前全部为 🔍）
> 本文件记录 13 条的核验结论与修复决策。

---

## 0. 核验结论总表

逐条比对上游 `git show <sha>` 原始 diff 与 Go 生产代码：

| 编号 | 级别 | 结论 | Go 落点 |
| --- | --- | --- | --- |
| E-01 | P0 | 存在 | `ltm/add_messages.go`、`mem_model/sql_message_store.go` |
| E-02 | P1 | 存在（最严重） | `lite/manager_impl.go`、`lite/embeddings.go` |
| E-03 | P1 | 存在（比文档描述更严重） | `lite/embeddings.go`、`lite/manager_impl.go` |
| E-04 | P1 | 存在（两份实现） | `lite/internal.go` + `lite/manager_impl.go`；`swarm/.../common/memory/internal.go` + `manager.go` |
| E-05 | P2 | 存在（影响超出 P2） | `manage/index/summary_manager.go`、`foundation/store/index/simple.go` |
| E-06 | P1 | **不适用** | Go 无 `JiuwenMemoryProvider`（mem2） |
| E-07 | P1 | 存在 | `ltm/config_ops.go`、`manage/mem_model/scope_user_mapping_manager.go` |
| E-08 | P0 | 存在 | `migration/migrator/message_migrator.go` |
| E-09 | P1 | **不适用** | 同 E-06 |
| E-10 | P1 | 存在（4 个独立缺口） | `agent_teams/memory/manager.go`、`agent_teams/agent/coordination/kernel.go`、`agent_teams/agent/agent_configurator.go`、`agent_teams/agent/team_agent.go` |
| E-11 | P2 | 存在 | `memory/prompts/fragment_memory_prompt.md` |
| E-12 | P1 | 存在（去重完全空转） | `graph/graph_memory/parse_llm_response.go` |
| E-13 | P2 | 存在（`Dispose` 缺两项） | `agent_teams/harness/native_harness.go`、`lite/manager_impl.go` |

统计：**存在 11 条、不适用 2 条（E-06/E-09）**。

### 核验补充事实

- E-06 / E-09 不适用：上游这两个修复都落在 `openjiuwen/core/memory/external/jiuwen_memory_provider.py`（`mem2` server/SDK 双后端）。Go 的 `memory/external/` 只有 openjiuwen / mem0 / viking / agentarts 四个 provider，无此实现，也无 `/v1` 契约面。**故本 spec 只覆盖其余 11 条**。
- E-04 涉及**两份**平行实现，各有各的上游：
  - `internal/agentcore/memory/lite/` ↔ agent-core `core/memory/lite/`，上游**已修**（`split_query_tokens` + trigram + LIKE 兜底）。
  - `internal/swarm/agents/harness/common/memory/` ↔ jiuwenswarm `agents/harness/common/memory/`，上游 HEAD **未修**（仍 `\w+` + OR，全文件 0 处 LIKE），且该目录只 import 同包模块，不引用 `openjiuwen.core.memory.lite`——两仓库各自维护平行实现，上游本身即有两份。
- E-11 提示词漂移全量核对：`memory_analysis_prompt.md`、`memory_update_check.md`、`semantic_validation.md` 与上游**逐字节一致**；`graph/extraction/prompts/cn/*.pr.md` 11 个文件亦**逐字节一致**。漂移仅 `fragment_memory_prompt.md` 一处。
- E-13 上游 `dispose` 除 memory manager 外还释放 `sys_operation`；Go 的 `Dispose` 两项都缺（`Stop` 第 6 步仅一行占位，注释自承 teardown_tools 缺等价物）。

---

## 1. E-01 消息时区导致排序错乱

**上游**：`5ba83785`（agent-core）——`add_messages` 入口归一化时间戳：`None → now(utc)`，非空 → `astimezone(utc)`；随后 `strftime('%Y-%m-%d %H:%M:%S')` 落盘（不带偏移）。

**Go 缺陷**：
- `ltm/add_messages.go:127` 为 nil 时用 `time.Now()`（本地时区）。
- `mem_model/sql_message_store.go:114/160` 落盘 `Format(time.RFC3339)`（带 `+08:00`）。
- `GetMessages` 以 `ORDER BY timestamp` 字符串排序；同一列混入 `Z` 与 `+08:00` 后字典序不等于时序。
- `GetWithSortAndTimeRange` 的 `timestamp >= ?` 边界同样按 RFC3339 格式化。

**修复（方案 A）**：
1. `add_messages` 的 timestamp 归一化为 `UTC()`。
2. 落盘格式改为 `msgTimestampLayout = "2006-01-02 15:04:05"`（与上游 strftime 对齐）。
3. `generateMessageID` 同步改用新格式（与 Python `timestamp.__str__()` 对齐）。
4. `rowToMessageAndMeta` 解析：先按新格式，**失败回退 RFC3339**（兼容历史行）。
5. 范围查询边界值按新格式格式化。

**已知偏差 D-1**：历史行仍为 RFC3339 带偏移，不做数据迁移（历史 `message_id` 由旧格式算出，无法回溯改写）。读取侧回退分支即为兼容路径。回收条件：上游若引入时间戳归一化迁移器，按上游实现对齐。

---

## 2. E-02 embedding 配置变更后索引无法重建

**上游**：`0d62218` + `5dcd1357`（agent-core）——① `providerKey` 改为含归一化 base_url + `api_key` sha256[:16] 的 `config_fingerprint`；② 初始化阶段 `_needs_rebuild_on_config_change` 先判配置是否变化，变化则 `_clear_index_tables()` 再 `sync(force=True)`（跨进程文件锁串行）；③ `sync(force=True)` 令 hash 未变的文件也重新嵌入。

**Go 缺陷**：
- `lite/embeddings.go` 无 `ConfigFingerprint`，`manager_impl.go:712` 只拼 `id:model`。
- `needsReindex` 不比对 `providerKey`。
- `runReindex` 调 `syncMemoryFiles/syncSessionFiles` 时**没有 force**，两函数也无 force 参数 → hash 未变的文件被跳过。
- 无 `clearIndexTables`。
- 净效果：换模型/key 触发重建后**索引表被清空却重新填充不出内容**，检索永久为空。

**修复（方案 A，全量对齐上游）**：
1. 新增 `ConfigFingerprint()`：`{id}:{model}:{归一化base_url}:{sha256(api_key)[:16]}`。
2. 新增 `needsRebuildOnConfigChange()`：仅比对 `provider / model / providerKey / chunkTokens`；**`provider == nil` 时返回 false**（运行期健康问题，不清表）；无 meta 时返回 false（新库直接走正常 sync）。
3. 新增 `clearIndexTables()`：清 `files / chunks / chunks_vec / chunks_fts`（后两者按存在性）。
4. `syncMemoryFiles(ctx, force bool)` / `syncSessionFiles(ctx, force bool)` 增加 force 参数；`Sync` 增量路径传 false。
5. `runReindex` 传 force=true。
6. `needsReindex` 补 `providerKey` 比对。
7. `Initialize` 中按上游顺序：`needsRebuildOnConfigChange() || needsFtsMigrationReindex()` → 加锁重判 → 清表 + 全量 sync。

**已知偏差 D-2**：上游用跨进程文件锁串行重建；Go 侧只有进程内 `indexCache` 与 SQLite `_busy_timeout`，不引入文件锁。理由：Go 的 `memoryIndexManager` 无跨进程共享语义（每个进程独立 `indexCache`），跨进程并发由 SQLite WAL 自身处理。回收条件：若出现多进程共享同一 `memory.db` 的部署形态。

---

## 3. E-03 未配置 embedding 时未降级为 FTS5-only

**上游**：`0fc618ad`（agent-core）——`_initialize_provider` 在无 `embedding_config` 或无 `api_key` 时置 `provider = None`、`provider_key = "none:no-embedding"`；`_should_full_reindex` 对 `provider is None` 返回 false；`search` 在 `has_vector` 为 false 时**跳过 rerank，直接用关键词分数**，阈值取更低的 `keywordMinScore`（默认 0.1）。

**Go 缺陷**：`CreateEmbeddingProvider` 缺 key 时返回 **mock provider**（确定性伪随机向量）→ `hasVector` 恒真 → 向量侧（权重 0.7）以随机相似度污染结果；且 `Search` 无 `!hasVector` 分支。mock 向量还会写入 `chunks.embedding` 与 `embedding_cache`，污染持久化数据。

**修复（方案 A，全量对齐上游）**：
1. `CreateEmbeddingProvider` 缺 key / 无 embedding_config 时返回 `(nil, nil)`；`fallback == "mock"` 保留为**显式 opt-in**（不传 fallback 不再自动 mock）。
2. `initializeProvider` 接受 nil provider，置 `providerKey = "none:no-embedding"`，记 Info 日志。
3. `needsReindex` / `runReindex` / `ensureVectorTable` / `indexChunk` / `embedQueryWithTimeout` 全面 nil-safe。
4. `Search` 增加 `!hasVector` 分支：纯关键词结果按 `Query.KeywordMinScore`（新增配置项，默认 0.1）过滤后截断返回。
5. `indexChunk` 在 provider 为 nil 时 `model` 列写 nil、`embedding` 写 nil（对齐上游 `model_name = self.provider.model if self.provider else None`）。

**已知偏差 D-3**：Go 侧 `Search` 的 minScore 缺省回退顺序与上游一致（`opts.MinScore != 0` → settings），但 Go 用值类型 `SearchOpts` 表示"未设置"，因此**显式传 0 等价于未设置**。上游用 `"min_score" in opts` 判断。此偏差为本仓既有设计，本次不改（改动会波及全部调用方），仅登记。

---

## 4. E-04 BM25 分词器缺陷

**上游**：
- agent-core `1df6d247`：`split_query_tokens` 按 3 字符阈值分流；`build_fts_query` 只用 long tokens；`_search_keyword` 增 `LIKE '%tok%' ESCAPE '\'` 兜底（固定分 0.5，按 id 去重、FTS 优先）；`search` 无向量时用 `keywordMinScore`；FTS 表迁 `tokenize='trigram'`。
- jiuwenswarm HEAD：仍为 `\w+` + `OR`，**无** LIKE 兜底（未修）。

**Go 缺陷**：两份实现都只有 FTS 一条路，`chunks_fts` 无 tokenizer，2 字中文词（"组网""日志""配置"）永远匹配不到。

**修复（方案 B：两份都上完整修复）**：
- **第一份** `agentcore/memory/lite`：
  1. 新增 `SplitQueryTokens(query) (long, short []string)`：正则 `[\p{L}\p{N}_]+` 切分，取前 10 个，按 `len(t) < 3` 分流（按 rune 计数）。
  2. `BuildFTSQuery` 改为：正则切分 + 前 10 个 + `OR` 连接（**移除 gse 分词路径**，索引侧同步改）。
  3. 索引侧 `indexChunk`：`chunks_fts` 写入改为**原文**（不再 `joinSegments(segmentForFTS(...))`）。
  4. FTS 表建表改 `tokenize='trigram'`；新增 `ftsTableIsLegacy()` 检测旧表并 DROP 重建，置 `ftsMigrated` 标记。
  5. 新增 `needsFtsMigrationReindex()`：`ftsMigrated` 且 meta 无 `ftsTrigram` 时为真（配合 E-02 的重建通路）。
  6. `searchKeyword` 增加 LIKE 兜底：`escapeLike` 转义 `%`/`_`/`\`，固定分 0.5，按 id 去重且 FTS 优先。
- **第二份** `swarm/agents/harness/common/memory`：同样实现 1/2/4/6（该包无 gse 分词，索引侧本来就是原文，无需第 3 步）。

**已知偏差 D-4**：第二份的 `split_query_tokens` / LIKE 兜底 / trigram 迁移**超出 jiuwenswarm 上游现状**（上游 HEAD 仍是 `\w+` + OR 无兜底）。理由：E 组按 agent-core 语义统一修复，且 2 字中文词搜不到是真实可用性缺陷。回收条件：jiuwenswarm 上游补上同类修复后，把偏差标注删除并逐字对齐。

**测试要求（用户指定）**：E-04 必须配**真集成测试**——起临时 workspace + 真 SQLite + `sqlite_fts5` 标签，写入含中文 2 字词/3 字词/英文的记忆文件，断言：① 2 字词能命中（LIKE 兜底生效）；② 3 字以上走 FTS 且分数高于 LIKE 命中；③ 旧 unicode61 表能被检测并重建；④ 两份实现的检索结果对同一输入等价。

---

## 5. E-05 空 summary 拖垮整批 embedding

**上游**：`1fced429`（agent-core）——`SummaryManager.add_memories` 遍历时 `if not mem_unit.summary or not mem_unit.summary.strip(): continue`。

**Go 缺陷**：`summary_manager.go:240 convertToMemoryDocs` 不过滤空文本；`SimpleMemoryIndex.AddMemories` 整批 `EmbedDocuments`；`embedding/common.go:96` `ValidateEmbedDocs` 对空串直接报错 → 一条空 summary 使整批（含正常摘要）写入失败。

**修复（方案 B）**：
1. `SummaryManager.AddMemories` 过滤循环加空判断，跳过并记 **Warn** 日志（含 user_id / scope_id / mem_id）。
2. `SimpleMemoryIndex.AddMemories` 增兜底：过滤 `strings.TrimSpace(d.Text) == ""` 的 doc，记 **Warn** 日志后继续；过滤后为空则按既有"空列表"语义返回 nil。

**已知偏差 D-5**：第 2 步（索引层兜底）上游无对应逻辑，属额外防线。回收条件：无（这是防"上层漏过滤"的显式加固，保留）。

---

## 6. E-07 LongTermMemory 强依赖 db_store

**上游**：`117b1290`（agent-core）——① `set_config` 守卫收窄为只查 `kv_store`；② db_store 缺失时改用新增的 `KvScopeUserMappingManager`（kv 后端）；③ `message_manager` 为空、`scope_user_data` 为空时补降级与日志。

**Go 缺陷**：`ltm/config_ops.go:42` 仍为 `kvStore == nil || dbStore == nil`；`config_ops.go:75-78` 无条件 `NewSqlDbStore(m.dbStore)`。其余调用点（`getHistoryMessages` / `GetRecentMessages` / `DeleteMessages` / `AddMessages`）已是 nil-safe。

**修复（方案 A，全量对齐上游）**：
1. `SetConfig` 守卫改为只查 `kvStore`。
2. 新增 `KvScopeUserMappingManager`（kv 后端，键前缀走既有 `kv_prefix_registry`），实现 `Add / DeleteByScopeID / GetByScopeID`；db_store 为空时构造它。
3. `sqlDbStore` / `messageManager` / `writeManager` 按存在性条件构造。
4. 补两条降级 Warn 日志：「消息持久化未启用」（无 messageStore）与「scope 映射走 kv 后端」（无 dbStore）。
5. `DeleteMemByScope` 在 `scope_user_data` 为空时记 Debug 并直接返回成功（对齐上游）。

---

## 7. E-08 迁移前备份不足却照删

**上游**：`adce3cda`（agent-core）——① `_create_backup` 先 `count_messages` 取 `expected`，再 `get_messages(limit=max(expected, 1000))` 一次取全（`get_messages` 无 offset，固定分页必漏最新）；② 返回 `(backup_data, expected)`；③ `_restore_from_backup` 若 `len(backup_data) < expected` 直接 raise **跳过删除**；④ 恢复后复核条数，不等则记 error。

**Go 缺陷**：`restoreFromBackup` 无条件 `DeleteMessages(nil)`，无"备份不足拒删"守卫；`createBackup` 用 `StartTime` 游标分页，同 timestamp 的多条消息有跳过风险。

**修复（方案 A）**：
1. `createBackup` 返回 `(records, expected)`：先 `CountMessages(nil)`，再 `GetMessages(filter, max(expected, 1000), "timestamp", "asc")` 一次取全。
2. `restoreFromBackup(ctx, records, expected, version)` 入口加守卫：`len(records) < expected` → 返回 `StatusMemoryMigrateMemoryExecutionError` 且**不删任何行**。
3. 恢复后 `CountMessages` 复核，不等则 Error 日志（含实际/期望条数）。

---

## 8. E-10 team 模式记忆初始化/生成失败

**上游**：`06def473`（agent-core）——四件事：
1. `TeamMemoryManager` 增 `_active_session_id` + `bind_session_id()`；kernel 在 bind/release session 后同步；`extract_after_round` 包 `_extract_with_session_context`（提取前临时 set session_id）。
2. 提取时机从 `finalize_round` 挪到 `pause()` / `stop()`；`stop` 里仅当"从 running 直接进"时提取。
3. `register_tools` 幂等键改为「同一 deep_agent 才跳过」，换 deep_agent 则清空 owned 名单重新注册；`close` 中删除 `ability_manager.remove` 整段。
4. `recover_from_session` 补重注 `spec.memory.embedding_config`。

**Go 缺陷**：四项全缺。另 `BuildMemoryManager` 用硬编码 `NewTeamMemoryConfig()`，**根本不读 `spec.memory`**。

**修复（方案 A：4 项一起做）**：
1. `TeamMemoryManager` 增 `activeSessionID string` + `BindSessionID(id string)`；`ExtractAfterRound` 改为 `extractWithSessionContext`（用 `sessionctx.SetSessionID` 临时设值，`defer` 还原）。
2. kernel：bind session 后 `BindSessionID(sessID)`，release 后 `BindSessionID("")`；`Pause` 与 `Stop`（仅 `lifecycleState == running` 时）在 drain 之后调 `ExtractAfterRound`；`FinalizeRound` 移除提取。
3. `RegisterTools` 幂等条件改为 `m.deepAgentForCleanup == deepAgent`；换 agent 时先清空 `ownedToolNames`；`Close` 移除 `am.RemoveMany` 调用（保留 `resource_mgr.RemoveTool`）。
4. `BuildMemoryManager` 改为读 `spec.Memory`：`!spec.Memory.Enabled → return nil`；其余字段从 `spec.Memory` 取；`RecoverFromSession` 在 `runtimeSpec != nil` 时重注 `spec.Memory.EmbeddingConfig`。

---

## 9. E-11 episodic memory 时间抽取不准

**上游**：`688b38f5` + `e343eed`（agent-core）。

**Go 缺陷**：`prompts/fragment_memory_prompt.md` 为原始版，缺三段改动。

**修复（方案 C）**：逐字复刻上游最终文案（含上游行尾多余引号，保持一字不差），仅改该文件。
**漂移核对结论**：其余三个 memory 提示词与 11 个 graph 提取提示词均与上游逐字节一致，**无其他漂移**。

---

## 10. E-12 graph-memory 关系去重按错字段

**上游**：`600b468f`（agent-core）——去重键改 `fact`，重复项 `continue` 丢弃（不再置 `content=""`）。

**Go 缺陷**：`parse_llm_response.go:240-256` 按 `content` 去重并置空；而 `Dict2Relation` 读 `response["fact"]`（:190）。LLM 返回体无 `content` 字段 → `newContent` 恒空 → 所有关系互为"包含"→ 从第二条起全被置空，但 `Dict2Relation` 不读 `content` → **去重完全空转，重复关系全部入库**。

**修复（方案 A）**：改为按 `fact` 去重，`fact` 为空则丢弃，重复项 `continue`（不再置空）。

---

## 11. E-13 成员释放时未关闭 memory index manager

**上游**：`339d64df`（agent-core）——① `close()` 的 observer join 加 5s 超时并 `to_thread`；② 新增 `aclose_memory_managers_for(agent_id, workspace)`；③ `NativeHarness.dispose` 在 `stop` 后调 `_close_memory_managers`，再幂等释放 `sys_operation`。

**Go 缺陷**：`NativeHarness.Dispose` 只调 `Stop`；既没关该成员的 memory manager，也没释放 `sys_operation`。`Stop` 是轮末拆除（有意保留 sys_operation 供下一轮复用），不能代替 `Dispose`。

**修复（方案 A，修正后范围）**：
1. 新增 `lite.CloseMemoryManagersFor(agentID string, ws *workspace.Workspace) int`：按 `agentID` + `ws.RootPath` 匹配 `indexCache` 逐条 `Close()`，返回关闭个数。
2. `NativeHarness.Dispose` 改为：`Stop(ctx)` → `CloseMemoryManagersFor(card.ID(), workspace)`（记 Info）→ 幂等释放 `sys_operation`（仅当仍注册时 `RemoveSysOperation`）。
3. `Stop` 保持不动。

---

## 12. 修复批次与依赖

| 批次 | 内容 | 依赖 |
| --- | --- | --- |
| B1 | E-02（重建通路 + fingerprint） | 无 |
| B2 | E-03（nil provider + 关键词降级） | 依赖 B1 的 nil-safe 骨架 |
| B3 | E-04（两份实现：tokens/LIKE/trigram） | 依赖 B1 的 `needsFtsMigrationReindex` |
| B4 | E-05（空 summary 过滤 + 索引层兜底） | 无 |
| B5 | E-01（时区归一） | 无 |
| B6 | E-08（备份守卫） | 无 |
| B7 | E-07（db_store 可选 + kv 映射） | 无 |
| B8 | E-12（fact 去重） | 无 |
| B9 | E-11（提示词） | 无 |
| B10 | E-10（team 记忆四项） | 无 |
| B11 | E-13（Dispose 两处释放） | 依赖 B1 的 `Close` 语义 |

---

## 13. 已知偏差汇总

| 编号 | 偏差 | 理由 | 回收条件 |
| --- | --- | --- | --- |
| D-1 | 历史消息行保留 RFC3339，不做迁移 | 历史 `message_id` 由旧格式算出，不可回溯改写 | 上游引入时间戳归一化迁移器时对齐 |
| D-2 | 重建不做跨进程文件锁 | Go 无跨进程共享 `indexCache` 语义，SQLite WAL 已处理并发 | 出现多进程共享同一 `memory.db` 的部署 |
| D-3 | `SearchOpts` 值类型，显式 0 等价未设置 | 本仓既有设计，改动波及全部调用方 | 上游改用显式 presence 判断且本仓跟进 |
| D-4 | 第二份实现超出 jiuwenswarm 上游（加 LIKE/trigram/tokens 分流） | E 组按 agent-core 语义统一修复；2 字中文词搜不到是真实缺陷 | jiuwenswarm 上游补上同类修复 |
| D-5 | 索引层空文本兜底为额外防线 | 防上层漏过滤 | 无（显式加固，保留） |

---

## 14. 测试要求

- 每条修复补单元测试，所在包覆盖率 ≥ 85%。
- **E-04 必须配真集成测试**（跨 `agentcore/memory/lite` + `foundation/store/index` + `sqlite` 至少 3 个子系统），使用 `sqlite_fts5` + `test` 构建标签，覆盖：2 字词 LIKE 命中、3 字以上走 FTS、旧 unicode61 表检测重建、两份实现结果等价。
- E-01 / E-02 / E-08 补集成用例（真 SQLite / 真文件系统），验证排序、重建、备份守卫的端到端行为。
- 本地门槛：`go build ./...`、`go vet ./...`、`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 全通过后推送。
