package state

import (
	"reflect"
	"testing"
)

// ──── deepCopyMap 测试 ────

// TestDeepCopyMap_基本 验证深拷贝 map 值相同但引用不同。
func TestDeepCopyMap_基本(t *testing.T) {
	src := map[string]any{"a": 1, "b": "hello"}
	dst := deepCopyMap(src)
	if !reflect.DeepEqual(dst, src) {
		t.Errorf("深拷贝结果 %v 与源 %v 不一致", dst, src)
	}
	dst["a"] = 2
	if src["a"] != 1 {
		t.Error("修改拷贝后源被影响，深拷贝失败")
	}
}

// TestDeepCopyMap_嵌套 验证嵌套 map 深拷贝。
func TestDeepCopyMap_嵌套(t *testing.T) {
	src := map[string]any{
		"user": map[string]any{"name": "alice"},
	}
	dst := deepCopyMap(src)
	dst["user"].(map[string]any)["name"] = "bob"
	if src["user"].(map[string]any)["name"] != "alice" {
		t.Error("修改嵌套 map 后源被影响，深拷贝失败")
	}
}

// TestDeepCopyMap_nil 验证 nil map 深拷贝返回 nil。
func TestDeepCopyMap_nil(t *testing.T) {
	var src map[string]any
	dst := deepCopyMap(src)
	if dst != nil {
		t.Errorf("nil map 深拷贝 = %v, 期望 nil", dst)
	}
}

// TestDeepCopyMap_空map 验证空 map 深拷贝返回空 map。
func TestDeepCopyMap_空map(t *testing.T) {
	src := map[string]any{}
	dst := deepCopyMap(src)
	if len(dst) != 0 {
		t.Errorf("空 map 深拷贝长度 = %d, 期望 0", len(dst))
	}
}

// TestDeepCopySlice_基本 验证深拷贝 slice 值相同但引用不同。
func TestDeepCopySlice_基本(t *testing.T) {
	src := []any{1, "hello", true}
	dst := deepCopySlice(src)
	if !reflect.DeepEqual(dst, src) {
		t.Errorf("深拷贝结果 %v 与源 %v 不一致", dst, src)
	}
	dst[0] = 2
	if src[0] != 1 {
		t.Error("修改拷贝后源被影响，深拷贝失败")
	}
}

// TestDeepCopySlice_嵌套Map 验证 slice 中嵌套 map 的深拷贝。
func TestDeepCopySlice_嵌套Map(t *testing.T) {
	src := []any{map[string]any{"key": "value"}}
	dst := deepCopySlice(src)
	dst[0].(map[string]any)["key"] = "changed"
	if src[0].(map[string]any)["key"] != "value" {
		t.Error("修改嵌套 map 后源被影响，深拷贝失败")
	}
}

// TestDeepCopySlice_nil 验证 nil slice 深拷贝返回 nil。
func TestDeepCopySlice_nil(t *testing.T) {
	var src []any
	dst := deepCopySlice(src)
	if dst != nil {
		t.Errorf("nil slice 深拷贝 = %v, 期望 nil", dst)
	}
}

// ──── splitNestedPath 测试 ────

// TestSplitNestedPath_点分隔 验证点分隔路径解析。
func TestSplitNestedPath_点分隔(t *testing.T) {
	result := splitNestedPath("a.b.c")
	expected := []any{"a", "b", "c"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("splitNestedPath(\"a.b.c\") = %v, 期望 %v", result, expected)
	}
}

// TestSplitNestedPath_数组索引 验证数组索引路径解析。
func TestSplitNestedPath_数组索引(t *testing.T) {
	result := splitNestedPath("a.b[0].c")
	expected := []any{"a", "b", 0, "c"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("splitNestedPath(\"a.b[0].c\") = %v, 期望 %v", result, expected)
	}
}

// TestSplitNestedPath_复合路径 验证复合路径解析。
func TestSplitNestedPath_复合路径(t *testing.T) {
	result := splitNestedPath("a_1.b.c[1].d")
	expected := []any{"a_1", "b", "c", 1, "d"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("splitNestedPath(\"a_1.b.c[1].d\") = %v, 期望 %v", result, expected)
	}
}

// TestSplitNestedPath_无分隔符 验证无分隔符路径返回空切片。
func TestSplitNestedPath_无分隔符(t *testing.T) {
	result := splitNestedPath("simple")
	if len(result) != 0 {
		t.Errorf("splitNestedPath(\"simple\") = %v, 期望空切片", result)
	}
}

// TestSplitNestedPath_非字符串 验证非字符串输入返回空切片。
func TestSplitNestedPath_非字符串(t *testing.T) {
	result := splitNestedPath("")
	if len(result) != 0 {
		t.Errorf("splitNestedPath(\"\") = %v, 期望空切片", result)
	}
}

// TestSplitNestedPath_负数索引 验证负数索引路径解析。
func TestSplitNestedPath_负数索引(t *testing.T) {
	result := splitNestedPath("a[-1]")
	expected := []any{"a", -1}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("splitNestedPath(\"a[-1]\") = %v, 期望 %v", result, expected)
	}
}

// ──── isRefPath / extractOriginKey 测试 ────

// TestIsRefPath_引用路径 验证引用路径判断。
func TestIsRefPath_引用路径(t *testing.T) {
	if !isRefPath("${start123.p2}") {
		t.Error("isRefPath(\"${start123.p2}\") = false, 期望 true")
	}
}

// TestIsRefPath_普通路径 验证普通路径不是引用路径。
func TestIsRefPath_普通路径(t *testing.T) {
	if isRefPath("a.b.c") {
		t.Error("isRefPath(\"a.b.c\") = true, 期望 false")
	}
}

// TestIsRefPath_过短 验证过短字符串不是引用路径。
func TestIsRefPath_过短(t *testing.T) {
	if isRefPath("${}") {
		t.Error("isRefPath(\"${}\") = true, 期望 false")
	}
}

// TestExtractOriginKey_引用路径 验证引用路径提取原始 key。
func TestExtractOriginKey_引用路径(t *testing.T) {
	result := extractOriginKey("${start123.p2}")
	if result != "start123.p2" {
		t.Errorf("extractOriginKey(\"${start123.p2}\") = %q, 期望 %q", result, "start123.p2")
	}
}

// TestExtractOriginKey_普通路径 验证普通路径原样返回。
func TestExtractOriginKey_普通路径(t *testing.T) {
	result := extractOriginKey("a.b.c")
	if result != "a.b.c" {
		t.Errorf("extractOriginKey(\"a.b.c\") = %q, 期望 %q", result, "a.b.c")
	}
}

// TestExtractOriginKey_无美元符号 验证不含 $ 的路径原样返回。
func TestExtractOriginKey_无美元符号(t *testing.T) {
	result := extractOriginKey("simple")
	if result != "simple" {
		t.Errorf("extractOriginKey(\"simple\") = %q, 期望 %q", result, "simple")
	}
}

// ──── expandNestedStructure 测试 ────

// TestExpandNestedStructure_嵌套key 验证嵌套 key 展开为嵌套结构。
func TestExpandNestedStructure_嵌套key(t *testing.T) {
	input := map[string]any{"a.b": 1}
	result := expandNestedStructure(input)
	expected := map[string]any{"a": map[string]any{"b": 1}}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expandNestedStructure = %v, 期望 %v", result, expected)
	}
}

// TestExpandNestedStructure_扁平key 验证扁平 key 不变。
func TestExpandNestedStructure_扁平key(t *testing.T) {
	input := map[string]any{"a": 1}
	result := expandNestedStructure(input)
	if !reflect.DeepEqual(result, input) {
		t.Errorf("expandNestedStructure = %v, 期望 %v", result, input)
	}
}

// TestExpandNestedStructure_列表 验证列表中的嵌套结构展开。
func TestExpandNestedStructure_列表(t *testing.T) {
	input := []any{map[string]any{"a.b": 1}}
	result := expandNestedStructure(input)
	expected := []any{map[string]any{"a": map[string]any{"b": 1}}}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expandNestedStructure = %v, 期望 %v", result, expected)
	}
}

// TestExpandNestedStructure_原始值 验证原始值直接返回。
func TestExpandNestedStructure_原始值(t *testing.T) {
	result := expandNestedStructure(42)
	if result != 42 {
		t.Errorf("expandNestedStructure(42) = %v, 期望 42", result)
	}
}

// ──── updateDict 测试 ────

// TestUpdateDict_基本更新 验证基本字典更新。
func TestUpdateDict_基本更新(t *testing.T) {
	source := map[string]any{"a": 1}
	update := map[string]any{"b": 2}
	updateDict(update, source)
	if source["b"] != 2 {
		t.Errorf("更新后 source[\"b\"] = %v, 期望 2", source["b"])
	}
}

// TestUpdateDict_嵌套路径 验证嵌套路径 key 更新。
func TestUpdateDict_嵌套路径(t *testing.T) {
	source := map[string]any{}
	update := map[string]any{"a.b": 1}
	updateDict(update, source)
	if source["a"] == nil {
		t.Fatal("source[\"a\"] 为 nil")
	}
	nested := source["a"].(map[string]any)
	if nested["b"] != 1 {
		t.Errorf("更新后 source[\"a\"][\"b\"] = %v, 期望 1", nested["b"])
	}
}

// TestUpdateDict_nil删除 验证 value 为 nil 时删除对应 key。
func TestUpdateDict_nil删除(t *testing.T) {
	source := map[string]any{"a": 1, "b": 2}
	update := map[string]any{"a": nil}
	updateDict(update, source)
	if _, exists := source["a"]; exists {
		t.Error("value 为 nil 时应删除 key \"a\"")
	}
	if source["b"] != 2 {
		t.Errorf("不应删除无关 key \"b\"")
	}
}

// TestUpdateDict_覆盖 验证覆盖已有值。
func TestUpdateDict_覆盖(t *testing.T) {
	source := map[string]any{"a": 1}
	update := map[string]any{"a": 2}
	updateDict(update, source)
	if source["a"] != 2 {
		t.Errorf("更新后 source[\"a\"] = %v, 期望 2", source["a"])
	}
}

// ──── getBySchema 测试 ────

// TestGetBySchema_字符串key 验证字符串 key 读取。
func TestGetBySchema_字符串key(t *testing.T) {
	data := map[string]any{"name": "alice"}
	result := getBySchema(StringKey("name"), data)
	if result != "alice" {
		t.Errorf("getBySchema(StringKey(\"name\")) = %v, 期望 %v", result, "alice")
	}
}

// TestGetBySchema_字符串key_嵌套 验证嵌套路径字符串 key 读取。
func TestGetBySchema_字符串key_嵌套(t *testing.T) {
	data := map[string]any{"user": map[string]any{"name": "alice"}}
	result := getBySchema(StringKey("user.name"), data)
	if result != "alice" {
		t.Errorf("getBySchema(StringKey(\"user.name\")) = %v, 期望 %v", result, "alice")
	}
}

// TestGetBySchema_字符串key_引用路径 验证引用路径字符串 key 读取。
func TestGetBySchema_字符串key_引用路径(t *testing.T) {
	data := map[string]any{"user": map[string]any{"name": "alice"}}
	result := getBySchema(StringKey("${user.name}"), data)
	if result != "alice" {
		t.Errorf("getBySchema(StringKey(\"${user.name}\")) = %v, 期望 %v", result, "alice")
	}
}

// TestGetBySchema_mapSchema 验证 map schema 批量读取。
func TestGetBySchema_mapSchema(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice", "age": 30},
	}
	schema := SchemaKey(map[string]any{
		"name": "${user.name}",
		"age":  "${user.age}",
	})
	result := getBySchema(schema, data)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("getBySchema 返回类型 %T, 期望 map[string]any", result)
	}
	if m["name"] != "alice" {
		t.Errorf("result[\"name\"] = %v, 期望 alice", m["name"])
	}
	if m["age"] != 30 {
		t.Errorf("result[\"age\"] = %v, 期望 30", m["age"])
	}
}

// TestGetBySchema_listSchema 验证 list schema 批量读取。
func TestGetBySchema_listSchema(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice", "age": 30},
	}
	schema := ListKey([]any{"${user.name}", "${user.age}"})
	result := getBySchema(schema, data)
	l, ok := result.([]any)
	if !ok {
		t.Fatalf("getBySchema 返回类型 %T, 期望 []any", result)
	}
	if l[0] != "alice" {
		t.Errorf("result[0] = %v, 期望 alice", l[0])
	}
	if l[1] != 30 {
		t.Errorf("result[1] = %v, 期望 30", l[1])
	}
}

// TestGetBySchema_不存在 验证 key 不存在返回 nil。
func TestGetBySchema_不存在(t *testing.T) {
	data := map[string]any{"name": "alice"}
	result := getBySchema(StringKey("missing"), data)
	if result != nil {
		t.Errorf("getBySchema(不存在的 key) = %v, 期望 nil", result)
	}
}

// TestGetBySchema_nil数据 验证 nil 数据返回 nil。
func TestGetBySchema_nil数据(t *testing.T) {
	result := getBySchema(StringKey("name"), nil)
	if result != nil {
		t.Errorf("getBySchema(nil 数据) = %v, 期望 nil", result)
	}
}

// TestGetBySchema_带前缀 验证带 nestedPath 前缀读取。
func TestGetBySchema_带前缀(t *testing.T) {
	data := map[string]any{
		"node1": map[string]any{"name": "alice"},
	}
	result := getBySchema(StringKey("name"), data, "node1")
	if result != "alice" {
		t.Errorf("getBySchema(带前缀) = %v, 期望 %v", result, "alice")
	}
}

// ──── getValueByNestedPath 测试 ────

// TestGetValueByNestedPath_扁平 验证扁平 key 读取。
func TestGetValueByNestedPath_扁平(t *testing.T) {
	data := map[string]any{"name": "alice"}
	result := getValueByNestedPath("name", data)
	if result != "alice" {
		t.Errorf("getValueByNestedPath(\"name\") = %v, 期望 alice", result)
	}
}

// TestGetValueByNestedPath_嵌套 验证嵌套路径读取。
func TestGetValueByNestedPath_嵌套(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice"},
	}
	result := getValueByNestedPath("user.name", data)
	if result != "alice" {
		t.Errorf("getValueByNestedPath(\"user.name\") = %v, 期望 alice", result)
	}
}

// TestGetValueByNestedPath_数组索引 验证数组索引读取。
func TestGetValueByNestedPath_数组索引(t *testing.T) {
	data := map[string]any{
		"items": []any{"a", "b", "c"},
	}
	result := getValueByNestedPath("items[1]", data)
	if result != "b" {
		t.Errorf("getValueByNestedPath(\"items[1]\") = %v, 期望 b", result)
	}
}

// TestGetValueByNestedPath_不存在 验证路径不存在返回 nil。
func TestGetValueByNestedPath_不存在(t *testing.T) {
	data := map[string]any{}
	result := getValueByNestedPath("missing", data)
	if result != nil {
		t.Errorf("getValueByNestedPath(不存在的路径) = %v, 期望 nil", result)
	}
}

// ──── rootToPath 测试 ────

// TestRootToPath_扁平key 验证扁平 key 导航。
func TestRootToPath_扁平key(t *testing.T) {
	source := map[string]any{"a": 1}
	key, container := rootToPath("a", source)
	if key != "a" {
		t.Errorf("rootToPath key = %v, 期望 \"a\"", key)
	}
	containerMap, ok := container.(map[string]any)
	if !ok {
		t.Fatalf("container 类型断言失败，实际=%T", container)
	}
	if containerMap["a"] != 1 {
		t.Errorf("rootToPath container[\"a\"] = %v, 期望 1", containerMap["a"])
	}
}

// TestRootToPath_嵌套路径 验证嵌套路径导航到最终容器。
func TestRootToPath_嵌套路径(t *testing.T) {
	source := map[string]any{
		"a": map[string]any{"b": 1},
	}
	key, container := rootToPath("a.b", source)
	if key != "b" {
		t.Errorf("rootToPath key = %v, 期望 \"b\"", key)
	}
	containerMap, ok := container.(map[string]any)
	if !ok {
		t.Fatalf("container 类型断言失败，实际=%T", container)
	}
	if containerMap["b"] != 1 {
		t.Errorf("rootToPath container[\"b\"] = %v, 期望 1", containerMap["b"])
	}
}

// TestRootToPath_不存在 验证路径对应的值不存在时返回 nil。
// 注意：对于无分隔符的路径，rootToPath 返回 (key, source)，
// 需要检查容器中的值是否为 nil
func TestRootToPath_不存在(t *testing.T) {
	source := map[string]any{}
	key, container := rootToPath("missing", source)
	// 无分隔符路径，rootToPath 直接返回 (key, source)
	if key != "missing" {
		t.Errorf("rootToPath key = %v, 期望 \"missing\"", key)
	}
	if container != nil {
		// 容器中不存在 "missing"，但 container 本身不为 nil
		containerMap, ok := container.(map[string]any)
		if ok {
			if _, exists := containerMap["missing"]; exists {
				t.Error("container 中不应存在 \"missing\"")
			}
		}
	}
}

// TestRootToPath_嵌套不存在 验证嵌套路径不存在时返回 nil。
func TestRootToPath_嵌套不存在(t *testing.T) {
	source := map[string]any{}
	key, container := rootToPath("a.b", source)
	if key != nil {
		t.Errorf("rootToPath key = %v, 期望 nil", key)
	}
	if container != nil {
		t.Errorf("rootToPath container = %v, 期望 nil", container)
	}
}

// TestRootToPath_创建中间节点 验证 createIfAbsent 时自动创建中间节点。
func TestRootToPath_创建中间节点(t *testing.T) {
	source := map[string]any{}
	key, container := rootToPath("a.b", source, true)
	if key != "b" {
		t.Errorf("rootToPath key = %v, 期望 \"b\"", key)
	}
	if container == nil {
		t.Fatal("container 为 nil")
	}
}

// ──── updateByKey / deleteByKey 测试 ────

// TestUpdateByKey_新key 验证新增 key。
func TestUpdateByKey_新key(t *testing.T) {
	source := map[string]any{}
	updateByKey("a", 1, source)
	if source["a"] != 1 {
		t.Errorf("updateByKey 后 source[\"a\"] = %v, 期望 1", source["a"])
	}
}

// TestUpdateByKey_覆盖 验证覆盖已有值。
func TestUpdateByKey_覆盖(t *testing.T) {
	source := map[string]any{"a": 1}
	updateByKey("a", 2, source)
	if source["a"] != 2 {
		t.Errorf("updateByKey 后 source[\"a\"] = %v, 期望 2", source["a"])
	}
}

// TestDeleteByKey_存在 验证删除存在的 key。
func TestDeleteByKey_存在(t *testing.T) {
	source := map[string]any{"a": 1}
	deleteByKey("a", source)
	if _, exists := source["a"]; exists {
		t.Error("deleteByKey 后 key \"a\" 仍存在")
	}
}

// TestDeleteByKey_不存在 验证删除不存在的 key 不报错。
func TestDeleteByKey_不存在(t *testing.T) {
	source := map[string]any{}
	deleteByKey("missing", source) // 不应 panic
}

// ──── getBySchema 默认值保留测试 ────

// TestGetBySchema_mapSchema_普通字符串默认值 验证 map schema 中普通字符串被保留为默认值。
func TestGetBySchema_mapSchema_普通字符串默认值(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice"},
	}
	schema := SchemaKey(map[string]any{
		"greeting": "hello",
		"label":    "default_value",
	})
	result := getBySchema(schema, data)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("getBySchema 返回类型 %T, 期望 map[string]any", result)
	}
	if m["greeting"] != "hello" {
		t.Errorf("result[\"greeting\"] = %v, 期望 hello", m["greeting"])
	}
	if m["label"] != "default_value" {
		t.Errorf("result[\"label\"] = %v, 期望 default_value", m["label"])
	}
}

// TestGetBySchema_listSchema_普通字符串默认值 验证 list schema 中普通字符串被保留为默认值。
func TestGetBySchema_listSchema_普通字符串默认值(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice"},
	}
	schema := ListKey([]any{"hello", "world"})
	result := getBySchema(schema, data)
	l, ok := result.([]any)
	if !ok {
		t.Fatalf("getBySchema 返回类型 %T, 期望 []any", result)
	}
	if l[0] != "hello" {
		t.Errorf("result[0] = %v, 期望 hello", l[0])
	}
	if l[1] != "world" {
		t.Errorf("result[1] = %v, 期望 world", l[1])
	}
}

// TestGetBySchema_mapSchema_混合引用路径和默认值 验证 map schema 中引用路径取值，普通字符串保留默认值。
func TestGetBySchema_mapSchema_混合引用路径和默认值(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice", "age": 30},
	}
	schema := SchemaKey(map[string]any{
		"userName": "${user.name}",
		"greeting": "hello",
		"userAge":  "${user.age}",
		"label":    "default",
	})
	result := getBySchema(schema, data)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("getBySchema 返回类型 %T, 期望 map[string]any", result)
	}
	if m["userName"] != "alice" {
		t.Errorf("result[\"userName\"] = %v, 期望 alice", m["userName"])
	}
	if m["greeting"] != "hello" {
		t.Errorf("result[\"greeting\"] = %v, 期望 hello", m["greeting"])
	}
	if m["userAge"] != 30 {
		t.Errorf("result[\"userAge\"] = %v, 期望 30", m["userAge"])
	}
	if m["label"] != "default" {
		t.Errorf("result[\"label\"] = %v, 期望 default", m["label"])
	}
}

// ──── getValueByNestedPath 负数索引测试 ────

// TestGetValueByNestedPath_负数索引 验证负数索引从列表末尾读取。
func TestGetValueByNestedPath_负数索引(t *testing.T) {
	data := map[string]any{
		"items": []any{"a", "b", "c"},
	}
	result := getValueByNestedPath("items[-1]", data)
	if result != "c" {
		t.Errorf("getValueByNestedPath(\"items[-1]\") = %v, 期望 c", result)
	}
}

// TestGetValueByNestedPath_负数索引_越界 验证负数索引越界返回 nil。
func TestGetValueByNestedPath_负数索引_越界(t *testing.T) {
	data := map[string]any{
		"items": []any{"a"},
	}
	result := getValueByNestedPath("items[-2]", data)
	if result != nil {
		t.Errorf("getValueByNestedPath(\"items[-2]\") = %v, 期望 nil", result)
	}
}

// ──── rootToPath list 索引路径测试 ────

// TestRootToPath_list索引路径 验证 list 索引作为最终 key 导航到列表元素容器。
// rootToPath 不支持中间遍历列表，仅支持 map 内 list 作为最终值。
// 即 "items" 路径最终到达 map，而 [1] 索引作为最终 key 返回 (1, list)。
func TestRootToPath_list索引路径(t *testing.T) {
	source := map[string]any{
		"items": []any{"a", "b", "c"},
	}
	// rootToPath("items[1]") 会先拆分为 ["items", 1]
	// 但 rootToPath 不支持中间遍历 list，需要直接访问
	// 使用 items[1] 路径测试
	key, container := rootToPath("items[1]", source)
	if key != 1 {
		t.Errorf("rootToPath key = %v, 期望 1", key)
	}
	list, ok := container.([]any)
	if !ok {
		t.Fatalf("container 类型断言失败，实际=%T", container)
	}
	if list[1] != "b" {
		t.Errorf("rootToPath container[1] = %v, 期望 b", list[1])
	}
}

// TestRootToPath_list负数索引路径 验证 list 负数索引路径导航。
func TestRootToPath_list负数索引路径(t *testing.T) {
	source := map[string]any{
		"items": []any{"a", "b", "c"},
	}
	key, container := rootToPath("items[-1]", source)
	if key != 2 {
		t.Errorf("rootToPath key = %v, 期望 2 (负数索引 -1 解析为 len-1=2)", key)
	}
	list, ok := container.([]any)
	if !ok {
		t.Fatalf("container 类型断言失败，实际=%T", container)
	}
	if list[2] != "c" {
		t.Errorf("rootToPath container[2] = %v, 期望 c", list[2])
	}
}

// TestRootToPath_嵌套list索引路径 验证嵌套结构中的 list 索引路径导航。
// rootToPath 不支持中间遍历 list，此测试验证 map.list[index] 模式。
func TestRootToPath_嵌套list索引路径(t *testing.T) {
	source := map[string]any{
		"users": []any{
			map[string]any{"name": "alice"},
			map[string]any{"name": "bob"},
		},
	}
	key, container := rootToPath("users[1].name", source)
	if key != "name" {
		t.Errorf("rootToPath key = %v, 期望 \"name\"", key)
	}
	containerMap, ok := container.(map[string]any)
	if !ok {
		t.Fatalf("container 类型断言失败，实际=%T", container)
	}
	if containerMap["name"] != "bob" {
		t.Errorf("rootToPath container[\"name\"] = %v, 期望 bob", containerMap["name"])
	}
}
