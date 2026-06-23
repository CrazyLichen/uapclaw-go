# 6.2 BaseAgent 接口实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 BaseAgent 接口（Configure/Invoke/Stream + 全部公开方法）和 WarpBaseAgent 默认实现（含回调包装骨架），新增 Agent 事件域到 CallbackFramework

**Architecture:** BaseAgent 接口定义在 interfaces 包，WarpBaseAgent 默认实现在 single_agent 包。WarpBaseAgent 通过 agentInvoker 接口字段实现虚分发，Invoke/Stream 方法体包含 emit_before → invokeImpl → emit_after 的回调包装骨架。Agent 回调域（AgentCallEventType + OnAgent/TriggerAgent）新增到 CallbackFramework。

**Tech Stack:** Go 1.24, 依赖已有包：single_agent/schema, session, session/stream, runner/callback, common/exception, common/logger

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `internal/agentcore/single_agent/interfaces/interface.go` | BaseAgent 接口定义 + AgentOptions |
| Modify | `internal/agentcore/single_agent/interfaces/doc.go` | 更新包文档 |
| Modify | `internal/agentcore/runner/callback/events.go` | AgentCallEventType/EventData/CallbackFunc |
| Modify | `internal/agentcore/runner/callback/framework.go` | OnAgent/OffAgent/TriggerAgent |
| Modify | `internal/agentcore/runner/callback/doc.go` | 更新包文档 |
| Modify | `internal/common/exception/codes_agent.go` | StatusAgentNotConfigured |
| Create | `internal/agentcore/single_agent/base.go` | agentInvoker + WarpBaseAgent |
| Create | `internal/agentcore/single_agent/base_test.go` | 全部单元测试 |
| Modify | `internal/agentcore/single_agent/resource_manager.go` | GetAgent 返回 BaseAgent |
| Modify | `internal/agentcore/single_agent/ability_manager.go` | 移除 SetContextEngine 预留语义 |
| Modify | `internal/agentcore/single_agent/doc.go` | 更新文件目录 |
| Modify | `IMPLEMENTATION_PLAN.md` | 6.2 状态更新 |

---

### Task 1: 新增错误码 StatusAgentNotConfigured

**Files:**
- Modify: `internal/common/exception/codes_agent.go`

- [ ] **Step 1: 在 codes_agent.go 的 Agent Orchestration 区间新增错误码**

在 `StatusAbilityMalformedArguments` (120007) 之后添加：

```go
// StatusAgentNotConfigured Agent 未配置（invoker 未设置）
StatusAgentNotConfigured = NewStatusCode(
	"AGENT_NOT_CONFIGURED", 120008,
	"agent not configured, invoker not set, reason: {error_msg}")
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/common/exception/...`
Expected: 编译通过

- [ ] **Step 3: Commit**

```bash
git add internal/common/exception/codes_agent.go
git commit -m "feat(exception): 新增 StatusAgentNotConfigured 错误码 (120008)"
```

---

### Task 2: 新增 AgentCallEventType + AgentCallEventData + AgentCallbackFunc

**Files:**
- Modify: `internal/agentcore/runner/callback/events.go`

- [ ] **Step 1: 在 events.go 末尾（非导出函数区块之前）新增 Agent 事件类型**

在 `ContextCallEventType` 常量块之后、`NewToolCallEventData` 函数之前，添加：

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

需要在 events.go 的 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/session"`。

同时在导出函数区块添加 Stringer 实现：

```go
// String 实现 fmt.Stringer 接口。
func (t AgentCallEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *AgentCallEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("AgentCallEventData{事件:%s, AgentID:%s}", d.Event, d.AgentID)
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/runner/callback/...`
Expected: 编译通过

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/runner/callback/events.go
git commit -m "feat(callback): 新增 AgentCallEventType/AgentCallEventData/AgentCallbackFunc"
```

---

### Task 3: CallbackFramework 新增 OnAgent/OffAgent/TriggerAgent

**Files:**
- Modify: `internal/agentcore/runner/callback/framework.go`

- [ ] **Step 1: 在 CallbackFramework 结构体新增 agentCallbacks 字段**

在 `contextCallbacks` 字段之后添加：

```go
	// agentCallbacks Agent 回调函数注册表
	agentCallbacks map[AgentCallEventType][]AgentCallbackFunc
```

- [ ] **Step 2: 在 NewCallbackFramework 中初始化 agentCallbacks**

在 `contextCallbacks: make(map[ContextCallEventType][]ContextCallbackFunc),` 之后添加：

```go
		agentCallbacks:    make(map[AgentCallEventType][]AgentCallbackFunc),
```

- [ ] **Step 3: 在 TriggerContext 方法之后、非导出函数区块之前，添加 OnAgent/OffAgent/TriggerAgent**

```go
// OnAgent 注册 Agent 事件回调函数。
//
// 同一事件可注册多个回调，按注册顺序执行。
//
// 对应 Python: AsyncCallbackFramework.on(event, callback)
func (fw *CallbackFramework) OnAgent(event AgentCallEventType, fn AgentCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.agentCallbacks[event] = append(fw.agentCallbacks[event], fn)
}

// OffAgent 注销 Agent 事件回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按指针匹配）。
// 若事件下无匹配回调，不做任何操作。
//
// 对应 Python: AsyncCallbackFramework.unregister(event, callback)
func (fw *CallbackFramework) OffAgent(event AgentCallEventType, fn AgentCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.agentCallbacks[event]
	if !ok {
		return
	}

	for i, cb := range callbacks {
		if fmt.Sprintf("%p", cb) == fmt.Sprintf("%p", fn) {
			fw.agentCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerAgent 触发 Agent 事件，按注册顺序调用所有回调，返回所有回调结果。
//
// 若 ctx 为 nil 或 data 为 nil，直接返回 nil。
//
// 对应 Python: AsyncCallbackFramework.trigger(event, **kwargs) → List[Any]
func (fw *CallbackFramework) TriggerAgent(ctx context.Context, data *AgentCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	fw.mu.RLock()
	callbacks := fw.agentCallbacks[data.Event]
	fw.mu.RUnlock()

	results := make([]any, 0, len(callbacks))
	for _, fn := range callbacks {
		result := fn(ctx, data)
		results = append(results, result)
	}
	return results
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/runner/callback/...`
Expected: 编译通过

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/runner/callback/framework.go
git commit -m "feat(callback): CallbackFramework 新增 OnAgent/OffAgent/TriggerAgent"
```

---

### Task 4: BaseAgent 接口定义 + AgentOptions 扩展

**Files:**
- Modify: `internal/agentcore/single_agent/interfaces/interface.go`
- Modify: `internal/agentcore/single_agent/interfaces/doc.go`

- [ ] **Step 1: 重写 interfaces/interface.go**

将 `Agent` 接口重命名为 `BaseAgent` 并扩展完整方法集，充实 AgentOptions。替换整个文件内容：

```go
package interfaces

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Workflow 工作流执行接口（最小定义，领域八扩展）。
//
// 对应 Python: openjiuwen/core/workflow/workflow.py (Workflow)
// Python 的 Workflow 有 invoke/stream/card 三个能力，
// Go 当前定义 Invoke/Stream/Card 三个方法，对齐 Python。
// Invoke 返回值暂用 (any, error)，领域八扩展为 (*WorkflowOutput, error)。
type Workflow interface {
	// Invoke 非流式调用工作流
	//
	// 对应 Python: Workflow.invoke(inputs, session, context, **kwargs) -> WorkflowOutput
	Invoke(ctx context.Context, inputs map[string]any, opts ...WorkflowOption) (any, error)
	// Stream 流式调用工作流
	//
	// 对应 Python: Workflow.stream(inputs, session, context, stream_modes, **kwargs) -> AsyncIterator[WorkflowChunk]
	// 返回 channel 中的 stream.Schema 对应 Python 的 WorkflowChunk = Union[OutputSchema, CustomSchema, TraceSchema]。
	Stream(ctx context.Context, inputs map[string]any, opts ...WorkflowOption) (<-chan stream.Schema, error)
	// Card 返回工作流配置卡片
	//
	// 对应 Python: Workflow.card 属性（@property）
	// 用于 tracer 装饰器提取 instanceInfo.metadata（id/name/description/version）。
	Card() *schema.WorkflowCard
}

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
	// ⤵️ 返回类型暂用 any，避免 single_agent → single_agent 循环依赖；
	// 调用方通过类型断言获取 *AbilityManager。6.2 完成后可考虑提取接口。
	AbilityManager() any

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

// WorkflowOptions 工作流执行选项（预留）。
type WorkflowOptions struct{}

// AgentOptions Agent 调用选项。
type AgentOptions struct {
	// Session 会话实例（可选）
	// 对应 Python: invoke(inputs, session) / stream(inputs, session, stream_modes) 的 session 参数
	Session *session.Session
	// StreamModes 流式输出模式（可选）
	// 对应 Python: stream(inputs, session, stream_modes) 的 stream_modes 参数
	StreamModes []stream.StreamMode
}

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WorkflowOption 工作流执行选项函数（预留，领域八扩展）。
type WorkflowOption func(*WorkflowOptions)

// AgentOption Agent 调用选项函数。
type AgentOption func(*AgentOptions)

// WithSession 设置会话实例。
func WithSession(sess *session.Session) AgentOption {
	return func(o *AgentOptions) { o.Session = sess }
}

// WithStreamModes 设置流式输出模式。
func WithStreamModes(modes []stream.StreamMode) AgentOption {
	return func(o *AgentOptions) { o.StreamModes = modes }
}

// NewAgentOptions 从选项列表构建 AgentOptions。
func NewAgentOptions(opts ...AgentOption) *AgentOptions {
	o := &AgentOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

注意：`AbilityManager()` 返回 `any` 而非 `*single_agent.AbilityManager`，因为 interfaces 包不能导入 single_agent 包（会产生循环依赖：single_agent → interfaces → single_agent）。调用方通过类型断言获取具体类型。

- [ ] **Step 2: 更新 interfaces/doc.go**

更新包文档，反映 BaseAgent 接口变更。

- [ ] **Step 3: 适配所有引用 interfaces.Agent 的代码**

搜索所有 `interfaces.Agent` 引用并改为 `interfaces.BaseAgent`。已知引用位置：
- `resource_manager.go`: `GetAgent(...) (interfaces.Agent, error)` → `GetAgent(...) (interfaces.BaseAgent, error)`
- `resource_manager_test.go`: `fakeResourceManager.GetAgent` 返回类型 + `fakeAgent` 实现接口

`fakeAgent` 需要实现新增的 BaseAgent 方法（Configure/Card/Config/AbilityManager/CallbackManager/RegisterCallback/RegisterRail/UnregisterRail），提供最小桩实现。

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/...`
Expected: 编译通过

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/single_agent/interfaces/ internal/agentcore/single_agent/resource_manager.go internal/agentcore/single_agent/resource_manager_test.go
git commit -m "feat(interfaces): Agent 重命名为 BaseAgent，扩展完整方法集，充实 AgentOptions"
```

---

### Task 5: WarpBaseAgent 默认实现

**Files:**
- Create: `internal/agentcore/single_agent/base.go`

- [ ] **Step 1: 创建 base.go**

```go
package single_agent

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// agentInvoker 子类真实执行接口，用于虚分发。
//
// Go 内嵌结构体无法虚分发到子类方法（w.invokeImpl() 在编译期
// 绑定 WarpBaseAgent.invokeImpl，不会调用 ReActAgent.invokeImpl）。
// 通过接口字段 invoker agentInvoker 实现等价虚方法表：
// 构造时 agent.invoker = agent，调用 w.invoker.invokeImpl() 走虚分发。
type agentInvoker interface {
	// invokeImpl 子类实现的非流式调用逻辑
	invokeImpl(ctx context.Context, inputs map[string]any, opts ...AgentOption) (any, error)
	// streamImpl 子类实现的流式调用逻辑
	streamImpl(ctx context.Context, inputs map[string]any, opts ...AgentOption) (<-chan stream.Schema, error)
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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWarpBaseAgent 创建 WarpBaseAgent 实例。
func NewWarpBaseAgent(card *agentschema.AgentCard, resourceMgr ResourceManager) *WarpBaseAgent {
	return &WarpBaseAgent{
		card:           card,
		abilityManager: NewAbilityManager(resourceMgr),
	}
}

// Configure 配置 Agent。
// 对应 Python: BaseAgent.configure(config)
func (w *WarpBaseAgent) Configure(_ context.Context, config any) error {
	w.config = config
	return nil
}

// Invoke 非流式调用，包含回调包装骨架。
// 执行顺序：① emit_before → ② transform_io(输入) → invokeImpl → ② transform_io(输出) → ③ emit_after
//
// 对应 Python: _AgentMeta 元类装饰后的 invoke
func (w *WarpBaseAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...AgentOption) (any, error) {
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

// Stream 流式调用，包含回调包装骨架。
// 执行顺序：① emit_before → streamImpl → per-item { ② transform_io(输出) → ③ emit_after }
//
// 对应 Python: _AgentMeta 元类装饰后的 stream
func (w *WarpBaseAgent) Stream(ctx context.Context, inputs map[string]any, opts ...AgentOption) (<-chan stream.Schema, error) {
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

// Card 返回 Agent 身份卡片。
func (w *WarpBaseAgent) Card() *agentschema.AgentCard { return w.card }

// Config 返回当前配置。
func (w *WarpBaseAgent) Config() any { return w.config }

// AbilityManager 返回能力管理器。
func (w *WarpBaseAgent) AbilityManager() any { return w.abilityManager }

// CallbackManager 返回回调管理器。
// ⤵️ 6.6 回填：返回类型从 any 改为 *AgentCallbackManager
func (w *WarpBaseAgent) CallbackManager() any { return w.callbackManager }

// RegisterCallback 注册回调。
// ⤵️ 预留：6.4-6.6 实现后委托给 AgentCallbackManager
func (w *WarpBaseAgent) RegisterCallback(_ context.Context, _ any, _ any, _ int) error {
	return nil
}

// RegisterRail 注册 Rail。
// ⤵️ 预留：6.7 实现后委托给 AgentCallbackManager
func (w *WarpBaseAgent) RegisterRail(_ context.Context, _ any) error {
	return nil
}

// UnregisterRail 注销 Rail。
// ⤵️ 预留：6.7 实现后委托给 AgentCallbackManager
func (w *WarpBaseAgent) UnregisterRail(_ context.Context, _ any) error {
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

注意：`WarpBaseAgent` 的 `Invoke/Stream` 方法签名中使用 `AgentOption`（来自同一 `single_agent` 包的 type alias，或直接引用 `interfaces.AgentOption`）。由于 `interfaces.AgentOption` 需要导入 interfaces 包，而 interfaces 包又引用 single_agent/schema，不构成循环依赖。需在 base.go 的 import 中添加 interfaces 包的别名。

实际上 `AgentOption` 已经在 `ability_manager.go` 中通过 `interfaces.AgentOption` 使用。base.go 同样通过 `interfaces.AgentOption` 引用即可。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/single_agent/...`
Expected: 编译通过

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/single_agent/base.go
git commit -m "feat(single_agent): 新增 agentInvoker 接口 + WarpBaseAgent 默认实现"
```

---

### Task 6: WarpBaseAgent 单元测试

**Files:**
- Create: `internal/agentcore/single_agent/base_test.go`

- [ ] **Step 1: 创建 base_test.go，包含全部测试用例**

测试覆盖：
- `TestNewWarpBaseAgent` 构造函数
- `TestWarpBaseAgent_Invoke_正常调用`
- `TestWarpBaseAgent_Invoke_invoker未设置`
- `TestWarpBaseAgent_Invoke_触发回调`（注册 AgentCallbackFunc，验证 TriggerAgent 被调用）
- `TestWarpBaseAgent_Invoke_子类错误透传`（invokeImpl 返回 BaseError）
- `TestWarpBaseAgent_Invoke_普通错误包装`（invokeImpl 返回普通 error）
- `TestWarpBaseAgent_Stream_正常调用`
- `TestWarpBaseAgent_Stream_invoker未设置`
- `TestWarpBaseAgent_Stream_每项触发回调`
- `TestWarpBaseAgent_Configure`
- `TestWarpBaseAgent_访问器`
- `TestWarpBaseAgent_虚分发`（子类内嵌 + 实现 agentInvoker）
- `TestCallbackFramework_OnAgent_TriggerAgent`
- `TestCallbackFramework_OffAgent`
- `TestAgentCallEventType_事件名对齐Python`

需要定义测试辅助类型：
- `stubInvoker`：实现 agentInvoker 接口的桩
- `stubSubAgent`：内嵌 WarpBaseAgent + 实现 agentInvoker，验证虚分发

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/... -run "TestNewWarpBaseAgent|TestWarpBaseAgent|TestCallbackFramework_OnAgent|TestCallbackFramework_OffAgent|TestAgentCallEventType" -v`
Expected: 全部 PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/single_agent/base_test.go
git commit -m "test(single_agent): WarpBaseAgent + AgentCallbackFramework 单元测试"
```

---

### Task 7: 移除 AbilityManager.SetContextEngine 预留语义 + 更新 doc.go

**Files:**
- Modify: `internal/agentcore/single_agent/ability_manager.go`
- Modify: `internal/agentcore/single_agent/doc.go`

- [ ] **Step 1: 移除 ability_manager.go 中 SetContextEngine 的预留注释**

将注释 `"SetContextEngine 设置上下文引擎。"` 保持不变，但确认其实现是完整的（不再是"预留"状态）。如果注释中有"预留"字样则移除。

- [ ] **Step 2: 更新 single_agent/doc.go 文件目录**

在文件目录树中添加 base.go 条目。

- [ ] **Step 3: 更新 callback/doc.go 文件目录**

在文件目录树中反映 Agent 事件域的新增。

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/...`
Expected: 编译通过

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/single_agent/ability_manager.go internal/agentcore/single_agent/doc.go internal/agentcore/runner/callback/doc.go
git commit -m "docs: 移除 SetContextEngine 预留语义，更新 doc.go 文件目录"
```

---

### Task 8: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 6.2 步骤状态从 ☐ 改为 ✅**

找到 `| 6.2 | ☐ | BaseAgent 接口` 行，将 ☐ 改为 ✅，并更新产出描述为已实现内容。

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 6.2 状态为 ✅"
```

---

### Task 9: 最终编译与测试验证

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译通过

- [ ] **Step 2: 运行受影响包的测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/... ./internal/agentcore/runner/callback/... ./internal/common/exception/... -v`
Expected: 全部 PASS

- [ ] **Step 3: 运行全量测试（可选，耗时长）**

Run: `cd /home/opensource/uap-claw-go && go test ./... -count=1`
Expected: 全部 PASS（无回归）
