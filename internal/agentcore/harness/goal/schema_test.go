package goal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGoalStatus_枚举取值 校验目标状态与上游字符串一致
// Python: GoalStatus (schema.py L14)
func TestGoalStatus_枚举取值(t *testing.T) {
	assert.Equal(t, "active", string(GoalStatusActive))
	assert.Equal(t, "paused", string(GoalStatusPaused))
	assert.Equal(t, "completed", string(GoalStatusCompleted))
	assert.Equal(t, "blocked", string(GoalStatusBlocked))
}

// TestGoalAssessmentStatus_枚举取值 校验评估三态与上游字符串一致
// Python: GoalAssessmentStatus (schema.py L21)
func TestGoalAssessmentStatus_枚举取值(t *testing.T) {
	assert.Equal(t, "continue", string(GoalAssessmentStatusContinue))
	assert.Equal(t, "complete", string(GoalAssessmentStatusComplete))
	assert.Equal(t, "blocked", string(GoalAssessmentStatusBlocked))
}

// TestGoalStopStrategy_枚举取值 校验停止策略与上游字符串一致
// Python: GoalStopStrategy (schema.py L27)
func TestGoalStopStrategy_枚举取值(t *testing.T) {
	assert.Equal(t, "agent_report", string(GoalStopStrategyAgentReport))
	assert.Equal(t, "transcript", string(GoalStopStrategyTranscript))
	assert.Equal(t, "hybrid", string(GoalStopStrategyHybrid))
}
