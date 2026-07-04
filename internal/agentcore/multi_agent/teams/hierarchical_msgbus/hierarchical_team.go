package hierarchical_msgbus

import (
	"context"
	"fmt"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/teams"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HierarchicalTeam 消息总线驱动的层级多 Agent 团队。
//
// 通过 SupervisorAgent 驱动 LLM 决策，自动将子 Agent 任务派发给团队中的其他 Agent。
// 适用于"一个智能调度者 + 多个专业执行者"的场景。
//
// 对应 Python: HierarchicalTeam (hierarchical_msgbus/hierarchical_team.py)
type HierarchicalTeam struct {
	// card 团队身份卡片
	card maschema.TeamCardInterface
	// config 完整配置
	config HierarchicalTeamConfig
	// runtime 团队运行时
	runtime *team_runtime.TeamRuntime
	// supervisorID 监督者 Agent ID
	supervisorID string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamLogComponent 日志组件标识
	teamLogComponent = logger.ComponentChannel
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 HierarchicalTeam 满足 BaseTeam 接口
var _ maschema.BaseTeam = (*HierarchicalTeam)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHierarchicalTeam 创建 HierarchicalTeam 实例。
//
// 参数：
//   - card：团队身份卡片
//   - config：完整配置
//   - runtime：团队运行时，nil 时自动创建
//
// 对应 Python: HierarchicalTeam(card, config)
func NewHierarchicalTeam(card maschema.TeamCardInterface, config *HierarchicalTeamConfig, runtime *team_runtime.TeamRuntime) *HierarchicalTeam {
	if config == nil {
		defaultCfg := NewHierarchicalTeamConfig()
		config = defaultCfg
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

	var supervisorID string
	if config.SupervisorAgent != nil {
		supervisorID = config.SupervisorAgent.ID
	}

	team := &HierarchicalTeam{
		card:         card,
		config:       *config,
		runtime:      tr,
		supervisorID: supervisorID,
	}

	logger.Info(teamLogComponent).
		Str("action", "new_hierarchical_team").
		Str("team_id", teamID).
		Str("supervisor_id", supervisorID).
		Msg("创建 HierarchicalTeam")

	return team
}

// Invoke 非流式调用团队，通过 supervisor 运行并返回最终结果。
//
// 对应 Python: HierarchicalTeam.invoke(message, session, timeout)
func (t *HierarchicalTeam) Invoke(ctx context.Context, inputs map[string]any, opts ...maschema.TeamOption) (any, error) {
	if err := t.assertReady(); err != nil {
		return nil, err
	}

	teamOpts := maschema.NewTeamOptions(opts...)
	sess := teamOpts.Session
	timeout := teamOpts.Timeout
	if timeout == 0 {
		timeout = t.config.Timeout
	}

	result, err := teams.StandaloneInvokeContext(ctx, t.runtime, t.card, inputs, sess,
		func(teamSession *session.AgentTeamSession, sessionID string) (map[string]any, error) {
			logger.Debug(teamLogComponent).
				Str("action", "hierarchical_invoke").
				Str("session_id", sessionID).
				Str("supervisor_id", t.supervisorID).
				Msg("开始 invoke")

			res, sendErr := t.runtime.Send(ctx, inputs, t.supervisorID, t.card.GetID(),
				maschema.WithTeamSessionID(sessionID),
				maschema.WithTeamTimeout(timeout),
			)
			if sendErr != nil {
				logger.Error(teamLogComponent).Err(sendErr).
					Str("event_type", "LLM_CALL_ERROR").
					Str("method", "HierarchicalTeam.Invoke").
					Str("supervisor_id", t.supervisorID).
					Msg("Send 到 supervisor 失败")
				return nil, sendErr
			}

			logger.Debug(teamLogComponent).
				Str("action", "hierarchical_invoke_end").
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

// Stream 流式调用团队，运行 supervisor 并流式输出结果。
//
// 对应 Python: HierarchicalTeam.stream(message, session, timeout)
func (t *HierarchicalTeam) Stream(ctx context.Context, inputs map[string]any, opts ...maschema.TeamOption) (<-chan stream.Schema, error) {
	if err := t.assertReady(); err != nil {
		return nil, err
	}

	teamOpts := maschema.NewTeamOptions(opts...)
	sess := teamOpts.Session
	timeout := teamOpts.Timeout
	if timeout == 0 {
		timeout = t.config.Timeout
	}

	logger.Debug(teamLogComponent).
		Str("action", "hierarchical_stream").
		Str("supervisor_id", t.supervisorID).
		Msg("开始 stream")

	return teams.StandaloneStreamContext(ctx, t.runtime, t.card, inputs, sess,
		func(teamSession *session.AgentTeamSession, sessionID string) error {
			result, sendErr := t.runtime.Send(ctx, inputs, t.supervisorID, t.card.GetID(),
				maschema.WithTeamSessionID(sessionID),
				maschema.WithTeamTimeout(timeout),
			)
			if sendErr != nil {
				logger.Error(teamLogComponent).Err(sendErr).
					Str("event_type", "LLM_CALL_ERROR").
					Str("method", "HierarchicalTeam.Stream").
					Str("supervisor_id", t.supervisorID).
					Msg("Send 到 supervisor 失败")
				return sendErr
			}

			if result != nil {
				resultMap, ok := result.(map[string]any)
				if !ok {
					resultMap = map[string]any{"output": result}
				}
				if writeErr := teamSession.WriteStream(ctx, resultMap); writeErr != nil {
					logger.Warn(teamLogComponent).Err(writeErr).
						Str("action", "hierarchical_stream_write").
						Msg("写入流失败")
				}
			}

			logger.Debug(teamLogComponent).
				Str("action", "hierarchical_stream_end").
				Str("session_id", sessionID).
				Msg("stream 结束")
			return nil
		},
	)
}

// AddAgent 向团队注册 Agent。
//
// 如果 Agent 已存在则跳过，否则注册到运行时。
// 如果 card.ID == supervisorID，设置 P2P timeout。
//
// 对应 Python: HierarchicalTeam.add_agent(card, provider)
func (t *HierarchicalTeam) AddAgent(ctx context.Context, card *agentschema.AgentCard, provider maschema.TeamAgentProvider, _ ...maschema.TeamOption) error {
	if t.runtime.HasAgent(card.ID) {
		logger.Warn(teamLogComponent).
			Str("action", "add_agent_skip").
			Str("agent_id", card.ID).
			Str("team_id", t.card.GetID()).
			Msg("Agent 已存在，跳过注册")
		return nil
	}

	// 注册到运行时（包装为 resources_manager.AgentProvider）
	wrappedProvider := resources_manager.AgentProvider(provider)
	if err := t.runtime.RegisterAgent(ctx, card, wrappedProvider); err != nil {
		logger.Error(teamLogComponent).Err(err).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HierarchicalTeam.AddAgent").
			Str("agent_id", card.ID).
			Msg("注册 Agent 到运行时失败")
		return err
	}

	// 识别 supervisor，设置 P2P timeout
	if card.ID == t.supervisorID {
		logger.Info(teamLogComponent).
			Str("action", "add_agent_supervisor").
			Str("supervisor_id", card.ID).
			Str("team_id", t.card.GetID()).
			Msg("注册 supervisor 到 HierarchicalTeam")
		t.runtime.SetP2PTimeout(t.config.Timeout)
	}

	logger.Info(teamLogComponent).
		Str("action", "add_agent").
		Str("agent_id", card.ID).
		Str("team_id", t.card.GetID()).
		Msg("Agent 已注册到 HierarchicalTeam")

	return nil
}

// RemoveAgent 从团队注销 Agent。
//
// 对应 Python: BaseTeam.remove_agent(agent)
func (t *HierarchicalTeam) RemoveAgent(ctx context.Context, agentID string) error {
	_, err := t.runtime.UnregisterAgent(ctx, agentID)
	if err != nil {
		logger.Error(teamLogComponent).Err(err).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HierarchicalTeam.RemoveAgent").
			Str("agent_id", agentID).
			Msg("注销 Agent 失败")
		return err
	}

	logger.Info(teamLogComponent).
		Str("action", "remove_agent").
		Str("agent_id", agentID).
		Str("team_id", t.card.GetID()).
		Msg("Agent 已从 HierarchicalTeam 注销")

	return nil
}

// Send P2P 发送消息，委托运行时。
//
// 对应 Python: BaseTeam.send(message, recipient, sender, session_id, timeout)
func (t *HierarchicalTeam) Send(ctx context.Context, message map[string]any, recipient string, sender string, opts ...maschema.TeamOption) (any, error) {
	return t.runtime.Send(ctx, message, recipient, sender, opts...)
}

// Publish Pub-Sub 发布消息，委托运行时。
//
// 对应 Python: BaseTeam.publish(message, topic_id, sender, session_id)
func (t *HierarchicalTeam) Publish(ctx context.Context, message map[string]any, topicID string, sender string, opts ...maschema.TeamOption) error {
	return t.runtime.Publish(ctx, message, topicID, sender, opts...)
}

// Subscribe 订阅主题，委托运行时。
//
// 对应 Python: BaseTeam.subscribe(agent_id, topic)
func (t *HierarchicalTeam) Subscribe(ctx context.Context, agentID string, topic string) error {
	return t.runtime.Subscribe(ctx, agentID, topic)
}

// Unsubscribe 取消订阅，委托运行时。
//
// 对应 Python: BaseTeam.unsubscribe(agent_id, topic)
func (t *HierarchicalTeam) Unsubscribe(ctx context.Context, agentID string, topic string) error {
	return t.runtime.Unsubscribe(ctx, agentID, topic)
}

// Configure 配置团队。
//
// 对应 Python: BaseTeam.configure(config) -> self
func (t *HierarchicalTeam) Configure(_ context.Context, config maschema.TeamConfig) error {
	t.config.TeamConfig = config
	logger.Info(teamLogComponent).
		Str("action", "configure").
		Str("team_id", t.card.GetID()).
		Msg("HierarchicalTeam 配置已更新")
	return nil
}

// GetAgentCard 获取 Agent 卡片，委托运行时。
//
// 对应 Python: BaseTeam.get_agent_card(agent_id)
func (t *HierarchicalTeam) GetAgentCard(agentID string) (*agentschema.AgentCard, error) {
	return t.runtime.GetAgentCard(agentID)
}

// GetAgentCount 获取 Agent 数量，委托运行时。
//
// 对应 Python: BaseTeam.get_agent_count()
func (t *HierarchicalTeam) GetAgentCount() int {
	return t.runtime.GetAgentCount()
}

// ListAgents 列出所有 Agent ID，委托运行时。
//
// 对应 Python: BaseTeam.list_agents()
func (t *HierarchicalTeam) ListAgents() []string {
	return t.runtime.ListAgents()
}

// Card 返回团队身份卡片。
//
// 对应 Python: BaseTeam.card 属性
func (t *HierarchicalTeam) Card() maschema.TeamCardInterface {
	return t.card
}

// Config 返回团队配置。
//
// 对应 Python: BaseTeam.config 属性
func (t *HierarchicalTeam) Config() *maschema.TeamConfig {
	return &t.config.TeamConfig
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// assertReady 校验团队就绪状态。
//
// 校验 supervisorID 非空且 runtime.HasAgent(supervisorID)。
//
// 对应 Python: HierarchicalTeam._assert_ready()
func (t *HierarchicalTeam) assertReady() error {
	if t.supervisorID == "" {
		return exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", "HierarchicalTeamConfig 未配置 SupervisorAgent"),
		)
	}
	if !t.runtime.HasAgent(t.supervisorID) {
		return exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf(
				"Supervisor '%s' 未注册到运行时，请先调用 AddAgent(supervisorCard, supervisorProvider)",
				t.supervisorID,
			)),
		)
	}
	return nil
}
