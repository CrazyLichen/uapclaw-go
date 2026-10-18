# 48h 逻辑审查 — 2026-10-11

> 审查范围：48小时内提交记录涉及的实现章节
> - 9.85 TeamRunner（runtime/dispatch + team_runner + manager Activate 内联回填）
> - 13.2 ChromaIndexer（retrieval/indexing 完整索引管道）
> - deep_adapter_team（team 模式流处理 + evolution 推送 + slash 命令）
> - team_helpers 重写（processTeamMessageStream + consumeStreamWithQuery + evolution 监控）
> - Extra() 并发修复、VectorStore any 消除等修复
>
> 审查方法：对照 Python 参考项目（jiuwenswarm + openjiuwen），逐函数对比方法签名、步骤完整性、⤵️占位代码实际状态

---

## 汇总

| 级别 | 数量 |
|------|------|
| 严重 (S) | 22 |
| 一般 (M) | 18 |
| 提示 (T) | 9 |
| **合计** | **49** |

---

## 严重问题 (S-01 ~ S-22)

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
result, err := activation.Agent.Invoke(ctx, inputs, agentinterfaces.WithSession(activation.Session))
agentCh, streamErr := activation.Agent.Stream(ctx, inputs, agentinterfaces.WithSession(activation.Session))
```
注意 `RunAgentTeam` 函数签名已有 `inputs any` 参数，只需在 `runAgentTeam` 内部将其转为 `map[string]any` 后传递。

---

### S-02 TeamRunner: Finalize/closeGate/PostRun 不在 defer 中，资源泄漏风险

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L196-204
try:
    result = await activation.agent.invoke(inputs, session=activation.session)
    return result
finally:
    await self.finalize(activation)
    await self._close_team_interact_gate(activation)
    await activation.session.post_run()
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L417-421
result, err := activation.Agent.Invoke(ctx, nil, ...)
// 以下三行不在 defer 中，如果 Invoke panic 或返回后 cleanup panic，后续清理不会执行
GetTeamRuntimeManager().Finalize(ctx, activation)
closeTeamInteractGate(activation)
_ = activation.Session.PostRun(ctx)
```

受影响位置共 6 处：
| 函数 | 行号 | 问题 |
|------|------|------|
| `runAgentTeam` | L419-421 | Finalize+closeGate+PostRun 不在 defer |
| `runAgentTeamStreaming` | L494-496 | goroutine 内 Finalize+closeGate+PostRun 不在 defer |
| `RunBaseTeam` | L139 | PostRun 不在 defer |
| `RunBaseTeamStreaming` | L198-201 | goroutine 内 Unbind+PostRun 不在 defer |
| `RunTeamMember` | L238-242 | FinalizeMember+Unbind+PostRun 不在 defer |
| `RunTeamMemberStreaming` | L292-296 | goroutine 内 FinalizeMember+Unbind+PostRun 不在 defer |

**修复方案：** 在进入执行路径时立即用 `defer` 注册清理函数：
```go
defer func() {
    GetTeamRuntimeManager().Finalize(ctx, activation)
    closeTeamInteractGate(activation)
    _ = activation.Session.PostRun(ctx)
}()
result, err := activation.Agent.Invoke(ctx, inputs, ...)
```

---

### S-03 TeamRunner: Finalize 缺失顶层异常保护

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L203-229
try:
    # 决策逻辑：判断 lifecycle/shutdown/requested
    if ag.is_shutdown_requested():
        ...
    elif ag.lifecycle() == ...:
        ...
except Exception as exc:
    team_logger.warning("finalize failed: %s", exc)
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L285-318
// Finalize 函数无 recover 保护，如果 ag.IsShutdownRequested() 或 ag.Lifecycle() panic，进程崩溃
func (m *TeamRunnerManager) Finalize(ctx context.Context, activation *Activation) {
    ag := activation.Agent
    if ag.IsShutdownRequested() { ... }
```

**修复方案：** 在 Finalize 入口处加 `defer recover`：
```go
func (m *TeamRunnerManager) Finalize(ctx context.Context, activation *Activation) {
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(logComponent).Any("recover", r).Msg("Finalize panic recovered")
        }
    }()
    ...
}
```

---

### S-04 TeamRunner: prepareBaseTeam 不支持 str team_id 解析

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L669-670
if isinstance(base_team, str):
    return await self._resource_manager.get_agent_team(team_id=base_team)
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L545-553
// 对 str 类型直接返回错误，无法通过 team_id 字符串解析到 BaseTeam 实例
```

**修复方案：** 在 `prepareBaseTeam` 中增加 string 类型分支，通过 ResourceManager 查找：
```go
if teamID, ok := baseTeam.(string); ok {
    resolved, err := m.resourceManager.GetAgentTeam(teamID)
    if err != nil { return nil, err }
    baseTeam = resolved
}
```

---

### S-05 ChromaIndexer: 不缓存 client，每次操作重新创建 VectorStore

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/chroma_indexer.py L90-93
self._client = ChromaVectorStore.create_client(
    database_name=self.database_name,
    path_or_uri=self.chroma_path,
)
```

**Go 问题：**
```go
// internal/agentcore/retrieval/indexing/indexer/chroma.go
// BuildIndex L167-174、DeleteIndex L307-314、IndexExists L376-383、GetIndexInfo L412-419
// 每个方法都重新创建 ChromaVectorStore 实例
vectorStore, err := vector_store.NewChromaVectorStore(vectorStoreConfig, ...)
```

每次 CRUD 操作都重新打开数据库连接、重建集合引用。Python 在构造时创建并缓存 `_client`，后续所有操作复用。

**修复方案：** 在 `ChromaIndexer` 结构体中缓存 `vectorStore` 实例：
```go
type ChromaIndexer struct {
    ...
    vectorStore vector_store.VectorStore // 缓存实例
    mu          sync.RWMutex
}

func (ci *ChromaIndexer) getVectorStore(config VectorStoreConfig) (vector_store.VectorStore, error) {
    ci.mu.RLock()
    if ci.vectorStore != nil { ci.mu.RUnlock(); return ci.vectorStore, nil }
    ci.mu.RUnlock()
    // 创建并缓存
}
```

---

### S-06 ChromaIndexer: DeleteIndex/IndexExists/GetIndexInfo 缺少 databaseName

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/chroma_indexer.py L66
self.database_name = config.database_name
# 所有方法通过 self.database_name 传入 database_name
```

**Go 问题：**
```go
// internal/agentcore/retrieval/indexing/indexer/chroma.go
// BuildIndex L157-163: 从 indexOpts.Extra["database_name"] 获取，OK
// DeleteIndex L302-305: VectorStoreConfig 缺失 DatabaseName
// IndexExists L371-374: VectorStoreConfig 缺失 DatabaseName
// GetIndexInfo L407-410: VectorStoreConfig 缺失 DatabaseName
```

非 BuildIndex 方法创建 `VectorStoreConfig` 时未设置 `DatabaseName`，如果数据库使用非默认 database，操作会命中错误的 database。

**修复方案：** 在 `ChromaIndexer` 结构体中存储 `databaseName`，在所有方法中统一设置：
```go
type ChromaIndexer struct {
    databaseName string // 新增字段
    ...
}
// 每个方法的 VectorStoreConfig 中设置 DatabaseName: ci.databaseName
```

---

### S-07 deep_adapter_team: processTeamMessageStream 缺少 team 模式前 6 步运行时设置

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/interface_deep.py L4549-4561
resolved_model = self._resolve_model_for_request(request)
self._apply_model_to_react_agent(resolved_model)
resolved_language = self._resolve_runtime_language()
resolved_channel = str(cid or self._resolve_prompt_channel(session_id) or "web").strip() or "web"
self._runtime_prompt_rail.set_model_name(...)
self._runtime_prompt_rail.set_mode(mode)
self._write_runtime_state(mode=..., language=..., channel=..., project_dir=...)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L1010-1011
// team 模式分支直接调用 d.processTeamMessageStream(ctx, req, inputs)
// 跳过了上述全部 6 步运行时状态设置
```

模型不切换、language/channel 不解析、prompt_rail 不配置，后续 agent 行为可能不正确。

**修复方案：** 在 `processTeamMessageStream` 入口处或调用前，补全这 6 步运行时设置。优先复用 Agent/Code 模式已有的 `resolveModelForRequest`/`applyModelToReactAgent`/`resolveRuntimeLanguage` 等方法。

---

### S-08 deep_adapter_team: 缺失 wait_for_pending_shutdown_cleanup_for_session

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

**Go 问题：** `processTeamMessageStream`（deep_adapter_team.go L192+）中没有等价调用。如果上一次 shutdown 清理未完成就发起新的 team 消息，可能导致 pool 条目残留、DB 状态不一致。

**修复方案：** 在 `processTeamMessageStream` 的 teamManager 获取后、isFirstRequest 判断前，添加对 `WaitForPendingShutdownCleanup` 的调用。需要 TeamManager 暴露此方法或实现等效的同步等待机制（15秒超时）。

---

### S-09 deep_adapter_team: handleTeamSkillEvolveApproval 审批后缺少 sync_team_skills

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/interface_deep.py L3709-3714
try:
    from jiuwenswarm.agents.harness.team import sync_team_skills_across_managers
    if session_id:
        sync_team_skills_across_managers(session_id)
except Exception as exc:
    logger.warning(...)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L89+
func (d *DeepAdapter) handleTeamSkillEvolveApproval(...) bool {
    // rail.ApproveRecord 成功后直接跳到 pushTeamSkillEvolveResolutionStatus
    // 完全没有 SyncTeamSkillsAcrossManagers 调用
}
```

审批通过后 skill 变更不同步到全局目录，其他 session 无法感知最新 skill。

**修复方案：** 在 `handleTeamSkillEvolveApproval` 的 `rail.ApproveRecord` 成功后，调用 `SyncTeamSkillsAcrossManagers(sessionID)` 或等价的同步机制。

---

### S-10 deep_adapter_team: pushTeamSkillEvolveResolutionStatus 空实现

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/interface_deep.py L3729-3767
# 通过 WebSocketGatewayPushTransport + push_evolution_status + build_evolution_status_update
# 完整推送到前端，参数包含 accepted: bool，决定 stage 为 "completed" 或 "hidden"
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L140-148
func (d *DeepAdapter) pushTeamSkillEvolveResolutionStatus(
    ctx context.Context, requestID string, status string,
) error {
    // Python: 通过 stream event 推送，当前 Go 端仅记录日志
    return nil
}
```

Go 版是空实现，仅记日志。前端永远收不到审批结果的推送通知。

**修复方案：** 实现 `EvolutionPushContext`，集成 `WebSocketGatewayPushTransport`，在 `pushTeamSkillEvolveResolutionStatus` 中调用 `transport.SendPush` 发送到前端。

---

### S-11 deep_adapter_team: /evolve 命令缺少信号检测（SignalDetector）和消息收集

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/interface_deep.py L3827-3863
parsed_messages = self._collect_messages_for_evolve(session_id)
detector = SignalDetector(existing_skills=existing_skills)
detected = detector.detect(parsed_messages)
new_signals, attributed = ...
if not detected and not user_query:
    return {"output": "未发现明确的演进信号", ...}
# 自动推导 evolution_intent
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_slash.go L99-142
// handleEvolveCommand 完全没有上述步骤
// 直接调用 RequestUserEvolution，没有 _collect_messages_for_evolve
// 没有 SignalDetector，没有信号去重
```

`/evolve` 命令无法自动从对话中检测演进信号，功能严重降级。

**修复方案：** 实现 `SignalDetector` 和 `_collect_messages_for_evolve`，在 `/evolve` 命令处理中先收集对话消息、检测信号，再决定是否发起 evolution 请求。

---

### S-12 deep_adapter_team: consumeStreamWithQuery 缺少异常处理和 team.error 广播

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L1028-1044
except Exception as exc:
    logger.error("[TeamHelpers] stream failed: %s", exc)
    _broadcast_event(channel_id, session_id, {
        "event_type": "team.error",
        "error": str(exc),
        "session_id": session_id,
    })
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L584-735
// consumeStreamWithQuery 中没有 defer recover 或 try-catch 等效机制
// Go 端有 2 处 team.error 广播（L635-639, L725-729），但缺少 Python 中的 except Exception 广播
```

流处理过程中如果内部代码 panic，goroutine 崩溃且不会广播 `team.error` 给前端。

**修复方案：** 在 `consumeStreamWithQuery` 的 `defer` 块中添加 `recover` 逻辑：
```go
defer func() {
    if r := recover(); r != nil {
        logger.Error(logComponent).Any("recover", r).Msg("consumeStreamWithQuery panic")
        broadcastTeamEvent(channelID, sessionID, map[string]any{
            "event_type": "team.error",
            "error":      fmt.Sprintf("%v", r),
        })
    }
}()
```

---

### S-13 deep_adapter_team: watchTeamEvolutionAndPushTeam 缺少外层异常处理

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L1376-1393
except Exception as exc:
    logger.warning("[TeamHelpers] evolution monitor failed: %s", exc)
    try:
        if active_cycle_request_id is None:
            return
        await push_evolution_status(push_context, build_evolution_status_update(
            request_id=active_cycle_request_id,
            status="end", stage=TEAM_EVOLUTION_HIDDEN_STAGE,
            message=f"团队技能演进分析失败: {exc}",
        ), build_server_push_message)
    except Exception as push_exc:
        logger.warning(...)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L791-1100
// watchTeamEvolutionAndPushTeam 没有外层 recover/异常处理
// 如果循环体 panic，goroutine 崩溃，不会向用户发送 "end" 状态通知
```

**修复方案：** 在 goroutine 入口添加 `defer recover`，在 recover 时推送 "end" 状态通知：
```go
defer func() {
    if r := recover(); r != nil {
        logger.Warn(logComponent).Any("recover", r).Msg("watchTeamEvolutionAndPushTeam panic")
        d.pushEvolutionStatus(ctx, activeCycleRequestID, "end")
    }
}()
```

---

### S-14 deep_adapter_team: RunAgentTeamStreaming 缺少 envs 参数传递

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L932-938
async for chunk in Runner.run_agent_team_streaming(
    agent_team=team_spec,
    inputs={"query": initial_query},
    session=session_id,
    envs=envs,          # <-- 传入了 envs（含 hide_dm 和 _STREAM_TRACE_ENV_KEY）
    stream_logger=lg,
):
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L631
streamCh, err := ateamruntime.RunAgentTeamStreaming(
    ctx, teamSpec, map[string]any{"query": initialQuery},
    false, false, sessionID, nil,    // 缺少 envs 传递
)
```

Go 端没有传入 `envs`（包含 `hide_dm` 和 `_STREAM_TRACE_ENV_KEY`）。下游 runner 无法感知 hide_dm 和 debug 环境变量，行为可能不一致。

**修复方案：** 在 `RunAgentTeamStreaming` 签名中增加 `envs map[string]string` 参数，将 `envs` 传递到底层 `activation.Session` 的 context 中。

---

### S-15 deep_adapter_team: utilsParseStreamChunk 空实现，前端事件格式可能缺失

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L944
parsed = parse_stream_chunk(chunk)
# parse_stream_chunk 将 SDK OutputSchema 转为前端可消费的 dict
# 包含 event_type/content 等字段，对 llm_output/tool_call/answer 等 chunk type 做规范化
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L1195-1202
func utilsParseStreamChunk(chunk map[string]any) map[string]any {
    if chunk == nil { return nil }
    // Python: parse_stream_chunk 将 SDK chunk 转为前端可消费的 dict
    // Go 差异：chunk 已经是 map[string]any，直接返回
    return chunk
}
```

直接返回输入 chunk，不做任何解析转换。Python 端对 `llm_output`/`tool_call`/`answer` 等 chunk type 做了事件类型规范化和字段提取。Go 端跳过了这些逻辑，`isLeaderOutput`/`isTeammateOutput` 等下游判断可能因缺少 `event_type` 字段而漏掉事件。

**修复方案：** 实现 Python `parse_stream_chunk` 的等价逻辑，从 chunk 中提取 `payload.type` 或 `payload.event_type`，对 `llm_output`/`tool_call`/`tool_result`/`answer` 等类型做规范化映射。

---

### S-16 deep_adapter_team: findTeamSkillRail 查找逻辑与 Python 完全不同

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/interface_deep.py L3656-3668
@staticmethod
def find_team_skill_rail(request_id: str, channel_id: str | None = None):
    rail = get_team_manager(channel_id).find_team_skill_rail_for_request(request_id)
    if rail is not None:
        return rail
    return find_team_skill_rail_across_managers(request_id)
```

Python 通过 `request_id` 在所有 TeamManager 中查找拥有该 request 的 rail，是按 request_id 反查的全局搜索。

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L46-61
func (d *DeepAdapter) findTeamSkillRail() *coreevolution.TeamSkillEvolutionRail {
    // 不接受 requestID 参数，仅在当前 DeepAdapter 实例的已注册 rails 中按类型查找
    // 缺少 FindTeamSkillRailForRequest 和 FindTeamSkillRailAcrossManagers 两级全局搜索
}
```

多 channel 场景下 team skill 审批将找不到正确的 rail。

**修复方案：** `findTeamSkillRail` 增加 `requestID` 参数，实现两级查找：先在当前 TeamManager 中按 request_id 查找，未找到则在所有 TeamManager 中搜索。

---

### S-17 ChromaIndexer: BuildIndex 缺少 BaseError 透传和通用异常包装

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/chroma_indexer.py L188-196
except BaseError:
    raise
except Exception as e:
    raise build_error(
        StatusRetrievalIndexingAddDocRuntimeError,
        f"Failed to build index for doc_id={doc_id}: {e}",
    ) from e
```

**Go 问题：** Go 版直接返回底层 error，不做 `StatusRetrievalIndexingAddDocRuntimeError` 包装。Python 先捕获 BaseError 原样抛出（保留原始错误码），再捕获通用异常包装为特定错误码。

**修复方案：** 在 `BuildIndex` 返回 error 时，判断是否已经是 `BaseError`（通过类型断言），是则原样返回；否则包装为 `StatusRetrievalIndexingAddDocRuntimeError`：
```go
var baseErr *exception.BaseError
if errors.As(err, &baseErr) {
    return false, err
}
return false, exception.NewBaseError(StatusRetrievalIndexingAddDocRuntimeError, fmt.Sprintf("Failed to build index: %v", err))
```

---

### S-18 ChromaIndexer: MilvusIndexer 完全缺失

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/milvus_indexer.py (472行)
# 完整的 MilvusIndexer 实现：
# - __init__: milvus_alias, token, VectorStoreConfig
# - build_index: _ensure_collection 自动建表, doc_id 查重, 嵌入, 数据写入
# - update_index: flush 逻辑
# - delete_index: filter 表达式删除
# - index_exists: has_collection
# - get_index_info: get_collection_stats + describe_collection
# - _ensure_collection: 自动建 Collection + Schema + BM25 Function + Index
# - close()
```

**Go 问题：** Go 端完全没有 `MilvusIndexer` 实现。

**修复方案：** 新建 `internal/agentcore/retrieval/indexing/indexer/milvus.go`，实现 `Indexer` 接口。可参考 `ChromaIndexer` 的结构和 Python 的 `MilvusIndexer` 实现。

---

### S-19 retrieval/indexing: processor/ 子树（parser/chunker/extractor/splitter）全部缺失

**Python 样例：**
```
openjiuwen/core/retrieval/indexing/processor/
├── parser/         # 12 个文件: AutoParser, PDFParser, TxtMdParser, HTMLFileParser, JSONParser, ExcelParser, WordParser, ImageParser, WebPageParser, WeChatArticleParser, ImageCaptioner
├── chunker/        # 7 个文件: CharChunker, TokenizerChunker, TextChunker, HybridChunker, TextPreprocessor, SentenceSplitter
├── extractor/      # 2 个文件: TripleExtractor (LLM 三元组提取)
└── splitter/       # 2 个文件: SentenceSplitter
```

**Go 问题：** Go 端 `processor/` 子树全部缺失，共 23 个 Python 文件约 2500+ 行代码未移植。`Indexer` 接口要求的 parser/chunker/extractor 预处理链无 Go 端支撑。

**修复方案：** 按 Python 的 processor 层次逐步移植。优先级：
1. `Chunker`（CharChunker + TokenizerChunker）— BuildIndex 核心依赖
2. `Parser`（AutoFileParser + PDFParser + TxtMdParser）— 文档解析必需
3. `Extractor`（TripleExtractor）— 知识图谱场景
4. 其他 parser 和 splitter 按需补充

---

### S-20 Extra() 并发安全不完整 — 14+ 处直接绕过 mutex 写入

**Python 样例：** Python 使用 asyncio 单线程事件循环，不存在并发 map 读写问题。

**Go 问题：**
```go
// internal/agentcore/single_agent/interfaces/callback.go L422
func (c *AgentCallbackContext) Extra() map[string]any { return c.extra }
```

`Extra()` 直接返回内部 map 引用，14+ 处代码通过 `cbc.Extra()[key] = value` 直接写入，完全绕过了 `extraMu sync.RWMutex` 保护：
- `interrupt/handler.go:254` — `cbc.Extra()[ResumeUserInputKey] = userInput`
- `interrupt/handler.go:297` — `cbc.Extra()[ResumeStartIterationKey] = resumeIteration + 1`
- `server/hooks/user_hook_rail.go:77-78` — 写入 `_skip_tool`/`_hook_feedback`
- `agent_teams/observability/rail.go:86,100,108` — span 写入/读取/删除
- `harness/common/rails/stream_event_rail.go:327` — 写入 sidKey
- `harness/common/rails/avatar_rail.go:256` — 写入 `_skip_tool`

stream 模式下多个 rail 并发回调时仍可能触发 `concurrent map read and map write` fatal error。

**修复方案：** 将所有 `cbc.Extra()[key] = value` 改为 `cbc.SetExtra(key, value)`，`cbc.Extra()[key]` 读取改为 `cbc.GetExtra(key)`，`delete(cbc.Extra(), key)` 改为 `cbc.DeleteExtra(key)`。

---

### S-21 closeTeamInteractGate 使用 context.Background() 无法取消

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L711-715
await entry.interact_gate.close_and_drain()
# asyncio 中天然可取消
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L572-578
entry.InteractGate.CloseAndDrain(context.Background())
// 使用 context.Background() 传入，如果程序需要优雅停机，此调用无法被取消
```

**修复方案：** 将 `context.Background()` 替换为传入的 `ctx` 或一个带超时的 context：
```go
closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
entry.InteractGate.CloseAndDrain(closeCtx)
```

---

### S-22 deep_adapter_team: gateway_push 推送完全空实现

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L1109-1113
push_context = EvolutionPushContext(
    transport=WebSocketGatewayPushTransport(),
    channel_id=channel_id,
    session_id=session_id,
)
await push_evolution_status(push_context, build_evolution_status_update(...), build_server_push_message)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L816-817
// TODO(#9.85): 待 gateway_push 集成完善后，通过 transport.SendPush 推送到前端
logger.Info(logComponent)...Msg("pushEvolutionStatus: [TODO] 推送 evolution status")
```

所有 `pushEvolutionStatus` 和 `pushEvolutionEvent` 调用仅记录日志，前端无法收到 evolution 审批/进度事件。Team skill evolution 功能对用户完全不可见。

**修复方案：** 实现 `EvolutionPushContext`，集成 `WebSocketGatewayPushTransport`，在 `pushEvolutionStatus`/`pushEvolutionEvent` 中调用 `transport.SendPush` 发送到前端。

---

## 一般问题 (M-01 ~ M-18)

### M-01 TeamRunner: 缺失 context: ModelContext 和 envs 参数

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L151-160
async def run_agent_team(self, agent_team, inputs, *, member, session, context, envs):
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L65-72
func (m *TeamRunnerManager) RunAgentTeam(ctx, agentTeam, inputs, base, member, sess)
// 缺少 context: ModelContext 和 envs: dict 参数
```

Python 将 `context`（上下文窗口）和 `envs`（环境变量）传给 runner，Go 完全丢弃。`RunAgentTeamStreaming` 同样缺少 `stream_modes` 和 `envs`。

**修复方案：** 在 `RunAgentTeam`/`RunAgentTeamStreaming` 签名中增加 `envs map[string]string` 参数，将 envs 注入到 session 的 context 中。

---

### M-02 TeamRunner: 六处缺失 _root_task_group_scope()

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L181
async with self._root_task_group_scope():
    # activate/invoke/finalize 在此 scope 内执行
```

**Go 问题：** 6 个路径（runAgentTeam / runAgentTeamStreaming / RunBaseTeam / RunBaseTeamStreaming / RunTeamMember / RunTeamMemberStreaming）均缺失等价机制。Go 没有异步 task group，但可以用 `errgroup` 或 `sync.WaitGroup` 实现类似生命周期管理。

**修复方案：** 当前影响较小（Go 的 goroutine 不需要 task group 也能正常执行），后续如需精细化资源管理，可引入 `errgroup.Group`。

---

### M-03 TeamRunner: FinalizeMember 的 ag.TeamMemberHandle()/MemberName() 调用无 recover 保护

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L304-309
except Exception as exc:
    team_logger.warning("finalize_member failed: %s", exc)
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L521-586
// FinalizeMember 内部用 defer recover + 匿名函数保护处理逻辑
// 但外层的 ag.TeamMemberHandle() 和 ag.MemberName() 调用不在匿名函数内
// 如果这些方法 panic，recover 不生效
```

**修复方案：** 将 `ag.TeamMemberHandle()` 和 `ag.MemberName()` 调用移入 recover 保护的匿名函数内，或在 `FinalizeMember` 入口处增加顶层 `defer recover`。

---

### M-04 ChromaIndexer: 不校验 doc_index_callback 类型

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/chroma_indexer.py L81-88
if isinstance(doc_index_callback, type) and issubclass(doc_index_callback, BaseCallback):
    ...
else:
    raise build_error(StatusRetrievalEmbeddingCallbackInvalid, ...)
```

**Go 问题：** `NewChromaIndexer` 不校验 `DocIndexCallback` 类型，传入非法类型不会在构造时报错，运行时才报错。

**修复方案：** 在 `NewChromaIndexer` 中增加类型断言校验，不符合时返回 error。

---

### M-05 ChromaIndexer: 缺少 Client() 方法

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/chroma_indexer.py L96-98
@property
def client(self):
    return self._client
```

**Go 问题：** Go 端 `ChromaIndexer` 没有 `Client()` 方法暴露底层 ChromaDB 客户端。

**修复方案：** 添加 `Client()` 方法返回缓存的 `ChromaVectorStore` 实例。

---

### M-06 ChromaIndexer: UpdateIndex/DeleteIndex 异常处理语义不一致

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/chroma_indexer.py L213-215 (update_index)
except Exception as e:
    logger.error(...); return False  # 吞异常返回 False

# L251-253 (delete_index)
except Exception as e:
    logger.error(...); return False  # 吞异常返回 False
```

**Go 问题：** Go 版 `UpdateIndex` 在 `DeleteIndex` 失败时 `return false, err`（error 传播到调用方），`DeleteIndex` 返回 `(false, err)`。Python 吞异常返回 False 不抛出，Go 传播 error，语义不一致。

**修复方案：** 按照项目惯例，Go 端传播 error 是更正确的做法（不吞异常），此处保留 Go 行为但需确认调用方是否期望 Python 的"吞异常"语义。如果调用方不处理 error，可能导致不必要的 panic。

---

### M-07 ChromaIndexer: 缺少 Close() 方法

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/chroma_indexer.py L302-311
def close(self):
    # ChromaDB 客户端不需要显式关闭，保留方法作为扩展点
```

**Go 问题：** Go 端没有 `Close()` 方法。

**修复方案：** 添加 `Close()` 方法（可先空实现），保持接口一致性。

---

### M-08 HandleTeamEvolveListCommand 缺少 Used/Effect 列

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L469-484
"| # | Score | Used | Effect | Section | Content (preview) |"
used_str = f"{stats.times_used}/{stats.times_presented}"
effect_str = f"+{stats.times_positive}/-{stats.times_negative}"
```

**Go 问题：**
```go
// internal/swarm/server/adapter/team_helpers.go L185-198
"| # | Score | Section | Content (preview) |"
// 缺少 Used 和 Effect 两列
```

用户看到的演进经验信息不完整，无法判断哪些经验被实际使用过。

**修复方案：** 在 `HandleTeamEvolveListCommand` 的表格格式中增加 Used/Effect 列。需要 `GetRecordsByScore` 返回值中包含 `UsageStats` 字段（可能需要在 evolution record 类型中补充）。

---

### M-09 /evolve 命令缺少 isinstance(evolve_result, str) 分支

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L597-601
if isinstance(evolve_result, str) and evolve_result.strip():
    return {
        "output": f"Skill '{skill_name}' 演进请求已提交，请等待审批。",
        "result_type": "answer",
    }
```

**Go 问题：** Go 端 `HandleTeamSlashCommand` 的 /evolve 分支直接调用 `approvalResultFromEventOrItems`，没有检查 `evolve_result` 是否为字符串类型。

**修复方案：** 在 `/evolve` 分支中增加对 `evolveResult` 状态的判断——如果 `evolveResult.ApprovalEvent == nil && len(evolveResult.Records) == 0`，返回"演进请求已提交，请等待审批"消息。

---

### M-10 /evolve list 输出格式不一致

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/interface_deep.py L3797-3808
summary = await store.list_pending_summary(skill_names)
return {"output": f"**Skills 演进记录：**\n\n{summary}", ...}
```

**Go 问题：** Go `handleEvolveListCommand` 返回 `{"action": "evolve_list", "skills": [...]}` 格式，与 Python 的文本摘要格式不同。

**修复方案：** 统一输出格式，对齐 Python 的 `list_pending_summary` 文本摘要格式。

---

### M-11 onTeamWatcherDone 缺少异常检查

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L1086-1098
def _on_team_watcher_done(task):
    ...
    if task.cancelled(): return
    exc = task.exception()
    if exc is not None:
        logger.warning("[TeamHelpers] evolution monitor task exception: %s", exc)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/team_helpers.go L679-681
func onTeamWatcherDone(sessionID string) {
    logger.Info(logComponent).Str("session_id", sessionID).Msg("onTeamWatcherDone: team evolution watcher done")
}
```

只记录 Info 日志，不检查 goroutine 是否异常退出，也不记录异常。

**修复方案：** `onTeamWatcherDone` 增加 `err error` 参数，当 err != nil 时记录 Warning 日志。

---

### M-12 TeamStreamLogger 未集成

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L927-931
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

Go 端 `RunAgentTeamStreaming` 调用时传入 `nil` 作为 streamLogger 参数。调试追踪功能缺失，线上问题排查无 trace 文件。

**修复方案：** 在 `processTeamMessageStream` 中集成 TeamStreamLogger：当 `streamTraceEnabled` 为 true 时，创建 `TeamStreamLogger` 实例并传入 `RunAgentTeamStreaming`。

---

### M-13 分布式 hooks 传 nil channel_id

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py L963-967
await tm.attach_distributed_hooks_for_runner_runtime(
    team_name=ready_team_name,
    session_id=session_id,
    channel_id=channel_id,
)
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L698-699
// 分布式 hooks — ⤵️(#9.72) 待回填
tm.AttachDistributedHooksForRunnerRuntime(readyTeamName, sessionID, nil)
```

Go 传了 `nil` 作为 channel_id 参数，Python 传了 `channel_id`。分布式 hooks 注册无效，可能导致多实例场景下事件分发失败。

**修复方案：** 将 `channelID` 参数传入 `AttachDistributedHooksForRunnerRuntime`。

---

### M-14 handleEvolveCommand 返回格式缺少 output/result_type 字段

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/interface_deep.py L3913-3920
return {
    "output": f"已为 Skill '{skill_name}' 生成 {len(questions)} 条演进经验，请审批：\n{summaries}",
    "result_type": "answer",
    "approval_chunks": approval_chunks,
}
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_slash.go L129-141
return map[string]any{
    "action": "approval_required",
    "approval_chunks": []any{result.ApprovalEvent},
    "request_id": ...,
    "skill_name": ...,
}
```

缺少 `output` 和 `result_type` 字段，前端展示逻辑可能无法正确处理。

**修复方案：** 在返回 map 中增加 `"output"` 和 `"result_type": "answer"` 字段，对齐 Python 格式。

---

### M-15 optionMatches 函数语义与 Python _option_matches 完全不同

**Python 样例：**
```python
# jiuwenswarm/server/runtime/agent_adapter/interface_deep.py L3671-3685
@staticmethod
def _option_matches(answers: list, accept_labels: tuple[str, ...]) -> bool:
    normalized_accept = {s.strip().lower() for s in accept_labels}
    for ans in answers:
        if not isinstance(ans, dict): continue
        for opt in ans.get("selected_options", []) or []:
            if isinstance(opt, str) and opt.strip().lower() in normalized_accept:
                return True
    return False
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L152-180
func (d *DeepAdapter) optionMatches(option map[string]any, answers any) bool {
    // 完全不同的实现：检查 option["id"] 是否在 answers 中匹配
    // 不是 Python _option_matches 的移植
}
```

Go 版本在 `handleTeamSkillEvolveApproval` 中**未调用**此函数（使用的是 `parseApprovalAnswers`），所以此函数是死代码。

**修复方案：** 删除 `optionMatches` 函数，或在 `handleTeamSkillEvolveApproval` 中按 Python 逻辑使用 `_option_matches`（检查 `selected_options` 是否匹配 `accept_labels`）。

---

### M-16 resolveEvolutionEventTimeoutSec 未从 rail 配置读取

**Python 样例：**
```python
# evolution_helpers.py
sdk_timeout = rail.get("evolution_total_timeout_secs")
```

**Go 问题：**
```go
// internal/swarm/server/adapter/deep_adapter_team.go L1209-1217
func resolveEvolutionEventTimeoutSec(rail *coreevolution.TeamSkillEvolutionRail, fallback float64) float64 {
    if rail == nil { return fallback }
    // Go 端 TeamSkillEvolutionRail 结构体可能没有此字段，使用默认超时
    return fallback
}
```

直接返回 fallback，无法自定义超时。

**修复方案：** 在 `TeamSkillEvolutionRail` 配置中增加 `EvolutionTotalTimeoutSecs` 字段，`resolveEvolutionEventTimeoutSec` 从中读取。

---

### M-17 ChromaIndexer: UpdateIndex/DeleteIndex 异常时 Go 传播 error 而 Python 吞异常

**Python 样例：**
```python
# openjiuwen/core/retrieval/indexing/chroma_indexer.py L213-215
except Exception as e:
    logger.error(...); return False  # Python 吞异常返回 False

# L251-253 (delete_index)
except Exception as e:
    logger.error(...); return False  # Python 吞异常返回 False
```

**Go 问题：** Go 版 `UpdateIndex` 在 `DeleteIndex` 失败时 `return false, err`（error 传播到调用方），Python 吞异常。语义不一致。

**修复方案：** 保留 Go 传播 error 的做法（更符合 Go 惯例），但需确认调用方是否正确处理 error。

---

### M-18 TeamRunner: RegisterHumanAgentInbound 返回值差异

**Python 样例：**
```python
# openjiuwen/core/runner/team_runner.py L487-511
# 返回 bool，可能抛 KeyError
```

**Go 问题：**
```go
// internal/agent_teams/runtime/team_runner.go L621-644
// 返回 (bool, error)，callback 类型校验失败返回 (false, nil) 而非 error
```

**修复方案：** callback 类型校验失败时应返回 `(false, error)` 以便调用方感知错误。

---

## 提示问题 (T-01 ~ T-09)

### T-01 ChromaVectorStore 构造参数差异：Python 传 vector_field 字符串，Go 传对象

**Python 样例：** `vector_field=self.vector_field.vector_field`（字符串）
**Go 问题：** 传整个 `*vector_fields.ChromaVectorField` 对象
**说明：** 这是 API 设计差异，不是逻辑缺失。

---

### T-02 DeleteIndex 重建 VectorStore 而非复用 _client

**Python 样例：** `self._client.get_collection(name=...)`（复用缓存的 client）
**Go 问题：** 每次重新创建 `ChromaVectorStore`
**说明：** 与 S-05 同根因，S-05 修复后此问题自动解决。

---

### T-03 IndexExists 返回值差异：Python 返回 bool，Go 返回 (bool, error)

**Python 样例：** `try: get_collection; return True; except: return False`
**Go 问题：** `return (bool, error)` — Go 增加了 error 返回
**说明：** 合理的 Go 适配，无需修改。

---

### T-04 TeamRunner: InteractAgentTeam 的 contextvars 差异

**Python 样例：** 使用 `contextvars.set_session_id/reset_session_id`
**Go 问题：** 用 `sessionctx.WithSessionState` 绑定到 ctx
**说明：** 合理适配，无需修改。

---

### T-05 TeamRunner: DeleteTeam 中 Go 增加了 db.Initialize() 调用

**Python 样例：** `await db.initialize()` 没有错误恢复
**Go 问题：** Go 增加 `db.Initialize()` 失败仅 warning 然后跳过
**说明：** Go 更健壮，无需修改。

---

### T-06 /evolve_rebuild / /evolve_rollback 在 team 模式下行为与 Python 对齐

**说明：** Python 将这两个命令放在 `_handle_slash_command`（agent 模式）中，team 模式下不走。Go 端行为一致。

---

### T-07 sync_team_identity_metadata 调用顺序差异

**说明：** Go 版先检查 `teamCreateKinds` 再获取 `metadata`，Python 先获取 metadata。功能等价（提前 return 不会多读），无需修改。

---

### T-08 ResolveTeamRebuildFollowup 返回空串 vs Python 返回 None

**Python 样例：** 返回 `(None, None)`
**Go 问题：** 返回 `("", "")`
**说明：** 语义基本对齐，空串可表达"无结果"。

---

### T-09 decide_run_action 中 Session 不匹配时 Go 用 panic 而 Python 用 raise RuntimeError

**Python 样例：** `raise RuntimeError(...)`
**Go 问题：** `panic(...)`
**说明：** 合理适配，契约违反时 panic 是 Go 惯用法。

---

## ⤵️ 占位代码实际状态

| 占位标记 | 预期回填章节 | 当前状态 | 说明 |
|----------|-------------|---------|------|
| `⤵️(#9.85)` Finalize/closeGate/PostRun defer 保护 | 9.85 | **未实现** | 见 S-02 |
| `⤵️(#9.85)` envs 参数传递 | 9.85 | **未实现** | 见 S-14, M-01 |
| `⤵️(#9.72)` 分布式 hooks channel_id | 9.72 | **未实现** | 见 M-13，传 nil |
| `⤵️(#9.85)` gateway_push 推送 | 9.85 | **未实现** | 见 S-22，仅记日志 |
| `⤵️(#9.72)` wait_for_pending_shutdown_cleanup | 9.72 | **未实现** | 见 S-08 |
| `⤵️(#9.26)` BrowserAgent 6 处 Playwright 占位 | 9.38-49 | **未实现** | CodeExecutor/ManagedBrowserDriver/WorkerAgent/runTaskOnce/Probe/restart |
| `⤵️(#5.2)` ActorManager 返回类型 | 后续 | **未实现** | 5.2/5.3/5.4 都标记 |
| `⤵️(#2.16)` UserConfig.is_sensitive() 敏感信息过滤 | common/security | **未实现** | 等待 UserConfig 模块迁移 |
| `TODO` TeamStreamLogger 对齐 | 9.85 | **未实现** | 见 M-12 |
| `TODO` utilsParseStreamChunk 实际解析 | 10.3 | **未实现** | 见 S-15 |
| `TODO` MilvusIndexer | 13.2 | **未实现** | 见 S-18 |
| `TODO` processor/ 子树 (parser/chunker/extractor/splitter) | 13.2 | **未实现** | 见 S-19 |

---

## 修复优先级建议

| 优先级 | 问题编号 | 预计工作量 |
|--------|---------|-----------|
| P0 (立即修复) | S-01, S-02, S-03, S-15, S-20 | 2-3天 |
| P1 (本迭代修复) | S-04, S-05, S-06, S-07, S-08, S-09, S-10, S-11, S-12, S-13, S-14, S-16, S-17, S-21, S-22 | 5-7天 |
| P2 (下一迭代) | S-18, S-19, M-01~M-18 | 10-15天 |
| P3 (低优先级) | T-01~T-09 | 随需 |
