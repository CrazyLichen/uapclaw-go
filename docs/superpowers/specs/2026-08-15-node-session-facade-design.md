# NodeSessionFacade 设计文档

> 对应实现计划步骤 5.5：SessionNode（会话节点）
> Python 源码：`openjiuwen/core/session/node.py`

## 1. 概述

NodeSessionFacade 是工作流组件执行时的面向用户会话门面。它包装内部的 `internal.NodeSession`，
为工作流组件（ComponentExecutable）提供身份查询、状态读写、追踪、交互、流写入、环境变量等 API。

Python 中此类名为 `Session`（定义在 `node.py`），与 `agent.py` 的 `Session` 重名，
通过不同模块导入区分。Go 端命名为 `NodeSessionFacade` 以避免歧义。

### 消费者

`ComponentExecutable`（`component.py`）的 `on_invoke`/`on_stream`/`on_collect`/`on_transform`
方法将内部 `NodeSession` 包装为 `NodeSessionFacade` 后传给组件的 `invoke`/`stream`/`collect`/`transform`：

```
on_invoke(inputs, NodeSession)  → invoke(inputs, NodeSessionFacade(session, streamMode=false))
on_stream(inputs, NodeSession)  → stream(inputs, NodeSessionFacade(session, streamMode=false))
on_collect(inputs, NodeSession) → collect(inputs, NodeSessionFacade(session, streamMode=true))
on_transform(inputs, NodeSession) → transform(inputs, NodeSessionFacade(session, streamMode=true))
```

## 2. 结构体设计

**文件位置**：`internal/agentcore/session/node.go`

```go
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
```

## 3. 构造函数

```go
func NewNodeSessionFacade(inner *internal.NodeSession, streamMode bool) *NodeSessionFacade
```

构造时自动生成 `description` 字段：`[wf_id={inner.WorkflowID()},comp_id={inner.NodeID()}]`。

## 4. 方法清单

### 4.1 身份方法（无外部依赖）

| 方法 | 签名 | 委托目标 |
|------|------|---------|
| `GetWorkflowID()` | `string` | `inner.WorkflowID()` |
| `GetComponentID()` | `string` | `inner.NodeID()` |
| `GetComponentType()` | `string` | `inner.NodeType()` |
| `GetComponentDescription()` | `string` | `description` 字段 |
| `GetExecutableID()` | `string` | `inner.ExecutableID()` |
| `GetSessionID()` | `string` | `inner.SessionID()` |

### 4.2 状态方法（无外部依赖，类型断言委托）

委托模式：`if cs, ok := inner.State().(*state.WorkflowCommitState); ok { ... }`

| 方法 | 签名 | 委托目标 |
|------|------|---------|
| `UpdateState(data map[string]any)` | — | `cs.Update(data)` |
| `GetState(key state.StateKey)` | `(any, error)` | `cs.Get(key)` |
| `UpdateGlobalState(data map[string]any)` | — | `cs.UpdateGlobal(data)` |
| `GetGlobalState(key state.StateKey)` | `(any, error)` | `cs.GetGlobal(key)` |
| `DumpState()` | `map[string]any` | `cs.Dump()` |

### 4.3 追踪方法（⤵️ 5.11 回填）

| 方法 | 签名 | 委托目标 |
|------|------|---------|
| `Trace(ctx context.Context, data map[string]any)` | `error` | `TracerWorkflowUtils.Trace()` |
| `TraceError(ctx context.Context, err error)` | `error` | `TracerWorkflowUtils.TraceError()` |

桩实现：检查 `inner.SkipTrace()` 为 true 则直接返回 nil，否则返回 nil（待回填）。

### 4.4 交互方法（⤵️ 5.7 回填）

| 方法 | 签名 | 委托目标 |
|------|------|---------|
| `Interact(ctx context.Context, value any)` | `(any, error)` | `WorkflowInteraction.WaitUserInputs()` |

桩实现：
- `streamMode == true` 时返回错误（与 Python 一致：流式模式不支持交互）
- 否则返回 `(nil, nil)`（待回填）

### 4.5 流写入方法（⤵️ 5.10 回填）

| 方法 | 签名 | 委托目标 |
|------|------|---------|
| `WriteStream(ctx context.Context, data any)` | `error` | `StreamWriterManager.GetOutputWriter().Write()` |
| `WriteCustomStream(ctx context.Context, data any)` | `error` | `StreamWriterManager.GetCustomWriter().Write()` |

桩实现：返回 nil（待回填）。

### 4.6 环境/配置方法（⤵️ 5.12 回填）

| 方法 | 签名 | 委托目标 |
|------|------|---------|
| `GetEnv(key string)` | `any` | `inner.Config().GetEnv(key)` |
| `GetNodeConfig()` | `any` | `inner.NodeConfig()` |

桩实现：返回 nil（待回填）。

## 5. 伴随修改

### 5.1 agent.go — GetState 参数类型修改

```go
// 修改前
func (s *Session) GetState(key string) any

// 修改后
func (s *Session) GetState(key state.StateKey) (any, error)
```

委托目标不变：`coll.GetGlobal(key)`。

### 5.2 workflow.go — 删除 Python 没有的状态方法

删除以下 4 个方法：
- `State()` — Python Workflow Session 不暴露状态
- `UpdateState(data map[string]any)` — Python Workflow Session 不暴露状态
- `GetState(key state.StateKey)` — Python Workflow Session 不暴露状态
- `DumpState()` — Python Workflow Session 不暴露状态

保留：
- `Close()` — Go 资源管理惯用模式
- `Inner()` — 内部层交互通道

### 5.3 doc.go — 更新文件目录

在 `session/doc.go` 的文件目录中添加 `node.go` 条目。

### 5.4 测试文件同步修改

- `session/agent_test.go` — `GetState` 调用处改用 `state.StringKey()`
- `session/workflow_test.go` — 移除已删除方法的测试用例
- 新建 `session/node_test.go` — NodeSessionFacade 全量测试

## 6. 回填计划

| 回填标记 | 目标步骤 | 内容 |
|---------|---------|------|
| `⤵️ 5.7` | Interaction | `interaction` 字段类型 `any → WorkflowInteraction`；`Interact()` 真实逻辑 |
| `⤵️ 5.10` | StreamWriter | `WriteStream()`/`WriteCustomStream()` 真实逻辑 |
| `⤵️ 5.11` | Tracer | `Trace()`/`TraceError()` 真实逻辑 |
| `⤵️ 5.12` | Config | `GetEnv()`/`GetNodeConfig()` 真实逻辑 |
