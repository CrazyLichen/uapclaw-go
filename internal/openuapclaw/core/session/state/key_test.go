package state

import (
	"reflect"
	"testing"
)

// TestStringKey_基本 验证 StringKey 创建的 StateKey 类型正确、值可获取。
func TestStringKey_基本(t *testing.T) {
	k := StringKey("a.b.c")
	if k.Type() != StateKeyString {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyString)
	}
	if k.String() != "a.b.c" {
		t.Errorf("String() = %q, 期望 %q", k.String(), "a.b.c")
	}
}

// TestSchemaKey_基本 验证 SchemaKey 创建的 StateKey 类型正确、值可获取。
func TestSchemaKey_基本(t *testing.T) {
	schema := map[string]any{"name": "user.name", "age": "user.age"}
	k := SchemaKey(schema)
	if k.Type() != StateKeyMap {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyMap)
	}
	m := k.Map()
	if m["name"] != "user.name" {
		t.Errorf("Map()[\"name\"] = %v, 期望 %v", m["name"], "user.name")
	}
}

// TestListKey_基本 验证 ListKey 创建的 StateKey 类型正确、值可获取。
func TestListKey_基本(t *testing.T) {
	keys := []any{"user.name", "user.age"}
	k := ListKey(keys)
	if k.Type() != StateKeyList {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyList)
	}
	l := k.List()
	if l[0] != "user.name" {
		t.Errorf("List()[0] = %v, 期望 %v", l[0], "user.name")
	}
}

// TestSchemaKey_深拷贝 验证 SchemaKey 构造时做了深拷贝，外部修改不影响 StateKey。
func TestSchemaKey_深拷贝(t *testing.T) {
	schema := map[string]any{"key": "value"}
	k := SchemaKey(schema)
	schema["key"] = "changed"
	m := k.Map()
	if m["key"] != "value" {
		t.Errorf("外部修改后 Map()[\"key\"] = %v, 期望 %v（应深拷贝隔离）", m["key"], "value")
	}
}

// TestListKey_深拷贝 验证 ListKey 构造时做了深拷贝，外部修改不影响 StateKey。
func TestListKey_深拷贝(t *testing.T) {
	keys := []any{"a", "b"}
	k := ListKey(keys)
	keys[0] = "changed"
	l := k.List()
	if l[0] != "a" {
		t.Errorf("外部修改后 List()[0] = %v, 期望 %v（应深拷贝隔离）", l[0], "a")
	}
}

// TestStringKey_引用路径 验证引用路径风格的字符串 key。
func TestStringKey_引用路径(t *testing.T) {
	k := StringKey("${start123.p2}")
	if k.Type() != StateKeyString {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyString)
	}
	if k.String() != "${start123.p2}" {
		t.Errorf("String() = %q, 期望 %q", k.String(), "${start123.p2}")
	}
}

// TestSchemaKey_返回深拷贝 验证 Map() 返回的是深拷贝，修改返回值不影响内部。
func TestSchemaKey_返回深拷贝(t *testing.T) {
	k := SchemaKey(map[string]any{"key": "value"})
	m := k.Map()
	m["key"] = "changed"
	m2 := k.Map()
	if m2["key"] != "value" {
		t.Errorf("修改返回值后再次 Map()[\"key\"] = %v, 期望 %v（返回值应深拷贝）", m2["key"], "value")
	}
}

// TestListKey_返回深拷贝 验证 List() 返回的是深拷贝，修改返回值不影响内部。
func TestListKey_返回深拷贝(t *testing.T) {
	k := ListKey([]any{"a", "b"})
	l := k.List()
	l[0] = "changed"
	l2 := k.List()
	if l2[0] != "a" {
		t.Errorf("修改返回值后再次 List()[0] = %v, 期望 %v（返回值应深拷贝）", l2[0], "a")
	}
}

// TestStateKey_零值 验证零值 StateKey 的 Type 为默认值。
func TestStateKey_零值(t *testing.T) {
	var k StateKey
	if k.Type() != StateKeyString {
		t.Errorf("零值 StateKey Type() = %v, 期望 %v", k.Type(), StateKeyString)
	}
}

// TestStateKey_嵌套深拷贝 验证 SchemaKey 对嵌套 map 做了深拷贝。
func TestStateKey_嵌套深拷贝(t *testing.T) {
	schema := map[string]any{
		"user": map[string]any{"name": "user.name"},
	}
	k := SchemaKey(schema)
	nested := schema["user"].(map[string]any)
	nested["name"] = "changed"
	m := k.Map()
	if m["user"].(map[string]any)["name"] != "user.name" {
		t.Errorf("嵌套 map 未深拷贝，修改后 = %v", m["user"].(map[string]any)["name"])
	}
}

// TestStateKey_列表嵌套深拷贝 验证 ListKey 对嵌套 map 做了深拷贝。
func TestStateKey_列表嵌套深拷贝(t *testing.T) {
	keys := []any{
		map[string]any{"key": "value"},
	}
	k := ListKey(keys)
	keys[0].(map[string]any)["key"] = "changed"
	l := k.List()
	if l[0].(map[string]any)["key"] != "value" {
		t.Errorf("嵌套 map 未深拷贝，修改后 = %v", l[0].(map[string]any)["key"])
	}
}

// ──── 构造函数类型安全测试 ────

// TestStringKey_空字符串 验证空字符串 key 正常创建。
func TestStringKey_空字符串(t *testing.T) {
	k := StringKey("")
	if k.Type() != StateKeyString {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyString)
	}
	if k.String() != "" {
		t.Errorf("String() = %q, 期望空字符串", k.String())
	}
}

// TestSchemaKey_空map 验证空 map schema 正常创建。
func TestSchemaKey_空map(t *testing.T) {
	k := SchemaKey(map[string]any{})
	if k.Type() != StateKeyMap {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyMap)
	}
	if len(k.Map()) != 0 {
		t.Errorf("Map() 长度 = %d, 期望 0", len(k.Map()))
	}
}

// TestListKey_空slice 验证空 slice schema 正常创建。
func TestListKey_空slice(t *testing.T) {
	k := ListKey([]any{})
	if k.Type() != StateKeyList {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyList)
	}
	if len(k.List()) != 0 {
		t.Errorf("List() 长度 = %d, 期望 0", len(k.List()))
	}
}

// ──── 类型不匹配访问测试 ────

// TestStateKey_类型不匹配访问 验证用错误访问方法获取值时返回零值。
func TestStateKey_类型不匹配访问(t *testing.T) {
	k := StringKey("a.b.c")
	if m := k.Map(); m != nil {
		t.Errorf("StringKey.Map() = %v, 期望 nil", m)
	}
	if l := k.List(); l != nil {
		t.Errorf("StringKey.List() = %v, 期望 nil", l)
	}

	schemaKey := SchemaKey(map[string]any{"key": "val"})
	if s := schemaKey.String(); s != "" {
		t.Errorf("SchemaKey.String() = %q, 期望空字符串", s)
	}
	if l := schemaKey.List(); l != nil {
		t.Errorf("SchemaKey.List() = %v, 期望 nil", l)
	}

	listKey := ListKey([]any{"a"})
	if s := listKey.String(); s != "" {
		t.Errorf("ListKey.String() = %q, 期望空字符串", s)
	}
	if m := listKey.Map(); m != nil {
		t.Errorf("ListKey.Map() = %v, 期望 nil", m)
	}
}

// ──── 反射验证 ────

// TestStateKey_类型校验 验证 StateKey 内部 value 字段存储的类型正确。
func TestStateKey_类型校验(t *testing.T) {
	sk := StringKey("test")
	if reflect.TypeOf(sk.value) != reflect.TypeOf("") {
		t.Errorf("StringKey 内部 value 类型 = %T, 期望 string", sk.value)
	}

	mk := SchemaKey(map[string]any{"k": "v"})
	if reflect.TypeOf(mk.value) != reflect.TypeOf(map[string]any{}) {
		t.Errorf("SchemaKey 内部 value 类型 = %T, 期望 map[string]any", mk.value)
	}

	lk := ListKey([]any{"a"})
	if reflect.TypeOf(lk.value) != reflect.TypeOf([]any{}) {
		t.Errorf("ListKey 内部 value 类型 = %T, 期望 []any", lk.value)
	}
}
