# 8.30 TeamRuntime 完整包设计

## 1. 概述

实现文档 8.30 步骤：TeamRuntime — 消息总线，P2P 通信。Python 参考路径：`openjiuwen/core/multi_agent/team_runtime/`。

**实现范围**：8.30 一步到位实现完整 team_runtime 包（包含 8.30-8.33 的所有内容），8.31-8.33 在实现计划中标记为已包含。同时补充 AgentTeamSession（内部层 + 公开层），并将 BaseTeam 接口迁移到 schema 包解决循环依赖。

## 2. 流程位置与作用

### 2.1 在多 Agent 团队分组中的位置

8.30 是**多 Agent 团队从"静态定义"进入"动态运行"的关键转折点**：

```
静态定义层（已完成 ✅）
  8.27 BaseTeam 接口  → 定义了 Send/Publish/Subscribe 等通信方法签名
  8.28 TeamCard/TeamConfig → 定义了团队元数据与配置
  8.29 EventDrivenTeamCard → 定义了事件驱动的团队卡片（含订阅映射）

── 分界线 ──

动态运行层（未开始 ☐）
  8.30 TeamRuntime → 【当前步骤】消息总线 + P2P 通信的运行时引擎
  8.31 CommunicableAgent → 依赖 8.30，可通信 Agent 包装
  8.32 MessageRouter / SubscriptionManager → 依赖 8.30，消息路由与订阅匹配
  8.33 MessageBus → 依赖 8.30，消息总线核心
  8.34-8.36 上层团队模式 → 依赖 8.30-8.33 的完整通信基础设施
```

### 2.2 TeamRuntime 的核心作用

TeamRuntime 是整个多 Agent 通信系统的编排入口，职责包括：

1. **Agent 注册表**：管理已注册的 Agent 卡片（`agentCards`），是 Agent 存在性的权威来源
2. **生命周期管理**：启动/停止消息总线，支持惰性启动
3. **P2P 通信**：点对点消息发送（请求-响应模式），支持超时
4. **Pub-Sub 通信**：发布/订阅消息（发后即忘模式），支持主题订阅
5. **会话管理**：绑定/解绑团队会话，提供会话级消息隔离
6. **Provider 包装**：自动将 CommunicableAgent 实例绑定到 runtime，使其可直接调用通信方法

## 3. 关键设计决策记录

### 3.1 实现范围：完整 team_runtime 包（8.30-8.33 一步到位）

**决策**：8.30 一步到位实现全部 6 个 Python 文件的等价内容，8.31-8.33 标记为已包含。

**原因**：Python 中 team_runtime 包的 6 个组件紧密耦合，拆分实现会导致大量 stub 占位，增加复杂度但无实际收益。

### 3.2 CommunicableAgent 实现方式：嵌入结构体（Go 等价 Mixin）

**决策**：`CommunicableAgent` 是一个具体结构体，提供 `Communicable` 接口的默认实现。Agent 子类通过嵌入 `CommunicableAgent` 获得通信能力（Go 组合 = Python Mixin）。

**原因**：
- Go 没有 Mixin/多重继承
- 接口组合方案（只定义接口不含实现）无法提供默认的 `Send/Publish/Subscribe` 委托逻辑
- 嵌入结构体是 Python `class MyAgent(CommunicableAgent, BaseAgent)` 的 Go 等价物
- Agent 子类嵌入后直接调用 `a.Send()`，无需自行实现通信方法

**三个接口/结构体的关系**：

| 组件 | 所在包 | 类型 | 职责 |
|------|--------|------|------|
| `Communicable` | `multi_agent/schema` | 接口 | 纯通信方法签名（Send/Publish/Subscribe/Unsubscribe） |
| `RuntimeBindable` | `multi_agent/team_runtime` | 接口 | 绑定运行时（BindRuntime），引用 TeamRuntime 类型 |
| `CommunicableAgent` | `multi_agent/team_runtime` | 结构体 | 具体实现，同时满足 Communicable + RuntimeBindable |

**外部使用方式**：从 Runner.ResourceMgr 获取的是 `BaseAgent` 接口（无 Send 方法），外部需要通信时通过**类型断言** `agent.(schema.Communicable)` 获取通信接口。这与 Go 标准库 `io.Reader/Writer` 的做法一致。

### 3.3 BindRuntime 需要 runtime + agentID 两个参数

**决策**：`BindRuntime(runtime *TeamRuntime, agentID string)` 需要两个参数。

**原因**：
- `runtime`：让 Agent 知道"通过哪个 TeamRuntime 通信"——通信方法全部委托给 runtime
- `agentID`：让 Agent 知道"我是谁"——`Send()` 调用 `runtime.Send(message, recipient, sender=self.agentID)` 需要填充 sender 字段；`Subscribe()` 调用 `runtime.Subscribe(self.agentID, topic)` 也需要 agentID

### 3.4 Provider 包装机制

**决策**：`TeamRuntime.RegisterAgent()` 包装原始 provider，在 Agent 创建后自动调用 `BindRuntime`。

**原因**：Agent 是延迟创建的（注册时只传工厂函数，真正创建在首次使用时）。包装确保不管 Agent 什么时候被创建，创建后都会自动绑定 runtime。

**流程**：
1. 用户传入原始 provider（创建 Agent 实例的工厂函数）
2. `RegisterAgent()` 包装为 wrapped provider
3. 注册 wrapped provider 到 Runner.ResourceMgr
4. Runner 首次使用时调用 wrapped provider 创建 Agent
5. wrapped provider 内部：先调用原始 provider 创建 Agent → 检查是否实现 RuntimeBindable → 如果是则自动调用 BindRuntime

### 3.5 P2P 请求-响应：复用已有 InvokeQueueMessage

**决策**：直接复用 `runner/message_queue` 中的 `InvokeQueueMessage`（带 WaitResponse/CompleteResponse 语义），与 Python 完全一致。

**原因**：Python 中 MessageBus 也直接使用 Runner 的 `InvokeQueueMessage`（`from openjiuwen.core.runner.message_queue_base import InvokeQueueMessage`）和 `QueueMessage`，Go 项目已有等价实现。

### 3.6 通配符匹配：使用已有 fnmatch 库

**决策**：SubscriptionManager 使用 `github.com/danwakefield/fnmatch` 做通配符匹配。

**原因**：项目已引入此库（`go.mod` 中已有），且 `message_offloader.go` 中已在用。与 Python `fnmatch` 行为完全一致，零额外依赖。

### 3.7 循环依赖解决：BaseTeam 迁移到 schema 包

**决策**：将 `BaseTeam` 接口、`AgentTeamProvider`、`TeamAgentProvider` 从 `multi_agent/team.go` 迁移到 `multi_agent/schema/team_interface.go`。

**原因**：
- `runner/resources_manager` 已导入 `multi_agent`（引用 BaseTeam、AgentTeamProvider、TeamCardInterface）
- 如果 `multi_agent/team_runtime` 反过来导入 `runner`，会形成循环：`runner/resources_manager` → `multi_agent` → `runner`
- 迁移到 schema 包后：`runner/resources_manager` → `multi_agent/schema`（纯接口），`multi_agent/team_runtime` → `runner`（无循环）

**迁移项**：

| 迁移项 | 原位置 | 新位置 |
|--------|--------|--------|
| `BaseTeam` 接口 | `multi_agent/team.go` | `multi_agent/schema/team_interface.go` |
| `AgentTeamProvider` 函数类型 | `multi_agent/team.go` | `multi_agent/schema/team_interface.go` |
| `TeamAgentProvider` 函数类型 | `multi_agent/team.go` | `multi_agent/schema/team_interface.go` |

### 3.8 AgentTeamSession：完整实现（内部层 + 公开层）

**决策**：补充 AgentTeamSession 的完整实现，不在 8.30 中使用 `any` 占位。

**原因**：
- Python 中 `AgentTeamSession` 定义在 `core/session/agent_team.py`（属于 session 包，不在 multi_agent 中）
- Go 中 session 包已存在相关基础设施（InnerSession 接口、TeamIDProvider 接口）
- TeamRuntime 的 `activeTeamSessions` 需要具体类型，不应等到 9.85

**公开层命名**：`AgentTeamSession`（非 `Session`），避免与 Agent 的 `session.Session` 重名混淆。

**SessionFacade.Interact 处理**：返回 error（"team session does not support interact"）。Team 不直接与用户交互，这是 Agent 的概念。

### 3.9 activeSubscriptions vs subscriptionManager 区分

**决策**：MessageBus 中两个订阅相关字段各司其职，不可混淆。

| | `activeSubscriptions` | `subscriptionManager` |
|---|---|---|
| **层级** | 消息队列层（MQ 基础设施） | 业务逻辑层（Agent 订阅关系） |
| **内容** | topic → Subscription（队列订阅对象，有 handler 和 activate/deactivate 生命周期） | topic_pattern → agent_id 集合（逻辑映射，支持通配符） |
| **用途** | 确保每个 topic 在 MessageQueueInMemory 上恰好有一个订阅，绑定了消息处理 handler（handleP2pMessage / handlePubsubMessage） | 查找某个 topic_id 有哪些 Agent 订阅了（Pub-Sub 扇出时使用） |
| **生命周期** | 随 session 创建/销毁，需 activate/deactivate | 纯数据结构，随 subscribe/unsubscribe 操作 |
| **数量** | 少：通常只有 2 个（一个 P2P topic、一个 Pub-Sub topic），按 session 可能多个 | 多：每个 Agent 可以订阅任意多个 topic pattern |

### 3.10 Topic 隔离：照搬 Python 命名规则

**决策**：完全对齐 Python 的 topic 命名规则：`{team_id}_{session_id}__p2p__` / `{team_id}_{session_id}__pubsub__`。

**原因**：保证跨语言行为一致，调试时可直接对照。

### 3.11 并发模型对照

| Python | Go |
|--------|-----|
| `asyncio.Lock` | `sync.Mutex` / `sync.RWMutex` |
| `asyncio.gather(*tasks, return_exceptions=True)` | `errgroup.Group`（Pub-Sub 扇出） |
| `asyncio.wait_for(coro, timeout)` | `context.WithTimeout` + channel select |
| `InvokeQueueMessage.response` Future | `InvokeQueueMessage.WaitResponse()` / `CompleteResponse()` |
| 惰性启动双检锁 `asyncio.Lock` | `sync.Once` |

## 4. 文件组织

```
# ── AgentTeamSession 补充 ──
session/
├── internal/
│   ├── agent_session.go              # ✅ 已有
│   ├── agent_team_session.go         # 🆕 内部层 AgentTeamSession
│   └── agent_team_session_test.go    # 🆕
├── agent_team.go                     # 🆕 公开层 AgentTeamSession + CreateAgentTeamSession()
├── agent_team_test.go                # 🆕
└── ...

# ── BaseTeam 迁移 + Communicable 接口 ──
multi_agent/
├── schema/
│   ├── team_card.go                  # ✅ 已有
│   ├── team_interface.go             # 🆕 迁移：BaseTeam + AgentTeamProvider + TeamAgentProvider
│   └── communicable.go              # 🆕 Communicable 接口
├── team_runtime/                     # 🆕 核心实现子包
│   ├── doc.go                        # 包文档
│   ├── envelope.go                   # MessageEnvelope
│   ├── message_bus.go                # MessageBus + MessageBusConfig
│   ├── message_router.go             # MessageRouter
│   ├── subscription_manager.go       # SubscriptionManager
│   ├── team_runtime.go               # TeamRuntime + RuntimeConfig
│   ├── runtime_bindable.go           # RuntimeBindable 接口
│   ├── communicable_agent.go         # CommunicableAgent 具体实现
│   ├── envelope_test.go
│   ├── message_bus_test.go
│   ├── message_router_test.go
│   ├── subscription_manager_test.go
│   ├── team_runtime_test.go
│   ├── communicable_agent_test.go
│   └── doc.go 更新
├── team.go                           # → 改为引用 schema.TeamInterface 中的类型
├── team_option.go                    # → 回填 TeamSession 为 *session.AgentTeamSession
└── config.go                         # ✅ 不变
```

## 5. 核心类型设计

### 5.1 AgentTeamSession（内部层）

**文件**：`session/internal/agent_team_session.go`

**对应 Python**：`openjiuwen/core/session/internal/agent_team.py` (AgentTeamSession)

```go
// AgentTeamSession Agent 团队内部会话，实现 InnerSession + TeamIDProvider 接口。
type AgentTeamSession struct {
    sessionID           string
    teamID              string
    config              config.SessionConfig
    state               state.SessionState       // StateCollection（非 AgentStateCollection）
    streamWriterManager *stream.StreamWriterManager
    tracer              *tracer.Tracer
    checkpointer        checkpointer.Checkpointer
    teamSpan            *tracer.TraceAgentSpan   // 团队追踪跨度
}
```

实现 `InnerSession` 全部方法 + `TeamIDProvider.TeamID()` + `TeamSpan()`。

构造选项模式对齐 `AgentSession`：`NewAgentTeamSession(sessionID, teamID, opts...)` + `WithTeamConfig/WithTeamState/WithTeamTracer/...`

### 5.2 AgentTeamSession（公开层）

**文件**：`session/agent_team.go`

**对应 Python**：`openjiuwen/core/session/agent_team.py` (Session)

```go
// AgentTeamSession Agent 团队公开会话，实现 SessionFacade 接口。
type AgentTeamSession struct {
    sessionID   string
    teamID      string
    inner       *internal.AgentTeamSession
    preRunDone  bool
    postRunDone bool
}

// 编译时检查
var _ interfaces.SessionFacade = (*AgentTeamSession)(nil)
```

**SessionFacade 接口实现**：

| 方法 | 实现 |
|------|------|
| `GetSessionID()` | 返回 sessionID |
| `UpdateState(data)` | 委托 inner.State().UpdateGlobal(data) |
| `GetState(key)` | 委托 inner.State().GetGlobal(key) |
| `DumpState()` | 委托 inner.State().Dump() |
| `WriteStream(ctx, data)` | 委托 inner.StreamWriterManager() 写入 |
| `WriteCustomStream(ctx, data)` | 委托 inner.StreamWriterManager() 写入 |
| `GetEnv(key, default...)` | 委托 inner.Config().GetEnv(key, default) |
| `Interact(ctx, value)` | **返回 error**（"team session does not support interact"） |

**额外方法**（AgentTeamSession 独有，不属 SessionFacade）：

| 方法 | 实现 |
|------|------|
| `GetTeamID()` | 返回 teamID |
| `GetEnvs()` | 委托 inner.Config().GetEnvs() |
| `PreRun(ctx, inputs)` | 检查点 PreAgentTeamExecute |
| `PostRun(ctx)` | CloseStream + Commit |
| `Commit(ctx)` | 检查点 PostAgentTeamExecute |
| `FlushCheckpoint(ctx)` | 等价 Commit |
| `CloseStream(ctx)` | 关闭流发射器 |
| `CreateAgentSession(card, agentID)` | 创建子 AgentSession |
| `Inner()` | 返回内部 AgentTeamSession（供 TeamRuntime 使用） |

工厂函数：`CreateAgentTeamSession(sessionID, envs, teamID) *AgentTeamSession`

### 5.3 Communicable 接口

**文件**：`multi_agent/schema/communicable.go`

```go
// Communicable 可通信接口，Agent 实现此接口即可使用 P2P/Pub-Sub 通信。
type Communicable interface {
    Send(ctx context.Context, message any, recipient string, opts ...TeamOption) (any, error)
    Publish(ctx context.Context, message any, topicID string, opts ...TeamOption) error
    Subscribe(ctx context.Context, agentID string, topic string) error
    Unsubscribe(ctx context.Context, agentID string, topic string) error
}
```

### 5.4 RuntimeBindable 接口

**文件**：`multi_agent/team_runtime/runtime_bindable.go`

```go
// RuntimeBindable 可绑定运行时接口。
// Agent 实现此接口后，TeamRuntime.RegisterAgent() 会在 Agent 创建时
// 自动调用 BindRuntime 注入 TeamRuntime 引用和 agentID。
type RuntimeBindable interface {
    BindRuntime(runtime *TeamRuntime, agentID string)
}
```

### 5.5 CommunicableAgent（具体实现，Go 版 Mixin）

**文件**：`multi_agent/team_runtime/communicable_agent.go`

**对应 Python**：`openjiuwen/core/multi_agent/team_runtime/communicable_agent.py`

```go
// CommunicableAgent 可通信 Agent，提供 Communicable 接口的默认实现。
// Agent 子类通过嵌入此结构体获得通信能力（Go 组合 = Python Mixin）。
type CommunicableAgent struct {
    runtime *TeamRuntime
    agentID string
}

// 编译时检查
var _ schema.Communicable = (*CommunicableAgent)(nil)
var _ RuntimeBindable = (*CommunicableAgent)(nil)

func (c *CommunicableAgent) BindRuntime(runtime *TeamRuntime, agentID string) {
    c.runtime = runtime
    c.agentID = agentID
}

func (c *CommunicableAgent) Send(ctx, message, recipient, opts...) (any, error) {
    return c.runtime.Send(ctx, message, recipient, c.agentID, opts...)
}
func (c *CommunicableAgent) Publish(ctx, message, topicID, opts...) error {
    return c.runtime.Publish(ctx, message, topicID, c.agentID, opts...)
}
func (c *CommunicableAgent) Subscribe(ctx, topic string) error {
    return c.runtime.Subscribe(ctx, c.agentID, topic)
}
func (c *CommunicableAgent) Unsubscribe(ctx, topic string) error {
    return c.runtime.Unsubscribe(ctx, c.agentID, topic)
}
```

**Agent 子类使用方式**：

```go
// 嵌入 CommunicableAgent 获得通信能力
type CoderAgent struct {
    team_runtime.CommunicableAgent
    // ... Agent 自有字段
}

// Agent 内部直接调用通信方法
func (a *CoderAgent) Invoke(ctx, inputs, opts...) (any, error) {
    response, err := a.Send(ctx, msg, "reviewer")  // 嵌入的 CommunicableAgent 提供
}

// 外部使用：类型断言获取通信接口
agent := resourceMgr.GetAgent(ctx, "coder")
if comm, ok := agent.(schema.Communicable); ok {
    comm.Send(ctx, msg, "reviewer")
}
```

### 5.6 MessageEnvelope

**文件**：`multi_agent/team_runtime/envelope.go`

**对应 Python**：`openjiuwen/core/multi_agent/team_runtime/envelope.py`

```go
// MessageEnvelope 消息信封，Agent 间路由消息的不可变容器。
type MessageEnvelope struct {
    MessageID string         // 唯一消息标识
    Message   any            // 消息负载
    Sender    string         // 发送者 Agent ID
    Recipient string         // 接收者 Agent ID（P2P 模式）
    TopicID   string         // 主题 ID（Pub-Sub 模式）
    SessionID string         // 会话 ID
    Metadata  map[string]any // 附加元数据
}

func (e *MessageEnvelope) IsP2P() bool      // Recipient != ""
func (e *MessageEnvelope) IsPubSub() bool    // TopicID != ""
```

### 5.7 SubscriptionManager

**文件**：`multi_agent/team_runtime/subscription_manager.go`

**对应 Python**：`openjiuwen/core/multi_agent/team_runtime/subscription_manager.py`

```go
// SubscriptionManager 主题到 Agent 的订阅映射管理器，支持 fnmatch 通配符匹配。
type SubscriptionManager struct {
    subscriptions map[string]map[string]struct{}  // topic_pattern → agent_id 集合
    agentTopics   map[string]map[string]struct{}  // agent_id → topic_pattern 集合（反查）
    mu            sync.RWMutex
}

func (m *SubscriptionManager) Subscribe(agentID, topicPattern string)
func (m *SubscriptionManager) Unsubscribe(agentID, topicPattern string)
func (m *SubscriptionManager) UnsubscribeAll(agentID string)
func (m *SubscriptionManager) GetSubscribers(topicID string) []string  // fnmatch 匹配
func (m *SubscriptionManager) GetSubscriptionCount() int
func (m *SubscriptionManager) ListSubscriptions(agentID string) map[string]any
```

通配符匹配使用 `github.com/danwakefield/fnmatch`，与 Python `fnmatch` 行为一致。

### 5.8 MessageRouter

**文件**：`multi_agent/team_runtime/message_router.go`

**对应 Python**：`openjiuwen/core/multi_agent/team_runtime/message_router.py`

```go
// MessageRouter 消息路由器，将消息分发到目标 Agent。
type MessageRouter struct {
    subscriptionManager *SubscriptionManager
    runtime            *TeamRuntime  // 获取 session 和 agentCard
}

func (r *MessageRouter) RouteP2PMessage(ctx, envelope) (any, error)
func (r *MessageRouter) RoutePubsubMessage(ctx, envelope) error
```

**P2P 路由流程**：
1. 触发 `AgentP2PReceived` 回调事件
2. 调用 `_buildAgentSession` 构建会话
3. 调用 `Runner.RunAgent(agent=recipient, inputs=message, session=session)`
4. 返回 Agent 响应结果
5. 异常包装为 `RUNNER_RUN_AGENT_ERROR`

**Pub-Sub 路由流程**：
1. 通过 `SubscriptionManager.GetSubscribers(topicID)` 获取匹配的订阅者列表
2. 无订阅者时记录警告并返回（fire-and-forget 语义）
3. 并发调用所有订阅者：`errgroup.Group`
4. 单个订阅者失败只记录错误，不中断其他订阅者的投递

### 5.9 MessageBus

**文件**：`multi_agent/team_runtime/message_bus.go`

**对应 Python**：`openjiuwen/core/multi_agent/team_runtime/message_bus.py`

```go
// MessageBusConfig 消息总线配置
type MessageBusConfig struct {
    MaxQueueSize   int     // 默认 1000
    ProcessTimeout float64 // 默认 1800.0
    TeamID         string  // 团队 ID，用于主题隔离
}

// MessageBus 消息总线，P2P 和 Pub-Sub 通信基础设施。
type MessageBus struct {
    config              MessageBusConfig
    teamID              string
    mq                  *message_queue.MessageQueueInMemory    // 复用已有内存队列
    activeSubscriptions map[string]message_queue.SubscriptionBase // 队列层订阅
    subscriptionLock    sync.Mutex
    subscriptionManager *SubscriptionManager                    // 业务层订阅
    router              *MessageRouter
    running             bool
}
```

**Topic 命名规则**（与 Python 完全一致）：

```
P2P topic:     {team_id}_{session_id}__p2p__   或   {team_id}__p2p__
Pub-Sub topic: {team_id}_{session_id}__pubsub__ 或   {team_id}__pubsub__
```

**P2P 消息流程**（请求-响应模式）：
1. 发送方调用 `Send()` → 创建 `InvokeQueueMessage` → 投递到 P2P topic
2. 队列触发 `handleP2pMessage` → 路由到 `MessageRouter.RouteP2PMessage`
3. `Runner.RunAgent()` 执行目标 Agent → 响应通过 `InvokeQueueMessage.CompleteResponse()` 返回
4. 发送方通过 `InvokeQueueMessage.WaitResponse()` 获取结果

**Pub-Sub 消息流程**（发后即忘模式）：
1. 发布方调用 `Publish()` → 创建 `QueueMessage` → 投递到 Pub-Sub topic
2. 队列触发 `handlePubsubMessage` → 路由到 `MessageRouter.RoutePubsubMessage`
3. 扇出到所有匹配订阅者 → 各订阅者并发执行

**双检锁订阅保证**：`ensureSubscription()` 使用 `sync.Mutex` + 双重检查，防止并发创建重复订阅。

### 5.10 TeamRuntime

**文件**：`multi_agent/team_runtime/team_runtime.go`

**对应 Python**：`openjiuwen/core/multi_agent/team_runtime/team_runtime.py`

```go
// RuntimeConfig 团队运行时配置
type RuntimeConfig struct {
    TeamID     string          // 默认 "default"
    MessageBus *MessageBusConfig
    P2PTimeout float64         // 默认 1800.0
}

// TeamRuntime 团队运行时，自包含的多 Agent 通信运行时。
type TeamRuntime struct {
    config             RuntimeConfig
    teamID             string
    agentCards         map[string]*agentschema.AgentCard
    messageBus         *MessageBus
    activeTeamSessions map[string]*session.AgentTeamSession  // 具体类型（已回填）
    running            bool
    startOnce          sync.Once  // 惰性启动
    p2pTimeout         float64
}
```

**核心方法**：

| 类别 | 方法 |
|------|------|
| 生命周期 | `Start()` / `Stop()` / `CleanupSession(sessionID)` |
| Agent 注册 | `RegisterAgent(card, provider)` / `UnregisterAgent(agentID)` / `HasAgent()` / `GetAgentCard()` / `ListAgents()` / `GetAgentCount()` |
| P2P 通信 | `Send(ctx, message, recipient, sender, opts...)` |
| Pub-Sub 通信 | `Publish(ctx, message, topicID, sender, opts...)` |
| 订阅管理 | `Subscribe(ctx, agentID, topic)` / `Unsubscribe(ctx, agentID, topic)` |
| 会话管理 | `BindTeamSession(session)` / `UnbindTeamSession(sessionID)` / `GetTeamSession(sessionID)` |
| 订阅查询 | `ListSubscriptions(agentID)` / `GetSubscriptionCount()` |

**RegisterAgent 流程**（含 provider 包装）：

1. 存储 AgentCard 到 `agentCards` 字典
2. 包装原始 provider：在创建 Agent 后检查是否实现 `RuntimeBindable`，如果是则自动调用 `BindRuntime(tr, card.AbilityID())`
3. 注册包装后的 provider 到 `Runner.ResourceMgr.AddAgent()`
4. 如果 Agent 未实现 RuntimeBindable，记录警告（通信方法不可用）

## 6. 通信模式对比

| 维度 | P2P (Send) | Pub-Sub (Publish) |
|------|-----------|-------------------|
| 寻址 | 指定 recipient agentID | 指定 topicID |
| 语义 | 请求-响应（等待结果） | 发后即忘（不等待） |
| 队列消息 | `InvokeQueueMessage` | `QueueMessage` |
| 路由 | 单一目标 Agent | 扇出到所有匹配订阅者 |
| 错误处理 | 异常上抛调用方 | 单订阅者失败不中断，仅记录日志 |
| 回调 | `AgentP2PReceived` | `AgentPubsubReceived` |
| Topic | `{team_id}__p2p__` | `{team_id}__pubsub__` |
| 超时 | 支持 | 不支持 |

## 7. 依赖方向

迁移 BaseTeam 到 schema 包后的依赖关系：

```
runner/resources_manager → multi_agent/schema（纯接口，无循环）
multi_agent/team_runtime → runner（调用 RunAgent/ResourceMgr，无循环）
multi_agent             → multi_agent/schema（主包引用 schema 中的类型，无循环）
multi_agent             → multi_agent/team_runtime（主包引用子包，无循环）
session                 → 不依赖 multi_agent（AgentTeamSession 是 session 自身概念）
multi_agent/team_runtime → session（引用 AgentTeamSession 类型，无循环）
```

## 8. 回填点

| 预留位置 | 文件 | 回填内容 |
|----------|------|----------|
| `team_option.go` | `⤵️ 8.30 TeamSession 实现后替换为具体类型` | `Session any` → `Session *session.AgentTeamSession`；`WithTeamSession(sess any)` → `WithTeamSession(sess *session.AgentTeamSession)` |
| `global_controller.go` | `⤵️ 5.13+ 回填：等 AgentTeamEvents 定义后注册 P2P/PubSub 回调` | 注册 `AgentP2PReceived` 和 `AgentPubsubReceived` 回调 |
| `runner.go` | `teamRuntimeManager any` | **8.30 不回填**，留给 9.85 TeamRunner |

## 9. 已有基础设施复用

| 已有组件 | 路径 | 复用方式 |
|----------|------|----------|
| `MessageQueueInMemory`（Go channel 实现） | `runner/message_queue/queue.go` | 直接复用，作为 MessageBus 底层队列 |
| `QueueMessage` / `InvokeQueueMessage` | `runner/message_queue/base.go` | 直接复用，P2P 用 InvokeQueueMessage，Pub-Sub 用 QueueMessage |
| `CallbackFramework`（AgentTeam 事件系统） | `runner/callback/` | 直接复用，触发 P2PReceived/PubsubReceived 回调 |
| `AgentTeamEventType`（P2PReceived/PubsubReceived） | `runner/callback/events.go` | 直接复用 |
| `fnmatch` 库 | `github.com/danwakefield/fnmatch` | 直接复用，SubscriptionManager 通配符匹配 |
| `AgentTeamMgr`（团队资源管理器） | `runner/resources_manager/` | 需适配（改为导入 schema 包） |
| `TeamCard` / `EventDrivenTeamCard` | `multi_agent/schema/` | 直接复用 |

## 10. Python 参考文件索引

| Python 文件 | Go 对应文件 | 职责 |
|------------|------------|------|
| `team_runtime/__init__.py` | `team_runtime/doc.go` | 包入口 |
| `team_runtime/envelope.py` | `team_runtime/envelope.go` | 消息信封 |
| `team_runtime/communicable_agent.py` | `team_runtime/communicable_agent.go` + `schema/communicable.go` + `team_runtime/runtime_bindable.go` | 可通信 Agent |
| `team_runtime/message_router.py` | `team_runtime/message_router.go` | 消息路由器 |
| `team_runtime/subscription_manager.py` | `team_runtime/subscription_manager.go` | 订阅管理器 |
| `team_runtime/message_bus.py` | `team_runtime/message_bus.go` | 消息总线 |
| `team_runtime/team_runtime.py` | `team_runtime/team_runtime.go` | 团队运行时 |
| `session/internal/agent_team.py` | `session/internal/agent_team_session.go` | AgentTeam 内部会话 |
| `session/agent_team.py` | `session/agent_team.go` | AgentTeam 公开会话 |
