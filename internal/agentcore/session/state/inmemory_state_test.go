package state

import (
	"reflect"
	"testing"
)

// TestNewInMemoryState 验证构造函数创建非 nil 实例。
func TestNewInMemoryState(t *testing.T) {
	s := NewInMemoryState()
	if s == nil {
		t.Fatal("NewInMemoryState 返回 nil")
	}
}

// TestInMemoryState_Get_字符串key 验证字符串 key 读取。
func TestInMemoryState_Get_字符串key(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"name": "alice"})
	result := s.Get(StringKey("name"))
	if result != "alice" {
		t.Errorf("Get = %v, 期望 alice", result)
	}
}

// TestInMemoryState_Get_嵌套路径 验证嵌套路径读取。
func TestInMemoryState_Get_嵌套路径(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"user": map[string]any{"name": "alice"}})
	result := s.Get(StringKey("user.name"))
	if result != "alice" {
		t.Errorf("Get = %v, 期望 alice", result)
	}
}

// TestInMemoryState_Get_mapSchema 验证 map schema 批量读取。
func TestInMemoryState_Get_mapSchema(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{
		"user": map[string]any{"name": "alice", "age": 30},
	})
	result := s.Get(SchemaKey(map[string]any{
		"name": "user.name",
		"age":  "user.age",
	}))
	m := result.(map[string]any)
	if m["name"] != "alice" {
		t.Errorf("name = %v, 期望 alice", m["name"])
	}
}

// TestInMemoryState_Get_listSchema 验证 list schema 批量读取。
func TestInMemoryState_Get_listSchema(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{
		"user": map[string]any{"name": "alice", "age": 30},
	})
	result := s.Get(ListKey([]any{"user.name", "user.age"}))
	l := result.([]any)
	if l[0] != "alice" {
		t.Errorf("result[0] = %v, 期望 alice", l[0])
	}
}

// TestInMemoryState_Get_深拷贝 验证 Get 返回深拷贝，修改返回值不影响内部。
func TestInMemoryState_Get_深拷贝(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"user": map[string]any{"name": "alice"}})
	result := s.Get(StringKey("user"))
	result.(map[string]any)["name"] = "bob"
	// 再次获取，内部应不受影响
	result2 := s.Get(StringKey("user.name"))
	if result2 != "alice" {
		t.Errorf("Get 返回非深拷贝，修改返回值后内部被影响")
	}
}

// TestInMemoryState_Get_不存在 验证不存在的 key 返回 nil。
func TestInMemoryState_Get_不存在(t *testing.T) {
	s := NewInMemoryState()
	result := s.Get(StringKey("missing"))
	if result != nil {
		t.Errorf("Get(不存在的 key) = %v, 期望 nil", result)
	}
}

// TestInMemoryState_GetByPrefix 验证带前缀读取。
func TestInMemoryState_GetByPrefix(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{
		"node1": map[string]any{"name": "alice"},
	})
	result := s.GetByPrefix(StringKey("name"), "node1")
	if result != "alice" {
		t.Errorf("GetByPrefix = %v, 期望 alice", result)
	}
}

// TestInMemoryState_Update 验证 Update 更新状态。
func TestInMemoryState_Update(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1})
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("Update 后 Get = %v, 期望 1", s.Get(StringKey("a")))
	}
}

// TestInMemoryState_Update_深拷贝输入 验证 Update 深拷贝输入数据。
func TestInMemoryState_Update_深拷贝输入(t *testing.T) {
	s := NewInMemoryState()
	data := map[string]any{"a": 1}
	s.Update(data)
	data["a"] = 2
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("Update 未深拷贝输入，外部修改影响了内部状态")
	}
}

// TestInMemoryState_Update_覆盖 验证 Update 覆盖已有值。
func TestInMemoryState_Update_覆盖(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1})
	s.Update(map[string]any{"a": 2})
	if s.Get(StringKey("a")) != 2 {
		t.Errorf("覆盖后 Get = %v, 期望 2", s.Get(StringKey("a")))
	}
}

// TestInMemoryState_GetByTransformer 验证通过转换函数获取值。
func TestInMemoryState_GetByTransformer(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1, "b": 2})
	result := s.GetByTransformer(func(r ReadableState) any {
		return r.Get(StringKey("a"))
	})
	if result != 1 {
		t.Errorf("GetByTransformer = %v, 期望 1", result)
	}
}

// TestInMemoryState_GetState 验证 GetState 返回完整快照。
func TestInMemoryState_GetState(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1})
	state := s.GetState()
	if state["a"] != 1 {
		t.Errorf("GetState[\"a\"] = %v, 期望 1", state["a"])
	}
}

// TestInMemoryState_GetState_深拷贝 验证 GetState 返回深拷贝。
func TestInMemoryState_GetState_深拷贝(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1})
	state := s.GetState()
	state["a"] = 2
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("GetState 返回非深拷贝，修改后内部被影响")
	}
}

// TestInMemoryState_SetState 验证 SetState 从快照恢复状态。
func TestInMemoryState_SetState(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1})
	s.SetState(map[string]any{"b": 2})
	if s.Get(StringKey("a")) != nil {
		t.Errorf("SetState 后旧 key 仍存在")
	}
	if s.Get(StringKey("b")) != 2 {
		t.Errorf("SetState 后 Get = %v, 期望 2", s.Get(StringKey("b")))
	}
}

// TestInMemoryState_SetState_nil 验证 SetState 传入 nil 不影响当前状态。
func TestInMemoryState_SetState_nil(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1})
	s.SetState(nil)
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("SetState(nil) 后状态被清空，期望保留")
	}
}

// TestInMemoryState_接口满足 验证 InMemoryState 满足 State 接口。
func TestInMemoryState_接口满足(t *testing.T) {
	var _ State = (*InMemoryState)(nil)
}

// TestInMemoryState_完整读写流程 验证完整的读写流程。
func TestInMemoryState_完整读写流程(t *testing.T) {
	s := NewInMemoryState()

	// 初始写入
	s.Update(map[string]any{
		"user": map[string]any{
			"name": "alice",
			"age":  30,
		},
		"tags": []any{"go", "python"},
	})

	// 读取嵌套值
	name := s.Get(StringKey("user.name"))
	if name != "alice" {
		t.Errorf("user.name = %v, 期望 alice", name)
	}

	// 批量 schema 读取
	result := s.Get(SchemaKey(map[string]any{
		"userName": "user.name",
		"userAge":  "user.age",
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
	s2 := NewInMemoryState()
	s2.SetState(snapshot)
	if s2.Get(StringKey("user.name")) != "alice" {
		t.Errorf("恢复后 user.name = %v, 期望 alice", s2.Get(StringKey("user.name")))
	}
}
