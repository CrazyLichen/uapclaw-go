# 10.3.8 TeamHelpers 辅助函数设计

## 1. 概述

10.3.8 TeamHelpers 是 team 模式的完整运行时胶水层，对应 Python `jiuwenswarm/server/runtime/agent_adapter/team_helpers.py`（1393 行）。

本文档仅覆盖**辅助函数**的实现——即不依赖 TeamRunner（9.85）和 Swarm Team（10.6.19-23）大块的部分。依赖大块的主流程方法在 `deep_adapter_team.go` 中保留 ⤵️ 标记。

### 1.1 在 Agent 会话中的流程位置

```
DeepAdapter.processMessageStreamImpl
  ├─ 步骤 1-6: 前置处理（模式判断、请求构建等）
  ├─ 步骤 7: team 模式分流 → processTeamMessageStream  ← TeamHelpers 在这里
  │   ├─ query directives 提取（/hide_dm, /debug）
  │   ├─ team slash 命令处理（/evolve, /evolve_list, /evolve_simplify, /evolve_rebuild）
  │   ├─ 首次请求：创建 TeamAgent + 启动 streaming ⤵️(#9.85)
  │   └─ 后续请求：TeamManager.Interact
  └─ 步骤 8+: 其他模式
```

### 1.2 核心作用

TeamHelpers 负责：

1. **会话生命周期管理** — 首次请求 vs 后续请求分流
2. **Slash 命令处理** — `/evolve`、`/evolve_list`、`/evolve_simplify`、`/evolve_rebuild`、`/hide_dm`、`/debug`
3. **流式广播** — 通过 `pendingWaiters` 全局字典 + `broadcastEvent` 将 Runner 产出的事件分发给所有等待中的请求
4. **Team 身份元数据同步** — `SyncTeamIdentityMetadata` 在 `runtime_ready` 事件时持久化 team 身份
5. **Team Monitor 绑定** — `EnsureMonitorForActiveRuntime` 在 runtime 就绪后挂载 TeamMonitorHandler
6. **Evolution 监控** — `EnsureTeamEvolutionWatcher` + `watchTeamEvolutionAndPush` 后台监控 TeamSkillEvolutionRail
7. **Team 首次请求初始化** — 调用 `teamManager.EnsureTeamSharedSkillsInitialized` + `PrepareRuntimeActivation`

### 1.3 Python 对齐文件

- `jiuwenswarm/server/runtime/agent_adapter/team_helpers.py`（1393 行）
- `jiuwenswarm/server/runtime/agent_adapter/evolution_helpers.py`（480 行，Go 已实现）

## 2. 文件组织

对齐 Python `team_helpers.py` 单文件风格，在 `adapter` 包下新建 `team_helpers.go`：

```
internal/swarm/server/adapter/
├── team_helpers.go              # 新文件：包级辅助函数（对齐 Python team_helpers.py）
├── deep_adapter_team.go         # 现有文件：*DeepAdapter 方法（保留 + 增强）
├── deep_adapter_evolution.go    # 现有文件：single-agent evolution 相关
├── deep_adapter_slash.go        # 现有文件：single-agent slash 命令
└── evolution/                   # 现有子包：evolution_helpers（已实现）
```

Python 的 `team_helpers.py` 里全是**模块级函数**（不是 DeepAgent 的方法），Go 的 `team_helpers.go` 同样存放**包级函数**。`deep_adapter_team.go` 中的 `*DeepAdapter` 方法内部调用 `team_helpers.go` 的函数。

## 3. team_helpers.go 详细设计

### 3.1 常量

| Python 常量 | Go 常量 | 值 | 说明 |
|------------|---------|---|------|
| `_HIDE_DM_PREFIX` | `hideDMPrefix` | `"/hide_dm"` | 隐藏 DM 指令前缀 |
| `_DEBUG_PREFIX` | `debugPrefix` | `"/debug"` | 调试指令前缀 |
| `_STREAM_TRACE_ENV_KEY` | `streamTraceEnvKey` | `"JIUWENSWARM_TEAM_STREAM_TRACE"` | 流追踪环境变量 |
| `_TEAM_CREATE_KINDS` | `teamCreateKinds` | `map[string]bool{"CREATE": true, "NEW_TEAM_IN_SESSION": true}` | 团队创建类型集合 |

### 3.2 全局状态

```go
var (
    // pendingWaitersMu 保护 pendingWaiters 的读写锁
    pendingWaitersMu sync.RWMutex
    // pendingWaiters 等待事件广播的请求队列
    // key: (channelID, sessionID)，value: [(requestID, chan map[string]any)]
    // 对齐 Python: _pending_waiters: dict[tuple[str, str], list[tuple[str, asyncio.Queue]]]
    pendingWaiters = make(map[[2]string][]pendingWaiter)
)

// pendingWaiter 单个等待条目
type pendingWaiter struct {
    RequestID string
    Ch        chan map[string]any
}
```

**并发安全：** Python 用 asyncio（单线程事件循环），Go 需要 `sync.RWMutex` 保护。写操作（注册/注销 waiter、广播）加写锁，读操作（检查 waiter 数量）加读锁。

### 3.3 纯函数

#### `stripDirective(query, prefix string) (string, bool)`

对齐 Python `_strip_directive(query, prefix)`。

从 query 开头去除 slash 指令前缀。仅当 query 以 prefix 开头且后跟空格或结尾时才去除。

```go
func stripDirective(query, prefix string) (string, bool)
```

#### `extractQueryDirectives(query string) (cleanedQuery string, hideDM, debug bool)`

对齐 Python `_extract_query_directives(query)`。

依次去除 `/hide_dm` 和 `/debug` 指令，返回清理后的 query 和指令标志。

```go
func extractQueryDirectives(query string) (string, bool, bool)
```

#### `resolveChannelID(channelID string) string`

对齐 Python `_resolve_channel_id(channel_id)`。

将空/nil channelID 规范化为 `"default"`。

```go
func resolveChannelID(channelID string) string
```

### 3.4 事件判断函数

#### `isLeaderOutput(chunk map[string]any) bool`

对齐 Python `_is_leader_output(chunk)`。

判断 team OutputSchema chunk 是否应展示给 claw 用户。规则：
- `chunk["type"] == "message"` 且 payload 中 `event_type` 为 `"team.runtime_ready"` 或 `"team.completed"` → true
- `chunk["type"] == "team.runtime_ready"` → true
- `chunk["role"]` 为 `TeamRole.LEADER` → true
- role 为 nil → true（默认视为 leader）
- 其他 → false

```go
func isLeaderOutput(chunk map[string]any) bool
```

#### `isTeammateOutput(chunk map[string]any) bool`

对齐 Python `_is_teammate_output(chunk)`。

判断 chunk 是否来自 teammate。规则：
- `chunk["role"]` 为 `TeamRole.TEAMMATE` → true
- 其他 → false

```go
func isTeammateOutput(chunk map[string]any) bool
```

#### `enrichTeammateEvent(parsed map[string]any, chunk map[string]any) map[string]any`

对齐 Python `_enrich_teammate_event(parsed, chunk)`。

丰富 teammate 事件，添加 `role` 和 `member_name` 字段。

```go
func enrichTeammateEvent(parsed map[string]any, chunk map[string]any) map[string]any
```

#### `isDuplicateAskUserQuestion(parsed map[string]any, emittedRequestIDs map[string]bool) bool`

对齐 Python `_is_duplicate_ask_user_question(parsed, emitted_request_ids)`。

去重 `chat.ask_user_question` 事件。若 `event_type != "chat.ask_user_question"` 或 request_id 为空 → false；若 request_id 已在集合中 → true；否则加入集合返回 false。

```go
func isDuplicateAskUserQuestion(parsed map[string]any, emittedRequestIDs map[string]bool) bool
```

### 3.5 审批辅助函数

#### `approvalChunkFromEvent(evt map[string]any) map[string]any`

对齐 Python `_approval_chunk_from_event(evt)`。

从事件中提取审批 chunk。通过 `parseStreamChunk` 解析，检查 `event_type == "chat.ask_user_question"` 且 request_id 和 questions 有效。

```go
func approvalChunkFromEvent(evt map[string]any) map[string]any
```

#### `approvalResultFromEventOrItems(skillName string, event, items any, noChangesOutput, invalidOutput string) map[string]any`

对齐 Python `_approval_result_from_event_or_items(...)`。

构建审批结果 dict。优先从 event 提取 approval_chunk；次选 items 非空时返回 invalid_output；最后返回 no_changes_output。

```go
func approvalResultFromEventOrItems(skillName string, event any, items []any, noChangesOutput, invalidOutput string) map[string]any
```

### 3.6 完成状态函数

#### `teamProcessingDoneChunk(requestID, channelID, sessionID string) *agentschema.AgentResponseChunk`

对齐 Python `_team_processing_done_chunk(request_id, channel_id, session_id)`。

构建 `chat.processing_status(is_complete=True)` chunk。

```go
func teamProcessingDoneChunk(requestID, channelID, sessionID string) *agentschema.AgentResponseChunk
```

### 3.7 广播函数

#### `broadcastEvent(channelID, sessionID string, event map[string]any)`

对齐 Python `_broadcast_event(channel_id, session_id, event)`。

向所有等待同一 `(channelID, sessionID)` 的请求队列广播事件。遍历 `pendingWaiters` 中的 waiter，向每个 channel 发送事件的浅拷贝。

```go
func broadcastEvent(channelID, sessionID string, event map[string]any)
```

#### `groupTeamEvolutionApprovals(sessionID string, events []map[string]any) (map[string][]map[string]any, []string)`

对齐 Python `_group_team_evolution_approvals(session_id, events)`。

包装 `evolution.GroupEvolutionApprovals`，提供 `warnMissingRequestID` 回调（记录日志）。

```go
func groupTeamEvolutionApprovals(sessionID string, events []map[string]any) (map[string][]map[string]any, []string)
```

### 3.8 元数据同步

#### `SyncTeamIdentityMetadata(ctx, channelID, sessionID, mode, readyTeamName, activationKind string)`

对齐 Python `sync_team_identity_metadata(...)`。

仅在 `activationKind` 属于 `teamCreateKinds` 时持久化 team 身份。已有 team_name 不匹配时保留现有元数据并 warn。

依赖：`sessionmd.GetSessionMetadata` / `sessionmd.UpdateSessionMetadata`（已实现）。

```go
func SyncTeamIdentityMetadata(ctx context.Context, channelID, sessionID, mode, readyTeamName, activationKind string)
```

### 3.9 Team Slash 命令

#### `handleTeamEvolveListCommand(ctx, channelID, sessionID, query string) map[string]any`

对齐 Python `_handle_team_evolve_list_command(channel_id, session_id, query)`。

处理 `/evolve_list <skill_name>` 命令。通过 `team.GetTeamManager(channelID).GetTeamSkillRail(sessionID)` 获取 rail，查询 store 展示经验摘要。

**与 single-agent 版的区别：** `deep_adapter_slash.go` 的 `handleEvolveListCommand` 通过 `d.skillEvolutionRail`（DeepAdapter 字段）获取 rail；team 版通过 `TeamManager.GetTeamSkillRail(sessionID)` 获取。

```go
func handleTeamEvolveListCommand(ctx context.Context, channelID, sessionID, query string) map[string]any
```

#### `handleTeamSlashCommand(ctx context.Context, channelID, sessionID, query string) map[string]any`

对齐 Python `_handle_team_slash_command(channel_id, session_id, query)`。

Team 模式专用的 slash 命令路由，依次检查：
1. `/evolve_list` → `handleTeamEvolveListCommand`
2. `/evolve_simplify` → 通过 TeamManager.GetTeamSkillRail 调用 `rail.RequestSimplify`
3. `/evolve` → 通过 TeamManager.GetTeamSkillRail 调用 `rail.RequestUserEvolution`

非 team slash 命令返回 nil。

```go
func handleTeamSlashCommand(ctx context.Context, channelID, sessionID, query string) map[string]any
```

#### `resolveTeamRebuildFollowup(ctx context.Context, channelID, sessionID, query string) (followupPrompt string, errMsg string)`

对齐 Python `_resolve_team_rebuild_followup(channel_id, session_id, query)`。

解析 `/evolve_rebuild` 命令，返回 followup_prompt 用于注入 agent loop。通过 TeamManager 获取 rail，调用 `rail.RequestRebuild`。

```go
func resolveTeamRebuildFollowup(ctx context.Context, channelID, sessionID, query string) (string, string)
```

### 3.10 回调函数

#### `onTeamWatcherDone(sessionID string)`

对齐 Python `_on_team_watcher_done(task)`。

Evolution watcher 完成回调，记录日志。

```go
func onTeamWatcherDone(sessionID string)
```

## 4. deep_adapter_team.go 增强

### 4.1 现有方法保留（不丢失）

| 方法 | 状态 | 说明 |
|------|------|------|
| `findTeamSkillRail` | ✅ 保留 | 查找 TeamSkillEvolutionRail |
| `findSkillCreateRail` | ✅ 保留 | 查找 TeamSkillCreateRail |
| `handleTeamSkillEvolveApproval` | ✅ 保留 | 处理 team skill 演进审批 |
| `pushTeamSkillEvolveResolutionStatus` | ✅ 保留 | 推送审批结果 |
| `optionMatches` | ✅ 保留 | 检查选项匹配 |
| `processTeamMessageStream` | ✅ 保留并增强 | 见 4.2 |

### 4.2 processTeamMessageStream 增强

在现有骨架中补充以下步骤（对齐 Python `process_team_message_stream` 的前半段）：

```go
func (d *DeepAdapter) processTeamMessageStream(...) {
    // 1. 获取 sessionID, channelID（已有）
    // 2. 获取 TeamManager（已有）
    // 3. 判断 isFirstRequest（已有）
    // 4. 提取 query directives（新增）
    if isFirstRequest {
        query, hideDM, debug := extractQueryDirectives(queryText)
        // 日志记录
    }
    // 5. 处理 team slash 命令（新增）
    slashResult := handleTeamSlashCommand(ctx, channelID, sessionID, queryText)
    if slashResult != nil {
        // yield approval chunks 或 final/error + done + complete
        return
    }
    // 6. 首次请求路径：⤵️(#9.85) TeamRunner 完整创建 + streaming
    // 7. 后续请求：TeamManager.Interact（已有）
}
```

### 4.3 新增方法（⤵️ 标记）

| 方法 | 对齐 Python | ⤵️ 原因 |
|------|------------|---------|
| `ensureMonitorForActiveRuntime` | `ensure_monitor_for_active_runtime` | 依赖 `Runner.get_agent_team_monitor`（9.85） |
| `consumeStreamWithQuery` | `_consume_stream_with_query` | 依赖 `Runner.run_agent_team_streaming`（9.85） |
| `consumeMonitorEvents` | `_consume_monitor_events` | 依赖 `TeamMonitorHandler` 事件流（10.6） |
| `watchTeamEvolutionAndPush` team 版 | `_watch_team_evolution_and_push` | 依赖 Runner 流式接口（9.85） |
| `ensureTeamEvolutionWatcher` | `ensure_team_evolution_watcher` | 检查逻辑可落地，启动 goroutine 处 ⤵️（依赖 watchTeamEvolutionAndPush） |

`ensureTeamEvolutionWatcher` 的部分逻辑（检查已有 watcher、获取 rail、检查 auto_scan）可以立即实现，仅启动 watcher goroutine 处标记 `⤵️(#9.85)`。

## 5. Waiter 注册/注销 API

`team_helpers.go` 还需提供 waiter 的注册和注销函数，供 `processTeamMessageStream` 调用：

```go
// registerWaiter 注册一个请求等待者
func registerWaiter(channelID, sessionID, requestID string, ch chan map[string]any)

// unregisterWaiter 注销一个请求等待者
func unregisterWaiter(channelID, sessionID, requestID string)
```

`processTeamMessageStream` 首次请求路径中：
1. 创建 `ch := make(chan map[string]any, 64)`
2. `registerWaiter(channelID, sessionID, requestID, ch)`
3. 从 ch 读取事件，转为 `AgentResponseChunk` 发送
4. defer `unregisterWaiter(channelID, sessionID, requestID)`

## 6. 依赖关系

### 6.1 已满足的依赖

| 依赖 | Go 实现 | 状态 |
|------|---------|------|
| `evolution.GroupEvolutionApprovals` | `evolution/helpers.go` | ✅ |
| `sessionmd.GetSessionMetadata` / `UpdateSessionMetadata` | `session/` | ✅ |
| `team.GetTeamManager` | `team/team_manager.go` | ✅ |
| `TeamManager.GetTeamSkillRail` | `team/team_manager_skill.go` | ✅ |
| `TeamManager.HasStreamTask` | `team/team_manager_monitor.go` | ✅ |
| `TeamManager.Interact` | `team/team_manager_interact.go` | ✅ |
| `TeamManager.GetEnrichedTeamSpec` | `team/team_manager_spec.go` | ✅ |
| `TeamRole.LEADER / TEAMMATE` | `agent_teams/schema/team/` | ✅ |
| `parseStreamChunk` | 需确认是否存在 | 待查 |

### 6.2 ⤵️ 标记的依赖（不阻塞本次实现）

| 依赖 | 章节 | 影响 |
|------|------|------|
| `Runner.get_agent_team_monitor` | 9.85 | `ensureMonitorForActiveRuntime` |
| `Runner.run_agent_team_streaming` | 9.85 | `consumeStreamWithQuery` |
| `TeamMonitorHandler` 事件流 | 10.6 | `consumeMonitorEvents` |
| `EnsureTeamSharedSkillsInitialized` 实现 | 9.72 | `processTeamMessageStream` 首次请求路径 |
| `AttachDistributedHooksForRunnerRuntime` 实现 | 9.72 | `runtime_ready` 后调用 |

## 7. 测试计划

### 7.1 team_helpers.go 测试

`team_helpers_test.go` 放同包下，覆盖：

1. **`stripDirective`** — 正常剥离、前缀不匹配、前缀无空格、空 query
2. **`extractQueryDirectives`** — 组合指令、无指令、单一指令
3. **`resolveChannelID`** — 空串、"default"、自定义值
4. **`isLeaderOutput`** — 各类 chunk（runtime_ready、completed、LEADER role、nil role、TEAMMATE role）
5. **`isTeammateOutput`** — TEAMMATE role、LEADER role、nil role
6. **`enrichTeammateEvent`** — 有 source_member、无 source_member
7. **`isDuplicateAskUserQuestion`** — 首次/重复/非 ask_user_question/空 request_id
8. **`teamProcessingDoneChunk`** — 字段验证
9. **`broadcastEvent`** — 多 waiter 广播、无 waiter、channel 满时不阻塞
10. **`groupTeamEvolutionApprovals`** — 有 approval 事件、无 approval 事件、缺 request_id
11. **`SyncTeamIdentityMetadata`** — CREATE kind、非 CREATE kind、已有不同 team_name
12. **`handleTeamSlashCommand`** — /evolve_list、/evolve_simplify、/evolve、未知命令
13. **`resolveTeamRebuildFollowup`** — 正常、无 skill、rail 为 nil
14. **`registerWaiter` / `unregisterWaiter`** — 注册/注销/并发安全
15. **`approvalChunkFromEvent`** — 有效审批事件、非审批事件、缺字段
16. **`approvalResultFromEventOrItems`** — 有 approval_chunk、有 items、都无

### 7.2 deep_adapter_team.go 增强 测试

`processTeamMessageStream` 增强部分的测试：
- query directives 提取分支
- team slash 命令分支（yield approval chunks / final / error）
- 现有 6 个方法的回归测试

## 8. doc.go 更新

`adapter/doc.go` 文件目录中需添加：

```
// ├── team_helpers.go            # Team 辅助函数：directives/事件判断/广播/slash命令/元数据同步
```
