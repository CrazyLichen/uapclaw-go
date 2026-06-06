package prompt

import (
	"testing"
)

// ────────────────────── 构造测试 ──────────────────────

// TestDictableVariable_MapInit 验证 map 类型初始化。
func TestDictableVariable_MapInit(t *testing.T) {
	data := map[string]any{
		"text": "Hello {{name}}",
		"info": map[string]any{"age": "{{age}}"},
	}
	v, err := NewDictableVariable(data, "default")
	if err != nil {
		t.Fatalf("NewDictableVariable 失败: %v", err)
	}
	if len(v.InputKeys()) != 2 {
		t.Errorf("InputKeys() = %v, want len 2", v.InputKeys())
	}
	if len(v.Placeholders()) != 2 {
		t.Errorf("Placeholders() = %v, want len 2", v.Placeholders())
	}
}

// TestDictableVariable_SliceInit 验证 slice 类型初始化。
func TestDictableVariable_SliceInit(t *testing.T) {
	data := []any{
		map[string]any{"type": "text", "content": "{{user.profile.name}}"},
	}
	v, err := NewDictableVariable(data, "default")
	if err != nil {
		t.Fatalf("NewDictableVariable 失败: %v", err)
	}
	if len(v.InputKeys()) != 1 || v.InputKeys()[0] != "user" {
		t.Errorf("InputKeys() = %v, want [user]", v.InputKeys())
	}
	if len(v.Placeholders()) != 1 || v.Placeholders()[0] != "user.profile.name" {
		t.Errorf("Placeholders() = %v, want [user.profile.name]", v.Placeholders())
	}
}

// ────────────────────── Update 测试 ──────────────────────

// TestDictableVariable_Update_Map 验证 map 类型占位符替换。
func TestDictableVariable_Update_Map(t *testing.T) {
	data := map[string]any{
		"message": "Hi {{user}}",
		"details": map[string]any{
			"id":  101,
			"tag": "{{tag}}",
		},
	}
	v, err := NewDictableVariable(data, "default")
	if err != nil {
		t.Fatalf("NewDictableVariable 失败: %v", err)
	}

	v.Update(map[string]any{
		"user": "Alice",
		"tag":  "VIP",
	})

	result, ok := v.Value().(map[string]any)
	if !ok {
		t.Fatalf("Value() 类型错误: %T", v.Value())
	}
	if result["message"] != "Hi Alice" {
		t.Errorf("message = %v, want 'Hi Alice'", result["message"])
	}
	details, ok := result["details"].(map[string]any)
	if !ok {
		t.Fatalf("details 类型错误: %T", result["details"])
	}
	if details["tag"] != "VIP" {
		t.Errorf("tag = %v, want 'VIP'", details["tag"])
	}
	if details["id"] != 101 {
		t.Errorf("id = %v, want 101", details["id"])
	}
}

// TestDictableVariable_Update_Slice 验证 slice 类型占位符替换。
func TestDictableVariable_Update_Slice(t *testing.T) {
	data := []any{
		map[string]any{"type": "text", "text": "{{query}}"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "{{url}}"}},
	}
	v, err := NewDictableVariable(data, "default")
	if err != nil {
		t.Fatalf("NewDictableVariable 失败: %v", err)
	}

	v.Update(map[string]any{
		"query": "What is this?",
		"url":   "http://example.com/1.jpg",
	})

	result, ok := v.Value().([]any)
	if !ok {
		t.Fatalf("Value() 类型错误: %T", v.Value())
	}
	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}

	item0, _ := result[0].(map[string]any)
	if item0["text"] != "What is this?" {
		t.Errorf("text = %v, want 'What is this?'", item0["text"])
	}

	item1, _ := result[1].(map[string]any)
	imgURL, _ := item1["image_url"].(map[string]any)
	if imgURL["url"] != "http://example.com/1.jpg" {
		t.Errorf("url = %v, want 'http://example.com/1.jpg'", imgURL["url"])
	}
}

// TestDictableVariable_Update_NestedObj 验证嵌套属性替换。
func TestDictableVariable_Update_NestedObj(t *testing.T) {
	data := map[string]any{
		"info": "Author is {{author.name}}",
	}
	v, err := NewDictableVariable(data, "default")
	if err != nil {
		t.Fatalf("NewDictableVariable 失败: %v", err)
	}

	v.Update(map[string]any{
		"author": map[string]any{"name": "Bob"},
	})

	result, _ := v.Value().(map[string]any)
	if result["info"] != "Author is Bob" {
		t.Errorf("info = %v, want 'Author is Bob'", result["info"])
	}
}

// TestDictableVariable_Update_NonStringLog 验证非字符串类型的 str() 转换。
func TestDictableVariable_Update_NonStringLog(t *testing.T) {
	data := map[string]any{
		"count": "Total: {{num}}",
	}
	v, err := NewDictableVariable(data, "default")
	if err != nil {
		t.Fatalf("NewDictableVariable 失败: %v", err)
	}

	v.Update(map[string]any{"num": 100})
	result, _ := v.Value().(map[string]any)
	if result["count"] != "Total: 100" {
		t.Errorf("count = %v, want 'Total: 100'", result["count"])
	}

	v.Update(map[string]any{"num": true})
	result2, _ := v.Value().(map[string]any)
	if result2["count"] != "Total: true" {
		t.Errorf("count = %v, want 'Total: true'", result2["count"])
	}
}

// ────────────────────── Eval 测试 ──────────────────────

// TestDictableVariable_Eval 验证 Eval 求值。
func TestDictableVariable_Eval(t *testing.T) {
	data := map[string]any{
		"message": "Hi {{user}}",
	}
	v, err := NewDictableVariable(data, "default")
	if err != nil {
		t.Fatalf("NewDictableVariable 失败: %v", err)
	}

	result := v.Eval(map[string]any{"user": "Alice"})
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("Eval() 类型错误: %T", result)
	}
	if resultMap["message"] != "Hi Alice" {
		t.Errorf("message = %v, want 'Hi Alice'", resultMap["message"])
	}
}

// ────────────────────── 深拷贝测试 ──────────────────────

// TestDictableVariable_DeepCopy 验证 Update 不修改原始 data。
func TestDictableVariable_DeepCopy(t *testing.T) {
	data := map[string]any{
		"text": "Hello {{name}}",
	}
	v, err := NewDictableVariable(data, "default")
	if err != nil {
		t.Fatalf("NewDictableVariable 失败: %v", err)
	}

	v.Update(map[string]any{"name": "World"})

	// 原始 data 不应被修改
	if data["text"] != "Hello {{name}}" {
		t.Errorf("原始 data 被修改: %v", data["text"])
	}
}

// TestDictableVariable_DataGetter 验证 Data() getter。
func TestDictableVariable_DataGetter(t *testing.T) {
	data := map[string]any{"key": "value"}
	v, err := NewDictableVariable(data, "default")
	if err != nil {
		t.Fatalf("NewDictableVariable 失败: %v", err)
	}
	if v.Data() == nil {
		t.Error("Data() 不应返回 nil")
	}
}

// TestDictableVariable_Update_NonStringValues 验证非字符串值原样保留。
func TestDictableVariable_Update_NonStringValues(t *testing.T) {
	data := map[string]any{
		"id":    42,
		"name":  "{{user}}",
		"valid": true,
	}
	v, err := NewDictableVariable(data, "default")
	if err != nil {
		t.Fatalf("NewDictableVariable 失败: %v", err)
	}

	v.Update(map[string]any{"user": "Alice"})

	result, _ := v.Value().(map[string]any)
	if result["id"] != 42 {
		t.Errorf("id = %v, want 42", result["id"])
	}
	if result["valid"] != true {
		t.Errorf("valid = %v, want true", result["valid"])
	}
	if result["name"] != "Alice" {
		t.Errorf("name = %v, want Alice", result["name"])
	}
}

// TestDictableVariable_Update_EmptyName 验证空名称使用默认值。
func TestDictableVariable_Update_EmptyName(t *testing.T) {
	data := map[string]any{"text": "Hi {{user}}"}
	v, err := NewDictableVariable(data, "")
	if err != nil {
		t.Fatalf("NewDictableVariable 失败: %v", err)
	}
	if v.Name() != defaultVarName {
		t.Errorf("Name() = %q, want %q", v.Name(), defaultVarName)
	}
}
