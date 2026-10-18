package observability

import (
	"context"
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
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

	railCtx := newRailCtx(&agentinterfaces.TaskIterationInputs{Iteration: 3, IsFollowUp: true})

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

	railCtx := newRailCtx(&agentinterfaces.TaskIterationInputs{Iteration: 0})

	// Before 前置钩子
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

	railCtx := newRailCtx(&agentinterfaces.TaskIterationInputs{Iteration: 0})

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

// ──────────────────────────── extract 函数测试 ────────────────────────────

// TestExtractIteration 测试从各类 EventInputs 提取 iteration。
// 对齐 Python getattr(inputs, "iteration", 0)：仅 TaskIterationInputs 携带该属性。
func TestExtractIteration(t *testing.T) {
	tests := []struct {
		name   string
		inputs agentinterfaces.EventInputs
		want   int
	}{
		{
			name:   "TaskIterationInputs 携带迭代号",
			inputs: &agentinterfaces.TaskIterationInputs{Iteration: 7},
			want:   7,
		},
		{
			name:   "TaskIterationInputs 零值",
			inputs: &agentinterfaces.TaskIterationInputs{Iteration: 0},
			want:   0,
		},
		{
			name:   "MapInputs 返回默认值（Python getattr 对 dict 同样返回默认值）",
			inputs: agentinterfaces.NewMapInputs(),
			want:   0,
		},
		{
			name:   "InvokeInputs 返回默认值",
			inputs: &agentinterfaces.InvokeInputs{},
			want:   0,
		},
		{
			name:   "nil 返回默认值",
			inputs: nil,
			want:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractIteration(tt.inputs); got != tt.want {
				t.Errorf("extractIteration() = %v, 期望 %v", got, tt.want)
			}
		})
	}
}

// TestExtractIsFollowUp 测试从各类 EventInputs 提取 is_follow_up。
func TestExtractIsFollowUp(t *testing.T) {
	tests := []struct {
		name   string
		inputs agentinterfaces.EventInputs
		want   bool
	}{
		{
			name:   "TaskIterationInputs 为 true",
			inputs: &agentinterfaces.TaskIterationInputs{IsFollowUp: true},
			want:   true,
		},
		{
			name:   "TaskIterationInputs 为 false",
			inputs: &agentinterfaces.TaskIterationInputs{IsFollowUp: false},
			want:   false,
		},
		{
			name:   "MapInputs 返回默认值",
			inputs: agentinterfaces.NewMapInputs(),
			want:   false,
		},
		{
			name:   "nil 返回默认值",
			inputs: nil,
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractIsFollowUp(tt.inputs); got != tt.want {
				t.Errorf("extractIsFollowUp() = %v, 期望 %v", got, tt.want)
			}
		})
	}
}

// TestObservabilityRail_BeforeTaskIteration_span属性写入 验证 iteration/is_follow_up
// 真实写入 span 属性（回归测试：原匿名接口断言永不成功导致恒为 0/false）。
func TestObservabilityRail_BeforeTaskIteration_span属性写入(t *testing.T) {
	rail, tp, recorder := newTestRail()
	defer tp.Shutdown(context.Background())

	railCtx := newRailCtx(&agentinterfaces.TaskIterationInputs{Iteration: 5, IsFollowUp: true})

	err := rail.BeforeTaskIteration(context.Background(), railCtx)
	require.NoError(t, err)

	spans := recorder.Ended()
	// Before 阶段 span 尚未结束，Ended() 为空属预期；
	// 通过 AfterTaskIteration 关闭后验证属性
	require.Empty(t, spans)

	err = rail.AfterTaskIteration(context.Background(), railCtx)
	require.NoError(t, err)

	spans = recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "deepagent.task_iteration.5", spans[0].Name())

	// 从属性切片中查找目标键（Attributes 返回 []attribute.KeyValue）
	findAttr := func(key attribute.Key) (attribute.Value, bool) {
		for _, kv := range spans[0].Attributes() {
			if kv.Key == key {
				return kv.Value, true
			}
		}
		return attribute.Value{}, false
	}

	// 验证 iteration 属性值
	iterVal, ok := findAttr(attribute.Key(DATaskIteration))
	require.True(t, ok, "span 缺少 iteration 属性")
	assert.Equal(t, int64(5), iterVal.AsInt64())

	// 验证 is_follow_up 属性值
	followUpVal, ok := findAttr(attribute.Key(DATaskIsFollowUp))
	require.True(t, ok, "span 缺少 is_follow_up 属性")
	assert.Equal(t, true, followUpVal.AsBool())
}
