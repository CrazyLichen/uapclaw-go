package ltm

import (
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemInfo 记忆信息。
//
// Python: MemInfo
type MemInfo struct {
	// MemID 记忆唯一标识
	MemID string `json:"mem_id"`
	// Content 记忆内容
	Content string `json:"content"`
	// Type 记忆类型
	Type mem_model.MemoryType `json:"type"`
	// Timestamp 记忆时间戳
	Timestamp *time.Time `json:"timestamp"`
}

// MemResult 记忆搜索结果。
//
// Python: MemResult
type MemResult struct {
	// MemInfo 记忆信息
	MemInfo *MemInfo `json:"mem_info"`
	// Score 相关度分数
	Score float64 `json:"score"`
}

// AddMemResult 添加记忆操作结果。
//
// Python: AddMemResult
type AddMemResult struct {
	// Variables 变量记忆结果
	Variables []*mem_model.VariableUnit `json:"variables"`
	// UserProfile 用户画像记忆结果
	UserProfile []*mem_model.FragmentMemoryUnit `json:"user_profile"`
	// SemanticMemory 语义记忆结果
	SemanticMemory []*mem_model.FragmentMemoryUnit `json:"semantic_memory"`
	// EpisodicMemory 情景记忆结果
	EpisodicMemory []*mem_model.FragmentMemoryUnit `json:"episodic_memory"`
	// Summary 摘要记忆结果
	Summary []*mem_model.SummaryUnit `json:"summary"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
