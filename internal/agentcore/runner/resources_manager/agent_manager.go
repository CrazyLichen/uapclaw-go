package resources_manager

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentMgr Agent 资源管理器，嵌入 AbstractManager 复用 provider 注册/获取/注销能力。
//
// 对应 Python: AgentMgr (openjiuwen/core/runner/resources_manager/agent_manager.py)
type AgentMgr struct {
	AbstractManager[interfaces.BaseAgent]
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentMgr 创建 Agent 资源管理器。
func NewAgentMgr() AgentMgr {
	return AgentMgr{
		AbstractManager: NewAbstractManager[interfaces.BaseAgent](),
	}
}

// AddAgent 注册 Agent 提供者。
// 调用 AbstractManager.RegisterProvider 将 provider 包装后存入注册表。
//
// 对应 Python: AgentMgr.add_agent(agent_id, provider)
//
// ⤵️ 预留：分布式场景下 AgentAdapter/RemoteAgent/_is_remote_agent/interface_url 的判断逻辑
func (m *AgentMgr) AddAgent(agentID string, provider AgentProvider) error {
	if agentID == "" {
		return exception.BuildError(exception.StatusResourceIDValueInvalid,
			exception.WithParam("resource_type", "agent"),
			exception.WithParam("reason", "agent id is empty"),
		)
	}
	if provider == nil {
		return exception.BuildError(exception.StatusResourceProviderInvalid,
			exception.WithParam("resource_type", "agent"),
			exception.WithParam("reason", "agent provider is nil"),
		)
	}

	// 将 AgentProvider 包装为 AbstractManager 所需的 func(context.Context) (T, error) 签名
	wrappedProvider := func(ctx context.Context) (interfaces.BaseAgent, error) {
		// AgentProvider 签名为 func(ctx, card) → (BaseAgent, error)
		// 此处 card 为 nil，由 provider 内部自行处理
		// ⤵️ 预留：分布式场景下从 AgentAdapter 获取 card
		return provider(ctx, nil)
	}

	err := m.registerProvider(agentID, wrappedProvider)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "AGENT_ADD_ERROR").
			Str("agent_id", agentID).
			Err(err).
			Msg("添加 Agent 失败")
		return exception.BuildError(exception.StatusResourceAddError,
			exception.WithParam("card", agentID),
			exception.WithParam("reason", err.Error()),
		)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "AGENT_ADD_SUCCESS").
		Str("agent_id", agentID).
		Msg("添加 Agent 成功")
	return nil
}

// RemoveAgent 注销 Agent 提供者，返回被注销的 provider。
//
// 对应 Python: AgentMgr.remove_agent(agent_id)
//
// ⤵️ 预留：分布式场景下 RemoteAgent 的清理逻辑
func (m *AgentMgr) RemoveAgent(agentID string) (AgentProvider, error) {
	unwrapped, err := m.unregisterProvider(agentID)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "AGENT_REMOVE_ERROR").
			Str("agent_id", agentID).
			Err(err).
			Msg("移除 Agent 失败")
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_id", agentID),
			exception.WithParam("resource_type", "agent"),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 将 wrapped provider 还原为 AgentProvider
	provider := func(ctx context.Context, card *agentschema.AgentCard) (interfaces.BaseAgent, error) {
		return unwrapped(ctx)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "AGENT_REMOVE_SUCCESS").
		Str("agent_id", agentID).
		Msg("移除 Agent 成功")
	return provider, nil
}

// GetAgent 获取 Agent 实例。
// 调用 AbstractManager.GetResource 获取 provider 并执行。
//
// 对应 Python: AgentMgr.get_agent(agent_id)
//
// ⤵️ 预留：分布式场景下 _is_remote_agent 判断和 RemoteAgent 调用逻辑
func (m *AgentMgr) GetAgent(ctx context.Context, agentID string) (interfaces.BaseAgent, error) {
	agent, err := m.getResource(ctx, agentID)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "AGENT_GET_ERROR").
			Str("agent_id", agentID).
			Err(err).
			Msg("获取 Agent 失败")
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_id", agentID),
			exception.WithParam("resource_type", "agent"),
			exception.WithParam("reason", err.Error()),
		)
	}
	return agent, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
