# 6.2 BaseAgent 接口设计文档

> 对应 Python: `openjiuwen/core/single_agent/base.py` (BaseAgent)
> 实现计划步骤: 6.2 — BaseAgent 接口：`Configure/Invoke/Stream`，AbilityManager 挂载

## 1. 流程位置与作用

### 1.1 在领域六中的位置

```
6.1 (✅ AgentCard/AgentResult 数据模型) → 6.2 (☐ BaseAgent 接口) → 6.3 (☐ ReActAgentConfig) → ... → 6.11 (ReActAgent 实现)
```

6.2 是从**数据模型层**迈向**行为接口层**的关键一步：
- 6.1 定义了 Agent "数据长什么样"
- 6.2 定义了 Agent "能做什么"（Configure/Invoke/Stream + AbilityManager/AgentCallbackManager 挂载）
- 6.3+ 的所有具体 Agent 实现都基于此接口

### 1.2 在 Agent 会话中的流程位置

```
用户请求 → Runner → BaseAgent.Invoke/Stream
                      ├── ① emit_before (全局回调触发)
                      ├── ② transform_io (输入/输出变换，6.24 回填)
                      ├── invokeImpl/streamImpl (子类真实逻辑，如 ReAct 循环)
                      ├── ② transform_io (输出变换，6.24 回填)
                      └── ③ emit_after (全局回调触发)
```

## 2. 决策清单

| # | 决策 | 选择 | 原因 |
|---|------|------|------|
| 1 | BaseAgent 形态 | 接口优先（interface） | Go 惯用，依赖注入友好 |
| 2 | 接口命名 | `BaseAgent`（重命名 `interfaces.Agent`） | Python 也叫 BaseAgent；ResourceManager.get_agent() 返回 BaseAgent |
| 3 | 接口方法集 | 全部暴露，对齐 Python | 用户要求与 Python 完全对齐 |
| 4 | Stream 返回类型 | `<-chan stream.Schema` | 与 Workflow.Stream 一致；Schema 接口可容纳 OutputSchema/ControllerOutputChunk |
| 5 | 回调包装方式 | 模板方法：WarpBaseAgent 提供骨架 | Python 元类自动装饰的等价方案 |
| 6 | 虚分发 | 接口字段 `agentInvoker` | Go 内嵌结构体无法虚分发到子类方法，接口字段可实现等价虚方法表 |
| 7 | WarpBaseAgent 位置 | `single_agent/base.go` | 对齐 Python 的 `single_agent/base.py` |
| 8 | Agent 回调域 | 新增 `AgentCallEventType` + `TriggerAgent` | 与 LLM/Tool/Session/Context 平级，类型安全 |
| 9 | Configure 返回值 | 返回 error | Go 惯用，放弃 Python 的链式调用 |
| 10 | interfaces.Agent → BaseAgent | 直接重命名扩展 | Python 的 ResourceManager.get_agent() 返回 BaseAgent |
| 11 | 6.4-6.6 依赖类型 | any 占位，6.4-6.6 回填 | 避免 6.2 提前依赖未实现的类型 |
| 12 | Invoke/Stream 回调 | 用 `CallbackFramework.TriggerAgent`（全局 AgentEvents） | Python 元类用全局 AgentEvents（不带 agent_id 前缀） |
| 13 | AgentCallEventData.Session | 具体类型 `*session.Session` | Go 中已有具体类型，不应用 any |
| 14 | 错误码 | 新增 `StatusAgentNotConfigured` (120008) | invoker 未设置时的错误 |

## 3. Python 参考实现分析

### 3.1 Python BaseAgent 核心结构

```python
class BaseAgent(metaclass=_AgentMeta):
    def __init__(self, card: AgentCard):
        self.card = card
        self._ability_manager = AbilityManager()
        self._agent_callback_manager = AgentCallbackManager(card.id)
        self._skill_util = None
        self.lazy_init_skill()

    @abstractmethod
    def configure(self, config) -> 'BaseAgent': pass

    @abstractmethod
    async def invoke(self, inputs, session=None) -> Any: ...

    @abstractmethod
    async def stream(self, inputs, session=None, stream_modes=None) -> AsyncIterator[Any]: ...

    async def register_callback(self, event, callback, priority=100) -> 'BaseAgent': ...
    async def register_rail(self, rail) -> 'BaseAgent': ...
    async def unregister_rail(self, rail) -> 'BaseAgent': ...
    async def _execute_callbacks(self, event, inputs, session, context): ...
```

### 3.2 Python 元类 `_AgentMeta` 装饰链

```python
class _AgentMeta(ABCMeta):
    def __call__(cls, *args, **kwargs):
        instance = super().__call__(*args, **kwargs)
        _fw = Runner.callback_framework

        # invoke 包装
        fn = instance.invoke
        fn = _fw.emit_before(AgentEvents.AGENT_INVOKE_INPUT)(fn)
        fn = _fw.transform_io(input_event=AGENT_INVOKE_INPUT, output_event=AGENT_INVOKE_OUTPUT)(fn)
        fn = _fw.emit_after(AgentEvents.AGENT_INVOKE_OUTPUT)(fn)
        instance.invoke = fn

        # stream 包装（同理）
        fn = instance.stream
        fn = _fw.emit_before(AgentEvents.AGENT_STREAM_INPUT)(fn)
        fn = _fw.transform_io(input_event=AGENT_STREAM_INPUT, output_event=AGENT_STREAM_OUTPUT)(fn)
        fn = _fw.emit_after(AgentEvents.AGENT_STREAM_OUTPUT, item_key="result")(fn)
        instance.stream = fn
        return instance
```

**三层装饰执行顺序**：`emit_before` → `transform_io` → 真实方法 → `transform_io` → `emit_after`

### 3.3 Python 两套回调体系

| | AgentEvents（元类装饰） | AgentCallbackManager（per-Agent） |
|---|---|---|
| **事件名** | `_framework:agent_invoke_input` | `{agent_id}_before_invoke` |
| **范围** | 全局，所有 Agent 共享 | per-Agent，仅对该 Agent |
| **谁注册** | 框架级监听器（日志、tracer） | 用户级 Rail/回调（重试、强制完成） |
| **触发方式** | 元类自动装饰 | ReAct 循环中手动 `_execute_callbacks` |

**AgentCallbackManager 本身没有自己的注册表**，它只是一个带 agent_id 前缀的适配层，所有操作都穿透到全局 CallbackFramework。

### 3.4 Python ResourceManager.get_agent() 返回 BaseAgent

```python
async def get_agent(self, agent_id, ...) -> Optional[BaseAgent] | list[Optional[BaseAgent]]:
    ...
```

## 4. Go 端设计

### 4.1 BaseAgent 接口（`interfaces/interface.go`）

```go
// BaseAgent Agent 执行的核心行为契约。
//
// 对应 Python: openjiuwen/core/single_agent/base.py (BaseAgent)
//
// 设计原则：
//   - Card is required（定义 Agent 是什么）
//   - Config is optional（定义 Agent 怎么运行）
//   - 所有子类（ReActAgent/ControllerAgent）实现此接口
type BaseAgent interface {
    // ── 核心三方法 ──

    // Configure 配置 Agent。
    // 对应 Python: BaseAgent.configure(config)
    Configure(ctx context.Context, config any) error

    // Invoke 非流式调用 Agent。
    // 对应 Python: BaseAgent.invoke(inputs, session)
    Invoke(ctx context.Context, inputs map[string]any, opts ...AgentOption) (any, error)

    // Stream 流式调用 Agent。
    // 对应 Python: BaseAgent.stream(inputs, session, stream_modes)
    Stream(ctx context.Context, inputs map[string]any, opts ...AgentOption) (<-chan stream.Schema, error)

    // ── 访问器 ──

    // Card 返回 Agent 身份卡片。
    // 对应 Python: BaseAgent.card 属性
    Card() *agentschema.AgentCard

    // Config 返回当前配置。
    // 对应 Python: BaseAgent.config 属性
    Config() any

    // AbilityManager 返回能力管理器。
    // 对应 Python: BaseAgent.ability_manager 属性
    AbilityManager() *single_agent.AbilityManager

    // CallbackManager 返回回调管理器。
    // 对应 Python: BaseAgent.agent_callback_manager 属性
    // ⤵️ 6.6 回填：返回类型从 any 改为 *AgentCallbackManager
    CallbackManager() any

    // ── 回调/Rail 注册 ──

    // RegisterCallback 注册回调。
    // 对应 Python: BaseAgent.register_callback(event, callback, priority)
    // ⤵️ 6.4-6.6 回填：event/callback 参数类型从 any 改为具体类型
    RegisterCallback(ctx context.Context, event any, callback any, priority int) error

    // RegisterRail 注册 Rail。
    // 对应 Python: BaseAgent.register_rail(rail)
    // ⤵️ 6.7 回填：rail 参数类型从 any 改为 AgentRail
    RegisterRail(ctx context.Context, rail any) error

    // UnregisterRail 注销 Rail。
    // 对应 Python: BaseAgent.unregister_rail(rail)
    // ⤵️ 6.7 回填：rail 参数类型从 any 改为 AgentRail
    UnregisterRail(ctx context.Context, rail any) error
}
```

### 4.2 AgentOptions 扩展（`interfaces/interface.go`）

```go
// AgentOptions Agent 调用选项。
type AgentOptions struct {
    // Session 会话实例（可选）
    // 对应 Python: invoke(inputs, session) / stream(inputs, session, stream_modes) 的 session 参数
    Session *session.Session
    // StreamModes 流式输出模式（可选）
    // 对应 Python: stream(inputs, session, stream_modes) 的 stream_modes 参数
    StreamModes []stream.StreamMode
}

// AgentOption Agent 调用选项函数。
type AgentOption func(*AgentOptions)
```

### 4.3 AgentCallEventType + AgentCallEventData（`callback/events.go` 新增）

```go
// AgentCallEventType Agent 调用事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (AgentEvents)
type AgentCallEventType string

const (
    // AgentStarted Agent 执行启动
    AgentStarted AgentCallEventType = "_framework:agent_started"
    // AgentInvokeInput invoke 调用前触发
    AgentInvokeInput AgentCallEventType = "_framework:agent_invoke_input"
    // AgentInvokeOutput invoke 调用后触发
    AgentInvokeOutput AgentCallEventType = "_framework:agent_invoke_output"
    // AgentStreamInput stream 调用前触发
    AgentStreamInput AgentCallEventType = "_framework:agent_stream_input"
    // AgentStreamOutput stream 每项触发
    AgentStreamOutput AgentCallEventType = "_framework:agent_stream_output"
)

// AgentCallEventData Agent 调用事件数据。
type AgentCallEventData struct {
    // Event 事件类型
    Event AgentCallEventType
    // AgentID Agent 标识
    AgentID string
    // Inputs 调用输入
    Inputs map[string]any
    // Result 调用结果（InvokeOutput/StreamOutput 时有值）
    Result any
    // Session 会话实例
    Session *session.Session
    // Error 错误信息
    Error error
    // Extra 额外数据
    Extra map[string]any
}

// AgentCallbackFunc Agent 回调函数类型。
type AgentCallbackFunc func(ctx context.Context, data *AgentCallEventData) any
```

### 4.4 CallbackFramework 扩展（`callback/framework.go` 新增）

```go
// CallbackFramework 结构体新增字段：
type CallbackFramework struct {
    // ... 已有字段
    agentCallbacks map[AgentCallEventType][]AgentCallbackFunc
}

// OnAgent 注册 Agent 事件回调函数。
func (fw *CallbackFramework) OnAgent(event AgentCallEventType, fn AgentCallbackFunc) { ... }

// OffAgent 注销 Agent 事件回调函数。
func (fw *CallbackFramework) OffAgent(event AgentCallEventType, fn AgentCallbackFunc) { ... }

// TriggerAgent 触发 Agent 事件，按注册顺序调用所有回调，返回所有回调结果。
func (fw *CallbackFramework) TriggerAgent(ctx context.Context, data *AgentCallEventData) []any { ... }
```

### 4.5 agentInvoker 接口 + WarpBaseAgent 默认实现（`single_agent/base.go`）

```go
// agentInvoker 子类真实执行接口，用于虚分发。
//
// Go 内嵌结构体无法虚分发到子类方法（w.invokeImpl() 在编译期
// 绑定 WarpBaseAgent.invokeImpl，不会调用 ReActAgent.invokeImpl）。
// 通过接口字段 invoker agentInvoker 实现等价虚方法表：
// 构造时 agent.invoker = agent，调用 w.invoker.invokeImpl() 走虚分发。
type agentInvoker interface {
    invokeImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error)
    streamImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error)
}

// WarpBaseAgent BaseAgent 的默认实现，提供 Invoke/Stream 的回调包装骨架。
// 子类内嵌 WarpBaseAgent 并实现 agentInvoker 接口。
//
// 对应 Python: openjiuwen/core/single_agent/base.py (BaseAgent)
type WarpBaseAgent struct {
    // card Agent 身份卡片（必需）
    card *agentschema.AgentCard
    // config Agent 配置（可选，Configure 时设置）
    config any
    // abilityManager 能力管理器
    abilityManager *AbilityManager
    // callbackManager 回调管理器
    // ⤵️ 6.6 回填：从 any 改为 *AgentCallbackManager
    callbackManager any
    // invoker 子类注入的真实执行逻辑，实现虚分发
    invoker agentInvoker
}
```

### 4.6 WarpBaseAgent.Invoke 模板方法

```go
// Invoke 非流式调用，包含回调包装骨架。
// 执行顺序：① emit_before → ② transform_io(输入) → invokeImpl → ② transform_io(输出) → ③ emit_after
//
// 对应 Python: _AgentMeta 元类装饰后的 invoke
func (w *WarpBaseAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
    if w.invoker == nil {
        return nil, exception.NewBaseError(exception.StatusAgentNotConfigured,
            exception.WithMsg("invoker 未设置，子类构造时必须设置 invoker"))
    }

    fw := callback.GetCallbackFramework()

    // ① emit_before: 触发全局 AgentInvokeInput 事件
    fw.TriggerAgent(ctx, &callback.AgentCallEventData{
        Event:   callback.AgentInvokeInput,
        AgentID: w.card.ID,
        Inputs:  inputs,
    })

    // ② transform_io 输入变换（⤵️ 预留，6.24 回填）
    // inputs = fw.TriggerTransform(ctx, callback.AgentInvokeInput, inputs)

    // 执行子类的真实逻辑
    result, err := w.invoker.invokeImpl(ctx, inputs, opts...)
    if err != nil {
        // 已经是 BaseError 则直接返回（对齐 Python except BaseError: raise）
        if _, ok := err.(*exception.BaseError); ok {
            return nil, err
        }
        // 其他错误包装（对齐 Python except Exception as e: raise build_error(...)）
        logger.Error(logger.ComponentAgentCore).
            Str("agent_id", w.card.ID).
            Err(err).
            Msg("Agent invoke 错误")
        return nil, exception.NewBaseError(exception.StatusAgentControllerRuntimeError,
            exception.WithCause(err),
        )
    }

    // ② transform_io 输出变换（⤵️ 预留，6.24 回填）
    // result = fw.TriggerTransform(ctx, callback.AgentInvokeOutput, result)

    // ③ emit_after: 触发全局 AgentInvokeOutput 事件
    fw.TriggerAgent(ctx, &callback.AgentCallEventData{
        Event:   callback.AgentInvokeOutput,
        AgentID: w.card.ID,
        Result:  result,
    })

    return result, nil
}
```

### 4.7 WarpBaseAgent.Stream 模板方法

```go
// Stream 流式调用，包含回调包装骨架。
// 执行顺序：① emit_before → streamImpl → per-item { ② transform_io(输出) → ③ emit_after }
//
// 对应 Python: _AgentMeta 元类装饰后的 stream
func (w *WarpBaseAgent) Stream(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error) {
    if w.invoker == nil {
        return nil, exception.NewBaseError(exception.StatusAgentNotConfigured,
            exception.WithMsg("invoker 未设置，子类构造时必须设置 invoker"))
    }

    fw := callback.GetCallbackFramework()

    // ① emit_before
    fw.TriggerAgent(ctx, &callback.AgentCallEventData{
        Event:   callback.AgentStreamInput,
        AgentID: w.card.ID,
        Inputs:  inputs,
    })

    // 调用子类的真实 stream
    ch, err := w.invoker.streamImpl(ctx, inputs, opts...)
    if err != nil {
        if _, ok := err.(*exception.BaseError); ok {
            return nil, err
        }
        logger.Error(logger.ComponentAgentCore).
            Str("agent_id", w.card.ID).
            Err(err).
            Msg("Agent stream 错误")
        return nil, exception.NewBaseError(exception.StatusAgentControllerRuntimeError,
            exception.WithCause(err),
        )
    }

    // 包装 channel：每个 item 触发 ③ emit_after 后转发
    out := make(chan stream.Schema)
    go func() {
        defer close(out)
        for item := range ch {
            // ② transform_io 输出变换（⤵️ 预留，6.24 回填）
            // item = fw.TriggerTransform(ctx, callback.AgentStreamOutput, item)

            // ③ emit_after (per-item)
            fw.TriggerAgent(ctx, &callback.AgentCallEventData{
                Event:   callback.AgentStreamOutput,
                AgentID: w.card.ID,
                Result:  item,
            })

            out <- item
        }
    }()

    return out, nil
}
```

### 4.8 子类使用示例（ReActAgent，6.11 实现）

```go
type ReActAgent struct {
    WarpBaseAgent
    // ReAct 特有字段...
}

func NewReActAgent(card *AgentCard, config *ReActAgentConfig, resourceMgr ResourceManager) *ReActAgent {
    agent := &ReActAgent{
        WarpBaseAgent: *NewWarpBaseAgent(card, resourceMgr),
    }
    // 虚分发：invoker 指向自身，ReActAgent 实现 agentInvoker 接口
    agent.invoker = agent
    return agent
}

// 实现 agentInvoker 接口
func (r *ReActAgent) invokeImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
    // ReAct 循环的真实逻辑
}

func (r *ReActAgent) streamImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error) {
    // ReAct 流式的真实逻辑
}
```

## 5. 错误码

新增于 `codes_agent.go`（Agent Orchestration 区间 120000-120999）：

```go
// StatusAgentNotConfigured Agent 未配置（invoker 未设置）
StatusAgentNotConfigured = NewStatusCode(
    "AGENT_NOT_CONFIGURED", 120008,
    "agent not configured, invoker not set, reason: {error_msg}")
```

## 6. 文件变更清单

### 6.1 新增文件

| 文件 | 内容 |
|------|------|
| `single_agent/base.go` | `agentInvoker` 接口 + `WarpBaseAgent` 结构体 + 全部方法实现 |
| `single_agent/base_test.go` | WarpBaseAgent 单元测试 |

### 6.2 修改文件

| 文件 | 修改内容 |
|------|---------|
| `interfaces/interface.go` | `Agent` → `BaseAgent`，扩展方法集，充实 AgentOptions |
| `interfaces/doc.go` | 更新说明 |
| `callback/events.go` | +`AgentCallEventType`/`AgentCallEventData`/`AgentCallbackFunc` |
| `callback/framework.go` | +`agentCallbacks` map + `OnAgent/OffAgent/TriggerAgent` |
| `callback/doc.go` | 更新文件目录 |
| `resource_manager.go` | `GetAgent` 返回 `interfaces.BaseAgent` |
| `resource_manager_test.go` | 适配 BaseAgent 接口变更 |
| `ability_manager.go` | 移除 `SetContextEngine` 预留注释语义 |
| `ability_manager_test.go` | 适配接口变更 |
| `codes_agent.go` | +`StatusAgentNotConfigured` (120008) |
| `single_agent/doc.go` | 更新文件目录，加入 base.go |
| `IMPLEMENTATION_PLAN.md` | 6.2 状态 ☐ → ✅ |

## 7. 回填标记汇总

| 位置 | 回填内容 | 目标步骤 |
|------|---------|---------|
| `WarpBaseAgent.Invoke/Stream` | transform_io 输入/输出变换 | 6.24 |
| `WarpBaseAgent.callbackManager` | any → `*AgentCallbackManager` | 6.6 |
| `WarpBaseAgent.CallbackManager()` 返回类型 | any → `*AgentCallbackManager` | 6.6 |
| `WarpBaseAgent.RegisterCallback` 参数 | any → `AgentCallbackEvent`/`AnyAgentCallback` | 6.4-6.6 |
| `WarpBaseAgent.RegisterRail/UnregisterRail` 参数 | any → `AgentRail` | 6.7 |
| `AbilityManager.rail` / `SetRail` | Rail 钩子调用点 | 6.4-6.10 |
| `ToolCallContext` 预留字段 | force_finish/steering_queue/skip_tool | 6.5 |

## 8. 测试策略

### 8.1 WarpBaseAgent 单元测试覆盖

| 测试用例 | 覆盖内容 |
|---------|---------|
| `TestNewWarpBaseAgent` | 构造函数，字段初始化正确 |
| `TestWarpBaseAgent_Invoke_正常调用` | invoker 设置后，Invoke 返回 invokeImpl 结果 |
| `TestWarpBaseAgent_Invoke_invoker未设置` | invoker 为 nil 时返回 StatusAgentNotConfigured 错误 |
| `TestWarpBaseAgent_Invoke_触发回调` | Verify TriggerAgent 被调用两次（before + after） |
| `TestWarpBaseAgent_Invoke_子类错误透传` | invokeImpl 返回 BaseError 时直接透传 |
| `TestWarpBaseAgent_Invoke_普通错误包装` | invokeImpl 返回普通 error 时包装为 BaseError |
| `TestWarpBaseAgent_Stream_正常调用` | invoker 设置后，Stream 返回 channel 且数据正确 |
| `TestWarpBaseAgent_Stream_invoker未设置` | invoker 为 nil 时返回错误 |
| `TestWarpBaseAgent_Stream_每项触发回调` | Verify 每个 stream item 触发一次 TriggerAgent |
| `TestWarpBaseAgent_Configure` | Configure 设置 config 成功 |
| `TestWarpBaseAgent_访问器` | Card/Config/AbilityManager 返回正确值 |
| `TestWarpBaseAgent_虚分发` | 内嵌 WarpBaseAgent + 实现 agentInvoker，验证 invokeImpl 走子类实现 |
| `TestAgentCallEventType_事件名` | 验证事件名与 Python AgentEvents 对齐 |

### 8.2 CallbackFramework OnAgent/TriggerAgent 测试

| 测试用例 | 覆盖内容 |
|---------|---------|
| `TestCallbackFramework_OnAgent_TriggerAgent` | 注册+触发+结果返回 |
| `TestCallbackFramework_OffAgent` | 注销后不再触发 |
| `TestCallbackFramework_TriggerAgent_无回调` | 无注册时返回 nil |

## 9. stream.Schema 解析链路确认

Go 端已完整实现与 Python 对齐的 stream 数据写入和解析链路：

```
写入: ReActAgent → Session.WriteStream(data)
        → normalizeOutputStream()  # 三种分支对齐 Python _normalize_output_stream
        → outputWriter.Write()     # Validate 校验
        → StreamEmitter.Emit()     # 入队

消费: 调用方 range agent.Stream(...)
        ← WarpBaseAgent.Stream() 包装 channel
          ← ReActAgent.streamImpl()
            ← Session.StreamIterator()
              ← StreamWriterManager.StreamOutput()  # 返回 <-chan Schema
                ← StreamQueue.Receive()             # data 就是 OutputSchema
```

Python 中 ReActAgent.stream() yield `OutputSchema`，ControllerAgent.stream() yield `ControllerOutputChunk`（OutputSchema 子类）。
Go 中 `stream.Schema` 接口统一容纳，通过类型断言区分具体类型。
