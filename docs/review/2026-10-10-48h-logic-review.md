# 48h 逻辑审查 — 2026-10-10

> 审查范围：最近一周（10月5日-10月10日）提交记录涉及的实现章节
> — 9.85 TeamRunner、10.3.7-11 TeamHelpers/EvolutionHelpers/适配器辅助、10.6.3-10 Swarm Rails、10.6.12 SessionOps、10.6.13-18 Swarm Memory、13.2 ChromaIndexer、13.4 ComputeChunkEmbeddings
> — 以及回填⤵️标记 + any 安全修复提交 (390e329)
>
> 审查方法：对照 Python 参考项目，逐函数对比方法签名、步骤完整性、⤵️占位代码实际状态

---

## 汇总

| 级别 | 数量 |
|------|------|
| 严重 (S) | 21 |
| 一般 (M) | 29 |
| 提示 (T) | 15 |

---

## 严重问题 (S-01 ~ S-21)

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
// internal/agent_teams/runtime/team_runner.go:417
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

### S-02 TeamRunner: prepareBaseTeam 缺失 str team_id 解析路径

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L669-670
if isinstance(base_team, str):
    return await self._resource_manager.get_agent_team(team_id=base_team)
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go:545-553
func prepareBaseTeam(baseTeam any) (maschema.BaseTeam, error) {
    if team, ok := baseTeam.(maschema.BaseTeam); ok {
        return team, nil
    }
    return nil, fmt.Errorf("run_agent_team(base=True) accepts BaseTeam; got %T. For str team_id use runner.ResourceMgr", baseTeam)
}
```

`string` 类型直接返回错误，而 Python 会通过 `resource_manager.get_agent_team(team_id=...)` 解析。

**修复方案：** 新增 `string` 分支，调用 `runner.GetResourceMgr().GetAgentTeam(ctx, teamID)` 解析为 `BaseTeam` 实例。

---

### S-03 TeamRunner: runAgentTeam/runAgentTeamStreaming finally 块不保证执行

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L183-204
try:
    ...
    return await activation.agent.invoke(inputs, session=activation.session)
finally:
    await self._get_team_runtime_manager().finalize(...)
    await self._close_team_interact_gate(...)
    await activation.session.post_run()
```

Python 的 `try/finally` 保证清理在任何退出路径都执行。

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go:417-423 — 无 defer
result, err := activation.Agent.Invoke(ctx, nil, ...)
_ = GetTeamRuntimeManager().Finalize(ctx, spec.TeamName, activation.Session.GetSessionID())
closeTeamInteractGate(spec.TeamName, activation.Session.GetSessionID())
_ = activation.Session.PostRun(ctx)
```

若 `Invoke` panic 或中间某步骤 panic，后续清理步骤不执行。流式版本在 goroutine 中同样无 defer 保护。

**修复方案：** 非流式路径用 `defer` 包裹 finally 逻辑；流式路径在 goroutine 中用 `defer` 保证清理顺序。

---

### S-04 TeamRunner: RegisterHumanAgentInbound 缺失包级函数

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L878-892
@classmethod
async def register_human_agent_inbound(cls, *, team_name, session_id, member_name, callback) -> bool:
    return await _global_runner().register_human_agent_inbound(...)
```

**Go 问题：** `team_runner.go` 中无 `RegisterHumanAgentInbound` 包级函数。`TeamRuntimeManager.RegisterHumanAgentInbound` 方法存在于 `manager.go:621`，但未暴露。

**修复方案：** 在 `team_runner.go` 添加门面函数：
```go
func RegisterHumanAgentInbound(ctx context.Context, teamName, sessionID, memberName string, callback any) (bool, error) {
    return GetTeamRuntimeManager().RegisterHumanAgentInbound(ctx, teamName, sessionID, memberName, callback)
}
```

---

### S-05 DeepAdapter: evolutionOutputSchemasToMaps 类型断言永远失败 — evolution 监控全部失效

**Python 样例：**
```python
# team_helpers.py — evolution 事件从 watcher 获取，类型为 list[dict]
events = await watcher.drain_pending_approval_events()
for event in events:
    # event 是 dict，直接使用
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go:1175
func evolutionOutputSchemasToMaps(events any) []map[string]any {
    if typed, ok := events.([]any); ok {  // ← 永远 false
```

`DrainPendingApprovalEvents` 返回 `[]*stream.OutputSchema`，但断言为 `[]any`。Go 中 `[]*stream.OutputSchema` ≠ `[]any`，断言永远为 false，函数始终返回 nil。

**影响：** `watchTeamEvolutionAndPushTeam` 的核心循环永远看不到事件，evolution 监控完全失效。

**修复方案：** 改为接受 `[]*stream.OutputSchema` 参数，或先转为 `[]any`：
```go
func evolutionOutputSchemasToMaps(events any) []map[string]any {
    if typed, ok := events.([]*stream.OutputSchema); ok {
        // 转换逻辑
    }
    return nil
}
```

---

### S-06 DeepAdapter: EnsureEventBroadcastInjected 全局单例不区分 channelID — 多 channel 失效

**Python 样例：**
```python
# team_helpers.py:181-196 — _broadcast_event 是模块级函数，无需注入
```

**Go 问题：**
```go
// internal/swarm/server/adapter/team_helpers.go:62-80
var eventBroadcastInitialized bool  // 全局 bool
func EnsureEventBroadcastInjected(channelID string) {
    if eventBroadcastInitialized { return }  // 只注入第一个 channel
```

第一个 channelID 注入后，后续不同 channelID 的 TeamManager.onEventBroadcast 为 nil。

**影响：** 多 channel 并发场景下，除第一个 channel 外的 monitor/evolution 事件无法广播。

**修复方案：** 改为 `map[string]bool` 按 channelID 追踪，或去掉全局标志每次都注入（SetOnEventBroadcast 可幂等）。

---

### S-07 DeepAdapter: eventBroadcastInitialized 无并发保护 — 数据竞争

**Go 问题：**
```go
// internal/swarm/server/adapter/team_helpers.go:62
var eventBroadcastInitialized bool  // 无锁保护
```

`processTeamMessageStream` 可从多个 goroutine 并发调用，`eventBroadcastInitialized` 被并发读写。

**修复方案：** 使用 `sync.Once` 或 `sync.Mutex` 保护，结合 S-06 的 per-channelID map 方案一并修复。

---

### S-08 DeepAdapter: for-select 中 break 只跳出 select 不跳出 for 循环 — team.error 不终止流

**Python 样例：**
```python
# team_helpers.py — while 循环中 break 跳出 while
if event_type == "team.error":
    break  # 跳出 while 循环
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go:404
select {
case evt, ok := <-eventCh:
    ...
    if eventType == "team.error" {
        break  // 只跳出 select，for 循环继续！
    }
```

Go 的 `break` 在 `select` 中只退出 `select` 块，不退出 `for` 循环。

**影响：** team.error 事件后流不会终止，客户端持续等待直到 stream task 被外部取消。

**修复方案：** 使用带标签的 break 或 goto：
```go
streamDone:
    // 清理逻辑
for {
    select {
    case evt, ok := <-eventCh:
        if eventType == "team.error" {
            goto streamDone
        }
    }
}
```

---

### S-09 DeepAdapter: AttachDistributedHooksForRunnerRuntime 第三个参数传 nil（channelID 丢失）

**Python 样例：**
```python
# team_helpers.py:963-967
await tm.attach_distributed_hooks_for_runner_runtime(team_name=, session_id=, channel_id=channel_id)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go:699
tm.AttachDistributedHooksForRunnerRuntime(readyTeamName, sessionID, nil)  // channelID 传 nil
```

**修复方案：** 传入 channelID 变量（`channelID` 在 `processTeamMessageStream` 开头已解析）。

---

### S-10 ChromaIndexer: 未持有 client 引用，每个方法都重建 VectorStore

**Python 样例：**
```python
# chroma_indexer.py:90-93
self._client = ChromaVectorStore.create_client(database_name=self.database_name, path_or_uri=chroma_path)
# 后续方法直接用 self._client.get_collection / self._client.get_or_create_collection
```

**Go 问题：** `ChromaIndexer` 结构体无 client 字段，`BuildIndex`/`DeleteIndex`/`IndexExists`/`GetIndexInfo` 每次调用都 `NewChromaVectorStore()` 创建新客户端+新集合引用，开销极大且可能泄漏资源。

**修复方案：** ChromaIndexer 结构体增加 `client chromav2.Client` 和 `databaseName string` 字段，在 NewChromaIndexer 中创建 client 并存储。各方法通过 `ci.client.GetCollection()` / `ci.client.GetOrCreateCollection()` 获取 collection。

---

### S-11 ChromaIndexer: UpdateIndex 异常处理与 Python 不一致

**Python 样例：**
```python
# chroma_indexer.py:198-215
async def update_index(self, chunks, doc_id, config, embed_model=None, **kwargs):
    try:
        await self.delete_index(doc_id, config.index_name)
        return await self.build_index(chunks, config, embed_model, **kwargs)
    except Exception as e:
        logger.error(f"Failed to update index: {e}")
        return False  # 吞掉异常，返回 False
```

**Go 问题：**
```go
// chroma.go:282-296
_, err := ci.DeleteIndex(ctx, docID, config.IndexName)
if err != nil {
    return false, err  // 传播了错误，与 Python 不同
}
```

**修复方案：** UpdateIndex 中无论 DeleteIndex 还是 BuildIndex 出错，都记录日志后 `return false, nil`，对齐 Python 吞掉异常的行为。DeleteIndex 也需同样处理（S-12）。

---

### S-12 ChromaIndexer: DeleteIndex 异常处理与 Python 不一致

**Python 样例：**
```python
# chroma_indexer.py:217-253
async def delete_index(self, doc_id, index_name, **kwargs):
    try:
        ...
        return len(results["ids"]) > 0
    except Exception as e:
        logger.error(f"Failed to delete index entries: {e}")
        return False  # 吞掉异常
```

**Go 问题：** `DeleteIndex` 在 Get/Delete 失败时返回 `error`，会在调用方产生不同行为。

**修复方案：** DeleteIndex 内部捕获所有异常，记录日志后 `return false, nil`，对齐 Python 行为。

---

### S-13 ChromaVectorStore: CheckVectorField 传入空 actual 配置，校验永远失败

**Python 样例：**
```python
# chroma_store.py:131-133
def check_vector_field(self) -> None:
    self._check_configs_matching(self._construct_config, self.collection.configuration.get("hnsw", {}))
```

**Go 问题：**
```go
// chroma.go:219-221
func (s *ChromaVectorStore) CheckVectorField() error {
    return CheckConfigsMatching(s.constructConfig, map[string]any{})  // actual 传空 map
}
```

空 `map[string]any{}` 作为 actual，意味着任何配置键都被判断为不匹配，方法完全无效。

**修复方案：** 从 collection 获取实际的 HNSW 配置作为 actual 参数传入。

---

### S-14 ChromaIndexer: IndexExists 使用 get_or_create 可能隐式创建空集合

**Python 样例：**
```python
# chroma_indexer.py:255-268
async def index_exists(self, index_name):
    try:
        await asyncio.to_thread(self._client.get_collection, name=index_name)
        return True
    except Exception:
        return False
```

Python 用 `get_collection`（只读），Go 用 `NewChromaVectorStore`（内部是 `get_or_create_collection`），可能隐式创建空集合。

**修复方案：** 应使用 `client.ListCollections()` 或 `client.GetCollection()`（非 get_or_create）来判断是否存在。

---

### S-15 Swarm Memory: config.py 完全缺失（MemorySettings/embed/is_memory_enabled 等）

**Python 样例：**
```python
# jiuwenswarm/agents/harness/common/memory/config.py (259行)
class MemorySettings:
    ...
def is_memory_enabled(): ...
def is_proactive_memory(): ...
def get_memory_mode(): ...
def create_memory_settings(): ...
```

**Go 问题：** `/internal/swarm/agents/harness/common/memory/` 仅有 `forbidden.go` + `dreaming/` 子包，`config.go` 完全缺失。这些是记忆系统的配置基础设施，其他模块（manager/embeddings/external）都依赖它。

**修复方案：** 新建 `config.go`，对齐 Python `MemorySettings` + `_load_config()` + `_resolve_env_vars()` + `clear_config_cache()` + `get_embed_config()` + `create_memory_settings()` + `_resolve_mode_memory()` + `is_memory_enabled()` + `is_proactive_memory()` + `get_memory_mode()`。

---

### S-16 Swarm Memory: embeddings.py 完全缺失（EmbeddingProvider/OpenAI/Mock）

**Python 样例：**
```python
# jiuwenswarm/agents/harness/common/memory/embeddings.py (165行)
class EmbeddingProvider(ABC):
    async def embed(self, text: str) -> list[float]: ...
class OpenAICompatibleEmbeddingProvider(EmbeddingProvider): ...
class MockEmbeddingProvider(EmbeddingProvider): ...
def create_embedding_provider(memory_settings) -> EmbeddingProvider: ...
```

**Go 问题：** 完全缺失，向量搜索核心依赖不存在。

**修复方案：** 新建 `embeddings.go`，对齐 Python 四个类型。

---

### S-17 Swarm Memory: manager.py 完全缺失（MemoryIndexManager/SQLite/vec/FTS/搜索）

**Python 样例：**
```python
# jiuwenswarm/agents/harness/common/memory/manager.py (1224行)
class MemoryIndexManager:
    def __init__(self, ...): ...  # SQLite + sqlite-vec + FTS5 初始化
    async def index_file(self, ...): ...
    async def search(self, ...): ...  # 混合搜索（vector + keyword）
    async def sync_session_files(self, ...): ...
```

**Go 问题：** 1224 行的完整记忆索引管理器完全缺失。

**修复方案：** 新建 `manager.go`，实现 SQLite + vec + FTS5 的混合搜索索引管理器。这是最大的缺失，建议分阶段实现。

---

### S-18 Swarm Memory: internal.py 完全缺失（chunk/fts/cosine 等工具）

**Python 样例：**
```python
# jiuwenswarm/agents/harness/common/memory/internal.py (215行)
def estimate_tokens(text): ...
def chunk_markdown(text, chunk_size, overlap): ...
def build_fts_query(query): ...
def cosine_similarity(a, b): ...
```

**Go 问题：** 完全缺失。

**修复方案：** 新建 `internal.go`，对齐 Python 工具函数。

---

### S-19 Swarm Memory: types.py 完全缺失 + external_memory_builder/config.py 完全缺失

**Python 样例：**
```python
# types.py (79行)
class MemorySource: ...
class MemorySearchResult: ...
class MemoryChunk: ...
class FileEntry: ...

# external_memory_builder.py (142行)
def build_external_memory_rail(...): ...

# external_memory_config.py (126行)
def get_memory_engine(): ...
def is_builtin_memory_allowed(): ...
def is_external_memory_allowed(): ...
```

**Go 问题：** 三个文件完全缺失。

**修复方案：** 新建 `types.go` + `external_memory_builder.go` + `external_memory_config.go`，对齐 Python。

---

### S-20 Permissions 子包: 缺少 BuildPermissionRail 统一入口函数

**Python 样例：**
```python
# interrupt_helpers.py
def build_permission_rail():
    # 整合 PermissionInterruptRail 创建，包括 4 个闭包
    _persist_allow_rule, _resolve_session_id, _request_permission_confirmation, _permission_scene_hook
```

**Go 问题：** `permissions/` 包拆分成了 `owner_scopes.go`、`permissions_config_rpc.go`、`permissions_persist.go` 3 个文件，但没有一个统一的 `BuildPermissionRail()` 函数将它们组装起来。

**修复方案：** 在 `permissions/` 包新增 `permission_rail_builder.go`，提供 `BuildPermissionRail()` 函数。

---

### S-21 TeamHelpers: watchTeamEvolutionAndPushTeam 推送功能为空壳（仅日志不推送）

**Python 样例：**
```python
# team_helpers.py:1101-1393
push_evolution_status(...)  # 通过 EvolutionPushContext.transport.SendPush() 实际推送
push_evolution_event(...)   # 通过 EvolutionPushContext.transport.SendPush() 实际推送
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go:808-817
func (d *DeepAdapter) pushEvolutionStatus(...) {
    // 仅写日志，标注 TODO(#9.85) 但实际不推送
}
// L836-855
func (d *DeepAdapter) pushEvolutionEvent(...) {
    // 仅写日志，标注 TODO(#9.85) 但实际不推送
}
```

**修复方案：** 注入 `sendPushFunc`（与 `pushEventToFrontend` 相同模式），通过 `sessionmd.BuildServerPushMessage` 构建消息后推送。

---

## 一般问题 (M-01 ~ M-29)

### M-01 TeamRunner: context/envs 参数缺失

Python `run_agent_team` 签名包含 `context=None, envs=None` 参数，Go 只有 `ctx, agentTeam, inputs, base, member, sess`，缺少 `modelCtx` 和 `envs` 参数。

**修复方案：** 添加 `modelCtx any` 和 `envs map[string]any` 参数，或使用 Functional Options 模式。

---

### M-02 TeamRunner: bindInteractTeamSession 不检查当前 ctx 中是否已有匹配的 session ID

**Python 样例：**
```python
# team_runner.py:130-146
if session_id and get_session_id() != session_id:
    token = set_session_id(session_id)
```

Python 先检查当前值是否已匹配再设置。Go 的 `bindInteractTeamSession` 无条件 `InitSessionState()` + `SetSessionID()`。

**修复方案：** 检查 `sessionctx.FromContext(ctx)` 是否已持有相同 sessionID，若是则直接返回 `ctx`。

---

### M-03 TeamRunner: closeTeamInteractGate 使用 context.Background()

**Go 问题：** `team_runner.go:577` — `_ = entry.InteractGate.CloseAndDrain(context.Background())`。如果 inflight 永不归零，goroutine 将永远阻塞。

**修复方案：** 使用调用方的 `ctx` 或带超时的 context。

---

### M-04 TeamRunner: RunAgentTeamStreaming 返回 nil channel 导致调用方永久阻塞

**Go 问题：** `team_runner.go:453` — `IsTeamRejectKind` 为 true 时返回 `(nil, nil)`。nil channel 会导致调用方 `range` 永久阻塞。

**修复方案：** 返回一个立即关闭的 channel，或在 channel 中发送一个拒绝事件帧。

---

### M-05 TeamHelpers: /evolve 命令缺少 isinstance(evolve_result, str) 分支

**Python 样例：**
```python
# team_helpers.py:597-601
if isinstance(evolve_result, str) and evolve_result.strip():
    return {"output": f"Skill '{skill_name}' 演进请求已提交，请等待审批。", "result_type": "answer"}
```

**Go 问题：** `handleEvolveCommand` 直接处理 `result.ApprovalEvent`，无字符串分支判断。

**修复方案：** 检查 `rail.RequestUserEvolution` 返回值是否可能为字符串，增加对应分支。

---

### M-06 TeamHelpers: /evolve 命令中 approvalResultFromEventOrItems 传 nil items

**Python 样例：**
```python
# team_helpers.py:602-608
items=list(getattr(evolve_result, "records", []) or [])
```

**Go 问题：** `team_helpers.go:333` 直接传 `nil` 作为 items 参数。

**修复方案：** 从 evolveResult 提取 Records 字段传入。

---

### M-07 TeamHelpers: TEAM_EVOLUTION_EVENT_TIMEOUT_SEC / IDLE_SLEEP_SEC 常量与 Python 不一致

**Python 参考值：** `TEAM_EVOLUTION_IDLE_SLEEP_SEC = 1.0` (秒), `TEAM_EVOLUTION_EVENT_TIMEOUT_SEC = 900.0` (秒)

**Go 当前值：** `teamEvolutionIdleSleepSec = 500` (毫秒=0.5s), `teamEvolutionEventTimeoutSec = 60.0` (秒)

**修复方案：** 对齐 Python 值：`teamEvolutionIdleSleepSec = 1000`（1秒），`teamEvolutionEventTimeoutSec = 900.0`。

---

### M-08 TeamHelpers: HandleTeamEvolveListCommand 缺少 Used/Effect 列

**Python 样例：**
```python
# team_helpers.py:462-487 — 表格包含 Used 和 Effect 列
# Used: times_used/times_presented
# Effect: times_positive/times_negative
```

**Go 问题：** 表格只有 `# | Score | Section | Content (preview)`，缺少 Used 和 Effect 列。

**修复方案：** 从 `record.UsageStats` 提取数据，补充列。

---

### M-09 TeamHelpers: resolveEvolutionEventTimeoutSec 永远返回 fallback

**Go 问题：** `deep_adapter_team.go:1275-1289` 直接 return fallback，未从 rail 读取 `evolution_total_timeout_secs`。

**修复方案：** 检查 `TeamSkillEvolutionRail` 是否有 `EvolutionTotalTimeoutSecs` 字段，如有则读取；如无则添加。

---

### M-10 TeamHelpers: pushTeamSkillEvolveResolutionStatus 为空壳

**Go 问题：** `deep_adapter_team.go:140-148` 仅记录日志，不推送审批结果到前端。

**修复方案：** 使用 `d.getSendPushFunc()` 推送审批结果到前端。

---

### M-11 TeamHelpers: consumeStreamWithQuery 缺少 envs 和 streamLogger 参数

**Python 样例：**
```python
# team_helpers.py:932-938
Runner.run_agent_team_streaming(agent_team=team_spec, inputs=..., session=session_id, envs=envs, stream_logger=lg)
```

**Go 问题：** `deep_adapter_team.go:631` — envs 和 streamLogger 未传入。

**修复方案：** 传入 streamEnvs 和 streamLogger 参数。

---

### M-12 DeepAdapter: modelCache 类型断言不安全

**Go 问题：** `parentAgent.(interface{ ModelCache() map[string]*llm.Model })` 断言失败时 modelCache 为 nil，agentDefToSubagentConfig 传入 nil modelCache 会导致子 agent 创建 fallback 到默认模型，但无任何告警。

**修复方案：** modelCache 为 nil 时加 Warn 日志；或让 `BaseAgent` 接口直接包含 `ModelCache()` 方法。

---

### M-13 DeepAdapter: consumeMonitorEvents 死代码

**Go 问题：** `deep_adapter_team.go:749` — `d.consumeMonitorEvents` 无任何调用方。实际使用的是 `TeamManager.consumeMonitorEvents`。

**修复方案：** 删除此死代码。

---

### M-14 DeepAdapter: sharedRuntime 创建时 TeamID 硬编码为 "default"

**Go 问题：** `shared_resources.go:48-50` — `teamruntime.NewTeamRuntime(teamruntime.RuntimeConfig{TeamID: "default"})`。

**修复方案：** 从配置或环境变量获取 TeamID，或添加注释说明。

---

### M-15 DeepAdapter: processTeamMessageStream 缺少 wait_for_pending_shutdown_cleanup_for_session

**Python 样例：**
```python
# team_helpers.py:625-636
await wait_for_pending_shutdown_cleanup_for_session(session_id)
```

**Go 问题：** Go 端 `processTeamMessageStream` 未调用对应等待逻辑。如果上一次 shutdown 清理未完成就发起新的 team 消息，可能导致状态不一致。

**修复方案：** 在 TeamManager 暴露此方法或同步等待清理完成的机制。

---

### M-16 ChromaVectorStore: HNSW searchConfig 合并丢失

**Python 样例：**
```python
# chroma_store.py:99-102
self._collection = self._client.get_or_create_collection(
    configuration={"hnsw": self._construct_config | self._search_config},
)
```

**Go 问题：** `NewChromaVectorStore` 只传 `chromav2.WithHNSWSpaceCreate(mapToChromaDistanceMetric(...))`，searchConfig（如 efSearch 的 extra_search 参数）完全丢失。

**修复方案：** 调研 chromav2 API 是否支持传入完整 HNSW 配置；如不支持，添加 TODO 标记。

---

### M-17 ChromaVectorStore: addBatch 中 metadata 解析 JSON 失败时无警告日志

**Python 样例：**
```python
# chroma_store.py:213-215
except Exception:
    logger.warning(f"Failed to load metadata: {raw_metadata}")
```

**Go 问题：** `json.Unmarshal` 失败时静默跳过，无任何日志。

**修复方案：** 在 json.Unmarshal 失败时添加 Warn 日志。

---

### M-18 ChromaVectorStore: sparse_vector_field 反序列化缺失

**Python 样例：**
```python
# chroma_store.py:468-473
if self.sparse_vector_field in metadata:
    sparse_vec = json.loads(metadata[self.sparse_vector_field])
    metadata[self.sparse_vector_field] = sparse_vec
```

**Go 问题：** `chromaResultToSearchResults` 完全没有处理 sparse_vector_field 的反序列化。

**修复方案：** 添加 sparse_vector_field 的 JSON 反序列化逻辑和警告日志。

---

### M-19 ChromaVectorStore: database_name 自定义数据库未处理

**Python 样例：**
```python
# chroma_store.py:119-129
if database_name and database_name != DEFAULT_DATABASE:
    admin_client = chromadb.AdminClient(...)
    admin_client.create_database(database_name)
```

**Go 问题：** `NewChromaVectorStore` 忽略了 `s.databaseName` 字段。

**修复方案：** 调研 chromav2 API 是否支持自定义数据库，添加对应逻辑或 TODO。

---

### M-20 ProjectMemoryRail: BuildProjectMemorySection 缺少 language 参数

**Python 样例：**
```python
# project_memory_rail.py:186-192
section = build_project_memory_section(merged, language=self._language, priority=self.SECTION_PRIORITY)
```

**Go 问题：** `project_memory_rail.go:281` — `section := project_memory.BuildProjectMemorySection(merged, sectionPriority)` 缺少 language 参数。

**注：** Python 中 `BuildProjectMemorySection` 的 language 参数标记 `del language # accepted for API compat, not used`，所以 Go 当前实现功能上无差异，但签名不一致。

---

### M-21 SessionOps: getUniqueForkName 数字解析 bug

**Go 问题：**
```go
// session_ops.go:820-824
if num, err := fmt.Sscanf(m[1], "%d", new(int)); err == nil && num == 1 {
    var n int
    fmt.Sscanf(m[1], "%d", &n)  // 重复解析，第一次结果被丢弃
    usedNumbers[n] = true
}
```

**修复方案：** `n, err := strconv.Atoi(m[1]); if err == nil { usedNumbers[n] = true }`

---

### M-22 SessionOps: ListSessionTurns 的 preview 截断使用字节而非 rune

**Python 样例：**
```python
# session_ops_service.py:281
preview = cleaned.strip()[:80]  # Python 按字符截断，CJK 安全
```

**Go 问题：** `session_ops.go:414` — `if len(preview) > 80 { preview = preview[:80] }` — Go `len()` 返回字节数，CJK 字符可能被截断在中间。

**修复方案：** 使用 `[]rune` 截断：`if len([]rune(preview)) > 80 { preview = string([]rune(preview)[:80]) }`

---

### M-23 TeamManager: EnsureTeamSharedSkillsInitialized 两个步骤均为 TODO 空壳

**Go 问题：** `team_manager_skill.go:27-35` — `_copy_global_skills_to_team_shared_dir` 和 `_sync_team_shared_skills_to_agent_global` 两个步骤只打日志，无实际实现。

**修复方案：** 实现文件系统复制/同步逻辑，对齐 Python。

---

### M-24 TeamManager: SyncTeamSkills 中 _sync_skills_dir 未实现

**Go 问题：** `team_manager_skill.go:52` — `⤵️(#9.72) _sync_skills_dir — 待 shutil / Path 等效实现回填`

**修复方案：** 使用 Go 的 `io.Copy` + `filepath.Walk` 实现 shutil 等效目录同步。

---

### M-25 TeamManager: updateEvolutionConfig 中 _build_and_mount_member_rails_for_context 未实现

**Go 问题：** `team_manager_skill.go:240-250` — `_build_and_mount_member_rails_for_context 待回填`

**修复方案：** 对齐 Python 实现 skill create rail 挂载逻辑。

**状态更新（2026-10-14）：** 标记已从 `TODO(#9.72)` 改为 `⤵️(#10.6.22)`（A2X RegistryRuntime 章节），实现延后至该章节统一回填。

---

### M-26 TeamManager: unregisterLiveRail 使用 context.Background()

**Go 问题：** `team_manager_skill.go:314` — `entry.Agent.UnregisterRail(context.Background(), rail)` 应使用传入的 ctx。

**修复方案：** 给 `unregisterLiveRail` 添加 `ctx context.Context` 参数。

---

### M-27 UserInbox/HumanAgentInbox: 所有方法使用 context.Background()

**Go 问题：** `user_inbox.go:60,81,110` 和 `human_agent_inbox.go:120,223,232` — 6 处 `context.Background()` 替代了应有 ctx 传播的位置。

**修复方案：** 给 `Direct`/`Broadcast`/`DeliverToLeader` 添加 `ctx context.Context` 参数；给 `HumanAgentInbox` 的方法同理。

---

### M-28 agent_teams/agent/member.go: 4 个 TODO(#9.65) DB 操作未实现

**Go 问题：** `member.go:50,57,67,78` — `SetMemberStatus`/`SetMemberExecutionStatus` 等方法从 DB 读写状态的逻辑全为 TODO 占位。

**修复方案：** 对齐 Python，实现 DB 读写 + 事件发布逻辑。

---

### M-29 agent_teams/agent/session_manager.go: session 参数类型为 any

**Go 问题：** `session_manager.go:103` — `session any, // TODO(#9.59): AgentTeamSession 接口` — 参数类型未升级为具体类型。

**修复方案：** 定义 `AgentTeamSession` 接口后替换 any。

---

## 提示问题 (T-01 ~ T-15)

### T-01 TeamRunner: RunTeamMember 类型断言限制为 *agent.TeamAgent

Python 接受任何 `BaseAgent`，Go 强制断言为 `*agent.TeamAgent`。当前可接受，若未来有其他 BaseAgent 实现需放宽。

---

### T-02 TeamRunner: buildTeamRuntimeReadyChunk leader_role 硬编码为 TeamRoleLeader

Python 从 `blueprint` 动态读取 `role` 属性，Go 硬编码。建议从 `AgentCard()` 中尝试读取。

---

### T-03 TeamRunner: _root_task_group_scope 缺失（已确认的差异）

Go 使用全局 `utils.GetTaskManager()` 替代 Python 的 ContextVar 传播，设计合理，无需逐方法绑定。

---

### T-04 ChromaIndexer: callback 类型校验缺失

Python 校验 `doc_index_callback` 必须是 `type[BaseCallback]`，Go 无此校验。Go 的接口隐式实现使此校验意义不大。

---

### T-05 ChromaIndexer: RRF Fusion 中 rank 起始值确认对齐正确

Go 用 `rank+1` 补偿了索引差异（Go 从 0 开始，Python 从 1 开始），逻辑正确。无需修改。

---

### T-06 StreamEventRail: emitContextUsage 中 ResolveContextMax fallback 值

Go 端调用 `ResolveContextMax(modelName, 0, nil)`。经确认，`ResolveContextMax` 最终返回 `DefaultContextMaxTokens=200000`，不会返回 0。与 Python 行为一致。

---

### T-07 StreamEventRail: _stream_tasks 字段缺失

Python `self._stream_tasks: set[asyncio.Task] = set()` 是预留字段，Python 中也未被使用。Go 可忽略。

---

### T-08 AvatarPromptRail: _build_memory_disabled_prompt 未使用函数缺失

Python 中此函数从未被调用，Go 缺失不影响功能。

---

### T-09 Forbidden: Description map 缺少 "cn" 键

Go 同时接受 `"zh"` 和 `"cn"` 判断，但 Description map 只有 `"zh"` 和 `"en"` 键。建议统一添加 `"cn"` 键。

---

### T-10 SysOpBuilder: 环境变量名 UAPCLAW vs JIUSWARM

Go 使用 `UAPCLAW_SANDBOX_PROJECT_DIR`，Python 使用 `JIUSWARM_SANDBOX_PROJECT_DIR`。如果项目规范要求替换为 UAPCLAW 前缀则无需修改。

---

### T-11 TeamStreamLogger 未实现

`deep_adapter_team.go:626` — `TODO: TeamStreamLogger 对齐`。当前流式 trace 记录功能缺失。

---

### T-12 常量名 teamEvolutionIdleSleepSec 含义与值不符

名称含 "Sec" 但值为 500 毫秒。建议重命名为 `teamEvolutionIdleSleepMs` 或改为 `500 * time.Millisecond`。

---

### T-13 utilsParseStreamChunk 仅原样返回

Python 的 `parse_stream_chunk` 对 chunk 做了字段提取/转换，Go 端直接返回输入。需确认是否与前端期望一致。

---

### T-14 inprocess_spawn.go: runTeamMemberFunc 仍为 any 类型

回填只增加了断言失败的 Error 日志，但 `runTeamMemberFunc` 仍为 `any` + 类型断言。建议改为具体函数类型。

---

### T-15 DeepAdapter: watchEvolutionAndPush 中 PushEvolutionProgress 缺少 status push

`deep_adapter_evolution.go:194-203` 有部分推送逻辑，但仅处理非审批非结果事件，缺少 status push。需统一 watchTeamEvolutionAndPushTeam 和 watchEvolutionAndPush 的推送路径。

---

## 按章节汇总

| 章节 | S | M | T | 关键问题 |
|------|---|---|---|---------|
| 9.85 TeamRunner | 4 | 4 | 3 | inputs传nil、finally不保证、str解析缺失、HumanAgentInbound缺失 |
| 10.3.7-11 TeamHelpers/Adapter | 6 | 10 | 5 | evolution断言失败、多channel回调缺失、for-select break错误、channelID传nil、推送空壳 |
| 10.6.3-10 Swarm Rails | 1 | 2 | 4 | BuildPermissionRail缺失 |
| 10.6.12 SessionOps | 0 | 2 | 0 | getUniqueForkName数字解析bug、CJK截断 |
| 10.6.13-18 Swarm Memory | 5 | 0 | 0 | config/embeddings/manager/internal/types+builder+config共7文件完全缺失 |
| 13.2 ChromaIndexer | 5 | 4 | 2 | 无client引用、UpdateIndex/DeleteIndex异常传播、CheckVectorField空actual、IndexExists隐式创建 |
| 回填提交审查 | 0 | 0 | 1 | any类型残留 |
| **合计** | **21** | **22** | **15** | |

> 注：M 级额外补充了 7 项（M-23~M-29），总计 29 项 M。

---

## P0 修复优先级

1. **S-01** TeamRunner inputs 传 nil — 用户 query 丢失，直接影响功能
2. **S-05** evolutionOutputSchemasToMaps 断言失败 — evolution 监控完全失效
3. **S-08** for-select break 语义错误 — team.error 后流不终止
4. **S-04** TeamRunner finally 不保证 — panic 时清理丢失
5. **S-11/S-12** ChromaIndexer Update/Delete 异常传播 — 调用方行为与 Python 不一致
6. **S-06/S-07** 多 channel 回调缺失 + 数据竞争 — 并发场景失效
7. **S-09** channelID 传 nil — 分布式 hooks 绑定失败
8. **S-13** CheckVectorField 空 actual — 向量配置校验完全无效

---

## 功能缺失清单（按影响面排序）

| 缺失模块 | Python 行数 | 影响 | 建议 |
|----------|------------|------|------|
| Swarm Memory manager.py | 1224 | 记忆索引管理核心 | 分阶段实现 |
| Swarm Memory config.py | 259 | 配置基础设施 | 优先实现 |
| Swarm Memory internal.py | 215 | 工具函数 | 优先实现 |
| Swarm Memory types.py + external_memory_builder + external_memory_config | 347 | 类型+外部记忆集成 | 同步实现 |
| Swarm Memory embeddings.py | 165 | 向量嵌入 | 同步实现 |
| BuildPermissionRail 统一入口 | ~100 | 权限 Rail 组装 | 单文件实现 |
