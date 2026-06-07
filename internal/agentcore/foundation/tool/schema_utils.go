package tool

import (
	"fmt"
	"math"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SchemaUtils 工具参数 Schema 工具类，提供校验、格式化、类型转换能力。
//
// 对应 Python: openjiuwen/core/common/utils/schema_utils.py (SchemaUtils)
type SchemaUtils struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// FormatOption 格式化选项函数。
type FormatOption func(*formatOptions)

// formatOptions 格式化选项。
type formatOptions struct {
	skipNoneValue bool
	skipValidate  bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// WithFormatSkipNoneValue 设置格式化时是否跳过 nil 值。
func WithFormatSkipNoneValue(skip bool) FormatOption {
	return func(o *formatOptions) { o.skipNoneValue = skip }
}

// WithFormatSkipValidate 设置格式化时是否跳过校验。
func WithFormatSkipValidate(skip bool) FormatOption {
	return func(o *formatOptions) { o.skipValidate = skip }
}

// FormatWithSchema 根据参数 schema 格式化输入数据，填充默认值。
//
// 流程：RemoveNoneValues（可选）→ Validate（可选）→ 填充默认值
//
// 对应 Python: SchemaUtils.format_with_schema()
func (su SchemaUtils) FormatWithSchema(data map[string]any, params []*schema.Param, opts ...FormatOption) (map[string]any, error) {
	o := &formatOptions{}
	for _, opt := range opts {
		opt(o)
	}

	// 1. 可选：移除 nil 值
	if o.skipNoneValue {
		data = su.RemoveNoneValues(data)
		if data == nil {
			data = make(map[string]any)
		}
	}

	// 2. 可选：校验
	if !o.skipValidate {
		if err := su.Validate(data, params); err != nil {
			return nil, err
		}
	}

	// 3. 填充默认值
	result := make(map[string]any, len(data))
	for _, p := range params {
		if val, ok := data[p.Name]; ok {
			result[p.Name] = val
		} else if p.Default != nil {
			result[p.Name] = p.Default
		} else if p.Required {
			return nil, exception.BuildError(
				exception.StatusSchemaFormatInvalid,
				exception.WithParam("param", p.Name),
				exception.WithParam("reason", "missing required param"),
			)
		}
	}

	// 4. 保留额外字段
	for k, v := range data {
		if _, ok := result[k]; !ok {
			result[k] = v
		}
	}

	return result, nil
}

// Validate 校验输入数据是否符合参数 schema。
//
// 检查必填字段是否存在、类型是否匹配。
//
// 对应 Python: SchemaUtils.validate_with_schema()
func (su SchemaUtils) Validate(data map[string]any, params []*schema.Param) error {
	if data == nil {
		data = make(map[string]any)
	}

	paramMap := make(map[string]*schema.Param, len(params))
	for _, p := range params {
		paramMap[p.Name] = p
	}

	// 1. 检查必填字段
	for _, p := range params {
		if p.Required {
			if _, ok := data[p.Name]; !ok {
				return exception.BuildError(
					exception.StatusSchemaValidateInvalid,
					exception.WithParam("param", p.Name),
					exception.WithParam("reason", "missing required param"),
				)
			}
		}
	}

	// 2. 检查类型匹配
	for key, val := range data {
		p, ok := paramMap[key]
		if !ok {
			continue
		}
		if err := validateParamType(key, val, p); err != nil {
			return err
		}
	}

	return nil
}

// RemoveNoneValues 递归移除 map 中的 nil 值。
//
// 对应 Python: SchemaUtils.remove_none_values()
func (su SchemaUtils) RemoveNoneValues(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	result := make(map[string]any, len(data))
	for k, v := range data {
		if v == nil {
			continue
		}
		switch tv := v.(type) {
		case map[string]any:
			cleaned := su.RemoveNoneValues(tv)
			if cleaned != nil {
				result[k] = cleaned
			}
		case []any:
			cleaned := removeNoneFromArray(tv)
			if cleaned != nil {
				result[k] = cleaned
			}
		default:
			result[k] = v
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateParamType 校验值是否匹配参数类型
func validateParamType(key string, val any, p *schema.Param) error {
	switch p.Type {
	case schema.ParamTypeString:
		if _, ok := val.(string); !ok {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("expected string, got %T", val)),
			)
		}
	case schema.ParamTypeBoolean:
		if _, ok := val.(bool); !ok {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("expected boolean, got %T", val)),
			)
		}
	case schema.ParamTypeInteger:
		f, ok := val.(float64)
		if !ok {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("expected integer, got %T", val)),
			)
		}
		if f != math.Trunc(f) {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("expected integer, got float64 with fractional part %v", f)),
			)
		}
	case schema.ParamTypeNumber:
		switch val.(type) {
		case float64, int, int64:
			// ok
		default:
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("expected number, got %T", val)),
			)
		}
	case schema.ParamTypeArray:
		if _, ok := val.([]any); !ok {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("expected array, got %T", val)),
			)
		}
	case schema.ParamTypeObject:
		if _, ok := val.(map[string]any); !ok {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("expected object, got %T", val)),
			)
		}
	}
	return nil
}

// removeNoneFromArray 递归移除数组中的 nil 值
func removeNoneFromArray(arr []any) []any {
	var result []any
	for _, item := range arr {
		if item == nil {
			continue
		}
		switch tv := item.(type) {
		case map[string]any:
			cleaned := SchemaUtils{}.RemoveNoneValues(tv)
			if cleaned != nil {
				result = append(result, cleaned)
			}
		case []any:
			cleaned := removeNoneFromArray(tv)
			if cleaned != nil {
				result = append(result, cleaned)
			}
		default:
			result = append(result, item)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
