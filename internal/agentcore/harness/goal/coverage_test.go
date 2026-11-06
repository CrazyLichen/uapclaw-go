package goal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
)

// ──────────────────────────── 空值与非理想分支 ────────────────────────────

// TestGoalEvaluator_Strategy 校验策略访问器
// Python: GoalEvaluator.strategy (evaluation.py L84)
func TestGoalEvaluator_Strategy(t *testing.T) {
	assert.Equal(t, GoalStopStrategyHybrid, NewGoalEvaluator(nil).Strategy())
	assert.Equal(t,
		GoalStopStrategyTranscript,
		NewGoalEvaluator(&GoalStopConfig{Strategy: GoalStopStrategyTranscript}).Strategy(),
	)
}

// TestGoalEvaluator_监理解析失败与抽检容错 校验兜底分支
// Python: _verify_with_transcript 的 trust_on_failure 两支 (evaluation.py L225-254)
func TestGoalEvaluator_监理解析失败与抽检容错(t *testing.T) {
	assessor := NewGoalEvaluator(nil)
	garbage := "not json"

	// 终态报告 + 监理输出无法解析 → 退回 continue
	got := assessor.Assess(newEvaluationRecord(), newReport(GoalAssessmentStatusComplete), &garbage)
	assert.Equal(t, GoalAssessmentStatusContinue, got.Status)
	assert.Contains(t, got.Evidence, "assessment_error")

	// 抽检命中 + 监理输出无法解析 → 信任自报（trust_on_failure）
	interval := 1
	spotCheck := NewGoalEvaluator(&GoalStopConfig{
		Strategy:             GoalStopStrategyHybrid,
		VerificationInterval: &interval,
	})
	record := newEvaluationRecord()
	record.AttemptCount = 1
	trusted := spotCheck.Assess(record, newReport(GoalAssessmentStatusContinue), &garbage)
	assert.Equal(t, GoalAssessmentStatusContinue, trusted.Status)
	assert.Equal(t, "agent evidence", trusted.Evidence)
}

// TestGoalEvaluator_非法自报状态归一 校验 normalize 分支
// Python: GoalEvaluator._normalize_report 的 ValueError 分支 (evaluation.py L276)
func TestGoalEvaluator_非法自报状态归一(t *testing.T) {
	assessor := NewGoalEvaluator(&GoalStopConfig{Strategy: GoalStopStrategyAgentReport})
	report := &GoalAssessment{Status: GoalAssessmentStatus("unexpected"), Evidence: "x"}

	got := assessor.Assess(newEvaluationRecord(), report, nil)
	assert.Equal(t, GoalAssessmentStatusContinue, got.Status)
	assert.Contains(t, got.Evidence, "normalized_invalid_status")
}

// TestGoalManager_可选依赖缺失不崩溃 校验 nil eventManager/回调/锁
func TestGoalManager_可选依赖缺失不崩溃(t *testing.T) {
	store := NewSessionGoalStore(newFakeGoalSession("s1"))
	manager := NewGoalManager(GoalManagerConfig{Store: store})

	assert.Nil(t, manager.Peek())
	got, err := manager.Get(context.Background())
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.False(t, manager.HasActiveRecord())
	assert.False(t, manager.EnsureActiveGoalWorkLocked())

	_, err = manager.GetStore("")
	require.NoError(t, err, "空串不校验会话")

	// 无 eventManager 时 set 仍可写库，但不会排队
	record, err := manager.Set(context.Background(), "objective", SetOptions{})
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.False(t, manager.EnsureActiveGoalWorkLocked())

	paused, err := manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, paused)
	assert.Equal(t, GoalStatusPaused, paused.Status)

	cleared, err := manager.Clear(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cleared)
	assert.Nil(t, manager.Peek())
}

// TestSessionGoalStore_空会话安全 校验 nil 会话与 nil 记录
func TestSessionGoalStore_空会话安全(t *testing.T) {
	store := NewSessionGoalStore(nil)
	assert.Equal(t, "", store.SessionID())
	assert.Nil(t, store.Load())
	store.Save(CreateGoalRecord("s1", "objective", nil, nil))
	store.Clear()
	require.NoError(t, store.Commit(context.Background()))

	session := newFakeGoalSession("s1")
	realStore := NewSessionGoalStore(session)
	realStore.Save(nil)
	assert.Nil(t, realStore.Load(), "nil 记录不得写入")
}

// TestPyTypeName_全类型 校验日志用类型名
func TestPyTypeName_全类型(t *testing.T) {
	cases := map[string]any{
		"NoneType": nil,
		"str":      "x",
		"bool":     true,
		"int":      int64(1),
		"float":    1.5,
		"list":     []any{},
		"dict":     map[string]any{},
	}
	for want, value := range cases {
		assert.Equal(t, want, pyTypeName(value))
	}
	assert.Equal(t, "object", pyTypeName(struct{}{}))
}

// TestPyTruthy与PyStr_边界 校验 Python 真值/字符串化的边界
func TestPyTruthy与PyStr_边界(t *testing.T) {
	assert.False(t, pyTruthy(nil))
	assert.False(t, pyTruthy(false))
	assert.False(t, pyTruthy(""))
	assert.False(t, pyTruthy(0))
	assert.False(t, pyTruthy(int64(0)))
	assert.False(t, pyTruthy(0.0))
	assert.False(t, pyTruthy([]any{}))
	assert.False(t, pyTruthy(map[string]any{}))
	assert.True(t, pyTruthy(struct{}{}))

	assert.Equal(t, "None", pyStr(nil))
	assert.Equal(t, "x", pyStr("x"))
	assert.Equal(t, "7", pyStr(7))
}

// TestPyInt_更多数值类型 校验其余整数宽度
func TestPyInt_更多数值类型(t *testing.T) {
	for _, value := range []any{
		int8(1), int16(1), int32(1), uint(1), uint8(1), uint16(1), uint64(1), float32(1),
	} {
		got, err := pyInt(value)
		require.NoError(t, err)
		assert.Equal(t, 1, got)
	}
}

// TestGoalRecord_FromDict_tokenUsage非法 校验嵌套用量错误上抛
func TestGoalRecord_FromDict_tokenUsage非法(t *testing.T) {
	_, err := FromGoalRecordDict(map[string]any{
		"goal_id": "g", "session_id": "s", "objective": "x",
		"token_usage": map[string]any{"input_tokens": "abc"},
	})
	assert.Error(t, err)
}

// TestGoalRecord_FromDict_状态键为nil 校验非字符串状态报错
// Python: GoalStatus(str(None)) → ValueError
func TestGoalRecord_FromDict_状态键为nil(t *testing.T) {
	_, err := FromGoalRecordDict(map[string]any{
		"goal_id": "g", "session_id": "s", "objective": "x", "status": nil,
	})
	assert.Error(t, err)
}

// TestGoalManager_ApplyAssessment异代际 校验代际栅栏拒绝写入
func TestGoalManager_ApplyAssessment异代际(t *testing.T) {
	harness := newManagerHarness(false)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	got, err := harness.manager.ApplyAssessment(
		context.Background(), record.GoalID, record.Revision+1,
		GoalAssessment{Status: GoalAssessmentStatusComplete, Evidence: "x"},
	)
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestGoalManager_ResumeFromBlocked 校验 BLOCKED 可恢复
// Python: resume 接受 PAUSED 与 BLOCKED (manager.py L208)
func TestGoalManager_ResumeFromBlocked(t *testing.T) {
	harness := newManagerHarness(false)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)
	blocked := record.Clone()
	blocked.Status = GoalStatusBlocked
	harness.store.Save(blocked)

	resumed, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resumed)
	assert.Equal(t, GoalStatusActive, resumed.Status)
	assert.Equal(t, record.Revision+1, resumed.Revision, "BLOCKED 恢复提升代际")
}

// TestGoalManager_Completed不可Resume 校验 COMPLETED 不是可恢复态
// Python: resume 仅 PAUSED/BLOCKED (manager.py L208)
func TestGoalManager_Completed不可Resume(t *testing.T) {
	harness := newManagerHarness(false)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)
	completed := record.Clone()
	completed.Status = GoalStatusCompleted
	harness.store.Save(completed)

	resumed, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resumed)
	assert.Equal(t, GoalStatusCompleted, resumed.Status, "COMPLETED 原样返回")
}

// TestGoalManager_EnsureGoalWork_同代际去重 校验重复补排被去重
func TestGoalManager_EnsureGoalWork_同代际去重(t *testing.T) {
	harness := newManagerHarness(true)
	record := CreateGoalRecord("session-1", "objective", nil, nil)
	harness.store.Save(record)

	require.True(t, harness.manager.EnsureActiveGoalWorkLocked())
	assert.False(t, harness.manager.EnsureActiveGoalWorkLocked(), "同代际去重")
}

// TestCoordinator_接口断言 编译期保证 *GoalManager 满足 Coordinator
func TestCoordinator_接口断言(t *testing.T) {
	// 编译期断言：两个赋值不通过则构建失败
	var coordinator Coordinator = &GoalManager{}
	var store GoalStore = &SessionGoalStore{}

	assert.NotNil(t, coordinator)
	assert.NotNil(t, store)
}

// TestNewGoalManager_默认语言 校验空语言走 resolve_language
func TestNewGoalManager_默认语言(t *testing.T) {
	t.Setenv("AGENT_PROMPT_LANGUAGE", "")
	manager := NewGoalManager(GoalManagerConfig{
		Store:        NewSessionGoalStore(newFakeGoalSession("s1")),
		EventManager: task_loop.NewEventManager(),
	})
	assert.Equal(t, "cn", manager.Language())
}
