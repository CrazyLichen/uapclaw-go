package multi_agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseTeam 多 Agent 团队核心行为契约，类型别名指向 schema 包。
//
// 对应 Python: openjiuwen/core/multi_agent/team.py (BaseTeam)
type BaseTeam = schema.BaseTeam

// ──────────────────────────── 枚举 ────────────────────────────

// AgentTeamProvider 团队资源提供者函数，类型别名指向 schema 包。
//
// 对应 Python: AgentTeamProvider = Callable[[TeamCard], Awaitable[BaseTeam]] | Callable[[TeamCard], BaseTeam]
type AgentTeamProvider = schema.AgentTeamProvider

// TeamAgentProvider 团队内 Agent 资源提供者函数，类型别名指向 schema 包。
//
// 对应 Python: AgentProvider = Callable[[AgentCard], Awaitable[BaseAgent]] | Callable[[AgentCard], BaseAgent]
type TeamAgentProvider = schema.TeamAgentProvider

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
