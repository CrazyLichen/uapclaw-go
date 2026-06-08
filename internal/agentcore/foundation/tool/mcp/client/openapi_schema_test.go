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
		t.Errorf("期望 Enum[0]=active，实际 %s", p.Enum[0])
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
