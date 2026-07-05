package callback

import (
	"context"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EventBase 事件基类，提供 scope 支持。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (EventBase)
type EventBase struct {
	// Scope 作用域
	Scope string
}

// LLMCallEventData LLM 调用事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: AsyncCallbackFramework.trigger() 中的 kwargs 参数集合
type LLMCallEventData struct {
	// Event 事件类型
	Event LLMCallEventType
	// ModelName 模型名称
	ModelName string
	// ModelProvider 模型服务商
	ModelProvider string
	// Messages 请求消息列表（敏感模式下为 nil）
	Messages any
	// Tools 请求工具列表（敏感模式下为 nil）
	Tools any
	// Temperature 采样温度
	Temperature *float64
	// TopP Top-p 采样参数
	TopP *float64
	// MaxTokens 最大 token 数
	MaxTokens *int
	// IsStream 是否流式调用
	IsStream bool
	// Response LLM 响应（*AssistantMessage 或 *AssistantMessageChunk）
	Response any
	// Usage 用量元数据
	Usage *llmschema.UsageMetadata
	// Error 错误信息
	Error error
	// Extra 额外数据（如 model_config, model_client_config, session_id 等）
	Extra map[string]any
}

// ToolCallEventData 工具调用事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: _ToolMeta.__call__ 中 trigger 调用时的 kwargs 参数集合
type ToolCallEventData struct {
	// Event 事件类型
	Event ToolCallEventType
	// ToolName 工具名称
	ToolName string
	// ToolID 工具 ID
	ToolID string
	// Inputs 调用输入参数
	Inputs map[string]any
	// Result 调用结果（Finished/InvokeOutput/StreamOutput 时有值）
	Result map[string]any
	// Error 错误信息（Error 事件时有值）
	Error error
	// Extra 额外数据
	Extra map[string]any
}

// SessionCallEventData Session 调用事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: openjiuwen/core/session/agent.py 中 trigger(SessionEvents.AGENT_SESSION_CREATED, ...) 的 kwargs
type SessionCallEventData struct {
	// Event 事件类型
	Event SessionCallEventType
	// SessionID 会话标识
	SessionID string
	// Card Agent 身份元数据
	Card any
	// Session 会话实例
	Session any
	// Extra 额外数据
	Extra map[string]any
}

// ContextCallEventData 上下文调用事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (ContextEvents) +
//
//	openjiuwen/core/runner/callback/framework.py (trigger kwargs)
type ContextCallEventData struct {
	// Event 事件类型
	Event ContextCallEventType
	// SessionID 会话标识
	SessionID string
	// ContextID 上下文标识
	ContextID string
	// State 压缩状态（仅 ContextCompressionStateEvent 事件有值，实际类型 *schema.ContextCompressionState）
	State any
	// Context 上下文实例引用（实际类型 context_engine.ModelContext）
	Context any
	// Extra 额外数据
	Extra map[string]any
}

// WorkflowEventData Workflow 事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (WorkflowEvents)
type WorkflowEventData struct {
	// Event 事件类型
	Event WorkflowEventType
	// WorkflowID 工作流标识
	WorkflowID string
	// NodeID 节点标识
	NodeID string
	// Inputs 调用输入参数
	Inputs map[string]any
	// Result 调用结果
	Result any
	// Error 错误信息
	Error error
	// Extra 额外数据
	Extra map[string]any
}

// AgentTeamEventData Agent 协作事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (AgentTeamEvents)
type AgentTeamEventData struct {
	// Event 事件类型
	Event AgentTeamEventType
	// AgentID Agent 标识
	AgentID string
	// Message 消息内容
	Message any
	// Extra 额外数据
	Extra map[string]any
}

// RetrievalEventData 检索事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (RetrievalEvents)
type RetrievalEventData struct {
	// Event 事件类型
	Event RetrievalEventType
	// Query 检索查询
	Query string
	// Results 检索结果
	Results any
	// Extra 额外数据
	Extra map[string]any
}

// MemoryEventData 记忆事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (MemoryEvents)
type MemoryEventData struct {
	// Event 事件类型
	Event MemoryEventType
	// Key 记忆键
	Key string
	// Value 记忆值
	Value any
	// Extra 额外数据
	Extra map[string]any
}

// TaskManagerEventData 任务管理事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (TaskManagerEvents)
type TaskManagerEventData struct {
	// Event 事件类型
	Event TaskManagerEventType
	// TaskID 任务标识
	TaskID string
	// Status 任务状态
	Status string
	// Result 任务结果
	Result any
	// Error 错误信息
	Error error
	// Extra 额外数据
	Extra map[string]any
}

// GlobalAgentEventData Agent 调用事件数据。
type GlobalAgentEventData struct {
	// Event 事件类型
	Event GlobalAgentEventType
	// AgentID Agent 标识
	AgentID string
	// AgentName Agent 名称
	AgentName string
	// Inputs 调用输入
	Inputs map[string]any
	// Result 调用结果（InvokeOutput/StreamOutput 时有值）
	Result any
	// Session 会话实例（实际类型 *session.Session）
	Session any
	// Error 错误信息
	Error error
	// Extra 额外数据
	Extra map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// LLMCallEventType LLM 调用事件类型。
//
// 事件名格式 "_framework:{event_name}"，与 Python EventBase.get_event() 构建规则一致。
// 默认作用域为 "_framework"。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (LLMCallEvents)
type LLMCallEventType string

// ToolCallEventType 工具调用事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (ToolCallEvents)
type ToolCallEventType string

// SessionCallEventType Session 调用事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (SessionEvents)
type SessionCallEventType string

// ContextCallEventType 上下文调用事件类型。
//
// 事件名格式 "_framework:{event_name}"，与 Python EventBase.get_event() 构建规则一致。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (ContextEvents)
type ContextCallEventType string

// GlobalAgentEventType Agent 调用全局事件类型。
//
// 与 Rail 层 AgentCallbackEvent（per-Agent 实例级事件）区分：
//   - GlobalAgentEventType = 框架级全局观测（日志/监控/transform_io）
//   - AgentCallbackEvent = 实例级 Rail 拦截/控制（重试/提前终止/steering）
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (AgentEvents)
type GlobalAgentEventType string

// WorkflowEventType Workflow 事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (WorkflowEvents)
type WorkflowEventType string

// AgentTeamEventType Agent 协作事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (AgentTeamEvents)
type AgentTeamEventType string

// RetrievalEventType 检索事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (RetrievalEvents)
type RetrievalEventType string

// MemoryEventType 记忆事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (MemoryEvents)
type MemoryEventType string

// TaskManagerEventType 任务管理事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (TaskManagerEvents)
type TaskManagerEventType string

// TransformLLMIOInputFunc LLM 层输入变换回调函数类型。
// 接收事件名和原始输入，返回变换后的输入。
// 对齐 Python: transform_io 的 input_fn（LLM_STREAM_INPUT / LLM_INVOKE_INPUT）
type TransformLLMIOInputFunc func(ctx context.Context, event LLMCallEventType, input any) any

// TransformLLMIOOutputFunc LLM 层输出变换回调函数类型。
// 接收事件名和原始输出，返回变换后的输出。
// 对齐 Python: transform_io 的 output_fn（LLM_STREAM_OUTPUT / LLM_INVOKE_OUTPUT）
type TransformLLMIOOutputFunc func(ctx context.Context, event LLMCallEventType, output any) any

// TransformAgentIOInputFunc Agent 层输入变换回调函数类型。
// 对齐 Python: transform_io 的 input_fn（AGENT_STREAM_INPUT / AGENT_INVOKE_INPUT）
type TransformAgentIOInputFunc func(ctx context.Context, event GlobalAgentEventType, input any) any

// TransformAgentIOOutputFunc Agent 层输出变换回调函数类型。
// 对齐 Python: transform_io 的 output_fn（AGENT_STREAM_OUTPUT / AGENT_INVOKE_OUTPUT）
type TransformAgentIOOutputFunc func(ctx context.Context, event GlobalAgentEventType, output any) any

// TransformToolIOInputFunc Tool 层输入变换回调函数类型。
// 接收事件名和原始输入，返回变换后的输入。
// 对齐 Python: transform_io 的 input_fn（TOOL_STREAM_INPUT / TOOL_INVOKE_INPUT）
type TransformToolIOInputFunc func(ctx context.Context, event ToolCallEventType, input map[string]any) map[string]any

// TransformToolIOOutputFunc Tool 层输出变换回调函数类型。
// 接收事件名和原始输出，返回变换后的输出。
// 对齐 Python: transform_io 的 output_fn（TOOL_STREAM_OUTPUT / TOOL_INVOKE_OUTPUT）
type TransformToolIOOutputFunc func(ctx context.Context, event ToolCallEventType, output map[string]any) map[string]any

// GlobalAgentCallbackFunc Agent 回调函数类型。
type GlobalAgentCallbackFunc func(ctx context.Context, data *GlobalAgentEventData) any

// PerAgentCallbackFunc 实例级 PerAgent 回调函数类型。
// agentCallbackContext 实际类型为 *rail.AgentCallbackContext，回调内需类型断言。
//
// 对应 Python: AnyAgentCallback = Union[AgentCallback, SyncAgentCallback]
type PerAgentCallbackFunc func(ctx context.Context, agentCallbackContext any) error

// WorkflowCallbackFunc Workflow 回调函数类型。
type WorkflowCallbackFunc func(ctx context.Context, data *WorkflowEventData) any

// AgentTeamCallbackFunc Agent 协作回调函数类型。
type AgentTeamCallbackFunc func(ctx context.Context, data *AgentTeamEventData) any

// RetrievalCallbackFunc 检索回调函数类型。
type RetrievalCallbackFunc func(ctx context.Context, data *RetrievalEventData) any

// MemoryCallbackFunc 记忆回调函数类型。
type MemoryCallbackFunc func(ctx context.Context, data *MemoryEventData) any

// TaskManagerCallbackFunc 任务管理回调函数类型。
type TaskManagerCallbackFunc func(ctx context.Context, data *TaskManagerEventData) any

// ──────────────────────────── 常量 ────────────────────────────
const (
	// LLMCallStarted LLM 调用启动
	LLMCallStarted LLMCallEventType = "_framework:llm_call_started"
	// LLMCallError LLM 调用失败
	LLMCallError LLMCallEventType = "_framework:llm_call_error"
	// LLMResponseReceived LLM 响应接收（流式/非流式）
	LLMResponseReceived LLMCallEventType = "_framework:llm_response_received"
	// LLMInvokeInput invoke 调用前触发
	LLMInvokeInput LLMCallEventType = "_framework:llm_invoke_input"
	// LLMInvokeOutput invoke 调用后触发
	LLMInvokeOutput LLMCallEventType = "_framework:llm_invoke_output"
	// LLMStreamInput stream 调用前触发
	LLMStreamInput LLMCallEventType = "_framework:llm_stream_input"
	// LLMStreamOutput stream 每项触发
	LLMStreamOutput LLMCallEventType = "_framework:llm_stream_output"
	// LLMInput 请求前触发（含 messages/tools），细粒度事件
	LLMInput LLMCallEventType = "_framework:llm_input"
	// LLMOutput 响应后触发（含 response/usage），细粒度事件
	LLMOutput LLMCallEventType = "_framework:llm_output"
)

const (
	// ToolCallStarted 工具调用启动
	ToolCallStarted ToolCallEventType = "_framework:tool_call_started"
	// ToolCallFinished 工具调用完成
	ToolCallFinished ToolCallEventType = "_framework:tool_call_finished"
	// ToolCallError 工具调用出错
	ToolCallError ToolCallEventType = "_framework:tool_call_error"
	// ToolResultReceived 工具结果接收（流式逐 chunk）
	ToolResultReceived ToolCallEventType = "_framework:tool_result_received"
	// ToolParseStarted 工具参数解析开始
	ToolParseStarted ToolCallEventType = "_framework:tool_parse_started"
	// ToolParseFinished 工具参数解析完成
	ToolParseFinished ToolCallEventType = "_framework:tool_parse_finished"
	// ToolInvokeInput invoke 调用前触发
	ToolInvokeInput ToolCallEventType = "_framework:tool_invoke_input"
	// ToolInvokeOutput invoke 调用后触发
	ToolInvokeOutput ToolCallEventType = "_framework:tool_invoke_output"
	// ToolStreamInput stream 调用前触发
	ToolStreamInput ToolCallEventType = "_framework:tool_stream_input"
	// ToolStreamOutput stream 每项触发
	ToolStreamOutput ToolCallEventType = "_framework:tool_stream_output"
	// ToolAuth 工具认证事件
	ToolAuth ToolCallEventType = "_framework:tool_auth"
)

const (
	// SessionCreated 会话创建事件
	SessionCreated SessionCallEventType = "_framework:session_created"
	// AgentSessionCreated Agent 会话创建事件
	AgentSessionCreated SessionCallEventType = "_framework:agent_session_created"
)

const (
	// ContextUpdated 上下文更新事件（add_messages 后触发）
	ContextUpdated ContextCallEventType = "_framework:context_updated"
	// ContextOffloaded 上下文卸载事件（offload 后触发）
	ContextOffloaded ContextCallEventType = "_framework:context_offloaded"
	// ContextRetrieved 上下文检索事件（get_context_window 后触发）
	ContextRetrieved ContextCallEventType = "_framework:context_retrieved"
	// ContextCleared 上下文清空事件（clear 后触发）
	ContextCleared ContextCallEventType = "_framework:context_cleared"
	// ContextCompressionStateEvent 压缩状态事件（处理器执行后触发）
	ContextCompressionStateEvent ContextCallEventType = "_framework:context.compression_state"
)

const (
	// GlobalAgentStarted Agent 执行启动
	GlobalAgentStarted GlobalAgentEventType = "_framework:agent_started"
	// GlobalAgentInvokeInput invoke 调用前触发
	GlobalAgentInvokeInput GlobalAgentEventType = "_framework:agent_invoke_input"
	// GlobalAgentInvokeOutput invoke 调用后触发
	GlobalAgentInvokeOutput GlobalAgentEventType = "_framework:agent_invoke_output"
	// GlobalAgentStreamInput stream 调用前触发
	GlobalAgentStreamInput GlobalAgentEventType = "_framework:agent_stream_input"
	// GlobalAgentStreamOutput stream 每项触发
	GlobalAgentStreamOutput GlobalAgentEventType = "_framework:agent_stream_output"
)

const (
	// WorkflowStarted 工作流启动
	WorkflowStarted WorkflowEventType = "_framework:workflow_started"
	// WorkflowFinished 工作流完成
	WorkflowFinished WorkflowEventType = "_framework:workflow_finished"
	// WorkflowError 工作流出错
	WorkflowError WorkflowEventType = "_framework:workflow_error"
	// WorkflowCancelled 工作流取消
	WorkflowCancelled WorkflowEventType = "_framework:workflow_cancelled"
	// NodeExecuted 节点执行完成
	NodeExecuted WorkflowEventType = "_framework:node_executed"
	// NodeError 节点执行出错
	NodeError WorkflowEventType = "_framework:node_error"
	// EdgeTraversed 边遍历
	EdgeTraversed WorkflowEventType = "_framework:edge_traversed"
	// LoopStarted 循环开始
	LoopStarted WorkflowEventType = "_framework:loop_started"
	// LoopFinished 循环结束
	LoopFinished WorkflowEventType = "_framework:loop_finished"
	// WorkflowInvokeInput invoke 调用前触发
	WorkflowInvokeInput WorkflowEventType = "_framework:workflow_invoke_input"
	// WorkflowInvokeOutput invoke 调用后触发
	WorkflowInvokeOutput WorkflowEventType = "_framework:workflow_invoke_output"
	// WorkflowStreamInput stream 调用前触发
	WorkflowStreamInput WorkflowEventType = "_framework:workflow_stream_input"
	// WorkflowStreamOutput stream 每项触发
	WorkflowStreamOutput WorkflowEventType = "_framework:workflow_stream_output"
	// ComponentBatchInput 组件批量输入
	ComponentBatchInput WorkflowEventType = "_framework:component_batch_input"
	// ComponentBatchOutput 组件批量输出
	ComponentBatchOutput WorkflowEventType = "_framework:component_batch_output"
	// ComponentStreamInput 组件流式输入
	ComponentStreamInput WorkflowEventType = "_framework:component_stream_input"
)

const (
	// AgentP2PReceived 点对点消息接收
	AgentP2PReceived AgentTeamEventType = "_framework:agent_p2p_received"
	// AgentPubsubReceived 发布订阅消息接收
	AgentPubsubReceived AgentTeamEventType = "_framework:agent_pubsub_received"
)

const (
	// RetrievalStarted 检索启动
	RetrievalStarted RetrievalEventType = "_framework:retrieval_started"
)

const (
	// MemoryAdded 记忆新增
	MemoryAdded MemoryEventType = "_framework:memory_added"
	// MemoryUpdated 记忆更新
	MemoryUpdated MemoryEventType = "_framework:memory_updated"
	// MemoryDeleted 记忆删除
	MemoryDeleted MemoryEventType = "_framework:memory_deleted"
	// MemorySearchStarted 记忆检索启动
	MemorySearchStarted MemoryEventType = "_framework:memory_search_started"
	// MemorySearchFinished 记忆检索完成
	MemorySearchFinished MemoryEventType = "_framework:memory_search_finished"
)

const (
	// TaskCreated 任务创建
	TaskCreated TaskManagerEventType = "_framework:task_created"
	// TaskRunning 任务运行中
	TaskRunning TaskManagerEventType = "_framework:task_running"
	// TaskCompleted 任务完成
	TaskCompleted TaskManagerEventType = "_framework:task_completed"
	// TaskFailed 任务失败
	TaskFailed TaskManagerEventType = "_framework:task_failed"
	// TaskCancelled 任务取消
	TaskCancelled TaskManagerEventType = "_framework:task_cancelled"
	// TaskTimeout 任务超时
	TaskTimeout TaskManagerEventType = "_framework:task_timeout"
)

// DefaultScope 默认作用域，与 Python DEFAULT_SCOPE 一致。
const DefaultScope = "_framework"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildEventName 构建带 scope 的事件名。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py build_event_name(scope, event_name)
func BuildEventName(scope, eventName string) string {
	return scope + ":" + eventName
}

// ParseEventName 解析带 scope 的事件名，返回 (scope, eventName)。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py parse_event_name(scoped_event)
func ParseEventName(scopedEvent string) (scope, eventName string) {
	for i := 0; i < len(scopedEvent); i++ {
		if scopedEvent[i] == ':' {
			return scopedEvent[:i], scopedEvent[i+1:]
		}
	}
	return DefaultScope, scopedEvent
}

// GetEvent 获取带 scope 的完整事件名。
//
// 对应 Python: EventBase.get_event(event_name)
func (e *EventBase) GetEvent(eventName string) string {
	return BuildEventName(e.Scope, eventName)
}

// NewToolCallEventData 创建工具调用事件数据。
func NewToolCallEventData(event ToolCallEventType, card commonschema.CardInterface) *ToolCallEventData {
	if card == nil {
		return &ToolCallEventData{Event: event}
	}
	return &ToolCallEventData{
		Event:    event,
		ToolName: card.GetName(),
		ToolID:   card.GetID(),
	}
}

// String 实现 fmt.Stringer 接口。
func (t ToolCallEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口。
func (t LLMCallEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *ToolCallEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("ToolCallEventData{事件:%s, 工具名:%s, 工具ID:%s}", d.Event, d.ToolName, d.ToolID)
}

// String 实现 fmt.Stringer 接口。
func (t SessionCallEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *SessionCallEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("SessionCallEventData{事件:%s, 会话ID:%s}", d.Event, d.SessionID)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *LLMCallEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("LLMCallEventData{事件:%s, 模型名:%s, 服务商:%s}", d.Event, d.ModelName, d.ModelProvider)
}

// String 实现 fmt.Stringer 接口。
func (t ContextCallEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *ContextCallEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("ContextCallEventData{事件:%s, 会话ID:%s, 上下文ID:%s}", d.Event, d.SessionID, d.ContextID)
}

// String 实现 fmt.Stringer 接口。
func (t GlobalAgentEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *GlobalAgentEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("GlobalAgentEventData{事件:%s, AgentID:%s, AgentName:%s}", d.Event, d.AgentID, d.AgentName)
}

// String 实现 fmt.Stringer 接口。
func (t WorkflowEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *WorkflowEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("WorkflowEventData{事件:%s, 工作流ID:%s, 节点ID:%s}", d.Event, d.WorkflowID, d.NodeID)
}

// String 实现 fmt.Stringer 接口。
func (t AgentTeamEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *AgentTeamEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("AgentTeamEventData{事件:%s, AgentID:%s}", d.Event, d.AgentID)
}

// String 实现 fmt.Stringer 接口。
func (t RetrievalEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *RetrievalEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("RetrievalEventData{事件:%s, 查询:%s}", d.Event, d.Query)
}

// String 实现 fmt.Stringer 接口。
func (t MemoryEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *MemoryEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("MemoryEventData{事件:%s, 键:%s}", d.Event, d.Key)
}

// String 实现 fmt.Stringer 接口。
func (t TaskManagerEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *TaskManagerEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("TaskManagerEventData{事件:%s, 任务ID:%s, 状态:%s}", d.Event, d.TaskID, d.Status)
}
