# 9.67 Team Observability — Go 实现设计

## 概述

9.67 实现 Team Observability 三层可观测性基础设施，将 Python `openjiuwen/agent_teams/observability/` 的 9 个文件移植为 Go 包。

### 在 Agent 会话流程中的位置与作用

```
用户消息 → Gateway → AgentServer → TeamAgent
                                      ├─ EventMessage 流 ──→ OtelTeamMonitorHandler（Monitor 层）
                                      └─ DeepAgent ReAct 循环
                                           ├─ task_iteration ──→ ObservabilityRail（Rail 层）
                                           ├─ LLM 调用 ──────→ OtelCallbackHandler（Callback 层）
                                           ├─ Tool 调用 ─────→ OtelCallbackHandler
                                           └─ Agent 调用 ────→ OtelCallbackHandler
```

三层各司其职，互不重叠：
- **Callback 层**：拦截 LLM/Tool/Agent 的 input/output 事件，生成 OpenTelemetry span
- **Monitor 层**：消费 TeamAgent EventMessage 事件流，生成 team/task 级别 span
- **Rail 层**：覆盖 DeepAgent 外层 task_iteration 边界（Callback 层未覆盖的缺口）

### 依赖关系

- 9.55 TeamAgent（`add_event_listener`）：Monitor 层需要，**暂用桩实现**，9.55 完成后回填
- 9.68-69 Team Rails/Prompts：不需要提前实现，Rail 系统已支持注册

### 回填标记

- 本节无需回填之前章节的 ⤵️/⤴️ 标记
- 9.55 完成后需回填 `attach_to_team_agent` 从桩到真实实现

---

## 核心设计决策

### 决策 1：SpanState 通过 ctx 注入（对齐 SessionState 模式）

**Python 方式**：`contextvars.ContextVar` 自动按 asyncio Task 隔离 span 栈。Go 无等价机制。

**Go 方案**：和 `SessionState`、`CwdState` 完全一致的模式：

```go
// OtelSpanState 每-DeepAgent 的可变 span 追踪状态容器。
// Python: span_context.py 中 ContextVar 的集合
//
// 通过 context.Value 传播 *OtelSpanState 指针：
//   - 同一 DeepAgent 内的 goroutine 共享同一 OtelSpanState 引用
//   - 子 DeepAgent 创建新实例 + WithSpanState 派生新 ctx，父不受影响
//
// 并发安全：所有字段读写通过 sync.Mutex 保护。
type OtelSpanState struct {
    mu           sync.Mutex
    llmStack     []*LlmSpanState  // LLM span 栈（LIFO）
    toolSpanMap  map[string][]trace.Span  // Tool span map，key=toolName
    agentSpanMap map[string][]trace.Span  // Agent span map，key=agentID
}
```

**注入时机**：在 `DeepAgent.ensureInitialized` 中（和 CwdState 同一位置）：

```go
// 在 CWD 初始化之后注入 SpanState
if otel.SpanStateFromCtx(ctx) == nil {
    spanState := otel.InitSpanState()
    ctx = otel.WithSpanState(ctx, spanState)
}
```

**读取时机**：OtelCallbackHandler 回调中通过 `ctx` 直接读取：

```go
func (h *OtelCallbackHandler) onLLMInvokeInput(ctx context.Context, data *callback.LLMCallEventData) any {
    spanState := otel.SpanStateFromCtx(ctx)
    if spanState == nil {
        return nil  // 未启用可观测性
    }
    // 打开 span → push 到 spanState.llmStack
    ...
}
```

**关键依据**：经代码验证，Go CallbackFramework 的 `TriggerLLM`/`TriggerTool`/`TriggerGlobalAgent` 将 `ctx` 原封不动传给回调函数。回调函数收到的 ctx 与触发时使用的是同一个（或继承所有值的子 ctx），所以 ctx 中设置的值回调一定可见。

**并发隔离**：每个 DeepAgent 有自己的 ctx（子 goroutine 通过 `WithSpanState` 派生新 ctx），天然隔离，无需 sync.Map。

### 决策 2：全局 OtelCallbackHandler + 10 个类型安全方法

**Python 方式**：`OtelCallbackHandler` 是全局单例，通过 `framework.register_sync(event, bound_method, namespace=...)` 注册。

**Go 方案**：全局单例 + 10 个方法匹配 Go CallbackFramework 类型安全签名：

```go
type OtelCallbackHandler struct {
    config         *ObservabilityConfig
    injectedTracer trace.Tracer  // 可选，测试时注入
}

// LLM 回调（5 个）
func (h *OtelCallbackHandler) onLLMInvokeInput(ctx context.Context, data *callback.LLMCallEventData) any
func (h *OtelCallbackHandler) onLLMStreamInput(ctx context.Context, data *callback.LLMCallEventData) any
func (h *OtelCallbackHandler) onLLMStreamOutput(ctx context.Context, data *callback.LLMCallEventData) any
func (h *OtelCallbackHandler) onLLMInvokeOutput(ctx context.Context, data *callback.LLMCallEventData) any
func (h *OtelCallbackHandler) onLLMCallError(ctx context.Context, data *callback.LLMCallEventData) any

// Tool 回调（3 个）
func (h *OtelCallbackHandler) onToolCallStarted(ctx context.Context, data *callback.ToolCallEventData) any
func (h *OtelCallbackHandler) onToolCallFinished(ctx context.Context, data *callback.ToolCallEventData) any
func (h *OtelCallbackHandler) onToolCallError(ctx context.Context, data *callback.ToolCallEventData) any

// Agent 回调（2 个）
func (h *OtelCallbackHandler) onAgentInvokeInput(ctx context.Context, data *callback.GlobalAgentEventData) any
func (h *OtelCallbackHandler) onAgentInvokeOutput(ctx context.Context, data *callback.GlobalAgentEventData) any
```

### 决策 3：Callback 注册/注销通过 Namespace

注册时指定 `WithNamespace("agent_teams.observability")`：

```go
fw.OnLLM(LLMInvokeInput, handler.onLLMInvokeInput, callback.WithNamespace(namespace))
```

注销时通过 `UnregisterByNamespace` 批量移除。需要扩展 `UnregisterNamespace` 方法使其也遍历 `llmCallbacks`/`toolCallbacks`/`globalAgentCallbacks`（当前只处理 customCallbacks 和 perAgentCallbacks）。

### 决策 4：ObservabilityRail 通过 extra 存储 task_iteration span

**Python 方式**：`ctx.extra[_SPAN_KEY] = span`，after 时 `ctx.extra.pop(_SPAN_KEY)`。

**Go 方案**：Go 的 `AgentCallbackContext` 也有 `extra map[string]any` 字段，对齐 Python：

```go
const spanKey = "_otel_task_iter_span"

func (r *ObservabilityRail) BeforeTaskIteration(ctx context.Context, railCtx *agentinterfaces.AgentCallbackContext) error {
    span := r.tracer().Start(ctx, "deepagent.task_iteration.N", trace.WithSpanKind(trace.SpanKindInternal))
    railCtx.Extra[spanKey] = span
    return nil
}

func (r *ObservabilityRail) AfterTaskIteration(ctx context.Context, railCtx *agentinterfaces.AgentCallbackContext) error {
    span, _ := railCtx.Extra[spanKey].(trace.Span)
    delete(railCtx.Extra, spanKey)
    // 关闭 span
    ...
    return nil
}
```

### 决策 5：Monitor 层暂用桩实现

`attach_to_team_agent` 为 no-op，待 9.55 TeamAgent 实现后回填。`OtelTeamMonitorHandler` 结构体和方法逻辑照写，但不注册到 TeamAgent。

---

## 文件目录

```
internal/agent_teams/observability/
├── doc.go               # 包文档
├── config.go            # ObservabilityConfig 配置结构体
├── semconv.go           # 语义约定常量（gen_ai.* / agentteam.* / deepagent.*）
├── redaction.go         # Prompt/Completion 脱敏（SHA-256 哈希或截断）
├── span_state.go        # OtelSpanState + ctx 注入（对齐 SessionState 模式）
├── callback_handler.go  # OtelCallbackHandler — 10 个回调方法
├── monitor_handler.go   # OtelTeamMonitorHandler — 桩实现
├── rail.go              # ObservabilityRail（嵌入 DeepAgentRail）
├── setup.go             # init_observability / shutdown_observability / get_tracer / attach_to_team_agent
└── observability.go     # 公共 API 导出（对齐 Python __init__.py）
```

对应 Python 源码：`openjiuwen/agent_teams/observability/`

---

## 各文件详细设计

### 1. config.go — ObservabilityConfig

对齐 `config.py`：

```go
type ObservabilityConfig struct {
    Enabled                  bool
    ServiceName              string
    Exporter                 string  // "otlp_grpc" | "otlp_http" | "console"
    Endpoint                 string
    SampleRate               float64
    RedactPrompts            bool
    RedactCompletions        bool
    AttributeValueMaxLength  int
    ExportTimeoutMs          int
}

func DefaultObservabilityConfig() *ObservabilityConfig
```

### 2. semconv.go — 语义约定常量

对齐 `semconv.py`，三组常量：

```go
// OpenLLMetry / GenAI 标准属性
const (
    GenAISystem             = "gen_ai.system"
    GenAIRequestModel       = "gen_ai.request.model"
    GenAIRequestTemperature = "gen_ai.request.temperature"
    GenAIRequestTopP        = "gen_ai.request.top_p"
    GenAIRequestMaxTokens   = "gen_ai.request.max_tokens"
    GenAIPrompt             = "gen_ai.prompt"
    GenAICompletion         = "gen_ai.completion"
    GenAIUsagePromptTokens  = "gen_ai.usage.prompt_tokens"
    GenAIUsageCompletionTokens = "gen_ai.usage.completion_tokens"
    GenAIUsageTotalTokens   = "gen_ai.usage.total_tokens"
    GenAIResponseFinishReason = "gen_ai.response.finish_reason"
    GenAIResponseModel      = "gen_ai.response.model"
    GenAIResponseTTFTMs     = "gen_ai.response.time_to_first_token_ms"
    GenAIToolName           = "gen_ai.tool.name"
    GenAIToolInput          = "gen_ai.tool.input"
    GenAIToolOutput         = "gen_ai.tool.output"
)

// agentteam.* — Team 协作属性
const (
    ATTeamName        = "agentteam.team.name"
    ATTeamDisplayName = "agentteam.team.display_name"
    ATEventType       = "agentteam.event_type"
    ATAgentID         = "agentteam.agent.id"
    ATAgentRole       = "agentteam.agent.role"
    ATAgentInput      = "agentteam.agent.input"
    ATAgentOutput     = "agentteam.agent.output"
    // ... member / message / task 属性同 Python
)

// deepagent.* — DeepAgent task-loop 属性
const (
    DATaskIteration  = "deepagent.task.iteration"
    DATaskIsFollowUp = "deepagent.task.is_follow_up"
)
```

### 3. redaction.go — 脱敏工具

对齐 `redaction.py`：

```go
func redactPrompt(value any, config *ObservabilityConfig) string
func redactCompletion(value any, config *ObservabilityConfig) string
func truncate(value string, maxLength int) string
func hashValue(value string) string
```

### 4. span_state.go — OtelSpanState（核心）

对齐 `span_context.py`，但改为通过 ctx 传播而非 ContextVar：

```go
// LlmSpanState 单次 LLM 调用的 span 追踪状态。
// Python: LlmSpanState dataclass
type LlmSpanState struct {
    Span         trace.Span
    StartNs      int64           // time.Now().UnixNano()
    FirstChunkNs int64           // 首个 stream chunk 时间（0 表示未到达）
    ChunkCount   int             // stream chunk 计数
}

func (s *LlmSpanState) NextChunkSeq() int {
    s.ChunkCount++
    return s.ChunkCount
}

// OtelSpanState 每-DeepAgent 的可变 span 追踪状态容器。
// Python: _llm_span_stack + _tool_span_map + _agent_span_map (3 个 ContextVar)
//
// 通过 context.Value 传播 *OtelSpanState 指针（同 SessionState 模式）。
type OtelSpanState struct {
    mu           sync.Mutex
    llmStack     []*LlmSpanState
    toolSpanMap  map[string][]trace.Span  // key=toolName
    agentSpanMap map[string][]trace.Span  // key=agentID
}

type spanStateKeyType struct{}

func InitSpanState() *OtelSpanState
func WithSpanState(ctx context.Context, state *OtelSpanState) context.Context
func SpanStateFromCtx(ctx context.Context) *OtelSpanState

// LLM span 栈操作（LIFO）
func (s *OtelSpanState) PushLlmSpanState(state *LlmSpanState)
func (s *OtelSpanState) PopLlmSpanState(peek bool) *LlmSpanState

// Tool span 操作
func (s *OtelSpanState) PushToolSpan(toolName string, span trace.Span)
func (s *OtelSpanState) PopToolSpan(toolName string) trace.Span

// Agent span 操作
func (s *OtelSpanState) PushAgentSpan(agentID string, span trace.Span)
func (s *OtelSpanState) PopAgentSpan(agentID string) trace.Span

// 重置（测试用）
func (s *OtelSpanState) ResetAll()
```

**与 Python 的关键差异**：
- Python 用 3 个独立 ContextVar，Go 合并为一个 `OtelSpanState` 结构体（因为 Go 的 context.Value 是单 key）
- Python `LlmSpanState` 有 `context_token` 字段用于 `otel_context.detach`，Go 不需要此字段（原因见下方）

**Span 父子链对齐（核心断链修复）**：

Python 在 `_open_llm_span` 中用 `otel_context.attach(set_span_in_context(span))` 把 LLM span 设为 OTel 全局当前 context，后续同一 asyncio Task 中创建的子 span（Tool、嵌套 LLM）自动继承它为 parent。Go 的 `context.Context` 是不可变值，且回调返回 `any` 不是 `context.Context`，所以 `onLLMInvokeInput` 中对 ctx 的修改无法传回给后续回调——**下游回调收到的是原始 ctx，不含 LLM span**，`tracer.Start(ctx, ...)` 会创建根 span 而非子 span。

修复方式：创建子 span 时，从 `OtelSpanState` 栈顶取当前 LLM span 的 `SpanContext`，显式注入到 ctx：

```go
// OtelSpanState 新增方法
func (s *OtelSpanState) CurrentLLMSpanContext() trace.SpanContext {
    s.mu.Lock()
    defer s.mu.Unlock()
    if len(s.llmStack) == 0 {
        return trace.SpanContext{}  // 空 → 根 span
    }
    return s.llmStack[len(s.llmStack)-1].Span.SpanContext()
}

// Tool span 创建时：以当前 LLM span 为 parent
func (h *OtelCallbackHandler) onToolCallStarted(ctx context.Context, data *callback.ToolCallEventData) any {
    spanState := SpanStateFromCtx(ctx)
    if spanState == nil { return nil }
    parentCtx := trace.ContextWithSpanContext(ctx, spanState.CurrentLLMSpanContext())
    _, toolSpan := h.tracer().Start(parentCtx, "tool."+toolName, trace.WithSpanKind(trace.SpanKindInternal))
    spanState.PushToolSpan(toolName, toolSpan)
    return nil
}

// 嵌套 LLM 调用同理：新 LLM span 自动成为栈顶 LLM span 的子 span
func (h *OtelCallbackHandler) openLlmSpan(ctx context.Context, data *callback.LLMCallEventData, spanState *OtelSpanState) {
    parentCtx := trace.ContextWithSpanContext(ctx, spanState.CurrentLLMSpanContext())
    _, llmSpan := h.tracer().Start(parentCtx, "llm.call", trace.WithSpanKind(trace.SpanKindClient))
    spanState.PushLlmSpanState(&LlmSpanState{Span: llmSpan, StartNs: time.Now().UnixNano()})
}
```

**Python ↔ Go Span 父子链等价映射表**：

| Python | Go | 说明 |
|--------|-----|------|
| `otel_context.attach(set_span_in_context(span))` | `spanState.PushLlmSpanState(state)` + 创建子 span 时 `trace.ContextWithSpanContext(ctx, spanState.CurrentLLMSpanContext())` | Python 隐式继承（contextvars 全局生效），Go 显式注入（从栈取 parent） |
| `otel_context.detach(token)` | 不需要 | Go ctx 不可变，无全局状态需恢复 |
| `LlmSpanState.context_token` | 不需要此字段 | Go 不做 attach/detach |
| `self._tracer().start_span(name=..., kind=...)` | `h.tracer().Start(parentCtx, name, trace.WithSpanKind(...))` | Go 需显式传入含 parent 的 ctx |
| `self._tracer().start_as_current_span(name=..., context=set_span_in_context(state.span))` | `h.tracer().Start(trace.ContextWithSpan(context.Background(), state.Span), name, ...)` | reasoning span 作为 LLM span 的子 span |

### 5. callback_handler.go — OtelCallbackHandler

对齐 `callback_handler.py`，10 个方法。每个方法通过 `SpanStateFromCtx(ctx)` 获取 span 状态：

```go
type OtelCallbackHandler struct {
    config         *ObservabilityConfig
    injectedTracer trace.Tracer
}

func (h *OtelCallbackHandler) tracer() trace.Tracer

// LLM
func (h *OtelCallbackHandler) onLLMInvokeInput(ctx context.Context, data *callback.LLMCallEventData) any
func (h *OtelCallbackHandler) onLLMStreamInput(ctx context.Context, data *callback.LLMCallEventData) any
func (h *OtelCallbackHandler) onLLMStreamOutput(ctx context.Context, data *callback.LLMCallEventData) any
func (h *OtelCallbackHandler) onLLMInvokeOutput(ctx context.Context, data *callback.LLMCallEventData) any
func (h *OtelCallbackHandler) onLLMCallError(ctx context.Context, data *callback.LLMCallEventData) any

// Tool
func (h *OtelCallbackHandler) onToolCallStarted(ctx context.Context, data *callback.ToolCallEventData) any
func (h *OtelCallbackHandler) onToolCallFinished(ctx context.Context, data *callback.ToolCallEventData) any
func (h *OtelCallbackHandler) onToolCallError(ctx context.Context, data *callback.ToolCallEventData) any

// Agent
func (h *OtelCallbackHandler) onAgentInvokeInput(ctx context.Context, data *callback.GlobalAgentEventData) any
func (h *OtelCallbackHandler) onAgentInvokeOutput(ctx context.Context, data *callback.GlobalAgentEventData) any

// 内部辅助
func (h *OtelCallbackHandler) openLlmSpan(ctx context.Context, data *callback.LLMCallEventData)
func (h *OtelCallbackHandler) closeLlmSpan(state *LlmSpanState, response any)
func (h *OtelCallbackHandler) maybeRecordResponseAttrs(state *LlmSpanState, response any)
```

**回调方法中的 span 状态读写模式**：

```go
// Input 回调：创建 span → push 到栈
func (h *OtelCallbackHandler) onLLMInvokeInput(ctx context.Context, data *callback.LLMCallEventData) any {
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(logComponent).Any("error", r).Msg("otel: onLLMInvokeInput failed")
        }
    }()
    spanState := SpanStateFromCtx(ctx)
    if spanState == nil {
        return nil
    }
    h.openLlmSpan(ctx, data, spanState)
    return nil
}

// Output 回调：pop 栈顶 → 关闭 span
func (h *OtelCallbackHandler) onLLMInvokeOutput(ctx context.Context, data *callback.LLMCallEventData) any {
    defer func() { /* 同上 recover */ }()
    spanState := SpanStateFromCtx(ctx)
    if spanState == nil {
        return nil
    }
    state := spanState.PopLlmSpanState(false)  // pop
    if state == nil {
        return nil
    }
    h.closeLlmSpan(state, data.Response)
    return nil
}

// Stream Output 回调：peek 栈顶 → 记录 chunk
func (h *OtelCallbackHandler) onLLMStreamOutput(ctx context.Context, data *callback.LLMCallEventData) any {
    spanState := SpanStateFromCtx(ctx)
    if spanState == nil {
        return nil
    }
    state := spanState.PopLlmSpanState(true)  // peek
    if state == nil {
        return nil
    }
    seq := state.NextChunkSeq()
    if state.FirstChunkNs == 0 {
        state.FirstChunkNs = time.Now().UnixNano()
        ttftMs := float64(state.FirstChunkNs-state.StartNs) / 1e6
        state.Span.SetAttributes(attribute.Float64(GenAIResponseTTFTMs, ttftMs))
    }
    // 记录 chunk event
    ...
    return nil
}
```

**Python `context_token` / `otel_context.attach` 的 Go 替代方案**：

Python 在 `_open_llm_span` 中用 `otel_context.attach(set_span_in_context(span))` 将新 span 设为 OTel 当前 context，使后续子 span 自动继承。

**Go 无法直接对齐**：因为 Go 的 callback 返回 `any`，不返回 `context.Context`，所以 `onLLMInvokeInput` 中对 ctx 的修改无法传回给后续回调。

**解决方案**：见上方"Span 父子链对齐"章节——创建子 span 时从 `OtelSpanState` 栈顶取 `CurrentLLMSpanContext()`，显式注入到 ctx。效果与 Python 的 `otel_context.attach` 等价。

### 6. monitor_handler.go — OtelTeamMonitorHandler（桩）

对齐 `monitor_handler.py` 的结构体和方法，但 `attach_to_team_agent` 为 no-op：

```go
type OtelTeamMonitorHandler struct {
    config         *ObservabilityConfig
    injectedTracer trace.Tracer
    teamSpans      map[string]trace.Span  // key=teamName
    taskSpans      map[string]trace.Span  // key=taskID
}

func (h *OtelTeamMonitorHandler) tracer() trace.Tracer

// 事件分发（对齐 Python __call__）
func (h *OtelTeamMonitorHandler) HandleEvent(event schema.EventMessage)

// Team span 生命周期
func (h *OtelTeamMonitorHandler) openTeamSpan(teamName string, payload map[string]any)
func (h *OtelTeamMonitorHandler) closeTeamSpan(teamName string)
func (h *OtelTeamMonitorHandler) recordTeamEvent(teamName string, name string, attrs map[string]any)

// Task span 生命周期
func (h *OtelTeamMonitorHandler) openTaskSpan(teamName string, payload map[string]any)
func (h *OtelTeamMonitorHandler) closeTaskSpan(payload map[string]any, etype string)
func (h *OtelTeamMonitorHandler) recordTaskEvent(payload map[string]any, etype string)

// Member / Message 事件
func (h *OtelTeamMonitorHandler) recordMemberEvent(teamName string, payload map[string]any, etype string)
func (h *OtelTeamMonitorHandler) recordMessageEvent(teamName string, payload map[string]any, etype string)
```

### 7. rail.go — ObservabilityRail

对齐 `rail.py`，嵌入 `DeepAgentRail`：

```go
type ObservabilityRail struct {
    rails.DeepAgentRail
    injectedTracer trace.Tracer
}

func (r *ObservabilityRail) tracer() trace.Tracer

func (r *ObservabilityRail) BeforeTaskIteration(ctx context.Context, railCtx *agentinterfaces.AgentCallbackContext) error
func (r *ObservabilityRail) AfterTaskIteration(ctx context.Context, railCtx *agentinterfaces.AgentCallbackContext) error
```

Rail 的 `GetCallbacks()` 自动通过 `DeepAgentRail` 机制注册 `BeforeTaskIteration` / `AfterTaskIteration`。

### 8. setup.go — 生命周期管理

对齐 `setup.py`：

```go
var (
    provider         *trace.TracerProvider
    callbackHandler  *OtelCallbackHandler
    monitorHandler   *OtelTeamMonitorHandler
)

func InitObservability(config *ObservabilityConfig, opts ...ObservabilityOption) error
func ShutdownObservability()
func GetTracer(name string) trace.Tracer
func AttachToTeamAgent(teamAgent TeamAgentInterface)  // 暂为桩
func DetachFromTeamAgent(teamAgent TeamAgentInterface) // 暂为桩

// 内部
func buildExporter(config *ObservabilityConfig) sdktrace.SpanExporter
func wireCallbackHandlers(handler *OtelCallbackHandler, fw *callback.CallbackFramework, namespace string)
```

**`InitObservability` 流程**：

1. 检查 `config.Enabled`，false 则直接返回
2. 检查 `provider != nil`，已初始化则 warn 并返回
3. 创建 `Resource`、`TracerProvider`、`SpanProcessor`
4. 创建 `OtelCallbackHandler` 和 `OtelTeamMonitorHandler`
5. 调用 `wireCallbackHandlers` 注册 10 个回调

**`wireCallbackHandlers` 注册方式**：

```go
func wireCallbackHandlers(handler *OtelCallbackHandler, fw *callback.CallbackFramework, namespace string) {
    ns := callback.WithNamespace(namespace)
    // LLM
    fw.OnLLM(callback.LLMInvokeInput, handler.onLLMInvokeInput, ns)
    fw.OnLLM(callback.LLMStreamInput, handler.onLLMStreamInput, ns)
    fw.OnLLM(callback.LLMStreamOutput, handler.onLLMStreamOutput, ns)
    fw.OnLLM(callback.LLMInvokeOutput, handler.onLLMInvokeOutput, ns)
    fw.OnLLM(callback.LLMCallError, handler.onLLMCallError, ns)
    // Tool
    fw.OnTool(callback.ToolCallStarted, handler.onToolCallStarted, ns)
    fw.OnTool(callback.ToolCallFinished, handler.onToolCallFinished, ns)
    fw.OnTool(callback.ToolCallError, handler.onToolCallError, ns)
    // Agent
    fw.OnGlobalAgent(callback.GlobalAgentInvokeInput, handler.onAgentInvokeInput, ns)
    fw.OnGlobalAgent(callback.GlobalAgentInvokeOutput, handler.onAgentInvokeOutput, ns)
}
```

**`ShutdownObservability` 流程**：

1. 通过 `UnregisterNamespace("agent_teams.observability")` 注销所有回调
2. 调用 `provider.Shutdown()` flush 残留 span
3. 重置全局变量为 nil

### 9. observability.go — 公共 API

对齐 `__init__.py`：

```go
// 公共类型和函数的重导出
type ObservabilityConfig = ...
type ObservabilityRail = ...
var InitObservability = ...
var ShutdownObservability = ...
var AttachToTeamAgent = ...
var DetachFromTeamAgent = ...
```

---

## 需要修改的已有代码

### 1. DeepAgent.ensureInitialized — 注入 SpanState

在 `internal/agentcore/harness/deep_agent.go` 的 `ensureInitialized` 方法中，CWD 初始化之后：

```go
// 初始化可观测性 SpanState（对齐 SessionState 注入模式）
if observability.SpanStateFromCtx(ctx) == nil {
    spanState := observability.InitSpanState()
    ctx = observability.WithSpanState(ctx, spanState)
}
```

### 2. DeepAgent.createSubagent — 子 Agent 隔离 SpanState

在 `createSubagent` 中创建独立的 SpanState（同 CwdState 模式）：

```go
subSpanState := observability.InitSpanState()
subCtx := observability.WithSpanState(ctx, subSpanState)
```

### 3. CallbackFramework.UnregisterNamespace — 扩展支持

当前 `UnregisterNamespace` 只遍历 `customCallbacks` 和 `perAgentCallbacks`，需要扩展为也遍历 `llmCallbacks`、`toolCallbacks`、`globalAgentCallbacks`：

```go
// 在现有逻辑之后追加：
for k, v := range fw.llmCallbacks {
    filtered := make([]*CallbackInfo[LLMCallbackFunc], 0, len(v))
    for _, info := range v {
        if info.Namespace != namespace {
            filtered = append(filtered, info)
        }
    }
    fw.llmCallbacks[k] = filtered
}
// toolCallbacks 和 globalAgentCallbacks 同理
```

---

## Python ↔ Go 映射表

| Python 文件 | Go 文件 | 关键差异 |
|------------|---------|---------|
| `config.py` | `config.go` | Pydantic BaseModel → Go struct |
| `semconv.py` | `semconv.go` | 直接对应，无差异 |
| `redaction.py` | `redaction.go` | hashlib.sha256 → crypto/sha256 |
| `span_context.py` | `span_state.go` | 3 个 ContextVar → 1 个 OtelSpanState + ctx 注入 |
| `callback_handler.py` | `callback_handler.go` | `*args/**kwargs` → 类型安全参数；`SpanStateFromCtx(ctx)` 替代 `pop_llm_span_state()` |
| `monitor_handler.py` | `monitor_handler.go` | `__call__` → `HandleEvent`；暂为桩 |
| `rail.py` | `rail.go` | `ctx.extra[_SPAN_KEY]` → `railCtx.Extra[spanKey]` |
| `setup.py` | `setup.go` | `register_sync` → `fw.OnLLM/OnTool/OnGlobalAgent`；`unregister_sync` → `UnregisterNamespace` |
| `__init__.py` | `observability.go` | Python module exports → Go type alias + var assignment |

---

## 测试策略

### 单元测试（go test ./...）

每个文件对应 `_test.go`，使用 mock tracer（`sdktrace.NewTracerProvider` + `go.opentelemetry.io/sdk/trace/tracetest`）：

- `config_test.go`：默认值验证、配置合法性
- `semconv_test.go`：常量值与 Python 完全一致
- `redaction_test.go`：脱敏/截断边界值
- `span_state_test.go`：Push/Pop LIFO 语义、并发安全、Tool/Agent map 操作
- `callback_handler_test.go`：使用 mock tracer 验证 span 属性（模型名、温度、TTFT 等）
- `monitor_handler_test.go`：HandleEvent 分发逻辑
- `rail_test.go`：BeforeTaskIteration/AfterTaskIteration span 开闭
- `setup_test.go`：InitObservability/ShutdownObservability 生命周期、回调注册/注销

### 集成测试（go:build integration）

- 真实 OTLP exporter 端到端测试
- 需要运行 OTel Collector
