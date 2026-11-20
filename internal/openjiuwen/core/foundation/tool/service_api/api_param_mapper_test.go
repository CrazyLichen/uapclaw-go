package service_api

import (
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestAPIParamLocation_String 测试 APIParamLocation 的字符串表示
func TestAPIParamLocation_String(t *testing.T) {
	tests := []struct {
		location APIParamLocation
		expected string
	}{
		{APIParamLocationQuery, "query"},
		{APIParamLocationPath, "path"},
		{APIParamLocationBody, "body"},
		{APIParamLocationHeader, "header"},
		{APIParamLocationForm, "form"},
	}
	for _, tt := range tests {
		if got := tt.location.String(); got != tt.expected {
			t.Errorf("APIParamLocation(%d).String() = %q, 期望 %q", tt.location, got, tt.expected)
		}
	}
}

// TestAPIParamMapper_Map_基本映射 测试基本参数位置映射
func TestAPIParamMapper_Map_基本映射(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":    map[string]any{"type": "integer", "location": "path"},
			"q":     map[string]any{"type": "string", "location": "query"},
			"token": map[string]any{"type": "string", "location": "header"},
			"name":  map[string]any{"type": "string"}, // 无 location，走 defaultLocation
			"file":  map[string]any{"type": "string", "location": "form", "form_handler_type": "file"},
		},
	}

	mapper := NewAPIParamMapper(schema, nil, nil, nil)
	inputs := map[string]any{
		"id":    42,
		"q":     "search",
		"token": "abc123",
		"name":  "Alice",
		"file":  "data.txt",
	}

	result := mapper.Map(inputs, APIParamLocationBody)

	// 验证 path 参数
	if result[APIParamLocationPath]["id"] != 42 {
		t.Errorf("path.id: 期望 42，实际 %v", result[APIParamLocationPath]["id"])
	}
	// 验证 query 参数
	if result[APIParamLocationQuery]["q"] != "search" {
		t.Errorf("query.q: 期望 search，实际 %v", result[APIParamLocationQuery]["q"])
	}
	// 验证 header 参数
	if result[APIParamLocationHeader]["token"] != "abc123" {
		t.Errorf("header.token: 期望 abc123，实际 %v", result[APIParamLocationHeader]["token"])
	}
	// 验证 body 参数（无 location 的走 defaultLocation=Body）
	if result[APIParamLocationBody]["name"] != "Alice" {
		t.Errorf("body.name: 期望 Alice，实际 %v", result[APIParamLocationBody]["name"])
	}
	// 验证 form 参数
	formVal, ok := result[APIParamLocationForm]["file"].(map[string]any)
	if !ok {
		t.Fatalf("form.file 类型错误: %T", result[APIParamLocationForm]["file"])
	}
	if formVal["form_handler_type"] != "file" {
		t.Errorf("form.file.form_handler_type: 期望 file，实际 %v", formVal["form_handler_type"])
	}
	if formVal["value"] != "data.txt" {
		t.Errorf("form.file.value: 期望 data.txt，实际 %v", formVal["value"])
	}
}

// TestAPIParamMapper_Map_默认参数合并 测试默认参数合并
func TestAPIParamMapper_Map_默认参数合并(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":    map[string]any{"type": "integer", "location": "path"},
			"q":     map[string]any{"type": "string", "location": "query"},
			"token": map[string]any{"type": "string", "location": "header"},
		},
	}

	mapper := NewAPIParamMapper(
		schema,
		map[string]any{"q": "default_search", "page": "1"}, // defaultQueries
		map[string]any{"X-Auth": "default_token"},          // defaultHeaders
		map[string]any{"id": "0"},                          // defaultPaths
	)

	inputs := map[string]any{
		"id": 42,
		"q":  "search", // 覆盖 defaultQueries["q"]
		// token 不在 inputs 中，defaultHeaders["X-Auth"] 保留
	}

	result := mapper.Map(inputs, APIParamLocationBody)

	// path: inputs 覆盖 defaults
	if result[APIParamLocationPath]["id"] != 42 {
		t.Errorf("path.id: 期望 42，实际 %v", result[APIParamLocationPath]["id"])
	}

	// query: inputs 覆盖 defaults，额外 defaults 保留
	if result[APIParamLocationQuery]["q"] != "search" {
		t.Errorf("query.q: 期望 search（覆盖默认值），实际 %v", result[APIParamLocationQuery]["q"])
	}
	if result[APIParamLocationQuery]["page"] != "1" {
		t.Errorf("query.page: 期望 1（来自默认值），实际 %v", result[APIParamLocationQuery]["page"])
	}

	// header: defaults 保留（inputs 中没有 token → defaultHeaders 的 X-Auth 保留）
	if result[APIParamLocationHeader]["X-Auth"] != "default_token" {
		t.Errorf("header.X-Auth: 期望 default_token，实际 %v", result[APIParamLocationHeader]["X-Auth"])
	}
}

// TestAPIParamMapper_Map_空值不覆盖默认值 测试空值不覆盖 defaults
func TestAPIParamMapper_Map_空值不覆盖默认值(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"q": map[string]any{"type": "string", "location": "query"},
		},
	}

	mapper := NewAPIParamMapper(schema, map[string]any{"q": "default"}, nil, nil)
	inputs := map[string]any{"q": ""} // 空字符串不覆盖默认值

	result := mapper.Map(inputs, APIParamLocationBody)
	if result[APIParamLocationQuery]["q"] != "default" {
		t.Errorf("空字符串不应覆盖默认值: 期望 default，实际 %v", result[APIParamLocationQuery]["q"])
	}
}

// TestAPIParamMapper_Map_nil值不覆盖默认值 测试 nil 值不覆盖 defaults
func TestAPIParamMapper_Map_nil值不覆盖默认值(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"q": map[string]any{"type": "string", "location": "query"},
		},
	}

	mapper := NewAPIParamMapper(schema, map[string]any{"q": "default"}, nil, nil)
	inputs := map[string]any{"q": nil} // nil 不覆盖默认值

	result := mapper.Map(inputs, APIParamLocationBody)
	if result[APIParamLocationQuery]["q"] != "default" {
		t.Errorf("nil 不应覆盖默认值: 期望 default，实际 %v", result[APIParamLocationQuery]["q"])
	}
}

// TestAPIParamMapper_Map_无schema 测试无 schema 时所有输入放到 defaultLocation
func TestAPIParamMapper_Map_无schema(t *testing.T) {
	mapper := NewAPIParamMapper(nil, nil, nil, nil)
	inputs := map[string]any{"key": "value"}

	result := mapper.Map(inputs, APIParamLocationQuery)
	if result[APIParamLocationQuery]["key"] != "value" {
		t.Errorf("无 schema 时应放到 defaultLocation: 期望 value，实际 %v", result[APIParamLocationQuery]["key"])
	}
}

// TestAPIParamMapper_Map_默认位置为Query 测试 GET 等方法默认 location 为 Query
func TestAPIParamMapper_Map_默认位置为Query(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"}, // 无 location
		},
	}

	mapper := NewAPIParamMapper(schema, nil, nil, nil)
	inputs := map[string]any{"name": "Alice"}

	// 模拟 GET 请求：defaultLocation 为 Query
	result := mapper.Map(inputs, APIParamLocationQuery)
	if result[APIParamLocationQuery]["name"] != "Alice" {
		t.Errorf("GET 默认 location 应为 query: 期望 Alice，实际 %v", result[APIParamLocationQuery]["name"])
	}
}

// TestAPIParamMapper_Map_Form参数nil值不存储 测试 Form 参数 nil 值不存储
func TestAPIParamMapper_Map_Form参数nil值不存储(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{"type": "string", "location": "form"},
		},
	}

	mapper := NewAPIParamMapper(schema, nil, nil, nil)
	inputs := map[string]any{"file": nil}

	result := mapper.Map(inputs, APIParamLocationBody)
	if len(result[APIParamLocationForm]) != 0 {
		t.Errorf("Form 参数 nil 值不应存储: %v", result[APIParamLocationForm])
	}
}

// TestAPIParamMapper_Map_未在inputs中的参数不映射 测试 inputs 中不存在的参数不映射
func TestAPIParamMapper_Map_未在inputs中的参数不映射(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":   map[string]any{"type": "integer", "location": "path"},
			"name": map[string]any{"type": "string", "location": "body"},
		},
	}

	mapper := NewAPIParamMapper(schema, nil, nil, nil)
	inputs := map[string]any{"id": 42} // 只有 id，没有 name

	result := mapper.Map(inputs, APIParamLocationBody)
	if result[APIParamLocationPath]["id"] != 42 {
		t.Errorf("path.id: 期望 42，实际 %v", result[APIParamLocationPath]["id"])
	}
	if _, ok := result[APIParamLocationBody]["name"]; ok {
		t.Error("inputs 中不存在的参数不应出现在 body 中")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestParseAPIParamLocation_未知位置 测试未知位置 fallback 到 body
func TestParseAPIParamLocation_未知位置(t *testing.T) {
	loc, err := parseAPIParamLocation("cookie")
	if err != nil {
		t.Errorf("未知位置不应返回错误: %v", err)
	}
	if loc != APIParamLocationBody {
		t.Errorf("未知位置应 fallback 到 body，实际: %v", loc)
	}
}

// TestParseAPIParamLocation_已知位置 测试已知位置正确解析
func TestParseAPIParamLocation_已知位置(t *testing.T) {
	tests := []struct {
		input    string
		expected APIParamLocation
	}{
		{"query", APIParamLocationQuery},
		{"path", APIParamLocationPath},
		{"body", APIParamLocationBody},
		{"header", APIParamLocationHeader},
		{"form", APIParamLocationForm},
		{"Query", APIParamLocationQuery},   // 大小写不敏感
		{"HEADER", APIParamLocationHeader}, // 大小写不敏感
	}
	for _, tt := range tests {
		loc, err := parseAPIParamLocation(tt.input)
		if err != nil {
			t.Errorf("parseAPIParamLocation(%q) 返回错误: %v", tt.input, err)
		}
		if loc != tt.expected {
			t.Errorf("parseAPIParamLocation(%q) = %v, 期望 %v", tt.input, loc, tt.expected)
		}
	}
}
