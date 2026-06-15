# State 双层接口体系重构设计

> 日期：2026-08-28
> 状态：待审批
> 对应 Python：`openjiuwen/core/session/state/base.py` + `agent_state.py` + `workflow_state.py`

## 一、背景

### 1.1 当前问题

当前 Go 的 State 体系只有一套接口链：

```
ReadableState → RecoverableState → State → CommitState
```

这条链对应 Python 的**底层体系**（`StateLike` 系列），缺少 Python 的**上层体系**（`State` 接口）。
Python 的 `State(RecoverableStateLike)` 是面向会话调用方的独立接口，包含 `get_global/update_global/update_trace/dump`，
而 Go 中这些方法被降级为具体结构体的自有方法，导致消费方到处类型断言：

```go
// 当前：每个消费方方法都要断言具体类型
if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok {
    cs.GetGlobal(key)
}
```

### 1.2 目标

完全对齐 Python 的双层体系设计，引入上层 `SessionState` 接口，消除消费方的类型断言，
让 `GetGlobal/UpdateGlobal/UpdateTrace/Dump` 可通过接口多态调用。

## 二、设计方案

### 2.1 接口改名映射

| Python | Go 当前名 | Go 新名 | 说明 |
|--------|----------|---------|------|
| `ReadableStateLike` | `ReadableState` | `ReadableStateLike` | 底层只读接口，加 Like 后缀 |
| `RecoverableStateLike` | `RecoverableState` | `RecoverableStateLike` | 底层可恢复接口，加 Like 后缀 |
| `StateLike` | `State` | `StateLike` | 底层可读写接口，加 Like 后缀 |
| `CommitStateLike` | `CommitState` | `CommitStateLike` | 底层事务接口，加 Like 后缀 |
| `State`（上层） | 无 | `SessionState` | 新增上层会话状态接口 |

**命名规则**：
- `Like` 后缀 = 底层存储抽象（面向存储实现）
- 无 `Like` 后缀 = 上层会话抽象（面向调用方）

### 2.2 接口定义

#### 底层体系（StateLike 系列）

```go
// ReadableStateLike 只读状态访问接口
// 对应 Python: ReadableStateLike
type ReadableStateLike interface {
    Get(key StateKey) any
    GetByPrefix(key StateKey, nestedPrefix string) any
}

// RecoverableStateLike 可恢复状态接口
// 对应 Python: RecoverableStateLike
type RecoverableStateLike interface {
    GetState() map[string]any
    SetState(state map[string]any)
}

// StateLike 可读写状态接口，组合只读和可恢复能力
// 对应 Python: StateLike(ReadableStateLike, RecoverableStateLike)
type StateLike interface {
    ReadableStateLike
    RecoverableStateLike
    Update(data map[string]any) error
    GetByTransformer(transformer Transformer) any
}

// CommitStateLike 事务性状态接口，支持按节点 ID 的提交/回滚
// 对应 Python: CommitStateLike(StateLike)
type CommitStateLike interface {
    StateLike
    UpdateByID(nodeID string, data map[string]any) error
    Commit(nodeID ...string)
    Rollback(nodeID string)
    GetUpdates() map[string][]map[string]any
    SetUpdates(updates map[string][]map[string]any)
}
```

#### 上层体系（SessionState）

```go
// SessionState 会话状态接口，面向会话调用方的统一抽象
// 对应 Python: State(RecoverableStateLike)
//
// 提供 get_global/update_global/update_trace/dump 等方法，
// 由 AgentStateCollection 和 WorkflowStateCollection 实现。
// 消费方通过此接口多态调用，无需类型断言。
type SessionState interface {
    RecoverableStateLike
    GetGlobal(key StateKey) any
    UpdateGlobal(data map[string]any)
    UpdateTrace(span any)
    Update(data map[string]any) error
    Get(key StateKey) any
    Dump() map[string]any
}
```

### 2.3 结构体改名映射

| Python | Go 当前名 | Go 新名 |
|--------|----------|---------|
| `InMemoryStateLike` | `InMemoryState` | `InMemoryStateLike` |
| `InMemoryCommitState` | `InMemoryCommitState` | `InMemoryCommitState`（不变） |
| `agent_state.StateCollection` | `AgentStateCollection` | `AgentStateCollection`（不变） |
| `workflow_state.StateCollection` | `WorkflowStateCollection` | `WorkflowStateCollection`（不变） |
| `workflow_state.CommitState` | `WorkflowCommitState` | `WorkflowCommitState`（不变） |
| `workflow_state.InMemoryState` | `NewInMemoryWorkflowState()` 工厂 | `NewInMemoryWorkflowState()`（不变） |

### 2.4 结构体实现的接口

```
InMemoryStateLike    → 实现 StateLike + SessionState（默认实现）
InMemoryCommitState  → 实现 CommitStateLike + SessionState（默认实现）
AgentStateCollection → 实现 SessionState
WorkflowStateCollection → 实现 SessionState
WorkflowCommitState  → 嵌入 WorkflowStateCollection，实现 SessionState（不实现 CommitStateLike，与 Python 一致）
```

### 2.5 InMemoryStateLike 对 SessionState 的默认实现

`InMemoryStateLike` 是单个存储单元，没有"全局"和"组件"的概念。
对 `SessionState` 的上层方法提供合理默认：

```go
// GetGlobal 单存储单元无全局概念，返回 nil
func (s *InMemoryStateLike) GetGlobal(key StateKey) any { return nil }

// UpdateGlobal 单存储单元无全局概念，空操作
func (s *InMemoryStateLike) UpdateGlobal(data map[string]any) {}

// UpdateTrace 单存储单元无追踪概念，空操作
func (s *InMemoryStateLike) UpdateTrace(span any) {}

// Dump 委托 GetState
func (s *InMemoryStateLike) Dump() map[string]any { return s.GetState() }
```

### 2.6 InMemoryCommitState 对 SessionState 的默认实现

类似 `InMemoryStateLike`，委托底层 `StateLike`：

```go
// GetGlobal 委托底层 state
func (s *InMemoryCommitState) GetGlobal(key StateKey) any { return nil }

// UpdateGlobal 空操作
func (s *InMemoryCommitState) UpdateGlobal(data map[string]any) {}

// UpdateTrace 空操作
func (s *InMemoryCommitState) UpdateTrace(span any) {}

// Dump 委托 GetState
func (s *InMemoryCommitState) Dump() map[string]any { return s.GetState() }
```

### 2.7 Commit 方法统一为可变参数

**当前问题**：`CommitState.Commit(nodeID ...string)` 与 `WorkflowCommitState.Commit()` 无参签名冲突，
导致 `WorkflowCommitState` 无法直接实现 `CommitState` 接口。

**解决方案**：`WorkflowCommitState.Commit()` 改为 `Commit(nodeID ...string)`，与 `CommitStateLike` 签名一致。

```go
// WorkflowCommitState.Commit 统一为可变参数
// 不传参 → 提交全部四个子状态（对应 Python CommitState.commit()）
// 传参 → 提交指定节点（对应 Python CommitStateLike.commit(node_id)）
func (s *WorkflowCommitState) Commit(nodeID ...string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.ioState.Commit(nodeID...)
    s.compState.Commit(nodeID...)
    s.globalState.Commit(nodeID...)
    s.workflowState.Commit(nodeID...)
}
```

**删除适配方法**：`CommitCommitState()` 和 `RollbackNode()` 不再需要，
因为 `Commit(nodeID ...string)` 和 `Rollback(nodeID string)` 已直接满足 `CommitStateLike` 接口。

### 2.9 GetUpdates/SetUpdates 返回类型调整

**当前问题**：`WorkflowCommitState.GetUpdates()` 返回 `map[string]any`，而 `CommitStateLike` 接口
要求 `map[string][]map[string]any`。两者类型不一致，且 Python 的 `CommitState.get_updates()`
也是返回混合了 `dict[str, list[dict]]` 和 `None` 的 `dict`。

**Go 静态类型的限制**：`map[string][]map[string]any` 的 value 必须是 `[]map[string]any`，
无法存放 nil。因此 `WorkflowCommitState` 不能直接用 `map[string][]map[string]any` 表示
`workflowOnly=false` 时 `global_state_updates` 为 nil 的语义。

**方案：WorkflowCommitState 不实现 CommitStateLike 的 GetUpdates/SetUpdates**。
`WorkflowCommitState` 的 `GetUpdates/SetUpdates` 语义与单个 `CommitStateLike` 不同——
它是四个子状态的聚合视图。强制对齐签名会丢失 `workflowOnly` 条件分支的 nil 语义。

具体处理：
- `WorkflowCommitState` 保留 `GetUpdates() map[string]any` 和 `SetUpdates(map[string]any)` 签名不变
- `WorkflowCommitState` 不声明实现 `CommitStateLike`（与当前行为一致），但通过 `Commit(nodeID ...string)`
  和 `Rollback(nodeID string)` 签名统一，仍能**部分满足** `CommitStateLike` 接口
- 如需将 `WorkflowCommitState` 当作 `CommitStateLike` 使用（如 `WorkflowStateCollection` 的四个字段），
  通过 `CommitStateLike` 接口引用四个子状态即可，`WorkflowCommitState` 本身不是 `CommitStateLike`

**结论**：`WorkflowCommitState` 实现 `SessionState` 接口（上层），
但不实现 `CommitStateLike` 接口（底层）。这与 Python 的类继承体系一致——
Python 中 `CommitState(StateCollection)` 继承自 `StateCollection(State)`，
它**不是** `CommitStateLike` 的子类，只是内部持有四个 `CommitStateLike` 实例。

### 2.8（修订）WorkflowCommitState 接口实现

```
WorkflowCommitState 实现：
  ✅ SessionState      — 上层会话状态接口
  ❌ CommitStateLike   — 底层事务接口（与 Python 一致，CommitState 不是 CommitStateLike 的子类）
```

删除适配方法 `CommitCommitState/RollbackNode`，但保留 `Commit(nodeID ...string)` 和 `Rollback(nodeID string)`
的统一签名。这两个方法虽然签名与 `CommitStateLike` 一致，但由于 `GetUpdates/SetUpdates` 签名不同，
`WorkflowCommitState` 整体不满足 `CommitStateLike` 接口。

调用方如需按 `CommitStateLike` 使用 `WorkflowCommitState`，需通过其子状态字段访问（如 `wcs.compState`）。

### 2.10 AgentStateCollection 补充 traceState

当前缺失 `traceState` 字段和 `UpdateTrace` 方法。Python 的 `agent_state.StateCollection` 有：

```python
self._trace_state = dict()

def update_trace(self, span):
    pass  # 基类空实现

def dump(self) -> dict:
    return {
        "global_state": ...,
        "agent_state": ...,
        "trace_state": self._trace_state
    }
```

Go 补充：

```go
type AgentStateCollection struct {
    mu         sync.RWMutex
    globalState *InMemoryStateLike
    agentState  *InMemoryStateLike
    traceState  map[string]any   // 新增
}

func (s *AgentStateCollection) UpdateTrace(span any) {
    s.mu.Lock()
    defer s.mu.Unlock()
    // Agent 层的 trace 是空实现，与 Python 一致
}

func (s *AgentStateCollection) Dump() map[string]any {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return map[string]any{
        GlobalStateKey: s.globalState.GetState(),
        AgentStateKey:  s.agentState.GetState(),
        "trace_state":  s.traceState,
    }
}
```

### 2.11 消费方改动

#### NodeSessionFacade（node.go）

**当前**：到处类型断言
```go
if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok {
    cs.GetGlobal(key)
}
```

**改为**：通过 `SessionState` 接口调用
```go
f.inner.State().GetGlobal(key)
```

前提：`NodeSession.inner.State()` 返回类型从 `state.StateLike` 改为 `state.SessionState`。

#### Session（agent.go）

**当前**：类型断言 `*state.AgentStateCollection`
```go
if coll, ok := s.inner.State().(*state.AgentStateCollection); ok {
    coll.UpdateGlobal(data)
}
```

**改为**：通过 `SessionState` 接口调用
```go
s.inner.State().UpdateGlobal(data)
```

#### interaction/base.go

**当前**：类型断言 `*state.WorkflowCommitState`
```go
if cs, ok := st.(*state.WorkflowCommitState); ok {
    existing = cs.GetGlobal(state.StringKey(InteractiveInputKey))
}
```

**改为**：
```go
existing = st.GetGlobal(state.StringKey(InteractiveInputKey))
```

### 2.12 internal 层 State() 返回类型变更

```go
// AgentSession.State() 返回类型
// 当前：state.StateLike
// 改为：state.SessionState
func (s *AgentSession) State() state.SessionState { return s.st }

// NodeSession.State() 返回类型
// 当前：state.StateLike
// 改为：state.SessionState
func (n *NodeSession) State() state.SessionState { return n.st }

// WorkflowSession.st 字段类型
// 当前：state.StateLike
// 改为：state.SessionState
```

## 三、改动文件清单

### 3.1 state 包内部

| 文件 | 改动 |
|------|------|
| `state.go` | 接口改名：`ReadableState→ReadableStateLike`、`RecoverableState→RecoverableStateLike`、`State→StateLike`、`CommitState→CommitStateLike`；新增 `SessionState` 接口 |
| `inmemory_state.go` | 结构体改名 `InMemoryState→InMemoryStateLike`；新增 `SessionState` 默认实现（GetGlobal/UpdateGlobal/UpdateTrace/Dump） |
| `inmemory_commit_state.go` | 新增 `SessionState` 默认实现；构造函数参数类型 `State→StateLike` |
| `agent_state_collection.go` | 新增 `traceState` 字段；`UpdateTrace` 方法；`Dump` 含 trace_state；实现 `SessionState` 接口；字段类型 `*InMemoryState→*InMemoryStateLike` |
| `workflow_state_collection.go` | 实现 `SessionState` 接口；字段类型 `CommitState→CommitStateLike` |
| `workflow_commit_state.go` | `Commit()` 改为可变参数签名；删除 `CommitCommitState()/RollbackNode()` 适配方法；`Rollback()` 改为 `Rollback(nodeID string)`；实现 `SessionState` 接口；保留 `GetUpdates() map[string]any` 和 `SetUpdates(map[string]any)` 签名不变 |
| `workflow_inmemory_state.go` | 参数类型 `CommitState→CommitStateLike` |
| `key.go` | 无改动 |
| `utils.go` | 无改动 |
| `doc.go` | 更新接口和结构体说明 |

### 3.2 state 包外部

| 文件 | 改动 |
|------|------|
| `session/internal/agent_session.go` | `WithState` 参数类型 `State→SessionState`；`st` 字段类型 `State→SessionState`；`State()` 返回类型 `SessionState` |
| `session/internal/workflow_session.go` | `WithWorkflowState` 参数类型 `State→SessionState`；`st` 字段类型 `State→SessionState`；`State()` 返回类型 `SessionState`；类型断言改为接口调用 |
| `session/internal/node_session.go` | `State()` 返回类型 `SessionState` |
| `session/agent.go` | 类型断言改为接口调用；`CreateWorkflowSession` 中 `CommitState→CommitStateLike` |
| `session/node.go` | 类型断言改为接口调用 |
| `session/workflow.go` | 相关类型引用更新 |
| `session/interaction/base.go` | 类型断言改为接口调用 |
| `session/interaction/interaction.go` | 类型断言改为接口调用 |

### 3.3 测试文件

所有测试文件中的 `State` → `StateLike`、`CommitState` → `CommitStateLike`、`InMemoryState` → `InMemoryStateLike`。
类型断言测试改为接口调用测试。

## 四、兼容性处理

### 4.1 向后兼容别名（可选）

如果外部包引用了旧名，可暂时保留别名：

```go
// 向后兼容别名，后续版本移除
type ReadableState = ReadableStateLike
type RecoverableState = RecoverableStateLike
type State = StateLike
type CommitState = CommitStateLike
type InMemoryState = InMemoryStateLike
```

### 4.2 Rollback 语义变化

**当前**：`WorkflowCommitState.Rollback()` 无参，回滚当前节点四个子状态。
**改为**：`Rollback(nodeID string)` 单参数，与 `CommitStateLike` 一致。

调用方需从 `cs.Rollback()` 改为 `cs.Rollback(nodeID)`。
`CommitUserInputs` 中已持有 `s.nodeID`，直接传入即可。

## 五、Python 对齐验证

### 5.1 接口对齐

| Python 接口 | Go 接口 | 方法覆盖 |
|-------------|---------|---------|
| `ReadableStateLike` | `ReadableStateLike` | `Get` + `GetByPrefix` ✅ |
| `RecoverableStateLike` | `RecoverableStateLike` | `GetState` + `SetState` ✅ |
| `StateLike` | `StateLike` | `Update` + `GetByTransformer` ✅ |
| `CommitStateLike` | `CommitStateLike` | `UpdateByID` + `Commit` + `Rollback` + `GetUpdates` + `SetUpdates` ✅ |
| `State` | `SessionState` | `GetGlobal` + `UpdateGlobal` + `UpdateTrace` + `Update` + `Get` + `Dump` ✅ |

### 5.2 实现对齐

| Python 类 | Go 结构体 | 实现接口 |
|-----------|----------|---------|
| `InMemoryStateLike(StateLike)` | `InMemoryStateLike` | `StateLike` + `SessionState` ✅ |
| `InMemoryCommitState(CommitStateLike)` | `InMemoryCommitState` | `CommitStateLike` + `SessionState` ✅ |
| `agent_state.StateCollection(State)` | `AgentStateCollection` | `SessionState` ✅ |
| `workflow_state.StateCollection(State)` | `WorkflowStateCollection` | `SessionState` ✅ |
| `workflow_state.CommitState(StateCollection)` | `WorkflowCommitState` | `SessionState`（不实现 CommitStateLike，与 Python 一致） ✅ |
| `workflow_state.InMemoryState(CommitState)` | `NewInMemoryWorkflowState()` | `SessionState` ✅ |

### 5.3 Commit 方法对齐

| Python 调用 | Go 调用 | 语义 |
|-------------|---------|------|
| `commit()` | `Commit()` | 提交全部 ✅ |
| `commit(node_id)` | `Commit(nodeID)` | 提交指定节点 ✅ |
| `commit()`（WorkflowCommitState 覆写版） | `Commit()` | 提交四个子状态 ✅ |

## 六、风险和注意事项

1. **改动面大**：所有引用 `state.State`/`state.CommitState`/`state.InMemoryState` 的地方都需要改名，
   但通过 type alias 可平滑过渡。

2. **Rollback 签名变化**：`WorkflowCommitState.Rollback()` 无参改为 `Rollback(nodeID string)` 有参，
   所有调用方需补充 nodeID 参数。

3. **WorkflowCommitState 不实现 CommitStateLike**：与 Python 一致，`CommitState(StateCollection)` 不是
   `CommitStateLike` 的子类。但 Go 中这意味着 `WorkflowCommitState` 不能赋值给 `CommitStateLike` 变量。
   需通过子状态字段（`wcs.compState` 等）间接访问。
