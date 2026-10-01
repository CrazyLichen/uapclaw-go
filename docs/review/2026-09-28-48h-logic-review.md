# 48h 逻辑审查报告 — 2026-09-28

> 审查范围：48 小时内提交的代码（2026-09-26 ~ 2026-09-28）
> 涉及章节：7.13 ExternalMemoryRail / 7.14 Mem0Provider / 7.15 OpenVikingProvider / 7.25 DistributedLock / 9.61 RecoveryManager+metadata / 9.67 Team Observability / 9.68-69 Team Rails/Prompts
> Python 参考路径：`/home/opensource/agent-core/openjiuwen/`

---

## 统计总览

| 类别 | 数量 |
|------|------|
| **S（严重）** | 29 |
| **M（一般）** | 28 |
| **T（提示）** | 16 |
| **合计** | **73** |

---

## 7.25 Memory Common — DistributedLock

### S-01：Release 中 Get+Delete 非原子操作（TOCTOU 竞态条件）

**严重性**：S（继承自 Python 的已知设计缺陷）  
**文件**：`internal/agentcore/memory/common/distributed_lock.go:87-103`

**问题**：Go 的 `Release` 先 `Get` 读取值再比较后 `Delete`，两步不是原子操作。在 Get 和 Delete 之间，锁可能因 TTL 过期被别人获取，导致误删别人的锁。Python 有完全相同的问题。

**Python 样例**：
```python
async def release(self):
    try:
        lock_key = await self.store.get(self.lock_key)   # 步骤1
        if lock_key == self.lock_value:
            await self.store.delete(self.lock_key)        # 步骤2 - 与步骤1之间有间隙
    except Exception as e:
        memory_logger.error("Error releasing lock", ...)
```

**Go 问题代码**：
```go
func (l *DistributedLock) Release(ctx context.Context) error {
    val, err := l.store.Get(ctx, l.lockKey)
    if string(val) == l.lockValue {
        if delErr := l.store.Delete(ctx, l.lockKey); delErr != nil { ... }
```

**修复方案**：长期方案：在 BaseKVStore 接口增加 `CompareAndDelete(key, expectedValue)` 原子操作（Redis 可用 Lua 脚本实现），Release 改用该原子操作。短期：标注为继承 Python 的已知 TOCTOU 问题。

---

### S-02：Release 中锁已不存在时无日志

**严重性**：S  
**文件**：`internal/agentcore/memory/common/distributed_lock.go:87-103`

**问题**：当锁 key 不存在（已被过期清理），`store.Get` 返回 `nil, nil`，`string(nil) == ""` 不等于 UUID，跳过 Delete 但不记录日志。

**修复方案**：在 `string(val) == l.lockValue` 的 else 分支添加 Warn 日志：
```go
} else {
    logger.Warn(logComponent).Str("lock_key", l.lockKey).
        Str("lock_value", l.lockValue).Str("event_type", "MEMORY_STORE").
        Msg("releasing lock skipped: lock not found or held by another owner")
}
```

---

### M-01：DistributedLock 不是并发安全的——lockValue 字段有 data race

**严重性**：M  
**文件**：`internal/agentcore/memory/common/distributed_lock.go:19-30`

**问题**：结构体无内部互斥锁。如果同一实例被多个 goroutine 并发调用 Acquire/Release，`l.lockValue` 字段会有 data race。Python 用 GIL + 单协程的 `async with` 保证安全。

**修复方案**：在文档中明确标注 `DistributedLock` 实例不可跨 goroutine 共享。从 Python 使用模式看（每次 `async with` 前新建实例），当前 Go 使用方式也是每次新建，实际不会触发 data race。

---

### M-02：Release 在 Get 失败时返回 nil，调用方无法区分

**严重性**：M  
**文件**：`internal/agentcore/memory/common/distributed_lock.go:88-93`

**问题**：Go 的 `Release` 在 `Get` 返回 error 时记录日志后返回 `nil`，与 Python 的 `try/except` 静默处理一致。但 Go 惯例应返回 error。

**修复方案**：在注释中明确说明返回 nil 并不保证锁被释放，或改为返回 error 让 WithLock 的 defer 用 `_ =` 忽略。

---

### T-01：Release 错误日志缺少 lockKey 和 lockValue 上下文字段

**文件**：`internal/agentcore/memory/common/distributed_lock.go:90-93`

**Go 问题代码**：
```go
logger.Error(logComponent).Err(err).
    Str("event_type", "MEMORY_STORE").
    Msg("释放锁失败")   // ← 缺少 lock_key, lock_value；Msg 应为英文
```

**修复方案**：补充字段，Msg 改英文：`Msg("failed to release lock")`。

---

### T-02：分布式锁测试使用 `//go:build test` 标签但内容全部可 mock

**文件**：`internal/agentcore/memory/common/distributed_lock_test.go`

**问题**：测试使用 `kv.NewInMemoryKVStore()` 作为 mock 后端，不需要外部环境。根据项目规范，可 mock 的测试不应使用 build tag 隔离。

**修复方案**：移除 `//go:build test` 标签。

---

## 7.14 Mem0Provider

### S-03：Mem0Provider.SyncTurn 错误时向上传播 error（Python 静默处理）

**严重性**：S  
**文件**：`internal/agentcore/memory/external/mem0_provider.go:266-286`

**问题**：Python 的 `sync_turn` 用 try/except 捕获所有异常并静默处理，Go 版本在 `client.add` 失败时 `return err`。这会导致 Mem0 网络抖动时错误向上传播到 ExternalMemoryRail，可能中断对话流程。

**Python 样例**：
```python
async def sync_turn(self, user_msg, assistant_msg, **kwargs):
    try:
        await self._client_call("add", messages, **self._write_filters())
        self._record_success()
    except Exception as exc:
        self._record_failure()
        logger.warning("Mem0 sync failed: %s", exc)
        # ← 不抛出异常
```

**Go 问题代码**：
```go
err := client.add(ctx, messages, p.writeFilters(), nil)
if err != nil {
    p.recordFailure()
    logger.Warn(mem0LogComponent).Err(err).Msg("Mem0 sync_turn 失败")
    return err  // ← 错误向上传播！
}
```

**修复方案**：对齐 Python，失败时返回 nil：
```go
if err != nil {
    p.recordFailure()
    logger.Warn(mem0LogComponent).Err(err).Msg("Mem0 sync_turn failed")
    return nil
}
```

---

### S-04：Mem0Provider.Prefetch/QueuePrefetch 不接受 top_k kwargs

**严重性**：S  
**文件**：`internal/agentcore/memory/external/mem0_provider.go:193-227,232-262`

**问题**：Python 的 `prefetch` 和 `queue_prefetch` 接受 `top_k` kwargs 并 min(top_k, 50)，Go 硬编码 `topK := 5`，不读取 ProviderOptions 中的值。`Initialize` 也缺少 `api_key`/`rerank` kwargs 覆盖。

**Python 样例**：
```python
async def prefetch(self, query, **kwargs):
    top_k = min(int(kwargs.get("top_k", 5)), 50)
    rerank = bool(kwargs.get("rerank", self._rerank))

async def initialize(self, **kwargs):
    self._api_key = kwargs.get("api_key") or self._api_key
    if "rerank" in kwargs:
        self._rerank = bool(kwargs["rerank"])
```

**修复方案**：扩展 `ProviderOptions` 增加 TopK/Rerank/APIKey 字段，在 Prefetch/QueuePrefetch/Initialize 中读取。

---

### M-03：Mem0Provider.handleSearch 返回原始 response 对象而非精简 payload

**严重性**：M  
**文件**：`internal/agentcore/memory/external/mem0_provider.go:480-484`

**问题**：Python 对 mem0_search 返回精简 payload：`[{"memory": ..., "score": ...}]`，Go 直接返回原始 `mem0MemoryItem`。

**Python 样例**：
```python
payload = [{"memory": item.get("memory", ""), "score": item.get("score", 0)} for item in items]
```

**Go 问题代码**：
```go
"results": response,  // ← 原始 mem0MemoryItem
```

**修复方案**：构造精简 payload 列表，只含 memory 和 score 字段。

---

### T-03：Mem0Provider 中文日志 Msg 违反英文规则

**文件**：`internal/agentcore/memory/external/mem0_provider.go`

**问题**：`"Mem0 prefetch 失败"`, `"Mem0 sync_turn 失败"`, `"Mem0 queue_prefetch 失败"`, `"[Mem0Provider] 熔断器开启"` 均为中文 Msg。

**修复方案**：Msg 改英文：`"Mem0 prefetch failed"`, `"Mem0 sync_turn failed"`, `"Mem0 queue_prefetch failed"`, `"circuit breaker opened"`。

---

## 7.15 OpenVikingProvider

### S-05：OpenVikingProvider.Initialize 健康检查失败不返回 error

**严重性**：S  
**文件**：`internal/agentcore/memory/external/viking_provider.go:235-260`

**问题**：健康检查失败时 `return nil`（不返回 error），调用方无法知道初始化失败。Python 同样不抛出异常（静默处理），但 Go 应提供区分手段。

**Python 样例**：
```python
async def initialize(self, **kwargs):
    try:
        self._client = _VikingClient(...)
        healthy = await asyncio.to_thread(self._client.health)
        if not healthy:
            logger.warning("OpenViking at %s not reachable", self._endpoint)
            self._client = None
    except ImportError:
        logger.warning("httpx not installed — OpenViking disabled")
        self._client = None
```

**Go 问题代码**：
```go
if !healthy {
    p.client = nil
    p.initialized = false
    return nil  // ← 调用方不知道初始化失败
}
```

**修复方案**：返回 sentinel error 让调用方区分：
```go
return fmt.Errorf("OpenViking at %s not reachable", p.endpoint)
```

---

### S-06：OpenVikingProvider.handleVikingSearch 不读取 top_k 参数

**严重性**：S
**文件**：`internal/agentcore/memory/external/viking_provider.go:444-466`

**问题**：工具 Schema 声明 `top_k` 参数，但 handleVikingSearch 只读取 `limit`（映射到 top_k），不读取 `top_k`。且无默认值 10。注意：Python 也只读取 `limit`（`if args.get("limit"): payload["top_k"] = args["limit"]`），Schema 与代码不一致是继承 Python 的已知问题。Go 至少应添加默认值 10。

**Python 样例**：
```python
if args.get("limit"):
    payload["top_k"] = args["limit"]
# ← 同样不读取 args["top_k"]，也无默认值
```

**Go 问题代码**：
```go
if limit := floatVal(args["limit"]); limit > 0 {
    payload["top_k"] = int(limit)  // ← 不读取 args["top_k"]，无默认值
}
```

**修复方案**：添加默认值 10，并同步读取 `top_k` 参数（比 Python 更完整）：
```go
topK := 10
if v := floatVal(args["top_k"]); v > 0 { topK = int(v) }
else if v := floatVal(args["limit"]); v > 0 { topK = int(v) }
payload["top_k"] = topK
```

---

### M-04：OpenVikingProvider.SyncTurn 两条消息非原子

**严重性**：M  
**文件**：`internal/agentcore/memory/external/viking_provider.go:331-353`

**问题**：user 消息成功但 assistant 消息失败时，会话数据不完整。Python 有同样问题。

**修复方案**：标注为继承的已知设计问题。

---

### M-05：OpenVikingProvider.handleVikingSearch 缺少 top_k 默认值

**严重性**：M  

**修复方案**：当 args 中无 top_k/limit 时，显式设置默认值 10。

---

### T-04：OpenVikingProvider 中文日志 Msg 违反英文规则

**文件**：`internal/agentcore/memory/external/viking_provider.go`

**问题**：`"OpenViking 不可达"`, `"OpenViking prefetch 失败"`, `"OpenViking sync_turn 失败"`, `"OpenViking session commit 失败"` 均为中文 Msg。

**修复方案**：Msg 改英文。

---

## 9.61 RecoveryManager + metadata

### S-07：RecoverFromSession 完全未实现

**严重性**：S  
**文件**：`internal/agent_teams/agent/team_agent.go:793-795`

**问题**：从 session checkpoint 重建 Leader TeamAgent 的关键方法，直接返回 error。Python 完整实现：read_team_namespace → validate spec → model_validate → resolve agent_spec → create card → NewTeamAgent → configure → restore_allocator_state → set_session_id。

**Python 样例**：
```python
@classmethod
def recover_from_session(cls, session, team_name, runtime_spec=None):
    bucket = read_team_namespace(session, team_name)
    if bucket is None:
        raise ValueError(f"No persisted state for team '{team_name}'")
    spec = TeamAgentSpec.model_validate(bucket["spec"])
    context = TeamRuntimeContext.model_validate(bucket["context"])
    agent = cls(card)
    agent.configure(spec, context)
    allocator_state = bucket.get("model_allocator_state")
    if allocator_state:
        agent.restore_allocator_state(allocator_state)
    return agent
```

**Go 问题代码**：
```go
func RecoverFromSession(ctx context.Context, session any, teamName string, ...) (*TeamAgent, error) {
    return nil, fmt.Errorf("RecoverFromSession 尚未实现（#9.55）")
}
```

**修复方案**：实现完整恢复流程。metadata 子包已提供 `ReadTeamNamespace`。步骤：1. ReadTeamNamespace 2. 验证 spec_data 3. json.Unmarshal 4. 构造 card 5. NewTeamAgent + Configure 6. RestoreAllocatorState 7. 注入 session_id

---

### S-08：FromSpawnPayload 完全未实现

**严重性**：S  
**文件**：`internal/agent_teams/agent/team_agent.go:751-754`

**问题**：teammate 进程创建的关键方法，返回 `nil, nil`。Python 完整实现。

**Python 样例**：
```python
@classmethod
async def from_spawn_payload(cls, payload):
    spec = TeamAgentSpec.model_validate(payload["spec"])
    context = TeamRuntimeContext.model_validate(payload["context"])
    agent = cls(card)
    agent.configure(spec, context)
    if backend is not None:
        await backend.refresh_human_agent_roster()
    return agent
```

**修复方案**：实现完整流程。

---

### S-09：RecoverForExistingSession 缺少 _stop_coordination 调用

**严重性**：S  
**文件**：`internal/agent_teams/agent/team_agent.go:772-777`

**Python 样例**：
```python
async def recover_for_existing_session(self, session):
    await self._stop_coordination()
    await self._session_manager.recover_for_existing_session(session)
```

**Go 问题代码**：
```go
func (a *TeamAgent) RecoverForExistingSession(ctx context.Context, session any) (context.Context, error) {
    if a.sessionManager != nil {
        return a.sessionManager.RecoverForExistingSession(ctx, session)
    }
    return ctx, nil  // ← 缺少 StopCoordination
}
```

**修复方案**：在委托前添加 `a.StopCoordination(ctx)` 调用。

---

### S-10：DestroyTeam 缺少 _stop_coordination、_remove_self_from_pool、force_clean_team

**严重性**：S  
**文件**：`internal/agent_teams/agent/team_agent.go:669-676`

**Python 样例**：
```python
async def destroy_team(self, force=True):
    session_id_snapshot = self.session_id
    try: await self.cancel_agent()
    try: await self._stop_coordination()
    await self._remove_self_from_pool(session_id_snapshot)
    return await self._configurator.team_backend.force_clean_team(shutdown_members=force)
```

**Go 问题代码**：
```go
func (a *TeamAgent) DestroyTeam(ctx context.Context, force bool) (bool, error) {
    if a.streamController != nil { _ = a.streamController.CancelAgent(ctx) }
    // ⤵️(#9.62+#9.58): 缺少 3 个关键步骤
    return false, nil
}
```

**修复方案**：实现完整流程：cancel → stop_coordination → remove_self_from_pool → force_clean_team。

---

### S-11：ShutdownSelf 缺少 _close_stream 调用

**严重性**：S  
**文件**：`internal/agent_teams/agent/team_agent.go:642-652`

**Python 样例**：
```python
async def shutdown_self(self):
    await self._stream_controller.cooperative_cancel()
    if self._state.team_member is not None:
        await self._state.team_member.update_status(MemberStatus.SHUTDOWN)
    self._close_stream()  # ← Go 缺少
```

**修复方案**：添加 closeStream 调用。

---

### S-12：updateExecution 是空操作——执行状态从不持久化

**严重性**：S  
**文件**：`internal/agent_teams/agent/team_agent.go:863-868`

**Python 样例**：
```python
async def _update_execution(self, status):
    if self._state.team_member:
        await self._state.team_member.update_execution_status(status)
```

**Go 问题代码**：
```go
func (a *TeamAgent) updateExecution(ctx context.Context, status atschema.ExecutionStatus) error {
    // ⤵️ 待 9.55 完善后实现
    logger.Debug(logComponent)...
    return nil
}
```

**修复方案**：委托 `a.state.TeamMember.UpdateExecutionStatus(ctx, status)`。

---

### S-13：PersistAllocatorState on TeamAgent 是空 stub

**严重性**：S  
**文件**：`internal/agent_teams/agent/team_agent.go:384-386`

**问题**：底层 `RecoveryManager.PersistAllocatorState` 已实现，只需接线。

**修复方案**：
```go
func (a *TeamAgent) PersistAllocatorState() {
    if a.recoveryManager != nil && a.sessionManager != nil {
        if sf, ok := a.sessionManager.TeamSession().(sessinterfaces.SessionFacade); ok {
            a.recoveryManager.PersistAllocatorState(sf)
        }
    }
}
```

---

### S-14：context.Background() 在 SpawnManager 和 AgentConfigurator 中泛滥

**严重性**：S  
**文件**：`internal/agent_teams/agent/spawn_manager.go`, `agent_configurator.go`

**问题**：`CleanupTeammate(context.Background(), ...)`, `UpdateMemberStatus(context.Background(), ...)`, `teammate.Configure(context.Background(), ...)`, `WorkspaceManager().Initialize(context.Background())` 等均丢弃调用方上下文。

**修复方案**：传播实际 `ctx` 参数。对健康检查 goroutine，从适当父级派生 context。

---

### M-06：AutoStartMember 和 AutoStartAll 是空 stub

**严重性**：M  
**文件**：`internal/agent_teams/agent/team_agent.go:710-720`

**Python 样例**：
```python
async def auto_start_member(self, member_name):
    started = await backend.startup_member(member_name, on_created=self._on_teammate_created)
    if started:
        team_logger.info("Auto-started member via interact: {}", member_name)
    return started
```

**修复方案**：实现调用 `backend.StartupMember` 和 `backend.Startup`。

---

### M-07：UpdateModelPool 缺少 role/spec nil 检查

**严重性**：M  
**文件**：`internal/agent_teams/agent/team_agent.go:809-819`

**Python 样例**：
```python
def update_model_pool(self, new_pool):
    if self._configurator.spec is None or self.role != TeamRole.LEADER:
        return
    team_session = self._session_manager.team_session
    if team_session is None:
        return
    self._recovery_manager.persist_leader_config(team_session)
```

**修复方案**：添加 role 和 spec nil 检查。

---

### M-08：DeliverInput 缺少排队日志

**严重性**：M  
**文件**：`internal/agent_teams/agent/team_agent.go:578-579`

**Python 样例**：
```python
team_logger.info("[{}] queueing input for next round (transition window): {:.60}", ...)
```

**修复方案**：添加 Info 日志。

---

### M-09：LookupHumanAgentRuntime 缺少 is_human_agent 守卫

**严重性**：M  
**文件**：`internal/agent_teams/agent/team_agent.go:409-423`

**Python 样例**：
```python
def lookup_human_agent_runtime(self, member_name):
    if backend is None or not backend.is_human_agent(member_name):
        return None
    return self._spawn_manager.lookup_inprocess_agent(member_name)
```

**修复方案**：添加 `backend.IsHumanAgent(memberName)` 检查。

---

### M-10：metadata.writeTeamNamespace 不拷贝 payload（Python 拷贝）

**严重性**：M  
**文件**：`internal/agent_teams/metadata/metadata.go:98-102`

**问题**：Python `write_team_namespace` 做 `teams[team_name] = dict(payload)` 拷贝 payload。Go 直接赋值 `teams[teamName] = payload`，调用方后续修改 payload 会影响已存储的状态。

**Python 样例**：
```python
teams[team_name] = dict(payload)
```

**Go 问题代码**：
```go
teams[teamName] = payload  // ← 无拷贝
```

**修复方案**：`teams[teamName] = copyMap(payload)`。

---

### M-11：RecoverTeam.UpdateMemberStatus 忽略 error 返回

**严重性**：M  
**文件**：`internal/agent_teams/agent/recovery_manager.go:100-103`

**问题**：`UpdateMemberStatus` 的返回值被丢弃，如果更新失败应记录日志。

**修复方案**：捕获 error 并记录 Warn 日志。

---

### T-05：ReleaseSession 日志缺少 session_id

**文件**：`internal/agent_teams/agent/session_manager.go:169-171`

**修复方案**：在清空前记录 session_id。

---

### T-06：PersistLeaderConfig 使用 json.Marshal→json.Unmarshal round-trip

**文件**：`internal/agent_teams/agent/recovery_manager.go:130-136`

**修复方案**：考虑添加 `ToMap()` 方法。当前功能正确。

---

## 9.67 Team Observability

### S-15：ObservabilityRail.BeforeTaskIteration 使用 context.Background() 创建 span

**严重性**：S  
**文件**：`internal/agent_teams/observability/rail.go:72`

**问题**：`r.tracer().Start(context.Background(), ...)` 导致 span 与父级上下文断开，无法形成正确的 trace 链。

**Go 问题代码**：
```go
_, span := r.tracer().Start(context.Background(),
    fmt.Sprintf("deepagent.task_iteration.%d", iteration),
    trace.WithSpanKind(trace.SpanKindInternal),
)
```

**修复方案**：使用传入的 ctx：
```go
_, span := r.tracer().Start(ctx, ...)
```

---

### S-16：OtelTeamMonitorHandler 所有 span 使用 context.Background() 创建

**严重性**：S  
**文件**：`internal/agent_teams/observability/monitor_handler.go:136,180`

**问题**：team span 和 task span 之间无 parent 关系，所有 span 都成孤立根 span。

**修复方案**：task span 创建时以 team span 为 parent：
```go
func (h *OtelTeamMonitorHandler) openTaskSpan(teamName string, payload map[string]any) {
    teamSpan := h.teamSpans[teamName]
    var ctx context.Context = context.Background()
    if teamSpan != nil {
        ctx = trace.ContextWithSpan(context.Background(), teamSpan)
    }
    _, span := h.tracer().Start(ctx, "task."+taskID, ...)
```

---

### S-17：AttachToTeamAgent 和 DetachFromTeamAgent 是空桩

**严重性**：S
**文件**：`internal/agent_teams/observability/setup.go:194-214`

**问题**：Monitor 层完全不可用，无法将 MonitorHandler 注册到 TeamAgent 事件流。

**Python 样例**：
```python
def attach_to_team_agent(team_agent):
    team_agent.add_event_listener(_monitor_handler)
```

**修复方案**：当 TeamAgent 实现 EventListenerRegistrar 后，调用 `teamAgent.AddEventListener(monitorHandler)`。

---

### S-27：OtelTeamMonitorHandler.teamSpans/taskSpans map 无并发保护

**严重性**：S
**文件**：`internal/agent_teams/observability/monitor_handler.go:62-64`

**问题**：`teamSpans` 和 `taskSpans` 是 `map[string]trace.Span`，没有任何互斥锁保护。如果 TeamAgent 事件从多个 goroutine 到达（Python 通过 asyncio 单线程保证安全），并发读写 map 会导致 Go runtime fatal error: concurrent map read and map write。

**Python 样例**：
```python
# Python asyncio 保证单线程，map 读写天然安全
self._team_spans: dict[str, Span] = {}
self._task_spans: dict[str, Span] = {}
```

**Go 问题代码**：
```go
type OtelTeamMonitorHandler struct {
    teamSpans map[string]trace.Span  // ← 无 mutex
    taskSpans map[string]trace.Span  // ← 无 mutex
}
```

**修复方案**：添加 `sync.RWMutex` 保护 map 读写：
```go
type OtelTeamMonitorHandler struct {
    mu        sync.RWMutex
    teamSpans map[string]trace.Span
    taskSpans map[string]trace.Span
}
// 所有读写方法中加锁：h.mu.Lock() / h.mu.RLock()
```

---

### S-28：ObservabilityRail 缺少 Priority 字段（Python priority=10）

**严重性**：S
**文件**：`internal/agent_teams/observability/rail.go:34-38`

**问题**：Python 的 `ObservabilityRail` 声明 `priority: int = 10`（低优先级，确保可观测性 rail 最后执行）。Go 的 `ObservabilityRail` 嵌入 `DeepAgentRail` 但不覆盖 `Priority()` 方法，导致使用 DeepAgentRail 默认值 50，与 Python 不一致。Rail 调度顺序错误。

**Python 样例**：
```python
class ObservabilityRail(DeepAgentRail):
    priority: int = 10  # Low priority: observability runs last.
```

**Go 问题代码**：
```go
type ObservabilityRail struct {
    rails.DeepAgentRail
    // ← 缺少 Priority() 方法
}
// DeepAgentRail.Priority() 返回 50
```

**修复方案**：在 `NewObservabilityRail` 中调用 `r.WithPriority(10)` 或添加 `Priority() int` 方法。

---

### S-29：OpenVikingProvider.IsInitialized 使用独立 bool 而非 `p.client != nil`

**严重性**：S
**文件**：`internal/agentcore/memory/external/viking_provider.go:38-39,206-208`

**问题**：Python 的 `is_initialized` 属性返回 `self._client is not None`，直接反映 client 状态。Go 使用独立的 `initialized` bool 字段，在 `Shutdown` 中需要同时清理 `p.client = nil` 和 `p.initialized = false`。如果只清理其中一个（如忘记清理 `initialized`），状态不一致。实际上 `Initialize` 健康检查失败时已经设置 `p.client = nil` + `p.initialized = false`，但 `Shutdown` 中如果 `p.client.close()` panic 被捕获，后续 `p.client = nil` 可能不执行，导致 `initialized` 仍为 true。

**Python 样例**：
```python
@property
def is_initialized(self) -> bool:
    return self._client is not None
```

**Go 问题代码**：
```go
func (p *OpenVikingProvider) IsInitialized() bool {
    return p.initialized  // ← 独立 bool，与 client 状态可能不一致
}
```

**修复方案**：改为 `return p.client != nil`，删除 `initialized` 字段。

---

### M-12：closeLlmSpan 中 reasoning span 的 context 使用 context.Background()

**严重性**：M  
**文件**：`internal/agent_teams/observability/callback_handler.go:385-386`

**Go 问题代码**：
```go
reasoningCtx := trace.ContextWithSpan(context.Background(), state.Span)
```

**修复方案**：改为 `trace.ContextWithSpan(ctx, state.Span)` 保留 ctx 中的其他信息。

---

### M-13：otlp_http exporter 未实现

**严重性**：M
**文件**：`internal/agent_teams/observability/setup.go:266`

**Python 样例**：
```python
if config.exporter == "otlp_http":
    from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
        OTLPSpanExporter as HttpExporter,
    )
    return HttpExporter(endpoint=config.endpoint)
```

**修复方案**：使用 `otlptracehttp` 包实现。

---

### M-23：hashValue 十六进制格式不零填充，与 Python hexdigest 不匹配

**严重性**：M
**文件**：`internal/agent_teams/observability/redaction.go:53-56`

**问题**：Python `hashlib.sha256(...).digest()[:8].hex()` 产生 16 个十六进制字符（每字节 2 字符，零填充）。Go `fmt.Sprintf("%x", digest[:8])` 对前导零字节不零填充。例如 digest `[0x00, 0x01, ...]` → Python 输出 `0001...`，Go 输出 `1...`。导致 hash 值长度不一致，跨语言比对时匹配失败。

**Python 样例**：
```python
import hashlib
digest = hashlib.sha256(value.encode()).digest()[:8]
return "sha256:" + digest.hex()  # hex() 每字节零填充为 2 字符
```

**Go 问题代码**：
```go
func hashValue(value string) string {
    digest := sha256.Sum256([]byte(value))
    return fmt.Sprintf("%s%x", redactedPrefix, digest[:8])  // %x 不零填充
}
```

**修复方案**：使用 `%x` 替换为 `hex.EncodeToString(digest[:8])` 或 `fmt.Sprintf("%x", ...)` 改为逐字节格式化：
```go
return redactedPrefix + hex.EncodeToString(digest[:8])
```

---

### M-24：truncStr 按 byte 截断而非 rune 截断（CJK 文本截断乱码）

**严重性**：M
**文件**：`internal/agentcore/memory/external/viking_provider.go:748-753`

**问题**：`truncStr(s, 4000)` 使用 `len(s)` 和 `s[:maxLen]` 按字节截断。中文等 CJK 字符占 3 字节，截断位置可能落在 UTF-8 多字节序列中间，产生无效 UTF-8。

**Python 样例**：
```python
user_msg[:4000]  # Python str 按 Unicode code point 截断，不会产生乱码
```

**Go 问题代码**：
```go
func truncStr(s string, maxLen int) string {
    if len(s) <= maxLen { return s }
    return s[:maxLen]  // ← 可能截断在 UTF-8 多字节中间
}
```

**修复方案**：
```go
func truncStr(s string, maxLen int) string {
    if utf8.RuneCountInString(s) <= maxLen { return s }
    runes := []rune(s)
    return string(runes[:maxLen])
}
```
注意：如果确实需要对齐 Python `str[:N]` 语义（按 code point），必须用 rune 截断。

---

### M-25：truncate 按 byte 截断而非 rune 截断（同 M-24）

**严重性**：M
**文件**：`internal/agent_teams/observability/redaction.go:44-49`

**问题**：与 M-24 相同的问题。`truncate` 使用 `len(value)` 按 byte 长度截断。

**Go 问题代码**：
```go
func truncate(value string, maxLength int) string {
    if maxLength <= 0 || len(value) <= maxLength { return value }
    return value[:maxLength] + fmt.Sprintf("...<truncated %d chars>", len(value)-maxLength)
}
```

**修复方案**：改用 rune 截断。注意 `truncated N chars` 的计数也需改为 rune 数。

---

### M-26：roundTo3 对负数结果不正确

**严重性**：M
**文件**：`internal/agentcore/memory/external/viking_provider.go:777-779`

**问题**：`roundTo3(f)` 使用 `float64(int(f*1000+0.5)) / 1000`。当 f 为负数时（如 -0.1234），`int(f*1000+0.5)` = `int(-123.4+0.5)` = `int(-122.9)` = `-122`，结果为 -0.122 而非 -0.123。Python `round()` 使用银行家舍入。

**Python 样例**：
```python
round(-0.1234, 3)  # → -0.123
```

**Go 问题代码**：
```go
func roundTo3(f float64) float64 {
    return float64(int(f*1000+0.5)) / 1000  // ← 负数时方向错误
}
```

**修复方案**：使用 `math.Round`：
```go
import "math"
func roundTo3(f float64) float64 {
    return math.Round(f*1000) / 1000
}
```

---

### M-27：RedactCompletion 用 fmt.Sprintf("%v") 对 map 序列化而非 json.Marshal

**严重性**：M
**文件**：`internal/agent_teams/observability/redaction.go:29-33`

**问题**：当 `value` 为 `map[string]any` 类型时，`fmt.Sprintf("%v", value)` 输出 Go 原生 map 格式 `map[key1:val1 key2:val2]`，而非 JSON。Python 对 completion 用 `str()` 转换，如果值是 dict 则输出 Python dict 格式。两者都无法被下游 JSON parser 解析。但 Go 的 `%v` 格式更难读。

**Go 问题代码**：
```go
func RedactCompletion(value any, config *ObservabilityConfig) string {
    text := ""
    if value != nil {
        text = fmt.Sprintf("%v", value)  // ← map 输出 Go 原生格式
    }
```

**修复方案**：对 map/slice 类型使用 `json.Marshal`，对 string 类型直接用：
```go
func RedactCompletion(value any, config *ObservabilityConfig) string {
    text := coerceToString(value)
    ...
}
func coerceToString(v any) string {
    switch val := v.(type) {
    case string: return val
    default:
        b, err := json.Marshal(v)
        if err != nil { return fmt.Sprintf("%v", v) }
        return string(b)
    }
}
```

---

### M-28：ShutdownObservability 缺少未关闭 span 的清理

**严重性**：M
**文件**：`internal/agent_teams/observability/setup.go:156-177`

**问题**：`ShutdownObservability` 在调用 `tp.Shutdown()` 前，如果 `monitorHandler.teamSpans` 或 `monitorHandler.taskSpans` 中有未关闭的 span，这些 span 不会被 End()。Python 同样没有显式清理（依赖 provider.Shutdown 冲刷），但 Go 的 BatchSpanProcessor 在 Shutdown 时只导出已 End 的 span，未 End 的 span 被静默丢弃。

**修复方案**：在 `tp.Shutdown()` 前遍历 teamSpans/taskSpans，对每个 span 调用 `span.End()`：
```go
if monitorHandler != nil {
    for _, span := range monitorHandler.teamSpans { span.End() }
    for _, span := range monitorHandler.taskSpans { span.End() }
}
```

---

### T-07：HandleEvent ctx 参数完全未使用

**文件**：`internal/agent_teams/observability/monitor_handler.go:83`

**修复方案**：所有 `Start(context.Background(), ...)` 改为 `Start(ctx, ...)`。

---

### T-08：缺少 on_agent_call_error 回调

**文件**：`internal/agent_teams/observability/callback_handler.go`

**问题**：Python 注册了 `on_agent_call_error`（关闭 agent span 并标记错误），Go 没有。

**修复方案**：添加 `OnAgentCallError` 方法并在 `wireCallbackHandlers` 中注册（需确认 CallbackFramework 是否有此事件类型）。

---

### T-16：otel.SetTracerProvider 无保护，可被重复覆盖

**文件**：`internal/agent_teams/observability/setup.go:136`

**问题**：`otel.SetTracerProvider(tp)` 无条件设置全局 TracerProvider。如果其他库（如 SDK 自动初始化）之前已设置，此处会静默覆盖。Python 也有同样行为，但 Go 生态中全局 TracerProvider 通常只设置一次。

**修复方案**：记录 Warn 日志当 `otel.GetTracerProvider()` 不为默认 provider 时。

---

## 9.68 Team Rails

### S-18：TeamPolicyRail.BeforeModelCall 使用 context.Background() 刷新动态缓存

**严重性**：S  
**文件**：`internal/agent_teams/rails/team_policy_rail.go:148-163`

**问题**：方法签名有 `_ context.Context` 但用 `context.Background()` 刷新缓存，DB 查询无法被取消。

**Go 问题代码**：
```go
if r.infoCache != nil {
    ctx := context.Background()       // ← 忽略方法参数的 ctx
    infoSection := r.infoCache.Refresh(ctx)
```

**修复方案**：使用传入的 ctx：
```go
func (r *TeamPolicyRail) BeforeModelCall(ctx context.Context, _ *agentinterfaces.AgentCallbackContext) error {
    ...
    infoSection := r.infoCache.Refresh(ctx)
```

---

### S-19：TeamToolApprovalRail.resolveInterrupt 使用 context.Background() 发送消息

**严重性**：S  
**文件**：`internal/agent_teams/rails/tool_approval_rail.go:147`

**Go 问题代码**：
```go
messageID, err := r.messageManager.SendMessage(context.Background(), message, ...)
```

**修复方案**：resolveInterrupt 方法签名需传入 ctx，然后传播。

---

### S-20：isAutoConfirmedSimple 只处理 bool 类型，Python 用 truthy 语义

**严重性**：S  
**文件**：`internal/agent_teams/rails/tool_approval_rail.go:250-262`

**问题**：Python `dict.get(key, False)` 使用 truthy 语义（int 1, string "yes" 都算 true）。Go 的 `isAutoConfirmedSimple` 只处理 bool，其他类型一律返回 false。父类 `ConfirmInterruptRail.isAutoConfirmed` 已实现完整类型支持。

**Python 样例**：
```python
return config.get(key, False)  # truthy 语义
```

**Go 问题代码**：
```go
func isAutoConfirmedSimple(config map[string]any, key string) bool {
    if b, ok := val.(bool); ok { return b }
    return false  // 只处理 bool
}
```

**修复方案**：替换为 `interrupt.IsAutoConfirmed` 或内联完整的类型支持（bool/int/int64/float64/string）。

---

### S-21：BuildTeamHarness 中所有 AddRail 调用被注释掉

**严重性**：S  
**文件**：`internal/agent_teams/harness.go:128-133`

**问题**：所有六个 `deepAgent.AddRail()` 调用被注释掉（`// TODO(#9.56)`），意味着没有任何 rail 实际注册到 agent 上，所有生命周期钩子（Init, BeforeModelCall 等）不会触发。

**Python 样例**：
```python
deep_agent.add_rail(team_tool_rail)
deep_agent.add_rail(team_policy_rail)
deep_agent.add_rail(first_iter_gate)
# ... etc
```

**Go 问题代码**：
```go
// deepAgent.AddRail(teamToolRail)
// deepAgent.AddRail(teamPolicyRail)
// deepAgent.AddRail(firstIterGate)
// deepAgent.AddRail(teamWorkspaceRail)
// deepAgent.AddRail(toolApprovalRail)
// deepAgent.AddRail(teamPlanModeRail)
```

**修复方案**：当 deepAgent 可用后取消注释并实现调用。

---

### S-22：TeamToolRail.Init 缺少 Runner.resource_mgr.add_tool 调用

**严重性**：S  
**文件**：`internal/agent_teams/rails/team_tool_rail.go:120-132`

**问题**：Python 的 `TeamToolRail.init()` 注册到 `Runner.resource_mgr` 使 dispatcher 能解析调用。Go 只注册到 AbilityManager，跳过 resource_mgr。

**Python 样例**：
```python
try:
    Runner.resource_mgr.add_tool(tools, refresh=True)
except Exception:
    team_logger.debug("Runner.resource_mgr not available, skipping")
```

**修复方案**：添加 resource_mgr 注册。待 Runner 共享资源管理器实现后补齐。

---

### S-23：TeamToolRail.Uninit 缺少 Runner.resource_mgr.remove_tool 调用

**严重性**：S  
**文件**：`internal/agent_teams/rails/team_tool_rail.go:137-152`

**问题**：清理时只移除 AbilityManager，不清理 resource_mgr，留过时注册。

**Python 样例**：
```python
if tool_id:
    try:
        Runner.resource_mgr.remove_tool(tool_id)
    except Exception:
        team_logger.debug("Runner.resource_mgr removal failed for {}", tool_id)
```

**修复方案**：同 S-22。

---

### M-14：TeamPolicyRail.Init 不调用 DeepAgentRail.Init

**严重性**：M  
**文件**：`internal/agent_teams/rails/team_policy_rail.go:114-117`

**问题**：Python 调用 `super().init(agent)` 设置 DeepAgentRail 的 agent 引用。Go 不调用，导致嵌入的 DeepAgentRail agent 引用为空。

**Python 样例**：
```python
def init(self, agent):
    super().init(agent)
    self.system_prompt_builder = getattr(agent, "system_prompt_builder", None)
```

**修复方案**：
```go
func (r *TeamPolicyRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
    if err := r.DeepAgentRail.Init(ctx, agent); err != nil { return err }
    r.systemPromptBuilder = agent.SystemPromptBuilder()
    return nil
}
```

---

### M-15：TeamPlanModeRail.Init 不调用 DeepAgentRail.Init

**严重性**：M  
**文件**：`internal/agent_teams/rails/team_plan_mode_rail.go:67-71`

**修复方案**：同 M-14，添加 `r.DeepAgentRail.Init(ctx, agent)` 调用。

---

### M-16：TeamPlanModeRail.specializePlanAgent 是空操作

**严重性**：M  
**文件**：`internal/agent_teams/rails/team_plan_mode_rail.go:151-161`

**问题**：Python 的 `_specialize_plan_agent` 替换 plan subagent 的 system prompt 为 team.plan 版本。Go 是空操作。

**Python 样例**：
```python
def _specialize_plan_agent(self):
    deep_config = getattr(self._agent, "deep_config", None)
    applied = apply_team_plan_agent_prompt(deep_config.subagents, language=...)
    if applied:
        team_logger.info("[team.plan] specialized built-in plan_agent prompt")
```

**修复方案**：实现 `ApplyTeamPlanAgentPrompt` 并在 specializePlanAgent 中调用。

---

### M-17：TeamPlanModeRail.BeforeModelCall 缺少 plan_mode 状态检查

**严重性**：M  
**文件**：`internal/agent_teams/rails/team_plan_mode_rail.go:87-117`

**问题**：Python 先检查 `state.plan_mode.mode != "plan"` 再决定是否注入。Go 总是注入，导致非 plan 模式下也注入了不相关的 team.plan 指令。

**Python 样例**：
```python
state = self._agent.load_state(ctx.session)
if getattr(state.plan_mode, "mode", None) != "plan":
    self.system_prompt_builder.remove_section(SectionName.MODE_INSTRUCTIONS)
    return
```

**修复方案**：TODO 已标注。当 agent plan_mode 状态可访问后添加条件检查。

---

### M-18：TeamPolicyRail 缺少 sorted(human_agent_names)

**严重性**：M  
**文件**：`internal/agent_teams/rails/team_policy_rail.go:91-92`

**Python 样例**：
```python
human_names: list[str] = sorted(team_backend.human_agent_names())
```

**Go 问题代码**：
```go
humanNames = r.teamBackend.HumanAgentNames()  // 无排序
```

**修复方案**：添加 `sort.Strings(humanNames)`。

---

### M-19：TeamToolApprovalRail 构造函数仅在 db && messager 都非 nil 时创建 messageManager

**严重性**：M  
**文件**：`internal/agent_teams/rails/tool_approval_rail.go:77-79`

**问题**：Python 总是创建 TeamMessageManager（传 nil 也创建），Go 条件创建可能导致行为差异。

**修复方案**：始终创建或添加 Warn 日志。

---

### T-09：TeamToolRail 默认 teamName/memberName 与 Python 不对齐

**文件**：`internal/agent_teams/rails/team_tool_rail.go:71-73`

**问题**：Go 默认 `teamName: "default"`, `memberName: ""`。Python 默认 `member_name or "unknown"`。

**修复方案**：添加 `if r.memberName == "" { r.memberName = "unknown" }`。

---

### T-10：所有 14 个 team tool Invoke() 是 stub

**文件**：`internal/agent_teams/tools/team_tools.go`

**问题**：全部返回 `fmt.Errorf("XxxTool.Invoke 未实现")`。虽然 card 注册正常，但无法执行。

**修复方案**：逐个实现。需跟踪为独立任务。

---

## 9.69 Team Prompts

### S-24：BuildSystemPrompt 在 LoadTemplate 返回 nil 时会 panic

**严重性**：S  
**文件**：`internal/agent_teams/prompts/policy.go:107,116,127`

**问题**：`LoadTemplate(policyName, language).Content()` 在模板不存在时会 nil pointer dereference panic。对比 `sections.go` 中有 nil 检查。

**Go 问题代码**：
```go
rolePolicyText := LoadTemplate(policyName, language).Content()  // ← 无 nil 检查
workflowSection = LoadTemplate(workflowName, language).Content()  // ← 无 nil 检查
lifecycleSection = LoadTemplate(lcName, language).Content()       // ← 无 nil 检查
```

**修复方案**：添加 nil 检查：
```go
tpl := LoadTemplate(policyName, language)
if tpl == nil { return "" }
rolePolicyText := tpl.Content()
```

---

### S-25：apply_team_plan_agent_prompt 未实现

**严重性**：S  
**文件**：`internal/agent_teams/prompts/team_plan_agent.go`

**问题**：Python 导出 `apply_team_plan_agent_prompt(subagents, language)` 用于替换 plan subagent 的 system prompt。Go 没有对应函数。

**Python 样例**：
```python
def apply_team_plan_agent_prompt(subagents, *, language=None) -> bool:
    for spec in subagents:
        if spec.agent_card.name != "plan_agent":
            continue
        if spec.system_prompt not in builtin_prompts:
            return False
        spec.system_prompt = _team_plan_agent_prompt(resolved_language)
        spec.agent_card = spec.agent_card.model_copy(update={"description": ...})
        return True
    return False
```

**修复方案**：实现 `ApplyTeamPlanAgentPrompt(subagents []SubAgentConfig, language string) bool`。

---

### S-26：BuildTeamPlanModePrompt 传入空字符串——plan_mode 状态未接入

**严重性**：S  
**文件**：`internal/agent_teams/rails/team_plan_mode_rail.go:107`

**问题**：`BuildTeamPlanModePrompt(language, "", "")` 始终传入空的 enterPlanModeStatus 和 planFileInfo，导致模板总是显示"你尚未调用 enter_plan_mode"和"暂无 plan 文件"。

**Python 样例**：
```python
def build_team_plan_mode_section(*, language, agent, session):
    content = build_team_plan_mode_prompt(
        language,
        enter_plan_mode_status=_build_enter_plan_mode_status(agent, session, language),
        plan_file_info=_build_plan_file_info(agent, session, language),
    )
```

**修复方案**：从 agent/session 状态获取真实的 plan_mode 状态和 plan 文件信息。

---

### M-20：Priority 不匹配：Go 84 vs Python 85

**严重性**：M  
**文件**：`internal/agent_teams/rails/team_plan_mode_rail.go:112`

**问题**：注释写"对齐 Python TeamPlanModeRail.priority = 84"，但 Python 实际是 85。

**Python 样例**：
```python
return PromptSection(name=SectionName.MODE_INSTRUCTIONS, content=..., priority=85)
```

**Go 问题代码**：
```go
84, // priority 对齐 Python TeamPlanModeRail.priority = 84
```

**修复方案**：改为 `85`。

---

### M-21：RolePolicy 使用 string role 而非 atschema.TeamRole 类型

**严重性**：M  
**文件**：`internal/agent_teams/prompts/policy.go`

**问题**：`RolePolicy(role string, ...)` 和 `BuildSystemPrompt(role string, ...)` 用 string，而 `BuildTeamRoleSection` 用 `atschema.TeamRole`，不一致。

**修复方案**：改为 `RolePolicy(role atschema.TeamRole, language string) string`。

---

### M-22：MtimeSectionCache.Invalidate 不重置 cached/cachedMtime

**严重性**：M  
**文件**：`internal/agent_teams/prompts/section_cache.go:72-76`

**Python 样例**：
```python
def invalidate(self):
    self._cached_section = None
    self._cached_mtime = 0
    self._initialized = False
```

**Go 问题代码**：
```go
func (c *MtimeSectionCache) Invalidate() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.initialized = false  // ← 不重置 cached/cachedMtime
}
```

**修复方案**：添加 `c.cached = nil` 和 `c.cachedMtime = 0`。

---

### T-11：LoadTemplate 模板未找到时无日志

**文件**：`internal/agent_teams/prompts/loader.go`

**修复方案**：添加 Warn 日志。

---

### T-12：BuildTeamRoleSection 回退文本硬编码中文

**文件**：`internal/agent_teams/prompts/sections.go:145`

**问题**：`"（策略模板加载失败）"` 在英文模式下也显示中文。

**修复方案**：根据 language 返回双语回退文本。

---

### T-13：模板文件 19 个 md 内容与 Python 完全一致

**文件**：`internal/agent_teams/prompts/cn/*.md`, `en/*.md`

**确认**：所有模板文件字节级别一致，无差异。✅

---

## 按严重性排序

### P0 严重问题（必须修复）

| ID | 章节 | 问题 | 影响 |
|----|------|------|------|
| S-21 | 9.68 | BuildTeamHarness 所有 AddRail 被注释 | 无 rail 实际注册 |
| S-07 | 9.61 | RecoverFromSession 完全未实现 | 冷启动恢复断路 |
| S-08 | 9.61 | FromSpawnPayload 完全未实现 | teammate 创建断路 |
| S-24 | 9.69 | BuildSystemPrompt nil panic | 模板缺失时崩溃 |
| S-27 | 9.67 | MonitorHandler map 无并发保护 | runtime fatal error |
| S-03 | 7.14 | SyncTurn 错误时向上传播 | Mem0 抖动中断对话 |
| S-09 | 9.61 | RecoverForExistingSession 缺 stop_coordination | stale handler |
| S-10 | 9.61 | ShutdownSelf 缺 close_stream | stream 泄漏 |
| S-14 | 9.61 | context.Background() 泛滥 spawn 路径 | 上下文传播断裂 |
| S-11 | 9.61 | DestroyTeam 缺少 3 个关键步骤 | 团队拆卸不完整 |
| S-12 | 9.61 | updateExecution 空操作 | 执行状态不持久化 |
| S-13 | 9.61 | PersistAllocatorState 空 stub | allocator 状态丢失 |
| S-15 | 9.67 | ObservabilityRail span 用 context.Background() | trace 链断裂 |
| S-16 | 9.67 | MonitorHandler span 无 parent 关系 | span 孤立 |
| S-17 | 9.67 | AttachToTeamAgent 空桩 | Monitor 层不可用 |
| S-28 | 9.67 | ObservabilityRail 缺 Priority=10 | Rail 调度顺序错误 |
| S-18 | 9.68 | TeamPolicyRail.BeforeModelCall 用 context.Background() | 缓存刷新无法取消 |
| S-19 | 9.68 | TeamToolApprovalRail 用 context.Background() 发消息 | 消息丢失超时控制 |
| S-20 | 9.68 | isAutoConfirmedSimple 只处理 bool | 非布尔 truthy 值被拒绝 |
| S-22 | 9.68 | TeamToolRail.Init 缺 resource_mgr 注册 | 工具调用可能失败 |
| S-23 | 9.68 | TeamToolRail.Uninit 缺 resource_mgr 清理 | 过时注册残留 |
| S-25 | 9.69 | apply_team_plan_agent_prompt 未实现 | plan subagent 未特化 |
| S-26 | 9.69 | BuildTeamPlanModePrompt 传空字符串 | plan 状态信息丢失 |
| S-29 | 7.15 | IsInitialized 用独立 bool 而非 client!=nil | 状态可能不一致 |
| S-01 | 7.25 | Release Get+Delete 非原子（TOCTOU） | 继承 Python 已知缺陷 |
| S-02 | 7.25 | Release 锁不存在时无日志 | 排查困难 |
| S-04 | 7.14 | Prefetch/QueuePrefetch 不接受 top_k | 行为与 Python 不一致 |
| S-05 | 7.15 | Initialize 健康检查失败不返回 error | 调用方无法区分 |
| S-06 | 7.15 | viking_search 不读取 top_k 参数 | 工具参数丢失（继承 Python） |

### P1 一般问题

| ID | 章节 | 问题 |
|----|------|------|
| M-01 | 7.25 | DistributedLock 非并发安全 |
| M-02 | 7.25 | Release 吞掉 error |
| M-03 | 7.14 | handleSearch 返回原始对象 |
| M-04 | 7.15 | SyncTurn 两条消息非原子 |
| M-05 | 7.15 | handleVikingSearch 缺 top_k 默认值 |
| M-06 | 9.61 | AutoStartMember/AutoStartAll 空 stub |
| M-07 | 9.61 | UpdateModelPool 缺 role/spec 检查 |
| M-08 | 9.61 | DeliverInput 缺排队日志 |
| M-09 | 9.61 | LookupHumanAgentRuntime 缺守卫 |
| M-10 | 9.61 | writeTeamNamespace 不拷贝 payload |
| M-11 | 9.61 | RecoverTeam.UpdateMemberStatus 忽略 error |
| M-12 | 9.67 | reasoning span context 用 Background |
| M-13 | 9.67 | otlp_http exporter 未实现 |
| M-23 | 9.67 | hashValue 十六进制不零填充，与 Python hexdigest 不匹配 |
| M-24 | 7.15 | truncStr 按 byte 截断而非 rune（CJK 乱码） |
| M-25 | 9.67 | truncate 按 byte 截断而非 rune（CJK 乱码） |
| M-26 | 7.15 | roundTo3 对负数结果不正确 |
| M-27 | 9.67 | RedactCompletion 用 %v 而非 json.Marshal 序列化 map |
| M-28 | 9.67 | ShutdownObservability 缺少未关闭 span 清理 |
| M-14 | 9.68 | TeamPolicyRail.Init 不调 DeepAgentRail.Init |
| M-15 | 9.68 | TeamPlanModeRail.Init 不调 DeepAgentRail.Init |
| M-16 | 9.68 | specializePlanAgent 空操作 |
| M-17 | 9.68 | TeamPlanModeRail 缺 plan_mode 检查 |
| M-18 | 9.68 | TeamPolicyRail 缺 sorted(human_agent_names) |
| M-19 | 9.68 | TeamToolApprovalRail 条件创建 messageManager |
| M-20 | 9.69 | Priority 不匹配 Go 84 vs Python 85 |
| M-21 | 9.69 | RolePolicy 用 string 而非 TeamRole |
| M-22 | 9.69 | MtimeSectionCache.Invalidate 不重置 cached |

### P2 提示

| ID | 章节 | 问题 |
|----|------|------|
| T-01 | 7.25 | Release 错误日志缺字段 + Msg 中文 |
| T-02 | 7.25 | 测试文件用 //go:build test 标签 |
| T-03 | 7.14 | 中文 Msg 违反英文规则 |
| T-04 | 7.15 | 中文 Msg 违反英文规则 |
| T-05 | 9.61 | ReleaseSession 日志缺 session_id |
| T-06 | 9.61 | PersistLeaderConfig json round-trip |
| T-07 | 9.67 | HandleEvent ctx 未使用 |
| T-08 | 9.67 | 缺 on_agent_call_error 回调 |
| T-16 | 9.67 | otel.SetTracerProvider 无保护，可被重复覆盖 |
| T-09 | 9.68 | teamName/memberName 默认值与 Python 不对齐 |
| T-10 | 9.68 | 14 个 team tool Invoke 全是 stub |
| T-11 | 9.69 | LoadTemplate 无日志 |
| T-12 | 9.69 | 回退文本硬编码中文 |
| T-13 | 9.69 | 模板文件已确认一致 ✅ |
| T-14 | 9.69 | 重复 workflowTemplates 变量 |
| T-15 | 9.69 | workspace_meta 在 SharedToolsStr 但未创建 |
