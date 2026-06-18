# 5.11 Session Tracer 设计文档

## 概述

Session Tracer 是会话系统的可观测性层，负责记录 Agent/Workflow 执行过程中的每一步调用轨迹，包括 LLM 调用、Tool 调用、Workflow 组件调用的输入/输出/耗时/状态，并通过 StreamWriter 实时推送给客户端。

对应 Python 代码：`openjiuwen/core/session/tracer/`

## 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 包组织 | 单包 `session/tracer/`，文件按职责拆分 | 与 Python 目录一一对应，便于对照维护 |
| 异步模型 | 同步执行，handler 方法带 `context.Context` | Python 的 `await trigger()` 本质是同步等待执行完，Go 不需要 async |
| Handler 方法映射 | 逐一映射（21 个 Agent 方法 + 8 个 Workflow 方法） | 与 Python 一一对应，便于后续对照维护 |
| 装饰器替代 | 包装器结构体（实现相同接口 + 内部委托 inner） | Go 无 `__getattr__`，用接口包装替代 Python 代理 |
| JSON 序列化 | json tag camelCase（`json:"traceId"`） | 与 Python `model_dump(by_alias=True)` 输出格式完全对齐 |
| Trigger 分发 | map 查表 + `TriggerAgent`/`TriggerWorkflow` 两个方法 | O(1) 查找，调用方不需要传 handlerName，语义清晰 |
| 实现范围 | 核心 + 装饰器一起实现 | 保持与 Python 完整对齐 |

## 包结构

```
session/tracer/
├── doc.go              # 包文档
├── data.go             # InvokeType/NodeStatus/HandlerName/TraceEvent 枚举
├── span.go             # Span/TraceAgentSpan/TraceWorkflowSpan/SpanManager
├── tracer.go           # Tracer 核心（Init/TriggerAgent/TriggerWorkflow/Register/GetWorkflowSpan/PopWorkflowSpan）
├── handler.go          # TraceBaseHandler/TraceAgentHandler/TraceWorkflowHandler
├── decorator.go        # TracedModelClient/TracedTool/TracedWorkflow + DecorateXxxWithTrace 函数
└── workflow.go         # TracerWorkflowUtils（静态工具方法集）
```

## 数据模型（data.go）

### InvokeType 调用类型枚举

```go
type InvokeType string

const (
    InvokeTypePrompt    InvokeType = "prompt"
    InvokeTypeLLM       InvokeType = "llm"
    InvokeTypePlugin    InvokeType = "plugin"
    InvokeTypeWorkflow  InvokeType = "workflow"
    InvokeTypeChain     InvokeType = "chain"
    InvokeTypeRetriever InvokeType = "retriever"
    InvokeTypeEvaluator InvokeType = "evaluator"
)
```

### NodeStatus 节点状态枚举

```go
type NodeStatus string

const (
    NodeStatusStart       NodeStatus = "start"
    NodeStatusFinish      NodeStatus = "finish"
    NodeStatusRunning     NodeStatus = "running"
    NodeStatusInterrupted NodeStatus = "interrupted"
    NodeStatusError       NodeStatus = "error"
)
```

### TraceEvent 追踪事件枚举

替代 Python 的字符串反射分发，实现类型安全的 map 查表分发。

```go
type TraceEvent string

const (
    // ─── Agent 事件（由装饰器触发） ───
    TraceChainStart      TraceEvent = "on_chain_start"
    TraceChainEnd        TraceEvent = "on_chain_end"
    TraceChainError      TraceEvent = "on_chain_error"
    TraceLLMStart        TraceEvent = "on_llm_start"
    TraceLLMRequest      TraceEvent = "on_llm_request"
    TraceLLMEnd          TraceEvent = "on_llm_end"
    TraceLLMError        TraceEvent = "on_llm_error"
    TracePromptStart     TraceEvent = "on_prompt_start"
    TracePromptEnd       TraceEvent = "on_prompt_end"
    TracePromptError     TraceEvent = "on_prompt_error"
    TracePluginStart     TraceEvent = "on_plugin_start"
    TracePluginEnd       TraceEvent = "on_plugin_end"
    TracePluginError     TraceEvent = "on_plugin_error"
    TraceRetrieverStart  TraceEvent = "on_retriever_start"
    TraceRetrieverEnd    TraceEvent = "on_retriever_end"
    TraceRetrieverError  TraceEvent = "on_retriever_error"
    TraceEvaluatorStart  TraceEvent = "on_evaluator_start"
    TraceEvaluatorEnd    TraceEvent = "on_evaluator_end"
    TraceEvaluatorError  TraceEvent = "on_evaluator_error"
    TraceWorkflowStart   TraceEvent = "on_workflow_start"
    TraceWorkflowEnd     TraceEvent = "on_workflow_end"
    TraceWorkflowError   TraceEvent = "on_workflow_error"

    // ─── Workflow 事件（由 TracerWorkflowUtils 触发） ───
    TraceWFCallStart  TraceEvent = "on_call_start"
    TraceWFPreInvoke  TraceEvent = "on_pre_invoke"
    TraceWFPreStream  TraceEvent = "on_pre_stream"
    TraceWFInvoke     TraceEvent = "on_invoke"
    TraceWFPostStream TraceEvent = "on_post_stream"
    TraceWFPostInvoke TraceEvent = "on_post_invoke"
    TraceWFCallDone   TraceEvent = "on_call_done"
    TraceWFInteract   TraceEvent = "on_interact"
)
```

## Span 体系（span.go）

对应 Python: `openjiuwen/core/session/tracer/span.py`

### Span 基础结构

```go
// Span 追踪基础结构，记录一次调用的完整生命周期
type Span struct {
    TraceID        string              `json:"traceId"`
    StartTime      *time.Time          `json:"startTime,omitempty"`
    EndTime        *time.Time          `json:"endTime,omitempty"`
    Inputs         any                 `json:"inputs,omitempty"`
    Outputs        any                 `json:"outputs,omitempty"`
    Error          map[string]any      `json:"error,omitempty"`
    InvokeID       string              `json:"invokeId,omitempty"`
    ParentInvokeID string              `json:"parentInvokeId,omitempty"`
    ChildInvokesID []string            `json:"childInvokes,omitempty"`
    Status         string              `json:"status,omitempty"`
    OnInvokeData   []map[string]any    `json:"onInvokeData,omitempty"`
}
```

### TraceAgentSpan Agent 层追踪 Span

```go
type TraceAgentSpan struct {
    Span
    InvokeType  string         `json:"invokeType,omitempty"`
    Name        string         `json:"name,omitempty"`
    ElapsedTime string         `json:"elapsedTime,omitempty"`
    MetaData    map[string]any `json:"metaData,omitempty"`
}
```

### TraceWorkflowSpan Workflow 层追踪 Span

```go
type TraceWorkflowSpan struct {
    Span
    ExecutionID      string                    `json:"executionId,omitempty"`
    SourceIDs        []string                  `json:"sourceIds,omitempty"`
    WorkflowID       string                    `json:"workflowId,omitempty"`
    WorkflowVersion  string                    `json:"workflowVersion,omitempty"`
    WorkflowName     string                    `json:"workflowName,omitempty"`
    ComponentID      string                    `json:"componentId,omitempty"`
    ComponentName    string                    `json:"componentName,omitempty"`
    ComponentType    string                    `json:"componentType,omitempty"`
    LoopNodeID       string                    `json:"loopNodeId,omitempty"`
    LoopIndex        *int                      `json:"loopIndex,omitempty"`
    LLMInvokeData    map[string]map[string]any `json:"-"` // exclude，与 Python exclude=True 对齐
    ParentNodeID     string                    `json:"parentNodeId,omitempty"`
    StreamInputs     []any                     `json:"streamInputs,omitempty"`
    StreamOutputs    []any                     `json:"streamOutputs,omitempty"`
    InteractiveInputs any                      `json:"interactiveInputs,omitempty"`
    InnerError       map[string]any            `json:"innerError,omitempty"`
}
```

### SpanManager

```go
// SpanManager 管理一次追踪中的所有 Span
type SpanManager struct {
    traceID      string
    parentID     string
    order        []string              // 调用顺序
    sessionSpans map[string]*Span      // invokeID → Span（统一用 *Span，具体类型为 TraceAgentSpan/TraceWorkflowSpan）
}

func NewSpanManager(traceID string, parentID ...string) *SpanManager
func (m *SpanManager) GetSpan(invokeID string) *Span
func (m *SpanManager) PopSpan(invokeID string)
func (m *SpanManager) CreateAgentSpan(parentSpan ...*TraceAgentSpan) *TraceAgentSpan
func (m *SpanManager) CreateWorkflowSpan(invokeID string, parentSpan ...*TraceWorkflowSpan) *TraceWorkflowSpan
func (m *SpanManager) UpdateSpan(span *Span, data map[string]any)
func (m *SpanManager) LastSpan() *Span
```

**与 Python 的差异**：Python 用 `Dict[str, Span]` 存储，Go 用 `map[string]*Span`。Python 的 `refresh_span_record` 在 Go 中合并到 `UpdateSpan`。

## Tracer 核心（tracer.go）

对应 Python: `openjiuwen/core/session/tracer/tracer.py`

### 数据结构

```go
type Tracer struct {
    traceID                    string
    AgentSpanManager           *SpanManager
    WorkflowSpanManagerDict    map[string]*SpanManager  // parentID → SpanManager
    streamWriterManager        *stream.StreamWriterManager

    // 分发表：event → agentHandler 方法
    agentDispatch   map[TraceEvent]agentHandlerFunc
    // 分发表：event → workflowHandler 方法（按 parentID 分组）
    workflowDispatch map[string]map[TraceEvent]workflowHandlerFunc  // parentID → event → handler
}
```

### 方法

```go
func NewTracer() *Tracer

// Init 初始化 Tracer，绑定 StreamWriterManager，创建默认 Handler，构建分发表
func (t *Tracer) Init(swm *stream.StreamWriterManager)

// RegisterWorkflowSpanManager 注册 Workflow 级别的 SpanManager 和 Handler
func (t *Tracer) RegisterWorkflowSpanManager(parentNodeID string)

// TriggerAgent 触发 Agent 层追踪事件，通过 map 查表分发到 TraceAgentHandler
func (t *Tracer) TriggerAgent(ctx context.Context, event TraceEvent, params *TriggerParams)

// TriggerWorkflow 触发 Workflow 层追踪事件，通过 map 查表分发到 TraceWorkflowHandler
// parentNodeID 用于区分子工作流层级
func (t *Tracer) TriggerWorkflow(ctx context.Context, event TraceEvent, parentNodeID string, params *TriggerParams)

// GetWorkflowSpan 获取指定 Workflow 的 Span
func (t *Tracer) GetWorkflowSpan(invokeID, parentNodeID string) *TraceWorkflowSpan

// PopWorkflowSpan 移除指定 Workflow 的 Span
func (t *Tracer) PopWorkflowSpan(invokeID, parentNodeID string)
```

### TriggerParams

```go
// TriggerParams Trigger 调用的可选参数集合
type TriggerParams struct {
    Span              *Span
    Inputs            any
    Outputs           any
    Error             error
    InstanceInfo      map[string]any
    InvokeID          string
    Metadata          map[string]any
    SourceIDs         []string
    NeedSend          bool
    OnInvokeData      map[string]any
    Chunk             any
    ComponentMetadata map[string]any
}
```

### 分发表构建

```go
// Init 中构建分发表
agentHandler := NewTraceAgentHandler(swm, t.AgentSpanManager)

t.agentDispatch = map[TraceEvent]agentHandlerFunc{
    TraceChainStart:     agentHandler.OnChainStart,
    TraceChainEnd:       agentHandler.OnChainEnd,
    TraceChainError:     agentHandler.OnChainError,
    TraceLLMStart:       agentHandler.OnLLMStart,
    TraceLLMRequest:     agentHandler.OnLLMRequest,
    TraceLLMEnd:         agentHandler.OnLLMEnd,
    TraceLLMError:       agentHandler.OnLLMError,
    // ... 其余 Agent 事件
}

// Workflow 分发表按 parentID 分组，RegisterWorkflowSpanManager 时构建
wfHandler := NewTraceWorkflowHandler(swm, spanManager)
t.workflowDispatch[parentNodeID] = map[TraceEvent]workflowHandlerFunc{
    TraceWFCallStart:  wfHandler.OnCallStart,
    TraceWFPreInvoke:  wfHandler.OnPreInvoke,
    TraceWFPreStream:  wfHandler.OnPreStream,
    // ... 其余 Workflow 事件
}
```

## Handler 体系（handler.go）

对应 Python: `openjiuwen/core/session/tracer/handler.py`

### TraceBaseHandler 抽象基类

```go
type TraceBaseHandler interface {
    FormatData(span *Span) map[string]any
}

type traceBaseHandler struct {
    streamWriter stream.StreamWriter
    spanManager  *SpanManager
}

func (h *traceBaseHandler) EmitStreamWriter(ctx context.Context, span *Span) error
func (h *traceBaseHandler) GetElapsedTime(start, end time.Time) string
func (h *traceBaseHandler) GetNodeStatus(span *Span) NodeStatus
```

### TraceAgentHandler（21 个方法，逐一映射）

```go
type TraceAgentHandler struct {
    traceBaseHandler
}

// 更新辅助方法
func (h *TraceAgentHandler) updateStartTraceData(span *TraceAgentSpan, invokeType string, inputs any, instanceInfo map[string]any)
func (h *TraceAgentHandler) updateEndTraceData(span *TraceAgentSpan, outputs any)
func (h *TraceAgentHandler) updateErrorTraceData(span *TraceAgentSpan, err error)
func (h *TraceAgentHandler) updateRunningTraceData(span *TraceAgentSpan, data map[string]any)

// Chain 事件
func (h *TraceAgentHandler) OnChainStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error
func (h *TraceAgentHandler) OnChainEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error
func (h *TraceAgentHandler) OnChainError(ctx context.Context, span *TraceAgentSpan, err error) error

// LLM 事件
func (h *TraceAgentHandler) OnLLMStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error
func (h *TraceAgentHandler) OnLLMRequest(ctx context.Context, span *TraceAgentSpan, data map[string]any) error
func (h *TraceAgentHandler) OnLLMEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error
func (h *TraceAgentHandler) OnLLMError(ctx context.Context, span *TraceAgentSpan, err error) error

// Prompt 事件
func (h *TraceAgentHandler) OnPromptStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error
func (h *TraceAgentHandler) OnPromptEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error
func (h *TraceAgentHandler) OnPromptError(ctx context.Context, span *TraceAgentSpan, err error) error

// Plugin 事件
func (h *TraceAgentHandler) OnPluginStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error
func (h *TraceAgentHandler) OnPluginEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error
func (h *TraceAgentHandler) OnPluginError(ctx context.Context, span *TraceAgentSpan, err error) error

// Retriever 事件
func (h *TraceAgentHandler) OnRetrieverStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error
func (h *TraceAgentHandler) OnRetrieverEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error
func (h *TraceAgentHandler) OnRetrieverError(ctx context.Context, span *TraceAgentSpan, err error) error

// Evaluator 事件
func (h *TraceAgentHandler) OnEvaluatorStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error
func (h *TraceAgentHandler) OnEvaluatorEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error
func (h *TraceAgentHandler) OnEvaluatorError(ctx context.Context, span *TraceAgentSpan, err error) error

// Workflow 事件（Agent 层视角的 Workflow 调用）
func (h *TraceAgentHandler) OnWorkflowStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error
func (h *TraceAgentHandler) OnWorkflowEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error
func (h *TraceAgentHandler) OnWorkflowError(ctx context.Context, span *TraceAgentSpan, err error) error
```

### TraceWorkflowHandler（8 个方法，逐一映射）

```go
type TraceWorkflowHandler struct {
    traceBaseHandler
}

func (h *TraceWorkflowHandler) OnCallStart(ctx context.Context, invokeID string, metadata map[string]any, inputs any, needSend bool, sourceIDs []string) error
func (h *TraceWorkflowHandler) OnPreInvoke(ctx context.Context, invokeID string, inputs any, componentMetadata map[string]any, needSend bool) error
func (h *TraceWorkflowHandler) OnPreStream(ctx context.Context, invokeID string, chunk any, needSend bool) error
func (h *TraceWorkflowHandler) OnInvoke(ctx context.Context, invokeID string, onInvokeData map[string]any, exception error) error
func (h *TraceWorkflowHandler) OnPostStream(ctx context.Context, invokeID string, chunk any) error
func (h *TraceWorkflowHandler) OnPostInvoke(ctx context.Context, invokeID string, outputs any, inputs any) error
func (h *TraceWorkflowHandler) OnCallDone(ctx context.Context, invokeID string, outputs any) error
func (h *TraceWorkflowHandler) OnInteract(ctx context.Context, invokeID string, inputs any, componentMetadata map[string]any, needSend bool) error
```

### FormatData 输出格式

与 Python 的 `_format_data` 对齐，输出 `{type, payload}` 结构，通过 `TraceSchema` 写入 StreamWriter：

```go
// TraceAgentHandler.FormatData
func (h *TraceAgentHandler) FormatData(span *Span) map[string]any {
    agentSpan := span.(*TraceAgentSpan)
    if agentSpan.Status != string(NodeStatusInterrupted) {
        agentSpan.Status = string(h.GetNodeStatus(span))
    }
    return map[string]any{
        "type":    "tracer_agent",
        "payload": agentSpan,  // json.Marshal 时自动用 camelCase tag
    }
}

// TraceWorkflowHandler.FormatData
func (h *TraceWorkflowHandler) FormatData(span *Span) map[string]any {
    wfSpan := span.(*TraceWorkflowSpan)
    if wfSpan.Status != string(NodeStatusInterrupted) {
        wfSpan.Status = string(h.GetNodeStatus(span))
    }
    return map[string]any{
        "type":    "tracer_workflow",
        "payload": wfSpan,
    }
}
```

## 装饰器（decorator.go）

对应 Python: `openjiuwen/core/session/tracer/decorator.py`

### TracedModelClient

```go
// TracedModelClient 追踪包装器，实现 BaseModelClient 接口
type TracedModelClient struct {
    inner       model_clients.BaseModelClient
    tracer      *Tracer
    agentSpan   *TraceAgentSpan
    instanceInfo map[string]any
}

func (t *TracedModelClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
    span := t.tracer.AgentSpanManager.CreateAgentSpan(t.agentSpan)
    t.tracer.TriggerAgent(ctx, TraceLLMStart, &TriggerParams{Span: &span.Span, Inputs: messages, InstanceInfo: t.instanceInfo})
    result, err := t.inner.Invoke(ctx, messages, opts...)
    if err != nil {
        t.tracer.TriggerAgent(ctx, TraceLLMError, &TriggerParams{Span: &span.Span, Error: err})
        return nil, err
    }
    t.tracer.TriggerAgent(ctx, TraceLLMEnd, &TriggerParams{Span: &span.Span, Outputs: result})
    return result, nil
}

// Stream 类似 Invoke，但收集流式结果后记录 OnLLMEnd
func (t *TracedModelClient) Stream(...) (*model_clients.StreamResult, error)

// 其他方法直接委托
func (t *TracedModelClient) GenerateImage(ctx context.Context, ...) (*llmschema.ImageGenerationResponse, error) {
    return t.inner.GenerateImage(ctx, ...)
}
// GenerateSpeech / GenerateVideo / Release 同理
```

### TracedTool

```go
type TracedTool struct {
    inner       tool.Tool
    tracer      *Tracer
    agentSpan   *TraceAgentSpan
    instanceInfo map[string]any
}

func (t *TracedTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
    span := t.tracer.AgentSpanManager.CreateAgentSpan(t.agentSpan)
    t.tracer.TriggerAgent(ctx, TracePluginStart, &TriggerParams{Span: &span.Span, Inputs: inputs, InstanceInfo: t.instanceInfo})
    result, err := t.inner.Invoke(ctx, inputs, opts...)
    if err != nil {
        t.tracer.TriggerAgent(ctx, TracePluginError, &TriggerParams{Span: &span.Span, Error: err})
        return nil, err
    }
    t.tracer.TriggerAgent(ctx, TracePluginEnd, &TriggerParams{Span: &span.Span, Outputs: result})
    return result, nil
}

func (t *TracedTool) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
    return t.inner.Stream(ctx, inputs, opts...)
}

func (t *TracedTool) Card() *tool.ToolCard {
    return t.inner.Card()
}
```

### TracedWorkflow

```go
type TracedWorkflow struct {
    inner       WorkflowInterface  // Workflow 接口（待领域6定义）
    tracer      *Tracer
    agentSpan   *TraceAgentSpan
    instanceInfo map[string]any
}

func (t *TracedWorkflow) Invoke(...)  { /* 类似 TracedModelClient */ }
func (t *TracedWorkflow) Stream(...)  { /* 类似 TracedModelClient */ }
```

### 装饰函数

```go
// DecorateModelWithTrace 如果 session 有 tracer 和 span，返回追踪包装器；否则返回原始 model
func DecorateModelWithTrace(model model_clients.BaseModelClient, session *internal.AgentSession) model_clients.BaseModelClient

// DecorateToolWithTrace 如果 session 有 tracer 和 span，返回追踪包装器；否则返回原始 tool
func DecorateToolWithTrace(t tool.Tool, session *internal.AgentSession) tool.Tool

// DecorateWorkflowWithTrace 如果 session 有 tracer 和 span，返回追踪包装器；否则返回原始 workflow
func DecorateWorkflowWithTrace(w WorkflowInterface, session *internal.AgentSession) WorkflowInterface
```

**注意**：`DecorateWorkflowWithTrace` 的 `WorkflowInterface` 参数依赖领域6的 Workflow 接口定义，5.11 先定义最小接口占位（`Invoke` + `Stream`），领域6实现后再替换。

## TracerWorkflowUtils（workflow.go）

对应 Python: `openjiuwen/core/session/tracer/workflow_tracer.py`

```go
type TracerWorkflowUtils struct{}

// TraceWorkflowStart 记录工作流开始
func (TracerWorkflowUtils) TraceWorkflowStart(ctx context.Context, session BaseWorkflowSession, inputs map[string]any) error

// TraceComponentBegin 记录组件开始
func (TracerWorkflowUtils) TraceComponentBegin(ctx context.Context, session BaseWorkflowSession, sourceIDs []string) error

// TraceComponentInputs 记录组件输入
func (TracerWorkflowUtils) TraceComponentInputs(ctx context.Context, session BaseWorkflowSession, inputs map[string]any, send bool) error

// TraceComponentStreamInput 记录组件流式输入
func (TracerWorkflowUtils) TraceComponentStreamInput(ctx context.Context, session BaseWorkflowSession, chunk any, send bool) error

// TraceComponentOutputs 记录组件输出
func (TracerWorkflowUtils) TraceComponentOutputs(ctx context.Context, session BaseWorkflowSession, outputs map[string]any) error

// TraceComponentStreamOutput 记录组件流式输出
func (TracerWorkflowUtils) TraceComponentStreamOutput(ctx context.Context, session BaseWorkflowSession, chunk any) error

// TraceWorkflowDone 记录工作流完成
func (TracerWorkflowUtils) TraceWorkflowDone(ctx context.Context, session BaseWorkflowSession, outputs map[string]any) error

// TraceComponentDone 记录组件完成
func (TracerWorkflowUtils) TraceComponentDone(ctx context.Context, session BaseWorkflowSession) error

// Trace 记录组件追踪数据（用户主动调用，对应 NodeSessionFacade.Trace）
func (TracerWorkflowUtils) Trace(ctx context.Context, session BaseWorkflowSession, data map[string]any) error

// TraceError 记录组件错误追踪（对应 NodeSessionFacade.TraceError）
func (TracerWorkflowUtils) TraceError(ctx context.Context, session BaseWorkflowSession, err error) error

// TraceComponentInteractiveInputs 记录组件交互输入
func (TracerWorkflowUtils) TraceComponentInteractiveInputs(ctx context.Context, session BaseWorkflowSession, inputs map[string]any, send bool) error
```

**BaseWorkflowSession 接口**：TracerWorkflowUtils 需要从 session 获取 `Tracer()`/`ExecutableID()`/`ParentID()`/`WorkflowID()`/`NodeID()`/`NodeType()`/`State()`/`Config()` 等信息。定义最小接口避免依赖具体实现：

```go
// BaseWorkflowSession TracerWorkflowUtils 需要的会话最小接口
type BaseWorkflowSession interface {
    Tracer() *Tracer
    ExecutableID() string
    ParentID() string
    WorkflowID() string
    NodeID() string
    NodeType() string
    State() state.SessionState
    Config() SessionConfig  // ⤵️ 5.12 回填具体类型
}
```

## 回填已有代码

### interfaces/interfaces.go

```go
// 修改前
Tracer() any

// 修改后
Tracer() *tracer.Tracer
```

### internal/agent_session.go

```go
// 修改前
tracer any

// 修改后
tracer *tracer.Tracer

// 取消注释并修改
if s.tracer == nil {
    s.tracer = tracer.NewTracer()
    s.tracer.Init(s.streamWriterManager)
}
if s.agentSpan == nil && s.tracer != nil {
    s.agentSpan = s.tracer.AgentSpanManager.CreateAgentSpan()
}
```

### internal/workflow_session.go

```go
// 修改前
tracer any

// 修改后
tracer *tracer.Tracer
```

### node.go

```go
// Trace 修改前（桩实现）
func (f *NodeSessionFacade) Trace(ctx context.Context, data map[string]any) error {
    if f.inner.SkipTrace() { return nil }
    // ⤵️ 5.11 回填
    return nil
}

// Trace 修改后
func (f *NodeSessionFacade) Trace(ctx context.Context, data map[string]any) error {
    if f.inner.SkipTrace() { return nil }
    return TracerWorkflowUtils{}.Trace(ctx, f.inner, data)
}

// TraceError 修改前（桩实现）
func (f *NodeSessionFacade) TraceError(ctx context.Context, err error) error {
    if f.inner.SkipTrace() { return nil }
    // ⤵️ 5.11 回填
    return nil
}

// TraceError 修改后
func (f *NodeSessionFacade) TraceError(ctx context.Context, err error) error {
    if f.inner.SkipTrace() { return nil }
    return TracerWorkflowUtils{}.TraceError(ctx, f.inner, err)
}
```

### wrapper.go

```go
// Trace 修改前
func (r *RouterSessionFacade) Trace(ctx context.Context, data map[string]any) error {
    return r.inner.Trace(ctx, data)
}

// Trace 修改后（无需改动，inner 已经是 NodeSessionFacade，会走真实逻辑）

// TraceError 同理
```

### doc.go

更新 session/ 包文档，增加 `tracer/` 子包描述。

## 调用链路

### Agent 场景

```
AgentSession 创建
  → Tracer 初始化（绑定 StreamWriterManager）
  → AgentSpanManager 创建
  → agentSpan = AgentSpanManager.CreateAgentSpan()

Agent 执行
  → model = DecorateModelWithTrace(model, session)  // 原始 model → TracedModelClient
  → tool  = DecorateToolWithTrace(tool, session)    // 原始 tool  → TracedTool

  → TracedModelClient.Invoke(messages)
    ├── TriggerAgent(TraceLLMStart)   → TraceAgentHandler.OnLLMStart  → 写 TraceWriter
    ├── inner.Invoke(messages)        → 真正调用 LLM
    ├── TriggerAgent(TraceLLMEnd)     → TraceAgentHandler.OnLLMEnd    → 写 TraceWriter
    └── 返回结果

  → TracedTool.Invoke(inputs)
    ├── TriggerAgent(TracePluginStart) → TraceAgentHandler.OnPluginStart → 写 TraceWriter
    ├── inner.Invoke(inputs)           → 真正调用 Tool
    ├── TriggerAgent(TracePluginEnd)   → TraceAgentHandler.OnPluginEnd   → 写 TraceWriter
    └── 返回结果
```

### Workflow 场景

```
WorkflowSession 开始
  → TracerWorkflowUtils.TraceWorkflowStart()
    → TriggerWorkflow(TraceWFCallStart, parentNodeID="")

  组件1 执行
    → TracerWorkflowUtils.TraceComponentBegin()
      → TriggerWorkflow(TraceWFCallStart, parentNodeID=parentID)
    → TracerWorkflowUtils.TraceComponentInputs()
      → TriggerWorkflow(TraceWFPreInvoke, parentNodeID=parentID)
    → TracerWorkflowUtils.TraceComponentOutputs()
      → TriggerWorkflow(TraceWFPostInvoke, parentNodeID=parentID)
    → TracerWorkflowUtils.TraceComponentDone()
      → TriggerWorkflow(TraceWFCallDone, parentNodeID=parentID)

  用户主动追踪
    → NodeSessionFacade.Trace(data)
      → TracerWorkflowUtils.Trace()
        → TriggerWorkflow(TraceWFInvoke, parentNodeID=parentID)

  错误追踪
    → NodeSessionFacade.TraceError(err)
      → TracerWorkflowUtils.TraceError()
        → TriggerWorkflow(TraceWFInvoke, parentNodeID=parentID)

  Workflow 完成
    → TracerWorkflowUtils.TraceWorkflowDone()
      → TriggerWorkflow(TraceWFCallDone, parentNodeID="")
```

## 日志规范

按项目规则 3，逐条对照 Python tracer 中的 logger 调用，在等价位置补充日志。

### Python 显式日志（1 处）

| Python 位置 | Python 日志 | Go 等价位置 | Go 日志 |
|-------------|------------|-------------|---------|
| `handler.py:104-108` | `session_logger.error("Failed to process metadata for trace", event_type=SYSTEM_ERROR, metadata={"error": str(err), "instance_info": str(instance_info)})` | `TraceAgentHandler.updateStartTraceData` 中 `json.Marshal` 失败时 | `logger.Error(ComponentAgentCore).Str("event_type", "SYSTEM_ERROR").Str("error", errStr).Any("instance_info", instanceInfo).Msg("元数据处理失败")` |

### 防御性日志（2 处）

按规则 3.4 异常路径日志规则，补充 Python 中未显式记录但应记录的防御性日志：

| Go 位置 | 级别 | 条件 | 日志内容 |
|---------|------|------|---------|
| `Tracer.TriggerAgent` / `TriggerWorkflow` | Warn | 分发表中找不到 event 对应的 handler | `logger.Warn(ComponentAgentCore).Str("event_type", string(event)).Msg("追踪事件未找到处理器")` |
| `traceBaseHandler.EmitStreamWriter` | Error | StreamWriter.Write 返回错误 | `logger.Error(ComponentAgentCore).Err(writeErr).Msg("追踪数据写入流失败")` |

### 不记录日志的场景

| 场景 | 原因 |
|------|------|
| `TracerWorkflowUtils` 中 `session.Tracer() == nil` | Python 也是静默跳过（`if tracer is None: return`），属于正常场景——session 可选择不启用 tracer |
| `TracedModelClient`/`TracedTool`/`TracedWorkflow` 中 inner 调用失败且已 trigger error 时 | error 已通过 OnLLMError/OnPluginError 写入 span，避免重复记录 |
| `traceBaseHandler` 中 `streamWriter == nil` | Python 也是静默跳过（`if self._stream_writer is None: return`），属于正常场景 |

### 组件常量

```go
const logComponent = logger.ComponentAgentCore  // tracer 属于 agentcore 子系统
```

## 测试策略

覆盖率目标：≥ 85%。可 mock 的禁止用 build tag。测试函数命名 `TestXxx_场景描述`。

### 测试文件

| 测试文件 | 覆盖源文件 | 测试内容 |
|---------|-----------|---------|
| `data_test.go` | `data.go` | 枚举值与 Python 对齐 |
| `span_test.go` | `span.go` | Span 创建/更新/父子关系/序列化/SpanManager |
| `tracer_test.go` | `tracer.go` | Init/TriggerAgent/TriggerWorkflow/Register/GetWorkflowSpan/PopWorkflowSpan 分发 |
| `handler_test.go` | `handler.go` | 每个 Handler 方法的 update + send + FormatData |
| `decorator_test.go` | `decorator.go` | 包装器委托 + 追踪逻辑 + DecorateXxx 函数 |
| `workflow_test.go` | `workflow.go` | TracerWorkflowUtils 各方法 |
| 回填集成测试 | 已有 `*_test.go` | AgentSession 创建时 tracer 自动初始化、NodeSessionFacade.Trace/TraceError 走真实逻辑 |

### 各文件详细测试用例

**data_test.go**：
- `TestInvokeType_值对齐`：验证 7 种 InvokeType 值与 Python 一致
- `TestNodeStatus_值对齐`：验证 5 种 NodeStatus 值与 Python 一致
- `TestTraceEvent_Agent事件完整性`：验证 21 种 Agent TraceEvent 值
- `TestTraceEvent_Workflow事件完整性`：验证 8 种 Workflow TraceEvent 值

**span_test.go**：
- `TestSpan_Update`：更新各字段
- `TestSpan_AppendChildInvokeID`：追加子调用 ID
- `TestTraceAgentSpan_JSON序列化`：验证 camelCase tag（traceId/invokeType/startTime 等）
- `TestTraceWorkflowSpan_JSON序列化`：验证 camelCase tag + LLMInvokeData 被 exclude（`json:"-"`）
- `TestTraceWorkflowSpan_AppendStreamOutput`：追加流式输出
- `TestTraceWorkflowSpan_AppendStreamInputs`：追加流式输入
- `TestSpanManager_CreateAgentSpan`：创建 + invokeID 生成 + 注册到 sessionSpans
- `TestSpanManager_CreateAgentSpan_有父Span`：parentInvokeID + childInvokesID 正确建立
- `TestSpanManager_CreateWorkflowSpan`：创建 + parentNodeID 继承
- `TestSpanManager_GetSpan`：存在/不存在
- `TestSpanManager_PopSpan`：移除后 GetSpan 返回 nil
- `TestSpanManager_UpdateSpan`：更新字段后 sessionSpans 同步
- `TestSpanManager_LastSpan`：返回最近创建的 Span
- `TestSpanManager_LastSpan_空时返回nil`：order 为空时返回 nil

**tracer_test.go**：
- `TestNewTracer`：traceID 自动生成
- `TestTracer_Init`：AgentSpanManager/WorkflowSpanManagerDict/分发表初始化
- `TestTracer_Init_SpanManagerDict默认条目`：`""` key 的默认 SpanManager 已注册
- `TestTracer_TriggerAgent_LLMStart`：验证分发到 OnLLMStart，span 字段更新
- `TestTracer_TriggerAgent_LLMEnd`：验证分发到 OnLLMEnd
- `TestTracer_TriggerAgent_LLMError`：验证分发到 OnLLMError
- `TestTracer_TriggerAgent_未注册事件`：Warn 日志 + 不 panic
- `TestTracer_TriggerWorkflow_CallStart`：验证分发到 OnCallStart
- `TestTracer_TriggerWorkflow_带ParentNodeID`：使用对应 parentID 的 handler
- `TestTracer_RegisterWorkflowSpanManager`：新 SpanManager 注册 + 分发表扩展
- `TestTracer_GetWorkflowSpan`：存在/不存在
- `TestTracer_PopWorkflowSpan`：移除后 GetWorkflowSpan 返回 nil

**handler_test.go**：
- `TestTraceAgentHandler_OnLLMStart`：span 字段（startTime/invokeType/inputs/name/metaData）+ streamWriter 写入
- `TestTraceAgentHandler_OnLLMRequest`：OnInvokeData 追加 + streamWriter 写入
- `TestTraceAgentHandler_OnLLMEnd`：endTime/outputs/elapsedTime + streamWriter 写入
- `TestTraceAgentHandler_OnLLMError`：endTime/error/elapsedTime + streamWriter 写入
- `TestTraceAgentHandler_OnPluginStart` / `OnPluginEnd` / `OnPluginError`：类似 LLM
- `TestTraceAgentHandler_OnChainStart` / `OnChainEnd` / `OnChainError`：类似 LLM
- `TestTraceAgentHandler_updateStartTraceData_Marshal失败`：json.Marshal 不可序列化对象时 Error 日志
- `TestTraceAgentHandler_GetElapsedTime_毫秒`：< 1s 返回 "Xms"
- `TestTraceAgentHandler_GetElapsedTime_秒`：≥ 1s 返回 "X.XXs"
- `TestTraceAgentHandler_GetNodeStatus_各状态`：error/running/finish/start
- `TestTraceAgentHandler_FormatData`：`{type: "tracer_agent", payload: ...}` 结构
- `TestTraceWorkflowHandler_OnCallStart`：span 字段 + needSend 控制写入
- `TestTraceWorkflowHandler_OnPreInvoke`：componentMetadata 合并
- `TestTraceWorkflowHandler_OnInvoke_正常`：OnInvokeData 追加
- `TestTraceWorkflowHandler_OnInvoke_异常_BaseError`：error 字段 + endTime
- `TestTraceWorkflowHandler_OnInvoke_异常_GraphInterrupt`：status = interrupted
- `TestTraceWorkflowHandler_OnInvoke_异常_InnerError`：innerError 字段
- `TestTraceWorkflowHandler_OnPostInvoke`：outputs 更新
- `TestTraceWorkflowHandler_OnPostStream`：StreamOutputs 追加
- `TestTraceWorkflowHandler_OnCallDone`：endTime/elapsedTime
- `TestTraceWorkflowHandler_OnInteract`：interactiveInputs 更新
- `TestTraceWorkflowHandler_FormatData`：`{type: "tracer_workflow", payload: ...}` + exclude childInvokesID/llmInvokeData

**decorator_test.go**：
- `TestTracedModelClient_Invoke_成功`：TriggerAgent 调用顺序(Start→End) + 委托 inner
- `TestTracedModelClient_Invoke_失败`：TriggerAgent(Start→Error) + 返回 error
- `TestTracedModelClient_Stream_成功`：流式结果收集 + TriggerAgent(Start→End)
- `TestTracedModelClient_GenerateImage_直接委托`：不触发追踪
- `TestTracedTool_Invoke_成功`：TriggerAgent(Start→End) + 委托 inner
- `TestTracedTool_Invoke_失败`：TriggerAgent(Start→Error)
- `TestTracedTool_Card_委托`：返回 inner.Card()
- `TestTracedTool_Stream_委托`：不触发追踪
- `TestDecorateModelWithTrace_有Tracer`：返回 TracedModelClient
- `TestDecorateModelWithTrace_无Tracer`：返回原始 model
- `TestDecorateToolWithTrace_有Tracer`：返回 TracedTool
- `TestDecorateToolWithTrace_无Tracer`：返回原始 tool
- `TestDecorateWorkflowWithTrace_有Tracer`：返回 TracedWorkflow
- `TestDecorateWorkflowWithTrace_无Tracer`：返回原始 workflow

**workflow_test.go**：
- `TestTracerWorkflowUtils_TraceWorkflowStart`：验证 TriggerWorkflow(TraceWFCallStart, parentNodeID="") 参数
- `TestTracerWorkflowUtils_TraceWorkflowDone`：验证 TriggerWorkflow(TraceWFCallDone, parentNodeID="") 参数
- `TestTracerWorkflowUtils_TraceComponentBegin`：验证 executableID/parentID/sourceIDs 传递
- `TestTracerWorkflowUtils_TraceComponentInputs`：验证 inputs/componentMetadata 传递
- `TestTracerWorkflowUtils_TraceComponentOutputs`：验证 outputs 传递
- `TestTracerWorkflowUtils_TraceComponentDone`：验证 TriggerWorkflow(TraceWFCallDone) + PopWorkflowSpan
- `TestTracerWorkflowUtils_Trace`：验证 TraceWFInvoke + onInvokeData 传递
- `TestTracerWorkflowUtils_TraceError`：验证 TraceWFInvoke + exception 传递
- `TestTracerWorkflowUtils_Tracer为nil_静默返回`：session.Tracer() 返回 nil 时不 panic
- `TestTracerWorkflowUtils_TraceComponentStreamInput`：验证 chunk 为 dict 时传递，为 string 时跳过
- `TestTracerWorkflowUtils_TraceComponentStreamOutput`：验证 chunk 传递
- `TestTracerWorkflowUtils_TraceComponentInteractiveInputs`：验证 inputs/componentMetadata 传递

**回填集成测试（更新已有 `*_test.go`）**：
- `TestAgentSession_Tracer自动初始化`：创建 AgentSession 时 tracer 自动 New + Init，AgentSpanManager 非 nil
- `TestAgentSession_AgentSpan自动创建`：tracer 非空时 agentSpan 自动 CreateAgentSpan
- `TestNodeSessionFacade_Trace_走真实逻辑`：SkipTrace=false 时调用 TracerWorkflowUtils.Trace，tracer 触发 TraceWFInvoke
- `TestNodeSessionFacade_TraceError_走真实逻辑`：SkipTrace=false 时调用 TracerWorkflowUtils.TraceError
- `TestNodeSessionFacade_Trace_SkipTrace跳过`：SkipTrace=true 时静默返回 nil
- 更新已有测试中 `Tracer()` 返回类型从 `any` 改为 `*tracer.Tracer`

### Mock 策略

| 依赖 | Mock 方式 | 说明 |
|------|----------|------|
| `stream.StreamWriterManager` | 创建真实实例 + mock Emitter | 收集写入数据验证 |
| `stream.StreamWriter` | 接口 mock（`mockStreamWriter`） | 记录 Write 调用参数 |
| `model_clients.BaseModelClient` | `fakeModelClient` 结构体实现接口 | 记录 Invoke/Stream 调用，返回预设结果 |
| `tool.Tool` | `fakeTool` 结构体实现接口 | 记录 Invoke 调用，返回预设结果 |
| `BaseWorkflowSession` | `fakeWorkflowSession` 结构体实现接口 | 返回预设 Tracer/ExecutableID/ParentID 等 |
| `AgentSession`（decorator 测试） | `fakeAgentSessionForDecorate` | 仅暴露 tracer/span 字段 |

### 端到端测试（延后领域6）

Python 的 `test_agent.py`（Agent + Workflow 共用 Tracer）和 `test_workflow_tracer.py`（Workflow 各组件追踪）依赖 Agent/Workflow 领域完整实现，5.11 暂不实现，延后到领域6。
