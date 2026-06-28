# 6.25 Runner 单例 + ReActAgent 接口满足性改造设计

## 1. 概述

实现计划 6.25「Runner 单例」，提供 `RunAgent/RunAgentStreaming/RunWorkflow/RunWorkflowStreaming/SpawnAgent/SpawnAgentStreaming` 全局编排入口，以及配套的生命周期管理和资源访问方法。

同时，为了使 `ReActAgent` 满足 `interfaces.BaseAgent` 接口（Runner 依赖此接口），需要删除 `single_agent.BaseAgent` 结构体，将字段直接内嵌到 ReActAgent 中。

对应 Python 源码：`openjiuwen/core/runner/runner.py`

## 2. 设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 单例模式 | 全局 Runner 结构体 + 包级函数代理 | 可整体替换 Runner 实例（测试注入），保留包级函数便利性 |
| 回填策略 | 全量回填 + 新增缺失函数 | 一步到位，未实现依赖定义函数+注释标记回填 |
| Runner 字段 | 与 Python 完全对齐 | 未实现的用指针字段+注释标注 |
| Start/Stop | 完整实现已实现部分 | Checkpointer+MQ 已实现可直接做，TaskGroup 等标记回填 |
| Agent/Workflow 引用 | AgentRef / WorkflowRef 结构体 | 语义清晰，编译期安全，与 Python 的 str\|BaseAgent 对应 |
| BaseAgent 结构体 | 删除，字段直接内嵌到每个子类 | 消除 ReActAgent 不满足接口的根因 |
| 子类共享机制 | 每个子类独立持有字段和方法 | 代码有重复但类型清晰，无隐式耦合 |
| 函数签名 | 对齐 Python 完整参数 | 保持与 Python 一一对应，便于对照维护 |
| 未实现组件 | 定义函数/分支+回填注释 | 不引入编译错误，保持代码可编译 |

## 3. Runner 结构体设计

### 3.1 结构体定义

```go
type Runner struct {
    // runnerID Runner 唯一标识（对齐 Python _runner_id）
    runnerID string
    // resourceMgr 全局资源注册表（对齐 Python _resource_manager）
    resourceMgr *resourcesmanager.ResourceMgr
    // messageQueue 本地消息队列（对齐 Python _message_queue）
    messageQueue *messagequeue.MessageQueueInMemory
    // callbackFramework 异步回调框架（对齐 Python _callback_framework）
    callbackFramework *callback.AsyncCallbackFramework
    // rootTaskGroup 根任务组（对齐 Python _root_task_group）
    // ⤵️ 预留：任务组实现后回填
    rootTaskGroup any
    // teamRuntimeManager Team 运行时管理器（对齐 Python _team_runtime_manager）
    // ⤵️ 预留：TeamRunner（9.85）实现后回填
    teamRuntimeManager any
    // distributeMessageQueue 分布式消息队列（对齐 Python _distribute_message_queue）
    // ⤵️ 预留：分布式模式实现后回填
    distributeMessageQueue any
    // systemReplySub 系统回复订阅（对齐 Python system_reply_sub）
    // ⤵️ 预留：分布式模式实现后回填
    systemReplySub any
}
```

### 3.2 全局实例

```go
var (
    globalRunner *Runner
    runnerOnce   sync.Once
)
```

初始化函数 `initRunner()` 创建默认实例，包级函数代理到 `globalRunner`。

### 3.3 Runner 常量

```go
const (
    defaultRunnerID        = "global"
    defaultAgentSessionID  = "default_session"
    agentConversationIDKey = "conversation_id"
)
```

## 4. 包级函数完整列表

### 4.1 生命周期

| 包级函数 | Python 对应 | 签名 |
|---------|------------|------|
| `Start` | `Runner.start()` | `func Start(ctx context.Context) error` |
| `Stop` | `Runner.stop()` | `func Stop(ctx context.Context) error` |

### 4.2 Agent 执行

| 包级函数 | Python 对应 | 签名 |
|---------|------------|------|
| `RunAgent` | `Runner.run_agent()` | `func RunAgent(ctx, AgentRef, inputs, session, modelCtx, envs) (any, error)` |
| `RunAgentStreaming` | `Runner.run_agent_streaming()` | `func RunAgentStreaming(ctx, AgentRef, inputs, session, modelCtx, streamModes, envs) (<-chan stream.Schema, error)` |

### 4.3 Workflow 执行

| 包级函数 | Python 对应 | 签名 |
|---------|------------|------|
| `RunWorkflow` | `Runner.run_workflow()` | `func RunWorkflow(ctx, WorkflowRef, inputs, session, modelCtx, envs) (any, error)` |
| `RunWorkflowStreaming` | `Runner.run_workflow_streaming()` | `func RunWorkflowStreaming(ctx, WorkflowRef, inputs, session, modelCtx, streamModes, envs) (<-chan stream.Schema, error)` |

### 4.4 Spawn

| 包级函数 | Python 对应 | 签名 |
|---------|------------|------|
| `SpawnAgent` | `Runner.spawn_agent()` | 定义+回填注释（依赖 6.28） |
| `SpawnAgentStreaming` | `Runner.spawn_agent_streaming()` | 定义+回填注释（依赖 6.28） |

### 4.5 其他

| 包级函数 | Python 对应 | 签名 |
|---------|------------|------|
| `Release` | `Runner.release()` | `func Release(ctx, sessionID, force) error` |
| `SetConfig` / `GetConfig` | `Runner.set_config()` / `Runner.get_config()` | 委托到 config/global.go |
| `GetResourceMgr` | `Runner.resource_mgr` | 属性代理 |
| `GetPubSub` | `Runner.pubsub` | 属性代理 |
| `GetCallbackFramework` | `Runner.callback_framework` | 属性代理 |

## 5. AgentRef / WorkflowRef 设计

```go
// AgentRef Agent 引用，支持按 ID 查找或直接传入实例。
// 对齐 Python: agent: str | BaseAgent | LegacyBaseAgent
type AgentRef struct {
    id    string
    agent interfaces.BaseAgent
}

func ByAgentID(id string) AgentRef  // 按 ID 查找
func ByAgent(agent interfaces.BaseAgent) AgentRef  // 直接传入实例
func (r AgentRef) IsByID() bool
func (r AgentRef) IsByInstance() bool
func (r AgentRef) ID() string
func (r AgentRef) Agent() interfaces.BaseAgent

// WorkflowRef 工作流引用，支持按 ID 查找或直接传入实例。
// 对齐 Python: workflow: str | Workflow
type WorkflowRef struct {
    id       string
    workflow interfaces.Workflow
}

func ByWorkflowID(id string) WorkflowRef
func ByWorkflow(wf interfaces.Workflow) WorkflowRef
func (r WorkflowRef) IsByID() bool
func (r WorkflowRef) IsByInstance() bool
func (r WorkflowRef) ID() string
func (r WorkflowRef) Workflow() interfaces.Workflow
```

## 6. _prepareAgent / _prepareWorkflow 实现步骤

### 6.1 _prepareAgent（对齐 Python L502-530）

```
输入: ctx, AgentRef, inputs, session
输出: (interfaces.BaseAgent, sessioninterfaces.SessionFacade, error)

步骤（严格对齐 Python runner.py L502-530）：

1. 如果 session 是 AgentSession 类型（对齐 Python L504: isinstance(session, AgentSession)）:
   a. 如果 AgentRef 是 ByID（对齐 Python L505: isinstance(agent, str)）:
      - 从 ResourceMgr 获取 agent 实例（对齐 Python L506: await self._resource_manager.get_agent(agent_id=agent)）
      - 校验 agent 不为 nil（对齐 Python L507-508）
      - 调用 session.PreRun（对齐 Python L509: await session.pre_run(inputs=inputs)）
      - 返回 (agent_instance, session)
   b. 如果 AgentRef 是 ByInstance（对齐 Python L511）:
      - 调用 session.PreRun（对齐 Python L511: await session.pre_run(inputs=inputs)）
      - 返回 (agent, session)

2. 如果 session 不是 AgentSession（对齐 Python L513-530）:
   a. 解析 sessionID（对齐 Python L513-514: session_id = inputs.get(conversation_id, ...)）
   b. 如果 AgentRef 是 ByID（对齐 Python L515）:
      - 从 ResourceMgr 获取 agent 实例（对齐 Python L516）
      - 校验 agent 不为 nil（对齐 Python L517-518）
      - 判断是否远程 Agent（对齐 Python L519: if self._is_remote_agent）:
        - ⤵️ 预留：远程 Agent 支持
      - 创建 AgentSession（对齐 Python L524: agent_session = self._create_agent_session(...)）
      - 调用 agentSession.PreRun（对齐 Python L525: await agent_session.pre_run(inputs=inputs)）
      - 返回 (agent_instance, agent_session)
   c. 如果 AgentRef 是 ByInstance（对齐 Python L528-530）:
      - 创建 AgentSession（对齐 Python L528）
      - 调用 agentSession.PreRun（对齐 Python L529）
      - 返回 (agent, agent_session)
```

### 6.2 _prepareWorkflow（对齐 Python L642-655）

```
输入: ctx, WorkflowRef, session
输出: (interfaces.Workflow, *session.WorkflowSession, error)

步骤（严格对齐 Python runner.py L642-655）：

1. 解析 workflow key（对齐 Python L643-648）:
   a. 如果 WorkflowRef 是 ByID → workflowKey = id
   b. 如果 WorkflowRef 是 ByInstance → workflowKey = generateWorkflowKey(wf.Card().ID, wf.Card().Version)

2. 创建 workflow session（对齐 Python L649: workflow_session = self._create_workflow_session(session)）

3. 获取 workflow 实例（对齐 Python L650-654）:
   a. 如果 WorkflowRef 是 ByID → 从 ResourceMgr 获取（对齐 Python L651-652）
   b. 如果 WorkflowRef 是 ByInstance → 直接使用

4. 返回 (workflow_instance, workflow_session)
```

## 7. RunAgent / RunAgentStreaming 实现步骤

### 7.1 RunAgent（对齐 Python L399-427）

```
步骤（严格对齐 Python runner.py L399-427）：

1. 进入任务组作用域（对齐 Python L417: with self._root_task_group_scope()）
   ⤵️ 预留：任务组作用域

2. _prepareAgent → 获取 agent 实例和 session（对齐 Python L418）

3. 判断是否远程 Agent（对齐 Python L419-420: if _is_remote_agent）
   ⤵️ 预留：远程 Agent 支持（依赖 RemoteAgent 实现）

4. 判断是否 LegacyBaseAgent（对齐 Python L421-423: elif isinstance(agent_instance, LegacyBaseAgent)）
   ⤵️ 预留：LegacyBaseAgent 兼容（依赖 LegacyBaseAgent 实现）

5. 正常 Agent 调用（对齐 Python L425: res = await agent_instance.invoke(inputs, agent_session)）
   result, err := agentInstance.Invoke(ctx, inputs, WithSession(agentSession))

6. PostRun 清理（对齐 Python L426: await agent_session.post_run()）
   agentSession.PostRun(ctx)

7. 返回 result
```

### 7.2 RunAgentStreaming（对齐 Python L429-463）

```
步骤（严格对齐 Python runner.py L429-463）：

1. 进入任务组上下文（对齐 Python L448: token = self._enter_root_task_group_context()）
   ⤵️ 预留：任务组上下文

2. _prepareAgent → 获取 agent 实例和 session（对齐 Python L450）

3. 判断是否远程 Agent（对齐 Python L451-453）
   ⤵️ 预留：远程 Agent 流式支持

4. 判断是否 LegacyBaseAgent（对齐 Python L454-457）
   ⤵️ 预留：LegacyBaseAgent 流式兼容

5. 正常 Agent 流式调用（对齐 Python L459-460: async for chunk in agent_instance.stream(inputs, session=agent_session)）
   ch, err := agentInstance.Stream(ctx, inputs, WithSession(agentSession), WithStreamModes(streamModes))

6. PostRun 清理（对齐 Python L461: await agent_session.post_run()）
   在 goroutine 中等待流完成后调用 agentSession.PostRun(ctx)

7. 退出任务组上下文（对齐 Python L463: self._exit_root_task_group_context(token)）
   ⤵️ 预留：任务组上下文退出

8. 返回 channel
```

## 8. RunWorkflow / RunWorkflowStreaming 实现步骤

### 8.1 RunWorkflow（对齐 Python L350-369）

```
步骤（严格对齐 Python runner.py L350-369）：

1. 进入任务组作用域（对齐 Python L367: with self._root_task_group_scope()）
   ⤵️ 预留：任务组作用域

2. _prepareWorkflow → 获取 workflow 实例和 session（对齐 Python L368）

3. 调用 workflow.Invoke（对齐 Python L369: workflow_instance.invoke(inputs, session=workflow_session, context=context)）
   result, err := workflowInstance.Invoke(ctx, inputs, WithWorkflowSession(workflowSession), WithWorkflowContext(modelCtx))

4. 返回 result
```

### 8.2 RunWorkflowStreaming（对齐 Python L371-397）

```
步骤（严格对齐 Python runner.py L371-397）：

1. 进入任务组上下文（对齐 Python L390: token = self._enter_root_task_group_context()）
   ⤵️ 预留：任务组上下文

2. _prepareWorkflow → 获取 workflow 实例和 session（对齐 Python L392）

3. 调用 workflow.Stream（对齐 Python L393-395）
   ch, err := workflowInstance.Stream(ctx, inputs, WithWorkflowSession(workflowSession), WithWorkflowContext(modelCtx), WithStreamModes(streamModes))

4. 退出任务组上下文（对齐 Python L397: self._exit_root_task_group_context(token)）
   ⤵️ 预留：任务组上下文退出

5. 返回 channel
```

## 9. Start / Stop 实现步骤

### 9.1 Start（对齐 Python L267-322）

```
步骤（严格对齐 Python runner.py L267-322）：

1. 确保根任务组（对齐 Python L271: await self._ensure_root_task_group()）
   ⤵️ 预留：任务组初始化

2. 进入任务组作用域（对齐 Python L274: with self._root_task_group_scope()）
   ⤵️ 预留：任务组作用域

3. 初始化 Checkpointer（对齐 Python L277-302）
   - 读取 checkpointer 配置
   - 调用 CheckpointerFactory.Create
   - 设置默认 Checkpointer
   - ⤵️ 预留：Redis checkpointer 懒加载

4. 启动分布式消息队列（对齐 Python L304-312）
   ⤵️ 预留：分布式模式

5. 启动本地消息队列（对齐 Python L312: result = await self._message_queue.start()）

6. 返回成功
```

### 9.2 Stop（对齐 Python L324-348）

```
步骤（严格对齐 Python runner.py L324-348）：

1. 进入任务组作用域（对齐 Python L328: with self._root_task_group_scope()）
   ⤵️ 预留：任务组作用域

2. 停止分布式组件（对齐 Python L329-337）
   ⤵️ 预留：分布式模式

3. 停止本地消息队列（对齐 Python L339: result = await self._message_queue.stop()）

4. 释放资源管理器（对齐 Python L347: await self._resource_manager.release()）

5. 关闭根任务组（对齐 Python L348: await self._close_root_task_group()）
   ⤵️ 预留：任务组关闭
```

## 10. Release 实现步骤（对齐 Python L465-483）

```
步骤（严格对齐 Python runner.py L465-483）：

1. 尝试释放 Team 会话（对齐 Python L481: if await self._maybe_release_team_session(...)）
   ⤵️ 预留：Team 会话释放（依赖 9.85 TeamRunner 实现）

2. 获取 Checkpointer 并释放（对齐 Python L483: await CheckpointerFactory.get_checkpointer().release(session_id)）
```

## 11. SpawnAgent / SpawnAgentStreaming

定义函数签名，内部全部标记回填注释：

```go
// SpawnAgent 启动子进程运行 Agent。
// 对齐 Python: Runner.spawn_agent() (runner.py L532-576)
// ⤵️ 预留：Spawn 子进程（依赖 6.28 SpawnedProcessHandle 实现）
func SpawnAgent(ctx context.Context, agentConfig SpawnAgentConfig, inputs map[string]any, ...) (*SpawnedProcessHandle, error) {
    // ⤵️ 预留：Spawn 子进程实现
    return nil, fmt.Errorf("spawn not implemented: depends on 6.28")
}

// SpawnAgentStreaming 启动子进程运行 Agent（流式）。
// 对齐 Python: Runner.spawn_agent_streaming() (runner.py L578-640)
// ⤵️ 预留：Spawn 子进程流式（依赖 6.28 SpawnedProcessHandle 实现）
func SpawnAgentStreaming(ctx context.Context, agentConfig SpawnAgentConfig, inputs map[string]any, ...) (<-chan SpawnStreamChunk, error) {
    // ⤵️ 预留：Spawn 子进程流式实现
    return nil, fmt.Errorf("spawn streaming not implemented: depends on 6.28")
}
```

## 12. ReActAgent 改造（删除 BaseAgent 结构体）

### 12.1 删除文件

- `internal/agentcore/single_agent/base.go` — 删除
- `internal/agentcore/single_agent/base_test.go` — 删除（迁移 2 个无关测试）

### 12.2 迁移无关测试

从 `base_test.go` 迁移到其他文件：
- `TestGlobalAgentEventType_事件名对齐Python` → 迁移到 `rail/` 或 `callback/` 包
- `TestBaseError_StatusAgentNotConfigured` → 迁移到 `common/exception/` 包

### 12.3 ReActAgent 新增字段

```go
type ReActAgent struct {
    // ── 原 BaseAgent 字段直接内嵌 ──
    card            *agentschema.AgentCard         // 对齐 Python BaseAgent.card
    abilityManager  *ability.AbilityManager        // 对齐 Python BaseAgent._ability_manager
    callbackManager *rail.AgentCallbackManager      // 对齐 Python BaseAgent._agent_callback_manager

    // ── ReActAgent 自有字段 ──
    config          *saconfig.ReActAgentConfig     // ReActAgent 专用配置
    contextEngine   ceinterface.ContextEngine
    llm             *llm.Model
    promptBuilder   *SystemPromptBuilder
    llmOnce         sync.Once
    kvReleaseWarningLogged bool
    hitlHandler     *interrupt.ToolInterruptHandler
    skillUtil       *skills.SkillUtil
}
```

### 12.4 ReActAgent 新增/修改方法

| 方法 | 当前状态 | 改造方式 |
|------|---------|---------|
| `Card() *AgentCard` | 缺失 | 新增，返回 `a.card` |
| `Config() interfaces.AgentConfig` | 缺失 | 新增，返回 `a.config`（ReActAgentConfig 实现了 AgentConfig 接口） |
| `AbilityManager() any` | 缺失 | 新增，返回 `a.abilityManager` |
| `AgentID() string` | 已有 | 修改，直接返回 `a.card.ID`（不再委托 base） |
| `CallbackManager() *AgentCallbackManager` | 已有 | 修改，直接返回 `a.callbackManager`（不再委托 base） |
| `Configure(ctx, interfaces.AgentConfig)` | 签名不对 | 修改签名，内部断言为 `*ReActAgentConfig` |
| `RegisterCallback(...)` | 缺失 | 新增，委托 `a.callbackManager` |
| `RegisterRail(...)` | 缺失 | 新增，调用 `r.Init(a)` + 委托 `a.callbackManager` |
| `UnregisterRail(...)` | 缺失 | 新增，委托 `a.callbackManager` + 调用 `r.Uninit(a)` |

### 12.5 编译期接口检查

添加编译期断言：
```go
var _ interfaces.BaseAgent = (*ReActAgent)(nil)
```

### 12.6 NewReActAgent 构造函数修改

```go
func NewReActAgent(card *agentschema.AgentCard, config *saconfig.ReActAgentConfig) *ReActAgent {
    agent := &ReActAgent{
        card:            card,
        abilityManager:  ability.NewAbilityManager(nil),
        callbackManager: rail.NewAgentCallbackManager(card.ID),
        config:          config,
        promptBuilder:   NewSystemPromptBuilder(),
    }
    agent.hitlHandler = interrupt.NewToolInterruptHandler(agent)
    return agent
}
```

### 12.7 re-exports 处理

`base.go` 中的 5 个类型别名（`AgentOption`、`AbilityManager`、`AddAbilityResult`、`ExecuteResult`、`AbilityExecutionError`）迁移到新文件 `single_agent/reexports.go`。

### 12.8 doc.go 更新

从文件目录中移除 `base.go`，新增 `reexports.go`。

## 13. Runner 包 doc.go 更新

更新文件目录，新增：
- `runner.go` 描述更新（Runner 结构体 + 全局实例 + 全部包级函数）
- `ref.go`（AgentRef / WorkflowRef）

## 14. 测试策略

- `runner_test.go` — 测试全部包级函数，通过 mock ResourceMgr 注入测试 Agent
- `react_agent_test.go` — 更新，删除 `agent.base` 引用，新增接口满足性测试
- 新增 `ref_test.go` — 测试 AgentRef / WorkflowRef 构造和访问

## 15. 未实现组件汇总

| 组件 | 回填标记 | 依赖章节 |
|------|---------|---------|
| TaskGroup 作用域 | `⤵️ 预留：任务组作用域（依赖 TaskGroup 实现）` | 新章节 |
| IsRemoteAgent | `⤵️ 预留：远程 Agent 支持（依赖 RemoteAgent 实现）` | 分布式章节 |
| LegacyBaseAgent | `⤵️ 预留：LegacyBaseAgent 兼容（依赖 LegacyBaseAgent 实现）` | 兼容层章节 |
| SpawnAgent/SpawnAgentStreaming | `⤵️ 预留：Spawn 子进程（依赖 6.28）` | 6.28 |
| TeamRuntimeManager | `⤵️ 预留：Team 会话释放（依赖 9.85）` | 9.85 |
| DistributeMessageQueue | `⤵️ 预留：分布式消息队列（依赖分布式模式实现）` | 分布式章节 |
