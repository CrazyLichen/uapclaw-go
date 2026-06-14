# 5.4 WorkflowSession 设计文档

## 概述

实现 WorkflowSession 完整体系，对标 Python `openjiuwen/core/session/` 中的工作流会话机制。包含 Workflow State 体系、内部 WorkflowSession、外部 WorkflowSession 门面、NodeSession 和 SubWorkflowSession。

## 对标 Python 源码

| Python 文件 | Go 文件 |
|------------|---------|
| `openjiuwen/core/session/state/workflow_state.py` — StateCollection/CommitState/InMemoryState | `session/state/workflow_state_collection.go` + `workflow_commit_state.go` + `workflow_inmemory_state.go` |
| `openjiuwen/core/session/internal/workflow.py` — WorkflowSession/NodeSession/SubWorkflowSession | `session/internal/workflow_session.go` |
| `openjiuwen/core/session/workflow.py` — Session（外部封装） | `session/workflow.go` |
| `openjiuwen/core/session/agent.py` — create_workflow_session | `session/agent.go` 回填 |

## 分层架构

与 AgentSession 保持一致的两层模式：

| 层次 | AgentSession 模式 | WorkflowSession 对标 |
|------|-------------------|---------------------|
| 内部实现 | `internal/agent_session.go` → `AgentSession` struct | `internal/workflow_session.go` → `WorkflowSession` struct |
| 外部门面 | `agent.go` → `Session` struct | `workflow.go` → `WorkflowSession` struct |

内部层实现 `BaseSession` 接口，持有状态、配置等组件；外部门面组合内部实例，提供业务 API。

## 一、Workflow State 体系

### 1.1 WorkflowStateCollection

对标 Python `workflow_state.py` 的 `StateCollection`，组合 4 个 `CommitState` 区域：

```go
type WorkflowStateCollection struct {
    ioState       CommitState      // 输入输出状态
    globalState   CommitState      // 全局状态（从 AgentSession 共享）
    compState     CommitState      // 组件状态
    workflowState CommitState      // 工作流状态
    traceState    map[string]any   // 追踪状态
    parentID      string           // 父节点 ID
    nodeID        string           // 当前节点 ID
}
```

核心方法：

| 方法 | 逻辑 |
|------|------|
| `GetGlobal(key)` | 三级回退：globalState → ioState(parentID前缀) → ioState(nodeID前缀) |
| `UpdateGlobal(data)` | `globalState.UpdateByID(nodeID, data)` |
| `Update(data)` | `compState.UpdateByID(nodeID, {nodeID: data})` |
| `Get(key)` | key 为 nil 返回 compState(nodeID)；否则 compState(prefix+nodeID) |
| `CommitCmp()` | 提交 compState + ioState（仅当前 nodeID） |
| `GetState()` / `SetState()` | 持久化/恢复整体状态 |
| `Dump()` | 导出完整快照 |

同时实现 `State` 接口（`Get`, `GetByPrefix`, `GetByTransformer`, `Update`, `GetState`, `SetState`）。

### 1.2 WorkflowCommitState

对标 Python `CommitState`，嵌入 WorkflowStateCollection 并增加工作流语义方法：

```go
type WorkflowCommitState struct {
    WorkflowStateCollection          // 嵌入基础四区状态
    workflowOnly           bool      // 是否仅工作流模式（无共享 globalState 时为 true）
}
```

额外方法：

| 方法 | 逻辑 |
|------|------|
| `GetWorkflowState(key)` | 查询 workflowState |
| `UpdateAndCommitWorkflowState(data)` | 立即更新并提交 workflowState |
| `SetOutputs(data)` | 向 ioState 写入当前节点输出 |
| `GetInputs(schema)` | 从 ioState 查询父节点输出（即当前节点输入） |
| `GetOutputs(nodeID)` | 从 ioState 查询指定节点输出 |
| `GetInputsByTransformer(transformer)` | 通过 transformer 获取输入 |
| `CommitUserInputs(inputs)` | 同时写入 ioState + globalState 并 commit |
| `Commit()` | 提交全部四个状态 |
| `Rollback()` | 回滚全部四个状态的当前节点 |
| `GetUpdates()` / `SetUpdates()` | 获取/设置未提交更新 |
| `CreateNodeState(nodeID, parentID)` | **关键工厂方法**：创建新的 WorkflowCommitState 视图，共享底层状态，切换 nodeID/parentID |

### 1.3 InMemoryWorkflowState

对标 Python `InMemoryState`，便捷构造器：

```go
func NewInMemoryWorkflowState(globalState CommitState) *WorkflowCommitState
```

- 有 `globalState` 时：`workflowOnly = false`，getUpdates/getState 包含 globalState
- 无 `globalState` 时：`workflowOnly = true`，所有状态独立

### 1.4 与现有 State 体系的关系

```
State (接口，已有)
├── InMemoryState (已有)           ← AgentSession 用
├── AgentStateCollection (已有)    ← AgentSession 用
└── WorkflowCommitState (新增)     ← WorkflowSession 用
       └── 嵌入 WorkflowStateCollection (新增)

CommitState (接口，已有)
├── InMemoryCommitState (已有)     ← 作为 4 个区域的底层实现
└── WorkflowCommitState 也实现 CommitState（委托给 4 个子区域）
```

## 二、内部 WorkflowSession

对标 Python `internal/workflow.py` 的 `WorkflowSession`，实现 `BaseSession` 接口。

### 2.1 结构体

```go
type WorkflowSession struct {
    sessionID           string              // 会话 ID（从 parent 继承或自动生成）
    parent              session.BaseSession // 父会话（通常是 AgentSession）
    config              any                 // 配置对象 ⤵️ 5.12 → SessionConfig
    tracer              any                 // 追踪器 ⤵️ 5.11 → Tracer
    state               state.State         // 状态对象（WorkflowCommitState）
    streamWriterManager any                 // 流写入管理器 ⤵️ 5.10 → StreamWriterManager
    actorManager        any                 // Actor 管理器 ⤵️ 后续 → ActorManager
    workflowID          string              // 工作流 ID
}
```

### 2.2 构造

Functional Options 模式：`NewWorkflowSession(opts ...WorkflowSessionOption) *WorkflowSession`

默认行为：
- 有 parent 时：sessionID 继承 parent、config 继承 parent、tracer 继承 parent
- 无 parent 时：sessionID 自动生成 UUID、config 新建、tracer 为 nil
- state 默认创建 `NewInMemoryWorkflowState(nil)`（workflowOnly=true）
- streamWriterManager 和 actorManager 初始为 nil，需外部注入

选项函数：`WithParent`, `WithSessionID`, `WithState`, `WithWorkflowID`

### 2.3 BaseSession 接口实现

| 方法 | 行为 |
|------|------|
| `Config()` | 返回 `s.config` |
| `State()` | 返回 `s.state` |
| `Tracer()` | 返回 `s.tracer` |
| `StreamWriterManager()` | 返回 `s.streamWriterManager` |
| `SessionID()` | 返回 `s.sessionID` |
| `Checkpointer()` | 有 parent 则委托给 parent；无 parent 则从工厂获取（懒加载） |
| `ActorManager()` | 返回 `s.actorManager` |
| `Close()` | 如果 actorManager 不为 nil，调用其 Shutdown() |

### 2.4 额外方法

| 方法 | 行为 |
|------|------|
| `SetStreamWriterManager(mgr)` | 幂等注入，已设置则不覆盖 |
| `SetTracer(tracer)` | 直接设置（无幂等保护，与 Python 一致） |
| `SetActorManager(mgr)` | 幂等注入，已设置则不覆盖 |
| `SetWorkflowID(id)` | 设置 workflowID |
| `WorkflowID()` | 返回 workflowID |
| `MainWorkflowID()` | 直接返回 `WorkflowID()` |
| `WorkflowNestingDepth()` | 固定返回 0 |
| `Parent()` | 返回 parent |

### 2.5 幂等保护

`SetStreamWriterManager` 和 `SetActorManager` 只在当前值为 nil 时才设置，防止覆盖。`SetTracer` 无幂等保护，与 Python 行为一致。

## 三、外部 WorkflowSession 门面

对标 Python `session/workflow.py` 的 `Session` 类，组合内部 `WorkflowSession`。

### 3.1 结构体

```go
type WorkflowSession struct {
    inner        *internal.WorkflowSession  // 内部实现
    envs         map[string]any             // 环境变量（从 parent.config 获取）
    workflowCard any                        // 工作流卡片 ⤵️ 后续回填
}
```

### 3.2 构造

`NewWorkflowSession(opts ...WorkflowSessionOption) *WorkflowSession`

默认行为：
- 有 parent 时：sessionID 使用传入值，envs 从 parent.Config() 获取
- 无 parent 但有 sessionID：使用传入值
- 两者都没有：自动生成 UUID

### 3.3 方法

| 方法 | 行为 |
|------|------|
| `GetSessionID()` | 返回 inner.SessionID() |
| `GetEnvs()` | 返回 envs |
| `GetParent()` | 返回 inner.Parent() |
| `SetWorkflowCard(card)` | 设置 workflowCard |
| `GetWorkflowCard()` | 返回 workflowCard |
| `State()` | 返回 inner.State() |
| `UpdateState(data)` | 类型断言为 WorkflowCommitState，调用 UpdateGlobal |
| `GetState(key)` | 类型断言为 WorkflowCommitState，调用 GetGlobal |
| `DumpState()` | 类型断言为 WorkflowCommitState，调用 Dump |
| `Close()` | 委托 inner.Close() |

### 3.4 与 AgentSession 门面的对比

| 特性 | Session（Agent 门面） | WorkflowSession（Workflow 门面） |
|------|----------------------|-------------------------------|
| 内部组合 | `*internal.AgentSession` | `*internal.WorkflowSession` |
| 状态操作 | `AgentStateCollection` 的 GetGlobal/UpdateGlobal | `WorkflowCommitState` 的 GetGlobal/UpdateGlobal |
| 生命周期 | PreRun/PostRun/Commit | Close（更简单） |
| 流操作 | WriteStream/CloseStream 等 | 无（由内部层管理） |
| 创建子会话 | `CreateWorkflowSession()` | 无 |
| 额外字段 | preRunDone/postRunDone/interaction | envs/workflowCard |

## 四、NodeSession + SubWorkflowSession

### 4.1 NodeSession

代表工作流中单个节点的会话视图，包装已有 BaseSession，通过 CreateNodeState 切换视角。

```go
type NodeSession struct {
    session      session.BaseSession // 被包装的会话（通常是 WorkflowSession）
    executableID string              // 执行路径（parentID + "." + nodeID）
    nodeID       string              // 节点 ID
    parentID     string              // 父节点 ID
    nodeConfig   map[string]any      // 节点级配置
    skipTrace    bool                // 是否跳过追踪
}
```

构造：`NewNodeSession(parent BaseSession, nodeID, parentID string) *NodeSession`

- `executableID = parentID + "." + nodeID`
- State 视图切换：从 parent 的 State 类型断言为 `*WorkflowCommitState`，调用 `CreateNodeState(executableID, parentID)`

BaseSession 接口实现：所有方法委托给被包装 session，除了：
- `State()` 返回 CreateNodeState 创建的节点专属状态视图
- `Close()` 空实现，不关闭底层 session

额外方法：`NodeConfig()`, `SetNodeConfig()`, `SkipTrace()`, `SetSkipTrace()`, `ExecutableID()`, `NodeID()`, `ParentID()`

### 4.2 SubWorkflowSession

代表嵌套的子工作流，嵌入 NodeSession，增加 ActorManager 和嵌套深度管理。

```go
type SubWorkflowSession struct {
    NodeSession                      // 嵌入 NodeSession
    actorManager        any          // 子工作流专属 Actor 管理器 ⤵️ → ActorManager
    workflowNestingDepth int         // 工作流嵌套深度
}
```

构造：`NewSubWorkflowSession(parent BaseSession, nodeID, parentID string, parentDepth int) *SubWorkflowSession`

- `workflowNestingDepth = parentDepth + 1`

与 NodeSession 的差异：

| 特性 | NodeSession | SubWorkflowSession |
|------|------------|-------------------|
| ActorManager | 委托给被包装 session | 拥有自己的 actorManager |
| WorkflowNestingDepth | 不涉及 | parentDepth + 1 |
| Close() | 空实现 | 如果 actorManager 不为 nil，调用其 Shutdown() |
| SetActorManager() | 无 | 幂等注入 |

## 五、状态共享机制

AgentSession 创建 WorkflowSession 时，globalState 共享同一个 `*InMemoryState` 实例：

```
AgentSession
  └── AgentStateCollection
        └── globalState (*InMemoryState 实例 0xc0001a2000)
              ↑ 共享同一个指针
WorkflowSession
  └── WorkflowCommitState
        └── globalState (InMemoryCommitState)
              └── 底层 state = 0xc0001a2000（同一个实例）
```

ioState、compState、workflowState 则各自独立，不共享。

## 六、Go 组合 vs Python 继承

| Python | Go |
|--------|-----|
| `class WorkflowSession(BaseSession)` | `struct WorkflowSession` 实现 `BaseSession` 接口 |
| `class NodeSession(BaseSession)` | `struct NodeSession` 实现 `BaseSession` 接口，委托给被包装 session |
| `class SubWorkflowSession(NodeSession)` | `struct SubWorkflowSession` 嵌入 `NodeSession`，覆写 ActorManager/Close |

## 七、文件变更清单

| 文件 | 操作 | 内容 |
|------|------|------|
| `session/state/workflow_state_collection.go` | 新增 | WorkflowStateCollection — 四区状态集合 |
| `session/state/workflow_commit_state.go` | 新增 | WorkflowCommitState — 增加 commit/rollback/IO 语义 |
| `session/state/workflow_inmemory_state.go` | 新增 | InMemoryWorkflowState — 便捷构造器 |
| `session/internal/workflow_session.go` | 新增 | WorkflowSession + NodeSession + SubWorkflowSession |
| `session/workflow.go` | 新增 | WorkflowSession 外部门面 |
| `session/agent.go` | 修改 | `CreateWorkflowSession()` 回填真实逻辑 |
| `session/state/doc.go` | 修改 | 更新文件目录树 |
| `session/internal/doc.go` | 修改 | 更新文件目录树 |
| `session/doc.go` | 修改 | 更新文件目录树 |

## 八、测试文件

| 测试文件 | 覆盖范围 |
|---------|---------|
| `session/state/workflow_state_collection_test.go` | 四区状态读写、三级回退查询 |
| `session/state/workflow_commit_state_test.go` | commit/rollback/set_outputs/get_inputs/create_node_state |
| `session/state/workflow_inmemory_state_test.go` | 便捷构造（有/无 globalState 两种模式） |
| `session/internal/workflow_session_test.go` | 内部 WorkflowSession + NodeSession + SubWorkflowSession |
| `session/workflow_test.go` | 外部门面 + AgentSession.CreateWorkflowSession() 集成 |

## 九、依赖关系（自底向上）

```
state/ 包内（已有基础）：
  InMemoryState + InMemoryCommitState + AgentStateCollection (已有)
        ↑ 依赖
  WorkflowStateCollection (新增)
        ↑ 嵌入
  WorkflowCommitState (新增)
        ↑ 便捷构造
  InMemoryWorkflowState (新增)

internal/ 包内：
  AgentSession (已有) ← 作为 parent
  WorkflowSession (新增) ← State 用 WorkflowCommitState
  NodeSession (新增) ← 包装 WorkflowSession，CreateNodeState 切视角
  SubWorkflowSession (新增) ← 嵌入 NodeSession，加 ActorManager

session/ 包内（公开层）：
  Session (已有) ← CreateWorkflowSession() 回填
  WorkflowSession (新增) ← 组合 internal.WorkflowSession
```
