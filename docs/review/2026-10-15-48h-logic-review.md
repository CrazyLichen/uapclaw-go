# 48h 逻辑审查 — 2026-10-15

> 审查范围：最近完成的关键实现章节，对照 Python 参考项目深度对比
> - 9.85 TeamRunner（runtime/dispatch + team_runner + manager Activate 内联回填）
> - 10.3.7-11 适配器辅助（deep_adapter_team + team_helpers + CodeAgentRail）
> - 13.2 ChromaIndexer（retrieval/indexing 完整索引管道）
> - 13.4 ComputeChunkEmbeddings
> - agent_teams 核心模块（TeamAgent/CoordinationKernel/SpawnManager）
> - swarm 层（TeamManager/Rails/Session/Gateway）
> - agentcore 层（context_evolver/controller/single_agent/sys_operation）
>
> 审查方法：对照 Python 参考项目（jiuwenswarm + openjiuwen），逐函数对比方法签名、步骤完整性、⤵️占位代码实际状态
>
> 注意：S-01~S-29/M-01~M-27/T-01~T-14 编号体系延续 2026-10-13 审查，本次编号从 S-30 起始

---

## 汇总

| 级别 | 数量 |
|------|------|
| 严重 (S) | 33 |
| 一般 (M) | 32 |
| 提示 (T) | 14 |
| **合计** | **79** |

---

## 严重问题 (S-30 ~ S-57)

### S-30 [TeamAgent] Stream() 中 FinalizeRound 使用已取消的 ctx

**Python 样例：**
```python
# openjiuwen/agent_teams/agent/team_agent.py L556-570
async def stream(self, inputs, **kwargs):
    try:
        ...
    finally:
        await self._coordination.finalize_round()  # finally 块不受 cancellation 影响
```

**Go 问题：**
```go
// internal/agent_teams/agent/team_agent.go L642-654
// goroutine 中
a.coordination.FinalizeRound(ctx)  // ctx 可能已被调用方 cancel
```

goroutine 继承 `Stream` 入参的 ctx，如果调用方 cancel（如 HTTP 断开），`FinalizeRound(ctx)` 中 ctx 已取消，`memMgr.ExtractAfterRound(ctx)` 可能因 ctx 取消失败或跳过。Python 的 `finally` 块不受 cancellation 影响。

**修复方案：** 使用 `context.WithoutCancel(ctx)`（Go 1.21+）保证清理逻辑不受 ctx 取消影响：
```go
cleanupCtx := context.WithoutCancel(ctx)
a.coordination.FinalizeRound(cleanupCtx)
```

---

### S-31 [TeamAgent] Stream() 空 streamQueue 路径不调用 FinalizeRound

**Python 样例：**
```python
# team_agent.py L556-570 — stream() 的 finally 块确保 finalize_round 总被调用
```

**Go 问题：**
```go
// internal/agent_teams/agent/team_agent.go L655-657
if a.streamController == nil || a.streamController.streamQueue == nil {
    close(outCh)
    return outCh, nil  // FinalizeRound 永远不会被调用
}
```

`Invoke()` 使用 `defer a.coordination.FinalizeRound(ctx)` 保证调用。但 `Stream()` 仅在 goroutine 内调用，如果 streamQueue 为 nil 就直接返回，FinalizeRound 永远不执行，导致内存提取和队列清理缺失。

**修复方案：** 在 Stream() 所有退出路径上确保 FinalizeRound 被调用：
```go
finalized := false
defer func() {
    if !finalized {
        a.coordination.FinalizeRound(context.WithoutCancel(ctx))
    }
}()
// goroutine 中设置 finalized = true 并调用 FinalizeRound
```

---

### S-32 [TeamAgent] Configure() 传 nil blueprint/infra 给 CoordinationKernel.Setup，协调子系统从未初始化

**Python 样例：**
```python
# team_agent.py L444
self._coordination.setup(role=ctx.role)
# setup() 内部从 host.blueprint/host.infra 获取参数，为 None 时抛 RuntimeError
```

**Go 问题：**
```go
// internal/agent_teams/agent/team_agent.go L524
a.coordination.Setup(runtimeCtx.Role, nil, nil)
```

传入 `nil, nil`，但 `kernel.go:96-107` 中当 bp/inf 为 nil 时仅 log error 并 return。结果是 Setup 后 EventBus 和 Dispatcher 均为 nil，后续 Start() 直接 return，整个协调子系统从未初始化，但 TeamAgent 不报错。Python 会抛 RuntimeError。

**修复方案：** 扩展 `types.KernelHost` 接口增加 `BlueprintAccessor()` 和 `InfraAccessor()`，让 Setup 从 host 获取：
```go
func (k *CoordinationKernel) Setup(role schema.TeamRole, opts ...KernelOption) {
    bp := k.host.BlueprintAccessor()
    inf := k.host.InfraAccessor()
    if bp == nil || inf == nil {
        logger.Error(...).Msg("Setup requires configured blueprint/infra")
        return
    }
    ...
}
```

---

### S-33 [TeamAgent] StreamController 的 wakeMailboxCb/requestCompletionPollCb 始终为 nil

**Python 样例：**
```python
# team_agent.py L83-91
StreamController(
    wake_mailbox_callback=self._wake_mailbox_if_interrupt_cleared,
    request_completion_poll_callback=self._request_completion_poll,
)
```

**Go 问题：**
```go
// internal/agent_teams/agent/team_agent.go L128-135
a.streamController = NewStreamController(...)
// 没有传入 WithWakeMailbox 和 WithRequestCompletionPoll 选项
```

`sc.wakeMailboxCb == nil`（直接 return），`sc.requestCompletionPollCb == nil`（永远跳过）。**Leader 的 POLL_TASK 即时完成评估永远不会被触发**。

**修复方案：** 注入回调：
```go
a.streamController = NewStreamController(
    ...,
    WithWakeMailbox(func(ctx context.Context) error {
        a.coordination.WakeMailboxIfInterruptCleared()
        return nil
    }),
    WithRequestCompletionPoll(func(ctx context.Context) error {
        return a.requestCompletionPoll(ctx)
    }),
)
```

---

### S-34 [TeamAgent] TeamMember.Status/UpdateStatus/UpdateExecutionStatus 全部是 stub

**Python 样例：**
```python
# member.py
status() -> 从 DB 读取真实状态
update_status() -> 校验状态转换 + 写 DB + 发布事件
```

**Go 问题：**
```go
// internal/agent_teams/agent/member.go L49-82
func (m *TeamMember) Status() MemberStatus { return MemberStatusReady }  // 始终返回 READY
func (m *TeamMember) UpdateStatus(...) { /* 只打日志 */ }
```

`IsShutdownRequested()` 永远返回 false，`runOneRound` 中的 shutdown 检查永远为 true，状态转换事件从未发布。

**修复方案：** 实现 `TeamMember` 的 DB 读写方法。`UpdateStatus` 需要：读取当前状态 → `fsm.IsValidMemberTransition` 校验 → 写 DB → 发布事件。标记为 `#9.65` 的 TODO 应尽快回填。

---

### S-35 [CoordinationKernel] Start() 缺少 subscribedTopics 守卫导致重复订阅

**Python 样例：**
```python
# kernel.py L174-177
if team_name and not self._subscribed_topics:
    await self.subscribe_transport(team_name)
```

**Go 问题：**
```go
// internal/agent_teams/agent/coordination/kernel.go L253-254
_ = k.host.SubscribeTransport(ctx, k.host.TeamName())
// 没有检查 len(k.subscribedTopics) == 0
```

如果 `Start()` 被多次调用（如 recover 后重新 start），同一 topic 被重复订阅，每次事件触发多次回调。

**修复方案：**
```go
if len(k.subscribedTopics) == 0 {
    _ = k.host.SubscribeTransport(ctx, k.host.TeamName())
}
```

---

### S-36 [CoordinationKernel] Start() Workspace 重复初始化缺少 initialized 检查

**Python 样例：**
```python
# kernel.py L147-151
if infra.workspace_manager and not infra.workspace_initialized:
    await infra.workspace_manager.initialize(...)
```

**Go 问题：**
```go
// internal/agent_teams/agent/coordination/kernel.go L214-223
if wsMgr != nil {
    _ = wsMgr.Initialize(ctx, remoteURL)  // 每次都调用
}
```

缺少 `infra.workspace_initialized` 检查。Team 被恢复时 workspace 会被重复初始化。

**修复方案：** 添加初始化标志检查：
```go
if wsMgr != nil && !k.host.IsWorkspaceInitialized() {
    _ = wsMgr.Initialize(ctx, remoteURL)
    k.host.SetWorkspaceInitialized()
}
```

---

### S-37 [SpawnManager] baseCtx 使用 context.Background()，恢复操作丢失 ctx 传播链

**Python 样例：** Python asyncio 不需要 ctx 传播，所有操作在同一线程。

**Go 问题：**
```go
// internal/agent_teams/agent/spawn_manager.go L76
baseCtx: context.Background()
```

`OnTeammateUnhealthy` 中所有操作都使用 `m.baseCtx`，Leader 的 ctx 被取消时恢复操作无法感知。

**修复方案：** `baseCtx` 应从构造时传入的 ctx 派生：
```go
func NewSpawnManager(ctx context.Context, ...) *SpawnManager {
    return &SpawnManager{
        baseCtx: ctx,
        ...
    }
}
```

---

### S-38 [Spawn] InProcessSpawn goroutine 忽略 runTeamMemberFunc 返回错误

**Python 样例：**
```python
# inprocess_spawn.py L76-89
try:
    await Runner.run_agent_team(...)
except Exception as e:
    # 异常通过 task.exception() 传播
```

**Go 问题：**
```go
// internal/agent_teams/spawn/inprocess_spawn.go L87-88
_, _ = runFn(runCtx, teammate, inputs, sessionID)  // 完全忽略错误
```

teammate 执行失败，错误被静默吞掉，外部无法知道是正常退出还是异常。

**修复方案：**
```go
result, err := runFn(runCtx, teammate, inputs, sessionID)
if err != nil {
    logger.Error(inprocessLogComponent).
        Str("member_name", runtimeCtx.MemberName).Err(err).
        Msg("[inprocess] teammate execution failed")
}
```

---

### S-39 [Spawn] spawnInprocess 不传播 session_id 到 teammate context

**Python 样例：**
```python
# inprocess_spawn.py L70-74
run_ctx = contextvars.copy_context()
set_session_id(session_id)  # 确保 task 继承 session_id
```

**Go 问题：**
```go
// internal/agent_teams/spawn/inprocess_spawn.go L73
runCtx, cancel := context.WithCancel(ctx)
// sessionID 仅作为参数传递给 runFn，没有注入到 runCtx
```

teammate goroutine 内部通过 `agentteams.GetSessionID(ctx)` 读取的 session_id 为空。

**修复方案：**
```go
if sessionID != "" {
    state := sessionctx.InitSessionState()
    state.SetSessionID(sessionID)
    runCtx = sessionctx.WithSessionState(runCtx, state)
}
```

---

### S-40 [deep_adapter] evolutionOutputSchemasToMaps 类型断言永远失败 — evolution watcher 完全瘫痪

**Python 样例：**
```python
# team_helpers.py L1180
events = await rail.drain_pending_approval_events(wait=False) or []
# events 是 list[Any]，每个元素可以直接当 dict 传
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L1175-1191
func evolutionOutputSchemasToMaps(events any) []map[string]any {
    if slice, ok := events.([]any); ok {  // 永远不会匹配！
```

`rail.DrainPendingApprovalEvents` 返回 `[]*stream.OutputSchema`，无法类型断言为 `[]any`。所有 evolution 监控逻辑永远走空分支，审批事件完全丢失。

**修复方案：** 函数签名改为接受实际类型：
```go
func evolutionOutputSchemasToMaps(events []*stream.OutputSchema) []map[string]any {
    result := make([]map[string]any, 0, len(events))
    for _, item := range events {
        if m := chunkToMap(item); m != nil {
            result = append(result, m)
        }
    }
    return result
}
```

---

### S-41 [deep_adapter] handleTeamSkillEvolveApproval 的 rail 查找逻辑与 Python 严重偏离

**Python 样例：**
```python
# interface_deep.py L3656-3668
def find_team_skill_rail(request_id, channel_id=None):
    rail = get_team_manager(channel_id).find_team_skill_rail_for_request(request_id)
    if rail is not None: return rail
    return find_team_skill_rail_across_managers(request_id)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L46-61
func (d *DeepAdapter) findTeamSkillRail() *coreevolution.TeamSkillEvolutionRail {
    if d.teamSkillEvolutionRail != nil { return d.teamSkillEvolutionRail }
    found := d.instance.FindRailsByType(reflect.TypeOf(&coreevolution.TeamSkillEvolutionRail{}))
```

Go 端：① 直接返回缓存字段（可能是另一个 session 的 rail）② 按 reflect.Type 查找（不按 request_id 匹配）③ 完全缺失跨 TeamManager 搜索 ④ 函数签名不接收 requestID/channelID。

**修复方案：** 对齐 Python，接受 `requestID` 和 `channelID` 参数：
```go
func findTeamSkillRail(requestID string, channelID string) *coreevolution.TeamSkillEvolutionRail {
    tm := team.GetTeamManager(channelID)
    if rail := tm.FindTeamSkillRailForRequest(requestID); rail != nil {
        return rail
    }
    return team.FindTeamSkillRailAcrossManagers(requestID)
}
```

---

### S-42 [deep_adapter] 审批匹配关键字不完整 + 缺 SyncTeamSkillsAcrossManagers

**Python 样例：**
```python
# interface_deep.py L3671-3699
accepted = self._option_matches(answers, ("accept", "接收", "接受"))
# 审批成功后:
await sync_team_skills_across_managers(session_id)
```

**Go 问题：**
1. Go 端 `parseApprovalAnswers` 硬编码 4 个关键字，缺少 `"accept"` 和 `"接受"`（只有 `"Accept"` 大写）
2. 审批成功后缺失 `SyncTeamSkillsAcrossManagers` 调用
3. `pushTeamSkillEvolveResolutionStatus` 是空壳（只记日志），Python 实际推送 WebSocket

**修复方案：**
1. 统一审批匹配逻辑，使用与 Python 相同的 `accept_labels` 参数列表，添加 `"accept"`/`"接受"` 并做 `.ToLower()` 匹配
2. 审批成功后调用 `team.SyncTeamSkillsAcrossManagers(sessionID)`

---

### S-43 [deep_adapter] watchTeamEvolutionAndPushTeam 缺失异常保护 — panic 直接崩溃

**Python 样例：**
```python
# team_helpers.py L1158-1392
try:
    while True: ...
except Exception as exc:
    logger.warning("[TeamHelpers] evolution monitor failed: %s", exc)
    if active_cycle_request_id is not None:
        await push_evolution_status(...)
```

**Go 问题：** `deep_adapter_team.go L860-1099` 整个函数没有 `defer recover`。panic 后不会推送失败状态、不会 cleanupEvolutionRail、不会 PopTeamEvolutionWatcher。

**修复方案：** 在 goroutine 入口加 `defer recover` + 失败状态推送：
```go
go func() {
    defer func() {
        if r := recover(); r != nil {
            logger.Error(logComponent).Any("panic", r).Msg("watchTeamEvolutionAndPushTeam: panic recovered")
            cleanupEvolutionRail()
            tm.PopTeamEvolutionWatcher(sessionID)
        }
    }()
    ...
}()
```

---

### S-44 [deep_adapter] consumeStreamWithQuery 缺失异常广播 — 流失败时前端无感知

**Python 样例：**
```python
# team_helpers.py L1028-1044
except Exception as exc:
    logger.error("[TeamHelpers] stream failed: ...", exc_info=True)
    _broadcast_event(channel_id, session_id, {"event_type": "team.error", "error": str(exc), ...})
```

**Go 问题：** `deep_adapter_team.go L584-735` 没有 try/except 包裹。panics 后不会广播 `team.error`。

**修复方案：** 在 `for chunk := range streamCh` 外层加 `defer func() { if r := recover() ... }()` + 广播 `team.error` 事件。

---

### S-45 [deep_adapter] processTeamMessageStream goroutine 缺失 panic recovery — channel 泄漏

**Python 样例：** Python 的 `process_team_message_stream` 是 async generator，异常通过 yield 传播。

**Go 问题：** `deep_adapter_team.go L224-466` 中 `go func()` 只有 `defer close(ch)`。panic 绕过 defer（除非 recover），导致 `ch` 永远不关闭、消费方永远阻塞、waiter 永久残留。

**修复方案：** 在 goroutine 开头加 `defer func() { if r := recover(); r != nil { ... } }()` 确保 `close(ch)` 总能执行。

---

### S-46 [deep_adapter] /evolve 命令缺失 isinstance(evolve_result, str) 分支

**Python 样例：**
```python
# team_helpers.py L597-601
if isinstance(evolve_result, str) and evolve_result.strip():
    return {"output": f"Skill '{skill_name}' 演进请求已提交，请等待审批。", "result_type": "answer"}
```

**Go 问题：** `team_helpers.go L319-336` 直接跳到 `approvalResultFromEventOrItems`，完全缺失 str 返回值分支。当 `RequestUserEvolution` 返回字符串类型时，Go 端会尝试从字符串中取 `ApprovalEvent`，得到 nil，返回"未生成新的团队技能演进经验"——语义完全错误。

**修复方案：** 检查 `evolveResult` 是否为字符串类型，对齐 Python 的分支逻辑。

---

### S-47 [ChromaIndexer] ToConstructDict/ToSearchDict 输出 PascalCase 键名，Python 输出 snake_case

**Python 样例：**
```python
# ChromaVectorField.to_dict(stage="construct") 输出:
{'max_neighbors': 16, 'ef_construction': 100, 'ef_search': 100}
```

**Go 问题：**
```go
// internal/agentcore/foundation/store/vector_fields/base.go L188
result[field.Name] = fieldValue.Interface()
// 输出: {"MaxNeighbors": 16, "EfConstruction": 100, "EfSearch": 100}
```

ChromaDB HNSW 配置期望 snake_case 键名。导致：① CheckConfigsMatching 永远报错 ② HNSW 配置未传给集合创建。

**修复方案：** 使用 struct tag `vf:"construct,name=max_neighbors"` 指定键名，或在 `collectFields` 中将 PascalCase 转换为 snake_case。

---

### S-48 [ChromaVectorStore] GetOrCreateCollection 未传递完整 HNSW 配置

**Python 样例：**
```python
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
// 仅传 distance metric，丢失了 max_neighbors/ef_construction/ef_search/resize_factor 等
```

**修复方案：** 根据 `constructConfig` 和 `searchConfig` 构建完整的 `CreateCollectionOption` 列表，使用 chroma-go v2 的 `WithHNSWMCreate`/`WithHNSWConstructionEfCreate`/`WithHNSWSearchEfCreate` 等选项。

---

### S-49 [ChromaVectorStore] CheckVectorField 传入空 map — 校验形同虚设

**Python 样例：**
```python
def check_vector_field(self):
    self._check_configs_matching(self._construct_config, self.collection.configuration.get("hnsw", {}))
```

**Go 问题：**
```go
// internal/agentcore/retrieval/vector_store/chroma.go L220-221
func (s *ChromaVectorStore) CheckVectorField() error {
    return CheckConfigsMatching(s.constructConfig, map[string]any{})  // actual 始终是空 map
}
```

`actual` 始终为空，导致所有键在 actual 中缺失，校验结果无意义。

**修复方案：** 需要从 `s.collection` 获取实际 HNSW 配置并传入。

---

### S-50 [ChromaVectorStore] NewChromaVectorStore 未使用 databaseName — 多数据库功能缺失

**Python 样例：**
```python
@staticmethod
def create_client(database_name, path_or_uri, token="", **kwargs):
    if database_name and database_name != DEFAULT_DATABASE:
        admin_client = chromadb.AdminClient(...)
        if database_name not in {db.get("name") for db in admin_client.list_databases()}:
            admin_client.create_database(database_name)
    return chromadb.PersistentClient(path=path_or_uri, database=database_name)
```

**Go 问题：** 完全忽略 `databaseName`，所有数据都写入默认数据库，未调用 AdminClient API。

**修复方案：** 实现 `createClient` 方法，当 `databaseName` 非默认时通过 `WithDatabaseAndTenant(dbName, tenant)` 指定数据库。

---

### S-51 [context_evolver] ReMe RecallMemoryOp 缺少 user_id 默认值 "default"

**Python 样例：**
```python
user_id = context.get("user_id", "default")
```

**Go 问题：**
```go
// internal/agentcore/context_evolver/retrieve/task/reme/run.go L104
userID, _ := cecontext.GetTyped[string](rc, "user_id")
// user_id 未设置时为空字符串 ""
```

后续 metadata_filter 构建时 `if userID != ""` 条件导致 workspace_id 不加入 filter，搜索返回所有用户的数据。

**修复方案：**
```go
userID, _ := cecontext.GetTyped[string](rc, "user_id")
if userID == "" {
    userID = "default"
}
```

---

### S-52 [context_evolver] ReMe RecallMemoryOp 跳过 from_vector_node 转换，直接读 Metadata

**Python 样例：**
```python
memory = ReMeMemory.from_vector_node(node)
retrieved_mem = ReMeRetrievedMemory(when_to_use=memory.when_to_use, content=memory.content)
```

**Go 问题：**
```go
// internal/agentcore/context_evolver/retrieve/task/reme/run.go L136-141
whenToUse, _ := node.Metadata["when_to_use"].(string)
content, _ := node.Metadata["content"].(string)
```

跳过 `from_vector_node` 转换，如果数据存储在 Content 字段而非 Metadata，会静默返回空值。

**修复方案：** 先用 `ceschema.NewReMeMemoryFromVectorNode(node)` 转换，再从 ReMeMemory 字段取值。

---

### S-53 [controller] EventHandlerBase 缺少 4 个核心方法的默认实现

**Python 样例：** EventHandler ABC 定义了 `handle_input`, `handle_task_interaction`, `handle_task_completion`, `handle_task_failed` 抽象方法。

**Go 问题：** `event_handler.go` 中 `EventHandlerBase` 只提供了部分默认实现，核心方法 `HandleInput/HandleTaskInteraction/HandleTaskCompletion/HandleTaskFailed` 没有默认实现。忘记实现时编译不报错（嵌入类型），运行时 panic。

**修复方案：** 为这 4 个核心方法提供桩默认实现：
```go
func (b *EventHandlerBase) HandleInput(_ context.Context, _ *EventHandlerInput) (map[string]any, error) {
    return map[string]any{"status": "not_supported"}, nil
}
```

---

### S-54 [single_agent] reactLoop 中 getTools 忽略错误

**Python 样例：** Python 端 `get_tools` 不会返回 error。

**Go 问题：**
```go
// internal/agentcore/single_agent/agents/react_invoke.go L604
tools, _ := a.getTools(ctx)
```

错误被静默忽略，如果工具获取失败，Agent 将以空工具列表执行，行为严重退化。

**修复方案：**
```go
tools, err := a.getTools(ctx)
if err != nil {
    logger.Warn(logComponent).Err(err).Msg("获取工具列表失败，使用空工具列表")
}
```

---

### S-55 [single_agent] invokeImpl 最终 result 可能为 nil map

**Go 问题：** `react_invoke.go:443-456` 中 `var result map[string]any`，如果 FireLifecycle 回调正常完成但 result 未被赋值，且 cbc.Inputs() 和 invokeInputs.Result 都为 nil，最终 `return result, nil` 返回 `(nil, nil)`，调用方可能 panic。

**修复方案：**
```go
if result == nil {
    result = map[string]any{"output": "", "result_type": "error"}
}
return result, nil
```

---

### S-56 [context_engine] offload.go WriteFile 使用 context.Background()

**Go 问题：**
```go
// internal/agentcore/context_engine/processor/offload.go L156
result, err := sysOperation.Fs().WriteFile(context.Background(), offloadPath, string(contentJSON))
```

上下文取消时无法中断 I/O 操作。该方法已有 ctx 参数。

**修复方案：** 传入调用方的 ctx。

---

### S-57 [context_engine] processor_state_recorder.go WriteStream 使用 context.Background()

**Go 问题：**
```go
// internal/agentcore/context_engine/context/processor_state_recorder.go L163
_ = sessionRef.WriteStream(context.Background(), stream.OutputSchema{...})
```

**修复方案：** 在 Emit 方法签名中添加 ctx 参数，或从 SessionFacade 获取。

---

### S-58 [TeamRunner] 流式 goroutine 泄露 — 消费者停止读取后 Finalize/PostRun 永不执行

**Python 样例：**
```python
# team_runner.py L231-275
# Python 使用 async for ... yield + try/finally
# 即使消费者提前退出迭代，Python 的 generator cleanup 机制保证 finally 块执行
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L192-204, L286-299, L478-497
mergedCh := make(chan stream.Schema, 64)
go func() {
    defer close(mergedCh)
    for chunk := range agentCh {
        mergedCh <- chunk  // ← 如果消费者停止读取，此处永久阻塞
    }
    // 以下清理代码永不执行：
    teamRuntime.UnbindTeamSession(...)
    _ = teamSession.PostRun(ctx)
    _ = GetTeamRuntimeManager().Finalize(...)
    closeTeamInteractGate(...)
}()
```

Go 中 3 个流式路径都存在此问题：goroutine 向 `mergedCh` 发送时，如果消费者停止接收（ctx 取消/上游断开），goroutine 永久阻塞，清理代码永不执行。

**修复方案：** 在 goroutine 内部用 `select` 检查 `ctx.Done()`：
```go
go func() {
    defer close(mergedCh)
    for chunk := range agentCh {
        select {
        case mergedCh <- chunk:
        case <-ctx.Done():
            return  // 触发 defer close + 清理
        }
    }
    // 清理代码...
}()
```

---

### S-59 [TeamRunner] Finalize 异常处理不等价 — StopCoordination 失败后仍 Remove pool

**Python 样例：**
```python
# manager.py L203-229
try:
    shutdown_requested = await agent.is_shutdown_requested()
    if shutdown_requested or agent.lifecycle != "persistent":
        await agent.stop_coordination()
        await self._pool.remove(team_name)
    else:
        await agent.pause_coordination()
        entry.state = RuntimeState.PAUSED
except Exception as exc:
    team_logger.warning(...)
```

Python 的 `try/except` 包裹整个决策块。如果 `stop_coordination()` 失败，`pool.remove()` **不会执行**。

**Go 问题：**
```go
// internal/agent_teams/runtime/manager.go L298-317
if shutdownRequested || ag.Lifecycle() != "persistent" {
    if stopErr := ag.StopCoordination(ctx); stopErr != nil {
        logger.Warn(...)
    }
    m.pool.Remove(teamName)  // ← 无论 StopCoordination 是否失败都执行！
}
```

同样，`PauseCoordination` 失败后 Go 仍将 `entry.State` 设为 `RuntimeStatePaused`。

**修复方案：** 对齐 Python 的原子性——只有操作成功才修改状态：
```go
if stopErr := ag.StopCoordination(ctx); stopErr != nil {
    logger.Warn(...)
    return stopErr  // 不从 pool 移除，保留条目供后续重试
}
m.pool.Remove(teamName)
```

---

### S-60 [TeamRunner] inspectSession 吞掉 PreRun 错误但 Python 传播

**Python 样例：**
```python
# manager.py L839
await team_session.pre_run()  # 无 try/except，错误向上传播
```

**Go 问题：**
```go
// internal/agent_teams/runtime/manager.go L836-841
if preErr := teamSession.PreRun(ctx); preErr != nil {
    logger.Warn(mgrLogComponent).Err(preErr).Msg("inspectSession: PreRun 失败")
    // 继续执行！session 可能未初始化
}
```

Python 中 `pre_run()` 失败直接抛异常中断 `activate` 流程。Go 吞掉错误后继续，session 处于未初始化状态。

**修复方案：**
```go
if preErr := teamSession.PreRun(ctx); preErr != nil {
    return false, false, "", fmt.Errorf("inspectSession: PreRun 失败: %w", preErr)
}
```

---

### S-61 [TeamRunner] NewTeamAgentSpecFromDict 解析失败返回默认值而非 nil

**Python 样例：**
```python
# team_runner.py L648-657
try:
    return TeamAgentSpec.model_validate(spec_data)
except Exception as exc:
    logger.warning("failed to parse persisted spec ...")
    return None  # 明确返回 None
```

**Go 问题：**
```go
// team_runner.go L532-538
spec := atschema.NewTeamAgentSpecFromDict(specMap)
if spec == nil {  // ← 此条件永远不会为 true！
    logger.Warn(...).Msg("resolveSpecFromSessionBucket: spec 反序列化失败")
    return nil
}
```

`NewTeamAgentSpecFromDict` 在解析失败时返回默认的 `TeamAgentSpec{}`，而非 `nil`。`if spec == nil` 是死代码——当 spec 数据损坏时，函数返回无效默认 spec。

**修复方案：** 修改 `NewTeamAgentSpecFromDict` 在解析失败时返回 `nil`。

---

### S-62 [TeamRunner] leader_member_name 取错字段 — AgentCard().Name vs Blueprint().MemberName()

**Python 样例：**
```python
# team_runner.py L247-249
blueprint = getattr(activation.agent, "blueprint", None)
leader_member_name = getattr(blueprint, "member_name", None)
leader_role = getattr(blueprint, "role", None)
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L456-460
if bp := activation.Agent.AgentCard(); bp != nil {
    leaderMemberName = bp.Name  // ← 这是 display_name，不是 member_name！
}
leaderRole := atschema.TeamRoleLeader  // ← 硬编码，未从 Blueprint 读取
```

`AgentCard().Name` 对应 Python 的 `card.name`（显示名称），而 Python 取的是 `blueprint.member_name`（内部标识）。两者语义完全不同。

**修复方案：**
```go
if bp := activation.Agent.Blueprint(); bp != nil {
    if mn := bp.MemberName(); mn != nil {
        leaderMemberName = *mn
    }
    r := bp.Role()
    leaderRole = &r
}
```

---

## 一般问题 (M-28 ~ M-59)

### M-28 [TeamAgent] Invoke() chunk 类型断言失败导致 lastResult 始终为 nil

**Go 问题：** `team_agent.go:588-590` 中 chunk 类型断言为 `interface{ ToMap() map[string]any }`，但 `TeamOutputSchema` 没有实现 `ToMap()` 方法。`lastResult` 永远为 nil。

**修复方案：** 改为直接返回 chunk，或为 `TeamOutputSchema` 添加 `ToMap()` 方法。

---

### M-29 [TeamAgent] human_agent_say 缺少 on_inbound 回调

**Python 样例：** Python 的 `HumanAgentInbox.__init__` 有 `on_inbound` 回调（触发 avatar LLM 生成）。

**Go 问题：** `team_agent.go:712` 传入 `nil`，human agent 收到消息后不会自动驱动 avatar 生成响应。

**修复方案：** 实现 `on_inbound` 回调。

---

### M-30 [Kernel] Start() Memory 初始化不传 pending_user_query

**Python 样例：**
```python
await harness.inject_member_memory(memory_manager, query=host.state.pending_user_query or "")
```

**Go 问题：** `kernel.go:235` — `_ = harnessAccessor.InjectMemberMemory(ctx, memMgr, "")`，第三个参数始终传空字符串。

**修复方案：** 从 host 获取 `PendingUserQuery()` 并传入。需扩展 `types.KernelHost` 接口。

---

### M-31 [SpawnManager] PublishRestartEvent 未发布任何事件

**Python 样例：** `spawn_manager.py:270-293` 通过 Messager 发布 `MemberRestartedEvent` 到 TEAM 主题。

**Go 问题：** `spawn_manager.go:395-408` 只记录 Debug 日志，标注 "⤵️ 9.65: Messager 发布待回填"。

**修复方案：** 实现 `PublishRestartEvent`，构造 `MemberRestartedEvent` 并通过 Messager 发布。

---

### M-32 [deep_adapter] /evolve_list 表格缺少 Used 和 Effect 列

**Python 样例：**
```python
"| # | Score | Used | Effect | Section | Content (preview) |"
```

**Go 问题：** `team_helpers.go:185-198` 缺少 `Used`（times_used/times_presented）和 `Effect`（times_positive/times_negative）列。

**修复方案：** 对齐 Python 表格格式，添加 Used 和 Effect 列。

---

### M-33 [deep_adapter] consumeStreamWithQuery 未传 envs 给 RunAgentTeamStreaming

**Python 样例：**
```python
async for chunk in Runner.run_agent_team_streaming(..., envs=envs, stream_logger=lg)
```

**Go 问题：** `deep_adapter_team.go:631` 未传 `envs`（含 `hide_dm` 等），也未传 `streamLogger`。

**修复方案：** 扩展 `RunAgentTeamStreaming` 签名或使用 functional options。

---

### M-34 [deep_adapter] processTeamMessageStream 未从 request.params 提取 mode 到 requestMetadata

**Python 样例：**
```python
request_metadata.setdefault("mode", request.params.get("mode"))
```

**Go 问题：** 只从 `req.Metadata` 拷贝，不包含 `params.mode`。

**修复方案：** 检查 `req.Params` 中的 `"mode"` 字段，setdefault 到 `requestMetadata`。

---

### M-35 [deep_adapter] resolveEvolutionEventTimeoutSec 是空壳 — 永远返回 fallback

**Go 问题：** `deep_adapter_team.go:1209-1217` 未读取 rail 配置的超时值。

**修复方案：** 从 `TeamSkillEvolutionRail` 读取配置的超时值，或添加 TODO 注释。

---

### M-36 [deep_adapter] consumeStreamWithQuery 缺失 TeamStreamLogger

**Python 样例：**
```python
if stream_trace_enabled:
    lg = TeamStreamLogger(file_path=str(traces_dir / f"dump-team-{session_id}.txt"))
```

**Go 问题：** `deep_adapter_team.go:617-626` — `streamTraceEnabled` 已计算但被丢弃。

**修复方案：** 当 `streamTraceEnabled` 为 true 时创建 TeamStreamLogger 实例。

---

### M-37 [deep_adapter] HandleTeamSlashCommand 对 /evolve 缺少 maxsplit 限制

**Python 样例：** `stripped.split(maxsplit=2)`

**Go 问题：** 使用 `strings.Fields` 代替 `strings.SplitN(stripped, " ", 3)`。当用户输入含空格的 intent 时结果不同。

**修复方案：** 使用 `strings.SplitN(stripped, " ", 3)` 对齐 Python 的 `split(maxsplit=2)` 行为。

---

### M-38 [Swarm] TeamManager loadTeamSpec 缺少 model_pool 构建逻辑

**Python 样例：** `team_manager.py:322-350` 检查 `default_models > 1` 时填充 `model_pool` 和 `model_pool_strategy`。

**Go 问题：** `team_manager_spec.go` 的 `loadTeamSpec` 完全缺失 model_pool 逻辑。

**修复方案：** 在 `loadTeamSpec` 中添加 model_pool 构建逻辑。

---

### M-39 [Swarm] BuildAgentCustomizer 闭包体 5 个步骤全部 ⤵️ 未回填

**Python 样例：** `team_manager.py:558-596` 闭包约 200 行，包含技能同步、rail 注册、能力卡继承等。

**Go 问题：** `team_manager_spec.go:147-168` 只有 5 个占位注释和步骤 f 的 RegisterMemberRuntimeTools。

**修复方案：** 逐步回填闭包内部逻辑，至少完成步骤 b（技能同步）和步骤 d（rail 注册）。

---

### M-40 [Swarm] EnsureMonitor 的两个 goroutine 同时消费同一 Events channel 互相抢夺

**Go 问题：** `team_manager_monitor.go:207-211` 和 `L359-366`，`consumeMonitorEvents` 和 `autoStartEvolutionWatcher` 都 `range handler.Events()`。Go channel 只能被消费一次。

**修复方案：** 在 TeamMonitorHandler 层面实现事件扇出（fan-out），或由 `consumeMonitorEvents` 在处理事件时分发给 evolution watcher。

---

### M-41 [Swarm] StreamEventRail.Uninit 不清理 per-session maps — 长期运行内存泄漏

**Go 问题：** `stream_event_rail.go:172-177` 只清除了 `deepAgent = nil`，没有清理 `abortRequested`、`pauseConds`、`conversationIDs` 等 per-session maps。

**修复方案：** 在 Uninit 中重建所有 per-session map，或遍历所有已知 sessionID 调用 CleanupSession。

---

### M-42 [Swarm] ChannelTransport.Send 在 mu.Unlock 后才操作 channel — TOCTOU 竞态

**Go 问题：** `channel_transport.go:83-91` 先 Lock 检查 closed，Unlock 后才 send。Unlock 和 send 之间，另一个 goroutine 可以 Close 导致 panic。

**修复方案：** 将 closed 检查和 channel 发送放在同一锁保护范围内。

---

### M-43 [Swarm] SessionManager 缺少 CancelAllSessionTasks 方法

**Python 样例：** `session_manager.py:73-76` 有 `cancel_all_session_tasks` 方法。

**Go 问题：** Go 端有 `CancelSessionTask` 但缺少批量取消能力。

**修复方案：** 添加 `CancelAllSessionTasks(ctx context.Context, logPrefix string) error`。

---

### M-44 [ChromaIndexer] 每次操作都创建新的 ChromaVectorStore（含 PersistentClient）— 资源浪费

**Python 样例：** Python ChromaIndexer 在 `__init__` 中只创建一次 client 并复用。

**Go 问题：** BuildIndex/DeleteIndex/IndexExists/GetIndexInfo 每次都 `NewChromaVectorStore(...)` 创建新 client。

**修复方案：** 在 `ChromaIndexer` 中缓存 client，后续操作复用。

---

### M-45 [ChromaIndexer] BuildIndex 创建了 vectorStore 但没有 Close

**Go 问题：** DeleteIndex/GetIndexInfo/IndexExists 都正确 Close，但 BuildIndex 没有。

**修复方案：** 在 BuildIndex 中添加 `defer vectorStore.Close()`。

---

### M-46 [ChromaVectorStore] addBatch 中 embedding 类型断言仅支持 []float64

**Go 问题：** 从 JSON 反序列化的 `[]any`（每个元素是 float64）会被静默跳过，数据丢失。

**修复方案：** 增加类型转换辅助函数，支持 `[]float64` 和 `[]any` 两种类型。

---

### M-47 [ChromaVectorStore] CheckConfigsMatching isclose 基准使用 abs(val) 而非 max(abs(a),abs(b))

**Go 问题：** 与 Python `isclose` 公式不一致，在 val 接近 0 时差异明显。

**修复方案：** 改为 `math.Abs(actualFloat-valFloat) <= 1e-2*math.Max(math.Abs(actualFloat), math.Abs(valFloat))+1e-3`。

---

### M-48 [context_evolver] ParallelOp 共享 RuntimeContext 并发写入风险

**Go 问题：** 多个 Op 并发写入同一个 key 时，最终值非确定性。

**修复方案：** 在注释中明确说明调用方应确保并行 Op 不写同一个 key，或为 ParallelOp 提供 key 前缀隔离。

---

### M-49 [single_agent] Stream wrapper channel 缺 ctx 取消检查

**Go 问题：** `react_invoke.go:155-181` channel 包装 goroutine 使用传入的 ctx，但 ctx 被 cancel 后 goroutine 可能泄漏。

**修复方案：** 在 goroutine 内加 `if ctx.Err() != nil { return }`。

---

### M-50 [single_agent] remote_skill_util HTTP 请求 context.Background()

**Go 问题：** `remote_skill_util.go:304,430,490` 三处 HTTP GET 请求均使用 `context.Background()`。

**修复方案：** 方法签名中应接受 ctx 参数并传递到 HTTP 请求。

---

### M-51 [sys_operation] OperationRegistry.GetSupportedOperations 使用冒泡排序

**Go 问题：** O(n^2) 排序，应使用 `sort.Strings` O(n log n)。

**修复方案：** 替换为 `sort.Strings(names)`。

---

### M-52 [sys_operation] OperationRegistry.Register 不校验工厂函数一致性

**Go 问题：** 同一个 name+mode 被注册两次但 NewFunc 不同时，第二次注册被静默忽略。

**修复方案：** 在幂等跳过前增加 Warn 日志，记录工厂函数不一致。

---

### M-53 [TeamRunner] 非流式路径缺少 defer 保护 — panic 时清理代码不执行

**Python 样例：**
```python
# team_runner.py L183-204
try:
    return await activation.agent.invoke(inputs, session=activation.session)
finally:
    await self._get_team_runtime_manager().finalize(...)
    await self._close_team_interact_gate(...)
    await activation.session.post_run()
```

**Go 问题：** `team_runner.go:417-421` 中 `RunBaseTeam`/`RunTeamMember`/`runAgentTeam` 三个非流式路径都没有 `defer` 保护。如果 `Invoke` panic，Finalize/closeGate/PostRun 全部跳过。

**修复方案：** 用 `defer` 对齐 Python 的 `try/finally`。

---

### M-54 [TeamRunner] closeTeamInteractGate 使用 context.Background() 可能无限阻塞

**Go 问题：** `team_runner.go:577` 中 `_ = entry.InteractGate.CloseAndDrain(context.Background())`，无超时保护。

**修复方案：** 传入带超时的 context：`context.WithTimeout(ctx, 30*time.Second)`。

---

### M-55 [TeamRunner] resolveSpecFromSessionBucket 空桶检查遗漏

**Python 样例：** `if not bucket:` 检查同时排除 `None` 和空字典。

**Go 问题：** 只检查 `nil`，空 `map[string]any{}` 会通过。

**修复方案：** `if bucket == nil || len(bucket) == 0 { return nil }`。

---

### M-56 [TeamRunner] prepareBaseTeam 不支持 str 类型输入

**Python 样例：** `if isinstance(base_team, str): return await self._resource_manager.get_agent_team(team_id=base_team)`

**Go 问题：** 只处理 `BaseTeam`，`str` 类型直接报错。

**修复方案：** 添加 str 分支（需要导入 resource_mgr 或通过接口注入）。

---

### M-57 [TeamRunner] DeleteTeam 在 db.Initialize 失败时仍继续删除

**Python 样例：** `db.initialize()` 失败 → 抛异常，后续不执行。

**Go 问题：** Go 在 `db.Initialize` 失败时只记录警告，但继续执行后续步骤（释放 checkpoint、删除目录等）。

**修复方案：** 对齐 Python，在 `db.Initialize` 失败时返回错误。

---

### M-58 [TeamRunner] ResolveTeamSessionReleaseInfo 使用 context.Background()

**Go 问题：** `release_info.go:52` 中 `sess.PreRun(context.Background())`。

**修复方案：** 接受 ctx 参数并传播。

---

### M-59 [TeamRunner] buildTeamRuntimeReadyChunk 中 leader_role 总是非 nil

**Python 样例：** `TeamOutputSchema(..., role=leader_role)` — `leader_role` 可以为 None。

**Go 问题：** Go 总是传递 `&role`（非 nil 指针），即使 `leaderRole` 为零值。下游代码可能依赖此区分。

**修复方案：** 当 role 为零值时传 nil。

---

## 提示问题 (T-15 ~ T-28)

### T-15 [TeamAgent] NewStreamController 缺少 requestCompletionPoll 的角色守卫

**修复方案：** 当 S-33 修复后注入 `requestCompletionPollCb` 时，需添加 `role == LEADER && lifecycle == "persistent"` 守卫。

---

### T-16 [Kernel] persistTeamLifecycle 使用脆弱的类型断言

**修复方案：** 在 `metadata` 包中添加 `MergeTeamNamespace` 函数，对齐 Python。

---

### T-17 [Spawn] CleanupSharedResources 缺少 cleanup_inprocess_bus 回填

**修复方案：** Messager（9.65）实现后回填。

---

### T-18 [Swarm] ResponsePromptRail.BeforeModelCall 不先 RemoveSection 再 AddSection

**Python 样例：** Python 调用 `builder.remove_section("response")` 再 `add_section`。

**修复方案：** 在 AddSection 前添加 `builder.RemoveSection("response")`。

---

### T-19 [Swarm] RuntimePromptRail 部分 inject 方法未先 RemoveSection

**修复方案：** 在每个 inject 方法开头添加 `builder.RemoveSection(sectionName)`。

---

### T-20 [Swarm] PermissionContext.FromDict 未处理非布尔值类型

**修复方案：** 添加字符串 "true"/"false" → bool 的转换逻辑。

---

### T-21 [Swarm] E2A normalizeTimestampValue 对 float64 取整秒，丢失亚秒精度

**Python 样例：** Python `datetime.fromtimestamp(ts)` 保留微秒。

**修复方案：** 使用 `time.Unix(int64(v), int64((v-float64(int64(v)))*1e9))` 保留纳秒精度。

---

### T-22 [Chroma] truncateText 按字节截断，CJK 字符可能在中间断开

**修复方案：** 使用 `[]rune(s)` 按字符截断。

---

### T-23 [Chroma] toFloat 不支持 uint/int8/int16/int32

**修复方案：** `toFloat` 增加对应 case 分支。

---

### T-24 [Chroma] isNumericString 不支持科学计数法

**修复方案：** 增加 `e`/`E` 后跟可选 `+`/`-` 和数字的支持。

---

### T-25 [样式提交] 多个文件末尾出现空区块分隔注释

**修复方案：** 删除无对应声明的空区块标题；"常数" 改为 "常量"。

---

### T-26 [样式提交] retrieval/vector_store/base.go "常数" 应为 "常量"

**修复方案：** 将 "常数" 改为 "常量"。

---

### T-27 [TeamRunner] _root_task_group_scope 缺失 — task group ContextVar 未绑定

**Python 样例：** `team_runner.py L181` 中通过 `_root_task_group_scope()` 绑定 task group ContextVar，使子任务能被正确管理。

**Go 问题：** Go 代码完全没有 `_root_task_group_scope` 的等价机制。

**修复方案：** 如果 Go 端有 task group 机制需在入口处绑定 context；否则作为架构差异记录。

---

### T-28 [TeamRunner] FinalizeMember 中 defer recover 只捕获 panic，不捕获 error

**Python 样例：** `manager.py L264-309` 中 `try/except Exception` 捕获所有异常。

**Go 问题：** `recover()` 只捕获 panic，不捕获 error。当前代码中 error 已单独处理，实际效果等价。但建议在注释中明确说明 recover 仅防 panic。

---

## 占位代码清单（⤵️ 核心缺失）

| 文件 | 位置 | 描述 | 级别 |
|------|------|------|------|
| `team_manager_spec.go L147` | BuildAgentCustomizer 闭包 | 步骤 a-e: 继承/技能同步/rail注册 | S-39 对应 |
| `team_manager_spec.go L297` | loadTeamSpec | 分布式模式标准化 | S |
| `deep_adapter_team.go L140` | pushTeamSkillEvolveResolutionStatus | 审批结果推送空壳 | S-42 对应 |
| `deep_adapter_team.go L860` | watchTeamEvolutionAndPushTeam | 缺 panic recovery | S-43 对应 |
| `team_agent.go L49-82` | TeamMember Status/UpdateStatus | 全部 stub | S-34 对应 |
| `memory/manager.go` | TeamMemoryManager 5 个生命周期方法 | 全部空实现 | M |
| `memory/extractor.go` | ExtractTeamMemories | 全部空实现 | M |
| `spawn_manager.go L395` | PublishRestartEvent | 仅日志 | M-31 对应 |
| `skilldev/context.go L130` | CreateStageAgent | 返回固定错误 | M |
| `skilldev/context.go L138` | RegisterTools | 返回固定错误 | M |
| `deep_adapter_team.go L617` | TeamStreamLogger | TODO 未对齐 | M-36 对应 |
| `react_invoke.go L257` | Workflow 中断写入流 | ⤵️ 6.12 | M |
| `react_invoke.go L688` | Workflow 中断检测 | ⤵️ 6.11 | M |

---

## context.Background() 滥用清单（非测试代码）

| 文件 | 行号/场景 | 建议 |
|------|-----------|------|
| `spawn_manager.go L76` | baseCtx | 传入调用方 ctx |
| `inprocess_spawn.go L73` | runCtx 缺 session_id | 注入 session_state |
| `kernel.go L577` | closeTeamInteractGate | 使用 context.WithoutCancel |
| `code_agent_rail.go L221` | Reload→Init | 传播调用方 ctx |
| `offload.go L156` | WriteFile | 传入调用方 ctx |
| `processor_state_recorder.go L163` | WriteStream | 添加 ctx 参数 |
| `chroma.go L186` | GetOrCreateCollection | 添加 ctx 参数 |
| `shared_resources.go L84` | NewTeamDatabase | 传入 ctx |
| `remote_skill_util.go L304,430,490` | HTTP GET | 接受 ctx 参数 |
| `release_info.go L52` | PreRun | 接受 ctx 参数 |

---

## P0 必须修复（影响核心功能）

| 编号 | 问题 | 影响 |
|------|------|------|
| S-30 | Stream FinalizeRound 使用已取消 ctx | 内存提取失败 |
| S-31 | Stream 空 queue 路径不调 FinalizeRound | 清理缺失 |
| S-33 | wakeMailboxCb/requestCompletionPollCb 为 nil | Leader POLL_TASK 永不触发 |
| S-34 | TeamMember Status 全 stub | shutdown 检查失效 |
| S-40 | evolutionOutputSchemasToMaps 断言失败 | evolution 完全瘫痪 |
| S-41 | findTeamSkillRail 查找偏离 | 审批路由错误 |
| S-42 | 审批关键字不完整+缺 Sync | 审批匹配/同步失效 |
| S-43/44/45 | panic 无 recovery | goroutine 崩溃/channel 泄漏 |
| S-46 | /evolve 缺 str 分支 | 审批语义错误 |
| S-47 | PascalCase 键名 | HNSW 配置完全失效 |
| S-48 | HNSW 配置未传 | 集合使用默认配置 |
| S-51 | user_id 缺默认值 | 跨用户数据泄露 |
| S-58 | 流式 goroutine 泄露 | Finalize/PostRun 永不执行 |
| S-59 | Finalize 原子性不等价 | 状态不一致 |
| S-60 | inspectSession 吞掉 PreRun 错误 | session 未初始化 |
| S-61 | SpecFromDict 返回默认值 | 损坏 spec 被当作有效 |
| S-62 | leader_member_name 取错字段 | 输出 source_member 错误 |

## P1 应修复（功能偏差/数据丢失）

| 编号 | 问题 | 影响 |
|------|------|------|
| S-32 | Configure 传 nil → Setup 静默失败 | 协调子系统未初始化 |
| S-35 | subscribedTopics 无守卫 | 重复订阅 |
| S-36 | Workspace 重复初始化 | 恢复场景异常 |
| S-37 | SpawnManager baseCtx | 恢复操作无法取消 |
| S-38/39 | Spawn 错误丢失/session_id 缺失 | 执行异常无感知 |
| S-49 | CheckVectorField 空 map | 校验无意义 |
| S-50 | databaseName 未使用 | 多数据库不可用 |
| S-52 | ReMe from_vector_node 跳过 | 数据读取位置错误 |
| S-53 | EventHandlerBase 缺默认实现 | 运行时 panic 风险 |
| S-54 | getTools 忽略错误 | 工具不可用 |
| S-55 | invokeImpl nil result | 调用方 panic |
| S-56/57 | context.Background() 滥用 | 请求取消无效 |
| M-40 | Events channel 竞争 | 监控事件丢失 |
| M-41 | StreamEventRail 内存泄漏 | 长期运行 OOM |
| M-42 | ChannelTransport TOCTOU | 并发 panic |
| M-53 | 非流式路径缺 defer | panic 时资源泄漏 |
| M-57 | DeleteTeam 初始化失败仍继续 | 数据不一致 |
