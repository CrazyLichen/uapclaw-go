package team_runtime

import (
	"context"
	"fmt"
	"sync"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeConfig 团队运行时配置。
//
// 对应 Python: RuntimeConfig (openjiuwen/core/multi_agent/team_runtime/team_runtime.py)
type RuntimeConfig struct {
	// TeamID 团队标识
	TeamID string
	// MessageBus 消息总线配置
	MessageBus *MessageBusConfig
	// P2PTimeout P2P 通信超时秒数，默认 1800.0
	P2PTimeout float64
}

// TeamRuntime 团队运行时编排入口，聚合消息总线、订阅管理和 Agent 注册。
//
// 职责：
//   - Agent 注册/注销（存储 AgentCard + wrapProvider 注入 RuntimeBindable + 注册到 Runner.resource_mgr）
//   - 消息通信（Send/Publish/Subscribe 委托 messageBus）
//   - 会话管理（BindTeamSession/UnbindTeamSession）
//   - 生命周期（Start/Stop/CleanupSession）
//
// 对应 Python: TeamRuntime (openjiuwen/core/multi_agent/team_runtime/team_runtime.py)
type TeamRuntime struct {
	// config 运行时配置
	config RuntimeConfig
	// teamID 团队标识
	teamID string
	// agentCards Agent 卡片映射，agentID → AgentCard
	agentCards map[string]*agentschema.AgentCard
	// messageBus 消息总线
	messageBus MessageBusInterface
	// activeTeamSessions 活跃团队会话映射，sessionID → AgentTeamSession
	activeTeamSessions map[string]*session.AgentTeamSession
	// running 是否运行中
	running bool
	// startOnce 启动同步原语，确保 Start 只执行一次
	startOnce sync.Once
	// p2pTimeout P2P 通信超时秒数
	p2pTimeout float64
	// mu 保护 agentCards、running 和 activeTeamSessions
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// RuntimeConfigOption 团队运行时配置选项函数类型
type RuntimeConfigOption func(*RuntimeConfig)

// ──────────────────────────── 常量 ────────────────────────────
const (
	// defaultP2PTimeout 默认 P2P 超时秒数
	defaultP2PTimeout = 1800.0
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRuntimeConfig 创建团队运行时配置，设置默认值。
func NewRuntimeConfig(opts ...RuntimeConfigOption) *RuntimeConfig {
	cfg := &RuntimeConfig{
		P2PTimeout: defaultP2PTimeout,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.MessageBus == nil {
		busOpts := []MessageBusConfigOption{
			WithProcessTimeout(cfg.P2PTimeout),
		}
		if cfg.TeamID != "" {
			busOpts = append(busOpts, WithTeamID(cfg.TeamID))
		}
		cfg.MessageBus = NewMessageBusConfig(busOpts...)
	}
	return cfg
}

// WithRuntimeTeamID 设置团队标识选项。
func WithRuntimeTeamID(teamID string) RuntimeConfigOption {
	return func(c *RuntimeConfig) { c.TeamID = teamID }
}

// WithRuntimeMessageBus 设置消息总线配置选项。
func WithRuntimeMessageBus(busConfig *MessageBusConfig) RuntimeConfigOption {
	return func(c *RuntimeConfig) { c.MessageBus = busConfig }
}

// WithRuntimeP2PTimeout 设置 P2P 超时选项。
func WithRuntimeP2PTimeout(timeout float64) RuntimeConfigOption {
	return func(c *RuntimeConfig) { c.P2PTimeout = timeout }
}

// NewTeamRuntime 创建团队运行时实例。
//
// 对应 Python: TeamRuntime.__init__(config)
func NewTeamRuntime(config RuntimeConfig) *TeamRuntime {
	return &TeamRuntime{
		config:             config,
		teamID:             config.TeamID,
		agentCards:         make(map[string]*agentschema.AgentCard),
		activeTeamSessions: make(map[string]*session.AgentTeamSession),
		p2pTimeout:         config.P2PTimeout,
	}
}

// Start 启动团队运行时，初始化并启动消息总线。
//
// 对应 Python: TeamRuntime.start()
func (tr *TeamRuntime) Start(ctx context.Context) error {
	var startErr error
	tr.startOnce.Do(func() {
		if tr.messageBus == nil {
			startErr = fmt.Errorf("消息总线未设置，请先调用 SetMessageBus")
			return
		}
		if err := tr.messageBus.Start(ctx); err != nil {
			startErr = err
			return
		}
		tr.mu.Lock()
		tr.running = true
		tr.mu.Unlock()

		logger.Info(logComponent).
			Str("event_type", "TEAM_RUNTIME_STARTED").
			Str("team_id", tr.teamID).
			Msg("团队运行时已启动")
	})
	return startErr
}

// Stop 停止团队运行时，停止消息总线。
//
// 对应 Python: TeamRuntime.stop()
func (tr *TeamRuntime) Stop(ctx context.Context) error {
	tr.mu.Lock()
	tr.running = false
	tr.mu.Unlock()

	if tr.messageBus != nil {
		if err := tr.messageBus.Stop(ctx); err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "TEAM_RUNTIME_STOP_ERROR").
				Str("team_id", tr.teamID).
				Msg("团队运行时停止失败")
			return err
		}
	}

	logger.Info(logComponent).
		Str("event_type", "TEAM_RUNTIME_STOPPED").
		Str("team_id", tr.teamID).
		Msg("团队运行时已停止")

	return nil
}

// CleanupSession 清理会话，委托消息总线清理。
//
// 对应 Python: TeamRuntime.cleanup_session(session_id)
func (tr *TeamRuntime) CleanupSession(ctx context.Context, sessionID string) error {
	if tr.messageBus != nil {
		return tr.messageBus.CleanupSession(ctx, sessionID)
	}
	return nil
}

// RegisterAgent 注册 Agent，存储 AgentCard 并包装 provider 注入 RuntimeBindable。
//
// 流程：
//  1. 存储 AgentCard 到 agentCards
//  2. 调用 wrapProvider 包装 provider（自动注入 BindRuntime）
//  3. 注册到 ResourceMgr（如果已设置）
//
// 对应 Python: TeamRuntime.register_agent(card, provider)
func (tr *TeamRuntime) RegisterAgent(ctx context.Context, card *agentschema.AgentCard, provider resources_manager.AgentProvider) error {
	agentID := card.ID

	tr.mu.Lock()
	tr.agentCards[agentID] = card
	tr.mu.Unlock()

	// 包装 provider 注入 RuntimeBindable
	wrappedProvider := tr.wrapProvider(provider, agentID)

	// 注册到 ResourceMgr（对齐 Python: Runner.resource_mgr.add_agent(card, wrapped_provider)）
	// Python 通过 delayed import 访问 Runner.resource_mgr，
	// Go 直接调用 runner.GetResourceMgr() 等价访问全局 ResourceMgr。
	if resourceMgr := runner.GetResourceMgr(); resourceMgr != nil {
		if err := resourceMgr.AddAgent(card, wrappedProvider); err != nil {
			logger.Warn(logComponent).Err(err).
				Str("event_type", "AGENT_REGISTER_TO_RESOURCEMGR_ERROR").
				Str("agent_id", agentID).
				Msg("注册 Agent 到 ResourceMgr 失败，可能已存在")
		}
	}

	logger.Info(logComponent).
		Str("event_type", "AGENT_REGISTERED").
		Str("agent_id", agentID).
		Str("team_id", tr.teamID).
		Msg("Agent 已注册到团队运行时")

	return nil
}

// UnregisterAgent 注销 Agent，移除 AgentCard 和所有订阅。
//
// 对应 Python: TeamRuntime.unregister_agent(agent_id)
func (tr *TeamRuntime) UnregisterAgent(ctx context.Context, agentID string) (*agentschema.AgentCard, error) {
	tr.mu.Lock()
	card, ok := tr.agentCards[agentID]
	if !ok {
		tr.mu.Unlock()
		return nil, fmt.Errorf("agent %s 不存在", agentID)
	}
	delete(tr.agentCards, agentID)
	tr.mu.Unlock()

	// 移除所有订阅
	if tr.messageBus != nil {
		tr.messageBus.RemoveAllSubscriptions(agentID)
	}

	logger.Info(logComponent).
		Str("event_type", "AGENT_UNREGISTERED").
		Str("agent_id", agentID).
		Str("team_id", tr.teamID).
		Msg("Agent 已从团队运行时注销")

	return card, nil
}

// HasAgent 判断 Agent 是否已注册。
//
// 对应 Python: TeamRuntime.has_agent(agent_id)
func (tr *TeamRuntime) HasAgent(agentID string) bool {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	_, ok := tr.agentCards[agentID]
	return ok
}

// GetAgentCard 获取 Agent 卡片。
//
// 对应 Python: TeamRuntime.get_agent_card(agent_id)
func (tr *TeamRuntime) GetAgentCard(agentID string) (*agentschema.AgentCard, error) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	card, ok := tr.agentCards[agentID]
	if !ok {
		return nil, fmt.Errorf("agent %s 不存在", agentID)
	}
	return card, nil
}

// ListAgents 列出所有 Agent ID。
//
// 对应 Python: TeamRuntime.list_agents()
func (tr *TeamRuntime) ListAgents() []string {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	agents := make([]string, 0, len(tr.agentCards))
	for id := range tr.agentCards {
		agents = append(agents, id)
	}
	return agents
}

// GetAgentCount 获取 Agent 数量。
//
// 对应 Python: TeamRuntime.get_agent_count()
func (tr *TeamRuntime) GetAgentCount() int {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return len(tr.agentCards)
}

// Send P2P 发送消息，校验参数后委托消息总线。
//
// 对应 Python: TeamRuntime.send(message, recipient, sender, opts)
func (tr *TeamRuntime) Send(ctx context.Context, message any, recipient string, sender string, opts ...maschema.TeamOption) (any, error) {
	if !tr.IsRunning() {
		return nil, fmt.Errorf("团队运行时未启动，无法发送消息")
	}
	if !tr.HasAgent(recipient) {
		return nil, fmt.Errorf("接收者 Agent %s 不存在", recipient)
	}

	teamOpts := maschema.NewTeamOptions(opts...)
	sessionID := teamOpts.SessionID
	timeout := teamOpts.Timeout
	if timeout <= 0 {
		timeout = tr.p2pTimeout
	}

	return tr.messageBus.Send(ctx, message, recipient, sender, sessionID, timeout)
}

// Publish Pub-Sub 发布消息，校验参数后委托消息总线。
//
// 对应 Python: TeamRuntime.publish(message, topic_id, sender, opts)
func (tr *TeamRuntime) Publish(ctx context.Context, message any, topicID string, sender string, opts ...maschema.TeamOption) error {
	if !tr.IsRunning() {
		return fmt.Errorf("团队运行时未启动，无法发布消息")
	}

	teamOpts := maschema.NewTeamOptions(opts...)
	sessionID := teamOpts.SessionID

	return tr.messageBus.Publish(ctx, message, topicID, sender, sessionID)
}

// Subscribe 订阅主题，委托消息总线。
//
// 对应 Python: TeamRuntime.subscribe(agent_id, topic)
func (tr *TeamRuntime) Subscribe(ctx context.Context, agentID string, topic string) error {
	if tr.messageBus != nil {
		tr.messageBus.AddSubscription(agentID, topic)
	}
	return nil
}

// Unsubscribe 取消订阅，委托消息总线。
//
// 对应 Python: TeamRuntime.unsubscribe(agent_id, topic)
func (tr *TeamRuntime) Unsubscribe(ctx context.Context, agentID string, topic string) error {
	if tr.messageBus != nil {
		tr.messageBus.RemoveSubscription(agentID, topic)
	}
	return nil
}

// BindTeamSession 绑定团队会话。
//
// 对应 Python: TeamRuntime.bind_team_session(session)
func (tr *TeamRuntime) BindTeamSession(sess *session.AgentTeamSession) {
	if sess == nil {
		return
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.activeTeamSessions[sess.GetSessionID()] = sess
}

// UnbindTeamSession 解绑团队会话。
//
// 对应 Python: TeamRuntime.unbind_team_session(session_id)
func (tr *TeamRuntime) UnbindTeamSession(sessionID string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	delete(tr.activeTeamSessions, sessionID)
}

// GetTeamSession 获取团队会话。
//
// 对应 Python: TeamRuntime.get_team_session(session_id)
func (tr *TeamRuntime) GetTeamSession(sessionID string) *session.AgentTeamSession {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return tr.activeTeamSessions[sessionID]
}

// ListSubscriptions 列出订阅信息，委托消息总线。
//
// 对应 Python: TeamRuntime.list_subscriptions(agent_id)
func (tr *TeamRuntime) ListSubscriptions(agentID string) map[string]any {
	if tr.messageBus != nil {
		return tr.messageBus.ListSubscriptions(agentID)
	}
	return nil
}

// GetSubscriptionCount 获取总订阅数，委托消息总线。
//
// 对应 Python: TeamRuntime.get_subscription_count()
func (tr *TeamRuntime) GetSubscriptionCount() int {
	if tr.messageBus != nil {
		return tr.messageBus.GetSubscriptionCount()
	}
	return 0
}

// P2PTimeout 获取 P2P 超时秒数。
func (tr *TeamRuntime) P2PTimeout() float64 {
	return tr.p2pTimeout
}

// SetP2PTimeout 设置 P2P 超时秒数。
func (tr *TeamRuntime) SetP2PTimeout(timeout float64) {
	tr.p2pTimeout = timeout
}

// GetP2PTimeout 获取 P2P 超时秒数。
func (tr *TeamRuntime) GetP2PTimeout() float64 {
	return tr.p2pTimeout
}

// IsRunning 返回运行时是否已启动。
func (tr *TeamRuntime) IsRunning() bool {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return tr.running
}

// SetMessageBus 设置消息总线，供外部注入。
func (tr *TeamRuntime) SetMessageBus(bus MessageBusInterface) {
	tr.messageBus = bus
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// wrapProvider 包装 Agent provider，在 Agent 创建后自动注入 RuntimeBindable。
//
// 对应 Python: TeamRuntime._wrap_provider(provider, agent_id)
func (tr *TeamRuntime) wrapProvider(provider resources_manager.AgentProvider, agentID string) resources_manager.AgentProvider {
	return func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		agent, err := provider(ctx, card)
		if err != nil {
			return nil, err
		}
		if bindable, ok := agent.(RuntimeBindable); ok {
			bindable.BindRuntime(tr, agentID)
			logger.Info(logComponent).
				Str("event_type", "RUNTIME_BINDABLE_AUTO_BOUND").
				Str("agent_id", agentID).
				Str("team_id", tr.teamID).
				Msg("自动绑定 CommunicableAgent")
		} else {
			logger.Warn(logComponent).
				Str("event_type", "RUNTIME_BINDABLE_NOT_IMPLEMENTED").
				Str("agent_id", agentID).
				Msg("Agent 未实现 RuntimeBindable，通信方法不可用")
		}
		return agent, nil
	}
}
