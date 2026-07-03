package hierarchical

import (
	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HierarchicalTeamConfig 层级团队（消息总线模式）配置。
//
// 对应 Python: HierarchicalTeamConfig (hierarchical_msgbus/hierarchical_config.py)
type HierarchicalTeamConfig struct {
	// TeamConfig 嵌入基础团队配置
	TeamConfig maschema.TeamConfig
	// SupervisorAgent 监督者 Agent 卡片（必填）
	SupervisorAgent *agentschema.AgentCard
	// Timeout P2P 通信超时秒数，默认 1800.0
	Timeout float64
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultP2PTimeout 默认 P2P 通信超时秒数
	defaultP2PTimeout = 1800.0
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHierarchicalTeamConfig 创建默认 HierarchicalTeamConfig。
func NewHierarchicalTeamConfig() *HierarchicalTeamConfig {
	return &HierarchicalTeamConfig{
		Timeout: defaultP2PTimeout,
	}
}
