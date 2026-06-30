# 8.28 TeamCard / TeamConfig 完整实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整实现 TeamCard（团队身份卡片）和 TeamConfig（团队运行时配置），严格对齐 Python 源码的字段定义和文件组织结构。

**Architecture:** 建立 `multi_agent/schema/` 子包放置 TeamCard，新建 `config.go` 放置 TeamConfig，从 `team.go` 移除占位定义并通过类型别名保持 API 兼容。TDD 方式：先写测试再实现。

**Tech Stack:** Go 1.26, 标准库 `encoding/json`、`fmt`，项目内 `common/schema`（BaseCard）、`single_agent/schema`（AgentCard）

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 新建 | `internal/agentcore/multi_agent/schema/team_card.go` | TeamCard 结构体 + 构造函数 + 选项函数 + String |
| 新建 | `internal/agentcore/multi_agent/schema/team_card_test.go` | TeamCard 单元测试 |
| 新建 | `internal/agentcore/multi_agent/schema/doc.go` | schema 子包文档 |
| 新建 | `internal/agentcore/multi_agent/config.go` | TeamConfig 结构体 + 构造函数 + 链式配置方法 + Extra 辅助 |
| 新建 | `internal/agentcore/multi_agent/config_test.go` | TeamConfig 单元测试 |
| 修改 | `internal/agentcore/multi_agent/team.go` | 移除 TeamCard/TeamConfig 占位定义，添加类型别名 + import |
| 修改 | `internal/agentcore/multi_agent/team_test.go` | 更新 TeamCard/TeamConfig 构造方式 |
| 修改 | `internal/agentcore/multi_agent/doc.go` | 更新文件目录和职责描述 |

---

### Task 1: 新建 schema/team_card.go — TeamCard 完整实现

**Files:**
- Create: `internal/agentcore/multi_agent/schema/team_card.go`
- Test: `internal/agentcore/multi_agent/schema/team_card_test.go`

- [ ] **Step 1: 创建 schema 子目录**

```bash
mkdir -p /home/opensource/uap-claw-go/internal/agentcore/multi_agent/schema
```

- [ ] **Step 2: 编写 team_card_test.go 测试文件**

```go
package schema

import (
	"encoding/json"
	"fmt"
	"testing"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTeamCard_默认值 验证默认 Version="1.0.0"，其他字段为零值。
func TestNewTeamCard_默认值(t *testing.T) {
	card := NewTeamCard()
	if card.Version != "1.0.0" {
		t.Errorf("期望 Version='1.0.0'，实际 '%s'", card.Version)
	}
	if card.Topic != "" {
		t.Errorf("期望 Topic=''，实际 '%s'", card.Topic)
	}
	if card.AgentCards != nil {
		t.Errorf("期望 AgentCards=nil，实际 %v", card.AgentCards)
	}
	if card.Tags != nil {
		t.Errorf("期望 Tags=nil，实际 %v", card.Tags)
	}
	// BaseCard 字段
	if card.ID == "" {
		t.Error("期望 ID 非空")
	}
}

// TestNewTeamCard_带选项 验证所有 TeamCardOption。
func TestNewTeamCard_带选项(t *testing.T) {
	agentCard := agentschema.NewAgentCard(schema.WithName("agent1"))
	cards := []*agentschema.AgentCard{agentCard}

	card := NewTeamCard(
		schema.WithName("my-team"),
		schema.WithDescription("测试团队"),
		WithAgentCards(cards),
		WithTopic("coding"),
		WithTeamVersion("2.0.0"),
		WithTags([]string{"tag1", "tag2"}),
	)

	if card.Name != "my-team" {
		t.Errorf("期望 Name='my-team'，实际 '%s'", card.Name)
	}
	if card.Description != "测试团队" {
		t.Errorf("期望 Description='测试团队'，实际 '%s'", card.Description)
	}
	if len(card.AgentCards) != 1 {
		t.Fatalf("期望 len(AgentCards)=1，实际 %d", len(card.AgentCards))
	}
	if card.AgentCards[0].Name != "agent1" {
		t.Errorf("期望 AgentCards[0].Name='agent1'，实际 '%s'", card.AgentCards[0].Name)
	}
	if card.Topic != "coding" {
		t.Errorf("期望 Topic='coding'，实际 '%s'", card.Topic)
	}
	if card.Version != "2.0.0" {
		t.Errorf("期望 Version='2.0.0'，实际 '%s'", card.Version)
	}
	if len(card.Tags) != 2 || card.Tags[0] != "tag1" {
		t.Errorf("期望 Tags=['tag1','tag2']，实际 %v", card.Tags)
	}
}

// TestTeamCard_String 验证 fmt.Stringer 输出。
func TestTeamCard_String(t *testing.T) {
	card := NewTeamCard(
		schema.WithName("team1"),
		WithTopic("math"),
		WithTeamVersion("1.0.0"),
	)
	s := fmt.Sprintf("%v", card)
	if s == "" {
		t.Error("String() 不应返回空字符串")
	}
	// 验证包含关键字段
	if card.ID != "" && !contains(s, card.ID) {
		t.Errorf("String() 应包含 ID='%s'，实际 '%s'", card.ID, s)
	}
	if !contains(s, "team1") {
		t.Errorf("String() 应包含 Name='team1'，实际 '%s'", s)
	}
	if !contains(s, "math") {
		t.Errorf("String() 应包含 Topic='math'，实际 '%s'", s)
	}
}

// TestTeamCard_JSON序列化 验证 JSON marshal/unmarshal 和 omitempty。
func TestTeamCard_JSON序列化(t *testing.T) {
	card := NewTeamCard(
		schema.WithName("team1"),
		WithTopic("coding"),
		WithTeamVersion("2.0.0"),
		WithTags([]string{"ai"}),
	)

	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded TeamCard
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.Name != "team1" {
		t.Errorf("期望 Name='team1'，实际 '%s'", decoded.Name)
	}
	if decoded.Topic != "coding" {
		t.Errorf("期望 Topic='coding'，实际 '%s'", decoded.Topic)
	}
	if decoded.Version != "2.0.0" {
		t.Errorf("期望 Version='2.0.0'，实际 '%s'", decoded.Version)
	}
	if len(decoded.Tags) != 1 || decoded.Tags[0] != "ai" {
		t.Errorf("期望 Tags=['ai']，实际 %v", decoded.Tags)
	}
}

// TestTeamCard_JSON序列化_omitempty 验证零值字段不出现在 JSON 中。
func TestTeamCard_JSON序列化_omitempty(t *testing.T) {
	card := NewTeamCard() // Topic/Tags/AgentCards 全零值
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal map 失败: %v", err)
	}
	if _, ok := m["agent_cards"]; ok {
		t.Error("零值 AgentCards 不应出现在 JSON 中")
	}
	if _, ok := m["topic"]; ok {
		t.Error("零值 Topic 不应出现在 JSON 中")
	}
	if _, ok := m["tags"]; ok {
		t.Error("零值 Tags 不应出现在 JSON 中")
	}
}

// TestTeamCard_嵌入BaseCard 验证嵌入后 ID/Name/Description 可访问。
func TestTeamCard_嵌入BaseCard(t *testing.T) {
	card := NewTeamCard(schema.WithID("abc123"), schema.WithName("n"), schema.WithDescription("d"))
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// contains 检查字符串是否包含子串。
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && findSubstring(s, sub)))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: 运行测试验证编译失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/schema/... 2>&1 | head -20
```

预期：编译失败，`NewTeamCard`、`TeamCard`、`WithAgentCards` 等未定义。

- [ ] **Step 4: 编写 team_card.go 实现文件**

```go
package schema

import (
	"fmt"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamCard 团队身份卡片，嵌入 BaseCard 提供统一身份标识。
//
// 不可变元数据，描述团队的"身份"和"组成"。
// AgentCards 仅存储成员 Agent 的卡片（元数据），不是 Agent 实例。
//
// 对应 Python: openjiuwen/core/multi_agent/schema/team_card.py (TeamCard)
// Python 继承 BaseCard: id/name/description + agent_cards/topic/version/tags
type TeamCard struct {
	schema.BaseCard
	// AgentCards 成员 Agent 的卡片列表（仅元数据，非实例）
	AgentCards []*agentschema.AgentCard `json:"agent_cards,omitempty"`
	// Topic 团队主题/领域
	Topic string `json:"topic,omitempty"`
	// Version 团队版本号
	Version string `json:"version,omitempty"`
	// Tags 分类标签
	Tags []string `json:"tags,omitempty"`
}

// TeamCardOption TeamCard 构造选项函数。
type TeamCardOption func(*TeamCard)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamCard 创建 TeamCard 实例，默认 Version="1.0.0"。
//
// 对应 Python: TeamCard(id=uuid4().hex, name="", description="", agent_cards=[], topic="", version="1.0.0", tags=[])
func NewTeamCard(opts ...any) *TeamCard {
	card := &TeamCard{
		BaseCard: *schema.NewBaseCard(),
		Version:  "1.0.0",
	}
	for _, opt := range opts {
		switch o := opt.(type) {
		case schema.CardOption:
			o(&card.BaseCard)
		case TeamCardOption:
			o(card)
		}
	}
	return card
}

// WithAgentCards 设置成员 Agent 卡片列表。
func WithAgentCards(cards []*agentschema.AgentCard) TeamCardOption {
	return func(c *TeamCard) { c.AgentCards = cards }
}

// WithTopic 设置团队主题。
func WithTopic(topic string) TeamCardOption {
	return func(c *TeamCard) { c.Topic = topic }
}

// WithTeamVersion 设置团队版本号。
func WithTeamVersion(version string) TeamCardOption {
	return func(c *TeamCard) { c.Version = version }
}

// WithTags 设置分类标签。
func WithTags(tags []string) TeamCardOption {
	return func(c *TeamCard) { c.Tags = tags }
}

// String 实现 fmt.Stringer 接口，返回简洁的身份描述。
//
// 对应 Python: BaseCard.to_str() 扩展
func (c *TeamCard) String() string {
	return fmt.Sprintf("id=%s,name=%s,topic=%s,version=%s", c.ID, c.Name, c.Topic, c.Version)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 5: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/schema/... -v
```

预期：所有测试 PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/multi_agent/schema/team_card.go internal/agentcore/multi_agent/schema/team_card_test.go
git commit -m "feat(multi_agent): 新建 schema 子包，实现 TeamCard 完整定义 (8.28)"
```

---

### Task 2: 新建 schema/doc.go — schema 子包文档

**Files:**
- Create: `internal/agentcore/multi_agent/schema/doc.go`

- [ ] **Step 1: 编写 doc.go**

```go
// Package schema 提供多 Agent 团队的类型定义，包括 TeamCard 等身份卡片。
//
// 本包是 multi_agent 的子包，对应 Python 的 openjiuwen/core/multi_agent/schema/ 目录。
// TeamCard 定义团队的不可变元数据（身份、成员列表、主题、版本、标签），
// 被 BaseTeam.Card() 返回，也被 EventDrivenTeamCard(8.29) 继承。
//
// 依赖约束：本包只依赖 common/schema/（BaseCard）和 single_agent/schema/（AgentCard），
// 不引用 multi_agent 包层的其他文件，避免循环依赖。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	└── team_card.go     # TeamCard 结构体 + 构造函数 + TeamCardOption + String
//
// 对应 Python 代码：openjiuwen/core/multi_agent/schema/
package schema
```

- [ ] **Step 2: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/schema/...
```

预期：编译成功。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/schema/doc.go
git commit -m "docs(multi_agent/schema): 添加包文档 doc.go (8.28)"
```

---

### Task 3: 新建 config.go — TeamConfig 完整实现

**Files:**
- Create: `internal/agentcore/multi_agent/config.go`
- Test: `internal/agentcore/multi_agent/config_test.go`

- [ ] **Step 1: 编写 config_test.go 测试文件**

```go
package multi_agent

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTeamConfig_默认值 验证默认值 MaxAgents=10, MaxConcurrentMessages=100, MessageTimeout=30.0。
func TestNewTeamConfig_默认值(t *testing.T) {
	cfg := NewTeamConfig()
	if cfg.MaxAgents != 10 {
		t.Errorf("期望 MaxAgents=10，实际 %d", cfg.MaxAgents)
	}
	if cfg.MaxConcurrentMessages != 100 {
		t.Errorf("期望 MaxConcurrentMessages=100，实际 %d", cfg.MaxConcurrentMessages)
	}
	if cfg.MessageTimeout != 30.0 {
		t.Errorf("期望 MessageTimeout=30.0，实际 %f", cfg.MessageTimeout)
	}
}

// TestTeamConfig_链式配置 验证 ConfigureMaxAgents/ConfigureTimeout/ConfigureConcurrency 链式调用。
func TestTeamConfig_链式配置(t *testing.T) {
	cfg := NewTeamConfig().
		ConfigureMaxAgents(5).
		ConfigureTimeout(60.0).
		ConfigureConcurrency(200)

	if cfg.MaxAgents != 5 {
		t.Errorf("期望 MaxAgents=5，实际 %d", cfg.MaxAgents)
	}
	if cfg.MessageTimeout != 60.0 {
		t.Errorf("期望 MessageTimeout=60.0，实际 %f", cfg.MessageTimeout)
	}
	if cfg.MaxConcurrentMessages != 200 {
		t.Errorf("期望 MaxConcurrentMessages=200，实际 %d", cfg.MaxConcurrentMessages)
	}
}

// TestTeamConfig_链式配置_返回自身 验证链式方法返回 *TeamConfig 指针。
func TestTeamConfig_链式配置_返回自身(t *testing.T) {
	cfg := NewTeamConfig()
	ptr1 := cfg.ConfigureMaxAgents(3)
	if ptr1 != cfg {
		t.Error("ConfigureMaxAgents 应返回自身指针")
	}
	ptr2 := cfg.ConfigureTimeout(10.0)
	if ptr2 != cfg {
		t.Error("ConfigureTimeout 应返回自身指针")
	}
	ptr3 := cfg.ConfigureConcurrency(50)
	if ptr3 != cfg {
		t.Error("ConfigureConcurrency 应返回自身指针")
	}
}

// TestTeamConfig_Extra 验证 SetExtra/GetExtra 读写。
func TestTeamConfig_Extra(t *testing.T) {
	cfg := NewTeamConfig()

	// 不存在的 key
	val, ok := cfg.GetExtra("not_exist")
	if ok {
		t.Error("不存在的 key 不应返回 ok=true")
	}
	if val != nil {
		t.Errorf("不存在的 key 应返回 nil，实际 %v", val)
	}

	// 设置后读取
	cfg.SetExtra("custom_key", "custom_value")
	val, ok = cfg.GetExtra("custom_key")
	if !ok {
		t.Error("已设置的 key 应返回 ok=true")
	}
	if val != "custom_value" {
		t.Errorf("期望 'custom_value'，实际 %v", val)
	}

	// 覆盖
	cfg.SetExtra("custom_key", 42)
	val, ok = cfg.GetExtra("custom_key")
	if !ok {
		t.Error("覆盖后应返回 ok=true")
	}
	if val != 42 {
		t.Errorf("期望 42，实际 %v", val)
	}
}

// TestTeamConfig_JSON序列化 验证 JSON marshal/unmarshal（Extra 不序列化）。
func TestTeamConfig_JSON序列化(t *testing.T) {
	cfg := NewTeamConfig()
	cfg.SetExtra("secret", "value")

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded TeamConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.MaxAgents != 10 {
		t.Errorf("期望 MaxAgents=10，实际 %d", decoded.MaxAgents)
	}
	// Extra 不参与 JSON 序列化
	if decoded.Extra != nil {
		t.Errorf("Extra 应为 nil（不参与 JSON 反序列化），实际 %v", decoded.Extra)
	}
}

// TestTeamConfig_JSON序列化_omitempty 验证非零值字段出现在 JSON 中。
func TestTeamConfig_JSON序列化_omitempty(t *testing.T) {
	cfg := NewTeamConfig()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal map 失败: %v", err)
	}
	if _, ok := m["max_agents"]; !ok {
		t.Error("max_agents 应出现在 JSON 中（非零值）")
	}
}
```

- [ ] **Step 2: 运行测试验证编译失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... -run "TestNewTeamConfig|TestTeamConfig" 2>&1 | head -20
```

预期：编译失败（NewTeamConfig、TeamConfig 新字段未定义）。

- [ ] **Step 3: 编写 config.go 实现文件**

```go
package multi_agent

// ──────────────────────────── 结构体 ────────────────────────────

// TeamConfig 团队运行时配置，控制团队的最大 Agent 数、并发数和超时。
//
// 可变参数，描述团队"怎么运行"。所有配置方法支持链式调用。
//
// 对应 Python: openjiuwen/core/multi_agent/config.py (TeamConfig)
// Python 字段: max_agents=10, max_concurrent_messages=100, message_timeout=30.0
type TeamConfig struct {
	// MaxAgents 团队最大 Agent 数量，默认 10
	MaxAgents int `json:"max_agents,omitempty"`
	// MaxConcurrentMessages 最大并发消息数，默认 100
	MaxConcurrentMessages int `json:"max_concurrent_messages,omitempty"`
	// MessageTimeout 消息处理超时秒数，默认 30.0
	MessageTimeout float64 `json:"message_timeout,omitempty"`
	// Extra 额外配置字段，对应 Python model_config={"extra": "allow"}
	//
	// json:"-" 表示不参与 JSON 序列化，Extra 是运行时注入的动态配置。
	Extra map[string]any `json:"-"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamConfig 创建 TeamConfig 实例，设置默认值。
//
// 对应 Python: TeamConfig(max_agents=10, max_concurrent_messages=100, message_timeout=30.0)
func NewTeamConfig() *TeamConfig {
	return &TeamConfig{
		MaxAgents:             10,
		MaxConcurrentMessages: 100,
		MessageTimeout:        30.0,
	}
}

// ConfigureMaxAgents 链式配置最大 Agent 数量。
//
// 对应 Python: TeamConfig.configure_max_agents(max_agents) -> self
func (c *TeamConfig) ConfigureMaxAgents(maxAgents int) *TeamConfig {
	c.MaxAgents = maxAgents
	return c
}

// ConfigureTimeout 链式配置消息超时秒数。
//
// 对应 Python: TeamConfig.configure_timeout(timeout) -> self
func (c *TeamConfig) ConfigureTimeout(timeout float64) *TeamConfig {
	c.MessageTimeout = timeout
	return c
}

// ConfigureConcurrency 链式配置最大并发消息数。
//
// 对应 Python: TeamConfig.configure_concurrency(max_concurrent) -> self
func (c *TeamConfig) ConfigureConcurrency(maxConcurrent int) *TeamConfig {
	c.MaxConcurrentMessages = maxConcurrent
	return c
}

// SetExtra 设置额外配置字段。
func (c *TeamConfig) SetExtra(key string, value any) {
	if c.Extra == nil {
		c.Extra = make(map[string]any)
	}
	c.Extra[key] = value
}

// GetExtra 获取额外配置字段。
func (c *TeamConfig) GetExtra(key string) (any, bool) {
	if c.Extra == nil {
		return nil, false
	}
	val, ok := c.Extra[key]
	return val, ok
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... -run "TestNewTeamConfig|TestTeamConfig" -v
```

注意：此时 team.go 中仍有 TeamCard 占位定义，与 config_test.go 无冲突。但 TeamConfig 已从占位空结构体变为完整定义，team_test.go 中的 `TeamConfig{}` 可能需先临时处理。

预期：config_test.go 中所有测试 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/multi_agent/config.go internal/agentcore/multi_agent/config_test.go
git commit -m "feat(multi_agent): 实现 TeamConfig 完整定义及链式配置方法 (8.28)"
```

---

### Task 4: 修改 team.go — 移除占位，添加类型别名

**Files:**
- Modify: `internal/agentcore/multi_agent/team.go`

- [ ] **Step 1: 修改 team.go**

将 team.go 中的 TeamCard 占位定义替换为 schema 子包的类型别名，移除 TeamConfig 占位定义（已在 config.go 中实现）。

修改后的 team.go 完整内容：

```go
package multi_agent

import (
	"context"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamCard 团队身份卡片（类型别名，实际定义在 schema 子包）。
//
// 使用类型别名保持外部 API 兼容：所有通过 multiagent.TeamCard 的代码无需修改 import。
// 完整定义见 internal/agentcore/multi_agent/schema/team_card.go
type TeamCard = schema.TeamCard

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BaseTeam 多 Agent 团队核心行为契约。
//
// 对应 Python: openjiuwen/core/multi_agent/team.py (BaseTeam)
//
// 设计原则：
//   - Card 是必填项（定义团队身份）
//   - Config 是可选项（定义团队运行时行为）
//   - BaseTeam 与 BaseAgent 是平行的两个体系，不继承 BaseAgent
//   - Invoke/Stream 是对整个团队的调用，由子类实现
//   - AddAgent/RemoveAgent/Send/Publish/Subscribe 等方法在具体团队中实现
type BaseTeam interface {
	// ── 核心执行 ──

	// Invoke 非流式调用团队。
	//
	// 对应 Python: BaseTeam.invoke(message, session)
	Invoke(ctx context.Context, inputs map[string]any, opts ...TeamOption) (any, error)

	// Stream 流式调用团队。
	//
	// 对应 Python: BaseTeam.stream(message, session) -> AsyncIterator
	Stream(ctx context.Context, inputs map[string]any, opts ...TeamOption) (<-chan stream.Schema, error)

	// ── Agent 管理 ──

	// AddAgent 向团队注册 Agent。
	//
	// 对应 Python: BaseTeam.add_agent(card, provider) -> self
	AddAgent(ctx context.Context, card *agentschema.AgentCard, provider TeamAgentProvider) error

	// RemoveAgent 从团队注销 Agent。
	//
	// 对应 Python: BaseTeam.remove_agent(agent) -> self
	RemoveAgent(ctx context.Context, agentID string) error

	// ── 消息通信 ──

	// Send 点对点消息发送。
	//
	// 对应 Python: BaseTeam.send(message, recipient, sender, session_id, timeout)
	Send(ctx context.Context, message map[string]any, recipient string, sender string, opts ...TeamOption) (any, error)

	// Publish 发布消息到主题。
	//
	// 对应 Python: BaseTeam.publish(message, topic_id, sender, session_id)
	Publish(ctx context.Context, message map[string]any, topicID string, sender string, opts ...TeamOption) error

	// Subscribe 订阅主题。
	//
	// 对应 Python: BaseTeam.subscribe(agent_id, topic)
	Subscribe(ctx context.Context, agentID string, topic string) error

	// Unsubscribe 取消订阅。
	//
	// 对应 Python: BaseTeam.unsubscribe(agent_id, topic)
	Unsubscribe(ctx context.Context, agentID string, topic string) error

	// ── 配置 ──

	// Configure 配置团队。
	//
	// 对应 Python: BaseTeam.configure(config) -> self
	Configure(ctx context.Context, config TeamConfig) error

	// ── 查询 ──

	// GetAgentCard 获取 Agent 卡片。
	//
	// 对应 Python: BaseTeam.get_agent_card(agent_id)
	GetAgentCard(agentID string) (*agentschema.AgentCard, error)

	// GetAgentCount 获取 Agent 数量。
	//
	// 对应 Python: BaseTeam.get_agent_count()
	GetAgentCount() int

	// ListAgents 列出所有 Agent ID。
	//
	// 对应 Python: BaseTeam.list_agents()
	ListAgents() []string

	// ── 访问器 ──

	// Card 返回团队身份卡片。
	//
	// 对应 Python: BaseTeam.card 属性
	Card() *TeamCard

	// Config 返回团队配置。
	//
	// 对应 Python: BaseTeam.config 属性
	Config() *TeamConfig
}

// AgentTeamProvider 团队资源提供者函数，接受 TeamCard 返回 BaseTeam 实例。
//
// 对应 Python: AgentTeamProvider = Callable[[TeamCard], Awaitable[BaseTeam]] | Callable[[TeamCard], BaseTeam]
// 用于延迟加载团队资源，注册时传入工厂函数而非实例。
type AgentTeamProvider func(ctx context.Context, card *TeamCard) (BaseTeam, error)

// TeamAgentProvider 团队内 Agent 资源提供者函数，接受 AgentCard 返回 Agent 实例。
//
// 对应 Python: AgentProvider = Callable[[AgentCard], Awaitable[BaseAgent]] | Callable[[AgentCard], BaseAgent]
// 签名与 resources_manager.AgentProvider 一致，在 multi_agent 包内定义以避免循环依赖。
// 具体团队实现中可通过类型转换互转：resources_manager.AgentProvider(provider) 或 TeamAgentProvider(rmProvider)。
type TeamAgentProvider func(ctx context.Context, card *agentschema.AgentCard) (any, error)

// ──────────────────────────── 非导出函数 ────────────────────────────
```

**关键变更：**
1. 移除 `TeamCard struct{ schema.BaseCard }` 占位定义，替换为 `type TeamCard = schema.TeamCard` 类型别名
2. 移除 `TeamConfig struct{}` 占位定义（已在 config.go 中）
3. 新增 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"`
4. 移除 import `"github.com/uapclaw/uapclaw-go/internal/common/schema"`（不再直接引用 BaseCard）
5. `Config()` 返回类型从 `TeamConfig` 改为 `*TeamConfig`（与 NewTeamConfig 返回指针一致，且 BaseTeam 实现中 config 通常是字段指针）

- [ ] **Step 2: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/...
```

预期：编译成功（TeamCard 类型别名完全等价，外部引用无需修改）。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/team.go
git commit -m "refactor(multi_agent): 移除 TeamCard/TeamConfig 占位，改用 schema 子包类型别名 (8.28)"
```

---

### Task 5: 更新 team_test.go — 适配新类型

**Files:**
- Modify: `internal/agentcore/multi_agent/team_test.go`

- [ ] **Step 1: 修改 team_test.go**

更新 stubTeam 和测试函数，适配 TeamCard 类型别名和 TeamConfig 指针返回：

```go
package multi_agent

import (
	"context"
	"testing"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubTeam 用于编译时检查 BaseTeam 接口满足的桩实现。
type stubTeam struct {
	card   *TeamCard
	config *TeamConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// 编译时检查 stubTeam 满足 BaseTeam 接口。
var _ BaseTeam = (*stubTeam)(nil)

func (t *stubTeam) Invoke(_ context.Context, _ map[string]any, _ ...TeamOption) (any, error) {
	return nil, nil
}

func (t *stubTeam) Stream(_ context.Context, _ map[string]any, _ ...TeamOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema)
	close(ch)
	return ch, nil
}

func (t *stubTeam) AddAgent(_ context.Context, _ *agentschema.AgentCard, _ TeamAgentProvider) error {
	return nil
}

func (t *stubTeam) RemoveAgent(_ context.Context, _ string) error {
	return nil
}

func (t *stubTeam) Send(_ context.Context, _ map[string]any, _ string, _ string, _ ...TeamOption) (any, error) {
	return nil, nil
}

func (t *stubTeam) Publish(_ context.Context, _ map[string]any, _ string, _ string, _ ...TeamOption) error {
	return nil
}

func (t *stubTeam) Subscribe(_ context.Context, _ string, _ string) error {
	return nil
}

func (t *stubTeam) Unsubscribe(_ context.Context, _ string, _ string) error {
	return nil
}

func (t *stubTeam) Configure(_ context.Context, _ TeamConfig) error {
	return nil
}

func (t *stubTeam) GetAgentCard(_ string) (*agentschema.AgentCard, error) {
	return nil, nil
}

func (t *stubTeam) GetAgentCount() int {
	return 0
}

func (t *stubTeam) ListAgents() []string {
	return nil
}

func (t *stubTeam) Card() *TeamCard {
	return t.card
}

func (t *stubTeam) Config() *TeamConfig {
	return t.config
}

// TestBaseTeam_编译时接口检查 验证 stubTeam 满足 BaseTeam 接口。
func TestBaseTeam_编译时接口检查(t *testing.T) {
	card := schema.NewTeamCard(commonschema.WithName("test-team"))
	team := &stubTeam{card: card, config: NewTeamConfig()}

	// 基本调用验证
	_ = team.Card()
	_ = team.Config()
	_ = team.GetAgentCount()
	_ = team.ListAgents()
}
```

- [ ] **Step 2: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... -v
```

预期：所有测试 PASS（包括 team_test.go 和 config_test.go）。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/team_test.go
git commit -m "test(multi_agent): 更新 team_test 适配 TeamCard 类型别名和 TeamConfig 指针 (8.28)"
```

---

### Task 6: 更新 doc.go — 文件目录同步

**Files:**
- Modify: `internal/agentcore/multi_agent/doc.go`

- [ ] **Step 1: 修改 doc.go**

```go
// Package multi_agent 提供多 Agent 团队的核心抽象与运行时基础设施。
//
// 定义 BaseTeam 接口作为多 Agent 团队体系的根基契约，
// 具体团队实现（HandoffTeam、HierarchicalTeam）均实现此接口。
// 团队内部的 Agent 通信通过 TeamRuntime（8.30）的 P2P 和 Pub-Sub 消息机制完成。
//
// 文件目录：
//
//	multi_agent/
//	├── doc.go              # 包文档
//	├── config.go           # TeamConfig 团队运行时配置 + 链式配置方法
//	├── team.go             # BaseTeam 接口 + TeamCard 类型别名 + AgentTeamProvider/TeamAgentProvider
//	├── team_option.go      # TeamOptions 结构体 + TeamOption 函数类型 + WithXxx
//	└── schema/
//	    ├── doc.go           # schema 子包文档
//	    └── team_card.go    # TeamCard 团队身份卡片 + 构造函数 + TeamCardOption + String
//
// 对应 Python 代码：openjiuwen/core/multi_agent/
package multi_agent
```

- [ ] **Step 2: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/doc.go
git commit -m "docs(multi_agent): 更新 doc.go 文件目录，反映 schema 子包和 config.go (8.28)"
```

---

### Task 7: 验证外部引用兼容性 + 全量测试

**Files:**
- 无新增文件，验证已有代码

- [ ] **Step 1: 检查 resources_manager 编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/runner/resources_manager/...
```

预期：编译成功。`resources_manager` 通过 `multiagents.TeamCard` 引用 TeamCard，类型别名保证兼容。

- [ ] **Step 2: 运行 multi_agent 全量测试**

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/multi_agent/... -v
```

预期：所有测试 PASS，覆盖率达标。

- [ ] **Step 3: 运行 resources_manager 测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/... -v
```

预期：所有测试 PASS。

- [ ] **Step 4: 运行更广范围的编译检查**

```bash
cd /home/opensource/uap-claw-go && go build ./...
```

预期：全项目编译成功。

- [ ] **Step 5: 更新 IMPLEMENTATION_PLAN.md 中 8.28 状态**

将 `8.28 | ☐` 改为 `8.28 | ✅`。

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore: 更新实现计划 8.28 状态为已完成"
```

---

### Task 8: 最终提交合并

- [ ] **Step 1: 确认所有变更文件**

```bash
cd /home/opensource/uap-claw-go && git status
```

- [ ] **Step 2: 确认全量测试通过**

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/multi_agent/... ./internal/agentcore/runner/resources_manager/... -v
```

预期：全部 PASS。
