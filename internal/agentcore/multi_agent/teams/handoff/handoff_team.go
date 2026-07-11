package handoff

import (
	"context"
	"fmt"
	"sync"
	"time"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/teams"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HandoffTeam 事件驱动单活跃 Agent 交接多 Agent 团队。
//
// 交接编排模式下，Agent 按照预定义的路由规则依次交接任务，
// 每个 Agent 处理完毕后决定下一步交接给哪个 Agent，直到满足终止条件。
//
// 职责：
//   - 管理 Agent 注册和配置
//   - 懒初始化内部 ContainerAgent 端点
//   - 执行交接链路（创建协调器 → 发布 → 等待 → 清理）
//   - 委托 TeamRuntime 进行消息通信
//
// 对应 Python: HandoffTeam (handoff_team.py)
type HandoffTeam struct {
	// card 团队身份卡片
	card maschema.TeamCardInterface
	// config 完整配置
	config HandoffTeamConfig
	// runtime 团队运行时
	runtime *team_runtime.TeamRuntime
	// agentProviders Agent 资源提供者映射
	agentProviders map[string]maschema.TeamAgentProvider
	// internalAgentsReady 内部 Agent 是否就绪
	internalAgentsReady bool
	// internalAgentsErr ensureInternalAgents 首次执行的错误结果
	internalAgentsErr error
	// coordinatorRegistry 会话协调器注册表
	coordinatorRegistry map[string]*HandoffOrchestrator
	// registryMu coordinatorRegistry 并发保护锁
	registryMu sync.RWMutex
	// initLock 初始化锁
	initLock sync.Mutex
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// handoffEndpointPrefix 内部端点 ID 前缀
	handoffEndpointPrefix = "__handoff_ep_"
	// containerTopicPrefix 容器主题前缀
	containerTopicPrefix = "container_"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 HandoffTeam 满足 BaseTeam 接口
var _ maschema.BaseTeam = (*HandoffTeam)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHandoffTeam 创建 HandoffTeam 实例。
//
// 参数：
//   - card：团队身份卡片
//   - config：完整配置，nil 时使用默认值
//   - runtime：团队运行时，nil 时自动创建
//
// 对应 Python: HandoffTeam(card, config=None)
func NewHandoffTeam(card maschema.TeamCardInterface, config *HandoffTeamConfig, runtime *team_runtime.TeamRuntime) *HandoffTeam {
	if config == nil {
		defaultCfg := NewHandoffTeamConfig()
		config = defaultCfg
	}

	teamID := card.GetID()
	var tr *team_runtime.TeamRuntime
	if runtime != nil {
		tr = runtime
	} else {
		// 对齐 Python: BaseTeam._create_default_runtime()
		// 字段映射：TeamConfig.max_concurrent_messages → MessageBusConfig.max_queue_size
		// 字段映射：TeamConfig.message_timeout → MessageBusConfig.process_timeout
		busCfg := team_runtime.NewMessageBusConfig(
			team_runtime.WithMaxQueueSize(config.MaxConcurrentMessages),
			team_runtime.WithProcessTimeout(config.MessageTimeout),
			team_runtime.WithTeamID(teamID),
		)
		rtCfg := team_runtime.NewRuntimeConfig(
			team_runtime.WithRuntimeTeamID(teamID),
			team_runtime.WithRuntimeMessageBus(busCfg),
		)
		tr = team_runtime.NewTeamRuntime(*rtCfg)
	}

	team := &HandoffTeam{
		card:                card,
		config:              *config,
		runtime:             tr,
		agentProviders:      make(map[string]maschema.TeamAgentProvider),
		internalAgentsReady: false,
		coordinatorRegistry: make(map[string]*HandoffOrchestrator),
	}

	logger.Info(logComponent).
		Str("action", "new_handoff_team").
		Str("team_id", teamID).
		Msg("创建 HandoffTeam")

	return team
}

// Invoke 非流式调用团队，执行交接链路。
//
// 使用 StandaloneInvokeContext 管理会话生命周期（创建/PreRun/Bind/Unbind/Cleanup/Close/Commit），
// 在上下文内执行 runChain 完成交接编排。
//
// 对应 Python: HandoffTeam.invoke(message, session=None)
func (t *HandoffTeam) Invoke(ctx context.Context, inputs map[string]any, opts ...maschema.TeamOption) (any, error) {
	teamOpts := maschema.NewTeamOptions(opts...)
	sess := teamOpts.Session

	result, err := teams.StandaloneInvokeContext(ctx, t.runtime, t.card, inputs, sess,
		func(teamSession *session.AgentTeamSession, _ string) (map[string]any, error) {
			return t.runChain(ctx, inputs, teamSession)
		},
	)

	if err != nil {
		return nil, err
	}
	return result, nil
}

// Stream 流式调用团队，通过 standalone_stream_context 实现。
// 后台 goroutine 运行 runChain，前台从 session.StreamIterator() 消费流式输出。
//
// 对齐 Python: HandoffTeam.stream(message, session=None)
// Python 使用 standalone_stream_context，Go 使用 StandaloneStreamContext 对齐。
func (t *HandoffTeam) Stream(ctx context.Context, inputs map[string]any, opts ...maschema.TeamOption) (<-chan stream.Schema, error) {
	teamOpts := maschema.NewTeamOptions(opts...)
	sess := teamOpts.Session

	return teams.StandaloneStreamContext(ctx, t.runtime, t.card, inputs, sess,
		func(teamSession *session.AgentTeamSession, _ string) error {
			_, err := t.runChain(ctx, inputs, teamSession)
			return err
		},
	)
}

// AddAgent 向团队注册 Agent。
//
// 如果 Agent 已存在则跳过，否则注册到运行时并标记内部 Agent 需要重新初始化。
//
// 对应 Python: HandoffTeam.add_agent(card, provider) -> self
func (t *HandoffTeam) AddAgent(ctx context.Context, card *agentschema.AgentCard, provider maschema.TeamAgentProvider, _ ...maschema.TeamOption) error {
	if t.runtime.HasAgent(card.ID) {
		logger.Warn(logComponent).
			Str("action", "add_agent_skip").
			Str("agent_id", card.ID).
			Str("team_id", t.card.GetID()).
			Msg("Agent 已存在，跳过注册")
		return nil
	}

	// 对齐 Python: if self.runtime.get_agent_count() >= self.config.max_agents
	if t.config.MaxAgents > 0 && t.runtime.GetAgentCount() >= t.config.MaxAgents {
		return exception.BuildError(exception.StatusAgentTeamAddRuntimeError,
			exception.WithParam("error_msg", fmt.Sprintf(
				"Agent 数量超过上限 (%d)", t.config.MaxAgents,
			)),
		)
	}

	// 注册到运行时（包装为 resources_manager.AgentProvider）
	wrappedProvider := t.wrapTeamAgentProvider(provider)
	if err := t.runtime.RegisterAgent(ctx, card, wrappedProvider); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HandoffTeam.AddAgent").
			Str("agent_id", card.ID).
			Msg("注册 Agent 到运行时失败")
		return err
	}

	// 对齐 Python: self.card.agent_cards.append(card)
	t.card.AddAgentCard(card)

	// 存储原始 TeamAgentProvider（ContainerAgent 创建时使用）
	t.agentProviders[card.ID] = provider

	// 标记内部 Agent 需要重新初始化（在锁保护下重置，确保与 ensureInternalAgents 互斥）
	t.initLock.Lock()
	t.resetInternalAgents()
	t.initLock.Unlock()

	logger.Info(logComponent).
		Str("action", "add_agent").
		Str("agent_id", card.ID).
		Str("team_id", t.card.GetID()).
		Msg("Agent 已注册到 HandoffTeam")

	return nil
}

// RemoveAgent 从团队注销 Agent。
//
// 对应 Python: BaseTeam.remove_agent(agent)
func (t *HandoffTeam) RemoveAgent(ctx context.Context, agentID string) error {
	_, err := t.runtime.UnregisterAgent(ctx, agentID)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HandoffTeam.RemoveAgent").
			Str("agent_id", agentID).
			Msg("注销 Agent 失败")
		return err
	}

	// 从 agentProviders 中移除
	delete(t.agentProviders, agentID)

	// 对齐 Python: self.card.agent_cards = [c for c in self.card.agent_cards if c.id != removed_card.id]
	t.card.RemoveAgentCard(agentID)

	// 标记内部 Agent 需要重新初始化（在锁保护下重置，确保与 ensureInternalAgents 互斥）
	t.initLock.Lock()
	t.resetInternalAgents()
	t.initLock.Unlock()

	logger.Info(logComponent).
		Str("action", "remove_agent").
		Str("agent_id", agentID).
		Str("team_id", t.card.GetID()).
		Msg("Agent 已从 HandoffTeam 注销")

	return nil
}

// Send P2P 发送消息，委托运行时。
//
// 对应 Python: BaseTeam.send(message, recipient, sender, session_id, timeout)
func (t *HandoffTeam) Send(ctx context.Context, message map[string]any, recipient string, sender string, opts ...maschema.TeamOption) (any, error) {
	return t.runtime.Send(ctx, message, recipient, sender, opts...)
}

// Publish Pub-Sub 发布消息，委托运行时。
//
// 对应 Python: BaseTeam.publish(message, topic_id, sender, session_id)
func (t *HandoffTeam) Publish(ctx context.Context, message map[string]any, topicID string, sender string, opts ...maschema.TeamOption) error {
	return t.runtime.Publish(ctx, message, topicID, sender, opts...)
}

// Subscribe 订阅主题，委托运行时。
//
// 对应 Python: BaseTeam.subscribe(agent_id, topic)
func (t *HandoffTeam) Subscribe(ctx context.Context, agentID string, topic string) error {
	return t.runtime.Subscribe(ctx, agentID, topic)
}

// Unsubscribe 取消订阅，委托运行时。
//
// 对应 Python: BaseTeam.unsubscribe(agent_id, topic)
func (t *HandoffTeam) Unsubscribe(ctx context.Context, agentID string, topic string) error {
	return t.runtime.Unsubscribe(ctx, agentID, topic)
}

// Configure 配置团队。
//
// 对应 Python: BaseTeam.configure(config) -> self
func (t *HandoffTeam) Configure(_ context.Context, config maschema.TeamConfig) error {
	t.config.TeamConfig = config
	logger.Info(logComponent).
		Str("action", "configure").
		Str("team_id", t.card.GetID()).
		Msg("HandoffTeam 配置已更新")
	return nil
}

// GetAgentCard 获取 Agent 卡片，委托运行时。
//
// 对应 Python: BaseTeam.get_agent_card(agent_id)
func (t *HandoffTeam) GetAgentCard(agentID string) (*agentschema.AgentCard, error) {
	return t.runtime.GetAgentCard(agentID)
}

// GetAgentCount 获取 Agent 数量，委托运行时。
//
// 对应 Python: BaseTeam.get_agent_count()
func (t *HandoffTeam) GetAgentCount() int {
	return t.runtime.GetAgentCount()
}

// ListAgents 列出所有 Agent ID，委托运行时。
//
// 对应 Python: BaseTeam.list_agents()
func (t *HandoffTeam) ListAgents() []string {
	return t.runtime.ListAgents()
}

// Card 返回团队身份卡片。
//
// 对应 Python: BaseTeam.card 属性
func (t *HandoffTeam) Card() maschema.TeamCardInterface {
	return t.card
}

// Config 返回团队配置。
//
// 对应 Python: BaseTeam.config 属性
func (t *HandoffTeam) Config() *maschema.TeamConfig {
	return &t.config.TeamConfig
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// lookupCoordinator 查找会话协调器。
//
// 对应 Python: HandoffTeam._lookup_coordinator(session_id)
func (t *HandoffTeam) lookupCoordinator(sessionID string) *HandoffOrchestrator {
	t.registryMu.RLock()
	defer t.registryMu.RUnlock()
	return t.coordinatorRegistry[sessionID]
}

// getStartAgentID 获取起始 Agent ID。
//
// 配置了 StartAgent 时返回其 ID，否则返回第一个 Agent 的 ID。
//
// 对应 Python: HandoffTeam._get_start_agent_id()
func (t *HandoffTeam) getStartAgentID() string {
	cfg := t.config.Handoff
	if cfg.StartAgent != nil {
		return cfg.StartAgent.ID
	}
	agentCards := t.card.GetAgentCards()
	if len(agentCards) > 0 {
		return agentCards[0].ID
	}
	return ""
}

// ensureInternalAgents 确保内部 Agent 端点已初始化。
// 使用 initLock + internalAgentsReady bool 控制初始化，
// 保证与 AddAgent/RemoveAgent 的 resetInternalAgents 互斥。
//
// 流程：
//  1. sync.Once 保证初始化逻辑只执行一次（并发安全）
//  2. 构建 routeGraph = BuildRouteGraph(agentIDs, routes)
//  3. 对每个 agentID：
//     - endpointID = __handoff_ep_{teamID}_{agentID}
//     - endpointCard = AgentCard{ID: endpointID, Name: endpointID}
//     - containerProvider = makeContainerProvider(card, agentID, allowedTargets)
//     - runtime.RegisterAgent(endpointCard, containerProvider)
//     - runtime.Subscribe(endpointID, container_{agentID})
//
// 对应 Python: HandoffTeam._ensure_internal_agents()
func (t *HandoffTeam) ensureInternalAgents(ctx context.Context) error {
	t.initLock.Lock()
	defer t.initLock.Unlock()

	if t.internalAgentsReady {
		return t.internalAgentsErr
	}

	err := t.initInternalAgents(ctx)
	if err != nil {
		t.internalAgentsErr = err
		return err
	}
	t.internalAgentsReady = true
	return nil
}

// initInternalAgents 执行内部 Agent 端点的实际初始化逻辑。
//
// 包含构建路由图、创建端点卡片、注册到运行时、订阅容器主题等步骤。
func (t *HandoffTeam) initInternalAgents(ctx context.Context) error {
	cfg := t.config.Handoff
	agentCards := t.card.GetAgentCards()
	agentIDs := make([]string, 0, len(agentCards))
	for _, c := range agentCards {
		agentIDs = append(agentIDs, c.ID)
	}

	routeGraph := BuildRouteGraph(agentIDs, cfg.Routes)

	teamID := t.card.GetID()
	for _, agentID := range agentIDs {
		card, err := t.runtime.GetAgentCard(agentID)
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "LLM_CALL_ERROR").
				Str("method", "HandoffTeam.ensureInternalAgents").
				Str("agent_id", agentID).
				Msg("获取 Agent 卡片失败")
			return fmt.Errorf("获取 Agent 卡片失败: %w", err)
		}

		// 构建允许目标列表
		allowedTargets := make(map[string]struct{})
		if targets, ok := routeGraph[agentID]; ok {
			for target := range targets {
				allowedTargets[target] = struct{}{}
			}
		}

		// 创建端点标识和卡片
		endpointID := fmt.Sprintf("%s%s_%s", handoffEndpointPrefix, teamID, agentID)
		endpointCard := agentschema.NewAgentCard(
			agentschema.WithAgentID(endpointID),
			agentschema.WithAgentName(endpointID),
		)

		// 创建 ContainerAgent provider
		containerProvider := t.makeContainerProvider(card, agentID, allowedTargets)

		// 注册端点到运行时
		if err := t.runtime.RegisterAgent(ctx, endpointCard, containerProvider); err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "LLM_CALL_ERROR").
				Str("method", "HandoffTeam.ensureInternalAgents").
				Str("endpoint_id", endpointID).
				Msg("注册端点 Agent 失败")
			return fmt.Errorf("注册端点 Agent 失败: %w", err)
		}

		// 订阅容器主题
		topic := containerTopicPrefix + agentID
		if err := t.runtime.Subscribe(ctx, endpointID, topic); err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "LLM_CALL_ERROR").
				Str("method", "HandoffTeam.ensureInternalAgents").
				Str("endpoint_id", endpointID).
				Str("topic", topic).
				Msg("订阅容器主题失败")
			return fmt.Errorf("订阅容器主题失败: %w", err)
		}
	}

	logger.Info(logComponent).
		Str("action", "ensure_internal_agents").
		Str("team_id", teamID).
		Int("agent_count", len(agentIDs)).
		Msg("内部 Agent 端点初始化完成")

	return nil
}

// resetInternalAgents 重置内部 Agent 初始化状态。
//
// 在 AddAgent 或 RemoveAgent 后调用，使后续 ensureInternalAgents 调用重新执行初始化。
// 必须在 initLock 保护下调用，确保与 ensureInternalAgents 互斥。
func (t *HandoffTeam) resetInternalAgents() {
	t.internalAgentsReady = false
	t.internalAgentsErr = nil
}

// makeContainerProvider 创建 ContainerAgent provider 闭包。
//
// 返回的 provider 每次调用时创建新的 ContainerAgent 实例，
// 注入目标 Agent 的卡片、provider、允许目标和协调器查找函数。
//
// 对应 Python: HandoffTeam._make_container_provider(card, agent_id, allowed_targets)
func (t *HandoffTeam) makeContainerProvider(
	card *agentschema.AgentCard,
	agentID string,
	allowedTargets map[string]struct{},
) resources_manager.AgentProvider {
	coordinatorLookup := t.lookupCoordinator
	agentProvider := t.agentProviders[agentID]
	runtime := t.runtime
	teamID := t.card.GetID()

	return func(ctx context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		container := NewContainerAgent(card, agentProvider, allowedTargets, coordinatorLookup)
		endpointID := fmt.Sprintf("%s%s_%s", handoffEndpointPrefix, teamID, agentID)
		container.BindRuntime(runtime, endpointID)
		return container, nil
	}
}

// runChain 执行交接链路。
//
// 流程：
//  1. ensureInternalAgents()
//  2. coordinator = RestoreFromSession(session, startAgentID, agentIDs, config)
//  3. 恢复 history（isResume 时过滤中断项）
//  4. coordinatorRegistry[sessionID] = coordinator
//  5. runtime.Publish(HandoffRequest, container_{currentAgentID})
//  6. 等待 coordinator.DoneCh()（带超时：select + time.After）
//  7. 清理：移除 coordinator、CleanupSession
//
// 对应 Python: HandoffTeam._run_chain(message, session)
func (t *HandoffTeam) runChain(ctx context.Context, inputs map[string]any, sess *session.AgentTeamSession) (map[string]any, error) {
	sessionID := sess.GetSessionID()

	// 确保内部 Agent 已就绪
	if err := t.ensureInternalAgents(ctx); err != nil {
		return nil, err
	}

	// 从会话恢复协调器
	cfg := t.config.Handoff
	agentCards := t.card.GetAgentCards()
	agentIDs := make([]string, 0, len(agentCards))
	for _, c := range agentCards {
		agentIDs = append(agentIDs, c.ID)
	}

	coordinator := RestoreFromSession(sess, t.getStartAgentID(), agentIDs, &cfg)

	// 恢复交接历史
	history := make([]HandoffHistoryEntry, 0)
	historyVal, _ := sess.GetState(state.StringKey(HandoffHistoryKey))
	if historyVal != nil {
		if h, ok := historyVal.([]HandoffHistoryEntry); ok {
			history = h
		}
		// 尝试从 []any 转换
		if hAny, ok := historyVal.([]any); ok {
			for _, item := range hAny {
				if entry, ok := item.(HandoffHistoryEntry); ok {
					history = append(history, entry)
				}
			}
		}
	}

	// 检查是否恢复会话，过滤中断项
	isResume := false
	coordStateVal, _ := sess.GetState(state.StringKey(CoordinatorStateKey))
	if coordStateVal != nil {
		isResume = true
	}
	if isResume {
		history = filterInterruptHistory(history)
	}

	// 注册协调器
	t.registryMu.Lock()
	t.coordinatorRegistry[sessionID] = coordinator
	t.registryMu.Unlock()

	// 构建交接请求
	handoffReq := &HandoffRequest{
		InputMessage: inputs,
		History:      history,
		Session:      sess,
	}

	// 发布交接请求到当前活跃 Agent 的容器主题
	currentAgentID := coordinator.CurrentAgentID()
	topic := containerTopicPrefix + currentAgentID

	logger.Info(logComponent).
		Str("action", "run_chain_publish").
		Str("team_id", t.card.GetID()).
		Str("session_id", sessionID).
		Str("current_agent_id", currentAgentID).
		Str("topic", topic).
		Msg("发布交接请求到容器主题")

	if err := t.runtime.Publish(ctx, handoffReq, topic, t.card.GetID(),
		maschema.WithTeamSessionID(sessionID),
	); err != nil {
		// 发布失败，清理协调器
		t.registryMu.Lock()
		delete(t.coordinatorRegistry, sessionID)
		t.registryMu.Unlock()
		_ = t.runtime.CleanupSession(ctx, sessionID)

		logger.Error(logComponent).Err(err).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HandoffTeam.runChain").
			Str("team_id", t.card.GetID()).
			Str("session_id", sessionID).
			Msg("发布交接请求失败")
		return nil, exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithCause(err),
			exception.WithParam("error_msg", fmt.Sprintf("发布交接请求失败: %s", err.Error())),
		)
	}

	// 等待协调器完成，带超时
	timeout := t.config.MessageTimeout
	var result map[string]any
	var coordErr error
	if timeout > 0 {
		select {
		case hr := <-coordinator.DoneCh():
			result, coordErr = hr.result, hr.err
		case <-time.After(time.Duration(timeout * float64(time.Second))):
			// 超时，清理协调器
			t.registryMu.Lock()
			delete(t.coordinatorRegistry, sessionID)
			t.registryMu.Unlock()
			_ = t.runtime.CleanupSession(ctx, sessionID)

			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("method", "HandoffTeam.runChain").
				Str("team_id", t.card.GetID()).
				Str("session_id", sessionID).
				Float64("timeout", timeout).
				Msg("交接编排超时")
			return nil, exception.BuildError(exception.StatusAgentTeamExecutionError,
				exception.WithParam("error_msg", fmt.Sprintf("交接编排超时 (%.1fs)", timeout)),
			)
		case <-ctx.Done():
			// 上下文取消，清理协调器
			t.registryMu.Lock()
			delete(t.coordinatorRegistry, sessionID)
			t.registryMu.Unlock()
			_ = t.runtime.CleanupSession(ctx, sessionID)
			return nil, ctx.Err()
		}
	} else {
		select {
		case hr := <-coordinator.DoneCh():
			result, coordErr = hr.result, hr.err
		case <-ctx.Done():
			// 上下文取消，清理协调器
			t.registryMu.Lock()
			delete(t.coordinatorRegistry, sessionID)
			t.registryMu.Unlock()
			_ = t.runtime.CleanupSession(ctx, sessionID)
			return nil, ctx.Err()
		}
	}

	// 清理协调器和会话
	t.registryMu.Lock()
	delete(t.coordinatorRegistry, sessionID)
	t.registryMu.Unlock()
	_ = t.runtime.CleanupSession(ctx, sessionID)

	// 对齐 Python: await coordinator.done_future — 若有异常则传播
	if coordErr != nil {
		logger.Error(logComponent).Err(coordErr).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HandoffTeam.runChain").
			Str("team_id", t.card.GetID()).
			Str("session_id", sessionID).
			Msg("交接编排发生错误")
		return nil, coordErr
	}

	logger.Info(logComponent).
		Str("action", "run_chain_complete").
		Str("team_id", t.card.GetID()).
		Str("session_id", sessionID).
		Msg("交接链路执行完成")

	return result, nil
}

// wrapTeamAgentProvider 将 TeamAgentProvider 包装为 resources_manager.AgentProvider。
//
// TeamAgentProvider 签名：func(ctx, *AgentCard) (BaseAgent, error)
// AgentProvider 签名：func(ctx, *AgentCard) (BaseAgent, error)
// 两者签名完全一致，直接类型转换。
func (t *HandoffTeam) wrapTeamAgentProvider(provider maschema.TeamAgentProvider) resources_manager.AgentProvider {
	return resources_manager.AgentProvider(provider)
}

// filterInterruptHistory 过滤交接历史中的中断项。
//
// 对应 Python: filtered = [h for h in history if not (isinstance(h.get("output"), dict) and h.get("output", {}).get("result_type") == "interrupt")]
func filterInterruptHistory(history []HandoffHistoryEntry) []HandoffHistoryEntry {
	var filtered []HandoffHistoryEntry
	for _, h := range history {
		if h.Output == nil {
			filtered = append(filtered, h)
			continue
		}
		resultType, ok := h.Output["result_type"]
		if ok {
			if rtStr, ok := resultType.(string); ok && rtStr == "interrupt" {
				continue
			}
		}
		filtered = append(filtered, h)
	}
	return filtered
}
