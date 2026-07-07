package schema

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Param 参数定义模型，支持嵌套结构。
//
// 用于描述工具、Agent、Workflow 的输入参数，最终转换为 LLM function calling 的 JSON Schema。
//
// 设计原则：
//   - Items 字段仅在 Type 为 Array 时使用
//   - Properties 字段仅在 Type 为 Object 时使用
//   - 其他类型这两个字段必须为 nil
//
// 对应 Python: openjiuwen/core/common/schema/param.py (Param)
type Param struct {
	// Name 参数名
	Name string `json:"name"`
	// Description 参数描述
	Description string `json:"description"`
	// Type 参数类型
	Type ParamType `json:"type"`
	// Required 是否必填
	Required bool `json:"required"`
	// Default 默认值（可选），实际类型取决于 Type
	Default any `json:"default,omitempty"`
	// Enum 枚举值列表（可选），限制参数只能取这些值，元素类型取决于 Type（string/number/integer/boolean/null）
	Enum []any `json:"enum,omitempty"`
	// MinLength 字符串最小长度（可选，仅 String 类型）
	MinLength int `json:"minLength,omitempty"`
	// MaxLength 字符串最大长度（可选，仅 String 类型）
	MaxLength int `json:"maxLength,omitempty"`
	// Pattern 正则校验模式（可选，仅 String 类型）
	Pattern string `json:"pattern,omitempty"`
	// Minimum 数值最小值（可选，仅 Integer/Number 类型）
	// 使用 NaN 作为无效值标记，math.IsNaN(Minimum) 表示未设置，0 是合法约束值
	Minimum float64 `json:"-"`
	// Maximum 数值最大值（可选，仅 Integer/Number 类型）
	// 使用 NaN 作为无效值标记，math.IsNaN(Maximum) 表示未设置，0 是合法约束值
	Maximum float64 `json:"-"`
	// Format 格式标识（可选，如 email/uri/date-time 等）
	Format string `json:"format,omitempty"`
	// Nullable 是否可为 null（可选），输出 JSON Schema 时将 type 扩展为数组含 "null"
	Nullable bool `json:"nullable,omitempty"`
	// Items 数组元素类型定义（仅 Array 类型使用）
	Items *Param `json:"items,omitempty"`
	// Properties 对象属性列表（仅 Object 类型使用）
	Properties []*Param `json:"properties,omitempty"`
	// AdditionalProperties 对象是否允许额外属性（可选，仅 Object 类型）
	AdditionalProperties bool `json:"additionalProperties,omitempty"`
	// MinItems 数组最少元素数（可选，仅 Array 类型）
	MinItems int `json:"minItems,omitempty"`
	// MaxItems 数组最多元素数（可选，仅 Array 类型）
	MaxItems int `json:"maxItems,omitempty"`
	// AnyOf 多子 schema 至少匹配一个（JSON Schema 标准关键字）
	AnyOf []*Param `json:"anyOf,omitempty"`
	// AllOf 多子 schema 全部匹配（JSON Schema 标准关键字）
	AllOf []*Param `json:"allOf,omitempty"`
	// OneOf 多子 schema 恰好匹配一个（JSON Schema 标准关键字）
	OneOf []*Param `json:"oneOf,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ParamType 参数类型枚举，对应 JSON Schema 的基本类型。
//
// 对应 Python: openjiuwen/core/common/schema/param.py (ParamType)
type ParamType int

const (
	// ParamTypeString 字符串类型
	ParamTypeString ParamType = iota
	// ParamTypeBoolean 布尔类型
	ParamTypeBoolean
	// ParamTypeInteger 整数类型
	ParamTypeInteger
	// ParamTypeNumber 浮点数类型
	ParamTypeNumber
	// ParamTypeArray 数组类型
	ParamTypeArray
	// ParamTypeObject 对象类型
	ParamTypeObject
)

// ──────────────────────────── 全局变量 ────────────────────────────

// paramTypeStrings ParamType 枚举值对应的字符串表示，与 Python 端保持一致。
var paramTypeStrings = [...]string{
	"string",
	"boolean",
	"integer",
	"number",
	"array",
	"object",
}

// paramTypeMap 字符串到 ParamType 的映射，用于 JSON 反序列化。
var paramTypeMap map[string]ParamType

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStringParam 创建字符串类型参数。
//
// 对应 Python: Param.string(name, description, required, default)
func NewStringParam(name, description string, required bool, defaultVal ...string) *Param {
	p := &Param{
		Name:        name,
		Description: description,
		Type:        ParamTypeString,
		Required:    required,
	}
	if len(defaultVal) > 0 {
		p.Default = defaultVal[0]
	}
	return p
}

// NewBooleanParam 创建布尔类型参数。
func NewBooleanParam(name, description string, required bool, defaultVal ...bool) *Param {
	p := &Param{
		Name:        name,
		Description: description,
		Type:        ParamTypeBoolean,
		Required:    required,
	}
	if len(defaultVal) > 0 {
		p.Default = defaultVal[0]
	}
	return p
}

// NewIntegerParam 创建整数类型参数。
func NewIntegerParam(name, description string, required bool, defaultVal ...int) *Param {
	p := &Param{
		Name:        name,
		Description: description,
		Type:        ParamTypeInteger,
		Required:    required,
		Minimum:     math.NaN(),
		Maximum:     math.NaN(),
	}
	if len(defaultVal) > 0 {
		p.Default = defaultVal[0]
	}
	return p
}

// NewNumberParam 创建浮点数类型参数。
func NewNumberParam(name, description string, required bool, defaultVal ...float64) *Param {
	p := &Param{
		Name:        name,
		Description: description,
		Type:        ParamTypeNumber,
		Required:    required,
		Minimum:     math.NaN(),
		Maximum:     math.NaN(),
	}
	if len(defaultVal) > 0 {
		p.Default = defaultVal[0]
	}
	return p
}

// NewArrayParam 创建数组类型参数，items 定义数组元素的类型。
//
// 对应 Python: Param.array(name, description, required, items, default)
func NewArrayParam(name, description string, required bool, items *Param, defaultVal ...[]any) *Param {
	p := &Param{
		Name:        name,
		Description: description,
		Type:        ParamTypeArray,
		Required:    required,
		Items:       items,
	}
	if len(defaultVal) > 0 {
		p.Default = defaultVal[0]
	}
	return p
}

// NewObjectParam 创建对象类型参数，properties 定义对象的属性列表。
//
// 对应 Python: Param.object(name, description, required, properties, default)
func NewObjectParam(name, description string, required bool, properties []*Param, defaultVal ...map[string]any) *Param {
	p := &Param{
		Name:        name,
		Description: description,
		Type:        ParamTypeObject,
		Required:    required,
		Properties:  properties,
	}
	if len(defaultVal) > 0 {
		p.Default = defaultVal[0]
	}
	return p
}

// String 实现 fmt.Stringer 接口，返回参数的简洁描述。
func (t ParamType) String() string {
	if int(t) >= 0 && int(t) < len(paramTypeStrings) {
		return paramTypeStrings[t]
	}
	return fmt.Sprintf("ParamType(%d)", int(t))
}

// MarshalJSON 实现 json.Marshaler 接口，将 ParamType 序列化为字符串。
// 与 Python 端枚举值格式保持一致（"string"/"boolean"/"integer"/"number"/"array"/"object"）。
func (t ParamType) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口，将字符串反序列化为 ParamType。
func (t *ParamType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("ParamType 反序列化失败: %w", err)
	}
	pt, ok := paramTypeMap[strings.ToLower(s)]
	if !ok {
		return fmt.Errorf("未知的 ParamType: %q", s)
	}
	*t = pt
	return nil
}

// Validate 验证参数定义的一致性。
//
// 规则：
//   - Array 类型必须有 Items，不能有 Properties
//   - Object 类型必须有 Properties，不能有 Items
//   - 其他类型不能有 Items 或 Properties
//   - AnyOf/AllOf/OneOf 可与任意类型共存（JSON Schema 标准）
//
// 对应 Python: Param.validate_type_specific_fields()
func (p *Param) Validate() error {
	switch p.Type {
	case ParamTypeArray:
		if p.Items == nil {
			return fmt.Errorf("Param %q: Array 类型必须有 items 字段", p.Name)
		}
		if len(p.Properties) > 0 {
			return fmt.Errorf("Param %q: Array 类型不应有 properties 字段", p.Name)
		}
		if p.AdditionalProperties {
			return fmt.Errorf("Param %q: Array 类型不应有 additionalProperties 字段", p.Name)
		}
		// 递归验证 items
		if err := p.Items.Validate(); err != nil {
			return err
		}

	case ParamTypeObject:
		if len(p.Properties) == 0 && !p.AdditionalProperties {
			return fmt.Errorf("Param %q: Object 类型必须有 properties 字段或 additionalProperties=true", p.Name)
		}
		if p.Items != nil {
			return fmt.Errorf("Param %q: Object 类型不应有 items 字段", p.Name)
		}
		if p.MinItems > 0 {
			return fmt.Errorf("Param %q: Object 类型不应有 minItems 字段", p.Name)
		}
		if p.MaxItems > 0 {
			return fmt.Errorf("Param %q: Object 类型不应有 maxItems 字段", p.Name)
		}
		// 递归验证 properties
		for _, prop := range p.Properties {
			if err := prop.Validate(); err != nil {
				return err
			}
		}

	default:
		if p.Items != nil {
			return fmt.Errorf("Param %q: %s 类型不应有 items 字段", p.Name, p.Type)
		}
		if len(p.Properties) > 0 {
			return fmt.Errorf("Param %q: %s 类型不应有 properties 字段", p.Name, p.Type)
		}
		if p.AdditionalProperties {
			return fmt.Errorf("Param %q: %s 类型不应有 additionalProperties 字段", p.Name, p.Type)
		}
		if p.MinItems > 0 {
			return fmt.Errorf("Param %q: %s 类型不应有 minItems 字段", p.Name, p.Type)
		}
		if p.MaxItems > 0 {
			return fmt.Errorf("Param %q: %s 类型不应有 maxItems 字段", p.Name, p.Type)
		}
	}

	// 递归验证组合 Schema
	for _, sub := range p.AnyOf {
		if err := sub.Validate(); err != nil {
			return err
		}
	}
	for _, sub := range p.AllOf {
		if err := sub.Validate(); err != nil {
			return err
		}
	}
	for _, sub := range p.OneOf {
		if err := sub.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// ToJSONSchemaMap 将 []*Param 列表转换为 OpenAI function calling 格式的 JSON Schema parameters。
//
// 生成格式：
//
//	{
//	  "type": "object",
//	  "properties": { <每个 Param 的 JSON Schema> },
//	  "required": [ <必填参数名列表> ]
//	}
//
// 对应 Python: ToolInfo.parameters 从 ToolCard.input_params 自动生成的逻辑
func ToJSONSchemaMap(params []*Param) map[string]any {
	if len(params) == 0 {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	properties := make(map[string]any, len(params))
	var required []string

	for _, p := range params {
		properties[p.Name] = paramToSchema(p)
		if p.Required {
			required = append(required, p.Name)
		}
	}

	result := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}

// String 实现 fmt.Stringer 接口，返回参数的简洁描述。
func (p *Param) String() string {
	return fmt.Sprintf("%s(%s, required=%v)", p.Name, p.Type, p.Required)
}

// MarshalJSON 实现 json.Marshaler 接口，处理 NaN 值的 Minimum/Maximum。
// NaN 表示未设置，不输出到 JSON；非 NaN（包括 0）正常输出。
func (p *Param) MarshalJSON() ([]byte, error) {
	// 直接构建 map，避免 Marshal→Unmarshal→Marshal 的 round-trip 导致精度丢失
	m := map[string]any{
		"name":        p.Name,
		"description": p.Description,
		"type":        p.Type,
		"required":    p.Required,
	}

	// 可选字段
	if p.Default != nil {
		m["default"] = p.Default
	}
	if len(p.Enum) > 0 {
		m["enum"] = p.Enum
	}
	if p.MinLength > 0 {
		m["minLength"] = p.MinLength
	}
	if p.MaxLength > 0 {
		m["maxLength"] = p.MaxLength
	}
	if p.Pattern != "" {
		m["pattern"] = p.Pattern
	}
	if !math.IsNaN(p.Minimum) {
		m["minimum"] = p.Minimum
	}
	if !math.IsNaN(p.Maximum) {
		m["maximum"] = p.Maximum
	}
	if p.Format != "" {
		m["format"] = p.Format
	}
	if p.Nullable {
		m["nullable"] = p.Nullable
	}
	if p.Items != nil {
		m["items"] = p.Items
	}
	if len(p.Properties) > 0 {
		m["properties"] = p.Properties
	}
	if p.AdditionalProperties {
		m["additionalProperties"] = p.AdditionalProperties
	}
	if p.MinItems > 0 {
		m["minItems"] = p.MinItems
	}
	if p.MaxItems > 0 {
		m["maxItems"] = p.MaxItems
	}
	if len(p.AnyOf) > 0 {
		m["anyOf"] = p.AnyOf
	}
	if len(p.AllOf) > 0 {
		m["allOf"] = p.AllOf
	}
	if len(p.OneOf) > 0 {
		m["oneOf"] = p.OneOf
	}

	return json.Marshal(m)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// paramToSchema 将单个 Param 转换为 JSON Schema 字典。
func paramToSchema(p *Param) map[string]any {
	s := map[string]any{}

	// type：Nullable=true 时输出 ["xxx", "null"]，否则输出字符串
	if p.Nullable {
		s["type"] = []string{p.Type.String(), "null"}
	} else {
		s["type"] = p.Type.String()
	}

	if p.Description != "" {
		s["description"] = p.Description
	}
	if p.Default != nil {
		s["default"] = p.Default
	}
	if len(p.Enum) > 0 {
		s["enum"] = p.Enum
	}
	// 输出约束字段（NaN 表示未设置，不输出；0 是合法值需输出）
	if p.MinLength > 0 {
		s["minLength"] = p.MinLength
	}
	if p.MaxLength > 0 {
		s["maxLength"] = p.MaxLength
	}
	if p.Pattern != "" {
		s["pattern"] = p.Pattern
	}
	if !math.IsNaN(p.Minimum) {
		s["minimum"] = p.Minimum
	}
	if !math.IsNaN(p.Maximum) {
		s["maximum"] = p.Maximum
	}
	if p.Format != "" {
		s["format"] = p.Format
	}
	switch p.Type {
	case ParamTypeArray:
		if p.Items != nil {
			s["items"] = paramToSchema(p.Items)
		}
		if p.MinItems > 0 {
			s["minItems"] = p.MinItems
		}
		if p.MaxItems > 0 {
			s["maxItems"] = p.MaxItems
		}
	case ParamTypeObject:
		if p.AdditionalProperties {
			s["additionalProperties"] = true
		}
		if len(p.Properties) > 0 {
			objProps := make(map[string]any, len(p.Properties))
			var objRequired []string
			for _, prop := range p.Properties {
				objProps[prop.Name] = paramToSchema(prop)
				if prop.Required {
					objRequired = append(objRequired, prop.Name)
				}
			}
			s["properties"] = objProps
			if len(objRequired) > 0 {
				s["required"] = objRequired
			}
		}
	}
	// 输出组合 Schema 关键字（JSON Schema 标准）
	if len(p.AnyOf) > 0 {
		items := make([]any, 0, len(p.AnyOf))
		for _, sub := range p.AnyOf {
			items = append(items, paramToSchema(sub))
		}
		s["anyOf"] = items
	}
	if len(p.AllOf) > 0 {
		items := make([]any, 0, len(p.AllOf))
		for _, sub := range p.AllOf {
			items = append(items, paramToSchema(sub))
		}
		s["allOf"] = items
	}
	if len(p.OneOf) > 0 {
		items := make([]any, 0, len(p.OneOf))
		for _, sub := range p.OneOf {
			items = append(items, paramToSchema(sub))
		}
		s["oneOf"] = items
	}
	return s
}

// ParseJSONSchemaMap 将 JSON Schema dict 转换为 []*Param。
//
// 输入格式为 MetadataProvider.GetInputParams() 返回的 map[string]any，
// 即标准 JSON Schema object 定义。
//
// 对应 Python: 无直接等价物（Python ToolCard.input_params 直接用 Dict[str, Any]）。
// Go 因 ToolCard.InputParams 类型为 []*Param 需要此转换。
func ParseJSONSchemaMap(schemaMap map[string]any) ([]*Param, error) {
	// 1. 校验顶层 type == "object"
	typ, _ := schemaMap["type"].(string)
	if typ != "object" {
		return nil, fmt.Errorf("schema must have type 'object', got %q", typ)
	}

	// 2. 提取 properties
	propsVal, ok := schemaMap["properties"].(map[string]any)
	if !ok || propsVal == nil {
		return nil, nil
	}

	// 3. 提取 required set（支持 []any 和 []string）
	requiredSet := make(map[string]bool)
	if req, ok := schemaMap["required"]; ok {
		switch r := req.(type) {
		case []any:
			for _, v := range r {
				if s, ok := v.(string); ok {
					requiredSet[s] = true
				}
			}
		case []string:
			for _, s := range r {
				requiredSet[s] = true
			}
		}
	}

	// 4. 遍历 properties，调用 parsePropertyToParam
	params := make([]*Param, 0, len(propsVal))
	for name, prop := range propsVal {
		propMap, ok := prop.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("property %q is not a map", name)
		}
		p, err := parsePropertyToParam(name, propMap, requiredSet)
		if err != nil {
			return nil, err
		}
		params = append(params, p)
	}
	return params, nil
}

// parsePropertyToParam 将单个 JSON Schema 属性定义解析为 *Param。
func parsePropertyToParam(name string, prop map[string]any, requiredSet map[string]bool) (*Param, error) {
	// 1. 解析 type（可选：当有 anyOf/allOf/oneOf 时可省略 type）
	typeStr, hasType := prop["type"].(string)
	var paramType ParamType
	if hasType {
		var ok bool
		paramType, ok = parseParamType(typeStr)
		if !ok {
			return nil, fmt.Errorf("property %q has unsupported type %q", name, typeStr)
		}
	} else {
		// 无 type 字段时，检查是否有组合 schema，默认使用 String 类型
		if _, hasAnyOf := prop["anyOf"]; hasAnyOf {
			paramType = ParamTypeString // anyOf 组合时的占位类型
		} else if _, hasAllOf := prop["allOf"]; hasAllOf {
			paramType = ParamTypeObject // allOf 通常用于 object
		} else if _, hasOneOf := prop["oneOf"]; hasOneOf {
			paramType = ParamTypeString // oneOf 组合时的占位类型
		} else {
			return nil, fmt.Errorf("property %q missing required 'type' field", name)
		}
	}

	p := &Param{Name: name, Type: paramType, Required: requiredSet[name]}

	// 对 integer/number 类型，初始化 Minimum/Maximum 为 NaN（表示未设置）
	// 零值 0 是合法约束值，必须用 NaN 区分"未设置"和"约束为 0"
	if paramType == ParamTypeInteger || paramType == ParamTypeNumber {
		p.Minimum = math.NaN()
		p.Maximum = math.NaN()
	}

	// 2. 解析 description
	if desc, ok := prop["description"].(string); ok {
		p.Description = desc
	}

	// 3. 解析 enum
	if enum, ok := prop["enum"].([]any); ok {
		p.Enum = enum
	}

	// 4. 解析 default
	if def, ok := prop["default"]; ok {
		p.Default = def
	}

	// 5. 解析 nullable
	if nullable, ok := prop["nullable"].(bool); ok {
		p.Nullable = nullable
	}

	// 6. 解析数值/字符串约束
	if min, ok := prop["minimum"]; ok {
		p.Minimum = toFloat64(min)
	}
	if max, ok := prop["maximum"]; ok {
		p.Maximum = toFloat64(max)
	}
	if minLen, ok := toInt(prop["minLength"]); ok {
		p.MinLength = minLen
	}
	if maxLen, ok := toInt(prop["maxLength"]); ok {
		p.MaxLength = maxLen
	}
	if pattern, ok := prop["pattern"].(string); ok {
		p.Pattern = pattern
	}
	if format, ok := prop["format"].(string); ok {
		p.Format = format
	}

	// 7. 解析 array 约束
	if paramType == ParamTypeArray {
		if items, ok := prop["items"].(map[string]any); ok {
			item, err := parsePropertyToParam("item", items, nil)
			if err != nil {
				return nil, fmt.Errorf("property %q items: %w", name, err)
			}
			p.Items = item
		}
		if mi, ok := toInt(prop["minItems"]); ok {
			p.MinItems = mi
		}
		if mi, ok := toInt(prop["maxItems"]); ok {
			p.MaxItems = mi
		}
	}

	// 8. 解析 object 约束
	if paramType == ParamTypeObject {
		if ap, ok := prop["additionalProperties"].(bool); ok {
			p.AdditionalProperties = ap
		}
		if nestedProps, ok := prop["properties"].(map[string]any); ok {
			nestedRequired := make(map[string]bool)
			if req, ok2 := prop["required"]; ok2 {
				switch r := req.(type) {
				case []any:
					for _, v := range r {
						if s, ok2 := v.(string); ok2 {
							nestedRequired[s] = true
						}
					}
				case []string:
					for _, s := range r {
						nestedRequired[s] = true
					}
				}
			}
			props := make([]*Param, 0, len(nestedProps))
			for propName, propDef := range nestedProps {
				propMap, ok := propDef.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("property %q.%q is not a map", name, propName)
				}
				nested, err := parsePropertyToParam(propName, propMap, nestedRequired)
				if err != nil {
					return nil, fmt.Errorf("property %q.%q: %w", name, propName, err)
				}
				props = append(props, nested)
			}
			p.Properties = props
		}
	}

	// 9. 解析组合 schema
	for _, keyword := range []string{"anyOf", "allOf", "oneOf"} {
		if subs, ok := prop[keyword].([]any); ok {
			subParams := make([]*Param, 0, len(subs))
			for i, sub := range subs {
				subMap, ok := sub.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("property %q %s[%d] is not a map", name, keyword, i)
				}
				sp, err := parsePropertyToParam(fmt.Sprintf("%s_%d", keyword, i), subMap, nil)
				if err != nil {
					return nil, fmt.Errorf("property %q %s[%d]: %w", name, keyword, i, err)
				}
				subParams = append(subParams, sp)
			}
			switch keyword {
			case "anyOf":
				p.AnyOf = subParams
			case "allOf":
				p.AllOf = subParams
			case "oneOf":
				p.OneOf = subParams
			}
		}
	}

	return p, nil
}

// parseParamType 将 JSON Schema 类型字符串转换为 ParamType。
func parseParamType(typeStr string) (ParamType, bool) {
	switch typeStr {
	case "string":
		return ParamTypeString, true
	case "boolean":
		return ParamTypeBoolean, true
	case "integer":
		return ParamTypeInteger, true
	case "number":
		return ParamTypeNumber, true
	case "array":
		return ParamTypeArray, true
	case "object":
		return ParamTypeObject, true
	default:
		return ParamType(-1), false
	}
}

// toFloat64 将 any 值转换为 float64，支持 int/float64/float32 等数值类型。
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	default:
		return math.NaN()
	}
}

// toInt 将 any 值转换为 int，支持 int/float64（整数）等类型。
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

// init 初始化 paramTypeMap 映射表
func init() {
	// 初始化 paramTypeMap，用于 JSON 反序列化
	paramTypeMap = make(map[string]ParamType, len(paramTypeStrings))
	for i, s := range paramTypeStrings {
		paramTypeMap[s] = ParamType(i)
	}
}
