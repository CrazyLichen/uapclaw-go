package observability

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// fakeSpanProcessor 测试桩：满足 sdktrace.SpanProcessor 的最小空实现。
type fakeSpanProcessor struct{}

func (f *fakeSpanProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {}
func (f *fakeSpanProcessor) OnEnd(s sdktrace.ReadOnlySpan)                            {}
func (f *fakeSpanProcessor) Shutdown(ctx context.Context) error                       { return nil }
func (f *fakeSpanProcessor) ForceFlush(ctx context.Context) error                     { return nil }

// TestGetTrajectorySpanProcessor_PutGet 对齐 demand.py:52-73——对外仅 Get
//（Python 无 setter），槽位幂等返回同实例。
func TestGetTrajectorySpanProcessor_PutGet(t *testing.T) {
	resetTrajectorySpanProcessorForTest()
	if GetTrajectorySpanProcessor() != nil {
		t.Fatal("未配置应返回 nil（懒构造 ⤴️evolving，D-15.16-c2②）")
	}
	p := &fakeSpanProcessor{}
	setTrajectorySpanProcessorForTest(p)
	if GetTrajectorySpanProcessor() != p {
		t.Fatal("应返回同一实例（进程级单槽）")
	}
}
