# 8.35 HierarchicalTeam (msgbus) 设计文档

> 日期：2025-07-22
> 状态：设计完成，待实现
> 对应 Python：`openjiuwen/core/multi_agent/teams/hierarchical_msgbus/`
> 实现计划步骤：8.35

---

## 1. 概述

HierarchicalTeam (msgbus) 是**消息总线驱动的层级多 Agent 团队**，实现"监督者-执行者"编排模式。

与 HandoffTeam 的"交接链路"模式不同，HierarchicalTeam 采用"监督委派"模式：
- **SupervisorAgent**（监督者）通过 ReAct 循环推理，LLM 返回 tool_call 时自动派发给子 Agent
- 子 Agent 间通过 MessageBus 的 P2P 模式通信，支持并行派发（Semaphore 限流）
- 适用于"一个智能调度者 + 多个专业执行者"的场景（如：产品经理 + 前端 + 后端 + 测试）

### 执行流程

```
用户消息 → HierarchicalTeam.invoke()
  → runtime.Send(message, recipient=supervisor_id)  [P2P]
    → SupervisorAgent ReAct 循环
      → LLM 返回 tool_call(子Agent名, 参数)
        → P2PAbilityManager.Execute()
          → 分区: AgentCard调用 vs 普通工具调用
          → AgentCard: supervisor.Send(args, recipient=sub_agent_id)  [P2P]
          → 普通工具: AbilityManager.Execute()
      → LLM 得到最终答案
    → 返回结果
```

---

## 2. 前置重构：AbilityManager 接口化

### 2.1 动机

ReActAgent.abilityManager 是私有字段（`*ability.AbilityManager`），无法外部替换为 P2PAbilityManager。引入 AbilityManagerInterface 后：
- ReActAgent.abilityManager 改为接口类型字段
- 新增 SetAbilityManager() 方法
- SupervisorAgent 可在构造时注入 P2PAbilityManager

### 2.2 ExecuteResult 迁移

将 `ExecuteResult` 从 `ability/ability_types.go` 移到 `single_agent/schema/` 包。

**原因**：AbilityManagerInterface 定义在 `interfaces` 包中，Execute() 返回 `[]ExecuteResult`，如果 ExecuteResult 留在 ability 包，则 interfaces → ability 形成导入，与已有的 ability → resources_manager → interfaces 构成循环依赖。移到 schema 包后，interfaces → schema（已有导入），无循环。

**ExecuteResult 定义**（迁移后）：

```go
// single_agent/schema/execute_result.go

// ExecuteResult 单个工具调用的执行结果。
type ExecuteResult struct {
    // Result 执行结果。
    Result any
    // ToolMsg 返回给 LLM 的 ToolMessage
    ToolMsg *llmschema.ToolMessage
}
```

**AddAbilityResult 也迁移到 schema 包**：AbilityManagerInterface.Add() 返回 AddAbilityResult，如果留在 ability 包则 interfaces → ability 形成循环依赖。一并移到 schema 包。

**AbilityExecutionError 保留在 ability 包**：不在接口方法签名中使用，且仅在 ability 内部使用。

### 2.3 AbilityManagerInterface 定义

在 `single_agent/interfaces/interface.go` 中新增：

```go
// AbilityManagerInterface 能力管理器接口，Agent 通过此接口注册和调度能力。
//
// 对应 Python: AbilityManager 的公开方法集。
// 具体实现：ability.AbilityManager、P2PAbilityManager。
type AbilityManagerInterface interface {
    // Add 添加单个能力。
    Add(ability schema.Ability) agentschema.AddAbilityResult
    // AddMany 批量添加能力。
    AddMany(abilities []schema.Ability) []agentschema.AddAbilityResult
    // Remove 移除指定名称的能力。
    Remove(name string) schema.Ability
    // RemoveMany 批量移除能力。
    RemoveMany(names []string) []schema.Ability
    // Get 获取指定名称的能力。
    Get(name string) schema.Ability
    // List 列出所有已注册能力。
    List() []schema.Ability
    // ListToolInfo 列出工具信息供 LLM 使用。
    ListToolInfo(ctx context.Context, names []string, mcpServerName ...string) ([]*schema.ToolInfo, error)
    // Execute 执行工具调用。
    Execute(
        ctx context.Context,
        cbc *rail.AgentCallbackContext,
        toolCalls []*llmschema.ToolCall,
        sess sessioninterfaces.SessionFacade,
        tag string,
    ) []agentschema.ExecuteResult
    // SetContextEngine 设置上下文引擎。
    SetContextEngine(ce ceinterface.ContextEngine)
    // ReorderTools 重排工具顺序。
    ReorderTools(orderedNames []string)
}
```

**依赖方向验证**：
- `interfaces` → `single_agent/schema` ✅ 已有
- `interfaces` → `foundation/llm/schema` ✅ 已有
- `interfaces` → `session/interfaces` ✅ 已有
- `interfaces` → `context_engine/interface` ✅ 需新增（与已有导入不冲突）
- `interfaces` → `ability` ❌ 不需要
- **无循环依赖**

### 2.4 影响面

| 文件 | 变更 |
|------|------|
| `single_agent/schema/execute_result.go` | **新增**：ExecuteResult + AddAbilityResult 类型定义 |
| `single_agent/ability/ability_types.go` | 移除 ExecuteResult 和 AddAbilityResult，改为引用 schema 包 |
| `single_agent/interfaces/interface.go` | 新增 AbilityManagerInterface；BaseAgent.AbilityManager() 返回类型从 `any` 改为 `AbilityManagerInterface` |
| `single_agent/agents/react_agent.go` | abilityManager 字段类型从 `*ability.AbilityManager` 改为 `interfaces.AbilityManagerInterface` |
| `single_agent/agents/react_helpers.go` | getAbilityManager() 返回类型改为 `interfaces.AbilityManagerInterface`；新增 SetAbilityManager() 方法 |
| `single_agent/agents/react_prompt.go` | AbilityManager() 返回 `AbilityManagerInterface`（不再是 any） |
| `controller/controller.go` | abilityMgr 字段类型改为 `interfaces.AbilityManagerInterface` |
| `controller/modules/task_scheduler.go` | 同上 |
| `controller/modules/task_executor.go` | 同上 |
| `controller/modules/event_handler.go` | 同上 |
| `multi_agent/teams/handoff/container_agent.go` | 不再需要 `abilityMgrAny.(*ability.AbilityManager)` 类型断言，直接调用接口方法 |

---

## 3. CommunicableAgent 重构

### 3.1 移除 AgentID() 导出方法

**原因**：ReActAgent 和 CommunicableAgent 都有 `AgentID() string` 方法，Go 不允许双重嵌入时同名方法歧义。移除 CommunicableAgent.AgentID() 后，SupervisorAgent 可同时嵌入两者。

**影响**：
- `agentID` 私有字段保留——Send/Publish/Subscribe/Unsubscribe 内部使用
- 外部通过 `Card().ID` 或 `ReActAgent.AgentID()` 获取 AgentID
- `communicable_agent_test.go` 中对 AgentID() 的测试调整

### 3.2 保留 Runtime() 导出方法

P2PAbilityManager 构造时通过 SupervisorAgent 获取 runtime 的 P2PTimeout()。

---

## 4. SupervisorAgent

### 4.1 结构体

```go
// SupervisorAgent 默认内置监督者 Agent，组合 CommunicableAgent + ReActAgent。
//
// 通过 CommunicableAgent 获得 P2P/Pub-Sub 通信能力，
// 通过 ReActAgent 获得 ReAct 循环执行能力，
// 内部使用 P2PAbilityManager 将 AgentCard 类型的 tool_call 转为 P2P 消息派发。
//
// 对应 Python: SupervisorAgent(CommunicableAgent, ReActAgent)
type SupervisorAgent struct {
    team_runtime.CommunicableAgent  // 嵌入：Send/Publish/Subscribe/Unsubscribe/Runtime/BindRuntime
    agents.ReActAgent               // 嵌入：Invoke/Stream/Card/Configure/AgentID/...
}
```

### 4.2 满足的接口

| 接口 | 来源 |
|------|------|
| `BaseAgent` | ReActAgent 嵌入提升 |
| `schema.Communicable` | CommunicableAgent 嵌入提升（Send/Publish/Subscribe/Unsubscribe） |
| `RuntimeBindable` | CommunicableAgent 嵌入提升（BindRuntime） |

**无方法冲突**：CommunicableAgent.AgentID() 已移除。

### 4.3 构造函数

```go
// NewSupervisorAgent 创建 SupervisorAgent 实例。
func NewSupervisorAgent(
    card *agentschema.AgentCard,
    config *saconfig.ReActAgentConfig,
    maxParallelSubAgents int,
) *SupervisorAgent
```

内部流程：
1. `react := agents.NewReActAgent(card, config)` 创建 ReActAgent
2. `p2pAm := NewP2PAbilityManager(supervisor, maxParallelSubAgents, timeout)` 创建 P2PAbilityManager
3. `react.SetAbilityManager(p2pAm)` 注入替换
4. 组装 SupervisorAgent{CommunicableAgent: *NewCommunicableAgent(), ReActAgent: *react}

### 4.4 工厂方法 Create

```go
// Create 创建预加载子 Agent 卡片的 SupervisorAgent。
// 返回 (AgentCard, AgentProvider) 元组，兼容 HierarchicalTeam.AddAgent()。
func Create(
    agents []*agentschema.AgentCard,
    modelClientConfig *llmschema.ModelClientConfig,
    modelRequestConfig *llmschema.ModelRequestConfig,
    agentCard *agentschema.AgentCard,
    systemPrompt string,
    maxIterations int,
    maxParallelSubAgents int,
    timeout float64,
) (*agentschema.AgentCard, resources_manager.AgentProvider)
```

内部流程：
1. 校验 agents 非空且均为 AgentCard 类型
2. 返回 `(agentCard, _provider)`，`_provider` 是懒构造闭包
3. 闭包内：构造 ReActAgentConfig → NewSupervisorAgent → registerSubAgentCard 循环注册

### 4.5 子 Agent 注册

```go
// RegisterSubAgentCard 将子 Agent 卡片注册到 P2PAbilityManager。
// 使 LLM 可将子 Agent 视为可调用的工具。
func (s *SupervisorAgent) RegisterSubAgentCard(card *agentschema.AgentCard)
```

委托给 `P2PAbilityManager.Add(card)`。

---

## 5. P2PAbilityManager

### 5.1 结构体

```go
// P2PAbilityManager 层级团队的 P2P 能力管理器。
//
// 拦截 AgentCard 类型的 tool_call，通过 supervisor.Send() 做 P2P 派发，
// 其他能力类型转发给嵌入的 AbilityManager 执行。
// AgentCard 派发受 maxParallel 限流。
//
// 对应 Python: P2PAbilityManager(AbilityManager)
type P2PAbilityManager struct {
    ability.AbilityManager           // 嵌入：Add/Remove/Get/List/ListToolInfo 等
    supervisor  *SupervisorAgent     // 持有：用于 P2P send
    maxParallel int                  // 最大并行子 Agent 派发数
    timeout     float64             // P2P 超时秒数（构造时传入）
    sem         chan struct{}        // 限流信号量（懒初始化）
    semOnce     sync.Once           // 信号量初始化同步原语
}
```

### 5.2 Execute() 覆写

对齐 AbilityManager 已有的并行模式（sync.WaitGroup + 预分配 results slice + 按索引写入）：

```go
func (m *P2PAbilityManager) Execute(
    ctx context.Context,
    cbc *rail.AgentCallbackContext,
    toolCalls []*llmschema.ToolCall,
    sess sessioninterfaces.SessionFacade,
    tag string,
) []agentschema.ExecuteResult
```

流程：
1. `_normalizeToolCalls(toolCalls)` 规范化
2. **分区**：
   - `agentIndices` = 索引列表，其中 `toolCalls[i].Name` 在 `_agents` map 中
   - `otherIndices` = 其余索引
3. **Fast path**：无 Agent 调用时，委托 `embedded AbilityManager.Execute()`
4. **并行执行**：
   - 懒初始化 semaphore（`chan struct{}` 容量=maxParallel）
   - 预分配 `results := make([]ExecuteResult, len(toolCalls))`
   - Agent 调用：goroutine 内 `sem <- struct{}{}` 获取令牌 → `executeSingleP2P()` → `<-sem` 释放
   - Other 调用：委托 `embedded AbilityManager.Execute()`，结果按索引写入
   - `sync.WaitGroup` 等待所有 goroutine 完成
5. **结果重组**：按原 toolCalls 索引顺序

### 5.3 executeSingleP2P()

```go
func (m *P2PAbilityManager) executeSingleP2P(
    ctx context.Context,
    toolCall *llmschema.ToolCall,
    sess sessioninterfaces.SessionFacade,
) (agentschema.ExecuteResult, error)
```

流程：
1. 解析 toolCall.Arguments 为 `map[string]any`
2. 从 session 获取 sessionID
3. `m.supervisor.Send(ctx, toolArgs, agentCard.ID)` — P2P 消息
4. 成功：返回 `(result, ToolMessage{content: str(result), tool_call_id: toolCall.ID})`
5. 失败：返回 error，记录 Error 日志

---

## 6. HierarchicalTeam

### 6.1 结构体

```go
// HierarchicalTeam 消息总线驱动的层级多 Agent 团队。
//
// 通过 SupervisorAgent 驱动 LLM 决策，自动将子 Agent 任务派发给团队中的其他 Agent。
// 适用于"一个智能调度者 + 多个专业执行者"的场景。
//
// 对应 Python: HierarchicalTeam (hierarchical_msgbus/hierarchical_team.py)
type HierarchicalTeam struct {
    // card 团队身份卡片
    card maschema.TeamCardInterface
    // config 完整配置
    config HierarchicalTeamConfig
    // runtime 团队运行时
    runtime *team_runtime.TeamRuntime
    // supervisorID 监督者 Agent ID
    supervisorID string
}
```

### 6.2 HierarchicalTeamConfig

```go
// HierarchicalTeamConfig 层级团队（消息总线模式）配置。
//
// 对应 Python: HierarchicalTeamConfig (hierarchical_msgbus/hierarchical_config.py)
type HierarchicalTeamConfig struct {
    maschema.TeamConfig              // 嵌入基础团队配置
    SupervisorAgent *agentschema.AgentCard  // 监督者 Agent 卡片（必填）
    Timeout         float64                // P2P 通信超时秒数，默认 1800.0
}
```

### 6.3 Invoke

```go
func (t *HierarchicalTeam) Invoke(
    ctx context.Context,
    inputs map[string]any,
    opts ...maschema.TeamOption,
) (any, error)
```

流程：
1. `t.assertReady()` — 校验 supervisor 已注册
2. 提取 opts 中的 timeout，未指定则使用 config.Timeout
3. `teams.StandaloneInvokeContext(ctx, t.runtime, t.card, inputs, sess, fn)`
4. `fn(teamSession, sessionID)` 内：
   - `t.runtime.Send(ctx, inputs, t.supervisorID, t.card.GetID(), WithTeamSessionID(sessionID), WithTimeout(timeout))`

### 6.4 Stream

```go
func (t *HierarchicalTeam) Stream(
    ctx context.Context,
    inputs map[string]any,
    opts ...maschema.TeamOption,
) (<-chan stream.Schema, error)
```

流程：
1. `t.assertReady()`
2. 提取 timeout
3. `teams.StandaloneStreamContext(ctx, t.runtime, t.card, inputs, sess, runFn)`
4. `runFn(teamSession, sessionID)` 内：
   - `result := t.runtime.Send(ctx, inputs, t.supervisorID, ...)`
   - `teamSession.WriteStream({"output": result})`

### 6.5 AddAgent

```go
func (t *HierarchicalTeam) AddAgent(
    ctx context.Context,
    card *agentschema.AgentCard,
    provider maschema.TeamAgentProvider,
) error
```

流程：
1. 注册到 runtime（委托 `t.runtime.RegisterAgent()`）
2. 如果 `card.ID == t.supervisorID`，设置 `t.runtime.SetP2PTimeout(t.config.Timeout)`

### 6.6 其他 BaseTeam 方法

与 HandoffTeam 一致，委托 runtime：
- `RemoveAgent` → `runtime.UnregisterAgent()`
- `Send` → `runtime.Send()`
- `Publish` → `runtime.Publish()`
- `Subscribe` → `runtime.Subscribe()`
- `Unsubscribe` → `runtime.Unsubscribe()`
- `Configure` → 更新 `config.TeamConfig`
- `GetAgentCard` → `runtime.GetAgentCard()`
- `GetAgentCount` → `runtime.GetAgentCount()`
- `ListAgents` → `runtime.ListAgents()`
- `Card` → 返回 `card`
- `Config` → 返回 `&config.TeamConfig`

---

## 7. 与 HandoffTeam 的对比

| 特性 | HandoffTeam (8.34) | HierarchicalTeam msgbus (8.35) |
|------|-------------------|-------------------------------|
| 编排模式 | 交接链路：A → B → C，预定义路由 | 监督委派：Supervisor LLM 动态选择子 Agent |
| 决策方式 | HandoffRoute + ContainerAgent 提取交接信号 | Supervisor ReAct 循环，LLM tool_call 触发派发 |
| 通信方式 | Pub-Sub 发布到容器主题 | P2P send 到子 Agent（request-response） |
| 并行支持 | 串行交接 | 并行派发（Semaphore 限流） |
| 会话管理 | 自己管理（后续改用 StandaloneInvokeContext） | 复用 StandaloneInvokeContext/StandaloneStreamContext |
| 核心组件 | ContainerAgent + HandoffOrchestrator + HandoffTool | SupervisorAgent + P2PAbilityManager |
| 特殊 Agent | ContainerAgent（包装器） | SupervisorAgent（监督者，双重嵌入） |

---

## 8. 文件组织

```
teams/hierarchical/
├── doc.go                        # 包文档
├── hierarchical_team.go          # HierarchicalTeam 实现 BaseTeam
├── hierarchical_team_test.go     # 测试
├── hierarchical_config.go        # HierarchicalTeamConfig
├── hierarchical_config_test.go   # 测试
├── supervisor_agent.go           # SupervisorAgent
├── supervisor_agent_test.go      # 测试
├── p2p_ability_manager.go        # P2PAbilityManager
└── p2p_ability_manager_test.go   # 测试
```

对应 Python 文件映射：

| Go 文件 | Python 文件 |
|---------|------------|
| hierarchical_team.go | hierarchical_msgbus/hierarchical_team.py |
| hierarchical_config.go | hierarchical_msgbus/hierarchical_config.py |
| supervisor_agent.go | hierarchical_msgbus/supervisor_agent.py |
| p2p_ability_manager.go | hierarchical_msgbus/p2p_ability_manager.py |

---

## 9. 回填内容

| 回填位置 | 变更 |
|----------|------|
| `teams/doc.go` | 文件目录添加 `hierarchical/` 子目录条目 |
| `IMPLEMENTATION_PLAN.md` 8.35 | ☐ → 🔄 |
| `single_agent/ability/doc.go` | 如有引用 ExecuteResult，更新说明 |

---

## 10. 测试策略

### 10.1 P2PAbilityManager 测试

- `TestP2PAbilityManager_Execute_无Agent调用` — fast path 委托基类
- `TestP2PAbilityManager_Execute_纯Agent调用` — 全部走 P2P
- `TestP2PAbilityManager_Execute_混合调用` — Agent + 普通工具并行
- `TestP2PAbilityManager_Execute_并行限流` — 验证 Semaphore 限流
- `TestP2PAbilityManager_Execute_异常处理` — P2P 派发失败时返回 error ToolMessage
- `TestP2PAbilityManager_executeSingleP2P_成功` — 正常 P2P 派发
- `TestP2PAbilityManager_executeSingleP2P_失败` — Send 返回错误

### 10.2 SupervisorAgent 测试

- `TestNewSupervisorAgent` — 构造验证
- `TestSupervisorAgent_Create` — 工厂方法
- `TestSupervisorAgent_Create_空Agents` — 空列表报错
- `TestSupervisorAgent_RegisterSubAgentCard` — 子 Agent 注册
- `TestSupervisorAgent_满足BaseAgent接口` — 编译时接口检查
- `TestSupervisorAgent_满足Communicable接口` — 编译时接口检查
- `TestSupervisorAgent_满足RuntimeBindable接口` — 编译时接口检查

### 10.3 HierarchicalTeam 测试

- `TestNewHierarchicalTeam` — 构造验证
- `TestHierarchicalTeam_Invoke` — 正常调用
- `TestHierarchicalTeam_Invoke_Supervisor未注册` — assertReady 报错
- `TestHierarchicalTeam_Invoke_带Session` — 外部会话
- `TestHierarchicalTeam_Stream` — 流式调用
- `TestHierarchicalTeam_AddAgent` — Agent 注册 + Supervisor 识别
- `TestHierarchicalTeam_AddAgent_设置Timeout` — Supervisor 注册时设置 P2P timeout
- `TestHierarchicalTeam_RemoveAgent` — Agent 注销
- `TestHierarchicalTeam_满足BaseTeam接口` — 编译时接口检查

### 10.4 接口化重构测试

- 现有 AbilityManager/ReActAgent/Controller/ContainerAgent 测试应全部通过
- `TestBaseAgent_AbilityManager_返回接口类型` — 验证返回 AbilityManagerInterface

---

## 11. 日志同步

对照 Python 日志，在 Go 等价位置补充日志：

| Python 位置 | Go 位置 | 日志内容 |
|-------------|---------|---------|
| `HierarchicalTeam.add_agent` info | `AddAgent()` | supervisor 注册 + team_id |
| `HierarchicalTeam.invoke` debug | `Invoke()` | session_id + supervisor |
| `HierarchicalTeam.stream` debug | `Stream()` | supervisor |
| `SupervisorAgent.create` info | `Create()` | supervisor id + sub_agents + max_parallel |
| `SupervisorAgent.create` debug | `Create()` 闭包内 | 注册 sub-agent card id |
| `SupervisorAgent.register_sub_agent_card` debug | `RegisterSubAgentCard()` | card name + id |
| `P2PAbilityManager.execute` debug | `Execute()` | agent_calls 数 + other_calls 数 + max_parallel |
| `P2PAbilityManager._execute_single_tool_call` debug | `executeSingleP2P()` | tool_name + agent_id + session_id + timeout |
| `P2PAbilityManager._execute_single_tool_call` warning | `executeSingleP2P()` | P2P dispatch failed |
