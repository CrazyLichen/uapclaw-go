package resources_manager

import (
	"fmt"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentTeamMgr Agent 团队资源管理器。
//
// 对应 Python: AgentTeamMgr (openjiuwen/core/runner/resources_manager/agent_team_manager.py)
//
// ⤵️ 预留：等 multi_agent 领域实现 TeamCard/BaseTeam 后回填。
// 当前仅定义结构体和方法签名，核心逻辑标记 ⤵️。
type AgentTeamMgr struct {
	// ⤵️ 预留：等 multi_agent 领域实现 TeamCard/BaseTeam 后回填
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
// ⤵️ 预留：等 TeamCard/BaseTeam 类型实现后回填
func (m *AgentTeamMgr) AddAgentTeam(agentTeamID string, provider any) error {
	// ⤵️ 预留：核心逻辑待 TeamCard/BaseTeam 类型实现后回填
	return fmt.Errorf("agent team manager not implemented, agent_team_id=%s", agentTeamID)
}

// RemoveAgentTeam 注销 Agent 团队。
//
// ⤵️ 预留：等 TeamCard/BaseTeam 类型实现后回填
func (m *AgentTeamMgr) RemoveAgentTeam(agentTeamID string) (any, error) {
	// ⤵️ 预留：核心逻辑待 TeamCard/BaseTeam 类型实现后回填
	return nil, fmt.Errorf("agent team manager not implemented, agent_team_id=%s", agentTeamID)
}

// GetAgentTeam 获取 Agent 团队。
//
// ⤵️ 预留：等 TeamCard/BaseTeam 类型实现后回填
func (m *AgentTeamMgr) GetAgentTeam(agentTeamID string) (any, error) {
	// ⤵️ 预留：核心逻辑待 TeamCard/BaseTeam 类型实现后回填
	return nil, fmt.Errorf("agent team manager not implemented, agent_team_id=%s", agentTeamID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
