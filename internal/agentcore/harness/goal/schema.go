// 本文件对应 Python openjiuwen/harness/goal/schema.py，
// 提供持久化 Goal 领域模型与能力错误契约。
package goal

// ──────────────────────────── 结构体 ────────────────────────────

// GoalStatus 目标状态。
// Python: GoalStatus (schema.py L14)
type GoalStatus string

// GoalAssessmentStatus 一次评估的结论状态。
// Python: GoalAssessmentStatus (schema.py L21)
type GoalAssessmentStatus string

// GoalStopStrategy 停止策略。
// Python: GoalStopStrategy (schema.py L27)
type GoalStopStrategy string

// ──────────────────────────── 枚举 ────────────────────────────

const (
	// GoalStatusActive 进行中
	GoalStatusActive GoalStatus = "active"
	// GoalStatusPaused 已暂停（在飞轮允许自然收尾）
	GoalStatusPaused GoalStatus = "paused"
	// GoalStatusCompleted 已完成
	GoalStatusCompleted GoalStatus = "completed"
	// GoalStatusBlocked 已阻塞
	GoalStatusBlocked GoalStatus = "blocked"
)

const (
	// GoalAssessmentStatusContinue 继续下一轮
	GoalAssessmentStatusContinue GoalAssessmentStatus = "continue"
	// GoalAssessmentStatusComplete 目标完成
	GoalAssessmentStatusComplete GoalAssessmentStatus = "complete"
	// GoalAssessmentStatusBlocked 目标阻塞
	GoalAssessmentStatusBlocked GoalAssessmentStatus = "blocked"
)

const (
	// GoalStopStrategyAgentReport 只信主模型自报
	GoalStopStrategyAgentReport GoalStopStrategy = "agent_report"
	// GoalStopStrategyTranscript 只用独立监理模型
	GoalStopStrategyTranscript GoalStopStrategy = "transcript"
	// GoalStopStrategyHybrid 自报 + 终态复核（默认）
	GoalStopStrategyHybrid GoalStopStrategy = "hybrid"
)
