package trajectory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	obs "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/observability"
)

// processor.go 完成的 OpenTelemetry span 到轨迹的进程内路由。
// 对齐 Python: agent_evolving/trajectory/processor.py（全文，2026-12-04 快照）。
// processor 刻意不承担导出或持久化职责；订阅持有一个小的 pending 缓冲，
// Drain 把缓冲转移到规范 Trajectory。

// ──────────────────────────── 常量 ────────────────────────────

// ProcessorCategories 可订阅的 span 类别闭集。
// Python: processor.py _CATEGORIES。
var ProcessorCategories = map[string]struct{}{
	"llm": {}, "tool": {}, "event": {}, "agent": {}, "task": {},
	"message": {}, "member": {}, "team": {},
}

// categoryByOperation LLM/tool span 以其承载的 GenAI 操作命名（"chat gpt-4o"、
// "execute_tool search"），名字内嵌模型/工具名无法前缀匹配；操作属性是稳定事实。
// Python: processor.py _CATEGORY_BY_OPERATION。
var categoryByOperation = map[string]string{"chat": "llm", "execute_tool": "tool"}

// maxIssues 每订阅 issue 上限。
// Python: processor.py _MAX_ISSUES = 128。
const maxIssues = 128

// ──────────────────────────── 结构体 ────────────────────────────

// SubscriptionHandle subscribe 返回的不透明句柄。
// 刻意不暴露路由状态；状态保存在 processor 拥有的表里，使句柄可跨任务
// 传递而不让 rail 依赖实现细节。
// Python: processor.py _SubscriptionHandle。
type SubscriptionHandle struct {
	// id 句柄标识（Go 显式注册表语义，偏差 D-25.2b-4）。
	id uint64
}

// pendingSpan 缓冲中的一条待取 span。
// Python: processor.py _PendingSpan。
type pendingSpan struct {
	identity SpanIdentity
	payload  map[string]any
	endTime  int64
}

// subscriptionState 一个订阅的路由状态。
// Python: processor.py _SubscriptionState。
type subscriptionState struct {
	categories map[string]struct{}
	traceID    string // 空串表示 None（Python trace_id=None）
	pending    []pendingSpan
	pendingIDs map[SpanIdentity]struct{}
	issues     []map[string]any
	active     bool
}

// TrajectorySpanProcessor 把结束的观测 span 扇出到隔离订阅。
// Python: processor.py TrajectorySpanProcessor(SpanProcessor)。
type TrajectorySpanProcessor struct {
	mu            sync.Mutex
	maxPending    int
	subs          map[*SubscriptionHandle]*subscriptionState
	nextID        uint64
	suppressDepth int
}

// recordKindKey / operationNameKey span 归类读取的属性键（observability 常量）。
var (
	recordKindKey    = obs.OJTrajectoryRecordKind
	operationNameKey = obs.GenAIOperationName
)

// CaptureIssue 采集质量问题记录。
// Python: processor.py _issue 返回的 Mapping。
type CaptureIssue struct {
	Code     string
	Message  string
	SpanName string
	TraceID  string
	SpanID   string
}

// asMap issue 的映射形态（对齐 Python dict(issue) 消费面）。
func (c CaptureIssue) asMap() map[string]any {
	data := map[string]any{"code": c.Code}
	if c.Message != "" {
		data["message"] = c.Message
	}
	if c.SpanName != "" {
		data["span_name"] = c.SpanName
	}
	if c.TraceID != "" {
		data["trace_id"] = c.TraceID
	}
	if c.SpanID != "" {
		data["span_id"] = c.SpanID
	}
	return data
}

// buildIssue 构造私有不可变 issue 形态。
// Python: processor.py _issue。
func buildIssue(code string, message, spanName, traceID, spanID string) CaptureIssue {
	return CaptureIssue{
		Code:     code,
		Message:  truncateText(message, 512),
		SpanName: spanName,
		TraceID:  traceID,
		SpanID:   spanID,
	}
}

// truncateText 截断文本。
func truncateText(text string, limit int) string {
	if len(text) > limit {
		return text[:limit]
	}
	return text
}

// NewTrajectorySpanProcessor 创建 processor。
// Python: processor.py __init__（max_pending_spans 可 None = 无上限）。
func NewTrajectorySpanProcessor(maxPendingSpans int) *TrajectorySpanProcessor {
	return &TrajectorySpanProcessor{
		maxPending: maxPendingSpans,
		subs:       map[*SubscriptionHandle]*subscriptionState{},
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Subscribe 订阅指定类别集合（可选绑 trace_id；team 订阅必须带）。
// Python: processor.py subscribe。
func (p *TrajectorySpanProcessor) Subscribe(includeCategories []string, traceID string) (*SubscriptionHandle, error) {
	categories := map[string]struct{}{}
	for _, category := range includeCategories {
		if _, known := ProcessorCategories[category]; !known {
			return nil, fmt.Errorf("unsupported span categories: %s", category)
		}
		categories[category] = struct{}{}
	}
	if len(categories) == 0 {
		return nil, fmt.Errorf("include_span_categories must not be empty")
	}
	if _, hasTeam := categories["team"]; hasTeam && traceID == "" {
		return nil, fmt.Errorf("team subscriptions require a trace_id")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	handle := &SubscriptionHandle{id: p.nextID}
	p.subs[handle] = &subscriptionState{
		categories: categories,
		traceID:    normalizeTraceIDText(traceID),
		pendingIDs: map[SpanIdentity]struct{}{},
		active:     true,
	}
	return handle, nil
}

// Unsubscribe 移除订阅并清空其缓冲。
// Python: processor.py unsubscribe。
func (p *TrajectorySpanProcessor) Unsubscribe(handle *SubscriptionHandle) {
	if handle == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if state, exists := p.subs[handle]; exists {
		state.active = false
		state.pending = nil
		state.pendingIDs = map[SpanIdentity]struct{}{}
		state.issues = nil
	}
	delete(p.subs, handle)
}

// Drain 取走并清空该订阅 pending，规范化合并为 Trajectory。
// Python: processor.py drain（Trajectory.from_otlp(_merge_pending(pending))；
// 转换失败返回 nil + issues + capture issue）。
func (p *TrajectorySpanProcessor) Drain(handle *SubscriptionHandle) (*Trajectory, []map[string]any) {
	if handle == nil {
		return nil, nil
	}
	p.mu.Lock()
	state, exists := p.subs[handle]
	if !exists || !state.active {
		p.mu.Unlock()
		return nil, nil
	}
	pending := state.pending
	state.pending = nil
	state.pendingIDs = map[SpanIdentity]struct{}{}
	issues := state.issues
	state.issues = nil
	p.mu.Unlock()
	if len(pending) == 0 {
		return nil, issues
	}
	payload, err := mergePending(pending)
	if err != nil {
		issue := buildIssue("trajectory_conversion_error", err.Error(), "", "", "")
		all := append(issues, issue.asMap())
		return nil, all
	}
	trajectory, err := FromOTLP(payload)
	if err != nil {
		issue := buildIssue("trajectory_conversion_error", err.Error(), "", "", "")
		all := append(issues, issue.asMap())
		return nil, all
	}
	return trajectory, issues
}

// DrainedIssues 把 CaptureIssue 列表转为映射列表（消费面对齐 Python）。
func DrainedIssues(issues []CaptureIssue) []map[string]any {
	result := make([]map[string]any, 0, len(issues))
	for _, issue := range issues {
		result = append(result, issue.asMap())
	}
	return result
}

// Suppress 抑制窗口内的 on_end 采集（Python contextmanager 语义；
// Go 以显式计数器实现，偏差 D-25.2b-4）。
type Suppress func()

// WithSuppress 进入抑制态，返回退出函数。
func (p *TrajectorySpanProcessor) WithSuppress() Suppress {
	p.mu.Lock()
	p.suppressDepth++
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		p.suppressDepth--
		p.mu.Unlock()
	}
}

// OnStart 空操作。
// Python: processor.py on_start。
func (p *TrajectorySpanProcessor) OnStart(_ context.Context, _ sdktrace.ReadWriteSpan) {}

// OnEnd 捕获一个结束的 span，绝不影响调用线程。
// OTel 在 span.end() 中同步调用 processor；畸形 span 或路由侧失败不得
// 变成业务路径异常——尽力记录 issue，issue 侧自身也受保护。
// Python: processor.py on_end / _on_end。
func (p *TrajectorySpanProcessor) OnEnd(span sdktrace.ReadOnlySpan) {
	defer func() {
		if r := recover(); r != nil {
			p.safeLogIssue("span processing issue recording failed", fmt.Errorf("%v", r))
		}
	}()
	p.mu.Lock()
	depth := p.suppressDepth
	p.mu.Unlock()
	if depth > 0 {
		return
	}
	spanName := span.Name()
	category := p.categoryForSpan(spanName, attrMapOf(span))
	if category == "" {
		return
	}
	payload, identity, err := spanToOTLP(span)
	if err != nil {
		p.recordCaptureIssue(spanName, span, err, category, "span_conversion_error")
		return
	}
	traceID := identity.TraceID
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, state := range p.subs {
		if !state.active {
			continue
		}
		if _, wanted := state.categories[category]; !wanted {
			continue
		}
		if state.traceID != "" && state.traceID != traceID {
			// 绑定 trace 的订阅（team scope）按 trace 匹配。
			continue
		}
		// 未绑定 trace 的订阅（agent scope）= 活跃订阅：Python 的
		// _ACTIVE_SUBSCRIPTIONS contextvar 语义（订阅期间一律接收），
		// Go 以 state.active 表达（偏差 D-25.2b-4）。
		if _, duplicated := state.pendingIDs[*identity]; duplicated {
			continue
		}
		if p.maxPending > 0 && len(state.pending) >= p.maxPending {
			issue := buildIssue("pending_buffer_full", "pending span buffer is full",
				spanName, traceID, identity.SpanID)
			p.appendIssueLocked(state, issue)
			continue
		}
		state.pending = append(state.pending, pendingSpan{
			identity: *identity,
			payload:  copyJSON(payload).(map[string]any),
			endTime:  endTimeOf(payload),
		})
		state.pendingIDs[*identity] = struct{}{}
	}
}

// ForceFlush 无操作（采集是同步的）。
// Python: processor.py force_flush。
func (p *TrajectorySpanProcessor) ForceFlush(_ context.Context) error { return nil }

// Shutdown 清空全部订阅。
// Python: processor.py shutdown。
func (p *TrajectorySpanProcessor) Shutdown(_ context.Context) error {
	p.shutdownLocked()
	return nil
}

// shutdownLocked 槽位清理本体（分离以满足 SpanProcessor 接口签名）。
func (p *TrajectorySpanProcessor) shutdownLocked() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, state := range p.subs {
		state.active = false
		state.pending = nil
		state.pendingIDs = map[SpanIdentity]struct{}{}
		state.issues = nil
	}
	p.subs = map[*SubscriptionHandle]*subscriptionState{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// suppressDepth 抑制深度（Python _SUPPRESSION_DEPTH contextvar 的显式化）。
// 附属字段；构造零值即可用。
// 偏差 D-25.2b-4 登记。

// categoryForSpan 按 GenAI 操作归类，回落到 span 名。
// Python: processor.py _category_for_span / _category_for_name。
func (p *TrajectorySpanProcessor) categoryForSpan(spanName string, attributes map[string]any) string {
	// record kind 先于操作判定：reasoning 子 span 也陈述 chat，但它是父
	// 输出的复述，只会消耗窗口的 span 名额。
	recordKind := fmt.Sprint(attributes[recordKindKey])
	if recordKind == "event" {
		return "event"
	}
	if recordKind == "reasoning" {
		return ""
	}
	operation := fmt.Sprint(attributes[operationNameKey])
	if category, exists := categoryByOperation[operation]; exists {
		return category
	}
	return categoryForName(spanName)
}

// categoryForName 按命名前缀归类。
// Python: processor.py _category_for_name。
func categoryForName(name string) string {
	if name == "llm.call" {
		return "llm"
	}
	for _, pair := range []struct {
		prefix   string
		category string
	}{
		{"tool.", "tool"},
		{"agent.", "agent"},
		{"task.", "task"},
	} {
		if len(name) >= len(pair.prefix) && name[:len(pair.prefix)] == pair.prefix {
			return pair.category
		}
	}
	for _, prefix := range []string{"message.", "msg."} {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			return "message"
		}
	}
	if len(name) >= 7 && name[:7] == "member." {
		return "member"
	}
	for _, prefix := range []string{"team.", "plan.", "event."} {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			return "team"
		}
	}
	return ""
}

// appendIssueLocked 追加 issue（持锁调用）+ 告警。
// Python: processor.py _append_issue。
func (p *TrajectorySpanProcessor) appendIssueLocked(state *subscriptionState, issue CaptureIssue) {
	if len(state.issues) < maxIssues {
		state.issues = append(state.issues, issue.asMap())
	}
	p.safeLogIssue("trajectory span processor capture issue", fmt.Errorf("%v", issue.asMap()))
}

// recordCaptureIssue 以隔离 issue 侧失败的方式路由稳定采集 issue。
// Python: processor.py _record_capture_issue。
func (p *TrajectorySpanProcessor) recordCaptureIssue(spanName string, span sdktrace.ReadOnlySpan, exc error, category, code string) {
	var identity SpanIdentity
	traceID := ""
	spanID := ""
	if ctx := span.SpanContext(); ctx.HasTraceID() && ctx.HasSpanID() {
		identity = SpanIdentity{TraceID: normalizeTraceIDText(ctx.TraceID().String()), SpanID: normalizeSpanIDText(ctx.SpanID().String())}
		traceID = identity.TraceID
		spanID = identity.SpanID
	}
	issue := buildIssue(code, exc.Error(), spanName, traceID, spanID)
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, state := range p.subs {
		if !state.active {
			continue
		}
		if _, wanted := state.categories[category]; !wanted {
			continue
		}
		if state.traceID != "" && state.traceID != traceID {
			continue
		}
		p.appendIssueLocked(state, issue)
	}
}

// safeLogIssue 日志次于采集，不得逃出 on_end。
// Python: processor.py _safe_log_issue。
func (p *TrajectorySpanProcessor) safeLogIssue(message string, exc error) {
	_ = message
	_ = exc // 静默尽力：与 Python suppress(Exception) 语义一致
}

// mergePending 合并缓冲 span（保留不同的 OTLP 资源/scope 组）。
// Python: processor.py _merge_pending。
func mergePending(pending []pendingSpan) (map[string]any, error) {
	trajectoryID := pending[0].identity.TraceID
	resourceGroups := map[string]map[string]any{}
	scopeGroups := map[string]map[string]any{}
	var resourceOrder []string
	scopeOrder := map[string][]string{}
	sorted := make([]pendingSpan, len(pending))
	copy(sorted, pending)
	sortPendingSpans(sorted)
	for _, item := range sorted {
		resourceSpans, _ := item.payload["resourceSpans"].([]any)
		for _, rsItem := range resourceSpans {
			sourceResourceSpan, isMap := rsItem.(map[string]any)
			if !isMap {
				continue
			}
			resource, _ := sourceResourceSpan["resource"].(map[string]any)
			if resource == nil {
				resource = map[string]any{}
			}
			resourceKey := stableKey(resource)
			targetResource, exists := resourceGroups[resourceKey]
			if !exists {
				targetResource = map[string]any{"resource": copyJSON(resource), "scopeSpans": []any{}}
				resourceGroups[resourceKey] = targetResource
				resourceOrder = append(resourceOrder, resourceKey)
			}
			scopeSpans, _ := sourceResourceSpan["scopeSpans"].([]any)
			for _, ssItem := range scopeSpans {
				sourceScopeSpan, isMap := ssItem.(map[string]any)
				if !isMap {
					continue
				}
				scope, _ := sourceScopeSpan["scope"].(map[string]any)
				if scope == nil {
					scope = map[string]any{}
				}
				scopeKey := resourceKey + "\x00" + stableKey(scope)
				targetScope, exists := scopeGroups[scopeKey]
				if !exists {
					targetScope = map[string]any{"scope": copyJSON(scope), "spans": []any{}}
					scopeGroups[scopeKey] = targetScope
					targetResource["scopeSpans"] = append(targetResource["scopeSpans"].([]any), targetScope)
					scopeOrder[resourceKey] = append(scopeOrder[resourceKey], scopeKey)
				}
				spans, _ := sourceScopeSpan["spans"].([]any)
				for _, spanItem := range spans {
					targetScope["spans"] = append(targetScope["spans"].([]any), copyJSON(spanItem))
				}
			}
		}
	}
	result := map[string]any{"resourceSpans": []any{}}
	resourceSpansList := result["resourceSpans"].([]any)
	for _, resourceKey := range resourceOrder {
		resourceSpan := resourceGroups[resourceKey]
		// 资源属性注入 trajectory_id（首条身份）。
		resource := resourceSpan["resource"].(map[string]any)
		attributes := AttributesToMap(resource["attributes"])
		attributes[TrajectoryID] = trajectoryID
		resource["attributes"] = AttributesFromMap(attributes)
		resourceSpansList = append(resourceSpansList, resourceSpan)
	}
	result["resourceSpans"] = resourceSpansList
	return NormalizeOTLP(result)
}

// sortPendingSpans 按 (end_time, identity) 排序。
// Python: _merge_pending 的 sorted(key=(end_time, identity))。
func sortPendingSpans(spans []pendingSpan) {
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0; j-- {
			a, b := spans[j-1], spans[j]
			if a.endTime < b.endTime || (a.endTime == b.endTime &&
				(a.identity.TraceID < b.identity.TraceID ||
					(a.identity.TraceID == b.identity.TraceID && a.identity.SpanID < b.identity.SpanID))) {
				continue
			}
			spans[j-1], spans[j] = b, a
		}
	}
}

// endTimeOf 读载荷内 span 的结束时间。
// Python: processor.py _end_time。
func endTimeOf(payload map[string]any) int64 {
	resourceSpans, _ := payload["resourceSpans"].([]any)
	if len(resourceSpans) == 0 {
		return 0
	}
	resourceSpan, _ := resourceSpans[0].(map[string]any)
	if resourceSpan == nil {
		return 0
	}
	scopeSpans, _ := resourceSpan["scopeSpans"].([]any)
	if len(scopeSpans) == 0 {
		return 0
	}
	scopeSpan, _ := scopeSpans[0].(map[string]any)
	if scopeSpan == nil {
		return 0
	}
	spans, _ := scopeSpan["spans"].([]any)
	if len(spans) == 0 {
		return 0
	}
	span, _ := spans[0].(map[string]any)
	if span == nil {
		return 0
	}
	return timeValue(span, "endTimeUnixNano")
}

// stableKey 值的稳定 JSON 键（排序键）。
// Python: processor.py _stable_key。
func stableKey(value any) string {
	ordered := sortJSONKeys(toJsonCompatible(value))
	data, err := jsonMarshal(ordered)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

// normalizeTraceIDText 归一化 OTel 整数/hex/UUID ID（宽容测试替身）。
// Python: processor.py _normalise_trace_id。
func normalizeTraceIDText(value string) string {
	text := trimSpaces(value)
	if text == "" {
		return ""
	}
	if len(text) <= 32 && isHex(text) {
		return padLeft(text, 32)
	}
	// UUID 形态去除连字符后视作 hex。
	cleaned := removeDashes(text)
	if len(cleaned) == 32 && isHex(cleaned) {
		return cleaned
	}
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:])[:32]
}

// normalizeSpanIDText 归一化 span ID（16 位）。
// Python: processor.py _normalise_span_id。
func normalizeSpanIDText(value string) string {
	text := trimSpaces(value)
	if text == "" {
		return ""
	}
	if len(text) <= 16 && isHex(text) {
		return padLeft(text, 16)
	}
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:])[:16]
}

// isHex 是否全 hex 字符。
func isHex(text string) bool {
	for _, c := range text {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(text) > 0
}

// padLeft 左填充 0 到指定宽度。
func padLeft(text string, width int) string {
	for len(text) < width {
		text = "0" + text
	}
	return text
}

// removeDashes 去除连字符。
func removeDashes(text string) string {
	result := make([]byte, 0, len(text))
	for i := 0; i < len(text); i++ {
		if text[i] != '-' {
			result = append(result, text[i])
		}
	}
	return string(result)
}

// trimSpaces 修剪空白。
func trimSpaces(text string) string {
	start, end := 0, len(text)
	for start < end && (text[start] == ' ' || text[start] == '\t') {
		start++
	}
	for end > start && (text[end-1] == ' ' || text[end-1] == '\t') {
		end--
	}
	return text[start:end]
}
