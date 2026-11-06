package goal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestUtcNowISO_产出Python同形时间戳 校验字符串格式与 Python isoformat 一致
// Python: _utc_now_iso (schema.py L80) → "1970-01-01T00:16:40+00:00"
func TestUtcNowISO_产出Python同形时间戳(t *testing.T) {
	restore := utcNow
	t.Cleanup(func() { utcNow = restore })

	utcNow = func() time.Time { return time.Unix(1000, 0).UTC() }
	assert.Equal(t, "1970-01-01T00:16:40+00:00", utcNowISO())

	utcNow = func() time.Time { return time.Unix(1000, 123456000).UTC() }
	assert.Equal(t, "1970-01-01T00:16:40.123456+00:00", utcNowISO())
}

// TestParseGoalTimestamp_拒绝无时区与非法值 校验严格解析
// Python: _parse_timestamp (schema.py L56)
func TestParseGoalTimestamp_拒绝无时区与非法值(t *testing.T) {
	_, err := parseGoalTimestamp("2026-11-06T06:10:22", "created_at")
	assert.Error(t, err, "无时区必须报错")

	_, err = parseGoalTimestamp("not-a-time", "created_at")
	assert.Error(t, err)

	_, err = parseGoalTimestamp(12345, "created_at")
	assert.Error(t, err, "非字符串必须报错")

	parsed, err := parseGoalTimestamp("2026-11-06T06:10:22+00:00", "created_at")
	assert.NoError(t, err)
	assert.Equal(t, 2026, parsed.Year())
}

// TestPyInt_全类型表 校验 Python int() 语义
// Python: int(value) 接受 int/float(截断)/str
func TestPyInt_全类型表(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  int
		ok    bool
	}{
		{name: "int", input: 7, want: 7, ok: true},
		{name: "int64", input: int64(7), want: 7, ok: true},
		{name: "uint32", input: uint32(7), want: 7, ok: true},
		{name: "float64整数", input: float64(7), want: 7, ok: true},
		{name: "float64截断", input: 3.9, want: 3, ok: true},
		{name: "字符串", input: "7", want: 7, ok: true},
		{name: "字符串带空白", input: " 7 ", want: 7, ok: true},
		{name: "nil", input: nil, ok: false},
		{name: "非数字字符串", input: "abc", ok: false},
		{name: "布尔", input: true, ok: false},
		{name: "map", input: map[string]any{}, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pyInt(tc.input)
			if !tc.ok {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestOptionalPositiveInt_只接受正数 校验持久化字段校验
// Python: _optional_positive_int (schema.py L33)
func TestOptionalPositiveInt_只接受正数(t *testing.T) {
	assert.Nil(t, mustOptionalPositiveInt(t, nil, "max_attempts"))

	value := mustOptionalPositiveInt(t, 5, "max_attempts")
	require.NotNil(t, value)
	assert.Equal(t, 5, *value)

	_, err := optionalPositiveInt(0, "max_attempts")
	assert.ErrorContains(t, err, "invalid persisted GoalRecord max_attempts")

	_, err = optionalPositiveInt(-1, "max_attempts")
	assert.Error(t, err)
}

// TestNonNegativeInt_拒绝负数 校验持久化字段校验
// Python: _non_negative_int (schema.py L46)
func TestNonNegativeInt_拒绝负数(t *testing.T) {
	got, err := nonNegativeInt(0, "time_used_seconds")
	assert.NoError(t, err)
	assert.Equal(t, 0, got)

	_, err = nonNegativeInt(-1, "time_used_seconds")
	assert.ErrorContains(t, err, "invalid persisted GoalRecord time_used_seconds")
}

// mustOptionalPositiveInt 测试助手：断言无错误并返回值
func mustOptionalPositiveInt(t *testing.T, input any, field string) *int {
	t.Helper()
	value, err := optionalPositiveInt(input, field)
	assert.NoError(t, err)
	return value
}
