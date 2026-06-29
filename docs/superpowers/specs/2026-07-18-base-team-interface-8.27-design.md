# 8.27 BaseTeam 接口设计

## 概述

本文档定义领域八 8.27 步骤 — BaseTeam 接口的 Go 实现设计。BaseTeam 是多 Agent 团队体系的根基接口，定义了团队与外部交互的统一契约。

## 流程位置

```
领域八：工作流与图引擎 + 多 Agent 团队
├── 8.x 工作流（8.13-8.26）
├── 8.x 多 Agent 团队（8.27-8.36）
│   ├── 8.27 BaseTeam 接口 ← 当前步骤
│   ├── 8.28 TeamCard/TeamConfig ← 依赖 8.27 的 TeamCard/TeamConfig 占位
│   ├── 8.29 EventDrivenTeamCard
│   ├── 8.30 TeamRuntime ← 被 8.27 委托
│   ├── 8.31 CommunicableAgent
│   ├── 8.32 MessageRouter/SubscriptionManager
│   ├── 8.33 MessageBus
│   ├── 8.34 HandoffTeam ← BaseTeam 的具体实现
│   ├── 8.35 HierarchicalTeam(msgbus) ← BaseTeam 的具体实现
│   └── 8.36 HierarchicalTeam(tools) ← BaseTeam 的具体实现
```

**作用：** BaseTeam 是多 Agent 团队体系的基础接口，后续所有具体团队（HandoffTeam、HierarchicalTeam）都实现此接口。

## 设计决策

### D1: 接口 vs 嵌入结构体

**决策：纯 Go 接口**

Python 中 BaseTeam 是 ABC 抽象基类（部分方法有默认实现，invoke/stream 是 abstractmethod）。Go 中选择纯接口，具体团队各自完整实现全部方法。理由：Go 惯用法优先组合而非继承，纯接口更符合 Go 风格。

### D2: 方法范围

**决策：完整对齐 Python BaseTeam 的所有方法**

包括 Python 中的辅助方法（unsubscribe、configure、get_agent_card、get_agent_count、list_agents），确保 1:1 映射。

### D3: invoke/stream 签名

**决策：对齐已有 Agent 接口模式，签名独立**

BaseTeam 与 BaseAgent 是平行的两个体系，不继承 BaseAgent。Team 的 invoke/stream 是对整个团队的调用（协调多个 Agent），签名风格对齐（`ctx + inputs + opts`），但使用独立的 `TeamOption`。

- `Invoke(ctx, inputs map[string]any, opts ...TeamOption) (any, error)`
- `Stream(ctx, inputs map[string]any, opts ...TeamOption) (<-chan stream.Schema, error)`

### D4: send/publish 的 message 类型

**决策：`map[string]any`**

调研 Python 源码后的结论：
- **send：** 所有调用场景（CommunicableAgent、HierarchicalTeam、P2PAbilityManager）的 message 全部是 dict
- **publish：** 外部用户通过 BaseTeam.publish 传 dict；HandoffTeam 内部传 HandoffRequest 结构体时走 `runtime.publish()`（TeamRuntime 层，8.30），不走 BaseTeam 接口
- 因此 BaseTeam 接口的 send/publish 用 `map[string]any` 足够，HandoffRequest 在 TeamRuntime 层用 any 处理

### D5: 参数传递风格

**决策：显式参数保留（对齐 Python），可选参数用 TeamOption 传递**

核心参数（message、recipient、sender、topicID、agentID 等）显式列出，可选参数（session、sessionID、timeout、streamModes）通过 TeamOption 传递。

### D6: TeamOption 独立定义

**决策：新建独立 TeamOption，不复用 AgentOption**

TeamOption 在 multi_agent 包下定义，携带 Session、SessionID、Timeout、StreamModes 等字段。与 AgentOption/WorkflowOption 平行，各自独立扩展。

### D7: 包路径

**决策：`internal/agentcore/multi_agent/`**

在 agentcore 下创建 multi_agent 子包，与 Python 的 `core/multi_agent/` 对应。

### D8: 文件组织

**决策：方案 A — team.go + team_option.go**

接口和 Option 各一个文件，紧凑实用。后续 8.28 TeamCard/TeamConfig 在 `schema/` 子目录和 `config.go` 中扩展。

### D9: 回填策略

**决策：8.27 一步到位**

定义 BaseTeam 接口 + 回填 AgentTeamMgr/ResourceManager 的预留标记，在同一步骤中完成。

## 文件结构

```
internal/agentcore/multi_agent/
├── doc.go                 # 包文档
├── team.go                # BaseTeam 接口 + AgentTeamProvider 类型 + TeamCard/TeamConfig 占位
└── team_option.go         # TeamOptions 结构体 + TeamOption 函数类型 + WithXxx
```

## BaseTeam 接口定义

```go
// BaseTeam 多 Agent 团队核心行为契约。
//
// 对应 Python: openjiuwen/core/multi_agent/team.py (BaseTeam)
//
// 设计原则：
//   - Card 是必填项（定义团队身份）
//   - Config 是可选项（定义团队运行时行为）
//   - BaseTeam 与 BaseAgent 是平行的两个体系，不继承 BaseAgent
//   - invoke/stream 是对整个团队的调用，由子类实现
//   - add_agent/remove_agent/send/publish/subscribe 等方法在具体团队中实现
type BaseTeam interface {
    // ── 核心执行 ──

    // Invoke 非流式调用团队。
    // 对应 Python: BaseTeam.invoke(message, session)
    Invoke(ctx context.Context, inputs map[string]any, opts ...TeamOption) (any, error)

    // Stream 流式调用团队。
    // 对应 Python: BaseTeam.stream(message, session) -> AsyncIterator
    Stream(ctx context.Context, inputs map[string]any, opts ...TeamOption) (<-chan stream.Schema, error)

    // ── Agent 管理 ──

    // AddAgent 向团队注册 Agent。
    // 对应 Python: BaseTeam.add_agent(card, provider) -> self
    AddAgent(ctx context.Context, card *agentschema.AgentCard, provider AgentProvider) error

    // RemoveAgent 从团队注销 Agent。
    // 对应 Python: BaseTeam.remove_agent(agent) -> self
    RemoveAgent(ctx context.Context, agentID string) error

    // ── 消息通信 ──

    // Send 点对点消息发送。
    // 对应 Python: BaseTeam.send(message, recipient, sender, session_id, timeout)
    Send(ctx context.Context, message map[string]any, recipient string, sender string, opts ...TeamOption) (any, error)

    // Publish 发布消息到主题。
    // 对应 Python: BaseTeam.publish(message, topic_id, sender, session_id)
    Publish(ctx context.Context, message map[string]any, topicID string, sender string, opts ...TeamOption) error

    // Subscribe 订阅主题。
    // 对应 Python: BaseTeam.subscribe(agent_id, topic)
    Subscribe(ctx context.Context, agentID string, topic string) error

    // Unsubscribe 取消订阅。
    // 对应 Python: BaseTeam.unsubscribe(agent_id, topic)
    Unsubscribe(ctx context.Context, agentID string, topic string) error

    // ── 配置 ──

    // Configure 配置团队。
    // 对应 Python: BaseTeam.configure(config) -> self
    Configure(ctx context.Context, config TeamConfig) error

    // ── 查询 ──

    // GetAgentCard 获取 Agent 卡片。
    // 对应 Python: BaseTeam.get_agent_card(agent_id)
    GetAgentCard(agentID string) (*agentschema.AgentCard, error)

    // GetAgentCount 获取 Agent 数量。
    // 对应 Python: BaseTeam.get_agent_count()
    GetAgentCount() int

    // ListAgents 列出所有 Agent ID。
    // 对应 Python: BaseTeam.list_agents()
    ListAgents() []string

    // ── 访问器 ──

    // Card 返回团队身份卡片。
    // 对应 Python: BaseTeam.card 属性
    Card() *TeamCard

    // Config 返回团队配置。
    // 对应 Python: BaseTeam.config 属性
    Config() TeamConfig
}
```

## AgentTeamProvider 类型

```go
// AgentTeamProvider 团队资源提供者函数，接受 TeamCard 返回 BaseTeam 实例。
//
// 对应 Python: AgentTeamProvider = Callable[[TeamCard], Awaitable[BaseTeam]] | Callable[[TeamCard], BaseTeam]
// 用于延迟加载团队资源，注册时传入工厂函数而非实例。
type AgentTeamProvider func(ctx context.Context, card *TeamCard) (BaseTeam, error)
```

## TeamOption 定义

```go
// TeamOptions 团队调用选项。
type TeamOptions struct {
    // Session 团队会话（可选）
    // 对应 Python: invoke(message, session) 的 session 参数
    // ⤵️ 8.30 TeamSession 实现后替换为具体类型
    Session any
    // SessionID 会话标识（可选）
    // 对应 Python: send/publish 的 session_id 参数
    SessionID string
    // Timeout 响应超时秒数（可选）
    // 对应 Python: send 的 timeout 参数
    Timeout float64
    // StreamModes 流式输出模式（可选）
    // 对应 Python: stream 的 stream_modes 参数
    StreamModes []stream.StreamMode
}

// TeamOption 团队调用选项函数。
type TeamOption func(*TeamOptions)

// WithTeamSession 设置团队会话。
func WithTeamSession(sess any) TeamOption

// WithTeamSessionID 设置会话标识。
func WithTeamSessionID(sessionID string) TeamOption

// WithTeamTimeout 设置响应超时。
func WithTeamTimeout(timeout float64) TeamOption

// WithTeamStreamModes 设置流式输出模式。
func WithTeamStreamModes(modes []stream.StreamMode) TeamOption

// NewTeamOptions 从选项列表构建 TeamOptions。
func NewTeamOptions(opts ...TeamOption) *TeamOptions
```

## 最小占位类型

8.27 需要引用 TeamCard 和 TeamConfig，但完整实现分别在 8.28。在 8.27 中先定义最小占位：

```go
// TeamCard 团队身份卡片（最小占位，8.28 完整实现）。
//
// 对应 Python: openjiuwen/core/multi_agent/schema/team_card.py (TeamCard)
// Python 继承 BaseCard: id/name/description + agent_cards/topic/version/tags
type TeamCard struct {
    schema.BaseCard
}

// TeamConfig 团队运行时配置（最小占位，8.28 完整实现）。
//
// 对应 Python: openjiuwen/core/multi_agent/config.py (TeamConfig)
// Python 字段: max_agents=10, max_concurrent_messages=100, message_timeout=30.0
type TeamConfig struct{}
```

## 回填内容

### agent_team_manager.go 回填

**回填前：**
```go
type AgentTeamMgr struct {
    agents map[string]any  // ⤴️ 8.27 后替换
    mu sync.RWMutex
}
// AddAgentTeam / RemoveAgentTeam / GetAgentTeam 返回 not implemented error
```

**回填后：**
- 将 `AgentTeamMgr` 改为使用 `AbstractManager` 模式（对齐 `AgentMgr`/`WorkflowMgr`/`ModelMgr`）
- 三个方法委托给 `registerProvider`/`unregisterProvider`/`getResource`
- 新增 `AgentTeamEntry` 结构体（对齐 `AgentEntry`/`WorkflowEntry`）

### resource_manager.go 回填

- `AddAgentTeam` / `RemoveAgentTeam` / `GetAgentTeam` 方法补全实现
- `dispatchAdd` case "team" 分支补全
- `dispatchGet` case "team" 分支补全
- `resourceTypeOf` 添加 `reflect.TypeOf((*TeamCard)(nil))` → "team"
- 清除所有 `⤵️ 预留：8.27/8.28` 标记

### base.go 新增

- `AgentTeamEntry` 结构体（对齐 `AgentEntry`/`WorkflowEntry`）
- `AgentTeamProvider` 类型已在 multi_agent 包定义，base.go 中可引用或定义兼容类型

## 测试计划

| 测试文件 | 测试内容 |
|---------|---------|
| `team_test.go` | 编译时接口满足检查 `var _ BaseTeam = (*stubTeam)(nil))` |
| `team_option_test.go` | TeamOptions 构建、WithXxx 函数、NewTeamOptions |
| `agent_team_manager_test.go` 更新 | 回填后的 Add/Remove/Get 测试 |

## Python 对齐对照表

| Go 方法 | Python 方法 | Go 签名 | Python 签名 |
|---------|-----------|---------|------------|
| `Invoke` | `invoke` | `(ctx, inputs map[string]any, opts ...TeamOption) (any, error)` | `async invoke(message, session=None) -> Any` |
| `Stream` | `stream` | `(ctx, inputs map[string]any, opts ...TeamOption) (<-chan stream.Schema, error)` | `async stream(message, session=None) -> AsyncIterator[Any]` |
| `AddAgent` | `add_agent` | `(ctx, card, provider) error` | `add_agent(card, provider) -> self` |
| `RemoveAgent` | `remove_agent` | `(ctx, agentID) error` | `remove_agent(agent) -> self` |
| `Send` | `send` | `(ctx, message map[string]any, recipient, sender, opts) (any, error)` | `async send(message, recipient, sender, session_id, timeout) -> Any` |
| `Publish` | `publish` | `(ctx, message map[string]any, topicID, sender, opts) error` | `async publish(message, topic_id, sender, session_id) -> None` |
| `Subscribe` | `subscribe` | `(ctx, agentID, topic) error` | `async subscribe(agent_id, topic) -> None` |
| `Unsubscribe` | `unsubscribe` | `(ctx, agentID, topic) error` | `async unsubscribe(agent_id, topic) -> None` |
| `Configure` | `configure` | `(ctx, config TeamConfig) error` | `configure(config) -> self` |
| `GetAgentCard` | `get_agent_card` | `(agentID) (*AgentCard, error)` | `get_agent_card(agent_id) -> AgentCard or None` |
| `GetAgentCount` | `get_agent_count` | `() int` | `get_agent_count() -> int` |
| `ListAgents` | `list_agents` | `() []string` | `list_agents() -> list[str]` |
| `Card` | `card` 属性 | `() *TeamCard` | `@property card -> TeamCard` |
| `Config` | `config` 属性 | `() TeamConfig` | `@property config -> TeamConfig` |
