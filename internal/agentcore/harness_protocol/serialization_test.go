package harness_protocol

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAsString 验证必填字符串三种失败形态。
// Python: serialization.py:486-493 _string / _required_string
func TestAsString(t *testing.T) {
	t.Run("正常", func(t *testing.T) {
		got, err := asString(map[string]any{"k": "v"}, "k")
		require.NoError(t, err)
		assert.Equal(t, "v", got)
	})

	t.Run("键缺失", func(t *testing.T) {
		_, err := asString(map[string]any{}, "k")
		require.Error(t, err)
		// Python 是 KeyError 被外层包成 invalid harness event wire object
		assert.Contains(t, err.Error(), "invalid harness event wire object")
	})

	t.Run("空串", func(t *testing.T) {
		_, err := asString(map[string]any{"k": ""}, "k")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "k must be a non-empty string")
	})

	t.Run("非字符串", func(t *testing.T) {
		_, err := asString(map[string]any{"k": 1}, "k")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "k must be a non-empty string")
	})
}

// TestAsOptionalString 验证 nil 与空串的区别。
// Python: serialization.py:496-499 _optional_string
func TestAsOptionalString(t *testing.T) {
	got, err := asOptionalString(nil, "k")
	require.NoError(t, err)
	assert.Nil(t, got, "nil 表示未提供")

	// 空串是非法值，不是「未提供」
	_, err = asOptionalString("", "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "k must be a non-empty string")
}

// TestAsInteger 验证四步：拒 bool、整数族、整值 float64、其余报错。
// Python: serialization.py:502-506 _integer（+ spec D4 放宽整值 float64）
func TestAsInteger(t *testing.T) {
	cases := []struct {
		name    string
		value   any
		want    int
		wantErr bool
	}{
		{"int", 3, 3, false},
		{"int64", int64(7), 7, false},
		{"整值 float64", 3.0, 3, false},
		{"负零 float64", math.Copysign(0, -1), 0, false},
		{"bool true 拒绝", true, 0, true},
		{"bool false 拒绝", false, 0, true},
		{"小数 3.5 拒绝", 3.5, 0, true},
		{"NaN 拒绝", math.NaN(), 0, true},
		{"+Inf 拒绝", math.Inf(1), 0, true},
		{"溢出 1e300 拒绝", 1e300, 0, true},
		{"字符串拒绝", "3", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := asInteger(c.value, "sequence")
			if c.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "sequence must be an integer")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

// TestAsNumber 验证拒 bool、接受整数族与任意 float64。
// Python: serialization.py:509-513 _number
func TestAsNumber(t *testing.T) {
	got, err := asNumber(3, "timestamp")
	require.NoError(t, err)
	assert.Equal(t, 3.0, got)

	got, err = asNumber(2.5, "timestamp")
	require.NoError(t, err)
	assert.Equal(t, 2.5, got)

	// NaN 在 asNumber 不拒（交由 FreezeJsonValue 拒绝）
	got, err = asNumber(math.NaN(), "timestamp")
	require.NoError(t, err)
	assert.True(t, math.IsNaN(got))

	_, err = asNumber(true, "timestamp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timestamp must be a number")

	_, err = asNumber("1", "timestamp")
	require.Error(t, err)
}

// TestAsMapping 验证对象与非 string key 拒绝。
// Python: serialization.py:470-473 _mapping
func TestAsMapping(t *testing.T) {
	got, err := asMapping(map[string]any{"a": 1}, "obj")
	require.NoError(t, err)
	assert.Equal(t, 1, got["a"])

	_, err = asMapping([]any{1}, "obj")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "obj must be a JSON object")

	// 非 string key（Go 的 map[any]any）
	_, err = asMapping(map[any]any{1: "x"}, "obj")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "obj must be a JSON object")
}

// TestAsList 验证数组与非数组拒绝。
// Python: serialization.py:480-483 _list
func TestAsList(t *testing.T) {
	got, err := asList([]any{1, 2}, "arr")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	_, err = asList(map[string]any{}, "arr")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "arr must be a JSON array")
}

// TestParseXxx_合法值 验证 13 个枚举解析各自接受全部合法值。
// 成员数已机器核对（spec §5.2.2）：合计 45 成员。
// Python: serialization.py 的 14 处 _enum 调用
func TestParseXxx_合法值(t *testing.T) {
	cases := map[string]struct {
		parse func(any, string) (string, error)
		valid []string
	}{
		"MessageRole":         {func(v any, f string) (string, error) { r, e := parseMessageRole(v, f); return string(r), e }, []string{"user", "assistant", "system", "tool"}},
		"ModelRequestStatus":  {func(v any, f string) (string, error) { r, e := parseModelRequestStatus(v, f); return string(r), e }, []string{"completed", "failed", "cancelled"}},
		"TurnStatus":          {func(v any, f string) (string, error) { r, e := parseTurnStatus(v, f); return string(r), e }, []string{"completed", "interrupted", "failed"}},
		"TurnTerminationKind": {func(v any, f string) (string, error) { r, e := parseTurnTerminationKind(v, f); return string(r), e }, []string{"user_abort", "timeout", "policy", "provider", "harness_stop"}},
		"OutputKind":          {func(v any, f string) (string, error) { r, e := parseOutputKind(v, f); return string(r), e }, []string{"text", "structured"}},
		"OutputOperation":     {func(v any, f string) (string, error) { r, e := parseOutputOperation(v, f); return string(r), e }, []string{"delta", "snapshot", "final"}},
		"OutputChannel":       {func(v any, f string) (string, error) { r, e := parseOutputChannel(v, f); return string(r), e }, []string{"answer", "reasoning", "system"}},
		"ItemEventKind":       {func(v any, f string) (string, error) { r, e := parseItemEventKind(v, f); return string(r), e }, []string{"started", "updated", "completed"}},
		"UsageUpdateMode":     {func(v any, f string) (string, error) { r, e := parseUsageUpdateMode(v, f); return string(r), e }, []string{"delta", "cumulative"}},
		"HarnessState":        {func(v any, f string) (string, error) { r, e := parseHarnessState(v, f); return string(r), e }, []string{"idle", "running", "pausing", "paused", "terminated"}},
		"TurnEventKind":       {func(v any, f string) (string, error) { r, e := parseTurnEventKind(v, f); return string(r), e }, []string{"started", "paused", "resumed", "finished", "aborted", "failed"}},
		"HookEventPhase":      {func(v any, f string) (string, error) { r, e := parseHookEventPhase(v, f); return string(r), e }, []string{"started", "finished"}},
		"DiagnosticLevel":     {func(v any, f string) (string, error) { r, e := parseDiagnosticLevel(v, f); return string(r), e }, []string{"debug", "info", "warning", "error"}},
	}
	total := 0
	for name, c := range cases {
		total += len(c.valid)
		for _, v := range c.valid {
			got, err := c.parse(v, name)
			require.NoError(t, err, "%s 应接受 %q", name, v)
			assert.Equal(t, v, got)
		}
	}
	// spec §5.2.2 机器核对值：45
	assert.Equal(t, 45, total, "13 个枚举成员合计")
}

// TestParseXxx_非法值 验证非法值与非字符串的两种错误文案。
// Python: serialization.py:517-522
func TestParseXxx_非法值(t *testing.T) {
	t.Run("非字符串", func(t *testing.T) {
		_, err := parseOutputKind(1, "output.kind")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "output.kind must be a string enum value")
	})

	t.Run("未知值带单引号 repr", func(t *testing.T) {
		_, err := parseOutputKind("bogus", "output.kind")
		require.Error(t, err)
		// 上游用 {value!r}，字符串表现为 'bogus'
		assert.Contains(t, err.Error(), "unknown output.kind: 'bogus'")
	})

	t.Run("空串是非法值", func(t *testing.T) {
		_, err := parseOutputKind("", "output.kind")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown output.kind: ''")
	})
}

// TestErrorFromDict_前缀来自实参 验证 errorFromDict 在不同调用点
// 产出不同前缀（模型请求 vs 轮次结果 vs 终止原因）。
// Python: serialization.py:126-134 _error_from_dict（f"{field_name}.code"）
func TestErrorFromDict_前缀来自实参(t *testing.T) {
	t.Run("model_request.error", func(t *testing.T) {
		_, err := errorFromDict(map[string]any{"message": "boom", "code": 1}, "model_request.error")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model_request.error.code")
	})

	t.Run("turn_result.error", func(t *testing.T) {
		_, err := errorFromDict(map[string]any{"message": "boom", "code": 1}, "turn_result.error")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "turn_result.error.code")
	})

	t.Run("turn_result.termination", func(t *testing.T) {
		_, err := errorFromDict(map[string]any{"message": "boom", "category": 1}, "turn_result.termination")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "turn_result.termination.category")
	})
}

// TestBlockFromDict_前缀硬编码 验证 blockFromDict 无论调用点都是 content_block. 前缀。
// Python: serialization.py:88-94 _block_from_dict（硬编码 "content_block.data"）
func TestBlockFromDict_前缀硬编码(t *testing.T) {
	_, err := blockFromDict(map[string]any{"block_id": "b", "kind": "text", "data": []any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content_block.data",
		"前缀必须硬编码为 content_block.，不得随调用点变化")
}

// TestMessageFromDict_前缀硬编码 验证 messageFromDict 前缀是 turn_message.。
// Python: serialization.py:106-113 _message_from_dict
func TestMessageFromDict_前缀硬编码(t *testing.T) {
	_, err := messageFromDict(map[string]any{"message_id": "m", "role": "user", "content": map[string]any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "turn_message.content")

	_, err = messageFromDict(map[string]any{"message_id": "m", "role": "bogus", "content": []any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "turn_message.role")
}

// TestUsageFromDict_前缀硬编码 验证 usage 前缀。
// Python: serialization.py:68-76 _usage_from_dict（"usage.provider_data"）
func TestUsageFromDict_前缀硬编码(t *testing.T) {
	_, err := usageFromDict(map[string]any{"provider_data": []any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "usage.provider_data")
}

// TestUsageRoundTrip 验证 usage 往返。
// Python: serialization.py:57-76
func TestUsageRoundTrip(t *testing.T) {
	in, out := 30, 5
	cached := 10
	usage := TurnUsage{
		InputTokens:       &in,
		OutputTokens:      &out,
		CachedInputTokens: &cached,
		ProviderData:      map[string]any{"vendor": "a"},
	}
	d := usageToDict(usage)
	assert.Equal(t, 30, d["input_tokens"])
	assert.Equal(t, map[string]any{"vendor": "a"}, d["provider_data"])

	back, err := usageFromDict(d)
	require.NoError(t, err)
	assert.Equal(t, 30, *back.InputTokens)
	assert.Equal(t, 5, *back.OutputTokens)
	assert.Equal(t, 10, *back.CachedInputTokens)
	assert.Nil(t, back.ReasoningOutputTokens)
}

// TestBlockRoundTrip 验证内容块往返。
// Python: serialization.py:79-94
func TestBlockRoundTrip(t *testing.T) {
	block := ContentBlock{
		BlockID: "block-1",
		Kind:    "text",
		Content: "partial",
		Data:    map[string]any{},
	}
	d := blockToDict(block)
	assert.Equal(t, "block-1", d["block_id"])
	assert.Equal(t, "partial", d["content"])

	back, err := blockFromDict(d)
	require.NoError(t, err)
	assert.Equal(t, "partial", back.Content)
}

// TestMessageRoundTrip 验证消息往返。
// Python: serialization.py:97-113
func TestMessageRoundTrip(t *testing.T) {
	msg := TurnMessage{
		MessageID: "message-1",
		Role:      MessageRoleAssistant,
		Content: []ContentBlock{
			{BlockID: "block-1", Kind: "text", Content: "hi", Data: map[string]any{}},
		},
		Data: map[string]any{"origin": "external"},
	}
	d := messageToDict(msg)
	assert.Equal(t, "assistant", d["role"])

	back, err := messageFromDict(d)
	require.NoError(t, err)
	require.Len(t, back.Content, 1)
	assert.Equal(t, "hi", back.Content[0].Content)
	assert.Equal(t, "external", back.Data["origin"])
}

// TestErrorRoundTrip 验证错误往返与可空字段。
// Python: serialization.py:116-134
func TestErrorRoundTrip(t *testing.T) {
	code := "E1"
	category := "sdk_error"
	retryable := true
	e := TurnError{Message: "boom", Code: &code, Category: &category, Retryable: &retryable}
	d := errorToDict(e)
	assert.Equal(t, "boom", d["message"])
	assert.Equal(t, "E1", d["code"])

	back, err := errorFromDict(d, "turn_result.error")
	require.NoError(t, err)
	assert.Equal(t, "E1", *back.Code)
	assert.True(t, *back.Retryable)

	// nil 可空字段
	minimal := TurnError{Message: "boom"}
	d2 := errorToDict(minimal)
	assert.Nil(t, d2["code"])
}

// newTestModelRequestEvent 构造 spec 里的标准 ModelRequestEvent。
// Python: tests/unit_tests/harness_protocol/test_protocol.py:749-785
func newTestModelRequestEvent(t *testing.T) ModelRequestEvent {
	t.Helper()
	usageIn, usageOut, usageCached := 30, 5, 10
	ev, err := NewModelRequestEvent(ModelRequestOptions{
		RequestID:    "request-1",
		Status:       ModelRequestStatusCompleted,
		StartedAt:    10.0,
		EndedAt:      12.5,
		Model:        "model-a",
		ProviderName: "vendor-a",
		SystemInstructions: []ContentBlock{
			{BlockID: "system-0", Kind: "text", Content: "be brief", Data: map[string]any{}},
		},
		InputMessages: []TurnMessage{
			{
				MessageID: "user-1",
				Role:      MessageRoleUser,
				Content: []ContentBlock{
					{BlockID: "user-1:0", Kind: "text", Content: "list files", Data: map[string]any{}},
				},
				Data: map[string]any{"origin": "external_user"},
			},
		},
		InputObserved: true,
		OutputMessage: &TurnMessage{
			MessageID: "assistant-1",
			Role:      MessageRoleAssistant,
			Content: []ContentBlock{
				{BlockID: "assistant-1:0", Kind: "reasoning", Content: "look first", Data: map[string]any{}},
			},
		},
		ToolDefinitions:   []any{map[string]any{"name": "ls"}},
		RequestParameters: map[string]any{},
		Usage:             &TurnUsage{InputTokens: &usageIn, OutputTokens: &usageOut, CachedInputTokens: &usageCached},
		Data:              map[string]any{"vendor-a": map[string]any{"ttft_ms": 120}},
	})
	require.NoError(t, err)
	return ev
}

// TestModelRequestRoundTrip 验证最复杂载荷的往返。
// Python: serialization.py:137-197
func TestModelRequestRoundTrip(t *testing.T) {
	ev := newTestModelRequestEvent(t)
	d := modelRequestToDict(ev)
	assert.Equal(t, "request-1", d["request_id"])
	assert.Equal(t, "completed", d["status"])

	back, err := modelRequestFromDict(d)
	require.NoError(t, err)
	assert.Equal(t, ev.RequestID, back.RequestID)
	assert.Equal(t, ev.Status, back.Status)
	assert.Equal(t, ev.StartedAt, back.StartedAt)
	assert.Equal(t, ev.EndedAt, back.EndedAt)
	assert.Equal(t, ev.Model, back.Model)
	require.Len(t, back.SystemInstructions, 1)
	require.Len(t, back.InputMessages, 1)
	require.NotNil(t, back.OutputMessage)
	require.NotNil(t, back.Usage)
	assert.Equal(t, 30, *back.Usage.InputTokens)
}

// TestModelRequestFromDict_字段路径 验证悬空字段的错误前缀。
// Python: serialization.py:161-197（前缀 "model_request."）
func TestModelRequestFromDict_字段路径(t *testing.T) {
	base := func() map[string]any {
		return modelRequestToDict(newTestModelRequestEvent(t))
	}

	t.Run("cost 结构错误", func(t *testing.T) {
		d := base()
		d["cost"] = []any{}
		_, err := modelRequestFromDict(d)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model_request.cost")
	})

	t.Run("usage 结构错误", func(t *testing.T) {
		d := base()
		d["usage"] = []any{}
		_, err := modelRequestFromDict(d)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model_request.usage")
	})

	t.Run("request_parameters 非对象", func(t *testing.T) {
		d := base()
		d["request_parameters"] = []any{}
		_, err := modelRequestFromDict(d)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model_request.request_parameters")
	})
}

// TestTurnResultRoundTrip 验证终态结果往返（含 termination / cost 两可选）。
// Python: serialization.py:200-274
func TestTurnResultRoundTrip(t *testing.T) {
	micros := int64(125000)
	code := "E1"
	termMsg := "cancelled by user"
	completed, err := NewTurnResult(TurnStatusCompleted, ResultOptions{
		Messages: []TurnMessage{
			{MessageID: "m1", Role: MessageRoleAssistant, Content: []ContentBlock{}, Data: map[string]any{}},
		},
		FinalOutput: "partial",
		Cost:        &MonetaryAmount{Micros: micros, Currency: "USD"},
	})
	require.NoError(t, err)

	d := resultToDict(completed)
	assert.Equal(t, "completed", d["status"])
	assert.Equal(t, "partial", d["final_output"])

	back, err := resultFromDict(d)
	require.NoError(t, err)
	assert.Equal(t, TurnStatusCompleted, back.Status)
	require.NotNil(t, back.Cost)
	assert.Equal(t, micros, back.Cost.Micros)
	assert.Equal(t, "USD", back.Cost.Currency)

	// interrupted 带 termination
	interrupted, err := NewTurnResult(TurnStatusInterrupted, ResultOptions{
		Termination: &TurnTermination{
			Kind:    TurnTerminationKindUserAbort,
			Message: &termMsg,
			Code:    &code,
		},
	})
	require.NoError(t, err)
	d2 := resultToDict(interrupted)
	term, ok := d2["termination"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "user_abort", term["kind"])

	back2, err := resultFromDict(d2)
	require.NoError(t, err)
	require.NotNil(t, back2.Termination)
	assert.Equal(t, TurnTerminationKindUserAbort, back2.Termination.Kind)
	assert.Equal(t, "cancelled by user", *back2.Termination.Message)
}

// TestTurnResultFromDict_字段路径 验证 turn_result 前缀。
// Python: serialization.py:230-274
func TestTurnResultFromDict_字段路径(t *testing.T) {
	completed, err := NewTurnResult(TurnStatusCompleted, ResultOptions{})
	require.NoError(t, err)
	base := func() map[string]any { return resultToDict(completed) }

	t.Run("termination 结构错误", func(t *testing.T) {
		d := base()
		d["termination"] = []any{}
		_, err := resultFromDict(d)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "turn_result.termination")
	})

	t.Run("termination.kind 非法", func(t *testing.T) {
		d := base()
		d["termination"] = map[string]any{"kind": "bogus"}
		_, err := resultFromDict(d)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "turn_result.termination.kind")
	})

	t.Run("messages 非数组", func(t *testing.T) {
		d := base()
		d["messages"] = map[string]any{}
		_, err := resultFromDict(d)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "turn_result.messages")
	})

	t.Run("status 非法", func(t *testing.T) {
		d := base()
		d["status"] = "bogus"
		_, err := resultFromDict(d)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "turn_result.status")
	})
}

// asPointerPayload 把接口值转成其指针形态，用于覆盖 *T 分支。
// 与包内既有 reflectNew(覆盖率助手，签名带 *testing.T) 区分命名。
func asPointerPayload(v any) HarnessEventPayload {
	rv := reflect.ValueOf(v)
	p := reflect.New(rv.Type())
	p.Elem().Set(rv)
	return p.Interface().(HarnessEventPayload)
}

// mustOutputEvent 构造合法的 OutputEvent。
func mustOutputEvent(t *testing.T) OutputEvent {
	t.Helper()
	ev, err := NewOutputEvent("answer-1", OutputKindStructured,
		map[string]any{"answer": []any{1, 2}}, OutputOperationFinal,
		OutputChannelAnswer, 0, "", map[string]any{})
	require.NoError(t, err)
	return ev
}

// mustItemLifecycleEvent 构造合法的 ItemLifecycleEvent。
func mustItemLifecycleEvent(t *testing.T) ItemLifecycleEvent {
	t.Helper()
	ev, err := NewItemLifecycleEvent(ItemEventKindStarted, "tool", map[string]any{})
	require.NoError(t, err)
	return ev
}

// mustUsageUpdatedEvent 构造合法的 UsageUpdatedEvent。
func mustUsageUpdatedEvent(t *testing.T) UsageUpdatedEvent {
	t.Helper()
	in := 4
	usage, err := NewTurnUsage(&in, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	ev, err := NewUsageUpdatedEvent(usage, UsageUpdateModeDelta)
	require.NoError(t, err)
	return ev
}

// mustStateChangedEvent 构造合法的 StateChangedEvent。
func mustStateChangedEvent(t *testing.T) StateChangedEvent {
	t.Helper()
	ev, err := NewStateChangedEvent(HarnessStateIdle, HarnessStateRunning)
	require.NoError(t, err)
	return ev
}

// mustTurnLifecycleEvent 构造合法的非终态 TurnLifecycleEvent。
func mustTurnLifecycleEvent(t *testing.T) TurnLifecycleEvent {
	t.Helper()
	ev, err := NewTurnLifecycleEvent(TurnEventKindStarted, nil)
	require.NoError(t, err)
	return ev
}

// mustHookObservedEvent 构造合法的 HookObservedEvent。
func mustHookObservedEvent(t *testing.T) HookObservedEvent {
	t.Helper()
	ev, err := NewHookObservedEvent(HookEventPhaseStarted, "before_tool", "hook-1", map[string]any{})
	require.NoError(t, err)
	return ev
}

// mustDiagnosticEvent 构造合法的 DiagnosticEvent。
func mustDiagnosticEvent(t *testing.T) DiagnosticEvent {
	t.Helper()
	ev, err := NewDiagnosticEvent(DiagnosticLevelInfo, "ready", map[string]any{})
	require.NoError(t, err)
	return ev
}

// mustProviderEvent 构造合法的 ProviderEvent。
func mustProviderEvent(t *testing.T) ProviderEvent {
	t.Helper()
	ev, err := NewProviderEvent("vendor-a", "custom", "1", map[string]any{"k": "v"})
	require.NoError(t, err)
	return ev
}

// TestPayloadToWire_十载荷双形态 验证 10 个载荷的值与指针形态都能编码。
// 双形态是包内既定约定（events.go:454-500 EventRetentionOf 同理）。
// Python: serialization.py:277-359 _payload_to_wire（9 条 isinstance if）
func TestPayloadToWire_十载荷双形态(t *testing.T) {
	payloads := map[string]struct {
		value HarnessEventPayload
		want  string
	}{
		"output":         {mustOutputEvent(t), "output"},
		"item_lifecycle": {mustItemLifecycleEvent(t), "item_lifecycle"},
		"usage_updated":  {mustUsageUpdatedEvent(t), "usage_updated"},
		"model_request":  {newTestModelRequestEvent(t), "model_request"},
		"state_changed":  {mustStateChangedEvent(t), "state_changed"},
		"turn_lifecycle": {mustTurnLifecycleEvent(t), "turn_lifecycle"},
		"hook_observed":  {mustHookObservedEvent(t), "hook_observed"},
		"diagnostic":     {mustDiagnosticEvent(t), "diagnostic"},
		"provider":       {mustProviderEvent(t), "provider"},
	}
	for name, c := range payloads {
		t.Run(name+"_值形态", func(t *testing.T) {
			eventType, schemaVersion, payload, err := payloadToWire(c.value)
			require.NoError(t, err)
			assert.Equal(t, c.want, eventType)
			assert.Equal(t, EventWireSchemaVersion, schemaVersion)
			assert.NotNil(t, payload)
		})
		t.Run(name+"_指针形态", func(t *testing.T) {
			eventType, schemaVersion, payload, err := payloadToWire(asPointerPayload(c.value))
			require.NoError(t, err)
			assert.Equal(t, c.want, eventType)
			assert.Equal(t, EventWireSchemaVersion, schemaVersion)
			assert.NotNil(t, payload)
		})
	}
}

// TestPayloadToWire_UnknownEvent特判 验证 UnknownEvent 走自身 EventType
// 而非 payloadKind() 的 "unknown"。
// Python: serialization.py:357-358
func TestPayloadToWire_UnknownEvent特判(t *testing.T) {
	unknown, err := NewUnknownEvent("future_event", "2", map[string]any{"new_field": []any{1, 2, 3}})
	require.NoError(t, err)

	for _, payload := range []HarnessEventPayload{unknown, &unknown} {
		eventType, schemaVersion, body, err := payloadToWire(payload)
		require.NoError(t, err)
		assert.Equal(t, "future_event", eventType, "必须取自身 EventType，不能是 unknown")
		assert.Equal(t, "2", schemaVersion, "必须取自身 SchemaVersion")
		assert.Equal(t, []any{1, 2, 3}, body["new_field"])
	}
}

// TestPayloadToWire_未知类型报错 验证兜底。
// Python: serialization.py:359
func TestPayloadToWire_未知类型报错(t *testing.T) {
	_, _, _, err := payloadToWire(unknownPayloadKind{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported event payload type")
}

// unknownPayloadKind 测试用：实现 HarnessEventPayload 但不属于任何已知载荷。
type unknownPayloadKind struct{}

func (unknownPayloadKind) payloadKind() string { return "not-a-known-kind" }

// TestPayloadFromWire_九载荷往返 验证 9 个已知载荷解码后类型正确。
// Python: serialization.py:382-441
func TestPayloadFromWire_九载荷往返(t *testing.T) {
	cases := []struct {
		name      string
		payload   HarnessEventPayload
		assertTyp func(t *testing.T, got HarnessEventPayload)
	}{
		{"output", mustOutputEvent(t), func(t *testing.T, got HarnessEventPayload) {
			e, ok := got.(OutputEvent)
			if !ok {
				t.Fatalf("类型 = %T，期望 OutputEvent", got)
			}
			if e.OutputID != "answer-1" {
				t.Errorf("output_id = %q", e.OutputID)
			}
		}},
		{"item_lifecycle", mustItemLifecycleEvent(t), func(t *testing.T, got HarnessEventPayload) {
			if _, ok := got.(ItemLifecycleEvent); !ok {
				t.Fatalf("类型 = %T", got)
			}
		}},
		{"usage_updated", mustUsageUpdatedEvent(t), func(t *testing.T, got HarnessEventPayload) {
			e, ok := got.(UsageUpdatedEvent)
			if !ok {
				t.Fatalf("类型 = %T", got)
			}
			if e.Mode != UsageUpdateModeDelta {
				t.Errorf("mode = %q", e.Mode)
			}
		}},
		{"model_request", newTestModelRequestEvent(t), func(t *testing.T, got HarnessEventPayload) {
			e, ok := got.(ModelRequestEvent)
			if !ok {
				t.Fatalf("类型 = %T", got)
			}
			if e.RequestID != "request-1" {
				t.Errorf("request_id = %q", e.RequestID)
			}
		}},
		{"state_changed", mustStateChangedEvent(t), func(t *testing.T, got HarnessEventPayload) {
			e, ok := got.(StateChangedEvent)
			if !ok {
				t.Fatalf("类型 = %T", got)
			}
			if e.Old != HarnessStateIdle || e.New != HarnessStateRunning {
				t.Errorf("old=%q new=%q", e.Old, e.New)
			}
		}},
		{"turn_lifecycle", mustTurnLifecycleEvent(t), func(t *testing.T, got HarnessEventPayload) {
			e, ok := got.(TurnLifecycleEvent)
			if !ok {
				t.Fatalf("类型 = %T", got)
			}
			if e.Kind != TurnEventKindStarted {
				t.Errorf("kind = %q", e.Kind)
			}
		}},
		{"hook_observed", mustHookObservedEvent(t), func(t *testing.T, got HarnessEventPayload) {
			e, ok := got.(HookObservedEvent)
			if !ok {
				t.Fatalf("类型 = %T", got)
			}
			if e.HookName != "before_tool" {
				t.Errorf("hook_name = %q", e.HookName)
			}
		}},
		{"diagnostic", mustDiagnosticEvent(t), func(t *testing.T, got HarnessEventPayload) {
			e, ok := got.(DiagnosticEvent)
			if !ok {
				t.Fatalf("类型 = %T", got)
			}
			if e.Level != DiagnosticLevelInfo {
				t.Errorf("level = %q", e.Level)
			}
		}},
		{"provider", mustProviderEvent(t), func(t *testing.T, got HarnessEventPayload) {
			e, ok := got.(ProviderEvent)
			if !ok {
				t.Fatalf("类型 = %T", got)
			}
			if e.Provider != "vendor-a" {
				t.Errorf("provider = %q", e.Provider)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eventType, schemaVersion, body, err := payloadToWire(c.payload)
			require.NoError(t, err)
			got, err := payloadFromWire(eventType, schemaVersion, body)
			require.NoError(t, err)
			c.assertTyp(t, got)
		})
	}
}

// TestPayloadFromWire_未知字段保留 验证两条 UnknownEvent 出口。
// Python: serialization.py:383-384（schema 不匹配）、:442（未知 event_type）
func TestPayloadFromWire_未知字段保留(t *testing.T) {
	t.Run("schema_version 不匹配早退", func(t *testing.T) {
		got, err := payloadFromWire("output", "2", map[string]any{"whatever": "x"})
		require.NoError(t, err)
		unknown, ok := got.(UnknownEvent)
		require.True(t, ok, "类型 = %T，期望 UnknownEvent", got)
		assert.Equal(t, "output", unknown.EventType)
		assert.Equal(t, "2", unknown.SchemaVersion)
		assert.Equal(t, "x", unknown.Payload["whatever"])
	})

	t.Run("未知 event_type 兜底", func(t *testing.T) {
		got, err := payloadFromWire("future_event", "1", map[string]any{"new_field": []any{1, 2, 3}})
		require.NoError(t, err)
		unknown, ok := got.(UnknownEvent)
		require.True(t, ok)
		assert.Equal(t, "future_event", unknown.EventType)
		assert.Equal(t, "1", unknown.SchemaVersion)
		assert.Equal(t, []any{1, 2, 3}, unknown.Payload["new_field"])
	})
}

// TestHarnessEventRoundTrip 验证完整事件往返 + 线格式不携带 team 私名。
// Python: test_protocol.py:743-775
func TestHarnessEventRoundTrip(t *testing.T) {
	ev, err := NewHarnessEvent(HarnessEventOptions{
		Sequence:          7,
		Timestamp:         7.5,
		Event:             mustOutputEvent(t),
		HostSessionID:     "team-session-1",
		AgentID:           "team-a_member-a",
		ProviderSessionID: "provider-session-1",
		TurnID:            "turn-1",
		CorrelationID:     "trace-1",
		CausationIDs:      []string{"message-1"},
	})
	require.NoError(t, err)

	wire := HarnessEventToDict(ev)

	// 线格式键集合
	assert.Equal(t, "team-session-1", wire["host_session_id"])
	assert.Equal(t, "team-a_member-a", wire["agent_id"])
	assert.Equal(t, "provider-session-1", wire["provider_session_id"])
	assert.Equal(t, "trace-1", wire["correlation_id"])
	assert.Equal(t, 7, wire["sequence"])
	assert.Equal(t, 7.5, wire["timestamp"])

	// 不得出现 team 私名（对齐上游 isdisjoint 断言）
	for _, forbidden := range []string{"team_session_id", "member_agent_id", "session_id"} {
		_, present := wire[forbidden]
		assert.False(t, present, "线格式不得携带 %q", forbidden)
	}

	back, err := HarnessEventFromDict(wire)
	require.NoError(t, err)
	assert.Equal(t, ev.Sequence, back.Sequence)
	assert.Equal(t, ev.Timestamp, back.Timestamp)
	assert.Equal(t, ev.HostSessionID, back.HostSessionID)
	assert.Equal(t, ev.AgentID, back.AgentID)
	assert.Equal(t, ev.ProviderSessionID, back.ProviderSessionID)
	assert.Equal(t, ev.TurnID, back.TurnID)
	assert.Equal(t, ev.CorrelationID, back.CorrelationID)
	assert.Equal(t, ev.CausationIDs, back.CausationIDs)
	out, ok := back.Event.(OutputEvent)
	require.True(t, ok)
	assert.Equal(t, "answer-1", out.OutputID)
}

// TestHarnessEventUnknownEvent往返 验证未知事件经完整信封往返后仍能原样发出。
// Python: test_protocol.py:769-775
func TestHarnessEventUnknownEvent往返(t *testing.T) {
	ev, err := NewHarnessEvent(HarnessEventOptions{
		Sequence:      1,
		Timestamp:     1.0,
		Event:         mustDiagnosticEvent(t),
		HostSessionID: "s",
		AgentID:       "a",
	})
	require.NoError(t, err)

	wire := HarnessEventToDict(ev)
	// 篡改成未知事件类型与新载荷
	wire["event_type"] = "future_event"
	wire["payload"] = map[string]any{"new_field": []any{1, 2, 3}}

	decoded, err := HarnessEventFromDict(wire)
	require.NoError(t, err)
	unknown, ok := decoded.Event.(UnknownEvent)
	require.True(t, ok, "类型 = %T，期望 UnknownEvent", decoded.Event)
	assert.Equal(t, []any{1, 2, 3}, unknown.Payload["new_field"])

	// 再编码回去，event_type 保持不变
	rewire := HarnessEventToDict(decoded)
	assert.Equal(t, "future_event", rewire["event_type"])
}

// TestHarnessEventFromDict_错误包装 验证字段缺失时报 invalid harness event wire object。
// Python: serialization.py:448-467
func TestHarnessEventFromDict_错误包装(t *testing.T) {
	t.Run("event_type 缺失", func(t *testing.T) {
		_, err := HarnessEventFromDict(map[string]any{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid harness event wire object")
	})

	t.Run("payload 非对象", func(t *testing.T) {
		_, err := HarnessEventFromDict(map[string]any{
			"event_type":      "diagnostic",
			"schema_version":  "1",
			"payload":         []any{},
			"sequence":        1,
			"timestamp":       1.0,
			"host_session_id": "s",
			"agent_id":        "a",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "payload must be a JSON object")
	})

	t.Run("causation_ids 含空串", func(t *testing.T) {
		_, err := HarnessEventFromDict(map[string]any{
			"event_type":      "diagnostic",
			"schema_version":  "1",
			"payload":         map[string]any{"level": "info", "message": "x", "data": map[string]any{}},
			"sequence":        1,
			"timestamp":       1.0,
			"host_session_id": "s",
			"agent_id":        "a",
			"causation_ids":   []any{""},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "causation_id must be a non-empty string")
	})
}

// TestHarnessEventToDict_JSON可序列化 验证输出能被 json.Marshal。
// Python: test_protocol.py:762（json.dumps(wire) 不抛）
func TestHarnessEventToDict_JSON可序列化(t *testing.T) {
	ev, err := NewHarnessEvent(HarnessEventOptions{
		Sequence:      1,
		Timestamp:     1.0,
		Event:         newTestModelRequestEvent(t),
		HostSessionID: "s",
		AgentID:       "a",
		TurnID:        "turn-1",
	})
	require.NoError(t, err)
	_, err = json.Marshal(HarnessEventToDict(ev))
	require.NoError(t, err, "线格式必须可直接 json.Marshal")
}
