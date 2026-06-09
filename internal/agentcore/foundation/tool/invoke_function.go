package tool

import (
	"context"
	"encoding/json"
	"reflect"

	runnnercallback "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InvokeFunction 本地函数工具（Invoke 模式），将 Go 函数包装为 Tool。
//
// 用户函数签名：func(ctx context.Context, input I) (O, error)
//
// 对应 Python: openjiuwen/core/foundation/tool/function/function.py (LocalFunction)
// Python 不区分 Invoke/Stream，Go 通过不同类型在编译期保证签名正确。
type InvokeFunction[I any, O any] struct {
	card *ToolCard
	fn   func(context.Context, I) (O, error)
}

// localFuncConfig 内部配置。
type localFuncConfig struct {
	description string
	inputParams []*schema.Param
	card        *ToolCard
}

// ──────────────────────────── 枚举 ────────────────────────────

// LocalFuncOption 本地函数构造选项函数。
type LocalFuncOption func(*localFuncConfig)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithDescription 设置工具描述（覆盖自动提取）。
func WithDescription(desc string) LocalFuncOption {
	return func(c *localFuncConfig) { c.description = desc }
}

// WithInputParams 手动设置输入参数（覆盖自动提取）。
func WithInputParams(params []*schema.Param) LocalFuncOption {
	return func(c *localFuncConfig) { c.inputParams = params }
}

// WithCard 使用预构建的 ToolCard。
func WithCard(card *ToolCard) LocalFuncOption {
	return func(c *localFuncConfig) { c.card = card }
}

// NewInvokeFunction 创建 Invoke 模式的本地函数工具。
//
// 自动从 I 类型的 struct tag 提取 InputParams 填入 ToolCard。
// Go 编译器从 fn 参数自动推断 I 和 O，用户通常无需显式指定泛型参数。
//
// 使用示例：
//
//	fn, _ := NewInvokeFunction("search", Search)
//	// 等价于 NewInvokeFunction[SearchInput, SearchOutput]("search", Search)
//
// 对应 Python: LocalFunction(card=card, func=func)
func NewInvokeFunction[I any, O any](name string, fn func(context.Context, I) (O, error), opts ...LocalFuncOption) (*InvokeFunction[I, O], error) {
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

	// 校验 ToolCard 合法性
	if err := ValidateToolCard(card); err != nil {
		return nil, err
	}

	return &InvokeFunction[I, O]{card: card, fn: fn}, nil
}

// Card 返回工具配置卡片。
func (f *InvokeFunction[I, O]) Card() *ToolCard {
	return f.card
}

// Invoke 执行工具调用：校验输入 → 格式化 → map→struct → 调用用户函数 → struct→map。
func (f *InvokeFunction[I, O]) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	o := NewToolCallOptions(opts...)

	// 1. 参数格式化
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

	// 2. map → struct
	jsonBytes, err := json.Marshal(inputs)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", "marshal inputs failed"),
		)
	}
	var input I
	if err := json.Unmarshal(jsonBytes, &input); err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", "unmarshal inputs to struct failed"),
		)
	}

	// 3. 调用用户函数
	output, err := f.fn(ctx, input)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 4. struct → map
	result, err := structToMap(output)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", "convert output to map failed"),
		)
	}

	return result, nil
}

// Stream 不支持流式调用，返回 ErrStreamNotSupported。
func (f *InvokeFunction[I, O]) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	return nil, NewErrStreamNotSupported(f.card.String())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// structToMap 将任意值转换为 map[string]any。
func structToMap(v any) (map[string]any, error) {
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		// 非 object 输出包装为 {"result": v}
		// 与 Python 的差异：Python LocalFunction 直接返回原始值（其 Tool 接口不限制返回类型），
		// Go 的 Tool 接口要求返回 map[string]any，非 object 返回值必须包装为 {"result": v} 以满足接口约束。
		return map[string]any{"result": v}, nil
	}
	return result, nil
}
