package observability

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cbschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	intellirouter "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients/intellirouter"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	"go.opentelemetry.io/otel/attribute"
)

// ──────────────────────────── OnLLMInput ────────────────────────────

// TestOnLLMInput_提交上下文窗口 LLM_INPUT 为 provider 归一后的请求提交一次
// context.window.commit，并置位 committed 标记；重复事件不重复提交。
// Python: on_llm_input（callback_handler.py:529-564）。
func TestOnLLMInput_提交上下文窗口(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	handler.OnLLMInvokeInput(ctx, &cb.LLMCallEventData{ModelName: "m1"})
	handler.OnLLMInput(ctx, &cb.LLMCallEventData{
		Messages: []map[string]any{
			{"role": "user", "content": "hello"},
		},
	})

	state := spanState.PopLlmSpanState(true)
	require.NotNil(t, state)
	assert.True(t, state.ContextWindowCommitted)

	state.Span.End()
	spans := recorder.Ended()
	// llm.call + context.window.commit
	var commitFound bool
	for _, s := range spans {
		if s.Name() == "context.window.commit" {
			commitFound = true
		}
	}
	assert.True(t, commitFound, "LLM_INPUT 应发出 context.window.commit")

	// 重复事件：committed 已置位，不产生第二个 commit
	count := len(recorder.Ended())
	handler.OnLLMInput(ctx, &cb.LLMCallEventData{
		Messages: []map[string]any{{"role": "user", "content": "hello"}},
	})
	assert.Len(t, recorder.Ended(), count)
}

// TestOnLLMInput_无openSpan时幂等 无打开的 LLM span 时事件不 panic、不产生提交。
func TestOnLLMInput_无openSpan时幂等(t *testing.T) {
	handler, tp, _ := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	assert.NotPanics(t, func() {
		handler.OnLLMInput(ctx, &cb.LLMCallEventData{
			Messages: []map[string]any{{"role": "user", "content": "hi"}},
		})
	})
}

// ──────────────────────────── OnLLMResponseStarted ────────────────────────────

// TestOnLLMResponseStarted_首字节时延与重试计数 记录 TTBF 与 retry_count，且只记一次。
// Python: on_llm_response_started（callback_handler.py:608-624）。
func TestOnLLMResponseStarted_首字节时延与重试计数(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	retry := 2
	handler.OnLLMInvokeInput(ctx, &cb.LLMCallEventData{ModelName: "m1"})
	handler.OnLLMResponseStarted(ctx, &cb.LLMCallEventData{RetryCount: &retry})
	handler.OnLLMResponseStarted(ctx, &cb.LLMCallEventData{RetryCount: &retry}) // 幂等

	state := spanState.PopLlmSpanState(true)
	require.NotNil(t, state)
	assert.NotZero(t, state.ResponseStartedNs)
	state.Span.End()

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	attrs := attrMap(spans[0].Attributes())
	assert.Contains(t, attrs, OJGenAIResponseTimeToFirstByteMs)
	assert.Equal(t, int64(2), attrs[OJRequestRetryCount].AsInt64())
}

// ──────────────────────────── OnLLMRoute ────────────────────────────

// TestOnLLMRoute_改名并回填占位符 路由事件把 span 改名为实际模型并回填
// 记录输入中的占位符。
// Python: on_llm_route + _fill_routed_model_in_input。
func TestOnLLMRoute_改名并回填占位符(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	handler.OnLLMInvokeInput(ctx, &cb.LLMCallEventData{
		ModelName: intellirouter.RoutedModelPlaceholder,
		Messages: []map[string]any{
			{"role": "user", "content": "say " + intellirouter.RoutedModelPlaceholder + " hi"},
		},
	})
	handler.OnLLMRoute(ctx, map[string]any{
		"route_metadata": map[string]any{
			"model_group_id": "group-1",
			"model_name":     "qwen-max",
			"route_id":       "route-9",
			"routed_model":   "qwen2.5-72b",
		},
	})

	state := spanState.PopLlmSpanState(true)
	require.NotNil(t, state)
	state.Span.End()

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	attrs := attrMap(spans[0].Attributes())
	assert.Equal(t, "chat qwen-max", spans[0].Name())
	assert.Equal(t, "group-1", attrs[OJModelGroupID].AsString())
	assert.Equal(t, "route-9", attrs[OJModelGroupRouteID].AsString())
	inputMessages := attrs[GenAIInputMessages].AsString()
	assert.NotEmpty(t, inputMessages)
	assert.False(t, strings.Contains(inputMessages, intellirouter.RoutedModelPlaceholder),
		"回填后输入消息不应残留占位符")
	assert.True(t, strings.Contains(inputMessages, "qwen2.5-72b"))
}

// TestOnLLMRoute_无routeMetadata时幂等 缺 route_metadata 或模型名为空时不动作。
func TestOnLLMRoute_无routeMetadata时幂等(t *testing.T) {
	handler, tp, _ := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	handler.OnLLMInvokeInput(ctx, &cb.LLMCallEventData{ModelName: "m1"})
	assert.NotPanics(t, func() {
		handler.OnLLMRoute(ctx, map[string]any{})
		handler.OnLLMRoute(ctx, map[string]any{
			"route_metadata": map[string]any{"model_group_id": "g"},
		})
	})
	spanState.PopLlmSpanState(true).Span.End()
}

// ──────────────────────────── OnLLMStreamCompleted ────────────────────────────

// TestOnLLMStreamCompleted_关闭span 流式自然耗尽后关闭 span。
// Python: on_llm_stream_completed。
func TestOnLLMStreamCompleted_关闭span(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	handler.OnLLMStreamInput(ctx, &cb.LLMCallEventData{ModelName: "m2", IsStream: true})
	result := handler.OnLLMStreamCompleted(ctx, &cb.LLMCallEventData{
		Response: llmschema.NewAssistantMessage("done"),
	})
	assert.NotNil(t, result)
	// 栈应已弹空
	assert.Nil(t, spanState.PopLlmSpanState(true))
	// llm.call + completion 相关子 span 落 recorder
	require.NotEmpty(t, recorder.Ended())
}

// ──────────────────────────── OnLLMOutput ────────────────────────────

// TestOnLLMOutput_统一收尾期只增强 经 call scope（unified_completion）时
// LLM_OUTPUT 只增强不关闭。
// Python: on_llm_output 的 expects_unified_llm_completion 分支。
func TestOnLLMOutput_统一收尾期只增强(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	scopeCtx, done := llm.EnterCallScope(context.Background(), true)
	defer done()
	ctx := WithSpanState(scopeCtx, spanState)

	handler.OnLLMInvokeInput(ctx, &cb.LLMCallEventData{ModelName: "m3"})
	handler.OnLLMOutput(ctx, &cb.LLMCallEventData{Response: llmschema.NewAssistantMessage("partial")})
	// span 仍开着（统一收尾由 LLM_STREAM_COMPLETED/INVOKE_OUTPUT 负责）
	state := spanState.PopLlmSpanState(true)
	require.NotNil(t, state)
	assert.True(t, state.Span.IsRecording())
	state.Span.End()
	_ = recorder
}

// TestOnLLMOutput_旧式路径视为终态 无统一收尾期望时 LLM_OUTPUT 关闭 span。
func TestOnLLMOutput_旧式路径视为终态(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	handler.OnLLMInvokeInput(ctx, &cb.LLMCallEventData{ModelName: "m4"})
	handler.OnLLMOutput(ctx, &cb.LLMCallEventData{Response: llmschema.NewAssistantMessage("final")})
	// 栈已弹空
	assert.Nil(t, spanState.PopLlmSpanState(true))
	require.NotEmpty(t, recorder.Ended())
}

// ──────────────────────────── OnAgentStreamInput/Output ────────────────────────────

// TestOnAgentStream_委托invoke路径 stream 输入/输出与 invoke 路径同逻辑。
// Python: on_agent_stream_input/output。
func TestOnAgentStream_委托invoke路径(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	handler.OnAgentStreamInput(ctx, &cb.GlobalAgentEventData{AgentID: "a1"})
	handler.OnAgentStreamOutput(ctx, &cb.GlobalAgentEventData{AgentID: "a1"})
	assert.NotEmpty(t, recorder.Ended())
}

// ──────────────────────────── 压缩观测桥 ────────────────────────────

// TestCompressionBridge_非压缩请求早退 非压缩用途或缺 operation_id 时早退。
// Python: on_llm_request_input 的守卫分支。
func TestCompressionBridge_非压缩请求早退(t *testing.T) {
	_, tp, _ := newTestHandler()
	defer tp.Shutdown(context.Background())

	bridge := NewContextCompressionObservabilityBridge(nil, nil)
	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	// request_purpose 非 compaction：不盖戳、不保留
	bridge.OnLLMRequestInput(ctx, &cb.LLMCallEventData{
		Extra: map[string]any{"context_operation_id": "op-1", "request_purpose": "assistant"},
	})
	bridge.expectNoRequestRef(t, "op-1")

	// 缺 operation_id：同样早退
	bridge.OnLLMRequestInput(ctx, &cb.LLMCallEventData{
		Extra: map[string]any{"request_purpose": "compaction"},
	})
	bridge.expectNoRequestRef(t, "op-1")
}

// TestCompressionBridge_状态事件类型守卫 State 非 ContextCompressionState 时早退；
// 非终态不消费桶、非 completed 不发事件。
func TestCompressionBridge_状态事件类型守卫(t *testing.T) {
	_, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	bridge := NewContextCompressionObservabilityBridge(nil, nil)

	assert.NotPanics(t, func() {
		bridge.OnContextCompressionState(context.Background(), &cb.ContextCallEventData{
			State: "not-a-state",
		})
		bridge.OnContextCompressionState(context.Background(), &cb.ContextCallEventData{
			State: &cbschema.ContextCompressionState{OperationID: "op-9", Status: cbschema.CompressionStarted},
		})
		bridge.OnContextCompressionState(context.Background(), &cb.ContextCallEventData{
			State: &cbschema.ContextCompressionState{OperationID: "op-9", Status: cbschema.CompressionNoop},
		})
	})
	assert.Empty(t, recorder.Ended())
}

// TestContextCompactionNumber_序号稳定 同一操作重复取号得到同一序号，
// 新操作递增；无法命名时返回 0。
// Python: context_compaction_number（span_context.py:568-602）。
func TestContextCompactionNumber_序号稳定(t *testing.T) {
	n1 := ContextCompactionNumber("sess", "main", "op-a")
	n1Again := ContextCompactionNumber("sess", "main", "op-a")
	n2 := ContextCompactionNumber("sess", "main", "op-b")
	assert.Equal(t, 1, n1)
	assert.Equal(t, n1, n1Again)
	assert.Equal(t, 2, n2)
	assert.Equal(t, 0, ContextCompactionNumber("", "main", "op-c"))
	assert.Equal(t, 0, ContextCompactionNumber("sess", "", "op-c"))
	assert.Equal(t, 0, ContextCompactionNumber("sess", "main", ""))
}

// ──────────────────────────── 测试助手 ────────────────────────────

// attrMap 把属性切片转为 map 便于断言。
func attrMap(attrs []attribute.KeyValue) map[string]attribute.Value {
	result := make(map[string]attribute.Value, len(attrs))
	for _, kv := range attrs {
		result[string(kv.Key)] = kv.Value
	}
	return result
}

// expectNoRequestRef 断言操作桶中未保留请求引用。
func (b *ContextCompressionObservabilityBridge) expectNoRequestRef(t *testing.T, op string) {
	t.Helper()
	b.modelRequestsLock.Lock()
	defer b.modelRequestsLock.Unlock()
	_, ok := b.modelRequestsByOperation[op]
	assert.False(t, ok)
}
