package callback

import (
	"context"
	"fmt"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LLMCallbackFunc LLM 回调函数类型。
//
// 回调函数接收 context 和事件数据，用于监听 LLM 调用生命周期事件。
// 回调函数应为只读的（不应修改传入的数据），变换型回调在 6.24 节实现。
type LLMCallbackFunc func(ctx context.Context, data *LLMCallEventData)

// ToolCallbackFunc 工具回调函数类型。
type ToolCallbackFunc func(ctx context.Context, data *ToolCallEventData)

// CallbackFramework 回调框架，事件注册与触发的核心结构。
//
// 统一管理 LLM 和 Tool 事件的注册与触发。
// 2.14 节仅实现最小子集：OnLLM/OffLLM/TriggerLLM、OnTool/OffTool/TriggerTool。
// 完整能力（过滤器/熔断器/链式执行/装饰器/transform_io）在 6.24 节实现。
//
// 对应 Python: openjiuwen/core/runner/callback/framework.py (AsyncCallbackFramework)
// 命名区别：Go 为同步调用（无 async/await），去掉 Async 前缀。
type CallbackFramework struct {
	mu             sync.RWMutex
	llmCallbacks   map[LLMCallEventType][]LLMCallbackFunc
	toolCallbacks  map[ToolCallEventType][]ToolCallbackFunc
}

// ──────────────────────────── 全局变量 ────────────────────────────

// globalCallbackFramework 全局回调框架单例。
//
// 对应 Python: Runner.callback_framework（Runner 初始化时创建的全局单例）
var globalCallbackFramework = NewCallbackFramework()

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCallbackFramework 创建回调框架实例。
//
// 默认注册 LLM 日志回调，保持与原有日志行为一致。
func NewCallbackFramework() *CallbackFramework {
	fw := &CallbackFramework{
		llmCallbacks:  make(map[LLMCallEventType][]LLMCallbackFunc),
		toolCallbacks: make(map[ToolCallEventType][]ToolCallbackFunc),
	}
	// 默认注册 LLM 日志回调，保持与原有 logger.Info/Error 行为一致
	fw.OnLLM(LLMCallStarted, LoggingLLMCallback)
	fw.OnLLM(LLMCallError, LoggingLLMCallback)
	fw.OnLLM(LLMResponseReceived, LoggingLLMCallback)
	fw.OnLLM(LLMInvokeInput, LoggingLLMCallback)
	fw.OnLLM(LLMInvokeOutput, LoggingLLMCallback)
	fw.OnLLM(LLMStreamInput, LoggingLLMCallback)
	fw.OnLLM(LLMStreamOutput, LoggingLLMCallback)
	fw.OnLLM(LLMInput, LoggingLLMCallback)
	fw.OnLLM(LLMOutput, LoggingLLMCallback)
	return fw
}

// GetCallbackFramework 返回全局回调框架单例。
//
// 对应 Python: Runner.callback_framework 类属性
func GetCallbackFramework() *CallbackFramework {
	return globalCallbackFramework
}

// OnLLM 注册 LLM 事件回调函数。
//
// 同一事件可注册多个回调，按注册顺序执行。
//
// 对应 Python: AsyncCallbackFramework.on(event, callback)
func (fw *CallbackFramework) OnLLM(event LLMCallEventType, fn LLMCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.llmCallbacks[event] = append(fw.llmCallbacks[event], fn)
}

// OffLLM 注销 LLM 事件回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按指针匹配）。
// 若事件下无匹配回调，不做任何操作。
//
// 对应 Python: AsyncCallbackFramework.unregister(event, callback)
func (fw *CallbackFramework) OffLLM(event LLMCallEventType, fn LLMCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.llmCallbacks[event]
	if !ok {
		return
	}

	for i, cb := range callbacks {
		if fmt.Sprintf("%p", cb) == fmt.Sprintf("%p", fn) {
			fw.llmCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerLLM 触发 LLM 事件，按注册顺序调用所有回调。
//
// 若 ctx 为 nil 或 data 为 nil，直接返回不触发任何回调。
//
// 对应 Python: AsyncCallbackFramework.trigger(event, *args, **kwargs)
func (fw *CallbackFramework) TriggerLLM(ctx context.Context, data *LLMCallEventData) {
	if ctx == nil || data == nil {
		return
	}

	fw.mu.RLock()
	callbacks := fw.llmCallbacks[data.Event]
	fw.mu.RUnlock()

	for _, fn := range callbacks {
		fn(ctx, data)
	}
}

// OnTool 注册 Tool 事件回调函数。
func (fw *CallbackFramework) OnTool(event ToolCallEventType, fn ToolCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.toolCallbacks[event] = append(fw.toolCallbacks[event], fn)
}

// OffTool 注销 Tool 事件回调函数。
func (fw *CallbackFramework) OffTool(event ToolCallEventType, fn ToolCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.toolCallbacks[event]
	if !ok {
		return
	}

	for i, cb := range callbacks {
		if fmt.Sprintf("%p", cb) == fmt.Sprintf("%p", fn) {
			fw.toolCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerTool 触发 Tool 事件，按注册顺序调用所有回调。
func (fw *CallbackFramework) TriggerTool(ctx context.Context, data *ToolCallEventData) {
	if ctx == nil || data == nil {
		return
	}

	fw.mu.RLock()
	callbacks := fw.toolCallbacks[data.Event]
	fw.mu.RUnlock()

	for _, fn := range callbacks {
		fn(ctx, data)
	}
}

// GetCallbacksForTest 返回指定 LLM 事件的回调列表，仅供测试使用。
func (fw *CallbackFramework) GetCallbacksForTest(event LLMCallEventType) []LLMCallbackFunc {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	return fw.llmCallbacks[event]
}
