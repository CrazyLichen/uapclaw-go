package tool

import (
	"reflect"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试用结构体 ────────────────────────────

// basicTypesInput 基本类型映射测试
type basicTypesInput struct {
	Name   string  `json:"name" jsonschema:"description=名称"`
	Age    int     `json:"age" jsonschema:"description=年龄"`
	Score  float64 `json:"score" jsonschema:"description=分数"`
	Active bool    `json:"active" jsonschema:"description=是否激活"`
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestStructSchemaExtractor_基本类型映射 测试 string/int/float64/bool 映射
func TestStructSchemaExtractor_基本类型映射(t *testing.T) {
	typ := reflect.TypeOf(basicTypesInput{})
	params, err := StructSchemaExtractor{}.Extract(typ)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if len(params) != 4 {
		t.Fatalf("期望 4 个参数，实际 %d", len(params))
	}

	assertParam(t, params[0], "name", schema.ParamTypeString, "名称", true)
	assertParam(t, params[1], "age", schema.ParamTypeInteger, "年龄", true)
	assertParam(t, params[2], "score", schema.ParamTypeNumber, "分数", true)
	assertParam(t, params[3], "active", schema.ParamTypeBoolean, "是否激活", true)
}

// omitemptyInput omitempty 和 required 测试
type omitemptyInput struct {
	Name  string `json:"name" jsonschema:"description=名称"`
	Nick  string `json:"nick,omitempty" jsonschema:"description=昵称"`
	Force string `json:"force,omitempty" jsonschema:"description=强制字段,required"`
}

// TestStructSchemaExtractor_omitempty和required 测试 Required 推断规则
func TestStructSchemaExtractor_omitempty和required(t *testing.T) {
	typ := reflect.TypeOf(omitemptyInput{})
	params, err := StructSchemaExtractor{}.Extract(typ)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if len(params) != 3 {
		t.Fatalf("期望 3 个参数，实际 %d", len(params))
	}
	// name: 无 omitempty → Required=true
	if !params[0].Required {
		t.Error("name 应该 Required=true")
	}
	// nick: omitempty → Required=false
	if params[1].Required {
		t.Error("nick 应该 Required=false")
	}
	// force: omitempty 但 jsonschema:required → Required=true
	if !params[2].Required {
		t.Error("force 应该 Required=true（jsonschema:required 覆盖 omitempty）")
	}
}

// defaultInput 默认值测试
type defaultInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"description=数量上限,default=10"`
	Sort   string `json:"sort,omitempty" jsonschema:"description=排序,default=asc"`
	Active bool   `json:"active,omitempty" jsonschema:"description=是否激活,default=true"`
}

// TestStructSchemaExtractor_默认值 测试 default tag 解析和类型转换
func TestStructSchemaExtractor_默认值(t *testing.T) {
	typ := reflect.TypeOf(defaultInput{})
	params, err := StructSchemaExtractor{}.Extract(typ)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if params[0].Default != 10 {
		t.Errorf("Limit default: 期望 10，实际 %v", params[0].Default)
	}
	if params[1].Default != "asc" {
		t.Errorf("Sort default: 期望 asc，实际 %v", params[1].Default)
	}
	if params[2].Default != true {
		t.Errorf("Active default: 期望 true，实际 %v", params[2].Default)
	}
}

// nestedFilter 嵌套 struct 测试用
type nestedFilter struct {
	Category string `json:"category,omitempty" jsonschema:"description=分类"`
}

// nestedInput 嵌套 struct 测试
type nestedInput struct {
	Query  string        `json:"query" jsonschema:"description=查询关键词"`
	Filter *nestedFilter `json:"filter,omitempty" jsonschema:"description=过滤条件"`
}

// TestStructSchemaExtractor_嵌套struct 测试递归提取嵌套对象
func TestStructSchemaExtractor_嵌套struct(t *testing.T) {
	typ := reflect.TypeOf(nestedInput{})
	params, err := StructSchemaExtractor{}.Extract(typ)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if len(params) != 2 {
		t.Fatalf("期望 2 个参数，实际 %d", len(params))
	}
	// filter 应该是 Object 类型，有 Properties
	if params[1].Type != schema.ParamTypeObject {
		t.Errorf("filter 类型: 期望 Object，实际 %v", params[1].Type)
	}
	if len(params[1].Properties) != 1 {
		t.Errorf("filter 属性数: 期望 1，实际 %d", len(params[1].Properties))
	}
	if params[1].Properties[0].Name != "category" {
		t.Errorf("filter 属性名: 期望 category，实际 %q", params[1].Properties[0].Name)
	}
}

// sliceInput slice 类型测试
type sliceInput struct {
	Tags  []string       `json:"tags,omitempty" jsonschema:"description=标签列表"`
	Items []nestedFilter `json:"items,omitempty" jsonschema:"description=条目列表"`
}

// TestStructSchemaExtractor_slice类型 测试数组类型提取
func TestStructSchemaExtractor_slice类型(t *testing.T) {
	typ := reflect.TypeOf(sliceInput{})
	params, err := StructSchemaExtractor{}.Extract(typ)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	// Tags: Array[String]
	if params[0].Type != schema.ParamTypeArray {
		t.Errorf("Tags 类型: 期望 Array，实际 %v", params[0].Type)
	}
	if params[0].Items == nil || params[0].Items.Type != schema.ParamTypeString {
		t.Errorf("Tags items 类型: 期望 String，实际 %v", params[0].Items)
	}
	// Items: Array[Object]
	if params[1].Type != schema.ParamTypeArray {
		t.Errorf("Items 类型: 期望 Array，实际 %v", params[1].Type)
	}
	if params[1].Items == nil || params[1].Items.Type != schema.ParamTypeObject {
		t.Errorf("Items items 类型: 期望 Object，实际 %v", params[1].Items)
	}
}

// TestStructSchemaExtractor_空struct 测试空 struct 返回空列表
func TestStructSchemaExtractor_空struct(t *testing.T) {
	type emptyInput struct{}
	typ := reflect.TypeOf(emptyInput{})
	params, err := StructSchemaExtractor{}.Extract(typ)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if len(params) != 0 {
		t.Errorf("空 struct 应返回空列表，实际 %d", len(params))
	}
}

// TestStructSchemaExtractor_无jsonTag字段跳过 测试无 json tag 的字段被跳过
func TestStructSchemaExtractor_无jsonTag字段跳过(t *testing.T) {
	type noJSONInput struct {
		Name    string `json:"name"`
		Private string // 无 json tag
	}
	typ := reflect.TypeOf(noJSONInput{})
	params, err := StructSchemaExtractor{}.Extract(typ)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if len(params) != 1 {
		t.Errorf("期望 1 个参数（Private 应跳过），实际 %d", len(params))
	}
}

// TestStructSchemaExtractor_指针类型 测试指针字段解引用
func TestStructSchemaExtractor_指针类型(t *testing.T) {
	type ptrInput struct {
		Name *string `json:"name" jsonschema:"description=名称"`
	}
	typ := reflect.TypeOf(ptrInput{})
	params, err := StructSchemaExtractor{}.Extract(typ)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if len(params) != 1 {
		t.Fatalf("期望 1 个参数，实际 %d", len(params))
	}
	if params[0].Type != schema.ParamTypeString {
		t.Errorf("指针字段应解引用为 String，实际 %v", params[0].Type)
	}
}

// TestStructSchemaExtractor_不支持的类型 测试不支持的类型返回错误
func TestStructSchemaExtractor_不支持的类型(t *testing.T) {
	type badInput struct {
		Ch chan int `json:"ch" jsonschema:"description=通道"`
	}
	typ := reflect.TypeOf(badInput{})
	_, err := StructSchemaExtractor{}.Extract(typ)
	if err == nil {
		t.Error("不支持的类型应返回错误")
	}
}

// TestStructSchemaExtractor_非struct类型 测试非 struct 类型返回错误
func TestStructSchemaExtractor_非struct类型(t *testing.T) {
	typ := reflect.TypeOf("string")
	_, err := StructSchemaExtractor{}.Extract(typ)
	if err == nil {
		t.Error("非 struct 类型应返回错误")
	}
}

// TestStructSchemaExtractor_float默认值 测试 float 类型默认值
func TestStructSchemaExtractor_float默认值(t *testing.T) {
	type floatDefaultInput struct {
		Rate float64 `json:"rate,omitempty" jsonschema:"description=比率,default=0.5"`
	}
	typ := reflect.TypeOf(floatDefaultInput{})
	params, err := StructSchemaExtractor{}.Extract(typ)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if params[0].Default != 0.5 {
		t.Errorf("Rate default: 期望 0.5，实际 %v", params[0].Default)
	}
}

// TestStructSchemaExtractor_bool默认值 测试 bool 类型默认值
func TestStructSchemaExtractor_bool默认值(t *testing.T) {
	type boolDefaultInput struct {
		Active bool `json:"active,omitempty" jsonschema:"description=激活,default=false"`
	}
	typ := reflect.TypeOf(boolDefaultInput{})
	params, err := StructSchemaExtractor{}.Extract(typ)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if params[0].Default != false {
		t.Errorf("Active default: 期望 false，实际 %v", params[0].Default)
	}
}

// TestStructSchemaExtractor_ExtractDescription 测试描述提取
func TestStructSchemaExtractor_ExtractDescription(t *testing.T) {
	typ := reflect.TypeOf(basicTypesInput{})
	desc := StructSchemaExtractor{}.ExtractDescription(typ)
	// basicTypesInput → humanize → "basic types input"
	if desc == "" {
		t.Error("ExtractDescription 应返回 humanize 后的名称，而非空字符串")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestHumanizeName 测试变量名转人类可读描述
func TestHumanizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// snake_case
		{"search_query", "search query"},
		{"user_name", "user name"},
		{"", ""},
		// camelCase / PascalCase
		{"userName", "user name"},
		{"SearchQuery", "search query"},
		{"XMLParser", "xml parser"},
		// Python _humanize_name 行为：缩写替换在 .lower() 之后也被转小写
		// 与 Python 保持一致，输出全小写
		{"user_id", "user id"},
		{"api_url", "api url"},
		{"html_content", "html content"},
	}
	for _, tt := range tests {
		result := humanizeName(tt.input)
		if result != tt.expected {
			t.Errorf("humanizeName(%q) = %q, 期望 %q", tt.input, result, tt.expected)
		}
	}
}

// TestResolveDescription 测试描述优先级（tag 优先，缺失时 humanize）
func TestResolveDescription(t *testing.T) {
	// 有 description tag 时优先使用
	tags := parseSchemaTag("description=自定义描述")
	result := resolveDescription("search_query", tags)
	if result != "自定义描述" {
		t.Errorf("有 description tag 时应使用 tag 值，实际 %q", result)
	}

	// 无 description tag 时 humanize
	tags2 := parseSchemaTag("required")
	result2 := resolveDescription("search_query", tags2)
	if result2 != "search query" {
		t.Errorf("无 description tag 时应 humanize，实际 %q", result2)
	}
}

// assertParam 断言参数定义
func assertParam(t *testing.T, p *schema.Param, name string, typ schema.ParamType, desc string, required bool) {
	t.Helper()
	if p.Name != name {
		t.Errorf("Name: 期望 %q，实际 %q", name, p.Name)
	}
	if p.Type != typ {
		t.Errorf("Type: 期望 %v，实际 %v", typ, p.Type)
	}
	if p.Description != desc {
		t.Errorf("Description: 期望 %q，实际 %q", desc, p.Description)
	}
	if p.Required != required {
		t.Errorf("Required: 期望 %v，实际 %v", required, p.Required)
	}
}
