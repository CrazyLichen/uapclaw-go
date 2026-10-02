package config

import (
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentMemoryConfig Agent 记忆配置。
//
// 定义 Agent 级别的记忆功能开关和变量配置。
//
// Python: openjiuwen/core/memory/config/config.py (AgentMemoryConfig)
type AgentMemoryConfig struct {
	// MemVariables 记忆变量配置列表
	MemVariables []commonschema.Param
	// EnableLongTermMem 是否启用长期记忆
	EnableLongTermMem bool
	// EnableUserProfile 是否启用用户画像记忆
	EnableUserProfile bool
	// EnableSemanticMemory 是否启用语义记忆
	EnableSemanticMemory bool
	// EnableEpisodicMemory 是否启用情景记忆
	EnableEpisodicMemory bool
	// EnableSummaryMemory 是否启用摘要记忆
	EnableSummaryMemory bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultAgentMemoryConfig 返回默认 Agent 记忆配置。
//
// 所有 enable 标志默认为 true，MemVariables 为空。
// 对齐 Python AgentMemoryConfig 的字段默认值。
func DefaultAgentMemoryConfig() *AgentMemoryConfig {
	return &AgentMemoryConfig{
		MemVariables:         []commonschema.Param{},
		EnableLongTermMem:    true,
		EnableUserProfile:    true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory:  true,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
