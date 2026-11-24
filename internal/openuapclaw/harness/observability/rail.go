package harnessobservability

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	extdemand "github.com/uapclaw/uapclaw-go/internal/openuapclaw/extensions/observability/demand"
	rails "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
)

// rail.py AgentObservabilityRail——每个任务循环迭代一个 AGENT span，
// 或单轮 invoke 一个。Python: harness/observability/rail.py（全文 1265 行，
// 一比一；Go 侧父链经显式 SpanContext 注入替代 Python 的 otel_context
// attach/detach——偏差登记 spec §4）。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// railTracerNameRail rail 层 tracer 名。
	// Python: _TRACER_NAME = "openjiuwen.harness.observability.rail"（rail.py:115）。
	railTracerNameRail = "openjiuwen.harness.observability.rail"
	// orphanAgentForcedCloseReason 孤儿 agent span 强制收口原因。
	// Python: _ORPHAN_AGENT_FORCED_CLOSE_REASON（rail.py:116）。
	orphanAgentForcedCloseReason = "missing_agent_terminal_callback"
	// openStepStateKey 开启中 Step 的会话状态键。
	// Python: OPEN_STEP_STATE_KEY = "_observability_open_step"（rail.py:122）。
	openStepStateKey = "_observability_open_step"

	// decorationCtxKeyDecoration 待应用贡献的 ctx.extra 键。
	// Python: AgentSpanDecoration._CTX_KEY（rail.py:169）。
	decorationCtxKeyDecoration = "_otel_agent_span_decoration"
	// scopeCtxKey 开启中 agent span scope 的 ctx.extra 键。
	// Python: AgentSpanScope._CTX_KEY（rail.py:204）。
	scopeCtxKey = "_otel_agent_scope"
	// toolScopesCtxKey 工具 span scope 的 ctx.extra 键。
	// Python: ToolSpanScope._CTX_KEY（rail.py:284）。
	toolScopesCtxKey = "_otel_ability_tool_scopes"

	// agentSpanScopeKindIteration 迭代路径。Python: KIND_ITERATION。
	agentSpanScopeKindIteration = "iteration"
	// agentSpanScopeKindInvoke 单轮路径。Python: KIND_INVOKE。
	agentSpanScopeKindInvoke = "invoke"

	// agentSpanCurrentKey OtelSpanState 中"当前 agent span"的槽位 id。
	// Python: _current_agent_span ContextVar；Go 无 ContextVar，
	// 以 OtelSpanState.PushAgentSpan("current", span) 承载（偏差 spec §4）。
	agentSpanCurrentKey = "current"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentSpanDecoration 另一 rail 贡献给 agent span 的额外属性。
// 贡献 rail 在 before_* 钩子构造并 park；本 rail 在开的 span 上应用。
// 两个镜像键列表存在的原因：贡献方的命名空间常以自己的键复刻通用
// 输入/输出属性，输出只有 span 关闭时才可知。
// Python: AgentSpanDecoration（rail.py:145-177）。
type AgentSpanDecoration struct {
	// Attributes span 打开时原样应用。
	Attributes []attribute.KeyValue
	// InputAttributeKeys 脱敏后 query 镜像进的键。
	InputAttributeKeys []string
	// OutputAttributeKeys 关闭时脱敏结果镜像进的键。
	OutputAttributeKeys []string
}

// Park 把本贡献递交给即将打开的 agent span。
// Python: park（rail.py:172-174）。
func (d *AgentSpanDecoration) Park(cbc *agentinterfaces.AgentCallbackContext) {
	cbc.Extra()[decorationCtxKeyDecoration] = d
}

// CollectDecoration 返回本 context 上已递交的贡献，无则空贡献。
// Python: collect（rail.py:176-180）。
func CollectDecoration(cbc *agentinterfaces.AgentCallbackContext) *AgentSpanDecoration {
	if d, ok := cbc.Extra()[decorationCtxKeyDecoration].(*AgentSpanDecoration); ok {
		return d
	}
	return &AgentSpanDecoration{}
}

// AgentSpanScope 持有单个开启中 agent span 的生命周期与嵌套决策。
// 嵌套按结构决定：当前 agent span（OtelSpanState "current" 槽）只要仍在
// recording 就是合法父——不看任何层枚举。scope 记住嵌套的父，close 时恢复
// 其为 current。嵌套路径不动继承的 llm/tool 栈（属于仍开启的父，由父的
// scope 收口）；cascade-close 仅在最外层 agent scope 运行。
// scope 在 span 存续期挂在 ctx.extra——before_task_iteration/before_invoke 开，
// 对应 after_* 取回。ctx.extra 按 callback context 隔离，不会像 ContextVar
// 那样在 iteration/invoke 嵌套下跨任务泄漏。
// Python: AgentSpanScope（rail.py:183-317）。
type AgentSpanScope struct {
	// Span 本 scope 的 agent 层 span。
	Span trace.Span
	// Kind iteration / invoke。
	Kind string
	// ParentAgentSpan 打开时为 current 的 agent span，close 时恢复；
	// 直接嵌在运行根下（无 agent 层父）时为 nil。
	ParentAgentSpan trace.Span
	// IsOutermost 本 scope 是否拥有子 llm/tool span 的 cascade-close
	// （迭代路径，或无 agent 父的 invoke scope）。
	IsOutermost bool
	// config 开启时捕获的观测配置；nil 时 close 存原始输出串。
	config *observability.ObservabilityConfig
	// outputAttributeKeys close 时脱敏输出镜像进的键。
	outputAttributeKeys []string
}

// CurrentAgentSpanScope 返回挂在本回调 context 上的 scope，无则 nil。
// Python: current（rail.py:237-240）。
func CurrentAgentSpanScope(cbc *agentinterfaces.AgentCallbackContext) *AgentSpanScope {
	if s, ok := cbc.Extra()[scopeCtxKey].(*AgentSpanScope); ok {
		return s
	}
	return nil
}

// Attach 把 scope 挂上回调 context，供对应 after_* 取回。
// Python: attach（rail.py:241-243）。
func (s *AgentSpanScope) Attach(cbc *agentinterfaces.AgentCallbackContext) {
	cbc.Extra()[scopeCtxKey] = s
}

// DetachAgentSpanScope 弹出并返回挂在本 context 上的 scope。
// Python: detach（rail.py:245-248）。
func DetachAgentSpanScope(cbc *agentinterfaces.AgentCallbackContext) *AgentSpanScope {
	s := CurrentAgentSpanScope(cbc)
	if s != nil {
		delete(cbc.Extra(), scopeCtxKey)
	}
	return s
}

// Close 收口本 scope 的 span 并恢复父为 current。
// Python: close（rail.py:250-316）。
func (s *AgentSpanScope) Close(output any, exception error, state *observability.OtelSpanState) {
	span := s.Span
	if !span.IsRecording() {
		return
	}
	if output != nil {
		outputStr := fmt.Sprintf("%v", output)
		redacted := outputStr
		if s.config != nil {
			redacted = observability.RedactCompletion(outputStr, s.config)
		}
		span.SetAttributes(attribute.String(observability.OJSpanOutput, redacted))
		for _, key := range s.outputAttributeKeys {
			span.SetAttributes(attribute.String(key, redacted))
		}
	}

	if s.IsOutermost {
		// Python: cascade_close_children() 无参（ContextVar 取 state）；
		// Go 经 state 显式携带（偏差 spec §4）。
		observability.CascadeCloseChildren(observability.WithSpanState(context.Background(), state))
	}

	if exception != nil {
		RecordSpanError(span, exception, s.config)
	} else {
		span.SetStatus(codes.Ok, "")
		span.End()
	}

	// 恢复父 agent span（无则 None），父的后续 llm/tool span 正确续接嵌套。
	if state != nil {
		if s.ParentAgentSpan != nil {
			state.PushAgentSpan(agentSpanCurrentKey, s.ParentAgentSpan)
		} else {
			state.PopAgentSpan(agentSpanCurrentKey)
		}
	}
}

// ToolSpanScope 跨 rail 回调持有一个 AbilityManager 调用 span。
// Python: ToolSpanScope（rail.py:319-363）。
type ToolSpanScope struct {
	Span     trace.Span
	ToolName string
	config   *observability.ObservabilityConfig
	// state 所属运行的 span 栈（Python ContextVar 的 Go 显式承载）。
	state *observability.OtelSpanState
}

// AttachToolSpanScope 把工具 scope 挂上回调 context。
// Python: attach（rail.py:329-332）——ctx.extra 按事件为键；Go cbc 已按
// 事件隔离，直接单槽（偏差 spec §4）。
func AttachToolSpanScope(cbc *agentinterfaces.AgentCallbackContext, s *ToolSpanScope) {
	cbc.Extra()[toolScopesCtxKey] = s
}

// DetachToolSpanScope 弹出工具 scope。
// Python: detach（rail.py:334-344）。
func DetachToolSpanScope(cbc *agentinterfaces.AgentCallbackContext) *ToolSpanScope {
	if s, ok := cbc.Extra()[toolScopesCtxKey].(*ToolSpanScope); ok {
		delete(cbc.Extra(), toolScopesCtxKey)
		return s
	}
	return nil
}

// Close 收口工具 span，记录结果与真实结局。
// 输出每条路径都写——含抛异常的调用：ability manager 同样把工具结果递给
// 模型，没有输出的 span 会让轨迹与对话不一致。状态从结局读而非"是否抛过"，
// 返回 success=false 的工具不再记成 OK。
// Python: close（rail.py:346-316 段内）。
func (s *ToolSpanScope) Close(output any, exception error) {
	popped := PopToolSpanCurrent(s.state, s.ToolName)
	span := s.Span
	if popped != nil && popped != span {
		// 调用方违反预期栈序时保全嵌套不同的 span；收掉别人的 span
		// 比留给活动 span 安全网更糟。
		PushToolSpanCurrent(s.state, s.ToolName, popped)
	}
	if !span.IsRecording() {
		return
	}

	recordedOutput := output
	if exception != nil {
		recordedOutput = observability.ToolResultForException(exception)
	}
	rawCallResult := "null"
	if recordedOutput != nil {
		rawCallResult = SerializeAbilityValue(recordedOutput)
	}
	redactedCallResult := rawCallResult
	if s.config != nil {
		redactedCallResult = observability.RedactCompletion(rawCallResult, s.config)
	}
	span.SetAttributes(
		attribute.String(observability.GenAIToolCallResult, redactedCallResult),
		attribute.String(observability.OJSpanOutput, redactedCallResult),
	)

	if exception != nil {
		RecordSpanError(span, exception, s.config)
		return
	}

	if failureReason := observability.ToolFailureReason(output); failureReason == "" {
		span.SetStatus(codes.Ok, "")
	} else {
		span.SetAttributes(attribute.String(observability.ErrorType, observability.ToolReportedFailure))
		summary := failureReason
		if s.config != nil {
			summary = observability.RedactErrorSummary(failureReason, s.config)
		}
		span.SetStatus(codes.Error, summary)
	}
	span.End()
}

// AgentObservabilityRail 围绕每个任务循环迭代（或单轮 invoke）建 AGENT span。
// Python: AgentObservabilityRail（rail.py:355-1210），priority=10。
type AgentObservabilityRail struct {
	rails.DeepAgentRail
	// injectedTracer 测试注入的 tracer。
	injectedTracer trace.Tracer
	// openInvokeSpan 本 agent 当前开启的 invoke span（before_invoke 打开时）。
	// 一个 rail 实例归属单个 agent，DeepAgent 一次只跑一个 invoke，无需
	// per-context 存储——这正是要点：iteration 钩子在不继承 invoke
	// ContextVar 的任务里触发，仍要找到这个 span 来嵌套。否则同时获得两个
	// 钩子的 agent 会把 invoke 与 iterations 记成兄弟，invoke 为空。
	openInvokeSpan trace.Span
	// openReactStepSpan/openReactStepParent/openReactStepIteration 内层 ReAct Step 状态。
	openReactStepSpan      trace.Span
	openReactStepParent    trace.Span
	openReactStepIteration int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentObservabilityRail 构造 rail。Python: __init__（rail.py:361-372）。
func NewAgentObservabilityRail(injectedTracer trace.Tracer) *AgentObservabilityRail {
	return &AgentObservabilityRail{injectedTracer: injectedTracer}
}

// MaybeAgentObservabilityRail 观测已初始化则返回 rail，否则 nil——所有挂载点
// （manifest providers、子 agent dispatch 钩子、平台适配器）共享的
// "初始化即建 rail"守卫单源，保证它们都是安全的无条件追加。
// Python: maybe_agent_observability_rail（rail.py:1237-1250）。
func MaybeAgentObservabilityRail() *AgentObservabilityRail {
	if !extdemand.IsInitialized() {
		return nil
	}
	return NewAgentObservabilityRail(nil)
}

// ForkForAgent 返回一个具体 Agent 实例的空生命周期持有者。
// 子 agent spec 是可复用模板：复用 rail 对象会连 open invoke/Step 状态一起
// 复用，并发子 agent 会互相收口或互为父。只保留注入 tracer；全部生命周期
// 状态从零开始。Python: fork_for_agent（rail.py:374-382）。
func (r *AgentObservabilityRail) ForkForAgent() *AgentObservabilityRail {
	return NewAgentObservabilityRail(r.injectedTracer)
}

// CallbackPriorityFor priority=10（低优先级：可观测性最后执行）。
// Python: priority = 10。
func (r *AgentObservabilityRail) CallbackPriorityFor(event agentinterfaces.AgentCallbackEvent) int {
	return 10
}

// GetCallbacks 提取已覆盖钩子映射。Python 由 class 方法表隐式给出。
// 对齐 agent_mode.go 的包裹模式（PerAgentCallbackFunc → 具体 cbc 断言）。
func (r *AgentObservabilityRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc{
		agentinterfaces.CallbackBeforeTaskIteration: func(ctx context.Context, railCtx any) error {
			return r.BeforeTaskIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		},
		agentinterfaces.CallbackAfterTaskIteration: func(ctx context.Context, railCtx any) error {
			return r.AfterTaskIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		},
		agentinterfaces.CallbackBeforeInvoke: func(ctx context.Context, railCtx any) error {
			return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		},
		agentinterfaces.CallbackAfterInvoke: func(ctx context.Context, railCtx any) error {
			return r.AfterInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		},
		agentinterfaces.CallbackBeforeModelCall: func(ctx context.Context, railCtx any) error {
			return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		},
		agentinterfaces.CallbackAfterReactIteration: func(ctx context.Context, railCtx any) error {
			return r.AfterReactIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		},
		agentinterfaces.CallbackBeforeToolCall: func(ctx context.Context, railCtx any) error {
			return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		},
		agentinterfaces.CallbackAfterToolCall: func(ctx context.Context, railCtx any) error {
			return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		},
		agentinterfaces.CallbackOnToolException: func(ctx context.Context, railCtx any) error {
			return r.OnToolException(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		},
	}
}

// SerializeAbilityValue 把工具参数或结果渲染为记在 span 上的文本。
// 值本身是文本则原样；可 JSON 序列化则 JSON 形态；否则 str 形态。
// Python: serialize_ability_value（rail.py:125-149）。
func SerializeAbilityValue(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(b)
}

// ──────────────────────────── 钩子：任务迭代 ────────────────────────────

// BeforeTaskIteration 打开本轮迭代的 agent span。
// Python: before_task_iteration（rail.py:513-607）。
func (r *AgentObservabilityRail) BeforeTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	defer recoverRail("before_task_iteration")
	inputs, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs)
	if !ok {
		return nil
	}
	iteration := inputs.Iteration
	isFollowUp := inputs.IsFollowUp
	agent := cbc.Agent()
	agentName := r.ResolveAgentName(agent)

	rootSpan := r.rootSpanFor(cbc)
	if rootSpan == nil {
		// 无运行根——没有可挂的父，孤儿根 span 会自成迹线。根由宿主开
		// （Team：team.{name}；单 agent：run span）。
		logger.Debug(logComponent).Str("agent", agentName).
			Msg("[AgentObservability] no run root span; skipping agent span")
		return nil
	}
	if !rootSpan.IsRecording() {
		logger.Warn(logComponent).Str("agent", agentName).Int("iteration", iteration).
			Msg("[AgentObservability] run root span already ended")
		return nil
	}
	if cur := CurrentAgentSpanScope(cbc); cur != nil {
		logger.Warn(logComponent).
			Msg("[AgentObservability] duplicate agent span call, skipped")
		return nil
	}

	// 迭代永远是其任务的最外层 agent scope——拥有子 llm/tool span 的
	// cascade-close。开之前先清理 context 里的残留 agent span：
	//   - 同 agent 仍在 recording → 上一迭代未收口的孤儿；排空子 span 并收口；
	//   - 其他 agent → 别的任务继承来的陈旧快照；不动它，只清继承的
	//     llm/tool 栈让本轮干净开始（我们的 cascade 碰不到别的 agent 的 span）。
	state := spanStateOf(ctx)
	r.drainOrClearStale(agentName, rootSpan, state)

	label := agentName
	if label == "" {
		label = "unknown"
	}
	// 一个 invoke、N 迭代：本 agent 同时拿到 invoke span 时，迭代挂它下面
	// 而非并列。
	iterationParent := rootSpan
	if r.openInvokeSpan != nil && r.openInvokeSpan.IsRecording() {
		iterationParent = r.openInvokeSpan
	}
	spanCtx := trace.ContextWithSpanContext(context.Background(), iterationParent.SpanContext())
	_, span := r.tracer().Start(
		spanCtx, fmt.Sprintf("agent.%s.task_iteration.%d", label, iteration),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(r.agentAttributes(agent, agentName, rootSpan, iterationParent)...),
	)

	config := extdemand.GetConfig()
	decoration := CollectDecoration(cbc)
	span.SetAttributes(
		attribute.Int(observability.DATaskIteration, iteration),
		attribute.Bool(observability.DATaskIsFollowUp, isFollowUp),
	)
	applyDecoration(span, decoration, invokeQueryText(inputs.Query), config)

	if state != nil {
		state.PushAgentSpan(agentSpanCurrentKey, span)
	}

	logger.Debug(logComponent).
		Str("name", fmt.Sprintf("agent.%s.task_iteration.%d", label, iteration)).
		Str("span_id", span.SpanContext().SpanID().String()).
		Str("trace_id", span.SpanContext().TraceID().String()).
		Msg("[AgentObservability] agent span opened")

	// 迭代绝不嵌在别的 agent span 下——它是自己任务的 agent 层；
	// parent_agent_span 清理后为 None，仅为恢复对称性记录。
	(&AgentSpanScope{
		Span:                span,
		Kind:                agentSpanScopeKindIteration,
		ParentAgentSpan:     nil,
		IsOutermost:         true,
		config:              config,
		outputAttributeKeys: decoration.OutputAttributeKeys,
	}).Attach(cbc)
	return nil
}

// AfterTaskIteration 收口本轮迭代的 agent span。
// Python: after_task_iteration（rail.py:609-646）。
func (r *AgentObservabilityRail) AfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	defer recoverRail("after_task_iteration")
	r.closeReactStep(cbc.Exception(), spanStateOf(ctx))
	scope := DetachAgentSpanScope(cbc)
	if scope == nil {
		return nil
	}
	var output any
	if inputs, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs); ok {
		output = inputs.Result
	}
	scope.Close(output, cbc.Exception(), spanStateOf(ctx))

	// 迭代收口把 current 恢复为 None，已收口的 agent span 留在环境上下文里。
	// 重挂运行根，让这轮之后的事（别的 rail、宿主自己的 span）仍正确嵌套，
	// 而不是挂到已结束的 span 上。
	rootSpan := r.rootSpanFor(cbc)
	if rootSpan != nil && rootSpan.IsRecording() {
		// Go：后续子 span 由显式 SpanContext 决定父，此处无 attach 动作
		// （偏差 spec §4）。
		_ = rootSpan
	}
	logger.Debug(logComponent).
		Bool("has_output", output != nil).
		Msg("[AgentObservability] agent span closed")
	return nil
}

// ──────────────────────────── 钩子：invoke 兜底 ────────────────────────────

// BeforeInvoke 单轮 agent 的 invoke span（覆盖单轮 agent 与子 agent）。
// 子 agent（plan/code/explore/...）默认 enable_task_loop=False，经
// _run_single_round_invoke 运行，永不触发 TASK_ITERATION 事件——没有这两个
// 钩子其 LLM/tool span 会回退运行根（或被跳过），迹线缺 agent 层。
// 只为单轮 agent 开 invoke span（enable_task_loop=False）；多轮路径从
// before_task_iteration 拿迭代 span，此处跳过，两者绝不双开。
// Python: before_invoke（rail.py:655-775）。
func (r *AgentObservabilityRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	defer recoverRail("before_invoke")
	if CurrentAgentSpanScope(cbc) != nil {
		return nil
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	if !ok {
		return nil
	}
	agent := cbc.Agent()
	enableTaskLoop := deepEnableTaskLoop(agent)
	if enableTaskLoop {
		return nil
	}
	agentName := r.ResolveAgentName(agent)
	if agentName == "" {
		agentName = "unknown"
	}

	rootSpan := r.rootSpanFor(cbc)
	if rootSpan == nil || !rootSpan.IsRecording() {
		// 无运行根（子 agent 在任何运行之外被调用）——没有可挂的父，
		// 跳过而不造孤儿根。
		return nil
	}

	// 嵌套按结构决定：当前 agent span 只要仍 recording 就是合法父——
	// 不看属于谁。子 agent 作为同步 await 在父迭代内（task_tool）运行，
	// 嵌在仍 recording 的父下得到正确的
	// root → iteration → subagent.invoke → llm/tool 树。父已结束则回退运行根。
	state := spanStateOf(ctx)
	prev := currentAgentSpan(state)
	var parentSpan trace.Span
	var isOutermost bool
	if prev != nil && prev.IsRecording() {
		parentSpan = prev
		// 嵌在活 agent span 下——父拥有 cascade-close；本 scope 不碰
		// 继承的 llm/tool 栈（那是父仍开启的子 span）。
		isOutermost = false
	} else {
		parentSpan = rootSpan
		// 无活的 agent 父——本 scope 是其任务的最外层 agent 层，拥有 cascade。
		isOutermost = true
		if prev != nil {
			// context 里残留已结束的 agent span——清掉让运行根成为环境父。
			if state != nil {
				state.PopAgentSpan(agentSpanCurrentKey)
			}
		}
	}

	// 子 agent 是从工具调用里派发的，并在其中跑到完成——派发它的工具 span
	// （而非工具自己挂的 agent span）才是它的 span 父。只有直接开在上述父
	// 下的工具 span 符合条件，保持只对真实派发生效。
	otelParent := parentSpan
	if dispatchToolSpan := currentToolSpan(state); dispatchToolSpan != nil {
		if ps, ok := dispatchToolSpan.(sdktrace.ReadOnlySpan); ok {
			if ps.Parent().IsValid() && ps.Parent().SpanID() == parentSpan.SpanContext().SpanID() {
				otelParent = dispatchToolSpan
			}
		}
	}

	spanCtx := trace.ContextWithSpanContext(context.Background(), otelParent.SpanContext())
	_, span := r.tracer().Start(
		spanCtx, fmt.Sprintf("agent.%s.invoke", agentName),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(r.agentAttributes(agent, agentName, rootSpan, parentSpan)...),
	)

	config := extdemand.GetConfig()
	decoration := CollectDecoration(cbc)
	applyDecoration(span, decoration, invokeQueryText(inputs.Query), config)

	if state != nil {
		state.PushAgentSpan(agentSpanCurrentKey, span)
	}

	// parent_agent_span 是我们嵌进去的活 agent span（close 恢复），或回退
	// 运行根时的 nil。
	var parentAgentSpan trace.Span
	if parentSpan == prev {
		parentAgentSpan = prev
	}
	(&AgentSpanScope{
		Span:                span,
		Kind:                agentSpanScopeKindInvoke,
		ParentAgentSpan:     parentAgentSpan,
		IsOutermost:         isOutermost,
		config:              config,
		outputAttributeKeys: decoration.OutputAttributeKeys,
	}).Attach(cbc)
	// 供 before_task_iteration 发布：同时拿到两个钩子的 agent 把迭代嵌在本
	// span 下。
	r.openInvokeSpan = span

	logger.Debug(logComponent).
		Str("name", fmt.Sprintf("agent.%s.invoke", agentName)).
		Bool("nested", parentAgentSpan != nil).
		Msg("[AgentObservability] invoke span opened (single-round)")
	return nil
}

// AfterInvoke 收口单轮 invoke span。
// Python: after_invoke（rail.py:777-809）。
func (r *AgentObservabilityRail) AfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	defer recoverRail("after_invoke")
	r.closeReactStep(cbc.Exception(), spanStateOf(ctx))
	scope := DetachAgentSpanScope(cbc)
	if scope == nil || scope.Kind != agentSpanScopeKindInvoke {
		// before_invoke 跳过（多轮路径）——无可收口。
		return nil
	}
	if scope.Span == r.openInvokeSpan {
		r.openInvokeSpan = nil
	}
	var output any
	if inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs); ok {
		output = inputs.Result
	}
	scope.Close(output, cbc.Exception(), spanStateOf(ctx))
	logger.Debug(logComponent).Msg("[AgentObservability] invoke span closed")
	return nil
}

// ──────────────────────────── 钩子：内层 ReAct Step ────────────────────────────

// BeforeModelCall 打开拥有本次物理模型请求及其工具的 Step。
// Python: before_model_call（rail.py:811-894）。
func (r *AgentObservabilityRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	defer recoverRail("before_model_call")
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok {
		return nil
	}
	iteration := inputs.ReactIteration
	if iteration <= 0 {
		return nil
	}
	state := spanStateOf(ctx)
	if r.openReactStepSpan != nil && r.openReactStepSpan.IsRecording() && r.openReactStepIteration == iteration {
		if state != nil {
			state.PushAgentSpan(agentSpanCurrentKey, r.openReactStepSpan)
		}
		return nil
	}

	r.closeReactStep(nil, state)
	scopeParent := currentAgentSpan(state)
	rootSpan := r.rootSpanFor(cbc)
	if scopeParent == nil || !scopeParent.IsRecording() {
		scopeParent = rootSpan
	}
	if scopeParent == nil || !scopeParent.IsRecording() {
		return nil
	}

	// 单 agent ReAct Step 是 Turn 根的语义子，不是外层任务循环记账 span 的。
	// 嵌套/子 agent scope 保持结构父。
	otelParent := scopeParent
	scopeParentIsTurnChild := false
	if ro, ok := scopeParent.(sdktrace.ReadOnlySpan); ok && rootSpan != nil && rootSpan.IsRecording() {
		scopeParentIsTurnChild = readOnlyAttrBool(ro, observability.OJTraceRoot) &&
			ro.Parent().IsValid() && ro.Parent().SpanID() == rootSpan.SpanContext().SpanID()
	}
	if scopeParentIsTurnChild {
		otelParent = rootSpan
	}

	agent := cbc.Agent()
	agentName := scopeParentAgentName(scopeParent)
	if agentName == "" {
		agentName = r.ResolveAgentName(agent)
	}
	if agentName == "" {
		agentName = "unknown"
	}
	spanCtx := trace.ContextWithSpanContext(context.Background(), otelParent.SpanContext())
	_, span := r.tracer().Start(
		spanCtx, fmt.Sprintf("agent.%s.react_iteration.%d", agentName, iteration),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(r.agentAttributes(agent, agentName, rootSpan, scopeParent)...),
	)
	span.SetAttributes(
		attribute.String(observability.OJTrajectoryRecordKind, "step"),
		attribute.String(observability.OJTrajectorySchemaVersion, observability.TrajectorySpanSchemaVersion),
	)
	stepID := span.SpanContext().SpanID().String()
	span.SetAttributes(
		attribute.String(observability.OJStepID, stepID),
		attribute.Int(observability.OJStepNumber, iteration),
	)
	publishOpenStep(cbc, stepID, iteration)

	r.openReactStepSpan = span
	r.openReactStepParent = scopeParent
	r.openReactStepIteration = iteration
	if state != nil {
		state.PushAgentSpan(agentSpanCurrentKey, span)
	}
	return nil
}

// AfterReactIteration 模型与工具完成后收口成功的 ReAct Step。
// Python: after_react_iteration（rail.py:896-903）。
func (r *AgentObservabilityRail) AfterReactIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	defer recoverRail("after_react_iteration")
	r.closeReactStep(cbc.Exception(), spanStateOf(ctx))
	return nil
}

// closeReactStep Step 收口本体。
// Python: _close_react_step（rail.py:905-929）。
func (r *AgentObservabilityRail) closeReactStep(exception error, state *observability.OtelSpanState) {
	span := r.openReactStepSpan
	parent := r.openReactStepParent
	r.openReactStepSpan = nil
	r.openReactStepParent = nil
	r.openReactStepIteration = 0
	if span == nil {
		return
	}
	if span.IsRecording() {
		observability.CascadeCloseChildren(observability.WithSpanState(context.Background(), state))
		if exception != nil {
			RecordSpanError(span, exception, extdemand.GetConfig())
		} else {
			span.SetStatus(codes.Ok, "")
			span.End()
		}
	}
	if state != nil {
		if parent != nil && parent.IsRecording() {
			state.PushAgentSpan(agentSpanCurrentKey, parent)
		} else {
			state.PopAgentSpan(agentSpanCurrentKey)
		}
	}
}

// ──────────────────────────── 钩子：工具生命周期 ────────────────────────────

// BeforeToolCall 打开一次 AbilityManager 执行的权威 span。
// 本 rail 挂载的每个 agent（含 Team 成员）都会获得。只有此钩子看得到模型的
// 工具调用，只有这个 span 能陈述 call id；团队成员原来的全局工具回调 span
// 不陈述任何东西，调用无法与模型读到的工具消息关联。
// Python: before_tool_call（rail.py:931-998）。
func (r *AgentObservabilityRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	defer recoverRail("before_tool_call")
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	toolName := inputs.ToolName
	if toolName == "" {
		toolName = "unknown"
	}
	state := spanStateOf(ctx)
	currentAgent := currentAgentSpan(state)
	rootSpan := r.rootSpanFor(cbc)
	parent := rootSpan
	if currentAgent != nil && currentAgent.IsRecording() {
		parent = currentAgent
	}
	if parent == nil || !parent.IsRecording() {
		return nil
	}

	spanCtx := trace.ContextWithSpanContext(context.Background(), parent.SpanContext())
	_, span := r.tracer().Start(
		spanCtx, fmt.Sprintf("execute_tool %s", toolName),
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	span.SetAttributes(
		attribute.String(observability.GenAIOperationName, "execute_tool"),
		attribute.String(observability.GenAIToolName, toolName),
		attribute.String(observability.OJTrajectorySchemaVersion, observability.TrajectorySpanSchemaVersion),
		attribute.String(observability.OJTrajectoryRecordKind, "tool"),
		attribute.Bool(observability.OJToolAuthoritative, true),
	)
	if inputs.ToolCall != nil && inputs.ToolCall.ID != "" {
		span.SetAttributes(attribute.String(observability.GenAIToolCallID, inputs.ToolCall.ID))
	}

	agent := cbc.Agent()
	card := resolveAbilityCard(agent, toolName)
	if card != nil {
		if card.ID != "" {
			span.SetAttributes(attribute.String(observability.OJToolResourceID, card.ID))
		}
		if card.Description != "" {
			span.SetAttributes(attribute.String(observability.GenAIToolDescription, card.Description))
		}
	}
	// AbilityManager 在 agent 控制的 runtime 内执行工具，属 OTel GenAI
	// "extension"。MCP 等传输细节是独立关联元数据。
	span.SetAttributes(
		attribute.String(observability.GenAIToolType, "extension"),
	)
	if protocol := abilityProtocol(card); protocol != "" {
		span.SetAttributes(attribute.String(observability.OJToolProtocol, protocol))
	}

	rawArguments := SerializeAbilityValue(inputs.ToolArgs)
	config := extdemand.GetConfig()
	redactedArguments := rawArguments
	if config != nil {
		redactedArguments = observability.RedactPrompt(rawArguments, config)
	}
	span.SetAttributes(
		attribute.String(observability.GenAIToolCallArguments, redactedArguments),
		attribute.String(observability.OJSpanInput, redactedArguments),
	)
	copyParentCorrelation(parent, span)
	// resume 在循环重开 Step span 前重放被中断 Step 的工具，此处的父是运行根、
	// 没有 Step 身份可继承——改从会话恢复，让重放工作陈述自己属于哪个 Step。
	if !parentHasAttr(parent, observability.OJStepID) {
		persisted := resolvePersistedStep(cbc)
		reactIteration := inputsReactIteration(cbc)
		if persisted != nil && persisted.stepNumber == reactIteration {
			span.SetAttributes(
				attribute.String(observability.OJStepID, persisted.stepID),
				attribute.Int(observability.OJStepNumber, persisted.stepNumber),
			)
		} else if reactIteration > 0 {
			span.SetAttributes(attribute.Int(observability.OJStepNumber, reactIteration))
		}
	}

	PushToolSpanCurrent(state, toolName, span)
	AttachToolSpanScope(cbc, &ToolSpanScope{Span: span, ToolName: toolName, config: config, state: state})
	return nil
}

// AfterToolCall 工具执行后收口。
// Python: after_tool_call（rail.py:1000-1010）。
func (r *AgentObservabilityRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	defer recoverRail("after_tool_call")
	scope := DetachToolSpanScope(cbc)
	if scope == nil {
		return nil
	}
	var output any
	if inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok {
		output = inputs.ToolResult
	}
	scope.Close(output, cbc.Exception())
	return nil
}

// OnToolException 工具异常收口。
// Python: on_tool_exception（rail.py:1012-1020）。
func (r *AgentObservabilityRail) OnToolException(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	defer recoverRail("on_tool_exception")
	scope := DetachToolSpanScope(cbc)
	if scope == nil {
		return nil
	}
	scope.Close(nil, cbc.Exception())
	return nil
}

// MaybeMountedRailFor 供挂载点取 rail（builtin_elements / 子 agent dispatch）。
// Python 侧直接调 maybe_agent_observability_rail；Go 由挂载点调用本方法别名。
func MaybeMountedRailFor() *AgentObservabilityRail { return MaybeAgentObservabilityRail() }
