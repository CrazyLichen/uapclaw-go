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
//	Python 装饰器链（由内到外包装）：
//	  fn0 = _lifecycle_invoke    (内层: STARTED/FINISHED/ERROR)
//	  fn1 = emit_before(fn0)     (中层: 触发 INVOKE_INPUT)
//	  fn2 = transform_io(fn1)    (外层: 输入/输出变换)
//	  fn3 = emit_after(fn2)      (最外: 触发 INVOKE_OUTPUT)
//
//	执行时由外到内调用，实际顺序：
//	  TransformIO(input) → emit_before(INVOKE_INPUT) → STARTED → [执行] → FINISHED → TransformIO(output) → emit_after(INVOKE_OUTPUT)
//
// 异常时：TransformIO(input) → emit_before(INVOKE_INPUT) → STARTED → [执行] → ERROR
func (t *LifecycleTool) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	card := t.inner.Card()

	// 1. TransformToolIOInput — 输入变换（transform_io 最外层先执行）
	inputs = t.fw.TransformToolIOInput(ctx, runnnercallback.ToolInvokeInput, inputs)

	// 2. emit_before：触发 TOOL_INVOKE_INPUT（emit_before 中层，拿到变换后的参数）
	_ = t.fw.TriggerTool(ctx, newInvokeInputData(card, inputs))

	// 3. 触发 TOOL_CALL_STARTED（_lifecycle_invoke 内层）
	_ = t.fw.TriggerTool(ctx, newStartedData(card, inputs))

	// 4. 执行内部 Tool
	result, err := t.inner.Invoke(ctx, inputs, opts...)

	if err != nil {
		// 5. 触发 TOOL_CALL_ERROR
		_ = t.fw.TriggerTool(ctx, newErrorData(card, inputs, err))
		return nil, err
	}

	// 6. 触发 TOOL_CALL_FINISHED（_lifecycle_invoke 内层）
	_ = t.fw.TriggerTool(ctx, newFinishedData(card, inputs, result))

	// 7. TransformToolIOOutput — 输出变换（transform_io 外层）
	result = t.fw.TransformToolIOOutput(ctx, runnnercallback.ToolInvokeOutput, result)

	// 8. emit_after：触发 TOOL_INVOKE_OUTPUT（emit_after 最外层）
	_ = t.fw.TriggerTool(ctx, newInvokeOutputData(card, result))

	return result, nil
}

// Stream 包装生命周期（对齐 Python _ToolMeta 两步装饰链顺序）：
//
//	Python 装饰器链（由内到外包装）：
//	  fn0 = _lifecycle_stream     (内层: STARTED/RESULT_RECEIVED/FINISHED/ERROR)
//	  fn1 = emit_before(fn0)      (中层: 触发 STREAM_INPUT)
//	  fn2 = transform_io(fn1)     (外层: 输入/输出变换)
//	  fn3 = emit_after(fn2)       (最外: 触发 STREAM_OUTPUT, per-item)
//
//	执行时由外到内调用，实际顺序：
//	  TransformIO(input) → emit_before(STREAM_INPUT) → STARTED → [执行]
//	  → per-chunk: RESULT_RECEIVED(原始数据) → TransformIO(output) → STREAM_OUTPUT(变换后数据)
//	  → Done: FINISHED
//
//	RESULT_RECEIVED 在内层 _lifecycle_stream 触发，拿到原始 chunk（未变换）；
//	TransformIO/STREAM_OUTPUT 在外层处理，拿到变换后的数据。
//
// 异常时：触发 TOOL_CALL_ERROR
func (t *LifecycleTool) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	card := t.inner.Card()

	// 1. TransformToolIOInput — 输入变换（transform_io 最外层先执行）
	inputs = t.fw.TransformToolIOInput(ctx, runnnercallback.ToolStreamInput, inputs)

	// 2. emit_before：触发 TOOL_STREAM_INPUT（emit_before 中层，拿到变换后的参数）
	_ = t.fw.TriggerTool(ctx, newStreamInputData(card, inputs))

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
			// RESULT_RECEIVED：内层 _lifecycle_stream 触发，拿到原始数据（未变换）
			// 对齐 Python：_lifecycle_stream 中 async for chunk → trigger(RESULT_RECEIVED, chunk) → yield chunk
			_ = t.fw.TriggerTool(ctx, newResultReceivedData(card, chunk.Data))
			// TransformToolIOOutput — per-chunk 输出变换（transform_io 外层处理 yield 出来的 item）
			transformedData := t.fw.TransformToolIOOutput(ctx, runnnercallback.ToolStreamOutput, chunk.Data)
			// STREAM_OUTPUT：emit_after 最外层触发，拿到变换后的数据
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
	data.Extra = map[string]any{"tool_info": card.ToolInfo()}
	return data
}

// newFinishedData 创建 TOOL_CALL_FINISHED 事件数据
func newFinishedData(card *ToolCard, inputs map[string]any, result map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolCallFinished, &card.BaseCard)
	data.Inputs = inputs
	data.Result = result
	data.Extra = map[string]any{"tool_info": card.ToolInfo()}
	return data
}

// newErrorData 创建 TOOL_CALL_ERROR 事件数据
func newErrorData(card *ToolCard, inputs map[string]any, err error) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolCallError, &card.BaseCard)
	data.Inputs = inputs
	data.Error = err
	data.Extra = map[string]any{"tool_info": card.ToolInfo()}
	return data
}

// newResultReceivedData 创建 TOOL_RESULT_RECEIVED 事件数据
func newResultReceivedData(card *ToolCard, result map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolResultReceived, &card.BaseCard)
	data.Result = result
	data.Extra = map[string]any{"tool_info": card.ToolInfo()}
	return data
}

// newInvokeInputData 创建 TOOL_INVOKE_INPUT 事件数据
func newInvokeInputData(card *ToolCard, inputs map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolInvokeInput, &card.BaseCard)
	data.Inputs = inputs
	data.Extra = map[string]any{"tool_info": card.ToolInfo()}
	return data
}

// newInvokeOutputData 创建 TOOL_INVOKE_OUTPUT 事件数据
func newInvokeOutputData(card *ToolCard, result map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolInvokeOutput, &card.BaseCard)
	data.Result = result
	data.Extra = map[string]any{"tool_info": card.ToolInfo()}
	return data
}

// newStreamInputData 创建 TOOL_STREAM_INPUT 事件数据
func newStreamInputData(card *ToolCard, inputs map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolStreamInput, &card.BaseCard)
	data.Inputs = inputs
	data.Extra = map[string]any{"tool_info": card.ToolInfo()}
	return data
}

// newStreamOutputData 创建 TOOL_STREAM_OUTPUT 事件数据
func newStreamOutputData(card *ToolCard, result map[string]any) *runnnercallback.ToolCallEventData {
	data := runnnercallback.NewToolCallEventData(runnnercallback.ToolStreamOutput, &card.BaseCard)
	data.Result = result
	data.Extra = map[string]any{"tool_info": card.ToolInfo()}
	return data
}
