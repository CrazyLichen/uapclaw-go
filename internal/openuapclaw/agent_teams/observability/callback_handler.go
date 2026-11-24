package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	intellirouter "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients/intellirouter"
	"reflect"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	hprompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OtelCallbackHandler 注册到 CallbackFramework 的 OTel 回调处理器。
// Python: OtelCallbackHandler (callback_handler.py)
//
// 设计为全局单例，在 init_observability 时创建并注册。
// 每个回调方法从 ctx 中读取 OtelSpanState 来追踪 span 栈。
type OtelCallbackHandler struct {
	// config 当前可观测性配置
	config *ObservabilityConfig
	// injectedTracer 可选显式注入的 tracer（测试用）
	injectedTracer trace.Tracer
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// tracerName OTel tracer 名称
	// Python: _TRACER_NAME = "openjiuwen.agent_teams.observability"
	tracerName = "openjiuwen.agent_teams.observability"
	// genAISystemValue gen_ai.system 属性值
	// Python: _GEN_AI_SYSTEM_VALUE = "openjiuwen"
	genAISystemValue = "openjiuwen"
	// logComponent 日志组件
	logComponent = logger.ComponentTeam
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOtelCallbackHandler 创建 OtelCallbackHandler。
// Python: OtelCallbackHandler(config, tracer=...)
func NewOtelCallbackHandler(config *ObservabilityConfig, tracer trace.Tracer) *OtelCallbackHandler {
	return &OtelCallbackHandler{
		config:         config,
		injectedTracer: tracer,
	}
}

// OnLLMInvokeInput LLM invoke 调用前：打开 LLM span 并附加 prompt 属性。
// Python: on_llm_invoke_input
func (h *OtelCallbackHandler) OnLLMInvokeInput(ctx context.Context, data *cb.LLMCallEventData) any {
	defer recoverCallback("on_llm_invoke_input")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	h.openLlmSpan(ctx, data, spanState)
	return nil
}

// OnLLMStreamInput LLM stream 调用前：打开 LLM span（与 invoke_input 共享逻辑）。
// Python: on_llm_stream_input
func (h *OtelCallbackHandler) OnLLMStreamInput(ctx context.Context, data *cb.LLMCallEventData) any {
	defer recoverCallback("on_llm_stream_input")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	h.openLlmSpan(ctx, data, spanState)
	return nil
}

// OnLLMInput 用 provider 归一后的请求增强已开的 LLM span。
// Python: on_llm_input（callback_handler.py:529-564）——LLM_*_INPUT 在
// 模型客户端归一请求前开 span，provider 的 LLM_INPUT 事件是附加结构化
// 请求字段的权威来源；它不关闭也不替换 span，不动旧扁平索引字段。
func (h *OtelCallbackHandler) OnLLMInput(ctx context.Context, data *cb.LLMCallEventData) any {
	defer recoverCallback("on_llm_input")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	// Python: span = get_current_llm_span(); state = span.otel_llm_state
	state := spanState.PopLlmSpanState(true)
	if state == nil || !state.Span.IsRecording() {
		return nil
	}
	if data.Messages == nil || state.ContextWindowCommitted {
		return nil
	}
	providerMessages := normalizeMessages(data.Messages)
	if len(providerMessages) == len(state.MessageOccurrenceIDs) {
		h.recordStandardStructuredInput(state.Span, providerMessages)
		state.trajectoryMessagesCommitted = h.trajectoryMessages(providerMessages,
			state.MessageOccurrenceIDs, state.MessageMetadata)
	} else {
		state.trajectoryMessagesCommitted = cloneMessageMaps(state.InitialTrajectoryMessages)
	}
	// Python: publish_span_snapshot(state.span, "attributes")——span_record
	// 落盘域未移植（偏差 spec §4），跳过。
	// Python: emit_context_window_commit(tracer, llm_span, messages, request_purpose)
	EmitContextWindowCommit(h.tracer(), state.Span, state.trajectoryMessagesCommitted,
		state.RequestPurpose, state.parentAttributes)
	state.ContextWindowCommitted = true
	return nil
}

// OnLLMResponseStarted 响应头到达时记录首字节时延与 SDK 重试次数。
// Python: on_llm_response_started（callback_handler.py:608-624）。
func (h *OtelCallbackHandler) OnLLMResponseStarted(ctx context.Context, data *cb.LLMCallEventData) any {
	defer recoverCallback("on_llm_response_started")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	state := spanState.PopLlmSpanState(true)
	if state == nil || !state.Span.IsRecording() || state.ResponseStartedNs != 0 {
		return nil
	}
	state.ResponseStartedNs = time.Now().UnixNano()
	state.Span.SetAttributes(attribute.Float64(OJGenAIResponseTimeToFirstByteMs,
		float64(state.ResponseStartedNs-state.StartNs)/1_000_000.0))
	if data.RetryCount != nil {
		state.Span.SetAttributes(attribute.Int(OJRequestRetryCount, *data.RetryCount))
	}
	return nil
}

// OnLLMRoute 把模型组请求实际路由到的模型名写在已开的 span 上。
// Python: on_llm_route（callback_handler.py:626-664）——span 在路由器选定
// 部署前打开，初名是逻辑 "*" 模型；LLM_ROUTE_ATTEMPT（请求发出，无论成败）
// 与 LLM_ROUTE（有路由应答）都在同一次调用内为 span 改名并回填记录的输入。
func (h *OtelCallbackHandler) OnLLMRoute(ctx context.Context, data map[string]any) any {
	defer recoverCallback("on_llm_route")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	state := spanState.PopLlmSpanState(true)
	if state == nil || !state.Span.IsRecording() {
		return nil
	}
	routeMetadata, _ := data["route_metadata"].(map[string]any)
	if routeMetadata == nil {
		return nil
	}
	// Python: group_id = str(route_metadata.get("model_group_id") or "").strip()
	groupID := trimmedString(routeMetadata["model_group_id"])
	if groupID != "" {
		state.Span.SetAttributes(attribute.String(OJModelGroupID, groupID))
	}
	modelName := trimmedString(routeMetadata["model_name"])
	if modelName == "" {
		return nil
	}
	// Python: state.span.update_name(f"chat {model_name}")
	if namer, ok := state.Span.(interface{ SetName(string) }); ok {
		namer.SetName(fmt.Sprintf("chat %s", modelName))
	}
	state.Span.SetAttributes(attribute.String(GenAIRequestModel, modelName))
	routeID := trimmedString(routeMetadata["route_id"])
	if routeID != "" {
		state.Span.SetAttributes(attribute.String(OJModelGroupRouteID, routeID))
	}
	routedModel := trimmedString(routeMetadata["routed_model"])
	if routedModel == "" {
		return nil
	}
	// Python: self._fill_routed_model_in_input(state, routed_model)
	h.fillRoutedModelInInput(state, routedModel)
	return nil
}

// fillRoutedModelInInput 把 span 记录的输入中的路由模型占位符回填为实际路由。
// Python: _fill_routed_model_in_input（callback_handler.py:652-676）——覆盖
// 记录输入的两个位置：输入消息与（动态上下文以 system 文本发送的厂商的）
// system instructions；始终按首次记录的原值回填，每次尝试陈述它实际发往的模型。
func (h *OtelCallbackHandler) fillRoutedModelInInput(state *LlmSpanState, routedModel string) {
	if state.RoutedInputOriginal == nil {
		state.RoutedInputOriginal = map[string]string{}
		for key, value := range map[string]string{
			GenAIInputMessages:      state.initialInputMessagesJSON,
			GenAISystemInstructions: state.initialSystemInstructionsJSON,
		} {
			if value != "" && intellirouter.HasRoutedModelPlaceholder(value) {
				state.RoutedInputOriginal[key] = value
			}
		}
	}
	for key, value := range state.RoutedInputOriginal {
		state.Span.SetAttributes(attribute.String(key, intellirouter.FillRoutedModel(value, routedModel)))
	}
}

// OnLLMStreamCompleted 以累计结果关闭一次自然耗尽的流式调用。
// Python: on_llm_stream_completed（callback_handler.py:749-777）。
func (h *OtelCallbackHandler) OnLLMStreamCompleted(ctx context.Context, data *cb.LLMCallEventData) any {
	defer recoverCallback("on_llm_stream_completed")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return data.Response
	}
	state := spanState.PopLlmSpanState(false)
	if state == nil {
		return data.Response
	}
	h.closeLlmSpan(ctx, state, data.Response)
	return data.Response
}

// OnLLMOutput 应用 provider 输出事实并保留旧式终态事件行为。
// Python: on_llm_output（callback_handler.py:684-748）——经 Model 走统一
// 收尾的调用只增强未关 span；旧式手工开 span 的路径仍视其为终态。
func (h *OtelCallbackHandler) OnLLMOutput(ctx context.Context, data *cb.LLMCallEventData) any {
	defer recoverCallback("on_llm_output")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	state := spanState.PopLlmSpanState(true)
	if state == nil {
		return nil
	}
	if !state.Span.IsRecording() {
		return nil
	}
	// Python: if usage_from_trigger is not None: self._record_usage_attrs(state, usage, skip_existing=True)
	if data.Usage != nil {
		h.recordUsageAttrs(state, data.Usage)
	}
	// Python: if expects_unified_llm_completion(): return
	if llm.ExpectsUnifiedLLMCompletion(ctx) {
		return nil
	}
	// 旧式手工开 span 路径：LLM_OUTPUT 仍为终态。
	state = spanState.PopLlmSpanState(false)
	if state == nil || !state.Span.IsRecording() {
		return nil
	}
	completionText := fmt.Sprintf("%v", data.Response)
	reasoningText := extractReasoningContent(data.Response)
	h.finalizeLlmSpanOutput(ctx, state, completionText, reasoningText, data.Response, data.Usage)
	return nil
}

// OnAgentStreamInput agent stream 调用前：与 invoke_input 同逻辑。
// Python: on_agent_stream_input（callback_handler.py:1052-1054）。
func (h *OtelCallbackHandler) OnAgentStreamInput(ctx context.Context, data *cb.GlobalAgentEventData) any {
	defer recoverCallback("on_agent_stream_input")
	return h.OnAgentInvokeInput(ctx, data)
}

// OnAgentStreamOutput agent stream 输出：与 invoke_output 同逻辑。
// Python: on_agent_stream_output（callback_handler.py:1056-1059）。
func (h *OtelCallbackHandler) OnAgentStreamOutput(ctx context.Context, data *cb.GlobalAgentEventData) any {
	defer recoverCallback("on_agent_stream_output")
	return h.OnAgentInvokeOutput(ctx, data)
}

// OnLLMStreamOutput LLM stream 每个 chunk：记录 per-chunk 事件和 TTFT。
// Python: on_llm_stream_output
func (h *OtelCallbackHandler) OnLLMStreamOutput(ctx context.Context, data *cb.LLMCallEventData) any {
	defer recoverCallback("on_llm_stream_output")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	// Python: state = pop_llm_span_state(peek=True)
	state := spanState.PopLlmSpanState(true)
	if state == nil {
		return nil
	}
	// Python: seq = state.next_chunk_seq()
	seq := state.NextChunkSeq()
	// Python: 首个 chunk 记录 TTFT
	if state.FirstChunkNs == 0 {
		state.FirstChunkNs = time.Now().UnixNano()
		ttftMs := float64(state.FirstChunkNs-state.StartNs) / 1e6
		// Python: state.span.set_attribute(GEN_AI_RESPONSE_TTFT_MS, ttft_ms)
		state.Span.SetAttributes(attribute.Float64(GenAIResponseTTFTMs, ttftMs))
	}
	// Python: delta = _coerce_message_content(_message_content(chunk))
	delta := coerceMessageContent(messageContent(data.Response))
	// Python: state.span.add_event(name="llm.chunk", attributes={"seq": seq, "delta_chars": len(delta)})
	state.Span.AddEvent("llm.chunk", trace.WithAttributes(
		attribute.Int("seq", seq),
		attribute.Int("delta_chars", len(delta)),
	))
	// Python: self._maybe_record_response_attrs(state, chunk)
	h.maybeRecordResponseAttrs(state, data.Response)
	return nil
}

// OnLLMInvokeOutput LLM invoke 调用后：关闭 LLM span，可选生成 reasoning 子 span。
// Python: on_llm_invoke_output
func (h *OtelCallbackHandler) OnLLMInvokeOutput(ctx context.Context, data *cb.LLMCallEventData) any {
	defer recoverCallback("on_llm_invoke_output")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	// Python: state = pop_llm_span_state()
	state := spanState.PopLlmSpanState(false)
	if state == nil {
		return nil
	}
	// Python: response = kwargs.get("result")
	h.closeLlmSpan(ctx, state, data.Response)
	return nil
}

// OnLLMCallError LLM 调用失败：标记 span 为 ERROR 并关闭。
// Python: on_llm_call_error
func (h *OtelCallbackHandler) OnLLMCallError(ctx context.Context, data *cb.LLMCallEventData) any {
	defer recoverCallback("on_llm_call_error")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	// Python: state = pop_llm_span_state()
	state := spanState.PopLlmSpanState(false)
	if state == nil {
		return nil
	}
	// Python: exc = kwargs.get("error") or kwargs.get("exception")
	if data.Error != nil {
		// Python: state.span.record_exception(exc)
		state.Span.RecordError(data.Error)
		// Python: state.span.set_status(Status(StatusCode.ERROR, str(exc)))
		state.Span.SetStatus(codes.Error, data.Error.Error())
	} else {
		// Python: state.span.set_status(Status(StatusCode.ERROR, "llm call error"))
		state.Span.SetStatus(codes.Error, "llm call error")
	}
	// Python: state.span.end()
	state.Span.End()
	// Go 不需要 otel_context.detach（Python: if state.context_token is not None: otel_context.detach）
	return nil
}

// OnToolCallStarted Tool 调用启动：打开 tool span。
// Python: on_tool_call_started
func (h *OtelCallbackHandler) OnToolCallStarted(ctx context.Context, data *cb.ToolCallEventData) any {
	defer recoverCallback("on_tool_call_started")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	// Python: tool_name = str(kwargs.get("tool_name") or "unknown")
	toolName := data.ToolName
	if toolName == "" {
		toolName = "unknown"
	}
	// 创建子 span 时，以当前 LLM span 为 parent
	// Python: span = self._tracer().start_span(name=f"tool.{tool_name}", kind=SpanKind.INTERNAL)
	llmSC := spanState.CurrentLLMSpanContext()
	parentCtx := ctx
	if llmSC.IsValid() {
		parentCtx = trace.ContextWithSpanContext(ctx, llmSC)
	}
	_, toolSpan := h.tracer().Start(parentCtx, "tool."+toolName, trace.WithSpanKind(trace.SpanKindInternal))
	// Python: span.set_attribute(GEN_AI_TOOL_NAME, tool_name)
	toolSpan.SetAttributes(attribute.String(GenAIToolName, toolName))
	// Python: if tool_id is not None: span.set_attribute("gen_ai.tool.id", str(tool_id))
	if data.ToolID != "" {
		toolSpan.SetAttributes(attribute.String(GenAIToolID, data.ToolID))
	}
	// Python: span.set_attribute(GEN_AI_TOOL_INPUT, redact_prompt(self._serialize_tool_inputs(inputs), self._config))
	toolSpan.SetAttributes(attribute.String(GenAIToolInput, RedactPrompt(serializeToolInputs(data.Inputs), h.config)))
	// Python: push_tool_span(tool_name, span)
	spanState.PushToolSpan(toolName, toolSpan)
	return nil
}

// OnToolCallFinished Tool 调用完成：附加 result 并关闭 tool span。
// Python: on_tool_call_finished
func (h *OtelCallbackHandler) OnToolCallFinished(ctx context.Context, data *cb.ToolCallEventData) any {
	defer recoverCallback("on_tool_call_finished")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	// Python: tool_name = str(kwargs.get("tool_name") or "unknown")
	toolName := data.ToolName
	if toolName == "" {
		toolName = "unknown"
	}
	// Python: span = pop_tool_span(tool_name)
	span := spanState.PopToolSpan(toolName)
	if span == nil {
		return nil
	}
	// Python: span.set_attribute(GEN_AI_TOOL_OUTPUT, redact_completion(result, self._config))
	span.SetAttributes(attribute.String(GenAIToolOutput, RedactCompletion(data.Result, h.config)))
	// Python: span.set_status(Status(StatusCode.OK))
	span.SetStatus(codes.Ok, "")
	// Python: span.end()
	span.End()
	return nil
}

// OnToolCallError Tool 调用失败：标记并关闭 tool span。
// Python: on_tool_call_error
func (h *OtelCallbackHandler) OnToolCallError(ctx context.Context, data *cb.ToolCallEventData) any {
	defer recoverCallback("on_tool_call_error")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	toolName := data.ToolName
	if toolName == "" {
		toolName = "unknown"
	}
	// Python: span = pop_tool_span(tool_name)
	span := spanState.PopToolSpan(toolName)
	if span == nil {
		return nil
	}
	// Python: if isinstance(exc, BaseException): ...
	if data.Error != nil {
		span.RecordError(data.Error)
		span.SetStatus(codes.Error, data.Error.Error())
	} else {
		// Python: span.set_status(Status(StatusCode.ERROR, "tool call error"))
		span.SetStatus(codes.Error, "tool call error")
	}
	// Python: span.end()
	span.End()
	return nil
}

// OnAgentInvokeInput Agent 调用前：打开 agent span。
// Python: on_agent_invoke_input
func (h *OtelCallbackHandler) OnAgentInvokeInput(ctx context.Context, data *cb.GlobalAgentEventData) any {
	defer recoverCallback("on_agent_invoke_input")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	// Python: agent_id, role, query = self._unpack_agent_inputs(inputs)
	agentID, role, query := unpackAgentInputs(data.Inputs, data.AgentID)
	// 创建子 span 时，以当前 LLM span 为 parent
	// Python: span = self._tracer().start_span(name=f"agent.{agent_id}", kind=SpanKind.INTERNAL)
	llmSC := spanState.CurrentLLMSpanContext()
	parentCtx := ctx
	if llmSC.IsValid() {
		parentCtx = trace.ContextWithSpanContext(ctx, llmSC)
	}
	_, agentSpan := h.tracer().Start(parentCtx, "agent."+agentID, trace.WithSpanKind(trace.SpanKindInternal))
	// Python: span.set_attribute(AT_AGENT_ID, agent_id)
	agentSpan.SetAttributes(attribute.String(ATAgentID, agentID))
	// Python: if role: span.set_attribute(AT_AGENT_ROLE, role)
	if role != "" {
		agentSpan.SetAttributes(attribute.String(ATAgentRole, role))
	}
	// Python: if query: span.set_attribute(AT_AGENT_INPUT, redact_prompt(query, self._config))
	if query != "" {
		agentSpan.SetAttributes(attribute.String(ATAgentInput, RedactPrompt(query, h.config)))
	}
	// Python: push_agent_span(agent_id, span)
	spanState.PushAgentSpan(agentID, agentSpan)
	return nil
}

// OnAgentInvokeOutput Agent 调用后：关闭 agent span。
// Python: on_agent_invoke_output
func (h *OtelCallbackHandler) OnAgentInvokeOutput(ctx context.Context, data *cb.GlobalAgentEventData) any {
	defer recoverCallback("on_agent_invoke_output")
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	// Python: agent_id, _, _ = self._unpack_agent_inputs(inputs)
	agentID, _, _ := unpackAgentInputs(data.Inputs, data.AgentID)
	// Python: span = pop_agent_span(agent_id)
	span := spanState.PopAgentSpan(agentID)
	if span == nil {
		return nil
	}
	// Python: span.set_attribute(AT_AGENT_OUTPUT, redact_completion(output, self._config))
	span.SetAttributes(attribute.String(ATAgentOutput, RedactCompletion(data.Result, h.config)))
	// Python: span.set_status(Status(StatusCode.OK))
	span.SetStatus(codes.Ok, "")
	// Python: span.end()
	span.End()
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tracer 解析 tracer。如果注入了显式 tracer 则使用，否则懒加载。
// Python: OtelCallbackHandler._tracer()
func (h *OtelCallbackHandler) tracer() trace.Tracer {
	if h.injectedTracer != nil {
		return h.injectedTracer
	}
	return GetTracer(tracerName)
}

// openLlmSpan 共享逻辑：打开 LLM span。
// Python: _open_llm_span(kwargs)
func (h *OtelCallbackHandler) openLlmSpan(ctx context.Context, data *cb.LLMCallEventData, spanState *OtelSpanState) {
	// Python: model_name = kwargs.get("model") or self._derive_model_name(kwargs) or "unknown"
	modelName := data.ModelName
	if modelName == "" {
		modelName = deriveModelName(data.Extra)
	}
	if modelName == "" {
		modelName = "unknown"
	}
	// 创建子 span 时，以当前 LLM span 为 parent（支持嵌套 LLM 调用）
	// Python: span = self._tracer().start_span(name="llm.call", kind=SpanKind.CLIENT)
	llmSC := spanState.CurrentLLMSpanContext()
	parentCtx := ctx
	if llmSC.IsValid() {
		parentCtx = trace.ContextWithSpanContext(ctx, llmSC)
	}
	_, llmSpan := h.tracer().Start(parentCtx, "llm.call", trace.WithSpanKind(trace.SpanKindClient))
	// Python: span.set_attribute(GEN_AI_SYSTEM, _GEN_AI_SYSTEM_VALUE)
	llmSpan.SetAttributes(attribute.String(GenAISystem, genAISystemValue))
	// 开 span 时捕获供后续 fill/commit 使用的属性值（Go 的 trace.Span 接口
	// 不支持属性回读，等价偏差登记 spec §4）。
	var structuredInputJSON, structuredSystemJSON string
	// Python: span.set_attribute(GEN_AI_REQUEST_MODEL, str(model_name))
	llmSpan.SetAttributes(attribute.String(GenAIRequestModel, modelName))
	// Python: temperature / top_p / max_tokens
	if data.Temperature != nil {
		llmSpan.SetAttributes(attribute.Float64(GenAIRequestTemperature, *data.Temperature))
	}
	if data.TopP != nil {
		llmSpan.SetAttributes(attribute.Float64(GenAIRequestTopP, *data.TopP))
	}
	if data.MaxTokens != nil {
		llmSpan.SetAttributes(attribute.Int(GenAIRequestMaxTokens, *data.MaxTokens))
	}
	// Python: 遍历 messages
	// 扁平 gen_ai.prompt.N.* 为 15.11 子集残留（v2 已改写标准结构化属性，
	// 全量删除随 22.4 对齐批回收）。
	normalized := normalizeMessages(data.Messages)
	if len(normalized) > 0 {
		{
			for i, msg := range normalized {
				role := messageRole(msg)
				content := coerceMessageContent(messageContent(msg))
				llmSpan.SetAttributes(
					attribute.String(fmt.Sprintf("%s.%d.role", GenAIPrompt, i), role),
					attribute.String(fmt.Sprintf("%s.%d.content", GenAIPrompt, i), RedactPrompt(content, h.config)),
				)
				// 附件投影条目加 kind/mode 标注（真 system prompt 不标注）
				if kind, mode := attachmentMarkOf(msg); kind != "" {
					llmSpan.SetAttributes(
						attribute.String(fmt.Sprintf("%s.%d.%s", GenAIPrompt, i, GenAIPromptAttachmentKind), kind),
						attribute.String(fmt.Sprintf("%s.%d.%s", GenAIPrompt, i, GenAIPromptAttachmentMode), mode),
					)
				}
			}
		}
		// Python: msg_count = len(messages); span.set_attribute(OJ_REQUEST_MESSAGE_COUNT, msg_count)
		llmSpan.SetAttributes(attribute.Int(OJRequestMessageCount, len(normalized)))
		// Python: self._record_standard_structured_input(span, messages)
		// callback_handler.py:1116 —— 标准结构化输入属性是轨迹消息重放
		// （ReadLLMExchange）的数据源，缺它 TrajectoryToMessages 恒空。
		structuredInputJSON, structuredSystemJSON = h.recordStandardStructuredInput(llmSpan, normalized)
	}
	// Python: call_id = get_current_llm_call_id()
	callID := llm.GetCurrentLLMCallID(ctx)
	if callID != "" {
		llmSpan.SetAttributes(attribute.String(OJRequestID, callID))
	}
	// Python: span.set_attribute(OJ_INFERENCE_ID, f"{span.context.span_id:016x}")
	llmSpan.SetAttributes(attribute.String(OJInferenceID, fmt.Sprintf("%016x", llmSpan.SpanContext().SpanID())))
	// Python: span.set_attribute(GEN_AI_OPERATION_NAME, "chat")
	llmSpan.SetAttributes(attribute.String(GenAIOperationName, "chat"))
	// Python: span.set_attribute(OJ_TRAJECTORY_SCHEMA_VERSION, TRAJECTORY_SPAN_SCHEMA_VERSION)
	llmSpan.SetAttributes(attribute.String(OJTrajectorySchemaVersion, TrajectorySpanSchemaVersion))
	// Python: span.set_attribute(OJ_TRAJECTORY_RECORD_KIND, "inference")
	llmSpan.SetAttributes(attribute.String(OJTrajectoryRecordKind, TrajectoryRecordKindInference))
	// Python: span.set_attribute(GEN_AI_REQUEST_STREAM, is_streaming)
	llmSpan.SetAttributes(attribute.Bool(GenAIRequestStream, data.IsStream))
	// Python: request_purpose 归一（assistant / compaction）
	requestPurpose := "assistant"
	if v, ok := data.Extra["request_purpose"].(string); ok && (v == "assistant" || v == "compaction") {
		requestPurpose = v
	}
	llmSpan.SetAttributes(attribute.String(OJRequestPurpose, requestPurpose))
	// Python: session_id = span.attributes.get(GEN_AI_CONVERSATION_ID) or get_current_session_id()
	if sessionID := GetCurrentSessionID(); sessionID != "" {
		llmSpan.SetAttributes(attribute.String(GenAIConversationID, sessionID))
	}
	// 开 span 时捕获供后续 fill/commit 使用的属性值（Go 的 trace.Span 接口
	// 不支持属性回读，等价偏差登记 spec §4）。
	parentAttributes := map[string]attribute.Value{}
	if sessionID := GetCurrentSessionID(); sessionID != "" {
		parentAttributes[GenAIConversationID] = attribute.StringValue(sessionID)
	}
	// Python: self._record_input_message_provenance(span, messages)
	if len(normalized) > 0 {
		h.recordInputMessageProvenance(llmSpan, normalized)
	}
	if len(normalized) > 0 {
		h.recordInputMessageProvenance(llmSpan, normalized)
	}
	// Python: message_occurrence_ids = self._message_occurrence_ids(messages)
	//         message_metadata = tuple(_get_field(message, "metadata") for message in messages)
	//         initial_trajectory_messages = self._trajectory_messages(messages, ...)
	occurrenceIDs := messageOccurrenceIDs(normalized)
	messageMetadata := make([]map[string]any, 0, len(normalized))
	for _, msg := range normalized {
		if md, ok := msg["metadata"].(map[string]any); ok {
			messageMetadata = append(messageMetadata, md)
		} else {
			messageMetadata = append(messageMetadata, nil)
		}
	}
	initialTrajectoryMessages := h.trajectoryMessages(normalized, occurrenceIDs, messageMetadata)
	// Python: push_llm_span_state(LlmSpanState(span=span, start_ns=time.monotonic_ns(), ...))
	// Go 不需要 context_token（不做 otel_context.attach/detach）
	spanState.PushLlmSpanState(&LlmSpanState{
		Span:                          llmSpan,
		StartNs:                       time.Now().UnixNano(),
		CallID:                        callID,
		IsStreaming:                   data.IsStream,
		RequestPurpose:                requestPurpose,
		MessageOccurrenceIDs:          occurrenceIDs,
		MessageMetadata:               messageMetadata,
		InitialTrajectoryMessages:     initialTrajectoryMessages,
		parentAttributes:              parentAttributes,
		initialInputMessagesJSON:      structuredInputJSON,
		initialSystemInstructionsJSON: structuredSystemJSON,
	})
}

// closeLlmSpan 关闭 LLM span：写入 completion 属性，可选生成 reasoning 子 span。
// Python: _close_llm_span(state, response)
func (h *OtelCallbackHandler) closeLlmSpan(ctx context.Context, state *LlmSpanState, response any) {
	// Python: completion_text = _coerce_message_content(_message_content(response))
	completionText := coerceMessageContent(messageContent(response))
	// Python: reasoning_text = str(getattr(response, "reasoning_content", "") or "")
	reasoningText := extractReasoningContent(response)
	// Python: self._maybe_record_response_attrs(state, response)
	h.maybeRecordResponseAttrs(state, response)
	// Python: self._record_structured_output(state.span, response, fallback_text=completion_text)
	// callback_handler.py:1243 —— gen_ai.output.messages 是轨迹 completion
	// 的数据源；消息对象经 BaseMessageToMap 归一后交由结构化输出记录。
	structuredResponse := response
	if bm, ok := response.(llmschema.BaseMessage); ok {
		structuredResponse = llmschema.BaseMessageToMap(bm)
	}
	h.recordStructuredOutput(state.Span, structuredResponse)
	// Python: state.span.set_attribute(f"{GEN_AI_COMPLETION}.0.role", "assistant")
	state.Span.SetAttributes(
		attribute.String(fmt.Sprintf("%s.0.role", GenAICompletion), "assistant"),
		attribute.String(fmt.Sprintf("%s.0.content", GenAICompletion), RedactCompletion(completionText, h.config)),
	)
	// Python: if reasoning_text: 生成 reasoning 子 span
	if reasoningText != "" {
		// Python: with self._tracer().start_as_current_span(name="llm.reasoning", context=set_span_in_context(state.span))
		// 对齐 Python: 从 state.Span 构建新 context 作为 reasoning span 的 parent
		reasoningCtx := trace.ContextWithSpan(ctx, state.Span)
		_, reasoningSpan := h.tracer().Start(reasoningCtx, "llm.reasoning", trace.WithSpanKind(trace.SpanKindInternal))
		reasoningSpan.SetAttributes(
			attribute.String(fmt.Sprintf("%s.0.role", GenAICompletion), "reasoning"),
			attribute.Bool(fmt.Sprintf("%s.0.is_reasoning", GenAICompletion), true),
			attribute.String(fmt.Sprintf("%s.0.content", GenAICompletion), RedactCompletion(reasoningText, h.config)),
		)
		reasoningSpan.SetStatus(codes.Ok, "")
		reasoningSpan.End()
	}
	// Python: state.span.set_status(Status(StatusCode.OK))
	state.Span.SetStatus(codes.Ok, "")
	// Python: state.span.end()
	state.Span.End()
	// Go 不需要 otel_context.detach
}

// maybeRecordResponseAttrs 写入 usage / finish_reason / model 属性（如果响应中存在）。
// Python: _maybe_record_response_attrs(state, response)
func (h *OtelCallbackHandler) maybeRecordResponseAttrs(state *LlmSpanState, response any) {
	if response == nil {
		return
	}
	// Python: usage = getattr(response, "usage_metadata", None)
	usage := extractUsageMetadata(response)
	if usage != nil {
		// Python: input_tokens → GEN_AI_USAGE_PROMPT_TOKENS
		if usage.InputTokens > 0 {
			state.Span.SetAttributes(attribute.Int(GenAIUsagePromptTokens, usage.InputTokens))
		}
		// Python: output_tokens → GEN_AI_USAGE_COMPLETION_TOKENS
		if usage.OutputTokens > 0 {
			state.Span.SetAttributes(attribute.Int(GenAIUsageCompletionTokens, usage.OutputTokens))
		}
		// Python: total_tokens → GEN_AI_USAGE_TOTAL_TOKENS
		if usage.TotalTokens > 0 {
			state.Span.SetAttributes(attribute.Int(GenAIUsageTotalTokens, usage.TotalTokens))
		}
		// Python: model_name → GEN_AI_RESPONSE_MODEL
		if usage.ModelName != "" {
			state.Span.SetAttributes(attribute.String(GenAIResponseModel, usage.ModelName))
		}
	}
	// Python: finish_reason = getattr(response, "finish_reason", None)
	finishReason := extractFinishReason(response)
	if finishReason != "" && finishReason != "null" {
		state.Span.SetAttributes(attribute.String(GenAIResponseFinishReason, finishReason))
	}
}

// recoverCallback 可观测性回调的异常兜底，保证不中断业务路径。
// Python: except Exception as exc: team_logger.warning("otel: xxx failed: {}", exc)
func recoverCallback(methodName string) {
	if r := recover(); r != nil {
		logger.Warn(logComponent).Any("error", r).Str("method", methodName).Msg("OTel callbackexception")
	}
}

// attachmentMarkOf 返回附件消息的观测标注；非附件消息返回空。
// 上游在新版 semconv 下区分 gen_ai.system_instructions 与 chat history
// （callback_handler.py:1718-1733 / 1915-1930）；本项目使用 OpenLLMetry 的
// gen_ai.prompt.<i>.* 扁平属性，等价做法是在消息条目上加 kind/mode 标注。
func attachmentMarkOf(msg any) (kind string, mode string) {
	if msg == nil {
		return "", ""
	}
	if base, ok := msg.(llmschema.BaseMessage); ok {
		if !hprompts.IsPromptAttachmentHistory(base) {
			return "", ""
		}
		raw, _ := base.GetMetadata()[hprompts.MetadataKeyMode].(string)
		return PromptAttachmentHistoryKind, raw
	}
	// map 形态（llm_params 反序列化后的消息）
	message, ok := msg.(map[string]any)
	if !ok {
		return "", ""
	}
	meta, ok := message["metadata"].(map[string]any)
	if !ok {
		return "", ""
	}
	if marked, _ := meta[hprompts.MetadataKeyAttachmentHistory].(bool); !marked {
		return "", ""
	}
	raw, _ := meta[hprompts.MetadataKeyMode].(string)
	if raw != hprompts.ModeSnapshot && raw != hprompts.ModeDelta {
		return "", ""
	}
	return PromptAttachmentHistoryKind, raw
}

// coerceMessageContent 将 content（string 或 list）强制转换为扁平字符串。
// Python: _coerce_message_content(content)
func coerceMessageContent(content any) string {
	if content == nil {
		return ""
	}
	if s, ok := content.(string); ok {
		return s
	}
	// 尝试 JSON 序列化
	b, err := json.Marshal(content)
	if err != nil {
		return fmt.Sprintf("%v", content)
	}
	return string(b)
}

// messageRole 从消息实例或 map 中提取 role。
// Python: _message_role(msg)
func messageRole(msg any) string {
	if m, ok := msg.(map[string]any); ok {
		if r, ok := m["role"]; ok {
			return fmt.Sprintf("%v", r)
		}
		return ""
	}
	// llmschema 消息：GetRole() RoleType（15.24a 集成首跑实证缺陷——
	// 旧断言 GetRole() string 永不匹配，角色恒空）。
	if r, ok := msg.(llmschema.BaseMessage); ok {
		return r.GetRole().String()
	}
	// 兜底：其他 GetRole() string 载体。
	type roleHolder interface{ GetRole() string }
	if r, ok := msg.(roleHolder); ok {
		return r.GetRole()
	}
	return ""
}

// messageContent 从消息实例或 map 中提取 content。
// Python: _message_content(msg)
func messageContent(msg any) any {
	if msg == nil {
		return nil
	}
	if m, ok := msg.(map[string]any); ok {
		return m["content"]
	}
	// llmschema 消息：GetContent() MessageContent（同上实证缺陷）。
	if m, ok := msg.(llmschema.BaseMessage); ok {
		return m.GetContent().String()
	}
	type contentHolder interface{ GetContent() any }
	if c, ok := msg.(contentHolder); ok {
		return c.GetContent()
	}
	return nil
}

// toMessageSlice 将 any 类型的 messages 转换为 []any 切片。
func toMessageSlice(messages any) ([]any, bool) {
	if m, ok := messages.([]any); ok {
		return m, true
	}
	// MessagesParam（model_clients 的消息载体，15.24a 集成首跑实证缺陷）：
	// 事件经 LLMStreamInput/LLMInvokeInput 携带的是本类型，旧实现只认 []any
	// 导致 prompt 属性恒空、轨迹重放不出消息。Python 侧 messages 为 dict/list
	// 直读，无此缝。三种模式归一为 []any。
	if mp, ok := messages.(model_clients.MessagesParam); ok {
		if mp.IsText() {
			// Python str 输入被 SDK 包成一条 UserMessage；此处取语义等价投影。
			return []any{map[string]any{"role": "user", "content": mp.Text()}}, true
		}
		if mp.IsMessages() {
			out := make([]any, 0, len(mp.Messages()))
			for _, m := range mp.Messages() {
				out = append(out, m)
			}
			return out, true
		}
		if dicts := mp.Dicts(); len(dicts) > 0 {
			out := make([]any, 0, len(dicts))
			for _, d := range dicts {
				out = append(out, d)
			}
			return out, true
		}
	}
	// BaseMessage 列表（个别直传路径）
	if m, ok := messages.([]llmschema.BaseMessage); ok {
		out := make([]any, 0, len(m))
		for _, item := range m {
			out = append(out, item)
		}
		return out, true
	}
	return nil, false
}

// extractUsageMetadata 从响应中提取 UsageMetadata。
// Python: usage = getattr(response, "usage_metadata", None)
func extractUsageMetadata(response any) *llmschema.UsageMetadata {
	if response == nil {
		return nil
	}
	if u, ok := response.(*llmschema.UsageMetadata); ok {
		return u
	}
	// 尝试通过接口提取
	type usageHolder interface {
		GetUsageMetadata() *llmschema.UsageMetadata
	}
	if u, ok := response.(usageHolder); ok {
		return u.GetUsageMetadata()
	}
	return nil
}

// extractFinishReason 从响应中提取 finish_reason。
// Python: finish_reason = getattr(response, "finish_reason", None)
func extractFinishReason(response any) string {
	if response == nil {
		return ""
	}
	type finishReasonHolder interface{ GetFinishReason() string }
	if fr, ok := response.(finishReasonHolder); ok {
		return fr.GetFinishReason()
	}
	// 对 map 类型
	if m, ok := response.(map[string]any); ok {
		if fr, ok := m["finish_reason"]; ok {
			return fmt.Sprintf("%v", fr)
		}
	}
	return ""
}

// extractReasoningContent 从响应中提取 reasoning_content。
// Python: reasoning_text = str(getattr(response, "reasoning_content", "") or "")
func extractReasoningContent(response any) string {
	if response == nil {
		return ""
	}
	type reasoningHolder interface{ GetReasoningContent() string }
	if r, ok := response.(reasoningHolder); ok {
		rc := r.GetReasoningContent()
		if rc != "" {
			return rc
		}
	}
	if m, ok := response.(map[string]any); ok {
		if rc, ok := m["reasoning_content"]; ok {
			if s, ok := rc.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// deriveModelName 从 extra 数据中推导模型名称。
// Python: _derive_model_name(kwargs)
func deriveModelName(extra map[string]any) string {
	if extra == nil {
		return ""
	}
	if mc, ok := extra["model_config"]; ok {
		type modelConfigHolder interface{ GetModel() string }
		if mch, ok := mc.(modelConfigHolder); ok {
			name := mch.GetModel()
			if name != "" {
				return name
			}
		}
		if m, ok := mc.(map[string]any); ok {
			if name, ok := m["model"]; ok {
				if s, ok := name.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// serializeToolInputs 渲染 tool inputs 为 JSON 字符串。
// Python: _serialize_tool_inputs(inputs)
func serializeToolInputs(inputs any) string {
	if inputs == nil {
		return ""
	}
	b, err := json.Marshal(inputs)
	if err != nil {
		return fmt.Sprintf("%v", inputs)
	}
	return string(b)
}

// unpackAgentInputs 从异构的 inputs payload 中提取 (agentID, role, query)。
// Python: _unpack_agent_inputs(inputs)
func unpackAgentInputs(inputs map[string]any, fallbackAgentID string) (string, string, string) {
	if inputs == nil {
		if fallbackAgentID != "" {
			return fallbackAgentID, "", ""
		}
		return "unknown", "", ""
	}
	// Python: agent_id = str(inputs.get("agent_id") or inputs.get("session_id") or "unknown")
	agentID := ""
	if a, ok := inputs["agent_id"]; ok && a != nil {
		agentID = fmt.Sprintf("%v", a)
	}
	if agentID == "" {
		if s, ok := inputs["session_id"]; ok && s != nil {
			agentID = fmt.Sprintf("%v", s)
		}
	}
	if agentID == "" {
		agentID = fallbackAgentID
	}
	if agentID == "" {
		agentID = "unknown"
	}
	// Python: role = str(inputs.get("role", "") or "")
	role := ""
	if r, ok := inputs["role"]; ok && r != nil {
		role = fmt.Sprintf("%v", r)
	}
	// Python: query = inputs.get("user_input") or inputs.get("query") or ""
	query := ""
	if q, ok := inputs["user_input"]; ok && q != nil {
		query = fmt.Sprintf("%v", q)
	}
	if query == "" {
		if q, ok := inputs["query"]; ok && q != nil {
			query = fmt.Sprintf("%v", q)
		}
	}
	return agentID, role, query
}

// ──────────────────────────── 15.11 三方法与结构化属性链子集（⤴️22.4） ────────────────────────────

// ContextWindowMessages 返回上下文窗口 commit 所陈述的规范形态消息。
// 与模型请求的 commit 使用同一套身份与脱敏规则，因此直接从上下文引擎
// 陈述的窗口（压缩后等）能接入下一次请求的 commit 延续链：
// 出现 id 来自每条消息的 context_message_id。
// Python: callback_handler.py:399-417 context_window_messages
func (h *OtelCallbackHandler) ContextWindowMessages(messages any) []map[string]any {
	return h.trajectoryMessages(messages, messageOccurrenceIDs(messages), nil)
}

// RecordRequestInput 在推理 span 上记录一次模型请求的消息。
// 写入框架模型调用同款的两个标准输入属性（gen_ai.system_instructions /
// gen_ai.input.messages、消息来源与计数），供在框架模型客户端之外
// 观测请求的宿主（如第三方 harness）使用。
// Python: callback_handler.py:419-434 record_request_input
func (h *OtelCallbackHandler) RecordRequestInput(span trace.Span, messages any) {
	normalized := normalizeMessages(messages)
	span.SetAttributes(attribute.Int(OJRequestMessageCount, len(normalized)))
	h.recordInputMessageProvenance(span, normalized)
	h.recordStandardStructuredInput(span, normalized)
}

// RecordResponseOutput 以 gen_ai.output.messages 记录模型回复。
// Python: callback_handler.py:436-445 record_response_output
func (h *OtelCallbackHandler) RecordResponseOutput(span trace.Span, message any) {
	h.recordStructuredOutput(span, message)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeMessages 消息形参归一为 map 列表。
// 偏差 D-15.11-g：trajectory.py 整形产物恒为 map[string]any，
// Python 的 getattr 分支（对象消息）在 Go 侧不可达，仅保留 map 访问。
// Python: callback_handler.py:1853-1861 _normalize_messages
func normalizeMessages(messages any) []map[string]any {
	if messages == nil {
		return nil
	}
	switch v := messages.(type) {
	case string:
		return []map[string]any{{"role": "user", "content": v}}
	case []map[string]any:
		return v
	case []llmschema.BaseMessage:
		// BaseMessage 列表（模型客户端直传路径）：归一为 dict 形态，
		// role/content 经 BaseMessageToMap 保留（Python 对象消息的 getattr
		// 分支等价——15.24a 集成首跑实证该分支可达）。
		result := make([]map[string]any, 0, len(v))
		for _, item := range v {
			result = append(result, llmschema.BaseMessageToMap(item))
		}
		return result
	case []any:
		result := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			} else if bm, ok := item.(llmschema.BaseMessage); ok {
				result = append(result, llmschema.BaseMessageToMap(bm))
			} else {
				result = append(result, map[string]any{"value": item})
			}
		}
		return result
	default:
		return []map[string]any{{"value": messages}}
	}
}

// messageFieldOf 取 map 消息字段（缺失返 nil）。
// Python: callback_handler.py:246-250 _get_field（dict 分支）
func messageFieldOf(message map[string]any, name string) any {
	value, _ := message[name]
	return value
}

// messageRoleOf 取消息角色。
// Python: callback_handler.py:216-220 _message_role
func messageRoleOf(message map[string]any) string {
	role, _ := message["role"].(string)
	return role
}

// messageContentOf 取消息内容。
// Python: callback_handler.py:222-226 _message_content
func messageContentOf(message map[string]any) any {
	value, ok := message["content"]
	if !ok {
		return ""
	}
	return value
}

// controlledString 有界字符串兜底，不触发用户代码异常。
// Python: callback_handler.py:304-313 _controlled_string（Go 值均为内置类型，无自定义 __str__ 面）
func controlledString(value any) string {
	rendered := fmt.Sprintf("%v", value)
	runes := []rune(rendered)
	if len(runes) > 1024 {
		return string(runes[:1024])
	}
	return rendered
}

// jsonCompatible 归一模型/工具 schema 值且不泄漏用户异常。
// 偏差 D-15.11-g：Go 侧输入恒为 JSON 可表达类型（map/切片/标量），
// Python 的 pydantic/dataclass/Enum 分支不可达。
// Python: callback_handler.py:315-383 _json_compatible
func jsonCompatible(value any, depth int, seen map[uintptr]bool) any {
	if depth > 20 {
		return fmt.Sprintf("<max-depth:%T>", value)
	}
	switch v := value.(type) {
	case nil, string, bool, int, int64, float64, uint64:
		return v
	case int32, uint32, uint, uint8, int8, int16, uint16, float32:
		return v
	case map[string]any:
		key := reflect.ValueOf(v).Pointer()
		if seen[key] {
			return fmt.Sprintf("<recursive:%T>", value)
		}
		seen[key] = true
		defer delete(seen, key)
		normalized := make(map[string]any, len(v))
		for k, item := range v {
			normalized[controlledString(k)] = jsonCompatible(item, depth+1, seen)
		}
		return normalized
	case []any:
		key := reflect.ValueOf(v).Pointer()
		if seen[key] {
			return fmt.Sprintf("<recursive:%T>", value)
		}
		seen[key] = true
		defer delete(seen, key)
		result := make([]any, 0, len(v))
		for _, item := range v {
			result = append(result, jsonCompatible(item, depth+1, seen))
		}
		return result
	default:
		return controlledString(value)
	}
}

// jsonValueIfUnchanged 脱敏/截断未改变原文时保留 JSON 结构。
// Python: callback_handler.py:294-302 _json_value_if_unchanged
func jsonValueIfUnchanged(raw, redacted string) any {
	if redacted != raw {
		return redacted
	}
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		return parsed
	}
	return redacted
}

// trajectoryMessageOrigin 返回显式来源事实，不窥探消息内容。
// Python: callback_handler.py:261-292 _trajectory_message_origin
func trajectoryMessageOrigin(message map[string]any, sourceMetadata map[string]any) map[string]string {
	metadata, _ := message["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	origin := ""
	if v, ok := metadata[llmschema.OpenjiuwenMessageOriginMetadata].(string); ok && v != "" {
		origin = v
	} else if sourceMetadata != nil {
		if v, ok := sourceMetadata[llmschema.OpenjiuwenMessageOriginMetadata].(string); ok {
			origin = v
		}
	}
	externalUser := messageRoleOf(message) == "user" &&
		origin == llmschema.OpenjiuwenMessageOriginExternalUser
	result := map[string]string{
		"origin": llmschema.OpenjiuwenMessageOriginHarnessInternal,
	}
	if externalUser {
		result["origin"] = llmschema.OpenjiuwenMessageOriginExternalUser
	}
	sourceKind := ""
	if v, ok := metadata[llmschema.OpenjiuwenMessageSourceKindMetadata].(string); ok {
		sourceKind = v
	} else if sourceMetadata != nil {
		if v, ok := sourceMetadata[llmschema.OpenjiuwenMessageSourceKindMetadata].(string); ok {
			sourceKind = v
		}
	}
	if externalUser && strings.TrimSpace(sourceKind) != "" {
		result["source_kind"] = strings.TrimSpace(sourceKind)
	}
	return result
}

// messageOccurrenceIDs 计算每条消息的出现 id。
// Python: callback_handler.py:1628-1652 _message_occurrence_ids
func messageOccurrenceIDs(messages any) []string {
	normalized := normalizeMessages(messages)
	seen := make(map[string]int, len(normalized))
	occurrenceIDs := make([]string, 0, len(normalized))
	systemSlot := 0
	for _, message := range normalized {
		metadata, _ := message["metadata"].(map[string]any)
		explicit := ""
		if metadata != nil {
			explicit, _ = metadata["context_message_id"].(string)
		}
		if explicit == "" {
			if v, ok := message["message_id"].(string); ok && v != "" {
				explicit = v
			} else if v, ok := message["id"].(string); ok && v != "" {
				explicit = v
			}
		}
		if explicit == "" && metadata != nil {
			if v, ok := metadata["message_id"].(string); ok && v != "" {
				explicit = v
			} else if v, ok := metadata["openjiuwen.message_id"].(string); ok && v != "" {
				explicit = v
			}
		}
		if explicit == "" && messageRoleOf(message) == "system" {
			explicit = fmt.Sprintf("%s%d", RequestSystemSlotPrefix, systemSlot)
			systemSlot++
		}
		if explicit != "" {
			duplicateIndex := seen[explicit]
			seen[explicit] = duplicateIndex + 1
			if duplicateIndex == 0 {
				occurrenceIDs = append(occurrenceIDs, explicit)
			} else {
				occurrenceIDs = append(occurrenceIDs, fmt.Sprintf("%s#occurrence:%d", explicit, duplicateIndex))
			}
		} else {
			occurrenceIDs = append(occurrenceIDs, hexUUID())
		}
	}
	return occurrenceIDs
}

// trajectoryValue 递归脱敏一个轨迹值（system_prompt 时用系统提示词保护）。
// Python: callback_handler.py:1654-1669 _trajectory_value
func (h *OtelCallbackHandler) trajectoryValue(value any, systemPrompt bool) any {
	normalized := jsonCompatible(value, 0, map[uintptr]bool{})
	if text, ok := normalized.(string); ok {
		if systemPrompt {
			return RedactSystemPrompt(text, h.config)
		}
		return RedactPrompt(text, h.config)
	}
	if list, ok := normalized.([]any); ok {
		result := make([]any, 0, len(list))
		for _, item := range list {
			result = append(result, h.trajectoryValue(item, systemPrompt))
		}
		return result
	}
	if m, ok := normalized.(map[string]any); ok {
		result := make(map[string]any, len(m))
		for key, item := range m {
			result[key] = h.trajectoryValue(item, systemPrompt)
		}
		return result
	}
	return normalized
}

// isPromptAttachmentHistory 判断该 system 消息是否注入的动态上下文。
// prompt-attachment 快照与增量随运行写进对话，属聊天历史而非随行指令。
// Python: callback_handler.py:1716-1729 _is_prompt_attachment_history
func isPromptAttachmentHistory(message map[string]any) bool {
	metadata, _ := message["metadata"].(map[string]any)
	if metadata == nil {
		return false
	}
	flag, _ := metadata["_openjiuwen_prompt_attachment_history"].(bool)
	return flag
}

// trajectoryMessages 把消息转为规范轨迹消息列表。
// Python: callback_handler.py:1671-1714 _trajectory_messages
func (h *OtelCallbackHandler) trajectoryMessages(messages any, occurrenceIDs []string, sourceMetadata []map[string]any) []map[string]any {
	normalized := normalizeMessages(messages)
	if len(occurrenceIDs) != len(normalized) {
		occurrenceIDs = messageOccurrenceIDs(normalized)
	}
	result := make([]map[string]any, 0, len(normalized))
	for index, message := range normalized {
		role := messageRoleOf(message)
		systemPrompt := role == "system" && !isPromptAttachmentHistory(message)
		var sourceMessageMetadata map[string]any
		if index < len(sourceMetadata) {
			sourceMessageMetadata = sourceMetadata[index]
		}
		item := map[string]any{
			"message_id": occurrenceIDs[index],
			"role":       role,
			"content":    h.trajectoryValue(messageContentOf(message), systemPrompt),
		}
		for key, value := range trajectoryMessageOrigin(message, sourceMessageMetadata) {
			item[key] = value
		}
		for _, key := range []string{"tool_calls", "tool_call_id", "name"} {
			value := messageFieldOf(message, key)
			if value == nil || value == "" {
				continue
			}
			item[key] = h.trajectoryValue(value, false)
		}
		metadataValue := messageFieldOf(message, "metadata")
		if metadataValue == nil && sourceMessageMetadata != nil {
			metadataValue = sourceMessageMetadata
		}
		if metadataValue != nil {
			item["metadata"] = h.trajectoryValue(metadataValue, false)
		}
		result = append(result, item)
	}
	return result
}

// recordStandardStructuredInput 以两个标准输入属性记录请求。
// gen_ai.system_instructions 定义为对话历史之外单独提供的指令（稳定的
// system prompt）；注入对话的 system 轮（prompt-attachment history）保留在
// gen_ai.input.messages 的 system 条目里，其边界与历史标记得以存活。
// Python: callback_handler.py:1730-1766 _record_standard_structured_input
// recordStandardStructuredInput 记录标准结构化输入属性并返回两个属性的原文
// （gen_ai.input.messages / gen_ai.system_instructions），供路由占位符回填
// 捕获首记值（Python 惰性从 span.attributes 读，Go 的 trace.Span 接口不支持
// 属性回读，改为返回值捕获——偏差登记 spec §4）。
func (h *OtelCallbackHandler) recordStandardStructuredInput(span trace.Span, messages []map[string]any) (inputMessagesJSON, systemInstructionsJSON string) {
	var systemParts []map[string]any
	var inputMessages []map[string]any
	for _, message := range messages {
		role := messageRoleOf(message)
		if role == "system" && !isPromptAttachmentHistory(message) {
			structured := h.structuredContentParts(messageContentOf(message), redactSystemPromptFn(h.config))
			systemParts = append(systemParts, structured...)
			continue
		}
		inputMessages = append(inputMessages, h.structuredMessage(message, false))
	}
	if len(systemParts) > 0 {
		systemInstructionsJSON = marshalJSONString(systemParts)
		span.SetAttributes(attribute.String(GenAISystemInstructions, systemInstructionsJSON))
	}
	if len(inputMessages) > 0 {
		inputMessagesJSON = marshalJSONString(inputMessages)
		span.SetAttributes(attribute.String(GenAIInputMessages, inputMessagesJSON))
	}
	return inputMessagesJSON, systemInstructionsJSON
}

// recordInputMessageProvenance 记录输入消息来源（prompt 附件等）。
// Python: callback_handler.py:1767-1817 _record_input_message_provenance
func (h *OtelCallbackHandler) recordInputMessageProvenance(span trace.Span, messages []map[string]any) {
	var provenanceEntries []map[string]any
	inputMessageIndex := 0
	for requestMessageIndex, message := range messages {
		if messageRoleOf(message) == "system" {
			continue
		}
		metadata, _ := message["metadata"].(map[string]any)
		var provenance map[string]any
		if metadata != nil {
			provenance, _ = metadata[llmschema.OpenjiuwenMessageProvenanceMetadata].(map[string]any)
		}
		kind, _ := provenance["kind"].(string)
		scope, _ := provenance["scope"].(string)
		if provenance != nil && kind == "prompt_attachment" && scope == "request" {
			var items []map[string]any
			if rawItems, ok := provenance["items"].([]any); ok {
				for _, rawItem := range rawItems {
					item, ok := rawItem.(map[string]any)
					if !ok {
						continue
					}
					entry := map[string]any{}
					for _, key := range []string{"id", "section", "kind", "source"} {
						value, present := item[key]
						if !present {
							continue
						}
						if s, ok := value.(string); ok {
							entry[key] = s
						} else if key == "source" && value == nil {
							entry[key] = nil
						}
					}
					if priority, ok := item["priority"].(float64); ok {
						entry["priority"] = int(priority)
					} else if priority, ok := item["priority"].(int); ok {
						entry["priority"] = priority
					}
					items = append(items, entry)
				}
			}
			provenanceEntries = append(provenanceEntries, map[string]any{
				"request_message_index": requestMessageIndex,
				"input_message_index":   inputMessageIndex,
				"kind":                  "prompt_attachment",
				"scope":                 "request",
				"items":                 items,
			})
		}
		inputMessageIndex++
	}
	if len(provenanceEntries) > 0 {
		span.SetAttributes(attribute.String(OJGenAIInputMessageProvenance, marshalJSONString(provenanceEntries)))
	}
}

// recordStructuredOutput 以标准输出属性记录回复。
// response 为裸文本时由 fallbackText 承载回复（本调用面不传）。
// Python: callback_handler.py:1818-1852 _record_structured_output
func (h *OtelCallbackHandler) recordStructuredOutput(span trace.Span, response any) {
	if response == nil {
		return
	}
	// 偏差 D-15.11-g：本调用面（trajectory.go 的 messageDicts 产物）恒为 map，
	// Python 的对象消息分支在 Go 侧不可达，非 map 视为无内容返回
	message, ok := response.(map[string]any)
	if !ok {
		return
	}
	structured := h.structuredMessage(message, true)
	hasText := false
	parts, _ := structured["parts"].([]map[string]any)
	for _, part := range parts {
		if content, _ := part["content"].(string); content != "" {
			hasText = true
			break
		}
	}
	if !hasText {
		return
	}
	span.SetAttributes(attribute.String(GenAIOutputMessages, marshalJSONString([]map[string]any{structured})))
}

// structuredMessage 把一条消息归一为 (role, parts) 结构。
// Python: callback_handler.py:1862-1933 _structured_message
func (h *OtelCallbackHandler) structuredMessage(message map[string]any, isOutput bool) map[string]any {
	role := messageRoleOf(message)
	if role == "" {
		if isOutput {
			role = "assistant"
		} else {
			role = "user"
		}
	}
	var redact func(string) string
	if isOutput {
		redact = redactCompletionFn(h.config)
	} else {
		redact = redactPromptFn(h.config)
	}
	var parts []map[string]any

	// 推理内容
	if reasoning := messageFieldOf(message, "reasoning_content"); reasoning != nil && reasoning != "" {
		parts = append(parts, map[string]any{
			"type":    "reasoning",
			"content": redact(coerceMessageContent(reasoning)),
		})
	}

	content := messageContentOf(message)
	rawContent := coerceMessageContent(content)
	if rawContent != "" {
		if role == "tool" {
			redactedContent := redact(rawContent)
			responseValue := jsonValueIfUnchanged(rawContent, redactedContent)
			part := map[string]any{
				"type":     "tool_call_response",
				"response": responseValue,
			}
			if toolCallID, _ := messageFieldOf(message, "tool_call_id").(string); toolCallID != "" {
				part["id"] = toolCallID
			}
			if name, _ := messageFieldOf(message, "name").(string); name != "" {
				part["name"] = name
			}
			parts = append(parts, part)
		} else {
			parts = append(parts, h.structuredContentParts(content, redact)...)
		}
	}

	// 工具调用（OpenAI 形状把 name/arguments 嵌在 function 下；原生的直接携带）
	if rawCalls := messageFieldOf(message, "tool_calls"); rawCalls != nil {
		calls, _ := rawCalls.([]any)
		for _, rawCall := range calls {
			call, ok := rawCall.(map[string]any)
			if !ok {
				call = map[string]any{}
			}
			function, _ := call["function"].(map[string]any)
			var arguments any
			if call["arguments"] != nil {
				arguments = call["arguments"]
			} else if function != nil {
				arguments = function["arguments"]
			}
			rawArguments := coerceMessageContent(arguments)
			redactedArguments := redact(rawArguments)
			part := map[string]any{
				"type":      "tool_call",
				"arguments": jsonValueIfUnchanged(rawArguments, redactedArguments),
			}
			if toolID, _ := call["id"].(string); toolID != "" {
				part["id"] = toolID
			}
			toolName := ""
			if name, _ := call["name"].(string); name != "" {
				toolName = name
			} else if function != nil {
				toolName, _ = function["name"].(string)
			}
			if toolName != "" {
				part["name"] = toolName
			}
			parts = append(parts, part)
		}
	}

	structured := map[string]any{"role": role, "parts": parts}
	// prompt-attachment history 的 system 输出消息标注 openjiuwen 事实
	if !isOutput && role == "system" {
		if metadata, ok := message["metadata"].(map[string]any); ok {
			if flag, _ := metadata["_openjiuwen_prompt_attachment_history"].(bool); flag {
				switch historyMode := metadata["mode"].(type) {
				case string:
					if historyMode == "snapshot" || historyMode == "delta" {
						structured["openjiuwen"] = map[string]any{
							"kind": "prompt_attachment_history",
							"mode": historyMode,
						}
					}
				}
			}
		}
	}
	if name, _ := messageFieldOf(message, "name").(string); name != "" {
		structured["name"] = name
	}
	return structured
}

// structuredContentParts 归一有序的字符串/provider 内容且不丢载荷。
// 文本形状部件暴露可读 content；其余 dict 序列化为脱敏 JSON content，
// 图像/文档特定字段即便 UI 不认识该类型也存活在原始属性里。
// Python: callback_handler.py:1934-2007 _structured_content_parts
func (h *OtelCallbackHandler) structuredContentParts(content any, redact func(string) string) []map[string]any {
	if content == nil {
		return nil
	}
	var items []any
	switch v := content.(type) {
	case []any:
		items = v
	default:
		items = []any{content}
	}
	parts := make([]map[string]any, 0, len(items))
	for _, rawItem := range items {
		if text, ok := rawItem.(string); ok {
			parts = append(parts, map[string]any{"type": "text", "content": redact(text)})
			continue
		}
		item, ok := rawItem.(map[string]any)
		if !ok {
			parts = append(parts, map[string]any{"type": "unknown", "content": redact(coerceMessageContent(rawItem))})
			continue
		}
		partType := "unknown"
		if t, ok := item["type"].(string); ok && t != "" {
			partType = t
		}
		textValue := item["text"]
		if textValue == nil {
			textValue = item["content"]
		}
		isTextShape := false
		for _, shape := range []string{"text", "input_text", "output_text"} {
			if partType == shape {
				isTextShape = true
				break
			}
		}
		if isTextShape {
			if text, ok := textValue.(string); ok {
				part := map[string]any{"type": partType, "content": redact(text)}
				extras := map[string]any{}
				for key, value := range item {
					if key == "type" || key == "text" || key == "content" {
						continue
					}
					extras[key] = value
				}
				if len(extras) > 0 {
					rawExtras := marshalJSONString(extras)
					part["metadata"] = jsonValueIfUnchanged(rawExtras, redact(rawExtras))
				}
				parts = append(parts, part)
				continue
			}
		}
		payload := map[string]any{}
		for key, value := range item {
			if key == "type" {
				continue
			}
			payload[key] = value
		}
		part := map[string]any{"type": partType, "content": redact(marshalJSONString(payload))}
		// 保留 profile 识别的标量身份字段（独立脱敏，防旁路上面的 JSON content 保护）
		for _, key := range []string{"id", "name", "modality", "file_id", "uri"} {
			if value, ok := item[key].(string); ok && value != "" {
				part[key] = redact(value)
			}
		}
		parts = append(parts, part)
	}
	return parts
}

// redactPromptFn / redactCompletionFn / redactSystemPromptFn 供结构化链使用的脱敏闭包。
// Python 侧无对应实现：Python 直接传函数对象，Go 以闭包等价
func redactPromptFn(config *ObservabilityConfig) func(string) string {
	return func(text string) string { return RedactPrompt(text, config) }
}

func redactCompletionFn(config *ObservabilityConfig) func(string) string {
	return func(text string) string { return RedactCompletion(text, config) }
}

func redactSystemPromptFn(config *ObservabilityConfig) func(string) string {
	return func(text string) string { return RedactSystemPrompt(text, config) }
}

// marshalJSONString JSON 序列化（ensure_ascii=False 语义：Go 默认不转义非 ASCII）。
// Python: json.dumps(value, ensure_ascii=False, default=str)
func marshalJSONString(value any) string {
	// Python: json.dumps(value, ensure_ascii=False)——< > & 不转义为 \u00XX，
	// 路由占位符 <routed-model/> 才能以原文进属性并被 fill_routed_model 匹配
	//（15.24a 集成实证：json.Marshal 的 HTML 转义让占位符匹配恒失败）。
	data, err := utils.MarshalNoEscape(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

// recordUsageAttrs 记录 provider 用量属性（usage tokens / 缓存计数）。
// Python: _record_usage_attrs（callback_handler.py:1353-1396）。
// skip_existing 语义（Python 按已写属性跳过）无法在 Go 的 trace.Span 接口上
// 实现（不支持属性回读），Go 恒为覆盖写——重复记录的是同一事实，等价偏差
// 登记 spec §4。
func (h *OtelCallbackHandler) recordUsageAttrs(state *LlmSpanState, usage *llmschema.UsageMetadata) {
	if usage == nil {
		return
	}
	state.Span.SetAttributes(
		attribute.Int(GenAIUsagePromptTokens, usage.InputTokens),
		attribute.Int(GenAIUsageCompletionTokens, usage.OutputTokens),
	)
	if usage.TotalTokens > 0 {
		state.Span.SetAttributes(attribute.Int(GenAIUsageTotalTokens, usage.TotalTokens))
	}
}

// cloneMessageMaps 深拷贝消息 map 列表（Python dict(item) 逐条浅拷贝等价）。
func cloneMessageMaps(messages []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		copied := make(map[string]any, len(m))
		for k, v := range m {
			copied[k] = v
		}
		out = append(out, copied)
	}
	return out
}

// trimmedString 取 map 值的去除空白字符串形态（Python str(v or "").strip()）。
func trimmedString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// finalizeLlmSpanOutput 输出收尾共享路径。
// Python: _finalize_llm_span_output（callback_handler.py:1230-1295）——由
// _close_llm_span（非流式）与 on_llm_output（流式终态）共用：结构化输出、
// 响应详情、总时延，随后主 llm span 恒被关闭（即使写属性出错也不成孤儿）；
// reasoning 子 span 在主 span 安全关闭后尽力创建。
func (h *OtelCallbackHandler) finalizeLlmSpanOutput(ctx context.Context, state *LlmSpanState, completionText, reasoningText string, response any, usage *llmschema.UsageMetadata) {
	if usage != nil {
		// Python: self._record_usage_attrs(state, usage, skip_existing=False)
		h.recordUsageAttrs(state, usage)
	}
	// Python: total_latency_ms = (time.monotonic_ns() - state.start_ns) / 1_000_000.0
	state.Span.SetAttributes(attribute.Float64(OJGenAIResponseTotalLatencyMs,
		float64(time.Now().UnixNano()-state.StartNs)/1_000_000.0))
	// 复用 closeLlmSpan：结构化输出 + completion 索引 + reasoning 子 span + OK 终态 + End。
	h.closeLlmSpan(ctx, state, response)
	_ = completionText
	_ = reasoningText
}
