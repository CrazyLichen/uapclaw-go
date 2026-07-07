package tool

import (
	"fmt"
	"math"
	"regexp"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SchemaUtils 工具参数 Schema 工具类，提供校验、格式化、类型转换能力。
//
// 对应 Python: openjiuwen/core/common/utils/schema_utils.py (SchemaUtils)
type SchemaUtils struct{}

// formatOptions 格式化选项。
type formatOptions struct {
	skipNoneValue bool
	skipValidate  bool
}

// FormatOption 格式化选项函数。
type FormatOption func(*formatOptions)

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

	// 3. 填充默认值（递归）
	result := make(map[string]any, len(data))
	for _, p := range params {
		if val, ok := data[p.Name]; ok {
			// 递归填充嵌套默认值
			result[p.Name] = su.fillDefaults(val, p)
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

// FormatWithSchemaMap 根据原始 JSON Schema map 格式化输入数据，填充默认值。
//
// 用于 RestfulApi 等 input_params 为原始 JSON Schema map 的场景。
// schemaMap 格式为标准 JSON Schema：{"type":"object","properties":{...},"required":[...]}。
//
// 流程：RemoveNoneValues（可选）→ 校验必填字段（可选）→ 填充默认值
//
// 对应 Python: SchemaUtils.format_with_schema(schema=Dict[str,Any]) 走 JSON Schema dict 路径
func (su SchemaUtils) FormatWithSchemaMap(data map[string]any, schemaMap map[string]any, opts ...FormatOption) (map[string]any, error) {
	o := &formatOptions{}
	for _, opt := range opts {
		opt(o)
	}

	if data == nil {
		data = make(map[string]any)
	}

	// 1. 可选：移除 nil 值
	if o.skipNoneValue {
		data = su.RemoveNoneValues(data)
		if data == nil {
			data = make(map[string]any)
		}
	}

	// 提取 properties
	properties, _ := schemaMap["properties"].(map[string]any)

	// 2. 填充默认值（在校验之前，对齐 Python 逻辑）
	result := make(map[string]any, len(data))
	for name, prop := range properties {
		propMap, ok := prop.(map[string]any)
		if !ok {
			continue
		}
		if val, exists := data[name]; exists {
			result[name] = val
		} else if defaultVal, hasDefault := propMap["default"]; hasDefault {
			result[name] = defaultVal
		}
	}

	// 保留额外字段（不在 properties 中的）
	for k, v := range data {
		if _, ok := result[k]; !ok {
			result[k] = v
		}
	}

	// 3. 可选：用 jsonschema 库校验（校验填充默认值后的数据）
	if !o.skipValidate {
		c := jsonschema.NewCompiler()
		if err := c.AddResource("schema.json", schemaMap); err != nil {
			return nil, exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("reason", fmt.Sprintf("compile schema failed: %v", err)),
			)
		}
		sch, err := c.Compile("schema.json")
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("reason", fmt.Sprintf("compile schema failed: %v", err)),
			)
		}
		if err := sch.Validate(result); err != nil {
			return nil, exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("reason", err.Error()),
			)
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
		// 3. 检查约束条件
		if err := validateParamConstraints(key, val, p); err != nil {
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
			// 类型匹配，无需处理
		default:
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("expected number, got %T", val)),
			)
		}
	case schema.ParamTypeArray:
		arr, ok := val.([]any)
		if !ok {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("expected array, got %T", val)),
			)
		}
		// 递归校验 array items
		if p.Items != nil {
			for i, item := range arr {
				itemKey := fmt.Sprintf("%s[%d]", key, i)
				if err := validateParamType(itemKey, item, p.Items); err != nil {
					return err
				}
				if err := validateParamConstraints(itemKey, item, p.Items); err != nil {
					return err
				}
			}
		}
	case schema.ParamTypeObject:
		obj, ok := val.(map[string]any)
		if !ok {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("expected object, got %T", val)),
			)
		}
		// 递归校验 object properties
		if len(p.Properties) > 0 {
			propMap := make(map[string]*schema.Param, len(p.Properties))
			for _, prop := range p.Properties {
				propMap[prop.Name] = prop
			}
			for k, v := range obj {
				if prop, ok := propMap[k]; ok {
					if err := validateParamType(k, v, prop); err != nil {
						return err
					}
					if err := validateParamConstraints(k, v, prop); err != nil {
						return err
					}
				}
			}
			// 校验嵌套 required
			for _, prop := range p.Properties {
				if prop.Required {
					if _, ok := obj[prop.Name]; !ok {
						return exception.BuildError(
							exception.StatusSchemaValidateInvalid,
							exception.WithParam("param", prop.Name),
							exception.WithParam("reason", "missing required param in nested object"),
						)
					}
				}
			}
		}
	}
	return nil
}

// validateParamConstraints 校验值是否符合参数约束条件（minLength/maxLength/pattern/minimum/maximum）
func validateParamConstraints(key string, val any, p *schema.Param) error {
	switch p.Type {
	case schema.ParamTypeString:
		s, ok := val.(string)
		if !ok {
			return nil // 类型不匹配已在 validateParamType 中处理
		}
		if p.MinLength > 0 && len(s) < p.MinLength {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("string length %d < minLength %d", len(s), p.MinLength)),
			)
		}
		if p.MaxLength > 0 && len(s) > p.MaxLength {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("string length %d > maxLength %d", len(s), p.MaxLength)),
			)
		}
		if p.Pattern != "" {
			matched, err := regexp.MatchString(p.Pattern, s)
			if err != nil {
				return exception.BuildError(
					exception.StatusSchemaValidateInvalid,
					exception.WithParam("param", key),
					exception.WithParam("reason", fmt.Sprintf("invalid pattern %q: %v", p.Pattern, err)),
				)
			}
			if !matched {
				return exception.BuildError(
					exception.StatusSchemaValidateInvalid,
					exception.WithParam("param", key),
					exception.WithParam("reason", fmt.Sprintf("string %q does not match pattern %q", s, p.Pattern)),
				)
			}
		}
	case schema.ParamTypeInteger, schema.ParamTypeNumber:
		f, ok := toFloat64(val)
		if !ok {
			return nil // 类型不匹配已在 validateParamType 中处理
		}
		if !math.IsNaN(p.Minimum) && f < p.Minimum {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("value %v < minimum %v", f, p.Minimum)),
			)
		}
		if !math.IsNaN(p.Maximum) && f > p.Maximum {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("value %v > maximum %v", f, p.Maximum)),
			)
		}
	case schema.ParamTypeArray:
		arr, ok := val.([]any)
		if !ok {
			return nil // 类型不匹配已在 validateParamType 中处理
		}
		if p.MinItems > 0 && len(arr) < p.MinItems {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("array length %d < minItems %d", len(arr), p.MinItems)),
			)
		}
		if p.MaxItems > 0 && len(arr) > p.MaxItems {
			return exception.BuildError(
				exception.StatusSchemaValidateInvalid,
				exception.WithParam("param", key),
				exception.WithParam("reason", fmt.Sprintf("array length %d > maxItems %d", len(arr), p.MaxItems)),
			)
		}
	}
	return nil
}

// toFloat64 将数值类型转换为 float64
func toFloat64(val any) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	default:
		return 0, false
	}
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

// fillDefaults 递归填充嵌套默认值。
func (su SchemaUtils) fillDefaults(val any, p *schema.Param) any {
	switch p.Type {
	case schema.ParamTypeObject:
		obj, ok := val.(map[string]any)
		if !ok || len(p.Properties) == 0 {
			return val
		}
		result := make(map[string]any, len(obj))
		for k, v := range obj {
			result[k] = v
		}
		for _, prop := range p.Properties {
			if _, ok := result[prop.Name]; !ok && prop.Default != nil {
				result[prop.Name] = prop.Default
			}
		}
		return result
	case schema.ParamTypeArray:
		arr, ok := val.([]any)
		if !ok || p.Items == nil {
			return val
		}
		// 递归填充数组中每个元素的默认值
		result := make([]any, len(arr))
		for i, item := range arr {
			result[i] = su.fillDefaults(item, p.Items)
		}
		return result
	default:
		return val
	}
}
