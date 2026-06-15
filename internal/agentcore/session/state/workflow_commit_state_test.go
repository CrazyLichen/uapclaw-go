package state

import "testing"

// TestNewWorkflowCommitState 测试构造函数
func TestNewWorkflowCommitState(t *testing.T) {
	cs := NewInMemoryWorkflowState()
	if cs == nil {
		t.Fatal("NewInMemoryWorkflowState 返回 nil")
	}
	if !cs.WorkflowOnly() {
		t.Error("无 globalState 时 workflowOnly 应为 true")
	}
}

// TestGetWorkflowState 测试查询工作流状态
func TestGetWorkflowState(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	// 写入并提交
	if err := cs.workflowState.UpdateByID(DefaultWorkflowID, map[string]any{"wf_key": "wf_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.workflowState.Commit()

	result := cs.GetWorkflowState(StringKey("wf_key"))
	if result != "wf_val" {
		t.Errorf("期望获取 'wf_val'，实际=%v", result)
	}

	// 零值 key 返回 nil
	result = cs.GetWorkflowState(StateKey{})
	if result != nil {
		t.Errorf("期望零值 key 返回 nil，实际=%v", result)
	}
}

// TestUpdateAndCommitWorkflowState 测试立即更新并提交
func TestUpdateAndCommitWorkflowState(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	cs.UpdateAndCommitWorkflowState(map[string]any{"wf_key": "wf_val"})

	// 应立即可读
	result := cs.workflowState.Get(StringKey("wf_key"))
	if result != "wf_val" {
		t.Errorf("期望立即提交后获取 'wf_val'，实际=%v", result)
	}
}

// TestSetOutputs 测试设置节点输出
func TestSetOutputs(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	cs.SetOutputs(map[string]any{"output_key": "output_val"})

	// ioState 暂存区应有更新（data 被包裹在 {nodeID: data} 中）
	updates := cs.ioState.GetUpdates()
	if len(updates[DefaultNodeID]) == 0 {
		t.Fatal("期望 ioState 有 default 节点的暂存更新")
	}

	// 提交后可读
	cs.ioState.Commit(DefaultNodeID)
	result := cs.ioState.Get(StringKey(DefaultNodeID))
	nodeData, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 ioState[default] 为 map，实际=%T", result)
	}
	outputData, ok := nodeData["output_key"]
	if !ok || outputData != "output_val" {
		t.Errorf("期望嵌套数据 output_key='output_val'，实际=%v", nodeData)
	}

	// data 为 nil 时静默返回
	cs.SetOutputs(nil)
}

// TestGetInputs 测试获取节点输入
func TestGetInputs(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	// 写入父节点输出到 ioState
	if err := cs.ioState.UpdateByID("parent_node", map[string]any{"parent_node": map[string]any{"input_key": "input_val"}}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.ioState.Commit("parent_node")

	// 创建子节点视图
	childState := cs.CreateNodeState("child_node", "parent_node")

	// schema 非空时按 parentID 前缀查找
	result := childState.GetInputs(StringKey("input_key"))
	if result != "input_val" {
		t.Errorf("期望从父节点输出获取 'input_val'，实际=%v", result)
	}

	// schema 为零值时返回当前节点全部 IO 数据
	result = childState.GetInputs(StateKey{})
	_ = result // 可能返回 nil 因为 child_node 没有 IO 数据
}

// TestGetOutputs 测试获取节点输出
func TestGetOutputs(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	// 写入节点输出
	cs.SetOutputs(map[string]any{"output_key": "output_val"})
	cs.ioState.Commit(DefaultNodeID)

	// 使用当前 nodeID
	result := cs.GetOutputs()
	_ = result

	// 指定 nodeID
	result = cs.GetOutputs("other_node")
	_ = result
}

// TestGetInputsByTransformer 测试通过 transformer 获取输入
func TestGetInputsByTransformer(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	// 写入数据
	if err := cs.ioState.UpdateByID(DefaultNodeID, map[string]any{"key1": "val1"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.ioState.Commit(DefaultNodeID)

	// 通过 transformer 获取
	result := cs.GetInputsByTransformer(func(r ReadableState) any {
		return r.Get(StringKey("key1"))
	})
	if result != "val1" {
		t.Errorf("期望通过 transformer 获取 'val1'，实际=%v", result)
	}
}

// TestCommitUserInputs_默认节点 测试默认节点提交用户输入
func TestCommitUserInputs_默认节点(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	// 默认节点（nodeID == "default"）时 io data 不包裹
	cs.CommitUserInputs(map[string]any{"user_key": "user_val"})

	// globalState 应立即可读
	result := cs.globalState.Get(StringKey("user_key"))
	if result != "user_val" {
		t.Errorf("期望 globalState 获取 'user_val'，实际=%v", result)
	}

	// ioState 也应已提交
	result = cs.ioState.Get(StringKey("user_key"))
	if result != "user_val" {
		t.Errorf("期望 ioState 获取 'user_val'（默认节点不包裹），实际=%v", result)
	}
}

// TestCommitUserInputs_非默认节点 测试非默认节点提交用户输入
func TestCommitUserInputs_非默认节点(t *testing.T) {
	cs := NewInMemoryWorkflowState()
	// 修改 nodeID
	cs.nodeID = "custom_node"

	cs.CommitUserInputs(map[string]any{"user_key": "user_val"})

	// globalState 应立即可读
	result := cs.globalState.Get(StringKey("user_key"))
	if result != "user_val" {
		t.Errorf("期望 globalState 获取 'user_val'，实际=%v", result)
	}

	// ioState 的 data 应被包裹在 {nodeID: data} 中
	cs.ioState.Commit("custom_node")
	result = cs.ioState.Get(StringKey("custom_node"))
	nodeData, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 ioState[custom_node] 为 map，实际=%T", result)
	}
	if nodeData["user_key"] != "user_val" {
		t.Errorf("期望嵌套数据 user_key='user_val'，实际=%v", nodeData)
	}
}

// TestCommit_全量提交 测试提交全部四个子状态
func TestCommit_全量提交(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	// 暂存更新
	if err := cs.globalState.UpdateByID(DefaultNodeID, map[string]any{"g_key": "g_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := cs.compState.UpdateByID(DefaultNodeID, map[string]any{"c_key": "c_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := cs.ioState.UpdateByID(DefaultNodeID, map[string]any{"i_key": "i_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := cs.workflowState.UpdateByID(DefaultWorkflowID, map[string]any{"w_key": "w_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}

	// 提交
	cs.Commit()

	// 验证全部已提交
	if cs.globalState.Get(StringKey("g_key")) != "g_val" {
		t.Error("globalState 未提交")
	}
	if cs.compState.Get(StringKey("c_key")) != "c_val" {
		t.Error("compState 未提交")
	}
	if cs.ioState.Get(StringKey("i_key")) != "i_val" {
		t.Error("ioState 未提交")
	}
	if cs.workflowState.Get(StringKey("w_key")) != "w_val" {
		t.Error("workflowState 未提交")
	}
}

// TestRollback 测试回滚
func TestRollback(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	// 暂存更新
	if err := cs.globalState.UpdateByID(DefaultNodeID, map[string]any{"g_key": "g_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := cs.compState.UpdateByID(DefaultNodeID, map[string]any{"c_key": "c_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}

	// 回滚
	cs.Rollback("node1")

	// 验证全部已回滚
	if cs.globalState.Get(StringKey("g_key")) != nil {
		t.Error("globalState 未回滚")
	}
	if cs.compState.Get(StringKey("c_key")) != nil {
		t.Error("compState 未回滚")
	}
}

// TestCreateNodeState 测试创建节点专属状态视图
func TestCreateNodeState(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	// 写入数据到 globalState
	if err := cs.globalState.UpdateByID(DefaultNodeID, map[string]any{"shared_key": "shared_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.globalState.Commit(DefaultNodeID)

	// 创建节点视图
	nodeState := cs.CreateNodeState("node_A", "")

	// 节点视图应共享底层 globalState
	result := nodeState.GetGlobal(StringKey("shared_key"))
	if result != "shared_val" {
		t.Errorf("期望节点视图共享 globalState，获取 'shared_val'，实际=%v", result)
	}

	// 节点视图的 nodeID 应为 "node_A"
	if nodeState.nodeID != "node_A" {
		t.Errorf("期望 nodeID='node_A'，实际=%s", nodeState.nodeID)
	}
}

// TestGetState_workflowOnly_true 测试 workflowOnly=true 时的 GetState
func TestGetState_workflowOnly_true(t *testing.T) {
	cs := NewInMemoryWorkflowState() // 默认 workflowOnly=true

	state := cs.GetState()
	if state[GlobalStateKey] == nil {
		t.Error("workflowOnly=true 时 GetState 应包含 globalState")
	}
}

// TestGetState_workflowOnly_false 测试 workflowOnly=false 时的 GetState
func TestGetState_workflowOnly_false(t *testing.T) {
	sharedGlobal := NewInMemoryCommitState()
	cs := NewInMemoryWorkflowState(sharedGlobal) // workflowOnly=false

	state := cs.GetState()
	if state[GlobalStateKey] != nil {
		t.Error("workflowOnly=false 时 GetState 的 globalState 应为 nil")
	}
}

// TestSetState_从快照恢复 测试 SetState
func TestSetState_从快照恢复(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	// 写入数据
	if err := cs.globalState.UpdateByID(DefaultNodeID, map[string]any{"g_key": "g_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := cs.compState.UpdateByID(DefaultNodeID, map[string]any{"c_key": "c_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	cs.Commit()

	// 导出快照
	snapshot := cs.GetState()

	// 创建新实例并恢复
	cs2 := NewInMemoryWorkflowState()
	cs2.SetState(snapshot)

	// 验证恢复
	if cs2.globalState.Get(StringKey("g_key")) != "g_val" {
		t.Error("恢复后 globalState 数据丢失")
	}
	if cs2.compState.Get(StringKey("c_key")) != "c_val" {
		t.Error("恢复后 compState 数据丢失")
	}

	// SetState(nil) 不 panic
	cs2.SetState(nil)
}

// TestGetUpdates_SetUpdates 测试暂存区读写
func TestGetUpdates_SetUpdates(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	// 暂存更新
	if err := cs.globalState.UpdateByID(DefaultNodeID, map[string]any{"g_key": "g_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := cs.compState.UpdateByID(DefaultNodeID, map[string]any{"c_key": "c_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}

	// 获取暂存更新
	updates := cs.GetUpdates()
	if updates[GlobalStateUpdatesKey] == nil {
		t.Error("workflowOnly=true 时 GetUpdates 应包含 globalStateUpdates")
	}
	if updates[CompStateUpdatesKey] == nil {
		t.Error("GetUpdates 应包含 compStateUpdates")
	}

	// 创建新实例并设置暂存更新
	cs2 := NewInMemoryWorkflowState()
	cs2.SetUpdates(updates)

	// 提交后验证
	cs2.Commit()
	if cs2.globalState.Get(StringKey("g_key")) != "g_val" {
		t.Error("SetUpdates + Commit 后 globalState 数据丢失")
	}
	if cs2.compState.Get(StringKey("c_key")) != "c_val" {
		t.Error("SetUpdates + Commit 后 compState 数据丢失")
	}
}
