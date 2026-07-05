package tool

import (
	"context"
	"reflect"
	"runtime"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// toolFuncConfig 便捷函数内部配置。
type toolFuncConfig struct {
	name        string
	description string
	inputParams []*schema.Param
	card        *ToolCard
}

// ToolFuncOption 工具注册选项函数。
type ToolFuncOption func(*toolFuncConfig)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithToolName 设置工具名称（覆盖函数名）。
func WithToolName(name string) ToolFuncOption {
	return func(c *toolFuncConfig) { c.name = name }
}

// WithToolDescription 设置工具描述（覆盖自动提取）。
func WithToolDescription(desc string) ToolFuncOption {
	return func(c *toolFuncConfig) { c.description = desc }
}

// WithToolInputParams 手动设置输入参数（覆盖自动提取）。
func WithToolInputParams(params []*schema.Param) ToolFuncOption {
	return func(c *toolFuncConfig) { c.inputParams = params }
}

// WithToolCard 使用预构建的 ToolCard。
func WithToolCard(card *ToolCard) ToolFuncOption {
	return func(c *toolFuncConfig) { c.card = card }
}

// NewTool 便捷注册函数（Invoke 模式），对标 Python @tool 装饰器。
//
// Go 编译器从 fn 参数自动推断 I 和 O，用户无需显式指定泛型参数。
//
// 使用示例：
//
//	// 最简写法：自动推断 I/O，自动提取 schema
//	fn, _ := NewTool(Search)
//
//	// 自定义名称和描述
//	fn, _ := NewTool(Search, WithToolName("custom_search"), WithToolDescription("搜索工具"))
//
//	// 手动指定参数（覆盖自动提取）
//	fn, _ := NewTool(Search, WithToolInputParams(params))
func NewTool[I any, O any](fn func(context.Context, I, ...ToolOption) (O, error), opts ...ToolFuncOption) (*InvokeFunction[I, O], error) {
	cfg := &toolFuncConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 确定名称（从函数名提取，或由选项覆盖）
	name := cfg.name
	if name == "" {
		name = extractFuncName(fn)
	}

	// 确定 InputParams（从 I 类型反射提取，或由选项覆盖）
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
			return nil, err
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

	return &InvokeFunction[I, O]{card: card, fn: fn}, nil
}

// NewStreamTool 便捷注册函数（Stream 模式），对标 Python @tool 装饰器。
//
// 使用示例：
//
//	fn, _ := NewStreamTool(StreamSearch, WithToolName("stream_search"))
func NewStreamTool[I any, O any](fn func(context.Context, I, ...ToolOption) (<-chan O, error), opts ...ToolFuncOption) (*StreamFunction[I, O], error) {
	cfg := &toolFuncConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 确定名称
	name := cfg.name
	if name == "" {
		name = extractFuncName(fn)
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
			return nil, err
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractFuncName 通过 runtime.FuncForPC 提取函数名。
func extractFuncName(fn any) string {
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		return ""
	}
	fullName := runtime.FuncForPC(v.Pointer()).Name()
	// 取最后一个 . 之后的部分
	if idx := strings.LastIndex(fullName, "."); idx >= 0 {
		fullName = fullName[idx+1:]
	}
	return fullName
}
