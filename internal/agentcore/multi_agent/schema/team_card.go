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

// ──────────────────────────── 非导出函数 ────────────────────────────
