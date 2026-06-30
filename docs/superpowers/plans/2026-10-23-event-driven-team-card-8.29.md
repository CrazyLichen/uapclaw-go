# 8.29 EventDrivenTeamCard 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 EventDrivenTeamCard（事件驱动团队卡片），引入 TeamCardInterface 完整 getter 接口，删除 TeamCard 类型别名 re-export，全链路支持多态。

**Architecture:** EventDrivenTeamCard 嵌入 TeamCard，增加 Subscriptions 字段。新增 TeamCardInterface 接口含完整 getter（含 GetSubscriptions），TeamCard 返回 nil 而 EventDrivenTeamCard 返回实际值。删除 team.go 中的 type TeamCard = schema.TeamCard re-export，所有使用方改为 maschema.TeamCard / maschema.TeamCardInterface。

**Tech Stack:** Go 1.24+, strict coding conventions (中文注释, 声明排列顺序), functional options pattern

**Spec:** `docs/superpowers/specs/2026-10-23-event-driven-team-card-8.29-design.md`

---

## File Structure

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/common/schema/card.go` | 修改 | BaseCard 添加 GetID/GetName/GetDescription getter |
| `internal/common/schema/card_test.go` | 修改 | 新增 BaseCard getter 测试 |
| `internal/agentcore/multi_agent/schema/team_card.go` | 修改 | 新增 TeamCardInterface、TeamCard getter、EventDrivenTeamCard 完整实现 |
| `internal/agentcore/multi_agent/schema/team_card_test.go` | 修改 | 新增 EventDrivenTeamCard 和 TeamCardInterface 测试 |
| `internal/agentcore/multi_agent/schema/doc.go` | 修改 | 更新包描述和文件目录 |
| `internal/agentcore/multi_agent/team.go` | 修改 | 删除 TeamCard 类型别名、Card() 返回 TeamCardInterface、AgentTeamProvider 签名改 TeamCardInterface |
| `internal/agentcore/multi_agent/team_test.go` | 修改 | stubTeam 适配 TeamCardInterface |
| `internal/agentcore/multi_agent/doc.go` | 修改 | 更新文件目录 |
| `internal/agentcore/runner/resources_manager/base.go` | 修改 | Card 字段改 TeamCardInterface + import maschema |
| `internal/agentcore/runner/resources_manager/agent_team_manager.go` | 修改 | 签名改 TeamCardInterface + import maschema |
| `internal/agentcore/runner/resources_manager/agent_team_manager_test.go` | 修改 | import maschema + 签名改 TeamCardInterface |
| `internal/agentcore/runner/resources_manager/resource_manager.go` | 修改 | AddAgentTeam 签名改 TeamCardInterface + import maschema + reflect 调整 |
| `internal/agentcore/runner/resources_manager/resource_manager_test.go` | 修改 | import maschema + 构造方式调整 |
| `IMPLEMENTATION_PLAN.md` | 修改 | 8.29 状态 ☐→✅ |

---

### Task 1: BaseCard 添加 getter 方法

**Files:**
- Modify: `internal/common/schema/card.go:131` (在 String 方法前添加 getter)
- Test: `internal/common/schema/card_test.go`

- [ ] **Step 1: 在 card.go 中添加 GetID/GetName/GetDescription 方法**

在 `导出函数` 区块，`String()` 方法之前，添加三个 getter 方法：

```go
// GetID 返回唯一标识符。
func (c *BaseCard) GetID() string { return c.ID }

// GetName 返回名称。
func (c *BaseCard) GetName() string { return c.Name }

// GetDescription 返回描述信息。
func (c *BaseCard) GetDescription() string { return c.Description }
```

- [ ] **Step 2: 在 card_test.go 中添加 getter 测试**

在文件末尾（`TestAbilityKind_String` 之后）添加：

```go
// TestBaseCard_GetID 验证 GetID() 返回 ID 字段。
func TestBaseCard_GetID(t *testing.T) {
	card := NewBaseCard(WithID("test-id"))
	if got := card.GetID(); got != "test-id" {
		t.Errorf("GetID() = %q, want %q", got, "test-id")
	}
}

// TestBaseCard_GetName 验证 GetName() 返回 Name 字段。
func TestBaseCard_GetName(t *testing.T) {
	card := NewBaseCard(WithName("test-name"))
	if got := card.GetName(); got != "test-name" {
		t.Errorf("GetName() = %q, want %q", got, "test-name")
	}
}

// TestBaseCard_GetDescription 验证 GetDescription() 返回 Description 字段。
func TestBaseCard_GetDescription(t *testing.T) {
	card := NewBaseCard(WithDescription("测试描述"))
	if got := card.GetDescription(); got != "测试描述" {
		t.Errorf("GetDescription() = %q, want %q", got, "测试描述")
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/common/schema/ -v -run "TestBaseCard_Get"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/common/schema/card.go internal/common/schema/card_test.go
git commit -m "feat(common): 为 BaseCard 添加 GetID/GetName/GetDescription getter 方法

8.29 前置：TeamCardInterface 需要 BaseCard 层的 getter 方法。
对应 Python: BaseCard.id/name/description 字段的只读访问。"
```

---

### Task 2: TeamCard 添加 getter 方法 + TeamCardInterface 接口定义

**Files:**
- Modify: `internal/agentcore/multi_agent/schema/team_card.go`
- Test: `internal/agentcore/multi_agent/schema/team_card_test.go`

- [ ] **Step 1: 在 team_card.go 中添加 TeamCardInterface 接口和 TeamCard getter**

在 `结构体` 区块开头（TeamCard struct 定义之前）添加 TeamCardInterface 接口：

```go
// TeamCardInterface 团队卡片只读接口。
//
// TeamCard 和 EventDrivenTeamCard 均实现此接口。
// BaseTeam.Card() 返回此接口类型，支持多态访问。
// 需要 Subscriptions 时，直接调用 GetSubscriptions()，
// 非 EventDrivenTeamCard 返回 nil。
//
// 对应 Python: BaseTeam.card 属性的类型声明 TeamCard（Python 运行时允许 TeamCard 子类实例）。
// Go 中用接口实现多态，Python 中用继承实现。
type TeamCardInterface interface {
	// ── BaseCard 层 ──

	// GetID 返回唯一标识符
	GetID() string
	// GetName 返回名称
	GetName() string
	// GetDescription 返回描述信息
	GetDescription() string

	// ── TeamCard 层 ──

	// GetAgentCards 返回成员 Agent 卡片列表
	GetAgentCards() []*agentschema.AgentCard
	// GetTopic 返回团队主题/领域
	GetTopic() string
	// GetVersion 返回团队版本号
	GetVersion() string
	// GetTags 返回分类标签
	GetTags() []string

	// ── EventDrivenTeamCard 层（TeamCard 返回 nil）──

	// GetSubscriptions 返回订阅映射。
	// TeamCard 实现返回 nil；EventDrivenTeamCard 实现返回实际值。
	GetSubscriptions() map[string][]string

	// ── 通用 ──

	// String 返回简洁的身份描述
	String() string
}
```

在 TeamCard struct 定义之后、TeamCardOption 定义之前，保持 TeamCardOption 在 TeamCard 同一区块。

在 `导出函数` 区块，`String()` 方法之后添加 TeamCard 的 getter 方法：

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

在 `全局变量` 区块添加编译时接口检查：

```go
// 编译时验证 TeamCard 满足 TeamCardInterface。
var _ TeamCardInterface = (*TeamCard)(nil)
```

- [ ] **Step 2: 在 team_card_test.go 中添加 TeamCardInterface 和 getter 测试**

添加编译时接口检查测试和 getter 测试：

```go
// TestTeamCardInterface_TeamCard满足接口 验证 *TeamCard 满足 TeamCardInterface。
func TestTeamCardInterface_TeamCard满足接口(t *testing.T) {
	card := NewTeamCard(WithTopic("test"))
	var iface TeamCardInterface = card
	if iface.GetTopic() != "test" {
		t.Errorf("GetTopic() = %q, want %q", iface.GetTopic(), "test")
	}
	if iface.GetSubscriptions() != nil {
		t.Error("TeamCard.GetSubscriptions() 应返回 nil")
	}
}

// TestTeamCard_GetSubscriptions_返回nil 验证 TeamCard.GetSubscriptions() 返回 nil。
func TestTeamCard_GetSubscriptions_返回nil(t *testing.T) {
	card := NewTeamCard()
	if subs := card.GetSubscriptions(); subs != nil {
		t.Errorf("期望 nil，实际 %v", subs)
	}
}

// TestTeamCard_GetAgentCards 验证 GetAgentCards() 返回 AgentCards 字段。
func TestTeamCard_GetAgentCards(t *testing.T) {
	agentCard := agentschema.NewAgentCard(schema.WithName("a1"))
	card := NewTeamCard(WithAgentCards([]*agentschema.AgentCard{agentCard}))
	if got := card.GetAgentCards(); len(got) != 1 || got[0].Name != "a1" {
		t.Errorf("GetAgentCards() = %v, want 1 个 agent a1", got)
	}
}

// TestTeamCard_GetTopic 验证 GetTopic() 返回 Topic 字段。
func TestTeamCard_GetTopic(t *testing.T) {
	card := NewTeamCard(WithTopic("math"))
	if got := card.GetTopic(); got != "math" {
		t.Errorf("GetTopic() = %q, want %q", got, "math")
	}
}

// TestTeamCard_GetVersion 验证 GetVersion() 返回 Version 字段。
func TestTeamCard_GetVersion(t *testing.T) {
	card := NewTeamCard(WithTeamVersion("2.0.0"))
	if got := card.GetVersion(); got != "2.0.0" {
		t.Errorf("GetVersion() = %q, want %q", got, "2.0.0")
	}
}

// TestTeamCard_GetTags 验证 GetTags() 返回 Tags 字段。
func TestTeamCard_GetTags(t *testing.T) {
	card := NewTeamCard(WithTags([]string{"ai", "ml"}))
	if got := card.GetTags(); len(got) != 2 || got[0] != "ai" {
		t.Errorf("GetTags() = %v, want [ai ml]", got)
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/schema/ -v -run "TestTeamCardInterface|TestTeamCard_Get"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/schema/team_card.go internal/agentcore/multi_agent/schema/team_card_test.go
git commit -m "feat(multi_agent): 添加 TeamCardInterface 接口和 TeamCard getter 方法

8.29：定义 TeamCardInterface 完整 getter 接口（含 GetSubscriptions），
TeamCard 实现此接口，GetSubscriptions() 返回 nil。
为 TeamCard 添加 GetAgentCards/GetTopic/GetVersion/GetTags/GetSubscriptions。"
```

---

### Task 3: EventDrivenTeamCard 完整实现

**Files:**
- Modify: `internal/agentcore/multi_agent/schema/team_card.go`
- Test: `internal/agentcore/multi_agent/schema/team_card_test.go`

- [ ] **Step 1: 在 team_card.go 中添加 EventDrivenTeamCard 结构体和构造函数**

在 TeamCard 的 getter 方法之后、非导出函数区块之前，添加 EventDrivenTeamCard 的完整实现。

**结构体区块**（在 TeamCardOption 之后添加）：

```go
// EventDrivenTeamCard 事件驱动团队卡片，嵌入 TeamCard 并增加订阅映射。
//
// 不可变元数据 + 声明式订阅关系，描述"团队是什么"和"谁关心什么事件"。
// AgentCards 仅存储成员 Agent 的卡片（元数据），不是 Agent 实例。
//
// 满足 TeamCardInterface 接口。
//
// 对应 Python: openjiuwen/core/multi_agent/schema/team_card.py (EventDrivenTeamCard)
// Python 继承 TeamCard + subscriptions: Dict[str, List[str]]
type EventDrivenTeamCard struct {
	TeamCard
	// Subscriptions 订阅映射：agent_id → 订阅的 topic 列表
	//
	// 对应 Python: EventDrivenTeamCard.subscriptions: Dict[str, List[str]] = Field(default_factory=dict)
	Subscriptions map[string][]string `json:"subscriptions,omitempty"`
}

// EventDrivenTeamCardOption EventDrivenTeamCard 构造选项函数。
type EventDrivenTeamCardOption func(*EventDrivenTeamCard)
```

**全局变量区块**（添加编译时检查）：

```go
// 编译时验证 EventDrivenTeamCard 满足 TeamCardInterface。
var _ TeamCardInterface = (*EventDrivenTeamCard)(nil)
```

**导出函数区块**（在 TeamCard getter 之后添加）：

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

// GetSubscriptions 返回订阅映射。
func (c *EventDrivenTeamCard) GetSubscriptions() map[string][]string {
	return c.Subscriptions
}

// String 实现 fmt.Stringer 接口，覆盖 TeamCard.String()。
//
// 对应 Python: BaseCard.to_str() 扩展，增加 subscriptions 数量字段
func (c *EventDrivenTeamCard) String() string {
	return fmt.Sprintf("id=%s,name=%s,topic=%s,version=%s,subscriptions=%d",
		c.ID, c.Name, c.Topic, c.Version, len(c.Subscriptions))
}
```

- [ ] **Step 2: 在 team_card_test.go 中添加 EventDrivenTeamCard 测试**

```go
// TestNewEventDrivenTeamCard_默认值 验证默认 Version="1.0.0"，Subscriptions=nil。
func TestNewEventDrivenTeamCard_默认值(t *testing.T) {
	card := NewEventDrivenTeamCard()
	if card.Version != "1.0.0" {
		t.Errorf("期望 Version='1.0.0'，实际 '%s'", card.Version)
	}
	if card.Subscriptions != nil {
		t.Errorf("期望 Subscriptions=nil，实际 %v", card.Subscriptions)
	}
	if card.Topic != "" {
		t.Errorf("期望 Topic=''，实际 '%s'", card.Topic)
	}
	if card.ID == "" {
		t.Error("期望 ID 非空")
	}
}

// TestNewEventDrivenTeamCard_带选项 验证所有 EventDrivenTeamCardOption。
func TestNewEventDrivenTeamCard_带选项(t *testing.T) {
	agentCard := agentschema.NewAgentCard(schema.WithName("agent1"))
	subs := map[string][]string{
		"reviewer": {"code_events", "task_updates"},
		"coder":    {"review_events"},
	}
	card := NewEventDrivenTeamCard(
		WithEDID("team-123"),
		WithEDName("event-team"),
		WithEDDescription("事件驱动团队"),
		WithEDAgentCards([]*agentschema.AgentCard{agentCard}),
		WithEDTopic("coding"),
		WithEDTeamVersion("2.0.0"),
		WithEDTags([]string{"event", "driven"}),
		WithSubscriptions(subs),
	)
	if card.ID != "team-123" {
		t.Errorf("期望 ID='team-123'，实际 '%s'", card.ID)
	}
	if card.Name != "event-team" {
		t.Errorf("期望 Name='event-team'，实际 '%s'", card.Name)
	}
	if card.Description != "事件驱动团队" {
		t.Errorf("期望 Description='事件驱动团队'，实际 '%s'", card.Description)
	}
	if len(card.AgentCards) != 1 || card.AgentCards[0].Name != "agent1" {
		t.Errorf("期望 AgentCards[0].Name='agent1'，实际 %v", card.AgentCards)
	}
	if card.Topic != "coding" {
		t.Errorf("期望 Topic='coding'，实际 '%s'", card.Topic)
	}
	if card.Version != "2.0.0" {
		t.Errorf("期望 Version='2.0.0'，实际 '%s'", card.Version)
	}
	if len(card.Tags) != 2 || card.Tags[0] != "event" {
		t.Errorf("期望 Tags=['event','driven']，实际 %v", card.Tags)
	}
	if len(card.Subscriptions) != 2 {
		t.Errorf("期望 len(Subscriptions)=2，实际 %d", len(card.Subscriptions))
	}
	if topics, ok := card.Subscriptions["reviewer"]; !ok || len(topics) != 2 {
		t.Errorf("期望 reviewer 订阅 2 个 topic，实际 %v", topics)
	}
}

// TestEventDrivenTeamCard_GetSubscriptions 验证 GetSubscriptions() 返回实际订阅映射。
func TestEventDrivenTeamCard_GetSubscriptions(t *testing.T) {
	subs := map[string][]string{"agent1": {"topic1"}}
	card := NewEventDrivenTeamCard(WithSubscriptions(subs))
	if got := card.GetSubscriptions(); len(got) != 1 || got["agent1"][0] != "topic1" {
		t.Errorf("GetSubscriptions() = %v, want agent1→[topic1]", got)
	}
}

// TestEventDrivenTeamCard_String 验证 String() 包含 subscriptions 数量。
func TestEventDrivenTeamCard_String(t *testing.T) {
	subs := map[string][]string{"a": {"t1"}, "b": {"t2"}}
	card := NewEventDrivenTeamCard(
		WithEDName("team1"),
		WithEDTopic("math"),
		WithSubscriptions(subs),
	)
	s := fmt.Sprintf("%v", card)
	if !contains(s, "team1") {
		t.Errorf("String() 应包含 Name='team1'，实际 '%s'", s)
	}
	if !contains(s, "math") {
		t.Errorf("String() 应包含 Topic='math'，实际 '%s'", s)
	}
	if !contains(s, "subscriptions=2") {
		t.Errorf("String() 应包含 subscriptions=2，实际 '%s'", s)
	}
}

// TestEventDrivenTeamCard_JSON序列化 验证 JSON marshal/unmarshal。
func TestEventDrivenTeamCard_JSON序列化(t *testing.T) {
	subs := map[string][]string{"reviewer": {"code_events"}}
	card := NewEventDrivenTeamCard(
		WithEDName("event-team"),
		WithEDTopic("coding"),
		WithEDTeamVersion("2.0.0"),
		WithSubscriptions(subs),
	)
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var decoded EventDrivenTeamCard
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if decoded.Name != "event-team" {
		t.Errorf("期望 Name='event-team'，实际 '%s'", decoded.Name)
	}
	if decoded.Topic != "coding" {
		t.Errorf("期望 Topic='coding'，实际 '%s'", decoded.Topic)
	}
	if len(decoded.Subscriptions) != 1 || decoded.Subscriptions["reviewer"][0] != "code_events" {
		t.Errorf("期望 Subscriptions[reviewer]=[code_events]，实际 %v", decoded.Subscriptions)
	}
}

// TestEventDrivenTeamCard_JSON序列化_omitempty 验证零值 Subscriptions 不出现在 JSON 中。
func TestEventDrivenTeamCard_JSON序列化_omitempty(t *testing.T) {
	card := NewEventDrivenTeamCard() // Subscriptions 为 nil
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal map 失败: %v", err)
	}
	if _, ok := m["subscriptions"]; ok {
		t.Error("零值 Subscriptions 不应出现在 JSON 中")
	}
}

// TestEventDrivenTeamCard_嵌入TeamCard 验证嵌入后 TeamCard 字段可访问。
func TestEventDrivenTeamCard_嵌入TeamCard(t *testing.T) {
	card := NewEventDrivenTeamCard(
		WithEDID("abc123"),
		WithEDName("n"),
		WithEDDescription("d"),
	)
	if card.ID != "abc123" {
		t.Errorf("期望 ID='abc123'，实际 '%s'", card.ID)
	}
	if card.Name != "n" {
		t.Errorf("期望 Name='n'，实际 '%s'", card.Name)
	}
	if card.Description != "d" {
		t.Errorf("期望 Description='d'，实际 '%s'", card.Description)
	}
}

// TestTeamCardInterface_EventDrivenTeamCard满足接口 验证 *EventDrivenTeamCard 满足 TeamCardInterface。
func TestTeamCardInterface_EventDrivenTeamCard满足接口(t *testing.T) {
	subs := map[string][]string{"agent1": {"topic1"}}
	card := NewEventDrivenTeamCard(
		WithEDName("ed-team"),
		WithEDTopic("coding"),
		WithSubscriptions(subs),
	)
	var iface TeamCardInterface = card
	if iface.GetName() != "ed-team" {
		t.Errorf("GetName() = %q, want %q", iface.GetName(), "ed-team")
	}
	if iface.GetTopic() != "coding" {
		t.Errorf("GetTopic() = %q, want %q", iface.GetTopic(), "coding")
	}
	if got := iface.GetSubscriptions(); len(got) != 1 || got["agent1"][0] != "topic1" {
		t.Errorf("GetSubscriptions() = %v, want agent1→[topic1]", got)
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/schema/ -v -run "TestNewEventDrivenTeamCard|TestEventDrivenTeamCard|TestTeamCardInterface_EventDrivenTeamCard"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/schema/team_card.go internal/agentcore/multi_agent/schema/team_card_test.go
git commit -m "feat(multi_agent): 实现 EventDrivenTeamCard 事件驱动团队卡片

8.29：EventDrivenTeamCard 嵌入 TeamCard，增加 Subscriptions map[string][]string。
新增 NewEventDrivenTeamCard + EventDrivenTeamCardOption + WithED* 选项函数。
实现 GetSubscriptions() 和覆盖 String()。
编译时验证满足 TeamCardInterface。"
```

---

### Task 4: 删除 TeamCard 类型别名，更新 BaseTeam 接口

**Files:**
- Modify: `internal/agentcore/multi_agent/team.go`
- Modify: `internal/agentcore/multi_agent/team_test.go`

- [ ] **Step 1: 修改 team.go**

1. 删除 `type TeamCard = schema.TeamCard` 类型别名定义及其注释
2. 修改 `BaseTeam` 接口中 `Card()` 返回类型：`Card() *TeamCard` → `Card() schema.TeamCardInterface`
3. 修改 `AgentTeamProvider` 签名：`func(ctx context.Context, card *TeamCard) (BaseTeam, error)` → `func(ctx context.Context, card schema.TeamCardInterface) (BaseTeam, error)`
4. 删除旧注释中关于"类型别名保持外部 API 兼容"的说明
5. 确保 import 中有 `"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"`

具体变更：
- 删除第 15–21 行（TeamCard 类型别名及其注释块）
- `Card() *TeamCard` → `Card() schema.TeamCardInterface`
- `AgentTeamProvider func(ctx context.Context, card *TeamCard)` → `AgentTeamProvider func(ctx context.Context, card schema.TeamCardInterface)`

- [ ] **Step 2: 修改 team_test.go**

1. 修改 `stubTeam` 结构体：`card *TeamCard` → `card schema.TeamCardInterface`
2. 修改 `stubTeam.Card()` 返回类型：`func (t *stubTeam) Card() *TeamCard` → `func (t *stubTeam) Card() schema.TeamCardInterface`
3. 确保 import 中有 `"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"`
4. `TestBaseTeam_编译时接口检查` 中的 `schema.NewTeamCard` 调用返回 `*schema.TeamCard`，它满足 `schema.TeamCardInterface`，可以直接赋值

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/ -v -run "TestBaseTeam"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/team.go internal/agentcore/multi_agent/team_test.go
git commit -m "refactor(multi_agent): 删除 TeamCard 类型别名，Card() 返回 TeamCardInterface

8.29：BaseTeam.Card() 返回类型从 *TeamCard 改为 schema.TeamCardInterface。
AgentTeamProvider 签名中 card 参数从 *TeamCard 改为 schema.TeamCardInterface。
删除 team.go 中的 type TeamCard = schema.TeamCard re-export。"
```

---

### Task 5: 更新 resources_manager 使用 TeamCardInterface

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/base.go`
- Modify: `internal/agentcore/runner/resources_manager/agent_team_manager.go`
- Modify: `internal/agentcore/runner/resources_manager/agent_team_manager_test.go`
- Modify: `internal/agentcore/runner/resources_manager/resource_manager.go`
- Modify: `internal/agentcore/runner/resources_manager/resource_manager_test.go`

- [ ] **Step 1: 修改 base.go**

1. 新增 import：`maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"`
2. `AgentTeamEntry.Card` 字段类型：`*multiagent.TeamCard` → `maschema.TeamCardInterface`
3. `AgentTeamEntry.Provider` 字段类型：`multiagent.AgentTeamProvider`（不变，AgentTeamProvider 定义在 multi_agent 包层）
4. 如果 `multiagent` import 不再被其他代码引用则删除（检查 base.go 中是否还引用 multiagent.BaseTeam 等——当前不被引用，但 `AgentTeamEntry.Provider` 仍是 `multiagent.AgentTeamProvider`，所以保留 multiagent import）

- [ ] **Step 2: 修改 agent_team_manager.go**

1. 新增 import：`maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"`
2. `RemoveAgentTeam` 返回的 provider lambda 签名：`func(ctx context.Context, card *multiagent.TeamCard)` → `func(ctx context.Context, card maschema.TeamCardInterface)`

- [ ] **Step 3: 修改 agent_team_manager_test.go**

1. 将 import `multiagents "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent"` 替换为 `maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"` 和 `multiagents "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent"`（需要保留 multiagents 因为 BaseTeam 类型仍在使用）
2. 所有 `*multiagents.TeamCard` → `maschema.TeamCardInterface`（在 provider lambda 签名中）

- [ ] **Step 4: 修改 resource_manager.go**

1. 新增 import：`maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"`
2. `AddAgentTeam` 签名：`card *multiagents.TeamCard` → `card maschema.TeamCardInterface`
3. `AddAgentTeam` 内部 `&card.BaseCard` 需要调整：TeamCardInterface 没有直接的 BaseCard 字段，改为 `card.GetID()` 获取 ID，用 `innerAddResource` 时 BaseCard 部分需要重新构造或调整函数签名。
   - 当前调用：`m.innerAddResource(card.ID, "team", provider, &card.BaseCard, "", "")`
   - 改为：`m.innerAddResource(card.GetID(), "team", provider, nil, "", "")` —— 但 innerAddResource 需要 *BaseCard 参数
   - 需要检查 innerAddResource 对 *BaseCard 的使用。如果仅用于 ID 获取，则可直接用 `card.GetID()`。如果需要完整 BaseCard（如 Name/Description 用于 tag 或日志），则需要构造一个临时 BaseCard。
   - **解决方案**：在 AddAgentTeam 中构造临时 BaseCard：`baseCard := &schema.BaseCard{ID: card.GetID(), Name: card.GetName(), Description: card.GetDescription()}`
4. `reflect.TypeOf((*multiagents.TeamCard)(nil))` case 需要调整。TeamCardInterface 是接口类型，reflect.TypeOf 对接口返回的是动态类型。改为检查 TeamCard 具体类型：`reflect.TypeOf((*maschema.TeamCard)(nil))` 和 `reflect.TypeOf((*maschema.EventDrivenTeamCard)(nil))`。或者在 `getCardType` 函数中增加 EventDrivenTeamCard 的 case。
   - 当前 getCardType 函数接收 `any` 参数（card 实例），用 `reflect.TypeOf` 判断类型。TeamCardInterface 传入后 reflect.TypeOf 返回的是动态类型（*TeamCard 或 *EventDrivenTeamCard），所以需要两个 case：
     - `reflect.TypeOf((*maschema.TeamCard)(nil))` → "team"
     - `reflect.TypeOf((*maschema.EventDrivenTeamCard)(nil))` → "team"

- [ ] **Step 5: 修改 resource_manager_test.go**

1. 新增 import：`maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"`
2. `&multiagents.TeamCard{BaseCard: schema.BaseCard{ID: "team-1", Name: "test-team"}}` → `maschema.NewTeamCard(maschema.WithID("team-1"), maschema.WithName("test-team"))`（改为用构造函数，因为 TeamCardInterface 无法用字面量构造）

- [ ] **Step 6: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/ -v -run "TestAgentTeamMgr|TestResourceMgr_AddAgentTeam|TestResourceMgr_RemoveAgentTeam|TestResourceMgr_GetAgentTeam"`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/runner/resources_manager/base.go \
  internal/agentcore/runner/resources_manager/agent_team_manager.go \
  internal/agentcore/runner/resources_manager/agent_team_manager_test.go \
  internal/agentcore/runner/resources_manager/resource_manager.go \
  internal/agentcore/runner/resources_manager/resource_manager_test.go
git commit -m "refactor(resources_manager): TeamCard 引用改为 maschema.TeamCardInterface

8.29：resources_manager 中 TeamCard 相关字段/参数改为 TeamCardInterface。
AddAgentTeam 签名从 *TeamCard 改为 TeamCardInterface。
getCardType 增加 EventDrivenTeamCard case。
import 新增 maschema 别名。"
```

---

### Task 6: 更新 doc.go 文件

**Files:**
- Modify: `internal/agentcore/multi_agent/schema/doc.go`
- Modify: `internal/agentcore/multi_agent/doc.go`

- [ ] **Step 1: 更新 schema/doc.go**

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

- [ ] **Step 2: 更新 multi_agent/doc.go**

删除对 TeamCard 类型别名的提及，更新文件目录描述，说明 Card() 返回 TeamCardInterface。

```go
// Package multi_agent 提供多 Agent 团队的核心抽象与运行时基础设施。
//
// 定义 BaseTeam 接口作为多 Agent 团队体系的根基契约，
// 具体团队实现（HandoffTeam、HierarchicalTeam）均实现此接口。
// 团队内部的 Agent 通信通过 TeamRuntime（8.30）的 P2P 和 Pub-Sub 消息机制完成。
// BaseTeam.Card() 返回 schema.TeamCardInterface，支持 TeamCard 和 EventDrivenTeamCard 多态访问。
//
// 文件目录：
//
//	multi_agent/
//	├── doc.go              # 包文档
//	├── config.go           # TeamConfig 团队运行时配置 + 链式配置方法
//	├── team.go             # BaseTeam 接口 + AgentTeamProvider/TeamAgentProvider
//	├── team_option.go      # TeamOptions 结构体 + TeamOption 函数类型 + WithXxx
//	└── schema/
//	    ├── doc.go           # schema 子包文档
//	    └── team_card.go    # TeamCardInterface + TeamCard + EventDrivenTeamCard + 构造函数 + 选项 + String
//
// 对应 Python 代码：openjiuwen/core/multi_agent/
package multi_agent
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/schema/doc.go internal/agentcore/multi_agent/doc.go
git commit -m "docs(multi_agent): 更新 doc.go 文件，增加 EventDrivenTeamCard 和 TeamCardInterface 描述

8.29：schema/doc.go 增加 EventDrivenTeamCard 和 TeamCardInterface 说明。
multi_agent/doc.go 删除 TeamCard 类型别名提及，更新 Card() 返回类型说明。"
```

---

### Task 7: 全量编译验证 + IMPLEMENTATION_PLAN 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 运行全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译成功，无错误

- [ ] **Step 2: 运行受影响包的全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/common/schema/ ./internal/agentcore/multi_agent/... ./internal/agentcore/runner/resources_manager/ -v -count=1`
Expected: 全部 PASS

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 8.29 行的状态从 `☐` 改为 `✅`：

`| 8.29 | ☐ | EventDrivenTeamCard | 事件驱动团队卡片 |` → `| 8.29 | ✅ | EventDrivenTeamCard | 事件驱动团队卡片 |`

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore: 更新 IMPLEMENTATION_PLAN 8.29 状态为 ✅

8.29 EventDrivenTeamCard 实现完成。"
```
