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

// ──────────────────────────── 常量 ────────────────────────────

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
		// 递归验证 items
		if err := p.Items.Validate(); err != nil {
			return err
		}

	case ParamTypeObject:
		if len(p.Properties) == 0 {
			return fmt.Errorf("Param %q: Object 类型必须有 properties 字段", p.Name)
		}
		if p.Items != nil {
			return fmt.Errorf("Param %q: Object 类型不应有 items 字段", p.Name)
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
	// 使用别名避免递归调用 MarshalJSON
	type ParamAlias Param
	alias := ParamAlias(*p)

	// 先序列化基本字段
	data, err := json.Marshal(map[string]any{
		"name":        alias.Name,
		"description": alias.Description,
		"type":        alias.Type,
		"required":    alias.Required,
	})
	if err != nil {
		return nil, err
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}

	// 可选字段
	if alias.Default != nil {
		m["default"] = alias.Default
	}
	if len(alias.Enum) > 0 {
		m["enum"] = alias.Enum
	}
	if alias.MinLength > 0 {
		m["minLength"] = alias.MinLength
	}
	if alias.MaxLength > 0 {
		m["maxLength"] = alias.MaxLength
	}
	if alias.Pattern != "" {
		m["pattern"] = alias.Pattern
	}
	if !math.IsNaN(alias.Minimum) {
		m["minimum"] = alias.Minimum
	}
	if !math.IsNaN(alias.Maximum) {
		m["maximum"] = alias.Maximum
	}
	if alias.Format != "" {
		m["format"] = alias.Format
	}
	if alias.Nullable {
		m["nullable"] = alias.Nullable
	}
	if alias.Items != nil {
		m["items"] = alias.Items
	}
	if len(alias.Properties) > 0 {
		m["properties"] = alias.Properties
	}
	if len(alias.AnyOf) > 0 {
		m["anyOf"] = alias.AnyOf
	}
	if len(alias.AllOf) > 0 {
		m["allOf"] = alias.AllOf
	}
	if len(alias.OneOf) > 0 {
		m["oneOf"] = alias.OneOf
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
	case ParamTypeObject:
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

// init 初始化 paramTypeMap 映射表
func init() {
	// 初始化 paramTypeMap，用于 JSON 反序列化
	paramTypeMap = make(map[string]ParamType, len(paramTypeStrings))
	for i, s := range paramTypeStrings {
		paramTypeMap[s] = ParamType(i)
	}
}
