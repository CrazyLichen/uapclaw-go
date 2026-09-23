package extraction

import (
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常数 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestParseJSON_标准JSON 测试直接解析标准 JSON 字符串
func TestParseJSON_标准JSON(t *testing.T) {
	input := `{"name": "Alice", "age": 30}`
	result := ParseJSON(input, nil)

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["name"] != "Alice" {
		t.Errorf("期望 name=Alice，实际 %v", m["name"])
	}
	// json.Unmarshal 将数字解析为 float64
	if age, _ := m["age"].(float64); age != 30 {
		t.Errorf("期望 age=30，实际 %v", m["age"])
	}
}

// TestParseJSON_代码块中提取 测试从 Markdown 代码块中提取 JSON
func TestParseJSON_代码块中提取(t *testing.T) {
	input := "Here is the result:\n```json\n{\"key\": \"value\"}\n```\nDone."
	result := ParseJSON(input, nil)

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["key"] != "value" {
		t.Errorf("期望 key=value，实际 %v", m["key"])
	}
}

// TestParseJSON_代码块无类型标注 测试无语言标注的代码块也能提取 JSON
func TestParseJSON_代码块无类型标注(t *testing.T) {
	input := "Result:\n```\n{\"key\": \"value\"}\n```\n"
	result := ParseJSON(input, nil)

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["key"] != "value" {
		t.Errorf("期望 key=value，实际 %v", m["key"])
	}
}

// TestParseJSON_代码块非JSON类型 测试非 JSON 类型的代码块被跳过
func TestParseJSON_代码块非JSON类型(t *testing.T) {
	input := "Result:\n```python\nprint('hello')\n```\n{\"key\": \"value\"}"
	result := ParseJSON(input, nil)

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["key"] != "value" {
		t.Errorf("期望 key=value，实际 %v", m["key"])
	}
}

// TestParseJSON_截断修复 测试截断的 JSON 数组能被修复解析
func TestParseJSON_截断修复(t *testing.T) {
	// 模拟 LLM 响应被截断：JSON 数组未正常关闭，最后一个元素后有 "},"
	input := `[{"name": "Alice"}, {"name": "Bob"},`
	result := ParseJSON(input, nil)

	list, ok := result.([]any)
	if !ok {
		t.Fatalf("期望 []any，实际 %T", result)
	}
	if len(list) != 2 {
		t.Errorf("期望 2 个元素，实际 %d", len(list))
	}
}

// TestParseJSON_截断修复带尾部文本 测试截断 JSON 后面有乱码的情况
func TestParseJSON_截断修复带尾部文本(t *testing.T) {
	// 截断的 JSON 数组，后面有无效内容
	input := `[{"name": "Alice"}, {"name": "Bob"}, some garbage text here`
	result := ParseJSON(input, nil)

	list, ok := result.([]any)
	if !ok {
		t.Fatalf("期望 []any，实际 %T", result)
	}
	if len(list) != 2 {
		t.Errorf("期望 2 个元素，实际 %d", len(list))
	}
}

// TestParseJSON_空输入 测试空字符串返回 nil
func TestParseJSON_空输入(t *testing.T) {
	result := ParseJSON("", nil)
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// TestParseJSON_纯文本无JSON 测试不含 JSON 的纯文本返回 nil
func TestParseJSON_纯文本无JSON(t *testing.T) {
	result := ParseJSON("Hello world, no JSON here", nil)
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// TestParseJSON_带outputSchema 测试 outputSchema 的 required 字段模糊匹配
func TestParseJSON_带outputSchema(t *testing.T) {
	input := `{"entity_name": "Alice", "entity_type": "person"}`
	outputSchema := map[string]any{
		"json_schema": map[string]any{
			"required": []any{"entity_name"},
		},
	}
	result := ParseJSON(input, outputSchema)

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["entity_name"] != "Alice" {
		t.Errorf("期望 entity_name=Alice，实际 %v", m["entity_name"])
	}
	// 只有 required 中的 key 被保留
	if _, exists := m["entity_type"]; exists {
		t.Errorf("期望 entity_type 不存在，但存在")
	}
}

// TestParseJSON_JSON数组 测试解析 JSON 数组
func TestParseJSON_JSON数组(t *testing.T) {
	input := `[1, 2, 3]`
	result := ParseJSON(input, nil)

	list, ok := result.([]any)
	if !ok {
		t.Fatalf("期望 []any，实际 %T", result)
	}
	if len(list) != 3 {
		t.Errorf("期望 3 个元素，实际 %d", len(list))
	}
}

// TestEnsureList_列表 测试列表直接返回
func TestEnsureList_列表(t *testing.T) {
	input := []any{"a", "b", "c"}
	result := EnsureList(input)

	if len(result) != 3 {
		t.Errorf("期望 3 个元素，实际 %d", len(result))
	}
	if result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("期望 [a b c]，实际 %v", result)
	}
}

// TestEnsureList_单元素字典 测试单元素字典提取列表值
func TestEnsureList_单元素字典(t *testing.T) {
	input := map[string]any{
		"items": []any{"x", "y"},
	}
	result := EnsureList(input)

	if len(result) != 2 {
		t.Errorf("期望 2 个元素，实际 %d", len(result))
	}
	if result[0] != "x" || result[1] != "y" {
		t.Errorf("期望 [x y]，实际 %v", result)
	}
}

// TestEnsureList_单元素字典值非列表 测试单元素字典的值不是列表时包装为列表
func TestEnsureList_单元素字典值非列表(t *testing.T) {
	input := map[string]any{
		"item": "hello",
	}
	result := EnsureList(input)

	if len(result) != 1 {
		t.Errorf("期望 1 个元素，实际 %d", len(result))
	}
	// 整个 map 被包装为单元素列表
	if m, ok := result[0].(map[string]any); !ok || m["item"] != "hello" {
		t.Errorf("期望 [{item:hello}]，实际 %v", result)
	}
}

// TestEnsureList_普通值 测试非列表非字典的值包装为列表
func TestEnsureList_普通值(t *testing.T) {
	result := EnsureList("hello")

	if len(result) != 1 {
		t.Errorf("期望 1 个元素，实际 %d", len(result))
	}
	if result[0] != "hello" {
		t.Errorf("期望 [hello]，实际 %v", result)
	}
}

// TestEnsureList_多元素字典 测试多元素字典包装为列表
func TestEnsureList_多元素字典(t *testing.T) {
	input := map[string]any{
		"a": 1,
		"b": 2,
	}
	result := EnsureList(input)

	if len(result) != 1 {
		t.Errorf("期望 1 个元素，实际 %d", len(result))
	}
	if _, ok := result[0].(map[string]any); !ok {
		t.Errorf("期望 map[string]any，实际 %T", result[0])
	}
}

// TestTryGetKey_精确匹配 测试精确 key 匹配
func TestTryGetKey_精确匹配(t *testing.T) {
	src := map[string]any{"name": "Alice"}
	result := TryGetKey("name", src)
	if result != "name" {
		t.Errorf("期望 name，实际 %s", result)
	}
}

// TestTryGetKey_模糊匹配 测试大小写差异的模糊匹配
func TestTryGetKey_模糊匹配(t *testing.T) {
	src := map[string]any{"EntityName": "Alice"}
	result := TryGetKey("entity_name", src)
	if result != "EntityName" {
		t.Errorf("期望 EntityName，实际 %s", result)
	}
}

// TestTryGetKey_无匹配 测试不存在的 key 返回空字符串
func TestTryGetKey_无匹配(t *testing.T) {
	src := map[string]any{"name": "Alice"}
	result := TryGetKey("xyz", src)
	if result != "" {
		t.Errorf("期望空字符串，实际 %s", result)
	}
}

// TestRawDecodeJSON_标准解析 测试 rawDecodeJSON 直接解析
func TestRawDecodeJSON_标准解析(t *testing.T) {
	result := rawDecodeJSON(`{"key": "value"}`, nil)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["key"] != "value" {
		t.Errorf("期望 key=value，实际 %v", m["key"])
	}
}

// TestRawDecodeJSON_截断修复 测试 rawDecodeJSON 截断修复
func TestRawDecodeJSON_截断修复(t *testing.T) {
	input := `[{"a": 1}, {"b": 2},`
	result := rawDecodeJSON(input, nil)
	list, ok := result.([]any)
	if !ok {
		t.Fatalf("期望 []any，实际 %T", result)
	}
	if len(list) != 2 {
		t.Errorf("期望 2 个元素，实际 %d", len(list))
	}
}

// TestSequenceMatcherRatio 测试相似度计算
func TestSequenceMatcherRatio(t *testing.T) {
	tests := []struct {
		a, b   string
		expect float64
	}{
		{"abc", "abc", 1.0},
		{"", "", 1.0},
		{"abc", "", 0.0},
		{"", "abc", 0.0},
		{"abc", "def", 0.0},
		{"abc", "abcd", 0.857},            // 2*3/7 ≈ 0.857
		{"entityname", "entityname", 1.0}, // 相同字符串
	}
	for _, tt := range tests {
		ratio := sequenceMatcherRatio(tt.a, tt.b)
		// 允许小范围浮点误差
		delta := 0.01
		if ratio < tt.expect-delta || ratio > tt.expect+delta {
			t.Errorf("sequenceMatcherRatio(%q, %q) = %.4f，期望约 %.4f", tt.a, tt.b, ratio, tt.expect)
		}
	}
}
