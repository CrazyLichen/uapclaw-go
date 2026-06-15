package state

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewAgentStateCollection 测试构造函数
func TestNewAgentStateCollection(t *testing.T) {
	coll := NewAgentStateCollection()
	if coll == nil {
		t.Fatal("NewAgentStateCollection 返回 nil")
	}
}

// TestAgentStateCollection_GetGlobal_零值Key返回完整全局状态 测试零值 key 返回完整全局状态
func TestAgentStateCollection_GetGlobal_零值Key返回完整全局状态(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"foo": "bar", "baz": 123})

	result := coll.GetGlobal(StateKey{})
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["foo"] != "bar" {
		t.Errorf("期望 foo=bar，实际 %v", m["foo"])
	}
	if m["baz"] != 123 {
		t.Errorf("期望 baz=123，实际 %v", m["baz"])
	}
}

// TestAgentStateCollection_GetGlobal_有Key返回对应值 测试有 key 返回对应值
func TestAgentStateCollection_GetGlobal_有Key返回对应值(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"foo": "bar"})

	result := coll.GetGlobal(StringKey("foo"))
	if result != "bar" {
		t.Errorf("期望 bar，实际 %v", result)
	}
}

// TestAgentStateCollection_GetGlobal_不存在的Key返回Nil 测试不存在的 key 返回 nil
func TestAgentStateCollection_GetGlobal_不存在的Key返回Nil(t *testing.T) {
	coll := NewAgentStateCollection()
	result := coll.GetGlobal(StringKey("nonexistent"))
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// TestAgentStateCollection_UpdateGlobal 测试更新全局状态
func TestAgentStateCollection_UpdateGlobal(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"a": 1})
	coll.UpdateGlobal(map[string]any{"b": 2})

	// a 仍存在，b 新增
	if coll.GetGlobal(StringKey("a")) != 1 {
		t.Errorf("期望 a=1，实际 %v", coll.GetGlobal(StringKey("a")))
	}
	if coll.GetGlobal(StringKey("b")) != 2 {
		t.Errorf("期望 b=2，实际 %v", coll.GetGlobal(StringKey("b")))
	}
}

// TestAgentStateCollection_Get_零值Key返回完整Agent状态 测试零值 key 返回完整 Agent 状态
func TestAgentStateCollection_Get_零值Key返回完整Agent状态(t *testing.T) {
	coll := NewAgentStateCollection()
	require.NoError(t, coll.Update(map[string]any{"x": "y"}))

	result := coll.GetAgent(StateKey{})
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["x"] != "y" {
		t.Errorf("期望 x=y，实际 %v", m["x"])
	}
}

// TestAgentStateCollection_Get_有Key返回对应值 测试有 key 返回对应值
func TestAgentStateCollection_Get_有Key返回对应值(t *testing.T) {
	coll := NewAgentStateCollection()
	require.NoError(t, coll.Update(map[string]any{"x": "y"}))

	result := coll.GetAgent(StringKey("x"))
	if result != "y" {
		t.Errorf("期望 y，实际 %v", result)
	}
}

// TestAgentStateCollection_Update 测试更新 Agent 状态
func TestAgentStateCollection_Update(t *testing.T) {
	coll := NewAgentStateCollection()
	require.NoError(t, coll.Update(map[string]any{"a": 1}))
	require.NoError(t, coll.Update(map[string]any{"b": 2}))

	if coll.GetAgent(StringKey("a")) != 1 {
		t.Errorf("期望 a=1，实际 %v", coll.GetAgent(StringKey("a")))
	}
	if coll.GetAgent(StringKey("b")) != 2 {
		t.Errorf("期望 b=2，实际 %v", coll.GetAgent(StringKey("b")))
	}
}

// TestAgentStateCollection_GetState 测试导出快照
func TestAgentStateCollection_GetState(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"g": 1})
	require.NoError(t, coll.Update(map[string]any{"a": 2}))

	st := coll.GetState()
	gs, ok := st[GlobalStateKey]
	if !ok {
		t.Fatal("快照中缺少 global_state")
	}
	gm, _ := gs.(map[string]any)
	if gm["g"] != 1 {
		t.Errorf("期望 global_state.g=1，实际 %v", gm["g"])
	}

	as, ok := st[AgentStateKey]
	if !ok {
		t.Fatal("快照中缺少 agent_state")
	}
	am, _ := as.(map[string]any)
	if am["a"] != 2 {
		t.Errorf("期望 agent_state.a=2，实际 %v", am["a"])
	}
}

// TestAgentStateCollection_SetState 测试从快照恢复
func TestAgentStateCollection_SetState(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"g": 1})
	require.NoError(t, coll.Update(map[string]any{"a": 2}))

	snapshot := coll.GetState()

	// 新实例从快照恢复
	coll2 := NewAgentStateCollection()
	coll2.SetState(snapshot)

	if coll2.GetGlobal(StringKey("g")) != 1 {
		t.Errorf("恢复后期望 g=1，实际 %v", coll2.GetGlobal(StringKey("g")))
	}
	if coll2.GetAgent(StringKey("a")) != 2 {
		t.Errorf("恢复后期望 a=2，实际 %v", coll2.GetAgent(StringKey("a")))
	}
}

// TestAgentStateCollection_Dump 测试完整导出
func TestAgentStateCollection_Dump(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"g": 1})
	require.NoError(t, coll.Update(map[string]any{"a": 2}))

	dump := coll.Dump()
	if _, ok := dump[GlobalStateKey]; !ok {
		t.Fatal("dump 中缺少 global_state")
	}
	if _, ok := dump[AgentStateKey]; !ok {
		t.Fatal("dump 中缺少 agent_state")
	}
}

// TestAgentStateCollection_状态隔离 测试 globalState 和 agentState 互不干扰
func TestAgentStateCollection_状态隔离(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"key": "global_val"})
	require.NoError(t, coll.Update(map[string]any{"key": "agent_val"}))

	if coll.GetGlobal(StringKey("key")) != "global_val" {
		t.Errorf("全局状态期望 global_val，实际 %v", coll.GetGlobal(StringKey("key")))
	}
	if coll.GetAgent(StringKey("key")) != "agent_val" {
		t.Errorf("Agent 状态期望 agent_val，实际 %v", coll.GetAgent(StringKey("key")))
	}
}

// TestAgentStateCollection_GetByPrefix 测试前缀查询
func TestAgentStateCollection_GetByPrefix(t *testing.T) {
	coll := NewAgentStateCollection()
	require.NoError(t, coll.Update(map[string]any{"nested": map[string]any{"child": "value"}}))

	result := coll.GetByPrefix(StringKey("child"), "nested")
	if result != "value" {
		t.Errorf("期望 value，实际 %v", result)
	}
}

// TestAgentStateCollection_GetByTransformer 测试转换函数
func TestAgentStateCollection_GetByTransformer(t *testing.T) {
	coll := NewAgentStateCollection()
	require.NoError(t, coll.Update(map[string]any{"x": 42}))

	result := coll.GetByTransformer(func(r ReadableState) any {
		return r.Get(StringKey("x"))
	})
	if result != 42 {
		t.Errorf("期望 42，实际 %v", result)
	}
}

// TestAgentStateCollection_实现State接口 测试 AgentStateCollection 满足 State 接口
func TestAgentStateCollection_实现State接口(t *testing.T) {
	var _ State = NewAgentStateCollection()
}

// TestAgentStateCollection_SetState_Nil测试 测试 nil 输入不 panic
func TestAgentStateCollection_SetState_Nil测试(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.SetState(nil) // 不应 panic
}

// TestAgentStateCollection_GlobalState返回内部引用 测试 GlobalState() 方法
func TestAgentStateCollection_GlobalState返回内部引用(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"g": 1})

	gs := coll.GlobalState()
	if gs == nil {
		t.Fatal("GlobalState 不应返回 nil")
	}
	val := gs.Get(StringKey("g"))
	if val != 1 {
		t.Errorf("期望 g=1，实际 %v", val)
	}
}

// TestAgentStateCollection_UpdateTrace_空实现 验证 Agent 层 UpdateTrace 是空实现。
func TestAgentStateCollection_UpdateTrace_空实现(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateTrace("span_data") // 不应 panic
}

// TestAgentStateCollection_Get_委托agentState 验证 Get 委托到 agentState。
func TestAgentStateCollection_Get_委托agentState(t *testing.T) {
	coll := NewAgentStateCollection()
	require.NoError(t, coll.Update(map[string]any{"x": "y"}))
	result := coll.Get(StringKey("x"))
	if result != "y" {
		t.Errorf("期望 y，实际 %v", result)
	}
}
