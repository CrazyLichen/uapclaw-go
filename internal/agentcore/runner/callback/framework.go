package callback

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CallbackFramework 回调框架，事件注册与触发的核心结构。
//
// 统一管理 LLM、Tool、Session 和自定义事件的注册与触发。
// 2.14 节仅实现最小子集：OnLLM/OffLLM/TriggerLLM、OnTool/OffTool/TriggerTool。
// 5.3 节扩展：OnSession/OffSession/TriggerSession。
// SW-31/32/33 扩展：OnCustom/OffCustom/OffAllCustom/TriggerCustom，支持动态事件名。
// 完整能力（过滤器/熔断器/链式执行/装饰器/transform_io）在 6.24 节实现。
//
// 对应 Python: openjiuwen/core/runner/callback/framework.py (AsyncCallbackFramework)
// 命名区别：Go 为同步调用（无 async/await），去掉 Async 前缀。
//
// 自定义事件域与 LLM/Tool/Session 域的设计差异：
//   - LLM/Tool/Session 域使用预定义枚举事件名和固定数据结构，适合框架内部生命周期事件
//   - 自定义域使用自由字符串事件名和 map[string]any 数据，对应 Python 的 trigger(event, **kwargs)
//   - Python 的 AsyncCallbackFramework 只有一个 _callbacks: Dict[str, List]，
//     所有事件（包括 "abc-123write_stream" 这类动态事件名）共用同一注册表。
//     Go 将其拆分为四个独立 map，自定义域承载动态事件名场景。
type CallbackFramework struct {
	// mu 并发读写锁
	mu sync.RWMutex
	// llmCallbacks LLM 回调函数注册表
	llmCallbacks map[LLMCallEventType][]*CallbackInfo[LLMCallbackFunc]
	// toolCallbacks 工具回调函数注册表
	toolCallbacks map[ToolCallEventType][]*CallbackInfo[ToolCallbackFunc]
	// sessionCallbacks 会话回调函数注册表
	sessionCallbacks map[SessionCallEventType][]*CallbackInfo[SessionCallbackFunc]
	// customCallbacks 自定义事件回调函数注册表
	//
	// 对应 Python: AsyncCallbackFramework._callbacks 中的动态事件名条目。
	// Python 用 session_id + "write_stream" 构造 per-session 事件名，
	// Go 在此 map 中以相同方式存储，实现 per-session 隔离。
	customCallbacks map[string][]*CallbackInfo[CustomCallbackFunc]
	// contextCallbacks 上下文事件回调函数注册表
	contextCallbacks map[ContextCallEventType][]*CallbackInfo[ContextCallbackFunc]
	// globalAgentCallbacks Agent 回调函数注册表
	globalAgentCallbacks map[GlobalAgentEventType][]*CallbackInfo[GlobalAgentCallbackFunc]
	// perAgentCallbacks 实例级 PerAgent 回调函数注册表
	//
	// 键格式为 "{agentID}_{event}"（如 "agent1_before_model_call"），由 AgentCallbackManager 构造。
	perAgentCallbacks map[string][]*CallbackInfo[PerAgentCallbackFunc]
	// llmTransformIO LLM 层 IO 变换回调注册表，键为 inputEvent
	llmTransformIO map[LLMCallEventType]*llmTransformIOEntry
	// agentTransformIO Agent 层 IO 变换回调注册表，键为 inputEvent
	agentTransformIO map[GlobalAgentEventType]*agentTransformIOEntry
	// toolTransformIO Tool 层 IO 变换回调注册表，键为 inputEvent 或 outputEvent
	toolTransformIO map[ToolCallEventType]*toolTransformIOEntry
}

// llmTransformIOEntry LLM 层 TransformIO 注册条目
type llmTransformIOEntry struct {
	// inputFn 输入变换函数
	inputFn TransformLLMIOInputFunc
	// outputFn 输出变换函数
	outputFn TransformLLMIOOutputFunc
}

// agentTransformIOEntry Agent 层 TransformIO 注册条目
type agentTransformIOEntry struct {
	// inputFn 输入变换函数
	inputFn TransformAgentIOInputFunc
	// outputFn 输出变换函数
	outputFn TransformAgentIOOutputFunc
}

// toolTransformIOEntry Tool 层 TransformIO 注册条目
type toolTransformIOEntry struct {
	// inputFn 输入变换函数
	inputFn TransformToolIOInputFunc
	// outputFn 输出变换函数
	outputFn TransformToolIOOutputFunc
}

// ──────────────────────────── 枚举 ────────────────────────────

// triggerStrategy 回调触发执行策略。
type triggerStrategy int

const (
	// strategyCollect 收集所有返回值，不中断（观测型）
	strategyCollect triggerStrategy = iota
	// strategyAbortOnError 遇 error 中断（控制型）
	strategyAbortOnError
)

// LLMCallbackFunc LLM 回调函数类型。
//
// 回调函数接收 context 和事件数据，用于监听 LLM 调用生命周期事件。
// 回调函数应为只读的（不应修改传入的数据），变换型回调在 6.24 节实现。
type LLMCallbackFunc func(ctx context.Context, data *LLMCallEventData) any

// ToolCallbackFunc 工具回调函数类型。
type ToolCallbackFunc func(ctx context.Context, data *ToolCallEventData) any

// SessionCallbackFunc Session 回调函数类型。
type SessionCallbackFunc func(ctx context.Context, data *SessionCallEventData) any

// CustomCallbackFunc 自定义事件回调函数类型。
//
// 对应 Python: AsyncCallbackFramework.on(event, callback) 中的 callback。
// Python 的回调通过 **kwargs 接收参数，Go 使用 map[string]any 传递。
// 事件名由调用方自由构造（如 sessionID + "write_stream"），
// 不受预定义枚举约束，适合 per-session 隔离等动态场景。
type CustomCallbackFunc func(ctx context.Context, data map[string]any) any

// ContextCallbackFunc 上下文事件回调函数类型。
type ContextCallbackFunc func(ctx context.Context, data *ContextCallEventData) any

// ──────────────────────────── 常量 ────────────────────────────

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
		llmCallbacks:         make(map[LLMCallEventType][]*CallbackInfo[LLMCallbackFunc]),
		toolCallbacks:        make(map[ToolCallEventType][]*CallbackInfo[ToolCallbackFunc]),
		sessionCallbacks:     make(map[SessionCallEventType][]*CallbackInfo[SessionCallbackFunc]),
		customCallbacks:      make(map[string][]*CallbackInfo[CustomCallbackFunc]),
		contextCallbacks:     make(map[ContextCallEventType][]*CallbackInfo[ContextCallbackFunc]),
		globalAgentCallbacks: make(map[GlobalAgentEventType][]*CallbackInfo[GlobalAgentCallbackFunc]),
		perAgentCallbacks:    make(map[string][]*CallbackInfo[PerAgentCallbackFunc]),
		llmTransformIO:       make(map[LLMCallEventType]*llmTransformIOEntry),
		agentTransformIO:     make(map[GlobalAgentEventType]*agentTransformIOEntry),
		toolTransformIO:      make(map[ToolCallEventType]*toolTransformIOEntry),
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
// 同一事件可注册多个回调，按优先级排序执行（Priority 降序，相同 Priority 按 CreatedAt 升序）。
//
// 对应 Python: AsyncCallbackFramework.on(event, callback)
func (fw *CallbackFramework) OnLLM(event LLMCallEventType, fn LLMCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[LLMCallbackFunc]{
		Callback:     fn,
		Priority:     cfg.Priority,
		Once:         cfg.Once,
		Enabled:      true,
		Namespace:    cfg.Namespace,
		Tags:         cfg.Tags,
		MaxRetries:   cfg.MaxRetries,
		RetryDelay:   cfg.RetryDelay,
		Timeout:      cfg.Timeout,
		CreatedAt:    float64(time.Now().UnixNano()) / 1e9,
		CallbackType: cfg.CallbackType,
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.llmCallbacks[event] = append(fw.llmCallbacks[event], info)
	sortCallbacks(fw.llmCallbacks[event])
}

// OffLLM 注销 LLM 事件回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按 info.Callback 的指针匹配）。
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

	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.llmCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerLLM 触发 LLM 事件，按优先级顺序调用所有回调，返回所有回调结果。
//
// 若 ctx 为 nil 或 data 为 nil，直接返回 nil。
//
// 对应 Python: AsyncCallbackFramework.trigger(event, *args, **kwargs) → List[Any]
func (fw *CallbackFramework) TriggerLLM(ctx context.Context, data *LLMCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.llmCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn LLMCallbackFunc, ctx context.Context, data *LLMCallEventData) (any, error) {
			return fn(ctx, data), nil
		},
	)
	return results
}

// OnTool 注册 Tool 事件回调函数。
func (fw *CallbackFramework) OnTool(event ToolCallEventType, fn ToolCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[ToolCallbackFunc]{
		Callback:     fn,
		Priority:     cfg.Priority,
		Once:         cfg.Once,
		Enabled:      true,
		Namespace:    cfg.Namespace,
		Tags:         cfg.Tags,
		MaxRetries:   cfg.MaxRetries,
		RetryDelay:   cfg.RetryDelay,
		Timeout:      cfg.Timeout,
		CreatedAt:    float64(time.Now().UnixNano()) / 1e9,
		CallbackType: cfg.CallbackType,
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.toolCallbacks[event] = append(fw.toolCallbacks[event], info)
	sortCallbacks(fw.toolCallbacks[event])
}

// OffTool 注销 Tool 事件回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按 info.Callback 的指针匹配）。
// 若事件下无匹配回调，不做任何操作。
//
// 对应 Python: AsyncCallbackFramework.unregister(event, callback)
func (fw *CallbackFramework) OffTool(event ToolCallEventType, fn ToolCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.toolCallbacks[event]
	if !ok {
		return
	}

	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.toolCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerTool 触发 Tool 事件，按优先级顺序调用所有回调，返回所有回调结果。
func (fw *CallbackFramework) TriggerTool(ctx context.Context, data *ToolCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.toolCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn ToolCallbackFunc, ctx context.Context, data *ToolCallEventData) (any, error) {
			return fn(ctx, data), nil
		},
	)
	return results
}

// OnSession 注册 Session 事件回调函数。
func (fw *CallbackFramework) OnSession(event SessionCallEventType, fn SessionCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[SessionCallbackFunc]{
		Callback:     fn,
		Priority:     cfg.Priority,
		Once:         cfg.Once,
		Enabled:      true,
		Namespace:    cfg.Namespace,
		Tags:         cfg.Tags,
		MaxRetries:   cfg.MaxRetries,
		RetryDelay:   cfg.RetryDelay,
		Timeout:      cfg.Timeout,
		CreatedAt:    float64(time.Now().UnixNano()) / 1e9,
		CallbackType: cfg.CallbackType,
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.sessionCallbacks[event] = append(fw.sessionCallbacks[event], info)
	sortCallbacks(fw.sessionCallbacks[event])
}

// OffSession 注销 Session 事件回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按 info.Callback 的指针匹配）。
// 若事件下无匹配回调，不做任何操作。
//
// 对应 Python: AsyncCallbackFramework.unregister(event, callback)
func (fw *CallbackFramework) OffSession(event SessionCallEventType, fn SessionCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.sessionCallbacks[event]
	if !ok {
		return
	}

	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.sessionCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerSession 触发 Session 事件，按优先级顺序调用所有回调，返回所有回调结果。
func (fw *CallbackFramework) TriggerSession(ctx context.Context, data *SessionCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.sessionCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn SessionCallbackFunc, ctx context.Context, data *SessionCallEventData) (any, error) {
			return fn(ctx, data), nil
		},
	)
	return results
}

// OnCustom 注册自定义事件回调函数。
//
// 同一事件可注册多个回调，按优先级排序执行。
// 事件名为自由字符串，不受预定义枚举约束。
//
// 对应 Python: AsyncCallbackFramework.on(event, callback)
func (fw *CallbackFramework) OnCustom(event string, fn CustomCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[CustomCallbackFunc]{
		Callback:     fn,
		Priority:     cfg.Priority,
		Once:         cfg.Once,
		Enabled:      true,
		Namespace:    cfg.Namespace,
		Tags:         cfg.Tags,
		MaxRetries:   cfg.MaxRetries,
		RetryDelay:   cfg.RetryDelay,
		Timeout:      cfg.Timeout,
		CreatedAt:    float64(time.Now().UnixNano()) / 1e9,
		CallbackType: cfg.CallbackType,
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.customCallbacks[event] = append(fw.customCallbacks[event], info)
	sortCallbacks(fw.customCallbacks[event])
}

// OffCustom 注销自定义事件的单个回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按 info.Callback 的指针匹配）。
// 若事件下无匹配回调，不做任何操作。
//
// 对应 Python: AsyncCallbackFramework.unregister(event, callback)
func (fw *CallbackFramework) OffCustom(event string, fn CustomCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.customCallbacks[event]
	if !ok {
		return
	}

	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.customCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// OffAllCustom 注销指定自定义事件的全部回调。
//
// 清除该事件名下的所有回调函数，常用于 session 结束时清理 per-session 回调。
// 与 OffCustom 不同：OffCustom 按指针移除单个回调，OffAllCustom 清除整个事件。
//
// 对应 Python: AsyncCallbackFramework.unregister_event(event)
func (fw *CallbackFramework) OffAllCustom(event string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	delete(fw.customCallbacks, event)
}

// TriggerCustom 触发自定义事件，按优先级顺序调用所有回调，返回所有回调结果。
//
// 若 ctx 为 nil，直接返回 nil。
// data 通过 map[string]any 传递，对应 Python 的 **kwargs。
//
// 对应 Python: await trigger(event, **kwargs)
func (fw *CallbackFramework) TriggerCustom(ctx context.Context, event string, data map[string]any) []any {
	if ctx == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.customCallbacks, event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn CustomCallbackFunc, ctx context.Context, data map[string]any) (any, error) {
			return fn(ctx, data), nil
		},
	)
	return results
}

// GetCallbacksForTest 返回指定 LLM 事件的回调列表，仅供测试使用。
func (fw *CallbackFramework) GetCallbacksForTest(event LLMCallEventType) []*CallbackInfo[LLMCallbackFunc] {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	return fw.llmCallbacks[event]
}

// OnContext 注册上下文事件回调函数。
//
// 同一事件可注册多个回调，按优先级排序执行。
//
// 对应 Python: AsyncCallbackFramework.on(event, callback)
func (fw *CallbackFramework) OnContext(event ContextCallEventType, fn ContextCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[ContextCallbackFunc]{
		Callback:     fn,
		Priority:     cfg.Priority,
		Once:         cfg.Once,
		Enabled:      true,
		Namespace:    cfg.Namespace,
		Tags:         cfg.Tags,
		MaxRetries:   cfg.MaxRetries,
		RetryDelay:   cfg.RetryDelay,
		Timeout:      cfg.Timeout,
		CreatedAt:    float64(time.Now().UnixNano()) / 1e9,
		CallbackType: cfg.CallbackType,
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.contextCallbacks[event] = append(fw.contextCallbacks[event], info)
	sortCallbacks(fw.contextCallbacks[event])
}

// OffContext 注销上下文事件回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按 info.Callback 的指针匹配）。
// 若事件下无匹配回调，不做任何操作。
//
// 对应 Python: AsyncCallbackFramework.unregister(event, callback)
func (fw *CallbackFramework) OffContext(event ContextCallEventType, fn ContextCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.contextCallbacks[event]
	if !ok {
		return
	}

	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.contextCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerContext 触发上下文事件，按优先级顺序调用所有回调，返回所有回调结果。
//
// 若 ctx 为 nil 或 data 为 nil，直接返回 nil。
//
// 对应 Python: AsyncCallbackFramework.trigger(event, **kwargs) → List[Any]
func (fw *CallbackFramework) TriggerContext(ctx context.Context, data *ContextCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.contextCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn ContextCallbackFunc, ctx context.Context, data *ContextCallEventData) (any, error) {
			return fn(ctx, data), nil
		},
	)
	return results
}

// OnGlobalAgent 注册 Agent 事件回调函数。
//
// 同一事件可注册多个回调，按优先级排序执行。
//
// 对应 Python: AsyncCallbackFramework.on(event, callback)
func (fw *CallbackFramework) OnGlobalAgent(event GlobalAgentEventType, fn GlobalAgentCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[GlobalAgentCallbackFunc]{
		Callback:     fn,
		Priority:     cfg.Priority,
		Once:         cfg.Once,
		Enabled:      true,
		Namespace:    cfg.Namespace,
		Tags:         cfg.Tags,
		MaxRetries:   cfg.MaxRetries,
		RetryDelay:   cfg.RetryDelay,
		Timeout:      cfg.Timeout,
		CreatedAt:    float64(time.Now().UnixNano()) / 1e9,
		CallbackType: cfg.CallbackType,
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.globalAgentCallbacks[event] = append(fw.globalAgentCallbacks[event], info)
	sortCallbacks(fw.globalAgentCallbacks[event])
}

// OffGlobalAgent 注销 Agent 事件回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按 info.Callback 的指针匹配）。
// 若事件下无匹配回调，不做任何操作。
//
// 对应 Python: AsyncCallbackFramework.unregister(event, callback)
func (fw *CallbackFramework) OffGlobalAgent(event GlobalAgentEventType, fn GlobalAgentCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.globalAgentCallbacks[event]
	if !ok {
		return
	}

	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.globalAgentCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerGlobalAgent 触发 Agent 事件，按优先级顺序调用所有回调，返回所有回调结果。
//
// 若 ctx 为 nil 或 data 为 nil，直接返回 nil。
//
// 对应 Python: AsyncCallbackFramework.trigger(event, **kwargs) → List[Any]
func (fw *CallbackFramework) TriggerGlobalAgent(ctx context.Context, data *GlobalAgentEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.globalAgentCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn GlobalAgentCallbackFunc, ctx context.Context, data *GlobalAgentEventData) (any, error) {
			return fn(ctx, data), nil
		},
	)
	return results
}

// OnPerAgent 注册实例级 PerAgent 回调。
//
// event 格式为 "{agentID}_{event}"（如 "agent1_before_model_call"），由 AgentCallbackManager 构造。
// 同一事件可注册多个回调，按优先级排序执行。
func (fw *CallbackFramework) OnPerAgent(event string, fn PerAgentCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[PerAgentCallbackFunc]{
		Callback:     fn,
		Priority:     cfg.Priority,
		Once:         cfg.Once,
		Enabled:      true,
		Namespace:    cfg.Namespace,
		Tags:         cfg.Tags,
		MaxRetries:   cfg.MaxRetries,
		RetryDelay:   cfg.RetryDelay,
		Timeout:      cfg.Timeout,
		CreatedAt:    float64(time.Now().UnixNano()) / 1e9,
		CallbackType: cfg.CallbackType,
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.perAgentCallbacks[event] = append(fw.perAgentCallbacks[event], info)
	sortCallbacks(fw.perAgentCallbacks[event])
}

// OffPerAgent 注销指定事件上的单个 PerAgent 回调（按 info.Callback 的指针匹配）。
func (fw *CallbackFramework) OffPerAgent(event string, fn PerAgentCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.perAgentCallbacks[event]
	if !ok {
		return
	}

	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.perAgentCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// OffAllPerAgent 清除指定事件上的所有 PerAgent 回调。
func (fw *CallbackFramework) OffAllPerAgent(event string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	delete(fw.perAgentCallbacks, event)
}

// TriggerPerAgent 触发指定事件的所有 PerAgent 回调，按优先级顺序执行。
//
// agentCallbackContext 实际类型为 *rail.AgentCallbackContext。
// 任一回调返回 error 时停止后续执行并返回该 error。
func (fw *CallbackFramework) TriggerPerAgent(ctx context.Context, event string, agentCallbackContext any) error {
	if ctx == nil {
		return nil
	}

	_, err := triggerCallbacks(fw.perAgentCallbacks, event, agentCallbackContext, ctx, &fw.mu,
		strategyAbortOnError,
		func(fn PerAgentCallbackFunc, ctx context.Context, data any) (any, error) {
			return nil, fn(ctx, data)
		},
	)
	return err
}

// HasPerAgentHooks 检查指定事件是否有已注册的 PerAgent 回调。
func (fw *CallbackFramework) HasPerAgentHooks(event string) bool {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	callbacks, ok := fw.perAgentCallbacks[event]
	return ok && len(callbacks) > 0
}

// RegisterLLMTransformIO 注册 LLM 层 IO 变换回调。
//
// 对齐 Python: CallbackFramework.transform_io 注册机制。
// inputFn 在 emit_before 前对输入做变换，outputFn 在 emit_after 前对输出做变换。
// 同时用 inputEvent 和 outputEvent 作为 key 注册，确保通过任一事件都能查到 entry。
func (fw *CallbackFramework) RegisterLLMTransformIO(
	inputEvent LLMCallEventType,
	outputEvent LLMCallEventType,
	inputFn TransformLLMIOInputFunc,
	outputFn TransformLLMIOOutputFunc,
) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	entry := &llmTransformIOEntry{
		inputFn:  inputFn,
		outputFn: outputFn,
	}
	fw.llmTransformIO[inputEvent] = entry
	fw.llmTransformIO[outputEvent] = entry
}

// TransformLLMIOInput 应用 LLM 层输入变换。
//
// 如果没有注册变换回调，返回原始输入（透传）。
// 对齐 Python: transform_io 的 input_fn 在 emit_before 前执行。
func (fw *CallbackFramework) TransformLLMIOInput(ctx context.Context, event LLMCallEventType, input any) any {
	fw.mu.RLock()
	entry, ok := fw.llmTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.inputFn == nil {
		return input
	}
	return entry.inputFn(ctx, event, input)
}

// TransformLLMIOOutput 应用 LLM 层输出变换。
//
// 如果没有注册变换回调，返回原始输出（透传）。
// 对齐 Python: transform_io 的 output_fn 在 emit_after 前执行。
func (fw *CallbackFramework) TransformLLMIOOutput(ctx context.Context, event LLMCallEventType, output any) any {
	fw.mu.RLock()
	entry, ok := fw.llmTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.outputFn == nil {
		return output
	}
	return entry.outputFn(ctx, event, output)
}

// RegisterAgentTransformIO 注册 Agent 层 IO 变换回调。
//
// 对齐 Python: CallbackFramework.transform_io 注册机制。
// 同时用 inputEvent 和 outputEvent 作为 key 注册，确保通过任一事件都能查到 entry。
func (fw *CallbackFramework) RegisterAgentTransformIO(
	inputEvent GlobalAgentEventType,
	outputEvent GlobalAgentEventType,
	inputFn TransformAgentIOInputFunc,
	outputFn TransformAgentIOOutputFunc,
) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	entry := &agentTransformIOEntry{
		inputFn:  inputFn,
		outputFn: outputFn,
	}
	fw.agentTransformIO[inputEvent] = entry
	fw.agentTransformIO[outputEvent] = entry
}

// TransformAgentIOInput 应用 Agent 层输入变换。
//
// 如果没有注册变换回调，返回原始输入（透传）。
func (fw *CallbackFramework) TransformAgentIOInput(ctx context.Context, event GlobalAgentEventType, input any) any {
	fw.mu.RLock()
	entry, ok := fw.agentTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.inputFn == nil {
		return input
	}
	return entry.inputFn(ctx, event, input)
}

// TransformAgentIOOutput 应用 Agent 层输出变换。
//
// 如果没有注册变换回调，返回原始输出（透传）。
func (fw *CallbackFramework) TransformAgentIOOutput(ctx context.Context, event GlobalAgentEventType, output any) any {
	fw.mu.RLock()
	entry, ok := fw.agentTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.outputFn == nil {
		return output
	}
	return entry.outputFn(ctx, event, output)
}

// RegisterToolTransformIO 注册 Tool 层 IO 变换回调。
//
// 对齐 Python: CallbackFramework.transform_io 注册机制。
// inputFn 在 emit_before 前对输入做变换，outputFn 在 emit_after 前对输出做变换。
// 同时用 inputEvent 和 outputEvent 作为 key 注册，确保通过任一事件都能查到 entry。
func (fw *CallbackFramework) RegisterToolTransformIO(
	inputEvent ToolCallEventType,
	outputEvent ToolCallEventType,
	inputFn TransformToolIOInputFunc,
	outputFn TransformToolIOOutputFunc,
) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	entry := &toolTransformIOEntry{
		inputFn:  inputFn,
		outputFn: outputFn,
	}
	fw.toolTransformIO[inputEvent] = entry
	fw.toolTransformIO[outputEvent] = entry
}

// TransformToolIOInput 应用 Tool 层输入变换。
//
// 如果没有注册变换回调，返回原始输入（透传）。
// 对齐 Python: transform_io 的 input_fn 在 emit_before 前执行。
func (fw *CallbackFramework) TransformToolIOInput(ctx context.Context, event ToolCallEventType, input map[string]any) map[string]any {
	fw.mu.RLock()
	entry, ok := fw.toolTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.inputFn == nil {
		return input
	}
	return entry.inputFn(ctx, event, input)
}

// TransformToolIOOutput 应用 Tool 层输出变换。
//
// 如果没有注册变换回调，返回原始输出（透传）。
// 对齐 Python: transform_io 的 output_fn 在 emit_after 前执行。
func (fw *CallbackFramework) TransformToolIOOutput(ctx context.Context, event ToolCallEventType, output map[string]any) map[string]any {
	fw.mu.RLock()
	entry, ok := fw.toolTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.outputFn == nil {
		return output
	}
	return entry.outputFn(ctx, event, output)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// triggerCallbacks 泛型触发核心逻辑（包级独立函数，因 Go 不支持方法类型参数）。
//
// 参数：
//   - callbacksMap: 事件→回调列表的映射
//   - event: 事件键（枚举或字符串）
//   - data: 事件数据
//   - ctx: 上下文
//   - mu: 并发读写锁
//   - strategy: 执行策略（strategyCollect 或 strategyAbortOnError）
//   - execute: 执行单个回调的闭包，返回 (result, error)
func triggerCallbacks[F any, E comparable, D any](
	callbacksMap map[E][]*CallbackInfo[F],
	event E,
	data D,
	ctx context.Context,
	mu *sync.RWMutex,
	strategy triggerStrategy,
	execute func(F, context.Context, D) (any, error),
) ([]any, error) {
	if ctx == nil {
		return nil, nil
	}

	mu.RLock()
	callbacks := callbacksMap[event]
	mu.RUnlock()

	// ⤵️ 回填：BEFORE 钩子执行（对应 Python: _execute_hooks(event, HookType.BEFORE)）

	var results []any
	for _, info := range callbacks {
		if !info.Enabled {
			continue
		}
		if info.CallbackType == "transform" {
			continue
		}

		// ⤵️ 回填：过滤器检查（对应 Python: _apply_filters）
		// ⤵️ 回填：熔断器检查（对应 Python: _circuit_breakers）
		// ⤵️ 回填：回调级超时控制（对应 Python: trigger_with_timeout）
		// ⤵️ 回填：回调级重试（对应 Python: max_retries/retry_delay）

		result, err := execute(info.Callback, ctx, data)

		if err != nil {
			// ⤵️ 回填：ERROR 钩子执行（对应 Python: _execute_hooks(event, HookType.ERROR)）
			// ⤵️ 回填：指标记录（is_error=True）
			if strategy == strategyAbortOnError {
				return nil, err
			}
			continue
		}

		// ⤵️ 回填：指标记录（is_error=False）
		results = append(results, result)

		if info.Once {
			info.Enabled = false
		}
	}

	// ⤵️ 回填：AFTER 钩子执行（对应 Python: _execute_hooks(event, HookType.AFTER)）

	return results, nil
}
