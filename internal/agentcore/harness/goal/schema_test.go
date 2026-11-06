package goal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/utils"
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
		{name: "布尔", input: true, want: 1, ok: true},
		{name: "map", input: map[string]any{}, ok: false},
		{name: "小数字符串", input: "3.0", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := utils.PyInt(tc.input)
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

// TestGoalAssessment_往返与非法状态 校验评估快照序列化
// Python: test_assessment_round_trip_and_invalid_status (test_goal_schema.py L59)
func TestGoalAssessment_往返与非法状态(t *testing.T) {
	remaining := "还差测试"
	original := &GoalAssessment{
		Status:        GoalAssessmentStatusContinue,
		Evidence:      "接口已实现",
		RemainingWork: &remaining,
	}
	dict := original.ToDict()
	assert.Equal(t, "continue", dict["status"])
	assert.Equal(t, "接口已实现", dict["evidence"])
	assert.Equal(t, "还差测试", dict["remaining_work"])
	assert.Nil(t, dict["next_instruction"], "未设置的可选字段必须序列化为 nil")

	restored := FromGoalAssessmentDict(dict)
	require.NotNil(t, restored)
	assert.Equal(t, GoalAssessmentStatusContinue, restored.Status)
	assert.Equal(t, "接口已实现", restored.Evidence)
	require.NotNil(t, restored.RemainingWork)
	assert.Equal(t, "还差测试", *restored.RemainingWork)

	invalid := FromGoalAssessmentDict(map[string]any{"status": "unknown", "evidence": "x"})
	require.NotNil(t, invalid)
	assert.Equal(t, GoalAssessmentStatusContinue, invalid.Status, "非法状态回退 continue")

	empty := FromGoalAssessmentDict(map[string]any{"status": "complete", "evidence": ""})
	require.NotNil(t, empty)
	assert.Equal(t, "", empty.Evidence, "evidence 缺失或空串 → 空串")
}

// TestTokenUsage_累加与往返 校验 token 计数
// Python: test_token_usage_accumulates_and_round_trips (test_goal_schema.py L45)
func TestTokenUsage_累加与往返(t *testing.T) {
	var usage TokenUsage
	usage.Accumulate(10, 20, 5)
	usage.Accumulate(1, 2, 0)

	assert.Equal(t, 11, usage.InputTokens)
	assert.Equal(t, 22, usage.OutputTokens)
	assert.Equal(t, 5, usage.CachedInputTokens)
	assert.Equal(t, 33, usage.TotalTokens, "total 只累计 input+output")

	restored, err := TokenUsageFromDict(usage.ToDict())
	require.NoError(t, err)
	assert.Equal(t, usage, restored)

	withFloat, err := TokenUsageFromDict(map[string]any{
		"input_tokens": float64(3), "output_tokens": "4",
	})
	require.NoError(t, err)
	assert.Equal(t, 3, withFloat.InputTokens)
	assert.Equal(t, 4, withFloat.OutputTokens)
	// 照抄上游：total 只从持久化字段读，不由 input+output 推导
	assert.Equal(t, 0, withFloat.TotalTokens)

	_, err = TokenUsageFromDict(map[string]any{"input_tokens": "abc"})
	assert.Error(t, err, "非法数值必须报错")
}

// TestGoalRecord_往返与响应副本 校验序列化与深拷贝隔离
// Python: test_goal_record_round_trip_and_response_copy (test_goal_schema.py L71)
func TestGoalRecord_往返与响应副本(t *testing.T) {
	maxAttempts := 4
	tokenBudget := 8000
	record := CreateGoalRecord("session-1", "Build a REST API", &maxAttempts, &tokenBudget)
	record.AttemptCount = 2
	record.LastAssessment = &GoalAssessment{
		Status:   GoalAssessmentStatusContinue,
		Evidence: "routes are ready",
	}
	record.Touch(true)

	restored, err := FromGoalRecordDict(record.ToDict())
	require.NoError(t, err)
	responseCopy := record.Clone()
	responseCopy.Objective = "changed only in the response"

	assert.Equal(t, record.GoalID, restored.GoalID)
	assert.Equal(t, GoalStatusActive, restored.Status)
	assert.Equal(t, 1, restored.Revision)
	require.NotNil(t, restored.LastAssessment)
	assert.Equal(t, "routes are ready", restored.LastAssessment.Evidence)
	assert.Equal(t, "Build a REST API", record.Objective, "改副本不得影响原记录")
}

// TestGoalRecord_时间字段往返 校验 create/settle 与序列化
// Python: test_goal_record_time_fields_round_trip (test_goal_schema.py L97)
func TestGoalRecord_时间字段往返(t *testing.T) {
	restore := utcNow
	t.Cleanup(func() { utcNow = restore })

	utcNow = func() time.Time { return time.Unix(1000, 0).UTC() }
	record := CreateGoalRecord("session-1", "Build a REST API", nil, nil)
	assert.Equal(t, 0, record.TimeUsedSeconds)
	require.NotNil(t, record.ActiveStartedAt)
	assert.Equal(t, "1970-01-01T00:16:40+00:00", *record.ActiveStartedAt)
	assert.Equal(t, "1970-01-01T00:16:40+00:00", record.CreatedAt)
	assert.Equal(t, "1970-01-01T00:16:40+00:00", record.UpdatedAt)

	utcNow = func() time.Time { return time.Unix(1065, 0).UTC() }
	require.NoError(t, record.SettleActiveTime(false))
	record.Status = GoalStatusPaused

	restored, err := FromGoalRecordDict(record.ToDict())
	require.NoError(t, err)
	assert.Equal(t, 65, restored.TimeUsedSeconds)
	assert.Nil(t, restored.ActiveStartedAt)
	assert.Equal(t, "1970-01-01T00:16:40+00:00", restored.CreatedAt)
	assert.Equal(t, "1970-01-01T00:17:45+00:00", restored.UpdatedAt)
}

// TestGoalRecord_拒绝非法持久化载荷 校验严格校验（表驱动）
// Python: test_goal_record_rejects_invalid_persistence_payload (test_goal_schema.py L132)
func TestGoalRecord_拒绝非法持久化载荷(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
	}{
		{
			name:    "空目标",
			payload: map[string]any{"goal_id": "g", "session_id": "s", "objective": ""},
		},
		{
			name:    "缺目标",
			payload: map[string]any{"goal_id": "g", "session_id": "s"},
		},
		{
			name:    "非字符串目标",
			payload: map[string]any{"goal_id": "g", "session_id": "s", "objective": 7},
		},
		{
			name:    "非法状态",
			payload: map[string]any{"goal_id": "g", "session_id": "s", "objective": "x", "status": "bad"},
		},
		{
			name: "负工时",
			payload: map[string]any{
				"goal_id": "g", "session_id": "s", "objective": "x",
				"status": "active", "time_used_seconds": -1,
			},
		},
		{
			name: "零次数上限",
			payload: map[string]any{
				"goal_id": "g", "session_id": "s", "objective": "x",
				"status": "active", "max_attempts": 0,
			},
		},
		{
			name: "无时区时间戳",
			payload: map[string]any{
				"goal_id": "g", "session_id": "s", "objective": "x",
				"status": "active", "created_at": "2026-11-06T06:10:22",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FromGoalRecordDict(tc.payload)
			assert.Error(t, err)
		})
	}
}

// TestGoalRecord_容忍Session删除的可选时间戳 校验 ACTIVE 时钟自愈
// Python: test_goal_record_tolerates_session_deleted_optional_timestamps (test_goal_schema.py L137)
func TestGoalRecord_容忍Session删除的可选时间戳(t *testing.T) {
	paused, err := FromGoalRecordDict(map[string]any{
		"goal_id": "g1", "session_id": "s1", "objective": "write a report",
		"status": "paused", "time_used_seconds": 12,
		"created_at": "1970-01-01T00:16:40+00:00",
		"updated_at": "1970-01-01T00:16:52+00:00",
	})
	require.NoError(t, err)
	assert.Equal(t, GoalStatusPaused, paused.Status)
	assert.Nil(t, paused.ActiveStartedAt)
	assert.Equal(t, 12, paused.TimeUsedSeconds)

	active, err := FromGoalRecordDict(map[string]any{
		"goal_id": "g2", "session_id": "s1", "objective": "write a report",
		"status": "active", "time_used_seconds": 5,
		"created_at": "1970-01-01T00:16:40+00:00",
		"updated_at": "1970-01-01T00:16:45+00:00",
	})
	require.NoError(t, err)
	require.NotNil(t, active.ActiveStartedAt, "ACTIVE 缺时钟必须自愈")
	assert.Equal(t, "1970-01-01T00:16:45+00:00", *active.ActiveStartedAt)
}

// TestGoalRecord_FromDict_JSON还原 证明 checkpoint 还原的 float64 不炸
func TestGoalRecord_FromDict_JSON还原(t *testing.T) {
	record := CreateGoalRecord("s1", "objective", nil, nil)
	record.TokenUsage.Accumulate(3, 4, 0)

	restored, err := FromGoalRecordDict(map[string]any{
		"goal_id":           record.GoalID,
		"session_id":        "s1",
		"objective":         "objective",
		"status":            "active",
		"revision":          float64(2),
		"attempt_count":     float64(1),
		"time_used_seconds": float64(30),
		"max_attempts":      float64(5),
		"token_usage": map[string]any{
			"input_tokens": float64(3), "output_tokens": float64(4),
			"cached_input_tokens": float64(0), "total_tokens": float64(7),
		},
		"created_at": "1970-01-01T00:16:40+00:00",
		"updated_at": "1970-01-01T00:17:10+00:00",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, restored.Revision)
	assert.Equal(t, 1, restored.AttemptCount)
	assert.Equal(t, 30, restored.TimeUsedSeconds)
	require.NotNil(t, restored.MaxAttempts)
	assert.Equal(t, 5, *restored.MaxAttempts)
	assert.Equal(t, 7, restored.TokenUsage.TotalTokens)
}

// TestClone_指针字段隔离 校验深拷贝覆盖全部指针字段
func TestClone_指针字段隔离(t *testing.T) {
	maxAttempts := 2
	tokenBudget := 100
	reason := "completed"
	startedAt := "1970-01-01T00:16:40+00:00"
	remaining := "gap"
	record := &GoalRecord{
		GoalID:          "g1",
		SessionID:       "s1",
		Objective:       "obj",
		Status:          GoalStatusActive,
		MaxAttempts:     &maxAttempts,
		TokenBudget:     &tokenBudget,
		LastStopReason:  &reason,
		ActiveStartedAt: &startedAt,
		LastAssessment: &GoalAssessment{
			Status:        GoalAssessmentStatusContinue,
			Evidence:      "evidence",
			RemainingWork: &remaining,
		},
	}

	clone := record.Clone()
	*clone.MaxAttempts = 9
	*clone.TokenBudget = 900
	*clone.LastStopReason = "blocked"
	*clone.ActiveStartedAt = "changed"
	*clone.LastAssessment.RemainingWork = "changed"

	assert.Equal(t, 2, *record.MaxAttempts)
	assert.Equal(t, 100, *record.TokenBudget)
	assert.Equal(t, "completed", *record.LastStopReason)
	assert.Equal(t, "1970-01-01T00:16:40+00:00", *record.ActiveStartedAt)
	assert.Equal(t, "gap", *record.LastAssessment.RemainingWork)
}

// TestGoalStopConfig_默认值 校验默认停止策略与窗口
// Python: test_stop_config_defaults (test_goal_schema.py L184)
func TestGoalStopConfig_默认值(t *testing.T) {
	config := NewGoalStopConfig()
	assert.Equal(t, GoalStopStrategyHybrid, config.Strategy)
	assert.Equal(t, 8, config.TranscriptWindowAttempts)
	assert.Nil(t, config.VerificationInterval)
	assert.Nil(t, config.MaxAttempts)
	assert.Nil(t, config.TokenBudget)
}

// TestGoalOperationError_持有隔离副本 校验错误对象不反改原记录
// Python: test_goal_operation_error_keeps_an_isolated_goal_copy (test_goal_schema.py L169)
func TestGoalOperationError_持有隔离副本(t *testing.T) {
	record := CreateGoalRecord("s", "original", nil, nil)
	err := newGoalOperationError("set", "already_exists", "a goal already exists", record)
	require.NotNil(t, err.Goal)

	err.Goal.Objective = "changed"
	assert.Equal(t, "original", record.Objective)
	assert.Equal(t, "already_exists", err.Code)
	assert.Equal(t, "a goal already exists", err.Error())

	var target *GoalOperationError
	wrapped := error(err)
	require.ErrorAs(t, wrapped, &target)
	assert.Equal(t, "set", target.Operation)
}
