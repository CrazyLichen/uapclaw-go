package tracer

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TriggerParams 追踪触发参数，对应 Python tracer.trigger() 的 kwargs 聚合。
type TriggerParams struct {
	// Span 追踪跨度（Agent 事件使用 TraceAgentSpan，Workflow 事件为 nil）
	Span *Span
	// Inputs 输入数据
	Inputs any
	// Outputs 输出数据
	Outputs any
	// Error 错误信息
	Error error
	// InstanceInfo 实例信息（如 class_name 等）
	InstanceInfo map[string]any
	// InvokeID 调用标识（Workflow 事件使用）
	InvokeID string
	// Metadata 元数据（Workflow 事件的 metadata 参数）
	Metadata map[string]any
	// SourceIDs 来源标识列表
	SourceIDs []string
	// NeedSend 是否发送流数据
	NeedSend bool
	// OnInvokeData 执行期间的中间过程信息
	OnInvokeData map[string]any
	// Chunk 流式数据块
	Chunk any
	// ComponentMetadata 组件元数据
	ComponentMetadata map[string]any
}

// Tracer 追踪器，对应 Python Tracer。
// 管理 Agent/Workflow 追踪事件分发，维护 SpanManager 和 Handler 的映射关系。
type Tracer struct {
	// traceID 追踪标识
	traceID string
	// mu 保护 workflow 相关 map 的读写锁
	mu sync.RWMutex
	// AgentSpanManager Agent 追踪跨度管理器
	AgentSpanManager *SpanManager
	// WorkflowSpanManagerDict 工作流追踪跨度管理器字典，parentID → SpanManager
	WorkflowSpanManagerDict map[string]*SpanManager
	// streamWriterManager 流写入器管理器
	streamWriterManager *stream.StreamWriterManager
	// agentHandler Agent 追踪处理器
	agentHandler *TraceAgentHandler
	// agentDispatch Agent 事件分发表，event → handler 方法闭包
	agentDispatch map[TraceEvent]func(context.Context, *TriggerParams)
	// workflowHandlers 工作流追踪处理器字典，parentID → handler
	workflowHandlers map[string]*TraceWorkflowHandler
	// workflowDispatch 工作流事件分发表，parentID → event → handler 方法闭包
	workflowDispatch map[string]map[TraceEvent]func(context.Context, *TriggerParams)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTracer 创建追踪器，自动生成 UUID 作为 traceID，创建 AgentSpanManager。
func NewTracer() *Tracer {
	traceID := uuid.New().String()
	return &Tracer{
		traceID:                 traceID,
		AgentSpanManager:        NewSpanManager(traceID),
		WorkflowSpanManagerDict: make(map[string]*SpanManager),
		agentDispatch:           make(map[TraceEvent]func(context.Context, *TriggerParams)),
		workflowHandlers:        make(map[string]*TraceWorkflowHandler),
		workflowDispatch:        make(map[string]map[TraceEvent]func(context.Context, *TriggerParams)),
	}
}

// Init 初始化追踪器，创建默认 Handler 并构建事件分发表。
// 对应 Python Tracer.init(stream_writer_manager)。
func (t *Tracer) Init(swm *stream.StreamWriterManager) {
	t.streamWriterManager = swm

	// 创建 Agent 追踪处理器
	traceWriter := swm.GetTraceWriter()
	t.agentHandler = NewTraceAgentHandler(traceWriter, t.AgentSpanManager)

	// 创建默认 Workflow SpanManager（parentID=""），对应 Python tracer_workflow_span_manager_dict[""]
	defaultSpanManager := NewSpanManager(t.traceID)
	t.WorkflowSpanManagerDict[""] = defaultSpanManager

	// 创建默认 Workflow 追踪处理器
	wfHandler := NewTraceWorkflowHandler(traceWriter, defaultSpanManager)
	t.workflowHandlers[""] = wfHandler

	// 构建 Agent 事件分发表（22 个事件）
	t.buildAgentDispatch()

	// 构建默认 Workflow 事件分发表（8 个事件）
	t.buildWorkflowDispatch("")
}

// TriggerAgent 触发 Agent 追踪事件，通过 agentDispatch 分发到对应 handler 方法。
// 对应 Python Tracer.trigger("tracer_agent", event_name, ...)。
func (t *Tracer) TriggerAgent(ctx context.Context, event TraceEvent, params *TriggerParams) {
	t.mu.RLock()
	handler, ok := t.agentDispatch[event]
	t.mu.RUnlock()
	if !ok {
		logger.Warn(logComponent).
			Str("event", string(event)).
			Msg("追踪事件未找到处理器")
		return
	}
	handler(ctx, params)
}

// TriggerWorkflow 触发工作流追踪事件，按 parentNodeID 查找 workflowDispatch 分发。
// 对应 Python Tracer.trigger("tracer_workflow", event_name, parent_node_id=..., ...)。
func (t *Tracer) TriggerWorkflow(ctx context.Context, event TraceEvent, parentNodeID string, params *TriggerParams) {
	t.mu.RLock()
	dispatch, ok := t.workflowDispatch[parentNodeID]
	t.mu.RUnlock()
	if !ok {
		logger.Warn(logComponent).
			Str("event", string(event)).
			Str("parent_node_id", parentNodeID).
			Msg("追踪事件未找到处理器")
		return
	}
	handler, ok := dispatch[event]
	if !ok {
		logger.Warn(logComponent).
			Str("event", string(event)).
			Str("parent_node_id", parentNodeID).
			Msg("追踪事件未找到处理器")
		return
	}
	handler(ctx, params)
}

// RegisterWorkflowSpanManager 注册新的 Workflow SpanManager，创建对应 Handler 并扩展分发表。
// 对应 Python Tracer.register_workflow_span_manager(parent_node_id)。
func (t *Tracer) RegisterWorkflowSpanManager(parentNodeID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	spanManager := NewSpanManager(t.traceID, parentNodeID)
	t.WorkflowSpanManagerDict[parentNodeID] = spanManager

	traceWriter := t.streamWriterManager.GetTraceWriter()
	handler := NewTraceWorkflowHandler(traceWriter, spanManager)
	t.workflowHandlers[parentNodeID] = handler

	// 构建该 parentNodeID 的 Workflow 事件分发表
	t.buildWorkflowDispatch(parentNodeID)
}

// GetWorkflowSpan 获取工作流追踪跨度，对应 Python Tracer.get_workflow_span。
func (t *Tracer) GetWorkflowSpan(invokeID, parentNodeID string) *TraceWorkflowSpan {
	t.mu.RLock()
	spanManager, ok := t.WorkflowSpanManagerDict[parentNodeID]
	if !ok {
		t.mu.RUnlock()
		return nil
	}
	handler := t.workflowHandlers[parentNodeID]
	t.mu.RUnlock()

	baseSpan := spanManager.GetSpan(invokeID)
	if baseSpan == nil {
		return nil
	}
	if handler == nil {
		return nil
	}
	return handler.getTracerWorkflowSpan(invokeID)
}

// PopWorkflowSpan 移除工作流追踪跨度，对应 Python Tracer.pop_workflow_span。
// 同时从 SpanManager 和 handler.workflowSpans 缓存中删除，避免内存泄漏。
func (t *Tracer) PopWorkflowSpan(invokeID, parentNodeID string) {
	t.mu.RLock()
	spanManager, ok := t.WorkflowSpanManagerDict[parentNodeID]
	if !ok {
		t.mu.RUnlock()
		return
	}
	handler := t.workflowHandlers[parentNodeID]
	t.mu.RUnlock()

	spanManager.PopSpan(invokeID)

	// 从 handler 的 workflowSpans 缓存中删除，避免内存泄漏
	if handler != nil {
		handler.deleteWorkflowSpan(invokeID)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildAgentDispatch 构建 Agent 事件分发表，22 个 Agent TraceEvent → handler 方法闭包。
func (t *Tracer) buildAgentDispatch() {
	h := t.agentHandler

	// 链式调用事件
	t.agentDispatch[TraceChainStart] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnChainStart(ctx, span, p.Inputs, p.InstanceInfo)
	}
	t.agentDispatch[TraceChainEnd] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnChainEnd(ctx, span, p.Outputs)
	}
	t.agentDispatch[TraceChainError] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnChainError(ctx, span, p.Error)
	}

	// LLM 调用事件
	t.agentDispatch[TraceLLMStart] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnLLMStart(ctx, span, p.Inputs, p.InstanceInfo)
	}
	t.agentDispatch[TraceLLMRequest] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnLLMRequest(ctx, span, p.OnInvokeData)
	}
	t.agentDispatch[TraceLLMEnd] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnLLMEnd(ctx, span, p.Outputs)
	}
	t.agentDispatch[TraceLLMError] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnLLMError(ctx, span, p.Error)
	}

	// 提示词调用事件
	t.agentDispatch[TracePromptStart] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnPromptStart(ctx, span, p.Inputs, p.InstanceInfo)
	}
	t.agentDispatch[TracePromptEnd] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnPromptEnd(ctx, span, p.Outputs)
	}
	t.agentDispatch[TracePromptError] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnPromptError(ctx, span, p.Error)
	}

	// 插件调用事件
	t.agentDispatch[TracePluginStart] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnPluginStart(ctx, span, p.Inputs, p.InstanceInfo)
	}
	t.agentDispatch[TracePluginEnd] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnPluginEnd(ctx, span, p.Outputs)
	}
	t.agentDispatch[TracePluginError] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnPluginError(ctx, span, p.Error)
	}

	// 检索调用事件
	t.agentDispatch[TraceRetrieverStart] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnRetrieverStart(ctx, span, p.Inputs, p.InstanceInfo)
	}
	t.agentDispatch[TraceRetrieverEnd] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnRetrieverEnd(ctx, span, p.Outputs)
	}
	t.agentDispatch[TraceRetrieverError] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnRetrieverError(ctx, span, p.Error)
	}

	// 评估调用事件
	t.agentDispatch[TraceEvaluatorStart] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnEvaluatorStart(ctx, span, p.Inputs, p.InstanceInfo)
	}
	t.agentDispatch[TraceEvaluatorEnd] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnEvaluatorEnd(ctx, span, p.Outputs)
	}
	t.agentDispatch[TraceEvaluatorError] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnEvaluatorError(ctx, span, p.Error)
	}

	// 工作流调用事件（Agent 层视角）
	t.agentDispatch[TraceWorkflowStart] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnWorkflowStart(ctx, span, p.Inputs, p.InstanceInfo)
	}
	t.agentDispatch[TraceWorkflowEnd] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnWorkflowEnd(ctx, span, p.Outputs)
	}
	t.agentDispatch[TraceWorkflowError] = func(ctx context.Context, p *TriggerParams) {
		span := t.getOrCreateAgentSpan(p)
		_ = h.OnWorkflowError(ctx, span, p.Error)
	}
}

// buildWorkflowDispatch 构建指定 parentNodeID 的 Workflow 事件分发表，8 个 Workflow TraceEvent → handler 方法闭包。
func (t *Tracer) buildWorkflowDispatch(parentNodeID string) {
	h := t.workflowHandlers[parentNodeID]
	if h == nil {
		return
	}

	dispatch := make(map[TraceEvent]func(context.Context, *TriggerParams))

	// 组件调用开始
	dispatch[TraceWFCallStart] = func(ctx context.Context, p *TriggerParams) {
		_ = h.OnCallStart(ctx, p.InvokeID, p.Metadata, p.Inputs, p.NeedSend, p.SourceIDs)
	}

	// 组件预调用
	dispatch[TraceWFPreInvoke] = func(ctx context.Context, p *TriggerParams) {
		_ = h.OnPreInvoke(ctx, p.InvokeID, p.Inputs, p.ComponentMetadata, p.NeedSend)
	}

	// 组件预流式
	dispatch[TraceWFPreStream] = func(ctx context.Context, p *TriggerParams) {
		_ = h.OnPreStream(ctx, p.InvokeID, p.Chunk, p.NeedSend)
	}

	// 组件调用中
	dispatch[TraceWFInvoke] = func(ctx context.Context, p *TriggerParams) {
		var exc any
		if p.Error != nil {
			exc = p.Error
		}
		_ = h.OnInvoke(ctx, p.InvokeID, p.OnInvokeData, exc)
	}

	// 组件后流式
	dispatch[TraceWFPostStream] = func(ctx context.Context, p *TriggerParams) {
		_ = h.OnPostStream(ctx, p.InvokeID, p.Chunk)
	}

	// 组件后调用
	dispatch[TraceWFPostInvoke] = func(ctx context.Context, p *TriggerParams) {
		_ = h.OnPostInvoke(ctx, p.InvokeID, p.Outputs, p.Inputs)
	}

	// 组件调用完成
	dispatch[TraceWFCallDone] = func(ctx context.Context, p *TriggerParams) {
		_ = h.OnCallDone(ctx, p.InvokeID, p.Outputs)
	}

	// 组件交互
	dispatch[TraceWFInteract] = func(ctx context.Context, p *TriggerParams) {
		_ = h.OnInteract(ctx, p.InvokeID, p.Inputs, p.ComponentMetadata, p.NeedSend)
	}

	t.workflowDispatch[parentNodeID] = dispatch
}

// GetOrCreateAgentSpan 从 TriggerParams 获取或创建 Agent 追踪跨度（导出版本）。
// 如果 params.Span 非空，尝试转换为 TraceAgentSpan；否则自动创建。
func (t *Tracer) GetOrCreateAgentSpan(p *TriggerParams) *TraceAgentSpan {
	return t.getOrCreateAgentSpan(p)
}

// getOrCreateAgentSpan 从 TriggerParams 获取或创建 Agent 追踪跨度。
// 如果 params.Span 非空，尝试转换为 TraceAgentSpan；否则自动创建。
func (t *Tracer) getOrCreateAgentSpan(p *TriggerParams) *TraceAgentSpan {
	if p.Span != nil {
		// 尝试从 SpanManager 的缓存中获取具体类型
		// TriggerParams.Span 是 *Span 基础类型，需要通过 handler 的缓存查找
		if span := t.agentHandler.getTracerAgentSpan(p.Span.InvokeID); span != nil {
			return span
		}
	}
	return t.agentHandler.getTracerAgentSpan("")
}

// SetAgentHandler 设置 Agent 追踪处理器（用于测试）。
func (t *Tracer) SetAgentHandler(h *TraceAgentHandler) {
	t.agentHandler = h
}

// SetAgentDispatchEntry 设置 Agent 事件分发表条目（用于测试）。
func (t *Tracer) SetAgentDispatchEntry(event TraceEvent, handler func(context.Context, *TriggerParams)) {
	t.agentDispatch[event] = handler
}
