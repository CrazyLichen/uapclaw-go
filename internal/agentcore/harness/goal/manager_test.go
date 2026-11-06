package goal

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// cancelCall 记录一次取消在飞轮回调
type cancelCall struct {
	// RunKind 期望的运行类型
	RunKind hschema.RoundWorkKind
	// GoalID 期望的目标标识
	GoalID string
	// Reason 取消原因
	Reason string
}

// managerHarness 真 store + 真 EventManager + 可控回调
type managerHarness struct {
	// session 内存会话
	session *fakeGoalSession
	// store 目标存储
	store *SessionGoalStore
	// events 双队列调度器（无内部锁，测试单线程使用）
	events *task_loop.EventManager
	// manager 被测对象
	manager *GoalManager
	// emitted 已发事件
	emitted []hschema.InteractionEvent
	// cancels 已发取消调用
	cancels []cancelCall
	// notifies 唤醒次数
	notifies int
	// outputAttached 是否有输出消费者
	outputAttached bool
}

// newManagerHarness 构造夹具
func newManagerHarness(outputAttached bool) *managerHarness {
	harness := &managerHarness{
		session:        newFakeGoalSession("session-1"),
		events:         task_loop.NewEventManager(),
		outputAttached: outputAttached,
	}
	harness.store = NewSessionGoalStore(harness.session)
	harness.manager = NewGoalManager(GoalManagerConfig{
		Store:           harness.store,
		EventManager:    harness.events,
		ControlLock:     &sync.Mutex{},
		HasOutputStream: func() bool { return harness.outputAttached },
		CancelActiveRound: func(_ context.Context, kind hschema.RoundWorkKind, goalID, reason string) {
			harness.cancels = append(harness.cancels, cancelCall{RunKind: kind, GoalID: goalID, Reason: reason})
		},
		EmitEvent:  func(event hschema.InteractionEvent) { harness.emitted = append(harness.emitted, event) },
		NotifyWork: func() { harness.notifies++ },
	})
	return harness
}

// freezeGoalClock 冻结 goal 时钟，返回可改写的秒数指针
func freezeGoalClock(t *testing.T, seconds int64) *int64 {
	t.Helper()
	restore := utcNow
	current := seconds
	utcNow = func() time.Time { return time.Unix(current, 0).UTC() }
	t.Cleanup(func() { utcNow = restore })
	return &current
}

// ──────────────────────────── 读路径 ────────────────────────────

// TestGoalManager_GetStore 校验会话绑定校验
// Python: get_store (manager.py L83)
func TestGoalManager_GetStore(t *testing.T) {
	harness := newManagerHarness(true)

	store, err := harness.manager.GetStore("session-1")
	require.NoError(t, err)
	assert.Equal(t, "session-1", store.SessionID())

	store, err = harness.manager.GetStore("")
	require.NoError(t, err, "空串视为未指定")
	assert.NotNil(t, store)

	_, err = harness.manager.GetStore("other-session")
	assert.ErrorIs(t, err, ErrSessionMismatch)
}

// TestGoalManager_Peek与Get 校验读路径
func TestGoalManager_Peek与Get(t *testing.T) {
	harness := newManagerHarness(true)
	assert.Nil(t, harness.manager.Peek())
	got, err := harness.manager.Get(context.Background())
	require.NoError(t, err)
	assert.Nil(t, got)

	harness.store.Save(CreateGoalRecord("session-1", "write a report", nil, nil))
	peeked := harness.manager.Peek()
	require.NotNil(t, peeked)
	assert.Equal(t, "write a report", peeked.Objective)

	peeked.Objective = "tampered"
	fresh := harness.manager.Peek()
	require.NotNil(t, fresh)
	assert.Equal(t, "write a report", fresh.Objective, "Peek 必须返回隔离副本")
}

// TestGoalManager_HasActiveRecord 校验 ACTIVE 判定
func TestGoalManager_HasActiveRecord(t *testing.T) {
	harness := newManagerHarness(true)
	assert.False(t, harness.manager.HasActiveRecord())

	record := CreateGoalRecord("session-1", "write a report", nil, nil)
	harness.store.Save(record)
	assert.True(t, harness.manager.HasActiveRecord())

	record.Status = GoalStatusPaused
	harness.store.Save(record)
	assert.False(t, harness.manager.HasActiveRecord())
}

// TestGoalManager_Language 校验语言解析
// Python: language property setter (manager.py L79)
func TestGoalManager_Language(t *testing.T) {
	// 清掉环境变量，避免 AGENT_PROMPT_LANGUAGE 影响回退判断
	t.Setenv("AGENT_PROMPT_LANGUAGE", "")

	harness := newManagerHarness(true)
	harness.manager.SetLanguage("en")
	assert.Equal(t, "en", harness.manager.Language())

	harness.manager.SetLanguage("unexpected")
	assert.Equal(t, "cn", harness.manager.Language(), "未知语言回退中文")
}

// ──────────────────────────── Set ────────────────────────────

// TestGoalManager_Set_空目标被拒 校验空目标与非法预算
// Python: test_set_requires_a_non_empty_objective (test_goal_manager.py L96)
func TestGoalManager_Set_空目标被拒(t *testing.T) {
	harness := newManagerHarness(true)

	_, err := harness.manager.Set(context.Background(), "  ", SetOptions{})
	require.Error(t, err)
	var operationErr *GoalOperationError
	require.ErrorAs(t, err, &operationErr)
	assert.Equal(t, "invalid_objective", operationErr.Code)

	got, getErr := harness.manager.Get(context.Background())
	require.NoError(t, getErr)
	assert.Nil(t, got, "校验失败不得写库")

	zero := 0
	_, err = harness.manager.Set(context.Background(), "objective", SetOptions{TokenBudget: &zero})
	require.Error(t, err)
	var budgetErr *GoalOperationError
	require.ErrorAs(t, err, &budgetErr)
	assert.Equal(t, "invalid_objective", budgetErr.Code, "非法预算沿用 invalid_objective 码")

	_, err = harness.manager.Set(context.Background(), "objective", SetOptions{MaxAttempts: &zero})
	require.Error(t, err)
}

// TestGoalManager_Set_仅有输出流时排队 校验排队与事件
// Python: test_set_persists_goal_and_queues_work_only_with_an_output_consumer (L107)
func TestGoalManager_Set_仅有输出流时排队(t *testing.T) {
	detached := newManagerHarness(false)
	record, err := detached.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "write a report", record.Objective)
	assert.Nil(t, detached.events.NextWork(), "无输出流不得排队")
	assert.Empty(t, detached.emitted)

	attached := newManagerHarness(true)
	record, err = attached.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	queued := attached.events.NextWork()
	require.NotNil(t, queued, "有输出流必须排队")
	assert.Equal(t, hschema.RoundWorkKindGoal, queued.Kind)
	assert.Equal(t, record.GoalID, queued.Context.GoalID)
	assert.Equal(t, record.Revision, queued.Context.Revision)
	assert.Equal(t, "session-1", queued.Context.SessionID)
	assert.Contains(t, queued.Inputs["query"], "<goal_task>")
	assert.Equal(t, 1, attached.notifies)
	require.Len(t, attached.emitted, 1)
	assert.Equal(t, hschema.InteractionEventTypeGoalUpdated, attached.emitted[0].Type)
}

// TestGoalManager_Set_覆盖需确认 校验 already_exists 与取消在飞轮
// Python: test_set_requires_confirmation_before_replacing_existing_goal (L146)
func TestGoalManager_Set_覆盖需确认(t *testing.T) {
	harness := newManagerHarness(true)
	previous, err := harness.manager.Set(context.Background(), "first goal", SetOptions{})
	require.NoError(t, err)

	_, err = harness.manager.Set(context.Background(), "second goal", SetOptions{})
	require.Error(t, err)
	var operationErr *GoalOperationError
	require.ErrorAs(t, err, &operationErr)
	assert.Equal(t, "already_exists", operationErr.Code)
	require.NotNil(t, operationErr.Goal)
	assert.Equal(t, previous.GoalID, operationErr.Goal.GoalID)

	replacement, err := harness.manager.Set(
		context.Background(), "second goal", SetOptions{OverwriteConfirmed: true},
	)
	require.NoError(t, err)
	assert.NotEqual(t, previous.GoalID, replacement.GoalID)
	assert.Equal(t, "second goal", replacement.Objective)

	require.Len(t, harness.cancels, 1)
	assert.Equal(t, hschema.RoundWorkKindGoal, harness.cancels[0].RunKind)
	assert.Equal(t, previous.GoalID, harness.cancels[0].GoalID)
	assert.Equal(t, "goal_overwrite", harness.cancels[0].Reason)
}

// TestGoalManager_Set_写库后立即提交 校验 commit 调用
// Python: test_goal_writes_are_committed_immediately (L127) 的 set 部分
func TestGoalManager_Set_写库后立即提交(t *testing.T) {
	harness := newManagerHarness(false)
	_, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, harness.session.commits)
}

// ──────────────────────────── Pause / Resume ────────────────────────────

// TestGoalManager_PauseResume_无目标时为noop 校验空记录语义
// Python: test_pause_and_resume_are_noops_without_a_goal (L167) 的 pause/resume 部分
func TestGoalManager_PauseResume_无目标时为noop(t *testing.T) {
	harness := newManagerHarness(true)

	paused, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	assert.Nil(t, paused)

	resumed, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	assert.Nil(t, resumed)
}

// TestGoalManager_PauseThenResume_重排 校验暂停丢弃 pending、恢复提升 revision
// Python: test_pause_then_resume_updates_state_and_requeues_goal_work (L176)
func TestGoalManager_PauseThenResume_重排(t *testing.T) {
	harness := newManagerHarness(true)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)
	assert.True(t, harness.events.HasPendingWork())

	paused, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, paused)
	assert.Equal(t, GoalStatusPaused, paused.Status)
	assert.Nil(t, harness.events.NextWork(), "暂停必须丢弃待办续跑")

	resumed, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resumed)
	assert.Equal(t, GoalStatusActive, resumed.Status)
	assert.Equal(t, record.Revision+1, resumed.Revision, "空闲恢复提升代际")

	queued := harness.events.NextWork()
	require.NotNil(t, queued)
	assert.Equal(t, record.GoalID, queued.Context.GoalID)
	assert.Equal(t, resumed.Revision, queued.Context.Revision)
}

// TestGoalManager_二次Pause仍可加载 校验暂停后再次暂停不自毁
// Python: test_second_pause_after_settle_still_loads_goal (L198)
func TestGoalManager_二次Pause仍可加载(t *testing.T) {
	harness := newManagerHarness(true)
	_, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	first, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, GoalStatusPaused, first.Status)
	assert.Nil(t, first.ActiveStartedAt)

	second, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, GoalStatusPaused, second.Status)

	got, err := harness.manager.Get(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, got, "二次暂停不得把记录清空")
}

// TestGoalManager_Pause_无在飞轮即结算 校验空闲暂停立即结算工时
// Python: test_pause_without_in_flight_attempt_still_settles_immediately (L320)
func TestGoalManager_Pause_无在飞轮即结算(t *testing.T) {
	clock := freezeGoalClock(t, 1000)
	harness := newManagerHarness(true)
	_, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	*clock = 1030
	paused, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, paused)
	assert.Equal(t, 30, paused.TimeUsedSeconds)
	assert.Nil(t, paused.ActiveStartedAt)
}

// TestGoalManager_Pause_在飞轮保持计时 校验在飞轮时 pause 重开时钟
// Python: test_pause_keeps_timing_while_in_flight_attempt_finishes (L263) 前半
func TestGoalManager_Pause_在飞轮保持计时(t *testing.T) {
	clock := freezeGoalClock(t, 1000)
	harness := newManagerHarness(true)
	_, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	work := harness.events.NextWork()
	require.NotNil(t, work)
	harness.events.MarkStarted(work)

	*clock = 1007
	paused, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, paused)
	assert.Equal(t, 7, paused.TimeUsedSeconds)
	require.NotNil(t, paused.ActiveStartedAt, "在飞轮必须重开时钟")
	assert.Equal(t, "1970-01-01T00:16:47+00:00", *paused.ActiveStartedAt)
}

// TestGoalManager_Pause_保持revision 校验 pause 不提升代际
// Python: test_pause_keeps_revision_so_in_flight_assessment_can_commit (L562)
func TestGoalManager_Pause_保持revision(t *testing.T) {
	harness := newManagerHarness(true)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	work := harness.events.NextWork()
	require.NotNil(t, work)
	harness.events.MarkStarted(work)

	paused, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, paused)
	assert.Equal(t, record.Revision, paused.Revision)

	resumed, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resumed)
	assert.Equal(t, record.Revision, resumed.Revision, "在飞轮恢复保持代际")
	assert.Nil(t, harness.events.NextWork(), "在飞轮恢复不重复排队")
}

// TestGoalManager_Resume_在飞轮时间跳变 校验暂停期在飞轮时间被折入
// Python: test_resume_jumps_time_used_after_in_flight_pause (L338)
func TestGoalManager_Resume_在飞轮时间跳变(t *testing.T) {
	clock := freezeGoalClock(t, 1000)
	harness := newManagerHarness(true)
	_, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	work := harness.events.NextWork()
	require.NotNil(t, work)
	harness.events.MarkStarted(work)

	*clock = 1010
	paused, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, paused)
	assert.Equal(t, 10, paused.TimeUsedSeconds)
	require.NotNil(t, paused.ActiveStartedAt)

	*clock = 1040
	resumed, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resumed)
	assert.Equal(t, GoalStatusActive, resumed.Status)
	assert.Equal(t, 40, resumed.TimeUsedSeconds, "10s + 暂停期 30s")
	require.NotNil(t, resumed.ActiveStartedAt)
	assert.Equal(t, "1970-01-01T00:17:20+00:00", *resumed.ActiveStartedAt)
	assert.Equal(t, paused.Revision, resumed.Revision)
	assert.Nil(t, harness.events.NextWork())
}

// TestGoalManager_二次PauseResume_时间仍跳变 校验二次循环
// Python: test_second_pause_resume_while_in_flight_still_jumps_time (L399)
func TestGoalManager_二次PauseResume_时间仍跳变(t *testing.T) {
	clock := freezeGoalClock(t, 1000)
	harness := newManagerHarness(true)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	work := harness.events.NextWork()
	require.NotNil(t, work)
	harness.events.MarkStarted(work)

	*clock = 1010
	firstPause, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, firstPause)
	assert.Equal(t, 10, firstPause.TimeUsedSeconds)

	*clock = 1020
	firstResume, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, firstResume)
	assert.Equal(t, 20, firstResume.TimeUsedSeconds)
	assert.Equal(t, record.Revision, firstResume.Revision)

	*clock = 1030
	secondPause, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, secondPause)
	assert.Equal(t, 30, secondPause.TimeUsedSeconds)
	require.NotNil(t, secondPause.ActiveStartedAt)

	*clock = 1050
	secondResume, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, secondResume)
	assert.Equal(t, 50, secondResume.TimeUsedSeconds)
	assert.Equal(t, record.Revision, secondResume.Revision)
	assert.Nil(t, harness.events.NextWork())
}

// TestGoalManager_空闲Resume_提升revision并保活 校验空闲恢复
// Python: test_idle_resume_bumps_revision_and_ensures_work (L437)
func TestGoalManager_空闲Resume_提升revision并保活(t *testing.T) {
	harness := newManagerHarness(true)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	paused, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, paused)
	assert.Equal(t, record.Revision, paused.Revision)

	resumed, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resumed)
	assert.Equal(t, record.Revision+1, resumed.Revision)

	queued := harness.events.NextWork()
	require.NotNil(t, queued)
	assert.Equal(t, resumed.Revision, queued.Context.Revision)
}

// TestGoalManager_Resume_不计空闲暂停 校验空闲时长不入账
// Python: test_resume_does_not_bill_idle_pause_without_in_flight (L454)
func TestGoalManager_Resume_不计空闲暂停(t *testing.T) {
	clock := freezeGoalClock(t, 1000)
	harness := newManagerHarness(true)
	_, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	*clock = 1030
	paused, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, paused)
	assert.Equal(t, 30, paused.TimeUsedSeconds)

	*clock = 2000
	resumed, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resumed)
	assert.Equal(t, 30, resumed.TimeUsedSeconds, "空闲暂停 970s 不得计入")
	require.NotNil(t, resumed.ActiveStartedAt)
	assert.Equal(t, "1970-01-01T00:33:20+00:00", *resumed.ActiveStartedAt)
}

// ──────────────────────────── Clear ────────────────────────────

// TestGoalManager_Clear_清work取消并发快照 校验清除三件事
// Python: test_clear_removes_goal_work_cancels_active_round_and_emits_snapshot (L513)
func TestGoalManager_Clear_清work取消并发快照(t *testing.T) {
	harness := newManagerHarness(true)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)
	harness.emitted = nil

	cleared, err := harness.manager.Clear(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cleared)
	assert.Equal(t, record.GoalID, cleared.GoalID)
	assert.Nil(t, cleared.ActiveStartedAt, "清除前必须结算时钟")

	assert.Nil(t, harness.events.NextWork(), "待办必须清空")
	require.Len(t, harness.cancels, 1)
	assert.Equal(t, record.GoalID, harness.cancels[0].GoalID)
	assert.Equal(t, "goal_clear", harness.cancels[0].Reason)

	require.Len(t, harness.emitted, 1)
	assert.Equal(t, hschema.InteractionEventTypeGoalUpdated, harness.emitted[0].Type)
	assert.Nil(t, harness.emitted[0].Payload["goal"], "清除事件载荷为 nil")

	got, err := harness.manager.Get(context.Background())
	require.NoError(t, err)
	assert.Nil(t, got, "记录必须被移除")
}

// TestGoalManager_Clear_无目标为noop 校验空记录
// Python: test_pause_and_resume_are_noops_without_a_goal (L167) 的 clear 部分
func TestGoalManager_Clear_无目标为noop(t *testing.T) {
	harness := newManagerHarness(true)

	cleared, err := harness.manager.Clear(context.Background())
	require.NoError(t, err)
	assert.Nil(t, cleared)
	assert.Empty(t, harness.cancels)
	assert.Empty(t, harness.emitted)
}

// TestGoalManager_Clear_无输出流不发事件 校验事件门卫
func TestGoalManager_Clear_无输出流不发事件(t *testing.T) {
	harness := newManagerHarness(false)
	_, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	cleared, err := harness.manager.Clear(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, cleared)
	assert.Empty(t, harness.emitted, "无输出流不得发 goal.updated")
}

// ──────────────────────────── 补排与视图 ────────────────────────────

// TestGoalManager_EnsureActiveGoalWorkLocked 校验补排的三条门卫
// Python: GoalManager.ensure_active_goal_work_locked (manager.py L251)
func TestGoalManager_EnsureActiveGoalWorkLocked(t *testing.T) {
	// 无记录
	empty := newManagerHarness(true)
	assert.False(t, empty.manager.EnsureActiveGoalWorkLocked())

	// PAUSED 不补排
	pausedHarness := newManagerHarness(true)
	pausedRecord := CreateGoalRecord("session-1", "write a report", nil, nil)
	pausedRecord.Status = GoalStatusPaused
	pausedHarness.store.Save(pausedRecord)
	assert.False(t, pausedHarness.manager.EnsureActiveGoalWorkLocked())

	// ACTIVE 但无输出流不补排
	detached := newManagerHarness(false)
	detached.store.Save(CreateGoalRecord("session-1", "write a report", nil, nil))
	assert.False(t, detached.manager.EnsureActiveGoalWorkLocked())
	assert.Nil(t, detached.events.NextWork())

	// ACTIVE 且有输出流补排一次
	attached := newManagerHarness(true)
	active := CreateGoalRecord("session-1", "write a report", nil, nil)
	attached.store.Save(active)
	assert.True(t, attached.manager.EnsureActiveGoalWorkLocked())
	queued := attached.events.NextWork()
	require.NotNil(t, queued)
	assert.Equal(t, active.GoalID, queued.Context.GoalID)
	assert.Equal(t, 1, attached.notifies)

	// 同代际已在队列 → 不重复排队
	assert.False(t, attached.manager.EnsureActiveGoalWorkLocked(), "同 (goal_id, revision) 去重")
}

// TestGoalManager_TaskView 校验记录到视图的转换
func TestGoalManager_TaskView(t *testing.T) {
	maxAttempts := 5
	tokenBudget := 100
	record := CreateGoalRecord("s1", "objective", &maxAttempts, &tokenBudget)
	record.AttemptCount = 2
	record.TokenUsage.TotalTokens = 30
	record.LastAssessment = &GoalAssessment{
		Status:          GoalAssessmentStatusContinue,
		Evidence:        "progress",
		RemainingWork:   strPtr("gap"),
		NextInstruction: strPtr("next step"),
	}

	view := record.TaskView()
	assert.Equal(t, "objective", view.Objective)
	assert.Equal(t, 2, view.AttemptCount)
	require.NotNil(t, view.MaxAttempts)
	assert.Equal(t, 5, *view.MaxAttempts)
	require.NotNil(t, view.TokenBudget)
	assert.Equal(t, 100, *view.TokenBudget)
	assert.Equal(t, 30, view.TotalTokens)
	require.NotNil(t, view.Previous)
	assert.Equal(t, "continue", view.Previous.Status)
	assert.Equal(t, "progress", view.Previous.Evidence)
	assert.Equal(t, "gap", view.Previous.RemainingWork)
	assert.Equal(t, "next step", view.Previous.NextInstruction)

	assert.Nil(t, (&GoalRecord{}).TaskView().Previous)
}

// ──────────────────────────── 尝试记账与评估 ────────────────────────────

// TestGoalManager_BeginAttempt 校验尝试记账与代际校验
// Python: test_attempt_usage_and_completion_are_written_for_current_generation (L532)
func TestGoalManager_BeginAttempt(t *testing.T) {
	harness := newManagerHarness(true)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	started, err := harness.manager.BeginAttempt(context.Background(), record.GoalID, record.Revision)
	require.NoError(t, err)
	require.NotNil(t, started)
	assert.Equal(t, 1, started.AttemptCount)

	// 异代际：返回 nil，不得记账
	stale, err := harness.manager.BeginAttempt(context.Background(), record.GoalID, record.Revision+9)
	require.NoError(t, err)
	assert.Nil(t, stale)

	// 异目标：返回 nil
	other, err := harness.manager.BeginAttempt(context.Background(), "other-goal", record.Revision)
	require.NoError(t, err)
	assert.Nil(t, other)

	// 非 ACTIVE：返回 nil
	_, err = harness.manager.Pause(context.Background())
	require.NoError(t, err)
	paused, err := harness.manager.BeginAttempt(context.Background(), record.GoalID, record.Revision)
	require.NoError(t, err)
	assert.Nil(t, paused, "PAUSED 代际不得开始新尝试")
}

// TestGoalManager_写操作立即提交 校验 set/pause/resume/begin_attempt 各提交一次
// Python: test_goal_writes_are_committed_immediately (L127)
func TestGoalManager_写操作立即提交(t *testing.T) {
	harness := newManagerHarness(false)

	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, harness.session.commits)

	_, err = harness.manager.Pause(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, harness.session.commits)

	resumed, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resumed)
	assert.Equal(t, 3, harness.session.commits)

	_, err = harness.manager.BeginAttempt(context.Background(), record.GoalID, resumed.Revision)
	require.NoError(t, err)
	assert.Equal(t, 4, harness.session.commits)
}

// TestGoalManager_ActiveUsage_冲刷已流逝时间 校验累 token 时结算工时
// Python: test_active_usage_accounting_flushes_elapsed_time (L478)
func TestGoalManager_ActiveUsage_冲刷已流逝时间(t *testing.T) {
	clock := freezeGoalClock(t, 1000)
	harness := newManagerHarness(false)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)
	_, err = harness.manager.BeginAttempt(context.Background(), record.GoalID, record.Revision)
	require.NoError(t, err)

	*clock = 1012
	require.NoError(t, harness.manager.AccumulateUsage(record.GoalID, record.Revision, 5, 0, 0))
	got, err := harness.manager.Get(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 12, got.TimeUsedSeconds)
	require.NotNil(t, got.ActiveStartedAt)
	assert.Equal(t, "1970-01-01T00:16:52+00:00", *got.ActiveStartedAt)

	*clock = 1020
	require.NoError(t, harness.manager.AccumulateUsage(record.GoalID, record.Revision, 0, 7, 0))
	got, err = harness.manager.Get(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 20, got.TimeUsedSeconds)
	assert.Equal(t, 12, got.TokenUsage.TotalTokens)

	// 异代际不计账
	require.NoError(t, harness.manager.AccumulateUsage(record.GoalID, record.Revision+9, 100, 100, 0))
	got, err = harness.manager.Get(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 12, got.TokenUsage.TotalTokens)
}

// TestGoalManager_UsageDuringPause 校验 PAUSED 在飞轮仍可写用量
// Python: test_pause_keeps_timing_while_in_flight_attempt_finishes (L263) 的中间段
func TestGoalManager_UsageDuringPause(t *testing.T) {
	clock := freezeGoalClock(t, 1000)
	harness := newManagerHarness(true)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	work := harness.events.NextWork()
	require.NotNil(t, work)
	harness.events.MarkStarted(work)

	*clock = 1007
	paused, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, paused)
	assert.Equal(t, 7, paused.TimeUsedSeconds)
	require.NotNil(t, paused.ActiveStartedAt)

	*clock = 1020
	require.NoError(t, harness.manager.AccumulateUsage(record.GoalID, record.Revision, 3, 0, 0))
	mid, err := harness.manager.Get(context.Background())
	require.NoError(t, err)
	require.NotNil(t, mid)
	assert.Equal(t, 20, mid.TimeUsedSeconds)
	require.NotNil(t, mid.ActiveStartedAt)
	assert.Equal(t, "1970-01-01T00:17:00+00:00", *mid.ActiveStartedAt)
	assert.Equal(t, 3, mid.TokenUsage.TotalTokens)

	*clock = 1025
	continued, err := harness.manager.ApplyAssessment(
		context.Background(), record.GoalID, record.Revision,
		GoalAssessment{Status: GoalAssessmentStatusContinue, Evidence: "still working"},
	)
	require.NoError(t, err)
	require.NotNil(t, continued)
	assert.Equal(t, GoalStatusPaused, continued.Status)
	assert.Equal(t, 25, continued.TimeUsedSeconds)
	assert.Nil(t, continued.ActiveStartedAt, "PAUSED 收尾必须关时钟")

	*clock = 1100
	resumed, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resumed)
	assert.Equal(t, GoalStatusActive, resumed.Status)
	assert.Equal(t, 25, resumed.TimeUsedSeconds, "空闲暂停 75s 不计入")
	require.NotNil(t, resumed.ActiveStartedAt)
	assert.Equal(t, "1970-01-01T00:18:20+00:00", *resumed.ActiveStartedAt)
}

// TestGoalManager_时间累计 校验 set/pause/resume/begin/complete 全链路工时
// Python: test_pause_resume_and_completion_preserve_accumulated_time (L215)
func TestGoalManager_时间累计(t *testing.T) {
	clock := freezeGoalClock(t, 1000)
	harness := newManagerHarness(true)

	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)
	assert.Equal(t, 0, record.TimeUsedSeconds)
	require.NotNil(t, record.ActiveStartedAt)
	assert.Equal(t, "1970-01-01T00:16:40+00:00", *record.ActiveStartedAt)

	*clock = 1030
	paused, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, paused)
	assert.Equal(t, 30, paused.TimeUsedSeconds)
	assert.Nil(t, paused.ActiveStartedAt)

	*clock = 2000
	resumed, err := harness.manager.Resume(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resumed)
	assert.Equal(t, 30, resumed.TimeUsedSeconds)
	require.NotNil(t, resumed.ActiveStartedAt)
	assert.Equal(t, "1970-01-01T00:33:20+00:00", *resumed.ActiveStartedAt)

	started, err := harness.manager.BeginAttempt(context.Background(), record.GoalID, resumed.Revision)
	require.NoError(t, err)
	require.NotNil(t, started)

	*clock = 2045
	completed, err := harness.manager.ApplyAssessment(
		context.Background(), record.GoalID, resumed.Revision,
		GoalAssessment{Status: GoalAssessmentStatusComplete, Evidence: "all checks passed"},
	)
	require.NoError(t, err)
	require.NotNil(t, completed)
	assert.Equal(t, GoalStatusCompleted, completed.Status)
	assert.Equal(t, 75, completed.TimeUsedSeconds)
	assert.Nil(t, completed.ActiveStartedAt)
}

// TestGoalManager_按代际写入 校验用量与终态写回
// Python: test_attempt_usage_and_completion_are_written_for_current_generation (L532)
func TestGoalManager_按代际写入(t *testing.T) {
	harness := newManagerHarness(true)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)

	started, err := harness.manager.BeginAttempt(context.Background(), record.GoalID, record.Revision)
	require.NoError(t, err)
	require.NotNil(t, started)
	assert.Equal(t, 1, started.AttemptCount)

	require.NoError(t, harness.manager.AccumulateUsage(record.GoalID, record.Revision, 5, 7, 0))
	completed, err := harness.manager.ApplyAssessment(
		context.Background(), record.GoalID, record.Revision,
		GoalAssessment{Status: GoalAssessmentStatusComplete, Evidence: "all checks passed"},
	)
	require.NoError(t, err)
	require.NotNil(t, completed)
	assert.Equal(t, GoalStatusCompleted, completed.Status)
	assert.Equal(t, 12, completed.TokenUsage.TotalTokens)
	require.NotNil(t, completed.LastStopReason)
	assert.Equal(t, "completed", *completed.LastStopReason)
	assert.Equal(t, 3, harness.session.commits, "set + begin_attempt + apply_assessment 各提交一次")
}

// TestGoalManager_PauseThenComplete_覆盖PAUSED 校验 COMPLETE 覆盖暂停态
// Python: test_pause_then_complete_assessment_overrides_paused (L591)
func TestGoalManager_PauseThenComplete_覆盖PAUSED(t *testing.T) {
	harness := newManagerHarness(true)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)
	started, err := harness.manager.BeginAttempt(context.Background(), record.GoalID, record.Revision)
	require.NoError(t, err)
	require.NotNil(t, started)

	paused, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, paused)
	assert.Equal(t, GoalStatusPaused, paused.Status)

	completed, err := harness.manager.ApplyAssessment(
		context.Background(), record.GoalID, record.Revision,
		GoalAssessment{Status: GoalAssessmentStatusComplete, Evidence: "done while paused"},
	)
	require.NoError(t, err)
	require.NotNil(t, completed)
	assert.Equal(t, GoalStatusCompleted, completed.Status, "COMPLETE 必须覆盖 PAUSED")
	require.NotNil(t, completed.LastStopReason)
	assert.Equal(t, "completed", *completed.LastStopReason)
}

// TestGoalManager_CONTINUE在PAUSED不续跑 校验暂停期间不重排
// Python: test_pause_keeps_revision_so_in_flight_assessment_can_commit (L562)
func TestGoalManager_CONTINUE在PAUSED不续跑(t *testing.T) {
	harness := newManagerHarness(true)
	record, err := harness.manager.Set(context.Background(), "write a report", SetOptions{})
	require.NoError(t, err)
	started, err := harness.manager.BeginAttempt(context.Background(), record.GoalID, record.Revision)
	require.NoError(t, err)
	require.NotNil(t, started)

	paused, err := harness.manager.Pause(context.Background())
	require.NoError(t, err)
	require.NotNil(t, paused)

	continued, err := harness.manager.ApplyAssessment(
		context.Background(), record.GoalID, record.Revision,
		GoalAssessment{Status: GoalAssessmentStatusContinue, Evidence: "partial progress"},
	)
	require.NoError(t, err)
	require.NotNil(t, continued)
	assert.Equal(t, GoalStatusPaused, continued.Status)
	require.NotNil(t, continued.LastAssessment)
	assert.Equal(t, "partial progress", continued.LastAssessment.Evidence)
	assert.Nil(t, harness.events.NextWork(), "PAUSED 下 CONTINUE 不得重排")
}

// TestGoalManager_实现Coordinator接口 编译期与运行期双重校验
func TestGoalManager_实现Coordinator接口(t *testing.T) {
	var coordinator Coordinator = NewGoalManager(GoalManagerConfig{
		Store:        NewSessionGoalStore(newFakeGoalSession("s1")),
		ControlLock:  &sync.Mutex{},
		EventManager: task_loop.NewEventManager(),
	})
	assert.False(t, coordinator.HasActiveRecord())
	assert.False(t, coordinator.EnsureActiveGoalWorkLocked())

	record, err := coordinator.BeginAttempt(context.Background(), "missing", 0)
	require.NoError(t, err)
	assert.Nil(t, record)
}

// TestGoalManager_并发写持同一把锁 竞争与死锁双重校验（需 -race）
func TestGoalManager_并发写持同一把锁(t *testing.T) {
	var notifies atomic.Int64
	var emitted atomic.Int64
	manager := NewGoalManager(GoalManagerConfig{
		Store:             NewSessionGoalStore(newFakeGoalSession("session-1")),
		EventManager:      task_loop.NewEventManager(),
		ControlLock:       &sync.Mutex{},
		HasOutputStream:   func() bool { return true },
		CancelActiveRound: func(context.Context, hschema.RoundWorkKind, string, string) {},
		EmitEvent:         func(hschema.InteractionEvent) { emitted.Add(1) },
		NotifyWork:        func() { notifies.Add(1) },
	})

	var waitGroup sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for round := 0; round < 20; round++ {
				record, err := manager.Set(
					context.Background(), "objective", SetOptions{OverwriteConfirmed: true},
				)
				if err != nil || record == nil {
					continue
				}
				_, _ = manager.BeginAttempt(context.Background(), record.GoalID, record.Revision)
				_ = manager.AccumulateUsage(record.GoalID, record.Revision, 1, 1, 0)
				_, _ = manager.ApplyAssessment(
					context.Background(), record.GoalID, record.Revision,
					GoalAssessment{Status: GoalAssessmentStatusContinue, Evidence: "progress"},
				)
				_, _ = manager.Pause(context.Background())
				_, _ = manager.Resume(context.Background())
				_ = manager.Peek()
				manager.HasActiveRecord()
				_, _ = manager.Get(context.Background())
			}
		}()
	}
	waitGroup.Wait()

	assert.Greater(t, emitted.Load(), int64(0))
	assert.Greater(t, notifies.Load(), int64(0))
}
