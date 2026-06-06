// Package callback 提供 LLM 调用回调框架，支持事件注册与触发。
//
// 独立子包设计避免循环依赖：llm/model.go 导入 model_clients，
// 而 model_clients 及其子包（openai/dashscope/...）需要使用回调框架，
// 因此将回调类型定义在独立子包中，由各方共同导入。
package callback

import (
	"context"
	"fmt"
	"sync"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

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

// CallbackFunc 回调函数类型。
//
// 回调函数接收 context 和事件数据，用于监听 LLM 调用生命周期事件。
// 回调函数应为只读的（不应修改传入的数据），变换型回调在 6.24 节实现。
type CallbackFunc func(ctx context.Context, data *LLMCallEventData)

// CallbackFramework 回调框架，LLM 事件注册与触发的核心结构。
//
// 2.14 节仅实现最小子集：On/Off/Trigger。
// 完整能力（过滤器/熔断器/链式执行/装饰器/transform_io）在 6.24 节实现。
//
// 对应 Python: openjiuwen/core/runner/callback/framework.py (AsyncCallbackFramework)
// 命名区别：Go 为同步调用（无 async/await），去掉 Async 前缀。
type CallbackFramework struct {
	mu        sync.RWMutex
	callbacks map[LLMCallEventType][]CallbackFunc
}

// ──────────────────────────── 全局变量 ────────────────────────────

// globalCallbackFramework 全局回调框架单例。
//
// 对应 Python: Runner.callback_framework（Runner 初始化时创建的全局单例）
var globalCallbackFramework = NewCallbackFramework()

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCallbackFramework 创建回调框架实例。
//
// 默认注册 LoggingCallback，保持与原有日志行为一致。
func NewCallbackFramework() *CallbackFramework {
	fw := &CallbackFramework{
		callbacks: make(map[LLMCallEventType][]CallbackFunc),
	}
	// 默认注册日志回调，保持与原有 logger.Info/Error 行为一致
	fw.On(LLMCallStarted, LoggingCallback)
	fw.On(LLMCallError, LoggingCallback)
	fw.On(LLMResponseReceived, LoggingCallback)
	fw.On(LLMInvokeInput, LoggingCallback)
	fw.On(LLMInvokeOutput, LoggingCallback)
	fw.On(LLMStreamInput, LoggingCallback)
	fw.On(LLMStreamOutput, LoggingCallback)
	fw.On(LLMInput, LoggingCallback)
	fw.On(LLMOutput, LoggingCallback)
	return fw
}

// GetCallbackFramework 返回全局回调框架单例。
//
// 对应 Python: Runner.callback_framework 类属性
func GetCallbackFramework() *CallbackFramework {
	return globalCallbackFramework
}

// On 注册回调函数。
//
// 同一事件可注册多个回调，按注册顺序执行。
// 2.14 节不支持 priority 参数，6.24 节扩展时添加。
//
// 对应 Python: AsyncCallbackFramework.on(event, callback)
func (fw *CallbackFramework) On(event LLMCallEventType, fn CallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	fw.callbacks[event] = append(fw.callbacks[event], fn)
}

// Off 注销回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按指针匹配）。
// 若事件下无匹配回调，不做任何操作。
//
// 对应 Python: AsyncCallbackFramework.unregister(event, callback)
func (fw *CallbackFramework) Off(event LLMCallEventType, fn CallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.callbacks[event]
	if !ok {
		return
	}

	for i, cb := range callbacks {
		// 通过指针比较匹配回调函数
		if fmt.Sprintf("%p", cb) == fmt.Sprintf("%p", fn) {
			fw.callbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// Trigger 触发事件，按注册顺序调用所有回调。
//
// 若 ctx 为 nil 或 data 为 nil，直接返回不触发任何回调。
//
// 对应 Python: AsyncCallbackFramework.trigger(event, *args, **kwargs)
func (fw *CallbackFramework) Trigger(ctx context.Context, data *LLMCallEventData) {
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

// LoggingCallback 默认日志回调，将事件数据记录到 zerolog。
//
// 此回调保持与原有散落在各 model_client 中的 logger.Info/Error 行为一致，
// 作为 CallbackFramework 的默认注册回调，确保不丢失任何日志。
func LoggingCallback(ctx context.Context, data *LLMCallEventData) {
	switch data.Event {
	case LLMCallStarted, LLMInvokeInput, LLMStreamInput, LLMInput:
		logLLMStart(ctx, data)
	case LLMCallError:
		logLLMError(ctx, data)
	case LLMResponseReceived, LLMInvokeOutput, LLMStreamOutput, LLMOutput:
		logLLMEnd(ctx, data)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// logLLMStart 记录 LLM 调用开始日志。
func logLLMStart(_ context.Context, data *LLMCallEventData) {
	evt := logger.Info(logger.ComponentAgentCore).
		Str("event_type", string(data.Event)).
		Str("model_name", data.ModelName).
		Str("model_provider", data.ModelProvider).
		Bool("is_stream", data.IsStream)

	if data.Temperature != nil {
		evt = evt.Float64("temperature", *data.Temperature)
	}
	if data.TopP != nil {
		evt = evt.Float64("top_p", *data.TopP)
	}
	if data.MaxTokens != nil {
		evt = evt.Int("max_tokens", *data.MaxTokens)
	}
	if data.Messages != nil {
		evt = evt.Any("messages", data.Messages)
	}
	if data.Tools != nil {
		evt = evt.Any("tools", data.Tools)
	}
	for k, v := range data.Extra {
		evt = evt.Any(k, v)
	}

	evt.Msg("LLM call started.")
}

// logLLMError 记录 LLM 调用错误日志。
func logLLMError(_ context.Context, data *LLMCallEventData) {
	evt := logger.Error(logger.ComponentAgentCore).
		Str("event_type", string(data.Event)).
		Str("model_name", data.ModelName).
		Str("model_provider", data.ModelProvider).
		Bool("is_stream", data.IsStream)

	if data.Error != nil {
		evt = evt.Err(data.Error)
	}
	if data.Messages != nil {
		evt = evt.Any("messages", data.Messages)
	}
	if data.Tools != nil {
		evt = evt.Any("tools", data.Tools)
	}
	for k, v := range data.Extra {
		evt = evt.Any(k, v)
	}

	evt.Msg("LLM call error.")
}

// logLLMEnd 记录 LLM 调用结束日志。
func logLLMEnd(_ context.Context, data *LLMCallEventData) {
	evt := logger.Info(logger.ComponentAgentCore).
		Str("event_type", string(data.Event)).
		Str("model_name", data.ModelName).
		Str("model_provider", data.ModelProvider).
		Bool("is_stream", data.IsStream)

	if data.Usage != nil {
		evt = evt.Int("input_tokens", data.Usage.InputTokens).
			Int("output_tokens", data.Usage.OutputTokens).
			Int("total_tokens", data.Usage.TotalTokens)
	}
	if data.Response != nil {
		evt = evt.Any("response_type", fmt.Sprintf("%T", data.Response))
	}
	for k, v := range data.Extra {
		evt = evt.Any(k, v)
	}

	evt.Msg("LLM call completed.")
}
