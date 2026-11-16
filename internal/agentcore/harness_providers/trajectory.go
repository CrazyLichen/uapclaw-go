package harness_providers

// HarnessTrajectoryRecorder：把一次 harness 的 HarnessEvent 观测流记录为轨迹 span。
//
// recorder 经 Observe 按流序消费每个信封，不支持并发使用；
// 宿主由消费 harness 事件流的任务驱动它。所有记录都归入宿主指定的
// 执行主体（ExecutionSubject），查看器据此归入该 agent 的泳道。
//
// provider 差异从不进入本模块：能观测模型请求的 provider 经
// ModelRequestEvent 上报，并用信封 causation_ids 关联其引发的工具项。
//
// Python: harness_providers/trajectory.py（810 行，单文件一比一）

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/observability"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

// llmschemaOriginKey / llmschemaOriginExternal 消息来源元数据简写
// Python: core/foundation/llm/schema/message.py:11,13
func llmschemaOriginKey() string      { return llmschema.OpenjiuwenMessageOriginMetadata }
func llmschemaOriginExternal() string { return llmschema.OpenjiuwenMessageOriginExternalUser }

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TracerName 本模块 tracer 名。Python: trajectory.py:115
	TracerName = "openjiuwen.harness_providers.trajectory"
	// toolItemType 工具 item 类别。Python: trajectory.py:116
	toolItemType = "tool"
	// assistantRequestPurpose 助手请求用途。Python: trajectory.py:117
	assistantRequestPurpose = "assistant"
)

// requestParameterAttributes provider 可能陈述的采样参数 → GenAI 属性映射。
// Python: trajectory.py:119-130
var requestParameterAttributes = map[string]string{
	"temperature":       observability.GenAIRequestTemperature,
	"top_p":             observability.GenAIRequestTopP,
	"top_k":             observability.GenAIRequestTopK,
	"max_tokens":        observability.GenAIRequestMaxTokens,
	"seed":              observability.GenAIRequestSeed,
	"choice_count":      observability.GenAIRequestChoiceCount,
	"presence_penalty":  observability.GenAIRequestPresencePenalty,
	"frequency_penalty": observability.GenAIRequestFrequencyPenalty,
	"reasoning_level":   observability.GenAIRequestReasoningLevel,
	"stream":            observability.GenAIRequestStream,
}

// ──────────────────────────── 结构体 ────────────────────────────

// turnRecord 一次被记录轮次的打开 span 与请求身份。
// Python: trajectory.py:135-143 _TurnRecord
type turnRecord struct {
	// span 轮次根 span
	span trace.Span
	// turnAttributes 轮次 span 携带的身份属性（后续子 span 复用，值保留 Python 的标量类型）
	turnAttributes map[string]attribute.Value
	// requestCount 已记录的模型请求数
	requestCount int
	// requests request_id → (inference_id, step_number)
	requests map[string]requestIdentity
	// tools 打开中的工具 span
	tools map[string]trace.Span
}

// requestIdentity 一次模型请求在轨迹中的身份
type requestIdentity struct {
	inferenceID string
	stepNumber  int
}

// HarnessTrajectoryRecorder 把一次 harness 的 HarnessEvent 流转为轨迹 span。
// Python: trajectory.py:146-193
type HarnessTrajectoryRecorder struct {
	subject           *harness.ExecutionSubject
	agentName         string
	agentMode         string
	tracer            trace.Tracer
	config            *observability.ObservabilityConfig
	attributes        map[string]attribute.Value
	handler           *observability.OtelCallbackHandler
	turns             map[string]*turnRecord
	pendingInputs     map[string]string
	turnInputs        map[string][]string
	pendingIdentities map[string]identityValue
	activeTurnID      string
	// externalUserMessageIDs 被识别为外部用户的消息 id。消息归谁不随携带它的
	// 轮次结束而变，而会话的寿命长于轮次。
	externalUserMessageIDs   map[string]struct{}
	externalUserIDsRecovered bool
}

// identityValue 宿主指派的轨迹轮次身份
type identityValue struct {
	turnID     string
	turnNumber *int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHarnessTrajectoryRecorder 构造 recorder。
// Python: trajectory.py:163-193 __init__（ValueError → Go error，偏差 D-15.11-h）
func NewHarnessTrajectoryRecorder(
	subject *harness.ExecutionSubject,
	agentName, agentMode string,
	tracer trace.Tracer,
	config *observability.ObservabilityConfig,
	attributes map[string]attribute.Value,
) (*HarnessTrajectoryRecorder, error) {
	if subject.SessionID == "" {
		return nil, fmt.Errorf("trajectory recorder subject requires a session_id")
	}
	if subject.DisplayName == "" {
		return nil, fmt.Errorf("trajectory recorder subject requires a display_name")
	}
	return &HarnessTrajectoryRecorder{
		subject:                subject,
		agentName:              agentName,
		agentMode:              agentMode,
		tracer:                 tracer,
		config:                 config,
		attributes:             attributes,
		handler:                observability.NewOtelCallbackHandler(config, tracer),
		turns:                  make(map[string]*turnRecord),
		pendingInputs:          make(map[string]string),
		turnInputs:             make(map[string][]string),
		pendingIdentities:      make(map[string]identityValue),
		externalUserMessageIDs: make(map[string]struct{}),
	}, nil
}

// CreateHarnessTrajectoryRecorder 在共享可观测性运行时上构建 recorder。
// 返回 (nil, nil) 表示可观测性未初始化。
// Python: trajectory.py:196-221 create（返回 None）
func CreateHarnessTrajectoryRecorder(
	subject *harness.ExecutionSubject,
	agentName, agentMode string,
	attributes map[string]attribute.Value,
) (*HarnessTrajectoryRecorder, error) {
	config := observability.GetObservabilityConfig()
	if !observability.IsObservabilityInitialized() || config == nil {
		return nil, nil
	}
	return NewHarnessTrajectoryRecorder(subject, agentName, agentMode,
		observability.GetTracer(TracerName), config, attributes)
}

// Subject 返回记录归属的执行主体。
// Python: trajectory.py:224-226
func (r *HarnessTrajectoryRecorder) Subject() *harness.ExecutionSubject {
	return r.subject
}

// RecordInput 记住宿主投给 turn_id 的一次输入。
// 轮次 span 陈述开启该轮的输入；每次输入（含打进运行中轮次的 steer）都被
// 记住，携带它的消息据此读作用户的。宿主交付的就是 agent 被告知的内容；
// harness 沿途加进自己上下文的消息匹配不到任何输入，保持 harness 自有。
// Python: trajectory.py:232-245 record_input
func (r *HarnessTrajectoryRecorder) RecordInput(turnID, text string) {
	if text == "" {
		return
	}
	if _, ok := r.turns[turnID]; !ok {
		if _, exists := r.pendingInputs[turnID]; !exists {
			r.pendingInputs[turnID] = text
		}
	}
	r.turnInputs[turnID] = append(r.turnInputs[turnID], text)
}

// RecordTurnIdentity 把宿主的轨迹轮次身份指派给一个协议轮次。
// 自己维护轮次编号（跨 harness 重启持久化）的宿主在该轮 STARTED 事件被
// 观测到之前陈述它。否则协议 turn_id 就是轨迹轮次 id 且不陈述轮次号。
// Python: trajectory.py:271-286 record_turn_identity
func (r *HarnessTrajectoryRecorder) RecordTurnIdentity(protocolTurnID, turnID string, turnNumber int) {
	if protocolTurnID == "" || turnID == "" || turnNumber < 1 {
		return
	}
	value := identityValue{turnID: turnID, turnNumber: &turnNumber}
	r.pendingIdentities[protocolTurnID] = value
}

// RecordFailure 在活动轮次上记录宿主定论的失败。
// 事件加进活动轮次 span 并镜像到属性（部分 OTLP 后端会丢弃 span 事件）。
// 无活动轮次（启动失败）时发一个零长失败轮次，失败仍显示在 agent 泳道里。
// Python: trajectory.py:288-324 record_failure
func (r *HarnessTrajectoryRecorder) RecordFailure(name, summary string, attributes map[string]attribute.Value) {
	var span trace.Span
	standalone := false
	record := r.turns[r.activeTurnID]
	if record != nil && record.span.IsRecording() {
		span = record.span
	} else {
		turnAttrs := r.turnAttributes("")
		identity := turnIdentityAttributes("", nil)
		_, span = r.tracer.Start(context.Background(),
			fmt.Sprintf("invoke_agent %s", r.agentName),
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(mergeAttributes(turnAttrs, identity)...),
		)
		standalone = true
	}
	// Python: span.add_event(name, dict(attributes)) + set_attribute 循环
	span.AddEvent(name, trace.WithAttributes(valueMapToAttributes(attributes)...))
	span.SetAttributes(valueMapToAttributes(attributes)...)
	span.SetStatus(codes.Error, observability.RedactErrorSummary(summary, r.config))
	if standalone {
		span.End()
	}
}

// Observe 记录观测流的一个信封。
// Python: trajectory.py:330-338 observe
func (r *HarnessTrajectoryRecorder) Observe(envelope harness_protocol.HarnessEvent) {
	switch payload := envelope.Event.(type) {
	case harness_protocol.TurnLifecycleEvent:
		r.observeTurn(envelope, payload)
	case *harness_protocol.ModelRequestEvent:
		r.observeModelRequest(envelope, payload)
	case harness_protocol.ItemLifecycleEvent:
		if payload.ItemType == toolItemType {
			r.observeTool(envelope, payload)
		}
	}
}

// Close 结束所有仍打开的 span（如 harness 中途停止时）。
// Python: trajectory.py:340-343 close
func (r *HarnessTrajectoryRecorder) Close() {
	turnIDs := make([]string, 0, len(r.turns))
	for turnID := range r.turns {
		turnIDs = append(turnIDs, turnID)
	}
	sort.Strings(turnIDs)
	for _, turnID := range turnIDs {
		r.finishTurn(turnID, nil, nil)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// observeTurn 观测轮次生命周期事件。
// Python: trajectory.py:345-350 _observe_turn
func (r *HarnessTrajectoryRecorder) observeTurn(envelope harness_protocol.HarnessEvent, payload harness_protocol.TurnLifecycleEvent) {
	turnID := envelope.TurnID
	switch payload.Kind {
	case harness_protocol.TurnEventKindStarted:
		r.startTurn(turnID, envelope.Timestamp)
	case harness_protocol.TurnEventKindFinished, harness_protocol.TurnEventKindAborted, harness_protocol.TurnEventKindFailed:
		r.finishTurn(turnID, payload.Result, &envelope.Timestamp)
	}
}

// startTurn 开启一个轮次 span。
// Python: trajectory.py:352-367 _start_turn
func (r *HarnessTrajectoryRecorder) startTurn(turnID string, startTime float64) {
	// 陈旧轮次先收尾
	if stale := r.turns[turnID]; stale != nil {
		r.finishTurn(turnID, nil, &startTime)
	}
	inputText := r.pendingInputs[turnID]
	delete(r.pendingInputs, turnID)
	trajectoryTurnID, turnNumber := r.popIdentity(turnID)
	identity := turnIdentityAttributes(trajectoryTurnID, turnNumber)
	_, span := r.tracer.Start(
		context.Background(),
		fmt.Sprintf("invoke_agent %s", r.agentName),
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithTimestamp(nsTime(startTime)),
		trace.WithAttributes(mergeAttributes(r.turnAttributes(inputText), identity)...),
	)
	r.turns[turnID] = &turnRecord{
		span:           span,
		turnAttributes: identity,
		requests:       make(map[string]requestIdentity),
		tools:          make(map[string]trace.Span),
	}
	r.activeTurnID = turnID
}

// popIdentity 弹出待定的轮次身份（缺省 (turn_id, nil)）。
// Python: trajectory.py:357 _pending_identities.pop(turn_id, (turn_id, None))
func (r *HarnessTrajectoryRecorder) popIdentity(turnID string) (string, *int) {
	if value, ok := r.pendingIdentities[turnID]; ok {
		delete(r.pendingIdentities, turnID)
		return value.turnID, value.turnNumber
	}
	return turnID, nil
}

// turnAttributes 轮次 span 属性。
// Python: trajectory.py:369-382 _turn_attributes
func (r *HarnessTrajectoryRecorder) turnAttributes(inputText string) map[string]attribute.Value {
	attributes := map[string]attribute.Value{
		observability.GenAIOperationName:            attribute.StringValue("invoke_agent"),
		observability.OJTrajectorySchemaVersion:     attribute.StringValue(observability.TrajectorySpanSchemaVersion),
		observability.OJTrajectoryRecordKind:        attribute.StringValue("turn"),
		observability.OJTraceRoot:                   attribute.BoolValue(true),
		observability.OJAgentMode:                   attribute.StringValue(r.agentMode),
		observability.GenAIConversationID:           attribute.StringValue(r.subject.SessionID),
		observability.OJExecutionSubjectID:          attribute.StringValue(r.subject.SubjectID),
		observability.OJExecutionSubjectDisplayName: attribute.StringValue(r.subject.DisplayName),
		observability.OJExecutionSubjectKind:        attribute.StringValue(r.subject.Kind),
		observability.OJExecutionSubjectSessionID:   attribute.StringValue(r.subject.SessionID),
	}
	if r.subject.ParentSubjectID != "" {
		attributes[observability.OJExecutionSubjectParentID] = attribute.StringValue(r.subject.ParentSubjectID)
	}
	for key, value := range r.attributes {
		attributes[key] = value
	}
	attributes[observability.GenAIAgentName] = attribute.StringValue(r.agentName)
	if inputText != "" {
		attributes[observability.OJSpanInput] = attribute.StringValue(
			observability.RedactPrompt(inputText, r.config))
	}
	return attributes
}

// finishTurn 收尾一个轮次。
// Python: trajectory.py:397-420 _finish_turn
func (r *HarnessTrajectoryRecorder) finishTurn(turnID string, result *harness_protocol.TurnResult, endTime *float64) {
	record := r.turns[turnID]
	delete(r.turns, turnID)
	delete(r.turnInputs, turnID)
	if r.activeTurnID == turnID {
		r.activeTurnID = ""
	}
	if record == nil {
		return
	}
	for _, toolSpan := range record.tools {
		if toolSpan.IsRecording() {
			toolSpan.SetStatus(codes.Error, "tool call did not complete before the turn ended")
			toolSpan.End(spanEndOption(endTime)...)
		}
	}
	record.tools = make(map[string]trace.Span)
	span := record.span
	if !span.IsRecording() {
		return
	}
	if result != nil {
		finalOutput := harness_protocol.JsonValueToBuiltin(result.FinalOutput)
		text := textOf(finalOutput)
		if text != "" && finalOutput != nil {
			span.SetAttributes(attribute.String(observability.OJSpanOutput,
				observability.RedactCompletion(text, r.config)))
		}
		if result.Error != nil {
			span.SetStatus(codes.Error, observability.RedactErrorSummary(result.Error.Message, r.config))
		} else if result.Termination == nil {
			span.SetStatus(codes.Ok, "")
		}
	}
	span.End(spanEndOption(endTime)...)
}

// observeModelRequest 观测一次模型请求。
// Python: trajectory.py:426-507 _observe_model_request
func (r *HarnessTrajectoryRecorder) observeModelRequest(envelope harness_protocol.HarnessEvent, event *harness_protocol.ModelRequestEvent) {
	turnKey := envelope.TurnID
	record := r.turns[turnKey]
	if record == nil || !record.span.IsRecording() {
		return
	}
	stepNumber := record.requestCount + 1
	model := event.Model
	// 数值/布尔属性保留 OTel 类型（Python: int/float/bool 属性）
	attrs := map[string]attribute.Value{
		observability.GenAIOperationName:            attribute.StringValue("chat"),
		observability.OJTrajectorySchemaVersion:     attribute.StringValue(observability.TrajectorySpanSchemaVersion),
		observability.OJTrajectoryRecordKind:        attribute.StringValue("inference"),
		observability.OJRequestID:                   attribute.StringValue(event.RequestID),
		observability.OJRequestPurpose:              attribute.StringValue(assistantRequestPurpose),
		observability.OJStepNumber:                  attribute.IntValue(stepNumber),
		observability.OJAgentMode:                   attribute.StringValue(r.agentMode),
		observability.GenAIAgentName:                attribute.StringValue(r.agentName),
		observability.GenAIConversationID:           attribute.StringValue(r.subject.SessionID),
		observability.OJExecutionSubjectID:          attribute.StringValue(r.subject.SubjectID),
		observability.OJExecutionSubjectDisplayName: attribute.StringValue(r.subject.DisplayName),
		observability.OJExecutionSubjectKind:        attribute.StringValue(r.subject.Kind),
		observability.OJExecutionSubjectSessionID:   attribute.StringValue(r.subject.SessionID),
	}
	if r.subject.ParentSubjectID != "" {
		attrs[observability.OJExecutionSubjectParentID] = attribute.StringValue(r.subject.ParentSubjectID)
	}
	for key, value := range record.turnAttributes {
		// 轮次身份属性（record.turn_attributes）在子 span 上复用；
		// 本体键（OJ_AGENT_MODE/GEN_AI_AGENT_NAME 等）已在上面按 Python 顺序显式重置
		switch key {
		case observability.OJAgentMode, observability.GenAIAgentName:
			continue
		}
		attrs[key] = value
	}
	requestNumber := observability.NextExecutionSubjectRequestNumber(r.subject.SessionID, r.subject.SubjectID)
	attrs[observability.OJExecutionSubjectRequestNumber] = attribute.IntValue(requestNumber)
	if model != "" {
		attrs[observability.GenAIRequestModel] = attribute.StringValue(model)
		attrs[observability.GenAIResponseModel] = attribute.StringValue(model)
	}
	if event.ProviderName != "" {
		attrs[observability.GenAIProviderName] = attribute.StringValue(event.ProviderName)
	}
	if event.ResponseID != "" {
		attrs[observability.GenAIResponseID] = attribute.StringValue(event.ResponseID)
	}
	if len(event.FinishReasons) > 0 {
		attrs[observability.GenAIResponseFinishReasons] = attribute.StringValue(marshalJSON(event.FinishReasons))
	}
	if !event.InputObserved {
		// 仅在为 false 时陈述：进程内推理总是观测自己的请求，缺省保持常态不变
		attrs[observability.OJInferenceInputObserved] = attribute.BoolValue(false)
	}
	latencyMS := (event.EndedAt - event.StartedAt) * 1000
	attrs[observability.OJGenAIResponseTotalLatencyMs] = attribute.Float64Value(latencyMS)
	if event.TimeToFirstChunk != nil {
		attrs[observability.GenAIResponseTimeToFirstChunk] = attribute.Float64Value(*event.TimeToFirstChunk)
	}
	for key, value := range requestParameterAttributeValues(event) {
		attrs[key] = value
	}
	for key, value := range usageAttributeValues(event) {
		attrs[key] = value
	}
	if event.Cost != nil {
		attrs[observability.OJGenAIUsageTotalCost] = attribute.Float64Value(float64(event.Cost.Micros) / 1_000_000)
	}
	spanName := "chat"
	if model != "" {
		spanName = fmt.Sprintf("chat %s", model)
	}
	ctx := trace.ContextWithSpanContext(context.Background(), record.span.SpanContext())
	// Python: set_span_in_context(record.span, otel_context.Context()) —— 空上下文
	// 之外不携带当前上下文；Go 以 Background 为底等价（偏差 D-15.11-a）
	_, span := r.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithTimestamp(nsTime(event.StartedAt)),
		trace.WithAttributes(valueMapToAttributes(attrs)...),
	)
	inferenceID := fmt.Sprintf("%016x", span.SpanContext().SpanID())
	span.SetAttributes(attribute.String(observability.OJInferenceID, inferenceID))
	requestMessages := r.requestMessages(event, r.turnInputs[turnKey], r.rememberedExternalUserIDs())
	if len(requestMessages) > 0 {
		r.handler.RecordRequestInput(span, requestMessages)
	}
	toolDefinitions := harness_protocol.JsonValueToBuiltin(event.ToolDefinitions)
	if toolDefinitions != nil && !isEmptyBuiltin(toolDefinitions) {
		span.SetAttributes(attribute.String(observability.GenAIToolDefinitions, marshalJSON(toolDefinitions)))
	}
	if event.OutputMessage != nil {
		outputMessages := r.messageDicts(*event.OutputMessage)
		if len(outputMessages) > 0 {
			r.handler.RecordResponseOutput(span, outputMessages[0])
		}
	}
	if event.InputObserved && len(requestMessages) > 0 {
		observability.EmitContextWindowCommit(r.tracer, span,
			r.handler.ContextWindowMessages(requestMessages),
			assistantRequestPurpose,
			attrs)
	}
	setRequestStatus(span, event, r.config)
	span.End(trace.WithTimestamp(nsTime(event.EndedAt)))
	record.requestCount = stepNumber
	record.requests[event.RequestID] = requestIdentity{inferenceID: inferenceID, stepNumber: stepNumber}
}

// rememberExternalUserIDs 返回已知为外部用户的消息 id，恢复一次。
// 对话中途重启的成员拿到新 recorder，而会话与为其提交的窗口继续。
// 不恢复先前 recorder 认出的消息 id，用户说过的话会被重述为 harness 自有，
// 读者会看到整段历史再来一遍，这次作为上下文。
// Python: trajectory.py:247-269 _remembered_external_user_ids
func (r *HarnessTrajectoryRecorder) rememberedExternalUserIDs() map[string]struct{} {
	if r.externalUserIDsRecovered {
		return r.externalUserMessageIDs
	}
	r.externalUserIDsRecovered = true
	committed := observability.CurrentContextWindowMessages(r.subject.SessionID, r.subject.SubjectID)
	for _, message := range committed {
		messageID, _ := message["message_id"].(string)
		if messageID == "" {
			continue
		}
		if origin, _ := message["origin"].(string); origin == "external_user" {
			r.externalUserMessageIDs[messageID] = struct{}{}
		}
	}
	return r.externalUserMessageIDs
}

// observeTool 观测工具 item 生命周期事件。
// Python: trajectory.py:513-527 _observe_tool
func (r *HarnessTrajectoryRecorder) observeTool(envelope harness_protocol.HarnessEvent, payload harness_protocol.ItemLifecycleEvent) {
	turnKey := envelope.TurnID
	if turnKey == "" {
		turnKey = r.activeTurnID
	}
	itemID := envelope.ItemID
	record := r.turns[turnKey]
	if record == nil || !record.span.IsRecording() || itemID == "" {
		return
	}
	data := harness_protocol.JsonValueToBuiltin(payload.Data)
	values := map[string]any{}
	if m, ok := data.(map[string]any); ok {
		values = m
	}
	switch payload.Kind {
	case harness_protocol.ItemEventKindStarted:
		r.startTool(record, envelope, itemID, values)
	case harness_protocol.ItemEventKindCompleted:
		span := record.tools[itemID]
		delete(record.tools, itemID)
		if span == nil {
			// 无 STARTED 的 COMPLETED：补造后立即收尾
			span = r.startTool(record, envelope, itemID, values)
			delete(record.tools, itemID)
		}
		r.finishTool(span, values, envelope.Timestamp)
	}
}

// startTool 开启一个工具 span。
// Python: trajectory.py:529-566 _start_tool
func (r *HarnessTrajectoryRecorder) startTool(record *turnRecord, envelope harness_protocol.HarnessEvent, itemID string, values map[string]any) trace.Span {
	toolName := "unknown"
	if v, ok := values["name"].(string); ok && v != "" {
		toolName = v
	} else if v, ok := values["tool_name"].(string); ok && v != "" {
		toolName = v
	}
	arguments := observability.RedactPrompt(textOf(values["arguments"]), r.config)
	attrs := map[string]attribute.Value{
		observability.GenAIOperationName:            attribute.StringValue("execute_tool"),
		observability.GenAIToolName:                 attribute.StringValue(toolName),
		observability.GenAIToolCallID:               attribute.StringValue(itemID),
		observability.GenAIToolCallArguments:        attribute.StringValue(arguments),
		observability.OJSpanInput:                   attribute.StringValue(arguments),
		observability.OJTrajectorySchemaVersion:     attribute.StringValue(observability.TrajectorySpanSchemaVersion),
		observability.OJTrajectoryRecordKind:        attribute.StringValue("tool"),
		observability.OJAgentMode:                   attribute.StringValue(r.agentMode),
		observability.GenAIAgentName:                attribute.StringValue(r.agentName),
		observability.GenAIConversationID:           attribute.StringValue(r.subject.SessionID),
		observability.OJExecutionSubjectID:          attribute.StringValue(r.subject.SubjectID),
		observability.OJExecutionSubjectDisplayName: attribute.StringValue(r.subject.DisplayName),
		observability.OJExecutionSubjectKind:        attribute.StringValue(r.subject.Kind),
		observability.OJExecutionSubjectSessionID:   attribute.StringValue(r.subject.SessionID),
	}
	if r.subject.ParentSubjectID != "" {
		attrs[observability.OJExecutionSubjectParentID] = attribute.StringValue(r.subject.ParentSubjectID)
	}
	for key, value := range record.turnAttributes {
		switch key {
		case observability.OJAgentMode, observability.GenAIAgentName:
			continue
		}
		attrs[key] = value
	}
	if owner := owningRequest(record, envelope); owner != nil {
		// 只有与引发它的请求绑定的工具是权威的；
		// 查看器丢弃它无法归位的权威工具
		attrs[observability.OJInferenceID] = attribute.StringValue(owner.inferenceID)
		attrs[observability.OJStepNumber] = attribute.IntValue(owner.stepNumber)
		attrs[observability.OJToolAuthoritative] = attribute.BoolValue(true)
	}
	ctx := trace.ContextWithSpanContext(context.Background(), record.span.SpanContext())
	_, span := r.tracer.Start(ctx,
		fmt.Sprintf("execute_tool %s", toolName),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithTimestamp(nsTime(envelope.Timestamp)),
		trace.WithAttributes(valueMapToAttributes(attrs)...),
	)
	record.tools[itemID] = span
	return span
}

// finishTool 收尾一个工具 span。
// Python: trajectory.py:568-579 _finish_tool
func (r *HarnessTrajectoryRecorder) finishTool(span trace.Span, values map[string]any, endTime float64) {
	if !span.IsRecording() {
		return
	}
	result := observability.RedactCompletion(textOf(values["result"]), r.config)
	span.SetAttributes(attribute.String(observability.GenAIToolCallResult, result))
	span.SetAttributes(attribute.String(observability.OJSpanOutput, result))
	if isErrorTrue(values["is_error"]) {
		span.SetAttributes(attribute.String(observability.ErrorType, observability.ToolReportedFailure))
		span.SetStatus(codes.Error, observability.RedactErrorSummary(result, r.config))
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End(trace.WithTimestamp(nsTime(endTime)))
}

// requestMessages 把请求整形为框架形态的消息 dict 列表，system 槽在前。
// hostInputs 是宿主投进该轮的文本。provider 陈述对话却不说哪条消息携带它们，
// 所以把包含各文本的最后一条 user 消息标记为外部用户的——与进程内 agent
// 标记它收到的输入同款。
// Python: trajectory.py:643-676 _request_messages
func (r *HarnessTrajectoryRecorder) requestMessages(event *harness_protocol.ModelRequestEvent, hostInputs []string, externalUserIDs map[string]struct{}) []map[string]any {
	var messages []map[string]any
	var systemParts []map[string]any
	for _, block := range event.SystemInstructions {
		systemParts = append(systemParts, contentPart(block))
	}
	if len(systemParts) > 0 {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": messageContent(systemParts),
		})
	}
	for _, message := range event.InputMessages {
		messages = append(messages, r.messageDicts(message)...)
	}
	for _, hostInput := range hostInputs {
		markHostInput(messages, hostInput, externalUserIDs)
	}
	for _, message := range messages {
		if messageID, ok := message["message_id"].(string); ok && messageID != "" {
			if _, known := externalUserIDs[messageID]; known {
				setExternalUserOrigin(message)
			}
		}
	}
	return messages
}

// markHostInput 标记携带该宿主输入的最后一条 user 消息。
// Python: trajectory.py:679-692 _mark_host_input
func markHostInput(messages []map[string]any, hostInput string, externalUserIDs map[string]struct{}) {
	text := strings.TrimSpace(hostInput)
	if text == "" {
		return
	}
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if role, _ := message["role"].(string); role != "user" {
			continue
		}
		content, _ := message["content"].(string)
		if strings.Contains(content, text) {
			setExternalUserOrigin(message)
			if messageID, ok := message["message_id"].(string); ok && messageID != "" {
				externalUserIDs[messageID] = struct{}{}
			}
			return
		}
	}
}

// setExternalUserOrigin 陈述该消息携带外部用户发送的内容。
// Python: trajectory.py:695-699 _set_external_user_origin
func setExternalUserOrigin(message map[string]any) {
	metadata := map[string]any{}
	if m, ok := message["metadata"].(map[string]any); ok {
		metadata = m
	} else {
		metadata = map[string]any{}
	}
	newMetadata := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		newMetadata[key] = value
	}
	newMetadata[llmschemaOriginKey()] = llmschemaOriginExternal()
	message["metadata"] = newMetadata
}

// messageDicts 把一条协议消息转成框架形态的消息 dict 列表。
// 工具结果成为按调用 id 分键的独立 tool 消息（框架轨迹助手读取的形状）；
// 消息其余部分保留原 message_id。
// Python: trajectory.py:702-752 _message_dicts
func (r *HarnessTrajectoryRecorder) messageDicts(message harness_protocol.TurnMessage) []map[string]any {
	role := string(message.Role)
	var parts []map[string]any
	var reasoning []string
	var toolCalls []map[string]any
	var toolResults []map[string]any
	for _, block := range message.Content {
		callID := blockCallID(block)
		content := harness_protocol.JsonValueToBuiltin(block.Content)
		switch block.Kind {
		case "reasoning":
			reasoning = append(reasoning, textOf(content))
		case "tool_call":
			call := map[string]any{}
			if m, ok := content.(map[string]any); ok {
				call = m
			}
			toolCalls = append(toolCalls, map[string]any{
				"id":        callID,
				"name":      textOf(call["name"]),
				"arguments": textOf(call["arguments"]),
			})
		case "tool_result":
			resultMessageID := message.MessageID
			if callID != "" {
				resultMessageID = fmt.Sprintf("%s:%s", message.MessageID, callID)
			}
			toolResults = append(toolResults, map[string]any{
				"role":         "tool",
				"message_id":   resultMessageID,
				"tool_call_id": callID,
				"content":      textOf(content),
			})
		default:
			parts = append(parts, contentPart(block))
		}
	}
	metadata := messageMetadata(message)
	var result []map[string]any
	carriesOwnContent := len(parts) > 0 || len(reasoning) > 0 || len(toolCalls) > 0
	if carriesOwnContent || len(toolResults) == 0 {
		item := map[string]any{
			"role":       role,
			"message_id": message.MessageID,
			"content":    messageContent(parts),
		}
		if len(reasoning) > 0 {
			item["reasoning_content"] = strings.Join(reasoning, "")
		}
		if len(toolCalls) > 0 {
			item["tool_calls"] = toolCalls
		}
		if len(metadata) > 0 {
			item["metadata"] = metadata
		}
		result = append(result, item)
	}
	result = append(result, toolResults...)
	return result
}

// contentPart 一个内容块的框架形态。
// Python: trajectory.py:755-761 _content_part
func contentPart(block harness_protocol.ContentBlock) map[string]any {
	content := harness_protocol.JsonValueToBuiltin(block.Content)
	if block.Kind == "text" {
		return map[string]any{"type": "text", "text": textOf(content)}
	}
	if m, ok := content.(map[string]any); ok {
		part := map[string]any{"type": block.Kind}
		for key, value := range m {
			part[key] = value
		}
		return part
	}
	return map[string]any{"type": block.Kind, "content": content}
}

// messageContent 消息体：全文本时合并，其余保留部件列表。
// provider 常把一条消息拆成多个文本块（注入的提醒加消息本体）。
// 读者把它们当一个整体读；保留拆分会 wherever 视图按文本渲染时
// 把消息陈述成 JSON 数组，所以纯文本块合并。带非文本部件（图像、文档）
// 的消息保留它们。
// Python: trajectory.py:764-777 _message_content
func messageContent(parts []map[string]any) any {
	if len(parts) == 0 {
		return ""
	}
	if isTextOnly(parts) {
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			if t, ok := part["text"].(string); ok {
				texts = append(texts, t)
			}
		}
		return strings.Join(texts, "\n\n")
	}
	return parts
}

// isTextOnly 是否全部为 text 部件。
// Python: trajectory.py:780-781 _is_text_only
func isTextOnly(parts []map[string]any) bool {
	for _, part := range parts {
		if t, _ := part["type"].(string); t != "text" {
			return false
		}
	}
	return true
}

// blockCallID 块数据中的 call_id。
// Python: trajectory.py:784-788 _block_call_id
func blockCallID(block harness_protocol.ContentBlock) string {
	data := harness_protocol.JsonValueToBuiltin(block.Data)
	m, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	return textOf(m["call_id"])
}

// messageMetadata 消息元数据（外部用户来源时陈述）。
// Python: trajectory.py:791-795 _message_metadata
func messageMetadata(message harness_protocol.TurnMessage) map[string]any {
	if message.Data == nil {
		return nil
	}
	if origin, ok := message.Data["origin"].(string); ok && origin == llmschemaOriginExternal() {
		return map[string]any{llmschemaOriginKey(): llmschemaOriginExternal()}
	}
	return nil
}

// turnIdentityAttributes 一个轮次内每个 span 都携带的轨迹轮次属性。
// Python: trajectory.py:582-589 _turn_identity_attributes
func turnIdentityAttributes(turnID string, turnNumber *int) map[string]attribute.Value {
	attributes := map[string]attribute.Value{}
	if turnID != "" {
		attributes[observability.OJTurnID] = attribute.StringValue(turnID)
	}
	if turnNumber != nil {
		attributes[observability.OJTurnNumber] = attribute.IntValue(*turnNumber)
	}
	return attributes
}

// owningRequest 返回引发一个 item 的请求的 inference id 与步号。
// Python: trajectory.py:592-598 _owning_request
func owningRequest(record *turnRecord, envelope harness_protocol.HarnessEvent) *requestIdentity {
	for _, causationID := range envelope.CausationIDs {
		if owner, ok := record.requests[causationID]; ok {
			return &owner
		}
	}
	return nil
}

// setRequestStatus 依据请求终态设置推理 span 状态。
// Python: trajectory.py:601-611 _set_request_status
func setRequestStatus(span trace.Span, event *harness_protocol.ModelRequestEvent, config *observability.ObservabilityConfig) {
	if event.Status == harness_protocol.ModelRequestStatusCompleted {
		span.SetStatus(codes.Ok, "")
		return
	}
	reason := string(event.Status)
	if event.Error != nil {
		reason = event.Error.Message
		if event.Error.Category != nil && *event.Error.Category != "" {
			span.SetAttributes(attribute.String(observability.ErrorType, *event.Error.Category))
		}
	}
	span.SetStatus(codes.Error, observability.RedactErrorSummary(reason, config))
}

// requestParameterAttributeValues 采样参数属性。
// Python: trajectory.py:614-627 _request_parameter_attributes
func requestParameterAttributeValues(event *harness_protocol.ModelRequestEvent) map[string]attribute.Value {
	attributes := map[string]attribute.Value{}
	parameters := harness_protocol.JsonValueToBuiltin(event.RequestParameters)
	params, ok := parameters.(map[string]any)
	if !ok {
		return attributes
	}
	names := make([]string, 0, len(requestParameterAttributes))
	for name := range requestParameterAttributes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		key := requestParameterAttributes[name]
		value := params[name]
		switch v := value.(type) {
		case string:
			attributes[key] = attribute.StringValue(v)
		case bool:
			attributes[key] = attribute.BoolValue(v)
		case int:
			attributes[key] = attribute.IntValue(v)
		case int64:
			attributes[key] = attribute.IntValue(int(v))
		case float64:
			attributes[key] = attribute.Float64Value(v)
		}
	}
	if stopSequences, ok := params["stop_sequences"].([]any); ok && len(stopSequences) > 0 {
		attributes[observability.GenAIRequestStopSequences] = attribute.StringValue(marshalJSON(stopSequences))
	}
	return attributes
}

// usageAttributeValues 用量属性（nil 值跳过）。
// Python: trajectory.py:630-640 _usage_attributes
func usageAttributeValues(event *harness_protocol.ModelRequestEvent) map[string]attribute.Value {
	usage := event.Usage
	if usage == nil {
		return map[string]attribute.Value{}
	}
	attributes := map[string]attribute.Value{}
	pairs := []struct {
		key   string
		value *int
	}{
		{observability.GenAIUsageInputTokens, usage.InputTokens},
		{observability.GenAIUsageOutputTokens, usage.OutputTokens},
		{observability.GenAIUsageCacheReadInputTokens, usage.CachedInputTokens},
		{observability.GenAIUsageReasoningOutputTokens, usage.ReasoningOutputTokens},
	}
	for _, pair := range pairs {
		if pair.value != nil {
			attributes[pair.key] = attribute.IntValue(*pair.value)
		}
	}
	return attributes
}

// textOf 值 → 文本：nil 为空串、字符串直取、其余 JSON 序列化（default=str 对应物）。
// Python: trajectory.py:798-803 _text
func textOf(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return marshalJSON(value)
}

// nsTime Unix 秒浮点 → OTel 时间（秒 × 1e9 纳秒）。
// Python: trajectory.py:806-807 _ns
func nsTime(timestamp float64) time.Time {
	return time.Unix(0, int64(timestamp*1_000_000_000))
}

// spanEndOption 收尾时间选项（nil 时间戳时无选项）
func spanEndOption(endTime *float64) []trace.SpanEndOption {
	if endTime == nil {
		return nil
	}
	return []trace.SpanEndOption{trace.WithTimestamp(time.Unix(0, int64(*endTime*1_000_000_000)))}
}

// isEmptyBuiltin 判断内建值为空容器
func isEmptyBuiltin(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case map[string]any:
		return len(v) == 0
	case []any:
		return len(v) == 0
	default:
		return false
	}
}

// isErrorTrue 判断 is_error 是否显式为 true。Python: values.get("is_error") is True
func isErrorTrue(value any) bool {
	b, ok := value.(bool)
	return ok && b
}

// valueMapToAttributes 值映射 → OTel 属性（键排序保证确定性；值类型原样保留，
// Python 侧无对应实现——Python dict 直接作 span 属性，Go 需显式展开）
func valueMapToAttributes(m map[string]attribute.Value) []attribute.KeyValue {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	attrs := make([]attribute.KeyValue, 0, len(keys))
	for _, key := range keys {
		attrs = append(attrs, attribute.KeyValue{Key: attribute.Key(key), Value: m[key]})
	}
	return attrs
}

// mergeAttributes 合并两个属性映射（后者覆盖前者）
func mergeAttributes(base, overlay map[string]attribute.Value) []attribute.KeyValue {
	merged := make(map[string]attribute.Value, len(base)+len(overlay))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overlay {
		merged[key] = value
	}
	return valueMapToAttributes(merged)
}

// marshalJSON JSON 序列化（ensure_ascii=False 语义）
func marshalJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}
