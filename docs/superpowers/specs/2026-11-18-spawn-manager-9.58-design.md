# 9.58 SpawnManager 设计文档

## 概述

SpawnManager 是 TeamAgent 编排层的核心子管理器，负责 teammate 进程生命周期管理。
支持双模式生成：inprocess（goroutine）和 subprocess（OS 进程），对上层透明。

## 核心决策

| 决策项 | 结论 | 原因 |
|--------|------|------|
| 实现范围 | 最小可运行，方法步骤完整对齐 Python | 用户要求不丢失方法和步骤 |
| 文件组织 | 三层分离，spawn/ 子包独立 | 对齐 Python 目录结构 |
| 循环依赖 | 最小接口 SpawnableAgent + 工厂函数 AgentFactory | spawn/ 不 import agent/ |
| InProcess 生命周期 | context.Cancel + done chan | 每个 handle 独立，对齐 Python task.cancel() + task.done() |
| goroutine 运行 | 启动 goroutine 对齐 Python create_task | 内部 TODO(#9.85) 等 RunAgentTeam |
| SpawnHandle 接口 | 不含回调，回调在构造时注入 | 更简洁，对齐 Python 回调在构造后赋值 |
| string 参数 | 不用指针，零值空串=默认 | 用户要求 |

## 流程位置

```
用户输入 → TeamAgent.invoke/stream
            │
            ├─ CoordinationKernel.start(session)
            │     └─ auto_start_all / auto_start_member
            │           └─ TeamBackend.startup(on_created)
            │                 └─ _on_teammate_created 回调
            │                       └─ SpawnManager.SpawnTeammate()  ◀◀ 核心
            │                             │
            │                             ├─ [inprocess] → InProcessSpawnHandle
            │                             │    └─ goroutine (TODO #9.85: Runner.RunAgentTeam)
            │                             │    └─ wireInprocessChunkForward (TODO #9.60)
            │                             │
            │                             └─ [subprocess] → SpawnedProcessHandle
            │                                  └─ runner.SpawnAgent → 子进程
            │
            └─ 异常/不健康 → OnTeammateUnhealthy
                  └─ SpawnManager.RestartTeammate()
                        ├─ CleanupTeammate（断 chunkForward + forceKill）
                        ├─ BuildContextFromDB（TODO #9.64）
                        └─ 指数退避重试（2^attempt 秒）
```

## 文件结构

### 新建文件

```
agent_teams/
├── spawn/                              # 新建子包（9.58）
│   ├── doc.go                          # 包文档
│   ├── handle.go                       # SpawnHandle 统一接口
│   ├── inprocess_handle.go             # InProcessSpawnHandle
│   ├── inprocess_spawn.go              # SpawnableAgent 接口 + AgentFactory + InProcessSpawn
│   └── shared_resources.go             # 进程级全局单例
│
├── agent/
│   ├── spawn_manager.go                # SpawnManager（新建）
│   └── spawn_manager_test.go           # 测试（新建）
```

### 修改文件

| 文件 | 修改内容 |
|------|---------|
| `agent_teams/doc.go` | spawn/ 子目录从 TODO 更新为实际文件列表 |
| `agent/team_agent.go` | `spawnManager any` → `*SpawnManager` + 方法实现 |
| `agent/agent_configurator.go` | 补全回调类型 + SetupTeamBackend |
| `agent/payload.go` | BuildSpawnConfig 返回实际 spawn.SpawnAgentConfig 类型 |
| `agentcore/runner/spawn/child.go` | TeamAgent 分支从报错改 TODO 占位 |

## 接口设计

### spawn.SpawnHandle

```go
// SpawnHandle 统一 inprocess 和 subprocess 的操作接口。
type SpawnHandle interface {
    ProcessID() string
    IsAlive() bool
    IsHealthy() bool
    Shutdown(ctx context.Context, timeout ...time.Duration) (bool, error)
    ForceKill() error
    WaitForCompletion() (int, error)
    StartHealthCheck(ctx context.Context, interval ...time.Duration) error
    StopHealthCheck() error
}
```

SpawnedProcessHandle 天然满足此接口，无需修改。

### spawn.SpawnableAgent

```go
// SpawnableAgent 进程内生成的 Agent 最小接口。
// 仅暴露 InProcessSpawnHandle 消费者所需操作。
type SpawnableAgent interface {
    AgentCard() *schema.AgentCard
}
```

### spawn.AgentFactory

```go
// AgentFactory 创建并配置 Agent 的工厂函数。
// 对齐 Python: _TeamAgent(card) + teammate.configure(spec, ctx)
// 由 SpawnManager 注入，封装 spec 解析 / card 构建 / 配置全流程。
type AgentFactory func(runtimeCtx atschema.TeamRuntimeContext) (SpawnableAgent, error)
```

## 结构体设计

### spawn.InProcessSpawnHandle

```go
type InProcessSpawnHandle struct {
    processID         string                // "inproc-{memberName}"
    cancelCtx         context.CancelFunc    // 取消 goroutine
    done              chan struct{}          // close 通知完成
    agentRef          SpawnableAgent        // 进程内 Agent 引用（对齐 Python agent_ref: Any）
    chunkForward      any                   // ⤵️ 预留：StreamController（9.60）
    onUnhealthy       func()                // 不健康回调
    shutdownRequested bool                  // 是否已请求关闭
    mu                sync.Mutex            // 保护并发访问
}
```

方法清单：

| # | 方法 | Python 对应 | 说明 |
|---|------|------------|------|
| 1 | `ProcessID() string` | `process_id` | 进程 ID |
| 2 | `IsAlive() bool` | `is_alive` | `select { case <-done: false; default: true }` |
| 3 | `IsHealthy() bool` | `is_healthy` | `IsAlive() && !shutdownRequested` |
| 4 | `StartHealthCheck(ctx, interval...) error` | `start_health_check` | No-op |
| 5 | `StopHealthCheck() error` | `stop_health_check` | No-op |
| 6 | `Shutdown(ctx, timeout...) (bool, error)` | `shutdown` | cancel + 等待 done 或超时 |
| 7 | `ForceKill() error` | `force_kill` | cancel + 不等待 |
| 8 | `WaitForCompletion() (int, error)` | `wait_for_completion` | 等 done，0=成功，-1=异常 |
| 9 | `SetOnUnhealthy(fn func())` | 构造后赋值 `on_unhealthy` | 设置不健康回调 |

### spawn.InProcessSpawn

```go
func InProcessSpawn(
    ctx context.Context,
    factory AgentFactory,
    runtimeCtx atschema.TeamRuntimeContext,
    initialMessage string,
    sessionID string,
) (*InProcessSpawnHandle, error)
```

核心逻辑：
1. 调用工厂创建并配置 teammate
2. 准备输入（initialMessage 为空时使用默认消息）
3. 启动 goroutine（对齐 Python: asyncio.create_task）
   - goroutine 内部 TODO(#9.85): Runner.RunAgentTeam
4. 包装 InProcessSpawnHandle 返回

### agent.SpawnManager

```go
type SpawnManager struct {
    state           *TeamAgentState
    configurator    *AgentConfigurator
    getTeamAgent    func() *TeamAgent
    spawnedHandles  map[string]spawn.SpawnHandle   // memberName → handle
    recoveryTasks   map[string]context.CancelFunc   // memberName → cancel
    mu              sync.Mutex
}
```

方法清单：

| # | 方法 | Python 对应 | 说明 | 依赖 |
|---|------|------------|------|------|
| 1 | `NewSpawnManager(state, configurator, teamAgentGetter)` | `__init__` | 构造 | — |
| 2 | `SpawnTeammate(ctx, runtimeCtx, initialMessage, sessionID, spawnConfig)` | `spawn_teammate` | 双路径生成 | #3,#4,#5 |
| 3 | `wireInprocessChunkForward(handle)` | `_wire_inprocess_chunk_forward` | chunk 转发 | ⤵️ #9.60 |
| 4 | `LookupInprocessAgent(memberName)` | `lookup_inprocess_agent` | 查找 agent 引用 | — |
| 5 | `CleanupTeammate(ctx, memberName)` | `cleanup_teammate` | 清理单个 | — |
| 6 | `RestartTeammate(ctx, memberName, maxRetries)` | `restart_teammate` | 指数退避重试 | #7,#5 |
| 7 | `BuildContextFromDB(memberName)` | `build_context_from_db` | 从 DB 恢复 | ⤵️ #9.64 |
| 8 | `OnTeammateUnhealthy(memberName)` | `on_teammate_unhealthy` | 不健康回调 | #6 |
| 9 | `PublishRestartEvent(memberName, restartCount)` | `publish_restart_event` | 发布重启事件 | ⤵️ #9.65 |
| 10 | `ShutdownAllHandles(ctx)` | `shutdown_all_handles` | 关闭所有 | #5 |
| 11 | `CancelRecoveryTasks()` | `cancel_recovery_tasks` | 取消恢复任务 | — |

### SpawnTeammate 双路径

```
SpawnTeammate(ctx, runtimeCtx, initialMessage, sessionID, spawnConfig)
  │
  ├─ spawnMode == "inprocess"
  │    ├─ spawn.InProcessSpawn(ctx, factory, runtimeCtx, initialMessage, sessionID)
  │    ├─ handle.SetOnUnhealthy(onTeammateUnhealthy)
  │    ├─ wireInprocessChunkForward(handle)           // ⤵️ #9.60
  │    └─ spawnedHandles[memberName] = handle
  │
  └─ spawnMode != "inprocess" (subprocess)
       ├─ payload := configurator.BuildSpawnPayload(runtimeCtx, initialMessage)
       ├─ config := configurator.BuildSpawnConfig(runtimeCtx)
       ├─ handle, err := runner.SpawnAgent(ctx, config, inputs, session, envs, spawnCfg)
       ├─ handle.SetOnUnhealthy(onTeammateUnhealthy)
       └─ spawnedHandles[memberName] = handle
```

### spawn.SharedResources

```go
var (
    sharedRuntime    any               // ⤵️ #9.85 TeamRuntime 单例
    sharedMemoryDB   any               // ⤵️ #9.64 InMemoryTeamDatabase 单例
    sharedDBInstances map[string]any   // ⤵️ #9.64 "db_type::conn_str" → TeamDatabase
)
```

| # | 方法 | Python 对应 | 说明 |
|---|------|------------|------|
| 1 | `GetSharedRuntime() any` | `get_shared_runtime()` | 懒初始化，当前 nil + TODO(#9.85) |
| 2 | `GetSharedDB(config any) any` | `get_shared_db(config)` | 按 db_type 区分，当前 nil + TODO(#9.64) |
| 3 | `CleanupSharedResources()` | `cleanup_shared_resources()` | 重置单例 + cleanup inprocess bus（⤵️ #9.65）|

## 现有文件修改详情

### agent/team_agent.go

1. `spawnManager any` → `spawnManager *SpawnManager`
2. `NewTeamAgent` 中构建 SpawnManager：`spawnManager: NewSpawnManager(state, configurator, func() *TeamAgent { return a })`
3. `SpawnTeammate` 委托 `spawnManager.SpawnTeammate`
4. `AutoStartMember` 委托 team_backend.startup_member（⤵️ #9.58 TeamBackend）
5. `AutoStartAll` 委托 team_backend.startup（⤵️ #9.58 TeamBackend）
6. `LookupHumanAgentRuntime` 通过 spawnManager.LookupInprocessAgent 查找

### agent/agent_configurator.go

1. `onTeammateCreated` / `onTeamCleaned` / `onTeamBuilt` 回调类型从 `any` 改为 `func(memberName string)` 等具体签名
2. `SetupTeamBackend` 方法体实现（构造 TeamBackend + 注册，⤵️ #9.58 TeamBackend 完整化）

### agent/payload.go

1. `BuildSpawnConfig` 返回 `spawn.SpawnAgentConfig`（当前返回 nil），填充 AgentKind/RunnerConfig/LoggingConfig/SessionID/Payload

### agentcore/runner/spawn/child.go

1. `ExecuteAgent` 中 `SpawnAgentKindTeamAgent` 分支：`return nil, fmt.Errorf("team_agent 模式尚未实现")` → `return nil, fmt.Errorf("team_agent 模式尚未实现：⤵️ 预留 TeamRunner（9.85）实现后回填")`

## 回填标记汇总

| 标记位置 | 等待章节 | 回填内容 |
|---------|---------|---------|
| InProcessSpawn goroutine 内 | #9.85 | runner.RunAgentTeam(runCtx, teammate, inputs, true, sessionID) |
| InProcessSpawnHandle.chunkForward | #9.60 | StreamController 观察者类型 |
| SpawnManager.wireInprocessChunkForward | #9.60 | StreamController.add_chunk_observer |
| SpawnManager.BuildContextFromDB | #9.64 | 从 TeamDatabase 恢复 TeamRuntimeContext |
| SpawnManager.PublishRestartEvent | #9.65 | 通过 Messager 发布 MemberRestartedEvent |
| SharedResources.GetSharedRuntime | #9.85 | TeamRuntime 单例创建 |
| SharedResources.GetSharedDB | #9.64 | TeamDatabase/InMemoryTeamDatabase 创建 |
| SharedResources.CleanupSharedResources | #9.65 | cleanup_inprocess_bus |
| AgentConfigurator.SetupTeamBackend | #9.58 | TeamBackend 构造和注册（9.58 自身范围内的子项） |

## agentcore/runner/spawn 与 agent_teams/spawn 的关系

| Python 层 | Go 已有 | 9.58 新增 | 说明 |
|-----------|--------|----------|------|
| core/runner/spawn/（通用基础设施） | ✅ agentcore/runner/spawn/ | — | 不需要动 |
| agent_teams/spawn/inprocess_handle.py | ❌ | spawn/inprocess_handle.go | InProcess 句柄 |
| agent_teams/spawn/inprocess_spawn.py | ❌ | spawn/inprocess_spawn.go | InProcess 生成 |
| agent_teams/spawn/shared_resources.py | ❌ | spawn/shared_resources.go | 全局单例 |
| agent_teams/agent/spawn_manager.py | ❌ | agent/spawn_manager.go | 管理层 |

agentcore/runner/spawn/ 是通用进程隔离基础设施，不感知 TeamAgent。
agent_teams/spawn/ + SpawnManager 是团队专用层，知道 TeamAgent 概念，调度两条路径。

## 循环依赖处理

| spawn/ 对 agent/ 的引用 | 处理方式 |
|------------------------|---------|
| InProcessSpawn 需要创建 TeamAgent | AgentFactory 闭包注入，spawn/ 不 import agent/ |
| InProcessSpawnHandle.agentRef | 类型 SpawnableAgent（接口），消费者断言 |
| SharedResources 的 Runtime/DB | 类型 any，TODO(#9.64/#9.85) 回填 |

依赖方向保持单向：agent/ → spawn/，spawn/ 零 import agent/。
