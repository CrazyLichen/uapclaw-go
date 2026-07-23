package tool

import (
	"fmt"
	"math"
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
//
// TODO(#通用): 当前仅支持 struct 反射提取，Python 有 12 种 TypeSchemaExtractor
// （可选类型/联合类型/字面量/列表/字典/元组/集合/枚举/任意类型/日期/日期时间/类型字典），
// 后续可添加 TypeSchemaExtractor 注册表机制，按类型选择提取器，
// 支持 Optional → Nullable、Union → anyOf、Literal → enum 等高级类型的 Schema 生成。
type StructSchemaExtractor struct{}

// schemaTagMap jsonschema tag 的键值对映射
type schemaTagMap map[string]string

// ──────────────────────────── 全局变量 ────────────────────────────

// commonAbbreviations 常见缩写列表，humanizeName 时转大写处理。
// 对应 Python: CallableSchemaExtractor._humanize_name() 中的 abbreviations 列表。
var commonAbbreviations = map[string]string{
	"id":    "ID",
	"url":   "URL",
	"uri":   "URI",
	"api":   "API",
	"ip":    "IP",
	"tcp":   "TCP",
	"udp":   "UDP",
	"http":  "HTTP",
	"https": "HTTPS",
	"ssl":   "SSL",
	"tls":   "TLS",
	"dns":   "DNS",
	"ssh":   "SSH",
	"sql":   "SQL",
	"json":  "JSON",
	"xml":   "XML",
	"html":  "HTML",
	"css":   "CSS",
	"cpu":   "CPU",
	"gpu":   "GPU",
	"ram":   "RAM",
	"io":    "IO",
	"os":    "OS",
}

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
			Minimum:     math.NaN(),
			Maximum:     math.NaN(),
		}

		// 确定 Required
		param.Required = resolveRequired(omitempty, schemaTags)

		// 设置默认值
		if def, ok := schemaTags.getOk("default"); ok {
			param.Default = convertDefaultValue(def, paramType)
		}

		// 设置枚举值（jsonschema:"enum=a|b|c" → Enum: ["a", "b", "c"]）
		if enumStr, ok := schemaTags.getOk("enum"); ok {
			parts := strings.Split(enumStr, "|")
			enumAny := make([]any, len(parts))
			for i, p := range parts {
				enumAny[i] = p
			}
			param.Enum = enumAny
		}

		// 设置约束字段
		if v, ok := schemaTags.getOk("minLength"); ok {
			n, _ := fmt.Sscanf(v, "%d", &param.MinLength)
			_ = n // Sscanf 已通过指针写入 param.MinLength
		}
		if v, ok := schemaTags.getOk("maxLength"); ok {
			n, _ := fmt.Sscanf(v, "%d", &param.MaxLength)
			_ = n // Sscanf 已通过指针写入 param.MaxLength
		}
		if v, ok := schemaTags.getOk("pattern"); ok {
			param.Pattern = v
		}
		if v, ok := schemaTags.getOk("minimum"); ok {
			n, _ := fmt.Sscanf(v, "%f", &param.Minimum)
			_ = n // Sscanf 已通过指针写入 param.Minimum
		}
		if v, ok := schemaTags.getOk("maximum"); ok {
			n, _ := fmt.Sscanf(v, "%f", &param.Maximum)
			_ = n // Sscanf 已通过指针写入 param.Maximum
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
//
// 与 Python 的差异：Python 的 CallableSchemaExtractor.extract_function_description() 会自动
// 从函数名提取描述（如 do_something → "do something"），而 Go 没有运行时函数名访问能力，
// 要求通过 jsonschema:"description=..." tag 显式指定描述。当未指定 description tag 时，
// 本方法使用 humanizeName 从 struct 名生成描述作为回退。
//
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

// get 获取指定键的值。
func (m schemaTagMap) get(key string) string {
	return m[key]
}

// getOk 获取指定键的值和是否存在。
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
// snake_case → "搜索查询"，camelCase/PascalCase → "用户名"。
// 对常见缩写（id, url, api 等）转大写，与 Python _humanize_name 行为一致。
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
				parts = append(parts, strings.ToLower(w))
			}
		}
		return strings.Join(parts, " ")
	}

	// 处理 camelCase / PascalCase：拆分单词后再处理缩写
	words := splitCamelWords(name)
	var parts []string
	for _, w := range words {
		lower := strings.ToLower(w)
		if upper, ok := commonAbbreviations[lower]; ok {
			parts = append(parts, upper)
		} else {
			parts = append(parts, lower)
		}
	}
	return strings.Join(parts, " ")
}

// splitCamelWords 将 camelCase/PascalCase 名称拆分为单词列表。
// 例如："userId" → ["user", "Id"]，"XMLParser" → ["XML", "Parser"]，"HTTPSUrl" → ["HTTPS", "Url"]。
func splitCamelWords(name string) []string {
	var words []string
	var current []rune
	runes := []rune(name)

	for i, char := range runes {
		if i == 0 {
			current = append(current, char)
			continue
		}
		if unicode.IsUpper(char) {
			prevLower := i > 0 && unicode.IsLower(runes[i-1])
			nextLower := i < len(runes)-1 && unicode.IsLower(runes[i+1])
			if prevLower || nextLower {
				if len(current) > 0 {
					words = append(words, string(current))
				}
				current = []rune{char}
			} else {
				current = append(current, char)
			}
		} else {
			current = append(current, char)
		}
	}
	if len(current) > 0 {
		words = append(words, string(current))
	}
	return words
}
