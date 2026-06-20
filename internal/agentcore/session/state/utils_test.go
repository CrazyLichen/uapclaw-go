package state

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
)

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

// ──── getValueByNestedPathMap 测试 ────

// TestGetValueByNestedPathMap_基本 验证基本 map 前缀查找。
func TestGetValueByNestedPathMap_基本(t *testing.T) {
	data := map[string]any{
		"node1": map[string]any{"name": "alice"},
	}
	result := getValueByNestedPathMap("node1", data)
	if result == nil {
		t.Fatal("期望非 nil")
	}
	if result["name"] != "alice" {
		t.Errorf("期望 name=alice，实际=%v", result["name"])
	}
}

// TestGetValueByNestedPathMap_nil源 验证 nil 源返回 nil。
func TestGetValueByNestedPathMap_nil源(t *testing.T) {
	result := getValueByNestedPathMap("key", nil)
	if result != nil {
		t.Errorf("期望 nil，实际=%v", result)
	}
}

// TestGetValueByNestedPathMap_值不是map 验证值不是 map 时返回 nil。
func TestGetValueByNestedPathMap_值不是map(t *testing.T) {
	data := map[string]any{
		"key": "scalar_value",
	}
	result := getValueByNestedPathMap("key", data)
	if result != nil {
		t.Errorf("期望 nil，实际=%v", result)
	}
}

// ──── getBySchemaMap 更多分支测试 ────

// TestGetBySchemaMap_列表schema值 验证 map schema 中列表 schema 值。
func TestGetBySchemaMap_列表schema值(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"tags": []any{"go", "py"}},
	}
	schema := map[string]any{
		"tags": []any{"${user.tags}"},
	}
	result := getBySchemaMap(schema, data)
	if result["tags"] == nil {
		t.Error("期望 tags 非 nil")
	}
}

// TestGetBySchemaMap_mapSchema值 验证 map schema 中嵌套 map schema 值。
func TestGetBySchemaMap_mapSchema值(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice"},
	}
	schema := map[string]any{
		"info": map[string]any{"n": "${user.name}"},
	}
	result := getBySchemaMap(schema, data)
	info, ok := result["info"].(map[string]any)
	if !ok {
		t.Fatalf("期望 info 为 map，实际=%T", result["info"])
	}
	if info["n"] != "alice" {
		t.Errorf("期望 n=alice，实际=%v", info["n"])
	}
}

// TestGetBySchemaMap_非字符串非map非list 验证其他类型值原样保留。
func TestGetBySchemaMap_非字符串非map非list(t *testing.T) {
	data := map[string]any{}
	schema := map[string]any{
		"count": 42,
	}
	result := getBySchemaMap(schema, data)
	if result["count"] != 42 {
		t.Errorf("期望 count=42，实际=%v", result["count"])
	}
}

// ──── getBySchemaList 更多分支测试 ────

// TestGetBySchemaList_mapSchema值 验证 list schema 中嵌套 map schema 值。
func TestGetBySchemaList_mapSchema值(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice"},
	}
	schema := []any{
		map[string]any{"n": "${user.name}"},
	}
	result := getBySchemaList(schema, data)
	m, ok := result[0].(map[string]any)
	if !ok {
		t.Fatalf("期望 l[0] 为 map，实际=%T", result[0])
	}
	if m["n"] != "alice" {
		t.Errorf("期望 n=alice，实际=%v", m["n"])
	}
}

// TestGetBySchemaList_列表schema值 验证 list schema 中嵌套 list schema 值。
func TestGetBySchemaList_列表schema值(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice"},
	}
	schema := []any{
		[]any{"${user.name}"},
	}
	result := getBySchemaList(schema, data)
	inner, ok := result[0].([]any)
	if !ok {
		t.Fatalf("期望 l[0] 为 []any，实际=%T", result[0])
	}
	if inner[0] != "alice" {
		t.Errorf("期望 inner[0]=alice，实际=%v", inner[0])
	}
}

// TestGetBySchemaList_非字符串非map非list 验证其他类型值原样保留。
func TestGetBySchemaList_非字符串非map非list(t *testing.T) {
	data := map[string]any{}
	schema := []any{42, true}
	result := getBySchemaList(schema, data)
	if result[0] != 42 {
		t.Errorf("期望 result[0]=42，实际=%v", result[0])
	}
	if result[1] != true {
		t.Errorf("期望 result[1]=true，实际=%v", result[1])
	}
}

// ──── getBySchema 更多分支测试 ────

// TestGetBySchema_非根层普通字符串 验证非根层非引用路径字符串作为默认值返回。
func TestGetBySchema_非根层普通字符串(t *testing.T) {
	data := map[string]any{"name": "alice"}
	result := getBySchema(StringKey("hello"), data, false)
	if result != "hello" {
		t.Errorf("期望返回默认值 \"hello\"，实际=%v", result)
	}
}

// TestGetBySchema_嵌套路径前缀为空 验证 nestedPath 为空字符串时直接使用 data。
func TestGetBySchema_嵌套路径前缀为空(t *testing.T) {
	data := map[string]any{"name": "alice"}
	result := getBySchema(StringKey("name"), data, "")
	if result != "alice" {
		t.Errorf("期望 alice，实际=%v", result)
	}
}

// TestGetBySchema_嵌套路径后data为nil 验证 nestedPath 定位后 data 为 nil 返回 nil。
func TestGetBySchema_嵌套路径后data为nil(t *testing.T) {
	data := map[string]any{
		"node1": "scalar_not_map",
	}
	result := getBySchema(StringKey("name"), data, "node1")
	if result != nil {
		t.Errorf("期望 nil，实际=%v", result)
	}
}

// TestGetBySchema_isRoot为false参数 验证 isRoot bool 参数传递。
func TestGetBySchema_isRoot为false参数(t *testing.T) {
	data := map[string]any{}
	result := getBySchema(StringKey("default_val"), data, false)
	if result != "default_val" {
		t.Errorf("期望返回默认值，实际=%v", result)
	}
}

// 确保 utils 包被引用（编译一致性）
var _ = utils.IsRefPath
