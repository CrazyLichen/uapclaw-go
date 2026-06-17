package state

import "testing"

// TestNewWorkflowStateCollection 测试构造函数
func TestNewWorkflowStateCollection(t *testing.T) {
	ioState := NewInMemoryCommitState()
	globalState := NewInMemoryCommitState()
	compState := NewInMemoryCommitState()
	workflowState := NewInMemoryCommitState()

	sc := NewWorkflowStateCollection(ioState, globalState, compState, workflowState, nil, "parent1", "node1")

	if sc.parentID != "parent1" {
		t.Errorf("期望 parentID=parent1，实际=%s", sc.parentID)
	}
	if sc.nodeID != "node1" {
		t.Errorf("期望 nodeID=node1，实际=%s", sc.nodeID)
	}
	// traceState nil 防御：应被初始化为空 map
	if sc.traceState == nil {
		t.Error("traceState 为 nil，期望被初始化为空 map")
	}
}

// TestGetGlobal_三级回退 测试三级回退查询
func TestGetGlobal_三级回退(t *testing.T) {
	ioState := NewInMemoryCommitState()
	globalState := NewInMemoryCommitState()
	compState := NewInMemoryCommitState()
	workflowState := NewInMemoryCommitState()

	sc := NewWorkflowStateCollection(ioState, globalState, compState, workflowState, nil, "parent1", "node1")

	// 1. globalState 有值
	if err := globalState.UpdateByID("default", map[string]any{"key1": "from_global"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	globalState.Commit("default")
	result := sc.GetGlobal(StringKey("key1"))
	if result != "from_global" {
		t.Errorf("期望从 globalState 获取 'from_global'，实际=%v", result)
	}

	// 2. globalState 无值，回退到 ioState[parentID]
	if err := ioState.UpdateByID("default", map[string]any{"parent1": map[string]any{"key2": "from_io_parent"}}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	ioState.Commit("default")
	sc2 := NewWorkflowStateCollection(ioState, NewInMemoryCommitState(), compState, workflowState, nil, "parent1", "node1")
	result = sc2.GetGlobal(StringKey("key2"))
	if result != "from_io_parent" {
		t.Errorf("期望从 ioState[parentID] 回退获取 'from_io_parent'，实际=%v", result)
	}

	// 3. globalState 和 ioState[parentID] 都无值，回退到 ioState[nodeID]
	if err := ioState.UpdateByID("default2", map[string]any{"node1": map[string]any{"key3": "from_io_node"}}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	ioState.Commit("default2")
	sc3 := NewWorkflowStateCollection(ioState, NewInMemoryCommitState(), compState, workflowState, nil, "parent1", "node1")
	result = sc3.GetGlobal(StringKey("key3"))
	if result != "from_io_node" {
		t.Errorf("期望从 ioState[nodeID] 回退获取 'from_io_node'，实际=%v", result)
	}
}

// TestUpdateGlobal 测试更新全局状态
func TestUpdateGlobal(t *testing.T) {
	ioState := NewInMemoryCommitState()
	globalState := NewInMemoryCommitState()
	compState := NewInMemoryCommitState()
	workflowState := NewInMemoryCommitState()

	sc := NewWorkflowStateCollection(ioState, globalState, compState, workflowState, nil, "", "node1")

	// 更新全局状态
	sc.UpdateGlobal(map[string]any{"key1": "value1"})

	// 暂存区应有更新
	updates := globalState.GetUpdates()
	if len(updates["node1"]) == 0 {
		t.Error("期望 globalState 有 node1 的暂存更新")
	}

	// 提交后可读取
	globalState.Commit("node1")
	result := globalState.Get(StringKey("key1"))
	if result != "value1" {
		t.Errorf("期望提交后获取 'value1'，实际=%v", result)
	}

	// data 为 nil 时静默返回
	sc.UpdateGlobal(nil)
}

// TestUpdateTrace 测试更新追踪状态
func TestUpdateTrace(t *testing.T) {
	ioState := NewInMemoryCommitState()
	globalState := NewInMemoryCommitState()
	compState := NewInMemoryCommitState()
	workflowState := NewInMemoryCommitState()

	sc := NewWorkflowStateCollection(ioState, globalState, compState, workflowState, nil, "", "node1")

	sc.UpdateTrace("span_data")
	if sc.traceState["node1"] != "span_data" {
		t.Errorf("期望 traceState[node1]='span_data'，实际=%v", sc.traceState["node1"])
	}
}

// TestCommitCmp 测试提交当前节点的 comp + io
func TestCommitCmp(t *testing.T) {
	ioState := NewInMemoryCommitState()
	globalState := NewInMemoryCommitState()
	compState := NewInMemoryCommitState()
	workflowState := NewInMemoryCommitState()

	sc := NewWorkflowStateCollection(ioState, globalState, compState, workflowState, nil, "", "node1")

	// 暂存更新
	if err := compState.UpdateByID("node1", map[string]any{"comp_key": "comp_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := ioState.UpdateByID("node1", map[string]any{"io_key": "io_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}

	// 提交
	sc.CommitCmp()

	// 验证 compState 已提交
	result := compState.Get(StringKey("comp_key"))
	if result != "comp_val" {
		t.Errorf("期望 compState 提交后获取 'comp_val'，实际=%v", result)
	}

	// 验证 ioState 已提交
	result = ioState.Get(StringKey("io_key"))
	if result != "io_val" {
		t.Errorf("期望 ioState 提交后获取 'io_val'，实际=%v", result)
	}
}

// TestGet_组件状态 测试 Get 方法
func TestGet_组件状态(t *testing.T) {
	ioState := NewInMemoryCommitState()
	globalState := NewInMemoryCommitState()
	compState := NewInMemoryCommitState()
	workflowState := NewInMemoryCommitState()

	sc := NewWorkflowStateCollection(ioState, globalState, compState, workflowState, nil, "", "node1")

	// key 为 nil 时返回 compState(nodeID)
	if err := compState.UpdateByID("node1", map[string]any{"node1": map[string]any{"data": "value"}}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	compState.Commit("node1")
	result := sc.Get(StateKey{})
	if result == nil {
		t.Error("期望 key 为 nil 时返回 compState(nodeID) 非空")
	}

	// key 非空时按前缀查找
	result = sc.GetByPrefix(StringKey("data"), "node1")
	if result == nil {
		t.Error("期望 key 非空时按前缀查找返回非空")
	}
}

// TestUpdate_组件状态 测试 Update 方法
func TestUpdate_组件状态(t *testing.T) {
	ioState := NewInMemoryCommitState()
	globalState := NewInMemoryCommitState()
	compState := NewInMemoryCommitState()
	workflowState := NewInMemoryCommitState()

	sc := NewWorkflowStateCollection(ioState, globalState, compState, workflowState, nil, "", "node1")

	// Update 应将 data 包裹在 {nodeID: data} 中
	err := sc.Update(map[string]any{"key1": "value1"})
	if err != nil {
		t.Errorf("Update 返回错误: %v", err)
	}

	// 暂存区应有更新
	updates := compState.GetUpdates()
	if len(updates["node1"]) == 0 {
		t.Error("期望 compState 有 node1 的暂存更新")
	}

	// 提交后可读取
	compState.Commit("node1")
	result := compState.Get(StringKey("node1"))
	nodeData, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 compState[node1] 为 map，实际=%T", result)
	}
	if nodeData["key1"] != "value1" {
		t.Errorf("期望 nodeData[key1]='value1'，实际=%v", nodeData["key1"])
	}
}

// TestDump 测试导出完整快照
func TestDump(t *testing.T) {
	ioState := NewInMemoryCommitState()
	globalState := NewInMemoryCommitState()
	compState := NewInMemoryCommitState()
	workflowState := NewInMemoryCommitState()

	sc := NewWorkflowStateCollection(ioState, globalState, compState, workflowState, nil, "", "node1")

	dump := sc.Dump()

	// 应包含 9 个键
	expectedKeys := []string{IOStateKey, IOStateUpdatesKey, GlobalStateKey, GlobalStateUpdatesKey,
		CompStateKey, CompStateUpdatesKey, WorkflowStateKey, WorkflowStateUpdatesKey, "trace_state"}
	for _, key := range expectedKeys {
		if _, ok := dump[key]; !ok {
			t.Errorf("Dump 缺少键: %s", key)
		}
	}
}

// TestGetState_SetState 测试持久化恢复循环
func TestGetState_SetState(t *testing.T) {
	ioState := NewInMemoryCommitState()
	globalState := NewInMemoryCommitState()
	compState := NewInMemoryCommitState()
	workflowState := NewInMemoryCommitState()

	sc := NewWorkflowStateCollection(ioState, globalState, compState, workflowState, nil, "", "node1")

	// 写入数据
	if err := compState.UpdateByID("node1", map[string]any{"key1": "value1"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	compState.Commit("node1")

	// 导出快照
	snapshot := sc.GetState()

	// 创建新实例并恢复
	ioState2 := NewInMemoryCommitState()
	globalState2 := NewInMemoryCommitState()
	compState2 := NewInMemoryCommitState()
	workflowState2 := NewInMemoryCommitState()
	sc2 := NewWorkflowStateCollection(ioState2, globalState2, compState2, workflowState2, nil, "", "node1")
	sc2.SetState(snapshot)

	// 验证恢复后的 compState
	result := compState2.Get(StringKey("key1"))
	if result != "value1" {
		t.Errorf("期望恢复后获取 'value1'，实际=%v", result)
	}

	// SetState(nil) 不 panic
	sc2.SetState(nil)
}

// TestNil防御 测试 nil 防御
func TestNil防御(t *testing.T) {
	sc := &WorkflowStateCollection{}

	// globalState 为 nil 时不 panic
	result := sc.GetGlobal(StringKey("key"))
	if result != nil {
		t.Errorf("期望 globalState 为 nil 时返回 nil，实际=%v", result)
	}

	// key 为 AllStateKey 时返回 nil（Workflow 层无"获取全部"语义）
	result = sc.GetGlobal(AllStateKey)
	if result != nil {
		t.Errorf("期望 AllStateKey 时返回 nil，实际=%v", result)
	}

	// key 为零值时返回 nil
	result = sc.GetGlobal(StateKey{})
	if result != nil {
		t.Errorf("期望零值 key 时返回 nil，实际=%v", result)
	}

	// UpdateGlobal data 为 nil 时不 panic
	sc.UpdateGlobal(nil)

	// compState 为 nil 时不 panic
	result = sc.Get(StringKey("key"))
	if result != nil {
		t.Errorf("期望 compState 为 nil 时返回 nil，实际=%v", result)
	}

	// Update compState 为 nil 时不 panic
	err := sc.Update(map[string]any{"key": "value"})
	if err != nil {
		t.Errorf("期望 compState 为 nil 时 Update 返回 nil，实际=%v", err)
	}
}

// TestGetByTransformer_组件状态 测试 GetByTransformer 方法
func TestGetByTransformer_组件状态(t *testing.T) {
	ioState := NewInMemoryCommitState()
	globalState := NewInMemoryCommitState()
	compState := NewInMemoryCommitState()
	workflowState := NewInMemoryCommitState()

	sc := NewWorkflowStateCollection(ioState, globalState, compState, workflowState, nil, "", "node1")

	// compState 非 nil 时委托
	if err := compState.UpdateByID("node1", map[string]any{"x": 42}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	compState.Commit("node1")
	result := sc.GetByTransformer(func(r ReadableState) any {
		return r.Get(StringKey("x"))
	})
	if result != 42 {
		t.Errorf("期望 42，实际=%v", result)
	}

	// compState 为 nil 时返回 nil
	sc2 := &WorkflowStateCollection{}
	result = sc2.GetByTransformer(func(r ReadableState) any {
		return r.Get(StringKey("x"))
	})
	if result != nil {
		t.Errorf("期望 nil，实际=%v", result)
	}
}

// TestGet_零值Key_compStateNil 测试 compState 为 nil 时零值 key 返回 nil
func TestGet_零值Key_compStateNil(t *testing.T) {
	scNil := &WorkflowStateCollection{}
	result := scNil.Get(StateKey{})
	if result != nil {
		t.Errorf("期望 nil，实际=%v", result)
	}
}
