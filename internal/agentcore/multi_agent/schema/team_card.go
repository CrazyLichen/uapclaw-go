package schema

import (
	"fmt"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

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
	//
	// 对应 Python: TeamCard.agent_cards: List[AgentCard] = Field(default_factory=list)
	AgentCards []*agentschema.AgentCard `json:"agent_cards,omitempty"`
	// Topic 团队主题/领域
	//
	// 对应 Python: TeamCard.topic: str = Field(default='')
	Topic string `json:"topic,omitempty"`
	// Version 团队版本号
	//
	// 对应 Python: TeamCard.version: str = Field(default='1.0.0')
	Version string `json:"version,omitempty"`
	// Tags 分类标签
	//
	// 对应 Python: TeamCard.tags: List[str] = Field(default_factory=list)
	Tags []string `json:"tags,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// TeamCardOption TeamCard 构造选项函数，统一设置 BaseCard 字段和 TeamCard 字段。
//
// 采用方案C：去掉 CardOption 混合，所有选项均通过 TeamCardOption 设置，
// 编译时类型安全，避免 opts ...any 的运行时 switch。
type TeamCardOption func(*TeamCard)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 TeamCard 满足 TeamCardInterface。
var _ TeamCardInterface = (*TeamCard)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamCard 创建 TeamCard 实例，默认 Version="1.0.0"。
//
// 对应 Python: TeamCard(id=uuid4().hex, name="", description="", agent_cards=[], topic="", version="1.0.0", tags=[])
// 所有选项（含 BaseCard 字段）均通过 TeamCardOption 设置，编译时类型安全。
func NewTeamCard(opts ...TeamCardOption) *TeamCard {
	card := &TeamCard{
		BaseCard: *schema.NewBaseCard(),
		Version:  "1.0.0",
	}
	for _, opt := range opts {
		opt(card)
	}
	return card
}

// WithTeamCardID 设置团队卡片 ID（覆盖自动生成的 UUID）。
//
// 对应 Python: TeamCard(id=...)
func WithTeamCardID(id string) TeamCardOption {
	return func(c *TeamCard) { c.ID = id }
}

// WithTeamCardName 设置团队名称。
//
// 对应 Python: TeamCard(name=...)
func WithTeamCardName(name string) TeamCardOption {
	return func(c *TeamCard) { c.Name = name }
}

// WithTeamCardDescription 设置团队描述。
//
// 对应 Python: TeamCard(description=...)
func WithTeamCardDescription(desc string) TeamCardOption {
	return func(c *TeamCard) { c.Description = desc }
}

// WithAgentCards 设置成员 Agent 卡片列表。
//
// 对应 Python: TeamCard(agent_cards=[...])
func WithAgentCards(cards []*agentschema.AgentCard) TeamCardOption {
	return func(c *TeamCard) { c.AgentCards = cards }
}

// WithTopic 设置团队主题。
//
// 对应 Python: TeamCard(topic="...")
func WithTopic(topic string) TeamCardOption {
	return func(c *TeamCard) { c.Topic = topic }
}

// WithTeamVersion 设置团队版本号。
//
// 对应 Python: TeamCard(version="...")
func WithTeamVersion(version string) TeamCardOption {
	return func(c *TeamCard) { c.Version = version }
}

// WithTags 设置分类标签。
//
// 对应 Python: TeamCard(tags=[...])
func WithTags(tags []string) TeamCardOption {
	return func(c *TeamCard) { c.Tags = tags }
}

// String 实现 fmt.Stringer 接口，返回简洁的身份描述。
//
// 对应 Python: BaseCard.to_str() 扩展，增加 topic 和 version 字段
func (c *TeamCard) String() string {
	return fmt.Sprintf("id=%s,name=%s,topic=%s,version=%s", c.ID, c.Name, c.Topic, c.Version)
}

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

// ──────────────────────────── 非导出函数 ────────────────────────────
