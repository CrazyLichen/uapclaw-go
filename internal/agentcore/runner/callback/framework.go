package callback

import (
	"context"
	"fmt"
	"sync"
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
	llmCallbacks map[LLMCallEventType][]LLMCallbackFunc
	// toolCallbacks 工具回调函数注册表
	toolCallbacks map[ToolCallEventType][]ToolCallbackFunc
	// sessionCallbacks 会话回调函数注册表
	sessionCallbacks map[SessionCallEventType][]SessionCallbackFunc
	// customCallbacks 自定义事件回调函数注册表
	//
	// 对应 Python: AsyncCallbackFramework._callbacks 中的动态事件名条目。
	// Python 用 session_id + "write_stream" 构造 per-session 事件名，
	// Go 在此 map 中以相同方式存储，实现 per-session 隔离。
	customCallbacks map[string][]CustomCallbackFunc
	// contextCallbacks 上下文事件回调函数注册表
	contextCallbacks map[ContextCallEventType][]ContextCallbackFunc
	// agentCallbacks Agent 回调函数注册表
	agentCallbacks map[AgentCallGlobalEventType][]AgentCallbackFunc
	// llmTransformIO LLM 层 IO 变换回调注册表，键为 inputEvent
	llmTransformIO map[LLMCallEventType]*llmTransformIOEntry
	// agentTransformIO Agent 层 IO 变换回调注册表，键为 inputEvent
	agentTransformIO map[AgentCallGlobalEventType]*agentTransformIOEntry
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
		llmCallbacks:     make(map[LLMCallEventType][]LLMCallbackFunc),
		toolCallbacks:    make(map[ToolCallEventType][]ToolCallbackFunc),
		sessionCallbacks: make(map[SessionCallEventType][]SessionCallbackFunc),
		customCallbacks:  make(map[string][]CustomCallbackFunc),
		contextCallbacks: make(map[ContextCallEventType][]ContextCallbackFunc),
		agentCallbacks:    make(map[AgentCallGlobalEventType][]AgentCallbackFunc),
		llmTransformIO:    make(map[LLMCallEventType]*llmTransformIOEntry),
		agentTransformIO:  make(map[AgentCallGlobalEventType]*agentTransformIOEntry),
		toolTransformIO:    make(map[ToolCallEventType]*toolTransformIOEntry),
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

// TriggerLLM 触发 LLM 事件，按注册顺序调用所有回调，返回所有回调结果。
//
// 若 ctx 为 nil 或 data 为 nil，直接返回 nil。
//
// 对应 Python: AsyncCallbackFramework.trigger(event, *args, **kwargs) → List[Any]
func (fw *CallbackFramework) TriggerLLM(ctx context.Context, data *LLMCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	fw.mu.RLock()
	callbacks := fw.llmCallbacks[data.Event]
	fw.mu.RUnlock()

	results := make([]any, 0, len(callbacks))
	for _, fn := range callbacks {
		result := fn(ctx, data)
		results = append(results, result)
	}
	return results
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

// TriggerTool 触发 Tool 事件，按注册顺序调用所有回调，返回所有回调结果。
func (fw *CallbackFramework) TriggerTool(ctx context.Context, data *ToolCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	fw.mu.RLock()
	callbacks := fw.toolCallbacks[data.Event]
	fw.mu.RUnlock()

	results := make([]any, 0, len(callbacks))
	for _, fn := range callbacks {
		result := fn(ctx, data)
		results = append(results, result)
	}
	return results
}

// OnSession 注册 Session 事件回调函数。
func (fw *CallbackFramework) OnSession(event SessionCallEventType, fn SessionCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.sessionCallbacks[event] = append(fw.sessionCallbacks[event], fn)
}

// OffSession 注销 Session 事件回调函数。
func (fw *CallbackFramework) OffSession(event SessionCallEventType, fn SessionCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.sessionCallbacks[event]
	if !ok {
		return
	}

	for i, cb := range callbacks {
		if fmt.Sprintf("%p", cb) == fmt.Sprintf("%p", fn) {
			fw.sessionCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerSession 触发 Session 事件，按注册顺序调用所有回调，返回所有回调结果。
func (fw *CallbackFramework) TriggerSession(ctx context.Context, data *SessionCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	fw.mu.RLock()
	callbacks := fw.sessionCallbacks[data.Event]
	fw.mu.RUnlock()

	results := make([]any, 0, len(callbacks))
	for _, fn := range callbacks {
		result := fn(ctx, data)
		results = append(results, result)
	}
	return results
}

// OnCustom 注册自定义事件回调函数。
//
// 同一事件可注册多个回调，按注册顺序执行。
// 事件名为自由字符串，不受预定义枚举约束。
//
// 对应 Python: AsyncCallbackFramework.on(event, callback)
func (fw *CallbackFramework) OnCustom(event string, fn CustomCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.customCallbacks[event] = append(fw.customCallbacks[event], fn)
}

// OffCustom 注销自定义事件的单个回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按指针匹配）。
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

	for i, cb := range callbacks {
		if fmt.Sprintf("%p", cb) == fmt.Sprintf("%p", fn) {
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

// TriggerCustom 触发自定义事件，按注册顺序调用所有回调，返回所有回调结果。
//
// 若 ctx 为 nil，直接返回 nil。
// data 通过 map[string]any 传递，对应 Python 的 **kwargs。
//
// 对应 Python: await trigger(event, **kwargs)
func (fw *CallbackFramework) TriggerCustom(ctx context.Context, event string, data map[string]any) []any {
	if ctx == nil {
		return nil
	}

	fw.mu.RLock()
	callbacks := fw.customCallbacks[event]
	fw.mu.RUnlock()

	results := make([]any, 0, len(callbacks))
	for _, fn := range callbacks {
		result := fn(ctx, data)
		results = append(results, result)
	}
	return results
}

// GetCallbacksForTest 返回指定 LLM 事件的回调列表，仅供测试使用。
func (fw *CallbackFramework) GetCallbacksForTest(event LLMCallEventType) []LLMCallbackFunc {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	return fw.llmCallbacks[event]
}

// OnContext 注册上下文事件回调函数。
//
// 同一事件可注册多个回调，按注册顺序执行。
//
// 对应 Python: AsyncCallbackFramework.on(event, callback)
func (fw *CallbackFramework) OnContext(event ContextCallEventType, fn ContextCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.contextCallbacks[event] = append(fw.contextCallbacks[event], fn)
}

// OffContext 注销上下文事件回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按指针匹配）。
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

	for i, cb := range callbacks {
		if fmt.Sprintf("%p", cb) == fmt.Sprintf("%p", fn) {
			fw.contextCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerContext 触发上下文事件，按注册顺序调用所有回调，返回所有回调结果。
//
// 若 ctx 为 nil 或 data 为 nil，直接返回 nil。
//
// 对应 Python: AsyncCallbackFramework.trigger(event, **kwargs) → List[Any]
func (fw *CallbackFramework) TriggerContext(ctx context.Context, data *ContextCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	fw.mu.RLock()
	callbacks := fw.contextCallbacks[data.Event]
	fw.mu.RUnlock()

	results := make([]any, 0, len(callbacks))
	for _, fn := range callbacks {
		result := fn(ctx, data)
		results = append(results, result)
	}
	return results
}

// OnAgent 注册 Agent 事件回调函数。
//
// 同一事件可注册多个回调，按注册顺序执行。
//
// 对应 Python: AsyncCallbackFramework.on(event, callback)
func (fw *CallbackFramework) OnAgent(event AgentCallGlobalEventType, fn AgentCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.agentCallbacks[event] = append(fw.agentCallbacks[event], fn)
}

// OffAgent 注销 Agent 事件回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按指针匹配）。
// 若事件下无匹配回调，不做任何操作。
//
// 对应 Python: AsyncCallbackFramework.unregister(event, callback)
func (fw *CallbackFramework) OffAgent(event AgentCallGlobalEventType, fn AgentCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.agentCallbacks[event]
	if !ok {
		return
	}

	for i, cb := range callbacks {
		if fmt.Sprintf("%p", cb) == fmt.Sprintf("%p", fn) {
			fw.agentCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerAgent 触发 Agent 事件，按注册顺序调用所有回调，返回所有回调结果。
//
// 若 ctx 为 nil 或 data 为 nil，直接返回 nil。
//
// 对应 Python: AsyncCallbackFramework.trigger(event, **kwargs) → List[Any]
func (fw *CallbackFramework) TriggerAgent(ctx context.Context, data *AgentCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	fw.mu.RLock()
	callbacks := fw.agentCallbacks[data.Event]
	fw.mu.RUnlock()

	results := make([]any, 0, len(callbacks))
	for _, fn := range callbacks {
		result := fn(ctx, data)
		results = append(results, result)
	}
	return results
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
	inputEvent AgentCallGlobalEventType,
	outputEvent AgentCallGlobalEventType,
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
func (fw *CallbackFramework) TransformAgentIOInput(ctx context.Context, event AgentCallGlobalEventType, input any) any {
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
func (fw *CallbackFramework) TransformAgentIOOutput(ctx context.Context, event AgentCallGlobalEventType, output any) any {
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
