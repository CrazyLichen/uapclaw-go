package state

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewInMemoryState 验证构造函数创建非 nil 实例。
func TestNewInMemoryState(t *testing.T) {
	s := NewInMemoryStateLike()
	if s == nil {
		t.Fatal("NewInMemoryStateLike 返回 nil")
	}
}

// TestInMemoryStateLike_Get_字符串key 验证字符串 key 读取。
func TestInMemoryStateLike_Get_字符串key(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{"name": "alice"}))
	result := s.Get(StringKey("name"))
	if result != "alice" {
		t.Errorf("Get = %v, 期望 alice", result)
	}
}

// TestInMemoryStateLike_Get_嵌套路径 验证嵌套路径读取。
func TestInMemoryStateLike_Get_嵌套路径(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{"user": map[string]any{"name": "alice"}}))
	result := s.Get(StringKey("user.name"))
	if result != "alice" {
		t.Errorf("Get = %v, 期望 alice", result)
	}
}

// TestInMemoryStateLike_Get_mapSchema 验证 map schema 批量读取。
func TestInMemoryStateLike_Get_mapSchema(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{
		"user": map[string]any{"name": "alice", "age": 30},
	}))
	result := s.Get(SchemaKey(map[string]any{
		"name": "${user.name}",
		"age":  "${user.age}",
	}))
	m := result.(map[string]any)
	if m["name"] != "alice" {
		t.Errorf("name = %v, 期望 alice", m["name"])
	}
}

// TestInMemoryStateLike_Get_listSchema 验证 list schema 批量读取。
func TestInMemoryStateLike_Get_listSchema(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{
		"user": map[string]any{"name": "alice", "age": 30},
	}))
	result := s.Get(ListKey([]any{"${user.name}", "${user.age}"}))
	l := result.([]any)
	if l[0] != "alice" {
		t.Errorf("result[0] = %v, 期望 alice", l[0])
	}
}

// TestInMemoryStateLike_Get_深拷贝 验证 Get 返回深拷贝，修改返回值不影响内部。
func TestInMemoryStateLike_Get_深拷贝(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{"user": map[string]any{"name": "alice"}}))
	result := s.Get(StringKey("user"))
	result.(map[string]any)["name"] = "bob"
	// 再次获取，内部应不受影响
	result2 := s.Get(StringKey("user.name"))
	if result2 != "alice" {
		t.Errorf("Get 返回非深拷贝，修改返回值后内部被影响")
	}
}

// TestInMemoryStateLike_Get_不存在 验证不存在的 key 返回 nil。
func TestInMemoryStateLike_Get_不存在(t *testing.T) {
	s := NewInMemoryStateLike()
	result := s.Get(StringKey("missing"))
	if result != nil {
		t.Errorf("Get(不存在的 key) = %v, 期望 nil", result)
	}
}

// TestInMemoryStateLike_GetByPrefix 验证带前缀读取。
func TestInMemoryStateLike_GetByPrefix(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{
		"node1": map[string]any{"name": "alice"},
	}))
	result := s.GetByPrefix(StringKey("name"), "node1")
	if result != "alice" {
		t.Errorf("GetByPrefix = %v, 期望 alice", result)
	}
}

// TestInMemoryStateLike_Update 验证 Update 更新状态。
func TestInMemoryStateLike_Update(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{"a": 1}))
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("Update 后 Get = %v, 期望 1", s.Get(StringKey("a")))
	}
}

// TestInMemoryStateLike_Update_深拷贝输入 验证 Update 深拷贝输入数据。
func TestInMemoryStateLike_Update_深拷贝输入(t *testing.T) {
	s := NewInMemoryStateLike()
	data := map[string]any{"a": 1}
	require.NoError(t, s.Update(data))
	data["a"] = 2
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("Update 未深拷贝输入，外部修改影响了内部状态")
	}
}

// TestInMemoryStateLike_Update_覆盖 验证 Update 覆盖已有值。
func TestInMemoryStateLike_Update_覆盖(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{"a": 1}))
	require.NoError(t, s.Update(map[string]any{"a": 2}))
	if s.Get(StringKey("a")) != 2 {
		t.Errorf("覆盖后 Get = %v, 期望 2", s.Get(StringKey("a")))
	}
}

// TestInMemoryStateLike_GetByTransformer 验证通过转换函数获取值。
func TestInMemoryStateLike_GetByTransformer(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{"a": 1, "b": 2}))
	result := s.GetByTransformer(func(r ReadableStateLike) any {
		return r.Get(StringKey("a"))
	})
	if result != 1 {
		t.Errorf("GetByTransformer = %v, 期望 1", result)
	}
}

// TestInMemoryStateLike_GetState 验证 GetState 返回完整快照。
func TestInMemoryStateLike_GetState(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{"a": 1}))
	state := s.GetState()
	if state["a"] != 1 {
		t.Errorf("GetState[\"a\"] = %v, 期望 1", state["a"])
	}
}

// TestInMemoryStateLike_GetState_深拷贝 验证 GetState 返回深拷贝。
func TestInMemoryStateLike_GetState_深拷贝(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{"a": 1}))
	state := s.GetState()
	state["a"] = 2
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("GetState 返回非深拷贝，修改后内部被影响")
	}
}

// TestInMemoryStateLike_SetState 验证 SetState 从快照恢复状态。
func TestInMemoryStateLike_SetState(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{"a": 1}))
	s.SetState(map[string]any{"b": 2})
	if s.Get(StringKey("a")) != nil {
		t.Errorf("SetState 后旧 key 仍存在")
	}
	if s.Get(StringKey("b")) != 2 {
		t.Errorf("SetState 后 Get = %v, 期望 2", s.Get(StringKey("b")))
	}
}

// TestInMemoryStateLike_SetState_nil 验证 SetState 传入 nil 不影响当前状态。
func TestInMemoryStateLike_SetState_nil(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{"a": 1}))
	s.SetState(nil)
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("SetState(nil) 后状态被清空，期望保留")
	}
}

// TestInMemoryStateLike_接口满足 验证 InMemoryStateLike 满足 StateLike 接口。
func TestInMemoryStateLike_接口满足(t *testing.T) {
	var _ StateLike = (*InMemoryStateLike)(nil)
}

// TestInMemoryStateLike_完整读写流程 验证完整的读写流程。
func TestInMemoryStateLike_完整读写流程(t *testing.T) {
	s := NewInMemoryStateLike()

	// 初始写入
	require.NoError(t, s.Update(map[string]any{
		"user": map[string]any{
			"name": "alice",
			"age":  30,
		},
		"tags": []any{"go", "python"},
	}))

	// 读取嵌套值
	name := s.Get(StringKey("user.name"))
	if name != "alice" {
		t.Errorf("user.name = %v, 期望 alice", name)
	}

	// 批量 schema 读取
	result := s.Get(SchemaKey(map[string]any{
		"userName": "${user.name}",
		"userAge":  "${user.age}",
	}))
	m := result.(map[string]any)
	if m["userName"] != "alice" {
		t.Errorf("userName = %v, 期望 alice", m["userName"])
	}

	// 获取完整快照
	snapshot := s.GetState()
	if !reflect.DeepEqual(snapshot["tags"], []any{"go", "python"}) {
		t.Errorf("tags = %v, 期望 [go python]", snapshot["tags"])
	}

	// 快照恢复
	s2 := NewInMemoryStateLike()
	s2.SetState(snapshot)
	if s2.Get(StringKey("user.name")) != "alice" {
		t.Errorf("恢复后 user.name = %v, 期望 alice", s2.Get(StringKey("user.name")))
	}
}

// TestInMemoryStateLike_GetGlobal_返回nil 验证 GetGlobal 返回 nil。
func TestInMemoryStateLike_GetGlobal_返回nil(t *testing.T) {
	s := NewInMemoryStateLike()
	result := s.GetGlobal(StringKey("key"))
	if result != nil {
		t.Errorf("GetGlobal 应返回 nil，实际=%v", result)
	}
}

// TestInMemoryStateLike_UpdateGlobal_空操作 验证 UpdateGlobal 不影响内部状态。
func TestInMemoryStateLike_UpdateGlobal_空操作(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{"a": 1}))
	s.UpdateGlobal(map[string]any{"a": 2})
	// UpdateGlobal 是空操作，不应影响内部状态
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("UpdateGlobal 不应影响内部状态，实际=%v", s.Get(StringKey("a")))
	}
}

// TestInMemoryStateLike_UpdateTrace_空操作 验证 UpdateTrace 不影响内部状态。
func TestInMemoryStateLike_UpdateTrace_空操作(t *testing.T) {
	s := NewInMemoryStateLike()
	s.UpdateTrace("span_data") // 不应 panic
}

// TestInMemoryStateLike_Dump_委托GetState 验证 Dump 返回与 GetState 相同的快照。
func TestInMemoryStateLike_Dump_委托GetState(t *testing.T) {
	s := NewInMemoryStateLike()
	require.NoError(t, s.Update(map[string]any{"a": 1}))
	dump := s.Dump()
	if dump["a"] != 1 {
		t.Errorf("Dump()[\"a\"] = %v, 期望 1", dump["a"])
	}
}

// TestInMemoryStateLike_SetGlobal 验证 SetGlobal 空操作不 panic
func TestInMemoryStateLike_SetGlobal(t *testing.T) {
	s := NewInMemoryStateLike()
	s.SetGlobal(map[string]any{"key": "val"})
	// 空操作，不 panic 即可
}

// TestInMemoryStateLike_UpdateGlobal 验证 UpdateGlobal 空操作不 panic
func TestInMemoryStateLike_UpdateGlobal(t *testing.T) {
	s := NewInMemoryStateLike()
	s.UpdateGlobal(map[string]any{"key": "val"})
	// 空操作，不 panic 即可
}

// TestInMemoryStateLike_UpdateTrace 验证 UpdateTrace 空操作不 panic
func TestInMemoryStateLike_UpdateTrace(t *testing.T) {
	s := NewInMemoryStateLike()
	s.UpdateTrace(nil)
	// 空操作，不 panic 即可
}

// TestInMemoryStateLike_GetGlobal 验证 GetGlobal 返回 nil
func TestInMemoryStateLike_GetGlobal(t *testing.T) {
	s := NewInMemoryStateLike()
	result := s.GetGlobal(StateKey{})
	if result != nil {
		t.Errorf("GetGlobal 应返回 nil，实际 %v", result)
	}
}
