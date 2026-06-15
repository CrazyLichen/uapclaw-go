package state

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewInMemoryCommitState_默认 验证默认构造创建非 nil 实例。
func TestNewInMemoryCommitState_默认(t *testing.T) {
	cs := NewInMemoryCommitState()
	if cs == nil {
		t.Fatal("NewInMemoryCommitState 返回 nil")
	}
}

// TestNewInMemoryCommitState_传入State 验证传入底层 State 构造。
func TestNewInMemoryCommitState_传入State(t *testing.T) {
	inner := NewInMemoryState()
	require.NoError(t, inner.Update(map[string]any{"a": 1}))
	cs := NewInMemoryCommitState(inner)
	result := cs.Get(StringKey("a"))
	if result != 1 {
		t.Errorf("Get = %v, 期望 1", result)
	}
}

// TestInMemoryCommitState_Update_禁止 验证直接调用 Update 返回错误。
func TestInMemoryCommitState_Update_禁止(t *testing.T) {
	cs := NewInMemoryCommitState()
	err := cs.Update(map[string]any{"a": 1})
	if err == nil {
		t.Error("Update 应返回错误")
	}
}

// TestInMemoryCommitState_UpdateByID_基本 验证按节点 ID 暂存更新。
func TestInMemoryCommitState_UpdateByID_基本(t *testing.T) {
	cs := NewInMemoryCommitState()
	if err := cs.UpdateByID("node1", map[string]any{"a": 1}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	// 暂存更新不影响底层 state
	result := cs.Get(StringKey("a"))
	if result != nil {
		t.Errorf("UpdateByID 后 Get = %v, 期望 nil（未 commit）", result)
	}
}

// TestInMemoryCommitState_UpdateByID_空nodeID 验证空 nodeID 返回错误。
func TestInMemoryCommitState_UpdateByID_空nodeID(t *testing.T) {
	cs := NewInMemoryCommitState()
	err := cs.UpdateByID("", map[string]any{"a": 1})
	if err == nil {
		t.Error("空 nodeID 应返回错误")
	}
}

// TestInMemoryCommitState_Commit_指定节点 验证提交指定节点的暂存更新。
func TestInMemoryCommitState_Commit_指定节点(t *testing.T) {
	cs := NewInMemoryCommitState()
	if err := cs.UpdateByID("node1", map[string]any{"a": 1}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.Commit("node1")
	result := cs.Get(StringKey("a"))
	if result != 1 {
		t.Errorf("Commit 后 Get = %v, 期望 1", result)
	}
}

// TestInMemoryCommitState_Commit_全部 验证不传 nodeID 提交全部暂存。
func TestInMemoryCommitState_Commit_全部(t *testing.T) {
	cs := NewInMemoryCommitState()
	if err := cs.UpdateByID("node1", map[string]any{"a": 1}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := cs.UpdateByID("node2", map[string]any{"b": 2}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.Commit()
	if cs.Get(StringKey("a")) != 1 {
		t.Errorf("全部 Commit 后 Get(\"a\") = %v, 期望 1", cs.Get(StringKey("a")))
	}
	if cs.Get(StringKey("b")) != 2 {
		t.Errorf("全部 Commit 后 Get(\"b\") = %v, 期望 2", cs.Get(StringKey("b")))
	}
}

// TestInMemoryCommitState_Commit_清空暂存 验证 commit 后暂存被清空。
func TestInMemoryCommitState_Commit_清空暂存(t *testing.T) {
	cs := NewInMemoryCommitState()
	if err := cs.UpdateByID("node1", map[string]any{"a": 1}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.Commit("node1")
	updates := cs.GetUpdates()
	if len(updates["node1"]) != 0 {
		t.Errorf("Commit 后暂存未清空，len = %d", len(updates["node1"]))
	}
}

// TestInMemoryCommitState_Rollback 验证回滚丢弃暂存更新。
func TestInMemoryCommitState_Rollback(t *testing.T) {
	cs := NewInMemoryCommitState()
	if err := cs.UpdateByID("node1", map[string]any{"a": 1}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.Rollback("node1")
	// commit 不应生效
	cs.Commit("node1")
	result := cs.Get(StringKey("a"))
	if result != nil {
		t.Errorf("Rollback 后 Commit 不应生效，Get = %v, 期望 nil", result)
	}
}

// TestInMemoryCommitState_GetUpdates 验证获取暂存更新。
func TestInMemoryCommitState_GetUpdates(t *testing.T) {
	cs := NewInMemoryCommitState()
	if err := cs.UpdateByID("node1", map[string]any{"a": 1}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := cs.UpdateByID("node1", map[string]any{"b": 2}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	updates := cs.GetUpdates()
	if len(updates["node1"]) != 2 {
		t.Errorf("GetUpdates 长度 = %d, 期望 2", len(updates["node1"]))
	}
}

// TestInMemoryCommitState_SetUpdates 验证设置暂存更新。
func TestInMemoryCommitState_SetUpdates(t *testing.T) {
	cs := NewInMemoryCommitState()
	updates := map[string][]map[string]any{
		"node1": {{"a": 1}},
	}
	cs.SetUpdates(updates)
	result := cs.GetUpdates()
	if len(result["node1"]) != 1 {
		t.Errorf("SetUpdates 后 GetUpdates 长度 = %d, 期望 1", len(result["node1"]))
	}
}

// TestInMemoryCommitState_SetUpdates_nil 验证传入 nil 不影响暂存。
func TestInMemoryCommitState_SetUpdates_nil(t *testing.T) {
	cs := NewInMemoryCommitState()
	if err := cs.UpdateByID("node1", map[string]any{"a": 1}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.SetUpdates(nil)
	if len(cs.GetUpdates()) == 0 {
		t.Error("SetUpdates(nil) 不应清空已有暂存")
	}
}

// TestInMemoryCommitState_GetByPrefix 验证委托给底层 state 的 GetByPrefix。
func TestInMemoryCommitState_GetByPrefix(t *testing.T) {
	inner := NewInMemoryState()
	require.NoError(t, inner.Update(map[string]any{
		"node1": map[string]any{"name": "alice"},
	}))
	cs := NewInMemoryCommitState(inner)
	result := cs.GetByPrefix(StringKey("name"), "node1")
	if result != "alice" {
		t.Errorf("GetByPrefix = %v, 期望 alice", result)
	}
}

// TestInMemoryCommitState_GetByTransformer 验证委托给底层 state 的 GetByTransformer。
func TestInMemoryCommitState_GetByTransformer(t *testing.T) {
	inner := NewInMemoryState()
	require.NoError(t, inner.Update(map[string]any{"a": 1}))
	cs := NewInMemoryCommitState(inner)
	result := cs.GetByTransformer(func(r ReadableState) any {
		return r.Get(StringKey("a"))
	})
	if result != 1 {
		t.Errorf("GetByTransformer = %v, 期望 1", result)
	}
}

// TestInMemoryCommitState_GetState 验证委托给底层 state 的 GetState。
func TestInMemoryCommitState_GetState(t *testing.T) {
	inner := NewInMemoryState()
	require.NoError(t, inner.Update(map[string]any{"a": 1}))
	cs := NewInMemoryCommitState(inner)
	state := cs.GetState()
	if state["a"] != 1 {
		t.Errorf("GetState[\"a\"] = %v, 期望 1", state["a"])
	}
}

// TestInMemoryCommitState_SetState 验证委托给底层 state 的 SetState。
func TestInMemoryCommitState_SetState(t *testing.T) {
	inner := NewInMemoryState()
	require.NoError(t, inner.Update(map[string]any{"a": 1}))
	cs := NewInMemoryCommitState(inner)
	cs.SetState(map[string]any{"b": 2})
	if cs.Get(StringKey("a")) != nil {
		t.Error("SetState 后旧 key 仍存在")
	}
	if cs.Get(StringKey("b")) != 2 {
		t.Errorf("SetState 后 Get = %v, 期望 2", cs.Get(StringKey("b")))
	}
}

// TestInMemoryCommitState_接口满足 验证 InMemoryCommitState 满足 CommitState 接口。
func TestInMemoryCommitState_接口满足(t *testing.T) {
	var _ CommitState = (*InMemoryCommitState)(nil)
}

// TestInMemoryCommitState_多次UpdateByID 验证同一节点多次 UpdateByID 累积。
func TestInMemoryCommitState_多次UpdateByID(t *testing.T) {
	cs := NewInMemoryCommitState()
	if err := cs.UpdateByID("node1", map[string]any{"a": 1}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := cs.UpdateByID("node1", map[string]any{"b": 2}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.Commit("node1")
	if cs.Get(StringKey("a")) != 1 {
		t.Errorf("Get(\"a\") = %v, 期望 1", cs.Get(StringKey("a")))
	}
	if cs.Get(StringKey("b")) != 2 {
		t.Errorf("Get(\"b\") = %v, 期望 2", cs.Get(StringKey("b")))
	}
}

// TestInMemoryCommitState_完整事务流程 验证完整的事务流程。
func TestInMemoryCommitState_完整事务流程(t *testing.T) {
	cs := NewInMemoryCommitState()

	// 暂存更新
	if err := cs.UpdateByID("node1", map[string]any{"a": 1}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}

	// 验证未提交
	if cs.Get(StringKey("a")) != nil {
		t.Error("未 commit 时不应能读到数据")
	}

	// 提交
	cs.Commit("node1")
	if cs.Get(StringKey("a")) != 1 {
		t.Errorf("commit 后 Get = %v, 期望 1", cs.Get(StringKey("a")))
	}

	// 再次暂存并回滚
	if err := cs.UpdateByID("node1", map[string]any{"b": 2}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.Rollback("node1")
	cs.Commit("node1") // 回滚后 commit 无数据
	if cs.Get(StringKey("b")) != nil {
		t.Error("rollback 后 commit 不应写入数据")
	}
}

// TestInMemoryCommitState_UpdateByID_深拷贝 验证 UpdateByID 深拷贝输入数据。
func TestInMemoryCommitState_UpdateByID_深拷贝(t *testing.T) {
	cs := NewInMemoryCommitState()
	data := map[string]any{"a": 1}
	if err := cs.UpdateByID("node1", data); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	data["a"] = 2
	cs.Commit("node1")
	if cs.Get(StringKey("a")) != 1 {
		t.Errorf("UpdateByID 未深拷贝输入，外部修改影响了内部")
	}
}

// TestInMemoryCommitState_Commit_无暂存 验证没有暂存时 Commit 不报错。
func TestInMemoryCommitState_Commit_无暂存(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.Commit("nonexistent") // 不应 panic
}

// TestInMemoryCommitState_GetUpdates_深拷贝 验证 GetUpdates 返回的不是内部引用。
func TestInMemoryCommitState_GetUpdates_深拷贝(t *testing.T) {
	cs := NewInMemoryCommitState()
	if err := cs.UpdateByID("node1", map[string]any{"a": 1}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	updates := cs.GetUpdates()
	// 修改返回值不应影响内部
	if len(updates["node1"]) > 0 {
		updates["node1"][0]["a"] = 999
		updates2 := cs.GetUpdates()
		if updates2["node1"][0]["a"] == 999 {
			t.Error("GetUpdates 返回了内部引用，修改后影响了内部")
		}
	}
}

// TestInMemoryCommitState_Rollback_不影响其他节点 验证回滚一个节点不影响其他节点。
func TestInMemoryCommitState_Rollback_不影响其他节点(t *testing.T) {
	cs := NewInMemoryCommitState()
	if err := cs.UpdateByID("node1", map[string]any{"a": 1}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := cs.UpdateByID("node2", map[string]any{"b": 2}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.Rollback("node1")
	cs.Commit("node2")
	if cs.Get(StringKey("b")) != 2 {
		t.Errorf("回滚 node1 后 node2 commit 应正常，Get(\"b\") = %v", cs.Get(StringKey("b")))
	}
}

// TestInMemoryCommitState_多节点提交 验证分别提交多个节点。
func TestInMemoryCommitState_多节点提交(t *testing.T) {
	cs := NewInMemoryCommitState()
	if err := cs.UpdateByID("node1", map[string]any{"a": 1}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := cs.UpdateByID("node2", map[string]any{"b": 2}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.Commit("node1")
	if cs.Get(StringKey("a")) != 1 {
		t.Errorf("node1 commit 后 Get(\"a\") = %v, 期望 1", cs.Get(StringKey("a")))
	}
	// node2 未 commit
	if cs.Get(StringKey("b")) != nil {
		t.Errorf("node2 未 commit，Get(\"b\") = %v, 期望 nil", cs.Get(StringKey("b")))
	}
	cs.Commit("node2")
	if cs.Get(StringKey("b")) != 2 {
		t.Errorf("node2 commit 后 Get(\"b\") = %v, 期望 2", cs.Get(StringKey("b")))
	}
}
