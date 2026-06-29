package resources_manager

import (
	"context"

	multiagent "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentTeamMgr Agent 团队资源管理器，嵌入 AbstractManager 复用 provider 注册/获取/注销能力。
//
// 对应 Python: AgentTeamMgr (openjiuwen/core/runner/resources_manager/agent_team_manager.py)
// Python 继承 AbstractManager[BaseTeam]，三个方法直接委托给父类。
type AgentTeamMgr struct {
	AbstractManager[multiagent.BaseTeam]
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentTeamMgr 创建 Agent 团队资源管理器。
func NewAgentTeamMgr() *AgentTeamMgr {
	return &AgentTeamMgr{
		AbstractManager: NewAbstractManager[multiagent.BaseTeam](),
	}
}

// AddAgentTeam 注册 Agent 团队提供者。
//
// 对应 Python: AgentTeamMgr.add_agent_team(agent_team_id, agent_team) → self._register_resource_provider(...)
func (m *AgentTeamMgr) AddAgentTeam(agentTeamID string, provider multiagent.AgentTeamProvider) error {
	if agentTeamID == "" {
		return exception.BuildError(exception.StatusResourceIDValueInvalid,
			exception.WithParam("resource_type", "team"),
			exception.WithParam("reason", "agent team id is empty"),
		)
	}
	if provider == nil {
		return exception.BuildError(exception.StatusResourceProviderInvalid,
			exception.WithParam("resource_type", "team"),
			exception.WithParam("reason", "agent team provider is nil"),
		)
	}

	// 将 AgentTeamProvider 包装为 AbstractManager 所需的 func(context.Context) (T, error) 签名
	wrappedProvider := func(ctx context.Context) (multiagent.BaseTeam, error) {
		// AgentTeamProvider 签名为 func(ctx, card) → (BaseTeam, error)
		// 此处 card 为 nil，由 provider 内部自行处理
		return provider(ctx, nil)
	}

	err := m.registerProvider(agentTeamID, wrappedProvider)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "AGENT_TEAM_ADD_ERROR").
			Str("agent_team_id", agentTeamID).
			Err(err).
			Msg("添加 Agent 团队失败")
		return exception.BuildError(exception.StatusResourceAddError,
			exception.WithParam("card", agentTeamID),
			exception.WithParam("reason", err.Error()),
		)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "AGENT_TEAM_ADD_SUCCESS").
		Str("agent_team_id", agentTeamID).
		Msg("添加 Agent 团队成功")
	return nil
}

// RemoveAgentTeam 注销 Agent 团队提供者，返回被注销的 provider。
//
// 对应 Python: AgentTeamMgr.remove_agent_team(agent_team_id) → self._unregister_resource_provider(...)
func (m *AgentTeamMgr) RemoveAgentTeam(agentTeamID string) (multiagent.AgentTeamProvider, error) {
	unwrapped, err := m.unregisterProvider(agentTeamID)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "AGENT_TEAM_REMOVE_ERROR").
			Str("agent_team_id", agentTeamID).
			Err(err).
			Msg("移除 Agent 团队失败")
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_id", agentTeamID),
			exception.WithParam("resource_type", "team"),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 将 wrapped provider 还原为 AgentTeamProvider
	provider := func(ctx context.Context, card *multiagent.TeamCard) (multiagent.BaseTeam, error) {
		return unwrapped(ctx)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "AGENT_TEAM_REMOVE_SUCCESS").
		Str("agent_team_id", agentTeamID).
		Msg("移除 Agent 团队成功")
	return provider, nil
}

// GetAgentTeam 获取 Agent 团队实例。
//
// 对应 Python: AgentTeamMgr.get_agent_team(agent_team_id) → await self._get_resource(...)
func (m *AgentTeamMgr) GetAgentTeam(ctx context.Context, agentTeamID string) (multiagent.BaseTeam, error) {
	team, err := m.getResource(ctx, agentTeamID)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "AGENT_TEAM_GET_ERROR").
			Str("agent_team_id", agentTeamID).
			Err(err).
			Msg("获取 Agent 团队失败")
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_id", agentTeamID),
			exception.WithParam("resource_type", "team"),
			exception.WithParam("reason", err.Error()),
		)
	}
	return team, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
