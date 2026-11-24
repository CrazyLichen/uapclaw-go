package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// TestRootSpanRegistry_注册与清除 session 注册表的基本生命周期。
// Python: set_root_span/get_root_span/clear_root_span（span_context.py:716/:748/:829）。
func TestRootSpanRegistry_注册与清除(t *testing.T) {
	rootRegistry = map[string]trace.Span{}
	defer func() { rootRegistry = map[string]trace.Span{} }()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tracer := tp.Tracer("root-registry-test")

	ctx, root := tracer.Start(context.Background(), "agent.plan.s1")
	defer root.End()

	SetRootSpan(root, "s1")
	assert.Equal(t, root, GetRootSpan("s1"))
	assert.Equal(t, root, GetSessionRootSpanT(t, "s1"))

	// expectedSpan 不匹配时不误删并发替换者
	_, other := tracer.Start(ctx, "agent.plan.s1-other")
	SetRootSpan(other, "s1")
	ClearRootSpan("s1", root)
	assert.Equal(t, other, GetRootSpan("s1"))
	other.End()
	ClearRootSpan("s1", other)
	assert.Nil(t, GetRootSpan("s1"))
}

// GetSessionRootSpanT 测试包装：错误断言收敛。
func GetSessionRootSpanT(t *testing.T, sessionID string) trace.Span {
	span, err := GetSessionRootSpan(sessionID)
	require.NoError(t, err)
	return span
}

// TestRootSpanRegistry_陈旧项清理 非 recording 的注册项读取时清除。
// Python: _registered_root_span(span_context.py:805)。
func TestRootSpanRegistry_陈旧项清理(t *testing.T) {
	rootRegistry = map[string]trace.Span{}
	defer func() { rootRegistry = map[string]trace.Span{} }()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	_, root := tp.Tracer("stale-test").Start(context.Background(), "agent.run.s2")
	SetRootSpan(root, "s2")
	root.End()

	assert.Nil(t, GetRootSpan("s2"), "已收口的根不应返回")
	rootRegistryLock.RLock()
	_, exists := rootRegistry["s2"]
	rootRegistryLock.RUnlock()
	assert.False(t, exists, "陈旧项应被清除")
}

// TestRootSpanFallback_回退钩子 注册表未命中时走兜底钩子。
// Python: install_root_span_fallback 的 session 注册表回退（harness 层注册）。
func TestRootSpanFallback_回退钩子(t *testing.T) {
	rootRegistry = map[string]trace.Span{}
	defer func() { rootRegistry = map[string]trace.Span{}; SetRootSpanFallback(nil) }()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	_, root := tp.Tracer("fallback-test").Start(context.Background(), "agent.run.h1")

	SetRootSpanFallback(func(sessionID string) trace.Span {
		if sessionID == "h1" {
			return root
		}
		return nil
	})

	assert.Equal(t, root, GetRootSpan("h1"))
	assert.Nil(t, GetRootSpan("other"), "兜底只应答自己的 session")
}

// TestActiveSpanTracker_追踪与清算 活动跟踪：End 的移除、未 End 的被 flush。
// Python: ActiveSpanTracker on_start/on_end/flush_spans_for_trace。
func TestActiveSpanTracker_追踪与清算(t *testing.T) {
	tracker := NewActiveSpanTracker()
	SetActiveSpanTracker(tracker)
	defer SetActiveSpanTracker(nil)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(tracker),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tracer := tp.Tracer("tracker-test")

	// 正常收口的 span
	_, closedSpan := tracer.Start(context.Background(), "llm.call.ok")
	closedSpan.End()

	// 未收口的 llm span（带 chat 操作属性）
	_, leaked := tracer.Start(context.Background(), "llm.call.leak",
		trace.WithAttributes(
			attribute.String(GenAIOperationName, "chat"),
		))

	traceID := leaked.SpanContext().TraceID().String()
	closed := tracker.FlushSpansForTrace(traceID, "")
	assert.Equal(t, 1, closed, "只应清算未收口的 llm span")
	assert.False(t, leaked.IsRecording(), "泄漏 span 应被强制收口")

	// 强制收口标记
	assert.True(t, hasAttrBool(leaked, OJSpanForcedClose))
}

// TestCascadeCloseChildren_收口孤儿工具与LLMspan 安全网：孤儿 tool span 直接收口、
// 父为 agent span 的孤儿 llm span 经 tracker 收口。
// Python: cascade_close_children（span_context.py:913）。
func TestCascadeCloseChildren_收口孤儿工具与LLMspan(t *testing.T) {
	tracker := NewActiveSpanTracker()
	SetActiveSpanTracker(tracker)
	defer SetActiveSpanTracker(nil)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(tracker),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tracer := tp.Tracer("cascade-test")

	state := InitSpanState()
	ctx := WithSpanState(context.Background(), state)

	_, agentSpan := tracer.Start(ctx, "agent.test")
	state.PushAgentSpan("a1", agentSpan)
	agentCtx := trace.ContextWithSpanContext(ctx, agentSpan.SpanContext())

	// 孤儿 tool span（父 = agent span，对齐生产上下文继承）
	_, toolSpan := tracer.Start(agentCtx, "execute_tool.search")
	state.PushToolSpan("search", toolSpan)

	// 孤儿 llm span（父 = agent span）
	_, llmSpan := tracer.Start(agentCtx, "llm.call",
		trace.WithAttributes(
			attribute.String(GenAIOperationName, "chat"),
		))
	tracker.RegisterLLMSpan("call-1", llmSpan)

	closed := CascadeCloseChildren(ctx)
	assert.Equal(t, 2, closed, "tool + llm 各收口一个")
	assert.False(t, toolSpan.IsRecording())
	assert.False(t, llmSpan.IsRecording())
	assert.True(t, hasAttrBool(llmSpan, OJSpanForcedClose))
}

// hasAttrBool 断言 span 是否带某布尔属性（读侧经 ReadOnlySpan）。
func hasAttrBool(span trace.Span, key string) bool {
	ro, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		return false
	}
	for _, kv := range ro.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsBool()
		}
	}
	return false
}
