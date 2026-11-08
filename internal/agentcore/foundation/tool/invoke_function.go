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

// localFuncConfig 本地函数配置，用于构造 InvokeFunction 时的内部参数。
type localFuncConfig struct {
	description string
	inputParams []*schema.Param
	card        *ToolCard
	// callTimeout 可选的外层预算钩子，供有内部执行期限的工具注册
	callTimeout func(inputs map[string]any, defaultTimeout float64) float64
}

// ──────────────────────────── 枚举 ────────────────────────────

// InvokeFunction 泛型本地函数工具，将 Go 函数包装为 Tool 接口实现。
type InvokeFunction[I any, O any] struct {
	card *ToolCard
	fn   func(context.Context, I, ...ToolOption) (O, error)
	// callTimeout 可选的外层预算钩子；nil 表示沿用卡片级策略。
	// 有内部执行期限的工具（bash/powershell）通过 WithCallTimeout 注册。
	callTimeout func(inputs map[string]any, defaultTimeout float64) float64
}

// LocalFuncOption 本地函数构造选项函数。
type LocalFuncOption func(*localFuncConfig)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithDescription 设置函数描述选项。
func WithDescription(desc string) LocalFuncOption {
	return func(c *localFuncConfig) { c.description = desc }
}

// WithInputParams 设置输入参数列表选项。
func WithInputParams(params []*schema.Param) LocalFuncOption {
	return func(c *localFuncConfig) { c.inputParams = params }
}

// WithCard 设置工具卡片选项。
func WithCard(card *ToolCard) LocalFuncOption {
	return func(c *localFuncConfig) { c.card = card }
}

// WithCallTimeout 为工具注册外层调用预算钩子。
// inputs 是调用方的原始参数 map；钩子返回本次调用应使用的预算（秒）。
// 有内部执行期限的工具（bash/powershell）用它把执行预算加上清理宽限，
// 使外层超时晚于内层超时。
func WithCallTimeout(fn func(inputs map[string]any, defaultTimeout float64) float64) LocalFuncOption {
	return func(c *localFuncConfig) { c.callTimeout = fn }
}

// NewInvokeFunction 创建泛型本地函数工具，将 Go 函数包装为 Tool 接口实现。
func NewInvokeFunction[I any, O any](name string, fn func(context.Context, I, ...ToolOption) (O, error), opts ...LocalFuncOption) (*InvokeFunction[I, O], error) {
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

	return &InvokeFunction[I, O]{card: card, fn: fn, callTimeout: cfg.callTimeout}, nil
}

// Card 返回该工具的描述卡片。
func (f *InvokeFunction[I, O]) Card() *ToolCard {
	return f.card
}

// Invoke 执行工具：先把入参转成强类型结构，再调用业务函数，最后把结果转回 map。
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

	// 2. map 转 struct（json 序列化中转）
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
	output, err := f.fn(ctx, input, opts...)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 4. struct 转 map（输出序列化）
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

// Stream 流式执行工具，函数式工具不支持流式返回。
func (f *InvokeFunction[I, O]) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	return nil, NewErrStreamNotSupported(f.card.String())
}

// GetCallTimeout 返回外层调用预算。
// 注册了钩子时委托给它（bash/powershell 用它在执行预算上再加清理宽限），
// 否则沿用卡片级策略。
func (f *InvokeFunction[I, O]) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	if f.callTimeout != nil {
		return f.callTimeout(inputs, defaultTimeout)
	}
	return BaseCallTimeout(inputs, defaultTimeout)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// structToMap 把结构体转成字典，非字典结构的输出统一包成 {"result": v}。
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
