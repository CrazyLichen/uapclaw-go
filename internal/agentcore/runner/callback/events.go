package callback

import (
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
	return fmt.Sprintf("ToolCallEventData{Event:%s, ToolName:%s, ToolID:%s}", d.Event, d.ToolName, d.ToolID)
}

// String 实现 fmt.Stringer 接口。
func (t SessionCallEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *SessionCallEventData) String() string {
	return fmt.Sprintf("SessionCallEventData{Event:%s, SessionID:%s}", d.Event, d.SessionID)
}
