# 48h 逻辑审查报告

> 审查时间：2026-10-14  
> 审查范围：最近14天内完成的实现计划章节（48h 内无提交，扩大到14天窗口覆盖最近一轮功能实现）  
> 涉及章节：9.85（团队Runner/调度决策）、10.3.8（TeamHelpers）、13.2（ChromaIndexer）、13.4（ComputeChunkEmbeddings）、10.6.12（SessionOps）、10.6.13-18（Swarm Memory）、9.19-9.68（agent_teams 占位桩全量核查）  
> 涵盖范围补充：SessionOps 7 方法与 Swarm Memory 8 模块经第二轮逐方法对比复核（含 Python 双参考源同源性验证），修正初版 S-07 误判；TeamHelpers/团队流路径经第三轮 chunk 类型链路验证，新增 S-20/S-21/S-22/S-23 与 M-17~M-19、T-17~T-18；9.85 runner 内层（team_runner/manager/child/kernel）与 adapter 残留、13.2/13.4 二轮复核合并，新增 S-24~S-29、M-20~M-32、T-19~T-28

---

## 统计摘要

| 严重程度 | 数量 |
|---------|------|
| 严重 (S) | 29 |
| 一般 (M) | 31 |
| 提示 (T) | 28 |
| **合计** | **88** |

> 编号说明：M-11 经验证升级为 S-18（类型断言必然失败，实测确认），故 M 序列跳号。S-20~S-23 为 TeamHelpers/团队流第三轮验证新增；S-24~S-27 + M-17~M-19 + T-17/T-18 为 9.85 runner 内层（team_runner/manager/child/kernel）复核新增；M-20~M-32 + T-19~T-28 为 9.85 adapter 残留、10.6.12/13-18 二轮、13.2/13.4 二轮合并新增；S-28/S-29 为 13.4/13.2 二轮复核新增。

**已验证误报（不计入）**：
- ~~Interact 输入类型不一致~~ — Go 端 `TeamManager.Interact` 正确提取 `InteractInput.Raw` 传递，与 Python `interact(session_id, user_input: Any)` 行为一致
- ~~BuildChromaWhereFilter 可能不生效~~ — 实现正确，逐类型生成 `EqString/EqInt/...` WhereClause
- ~~DeleteIndex copy 类型不匹配~~ — `DocumentID` 底层类型为 `string`，`copy([]DocumentID, []string)` 因类型可赋值而合法，编译通过
- ~~team_backend.go IsTeamCompleted 是桩~~ — 实际已完整实现（L338）
- ~~sys_operation Base 基类"未实现"错误~~ — 设计模式（对齐 Python Base 语义），local/ 子包有完整实现并注册 GlobalRegistry
- ~~agent_teams 50+ 处 return nil, nil~~ — 逐一核对均为"查无记录"语义（对齐 Python Optional），无假空实现
- ~~Dreaming config 每次 LLM 提取重读 config.yaml~~ — 实测 Python `get_config()` 同样无缓存每次读盘（common/config.py L106-112），Go 行为一致，非移植缺陷
- ~~ForkSession 写回 history 的 JSON HTML 转义破坏 file-content 标签匹配~~ — `\u003c` 转义仅存在于序列化字节流，`json.Unmarshal` 读回后还原为 `<`，剥离正则不受影响（已验证 Go 端无文本模式消费 history.json 的代码路径）
- ~~getUniqueForkName 多数标题无法编号~~ — `fmt.Sscanf` 返回的是成功解析条目数（恒为 1），`num == 1` 恒真，逻辑碰巧正确工作；语义晦涩问题保留为 T-06

---

## 严重问题 (S)

### S-01: `/evolve_list` 表格缺少 `Used` 和 `Effect` 两列

**Python 参考** (`team_helpers.py` L466-485):
```python
"| # | Score | Used | Effect | Section | Content (preview) |",
"|---|---:|---|---|---|---|",
for i, record in enumerate(records, 1):
    stats = record.usage_stats
    if stats:
        used_str = (
            f"{stats.times_used}/{stats.times_presented}"
            if stats.times_presented
            else "0/0"
        )
        effect_str = f"+{stats.times_positive}/-{stats.times_negative}"
    else:
        used_str = "0/0"
        effect_str = "+0/-0"
    section = str(record.change.section).replace("|", "\\|")
    preview = record.change.content.split("\n")[0][:40].replace("|", "\\|")
    lines.append(
        f"| {i} | {record.score:.2f} | {used_str} | {effect_str} | {section} | {preview} |"
    )
```

**Go 问题** (`internal/swarm/server/adapter/team_helpers.go` L185-199):
```go
lines := []string{
    fmt.Sprintf("📊 Skill \"%s\" — 经验库摘要\n", skillName),
    fmt.Sprintf("共 %d 条经验 | 平均分：%.2f\n", len(records), avgScore),
    "| # | Score | Section | Content (preview) |",   // ← 缺少 Used / Effect 列
    "|---|---:|---|---|",
}
for i, record := range records {
    // ... 未读取 record.UsageStats
    lines = append(lines, fmt.Sprintf("| %d | %.2f | %s | %s |", i+1, record.Score, section, preview))
}
```

**影响**: 用户无法看到每条演进经验的使用次数和正负反馈统计，经验库摘要信息不完整。

**修复方案**: Go 端 `UsageStats` 结构体已存在（`internal/evolving/checkpointing/types.go` L20，含 `TimesPresented/TimesUsed/TimesPositive/TimesNegative`），直接使用：
```go
usedStr := "0/0"
effectStr := "+0/-0"
if record.UsageStats != nil {
    if record.UsageStats.TimesPresented > 0 {
        usedStr = fmt.Sprintf("%d/%d", record.UsageStats.TimesUsed, record.UsageStats.TimesPresented)
    }
    effectStr = fmt.Sprintf("+%d/-%d", record.UsageStats.TimesPositive, record.UsageStats.TimesNegative)
}
lines = append(lines, fmt.Sprintf("| %d | %.2f | %s | %s | %s | %s |", i+1, record.Score, usedStr, effectStr, section, preview))
```
注意 Python 的 used_str 在 `times_presented == 0` 时显示 "0/0"，对齐此行为。

---

### S-02: `resolveEvolutionEventTimeoutSec` 未从 rail 配置读取超时值，始终返回 fallback

**Python 参考** (`evolution_helpers.py`):
```python
def resolve_evolution_event_timeout_sec(rail, fallback_sec=TEAM_EVOLUTION_EVENT_TIMEOUT_SEC):
    sdk_timeout = rail.get("evolution_total_timeout_secs")
    if sdk_timeout is not None:
        return float(sdk_timeout)
    return fallback_sec
```

**Go 问题** (`internal/swarm/server/adapter/deep_adapter_team.go` L1206-1214):
```go
func resolveEvolutionEventTimeoutSec(rail *coreevolution.TeamSkillEvolutionRail, fallback float64) float64 {
    if rail == nil {
        return fallback
    }
    // 尝试通过 rail 的配置获取超时值
    // 对齐 Python: sdk_timeout = rail.get("evolution_total_timeout_secs")
    // Go 端 TeamSkillEvolutionRail 结构体可能没有此字段，使用默认超时
    return fallback   // ← 永远返回 fallback，配置被忽略
}
```

**影响**: Python 端 SDK 层（agent_evolving）可下发 `evolution_total_timeout_secs` 自定义超时，Go 端忽略该配置，演进分析可能在 SDK 期望的更短/更长时间后被判定超时。

**修复方案**: 在 `TeamSkillEvolutionRail` 上暴露超时配置（或通过其 config 字段读取）：
```go
func resolveEvolutionEventTimeoutSec(rail *coreevolution.TeamSkillEvolutionRail, fallback float64) float64 {
    if rail == nil {
        return fallback
    }
    if v := rail.EvolutionTotalTimeoutSecs(); v > 0 {
        return v
    }
    return fallback
}
```

---

### S-03: `watchTeamEvolutionAndPushTeam` 的 gateway_push 推送完全未实现，仅记录日志

**Python 参考** (`team_helpers.py` L1107-1113, L1141-1155):
```python
push_context = EvolutionPushContext(
    transport=WebSocketGatewayPushTransport(),
    channel_id=channel_id,
    session_id=session_id,
)
# ... 整个 watcher 循环中的所有状态推送：
await push_evolution_status(push_context, build_evolution_status_update(...), build_server_push_message)
await push_evolution_event(push_context, request_id, evt, build_server_push_message)
```

**Go 问题** (`deep_adapter_team.go` L805-851):
```go
pushEvolutionStatus := func(requestID, status, stage string, message string) {
    logger.Info(logComponent).
        Str("session_id", sessionID).
        // ...
        Msg("pushEvolutionStatus: evolution status pushed")
    // TODO(#9.85): 待 gateway_push 集成完善后，通过 transport.SendPush 推送到前端
}

pushEvolutionEvent := func(requestID string, evt map[string]any) error {
    // ... 构建了 payload 但丢弃
    // TODO(#9.85): 待 gateway_push 集成完善后，通过 transport.SendPush 推送到前端
    _ = payload     // ← payload 被显式丢弃
    return nil
}
```

**影响**: 前端完全无法收到演进进度（start/progress/end 状态）和审批请求事件，团队技能演进功能对用户不可见——审批弹框不会出现。这是演进闭环的关键断点。

**修复方案**: Go 端已有 `internal/swarm/server/gateway_push/transport.go`，集成方式：
```go
// 初始化 push 上下文（替代日志）
transport := gateway_push.NewWebSocketGatewayPushTransport() // 或已有的单例获取方式
pushEvolutionStatus := func(requestID, status, stage, message string) {
    update := evolutionlogic.BuildEvolutionStatusUpdate(requestID, status, stage, message)
    msg := sessionmd.BuildServerPushMessage(...)
    _ = transport.SendPush(ctx, channelID, msg)
}
```
若因循环依赖无法直接导入 gateway_push，可通过函数注入模式将 transport 作为 `DeepAdapter` 字段传入。

---

### S-04: `processTeamMessageStream` 缺少 `wait_for_pending_shutdown_cleanup` 等待

**Python 参考** (`team_helpers.py` L624-635):
```python
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

**Go 问题** (`deep_adapter_team.go` L192 起): `processTeamMessageStream` 直接进入元信息提取，无任何 shutdown 清理等待；且全仓搜索确认 Go 端**没有** `WaitForPendingShutdownCleanup` 的任何实现或桩（`internal/swarm/agents/harness/team/` 下无匹配）。

**影响**: 前一个 team session 正在 shutdown（远程成员 bootstrap 清理中）时，新请求可能读取到残留的远程成员状态，产生竞态。

**修复方案**: 
1. 在 `internal/swarm/agents/harness/team/` 实现 `remote_member_bootstrap` 等价物（若远程成员功能尚未移植，至少实现会话级清理等待桩）
2. 在 `processTeamMessageStream` 开头调用：
```go
if err := team.WaitForPendingShutdownCleanup(sessionID); err != nil {
    logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
        Msg("processTeamMessageStream: waiting for pending shutdown cleanup failed")
}
```

---

### S-05: `consumeStreamWithQuery` 缺少 ctx 取消时的日志与状态广播

**Python 参考** (`team_helpers.py` L1021-1027):
```python
except asyncio.CancelledError:
    logger.info(
        "[TeamHelpers] stream cancelled: channel_id=%s session_id=%s",
        _resolve_channel_id(channel_id), session_id,
    )
    raise
```

**Go 问题** (`deep_adapter_team.go` L584-732): 函数体遍历 `streamCh` 时未监听 `ctx.Done()`：
```go
for chunk := range streamCh {
    receivedChunks++
    // ... 无 ctx 取消检查
}
```
取消发生后，goroutine 仍会持续消费 `streamCh` 直到上游生产者退出，且无 "stream cancelled" 日志。

**修复方案**:
```go
for {
    select {
    case <-ctx.Done():
        logger.Info(logComponent).
            Str("channel_id", resolveChannelID(channelID)).Str("session_id", sessionID).
            Msg("consumeStreamWithQuery: stream cancelled")
        return
    case chunk, ok := <-streamCh:
        if !ok {
            goto streamLoopDone
        }
        receivedChunks++
        // ...
    }
}
```

---

### S-06: `consumeStreamWithQuery` 缺少整体异常捕获和 team.error 广播

**Python 参考** (`team_helpers.py` L1028-1044):
```python
except Exception as exc:
    logger.error(
        "[TeamHelpers] stream failed: channel_id=%s session_id=%s error=%s",
        _resolve_channel_id(channel_id), session_id, exc, exc_info=True,
    )
    _broadcast_event(channel_id, session_id, {
        "event_type": "team.error",
        "error": str(exc),
        "session_id": session_id,
    })
```

**Go 问题** (`deep_adapter_team.go`): 
- `RunAgentTeamStreaming` 返回 err 时有广播（L632-641）✅
- 但流消费循环内部的 panic（如 `chunkToMap`/`utilsParseStreamChunk`/`broadcastEvent` 中意外 panic）不会被捕获，导致：① defer 清理仍会执行，但 waiter 队列永远收不到 team.error，前端流挂起；② panic 传播可能崩溃进程。

**修复方案**: 添加 recover：
```go
defer func() {
    if r := recover(); r != nil {
        logger.Error(logComponent).Any("panic", r).Str("session_id", sessionID).
            Msg("consumeStreamWithQuery: panic recovered, broadcasting team.error")
        broadcastEvent(channelID, sessionID, map[string]any{
            "event_type": "team.error",
            "error":      fmt.Sprintf("stream panic: %v", r),
            "session_id": sessionID,
        })
    }
}()
```

---

### S-07: Swarm Memory 模块状态修正 — manager/embeddings/internal/types 已由 lite 包覆盖，真正缺失仅 plugin provider 与 RPC 面

> **修正说明**：初版报告误判为"6 个核心模块完全缺失"。经核查 `agentcore/memory/lite/`（28 个文件）与 `deep_adapter_rails.go` 内联实现，结论修正如下。已验证两个 Python manager 同源（37/40 方法共有，`jiuwenswarm/agents/harness/common/memory/manager.py` 是 `openjiuwen/core/memory/lite/manager.py` 的改版分支，仅 `_initialize/initialize` 命名差异），Go 已实现 lite 包对应全部方法并作为 MemoryRail 搜索后端接线（`memory_rail.go` L42 持有 `lite.MemoryToolContext`）。

**Python 参考** (`jiuwenswarm/agents/harness/common/memory/`):
```
manager.py          (1224行) MemoryIndexManager — 与 openjiuwen lite manager 同源（已由 agentcore/memory/lite 覆盖）
embeddings.py       (165行)  → 已由 agentcore/memory/lite/embeddings.go 覆盖
internal.py         (215行)  → 已由 agentcore/memory/lite/internal.go 覆盖
types.py            (79行)   → 已由 agentcore/memory/lite/types.go 覆盖
external_memory_builder.py (142行) build_external_memory_rail — 4 种 provider 分发已内联至 deep_adapter_rails.go L568-646
external_memory_config.py  (126行) 引擎门禁/解析函数已内联至 deep_adapter_rails.go L1483-1524
```

**Go 现状**：
```
internal/swarm/agents/harness/common/memory/   # dreaming ✅ + forbidden ✅
internal/agentcore/memory/lite/                # 28 文件：manager/embeddings/internal/types/config 全覆盖（对应 openjiuwen/core/memory/lite/）
internal/swarm/server/adapter/deep_adapter_rails.go  # external 4 provider 内联（mem0/openviking/openjiuwen/agentarts）
```

**真正缺失（本问题实际范围）**：
1. **plugin provider**（Python `external_memory_builder.py` L54/L133 `_load_plugin_provider`）— `~/.jiuwenswarm/plugins/memory/` 插件发现机制 Go 无对应。实现计划 10.6.13-18 标记 External 🔄 状态准确。
2. **RPC 面**（10.6.18）— Python 侧对应 manager 搜索的远程调用面，Go 无对应实现也无占位标注。

**影响**：plugin 形态的外接记忆（用户自装插件）不可用；RPC 面不影响单机功能。其余 Python 能力（内置记忆搜索、4 种内置 external provider、引擎门禁）均已可用。

**修复方案**：
1. plugin provider：在 `deep_adapter_rails.go` 的 `buildExternalMemoryProvider` 增加插件目录扫描分支（对齐 Python `_load_plugin_provider` 的 `~/.jiuwenswarm/plugins/memory/` 发现 + entry_point 加载）
2. RPC 面：随 10.6.18 单独排期（建议在 IMPLEMENTATION_PLAN.md 补充占位说明）
3. **计划状态同步**：IMPLEMENTATION_PLAN.md 10.6.13-18 行的 `Embeddings` 应标记为 ✅（已由 lite 包实现）——当前"未完成"标记与实际状态矛盾

---

### S-08: `pushTeamSkillEvolveResolutionStatus` 审批结果推送未实现

**Python 参考**: 审批通过/拒绝后通过 stream 事件推送解决状态给前端。

**Go 问题** (`deep_adapter_team.go` L140-148):
```go
func (d *DeepAdapter) pushTeamSkillEvolveResolutionStatus(ctx context.Context, requestID string, status string) error {
    logger.Info(logComponent).
        Str("request_id", requestID).
        Str("status", status).
        Msg("pushTeamSkillEvolveResolutionStatus: approval result pushed")
    // Python: 通过 stream event 推送，当前 Go 端仅记录日志
    return nil
}
```

**影响**: 用户在审批弹框提交决定后，前端收不到 approved/rejected 回执，弹框状态无法关闭（与 S-03 同根因：gateway_push 未集成）。

**修复方案**: 与 S-03 一并修复，通过 `broadcastEvent` 或 transport.SendPush 推送 `{"event_type": "team.skill_evolve_resolution", "request_id": ..., "status": ...}`。

---

### S-09: ChromaIndexer 每次操作新建 ChromaVectorStore，`BuildIndex` 完成后未关闭连接

**Python 参考** (`chroma_indexer.py` L90-93):
```python
def __init__(self, ...):
    self._client = ChromaVectorStore.create_client(   # ← 仅创建一次
        database_name=self.database_name,
        path_or_uri=chroma_path,
    )
    # 后续所有方法复用 self._client
```

**Go 问题** (`internal/agentcore/retrieval/indexing/indexer/chroma.go`):
```go
// BuildIndex (L167)、UpdateIndex、DeleteIndex (L307)、IndexExists (L376)、GetIndexInfo (L412)
// 每个方法都调用：
vectorStore, err := vector_store.NewChromaVectorStore(...)
if err != nil {
    return false, err
}
// BuildIndex 结束时没有 defer vectorStore.Close()！
```

**两个问题**：
1. `BuildIndex`（L167-174）创建 store 后**没有 `defer vectorStore.Close()`**（DeleteIndex/IndexExists/GetIndexInfo 都有），资源泄漏
2. 五个方法各自新建 store/client，与 Python 的单 client 复用模式不一致，重复初始化开销大

**修复方案**:
```go
// 短期：BuildIndex 添加 defer
vectorStore, err := vector_store.NewChromaVectorStore(...)
if err != nil {
    return false, err
}
defer vectorStore.Close()   // ← 补上

// 长期：对齐 Python，在 NewChromaIndexer 中持有共享 store
type ChromaIndexer struct {
    // ...
    store *vector_store.ChromaVectorStore  // 复用
}
```

---

### S-10: `TeamHarness.RunStreaming` 返回已关闭的空 channel — 团队流式输出静默无输出

**已验证调用链**：`StreamController.streamOneRound`（stream_controller.go L681）→ `harness.RunStreaming` → 空 channel → **整轮流式输出为空**。

**Python 参考** (`openjiuwen/agent_teams/harness.py` `TeamHarness.run_streaming`):
```python
async def run_streaming(self, inputs, session_id, team_session=None):
    # 分支 1: team_session 为 None 且非 initial_plan_mode
    if team_session is None and not self._initial_plan_mode:
        agent = self._runner.run_agent_streaming(...)
        async for chunk in agent:
            yield chunk
        return
    # 分支 2: _prepare_agent_session + _ensure_initial_plan_mode + 流式执行
    session = await self._prepare_agent_session(session_id, team_session)
    ...
```

**Go 问题** (`internal/agent_teams/harness.go` L297-304):
```go
func (h *TeamHarness) RunStreaming(ctx context.Context, inputs map[string]any, sessionID string, teamSession any) (<-chan stream.Schema, error) {
    // ⤵️ 待 9.57 deepAgent 类型升级为 BaseAgent 后实现分支 1
    // 分支 2: ⤵️ 待 9.57+ session 层回填
    ch := make(chan stream.Schema)
    close(ch)          // ← 立即关闭 = 消费者零输出
    return ch, nil
}
```

**矛盾点**：9.85 已实现 `TeamRunner` 三路径（`team_runner.go` L96/L180/L273/L467 都在调 `Stream`），表面上链路已通，但**底层 harness 的 RunStreaming 是空壳**——如果 `TeamAgent.Stream` 的 `coordination` 流程走 `StreamController.streamOneRound` 路径，则用户看不到任何成员输出。

**修复方案**：实现两个分支：
```go
func (h *TeamHarness) RunStreaming(ctx context.Context, inputs map[string]any, sessionID string, teamSession any) (<-chan stream.Schema, error) {
    // 分支 1: 无 teamSession 且非 initialPlanMode → 直接委托 runner
    if teamSession == nil && !h.initialPlanMode {
        return h.runner.RunAgentStreaming(ctx, h.deepAgent, inputs, sessionID)
    }
    // 分支 2: _prepare_agent_session + _ensure_initial_plan_mode + 流式执行
    // ...
}
```

---

### S-11: `AgentConfigurator` 永远不构造 `TeamToolApprovalRail` / `TeamPlanModeRail` — 审批流与团队规划模式断链

**已验证**：`rails/tool_approval_rail.go` 和 `rails/team_plan_mode_rail.go` 都已**完整实现且有测试**，但全仓搜索确认除 configurator 外无任何构造点。

**Python 参考** (`openjiuwen/agent_teams/agent/agent_configurator.py` 步骤 13-14):
```python
# 步骤 13: TeamToolApprovalRail（仅 TEAMMATE + 有审批工具时）
approval_tool_names = [...从 spec 解析...]
if ctx.role == TeamRole.TEAMMATE and approval_tool_names:
    tool_approval_rail = TeamToolApprovalRail(approval_tool_names, ...)

# 步骤 14: TeamPlanModeRail（仅 LEADER + team.plan 启用时）
if ctx.role == TeamRole.LEADER and spec.team_plan_enabled:
    team_plan_mode_rail = TeamPlanModeRail(initial_plan_mode=True)
```

**Go 问题** (`internal/agent_teams/agent/agent_configurator.go` L390-414):
```go
// 步骤 13: TeamToolApprovalRail（仅 TEAMMATE + 有审批工具时）
var toolApprovalRail *rails.TeamToolApprovalRail
// TODO(#9.runtime): 集成审批工具条件判断        // ← 永远 nil

// 步骤 14: TeamPlanModeRail（仅 LEADER + team.plan 启用时）
var teamPlanModeRail *rails.TeamPlanModeRail
// TODO(#9.runtime): 集成 team.plan 启用条件    // ← 永远 nil

harness := agentteams.BuildTeamHarness(
    ...,
    toolApprovalRail,   // nil
    teamPlanModeRail,   // nil
    false,              // TODO(#9.runtime): 是否启用团队规划模式  ← 永远 false
)
```

**影响**: 团队成员的工具调用审批（权限确认弹框）和团队规划模式在生产链路中完全失效，尽管两个 Rail 的实现代码和测试都已就绪。

**修复方案**: 从 agentSpec 解析审批工具列表与 plan 启用标志：
```go
// 步骤 13
approvalTools := resolveApprovalToolNames(agentSpec, ctx.Role) // 从 spec.tools 中过滤需审批工具
if ctx.Role == atschema.TeamRoleTeammate && len(approvalTools) > 0 {
    toolApprovalRail = rails.NewTeamToolApprovalRail(approvalTools, ...)
}
// 步骤 14
teamPlanEnabled := agentSpec.TeamPlanEnabled() // 需在 spec 中暴露该配置
if ctx.Role == atschema.TeamRoleLeader && teamPlanEnabled {
    teamPlanModeRail = rails.NewTeamPlanModeRail(true)
    initialPlanMode = true
}
```

---

### S-12: `TeamMemoryManager` 5 个生命周期方法全桩 — 团队记忆功能整体失效

**Go 问题** (`internal/agent_teams/memory/manager.go` L132-152):
```go
func (m *TeamMemoryManager) InitToolkit(_ context.Context) (bool, error) {
    return false, nil                      // ← 永远 false
}
func (m *TeamMemoryManager) RegisterTools(_ interfaces.DeepAgentInterface) {}
func (m *TeamMemoryManager) LoadAndInject(...) error { return nil }         // 记忆不注入提示词
func (m *TeamMemoryManager) ExtractAfterRound(_ context.Context) error { return nil } // 不提取
func (m *TeamMemoryManager) Close(_ context.Context) error { return nil }
```
连带 `extractor.go` L74-85：`BuildExtractionContext` 返回 `""`、`CreateExtractionTools` 返回 `nil`、`ExtractTeamMemories` 返回 `nil`。

**关键证据**：`agent_configurator.go` L419 已在调用 `BuildMemoryManager` 并调用这些方法——**生产链路已接入，但记忆功能实际完全空转**。

**Python 参考**: `openjiuwen/agent_teams/memory/` 的 `TeamMemoryManager`（init_toolkit 注册 memory_save/memory_search 工具 → load_and_inject 个人+共享记忆注入系统提示词 → leader 的 extract_after_round 蒸馏团队记忆）。

**修复方案**: 对齐 Python 实现：
1. `InitToolkit` → 初始化记忆工具集（memory_save/memory_search 等本地函数工具）
2. `RegisterTools` → 通过 DeepAgent 的 AbilityManager 注册工具
3. `LoadAndInject` → 读个人/共享 MEMORY 文件，注入系统提示词段
4. `ExtractAfterRound` → Leader 轮次结束后调 LLM 提取蒸馏记忆并写入共享目录

---

### S-13: `TeamMember.Status/ExecutionStatus` 硬编码返回 Ready/Idle — SHUTDOWN 流控永不触发

**Go 问题** (`internal/agent_teams/agent/member.go` L49-82):
```go
func (m *TeamMember) Status(ctx context.Context) (atschema.MemberStatus, error) {
    // TODO(#9.65): 从 DB 读取成员状态
    return atschema.MemberStatusReady, nil      // ← 永远 Ready
}
func (m *TeamMember) UpdateStatus(...) (bool, error) {
    // TODO(#9.65): 读取旧状态 → 短路等值 → 写 DB → 发 MemberStatusChangedEvent
    logger.Info(...).Msg("TeamMember stateupdate")  // ← 只打日志
    return true, nil
}
```

**连带影响**：`stream_controller.go` L566/L593 注释明确写着 "TeamMember.Status() 是 stub（始终返回 READY）"——SHUTDOWN_REQUESTED 状态下的优雅关流逻辑**永不触发**，成员收到停机请求后仍继续产出。

**矛盾点**：TeamDatabase 的 `sql_member_dao.go` 已有 `GetMember/UpdateMember` 完整能力，**DB 层就绪但成员层未接线**。

**修复方案**:
```go
func (m *TeamMember) Status(ctx context.Context) (atschema.MemberStatus, error) {
    row, err := m.db.GetMember(ctx, m.sessionID, m.MemberName)
    if err != nil || row == nil {
        return atschema.MemberStatusReady, err
    }
    return parseMemberStatus(row.Status), nil
}
// UpdateStatus: 读取旧状态 → 等值短路 → UpdateMember 写 DB → 发布 MemberStatusChangedEvent
```

---

### S-14: `resolveBuiltinTools`/`resolveRails` 返回错误 — harness_config.yaml 声明式加载路径必报错

**Go 问题** (`internal/agentcore/harness/harness_config/builder.go` L293-376):
```go
func resolveBuiltinTools(...) {
    // L304:
    return nil, fmt.Errorf("内置工具组 '%s' 实例化尚未实现...", group)
    // L330/L333: package/entry_point 工具加载同样返回错误
}
func resolveRails(...) {
    // L370/373/376: Rail 加载返回错误
}
```

**矛盾点**：计划 9.38-49（工具集）和 9.19-23（Rails）均已标 ✅——工具和 Rail 的**直接实现**都已完成（Shell/Filesystem/Code/MCP 工具、各 Rail），唯独这条 **YAML 声明式加载路径**被遗漏。任何 `harness_config.yaml` 中声明 `tools:` 或 `rails:` 的配置在运行时必然报错。

**修复方案**: 为 `builtinToolGroups` 中每个组名接已实现工具：
```go
switch group {
case "shell":
    tools = append(tools, shelltool.New(...))
case "filesystem":
    tools = append(tools, fstool.NewDefaultSet(...))
// ... 对齐 Python BUILTIN_TOOL_GROUPS 注册表
}
```

---

### S-15: `DeepAgent.LoadHarnessConfig/UnloadHarnessConfig` 返回"尚未实现"错误

**Go 问题** (`internal/agentcore/harness/deep_agent.go` L1013-1026):
```go
func (d *DeepAgent) LoadHarnessConfig(...) error {
    return fmt.Errorf("尚未实现: ...")   // ← L1015
}
func (d *DeepAgent) UnloadHarnessConfig(...) error {
    return fmt.Errorf("尚未实现: ...")   // ← L1023
}
```

**连带影响**：`EnqueueHarnessConfig`（L1004）会将配置变更入队 → 下次 `Stream()` 时触发**必现报错**。`InitWorkspace`（L1043）只打日志空转。

**修复方案**: 接 S-14 的 resolveBuiltinTools/resolveRails 完成后，实现 harness 配置的加载/卸载/应用。

---

### S-16: `SpawnManager.resolveMemberModelFromDB` 返回 nil — 从 DB 恢复的成员无模型

**Go 问题** (`internal/agent_teams/agent/spawn_manager.go` L446-453):
```go
func resolveMemberModelFromDB(configurator *AgentConfigurator, modelRefJSON string) any {
    if configurator == nil || modelRefJSON == "" {
        return nil
    }
    // 简化实现：从 model_ref_json 解析模型引用
    // TODO(#9.64): 完整实现 model_ref_json 解析 + ResolveMemberModelFromPool
    return nil     // ← 非空 modelRefJSON 也返回 nil
}
```

**矛盾点**：计划 9.64（记忆/模型池）已标 ✅，但成员模型从 DB 恢复路径解析 model_ref_json 的逻辑缺失 → **重启/恢复场景下成员拿到 nil 模型，无法调用 LLM**。

**修复方案**: 解析 model_ref_json 并从模型池解析：
```go
func resolveMemberModelFromDB(configurator *AgentConfigurator, modelRefJSON string) any {
    if configurator == nil || modelRefJSON == "" {
        return nil
    }
    var ref ModelRef
    if err := json.Unmarshal([]byte(modelRefJSON), &ref); err != nil {
        return nil
    }
    return configurator.ResolveMemberModelFromPool(ref) // 9.64 模型池能力已实现，接线即可
}
```

---

### S-17: `TeamHarness` 其余 9 个方法桩（Steer/FollowUp/Abort/RunStreaming 外围）

**Go 问题** (`internal/agent_teams/harness.go` L240-302):
```go
Steer(...) error { return nil }                    // 转向指令被静默吞掉
FollowUp(...) error { return nil }                 // 追问被静默吞掉
Abort(...) error { return nil }                    // 中止被静默吞掉
RegisterRail/UnregisterRail(...) error { return nil } // 动态 Rail 注册无效果
HasPendingInterrupt() bool { return false }        // 中断检测恒 false
IsPendingInterruptResumeValid(...) bool { return false }
InitCwdForRound(...) {}                            // 空实现
PersistTeamDBState/MarkTeamCleaned/MarkTeamBuilt(...) {} // ⤵️ 9.56
```

**影响**：外层 API（如 DeepAdapter 的团队中断分流、cancelTeamWork）调用这些方法时得到"成功"假象，实际什么都没发生。与 S-10 同文件，建议一并回填。

**修复方案**：对齐 Python `TeamHarness` 各方法：Steer/FollowUp 转发到底层 Agent 的对应机制；Abort 取消 streamController 的当前轮；HasPendingInterrupt 查 streamController 状态。

---

### S-18: `evolutionOutputSchemasToMaps` 类型断言必然失败 — 演进事件全量静默丢弃（已实测验证）

**已实测确认**：`EvolutionRail.DrainPendingApprovalEvents`（evolution_rail.go L273）返回 **`[]*stream.OutputSchema`**（具体切片类型）。

**Go 问题** (`deep_adapter_team.go` L1172-1188):
```go
func evolutionOutputSchemasToMaps(events any) []map[string]any {
    if events == nil {
        return nil
    }
    if slice, ok := events.([]any); ok {   // ← []*stream.OutputSchema 断言 []any 必然失败
        // ...
    }
    return nil     // ← 永远走到这里，所有演进事件被静默丢弃
}
```

调用处（`watchTeamEvolutionAndPushTeam` L874-875）：
```go
events := rail.DrainPendingApprovalEvents(true, nil)   // 返回 []*stream.OutputSchema
eventsMap := evolutionOutputSchemasToMaps(events)       // 永远 nil
```

**影响链**：`eventsMap` 恒为 nil → `len(eventsMap) == 0` 恒真 → watcher 视为"无事件"→ 60 秒后 `activeCycleRequestID != ""` 场景超时退出 / 普通场景永久空转轮询。**即使修复了 S-03 的 gateway_push，演进事件也永远到不了推送层**——这是比 S-03 更前置的断点。

**修复方案**（具体类型优先 + []any 兜底）：
```go
func evolutionOutputSchemasToMaps(events any) []map[string]any {
    if events == nil {
        return nil
    }
    // 优先处理具体类型 []*stream.OutputSchema（DrainPendingApprovalEvents 的实际返回类型）
    if schemas, ok := events.([]*stream.OutputSchema); ok {
        result := make([]map[string]any, 0, len(schemas))
        for _, s := range schemas {
            if m := chunkToMap(s); m != nil {
                result = append(result, m)
            }
        }
        return result
    }
    // 兜底：[]any
    if slice, ok := events.([]any); ok {
        result := make([]map[string]any, 0, len(slice))
        for _, item := range slice {
            if m := chunkToMap(item); m != nil {
                result = append(result, m)
            }
        }
        return result
    }
    return nil
}
```

---

### S-19: CJK 字节截断 — sessionops 两处 80 字节预览 + dreaming sweeper 四处压缩截断，中文内容可能截出乱码且量级缩小 3 倍

Python 的 `str[:]` 切片按**字符**计，Go 的 `s[:]` 按**字节**计。中文场景（本项目主场景）每个汉字 3 字节，所有逐字对齐的截断点全部失真。这是全项目已知问题模式（2026-10-13/2026-10-05 审查已修复 viking/ltm/stream_event 等多处），本轮又发现 6 处遗漏。

**问题 1：SessionOps 预览截断**（`sessionops/session_ops.go` L331-334 与 L413-416）

**Python 参考** (`session_ops_service.py`): `removed_turn_content[:80]` / `preview[:80]` — 按字符截断，中文安全。

**Go 问题**:
```go
// RewindSession L331-334
if len(removedTurnContent) > 80 {
    contentPreview = removedTurnContent[:80]   // ← 80 字节 ≈ 26 个汉字，且可能落在 UTF-8 多字节序列中间 → 非法字符串
}
// ListSessionTurns L413-416 同款
if len(preview) > 80 {
    preview = preview[:80]
}
```

截断点落在多字节序列中间时产生非法 UTF-8，前端渲染乱码。

**问题 2：Dreaming sweeper 压缩截断**（`dreaming/sweeper.go` L727/L734/L744/L759-760）

**Python 参考** (`sweeper.py` L345-362): `content[:2000]` / `content[:3000]` / `len(result) // 2` / `result[-max_chars:]` — 全部按字符。

**Go 问题**:
```go
if len(content) > 2000 {
    content = content[:2000]        // ← 2000 字节 ≈ 666 汉字（Python 语义是 2000 字）
}
...
estTokens := len(result) / 2       // ← 字节数/2 ≈ 1.5×字符数，比 Python 估算（字符数/2）放大 3 倍
if estTokens > maxCompressTokens { // ← 提前触发轮次截断/硬截断
    ...
    result = ui.TruncateHard + result[len(result)-maxChars:]  // ← 尾部截断同样按字节
}
```

**影响链**（dreaming 是中文用户核心场景）：`estTokens` 字节口径放大 3 倍 → 稳态对话约 1/3 体量即触发截断 → 压缩后保留的内容被砍掉约 2/3，且压缩结果可能含非法 UTF-8（轮次裁剪后再次进入 [User]: 拼接循环，坏字节污染整个压缩文本，直接导致 LLM 提取质量下降或 JSON 解析失败）。

**修复方案**：统一改 rune 口径（项目内已有先例：`rails/stream_event_helpers.go` L222、`memory/lite/manager_impl.go` L1518、`memory/external/viking_provider.go` L766 均为 `runes := []rune(s)` 模式）：
```go
// sessionops：预览截断
if runeLen := len([]rune(removedTurnContent)); runeLen > 80 {
    contentPreview = string([]rune(removedTurnContent)[:80])
}
// sweeper：四处截断 + token 估算同理
if runeLen := len([]rune(content)); runeLen > 2000 {
    content = string([]rune(content)[:2000])
}
estTokens := len([]rune(result)) / 2
...
runes := []rune(result)
if len(runes) > maxChars {
    result = ui.TruncateHard + string(runes[len(runes)-maxChars:])
}
```

---

### S-20: `chunkToMap` 对所有 struct 类型 chunk 恒返回 nil — 整个团队流在 adapter 层被静默丢弃（已实测验证）

**已实测确认**：全项目 grep `GetType() string` / `GetPayload() any` / `Payload() any`（方法定义），除 `chunkToMap` 自身定义的接口外，**无任何类型实现这些方法**（唯一实现者 `ToolInfo` 的 `GetType` 是工具信息字段，非流 chunk）。

**Python 参考** (`stream_utils.py` L33-34):
```python
if hasattr(chunk, "type") and hasattr(chunk, "payload"):
    return _parse_typed_chunk(chunk, _has_streamed_content)
```
Python 用 `hasattr` 鸭子类型检查，`OutputSchema`（type + payload 字段）天然命中。

**Go 问题** (`deep_adapter_team.go` L1101-1141):
```go
func chunkToMap(chunk any) map[string]any {
    if m, ok := chunk.(map[string]any); ok { return m }
    type outputSchemaLike interface {
        GetType() string      // ← stream.OutputSchema 没有此方法！
        GetPayload() any      // ← stream.OutputSchema 没有此方法！
    }
    if os, ok := chunk.(outputSchemaLike); ok { ... }   // ← 永远不命中
    type payloadGetter interface { Payload() any }      // ← 同样无实现者
    if pg, ok := chunk.(payloadGetter); ok { ... }      // ← 永远不命中
    return nil   // ← 恒返回 nil
}
```

Go 的 `stream.OutputSchema`（base.go L28-37）只有**字段** `Type/Payload`，无 `GetType()/GetPayload()/Payload()` 方法——Go 接口断言需要**方法集**而非字段名，与 Python `hasattr`（字段或方法均可）语义根本不同。

**实际 chunk 类型链路**（已验证）：
- `RunAgentTeamStreaming` 返回 `chan stream.Schema`（runtime/team_runner.go L89/L431）
- 首帧 readyChunk 是 `*atschema.TeamOutputSchema`（L459，`NewTeamOutputSchema` 构造）
- leader 后续 chunks 来自 `activation.Agent.Stream()`，同为 OutputSchema 家族
- `consumeStreamWithQuery` L644-651：`chunkMap := chunkToMap(chunk); if chunkMap == nil { continue }` —— **所有 chunk 全部被 continue 跳过**

**影响链（灾难级）**：`receivedChunks` 恒 0 → 步骤 6 的"无 chunk → broadcast team.error"逻辑……需进一步核对（若 `receivedChunks==0` 走 team.error，则前端表现为"团队执行失败"；若不走，则表现为永久 processing 挂起）。无论哪种，**团队模式的所有流式输出（chat.delta/tool_call/team.completed 等）完全无法到达前端**。这是本轮审查最严重的问题——团队功能在 adapter 层整体断链。

**修复方案**：为 `chunkToMap` 增加对具体类型的直接处理（字段访问而非方法断言）：
```go
func chunkToMap(chunk any) map[string]any {
    if chunk == nil { return nil }
    if m, ok := chunk.(map[string]any); ok { return m }
    // 直接处理 stream.OutputSchema 及其嵌入结构（TeamOutputSchema 等）
    type typedPayload interface {
        SchemaType() string
        GetPayloadField() any   // 若不便加方法，用下面的类型 switch
    }
    switch s := chunk.(type) {
    case *stream.OutputSchema:
        return outputSchemaToMap(s.Type, s.Payload, nil, nil)
    case stream.OutputSchema:
        return outputSchemaToMap(s.Type, s.Payload, nil, nil)
    case *atschema.TeamOutputSchema:
        return outputSchemaToMap(s.Type, s.Payload, s.SourceMember, s.Role)
    }
    return nil
}

// outputSchemaToMap 组装 map，附 type/role/source_member（对齐 Python _parse_typed_chunk）
func outputSchemaToMap(typ string, payload any, sourceMember *string, role *atschema.TeamRole) map[string]any {
    if payload == nil { return nil }
    if m, ok := payload.(map[string]any); ok {
        result := make(map[string]any, len(m)+1)
        for k, v := range m { result[k] = v }
        result["type"] = typ
        if sourceMember != nil { result["source_member"] = *sourceMember }
        if role != nil { result["role"] = string(*role) }
        return result
    }
    return nil
}
```
注意：`isLeaderOutput`/`isTeammateOutput`（team_helpers.go L437/L467）读取 `chunk["role"]`（string 比较），修复时确保 role 序列化为小写字符串 "leader"/"teammate"（对齐 Python `TeamRole.LEADER.value`）。

---

### S-21: `/evolve` 与 `/evolve_simplify` 的审批弹框必然缺失 — `ApprovalEvent` 是 `*stream.OutputSchema`，`teamHelperEventAsMap` 恒 nil（已实测验证）

**已实测确认**：`EvolutionRequestResult.ApprovalEvent` 字段类型是 `*stream.OutputSchema`（evolution/contracts.go L70/L84），`TeamSkillEvolutionRail.RequestUserEvolution`（L745）返回该结构体。

**Python 参考** (`team_helpers.py` L199-209):
```python
def _approval_chunk_from_event(evt: Any) -> dict[str, Any] | None:
    parsed = parse_stream_chunk(evt)     # ← OutputSchema → dict（鸭子类型 hasattr 命中）
    if not isinstance(parsed, dict) or parsed.get("event_type") != "chat.ask_user_question":
        return None
```

**Go 问题** (`team_helpers.go` L557-573):
```go
func approvalResultFromEventOrItems(skillName string, event any, ...) map[string]any {
    approvalChunk := approvalChunkFromEvent(teamHelperEventAsMap(event))  // ← 恒 nil
    ...
}
// teamHelperEventAsMap L581-589：只接受 map[string]any，*stream.OutputSchema 返回 nil
func teamHelperEventAsMap(v any) map[string]any {
    if m, ok := v.(map[string]any); ok { return m }
    return nil   // ← ApprovalEvent 是 *stream.OutputSchema，恒走这里
}
```

**影响链**：`/evolve <skill> <query>` → `RequestUserEvolution` 成功生成审批事件 → `approvalResultFromEventOrItems` 中 `approvalChunk == nil` → 走 `items` 分支：`/evolve` 传 `nil`（M-04）→ 返回 "Skill 'x' 未生成新的团队技能演进经验"（**误导性文案：实际已生成**）；`/evolve_simplify` 传 `Actions`（[]map，非空时）→ 返回 **error** "审批事件为空或格式无效"。两个入口的审批弹框（approval_chunks）**永远无法到达前端**，用户技能演进审批流完全断链。

**修复方案**：
```go
// teamHelperEventAsMap 增加 OutputSchema 分支
func teamHelperEventAsMap(v any) map[string]any {
    if v == nil { return nil }
    if m, ok := v.(map[string]any); ok { return m }
    if os, ok := v.(*stream.OutputSchema); ok && os != nil {
        if m, ok := os.Payload.(map[string]any); ok {
            result := make(map[string]any, len(m)+1)
            for k, val := range m { result[k] = val }
            result["type"] = os.Type
            return result
        }
    }
    return nil
}
```
并确认 `BuildSkillApprovalEvent`/`BuildSimplifyApprovalEvent`（approval_events.go L121/L213）产出的 Payload 是含 `event_type: "chat.ask_user_question"` 的 map（Python 侧同款事件结构）。

---

### S-22: `ResolveTeamRebuildFollowup` 被错误放在 isFirstRequest 块内 — 后续请求的 `/evolve_rebuild` 完全失效

**Python 参考** (`team_helpers.py` L730-749):
```python
team_name = team_spec.team_name

followup_prompt, rebuild_error = await _resolve_team_rebuild_followup(...)  # ← try 块内、if is_first_request 之前
if rebuild_error is not None: ... return
if followup_prompt is not None: query = followup_prompt

try:
    if is_first_request:
        ...
```
（更准确：Python 在 team_spec 获取成功后、进入 `if is_first_request:` 分支**之前**统一解析 rebuild followup——首次和后续请求都会经过。）

**Go 问题** (`deep_adapter_team.go` L294-337):
```go
if isFirstRequest {
    ...
    teamSpec := teamManager.GetEnrichedTeamSpec(...)   // L309
    teamName := teamSpec.TeamName
    // 步骤 2: 解析 /evolve_rebuild followup
    followupPrompt, rebuildError := ResolveTeamRebuildFollowup(...)  // ← L325，在 isFirstRequest 块内
    ...
}
// ─── 后续请求：通过 TeamManager.Interact 路由 ───   ← L429 直接 Interact，无 rebuild followup 解析
```

**影响链**：用户在已有团队流的 session 里发送 `/evolve_rebuild ...` → 走后续请求分支 → 直接 `teamManager.Interact(sessionID, query)` 把 rebuild 命令原文当普通 query 投给团队 → agent 收到原始 slash 命令而非重建 followup prompt。重建审批流程（含 rebuild_error 的 chat.error 早退）全部失效。

**修复方案**：把 `ResolveTeamRebuildFollowup` 调用移到 `if isFirstRequest` 判断之前（对齐 Python 位置）；后续请求分支同样应用 `followupPrompt`/`rebuildError`。注意 Go 版本首次请求块内已正确实现（L325-337），只需上移。

---

### S-23: `consumeStreamWithQuery` 的 `DrainPendingApprovalEvents(true, nil)` — wait=true + nil timeout，watcher 循环可阻塞最长 720 秒

**Python 参考** (`team_helpers.py` watch 循环):
```python
events = await rail.drain_pending_approval_events(wait=False) or []   # ← 非阻塞
```

**Go 问题** (`deep_adapter_team.go` L874):
```go
// Python: events = await rail.drain_pending_approval_events(wait=False) or []
events := rail.DrainPendingApprovalEvents(true, nil)   // ← wait=true，与注释声明的 Python 语义相反
```

**阻塞机制**（已验证 `DrainPendingHostEvents` evolution_rail.go L206-231）：`wait=true && len(bgTasks)>0` 时筛选未完成任务 → `timeout == nil` → 取 `GetEvolutionTotalTimeoutSecs()`（默认 720s）作为 effectiveTimeout → **在等待任务完成期间该 goroutine 持续阻塞**。

**影响链**：每个 sweep tick（`teamEvolutionIdleSleepSec`）调用一次 → 若 rail 有未完成后台任务（演进分析中的常态），watcher goroutine 单次 tick 即阻塞至任务完成或 720s 超时 → `ctx.Done()` 检查频率骤降 → session 取消/关闭时 watcher 响应延迟；且阻塞期间 `lastEventAt` 不更新，超时判定失真。

**修复方案**：改为非阻塞语义：
```go
events := rail.DrainPendingApprovalEvents(false, nil)   // 对齐 Python wait=False
```
或传显式短超时 `timeout := time.Duration(teamEvolutionIdleSleepSec) * time.Millisecond`。同时修正 L873 注释（当前注释写的是 wait=False，代码却是 true——注释与实现背离正说明这是笔误）。

---

### S-24: `runAgentTeam`/`runAgentTeamStreaming` leader 路径 inputs 硬编码 nil — 用户 query 整体丢失（历次审查遗留 P0，第 4 次记录）

**Python 参考** (`team_runner.py` L194/L260):
```python
return await activation.agent.invoke(inputs, session=activation.session)
async for chunk in activation.agent.stream(inputs, session=activation.session):
```

**Go 问题** (`team_runner.go` L414/L467):
```go
result, err := activation.Agent.Invoke(ctx, nil, agentinterfaces.WithSession(activation.Session))
agentCh, streamErr := activation.Agent.Stream(ctx, nil, agentinterfaces.WithSession(activation.Session))
```

**调用链验证**：`deep_adapter_team.go` L631 传入了 `map[string]any{"query": initialQuery}`，但 leader 路径最后一步丢弃。`TeamAgent.Invoke` L556 `inputs["query"]` 读 nil map → `state.PendingUserQuery` 不设置 → `coordination.EnqueueUserInput("")` 入队空事件 → **团队启动后 leader 收不到首轮用户问题**。对比：`RunTeamMember`（L233）与 `RunBaseTeam`（L131）都正确转发 `inputsDict`，唯独 leader 路径丢失。

> 历史：2026-10-02/10-09/10-13 审查均记录此 P0，至今未修。本轮第 4 次确认。

**修复方案**（约 3 行）：
```go
inputsDict, _ := inputs.(map[string]any)
result, err := activation.Agent.Invoke(ctx, inputsDict, agentinterfaces.WithSession(activation.Session))
```

---

### S-25: 流式拒绝路径返回 nil channel + nil error — 调用方 `range` 永久阻塞 + 跳过 Python finally 三步清理

**Python 参考** (`team_runner.py` L238-246 + L273-275): reject 分支 `return` 结束生成器，**finally 仍执行** finalize + `_close_team_interact_gate` + `session.post_run()`。

**Go 问题** (`team_runner.go` L443-451):
```go
if IsTeamRejectKind(action.Kind) {
    ...
    return nil, nil     // ← ① nil channel；② 跳过 L469-471 的 Finalize/gate/PostRun
}
```

**两重后果**：
1. `deep_adapter_team.go` L644 `for chunk := range streamCh` 对 nil channel **永久阻塞**，goroutine 泄漏（defer 清理虽执行但流永不结束，waiter 永不收尾）
2. 拒绝场景跳过 Finalize（池条目滞留）、gate 不关闭、session PostRun 不执行（检查点不提交）

**修复方案**：
```go
if IsTeamRejectKind(action.Kind) {
    logger.Warn(...)
    _ = GetTeamRuntimeManager().Finalize(ctx, spec.TeamName, activation.Session.GetSessionID())
    closeTeamInteractGate(spec.TeamName, activation.Session.GetSessionID())
    _ = activation.Session.PostRun(ctx)
    ch := make(chan stream.Schema)
    close(ch)   // 已关闭空 channel：range 立即结束
    return ch, nil
}
```
（非流式路径 L404-411 同理需补三步清理。）

---

### S-26: `RecoverFromSession` 类型断言恒失败 — COLD_RECOVER 冷恢复路径 100% 报错（已实测验证）

**写入侧** (`recovery_manager.go` L125-136 `PersistLeaderConfig`):
```go
specBytes, _ := json.Marshal(spec)
var specMap map[string]any
_ = json.Unmarshal(specBytes, &specMap)
payload["spec"] = specMap      // ← bucket["spec"] 存的是 map[string]any
```

**读取侧** (`team_agent.go` L1040-1056):
```go
specAny := bucket["spec"]
spec, ok := specAny.(atschema.TeamAgentSpec)          // ← map 断言 struct，恒失败
...
runtimeCtx, ok := contextAny.(atschema.TeamRuntimeContext)   // ← 同样恒失败
```

**影响链**：进程重启后 pool 空、DB 有行、session 有桶 → `DecideRunAction` 判 COLD_RECOVER → `RecoverFromSession` 必然返回"team spec 反序列化失败"→ **冷恢复完全不可用**。对比：`resolveSpecFromSessionBucket`（team_runner.go L521-529）用了正确的 map → JSON 桥接模式。

**修复方案**（对齐 resolveSpecFromSessionBucket 模式）：
```go
specMap, ok := specAny.(map[string]any)
if !ok { return nil, fmt.Errorf("team spec 反序列化失败") }
data, _ := json.Marshal(specMap)
var spec atschema.TeamAgentSpec
if err := json.Unmarshal(data, &spec); err != nil { return nil, ... }
// context 同理
```

---

### S-27: 子进程 TEAM_AGENT 分支以 leader 路径运行 spawned teammate — 破坏 leader-only pool 不变式

**Python 参考** (`child_process.py` L170-193):
```python
agent = await TeamAgent.from_spawn_payload(agent_config.payload)
# Spawned member (teammate / human-agent), never a leader —
# ``member=True`` skips activate/dispatch and keeps the leader-only
# pool invariant intact.
return await Runner.run_agent_team(agent, inputs, member=True, session=session)
```

**Go 问题** (`child.go` L332-355):
```go
case SpawnAgentKindTeamAgent:
    ...
    return runFn(ctx, agentTeam, inputs, false, false, agentConfig.SessionID)
    //                                                ↑ member=false → 走 Activate→dispatch→pool.Add
```

**两重偏差**：
1. `member=false`：子进程 teammate 走 leader 完整路径，以 team_name 在池中注册条目——与真实 leader 的池条目冲突/覆盖（Python 注释明确 member=True 是为保住 leader-only pool invariant）
2. 从 Payload 裸提取 spec/team_name 字符串再解析，而非 `from_spawn_payload` 构建已配置实例；且完全无视 `streaming` 标志（子进程 teammate 永远非流式）

**修复方案**：TEAM 分支构建 TeamAgent 实例（FromSpawnPayload 等价物或工厂注入），`member` 参数传 `true`；streaming 时走注入的流式函数。

---

### S-28: 13.4 多模态路径：空文本图片分块必然报错 — Python 允许空 text，Go `loadMultimodalData` 拒绝空 data

**Python 参考** (`embed_chunks.py` L63-71 + `document.py` L271-274):
```python
multimodal_doc = (
    MultimodalDocument()
    .add_field("text", chunk.text or "")     # ← 显式 or ""，空文本合法
    .add_field("image", file_path=path)
)
# document.py _load_multimodal_data: isinstance(data, str) and kind == "text" → 直接返回，空串通过
```

**Go 问题**（`embed_chunks.go` L190-194 + `common/document.go` L339-345）:
```go
// embed_chunks.go L192 — 注释自称对齐 "chunk.text or ""，但直接传 chunk.Text
if _, err := doc.AddField(common.ModalityText, chunk.Text); err != nil {
    return err   // ← chunk.Text == "" 时 AddField 内部报错
}
// document.go loadMultimodalData L340：
if data == "" {
    return "", "", exception.BuildError(..., "必须提供 %s 类型的数据或文件路径", ...)  // ← 空串被拒
}
```

**影响链**：图片分块无 caption（`Text=""`，`use_caption_for_images=false` 时图片 chunk 的 text 就是空——Python 明确预期此场景）→ `ComputeChunkEmbeddings` 返回错误 → `BuildIndex` 中断。现有测试 `makeImageChunk` 全用非空文本，未覆盖。多模态索引构建在"无 caption"配置下**必然失败**。

**修复方案**（对齐 Python 语义，改 `loadMultimodalData`）：
```go
// document.go loadMultimodalData 的 data 模式分支
if data == "" && kind == ModalityText {
    // Python: text 模态允许空串（chunk.text or "" 场景）
    return kind, "", nil
}
if data == "" {
    return "", "", exception.BuildError(...)
}
```

---

### S-29: 13.2 BuildIndex 查重阶段 `collection.Get` 错误被静默吞掉 — 查重保护失效，重复 doc_id 破坏不变量

**Python 参考** (`chroma_indexer.py` L143-154 + L188-196):
```python
if doc_id not in filter_values and collection.get(where={self.doc_id_field: doc_id}).get("ids"):
    duplicate_doc_ids.append(doc_id)
# get 失败 → 异常向上抛 → 外层 except 转 ADD_DOC_RUNTIME_ERROR → build 失败
```

**Go 问题** (`indexer/chroma.go` L195-202):
```go
getResult, err := collection.Get(ctx, chromav2.WithWhere(whereFilter))
if err == nil && getResult != nil {   // ← err != nil 时既不报错也不记日志
    ids := getResult.GetIDs()
    ...
}
// Get 失败 → 该 doc_id 不进 duplicate 列表 → 查重静默通过
```

**影响链**：Chroma 连接抖动/临时故障时 `Get` 出错 → 查重形同虚设 → 重复 doc_id 数据写入 → 破坏"同 doc_id 只允许 update"不变量，后续检索返回重复结果。且全程无日志，故障不可追溯（违反日志同步规则 3.4）。

**修复方案**（对齐 Python 失败即中断）：
```go
getResult, err := collection.Get(ctx, chromav2.WithWhere(whereFilter))
if err != nil {
    logger.Error(logComponent).Str("doc_id", docID).Err(err).
        Msg("Failed to build index: duplicate check query failed")
    return false, exception.BuildError(exception.StatusRetrievalIndexingAddDocRuntimeError,
        exception.WithParam("error_msg", fmt.Sprintf("duplicate check failed for doc_id=%s: %v", docID, err)))
}
```

---

## 一般问题 (M)

### M-01: `consumeStreamWithQuery` 缺少 `TeamStreamLogger` 集成

**Python 参考** (`team_helpers.py` L927-931):
```python
lg: TeamStreamLogger | None = None
if stream_trace_enabled:
    traces_dir = get_agent_teams_home() / "traces"
    traces_dir.mkdir(parents=True, exist_ok=True)
    lg = TeamStreamLogger(file_path=str(traces_dir / f"dump-team-{session_id}.txt"))
```

**Go 问题** (`deep_adapter_team.go` L617-626):
```go
_ = streamTraceEnabled // TODO: TeamStreamLogger 对齐
```

**修复方案**: Go 端监控模块已实现 `TeamStreamLogger`（见记忆：monitor-module-complete），导入并接线：
```go
var lg *monitor.TeamStreamLogger
if streamTraceEnabled {
    tracesDir := filepath.Join(ateampaths.GetAgentTeamsHome(), "traces")
    _ = os.MkdirAll(tracesDir, 0o755)
    lg = monitor.NewTeamStreamLogger(filepath.Join(tracesDir, fmt.Sprintf("dump-team-%s.txt", sessionID)))
}
```

---

### M-02: `RunAgentTeamStreaming` 调用缺少 envs 参数传递

**Python 参考** (`team_helpers.py` L932-938):
```python
async for chunk in Runner.run_agent_team_streaming(
    agent_team=team_spec,
    inputs={"query": initial_query},
    session=session_id,
    envs=envs,          # ← hide_dm / stream_trace 环境变量传入
    stream_logger=lg,
):
```

**Go 问题** (`deep_adapter_team.go` L631):
```go
streamCh, err := ateamruntime.RunAgentTeamStreaming(ctx, teamSpec, map[string]any{"query": initialQuery}, false, false, sessionID, nil)
//                                                                                                              最后参数 nil = envs 未传
```

**影响**: `hide_dm` 指令对 team runtime 不生效（DM 消息不会被隐藏），`/debug` 追踪环境变量无法传递到 runtime 层。

**修复方案**: 
```go
streamCh, err := ateamruntime.RunAgentTeamStreaming(ctx, teamSpec, map[string]any{"query": initialQuery}, false, false, sessionID, envs)
```
需确认 Go 端 `RunAgentTeamStreaming` 的 envs 参数签名是否接受 `map[string]any`。

---

### M-03: `HandleTeamSlashCommand` 的 `/evolve` 结果缺少字符串分支处理

**Python 参考** (`team_helpers.py` L597-601):
```python
if isinstance(evolve_result, str) and evolve_result.strip():
    return {
        "output": f"Skill '{skill_name}' 演进请求已提交，请等待审批。",
        "result_type": "answer",
    }
```

**Go 问题** (`team_helpers.go` L320-336): 直接进入 `approvalResultFromEventOrItems`，未处理 `evolveResult` 为纯字符串提示的情况（Python 端 SDK 可能返回字符串而非结构体）。

**修复方案**: 
```go
// 对齐 Python: isinstance(evolve_result, str) 分支
if evolveResult != nil {
    if msg, ok := evolveResultAsString(evolveResult); ok && strings.TrimSpace(msg) != "" {
        return map[string]any{
            "output":      fmt.Sprintf("Skill '%s' 演进请求已提交，请等待审批。", skillName),
            "result_type": "answer",
        }
    }
}
```

---

### M-04: `HandleTeamSlashCommand` 的 `/evolve` 分支 items 传 nil，丢失 Records

**Python 参考** (`team_helpers.py` L605):
```python
items=list(getattr(evolve_result, "records", []) or []),
```

**Go 问题** (`team_helpers.go` L333):
```go
return approvalResultFromEventOrItems(
    skillName,
    evolveResult.ApprovalEvent,
    nil, // ← Python: items=list(getattr(evolve_result, "records", []) or [])
    ...
)
```

Go 端 `EvolutionRequestResult` 已有 `Records []checkpointing.EvolutionRecord` 字段（contracts.go L72），但未传递。

**影响**: 当审批事件为空但已生成 records 时，本应返回 "已生成演进经验但审批事件无效"（invalid_output），Go 端错误返回 "未生成新的演进经验"（no_changes_output），误导用户。

**修复方案**:
```go
items := make([]map[string]any, 0, len(evolveResult.Records))
for _, rec := range evolveResult.Records {
    items = append(items, recordToMap(rec)) // 或简单地用长度判断
}
// 若 approvalResultFromEventOrItems 只关心 len(items)，可直接传长度语义
```

---

### M-05: `ensureTeamEvolutionWatcher` 的 `PopTeamEvolutionWatcher` 在 panic 时不执行

**Go 问题** (`deep_adapter_team.go` L564-570):
```go
go func() {
    defer logger.Info(...) // ← 仅日志 defer
    d.watchTeamEvolutionAndPushTeam(watcherCtx, channelID, sessionID, rail)
    // ← 若上方函数 panic，以下两行不执行
    tm.PopTeamEvolutionWatcher(sessionID)
    onTeamWatcherDone(sessionID)
}()
```

**影响**: watcher panic 后 TeamManager 中残留失效的 cancel 函数，后续 `ensureTeamEvolutionWatcher` 误判 "watcher already running"，演进监控永久失效。

**修复方案**:
```go
go func() {
    defer tm.PopTeamEvolutionWatcher(sessionID)      // ← 放入 defer，保证任意退出路径执行
    defer onTeamWatcherDone(sessionID)
    defer func() {
        if r := recover(); r != nil {
            logger.Error(logComponent).Any("panic", r).Str("session_id", sessionID).
                Msg("watchTeamEvolutionAndPushTeam: panic recovered")
        }
    }()
    d.watchTeamEvolutionAndPushTeam(watcherCtx, channelID, sessionID, rail)
}()
```
（Python 通过 `add_done_callback` 保证任意 Task 结束路径都回调，语义等价）

---

### M-06: `ensureMonitorForActiveRuntime` 缺少已有 monitor 的 `is_running` 短路检查

**Python 参考** (`team_helpers.py` L141-143):
```python
existing = tm.get_monitor(session_id)
if existing is not None and existing.is_running:
    return
```

**Go 问题** (`deep_adapter_team.go` L486-508): 未检查已有 monitor，直接调用 `tm.EnsureMonitor`。若 `EnsureMonitor` 内部无幂等检查（需验证 team_manager_monitor.go），重复调用可能创建多个 monitor goroutine。

**修复方案**: 
```go
if existing := tm.GetMonitor(sessionID); existing != nil && existing.IsRunning() {
    return
}
```
或确认 `EnsureMonitor` 内部已含此检查并在注释中标注。

---

### M-07: `consumeMonitorEvents` 缺少 panic 恢复

**Python 参考** (`team_helpers.py` L1077-1083): `except Exception` 捕获并记录。

**Go 问题** (`deep_adapter_team.go` L746-776): 循环无 recover。

**修复方案**: 添加 `defer recover`（同 S-06 模式）。

---

### M-08: `processTeamMessageStream` 后续请求分支错误消息过于笼统

**Python 参考** (`team_helpers.py` L793-801): 错误固定为 "interact failed"（Python 也笼统）。

**Go 问题** (`deep_adapter_team.go` L438-459): `err != nil` 和 `!ok` 两个分支返回相同的 "interact failed"，但 Go 有 error 信息可用却被丢弃。

**修复方案**:
```go
if err != nil {
    // ... 
    Payload: map[string]any{"event_type": "chat.error", "error": fmt.Sprintf("interact failed: %v", err)},
}
// !ok 分支保持 "interact failed"（对齐 Python 非 active session 场景）
```

---

### M-09: `AttachDistributedHooksForRunnerRuntime` 调用传 nil 丢失 channelID

**Python 参考** (`team_helpers.py` L963-967):
```python
await tm.attach_distributed_hooks_for_runner_runtime(
    team_name=ready_team_name,
    session_id=session_id,
    channel_id=channel_id,       # ← 传递 channel_id
)
```

**Go 问题** (`deep_adapter_team.go` L696):
```go
tm.AttachDistributedHooksForRunnerRuntime(readyTeamName, sessionID, nil)  // ← nil 替代 channelID
```

**修复方案**: 传入 `channelID`（若第三参数类型不是 string，需检查签名对齐）。

---

### M-10: `consumeStreamWithQuery` 的 `team.completed` 分支重建事件丢失原始字段

**Python 参考** (`team_helpers.py` L979-995): 从 `parsed` 提取 member_count/task_count 构建新事件并 `continue`（原始 parsed 不广播）。

**Go 问题** (`deep_adapter_team.go` L700-711): 行为与 Python 一致（重建 processing_status 并 continue），但 `parsed["member_count"]` 若为 nil 时，广播的事件中 member_count 键值为 nil，Python 端同样。**实际差异**：Go 端 `parsed["member_count"]` 直接取值（any 类型），JSON 序列化 nil 为 `null`；Python 端 `.get()` 返回 None 同样为 null。**基本一致**，但 Go 端应确认 `team.completed` 原始 parsed 中的 `team.completed` 事件本身不再广播——与 Python 一致（continue 跳过）。

**降级说明**: 经复核该分支行为一致，此条仅为防御性提示——建议添加注释说明为何不广播原始事件。

---

### M-12: `ComputeChunkEmbeddings` 需确认跳过已有嵌入的逻辑

**Python 参考** (`embed_chunks.py`): 对已有 embedding 的 chunk 跳过重嵌入。

**Go 现状**: 需在 `embed_chunks.go` 中确认（本次审查未完整展开该文件，列为待验证项）。

**修复方案**: 若缺失，在嵌入循环开头添加：
```go
if chunk.Embedding != nil {
    continue
}
```

---

### M-13: `ForkSession` 元数据 `last_message_at` 缺少默认值

**Python 参考** (`session_ops_service.py` L133):
```python
"last_message_at": source_meta.get("last_message_at", 0),
```

**Go 问题** (`session_ops.go` L244):
```go
"last_message_at": sourceMeta["last_message_at"],  // ← 键不存在时为 nil，序列化为 null 而非 0
```

**修复方案**:
```go
lastMessageAt := sourceMeta["last_message_at"]
if lastMessageAt == nil {
    lastMessageAt = float64(0)
}
```
同类问题：`user_id`（Python 默认 ""，Go 为 nil）、`message_count`（Python 默认 0）。

---

### M-14: `ForkSession` 添加 `forked_from` 三层失败分支全部静默吞错

**Python 参考** (`session_ops_service.py` L86-94):
```python
try:
    data = json.loads(Path(target_history).read_text(encoding="utf-8"))
    for record in data:
        record["forked_from"] = {"session_id": source_session_id, "original_id": record.get("id")}
    Path(target_history).write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")
except Exception as exc:
    logger.warning("fork: failed to add forked_from to history: %s", exc)
```

**Go 问题** (`session_ops.go` L193-210):
```go
data, err := os.ReadFile(targetHistory)
if err == nil {                                   // ← 读取失败：无日志
    var records []map[string]any
    if err := json.Unmarshal(data, &records); err == nil {  // ← 解析失败：无日志
        ...
        if updated, err := json.MarshalIndent(records, "", "  "); err == nil {  // ← 序列化失败：无日志
            if writeErr := os.WriteFile(...); writeErr != nil {
                logger.Warn(...)  // ← 仅写文件失败有日志
            }
        }
    }
}
```

三层 `if err == nil` 嵌套，读取/解析/序列化任一失败都无任何日志（对齐规则 3.4 异常路径日志要求）。forked_from 元数据静默丢失后，后续按 forked_from 追溯分支来源的功能（如 Python 端的分支树展示）失效且无从排查。

**修复方案**：改为早返回结构，每个失败分支补 Warn：
```go
data, err := os.ReadFile(targetHistory)
if err != nil {
    logger.Warn(logComponent).Err(err).Msg("fork: failed to add forked_from to history")
    return ... // 或跳过该步骤继续后续流程
}
var records []map[string]any
if err := json.Unmarshal(data, &records); err != nil {
    logger.Warn(logComponent).Err(err).Msg("fork: failed to add forked_from to history")
} else {
    ... // 原写入逻辑，Marshal 失败同样补 Warn
}
```

---

### M-15: `getMemoryEngine` 缺少 `_VALID_ENGINES` 白名单校验 — 配置拼错时记忆功能静默全禁用

**Python 参考** (`external_memory_config.py` L27-39):
```python
_VALID_ENGINES = {"builtin", "external", "both", "none"}
def get_memory_engine(config: dict) -> str:
    value = str(mem.get("engine") or "builtin").strip().lower()
    return value if value in _VALID_ENGINES else "builtin"   # ← 非法值回退 builtin
```

**Go 问题** (`deep_adapter_rails.go` L1485-1496):
```go
func getMemoryEngine(config map[string]any) string {
    ...
    engine, _ := memCfg["engine"].(string)
    engine = strings.ToLower(strings.TrimSpace(engine))
    if engine == "" {
        return "builtin"
    }
    return engine   // ← 任意非空字符串直接返回，无白名单校验
}
```

**影响链**：配置拼错（如 `"externel"`）→ `getMemoryEngine` 返回 "externel" → `isBuiltinMemoryAllowed` = false（不等于 builtin/both）**且** `isExternalMemoryEnabled` = false（不等于 external/both）→ 双 false → 内置记忆 rail 与外接记忆 rail **都不挂载**，记忆功能整体静默禁用，无任何告警。Python 拼错时回退 builtin（内置记忆仍可用）。

**修复方案**：
```go
switch engine {
case "builtin", "external", "both", "none":
    return engine
default:
    logger.Warn(logComponent).Str("engine", engine).Msg("memory.engine invalid, fallback to builtin")
    return "builtin"
}
```

---

### M-16: `RewindSessionContext` 清空上下文失败仍返回 true — 与 Python 异常向上传播语义不一致

**Python 参考**: `context_engine.clear_context(...)` 无 try 包裹，失败会向上抛异常，被 `handleSessionRewind` 的 except 捕获记 warning 后 `rewind_context=False` 返回（`agent_ws_server.py` L1698-1701）。

**Go 问题** (`session_ops.go` L610-612):
```go
if err := contextEngine.ClearContext(ctx, ceinterface.WithSessionID(sessionID)); err != nil {
    logger.Warn(logComponent).Str("session_id", sessionID).Err(err).Msg("rewind_session_context: failed to clear context")
}
// ← 只记日志继续执行，后续构建新上下文并可能返回 true
```

行为差异：Python clear 失败 → 整个 rewind_context 返回 False；Go clear 失败 → 继续在**未清空的旧上下文**上构建新上下文 → 新旧消息混合，context 状态污染。

**修复方案**：clear 失败时 `return false`（对齐 Python 异常向上传播行为）。

---

### M-17: `buildTeamAgent` 双重 `Allocate` — round_robin/by_model_name 下 leader 分配偏移

**Python 参考** (`blueprint.py` L421-425): 只 Allocate 一次，同一 `leader_allocation` 同时用于 `to_team_model_config()` 和 `attach_model_allocator`（注释强调 "reuses the same rotation state"）。

**Go 问题** (`manager.go` L1076-1085 与 L1115-1122):
```go
leaderAlloc := modelAllocator.Allocate(spec.Leader.ModelName)   // ← 第一次
...
alloc := modelAllocator.Allocate(spec.Leader.ModelName)          // ← 第二次
ag.AttachModelAllocator(modelAllocator, leaderAlloc)
```

RoundRobin 下 index 递增两次：`TeamRuntimeContext.MemberModel` 与 attach 的 `leaderAlloc` 指向不同条目（pool[0] 与 pool[1]）；ByModelName 多条目同名组同样跳槽。下游 teammate 从被偏移的 rotation 开始分配。

**修复方案**：只 Allocate 一次，复用同一 `*Allocation` 传两处。

---

### M-18: `inspectSession` 吞掉 PreRun/DB Initialize 失败 — 调度决策基于错误输入

**Python 参考** (`manager.py` L839/L847-849): `pre_run()` 与 `db.initialize()` 失败直接异常上抛，activate 中断。

**Go 问题** (`manager.go` L833-838 与 L861-871):
```go
if preErr := teamSession.PreRun(ctx); preErr != nil {
    logger.Warn(...)   // ← 继续，session 检查点未恢复 → ReadTeamNamespace 读到空
}
...
db := spawn.GetSharedDB(dbCfgProvider)
if db != nil {
    if initErr := db.Initialize(ctx); initErr != nil {
        logger.Warn(...)   // ← teamInDB 保持 false
    }
}
```

**影响链**：① PreRun 失败 → 已有桶的团队被误判为 NEW_TEAM_IN_SESSION 而非 COLD_RECOVER（重复建队）；② DB 初始化失败/GetSharedDB 返回 nil → `teamInDB=false` → DecideRunAction 误判 REJECT_ORPHANED 或 CREATE——**DB 瞬断会把合法存在的团队拒绝为 orphaned**。

**修复方案**：db 为 nil 或 Initialize 失败时返回 error；PreRun 失败返回 error（对齐 Python 异常传播）。

---

### M-19: session 传播链在 runner→kernel 断裂 — `Invoke`/`Stream` 忽略 `WithSession` 选项 + `kernel.Start` 丢弃 `BindSession` 返回的 ctx

**Python 机制**：`sess_mgr.bind_session(session)` 在 kernel.start 内设置 contextvar，后续 recover_team→restart_teammate→spawn 链都能 `get_session_id()` 读到；`subscribe_transport` 用它构建 topic（跨 session 隔离的关键）。

**Go 三处断裂**：
1. `team_agent.go` L561/L624：`a.coordination.Start(ctx, agentteams.GetSessionID(ctx))` — opts 里的 session 被忽略；ctx 无 SessionState → 传 ""
2. `kernel.go` L189-194：`_, _ = sessCtrl.BindSession(ctx, sessionID)` — **返回的 newCtx（含注入的 SessionState）被丢弃**，后续 workspace/memory/UpdateStatus/SubscribeTransport 全部用旧 ctx
3. 调用链验证：`runAgentTeamStreaming` 的 ctx 来自 `streamCtx := context.WithCancel(ctx)`（无 SessionState）；`bindInteractTeamSession` 仅在 Interact 路径（L310）注入，主流式链路无注入

**影响链**：`SubscribeTransport`（L1294）`agentteams.GetSessionID(ctx)` 恒 "" → topic 按空 session 构建 → 跨 session 隔离失效；monitor（L360 注入真实 sessionID）与 agent（""）订阅的 topic 不一致 → **monitor 查不到团队数据**。

**修复方案**（两层都做）：(a) team_agent Invoke/Stream 从 opts 提取 session，把 SessionState 注入 ctx 传给 coordination.Start；(b) kernel.go Start 使用 BindSession 返回的 newCtx 继续后续步骤。

---

### M-20: `processTeamMessageStream` 的 waiter 注册竞窗 — goroutine 先于 `RegisterStreamTask` 启动

**Python 参考** (`team_helpers.py` L753-763): `asyncio.create_task` 创建任务后不会立即执行（事件循环调度），且 `_pending_waiters` 注册发生在 create_task 之前。

**Go 问题** (`deep_adapter_team.go` L382-385):
```go
go d.consumeStreamWithQuery(streamCtx, channelID, sessionID, teamSpec, queryText, roundID, streamEnvs)  // L382 先启动
teamManager.RegisterStreamTask(sessionID, streamCancel)                                                  // L385 后注册
```

goroutine 启动即可能被调度。若 `consumeStreamWithQuery` 在 Register 执行前推进到读取 `HasStreamTask` 的位置，会看到"无流任务"；而步骤 8 的 `for teamManager.HasStreamTask(sessionID)` 循环依赖该状态。低概率但真实存在的竞窗。另外：广播的 round-start 事件可能在 waiter 注册完成前发出，`broadcastEvent` 找不到 waiter → 首个 processing 事件丢失。

**修复方案**：调换顺序——先 `RegisterStreamTask`，再 `go consumeStreamWithQuery(...)`。

---

### M-21: `buildOpenJiuwenProviderConfig` 的 embed 配置键名与 Python 不一致 — base_url 永远落到 env 兜底

**Python 参考** (`external_memory_config.py` L114-117 + `memory/config.py` L74-86):
```python
embed_cfg = get_embed_config() or {}   # 键名是 embed_base_url / embed_api_key / embed_model
embedding = {
    "base_url": embed_cfg.get("base_url") or os.getenv("EMBED_BASE_URL", ""),
    ...
}
```

**Go 问题** (`deep_adapter_rails.go` L1613-1616):
```go
baseURL := strOr(utils.StrVal(embedCfg["embed_api_base"]), os.Getenv("EMBED_BASE_URL"))  // ← 键名 embed_api_base
```

Python 的 `get_embed_config` 读 `embed_base_url`，Go 读 `embed_api_base` —— 用户在 config.yaml 写 `embed_base_url`（Python 口径）时 Go 取不到，永远走 env 兜底。注意 `buildMemoryRailWithMode`（L799-801）倒是双键名兼容（先 embed_base_url 再 embed_api_base），本处未对齐。

**修复方案**：与 buildMemoryRailWithMode 一致的双键名兼容。

---

### M-22: `buildOpenJiuwenProviderConfig` 的 kv 默认值 "memory" vs Python "shelve" — 后端选型偏离

**Python 参考** (`external_memory_config.py` L101): `kv_backend = (oj_cfg.get("kv_type") or "shelve").lower()` — jiuwenswarm 适配层默认 **shelve**。

**Go 问题** (`deep_adapter_rails.go` L1595):
```go
// KV 配置 — 对齐 Python: _DEFAULT_KV_BACKEND = "memory"
kvBackend := strOr(utils.StrVal(ojCfg["kv_type"]), "memory")
```

注释引用的 `_DEFAULT_KV_BACKEND = "memory"` 来自 agent-core 的 `openjiuwen_memory_provider.py`（provider 内部兜底，另一套源），而 jiuwenswarm 适配层（本移植直接参考）默认 shelve。用户未配 kv_type 时：Python 用 shelve（文件持久化，重启不丢），Go 用 memory（进程内，重启丢失）。

**修复方案**：默认值改 "shelve" 对齐 jiuwenswarm；或在注释明确"刻意取 provider 内部默认"并经团队确认。

---

### M-23: `CopySessionState` 的 `plan_slug` 更新被包进 copyFile 成功分支 — 旧 plan 文件缺失时目标保留旧 slug

**Python 参考** (`session_ops_service.py` L582-599):
```python
if old_plan_path.exists():
    shutil.copy2(old_plan_path, new_plan_path)
plan_mode["plan_slug"] = new_slug          # ← 无条件更新
modified_state["plan_mode"] = plan_mode
```

**Go 问题** (`session_ops.go` L724-733): `planMode["plan_slug"] = newSlug` 包在 `copyFile err == nil` 分支内。

**影响链**：源会话 plan 文件已被删除（plan 完成后清理是常见场景）→ copyFile/Stat 失败 → 目标会话 state 保留**旧 slug**（指向源会话的 plan 文件路径）→ 分支会话读写 plan 时操作到源会话的文件。

**修复方案**：slug 更新移出 copy 成功分支（copy 失败仅记 Warn 不阻断），对齐 Python。

---

### M-24: `deepCopyMap` 失败返回空 map 后照写目标 state — JSON round-trip 失败时目标会话 state 被清空

**Python 参考**: `copy.deepcopy` 对 dict 不会失败；整体在 try 块内，异常时 return False（不写目标）。

**Go 问题** (`session_ops.go` L881-890 + L710-713): `deepCopyMap` 失败返回 `make(map[string]any)`，调用处在其上设置 3 个键后**直接写入目标 checkpointer**。

**影响链**：源 state 含 JSON 不可序列化值（NaN float/channel/func）→ Unmarshal 失败 → 空 map → 目标会话 state 被写成只有 3 个键的空壳，源会话全部业务状态丢失。

**修复方案**：deepCopyMap 改为 `(map, bool)` 签名，失败时调用方 return false。

---

### M-25: Dreaming legacy checkpoint（list 格式）反序列化失败 — 静默全量重扫

**Python 参考** (`sweeper.py` L114-121): `raw` 为 list（旧格式）→ `{sid: {} for sid in raw}`；为 dict → 直接用。双格式兼容。

**Go 问题** (`sweeper.go` L90-95 + L979-984): `CheckpointData.ScannedSessions` 是 `map[string]ScannedSession`，Python 旧版 checkpoint（`scanned_sessions: ["sid1", ...]`）Unmarshal 失败 → loadCheckpoint 返回空 → Init 重置 → **所有 session 重新扫描**。重复 promote 靠标题/hash 去重兜底，但 DREAMING.md 50 条上限的淘汰逻辑可能被重复写入扰动。

**修复方案**：loadCheckpoint 双格式兼容（先 map 格式，失败再尝试 `[]string` 转空 ScannedSession 映射）。

---

### M-26: `UpdateIndex` 的 DeleteIndex 失败直接终止 — Python 语义是吞掉继续 rebuild

**Python 参考** (`chroma_indexer.py` L198-215): `delete_index` 内部吞错（返回 False 不抛）→ update 的"先删后建"中删除失败**不阻断** rebuild；外层 catch-all 记 "Failed to update index" 并 return False（永不抛异常）。

**Go 问题** (`indexer/chroma.go` L283-292): `DeleteIndex` 失败直接 `return false, err` 终止 UpdateIndex；且无 update 级错误日志。

**修复方案**：需团队决策——(a) 严格对齐：delete 失败仅记日志继续 build；(b) 保留 Go error 传播但补齐 update 级日志并注释声明有意偏离。建议 (b)。

---

### M-27: `ChromaIndexer` 构造函数丢弃 `config.DatabaseName` — 多数据库能力丢失（与历史 S-50 联动）

**Python 参考** (`chroma_indexer.py` L66/L90-93): `self.database_name = config.database_name`，client 绑定该库；delete/exists/info 都用它。

**Go 问题** (`indexer/chroma.go` L27-48): 结构体无 `databaseName` 字段，`config.DatabaseName` 完全被丢弃；各方法构造 store 时 `VectorStoreConfig.DatabaseName` 也为空。叠加底层 `NewChromaVectorStore` 不支持 databaseName（历史 S-50），多数据库能力双倍丢失。

**修复方案**：增加字段并在各方法传入；配合 S-50 落实 `WithDatabaseAndTenant`/AdminClient 建库。

---

### M-28: `BuildIndex` 缺少 Python 外层异常包装与错误日志 — 错误裸透传

**Python 参考** (`chroma_indexer.py` L188-196): 外层 except 记 `logger.error(f"Failed to build index: {e}")` 并包装为 `ADD_DOC_RUNTIME_ERROR`（含 cause 链）。

**Go 问题** (`indexer/chroma.go` L148-269): `NewChromaVectorStore`/`ComputeChunkEmbeddings`/`Add` 错误原样透传——无包装、无 cause、无 build 级 Error 日志（违反规则 3.4）。store 创建/Add 失败完全无日志。

**修复方案**：外层对非 BuildError 错误统一包装 + 补 Error 日志（event_type=INDEXING_ERROR）。

---

### M-29: 能力检测分叉 — Go `OpenAIEmbedding` 实现了 `EmbedMultimodal`，Python 版没有

**Python 参考** (`openai_embedding.py`): 无 `embed_multimodal` → OpenAI 模型走**纯文本批量路径**（所有 chunks 一起 embed_documents，带回调）。

**Go 问题** (`retrieval/embedding/openai.go` L224-257): `OpenAIEmbedding.EmbedMultimodal` 存在（内部文本回退）→ 满足 `MultimodalEmbedder` 接口 → OpenAI + `use_caption_for_images=false` + 图片分块时走**多模态路径**：逐个 `EmbedQuery`（无回调、无批量）。

**影响**：① 图片分块不触发 DocIndexCallback（进度漏计）；② 逐条 API 调用损失批量性能；③ 行为与 Python 分叉。

**修复方案**：`OpenAIEmbedding` 删除 `EmbedMultimodal`（或独立 capability 标记），恢复接口断言语义。

---

### M-30: ChromaVectorStore 搜索结果未反序列化 `sparse_vector` 元数据 — 消费方拿到 JSON 字符串

**Python 参考** (`chroma_store.py` L467-473): 读出时 `json.loads` 还原稀疏向量，失败 Warn。

**Go 问题** (`vector_store/chroma.go` L641-655): `chromaResultToSearchResults` 完全没有处理 sparseVectorField——写入时 `addBatch` 把稀疏向量 `json.Marshal` 成字符串（L551-556），读出时原样返回 JSON 字符串。

**修复方案**：元数据循环后补 `json.Unmarshal` + 失败 Warn。

---

### M-31: `GetIndexInfo` count 失败静默置 0 — 无法区分空集合与查询失败

**Python 参考** (`chroma_indexer.py` L284-300): count 异常 → `return {"exists": False, "error": str(e)}`。

**Go 问题** (`indexer/chroma.go` L423-426): `count, err := collection.Count(ctx); if err != nil { count = 0 }` — 静默吞掉，仍返回 exists=true。

**修复方案**：count 失败时返回 `{"exists": false, "error": err.Error()}`。

---

### M-32: `restore_session_files` 的 MkdirAll 失败后未 continue — 双错误条目 + Python 语义偏离

**Python 参考** (`session_ops_service.py` L337-351): write 分支整体包 try——mkdir 失败异常直接跳 except（一个文件最多一个 error 条目，不会再执行 write）。

**Go 问题** (`session_ops.go` L468-477): MkdirAll 失败记录 error 后**无 continue**，仍执行 WriteFile → 大概率再失败 → 同一文件产生双错误条目。另 `RestoreContent == nil` 时 Go 静默跳过，Python KeyError 记 error（行为不对称）。

**修复方案**：MkdirAll 失败分支末尾加 `continue`。

---

## 提示问题 (T)

### T-01: `utilsParseStreamChunk` 为 identity return，建议补充 Go 差异说明

**Go 代码** (`deep_adapter_team.go` L1192-1199): Python 的 `parse_stream_chunk` 有真实解析逻辑，Go 端因 chunk 已是 map 直接返回。注释已说明，但建议标注 Python 源码行号。

---

### T-02: `eventBroadcastInitialized` 全局标志无重置机制

**Go 代码** (`team_helpers.go` L62): 一旦 true 永不重置，TeamManager 被替换后（测试场景）广播回调不会重新注入。建议提供 `ResetEventBroadcastInjection()` 测试辅助函数。

---

### T-03: `teamHelperStrFromAny` 仅一处使用，可内联

---

### T-04: `SyncTeamIdentityMetadata` 中 metadata 读取顺序与 Python 相反

Python 先读 metadata 再判断 activation_kind；Go 先判断 kind 再读 metadata。**功能等价**（kind 不匹配时两者都不更新），Go 版本避免了无用读取，属合理优化。建议添加注释说明差异原因。

---

### T-05: ChromaIndexer 的 `docIndexCallback` 建议改为接口

当前为函数类型别名，接口类型（`OnProgress(docID string, index, total int)`）更利于多态扩展。

---

### T-06: `getUniqueForkName` 存在冗余双重解析

**Go 代码** (`session_ops.go` L821-825):
```go
if num, err := fmt.Sscanf(m[1], "%d", new(int)); err == nil && num == 1 {
    var n int
    fmt.Sscanf(m[1], "%d", &n)   // ← 第二次解析冗余
    usedNumbers[n] = true
}
```
注意：`fmt.Sscanf` 第一个返回值是**成功解析的项目数**（不是解析值），所以 `num == 1` 是判断成功，逻辑正确但晦涩。简化为：
```go
var n int
if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil {
    usedNumbers[n] = true
}
```
（正则 `(\d+)` 已保证为数字，甚至可直接 `strconv.Atoi(m[1])`）

---

### T-07: Dreaming sweeper 的 `buildCompressedSessionText` 缺少 `max_rounds` 参数

**Python**: `build_compressed_session_text(session_id, max_rounds=10)`；Go 端无轮次上限，长会话可能生成超大压缩输入。

---

### T-08: `teamEvolutionIdleSleepSec` 常量命名与值单位矛盾

**Go 代码** (`deep_adapter_team.go` L29):
```go
teamEvolutionIdleSleepSec = 500 // 毫秒   ← 名字是 Sec，值是毫秒
```
使用处 `time.Duration(teamEvolutionIdleSleepSec) * time.Millisecond` 正确，但命名应改为 `teamEvolutionIdleSleepMs`。

---

### T-09: `ForkSession` 的 `source_mode` 缺少默认值

Python: `source_meta.get("mode", "code.normal")`；Go 端缺失时为空串。建议：
```go
sourceMode, _ := sourceMeta["mode"].(string)
if sourceMode == "" {
    sourceMode = "code.normal"
}
```

---

### T-10: `ListSessionTurns` 的 total 统计与 turns 列表口径差异

**Go** (`session_ops.go` L438): `Total: userCount` 统计所有 user 消息（含不可选的），`turns` 只含可选的——与 Python 一致（L271-299），无需修改，但建议加注释说明两者口径不同，避免后续维护误解。

---

### T-11: `consumeStreamWithQuery` 中 `_ = streamTraceEnabled` 掩盖未使用变量

与 M-01 关联：TeamStreamLogger 接入后此行应删除。

---

### T-12: `ListSessionTurns` 空结果返回 `Turns: nil` — 序列化为 `null` 而非 `[]`

**Python 参考**: `return {"turns": [], "total": 0}` — JSON 输出 `"turns": []`。

**Go 问题** (`session_ops.go` L354/L360/L365): `&ListSessionTurnsResult{Turns: nil, Total: 0}` — 序列化为 `"turns":null`。前端拿到 null vs [] 语义不同（遍历 null 报错）。

**修复方案**：初始化 `Turns: []SessionTurnInfo{}`。

---

### T-13: `copyFile` 注释声称对齐 `shutil.copy2` 但未复制 mtime

**Go 代码** (`session_ops.go` L855-879): 只复制内容和权限，未调 `os.Chtimes`。shutil.copy2 = 内容+权限+mtime/atime。影响极低（history 文件的 mtime 无业务消费方），但注释名不副实。补 `os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())` 或修改注释为对齐 copy 语义。

---

### T-14: Dreaming `StartDreaming` 签名比 Python 多 language 参数（行为增强，记录在案）

**Python**: `start_dreaming(sessions_dir, output_dir, mode, busy_checker)` — Sweeper 构造时 language 硬编码默认 "zh"。

**Go** (`api.go` L41): `StartDreaming(ctx, sessionsDir, outputDir, mode, language, busyChecker)` — language 由调用方传入，`deep_adapter_dreaming.go` L71-76 把运行时语言映射为 zh/en。属**有意增强**（语言跟随运行时而非硬编码），非逻辑丢失。建议：`sweeper_test.go` 补充 language="en" 分支的压缩截断文案用例。

---

### T-15: `extractContentStr` 只收 `type=="text"` 的 part — Python 版有 dict 兜底

**Python 参考** (`sweeper.py` L421-433): dict 项取 `item.get("text", "")` 不检查 type；非 str 非 list 时 `return str(content) if content else ""`。

**Go 问题** (`sweeper.go` L867-882): `if p.Type == "text"` 过滤——LLM 返回无 type 字段的 dict 型 content 时 Go 提取为空 → JSON 解析失败 → 空结果。低危（主流模型都返回 type 字段）。

---

### T-16: RunSweep 的 LLM 失败计费与 Python 不同 — Go 更精确（有意差异，建议注释声明）

**Python** (`sweeper.py` L414-419): `_extract_via_llm` 从不抛出——LLM 调用失败也返回 `[]`（session 记为成功并写入 checkpoint），存在 checkpoint 污染 bug（失败 session 永不重试）。

**Go** (`sweeper.go` L858-861): LLM 调用失败 → `return error` → failedIDs（可重试）；JSON 解析失败 → `(nil, nil)` → succeededIDs（与 Python 一致）。

Go 比 Python 更精确，属"移植时修复上游 bug"。若项目原则是一比一复刻，需确认此差异是有意的；建议在代码注释中声明"有意偏离 Python：LLM 失败进 failedIDs 以便重试"。

---

### T-17: `Finalize` 中 `IsShutdownRequested` 出错时行为与 Python 相反

**Python** (`manager.py` L203-229): 异常被外层 except 捕获 → **什么都不做**（不 stop 不 pause，条目保持原状）。

**Go 问题** (`manager.go` L290-295): err 后 `shutdownRequested=false` → 继续走 pause 分支，把 RUNNING 团队错误暂停。

**修复方案**：err != nil 时直接 return（保持条目原状）。

---

### T-18: `DecideRunAction` 契约违反用 panic — 无 recover 防护，进程崩溃风险

**Python** (`dispatch.py` L140-144): `raise RuntimeError`（可捕获）。**Go** (`dispatch.go` L129): `panic` —— Activate 守卫使正常路径不可达，但导出函数可直接调用；全包仅 FinalizeMember 一处 recover，此处无防护。建议改返回 error 或补 recover。

同类提示（9.85 runner 层，记录在案）：
- `RunBaseTeam` L138-148 把 invoke 结果强转 `map[string]any`，非 map 报错（Python 原样返回）
- runner 包缺 `RegisterHumanAgentInbound` facade（Python Runner 类门面有，Go 仅 TeamRuntimeManager 方法）
- `buildTeamRuntimeReadyChunk` 的 `source_member`/`role` 恒传非 nil，Python 可为 None（blueprint 未配置时），下游无法区分未设置
- `closeTeamInteractGate` 用 `context.Background()` 丢调用方 ctx（`CloseAndDrain` 有 ctx 能力未利用）
- 流式 goroutine（team_runner.go L491-493）用可取消外层 ctx 执行 Finalize/PostRun——调用方断开后清理被取消（Python finally 在服务器任务组中不受客户端断开影响）
- `FinalizeMember` 中 `Stop/PauseCoordination` 错误 `_ =` 静默吞掉（Python 外层 except 至少 warning）
- `ResolveTeamAgentSpec` 返回普通 `fmt.Errorf`，Python 是 `AGENT_TEAM_CONFIG_INVALID` 结构化错误码（上层无法映射 HTTP 状态）

---

### T-19: `teamSpecTeamName` 的两个接口分支均无实现者 — 死代码

**Go 代码** (`deep_adapter_team.go` L1143-1169): `GetTeamName()` 与 `TeamName()` 两个接口断言分支，全项目 grep 无任何类型实现这两个方法（`TeamAgentSpec` 有 `TeamName` **字段**而非方法）。唯一有效分支是 `map[string]any` 回退，但 L678 传入的 teamSpec 是 `*TeamAgentSpec` 结构体 → 三个分支全部落空 → 恒返回 ""。

**影响**：`consumeStreamWithQuery` 的 runtime_ready 处理中 `specTeamName` 恒空（需核对下游是否用 team_name 查询 metadata，若是则查空）。

**修复方案**：直接断言 `*atschema.TeamAgentSpec` 取 `.TeamName` 字段，删除两个无效接口分支。

---

### T-20: `ready_chunk` 的 `source_member` 取 `AgentCard().Name`（DisplayName）而非 Python 的 `blueprint.member_name`

**Python 参考**: `blueprint.member_name`（成员标识符，如 "leader"）。

**Go 问题** (`team_runner.go` L453-455): `leaderMemberName = bp.Name` — AgentCard.Name 是显示名（DisplayName），与 MemberName 是两个概念。前端按 source_member 归属成员时标签错值。

**修复方案**：从 activation.Agent 的 blueprint/configurator 取 `MemberName()`（TeamAgent 已有该方法，team_agent.go L540 Invoke 内就有调用）。

---

### T-21: `RunBaseTeam/RunTeamMember/runAgentTeam` 清理未用 defer — panic 时 Finalize 被跳过

**Go 代码** (`team_runner.go` L414-418 等): 清理三连（Finalize/closeGate/PostRun）是顺序语句而非 defer。中间某步 panic → 清理跳过 → 池条目滞留 RUNNING 状态（后续 Activate 判 PAUSED/RUNNING 决策错误）。

**修复方案**：改为 `defer func() { ... }()`（注意 defer 的执行顺序与 Python finally 等价性）。

---

### T-22: `duplicate_doc_ids` 错误信息顺序不确定

**Python**: `sorted({chunk.doc_id ...})` 输出有序。**Go** (`indexer/chroma.go` L188): 遍历 map → duplicateDocIDs 顺序随机 → 相同错误场景错误信息不同（影响日志比对/测试断言）。

**修复方案**：`sort.Strings(duplicateDocIDs)` 后再拼接。

---

### T-23: `RRFFusion` 排序非稳定 — 并列分数时输出顺序不确定

**Python**: `sorted(..., reverse=True)` 稳定排序（并列保持首次出现顺序）。**Go** (`retrieval/utils/fusion.go` L58): `sort.Slice` 非稳定。

**修复方案**：改 `sort.SliceStable`。

---

### T-24: HybridSearch ID 最终兜底缺失

**Python** (`chroma_store.py` L396): `result_id = rr.metadata.get("id") or id_mapping.get(rr.text, str(hash(rr.text)))` — 最后兜底 `str(hash(text))`。**Go** (`vector_store/chroma.go` L408-411): idMapping 未命中时 resultID 保持 ""。

**修复方案**：补 fnv hash 字符串化兜底。

---

### T-25: `addBatch` 元数据 JSON 解析失败静默 — Python 有 warning

**Python** (`chroma_store.py` L211-215): 解析失败 `logger.warning(f"Failed to load metadata: ...")`。**Go** (`vector_store/chroma.go` L534-541): `err == nil` 静默忽略。

**修复方案**：补 Warn 日志。

---

### T-26: `BuildIndex` 传完整 ChromaVectorField，Python 只传字段名 — HNSW 参数生效范围不一致

**Python** (`chroma_indexer.py` L135): `vector_field=self.vector_field.vector_field`（仅名字）→ 建集合用默认 HNSW（16/100/100），indexer 的自定义 HNSW 参数在 build 路径不生效。**Go** (`indexer/chroma.go` L167-171): 传完整对象 → 自定义参数生效。Go 行为更"合理"但与 Python 不一致。

**修复方案**：需决策——对齐 Python（传 `NewChromaVectorFieldFromName(...)`）或记录偏差（建议记录，Go 行为是修正性的）。

---

### T-27: `ComputeChunkEmbeddings` 空 chunks 早退语义偏差

**Python**: 非多模态路径空 chunks 经 embed_documents 校验抛错（"Empty texts list"）；多模态路径静默返回。**Go** (`embed_chunks.go` L99-101): 统一早退返回 nil → 纯文本模型 + 空 chunks + vector/hybrid 时 BuildIndex 成功写 0 条（Python 抛错）。

**修复方案**：对齐或在注释记录（边缘场景）。

---

### T-28: indexer 五个核心方法零测试 + `MultiKBRetrievalResult` 类型缺失

(a) `indexer/chroma_test.go` 仅 9 个构造函数用例，`BuildIndex/UpdateIndex/DeleteIndex/IndexExists/GetIndexInfo` **零测试**。`vector_store/chroma_test.go` 已证明可用 `t.TempDir()` + embedded client 跑真实流程，同方式可为 indexer 补集成级单测（含去重/先删后建/空文本图片分块回归）。

(b) Python `retrieval_result.py` 定义了 `MultiKBRetrievalResult`（text/score/raw_score/raw_score_scaled/kb_ids/metadata），Go `retrieval/common/result.go` 未实现——后续多知识库检索章节需要，建议在计划中标注回填。

---

## 附录A：按章节汇总

### agent_teams 占位桩核查（9.19-9.68 / 9.85 关联）

| 编号 | 严重度 | 问题 |
|------|--------|------|
| S-10 | 严重 | TeamHarness.RunStreaming 空流 — 团队输出静默丢失 |
| S-11 | 严重 | 审批/规划 Rail 已实现但 configurator 永不构造 |
| S-12 | 严重 | TeamMemoryManager 全桩 — 记忆功能整体失效 |
| S-13 | 严重 | TeamMember.Status 恒 Ready — SHUTDOWN 流控失效 |
| S-14 | 严重 | harness_config.yaml 声明式加载 tools/rails 必报错 |
| S-15 | 严重 | LoadHarnessConfig 返回"尚未实现" — 配置热更新报错 |
| S-16 | 严重 | resolveMemberModelFromDB 返回 nil — 恢复成员无模型 |
| S-17 | 严重 | TeamHarness 9 方法桩（Steer/Abort 等静默吞掉） |

**有意延后（计划 ☐/🔄 与代码 ⤵️ 一致，无需处理）**：扩展系统 loader/manager/crypto（10.5.7/8/10 ☐）、Browser Playwright 端到端 6 处（9.38-49 已列明）、A2X/Cron 7 方法（11.10 未到）、Swarm 内置工具 5 处（10.6.24 ☐）、AgentServer 5 stub（未到章节）、Graph/Workflow 域（8.x ☐）、Runner 分布式预留、检索域 MilvusIndexer/Parser/Chunker（13.3/13.5-13.18 ☐，文件尚不存在）。

**候选升级项（标 TODO(#9.63) 但 backend 已就绪）**：coordination/handlers 下 message.go（processUnreadMessagesWithSteer/ackUserBoundMessage/notifyHumanAgentInbound）、task_board.go（onTaskPlanDecision/nudgeIdleAgent/formatTaskAssignedToSelf）、team_completion.go（注释掉的框架代码可直接启用）、stale_task.go、member.go 共 10+ 处 handler 未接已实现的 TeamBackend/DAO——建议归入修复批次。

### 9.85 — 团队 Runner 三路径 + Activate 内联回填 + DecideRunAction

**adapter 层（deep_adapter_team.go）**：

| 编号 | 严重度 | 问题 |
|------|--------|------|
| S-03 | 严重 | evolution push 未实现，仅日志（TODO #9.85） |
| S-04 | 严重 | 缺少 wait_for_pending_shutdown_cleanup（Go 端无任何实现） |
| S-05 | 严重 | consumeStreamWithQuery 缺少 ctx 取消处理 |
| S-06 | 严重 | consumeStreamWithQuery 缺少 panic 恢复和 team.error 广播 |
| S-08 | 严重 | 审批结果推送 pushTeamSkillEvolveResolutionStatus 未实现 |
| S-18 | 严重 | evolutionOutputSchemasToMaps 类型断言必然失败 — 演进事件全量丢弃（已验证） |
| S-20 | 严重 | chunkToMap 对 *TeamOutputSchema 恒 nil — 团队流整体丢弃（已实测验证） |
| S-21 | 严重 | teamHelperEventAsMap 恒 nil — /evolve 审批弹框必然缺失（已实测验证） |
| S-22 | 严重 | ResolveTeamRebuildFollowup 在 isFirstRequest 块内 — 后续请求 /evolve_rebuild 失效 |
| S-23 | 严重 | DrainPendingApprovalEvents(true, nil) — watcher 循环可阻塞 720s |
| M-01 | 一般 | TeamStreamLogger 未接线（Go 端监控模块已有实现） |
| M-02 | 一般 | RunAgentTeamStreaming 缺少 envs 传递 |
| M-05 | 一般 | PopTeamEvolutionWatcher 在 panic 时不执行 |
| M-06 | 一般 | ensureMonitor 缺少 is_running 短路检查 |
| M-07 | 一般 | consumeMonitorEvents 缺少 panic 恢复 |
| M-08 | 一般 | 后续请求错误消息丢弃 err 详情 |
| M-09 | 一般 | AttachDistributedHooks 传 nil 丢 channelID |
| M-10 | 一般 | team.completed 重建事件丢失原始字段 |
| M-20 | 一般 | waiter 注册竞窗 — goroutine 先于 RegisterStreamTask 启动 |
| T-01/02/03/08/11 | 提示 | 杂项 |
| T-19 | 提示 | teamSpecTeamName 两接口分支无实现者 — 死代码 |

**runner 内层（team_runner/manager/child/kernel，第二轮复核）**：

| 编号 | 严重度 | 问题 |
|------|--------|------|
| S-24 | 严重 | leader 路径 inputs 硬编码 nil — 用户 query 丢失（历次 P0 第 4 次记录） |
| S-25 | 严重 | 流式拒绝路径返回 nil channel — range 永久阻塞 + 跳过 finally 三步清理 |
| S-26 | 严重 | RecoverFromSession 类型断言恒失败 — COLD_RECOVER 100% 报错（已实测验证） |
| S-27 | 严重 | 子进程 TEAM_AGENT 分支 member=false — spawned teammate 侵占 leader pool |
| M-17 | 一般 | buildTeamAgent 双重 Allocate — leader 分配偏移 |
| M-18 | 一般 | inspectSession 吞 PreRun/DB Initialize 失败 — 调度决策基于错误输入 |
| M-19 | 一般 | session 传播链断裂 — kernel 丢弃 BindSession ctx，topic 按 "" 构建 |
| T-17/T-18 | 提示 | Finalize err 行为反转 / DecideRunAction panic 无 recover |
| T-20/T-21 | 提示 | ready_chunk source_member 取 DisplayName / 清理未用 defer |

**复核确认已对齐（防误报）**：DecideRunAction 真值表（pending_create/cleaned 前置 + REJECT_INCONSISTENT + 冷路径 + session 契约检查）、Activate 6 步、applyAction 四路径（RESUME/COLD/NEW_TEAM/CREATE 步骤顺序）、Finalize/FinalizeMember 决策规则、gate 状态机（Reset double-close 防护）、resolveEntry、ResolveTeamSessionReleaseInfo（parse_errors 累积）。10-11 审查的 S-50（maybeReleaseTeamSession）、S-51 部分（Pause/Stop/Delete 门面）、S-29（createAgentTeamSession AgentSession 分支）均已修复。

### 10.3.8 — TeamHelpers 辅助函数

| 编号 | 严重度 | 问题 |
|------|--------|------|
| S-01 | 严重 | /evolve_list 表格缺少 Used/Effect 列 |
| M-03 | 一般 | /evolve 缺少字符串结果分支 |
| M-04 | 一般 | /evolve 的 items 传 nil 丢失 Records |
| T-04 | 提示 | SyncTeamIdentityMetadata 顺序差异（功能等价） |

### 13.2 — ChromaIndexer

| 编号 | 严重度 | 问题 |
|------|--------|------|
| S-09 | 严重 | BuildIndex 未 Close store（泄漏）+ 每次操作新建 store |
| S-29 | 严重 | 查重阶段 collection.Get 错误被静默吞掉 — 重复 doc_id 保护失效 |
| M-26 | 一般 | UpdateIndex 的 DeleteIndex 失败直接终止（Python 吞掉继续 rebuild） |
| M-27 | 一般 | 构造函数丢弃 config.DatabaseName |
| M-28 | 一般 | BuildIndex 缺少外层异常包装与错误日志 |
| M-30 | 一般 | 搜索结果未反序列化 sparse_vector 元数据 |
| M-31 | 一般 | GetIndexInfo count 失败静默置 0 |
| T-05 | 提示 | docIndexCallback 建议接口化 |
| T-22/T-23/T-24/T-25/T-26 | 提示 | duplicate 顺序 / fusion 稳定性 / HybridSearch 兜底 / addBatch 静默 / HNSW 参数范围 |
| T-28 | 提示 | indexer 五方法零测试 + MultiKBRetrievalResult 缺失 |

### 13.4 — ComputeChunkEmbeddings

| 编号 | 严重度 | 问题 |
|------|--------|------|
| S-28 | 严重 | 空文本图片分块必然报错 — Python 允许 text=""，Go loadMultimodalData 拒绝 |
| M-12 | 一般 | 跳过已有嵌入逻辑待验证 |
| M-29 | 一般 | 能力检测分叉 — Go OpenAIEmbedding 实现了 EmbedMultimodal |
| T-27 | 提示 | 空 chunks 早退语义偏差 |

### 10.6.12 — SessionOps

| 编号 | 严重度 | 问题 |
|------|--------|------|
| S-19 | 严重 | RewindSession/ListSessionTurns 80 字节预览截断（CJK 乱码） |
| M-13 | 一般 | ForkSession 元数据 nil→null 序列化差异（3 个字段） |
| M-14 | 一般 | forked_from 三层失败分支静默吞错 |
| M-16 | 一般 | RewindSessionContext clear 失败仍返回 true |
| M-23 | 一般 | CopySessionState 的 plan_slug 更新包进 copyFile 成功分支 |
| M-24 | 一般 | deepCopyMap 失败后目标 state 被清空 |
| M-32 | 一般 | restore_session_files 的 MkdirAll 失败后未 continue |
| T-06 | 提示 | getUniqueForkName 冗余双重解析（Sscanf 返回值误用） |
| T-09 | 提示 | source_mode 默认值缺失 |
| T-10 | 提示 | total 与 turns 口径差异注释 |
| T-12 | 提示 | ListSessionTurns 空结果 null vs [] |
| T-13 | 提示 | copyFile 注释与 shutil.copy2 语义不符 |

### 10.6.13-18 — Swarm Memory

| 编号 | 严重度 | 问题 |
|------|--------|------|
| S-02 | 严重 | evolution timeout 未从 rail 配置读取 |
| S-07 | 严重 | ~~6 模块缺失~~ → 修正：manager/embeddings/internal/types 已由 lite 包覆盖，缺 plugin provider + RPC 面 |
| S-19 | 严重 | dreaming sweeper 四处压缩截断按字节（CJK 放大 3 倍） |
| M-15 | 一般 | getMemoryEngine 缺 _VALID_ENGINES 白名单 — 配置拼错记忆全禁用 |
| M-21 | 一般 | embed 配置键名 embed_api_base vs Python embed_base_url |
| M-22 | 一般 | kv 默认值 memory vs Python shelve |
| M-25 | 一般 | Dreaming legacy checkpoint 反序列化失败静默全量重扫 |
| T-07 | 提示 | buildCompressedSessionText 缺 max_rounds |
| T-14 | 提示 | StartDreaming 多 language 参数（有意增强，记录在案） |
| T-15 | 提示 | extractContentStr 只收 type=="text"（Python 有 dict 兜底） |
| T-16 | 提示 | RunSweep LLM 失败计费 Go 更精确（有意差异，建议注释声明） |

---

## 附录B：修复优先级建议

**P0（生产链路断裂，建议立即修复）**：
1. **S-24**（runner leader 路径 inputs=nil——团队首请求用户 query 完全丢失，历次 P0 第 4 次记录，**修复仅 3 行**，且 S-20 chunkToMap 修复后此项成为团队流唯一阻断点）
2. **S-25**（流式拒绝路径 nil channel——range 永久阻塞 goroutine 泄漏 + 跳过 finally 三步清理，修复约 8 行）
3. **S-10 + S-17**（TeamHarness.RunStreaming 空流——团队输出/转向/中止全部静默失效，一个文件修复）
4. **S-20**（chunkToMap 对 *TeamOutputSchema 恒 nil——团队流在 adapter 层整体丢弃，与 S-24 构成双重断点，**修这两项团队流才通**）
5. **S-11**（审批/规划 Rail 接线——Rail 已实现，仅缺 configurator 构造逻辑，改动小收益大）
6. **S-13**（TeamMember 状态接 DB——DAO 已就绪，仅接线）
7. **S-16**（成员模型 DB 恢复返回 nil——模型池已实现，仅接线）

**P1（演进闭环 + 数据完整性）**：
8. **S-26**（RecoverFromSession 类型断言——COLD_RECOVER 冷恢复 100% 报错，已实测验证，按 resolveSpecFromSessionBucket 模式改写）
9. **S-27**（子进程 TEAM_AGENT member=false——spawned teammate 侵占 leader pool）
10. **S-19**（CJK 字节截断——6 处统一改 rune 口径，中文用户核心场景，改动小收益大）
11. **S-18 + S-21**（evolutionOutputSchemasToMaps / teamHelperEventAsMap 类型断言——同一模式两处，演进事件+审批弹框断链）
12. **S-28**（13.4 空文本图片分块——多模态索引在无 caption 配置下必然失败，改 loadMultimodalData 一处即可）
13. **S-29**（13.2 查重 Get 错误吞掉——数据不变量破坏 + 无日志不可追溯，修复约 5 行）
14. S-03 + S-08（gateway_push 集成，一个改动修复两处）
15. S-05 + S-06 + S-22 + S-23（consumeStreamWithQuery/stream 消费链健壮性，同一函数簇）
16. S-01（/evolve_list 列补全）
17. S-04（shutdown cleanup 等待）
18. M-15（engine 白名单——防配置拼错静默禁用，改动约 10 行）
19. M-17/M-18/M-19（runner 内层状态一致性：双重 Allocate/inspectSession 吞错/session 传播链）
20. M-20（waiter 注册竞窗——调换两行顺序即可）
21. M-04（Records 传递）
22. S-09（BuildIndex Close 泄漏——最小修复仅加一行 defer）
23. S-12（TeamMemoryManager——工作量大，建议独立排期）

**P2（功能对齐）**：
24. S-14 + S-15（harness_config YAML 加载链，两问题同根因）
25. S-02（timeout 配置读取）
26. M-02（envs 传递）
27. M-03/M-05/M-06/M-07/M-08/M-09/M-13/M-14/M-16
28. M-21/M-22（embed 键名/kv 默认值——配置兼容性，双键名兼容约 10 行）
29. M-23/M-24/M-25/M-32（SessionOps/Dreaming 状态一致性）
30. M-26/M-27/M-28/M-30/M-31（ChromaIndexer 语义对齐）
31. M-29（能力检测分叉——需决策记录）
32. handlers #9.63 接 TeamBackend（10+ 处，backend 已就绪）

**P3（大块工作，单独排期）**：
33. S-07 修正范围（plugin provider 插件发现 + RPC 面；同时同步 IMPLEMENTATION_PLAN.md 的 Embeddings 状态标记）
34. S-06 补充：mobile_gui_agent 工厂（9.31 计划 ☐，有意延后，测试已断言 stub 错误）
35. T-28（indexer 五方法测试补齐——vector_store 测试模式可复用）

---

## 附录C：审查方法说明

1. **提交记录**：`git log --since="14 days ago"`（48h 内无提交，扩大窗口至最近一轮功能实现）
2. **章节核对**：IMPLEMENTATION_PLAN.md 中 9.85/10.3.8/13.2/13.4/10.6.12/10.6.13-18 的 ✅/🔄 状态与产出说明
3. **代码对比**：Go 实现 vs Python 参考逐方法对比（方法签名、步骤顺序、分支完整性）
4. **模块归属核查**：对 Swarm Memory 章节，额外验证了 Python 两个参考源（`jiuwenswarm/agents/harness/common/memory/` 与 `openjiuwen/core/memory/lite/`）的同源性（方法集 37/40 共有），确认 Go 端 lite 包的跨项目覆盖关系，避免误判"模块缺失"
5. **Python 侧行为验证**：对可疑差异回读 Python 源码验证实际行为（如 `get_config()` 无缓存、`get_default_models` 逐次调用、`_VALID_ENGINES` 白名单），防止把 Python 自身的瑕疵当成 Go 移植基准
6. **占位桩全量扫描**：⤵️/TODO/FIXME/panic("not implemented")/nil,nil 空实现/`//go:build ignore` 全仓搜索（约 130+ 处标记，逐一核对计划条目）
7. **验证排除误报**：编译验证（go build）、调用链追踪（RunStreaming/Interact/Rail 构造点）、类型定义核查（DocumentID/UsageStats）、接口实现者全项目 grep（chunkToMap/teamHelperEventAsMap/teamSpecTeamName 三处恒失败断言均以"无实现者"实证）
