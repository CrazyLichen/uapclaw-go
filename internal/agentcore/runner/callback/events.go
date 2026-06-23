package callback

import (
	"context"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

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

// AgentCallEventData Agent 调用事件数据。
type AgentCallEventData struct {
	// Event 事件类型
	Event AgentCallGlobalEventType
	// AgentID Agent 标识
	AgentID string
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

// ToolCallEventType 工具调用事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (ToolCallEvents)
type ToolCallEventType string

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

// SessionCallEventType Session 调用事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (SessionEvents)
type SessionCallEventType string

const (
	// SessionCreated 会话创建事件
	SessionCreated SessionCallEventType = "_framework:session_created"
	// AgentSessionCreated Agent 会话创建事件
	AgentSessionCreated SessionCallEventType = "_framework:agent_session_created"
)

// ContextCallEventType 上下文调用事件类型。
//
// 事件名格式 "_framework:{event_name}"，与 Python EventBase.get_event() 构建规则一致。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (ContextEvents)
type ContextCallEventType string

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

// AgentCallGlobalEventType Agent 调用全局事件类型。
//
// 与 Rail 层 AgentCallbackEvent（per-Agent 实例级事件）区分：
//   - AgentCallGlobalEventType = 框架级全局观测（日志/监控/transform_io）
//   - AgentCallbackEvent = 实例级 Rail 拦截/控制（重试/提前终止/steering）
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (AgentEvents)
type AgentCallGlobalEventType string

const (
	// AgentStarted Agent 执行启动
	AgentStarted AgentCallGlobalEventType = "_framework:agent_started"
	// AgentInvokeInput invoke 调用前触发
	AgentInvokeInput AgentCallGlobalEventType = "_framework:agent_invoke_input"
	// AgentInvokeOutput invoke 调用后触发
	AgentInvokeOutput AgentCallGlobalEventType = "_framework:agent_invoke_output"
	// AgentStreamInput stream 调用前触发
	AgentStreamInput AgentCallGlobalEventType = "_framework:agent_stream_input"
	// AgentStreamOutput stream 每项触发
	AgentStreamOutput AgentCallGlobalEventType = "_framework:agent_stream_output"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolCallEventData 创建工具调用事件数据。
func NewToolCallEventData(event ToolCallEventType, card *commonschema.BaseCard) *ToolCallEventData {
	if card == nil {
		return &ToolCallEventData{Event: event}
	}
	return &ToolCallEventData{
		Event:    event,
		ToolName: card.Name,
		ToolID:   card.ID,
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
type TransformAgentIOInputFunc func(ctx context.Context, event AgentCallGlobalEventType, input any) any

// TransformAgentIOOutputFunc Agent 层输出变换回调函数类型。
// 对齐 Python: transform_io 的 output_fn（AGENT_STREAM_OUTPUT / AGENT_INVOKE_OUTPUT）
type TransformAgentIOOutputFunc func(ctx context.Context, event AgentCallGlobalEventType, output any) any

// TransformToolIOInputFunc Tool 层输入变换回调函数类型。
// 接收事件名和原始输入，返回变换后的输入。
// 对齐 Python: transform_io 的 input_fn（TOOL_STREAM_INPUT / TOOL_INVOKE_INPUT）
type TransformToolIOInputFunc func(ctx context.Context, event ToolCallEventType, input map[string]any) map[string]any

// TransformToolIOOutputFunc Tool 层输出变换回调函数类型。
// 接收事件名和原始输出，返回变换后的输出。
// 对齐 Python: transform_io 的 output_fn（TOOL_STREAM_OUTPUT / TOOL_INVOKE_OUTPUT）
type TransformToolIOOutputFunc func(ctx context.Context, event ToolCallEventType, output map[string]any) map[string]any

// AgentCallbackFunc Agent 回调函数类型。
type AgentCallbackFunc func(ctx context.Context, data *AgentCallEventData) any

// String 实现 fmt.Stringer 接口。
func (t AgentCallGlobalEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *AgentCallEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("AgentCallEventData{事件:%s, AgentID:%s}", d.Event, d.AgentID)
}
