# 8.29 EventDrivenTeamCard 实现设计

## 概述

实现步骤 8.29：EventDrivenTeamCard（事件驱动团队卡片），在 TeamCard 基础上增加订阅关系声明字段 `Subscriptions map[string][]string`，同时引入 TeamCardInterface 完整 getter 接口，使 BaseTeam.Card() 支持多态返回。

核心变更：
1. 删除 `team.go` 中的 `type TeamCard = schema.TeamCard` 类型别名 re-export，所有使用方改为直接引用 `maschema.TeamCard`
2. 新增 `TeamCardInterface` 完整 getter 接口（含 GetSubscriptions）
3. 为 `BaseCard` 和 `TeamCard` 添加 getter 方法
4. 新增 `EventDrivenTeamCard` struct（嵌入 TeamCard + Subscriptions 字段）
5. `BaseTeam.Card()` 返回类型从 `*TeamCard` 改为 `TeamCardInterface`
6. `resources_manager` 中 TeamCard 相关字段/参数改为 `TeamCardInterface`

## 流程位置与作用

### 在 Agent 会话流程中的位置

8.29 位于领域八「多 Agent 团队」子分组的第 3 步：

```
8.27 ✅  BaseTeam 接口              — 团队行为契约（已完成）
8.28 ✅  TeamCard / TeamConfig      — 团队元数据与配置（已完成）
8.29 ☐  EventDrivenTeamCard        — 事件驱动团队卡片（当前实现）
8.30 ☐  TeamRuntime                — 消息总线，P2P 通信
8.31 ☐  CommunicableAgent          — 可通信 Agent 包装
8.32 ☐  MessageRouter / SubscriptionManager — 消息路由与订阅
8.33 ☐  MessageBus                 — 消息总线
8.34 ☐  HandoffTeam                — 交接模式团队
8.35 ☐  HierarchicalTeam (msgbus)  — 层级团队-消息总线
8.36 ☐  HierarchicalTeam (tools)   — 层级团队-工具委托
```

### 作用

- **EventDrivenTeamCard**：TeamCard 的事件驱动扩展，增加 `Subscriptions map[string][]string` 字段，声明式描述每个 Agent 订阅哪些 topic。
  - 左侧（8.27–8.28）：定义团队"是什么"——接口契约 + 静态元数据
  - **8.29**：在静态元数据上增加**订阅关系声明**，将"谁关心什么事件"固化到团队卡片中
  - 右侧（8.30–8.33）：将声明转化为运行时行为——消息总线、路由、通信

- **TeamCardInterface**：完整 getter 接口，使 `BaseTeam.Card()` 能多态返回 TeamCard 或 EventDrivenTeamCard，调用方通过接口方法访问字段，无需关心具体类型。

- **Python 现状**：`EventDrivenTeamCard` 在 Python 中仅定义了数据模型（`subscriptions: Dict[str, List[str]]`），尚未被运行时（TeamRuntime 等）消费。Go 端实现数据模型 + getter 接口，为 8.30+ 的运行时自动初始化订阅预留支撑。

## 一、Python 参考实现

```python
# openjiuwen/core/multi_agent/schema/team_card.py

class TeamCard(BaseCard):
    """Team Identity Card"""
    agent_cards: List[AgentCard] = Field(default_factory=list)
    topic: str = Field(default='')
    version: str = Field(default='1.0.0')
    tags: List[str] = Field(default_factory=list)


class EventDrivenTeamCard(TeamCard):
    """Event-driven team card with subscription information"""
    subscriptions: Dict[str, List[str]] = Field(
        default_factory=dict,
        description="Subscription mapping: {agent_id: [topic1, topic2, ...]}"
    )
```

继承链：`BaseCard` → `TeamCard` → `EventDrivenTeamCard`

## 二、TeamCardInterface 接口定义

### 设计决策

| 决策项 | 选择 | 原因 |
|--------|------|------|
| 接口方法 | 完整 getter（BaseCard 层 + TeamCard 层 + GetSubscriptions） | 调用方无需类型断言即可访问所有字段 |
| GetSubscriptions 加入接口 | 是 | TeamCard 返回 nil，EventDrivenTeamCard 返回实际值，统一访问 |
| 接口定义位置 | `schema/team_card.go` | 与 TeamCard/EventDrivenTeamCard 同文件，实现方就在旁边 |

### 接口定义

```go
// TeamCardInterface 团队卡片只读接口。
//
// TeamCard 和 EventDrivenTeamCard 均实现此接口。
// BaseTeam.Card() 返回此接口类型，支持多态访问。
// 需要 Subscriptions 时，直接调用 GetSubscriptions()，
// 非 EventDrivenTeamCard 返回 nil。
type TeamCardInterface interface {
    // ── BaseCard 层 ──
    GetID() string
    GetName() string
    GetDescription() string

    // ── TeamCard 层 ──
    GetAgentCards() []*agentschema.AgentCard
    GetTopic() string
    GetVersion() string
    GetTags() []string

    // ── EventDrivenTeamCard 层（TeamCard 返回 nil）──
    GetSubscriptions() map[string][]string

    // ── 通用 ──
    String() string
}
```

## 三、BaseCard 添加 getter

在 `internal/common/schema/card.go` 中为 BaseCard 添加 getter 方法：

```go
// GetID 返回唯一标识符。
func (c *BaseCard) GetID() string { return c.ID }

// GetName 返回名称。
func (c *BaseCard) GetName() string { return c.Name }

// GetDescription 返回描述信息。
func (c *BaseCard) GetDescription() string { return c.Description }
```

BaseCard 已有 `String()` 方法，无需新增。

## 四、TeamCard 添加 getter

在 `internal/agentcore/multi_agent/schema/team_card.go` 中为 TeamCard 添加 getter 方法：

```go
// GetAgentCards 返回成员 Agent 卡片列表。
func (c *TeamCard) GetAgentCards() []*agentschema.AgentCard { return c.AgentCards }

// GetTopic 返回团队主题/领域。
func (c *TeamCard) GetTopic() string { return c.Topic }

// GetVersion 返回团队版本号。
func (c *TeamCard) GetVersion() string { return c.Version }

// GetTags 返回分类标签。
func (c *TeamCard) GetTags() []string { return c.Tags }

// GetSubscriptions 返回订阅映射。TeamCard 无订阅，返回 nil。
func (c *TeamCard) GetSubscriptions() map[string][]string { return nil }
```

TeamCard 已有 `String()` 方法，满足 TeamCardInterface 全部方法。

## 五、EventDrivenTeamCard 完整定义

### 结构体

```go
// EventDrivenTeamCard 事件驱动团队卡片，嵌入 TeamCard 并增加订阅映射。
//
// 对应 Python: openjiuwen/core/multi_agent/schema/team_card.py (EventDrivenTeamCard)
// Python 继承 TeamCard + subscriptions: Dict[str, List[str]]
//
// 满足 TeamCardInterface 接口。
type EventDrivenTeamCard struct {
    TeamCard
    // Subscriptions 订阅映射：agent_id → 订阅的 topic 列表
    //
    // 对应 Python: EventDrivenTeamCard.subscriptions: Dict[str, List[str]]
    Subscriptions map[string][]string `json:"subscriptions,omitempty"`
}
```

### 构造选项

```go
// EventDrivenTeamCardOption EventDrivenTeamCard 构造选项函数。
type EventDrivenTeamCardOption func(*EventDrivenTeamCard)
```

### 构造函数

```go
// NewEventDrivenTeamCard 创建 EventDrivenTeamCard 实例，默认 Version="1.0.0"。
//
// 对应 Python: EventDrivenTeamCard(id=uuid4().hex, name="", description="",
//     agent_cards=[], topic="", version="1.0.0", tags=[], subscriptions={})
func NewEventDrivenTeamCard(opts ...EventDrivenTeamCardOption) *EventDrivenTeamCard {
    card := &EventDrivenTeamCard{
        TeamCard: *NewTeamCard(),
    }
    for _, opt := range opts {
        opt(card)
    }
    return card
}
```

### 统一 With* 选项函数（全部返回 EventDrivenTeamCardOption）

```go
// ── BaseCard 层选项 ──

// WithEDID 设置唯一标识符。
func WithEDID(id string) EventDrivenTeamCardOption {
    return func(c *EventDrivenTeamCard) { c.ID = id }
}

// WithEDName 设置名称。
func WithEDName(name string) EventDrivenTeamCardOption {
    return func(c *EventDrivenTeamCard) { c.Name = name }
}

// WithEDDescription 设置描述信息。
func WithEDDescription(desc string) EventDrivenTeamCardOption {
    return func(c *EventDrivenTeamCard) { c.Description = desc }
}

// ── TeamCard 层选项 ──

// WithEDAgentCards 设置成员 Agent 卡片列表。
func WithEDAgentCards(cards []*agentschema.AgentCard) EventDrivenTeamCardOption {
    return func(c *EventDrivenTeamCard) { c.AgentCards = cards }
}

// WithEDTopic 设置团队主题。
func WithEDTopic(topic string) EventDrivenTeamCardOption {
    return func(c *EventDrivenTeamCard) { c.Topic = topic }
}

// WithEDTeamVersion 设置团队版本号。
func WithEDTeamVersion(version string) EventDrivenTeamCardOption {
    return func(c *EventDrivenTeamCard) { c.Version = version }
}

// WithEDTags 设置分类标签。
func WithEDTags(tags []string) EventDrivenTeamCardOption {
    return func(c *EventDrivenTeamCard) { c.Tags = tags }
}

// ── EventDrivenTeamCard 层选项 ──

// WithSubscriptions 设置订阅映射。
func WithSubscriptions(subs map[string][]string) EventDrivenTeamCardOption {
    return func(c *EventDrivenTeamCard) { c.Subscriptions = subs }
}
```

**命名说明**：With* 前缀加 `ED`（Event-Driven）以避免与同包 TeamCard 的 `WithID`/`WithName`/`WithTopic` 等函数名冲突。`WithSubscriptions` 是 EventDrivenTeamCard 独有字段，不会冲突，不加 ED 前缀。

### 方法

```go
// GetSubscriptions 返回订阅映射。
func (c *EventDrivenTeamCard) GetSubscriptions() map[string][]string {
    return c.Subscriptions
}

// String 实现 fmt.Stringer 接口，覆盖 TeamCard.String()。
//
// 对应 Python: BaseCard.to_str() 扩展，增加 subscriptions 字段
func (c *EventDrivenTeamCard) String() string {
    return fmt.Sprintf("id=%s,name=%s,topic=%s,version=%s,subscriptions=%d",
        c.ID, c.Name, c.Topic, c.Version, len(c.Subscriptions))
}
```

### 接口满足验证

```go
// 编译时验证 TeamCard 和 EventDrivenTeamCard 满足 TeamCardInterface。
var _ TeamCardInterface = (*TeamCard)(nil)
var _ TeamCardInterface = (*EventDrivenTeamCard)(nil)
```

## 六、删除 TeamCard 类型别名 re-export

### 当前状态

`team.go` 中有：
```go
type TeamCard = schema.TeamCard  // 删除此行
```

### 删除后的变更

1. **team.go**：
   - 删除 `type TeamCard = schema.TeamCard`
   - `BaseTeam.Card()` 返回类型从 `*TeamCard` 改为 `TeamCardInterface`
   - `AgentTeamProvider` 签名从 `func(ctx context.Context, card *TeamCard)` 改为 `func(ctx context.Context, card TeamCardInterface)`

2. **team_test.go**：
   - `stubTeam.card` 字段类型从 `*TeamCard` 改为 `TeamCardInterface`
   - `stubTeam.Card()` 返回类型改为 `TeamCardInterface`
   - TeamCard 构造改为 `schema.NewTeamCard(...)` + import maschema

3. **resources_manager/base.go**：
   - 新增 `import maschema ".../multi_agent/schema"`
   - `AgentTeamEntry.Card` 字段类型从 `*multiagent.TeamCard` 改为 `maschema.TeamCardInterface`

4. **resources_manager/agent_team_manager.go**：
   - 新增 `import maschema ".../multi_agent/schema"`
   - 签名中 `*multiagent.TeamCard` 改为 `maschema.TeamCardInterface`

5. **resources_manager/resource_manager.go**：
   - 新增 `import maschema ".../multi_agent/schema"`
   - `AddAgentTeam` 签名中 `*multiagents.TeamCard` 改为 `maschema.TeamCardInterface`
   - `reflect.TypeOf` 中的 `(*multiagents.TeamCard)(nil)` 改为 `(maschema.TeamCardInterface)(nil)` 或调整类型检查逻辑

6. **resources_manager/agent_team_manager_test.go**：
   - import 调整：`*multiagents.TeamCard` → `maschema.TeamCardInterface`

7. **resources_manager/resource_manager_test.go**：
   - import 调整 + 构造方式调整

### import 别名约定

所有需要引用 `multi_agent/schema` 包的文件统一使用别名 `maschema`：

```go
import maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
```

## 七、doc.go 更新

### schema/doc.go

```go
// Package schema 提供多 Agent 团队的类型定义，包括 TeamCard、EventDrivenTeamCard
// 和 TeamCardInterface 接口。
//
// 本包是 multi_agent 的子包，对应 Python 的 openjiuwen/core/multi_agent/schema/ 目录。
// TeamCard 定义团队的不可变元数据（身份、成员列表、主题、版本、标签）。
// EventDrivenTeamCard 扩展 TeamCard，增加订阅映射用于事件驱动消息路由。
// TeamCardInterface 是完整 getter 接口，TeamCard 和 EventDrivenTeamCard 均实现此接口，
// 被 BaseTeam.Card() 返回。
//
// 依赖约束：本包只依赖 common/schema/（BaseCard）和 single_agent/schema/（AgentCard），
// 不引用 multi_agent 包层的其他文件，避免循环依赖。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	└── team_card.go     # TeamCardInterface + TeamCard + EventDrivenTeamCard
//	                     # + 构造函数 + TeamCardOption/EventDrivenTeamCardOption + With* + String
//
// 对应 Python 代码：openjiuwen/core/multi_agent/schema/
package schema
```

### multi_agent/doc.go

更新文件目录：team.go 中不再有 TeamCard 类型别名，增加 EventDrivenTeamCard 相关说明。

## 八、测试覆盖

### schema/team_card_test.go 新增测试

| 测试用例 | 覆盖目标 |
|---------|---------|
| `TestTeamCardInterface_编译时接口检查` | 验证 TeamCard 和 EventDrivenTeamCard 满足 TeamCardInterface |
| `TestTeamCard_GetSubscriptions_返回nil` | TeamCard.GetSubscriptions() 返回 nil |
| `TestNewEventDrivenTeamCard_默认值` | 默认 Version="1.0.0"，Subscriptions=nil |
| `TestNewEventDrivenTeamCard_带选项` | WithEDID/WithEDName/WithEDDescription/WithEDAgentCards/WithEDTopic/WithEDTeamVersion/WithEDTags/WithSubscriptions |
| `TestEventDrivenTeamCard_GetSubscriptions` | GetSubscriptions() 返回实际订阅映射 |
| `TestEventDrivenTeamCard_String` | String() 包含 subscriptions 数量 |
| `TestEventDrivenTeamCard_JSON序列化` | JSON marshal/unmarshal，omitempty 行为 |
| `TestEventDrivenTeamCard_JSON序列化_omitempty` | 零值 Subscriptions 不出现在 JSON 中 |
| `TestEventDrivenTeamCard_嵌入TeamCard` | 嵌入后 TeamCard 字段可访问 |

### common/schema/card_test.go 新增测试

| 测试用例 | 覆盖目标 |
|---------|---------|
| `TestBaseCard_GetID` | GetID() 返回 ID 字段 |
| `TestBaseCard_GetName` | GetName() 返回 Name 字段 |
| `TestBaseCard_GetDescription` | GetDescription() 返回 Description 字段 |

### team_test.go 适配

| 测试用例 | 更新内容 |
|---------|---------|
| `TestBaseTeam_编译时接口检查` | stubTeam.card 字段类型改为 TeamCardInterface，Card() 返回类型改为 TeamCardInterface |

## 九、回填影响点

| 文件 | 回填内容 |
|------|---------|
| `common/schema/card.go` | 新增 GetID/GetName/GetDescription getter 方法 |
| `common/schema/card_test.go` | 新增 getter 测试 |
| `multi_agent/schema/team_card.go` | 新增 TeamCardInterface 接口、TeamCard getter、EventDrivenTeamCard struct + 构造 + 选项 + 方法 |
| `multi_agent/schema/team_card_test.go` | 新增 EventDrivenTeamCard 和 TeamCardInterface 测试 |
| `multi_agent/schema/doc.go` | 更新包描述和文件目录 |
| `multi_agent/team.go` | 删除 TeamCard 类型别名，Card() 返回 TeamCardInterface，AgentTeamProvider 签名改 TeamCardInterface |
| `multi_agent/team_test.go` | stubTeam 适配 TeamCardInterface |
| `multi_agent/doc.go` | 更新文件目录 |
| `resources_manager/base.go` | Card 字段改 TeamCardInterface + 新增 maschema import |
| `resources_manager/agent_team_manager.go` | 签名改 TeamCardInterface + import 调整 |
| `resources_manager/resource_manager.go` | 签名改 TeamCardInterface + import 调整 + reflect 类型检查调整 |
| `resources_manager/agent_team_manager_test.go` | import 调整 |
| `resources_manager/resource_manager_test.go` | import 调整 + 构造方式调整 |
| `IMPLEMENTATION_PLAN.md` | 8.29 状态 ☐→✅ |

## 十、声明排列（遵循编码规范）

所有文件严格按规范 2 排列：结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数。

### schema/team_card.go

```
结构体区块：  TeamCardInterface（接口排在结构体之前）
             TeamCard → TeamCardOption
             EventDrivenTeamCard → EventDrivenTeamCardOption
常量区块：    （编译时接口检查 var _ ... 放入全局变量区块）
导出函数区块：NewTeamCard → WithAgentCards → WithTopic → WithTeamVersion → WithTags
             TeamCard.GetID → TeamCard.GetName → TeamCard.GetDescription
             TeamCard.GetAgentCards → TeamCard.GetTopic → TeamCard.GetVersion
             TeamCard.GetTags → TeamCard.GetSubscriptions
             TeamCard.String
             NewEventDrivenTeamCard
             WithEDID → WithEDName → WithEDDescription
             WithEDAgentCards → WithEDTopic → WithEDTeamVersion → WithEDTags
             WithSubscriptions
             EventDrivenTeamCard.GetSubscriptions
             EventDrivenTeamCard.String
非导出函数区块：（无）
```

## 十一、日志同步

对照 Python `team_card.py`，该文件无 logger 调用，Go 侧无需补充日志。
