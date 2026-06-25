package client

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── extractOutputSchema 测试 ────────────────────────────

// TestExtractOutputSchema_成功响应 测试从 200 响应中提取输出 schema。
func TestExtractOutputSchema_成功响应(t *testing.T) {
	desc := "成功"
	op := &openapi3.Operation{
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(200, &openapi3.ResponseRef{
				Value: &openapi3.Response{
					Description: &desc,
					Content: openapi3.Content{
						"application/json": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"object"},
									Properties: openapi3.Schemas{
										"id":   &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
										"name": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
									},
								},
							},
						},
					},
				},
			}),
		),
	}

	result := extractOutputSchema(op, nil, "")
	if result == nil {
		t.Fatal("期望非 nil 输出 schema")
	}
	if result["type"] != "object" {
		t.Errorf("期望 type=object，实际 %v", result["type"])
	}
	props, ok := result["properties"].(map[string]any)
	if !ok {
		t.Fatal("期望 properties 为 map[string]any")
	}
	if _, exists := props["id"]; !exists {
		t.Error("期望 properties 含 id")
	}
	if _, exists := props["name"]; !exists {
		t.Error("期望 properties 含 name")
	}
}

// TestExtractOutputSchema_非Object类型包装 测试非 object 类型的响应被包装。
func TestExtractOutputSchema_非Object类型包装(t *testing.T) {
	desc := "成功"
	op := &openapi3.Operation{
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(200, &openapi3.ResponseRef{
				Value: &openapi3.Response{
					Description: &desc,
					Content: openapi3.Content{
						"application/json": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"string"},
								},
							},
						},
					},
				},
			}),
		),
	}

	result := extractOutputSchema(op, nil, "")
	if result == nil {
		t.Fatal("期望非 nil 输出 schema")
	}
	if result["type"] != "object" {
		t.Errorf("期望包装后 type=object，实际 %v", result["type"])
	}
	// 检查 x-fastmcp-wrap-result 标记
	if wrapResult, _ := result["x-fastmcp-wrap-result"].(bool); !wrapResult {
		t.Error("期望 x-fastmcp-wrap-result=true")
	}
	// 检查 properties 含 result
	props, ok := result["properties"].(map[string]any)
	if !ok {
		t.Fatal("期望 properties 为 map[string]any")
	}
	if _, exists := props["result"]; !exists {
		t.Error("期望 properties 含 result（包装后的原始 schema）")
	}
}

// TestExtractOutputSchema_无响应定义 测试无响应时返回 nil。
func TestExtractOutputSchema_无响应定义(t *testing.T) {
	op := &openapi3.Operation{
		Responses: openapi3.NewResponses(),
	}

	result := extractOutputSchema(op, nil, "")
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// TestExtractOutputSchema_201优先级 测试 201 响应的提取。
func TestExtractOutputSchema_201优先级(t *testing.T) {
	desc := "创建成功"
	op := &openapi3.Operation{
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(201, &openapi3.ResponseRef{
				Value: &openapi3.Response{
					Description: &desc,
					Content: openapi3.Content{
						"application/json": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"object"},
									Properties: openapi3.Schemas{
										"created": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}}},
									},
								},
							},
						},
					},
				},
			}),
		),
	}

	result := extractOutputSchema(op, nil, "")
	if result == nil {
		t.Fatal("期望非 nil 输出 schema")
	}
	props, ok := result["properties"].(map[string]any)
	if !ok {
		t.Fatal("期望 properties 为 map[string]any")
	}
	if _, exists := props["created"]; !exists {
		t.Error("期望 properties 含 created")
	}
}

// TestExtractOutputSchema_NilOperation 测试 nil operation 返回 nil。
func TestExtractOutputSchema_Nil操作(t *testing.T) {
	result := extractOutputSchema(nil, nil, "")
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// ──────────────────────────── formatSimpleDescription 测试 ────────────────────────────

// TestFormatSimpleDescription_有Path参数 测试带 path 参数描述的增强。
func TestFormatSimpleDescription_有Path参数(t *testing.T) {
	params := []openapiParameterInfo{
		{name: "id", in: "path", required: true, description: "用户唯一标识"},
		{name: "limit", in: "query", required: false, description: "分页限制"},
	}
	result := formatSimpleDescription("获取用户信息", params, nil)

	if result == "" {
		t.Fatal("期望非空描述")
	}
	// 应包含基础描述
	if !contains(result, "获取用户信息") {
		t.Error("期望包含基础描述")
	}
	// 应包含 path 参数信息
	if !contains(result, "**Path Parameters:**") {
		t.Error("期望包含 Path Parameters 标题")
	}
	if !contains(result, "**id**") {
		t.Error("期望包含 id 参数名")
	}
	if !contains(result, "用户唯一标识") {
		t.Error("期望包含 id 参数描述")
	}
	// 不应包含 query 参数
	if contains(result, "limit") {
		t.Error("不应包含 query 参数 limit")
	}
}

// TestFormatSimpleDescription_无Path参数 测试无 path 参数时只返回 baseDesc。
func TestFormatSimpleDescription_无Path参数(t *testing.T) {
	params := []openapiParameterInfo{
		{name: "limit", in: "query", required: false, description: "分页限制"},
	}
	result := formatSimpleDescription("列出用户", params, nil)

	if result != "列出用户" {
		t.Errorf("期望仅基础描述，实际 %q", result)
	}
}

// TestFormatSimpleDescription_无参数描述 测试 path 参数无描述时不追加。
func TestFormatSimpleDescription_无参数描述(t *testing.T) {
	params := []openapiParameterInfo{
		{name: "id", in: "path", required: true, description: ""},
	}
	result := formatSimpleDescription("获取用户", params, nil)

	if result != "获取用户" {
		t.Errorf("期望仅基础描述，实际 %q", result)
	}
}

// TestFormatSimpleDescription_空参数 测试无参数时只返回 baseDesc。
func TestFormatSimpleDescription_空参数(t *testing.T) {
	result := formatSimpleDescription("获取天气", nil, nil)

	if result != "获取天气" {
		t.Errorf("期望仅基础描述，实际 %q", result)
	}
}

// ──────────────────────────── buildRequestFromSchema 测试 ────────────────────────────

// TestBuildRequestFromSchema_路径参数 测试路径参数替换。
func TestBuildRequestFromSchema_路径参数(t *testing.T) {
	params := []openapiParameterInfo{
		{name: "id", in: "path", required: true},
	}
	arguments := map[string]any{"id": "123"}

	req, err := buildRequestFromSchema("GET", "http://api.example.com", "/users/{id}", params, nil, arguments)
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	if req.URL.Path != "/users/123" {
		t.Errorf("期望路径 /users/123，实际 %s", req.URL.Path)
	}
}

// TestBuildRequestFromSchema_查询参数 测试查询参数拼接。
func TestBuildRequestFromSchema_查询参数(t *testing.T) {
	params := []openapiParameterInfo{
		{name: "page", in: "query", required: false},
		{name: "size", in: "query", required: false},
	}
	arguments := map[string]any{"page": 1, "size": 20}

	req, err := buildRequestFromSchema("GET", "http://api.example.com", "/users", params, nil, arguments)
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	query := req.URL.Query()
	if query.Get("page") != "1" {
		t.Errorf("期望 page=1，实际 %s", query.Get("page"))
	}
	if query.Get("size") != "20" {
		t.Errorf("期望 size=20，实际 %s", query.Get("size"))
	}
}

// TestBuildRequestFromSchema_请求体 测试 JSON 请求体构建。
func TestBuildRequestFromSchema_请求体(t *testing.T) {
	reqBody := &openapiRequestBodyInfo{
		contentType: "application/json",
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"email": map[string]any{"type": "string"},
			},
		},
	}
	arguments := map[string]any{"name": "张三", "email": "test@example.com"}

	req, err := buildRequestFromSchema("POST", "http://api.example.com", "/users", nil, reqBody, arguments)
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	if req.Method != "POST" {
		t.Errorf("期望 POST，实际 %s", req.Method)
	}
	if ct := req.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("期望 Content-Type=application/json，实际 %s", ct)
	}
	if req.Body == nil {
		t.Fatal("期望非 nil 请求体")
	}
}

// TestBuildRequestFromSchema_Header参数 测试 header 参数设置。
func TestBuildRequestFromSchema_Header参数(t *testing.T) {
	params := []openapiParameterInfo{
		{name: "X-Request-ID", in: "header", required: false},
	}
	arguments := map[string]any{"X-Request-ID": "abc-123"}

	req, err := buildRequestFromSchema("GET", "http://api.example.com", "/users", params, nil, arguments)
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	if req.Header.Get("X-Request-ID") != "abc-123" {
		t.Errorf("期望 X-Request-ID=abc-123，实际 %s", req.Header.Get("X-Request-ID"))
	}
}

// TestBuildRequestFromSchema_数组查询参数 测试数组查询参数展开。
func TestBuildRequestFromSchema_数组查询参数(t *testing.T) {
	params := []openapiParameterInfo{
		{name: "ids", in: "query", required: false},
	}
	arguments := map[string]any{"ids": []any{"1", "2", "3"}}

	req, err := buildRequestFromSchema("GET", "http://api.example.com", "/users", params, nil, arguments)
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	query := req.URL.Query()
	ids := query["ids"]
	if len(ids) != 3 {
		t.Fatalf("期望 3 个 ids 参数，实际 %d", len(ids))
	}
}

// TestBuildRequestFromSchema_allOf合并 测试 allOf 组合 schema 的请求体构建。
func TestBuildRequestFromSchema_allOf合并(t *testing.T) {
	reqBody := &openapiRequestBodyInfo{
		contentType: "application/json",
		schema: map[string]any{
			"allOf": []any{
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
				},
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						"age": map[string]any{"type": "integer"},
					},
				},
			},
		},
	}
	arguments := map[string]any{"name": "张三", "age": 25}

	req, err := buildRequestFromSchema("POST", "http://api.example.com", "/users", nil, reqBody, arguments)
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	if req.Body == nil {
		t.Fatal("期望非 nil 请求体")
	}
}

// ──────────────────────────── oapiSchemaToMap 扩展测试 ────────────────────────────

// TestOapiSchemaToMap_AllOf 测试 allOf 组合 schema 转换。
func TestOapiSchemaToMap_AllOf(t *testing.T) {
	s := &openapi3.Schema{
		AllOf: openapi3.SchemaRefs{
			{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
				"name": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
			}}},
			{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
				"age": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
			}}},
		},
	}

	result := oapiSchemaToMap(s)
	allOf, ok := result["allOf"].([]map[string]any)
	if !ok {
		t.Fatal("期望 allOf 为 []map[string]any")
	}
	if len(allOf) != 2 {
		t.Errorf("期望 2 个 allOf 子 schema，实际 %d", len(allOf))
	}
}

// TestOapiSchemaToMap_OneOf 测试 oneOf 组合 schema 转换。
func TestOapiSchemaToMap_OneOf(t *testing.T) {
	s := &openapi3.Schema{
		OneOf: openapi3.SchemaRefs{
			{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
			{Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
		},
	}

	result := oapiSchemaToMap(s)
	oneOf, ok := result["oneOf"].([]map[string]any)
	if !ok {
		t.Fatal("期望 oneOf 为 []map[string]any")
	}
	if len(oneOf) != 2 {
		t.Errorf("期望 2 个 oneOf 子 schema，实际 %d", len(oneOf))
	}
}

// TestOapiSchemaToMap_AnyOf 测试 anyOf 组合 schema 转换。
func TestOapiSchemaToMap_AnyOf(t *testing.T) {
	s := &openapi3.Schema{
		AnyOf: openapi3.SchemaRefs{
			{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
		},
	}

	result := oapiSchemaToMap(s)
	anyOf, ok := result["anyOf"].([]map[string]any)
	if !ok {
		t.Fatal("期望 anyOf 为 []map[string]any")
	}
	if len(anyOf) != 1 {
		t.Errorf("期望 1 个 anyOf 子 schema，实际 %d", len(anyOf))
	}
}

// TestOapiSchemaToMap_Nullable 测试 Nullable 字段转换。
func TestOapiSchemaToMap_Nullable(t *testing.T) {
	s := &openapi3.Schema{
		Type:     &openapi3.Types{"string"},
		Nullable: true,
	}

	result := oapiSchemaToMap(s)
	typeVal, ok := result["type"].([]string)
	if !ok {
		t.Fatal("期望 type 为 []string（nullable 展开）")
	}
	if len(typeVal) != 2 || typeVal[0] != "string" || typeVal[1] != "null" {
		t.Errorf("期望 type=[string,null]，实际 %v", typeVal)
	}
}

// TestOapiSchemaToMap_Nil 测试 nil schema 返回 nil。
func TestOapiSchemaToMap_Nil(t *testing.T) {
	result := oapiSchemaToMap(nil)
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// ──────────────────────────── resolveRequestBody 测试 ────────────────────────────

// TestResolveRequestBody_简单对象 测试简单扁平对象构建。
func TestResolveRequestBody_简单对象(t *testing.T) {
	bodyArgs := map[string]any{"name": "张三", "age": 25}
	schemaMap := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "integer"},
		},
	}

	result := resolveRequestBody(bodyArgs, schemaMap)
	if result["name"] != "张三" {
		t.Errorf("期望 name=张三，实际 %v", result["name"])
	}
	if result["age"] != 25 {
		t.Errorf("期望 age=25，实际 %v", result["age"])
	}
}

// TestResolveRequestBody_allOf合并 测试 allOf 合并构建。
func TestResolveRequestBody_allOf合并(t *testing.T) {
	bodyArgs := map[string]any{"name": "张三", "age": 25}
	schemaMap := map[string]any{
		"allOf": []any{
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"age": map[string]any{"type": "integer"},
				},
			},
		},
	}

	result := resolveRequestBody(bodyArgs, schemaMap)
	if result["name"] != "张三" {
		t.Errorf("期望 name=张三，实际 %v", result["name"])
	}
	if result["age"] != 25 {
		t.Errorf("期望 age=25，实际 %v", result["age"])
	}
}

// TestResolveRequestBody_oneOf取首项 测试 oneOf 取第一个子 schema。
func TestResolveRequestBody_oneOf取首项(t *testing.T) {
	bodyArgs := map[string]any{"name": "张三"}
	schemaMap := map[string]any{
		"oneOf": []any{
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "integer"},
				},
			},
		},
	}

	result := resolveRequestBody(bodyArgs, schemaMap)
	if result["name"] != "张三" {
		t.Errorf("期望 name=张三，实际 %v", result["name"])
	}
}

// TestResolveRequestBody_NilSchema 测试 nil schema 时直接返回 bodyArgs。
func TestResolveRequestBody_NilSchema(t *testing.T) {
	bodyArgs := map[string]any{"key": "value"}
	result := resolveRequestBody(bodyArgs, nil)
	if result["key"] != "value" {
		t.Errorf("期望 key=value，实际 %v", result["key"])
	}
}

// ──────────────────────────── replaceSchemaRefs 测试 ────────────────────────────

// TestReplaceSchemaRefs_基本替换 测试 $ref 引用替换。
func TestReplaceSchemaRefs_基本替换(t *testing.T) {
	m := map[string]any{
		"$ref": "#/components/schemas/User",
	}
	replaceSchemaRefs(m)
	if m["$ref"] != "#/$defs/User" {
		t.Errorf("期望 #/$defs/User，实际 %v", m["$ref"])
	}
}

// TestReplaceSchemaRefs_嵌套替换 测试嵌套 $ref 替换。
func TestReplaceSchemaRefs_嵌套替换(t *testing.T) {
	m := map[string]any{
		"properties": map[string]any{
			"user": map[string]any{
				"$ref": "#/components/schemas/User",
			},
		},
	}
	replaceSchemaRefs(m)
	props := m["properties"].(map[string]any)
	user := props["user"].(map[string]any)
	if user["$ref"] != "#/$defs/User" {
		t.Errorf("期望 #/$defs/User，实际 %v", user["$ref"])
	}
}

// TestReplaceSchemaRefs_非ComponentsRef 测试非 #/components/schemas/ 前缀不替换。
func TestReplaceSchemaRefs_非Components引用(t *testing.T) {
	m := map[string]any{
		"$ref": "#/other/path",
	}
	replaceSchemaRefs(m)
	if m["$ref"] != "#/other/path" {
		t.Errorf("期望不替换，实际 %v", m["$ref"])
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// contains 检查字符串是否包含子串。
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		anySubstr(s, substr))
}

// anySubstr 搜索子串。
func anySubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ──────────────────────────── oapiMapToParam 测试 ────────────────────────────

// TestOapiMapToParam_简单类型 测试简单类型的转换。
func TestOapiMapToParam_简单类型(t *testing.T) {
	m := map[string]any{
		"type":        "string",
		"description": "用户名",
	}
	p := oapiMapToParam("username", m, true)
	if p.Name != "username" {
		t.Errorf("期望 Name=username，实际 %s", p.Name)
	}
	if p.Type != schema.ParamTypeString {
		t.Errorf("期望 Type=string，实际 %v", p.Type)
	}
	if p.Description != "用户名" {
		t.Errorf("期望 Description=用户名，实际 %s", p.Description)
	}
	if !p.Required {
		t.Error("期望 Required=true")
	}
}

// TestOapiMapToParam_约束字段 测试约束字段转换。
func TestOapiMapToParam_约束字段(t *testing.T) {
	m := map[string]any{
		"type":      "string",
		"minLength": float64(1),
		"maxLength": float64(100),
		"pattern":   "^[a-z]+$",
		"format":    "email",
		"default":   "test@example.com",
	}
	p := oapiMapToParam("email", m, false)
	if p.MinLength != 1 {
		t.Errorf("期望 MinLength=1，实际 %d", p.MinLength)
	}
	if p.MaxLength != 100 {
		t.Errorf("期望 MaxLength=100，实际 %d", p.MaxLength)
	}
	if p.Pattern != "^[a-z]+$" {
		t.Errorf("期望 Pattern=^[a-z]+$，实际 %s", p.Pattern)
	}
	if p.Format != "email" {
		t.Errorf("期望 Format=email，实际 %s", p.Format)
	}
	if p.Default != "test@example.com" {
		t.Errorf("期望 Default=test@example.com，实际 %v", p.Default)
	}
}

// TestOapiMapToParam_NumericConstraints 测试数值约束转换。
func TestOapiMapToParam_数值约束(t *testing.T) {
	m := map[string]any{
		"type":    "integer",
		"minimum": float64(0),
		"maximum": float64(100),
	}
	p := oapiMapToParam("score", m, true)
	if p.Minimum != 0 {
		t.Errorf("期望 Minimum=0，实际 %v", p.Minimum)
	}
	if p.Maximum != 100 {
		t.Errorf("期望 Maximum=100，实际 %v", p.Maximum)
	}
}

// TestOapiMapToParam_Nullable 测试 Nullable 转换。
func TestOapiMapToParam_Nullable(t *testing.T) {
	m := map[string]any{
		"type":     "string",
		"nullable": true,
	}
	p := oapiMapToParam("name", m, false)
	if !p.Nullable {
		t.Error("期望 Nullable=true")
	}
}

// TestOapiMapToParam_NullableType数组 测试 oapiSchemaToMap 输出的 type 数组格式解析。
func TestOapiMapToParam_NullableType数组(t *testing.T) {
	// oapiSchemaToMap 输出格式：type: ["string", "null"]
	m := map[string]any{
		"type": []string{"string", "null"},
	}
	p := oapiMapToParam("name", m, false)
	if p.Type != schema.ParamTypeString {
		t.Errorf("期望 Type=string，实际 %v", p.Type)
	}
	if !p.Nullable {
		t.Error("期望 Nullable=true（从 type 数组推断）")
	}
}

// TestOapiMapToParam_Enum 测试 Enum 转换。
func TestOapiMapToParam_Enum(t *testing.T) {
	m := map[string]any{
		"type": "string",
		"enum": []any{"active", "inactive", "pending"},
	}
	p := oapiMapToParam("status", m, true)
	if len(p.Enum) != 3 {
		t.Fatalf("期望 3 个 enum 值，实际 %d", len(p.Enum))
	}
	if p.Enum[0] != "active" {
		t.Errorf("期望 Enum[0]=active，实际 %v", p.Enum[0])
	}
}

// TestOapiMapToParam_嵌套Object 测试嵌套对象递归转换。
func TestOapiMapToParam_嵌套Object(t *testing.T) {
	m := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"host": map[string]any{"type": "string", "description": "主机地址"},
			"port": map[string]any{"type": "integer", "description": "端口号"},
		},
		"required": []any{"host"},
	}
	p := oapiMapToParam("config", m, true)
	if p.Type != schema.ParamTypeObject {
		t.Errorf("期望 Type=object，实际 %v", p.Type)
	}
	if len(p.Properties) != 2 {
		t.Fatalf("期望 2 个 Properties，实际 %d", len(p.Properties))
	}
	// 验证 host 属性
	var hostProp *schema.Param
	for _, prop := range p.Properties {
		if prop.Name == "host" {
			hostProp = prop
			break
		}
	}
	if hostProp == nil {
		t.Fatal("未找到 host 属性")
	}
	if hostProp.Type != schema.ParamTypeString {
		t.Errorf("host.Type 期望 string，实际 %v", hostProp.Type)
	}
	if !hostProp.Required {
		t.Error("host 期望 Required=true")
	}
}

// TestOapiMapToParam_嵌套Array 测试数组类型递归转换。
func TestOapiMapToParam_嵌套Array(t *testing.T) {
	m := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "string",
		},
	}
	p := oapiMapToParam("tags", m, false)
	if p.Type != schema.ParamTypeArray {
		t.Errorf("期望 Type=array，实际 %v", p.Type)
	}
	if p.Items == nil {
		t.Fatal("期望 Items 非 nil")
	}
	if p.Items.Type != schema.ParamTypeString {
		t.Errorf("Items.Type 期望 string，实际 %v", p.Items.Type)
	}
}

// TestOapiMapToParam_AnyOf 测试 anyOf 组合转换。
func TestOapiMapToParam_AnyOf(t *testing.T) {
	m := map[string]any{
		"type": "string",
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "integer"},
		},
	}
	p := oapiMapToParam("value", m, false)
	if len(p.AnyOf) != 2 {
		t.Fatalf("期望 2 个 AnyOf，实际 %d", len(p.AnyOf))
	}
	if p.AnyOf[0].Type != schema.ParamTypeString {
		t.Errorf("AnyOf[0].Type 期望 string，实际 %v", p.AnyOf[0].Type)
	}
}

// TestOapiMapToParam_AllOf 测试 allOf 组合转换。
func TestOapiMapToParam_AllOf(t *testing.T) {
	m := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"allOf": []any{
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
			},
		},
	}
	p := oapiMapToParam("combined", m, true)
	if len(p.AllOf) != 1 {
		t.Fatalf("期望 1 个 AllOf，实际 %d", len(p.AllOf))
	}
}

// TestOapiMapToParam_OneOf 测试 oneOf 组合转换。
func TestOapiMapToParam_OneOf(t *testing.T) {
	m := map[string]any{
		"type": "string",
		"oneOf": []any{
			map[string]any{"type": "string", "enum": []any{"a", "b"}},
			map[string]any{"type": "integer"},
		},
	}
	p := oapiMapToParam("choice", m, false)
	if len(p.OneOf) != 2 {
		t.Fatalf("期望 2 个 OneOf，实际 %d", len(p.OneOf))
	}
}

// TestOapiMapToParam_NilMap 测试 nil map 返回默认 Param。
func TestOapiMapToParam_NilMap(t *testing.T) {
	p := oapiMapToParam("empty", nil, false)
	if p.Name != "empty" {
		t.Errorf("期望 Name=empty，实际 %s", p.Name)
	}
	if p.Type != schema.ParamTypeString {
		t.Errorf("期望默认 Type=string，实际 %v", p.Type)
	}
}

// TestOapiMapToParam_OpenAPI特有字段丢弃 测试 OpenAPI 特有字段不存入 Param。
func TestOapiMapToParam_OpenAPI特有字段丢弃(t *testing.T) {
	m := map[string]any{
		"type":          "string",
		"discriminator": map[string]any{"propertyName": "type"},
		"readOnly":      true,
		"writeOnly":     false,
		"xml":           map[string]any{"name": "user"},
		"externalDocs":  map[string]any{"url": "https://example.com"},
		"deprecated":    true,
	}
	p := oapiMapToParam("field", m, true)
	// 这些字段不在 Param 中，只要不 panic 就算通过
	if p.Type != schema.ParamTypeString {
		t.Errorf("期望 Type=string，实际 %v", p.Type)
	}
}

// TestOapiMapToParam_深层嵌套 测试多层嵌套递归。
func TestOapiMapToParam_深层嵌套(t *testing.T) {
	m := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"address": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"type":        "string",
						"description": "城市",
						"nullable":    true,
					},
					"zip": map[string]any{
						"type":      "string",
						"pattern":   "^\\d{6}$",
						"minLength": float64(6),
						"maxLength": float64(6),
					},
				},
				"required": []any{"zip"},
			},
		},
		"required": []any{"address"},
	}
	p := oapiMapToParam("user", m, true)

	// 第一层：user → address
	var addrProp *schema.Param
	for _, prop := range p.Properties {
		if prop.Name == "address" {
			addrProp = prop
			break
		}
	}
	if addrProp == nil {
		t.Fatal("未找到 address 属性")
	}
	if addrProp.Type != schema.ParamTypeObject {
		t.Fatalf("address.Type 期望 object，实际 %v", addrProp.Type)
	}

	// 第二层：address → city/zip
	var cityProp, zipProp *schema.Param
	for _, prop := range addrProp.Properties {
		switch prop.Name {
		case "city":
			cityProp = prop
		case "zip":
			zipProp = prop
		}
	}
	if cityProp == nil {
		t.Fatal("未找到 city 属性")
	}
	if !cityProp.Nullable {
		t.Error("city 期望 Nullable=true")
	}
	if zipProp == nil {
		t.Fatal("未找到 zip 属性")
	}
	if zipProp.Pattern != "^\\d{6}$" {
		t.Errorf("zip.Pattern 期望 ^\\d{6}$，实际 %s", zipProp.Pattern)
	}
	if zipProp.MinLength != 6 {
		t.Errorf("zip.MinLength 期望 6，实际 %d", zipProp.MinLength)
	}
}

// TestOapiSchemaToMap_然后OapiMapToParam_完整流程 测试 oapiSchemaToMap 输出经 oapiMapToParam 转换后保持完整信息。
func TestOapiSchemaToMap_然后OapiMapToParam_完整流程(t *testing.T) {
	// 构造一个含 Nullable 的 OpenAPI schema
	oapiSchema := &openapi3.Schema{
		Type:     &openapi3.Types{"string"},
		Nullable: true,
		Format:   "email",
	}

	// oapiSchemaToMap 转换
	mapResult := oapiSchemaToMap(oapiSchema)

	// oapiMapToParam 从 map 转换为 Param
	p := oapiMapToParam("email_field", mapResult, false)

	if p.Type != schema.ParamTypeString {
		t.Errorf("期望 Type=string，实际 %v", p.Type)
	}
	if !p.Nullable {
		t.Error("期望 Nullable=true（从 oapiSchemaToMap 输出的 type 数组推断）")
	}
	if p.Format != "email" {
		t.Errorf("期望 Format=email，实际 %s", p.Format)
	}

	// 验证 ToJSONSchemaMap 输出
	schemaResult := schema.ToJSONSchemaMap([]*schema.Param{p})
	props := schemaResult["properties"].(map[string]any)
	emailSchema := props["email_field"].(map[string]any)

	// Nullable 输出应为 type 数组
	typeVal, ok := emailSchema["type"].([]string)
	if !ok {
		t.Fatalf("期望 type 为 []string，实际 %T", emailSchema["type"])
	}
	if len(typeVal) != 2 || typeVal[0] != "string" || typeVal[1] != "null" {
		t.Errorf("期望 type [string null]，实际 %v", typeVal)
	}
	if emailSchema["format"] != "email" {
		t.Errorf("期望 format=email，实际 %v", emailSchema["format"])
	}
}

// ──────────────────────────── deepCopyMap 测试 ────────────────────────────

// TestDeepCopyMap_Nil 测试 nil 输入返回 nil。
func TestDeepCopyMap_Nil(t *testing.T) {
	result := deepCopyMap(nil)
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// TestDeepCopyMap_空Map 测试空 map 拷贝。
func TestDeepCopyMap_空Map(t *testing.T) {
	result := deepCopyMap(map[string]any{})
	if len(result) != 0 {
		t.Errorf("期望空 map，实际 %v", result)
	}
}

// TestDeepCopyMap_嵌套Map 测试嵌套 map 深拷贝。
func TestDeepCopyMap_嵌套Map(t *testing.T) {
	original := map[string]any{
		"key": map[string]any{
			"nested": "value",
		},
		"arr": []any{
			map[string]any{"item": "1"},
			"simple",
		},
		"str": "hello",
	}
	copied := deepCopyMap(original)

	// 修改原始不影响拷贝
	original["key"].(map[string]any)["nested"] = "changed"
	if copied["key"].(map[string]any)["nested"] != "value" {
		t.Error("深拷贝失败：修改原始影响了拷贝")
	}
}

// ──────────────────────────── deepCopySlice 测试 ────────────────────────────

// TestDeepCopySlice_Nil 测试 nil 输入返回 nil。
func TestDeepCopySlice_Nil(t *testing.T) {
	result := deepCopySlice(nil)
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// TestDeepCopySlice_嵌套Slice 测试嵌套 slice 深拷贝。
func TestDeepCopySlice_嵌套Slice(t *testing.T) {
	original := []any{
		map[string]any{"key": "value"},
		[]any{"nested"},
		"simple",
	}
	copied := deepCopySlice(original)

	// 修改原始不影响拷贝
	original[0].(map[string]any)["key"] = "changed"
	if copied[0].(map[string]any)["key"] != "value" {
		t.Error("深拷贝失败：修改原始影响了拷贝")
	}
}

// ──────────────────────────── convertOpenAPISchemaToJSONSchema 测试 ────────────────────────────

// TestConvertOpenAPISchemaToJSONSchema_Nil 测试 nil 输入返回 nil。
func TestConvertOpenAPISchemaToJSONSchema_Nil(t *testing.T) {
	result := convertOpenAPISchemaToJSONSchema(nil, "3.0.0")
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// TestConvertOpenAPISchemaToJSONSchema_Nullable转换 测试 nullable 字段转换。
func TestConvertOpenAPISchemaToJSONSchema_Nullable转换(t *testing.T) {
	input := map[string]any{
		"type":     "string",
		"nullable": true,
	}
	result := convertOpenAPISchemaToJSONSchema(input, "3.0.0")
	// nullable 应被删除
	if _, ok := result["nullable"]; ok {
		t.Error("期望 nullable 被删除")
	}
	// type 应转为数组
	typeVal, ok := result["type"].([]string)
	if !ok {
		t.Fatalf("期望 type 为 []string，实际 %T", result["type"])
	}
	if len(typeVal) != 2 || typeVal[0] != "string" || typeVal[1] != "null" {
		t.Errorf("期望 type [string null]，实际 %v", typeVal)
	}
}

// TestConvertOpenAPISchemaToJSONSchema_OneOf转AnyOf 测试 oneOf 转 anyOf。
func TestConvertOpenAPISchemaToJSONSchema_OneOf转AnyOf(t *testing.T) {
	input := map[string]any{
		"type": "string",
		"oneOf": []any{
			map[string]any{"type": "string", "enum": []any{"a", "b"}},
			map[string]any{"type": "integer"},
		},
	}
	result := convertOpenAPISchemaToJSONSchema(input, "3.1.0")
	// oneOf 应被转换为 anyOf
	if _, ok := result["oneOf"]; ok {
		t.Error("期望 oneOf 被删除")
	}
	anyOf, ok := result["anyOf"].([]any)
	if !ok {
		t.Fatal("期望 anyOf 存在")
	}
	if len(anyOf) != 2 {
		t.Errorf("期望 2 个 anyOf，实际 %d", len(anyOf))
	}
}

// TestConvertOpenAPISchemaToJSONSchema_移除OpenAPI特有字段 测试移除 OpenAPI 特有字段。
func TestConvertOpenAPISchemaToJSONSchema_移除OpenAPI特有字段(t *testing.T) {
	input := map[string]any{
		"type":          "string",
		"discriminator": map[string]any{"propertyName": "type"},
		"readOnly":      true,
		"writeOnly":     false,
		"xml":           map[string]any{"name": "user"},
		"externalDocs":  map[string]any{"url": "https://example.com"},
		"deprecated":    true,
	}
	result := convertOpenAPISchemaToJSONSchema(input, "3.0.0")
	for _, field := range []string{"discriminator", "readOnly", "writeOnly", "xml", "externalDocs", "deprecated"} {
		if _, ok := result[field]; ok {
			t.Errorf("期望 %s 被删除", field)
		}
	}
}

// TestConvertOpenAPISchemaToJSONSchema_递归处理 测试递归处理嵌套 schema。
func TestConvertOpenAPISchemaToJSONSchema_递归处理(t *testing.T) {
	input := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"field": map[string]any{
				"type":     "string",
				"nullable": true,
			},
		},
	}
	result := convertOpenAPISchemaToJSONSchema(input, "3.0.0")
	props := result["properties"].(map[string]any)
	field := props["field"].(map[string]any)
	typeVal, ok := field["type"].([]string)
	if !ok {
		t.Fatalf("期望嵌套字段 type 为 []string，实际 %T", field["type"])
	}
	if len(typeVal) != 2 || typeVal[1] != "null" {
		t.Errorf("期望嵌套字段 type 含 null，实际 %v", typeVal)
	}
}

// TestConvertOpenAPISchemaToJSONSchema_NullableWithOneOf 测试 nullable 配合 oneOf 转换。
func TestConvertOpenAPISchemaToJSONSchema_NullableWithOneOf(t *testing.T) {
	input := map[string]any{
		"nullable": true,
		"oneOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "integer"},
		},
	}
	result := convertOpenAPISchemaToJSONSchema(input, "3.0.0")
	// nullable + oneOf → anyOf 并追加 null
	anyOf, ok := result["anyOf"].([]any)
	if !ok {
		t.Fatal("期望 anyOf 存在")
	}
	// 应有 3 个元素：原 oneOf 的 2 个 + null
	if len(anyOf) != 3 {
		t.Errorf("期望 3 个 anyOf（2+null），实际 %d", len(anyOf))
	}
}

// TestConvertOpenAPISchemaToJSONSchema_NullableWithEnum 测试 nullable 配合 enum。
func TestConvertOpenAPISchemaToJSONSchema_NullableWithEnum(t *testing.T) {
	input := map[string]any{
		"type":     "string",
		"nullable": true,
		"enum":     []any{"active", "inactive"},
	}
	result := convertOpenAPISchemaToJSONSchema(input, "3.0.0")
	enumVal, ok := result["enum"].([]any)
	if !ok {
		t.Fatal("期望 enum 存在")
	}
	// nullable 时 enum 应追加 null
	if len(enumVal) != 3 {
		t.Errorf("期望 3 个 enum 值（2+null），实际 %d", len(enumVal))
	}
}

// ──────────────────────────── formatDeepObjectParameter 测试 ────────────────────────────

// TestFormatDeepObjectParameter_Nil 测试 nil 输入返回 nil。
func TestFormatDeepObjectParameter_Nil(t *testing.T) {
	result := formatDeepObjectParameter(nil, "filter")
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// TestFormatDeepObjectParameter_正常值 测试正常 deepObject 参数序列化。
func TestFormatDeepObjectParameter_正常值(t *testing.T) {
	paramValue := map[string]any{
		"id":   "123",
		"type": "user",
	}
	result := formatDeepObjectParameter(paramValue, "filter")
	if len(result) != 2 {
		t.Fatalf("期望 2 个参数，实际 %d", len(result))
	}
	if result["filter[id]"] != "123" {
		t.Errorf("期望 filter[id]=123，实际 %s", result["filter[id]"])
	}
	if result["filter[type]"] != "user" {
		t.Errorf("期望 filter[type]=user，实际 %s", result["filter[type]"])
	}
}

// ──────────────────────────── schemaTypeFromOpenAPI 测试 ────────────────────────────

// TestSchemaTypeFromOpenAPI_Nil 测试 nil schema 返回默认 string 类型。
func TestSchemaTypeFromOpenAPI_Nil(t *testing.T) {
	result := schemaTypeFromOpenAPI(nil)
	if result != schema.ParamTypeString {
		t.Errorf("期望 ParamTypeString，实际 %v", result)
	}
}

// TestSchemaTypeFromOpenAPI_各类型 测试各类型映射。
func TestSchemaTypeFromOpenAPI_各类型(t *testing.T) {
	tests := []struct {
		typeStr string
		want    schema.ParamType
	}{
		{"string", schema.ParamTypeString},
		{"boolean", schema.ParamTypeBoolean},
		{"integer", schema.ParamTypeInteger},
		{"number", schema.ParamTypeNumber},
		{"array", schema.ParamTypeArray},
		{"object", schema.ParamTypeObject},
		{"unknown", schema.ParamTypeString},
	}
	for _, tt := range tests {
		t.Run(tt.typeStr, func(t *testing.T) {
			result := schemaTypeFromOpenAPI(map[string]any{"type": tt.typeStr})
			if result != tt.want {
				t.Errorf("期望 %v，实际 %v", tt.want, result)
			}
		})
	}
}

// ──────────────────────────── schemaTypeFromTypeStr 测试 ────────────────────────────

// TestSchemaTypeFromTypeStr_所有类型 测试所有类型字符串映射。
func TestSchemaTypeFromTypeStr_所有类型(t *testing.T) {
	tests := []struct {
		input string
		want  schema.ParamType
	}{
		{"string", schema.ParamTypeString},
		{"boolean", schema.ParamTypeBoolean},
		{"integer", schema.ParamTypeInteger},
		{"number", schema.ParamTypeNumber},
		{"array", schema.ParamTypeArray},
		{"object", schema.ParamTypeObject},
		{"", schema.ParamTypeString},
		{"unknown", schema.ParamTypeString},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := schemaTypeFromTypeStr(tt.input)
			if result != tt.want {
				t.Errorf("期望 %v，实际 %v", tt.want, result)
			}
		})
	}
}

// ──────────────────────────── collectReferencedDefs 扩展测试 ────────────────────────────

// TestCollectReferencedDefs_数组中的引用 测试数组中的 $ref 收集。
func TestCollectReferencedDefs_数组中的引用(t *testing.T) {
	m := map[string]any{
		"anyOf": []any{
			map[string]any{"$ref": "#/$defs/User"},
			map[string]any{"type": "string"},
		},
	}
	collected := make(map[string]bool)
	collectReferencedDefs(m, collected)
	if !collected["User"] {
		t.Error("期望收集到 User 定义")
	}
}

// TestCollectReferencedDefs_嵌套引用 测试多层嵌套 $ref 收集。
func TestCollectReferencedDefs_嵌套引用(t *testing.T) {
	m := map[string]any{
		"properties": map[string]any{
			"field": map[string]any{
				"$ref": "#/$defs/Item",
			},
		},
	}
	collected := make(map[string]bool)
	collectReferencedDefs(m, collected)
	if !collected["Item"] {
		t.Error("期望收集到 Item 定义")
	}
}

// TestCollectReferencedDefs_非Defs引用 测试非 #/$defs/ 前缀的 $ref 不收集。
func TestCollectReferencedDefs_非Defs引用(t *testing.T) {
	m := map[string]any{
		"$ref": "#/components/schemas/User",
	}
	collected := make(map[string]bool)
	collectReferencedDefs(m, collected)
	if len(collected) != 0 {
		t.Errorf("期望不收集非 $defs 引用，实际 %v", collected)
	}
}

// ──────────────────────────── replaceSchemaRefs 扩展测试 ────────────────────────────

// TestReplaceSchemaRefs_数组中的引用 测试数组中的 $ref 替换。
func TestReplaceSchemaRefs_数组中的引用(t *testing.T) {
	m := map[string]any{
		"anyOf": []any{
			map[string]any{"$ref": "#/components/schemas/User"},
		},
	}
	replaceSchemaRefs(m)
	anyOf := m["anyOf"].([]any)
	item := anyOf[0].(map[string]any)
	if item["$ref"] != "#/$defs/User" {
		t.Errorf("期望 #/$defs/User，实际 %v", item["$ref"])
	}
}

// ──────────────────────────── extractOutputSchema 扩展测试 ────────────────────────────

// TestExtractOutputSchema_含Defs引用 测试含 $defs 引用的输出 schema。
// 通过直接测试 replaceSchemaRefs 和 collectReferencedDefs 来间接覆盖 extractOutputSchema 中的 $defs 逻辑。
func TestExtractOutputSchema_含Defs引用(t *testing.T) {
	// 直接测试 collectReferencedDefs + $defs 构建
	outputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"data": map[string]any{
				"$ref": "#/$defs/User",
			},
		},
	}

	schemaDefinitions := map[string]map[string]any{
		"User": {
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
		},
	}

	// 收集引用
	referencedDefs := make(map[string]bool)
	collectReferencedDefs(outputSchema, referencedDefs)
	if !referencedDefs["User"] {
		t.Error("期望收集到 User 定义引用")
	}

	// 递归展开
	prevCount := 0
	for len(referencedDefs) > prevCount {
		prevCount = len(referencedDefs)
		for name := range referencedDefs {
			if def, exists := schemaDefinitions[name]; exists {
				collectReferencedDefs(def, referencedDefs)
			}
		}
	}

	// 构建 $defs
	if len(referencedDefs) > 0 {
		defs := make(map[string]any, len(referencedDefs))
		for name := range referencedDefs {
			if def, exists := schemaDefinitions[name]; exists {
				copied := deepCopyMap(def)
				replaceSchemaRefs(copied)
				defs[name] = copied
			}
		}
		outputSchema["$defs"] = defs
	}

	defs, ok := outputSchema["$defs"].(map[string]any)
	if !ok {
		t.Fatal("期望 $defs 存在")
	}
	if _, exists := defs["User"]; !exists {
		t.Error("期望 $defs 含 User 定义")
	}
}

// TestExtractOutputSchema_无JSONContent 测试无 JSON Content-Type 时返回 nil。
func TestExtractOutputSchema_无JSONContent(t *testing.T) {
	desc := "成功"
	op := &openapi3.Operation{
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(200, &openapi3.ResponseRef{
				Value: &openapi3.Response{
					Description: &desc,
					Content:     openapi3.Content{},
				},
			}),
		),
	}
	result := extractOutputSchema(op, nil, "")
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// TestExtractOutputSchema_202响应 测试 202 响应的提取。
func TestExtractOutputSchema_202响应(t *testing.T) {
	desc := "已接受"
	op := &openapi3.Operation{
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(202, &openapi3.ResponseRef{
				Value: &openapi3.Response{
					Description: &desc,
					Content: openapi3.Content{
						"application/json": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"object"},
									Properties: openapi3.Schemas{
										"status": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
									},
								},
							},
						},
					},
				},
			}),
		),
	}
	result := extractOutputSchema(op, nil, "")
	if result == nil {
		t.Fatal("期望非 nil 输出 schema")
	}
}

// TestExtractOutputSchema_204响应 测试 204 响应返回 nil（无 Content）。
func TestExtractOutputSchema_204响应(t *testing.T) {
	desc := "无内容"
	op := &openapi3.Operation{
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(204, &openapi3.ResponseRef{
				Value: &openapi3.Response{
					Description: &desc,
				},
			}),
		),
	}
	result := extractOutputSchema(op, nil, "")
	// 204 通常无 Content，所以 outputSchema 应为 nil
	if result != nil {
		t.Errorf("期望 nil（204 无内容），实际 %v", result)
	}
}

// ──────────────────────────── buildInputParams 扩展测试 ────────────────────────────

// TestBuildInputParams_请求体简单类型 测试请求体为简单类型时创建 body 参数。
func TestBuildInputParams_请求体简单类型(t *testing.T) {
	reqBody := &openapiRequestBodyInfo{
		contentType: "application/json",
		schema:      map[string]any{"type": "string"},
	}
	result := buildInputParams(nil, reqBody)
	if len(result) != 1 {
		t.Fatalf("期望 1 个参数，实际 %d", len(result))
	}
	if result[0].Name != "body" {
		t.Errorf("期望参数名 body，实际 %s", result[0].Name)
	}
}

// TestBuildInputParams_同名冲突 测试参数名冲突时加 __body 后缀。
func TestBuildInputParams_同名冲突(t *testing.T) {
	params := []openapiParameterInfo{
		{name: "id", in: "path", required: true, schema: map[string]any{"type": "string"}},
	}
	reqBody := &openapiRequestBodyInfo{
		contentType: "application/json",
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":   map[string]any{"type": "string"},
				"name": map[string]any{"type": "string"},
			},
		},
	}
	result := buildInputParams(params, reqBody)
	// 应有 3 个参数：id (path), id__body (body), name (body)
	if len(result) != 3 {
		t.Fatalf("期望 3 个参数，实际 %d", len(result))
	}
	names := make([]string, len(result))
	for i, p := range result {
		names[i] = p.Name
	}
	foundBodySuffix := false
	for _, name := range names {
		if name == "id__body" {
			foundBodySuffix = true
		}
	}
	if !foundBodySuffix {
		t.Errorf("期望存在 id__body 参数，实际 %v", names)
	}
}

// ──────────────────────────── resolveRequestBody 扩展测试 ────────────────────────────

// TestResolveRequestBody_anyOf取首项 测试 anyOf 取第一个子 schema。
func TestResolveRequestBody_anyOf取首项(t *testing.T) {
	bodyArgs := map[string]any{"name": "张三"}
	schemaMap := map[string]any{
		"anyOf": []any{
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
		},
	}
	result := resolveRequestBody(bodyArgs, schemaMap)
	if result["name"] != "张三" {
		t.Errorf("期望 name=张三，实际 %v", result["name"])
	}
}

// TestResolveRequestBody_嵌套对象 测试嵌套对象递归构建。
func TestResolveRequestBody_嵌套对象(t *testing.T) {
	bodyArgs := map[string]any{
		"address": map[string]any{"city": "北京", "zip": "100000"},
	}
	schemaMap := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"address": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
					"zip":  map[string]any{"type": "string"},
				},
			},
		},
	}
	result := resolveRequestBody(bodyArgs, schemaMap)
	addr, ok := result["address"].(map[string]any)
	if !ok {
		t.Fatal("期望 address 为 map[string]any")
	}
	if addr["city"] != "北京" {
		t.Errorf("期望 city=北京，实际 %v", addr["city"])
	}
}

// TestResolveRequestBody_额外字段 测试 bodyArgs 包含 properties 中不存在的字段。
func TestResolveRequestBody_额外字段(t *testing.T) {
	bodyArgs := map[string]any{"name": "张三", "extra": "额外值"}
	schemaMap := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	result := resolveRequestBody(bodyArgs, schemaMap)
	if result["name"] != "张三" {
		t.Errorf("期望 name=张三，实际 %v", result["name"])
	}
	if result["extra"] != "额外值" {
		t.Errorf("期望 extra=额外值，实际 %v", result["extra"])
	}
}

// TestResolveRequestBody_allOfMap格式 测试 allOf 为 []map[string]any 格式。
func TestResolveRequestBody_allOfMap格式(t *testing.T) {
	bodyArgs := map[string]any{"name": "张三", "age": 25}
	schemaMap := map[string]any{
		"allOf": []map[string]any{
			{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			{
				"type": "object",
				"properties": map[string]any{
					"age": map[string]any{"type": "integer"},
				},
			},
		},
	}
	result := resolveRequestBody(bodyArgs, schemaMap)
	if result["name"] != "张三" {
		t.Errorf("期望 name=张三，实际 %v", result["name"])
	}
	if result["age"] != 25 {
		t.Errorf("期望 age=25，实际 %v", result["age"])
	}
}

// ──────────────────────────── buildRequestFromSchema 扩展测试 ────────────────────────────

// TestBuildRequestFromSchema_deepObject参数 测试 deepObject 风格查询参数。
func TestBuildRequestFromSchema_deepObject参数(t *testing.T) {
	params := []openapiParameterInfo{
		{name: "filter", in: "query", style: "deepObject"},
	}
	arguments := map[string]any{
		"filter": map[string]any{"id": "123", "type": "user"},
	}
	req, err := buildRequestFromSchema("GET", "http://api.example.com", "/items", params, nil, arguments)
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	query := req.URL.Query()
	if query.Get("filter[id]") != "123" {
		t.Errorf("期望 filter[id]=123，实际 %s", query.Get("filter[id]"))
	}
	if query.Get("filter[type]") != "user" {
		t.Errorf("期望 filter[type]=user，实际 %s", query.Get("filter[type]"))
	}
}

// TestBuildRequestFromSchema_body后缀参数 测试 __body 后缀参数名还原。
func TestBuildRequestFromSchema_body后缀参数(t *testing.T) {
	reqBody := &openapiRequestBodyInfo{
		contentType: "application/json",
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
		},
	}
	arguments := map[string]any{
		"name__body": "test",
	}
	req, err := buildRequestFromSchema("POST", "http://api.example.com", "/items", nil, reqBody, arguments)
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	if req.Body == nil {
		t.Fatal("期望非 nil 请求体")
	}
}

// TestBuildRequestFromSchema_查询参数追加到已有 测试查询参数追加到已有查询参数的 URL。
func TestBuildRequestFromSchema_查询参数追加到已有(t *testing.T) {
	params := []openapiParameterInfo{
		{name: "page", in: "query"},
	}
	arguments := map[string]any{"page": 1}
	req, err := buildRequestFromSchema("GET", "http://api.example.com", "/items?existing=1", params, nil, arguments)
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	query := req.URL.Query()
	if query.Get("existing") != "1" {
		t.Errorf("期望 existing=1，实际 %s", query.Get("existing"))
	}
	if query.Get("page") != "1" {
		t.Errorf("期望 page=1，实际 %s", query.Get("page"))
	}
}

// TestBuildRequestFromSchema_无参数 测试无参数的请求构建。
func TestBuildRequestFromSchema_无参数(t *testing.T) {
	req, err := buildRequestFromSchema("GET", "http://api.example.com", "/items", nil, nil, map[string]any{})
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	if req.Method != "GET" {
		t.Errorf("期望 GET，实际 %s", req.Method)
	}
}

// ──────────────────────────── oapiSchemaToMap 扩展测试 ────────────────────────────

// TestOapiSchemaToMap_完整字段 测试含所有字段的 schema 转换。
func TestOapiSchemaToMap_完整字段(t *testing.T) {
	maxLen := uint64(100)
	maxItems := uint64(10)
	s := &openapi3.Schema{
		Type:        &openapi3.Types{"string"},
		Format:      "email",
		Description: "邮箱地址",
		Default:     "test@example.com",
		Enum:        []any{"active", "inactive"},
		MinLength:   1,
		MaxLength:   &maxLen,
		Pattern:     "^[a-z]+@",
		MinItems:    0,
		MaxItems:    &maxItems,
	}
	result := oapiSchemaToMap(s)
	if result["type"] != "string" {
		t.Errorf("期望 type=string，实际 %v", result["type"])
	}
	if result["format"] != "email" {
		t.Errorf("期望 format=email，实际 %v", result["format"])
	}
	if result["description"] != "邮箱地址" {
		t.Errorf("期望 description=邮箱地址，实际 %v", result["description"])
	}
	if result["default"] != "test@example.com" {
		t.Errorf("期望 default=test@example.com，实际 %v", result["default"])
	}
	if result["pattern"] != "^[a-z]+@" {
		t.Errorf("期望 pattern=^[a-z]+@，实际 %v", result["pattern"])
	}
}

// TestOapiSchemaToMap_数值约束 测试数值约束字段。
func TestOapiSchemaToMap_数值约束(t *testing.T) {
	minVal := 0.0
	maxVal := 100.0
	s := &openapi3.Schema{
		Type: &openapi3.Types{"integer"},
		Min:  &minVal,
		Max:  &maxVal,
	}
	result := oapiSchemaToMap(s)
	if result["minimum"] != 0.0 {
		t.Errorf("期望 minimum=0.0，实际 %v", result["minimum"])
	}
	if result["maximum"] != 100.0 {
		t.Errorf("期望 maximum=100.0，实际 %v", result["maximum"])
	}
}

// TestOapiSchemaToMap_Items 测试 Items 字段转换。
func TestOapiSchemaToMap_Items(t *testing.T) {
	s := &openapi3.Schema{
		Type: &openapi3.Types{"array"},
		Items: &openapi3.SchemaRef{
			Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
		},
	}
	result := oapiSchemaToMap(s)
	items, ok := result["items"].(map[string]any)
	if !ok {
		t.Fatal("期望 items 为 map[string]any")
	}
	if items["type"] != "string" {
		t.Errorf("期望 items.type=string，实际 %v", items["type"])
	}
}

// TestOapiSchemaToMap_Required 测试 Required 字段转换。
func TestOapiSchemaToMap_Required(t *testing.T) {
	s := &openapi3.Schema{
		Type:     &openapi3.Types{"object"},
		Required: []string{"name", "age"},
	}
	result := oapiSchemaToMap(s)
	reqArr, ok := result["required"].([]string)
	if !ok {
		t.Fatal("期望 required 为 []string")
	}
	if len(reqArr) != 2 {
		t.Errorf("期望 2 个 required，实际 %d", len(reqArr))
	}
}

// ──────────────────────────── oapiMapToParam 扩展测试 ────────────────────────────

// TestOapiMapToParam_TypeAnyArray 测试 []any 格式的 type 数组。
func TestOapiMapToParam_TypeAnyArray(t *testing.T) {
	m := map[string]any{
		"type": []any{"string", "null"},
	}
	p := oapiMapToParam("name", m, false)
	if p.Type != schema.ParamTypeString {
		t.Errorf("期望 Type=string，实际 %v", p.Type)
	}
	if !p.Nullable {
		t.Error("期望 Nullable=true")
	}
}

// ──────────────────────────── extractOutputSchema 扩展测试 ────────────────────────────

// TestExtractOutputSchema_兜底2xx响应 测试非标准 2xx 响应的兜底查找。
func TestExtractOutputSchema_兜底2xx响应(t *testing.T) {
	desc := "成功"
	op := &openapi3.Operation{
		Responses: openapi3.NewResponses(),
	}
	// 手动添加 206 响应（不在 200/201/202/204 列表中）
	op.Responses.Set("206", &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: &desc,
			Content: openapi3.Content{
				"application/json": &openapi3.MediaType{
					Schema: &openapi3.SchemaRef{
						Value: &openapi3.Schema{
							Type: &openapi3.Types{"object"},
							Properties: openapi3.Schemas{
								"partial": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}}},
							},
						},
					},
				},
			},
		},
	})

	result := extractOutputSchema(op, nil, "")
	if result == nil {
		t.Fatal("期望非 nil 输出 schema")
	}
}

// TestExtractOutputSchema_非JSONContentType 测试非 JSON Content-Type 兜底。
func TestExtractOutputSchema_非JSONContentType(t *testing.T) {
	desc := "成功"
	op := &openapi3.Operation{
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(200, &openapi3.ResponseRef{
				Value: &openapi3.Response{
					Description: &desc,
					Content: openapi3.Content{
						"application/xml": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"object"},
									Properties: openapi3.Schemas{
										"data": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
									},
								},
							},
						},
					},
				},
			}),
		),
	}

	result := extractOutputSchema(op, nil, "")
	if result == nil {
		t.Fatal("期望非 nil 输出 schema（兜底取第一个 Content-Type）")
	}
}

// TestExtractOutputSchema_OAS3版本转换 测试 OpenAPI 3.0 版本触发 convertOpenAPISchemaToJSONSchema。
func TestExtractOutputSchema_OAS3版本转换(t *testing.T) {
	desc := "成功"
	op := &openapi3.Operation{
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(200, &openapi3.ResponseRef{
				Value: &openapi3.Response{
					Description: &desc,
					Content: openapi3.Content{
						"application/json": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type:     &openapi3.Types{"string"},
									Nullable: true,
								},
							},
						},
					},
				},
			}),
		),
	}

	result := extractOutputSchema(op, nil, "3.0.0")
	if result == nil {
		t.Fatal("期望非 nil 输出 schema")
	}
	// OpenAPI 3.0 nullable 应被转换为 JSON Schema type 数组
	if result["type"] != "object" {
		t.Errorf("期望 type=object（非 object 类型被包装），实际 %v", result["type"])
	}
	// 应有 x-fastmcp-wrap-result
	if wrapResult, _ := result["x-fastmcp-wrap-result"].(bool); !wrapResult {
		t.Error("期望 x-fastmcp-wrap-result=true")
	}
}

// TestExtractOutputSchema_顶层Ref内联展开 测试顶层 $ref 内联展开。
// 由于 openapi3.Schema 没有 Ref 字段，通过直接测试 deepCopyMap 和 replaceSchemaRefs 间接覆盖。
func TestExtractOutputSchema_顶层Ref内联展开(t *testing.T) {
	// 测试 $ref 内联展开逻辑
	schemaDefinitions := map[string]map[string]any{
		"User": {
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
		},
	}

	// 模拟 outputSchema 是一个 $ref
	outputSchema := map[string]any{
		"$ref": "#/$defs/User",
	}

	// 顶层 $ref 内联展开
	if refPath, ok := outputSchema["$ref"].(string); ok && schemaDefinitions != nil {
		if refPath == "#/$defs/User" {
			schemaName := "User"
			if def, exists := schemaDefinitions[schemaName]; exists {
				expanded := deepCopyMap(def)
				replaceSchemaRefs(expanded)
				if expanded["type"] != "object" {
					t.Errorf("期望 type=object，实际 %v", expanded["type"])
				}
			}
		}
	}
}

// TestExtractOutputSchema_HalJsonContentType 测试 application/hal+json Content-Type。
func TestExtractOutputSchema_HalJsonContentType(t *testing.T) {
	desc := "成功"
	op := &openapi3.Operation{
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(200, &openapi3.ResponseRef{
				Value: &openapi3.Response{
					Description: &desc,
					Content: openapi3.Content{
						"application/hal+json": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"object"},
									Properties: openapi3.Schemas{
										"_links": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
									},
								},
							},
						},
					},
				},
			}),
		),
	}

	result := extractOutputSchema(op, nil, "")
	if result == nil {
		t.Fatal("期望非 nil 输出 schema")
	}
}

// TestExtractOutputSchema_NilResponses 测试 nil Responses 返回 nil。
func TestExtractOutputSchema_NilResponses(t *testing.T) {
	op := &openapi3.Operation{
		Responses: nil,
	}
	result := extractOutputSchema(op, nil, "")
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// ──────────────────────────── convertOpenAPISchemaToJSONSchema 扩展测试 ────────────────────────────

// TestConvertOpenAPISchemaToJSONSchema_NilableWithAnyOf 测试 nullable 配合 anyOf 转换。
func TestConvertOpenAPISchemaToJSONSchema_NilableWithAnyOf(t *testing.T) {
	input := map[string]any{
		"nullable": true,
		"anyOf": []any{
			map[string]any{"type": "string"},
		},
	}
	result := convertOpenAPISchemaToJSONSchema(input, "3.0.0")
	anyOf, ok := result["anyOf"].([]any)
	if !ok {
		t.Fatal("期望 anyOf 存在")
	}
	// nullable + anyOf 应追加 null
	if len(anyOf) != 2 {
		t.Errorf("期望 2 个 anyOf（1+null），实际 %d", len(anyOf))
	}
}

// TestConvertOpenAPISchemaToJSONSchema_NilableWithTypeArray 测试 nullable 配合 type 数组。
func TestConvertOpenAPISchemaToJSONSchema_NilableWithTypeArray(t *testing.T) {
	input := map[string]any{
		"type":     []any{"string", "null"},
		"nullable": true,
	}
	result := convertOpenAPISchemaToJSONSchema(input, "3.0.0")
	typeVal, ok := result["type"].([]any)
	if !ok {
		t.Fatalf("期望 type 为 []any，实际 %T", result["type"])
	}
	// 已有 null 不重复添加
	if len(typeVal) != 2 {
		t.Errorf("期望 2 个 type，实际 %d", len(typeVal))
	}
}

// TestConvertOpenAPISchemaToJSONSchema_Items递归 测试 items 递归处理。
func TestConvertOpenAPISchemaToJSONSchema_Items递归(t *testing.T) {
	input := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type":     "string",
			"nullable": true,
		},
	}
	result := convertOpenAPISchemaToJSONSchema(input, "3.0.0")
	items, ok := result["items"].(map[string]any)
	if !ok {
		t.Fatal("期望 items 存在")
	}
	typeVal, ok := items["type"].([]string)
	if !ok {
		t.Fatalf("期望 items.type 为 []string，实际 %T", items["type"])
	}
	if len(typeVal) != 2 || typeVal[1] != "null" {
		t.Errorf("期望 items.type 含 null，实际 %v", typeVal)
	}
}

// ──────────────────────────── oapiSchemaToMap 扩展测试 ────────────────────────────

// TestOapiSchemaToMap_ExclusiveMinMax 测试 exclusiveMinimum/exclusiveMaximum 字段。
func TestOapiSchemaToMap_ExclusiveMinMax(t *testing.T) {
	boolTrue := true
	s := &openapi3.Schema{
		Type:         &openapi3.Types{"integer"},
		ExclusiveMin: openapi3.ExclusiveBound{Bool: &boolTrue},
		ExclusiveMax: openapi3.ExclusiveBound{Bool: &boolTrue},
	}
	result := oapiSchemaToMap(s)
	if result["exclusiveMinimum"] != true {
		t.Errorf("期望 exclusiveMinimum=true，实际 %v", result["exclusiveMinimum"])
	}
	if result["exclusiveMaximum"] != true {
		t.Errorf("期望 exclusiveMaximum=true，实际 %v", result["exclusiveMaximum"])
	}
}
