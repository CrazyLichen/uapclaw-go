package goal

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
)

// fakeGoalStore 测试用 store，Load 返回预设记录。
type fakeGoalStore struct {
	record *goal.GoalRecord
}

func (f *fakeGoalStore) SessionID() string              { return "sess-1" }
func (f *fakeGoalStore) Load() *goal.GoalRecord         { return f.record }
func (f *fakeGoalStore) Save(_ *goal.GoalRecord)        {}
func (f *fakeGoalStore) Clear()                         {}
func (f *fakeGoalStore) Commit(_ context.Context) error { return nil }

// newTestGoalManager 构造一个绑定了 fakeStore 的 GoalManager。
func newTestGoalManager(record *goal.GoalRecord) *goal.GoalManager {
	return goal.NewGoalManager(goal.GoalManagerConfig{
		Store:             &fakeGoalStore{record: record},
		EventManager:      task_loop.NewEventManager(),
		ControlLock:       &sync.Mutex{},
		HasOutputStream:   func() bool { return true },
		CancelActiveRound: func(_ context.Context, _ hschema.RoundWorkKind, _, _ string) {},
		EmitEvent:         func(_ hschema.InteractionEvent) {},
		NotifyWork:        func() {},
	})
}

// ──────────────────────────── 用例 ────────────────────────────

// TestGoalReportSink_lastWriteWins 验证单次尝试内 last-write-wins + consume 清空。
func TestGoalReportSink_lastWriteWins(t *testing.T) {
	sink := NewGoalReportSink()
	sink.BeginAttempt("s1", "g1", 1, 0)
	sink.Submit(goal.GoalAssessment{Status: goal.GoalAssessmentStatusContinue})
	sink.Submit(goal.GoalAssessment{Status: goal.GoalAssessmentStatusComplete})

	report := sink.Consume()
	assert.NotNil(t, report)
	assert.Equal(t, goal.GoalAssessmentStatusComplete, report.Status)
	assert.Nil(t, sink.Consume(), "consume 后应为空")
}

// TestSubmitGoalReportTool_非法status归一化 验证非法 status 归一化 continue。
func TestSubmitGoalReportTool_非法status归一化(t *testing.T) {
	sink := NewGoalReportSink()
	sink.BeginAttempt("s1", "g1", 1, 0)
	toolInst, err := NewSubmitGoalReportTool(sink, "cn", "agent-1")
	assert.NoError(t, err)

	out, err := toolInst.Invoke(context.Background(), map[string]any{
		"status":           "bogus",
		"evidence":         "some evidence",
		"remaining_work":   "still to do",
		"next_instruction": "do next",
	})
	assert.NoError(t, err)
	assert.Equal(t, "report_accepted", out["result"])
	assert.Equal(t, "continue", out["status"])

	report := sink.Consume()
	assert.NotNil(t, report)
	assert.Equal(t, goal.GoalAssessmentStatusContinue, report.Status)
	assert.Equal(t, "some evidence", report.Evidence)
	assert.NotNil(t, report.RemainingWork)
	assert.Equal(t, "still to do", *report.RemainingWork)
	assert.NotNil(t, report.NextInstruction)
	assert.Equal(t, "do next", *report.NextInstruction)
}

// TestGetCurrentGoalTool_无goal 验证无 goal 时返回 has_goal=false + 双语 message。
func TestGetCurrentGoalTool_无goal(t *testing.T) {
	manager := newTestGoalManager(nil)
	toolInst, err := NewGetCurrentGoalTool(manager, "cn", "agent-1")
	assert.NoError(t, err)

	out, err := toolInst.Invoke(context.Background(), map[string]any{})
	assert.NoError(t, err)
	assert.Equal(t, false, out["has_goal"])
	assert.Equal(t, "当前会话没有设置持续目标。", out["message"])
}

// TestGetCurrentGoalTool_有goal 验证有 goal 时返回摘要字段。
func TestGetCurrentGoalTool_有goal(t *testing.T) {
	record := goal.CreateGoalRecord("sess-1", "构建一个 API", nil, nil)
	manager := newTestGoalManager(record)
	toolInst, err := NewGetCurrentGoalTool(manager, "cn", "agent-1")
	assert.NoError(t, err)

	out, err := toolInst.Invoke(context.Background(), map[string]any{})
	assert.NoError(t, err)
	assert.Equal(t, true, out["has_goal"])
	assert.Equal(t, "构建一个 API", out["objective"])
	assert.Equal(t, record.GoalID, out["goal_id"])
}
