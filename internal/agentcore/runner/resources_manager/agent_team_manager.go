package resources_manager

import (
	"fmt"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentTeamMgr Agent 团队资源管理器。
//
// 对应 Python: AgentTeamMgr (openjiuwen/core/runner/resources_manager/agent_team_manager.py)
// Python 继承 AbstractManager[BaseTeam]，三个方法直接委托给父类。
//
// ⤵️ 预留：8.27 BaseTeam 接口 + 8.28 TeamCard 实现后回填。
// 回填内容：将 map[string]any 替换为 AbstractManager[BaseTeam]（Provider 模式），
// 三个方法分别委托给 registerProvider/unregisterProvider/getResource。
type AgentTeamMgr struct {
	// agents 临时存储，⤴️ 8.27 后替换为 AbstractManager[BaseTeam]
	agents map[string]any
	// mu 读写锁
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentTeamMgr 创建 Agent 团队资源管理器。
func NewAgentTeamMgr() *AgentTeamMgr {
	return &AgentTeamMgr{
		agents: make(map[string]any),
	}
}

// AddAgentTeam 注册 Agent 团队。
//
// ⤵️ 预留：8.27/8.28 实现后回填。回填内容：委托给 AbstractManager.registerProvider。
// 对应 Python: AgentTeamMgr.add_agent_team(agent_team_id, agent_team) → self._register_resource_provider(...)
func (m *AgentTeamMgr) AddAgentTeam(agentTeamID string, provider any) error {
	// ⤵️ 预留：8.27/8.28 实现后回填
	return fmt.Errorf("agent team manager not implemented, agent_team_id=%s", agentTeamID)
}

// RemoveAgentTeam 注销 Agent 团队。
//
// ⤵️ 预留：8.27/8.28 实现后回填。回填内容：委托给 AbstractManager.unregisterProvider。
// 对应 Python: AgentTeamMgr.remove_agent_team(agent_team_id) → self._unregister_resource_provider(...)
func (m *AgentTeamMgr) RemoveAgentTeam(agentTeamID string) (any, error) {
	// ⤵️ 预留：8.27/8.28 实现后回填
	return nil, fmt.Errorf("agent team manager not implemented, agent_team_id=%s", agentTeamID)
}

// GetAgentTeam 获取 Agent 团队。
//
// ⤵️ 预留：8.27/8.28 实现后回填。回填内容：委托给 AbstractManager.getResource。
// 对应 Python: AgentTeamMgr.get_agent_team(agent_team_id) → await self._get_resource(...)
func (m *AgentTeamMgr) GetAgentTeam(agentTeamID string) (any, error) {
	// ⤵️ 预留：8.27/8.28 实现后回填
	return nil, fmt.Errorf("agent team manager not implemented, agent_team_id=%s", agentTeamID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
