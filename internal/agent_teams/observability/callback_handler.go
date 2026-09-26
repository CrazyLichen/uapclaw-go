package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// tracerName OTel tracer 名称
	// Python: _TRACER_NAME = "openjiuwen.agent_teams.observability"
	tracerName = "openjiuwen.agent_teams.observability"
	// genAISystemValue gen_ai.system 属性值
	// Python: _GEN_AI_SYSTEM_VALUE = "openjiuwen"
	genAISystemValue = "openjiuwen"
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
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

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOtelCallbackHandler 创建 OtelCallbackHandler。
// Python: OtelCallbackHandler(config, tracer=...)
func NewOtelCallbackHandler(config *ObservabilityConfig, tracer trace.Tracer) *OtelCallbackHandler {
	return &OtelCallbackHandler{
		config:         config,
		injectedTracer: tracer,
	}
}

// ──────────────────────────── LLM 回调 ────────────────────────────

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
	h.closeLlmSpan(state, data.Response)
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

// ──────────────────────────── Tool 回调 ────────────────────────────

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
	parentCtx := trace.ContextWithSpanContext(ctx, spanState.CurrentLLMSpanContext())
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

// ──────────────────────────── Agent 回调 ────────────────────────────

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
	parentCtx := trace.ContextWithSpanContext(ctx, spanState.CurrentLLMSpanContext())
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
	parentCtx := trace.ContextWithSpanContext(ctx, spanState.CurrentLLMSpanContext())
	_, llmSpan := h.tracer().Start(parentCtx, "llm.call", trace.WithSpanKind(trace.SpanKindClient))
	// Python: span.set_attribute(GEN_AI_SYSTEM, _GEN_AI_SYSTEM_VALUE)
	llmSpan.SetAttributes(attribute.String(GenAISystem, genAISystemValue))
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
	if data.Messages != nil {
		if msgSlice, ok := toMessageSlice(data.Messages); ok {
			for i, msg := range msgSlice {
				role := messageRole(msg)
				content := coerceMessageContent(messageContent(msg))
				llmSpan.SetAttributes(
					attribute.String(fmt.Sprintf("%s.%d.role", GenAIPrompt, i), role),
					attribute.String(fmt.Sprintf("%s.%d.content", GenAIPrompt, i), RedactPrompt(content, h.config)),
				)
			}
		}
	}
	// Python: push_llm_span_state(LlmSpanState(span=span, start_ns=time.monotonic_ns()))
	// Go 不需要 context_token（不做 otel_context.attach/detach）
	spanState.PushLlmSpanState(&LlmSpanState{
		Span:    llmSpan,
		StartNs: time.Now().UnixNano(),
	})
}

// closeLlmSpan 关闭 LLM span：写入 completion 属性，可选生成 reasoning 子 span。
// Python: _close_llm_span(state, response)
func (h *OtelCallbackHandler) closeLlmSpan(state *LlmSpanState, response any) {
	// Python: completion_text = _coerce_message_content(_message_content(response))
	completionText := coerceMessageContent(messageContent(response))
	// Python: reasoning_text = str(getattr(response, "reasoning_content", "") or "")
	reasoningText := extractReasoningContent(response)
	// Python: self._maybe_record_response_attrs(state, response)
	h.maybeRecordResponseAttrs(state, response)
	// Python: state.span.set_attribute(f"{GEN_AI_COMPLETION}.0.role", "assistant")
	state.Span.SetAttributes(
		attribute.String(fmt.Sprintf("%s.0.role", GenAICompletion), "assistant"),
		attribute.String(fmt.Sprintf("%s.0.content", GenAICompletion), RedactCompletion(completionText, h.config)),
	)
	// Python: if reasoning_text: 生成 reasoning 子 span
	if reasoningText != "" {
		// Python: with self._tracer().start_as_current_span(name="llm.reasoning", context=set_span_in_context(state.span))
		reasoningCtx := trace.ContextWithSpan(context.Background(), state.Span)
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
		logger.Warn(logComponent).Any("error", r).Str("method", methodName).Msg("otel 回调异常")
	}
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
	// 尝试反射取 Role 字段（对 Go struct）
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
	type usageHolder interface{ GetUsageMetadata() *llmschema.UsageMetadata }
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
