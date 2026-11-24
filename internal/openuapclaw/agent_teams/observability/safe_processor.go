package observability

import (
	"context"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// safeProcessor OTel SpanProcessor 的 panic 兜底包装。
// Python: extensions/observability/runtime.py SafeSpanProcessor（每个方法 try/except 吞异常）。
// 观测链路故障不得打断主执行链路：OnEnd 等方法 panic 只记日志不上抛。
type safeProcessor struct {
	inner sdktrace.SpanProcessor
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newSafeProcessor 用 panic 兜底包装一个 SpanProcessor。
// Python: SafeSpanProcessor(processor)（runtime.py _register_span_processors 挂载前统一包装）。
func newSafeProcessor(inner sdktrace.SpanProcessor) sdktrace.SpanProcessor {
	return &safeProcessor{inner: inner}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// OnStart 包装 inner.OnStart。
func (p *safeProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	defer catchProcessorPanic("OnStart")
	p.inner.OnStart(parent, s)
}

// OnEnd 包装 inner.OnEnd。
func (p *safeProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	defer catchProcessorPanic("OnEnd")
	p.inner.OnEnd(s)
}

// Shutdown 包装 inner.Shutdown。
func (p *safeProcessor) Shutdown(ctx context.Context) error {
	defer catchProcessorPanic("Shutdown")
	return p.inner.Shutdown(ctx)
}

// ForceFlush 包装 inner.ForceFlush。
func (p *safeProcessor) ForceFlush(ctx context.Context) error {
	defer catchProcessorPanic("ForceFlush")
	return p.inner.ForceFlush(ctx)
}

// catchProcessorPanic panic 兜底：记录并吞掉，对齐 Python SafeSpanProcessor 的
// except Exception 语义（观测故障不影响主链路）。
func catchProcessorPanic(method string) {
	if rec := recover(); rec != nil {
		logger.Error(logComponent).Str("method", method).Any("panic", rec).
			Msg("span processor panic suppressed")
	}
}
