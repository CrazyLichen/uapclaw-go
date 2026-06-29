package multi_agent

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

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
	// ⤴️ 8.28 回填：返回类型从值 TeamConfig 改为指针 *TeamConfig
	Config() *TeamConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// TeamCard 团队身份卡片（类型别名，实际定义在 schema 子包）。
//
// 使用类型别名保持外部 API 兼容：所有通过 multiagent.TeamCard 的代码无需修改 import。
// 完整定义见 internal/agentcore/multi_agent/schema/team_card.go
//
// 对应 Python: openjiuwen/core/multi_agent/schema/team_card.py (TeamCard)
// ⤴️ 8.28 回填：从 schema 子包引入类型别名
type TeamCard = schema.TeamCard

// AgentTeamProvider 团队资源提供者函数，接受 TeamCard 返回 BaseTeam 实例。
//
// 对应 Python: AgentTeamProvider = Callable[[TeamCard], Awaitable[BaseTeam]] | Callable[[TeamCard], BaseTeam]
// 用于延迟加载团队资源，注册时传入工厂函数而非实例。
type AgentTeamProvider func(ctx context.Context, card *TeamCard) (BaseTeam, error)

// TeamAgentProvider 团队内 Agent 资源提供者函数，接受 AgentCard 返回 BaseAgent 实例。
//
// 对应 Python: AgentProvider = Callable[[AgentCard], Awaitable[BaseAgent]] | Callable[[AgentCard], BaseAgent]
// 在 multi_agent 包内定义以避免 multi_agent → resources_manager 循环依赖。
// 签名与 resources_manager.AgentProvider 完全一致，具体团队实现中可直接互换。
type TeamAgentProvider func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
