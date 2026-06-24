# 6.7 AgentRail 接口设计

## 概述

本文档描述领域6第6.7小节 **AgentRail 接口** 的实现设计。AgentRail 是 class-based 的生命周期钩子容器，定义 10 个生命周期钩子方法，供 Rail 子类覆盖特定事件并批量注册到 AgentCallbackManager。

对应 Python 源码：`openjiuwen/core/single_agent/rail/base.py` 中 `AgentRail(ABC)` 类。

## 流程位置与作用

### 在 Agent 会话中的位置

```
Agent.invoke()
  ├── [Rail] before_invoke              ← AgentRail 钩子
  ├── Task Loop (iterations)
  │     ├── [Rail] before_task_iteration
  │     ├── Model Call
  │     │     ├── [Rail] before_model_call
  │     │     ├── LLM API 调用
  │     │     ├── [Rail] after_model_call
  │     │     └── [Rail] on_model_exception (异常时)
  │     ├── Tool Call
  │     │     ├── [Rail] before_tool_call
  │     │     ├── 工具执行
  │     │     ├── [Rail] after_tool_call
  │     │     └── [Rail] on_tool_exception (异常时)
  │     └── [Rail] after_task_iteration
  └── [Rail] after_invoke               ← AgentRail 钩子
```

### 核心作用

1. **结构化拦截**：将散落的回调按 class 组织，一个 Rail 子类可以覆盖多个钩子，共享状态（通过 struct 字段和 `ctx.Extra()`）
2. **优先级调度**：`Priority()` 控制同一事件上多个 Rail 的执行顺序（数值越大越先执行）
3. **工具自注册/注销**：`Init(agent)` 在注册时向 Agent 注入工具，`Uninit(agent)` 在注销时清理
4. **批量注册/注销**：`GetCallbacks()` 提取子类已覆盖的钩子，AgentCallbackManager 遍历批量注册
5. **与 @rail 装饰器协同**：6.8 的装饰器负责触发事件，6.7 的 Rail 负责响应事件

### 上下游依赖

| 上游（已完成） | 当前 | 下游（未开始） |
|---|---|---|
| 6.4 AgentCallbackEvent（10种事件枚举）✅ | **6.7 AgentRail 接口** | 6.8 @rail 装饰器等价 |
| 6.5 AgentCallbackContext（回调上下文）✅ | | 6.9 Rail Inputs（类型化输入） |
| 6.6 AgentCallbackManager（回调管理器）✅ | | 6.10 ForceFinish/RetryRequest |

## 设计决策

### D1：钩子注册策略 — 接口 + 结构体嵌入

**选择**：AgentRail 为接口 + BaseRail 结构体提供 no-op 默认。

**理由**：
- Go 没有反射判断子类是否覆盖方法的能力（Python 用 `_is_base_method` + `getattr`）
- 接口保证编译期类型安全，BaseRail 嵌入后只需覆盖关心的钩子
- `GetCallbacks()` 由用户显式实现，声明已覆盖的事件映射

**排除方案**：
- 纯接口（用户必须实现全部10个方法）
- 按事件拆分小接口（接口数量膨胀，10+接口）
- 注册全部10个钩子（no-op 也注册，运行时开销，与 Python 语义不一致）

### D2：GetCallbacks 辅助方法 — CallbackFrom + BuildCallbacks

**选择**：BaseRail 提供 `CallbackFrom(event, fn)` 和 `BuildCallbacks(entries...)` 辅助方法。

**理由**：
- 减少用户写 `GetCallbacks()` 的样板代码
- 仍然显式声明，零魔法，编译期安全
- 用法示例：

```go
func (r *MyRail) GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc {
    return r.BuildCallbacks(
        r.CallbackFrom(CallbackBeforeModelCall, r.BeforeModelCall),
        r.CallbackFrom(CallbackAfterModelCall, r.AfterModelCall),
    )
}
```

### D3：回调类型 — 直接复用 cb.PerAgentCallbackFunc

**选择**：不额外定义 `RailCallbackFunc`，直接使用 `cb.PerAgentCallbackFunc`。

**理由**：
- 与 Python `AnyAgentCallback` 语义一致
- 无桥接函数，RegisterRail 直接传入
- 钩子内部自行断言 `*AgentCallbackContext`（与当前 Fire→Execute→TriggerPerAgent 路径一致）

### D4：RailAgent 最小接口 — 包内定义，按需扩充

**选择**：在 `rail` 包内定义 `RailAgent` 最小接口，当前实装 `CallbackManager()` + `AgentID()`，其余用注释标记待扩充。

**理由**：
- 打破 `rail` → `interfaces` 循环依赖
- `interfaces.BaseAgent` 隐式满足此接口
- 后续实现具体 Rail 子类时按需添加方法

### D5：不实现 EVENT_METHOD_MAP

**选择**：不实现 Python 的 `EVENT_METHOD_MAP`。

**理由**：
- 方案A2 下 `CallbackFrom`/`BuildCallbacks` 由用户显式声明，不需要事件→方法名映射
- 与 `event.go` 注释一致："事件值即 Python AgentRail 对应方法名，无需额外 EVENT_METHOD_MAP 映射"

## 核心类型定义

### AgentRail 接口

```go
// AgentRail Agent 生命周期 Rail 接口。
//
// Rail 是 class-based 的生命周期钩子容器，允许在 Agent 执行流程的
// 特定时机注入拦截逻辑（重试、提前终止、steering 等）。
// 嵌入 BaseRail 后只需覆盖关心的钩子方法，并在 GetCallbacks() 中
// 声明已覆盖的事件映射。
//
// 对应 Python: AgentRail(ABC) (openjiuwen/core/single_agent/rail/base.py L451-573)
type AgentRail interface {
    // Priority 返回执行优先级（数值越大越先执行）
    Priority() int
    // Init Rail 初始化钩子（注册时调用，用于工具自注册等）
    Init(agent RailAgent) error
    // Uninit Rail 注销钩子（注销时调用，用于工具清理等）
    Uninit(agent RailAgent) error

    // 10 个生命周期钩子方法
    BeforeInvoke(ctx context.Context, cbc *AgentCallbackContext) error
    AfterInvoke(ctx context.Context, cbc *AgentCallbackContext) error
    BeforeModelCall(ctx context.Context, cbc *AgentCallbackContext) error
    AfterModelCall(ctx context.Context, cbc *AgentCallbackContext) error
    OnModelException(ctx context.Context, cbc *AgentCallbackContext) error
    BeforeToolCall(ctx context.Context, cbc *AgentCallbackContext) error
    AfterToolCall(ctx context.Context, cbc *AgentCallbackContext) error
    OnToolException(ctx context.Context, cbc *AgentCallbackContext) error
    BeforeTaskIteration(ctx context.Context, cbc *AgentCallbackContext) error
    AfterTaskIteration(ctx context.Context, cbc *AgentCallbackContext) error

    // GetCallbacks 提取已覆盖的钩子方法映射，供 RegisterRail 批量注册。
    GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc
}
```

### BaseRail 结构体

```go
// BaseRail AgentRail 的 no-op 默认实现。
//
// 用户嵌入此结构体后只需覆盖关心的钩子方法，并在 GetCallbacks() 中
// 通过 CallbackFrom + BuildCallbacks 声明已覆盖的事件映射。
type BaseRail struct {
    // priority 执行优先级（数值越大越先执行），默认 50
    priority int
}

// NewBaseRail 创建默认优先级(50)的 BaseRail。
func NewBaseRail() *BaseRail

// WithPriority 设置优先级（Functional Options 模式）。
func (r *BaseRail) WithPriority(p int) *BaseRail

// Priority 返回优先级。
func (r *BaseRail) Priority() int

// Init 默认 no-op。
func (r *BaseRail) Init(_ RailAgent) error { return nil }

// Uninit 默认 no-op。
func (r *BaseRail) Uninit(_ RailAgent) error { return nil }

// 10 个钩子方法全部返回 nil（no-op）
// GetCallbacks 返回空 map
```

### CallbackFrom / BuildCallbacks 辅助

```go
// callbackEntry 事件→回调映射条目，BuildCallbacks 的参数。
type callbackEntry struct {
    event AgentCallbackEvent
    fn    cb.PerAgentCallbackFunc
}

// CallbackFrom 创建一条事件→回调映射条目。
func (r *BaseRail) CallbackFrom(event AgentCallbackEvent, fn cb.PerAgentCallbackFunc) callbackEntry

// BuildCallbacks 从多条映射条目构建 GetCallbacks 返回值。
func (r *BaseRail) BuildCallbacks(entries ...callbackEntry) map[AgentCallbackEvent]cb.PerAgentCallbackFunc
```

### RailAgent 最小接口扩充

```go
// RailAgent Rail 包所需的最小 Agent 接口。
//
// 在 rail 包内定义，打破 rail → interfaces 循环依赖。
// interfaces.BaseAgent 隐式满足此接口。
type RailAgent interface {
    // CallbackManager 返回 PerAgent 回调管理器
    CallbackManager() *AgentCallbackManager
    // AgentID 返回 Agent 唯一标识
    // ⤴️ 6.7 定义；BaseAgent 需确保实现此方法
    AgentID() string
    // ⤵️ 后续 Rail 子类实现时按需扩充：
    // AbilityManager() — 工具注册/注销（MemoryRail, SkillUseRail 等需要）
    // SystemPromptBuilder() — 系统提示词构建器（多数 Rail init 中需要）
    // Card() — Agent 元数据（agent.card.id 等场景）
    // DeepConfig() — 深层配置（HeartbeatRail 等需要）
}
```

## 回填逻辑

### manager.go — RegisterRail / UnregisterRail

```go
// RegisterRail 批量注册一个 Rail 实例的所有回调。
func (m *AgentCallbackManager) RegisterRail(ctx context.Context, r AgentRail, opts ...cb.CallbackOption) error {
    callbacks := r.GetCallbacks()
    priorityOpt := cb.WithPriority(r.Priority())
    allOpts := append([]cb.CallbackOption{priorityOpt}, opts...)
    for event, fn := range callbacks {
        m.RegisterCallback(ctx, event, fn, allOpts...)
        logger.Debug(logComponent).
            Str("event_type", "rail_register_callback").
            Str("event", string(event)).
            Int("priority", r.Priority()).
            Msg("Rail 钩子注册到回调框架")
    }
    return nil
}

// UnregisterRail 批量注销一个 Rail 实例的所有回调。
func (m *AgentCallbackManager) UnregisterRail(ctx context.Context, r AgentRail) error {
    callbacks := r.GetCallbacks()
    for event, fn := range callbacks {
        m.Unregister(event, fn)
        logger.Debug(logComponent).
            Str("event_type", "rail_unregister_callback").
            Str("event", string(event)).
            Msg("Rail 钩子从回调框架注销")
    }
    return nil
}
```

### interfaces/interface.go — 参数类型回填

```go
// 回填前
RegisterRail(ctx context.Context, rail any, opts ...cb.CallbackOption) error
UnregisterRail(ctx context.Context, rail any) error

// 回填后
RegisterRail(ctx context.Context, rail rail.AgentRail, opts ...cb.CallbackOption) error
UnregisterRail(ctx context.Context, rail rail.AgentRail) error
```

### context.go — railAgent 接口扩充

在现有 `railAgent` 接口上添加 `AgentID() string` 方法。

## 文件组织

```
rail/
├── doc.go           # 包文档（更新：添加 rail.go 条目）
├── event.go         # AgentCallbackEvent 枚举（6.4 已完成）
├── context.go       # AgentCallbackContext（6.5 已完成，扩充 railAgent 接口）
├── inputs.go        # EventInputs 骨架（6.9 占位）
├── rail.go          # 【新增】AgentRail 接口 + BaseRail 结构体 + CallbackFrom/BuildCallbacks
└── manager.go       # AgentCallbackManager（6.6 已完成，回填 RegisterRail/UnregisterRail）
```

## 测试用例

| 测试函数 | 场景 |
|---|---|
| `TestBaseRail_默认优先级` | NewBaseRail() 默认 priority=50 |
| `TestBaseRail_WithPriority` | WithPriority 设置自定义优先级 |
| `TestBaseRail_所有钩子为NoOp` | 10个钩子方法均返回nil |
| `TestBaseRail_GetCallbacks_返回空Map` | 默认 GetCallbacks 返回空 map |
| `TestCallbackFrom_单条映射` | CallbackFrom 构建单条映射 |
| `TestBuildCallbacks_多条映射` | BuildCallbacks 合并多条映射 |
| `TestBuildCallbacks_空输入` | 无参数时返回空 map |
| `TestRegisterRail_批量注册` | 注册含2个钩子的Rail，验证2个事件均注册 |
| `TestRegisterRail_优先级传递` | 验证 RegisterRail 传入 rail.Priority() 到 CallbackOption |
| `TestUnregisterRail_批量注销` | 注销后验证事件无钩子 |
| `TestAgentRail_接口满足` | BaseRail 满足 AgentRail 接口（编译期检查） |
| `TestRailAgent_接口满足` | 验证 mock struct 满足 RailAgent 接口 |

## 日志对齐

- Python AgentRail 基类本身无日志输出（no-op 钩子）
- Python AgentCallbackManager.register_rail/unregister_rail 无日志
- Go 侧在 RegisterRail/UnregisterRail 中添加 Debug 级别日志（防御性日志，记录每个钩子的注册/注销事件），对齐 Python 中 @rail 装饰器的日志风格

## 不实现的内容

- **EVENT_METHOD_MAP**：方案A2 下用户显式声明，不需要事件→方法名映射
- **_is_base_method**：Go 无反射等价能力，由 GetCallbacks() 显式声明替代
- **@rail 装饰器**：属于 6.8 范围
- **RunKind/HeartbeatReason/RunContext**：属于 6.9 Inputs 的辅助类型
- **RetryRequest/ForceFinishRequest**：属于 6.10 范围
