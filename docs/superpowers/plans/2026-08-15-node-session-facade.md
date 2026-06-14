# NodeSessionFacade 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 5.5 SessionNode 会话节点门面，并同步修改已有 Session/WorkflowSession 的 GetState 参数类型

**Architecture:** 新建 `session/node.go` 文件，实现 NodeSessionFacade 结构体（包装 internal.NodeSession），提供 18 个方法（6 身份 + 5 状态 + 2 追踪桩 + 1 交互桩 + 2 流写入桩 + 2 环境配置桩）。同时修改 agent.go 的 GetState 参数从 `string` 改为 `state.StateKey`，删除 workflow.go 中 Python 没有的 4 个状态方法。

**Tech Stack:** Go 1.x, 标准库 testing

---

## File Structure

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/agentcore/session/node.go` | NodeSessionFacade 结构体及全部方法 |
| 新建 | `internal/agentcore/session/node_test.go` | NodeSessionFacade 全量测试 |
| 修改 | `internal/agentcore/session/agent.go` | GetState 参数 `string → state.StateKey`，返回值增加 error |
| 修改 | `internal/agentcore/session/agent_test.go` | GetState 调用适配 StateKey |
| 修改 | `internal/agentcore/session/workflow.go` | 删除 State/UpdateState/GetState/DumpState 4 个方法 |
| 修改 | `internal/agentcore/session/workflow_test.go` | 删除对应测试用例，更新 Inner为nil时防御 测试 |
| 修改 | `internal/agentcore/session/doc.go` | 文件目录添加 node.go 条目 |

---

### Task 1: 修改 agent.go — GetState 参数类型

**Files:**
- Modify: `internal/agentcore/session/agent.go:147-152`
- Modify: `internal/agentcore/session/agent_test.go:114-123,254-280`

- [ ] **Step 1: 修改 agent.go 的 GetState 签名**

将 `GetState(key string) any` 改为 `GetState(key state.StateKey) (any, error)`，委托到 `coll.GetGlobal(key)`，返回 `(result, nil)`。类型断言失败时返回 `(nil, nil)`。

```go
// GetState 获取全局状态值，委托到 inner.State() 的 AgentStateCollection
func (s *Session) GetState(key state.StateKey) (any, error) {
	if coll, ok := s.inner.State().(*state.AgentStateCollection); ok {
		return coll.GetGlobal(key), nil
	}
	return nil, nil
}
```

- [ ] **Step 2: 修改 agent_test.go 的 GetState 调用**

`TestSession_GetState` 中将 `s.GetState("key")` 改为 `s.GetState(state.StringKey("key"))`。

`TestSession_CreateWorkflowSession_GlobalState共享` 中将 `s.GetState("wf_key")` 改为 `s.GetState(state.StringKey("wf_key"))`，`ws.GetState("agent_key")` 改为 `ws.GetState(state.StringKey("agent_key"))`。注意 ws.GetState 将在 Task 2 中被删除，此处先改后删。

```go
// TestSession_GetState 中：
result, err := s.GetState(state.StringKey("key"))
if err != nil {
    t.Errorf("GetState 不应返回错误：%v", err)
}
if result != "value" {
    t.Errorf("期望 value，实际 %v", result)
}
```

```go
// TestSession_CreateWorkflowSession_GlobalState共享 中：
// AgentSession 读取 WorkflowSession 的更新
result, err := s.GetState(state.StringKey("wf_key"))
if err != nil {
    t.Errorf("GetState 不应返回错误：%v", err)
}
if result != "wf_val" {
    t.Errorf("期望 AgentSession 读取共享 globalState='wf_val'，实际=%v", result)
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/ -v -run "TestSession_GetState|TestSession_CreateWorkflowSession_GlobalState共享" -count=1`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/session/agent.go internal/agentcore/session/agent_test.go
git commit -m "refactor: Session.GetState 参数类型从 string 改为 state.StateKey"
```

---

### Task 2: 删除 WorkflowSession 门面中 Python 没有的状态方法

**Files:**
- Modify: `internal/agentcore/session/workflow.go:116-161`
- Modify: `internal/agentcore/session/workflow_test.go:60-125`
- Modify: `internal/agentcore/session/agent_test.go:254-280`

- [ ] **Step 1: 删除 workflow.go 的 4 个方法**

删除以下方法（约第 116-161 行）：
- `State() state.State`
- `UpdateState(data map[string]any)`
- `GetState(key state.StateKey)`
- `DumpState() map[string]any`

保留：`GetSessionID`, `GetEnvs`, `GetParent`, `SetWorkflowCard`, `GetWorkflowCard`, `Close`, `Inner`

- [ ] **Step 2: 删除 workflow_test.go 中的对应测试**

删除以下测试函数：
- `TestWorkflowSessionFacade_UpdateState_GetState`
- `TestWorkflowSessionFacade_DumpState`

修改 `TestWorkflowSessionFacade_Inner为nil时防御`：移除 `ws.State()`, `ws.UpdateState(...)`, `ws.GetState(...)`, `ws.DumpState()` 的检查行。

- [ ] **Step 3: 修改 agent_test.go 的 GlobalState共享 测试**

`TestSession_CreateWorkflowSession_GlobalState共享` 依赖 `ws.GetState()` 和 `ws.UpdateState()`，这两个方法已被删除。需要重写测试：通过 `ws.Inner().State()` 直接操作内部层验证共享状态。

```go
func TestSession_CreateWorkflowSession_GlobalState共享(t *testing.T) {
	s := NewSession()

	// AgentSession 写入全局状态
	s.UpdateState(map[string]any{"agent_key": "agent_val"})

	// 创建 WorkflowSession
	ws := s.CreateWorkflowSession()

	// 通过内部层验证 WorkflowSession 能读取 AgentSession 写入的 globalState
	if cs, ok := ws.Inner().State().(*state.WorkflowCommitState); ok {
		result := cs.GetGlobal(state.StringKey("agent_key"))
		if result != "agent_val" {
			t.Errorf("期望 WorkflowSession 读取共享 globalState='agent_val'，实际=%v", result)
		}

		// WorkflowSession 更新 globalState 并提交
		cs.UpdateGlobal(map[string]any{"wf_key": "wf_val"})
		cs.Commit()
	}

	// AgentSession 也应能读到 WorkflowSession 的更新
	result, err := s.GetState(state.StringKey("wf_key"))
	if err != nil {
		t.Errorf("GetState 不应返回错误：%v", err)
	}
	if result != "wf_val" {
		t.Errorf("期望 AgentSession 读取共享 globalState='wf_val'，实际=%v", result)
	}
}
```

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/ -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/session/workflow.go internal/agentcore/session/workflow_test.go internal/agentcore/session/agent_test.go
git commit -m "refactor: 删除 WorkflowSession 门面中 Python 没有的状态方法(State/UpdateState/GetState/DumpState)"
```

---

### Task 3: 新建 node.go — NodeSessionFacade 结构体与身份方法

**Files:**
- Create: `internal/agentcore/session/node.go`
- Create: `internal/agentcore/session/node_test.go`

- [ ] **Step 1: 写 node.go 的结构体、构造函数和 6 个身份方法**

```go
package session

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// NodeSessionFacade 工作流节点会话门面，提供组件开发者面向的 API。
//
// 包装内部层 NodeSession，为工作流组件（ComponentExecutable）提供
// 身份查询、状态读写、追踪、交互、流写入、环境变量等能力。
//
// 对应 Python: openjiuwen/core/session/node.py (Session)
type NodeSessionFacade struct {
	// inner 内部节点会话
	inner *internal.NodeSession
	// streamMode 流式模式标记
	// on_stream/on_collect/on_transform 时为 true
	// 流式模式下 Interact() 返回错误，因为 GraphInterrupt 无法在 async generator 中恢复
	streamMode bool
	// interaction 交互实例（懒初始化）
	// ⤵️ 5.7 回填：any → WorkflowInteraction
	interaction any
	// description 组件描述，格式: [wf_id=xxx,comp_id=xxx]
	description string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewNodeSessionFacade 创建工作流节点会话门面实例。
//
// streamMode 为 true 时，Interact() 将返回错误（流式模式不支持交互）。
// 对应 Python: Session(session, stream_mode)
func NewNodeSessionFacade(inner *internal.NodeSession, streamMode bool) *NodeSessionFacade {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_node_session_facade").
		Str("node_id", inner.NodeID()).
		Bool("stream_mode", streamMode).
		Msg("创建节点会话门面")

	desc := fmt.Sprintf("[wf_id=%s,comp_id=%s]", inner.WorkflowID(), inner.NodeID())
	return &NodeSessionFacade{
		inner:       inner,
		streamMode:  streamMode,
		description: desc,
	}
}

// ──────────────────────────── 身份方法 ────────────────────────────

// GetWorkflowID 返回工作流 ID
// 对应 Python: Session.get_workflow_id()
func (f *NodeSessionFacade) GetWorkflowID() string {
	return f.inner.WorkflowID()
}

// GetComponentID 返回组件 ID（节点 ID）
// 对应 Python: Session.get_component_id()
func (f *NodeSessionFacade) GetComponentID() string {
	return f.inner.NodeID()
}

// GetComponentType 返回组件类型
// 对应 Python: Session.get_component_type()
func (f *NodeSessionFacade) GetComponentType() string {
	return f.inner.NodeType()
}

// GetComponentDescription 返回组件描述
// 对应 Python: Session.get_component_descrip()
func (f *NodeSessionFacade) GetComponentDescription() string {
	return f.description
}

// GetExecutableID 返回全局唯一可执行路径 ID
// 对应 Python: Session.get_executable_id()
func (f *NodeSessionFacade) GetExecutableID() string {
	return f.inner.ExecutableID()
}

// GetSessionID 返回会话唯一标识
// 对应 Python: Session.get_session_id()
func (f *NodeSessionFacade) GetSessionID() string {
	return f.inner.SessionID()
}
```

- [ ] **Step 2: 写 node_test.go 的身份方法测试**

```go
package session

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
)

// TestNewNodeSessionFacade 测试构造函数
func TestNewNodeSessionFacade(t *testing.T) {
	ws := internal.NewWorkflowSession(internal.WithWorkflowID("wf-1"))
	ns := internal.NewNodeSession(ws, "llm_node", "LLM", false)
	facade := NewNodeSessionFacade(ns, false)

	if facade == nil {
		t.Fatal("NewNodeSessionFacade 返回 nil")
	}
}

// TestNodeSessionFacade_流式模式 测试 streamMode 字段
func TestNodeSessionFacade_流式模式(t *testing.T) {
	ws := internal.NewWorkflowSession(internal.WithWorkflowID("wf-1"))
	ns := internal.NewNodeSession(ws, "node1", "Test", false)

	f1 := NewNodeSessionFacade(ns, false)
	if f1.streamMode {
		t.Error("streamMode=false 时不应为 true")
	}

	f2 := NewNodeSessionFacade(ns, true)
	if !f2.streamMode {
		t.Error("streamMode=true 时不应为 false")
	}
}

// TestNodeSessionFacade_GetWorkflowID 测试返回工作流 ID
func TestNodeSessionFacade_GetWorkflowID(t *testing.T) {
	ws := internal.NewWorkflowSession(internal.WithWorkflowID("wf-1"))
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	if facade.GetWorkflowID() != "wf-1" {
		t.Errorf("期望 GetWorkflowID='wf-1'，实际=%s", facade.GetWorkflowID())
	}
}

// TestNodeSessionFacade_GetComponentID 测试返回节点 ID
func TestNodeSessionFacade_GetComponentID(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "llm_node", "LLM", false)
	facade := NewNodeSessionFacade(ns, false)

	if facade.GetComponentID() != "llm_node" {
		t.Errorf("期望 GetComponentID='llm_node'，实际=%s", facade.GetComponentID())
	}
}

// TestNodeSessionFacade_GetComponentType 测试返回节点类型
func TestNodeSessionFacade_GetComponentType(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "LLM", false)
	facade := NewNodeSessionFacade(ns, false)

	if facade.GetComponentType() != "LLM" {
		t.Errorf("期望 GetComponentType='LLM'，实际=%s", facade.GetComponentType())
	}
}

// TestNodeSessionFacade_GetComponentDescription 测试返回描述
func TestNodeSessionFacade_GetComponentDescription(t *testing.T) {
	ws := internal.NewWorkflowSession(internal.WithWorkflowID("wf-1"))
	ns := internal.NewNodeSession(ws, "llm_node", "LLM", false)
	facade := NewNodeSessionFacade(ns, false)

	expected := "[wf_id=wf-1,comp_id=llm_node]"
	if facade.GetComponentDescription() != expected {
		t.Errorf("期望 GetComponentDescription='%s'，实际=%s", expected, facade.GetComponentDescription())
	}
}

// TestNodeSessionFacade_GetExecutableID 测试返回可执行路径 ID
func TestNodeSessionFacade_GetExecutableID(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "start", "Start", false)
	ns2 := internal.NewNodeSession(ns, "llm_node", "LLM", false)
	facade := NewNodeSessionFacade(ns2, false)

	if facade.GetExecutableID() != "start.llm_node" {
		t.Errorf("期望 GetExecutableID='start.llm_node'，实际=%s", facade.GetExecutableID())
	}
}

// TestNodeSessionFacade_GetSessionID 测试返回会话 ID
func TestNodeSessionFacade_GetSessionID(t *testing.T) {
	parent := internal.NewAgentSession("sess-123")
	ns := internal.NewNodeSession(parent, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	if facade.GetSessionID() != "sess-123" {
		t.Errorf("期望 GetSessionID='sess-123'，实际=%s", facade.GetSessionID())
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/ -v -run "TestNodeSessionFacade" -count=1`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/session/node.go internal/agentcore/session/node_test.go
git commit -m "feat: 新建 NodeSessionFacade 结构体与 6 个身份方法"
```

---

### Task 4: 添加 NodeSessionFacade 的 5 个状态方法

**Files:**
- Modify: `internal/agentcore/session/node.go`
- Modify: `internal/agentcore/session/node_test.go`

- [ ] **Step 1: 在 node.go 的身份方法后添加状态方法**

```go
// ──────────────────────────── 状态方法 ────────────────────────────

// UpdateState 更新组件状态，委托到 inner.State() 的 WorkflowCommitState。
// 对应 Python: Session.update_state(data)
func (f *NodeSessionFacade) UpdateState(data map[string]any) {
	if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok {
		cs.Update(data)
	}
}

// GetState 获取组件状态值，委托到 inner.State() 的 WorkflowCommitState。
// 对应 Python: Session.get_state(key)
func (f *NodeSessionFacade) GetState(key state.StateKey) (any, error) {
	if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok {
		return cs.Get(key), nil
	}
	return nil, nil
}

// UpdateGlobalState 更新全局状态，委托到 inner.State() 的 WorkflowCommitState。
// 对应 Python: Session.update_global_state(data)
func (f *NodeSessionFacade) UpdateGlobalState(data map[string]any) {
	if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok {
		cs.UpdateGlobal(data)
	}
}

// GetGlobalState 获取全局状态值，委托到 inner.State() 的 WorkflowCommitState。
// 对应 Python: Session.get_global_state(key)
func (f *NodeSessionFacade) GetGlobalState(key state.StateKey) (any, error) {
	if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok {
		return cs.GetGlobal(key), nil
	}
	return nil, nil
}

// DumpState 导出完整状态快照，委托到 inner.State() 的 WorkflowCommitState。
// 对应 Python: Session.dump_state()
func (f *NodeSessionFacade) DumpState() map[string]any {
	if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok {
		return cs.Dump()
	}
	return nil
}
```

- [ ] **Step 2: 在 node_test.go 添加状态方法测试**

```go
// TestNodeSessionFacade_UpdateState_GetState 测试组件状态读写
func TestNodeSessionFacade_UpdateState_GetState(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	// 更新组件状态
	facade.UpdateState(map[string]any{"comp_key": "comp_val"})

	// 提交后可读
	if cs, ok := ns.State().(*state.WorkflowCommitState); ok {
		cs.Commit()
	}

	result, err := facade.GetState(state.StringKey("comp_key"))
	if err != nil {
		t.Errorf("GetState 不应返回错误：%v", err)
	}
	if result != "comp_val" {
		t.Errorf("期望 GetState='comp_val'，实际=%v", result)
	}
}

// TestNodeSessionFacade_UpdateGlobalState_GetGlobalState 测试全局状态读写
func TestNodeSessionFacade_UpdateGlobalState_GetGlobalState(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	// 更新全局状态
	facade.UpdateGlobalState(map[string]any{"global_key": "global_val"})

	// 提交后可读
	if cs, ok := ns.State().(*state.WorkflowCommitState); ok {
		cs.Commit()
	}

	result, err := facade.GetGlobalState(state.StringKey("global_key"))
	if err != nil {
		t.Errorf("GetGlobalState 不应返回错误：%v", err)
	}
	if result != "global_val" {
		t.Errorf("期望 GetGlobalState='global_val'，实际=%v", result)
	}
}

// TestNodeSessionFacade_DumpState 测试导出完整快照
func TestNodeSessionFacade_DumpState(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	dump := facade.DumpState()
	if dump == nil {
		t.Error("DumpState 不应返回 nil")
	}
}

// TestNodeSessionFacade_GetState_SchemaKey 测试 SchemaKey 访问
func TestNodeSessionFacade_GetState_SchemaKey(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	facade.UpdateState(map[string]any{"name": "test", "value": 42})
	if cs, ok := ns.State().(*state.WorkflowCommitState); ok {
		cs.Commit()
	}

	result, err := facade.GetState(state.SchemaKey(map[string]any{"name": nil}))
	if err != nil {
		t.Errorf("GetState(SchemaKey) 不应返回错误：%v", err)
	}
	if result == nil {
		t.Error("GetState(SchemaKey) 不应返回 nil")
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/ -v -run "TestNodeSessionFacade_UpdateState|TestNodeSessionFacade_GetState|TestNodeSessionFacade_DumpState|TestNodeSessionFacade_GetGlobalState" -count=1`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/session/node.go internal/agentcore/session/node_test.go
git commit -m "feat: NodeSessionFacade 添加 5 个状态方法(UpdateState/GetState/UpdateGlobalState/GetGlobalState/DumpState)"
```

---

### Task 5: 添加 NodeSessionFacade 的桩方法（追踪/交互/流写入/环境配置）

**Files:**
- Modify: `internal/agentcore/session/node.go`
- Modify: `internal/agentcore/session/node_test.go`

- [ ] **Step 1: 在 node.go 的状态方法后添加 7 个桩方法**

```go
// ──────────────────────────── 追踪方法（桩实现） ────────────────────────────

// Trace 记录组件追踪数据。
// ⤵️ 5.11 回填：TracerWorkflowUtils 实现后填充真实逻辑
// 对应 Python: Session.trace(data)
func (f *NodeSessionFacade) Trace(ctx context.Context, data map[string]any) error {
	if f.inner.SkipTrace() {
		return nil
	}
	// ⤵️ 5.11 回填：await TracerWorkflowUtils.trace(f.inner, data)
	return nil
}

// TraceError 记录组件错误追踪。
// ⤵️ 5.11 回填：TracerWorkflowUtils 实现后填充真实逻辑
// 对应 Python: Session.trace_error(error)
func (f *NodeSessionFacade) TraceError(ctx context.Context, err error) error {
	if f.inner.SkipTrace() {
		return nil
	}
	// ⤵️ 5.11 回填：await TracerWorkflowUtils.trace_error(f.inner, err)
	return nil
}

// ──────────────────────────── 交互方法（桩实现） ────────────────────────────

// Interact 请求用户输入。
//
// 流式模式下（streamMode=true）返回错误，因为 GraphInterrupt 无法在
// async generator 中恢复执行。这是工作流引擎的硬限制，不是设计偏好。
//
// ⤵️ 5.7 回填：WorkflowInteraction 实现后填充真实逻辑
// 对应 Python: Session.interact(value)
func (f *NodeSessionFacade) Interact(ctx context.Context, value any) (any, error) {
	if f.streamMode {
		return nil, fmt.Errorf("interact when streaming process(transform or collect) is not supported, comp_id=%s, workflow=%s",
			f.GetComponentID(), f.GetWorkflowID())
	}
	// ⤵️ 5.7 回填：if f.interaction == nil { f.interaction = NewWorkflowInteraction(f.inner) }
	// ⤵️ 5.7 回填：return f.interaction.WaitUserInputs(ctx, value)
	return nil, nil
}

// ──────────────────────────── 流写入方法（桩实现） ────────────────────────────

// WriteStream 写入标准输出流。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
// 对应 Python: Session.write_stream(data)
func (f *NodeSessionFacade) WriteStream(ctx context.Context, data any) error {
	// ⤵️ 5.10 回填：writer := f.streamWriter(); if writer != nil { return writer.Write(ctx, data) }
	return nil
}

// WriteCustomStream 写入自定义流。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
// 对应 Python: Session.write_custom_stream(data)
func (f *NodeSessionFacade) WriteCustomStream(ctx context.Context, data any) error {
	// ⤵️ 5.10 回填：writer := f.customWriter(); if writer != nil { return writer.Write(ctx, data) }
	return nil
}

// ──────────────────────────── 环境/配置方法（桩实现） ────────────────────────────

// GetEnv 获取环境变量值。
// ⤵️ 5.12 回填：Config 返回真实类型后实现 get_env
// 对应 Python: Session.get_env(key)
func (f *NodeSessionFacade) GetEnv(key string) any {
	// ⤵️ 5.12 回填：return f.inner.Config().GetEnv(key)
	return nil
}

// GetNodeConfig 获取节点级配置。
// ⤵️ 5.12 回填：Config 返回真实类型后实现 get_node_config
// 对应 Python: Session.get_node_config()
func (f *NodeSessionFacade) GetNodeConfig() any {
	// ⤵️ 5.12 回填：return f.inner.NodeConfig()
	return nil
}
```

确保 import 包含 `"context"` 和 `"fmt"`。

- [ ] **Step 2: 在 node_test.go 添加桩方法测试**

```go
// TestNodeSessionFacade_Trace_桩 测试 Trace 桩方法
func TestNodeSessionFacade_Trace_桩(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	err := facade.Trace(context.Background(), map[string]any{"data": "test"})
	if err != nil {
		t.Errorf("Trace 桩应返回 nil，实际=%v", err)
	}
}

// TestNodeSessionFacade_TraceError_桩 测试 TraceError 桩方法
func TestNodeSessionFacade_TraceError_桩(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	err := facade.TraceError(context.Background(), fmt.Errorf("test error"))
	if err != nil {
		t.Errorf("TraceError 桩应返回 nil，实际=%v", err)
	}
}

// TestNodeSessionFacade_Trace_SkipTrace 测试 SkipTrace 时跳过追踪
func TestNodeSessionFacade_Trace_SkipTrace(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", true) // skipTrace=true
	facade := NewNodeSessionFacade(ns, false)

	err := facade.Trace(context.Background(), map[string]any{"data": "test"})
	if err != nil {
		t.Errorf("SkipTrace 时 Trace 应返回 nil，实际=%v", err)
	}
}

// TestNodeSessionFacade_Interact_流式模式返回错误 测试流式模式下 Interact 禁止
func TestNodeSessionFacade_Interact_流式模式返回错误(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, true) // streamMode=true

	_, err := facade.Interact(context.Background(), "question")
	if err == nil {
		t.Error("流式模式下 Interact 应返回错误")
	}
}

// TestNodeSessionFacade_Interact_非流式模式 测试非流式模式下 Interact 桩返回 nil
func TestNodeSessionFacade_Interact_非流式模式(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false) // streamMode=false

	result, err := facade.Interact(context.Background(), "question")
	if err != nil {
		t.Errorf("非流式模式下 Interact 桩应返回 nil 错误，实际=%v", err)
	}
	if result != nil {
		t.Errorf("Interact 桩应返回 nil 结果，实际=%v", result)
	}
}

// TestNodeSessionFacade_WriteStream_桩 测试 WriteStream 桩方法
func TestNodeSessionFacade_WriteStream_桩(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	err := facade.WriteStream(context.Background(), map[string]any{"data": "test"})
	if err != nil {
		t.Errorf("WriteStream 桩应返回 nil，实际=%v", err)
	}
}

// TestNodeSessionFacade_WriteCustomStream_桩 测试 WriteCustomStream 桩方法
func TestNodeSessionFacade_WriteCustomStream_桩(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	err := facade.WriteCustomStream(context.Background(), map[string]any{"data": "test"})
	if err != nil {
		t.Errorf("WriteCustomStream 桩应返回 nil，实际=%v", err)
	}
}

// TestNodeSessionFacade_GetEnv_桩 测试 GetEnv 桩方法
func TestNodeSessionFacade_GetEnv_桩(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	result := facade.GetEnv("any_key")
	if result != nil {
		t.Errorf("GetEnv 桩应返回 nil，实际=%v", result)
	}
}

// TestNodeSessionFacade_GetNodeConfig_桩 测试 GetNodeConfig 桩方法
func TestNodeSessionFacade_GetNodeConfig_桩(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	result := facade.GetNodeConfig()
	if result != nil {
		t.Errorf("GetNodeConfig 桩应返回 nil，实际=%v", result)
	}
}
```

确保 import 包含 `"context"` 和 `"fmt"`。

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/ -v -run "TestNodeSessionFacade" -count=1`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/session/node.go internal/agentcore/session/node_test.go
git commit -m "feat: NodeSessionFacade 添加 7 个桩方法(Trace/TraceError/Interact/WriteStream/WriteCustomStream/GetEnv/GetNodeConfig)"
```

---

### Task 6: 更新 doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/session/doc.go`

- [ ] **Step 1: 在文件目录树中添加 node.go 条目**

在 `doc.go` 的文件目录中，在 `workflow.go` 行后添加 `node.go`：

```
//	├── agent.go            # Session 公开会话（Agent 场景）
//	├── workflow.go         # WorkflowSession 公开会话（Workflow 场景）
//	├── node.go             # NodeSessionFacade 公开会话（工作流组件场景）
```

同时在核心类型/接口索引中添加：

```
//	NodeSessionFacade — 工作流节点会话门面，组件开发者面向 API
```

- [ ] **Step 2: 运行测试验证 doc.go 格式正确**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/ -v -count=1`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/session/doc.go
git commit -m "docs: 更新 session/doc.go 文件目录，添加 node.go 条目"
```

---

### Task 7: 全量测试 + 更新实现计划状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -v -count=1`

Expected: 全部 PASS

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 中 5.5 状态**

将 `| 5.5 | ☐ | SessionNode |` 改为 `| 5.5 | ✅ | SessionNode |`，补充说明内容：

```
| 5.5 | ✅ | SessionNode | ✅ NodeSessionFacade 门面（18 个方法）；✅ Session.GetState 改为 StateKey；✅ 删除 WorkflowSession 多余状态方法；⤵️ 5.7 回填 Interaction；⤵️ 5.10 回填 StreamWriter；⤵️ 5.11 回填 Tracer；⤵️ 5.12 回填 Config | `openjiuwen/core/session/node.py` |
```

- [ ] **Step 3: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 5.5 SessionNode 状态为已完成"
```
