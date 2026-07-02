# HandoffTeam 设计规格

> 实现计划步骤 8.34：单活跃 Agent 交接模式
> Python 参考：`openjiuwen/core/multi_agent/teams/handoff/`

## 1. 概述

HandoffTeam 是事件驱动的顺序交接多 Agent 团队。同一时刻只有一个 Agent 在执行，当前 Agent 的 LLM 决定是完成任务还是调用注入的 `transfer_to_{agent_id}` 工具将控制权交给另一个 Agent。交接通过 Pub/Sub 消息总线驱动，支持中断/恢复。

### 在 Agent 会话流程中的位置

```
8.27-8.33 基础设施层（BaseTeam/TeamRuntime/MessageBus 等）  ✅ 已完成
8.34 HandoffTeam（单活跃 Agent 交接模式）                   ← 本设计
8.35 HierarchicalTeam(msgbus)                               ☐ 后续
8.36 HierarchicalTeam(tools)                                ☐ 后续
```

8.34 是基础设施层之上的第一个具体团队模式实现。

## 2. 目录结构

```
internal/agentcore/multi_agent/teams/
├── doc.go
├── utils.go                     # standaloneInvokeContext / standaloneStreamContext
└── handoff/
    ├── doc.go                   # 包文档
    ├── handoff_team.go          # HandoffTeam（实现 BaseTeam）
    ├── container_agent.go       # ContainerAgent（嵌入 CommunicableAgent + 持有 BaseAgent）
    ├── handoff_orchestrator.go  # HandoffOrchestrator（协调器，chan + doneOnce）
    ├── handoff_config.go        # HandoffRoute / HandoffConfig / HandoffTeamConfig
    ├── handoff_tool.go          # HandoffTool（实现 Tool 接口）
    ├── handoff_signal.go        # HandoffSignal + ExtractHandoffSignal（两层提取）
    ├── handoff_request.go       # HandoffRequest + HandoffHistoryEntry
    └── interrupt.go             # TeamInterruptSignal + ExtractInterruptSignal + FlushTeamSession
```

7 个核心文件与 Python 1:1 对应，加 `doc.go` 共 8 个。

## 3. 组件设计

### 3.1 HandoffConfig（配置层）

**文件**：`handoff_config.go`

```go
// HandoffRoute 交接路由规则
type HandoffRoute struct {
    Source string  // 源 Agent ID
    Target string  // 目标 Agent ID
}

// HandoffConfig 交接编排配置
type HandoffConfig struct {
    StartAgent           *agentschema.AgentCard               // 起始 Agent，nil 时取第一个
    MaxHandoffs          int                                   // 最大交接次数，默认 10
    Routes               []HandoffRoute                        // 路由规则，空时全互联
    TerminationCondition func(*HandoffOrchestrator) bool       // 可选终止条件
}

// HandoffTeamConfig HandoffTeam 完整配置，扩展 TeamConfig
type HandoffTeamConfig struct {
    maschema.TeamConfig
    Handoff HandoffConfig
}
```

### 3.2 HandoffOrchestrator（协调器）

**文件**：`handoff_orchestrator.go`

每会话协调器，追踪交接状态、路由审批、完成信号。

```go
const (
    CoordinatorStateKey = "__handoff_coordinator__"
    HandoffHistoryKey   = "__handoff_history__"
)

type HandoffOrchestrator struct {
    maxHandoffs          int
    terminationCondition func(*HandoffOrchestrator) bool
    handoffCount         int
    currentAgentID       string
    routeGraph           map[string]map[string]struct{}  // 邻接表
    doneCh               chan map[string]any             // 缓冲 1，完成通道
    doneOnce             sync.Once                       // 保证只发送一次
}
```

**完成机制**：`doneOnce` 保证 `doneCh <- result` 只执行一次，`Close()` 在 `_run_chain` 的 defer 中调用关闭 channel。

```go
func (o *HandoffOrchestrator) Complete(result map[string]any) {
    o.doneOnce.Do(func() {
        o.doneCh <- result
        close(o.doneCh)
    })
}

func (o *HandoffOrchestrator) Close() {
    close(o.doneCh)  // 在 _run_chain defer 中调用
}
```

**核心方法**：

| 方法 | 对应 Python | 说明 |
|------|-------------|------|
| `NewHandoffOrchestrator` | `__init__` | 从 Config 提取 maxHandoffs/terminationCondition，构建 routeGraph |
| `BuildRouteGraph` | `build_route_graph` | 全互联或显式路由的邻接表 |
| `RequestHandoff` | `request_handoff` | 检查 maxHandoffs/路由/终止条件，更新 handoffCount 和 currentAgentID |
| `Complete` | `complete` | doneOnce 保护发送结果到 doneCh 并 close |
| `Error` | `error` | 发送错误 dict 到 doneCh |
| `SaveToSession` | `save_to_session` | 持久化 currentAgentID/handoffCount 到 session |
| `RestoreFromSession` | `restore_from_session` | 从 session 恢复协调器状态 |

**字段与 Config 重复说明**：Orchestrator 从 Config 提取 `maxHandoffs`/`terminationCondition` 作为运行时检查用，Config 是 per-team 静态配置，Orchestrator 是 per-session 运行时对象，生命周期不同。与 Python 一致。

### 3.3 HandoffTool + HandoffSignal

**文件**：`handoff_tool.go`

```go
const (
    HandoffTargetKey  = "__handoff_to__"
    HandoffMessageKey = "__handoff_message__"
    HandoffReasonKey  = "__handoff_reason__"
)

type HandoffTool struct {
    card     *tool.ToolCard
    targetID string
}
```

HandoffTool 实现 `Tool` 接口（`Card()`/`Invoke()`/`Stream()`）。工具名为 `transfer_to_{targetID}`，LLM 通过调用此工具触发交接。

```go
func (t *HandoffTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
    reason, _ := inputs["reason"].(string)
    message, _ := inputs["message"].(string)
    return map[string]any{
        HandoffTargetKey:  t.targetID,
        HandoffMessageKey: message,
        HandoffReasonKey:  reason,
    }, nil
}
```

**文件**：`handoff_signal.go`

```go
type HandoffSignal struct {
    Target  string   // 目标 Agent ID
    Message string   // 传递给下一跳的上下文
    Reason  string   // 交接原因
}
```

**两层提取机制**（与 Python 一致）：

1. **第一层**：从 result dict 顶层查找 `__handoff_to__`（检查 result/output/result/content 子路径）
2. **第二层**：从 agentSession 消息历史中查找 tool message（遍历 `context → default_context_id → messages`，找 `role=tool` 的消息，解析 content JSON 检查 `__handoff_to__`）

Go 的 session state 存储结构与 Python 一致：`context → default_context_id → messages`。

### 3.4 HandoffRequest + TeamInterruptSignal

**文件**：`handoff_request.go`

```go
type HandoffHistoryEntry struct {
    AgentID string
    Output  map[string]any
}

type HandoffRequest struct {
    InputMessage map[string]any                    // 与 BaseTeam.Invoke/BaseAgent.Invoke 输入类型一致
    History      []HandoffHistoryEntry
    Session      *session.AgentTeamSession          // 始终是 team-level session
}

func (r *HandoffRequest) SessionID() string
```

**文件**：`interrupt.go`

```go
type TeamInterruptSignal struct {
    Result  map[string]any   // 必须包含 result_type="interrupt"
    Message string
}

func ExtractInterruptSignal(result map[string]any, err error) *TeamInterruptSignal
func FlushTeamSession(ctx context.Context, sess *session.AgentTeamSession) error
```

**中断检测两条路径**（与 Python 一致）：

1. `result["result_type"] == "interrupt"`：ReActAgent 返回中断时 err 为 nil，result 包含 interrupt 标记
2. `errors.As(err, &AgentInterrupt)`：直接抛异常的情况

### 3.5 ContainerAgent（核心包装器）

**文件**：`container_agent.go`

```go
const (
    HandoffRequestKey = "__handoff_request__"  // inputs map 中的键
    contextHistoryKey = "__handoff_ctx_history__"
    defaultContextID  = "default_context_id"
)

type ContainerAgent struct {
    teamruntime.CommunicableAgent              // 嵌入：获得 Send/Publish/Subscribe
    targetCard       *agentschema.AgentCard
    targetProvider   maschema.TeamAgentProvider
    allowedTargets   map[string]struct{}
    coordinatorLookup func(sessionID string) *HandoffOrchestrator
    resourceMgr      *resources_manager.ResourceMgr
    targetInstance   agentinterfaces.BaseAgent
    toolsInjected    bool
    mu               sync.Mutex
}
```

**ContainerAgent 实现 BaseAgent 接口**，可注册到 TeamRuntime。HandoffRequest 编码为 `map[string]any{HandoffRequestKey: req}` 传入 `Invoke`，内部断言提取。

**Invoke 完整流程**（与 Python `ContainerAgent.invoke` 一致）：

1. 从 inputs map 提取 `*HandoffRequest`
2. 获取 coordinator（通过 coordinatorLookup）
3. 懒初始化目标 Agent（`getTargetAgent`）
4. 注入 HandoffTool（`injectToolsOnce`：双层注册 ResourceMgr + AbilityManager）
5. 构建 Agent 输入（`buildAgentInput`：合并 handoff_history）
6. 调用目标 Agent：
   - 有 teamSession：`invokeTargetWithStream`（创建子 agentSession + 注入上下文历史 + 调用 + 保存上下文 + 流式转发）
   - 无 teamSession：直接调用（fallback）
7. 记录历史
8. 检查中断信号（`ExtractInterruptSignal(result, err)`）
9. 分支决策：
   - signal == nil → `coordinator.Complete(result)` 任务完成
   - signal != nil → `coordinator.RequestHandoff()` 审批后 Publish 到下一个 ContainerAgent 或强制 Complete

**所有私有方法与 Python 一一对应**：

| Go 方法 | Python 方法 | 说明 |
|---------|-------------|------|
| `getTargetAgent` | `_get_target_agent` | 懒初始化目标 Agent |
| `injectToolsOnce` | `_inject_tools_once` | 注入 HandoffTool（ResourceMgr + AbilityManager） |
| `buildAgentInput` | `_build_agent_input` | 合并 handoff_history 到输入 |
| `invokeTargetWithStream` | `_invoke_target_with_stream` | 调用 Agent + 流式转发 + 信号提取 |
| `saveAgentContext` | `_save_agent_context` | 持久化 Agent 上下文 |
| `saveContextToTeamSession` | `_save_context_to_team_session` | 保存上下文消息到 team session（去重） |
| `injectContextHistory` | `_inject_context_history` | 注入历史消息到 Agent session |
| `stripHandoffMessages` | `_strip_handoff_messages` | 去除 role=tool 和含 tool_calls 的消息 |
| `handleTeamInterrupt` | `_handle_team_interrupt` | 中断处理（保存状态 + flush + complete） |

### 3.6 HandoffTeam（顶层入口）

**文件**：`handoff_team.go`

```go
type HandoffTeam struct {
    card               maschema.TeamCardInterface
    config             HandoffTeamConfig
    runtime            *teamruntime.TeamRuntime
    agentProviders     map[string]maschema.TeamAgentProvider
    internalAgentsReady bool
    coordinatorRegistry map[string]*HandoffOrchestrator
    initLock           sync.Mutex
}
```

HandoffTeam 实现 `BaseTeam` 接口全部 13 个方法。

**核心方法与 Python 对应**：

| Go 方法 | Python 方法 | 说明 |
|---------|-------------|------|
| `AddAgent` | `add_agent` | 注册 Agent，标记 internalAgentsReady=false |
| `ensureInternalAgents` | `_ensure_internal_agents` | 为每个 Agent 创建 ContainerAgent + 端点注册 + 订阅 |
| `makeContainerProvider` | `_make_container_provider` | 创建 ContainerAgent provider 闭包 |
| `runChain` | `_run_chain` | 执行交接链路（创建协调器 → 发布 → 等待 → 清理） |
| `lookupCoordinator` | `_lookup_coordinator` | 查找会话协调器 |
| `getStartAgentID` | `_get_start_agent_id` | 获取起始 Agent ID |

**ensureInternalAgents 流程**（与 Python 一致）：

1. 构建 routeGraph = BuildRouteGraph(agentIDs, routes)
2. 对每个 agentID：
   - endpointID = `__handoff_ep_{teamID}_{agentID}`
   - endpointCard = AgentCard{ID: endpointID, Name: endpointID}
   - containerProvider = makeContainerProvider(card, agentID, allowedTargets)
   - runtime.RegisterAgent(endpointCard, containerProvider)
   - runtime.Subscribe(endpointID, `container_{agentID}`)

**runChain 流程**（与 Python `_run_chain` 一致）：

1. ensureInternalAgents()
2. coordinator = RestoreFromSession(session, startAgentID, agentIDs, config)
3. 恢复 history（isResume 时过滤中断项）
4. coordinatorRegistry[sessionID] = coordinator
5. runtime.Publish(HandoffRequest, `container_{currentAgentID}`)
6. 等待 coordinator.DoneCh()（带超时：select + time.After）
7. 清理：移除 coordinator、CleanupSession

## 4. 关联改动

### 4.1 BaseAgent.Invoke 返回类型统一

将 `BaseAgent.Invoke` 返回类型从 `(any, error)` 改为 `(map[string]any, error)`。

**生产代码（6 个文件）**：

| 文件 | 改动 |
|------|------|
| `single_agent/interfaces/interface.go` | 接口定义 |
| `single_agent/agents/react_invoke.go` | 实现 + 删除死代码类型断言 |
| `runner/runner.go` | RunAgent 返回类型 |
| `runner/child_runner.go` | ChildRunnerImpl.RunAgent 返回类型 |
| `runner/spawn/child.go` | ChildRunner.RunAgent 接口 |
| `multi_agent/team_runtime/message_router.go` | RouteP2PMessage 返回类型（MessageRouter 直接调用 runner.RunAgent，无 AgentExecutor） |

**测试代码（5 个文件）**：mockAgent/stubBaseAgent/fakeAgent Invoke 签名更新。

### 4.2 TeamRuntime.RegisterAgent 修复

当前 `RegisterAgent` 中 wrappedProvider 注册到 ResourceMgr 是 no-op placeholder（`_ = wrappedProvider`），需修复为真实注册，使 ContainerAgent 能通过标准路径被 MessageRouter 调用。

### 4.3 IMPLEMENTATION_PLAN.md

8.34 状态 `☐` → `✅`。

## 5. 新增文件汇总

| 生产文件 | 测试文件 |
|---------|---------|
| `teams/doc.go` | — |
| `teams/utils.go` | `teams/utils_test.go` |
| `handoff/doc.go` | — |
| `handoff/handoff_team.go` | `handoff/handoff_team_test.go` |
| `handoff/container_agent.go` | `handoff/container_agent_test.go` |
| `handoff/handoff_orchestrator.go` | `handoff/handoff_orchestrator_test.go` |
| `handoff/handoff_config.go` | `handoff/handoff_config_test.go` |
| `handoff/handoff_tool.go` | `handoff/handoff_tool_test.go` |
| `handoff/handoff_signal.go` | `handoff/handoff_signal_test.go` |
| `handoff/handoff_request.go` | `handoff/handoff_request_test.go` |
| `handoff/interrupt.go` | `handoff/interrupt_test.go` |

## 6. 组件关系图

```
HandoffTeam (BaseTeam)
  │
  ├─ ensureInternalAgents ───────────────────────────────┐
  │  对每个 agentID:                                      │
  │    ContainerAgent = New(targetCard, provider,         │
  │                         allowedTargets, coordLookup)  │
  │    runtime.RegisterAgent(endpointCard, containerProv) │
  │    runtime.Subscribe(endpointID, "container_{id}")    │
  │                                                       │
  ├─ runChain ─────────────────────────────────────────┐  │
  │  coordinator = NewOrchestrator(...)                 │  │
  │  runtime.Publish(HandoffRequest, topic)             │  │
  │  result = <-coordinator.DoneCh()                    │  │
  └────────────────────────────────────────────────────┘  │
                                                          │
         Publish ─────────────────────────────────────────┘
           │
           ▼
  ContainerAgent_A  ──handoff──▶  ContainerAgent_B
    ├─ CommunicableAgent            ├─ CommunicableAgent
    ├─ targetProvider               ├─ targetProvider
    └─ injectToolsOnce              └─ injectToolsOnce
       (transfer_to_B,                 (transfer_to_A,
        transfer_to_C)                  transfer_to_C)

  HandoffOrchestrator
    ├─ routeGraph: 邻接表
    ├─ doneCh: chan map[string]any (缓冲1)
    ├─ RequestHandoff → 检查 maxHandoffs/路由/终止条件
    ├─ Complete → doneOnce(ch<-result; close(ch))
    └─ Save/Restore to session

  HandoffTool              HandoffSignal            HandoffRequest
    "transfer_to_B"          ExtractHandoff           InputMessage
    Invoke → dict            Signal(result,           History
    {__handoff_to__}          agentSession)           Session
```

## 7. 设计决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| ContainerAgent 组合方式 | 嵌入 CommunicableAgent + 持有 BaseAgent 引用 | 与 Python 多重继承语义最接近 |
| Orchestrator 完成机制 | chan map[string]any + doneOnce | doneOnce 保证只发送一次，Close() 在 defer 中关闭 channel |
| ContainerAgent 执行入口 | 实现 BaseAgent 接口，HandoffRequest 编码为 map 的一个字段值 | ContainerAgent 可注册到 TeamRuntime，与 Python 一致 |
| HandoffRequest.InputMessage 类型 | map[string]any | 与 BaseTeam.Invoke/BaseAgent.Invoke 输入类型一致 |
| HandoffRequest.Session 类型 | *session.AgentTeamSession | Python 确认一定是 team-level session |
| HandoffTool 注册方式 | 双层：ResourceMgr + AbilityManager | 与 Python 一致 |
| 信号提取 | 两层：result dict + session 消息历史 | 与 Python 一致，Go session 存储结构与 Python 相同 |
| 中断检测 | 两条路径：result dict + errors.As | ReAct 返回中断时 err 为 nil，必须检查 result |
| BaseAgent.Invoke 返回类型 | (map[string]any, error) | 统一类型，与实际实现一致 |
| TerminationCondition | 同步 func | Python 支持 async，Go 用同步简化 |
| 文件组织 | 7 文件 1:1 对应 Python | 第一个具体团队模式，1:1 降低后续实现者认知负担 |
