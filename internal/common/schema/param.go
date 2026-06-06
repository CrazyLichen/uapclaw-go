package schema

import (
	"encoding/json"
	"fmt"
	"strings"
)

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

// paramTypeStrings ParamType 枚举值对应的字符串表示，与 Python 端保持一致。
var paramTypeStrings = [...]string{
	"string",
	"boolean",
	"integer",
	"number",
	"array",
	"object",
}

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
	// Items 数组元素类型定义（仅 Array 类型使用）
	Items *Param `json:"items,omitempty"`
	// Properties 对象属性列表（仅 Object 类型使用）
	Properties []*Param `json:"properties,omitempty"`
}

// ──────────────────────────── 常量 ────────────────────────────

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
		return p.Items.Validate()

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
		return nil

	default:
		if p.Items != nil {
			return fmt.Errorf("Param %q: %s 类型不应有 items 字段", p.Name, p.Type)
		}
		if len(p.Properties) > 0 {
			return fmt.Errorf("Param %q: %s 类型不应有 properties 字段", p.Name, p.Type)
		}
		return nil
	}
}

// String 实现 fmt.Stringer 接口，返回参数的简洁描述。
func (p *Param) String() string {
	return fmt.Sprintf("%s(%s, required=%v)", p.Name, p.Type, p.Required)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 初始化 paramTypeMap，用于 JSON 反序列化
	paramTypeMap = make(map[string]ParamType, len(paramTypeStrings))
	for i, s := range paramTypeStrings {
		paramTypeMap[s] = ParamType(i)
	}
}
