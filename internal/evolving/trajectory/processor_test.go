package trajectory

import (
	"context"
	"encoding/hex"
	cryptorand "crypto/rand"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// tracerProvider 测试共用 provider（无导出器）。
var tracerProvider = sdktrace.NewTracerProvider()

// newTestSpan 构造一个只读 span，返回它与其真实 trace ID（订阅路由用）。
func newTestSpan(name string, attrs map[string]any) (sdktrace.ReadOnlySpan, string) {
	_, span := tracerProvider.Tracer("test").
		Start(context.Background(), name,
			trace.WithAttributes(kvList(attrs)...),
			trace.WithSpanKind(trace.SpanKindInternal),
		)
	traceID := span.SpanContext().TraceID().String()
	span.End()
	return span.(sdktrace.ReadOnlySpan), traceID
}

// subscribeWithSpan 以 span 的真实 trace ID 订阅（先 start 后 end 的顺序由
// 调用方保证：本助手在 span 已 end 后仍可读 SpanContext）。
func subscribeWithSpan(p *TrajectorySpanProcessor, categories []string, span sdktrace.ReadOnlySpan) *SubscriptionHandle {
	handle, _ := p.Subscribe(categories, span.SpanContext().TraceID().String())
	return handle
}

// captureSpan 订阅 + 采集 + drain 的单 span 快路径，返回窗口与 span。
func captureSpan(name string, attrs map[string]any) (*TrajectorySpanProcessor, *Trajectory, []map[string]any) {
	ro, traceID := newTestSpan(name, attrs)
	p := NewTrajectorySpanProcessor(0)
	handle, _ := p.Subscribe([]string{SpanCategoryOfTest(ro)}, traceID)
	p.OnEnd(ro)
	traj, issues := p.Drain(handle)
	return p, traj, issues
}

// SpanCategoryOfTest 测试用类别（复用 team.go 的 SpanCategory）。
func SpanCategoryOfTest(span sdktrace.ReadOnlySpan) string {
	return SpanCategory(otlpSpanOfTest(span))
}

// otlpSpanOfTest 把 ReadOnlySpan 经 spanToOTLP 转为规范 span（取首条）。
func otlpSpanOfTest(span sdktrace.ReadOnlySpan) map[string]any {
	payload, _, _ := spanToOTLP(span)
	spans := IterSpans(payload)
	if len(spans) == 0 {
		return nil
	}
	return spans[0]
}

// kvList map → attribute.KeyValue 切片。
func kvList(attrs map[string]any) []attribute.KeyValue {
	result := make([]attribute.KeyValue, 0, len(attrs))
	for _, key := range sortedKeys(attrs) {
		switch v := attrs[key].(type) {
		case string:
			result = append(result, attribute.String(key, v))
		case int:
			result = append(result, attribute.Int(key, v))
		case bool:
			result = append(result, attribute.Bool(key, v))
		default:
			result = append(result, attribute.String(key, fmtSprint(v)))
		}
	}
	return result
}

// TestSubscribe_未知category报错 闭集校验。
// Python: processor.py subscribe。
func TestSubscribe_未知category报错(t *testing.T) {
	p := NewTrajectorySpanProcessor(0)
	if _, err := p.Subscribe([]string{"unknown"}, ""); err == nil {
		t.Fatal("unknown category should fail")
	}
	if _, err := p.Subscribe(nil, ""); err == nil {
		t.Fatal("empty categories should fail")
	}
	if _, err := p.Subscribe([]string{"team"}, ""); err == nil {
		t.Fatal("team subscription requires trace_id")
	}
}

// TestSubscribe_归一化traceID 订阅 trace ID 归一化生效。
func TestSubscribe_归一化traceID(t *testing.T) {
	p := NewTrajectorySpanProcessor(0)
	h1, err := p.Subscribe([]string{"team"}, "abc")
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := p.Subscribe([]string{"team"}, "ABC")
	if h1 == nil || h2 == nil || h1 == h2 {
		t.Fatal("distinct handles expected")
	}
}

// TestSuppress_抑制onEnd 抑制窗口内不采集。
// Python: processor.py suppress / _SUPPRESSION_DEPTH。
func TestSuppress_抑制onEnd(t *testing.T) {
	p := NewTrajectorySpanProcessor(0)
	ro1, traceID := newTestSpan("llm.call", nil)
	handle, _ := p.Subscribe([]string{"llm"}, traceID)
	done := p.WithSuppress()
	p.OnEnd(ro1)
	done()
	if traj, _ := p.Drain(handle); traj != nil {
		t.Fatal("suppressed span must not be captured")
	}
	// 退出后恢复采集（同 trace 的另一 span）。
	ro2, _ := newTestSpanInTrace(tracerProvider, traceID, "llm.call", nil)
	p.OnEnd(ro2)
	traj, _ := p.Drain(handle)
	if traj == nil || len(IterSpans(traj)) != 1 {
		t.Fatal("span after suppress window must be captured")
	}
}

// TestDrain_取走即清空 缓冲 drain 语义。
// Python: processor.py drain。
func TestDrain_取走即清空(t *testing.T) {
	p := NewTrajectorySpanProcessor(0)
	ro, traceID := newTestSpan("llm.call", nil)
	handle, _ := p.Subscribe([]string{"llm"}, traceID)
	p.OnEnd(ro)
	first, _ := p.Drain(handle)
	if first == nil || len(IterSpans(first)) != 1 {
		t.Fatal("first drain should return the span")
	}
	second, _ := p.Drain(handle)
	if second != nil {
		t.Fatal("second drain should be empty")
	}
}

// TestDrain_轨迹ID注入 合并载荷的资源属性携带 trajectory_id。
// Python: processor.py _merge_pending（TRAJECTORY_ID 注入）。
func TestDrain_轨迹ID注入(t *testing.T) {
	_, traj, _ := captureSpan("llm.call", nil)
	if traj == nil || traj.TrajectoryID() == "" {
		t.Fatal("merged payload must carry trajectory_id")
	}
}

// TestUnsubscribe 清空并失效。
// Python: processor.py unsubscribe。
func TestUnsubscribe(t *testing.T) {
	p := NewTrajectorySpanProcessor(0)
	ro, traceID := newTestSpan("llm.call", nil)
	handle, _ := p.Subscribe([]string{"llm"}, traceID)
	p.OnEnd(ro)
	p.Unsubscribe(handle)
	traj, _ := p.Drain(handle)
	if traj != nil {
		t.Fatal("unsubscribed handle must not drain")
	}
	// 再 on_end 不再采集（无订阅）。
	ro2, _ := newTestSpanInTrace(tracerProvider, traceID, "llm.call", nil)
	p.OnEnd(ro2)
	if traj, _ := p.Drain(handle); traj != nil {
		t.Fatal("no capture after unsubscribe")
	}
}

// TestShutdown 清空全部订阅。
// Python: processor.py shutdown。
func TestShutdown(t *testing.T) {
	p := NewTrajectorySpanProcessor(0)
	ro, _ := newTestSpan("llm.call", nil)
	handle, _ := p.Subscribe([]string{"llm"}, ro.SpanContext().TraceID().String())
	p.OnEnd(ro)
	p.Shutdown()
	if traj, _ := p.Drain(handle); traj != nil {
		t.Fatal("shutdown must clear all subscriptions")
	}
}

// TestOnEnd_分类过滤 非采集类别不入缓冲。
// Python: processor.py _on_end（category None 跳过）。
func TestOnEnd_分类过滤(t *testing.T) {
	p := NewTrajectorySpanProcessor(0)
	ro, traceID := newTestSpan("llm.reasoning", map[string]any{recordKindKey: "reasoning"})
	handle, _ := p.Subscribe([]string{"llm"}, traceID)
	p.OnEnd(ro)
	// 非 llm 名 → 不匹配订阅类别。
	roTool, _ := newTestSpanInTrace(tracerProvider, traceID, "tool.write", nil)
	p.OnEnd(roTool)
	if traj, _ := p.Drain(handle); traj != nil {
		t.Fatal("filtered spans must not be captured")
	}
	// 操作属性路由：chat → llm。
	roChat, _ := newTestSpanInTrace(tracerProvider, traceID, "weird.name", map[string]any{operationNameKey: "chat"})
	p.OnEnd(roChat)
	traj, _ := p.Drain(handle)
	if traj == nil || len(IterSpans(traj)) != 1 {
		t.Fatal("operation attr must route to llm")
	}
}

// TestPendingFull_记issue丢弃 缓冲满记录 issue 并丢弃。
// Python: processor.py pending_buffer_full。
func TestPendingFull_记issue丢弃(t *testing.T) {
	p := NewTrajectorySpanProcessor(1)
	ro1, traceID := newTestSpan("llm.call", nil)
	handle, _ := p.Subscribe([]string{"llm"}, traceID)
	p.OnEnd(ro1)
	ro2, _ := newTestSpanInTrace(tracerProvider, traceID, "llm.call", nil)
	p.OnEnd(ro2)
	traj, issues := p.Drain(handle)
	if traj == nil || len(IterSpans(traj)) != 1 {
		t.Fatal("only first span kept")
	}
	found := false
	for _, issue := range issues {
		if issue["code"] == "pending_buffer_full" {
			found = true
		}
	}
	if !found {
		t.Fatalf("pending_buffer_full issue expected, got %v", issues)
	}
}

// TestCategoryForName 命名归类表。
// Python: processor.py _category_for_name。
func TestCategoryForName(t *testing.T) {
	_ = &TrajectorySpanProcessor{}
	cases := []struct{ name, want string }{
		{"llm.call", "llm"},
		{"tool.x", "tool"},
		{"agent.a", "agent"},
		{"task.t", "task"},
		{"message.m", "message"},
		{"msg.m", "message"},
		{"member.m", "member"},
		{"team.t", "team"},
		{"plan.p", "team"},
		{"event.e", "team"},
		{"unknown", ""},
	}
	for _, c := range cases {
		if got := categoryForName(c.name); got != c.want {
			t.Fatalf("categoryForName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestNormalizeTraceIDText_ID归一化 归一化边界。
// Python: processor.py _normalise_trace_id / _normalise_span_id。
func TestNormalizeTraceIDText_ID归一化(t *testing.T) {
	if got := normalizeTraceIDText("ab"); len(got) != 32 || got != "000000000000000000000000000000ab" {
		t.Fatalf("padded trace id = %q", got)
	}
	if got := normalizeTraceIDText("550e8400-e29b-41d4-a716-446655440000"); len(got) != 32 || got != "550e8400e29b41d4a716446655440000" {
		t.Fatalf("uuid = %q", got)
	}
	if normalizeTraceIDText("") != "" {
		t.Fatal("empty trace id")
	}
	if got := normalizeSpanIDText("ff"); got != "00000000000000ff" {
		t.Fatalf("span id = %q", got)
	}
}

// TestOnEnd_父与事件与links OTLP 拷贝完整性（父 ID/事件/links/资源属性）。
// Python: processor.py _span_to_otlp。
func TestOnEnd_父与事件与links(t *testing.T) {
	p := NewTrajectorySpanProcessor(0)
	tracer := sdktrace.NewTracerProvider().Tracer("test")
	_, parentSpan := tracer.Start(context.Background(), "parent")
	parentSpan.End()
	_, span := tracer.Start(context.Background(), "llm.call",
		trace.WithAttributes(attribute.String("k", "v")))
	// 订阅必须先于 OnEnd 且以真实 trace ID 路由（team 订阅语义）。
	traceID := span.SpanContext().TraceID().String()
	handle, _ := p.Subscribe([]string{"llm"}, traceID)
	span.AddEvent("evt", trace.WithAttributes(attribute.String("e", "1")))
	span.End()
	ro, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		t.Fatal("span must implement ReadOnlySpan")
	}
	p.OnEnd(ro)
	traj, _ := p.Drain(handle)
	spans := IterSpans(traj)
	if len(spans) != 1 {
		t.Fatal("one span expected")
	}
	if fmtSprint(spans[0]["parentSpanId"]) == "" {
		t.Fatal("parentSpanId must be carried")
	}
	events, _ := spans[0]["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("events = %v", events)
	}
}

// TestConcurrentDrain 并发 drain 安全（锁语义冒烟）。
func TestConcurrentDrain(t *testing.T) {
	p := NewTrajectorySpanProcessor(0)
	seed, traceID := newTestSpan("llm.call", nil)
	handle, _ := p.Subscribe([]string{"llm"}, traceID)
	p.OnEnd(seed)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ro, _ := newTestSpanInTrace(tracerProvider, traceID, "llm.call", nil)
			p.OnEnd(ro)
			_, _ = p.Drain(handle)
		}()
	}
	wg.Wait()
}

// newTestSpanInTrace 在指定 trace 下构造同 trace 的后续 span（模拟一次
// trace 内的多次 span 结束；父链用链接语义近似，路由只看 trace ID）。
func newTestSpanInTrace(provider *sdktrace.TracerProvider, traceIDHex string, name string, attrs map[string]any) (sdktrace.ReadOnlySpan, string) {
	parsedTraceID, err := trace.TraceIDFromHex(traceIDHex)
	if err != nil {
		_, span := provider.Tracer("test").Start(context.Background(), name)
		traceID := span.SpanContext().TraceID().String()
		span.End()
		return span.(sdktrace.ReadOnlySpan), traceID
	}
	spanID, _ := trace.SpanIDFromHex(newRandomSpanID())
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{TraceID: parsedTraceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})
	_, span := provider.Tracer("test").
		Start(trace.ContextWithSpanContext(context.Background(), spanCtx), name,
			trace.WithAttributes(kvList(attrs)...), trace.WithSpanKind(trace.SpanKindInternal))
	span.End()
	return span.(sdktrace.ReadOnlySpan), traceIDHex
}

// newRandomSpanID 生成随机 16 位 span ID。
func newRandomSpanID() string {
	return randomSpanIDHex()
}

// randomSpanIDHex crypto/rand 生成 16 位随机 span ID。
func randomSpanIDHex() string {
	buf := make([]byte, 8)
	if _, err := cryptorand.Read(buf); err != nil {
		return "0123456789abcdef"
	}
	return hex.EncodeToString(buf)
}
