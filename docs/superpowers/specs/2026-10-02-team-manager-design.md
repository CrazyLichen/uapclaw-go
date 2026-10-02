# 应用层 TeamManager 设计文档

> 日期：2026-10-02
> 对齐 Python：`jiwenswarm/agents/harness/team/team_manager.py`

## 1. 背景与动机

Python 有两层架构管理 TeamAgent 生命周期：

| 层 | Python 类 | 职责 |
|---|---|---|
| **应用层** | `TeamManager`（jiwenswarm） | channel_id 索引、session 切换、Rail 管理、技能同步、stream task 管理、分布式 hooks |
| **核心层** | `TeamRuntimeManager`（openjiuwen） | team_name 索引的 Pool、activate/finalize/interact 路由、生命周期结算 |

Go 当前只有核心层 `TeamRuntimeManager`，缺少应用层 `TeamManager`。导致：

1. **M-22**：`processTeamMessageStream` 通过 `inputs["params"]["team_name"]` 提取 teamName，可能拿不到值；Python 通过 `channel_id → TeamManager` 路由
2. **M-44**：`Interact` 中 Agent 为 nil 无 warning——应用层缺少 session 活跃性检查
3. 多个 M 级问题（M-05/10/11/12/13/14 等 no-op）与应用层缺失有关

## 2. 文件结构

```
internal/swarm/agents/harness/team/
├── doc.go                        # 包文档
├── team_manager.go               # TeamManager 结构体 + 全局索引 + 访问器
├── team_manager_lifecycle.go     # 生命周期方法（create/destroy/prepare/commit/clear/terminate/stop/pause/delete）
├── team_manager_interact.go      # interact 路由
├── team_manager_spec.go          # Spec 构建：GetEnrichedTeamSpec, ApplyTeamPlanMode, BuildAgentCustomizer
├── team_manager_skill.go         # 技能管理：14 个 rail/skill 注册查询方法
├── team_manager_monitor.go       # 监控/流：monitor, stream_task, evolution_watcher
├── team_manager_distributed.go   # 分布式 hooks + 传输字段标准化
├── team_manager_test.go          # 单元测试
```

## 3. 核心结构体

```go
// TeamManager 管理一个 channel 下的所有 TeamAgent 运行时。
// 对齐 Python: jiuwenswarm/agents/harness/team/team_manager.py
//
// 每个 channel 拥有独立的 TeamManager 实例，通过 GetTeamManager(channelID) 获取。
// TeamManager 管理 session 级的活跃状态、Rail 生命周期、技能同步、流任务等应用层职责，
// 通过 TeamRuntimeManager（核心层）间接访问 Pool 执行实际的 interact/activate/finalize 操作。
type TeamManager struct {
    mu sync.Mutex

    // team agent 引用
    teamAgents       map[string]*agent.TeamAgent // sessionID → TeamAgent（内存持有）
    runnerTeamAgents map[string]*agent.TeamAgent // sessionID → Runner 池引用

    // 活跃状态（单活跃 session 语义）
    activeSessionID  *string
    activeTeamName   *string
    pendingSessionID *string
    pendingTeamName  *string

    // 监控
    teamMonitors map[string]*TeamMonitorHandler // sessionID → monitor

    // 流任务（sessionID → cancel 函数，Go 用 context.CancelFunc 替代 Python asyncio.Task）
    streamTasks map[string]context.CancelFunc

    // Rail 管理
    teamSkillRails          map[string]any            // sessionID → TeamSkillEvolutionRail
    teamMemberSkillEvoRails map[string][]any          // sessionID → []SkillEvolutionRail
    teamSkillCreateRails    map[string]any            // sessionID → TeamSkillCreateRail
    teamRailContexts        map[string]*TeamRailMountContext
    teamLiveRails           map[string][]LiveRailEntry
    teamSkillSyncTargets    map[string]SkillSyncTarget

    // 演进监控
    teamEvolutionWatchers map[string]context.CancelFunc
}
```

## 4. 全局索引

对齐 Python 的 `_team_managers: dict[str, TeamManager]` 和 `get_team_manager(channel_id)`：

```go
var (
    teamManagers   = make(map[string]*TeamManager)
    teamManagersMu sync.RWMutex
)

// GetTeamManager 获取指定 channel 的 TeamManager 实例。
// 对齐 Python: get_team_manager(channel_id)
func GetTeamManager(channelID string) *TeamManager {
    resolved := strings.TrimSpace(channelID)
    if resolved == "" {
        resolved = "default"
    }
    teamManagersMu.RLock()
    mgr, ok := teamManagers[resolved]
    teamManagersMu.RUnlock()
    if ok {
        return mgr
    }
    teamManagersMu.Lock()
    defer teamManagersMu.Unlock()
    // double-check
    if mgr, ok = teamManagers[resolved]; ok {
        return mgr
    }
    mgr = NewTeamManager()
    teamManagers[resolved] = mgr
    return mgr
}
```

## 5. 方法清单（完整对齐 Python 40+ 公开方法）

### 5.1 生命周期（15 个）

| Go 方法 | Python 对应 | 签名 |
|---------|------------|------|
| `CreateTeam` | `create_team` | `(ctx, sessionID, deepAgent, ...) (*agent.TeamAgent, error)` |
| `GetOrCreateTeam` | `get_or_create_team` | `(ctx, sessionID, ...) (*agent.TeamAgent, error)` |
| `DestroyTeam` | `destroy_team` | `(ctx, sessionID) (bool, error)` |
| `CleanupAll` | `cleanup_all` | `(ctx) error` |
| `PrepareRuntimeActivation` | `prepare_runtime_activation` | `(ctx, sessionID, teamName) error` |
| `PrepareSessionSwitch` | `prepare_session_switch` | `(ctx, targetSessionID, reason) error` |
| `CommitRuntimeReady` | `commit_runtime_ready` | `(sessionID, teamName)` |
| `ClearPendingRuntime` | `clear_pending_runtime` | `(sessionID)` |
| `ClearActiveRuntime` | `clear_active_runtime` | `(sessionID)` |
| `TerminateSessionRuntime` | `terminate_session_runtime` | `(ctx, sessionID, reason) (bool, error)` |
| `CancelSessionRuntime` | `cancel_session_runtime` | `(ctx, sessionID, reason) (bool, error)` |
| `StopSessionRuntime` | `stop_session_runtime` | `(ctx, sessionID, reason) (bool, error)` |
| `PauseSessionRuntime` | `pause_session_runtime` | `(ctx, sessionID, reason) (bool, error)` |
| `DeleteSessionRuntime` | `delete_session_runtime` | `(ctx, sessionID, reason) (bool, error)` |
| `CancelAllStreamTasks` | `cancel_all_stream_tasks` | `(reason)` |

### 5.2 交互（1 个）

| Go 方法 | Python 对应 | 签名 |
|---------|------------|------|
| `Interact` | `interact` | `(ctx, sessionID, userInput) (bool, error)` |

Python 的 `interact` 内部委托 `Runner.interact_agent_team(user_input, team_name=, session_id=)`，最终到达 `TeamRuntimeManager.interact`。Go 的 `Interact` 也委托到 `TeamRuntimeManager.Interact`，但通过 `activeTeamName` 确定 team_name。

### 5.3 Spec 构建（6 个）

| Go 方法 | Python 对应 |
|---------|------------|
| `GetEnrichedTeamSpec` | `get_enriched_team_spec` |
| `ApplyTeamPlanMode` | `apply_team_plan_mode` |
| `BuildAgentCustomizer` | `build_agent_customizer` |
| `RegisterMemberRuntimeTools` | `register_member_runtime_tools` |
| `NormalizeDistributedTransportFields` | `normalize_distributed_transport_fields` |
| `ParsePort` | `parse_port` |

### 5.4 技能管理（14 个）

| Go 方法 | Python 对应 |
|---------|------------|
| `EnsureTeamSharedSkillsInitialized` | `ensure_team_shared_skills_initialized` |
| `SyncTeamSkills` | `sync_team_skills` |
| `RegisterTeamSkillRail` | `register_team_skill_rail` |
| `RegisterTeamMemberSkillEvolutionRail` | `register_team_member_skill_evolution_rail` |
| `RegisterTeamSkillCreateRail` | `register_team_skill_create_rail` |
| `RegisterTeamRailContext` | `register_team_rail_context` |
| `RegisterTeamLiveRail` | `register_team_live_rail` |
| `RegisterTeamSkillSyncTarget` | `register_team_skill_sync_target` |
| `HasTeamSkillSyncTarget` | `has_team_skill_sync_target` |
| `GetTeamSkillRail` | `get_team_skill_rail` |
| `GetTeamSkillCreateRail` | `get_team_skill_create_rail` |
| `FindTeamSkillRailForRequest` | `find_team_skill_rail_for_request` |
| `DrainTeamSkillEvents` | `drain_team_skill_events` |
| `GetTeamRailContext` | `get_team_rail_context` |
| `UpdateEvolutionConfig` | `update_evolution_config` |

### 5.5 监控/流（8 个）

| Go 方法 | Python 对应 |
|---------|------------|
| `GetMonitor` | `get_monitor` |
| `RegisterMonitor` | `register_monitor` |
| `HasStreamTask` | `has_stream_task` |
| `PopStreamTask` | `pop_stream_task` |
| `RegisterStreamTask` | `register_stream_task` |
| `GetTeamEvolutionWatcher` | `get_team_evolution_watcher` |
| `RegisterTeamEvolutionWatcher` | `register_team_evolution_watcher` |
| `PopTeamEvolutionWatcher` | `pop_team_evolution_watcher` |

### 5.6 访问器（6 个）

| Go 方法 | Python 对应 |
|---------|------------|
| `ActiveSessionID()` | `active_session_id` property |
| `ActiveTeamName()` | `active_team_name` property |
| `PendingSessionID()` | `pending_session_id` property |
| `PendingTeamName()` | `pending_team_name` property |
| `GetTeamAgent(sessionID)` | `get_team_agent` |
| `AttachDistributedHooksForRunnerRuntime` | `attach_distributed_hooks_for_runner_runtime` |

## 6. processTeamMessageStream 改造

### 改造前（当前）

```go
func (d *DeepAdapter) processTeamMessageStream(ctx, req, inputs) {
    // 从 inputs["params"]["team_name"] 提取 teamName — 可能拿不到值
    teamName := ""
    if params, ok := inputs["params"].(map[string]any); ok {
        if tn, ok := params["team_name"].(string); ok {
            teamName = tn
        }
    }
    mgr := runtime.GetTeamRuntimeManager()
    entry := mgr.PoolEntry().GetEntry(teamName)  // 直接访问核心层 Pool
    // ...
}
```

### 改造后（对齐 Python）

```go
func (d *DeepAdapter) processTeamMessageStream(ctx, req, inputs) {
    sessionID := ""
    if req.SessionID != nil { sessionID = *req.SessionID }
    channelID := ""
    if req.ChannelID != nil { channelID = *req.ChannelID }

    // 通过 channel_id 获取 TeamManager
    teamManager := team.GetTeamManager(channelID)

    isFirstRequest := !teamManager.HasStreamTask(sessionID)

    if isFirstRequest {
        // 构建 TeamAgentSpec
        teamSpec, err := teamManager.GetEnrichedTeamSpec(ctx, sessionID, d.instance, ...)
        // 准备运行时激活
        teamManager.PrepareRuntimeActivation(ctx, sessionID, teamSpec.TeamName)
        // 启动后台流任务 + 注册
        // ...
    } else {
        // 后续请求：通过 TeamManager.Interact 路由
        query := paramsString(inputs, "query", "")
        ok, err := teamManager.Interact(ctx, sessionID, query)
        // ...
    }
}
```

调用链对齐：

| 层 | Python | Go |
|---|---|---|
| 应用层入口 | `get_team_manager(channel_id).interact(session_id, user_input)` | `GetTeamManager(channelID).Interact(ctx, sessionID, userInput)` |
| Runner 桥接 | `Runner.interact_agent_team(user_input, team_name=, session_id=)` | Go 无 Runner 桥接层，TeamManager 直接调核心层 |
| 核心层 | `TeamRuntimeManager.interact(payload, team_name=, session_id=)` | `TeamRuntimeManager.Interact(ctx, payload, teamName, sessionID)` |

**Go 简化**：Python 有 `TeamManager → Runner → TeamRuntimeManager` 三层，Go 省略 Runner 桥接（Go 的 Runner 尚未完整实现），TeamManager 直接调 TeamRuntimeManager。

## 7. registry.PoolEntry 删除

`registry.PoolEntry` 接口是 Go 独有的循环依赖破解方案，Python 不存在此接口。有了应用层 TeamManager 后：

1. `deep_adapter_team.go` 通过 TeamManager 访问 Pool，不再需要 `mgr.PoolEntry().GetEntry(teamName)`
2. `team_agent.go` 中的 `mgr.PoolEntry()` 调用改为通过 TeamManager 间接访问
3. `registry.PoolEntry` 接口和 `TeamRuntimeManager.PoolEntry()` 方法删除

### 删除清单

| 文件 | 删除内容 |
|------|---------|
| `internal/agent_teams/registry/interfaces.go` | `PoolEntry` 接口、`PoolTeamEntry` 接口 |
| `internal/agent_teams/runtime/manager.go` | `PoolEntry()` 方法 |
| `internal/agent_teams/runtime/pool.go` | `GetEntry()` 方法（如果仅服务于 PoolEntry）、编译期断言 `var _ registry.PoolEntry = ...` |
| `internal/agent_teams/agent/team_agent.go` | `mgr.PoolEntry()` 调用改为 TeamManager 间接访问 |
| `internal/swarm/server/adapter/deep_adapter_team.go` | `mgr.PoolEntry().GetEntry(teamName)` 改为通过 TeamManager |

## 8. 依赖关系

```
swarm/server/adapter (DeepAdapter)
  → swarm/agents/harness/team (TeamManager)     ← 新增
  → agent_teams/runtime (TeamRuntimeManager)     ← 保留核心层

swarm/agents/harness/team (TeamManager)
  → agent_teams/runtime (TeamRuntimeManager)
  → agent_teams/agent (TeamAgent)
  → agentcore/harness/rails/evolution (Rail 类型)

agent_teams/agent (TeamAgent)
  → agent_teams/runtime (TeamRuntimeManager)     ← 保留，但不再直接访问 PoolEntry
```

无循环依赖：TeamManager 在应用层（swarm），不依赖 adapter；adapter 依赖 TeamManager。

## 9. Go/Python 差异说明

| 差异点 | Python | Go | 原因 |
|--------|--------|-----|------|
| 全局索引 | `_team_managers: dict[str, TeamManager]` | `teamManagers map[string]*TeamManager` + `sync.RWMutex` | Go 需要显式并发保护 |
| 流任务 | `asyncio.Task` | `context.CancelFunc` | Go 无 asyncio，用 context 取消 goroutine |
| Runner 桥接 | `TeamManager → Runner → TeamRuntimeManager` | `TeamManager → TeamRuntimeManager`（省略 Runner） | Go 的 Runner 尚未完整实现 |
| Rail 字段类型 | 具体 Rail 类 | `any`（部分） | Go 的 Rail 类型系统尚未完全对齐，先用 any 占位 |
| session_id 传参 | 隐式通过 `get_session_id()` contextvar | 显式 `sessionID string` 参数 | Go 无 contextvar，显式传参 |
| pool_entry 访问 | 直接 `self._pool.get(team_name)` | 通过 TeamManager 间接访问 | 删除 registry.PoolEntry 破除循环依赖接口 |

## 10. 测试策略

1. **单元测试**：每个方法组一个测试文件，使用 fake TeamAgent/fake TeamRuntimeManager
2. **集成测试**：验证 `processTeamMessageStream` 的 channel_id → TeamManager → Interact 完整链路
3. **并发测试**：验证 `GetTeamManager` 的 double-check locking、`TeamManager.mu` 的互斥保护
