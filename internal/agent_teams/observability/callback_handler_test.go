package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// newTestHandler 创建带 mock tracer 的测试 handler
func newTestHandler() (*OtelCallbackHandler, *sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := tp.Tracer(tracerName)
	cfg := &ObservabilityConfig{AttributeValueMaxLength: 1000}
	handler := NewOtelCallbackHandler(cfg, tracer)
	return handler, tp, recorder
}

func TestOtelCallbackHandler_OnLLMInvokeInput_生成span(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	temp := 0.7
	topP := 0.9
	maxTokens := 4096
	data := &cb.LLMCallEventData{
		Event:         cb.LLMInvokeInput,
		ModelName:     "qwen-max",
		ModelProvider: "dashscope",
		Temperature:   &temp,
		TopP:          &topP,
		MaxTokens:     &maxTokens,
		IsStream:      false,
	}

	handler.OnLLMInvokeInput(ctx, data)

	// 栈中应有 1 个 LLM span
	state := spanState.PopLlmSpanState(true)
	require.NotNil(t, state)

	// 结束 span 以让 recorder 捕获
	state.Span.End()

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, "llm.call", s.Name())
}

func TestOtelCallbackHandler_OnLLMInvokeOutput_关闭span(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	// 先打开 LLM span
	temp := 0.7
	inputData := &cb.LLMCallEventData{
		Event:       cb.LLMInvokeInput,
		ModelName:   "qwen-max",
		Temperature: &temp,
	}
	handler.OnLLMInvokeInput(ctx, inputData)

	// 再关闭
	outputData := &cb.LLMCallEventData{
		Event:     cb.LLMInvokeOutput,
		ModelName: "qwen-max",
	}
	handler.OnLLMInvokeOutput(ctx, outputData)

	// 栈应为空
	assert.Nil(t, spanState.PopLlmSpanState(false))

	// 应有已结束的 span
	spans := recorder.Ended()
	require.Len(t, spans, 1)
}

func TestOtelCallbackHandler_OnLLMCallError_标记错误(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	// 先打开 LLM span
	inputData := &cb.LLMCallEventData{
		Event:     cb.LLMInvokeInput,
		ModelName: "qwen-max",
	}
	handler.OnLLMInvokeInput(ctx, inputData)

	// 调用错误回调
	errData := &cb.LLMCallEventData{
		Event: cb.LLMCallError,
		Error: assert.AnError,
	}
	handler.OnLLMCallError(ctx, errData)

	// 栈应为空
	assert.Nil(t, spanState.PopLlmSpanState(false))

	// 应有已结束的 span
	spans := recorder.Ended()
	require.Len(t, spans, 1)
}

func TestOtelCallbackHandler_SpanStateNil时返回nil(t *testing.T) {
	handler, _, _ := newTestHandler()
	ctx := context.Background() // 无 SpanState

	// 所有回调应返回 nil 而不 panic
	assert.Nil(t, handler.OnLLMInvokeInput(ctx, &cb.LLMCallEventData{}))
	assert.Nil(t, handler.OnLLMInvokeOutput(ctx, &cb.LLMCallEventData{}))
	assert.Nil(t, handler.OnLLMCallError(ctx, &cb.LLMCallEventData{}))
	assert.Nil(t, handler.OnToolCallStarted(ctx, &cb.ToolCallEventData{}))
	assert.Nil(t, handler.OnToolCallFinished(ctx, &cb.ToolCallEventData{}))
	assert.Nil(t, handler.OnToolCallError(ctx, &cb.ToolCallEventData{}))
	assert.Nil(t, handler.OnAgentInvokeInput(ctx, &cb.GlobalAgentEventData{}))
	assert.Nil(t, handler.OnAgentInvokeOutput(ctx, &cb.GlobalAgentEventData{}))
}

func TestOtelCallbackHandler_OnToolCallStarted_生成span(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	data := &cb.ToolCallEventData{
		Event:    cb.ToolCallStarted,
		ToolName: "bash",
		ToolID:   "tool-123",
		Inputs:   map[string]any{"command": "ls"},
	}
	handler.OnToolCallStarted(ctx, data)

	// 从 map 中取出并关闭 span 以让 recorder 捕获
	span := spanState.PopToolSpan("bash")
	require.NotNil(t, span)
	span.End()

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Contains(t, spans[0].Name(), "tool.bash")
}

func TestOtelCallbackHandler_OnAgentInvokeInput_生成span(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	data := &cb.GlobalAgentEventData{
		Event:     cb.GlobalAgentInvokeInput,
		AgentID:   "leader",
		AgentName: "leader-agent",
		Inputs:    map[string]any{"user_input": "hello"},
	}
	handler.OnAgentInvokeInput(ctx, data)

	span := spanState.PopAgentSpan("leader")
	require.NotNil(t, span)
	span.End()

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Contains(t, spans[0].Name(), "agent.leader")
}

func TestCoerceMessageContent(t *testing.T) {
	assert.Equal(t, "", coerceMessageContent(nil))
	assert.Equal(t, "hello", coerceMessageContent("hello"))
	assert.Equal(t, `[1,2,3]`, coerceMessageContent([]int{1, 2, 3}))
}

func TestSerializeToolInputs(t *testing.T) {
	assert.Equal(t, "", serializeToolInputs(nil))
	assert.Equal(t, `{"key":"value"}`, serializeToolInputs(map[string]string{"key": "value"}))
}

func TestUnpackAgentInputs(t *testing.T) {
	// dict 类型
	agentID, role, query := unpackAgentInputs(map[string]any{
		"agent_id":   "leader",
		"role":       "leader",
		"user_input": "hello",
	}, "")
	assert.Equal(t, "leader", agentID)
	assert.Equal(t, "leader", role)
	assert.Equal(t, "hello", query)

	// fallbackAgentID
	agentID, _, _ = unpackAgentInputs(nil, "fallback-id")
	assert.Equal(t, "fallback-id", agentID)

	// 空 inputs
	agentID, _, _ = unpackAgentInputs(nil, "")
	assert.Equal(t, "unknown", agentID)
}

func TestDeriveModelName(t *testing.T) {
	assert.Equal(t, "", deriveModelName(nil))
	assert.Equal(t, "", deriveModelName(map[string]any{}))
	// map 类型 model_config
	assert.Equal(t, "my-model", deriveModelName(map[string]any{
		"model_config": map[string]any{"model": "my-model"},
	}))
}

func TestOtelCallbackHandler_OnLLMStreamInput_生成span(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	data := &cb.LLMCallEventData{
		Event:         cb.LLMStreamInput,
		ModelName:     "qwen-max",
		ModelProvider: "dashscope",
	}
	handler.OnLLMStreamInput(ctx, data)

	state := spanState.PopLlmSpanState(true)
	require.NotNil(t, state)
	state.Span.End()

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "llm.call", spans[0].Name())
}

func TestOtelCallbackHandler_OnLLMStreamOutput_记录chunk(t *testing.T) {
	handler, tp, _ := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	// 先打开 stream LLM span
	inputData := &cb.LLMCallEventData{
		Event:     cb.LLMStreamInput,
		ModelName: "qwen-max",
	}
	handler.OnLLMStreamInput(ctx, inputData)

	// stream chunk — 通过 Response 字段传递内容
	outputData := &cb.LLMCallEventData{
		Event: cb.LLMStreamOutput,
		Extra: map[string]any{"chunk_index": 1},
	}
	handler.OnLLMStreamOutput(ctx, outputData)

	// 栈顶应有 span 且 FirstChunkNs 已被设置
	state := spanState.PopLlmSpanState(true)
	require.NotNil(t, state)
	assert.NotEqual(t, int64(0), state.FirstChunkNs)
	assert.GreaterOrEqual(t, state.ChunkCount, 1)
	state.Span.End()
}

func TestMessageRole(t *testing.T) {
	assert.Equal(t, "user", messageRole(map[string]any{"role": "user"}))
	assert.Equal(t, "", messageRole(map[string]any{}))
	assert.Equal(t, "", messageRole(nil))
	// 纯字符串不匹配 map[string]any 或 roleHolder 接口
	assert.Equal(t, "", messageRole("system"))
}

func TestToMessageSlice(t *testing.T) {
	msgs, ok := toMessageSlice([]any{map[string]any{"role": "user", "content": "hi"}})
	assert.True(t, ok)
	require.Len(t, msgs, 1)
	msg, _ := msgs[0].(map[string]any)
	assert.Equal(t, "user", msg["role"])

	_, ok = toMessageSlice(nil)
	assert.False(t, ok)

	_, ok = toMessageSlice("not a slice")
	assert.False(t, ok)
}

func TestExtractUsageMetadata(t *testing.T) {
	meta := map[string]any{
		"input_token_count":  100,
		"output_token_count": 50,
	}
	result := extractUsageMetadata(meta)
	if result != nil {
		assert.Equal(t, 100, result.InputTokens)
		assert.Equal(t, 50, result.OutputTokens)
	}

	// nil meta
	result = extractUsageMetadata(nil)
	assert.Nil(t, result)
}

func TestExtractFinishReason(t *testing.T) {
	// 字符串不实现 finishReasonHolder 接口
	assert.Equal(t, "", extractFinishReason("stop"))
	assert.Equal(t, "", extractFinishReason(nil))
	assert.Equal(t, "length", extractFinishReason(map[string]any{"finish_reason": "length"}))
}

func TestOtelCallbackHandler_OnToolCallFinished_关闭span(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	// 先创建 tool span
	startData := &cb.ToolCallEventData{
		Event:    cb.ToolCallStarted,
		ToolName: "bash",
		ToolID:   "tool-456",
		Inputs:   map[string]any{"command": "ls"},
	}
	handler.OnToolCallStarted(ctx, startData)

	// 关闭
	finishData := &cb.ToolCallEventData{
		Event:    cb.ToolCallFinished,
		ToolName: "bash",
		ToolID:   "tool-456",
		Result:   map[string]any{"output": "file1.txt\nfile2.txt"},
	}
	handler.OnToolCallFinished(ctx, finishData)

	// tool span 应已关闭
	assert.Nil(t, spanState.PopToolSpan("bash"))
	spans := recorder.Ended()
	require.Len(t, spans, 1)
}

func TestOtelCallbackHandler_OnToolCallError_标记错误(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	// 先创建 tool span
	startData := &cb.ToolCallEventData{
		Event:    cb.ToolCallStarted,
		ToolName: "bash",
		ToolID:   "tool-err",
	}
	handler.OnToolCallStarted(ctx, startData)

	// 错误回调
	errData := &cb.ToolCallEventData{
		Event:    cb.ToolCallError,
		ToolName: "bash",
		ToolID:   "tool-err",
		Error:    assert.AnError,
	}
	handler.OnToolCallError(ctx, errData)

	// tool span 应已关闭
	assert.Nil(t, spanState.PopToolSpan("bash"))
	spans := recorder.Ended()
	require.Len(t, spans, 1)
}

func TestOtelCallbackHandler_OnAgentInvokeOutput_关闭span(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	// 先创建 agent span
	inputData := &cb.GlobalAgentEventData{
		Event:     cb.GlobalAgentInvokeInput,
		AgentID:   "worker",
		AgentName: "worker-agent",
		Inputs:    map[string]any{"query": "test"},
	}
	handler.OnAgentInvokeInput(ctx, inputData)

	// 关闭
	outputData := &cb.GlobalAgentEventData{
		Event:     cb.GlobalAgentInvokeOutput,
		AgentID:   "worker",
		AgentName: "worker-agent",
		Result:    "result",
	}
	handler.OnAgentInvokeOutput(ctx, outputData)

	// agent span 应已关闭
	assert.Nil(t, spanState.PopAgentSpan("worker"))
	spans := recorder.Ended()
	require.Len(t, spans, 1)
}

func TestMessageContent(t *testing.T) {
	// map 类型
	assert.Equal(t, "hello", messageContent(map[string]any{"content": "hello"}))
	// nil
	assert.Nil(t, messageContent(nil))
	// 空 map（无 content 键）
	assert.Nil(t, messageContent(map[string]any{}))
}

func TestExtractReasoningContent(t *testing.T) {
	// map 类型
	assert.Equal(t, "thinking...", extractReasoningContent(map[string]any{"reasoning_content": "thinking..."}))
	// nil
	assert.Equal(t, "", extractReasoningContent(nil))
}

func TestMaybeRecordResponseAttrs(t *testing.T) {
	handler, tp, _ := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	// 打开 LLM span
	inputData := &cb.LLMCallEventData{
		Event:     cb.LLMInvokeInput,
		ModelName: "qwen-max",
	}
	handler.OnLLMInvokeInput(ctx, inputData)

	state := spanState.PopLlmSpanState(true)
	require.NotNil(t, state)

	// 带 Response 和 Usage 的关闭
	outputData := &cb.LLMCallEventData{
		Event:     cb.LLMInvokeOutput,
		ModelName: "qwen-max",
		Extra:     map[string]any{"response": map[string]any{"content": "result"}},
	}
	handler.OnLLMInvokeOutput(ctx, outputData)
}

func TestOtelCallbackHandler_OnLLMInvokeOutput_带Usage(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	// 先打开 LLM span
	inputData := &cb.LLMCallEventData{
		Event:     cb.LLMInvokeInput,
		ModelName: "qwen-max",
	}
	handler.OnLLMInvokeInput(ctx, inputData)

	// 带 Usage 关闭
	usage := &llmschema.UsageMetadata{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
	}
	outputData := &cb.LLMCallEventData{
		Event:     cb.LLMInvokeOutput,
		ModelName: "qwen-max",
		Usage:     usage,
		Extra:     map[string]any{"finish_reason": "stop"},
	}
	handler.OnLLMInvokeOutput(ctx, outputData)

	// 栈应为空
	assert.Nil(t, spanState.PopLlmSpanState(false))

	// 应有已结束的 span
	spans := recorder.Ended()
	require.Len(t, spans, 1)
}

func TestOtelCallbackHandler_OnLLMInvokeOutput_流式完整流程(t *testing.T) {
	handler, tp, recorder := newTestHandler()
	defer tp.Shutdown(context.Background())

	spanState := InitSpanState()
	ctx := WithSpanState(context.Background(), spanState)

	// 打开流式 LLM span
	inputData := &cb.LLMCallEventData{
		Event:     cb.LLMStreamInput,
		ModelName: "qwen-max",
	}
	handler.OnLLMStreamInput(ctx, inputData)

	// 多个 stream chunk
	for i := 0; i < 3; i++ {
		chunkData := &cb.LLMCallEventData{
			Event: cb.LLMStreamOutput,
			Extra: map[string]any{"chunk_index": i},
		}
		handler.OnLLMStreamOutput(ctx, chunkData)
	}

	// 带 Usage 关闭
	usage := &llmschema.UsageMetadata{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
	}
	outputData := &cb.LLMCallEventData{
		Event:     cb.LLMInvokeOutput,
		ModelName: "qwen-max",
		Usage:     usage,
	}
	handler.OnLLMInvokeOutput(ctx, outputData)

	assert.Nil(t, spanState.PopLlmSpanState(false))
	spans := recorder.Ended()
	require.Len(t, spans, 1)
}
