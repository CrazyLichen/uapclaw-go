package goal

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
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
	got := manager.Get()
	assert.Nil(t, got)
	assert.False(t, manager.HasActiveRecord())
	ensured, err := manager.EnsureActiveGoalWorkLocked()
	require.NoError(t, err)
	assert.False(t, ensured)

	_, err = manager.GetStore("")
	require.NoError(t, err, "空串不校验会话")

	// 无 eventManager 时 set 仍可写库，但不会排队
	record, err := manager.Set(context.Background(), "objective", SetOptions{})
	require.NoError(t, err)
	require.NotNil(t, record)
	ensured, err = manager.EnsureActiveGoalWorkLocked()
	require.NoError(t, err)
	assert.False(t, ensured)

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

	assert.Equal(t, "None", utils.PyStr(nil))
	assert.Equal(t, "x", utils.PyStr("x"))
	assert.Equal(t, "7", utils.PyStr(7))
}

// TestPyInt_更多数值类型 校验其余整数宽度
func TestPyInt_更多数值类型(t *testing.T) {
	for _, value := range []any{
		int8(1), int16(1), int32(1), uint(1), uint8(1), uint16(1), uint64(1), float32(1),
	} {
		got, err := utils.PyInt(value)
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

	first, firstErr := harness.manager.EnsureActiveGoalWorkLocked()
	require.NoError(t, firstErr)
	require.True(t, first)

	second, secondErr := harness.manager.EnsureActiveGoalWorkLocked()
	require.NoError(t, secondErr)
	assert.False(t, second, "同代际去重")
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

// TestGoalRecord_SettleActiveTime_坏时间戳报错 校验不再静默记 0 秒
// Python: settle_active_time → _parse_timestamp 抛 ValueError (schema.py L199)
func TestGoalRecord_SettleActiveTime_坏时间戳报错(t *testing.T) {
	bad := "not-a-timestamp"
	record := CreateGoalRecord("s1", "objective", nil, nil)
	record.ActiveStartedAt = &bad
	record.TimeUsedSeconds = 5

	err := record.SettleActiveTime(false)
	require.Error(t, err)
	assert.Equal(t, 5, record.TimeUsedSeconds, "失败时不得改动工时")
	assert.NotNil(t, record.ActiveStartedAt, "失败时不得清空时钟")
}

// corruptGoalStore 直接持有记录的内存 store，用于注入绕过 FromDict 校验的脏数据
type corruptGoalStore struct {
	record    *GoalRecord
	sessionID string
}

// SessionID 返回会话标识
func (s *corruptGoalStore) SessionID() string { return s.sessionID }

// Load 返回原始记录（不做校验）
func (s *corruptGoalStore) Load() *GoalRecord { return s.record }

// Save 覆盖记录
func (s *corruptGoalStore) Save(record *GoalRecord) { s.record = record }

// Clear 清空记录
func (s *corruptGoalStore) Clear() { s.record = nil }

// Commit 空提交
func (s *corruptGoalStore) Commit(context.Context) error { return nil }

// TestGoalManager_坏时间戳使写操作失败 校验错误向上冒泡
// Python: pause/clear/apply_assessment 内的 settle_active_time 抛 ValueError
func TestGoalManager_坏时间戳使写操作失败(t *testing.T) {
	bad := "not-a-timestamp"
	broken := CreateGoalRecord("session-1", "write a report", nil, nil)
	broken.ActiveStartedAt = &bad
	store := &corruptGoalStore{record: broken, sessionID: "session-1"}
	manager := NewGoalManager(GoalManagerConfig{Store: store, ControlLock: &sync.Mutex{}})

	_, err := manager.Pause(context.Background())
	assert.Error(t, err, "pause 必须把结算错误抛给调用方")

	_, err = manager.Clear(context.Background())
	assert.Error(t, err, "clear 同样必须报错")

	_, err = manager.ApplyAssessment(
		context.Background(), broken.GoalID, broken.Revision,
		GoalAssessment{Status: GoalAssessmentStatusComplete, Evidence: "x"},
	)
	assert.Error(t, err, "apply_assessment 同样必须报错")

	_, err = manager.Resume(context.Background())
	assert.NoError(t, err, "ACTIVE 记录不触发 resume 结算")
}

// TestGoalManager_Language并发读写 校验原子指针（-race 下暴露竞争）
func TestGoalManager_Language并发读写(t *testing.T) {
	t.Setenv("AGENT_PROMPT_LANGUAGE", "")
	harness := newManagerHarness(true)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	var waitGroup sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		waitGroup.Add(2)
		go func() {
			defer waitGroup.Done()
			for i := 0; i < 50; i++ {
				harness.manager.SetLanguage("en")
				harness.manager.SetLanguage("cn")
			}
		}()
		go func() {
			defer waitGroup.Done()
			for i := 0; i < 50; i++ {
				_ = harness.manager.Language()
				_, _ = harness.manager.EnsureActiveGoalWorkLocked()
				_ = harness.manager.Get()
			}
		}()
	}
	waitGroup.Wait()

	assert.NotEmpty(t, harness.manager.Language())
	assert.NotNil(t, record)
}
