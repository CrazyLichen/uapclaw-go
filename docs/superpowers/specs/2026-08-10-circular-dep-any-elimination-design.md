# 9.65-1 循环依赖重构设计：消除 `any`，恢复类型安全

## 背景

9.65-1 实现过程中，因 `schema → memory → tools → messager` 依赖链导致循环依赖，被迫在 `Messager` 接口和 `tools` 包中使用 `any` 代替 `schema.EventMessage`，引入了 `SenderIDStamper` 鸭子类型接口、`tools/events.go` 重复常量、`map[string]any` 载荷等妥协。本次重构通过搬移配置结构体到 `schema` 包打断循环链，逐步消除所有 `any` 妥协。

## 核心问题

循环依赖链：`schema → memory → tools → messager`

如果 `messager` import `schema`，形成 `schema → memory → tools → messager → schema`。
如果 `tools` import `schema`，形成 `schema → memory → tools → schema`。

**根因**：`schema` 包引用了 `memory.TeamMemoryConfig`（仅 1 处）和 `messager.MessagerTransportConfig`（2 处），这些是纯数据配置结构体，不应导致 schema 依赖整个 memory/messager 包。

## 依赖分析

### schema 当前对 memory/messager 的依赖

| 依赖 | 位置 | 用途 |
|------|------|------|
| `memory.TeamMemoryConfig` | `blueprint.go:94` | `TeamAgentSpec.Memory` 字段类型 |
| `messager.MessagerTransportConfig` | `blueprint.go:98` | `TransportBuilder` 返回类型 |
| `messager.MessagerTransportConfig` | `team.go:72` | `TeamRuntimeContext.MessagerConfig` 字段类型 |
| `messager.MessagerPeerConfig` | `base.go` 间接 | `MessagerTransportConfig` 内嵌 |
| `messager.NewMessagerTransportConfig()` | `blueprint.go:333,337` | 注册表默认构造 |

### schema 对 agent_teams 根包的依赖

| 依赖 | 位置 | 用途 |
|------|------|------|
| `SessionState` | `context.go` | session 状态管理 |
| `GetSessionID()` | `context.go` | 从 context 获取 session ID |
| `WithSessionState()` | `context.go` | 注入 SessionState 到 context |
| `agentteams.DefaultLeaderMemberName` | `blueprint.go` | 默认 Leader 名称 |
| `agentteams.ReservedMemberNames` | `blueprint.go` | 保留名校验 |
| `agentteams.T()` | `blueprint.go` | 国际化翻译 |

### 重构后依赖图

```
重构前：
  schema → memory → tools → messager    （循环链）
  schema → messager                      （直接依赖）
  schema → agent_teams 根包              （context.go）

重构后：
  schema（不再依赖 memory/messager/agent_teams 根包的 context 部分）
  messager → schema                      （新依赖，用于 EventMessage 类型）
  tools → schema                         （新依赖，用于 TypedEvent/GetSessionID）
  memory → tools → messager              （不变）
```

## 分步重构计划

### Step 1：搬配置到 schema，打断循环链

**目标**：schema 不再依赖 `memory` 和 `messager` 包，打断循环链。

**1.1 搬入 `MessagerTransportConfig` 相关**

从 `messager/base.go` 搬到 `schema/` 新文件（如 `schema/transport_config.go`）：

- `MessagerPeerConfig` 结构体
- `MessagerTransportConfig` 结构体
- `NewMessagerTransportConfig()` 函数
- `BroadcastTopic()` 方法

`messager` 包改为 import `schema`，内部使用 `schema.MessagerTransportConfig`。为避免循环，`messager` 包中：
- `MessagerTransportConfig` 改为 `schema.MessagerTransportConfig` 的类型别名，或直接删除本包定义
- `base.go` 中 `CreateMessager` 参数改为 `schema.MessagerTransportConfig`

**1.2 搬入 `TeamMemoryConfig` 相关**

从 `memory/config.go` 搬到 `schema/` 新文件（如 `schema/memory_config.go`）：

- `TeamMemoryConfig` 结构体（含 `EmbeddingConfig *embedding.EmbeddingConfig` 字段）
- `NewTeamMemoryConfig()` 函数

`memory` 包中：
- `config.go` 保留 `ResolveEmbeddingConfig` 函数（业务逻辑，依赖 agentcore）
- 删除 `TeamMemoryConfig` 结构体定义和 `NewTeamMemoryConfig()`
- `ResolveEmbeddingConfig` 参数改为 `*schema.TeamMemoryConfig`

**1.3 搬入 `SessionState` + context 相关**

从 `agent_teams/context.go` 搬到 `schema/` 新文件（如 `schema/session_context.go`）：

- `SessionState` 结构体
- `sessionStateKeyType` 类型
- `InitSessionState()` 函数
- `WithSessionState()` 函数
- `SessionStateFromCtx()` 函数
- `GetSessionID()` 全局函数
- `SessionState.GetSessionID()` 方法
- `SessionState.SetSessionID()` 方法

`agent_teams` 根包中：
- `context.go` 改为调用 `schema` 包的函数，或直接导出 `schema` 包的函数
- 其他文件（如 `agent/stream_controller.go`）中 `agentteams.GetSessionID(ctx)` 改为 `schema.GetSessionID(ctx)`

**1.4 更新所有 import 路径**

受影响的文件：
- `schema/blueprint.go`：删除 `memory` 和 `messager` import，配置类型已在同包
- `schema/team.go`：删除 `messager` import
- `schema/blueprint_test.go`：删除 `messager` import
- `messager/base.go`：import `schema`，参数类型改为 `schema.MessagerTransportConfig`
- `messager/inprocess.go`：`config` 字段类型改为 `schema.MessagerTransportConfig`
- `messager/base_test.go`：更新 import
- `memory/config.go`：import `schema`，`ResolveEmbeddingConfig` 参数改为 `*schema.TeamMemoryConfig`
- `memory/config_test.go`：更新 import
- `memory/manager.go` 等引用 `TeamMemoryConfig` 的文件：更新 import
- `agent/agent_configurator.go`：`memory.NewTeamMemoryConfig()` → `schema.NewTeamMemoryConfig()`
- `agent/payload.go`：`messager.MessagerTransportConfig` → `schema.MessagerTransportConfig`
- `agent_teams/context.go`：委托到 `schema` 包
- `agent_teams/context_test.go`：更新 import
- `agent/agent_configurator.go`：更新 import
- `agent/stream_controller.go`：更新 `GetSessionID` 调用

**验证**：编译通过 + 全部测试通过

---

### Step 2：Messager 接口改回 `schema.EventMessage`，删除 `SenderIDStamper`

**目标**：消除 `Messager` 接口中的 `any`，恢复类型安全。

**2.1 修改 `Messager` 接口**

```go
// 修改前
type MessagerHandler func(ctx context.Context, msg any) error
type Messager interface {
    Publish(ctx context.Context, topicID string, message any) error
    Send(ctx context.Context, agentID string, message any) error
    // ...
}

// 修改后
type MessagerHandler func(ctx context.Context, msg schema.EventMessage) error
type Messager interface {
    Publish(ctx context.Context, topicID string, message schema.EventMessage) error
    Send(ctx context.Context, agentID string, message schema.EventMessage) error
    // ...
}
```

**2.2 删除 `SenderIDStamper`**

- 删除 `SenderIDStamper` 接口定义
- 删除 `stampSenderID` 函数
- `InProcessMessager.Publish` 中直接操作 `message.SenderID`：
  ```go
  if message.SenderID == "" {
      message.SenderID = m.agentID()
  }
  ```
- 删除 `schema/events.go` 中的 `GetSenderID()`/`SetSenderID()` 方法

**2.3 更新测试**

- `inprocess_test.go`：删除 `testEventMessage`，改用 `schema.EventMessage`
- `schema/events_test.go`：删除 `GetSenderID`/`SetSenderID` 相关测试

**2.4 更新 `tools` 包调用方**

Step 2 改了 `Messager.Publish` 签名，`tools` 包中 `publishTaskEvent` 传 `map[string]any` 会编译失败，必须同步改为 `schema.EventMessage`：
- `publishTaskEvent` 改为接收 `schema.TypedEvent`，内部调用 `schema.EventMessageFromEvent(event)` 生成 `schema.EventMessage`
- `publishMessageEvent` 同理改为 `schema.EventMessage`
- `publishUnblockedEvents`、`maybePublishTaskListDrained` 同步适配

> 注意：此步 `tools/events.go` 仍保留（后续 Step 3 清理），但 `publishTaskEvent` 不再使用其中的常量，改为使用 `schema.TypedEvent` 具体事件结构体。

**验证**：编译通过 + 全部测试通过

---

### Step 3：tools 包清理

**目标**：消除 `tools/events.go` 重复常量和 `map[string]any` 载荷。

**3.1 删除 `tools/events.go`**

`tools/events.go` 中的事件类型常量和 topic builder 在 Step 2 已不再被 `publishTaskEvent` 使用（改用 `schema.TypedEvent`），此步彻底删除。

**3.2 在 schema 包中添加 TeamTopic 构建函数**

当前 `tools/events.go` 中的 `buildTopic`/`buildTaskTopic`/`buildMessageTopic` 需要对应到 schema 包。对齐 Python 的 `TeamTopic.build(session_id, team_name)` 在 schema 包中新增：

```go
// BuildTaskTopic 构建任务事件 topic。
// 对齐 Python: TeamTopic.build(session_id, team_name) + "task"
func BuildTaskTopic(sessionID, teamName string) string {
    return "session:" + sessionID + ":team:" + teamName + ":task"
}

// BuildMessageTopic 构建消息事件 topic。
func BuildMessageTopic(sessionID, teamName string) string {
    return "session:" + sessionID + ":team:" + teamName + ":message"
}
```

**3.3 重写 `publishTaskEvent`（Step 2 已部分完成，此步完善）**

Step 2 已将 `publishTaskEvent` 改为接收 `schema.TypedEvent`，此步完善：
- `topicID` 构建从 `buildTaskTopic` 改为 `schema.BuildTaskTopic`
- `publishMessageEvent` 中的 `buildMessageTopic` 改为 `schema.BuildMessageTopic`

**3.4 删除 `sessionID` 字段**

- `TeamTaskManager` 删除 `sessionID string` 字段
- `TeamMessageManager` 删除 `sessionID string` 字段
- 构造函数删除 `sessionID` 参数
- 内部调用 `schema.GetSessionID(ctx)` 获取 sessionID（Step 1 已搬入 schema）

**3.5 更新所有调用方**

- `agent/infra.go`、`agent/agent_configurator.go` 等构造 `TeamTaskManager`/`TeamMessageManager` 的地方删除 `sessionID` 参数

**验证**：编译通过 + 全部测试通过

---

### Step 4：MessageID 改用 UUID v4

**目标**：对齐 Python 的 `uuid.uuid4()`。

**4.1 修改 `TeamMessageManager`**

```go
// 修改前
messageID := fmt.Sprintf("msg_%s_%d_%d", tm.teamName, time.Now().UnixMilli(), time.Now().UnixNano()%1000)

// 修改后
messageID := uuid.New().String()
```

**4.2 更新 import**

- 删除 `"fmt"`（如果不再用于 MessageID）
- 添加 `"github.com/google/uuid"`

**4.3 更新测试**

- `message_manager_test.go` 中不再硬编码 MessageID 格式，改为校验非空和唯一性

**验证**：编译通过 + 全部测试通过

---

## 变更文件总览

| Step | 文件 | 操作 |
|------|------|------|
| 1 | `schema/transport_config.go` | 新增：搬入 MessagerTransportConfig 等 |
| 1 | `schema/memory_config.go` | 新增：搬入 TeamMemoryConfig 等 |
| 1 | `schema/session_context.go` | 新增：搬入 SessionState + context 函数 |
| 1 | `messager/base.go` | 修改：删除配置结构体，改用 schema |
| 1 | `messager/inprocess.go` | 修改：config 字段类型 |
| 1 | `memory/config.go` | 修改：删除 TeamMemoryConfig，改用 schema |
| 1 | `agent_teams/context.go` | 修改：委托到 schema |
| 1 | `schema/blueprint.go` | 修改：删除 memory/messager import |
| 1 | `schema/team.go` | 修改：删除 messager import |
| 1 | `agent/agent_configurator.go` | 修改：更新 import |
| 1 | `agent/payload.go` | 修改：更新 import |
| 2 | `messager/messager.go` | 修改：any → schema.EventMessage |
| 2 | `messager/inprocess.go` | 修改：删除 SenderIDStamper，直接操作 SenderID |
| 2 | `schema/events.go` | 修改：删除 GetSenderID/SetSenderID |
| 2 | `messager/inprocess_test.go` | 修改：删除 testEventMessage |
| 2 | `tools/task_manager.go` | 修改：publishTaskEvent 改用 schema.TypedEvent + EventMessageFromEvent |
| 2 | `tools/message_manager.go` | 修改：publishMessageEvent 改用 schema.EventMessage |
| 3 | `tools/events.go` | 删除 |
| 3 | `schema/team_topic.go` | 新增：BuildTaskTopic/BuildMessageTopic 函数 |
| 3 | `tools/task_manager.go` | 修改：删除 sessionID，用 schema.GetSessionID(ctx) |
| 3 | `tools/message_manager.go` | 修改：删除 sessionID，用 schema.GetSessionID(ctx) |
| 3 | `agent/infra.go` | 修改：删除 sessionID 参数 |
| 4 | `tools/message_manager.go` | 修改：UUID v4 |

## 风险与缓解

| 风险 | 缓解 |
|------|------|
| Step 1 搬移配置后 import 路径变更面大 | 机械性替换，每步编译验证 |
| schema 包体积增长 | 配置结构体是纯数据，无业务逻辑，符合 schema 包定位 |
| `TeamMemoryConfig` 搬入 schema 引入 agentcore 依赖 | schema 已依赖 agentcore 多个包（foundation/tool、single_agent/schema、session/stream），不是新问题 |
| Step 2 接口变更可能影响外部调用方 | 当前 Messager 仅在 agent_teams 内部使用，影响可控 |
