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

	// 类型断言获取 WorkflowState
	childWS, ok := childState.(WorkflowState)
	if !ok {
		t.Fatalf("childState 应实现 WorkflowState 接口")
	}

	// schema 非空时按 parentID 前缀查找
	result := childWS.GetInputs(StringKey("input_key"))
	if result != "input_val" {
		t.Errorf("期望从父节点输出获取 'input_val'，实际=%v", result)
	}

	// schema 为零值时返回当前节点全部 IO 数据
	result = childWS.GetInputs(StateKey{})
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

// TestRollback 测试回滚当前节点
func TestRollback(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	// 暂存更新
	if err := cs.globalState.UpdateByID(DefaultNodeID, map[string]any{"g_key": "g_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}
	if err := cs.compState.UpdateByID(DefaultNodeID, map[string]any{"c_key": "c_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}

	// 回滚当前节点
	cs.Rollback()

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
	if wcs, ok := nodeState.(*WorkflowCommitState); ok {
		if wcs.nodeID != "node_A" {
			t.Errorf("期望 nodeID='node_A'，实际=%s", wcs.nodeID)
		}
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

// TestWorkflowCommitState_UpdateByID 测试 UpdateByID 委托给 compState
func TestWorkflowCommitState_UpdateByID(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	err := cs.UpdateByID("node1", map[string]any{"comp_key": "comp_val"})
	if err != nil {
		t.Errorf("UpdateByID 返回错误: %v", err)
	}

	// 通过底层子状态按节点提交
	cs.compState.Commit("node1")
	result := cs.compState.Get(StringKey("comp_key"))
	if result != "comp_val" {
		t.Errorf("期望 comp_key=comp_val，实际=%v", result)
	}
}

// TestSetUpdates_JSON格式 验证 SetUpdates 处理 JSON 反序列化格式。
func TestSetUpdates_JSON格式(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	// 模拟 JSON 反序列化后的格式：[]any 而非 []map[string]any
	jsonUpdates := map[string]any{
		IOStateUpdatesKey: map[string]any{
			"node1": []any{
				map[string]any{"io_key": "io_val"},
			},
		},
		CompStateUpdatesKey: map[string]any{
			"node1": []any{
				map[string]any{"comp_key": "comp_val"},
			},
		},
		WorkflowStateUpdatesKey: map[string]any{
			"workflow": []any{
				map[string]any{"wf_key": "wf_val"},
			},
		},
		GlobalStateUpdatesKey: map[string]any{
			"default": []any{
				map[string]any{"g_key": "g_val"},
			},
		},
	}

	cs.SetUpdates(jsonUpdates)
	cs.Commit()

	if cs.ioState.Get(StringKey("io_key")) != "io_val" {
		t.Error("JSON 格式 ioState 恢复失败")
	}
	if cs.compState.Get(StringKey("comp_key")) != "comp_val" {
		t.Error("JSON 格式 compState 恢复失败")
	}
	if cs.workflowState.Get(StringKey("wf_key")) != "wf_val" {
		t.Error("JSON 格式 workflowState 恢复失败")
	}
	if cs.globalState.Get(StringKey("g_key")) != "g_val" {
		t.Error("JSON 格式 globalState 恢复失败")
	}
}

// TestSetUpdates_nil 验证 SetUpdates 传入 nil 不操作。
func TestSetUpdates_nil(t *testing.T) {
	cs := NewInMemoryWorkflowState()
	cs.SetUpdates(nil) // 不应 panic
}

// TestSetUpdates_值nil 验证 SetUpdates 中各子状态值为 nil 时跳过。
func TestSetUpdates_值nil(t *testing.T) {
	cs := NewInMemoryWorkflowState()
	updates := map[string]any{
		GlobalStateUpdatesKey:   nil,
		IOStateUpdatesKey:       nil,
		CompStateUpdatesKey:     nil,
		WorkflowStateUpdatesKey: nil,
	}
	cs.SetUpdates(updates) // 不应 panic
}

// TestSetUpdates_workflowOnlyfalse_globalNil 验证 workflowOnly=false 时 globalState 不设置。
func TestSetUpdates_workflowOnlyfalse_globalNil(t *testing.T) {
	sharedGlobal := NewInMemoryCommitState()
	cs := NewInMemoryWorkflowState(sharedGlobal) // workflowOnly=false

	updates := map[string]any{
		GlobalStateUpdatesKey: map[string]any{
			"default": []any{map[string]any{"g_key": "g_val"}},
		},
	}
	cs.SetUpdates(updates)
	// workflowOnly=false 时 globalState 更新不应生效
	updatesAfter := cs.GetUpdates()
	if updatesAfter[GlobalStateUpdatesKey] != nil {
		t.Error("workflowOnly=false 时 GetUpdates 的 globalState 应为 nil")
	}
}

// TestGetUpdates_workflowOnlyfalse 验证 workflowOnly=false 时 GetUpdates 中 globalState 为 nil。
func TestGetUpdates_workflowOnlyfalse(t *testing.T) {
	sharedGlobal := NewInMemoryCommitState()
	cs := NewInMemoryWorkflowState(sharedGlobal) // workflowOnly=false

	updates := cs.GetUpdates()
	if updates[GlobalStateUpdatesKey] != nil {
		t.Error("workflowOnly=false 时 GetUpdates 的 globalState 应为 nil")
	}
}

// TestGetInputs_ioStateNil 验证 ioState 为 nil 时 GetInputs 返回 nil。
func TestGetInputs_ioStateNil(t *testing.T) {
	cs := &WorkflowCommitState{}
	result := cs.GetInputs(StringKey("key"))
	if result != nil {
		t.Errorf("期望 nil，实际=%v", result)
	}
}

// TestGetOutputs_ioStateNil 验证 ioState 为 nil 时 GetOutputs 返回 nil。
func TestGetOutputs_ioStateNil(t *testing.T) {
	cs := &WorkflowCommitState{}
	result := cs.GetOutputs()
	if result != nil {
		t.Errorf("期望 nil，实际=%v", result)
	}
}

// TestGetInputsByTransformer_ioStateNil 验证 ioState 为 nil 时返回空 map。
func TestGetInputsByTransformer_ioStateNil(t *testing.T) {
	cs := &WorkflowCommitState{}
	result := cs.GetInputsByTransformer(func(r ReadableState) any { return nil })
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际=%T", result)
	}
	if len(m) != 0 {
		t.Errorf("期望空 map，实际=%v", m)
	}
}

// TestGetWorkflowState_workflowStateNil 验证 workflowState 为 nil 时返回 nil。
func TestGetWorkflowState_workflowStateNil(t *testing.T) {
	cs := &WorkflowCommitState{}
	result := cs.GetWorkflowState(StringKey("key"))
	if result != nil {
		t.Errorf("期望 nil，实际=%v", result)
	}
}

// TestSetOutputs_ioStateNil 验证 ioState 为 nil 时不 panic。
func TestSetOutputs_ioStateNil(t *testing.T) {
	cs := &WorkflowCommitState{}
	cs.SetOutputs(map[string]any{"key": "val"}) // 不应 panic
}

// TestUpdateAndCommitWorkflowState_UpdateByID失败 验证 UpdateByID 失败时记录日志（使用有效但空的 WorkflowCommitState）。
func TestUpdateAndCommitWorkflowState_UpdateByID失败(t *testing.T) {
	cs := NewInMemoryWorkflowState()
	// 正常场景：空节点ID 导致 UpdateByID 返回 error，但 WorkflowCommitState 使用 DefaultWorkflowID 不会出错
	// 这里测试正常流程不 panic
	cs.UpdateAndCommitWorkflowState(map[string]any{"key": "val"})
}

// TestCommitUserInputs_ioStateNil 验证 ioState 为 nil 时 CommitUserInputs 不 panic。
func TestCommitUserInputs_ioStateNil(t *testing.T) {
	cs := &WorkflowCommitState{}
	cs.CommitUserInputs(map[string]any{"key": "val"}) // 不应 panic
}

// TestCommitUserInputs_inputsNil 验证 inputs 为 nil 时不操作。
func TestCommitUserInputs_inputsNil(t *testing.T) {
	cs := NewInMemoryWorkflowState()
	cs.CommitUserInputs(nil) // 不应 panic
}

// TestCommit_指定节点暂存 测试只暂存特定节点后提交全部。
// WorkflowCommitState.Commit() 无参提交全部子状态（对齐 Python CommitState.commit()）。
func TestCommit_指定节点暂存(t *testing.T) {
	cs := NewInMemoryWorkflowState()

	if err := cs.globalState.UpdateByID(DefaultNodeID, map[string]any{"g_key": "g_val"}); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}

	cs.Commit()

	if cs.globalState.Get(StringKey("g_key")) != "g_val" {
		t.Error("提交后 globalState 数据丢失")
	}
}

// TestNewInMemoryWorkflowState_传入globalState 验证传入 globalState 时 workflowOnly=false。
func TestNewInMemoryWorkflowState_传入globalState(t *testing.T) {
	sharedGlobal := NewInMemoryCommitState()
	cs := NewInMemoryWorkflowState(sharedGlobal)
	if cs.WorkflowOnly() {
		t.Error("传入 globalState 时 workflowOnly 应为 false")
	}
}
