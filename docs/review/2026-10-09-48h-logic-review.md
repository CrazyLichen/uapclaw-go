# 48h 逻辑审查 — 2026-10-09

> 审查范围：48小时内提交记录涉及的实现章节 — 9.85 TeamRunner、10.3.8 TeamHelpers、10.6.12 SessionOps、13.2 ChromaIndexer、13.4 ComputeChunkEmbeddings
>
> 审查方法：对照 Python 参考项目，逐函数对比方法签名、步骤完整性、⤵️占位代码实际状态

---

## 汇总

| 级别 | 数量 |
|------|------|
| 严重 (S) | 14 |
| 一般 (M) | 23 |
| 提示 (T) | 9 |

---

## 严重问题 (S-01 ~ S-14)

### S-01 TeamRunner: runAgentTeam/runAgentTeamStreaming 中 inputs 传 nil，用户 query 丢失

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L194
return await activation.agent.invoke(inputs, session=activation.session)
# L260
async for chunk in activation.agent.stream(inputs, session=activation.session):
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L417
result, err := activation.Agent.Invoke(ctx, nil, agentinterfaces.WithSession(activation.Session))
// L470
agentCh, streamErr := activation.Agent.Stream(ctx, nil, agentinterfaces.WithSession(activation.Session))
```

`Invoke` 和 `Stream` 第二参数传 `nil` 而非 `inputs`。Python 传的是包含用户 query 的 `inputs` dict。这意味着首次请求的用户输入数据在 TeamAgent 路径中丢失。

**修复方案：** 将 `inputs` 参数传递下去：
```go
result, err := activation.Agent.Invoke(ctx, inputs, agentinterfaces.WithSession(activation.Session))
agentCh, streamErr := activation.Agent.Stream(ctx, inputs, agentinterfaces.WithSession(activation.Session))
```
注意 `RunAgentTeam` 函数签名已有 `inputs any` 参数，只需在 `runAgentTeam` 内部将其转为 `map[string]any` 后传递。

---

### S-02 TeamHelpers: 缺失 wait_for_pending_shutdown_cleanup_for_session

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L624-635
try:
    from jiuwenswarm.agents.harness.team.remote_member_bootstrap import (
        wait_for_pending_shutdown_cleanup_for_session,
    )
    await wait_for_pending_shutdown_cleanup_for_session(session_id)
except Exception as exc:
    logger.warning(
        "[TeamHelpers] waiting for pending shutdown cleanup failed: session_id=%s error=%s",
        session_id, exc,
    )
```

**Go 问题：** `processTeamMessageStream`（deep_adapter_team.go）中没有等价调用。如果上一次 shutdown 清理未完成就发起新的 team 消息，可能导致状态不一致。

**修复方案：** 在 `processTeamMessageStream` 的 teamManager 获取后、isFirstRequest 判断前，添加对 `WaitForPendingShutdownCleanup` 的调用（需要 TeamManager 暴露此方法或同步等待清理完成的机制）。当前标记为 `⤵️(#9.72)`。

---

### S-03 TeamHelpers: /evolve 命令缺失 isinstance(evolve_result, str) 分支

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L597-601
if isinstance(evolve_result, str) and evolve_result.strip():
    return {
        "output": f"Skill '{skill_name}' 演进请求已提交，请等待审批。",
        "result_type": "answer",
    }
```

**Go 问题：**
```go
// internal/swarm/server/adapter/team_helpers.go L606-622
evolveResult, err := rail.RequestUserEvolution(ctx, skillName, userQuery, false)
// ... 只走 approvalResultFromEventOrItems 分支
```

Go 端 `RequestUserEvolution` 返回 `(*EvolutionRequestResult, error)`，不会返回 string 类型。但 Python 中 `request_user_evolution` 在某些路径下可能返回 string（如 auto_approve 场景或 rail 代理层做了类型转换）。Go 端完全缺失此分支，当 `EvolutionRequestResult` 中某些字段表示"已提交待审批"语义时，用户看到的反馈与 Python 不同。

**修复方案：** 在 `HandleTeamSlashCommand` 的 /evolve 分支中，增加对 `evolveResult` 状态的判断——如果 `evolveResult.ApprovalEvent == nil && len(evolveResult.Records) == 0`，返回"演进请求已提交，请等待审批"消息。

---

### S-04 TeamHelpers: evolution status/event 推送完全未实现

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L1109-1113
push_context = EvolutionPushContext(
    transport=WebSocketGatewayPushTransport(),
    channel_id=channel_id,
    session_id=session_id,
)
# L1142-1155
await push_evolution_status(push_context, build_evolution_status_update(...), build_server_push_message)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L816
// TODO(#9.85): 待 gateway_push 集成完善后，通过 transport.SendPush 推送到前端
logger.Info(logComponent)...Msg("pushEvolutionStatus: [TODO] 推送 evolution status")
```

所有 `pushEvolutionStatus` 和 `pushEvolutionEvent` 调用仅记录日志，前端无法收到 evolution 审批/进度事件。Team skill evolution 功能对用户完全不可见。

**修复方案：** 实现 `EvolutionPushContext`，集成 `WebSocketGatewayPushTransport`，在 `pushEvolutionStatus`/`pushEvolutionEvent` 中调用 `transport.SendPush` 发送到前端。

---

### S-05 Extra() 并发安全不完整 — 14+ 处直接绕过 mutex 写入

**Python 样例：** Python 使用 asyncio 单线程事件循环，不存在并发 map 读写问题。

**Go 问题：**
```go
// internal/agentcore/single_agent/interfaces/callback.go L422
func (c *AgentCallbackContext) Extra() map[string]any { return c.extra }
```

`Extra()` 直接返回内部 map 引用，14+ 处代码通过 `cbc.Extra()[key] = value` 直接写入，完全绕过了 `extraMu sync.RWMutex` 保护：
- `interrupt/handler.go:254` — `cbc.Extra()[ResumeUserInputKey] = userInput`
- `interrupt/handler.go:297` — `cbc.Extra()[ResumeStartIterationKey] = resumeIteration + 1`
- `interrupt/handler.go:282` — `delete(cbc.Extra(), ResumeUserInputKey)`
- `server/hooks/user_hook_rail.go:77-78` — 写入 `_skip_tool`/`_hook_feedback`
- `server/hooks/user_hook_rail.go:97,99,101` — 读写 `_hook_additional_context`
- `agent_teams/observability/rail.go:86,100,108` — span 写入/读取/删除
- `agent_teams/team_workspace/rail.go:125` — 写入 `workspace_lock_rejected`
- `harness/common/rails/stream_event_rail.go:327` — 写入 sidKey
- `harness/common/rails/avatar_rail.go:257` — 写入 `_skip_tool`

stream 模式下多个 rail 并发回调时仍可能触发 `concurrent map read and map write` fatal error。

**修复方案：** 将所有 `cbc.Extra()[key] = value` 改为 `cbc.SetExtra(key, value)`，`cbc.Extra()[key]` 读取改为 `cbc.GetExtra(key)`，`delete(cbc.Extra(), key)` 改为 `cbc.DeleteExtra(key)`。或者将 `Extra()` 改为返回深拷贝（但影响性能）。

---

### S-06 eventBroadcastInitialized 全局变量无并发保护

**Go 问题：**
```go
// internal/swarm/server/adapter/team_helpers.go L62-63
var (
    eventBroadcastInitialized bool
)
// L72-81
func EnsureEventBroadcastInjected(channelID string) {
    if eventBroadcastInitialized { return }
    // ... 设置
    eventBroadcastInitialized = true
}
```

多 goroutine 并发调用 `EnsureEventBroadcastInjected` 时存在 TOCTOU 竞态：检查和设置之间无原子操作，可能导致回调被注入多次。

**修复方案：** 使用 `sync.Once` 替代：
```go
var eventBroadcastOnce sync.Once
func EnsureEventBroadcastInjected(channelID string) {
    eventBroadcastOnce.Do(func() {
        tm := team.GetTeamManager(channelID)
        tm.SetOnEventBroadcast(func(chID, sessID string, event map[string]any) {
            broadcastEvent(chID, sessID, event)
        })
    })
}
```

---

### S-07 ChromaIndexer: IndexExists 使用 GetOrCreateCollection 语义矛盾

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/indexer/chroma_indexer.py
def index_exists(self, index_name: str) -> bool:
    try:
        self._client.get_collection(name=index_name)
        return True
    except Exception:
        return False
```

**Go 问题：**
```go
// internal/agentcore/retrieval/indexing/indexer/chroma.go L370-387
func (ci *ChromaIndexer) IndexExists(ctx context.Context, indexName string) (bool, error) {
    vectorStore, err := vector_store.NewChromaVectorStore(...)
    if err != nil {
        return false, nil  // 创建失败=不存在
    }
    vectorStore.Close()
    return true, nil
}
```

`NewChromaVectorStore` 内部调用 `client.GetOrCreateCollection`——如果集合不存在会**创建**它，然后返回 true。Python 用 `get_collection` 检查存在性，Go 变成了"确保存在"。

**修复方案：** 在 `IndexExists` 和 `GetIndexInfo` 中直接用 `client.GetCollection` 而非 `NewChromaVectorStore`，或在 `ChromaIndexer` 持有 client 引用后直接调用 `client.GetCollection`。

---

### S-08 ChromaVectorStore: CheckVectorField 传入空 map，校验永远失败

**Python 样例：**
```python
# openjiuwen/core/retrieval/vector_store/chroma_store.py
def check_vector_field(self) -> None:
    _check_configs_matching(
        self._construct_config,
        self.collection.configuration.get("hnsw", {})
    )
```

**Go 问题：**
```go
// internal/agentcore/retrieval/vector_store/chroma.go L218-221
func (s *ChromaVectorStore) CheckVectorField() error {
    return CheckConfigsMatching(s.constructConfig, map[string]any{})
}
```

传入空 map `map[string]any{}` 作为 actual 配置，而 Python 从 `collection.configuration.get("hnsw", {})` 获取实际的 HNSW 配置。Go 端校验总是报告不匹配，完全失去配置一致性检查的意义。

**修复方案：** 从 `s.collection` 的实际配置中获取 HNSW 参数传入：
```go
actual := getCollectionHNSWConfig(s.collection)
return CheckConfigsMatching(s.constructConfig, actual)
```

---

### S-09 ChromaVectorStore: GetOrCreateCollection 只传 space，缺失完整 HNSW 配置

**Python 样例：**
```python
# openjiuwen/core/retrieval/vector_store/chroma_store.py
self._collection = self._client.get_or_create_collection(
    name=...,
    configuration={"hnsw": self._construct_config | self._search_config}
)
```

**Go 问题：**
```go
// internal/agentcore/retrieval/vector_store/chroma.go L186
collection, err := s.client.GetOrCreateCollection(context.Background(), s.collectionName,
    chromav2.WithHNSWSpaceCreate(mapToChromaDistanceMetric(s.distanceMetric)))
```

只传了 `space`（距离度量），没有传 `max_neighbors`、`ef_construction`、`ef_search` 等 HNSW 参数。Python 传入的是 construct + search 配置合并的完整字典。

**修复方案：** 使用 `chromav2.WithHNSWHNSWConfigCreate(...)` 传入完整的 HNSW 配置，包含 `constructConfig` 和 `searchConfig` 中的所有字段。

---

### S-10 ChromaVectorStore: Search/SparseSearch/HybridSearch 不支持 QueryExpr

**Python 样例：**
```python
# openjiuwen/core/retrieval/vector_store/chroma_store.py
def search(self, query, ..., filters: Optional[dict | QueryExpr] = None):
    if isinstance(filters, QueryExpr):
        where_filter = filters.to_expr("chroma")
```

**Go 问题：** `Search`/`SparseSearch`/`HybridSearch` 的 `filters` 参数类型为 `map[string]any`，不支持 `QueryExpr`。`BuildChromaWhereFilter` 只处理简单 `key=value` 等值比较。缺失范围查询、逻辑组合、文本匹配等复杂查询能力。

**修复方案：** 定义 `QueryExpr` 接口和 `ToExpr(provider string)` 方法，在 filters 参数中支持 `QueryExpr` 类型，并在 chroma_store 中实现转换逻辑。

---

### S-11 ChromaVectorStore: CreateClient 数据库检查/创建逻辑完全缺失

**Python 样例：**
```python
# openjiuwen/core/retrieval/vector_store/chroma_store.py
@staticmethod
def create_client(database_name, path_or_uri, token=None):
    if database_name and database_name != "default_database":
        admin = AdminClient(...)
        if not admin.get_database(database_name):
            admin.create_database(database_name)
    return chromadb.PersistentClient(path=path_or_uri, database=database_name)
```

**Go 问题：** `NewChromaVectorStore` 直接创建 `chromav2.NewPersistentClient`，没有数据库检查/创建逻辑。如果用户指定非默认数据库名称，Python 会自动创建该数据库，Go 不会，导致集合查找失败。

**修复方案：** 在 `NewChromaVectorStore` 中，如果 `config.DatabaseName` 非空且非默认，先用 `AdminClient` 检查/创建数据库，再创建 PersistentClient。

---

### S-12 ChromaIndexer: 未持有 client 引用，每次操作重新创建 ChromaVectorStore

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/indexer/chroma_indexer.py
class ChromaIndexer:
    def __init__(self, ...):
        self._client = ChromaVectorStore.create_client(...)
```

**Go 问题：** `NewChromaIndexer` 只返回 `*ChromaIndexer`，不持有 ChromaDB 客户端引用。`BuildIndex`/`DeleteIndex`/`IndexExists`/`GetIndexInfo` 每次都调用 `vector_store.NewChromaVectorStore(...)` 重新创建客户端+集合。

**修复方案：** 在 `NewChromaIndexer` 中创建并持有 `client` 引用（或在首次使用时懒创建），所有方法复用该引用。

---

### S-13 常量双重定义不一致: teamEvolutionEventTimeoutSec=60s vs Python 900s

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/evolution_helpers.py
TEAM_EVOLUTION_EVENT_TIMEOUT_SEC = 900.0  # 15 min
TEAM_EVOLUTION_IDLE_SLEEP_SEC = 1.0
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L29-32
teamEvolutionIdleSleepSec = 500 // 毫秒 = 0.5s (Python 1.0s)
teamEvolutionEventTimeoutSec = 60.0    // (Python 900.0s = 15min)
```

而 `internal/swarm/server/adapter/evolution/logic/helpers.go` 中正确定义：
```go
TeamEvolutionIdleSleepSec = 1.0
TeamEvolutionEventTimeoutSec = 900.0
```

`deep_adapter_team.go` 使用了自己的错误常量值（60s vs 900s），导致 evolution watcher 在 60s 无事件后超时退出，远早于 Python 的 15 分钟。空闲轮询间隔也不一致（500ms vs 1.0s）。

**修复方案：** 删除 `deep_adapter_team.go` 中的重复常量定义，统一使用 `evolution/logic/helpers.go` 中的导出常量。

---

### S-14 getUniqueForkName 中数字解析逻辑有 bug

**Python 样例：**
```python
# jiuwenswarm/agents/harness/common/session_ops_service.py L49
num = int(m.group(1)) if m.group(1) else 1
used_numbers.add(num)
```

**Go 问题：**
```go
// internal/swarm/agents/harness/common/sessionops/session_ops.go L820-823
if num, err := fmt.Sscanf(m[1], "%d", new(int)); err == nil && num == 1 {
    var n int
    fmt.Sscanf(m[1], "%d", &n)
    usedNumbers[n] = true
}
```

`fmt.Sscanf(m[1], "%d", new(int))` 扫描结果被丢弃，然后重复调用 `fmt.Sscanf`。冗余且不直观。更严重的是，如果 `m[1]` 为空字符串（匹配 "Branch" 无数字后缀），应将 1 加入集合，但 Go 的 `if m[1] != ""` 条件分支在 else 分支也设置了 `usedNumbers[1] = true`（L826-828），这部分逻辑正确但代码结构令人困惑。

**修复方案：** 用 `strconv.Atoi` 简化：
```go
if m[1] != "" {
    if n, err := strconv.Atoi(m[1]); err == nil {
        usedNumbers[n] = true
    }
} else {
    usedNumbers[1] = true
}
```

---

## 一般问题 (M-01 ~ M-23)

### M-01 TeamRunner: 缺少 context/envs/stream_modes 参数

**Python 样例：**
```python
async def run_agent_team(self, agent_team, inputs, *, context=None, envs=None):
async def run_agent_team_streaming(self, ..., *, stream_modes=None, envs=None, stream_logger=None):
```

**Go 问题：** Go 端所有 TeamRunner 方法都省略了 `context`（ModelContext）、`envs` 和 `stream_modes` 参数。`envs` 在 `consumeStreamWithQuery` 中构建了 `streamEnvs`（hide_dm/trace key），但无法通过 `RunAgentTeamStreaming` 传递到底层 Runner。

**修复方案：** 在 `RunAgentTeam`/`RunAgentTeamStreaming` 签名中增加 `envs map[string]any` 参数，透传到 `Activate` 和底层 Runner 调用。

---

### M-02 TeamRunner: _prepare_base_team 不支持 str → ResourceMgr 解析

**Python 样例：**
```python
if isinstance(base_team, str):
    return await self._resource_manager.get_agent_team(team_id=base_team)
```

**Go 问题：** `prepareBaseTeam` 只接受 `maschema.BaseTeam` 接口类型，str 输入直接报错。

**修复方案：** 在 `prepareBaseTeam` 中添加 string 分支，通过 ResourceMgr 解析。

---

### M-03 TeamHelpers: HandleTeamEvolveListCommand 表格缺 Used/Effect 两列

**Python 样例：**
```python
"| # | Score | Used | Effect | Section | Content (preview) |",
# Used = f"{stats.times_used}/{stats.times_presented}"
# Effect = f"+{stats.times_positive}/-{stats.times_negative}"
```

**Go 问题：**
```go
"| # | Score | Section | Content (preview) |",
```

缺少 `Used`（times_used/times_presented）和 `Effect`（times_positive/times_negative）两列。

**修复方案：** 在 `HandleTeamEvolveListCommand` 中使用 `record.UsageStats` 字段构建这两列数据。

---

### M-04 TeamHelpers: HandleTeamSlashCommand /evolve 分支 items 传 nil

**Python 样例：**
```python
items=list(getattr(evolve_result, "records", []) or []),
```

**Go 问题：**
```go
nil, // Python: items=list(getattr(evolve_result, "records", []) or [])
```

Go 端直接传 nil，如果 `evolveResult.Records` 有值，Python 会将其作为 items 传入 `approvalResultFromEventOrItems`，Go 端跳过了。

**修复方案：** 将 `evolveResult.Records` 转为 `[]map[string]any` 后传入。

---

### M-05 TeamHelpers: RunAgentTeamStreaming 未传 envs 和 stream_logger

**Python 样例：**
```python
async for chunk in Runner.run_agent_team_streaming(
    agent_team=team_spec, inputs={"query": initial_query},
    session=session_id, envs=envs, stream_logger=lg,
):
```

**Go 问题：** `ateamruntime.RunAgentTeamStreaming` 不传 envs（传了 nil）和 stream_logger。

**修复方案：** 在调用 `RunAgentTeamStreaming` 时传入 `streamEnvs` 和 `streamLogger`。

---

### M-06 TeamHelpers: resolveEvolutionEventTimeoutSec 在 DeepAdapter 中是空壳

**Go 问题：** `deep_adapter_team.go` 中的 `resolveEvolutionEventTimeoutSec` 始终返回 fallback，不读取 rail 的 `evolution_total_timeout_secs` 配置。而 `evolution/logic/helpers.go` 中的 `ResolveEvolutionEventTimeoutSec` 有完整实现。两处重复且 DeepAdapter 版本有 bug。

**修复方案：** 删除 `deep_adapter_team.go` 中的重复实现，统一使用 `evolution/logic.ResolveEvolutionEventTimeoutSec`。

---

### M-07 SessionOps: ListSessionTurns/RestoreSessionFiles 缺 diff service 异常保护

**Python 样例：**
```python
try:
    from jiuwenswarm.server.utils.diff_service import get_diff_service
    diff_service = get_diff_service()
    turn_diffs = diff_service.get_turn_diffs(session_id)
except Exception as exc:
    logger.debug("list_session_turns: diff service unavailable: %s", exc)
```

**Go 问题：**
```go
ds := utils.GetDiffService()
projectDir := ds.GetProjectDirFromMetadata(sessionID)
turnDiffs := ds.GetTurnDiffs(sessionID, projectDir)
```

无任何异常保护。如果 diff service 初始化失败或返回异常，可能 panic。

**修复方案：** 在 diff service 调用处添加 `defer recover()` 或在 `GetDiffService`/`GetTurnDiffs` 中返回 error。

---

### M-08 SessionOps: CopySessionState 中 plan slug 更新条件差异

**Python 样例：**
```python
if old_plan_path.exists():
    shutil.copy2(old_plan_path, new_plan_path)
plan_mode["plan_slug"] = new_slug  # 无论 copy 是否成功都更新
modified_state["plan_mode"] = plan_mode
```

**Go 问题：**
```go
if _, err := os.Stat(oldPlanPath); err == nil {
    if err := copyFile(oldPlanPath, newPlanPath); err == nil {
        planMode["plan_slug"] = newSlug  // 仅 copy 成功才更新
    }
}
```

Python 在 `old_plan_path.exists()` 时无论 copy 是否成功都更新 slug，Go 只在 copy 成功时才更新。

**修复方案：** 将 `planMode["plan_slug"] = newSlug` 移到 `if os.Stat` 内但在 `copyFile` 外，与 Python 对齐。

---

### M-09 SessionOps: CopySessionState 中 source session 的 PostRun 缺少 defer 保证

**Python 样例：**
```python
try:
    source_session = create_agent_session(...)
    await source_session.pre_run()
    source_state_dict = source_session.get_state(_SESSION_STATE_KEY)
except Exception:
    ...
finally:
    if source_session is not None:
        await source_session.post_run()
```

**Go 问题：** Go 中 `sourceSess.PostRun(ctx)` 在多处手动调用，但如果中间步骤 panic 则不会执行。

**修复方案：** 在 `sourceSess.PreRun` 成功后添加 `defer sourceSess.PostRun(ctx)`。

---

### M-10 ChromaIndexer: 缺少 database_name 字段初始化

**Python 样例：**
```python
self.database_name = config.database_name
```

**Go 问题：** `ChromaIndexer` struct 没有 `databaseName` 字段。`DeleteIndex`/`IndexExists`/`GetIndexInfo` 创建 VectorStore 时 `DatabaseName` 为空串。

**修复方案：** 在 `ChromaIndexer` 中添加 `databaseName string` 字段，在 `NewChromaIndexer` 中从 `config.DatabaseName` 初始化。

---

### M-11 ChromaIndexer: BuildIndex 未 Close 创建的 VectorStore

**Go 问题：** `BuildIndex` 和 `UpdateIndex` 创建了 `vectorStore` 但没有 `defer vectorStore.Close()`。`DeleteIndex` 和 `GetIndexInfo` 有 `defer vectorStore.Close()`，不一致。

**修复方案：** 在 `BuildIndex` 和 `UpdateIndex` 中添加 `defer vectorStore.Close()`。

---

### M-12 ChromaVectorStore: addBatch 缺少 metadata/sparse_vector 解析失败警告日志

**Python 样例：**
```python
logger.warning(f"Failed to load metadata: {raw_metadata}")
```

**Go 问题：** metadata string 解析失败和 sparse_vector 字段 JSON 序列化失败时静默忽略，无警告日志。

**修复方案：** 添加 `logger.Warn` 记录解析失败。

---

### M-13 ChromaVectorStore: chromaResultToSearchResults 缺少 sparse_vector 反序列化

**Python 样例：**
```python
if self.sparse_vector_field in metadata:
    sparse_vec = json.loads(metadata[self.sparse_vector_field])
```

**Go 问题：** 没有对 sparse_vector 的 JSON 反序列化逻辑。稀疏向量在 metadata 中仍为 JSON 字符串。

**修复方案：** 在 `chromaResultToSearchResults` 中添加 sparse_vector 字段的 JSON 反序列化。

---

### M-14 ChromaVectorStore: Delete 不支持 string filter_expr

**Python 样例：**
```python
def delete(self, ids=None, filter_expr=None):
    if isinstance(filter_expr, str):
        logger.warning("ChromaDB does not support string filter expressions.")
        return False
```

**Go 问题：** `filterExpr` 参数类型为 `map[string]any`，不接受 `string` 类型。

**修复方案：** 将 `filterExpr` 改为 `any` 类型，在内部检查是否为 string 并记录警告。

---

### M-15 CheckConfigsMatching: casefold vs ToLower 差异

**Python 样例：**
```python
val_str = str(val).casefold()  # 更激进的 Unicode 大小写归一化
```

**Go 问题：**
```go
strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", val)))
```

`ToLower` 不处理特殊 Unicode casefold（如德语 ß → ss）。对 ASCII 配置键值无影响，但 Unicode 值可能不匹配。

**修复方案：** 当前场景下配置值均为 ASCII，风险较低。如需对齐，可引入 `unicode.SimpleFold` 或第三方 casefold 库。

---

### M-16 CheckConfigsMatching: nil 值 str() vs Sprintf("%v") 差异

**Python 样例：**
```python
str(None) → "none"
```

**Go 问题：**
```go
fmt.Sprintf("%v", nil) → "<nil>"
```

如果配置值有 nil，Python `"none"` vs Go `"<nil>"` 导致比较不匹配。

**修复方案：** 在 `normalizeConfigValue` 中对 nil 做特殊处理，返回 `"none"` 与 Python 对齐。

---

### M-17 ComputeChunkEmbeddings: 空 chunks 提前返回与 Python 行为不同

**Python 样例：** 空 chunks 时仍调用 `embed_documents([])` 然后返回空 embeddings。

**Go 问题：**
```go
if len(chunks) == 0 { return nil }
```

Go 提前返回不调用 embed_model，Python 会调用一次。功能等价但行为微小不同（Python 可能触发 embed_model 初始化）。

**修复方案：** 保持 Go 行为即可（防御性编程更好），但需注意文档标注差异。

---

### M-18 TeamHelpers: TeamStreamLogger 未集成

**Python 样例：**
```python
if stream_trace_enabled:
    traces_dir = get_agent_teams_home() / "traces"
    traces_dir.mkdir(parents=True, exist_ok=True)
    lg = TeamStreamLogger(file_path=str(traces_dir / f"dump-team-{session_id}.txt"))
```

**Go 问题：**
```go
_ = streamTraceEnabled // TODO: TeamStreamLogger 对齐
```

**修复方案：** 实现 TeamStreamLogger 集成，在 `consumeStreamWithQuery` 中创建并使用。

---

### M-19 evolution watcher goroutine 缺少 recover 防 panic

**Python 样例：** Python 的 asyncio task 异常由事件循环捕获。

**Go 问题：** `watchTeamEvolutionAndPushTeam` 的 goroutine 中 for 循环无 recover，如果 panic 会直接崩溃整个进程。

**修复方案：** 在 goroutine 入口添加 `defer func() { if r := recover(); r != nil { ... } }()`。

---

### M-20 AttachDistributedHooksForRunnerRuntime 传 nil — 分布式 hooks 失效

**Python 样例：**
```python
await tm.attach_distributed_hooks_for_runner_runtime(
    team_name=ready_team_name, session_id=session_id, channel_id=channel_id
)
```

**Go 问题：**
```go
tm.AttachDistributedHooksForRunnerRuntime(readyTeamName, sessionID, nil)
// 标记 ⤵️(#9.72) 待回填，直接 return false
```

分布式部署场景下行为不正确。

**修复方案：** 待 #9.72 distributed_runtime 回填后实现。

---

### M-21 utilsParseStreamChunk 无实际解析

**Python 样例：**
```python
def parse_stream_chunk(chunk):
    # 复杂的 chunk → dict 转换逻辑
    if isinstance(chunk, dict): return chunk
    result = {}
    if hasattr(chunk, 'type'): result['type'] = chunk.type
    if hasattr(chunk, 'payload'): result['payload'] = chunk.payload
    ...
```

**Go 问题：** `utilsParseStreamChunk` 直接返回 chunk（无实际解析），与 Python 的提取/转换逻辑不一致。

**修复方案：** 实现 chunk 到 map 的完整转换逻辑，对齐 Python 的属性提取。

---

### M-22 TeamRunner: leader 信息获取方式差异

**Python 样例：**
```python
blueprint = getattr(activation.agent, "blueprint", None)
leader_member_name = getattr(blueprint, "member_name", None)
leader_role = getattr(blueprint, "role", None)
```

**Go 问题：**
```go
if bp := activation.Agent.AgentCard(); bp != nil {
    leaderMemberName = bp.Name
}
leaderRole := atschema.TeamRoleLeader
```

Python 的 `leader_role` 可能为 None（直接传给 TeamOutputSchema），Go 固定传 `TeamRoleLeader`。当 blueprint 无 role 时行为不同。

**修复方案：** 从 AgentCard 或 spec 中获取真实 role，而非硬编码 Leader。

---

### M-23 ChromaIndexer: UpdateIndex 错误传播 vs Python 吞错

**Python 样例：**
```python
except Exception as e:
    logger.error(...); return False
```

**Go 问题：** `UpdateIndex` 在 `DeleteIndex` 失败时返回 `(false, err)`——Go 暴露 error，Python 吞错返回 False。Go 做法更好，但与 Python 行为不一致。

**修复方案：** 保持 Go 行为（暴露 error），标注差异。

---

## 提示问题 (T-01 ~ T-09)

### T-01 SessionOps: copyFile 不复制修改时间

**Python 样例：** `shutil.copy2` 复制内容和 mtime。**Go 问题：** `copyFile` 只复制内容和权限。可通过 `os.Chtimes` 补充，但当前场景无功能影响。

### T-02 SessionOps: deepCopyMap JSON 限制

Python `copy.deepcopy` 对任何对象都能深拷贝。Go 的 `deepCopyMap` 通过 JSON 序列化/反序列化，无法处理 chan/func 类型。当前 state dict 仅含 JSON 可序列化类型，无实际影响。

### T-03 ChromaIndexer: 缺少 doc_index_callback 类型校验

Python 检查 `issubclass(doc_index_callback, BaseCallback)`。Go 的 `WithChromaDocIndexCallback` 无类型校验。

### T-04 ChromaIndexer: 缺少 vector_field str→ChromaVectorField 自动转换

Python 接受 str 类型自动转为 ChromaVectorField。Go 只接受 `*vector_fields.ChromaVectorField`。

### T-05 ChromaVectorStore: SparseSearch 返回 (nil, nil) 而非 ([], nil)

Python 失败时返回空列表 `[]`，Go 返回 `(nil, nil)`。调用方如果不检查 nil 可能不同，但 `len(nil) == 0` 使两者等价。

### T-06 ChromaVectorStore: NewChromaVectorStore 使用 context.Background() 创建 collection

**Go L186:** `s.client.GetOrCreateCollection(context.Background(), ...)` — 未传入 ctx。应考虑添加 ctx 参数。

### T-07 ComputeChunkEmbeddings: Go 日志更丰富

Go 版本在各路径都添加了详细的结构化日志，Python 只有少数 logger 调用。这是改进而非差异。

### T-08 RRFFusion: 实现完全一致

数学公式和排序逻辑完全对齐 Python。

### T-09 SessionOps: SessionStateKey 值确认一致

Python `_SESSION_STATE_KEY = "deepagent"`，Go `SessionStateKey = "deepagent"`，一致。

---

## ⤵️ 占位代码确认

| 标记位置 | 功能 | 实际状态 |
|----------|------|---------|
| `deep_adapter_team.go` L699 | `AttachDistributedHooksForRunnerRuntime` 传 nil | **确实未实现**，return false |
| `deep_adapter_team.go` L816,852 | `pushEvolutionStatus`/`pushEvolutionEvent` 仅日志 | **确实未实现**，前端无法收到事件 |
| `deep_adapter_team.go` L626 | TeamStreamLogger 未集成 | **确实未实现**，`_ = streamTraceEnabled` |
| `team_manager_distributed.go` L30 | distributed_runtime 完整实现 | **确实未实现** |
| `team_manager_lifecycle.go` L176 | `_ensure_postgresql_for_leader` | **确实未实现** |

---

## 修复优先级建议

| 优先级 | 问题 | 说明 |
|--------|------|------|
| **P0 立即修复** | S-01, S-05, S-07, S-13 | inputs 丢失/并发 fatal error/IndexExists 语义/常量不一致 |
| **P1 本周修复** | S-02, S-03, S-04, S-06, S-08, S-09, S-10, S-11, S-12 | 功能缺失/数据不一致 |
| **P2 下周修复** | S-14, M-01~M-23 | 代码质量/行为对齐 |
| **P3 择机修复** | T-01~T-09 | 日志/风格/微优化 |
