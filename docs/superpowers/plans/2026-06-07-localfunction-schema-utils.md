# LocalFunction 与 SchemaUtils 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 LocalFunction（强类型泛型函数包装为 Tool）及配套的 SchemaUtils 和 StructSchemaExtractor，使 LLM 可通过 function calling 调用 Go 函数。

**Architecture:** 用户定义 Input/Output struct（带 json + jsonschema tag），通过泛型 `NewInvokeFunction` / `NewStreamFunction` 注册为 Tool。注册时从 struct tag 反射提取 `[]*schema.Param` 生成 JSON Schema 供 LLM 消费。运行时 LLM 返回 `map[string]any`，经 SchemaUtils 格式化/校验后 JSON 中转为 struct，调用用户函数。

**Tech Stack:** Go 1.23 泛型、reflect 反射、encoding/json、项目已有 schema.Param 体系

**Design Doc:** `docs/superpowers/specs/2026-06-07-localfunction-schema-utils-design.md`

**覆盖实现计划章节：** 3.3 (LocalFunction) + 3.4 (@tool 等价) + 3.12 (SchemaUtils)

---

## 文件结构

```
internal/agentcore/foundation/tool/
├── doc.go                          # 修改：更新文件目录
├── base.go                         # 已有：不修改
├── tool_info.go                    # 已有：不修改
├── lifecycle_tool.go               # 已有：不修改
├── struct_schema_extractor.go      # 创建：struct → []*Param 反射提取器
├── struct_schema_extractor_test.go # 创建：反射提取器测试
├── schema_utils.go                 # 创建：SchemaUtils（Format/Validate/RemoveNoneValues）
├── schema_utils_test.go            # 创建：SchemaUtils 测试
├── invoke_function.go              # 创建：InvokeFunction[I,O] 泛型
├── invoke_function_test.go         # 创建：InvokeFunction 测试
├── stream_function.go              # 创建：StreamFunction[I,O] 泛型
├── stream_function_test.go         # 创建：StreamFunction 测试
├── map_function.go                 # 创建：MapFunction 降级
├── map_function_test.go            # 创建：MapFunction 测试
├── tool_func.go                    # 创建：Tool()/StreamTool() 便捷函数
└── tool_func_test.go               # 创建：便捷函数测试
```

---

### Task 1: StructSchemaExtractor — jsonschema tag 解析器

**Files:**
- Create: `internal/agentcore/foundation/tool/struct_schema_extractor.go`
- Test: `internal/agentcore/foundation/tool/struct_schema_extractor_test.go`

这是最底层的依赖，被所有后续 Task 依赖。先实现 tag 解析和基本类型映射。

- [ ] **Step 1: 写 StructSchemaExtractor 的测试 — 基本类型映射**

```go
// struct_schema_extractor_test.go
package tool

import (
	"reflect"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试用结构体 ────────────────────────────

// basicTypesInput 基本类型映射测试
type basicTypesInput struct {
	Name    string `json:"name" jsonschema:"description=名称"`
	Age     int    `json:"age" jsonschema:"description=年龄"`
	Score   float64 `json:"score" jsonschema:"description=分数"`
	Active  bool   `json:"active" jsonschema:"description=是否激活"`
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
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run TestStructSchemaExtractor -v`
Expected: FAIL — StructSchemaExtractor 未定义

- [ ] **Step 3: 实现 StructSchemaExtractor — jsonschema tag 解析 + 基本类型映射**

```go
// struct_schema_extractor.go
package tool

import (
	"fmt"
	"reflect"
	"strings"

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
			Description: schemaTags.get("description"),
		}

		// 确定 Required
		param.Required = resolveRequired(omitempty, schemaTags)

		// 设置默认值
		if def, ok := schemaTags.getOk("default"); ok {
			param.Default = convertDefaultValue(def, paramType)
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
// 目前简单返回空字符串，后续可通过注释解析增强。
func (StructSchemaExtractor) ExtractDescription(typ reflect.Type) string {
	return ""
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
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run TestStructSchemaExtractor -v`
Expected: PASS

- [ ] **Step 5: 写 StructSchemaExtractor 的测试 — omitempty/required/default/enum/嵌套/slice**

在 `struct_schema_extractor_test.go` 中追加：

```go
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
	Limit int    `json:"limit,omitempty" jsonschema:"description=数量上限,default=10"`
	Sort  string `json:"sort,omitempty" jsonschema:"description=排序,default=asc"`
	Active bool  `json:"active,omitempty" jsonschema:"description=是否激活,default=true"`
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

// nestedInput 嵌套 struct 测试
type nestedFilter struct {
	Category string `json:"category,omitempty" jsonschema:"description=分类"`
}

type nestedInput struct {
	Query string       `json:"query" jsonschema:"description=查询关键词"`
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
	Tags []string `json:"tags,omitempty" jsonschema:"description=标签列表"`
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
```

- [ ] **Step 6: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run TestStructSchemaExtractor -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/foundation/tool/struct_schema_extractor.go internal/agentcore/foundation/tool/struct_schema_extractor_test.go && git commit -m "feat(tool): 实现 StructSchemaExtractor — 从 struct tag 反射提取参数定义"
```

---

### Task 2: SchemaUtils — 格式化/校验/RemoveNoneValues

**Files:**
- Create: `internal/agentcore/foundation/tool/schema_utils.go`
- Test: `internal/agentcore/foundation/tool/schema_utils_test.go`

InvokeFunction.Invoke 强依赖这些能力。

- [ ] **Step 1: 写 SchemaUtils 测试**

```go
// schema_utils_test.go
package tool

import (
	"math"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestSchemaUtils_RemoveNoneValues 测试递归移除 nil 值
func TestSchemaUtils_RemoveNoneValues(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected map[string]any
	}{
		{
			name:     "nil输入",
			input:    nil,
			expected: nil,
		},
		{
			name:     "空map",
			input:    map[string]any{},
			expected: nil,
		},
		{
			name:     "移除顶层nil",
			input:    map[string]any{"a": "hello", "b": nil, "c": 42},
			expected: map[string]any{"a": "hello", "c": 42},
		},
		{
			name:     "递归移除嵌套nil",
			input:    map[string]any{"a": map[string]any{"x": 1, "y": nil}},
			expected: map[string]any{"a": map[string]any{"x": 1}},
		},
		{
			name:     "全部为nil返回nil",
			input:    map[string]any{"a": nil, "b": nil},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SchemaUtils{}.RemoveNoneValues(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("期望 nil，实际 %v", result)
				}
			} else {
				if result == nil {
					t.Errorf("期望 %v，实际 nil", tt.expected)
				}
			}
		})
	}
}

// TestSchemaUtils_Validate_必填校验 测试必填字段校验
func TestSchemaUtils_Validate_必填校验(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("name", "名称", true),
		schema.NewIntegerParam("age", "年龄", false),
	}

	// 缺少必填字段
	err := SchemaUtils{}.Validate(map[string]any{"age": float64(25)}, params)
	if err == nil {
		t.Error("缺少必填字段 name 应返回错误")
	}

	// 必填字段存在
	err = SchemaUtils{}.Validate(map[string]any{"name": "Alice"}, params)
	if err != nil {
		t.Errorf("必填字段存在应通过校验: %v", err)
	}
}

// TestSchemaUtils_Validate_类型校验 测试类型匹配校验
func TestSchemaUtils_Validate_类型校验(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("name", "名称", true),
		schema.NewIntegerParam("age", "年龄", true),
		schema.NewNumberParam("score", "分数", true),
		schema.NewBooleanParam("active", "是否激活", true),
	}

	// 正确类型
	err := SchemaUtils{}.Validate(map[string]any{
		"name":   "Alice",
		"age":    float64(25),
		"score":  99.5,
		"active": true,
	}, params)
	if err != nil {
		t.Errorf("正确类型应通过: %v", err)
	}

	// 类型不匹配：string 位置传 int
	err = SchemaUtils{}.Validate(map[string]any{"name": 42}, params)
	if err == nil {
		t.Error("string 位置传 int 应返回错误")
	}

	// float64 整数可接受为 integer
	err = SchemaUtils{}.Validate(map[string]any{"name": "A", "age": float64(25)}, []*schema.Param{
		schema.NewIntegerParam("age", "年龄", true),
	})
	if err != nil {
		t.Errorf("float64 整数应接受为 integer: %v", err)
	}

	// float64 非整数不可接受为 integer
	err = SchemaUtils{}.Validate(map[string]any{"name": "A", "age": 25.5}, []*schema.Param{
		schema.NewIntegerParam("age", "年龄", true),
	})
	if err == nil {
		t.Error("float64 非整数不可接受为 integer")
	}
}

// TestSchemaUtils_FormatWithSchema_默认值填充 测试默认值填充
func TestSchemaUtils_FormatWithSchema_默认值填充(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("name", "名称", true),
	}
	// 带 default 的参数需要手动创建
	limitParam := &schema.Param{
		Name: "limit", Type: schema.ParamTypeInteger,
		Description: "数量上限", Required: false, Default: 10,
	}
	params = append(params, limitParam)

	data := map[string]any{"name": "Alice"}
	result, err := SchemaUtils{}.FormatWithSchema(data, params)
	if err != nil {
		t.Fatalf("FormatWithSchema 失败: %v", err)
	}
	if result["name"] != "Alice" {
		t.Errorf("name: 期望 Alice，实际 %v", result["name"])
	}
	if result["limit"] != 10 {
		t.Errorf("limit default: 期望 10，实际 %v", result["limit"])
	}
}

// TestSchemaUtils_FormatWithSchema_必填缺失 测试必填字段缺失
func TestSchemaUtils_FormatWithSchema_必填缺失(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("name", "名称", true),
	}

	_, err := SchemaUtils{}.FormatWithSchema(map[string]any{}, params)
	if err == nil {
		t.Error("缺少必填字段应返回错误")
	}
}

// TestSchemaUtils_FormatWithSchema_额外字段保留 测试额外字段保留
func TestSchemaUtils_FormatWithSchema_额外字段保留(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("name", "名称", true),
	}

	data := map[string]any{"name": "Alice", "extra": "value"}
	result, err := SchemaUtils{}.FormatWithSchema(data, params)
	if err != nil {
		t.Fatalf("FormatWithSchema 失败: %v", err)
	}
	if result["extra"] != "value" {
		t.Errorf("额外字段应保留: extra=%v", result["extra"])
	}
}

// TestSchemaUtils_float64整数校验 辅助测试
func TestSchemaUtils_float64整数校验(t *testing.T) {
	if math.Trunc(25.0) != 25.0 {
		t.Error("25.0 应为整数")
	}
	if math.Trunc(25.5) == 25.5 {
		t.Error("25.5 不应为整数")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run TestSchemaUtils -v`
Expected: FAIL

- [ ] **Step 3: 实现 SchemaUtils**

```go
// schema_utils.go
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

// WithSkipNoneValue 设置格式化时是否跳过 nil 值。
func WithSkipNoneValue(skip bool) FormatOption {
	return func(o *formatOptions) { o.skipNoneValue = skip }
}

// WithSkipValidate 设置格式化时是否跳过校验。
func WithSkipValidate(skip bool) FormatOption {
	return func(o *formatOptions) { o.skipValidate = skip }
}

// FormatWithSchema 根据参数 schema 格式化输入数据，填充默认值。
//
// 流程：RemoveNoneValues（可选）→ Validate（可选）→ 填充默认值
//
// 对应 Python: SchemaUtils.format_with_schema()
func (SchemaUtils) FormatWithSchema(data map[string]any, params []*schema.Param, opts ...FormatOption) (map[string]any, error) {
	o := &formatOptions{}
	for _, opt := range opts {
		opt(o)
	}

	// 1. 可选：移除 nil 值
	if o.skipNoneValue {
		data = SchemaUtils{}.RemoveNoneValues(data)
		if data == nil {
			data = make(map[string]any)
		}
	}

	// 2. 可选：校验
	if !o.skipValidate {
		if err := SchemaUtils{}.Validate(data, params); err != nil {
			return nil, err
		}
	}

	// 3. 填充默认值
	result := make(map[string]any, len(data))
	paramMap := make(map[string]*schema.Param, len(params))
	for _, p := range params {
		paramMap[p.Name] = p
	}

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
func (SchemaUtils) Validate(data map[string]any, params []*schema.Param) error {
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
func (SchemaUtils) RemoveNoneValues(data map[string]any) map[string]any {
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
			cleaned := SchemaUtils{}.RemoveNoneValues(tv)
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
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run TestSchemaUtils -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/foundation/tool/schema_utils.go internal/agentcore/foundation/tool/schema_utils_test.go && git commit -m "feat(tool): 实现 SchemaUtils — 参数校验/格式化/RemoveNoneValues"
```

---

### Task 3: InvokeFunction — 泛型本地函数工具（Invoke 模式）

**Files:**
- Create: `internal/agentcore/foundation/tool/invoke_function.go`
- Test: `internal/agentcore/foundation/tool/invoke_function_test.go`

- [ ] **Step 1: 写 InvokeFunction 测试**

```go
// invoke_function_test.go
package tool

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试用结构体 ────────────────────────────

// searchInput 搜索输入
type searchInput struct {
	Query string `json:"query" jsonschema:"description=搜索关键词,required"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=返回数量上限,default=10"`
}

// searchOutput 搜索输出
type searchOutput struct {
	Results []string `json:"results"`
	Total   int      `json:"total"`
}

// ──────────────────────────── 测试用函数 ────────────────────────────

// searchFunc 搜索函数
func searchFunc(ctx context.Context, input searchInput) (searchOutput, error) {
	return searchOutput{
		Results: []string{input.Query},
		Total:   input.Limit,
	}, nil
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewInvokeFunction_自动推断 创建时自动推断泛型参数
func TestNewInvokeFunction_自动推断(t *testing.T) {
	fn, err := NewInvokeFunction("search", searchFunc)
	if err != nil {
		t.Fatalf("NewInvokeFunction 失败: %v", err)
	}
	if fn.Card().Name != "search" {
		t.Errorf("Name: 期望 search，实际 %q", fn.Card().Name)
	}
	if len(fn.Card().InputParams) != 2 {
		t.Errorf("InputParams: 期望 2 个，实际 %d", len(fn.Card().InputParams))
	}
}

// TestInvokeFunction_Invoke_完整流程 测试 map→struct→fn→map 完整流程
func TestInvokeFunction_Invoke_完整流程(t *testing.T) {
	fn, _ := NewInvokeFunction("search", searchFunc)
	result, err := fn.Invoke(context.Background(), map[string]any{
		"query": "hello",
		"limit": float64(5),
	})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	results, ok := result["results"].([]any)
	if !ok {
		t.Fatalf("results 类型错误: %T", result["results"])
	}
	if len(results) != 1 || results[0] != "hello" {
		t.Errorf("results: 期望 [hello]，实际 %v", results)
	}
}

// TestInvokeFunction_Invoke_默认值填充 测试默认值填充
func TestInvokeFunction_Invoke_默认值填充(t *testing.T) {
	fn, _ := NewInvokeFunction("search", searchFunc)
	result, err := fn.Invoke(context.Background(), map[string]any{
		"query": "hello",
	})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	total, ok := result["total"].(float64)
	if !ok {
		t.Fatalf("total 类型错误: %T", result["total"])
	}
	if total != 10 {
		t.Errorf("total: 期望 10（默认值），实际 %v", total)
	}
}

// TestInvokeFunction_Invoke_必填缺失 测试必填字段缺失
func TestInvokeFunction_Invoke_必填缺失(t *testing.T) {
	fn, _ := NewInvokeFunction("search", searchFunc)
	_, err := fn.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Error("缺少必填字段 query 应返回错误")
	}
}

// TestInvokeFunction_Stream_不支持 测试 Invoke 模式的 Stream 返回错误
func TestInvokeFunction_Stream_不支持(t *testing.T) {
	fn, _ := NewInvokeFunction("search", searchFunc)
	_, err := fn.Stream(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Error("Invoke 模式 Stream 应返回 ErrStreamNotSupported")
	}
}

// TestInvokeFunction_Card ToolInfo 测试
func TestInvokeFunction_Card_ToolInfo(t *testing.T) {
	fn, _ := NewInvokeFunction("search", searchFunc)
	info := fn.Card().ToolInfo()
	if info.Name != "search" {
		t.Errorf("ToolInfo.Name: 期望 search，实际 %q", info.Name)
	}
	if info.Parameters == nil {
		t.Error("ToolInfo.Parameters 不应为 nil")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run TestInvokeFunction -v`
Expected: FAIL

- [ ] **Step 3: 实现 InvokeFunction**

```go
// invoke_function.go
package tool

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InvokeFunction 本地函数工具（Invoke 模式），将 Go 函数包装为 Tool。
//
// 用户函数签名：func(ctx context.Context, input I) (O, error)
//
// 对应 Python: openjiuwen/core/foundation/tool/function/function.py (LocalFunction)
// Python 不区分 Invoke/Stream，Go 通过不同类型在编译期保证签名正确。
type InvokeFunction[I any, O any] struct {
	card *ToolCard
	fn   func(context.Context, I) (O, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// LocalFuncOption 本地函数构造选项函数。
type LocalFuncOption func(*localFuncConfig)

// ──────────────────────────── 全局变量 ────────────────────────────

// localFuncConfig 内部配置。
type localFuncConfig struct {
	description string
	inputParams []*schema.Param
	card        *ToolCard
}

// ──────────────────────────── 导出函数 ────────────────────────────

// WithDescription 设置工具描述（覆盖自动提取）。
func WithDescription(desc string) LocalFuncOption {
	return func(c *localFuncConfig) { c.description = desc }
}

// WithInputParams 手动设置输入参数（覆盖自动提取）。
func WithInputParams(params []*schema.Param) LocalFuncOption {
	return func(c *localFuncConfig) { c.inputParams = params }
}

// WithCard 使用预构建的 ToolCard。
func WithCard(card *ToolCard) LocalFuncOption {
	return func(c *localFuncConfig) { c.card = card }
}

// NewInvokeFunction 创建 Invoke 模式的本地函数工具。
//
// 自动从 I 类型的 struct tag 提取 InputParams 填入 ToolCard。
// Go 编译器从 fn 参数自动推断 I 和 O，用户通常无需显式指定泛型参数。
//
// 使用示例：
//
//	fn, _ := NewInvokeFunction("search", Search)
//	// 等价于 NewInvokeFunction[SearchInput, SearchOutput]("search", Search)
//
// 对应 Python: LocalFunction(card=card, func=func)
func NewInvokeFunction[I any, O any](name string, fn func(context.Context, I) (O, error), opts ...LocalFuncOption) (*InvokeFunction[I, O], error) {
	cfg := &localFuncConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 确定 InputParams
	var inputParams []*schema.Param
	if cfg.inputParams != nil {
		inputParams = cfg.inputParams
	} else {
		typ := reflect.TypeOf((*I)(nil)).Elem()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		extracted, err := StructSchemaExtractor{}.Extract(typ)
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusToolLocalFunctionFuncNotSupported,
				exception.WithParam("name", name),
				exception.WithParam("reason", err.Error()),
			)
		}
		inputParams = extracted
	}

	// 确定描述
	description := cfg.description
	if description == "" {
		typ := reflect.TypeOf((*I)(nil)).Elem()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		description = StructSchemaExtractor{}.ExtractDescription(typ)
	}
	if description == "" {
		description = name
	}

	// 构建 ToolCard
	var card *ToolCard
	if cfg.card != nil {
		card = cfg.card
	} else {
		card = NewToolCard(name, description, inputParams, nil)
	}

	return &InvokeFunction[I, O]{card: card, fn: fn}, nil
}

// Card 返回工具配置卡片。
func (f *InvokeFunction[I, O]) Card() *ToolCard {
	return f.card
}

// Invoke 执行工具调用：校验输入 → 格式化 → map→struct → 调用用户函数 → struct→map。
func (f *InvokeFunction[I, O]) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	o := NewToolCallOptions(opts...)

	// 1. 参数格式化
	if f.card.InputParams != nil {
		formatted, err := SchemaUtils{}.FormatWithSchema(inputs, f.card.InputParams,
			WithSkipNoneValue(o.SkipNoneValue),
			WithSkipValidate(o.SkipInputsValidate),
		)
		if err != nil {
			return nil, err
		}
		inputs = formatted
	}

	// 2. map → struct
	jsonBytes, err := json.Marshal(inputs)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", "marshal inputs failed"),
		)
	}
	var input I
	if err := json.Unmarshal(jsonBytes, &input); err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", "unmarshal inputs to struct failed"),
		)
	}

	// 3. 调用用户函数
	output, err := f.fn(ctx, input)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 4. struct → map
	result, err := structToMap(output)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", "convert output to map failed"),
		)
	}

	return result, nil
}

// Stream 不支持流式调用，返回 ErrStreamNotSupported。
func (f *InvokeFunction[I, O]) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	return nil, NewErrStreamNotSupported(f.card.String())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// structToMap 将任意值转换为 map[string]any。
func structToMap(v any) (map[string]any, error) {
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		// 非 struct 输出，包装为 {"result": v}
		return map[string]any{"result": v}, nil
	}
	return result, nil
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run TestInvokeFunction -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/foundation/tool/invoke_function.go internal/agentcore/foundation/tool/invoke_function_test.go && git commit -m "feat(tool): 实现 InvokeFunction[I,O] — 泛型本地函数工具 Invoke 模式"
```

---

### Task 4: StreamFunction — 泛型本地函数工具（Stream 模式）

**Files:**
- Create: `internal/agentcore/foundation/tool/stream_function.go`
- Test: `internal/agentcore/foundation/tool/stream_function_test.go`

- [ ] **Step 1: 写 StreamFunction 测试**

```go
// stream_function_test.go
package tool

import (
	"context"
	"testing"
)

// ──────────────────────────── 测试用函数 ────────────────────────────

// streamSearchFunc 流式搜索函数
func streamSearchFunc(ctx context.Context, input searchInput) (<-chan searchOutput, error) {
	ch := make(chan searchOutput, 2)
	go func() {
		defer close(ch)
		ch <- searchOutput{Results: []string{input.Query + "_1"}, Total: 1}
		ch <- searchOutput{Results: []string{input.Query + "_2"}, Total: 2}
	}()
	return ch, nil
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewStreamFunction_自动推断 测试自动推断
func TestNewStreamFunction_自动推断(t *testing.T) {
	fn, err := NewStreamFunction("stream_search", streamSearchFunc)
	if err != nil {
		t.Fatalf("NewStreamFunction 失败: %v", err)
	}
	if fn.Card().Name != "stream_search" {
		t.Errorf("Name: 期望 stream_search，实际 %q", fn.Card().Name)
	}
}

// TestStreamFunction_Stream_完整流程 测试流式完整流程
func TestStreamFunction_Stream_完整流程(t *testing.T) {
	fn, _ := NewStreamFunction("stream_search", streamSearchFunc)
	ch, err := fn.Stream(context.Background(), map[string]any{
		"query": "hello",
	})
	if err != nil {
		t.Fatalf("Stream 失败: %v", err)
	}

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	// 应收到 2 个数据块 + 1 个 Done
	dataChunks := 0
	doneChunks := 0
	for _, c := range chunks {
		if c.Done {
			doneChunks++
		} else if c.Error != nil {
			t.Fatalf("意外错误: %v", c.Error)
		} else {
			dataChunks++
		}
	}
	if dataChunks != 2 {
		t.Errorf("数据块: 期望 2，实际 %d", dataChunks)
	}
	if doneChunks != 1 {
		t.Errorf("Done 块: 期望 1，实际 %d", doneChunks)
	}
}

// TestStreamFunction_Invoke_不支持 测试 Stream 模式的 Invoke 返回错误
func TestStreamFunction_Invoke_不支持(t *testing.T) {
	fn, _ := NewStreamFunction("stream_search", streamSearchFunc)
	_, err := fn.Invoke(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Error("Stream 模式 Invoke 应返回 ErrStreamNotSupported")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run TestStreamFunction -v`
Expected: FAIL

- [ ] **Step 3: 实现 StreamFunction**

```go
// stream_function.go
package tool

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamFunction 本地函数工具（Stream 模式），将 Go 流式函数包装为 Tool。
//
// 用户函数签名：func(ctx context.Context, input I) (<-chan O, error)
//
// 对应 Python: openjiuwen/core/foundation/tool/function/function.py (LocalFunction.stream)
type StreamFunction[I any, O any] struct {
	card *ToolCard
	fn   func(context.Context, I) (<-chan O, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamFunction 创建 Stream 模式的本地函数工具。
//
// 使用示例：
//
//	fn, _ := NewStreamFunction("stream_search", StreamSearch)
func NewStreamFunction[I any, O any](name string, fn func(context.Context, I) (<-chan O, error), opts ...LocalFuncOption) (*StreamFunction[I, O], error) {
	cfg := &localFuncConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 确定 InputParams
	var inputParams []*schema.Param
	if cfg.inputParams != nil {
		inputParams = cfg.inputParams
	} else {
		typ := reflect.TypeOf((*I)(nil)).Elem()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		extracted, err := StructSchemaExtractor{}.Extract(typ)
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusToolLocalFunctionFuncNotSupported,
				exception.WithParam("name", name),
				exception.WithParam("reason", err.Error()),
			)
		}
		inputParams = extracted
	}

	// 确定描述
	description := cfg.description
	if description == "" {
		typ := reflect.TypeOf((*I)(nil)).Elem()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		description = StructSchemaExtractor{}.ExtractDescription(typ)
	}
	if description == "" {
		description = name
	}

	// 构建 ToolCard
	var card *ToolCard
	if cfg.card != nil {
		card = cfg.card
	} else {
		card = NewToolCard(name, description, inputParams, nil)
	}

	return &StreamFunction[I, O]{card: card, fn: fn}, nil
}

// Card 返回工具配置卡片。
func (f *StreamFunction[I, O]) Card() *ToolCard {
	return f.card
}

// Invoke 不支持一次性调用，返回 ErrStreamNotSupported。
func (f *StreamFunction[I, O]) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	return nil, NewErrStreamNotSupported(f.card.String())
}

// Stream 流式执行工具调用：校验输入 → 格式化 → map→struct → 调用用户流式函数 → 逐 chunk 转换。
func (f *StreamFunction[I, O]) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	o := NewToolCallOptions(opts...)

	// 1. 参数格式化
	if f.card.InputParams != nil {
		formatted, err := SchemaUtils{}.FormatWithSchema(inputs, f.card.InputParams,
			WithSkipNoneValue(o.SkipNoneValue),
			WithSkipValidate(o.SkipInputsValidate),
		)
		if err != nil {
			return nil, err
		}
		inputs = formatted
	}

	// 2. map → struct
	jsonBytes, err := json.Marshal(inputs)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "stream"),
			exception.WithParam("reason", "marshal inputs failed"),
		)
	}
	var input I
	if err := json.Unmarshal(jsonBytes, &input); err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "stream"),
			exception.WithParam("reason", "unmarshal inputs to struct failed"),
		)
	}

	// 3. 调用用户流式函数
	ch, err := f.fn(ctx, input)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "stream"),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 4. 包装输出 channel
	outCh := make(chan StreamChunk, 1)
	go func() {
		defer close(outCh)
		for chunk := range ch {
			data, err := structToMap(chunk)
			if err != nil {
				outCh <- StreamChunk{Error: err}
				return
			}
			outCh <- StreamChunk{Data: data}
		}
		outCh <- StreamChunk{Done: true}
	}()

	return outCh, nil
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run TestStreamFunction -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/foundation/tool/stream_function.go internal/agentcore/foundation/tool/stream_function_test.go && git commit -m "feat(tool): 实现 StreamFunction[I,O] — 泛型本地函数工具 Stream 模式"
```

---

### Task 5: MapFunction — 弱类型 map 降级模式

**Files:**
- Create: `internal/agentcore/foundation/tool/map_function.go`
- Test: `internal/agentcore/foundation/tool/map_function_test.go`

- [ ] **Step 1: 写 MapFunction 测试**

```go
// map_function_test.go
package tool

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestMapFunction_Invoke 测试弱类型 Invoke
func TestMapFunction_Invoke(t *testing.T) {
	card := NewToolCard("echo", "回显工具", []*schema.Param{
		schema.NewStringParam("message", "消息", true),
	}, nil)

	fn, err := NewMapFunction(card,
		func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
			return map[string]any{"echo": inputs["message"]}, nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("NewMapFunction 失败: %v", err)
	}

	result, err := fn.Invoke(context.Background(), map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	if result["echo"] != "hello" {
		t.Errorf("echo: 期望 hello，实际 %v", result["echo"])
	}
}

// TestMapFunction_Stream 测试弱类型 Stream
func TestMapFunction_Stream(t *testing.T) {
	card := NewToolCard("stream_echo", "流式回显", []*schema.Param{
		schema.NewStringParam("message", "消息", true),
	}, nil)

	fn, err := NewMapFunction(card,
		nil,
		func(ctx context.Context, inputs map[string]any) (<-chan map[string]any, error) {
			ch := make(chan map[string]any, 1)
			go func() {
				defer close(ch)
				ch <- map[string]any{"echo": inputs["message"]}
			}()
			return ch, nil
		},
	)
	if err != nil {
		t.Fatalf("NewMapFunction 失败: %v", err)
	}

	ch, err := fn.Stream(context.Background(), map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("Stream 失败: %v", err)
	}

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) < 1 {
		t.Error("应至少收到 1 个数据块")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run TestMapFunction -v`
Expected: FAIL

- [ ] **Step 3: 实现 MapFunction**

```go
// map_function.go
package tool

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MapFunction 弱类型 map 函数工具，降级模式。
//
// 当函数参数无法用 struct 描述时（如动态参数），使用 MapFunction 代替 InvokeFunction/StreamFunction。
// 用户需手动提供 InputParams。
//
// 对应 Python: LocalFunction(func=None) 的降级场景
type MapFunction struct {
	card     *ToolCard
	invokeFn func(ctx context.Context, inputs map[string]any) (map[string]any, error)
	streamFn func(ctx context.Context, inputs map[string]any) (<-chan map[string]any, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMapFunction 创建弱类型 map 函数工具（降级模式）。
//
// invokeFn / streamFn 二选一，另一个传 nil。
// card.InputParams 由用户手动提供。
func NewMapFunction(card *ToolCard,
	invokeFn func(ctx context.Context, inputs map[string]any) (map[string]any, error),
	streamFn func(ctx context.Context, inputs map[string]any) (<-chan map[string]any, error),
) (*MapFunction, error) {
	if err := ValidateToolCard(card); err != nil {
		return nil, err
	}
	return &MapFunction{
		card:     card,
		invokeFn: invokeFn,
		streamFn: streamFn,
	}, nil
}

// Card 返回工具配置卡片。
func (f *MapFunction) Card() *ToolCard {
	return f.card
}

// Invoke 执行弱类型函数调用，直接透传 map。
func (f *MapFunction) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	if f.invokeFn == nil {
		return nil, NewErrStreamNotSupported(f.card.String())
	}

	o := NewToolCallOptions(opts...)

	// 参数格式化
	if f.card.InputParams != nil {
		formatted, err := SchemaUtils{}.FormatWithSchema(inputs, f.card.InputParams,
			WithSkipNoneValue(o.SkipNoneValue),
			WithSkipValidate(o.SkipInputsValidate),
		)
		if err != nil {
			return nil, err
		}
		inputs = formatted
	}

	result, err := f.invokeFn(ctx, inputs)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", err.Error()),
		)
	}
	return result, nil
}

// Stream 执行弱类型流式函数调用。
func (f *MapFunction) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	if f.streamFn == nil {
		return nil, NewErrStreamNotSupported(f.card.String())
	}

	o := NewToolCallOptions(opts...)

	// 参数格式化
	if f.card.InputParams != nil {
		formatted, err := SchemaUtils{}.FormatWithSchema(inputs, f.card.InputParams,
			WithSkipNoneValue(o.SkipNoneValue),
			WithSkipValidate(o.SkipInputsValidate),
		)
		if err != nil {
			return nil, err
		}
		inputs = formatted
	}

	innerCh, err := f.streamFn(ctx, inputs)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolLocalFunctionExecutionError,
			exception.WithParam("method", "stream"),
			exception.WithParam("reason", err.Error()),
		)
	}

	outCh := make(chan StreamChunk, 1)
	go func() {
		defer close(outCh)
		for chunk := range innerCh {
			outCh <- StreamChunk{Data: chunk}
		}
		outCh <- StreamChunk{Done: true}
	}()

	return outCh, nil
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run TestMapFunction -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/foundation/tool/map_function.go internal/agentcore/foundation/tool/map_function_test.go && git commit -m "feat(tool): 实现 MapFunction — 弱类型 map 降级模式"
```

---

### Task 6: Tool() / StreamTool() 便捷注册函数

**Files:**
- Create: `internal/agentcore/foundation/tool/tool_func.go`
- Test: `internal/agentcore/foundation/tool/tool_func_test.go`

对标 Python `@tool` 装饰器。

- [ ] **Step 1: 写便捷函数测试**

```go
// tool_func_test.go
package tool

import (
	"context"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestTool_最简用法 测试 Tool(fn) 自动推断
func TestTool_最简用法(t *testing.T) {
	fn, err := Tool(searchFunc)
	if err != nil {
		t.Fatalf("Tool 失败: %v", err)
	}
	if fn.Card().Name == "" {
		t.Error("Name 不应为空")
	}
	if len(fn.Card().InputParams) == 0 {
		t.Error("InputParams 不应为空")
	}
}

// TestTool_自定义名称 测试 WithToolName
func TestTool_自定义名称(t *testing.T) {
	fn, err := Tool(searchFunc, WithToolName("custom_search"))
	if err != nil {
		t.Fatalf("Tool 失败: %v", err)
	}
	if fn.Card().Name != "custom_search" {
		t.Errorf("Name: 期望 custom_search，实际 %q", fn.Card().Name)
	}
}

// TestTool_自定义描述 测试 WithToolDescription
func TestTool_自定义描述(t *testing.T) {
	fn, err := Tool(searchFunc, WithToolDescription("自定义搜索工具"))
	if err != nil {
		t.Fatalf("Tool 失败: %v", err)
	}
	if fn.Card().Description != "自定义搜索工具" {
		t.Errorf("Description: 期望 自定义搜索工具，实际 %q", fn.Card().Description)
	}
}

// TestTool_Invoke端到端 测试 Tool 创建后的完整调用
func TestTool_Invoke端到端(t *testing.T) {
	fn, _ := Tool(searchFunc, WithToolName("search"))
	result, err := fn.Invoke(context.Background(), map[string]any{
		"query": "hello",
	})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	if result == nil {
		t.Error("result 不应为 nil")
	}
}

// TestStreamTool_最简用法 测试 StreamTool 自动推断
func TestStreamTool_最简用法(t *testing.T) {
	fn, err := StreamTool(streamSearchFunc, WithToolName("stream_search"))
	if err != nil {
		t.Fatalf("StreamTool 失败: %v", err)
	}
	if fn.Card().Name != "stream_search" {
		t.Errorf("Name: 期望 stream_search，实际 %q", fn.Card().Name)
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run "TestTool_|TestStreamTool_" -v`
Expected: FAIL

- [ ] **Step 3: 实现 Tool() / StreamTool() 便捷函数**

```go
// tool_func.go
package tool

import (
	"context"
	"reflect"
	"runtime"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// toolFuncConfig 便捷函数内部配置。
type toolFuncConfig struct {
	name        string
	description string
	inputParams []*schema.Param
	card        *ToolCard
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToolFuncOption 工具注册选项函数。
type ToolFuncOption func(*toolFuncConfig)

// WithToolName 设置工具名称（覆盖函数名）。
func WithToolName(name string) ToolFuncOption {
	return func(c *toolFuncConfig) { c.name = name }
}

// WithToolDescription 设置工具描述（覆盖自动提取）。
func WithToolDescription(desc string) ToolFuncOption {
	return func(c *toolFuncConfig) { c.description = desc }
}

// WithToolInputParams 手动设置输入参数（覆盖自动提取）。
func WithToolInputParams(params []*schema.Param) ToolFuncOption {
	return func(c *toolFuncConfig) { c.inputParams = params }
}

// WithToolCard 使用预构建的 ToolCard。
func WithToolCard(card *ToolCard) ToolFuncOption {
	return func(c *toolFuncConfig) { c.card = card }
}

// Tool 便捷注册函数（Invoke 模式），对标 Python @tool 装饰器。
//
// Go 编译器从 fn 参数自动推断 I 和 O，用户无需显式指定泛型参数。
//
// 使用示例：
//
//	fn, _ := Tool(Search)
//	fn, _ := Tool(Search, WithToolName("custom_search"), WithToolDescription("搜索工具"))
func Tool[I any, O any](fn func(context.Context, I) (O, error), opts ...ToolFuncOption) (*InvokeFunction[I, O], error) {
	cfg := &toolFuncConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 确定名称
	name := cfg.name
	if name == "" {
		name = extractFuncName(fn)
	}

	// 确定 InputParams
	var inputParams []*schema.Param
	if cfg.inputParams != nil {
		inputParams = cfg.inputParams
	} else {
		typ := reflect.TypeOf((*I)(nil)).Elem()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		extracted, err := StructSchemaExtractor{}.Extract(typ)
		if err != nil {
			return nil, err
		}
		inputParams = extracted
	}

	// 确定描述
	description := cfg.description
	if description == "" {
		typ := reflect.TypeOf((*I)(nil)).Elem()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		description = StructSchemaExtractor{}.ExtractDescription(typ)
	}
	if description == "" {
		description = name
	}

	// 构建 ToolCard
	var card *ToolCard
	if cfg.card != nil {
		card = cfg.card
	} else {
		card = NewToolCard(name, description, inputParams, nil)
	}

	return &InvokeFunction[I, O]{card: card, fn: fn}, nil
}

// StreamTool 便捷注册函数（Stream 模式）。
//
// 使用示例：
//
//	fn, _ := StreamTool(StreamSearch, WithToolName("stream_search"))
func StreamTool[I any, O any](fn func(context.Context, I) (<-chan O, error), opts ...ToolFuncOption) (*StreamFunction[I, O], error) {
	cfg := &toolFuncConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 确定名称
	name := cfg.name
	if name == "" {
		name = extractFuncNameFromReflect(fn)
	}

	// 确定 InputParams
	var inputParams []*schema.Param
	if cfg.inputParams != nil {
		inputParams = cfg.inputParams
	} else {
		typ := reflect.TypeOf((*I)(nil)).Elem()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		extracted, err := StructSchemaExtractor{}.Extract(typ)
		if err != nil {
			return nil, err
		}
		inputParams = extracted
	}

	// 确定描述
	description := cfg.description
	if description == "" {
		typ := reflect.TypeOf((*I)(nil)).Elem()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		description = StructSchemaExtractor{}.ExtractDescription(typ)
	}
	if description == "" {
		description = name
	}

	// 构建 ToolCard
	var card *ToolCard
	if cfg.card != nil {
		card = cfg.card
	} else {
		card = NewToolCard(name, description, inputParams, nil)
	}

	return &StreamFunction[I, O]{card: card, fn: fn}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractFuncName 从函数值提取函数名（Invoke 模式用）
func extractFuncName[I any, O any](fn func(context.Context, I) (O, error)) string {
	return extractFuncNameFromReflect(fn)
}

// extractFuncNameFromReflect 通过 reflect 提取函数名
func extractFuncNameFromReflect(fn any) string {
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		return ""
	}
	name := runtime.FuncForPC(v.Pointer()).Name()
	// 取最后一段，去掉包路径
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -run "TestTool_|TestStreamTool_" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/foundation/tool/tool_func.go internal/agentcore/foundation/tool/tool_func_test.go && git commit -m "feat(tool): 实现 Tool()/StreamTool() 便捷注册函数"
```

---

### Task 7: 更新 doc.go 和 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/foundation/tool/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件目录**

在 doc.go 的文件目录中添加新文件条目，更新包功能概述。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 状态**

将 3.3、3.4、3.12 的状态从 `☐` 更新为 `✅`。

- [ ] **Step 3: 运行全量测试确认**

Run: `cd /home/opensource/uap-claw-go && /usr/local/go/bin/go test ./internal/agentcore/foundation/tool/ -v -cover`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/foundation/tool/doc.go IMPLEMENTATION_PLAN.md && git commit -m "docs(tool): 更新 doc.go 和实现计划状态（3.3+3.4+3.12 完成）"
```
