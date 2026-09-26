package observability

import (
	"context"
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

func newTestRail() (*ObservabilityRail, *sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := tp.Tracer(railTracerName)
	rail := NewObservabilityRail(tracer)
	return rail, tp, recorder
}

// mockInputs 测试用 EventInputs 实现
type mockInputs struct {
	iteration  int
	isFollowUp bool
}

func (m *mockInputs) EventKind() string { return "task_iteration" }
func (m *mockInputs) GetIteration() int  { return m.iteration }
func (m *mockInputs) GetIsFollowUp() bool { return m.isFollowUp }

// newRailCtx 创建带初始化 extra 的 AgentCallbackContext
func newRailCtx(inputs agentinterfaces.EventInputs) *agentinterfaces.AgentCallbackContext {
	ctx := &agentinterfaces.AgentCallbackContext{}
	if inputs != nil {
		ctx.SetInputs(inputs)
	}
	// 使用反射初始化 unexported extra 字段
	v := reflect.ValueOf(ctx).Elem()
	f := v.FieldByName("extra")
	if f.IsNil() {
		// 通过 unsafe 设置 unexported 字段
		newExtra := make(map[string]any)
		fptr := reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr()))
		fptr.Elem().Set(reflect.ValueOf(newExtra))
	}
	return ctx
}

func TestObservabilityRail_BeforeTaskIteration_存储span(t *testing.T) {
	rail, tp, _ := newTestRail()
	defer tp.Shutdown(context.Background())

	railCtx := newRailCtx(&mockInputs{iteration: 3, isFollowUp: true})

	err := rail.BeforeTaskIteration(context.Background(), railCtx)
	require.NoError(t, err)

	// Extra 中应存储了 span
	spanVal, ok := railCtx.Extra()[spanKey]
	require.True(t, ok)
	_, ok = spanVal.(trace.Span)
	assert.True(t, ok)
}

func TestObservabilityRail_AfterTaskIteration_关闭span(t *testing.T) {
	rail, tp, recorder := newTestRail()
	defer tp.Shutdown(context.Background())

	railCtx := newRailCtx(&mockInputs{iteration: 0})

	// Before
	err := rail.BeforeTaskIteration(context.Background(), railCtx)
	require.NoError(t, err)

	// After（无异常）
	err = rail.AfterTaskIteration(context.Background(), railCtx)
	require.NoError(t, err)

	// Extra 中 span 应被移除
	_, ok := railCtx.Extra()[spanKey]
	assert.False(t, ok)

	// 应有已结束的 span
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Contains(t, spans[0].Name(), "deepagent.task_iteration")
}

func TestObservabilityRail_AfterTaskIteration_异常时设ERROR(t *testing.T) {
	rail, tp, recorder := newTestRail()
	defer tp.Shutdown(context.Background())

	railCtx := newRailCtx(&mockInputs{iteration: 0})

	err := rail.BeforeTaskIteration(context.Background(), railCtx)
	require.NoError(t, err)

	// 设置异常
	railCtx.SetException(assert.AnError)

	err = rail.AfterTaskIteration(context.Background(), railCtx)
	require.NoError(t, err)

	// 应有已结束的 span
	spans := recorder.Ended()
	require.Len(t, spans, 1)
}

func TestObservabilityRail_AfterTaskIteration_无span时不报错(t *testing.T) {
	rail, tp, _ := newTestRail()
	defer tp.Shutdown(context.Background())

	railCtx := newRailCtx(nil)

	// 不调用 Before，直接 After
	err := rail.AfterTaskIteration(context.Background(), railCtx)
	require.NoError(t, err)
}
