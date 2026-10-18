# 48h 逻辑审查 — 2026-10-13

> 审查范围：48小时内提交记录涉及的实现章节 + 最近一周完成的关键功能模块深度复查
> - 9.85 TeamRunner（runtime/dispatch + team_runner + manager Activate 内联回填）
> - 10.3.8 TeamHelpers（processTeamMessageStream + consumeStreamWithQuery + evolution 监控）
> - 13.2 ChromaIndexer（retrieval/indexing 完整索引管道）
> - 10.6.3-10 Stream Rails + Monitor/TeamStreamLogger + TeamManager
> - Extra() 并发修复（17d8eb9d）+ NewSessionMemoryManager 修复（266c8d1b）
> - 批量测试提交（203bed1e）质量评估
>
> 审查方法：对照 Python 参考项目（jiuwenswarm + openjiuwen），逐函数对比方法签名、步骤完整性、⤵️占位代码实际状态

---

## 汇总

| 级别 | 数量 |
|------|------|
| 严重 (S) | 29 |
| 一般 (M) | 27 |
| 提示 (T) | 14 |
| **合计** | **70** |

---

## 严重问题 (S-01 ~ S-29)

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

`Invoke` 和 `Stream` 第二参数传 `nil` 而非 `inputs`。Python 传的是包含用户 query 的 `inputs` dict。首次请求的用户输入数据在 TeamAgent 路径中丢失。

**修复方案：** 将 `inputs` 参数传递下去：
```go
inputsDict, _ := inputs.(map[string]any)
result, err := activation.Agent.Invoke(ctx, inputsDict, agentinterfaces.WithSession(activation.Session))
agentCh, streamErr := activation.Agent.Stream(ctx, inputsDict, agentinterfaces.WithSession(activation.Session))
```

---

### S-02 TeamRunner: runAgentTeamStreaming reject 时返回 `(nil, nil)`，调用方 range over nil channel 永久阻塞

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L246
return  # async generator 结束，调用方的 async for 循环正常退出
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L452-453
return nil, nil  // 返回 nil channel，调用方 for range ch 永久阻塞
```

Go 中 range over nil channel 会永久阻塞，与 Python async generator `return` 语义完全不同。

**修复方案：** 返回已关闭的空 channel：
```go
if IsTeamRejectKind(action.Kind) {
    // ...
    emptyCh := make(chan stream.Schema)
    close(emptyCh)
    return emptyCh, nil
}
```

---

### S-03 TeamRunner: closeTeamInteractGate 使用 `context.Background()` 丢弃调用方的 context

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L711-715
await entry.interact_gate.close_and_drain()
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L577
_ = entry.InteractGate.CloseAndDrain(context.Background())
```

使用 `context.Background()` 而非从调用方传入的 ctx。如果 `CloseAndDrain` 有超时或取消语义，即使上层请求已取消操作仍继续执行，可能导致资源泄漏或死锁。

**修复方案：** 给 `closeTeamInteractGate` 增加 ctx 参数，从调用方传入：
```go
func closeTeamInteractGate(ctx context.Context, teamName string, sessionID string) {
    _ = entry.InteractGate.CloseAndDrain(ctx)
}
```

---

### S-04 TeamRunner: RunBaseTeam/RunTeamMember 的 finally 块未使用 defer 保证清理

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L303-308
try:
    return await team_instance.invoke(inputs, session=team_session)
finally:
    if team_runtime is not None:
        team_runtime.unbind_team_session(team_session.get_session_id())
    await team_session.post_run()
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L132-151
// 顺序执行 UnbindTeamSession + PostRun，无 defer/try-finally 等价
```

如果 `Invoke` 内部 panic 且未被 recover，`UnbindTeamSession` 和 `PostRun` 不会执行。

**修复方案：** 使用 defer 模拟 finally：
```go
defer func() {
    if teamRuntime != nil {
        teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
    }
    _ = teamSession.PostRun(ctx)
}()
result, invokeErr := teamInstance.Invoke(ctx, inputsDict, ...)
```

---

### S-05 TeamRunner: runAgentTeamStreaming 中 leader_member_name 取错属性

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L247-249
blueprint = getattr(activation.agent, "blueprint", None)
leader_member_name = getattr(blueprint, "member_name", None)
leader_role = getattr(blueprint, "role", None)
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L456-460
var leaderMemberName string
if bp := activation.Agent.AgentCard(); bp != nil {
    leaderMemberName = bp.Name
}
leaderRole := atschema.TeamRoleLeader
```

Python 的 `blueprint.member_name` 是团队内角色标识（如 `"leader"`），而 `AgentCard().Name` 是对外显示名。两者是完全不同的概念。此外 `leaderRole` 直接硬编码为 `TeamRoleLeader`，而非从 blueprint 读取。

**修复方案：** 使用 `activation.Agent.Blueprint()` 获取 blueprint，再从中获取 `MemberName()` 和 `Role()`。

---

### S-06 TeamHelpers: sessionID 缺少默认值 "default"

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L617
session_id = request.session_id or "default"
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L204-207
sessionID := ""
if req.SessionID != nil {
    sessionID = *req.SessionID
}
```

空 sessionID 会导致 TeamManager 所有按 session 索引的操作失败。

**修复方案：**
```go
sessionID := "default"
if req.SessionID != nil && *req.SessionID != "" {
    sessionID = *req.SessionID
}
```

---

### S-07 TeamHelpers: 缺失 wait_for_pending_shutdown_cleanup 前置等待

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L625-635
try:
    from jiuwenswarm.agents.harness.team.remote_member_bootstrap import (
        wait_for_pending_shutdown_cleanup_for_session,
    )
    await wait_for_pending_shutdown_cleanup_for_session(session_id)
except Exception as exc:
    logger.warning(...)
```

**Go 问题：** 不存在此步骤。如果上一次 shutdown 清理未完成就发起新 team 消息，可能导致 pool 条目残留、DB 状态不一致。

**修复方案：** 在 `teamManager := team.GetTeamManager(channelID)` 后、`isFirstRequest` 判断前，添加 `WaitForPendingShutdownCleanupForSession` 调用（15秒超时）。需 TeamManager 暴露此方法或实现等效机制。

---

### S-08 TeamHelpers: 缺失 request.params.mode → request_metadata 合并

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L702-704
request_metadata = dict(request.metadata or {})
if isinstance(getattr(request, "params", None), dict):
    request_metadata.setdefault("mode", request.params.get("mode"))
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L302-308
var requestMetadata map[string]any
if req.Metadata != nil {
    requestMetadata = make(map[string]any, len(req.Metadata))
    for k, v := range req.Metadata {
        requestMetadata[k] = v
    }
}
// 缺少从 req.Params 解析 mode 并 setdefault 到 requestMetadata
```

`team.plan` 模式无法通过 request params 触发。

**修复方案：** 添加从 `req.Params` 解析 mode 并 setdefault：
```go
if len(req.Params) > 0 {
    var params map[string]any
    if json.Unmarshal(req.Params, &params) == nil {
        if mode, ok := params["mode"].(string); ok && mode != "" {
            if requestMetadata == nil {
                requestMetadata = map[string]any{}
            }
            if _, exists := requestMetadata["mode"]; !exists {
                requestMetadata["mode"] = mode
            }
        }
    }
}
```

---

### S-09 TeamHelpers: consumeStreamWithQuery 缺失异常处理 + team.error 广播

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L1021-1047
except asyncio.CancelledError:
    logger.info("[TeamHelpers] stream cancelled: ...")
    raise
except Exception as exc:
    logger.error("[TeamHelpers] stream failed: ...", exc_info=True)
    _broadcast_event(channel_id, session_id, {
        "event_type": "team.error", "error": str(exc), "session_id": session_id,
    })
finally:
    get_team_manager(channel_id).clear_pending_runtime(session_id)
    get_team_manager(channel_id).pop_stream_task(session_id)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L597-601
defer func() {
    tm := team.GetTeamManager(channelID)
    tm.ClearPendingRuntime(sessionID)
    tm.PopStreamTask(sessionID)
}()
```

缺失 panic recover、ctx cancel 处理和异常时 team.error 广播。

**修复方案：** 添加 recover 和 ctx 取消处理：
```go
defer func() {
    if r := recover(); r != nil {
        logger.Error(logComponent).Any("recover", r).Msg("consumeStreamWithQuery panic")
        broadcastEvent(channelID, sessionID, map[string]any{
            "event_type": "team.error", "error": fmt.Sprintf("panic: %v", r), "session_id": sessionID,
        })
    }
    tm := team.GetTeamManager(channelID)
    tm.ClearPendingRuntime(sessionID)
    tm.PopStreamTask(sessionID)
}()
```

---

### S-10 TeamHelpers: envs 未传递到 RunAgentTeamStreaming

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L932-938
async for chunk in Runner.run_agent_team_streaming(
    agent_team=team_spec, inputs={"query": initial_query},
    session=session_id, envs=envs, stream_logger=lg,
):
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L631
streamCh, err := ateamruntime.RunAgentTeamStreaming(ctx, teamSpec, map[string]any{"query": initialQuery}, false, false, sessionID, nil)
```

`envs` 和 `streamLogger` 都传了 nil。`hide_dm` 和 `JIUWENSWARM_TEAM_STREAM_TRACE` 环境变量虽然被提取到 `streamEnvs`，但实际未被底层 Runner 接收。

**修复方案：** `RunAgentTeamStreaming` 签名需扩展以支持 `envs` 参数传入。

---

### S-11 TeamHelpers: TeamStreamLogger 未集成

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L924-931
stream_trace_enabled = bool(_envs.get(_STREAM_TRACE_ENV_KEY) or os.environ.get(_STREAM_TRACE_ENV_KEY))
lg: TeamStreamLogger | None = None
if stream_trace_enabled:
    traces_dir = get_agent_teams_home() / "traces"
    traces_dir.mkdir(parents=True, exist_ok=True)
    lg = TeamStreamLogger(file_path=str(traces_dir / f"dump-team-{session_id}.txt"))
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L626
_ = streamTraceEnabled // TODO: TeamStreamLogger 对齐
```

Go 端已有 `monitor.NewTeamStreamLogger` 完整实现，但未接入。

**修复方案：** 在 `streamTraceEnabled` 为 true 时创建 logger 并传入 `RunAgentTeamStreaming`。

---

### S-12 TeamHelpers: /evolve 中 items 传 nil 导致审批记录丢失

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L597-607
if isinstance(evolve_result, str) and evolve_result.strip():
    return {"output": f"Skill '{skill_name}' 演进请求已提交...", "result_type": "answer"}
return _approval_result_from_event_or_items(
    skill_name=skill_name,
    event=getattr(evolve_result, "approval_event", None),
    items=list(getattr(evolve_result, "records", []) or []),
    ...
)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/team_helpers.go L330-336
return approvalResultFromEventOrItems(
    skillName, evolveResult.ApprovalEvent,
    nil,  // items 始终为 nil，丢失 records
    ...
)
```

当 `ApprovalEvent` 为空但 `Records` 非空时，会走到 `noChangesOutput` 而非正确结果。

**修复方案：** 将 records 转为 `[]map[string]any` 并传入：
```go
var items []map[string]any
if evolveResult != nil && len(evolveResult.Records) > 0 {
    for _, rec := range evolveResult.Records {
        items = append(items, rec.ToMap())
    }
}
```

---

### S-13 TeamHelpers: evolve_list 表格缺失 Used/Effect 列

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L466-484
"| # | Score | Used | Effect | Section | Content (preview) |",
"|---|---:|---|---|---|---|",
# ...
used_str = f"{stats.times_used}/{stats.times_presented}" if stats.times_presented else "0/0"
effect_str = f"+{stats.times_positive}/-{stats.times_negative}"
```

**Go 问题：**
```go
// internal/swarm/server/adapter/team_helpers.go L185-199
"| # | Score | Section | Content (preview) |",
"|---|---:|---|---|",
// 完全缺失 Used 和 Effect 列
```

**修复方案：** 添加 `Used` 和 `Effect` 列，从 `record.UsageStats` 提取。

---

### S-14 TeamHelpers: watchTeamEvolutionAndPushTeam 缺失外层异常处理

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L1376-1392
except Exception as exc:
    logger.warning("[TeamHelpers] evolution monitor failed: %s", exc)
    await push_evolution_status(..., status="end", message=f"团队技能演进分析失败: {exc}")
```

**Go 问题：** `watchTeamEvolutionAndPushTeam` 没有外层 recover/异常处理，goroutine 中 for 循环如果 panic 会直接崩溃整个进程。

**修复方案：** 在 for 循环外层添加 `defer recover` 并在异常时推送 "end" 状态。

---

### S-15 ChromaIndexer: IndexExists 永远返回 True（底层用 GetOrCreateCollection）

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/indexer/chroma_indexer.py L260-268
try:
    await asyncio.to_thread(self._client.get_collection, name=index_name)
    return True
except Exception:
    return False
```

**Go 问题：** Go 的 `NewChromaVectorStore` 使用 `GetOrCreateCollection`（如果不存在会创建！），导致 `IndexExists` 永远返回 True，因为不存在的集合会被自动创建。

**修复方案：** `IndexExists` 必须使用 `client.GetCollection`（不会创建）而非 `GetOrCreateCollection`。建议在 ChromaIndexer 中存储 client 字段，直接调用 `GetCollection`。

---

### S-16 ChromaIndexer: 缺少 `_client` 字段，每次操作新建 ChromaVectorStore

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/indexer/chroma_indexer.py L90-93
self._client = ChromaVectorStore.create_client(
    database_name=self.database_name, path_or_uri=chroma_path,
)
```

**Go 问题：** ChromaIndexer 没有 client 字段，`DeleteIndex`/`IndexExists`/`GetIndexInfo` 每次都新建 `ChromaVectorStore` 来获取客户端，浪费资源且与 Python 的单客户端复用语义严重不符。

**修复方案：** 在 `ChromaIndexer` 中添加 `client chromav2.Client` 字段，在 `NewChromaIndexer` 中创建并存储客户端。

---

### S-17 ChromaVectorStore: CheckVectorField 传空 map 作为 actual 配置，总是报 mismatch

**Python 样例：**
```python
# openjiuwen/core/retrieval/vector_store/chroma_store.py L131-133
def check_vector_field(self) -> None:
    self._check_configs_matching(self._construct_config, self.collection.configuration.get("hnsw", {}))
```

**Go 问题：**
```go
// internal/agentcore/retrieval/vector_store/chroma.go L219-221
func (s *ChromaVectorStore) CheckVectorField() error {
    return CheckConfigsMatching(s.constructConfig, map[string]any{})
}
```

传入空 map 作为 `actual` 参数，每个 configured 字段都会 mismatch，总是抛出异常。

**修复方案：** 从 `s.collection` 获取实际的 HNSW 配置作为 `actual` 参数传入。

---

### S-18 ChromaVectorStore: GetOrCreateCollection 只传距离度量，不传 HNSW 完整配置

**Python 样例：**
```python
# openjiuwen/core/retrieval/vector_store/chroma_store.py L99-102
self._collection = self._client.get_or_create_collection(
    name=self.collection_name,
    configuration={"hnsw": self._construct_config | self._search_config},
)
```

**Go 问题：**
```go
// internal/agentcore/retrieval/vector_store/chroma.go L186-188
collection, err := s.client.GetOrCreateCollection(context.Background(), s.collectionName,
    chromav2.WithHNSWSpaceCreate(mapToChromaDistanceMetric(s.distanceMetric)),
)
```

没有传入 HNSW 的 `max_neighbors`/`ef_construction`/`ef_search` 等参数，Go 创建的集合使用 ChromaDB 默认值而非用户指定值。

**修复方案：** 在 `GetOrCreateCollection` 中传入完整的 HNSW 配置参数。

---

### S-19 ChromaVectorStore: ConvertL2Squared 传 maxDist=0 导致 NaN

**Python 样例：**
```python
# openjiuwen/core/retrieval/vector_store/chroma_store.py L486
raw_score_scaled = convert_l2_squared(raw_score_val)  # default max_dist=4.0
```

**Go 问题：**
```go
// internal/agentcore/retrieval/vector_store/chroma.go L679
scaled := vector.ConvertL2Squared(*rawScoreVal, 0)  // BUG: maxDist=0 causes NaN
```

`(0 - rawScore) / 0` 产生 NaN 或 -Inf。foundation 层的 `getScoreConverter` 使用 `4.0` 是正确的。

**修复方案：** 将 `0` 改为 `4.0`：
```go
scaled := vector.ConvertL2Squared(*rawScoreVal, 4.0)
```

---

### S-20 ChromaVectorStore: 缺少 CreateClient 静态方法，不处理自定义数据库名

**Python 样例：**
```python
# openjiuwen/core/retrieval/vector_store/chroma_store.py L119-129
@staticmethod
def create_client(database_name: str, path_or_uri: str, ...) -> chromadb.PersistentClient:
    if database_name and database_name != DEFAULT_DATABASE:
        admin_client = chromadb.AdminClient(Settings(is_persistent=True, persist_directory=path_or_uri))
        if database_name not in {db.get("name") for db in admin_client.list_databases()}:
            admin_client.create_database(database_name)
    return chromadb.PersistentClient(path=path_or_uri, database=database_name)
```

**Go 问题：** 直接创建 `PersistentClient`，无 `database_name` 参数传递，无法创建/使用自定义数据库。

**修复方案：** 在 `NewChromaVectorStore` 或独立的 `CreateClient` 函数中实现 Python 的 `create_client` 逻辑。

---

### S-21 ChromaIndexer: UpdateIndex 错误处理与 Python 语义不同

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/indexer/chroma_indexer.py L207-215
try:
    await self.delete_index(doc_id, config.index_name)
    return await self.build_index(chunks, config, embed_model, **kwargs)
except Exception as e:
    logger.error(f"Failed to update index: {e}")
    return False  # 吞掉异常返回 False
```

**Go 问题：**
```go
// internal/agentcore/retrieval/indexing/indexer/chroma.go L282-296
_, err := ci.DeleteIndex(ctx, docID, config.IndexName)
if err != nil {
    return false, err  // Python 返回 False 不抛异常，Go 传播 error
}
```

Python 无论哪一步失败都只返回 `False`（吞掉异常），Go 会向上传播错误，调用方行为不一致。

**修复方案：** 将 `UpdateIndex` 改为与 Python 一致的错误处理：捕获所有 error，记录日志后返回 `(false, nil)`。

---

### S-22 Monitor: consumeMonitorEvents 注册为 stream task，CancelAllStreamTasks 过度取消

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L47-50
asyncio.create_task(_consume_monitor_events(channel_id, session_id, monitor_handler))
# Python 不将此 task 注册到 _stream_tasks
```

**Go 问题：**
```go
// internal/swarm/agents/harness/team/team_manager_monitor.go L207-210
if handler.IsRunning() {
    consumeCtx, cancel := context.WithCancel(context.Background())
    m.RegisterStreamTask(sessionID+"_monitor", cancel)  // 注册到 stream tasks
    go m.consumeMonitorEvents(consumeCtx, sessionID, handler)
}
```

`CancelAllStreamTasks` 会连带取消 monitor 事件消费 goroutine，但 Python 中 `_consume_monitor_events` 是独立 task，`cancel_all_stream_tasks` 不触及它。

**修复方案：** 不将 consumeMonitorEvents 注册为 stream task（对齐 Python），仅依赖 `handler.Stop()` 关闭 eventCh 来终止消费循环。

---

### S-23 Monitor: consumeMonitorEvents 中 context.Background() 丢失上游 ctx

**Go 问题：**
```go
// internal/swarm/agents/harness/team/team_manager_monitor.go L208
consumeCtx, cancel := context.WithCancel(context.Background())
```

`EnsureMonitor` 接收 `ctx context.Context` 参数，但 consume goroutine 使用 `context.Background()` 创建子 context，丢失了上游的 ctx 取消信号。

**修复方案：** 使用传入的 ctx 作为父 context：
```go
consumeCtx, cancel := context.WithCancel(ctx)
```

---

### S-24 Extra() 并发修复不完整：ForkForToolCall 共享 extra map 但子对象无锁

**Go 问题：**
```go
// internal/agentcore/single_agent/interfaces/callback.go
func (c *AgentCallbackContext) ForkForToolCall(toolCall *llmschema.ToolCall) *AgentCallbackContext {
    return &AgentCallbackContext{
        // ...
        extra:   c.extra,  // 共享父对象的 extra map
        // 缺少: extraMu 引用！子对象无法获取锁
    }
}

func (c *AgentCallbackContext) Extra() map[string]any { return c.extra }
```

`ForkForToolCall` 创建的子上下文共享父的 `extra` map，但没有引用 `extraMu` 互斥锁。子对象通过 `Extra()` 直接返回 map 引用，绕过了所有锁保护。虽然 `GetExtra`/`SetExtra`/`DeleteExtra` 方法有锁保护，但生产代码中有 7+ 处仍使用 `Extra()["key"]` 直接 map 读写（未迁移到线程安全方法）。

**涉及的生产代码文件：**
- `internal/agentcore/single_agent/interrupt/handler.go`
- `internal/agent_teams/observability/rail.go`
- `internal/agent_teams/team_workspace/rail.go`
- `internal/swarm/agents/harness/common/rails/stream_event_rail.go`
- `internal/swarm/agents/harness/common/rails/avatar_rail.go`
- `internal/swarm/server/hooks/user_hook_rail.go`

**修复方案：**
1. 将所有 `Extra()["key"] = value` 迁移为 `SetExtra(key, value)`
2. 将所有 `val, ok := Extra()["key"]` 迁移为 `GetExtra(key)`
3. 或者：让 `Extra()` 返回一个受锁保护的代理 map（通过 `sync.RWMutex` 保护的读写方法）

---

### S-25 DecideRunAction 使用 panic 而非返回 error

**Python 样例：**
```python
# openjiuwen/agent_teams/runtime/dispatch.py L139-144
raise RuntimeError(
    f"dispatch invariant violated: pool entry for {target_team_name!r} "
    f"on session {pool_entry.session_id!r} must be torn down before dispatching to session {target_session_id!r}"
)
```

**Go 问题：**
```go
// internal/agent_teams/runtime/dispatch.go L128-133
panic(fmt.Sprintf(
    "dispatch invariant violated: pool entry for %q on session %q must be torn down before dispatching to session %q",
    targetTeamName, poolEntry.SessionID, targetSessionID,
))
```

Go 的 panic 会崩溃整个进程（除非有 recover），Python 的 raise 可以被调用方捕获。

**修复方案：** 改为返回 error：
```go
func DecideRunAction(...) (RunAction, error) {
    // ...
    return RunAction{}, fmt.Errorf("dispatch invariant violated: ...")
}
```

---

### S-26 ChromaVectorField: ToConstructDict 输出 PascalCase 而非 snake_case

**Python 样例：**
```python
# openjiuwen/core/foundation/store/vector_fields/base.py L93-98
model_content = {attr: val for attr, val in model_filtered if val is not None}
# attr 来自 Pydantic model_fields，如 "max_neighbors"
```

**Go 问题：**
```go
// internal/agentcore/foundation/store/vector_fields/base.go L188
result[field.Name] = fieldValue.Interface()
// field.Name 是 Go 结构体字段名如 "MaxNeighbors"
```

ChromaDB Python SDK 期望 snake_case 键名（如 `max_neighbors`），Go 输出 PascalCase（如 `MaxNeighbors`），配置匹配会失败。

**修复方案：** 在 `collectFields` 中将 Go 的 PascalCase 字段名转换为 snake_case。

---

### S-27 TeamHelpers: teamHelperEventAsMap 无法处理 *OutputSchema 类型

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L200+
# _approval_chunk_from_event 通过 parse_stream_chunk(evt) 处理各种类型的事件
```

**Go 问题：**
```go
// internal/swarm/server/adapter/team_helpers.go L580-589
func teamHelperEventAsMap(v any) map[string]any {
    if v == nil { return nil }
    if m, ok := v.(map[string]any); ok { return m }
    return nil  // *stream.OutputSchema 到这里返回 nil，审批事件丢失
}
```

**修复方案：** `teamHelperEventAsMap` 需要处理 `*stream.OutputSchema` 类型，提取其 Payload。

---

### S-28 TeamHelpers: DrainPendingApprovalEvents wait 参数错误

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L1180
events = await rail.drain_pending_approval_events(wait=False) or []
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L877
events := rail.DrainPendingApprovalEvents(true, nil)
```

`wait=true` 意味着在没有事件时会阻塞等待，与 Python 的 `wait=False` 不一致，可能导致不必要的等待时间叠加。

**修复方案：** 将第一个参数改为 `false`：
```go
events := rail.DrainPendingApprovalEvents(false, nil)
```

---

### S-29 TeamRunner: createAgentTeamSession 缺少 AgentSession 分支

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L688-692
if isinstance(session, AgentSession):
    return create_agent_team_session(
        session_id=session.get_session_id(), envs=session.get_envs(),
    )
```

**Go 问题：** `createAgentTeamSession` L558-568 缺少对 `*session.Session`（Python AgentSession）的处理。

**修复方案：** 增加 `*session.Session` 分支：
```go
case *session.Session:
    return session.CreateAgentTeamSession(s.GetSessionID(), s.GetEnvs(), "")
```

---

## 一般问题 (M-01 ~ M-27)

### M-01 TeamRunner: RunTeamMember 中 FinalizeMember 返回的 error 被忽略

**Go 问题：**
```go
// internal/agentcore/runner/team_runner.go L238
_ = FinalizeMember(ctx, ag)
```

**修复方案：** 记录日志：
```go
if finErr := FinalizeMember(ctx, ag); finErr != nil {
    logger.Warn(teamRunnerLogComponent).Err(finErr).Msg("RunTeamMember: FinalizeMember 失败")
}
```

---

### M-02 TeamRunner: RunTeamMemberStreaming goroutine 中使用外层 ctx 可能已被取消

**Go 问题：** `RunBaseTeamStreaming` L193-202 和 `RunTeamMemberStreaming` L287-297 在 goroutine 中使用 `ctx` 执行清理操作，但外层函数返回 `mergedCh` 后 ctx 可能已被取消。

**修复方案：** 在 goroutine 中使用 `context.WithoutCancel(ctx)` 或 `context.Background()`：
```go
go func() {
    defer close(mergedCh)
    cleanupCtx := context.WithoutCancel(ctx)
    // ... 使用 cleanupCtx 执行清理操作
}()
```

---

### M-03 TeamRunner: RunBaseTeam/RunTeamMember reject 时返回 `(nil, nil)` 语义不明确

**Go 问题：** 非 streaming 路径 `return nil, nil` 让调用方无法区分"被拒绝"和"成功但无结果"。

**修复方案：** 返回 sentinel error：
```go
return nil, fmt.Errorf("run_agent_team rejected: kind=%s, reason=%s", action.Kind, action.Reason)
```

---

### M-04 TeamRunner: runAgentTeamStreaming 中 strPtr("") vs nil 差异

**Go 问题：** `buildTeamRuntimeReadyChunk` 中 `strPtr(leaderMemberName)` 在空字符串时返回 `*string("")` 而非 nil，Python 的 `source_member=leader_member_name` 在 None 时传 None。

**修复方案：**
```go
var memberNamePtr *string
if leaderMemberName != "" {
    memberNamePtr = strPtr(leaderMemberName)
}
```

---

### M-05 TeamHelpers: DeepAgent nil 校验缺失

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L700-701
if deep_agent is None:
    raise RuntimeError("DeepAgent not initialized")
```

**Go 问题：** `d.instance` 可能为 nil，但没有在调用 `GetEnrichedTeamSpec` 前做 nil 检查。

**修复方案：** 添加 nil 校验并在 DeepAgent 未初始化时返回错误响应。

---

### M-06 TeamHelpers: AttachDistributedHooksForRunnerRuntime 传 nil channelID

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L963-967
await tm.attach_distributed_hooks_for_runner_runtime(
    team_name=ready_team_name, session_id=session_id, channel_id=channel_id,
)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L699
tm.AttachDistributedHooksForRunnerRuntime(readyTeamName, sessionID, nil)  // channelID 传 nil
```

**修复方案：** 将 channelID 传入（虽然此方法当前是 ⤵️ 桩，但参数传递应正确）：
```go
tm.AttachDistributedHooksForRunnerRuntime(readyTeamName, sessionID, &channelID)
```

---

### M-07 TeamHelpers: resolveEvolutionEventTimeoutSec 始终返回 fallback

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L1160-1163
event_timeout_sec = resolve_evolution_event_timeout_sec(rail, fallback_sec=TEAM_EVOLUTION_EVENT_TIMEOUT_SEC)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L1209-1217
func resolveEvolutionEventTimeoutSec(rail *coreevolution.TeamSkillEvolutionRail, fallback float64) float64 {
    if rail == nil { return fallback }
    return fallback  // 始终返回 fallback，未读取 rail 中的配置
}
```

**修复方案：** 如果 `TeamSkillEvolutionRail` 有配置字段可获取超时值，应使用之。

---

### M-08 Monitor: interactionSummary 缺少非 dict 路径

**Python 样例：**
```python
# openjiuwen/agent_teams/monitor/stream_logger.py L236-242
if isinstance(payload, dict):
    iid = payload.get("interaction_id", "unknown")
else:
    iid = getattr(payload, "id", "unknown")
```

**Go 问题：** 只处理了 map 路径，缺少对 struct 的反射提取。

**修复方案：** 增加对 struct 的接口断言或反射提取。

---

### M-09 Monitor: Go 多了 MEMBER_CANCELED 映射，Python 不映射此事件

**Python 样例：** Python 的 `SDK_TO_TEAM_EVENT_MAP` 没有 `MEMBER_CANCELED` 映射。

**Go 问题：** `event_types.go` 多加了 `MonitorEventTypeMemberCanceled: TeamEventMemberCanceled`。

**修复方案：** 从 `sdkToTeamEventMap` 中删除此条目，对齐 Python 行为。

---

### M-10 Monitor: consumeMonitorEvents 缺少 panic recover 兜底

**Go 问题：** 如果 `handler.Events()` 返回的 channel 操作或广播回调 panic，整个 goroutine 崩溃，没有错误日志。

**修复方案：** 添加 `defer recover` 兜底，记录错误日志。

---

### M-11 Monitor: autoStartEvolutionWatcher 重复消费 monitor 事件

**Python 样例：** Python 的 evolution watcher 是独立的（监控 SkillEvolutionRail 的审批状态，与 monitor 事件流是两个不同来源）。

**Go 问题：** Go 的 `autoStartEvolutionWatcher` 重复了 `consumeMonitorEvents` 的逻辑（从 handler.Events() 读事件并广播），而非真正监控 evolution 审批状态。

**修复方案：** 改为真正的占位（仅注册 cancel 函数，不启动无效的重复广播 goroutine）。

---

### M-12 Monitor: Interact 返回 error（Python 吞掉异常返回 False）

**Python 样例：**
```python
# jiuwenswarm/agents/harness/team/team_manager.py
async def interact(self, session_id: str, user_input: Any) -> bool:
    try:
        # ...
        return success
    except Exception as exc:
        logger.error("[TeamManager] interact failed: ...")
        return False  # 吞掉异常
```

**Go 问题：**
```go
// internal/swarm/agents/harness/team/team_manager_interact.go
return false, err  // 传播 error
```

**修复方案：** 如严格对齐 Python，应吞掉 error 返回 `(false, nil)`。当前设计选择可保留但需文档说明。

---

### M-13 ChromaVectorStore: addBatch embedding 类型断言只支持 []float64

**Go 问题：** `addBatch` 中 `embedding, _ := node[s.vectorField.VectorFieldName].([]float64)` 只尝试 `[]float64` 类型断言，如果嵌入是 `[]float32` 或 `[]any` 会被跳过。

**修复方案：** 增加 `[]float32` 和 `[]any` 类型断言的 fallback 逻辑。

---

### M-14 ChromaVectorStore: addBatch JSON 解析 metadata 失败时静默忽略

**Python 样例：**
```python
# openjiuwen/core/retrieval/vector_store/chroma_store.py L213-215
except Exception:
    logger.warning(f"Failed to load metadata: {raw_metadata}")
```

**Go 问题：** JSON 解析失败时无日志。

**修复方案：** 添加 Warn 日志，对齐 Python。

---

### M-15 ChromaVectorStore: HybridSearch 缺少 str(hash) fallback

**Python 样例：**
```python
# openjiuwen/core/retrieval/vector_store/chroma_store.py L396-397
result_id = rr.metadata.get("id") or id_mapping.get(rr.text, str(hash(rr.text)))
```

**Go 问题：** `resultID` 为空时不设置（Go 用空字符串），Python 用 `str(hash)` 作为 fallback。

**修复方案：** 添加 hash fallback：`if resultID == "" { resultID = fmt.Sprintf("%d", hashString(rr.Text)) }`

---

### M-16 CheckConfigsMatching: isclose 基准用 max(abs(a), abs(b))，Go 用 abs(b)

**Python 样例：**
```python
# Python math.isclose 内部: abs(a-b) <= max(rel_tol * max(abs(a), abs(b)), abs_tol)
```

**Go 问题：**
```go
// internal/agentcore/retrieval/vector_store/base.go L118
isValid = math.Abs(actualFloat-valFloat) <= 1e-2*math.Abs(valFloat)+1e-3
```

当 `abs(a) > abs(b)` 时两者结果不同。

**修复方案：**
```go
maxAbs := math.Max(math.Abs(actualFloat), math.Abs(valFloat))
isValid = math.Abs(actualFloat-valFloat) <= math.Max(1e-2*maxAbs, 1e-3)
```

---

### M-17 ContextAssembleRail: Uninit 多移除了 SectionTools

**Python 样例：**
```python
# openjiuwen/core/single_agent/rail/context_engineer/context_assemble_rail.py L38-40
def uninit(self, agent) -> None:
    if self.system_prompt_builder is not None:
        self.system_prompt_builder.remove_section("workspace")
        self.system_prompt_builder.remove_section("context")
        # Python 不移除 tools
```

**Go 问题：**
```go
// internal/agentcore/harness/rails/context_engineer/context_assemble_rail.go L86-90
r.systemPromptBuilder.RemoveSection(sections.SectionWorkspace)
r.systemPromptBuilder.RemoveSection(sections.SectionContext)
r.systemPromptBuilder.RemoveSection(sections.SectionTools)  // Python 中不存在
```

**修复方案：** 删除 `RemoveSection(sections.SectionTools)` 这行。

---

### M-18 LoggingDocIndexCallback: 日志中 processed 字段是 endIdx 不是累计数

**Go 问题：**
```go
// internal/agentcore/retrieval/common/callbacks.go L82
logger.Info(logComponent).Int("processed", endIdx)  // endIdx 不是累计数
```

**修复方案：** 改为 `Int("processed", c.processed)` 或 `Int("processed", c.processed * batchSize)`。

---

### M-19 ChromaIndexer: BuildIndex 查重遍历顺序不确定

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/indexer/chroma_indexer.py L143
all_doc_ids = sorted({chunk.doc_id for chunk in chunks})  # 排序保证确定性
```

**Go 问题：** 使用 `map[string]bool` 遍历，顺序不确定。

**修复方案：** 先收集到切片再排序遍历。

---

### M-20 ChromaVectorStore: sparse 模式 doc_id 处理差异

**Python 样例：**
```python
# openjiuwen/core/retrieval/vector_store/chroma_store.py L464-465
if "doc_id" not in metadata:
    metadata["doc_id"] = metadata.pop(self.doc_id_field, None)  # pop 失败时设 None
```

**Go 问题：** 当 `doc_id_field` 不存在时，Go 不设置 `doc_id`，Python 设为 None。

**修复方案：** 当 `doc_id_field` 不存在时也设置 `metadata["doc_id"] = nil`。

---

### M-21 ChromaIndexer: 缺少 databaseName 字段

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/indexer/chroma_indexer.py L66
self.database_name = config.database_name
```

**Go 问题：** ChromaIndexer 没有存储 `databaseName`，无法创建/使用自定义数据库。与 S-16 是同一根因的不同表现。

---

### M-22 TeamRunner: resolveSpecFromSessionBucket 缺少 recover 保护

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L648-657
try:
    return TeamAgentSpec.model_validate(spec_data)
except Exception as exc:
    logger.warning("failed to parse persisted spec ...")
    return None
```

**Go 问题：** `NewTeamAgentSpecFromDict(specMap)` 无 recover 保护，如果该函数 panic 会传播到调用方。

**修复方案：** 添加 `defer recover` 保护。

---

### M-23 TeamRunner: RunAgentTeam/RunAgentTeamStreaming 缺少 Python 的 context/envs/stream_modes 参数

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L158-159, L214
context: Optional[ModelContext] = None, envs: Optional[dict[str, Any]] = None
stream_modes: Optional[list[BaseStreamMode]] = None
```

**Go 问题：** 签名缺少这三个参数。

**修复方案：** 在后续版本补齐或在当前签名中增加可选参数。

---

### M-24 lite/manager_impl: buildFileEntry 始终用 os.ReadFile 而 indexFile 在 sysOperation 模式下用 Fs().ReadFile

**Go 问题：** 如果 `sysOperation` 对文件有虚拟化，`buildFileEntry` 中的 `os.Stat/os.ReadFile` 可能失败或读到不一致的内容。

**修复方案：** `buildFileEntry` 在 `sysOperation != nil` 时应使用 `m.sysOperation.Fs().ReadFile`。

---

### M-25 TeamHelpers: onTeamWatcherDone 缺少异常检查

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L1086-1098
def _on_team_watcher_done(task: asyncio.Task) -> None:
    if task.cancelled(): return
    exc = task.exception()
    if exc is not None:
        logger.warning("[TeamHelpers] evolution monitor task exception: %s", exc)
```

**Go 问题：** `onTeamWatcherDone` 仅记录日志，不检查异常。

**修复方案：** 添加异常参数并记录。

---

### M-26 TeamRunner: RunTeamMember/RunTeamMemberStreaming 缺少 _root_task_group_scope 等价

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L353, L375
with self._root_task_group_scope():
```

**Go 问题：** 完全缺少此机制。子任务可能脱离管理。

**修复方案：** 至少添加 context-scoped 取消机制 `ctx, cancel := context.WithCancel(ctx)` + `defer cancel()` 或在 runner.go 的 TaskManager 中注册。当前阶段至少添加注释标记此缺口。

---

### M-27 ChromaVectorStore: Search/SparseSearch/Delete 缺少 QueryExpr 支持

**Python 样例：**
```python
# openjiuwen/core/retrieval/vector_store/chroma_store.py L273-274
elif isinstance(filters, QueryExpr):
    query_args.update({k: v for k, v in filters.to_expr("chroma").items() if v})
```

**Go 问题：** filters 参数只接受 `map[string]any`，不支持 `QueryExpr`。

**修复方案：** 如果项目有 `QueryExpr` Go 实现，应扩展 filters 参数类型。

---

## 提示问题 (T-01 ~ T-14)

### T-01 TeamRunner: PreRun 失败只记录 Warn 不返回 error，与 Python 语义可能不一致

**Python 样例：** `await team_session.pre_run(inputs=...)` 异常会传播到调用方。

**Go 问题：** Go 选择吞掉错误，Warn 日志后继续执行。

**修复方案：** 保持当前行为但添加注释说明此设计差异。

---

### T-02 TeamRunner: DecideRunAction 逻辑与 Python 完全一致

经逐行对比，`DecideRunAction` 的决策真值表与 Python `decide_run_action` 完全一致。唯一差异是 panic vs raise RuntimeError（已在 S-25 中记录）。

---

### T-03 TeamRunner: IsTeamRejectKind 和 RunAction 结构体实现正确

与 Python 语义一致。

---

### T-04 ChromaVectorStore: close 方法 Python 是空操作，Go 调用 client.Close()

Python `ChromaVectorStore.close` 中 `pass`，Go 调用 `s.client.Close()` 关闭客户端。这是 Go 的 SDK v2 需要 Close 释放资源，行为差异合理。

---

### T-05 ChromaVectorStore: HybridSearch alpha 参数未被使用

Python 本身就存在此 TODO（alpha 参数未使用），Go 对齐了 Python 当前行为。

---

### T-06 ChromaIndexer: doc_index_callback 运行时类型校验

Python 的 `isinstance(doc_index_callback, type) and issubclass(...)` 校验在 Go 中由接口类型系统在编译期保证。

---

### T-07 ChromaVectorStore: Add 方法不支持单 dict 输入

Go 强类型不需要 Python 式的单 dict → list 转换，API 更严格但行为一致。

---

### T-08 CheckConfigsMatching: casefold vs ToLower

对于配置参数（ASCII 字母数字），`casefold` 和 `ToLower` 行为等价。

---

### T-09 CheckConfigsMatching: isnumeric vs isNumericString

对于配置参数（ASCII 数字），Python `isnumeric()` 和 Go ASCII `isNumericString` 行为等价。

---

### T-10 Monitor: renderRole 返回空字符串 vs Python 返回 None

Go 中 `""` 在 emit 时通过 `if role == "" { role = unknown }` 兜底，Python 中 `None` 通过 `role or _UNKNOWN` 兜底。实际影响低。

---

### T-11 测试: context_assemble_rail_test.go 核心回调逻辑未测试

测试只验证了回调函数非 nil，从未调用它并验证其行为。属于"占位式"测试。

---

### T-12 测试: constructor_test.go（agent_mode 工具构造函数）是空壳测试

三个工具只验证构造非 nil，工具 Invoke 完全未测试。

---

### T-13 测试: list_dir_test.go 只测了 isHidden 一个极简函数

核心目录遍历逻辑完全未测试。

---

### T-14 占位代码: pushTeamSkillEvolveResolutionStatus/pushEvolutionStatus/pushEvolutionEvent 仅记录日志

这些函数标记为 TODO，完整的 stream 推送需 gateway_push 集成完善后实现。应在代码中添加 `// ⤵️(#9.85)` 标记。

---

## P0 必须修复（影响核心功能）

| 编号 | 问题 | 影响 |
|------|------|------|
| S-01 | inputs 传 nil | agent 收不到用户输入 |
| S-02 | streaming reject 返回 nil channel | 调用方永久阻塞 |
| S-06 | sessionID 无默认值 | TeamManager 按 session 索引失败 |
| S-15 | IndexExists 永远返回 True | 集合不存在被自动创建 |
| S-17 | CheckVectorField 传空 map | 总是报 mismatch |
| S-19 | ConvertL2Squared maxDist=0 | 产生 NaN |
| S-24 | Extra() 并发保护失效 | 运行时 fatal error |

## P1 应修复（功能偏差/数据丢失）

| 编号 | 问题 | 影响 |
|------|------|------|
| S-03 | context.Background() 丢弃 ctx | 资源清理可能死锁 |
| S-04 | finally 无 defer 保证 | 清理操作可能被跳过 |
| S-05 | leader_member_name 取错属性 | ready chunk 信息错误 |
| S-08 | 缺失 mode 合并 | team.plan 无法触发 |
| S-10 | envs 未传递 | 环境变量丢失 |
| S-12 | items 传 nil | 审批记录丢失 |
| S-13 | 表格缺失 Used/Effect 列 | 信息不完整 |
| S-16 | 每次新建 ChromaVectorStore | 资源浪费 |
| S-18 | HNSW 配置不完整 | 使用默认值而非用户指定 |
| S-22 | monitor 被过度取消 | 监控事件提前终止 |
| S-25 | panic 代替 error | 进程崩溃风险 |
| S-26 | PascalCase vs snake_case | 配置匹配失败 |
| S-27 | EventAsMap 不处理 OutputSchema | 审批事件丢失 |
| S-28 | wait 参数错误 | 不必要阻塞 |
| S-29 | 缺少 AgentSession 分支 | session 类型处理不完整 |
