// 压缩观测桥：把真实上下文压缩完成事实桥接进 trajectory v2。
//
// 压缩是自成的一轮：指令进、模型被要求产出一次摘要、重写后的对话作为
// 后续每轮的起点。桥在压缩完成时记录这轮的两半——compaction.completed
// 事件（发生了什么）与 context.window.commit（它留下的窗口）。
//
// 对应 Python 代码：openjiuwen/extensions/observability/context_compression_handler.py
// （1:1；Python 位于 extensions 层，Go 与本包同层共存，偏差登记 spec §4）。

package observability

import (
	"context"
	"encoding/json"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cbschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextCompressionObservabilityBridge 为每次完成的压缩发出事件与窗口提交。
// Python: ContextCompressionObservabilityBridge（context_compression_handler.py:45）。
type ContextCompressionObservabilityBridge struct {
	// tracer 发出轨迹 span 的 tracer
	tracer trace.Tracer
	// windowMessages 把上下文引擎消息转换为窗口提交陈述的规范轨迹形态；
	// 与模型请求提交使用同一转换器，两种 commit 共享消息身份。
	windowMessages func(messages any) []map[string]any
	// modelRequestsByOperation 压缩操作 → 其物理 LLM 请求引用列表
	modelRequestsByOperation map[string][]map[string]string
	// modelRequestsLock 操作桶锁（Python: _model_requests_lock）
	modelRequestsLock sync.Mutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextCompressionObservabilityBridge 创建压缩观测桥。
// Python: __init__（context_compression_handler.py:56-72）。
func NewContextCompressionObservabilityBridge(tracer trace.Tracer, windowMessages func(messages any) []map[string]any) *ContextCompressionObservabilityBridge {
	return &ContextCompressionObservabilityBridge{
		tracer:                   tracer,
		windowMessages:           windowMessages,
		modelRequestsByOperation: map[string][]map[string]string{},
	}
}

// OnLLMRequestInput 为一次压缩操作盖戳并保留其物理 LLM 请求。
// Python: on_llm_request_input（context_compression_handler.py:74-98）。
func (b *ContextCompressionObservabilityBridge) OnLLMRequestInput(ctx context.Context, data *cb.LLMCallEventData) any {
	// Python: operation_id = str(kwargs.get("context_operation_id") or "")
	operationID := stringOfAny(data.Extra["context_operation_id"])
	// Python: if kwargs.get("request_purpose") != "compaction" or not operation_id: return
	if stringOfAny(data.Extra["request_purpose"]) != "compaction" || operationID == "" {
		return nil
	}
	spanState := SpanStateFromCtx(ctx)
	if spanState == nil {
		return nil
	}
	// Python: span = get_current_llm_span()
	span := spanState.CurrentLLMSpan()
	if span == nil || !span.IsRecording() {
		return nil
	}
	span.SetAttributes(
		attribute.String(OJRequestPurpose, "compaction"),
		attribute.String(OJContextOperationID, operationID),
	)
	// Python: compaction_number = context_compaction_number(
	//     session_id=span.attributes.get(GEN_AI_CONVERSATION_ID),
	//     subject_id=span.attributes.get(OJ_EXECUTION_SUBJECT_ID),
	//     operation_id=operation_id)
	compactionNumber := ContextCompactionNumber(
		stringOfAny(spanAttributeValue(span, GenAIConversationID)),
		stringOfAny(spanAttributeValue(span, OJExecutionSubjectID)),
		operationID)
	if compactionNumber > 0 {
		span.SetAttributes(attribute.Int(OJCompactionNumber, compactionNumber))
	}
	requestRef := map[string]string{
		"request_id":   stringOfAny(spanAttributeValue(span, OJRequestID)),
		"inference_id": stringOfAny(spanAttributeValue(span, OJInferenceID)),
	}
	b.modelRequestsLock.Lock()
	defer b.modelRequestsLock.Unlock()
	for _, existing := range b.modelRequestsByOperation[operationID] {
		if existing["request_id"] == requestRef["request_id"] && existing["inference_id"] == requestRef["inference_id"] {
			return nil
		}
	}
	b.modelRequestsByOperation[operationID] = append(b.modelRequestsByOperation[operationID], requestRef)
	return nil
}

// OnContextCompressionState 处理压缩状态事件（终态时发出事件与窗口提交）。
// Python: on_context_compression_state（context_compression_handler.py:100-150）。
func (b *ContextCompressionObservabilityBridge) OnContextCompressionState(ctx context.Context, data *cb.ContextCallEventData) any {
	state, ok := data.State.(*cbschema.ContextCompressionState)
	if !ok {
		return nil
	}
	// Python: terminal = state.status in {"completed","noop","skipped","failed"}
	terminal := state.Status == cbschema.CompressionCompleted ||
		state.Status == cbschema.CompressionNoop ||
		state.Status == cbschema.CompressionSkipped ||
		state.Status == cbschema.CompressionFailed
	b.modelRequestsLock.Lock()
	var modelRequests []map[string]string
	if terminal {
		modelRequests = b.modelRequestsByOperation[state.OperationID]
		delete(b.modelRequestsByOperation, state.OperationID)
	} else {
		existing := b.modelRequestsByOperation[state.OperationID]
		modelRequests = make([]map[string]string, len(existing))
		copy(modelRequests, existing)
	}
	b.modelRequestsLock.Unlock()
	if state.Status != cbschema.CompressionCompleted {
		return nil
	}
	// Python: payload = state.model_dump(mode="json") + context_id/context_session_id/model_requests
	payload := compressionStatePayload(state)
	payload["context_id"] = data.ContextID
	payload["context_session_id"] = data.SessionID
	payload["model_requests"] = modelRequests
	// Python: parent_span = self._resolve_parent_span(str(kwargs.get("session_id") or ""))
	parentSpan := b.resolveParentSpan(ctx, data.SessionID)
	if parentSpan == nil {
		logger.Debug(logComponent).Str("operation_id", state.OperationID).
			Msg("otel: no live parent for completed context compression operation")
		return nil
	}
	event := EmitNativeTrajectoryEvent(ctx, b.tracer, parentSpan, "compaction.completed",
		payload, nil, nil, nil)
	if event == nil {
		return nil
	}
	if data.Context == nil {
		// 这里的沉默曾掩盖一个缺陷：没有窗口提交的压缩会让查看器把下一轮
		// 的删除渲染成新增行，而没有任何原因说明。
		logger.Warn(logComponent).Str("operation_id", state.OperationID).
			Msg("otel: context compaction states no output window; the completion callback carried no context")
		return nil
	}
	// Python: emit_compaction_window_commit(..., messages=self._window_messages(context.get_messages()), ...)
	EmitCompactionWindowCommit(b.tracer, parentSpan, b.windowMessages(contextMessages(data.Context)),
		state.OperationID, modelRequests, nil)
	return nil
}

// resolveParentSpan 解析压缩完成事件的父 span。
// Python: _resolve_parent_span（context_compression_handler.py:152-163）——
// 当前 span → 当前 agent span → 会话根 span。
// Go 无 get_current_agent_span 的 ContextVar 等价物，中段缺省（偏差登记
// spec §4）；ctx 携带 span 与会话根 span 两级保留。
func (b *ContextCompressionObservabilityBridge) resolveParentSpan(ctx context.Context, sessionID string) trace.Span {
	if current := trace.SpanFromContext(ctx); current != nil && current.IsRecording() {
		return current
	}
	if root := GetRootSpan(sessionID); root != nil && root.IsRecording() {
		return root
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stringOfAny 取 any 的字符串形态（Python str(v or "")）。
func stringOfAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// spanAttributeValue 读 span 属性（Python span.attributes.get(key)）。
// Go 的 trace.Span 接口不支持属性回读，经可选接口断言读取；不支持时返回
// nil（偏差登记 spec §4）。
func spanAttributeValue(span trace.Span, key string) any {
	type attrReader interface {
		AttributeValue(key string) (attribute.Value, bool)
	}
	if reader, ok := span.(attrReader); ok {
		if v, ok := reader.AttributeValue(key); ok {
			return v.AsInterface()
		}
	}
	return nil
}

// compressionStatePayload 把压缩状态序列化为 JSON 安全载荷。
// Python: state.model_dump(mode="json")——Go 经 json tag 序列化后往返为 map
// （memory 规则：序列化一律走 struct json tag）。
func compressionStatePayload(state *cbschema.ContextCompressionState) map[string]any {
	data, err := json.Marshal(state)
	if err != nil {
		return map[string]any{}
	}
	payload := map[string]any{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

// contextMessages 取上下文实例的消息列表（Python context.get_messages()）。
// 上下文实例有两种载体形态（鸭子式断言）：
//   - ModelContext（GetMessages(size, withHistory)）——size<=0 取全量、不带历史
//   - ContextWindow（GetMessages()）
func contextMessages(context any) any {
	if mc, ok := context.(interface {
		GetMessages(size int, withHistory bool) ([]llmschema.BaseMessage, error)
	}); ok {
		messages, err := mc.GetMessages(0, false)
		if err != nil {
			return nil
		}
		return messages
	}
	if cw, ok := context.(interface {
		GetMessages() []llmschema.BaseMessage
	}); ok {
		return cw.GetMessages()
	}
	return context
}
