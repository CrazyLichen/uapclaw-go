# WorkflowSession 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 5.4 WorkflowSession 完整体系，包括 Workflow State 体系、内部 WorkflowSession、外部 WorkflowSession 门面、NodeSession 和 SubWorkflowSession。

**Architecture:** 对标 Python 两层会话模式：内部层实现 BaseSession 接口（纯组件容器），外部门面组合内部实例提供业务 API。Workflow State 体系组合 4 个 CommitState 区域（io_state/global_state/comp_state/workflow_state），通过 CreateNodeState 共享底层状态切换 nodeID 视角。

**Tech Stack:** Go 1.x，项目内部 session/state 包和 session/internal 包

**设计文档:** `docs/superpowers/specs/2025-08-07-workflow-session-design.md`

---

## Task 1: WorkflowStateCollection — 四区状态集合

**Files:**
- Create: `internal/agentcore/session/state/workflow_state_collection.go`
- Test: `internal/agentcore/session/state/workflow_state_collection_test.go`

- [ ] **Step 1: 编写 WorkflowStateCollection 结构体和构造函数**

创建 `workflow_state_collection.go`：

```go
package state

import "github.com/uapclaw/uapclaw-go/internal/common/logger"

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowStateCollection 工作流场景的状态集合。
//
// 组合 io_state/global_state/comp_state/workflow_state 四个可提交区域，
// 提供三级回退查询（global_state → io_state[parentID] → io_state[nodeID]）。
// 实现 State 接口。
//
// 对应 Python: openjiuwen/core/session/state/workflow_state.py (StateCollection)
type WorkflowStateCollection struct {
	// ioState 输入输出状态
	ioState CommitState
	// globalState 全局状态（从 AgentSession 共享）
	globalState CommitState
	// compState 组件状态
	compState CommitState
	// workflowState 工作流状态
	workflowState CommitState
	// traceState 追踪状态（按 nodeID 存 span）
	traceState map[string]any
	// parentID 父节点 ID
	parentID string
	// nodeID 当前节点 ID
	nodeID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowStateCollection 创建工作流状态集合实例。
func NewWorkflowStateCollection(ioState, globalState, compState, workflowState CommitState, traceState map[string]any, parentID, nodeID string) *WorkflowStateCollection {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_workflow_state_collection").
		Str("parent_id", parentID).
		Str("node_id", nodeID).
		Msg("创建工作流状态集合")
	if traceState == nil {
		traceState = make(map[string]any)
	}
	return &WorkflowStateCollection{
		ioState:       ioState,
		globalState:   globalState,
		compState:     compState,
		workflowState: workflowState,
		traceState:    traceState,
		parentID:      parentID,
		nodeID:        nodeID,
	}
}

// ──────────────────────────── WorkflowStateCollection 方法 ────────────────────────────

// GetGlobal 从全局状态获取值。
// 三级回退查询：globalState → ioState[parentID] → ioState[nodeID]。
func (s *WorkflowStateCollection) GetGlobal(key StateKey) any {
	if s.globalState == nil || key == nil {
		return nil
	}
	result := s.globalState.Get(key)
	if result != nil {
		return result
	}
	result = s.ioState.GetByPrefix(key, s.parentID)
	if result != nil {
		return result
	}
	return s.ioState.GetByPrefix(key, s.nodeID)
}

// UpdateGlobal 更新全局状态，以当前 nodeID 为键暂存更新。
func (s *WorkflowStateCollection) UpdateGlobal(data map[string]any) {
	if s.globalState == nil || data == nil {
		return
	}
	s.globalState.UpdateByID(s.nodeID, data)
}

// UpdateTrace 更新追踪状态。
func (s *WorkflowStateCollection) UpdateTrace(span any) {
	s.traceState[s.nodeID] = span
}

// CommitCmp 提交当前节点的 comp_state 和 io_state。
func (s *WorkflowStateCollection) CommitCmp() {
	s.compState.Commit(s.nodeID)
	s.ioState.Commit(s.nodeID)
}

// Dump 导出完整状态快照。
func (s *WorkflowStateCollection) Dump() map[string]any {
	return map[string]any{
		IOStateKey:             s.ioState.GetState(),
		IOStateUpdatesKey:      s.ioState.GetUpdates(),
		GlobalStateKey:         s.globalState.GetState(),
		GlobalStateUpdatesKey:  s.globalState.GetUpdates(),
		CompStateKey:           s.compState.GetState(),
		CompStateUpdatesKey:    s.compState.GetUpdates(),
		WorkflowStateKey:       s.workflowState.GetState(),
		WorkflowStateUpdatesKey: s.workflowState.GetUpdates(),
		"trace_state":          s.traceState,
	}
}

// ──────────────────────────── State 接口实现 ────────────────────────────

// Get 根据 StateKey 获取组件状态值。
// key 为 nil 时返回当前节点的全部 comp_state；否则按 nodeID 前缀查找。
func (s *WorkflowStateCollection) Get(key StateKey) any {
	if s.compState == nil {
		return nil
	}
	if key == nil {
		return s.compState.Get(StringKey(s.nodeID))
	}
	return s.compState.GetByPrefix(key, s.nodeID)
}

// GetByPrefix 根据 key 和嵌套前缀获取组件状态值。
func (s *WorkflowStateCollection) GetByPrefix(key StateKey, nestedPrefix string) any {
	if s.compState == nil {
		return nil
	}
	return s.compState.GetByPrefix(key, nestedPrefix)
}

// GetByTransformer 通过转换函数获取组件状态值。
func (s *WorkflowStateCollection) GetByTransformer(transformer Transformer) any {
	if s.compState == nil {
		return nil
	}
	return s.compState.GetByTransformer(transformer)
}

// Update 更新组件状态，以当前 nodeID 为键暂存更新。
// data 被包裹在 {nodeID: data} 中。
func (s *WorkflowStateCollection) Update(data map[string]any) error {
	if s.compState == nil {
		return nil
	}
	s.compState.UpdateByID(s.nodeID, map[string]any{s.nodeID: data})
	return nil
}

// GetState 导出状态快照（用于检查点恢复）。
// 注意：此方法仅返回 io/comp/workflow 三个状态，global_state 由 WorkflowCommitState 管理。
func (s *WorkflowStateCollection) GetState() map[string]any {
	return map[string]any{
		IOStateKey:       s.ioState.GetState(),
		CompStateKey:     s.compState.GetState(),
		WorkflowStateKey: s.workflowState.GetState(),
	}
}

// SetState 从快照恢复状态。
func (s *WorkflowStateCollection) SetState(st map[string]any) {
	if st == nil {
		return
	}
	if io, ok := st[IOStateKey]; ok {
		if m, ok := io.(map[string]any); ok {
			s.ioState.SetState(m)
		}
	}
	if comp, ok := st[CompStateKey]; ok {
		if m, ok := comp.(map[string]any); ok {
			s.compState.SetState(m)
		}
	}
	if wf, ok := st[WorkflowStateKey]; ok {
		if m, ok := wf.(map[string]any); ok {
			s.workflowState.SetState(m)
		}
	}
}
```

- [ ] **Step 2: 编写 WorkflowStateCollection 测试**

创建 `workflow_state_collection_test.go`，覆盖以下场景：

1. `TestNewWorkflowStateCollection` — 构造函数，traceState nil 防御
2. `TestGetGlobal_三级回退` — globalState 有值 → ioState[parentID] 回退 → ioState[nodeID] 回退
3. `TestUpdateGlobal` — 以 nodeID 为键暂存到 globalState
4. `TestUpdateTrace` — 按 nodeID 存入 traceState
5. `TestCommitCmp` — 提交当前 nodeID 的 comp + io
6. `TestGet_组件状态` — key 为 nil 返回 compState(nodeID)；key 非空按前缀查找
7. `TestUpdate_组件状态` — data 被包裹在 {nodeID: data} 中
8. `TestDump` — 返回完整快照包含 9 个键
9. `TestGetState_SetState` — 持久化恢复循环
10. `TestNil防御` — globalState/compState 为 nil 时不 panic

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/state/... -run TestWorkflowStateCollection -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/state/workflow_state_collection.go internal/agentcore/session/state/workflow_state_collection_test.go
git commit -m "feat(session): 添加 WorkflowStateCollection 四区状态集合"
```

---

## Task 2: WorkflowCommitState — 增加工作流语义方法

**Files:**
- Create: `internal/agentcore/session/state/workflow_commit_state.go`
- Test: `internal/agentcore/session/state/workflow_commit_state_test.go`

- [ ] **Step 1: 编写 WorkflowCommitState 结构体和方法**

创建 `workflow_commit_state.go`：

```go
package state

import "github.com/uapclaw/uapclaw-go/internal/common/logger"

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowCommitState 工作流可提交状态。
//
// 在 WorkflowStateCollection 基础上增加 commit/rollback/IO 操作和节点状态创建能力。
// 实现 CommitState 接口。
//
// 对应 Python: openjiuwen/core/session/state/workflow_state.py (CommitState)
type WorkflowCommitState struct {
	// WorkflowStateCollection 嵌入基础四区状态
	WorkflowStateCollection
	// workflowOnly 是否仅工作流模式（无共享 globalState 时为 true）
	workflowOnly bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowCommitState 创建工作流可提交状态实例。
func NewWorkflowCommitState(ioState, globalState, compState, workflowState CommitState, traceState map[string]any, parentID, nodeID string, workflowOnly bool) *WorkflowCommitState {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_workflow_commit_state").
		Str("parent_id", parentID).
		Str("node_id", nodeID).
		Bool("workflow_only", workflowOnly).
		Msg("创建工作流可提交状态")
	return &WorkflowCommitState{
		WorkflowStateCollection: *NewWorkflowStateCollection(ioState, globalState, compState, workflowState, traceState, parentID, nodeID),
		workflowOnly:            workflowOnly,
	}
}

// ──────────────────────────── WorkflowCommitState 方法 ────────────────────────────

// GetWorkflowState 从工作流状态获取值。
func (s *WorkflowCommitState) GetWorkflowState(key StateKey) any {
	if s.workflowState == nil || key == nil {
		return nil
	}
	return s.workflowState.Get(key)
}

// UpdateAndCommitWorkflowState 立即更新并提交工作流状态。
func (s *WorkflowCommitState) UpdateAndCommitWorkflowState(data map[string]any) {
	s.workflowState.UpdateByID(DefaultWorkflowID, data)
	s.workflowState.Commit()
}

// SetOutputs 向 io_state 写入当前节点的输出。
// data 被包裹在 {nodeID: data} 中。
func (s *WorkflowCommitState) SetOutputs(data map[string]any) {
	if s.ioState == nil || data == nil {
		return
	}
	s.ioState.UpdateByID(s.nodeID, map[string]any{s.nodeID: data})
}

// GetInputs 从 io_state 查询父节点的输出（即当前节点输入）。
// schema 为 nil 时返回当前节点全部 IO 数据；否则按 parentID 前缀查找。
func (s *WorkflowCommitState) GetInputs(schema StateKey) any {
	if s.ioState == nil {
		return nil
	}
	if schema == nil {
		return s.ioState.Get(StringKey(s.nodeID))
	}
	return s.ioState.GetByPrefix(schema, s.parentID)
}

// GetOutputs 从 io_state 查询指定节点的输出。
// nodeID 为空时使用当前 nodeID。
func (s *WorkflowCommitState) GetOutputs(nodeID ...string) any {
	if s.ioState == nil {
		return nil
	}
	effectiveNodeID := s.nodeID
	if len(nodeID) > 0 && nodeID[0] != "" {
		effectiveNodeID = nodeID[0]
	}
	return s.ioState.GetByPrefix(StringKey(effectiveNodeID), s.parentID)
}

// GetInputsByTransformer 通过转换函数获取输入。
func (s *WorkflowCommitState) GetInputsByTransformer(transformer Transformer) any {
	if s.ioState == nil {
		return map[string]any{}
	}
	return s.ioState.GetByTransformer(transformer)
}

// CommitUserInputs 同时写入 io_state 和 global_state 并立即提交。
// 默认节点的 io_state data 不包裹在 {nodeID: data} 中。
func (s *WorkflowCommitState) CommitUserInputs(inputs map[string]any) {
	if s.ioState == nil || inputs == nil {
		return
	}
	if s.nodeID != DefaultNodeID {
		s.ioState.UpdateByID(s.nodeID, map[string]any{s.nodeID: inputs})
	} else {
		s.ioState.UpdateByID(s.nodeID, inputs)
	}
	s.globalState.UpdateByID(s.nodeID, inputs)
	s.Commit()
}

// Commit 提交全部四个子状态。
func (s *WorkflowCommitState) Commit() {
	s.ioState.Commit()
	s.compState.Commit()
	s.globalState.Commit()
	s.workflowState.Commit()
}

// Rollback 回滚全部四个子状态的当前节点更新。
func (s *WorkflowCommitState) Rollback() {
	s.compState.Rollback(s.nodeID)
	s.ioState.Rollback(s.nodeID)
	s.globalState.Rollback(s.nodeID)
	s.workflowState.Rollback(s.nodeID)
}

// CreateNodeState 创建节点专属状态视图。
// 共享底层四个子状态对象，切换 nodeID/parentID。
func (s *WorkflowCommitState) CreateNodeState(nodeID, parentID string) *WorkflowCommitState {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "create_node_state").
		Str("node_id", nodeID).
		Str("parent_id", parentID).
		Msg("创建节点专属状态视图")
	return NewWorkflowCommitState(
		s.ioState,
		s.globalState,
		s.compState,
		s.workflowState,
		s.traceState,
		parentID,
		nodeID,
		true, // create_node_state 总是 workflowOnly=true
	)
}

// ──────────────────────────── CommitState 接口实现 ────────────────────────────

// UpdateByID 委托给 compState。
func (s *WorkflowCommitState) UpdateByID(nodeID string, data map[string]any) {
	s.compState.UpdateByID(nodeID, data)
}

// CommitByID 提交指定节点的暂存更新（CommitState 接口）。
// 注意：此方法与 WorkflowCommitState.Commit() 不同，后者提交全部四个子状态。
func (s *WorkflowCommitState) CommitCommitState(nodeID ...string) {
	s.ioState.Commit(nodeID...)
	s.compState.Commit(nodeID...)
	s.globalState.Commit(nodeID...)
	s.workflowState.Commit(nodeID...)
}

// RollbackNode 回滚指定节点的暂存更新（CommitState 接口）。
func (s *WorkflowCommitState) RollbackNode(nodeID string) {
	s.compState.Rollback(nodeID)
	s.ioState.Rollback(nodeID)
	s.globalState.Rollback(nodeID)
	s.workflowState.Rollback(nodeID)
}

// GetUpdates 获取所有暂存更新。
// workflowOnly 控制是否包含 global_state_updates。
func (s *WorkflowCommitState) GetUpdates() map[string][]map[string]any {
	result := map[string][]map[string]any{
		IOStateUpdatesKey:       deepCopyUpdates(s.ioState.GetUpdates()),
		CompStateUpdatesKey:     deepCopyUpdates(s.compState.GetUpdates()),
		WorkflowStateUpdatesKey: deepCopyUpdates(s.workflowState.GetUpdates()),
	}
	if s.workflowOnly {
		result[GlobalStateUpdatesKey] = deepCopyUpdates(s.globalState.GetUpdates())
	} else {
		result[GlobalStateUpdatesKey] = nil
	}
	return result
}

// SetUpdates 设置暂存更新。
func (s *WorkflowCommitState) SetUpdates(updates map[string][]map[string]any) {
	if updates == nil {
		return
	}
	if gs, ok := updates[GlobalStateUpdatesKey]; ok && gs != nil && s.workflowOnly {
		s.globalState.SetUpdates(gs)
	}
	if io, ok := updates[IOStateUpdatesKey]; ok && io != nil {
		s.ioState.SetUpdates(io)
	}
	if comp, ok := updates[CompStateUpdatesKey]; ok && comp != nil {
		s.compState.SetUpdates(comp)
	}
	if wf, ok := updates[WorkflowStateUpdatesKey]; ok && wf != nil {
		s.workflowState.SetUpdates(wf)
	}
}

// WorkflowOnly 返回是否仅工作流模式。
func (s *WorkflowCommitState) WorkflowOnly() bool {
	return s.workflowOnly
}

// ──────────────────────────── RecoverableState 覆写 ────────────────────────────

// GetState 导出状态快照（覆写 WorkflowStateCollection.GetState）。
// workflowOnly 控制是否包含 global_state。
func (s *WorkflowCommitState) GetState() map[string]any {
	result := map[string]any{
		IOStateKey:       s.ioState.GetState(),
		CompStateKey:     s.compState.GetState(),
		WorkflowStateKey: s.workflowState.GetState(),
	}
	if s.workflowOnly {
		result[GlobalStateKey] = s.globalState.GetState()
	} else {
		result[GlobalStateKey] = nil
	}
	return result
}

// SetState 从快照恢复状态（覆写 WorkflowStateCollection.SetState）。
func (s *WorkflowCommitState) SetState(st map[string]any) {
	if st == nil {
		return
	}
	if gs, ok := st[GlobalStateKey]; ok && gs != nil {
		if m, ok := gs.(map[string]any); ok {
			s.globalState.SetState(m)
		}
	}
	if io, ok := st[IOStateKey]; ok && io != nil {
		if m, ok := io.(map[string]any); ok {
			s.ioState.SetState(m)
		}
	}
	if comp, ok := st[CompStateKey]; ok && comp != nil {
		if m, ok := comp.(map[string]any); ok {
			s.compState.SetState(m)
		}
	}
	if wf, ok := st[WorkflowStateKey]; ok && wf != nil {
		if m, ok := wf.(map[string]any); ok {
			s.workflowState.SetState(m)
		}
	}
}
```

同时在 `state/utils.go` 末尾添加辅助函数：

```go
// deepCopyUpdates 深拷贝暂存更新数据
func deepCopyUpdates(updates map[string][]map[string]any) map[string][]map[string]any {
	if updates == nil {
		return nil
	}
	result := make(map[string][]map[string]any, len(updates))
	for key, list := range updates {
		copied := make([]map[string]any, len(list))
		for i, u := range list {
			copied[i] = deepCopyMap(u)
		}
		result[key] = copied
	}
	return result
}
```

**注意：** WorkflowCommitState 同时需要实现 `CommitState` 接口。由于 Go 接口的方法签名冲突（CommitState.Commit(nodeID...string) 和 WorkflowCommitState.Commit() 无参），需要用以下方式解决：

WorkflowCommitState **不直接实现 CommitState 接口**（因为 `Commit()` 签名冲突），而是作为 `State` 接口的实现提供工作流语义方法。如果需要 CommitState 语义，通过 `CommitCommitState(nodeID...string)` 和 `RollbackNode(nodeID string)` 方法提供。这符合 Python 的设计：Python 中 CommitState 也没有实现 CommitStateLike 接口（它继承自 StateCollection 继承自 State）。

- [ ] **Step 2: 编写 WorkflowCommitState 测试**

创建 `workflow_commit_state_test.go`，覆盖：

1. `TestNewWorkflowCommitState` — 构造函数
2. `TestGetWorkflowState` — 查询 workflowState
3. `TestUpdateAndCommitWorkflowState` — 立即更新并提交
4. `TestSetOutputs` — data 被包裹在 {nodeID: data} 中
5. `TestGetInputs` — schema 为 nil / 非空两种情况
6. `TestGetOutputs` — 指定 nodeID / 使用当前 nodeID
7. `TestGetInputsByTransformer` — 通过 transformer 获取
8. `TestCommitUserInputs_默认节点` — nodeID == DefaultNodeID 时 io data 不包裹
9. `TestCommitUserInputs_非默认节点` — nodeID != DefaultNodeID 时 io data 包裹
10. `TestCommit_全量提交` — 四个子状态全部提交
11. `TestRollback` — 四个子状态全部回滚
12. `TestCreateNodeState` — 共享底层状态，切换 nodeID
13. `TestGetState_workflowOnly_true` — 包含 globalState
14. `TestGetState_workflowOnly_false` — globalState 为 nil
15. `TestSetState` — 从快照恢复
16. `TestGetUpdates_SetUpdates` — 暂存区读写

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/state/... -run TestWorkflowCommitState -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/state/workflow_commit_state.go internal/agentcore/session/state/workflow_commit_state_test.go internal/agentcore/session/state/utils.go
git commit -m "feat(session): 添加 WorkflowCommitState 工作流可提交状态"
```

---

## Task 3: InMemoryWorkflowState — 便捷构造器

**Files:**
- Create: `internal/agentcore/session/state/workflow_inmemory_state.go`
- Test: `internal/agentcore/session/state/workflow_inmemory_state_test.go`

- [ ] **Step 1: 编写 InMemoryWorkflowState 构造函数**

创建 `workflow_inmemory_state.go`：

```go
package state

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryWorkflowState 创建基于内存的工作流状态实例。
//
// globalState 可从外部传入（与 AgentSession 共享），未提供则创建新的 InMemoryCommitState。
// workflowOnly 取决于 globalState 是否为 nil：
//   - 传入 globalState → workflowOnly=false（global_state 由外部管理）
//   - 未传 globalState → workflowOnly=true（所有状态独立）
//
// 对应 Python: openjiuwen/core/session/state/workflow_state.py (InMemoryState)
func NewInMemoryWorkflowState(globalState ...CommitState) *WorkflowCommitState {
	var gs CommitState
	if len(globalState) > 0 && globalState[0] != nil {
		gs = globalState[0]
	} else {
		gs = NewInMemoryCommitState()
	}
	workflowOnly := len(globalState) == 0 || globalState[0] == nil
	return NewWorkflowCommitState(
		NewInMemoryCommitState(), // ioState
		gs,                       // globalState
		NewInMemoryCommitState(), // compState
		NewInMemoryCommitState(), // workflowState
		make(map[string]any),     // traceState
		"",                       // parentID
		DefaultNodeID,            // nodeID
		workflowOnly,
	)
}
```

- [ ] **Step 2: 编写 InMemoryWorkflowState 测试**

创建 `workflow_inmemory_state_test.go`，覆盖：

1. `TestNewInMemoryWorkflowState_无globalState` — workflowOnly=true，所有状态独立
2. `TestNewInMemoryWorkflowState_有globalState` — workflowOnly=false，共享 globalState
3. `TestNewInMemoryWorkflowState_共享验证` — AgentSession 和 WorkflowSession 的 globalState 共享同一个底层实例
4. `TestNewInMemoryWorkflowState_默认值` — parentID=""，nodeID="default"

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/state/... -run TestNewInMemoryWorkflowState -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/state/workflow_inmemory_state.go internal/agentcore/session/state/workflow_inmemory_state_test.go
git commit -m "feat(session): 添加 InMemoryWorkflowState 便捷构造器"
```

---

## Task 4: 内部 WorkflowSession + NodeSession + SubWorkflowSession

**Files:**
- Create: `internal/agentcore/session/internal/workflow_session.go`
- Test: `internal/agentcore/session/internal/workflow_session_test.go`

- [ ] **Step 1: 编写 WorkflowSession / NodeSession / SubWorkflowSession 结构体和方法**

创建 `workflow_session.go`：

```go
package internal

import (
	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowSession 工作流级内部会话，实现 BaseSession 接口。
//
// 持有工作流运行所需的基础设施组件，支持延迟注入 StreamWriterManager 和 ActorManager。
// Checkpointer 委托给 parent（通常是 AgentSession），确保父子会话共享持久化机制。
//
// 对应 Python: openjiuwen/core/session/internal/workflow.py (WorkflowSession)
type WorkflowSession struct {
	// sessionID 会话唯一标识（从 parent 继承或自动生成）
	sessionID string
	// parent 父会话（通常是 AgentSession）
	parent session.BaseSession
	// config 会话配置
	// ⤵️ 5.12 回填：any → SessionConfig
	config any
	// tracer 追踪器
	// ⤵️ 5.11 回填：any → Tracer
	tracer any
	// state 状态对象（WorkflowCommitState）
	state state.State
	// streamWriterManager 流写入管理器
	// ⤵️ 5.10 回填：any → StreamWriterManager
	streamWriterManager any
	// actorManager Actor 管理器
	// ⤵️ 后续回填：any → ActorManager
	actorManager any
	// workflowID 工作流 ID
	workflowID string
}

// NodeSession 工作流节点级会话，实现 BaseSession 接口。
//
// 包装一个 BaseSession（通常是 WorkflowSession），通过 CreateNodeState 创建节点专属的状态视图。
// 大部分方法委托给被包装的 session，但 State() 返回节点专属视图，Close() 为空实现。
//
// 对应 Python: openjiuwen/core/session/internal/workflow.py (NodeSession)
type NodeSession struct {
	// session 被包装的会话（通常是 WorkflowSession）
	delegate session.BaseSession
	// executableID 全局唯一可执行路径 ID（parentID + "." + nodeID）
	executableID string
	// nodeID 节点 ID
	nodeID string
	// nodeType 节点类型
	nodeType string
	// parentID 父节点 executable_id
	parentID string
	// state 节点专属状态视图
	nodeState state.State
	// workflowID 从父 session 继承的工作流 ID
	workflowID string
	// workflowNestingDepth 从父 session 继承的工作流嵌套深度
	workflowNestingDepth int
	// mainWorkflowID 从父 session 继承的主工作流 ID
	mainWorkflowID string
	// skipTrace 是否跳过追踪
	skipTrace bool
}

// SubWorkflowSession 子工作流会话，嵌入 NodeSession。
//
// 在 NodeSession 基础上增加自己的 ActorManager 和嵌套深度管理。
// Close() 时关闭自己的 ActorManager。
//
// 对应 Python: openjiuwen/core/session/internal/workflow.py (SubWorkflowSession)
type SubWorkflowSession struct {
	// NodeSession 嵌入节点会话
	NodeSession
	// actorManager 子工作流专属 Actor 管理器
	// ⤵️ 后续回填：any → ActorManager
	actorManager any
	// workflowNestingDepth 工作流嵌套深度（覆盖 NodeSession 的值）
	workflowNestingDepth2 int
	// workflowID 子工作流 ID（覆盖 NodeSession 的值）
	workflowID2 string
	// mainWorkflowID 主工作流 ID（覆盖 NodeSession 的值）
	mainWorkflowID2 string
}

// ──────────────────────────── 枚举 ────────────────────────────

// WorkflowSessionOption WorkflowSession 构造选项函数类型
type WorkflowSessionOption func(*WorkflowSession)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowSession 创建内部 WorkflowSession 实例。
//
// 默认行为：
//   - 有 parent 时：sessionID 继承 parent、config 继承 parent、tracer 继承 parent
//   - 无 parent 时：sessionID 自动生成 UUID、config 新建、tracer 为 nil
//   - state 默认创建 InMemoryWorkflowState（workflowOnly=true）
//   - streamWriterManager 和 actorManager 初始为 nil，需外部注入
func NewWorkflowSession(opts ...WorkflowSessionOption) *WorkflowSession {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_workflow_session").
		Msg("创建内部 WorkflowSession")

	s := &WorkflowSession{
		state: state.NewInMemoryWorkflowState(),
	}
	for _, opt := range opts {
		opt(s)
	}

	// 处理默认值：无 parent 时自建
	if s.parent == nil {
		if s.sessionID == "" {
			s.sessionID = uuid.New().String()
		}
	} else {
		if s.sessionID == "" {
			s.sessionID = s.parent.SessionID()
		}
		if s.config == nil {
			s.config = s.parent.Config()
		}
		if s.tracer == nil {
			s.tracer = s.parent.Tracer()
		}
	}

	return s
}

// WithWorkflowParent 设置父会话的选项
func WithWorkflowParent(parent session.BaseSession) WorkflowSessionOption {
	return func(s *WorkflowSession) {
		s.parent = parent
	}
}

// WithWorkflowSessionID 设置会话 ID 的选项
func WithWorkflowSessionID(id string) WorkflowSessionOption {
	return func(s *WorkflowSession) {
		s.sessionID = id
	}
}

// WithWorkflowState 设置状态的选项
func WithWorkflowState(st state.State) WorkflowSessionOption {
	return func(s *WorkflowSession) {
		s.state = st
	}
}

// WithWorkflowID 设置工作流 ID 的选项
func WithWorkflowID(id string) WorkflowSessionOption {
	return func(s *WorkflowSession) {
		s.workflowID = id
	}
}

// NewNodeSession 创建节点级会话实例。
//
// 从 parent session 的 state 创建节点专属状态视图（共享底层状态，切换 nodeID/parentID）。
// executableID = parentID + "." + nodeID（parentID 为空时退化为 nodeID）。
func NewNodeSession(parent session.BaseSession, nodeID, nodeType string, skipTrace bool) *NodeSession {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_node_session").
		Str("node_id", nodeID).
		Str("node_type", nodeType).
		Msg("创建节点级会话")

	parentID := createParentID(parent)
	executableID := createExecutableID(nodeID, parentID)

	// 从 parent 的 state 创建节点专属状态视图
	var nodeState state.State
	if cs, ok := parent.State().(*state.WorkflowCommitState); ok {
		nodeState = cs.CreateNodeState(executableID, parentID)
	} else {
		// 降级：无法创建节点视图时直接使用 parent 的 state
		nodeState = parent.State()
	}

	return &NodeSession{
		delegate:             parent,
		executableID:         executableID,
		nodeID:               nodeID,
		nodeType:             nodeType,
		parentID:             parentID,
		nodeState:            nodeState,
		workflowID:           getWorkflowID(parent),
		workflowNestingDepth: getWorkflowNestingDepth(parent),
		mainWorkflowID:       getMainWorkflowID(parent),
		skipTrace:            skipTrace,
	}
}

// NewSubWorkflowSession 创建子工作流会话实例。
//
// 嵌套深度 = 传入 NodeSession 的深度 + 1。
// 构造时以传入 NodeSession 的 parent() 作为父级 session，
// 使用原 NodeSession 的 nodeID 和 nodeType。
func NewSubWorkflowSession(nodeSession *NodeSession, workflowID string, actorManager any) *SubWorkflowSession {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_sub_workflow_session").
		Str("workflow_id", workflowID).
		Msg("创建子工作流会话")

	// 使用传入 NodeSession 的 parent() 作为父级
	parentSession := nodeSession.Parent()
	parentID := createParentID(parentSession)
	executableID := createExecutableID(nodeSession.NodeID(), parentID)

	// 从 parent 的 state 创建节点专属状态视图
	var nodeState state.State
	if cs, ok := parentSession.State().(*state.WorkflowCommitState); ok {
		nodeState = cs.CreateNodeState(executableID, parentID)
	} else {
		nodeState = parentSession.State()
	}

	return &SubWorkflowSession{
		NodeSession: NodeSession{
			delegate:             parentSession,
			executableID:         executableID,
			nodeID:               nodeSession.NodeID(),
			nodeType:             nodeSession.NodeType(),
			parentID:             parentID,
			nodeState:            nodeState,
			workflowID:           workflowID,
			workflowNestingDepth: nodeSession.WorkflowNestingDepth(),
			mainWorkflowID:       nodeSession.MainWorkflowID(),
			skipTrace:            false, // SubWorkflowSession 不传递 skipTrace
		},
		actorManager:         actorManager,
		workflowNestingDepth2: nodeSession.WorkflowNestingDepth() + 1,
		workflowID2:          workflowID,
		mainWorkflowID2:      nodeSession.MainWorkflowID(),
	}
}

// ──────────────────────────── WorkflowSession 方法 ────────────────────────────

// Config 获取会话配置
func (s *WorkflowSession) Config() any {
	return s.config
}

// State 获取会话状态
func (s *WorkflowSession) State() state.State {
	return s.state
}

// Tracer 获取追踪器
func (s *WorkflowSession) Tracer() any {
	return s.tracer
}

// StreamWriterManager 获取流写入管理器
func (s *WorkflowSession) StreamWriterManager() any {
	return s.streamWriterManager
}

// SessionID 获取会话唯一标识
func (s *WorkflowSession) SessionID() string {
	return s.sessionID
}

// Checkpointer 获取检查点管理器。
// 有 parent 则委托给 parent；无 parent 则从工厂获取（懒加载）。
func (s *WorkflowSession) Checkpointer() any {
	if s.parent != nil {
		return s.parent.Checkpointer()
	}
	// ⤵️ 5.8 回填：CheckpointerFactory 实现后从工厂获取
	return nil
}

// ActorManager 获取 Actor 管理器
func (s *WorkflowSession) ActorManager() any {
	return s.actorManager
}

// Close 关闭会话。如果 actorManager 不为 nil，调用其 Shutdown。
func (s *WorkflowSession) Close() error {
	if s.actorManager != nil {
		// ⤵️ 后续回填：actorManager 类型从 any → ActorManager 后调用 Shutdown()
		// 当前 actorManager 为 any，无法直接调用方法
	}
	return nil
}

// SetStreamWriterManager 幂等注入流写入管理器。已设置则不覆盖。
func (s *WorkflowSession) SetStreamWriterManager(mgr any) {
	if s.streamWriterManager == nil {
		s.streamWriterManager = mgr
	}
}

// SetTracer 设置追踪器（无幂等保护，与 Python 一致）。
func (s *WorkflowSession) SetTracer(tracer any) {
	s.tracer = tracer
}

// SetActorManager 幂等注入 Actor 管理器。已设置则不覆盖。
func (s *WorkflowSession) SetActorManager(mgr any) {
	if s.actorManager == nil {
		s.actorManager = mgr
	}
}

// SetWorkflowID 设置工作流 ID
func (s *WorkflowSession) SetWorkflowID(id string) {
	s.workflowID = id
}

// WorkflowID 返回工作流 ID
func (s *WorkflowSession) WorkflowID() string {
	return s.workflowID
}

// MainWorkflowID 返回主工作流 ID（直接返回 WorkflowID）
func (s *WorkflowSession) MainWorkflowID() string {
	return s.workflowID
}

// WorkflowNestingDepth 返回工作流嵌套深度（固定返回 0）
func (s *WorkflowSession) WorkflowNestingDepth() int {
	return 0
}

// Parent 返回父会话
func (s *WorkflowSession) Parent() session.BaseSession {
	return s.parent
}

// ──────────────────────────── NodeSession 方法 ────────────────────────────

// NodeID 返回节点 ID
func (n *NodeSession) NodeID() string {
	return n.nodeID
}

// NodeType 返回节点类型
func (n *NodeSession) NodeType() string {
	return n.nodeType
}

// ExecutableID 返回全局唯一可执行路径 ID
func (n *NodeSession) ExecutableID() string {
	return n.executableID
}

// ParentID 返回父节点 executable_id
func (n *NodeSession) ParentID() string {
	return n.parentID
}

// WorkflowID 返回工作流 ID
func (n *NodeSession) WorkflowID() string {
	return n.workflowID
}

// MainWorkflowID 返回主工作流 ID
func (n *NodeSession) MainWorkflowID() string {
	return n.mainWorkflowID
}

// WorkflowNestingDepth 返回工作流嵌套深度
func (n *NodeSession) WorkflowNestingDepth() int {
	return n.workflowNestingDepth
}

// SkipTrace 返回是否跳过追踪
func (n *NodeSession) SkipTrace() bool {
	return n.skipTrace
}

// Parent 返回父 session 引用
func (n *NodeSession) Parent() session.BaseSession {
	return n.delegate
}

// NodeConfig 获取节点级配置。
// ⤵️ 5.12 回填：Config 返回真实类型后实现 get_workflow_config
func (n *NodeSession) NodeConfig() any {
	return nil
}

// ──────────────────────────── NodeSession BaseSession 接口实现 ────────────────────────────

// Config 委托给父 session
func (n *NodeSession) Config() any {
	return n.delegate.Config()
}

// State 返回节点专属状态视图
func (n *NodeSession) State() state.State {
	return n.nodeState
}

// Tracer 委托给父 session
func (n *NodeSession) Tracer() any {
	return n.delegate.Tracer()
}

// StreamWriterManager 委托给父 session
func (n *NodeSession) StreamWriterManager() any {
	return n.delegate.StreamWriterManager()
}

// SessionID 委托给父 session
func (n *NodeSession) SessionID() string {
	return n.delegate.SessionID()
}

// Checkpointer 委托给父 session
func (n *NodeSession) Checkpointer() any {
	return n.delegate.Checkpointer()
}

// ActorManager 委托给父 session
func (n *NodeSession) ActorManager() any {
	return n.delegate.ActorManager()
}

// Close 空实现，节点不拥有生命周期
func (n *NodeSession) Close() error {
	return nil
}

// ──────────────────────────── SubWorkflowSession 方法 ────────────────────────────

// WorkflowID 返回子工作流 ID（覆写 NodeSession）
func (s *SubWorkflowSession) WorkflowID() string {
	return s.workflowID2
}

// MainWorkflowID 返回主工作流 ID（覆写 NodeSession）
func (s *SubWorkflowSession) MainWorkflowID() string {
	return s.mainWorkflowID2
}

// WorkflowNestingDepth 返回工作流嵌套深度（覆写 NodeSession）
func (s *SubWorkflowSession) WorkflowNestingDepth() int {
	return s.workflowNestingDepth2
}

// ActorManager 返回自己的 ActorManager（覆写 NodeSession 的委托）
func (s *SubWorkflowSession) ActorManager() any {
	return s.actorManager
}

// SetActorManager 幂等注入 Actor 管理器。已设置则不覆盖。
func (s *SubWorkflowSession) SetActorManager(mgr any) {
	if s.actorManager == nil {
		s.actorManager = mgr
	}
}

// Close 关闭子工作流会话。如果 actorManager 不为 nil，调用其 Shutdown。
func (s *SubWorkflowSession) Close() error {
	if s.actorManager != nil {
		// ⤵️ 后续回填：actorManager 类型从 any → ActorManager 后调用 Shutdown()
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createParentID 计算父节点 ID。
// 如果 session 是 NodeSession，返回其 executable_id；否则返回空字符串。
func createParentID(s session.BaseSession) string {
	if ns, ok := s.(*NodeSession); ok {
		return ns.ExecutableID()
	}
	return ""
}

// createExecutableID 计算全局唯一可执行路径 ID。
// parentID 非空时返回 parentID.nodeID；否则返回 nodeID。
func createExecutableID(nodeID, parentID string) string {
	if parentID != "" {
		return parentID + "." + nodeID
	}
	return nodeID
}

// getWorkflowID 从 BaseSession 获取 WorkflowID。
// 如果 session 有 WorkflowID 方法则调用，否则返回空字符串。
func getWorkflowID(s session.BaseSession) string {
	if ws, ok := s.(interface{ WorkflowID() string }); ok {
		return ws.WorkflowID()
	}
	return ""
}

// getWorkflowNestingDepth 从 BaseSession 获取嵌套深度。
func getWorkflowNestingDepth(s session.BaseSession) int {
	if ws, ok := s.(interface{ WorkflowNestingDepth() int }); ok {
		return ws.WorkflowNestingDepth()
	}
	return 0
}

// getMainWorkflowID 从 BaseSession 获取主工作流 ID。
func getMainWorkflowID(s session.BaseSession) string {
	if ws, ok := s.(interface{ MainWorkflowID() string }); ok {
		return ws.MainWorkflowID()
	}
	return ""
}
```

- [ ] **Step 2: 编写 WorkflowSession 测试**

创建 `workflow_session_test.go`，覆盖：

**WorkflowSession 部分：**
1. `TestNewWorkflowSession_有parent` — 继承 sessionID/config/tracer
2. `TestNewWorkflowSession_无parent` — 自动生成 UUID，config 为 nil
3. `TestWorkflowSession_BaseSession接口` — 8 个方法全部验证
4. `TestWorkflowSession_SetStreamWriterManager_幂等` — 已设置不覆盖
5. `TestWorkflowSession_SetTracer_非幂等` — 已设置也覆盖
6. `TestWorkflowSession_SetActorManager_幂等` — 已设置不覆盖
7. `TestWorkflowSession_Checkpointer_委托parent` — 有 parent 委托
8. `TestWorkflowSession_Checkpointer_无parent` — 无 parent 返回 nil
9. `TestWorkflowSession_WorkflowNestingDepth` — 固定返回 0
10. `TestWorkflowSession_Close` — actorManager 为 nil 时不 panic

**NodeSession 部分：**
11. `TestNewNodeSession` — executableID 计算正确
12. `TestNewNodeSession_嵌套路径` — 从 NodeSession 创建时 parentID 取 executableID
13. `TestNodeSession_BaseSession接口` — 委托方法验证
14. `TestNodeSession_State_节点专属视图` — State() 返回 CreateNodeState 的结果
15. `TestNodeSession_Close_空实现` — 不影响底层 session

**SubWorkflowSession 部分：**
16. `TestNewSubWorkflowSession` — 嵌套深度 +1
17. `TestSubWorkflowSession_ActorManager` — 返回自己的 actorManager
18. `TestSubWorkflowSession_SetActorManager_幂等` — 已设置不覆盖
19. `TestSubWorkflowSession_WorkflowID` — 返回子工作流 ID
20. `TestSubWorkflowSession_WorkflowNestingDepth` — 父深度 + 1

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/internal/... -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/internal/workflow_session.go internal/agentcore/session/internal/workflow_session_test.go
git commit -m "feat(session): 添加内部 WorkflowSession/NodeSession/SubWorkflowSession"
```

---

## Task 5: 外部 WorkflowSession 门面

**Files:**
- Create: `internal/agentcore/session/workflow.go`
- Test: `internal/agentcore/session/workflow_test.go`

- [ ] **Step 1: 编写外部 WorkflowSession 门面**

创建 `workflow.go`：

```go
package session

import (
	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowSession 工作流会话门面，提供用户面向的 API。
//
// 组合内部层 WorkflowSession，实现状态读写、环境变量管理、工作流卡片等业务功能。
// 生命周期比 Agent Session 更简单：仅 Close（无 PreRun/PostRun）。
//
// 对应 Python: openjiuwen/core/session/workflow.py (Session)
type WorkflowSession struct {
	// inner 内部 WorkflowSession 实例
	inner *internal.WorkflowSession
	// envs 环境变量（从 parent.config 获取）
	envs map[string]any
	// workflowCard 工作流卡片
	// ⤵️ 后续回填
	workflowCard any
}

// ──────────────────────────── 枚举 ────────────────────────────

// WorkflowSessionOption WorkflowSession 构造选项函数类型
type WorkflowSessionOption func(*WorkflowSession)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowSession 创建公开层 WorkflowSession 实例。
//
// 对应 Python: openjiuwen/core/session/workflow.py create_workflow_session()
func NewWorkflowSession(opts ...WorkflowSessionOption) *WorkflowSession {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_workflow_session_facade").
		Msg("创建公开层 WorkflowSession")

	ws := &WorkflowSession{
		envs: make(map[string]any),
	}
	for _, opt := range opts {
		opt(ws)
	}
	return ws
}

// WithWorkflowSessionParent 设置父会话的选项
func WithWorkflowSessionParent(parent BaseSession) WorkflowSessionOption {
	return func(ws *WorkflowSession) {
		var envs map[string]any
		// ⤵️ 5.12 回填：Config 返回真实类型后从 config 获取 envs
		_ = parent
		if envs == nil {
			envs = make(map[string]any)
		}
		ws.envs = envs
	}
}

// WithWorkflowSessionSessionID 设置会话 ID 的选项
func WithWorkflowSessionSessionID(id string) WorkflowSessionOption {
	return func(ws *WorkflowSession) {
		if ws.inner == nil {
			ws.inner = internal.NewWorkflowSession(
				internal.WithWorkflowSessionID(id),
			)
		}
	}
}

// WithWorkflowSessionInner 设置内部 WorkflowSession 的选项
func WithWorkflowSessionInner(inner *internal.WorkflowSession) WorkflowSessionOption {
	return func(ws *WorkflowSession) {
		ws.inner = inner
	}
}

// ──────────────────────────── WorkflowSession 方法 ────────────────────────────

// GetSessionID 返回会话唯一标识
func (ws *WorkflowSession) GetSessionID() string {
	if ws.inner == nil {
		return ""
	}
	return ws.inner.SessionID()
}

// GetEnvs 返回环境变量
func (ws *WorkflowSession) GetEnvs() map[string]any {
	return ws.envs
}

// GetParent 返回父会话
func (ws *WorkflowSession) GetParent() BaseSession {
	if ws.inner == nil {
		return nil
	}
	return ws.inner.Parent()
}

// SetWorkflowCard 设置工作流卡片
func (ws *WorkflowSession) SetWorkflowCard(card any) {
	ws.workflowCard = card
}

// GetWorkflowCard 返回工作流卡片
func (ws *WorkflowSession) GetWorkflowCard() any {
	return ws.workflowCard
}

// State 返回会话状态
func (ws *WorkflowSession) State() state.State {
	if ws.inner == nil {
		return nil
	}
	return ws.inner.State()
}

// UpdateState 更新全局状态，委托到 inner.State() 的 WorkflowCommitState
func (ws *WorkflowSession) UpdateState(data map[string]any) {
	if ws.inner == nil {
		return
	}
	if cs, ok := ws.inner.State().(*state.WorkflowCommitState); ok {
		cs.UpdateGlobal(data)
	}
}

// GetState 获取全局状态值，委托到 inner.State() 的 WorkflowCommitState
func (ws *WorkflowSession) GetState(key string) any {
	if ws.inner == nil {
		return nil
	}
	if cs, ok := ws.inner.State().(*state.WorkflowCommitState); ok {
		return cs.GetGlobal(state.StringKey(key))
	}
	return nil
}

// DumpState 导出完整状态快照，委托到 inner.State() 的 WorkflowCommitState
func (ws *WorkflowSession) DumpState() map[string]any {
	if ws.inner == nil {
		return nil
	}
	if cs, ok := ws.inner.State().(*state.WorkflowCommitState); ok {
		return cs.Dump()
	}
	return nil
}

// Close 关闭会话，委托 inner.Close()
func (ws *WorkflowSession) Close() error {
	if ws.inner == nil {
		return nil
	}
	return ws.inner.Close()
}

// Inner 返回内部 WorkflowSession 实例（用于高级场景）
func (ws *WorkflowSession) Inner() *internal.WorkflowSession {
	return ws.inner
}
```

- [ ] **Step 2: 编写 WorkflowSession 门面测试**

创建 `workflow_test.go`，覆盖：

1. `TestNewWorkflowSession` — 基本构造
2. `TestWorkflowSession_GetSessionID` — 返回 inner.SessionID()
3. `TestWorkflowSession_GetEnvs` — 返回 envs
4. `TestWorkflowSession_GetParent` — 返回 inner.Parent()
5. `TestWorkflowSession_SetGetWorkflowCard` — 设置和获取卡片
6. `TestWorkflowSession_UpdateState_GetState` — 通过 WorkflowCommitState 读写状态
7. `TestWorkflowSession_DumpState` — 导出完整快照
8. `TestWorkflowSession_Close` — 委托 inner.Close()
9. `TestWorkflowSession_Inner为nil时防御` — inner 为 nil 时各方法不 panic

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/... -run TestWorkflowSession -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/workflow.go internal/agentcore/session/workflow_test.go
git commit -m "feat(session): 添加外部 WorkflowSession 门面"
```

---

## Task 6: 回填 AgentSession.CreateWorkflowSession()

**Files:**
- Modify: `internal/agentcore/session/agent.go:264-268`
- Test: `internal/agentcore/session/agent_test.go`（添加 CreateWorkflowSession 测试）

- [ ] **Step 1: 修改 agent.go 中的 CreateWorkflowSession**

将 `agent.go` 第 264-268 行：

```go
// CreateWorkflowSession 创建子 WorkflowSession。
// ⤵️ 5.5 回填：WorkflowSession 实现后填充真实逻辑
func (s *Session) CreateWorkflowSession() any {
	return nil
}
```

替换为：

```go
// CreateWorkflowSession 创建子 WorkflowSession。
//
// 从 AgentSession 的 AgentStateCollection 获取 globalState，
// 包装为 WorkflowCommitState 与 AgentSession 共享全局状态。
// WorkflowSession 的 globalState 更新 commit 后 AgentSession 也能读到。
//
// 对应 Python: Session.create_workflow_session()
func (s *Session) CreateWorkflowSession() *WorkflowSession {
	// 取出 AgentStateCollection 的 globalState（*InMemoryState 实例）
	var workflowState *state.WorkflowCommitState
	if coll, ok := s.inner.State().(*state.AgentStateCollection); ok {
		// 用 globalState 包装为 InMemoryCommitState，与 AgentSession 共享同一个底层实例
		sharedGlobalState := state.NewInMemoryCommitState(coll.GlobalState())
		workflowState = state.NewInMemoryWorkflowState(sharedGlobalState)
	} else {
		workflowState = state.NewInMemoryWorkflowState()
	}

	inner := internal.NewWorkflowSession(
		internal.WithWorkflowParent(s.inner),
		internal.WithWorkflowSessionID(s.inner.SessionID()),
		internal.WithWorkflowState(workflowState),
	)

	return NewWorkflowSession(
		WithWorkflowSessionInner(inner),
		WithWorkflowSessionParent(s.inner),
		WithWorkflowSessionSessionID(s.inner.SessionID()),
	)
}
```

- [ ] **Step 2: 编写 CreateWorkflowSession 测试**

在 `agent_test.go` 中添加：

1. `TestSession_CreateWorkflowSession` — 创建成功返回非 nil
2. `TestSession_CreateWorkflowSession_SessionID共享` — WorkflowSession 与 AgentSession 共享 sessionID
3. `TestSession_CreateWorkflowSession_GlobalState共享` — WorkflowSession 更新 globalState 并 commit 后，AgentSession 也能读到

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/... -run TestSession_CreateWorkflowSession -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/agent.go internal/agentcore/session/agent_test.go
git commit -m "feat(session): 回填 AgentSession.CreateWorkflowSession() 真实逻辑"
```

---

## Task 7: 更新 doc.go 文件

**Files:**
- Modify: `internal/agentcore/session/state/doc.go`
- Modify: `internal/agentcore/session/internal/doc.go`
- Modify: `internal/agentcore/session/doc.go`

- [ ] **Step 1: 更新 state/doc.go**

在文件目录树中添加新增的 3 个文件：

```
├── workflow_state_collection.go  # WorkflowStateCollection 四区状态集合
├── workflow_commit_state.go      # WorkflowCommitState 增加 commit/rollback/IO 语义
├── workflow_inmemory_state.go    # InMemoryWorkflowState 便捷构造器
```

- [ ] **Step 2: 更新 internal/doc.go**

在文件目录树中添加新增文件：

```
├── workflow_session.go           # WorkflowSession/NodeSession/SubWorkflowSession
```

- [ ] **Step 3: 更新 session/doc.go**

在文件目录树中添加新增文件：

```
├── workflow.go                   # WorkflowSession 外部门面
```

- [ ] **Step 4: 运行全量测试确认无回归**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/... -v`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/session/state/doc.go internal/agentcore/session/internal/doc.go internal/agentcore/session/doc.go
git commit -m "docs(session): 更新 doc.go 文件目录树"
```

---

## Task 8: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新实现计划状态**

将 5.4 相关步骤的状态从 `☐` 改为 `✅`：

- 5.4 WorkflowSession（从 AgentSession 创建）→ ✅
- 5.1 中 Workflow State 相关的回填标记确认已完成

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 5.4 WorkflowSession 实现状态为已完成"
```
