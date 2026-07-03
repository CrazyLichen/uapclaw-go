package hierarchical_tools

import (
	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HierarchicalToolsTeamConfig 工具委托层级团队配置。
//
// 对应 Python: HierarchicalTeamConfig (hierarchical_tools/hierarchical_config.py)
type HierarchicalToolsTeamConfig struct {
	// TeamConfig 嵌入基础团队配置
	TeamConfig maschema.TeamConfig
	// RootAgent 根/入口 Agent 卡片（必填）
	RootAgent *agentschema.AgentCard
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHierarchicalToolsTeamConfig 创建默认 HierarchicalToolsTeamConfig。
func NewHierarchicalToolsTeamConfig() *HierarchicalToolsTeamConfig {
	return &HierarchicalToolsTeamConfig{}
}
