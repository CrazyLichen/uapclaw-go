package tracer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// graphInterrupter 图中断信号接口，避免 tracer → interaction 循环依赖。
// interaction.GraphInterrupt 隐式满足此接口。
type graphInterrupter interface {
	isGraphInterrupt()
}

// traceBaseHandler 追踪基础处理器，对应 Python TraceBaseHandler。
// 提供 FormatData 抽象方法、EmitStreamWriter/GetElapsedTime/GetNodeStatus 通用方法。
type traceBaseHandler struct {
	// streamWriter 流写入器
	streamWriter stream.StreamWriter
	// spanManager 追踪跨度管理器
	spanManager *SpanManager
}

// TraceAgentHandler Agent 追踪处理器，对应 Python TraceAgentHandler。
// 维护 agentSpans 映射以保持具体类型引用。
type TraceAgentHandler struct {
	traceBaseHandler
	// agentSpans Agent 追踪跨度缓存，invokeID → *TraceAgentSpan
	agentSpans map[string]*TraceAgentSpan
	// agentMu 保护 agentSpans 的读写锁
	agentMu sync.RWMutex
}

// TraceWorkflowHandler 工作流追踪处理器，对应 Python TraceWorkflowHandler。
// 维护 workflowSpans 映射以保持具体类型引用。
type TraceWorkflowHandler struct {
	traceBaseHandler
	// workflowSpans 工作流追踪跨度缓存，invokeID → *TraceWorkflowSpan
	workflowSpans map[string]*TraceWorkflowSpan
	// workflowMu 保护 workflowSpans 的读写锁
	workflowMu sync.RWMutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTraceAgentHandler 创建 Agent 追踪处理器
func NewTraceAgentHandler(streamWriter stream.StreamWriter, spanManager *SpanManager) *TraceAgentHandler {
	return &TraceAgentHandler{
		traceBaseHandler: traceBaseHandler{
			streamWriter: streamWriter,
			spanManager:  spanManager,
		},
		agentSpans: make(map[string]*TraceAgentSpan),
	}
}

// NewTraceWorkflowHandler 创建工作流追踪处理器
func NewTraceWorkflowHandler(streamWriter stream.StreamWriter, spanManager *SpanManager) *TraceWorkflowHandler {
	return &TraceWorkflowHandler{
		traceBaseHandler: traceBaseHandler{
			streamWriter: streamWriter,
			spanManager:  spanManager,
		},
		workflowSpans: make(map[string]*TraceWorkflowSpan),
	}
}

// OnChainStart 链式调用开始
func (h *TraceAgentHandler) OnChainStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error {
	if err := h.updateStartTraceData(span, string(InvokeTypeChain), inputs, instanceInfo); err != nil {
		return err
	}
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnChainEnd 链式调用结束
func (h *TraceAgentHandler) OnChainEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error {
	h.updateEndTraceData(span, outputs)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnChainError 链式调用错误
func (h *TraceAgentHandler) OnChainError(ctx context.Context, span *TraceAgentSpan, err error) error {
	h.updateErrorTraceData(span, err)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnLLMStart LLM 调用开始
func (h *TraceAgentHandler) OnLLMStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error {
	if err := h.updateStartTraceData(span, string(InvokeTypeLLM), inputs, instanceInfo); err != nil {
		return err
	}
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnLLMRequest LLM 请求详情
func (h *TraceAgentHandler) OnLLMRequest(ctx context.Context, span *TraceAgentSpan, data map[string]any) error {
	h.updateRunningTraceData(span, data)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnLLMEnd LLM 调用结束
func (h *TraceAgentHandler) OnLLMEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error {
	h.updateEndTraceData(span, outputs)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnLLMError LLM 调用错误
func (h *TraceAgentHandler) OnLLMError(ctx context.Context, span *TraceAgentSpan, err error) error {
	h.updateErrorTraceData(span, err)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnPromptStart 提示词调用开始
func (h *TraceAgentHandler) OnPromptStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error {
	if err := h.updateStartTraceData(span, string(InvokeTypePrompt), inputs, instanceInfo); err != nil {
		return err
	}
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnPromptEnd 提示词调用结束
func (h *TraceAgentHandler) OnPromptEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error {
	h.updateEndTraceData(span, outputs)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnPromptError 提示词调用错误
func (h *TraceAgentHandler) OnPromptError(ctx context.Context, span *TraceAgentSpan, err error) error {
	h.updateErrorTraceData(span, err)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnPluginStart 插件调用开始
func (h *TraceAgentHandler) OnPluginStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error {
	if err := h.updateStartTraceData(span, string(InvokeTypePlugin), inputs, instanceInfo); err != nil {
		return err
	}
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnPluginEnd 插件调用结束
func (h *TraceAgentHandler) OnPluginEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error {
	h.updateEndTraceData(span, outputs)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnPluginError 插件调用错误
func (h *TraceAgentHandler) OnPluginError(ctx context.Context, span *TraceAgentSpan, err error) error {
	h.updateErrorTraceData(span, err)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnRetrieverStart 检索调用开始
func (h *TraceAgentHandler) OnRetrieverStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error {
	if err := h.updateStartTraceData(span, string(InvokeTypeRetriever), inputs, instanceInfo); err != nil {
		return err
	}
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnRetrieverEnd 检索调用结束
func (h *TraceAgentHandler) OnRetrieverEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error {
	h.updateEndTraceData(span, outputs)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnRetrieverError 检索调用错误
func (h *TraceAgentHandler) OnRetrieverError(ctx context.Context, span *TraceAgentSpan, err error) error {
	h.updateErrorTraceData(span, err)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnEvaluatorStart 评估调用开始
func (h *TraceAgentHandler) OnEvaluatorStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error {
	if err := h.updateStartTraceData(span, string(InvokeTypeEvaluator), inputs, instanceInfo); err != nil {
		return err
	}
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnEvaluatorEnd 评估调用结束
func (h *TraceAgentHandler) OnEvaluatorEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error {
	h.updateEndTraceData(span, outputs)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnEvaluatorError 评估调用错误
func (h *TraceAgentHandler) OnEvaluatorError(ctx context.Context, span *TraceAgentSpan, err error) error {
	h.updateErrorTraceData(span, err)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnWorkflowStart 工作流调用开始
func (h *TraceAgentHandler) OnWorkflowStart(ctx context.Context, span *TraceAgentSpan, inputs any, instanceInfo map[string]any) error {
	if err := h.updateStartTraceData(span, string(InvokeTypeWorkflow), inputs, instanceInfo); err != nil {
		return err
	}
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnWorkflowEnd 工作流调用结束
func (h *TraceAgentHandler) OnWorkflowEnd(ctx context.Context, span *TraceAgentSpan, outputs any) error {
	h.updateEndTraceData(span, outputs)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// OnWorkflowError 工作流调用错误
func (h *TraceAgentHandler) OnWorkflowError(ctx context.Context, span *TraceAgentSpan, err error) error {
	h.updateErrorTraceData(span, err)
	return h.EmitStreamWriter(ctx, &span.Span)
}

// FormatData 格式化 Agent 追踪数据，返回 {"type": "tracer_agent", "payload": agentSpan}
func (h *TraceAgentHandler) FormatData(span *Span) map[string]any {
	agentSpan := h.getTracerAgentSpan(span.InvokeID)
	if agentSpan.Status != string(NodeStatusInterrupted) {
		agentSpan.Status = string(h.GetNodeStatus(&agentSpan.Span))
	}
	return map[string]any{
		"type":    string(TracerHandlerAgent),
		"payload": agentSpan,
	}
}

// OnCallStart 组件调用开始
func (h *TraceWorkflowHandler) OnCallStart(ctx context.Context, invokeID string, metadata map[string]any, inputs any, needSend bool, sourceIDs []string) error {
	span := h.getTracerWorkflowSpan(invokeID)
	now := time.Now()
	span.StartTime = &now
	span.OnInvokeData = []map[string]any{}
	span.Inputs = inputs
	span.Outputs = nil
	span.StreamOutputs = []any{}
	span.SourceIDs = sourceIDs
	// 合并 metadata 字段（如 ComponentID, ComponentName, ComponentType 等）
	setWorkflowMetadata(span, metadata)
	h.spanManager.UpdateSpan(&span.Span, map[string]any{})
	if needSend {
		return h.EmitStreamWriter(ctx, &span.Span)
	}
	return nil
}

// OnPreInvoke 组件预调用
func (h *TraceWorkflowHandler) OnPreInvoke(ctx context.Context, invokeID string, inputs any, componentMetadata map[string]any, needSend bool) error {
	span := h.getTracerWorkflowSpan(invokeID)
	span.Inputs = inputs
	setWorkflowMetadata(span, componentMetadata)
	h.spanManager.UpdateSpan(&span.Span, map[string]any{})
	if needSend {
		return h.sendData(span, map[string]bool{"outputs": true, "streamOutputs": true})
	}
	return nil
}

// OnPreStream 组件预流式，对应 Python TraceWorkflowHandler.on_pre_stream。
// 对齐 Python: if chunk and isinstance(chunk, dict) — 非空 dict 才追加到 streamInputs。
// Python 入口层 dict(chunk) 保证类型，Go 用类型断言替代。
func (h *TraceWorkflowHandler) OnPreStream(ctx context.Context, invokeID string, chunk any, needSend bool) error {
	span := h.getTracerWorkflowSpan(invokeID)
	// 对齐 Python: if chunk and isinstance(chunk, dict) — 非空 dict 才追加
	if m, ok := chunk.(map[string]any); ok && len(m) > 0 {
		span.AppendStreamInputs(m)
	}
	h.spanManager.UpdateSpan(&span.Span, map[string]any{})
	if needSend {
		return h.sendData(span, map[string]bool{"outputs": true, "streamOutputs": true})
	}
	return nil
}

// OnInvoke 组件调用中（运行时数据/错误）
// exc 参数支持 *exception.BaseError、*interaction.GraphInterrupt 和普通 error 三种类型
func (h *TraceWorkflowHandler) OnInvoke(ctx context.Context, invokeID string, onInvokeData map[string]any, exc any) error {
	span := h.getTracerWorkflowSpan(invokeID)

	if exc != nil {
		now := time.Now()
		switch e := exc.(type) {
		case *exception.BaseError:
			span.Error = map[string]any{
				"error_code": e.Code(),
				"message":    e.Message(),
			}
		case graphInterrupter:
			span.Status = string(NodeStatusInterrupted)
		case error:
			errMsg := exception.StatusWorkflowExecutionError.RenderMessage(
				map[string]any{"reason": fmt.Sprintf("%v", e), "workflow": ""})
			span.Error = map[string]any{
				"error_code": exception.StatusWorkflowExecutionError.Code(),
				"message":    errMsg,
			}
		default:
			errMsg := exception.StatusWorkflowExecutionError.RenderMessage(
				map[string]any{"reason": fmt.Sprintf("%v", exc), "workflow": ""})
			span.Error = map[string]any{
				"error_code": exception.StatusWorkflowExecutionError.Code(),
				"message":    errMsg,
			}
		}

		if onInvokeData != nil {
			if innerErr, ok := onInvokeData["inner_error"]; ok {
				if m, ok := innerErr.(map[string]any); ok {
					span.InnerError = m
				}
			}
			span.OnInvokeData = append(span.OnInvokeData, onInvokeData)
		}

		span.EndTime = &now
		if span.StartTime != nil {
			// 对齐 Python: elapsed_time = self._get_elapsed_time(span.start_time, end_time)
			// TraceWorkflowSpan 没有 ElapsedTime 字段，计算仅用于 UpdateSpan 更新
			elapsed := h.GetElapsedTime(*span.StartTime, now)
			h.spanManager.UpdateSpan(&span.Span, map[string]any{"elapsed_time": elapsed})
		}
		h.spanManager.UpdateSpan(&span.Span, map[string]any{})
	} else {
		if span.OnInvokeData == nil {
			span.OnInvokeData = []map[string]any{}
		}
		if onInvokeData != nil {
			if innerErr, ok := onInvokeData["inner_error"]; ok {
				if m, ok := innerErr.(map[string]any); ok {
					span.InnerError = m
				}
			}
			span.OnInvokeData = append(span.OnInvokeData, onInvokeData)
		}
		h.spanManager.UpdateSpan(&span.Span, map[string]any{})
	}

	writeErr := h.EmitStreamWriter(ctx, &span.Span)
	if exc != nil && span.ComponentType == "LLM" {
		h.spanManager.UpdateSpan(&span.Span, map[string]any{})
	}
	return writeErr
}

// OnPostStream 组件后流式
func (h *TraceWorkflowHandler) OnPostStream(_ context.Context, invokeID string, chunk any) error {
	span := h.getTracerWorkflowSpan(invokeID)
	span.AppendStreamOutput(chunk)
	return nil
}

// OnPostInvoke 组件后调用
func (h *TraceWorkflowHandler) OnPostInvoke(_ context.Context, invokeID string, outputs any, inputs any) error {
	span := h.getTracerWorkflowSpan(invokeID)
	span.Outputs = outputs
	if inputs != nil && (span.ComponentType == "End" || span.ComponentType == "Message") {
		span.Inputs = inputs
	}
	h.spanManager.UpdateSpan(&span.Span, map[string]any{})
	return nil
}

// OnCallDone 组件调用完成
func (h *TraceWorkflowHandler) OnCallDone(ctx context.Context, invokeID string, outputs any) error {
	span := h.getTracerWorkflowSpan(invokeID)
	now := time.Now()
	span.EndTime = &now
	if outputs != nil {
		span.Outputs = outputs
	}
	// 对齐 Python: self._span_manager.update_span(span, update_data)
	// 空 map 传入用于刷新 span 在 SpanManager 中的记录（确保 sessionSpans 映射存在）
	h.spanManager.UpdateSpan(&span.Span, map[string]any{})
	writeErr := h.EmitStreamWriter(ctx, &span.Span)
	if span.ComponentType == "End" && span.EndTime != nil {
		h.spanManager.UpdateSpan(&span.Span, map[string]any{})
	}
	return writeErr
}

// OnInteract 组件交互
func (h *TraceWorkflowHandler) OnInteract(ctx context.Context, invokeID string, inputs any, componentMetadata map[string]any, needSend bool) error {
	span := h.getTracerWorkflowSpan(invokeID)
	span.InteractiveInputs = inputs
	setWorkflowMetadata(span, componentMetadata)
	h.spanManager.UpdateSpan(&span.Span, map[string]any{})
	if needSend {
		return h.sendData(span, map[string]bool{"outputs": true, "streamOutputs": true})
	}
	return nil
}

// FormatData 格式化工作流追踪数据，返回 {"type": "tracer_workflow", "payload": wfSpan}，排除 ChildInvokesID 和 LLMInvokeData
func (h *TraceWorkflowHandler) FormatData(span *Span) map[string]any {
	wfSpan := h.getTracerWorkflowSpan(span.InvokeID)
	if wfSpan.Status != string(NodeStatusInterrupted) {
		status := h.GetNodeStatus(&wfSpan.Span)
		// Python getattr(span, "inner_error", None) 检查：有 inner_error 时也应返回 ERROR
		if wfSpan.InnerError != nil {
			status = NodeStatusError
		}
		wfSpan.Status = string(status)
	}
	result := buildWorkflowPayload(wfSpan)
	return map[string]any{
		"type":    string(TracerHandlerWorkflow),
		"payload": result,
	}
}

// EmitStreamWriter 格式化 Span 数据并写入流，对应 Python _emit_stream_writer。
// 注意：此方法需由具体子类（TraceAgentHandler/TraceWorkflowHandler）调用，
// 子类的 FormatData 会覆盖基础实现。
func (h *TraceAgentHandler) EmitStreamWriter(ctx context.Context, span *Span) error {
	if h.streamWriter == nil {
		return nil
	}
	data := h.FormatData(span)
	if writeErr := h.streamWriter.Write(ctx, stream.TraceSchema{
		Type:    data["type"].(string),
		Payload: data["payload"],
	}); writeErr != nil {
		logger.Error(logComponent).Err(writeErr).Msg("追踪数据写入流失败")
		return writeErr
	}
	return nil
}

// EmitStreamWriter 格式化 Span 数据并写入流，对应 Python _emit_stream_writer。
func (h *TraceWorkflowHandler) EmitStreamWriter(ctx context.Context, span *Span) error {
	if h.streamWriter == nil {
		return nil
	}
	data := h.FormatData(span)
	if writeErr := h.streamWriter.Write(ctx, stream.TraceSchema{
		Type:    data["type"].(string),
		Payload: data["payload"],
	}); writeErr != nil {
		logger.Error(logComponent).Err(writeErr).Msg("追踪数据写入流失败")
		return writeErr
	}
	return nil
}

// GetElapsedTime 计算耗时字符串，<1s 返回 "Xms"，>=1s 返回 "X.XXs"
func (h *traceBaseHandler) GetElapsedTime(start, end time.Time) string {
	ms := end.Sub(start).Milliseconds()
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.2fs", float64(ms)/1000)
}

// GetNodeStatus 根据 Span 状态判断节点状态，对应 Python _get_node_status。
// 注意：Python 还检查 inner_error（getattr(span, "inner_error", None)），
// 但 Go 的 GetNodeStatus 接收 *Span 基础类型无法访问 TraceWorkflowSpan.InnerError。
// Workflow 调用方应使用 GetWorkflowNodeStatus 补充 InnerError 检查。
func (h *traceBaseHandler) GetNodeStatus(span *Span) NodeStatus {
	if span.Error != nil {
		return NodeStatusError
	}
	if len(span.OnInvokeData) > 0 {
		if span.EndTime == nil {
			return NodeStatusRunning
		}
		return NodeStatusFinish
	}
	if span.EndTime != nil {
		return NodeStatusFinish
	}
	return NodeStatusStart
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// updateStartTraceData 更新开始追踪数据，对应 Python _update_start_trace_data。
// 返回 error 表示序列化失败，调用方应阻止后续 EmitStreamWriter（对齐 Python 异常中断流程）。
func (h *TraceAgentHandler) updateStartTraceData(span *TraceAgentSpan, invokeType string, inputs any, instanceInfo map[string]any) error {
	metaDataBytes, err := json.Marshal(instanceInfo)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "SYSTEM_ERROR").
			Str("error", err.Error()).
			Any("instance_info", instanceInfo).
			Msg("元数据处理失败")
		return err
	}
	var metaDataMap map[string]any
	if jsonErr := json.Unmarshal(metaDataBytes, &metaDataMap); jsonErr != nil {
		metaDataMap = instanceInfo
	}

	now := time.Now()
	span.StartTime = &now
	span.InvokeType = invokeType
	span.Inputs = inputs
	span.Name = ""
	if className, ok := instanceInfo["class_name"]; ok {
		span.Name = fmt.Sprintf("%v", className)
	}
	span.MetaData = metaDataMap

	h.spanManager.UpdateSpan(&span.Span, map[string]any{})
	return nil
}

// updateEndTraceData 更新结束追踪数据，对应 Python _update_end_trace_data
func (h *TraceAgentHandler) updateEndTraceData(span *TraceAgentSpan, outputs any) {
	now := time.Now()
	span.EndTime = &now
	span.Outputs = outputs
	if span.StartTime != nil {
		span.ElapsedTime = h.GetElapsedTime(*span.StartTime, now)
	}
	h.spanManager.UpdateSpan(&span.Span, map[string]any{})
}

// updateErrorTraceData 更新错误追踪数据，对应 Python _update_error_trace_data
func (h *TraceAgentHandler) updateErrorTraceData(span *TraceAgentSpan, err error) {
	now := time.Now()
	var errorInfo map[string]any

	if baseErr, ok := err.(*exception.BaseError); ok {
		errorInfo = map[string]any{
			"error_code": baseErr.Code(),
			"message":    baseErr.Message(),
		}
	} else {
		errMsg := exception.StatusWorkflowExecutionError.RenderMessage(
			map[string]any{"reason": fmt.Sprintf("%v", err), "workflow": ""})
		errorInfo = map[string]any{
			"error_code": exception.StatusWorkflowExecutionError.Code(),
			"message":    errMsg,
		}
	}

	span.EndTime = &now
	span.Error = errorInfo
	if span.StartTime != nil {
		span.ElapsedTime = h.GetElapsedTime(*span.StartTime, now)
	}
	h.spanManager.UpdateSpan(&span.Span, map[string]any{})
}

// updateRunningTraceData 更新运行中追踪数据，对应 Python _update_running_trace_data
func (h *TraceAgentHandler) updateRunningTraceData(span *TraceAgentSpan, data map[string]any) {
	if span.OnInvokeData == nil {
		span.OnInvokeData = []map[string]any{}
	}
	span.OnInvokeData = append(span.OnInvokeData, data)
	h.spanManager.UpdateSpan(&span.Span, map[string]any{})
}

// getTracerAgentSpan 获取或创建 Agent 追踪跨度，对应 Python _get_tracer_agent_span
func (h *TraceAgentHandler) getTracerAgentSpan(invokeID string) *TraceAgentSpan {
	h.agentMu.RLock()
	if span, ok := h.agentSpans[invokeID]; ok {
		h.agentMu.RUnlock()
		return span
	}
	h.agentMu.RUnlock()
	// SpanManager 中存在但 agentSpans 中没有时，说明是外部创建的，
	// 无法还原具体类型，仍需创建新的 TraceAgentSpan
	var parentSpan *TraceAgentSpan
	lastSpan := h.spanManager.LastSpan()
	if lastSpan != nil {
		h.agentMu.RLock()
		if p, ok := h.agentSpans[lastSpan.InvokeID]; ok {
			parentSpan = p
		}
		h.agentMu.RUnlock()
	}
	span := h.spanManager.CreateAgentSpan(parentSpan)
	h.agentMu.Lock()
	h.agentSpans[span.InvokeID] = span
	h.agentMu.Unlock()
	return span
}

// getTracerWorkflowSpan 获取或创建工作流追踪跨度，对应 Python _get_tracer_workflow_span
func (h *TraceWorkflowHandler) getTracerWorkflowSpan(invokeID string) *TraceWorkflowSpan {
	h.workflowMu.RLock()
	if span, ok := h.workflowSpans[invokeID]; ok {
		h.workflowMu.RUnlock()
		return span
	}
	h.workflowMu.RUnlock()
	var parentSpan *TraceWorkflowSpan
	lastSpan := h.spanManager.LastSpan()
	if lastSpan != nil {
		h.workflowMu.RLock()
		if p, ok := h.workflowSpans[lastSpan.InvokeID]; ok {
			parentSpan = p
		}
		h.workflowMu.RUnlock()
	}
	span := h.spanManager.CreateWorkflowSpan(invokeID, parentSpan)
	h.workflowMu.Lock()
	h.workflowSpans[invokeID] = span
	h.workflowMu.Unlock()
	return span
}

// deleteWorkflowSpan 从缓存中删除工作流追踪跨度，避免内存泄漏。
// 由 Tracer.PopWorkflowSpan 调用，与 SpanManager.PopSpan 配合使用。
func (h *TraceWorkflowHandler) deleteWorkflowSpan(invokeID string) {
	h.workflowMu.Lock()
	delete(h.workflowSpans, invokeID)
	h.workflowMu.Unlock()
}

// sendData 发送数据，exclude 指定需要排除的字段名，对应 Python _send_data
func (h *TraceWorkflowHandler) sendData(span *TraceWorkflowSpan, exclude map[string]bool) error {
	if h.streamWriter == nil {
		return nil
	}
	if span.Status != string(NodeStatusInterrupted) {
		status := h.GetNodeStatus(&span.Span)
		// Python getattr(span, "inner_error", None) 检查
		if span.InnerError != nil {
			status = NodeStatusError
		}
		span.Status = string(status)
	}
	payload := buildWorkflowPayloadWithExclude(span, exclude)
	data := map[string]any{
		"type":    string(TracerHandlerWorkflow),
		"payload": payload,
	}
	if writeErr := h.streamWriter.Write(context.Background(), stream.TraceSchema{
		Type:    data["type"].(string),
		Payload: data["payload"],
	}); writeErr != nil {
		logger.Error(logComponent).Err(writeErr).Msg("追踪数据写入流失败")
		return writeErr
	}
	return nil
}

// setWorkflowMetadata 将 metadata 映射设置到 TraceWorkflowSpan 对应字段
func setWorkflowMetadata(span *TraceWorkflowSpan, metadata map[string]any) {
	for k, v := range metadata {
		switch k {
		case "ExecutionID":
			if s, ok := v.(string); ok {
				span.ExecutionID = s
			}
		case "WorkflowID":
			if s, ok := v.(string); ok {
				span.WorkflowID = s
			}
		case "WorkflowVersion":
			if s, ok := v.(string); ok {
				span.WorkflowVersion = s
			}
		case "WorkflowName":
			if s, ok := v.(string); ok {
				span.WorkflowName = s
			}
		case "ComponentID":
			if s, ok := v.(string); ok {
				span.ComponentID = s
			}
		case "ComponentName":
			if s, ok := v.(string); ok {
				span.ComponentName = s
			}
		case "ComponentType":
			if s, ok := v.(string); ok {
				span.ComponentType = s
			}
		case "LoopNodeID":
			if s, ok := v.(string); ok {
				span.LoopNodeID = s
			}
		case "LoopIndex":
			if i, ok := v.(int); ok {
				span.LoopIndex = &i
			}
		case "ParentNodeID":
			if s, ok := v.(string); ok {
				span.ParentNodeID = s
			}
		default:
			// 忽略未知字段
		}
	}
}

// buildWorkflowPayload 构建工作流 payload，排除 ChildInvokesID 和 LLMInvokeData。
// 对齐 Python: span.model_dump(exclude_none=True, by_alias=True, exclude={"child_invokes_id", "llm_invoke_data"})
// Python 的 exclude_none 只排除 None 值，空字符串 "" 和空列表 [] 会保留。
// Go 端对齐：nil 指针不输出，字符串/切片始终输出（即使为空/零值）。
func buildWorkflowPayload(span *TraceWorkflowSpan) map[string]any {
	result := map[string]any{}

	result["traceId"] = span.TraceID
	result["invokeId"] = span.InvokeID
	result["parentInvokeId"] = span.ParentInvokeID
	result["status"] = span.Status
	if span.StartTime != nil {
		result["startTime"] = span.StartTime
	}
	if span.EndTime != nil {
		result["endTime"] = span.EndTime
	}
	if span.Inputs != nil {
		result["inputs"] = span.Inputs
	}
	if span.Outputs != nil {
		result["outputs"] = span.Outputs
	}
	if span.Error != nil {
		result["error"] = span.Error
	}
	result["onInvokeData"] = span.OnInvokeData
	result["executionId"] = span.ExecutionID
	result["sourceIds"] = span.SourceIDs
	result["workflowId"] = span.WorkflowID
	result["workflowVersion"] = span.WorkflowVersion
	result["workflowName"] = span.WorkflowName
	result["componentId"] = span.ComponentID
	result["componentName"] = span.ComponentName
	result["componentType"] = span.ComponentType
	result["loopNodeId"] = span.LoopNodeID
	if span.LoopIndex != nil {
		result["loopIndex"] = *span.LoopIndex
	}
	result["parentNodeId"] = span.ParentNodeID
	result["streamInputs"] = span.StreamInputs
	result["streamOutputs"] = span.StreamOutputs
	if span.InteractiveInputs != nil {
		result["interactiveInputs"] = span.InteractiveInputs
	}
	if span.InnerError != nil {
		result["innerError"] = span.InnerError
	}
	// 注意：不包含 childInvokesID 和 llmInvokeData，与 Python exclude 对齐
	return result
}

// buildWorkflowPayloadWithExclude 构建工作流 payload，排除指定字段
func buildWorkflowPayloadWithExclude(span *TraceWorkflowSpan, exclude map[string]bool) map[string]any {
	result := buildWorkflowPayload(span)
	for field := range exclude {
		delete(result, field)
	}
	return result
}
