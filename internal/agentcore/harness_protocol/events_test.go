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

// TestTurnLifecycleEvent_终态必须匹配结果 逐格验证 3×3 状态匹配表。
// Python: events.py:288-303 TurnLifecycleEvent.__post_init__
func TestTurnLifecycleEvent_终态必须匹配结果(t *testing.T) {
	completed, err := NewTurnResult(TurnStatusCompleted, ResultOptions{FinalOutput: "done"})
	require.NoError(t, err)
	interruptedTerm := TurnTermination{Kind: TurnTerminationKindUserAbort}
	interrupted, err := NewTurnResult(TurnStatusInterrupted, ResultOptions{Termination: &interruptedTerm})
	require.NoError(t, err)
	turnErr := TurnError{Message: "provider failed", Code: strPtr("provider_error")}
	failed, err := NewTurnResult(TurnStatusFailed, ResultOptions{Error: &turnErr})
	require.NoError(t, err)

	// 匹配：finished→completed / aborted→interrupted / failed→failed
	for _, ok := range []struct {
		kind   TurnEventKind
		result TurnResult
	}{
		{TurnEventKindFinished, completed},
		{TurnEventKindAborted, interrupted},
		{TurnEventKindFailed, failed},
	} {
		ev, err := NewTurnLifecycleEvent(ok.kind, &ok.result)
		require.NoError(t, err, "kind=%s", ok.kind)
		require.NotNil(t, ev.Result)
		assert.Equal(t, ok.kind, ev.Kind)
	}

	// 不匹配：finished←interrupted
	_, err = NewTurnLifecycleEvent(TurnEventKindFinished, &interrupted)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "finished turn event requires completed result")

	// 不匹配：aborted←completed
	_, err = NewTurnLifecycleEvent(TurnEventKindAborted, &completed)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aborted turn event requires interrupted result")

	// 不匹配：failed←completed
	_, err = NewTurnLifecycleEvent(TurnEventKindFailed, &completed)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed turn event requires failed result")
}

// TestTurnLifecycleEvent_非终态禁带结果 验证 started/paused/resumed 不得带 result。
// Python: events.py:289-292
func TestTurnLifecycleEvent_非终态禁带结果(t *testing.T) {
	completed, err := NewTurnResult(TurnStatusCompleted, ResultOptions{})
	require.NoError(t, err)
	for _, kind := range []TurnEventKind{
		TurnEventKindStarted, TurnEventKindPaused, TurnEventKindResumed,
	} {
		// 不带 result：通过
		ev, err := NewTurnLifecycleEvent(kind, nil)
		require.NoError(t, err, "kind=%s", kind)
		assert.Nil(t, ev.Result)

		// 带 result：拒绝
		_, err = NewTurnLifecycleEvent(kind, &completed)
		require.Error(t, err, "kind=%s", kind)
		assert.Contains(t, err.Error(), "non-terminal "+string(kind)+" turn event must not contain a result")
	}
}

// TestTurnLifecycleEvent_终态必须带结果 验证终态缺 result 被拒。
// Python: events.py:294-295
func TestTurnLifecycleEvent_终态必须带结果(t *testing.T) {
	for _, kind := range []TurnEventKind{
		TurnEventKindFinished, TurnEventKindAborted, TurnEventKindFailed,
	} {
		_, err := NewTurnLifecycleEvent(kind, nil)
		require.Error(t, err, "kind=%s", kind)
		assert.Contains(t, err.Error(), "terminal turn event requires a result")
	}
}

// TestPayloadKind_turnLifecycle 守护 discriminator。
// Python: serialization.py:316-324（→ "turn_lifecycle"）
func TestPayloadKind_turnLifecycle(t *testing.T) {
	ev, err := NewTurnLifecycleEvent(TurnEventKindStarted, nil)
	require.NoError(t, err)
	assert.Equal(t, "turn_lifecycle", ev.payloadKind())
}

// TestModelRequestEvent_19字段与口径 验证字段齐备与默认值。
// Python: events.py:185-271 ModelRequestEvent（AST 机器核对 19 字段）
func TestModelRequestEvent_19字段与口径(t *testing.T) {
	usage, err := NewTurnUsage(intPtr(12), intPtr(3), nil, nil, intPtr(15), nil)
	require.NoError(t, err)
	outMsg, err := NewTurnMessage("m2", MessageRoleAssistant, nil, nil)
	require.NoError(t, err)
	cost, err := NewMonetaryAmount(1500, "usd")
	require.NoError(t, err)

	ev, err := NewModelRequestEvent(ModelRequestOptions{
		RequestID:         "req-1",
		Status:            ModelRequestStatusCompleted,
		StartedAt:         10.0,
		EndedAt:           12.5,
		Model:             "qwen-max",
		ProviderName:      "dashscope",
		InputObserved:     true,
		OutputMessage:     &outMsg,
		ToolDefinitions:   []any{map[string]any{"name": "read_file"}},
		RequestParameters: map[string]any{"temperature": 0.2},
		ResponseID:        "resp-1",
		TimeToFirstChunk:  floatPtr(0.35),
		FinishReasons:     []string{"stop"},
		Usage:             &usage,
		Cost:              &cost,
	})
	require.NoError(t, err)
	assert.Equal(t, "req-1", ev.RequestID)
	assert.True(t, ev.InputObserved)
	require.NotNil(t, ev.TimeToFirstChunk)
	assert.Equal(t, 0.35, *ev.TimeToFirstChunk)
	assert.Equal(t, []string{"stop"}, ev.FinishReasons)
	require.NotNil(t, ev.Cost)
	assert.Equal(t, int64(1500), ev.Cost.Micros)
	// 19 个字段全部可达（编译期保证，此处抽验 3 个"易漏"字段）
	assert.Equal(t, "dashscope", ev.ProviderName)
	assert.Equal(t, "resp-1", ev.ResponseID)
	assert.Equal(t, "qwen-max", ev.Model)
}

// TestModelRequestEvent_校验顺序 逐条验证 7 步校验。
// Python: events.py:252-270
func TestModelRequestEvent_校验顺序(t *testing.T) {
	base := func() ModelRequestOptions {
		return ModelRequestOptions{
			RequestID: "req-1",
			Status:    ModelRequestStatusCompleted,
			StartedAt: 1.0,
			EndedAt:   2.0,
		}
	}

	// ① request_id 空
	o := base()
	o.RequestID = ""
	_, err := NewModelRequestEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model request_id must not be empty")

	// ② started_at 非有限
	o = base()
	o.StartedAt = math.NaN()
	_, err = NewModelRequestEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model request started_at must be a finite Unix timestamp")

	// ② ended_at 为负
	o = base()
	o.EndedAt = -1
	_, err = NewModelRequestEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model request ended_at must be a finite Unix timestamp")

	// ③ ended_at 早于 started_at
	o = base()
	o.EndedAt = 0.5
	_, err = NewModelRequestEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model request ended_at must not precede started_at")

	// ④ completed 不得带 error
	o = base()
	te := TurnError{Message: "boom"}
	o.Error = &te
	_, err = NewModelRequestEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "completed model request must not contain error")

	// ⑤ time_to_first_chunk 非负有限
	o = base()
	o.TimeToFirstChunk = floatPtr(-0.1)
	_, err = NewModelRequestEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model request time_to_first_chunk must be a non-negative duration")

	o = base()
	o.TimeToFirstChunk = floatPtr(math.Inf(1))
	_, err = NewModelRequestEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model request time_to_first_chunk must be a non-negative duration")
}

// TestModelRequestEvent_冻结与拷贝 验证 5 处深拷贝/冻结。
// Python: events.py:265-270（4 处 tuple + 3 处 freeze）
func TestModelRequestEvent_冻结与拷贝(t *testing.T) {
	finish := []string{"stop"}
	params := map[string]any{"temperature": 0.2}
	ev, err := NewModelRequestEvent(ModelRequestOptions{
		RequestID:         "req-1",
		Status:            ModelRequestStatusCompleted,
		StartedAt:         1.0,
		EndedAt:           2.0,
		FinishReasons:     finish,
		RequestParameters: params,
		ToolDefinitions:   map[string]any{"t": 1},
	})
	require.NoError(t, err)

	finish[0] = "mutated"
	params["temperature"] = 9.9
	assert.Equal(t, []string{"stop"}, ev.FinishReasons)
	assert.Equal(t, 0.2, ev.RequestParameters["temperature"])
}

// TestModelRequestEvent_失败状态可带错误 验证 failed 分支合法。
// Python: events.py:260-261（仅 completed 禁带 error）
func TestModelRequestEvent_失败状态可带错误(t *testing.T) {
	te := TurnError{Message: "auth", Code: strPtr("auth_required")}
	ev, err := NewModelRequestEvent(ModelRequestOptions{
		RequestID: "req-2",
		Status:    ModelRequestStatusFailed,
		StartedAt: 1.0,
		EndedAt:   1.5,
		Error:     &te,
	})
	require.NoError(t, err)
	require.NotNil(t, ev.Error)
	assert.Equal(t, "auth", ev.Error.Message)
}

// TestPayloadKind_modelRequest 守护 discriminator。
// Python: serialization.py:312-313（→ "model_request"）
func TestPayloadKind_modelRequest(t *testing.T) {
	ev, err := NewModelRequestEvent(ModelRequestOptions{
		RequestID: "r", Status: ModelRequestStatusCompleted, StartedAt: 1, EndedAt: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, "model_request", ev.payloadKind())
}

// intPtr / floatPtr 测试助手。
func intPtr(i int) *int           { return &i }
func floatPtr(f float64) *float64 { return &f }

// TestHarnessEvent_信封关联字段 验证 10 字段齐备与关联语义。
// Python: events.py:392-426 HarnessEvent（AST 机器核对 10 字段）
// 上游用例: test_protocol.py:576 test_event_envelope_carries_neutral_output_and_correlation
func TestHarnessEvent_信封关联字段(t *testing.T) {
	payload, err := NewOutputEvent(
		"answer-1", OutputKindText, "done", OutputOperationDelta, OutputChannelAnswer, 1, "", nil,
	)
	require.NoError(t, err)
	ev, err := NewHarnessEvent(HarnessEventOptions{
		Sequence:          1,
		Timestamp:         1.5,
		Event:             &payload,
		HostSessionID:     "team-session-1",
		AgentID:           "team-a_member-a",
		ProviderSessionID: "session-1",
		TurnID:            "turn-1",
		CorrelationID:     "message-1",
		CausationIDs:      []string{"message-1", "steer-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, ev.Sequence)
	assert.Equal(t, 1.5, ev.Timestamp)
	assert.Equal(t, "turn-1", ev.TurnID)
	assert.Equal(t, "message-1", ev.CorrelationID)
	assert.Equal(t, []string{"message-1", "steer-1"}, ev.CausationIDs)
	assert.Equal(t, "output", ev.Event.payloadKind())
}

// TestHarnessEvent_校验九步 逐条验证 9 步校验。
// Python: events.py:407-426
// 上游用例: test_protocol.py:859 test_event_scope_time_and_causation_are_validated
func TestHarnessEvent_校验九步(t *testing.T) {
	p, err := NewStateChangedEvent(HarnessStateIdle, HarnessStateRunning)
	require.NoError(t, err)
	base := func() HarnessEventOptions {
		return HarnessEventOptions{
			Sequence:      1,
			Timestamp:     1.0,
			Event:         &p,
			HostSessionID: "s1",
			AgentID:       "a1",
		}
	}

	// ① sequence 为负
	o := base()
	o.Sequence = -1
	_, err = NewHarnessEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event sequence must be non-negative")

	// ② timestamp 非有限
	o = base()
	o.Timestamp = math.NaN()
	_, err = NewHarnessEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event timestamp must be a finite Unix timestamp")

	// ② timestamp 为负
	o = base()
	o.Timestamp = -0.5
	_, err = NewHarnessEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event timestamp must be a finite Unix timestamp")

	// ③ host_session_id 空
	o = base()
	o.HostSessionID = ""
	_, err = NewHarnessEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event host_session_id and agent_id must not be empty")

	// ③ agent_id 空
	o = base()
	o.AgentID = ""
	_, err = NewHarnessEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event host_session_id and agent_id must not be empty")

	// ④ optional ID 为空串 == 未提供，合法（Q4 用户决策）
	o = base()
	o.ProviderSessionID = ""
	o.TurnID = ""
	o.ItemID = ""
	o.CorrelationID = ""
	_, err = NewHarnessEvent(o)
	require.NoError(t, err)

	// ⑤ causation_ids 含空串
	o = base()
	o.CausationIDs = []string{"c1", ""}
	_, err = NewHarnessEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event causation_ids must not contain empty values")

	// ⑥ causation_ids 重复
	o = base()
	o.CausationIDs = []string{"c1", "c1"}
	_, err = NewHarnessEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event causation_ids must not contain duplicates")

	// ⑦ Event 为 nil
	o = base()
	o.Event = nil
	_, err = NewHarnessEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event payload must not be nil")

	// ⑧ turn_lifecycle 缺 turn_id
	started, err2 := NewTurnLifecycleEvent(TurnEventKindStarted, nil)
	require.NoError(t, err2)
	o = base()
	o.Event = &started
	o.TurnID = ""
	_, err = NewHarnessEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "turn lifecycle event requires turn_id")

	// ⑨ model_request 缺 turn_id
	mr, err3 := NewModelRequestEvent(ModelRequestOptions{
		RequestID: "r", Status: ModelRequestStatusCompleted, StartedAt: 1, EndedAt: 2,
	})
	require.NoError(t, err3)
	o = base()
	o.Event = &mr
	o.TurnID = ""
	_, err = NewHarnessEvent(o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model request event requires turn_id")
}

// TestHarnessEvent_causation深拷贝 验证 causation_ids 不与入参共享。
// Python: events.py:417（tuple(self.causation_ids)）
func TestHarnessEvent_causation深拷贝(t *testing.T) {
	p, err := NewStateChangedEvent(HarnessStateIdle, HarnessStateRunning)
	require.NoError(t, err)
	causes := []string{"c1"}
	ev, err := NewHarnessEvent(HarnessEventOptions{
		Sequence: 1, Timestamp: 1, Event: &p,
		HostSessionID: "s1", AgentID: "a1", CausationIDs: causes,
	})
	require.NoError(t, err)
	causes[0] = "mutated"
	assert.Equal(t, []string{"c1"}, ev.CausationIDs)
}

// TestUnknownEvent_形状 验证未知事件保留 event_type 与 schema_version。
// Python: events.py:350-361 UnknownEvent（codec 行为留 15.4）
func TestUnknownEvent_形状(t *testing.T) {
	ev, err := NewUnknownEvent("future_event", "2", map[string]any{"k": 1})
	require.NoError(t, err)
	assert.Equal(t, "future_event", ev.EventType)
	assert.Equal(t, "2", ev.SchemaVersion)
	assert.Equal(t, "unknown", ev.payloadKind())

	_, err = NewUnknownEvent("", "2", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown event type and schema version must not be empty")
}

// TestEventBufferConfig_默认值与容量校验 验证 1024 默认与 capacity>0。
// Python: events.py:97-106 EventBufferConfig
// 上游用例: test_protocol.py:721 test_event_backpressure_retention_is_not_provider_selected
func TestEventBufferConfig_默认值与容量校验(t *testing.T) {
	c, err := NewEventBufferConfig(1024, EventOverflowPolicyCoalesceOrBlock)
	require.NoError(t, err)
	assert.Equal(t, 1024, c.Capacity)
	assert.Equal(t, EventOverflowPolicy("coalesce_or_block"), c.Overflow)

	_, err = NewEventBufferConfig(0, EventOverflowPolicyCoalesceOrBlock)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event buffer capacity must be positive")

	_, err = NewEventBufferConfig(-3, EventOverflowPolicyBlock)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event buffer capacity must be positive")
}

// TestEventRetentionOf_五条规则 逐条验证 retention 推导，顺序与上游一致。
// Python: events.py:378-389 event_retention（5 条 if，短路顺序不可调）
// 上游用例: test_protocol.py:721
func TestEventRetentionOf_五条规则(t *testing.T) {
	// 规则 1：OutputEvent + SNAPSHOT → coalescible
	snap, err := NewOutputEvent("a", OutputKindText, "current", OutputOperationSnapshot, OutputChannelAnswer, 0, "", nil)
	require.NoError(t, err)
	assert.Equal(t, EventRetentionCoalescible, EventRetentionOf(&snap))

	// 规则 1 反例：DELTA → required
	delta, err := NewOutputEvent("a", OutputKindText, "next", OutputOperationDelta, OutputChannelAnswer, 0, "", nil)
	require.NoError(t, err)
	assert.Equal(t, EventRetentionRequired, EventRetentionOf(&delta))

	// 规则 1 反例：FINAL → required
	final, err := NewOutputEvent("a", OutputKindText, "done", OutputOperationFinal, OutputChannelAnswer, 0, "", nil)
	require.NoError(t, err)
	assert.Equal(t, EventRetentionRequired, EventRetentionOf(&final))

	// 规则 2：UsageUpdatedEvent + CUMULATIVE → coalescible
	in := 1
	usage, err := NewTurnUsage(&in, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	cum, err := NewUsageUpdatedEvent(usage, UsageUpdateModeCumulative)
	require.NoError(t, err)
	assert.Equal(t, EventRetentionCoalescible, EventRetentionOf(&cum))

	// 规则 2 反例：DELTA → required
	ud, err := NewUsageUpdatedEvent(usage, UsageUpdateModeDelta)
	require.NoError(t, err)
	assert.Equal(t, EventRetentionRequired, EventRetentionOf(&ud))

	// 规则 3：HookObservedEvent → best_effort
	hook, err := NewHookObservedEvent(HookEventPhaseStarted, "before_tool", "h1", nil)
	require.NoError(t, err)
	assert.Equal(t, EventRetentionBestEffort, EventRetentionOf(&hook))

	// 规则 4：DiagnosticEvent + debug/info → best_effort
	for _, lv := range []DiagnosticLevel{DiagnosticLevelDebug, DiagnosticLevelInfo} {
		de, err := NewDiagnosticEvent(lv, "m", nil)
		require.NoError(t, err)
		assert.Equal(t, EventRetentionBestEffort, EventRetentionOf(&de), "level=%s", lv)
	}

	// 规则 4 反例：warning/error → required
	for _, lv := range []DiagnosticLevel{DiagnosticLevelWarning, DiagnosticLevelError} {
		de, err := NewDiagnosticEvent(lv, "m", nil)
		require.NoError(t, err)
		assert.Equal(t, EventRetentionRequired, EventRetentionOf(&de), "level=%s", lv)
	}

	// 规则 5：其余 → required
	item, err := NewItemLifecycleEvent(ItemEventKindStarted, "tool", nil)
	require.NoError(t, err)
	assert.Equal(t, EventRetentionRequired, EventRetentionOf(&item))

	st, err := NewStateChangedEvent(HarnessStateIdle, HarnessStateRunning)
	require.NoError(t, err)
	assert.Equal(t, EventRetentionRequired, EventRetentionOf(&st))

	tl, err := NewTurnLifecycleEvent(TurnEventKindStarted, nil)
	require.NoError(t, err)
	assert.Equal(t, EventRetentionRequired, EventRetentionOf(&tl))

	unk, err := NewUnknownEvent("future", "2", nil)
	require.NoError(t, err)
	assert.Equal(t, EventRetentionRequired, EventRetentionOf(&unk))

	// 规则 5：nil 兜底 required
	assert.Equal(t, EventRetentionRequired, EventRetentionOf(nil))
}
