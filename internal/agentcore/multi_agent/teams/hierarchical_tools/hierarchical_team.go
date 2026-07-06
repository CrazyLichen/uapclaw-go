package hierarchical_tools

import (
	"context"
	"fmt"
	"time"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/teams"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HierarchicalToolsTeam 工具委托层级多 Agent 团队。
//
// 子 Agent 通过 AddAgent + WithParentAgentID() 注册到父 Agent 的 ability_manager，
// LLM 将子 Agent 视为可调用的工具（tool_call），
// 子 Agent 的执行由 AbilityManager.executeAgent() → Runner.RunAgent() 完成。
// 支持多级树状层级（父→子→孙），任意 Agent 都可作为父节点。
//
// 对应 Python: HierarchicalTeam (hierarchical_tools/hierarchical_team.py)
type HierarchicalToolsTeam struct {
	// card 团队身份卡片
	card maschema.TeamCardInterface
	// config 完整配置
	config HierarchicalToolsTeamConfig
	// runtime 团队运行时
	runtime *team_runtime.TeamRuntime
	// rootAgentID 根/入口 Agent ID
	rootAgentID string
	// pendingChildren 待注册的父子关系：parentID → []childAgentCard
	pendingChildren map[string][]*agentschema.AgentCard
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// toolsLogComponent 日志组件标识
	toolsLogComponent = logger.ComponentChannel
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 HierarchicalToolsTeam 满足 BaseTeam 接口
var _ maschema.BaseTeam = (*HierarchicalToolsTeam)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHierarchicalToolsTeam 创建 HierarchicalToolsTeam 实例。
//
// 参数：
//   - card：团队身份卡片
//   - config：完整配置
//   - runtime：团队运行时，nil 时自动创建
//
// 对应 Python: HierarchicalTeam(card, config)
func NewHierarchicalToolsTeam(card maschema.TeamCardInterface, config *HierarchicalToolsTeamConfig, runtime *team_runtime.TeamRuntime) *HierarchicalToolsTeam {
	if config == nil {
		defaultCfg := NewHierarchicalToolsTeamConfig()
		config = defaultCfg
	}

	// 对齐 Python: root_agent: AgentCard = Field(...) 必填
	if config.RootAgent == nil {
		panic("NewHierarchicalToolsTeam: config.RootAgent 不能为 nil")
	}

	teamID := card.GetID()
	var tr *team_runtime.TeamRuntime
	if runtime != nil {
		tr = runtime
	} else {
		rtCfg := team_runtime.NewRuntimeConfig(
			team_runtime.WithRuntimeTeamID(teamID),
		)
		tr = team_runtime.NewTeamRuntime(*rtCfg)
	}

	var rootAgentID string
	if config.RootAgent != nil {
		rootAgentID = config.RootAgent.ID
	}

	team := &HierarchicalToolsTeam{
		card:            card,
		config:          *config,
		runtime:         tr,
		rootAgentID:     rootAgentID,
		pendingChildren: make(map[string][]*agentschema.AgentCard),
	}

	logger.Info(toolsLogComponent).
		Str("action", "new_hierarchical_tools_team").
		Str("team_id", teamID).
		Str("root_agent_id", rootAgentID).
		Msg("创建 HierarchicalToolsTeam")

	return team
}

// Invoke 非流式调用团队，通过 root_agent 运行并返回最终结果。
//
// 对应 Python: HierarchicalTeam.invoke(message, session)
func (t *HierarchicalToolsTeam) Invoke(ctx context.Context, inputs map[string]any, opts ...maschema.TeamOption) (any, error) {
	if err := t.assertReady(); err != nil {
		return nil, err
	}

	if err := t.setupHierarchy(ctx); err != nil {
		return nil, err
	}

	teamOpts := maschema.NewTeamOptions(opts...)
	sess := teamOpts.Session
	timeout := teamOpts.Timeout

	result, err := teams.StandaloneInvokeContext(ctx, t.runtime, t.card, inputs, sess,
		func(teamSession *session.AgentTeamSession, sessionID string) (map[string]any, error) {
			logger.Debug(toolsLogComponent).
				Str("action", "hierarchical_tools_invoke").
				Str("session_id", sessionID).
				Str("root_agent_id", t.rootAgentID).
				Msg("开始 invoke")

			res, sendErr := t.runtime.Send(ctx, inputs, t.rootAgentID, t.card.GetID(),
				maschema.WithTeamSessionID(sessionID),
				maschema.WithTeamTimeout(timeout),
			)
			if sendErr != nil {
				logger.Error(toolsLogComponent).Err(sendErr).
					Str("event_type", "LLM_CALL_ERROR").
					Str("method", "HierarchicalToolsTeam.Invoke").
					Str("root_agent_id", t.rootAgentID).
					Msg("Send 到 root_agent 失败")
				return nil, sendErr
			}

			logger.Debug(toolsLogComponent).
				Str("action", "hierarchical_tools_invoke_end").
				Str("session_id", sessionID).
				Msg("invoke 结束")

			resultMap, ok := res.(map[string]any)
			if !ok {
				resultMap = map[string]any{"result": res}
			}
			return resultMap, nil
		},
	)

	return result, err
}

// Stream 流式调用团队，直接调用 root_agent.Stream() 逐 chunk 转发。
//
// 与 msgbus 模式的关键区别：msgbus 走 runtime.Send() 等完整结果后一次性 WriteStream；
// tools 模式直接调用 agent.Stream() 逐 chunk 转发，提供真正的流式体验。
//
// 对应 Python: HierarchicalTeam.stream(message, session)
func (t *HierarchicalToolsTeam) Stream(ctx context.Context, inputs map[string]any, opts ...maschema.TeamOption) (<-chan stream.Schema, error) {
	if err := t.assertReady(); err != nil {
		return nil, err
	}

	if err := t.setupHierarchy(ctx); err != nil {
		return nil, err
	}

	teamOpts := maschema.NewTeamOptions(opts...)
	sess := teamOpts.Session
	timeout := teamOpts.Timeout

	// 如果有 timeout，给 ctx 加 deadline
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout*float64(time.Second)))
		defer cancel()
	}

	logger.Debug(toolsLogComponent).
		Str("action", "hierarchical_tools_stream").
		Str("root_agent_id", t.rootAgentID).
		Msg("开始 stream")

	return teams.StandaloneStreamContext(ctx, t.runtime, t.card, inputs, sess,
		func(teamSession *session.AgentTeamSession, sessionID string) error {
			// 从全局 ResourceMgr 获取 root_agent 实例
			resourceMgr := runner.GetResourceMgr()
			if resourceMgr == nil {
				return exception.BuildError(exception.StatusAgentTeamAgentNotFound,
					exception.WithParam("error_msg", "ResourceMgr 未初始化"),
				)
			}

			agents, err := resourceMgr.GetAgent(ctx, []string{t.rootAgentID})
			if err != nil || len(agents) == 0 {
				logger.Error(toolsLogComponent).Err(err).
					Str("event_type", "LLM_CALL_ERROR").
					Str("method", "HierarchicalToolsTeam.Stream").
					Str("root_agent_id", t.rootAgentID).
					Msg("获取 root_agent 实例失败")
				return exception.BuildError(exception.StatusAgentTeamAgentNotFound,
					exception.WithParam("error_msg", fmt.Sprintf("root_agent '%s' 实例未找到", t.rootAgentID)),
				)
			}

			agent := agents[0]

			// 构造带 conversation_id 和 sender 的 inputs
			inputsWithSID := make(map[string]any, len(inputs)+2)
			for k, v := range inputs {
				inputsWithSID[k] = v
			}
			inputsWithSID["conversation_id"] = sessionID
			inputsWithSID["sender"] = t.card.GetID()

			// 直接调用 agent.Stream() 逐 chunk 转发
			ch, streamErr := agent.Stream(ctx, inputsWithSID)
			if streamErr != nil {
				logger.Error(toolsLogComponent).Err(streamErr).
					Str("event_type", "LLM_CALL_ERROR").
					Str("method", "HierarchicalToolsTeam.Stream").
					Str("root_agent_id", t.rootAgentID).
					Msg("root_agent.Stream() 调用失败")
				// 对齐 Python: error_result = {"output": str(e), "result_type": "error"}
				errorResult := map[string]any{
					"output":      streamErr.Error(),
					"result_type": "error",
				}
				_ = teamSession.WriteStream(ctx, errorResult)
				return streamErr
			}

			for chunk := range ch {
				if writeErr := teamSession.WriteStream(ctx, chunk); writeErr != nil {
					logger.Warn(toolsLogComponent).Err(writeErr).
						Str("action", "hierarchical_tools_stream_write").
						Msg("写入流失败")
				}
			}

			logger.Debug(toolsLogComponent).
				Str("action", "hierarchical_tools_stream_end").
				Str("session_id", sessionID).
				Msg("stream 结束")
			return nil
		},
	)
}

// AddAgent 向团队注册 Agent。
//
// 若 Agent 已存在则跳过，否则注册到运行时。
// 通过 WithParentAgentID() Option 声明层级关系，对齐 Python 的 parent_agent_id 参数。
//
// 对应 Python: HierarchicalTeam.add_agent(card, provider, parent_agent_id=None)
func (t *HierarchicalToolsTeam) AddAgent(ctx context.Context, card *agentschema.AgentCard, provider maschema.TeamAgentProvider, opts ...maschema.TeamOption) error {
	if t.runtime.HasAgent(card.ID) {
		logger.Warn(toolsLogComponent).
			Str("action", "add_agent_skip").
			Str("agent_id", card.ID).
			Str("team_id", t.card.GetID()).
			Msg("Agent 已存在，跳过注册")
		return nil
	}

	// 对齐 Python: if self.runtime.get_agent_count() >= self.config.max_agents
	if t.config.TeamConfig.MaxAgents > 0 && t.runtime.GetAgentCount() >= t.config.TeamConfig.MaxAgents {
		return exception.BuildError(exception.StatusAgentTeamAddRuntimeError,
			exception.WithParam("error_msg", fmt.Sprintf(
				"Agent 数量超过上限 (%d)", t.config.TeamConfig.MaxAgents,
			)),
		)
	}

	// 注册到运行时（包装为 resources_manager.AgentProvider）
	wrappedProvider := resources_manager.AgentProvider(provider)
	if err := t.runtime.RegisterAgent(ctx, card, wrappedProvider); err != nil {
		logger.Error(toolsLogComponent).Err(err).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HierarchicalToolsTeam.AddAgent").
			Str("agent_id", card.ID).
			Msg("注册 Agent 到运行时失败")
		return err
	}

	// 对齐 Python: self.card.agent_cards.append(card)
	t.card.AddAgentCard(card)

	// 识别 rootAgent
	if card.ID == t.rootAgentID {
		logger.Info(toolsLogComponent).
			Str("action", "add_agent_root").
			Str("root_agent_id", card.ID).
			Str("team_id", t.card.GetID()).
			Msg("注册 root_agent 到 HierarchicalToolsTeam")
	}

	// 对齐 Python: if parent_agent_id: self._pending_children.setdefault(parent_agent_id, []).append(card)
	teamOpts := maschema.NewTeamOptions(opts...)
	if teamOpts.ParentAgentID != "" {
		t.pendingChildren[teamOpts.ParentAgentID] = append(t.pendingChildren[teamOpts.ParentAgentID], card)
		logger.Debug(toolsLogComponent).
			Str("action", "add_agent_with_parent").
			Str("child_id", card.ID).
			Str("parent_id", teamOpts.ParentAgentID).
			Msg("记录父子关系到 pendingChildren")
	}

	logger.Info(toolsLogComponent).
		Str("action", "add_agent").
		Str("agent_id", card.ID).
		Str("team_id", t.card.GetID()).
		Msg("Agent 已注册到 HierarchicalToolsTeam")

	return nil
}

// RemoveAgent 从团队注销 Agent。
//
// 对应 Python: BaseTeam.remove_agent(agent)
func (t *HierarchicalToolsTeam) RemoveAgent(ctx context.Context, agentID string) error {
	_, err := t.runtime.UnregisterAgent(ctx, agentID)
	if err != nil {
		logger.Error(toolsLogComponent).Err(err).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HierarchicalToolsTeam.RemoveAgent").
			Str("agent_id", agentID).
			Msg("注销 Agent 失败")
		return err
	}

	// 对齐 Python: self.card.agent_cards = [c for c in self.card.agent_cards if c.id != removed_card.id]
	t.card.RemoveAgentCard(agentID)

	logger.Info(toolsLogComponent).
		Str("action", "remove_agent").
		Str("agent_id", agentID).
		Str("team_id", t.card.GetID()).
		Msg("Agent 已从 HierarchicalToolsTeam 注销")

	return nil
}

// Send P2P 发送消息，委托运行时。
//
// 对应 Python: BaseTeam.send(message, recipient, sender, session_id, timeout)
func (t *HierarchicalToolsTeam) Send(ctx context.Context, message map[string]any, recipient string, sender string, opts ...maschema.TeamOption) (any, error) {
	return t.runtime.Send(ctx, message, recipient, sender, opts...)
}

// Publish Pub-Sub 发布消息，委托运行时。
//
// 对应 Python: BaseTeam.publish(message, topic_id, sender, session_id)
func (t *HierarchicalToolsTeam) Publish(ctx context.Context, message map[string]any, topicID string, sender string, opts ...maschema.TeamOption) error {
	return t.runtime.Publish(ctx, message, topicID, sender, opts...)
}

// Subscribe 订阅主题，委托运行时。
//
// 对应 Python: BaseTeam.subscribe(agent_id, topic)
func (t *HierarchicalToolsTeam) Subscribe(ctx context.Context, agentID string, topic string) error {
	return t.runtime.Subscribe(ctx, agentID, topic)
}

// Unsubscribe 取消订阅，委托运行时。
//
// 对应 Python: BaseTeam.unsubscribe(agent_id, topic)
func (t *HierarchicalToolsTeam) Unsubscribe(ctx context.Context, agentID string, topic string) error {
	return t.runtime.Unsubscribe(ctx, agentID, topic)
}

// Configure 配置团队。
//
// 对应 Python: BaseTeam.configure(config) -> self
func (t *HierarchicalToolsTeam) Configure(_ context.Context, config maschema.TeamConfig) error {
	t.config.TeamConfig = config
	logger.Info(toolsLogComponent).
		Str("action", "configure").
		Str("team_id", t.card.GetID()).
		Msg("HierarchicalToolsTeam 配置已更新")
	return nil
}

// GetAgentCard 获取 Agent 卡片，委托运行时。
//
// 对应 Python: BaseTeam.get_agent_card(agent_id)
func (t *HierarchicalToolsTeam) GetAgentCard(agentID string) (*agentschema.AgentCard, error) {
	return t.runtime.GetAgentCard(agentID)
}

// GetAgentCount 获取 Agent 数量，委托运行时。
//
// 对应 Python: BaseTeam.get_agent_count()
func (t *HierarchicalToolsTeam) GetAgentCount() int {
	return t.runtime.GetAgentCount()
}

// ListAgents 列出所有 Agent ID，委托运行时。
//
// 对应 Python: BaseTeam.list_agents()
func (t *HierarchicalToolsTeam) ListAgents() []string {
	return t.runtime.ListAgents()
}

// Card 返回团队身份卡片。
//
// 对应 Python: BaseTeam.card 属性
func (t *HierarchicalToolsTeam) Card() maschema.TeamCardInterface {
	return t.card
}

// Config 返回团队配置。
//
// 对应 Python: BaseTeam.config 属性
func (t *HierarchicalToolsTeam) Config() *maschema.TeamConfig {
	return &t.config.TeamConfig
}

// GetRuntime 返回团队运行时。
func (t *HierarchicalToolsTeam) GetRuntime() *team_runtime.TeamRuntime {
	return t.runtime
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// assertReady 校验团队就绪状态。
//
// 校验 rootAgentID 非空且 runtime.HasAgent(rootAgentID)。
//
// 对应 Python: HierarchicalTeam._assert_ready()
func (t *HierarchicalToolsTeam) assertReady() error {
	if t.rootAgentID == "" {
		return exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", "HierarchicalToolsTeamConfig 未配置 RootAgent"),
		)
	}
	if !t.runtime.HasAgent(t.rootAgentID) {
		return exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf(
				"RootAgent '%s' 未注册到运行时，请先调用 AddAgent(rootCard, rootProvider)",
				t.rootAgentID,
			)),
		)
	}
	return nil
}

// setupHierarchy 延迟注册子 Agent 到父 Agent 的 AbilityManager。
//
// 遍历 pendingChildren，从 ResourceMgr 获取父 Agent 实例，
// 对每个子 AgentCard 调用 parentAgent.AbilityManager().Add(childCard)。
// 执行后清空 pendingChildren，对齐 Python: self._pending_children.clear()。
//
// 对应 Python: HierarchicalTeam._setup_hierarchy()
func (t *HierarchicalToolsTeam) setupHierarchy(ctx context.Context) error {
	if len(t.pendingChildren) == 0 {
		return nil
	}

	resourceMgr := runner.GetResourceMgr()
	if resourceMgr == nil {
		// 对齐 Python: Runner.resource_mgr 为 None 时抛异常，非静默跳过
		return exception.BuildError(exception.StatusAgentTeamAgentNotFound,
			exception.WithParam("error_msg", "ResourceMgr 未初始化，无法建立层级关系"),
		)
	}

	for parentID, childCards := range t.pendingChildren {
		// 获取父 Agent 实例
		parentAgents, err := resourceMgr.GetAgent(ctx, []string{parentID})
		if err != nil || len(parentAgents) == 0 {
			logger.Error(toolsLogComponent).Err(err).
				Str("event_type", "LLM_CALL_ERROR").
				Str("method", "HierarchicalToolsTeam.setupHierarchy").
				Str("parent_id", parentID).
				Msg("获取父 Agent 实例失败")
			return exception.BuildError(exception.StatusAgentTeamAgentNotFound,
				exception.WithParam("error_msg", fmt.Sprintf("父 Agent '%s' 实例未找到", parentID)),
			)
		}

		parentAgent := parentAgents[0]
		am := parentAgent.AbilityManager()
		if am == nil {
			logger.Warn(toolsLogComponent).
				Str("action", "setup_hierarchy").
				Str("parent_id", parentID).
				Msg("父 Agent 无 AbilityManager，跳过子 Agent 注册")
			continue
		}

		for _, childCard := range childCards {
			am.Add(childCard)
			logger.Debug(toolsLogComponent).
				Str("action", "setup_hierarchy_register").
				Str("child_id", childCard.ID).
				Str("parent_id", parentID).
				Msg("子 Agent 已注册到父 Agent 的 ability_manager")
		}
	}

	// 对齐 Python: self._pending_children.clear()
	t.pendingChildren = make(map[string][]*agentschema.AgentCard)
	return nil
}
