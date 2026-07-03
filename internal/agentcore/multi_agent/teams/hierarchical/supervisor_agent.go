package hierarchical

import (
	"context"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SupervisorAgent 默认内置监督者 Agent，组合 CommunicableAgent + ReActAgent。
//
// 通过 CommunicableAgent 获得 P2P/Pub-Sub 通信能力，
// 通过 ReActAgent 获得 ReAct 循环执行能力，
// 内部使用 P2PAbilityManager 将 AgentCard 类型的 tool_call 转为 P2P 消息派发。
//
// 对应 Python: SupervisorAgent(CommunicableAgent, ReActAgent)
type SupervisorAgent struct {
	// CommunicableAgent 嵌入：Send/Publish/Subscribe/Unsubscribe/Runtime/BindRuntime
	team_runtime.CommunicableAgent
	// ReActAgent 嵌入：Invoke/Stream/Card/Configure/AgentID/...
	agents.ReActAgent
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMaxParallelSubAgents 默认最大并行子 Agent 派发数
	defaultMaxParallelSubAgents = 10
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 SupervisorAgent 满足 BaseAgent 接口
var _ agentinterfaces.BaseAgent = (*SupervisorAgent)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSupervisorAgent 创建 SupervisorAgent 实例。
//
// 对应 Python: SupervisorAgent(card, config, max_parallel_sub_agents)
func NewSupervisorAgent(
	card *agentschema.AgentCard,
	config *saconfig.ReActAgentConfig,
	maxParallelSubAgents int,
) *SupervisorAgent {
	react := agents.NewReActAgent(card, config)

	supervisor := &SupervisorAgent{
		CommunicableAgent: *team_runtime.NewCommunicableAgent(),
		ReActAgent:        *react,
	}

	if maxParallelSubAgents < 1 {
		maxParallelSubAgents = defaultMaxParallelSubAgents
	}

	// 从 runtime 获取 timeout，若未绑定则使用默认值
	p2pAm := NewP2PAbilityManager(supervisor, maxParallelSubAgents, defaultP2PTimeout)
	react.SetAbilityManager(p2pAm)

	return supervisor
}

// Create 创建预加载子 Agent 卡片的 SupervisorAgent。
// 返回 (AgentCard, AgentProvider) 元组，兼容 HierarchicalTeam.AddAgent()。
//
// 对应 Python: SupervisorAgent.create()
func Create(
	agentsList []*agentschema.AgentCard,
	modelClientConfig *llmschema.ModelClientConfig,
	modelRequestConfig *llmschema.ModelRequestConfig,
	agentCard *agentschema.AgentCard,
	systemPrompt string,
	maxIterations int,
	maxParallelSubAgents int,
) (*agentschema.AgentCard, resources_manager.AgentProvider) {
	if len(agentsList) == 0 {
		panic(exception.BuildError(exception.StatusAgentTeamCreateRuntimeError,
			exception.WithParam("error_msg", "[SupervisorAgent.create] agents 列表不能为空"),
		))
	}

	for _, card := range agentsList {
		if card == nil {
			panic(exception.BuildError(exception.StatusAgentTeamCreateRuntimeError,
				exception.WithParam("error_msg", "[SupervisorAgent.create] agents 中每项必须为 AgentCard"),
			))
		}
	}

	if maxIterations < 1 {
		maxIterations = 5
	}
	if maxParallelSubAgents < 1 {
		maxParallelSubAgents = defaultMaxParallelSubAgents
	}

	// 懒构造闭包
	provider := func(ctx context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		cfg := saconfig.NewReActAgentConfig()
		if modelClientConfig != nil {
			cfg.ModelClientConfig = modelClientConfig
			cfg.ModelProvider = fmt.Sprintf("%v", modelClientConfig.ClientProvider)
			cfg.APIKey = modelClientConfig.APIKey
			cfg.APIBase = modelClientConfig.APIBase
		}
		if modelRequestConfig != nil {
			cfg.ModelRequestConfig = modelRequestConfig
			if modelRequestConfig.ModelName != "" {
				cfg.ModelNameVal = modelRequestConfig.ModelName
			}
		}
		cfg.MaxIterations = maxIterations
		if systemPrompt != "" {
			cfg.PromptTemplate = []map[string]any{
				{"role": "system", "content": systemPrompt},
			}
		}

		supervisor := NewSupervisorAgent(agentCard, cfg, maxParallelSubAgents)

		for _, card := range agentsList {
			supervisor.RegisterSubAgentCard(card)
			logger.Debug(p2pLogComponent).
				Str("action", "supervisor_create").
				Str("sub_agent_id", card.ID).
				Msg("注册子 Agent 卡片")
		}

		logger.Info(p2pLogComponent).
			Str("action", "supervisor_create").
			Str("supervisor_id", agentCard.ID).
			Int("sub_agent_count", len(agentsList)).
			Int("max_parallel", maxParallelSubAgents).
			Msg("SupervisorAgent 创建完成")

		return supervisor, nil
	}

	return agentCard, provider
}

// RegisterSubAgentCard 将子 Agent 卡片注册到 P2PAbilityManager。
// 使 LLM 可将子 Agent 视为可调用的工具。
//
// 对应 Python: SupervisorAgent.register_sub_agent_card(card)
func (s *SupervisorAgent) RegisterSubAgentCard(card *agentschema.AgentCard) {
	am := s.ReActAgent.AbilityManager()
	if am != nil {
		am.Add(card)
	}
	logger.Debug(p2pLogComponent).
		Str("action", "register_sub_agent_card").
		Str("card_name", card.Name).
		Str("card_id", card.ID).
		Msg("注册子 Agent 卡片为 LLM 工具")
}
