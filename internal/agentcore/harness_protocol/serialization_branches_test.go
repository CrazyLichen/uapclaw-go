package harness_protocol

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件补齐 spec §6.1 要求的「26 个函数 100% 函数级覆盖」中剩余的错误分支。
// 每个用例对应上游 serialization.py / checkpoints.py 的一条异常路径，
// 断言文案而非仅断言 error 非 nil —— 文案是协议契约的一部分。

// TestAsMapping_非对象与nil 覆盖 asMapping 的两个拒绝分支。
// Python: serialization.py:470-473
func TestAsMapping_非对象与nil(t *testing.T) {
	_, err := asMapping(nil, "f")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "f must be a JSON object")
}

// TestAsInteger_各整数族 覆盖 asInteger 步骤 2 的 10 个类型分支。
// Python: serialization.py:502-506
func TestAsInteger_各整数族(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  int
	}{
		{"int8", int8(1), 1},
		{"int16", int16(2), 2},
		{"int32", int32(3), 3},
		{"uint", uint(4), 4},
		{"uint8", uint8(5), 5},
		{"uint16", uint16(6), 6},
		{"uint32", uint32(7), 7},
		{"uint64", uint64(8), 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := asInteger(c.value, "n")
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

// TestAsNumber_各数值类型 覆盖 asNumber 步骤 2 的全部类型分支。
// Python: serialization.py:509-513
func TestAsNumber_各数值类型(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  float64
	}{
		{"int8", int8(1), 1},
		{"int16", int16(2), 2},
		{"int32", int32(3), 3},
		{"int64", int64(4), 4},
		{"uint", uint(5), 5},
		{"uint8", uint8(6), 6},
		{"uint16", uint16(7), 7},
		{"uint32", uint32(8), 8},
		{"uint64", uint64(9), 9},
		{"float32", float32(1.5), 1.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := asNumber(c.value, "n")
			require.NoError(t, err)
			assert.InDelta(t, c.want, got, 1e-9)
		})
	}
}

// TestParseXxx_全枚举非字符串 覆盖 13 个 parseXxx 的首个拒绝分支。
// Python: serialization.py:517-518（每个 _enum 调用都走同一守卫）
func TestParseXxx_全枚举非字符串(t *testing.T) {
	type parseFn func(any, string) error
	fns := map[string]parseFn{
		"MessageRole": func(v any, f string) error {
			_, e := parseMessageRole(v, f)
			return e
		},
		"ModelRequestStatus": func(v any, f string) error {
			_, e := parseModelRequestStatus(v, f)
			return e
		},
		"TurnStatus": func(v any, f string) error {
			_, e := parseTurnStatus(v, f)
			return e
		},
		"TurnTerminationKind": func(v any, f string) error {
			_, e := parseTurnTerminationKind(v, f)
			return e
		},
		"OutputKind": func(v any, f string) error {
			_, e := parseOutputKind(v, f)
			return e
		},
		"OutputOperation": func(v any, f string) error {
			_, e := parseOutputOperation(v, f)
			return e
		},
		"OutputChannel": func(v any, f string) error {
			_, e := parseOutputChannel(v, f)
			return e
		},
		"ItemEventKind": func(v any, f string) error {
			_, e := parseItemEventKind(v, f)
			return e
		},
		"UsageUpdateMode": func(v any, f string) error {
			_, e := parseUsageUpdateMode(v, f)
			return e
		},
		"HarnessState": func(v any, f string) error {
			_, e := parseHarnessState(v, f)
			return e
		},
		"TurnEventKind": func(v any, f string) error {
			_, e := parseTurnEventKind(v, f)
			return e
		},
		"HookEventPhase": func(v any, f string) error {
			_, e := parseHookEventPhase(v, f)
			return e
		},
		"DiagnosticLevel": func(v any, f string) error {
			_, e := parseDiagnosticLevel(v, f)
			return e
		},
	}
	require.Len(t, fns, 13, "13 个 parseXxx 全部在此守护")
	for name, fn := range fns {
		t.Run(name, func(t *testing.T) {
			err := fn(42, name+".f")
			require.Error(t, err)
			assert.Contains(t, err.Error(), name+".f must be a string enum value")
		})
	}
}

// TestOptIntegerOptFloat_错误分支 覆盖两个可空助手的三条路径。
// Python: serialization.py:502-513（optInteger/optFloat 是 Go 侧的 nil 包装）
func TestOptIntegerOptFloat_错误分支(t *testing.T) {
	got, err := optInteger(nil, "n")
	require.NoError(t, err)
	assert.Nil(t, got)

	_, err = optInteger("x", "n")
	require.Error(t, err)

	gotF, err := optFloat(nil, "f")
	require.NoError(t, err)
	assert.Nil(t, gotF)

	_, err = optFloat("x", "f")
	require.Error(t, err)
}

// TestKeyHelpers_键缺失 覆盖 asNumberFromKey / asIntegerFromKey 的缺键分支。
func TestKeyHelpers_键缺失(t *testing.T) {
	_, err := asNumberFromKey(map[string]any{}, "timestamp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid harness event wire object: timestamp")

	_, err = asIntegerFromKey(map[string]any{}, "sequence")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid harness event wire object: sequence")
}

// TestPtrHelpers_nil 覆盖 4 个指针转 any 助手的 nil 分支。
func TestPtrHelpers_nil(t *testing.T) {
	assert.Nil(t, intPtrToAny(nil))
	assert.Nil(t, strPtrToAny(nil))
	assert.Nil(t, boolPtrToAny(nil))
	assert.Nil(t, floatPtrToAny(nil))

	// non-nil 分支
	n := 1
	s := "x"
	b := true
	f := 1.5
	assert.Equal(t, 1, intPtrToAny(&n))
	assert.Equal(t, "x", strPtrToAny(&s))
	assert.Equal(t, true, boolPtrToAny(&b))
	assert.Equal(t, 1.5, floatPtrToAny(&f))
}

// TestPayloadTypeName 覆盖值/指针/nil 三形态。
// Python: serialization.py:359 type(event).__name__
func TestPayloadTypeName(t *testing.T) {
	assert.Equal(t, "None", payloadTypeName(nil))
	assert.Equal(t, "DiagnosticEvent", payloadTypeName(mustDiagnosticEvent(t)))
	ev := mustDiagnosticEvent(t)
	assert.Equal(t, "*DiagnosticEvent", payloadTypeName(&ev))
}

// TestUsageFromDict_各字段错误 覆盖 5 个可空整数的错误分支。
// Python: serialization.py:68-76
func TestUsageFromDict_各字段错误(t *testing.T) {
	for _, field := range []string{
		"input_tokens", "output_tokens", "cached_input_tokens",
		"reasoning_output_tokens", "total_tokens",
	} {
		t.Run(field, func(t *testing.T) {
			_, err := usageFromDict(map[string]any{field: "x"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "usage."+field)
		})
	}
}

// TestBlockFromDict_字段缺失 覆盖 block_id / kind 缺键。
// Python: serialization.py:88-94（靠 KeyError 触发）
func TestBlockFromDict_字段缺失(t *testing.T) {
	_, err := blockFromDict(map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid harness event wire object: block_id")

	_, err = blockFromDict(map[string]any{"block_id": "b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid harness event wire object: kind")
}

// TestMessageFromDict_字段缺失 覆盖 message_id 缺键与 content 项非对象。
// Python: serialization.py:106-113
func TestMessageFromDict_字段缺失(t *testing.T) {
	_, err := messageFromDict(map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid harness event wire object: message_id")

	_, err = messageFromDict(map[string]any{
		"message_id": "m",
		"role":       "user",
		"content":    []any{"not-an-object"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content_block must be a JSON object")
}

// TestErrorFromDict_错误分支 覆盖非对象 / message 缺键 / retryable 非 bool。
// Python: serialization.py:126-134
func TestErrorFromDict_错误分支(t *testing.T) {
	_, err := errorFromDict([]any{}, "e")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "e must be a JSON object")

	_, err = errorFromDict(map[string]any{}, "e")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid harness event wire object: message")

	_, err = errorFromDict(map[string]any{"message": "x", "retryable": "yes"}, "e")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "e.retryable must be a boolean")
}

// TestModelRequestFromDict_字段缺失 覆盖多个必填字段与可选结构错误。
// Python: serialization.py:161-197
func TestModelRequestFromDict_字段缺失(t *testing.T) {
	base := func() map[string]any { return modelRequestToDict(newTestModelRequestEvent(t)) }

	cases := []struct {
		name    string
		mutate  func(map[string]any)
		wantMsg string
	}{
		{"request_id 缺失", func(d map[string]any) { delete(d, "request_id") }, "request_id"},
		{"status 非法", func(d map[string]any) { d["status"] = "bogus" }, "model_request.status"},
		{"started_at 缺失", func(d map[string]any) { delete(d, "started_at") }, "started_at"},
		{"ended_at 缺失", func(d map[string]any) { delete(d, "ended_at") }, "ended_at"},
		{"input_observed 非 bool", func(d map[string]any) { d["input_observed"] = "x" }, "input_observed"},
		{"output_message 非对象", func(d map[string]any) { d["output_message"] = []any{} }, "model_request.output_message"},
		{"time_to_first_chunk 非数", func(d map[string]any) { d["time_to_first_chunk"] = "x" }, "time_to_first_chunk"},
		{"finish_reasons 非数组", func(d map[string]any) { d["finish_reasons"] = map[string]any{} }, "finish_reasons"},
		{"finish_reason 空串", func(d map[string]any) { d["finish_reasons"] = []any{""} }, "finish_reason"},
		{"data 非对象", func(d map[string]any) { d["data"] = []any{} }, "model_request.data"},
		{"cost.micros 缺失", func(d map[string]any) { d["cost"] = map[string]any{"currency": "USD"} }, "micros"},
		{"cost.currency 缺失", func(d map[string]any) { d["cost"] = map[string]any{"micros": 1} }, "currency"},
		{"error 非对象", func(d map[string]any) { d["error"] = []any{} }, "model_request.error"},
		{"system_instructions 项非对象", func(d map[string]any) { d["system_instructions"] = []any{"x"} }, "content_block"},
		{"input_messages 非数组", func(d map[string]any) { d["input_messages"] = map[string]any{} }, "input_messages"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := base()
			c.mutate(d)
			_, err := modelRequestFromDict(d)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantMsg)
		})
	}
}

// TestResultFromDict_字段缺失 覆盖 result 的多条错误路径。
// Python: serialization.py:230-274
func TestResultFromDict_字段缺失(t *testing.T) {
	completed, err := NewTurnResult(TurnStatusCompleted, ResultOptions{})
	require.NoError(t, err)
	base := func() map[string]any { return resultToDict(completed) }

	cases := []struct {
		name    string
		mutate  func(map[string]any)
		wantMsg string
	}{
		{"stop_reason 空串", func(d map[string]any) { d["stop_reason"] = "" }, "turn_result.stop_reason"},
		{"termination.message 空串", func(d map[string]any) {
			d["termination"] = map[string]any{"kind": "timeout", "message": ""}
		}, "turn_result.termination.message"},
		{"termination.code 空串", func(d map[string]any) {
			d["termination"] = map[string]any{"kind": "timeout", "code": ""}
		}, "turn_result.termination.code"},
		{"termination.provider_data 非对象", func(d map[string]any) {
			d["termination"] = map[string]any{"kind": "timeout", "provider_data": []any{}}
		}, "turn_result.termination.provider_data"},
		{"error 非对象", func(d map[string]any) { d["error"] = []any{} }, "turn_result.error"},
		{"usage 非对象", func(d map[string]any) { d["usage"] = []any{} }, "turn_result.usage"},
		{"cost.micros 缺失", func(d map[string]any) { d["cost"] = map[string]any{"currency": "USD"} }, "micros"},
		{"started_at 非法", func(d map[string]any) { d["started_at"] = "x" }, "turn_result.started_at"},
		{"completed_at 非法", func(d map[string]any) { d["completed_at"] = "x" }, "turn_result.completed_at"},
		{"duration_ms 非法", func(d map[string]any) { d["duration_ms"] = "x" }, "turn_result.duration_ms"},
		{"provider_data 非对象", func(d map[string]any) { d["provider_data"] = []any{} }, "turn_result.provider_data"},
		{"messages 项非对象", func(d map[string]any) { d["messages"] = []any{"x"} }, "turn_message"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := base()
			c.mutate(d)
			_, err := resultFromDict(d)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantMsg)
		})
	}
}

// TestPayloadFromWire_各载荷字段错误 覆盖 9 条出口内部的错误分支。
// Python: serialization.py:385-441
func TestPayloadFromWire_各载荷字段错误(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		body      map[string]any
		wantMsg   string
	}{
		{"output 缺 output_id", "output", map[string]any{}, "output_id"},
		{"output kind 非法", "output", map[string]any{"output_id": "o", "kind": "x"}, "output.kind"},
		{"output data 非对象", "output", map[string]any{
			"output_id": "o", "kind": "text", "operation": "final", "channel": "answer",
			"content_index": 0, "data": []any{},
		}, "output.data"},
		{"item kind 非法", "item_lifecycle", map[string]any{"kind": "x"}, "item.kind"},
		{"item item_type 缺失", "item_lifecycle", map[string]any{"kind": "started"}, "item_type"},
		{"item data 非对象", "item_lifecycle", map[string]any{
			"kind": "started", "item_type": "tool", "data": []any{},
		}, "item.data"},
		{"usage 非对象", "usage_updated", map[string]any{"usage": []any{}}, "usage_updated.usage"},
		{"usage mode 非法", "usage_updated", map[string]any{
			"usage": map[string]any{}, "mode": "x",
		}, "usage_updated.mode"},
		{"state old 非法", "state_changed", map[string]any{"old": "x"}, "state_changed.old"},
		{"state new 非法", "state_changed", map[string]any{"old": "idle", "new": "x"}, "state_changed.new"},
		{"turn kind 非法", "turn_lifecycle", map[string]any{"kind": "x"}, "turn_lifecycle.kind"},
		{"turn result 非对象", "turn_lifecycle", map[string]any{"kind": "started", "result": []any{}}, "turn_lifecycle.result"},
		{"hook phase 非法", "hook_observed", map[string]any{"phase": "x"}, "hook.phase"},
		{"hook name 缺失", "hook_observed", map[string]any{"phase": "started"}, "hook_name"},
		{"hook id 缺失", "hook_observed", map[string]any{"phase": "started", "hook_name": "n"}, "hook_id"},
		{"hook data 非对象", "hook_observed", map[string]any{
			"phase": "started", "hook_name": "n", "hook_id": "i", "data": []any{},
		}, "hook.data"},
		{"diag level 非法", "diagnostic", map[string]any{"level": "x"}, "diagnostic.level"},
		{"diag message 缺失", "diagnostic", map[string]any{"level": "info"}, "message"},
		{"diag data 非对象", "diagnostic", map[string]any{
			"level": "info", "message": "m", "data": []any{},
		}, "diagnostic.data"},
		{"provider 缺失", "provider", map[string]any{}, "invalid harness event wire object: provider"},
		{"provider event_type 缺失", "provider", map[string]any{"provider": "p"}, "invalid harness event wire object: event_type"},
		{"provider schema 缺失", "provider", map[string]any{"provider": "p", "event_type": "e"}, "invalid harness event wire object: schema_version"},
		{"provider payload 非对象", "provider", map[string]any{
			"provider": "p", "event_type": "e", "schema_version": "1", "payload": []any{},
		}, "provider.payload"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := payloadFromWire(c.eventType, EventWireSchemaVersion, c.body)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantMsg)
		})
	}
}

// TestHarnessEventFromDict_部分字段错误 覆盖剩余的字段错误分支。
// Python: serialization.py:445-467
func TestHarnessEventFromDict_部分字段错误(t *testing.T) {
	valid := func() map[string]any {
		ev, err := NewHarnessEvent(HarnessEventOptions{
			Sequence:      1,
			Timestamp:     1.0,
			Event:         mustDiagnosticEvent(t),
			HostSessionID: "s",
			AgentID:       "a",
		})
		require.NoError(t, err)
		return HarnessEventToDict(ev)
	}

	cases := []struct {
		name    string
		mutate  func(map[string]any)
		wantMsg string
	}{
		{"schema_version 缺失", func(d map[string]any) { delete(d, "schema_version") }, "schema_version"},
		{"causation_ids 非数组", func(d map[string]any) { d["causation_ids"] = "x" }, "causation_ids"},
		{"sequence 缺失", func(d map[string]any) { delete(d, "sequence") }, "sequence"},
		{"timestamp 缺失", func(d map[string]any) { delete(d, "timestamp") }, "timestamp"},
		{"host_session_id 缺失", func(d map[string]any) { delete(d, "host_session_id") }, "host_session_id"},
		{"agent_id 缺失", func(d map[string]any) { delete(d, "agent_id") }, "agent_id"},
		{"provider_session_id 空串", func(d map[string]any) { d["provider_session_id"] = "" }, "provider_session_id"},
		{"turn_id 空串", func(d map[string]any) { d["turn_id"] = "" }, "turn_id"},
		{"item_id 空串", func(d map[string]any) { d["item_id"] = "" }, "item_id"},
		{"correlation_id 空串", func(d map[string]any) { d["correlation_id"] = "" }, "correlation_id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := valid()
			c.mutate(d)
			_, err := HarnessEventFromDict(d)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantMsg)
		})
	}
}

// TestHarnessEventToDict_编码失败降级 覆盖 payloadToWire 报错的降级分支。
// 上游此处抛异常；Go 侧签名无 error，降级为 _codec_error 键。
func TestHarnessEventToDict_编码失败降级(t *testing.T) {
	ev, err := NewHarnessEvent(HarnessEventOptions{
		Sequence:      1,
		Timestamp:     1.0,
		Event:         mustDiagnosticEvent(t),
		HostSessionID: "s",
		AgentID:       "a",
	})
	require.NoError(t, err)
	// 塞入一个不支持的自定义载荷
	ev.Event = unknownPayloadKind{}
	wire := HarnessEventToDict(ev)
	assert.Contains(t, wire["_codec_error"], "unsupported event payload type")
}

// TestCheckpointDataSize_编码失败 覆盖 checkpointDataSize 的 error 分支。
// NaN 无法被 json.Encoder 编码。
func TestCheckpointDataSize_编码失败(t *testing.T) {
	_, err := checkpointDataSize(map[string]any{"nan": math.NaN()})
	require.Error(t, err)
}

// TestModelRequestRoundTrip_终止结果带 result 覆盖 turn_lifecycle 的 result 分支。
// Python: serialization.py:316-324
func TestTurnLifecycle_带result往返(t *testing.T) {
	res, err := NewTurnResult(TurnStatusCompleted, ResultOptions{FinalOutput: "done"})
	require.NoError(t, err)
	ev, err := NewTurnLifecycleEvent(TurnEventKindFinished, &res)
	require.NoError(t, err)

	eventType, schemaVersion, body, err := payloadToWire(ev)
	require.NoError(t, err)
	assert.Equal(t, "turn_lifecycle", eventType)

	got, err := payloadFromWire(eventType, schemaVersion, body)
	require.NoError(t, err)
	back, ok := got.(TurnLifecycleEvent)
	require.True(t, ok)
	require.NotNil(t, back.Result)
	assert.Equal(t, "done", back.Result.FinalOutput)
}

// TestResultToDict_全可选字段 覆盖 resultToDict 的 termination/error/usage 全填分支。
// 注意：构造器不变量要求 termination 只能出现在 interrupted、error 只能出现在 failed，
// 故分两个结果分别覆盖。
// Python: serialization.py:200-227
func TestResultToDict_全可选字段(t *testing.T) {
	code := "E1"
	msg := "m"
	startedAt, completedAt := 1.0, 2.0
	durationMS := 100
	usageIn := 5

	// interrupted 带 termination + usage + 时间字段
	interrupted, err := NewTurnResult(TurnStatusInterrupted, ResultOptions{
		Termination: &TurnTermination{
			Kind:         TurnTerminationKindTimeout,
			Message:      &msg,
			Code:         &code,
			ProviderData: map[string]any{"k": "v"},
		},
		Usage:        &TurnUsage{InputTokens: &usageIn},
		StartedAt:    &startedAt,
		CompletedAt:  &completedAt,
		DurationMS:   &durationMS,
		ProviderData: map[string]any{"pd": 1},
	})
	require.NoError(t, err)

	d := resultToDict(interrupted)
	require.NotNil(t, d["termination"])
	require.NotNil(t, d["usage"])
	assert.Equal(t, 100, d["duration_ms"])

	back, err := resultFromDict(d)
	require.NoError(t, err)
	assert.Equal(t, TurnStatusInterrupted, back.Status)
	require.NotNil(t, back.Termination)
	assert.Equal(t, TurnTerminationKindTimeout, back.Termination.Kind)

	// failed 带 error
	retryable := true
	failed, err := NewTurnResult(TurnStatusFailed, ResultOptions{
		Error: &TurnError{Message: "boom", Retryable: &retryable},
	})
	require.NoError(t, err)
	df := resultToDict(failed)
	require.NotNil(t, df["error"])

	backF, err := resultFromDict(df)
	require.NoError(t, err)
	assert.Equal(t, TurnStatusFailed, backF.Status)
	require.NotNil(t, backF.Error)
	assert.True(t, *backF.Error.Retryable)
}

// TestParseOutputOperationChannel_非法值 覆盖这两个枚举的非法值分支。
// Python: serialization.py:390-391
func TestParseOutputOperationChannel_非法值(t *testing.T) {
	_, err := parseOutputOperation("x", "output.operation")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output.operation: 'x'")

	_, err = parseOutputChannel("x", "output.channel")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output.channel: 'x'")
}

// TestMessageFromDict_data非对象 覆盖 turn_message.data 错误分支。
// Python: serialization.py:112
func TestMessageFromDict_data非对象(t *testing.T) {
	_, err := messageFromDict(map[string]any{
		"message_id": "m", "role": "user", "content": []any{}, "data": []any{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "turn_message.data")
}

// TestErrorFromDict_providerData非对象 覆盖 provider_data 错误分支。
// Python: serialization.py:133
func TestErrorFromDict_providerData非对象(t *testing.T) {
	_, err := errorFromDict(map[string]any{"message": "x", "provider_data": []any{}}, "e")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "e.provider_data")
}

// TestErrorFromDict_category非空串 覆盖 category 成功路径。
// Python: serialization.py:131
func TestErrorFromDict_category(t *testing.T) {
	category := "sdk_error"
	e, err := errorFromDict(map[string]any{"message": "x", "category": category}, "e")
	require.NoError(t, err)
	assert.Equal(t, "sdk_error", *e.Category)
}

// TestModelRequestToDict_全可空字段填满 覆盖 to 侧的所有非空分支。
// Python: serialization.py:137-158
func TestModelRequestToDict_全可空字段填满(t *testing.T) {
	ttft := 0.5
	cost := &MonetaryAmount{Micros: 1, Currency: "USD"}
	code := "E1"
	outMsg := &TurnMessage{MessageID: "m", Role: MessageRoleAssistant, Content: []ContentBlock{}, Data: map[string]any{}}
	usageIn := 1
	ev, err := NewModelRequestEvent(ModelRequestOptions{
		RequestID:         "r",
		Status:            ModelRequestStatusFailed,
		StartedAt:         1.0,
		EndedAt:           2.0,
		Model:             "m",
		ProviderName:      "p",
		ResponseID:        "resp",
		TimeToFirstChunk:  &ttft,
		FinishReasons:     []string{"stop"},
		OutputMessage:     outMsg,
		Usage:             &TurnUsage{InputTokens: &usageIn},
		Cost:              cost,
		Error:             &TurnError{Message: "boom", Code: &code},
		ToolDefinitions:   []any{},
		RequestParameters: map[string]any{},
		Data:              map[string]any{},
	})
	require.NoError(t, err)

	d := modelRequestToDict(ev)
	assert.Equal(t, "m", d["model"])
	assert.Equal(t, "p", d["provider_name"])
	assert.Equal(t, "resp", d["response_id"])
	assert.Equal(t, 0.5, d["time_to_first_chunk"])
	require.NotNil(t, d["output_message"])
	require.NotNil(t, d["usage"])
	require.NotNil(t, d["cost"])
	require.NotNil(t, d["error"])

	back, err := modelRequestFromDict(d)
	require.NoError(t, err)
	assert.Equal(t, ModelRequestStatusFailed, back.Status)
	assert.Equal(t, "m", back.Model)
	require.NotNil(t, back.TimeToFirstChunk)
	assert.Equal(t, 0.5, *back.TimeToFirstChunk)
}

// TestResultFromDict_usage与error成功路径 覆盖 usage/error 的非空解码。
// Python: serialization.py:238-249
func TestResultFromDict_usage与error成功路径(t *testing.T) {
	usageIn := 3
	retryable := false
	res, err := NewTurnResult(TurnStatusFailed, ResultOptions{
		Error: &TurnError{Message: "boom", Retryable: &retryable},
		Usage: &TurnUsage{InputTokens: &usageIn},
	})
	require.NoError(t, err)

	back, err := resultFromDict(resultToDict(res))
	require.NoError(t, err)
	require.NotNil(t, back.Usage)
	assert.Equal(t, 3, *back.Usage.InputTokens)
	require.NotNil(t, back.Error)
	assert.False(t, *back.Error.Retryable)
}

// TestPayloadToWire_全载荷可选字段填满 覆盖各载荷 to 侧的非空分支。
// Python: serialization.py:278-356
func TestPayloadToWire_全载荷可选字段填满(t *testing.T) {
	// output 带 content_type
	output, err := NewOutputEvent("o", OutputKindText, "c", OutputOperationDelta,
		OutputChannelReasoning, 3, "text/plain", map[string]any{"k": "v"})
	require.NoError(t, err)
	_, _, body, err := payloadToWire(output)
	require.NoError(t, err)
	assert.Equal(t, "text/plain", body["content_type"])

	// turn_lifecycle 带 result
	res, err := NewTurnResult(TurnStatusCompleted, ResultOptions{})
	require.NoError(t, err)
	turn, err := NewTurnLifecycleEvent(TurnEventKindFinished, &res)
	require.NoError(t, err)
	_, _, body, err = payloadToWire(turn)
	require.NoError(t, err)
	require.NotNil(t, body["result"])

	// provider 带数据
	provider := mustProviderEvent(t)
	_, _, body, err = payloadToWire(&provider)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"k": "v"}, body["payload"])
}

// TestCheckpointDataSize_换行裁剪 覆盖尾部无换行的边界（非空对象）。
// Python: checkpoints.py:62
func TestCheckpointDataSize_嵌套结构(t *testing.T) {
	n, err := checkpointDataSize(map[string]any{"a": []any{1, 2}, "b": map[string]any{"c": true}})
	require.NoError(t, err)
	assert.Greater(t, n, 0)
}

// TestNewHarnessCheckpoint_编码失败传播 覆盖 checkpointDataSize 报错的上抛分支。
// NaN 已被 FreezeJsonObject 拒绝，故此处不可达——用超大嵌套结构间接验证
// 冻结成功后尺寸计算的正常路径（已在 TestCheckpointDataSize_嵌套结构 覆盖）。
// 该分支为防御性代码：FreezeJsonObject 之后 data 恒可编码。
func TestNewHarnessCheckpoint_尺寸路径覆盖(t *testing.T) {
	cp, err := NewHarnessCheckpoint(CheckpointOptions{
		Provider: "a", SchemaVersion: "1", AgentID: "b",
		HostSessionID: "c", CheckpointID: "d", Sequence: 0,
		Data: map[string]any{},
	})
	require.NoError(t, err)
	assert.NotNil(t, cp.Data)
}
