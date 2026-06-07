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
func NewLifecycleTool(inner Tool, fw *runnnercallback.CallbackFramework) *LifecycleTool {
	return &LifecycleTool{
		inner: inner,
		fw:    fw,
	}
}

// Card 委托给内部 Tool。
func (t *LifecycleTool) Card() *ToolCard {
	return t.inner.Card()
}

// Invoke 包装生命周期：STARTED → INVOKE_INPUT → [执行] → INVOKE_OUTPUT → FINISHED / ERROR
//
// 对应 Python: _ToolMeta 中对 invoke 的 _lifecycle_invoke 包装 + IO 转换钩子
func (t *LifecycleTool) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	card := t.inner.Card()

	// 1. 触发 TOOL_CALL_STARTED
	_ = t.fw.TriggerTool(ctx, newStartedData(card, inputs))

	// 2. 触发 TOOL_INVOKE_INPUT（emit_before）
	_ = t.fw.TriggerTool(ctx, newInvokeInputData(card, inputs))

	// 3. 执行内部 Tool
	result, err := t.inner.Invoke(ctx, inputs, opts...)

	if err != nil {
		// 4. 触发 TOOL_CALL_ERROR
		_ = t.fw.TriggerTool(ctx, newErrorData(card, inputs, err))
		return nil, err
	}

	// 5. 触发 TOOL_INVOKE_OUTPUT（emit_after）
	_ = t.fw.TriggerTool(ctx, newInvokeOutputData(card, result))

	// 6. 触发 TOOL_CALL_FINISHED
	_ = t.fw.TriggerTool(ctx, newFinishedData(card, inputs, result))

	return result, nil
}

// Stream 包装生命周期：STARTED → STREAM_INPUT → [执行] → 逐 chunk RESULT_RECEIVED → FINISHED / ERROR
//
// 对应 Python: _ToolMeta 中对 stream 的 _lifecycle_stream 包装
func (t *LifecycleTool) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	card := t.inner.Card()

	// 1. 触发 TOOL_CALL_STARTED
	_ = t.fw.TriggerTool(ctx, newStartedData(card, inputs))

	// 2. 触发 TOOL_STREAM_INPUT（emit_before）
	_ = t.fw.TriggerTool(ctx, newStreamInputData(card, inputs))

	// 3. 执行内部 Tool
	innerCh, err := t.inner.Stream(ctx, inputs, opts...)
	if err != nil {
		// 出错时触发 TOOL_CALL_ERROR
		_ = t.fw.TriggerTool(ctx, newErrorData(card, inputs, err))
		return nil, err
	}

	// 4. 包装输出 channel，逐 chunk 触发 RESULT_RECEIVED
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
				_ = t.fw.TriggerTool(ctx, newStreamOutputData(card, nil))
				_ = t.fw.TriggerTool(ctx, newFinishedData(card, inputs, nil))
				outCh <- chunk
				return
			}
			// 正常数据块：触发 TOOL_RESULT_RECEIVED
			_ = t.fw.TriggerTool(ctx, newResultReceivedData(card, chunk.Data))
			// 触发 TOOL_STREAM_OUTPUT
			_ = t.fw.TriggerTool(ctx, newStreamOutputData(card, chunk.Data))
			outCh <- chunk
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
