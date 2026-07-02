package handoff

import (
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HandoffRoute 交接路由规则，定义 Agent 间的定向交接路径。
//
// 对应 Python: HandoffRoute(source=str, target=str)
// Python 中 HandoffRoute 是 frozen dataclass，Go 中为值结构体。
type HandoffRoute struct {
	// Source 源 Agent ID
	Source string
	// Target 目标 Agent ID
	Target string
}

// HandoffConfig 交接编排配置，控制交接行为参数。
//
// 对应 Python: HandoffConfig(start_agent=None, max_handoffs=10, routes=[], termination_condition=None)
type HandoffConfig struct {
	// StartAgent 起始 Agent，nil 时取第一个
	StartAgent *agentschema.AgentCard
	// MaxHandoffs 最大交接次数，默认 10
	MaxHandoffs int
	// Routes 路由规则，空时全互联
	Routes []HandoffRoute
	// TerminationCondition 可选终止条件
	TerminationCondition func(*HandoffOrchestrator) bool
}

// HandoffTeamConfig HandoffTeam 完整配置，嵌入 TeamConfig 并增加交接编排配置。
//
// 对应 Python: HandoffTeamConfig(handoff=HandoffConfig(), ...)
// Python 继承 TeamConfig，Go 中嵌入 TeamConfig 实现等效组合。
type HandoffTeamConfig struct {
	maschema.TeamConfig
	// Handoff 交接编排配置
	Handoff HandoffConfig
}

// HandoffOrchestrator 交接编排器，被 TerminationCondition 回调引用。
//
// 当前为前向声明，具体字段和方法在后续任务中填充。
type HandoffOrchestrator struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHandoffConfig 创建 HandoffConfig 实例，设置默认值。
//
// 对应 Python: HandoffConfig() → start_agent=None, max_handoffs=10, routes=[], termination_condition=None
func NewHandoffConfig() *HandoffConfig {
	return &HandoffConfig{
		MaxHandoffs: 10,
	}
}

// NewHandoffTeamConfig 创建 HandoffTeamConfig 实例，TeamConfig 和 Handoff 均使用默认值。
//
// 对应 Python: HandoffTeamConfig() → TeamConfig 默认值 + HandoffConfig 默认值
func NewHandoffTeamConfig() *HandoffTeamConfig {
	return &HandoffTeamConfig{
		TeamConfig: *maschema.NewTeamConfig(),
		Handoff:    *NewHandoffConfig(),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
