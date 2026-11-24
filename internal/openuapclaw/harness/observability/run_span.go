package harnessobservability

import (
	"context"
	"reflect"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	obs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability"
	extdemand "github.com/uapclaw/uapclaw-go/internal/openuapclaw/extensions/observability/demand"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
)

// run_span.go 单 agent 运行根 span。
// Python: harness/observability/run_span.py（全文 333 行）。
// OtelCallbackHandler 在无父 span 时跳过 LLM/tool span 创建
// （callback_handler._get_parent_context_for_llm_tool）；单 agent 运行既没有
// team span 也没有 current agent span——没有根 span 则 init 后依然零 span。
// 这里开根 span 并注册为运行根（与 Team 内部 get_or_create_team_span 同机制），
// 使 LLM/tool span 嵌套其下并被导出。
//
// 用法（必须配对，同一协程以使上下文传播进 runner 的 LLM 调用）：
//
//	handle := OpenAgentRunSpan(sessionID, mode, ...)
//	defer CloseAgentRunSpan(handle, sessionID, &answer, err, "", "")

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildRunSpanName 构造层级化 span 名：agent.<mode>.<session_id>。
// mode 为宿主请求模式（形如 <category>.<submode>，如 agent.plan / code.normal）。
// 任一成分缺失时优雅回退。Python: build_run_span_name（run_span.py:41-67）。
func BuildRunSpanName(mode, sessionID string) string {
	normalizedMode := strings.TrimSpace(mode)
	normalizedSession := strings.TrimSpace(sessionID)
	if normalizedMode == "" {
		if normalizedSession != "" {
			return "agent.run." + normalizedSession
		}
		return "agent.run"
	}
	if normalizedSession == "" {
		return "agent." + normalizedMode + ".run"
	}
	return "agent." + normalizedMode + "." + normalizedSession
}

// OpenAgentRunSpan 打开单 agent 运行的根 span。
// Python: open_agent_run_span（run_span.py:69-175）。返回不透明句柄供
// CloseAgentRunSpan 使用；tracing 未开 / provider 未初始化时返回 nil（关闭为
// no-op）。属性对齐 Python base_attributes。
func OpenAgentRunSpan(sessionID, mode, requestID, runID, turnID string, turnNumber int, subject *harness.ExecutionSubject) trace.Span {
	if !IsInitialized() || !IsTracingEnabled() {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).Any("panic", rec).
				Msg("[AgentObservability] open root span failed")
		}
	}()
	tracer := obs.GetTracer(runTracerName)
	name := BuildRunSpanName(mode, sessionID)
	attrs := []attribute.KeyValue{
		attribute.String(obs.OJAgentMode, mode),
		attribute.Bool(obs.OJTraceRoot, true),
		attribute.String(obs.OJTrajectorySchemaVersion, obs.TrajectorySpanSchemaVersion),
		attribute.String(obs.GenAIOperationName, "invoke_agent"),
		attribute.String(obs.OJTrajectoryRecordKind, "turn"),
	}
	if subject != nil {
		attrs = append(attrs,
			attribute.String(obs.OJExecutionSubjectID, subject.SubjectID),
			attribute.String(obs.OJExecutionSubjectDisplayName, subject.DisplayName),
			attribute.String(obs.OJExecutionSubjectKind, subject.Kind),
			attribute.String(obs.OJExecutionSubjectSessionID, orDefault(subject.SessionID, sessionID)),
		)
		if subject.ParentSubjectID != "" {
			attrs = append(attrs, attribute.String(obs.OJExecutionSubjectParentID, subject.ParentSubjectID))
		}
	} else {
		attrs = append(attrs,
			attribute.String(obs.OJExecutionSubjectID, "main"),
			attribute.String(obs.OJExecutionSubjectDisplayName, "Main Agent"),
			attribute.String(obs.OJExecutionSubjectKind, "main_agent"),
			attribute.String(obs.OJExecutionSubjectSessionID, sessionID),
		)
	}
	if sessionID != "" {
		attrs = append(attrs, attribute.String(obs.GenAIConversationID, sessionID))
	}
	if requestID != "" {
		attrs = append(attrs, attribute.String(obs.OJRequestID, requestID))
	}
	if runID != "" {
		attrs = append(attrs, attribute.String(obs.OJRunID, runID))
	}
	if turnID != "" {
		attrs = append(attrs, attribute.String(obs.OJTurnID, turnID))
	}
	if turnNumber > 0 {
		attrs = append(attrs, attribute.Int(obs.OJTurnNumber, turnNumber))
	}

	_, span := tracer.Start(
		context.Background(),
		name,
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindServer),
	)
	// 注册为运行根：父查找在创建 LLM/tool span 时能命中。session id 同时进
	// 共享注册表与本地兜底表——supervisor 任务可能不继承 ContextVar。
	obs.SetRootSpan(span, sessionID)
	obs.SetCurrentSessionID(sessionID)
	RegisterRunRootSpan(span, sessionID)
	logger.Info(logComponent).Str("name", name).Msg("[AgentObservability] root span opened")
	return span
}

// StampRunOutput 把运行最终答案写到根 span 作为迹线输出。
// Python: stamp_run_output（run_span.py:177-206）——团队侧在 leader 的
// iteration 结果上盖等价属性（按 TeamRole.LEADER 触发，单 agent 永不触发，
// 迹线顶层输出会空）；单 agent 对应物就是运行的最终答案，在此盖章。
// redaction 按 active config（redact_completions 同样覆盖本属性）。
func StampRunOutput(handle trace.Span, output string, allowEmpty bool) {
	if handle == nil || (output == "" && !allowEmpty) {
		return
	}
	text := output
	if cfg := extdemand.GetConfig(); cfg != nil {
		text = obs.RedactCompletion(text, cfg)
	}
	handle.SetAttributes(attribute.String(obs.OJSpanOutput, text))
}

// CloseAgentRunSpan 收口 OpenAgentRunSpan 打开的根 span 并清除注册。
// Python: close_agent_run_span（run_span.py:208-330）。output 为 nil 表示不设
// 输出属性；显式空串是真实可观测结果（用 allowEmpty 路径）。exception 为运行
// 失败/取消记录在根上；errorType/errorMessage 为协议 in-band 报错的结构化
// 终态（原样记录，绝不合成异常类改写）。
func CloseAgentRunSpan(handle trace.Span, sessionID string, output *string, err error, errorType, errorMessage string) {
	// 只清本运行的注册项——会话重叠时误清会弄瞎仍在跑的运行（其子 agent
	// 会中途丢 span）。Python: unregister_run_root_span（run_span.py:248-250）。
	UnregisterRunRootSpan(handle, sessionID)
	if handle == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).Any("panic", rec).
				Msg("[AgentObservability] close root span failed")
		}
	}()

	// 双安全网（对齐 Python 注释 run_span.py:262-278）：
	//   ①cascade——收口本 context 上 llm/tool 栈的 span；
	//   ②flush——SpanProcessor 支撑的安全网（tracker 按 trace_id 收，跨 context）。
	// 都须在根绑定清除之前；flush 只限本迹线（flush_spans_for_trace），
	// 并发运行不受影响。根被按 span id 显式排除，保持 recording 到全部子 span
	// 结束——其最终记录因此是增量轨迹消费者可靠的完成标记。
	traceID := ""
	if sc := handle.SpanContext(); sc.IsValid() {
		traceID = sc.TraceID().String()
	}
	ctx := obs.WithSessionID(context.Background(), sessionID)
	forced := obs.CascadeCloseChildren(ctx)
	forced += obs.FlushChildSpans(traceID)
	if forced > 0 {
		handle.SetAttributes(attribute.Bool(obs.OJTraceForcedClose, true))
	}

	if output != nil {
		StampRunOutput(handle, *output, true)
	}

	if err != nil {
		handle.RecordError(err)
	}
	resolvedType := errorTypeName(err)
	resolvedMessage := err.Error()
	if err == nil {
		resolvedType = strings.TrimSpace(errorType)
		resolvedMessage = strings.TrimSpace(errorMessage)
		if resolvedType == "" && resolvedMessage == "" {
			// 无失败事实：正常 OK 收口。
			handle.SetStatus(codes.Ok, "")
			handle.SetAttributes(attribute.Bool(obs.OJTraceComplete, true))
			handle.End()
			obs.ClearRootSpan(sessionID, handle)
			obs.ClearRootSpan("", nil)
			return
		}
	}
	// 抛出的异常是权威失败事实；结构化字段只为 in-band 报错的协议存在，
	// 绝不改写真实异常（对齐 run_span.py:297-301 注释）。
	if resolvedType != "" {
		handle.SetAttributes(attribute.String(obs.ErrorType, resolvedType))
	}
	handle.SetStatus(codes.Error, orDefault(resolvedMessage, resolvedType))
	handle.SetAttributes(attribute.Bool(obs.OJTraceComplete, true))
	handle.End()
	obs.ClearRootSpan(sessionID, handle)
	obs.ClearRootSpan("", nil)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// orDefault 返回 v 非空则 v 否则 fallback。
func orDefault(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

// errorTypeName 异常的 type 名（Python type(exception).__name__ 等价）。
func errorTypeName(err error) string {
	if err == nil {
		return ""
	}
	t := reflect.TypeOf(err)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil {
		return ""
	}
	return t.Name()
}
