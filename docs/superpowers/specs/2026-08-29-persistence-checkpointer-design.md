# 5.9 PersistenceCheckpointer 设计

## 概述

5.9 的目标是实现 `PersistenceCheckpointer`，基于 `BaseKVStore` 接口的持久化检查点器。InMemory 版将状态存储在进程内存的 map 中，Persistence 版将状态序列化后存储到 KVStore（默认 SQLite），支持进程重启后恢复。

对应 Python 代码：
- `openjiuwen/core/session/checkpointer/persistence.py` — PersistenceCheckpointer + BaseStorage + BaseSingleStateStorage + AgentStorage/AgentTeamStorage/WorkflowStorage + GraphStore + PersistenceCheckpointerProvider

## 设计决策

| # | 决策点 | 选择 | 理由 |
|---|--------|------|------|
| 1 | 钩子注入方式 | 单接口三方法（EntityHooks） | 对齐 Python 模板方法模式，1:1 映射 _get_entity_id/_get_state_to_save/_restore_state |
| 2 | Provider 后端支持 | 只支持 sqlite（对齐 Python） | 其他后端通过直接传入 BaseKVStore 实例使用 |
| 3 | Pipeline 使用 | 完全对齐 Python，所有操作走 Pipeline | save/recover/exists/clear 都用 Pipeline batch 执行 |
| 4 | 序列化器 | 统一用 JSONSerializer | 与 InMemory 版一致，简单统一 |
| 5 | Storage.Clear 签名 | `Clear(ctx, entityID, sessionID)` | Persistence 版需要 sessionID 构建 KV key；InMemory 版忽略 sessionID |
| 6 | GraphStore | 5.9 跳过，等 8.7 | GraphStore 是独立子系统，不影响 PersistenceCheckpointer 核心功能 |
| 7 | 文件组织 | 单文件 persistence.go | 与 inmemory.go 对称 |
| 8 | InMemory 版回填 | 不需要 | 已决策：InMemory 和 Persistence 是两套独立 Storage 体系 |

## 已有代码修改

### 1. base.go — Storage.Clear 签名扩展

```go
// 修改前
Clear(ctx context.Context, entityID string) error

// 修改后
Clear(ctx context.Context, entityID, sessionID string) error
```

### 2. inmemory.go — 适配 Clear 签名

三个 Storage 的 Clear 方法签名统一改为 `Clear(ctx, entityID, sessionID)`，内部忽略 sessionID 参数。

### 3. factory.go — 注册 persistence Provider

在 `NewCheckpointerFactory()` 中注册 `"persistence"` Provider：
```go
f.Register("persistence", &persistenceProvider{})
```

### 4. doc.go — 文件目录添加 persistence.go

```
├── persistence.go      # PersistenceCheckpointer、持久化 Storage 实现、Provider
```

## 核心结构体设计

### EntityHooks 接口

模拟 Python BaseSingleStateStorage 的三个钩子方法：

```go
// EntityHooks 单实体状态存储的钩子接口。
// 对应 Python: BaseSingleStateStorage 的 _get_entity_id/_get_state_to_save/_restore_state
// Go 不支持虚方法分派，通过接口注入实现模板方法模式。
type EntityHooks interface {
    // GetEntityID 获取实体 ID（Agent 返回 agentID，AgentTeam 返回 teamID）
    GetEntityID(session CheckpointerSession) string
    // GetStateToSave 获取需要保存的状态
    GetStateToSave(session CheckpointerSession) any
    // RestoreState 将恢复的状态设置回 session
    RestoreState(session CheckpointerSession, state any)
}
```

### basePersistenceStorage（骨架 + 钩子注入）

```go
// basePersistenceStorage 持久化单实体状态存储基类。
// 持有 BaseKVStore + Serializer + EntityHooks，
// 通过 EntityHooks 注入实现 Python 模板方法模式。
// Save/Recover/Clear/Exists 为固定骨架，通过 hooks 调用子类逻辑。
//
// 对应 Python: persistence.py (BaseSingleStateStorage)
type basePersistenceStorage struct {
    // kvStore KV 存储后端
    kvStore kv.BaseKVStore
    // serde 序列化器
    serde Serializer
    // hooks 实体钩子（注入点）
    hooks EntityHooks
    // namespace 命名空间（"agent" / "agent-team"）
    namespace string
    // entityLabel 实体标签（"agent" / "agent_team"），用于日志
    entityLabel string
    // stateBlobsKey 状态数据键后缀
    stateBlobsKey string
    // stateDumpTypeKey 状态类型键后缀
    stateDumpTypeKey string
}
```

骨架方法实现：

| 方法 | 行为 |
|------|------|
| `Save` | hooks.GetStateToSave → serde.DumpsTyped → Pipeline Set(dumpType, blob) → Execute |
| `Recover` | Pipeline Get(dumpType, blob) → Execute → 解析 PipelineResult → serde.LoadsTyped → hooks.RestoreState |
| `Clear` | BuildKeyWithNamespace 构建 2 个 key → kvStore.BatchDelete |
| `Exists` | Pipeline Exists(dumpType, blob) → Execute → 两个都为 true |

辅助方法：

| 方法 | 用途 |
|------|------|
| `buildStateKeys(sessionID, entityID)` | 构建 (dumpTypeKey, blobKey)，对应 Python `_build_state_keys` |
| `logKwargs(sessionID, entityID, operation)` | 构建日志字段，对应 Python `_log_kwargs` |
| `entityLogExtra(entityID)` | 构建实体日志字段（agent_id 或 workflow_id） |
| `serializeState(state)` | 序列化状态，对应 Python `_serialize_state` |
| `deserializeState(dumpType, blob)` | 反序列化状态，对应 Python `_deserialize_state` |
| `decodeDumpType(dumpType)` | 解码 dump type，对应 Python `_decode_dump_type` |

### PersistenceAgentStorage

```go
// PersistenceAgentStorage Agent 持久化状态存储。
// 对应 Python: persistence.py (AgentStorage)
type PersistenceAgentStorage struct {
    basePersistenceStorage
}
```

注入 Agent 版 EntityHooks：
- `GetEntityID` → `GetAgentID(session)`
- `GetStateToSave` → `session.State().GetState()`
- `RestoreState` → `session.State().SetState(state)`（类型断言 map[string]any）

常量：
- namespace = `SessionNamespaceAgent` ("agent")
- entityLabel = "agent"
- stateBlobsKey = "agent_state_blobs"
- stateDumpTypeKey = "agent_state_blobs_dump_type"

### PersistenceAgentTeamStorage

```go
// PersistenceAgentTeamStorage AgentTeam 持久化状态存储。
// 对应 Python: persistence.py (AgentTeamStorage)
type PersistenceAgentTeamStorage struct {
    basePersistenceStorage
}
```

注入 Team 版 EntityHooks：
- `GetEntityID` → `GetTeamID(session)`
- `GetStateToSave` → 断言 `*state.AgentStateCollection` 调用 `GetState()`
- `RestoreState` → 断言 `*state.AgentStateCollection` 调用 `SetState(state)`

常量：
- namespace = `SessionNamespaceAgentTeam` ("agent-team")
- entityLabel = "agent_team"
- stateBlobsKey = "agent_team_state_blobs"
- stateDumpTypeKey = "agent_team_state_blobs_dump_type"

### PersistenceWorkflowStorage

独立结构体（不嵌入 basePersistenceStorage），因为需要同时管理 state + updates 共 4 个 key：

```go
// PersistenceWorkflowStorage Workflow 持久化状态存储。
// 独立于 basePersistenceStorage，因为需要同时保存 state + updates 两类数据（4 个 key）。
// 对应 Python: persistence.py (WorkflowStorage)
type PersistenceWorkflowStorage struct {
    kvStore kv.BaseKVStore
    serde   Serializer
}
```

常量（类级）：
- stateBlobs = "workflow_state_blobs"
- stateBlobsDumpType = "workflow_state_blobs_dump_type"
- updateBlobs = "workflow_update_blobs"
- updateBlobsDumpType = "workflow_update_blobs_dump_type"
- keyNums = 4

方法行为：

| 方法 | 行为 |
|------|------|
| `Save` | 获取 state → 序列化 → Pipeline Set 2 key；获取 updates → 序列化 → Pipeline Set 2 key；Execute |
| `Recover` | Pipeline Get 4 key → Execute → 解析 → 反序列化 state → SetState；处理 InteractiveInput；反序列化 updates → SetUpdates |
| `Clear` | BatchDelete 4 个 key |
| `Exists` | Pipeline Exists 4 key → Execute → state 两个 key 都为 true |

### PersistenceCheckpointer

```go
// PersistenceCheckpointer 持久化检查点器，所有状态存储在 BaseKVStore 中。
// 对应 Python: persistence.py (PersistenceCheckpointer)
type PersistenceCheckpointer struct {
    kvStore          kv.BaseKVStore
    agentStorage     *PersistenceAgentStorage
    agentTeamStorage *PersistenceAgentTeamStorage
    workflowStorage  *PersistenceWorkflowStorage
    graphStore       any  // ⤵️ 8.7 回填
}
```

构造函数：
```go
func NewPersistenceCheckpointer(kvStore kv.BaseKVStore) *PersistenceCheckpointer
```

方法行为（对齐 Python PersistenceCheckpointer）：

| 方法 | 行为 |
|------|------|
| `GetThreadID` | 委托 `GetThreadID(session)` |
| `PreAgentExecute` | agentStorage.Recover → 设置 InteractiveInput |
| `PreAgentTeamExecute` | agentTeamStorage.Recover → 设置 InteractiveInput |
| `InterruptAgentExecute` | agentStorage.Save |
| `PostAgentExecute` | agentStorage.Save |
| `PostAgentTeamExecute` | agentTeamStorage.Save |
| `PreWorkflowExecute` | InteractiveInput → workflowStorage.Recover；否则 → exists 检查 → force delete 或报错 |
| `PostWorkflowExecute` | 异常 → save + re-raise；中断 → save；正常完成 → graph delete + clear |
| `SessionExists` | kvStore.GetByPrefix(sessionID+":") 检查是否有 key |
| `Release` | agentID 非空 → agentStorage.Clear；否则 → kvStore.DeleteByPrefix(sessionID+":") |
| `GraphStore` | 返回 graphStore（当前为 nil，⤵️ 8.7 回填） |

### persistenceProvider

```go
// persistenceProvider Persistence 检查点器提供者。
// 对应 Python: PersistenceCheckpointerProvider
type persistenceProvider struct{}
```

Create 方法行为（对齐 Python）：
- conf["db_type"] 默认 "sqlite"
- conf["db_path"] 默认 "checkpointer"
- 创建 DbBasedKVStore（SQLite）→ NewPersistenceCheckpointer

注册：`type="persistence"`

## Pipeline 使用详解

### Pipeline 结果解析辅助方法

```go
// pipelineGetResult 从 Pipeline 结果中获取 Get 操作的值
func pipelineGetResult(results []kv.PipelineResult, idx int) ([]byte, error)

// pipelineExistsResult 从 Pipeline 结果中获取 Exists 操作的布尔值
func pipelineExistsResult(results []kv.PipelineResult, idx int) (bool, error)
```

### basePersistenceStorage 的 Pipeline 操作

#### Save 流程
```
state = hooks.GetStateToSave(session)
formatTag, data = serde.DumpsTyped(state)
dumpTypeKey, blobKey = buildStateKeys(sessionID, entityID)
pipeline = kvStore.Pipeline(ctx)
pipeline.Set(ctx, dumpTypeKey, []byte(formatTag), 0)
pipeline.Set(ctx, blobKey, data, 0)
results, err = pipeline.Execute(ctx)
```

#### Recover 流程
```
dumpTypeKey, blobKey = buildStateKeys(sessionID, entityID)
pipeline = kvStore.Pipeline(ctx)
pipeline.Get(ctx, dumpTypeKey)
pipeline.Get(ctx, blobKey)
results, err = pipeline.Execute(ctx)
dumpTypeBytes, _ = pipelineGetResult(results, 0)
blob, _ = pipelineGetResult(results, 1)
state = deserializeState(dumpTypeBytes, blob)
hooks.RestoreState(session, state)
```

#### Exists 流程
```
dumpTypeKey, blobKey = buildStateKeys(sessionID, entityID)
pipeline = kvStore.Pipeline(ctx)
pipeline.Exists(ctx, dumpTypeKey)
pipeline.Exists(ctx, blobKey)
results, err = pipeline.Execute(ctx)
bothExist = pipelineExistsResult(results, 0) && pipelineExistsResult(results, 1)
```

#### Clear 流程
```
dumpTypeKey, blobKey = buildStateKeys(sessionID, entityID)
kvStore.BatchDelete(ctx, []string{dumpTypeKey, blobKey}, 0)
```

### PersistenceWorkflowStorage 的 Pipeline 操作

#### Save 流程
```
state = session.State().GetState()
updates = commitState.GetUpdates()
pipeline = kvStore.Pipeline(ctx)
// state: Set dumpType + blob
// updates: Set dumpType + blob
pipeline.Execute(ctx)
```

#### Recover 流程
```
pipeline = kvStore.Pipeline(ctx)
pipeline.Get(ctx, stateDumpTypeKey)
pipeline.Get(ctx, stateBlobKey)
pipeline.Get(ctx, updateDumpTypeKey)
pipeline.Get(ctx, updateBlobKey)
results, err = pipeline.Execute(ctx)
// 解析 state → SetState
// 处理 InteractiveInput
// 解析 updates → SetUpdates
```

## KV Key 结构

所有 key 使用 `BuildKeyWithNamespace` 构建，格式为 `session:namespace:entity:suffix`：

| 存储类型 | Key 示例 |
|----------|---------|
| Agent dumpType | `{sessionID}:agent:{agentID}:agent_state_blobs_dump_type` |
| Agent blob | `{sessionID}:agent:{agentID}:agent_state_blobs` |
| AgentTeam dumpType | `{sessionID}:agent-team:{teamID}:agent_team_state_blobs_dump_type` |
| AgentTeam blob | `{sessionID}:agent-team:{teamID}:agent_team_state_blobs` |
| Workflow state dumpType | `{sessionID}:workflow:{workflowID}:workflow_state_blobs_dump_type` |
| Workflow state blob | `{sessionID}:workflow:{workflowID}:workflow_state_blobs` |
| Workflow updates dumpType | `{sessionID}:workflow:{workflowID}:workflow_update_blobs_dump_type` |
| Workflow updates blob | `{sessionID}:workflow:{workflowID}:workflow_update_blobs` |

## 日志规则

按项目规则 3，在 Go 代码中等价位置补充日志。Python 中 PersistenceCheckpointer 的日志调用点：

| Python 日志点 | Go 等价位置 | 日志字段 |
|--------------|-----------|---------|
| BaseSingleStateStorage.save 成功 | basePersistenceStorage.Save | session_id, agent_id/workflow_id, storage_type=persistence |
| BaseSingleStateStorage.save 序列化失败 | basePersistenceStorage.Save | session_id, agent_id/workflow_id, operation=serialize |
| BaseSingleStateStorage.save 写入失败 | basePersistenceStorage.Save | event_type=checkpoint_error, operation=save |
| BaseSingleStateStorage.recover 成功 | basePersistenceStorage.Recover | session_id, agent_id/workflow_id, storage_type=persistence |
| BaseSingleStateStorage.recover 无状态 | basePersistenceStorage.Recover | event_type=checkpoint_restore, storage_type=persistence |
| BaseSingleStateStorage.recover 设置失败 | basePersistenceStorage.Recover | event_type=checkpoint_error, operation=set_state |
| BaseSingleStateStorage.clear 成功 | basePersistenceStorage.Clear | session_id, agent_id/workflow_id, storage_type=persistence |
| BaseSingleStateStorage.exists | basePersistenceStorage.Exists | （Debug 级别） |
| PersistenceCheckpointer.pre_agent_execute | PreAgentExecute | event_type=checkpoint_restore, agent_id, storage_type=persistence |
| PersistenceCheckpointer.interrupt_agent_execute | InterruptAgentExecute | event_type=checkpoint_save, reason=interaction_required |
| PersistenceCheckpointer.post_agent_execute | PostAgentExecute | event_type=checkpoint_save, reason=agent_finished |
| PersistenceCheckpointer.pre_workflow_execute | PreWorkflowExecute | event_type=checkpoint_restore, workflow_id |
| PersistenceCheckpointer.post_workflow_execute 正常完成 | PostWorkflowExecute | event_type=checkpoint_clear, reason=workflow_completed |
| PersistenceCheckpointer.post_workflow_execute 中断 | PostWorkflowExecute | event_type=checkpoint_save, reason=interaction_required |
| PersistenceCheckpointer.release | Release | event_type=checkpoint_clear, storage_type=persistence |

组件常量使用 `logger.ComponentAgentCore`。

## 不做的事

- ❌ GraphStore 实现（等 8.7，PersistenceCheckpointer.graphStore 返回 nil）
- ❌ InMemory 版 Storage 回填（已决策：两套独立体系）
- ❌ shelve/file/redis Provider 后端（Provider 只支持 sqlite，其他通过直接构造）
- ❌ 自定义 Serializer 扩展（统一 JSONSerializer）
- ❌ WorkflowStorage._process_interactive_inputs 的完整 NodeSession 级别处理（当前简化处理：raw_inputs 直接 UpdateAndCommitWorkflowState，user_inputs 逐节点更新留待 Interaction 包完整后回填）

## 回填清单

### IMPLEMENTATION_PLAN.md 更新

- 5.9 状态改为 ✅
- 5.8 中 `⤵️ 8.7 回填 GraphStore` 标记不变

### doc.go 更新

- 文件目录添加 `persistence.go`
