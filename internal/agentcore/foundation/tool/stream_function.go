package tool

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamFunction 本地函数工具（Stream 模式），将 Go 流式函数包装为 Tool。
//
// 用户函数签名：func(ctx context.Context, input I) (<-chan O, error)
//
// 对应 Python: openjiuwen/core/foundation/tool/function/function.py (LocalFunction.stream)
type StreamFunction[I any, O any] struct {
	card *ToolCard
	fn   func(context.Context, I) (<-chan O, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamFunction 创建 Stream 模式的本地函数工具。
//
// 使用示例：
//
//	fn, _ := NewStreamFunction("stream_search", StreamSearch)
func NewStreamFunction[I any, O any](name string, fn func(context.Context, I) (<-chan O, error), opts ...LocalFuncOption) (*StreamFunction[I, O], error) {
	cfg := &localFuncConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 确定 InputParams
	var inputParams []*schema.Param
	if cfg.inputParams != nil {
		inputParams = cfg.inputParams
	} else {
		typ := reflect.TypeOf((*I)(nil)).Elem()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		extracted, err := StructSchemaExtractor{}.Extract(typ)
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusToolLocalFunctionFuncNotSupported,
				exception.WithParam("name", name),
				exception.WithParam("reason", err.Error()),
			)
		}
		inputParams = extracted
	}

	// 确定描述
	description := cfg.description
	if description == "" {
		typ := reflect.TypeOf((*I)(nil)).Elem()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		description = StructSchemaExtractor{}.ExtractDescription(typ)
	}
	if description == "" {
		description = name
	}

	// 构建 ToolCard
	var card *ToolCard
	if cfg.card != nil {
		card = cfg.card
	} else {
		card = NewToolCard(name, description, inputParams, nil)
	}

	return &StreamFunction[I, O]{card: card, fn: fn}, nil
}

// Card 返回工具配置卡片。
func (f *StreamFunction[I, O]) Card() *ToolCard {
	return f.card
}

// Invoke 不支持一次性调用，返回 ErrStreamNotSupported。
func (f *StreamFunction[I, O]) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	return nil, NewErrStreamNotSupported(f.card.String())
}

// Stream 流式执行工具调用：校验输入 → 格式化 → map→struct → 调用用户流式函数 → 逐 chunk 转换。
func (f *StreamFunction[I, O]) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	o := NewToolCallOptions(opts...)

	// 1. 参数格式化
	if f.card.InputParams != nil {
		formatted, err := SchemaUtils{}.FormatWithSchema(inputs, f.card.InputParams,
			WithFormatSkipNoneValue(o.SkipNoneValue),
			WithFormatSkipValidate(o.SkipInputsValidate),
		)
		if err != nil {
			return nil, err
		}
		inputs = formatted
	}

	// 2. map → struct
	jsonBytes, err := json.Marshal(inputs)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "stream"),
			exception.WithParam("reason", "marshal inputs failed"),
		)
	}
	var input I
	if err := json.Unmarshal(jsonBytes, &input); err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "stream"),
			exception.WithParam("reason", "unmarshal inputs to struct failed"),
		)
	}

	// 3. 调用用户流式函数
	ch, err := f.fn(ctx, input)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "stream"),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 4. 包装输出 channel
	outCh := make(chan StreamChunk, 1)
	go func() {
		defer close(outCh)
		for chunk := range ch {
			data, err := structToMap(chunk)
			if err != nil {
				outCh <- StreamChunk{Error: err}
				return
			}
			outCh <- StreamChunk{Data: data}
		}
		outCh <- StreamChunk{Done: true}
	}()

	return outCh, nil
}
