package observability

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// recordingProcessor 记录 OnEnd 调用次数的 mock processor。
type recordingProcessor struct {
	onEndCount atomic.Int64
}

func (p *recordingProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (p *recordingProcessor) OnEnd(sdktrace.ReadOnlySpan)                     { p.onEndCount.Add(1) }
func (p *recordingProcessor) Shutdown(context.Context) error                  { return nil }
func (p *recordingProcessor) ForceFlush(context.Context) error                { return nil }

// panickingProcessor OnEnd 必 panic 的 mock processor。
type panickingProcessor struct{}

func (p *panickingProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (p *panickingProcessor) OnEnd(sdktrace.ReadOnlySpan)                     { panic("boom") }
func (p *panickingProcessor) Shutdown(context.Context) error                  { return nil }
func (p *panickingProcessor) ForceFlush(context.Context) error                { return nil }

func resetObservabilityGlobals() {
	provider = nil
	callbackHandler = nil
	monitorHandler = nil
	registeredUnsubs = nil
	activeConfig = nil
}

// TestInitObservability_附加处理器被挂载 附加 processors 经 safeProcessor 挂进
// provider，span End 时收到回调。Python: demand.py:158 → runtime._register_span_processors。
func TestInitObservability_附加处理器被挂载(t *testing.T) {
	resetObservabilityGlobals()
	defer resetObservabilityGlobals()

	rec := &recordingProcessor{}
	cfg := &ObservabilityConfig{Enabled: true, ServiceName: "test", Exporter: "console", SampleRate: 1}
	err := InitObservability(cfg,
		WithSpanExporterOverride(newTestExporter()),
		WithAdditionalSpanProcessors(rec))
	require.NoError(t, err)
	defer ShutdownObservability()

	// 开一个 span 并结束，附加 processor 应收到 OnEnd
	_, span := GetTracer("mount-test").Start(context.Background(), "test.span")
	span.End()

	assert.GreaterOrEqual(t, rec.onEndCount.Load(), int64(1), "附加 processor 应收到 OnEnd")
}

// TestInitObservability_同实例附加处理器去重 同一 processor 实例重复传入只挂载一次。
// Python: runtime.py:225-228 existing is processor 去重。
func TestInitObservability_同实例附加处理器去重(t *testing.T) {
	resetObservabilityGlobals()
	defer resetObservabilityGlobals()

	rec := &recordingProcessor{}
	cfg := &ObservabilityConfig{Enabled: true, ServiceName: "test", Exporter: "console", SampleRate: 1}
	err := InitObservability(cfg,
		WithSpanExporterOverride(newTestExporter()),
		WithAdditionalSpanProcessors(rec, rec, rec))
	require.NoError(t, err)
	defer ShutdownObservability()

	_, span := GetTracer("dedup-test").Start(context.Background(), "test.span")
	span.End()

	assert.Equal(t, int64(1), rec.onEndCount.Load(), "同实例只应挂载一次")
}

// TestInitObservability_附加处理器panic不炸主链路 附加 processor panic 被兜底吞掉。
// Python: SafeSpanProcessor 每方法 try/except。
func TestInitObservability_附加处理器panic不炸主链路(t *testing.T) {
	resetObservabilityGlobals()
	defer resetObservabilityGlobals()

	cfg := &ObservabilityConfig{Enabled: true, ServiceName: "test", Exporter: "console", SampleRate: 1}
	err := InitObservability(cfg,
		WithSpanExporterOverride(newTestExporter()),
		WithAdditionalSpanProcessors(&panickingProcessor{}))
	require.NoError(t, err)
	defer ShutdownObservability()

	// OnEnd panic 被吞：span.End() 本身不 panic，主链路继续
	_, span := GetTracer("panic-test").Start(context.Background(), "test.span")
	assert.NotPanics(t, func() { span.End() })

	// provider 仍可用（后续 span 正常创建）
	tracer := GetTracer("panic-test")
	assert.NotNil(t, tracer)
	_, span2 := tracer.Start(context.Background(), "test.span2")
	span2.End()
}

// TestNewSafeProcessor_包装后接口完整 safeProcessor 保持 SpanProcessor 接口语义。
func TestNewSafeProcessor_包装后接口完整(t *testing.T) {
	var _ sdktrace.SpanProcessor = newSafeProcessor(&recordingProcessor{})
	var _ trace.TracerProvider = provider // 编译期哨兵：provider 类型未变
}
