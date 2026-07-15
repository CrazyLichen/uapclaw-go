package schema

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
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
	// 通过 WithParentAgentID() Option 声明层级关系（仅 HierarchicalToolsTeam 使用），
	// 其他 Team 实现忽略 opts。
	//
	// 对应 Python: BaseTeam.add_agent(card, provider, parent_agent_id=None) -> self
	AddAgent(ctx context.Context, card *agentschema.AgentCard, provider TeamAgentProvider, opts ...TeamOption) error

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
	Card() TeamCardInterface

	// Config 返回团队配置。
	//
	// 对应 Python: BaseTeam.config 属性
	Config() *TeamConfig
}

// TeamOptions 团队调用选项。
//
// 对应 Python: BaseTeam 各方法的可选参数（session、session_id、timeout、stream_modes）
type TeamOptions struct {
	// Session 团队会话（可选）
	//
	// 对应 Python: invoke(message, session) 的 session 参数
	Session *session.AgentTeamSession
	// SessionID 会话标识（可选）
	//
	// 对应 Python: send/publish 的 session_id 参数
	SessionID string
	// Timeout 响应超时秒数（可选）
	//
	// 对应 Python: send 的 timeout 参数
	Timeout float64
	// StreamModes 流式输出模式（可选）
	//
	// 对应 Python: stream 的 stream_modes 参数
	StreamModes []stream.StreamMode
	// ParentAgentID 父 Agent ID，用于 HierarchicalToolsTeam 的层级注册。
	//
	// 在 AddAgent 时通过 WithParentAgentID() Option 传递，
	// 声明当前 Agent 是哪个父 Agent 的子工具。
	ParentAgentID string
}

// TeamConfig 团队运行时配置，控制团队的最大 Agent 数、并发数和超时。
//
// 可变参数，描述团队"怎么运行"。所有配置方法支持链式调用。
//
// 对应 Python: openjiuwen/core/multi_agent/config.py (TeamConfig)
// Python 字段: max_agents=10, max_concurrent_messages=100, message_timeout=30.0
type TeamConfig struct {
	// MaxAgents 团队最大 Agent 数量，默认 10
	//
	// 对应 Python: TeamConfig.max_agents: int = Field(default=10)
	MaxAgents int `json:"max_agents,omitempty"`
	// MaxConcurrentMessages 最大并发消息数，默认 100
	//
	// 对应 Python: TeamConfig.max_concurrent_messages: int = Field(default=100)
	MaxConcurrentMessages int `json:"max_concurrent_messages,omitempty"`
	// MessageTimeout 消息处理超时秒数，默认 30.0
	//
	// 对应 Python: TeamConfig.message_timeout: float = Field(default=30.0)
	MessageTimeout float64 `json:"message_timeout,omitempty"`
	// Extra 额外配置字段，对应 Python model_config={"extra": "allow"}
	//
	// json:"-" 表示不参与 JSON 序列化，Extra 是运行时注入的动态配置。
	Extra map[string]any `json:"-"`
}

// AgentTeamProvider 团队资源提供者函数，接受 TeamCardInterface 返回 BaseTeam 实例。
//
// 对应 Python: AgentTeamProvider = Callable[[TeamCard], Awaitable[BaseTeam]] | Callable[[TeamCard], BaseTeam]
// 用于延迟加载团队资源，注册时传入工厂函数而非实例。
type AgentTeamProvider func(ctx context.Context, card TeamCardInterface) (BaseTeam, error)

// TeamAgentProvider 团队内 Agent 资源提供者函数，接受 AgentCard 返回 BaseAgent 实例。
//
// 对应 Python: AgentProvider = Callable[[AgentCard], Awaitable[BaseAgent]] | Callable[[AgentCard], BaseAgent]
// 在 multi_agent 包内定义以避免 multi_agent → resources_manager 循环依赖。
// 签名与 resources_manager.AgentProvider 完全一致，具体团队实现中可直接互换。
type TeamAgentProvider func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error)

// TeamOption 团队调用选项函数。
type TeamOption func(*TeamOptions)

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

// WithTeamSession 设置团队会话。
func WithTeamSession(sess *session.AgentTeamSession) TeamOption {
	return func(o *TeamOptions) { o.Session = sess }
}

// WithTeamSessionID 设置会话标识。
func WithTeamSessionID(sessionID string) TeamOption {
	return func(o *TeamOptions) { o.SessionID = sessionID }
}

// WithTeamTimeout 设置响应超时。
func WithTeamTimeout(timeout float64) TeamOption {
	return func(o *TeamOptions) { o.Timeout = timeout }
}

// WithTeamStreamModes 设置流式输出模式。
func WithTeamStreamModes(modes []stream.StreamMode) TeamOption {
	return func(o *TeamOptions) { o.StreamModes = modes }
}

// WithParentAgentID 设置父 Agent ID。
//
// 用于 HierarchicalToolsTeam.AddAgent() 时声明父子关系：
//
//	team.AddAgent(ctx, childCard, childProvider,
//	    maschema.WithParentAgentID("parent_agent_id"),
//	)
func WithParentAgentID(parentID string) TeamOption {
	return func(o *TeamOptions) { o.ParentAgentID = parentID }
}

// NewTeamOptions 从选项列表构建 TeamOptions。
func NewTeamOptions(opts ...TeamOption) *TeamOptions {
	o := &TeamOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
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
//
// 对应 Python: model_config={"extra": "allow"} 允许动态额外字段
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
