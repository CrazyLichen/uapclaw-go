package tool

import (
	"context"

	runnnercallback "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LifecycleTool 包装 Tool，在 Invoke/Stream 调用前后自动触发回调事件。
//
// LifecycleTool 实现了 Tool 接口，可以像普通 Tool 一样使用。
// 注册到 AbilityManager 时自动包装，对调用方透明。
//
// 对应 Python: _ToolMeta.__call__ 中的生命周期注入逻辑
type LifecycleTool struct {
	inner Tool
	fw    *runnnercallback.CallbackFramework
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLifecycleTool 创建带生命周期回调的工具包装器。
//
// fw 参数可选：不传或传 nil 时自动使用全局回调框架 GetCallbackFramework()，
// 对齐 Python: _ToolMeta.__call__ 中通过 Runner.callback_framework 获取。
func NewLifecycleTool(inner Tool, fw ...*runnnercallback.CallbackFramework) *LifecycleTool {
	var f *runnnercallback.CallbackFramework
	if len(fw) > 0 && fw[0] != nil {
		f = fw[0]
	} else {
		f = runnnercallback.GetCallbackFramework()
	}
	return &LifecycleTool{
		inner: inner,
		fw:    f,
	}
}

// Card 委托给内部 Tool。
func (t *LifecycleTool) Card() *ToolCard {
	return t.inner.Card()
}

// Invoke 包装生命周期（对齐 Python _ToolMeta 两步装饰链顺序）：
//
//	emit_before(INVOKE_INPUT) → TransformIO(input) → STARTED → [执行] → FINISHED → TransformIO(output) → emit_after(INVOKE_OUTPUT)
//
// 异常时：emit_before(INVOKE_INPUT) → TransformIO(input) → STARTED → [执行] → ERROR
//
// 对应 Python: _lifecycle_invoke（内层 STARTED/FINISHED）+ 外层 emit_before/transform_io/emit_after
func (t *LifecycleTool) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	card := t.inner.Card()

	// 1. emit_before：触发 TOOL_INVOKE_INPUT
	_ = t.fw.TriggerTool(ctx, newInvokeInputData(card, inputs))

	// 2. TransformToolIOInput — 输入变换
	inputs = t.fw.TransformToolIOInput(ctx, runnnercallback.ToolInvokeInput, inputs)

	// 3. 触发 TOOL_CALL_STARTED
	_ = t.fw.TriggerTool(ctx, newStartedData(card, inputs))

	// 4. 执行内部 Tool
	result, err := t.inner.Invoke(ctx, inputs, opts...)

	if err != nil {
		// 5. 触发 TOOL_CALL_ERROR
		_ = t.fw.TriggerTool(ctx, newErrorData(card, inputs, err))
		return nil, err
	}

	// 6. 触发 TOOL_CALL_FINISHED
	_ = t.fw.TriggerTool(ctx, newFinishedData(card, inputs, result))

	// 7. TransformToolIOOutput — 输出变换
	result = t.fw.TransformToolIOOutput(ctx, runnnercallback.ToolInvokeOutput, result)

	// 8. emit_after：触发 TOOL_INVOKE_OUTPUT
	_ = t.fw.TriggerTool(ctx, newInvokeOutputData(card, result))

	return result, nil
}

// Stream 包装生命周期（对齐 Python _ToolMeta 两步装饰链顺序）：
//
//	emit_before(STREAM_INPUT) → TransformIO(input) → STARTED → [执行]
//	  → per-chunk: TransformIO(output) → RESULT_RECEIVED → STREAM_OUTPUT
//	  → Done: FINISHED → emit_after(STREAM_OUTPUT)
//
// 异常时：触发 TOOL_CALL_ERROR
//
// 对应 Python: _lifecycle_stream（内层 STARTED/FINISHED）+ 外层 emit_before/transform_io/emit_after
func (t *LifecycleTool) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	card := t.inner.Card()

	// 1. emit_before：触发 TOOL_STREAM_INPUT
	_ = t.fw.TriggerTool(ctx, newStreamInputData(card, inputs))

	// 2. TransformToolIOInput — 输入变换
	inputs = t.fw.TransformToolIOInput(ctx, runnnercallback.ToolStreamInput, inputs)

	// 3. 触发 TOOL_CALL_STARTED
	_ = t.fw.TriggerTool(ctx, newStartedData(card, inputs))

	// 4. 执行内部 Tool
	innerCh, err := t.inner.Stream(ctx, inputs, opts...)
	if err != nil {
		// 出错时触发 TOOL_CALL_ERROR
		_ = t.fw.TriggerTool(ctx, newErrorData(card, inputs, err))
		return nil, err
	}

	// 5. 包装输出 channel，逐 chunk 触发生命周期事件
	outCh := make(chan StreamChunk, 1)
	go func() {
		defer close(outCh)
		for chunk := range innerCh {
			if chunk.Error != nil {
				// 流出错
				_ = t.fw.TriggerTool(ctx, newErrorData(card, inputs, chunk.Error))
				outCh <- chunk
				return
			}
			if chunk.Done {
				// 流正常结束
				// 6. 触发 TOOL_CALL_FINISHED
				_ = t.fw.TriggerTool(ctx, newFinishedData(card, inputs, nil))
				// 注意：Python 中 emit_after(STREAM_OUTPUT) 是 per-item 模式，
				// 仅在每个数据 chunk 上触发，Done 时不触发 STREAM_OUTPUT
				outCh <- chunk
				return
			}
			// TransformToolIOOutput — per-chunk 输出变换
			transformedData := t.fw.TransformToolIOOutput(ctx, runnnercallback.ToolStreamOutput, chunk.Data)
			// 触发 TOOL_RESULT_RECEIVED
			_ = t.fw.TriggerTool(ctx, newResultReceivedData(card, transformedData))
			// 触发 TOOL_STREAM_OUTPUT
			_ = t.fw.TriggerTool(ctx, newStreamOutputData(card, transformedData))
			// 用变换后的数据构造新 chunk 发给下游
			outCh <- StreamChunk{Data: transformedData}
		}
	}()

	return outCh, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newStartedData 创建 TOOL_CALL_STARTED 事件数据
func newStartedData(card *ToolCard, inputs map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolCallStarted, &card.BaseCard)
	data.Inputs = inputs
	return data
}

// newFinishedData 创建 TOOL_CALL_FINISHED 事件数据
func newFinishedData(card *ToolCard, inputs map[string]any, result map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolCallFinished, &card.BaseCard)
	data.Inputs = inputs
	data.Result = result
	return data
}

// newErrorData 创建 TOOL_CALL_ERROR 事件数据
func newErrorData(card *ToolCard, inputs map[string]any, err error) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolCallError, &card.BaseCard)
	data.Inputs = inputs
	data.Error = err
	return data
}

// newResultReceivedData 创建 TOOL_RESULT_RECEIVED 事件数据
func newResultReceivedData(card *ToolCard, result map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolResultReceived, &card.BaseCard)
	data.Result = result
	return data
}

// newInvokeInputData 创建 TOOL_INVOKE_INPUT 事件数据
func newInvokeInputData(card *ToolCard, inputs map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolInvokeInput, &card.BaseCard)
	data.Inputs = inputs
	return data
}

// newInvokeOutputData 创建 TOOL_INVOKE_OUTPUT 事件数据
func newInvokeOutputData(card *ToolCard, result map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolInvokeOutput, &card.BaseCard)
	data.Result = result
	return data
}

// newStreamInputData 创建 TOOL_STREAM_INPUT 事件数据
func newStreamInputData(card *ToolCard, inputs map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolStreamInput, &card.BaseCard)
	data.Inputs = inputs
	return data
}

// newStreamOutputData 创建 TOOL_STREAM_OUTPUT 事件数据
func newStreamOutputData(card *ToolCard, result map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolStreamOutput, &card.BaseCard)
	data.Result = result
	return data
}
