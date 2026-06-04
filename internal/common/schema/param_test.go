package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestNewStringParam(t *testing.T) {
	p := NewStringParam("city", "城市名", true)
	if p.Name != "city" {
		t.Errorf("期望 Name %q，实际 %q", "city", p.Name)
	}
	if p.Type != ParamTypeString {
		t.Errorf("期望 Type %v，实际 %v", ParamTypeString, p.Type)
	}
	if !p.Required {
		t.Error("期望 Required 为 true")
	}
	if p.Default != nil {
		t.Errorf("期望 Default 为 nil，实际 %v", p.Default)
	}
}

func TestNewStringParam_WithDefault(t *testing.T) {
	p := NewStringParam("city", "城市名", false, "Beijing")
	if p.Default != "Beijing" {
		t.Errorf("期望 Default %q，实际 %v", "Beijing", p.Default)
	}
}

func TestNewBooleanParam(t *testing.T) {
	p := NewBooleanParam("verbose", "详细输出", true)
	if p.Type != ParamTypeBoolean {
		t.Errorf("期望 Type %v，实际 %v", ParamTypeBoolean, p.Type)
	}
}

func TestNewIntegerParam(t *testing.T) {
	p := NewIntegerParam("limit", "数量限制", false, 10)
	if p.Type != ParamTypeInteger {
		t.Errorf("期望 Type %v，实际 %v", ParamTypeInteger, p.Type)
	}
	if p.Default != 10 {
		t.Errorf("期望 Default 10，实际 %v", p.Default)
	}
}

func TestNewNumberParam(t *testing.T) {
	p := NewNumberParam("threshold", "阈值", false, 0.95)
	if p.Type != ParamTypeNumber {
		t.Errorf("期望 Type %v，实际 %v", ParamTypeNumber, p.Type)
	}
	if p.Default != 0.95 {
		t.Errorf("期望 Default 0.95，实际 %v", p.Default)
	}
}

func TestNewArrayParam(t *testing.T) {
	items := NewStringParam("tag", "标签", true)
	p := NewArrayParam("tags", "标签列表", false, items)
	if p.Type != ParamTypeArray {
		t.Errorf("期望 Type %v，实际 %v", ParamTypeArray, p.Type)
	}
	if p.Items == nil {
		t.Error("Array 参数的 Items 不应为 nil")
	}
	if p.Items.Name != "tag" {
		t.Errorf("Items.Name 期望 %q，实际 %q", "tag", p.Items.Name)
	}
}

func TestNewObjectParam(t *testing.T) {
	props := []*Param{
		NewStringParam("username", "用户名", true),
		NewIntegerParam("age", "年龄", false),
	}
	p := NewObjectParam("user", "用户信息", true, props)
	if p.Type != ParamTypeObject {
		t.Errorf("期望 Type %v，实际 %v", ParamTypeObject, p.Type)
	}
	if len(p.Properties) != 2 {
		t.Errorf("期望 2 个 Properties，实际 %d", len(p.Properties))
	}
}

func TestParam_Validate_ArrayWithoutItems(t *testing.T) {
	p := &Param{
		Name:        "tags",
		Description: "标签列表",
		Type:        ParamTypeArray,
		Required:    false,
	}
	err := p.Validate()
	if err == nil {
		t.Error("Array 缺少 items 应返回错误")
	}
}

func TestParam_Validate_ArrayWithProperties(t *testing.T) {
	p := &Param{
		Name:        "tags",
		Description: "标签列表",
		Type:        ParamTypeArray,
		Required:    false,
		Items:       NewStringParam("tag", "标签", true),
		Properties:  []*Param{NewStringParam("x", "x", true)},
	}
	err := p.Validate()
	if err == nil {
		t.Error("Array 有 properties 应返回错误")
	}
}

func TestParam_Validate_ObjectWithoutProperties(t *testing.T) {
	p := &Param{
		Name:        "user",
		Description: "用户信息",
		Type:        ParamTypeObject,
		Required:    true,
	}
	err := p.Validate()
	if err == nil {
		t.Error("Object 缺少 properties 应返回错误")
	}
}

func TestParam_Validate_ObjectWithItems(t *testing.T) {
	p := &Param{
		Name:        "user",
		Description: "用户信息",
		Type:        ParamTypeObject,
		Required:    true,
		Items:       NewStringParam("x", "x", true),
		Properties:  []*Param{NewStringParam("name", "名称", true)},
	}
	err := p.Validate()
	if err == nil {
		t.Error("Object 有 items 应返回错误")
	}
}

func TestParam_Validate_SimpleWithItems(t *testing.T) {
	p := &Param{
		Name:        "city",
		Description: "城市名",
		Type:        ParamTypeString,
		Required:    true,
		Items:       NewStringParam("x", "x", true),
	}
	err := p.Validate()
	if err == nil {
		t.Error("String 类型有 items 应返回错误")
	}
}

func TestParam_Validate_SimpleWithProperties(t *testing.T) {
	p := &Param{
		Name:        "city",
		Description: "城市名",
		Type:        ParamTypeString,
		Required:    true,
		Properties:  []*Param{NewStringParam("x", "x", true)},
	}
	err := p.Validate()
	if err == nil {
		t.Error("String 类型有 properties 应返回错误")
	}
}

func TestParam_Validate_ValidSimple(t *testing.T) {
	p := NewStringParam("city", "城市名", true)
	if err := p.Validate(); err != nil {
		t.Errorf("合法的简单参数不应报错: %v", err)
	}
}

func TestParam_Validate_ValidArray(t *testing.T) {
	p := NewArrayParam("tags", "标签列表", false, NewStringParam("tag", "标签", true))
	if err := p.Validate(); err != nil {
		t.Errorf("合法的数组参数不应报错: %v", err)
	}
}

func TestParam_Validate_ValidObject(t *testing.T) {
	p := NewObjectParam("user", "用户信息", true, []*Param{
		NewStringParam("name", "名称", true),
		NewIntegerParam("age", "年龄", false),
	})
	if err := p.Validate(); err != nil {
		t.Errorf("合法的对象参数不应报错: %v", err)
	}
}

func TestParam_Validate_NestedObjectInArray(t *testing.T) {
	// 数组元素为对象类型：嵌套验证
	objItems := []*Param{
		NewStringParam("name", "名称", true),
	}
	p := NewArrayParam("users", "用户列表", false,
		NewObjectParam("user", "用户", true, objItems),
	)
	if err := p.Validate(); err != nil {
		t.Errorf("合法的嵌套参数不应报错: %v", err)
	}
}

func TestParam_MarshalJSON(t *testing.T) {
	p := NewStringParam("city", "城市名", true)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	// 验证 type 字段序列化为字符串 "string"
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if raw["type"] != "string" {
		t.Errorf("期望 type=%q，实际 %v", "string", raw["type"])
	}
}

func TestParam_UnmarshalJSON(t *testing.T) {
	jsonStr := `{
		"name": "tags",
		"description": "标签列表",
		"type": "array",
		"required": false,
		"items": {
			"name": "tag",
			"description": "标签",
			"type": "string",
			"required": true
		}
	}`

	var p Param
	if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if p.Type != ParamTypeArray {
		t.Errorf("期望 Type %v，实际 %v", ParamTypeArray, p.Type)
	}
	if p.Items == nil {
		t.Error("Items 不应为 nil")
	}
	if p.Items.Type != ParamTypeString {
		t.Errorf("Items.Type 期望 %v，实际 %v", ParamTypeString, p.Items.Type)
	}
}

func TestParam_UnmarshalJSON_UnknownType(t *testing.T) {
	jsonStr := `{"name":"x","description":"x","type":"unknown","required":true}`
	var p Param
	err := json.Unmarshal([]byte(jsonStr), &p)
	if err == nil {
		t.Error("未知 ParamType 应返回错误")
	}
}

func TestParamType_String(t *testing.T) {
	tests := []struct {
		pt       ParamType
		expected string
	}{
		{ParamTypeString, "string"},
		{ParamTypeBoolean, "boolean"},
		{ParamTypeInteger, "integer"},
		{ParamTypeNumber, "number"},
		{ParamTypeArray, "array"},
		{ParamTypeObject, "object"},
	}
	for _, tt := range tests {
		if got := tt.pt.String(); got != tt.expected {
			t.Errorf("ParamType(%d).String() = %q, 期望 %q", tt.pt, got, tt.expected)
		}
	}
}

func TestParam_String(t *testing.T) {
	p := NewStringParam("city", "城市名", true)
	s := p.String()
	if s != "city(string, required=true)" {
		t.Errorf("期望 %q，实际 %q", "city(string, required=true)", s)
	}
}

func TestParam_NestedStructure(t *testing.T) {
	// 构建一个嵌套对象参数：用户信息（包含字符串和整数属性）
	userParam := NewObjectParam("user", "用户信息", true, []*Param{
		NewStringParam("username", "用户名", true),
		NewIntegerParam("age", "年龄", false),
	})

	// 序列化 → 反序列化，验证嵌套结构保持一致
	data, err := json.Marshal(userParam)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var decoded Param
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	if decoded.Type != ParamTypeObject {
		t.Errorf("期望 Type %v，实际 %v", ParamTypeObject, decoded.Type)
	}
	if len(decoded.Properties) != 2 {
		t.Fatalf("期望 2 个 Properties，实际 %d", len(decoded.Properties))
	}
	if decoded.Properties[0].Name != "username" {
		t.Errorf("Properties[0].Name 期望 %q，实际 %q", "username", decoded.Properties[0].Name)
	}
	if decoded.Properties[0].Type != ParamTypeString {
		t.Errorf("Properties[0].Type 期望 %v，实际 %v", ParamTypeString, decoded.Properties[0].Type)
	}
	if decoded.Properties[1].Name != "age" {
		t.Errorf("Properties[1].Name 期望 %q，实际 %q", "age", decoded.Properties[1].Name)
	}
	if decoded.Properties[1].Type != ParamTypeInteger {
		t.Errorf("Properties[1].Type 期望 %v，实际 %v", ParamTypeInteger, decoded.Properties[1].Type)
	}
}
