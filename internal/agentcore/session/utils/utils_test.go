package utils

import (
	"reflect"
	"testing"
)

// ──── DeepCopyMap 测试 ────

// TestDeepCopyMap_基本 验证深拷贝 map 值相同但引用不同。
func TestDeepCopyMap_基本(t *testing.T) {
	src := map[string]any{"a": 1, "b": "hello"}
	dst := DeepCopyMap(src)
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
	dst := DeepCopyMap(src)
	dst["user"].(map[string]any)["name"] = "bob"
	if src["user"].(map[string]any)["name"] != "alice" {
		t.Error("修改嵌套 map 后源被影响，深拷贝失败")
	}
}

// TestDeepCopyMap_nil 验证 nil map 深拷贝返回 nil。
func TestDeepCopyMap_nil(t *testing.T) {
	var src map[string]any
	dst := DeepCopyMap(src)
	if dst != nil {
		t.Errorf("nil map 深拷贝 = %v, 期望 nil", dst)
	}
}

// TestDeepCopyMap_空map 验证空 map 深拷贝返回空 map。
func TestDeepCopyMap_空map(t *testing.T) {
	src := map[string]any{}
	dst := DeepCopyMap(src)
	if len(dst) != 0 {
		t.Errorf("空 map 深拷贝长度 = %d, 期望 0", len(dst))
	}
}

// ──── DeepCopySlice 测试 ────

// TestDeepCopySlice_基本 验证深拷贝 slice 值相同但引用不同。
func TestDeepCopySlice_基本(t *testing.T) {
	src := []any{1, "hello", true}
	dst := DeepCopySlice(src)
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
	dst := DeepCopySlice(src)
	dst[0].(map[string]any)["key"] = "changed"
	if src[0].(map[string]any)["key"] != "value" {
		t.Error("修改嵌套 map 后源被影响，深拷贝失败")
	}
}

// TestDeepCopySlice_nil 验证 nil slice 深拷贝返回 nil。
func TestDeepCopySlice_nil(t *testing.T) {
	var src []any
	dst := DeepCopySlice(src)
	if dst != nil {
		t.Errorf("nil slice 深拷贝 = %v, 期望 nil", dst)
	}
}

// ──── SplitNestedPath 测试 ────

// TestSplitNestedPath_点分隔 验证点分隔路径解析。
func TestSplitNestedPath_点分隔(t *testing.T) {
	result := SplitNestedPath("a.b.c")
	expected := []any{"a", "b", "c"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("SplitNestedPath(\"a.b.c\") = %v, 期望 %v", result, expected)
	}
}

// TestSplitNestedPath_数组索引 验证数组索引路径解析。
func TestSplitNestedPath_数组索引(t *testing.T) {
	result := SplitNestedPath("a.b[0].c")
	expected := []any{"a", "b", 0, "c"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("SplitNestedPath(\"a.b[0].c\") = %v, 期望 %v", result, expected)
	}
}

// TestSplitNestedPath_复合路径 验证复合路径解析。
func TestSplitNestedPath_复合路径(t *testing.T) {
	result := SplitNestedPath("a_1.b.c[1].d")
	expected := []any{"a_1", "b", "c", 1, "d"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("SplitNestedPath(\"a_1.b.c[1].d\") = %v, 期望 %v", result, expected)
	}
}

// TestSplitNestedPath_无分隔符 验证无分隔符路径返回空切片。
func TestSplitNestedPath_无分隔符(t *testing.T) {
	result := SplitNestedPath("simple")
	if len(result) != 0 {
		t.Errorf("SplitNestedPath(\"simple\") = %v, 期望空切片", result)
	}
}

// TestSplitNestedPath_非字符串 验证非字符串输入返回空切片。
func TestSplitNestedPath_非字符串(t *testing.T) {
	result := SplitNestedPath("")
	if len(result) != 0 {
		t.Errorf("SplitNestedPath(\"\") = %v, 期望空切片", result)
	}
}

// TestSplitNestedPath_负数索引 验证负数索引路径解析。
func TestSplitNestedPath_负数索引(t *testing.T) {
	result := SplitNestedPath("a[-1]")
	expected := []any{"a", -1}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("SplitNestedPath(\"a[-1]\") = %v, 期望 %v", result, expected)
	}
}

// TestSplitNestedPath_引号路径 验证包含 [' 的路径解析。
func TestSplitNestedPath_引号路径(t *testing.T) {
	result := SplitNestedPath("a['key']")
	if len(result) == 0 {
		t.Error("期望非空结果")
	}
}

// ──── IsRefPath / ExtractOriginKey 测试 ────

// TestIsRefPath_引用路径 验证引用路径判断。
func TestIsRefPath_引用路径(t *testing.T) {
	if !IsRefPath("${start123.p2}") {
		t.Error("IsRefPath(\"${start123.p2}\") = false, 期望 true")
	}
}

// TestIsRefPath_普通路径 验证普通路径不是引用路径。
func TestIsRefPath_普通路径(t *testing.T) {
	if IsRefPath("a.b.c") {
		t.Error("IsRefPath(\"a.b.c\") = true, 期望 false")
	}
}

// TestIsRefPath_过短 验证过短字符串不是引用路径。
func TestIsRefPath_过短(t *testing.T) {
	if IsRefPath("${}") {
		t.Error("IsRefPath(\"${}\") = true, 期望 false")
	}
}

// TestIsRefPath_过长 验证过长字符串不是引用路径。
func TestIsRefPath_过长(t *testing.T) {
	longPath := "${" + string(make([]byte, 1000)) + "}"
	if IsRefPath(longPath) {
		t.Error("过长字符串不应是引用路径")
	}
}

// TestIsRefPath_刚好4字符 验证刚好 4 字符的 ${x} 是引用路径（长度>3）。
func TestIsRefPath_刚好4字符(t *testing.T) {
	if !IsRefPath("${x}") {
		t.Error("\"${x}\" 长度为 4，>3 且以 ${ 开头 } 结尾，应是引用路径")
	}
}

// TestIsRefPath_5字符 验证 5 字符的引用路径。
func TestIsRefPath_5字符(t *testing.T) {
	if !IsRefPath("${ab}") {
		t.Error("\"${ab}\" 长度为 5，应是引用路径")
	}
}

// TestExtractOriginKey_引用路径 验证引用路径提取原始 key。
func TestExtractOriginKey_引用路径(t *testing.T) {
	result := ExtractOriginKey("${start123.p2}")
	if result != "start123.p2" {
		t.Errorf("ExtractOriginKey(\"${start123.p2}\") = %q, 期望 %q", result, "start123.p2")
	}
}

// TestExtractOriginKey_普通路径 验证普通路径原样返回。
func TestExtractOriginKey_普通路径(t *testing.T) {
	result := ExtractOriginKey("a.b.c")
	if result != "a.b.c" {
		t.Errorf("ExtractOriginKey(\"a.b.c\") = %q, 期望 %q", result, "a.b.c")
	}
}

// TestExtractOriginKey_无美元符号 验证不含 $ 的路径原样返回。
func TestExtractOriginKey_无美元符号(t *testing.T) {
	result := ExtractOriginKey("simple")
	if result != "simple" {
		t.Errorf("ExtractOriginKey(\"simple\") = %q, 期望 %q", result, "simple")
	}
}

// TestExtractOriginKey_无闭合大括号 验证无闭合大括号时原样返回。
func TestExtractOriginKey_无闭合大括号(t *testing.T) {
	result := ExtractOriginKey("${unclosed")
	if result != "${unclosed" {
		t.Errorf("期望原样返回，实际=%q", result)
	}
}

// ──── ExpandNestedStructure 测试 ────

// TestExpandNestedStructure_嵌套key 验证嵌套 key 展开为嵌套结构。
func TestExpandNestedStructure_嵌套key(t *testing.T) {
	input := map[string]any{"a.b": 1}
	result := ExpandNestedStructure(input)
	expected := map[string]any{"a": map[string]any{"b": 1}}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("ExpandNestedStructure = %v, 期望 %v", result, expected)
	}
}

// TestExpandNestedStructure_扁平key 验证扁平 key 不变。
func TestExpandNestedStructure_扁平key(t *testing.T) {
	input := map[string]any{"a": 1}
	result := ExpandNestedStructure(input)
	if !reflect.DeepEqual(result, input) {
		t.Errorf("ExpandNestedStructure = %v, 期望 %v", result, input)
	}
}

// TestExpandNestedStructure_列表 验证列表中的嵌套结构展开。
func TestExpandNestedStructure_列表(t *testing.T) {
	input := []any{map[string]any{"a.b": 1}}
	result := ExpandNestedStructure(input)
	expected := []any{map[string]any{"a": map[string]any{"b": 1}}}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("ExpandNestedStructure = %v, 期望 %v", result, expected)
	}
}

// TestExpandNestedStructure_原始值 验证原始值直接返回。
func TestExpandNestedStructure_原始值(t *testing.T) {
	result := ExpandNestedStructure(42)
	if result != 42 {
		t.Errorf("ExpandNestedStructure(42) = %v, 期望 42", result)
	}
}

// TestExpandNestedStructure_列表索引路径 验证含数组索引的嵌套路径展开。
func TestExpandNestedStructure_列表索引路径(t *testing.T) {
	input := map[string]any{"a[0]": 1}
	result := ExpandNestedStructure(input)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map，实际=%T", result)
	}
	list, ok := m["a"].([]any)
	if !ok {
		t.Fatalf("期望 a 为 []any，实际=%T", m["a"])
	}
	if list[0] != 1 {
		t.Errorf("期望 a[0]=1，实际=%v", list[0])
	}
}

// ──── UpdateDict 测试 ────

// TestUpdateDict_基本更新 验证基本字典更新。
func TestUpdateDict_基本更新(t *testing.T) {
	source := map[string]any{"a": 1}
	update := map[string]any{"b": 2}
	UpdateDict(update, source)
	if source["b"] != 2 {
		t.Errorf("更新后 source[\"b\"] = %v, 期望 2", source["b"])
	}
}

// TestUpdateDict_嵌套路径 验证嵌套路径 key 更新。
func TestUpdateDict_嵌套路径(t *testing.T) {
	source := map[string]any{}
	update := map[string]any{"a.b": 1}
	UpdateDict(update, source)
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
	UpdateDict(update, source)
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
	UpdateDict(update, source)
	if source["a"] != 2 {
		t.Errorf("更新后 source[\"a\"] = %v, 期望 2", source["a"])
	}
}

// ──── GetValueByNestedPath 测试 ────

// TestGetValueByNestedPath_扁平 验证扁平 key 读取。
func TestGetValueByNestedPath_扁平(t *testing.T) {
	data := map[string]any{"name": "alice"}
	result := GetValueByNestedPath("name", data)
	if result != "alice" {
		t.Errorf("GetValueByNestedPath(\"name\") = %v, 期望 alice", result)
	}
}

// TestGetValueByNestedPath_嵌套 验证嵌套路径读取。
func TestGetValueByNestedPath_嵌套(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice"},
	}
	result := GetValueByNestedPath("user.name", data)
	if result != "alice" {
		t.Errorf("GetValueByNestedPath(\"user.name\") = %v, 期望 alice", result)
	}
}

// TestGetValueByNestedPath_数组索引 验证数组索引读取。
func TestGetValueByNestedPath_数组索引(t *testing.T) {
	data := map[string]any{
		"items": []any{"a", "b", "c"},
	}
	result := GetValueByNestedPath("items[1]", data)
	if result != "b" {
		t.Errorf("GetValueByNestedPath(\"items[1]\") = %v, 期望 b", result)
	}
}

// TestGetValueByNestedPath_不存在 验证路径不存在返回 nil。
func TestGetValueByNestedPath_不存在(t *testing.T) {
	data := map[string]any{}
	result := GetValueByNestedPath("missing", data)
	if result != nil {
		t.Errorf("GetValueByNestedPath(不存在的路径) = %v, 期望 nil", result)
	}
}

// TestGetValueByNestedPath_负数索引 验证负数索引从列表末尾读取。
func TestGetValueByNestedPath_负数索引(t *testing.T) {
	data := map[string]any{
		"items": []any{"a", "b", "c"},
	}
	result := GetValueByNestedPath("items[-1]", data)
	if result != "c" {
		t.Errorf("GetValueByNestedPath(\"items[-1]\") = %v, 期望 c", result)
	}
}

// TestGetValueByNestedPath_负数索引_越界 验证负数索引越界返回 nil。
func TestGetValueByNestedPath_负数索引_越界(t *testing.T) {
	data := map[string]any{
		"items": []any{"a"},
	}
	result := GetValueByNestedPath("items[-2]", data)
	if result != nil {
		t.Errorf("GetValueByNestedPath(\"items[-2]\") = %v, 期望 nil", result)
	}
}

// TestGetValueByNestedPath_中间值不是map 验证中间值不是 map 时返回 nil。
func TestGetValueByNestedPath_中间值不是map(t *testing.T) {
	data := map[string]any{
		"a": "not_a_map",
	}
	result := GetValueByNestedPath("a.b", data)
	if result != nil {
		t.Errorf("期望 nil，实际=%v", result)
	}
}

// TestGetValueByNestedPath_中间值不是list 验证中间值不是 list 时返回 nil。
func TestGetValueByNestedPath_中间值不是list(t *testing.T) {
	data := map[string]any{
		"a": "not_a_list",
	}
	result := GetValueByNestedPath("a[0]", data)
	if result != nil {
		t.Errorf("期望 nil，实际=%v", result)
	}
}

// TestGetValueByNestedPath_正索引越界 验证正索引越界返回 nil。
func TestGetValueByNestedPath_正索引越界(t *testing.T) {
	data := map[string]any{
		"items": []any{"a"},
	}
	result := GetValueByNestedPath("items[5]", data)
	if result != nil {
		t.Errorf("期望 nil，实际=%v", result)
	}
}

// TestGetValueByNestedPath_嵌套路径中间是列表 验证嵌套路径中列表元素后继续访问。
func TestGetValueByNestedPath_嵌套路径中间是列表(t *testing.T) {
	data := map[string]any{
		"users": []any{
			map[string]any{"name": "alice"},
			map[string]any{"name": "bob"},
		},
	}
	result := GetValueByNestedPath("users[1].name", data)
	if result != "bob" {
		t.Errorf("期望 bob，实际=%v", result)
	}
}

// ──── RootToPath 测试 ────

// TestRootToPath_扁平key 验证扁平 key 导航。
func TestRootToPath_扁平key(t *testing.T) {
	source := map[string]any{"a": 1}
	key, container := RootToPath("a", source)
	if key != "a" {
		t.Errorf("RootToPath key = %v, 期望 \"a\"", key)
	}
	containerMap, ok := container.(map[string]any)
	if !ok {
		t.Fatalf("container 类型断言失败，实际=%T", container)
	}
	if containerMap["a"] != 1 {
		t.Errorf("RootToPath container[\"a\"] = %v, 期望 1", containerMap["a"])
	}
}

// TestRootToPath_嵌套路径 验证嵌套路径导航到最终容器。
func TestRootToPath_嵌套路径(t *testing.T) {
	source := map[string]any{
		"a": map[string]any{"b": 1},
	}
	key, container := RootToPath("a.b", source)
	if key != "b" {
		t.Errorf("RootToPath key = %v, 期望 \"b\"", key)
	}
	containerMap, ok := container.(map[string]any)
	if !ok {
		t.Fatalf("container 类型断言失败，实际=%T", container)
	}
	if containerMap["b"] != 1 {
		t.Errorf("RootToPath container[\"b\"] = %v, 期望 1", containerMap["b"])
	}
}

// TestRootToPath_不存在 验证路径对应的值不存在时返回 nil。
// 注意：对于无分隔符的路径，RootToPath 返回 (key, source)，
// 需要检查容器中的值是否为 nil
func TestRootToPath_不存在(t *testing.T) {
	source := map[string]any{}
	key, container := RootToPath("missing", source)
	// 无分隔符路径，RootToPath 直接返回 (key, source)
	if key != "missing" {
		t.Errorf("RootToPath key = %v, 期望 \"missing\"", key)
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
	key, container := RootToPath("a.b", source)
	if key != nil {
		t.Errorf("RootToPath key = %v, 期望 nil", key)
	}
	if container != nil {
		t.Errorf("RootToPath container = %v, 期望 nil", container)
	}
}

// TestRootToPath_创建中间节点 验证 createIfAbsent 时自动创建中间节点。
func TestRootToPath_创建中间节点(t *testing.T) {
	source := map[string]any{}
	key, container := RootToPath("a.b", source, true)
	if key != "b" {
		t.Errorf("RootToPath key = %v, 期望 \"b\"", key)
	}
	if container == nil {
		t.Fatal("container 为 nil")
	}
}

// TestRootToPath_list索引路径 验证 list 索引作为最终 key 导航到列表元素容器。
// RootToPath 不支持中间遍历列表，仅支持 map 内 list 作为最终值。
// 即 "items" 路径最终到达 map，而 [1] 索引作为最终 key 返回 (1, list)。
func TestRootToPath_list索引路径(t *testing.T) {
	source := map[string]any{
		"items": []any{"a", "b", "c"},
	}
	// RootToPath("items[1]") 会先拆分为 ["items", 1]
	// 但 RootToPath 不支持中间遍历 list，需要直接访问
	// 使用 items[1] 路径测试
	key, container := RootToPath("items[1]", source)
	if key != 1 {
		t.Errorf("RootToPath key = %v, 期望 1", key)
	}
	list, ok := container.([]any)
	if !ok {
		t.Fatalf("container 类型断言失败，实际=%T", container)
	}
	if list[1] != "b" {
		t.Errorf("RootToPath container[1] = %v, 期望 b", list[1])
	}
}

// TestRootToPath_list负数索引路径 验证 list 负数索引路径导航。
func TestRootToPath_list负数索引路径(t *testing.T) {
	source := map[string]any{
		"items": []any{"a", "b", "c"},
	}
	key, container := RootToPath("items[-1]", source)
	if key != 2 {
		t.Errorf("RootToPath key = %v, 期望 2 (负数索引 -1 解析为 len-1=2)", key)
	}
	list, ok := container.([]any)
	if !ok {
		t.Fatalf("container 类型断言失败，实际=%T", container)
	}
	if list[2] != "c" {
		t.Errorf("RootToPath container[2] = %v, 期望 c", list[2])
	}
}

// TestRootToPath_嵌套list索引路径 验证嵌套结构中的 list 索引路径导航。
// RootToPath 不支持中间遍历 list，此测试验证 map.list[index] 模式。
func TestRootToPath_嵌套list索引路径(t *testing.T) {
	source := map[string]any{
		"users": []any{
			map[string]any{"name": "alice"},
			map[string]any{"name": "bob"},
		},
	}
	key, container := RootToPath("users[1].name", source)
	if key != "name" {
		t.Errorf("RootToPath key = %v, 期望 \"name\"", key)
	}
	containerMap, ok := container.(map[string]any)
	if !ok {
		t.Fatalf("container 类型断言失败，实际=%T", container)
	}
	if containerMap["name"] != "bob" {
		t.Errorf("RootToPath container[\"name\"] = %v, 期望 bob", containerMap["name"])
	}
}

// TestRootToPath_创建列表中间节点 验证下一个路径是索引时创建列表中间节点。
func TestRootToPath_创建列表中间节点(t *testing.T) {
	source := map[string]any{}
	key, container := RootToPath("items[0]", source, true)
	if key != 0 {
		t.Errorf("期望 key=0，实际=%v", key)
	}
	list, ok := container.([]any)
	if !ok {
		t.Fatalf("期望 container 为 []any，实际=%T", container)
	}
	if len(list) == 0 {
		t.Error("列表为空")
	}
}

// TestRootToPath_列表越界扩展 验证列表索引越界时自动扩展。
func TestRootToPath_列表越界扩展(t *testing.T) {
	source := map[string]any{
		"items": []any{},
	}
	key, container := RootToPath("items[2]", source, true)
	if key != 2 {
		t.Errorf("期望 key=2，实际=%v", key)
	}
	list, ok := container.([]any)
	if !ok {
		t.Fatalf("期望 container 为 []any，实际=%T", container)
	}
	if len(list) != 3 {
		t.Errorf("期望 len=3，实际=%d", len(list))
	}
}

// TestRootToPath_当前不是map 验证 current 不是 map 时返回 nil。
func TestRootToPath_当前不是map(t *testing.T) {
	source := map[string]any{
		"a": "not_a_map",
	}
	key, container := RootToPath("a.b", source, false)
	if key != nil {
		t.Errorf("期望 key=nil，实际=%v", key)
	}
	if container != nil {
		t.Errorf("期望 container=nil，实际=%v", container)
	}
}

// TestRootToPath_中间值不是map也不是list 验证中间值不是 map/list 且 createIfAbsent 时创建新 map。
func TestRootToPath_中间值不是map也不是list(t *testing.T) {
	source := map[string]any{
		"a": "primitive_value",
	}
	key, container := RootToPath("a.b", source, true)
	if key != "b" {
		t.Errorf("期望 key=\"b\"，实际=%v", key)
	}
	_, ok := container.(map[string]any)
	if !ok {
		t.Fatalf("期望 container 为 map，实际=%T", container)
	}
	// 原始值应被替换为空 map
	if source["a"].(map[string]any) == nil {
		t.Error("source[\"a\"] 应被替换为 map")
	}
}

// TestRootToPath_当前不是list 验证 current 不是 list 时返回 nil。
func TestRootToPath_当前不是list(t *testing.T) {
	source := map[string]any{
		"a": map[string]any{},
	}
	// 先设置 a.b 为非列表，再尝试索引访问
	source["a"].(map[string]any)["b"] = "not_list"
	key, container := RootToPath("a.b[0]", source, false)
	if key != nil {
		t.Errorf("期望 key=nil，实际=%v", key)
	}
	if container != nil {
		t.Errorf("期望 container=nil，实际=%v", container)
	}
}

// TestRootToPath_列表越界不创建 验证列表越界且不创建时返回 nil。
func TestRootToPath_列表越界不创建(t *testing.T) {
	source := map[string]any{
		"items": []any{"a"},
	}
	key, container := RootToPath("items[5]", source, false)
	if key != nil {
		t.Errorf("期望 key=nil，实际=%v", key)
	}
	if container != nil {
		t.Errorf("期望 container=nil，实际=%v", container)
	}
}

// TestRootToPath_列表负索引越界 验证列表负索引越界返回 nil。
func TestRootToPath_列表负索引越界(t *testing.T) {
	source := map[string]any{
		"items": []any{"a"},
	}
	key, _ := RootToPath("items[-2]", source, false)
	if key != nil {
		t.Errorf("期望 key=nil，实际=%v", key)
	}
}

// ──── UpdateByKey 测试 ────

// TestUpdateByKey_新key 验证新增 key。
func TestUpdateByKey_新key(t *testing.T) {
	source := map[string]any{}
	UpdateByKey("a", 1, source)
	if source["a"] != 1 {
		t.Errorf("UpdateByKey 后 source[\"a\"] = %v, 期望 1", source["a"])
	}
}

// TestUpdateByKey_覆盖 验证覆盖已有值。
func TestUpdateByKey_覆盖(t *testing.T) {
	source := map[string]any{"a": 1}
	UpdateByKey("a", 2, source)
	if source["a"] != 2 {
		t.Errorf("UpdateByKey 后 source[\"a\"] = %v, 期望 2", source["a"])
	}
}

// TestUpdateByKey_已有map值合并 验证已有值和新值都是 map 时执行合并。
func TestUpdateByKey_已有map值合并(t *testing.T) {
	source := map[string]any{
		"a": map[string]any{"x": 1},
	}
	UpdateByKey("a", map[string]any{"y": 2}, source)
	m := source["a"].(map[string]any)
	if m["x"] != 1 {
		t.Errorf("期望保留 x=1，实际=%v", m["x"])
	}
	if m["y"] != 2 {
		t.Errorf("期望新增 y=2，实际=%v", m["y"])
	}
}

// TestUpdateByKey_已有map值被非map覆盖 验证已有 map 值被非 map 值覆盖。
func TestUpdateByKey_已有map值被非map覆盖(t *testing.T) {
	source := map[string]any{
		"a": map[string]any{"x": 1},
	}
	UpdateByKey("a", "scalar", source)
	if source["a"] != "scalar" {
		t.Errorf("期望 a=\"scalar\"，实际=%v", source["a"])
	}
}

// TestUpdateByKey_列表索引更新 验证通过列表索引更新值。
func TestUpdateByKey_列表索引更新(t *testing.T) {
	source := []any{"old", "keep"}
	UpdateByKey(0, "new", source)
	if source[0] != "new" {
		t.Errorf("期望 source[0]=\"new\"，实际=%v", source[0])
	}
}

// TestUpdateByKey_列表索引越界 验证列表索引越界不操作。
func TestUpdateByKey_列表索引越界(t *testing.T) {
	source := []any{"a"}
	UpdateByKey(5, "new", source) // 不应 panic
	if source[0] != "a" {
		t.Errorf("越界更新不应改变现有值")
	}
}

// TestUpdateByKey_source非map非list 验证 source 不是 map/list 时不操作。
func TestUpdateByKey_source非map非list(t *testing.T) {
	UpdateByKey("a", 1, "not_a_container") // 不应 panic
}

// ──── DeleteByKey 测试 ────

// TestDeleteByKey_存在 验证删除存在的 key。
func TestDeleteByKey_存在(t *testing.T) {
	source := map[string]any{"a": 1}
	DeleteByKey("a", source)
	if _, exists := source["a"]; exists {
		t.Error("DeleteByKey 后 key \"a\" 仍存在")
	}
}

// TestDeleteByKey_不存在 验证删除不存在的 key 不报错。
func TestDeleteByKey_不存在(t *testing.T) {
	source := map[string]any{}
	DeleteByKey("missing", source) // 不应 panic
}

// TestDeleteByKey_列表索引删除 验证通过列表索引删除（设为 nil）。
func TestDeleteByKey_列表索引删除(t *testing.T) {
	source := []any{"a", "b", "c"}
	DeleteByKey(1, source)
	if source[1] != nil {
		t.Errorf("期望 source[1]=nil，实际=%v", source[1])
	}
}

// TestDeleteByKey_列表索引越界 验证列表索引越界不操作。
func TestDeleteByKey_列表索引越界(t *testing.T) {
	source := []any{"a"}
	DeleteByKey(5, source) // 不应 panic
	if source[0] != "a" {
		t.Error("越界删除不应改变现有值")
	}
}

// TestDeleteByKey_source非map非list 验证 source 不是 map/list 时不操作。
func TestDeleteByKey_source非map非list(t *testing.T) {
	DeleteByKey("a", "not_a_container") // 不应 panic
}

// ──── SafeExtendContainer 测试 ────

// TestSafeExtendContainer_无需扩展 验证目标索引在范围内时不扩展。
func TestSafeExtendContainer_无需扩展(t *testing.T) {
	container := []any{map[string]any{}}
	result, ok := SafeExtendContainer(container, 0, true)
	if !ok {
		t.Error("期望 ok=true")
	}
	if len(result) != 1 {
		t.Errorf("期望 len=1，实际=%d", len(result))
	}
}

// TestSafeExtendContainer_扩展到map 验证 isFinal=true 时目标位置放空字典。
func TestSafeExtendContainer_扩展到map(t *testing.T) {
	container := []any{}
	result, ok := SafeExtendContainer(container, 2, true)
	if !ok {
		t.Error("期望 ok=true")
	}
	if len(result) != 3 {
		t.Errorf("期望 len=3，实际=%d", len(result))
	}
	if result[0] != nil {
		t.Errorf("中间位置应为 nil，实际=%v", result[0])
	}
	if _, isMap := result[2].(map[string]any); !isMap {
		t.Errorf("目标位置应为 map[string]any，实际=%T", result[2])
	}
}

// TestSafeExtendContainer_扩展到list 验证 isFinal=false 时目标位置放空列表。
func TestSafeExtendContainer_扩展到list(t *testing.T) {
	container := []any{}
	result, ok := SafeExtendContainer(container, 1, false)
	if !ok {
		t.Error("期望 ok=true")
	}
	if _, isSlice := result[1].([]any); !isSlice {
		t.Errorf("目标位置应为 []any，实际=%T", result[1])
	}
}

// TestSafeExtendContainer_索引越界 验证负索引和过大索引返回 false。
func TestSafeExtendContainer_索引越界(t *testing.T) {
	container := []any{}
	if _, ok := SafeExtendContainer(container, -1, true); ok {
		t.Error("负索引应返回 false")
	}
	if _, ok := SafeExtendContainer(container, 10001, true); ok {
		t.Error("索引 >10000 应返回 false")
	}
}

// TestSafeExtendContainer_扩展量过大 验证扩展量超过 10000 返回 false。
func TestSafeExtendContainer_扩展量过大(t *testing.T) {
	container := []any{}
	if _, ok := SafeExtendContainer(container, 10001, true); ok {
		t.Error("扩展量 >10000 应返回 false")
	}
}

// ──── RootToIndex 测试 ────

// TestRootToIndex_基本 验证通过索引路径导航列表。
func TestRootToIndex_基本(t *testing.T) {
	source := []any{map[string]any{"a": 1}, map[string]any{"b": 2}}
	idx, container := RootToIndex([]int{1}, source, false)
	if idx != 1 {
		t.Errorf("期望 idx=1，实际=%d", idx)
	}
	if container == nil {
		t.Fatal("container 为 nil")
	}
}

// TestRootToIndex_嵌套索引 验证嵌套列表索引导航。
func TestRootToIndex_嵌套索引(t *testing.T) {
	inner := []any{map[string]any{"x": 10}, map[string]any{"x": 20}}
	source := []any{inner}
	idx, container := RootToIndex([]int{0, 1}, source, false)
	if idx != 1 {
		t.Errorf("期望 idx=1，实际=%d", idx)
	}
	if container == nil {
		t.Fatal("container 为 nil")
	}
}

// TestRootToIndex_负索引 验证负索引从末尾计算。
func TestRootToIndex_负索引(t *testing.T) {
	source := []any{"a", "b", "c"}
	idx, container := RootToIndex([]int{-1}, source, false)
	if idx != 2 {
		t.Errorf("期望 idx=2，实际=%d", idx)
	}
	if container == nil {
		t.Fatal("container 为 nil")
	}
}

// TestRootToIndex_空输入 验证空索引返回 (-1, nil)。
func TestRootToIndex_空输入(t *testing.T) {
	idx, container := RootToIndex([]int{}, []any{}, false)
	if idx != -1 {
		t.Errorf("期望 idx=-1，实际=%d", idx)
	}
	if container != nil {
		t.Errorf("期望 container=nil，实际=%v", container)
	}
}

// TestRootToIndex_nil源 验证 nil 源返回 (-1, nil)。
func TestRootToIndex_nil源(t *testing.T) {
	idx, _ := RootToIndex([]int{0}, nil, false)
	if idx != -1 {
		t.Errorf("期望 idx=-1，实际=%d", idx)
	}
}

// TestRootToIndex_深度过大 验证超过 10 层深度返回 (-1, nil)。
func TestRootToIndex_深度过大(t *testing.T) {
	indexes := make([]int, 11)
	idx, _ := RootToIndex(indexes, []any{}, false)
	if idx != -1 {
		t.Errorf("期望深度过大返回 -1，实际=%d", idx)
	}
}

// TestRootToIndex_越界创建 验证 createIfAbsent=true 时自动扩展列表。
func TestRootToIndex_越界创建(t *testing.T) {
	source := []any{}
	idx, container := RootToIndex([]int{2}, source, true)
	if idx != 2 {
		t.Errorf("期望 idx=2，实际=%d", idx)
	}
	if container == nil {
		t.Fatal("container 为 nil")
	}
	if len(container) != 3 {
		t.Errorf("期望 len=3，实际=%d", len(container))
	}
}

// TestRootToIndex_越界不创建 验证 createIfAbsent=false 时越界返回 (-1, nil)。
func TestRootToIndex_越界不创建(t *testing.T) {
	source := []any{}
	idx, _ := RootToIndex([]int{5}, source, false)
	if idx != -1 {
		t.Errorf("期望越界返回 -1，实际=%d", idx)
	}
}

// TestRootToIndex_中间索引过大 验证中间索引超过 10000 返回 (-1, nil)。
func TestRootToIndex_中间索引过大(t *testing.T) {
	source := []any{[]any{}}
	idx, _ := RootToIndex([]int{0, 10001}, source, true)
	if idx != -1 {
		t.Errorf("期望索引过大返回 -1，实际=%d", idx)
	}
}

// TestRootToIndex_中间nil自动创建列表 验证中间位置为 nil 时自动创建列表。
func TestRootToIndex_中间nil自动创建列表(t *testing.T) {
	source := []any{nil, nil}
	idx, container := RootToIndex([]int{0, 0}, source, true)
	if idx != 0 {
		t.Errorf("期望 idx=0，实际=%d", idx)
	}
	if container == nil {
		t.Fatal("container 为 nil")
	}
}

// TestRootToIndex_中间非列表非nil 验证中间位置非列表非nil返回 (-1, nil)。
func TestRootToIndex_中间非列表非nil(t *testing.T) {
	source := []any{"not_a_list"}
	idx, _ := RootToIndex([]int{0, 0}, source, false)
	if idx != -1 {
		t.Errorf("期望返回 -1，实际=%d", idx)
	}
}

// TestRootToIndex_负索引越界 验证负索引越界返回 (-1, nil)。
func TestRootToIndex_负索引越界(t *testing.T) {
	source := []any{"only_one"}
	idx, _ := RootToIndex([]int{-2}, source, false)
	if idx != -1 {
		t.Errorf("期望负索引越界返回 -1，实际=%d", idx)
	}
}

// ──── writeBackList 测试 ────

// TestWriteBackList_空parents 验证空 parents 时不操作。
func TestWriteBackList_空parents(t *testing.T) {
	writeBackList(nil, []any{"a"}) // 不应 panic
}

// TestWriteBackList_写回map 验证写回 map 父容器。
func TestWriteBackList_写回map(t *testing.T) {
	parent := map[string]any{"key": []any{"old"}}
	entry := parentEntry{m: parent, mKey: "key", isMap: true}
	newList := []any{"new"}
	writeBackList([]parentEntry{entry}, newList)
	if parent["key"].([]any)[0] != "new" {
		t.Errorf("期望写回 map 成功，实际=%v", parent["key"])
	}
}

// TestWriteBackList_写回list 验证写回 list 父容器。
func TestWriteBackList_写回list(t *testing.T) {
	parentList := []any{[]any{"old"}, "other"}
	entry := parentEntry{l: parentList, lIdx: 0, isMap: false}
	newList := []any{"new"}
	writeBackList([]parentEntry{entry}, newList)
	if parentList[0].([]any)[0] != "new" {
		t.Errorf("期望写回 list 成功，实际=%v", parentList[0])
	}
}

// ──── ConvertUpdatesFromJSON 测试 ────

// TestConvertUpdatesFromJSON_正常数据 验证正常 JSON 反序列化数据转换。
func TestConvertUpdatesFromJSON_正常数据(t *testing.T) {
	raw := map[string]any{
		"node1": []any{
			map[string]any{"key": "val"},
		},
	}
	result, ok := ConvertUpdatesFromJSON(raw)
	if !ok {
		t.Fatal("期望转换成功")
	}
	if len(result["node1"]) != 1 {
		t.Errorf("期望 len=1，实际=%d", len(result["node1"]))
	}
	if result["node1"][0]["key"] != "val" {
		t.Errorf("期望 key=val，实际=%v", result["node1"][0]["key"])
	}
}

// TestConvertUpdatesFromJSON_非map输入 验证非 map 输入返回 false。
func TestConvertUpdatesFromJSON_非map输入(t *testing.T) {
	_, ok := ConvertUpdatesFromJSON("not_a_map")
	if ok {
		t.Error("期望转换失败")
	}
}

// TestConvertUpdatesFromJSON_值非slice 验证值非 slice 返回 false。
func TestConvertUpdatesFromJSON_值非slice(t *testing.T) {
	raw := map[string]any{
		"node1": "not_a_slice",
	}
	_, ok := ConvertUpdatesFromJSON(raw)
	if ok {
		t.Error("期望转换失败")
	}
}

// TestConvertUpdatesFromJSON_slice元素非map 验证 slice 元素非 map 返回 false。
func TestConvertUpdatesFromJSON_slice元素非map(t *testing.T) {
	raw := map[string]any{
		"node1": []any{"not_a_map"},
	}
	_, ok := ConvertUpdatesFromJSON(raw)
	if ok {
		t.Error("期望转换失败")
	}
}

// ──── DeepCopyUpdates 测试 ────

// TestDeepCopyUpdates_nil 验证 nil 输入返回 nil。
func TestDeepCopyUpdates_nil(t *testing.T) {
	result := DeepCopyUpdates(nil)
	if result != nil {
		t.Errorf("期望 nil，实际=%v", result)
	}
}

// ──── SplitString 测试 ────

// TestSplitString_空分隔符 验证空分隔符返回原字符串。
func TestSplitString_空分隔符(t *testing.T) {
	result := SplitString("a.b.c", "")
	if len(result) != 1 || result[0] != "a.b.c" {
		t.Errorf("期望 [\"a.b.c\"]，实际=%v", result)
	}
}

// ──── ParseListIndexes 测试 ────

// TestParseListIndexes_纯索引 验证纯索引路径解析。
func TestParseListIndexes_纯索引(t *testing.T) {
	result := ParseListIndexes("[0]")
	if len(result) != 1 {
		t.Fatalf("期望 len=1，实际=%d", len(result))
	}
	if result[0] != 0 {
		t.Errorf("期望 0，实际=%v", result[0])
	}
}

// TestParseListIndexes_非整数索引 验证非整数索引作为字符串保留。
func TestParseListIndexes_非整数索引(t *testing.T) {
	result := ParseListIndexes("a[abc]")
	if len(result) != 2 {
		t.Fatalf("期望 len=2，实际=%d", len(result))
	}
	if result[0] != "a" {
		t.Errorf("期望 \"a\"，实际=%v", result[0])
	}
	if result[1] != "abc" {
		t.Errorf("期望 \"abc\"，实际=%v", result[1])
	}
}

// TestParseListIndexes_无括号 验证无括号时原样返回。
func TestParseListIndexes_无括号(t *testing.T) {
	result := ParseListIndexes("simple")
	if len(result) != 1 || result[0] != "simple" {
		t.Errorf("期望 [\"simple\"]，实际=%v", result)
	}
}

// TestParseListIndexes_无闭合括号 验证无闭合括号时中断解析。
func TestParseListIndexes_无闭合括号(t *testing.T) {
	result := ParseListIndexes("a[0")
	if len(result) != 1 || result[0] != "a" {
		t.Errorf("期望 [\"a\"]，实际=%v", result)
	}
}
