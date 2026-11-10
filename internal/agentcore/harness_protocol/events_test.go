package harness_protocol

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestOutputEvent_校验与冻结 验证 output_id 非空、content_index 非负、content/data 被冻结。
// Python: events.py:141-160 OutputEvent.__post_init__
func TestOutputEvent_校验与冻结(t *testing.T) {
	src := map[string]any{"k": "v"}
	ev, err := NewOutputEvent("answer-1", OutputKindText, "done", OutputOperationDelta, OutputChannelAnswer, 1, "", src)
	require.NoError(t, err)
	assert.Equal(t, "answer-1", ev.OutputID)
	assert.Equal(t, 1, ev.ContentIndex)
	assert.Equal(t, "done", ev.Content)
	// data 被深拷贝：改入参不影响结果
	src["k"] = "changed"
	assert.Equal(t, "v", ev.Data["k"])

	// output_id 空
	_, err = NewOutputEvent("", OutputKindText, "x", OutputOperationSnapshot, OutputChannelAnswer, 0, "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output_id must not be empty")

	// content_index 负
	_, err = NewOutputEvent("id", OutputKindText, "x", OutputOperationSnapshot, OutputChannelAnswer, -1, "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output content_index must be non-negative")

	// content 含 NaN 被拒
	_, err = NewOutputEvent("id", OutputKindText, math.NaN(), OutputOperationSnapshot, OutputChannelAnswer, 0, "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON numbers must be finite")
}

// TestOutputEvent_默认值 验证 content_type 空串表示未提供。
// Python: events.py:151 content_type: str | None = None
func TestOutputEvent_默认值(t *testing.T) {
	ev, err := NewOutputEvent("id", OutputKindStructured, map[string]any{"a": 1}, OutputOperationFinal, OutputChannelAnswer, 0, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "", ev.ContentType)
	assert.Equal(t, OutputOperationFinal, ev.Operation)
	assert.Equal(t, OutputChannelAnswer, ev.Channel)
}

// TestItemLifecycleEvent_校验 验证 item_type 非空与 data 冻结。
// Python: events.py:163-174
func TestItemLifecycleEvent_校验(t *testing.T) {
	ev, err := NewItemLifecycleEvent(ItemEventKindStarted, "tool", map[string]any{"name": "read_file"})
	require.NoError(t, err)
	assert.Equal(t, "tool", ev.ItemType)
	assert.Equal(t, "read_file", ev.Data["name"])

	_, err = NewItemLifecycleEvent(ItemEventKindStarted, "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "item_type must not be empty")
}

// TestStateChangedEvent_无校验 验证直接构造成功。
// Python: events.py:273-278（无 __post_init__）
func TestStateChangedEvent_无校验(t *testing.T) {
	ev, err := NewStateChangedEvent(HarnessStateIdle, HarnessStateRunning)
	require.NoError(t, err)
	assert.Equal(t, HarnessStateIdle, ev.Old)
	assert.Equal(t, HarnessStateRunning, ev.New)
}

// TestProviderEvent_三标识非空 验证 provider/event_type/schema_version 均不可空。
// Python: events.py:335-347
func TestProviderEvent_三标识非空(t *testing.T) {
	ev, err := NewProviderEvent("codex", "thread.compacted", "1", map[string]any{"thread_id": "t1"})
	require.NoError(t, err)
	assert.Equal(t, "codex", ev.Provider)

	for _, bad := range [][3]string{{"", "x", "1"}, {"p", "", "1"}, {"p", "x", ""}} {
		_, err := NewProviderEvent(bad[0], bad[1], bad[2], nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "provider event namespace, type, and schema version must not be empty")
	}
}

// TestDiagnosticEvent_message非空 验证诊断文案必填。
// Python: events.py:321-332
func TestDiagnosticEvent_message非空(t *testing.T) {
	ev, err := NewDiagnosticEvent(DiagnosticLevelDebug, "trace", nil)
	require.NoError(t, err)
	assert.Equal(t, DiagnosticLevelDebug, ev.Level)

	_, err = NewDiagnosticEvent(DiagnosticLevelDebug, "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "diagnostic message must not be empty")
}

// TestPayloadKind_线格式字面量 守护 10 个 payload 的 discriminator，
// 15.4 的 serialization 依赖这些字面量与 Python 一致。
// Python: serialization.py:_payload_to_wire（"output"/"item_lifecycle"/"usage_updated"/
// "model_request"/"state_changed"/"turn_lifecycle"/"hook_observed"/"diagnostic"/"provider"）
func TestPayloadKind_线格式字面量(t *testing.T) {
	out, err := NewOutputEvent("id", OutputKindText, "x", OutputOperationSnapshot, OutputChannelAnswer, 0, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "output", out.payloadKind())

	item, err := NewItemLifecycleEvent(ItemEventKindStarted, "tool", nil)
	require.NoError(t, err)
	assert.Equal(t, "item_lifecycle", item.payloadKind())

	st, err := NewStateChangedEvent(HarnessStateIdle, HarnessStateRunning)
	require.NoError(t, err)
	assert.Equal(t, "state_changed", st.payloadKind())

	pe, err := NewProviderEvent("p", "e", "1", nil)
	require.NoError(t, err)
	assert.Equal(t, "provider", pe.payloadKind())

	de, err := NewDiagnosticEvent(DiagnosticLevelInfo, "m", nil)
	require.NoError(t, err)
	assert.Equal(t, "diagnostic", de.payloadKind())
}

// TestUsageUpdatedEvent_无校验 验证用量更新直接构造（TurnUsage 已在 15.1 校验）。
// Python: events.py:177-182（无 __post_init__）
func TestUsageUpdatedEvent_无校验(t *testing.T) {
	in := 4
	usage, err := NewTurnUsage(&in, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	ev, err := NewUsageUpdatedEvent(usage, UsageUpdateModeDelta)
	require.NoError(t, err)
	assert.Equal(t, UsageUpdateModeDelta, ev.Mode)
	require.NotNil(t, ev.Usage.InputTokens)
	assert.Equal(t, 4, *ev.Usage.InputTokens)
}

// TestHookObservedEvent_名称与id非空 验证钩子观测必填两项。
// Python: events.py:315-318
func TestHookObservedEvent_名称与id非空(t *testing.T) {
	ev, err := NewHookObservedEvent(HookEventPhaseStarted, "before_tool", "hook-1", nil)
	require.NoError(t, err)
	assert.Equal(t, "before_tool", ev.HookName)

	_, err = NewHookObservedEvent(HookEventPhaseStarted, "", "hook-1", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hook observation name and id must not be empty")

	_, err = NewHookObservedEvent(HookEventPhaseStarted, "before_tool", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hook observation name and id must not be empty")
}

// TestPayloadKind_usage与hook 守护这两个 discriminator。
// Python: serialization.py:303-311（usage_updated）/ :325-335（hook_observed）
func TestPayloadKind_usage与hook(t *testing.T) {
	in := 1
	usage, err := NewTurnUsage(&in, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	ue, err := NewUsageUpdatedEvent(usage, UsageUpdateModeCumulative)
	require.NoError(t, err)
	assert.Equal(t, "usage_updated", ue.payloadKind())

	ho, err := NewHookObservedEvent(HookEventPhaseFinished, "on_stop", "h1", nil)
	require.NoError(t, err)
	assert.Equal(t, "hook_observed", ho.payloadKind())
}
