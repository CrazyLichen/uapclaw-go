package signal

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionSignal 演化信号，标识 Agent 执行过程中的问题和诊断信息。
//
// 信号由评估结果（离线）或对话监控（在线）产生，
// 驱动优化器决定优化方向和内容。
//
// 对应 Python: openjiuwen/agent_evolving/signal/base.py EvolutionSignal
type EvolutionSignal struct {
	// SignalType 信号类型（如 "low_score"、"execution_failure"、"user_correction"）
	SignalType string
	// Section 建议修改的 SKILL.md 区域（如 "Troubleshooting"、"Examples"）
	Section string
	// Excerpt 问题摘要或关键片段
	Excerpt string
	// SkillName 关联的技能名称（可选）
	SkillName *string
	// Context 诊断上下文（如 question/label/answer/reason/score/source/tool_name）
	Context map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
