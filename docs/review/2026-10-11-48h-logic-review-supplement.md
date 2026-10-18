# 48h 逻辑审查 — 2026-10-11（深度补充）

> 审查范围：最近完成的关键实现章节，对照 Python 参考项目深度逐函数对比
> - **9.85 TeamRunner**（runtime/dispatch + team_runner + manager Activate 内联回填）
> - **9.85 runtime/dispatch**（RunActionKind + DecideRunAction 纯调度决策）
> - **10.3.8 TeamHelpers**（team_helpers.go + deep_adapter_team.go 流处理 + evolution 推送）
> - **10.6.12 SessionOps**（7 个服务函数完整对比）
> - **13.1-13.4 Indexer**（Indexer 接口 + ChromaIndexer + ComputeChunkEmbeddings + 配置类型）
>
> 审查方法：对照 Python 参考项目（jiuwenswarm + openjiuwen），逐函数对比方法签名、步骤完整性、⤵️占位代码实际状态
>
> 注意：本审查是对 2026-10-11-48h-logic-review.md 的深度补充，编号从 S-50 起始避免冲突

---

## 汇总

| 级别 | 数量 |
|------|------|
| 严重 (S) | 26 |
| 一般 (M) | 30 |
| 提示 (T) | 19 |
| **合计** | **75** |

### 各章节问题分布

| 章节 | S | M | T | 合计 |
|------|---|---|---|------|
| 9.85 TeamRunner | 8 | 10 | 7 | 25 |
| 9.85 Dispatch | 1 | 5 | 3 | 9 |
| 10.3.8 TeamHelpers | 4 | 8 | 4 | 16 |
| 10.6.12 SessionOps | 5 | 7 | 3 | 15 |
| 13.1-13.4 Indexer | 8 | 10 | 9 | 27（去重后） |

---

## 严重问题 (S-50 ~ S-75)

### S-50 [9.85 TeamRunner] _maybe_release_team_session 完全缺失

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L522-543
async def _maybe_release_team_session(self, session_id, *, force):
    release_info = await TeamRuntimeManager.resolve_team_session_release_info(session_id)
    if release_info is None:
        return False
    await self._get_team_runtime_manager().release_session(session_id, force=force)
    await CheckpointerFactory.get_checkpointer().release(session_id)
    return True
```

**Go 问题：** 无任何等价实现。Runner.release() 调用时无法区分 team session 和普通 session，team 动态表不会被释放。

**修复方案：** 在 runner 包中添加 `MaybeReleaseTeamSession` 函数，供 runner.release 调用：
```go
func MaybeReleaseTeamSession(ctx context.Context, sessionID string, force bool) (bool, error) {
    releaseInfo := GetTeamRuntimeManager().ResolveTeamSessionReleaseInfo(ctx, sessionID)
    if releaseInfo == nil {
        return false, nil
    }
    if err := GetTeamRuntimeManager().ReleaseSession(ctx, sessionID, force); err != nil {
        return false, err
    }
    if cp := session.GetCheckpointerFactory(); cp != nil {
        _ = cp.Release(ctx, sessionID)
    }
    return true, nil
}
```

---

### S-51 [9.85 TeamRunner] 4 个 facade 函数缺失 — register/pause/stop/delete

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py
# L877-892: register_human_agent_inbound
# L894-902: pause_agent_team
# L930-937: stop_agent_team
# L939-955: delete_agent_team
```

**Go 问题：** `team_runner.go` 只暴露了 `InteractAgentTeam` / `GetAgentTeamMonitor` / `ListActiveTeams`，缺少以上 4 个 facade 函数。外部调用者无法通过统一入口暂停/停止/删除团队和注册人机回调。

**修复方案：** 添加对应的导出函数，委托给 `GetTeamRuntimeManager()`：
```go
func RegisterHumanAgentInbound(ctx context.Context, teamName, sessionID, memberName string, handler func(...)) error
func PauseAgentTeam(ctx context.Context, teamName, sessionID string) error
func StopAgentTeam(ctx context.Context, teamName, sessionID string) error
func DeleteAgentTeam(ctx context.Context, teamName, sessionID string) error
```

---

### S-52 [9.85 TeamRunner] prepareBaseTeam 不支持 str team_id 解析路径

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L669-670
if isinstance(base_team, str):
    return await self._resource_manager.get_agent_team(team_id=base_team)
```

**Go 问题代码：**
```go
// internal/agent_teams/runtime/team_runner.go L545-553
func prepareBaseTeam(baseTeam any) (maschema.BaseTeam, error) {
    if team, ok := baseTeam.(maschema.BaseTeam); ok {
        return team, nil
    }
    return nil, fmt.Errorf("run_agent_team(base=True) accepts BaseTeam; got %T...")
}
```

Go 只接受 `BaseTeam` 实例，不支持 str team_id 解析。通过 team_id 字符串启动 BaseTeam 路径不可用。

**修复方案：** 添加 string 类型分支：
```go
if teamID, ok := baseTeam.(string); ok {
    return runner.GetResourceManager().GetAgentTeam(teamID)
}
```

---

### S-53 [9.85 TeamRunner] RunAgentTeam 函数签名缺少 context/envs 参数

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L151-160
async def run_agent_team(self, agent_team, inputs, *, member=False,
                         session=None, context=None, envs=None):
```

**Go 问题代码：**
```go
// internal/agent_teams/runtime/team_runner.go L65-72
func RunAgentTeam(ctx context.Context, agentTeam any, inputs any,
                  base bool, member bool, sess any) (map[string]any, error)
```

Go 缺少 `context`（ModelContext）和 `envs` 参数。这些在 Python 中传递到 Activate 和 BaseTeam 路径，影响 prompt 构建和 hide_dm/debug 等运行时配置。

**修复方案：** 在函数签名中添加 `modelCtx any` 和 `envs map[string]any` 参数（可选传 nil），或在 Functional Options 模式中添加 `WithModelContext` / `WithEnvs`。

---

### S-54 [9.85 Dispatch] DecideRunAction 使用 panic 而非 error 返回

**Python 样例：**
```python
# openjiuwen/agent_teams/runtime/dispatch.py L139-143
if pool_entry.current_session_id != target_session_id:
    raise RuntimeError(
        f"dispatch invariant violated: pool entry for {target_team_name!r} "
        f"on session {pool_entry.current_session_id!r} must be torn down "
        f"before dispatching to session {target_session_id!r}",
    )
```

**Go 问题代码：**
```go
// internal/agent_teams/runtime/dispatch.go L128-133
if poolEntry.SessionID != targetSessionID {
    panic(fmt.Sprintf(
        "dispatch invariant violated: pool entry for %q on session %q must be torn down before dispatching to session %q",
        targetTeamName, poolEntry.SessionID, targetSessionID,
    ))
}
```

Python 抛 `RuntimeError`（可被上层 catch），Go 使用 `panic` 会导致整个进程崩溃（除非有 `recover`）。纯调度函数不应有 panic 副作用。

**修复方案：** 将 `DecideRunAction` 签名改为返回 `(RunAction, error)`，契约违反时返回 error。调用方（`Activate`）已有 error 处理路径：
```go
func DecideRunAction(...) (RunAction, error) {
    // ...
    if poolEntry.SessionID != targetSessionID {
        return RunAction{}, fmt.Errorf("dispatch invariant violated: ...")
    }
}
```

---

### S-55 [10.3.8 TeamHelpers] processTeamMessageStream 缺失 wait_for_pending_shutdown_cleanup_for_session 前置等待

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L626-635
await wait_for_pending_shutdown_cleanup_for_session(session_id)
```

**Go 问题代码：**
```go
// internal/swarm/server/runtime/agent_adapter/deep_adapter_team.go L216-222
// 直接判断 isFirstRequest，无前置等待
```

此问题在多轮审查中反复标记但未修复。如果上一次 shutdown 的清理还在进行，新请求可能读到不一致的 team 状态。

**修复方案：** 在 `teamManager := team.GetTeamManager(channelID)` 后、`isFirstRequest` 判断前，添加 `WaitForPendingShutdownCleanupForSession` 调用（15秒超时）。

---

### S-56 [10.3.8 TeamHelpers] HandleTeamSlashCommand 的 /evolve 分支缺失字符串判断

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L597-601
if isinstance(evolve_result, str) and evolve_result.strip():
    return {
        "output": f"Skill '{skill_name}' 演进请求已提交，请等待审批。",
        "result_type": "answer",
    }
```

**Go 问题代码：**
```go
// internal/swarm/server/runtime/agent_adapter/team_helpers.go L320-336
// 直接进入 approvalResultFromEventOrItems，无字符串分支
```

Python 中 `evolve_result` 可以是 `str` 或对象。Go 的 `RequestUserEvolution` 返回 `*EvolutionRequestResult`，如果 `RequestID` 非空且 `ApprovalEvent` 为 nil，应返回"演进请求已提交"而非走 `approvalResultFromEventOrItems`（该路径 items=hardcoded nil，会误走 `noChangesOutput` 分支返回"未生成新的团队技能演进经验"，语义错误）。

**修复方案：** 在 `approvalResultFromEventOrItems` 调用前，添加判断：
```go
if evolveResult.RequestID != "" && evolveResult.ApprovalEvent == nil {
    return map[string]any{
        "output":      fmt.Sprintf("Skill '%s' 演进请求已提交，请等待审批。", skillName),
        "result_type": "answer",
    }
}
```

---

### S-57 [10.3.8 TeamHelpers] HandleTeamEvolveListCommand 缺少 Used / Effect 列

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L466-484
"| # | Score | Used | Effect | Section | Content (preview) |",
used_str = f"{stats.times_used}/{stats.times_presented}" if stats.times_presented else "0/0"
effect_str = f"+{stats.times_positive}/-{stats.times_negative}"
```

**Go 问题代码：**
```go
// internal/swarm/server/runtime/agent_adapter/team_helpers.go L185-198
// 表头缺少 Used 和 Effect 列，每行也缺少对应数据
```

**修复方案：** 在表头加 `Used | Effect`，遍历时从 `record.UsageStats` 提取 `TimesUsed/TimesPresented/TimesPositive/TimesNegative`。

---

### S-58 [10.3.8 TeamHelpers] approvalResultFromEventOrItems 的 items 参数硬编码 nil，丢失 records

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L605
items=list(getattr(evolve_result, "records", []) or []),
```

**Go 问题代码：**
```go
// internal/swarm/server/runtime/agent_adapter/team_helpers.go L333
nil, // Python: items=list(getattr(evolve_result, "records", []) or [])
```

即使 `EvolutionRequestResult.Records` 非空，也会误走 `noChangesOutput` 分支。结合 S-56，这里应该传入 `evolveResult.Records` 转为 `[]map[string]any`。

**修复方案：** 将 `nil` 改为转换后的 records：
```go
items := evolutionOutputSchemasToMaps(evolveResult.Records)
result := approvalResultFromEventOrItems(evolveResult.ApprovalEvent, items)
```

---

### S-59 [10.6.12 SessionOps] RewindSessionContext: content 列表处理逻辑与 Python 不一致

**Python 样例：**
```python
# jiuwenswarm/agents/harness/common/session_ops_service.py L423-427
if isinstance(content, list):
    content = " ".join(
        str(p) for p in content
        if isinstance(p, str) or (isinstance(p, dict) and p.get("type") == "text")
    )
content = str(content)
```

Python 对列表元素做两重过滤后对**每个**保留元素调 `str(p)`，dict 类型也会被 `str()` 转为字典字符串。

**Go 问题代码：**
```go
// internal/swarm/agents/harness/common/sessionops/session_ops.go L553-566
// 对 dict 元素提取了 text 字段值，而非 str(p) 整体转换
```

Go 版本行为更合理但与 Python 不一致。如果对齐 Python 严格行为，应改为 `fmt.Sprintf("%v", p)`。

**修复方案：** 建议保留当前 Go 实现（更合理），但添加注释说明与 Python 的差异。

---

### S-60 [10.6.12 SessionOps] RewindSessionContext: history_records 类型检查缺失

**Python 样例：**
```python
# session_ops_service.py L414
if not isinstance(history_records, list) or not history_records:
    logger.info("rewind_session_context: empty history for %s", session_id)
    return True
```

**Go 问题代码：**
```go
// session_ops.go L536
if len(historyRecords) == 0 {
    return true
}
```

Go 未检查 `historyRecords` 是否为 list（JSON 解析后可能是 `map` 或其他类型），如果 `history.json` 内容不是数组，语义不对。

**修复方案：** 添加类型检查：
```go
records, ok := historyRecords.([]any)
if !ok || len(records) == 0 {
    logger.Info(logComponent).Str("session_id", sessionID).Msg("rewind_session_context: empty or invalid history")
    return true
}
```

---

### S-61 [10.6.12 SessionOps] fork_session: original_id 取值 nil vs "" 差异

**Python 样例：**
```python
# session_ops_service.py L84-88
record["forked_from"] = {
    "session_id": source_session_id,
    "original_id": record.get("id", ""),  # None → ""
}
```

**Go 问题代码：**
```go
// session_ops.go L198-201
record["forked_from"] = map[string]any{
    "session_id":  params.SourceSessionID,
    "original_id": record["id"],  // key 不存在 → nil → JSON "null"
}
```

Go 的 map 取值在 key 不存在时返回 nil，写入 JSON 为 `null`，而 Python 写入 `""`。

**修复方案：**
```go
origID, _ := record["id"].(string)
record["forked_from"] = map[string]any{
    "session_id":  params.SourceSessionID,
    "original_id": origID,
}
```

---

### S-62 [10.6.12 SessionOps] CopySessionState: SaveState 签名差异需确认

**Python 样例：**
```python
# session_ops_service.py L487
deep_agent.save_state(session)  # 只接收一个参数
```

**Go 问题代码：**
```go
// session_ops.go L643
deepAgent.SaveState(sess, deepAgent.LoadState(sess))  # 接收两个参数
```

Go 的 `SaveState` 接收 `(session, state)` 两个参数，Python 只接收 `session`。需确认 Go 版 `SaveState` 的签名是否正确。

**修复方案：** 确认 Go 版 `SaveState` 的签名设计意图。如果是 `(session, loadedState)` 模式，当前实现正确；如果只需 `(session)` 则需修改。

---

### S-63 [13.1-13.4] ChromaIndexer 缺少 databaseName 字段和 _client 实例缓存

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/indexer/chroma_indexer.py L66
self.database_name = config.database_name
self._client = ChromaVectorStore.create_client(
    database_name=self.database_name,
    path_or_uri=chroma_path,
)
```

**Go 问题：** ChromaIndexer 结构体没有 `databaseName` 字段，也没有 `_client` 缓存。每次 `DeleteIndex/IndexExists/GetIndexInfo` 都调用 `NewChromaVectorStore()` 创建新 client，而 Python 只在构造时创建一次。

**影响：**
1. `DeleteIndex/IndexExists/GetIndexInfo` 中 `DatabaseName` 为空，可能路由到错误数据库
2. 每次操作都创建新 client 是严重的资源浪费和性能退化

**修复方案：** 在 ChromaIndexer 中增加 `databaseName string` 字段和 client 缓存：
```go
type ChromaIndexer struct {
    config          common.IndexConfig
    docIDField      string
    databaseName    string
    vectorStore     vector_store.ChromaVectorStore  // 缓存
    vectorStoreOnce sync.Once
}
```

---

### S-64 [13.1-13.4] IndexExists 语义错误 — 只检查 VectorStore 创建成功而非 Collection 存在

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/indexer/chroma_indexer.py L255-268
async def index_exists(self, index_name):
    try:
        await asyncio.to_thread(self._client.get_collection, name=index_name)
        return True
    except Exception:
        return False
```

**Go 问题代码：**
```go
// internal/agentcore/retrieval/indexing/indexer/chroma.go L370-387
func (ci *ChromaIndexer) IndexExists(ctx context.Context, indexName string) (bool, error) {
    vectorStore, err := vector_store.NewChromaVectorStore(...)
    if err != nil {
        return false, nil  // 创建 VectorStore 失败 → false
    }
    vectorStore.Close()
    return true, nil       // 创建成功 → true
}
```

Python 用 `get_collection` 判断 collection 是否存在，Go 用 `NewChromaVectorStore` 是否成功判断——但后者只代表 client 连接成功，**不代表 collection 存在**。这导致 `IndexExists` 对不存在的 collection 也可能返回 `true`。

**修复方案：** 通过 client 调用 `GetCollection` 来检查 collection 是否存在：
```go
func (ci *ChromaIndexer) IndexExists(ctx context.Context, indexName string) (bool, error) {
    // 使用缓存的 client
    _, err := ci.client.GetCollection(ctx, indexName)
    if err != nil {
        return false, nil
    }
    return true, nil
}
```

---

### S-65 [13.1-13.4] UpdateIndex 异常行为与 Python 不一致

**Python 样例：**
```python
# chroma_indexer.py L198-215
async def update_index(self, chunks, doc_id, config, embed_model=None, **kwargs):
    try:
        await self.delete_index(doc_id, config.index_name)
        return await self.build_index(chunks, config, embed_model, **kwargs)
    except Exception as e:
        logger.error(f"Failed to update index: {e}")
        return False  # 所有异常都返回 False，不向外抛
```

**Go 问题代码：**
```go
// chroma.go L274-296
func (ci *ChromaIndexer) UpdateIndex(...) (bool, error) {
    _, err := ci.DeleteIndex(ctx, docID, config.IndexName)
    if err != nil {
        return false, err   // ← 返回了 error！
    }
    return ci.BuildIndex(ctx, chunks, config, embedModel, opts...)
}
```

Python 的 `update_index` 所有异常都捕获后返回 `False` 不向外抛。Go 在 `DeleteIndex` 返回 error 时直接传给调用方。

**修复方案：** `UpdateIndex` 内部捕获所有 error，仅返回 `(false, nil)`：
```go
func (ci *ChromaIndexer) UpdateIndex(...) (bool, error) {
    _, delErr := ci.DeleteIndex(ctx, docID, config.IndexName)
    if delErr != nil {
        logger.Error(logComponent).Err(delErr).Msg("更新索引-删除旧数据失败")
        return false, nil  // 不传播 error
    }
    buildOK, buildErr := ci.BuildIndex(ctx, chunks, config, embedModel, opts...)
    if buildErr != nil {
        logger.Error(logComponent).Err(buildErr).Msg("更新索引-构建失败")
        return false, nil
    }
    return buildOK, nil
}
```

---

### S-66 [13.1-13.4] DeleteIndex 对错误返回 error，Python 不抛异常

**Python 样例：**
```python
# chroma_indexer.py L217-253
async def delete_index(self, doc_id, index_name, **kwargs):
    try:
        # ...
    except Exception as e:
        logger.error(f"Failed to delete index entries: {e}")
        return False  # 所有异常都返回 False
```

**Go 问题：** `DeleteIndex` 在 `NewChromaVectorStore` 或 `collection.Get` 失败时 `return false, err`，而 Python 所有异常都返回 `(False, None)`。

**修复方案：** `DeleteIndex` 应在所有错误路径返回 `(false, nil)`，对齐 Python 的"异常返回 False"语义。

---

### S-67 [13.1-13.4] MultimodalDocument.Content() 缓存策略与 Python 不一致

**Python 样例：**
```python
# openjiuwen/core/retrieval/common/document.py L109-137
@property
def content(self) -> list[dict[str, Any]]:
    if self._content_cache:
        return deepcopy(self._content_cache)
    # ... build content ...
    self._content_cache = deepcopy(content)
    return content  # 返回原始引用，cache 是 deepcopy
```

Python 的 `content` 每次访问都 `deepcopy` 缓存返回，且 `add_field` 时清空缓存。

**Go 问题代码：**
```go
// internal/agentcore/retrieval/common/document.go L169-206
func (d *MultimodalDocument) Content() []map[string]any {
    d.contentOnce.Do(func() {
        // ... build content ...
        d.contentCache = content
    })
    return d.contentCache  // ← 返回缓存引用，无深拷贝！
```

Go 使用 `sync.Once` 缓存，返回内部引用（无深拷贝），且 `AddField` 后再调 `Content()` 不会反映新添加的字段（`sync.Once` 只执行一次无法重置）。

**影响：**
1. 调用方修改返回值会破坏内部状态
2. `AddField` 后再调 `Content()` 不会反映新添加的字段

**修复方案：**
1. 使用 `atomic` 或条件变量方案替代 `sync.Once`，使 `AddField` 能重置缓存
2. `Content()` 返回时做深拷贝，或文档说明调用方不应修改返回值

---

### S-68 [13.1-13.4] BuildIndex 缺少 BaseError 透传逻辑

**Python 样例：**
```python
# chroma_indexer.py L126-196
try:
    # ... build logic ...
    return True
except BaseError:
    raise           # BaseError 透传，不被吞掉
except Exception as e:
    logger.error(f"Failed to build index: {e}")
    raise build_error(...) from e
```

**Go 问题：** `BuildIndex` 没有 `except BaseError: raise` 的等价逻辑。当底层已返回 `BaseError` 时，Python 直接 re-raise 保留原始错误码，而 Go 直接返回底层 error。更关键的是 Python 将非 BaseError 异常重新包装为 `RETRIEVAL_INDEXING_ADD_DOC_RUNTIME_ERROR`，Go 没有做这个包装。

**修复方案：** 在 `BuildIndex` 中增加错误判断：
```go
if errors.Is(err, baseError) || isBaseError(err) {
    return false, err  // BaseError 透传
}
return false, exception.NewBaseError(
    exception.StatusRetrievalIndexingAddDocRuntimeError,
    exception.WithErrorMsg(fmt.Sprintf("Failed to build index: %v", err)),
)
```

---

### S-69 [10.6.12 SessionOps] list_session_turns: 非 str content 处理路径缺失

**Python 样例：**
```python
# session_ops_service.py L277-284
if isinstance(content, str):
    cleaned = re.sub(r"<file-content[^>]*>.*?</file-content>", "", content, flags=re.DOTALL)
    preview = cleaned.strip()[:80]
else:
    preview = ""  # 非 str content 仍然纳入列表
```

**Go 问题代码：**
```go
// session_ops.go L406-408
content, _ := record["content"].(string)
if !isSelectableUserMessage(content) {
    continue
}
```

Go 通过类型断言获取 content，如果 content 是 list 则 `content = ""`，空字符串通过 selectable 检查。但 Go 丢失了 content 为 list 时的信息，且缺少 Python 的 `<file-content>` 标签清理逻辑。

**修复方案：** 对齐 Python，当 content 是 list 时，做提取处理生成 preview：
```go
switch v := record["content"].(type) {
case string:
    // 正常处理 + file-content 标签清理
case []any:
    // 提取 text 内容生成 preview
    preview = extractTextFromContentList(v)
}
```

---

### S-70 [10.6.12 SessionOps] RewindSessionContext: SaveState 失败未隔离

**Python 样例：**
```python
# session_ops_service.py L486-492
try:
    deep_agent.save_state(session)
except Exception as save_exc:
    logger.warning("rewind_session_context: deep_agent.save_state failed for %s: %s",
                   session_id, save_exc)
# save_state 失败不影响后续 post_run
```

**Go 问题代码：**
```go
// session_ops.go L641-648
if saved != nil {
    deepAgent.SaveState(sess, deepAgent.LoadState(sess))
    if postErr := sess.PostRun(ctx); postErr != nil {
        // ...
    }
}
```

Go 中 `SaveState` 和 `PostRun` 在 `else` 分支中顺序执行，如果 `SaveState` panic（没有 recover），`PostRun` 不会执行，导致 checkpointer 状态不一致。

**修复方案：** 为 `SaveState` 添加 recover 保护或隔离：
```go
func() {
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(logComponent).Any("panic", r).Str("session_id", sessionID).
                Msg("rewind_session_context: SaveState panic recovered")
        }
    }()
    deepAgent.SaveState(sess, deepAgent.LoadState(sess))
}()
// PostRun 始终执行
if postErr := sess.PostRun(ctx); postErr != nil {
    // ...
}
```

---

### S-71 [9.85 TeamRunner] RunTeamMember/RunTeamMemberStreaming inputs 类型断言丢失非 map 输入

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L360
return await agent.invoke(inputs, session=team_session)
```

**Go 问题代码：**
```go
// internal/agent_teams/runtime/team_runner.go L236
inputsDict, _ := inputs.(map[string]any)
result, err := ag.Invoke(ctx, inputsDict, ...)
```

Go 做 `inputs.(map[string]any)` 类型断言，如果 inputs 不是 map 则 `inputsDict=nil`。Python 透传原始 inputs。

**修复方案：** 对齐 Python 传入原始 inputs，或在断言失败时提供降级处理。

---

### S-72 [9.85 TeamRunner] runAgentTeam 非 reject 返回 (nil, nil) 语义不清

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L193
if _is_team_reject_kind(action.kind):
    logger.warning(...)
    return None
```

**Go 问题代码：**
```go
// team_runner.go L414-415
return nil, nil
```

返回 `(nil, nil)` 意味着"成功但结果为空"，和 reject 语义不同。调用者无法区分"正常返回空结果"和"被拒绝"。

**修复方案：** 返回特殊错误或包装结果类型：
```go
return nil, fmt.Errorf("team rejected: kind=%s, reason=%s", action.Kind, action.Reason)
```

---

### S-73 [13.1-13.4] VectorStoreConfig 默认 DistanceMetric 缺失

**Python** 中 `VectorStoreConfig` 的 `distance_metric` 默认值是 `"cosine"`。

**Go 问题：** `VectorStoreConfig` 没有默认构造函数，且 `DistanceMetric` 默认为零值空字符串而非 `"cosine"`。`NewDefaultRetrievalConfig` 也没有设置 `DistanceMetric`。

**修复方案：** 为 `VectorStoreConfig` 添加默认构造函数：
```go
func NewDefaultVectorStoreConfig() *VectorStoreConfig {
    return &VectorStoreConfig{
        DistanceMetric: DistanceMetricCosine,
    }
}
```

---

### S-74 [9.85 TeamRunner] closeTeamInteractGate 使用 context.Background() 可能永久阻塞

**Go 问题代码：**
```go
// team_runner.go L577
_ = entry.InteractGate.CloseAndDrain(context.Background())
```

如果 `CloseAndDrain` 因 inflight 永远不为 0 而阻塞，goroutine 会永远挂起（`context.Background()` 永远不会被取消）。

**修复方案：** 传带超时的 ctx：
```go
drainCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()
_ = entry.InteractGate.CloseAndDrain(drainCtx)
```

---

### S-75 [9.85 TeamRunner] RunBaseTeam 流式路径 PostRun 在 goroutine 中使用原始 ctx

**Go 问题代码：**
```go
// team_runner.go L200-201
_ = teamSession.PostRun(ctx)
```

PostRun 在 goroutine 内调用，但 ctx 可能在 goroutine 启动前就被取消。PostRun 需要运行到底（持久化 checkpoint），应使用独立 context。

**修复方案：**
```go
cleanupCtx := context.WithoutCancel(ctx)
_ = teamSession.PostRun(cleanupCtx)
```

---

## 一般问题 (M-50 ~ M-79)

### M-50 [9.85 TeamRunner] buildTeamRuntimeReadyChunk 中 leaderRole 硬编码 TeamRoleLeader

**Python 样例：**
```python
# team_runner.py L248-249
leader_role = getattr(blueprint, "role", None)
```

**Go 问题：** `team_runner.go:460` — 硬编码 `leaderRole := atschema.TeamRoleLeader`，而非从 AgentCard 读取实际 role。

**修复方案：** 从 `activation.Agent` 的属性读取实际 role。

---

### M-51 [9.85 TeamRunner] RunAgentTeamStreaming goroutine 未传播 ctx 取消

**Go 问题：** `team_runner.go:479-497` — goroutine 中使用外层 ctx，如果 ctx 被取消，Finalize/PostRun 使用的 `_ =` 忽略错误可能导致清理不完整。

**修复方案：** goroutine 内检查 ctx.Done()，Finalize/PostRun 使用 `context.WithoutCancel(ctx)`。

---

### M-52 [9.85 TeamRunner] ResolveTeamAgentSpec 反序列化完整性待确认

**Go 问题：** `team_runner.go:532` — `NewTeamAgentSpecFromDict` 是否完整支持 pydantic `model_validate` 的所有字段（包括嵌套结构、默认值等）？

**修复方案：** 审查 `NewTeamAgentSpecFromDict` 实现确保字段完整。

---

### M-53 [9.85 TeamRunner] buildTeamSession 不支持 *session.Session 类型

**Go 问题：** `manager.go:988-999` — `buildTeamSession` 不支持 `*session.Session`，而 `createAgentTeamSession` 支持。Activate 路径下如果传入 `*session.Session`，`buildTeamSession` 会走 default 创建空 session，丢失 envs 信息。

**修复方案：** 在 `buildTeamSession` 中添加 `*session.Session` case。

---

### M-54 [9.85 Dispatch] 缺少 teamInDB=true && teamInSession=false && poolEntry!=nil 测试用例

**Go 问题：** `dispatch_test.go` 缺少此边界情况测试。

**修复方案：** 添加测试用例覆盖此组合。

---

### M-55 [9.85 Dispatch] teamDBState 类型差异：Python Optional[str] vs Go string

**Python：** `team_db_state: Optional[str] = None`
**Go：** `teamDBState string`

Go 用空字符串 `""` 表示"无值"。语义对齐但 Go 调用方必须保证"无值"时传空字符串。

**修复方案：** 在 `DecideRunAction` 文档注释中注明空字符串表示 None。

---

### M-56 [9.85 Dispatch] RunAction 缺少 Python frozen=True 不可变语义

**Python：** `@dataclass(frozen=True, slots=True)`
**Go：** 默认可变结构体

当前代码中没有对 `RunAction` 做二次赋值，但缺少不可变性保证。非必要修改，记录即可。

---

### M-57 [10.3.8 TeamHelpers] SyncTeamIdentityMetadata 缺失 request.params.mode 注入

**Python 样例：**
```python
# team_helpers.py L703-704
request_metadata = dict(request.metadata or {})
if isinstance(getattr(request, "params", None), dict):
    request_metadata.setdefault("mode", request.params.get("mode"))
```

**Go 问题：** `deep_adapter_team.go:302-309` — `requestMetadata` 仅从 `req.Metadata` 构建，缺少 `request.params.mode` 回填。

**修复方案：** 检查 `req.Params` 是否有 `mode` 字段，若有且 `requestMetadata` 中无 `mode` 则注入。

---

### M-58 [10.3.8 TeamHelpers] isLeaderOutput/isTeammateOutput 未处理枚举对象场景

**Python 样例：**
```python
# team_helpers.py L250-257
role_value = getattr(role, "value", role)
return str(role_value).strip().lower() == TeamRole.LEADER.value
```

**Go 问题：** `team_helpers.go:454-462` — 只处理 `role.(string)`，未处理 `role` 为枚举值对象（如 `map[string]any{"value": "leader"}`）的情况。

**修复方案：** 增加 `map[string]any` 类型断言处理 role，检查 `role["value"]` 字段。

---

### M-59 [10.3.8 TeamHelpers] consumeStreamWithQuery 缺失 TeamStreamLogger 实现

**Python 样例：**
```python
# team_helpers.py L924-931
if stream_trace_enabled:
    lg = TeamStreamLogger(file_path=str(traces_dir / f"dump-team-{session_id}.txt"))
```

**Go 问题：** `deep_adapter_team.go:626` — `_ = streamTraceEnabled // TODO: TeamStreamLogger 对齐`

**修复方案：** 实现 Go 端 `TeamStreamLogger` 并在 `RunAgentTeamStreaming` 调用时传入。

---

### M-60 [10.3.8 TeamHelpers] watchTeamEvolutionAndPushTeam 推送用日志替代，未集成实际推送

**Go 问题：** `deep_adapter_team.go:808-816` — `pushEvolutionStatus` 和 `pushEvolutionEvent` 仅记录日志，带 `TODO(#9.85)`。

**修复方案：** 等待 `gateway_push` 集成完善后替换日志为实际推送。

---

### M-61 [10.3.8 TeamHelpers] evolutionOutputSchemasToMaps 类型断言不完整

**Go 问题：** `deep_adapter_team.go:1181` — `if slice, ok := events.([]any); ok` — 但 `DrainPendingApprovalEvents` 返回的是 `[]*stream.OutputSchema`，类型不匹配。

**修复方案：** 添加 `[]*stream.OutputSchema` 类型断言分支，对每个 `OutputSchema` 调用 `chunkToMap` 转换。

---

### M-62 [10.3.8 TeamHelpers] onTeamWatcherDone 是空壳，缺少异常日志

**Python 样例：**
```python
# team_helpers.py L1086-1098
def _on_team_watcher_done(task):
    # ...
    exc = task.exception()
    if exc is not None:
        logger.warning("[TeamHelpers] evolution monitor task exception: %s", exc)
```

**Go 问题：** `team_helpers.go:679-681` — 仅打日志，不检查异常。

**修复方案：** `onTeamWatcherDone` 中添加异常日志。

---

### M-63 [10.6.12 SessionOps] fork_session: 获取 existing_titles 缺少异常捕获

**Python 样例：**
```python
# session_ops_service.py L115-123
try:
    all_sessions = get_all_sessions_metadata(limit=500, offset=0)
except Exception as exc:
    logger.debug("fork_session: failed to get existing titles: %s", exc)
```

**Go 问题：** `session_ops.go:228-233` — 忽略了 `GetAllSessionsMetadata` 的错误返回值，且没有 recover 保护。

**修复方案：** 添加错误处理 + recover 保护。

---

### M-64 [10.6.12 SessionOps] fork_session: metadata 多个字段 nil vs 默认值

**Python：** `user_id` 默认 `""`，`last_message_at`/`message_count` 默认 `0`。
**Go：** 直接取 `sourceMeta["user_id"]`，key 不存在时为 nil → JSON `null`。

**修复方案：** 添加类型断言 + 默认值：
```go
userID, _ := sourceMeta["user_id"].(string)
lastMsgAt, _ := sourceMeta["last_message_at"].(float64)
msgCount, _ := sourceMeta["message_count"].(float64)
```

---

### M-65 [10.6.12 SessionOps] fork_session: JSON HTML 转义差异

**Python：** 使用 `json.dumps(ensure_ascii=False)` 保证中文直接写入。
**Go：** `json.MarshalIndent` 会转义 `&`, `<`, `>` 为 `\uXXXX`。如果 history 中包含 HTML 标签（如 `<bash-stdout>`），Go 会将其转义。

**修复方案：** 使用 `json.NewEncoder` + `SetEscapeHTML(false)`：
```go
var buf bytes.Buffer
enc := json.NewEncoder(&buf)
enc.SetEscapeHTML(false)
enc.SetIndent("", "  ")
if err := enc.Encode(records); err == nil { ... }
```

---

### M-66 [10.6.12 SessionOps] list_session_turns: diff service 缺少 recover 保护

**Python 样例：**
```python
# session_ops_service.py L248-268
try:
    from jiuwenswarm.server.utils.diff_service import get_diff_service
    ...
except Exception as exc:
    logger.debug("list_session_turns: diff service unavailable: %s", exc)
```

**Go 问题：** `session_ops.go:371-393` — 没有对 diff service 的异常做 recover 保护。

**修复方案：** 用 `defer recover()` 包裹 diff service 调用。

---

### M-67 [13.1-13.4] MultiKBRetrievalResult 类型缺失

**Python 样例：**
```python
# openjiuwen/core/retrieval/common/retrieval_result.py L33-42
class MultiKBRetrievalResult(BaseModel):
    text: str
    score: float
    raw_score: float
    raw_score_scaled: float
    kb_ids: list
    metadata: Dict[str, Any]
```

**Go：** 没有 `MultiKBRetrievalResult` 类型。

**修复方案：** 补充 `MultiKBRetrievalResult` 结构体。

---

### M-68 [13.1-13.4] Result Ranking 类型全部缺失

**Python：** `result_ranking.py` 导出了 `BaseRankConfig`, `WeightedRankConfig`, `RRFRankConfig`, `register_result_ranker_cls`。
**Go：** 没有任何对应的 Result Ranking 类型。

**修复方案：** 确认是否在 13.1-13.4 范围内。如果属于后续章节，记录为待回填。

---

### M-69 [13.1-13.4] MilvusIndexer 完全缺失

**Python：** `indexer/milvus_indexer.py` 有完整的 `MilvusIndexer` 实现（297行），包括 `_ensure_collection`、flush 等逻辑。
**Go：** 没有 `MilvusIndexer`（对应实现计划 13.3，状态 ☐）。

**修复方案：** 记录为待实现。

---

### M-70 [13.1-13.4] Indexer 接口 embedModel 参数可 nil 性标注

**Python：** `embed_model: Optional[Embedding] = None`
**Go：** `embedModel embedding.BaseEmbedding`（接口类型，已可以为 nil，但注释未说明）

**修复方案：** 添加注释说明 embedModel 可以为 nil（bm25 场景）。

---

### M-71 [13.1-13.4] RetrievalConfig.Validate() 空实现

**Go 问题：** `config.go:212-214` — `func (c *RetrievalConfig) Validate() error { return nil }`

`TopK` 应该校验 > 0，`ScoreThreshold` 如果非 nil 应校验范围。

**修复方案：** 补充 `TopK > 0` 和 `ScoreThreshold` 范围校验。

---

### M-72 [13.1-13.4] Python doc_index_callback 是类（type），Go 是实例

**Python：** `doc_index_callback: type[BaseCallback] = TqdmCallback` — 传的是类，后续在 `compute_chunk_embeddings` 中实例化。
**Go：** 直接存储回调接口实例。

Go 编译期已保证类型安全，差异可接受。记录即可。

---

### M-73 [13.1-13.4] ChromaIndexer 缺少 Close() 方法和 Client() 访问器

**Python 样例：**
```python
# chroma_indexer.py L302-311
def close(self) -> None:
    if self._client is not None:
        pass  # ChromaDB doesn't have close method

@property
def client(self) -> chromadb.PersistentClient:
    return self._client
```

**Go：** 没有 `Close()` 方法和 `Client()` getter。

**修复方案：** 添加 `Close()` 方法（保留接口）和 `Client()` getter。

---

### M-74 [13.1-13.4] BuildIndex 中 database_name 类型断言静默丢弃

**Go 问题：** `chroma.go:157` — `databaseName, _ := indexOpts.Extra["database_name"].(string)` — 忽略 ok 值，如果传入非 string 类型不会报错。

**修复方案：** 如果 `ok` 为 false 且值非 nil，应记录警告日志。

---

### M-75 [13.1-13.4] Go TextChunk add_field 不支持链式调用

**Python：** `doc.add_field("text", "x").add_field("image", ...)` 支持链式调用。
**Go：** `AddField` 返回 `(*MultimodalDocument, error)`，破坏链式调用。Go 惯用法差异，可接受。

---

### M-76 [13.1-13.4] loadFromFile 缺少 MIME 类型修正逻辑

**Python 样例：**
```python
# document.py L302-303
if not mime_type.startswith(kind):
    mime_type = "/".join([kind] + mime_type.split("/")[1:])
```

**Go：** `document.go:389-395` 没有这个 MIME 类型修正。例如 `.tif` 文件可能被识别为 `image/tiff`，Python 会修正为与 kind 一致。

**修复方案：** 补充 MIME 修正逻辑。

---

### M-77 [10.3.8 TeamHelpers] consumeStreamWithQuery 缺失 ctx 取消处理

**Python 样例：**
```python
# team_helpers.py L1021-1027
except asyncio.CancelledError:
    logger.info("stream cancelled: ...")
    raise
```

**Go 问题：** `deep_adapter_team.go:631` — `RunAgentTeamStreaming` 错误后直接 return，无 ctx 取消的区分日志。

**修复方案：** 在 `for chunk := range streamCh` 前检查 `ctx.Err()`，或用 select 监听 `ctx.Done()`。

---

### M-78 [9.85 TeamRunner] GetAgentTeamMonitor 返回 error 但 Python 返回 Optional

**Python：** `team_runner.py:484-488` — 返回 `Optional[TeamMonitor]`，无 error。
**Go：** `team_runner.go:319-321` — 返回 `(*monitor.TeamMonitor, error)`。Go 惯用模式，不算 bug。

---

### M-79 [9.85 TeamRunner] _root_task_group_scope 缺失

**Python：** 所有 `run_*` 方法都使用 `with self._root_task_group_scope():` 管理 anyio task group 生命周期。
**Go：** 完全没有等价实现。Python 中子任务异常会取消所有同级任务，Go 中子 goroutine 异常不会影响主流程。

**修复方案：** Go/Python 并发模型差异，需在架构层考虑。非 bug 但需记录。

---

## 提示问题 (T-50 ~ T-68)

### T-50 [9.85 TeamRunner] FinalizeMember 使用 defer recover 替代 Python 的 try/except

Go 用 `recover` 捕获 panic，Python 用 `try/except Exception` 捕获所有异常。行为等价但实现方式不同。✅

---

### T-51 [9.85 TeamRunner] InteractAgentTeam 返回 (*DeliverResult, error) 双模式错误

Python 的 interact 不抛异常，所有错误通过 DeliverResult.failure 传递。Go 某些路径可能返回 `(nil, error)` 而非 `(failure, nil)`。

**修复方案：** 确保 Interact 内部所有错误路径返回 `DeliverResult.failure` 而非 Go error。

---

### T-52 [9.85 Dispatch] IsTeamRejectKind 导出但 Python 中是模块级私有

Python 中 `_is_team_reject_kind` 是下划线前缀私有。Go 因跨文件引用必须导出，合理。

---

### T-53 [9.85 Dispatch] RunActionKind 枚举方式差异

Python 用 `str, Enum` 双继承，Go 用 `type string` + `const`。Go 惯用法，可接受。

---

### T-54 [9.85 Dispatch] 注释引用 Python 路径正确确认

`dispatch.go` 文件头引用 `openjiuwen/agent_teams/runtime/dispatch.py` 路径正确。✅

---

### T-55 [10.3.8 TeamHelpers] resolveEvolutionEventTimeoutSec 是空壳

**Go：** `deep_adapter_team.go:1209-1217` — 直接返回 fallback 60s，未尝试从 rail 配置中读取。

**修复方案：** 从 rail 的配置中读取 `evolution_total_timeout_secs`。

---

### T-56 [10.3.8 TeamHelpers] HandleTeamEvolveListCommand/HandleTeamSlashCommand 是导出函数

Python 中这些是模块级私有函数（下划线前缀）。Go 端需要跨文件调用，导出合理。

---

### T-57 [10.3.8 TeamHelpers] EnsureEventBroadcastInjected 是 Go 端额外添加的全局回调注入

Python 的事件广播直接通过模块级 `_broadcast_event` + `_pending_waiters` 实现。Go 端为了解决 TeamManager→adapter 的跨包调用引入了 `SetOnEventBroadcast` 回调注入，设计合理。

---

### T-58 [10.6.12 SessionOps] _derive_first_prompt 函数未实现

Python 有 `_derive_first_prompt` 函数，但 `fork_session` 不使用它。Go 的省略正确。

---

### T-59 [10.6.12 SessionOps] RewindSessionContext: context_id 显式传值等价默认

Go 显式传 `"default_context_id"`，Python 未传使用默认值。等价，正确。✅

---

### T-60 [13.1-13.4] Python EmbeddingConfig 循环依赖导致 Go 无法重导出

Go 中 `EmbeddingConfig` 没有重导出是因为循环依赖。Python 不受此限制。注释已说明原因。

---

### T-61 [13.1-13.4] Python callbacks.py 中 TqdmCallback 接受 use_rich 和 desc 参数

Go 的 `LoggingDocIndexCallback` 没有这些参数。Go 使用日志代替 tqdm 进度条，语义等价但接口不同。

---

### T-62 [13.1-13.4] Python Embedding 基类有 limiter，Go 基类没有

Python `BaseEmbedding` 有 `asyncio.Semaphore`，Go 的具体实现（如 `APIEmbedding`）内部有 `limiter chan struct{}`。接口层无影响。

---

### T-63 [13.1-13.4] Go 的 ChromaIndexer 方法签名全部增加 ctx context.Context

Python 是 `async def`，Go 是 `func (ctx context.Context, ...)`。Go 惯用法，正确。✅

---

### T-64 [13.1-13.4] Python Embedding.dimension 是 @property，Go 是方法

Go 额外提供了 `DimensionWithContext(ctx)` 方法，这是改进。✅

---

### T-65 [13.1-13.4] BuildIndex 查重逻辑 — Go map 无序 vs Python sorted set

功能上无差异（去重结果一样），Python 先排序仅影响日志中 duplicate_doc_ids 的顺序。

---

### T-66 [13.1-13.4] GetIndexInfo 中 metadata 获取方式与 Python SDK 差异

Python 直接访问 `collection.metadata` dict，Go 需通过 `Metadata()` -> `Keys()` -> `GetRaw()` 遍历。取决于 Go 的 chroma-go SDK API，实现合理。

---

### T-67 [9.85 TeamRunner] 缺少 _TeamRunnerClassMixin facade 模式说明

Python 的 `_TeamRunnerClassMixin` 是类方法 facade，Go 使用包级函数。语言差异，不算问题。

---

### T-68 [9.85 TeamRunner] 日志字段对齐确认

Go 的结构化日志格式已正确对齐 Python 的字段。✅

---

## ⤵️ 占位代码状态追踪

| 标记 | 位置 | 当前状态 | 优先级 |
|------|------|---------|--------|
| ⤵️ WaitForPendingShutdownCleanup | deep_adapter_team.go:216 | 未实现 | P0 — S-55 |
| ⤵️ TeamStreamLogger | deep_adapter_team.go:626 | 未实现 | P1 — M-59 |
| ⤵️ WebSocketGatewayPushTransport | deep_adapter_team.go:808 | 仅日志替代 | P1 — M-60 |
| ⤵️ MaybeReleaseTeamSession | team_runner.go | 完全缺失 | P0 — S-50 |
| ⤵️ register/pause/stop/delete facade | team_runner.go | 完全缺失 | P0 — S-51 |
| ⤵️ MilvusIndexer | retrieval/indexing/ | 完全缺失（13.3 ☐） | P1 — M-69 |

---

## 修复优先级建议

### P0 必须修复（影响核心流程 / 数据丢失）

| 编号 | 描述 | 章节 |
|------|------|------|
| S-50 | _maybe_release_team_session 缺失 | 9.85 TeamRunner |
| S-51 | 4 个 facade 函数缺失 | 9.85 TeamRunner |
| S-54 | DecideRunAction 用 panic 而非 error | 9.85 Dispatch |
| S-55 | processTeamMessageStream 缺前置等待 | 10.3.8 TeamHelpers |
| S-56 | /evolve 分支缺失字符串判断 | 10.3.8 TeamHelpers |
| S-58 | approvalResultFromEventOrItems items 硬编码 nil | 10.3.8 TeamHelpers |
| S-63 | ChromaIndexer 缺 client/databaseName 缓存 | 13.1-13.4 |
| S-64 | IndexExists 语义错误 | 13.1-13.4 |
| S-67 | Content() 缓存策略错误 | 13.1-13.4 |
| S-70 | SaveState 失败未隔离 | 10.6.12 SessionOps |

### P1 建议修复（语义偏差 / 数据不一致）

| 编号 | 描述 | 章节 |
|------|------|------|
| S-52 | prepareBaseTeam 不支持 str | 9.85 TeamRunner |
| S-53 | RunAgentTeam 缺 context/envs | 9.85 TeamRunner |
| S-57 | HandleTeamEvolveListCommand 缺 Used/Effect | 10.3.8 |
| S-59 | content 列表处理不一致 | 10.6.12 SessionOps |
| S-60 | history_records 类型检查缺失 | 10.6.12 SessionOps |
| S-61 | original_id nil vs "" | 10.6.12 SessionOps |
| S-65 | UpdateIndex 异常行为不一致 | 13.1-13.4 |
| S-66 | DeleteIndex 异常行为不一致 | 13.1-13.4 |
| S-68 | BuildIndex 缺 BaseError 透传 | 13.1-13.4 |
| S-73 | DistanceMetric 默认值缺失 | 13.1-13.4 |
| S-74 | closeTeamInteractGate 可能永久阻塞 | 9.85 TeamRunner |
| S-75 | PostRun 使用已取消的 ctx | 9.85 TeamRunner |

### P2 可延后（测试覆盖 / 类型安全 / 日志）

| 编号 | 描述 |
|------|------|
| M-50 ~ M-79 | 各一般问题 |
| T-50 ~ T-68 | 各提示问题 |
