# 8.36 HierarchicalTeam (tools) — Agents-as-Tools 层级团队设计

## 概述

实现 **Agents-as-Tools（工具委托）** 模式的层级多 Agent 团队。子 Agent 作为工具注册到父 Agent 的 `AbilityManager`，LLM 通过 `tool_call` 机制自主调度子 Agent，子 Agent 通过 `Runner.RunAgent()` 本地直接执行，无需消息总线。

对应 Python：`openjiuwen/core/multi_agent/teams/hierarchical_tools/`

## 流程位置

```
用户输入
  └→ HierarchicalToolsTeam.Invoke/Stream
       └→ setupHierarchy()                ← 将 pending 子 Agent 注册到父 Agent 的 ability_manager
       └→ root_agent.Invoke/Stream        ← 入口 Agent 执行 ReAct 循环
            └→ LLM 返回 tool_call(name="子Agent名", arguments={...})
                 └→ AbilityManager.Execute()
                      └→ executeSingleToolCall()
                           └→ IsAgent(name) == true
                                └→ AbilityManager.executeAgent()   ← 【8.36 核心路径】
                                     └→ Runner.RunAgent()          ← 本地直接调用子 Agent
                                          └→ 子 Agent ReAct 循环...
                                               └→ 返回结果 → 构建 ToolMessage
```

**作用**：
1. 支持多级树状层级编排（父→子→孙），任意 Agent 都可作为父节点
2. LLM 驱动调度：父 Agent 的 LLM 自主决定何时调用哪个子 Agent
3. 本地直接调用：子 Agent 通过 `AbilityManager.executeAgent() → Runner.RunAgent()` 执行，不经过消息总线
4. 更轻量：无需 SupervisorAgent、P2PAbilityManager、Semaphore 限流、P2P timeout

## 与 8.35 Msgbus 模式的核心差异

| 维度 | 8.36 Tools 模式 | 8.35 Msgbus 模式 |
|------|----------------|-----------------|
| 委托机制 | 子 Agent 注册到父 Agent 的 `AbilityManager`，LLM 通过 `tool_call` → `executeAgent()` → `Runner.RunAgent()` 本地调用 | 子 Agent 注册到 `P2PAbilityManager`，LLM 通过 `tool_call` → `executeSingleP2P()` → `supervisor.Send()` P2P 消息派发 |
| 层级建立 | `_pendingChildren` + `setupHierarchy()` 延迟注册 | 无需显式层级建立，所有子 Agent 注册到 SupervisorAgent 的 P2PAbilityManager |
| 配置入口 | `RootAgent *AgentCard`（根/入口 Agent） | `SupervisorAgent *AgentCard`（监督者 Agent） |
| AddAgent | 通过 `WithParentAgentID()` TeamOption 传递父子关系 | 无 parentAgentID |
| 特殊 Agent | 不需要 SupervisorAgent，任意 Agent 都可作为父节点 | 需要 SupervisorAgent (CommunicableAgent + ReActAgent 双重嵌入) |
| AbilityManager | 使用标准 `AbilityManager`，子 Agent 走 `_agents` 分支 | 使用 `P2PAbilityManager`，子 Agent 走 P2P Send |
| 并行限流 | 无（由 AbilityManager 原生处理） | Semaphore 限流 maxParallelSubAgents |
| stream 实现 | 直接调用 `agent.Stream()` 逐 chunk 转发 | 调用 `runtime.Send()` 获取完整 result，一次性 WriteStream |
| 超时 | 无 P2P timeout | 有 timeout（默认 1800s） |
| 额外组件 | 无 | SupervisorAgent + P2PAbilityManager |

## 架构设计

### 1. 配置 — HierarchicalToolsTeamConfig

```go
// HierarchicalToolsTeamConfig 工具委托层级团队配置。
//
// 对应 Python: HierarchicalTeamConfig (hierarchical_tools/hierarchical_config.py)
type HierarchicalToolsTeamConfig struct {
    // TeamConfig 嵌入基础团队配置
    TeamConfig maschema.TeamConfig
    // RootAgent 根/入口 Agent 卡片（必填）
    RootAgent *agentschema.AgentCard
}
```

### 2. 核心结构体 — HierarchicalToolsTeam

```go
// HierarchicalToolsTeam 工具委托层级多 Agent 团队。
//
// 子 Agent 通过 parentAgentID 注册到父 Agent 的 ability_manager，
// LLM 将子 Agent 视为可调用的工具（tool_call），
// 子 Agent 的执行由 AbilityManager.executeAgent() → Runner.RunAgent() 完成。
//
// 对应 Python: HierarchicalTeam (hierarchical_tools/hierarchical_team.py)
type HierarchicalToolsTeam struct {
    // card 团队身份卡片
    card maschema.TeamCardInterface
    // config 完整配置
    config HierarchicalToolsTeamConfig
    // runtime 团队运行时
    runtime *team_runtime.TeamRuntime
    // rootAgentID 根/入口 Agent ID
    rootAgentID string
    // pendingChildren 待注册的父子关系：parentID → []childAgentCard
    pendingChildren map[string][]*agentschema.AgentCard
    // hierarchySetup 标记层级是否已建立（幂等保护）
    hierarchySetup bool
}
```

### 3. 关键方法

#### 3.1 AddAgent — 通过 TeamOption 传递 parentAgentID

```go
func (t *HierarchicalToolsTeam) AddAgent(
    ctx context.Context,
    card *agentschema.AgentCard,
    provider maschema.TeamAgentProvider,
    opts ...maschema.TeamOption,
) error
```

- 从 `opts` 中解析 `ParentAgentID`
- 注册 Agent 到 runtime（与 msgbus 模式相同）
- 若 `parentAgentID` 非空，将 card 追加到 `pendingChildren[parentAgentID]`
- 识别 rootAgent，设置 P2P timeout（如果 runtime 需要）

#### 3.2 setupHierarchy — 延迟注册子 Agent 到父 Agent 的 AbilityManager

```go
func (t *HierarchicalToolsTeam) setupHierarchy(ctx context.Context) error
```

- 幂等：若 `hierarchySetup == true` 直接返回
- 遍历 `pendingChildren`：
  - 从 `ResourceMgr` 获取父 Agent 实例
  - 对每个子 AgentCard，调用 `parentAgent.AbilityManager().Add(childCard)`
  - 记录 Debug 日志
- 设置 `hierarchySetup = true`

#### 3.3 Invoke — 非流式调用

```go
func (t *HierarchicalToolsTeam) Invoke(
    ctx context.Context,
    inputs map[string]any,
    opts ...maschema.TeamOption,
) (any, error)
```

流程：
1. `assertReady()` — 校验 rootAgentID 非空且 runtime.HasAgent(rootAgentID)
2. `setupHierarchy(ctx)` — 建立层级关系
3. `StandaloneInvokeContext()` 管理会话
4. 在回调内部 `runtime.Send(ctx, inputs, rootAgentID, card.GetID())`

#### 3.4 Stream — 流式调用（关键差异点）

```go
func (t *HierarchicalToolsTeam) Stream(
    ctx context.Context,
    inputs map[string]any,
    opts ...maschema.TeamOption,
) (<-chan stream.Schema, error)
```

流程：
1. `assertReady()` — 校验
2. `setupHierarchy(ctx)` — 建立层级关系
3. `StandaloneStreamContext()` 管理会话
4. 在回调内部：
   - 从 `ResourceMgr` 获取 root_agent 实例
   - 构造带 `conversation_id` 和 `sender` 的 inputs
   - 调用 `agent.Stream(ctx, inputsWithSID)` 获取 `<-chan stream.Schema`
   - 逐 chunk 写入 `teamSession.WriteStream(ctx, chunk)`

**与 msgbus 模式的关键区别**：msgbus 走 `runtime.Send()` 等完整结果后一次性 `WriteStream`；tools 模式直接调用 `agent.Stream()` 逐 chunk 转发，提供真正的流式体验。

#### 3.5 assertReady

```go
func (t *HierarchicalToolsTeam) assertReady() error
```

- 校验 `rootAgentID` 非空
- 校验 `runtime.HasAgent(rootAgentID)`

### 4. TeamOptions 扩展

```go
// TeamOptions 新增字段
type TeamOptions struct {
    // ... 现有字段 ...
    // ParentAgentID 父 Agent ID，用于 HierarchicalToolsTeam 的层级注册
    ParentAgentID string
}

// WithParentAgentID 设置父 Agent ID。
//
// 用于 HierarchicalToolsTeam.AddAgent() 时声明父子关系：
//
//	team.AddAgent(ctx, childCard, childProvider,
//	    maschema.WithParentAgentID("parent_agent_id"),
//	)
func WithParentAgentID(parentID string) TeamOption {
    return func(o *TeamOptions) { o.ParentAgentID = parentID }
}
```

### 5. 其他 BaseTeam 方法

与 msgbus 模式完全相同，委托 runtime：
- `RemoveAgent` → `runtime.UnregisterAgent()`
- `Send` → `runtime.Send()`
- `Publish` → `runtime.Publish()`
- `Subscribe` → `runtime.Subscribe()`
- `Unsubscribe` → `runtime.Unsubscribe()`
- `Configure` → 更新嵌入的 TeamConfig
- `GetAgentCard` → `runtime.GetAgentCard()`
- `GetAgentCount` → `runtime.GetAgentCount()`
- `ListAgents` → `runtime.ListAgents()`
- `Card()` → 返回 card
- `Config()` → 返回 `&config.TeamConfig`
- `GetRuntime()` → 返回 runtime

## 包重命名：hierarchical → hierarchical_msgbus

将现有 `teams/hierarchical/` 重命名为 `teams/hierarchical_msgbus/`，与 Python 目录结构对齐。

变更范围：
1. 目录重命名：`teams/hierarchical/` → `teams/hierarchical_msgbus/`
2. 包声明：`package hierarchical` → `package hierarchical_msgbus`
3. doc.go 更新
4. teams/doc.go 更新（引用路径）
5. 目前无外部 import 引用（已确认），影响范围有限

## 文件清单

### 新增文件

| 文件 | 职责 |
|------|------|
| `teams/hierarchical_tools/doc.go` | 包文档 |
| `teams/hierarchical_tools/hierarchical_config.go` | HierarchicalToolsTeamConfig 配置定义 |
| `teams/hierarchical_tools/hierarchical_team.go` | HierarchicalToolsTeam 实现 BaseTeam 接口 |
| `teams/hierarchical_tools/hierarchical_config_test.go` | 配置测试 |
| `teams/hierarchical_tools/hierarchical_team_test.go` | 团队测试 |

### 修改文件

| 文件 | 变更 |
|------|------|
| `schema/team_interface.go` | TeamOptions 新增 `ParentAgentID` 字段 + `WithParentAgentID()` Option 函数 |
| `teams/hierarchical/` → `teams/hierarchical_msgbus/` | 目录重命名 + 包名修改 |
| `teams/doc.go` | 更新目录引用 |

### 回填

| 文件 | 变更 |
|------|------|
| `IMPLEMENTATION_PLAN.md` | 8.36 状态 ☐ → ✅ |

## 错误处理

- `assertReady()` 返回 `StatusAgentTeamExecutionError` 错误（与 msgbus 模式一致）
- `setupHierarchy()` 中父 Agent 未找到时返回错误
- `Stream()` 中 `agent.Stream()` 出错时，写入 error_result 到 teamSession（对齐 Python L169-172）

## 日志同步

对齐 Python 中 `logger.debug` 调用：
- 初始化时：记录 `team_id`、`root_agent_id`
- `AddAgent` 有 parentAgentID 时：记录 `child_id`、`parent_id`
- `setupHierarchy()` 注册时：记录 `child_id` → `parent_id.ability_manager`
- `Stream()` 出错时：记录 error

使用 `logger.ComponentChannel` 组件（与 hierarchical msgbus 一致）。

## 测试策略

- `hierarchical_config_test.go`：配置创建、默认值、RootAgent 必填
- `hierarchical_team_test.go`：
  - NewHierarchicalToolsTeam 创建
  - AddAgent 基本 + WithParentAgentID
  - assertReady 校验（rootAgentID 为空 / rootAgent 未注册）
  - setupHierarchy 幂等性
  - BaseTeam 接口满足（编译时验证）
  - Invoke / Stream 使用 mock AgentProvider
