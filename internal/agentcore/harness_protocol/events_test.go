package harness_protocol

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEvents枚举_值集与成员数 逐字核对 11 个枚举的全部取值与成员数。
// Python: events.py:31-139（TurnEventKind/OutputKind/OutputChannel/OutputOperation/
// UsageUpdateMode/EventRetention/EventOverflowPolicy/ItemEventKind/HookEventPhase/
// DiagnosticLevel/ModelRequestStatus）
func TestEvents枚举_值集与成员数(t *testing.T) {
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "TurnEventKind",
			got: []string{
				string(TurnEventKindStarted), string(TurnEventKindPaused), string(TurnEventKindResumed),
				string(TurnEventKindFinished), string(TurnEventKindAborted), string(TurnEventKindFailed),
			},
			want: []string{"started", "paused", "resumed", "finished", "aborted", "failed"},
		},
		{
			name: "OutputKind",
			got:  []string{string(OutputKindText), string(OutputKindStructured)},
			want: []string{"text", "structured"},
		},
		{
			name: "OutputChannel",
			got:  []string{string(OutputChannelAnswer), string(OutputChannelReasoning), string(OutputChannelSystem)},
			want: []string{"answer", "reasoning", "system"},
		},
		{
			name: "OutputOperation",
			got:  []string{string(OutputOperationDelta), string(OutputOperationSnapshot), string(OutputOperationFinal)},
			want: []string{"delta", "snapshot", "final"},
		},
		{
			name: "UsageUpdateMode",
			got:  []string{string(UsageUpdateModeDelta), string(UsageUpdateModeCumulative)},
			want: []string{"delta", "cumulative"},
		},
		{
			name: "EventRetention",
			got:  []string{string(EventRetentionRequired), string(EventRetentionCoalescible), string(EventRetentionBestEffort)},
			want: []string{"required", "coalescible", "best_effort"},
		},
		{
			name: "EventOverflowPolicy",
			got: []string{
				string(EventOverflowPolicyBlock), string(EventOverflowPolicyCoalesceOrBlock),
				string(EventOverflowPolicyDropBestEffortOrBlock),
			},
			want: []string{"block", "coalesce_or_block", "drop_best_effort_or_block"},
		},
		{
			name: "ItemEventKind",
			got:  []string{string(ItemEventKindStarted), string(ItemEventKindUpdated), string(ItemEventKindCompleted)},
			want: []string{"started", "updated", "completed"},
		},
		{
			name: "HookEventPhase",
			got:  []string{string(HookEventPhaseStarted), string(HookEventPhaseFinished)},
			want: []string{"started", "finished"},
		},
		{
			name: "DiagnosticLevel",
			got: []string{
				string(DiagnosticLevelDebug), string(DiagnosticLevelInfo),
				string(DiagnosticLevelWarning), string(DiagnosticLevelError),
			},
			want: []string{"debug", "info", "warning", "error"},
		},
		{
			name: "ModelRequestStatus",
			got: []string{
				string(ModelRequestStatusCompleted), string(ModelRequestStatusFailed), string(ModelRequestStatusCancelled),
			},
			want: []string{"completed", "failed", "cancelled"},
		},
	}
	// 11 个枚举、成员合计 34（AST 机器核对：6+2+3+3+2+3+3+3+2+4+3）
	assert.Len(t, cases, 11)
	total := 0
	for _, c := range cases {
		total += len(c.want)
		for i := range c.want {
			assert.Equal(t, c.want[i], c.got[i], "%s[%d]", c.name, i)
		}
	}
	assert.Equal(t, 34, total)
}

// TestTerminalTurnEventKinds_三值 验证终态集合与判定函数。
// Python: events.py:42-48 TERMINAL_TURN_EVENT_KINDS = {FINISHED, ABORTED, FAILED}
func TestTerminalTurnEventKinds_三值(t *testing.T) {
	assert.Len(t, TerminalTurnEventKinds, 3)
	assert.True(t, IsTerminalTurnEventKind(TurnEventKindFinished))
	assert.True(t, IsTerminalTurnEventKind(TurnEventKindAborted))
	assert.True(t, IsTerminalTurnEventKind(TurnEventKindFailed))
	assert.False(t, IsTerminalTurnEventKind(TurnEventKindStarted))
	assert.False(t, IsTerminalTurnEventKind(TurnEventKindPaused))
	assert.False(t, IsTerminalTurnEventKind(TurnEventKindResumed))
	assert.False(t, IsTerminalTurnEventKind(TurnEventKind("nope")))
}

// 上面对 events.go 的构造器尚未实现，故本文件先只覆盖枚举；
// 后续任务在同文件追加构造器用例（math 的引用在 Task 2 起生效）。
var _ = math.NaN
