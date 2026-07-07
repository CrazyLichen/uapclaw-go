package schema

import (
	"encoding/json"
	"fmt"
	"math"
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

func TestNewStringParam_带默认值(t *testing.T) {
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

func TestParam_Validate_数组缺少Items(t *testing.T) {
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

func TestParam_Validate_数组有Properties(t *testing.T) {
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

func TestParam_Validate_对象缺少Properties(t *testing.T) {
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

func TestParam_Validate_对象有Items(t *testing.T) {
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

func TestParam_Validate_简单类型有Items(t *testing.T) {
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

func TestParam_Validate_简单类型有Properties(t *testing.T) {
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

func TestParam_Validate_合法简单类型(t *testing.T) {
	p := NewStringParam("city", "城市名", true)
	if err := p.Validate(); err != nil {
		t.Errorf("合法的简单参数不应报错: %v", err)
	}
}

func TestParam_Validate_合法数组(t *testing.T) {
	p := NewArrayParam("tags", "标签列表", false, NewStringParam("tag", "标签", true))
	if err := p.Validate(); err != nil {
		t.Errorf("合法的数组参数不应报错: %v", err)
	}
}

func TestParam_Validate_合法对象(t *testing.T) {
	p := NewObjectParam("user", "用户信息", true, []*Param{
		NewStringParam("name", "名称", true),
		NewIntegerParam("age", "年龄", false),
	})
	if err := p.Validate(); err != nil {
		t.Errorf("合法的对象参数不应报错: %v", err)
	}
}

func TestParam_Validate_数组中嵌套对象(t *testing.T) {
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

func TestParam_UnmarshalJSON_未知类型(t *testing.T) {
	jsonStr := `{"name":"x","description":"x","type":"unknown","required":true}`
	var p Param
	err := json.Unmarshal([]byte(jsonStr), &p)
	if err == nil {
		t.Error("未知 ParamType 应返回错误")
	}
}

func TestParamType_字符串(t *testing.T) {
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

func TestParam_字符串表示(t *testing.T) {
	p := NewStringParam("city", "城市名", true)
	s := p.String()
	if s != "city(string, required=true)" {
		t.Errorf("期望 %q，实际 %q", "city(string, required=true)", s)
	}
}

func TestToJSONSchemaMap_空参数列表(t *testing.T) {
	result := ToJSONSchemaMap(nil)
	if result["type"] != "object" {
		t.Errorf("type = %v, want object", result["type"])
	}
	props, ok := result["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties 类型不是 map[string]any")
	}
	if len(props) != 0 {
		t.Errorf("properties 应为空，实际有 %d 项", len(props))
	}
	if _, hasRequired := result["required"]; hasRequired {
		t.Error("空参数列表不应有 required 字段")
	}
}

func TestToJSONSchemaMap_简单参数(t *testing.T) {
	params := []*Param{
		NewStringParam("city", "城市名", true),
		NewIntegerParam("count", "数量", false),
	}
	result := ToJSONSchemaMap(params)

	if result["type"] != "object" {
		t.Errorf("type = %v, want object", result["type"])
	}

	props := result["properties"].(map[string]any)
	citySchema := props["city"].(map[string]any)
	if citySchema["type"] != "string" {
		t.Errorf("city type = %v, want string", citySchema["type"])
	}
	if citySchema["description"] != "城市名" {
		t.Errorf("city description = %v, want 城市名", citySchema["description"])
	}

	required := result["required"].([]string)
	if len(required) != 1 || required[0] != "city" {
		t.Errorf("required = %v, want [city]", required)
	}
}

func TestToJSONSchemaMap_嵌套对象参数(t *testing.T) {
	params := []*Param{
		NewObjectParam("config", "配置", true, []*Param{
			NewStringParam("host", "主机", true),
			NewIntegerParam("port", "端口", false, 8080),
		}),
	}
	result := ToJSONSchemaMap(params)
	props := result["properties"].(map[string]any)
	configSchema := props["config"].(map[string]any)
	if configSchema["type"] != "object" {
		t.Errorf("config type = %v, want object", configSchema["type"])
	}
	innerProps := configSchema["properties"].(map[string]any)
	hostSchema := innerProps["host"].(map[string]any)
	if hostSchema["type"] != "string" {
		t.Errorf("host type = %v, want string", hostSchema["type"])
	}
}

func TestToJSONSchemaMap_数组参数(t *testing.T) {
	params := []*Param{
		NewArrayParam("tags", "标签", false, NewStringParam("", "", false)),
	}
	result := ToJSONSchemaMap(params)
	props := result["properties"].(map[string]any)
	tagsSchema := props["tags"].(map[string]any)
	if tagsSchema["type"] != "array" {
		t.Errorf("tags type = %v, want array", tagsSchema["type"])
	}
	itemsSchema := tagsSchema["items"].(map[string]any)
	if itemsSchema["type"] != "string" {
		t.Errorf("items type = %v, want string", itemsSchema["type"])
	}
}

func TestParam_嵌套结构(t *testing.T) {
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

func TestParam_Nullable输出Type数组(t *testing.T) {
	p := &Param{
		Name:        "email",
		Type:        ParamTypeString,
		Description: "邮箱地址",
		Required:    false,
		Nullable:    true,
	}
	schema := ToJSONSchemaMap([]*Param{p})
	props := schema["properties"].(map[string]any)
	emailSchema := props["email"].(map[string]any)

	// Nullable=true 时 type 应输出 ["string", "null"]
	typeVal, ok := emailSchema["type"].([]string)
	if !ok {
		t.Fatalf("期望 type 为 []string，实际 %T", emailSchema["type"])
	}
	if len(typeVal) != 2 || typeVal[0] != "string" || typeVal[1] != "null" {
		t.Errorf("期望 type [string null]，实际 %v", typeVal)
	}
}

func TestParam_Nullable为false输出Type字符串(t *testing.T) {
	p := NewStringParam("name", "名称", true)
	schema := ToJSONSchemaMap([]*Param{p})
	props := schema["properties"].(map[string]any)
	nameSchema := props["name"].(map[string]any)

	// Nullable=false（默认）时 type 应输出字符串
	typeStr, ok := nameSchema["type"].(string)
	if !ok {
		t.Fatalf("期望 type 为 string，实际 %T", nameSchema["type"])
	}
	if typeStr != "string" {
		t.Errorf("期望 type string，实际 %v", typeStr)
	}
}

func TestParam_AnyOf输出(t *testing.T) {
	p := &Param{
		Name:        "value",
		Type:        ParamTypeString,
		Description: "可以是字符串或整数",
		Required:    false,
		AnyOf: []*Param{
			{Type: ParamTypeString},
			{Type: ParamTypeInteger},
		},
	}
	schema := ToJSONSchemaMap([]*Param{p})
	props := schema["properties"].(map[string]any)
	valueSchema := props["value"].(map[string]any)

	anyOf, ok := valueSchema["anyOf"].([]any)
	if !ok {
		t.Fatalf("期望 anyOf 为 []any，实际 %T", valueSchema["anyOf"])
	}
	if len(anyOf) != 2 {
		t.Fatalf("期望 anyOf 有 2 项，实际 %d", len(anyOf))
	}
	first := anyOf[0].(map[string]any)
	if first["type"] != "string" {
		t.Errorf("anyOf[0].type 期望 string，实际 %v", first["type"])
	}
	second := anyOf[1].(map[string]any)
	if second["type"] != "integer" {
		t.Errorf("anyOf[1].type 期望 integer，实际 %v", second["type"])
	}
}

func TestParam_AllOf输出(t *testing.T) {
	p := &Param{
		Name:        "combined",
		Type:        ParamTypeObject,
		Description: "合并对象",
		Required:    true,
		Properties: []*Param{
			NewStringParam("name", "名称", true),
		},
		AllOf: []*Param{
			{
				Type: ParamTypeObject,
				Properties: []*Param{
					NewStringParam("id", "标识", true),
				},
			},
		},
	}
	schema := ToJSONSchemaMap([]*Param{p})
	props := schema["properties"].(map[string]any)
	combinedSchema := props["combined"].(map[string]any)

	allOf, ok := combinedSchema["allOf"].([]any)
	if !ok {
		t.Fatalf("期望 allOf 为 []any，实际 %T", combinedSchema["allOf"])
	}
	if len(allOf) != 1 {
		t.Fatalf("期望 allOf 有 1 项，实际 %d", len(allOf))
	}
}

func TestParam_OneOf输出(t *testing.T) {
	p := &Param{
		Name:        "choice",
		Type:        ParamTypeString,
		Description: "选择",
		Required:    false,
		OneOf: []*Param{
			{Type: ParamTypeString, Enum: []any{"a", "b"}},
			{Type: ParamTypeInteger},
		},
	}
	schema := ToJSONSchemaMap([]*Param{p})
	props := schema["properties"].(map[string]any)
	choiceSchema := props["choice"].(map[string]any)

	oneOf, ok := choiceSchema["oneOf"].([]any)
	if !ok {
		t.Fatalf("期望 oneOf 为 []any，实际 %T", choiceSchema["oneOf"])
	}
	if len(oneOf) != 2 {
		t.Fatalf("期望 oneOf 有 2 项，实际 %d", len(oneOf))
	}
}

func TestParam_Validate_组合Schema递归验证(t *testing.T) {
	// Array 类型带合法 AnyOf
	p := &Param{
		Name:        "data",
		Type:        ParamTypeString,
		Description: "数据",
		Required:    true,
		AnyOf: []*Param{
			NewStringParam("", "", false),
			NewIntegerParam("", "", false),
		},
	}
	if err := p.Validate(); err != nil {
		t.Errorf("带 AnyOf 的合法参数不应报错: %v", err)
	}
}

func TestParam_Validate_组合Schema内嵌无效类型(t *testing.T) {
	// AllOf 内含无效的 Array（缺少 Items）
	p := &Param{
		Name:        "data",
		Type:        ParamTypeString,
		Description: "数据",
		Required:    true,
		AllOf: []*Param{
			{Type: ParamTypeArray}, // 缺少 Items，应该校验失败
		},
	}
	err := p.Validate()
	if err == nil {
		t.Error("AllOf 内含无效子 schema 应返回错误")
	}
}

// TestParam_Minimum零值可输出 验证 Minimum=0 能正确输出到 JSON Schema。
func TestParam_Minimum零值可输出(t *testing.T) {
	p := NewNumberParam("score", "分数", true)
	p.Minimum = 0
	p.Maximum = 100

	schema := ToJSONSchemaMap([]*Param{p})
	props := schema["properties"].(map[string]any)
	scoreSchema := props["score"].(map[string]any)

	// Minimum=0 应输出到 JSON Schema
	if minVal, ok := scoreSchema["minimum"]; !ok {
		t.Error("Minimum=0 未输出到 JSON Schema")
	} else if minVal != float64(0) {
		t.Errorf("minimum = %v, 期望 0", minVal)
	}

	// Maximum=100 应正常输出
	if maxVal, ok := scoreSchema["maximum"]; !ok {
		t.Error("Maximum=100 未输出到 JSON Schema")
	} else if maxVal != float64(100) {
		t.Errorf("maximum = %v, 期望 100", maxVal)
	}
}

// TestParam_MinimumNaN不输出 验证 Minimum 为 NaN 时不输出到 JSON Schema。
func TestParam_MinimumNaN不输出(t *testing.T) {
	p := NewIntegerParam("count", "数量", true)
	// NewIntegerParam 默认 Minimum=NaN

	schema := ToJSONSchemaMap([]*Param{p})
	props := schema["properties"].(map[string]any)
	countSchema := props["count"].(map[string]any)

	// NaN 的 Minimum 不应输出
	if _, ok := countSchema["minimum"]; ok {
		t.Error("NaN 的 Minimum 不应输出到 JSON Schema")
	}
	if _, ok := countSchema["maximum"]; ok {
		t.Error("NaN 的 Maximum 不应输出到 JSON Schema")
	}
}

// TestNewIntegerParam_MinimumMaximum默认NaN 验证工厂方法初始化 NaN。
func TestNewIntegerParam_MinimumMaximum默认NaN(t *testing.T) {
	p := NewIntegerParam("x", "x", false)
	if !math.IsNaN(p.Minimum) {
		t.Errorf("NewIntegerParam Minimum = %v, 期望 NaN", p.Minimum)
	}
	if !math.IsNaN(p.Maximum) {
		t.Errorf("NewIntegerParam Maximum = %v, 期望 NaN", p.Maximum)
	}
}

// TestNewNumberParam_MinimumMaximum默认NaN 验证工厂方法初始化 NaN。
func TestNewNumberParam_MinimumMaximum默认NaN(t *testing.T) {
	p := NewNumberParam("x", "x", false)
	if !math.IsNaN(p.Minimum) {
		t.Errorf("NewNumberParam Minimum = %v, 期望 NaN", p.Minimum)
	}
	if !math.IsNaN(p.Maximum) {
		t.Errorf("NewNumberParam Maximum = %v, 期望 NaN", p.Maximum)
	}
}

// ──────────────────────────── AdditionalProperties / MinItems / MaxItems 测试 ────────────────────────────

func TestParam_AdditionalProperties_Object合法(t *testing.T) {
	p := &Param{
		Name:                 "config",
		Type:                 ParamTypeObject,
		Description:          "配置对象",
		Required:             true,
		AdditionalProperties: true,
		Properties: []*Param{
			NewStringParam("key", "键", true),
		},
	}
	if err := p.Validate(); err != nil {
		t.Errorf("Object 类型设置 AdditionalProperties=true 不应报错: %v", err)
	}
}

func TestParam_AdditionalProperties_非Object非法(t *testing.T) {
	p := &Param{
		Name:                 "name",
		Type:                 ParamTypeString,
		Description:          "名称",
		Required:             true,
		AdditionalProperties: true,
	}
	err := p.Validate()
	if err == nil {
		t.Error("String 类型设置 AdditionalProperties 应返回错误")
	}
}

func TestParam_MinItemsMaxItems_Array合法(t *testing.T) {
	p := &Param{
		Name:        "tags",
		Type:        ParamTypeArray,
		Description: "标签列表",
		Required:    true,
		Items:       NewStringParam("tag", "标签", true),
		MinItems:    1,
		MaxItems:    10,
	}
	if err := p.Validate(); err != nil {
		t.Errorf("Array 类型设置 MinItems/MaxItems 不应报错: %v", err)
	}
}

func TestParam_MinItemsMaxItems_非Array非法(t *testing.T) {
	p := &Param{
		Name:        "name",
		Type:        ParamTypeString,
		Description: "名称",
		Required:    true,
		MinItems:    1,
	}
	err := p.Validate()
	if err == nil {
		t.Error("String 类型设置 MinItems 应返回错误")
	}

	p2 := &Param{
		Name:        "name",
		Type:        ParamTypeString,
		Description: "名称",
		Required:    true,
		MaxItems:    5,
	}
	err2 := p2.Validate()
	if err2 == nil {
		t.Error("String 类型设置 MaxItems 应返回错误")
	}
}

func TestParamToSchemaMap_AdditionalProperties输出(t *testing.T) {
	p := &Param{
		Name:                 "config",
		Type:                 ParamTypeObject,
		Description:          "配置对象",
		Required:             true,
		AdditionalProperties: true,
		Properties: []*Param{
			NewStringParam("key", "键", true),
		},
	}
	schema := ToJSONSchemaMap([]*Param{p})
	props := schema["properties"].(map[string]any)
	configSchema := props["config"].(map[string]any)

	apVal, ok := configSchema["additionalProperties"]
	if !ok {
		t.Error("additionalProperties 未输出到 JSON Schema")
	} else if apVal != true {
		t.Errorf("additionalProperties = %v, 期望 true", apVal)
	}
}

func TestParamToSchemaMap_MinItemsMaxItems输出(t *testing.T) {
	p := &Param{
		Name:        "tags",
		Type:        ParamTypeArray,
		Description: "标签列表",
		Required:    true,
		Items:       NewStringParam("tag", "标签", true),
		MinItems:    1,
		MaxItems:    20,
	}
	schema := ToJSONSchemaMap([]*Param{p})
	props := schema["properties"].(map[string]any)
	tagsSchema := props["tags"].(map[string]any)

	if mi, ok := tagsSchema["minItems"]; !ok {
		t.Error("minItems 未输出到 JSON Schema")
	} else if mi != 1 {
		t.Errorf("minItems = %v, 期望 1", mi)
	}

	if mi, ok := tagsSchema["maxItems"]; !ok {
		t.Error("maxItems 未输出到 JSON Schema")
	} else if mi != 20 {
		t.Errorf("maxItems = %v, 期望 20", mi)
	}
}

func TestParam_Validate_Object仅有AdditionalProperties合法(t *testing.T) {
	// Object 类型可以仅有 AdditionalProperties=true 而无 Properties
	p := &Param{
		Name:                 "extra",
		Type:                 ParamTypeObject,
		Description:          "额外属性对象",
		Required:             true,
		AdditionalProperties: true,
	}
	if err := p.Validate(); err != nil {
		t.Errorf("Object 仅有 AdditionalProperties=true 不应报错: %v", err)
	}
}

func TestParam_Validate_Array有AdditionalProperties非法(t *testing.T) {
	p := &Param{
		Name:                 "arr",
		Type:                 ParamTypeArray,
		Description:          "数组",
		Required:             true,
		Items:                NewStringParam("item", "元素", true),
		AdditionalProperties: true,
	}
	err := p.Validate()
	if err == nil {
		t.Error("Array 类型设置 AdditionalProperties 应返回错误")
	}
}

// ──────────────────────────── ParseJSONSchemaMap 测试 ────────────────────────────

func TestParseJSONSchemaMap_空对象(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{}, "required": []any{}}
	params, err := ParseJSONSchemaMap(schema)
	if err != nil {
		t.Fatalf("解析空对象失败: %v", err)
	}
	if len(params) != 0 {
		t.Errorf("期望 0 个参数，实际 %d", len(params))
	}
}

func TestParseJSONSchemaMap_简单参数(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":  map[string]any{"type": "string", "description": "名称"},
			"count": map[string]any{"type": "integer", "description": "数量"},
		},
		"required": []any{"name"},
	}
	params, err := ParseJSONSchemaMap(schema)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(params) != 2 {
		t.Fatalf("期望 2 个参数，实际 %d", len(params))
	}

	// 找 name 参数
	var nameParam *Param
	for _, p := range params {
		if p.Name == "name" {
			nameParam = p
			break
		}
	}
	if nameParam == nil {
		t.Fatal("未找到 name 参数")
	}
	if nameParam.Type != ParamTypeString {
		t.Errorf("name.Type 期望 %v，实际 %v", ParamTypeString, nameParam.Type)
	}
	if !nameParam.Required {
		t.Error("name 应为 required")
	}

	// 找 count 参数
	var countParam *Param
	for _, p := range params {
		if p.Name == "count" {
			countParam = p
			break
		}
	}
	if countParam == nil {
		t.Fatal("未找到 count 参数")
	}
	if countParam.Type != ParamTypeInteger {
		t.Errorf("count.Type 期望 %v，实际 %v", ParamTypeInteger, countParam.Type)
	}
	if countParam.Required {
		t.Error("count 不应为 required")
	}
}

func TestParseJSONSchemaMap_属性缺type报错(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"bad": map[string]any{"description": "没有type"},
		},
	}
	_, err := ParseJSONSchemaMap(schema)
	if err == nil {
		t.Error("属性缺 type 应返回错误")
	}
	if err != nil && !containsStr(err.Error(), "missing required 'type' field") {
		t.Errorf("错误信息应包含 'missing required type field'，实际: %v", err)
	}
}

func TestParseJSONSchemaMap_布尔和数组参数(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"verbose": map[string]any{"type": "boolean", "description": "详细模式"},
			"tags": map[string]any{
				"type":        "array",
				"description": "标签列表",
				"items":       map[string]any{"type": "string", "description": "标签"},
			},
		},
	}
	params, err := ParseJSONSchemaMap(schema)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(params) != 2 {
		t.Fatalf("期望 2 个参数，实际 %d", len(params))
	}

	for _, p := range params {
		if p.Name == "verbose" && p.Type != ParamTypeBoolean {
			t.Errorf("verbose.Type 期望 %v，实际 %v", ParamTypeBoolean, p.Type)
		}
		if p.Name == "tags" {
			if p.Type != ParamTypeArray {
				t.Errorf("tags.Type 期望 %v，实际 %v", ParamTypeArray, p.Type)
			}
			if p.Items == nil {
				t.Error("tags.Items 不应为 nil")
			} else if p.Items.Type != ParamTypeString {
				t.Errorf("tags.Items.Type 期望 %v，实际 %v", ParamTypeString, p.Items.Type)
			}
		}
	}
}

func TestParseJSONSchemaMap_enum枚举(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "操作",
				"enum":        []any{"add", "remove", "list"},
			},
		},
		"required": []any{"action"},
	}
	params, err := ParseJSONSchemaMap(schema)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(params) != 1 {
		t.Fatalf("期望 1 个参数，实际 %d", len(params))
	}
	if len(params[0].Enum) != 3 {
		t.Errorf("期望 3 个枚举值，实际 %d", len(params[0].Enum))
	}
}

func TestParseJSONSchemaMap_default默认值(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit":   map[string]any{"type": "integer", "description": "数量限制", "default": 10},
			"enabled": map[string]any{"type": "boolean", "description": "是否启用", "default": true},
			"name":    map[string]any{"type": "string", "description": "名称", "default": "default"},
		},
	}
	params, err := ParseJSONSchemaMap(schema)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	for _, p := range params {
		switch p.Name {
		case "limit":
			if p.Default != 10 {
				t.Errorf("limit.Default 期望 10，实际 %v", p.Default)
			}
		case "enabled":
			if p.Default != true {
				t.Errorf("enabled.Default 期望 true，实际 %v", p.Default)
			}
		case "name":
			if p.Default != "default" {
				t.Errorf("name.Default 期望 'default'，实际 %v", p.Default)
			}
		}
	}
}

func TestParseJSONSchemaMap_约束字段(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"age":     map[string]any{"type": "integer", "minimum": 0, "maximum": 150},
			"pattern": map[string]any{"type": "string", "minLength": 1, "maxLength": 100, "pattern": "^[a-z]+$"},
			"email":   map[string]any{"type": "string", "format": "email"},
		},
	}
	params, err := ParseJSONSchemaMap(schema)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	for _, p := range params {
		switch p.Name {
		case "age":
			if p.Minimum != 0 {
				t.Errorf("age.Minimum 期望 0，实际 %v", p.Minimum)
			}
			if p.Maximum != 150 {
				t.Errorf("age.Maximum 期望 150，实际 %v", p.Maximum)
			}
		case "pattern":
			if p.MinLength != 1 {
				t.Errorf("pattern.MinLength 期望 1，实际 %d", p.MinLength)
			}
			if p.MaxLength != 100 {
				t.Errorf("pattern.MaxLength 期望 100，实际 %d", p.MaxLength)
			}
			if p.Pattern != "^[a-z]+$" {
				t.Errorf("pattern.Pattern 期望 '^[a-z]+$'，实际 %q", p.Pattern)
			}
		case "email":
			if p.Format != "email" {
				t.Errorf("email.Format 期望 'email'，实际 %q", p.Format)
			}
		}
	}
}

func TestParseJSONSchemaMap_嵌套object(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"config": map[string]any{
				"type":                 "object",
				"description":          "配置",
				"additionalProperties": true,
				"properties": map[string]any{
					"host": map[string]any{"type": "string", "description": "主机"},
					"port": map[string]any{"type": "integer", "description": "端口", "default": 8080},
				},
				"required": []any{"host"},
			},
		},
	}
	params, err := ParseJSONSchemaMap(schema)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(params) != 1 {
		t.Fatalf("期望 1 个参数，实际 %d", len(params))
	}
	p := params[0]
	if p.Type != ParamTypeObject {
		t.Errorf("config.Type 期望 %v，实际 %v", ParamTypeObject, p.Type)
	}
	if !p.AdditionalProperties {
		t.Error("config.AdditionalProperties 期望 true")
	}
	if len(p.Properties) != 2 {
		t.Fatalf("期望 2 个嵌套属性，实际 %d", len(p.Properties))
	}

	// 检查 host 是否为 required
	var hostParam *Param
	for _, prop := range p.Properties {
		if prop.Name == "host" {
			hostParam = prop
			break
		}
	}
	if hostParam == nil {
		t.Fatal("未找到 host 属性")
	}
	if !hostParam.Required {
		t.Error("host 应为 required")
	}
}

func TestParseJSONSchemaMap_数组items嵌套(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"users": map[string]any{
				"type":        "array",
				"description": "用户列表",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string", "description": "用户名"},
					},
					"required": []any{"name"},
				},
			},
		},
	}
	params, err := ParseJSONSchemaMap(schema)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(params) != 1 {
		t.Fatalf("期望 1 个参数，实际 %d", len(params))
	}
	p := params[0]
	if p.Type != ParamTypeArray {
		t.Errorf("users.Type 期望 %v，实际 %v", ParamTypeArray, p.Type)
	}
	if p.Items == nil {
		t.Fatal("users.Items 不应为 nil")
	}
	if p.Items.Type != ParamTypeObject {
		t.Errorf("items.Type 期望 %v，实际 %v", ParamTypeObject, p.Items.Type)
	}
	if len(p.Items.Properties) != 1 {
		t.Fatalf("期望 1 个嵌套属性，实际 %d", len(p.Items.Properties))
	}
}

func TestParseJSONSchemaMap_additionalProperties(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"extra": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
				"properties":           map[string]any{},
			},
		},
	}
	params, err := ParseJSONSchemaMap(schema)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !params[0].AdditionalProperties {
		t.Error("extra.AdditionalProperties 期望 true")
	}
}

func TestParseJSONSchemaMap_minItemsMaxItems(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tags": map[string]any{
				"type":     "array",
				"minItems": 1,
				"maxItems": 10,
				"items":    map[string]any{"type": "string"},
			},
		},
	}
	params, err := ParseJSONSchemaMap(schema)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if params[0].MinItems != 1 {
		t.Errorf("tags.MinItems 期望 1，实际 %d", params[0].MinItems)
	}
	if params[0].MaxItems != 10 {
		t.Errorf("tags.MaxItems 期望 10，实际 %d", params[0].MaxItems)
	}
}

func TestParseJSONSchemaMap_anyOf组合(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"value": map[string]any{
				"description": "可以是字符串或整数",
				"anyOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "integer"},
				},
			},
		},
	}
	params, err := ParseJSONSchemaMap(schema)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(params[0].AnyOf) != 2 {
		t.Fatalf("期望 2 个 AnyOf，实际 %d", len(params[0].AnyOf))
	}
	if params[0].AnyOf[0].Type != ParamTypeString {
		t.Errorf("anyOf[0].Type 期望 %v，实际 %v", ParamTypeString, params[0].AnyOf[0].Type)
	}
}

func TestParseJSONSchemaMap_顶层非object报错(t *testing.T) {
	schema := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	_, err := ParseJSONSchemaMap(schema)
	if err == nil {
		t.Error("顶层 type 非 object 应返回错误")
	}
}

func TestParseJSONSchemaMap_无properties返回空(t *testing.T) {
	schema := map[string]any{"type": "object"}
	params, err := ParseJSONSchemaMap(schema)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(params) != 0 {
		t.Errorf("期望 0 个参数，实际 %d", len(params))
	}
}

func TestParseJSONSchemaMap_RoundTrip(t *testing.T) {
	// 验证 ParseJSONSchemaMap(ToJSONSchemaMap(params)) 逻辑等价
	originalParams := []*Param{
		NewStringParam("query", "搜索查询", true),
		{
			Name:        "limit",
			Type:        ParamTypeInteger,
			Description: "数量限制",
			Required:    false,
			Default:     10,
			Minimum:     1,
			Maximum:     100,
		},
	}

	schemaMap := ToJSONSchemaMap(originalParams)
	parsedParams, err := ParseJSONSchemaMap(schemaMap)
	if err != nil {
		t.Fatalf("Round-trip 解析失败: %v", err)
	}

	if len(parsedParams) != len(originalParams) {
		t.Fatalf("Round-trip 参数数量不一致: 期望 %d，实际 %d", len(originalParams), len(parsedParams))
	}

	// 按 name 查找比较
	findByName := func(params []*Param, name string) *Param {
		for _, p := range params {
			if p.Name == name {
				return p
			}
		}
		return nil
	}

	for _, orig := range originalParams {
		parsed := findByName(parsedParams, orig.Name)
		if parsed == nil {
			t.Errorf("Round-trip 未找到参数 %q", orig.Name)
			continue
		}
		if parsed.Type != orig.Type {
			t.Errorf("Round-trip %q.Type 期望 %v，实际 %v", orig.Name, orig.Type, parsed.Type)
		}
		if parsed.Required != orig.Required {
			t.Errorf("Round-trip %q.Required 期望 %v，实际 %v", orig.Name, orig.Required, parsed.Required)
		}
		if parsed.Description != orig.Description {
			t.Errorf("Round-trip %q.Description 期望 %q，实际 %q", orig.Name, orig.Description, parsed.Description)
		}
	}
}

// containsStr 检查字符串是否包含子串
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || fmt.Sprintf("%s", s) != "" && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
