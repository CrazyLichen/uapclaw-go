// Package callback 提供 LLM 调用回调框架的类型重新导出。
//
// 真实实现已迁移至 agentcore/runner/callback/，
// 本包仅保留类型别名和函数别名以保持 API 兼容性。
// 新代码应直接使用 agentcore/runner/callback 包。
package callback

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 类型别名（导出） ────────────────────────────

// LLMCallEventType LLM 调用事件类型。详见 callback.LLMCallEventType。
type LLMCallEventType = callback.LLMCallEventType

// LLMCallEventData LLM 调用事件数据。详见 callback.LLMCallEventData。
type LLMCallEventData = callback.LLMCallEventData

// CallbackFunc LLM 回调函数类型。详见 callback.LLMCallbackFunc。
type CallbackFunc = callback.LLMCallbackFunc

// CallbackFramework 回调框架。详见 callback.CallbackFramework。
type CallbackFramework = callback.CallbackFramework

// ──────────────────────────── 常量别名（导出） ────────────────────────────

const (
	// LLMCallStarted LLM 调用启动
	LLMCallStarted LLMCallEventType = callback.LLMCallStarted
	// LLMCallError LLM 调用失败
	LLMCallError LLMCallEventType = callback.LLMCallError
	// LLMResponseReceived LLM 响应接收（流式/非流式）
	LLMResponseReceived LLMCallEventType = callback.LLMResponseReceived
	// LLMInvokeInput invoke 调用前触发
	LLMInvokeInput LLMCallEventType = callback.LLMInvokeInput
	// LLMInvokeOutput invoke 调用后触发
	LLMInvokeOutput LLMCallEventType = callback.LLMInvokeOutput
	// LLMStreamInput stream 调用前触发
	LLMStreamInput LLMCallEventType = callback.LLMStreamInput
	// LLMStreamOutput stream 每项触发
	LLMStreamOutput LLMCallEventType = callback.LLMStreamOutput
	// LLMInput 请求前触发（含 messages/tools），细粒度事件
	LLMInput LLMCallEventType = callback.LLMInput
	// LLMOutput 响应后触发（含 response/usage），细粒度事件
	LLMOutput LLMCallEventType = callback.LLMOutput
)

// ──────────────────────────── 函数别名（导出） ────────────────────────────

// NewCallbackFramework 创建回调框架实例。详见 callback.NewCallbackFramework。
var NewCallbackFramework = callback.NewCallbackFramework

// GetCallbackFramework 返回全局回调框架单例。详见 callback.GetCallbackFramework。
var GetCallbackFramework = callback.GetCallbackFramework

// LoggingCallback 默认日志回调。详见 callback.LoggingLLMCallback。
var LoggingCallback = callback.LoggingLLMCallback
