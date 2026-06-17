# 5.8 Checkpointer 接口 + 工厂设计

## 概述

5.8 的目标是实现 Checkpointer 接口 + 工厂（`CheckpointerFactory`），以及 InMemory 实现。同时回填之前各步骤（5.1/5.2/5.3/5.4/5.5/5.7）中预留的 Checkpointer 相关代码（`⤵️ 5.8` 标记）。

对应 Python 代码：
- `openjiuwen/core/session/checkpointer/base.py` — Checkpointer/Storage 抽象类
- `openjiuwen/core/session/checkpointer/checkpointer.py` — CheckpointerFactory/Provider/Config
- `openjiuwen/core/session/checkpointer/inmemory.py` — InMemoryCheckpointer + 3 个 Storage
- `openjiuwen/core/session/checkpointer/persistence.py` — PersistenceCheckpointer（5.9 实现）

## 设计决策

| # | 决策点 | 选择 | 理由 |
|---|--------|------|------|
| 1 | 包结构 | `session/checkpointer/` | 与 Python 结构一致 |
| 2 | Graph Store 依赖 | InMemory 先不实现 | 8.7 才实现 Graph Store，避免引入临时类型 |
| 3 | Checkpointer 方法签名 | `CheckpointerSession` 最小接口 | 类型安全，避免循环导入，类似 interaction 模式 |
| 4 | AgentID/TeamID | 不在接口中，用类型断言 | 与 Python `hasattr` 语义一致，参考 interaction 的 ExecutableIDProvider |
| 5 | 同步/异步 | 全部加 `ctx context.Context` | 为 5.9 Redis 预留，接口一次定义好 |
| 6 | Storage 层 | 保留 Storage 接口 | 与 Python 一致，三种实现复用 Storage 抽象 |
| 7 | 序列化 | Serializer 接口 + 默认 JSON | 与 Python 的 Serializer 抽象对应，5.9 复用无重构 |
| 8 | State 接口 | `state.SessionState` + 类型断言 | Python 也没有统一接口，AgentStateCollection/WorkflowCommitState 方法集不同；WorkflowState 独立接口已拆分，WorkflowStorage 断言 `state.WorkflowState` 接口而非具体类型 |

## 包结构与文件目录

```
internal/agentcore/session/checkpointer/
├── doc.go              # 包文档
├── base.go             # Checkpointer 接口、Storage 接口、命名空间常量、Key 构建函数、辅助接口
├── serializer.go       # Serializer 接口、JSONSerializer 实现
├── inmemory.go         # InMemoryCheckpointer、AgentStorage、AgentTeamStorage、WorkflowStorage
├── factory.go          # CheckpointerFactory、CheckpointerProvider、CheckpointerConfig
├── base_test.go        # 命名空间常量、Key 构建函数、辅助接口测试
├── serializer_test.go  # Serializer 测试
├── inmemory_test.go    # InMemoryCheckpointer 测试
└── factory_test.go     # CheckpointerFactory 测试
```

## 核心接口定义（base.go）

### Checkpointer 接口

```go
// Checkpointer 检查点器接口，定义会话状态持久化的生命周期钩子。
// 对应 Python: openjiuwen/core/session/checkpointer/base.py (Checkpointer)
type Checkpointer interface {
    GetThreadID(session CheckpointerSession) string
    PreWorkflowExecute(ctx context.Context, session CheckpointerSession, inputs any) error
    PostWorkflowExecute(ctx context.Context, session CheckpointerSession, result any, exception error) error
    PreAgentExecute(ctx context.Context, session CheckpointerSession, inputs any) error
    PreAgentTeamExecute(ctx context.Context, session CheckpointerSession, inputs any) error
    InterruptAgentExecute(ctx context.Context, session CheckpointerSession) error
    PostAgentExecute(ctx context.Context, session CheckpointerSession) error
    PostAgentTeamExecute(ctx context.Context, session CheckpointerSession) error
    SessionExists(ctx context.Context, sessionID string) (bool, error)
    Release(ctx context.Context, sessionID string) error
    // ⤵️ 8.7 回填：返回 Store 实例
    GraphStore() any
}
```

### Storage 接口

```go
// Storage 状态存储接口，负责单个实体的状态保存/恢复/清除。
// 对应 Python: openjiuwen/core/session/checkpointer/base.py (Storage)
type Storage interface {
    Save(ctx context.Context, session CheckpointerSession) error
    Recover(ctx context.Context, session CheckpointerSession, inputs any) error
    Clear(ctx context.Context, entityID string) error
    Exists(ctx context.Context, session CheckpointerSession) (bool, error)
}
```

### CheckpointerSession 最小接口

```go
// CheckpointerSession Checkpointer 所需的会话最小接口。
// AgentSession/WorkflowSession/NodeSession 天然满足此接口。
// AgentID/TeamID 通过 AgentIDProvider/TeamIDProvider 类型断言获取。
// WorkflowState 的扩展方法（GetUpdates/SetUpdates/Commit 等）通过
// 类型断言为 state.WorkflowState 接口获取（比断言具体类型更优雅）。
type CheckpointerSession interface {
    SessionID() string
    WorkflowID() string
    State() state.SessionState
    Config() CheckpointerConfig
    Parent() CheckpointerSession
}

// CheckpointerConfig Checkpointer 所需的配置最小接口。
type CheckpointerConfig interface {
    GetEnv(key string, defaultValue ...any) any
}
```

### 断言辅助接口

```go
// AgentIDProvider 提供 Agent ID（通过类型断言获取）。
// AgentSession 天然满足此接口。
type AgentIDProvider interface {
    AgentID() string
}

// TeamIDProvider 提供 Team ID（通过类型断言获取）。
// AgentTeamSession 天然满足此接口。
type TeamIDProvider interface {
    TeamID() string
}
```

### 命名空间常量和 Key 构建函数

```go
const (
    // SessionNamespaceAgent Agent 状态命名空间
    SessionNamespaceAgent = "agent"
    // SessionNamespaceAgentTeam AgentTeam 状态命名空间
    SessionNamespaceAgentTeam = "agent-team"
    // SessionNamespaceWorkflow Workflow 状态命名空间
    SessionNamespaceWorkflow = "workflow"
    // WorkflowNamespaceGraph Workflow 图状态命名空间
    WorkflowNamespaceGraph = "workflow-graph"
)

// BuildKey 用 ":" 连接各部分，构建存储键
func BuildKey(parts ...string) string

// BuildKeyWithNamespace 构建带命名空间的存储键：session:namespace:entity:suffixes
func BuildKeyWithNamespace(sessionID, namespace, entityID string, suffixes ...string) string

// GetAgentID 类型断言获取 Agent ID，不存在返回空字符串
func GetAgentID(session CheckpointerSession) string

// GetTeamID 类型断言获取 Team ID，不存在返回空字符串
func GetTeamID(session CheckpointerSession) string
```

## Serializer 接口（serializer.go）

```go
// Serializer 类型化序列化器接口。
// 对应 Python: openjiuwen/core/graph/store/serde.py (Serializer)
type Serializer interface {
    // DumpsTyped 序列化对象，返回 (格式标签, 字节流)
    DumpsTyped(obj any) (string, []byte, error)
    // LoadsTyped 反序列化对象
    LoadsTyped(formatTag string, data []byte) (any, error)
}

// serdeTuple 序列化元组，对应 Python 的 tuple[str, bytes]
type serdeTuple struct {
    FormatTag string
    Data      []byte
}

// JSONSerializer JSON 序列化器实现
type JSONSerializer struct{}

func NewJSONSerializer() *JSONSerializer
func (s *JSONSerializer) DumpsTyped(obj any) (string, []byte, error)
func (s *JSONSerializer) LoadsTyped(formatTag string, data []byte) (any, error)
```

- `DumpsTyped` 返回 `("json", jsonBytes, nil)`
- `LoadsTyped` 仅处理格式标签为 `"json"` 的数据，其他返回 nil
- 5.9 可添加 `GobSerializer`，格式标签为 `"gob"`

## InMemoryCheckpointer（inmemory.go）

### InMemoryCheckpointer 结构

```go
type InMemoryCheckpointer struct {
    agentStores          map[string]*AgentStorage          // session_id → AgentStorage
    agentTeamStores      map[string]*AgentTeamStorage      // session_id → AgentTeamStorage
    workflowStores       map[string]*WorkflowStorage        // session_id → WorkflowStorage
    sessionToWorkflowIDs map[string]map[string]bool        // session_id → workflow_id 集合
    graphStore           any                                // ⤵️ 8.7 回填
}
```

### InMemoryCheckpointer 方法行为

| 方法 | 行为 |
|------|------|
| `PreWorkflowExecute` | 有 InteractiveInput → recover；无输入但状态存在 → 强制删除或报错；新会话 → 创建 WorkflowStorage |
| `PostWorkflowExecute` | 异常 → save 后 re-raise；正常完成 → clear 全部（graph + workflow）；中断 → save |
| `PreAgentExecute` | 创建/获取 AgentStorage → recover → 设置 InteractiveInput |
| `PreAgentTeamExecute` | 创建/获取 AgentTeamStorage → recover → 设置 InteractiveInput |
| `InterruptAgentExecute` | AgentStorage.save() |
| `PostAgentExecute` | AgentStorage.save() |
| `PostAgentTeamExecute` | AgentTeamStorage.save() |
| `SessionExists` | 检查三个 stores 是否包含 sessionID |
| `Release` | agent_id 非空 → 清除单个 agent；否则 → 清除全部（graph + workflow + agent + team） |
| `GraphStore` | 返回 graphStore（当前为 nil，⤵️ 8.7 回填） |

内部辅助方法：
- `innerSaveWorkflowCheckpoint` — 保存工作流检查点
- `innerClearWorkflowSession` — 清除工作流会话

### 三个 InMemory Storage

#### baseSingleStateStorage

```go
// baseSingleStateStorage 单实体状态存储基类
type baseSingleStateStorage struct {
    stateBlobs map[string]serdeTuple  // entity_id → (formatTag, bytes)
    serde      Serializer
}
```

提供 Save/Recover/Clear/Exists 的通用实现，子类只需实现三个抽象方法：
- `GetEntityID(session) string`
- `GetStateToSave(session) map[string]any`
- `RestoreState(session, state)`

#### AgentStorage

```go
type AgentStorage struct {
    baseSingleStateStorage
}
```

- `GetEntityID` → `GetAgentID(session)` 类型断言
- `GetStateToSave` → `session.State().GetState()` （SessionState 接口自带）
- `RestoreState` → `session.State().SetState(state)` （SessionState 接口自带）

#### AgentTeamStorage

```go
type AgentTeamStorage struct {
    baseSingleStateStorage
}
```

- `GetEntityID` → `GetTeamID(session)` 类型断言
- `GetStateToSave` → 断言 `*state.AgentStateCollection` 调用 `GetGlobal(state.AllStateKey)`
- `RestoreState` → 断言 `*state.AgentStateCollection` 调用 `GlobalState().SetState(state)`（GlobalState 返回 `*InMemoryStateLike`）

#### WorkflowStorage（独立，不嵌入 baseSingleStateStorage）

```go
type WorkflowStorage struct {
    serde              Serializer
    stateBlobs         map[string]serdeTuple  // workflow_id → (formatTag, bytes)
    stateUpdatesBlobs  map[string]serdeTuple  // workflow_id → (formatTag, bytes)
}
```

- `Save` → 断言 `state.WorkflowState`，调用 `GetState()` + `GetUpdates()`
- `Recover` → 断言 `state.WorkflowState`，调用 `SetState()` + 处理 InteractiveInput + `SetUpdates()`
- InteractiveInput 处理需要调用 `UpdateAndCommitWorkflowState` / `Commit` / `Update`
- 注意：断言 `state.WorkflowState` 接口而非 `*state.WorkflowCommitState` 具体类型，更解耦

## CheckpointerFactory + Config（factory.go）

### CheckpointerConfig

```go
type CheckpointerConfig struct {
    Type string            // "in_memory" / "persistence" / "redis"
    Conf map[string]any    // 类型特定配置
}
```

### CheckpointerProvider

```go
type CheckpointerProvider interface {
    Create(ctx context.Context, conf map[string]any) (Checkpointer, error)
}
```

### CheckpointerFactory

```go
type CheckpointerFactory struct {
    registry            map[string]CheckpointerProvider  // name → Provider
    defaultCheckpointer Checkpointer
    typeCheckpointers   map[string]Checkpointer          // store_type → 实例
}
```

Factory 方法：
- `NewCheckpointerFactory()` — 创建实例，自动注册 `"in_memory"` Provider
- `Register(name, provider)` — 注册 Provider
- `Create(ctx, config)` — 根据 CheckpointerConfig 创建实例
- `SetDefaultCheckpointer(cp)` — 设置默认实例
- `SetCheckpointer(storeType, cp)` — 按类型设置实例
- `GetCheckpointer(storeType ...string)` — 获取（优先级：type缓存 → 默认 → InMemory 单例）

全局便捷函数（包级 `defaultFactory` 单例）：
- `GetCheckpointer(storeType ...string) Checkpointer`
- `SetDefaultCheckpointer(cp Checkpointer)`
- `SetCheckpointer(storeType string, cp Checkpointer)`
- `CreateCheckpointer(ctx, config) (Checkpointer, error)`
- `RegisterCheckpointerProvider(name string, provider CheckpointerProvider)`

## 回填计划

### 1. session/session.go

| 位置 | 当前 | 回填后 |
|------|------|--------|
| `BaseSession.Checkpointer() any` | 返回 `any`，标注 ⤵️ | 返回 `checkpointer.Checkpointer` |
| `ProxySession.Checkpointer() any` | 返回 `any` | 返回 `checkpointer.Checkpointer` |

### 2. session/internal/agent_session.go

| 位置 | 当前 | 回填后 |
|------|------|--------|
| `AgentSession.checkpointer any` | 字段类型 `any` | 改为 `checkpointer.Checkpointer` |
| `AgentSession.Checkpointer() any` | 返回 `any` | 返回 `checkpointer.Checkpointer` |
| `WithCheckpointer(cp any)` | 参数 `any` | 改为 `checkpointer.Checkpointer` |

### 3. session/internal/workflow_session.go

| 位置 | 当前 | 回填后 |
|------|------|--------|
| `internal.baseSession.Checkpointer() any` | 返回 `any` | 返回 `checkpointer.Checkpointer` |
| `WorkflowSession.Checkpointer()` | 无 parent 返回 nil | 无 parent 从工厂获取 `checkpointer.GetCheckpointer()` |
| `NodeSession.Checkpointer() any` | 返回 `any` | 返回 `checkpointer.Checkpointer` |

### 4. session/interaction/base.go

| 位置 | 当前 | 回填后 |
|------|------|--------|
| `baseSession.Checkpointer() any` | 返回 `any` | 返回 `checkpointer.Checkpointer` |
| `InteractionCheckpointer` 接口 | 独立接口，标注"5.8 后迁移" | 删除此接口，`interruptAgentExecute` 改用 `checkpointer.Checkpointer` 直接调用 `InterruptAgentExecute(ctx, session)` |

### 5. session/interaction/base_test.go

- `fakeBaseSession.Checkpointer()` 返回类型改为 `checkpointer.Checkpointer`
- `fakeSessionWithoutExecID.Checkpointer()` 同上

### 6. session/interaction/interaction_test.go

- `fakeCheckpointer` 改为实现 `checkpointer.Checkpointer`（所有方法的桩实现）

### 7. session/agent.go

| 位置 | 当前 | 回填后 |
|------|------|--------|
| `PreRun` 中 `⤵️ 5.8` | 跳过 | 调用 `inner.Checkpointer().PreAgentExecute(ctx, inner, inputs)` |
| `Commit` 中 `⤵️ 5.8` | 跳过 | 调用 `inner.Checkpointer().PostAgentExecute(ctx, inner)` |

### 8. session/session_test.go

- `mockStub.Checkpointer()` 返回类型改为 `checkpointer.Checkpointer`
- 测试断言适配新类型

### 9. session/internal/agent_session_test.go

- `WithCheckpointer("my-cp")` 改为传 `checkpointer.Checkpointer` mock

### 10. session/internal/workflow_session_test.go

- `WithCheckpointer("parent_checkpointer")` 改为传 `checkpointer.Checkpointer` mock

### 11. session/doc.go

- 更新 Checkpointer 返回类型说明

### IMPLEMENTATION_PLAN.md 更新

- 5.8 状态改为 ✅
- 5.1/5.2/5.3/5.4/5.5/5.7 中 `⤵️ 5.8` 标记改为 ✅

## 日志规则

按项目规则 3，在 Go 代码中等价位置补充日志。Python 中 Checkpointer 的日志调用点：

| Python 日志点 | Go 等价位置 | 日志内容 |
|--------------|-----------|---------|
| `pre_workflow_execute` 创建新 store | `PreWorkflowExecute` | session_id, workflow_id, storage_type |
| `pre_workflow_execute` restore | `PreWorkflowExecute` | CHECKPOINT_RESTORE |
| `pre_workflow_execute` force clear | `PreWorkflowExecute` | CHECKPOINT_CLEAR, session_id, workflow_id |
| `post_workflow_execute` save | `PostWorkflowExecute` | CHECKPOINT_SAVE, session_id, workflow_id |
| `post_workflow_execute` clear | `innerClearWorkflowSession` | CHECKPOINT_CLEAR |
| `pre_agent_execute` create/restore | `PreAgentExecute` | session_id, agent_id |
| `interrupt_agent_execute` save | `InterruptAgentExecute` | CHECKPOINT_SAVE, session_id, agent_id |
| `post_agent_execute` save | `PostAgentExecute` | CHECKPOINT_SAVE, session_id, agent_id |
| `pre_agent_team_execute` create/restore | `PreAgentTeamExecute` | session_id, team_id |
| `post_agent_team_execute` save | `PostAgentTeamExecute` | CHECKPOINT_SAVE, session_id, team_id |
| `release` clear | `Release` | CHECKPOINT_CLEAR, session_id |

组件常量使用 `logger.ComponentAgentCore`。
