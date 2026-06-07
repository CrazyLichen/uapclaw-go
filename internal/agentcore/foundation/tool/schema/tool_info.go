// Package schema 定义工具系统的事件类型和事件数据。
//
// 本包独立于 tool 主包，避免 LifecycleTool 与 Tool 之间的循环依赖。
//
// 文件目录：
//
//	schema/
//	└── tool_info.go   # ToolCallEventType + ToolCallEventData + ToolCallbackFramework
//
// 对应 Python 代码：openjiuwen/core/runner/callback/events.py (ToolCallEvents)
package schema

import (
	"context"
	"fmt"
	"sync"

	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 枚举 ────────────────────────────

// ToolCallEventType 工具调用事件类型。
//
// 事件名格式 "_framework:{event_name}"，与 Python EventBase.get_event() 构建规则一致。
// 与 LLMCallEventType 并列，使用相同的 scope "_framework"。
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

// ──────────────────────────── 结构体 ────────────────────────────

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

// ToolCallbackFunc 工具回调函数类型。
type ToolCallbackFunc func(ctx context.Context, data *ToolCallEventData)

// ToolCallbackFramework 工具调用回调框架，独立于 LLM CallbackFramework。
//
// 与 llm/callback.CallbackFramework 设计一致但事件类型不同。
// 后续 6.24 节统一回调框架时合并。
//
// 对应 Python: AsyncCallbackFramework（Tool 事件部分）
type ToolCallbackFramework struct {
	mu        sync.RWMutex
	callbacks map[ToolCallEventType][]ToolCallbackFunc
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolCallbackFramework 创建工具回调框架实例。
func NewToolCallbackFramework() *ToolCallbackFramework {
	return &ToolCallbackFramework{
		callbacks: make(map[ToolCallEventType][]ToolCallbackFunc),
	}
}

// On 注册回调函数。
func (fw *ToolCallbackFramework) On(event ToolCallEventType, fn ToolCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.callbacks[event] = append(fw.callbacks[event], fn)
}

// Off 注销回调函数。
func (fw *ToolCallbackFramework) Off(event ToolCallEventType, fn ToolCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.callbacks[event]
	if !ok {
		return
	}

	for i, cb := range callbacks {
		if fmt.Sprintf("%p", cb) == fmt.Sprintf("%p", fn) {
			fw.callbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// Trigger 触发事件。
func (fw *ToolCallbackFramework) Trigger(ctx context.Context, data *ToolCallEventData) {
	if ctx == nil || data == nil {
		return
	}

	fw.mu.RLock()
	callbacks := fw.callbacks[data.Event]
	fw.mu.RUnlock()

	for _, fn := range callbacks {
		fn(ctx, data)
	}
}

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
