package callback

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// ──────────────────────────── 结构体 ────────────────────────────

type eventHistoryEntry struct {
	// Event 事件名
	Event string
	// Time 发生时间
	Time time.Time
	// Data 事件数据
	Data any
}

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
	// workflowCallbacks 工作流回调函数注册表
	workflowCallbacks map[WorkflowEventType][]*CallbackInfo[WorkflowCallbackFunc]
	// agentTeamCallbacks AgentTeam 回调函数注册表
	agentTeamCallbacks map[AgentTeamEventType][]*CallbackInfo[AgentTeamCallbackFunc]
	// retrievalCallbacks 检索回调函数注册表
	retrievalCallbacks map[RetrievalEventType][]*CallbackInfo[RetrievalCallbackFunc]
	// memoryCallbacks 记忆回调函数注册表
	memoryCallbacks map[MemoryEventType][]*CallbackInfo[MemoryCallbackFunc]
	// taskManagerCallbacks 任务管理回调函数注册表
	taskManagerCallbacks map[TaskManagerEventType][]*CallbackInfo[TaskManagerCallbackFunc]
	// hooks 生命周期钩子（事件名 → HookType → 钩子函数列表）
	hooks map[string]map[HookType][]HookFunc
	// filters 事件级过滤器（事件名 → 过滤器列表）
	filters map[string][]EventFilter
	// globalFilters 全局过滤器
	globalFilters []EventFilter
	// callbackFilters 回调级过滤器（回调函数指针 → 过滤器列表）
	callbackFilters map[any][]EventFilter
	// metrics 执行指标（"{event}:{callbackName}" → CallbackMetrics）
	metrics map[string]*CallbackMetrics
	// circuitBreakers 熔断器（"{event}:{callbackName}" → *CircuitBreakerFilter）
	circuitBreakers map[string]*CircuitBreakerFilter
	// chains 回调链（事件名 → CallbackChain）
	chains map[string]*CallbackChain
	// enableEventHistory 是否启用事件历史
	enableEventHistory bool
	// eventHistory 事件历史记录（环形缓冲区，最大 1000 条）
	eventHistory []eventHistoryEntry
	// enableMetrics 是否启用指标
	enableMetrics bool
}

type llmTransformIOEntry struct {
	// inputFn 输入变换函数
	inputFn TransformLLMIOInputFunc
	// outputFn 输出变换函数
	outputFn TransformLLMIOOutputFunc
}

type agentTransformIOEntry struct {
	// inputFn 输入变换函数
	inputFn TransformAgentIOInputFunc
	// outputFn 输出变换函数
	outputFn TransformAgentIOOutputFunc
}

type toolTransformIOEntry struct {
	// inputFn 输入变换函数
	inputFn TransformToolIOInputFunc
	// outputFn 输出变换函数
	outputFn TransformToolIOOutputFunc
}

// HookFunc 钩子函数类型。
type HookFunc func(ctx context.Context, event string, data any)

// LLMCallbackFunc LLM 回调函数类型。
type LLMCallbackFunc func(ctx context.Context, data *LLMCallEventData) any

// ToolCallbackFunc 工具回调函数类型。
type ToolCallbackFunc func(ctx context.Context, data *ToolCallEventData) any

// SessionCallbackFunc 会话回调函数类型。
type SessionCallbackFunc func(ctx context.Context, data *SessionCallEventData) any

// CustomCallbackFunc 自定义回调函数类型。
type CustomCallbackFunc func(ctx context.Context, data map[string]any) any

// ContextCallbackFunc 上下文回调函数类型。
type ContextCallbackFunc func(ctx context.Context, data *ContextCallEventData) any

// ──────────────────────────── 枚举 ────────────────────────────

type triggerStrategy int

// ──────────────────────────── 常量 ────────────────────────────

const (
	// strategyCollect 收集所有返回值，不中断（观测型）
	strategyCollect triggerStrategy = iota
	// strategyAbortOnError 遇 error 中断（控制型）
	strategyAbortOnError
)

const (
	// maxEventHistory 事件历史记录最大条数
	maxEventHistory = 1000
)

// ──────────────────────────── 导出函数 ────────────────────────────

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
		workflowCallbacks:    make(map[WorkflowEventType][]*CallbackInfo[WorkflowCallbackFunc]),
		agentTeamCallbacks:   make(map[AgentTeamEventType][]*CallbackInfo[AgentTeamCallbackFunc]),
		retrievalCallbacks:   make(map[RetrievalEventType][]*CallbackInfo[RetrievalCallbackFunc]),
		memoryCallbacks:      make(map[MemoryEventType][]*CallbackInfo[MemoryCallbackFunc]),
		taskManagerCallbacks: make(map[TaskManagerEventType][]*CallbackInfo[TaskManagerCallbackFunc]),
		hooks:                make(map[string]map[HookType][]HookFunc),
		filters:              make(map[string][]EventFilter),
		globalFilters:        make([]EventFilter, 0),
		callbackFilters:      make(map[any][]EventFilter),
		metrics:              make(map[string]*CallbackMetrics),
		circuitBreakers:      make(map[string]*CircuitBreakerFilter),
		chains:               make(map[string]*CallbackChain),
		eventHistory:         make([]eventHistoryEntry, 0, maxEventHistory),
	}
	return fw
}

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

func (fw *CallbackFramework) TriggerLLM(ctx context.Context, data *LLMCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.llmCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn LLMCallbackFunc, ctx context.Context, data *LLMCallEventData) (any, error) {
			return fn(ctx, data), nil
		},
		fw,
	)
	return results
}

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

func (fw *CallbackFramework) TriggerTool(ctx context.Context, data *ToolCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.toolCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn ToolCallbackFunc, ctx context.Context, data *ToolCallEventData) (any, error) {
			return fn(ctx, data), nil
		},
		fw,
	)
	return results
}

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

func (fw *CallbackFramework) TriggerSession(ctx context.Context, data *SessionCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.sessionCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn SessionCallbackFunc, ctx context.Context, data *SessionCallEventData) (any, error) {
			return fn(ctx, data), nil
		},
		fw,
	)
	return results
}

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

func (fw *CallbackFramework) OffAllCustom(event string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	// 清理回调
	delete(fw.customCallbacks, event)
	// 清理事件级过滤器
	delete(fw.filters, event)
	// 清理链
	delete(fw.chains, event)
	// 清理钩子
	delete(fw.hooks, event)
	// 清理熔断器（按 event: 前缀匹配）
	for key := range fw.circuitBreakers {
		if strings.HasPrefix(key, event+":") {
			delete(fw.circuitBreakers, key)
		}
	}
}

func (fw *CallbackFramework) TriggerCustom(ctx context.Context, event string, data map[string]any) []any {
	if ctx == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.customCallbacks, event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn CustomCallbackFunc, ctx context.Context, data map[string]any) (any, error) {
			return fn(ctx, data), nil
		},
		fw,
	)
	return results
}

func (fw *CallbackFramework) GetCallbacksForTest(event LLMCallEventType) []*CallbackInfo[LLMCallbackFunc] {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	return fw.llmCallbacks[event]
}

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

func (fw *CallbackFramework) TriggerContext(ctx context.Context, data *ContextCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.contextCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn ContextCallbackFunc, ctx context.Context, data *ContextCallEventData) (any, error) {
			return fn(ctx, data), nil
		},
		fw,
	)
	return results
}

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

func (fw *CallbackFramework) TriggerGlobalAgent(ctx context.Context, data *GlobalAgentEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.globalAgentCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn GlobalAgentCallbackFunc, ctx context.Context, data *GlobalAgentEventData) (any, error) {
			return fn(ctx, data), nil
		},
		fw,
	)
	return results
}

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

func (fw *CallbackFramework) OffAllPerAgent(event string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	// 清理回调
	delete(fw.perAgentCallbacks, event)
	// 清理事件级过滤器
	delete(fw.filters, event)
	// 清理链
	delete(fw.chains, event)
	// 清理钩子
	delete(fw.hooks, event)
	// 清理熔断器（按 event: 前缀匹配）
	for key := range fw.circuitBreakers {
		if strings.HasPrefix(key, event+":") {
			delete(fw.circuitBreakers, key)
		}
	}
}

func (fw *CallbackFramework) TriggerPerAgent(ctx context.Context, event string, agentCallbackContext any) error {
	if ctx == nil {
		return nil
	}

	_, err := triggerCallbacks(fw.perAgentCallbacks, event, agentCallbackContext, ctx, &fw.mu,
		strategyAbortOnError,
		func(fn PerAgentCallbackFunc, ctx context.Context, data any) (any, error) {
			return nil, fn(ctx, data)
		},
		fw,
	)
	return err
}

func (fw *CallbackFramework) HasPerAgentHooks(event string) bool {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	callbacks, ok := fw.perAgentCallbacks[event]
	return ok && len(callbacks) > 0
}

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

func (fw *CallbackFramework) TransformLLMIOInput(ctx context.Context, event LLMCallEventType, input any) any {
	fw.mu.RLock()
	entry, ok := fw.llmTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.inputFn == nil {
		return input
	}
	return entry.inputFn(ctx, event, input)
}

func (fw *CallbackFramework) TransformLLMIOOutput(ctx context.Context, event LLMCallEventType, output any) any {
	fw.mu.RLock()
	entry, ok := fw.llmTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.outputFn == nil {
		return output
	}
	return entry.outputFn(ctx, event, output)
}

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

func (fw *CallbackFramework) TransformAgentIOInput(ctx context.Context, event GlobalAgentEventType, input any) any {
	fw.mu.RLock()
	entry, ok := fw.agentTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.inputFn == nil {
		return input
	}
	return entry.inputFn(ctx, event, input)
}

func (fw *CallbackFramework) TransformAgentIOOutput(ctx context.Context, event GlobalAgentEventType, output any) any {
	fw.mu.RLock()
	entry, ok := fw.agentTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.outputFn == nil {
		return output
	}
	return entry.outputFn(ctx, event, output)
}

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

func (fw *CallbackFramework) TransformToolIOInput(ctx context.Context, event ToolCallEventType, input map[string]any) map[string]any {
	fw.mu.RLock()
	entry, ok := fw.toolTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.inputFn == nil {
		return input
	}
	return entry.inputFn(ctx, event, input)
}

func (fw *CallbackFramework) TransformToolIOOutput(ctx context.Context, event ToolCallEventType, output map[string]any) map[string]any {
	fw.mu.RLock()
	entry, ok := fw.toolTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.outputFn == nil {
		return output
	}
	return entry.outputFn(ctx, event, output)
}

func (fw *CallbackFramework) OnWorkflow(event WorkflowEventType, fn WorkflowCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[WorkflowCallbackFunc]{
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
	fw.workflowCallbacks[event] = append(fw.workflowCallbacks[event], info)
	sortCallbacks(fw.workflowCallbacks[event])
}

func (fw *CallbackFramework) OffWorkflow(event WorkflowEventType, fn WorkflowCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.workflowCallbacks[event]
	if !ok {
		return
	}

	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.workflowCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

func (fw *CallbackFramework) TriggerWorkflow(ctx context.Context, data *WorkflowEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.workflowCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn WorkflowCallbackFunc, ctx context.Context, data *WorkflowEventData) (any, error) {
			return fn(ctx, data), nil
		},
		fw,
	)
	return results
}

func (fw *CallbackFramework) OnAgentTeam(event AgentTeamEventType, fn AgentTeamCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[AgentTeamCallbackFunc]{
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
	fw.agentTeamCallbacks[event] = append(fw.agentTeamCallbacks[event], info)
	sortCallbacks(fw.agentTeamCallbacks[event])
}

func (fw *CallbackFramework) OffAgentTeam(event AgentTeamEventType, fn AgentTeamCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.agentTeamCallbacks[event]
	if !ok {
		return
	}

	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.agentTeamCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

func (fw *CallbackFramework) TriggerAgentTeam(ctx context.Context, data *AgentTeamEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.agentTeamCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn AgentTeamCallbackFunc, ctx context.Context, data *AgentTeamEventData) (any, error) {
			return fn(ctx, data), nil
		},
		fw,
	)
	return results
}

func (fw *CallbackFramework) OnRetrieval(event RetrievalEventType, fn RetrievalCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[RetrievalCallbackFunc]{
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
	fw.retrievalCallbacks[event] = append(fw.retrievalCallbacks[event], info)
	sortCallbacks(fw.retrievalCallbacks[event])
}

func (fw *CallbackFramework) OffRetrieval(event RetrievalEventType, fn RetrievalCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.retrievalCallbacks[event]
	if !ok {
		return
	}

	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.retrievalCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

func (fw *CallbackFramework) TriggerRetrieval(ctx context.Context, data *RetrievalEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.retrievalCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn RetrievalCallbackFunc, ctx context.Context, data *RetrievalEventData) (any, error) {
			return fn(ctx, data), nil
		},
		fw,
	)
	return results
}

func (fw *CallbackFramework) OnMemory(event MemoryEventType, fn MemoryCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[MemoryCallbackFunc]{
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
	fw.memoryCallbacks[event] = append(fw.memoryCallbacks[event], info)
	sortCallbacks(fw.memoryCallbacks[event])
}

func (fw *CallbackFramework) OffMemory(event MemoryEventType, fn MemoryCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.memoryCallbacks[event]
	if !ok {
		return
	}

	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.memoryCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

func (fw *CallbackFramework) TriggerMemory(ctx context.Context, data *MemoryEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.memoryCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn MemoryCallbackFunc, ctx context.Context, data *MemoryEventData) (any, error) {
			return fn(ctx, data), nil
		},
		fw,
	)
	return results
}

func (fw *CallbackFramework) OnTaskManager(event TaskManagerEventType, fn TaskManagerCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[TaskManagerCallbackFunc]{
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
	fw.taskManagerCallbacks[event] = append(fw.taskManagerCallbacks[event], info)
	sortCallbacks(fw.taskManagerCallbacks[event])
}

func (fw *CallbackFramework) OffTaskManager(event TaskManagerEventType, fn TaskManagerCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.taskManagerCallbacks[event]
	if !ok {
		return
	}

	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.taskManagerCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

func (fw *CallbackFramework) TriggerTaskManager(ctx context.Context, data *TaskManagerEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	results, _ := triggerCallbacks(fw.taskManagerCallbacks, data.Event, data, ctx, &fw.mu,
		strategyCollect,
		func(fn TaskManagerCallbackFunc, ctx context.Context, data *TaskManagerEventData) (any, error) {
			return fn(ctx, data), nil
		},
		fw,
	)
	return results
}

func (fw *CallbackFramework) AddFilter(event string, filter EventFilter) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.filters[event] = append(fw.filters[event], filter)
}

func (fw *CallbackFramework) AddGlobalFilter(filter EventFilter) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.globalFilters = append(fw.globalFilters, filter)
}

func (fw *CallbackFramework) AddCircuitBreaker(event, callbackName string, failureThreshold int, timeout float64) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	key := event + ":" + callbackName
	breaker := NewCircuitBreakerFilter(failureThreshold, timeout, "")
	fw.circuitBreakers[key] = breaker
	// 与 Python 对齐：熔断器同时注册为事件过滤器
	fw.filters[event] = append(fw.filters[event], breaker)
}

func (fw *CallbackFramework) AddHook(event string, hookType HookType, hook HookFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if fw.hooks[event] == nil {
		fw.hooks[event] = make(map[HookType][]HookFunc)
	}
	fw.hooks[event][hookType] = append(fw.hooks[event][hookType], hook)
}

func (fw *CallbackFramework) TriggerChain(ctx context.Context, event string, data any) *ChainResult {
	fw.mu.RLock()
	chain, ok := fw.chains[event]
	fw.mu.RUnlock()
	if !ok {
		return &ChainResult{Action: ChainActionContinue, Result: data}
	}
	cctx := &ChainContext{
		Event:       event,
		InitialData: data,
		StartTime:   time.Now(),
		Metadata:    make(map[string]any),
	}
	return chain.Execute(ctx, cctx)
}

func (fw *CallbackFramework) TriggerParallel(ctx context.Context, event string, data map[string]any) []any {
	fw.mu.RLock()
	callbacks := fw.customCallbacks[event]
	fw.mu.RUnlock()

	if len(callbacks) == 0 {
		return nil
	}

	// BEFORE 钩子执行
	fw.executeHooks(ctx, event, HookTypeBefore, data)

	eg, egCtx := errgroup.WithContext(ctx)
	var resultsMu sync.Mutex
	var results []any

	for _, info := range callbacks {
		if !info.Enabled || info.CallbackType == "transform" {
			continue
		}
		cbInfo := info // 捕获循环变量

		eg.Go(func() error {
			// 检查 errgroup 上下文是否已取消
			if egCtx.Err() != nil {
				return egCtx.Err()
			}

			// 过滤器检查（全局 → 事件 → 回调级三级管线）
			filterResult := fw.applyFilters(egCtx, event, cbInfo, data)
			switch filterResult.Action {
			case FilterActionStop:
				return nil // STOP：跳过此回调，不中断其他并发回调
			case FilterActionSkip:
				return nil
			}

			// 熔断器检查
			cbKey := event + ":" + getCallbackName(cbInfo)
			if cb, ok := fw.circuitBreakers[cbKey]; ok && cb.IsOpen(cbKey) {
				return nil // 熔断器打开，跳过此回调
			}

			// 执行回调
			startTime := time.Now()
			result, err := cbInfo.Callback(egCtx, data), error(nil)
			executionTime := time.Since(startTime).Seconds()

			if err != nil {
				// ERROR 钩子执行
				fw.executeHooks(egCtx, event, HookTypeError, err)
				if fw.enableMetrics {
					fw.updateMetrics(cbKey, executionTime, true)
				}
				return nil // 单个回调错误不中断并发执行
			}

			// 指标记录
			if fw.enableMetrics {
				fw.updateMetrics(cbKey, executionTime, false)
			}

			resultsMu.Lock()
			results = append(results, result)
			resultsMu.Unlock()

			return nil
		})
	}

	// 等待所有并发回调完成（忽略 errgroup 中的错误，因为上面已 return nil）
	_ = eg.Wait()

	// AFTER 钩子执行
	fw.executeHooks(ctx, event, HookTypeAfter, data)

	return results
}

func (fw *CallbackFramework) TriggerUntil(ctx context.Context, event string, condition func(any) bool, data map[string]any) any {
	fw.mu.RLock()
	callbacks := fw.customCallbacks[event]
	fw.mu.RUnlock()

	if len(callbacks) == 0 {
		return nil
	}

	for _, info := range callbacks {
		if !info.Enabled || info.CallbackType == "transform" {
			continue
		}

		// 过滤器检查（全局 → 事件 → 回调级三级管线）
		filterResult := fw.applyFilters(ctx, event, info, data)
		switch filterResult.Action {
		case FilterActionStop:
			return nil // STOP：终止整个循环
		case FilterActionSkip:
			continue // SKIP：跳过当前回调
		case FilterActionModify:
			// MODIFY：使用修改后数据
			if modified, ok := filterResult.ModifiedData.(map[string]any); ok {
				data = modified
			}
		}

		// 执行回调
		result, err := info.Callback(ctx, data), error(nil)

		if err != nil {
			// ERROR 钩子执行
			fw.executeHooks(ctx, event, HookTypeError, err)
			continue // 异常时继续下一个回调
		}

		// 检查条件
		if condition(result) {
			// 处理 once-only 回调
			if info.Once {
				info.Enabled = false
			}
			return result
		}

		// 条件不满足，处理 once-only 回调
		if info.Once {
			info.Enabled = false
		}
	}

	return nil
}

func (fw *CallbackFramework) TriggerWithTimeout(ctx context.Context, event string, timeout float64, data map[string]any) ([]any, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout*float64(time.Second)))
	defer cancel()

	return triggerCallbacks(fw.customCallbacks, event, data, timeoutCtx, &fw.mu,
		strategyCollect,
		func(fn CustomCallbackFunc, ctx context.Context, data map[string]any) (any, error) {
			return fn(ctx, data), nil
		},
		fw,
	)
}

func (fw *CallbackFramework) GetMetrics(event, callbackName string) *CallbackMetrics {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	key := event + ":" + callbackName
	return fw.metrics[key]
}

func (fw *CallbackFramework) ResetMetrics() {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.metrics = make(map[string]*CallbackMetrics)
}

func (fw *CallbackFramework) GetSlowCallbacks(threshold float64) map[string]*CallbackMetrics {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	result := make(map[string]*CallbackMetrics)
	for key, m := range fw.metrics {
		if m.AvgTime() > threshold {
			result[key] = m
		}
	}
	return result
}

func (fw *CallbackFramework) EnableMetrics(enabled bool) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.enableMetrics = enabled
}

func (fw *CallbackFramework) EnableEventHistory(enabled bool) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.enableEventHistory = enabled
}

func (fw *CallbackFramework) GetEventHistory(event string, since time.Time) []eventHistoryEntry {
	fw.mu.RLock()
	defer fw.mu.RUnlock()

	result := make([]eventHistoryEntry, 0, len(fw.eventHistory))
	for _, entry := range fw.eventHistory {
		if event != "" && entry.Event != event {
			continue
		}
		if !since.IsZero() && entry.Time.Before(since) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func (fw *CallbackFramework) ReplayEvents(ctx context.Context, since time.Time) {
	history := fw.GetEventHistory("", since)
	for _, entry := range history {
		data, ok := entry.Data.(map[string]any)
		if !ok {
			data = map[string]any{"replay_data": entry.Data}
		}
		fw.TriggerCustom(ctx, entry.Event, data)
	}
}

func (fw *CallbackFramework) GetStatistics() map[string]any {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	return map[string]any{
		"llm_callbacks":          len(fw.llmCallbacks),
		"tool_callbacks":         len(fw.toolCallbacks),
		"session_callbacks":      len(fw.sessionCallbacks),
		"custom_callbacks":       len(fw.customCallbacks),
		"context_callbacks":      len(fw.contextCallbacks),
		"agent_callbacks":        len(fw.globalAgentCallbacks),
		"per_agent_callbacks":    len(fw.perAgentCallbacks),
		"workflow_callbacks":     len(fw.workflowCallbacks),
		"agent_team_callbacks":   len(fw.agentTeamCallbacks),
		"retrieval_callbacks":    len(fw.retrievalCallbacks),
		"memory_callbacks":       len(fw.memoryCallbacks),
		"task_manager_callbacks": len(fw.taskManagerCallbacks),
		"hooks":                  len(fw.hooks),
		"filters":                len(fw.filters),
		"global_filters":         len(fw.globalFilters),
		"callback_filters":       len(fw.callbackFilters),
		"metrics":                len(fw.metrics),
		"circuit_breakers":       len(fw.circuitBreakers),
		"chains":                 len(fw.chains),
		"enable_event_history":   fw.enableEventHistory,
		"event_history_count":    len(fw.eventHistory),
		"enable_metrics":         fw.enableMetrics,
	}
}

func (fw *CallbackFramework) TriggerDelayed(ctx context.Context, event string, data map[string]any, delay time.Duration) {
	go func() {
		time.Sleep(delay)
		fw.TriggerCustom(ctx, event, data)
	}()
}

func (fw *CallbackFramework) UnregisterNamespace(namespace string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	prefix := namespace + ":"
	// 遍历所有回调 map，删除命名空间匹配的条目
	for k, v := range fw.customCallbacks {
		if strings.HasPrefix(string(k), prefix) {
			delete(fw.customCallbacks, k)
		}
		_ = v // 避免未使用变量警告
	}
	for k, v := range fw.perAgentCallbacks {
		if strings.HasPrefix(string(k), prefix) {
			delete(fw.perAgentCallbacks, k)
		}
		_ = v
	}
	// 清理匹配的过滤器、链、钩子、熔断器
	for k := range fw.filters {
		if strings.HasPrefix(k, prefix) {
			delete(fw.filters, k)
		}
	}
	for k := range fw.chains {
		if strings.HasPrefix(k, prefix) {
			delete(fw.chains, k)
		}
	}
	for k := range fw.hooks {
		if strings.HasPrefix(k, prefix) {
			delete(fw.hooks, k)
		}
	}
	for k := range fw.circuitBreakers {
		if strings.HasPrefix(k, prefix) {
			delete(fw.circuitBreakers, k)
		}
	}
}

func (fw *CallbackFramework) UnregisterByTags(event string, tags []string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		tagSet[t] = true
	}
	// 从自定义回调中过滤
	if callbacks, ok := fw.customCallbacks[event]; ok {
		filtered := make([]*CallbackInfo[CustomCallbackFunc], 0, len(callbacks))
		for _, info := range callbacks {
			hasTag := false
			for _, t := range info.Tags {
				if tagSet[t] {
					hasTag = true
					break
				}
			}
			if !hasTag {
				filtered = append(filtered, info)
			}
		}
		fw.customCallbacks[event] = filtered
	}
}

func (fw *CallbackFramework) ListEvents(namespace string) []string {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	eventSet := make(map[string]bool)
	for k := range fw.customCallbacks {
		eventSet[k] = true
	}
	for k := range fw.perAgentCallbacks {
		eventSet[k] = true
	}
	result := make([]string, 0, len(eventSet))
	for e := range eventSet {
		if namespace != "" && !strings.HasPrefix(e, namespace+":") {
			continue
		}
		result = append(result, e)
	}
	return result
}

func (fw *CallbackFramework) ListCallbacks(event string) []map[string]any {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	result := make([]map[string]any, 0)
	if callbacks, ok := fw.customCallbacks[event]; ok {
		for _, info := range callbacks {
			result = append(result, map[string]any{
				"priority":      info.Priority,
				"once":          info.Once,
				"enabled":       info.Enabled,
				"namespace":     info.Namespace,
				"tags":          info.Tags,
				"callback_type": info.CallbackType,
			})
		}
	}
	return result
}

func (fw *CallbackFramework) OnChain(event string, rollbackHandler, errorHandler any) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if _, exists := fw.chains[event]; !exists {
		fw.chains[event] = NewCallbackChain(event)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func triggerCallbacks[F any, E comparable, D any](
	callbacksMap map[E][]*CallbackInfo[F],
	event E,
	data D,
	ctx context.Context,
	mu *sync.RWMutex,
	strategy triggerStrategy,
	execute func(F, context.Context, D) (any, error),
	fw *CallbackFramework,
) ([]any, error) {
	if ctx == nil {
		return nil, nil
	}

	mu.RLock()
	callbacks := callbacksMap[event]
	mu.RUnlock()

	eventStr := fmt.Sprintf("%v", event)
	hasFramework := fw != nil

	// 记录事件历史（与 Python 对齐）
	if hasFramework {
		fw.mu.Lock()
		if fw.enableEventHistory {
			entry := eventHistoryEntry{
				Event: eventStr,
				Time:  time.Now(),
				Data:  data,
			}
			fw.eventHistory = append(fw.eventHistory, entry)
			// 环形缓冲区：超过上限时截断最旧的记录
			if len(fw.eventHistory) > maxEventHistory {
				fw.eventHistory = fw.eventHistory[len(fw.eventHistory)-maxEventHistory:]
			}
		}
		fw.mu.Unlock()
	}

	// BEFORE 钩子执行
	if hasFramework {
		fw.executeHooks(ctx, eventStr, HookTypeBefore, data)
	}

	var results []any
	for _, info := range callbacks {
		if !info.Enabled {
			continue
		}
		if info.CallbackType == "transform" {
			continue
		}

		// 当前回调使用的执行数据（MODIFY 可能修改）
		execData := data

		// 过滤器检查（全局 → 事件 → 回调级三级管线）
		if hasFramework {
			filterResult := fw.applyFilters(ctx, eventStr, info, data)
			switch filterResult.Action {
			case FilterActionStop:
				return results, nil
			case FilterActionSkip:
				continue
			case FilterActionModify:
				// MODIFY：用修改后数据替换执行参数
				if modified, ok := filterResult.ModifiedData.(D); ok {
					execData = modified
				}
			}
		}

		// 熔断器检查
		cbKey := eventStr + ":" + getCallbackName(info)
		if hasFramework {
			if cb, ok := fw.circuitBreakers[cbKey]; ok && cb.IsOpen(cbKey) {
				continue // 熔断器打开，跳过此回调
			}
		}

		// 回调级重试 + 超时
		maxRetries := info.MaxRetries
		if maxRetries < 1 {
			maxRetries = 0
		}

		var result any
		var err error
		for retry := 0; retry <= maxRetries; retry++ {
			// 超时控制
			executeCtx := ctx
			var cancel context.CancelFunc
			if info.Timeout > 0 {
				executeCtx, cancel = context.WithTimeout(ctx, time.Duration(info.Timeout*float64(time.Second)))
			}

			startTime := time.Now()
			result, err = execute(info.Callback, executeCtx, execData)
			executionTime := time.Since(startTime).Seconds()

			if cancel != nil {
				cancel()
			}

			if err == nil {
				// 指标记录（is_error=False）
				if hasFramework && fw.enableMetrics {
					fw.updateMetrics(cbKey, executionTime, false)
				}
				// 熔断器记录成功
				if hasFramework {
					if cb, ok := fw.circuitBreakers[cbKey]; ok {
						parts := splitCircuitBreakerKey(cbKey)
						cb.RecordSuccess(parts[0], parts[1])
					}
				}
				break
			}

			// AbortError 检测
			var abortErr *AbortError
			if errors.As(err, &abortErr) {
				// ERROR 钩子执行（传入 abortErr.Cause ?? abortErr）
				if hasFramework {
					hookErr := error(abortErr)
					if abortErr.Cause != nil {
						hookErr = abortErr.Cause
					}
					fw.executeHooks(ctx, eventStr, HookTypeError, hookErr)
				}
				// 指标记录（is_error=True）
				if hasFramework && fw.enableMetrics {
					fw.updateMetrics(cbKey, executionTime, true)
				}
				// 熔断器记录失败
				if hasFramework {
					if cb, ok := fw.circuitBreakers[cbKey]; ok {
						parts := splitCircuitBreakerKey(cbKey)
						cb.RecordFailure(parts[0], parts[1])
					}
				}
				// AbortError 传播逻辑
				if abortErr.Cause != nil {
					return nil, abortErr.Cause
				}
				return nil, abortErr
			}

			// 普通错误处理
			// ERROR 钩子执行
			if hasFramework {
				fw.executeHooks(ctx, eventStr, HookTypeError, err)
			}
			// 指标记录（is_error=True）
			if hasFramework && fw.enableMetrics {
				fw.updateMetrics(cbKey, executionTime, true)
			}
			// 熔断器记录失败
			if hasFramework {
				if cb, ok := fw.circuitBreakers[cbKey]; ok {
					parts := splitCircuitBreakerKey(cbKey)
					cb.RecordFailure(parts[0], parts[1])
				}
			}

			// 重试判断
			if retry < maxRetries && info.RetryDelay > 0 {
				time.Sleep(time.Duration(info.RetryDelay * float64(time.Second)))
			}
		}

		if err != nil {
			if strategy == strategyAbortOnError {
				return nil, err
			}
			continue
		}

		results = append(results, result)

		if info.Once {
			info.Enabled = false
		}
	}

	// AFTER 钩子执行
	if hasFramework {
		fw.executeHooks(ctx, eventStr, HookTypeAfter, data)
	}

	return results, nil
}

func (fw *CallbackFramework) executeHooks(ctx context.Context, event string, hookType HookType, data any) {
	fw.mu.RLock()
	hookTypes, ok := fw.hooks[event]
	if !ok {
		fw.mu.RUnlock()
		return
	}
	hooks, ok := hookTypes[hookType]
	if !ok {
		fw.mu.RUnlock()
		return
	}
	// 复制一份避免持锁时间过长
	hooksCopy := make([]HookFunc, len(hooks))
	copy(hooksCopy, hooks)
	fw.mu.RUnlock()

	for _, hook := range hooksCopy {
		hook(ctx, event, data)
	}
}

func (fw *CallbackFramework) applyFilters(ctx context.Context, event string, info any, data any) FilterResult {
	callbackName := getCallbackNameFromAny(info)

	// 全局过滤器
	for _, f := range fw.globalFilters {
		result := f.Filter(ctx, event, callbackName, data)
		if result.Action == FilterActionStop || result.Action == FilterActionSkip {
			return result
		}
	}

	// 事件级过滤器
	fw.mu.RLock()
	eventFilters, ok := fw.filters[event]
	fw.mu.RUnlock()
	if ok {
		for _, f := range eventFilters {
			result := f.Filter(ctx, event, callbackName, data)
			if result.Action == FilterActionStop || result.Action == FilterActionSkip {
				return result
			}
		}
	}

	// 回调级过滤器
	fw.mu.RLock()
	cbFilters, ok := fw.callbackFilters[info]
	fw.mu.RUnlock()
	if ok {
		for _, f := range cbFilters {
			result := f.Filter(ctx, event, callbackName, data)
			if result.Action == FilterActionStop || result.Action == FilterActionSkip {
				return result
			}
		}
	}

	return FilterResult{Action: FilterActionContinue}
}

func (fw *CallbackFramework) updateMetrics(key string, executionTime float64, isError bool) {
	fw.mu.Lock()
	m, ok := fw.metrics[key]
	if !ok {
		m = &CallbackMetrics{}
		fw.metrics[key] = m
	}
	fw.mu.Unlock()
	m.Update(executionTime, isError)
}

func getCallbackName[F any](info *CallbackInfo[F]) string {
	return fmt.Sprintf("%T", info.Callback)
}

func getCallbackNameFromAny(info any) string {
	return fmt.Sprintf("%T", info)
}

func splitCircuitBreakerKey(key string) [2]string {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == ':' {
			return [2]string{key[:i], key[i+1:]}
		}
	}
	return [2]string{key, ""}
}
