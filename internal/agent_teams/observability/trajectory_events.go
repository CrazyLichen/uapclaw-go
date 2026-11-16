package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ──────────────────────────── 常量 ────────────────────────────

// RequestSystemSlotPrefix 每次请求的 system 槽在窗口头部的出现 id 前缀。
// 该槽是请求材料而非对话：压缩重写对话但不重写请求材料，
// 所以压缩产出的窗口保留前一个窗口的槽。
// Python: trajectory_events.py:58
const RequestSystemSlotPrefix = "openjiuwen:request-system-slot:"

// ──────────────────────────── 导出函数 ────────────────────────────

// stringValueOf 取路由属性中的字符串值（非字符串键返回空串）。
// Python: parent_span.attributes.get(key) 的取值语义
func stringValueOf(value attribute.Value) string {
	if value.Type() == attribute.STRING {
		return value.AsString()
	}
	return ""
}

// routingValueEmpty 路由透传的空值判定：仅字符串空串视为空
// （Python: value not in (None, "")，布尔 false 不属空值，照旧透传）。
func routingValueEmpty(value attribute.Value) bool {
	return value.Type() == attribute.STRING && value.AsString() == ""
}

// EmitNativeTrajectoryEvent 用父 span 的具体归属发出一条不可变 v2 事件。
//
// 偏差 D-15.11-f：Python 从 parent_span.attributes 读取归属与路由键，
// go-otel 的 trace.Span 不暴露属性读面，故改为调用方显式传入
// parentAttributes（trajectory.go 从 turnRecord.turnAttributes 组装，
// 数据同源语义等价）。
// Python: trajectory_events.py:62-125 emit_native_trajectory_event
func EmitNativeTrajectoryEvent(
	ctx context.Context,
	tracer trace.Tracer,
	parentSpan trace.Span,
	eventKind string,
	payload map[string]any,
	subjectSequence *int,
	sequenceEpoch *string,
	parentAttributes map[string]attribute.Value,
) trace.Span {
	requireKnownEventKind(eventKind)
	if !parentSpan.IsRecording() {
		return nil
	}
	sessionID := stringValueOf(parentAttributes[GenAIConversationID])
	subjectID := stringValueOf(parentAttributes[OJExecutionSubjectID])
	if subjectID == "" {
		subjectID = "main"
	}
	var resolvedEpoch string
	var sequence int
	if subjectSequence == nil && sequenceEpoch == nil {
		resolvedEpoch, sequence = NextTrajectorySubjectPosition(sessionID, subjectID)
	} else if subjectSequence != nil && sequenceEpoch != nil {
		resolvedEpoch = *sequenceEpoch
		sequence = *subjectSequence
	} else {
		// Python 抛 ValueError；Go 以 panic 对应（编程性契约违反）
		panic("subject_sequence and sequence_epoch must be provided together")
	}
	eventID := hexUUID()
	// Python: set_span_in_context(parent_span, otel_context.get_current())
	parentCtx := trace.ContextWithSpanContext(ctx, parentSpan.SpanContext())
	_, span := tracer.Start(parentCtx, eventKind, trace.WithSpanKind(trace.SpanKindInternal))
	recordedAt := time.Now().UnixNano()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		payloadJSON, _ = json.Marshal(map[string]any{"error": fmt.Sprintf("%v", payload)})
	}
	attrs := []attribute.KeyValue{
		attribute.String(OJTrajectorySchemaVersion, TrajectorySpanSchemaVersion),
		attribute.String(OJTrajectoryEventID, eventID),
		attribute.String(OJTrajectoryEventKind, eventKind),
		attribute.String(OJTrajectorySubjectID, subjectID),
		attribute.String(OJTrajectorySequenceEpoch, resolvedEpoch),
		attribute.Int(OJTrajectorySubjectSequence, sequence),
		attribute.String(GenAIConversationID, sessionID),
		attribute.Int64(OJTrajectoryRecordedAtUnixNano, recordedAt),
		attribute.String(OJTrajectoryPayload, string(payloadJSON)),
		attribute.String(OJTrajectoryRecordKind, "event"),
	}
	// 路由键透传（Python: trajectory_events.py:112-125 的 routing_key 循环）
	routingKeys := []string{
		OJTurnID, OJStepID, OJRequestID, OJRunID, OJAgentMode,
		OJTurnNumber, OJStepNumber,
		OJExecutionSubjectID, OJExecutionSubjectDisplayName,
		OJExecutionSubjectKind, OJExecutionSubjectParentID,
		OJExecutionSubjectSessionID,
	}
	for _, key := range routingKeys {
		if value, ok := parentAttributes[key]; ok && !routingValueEmpty(value) {
			attrs = append(attrs, attribute.KeyValue{Key: attribute.Key(key), Value: value})
		}
	}
	span.SetAttributes(attrs...)
	span.SetStatus(codes.Ok, "")
	span.End()
	return span
}

// RecordNativeTrajectoryLogEvent 在父 span 上追加一条原生轨迹 log 事件。
// Python: trajectory_events.py:127-160 record_native_trajectory_log_event
func RecordNativeTrajectoryLogEvent(
	parentSpan trace.Span,
	eventKind string,
	payload map[string]any,
	subjectID string,
	subjectSequence *int,
	sequenceEpoch *string,
	parentAttributes map[string]attribute.Value,
) bool {
	requireKnownEventKind(eventKind)
	if !parentSpan.IsRecording() {
		return false
	}
	sessionID := stringValueOf(parentAttributes[GenAIConversationID])
	var resolvedEpoch string
	var sequence int
	if subjectSequence == nil && sequenceEpoch == nil {
		resolvedEpoch, sequence = NextTrajectorySubjectPosition(sessionID, subjectID)
	} else if subjectSequence != nil && sequenceEpoch != nil {
		resolvedEpoch = *sequenceEpoch
		sequence = *subjectSequence
	} else {
		panic("subject_sequence and sequence_epoch must be provided together")
	}
	recordedAt := time.Now().UnixNano()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		payloadJSON, _ = json.Marshal(map[string]any{"error": fmt.Sprintf("%v", payload)})
	}
	attrs := []attribute.KeyValue{
		attribute.String(OJTrajectorySchemaVersion, TrajectorySpanSchemaVersion),
		attribute.String(OJTrajectoryEventID, hexUUID()),
		attribute.String(OJTrajectoryEventKind, eventKind),
		attribute.String(OJTrajectorySubjectID, subjectID),
		attribute.String(OJTrajectorySequenceEpoch, resolvedEpoch),
		attribute.Int(OJTrajectorySubjectSequence, sequence),
		attribute.String(GenAIConversationID, sessionID),
		attribute.Int64(OJTrajectoryRecordedAtUnixNano, recordedAt),
		attribute.String(OJTrajectoryPayload, string(payloadJSON)),
	}
	// 关联键透传（Python: trajectory_events.py:150-156）
	for _, key := range []string{OJTurnID, OJStepID, OJRequestID} {
		if value, ok := parentAttributes[key]; ok && !routingValueEmpty(value) {
			attrs = append(attrs, attribute.KeyValue{Key: attribute.Key(key), Value: value})
		}
	}
	parentSpan.AddEvent(eventKind, trace.WithAttributes(attrs...), trace.WithTimestamp(time.Unix(0, recordedAt)))
	return true
}

// EmitContextWindowCommit 发出一条已结束的 context.window.commit 子 span。
// 只有把对话向前推进的请求才推进链：压缩请求的 prompt 是「关于上下文的」
// 而不是上下文的一部分，提交它会拼接进读者重放的链；
// 压缩产出的窗口由 EmitCompactionWindowCommit 提交。
// 返回 nil 表示该请求不推进链或所属 span 已不在记录。
// Python: trajectory_events.py:163-205 emit_context_window_commit
func EmitContextWindowCommit(
	tracer trace.Tracer,
	llmSpan trace.Span,
	messages []map[string]any,
	requestPurpose string,
	parentAttributes map[string]attribute.Value,
) trace.Span {
	if requestPurpose == "compaction" {
		// 提前返回、不推进：推进动作会重写主体的规范状态，
		// 让压缩触达它既会污染链、也会让下一真实轮的 delta 变成近全量窗口。
		return nil
	}
	sessionID := stringValueOf(parentAttributes[GenAIConversationID])
	subjectID := stringValueOf(parentAttributes[OJExecutionSubjectID])
	if subjectID == "" {
		subjectID = "main"
	}
	sequenceEpoch, sequence, payload := contextWindowCommitPayload(
		sessionID, subjectID, messages, requestPurpose)
	return EmitNativeTrajectoryEvent(context.Background(), tracer, llmSpan, "context.window.commit",
		payload, &sequence, &sequenceEpoch, parentAttributes)
}

// EmitCompactionWindowCommit 提交一次完成的压缩产出的上下文窗口。
// 压缩是自成一轮：指令是输入、摘要请求是其模型调用、重写后的对话是
// 后续每轮的起点。压缩自己的模型调用在结果已知时已结束，
// 所以提交挂在存活的 agent/run span 上，经 model_requests 指名其物理请求。
// Python: trajectory_events.py:207-272 emit_compaction_window_commit
func EmitCompactionWindowCommit(
	tracer trace.Tracer,
	parentSpan trace.Span,
	messages []map[string]any,
	operationID string,
	modelRequests []map[string]string,
	parentAttributes map[string]attribute.Value,
) trace.Span {
	sessionID := stringValueOf(parentAttributes[GenAIConversationID])
	subjectID := stringValueOf(parentAttributes[OJExecutionSubjectID])
	if subjectID == "" {
		subjectID = "main"
	}
	previous := CurrentContextWindowMessages(sessionID, subjectID)
	if previous == nil {
		previous = []map[string]any{}
	}
	sequenceEpoch, sequence, payload := contextWindowCommitPayload(
		sessionID, subjectID, compactedWindow(previous, messages), "compaction")
	payload["caused_by_operation_id"] = operationID
	payload["input_window_id"] = payload["base_window_id"]
	payload["output_window_id"] = payload["window_id"]
	payload["model_requests"] = modelRequests
	if payload["transition_kind"] == "epoch_baseline" {
		payload["correlation_kind"] = "compaction"
	} else {
		payload["transition_kind"] = "compaction"
	}
	return EmitNativeTrajectoryEvent(context.Background(), tracer, parentSpan, "context.window.commit",
		payload, &sequence, &sequenceEpoch, parentAttributes)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// requireKnownEventKind 事件类别闭集门卫：读者把事件类别集当作闭集处理，
// 未知类别会被所有读者丢弃，所以在写入处拒绝。
// Python: trajectory_events.py:55-59 _require_known_event_kind
func requireKnownEventKind(eventKind string) {
	if _, ok := TrajectoryEventKinds[eventKind]; !ok {
		panic(fmt.Sprintf("unknown trajectory event kind: %q", eventKind))
	}
}

// compactedWindow 构建压缩留下的窗口。
// 前一窗口按模型请求视角陈述；messages 是上下文引擎视角的存活消息。
// 两者在非内容处（tool call 形状、provider 归一化部件）拼法不同，
// 这种差异不得读成压缩重写了它保留的消息：role 与 content 未变的幸存者
// 按前一窗口原样重述；内容变了的重述为新消息。
// 前一窗口头部的每请求 system 槽保留。
// Python: trajectory_events.py:274-311 _compacted_window
func compactedWindow(previous, messages []map[string]any) []map[string]any {
	previousByID := make(map[string]map[string]any, len(previous))
	for _, message := range previous {
		previousByID[messageIDOf(message)] = message
	}
	var window []map[string]any
	for _, message := range previous {
		id := messageIDOf(message)
		if len(id) >= len(RequestSystemSlotPrefix) && id[:len(RequestSystemSlotPrefix)] == RequestSystemSlotPrefix {
			window = append(window, message)
		}
	}
	for _, message := range messages {
		prior := previousByID[messageIDOf(message)]
		unchanged := prior != nil &&
			fmt.Sprintf("%v", prior["role"]) == fmt.Sprintf("%v", message["role"]) &&
			jsonEqual(prior["content"], message["content"])
		if unchanged {
			window = append(window, prior)
		} else {
			window = append(window, message)
		}
	}
	return window
}

// contextWindowCommitPayload 推进一个主体的窗口并构建陈述它的 commit。
// 返回 (纪元, 主体序号, commit 载荷)。
// 只有基线携带完整窗口；后续 commit 都是对前一个的增量。
// Python: trajectory_events.py:313-352 _context_window_commit_payload
func contextWindowCommitPayload(sessionID, subjectID string, messages []map[string]any, requestPurpose string) (string, int, map[string]any) {
	windowID := hexUUID()
	epoch, sequence, baseWindowID, delta, isBaseline := AdvanceContextWindow(sessionID, subjectID, windowID, messages)
	payload := map[string]any{
		"window_id":       windowID,
		"base_window_id":  baseWindowID,
		"complete":        true,
		"delta":           delta,
		"request_purpose": requestPurpose,
	}
	if isBaseline {
		payload["messages"] = messages
		payload["transition_kind"] = "epoch_baseline"
		payload["baseline_reason"] = "runtime_epoch_start"
	}
	return epoch, sequence, payload
}

// messageIDOf 取消息标识（缺失为空串）。Python: str(message.get("message_id", ""))
func messageIDOf(message map[string]any) string {
	if v, ok := message["message_id"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		// Python: default=str 的对应物
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// jsonEqual JSON 值相等比较（经序列化往返后比较）。
// Python 侧无对应实现：Python 用 == 直接比较 dict；
// Go 的 map 比较不可用，以稳定 JSON 字符串比较等价替代
func jsonEqual(a, b any) bool {
	return stableJSONFingerprint(toMap(a)) == stableJSONFingerprint(toMap(b))
}

// toMap 把 JSON 值归一为 map[string]any（非 map 值包一层以比较）
func toMap(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{"value": value}
}
