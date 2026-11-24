package observability

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// root_span.go 实现活动 span 跟踪器与根 span 注册表。
// Python: extensions/observability/span_context.py（ActiveSpanTracker :90-432、
// root registry :716-880、cascade/flush :913-1010/:1129-1179、session 绑定 :538-553）。
// 本仓既有拆分：span_context.go（窗口段）、span_state.go（LLM/Tool/Agent 栈段）——
// 本文件承接其余段，延续既有按段拆文件的偏差（登记 spec §4）。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// OJTraceComplete 迹线完成标记。
	// Python: semconv.py OJ_TRACE_COMPLETE = "openjiuwen.trace.complete"。
	OJTraceComplete = "openjiuwen.trace.complete"
	// OJTraceForcedClose 迹线含被安全网强制收口的子 span。
	// Python: semconv.py OJ_TRACE_FORCED_CLOSE = "openjiuwen.trace.forced_close"。
	OJTraceForcedClose = "openjiuwen.trace.forced_close"
	// OJSpanForcedClose 单 span 被安全网强制收口标记。
	// Python: semconv.py OJ_SPAN_FORCED_CLOSE。
	OJSpanForcedClose = "openjiuwen.span.forced_close"
	// OJSpanForcedCloseReason 强制收口的机器可读原因。
	// Python: semconv.py OJ_SPAN_FORCED_CLOSE_REASON。
	OJSpanForcedCloseReason = "openjiuwen.span.forced_close_reason"
)

// llmOperationNames 判定 llm.call span 的操作名闭集。
// Python: _is_llm_call —— GEN_AI_OPERATION_NAME ∈ {chat, generate_content, text_completion}。
var llmOperationNames = map[string]bool{
	"chat":             true,
	"generate_content": true,
	"text_completion":  true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// ActiveSpanTracker 跟踪全部活动 span 的 SpanProcessor，用于可靠清理。
// Python: ActiveSpanTracker(span_context.py:90)——强引用（普通 set）确保 span
// 在协程取消后仍存活；正常 End 的 span 在 OnEnd 移除防累积，只有从未被显式
// 收口的 span 留在集合里，由 flush 关闭。
type ActiveSpanTracker struct {
	mu sync.Mutex
	// spansByTrace 以 trace id（hex 字符串）为键的活动 span 集合。
	// Python: _spans_by_trace: dict[int, set[Span]]（trace_id 整数；Go 用 hex 串等价）。
	spansByTrace map[string]map[trace.Span]struct{}
	// llmSpansByCallID 以 LLM 请求 id 索引的开启中 llm.call span。
	// Python: _llm_spans_by_call_id（call_scope 关联；Go 侧 call scope 未移植，
	// 索引暂恒空，解析走 parent 回退——偏差登记 spec §4）。
	llmSpansByCallID map[string]trace.Span
	onStartCount     int
	onEndCount       int
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// activeSpanTracker 进程级活动 span 跟踪器单例。
	// Python: _active_span_tracker（get/set_active_span_tracker）。
	activeSpanTracker     *ActiveSpanTracker
	rootRegistryLock      sync.RWMutex
	// rootRegistry 以 session id 为键的运行根 span 注册表。
	// Python: _root_registry。
	rootRegistry          = map[string]trace.Span{}
	// ambientRootSpan 无上下文传播能力的宿主的进程级兜底根。
	// Python: _ambient_root_span。
	ambientRootSpan       trace.Span
	// rootSpanFallback 根 span 解析兜底钩子。
	// Python: install_root_span_fallback 猴子补丁 get_root_span（span_context.py:139-176）；
	// Go 无猴子补丁，改为钩子注入（harness/observability 注册 session 注册表回退），
	// 偏差登记 spec §4。
	rootSpanFallback      func(sessionID string) trace.Span
	currentSessionIDValue atomicSessionID
)

// atomicSessionID 当前通用会话 id（ContextVar 等价；进程级单槽 + context 传播双轨）。
type atomicSessionID struct {
	mu sync.RWMutex
	v  string
}

func (a *atomicSessionID) get() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.v
}

func (a *atomicSessionID) set(v string) {
	a.mu.Lock()
	a.v = v
	a.mu.Unlock()
}

// sessionIDKeyType context 传播的 session id 键。
type sessionIDKeyType struct{}

// rootSpanKeyType context 传播的根 span 绑定键。
type rootSpanKeyType struct{}

// rootSpanBinding context 中携带的根 span 绑定（span + 其所属 session）。
// Python: _root_span_ctx/_root_session_ctx 两个 ContextVar。
type rootSpanBinding struct {
	span      trace.Span
	sessionID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetActiveSpanTracker 返回进程级活动 span 跟踪器（未初始化为 nil）。
// Python: get_active_span_tracker(span_context.py:432)。
func GetActiveSpanTracker() *ActiveSpanTracker {
	return activeSpanTracker
}

// SetActiveSpanTracker 设置/清空进程级跟踪器（provider 构造时挂载）。
// Python: set_active_span_tracker(span_context.py:436)。
func SetActiveSpanTracker(t *ActiveSpanTracker) {
	activeSpanTracker = t
}

// NewActiveSpanTracker 构造跟踪器。
// Python: ActiveSpanTracker()（span_context.py:96）。
func NewActiveSpanTracker() *ActiveSpanTracker {
	return &ActiveSpanTracker{
		spansByTrace:     map[string]map[trace.Span]struct{}{},
		llmSpansByCallID: map[string]trace.Span{},
	}
}

// SetCurrentSessionID 绑定当前通用观测 session（回调查询用）。
// Python: set_current_session_id(span_context.py:542)。
func SetCurrentSessionID(sessionID string) {
	currentSessionIDValue.set(NormalizeSessionID(sessionID))
}

// GetCurrentSessionID 读取当前通用观测 session。
// Python: get_current_session_id(span_context.py:547)。
func GetCurrentSessionID() string {
	return currentSessionIDValue.get()
}

// ClearCurrentSessionID 清空当前通用观测 session。
// Python: clear_current_session_id(span_context.py:551)。
func ClearCurrentSessionID() {
	currentSessionIDValue.set("")
}

// WithSessionID 返回携带 session id 的 context（Go 侧传播通道；
// Python ContextVar 的 context 等价物）。
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKeyType{}, NormalizeSessionID(sessionID))
}

// SessionIDFromCtx 从 context 读取 session id（无则空串）。
func SessionIDFromCtx(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(sessionIDKeyType{}).(string); ok {
		return v
	}
	return ""
}

// WithRootSpanBinding 在 context 上临时绑定根 span（不进共享注册表）。
// Python: bind_root_span_context(span_context.py:727)——子执行的回调 session
// 与拥有根的 session 不同时使用。
func WithRootSpanBinding(ctx context.Context, span trace.Span, sessionID string) context.Context {
	return context.WithValue(ctx, rootSpanKeyType{}, &rootSpanBinding{
		span:      span,
		sessionID: NormalizeSessionID(sessionID),
	})
}

// SetRootSpan 绑定活跃根 span 并可选注册到 session。
// Python: set_root_span(span_context.py:716)。
func SetRootSpan(span trace.Span, sessionID string) {
	sid := NormalizeSessionID(sessionID)
	if sid != "" {
		rootRegistryLock.Lock()
		rootRegistry[sid] = span
		rootRegistryLock.Unlock()
	}
}

// GetRootSpan 解析活跃根：session 注册表 → 兜底钩子（harness 注册的
// session 注册表回退）→ ambient。仅按 session id 查注册表——没有 session
// 的调用方绝不收养别的 session 的根（"唯一的活根"只是碰巧在跑的那个）。
// Python: get_root_span(span_context.py:748)；context 绑定段由
// WithRootSpanBinding/GetBoundRootSpan 显式传播承载（偏差登记 spec §4）。
func GetRootSpan(sessionID string) trace.Span {
	requestedSID := NormalizeSessionID(sessionID)
	if requestedSID == "" {
		requestedSID = GetCurrentSessionID()
	}
	if requestedSID != "" {
		if span := registeredRootSpan(requestedSID); span != nil {
			return span
		}
	}
	if rootSpanFallback != nil {
		if span := rootSpanFallback(requestedSID); span != nil && span.IsRecording() {
			return span
		}
	}
	if ambientRootSpan != nil && ambientRootSpan.IsRecording() {
		return ambientRootSpan
	}
	return nil
}

// GetSessionRootSpan 返回恰为 sessionID 所拥有的活跃根（不收养他人）。
// Python: get_session_root_span(span_context.py:775)——context 绑定仅在
// session 为空（本执行自己的根）或恰为 sid 时生效。
func GetSessionRootSpan(sessionID string) (trace.Span, error) {
	sid := NormalizeSessionID(sessionID)
	if sid == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	return registeredRootSpan(sid), nil
}

// GetBoundRootSpan 仅返回绑定在当前 context 的根（不查注册表/ambient）。
// Python: get_bound_root_span(span_context.py:816)。
func GetBoundRootSpan(ctx context.Context) trace.Span {
	if ctx == nil {
		return nil
	}
	b, ok := ctx.Value(rootSpanKeyType{}).(*rootSpanBinding)
	if !ok || b == nil || b.span == nil || !b.span.IsRecording() {
		return nil
	}
	return b.span
}

// ClearRootSpan 清除根绑定，不误删并发替换者。
// Python: clear_root_span(span_context.py:829)。expectedSpan 非 nil 时仅当
// 当前绑定/注册项即该 span 才清。
func ClearRootSpan(sessionID string, expectedSpan trace.Span) {
	sid := NormalizeSessionID(sessionID)
	rootRegistryLock.Lock()
	defer rootRegistryLock.Unlock()
	if sid != "" {
		current, ok := rootRegistry[sid]
		if ok && (expectedSpan == nil || current == expectedSpan) {
			delete(rootRegistry, sid)
		}
		return
	}
	if expectedSpan != nil {
		for key, current := range rootRegistry {
			if current == expectedSpan {
				delete(rootRegistry, key)
			}
		}
	}
}

// SetAmbientRootSpan 注册进程级兜底根（无 context 传播的宿主用）。
// Python: set_ambient_root_span(span_context.py:861)。
func SetAmbientRootSpan(span trace.Span) {
	ambientRootSpan = span
}

// ClearAmbientRootSpan 清除进程级兜底根。
// Python: clear_ambient_root_span(span_context.py:867)。
func ClearAmbientRootSpan() {
	ambientRootSpan = nil
}

// SetRootSpanFallback 注入根解析兜底钩子（harness/observability 的
// session 注册表回退）。Python: install_root_span_fallback 猴子补丁的等价缝。
func SetRootSpanFallback(fallback func(sessionID string) trace.Span) {
	rootSpanFallback = fallback
}

// MarkSpanForcedClose 标记安全网子 span 收口并同步在根上浮现。
// Python: mark_span_forced_close(span_context.py:72)。
func MarkSpanForcedClose(span trace.Span, reason string) {
	span.SetAttributes(
		attribute.Bool(OJSpanForcedClose, true),
		attribute.String(OJSpanForcedCloseReason, reason),
	)
	sc := span.SpanContext()
	root := GetRootSpan("")
	if root != nil && root.IsRecording() && root.SpanContext().TraceID() == sc.TraceID() {
		root.SetAttributes(attribute.Bool(OJTraceForcedClose, true))
	}
}

// CascadeCloseChildren 收口当前 context 上所有未关的子 llm/tool span。
// Python: cascade_close_children(span_context.py:913)——AgentSpanScope.close 与
// close_current_agent_span 的单一事实源；到达此路径的 span 正常收口回调未触发，
// 保留 UNSET 状态、打 forced-close 标记、并立即在记录中的迹线根上浮现。
// 偏差：Python 按 _current_agent_span 判定归属；Go OtelSpanState 无单一 agent
// 槽，agentSpanMap 全部 agent span 均参与父比对（登记 spec §4）。
// 返回本次安全网收口的子 span 数。
func CascadeCloseChildren(ctx context.Context) int {
	closed := 0
	state := SpanStateFromCtx(ctx)
	if state == nil {
		return 0
	}
	// 收口 tool span：仍在 recording 的即孤儿。
	for toolName, stack := range state.SnapshotTools() {
		var keep []trace.Span
		for _, ts := range stack {
			if ts == nil || !ts.IsRecording() {
				continue
			}
			logger.Warn(logComponent).
				Str("name", toolName).
				Str("span_id", spanIDOf(ts)).
				Msg("ORPHAN tool span in cascade-close — on_tool_call_finished/on_tool_call_error did not fire")
			MarkSpanForcedClose(ts, "missing_tool_terminal_callback")
			ts.End()
			closed++
		}
		state.SetTools(toolName, keep)
	}
	// 收口属于各 agent span 的 llm.call span。
	// Python: tracker.close_llm_spans_by_parent 内部经 _resolve_root_span() 取
	// trace id（ContextVar）；Go 无 ContextVar，trace id 由 agent span 显式携带。
	if t := GetActiveSpanTracker(); t != nil {
		for _, agentSpanID := range state.AgentSpanIDs() {
			closed += t.CloseLLMSpansByParent(agentSpanID, state.AgentTraceID())
		}
	}
	return closed
}

// CloseCurrentAgentSpan 收口当前 agent span（先排空子 span）。
// Python: close_current_agent_span(span_context.py:964)。
func CloseCurrentAgentSpan(ctx context.Context) {
	CascadeCloseChildren(ctx)
	if state := SpanStateFromCtx(ctx); state != nil {
		state.CloseAgentSpans()
	}
}

// FlushChildSpans 清算指定迹线的未关子 span。
// Python: flush_child_spans(span_context.py:1129)。traceID 为空时从当前根解析；
// 根也不可解析则跳过（绝不吞别的运行中的迹线）。
func FlushChildSpans(traceID string) int {
	t := GetActiveSpanTracker()
	if t == nil {
		return 0
	}
	effective := traceID
	if effective == "" {
		if root := GetRootSpan(""); root != nil {
			effective = root.SpanContext().TraceID().String()
		}
	}
	if effective == "" {
		logger.Warn(logComponent).Msg("flush_child_spans: cannot determine trace_id — skipping flush")
		return 0
	}
	rootID := ""
	if root := GetRootSpan(""); root != nil {
		rootID = root.SpanContext().SpanID().String()
	}
	closed := t.FlushSpansForTrace(effective, rootID)
	if closed > 0 {
		logger.Info(logComponent).Str("trace_id", effective).Int("closed", closed).
			Msg("flush_child_spans closed spans")
	}
	return closed
}

// ResetSpanContextState 重置全部 per-task span 跟踪状态（测试隔离用）。
// Python: reset_state(span_context.py:1108)——本仓窗口/序列段已在
// ResetTrajectoryContextState（span_context.go:156），此处补根/会话/ambient 段。
func ResetSpanContextState() {
	ClearRootSpan("", nil)
	ClearCurrentSessionID()
	ClearAmbientRootSpan()
	rootRegistryLock.Lock()
	rootRegistry = map[string]trace.Span{}
	rootRegistryLock.Unlock()
	SetRootSpanFallback(nil)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rootSpanFromCtxBinding 已移除——Go 侧根绑定走 context 显式传播
// （WithRootSpanBinding/GetBoundRootSpan），无 ctx 的 GetRootSpan 入口
// 按注册表 → 兜底钩子 → ambient 三段解析（偏差登记 spec §4）。

// registeredRootSpan 返回 session 注册的活跃根，顺手清掉陈旧项。
// Python: _registered_root_span(span_context.py:805)。
func registeredRootSpan(sessionID string) trace.Span {
	rootRegistryLock.RLock()
	registered, ok := rootRegistry[sessionID]
	rootRegistryLock.RUnlock()
	if !ok {
		return nil
	}
	if registered.IsRecording() {
		return registered
	}
	rootRegistryLock.Lock()
	if current, ok := rootRegistry[sessionID]; ok && current == registered {
		delete(rootRegistry, sessionID)
	}
	rootRegistryLock.Unlock()
	return nil
}

// ──────────────────────────── ActiveSpanTracker 方法 ────────────────────────────

// OnStart 记录开启的 span。
// Python: ActiveSpanTracker.on_start(span_context.py:122)。
func (t *ActiveSpanTracker) OnStart(_ context.Context, s sdktrace.ReadWriteSpan) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).Any("panic", rec).Msg("ActiveSpanTracker.on_start failed")
		}
	}()
	sc := s.SpanContext()
	if !sc.IsValid() {
		return
	}
	t.mu.Lock()
	set, ok := t.spansByTrace[sc.TraceID().String()]
	if !ok {
		set = map[trace.Span]struct{}{}
		t.spansByTrace[sc.TraceID().String()] = set
	}
	if ts, ok := s.(trace.Span); ok {
		set[ts] = struct{}{}
	}
	t.onStartCount++
	t.mu.Unlock()
}

// OnEnd 移除正常收口的 span 防累积。
// Python: ActiveSpanTracker.on_end(span_context.py:134)。
func (t *ActiveSpanTracker) OnEnd(s sdktrace.ReadOnlySpan) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).Any("panic", rec).Msg("ActiveSpanTracker.on_end failed")
		}
	}()
	sc := s.SpanContext()
	if !sc.IsValid() {
		return
	}
	t.mu.Lock()
	if set, ok := t.spansByTrace[sc.TraceID().String()]; ok {
		if ts, ok := s.(trace.Span); ok {
			delete(set, ts)
		}
	}
	t.onEndCount++
	t.mu.Unlock()
}

// Shutdown 收口全部残留 span。
// Python: shutdown → flush_all_spans(span_context.py:391)。
func (t *ActiveSpanTracker) Shutdown(_ context.Context) error {
	t.FlushAllSpans()
	return nil
}

// ForceFlush 不做清算——provider 在调用方已收口子 span 后才调它，
// 此处收口会偷走归属者的 span。Python: force_flush(span_context.py:397)。
func (t *ActiveSpanTracker) ForceFlush(_ context.Context) error { return nil }

// RegisterLLMSpan 以 LLM 请求 id 索引开启中的 llm.call span。
// Python: register_llm_span(span_context.py:159)。callID 为空直接忽略。
func (t *ActiveSpanTracker) RegisterLLMSpan(callID string, span trace.Span) {
	if callID == "" {
		return
	}
	t.mu.Lock()
	t.llmSpansByCallID[callID] = span
	t.mu.Unlock()
}

// CloseLLMSpansByParent 收口父为 parentSpanID 的 recording llm.call span。
// Python: close_llm_spans_by_parent(span_context.py:182)——到达此路径说明正常
// 收口回调未触发，打 forced-close 标记、保留 UNSET 状态（不误报成功）。
// traceID 由调用方显式传入（Python 经 _resolve_root_span 的 ContextVar 取）。
func (t *ActiveSpanTracker) CloseLLMSpansByParent(parentSpanID trace.SpanID, traceID string) int {
	if traceID == "" {
		return 0
	}
	closed := 0
	t.mu.Lock()
	spans := make([]trace.Span, 0, len(t.spansByTrace[traceID]))
	for s := range t.spansByTrace[traceID] {
		spans = append(spans, s)
	}
	t.mu.Unlock()
	for _, s := range spans {
		ro, ok := s.(sdktrace.ReadOnlySpan)
		if !ok || !isLLMReadOnlySpan(ro) || !s.IsRecording() {
			continue
		}
		if parent := ro.Parent(); !parent.IsValid() || parent.SpanID() != parentSpanID {
			continue
		}
		logger.Warn(logComponent).
			Str("span_id", ro.SpanContext().SpanID().String()).
			Str("parent_span_id", parentSpanID.String()).
			Msg("ORPHAN LLM span in cascade-close — close callback did not fire")
		MarkSpanForcedClose(s, "missing_llm_terminal_callback")
		s.End()
		closed++
	}
	return closed
}

// FlushSpansForTrace 收口指定迹线的全部活动 span。
// Python: flush_spans_for_trace(span_context.py:302)。excludeRootSpanID 为根
// span id（由调用方传入，根留给归属者收口）。
func (t *ActiveSpanTracker) FlushSpansForTrace(traceID, excludeRootSpanID string) int {
	closed := 0
	t.mu.Lock()
	spansToClose := t.spansByTrace[traceID]
	delete(t.spansByTrace, traceID)
	t.mu.Unlock()
	for s := range spansToClose {
		if s == nil || !s.IsRecording() {
			continue
		}
		if excludeRootSpanID != "" &&
			s.SpanContext().SpanID().String() == excludeRootSpanID {
			continue
		}
		if isLLMSpanByAttrs(s) {
			logger.Error(logComponent).
				Str("span_id", s.SpanContext().SpanID().String()).
				Msg("ORPHAN llm span at flush — opened normally but close callback never fired")
		} else {
			logger.Warn(logComponent).
				Str("span_id", spanIDOf(s)).
				Msg("ORPHAN non-LLM span at flush — span was never properly closed")
		}
		MarkSpanForcedClose(s, "trace_safety_flush")
		s.End()
		closed++
	}
	if closed > 0 {
		logger.Info(logComponent).Str("trace_id", traceID).Int("closed", closed).
			Msg("ActiveSpanTracker.FlushSpansForTrace closed spans")
	}
	return closed
}

// FlushAllSpans 收口全部残留活动 span（finalize/shutdown）。
// Python: flush_all_spans(span_context.py:352)。
func (t *ActiveSpanTracker) FlushAllSpans() int {
	closed := 0
	t.mu.Lock()
	allTraces := t.spansByTrace
	t.spansByTrace = map[string]map[trace.Span]struct{}{}
	t.mu.Unlock()
	for _, spanSet := range allTraces {
		for s := range spanSet {
			if s == nil || !s.IsRecording() {
				continue
			}
			if isLLMSpanByAttrs(s) {
				logger.Error(logComponent).
					Str("span_id", s.SpanContext().SpanID().String()).
					Msg("ORPHAN llm span at flush — close callback never fired")
			} else {
				logger.Warn(logComponent).
					Str("span_id", spanIDOf(s)).
					Msg("ORPHAN non-LLM span at flush — span was never properly closed")
			}
			MarkSpanForcedClose(s, "provider_shutdown_flush")
			s.End()
			closed++
		}
	}
	if closed > 0 {
		logger.Info(logComponent).Int("closed", closed).Msg("ActiveSpanTracker.FlushAllSpans closed spans")
	}
	return closed
}

// ──────────────────────────── 非导出函数（续） ────────────────────────────

// isLLMSpanByAttrs 判定 span 是否 GenAI 推理 span（内部转 ReadOnlySpan 读属性）。
func isLLMSpanByAttrs(s trace.Span) bool {
	ro, ok := s.(sdktrace.ReadOnlySpan)
	if !ok {
		return false
	}
	return isLLMReadOnlySpan(ro)
}

// isLLMReadOnlySpan 按 GenAI 操作名属性判定 llm 推理 span。
// Python: _is_llm_call(span_context.py:49)——attributes 读 gen_ai.operation.name。
func isLLMReadOnlySpan(s sdktrace.ReadOnlySpan) bool {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == GenAIOperationName {
			return llmOperationNames[kv.Value.AsString()]
		}
	}
	return false
}

// spanIDOf 提取 span id hex（无上下文时 "0"）。
// Python: span.context.span_id 格式化。
func spanIDOf(s trace.Span) string {
	if s == nil {
		return "0"
	}
	sc := s.SpanContext()
	if !sc.IsValid() {
		return "0"
	}
	return sc.SpanID().String()
}
