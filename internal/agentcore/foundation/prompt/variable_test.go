package prompt

import (
	"testing"
)

// ────────────────────── prepareInputs 测试 ──────────────────────

// TestPrepareInputs_FilterByKey 验证按 inputKeys 过滤无关参数。
func TestPrepareInputs_FilterByKey(t *testing.T) {
	inputKeys := []string{"key1", "key2"}
	kwargs := map[string]any{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	result := PrepareInputs(inputKeys, kwargs)

	if len(result) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d", len(result))
	}
	if result["key1"] != "value1" {
		t.Errorf("key1 = %v, want value1", result["key1"])
	}
	if result["key2"] != "value2" {
		t.Errorf("key2 = %v, want value2", result["key2"])
	}
	if _, ok := result["key3"]; ok {
		t.Error("key3 不应出现在结果中")
	}
}

// TestPrepareInputs_EmptyInputKeys 验证空 inputKeys 返回空 map。
func TestPrepareInputs_EmptyInputKeys(t *testing.T) {
	result := PrepareInputs(nil, map[string]any{"key1": "value1"})
	if len(result) != 0 {
		t.Errorf("期望空 map，实际 %v", result)
	}
}

// TestPrepareInputs_EmptyKwargs 验证空 kwargs 返回空 map。
func TestPrepareInputs_EmptyKwargs(t *testing.T) {
	result := PrepareInputs([]string{"key1"}, nil)
	if len(result) != 0 {
		t.Errorf("期望空 map，实际 %v", result)
	}
}

// ────────────────────── extractInputKeys 测试 ──────────────────────

// TestExtractInputKeys_Simple 验证简单占位符提取。
func TestExtractInputKeys_Simple(t *testing.T) {
	keys := extractInputKeys([]string{"domain", "memory"})
	if len(keys) != 2 || keys[0] != "domain" || keys[1] != "memory" {
		t.Errorf("期望 [domain, memory]，实际 %v", keys)
	}
}

// TestExtractInputKeys_Nested 验证嵌套属性取点号前第一段。
func TestExtractInputKeys_Nested(t *testing.T) {
	keys := extractInputKeys([]string{"user.name", "user.age"})
	if len(keys) != 1 || keys[0] != "user" {
		t.Errorf("期望 [user]，实际 %v", keys)
	}
}

// TestExtractInputKeys_Dedup 验证去重保序。
func TestExtractInputKeys_Dedup(t *testing.T) {
	keys := extractInputKeys([]string{"domain", "memory", "domain"})
	if len(keys) != 2 || keys[0] != "domain" || keys[1] != "memory" {
		t.Errorf("期望 [domain, memory]，实际 %v", keys)
	}
}

// ────────────────────── resolveNestedValue 测试 ──────────────────────

// TestResolveNestedValue_Simple 验证简单键值查找。
func TestResolveNestedValue_Simple(t *testing.T) {
	root := map[string]any{"name": "Alice"}
	val, err := resolveNestedValue("name", root)
	if err != nil {
		t.Fatalf("resolveNestedValue 失败: %v", err)
	}
	if val != "Alice" {
		t.Errorf("期望 Alice，实际 %v", val)
	}
}

// TestResolveNestedValue_NestedMap 验证嵌套 map 解析。
func TestResolveNestedValue_NestedMap(t *testing.T) {
	root := map[string]any{
		"user": map[string]any{"name": "Bob"},
	}
	val, err := resolveNestedValue("user.name", root)
	if err != nil {
		t.Fatalf("resolveNestedValue 失败: %v", err)
	}
	if val != "Bob" {
		t.Errorf("期望 Bob，实际 %v", val)
	}
}

// TestResolveNestedValue_DeepNested 验证深层嵌套解析。
func TestResolveNestedValue_DeepNested(t *testing.T) {
	root := map[string]any{
		"user": map[string]any{
			"profile": map[string]any{
				"name": "Charlie",
			},
		},
	}
	val, err := resolveNestedValue("user.profile.name", root)
	if err != nil {
		t.Fatalf("resolveNestedValue 失败: %v", err)
	}
	if val != "Charlie" {
		t.Errorf("期望 Charlie，实际 %v", val)
	}
}

// TestResolveNestedValue_KeyNotFound 验证键不存在时返回错误。
func TestResolveNestedValue_KeyNotFound(t *testing.T) {
	root := map[string]any{"name": "Alice"}
	_, err := resolveNestedValue("missing", root)
	if err == nil {
		t.Error("期望错误，实际 nil")
	}
}

// TestResolveNestedValue_NestedKeyNotFound 验证嵌套路径中间键不存在。
func TestResolveNestedValue_NestedKeyNotFound(t *testing.T) {
	root := map[string]any{
		"user": map[string]any{"name": "Bob"},
	}
	_, err := resolveNestedValue("user.missing.field", root)
	if err == nil {
		t.Error("期望错误，实际 nil")
	}
}

// TestResolveNestedValue_EmptyPath 验证空路径返回错误。
func TestResolveNestedValue_EmptyPath(t *testing.T) {
	root := map[string]any{"name": "Alice"}
	_, err := resolveNestedValue("", root)
	if err == nil {
		t.Error("期望错误，实际 nil")
	}
}

// ────────────────────── accessField / accessStructField 测试 ──────────────────────

// TestAccessField_Map 验证 map 类型字段访问。
func TestAccessField_Map(t *testing.T) {
	val, err := accessField(map[string]any{"key": "value"}, "key")
	if err != nil {
		t.Fatalf("accessField 失败: %v", err)
	}
	if val != "value" {
		t.Errorf("期望 value，实际 %v", val)
	}
}

// TestAccessField_MapKeyNotFound 验证 map 中键不存在。
func TestAccessField_MapKeyNotFound(t *testing.T) {
	_, err := accessField(map[string]any{"key": "value"}, "missing")
	if err == nil {
		t.Error("期望错误，实际 nil")
	}
}

// testStruct 用于测试 reflect struct field 访问。
type testStruct struct {
	Name string
	Age  int
}

// TestAccessStructField_ExactMatch 验证精确匹配导出字段。
func TestAccessStructField_ExactMatch(t *testing.T) {
	s := testStruct{Name: "Alice", Age: 30}
	val, err := accessStructField(s, "Name")
	if err != nil {
		t.Fatalf("accessStructField 失败: %v", err)
	}
	if val != "Alice" {
		t.Errorf("期望 Alice，实际 %v", val)
	}
}

// TestAccessStructField_CaseInsensitive 验证不区分大小写匹配。
func TestAccessStructField_CaseInsensitive(t *testing.T) {
	s := testStruct{Name: "Alice"}
	val, err := accessStructField(s, "name")
	if err != nil {
		t.Fatalf("accessStructField 失败: %v", err)
	}
	if val != "Alice" {
		t.Errorf("期望 Alice，实际 %v", val)
	}
}

// TestAccessStructField_FieldNotFound 验证字段不存在。
func TestAccessStructField_FieldNotFound(t *testing.T) {
	s := testStruct{Name: "Alice"}
	_, err := accessStructField(s, "Missing")
	if err == nil {
		t.Error("期望错误，实际 nil")
	}
}

// TestAccessStructField_NonStruct 验证非 struct 类型返回错误。
func TestAccessStructField_NonStruct(t *testing.T) {
	_, err := accessStructField("not a struct", "field")
	if err == nil {
		t.Error("期望错误，实际 nil")
	}
}

// TestAccessStructField_Pointer 验证指针类型自动解引用。
func TestAccessStructField_Pointer(t *testing.T) {
	s := &testStruct{Name: "Bob"}
	val, err := accessStructField(s, "Name")
	if err != nil {
		t.Fatalf("accessStructField 失败: %v", err)
	}
	if val != "Bob" {
		t.Errorf("期望 Bob，实际 %v", val)
	}
}

// TestResolveNestedValue_StructField 验证嵌套路径中 map → struct 字段访问。
func TestResolveNestedValue_StructField(t *testing.T) {
	root := map[string]any{
		"obj": testStruct{Name: "Dave"},
	}
	val, err := resolveNestedValue("obj.Name", root)
	if err != nil {
		t.Fatalf("resolveNestedValue 失败: %v", err)
	}
	if val != "Dave" {
		t.Errorf("期望 Dave，实际 %v", val)
	}
}

// ────────────────────── baseVariable 测试 ──────────────────────

// TestBaseVariable_NameValueInputKeys 验证 baseVariable 基本字段。
func TestBaseVariable_NameValueInputKeys(t *testing.T) {
	b := baseVariable{
		name:      "test_var",
		inputKeys: []string{"key1", "key2"},
		value:     "",
	}

	if b.Name() != "test_var" {
		t.Errorf("Name() = %q, want %q", b.Name(), "test_var")
	}
	if len(b.InputKeys()) != 2 {
		t.Errorf("InputKeys() = %v, want len 2", b.InputKeys())
	}
	if b.Value() != "" {
		t.Errorf("Value() = %v, want empty string", b.Value())
	}
}

// ────────────────────── splitDotPath 测试 ──────────────────────

// TestSplitDotPath_Normal 验证正常路径拆分。
func TestSplitDotPath_Normal(t *testing.T) {
	parts := splitDotPath("user.profile.name")
	if len(parts) != 3 || parts[0] != "user" || parts[1] != "profile" || parts[2] != "name" {
		t.Errorf("期望 [user, profile, name]，实际 %v", parts)
	}
}

// TestSplitDotPath_Single 验证单段路径。
func TestSplitDotPath_Single(t *testing.T) {
	parts := splitDotPath("name")
	if len(parts) != 1 || parts[0] != "name" {
		t.Errorf("期望 [name]，实际 %v", parts)
	}
}

// TestSplitDotPath_Empty 验证空路径。
func TestSplitDotPath_Empty(t *testing.T) {
	parts := splitDotPath("")
	if parts != nil {
		t.Errorf("期望 nil，实际 %v", parts)
	}
}

// ────────────────────── findDotIndex 测试 ──────────────────────

// TestFindDotIndex_WithDot 验证包含点号。
func TestFindDotIndex_WithDot(t *testing.T) {
	if idx := findDotIndex("user.name"); idx != 4 {
		t.Errorf("期望 4，实际 %d", idx)
	}
}

// TestFindDotIndex_NoDot 验证不包含点号。
func TestFindDotIndex_NoDot(t *testing.T) {
	if idx := findDotIndex("name"); idx != -1 {
		t.Errorf("期望 -1，实际 %d", idx)
	}
}
