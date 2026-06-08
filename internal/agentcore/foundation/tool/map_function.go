package tool

import (
	"context"

	runnnercallback "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MapFunction 弱类型 map 函数工具，降级模式。
//
// 当函数参数无法用 struct 描述时（如动态参数），使用 MapFunction 代替 InvokeFunction/StreamFunction。
// 用户需手动提供 InputParams。
//
// 对应 Python: LocalFunction(func=None) 的降级场景
type MapFunction struct {
	card     *ToolCard
	invokeFn func(ctx context.Context, inputs map[string]any) (map[string]any, error)
	streamFn func(ctx context.Context, inputs map[string]any) (<-chan map[string]any, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMapFunction 创建弱类型 map 函数工具（降级模式）。
//
// invokeFn / streamFn 二选一，另一个传 nil。
// card.InputParams 由用户手动提供。
func NewMapFunction(card *ToolCard,
	invokeFn func(ctx context.Context, inputs map[string]any) (map[string]any, error),
	streamFn func(ctx context.Context, inputs map[string]any) (<-chan map[string]any, error),
) (*MapFunction, error) {
	if err := ValidateToolCard(card); err != nil {
		return nil, err
	}
	return &MapFunction{
		card:     card,
		invokeFn: invokeFn,
		streamFn: streamFn,
	}, nil
}

// Card 返回工具配置卡片。
func (f *MapFunction) Card() *ToolCard {
	return f.card
}

// Invoke 执行弱类型函数调用，直接透传 map。
func (f *MapFunction) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	if f.invokeFn == nil {
		return nil, NewErrStreamNotSupported(f.card.String())
	}

	o := NewToolCallOptions(opts...)

	// 参数格式化
	if f.card.InputParams != nil {
		// 触发 TOOL_PARSE_STARTED 事件
		runnnercallback.GetCallbackFramework().TriggerTool(ctx, &runnnercallback.ToolCallEventData{
			Event:    runnnercallback.ToolParseStarted,
			ToolName: f.card.Name,
			ToolID:   f.card.ID,
			Inputs:   inputs,
			Extra:    map[string]any{"schema": f.card.InputParams},
		})

		formatted, err := SchemaUtils{}.FormatWithSchema(inputs, f.card.InputParams,
			WithFormatSkipNoneValue(o.SkipNoneValue),
			WithFormatSkipValidate(o.SkipInputsValidate),
		)
		if err != nil {
			return nil, err
		}
		inputs = formatted

		// 触发 TOOL_PARSE_FINISHED 事件
		runnnercallback.GetCallbackFramework().TriggerTool(ctx, &runnnercallback.ToolCallEventData{
			Event:    runnnercallback.ToolParseFinished,
			ToolName: f.card.Name,
			ToolID:   f.card.ID,
			Inputs:   inputs,
		})
	}

	result, err := f.invokeFn(ctx, inputs)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", err.Error()),
		)
	}
	return result, nil
}

// Stream 执行弱类型流式函数调用。
func (f *MapFunction) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	if f.streamFn == nil {
		return nil, NewErrStreamNotSupported(f.card.String())
	}

	o := NewToolCallOptions(opts...)

	// 参数格式化
	if f.card.InputParams != nil {
		// 触发 TOOL_PARSE_STARTED 事件
		runnnercallback.GetCallbackFramework().TriggerTool(ctx, &runnnercallback.ToolCallEventData{
			Event:    runnnercallback.ToolParseStarted,
			ToolName: f.card.Name,
			ToolID:   f.card.ID,
			Inputs:   inputs,
			Extra:    map[string]any{"schema": f.card.InputParams},
		})

		formatted, err := SchemaUtils{}.FormatWithSchema(inputs, f.card.InputParams,
			WithFormatSkipNoneValue(o.SkipNoneValue),
			WithFormatSkipValidate(o.SkipInputsValidate),
		)
		if err != nil {
			return nil, err
		}
		inputs = formatted

		// 触发 TOOL_PARSE_FINISHED 事件
		runnnercallback.GetCallbackFramework().TriggerTool(ctx, &runnnercallback.ToolCallEventData{
			Event:    runnnercallback.ToolParseFinished,
			ToolName: f.card.Name,
			ToolID:   f.card.ID,
			Inputs:   inputs,
		})
	}

	innerCh, err := f.streamFn(ctx, inputs)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "stream"),
			exception.WithParam("reason", err.Error()),
		)
	}

	outCh := make(chan StreamChunk, 1)
	go func() {
		defer close(outCh)
		for chunk := range innerCh {
			outCh <- StreamChunk{Data: chunk}
		}
		outCh <- StreamChunk{Done: true}
	}()

	return outCh, nil
}
