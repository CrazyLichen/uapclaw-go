package harnessobservability

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
)

// rail_helpers.go rail 的共享助手。Python: rail.py 各 private 方法与
// extensions/observability/error_reporting.py record_span_error 一比一。

// ──────────────────────────── 导出函数 ────────────────────────────

// RecordSpanError 把一次错误结局写到 recording span 上并收口。
// 记录真实异常（保 stacktrace，message 换成脱敏摘要），设 error.type 与错误
// 状态，然后收口。Python: record_span_error（error_reporting.py:62-119）；
// Go 侧 stacktrace 脱敏函数未移植，RecordError 用原始异常（message 经
// RedactErrorSummary 体现在 status）——偏差登记 spec §4。
func RecordSpanError(span trace.Span, exception error, config *observability.ObservabilityConfig) {
	if span == nil || !span.IsRecording() {
		return
	}
	reason := ""
	if exception != nil {
		reason = observability.RedactErrorSummary(exception.Error(), config)
	}
	span.RecordError(exception)
	if exception != nil {
		span.SetAttributes(attribute.String(observability.ErrorType, errorTypeName(exception)))
	}
	span.SetStatus(codes.Error, reason)
	span.End()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// spanStateOf 从钩子 ctx 取 OtelSpanState（nil 安全）。
func spanStateOf(ctx context.Context) *observability.OtelSpanState {
	if ctx == nil {
		return nil
	}
	return observability.SpanStateFromCtx(ctx)
}

// tracer rail 的 tracer（注入优先）。
// Python: _tracer（rail.py:384-390）。
func (r *AgentObservabilityRail) tracer() trace.Tracer {
	if r.injectedTracer != nil {
		return r.injectedTracer
	}
	return observability.GetTracer(railTracerNameRail)
}

// rootSpanFor 只解析本回调具体 session 所属的运行根。
// Python: _root_span_for（rail.py:474-487）。
func (r *AgentObservabilityRail) rootSpanFor(cbc *agentinterfaces.AgentCallbackContext) trace.Span {
	sessionID := ""
	if cbc.Session() != nil {
		sessionID = cbc.Session().GetSessionID()
	}
	return observability.GetRootSpan(sessionID)
}

// currentAgentSpan 读当前 agent span（OtelSpanState "current" 槽）。
// Python: get_current_agent_span。
func currentAgentSpan(state *observability.OtelSpanState) trace.Span {
	if state == nil {
		return nil
	}
	return state.PeekAgentSpan(agentSpanCurrentKey)
}

// currentToolSpan 读当前 tool span（栈顶）。
// Python: get_current_tool_span。
func currentToolSpan(state *observability.OtelSpanState) trace.Span {
	if state == nil {
		return nil
	}
	return state.PeekAnyToolSpan()
}

// PushToolSpanCurrent / PopToolSpanCurrent "当前 tool span" 栈操作。
// Python: push_tool_span / pop_tool_span（ToolSpanScope.close 用）。
func PushToolSpanCurrent(state *observability.OtelSpanState, toolName string, span trace.Span) {
	if state != nil {
		state.PushToolSpan(toolName, span)
	}
}

// PopToolSpanCurrent 弹出当前 tool span。
func PopToolSpanCurrent(state *observability.OtelSpanState, toolName string) trace.Span {
	if state != nil {
		return state.PopToolSpan(toolName)
	}
	return nil
}

// deepEnableTaskLoop 读 agent 的 enable_task_loop 配置。
// Python: getattr(agent, "deep_config").enable_task_loop（rail.py:687）。
func deepEnableTaskLoop(agent agentinterfaces.BaseAgent) bool {
	d, ok := agent.(interface{ DeepConfig() any })
	if !ok {
		return false
	}
	// DeepAgent.DeepConfig 返回 *hschema.DeepAgentConfig；反射读 EnableTaskLoop
	// 以免 harness 观测包直接依赖 DeepAgent 具体类型（仅用接口断言）。
	cfg := reflect.ValueOf(d.DeepConfig())
	if cfg.Kind() == reflect.Ptr {
		cfg = cfg.Elem()
	}
	if !cfg.IsValid() || cfg.Kind() != reflect.Struct {
		return false
	}
	f := cfg.FieldByName("EnableTaskLoop")
	return f.IsValid() && f.Kind() == reflect.Bool && f.Bool()
}

// ResolveAgentName 返回本 agent span 命名用的稳定标识。
// 来源优先级：①member_name（TeamAgent 属性，派发成员的权威 id）；
// ②build_context.member_name（NativeHarness/DeepAgent 暴露 build context，
// leader 的来源——leader 无 member_name 属性，card.name 是 display_name）；
// ③card.name（两者皆无的 agent 兜底，如子 agent 的类型名）。
// 非字符串值被拒绝使回退链继续。Python: resolve_agent_name（rail.py:1096-1145）。
func (r *AgentObservabilityRail) ResolveAgentName(agent agentinterfaces.BaseAgent) string {
	if agent == nil {
		return ""
	}
	// ①/②：鸭子式断言（Python getattr 链的 Go 等价）。
	if m, ok := agent.(interface{ GetMemberName() string }); ok {
		if name := m.GetMemberName(); name != "" {
			return name
		}
	}
	if m, ok := agent.(interface {
		GetBuildContext() interface{ GetMemberName() string }
	}); ok {
		if bc := m.GetBuildContext(); bc != nil {
			if name := bc.GetMemberName(); name != "" {
				return name
			}
		}
	}
	// ③：card.name。
	card := agent.Card()
	if card != nil && card.Name != "" {
		return card.Name
	}
	return ""
}

// agentAttributes 迭代/invoke/Step span 共享的属性集。
// 关联属性来自运行根，唯 turn 身份由 scope parent 覆盖时以其为准（单 agent
// 根陈述 turn、每个 agent span 复制——parent 值即根值；team 根不陈述 turn，
// 一个迹线多个成员 turn——成员 span 陈述、Step 与嵌套子 span 从它继承）。
// Python: _stamp_agent_attributes（rail.py:1147-1234）。
func (r *AgentObservabilityRail) agentAttributes(agent agentinterfaces.BaseAgent, agentName string, rootSpan, scopeParent trace.Span) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(observability.GenAIOperationName, "invoke_agent"),
		attribute.String(observability.OJTrajectorySchemaVersion, observability.TrajectorySpanSchemaVersion),
		attribute.String(observability.OJTrajectoryRecordKind, "agent"),
	}
	if agentName != "" {
		attrs = append(attrs, attribute.String(observability.GenAIAgentName, agentName))
	}
	card := agent.Card()
	if card != nil {
		if card.ID != "" {
			attrs = append(attrs, attribute.String(observability.GenAIAgentID, card.ID))
		}
		if card.Description != "" {
			attrs = append(attrs, attribute.String(observability.GenAIAgentDescription, card.Description))
		}
	}
	if rootSpan != nil {
		for _, kv := range copyAttrsFrom(rootSpan,
			observability.GenAIConversationID,
			observability.OJRequestID, observability.OJRunID,
			observability.OJTurnID, observability.OJTurnNumber,
			observability.OJExecutionSubjectID, observability.OJExecutionSubjectDisplayName,
			observability.OJExecutionSubjectKind, observability.OJExecutionSubjectParentID,
			observability.OJExecutionSubjectSessionID,
		) {
			attrs = append(attrs, kv)
		}
	}
	// scope parent 陈述了 turn 则以其覆盖。
	if scopeParent != nil && parentHasAttr(scopeParent, observability.OJTurnID) {
		for _, kv := range copyAttrsFrom(scopeParent, observability.OJTurnID, observability.OJTurnNumber) {
			attrs = append(attrs, kv)
		}
	}
	// 会话 id 兜底：根未复制到 gen_ai.conversation.id 时以当前 session 填充
	// （导出适配器总能推出 session）。根复制的值权威，此处不覆盖。
	if !attrsHave(attrs, observability.GenAIConversationID) {
		if sid := observability.GetCurrentSessionID(); sid != "" {
			attrs = append(attrs, attribute.String(observability.GenAIConversationID, sid))
		}
	}
	return attrs
}

// applyDecoration 应用 query 输入镜像与装饰属性。
// Python: before_* 中 query/loop_event 段 + decoration.attributes 终局应用。
func applyDecoration(span trace.Span, decoration *AgentSpanDecoration, query string, config *observability.ObservabilityConfig) {
	if query != "" {
		redactedQuery := query
		if config != nil {
			redactedQuery = observability.RedactPrompt(query, config)
		}
		span.SetAttributes(attribute.String(observability.OJSpanInput, redactedQuery))
		for _, key := range decoration.InputAttributeKeys {
			span.SetAttributes(attribute.String(key, redactedQuery))
		}
	}
	for _, kv := range decoration.Attributes {
		span.SetAttributes(kv)
	}
}

// copyParentCorrelation 从父 span 复制关联属性到工具 span。
// Python: _copy_parent_correlation（rail.py:1040-1064）。
func copyParentCorrelation(parent trace.Span, span trace.Span) {
	for _, kv := range copyAttrsFrom(parent,
		observability.GenAIConversationID,
		observability.GenAIAgentDescription, observability.GenAIAgentID,
		observability.GenAIAgentName, observability.GenAIAgentVersion,
		observability.OJRequestID, observability.OJRunID,
		observability.OJTurnID, observability.OJTurnNumber,
		observability.OJExecutionSubjectID, observability.OJExecutionSubjectDisplayName,
		observability.OJExecutionSubjectKind, observability.OJExecutionSubjectParentID,
		observability.OJExecutionSubjectSessionID,
		observability.OJStepID, observability.OJStepNumber,
	) {
		span.SetAttributes(kv)
	}
}

// drainOrClearStale 开新 span 前清理当前 context 里的残留 agent span。
// 自己的孤儿（同 agent、同迹线）→ 排空子 span 并收口；其他 → 别的 agent 任务
// 继承的 ContextVar 快照，或同 agent 并发运行的另一会话——不动那个 span，
// 只清继承的 llm/tool 栈。随后总是清 current。
// 迹线比对是重叠会话安全的关键：同 agent 名出现在多个会话，仅按名相等会让
// 一个会话的新一轮收掉另一会话仍开着的 span。Python: _drain_or_clear_stale
// （rail.py:1066-1120）。
func (r *AgentObservabilityRail) drainOrClearStale(agentName string, rootSpan trace.Span, state *observability.OtelSpanState) {
	prev := currentAgentSpan(state)
	if prev == nil || !prev.IsRecording() {
		return
	}
	if prev == r.openInvokeSpan {
		// 本 agent 自己的活 invoke span，迭代即将嵌它下面——不是残留，
		// 在此收口会把请求的 span 在第一轮就截断。
		return
	}
	prevName := ""
	if ro, ok := prev.(sdktrace.ReadOnlySpan); ok {
		prevName = readOnlyAttrString(ro, observability.GenAIAgentName)
	}
	sameRun := rootSpan != nil &&
		prev.SpanContext().TraceID() == rootSpan.SpanContext().TraceID()
	if prevName == agentName && sameRun {
		logger.Warn(logComponent).Str("name", prevName).
			Msg("[AgentObservability] closing orphan agent span")
		observability.CascadeCloseChildren(observability.WithSpanState(context.Background(), state))
		observability.MarkSpanForcedClose(prev, orphanAgentForcedCloseReason)
		prev.End()
	} else {
		logger.Info(logComponent).
			Str("prev_name", prevName).Str("current_agent", agentName).
			Bool("same_run", sameRun).
			Msg("[AgentObservability] clearing stale agent span inherited from another task")
		// 清工具 span 栈让新 agent 干净开始。LLM span 无需等价清理：
		// ActiveSpanTracker 按请求 id 索引，继承 context 不可能让本 agent
		// 解析到别的 agent 的 span。
		// Go：ClearToolSpanContext 的等价物（偏差 spec §4）——保留栈。
	}
	if state != nil {
		state.PopAgentSpan(agentSpanCurrentKey)
	}
}

// publishOpenStep 记录开启中 Step 的身份，供 resume 重_join。
// HITL resume 在自己的请求里收口被中断 Step 的工具，无 Step span 可继承、
// 原 span id 早已不在——持久化身份使 openjiuwen.step.id 成为重放工作可携带
// 的事实。Python: _publish_open_step（rail.py:392-418）。
func publishOpenStep(cbc *agentinterfaces.AgentCallbackContext, stepID string, stepNumber int) {
	session := cbc.Session()
	if session == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			logger.Debug(logComponent).Any("panic", rec).
				Msg("[AgentObservability] open step publish failed")
		}
	}()
	session.UpdateState(map[string]any{
		openStepStateKey: map[string]any{"step_id": stepID, "step_number": stepNumber},
	})
}

// persistedStep 会话恢复的 Step 身份。
type persistedStep struct {
	stepID     string
	stepNumber int
}

// resolvePersistedStep 读回 resume 正在继续的 Step 身份。
// 快照不可读不得弄失败工具调用。Python: _resolve_persisted_step（rail.py:420-452）。
func resolvePersistedStep(cbc *agentinterfaces.AgentCallbackContext) *persistedStep {
	session := cbc.Session()
	if session == nil {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			logger.Debug(logComponent).Any("panic", rec).
				Msg("[AgentObservability] open step read failed")
		}
	}()
	raw, err := session.GetState(sessionstate.StringKey(openStepStateKey))
	if err != nil || raw == nil {
		return nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	stepID := fmt.Sprintf("%v", m["step_id"])
	num, _ := m["step_number"].(int)
	if stepID == "" || stepID == "<nil>" || num <= 0 {
		return nil
	}
	return &persistedStep{stepID: stepID, stepNumber: num}
}

// inputsReactIteration 读当前事件输入的 react_iteration（工具阶段从
// extra["_react_iteration"] 回读；ModelCallInputs 直读）。
// Python: getattr(inputs, "react_iteration", 0)。
func inputsReactIteration(cbc *agentinterfaces.AgentCallbackContext) int {
	if inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs); ok {
		return inputs.ReactIteration
	}
	return 0
}

// resolveAbilityCard 经 agent 的 ability manager 查工具卡。
// Python: _resolve_ability_card（rail.py:1022-1034）。
func resolveAbilityCard(agent agentinterfaces.BaseAgent, toolName string) *abilityCardInfo {
	if agent == nil {
		return nil
	}
	mgr := agent.AbilityManager()
	if mgr == nil {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			logger.Debug(logComponent).Any("panic", rec).Msg("otel: ability card lookup failed")
		}
	}()
	ability := mgr.Get(toolName)
	if ability == nil {
		return nil
	}
	info := &abilityCardInfo{}
	if m, ok := ability.(interface{ AbilityID() string }); ok {
		info.ID = m.AbilityID()
	}
	if m, ok := ability.(interface{ GetDescription() string }); ok {
		info.Description = m.GetDescription()
	}
	return info
}

// abilityCardInfo 工具卡信息投影（id + description）。
type abilityCardInfo struct {
	ID          string
	Description string
}

// abilityProtocol 解析工具协议标记（mcp 启发式：类型名含 mcp）。
// Python: _ability_protocol（rail.py:1036-1058）——mcp scope 解析在 Go 的
// ability manager 无对应方法，保留 card 类型名启发式（偏差 spec §4）。
func abilityProtocol(card *abilityCardInfo) string {
	if card == nil {
		return ""
	}
	className := strings.ToLower(typeName(card))
	if strings.Contains(className, "mcp") {
		return "mcp"
	}
	return ""
}

// copyAttrsFrom 从 span 的属性里摘指定键（ReadOnlySpan 断言）。
func copyAttrsFrom(span trace.Span, keys ...string) []attribute.KeyValue {
	ro, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		return nil
	}
	byKey := map[string]attribute.Value{}
	for _, kv := range ro.Attributes() {
		byKey[string(kv.Key)] = kv.Value
	}
	var out []attribute.KeyValue
	for _, key := range keys {
		if v, ok := byKey[key]; ok {
			out = append(out, attribute.KeyValue{Key: attribute.Key(key), Value: v})
		}
	}
	return out
}

// parentHasAttr 判 span 是否带某属性。
func parentHasAttr(span trace.Span, key string) bool {
	ro, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		return false
	}
	for _, kv := range ro.Attributes() {
		if string(kv.Key) == key {
			return true
		}
	}
	return false
}

// readOnlyAttrBool / readOnlyAttrString 属性读取助手。
func readOnlyAttrBool(ro sdktrace.ReadOnlySpan, key string) bool {
	for _, kv := range ro.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsBool()
		}
	}
	return false
}

func readOnlyAttrString(ro sdktrace.ReadOnlySpan, key string) string {
	for _, kv := range ro.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

// attrsHave 判属性集是否已含键。
func attrsHave(attrs []attribute.KeyValue, key string) bool {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return true
		}
	}
	return false
}

// scopeParentAgentName 读父 span 的 agent 名（Step span 命名用）。
// Python: scope_parent.attributes.get(GEN_AI_AGENT_NAME)（rail.py:818-822）。
func scopeParentAgentName(scopeParent trace.Span) string {
	if ro, ok := scopeParent.(sdktrace.ReadOnlySpan); ok {
		return readOnlyAttrString(ro, observability.GenAIAgentName)
	}
	return ""
}

// recoverRail 钩子统一 panic 兜底（Python 每方法 try/except + warning）。
func recoverRail(method string) {
	if rec := recover(); rec != nil {
		logger.Warn(logComponent).Str("method", method).Any("panic", rec).
			Msg("[AgentObservability] rail hook failed")
	}
}

// typeName 类型名（abilityProtocol 启发式用）。
func typeName(v any) string {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil {
		return ""
	}
	return t.Name()
}

// invokeQueryText InvokeQuery 的文本化（Python getattr(inputs, "query") 等价）。
func invokeQueryText(q any) string {
	switch v := q.(type) {
	case nil:
		return ""
	case string:
		return v
	case agentinterfaces.InvokeQuery:
		if s, ok := v.(interface{ String() string }); ok {
			return s.String()
		}
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
