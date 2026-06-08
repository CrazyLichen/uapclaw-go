package tool

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StructSchemaExtractor 从 Go struct 反射提取 []*schema.Param。
//
// 读取 struct 字段的 json tag（参数名）和 jsonschema tag（描述/必填/默认值/枚举），
// 递归处理嵌套 struct 和 slice，生成完整的参数定义列表。
//
// 对应 Python:
//   - openjiuwen/core/foundation/tool/utils/callable_schema_extractor.py (CallableSchemaExtractor)
//   - openjiuwen/core/foundation/tool/utils/type_schema_extractor.py (TypeSchemaExtractor 注册表)
type StructSchemaExtractor struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Extract 从 Go struct 类型反射提取 []*schema.Param。
//
// 读取字段标签：
//   - json:"name" → Param.Name（也用于判断是否导出：无 json tag 的字段跳过）
//   - json:"name,omitempty" → Param.Required=false（omitempty 表示非必填）
//   - jsonschema:"description=搜索关键词" → Param.Description
//   - jsonschema:"required" → Param.Required=true
//   - jsonschema:"default=10" → Param.Default
//   - jsonschema:"enum=a|b|c" → JSON Schema enum（用 | 分隔）
//
// 递归处理规则：
//   - 嵌套 struct → ParamTypeObject，递归提取 Properties
//   - []T / []*T → ParamTypeArray，递归提取 Items
//   - *T → 解引用后按 T 处理
//   - 基本类型 → 直接映射 ParamType
//
// 对应 Python:
//   - CallableSchemaExtractor.generate_schema()
//   - TypeSchemaExtractor 注册表
func (StructSchemaExtractor) Extract(typ reflect.Type) ([]*schema.Param, error) {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("期望 struct 类型，实际 %v", typ.Kind())
	}

	var params []*schema.Param
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// 跳过非导出字段和嵌入字段
		if !field.IsExported() {
			continue
		}

		// 解析 json tag 获取参数名
		jsonName, omitempty := parseJSONTag(field.Tag.Get("json"))
		if jsonName == "" || jsonName == "-" {
			continue
		}

		// 解析 jsonschema tag
		schemaTags := parseSchemaTag(field.Tag.Get("jsonschema"))

		// 确定 ParamType
		fieldType := field.Type
		paramType, err := goTypeToParamType(fieldType)
		if err != nil {
			return nil, fmt.Errorf("字段 %q 类型不支持: %w", jsonName, err)
		}

		param := &schema.Param{
			Name:        jsonName,
			Type:        paramType,
			Description: resolveDescription(jsonName, schemaTags),
		}

		// 确定 Required
		param.Required = resolveRequired(omitempty, schemaTags)

		// 设置默认值
		if def, ok := schemaTags.getOk("default"); ok {
			param.Default = convertDefaultValue(def, paramType)
		}

		// 设置枚举值（jsonschema:"enum=a|b|c" → Enum: ["a", "b", "c"]）
		if enumStr, ok := schemaTags.getOk("enum"); ok {
			param.Enum = strings.Split(enumStr, "|")
		}

		// 设置约束字段
		if v, ok := schemaTags.getOk("minLength"); ok {
			if n, err := fmt.Sscanf(v, "%d", &param.MinLength); err == nil && n == 1 {
				// 解析成功
			}
		}
		if v, ok := schemaTags.getOk("maxLength"); ok {
			if n, err := fmt.Sscanf(v, "%d", &param.MaxLength); err == nil && n == 1 {
				// 解析成功
			}
		}
		if v, ok := schemaTags.getOk("pattern"); ok {
			param.Pattern = v
		}
		if v, ok := schemaTags.getOk("minimum"); ok {
			if n, err := fmt.Sscanf(v, "%f", &param.Minimum); err == nil && n == 1 {
				// 解析成功
			}
		}
		if v, ok := schemaTags.getOk("maximum"); ok {
			if n, err := fmt.Sscanf(v, "%f", &param.Maximum); err == nil && n == 1 {
				// 解析成功
			}
		}
		if v, ok := schemaTags.getOk("format"); ok {
			param.Format = v
		}

		// 递归处理嵌套 struct
		if paramType == schema.ParamTypeObject {
			props, err := StructSchemaExtractor{}.Extract(dereferenceType(fieldType))
			if err != nil {
				return nil, fmt.Errorf("嵌套 struct %q 提取失败: %w", jsonName, err)
			}
			param.Properties = props
		}

		// 递归处理 slice 的 items
		if paramType == schema.ParamTypeArray {
			elemType := dereferenceType(fieldType.Elem())
			elemParamType, err := goTypeToParamType(elemType)
			if err != nil {
				return nil, fmt.Errorf("数组元素类型 %q 不支持: %w", jsonName, err)
			}
			if elemParamType == schema.ParamTypeObject {
				props, err := StructSchemaExtractor{}.Extract(elemType)
				if err != nil {
					return nil, fmt.Errorf("数组元素 struct %q 提取失败: %w", jsonName, err)
				}
				param.Items = &schema.Param{
					Type:        elemParamType,
					Properties:  props,
					Description: schemaTags.get("itemsDescription"),
				}
			} else {
				param.Items = &schema.Param{
					Type: elemParamType,
				}
			}
		}

		params = append(params, param)
	}

	return params, nil
}

// ExtractDescription 从 struct 提取工具描述。
// Go 无法在运行时获取注释/docstring，因此使用 humanize 将 struct 名转为可读描述。
// 对应 Python: CallableSchemaExtractor.extract_function_description()
func (StructSchemaExtractor) ExtractDescription(typ reflect.Type) string {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return ""
	}
	return humanizeName(typ.Name())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseJSONTag 解析 json tag，返回字段名和是否 omitempty。
func parseJSONTag(tag string) (name string, omitempty bool) {
	if tag == "" {
		return "", false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, opt := range parts[1:] {
		if opt == "omitempty" {
			omitempty = true
		}
	}
	return
}

// schemaTagMap jsonschema tag 的键值对映射
type schemaTagMap map[string]string

// parseSchemaTag 解析 jsonschema tag，格式: "description=xxx,required,default=10"
func parseSchemaTag(tag string) schemaTagMap {
	m := make(schemaTagMap)
	if tag == "" {
		return m
	}
	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, "="); idx >= 0 {
			m[part[:idx]] = part[idx+1:]
		} else {
			m[part] = "true"
		}
	}
	return m
}

func (m schemaTagMap) get(key string) string {
	return m[key]
}

func (m schemaTagMap) getOk(key string) (string, bool) {
	v, ok := m[key]
	return v, ok
}

// resolveRequired 确定 Required 字段。
// 优先级：jsonschema:"required" > json:",omitempty" > 默认必填
func resolveRequired(omitempty bool, tags schemaTagMap) bool {
	if _, ok := tags["required"]; ok {
		return true
	}
	if omitempty {
		return false
	}
	return true
}

// goTypeToParamType 将 Go 类型映射为 ParamType。
func goTypeToParamType(typ reflect.Type) (schema.ParamType, error) {
	typ = dereferenceType(typ)

	switch typ.Kind() {
	case reflect.String:
		return schema.ParamTypeString, nil
	case reflect.Bool:
		return schema.ParamTypeBoolean, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return schema.ParamTypeInteger, nil
	case reflect.Float32, reflect.Float64:
		return schema.ParamTypeNumber, nil
	case reflect.Slice, reflect.Array:
		return schema.ParamTypeArray, nil
	case reflect.Struct:
		return schema.ParamTypeObject, nil
	case reflect.Interface:
		return schema.ParamTypeObject, nil
	default:
		return schema.ParamTypeObject, fmt.Errorf("不支持的类型: %v", typ.Kind())
	}
}

// dereferenceType 解引用指针类型，返回底层类型。
func dereferenceType(typ reflect.Type) reflect.Type {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
}

// convertDefaultValue 将字符串形式的默认值按 ParamType 转换。
func convertDefaultValue(val string, typ schema.ParamType) any {
	switch typ {
	case schema.ParamTypeInteger:
		var i int
		if _, err := fmt.Sscanf(val, "%d", &i); err == nil {
			return i
		}
		return val
	case schema.ParamTypeNumber:
		var f float64
		if _, err := fmt.Sscanf(val, "%f", &f); err == nil {
			return f
		}
		return val
	case schema.ParamTypeBoolean:
		if val == "true" {
			return true
		}
		if val == "false" {
			return false
		}
		return val
	default:
		return val
	}
}

// resolveDescription 确定参数描述。
// 优先使用 jsonschema:"description=xxx" tag，缺失时使用 humanize 从参数名生成。
// 对应 Python: CallableSchemaExtractor 中 description = cls._humanize_name(param_name)
func resolveDescription(jsonName string, tags schemaTagMap) string {
	if desc := tags.get("description"); desc != "" {
		return desc
	}
	return humanizeName(jsonName)
}

// humanizeName 将变量名转换为人类可读的描述文本。
// snake_case → "search query"，camelCase/PascalCase → "user name"。
// 对应 Python: CallableSchemaExtractor._humanize_name()
func humanizeName(name string) string {
	if name == "" {
		return ""
	}

	// 处理 snake_case
	if strings.Contains(name, "_") {
		words := strings.Split(name, "_")
		var parts []string
		for _, w := range words {
			if w != "" {
				parts = append(parts, w)
			}
		}
		return strings.ToLower(strings.Join(parts, " "))
	}

	// 处理 camelCase / PascalCase
	var result []rune
	runes := []rune(name)
	for i, char := range runes {
		if i == 0 {
			result = append(result, unicode.ToLower(char))
			continue
		}
		if unicode.IsUpper(char) {
			prevLower := i > 0 && unicode.IsLower(runes[i-1])
			nextLower := i < len(runes)-1 && unicode.IsLower(runes[i+1])
			if prevLower || nextLower {
				result = append(result, ' ')
			}
			result = append(result, unicode.ToLower(char))
		} else {
			result = append(result, char)
		}
	}

	return string(result)
}
