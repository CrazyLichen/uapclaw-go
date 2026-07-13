# 9.55 TeamAgent 设计文档

## 概述

9.55 实现生产级团队 Agent（TeamAgent），是整个多 Agent 协作系统的核心编排节点。
采用骨架+同步推进策略：9.55 产出 TeamAgent 主体及全部前置数据类/Schema，
子组件（AgentConfigurator/SpawnManager/SessionManager/StreamController/
RecoveryManager/CoordinationKernel/EventBus/Dispatcher）用 `any` 占位，
方法体用 `⤵️ 回填: 9.xx` 标注，后续步骤回填。

## 在 Agent 会话中的流程位置与作用

- **流程位置**：领域九第七个子分组 `9.x TeamAgent 应用层` 的第一个步骤
- **作用**：TeamAgent 是生产级团队 Agent，既可充当 Leader（分发任务、协调成员），
  也可充当 Teammate（执行具体任务）。组合式架构，内部包裹 DeepAgent 实例，
  委托给专职 Manager 管理配置/生成/恢复/会话/流式/协调。

## 架构：组合式四象限分解

Python TeamAgent 采用四象限分解：

| 象限 | 类型 | Go 对应 | 描述 |
|------|------|---------|------|
| 静态蓝图 | TeamAgentBlueprint | `agent/blueprint.go` | 不可变配置，构造时确定 |
| 可变状态 | TeamAgentState | `agent/state.go` | 运行时可变值，跨 Manager 共享 |
| 进程级基础设施 | TeamInfra | `agent/infra.go` | 每进程一份（messager/team_backend/workspace） |
| 实例级资源 | PrivateAgentResources | `agent/resources.go` | 每实例一份（harness/worktree/memory_manager） |

## 产出文件清单（13 个文件）

```
internal/agent_teams/
├── agent/
│   ├── team_agent.go          # TeamAgent 主体
│   ├── state.go               # TeamAgentState 可变状态
│   ├── member.go              # TeamMember 成员状态管理
│   ├── member_factory.go      # create_member_handle 工厂
│   ├── blueprint.go           # TeamAgentBlueprint 不可变蓝图
│   ├── infra.go               # TeamInfra 进程级基础设施
│   └── resources.go           # PrivateAgentResources 实例级资源
├── schema/
│   ├── team.go                # TeamRole/TeamLifecycle/TeamSpec/TeamRuntimeContext/...
│   ├── status.go              # MemberStatus/ExecutionStatus/MemberMode/TaskStatus
│   ├── blueprint.go           # TeamAgentSpec/LeaderSpec/TransportSpec/StorageSpec
│   └── events.go              # TeamTopic/TeamEvent/EventMessage + 全部事件 Schema
├── constants.go               # 保留名常量
└── context.go                 # session_id contextvar
```

## Go 包组织

- `internal/agent_teams/` — 与 `agentcore/`、`swarm/` 同级
- 完整对齐 Python `openjiuwen/agent_teams/` 目录结构

## TeamAgent 结构体设计

```go
type TeamAgent struct {
    card           *agentschema.AgentCard
    deepAgent      hinterfaces.DeepAgentInterface  // 内层 DeepAgent（⤵️ 回填: 9.57）
    configurator   any                             // ⤵️ 回填: 9.57 AgentConfigurator
    state          *TeamAgentState
    spawnManager   any                             // ⤵️ 回填: 9.58 SpawnManager
    recoveryManager any                            // ⤵️ 回填: 9.61 RecoveryManager
    sessionManager any                             // ⤵️ 回填: 9.59 SessionManager
    streamController any                           // ⤵️ 回填: 9.60 StreamController
    coordination   any                             // ⤵️ 回填: 9.62 CoordinationKernel
}
```

## 回填标记约定

- 字段占位：`any` 类型 + `// ⤵️ 回填: 9.xx — 说明`
- 方法体：方法签名完整，体内部用 `// ⤵️ 回填: 9.xx — 说明` + `return nil` / 零值
- Schema 字段：引用未实现类型用 `any` + 注释

## 方法对齐 Python 映射

| Go 方法 | Python 方法 | 回填步骤 |
|---------|------------|---------|
| `Configure` | `configure(spec, context)` | 9.57 |
| `Invoke` | `invoke(inputs, session)` | 9.60+9.62 |
| `Stream` | `stream(inputs, session, stream_modes)` | 9.60+9.62 |
| `DestroyTeam` | `destroy_team(force)` | 9.58+9.62 |
| `SpawnTeammate` | `spawn_teammate(ctx, ...)` | 9.58 |
| `AutoStartAll` | `auto_start_all()` | 9.58 |
| `CancelAgent` | `cancel_agent()` | 9.60 |
| `Steer` | `steer(content)` | 9.60 |
| `ShutdownSelf` | `shutdown_self()` | 9.60 |
| `RecoverTeam` | `recover_team()` | 9.61 |
| `ResumeForNewSession` | `resume_for_new_session(session)` | 9.59 |
| `RecoverForExistingSession` | `recover_for_existing_session(session)` | 9.59 |
| `StartCoordination` | `_start_coordination(session)` | 9.62 |
| `PauseCoordination` | `pause_coordination()` | 9.62 |
| `StopCoordination` | `stop_coordination()` | 9.62 |
| `Interact` | `interact(message)` | 9.62 |
| `Broadcast` | `broadcast(content)` | 9.62 |
| `DeliverInput` | `deliver_input(content, use_steer)` | 9.60 |
| `ResumeInterrupt` | `resume_interrupt(user_input)` | 9.60 |
| `ConcludeCompletedRound` | `conclude_completed_round(...)` | 9.60 |
| `FromSpawnPayload` | `from_spawn_payload(payload)` | 9.57 |
| `RecoverFromSession` | `recover_from_session(session, team_name, ...)` | 9.61 |

## 对应 Python 代码

- `openjiuwen/agent_teams/agent/team_agent.py` — TeamAgent 主体
- `openjiuwen/agent_teams/agent/state.py` — TeamAgentState
- `openjiuwen/agent_teams/agent/member.py` — TeamMember
- `openjiuwen/agent_teams/agent/member_factory.py` — create_member_handle
- `openjiuwen/agent_teams/agent/blueprint.py` — TeamAgentBlueprint
- `openjiuwen/agent_teams/agent/infra.py` — TeamInfra
- `openjiuwen/agent_teams/agent/resources.py` — PrivateAgentResources
- `openjiuwen/agent_teams/schema/team.py` — TeamRole/TeamSpec/TeamRuntimeContext 等
- `openjiuwen/agent_teams/schema/status.py` — MemberStatus/ExecutionStatus/TaskStatus
- `openjiuwen/agent_teams/schema/blueprint.py` — TeamAgentSpec/LeaderSpec 等
- `openjiuwen/agent_teams/schema/events.py` — 事件类型
- `openjiuwen/agent_teams/constants.py` — 保留名常量
- `openjiuwen/agent_teams/context.py` — session_id contextvar
