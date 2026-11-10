package harness_protocol

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewContentBlock_两字段非空 验证 block_id / kind 必填。
// Python: results.py:47-60
func TestNewContentBlock_两字段非空(t *testing.T) {
	_, err := NewContentBlock("", "text", "hi", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id and kind must not be empty")

	_, err = NewContentBlock("b1", "", "hi", nil)
	require.Error(t, err)

	b, err := NewContentBlock("b1", "text", "hi", map[string]any{"k": "v"})
	require.NoError(t, err)
	assert.Equal(t, "hi", b.Content)
}

// TestNewTurnMessage_messageID非空 验证 id 必填与 content 拷贝。
// Python: results.py:63-76
func TestNewTurnMessage_messageID非空(t *testing.T) {
	_, err := NewTurnMessage("", MessageRoleUser, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "turn message_id must not be empty")

	block, _ := NewContentBlock("b1", "text", "hi", nil)
	m, err := NewTurnMessage("m1", MessageRoleAssistant, []ContentBlock{block}, nil)
	require.NoError(t, err)
	assert.Len(t, m.Content, 1)
}

// TestNewTurnTermination_冻结providerData 验证 provider_data 冻结。
// Python: results.py:79-89
func TestNewTurnTermination_冻结providerData(t *testing.T) {
	src := map[string]any{"k": []any{1}}
	term, err := NewTurnTermination(TurnTerminationKindTimeout, nil, nil, src)
	require.NoError(t, err)
	src["k"].([]any)[0] = 999
	assert.Equal(t, []any{1}, term.ProviderData["k"])
}

// TestNewMonetaryAmount_micros非负与货币码 验证两条规则。
// Python: results.py:92-105
func TestNewMonetaryAmount_micros非负与货币码(t *testing.T) {
	_, err := NewMonetaryAmount(-1, "USD")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "micros must be non-negative")

	_, err = NewMonetaryAmount(1, "US")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "three-letter code")

	_, err = NewMonetaryAmount(1, "U1D")
	require.Error(t, err)

	a, err := NewMonetaryAmount(1500, "usd")
	require.NoError(t, err)
	assert.Equal(t, "USD", a.Currency)
	assert.Equal(t, int64(1500), a.Micros)
}

// TestNewTurnUsage_计数非负 验证 5 个计数均需非负。
// Python: results.py:108-129
func TestNewTurnUsage_计数非负(t *testing.T) {
	neg := -1
	_, err := NewTurnUsage(&neg, nil, nil, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "counters must be non-negative")

	in, out := 10, 20
	u, err := NewTurnUsage(&in, &out, nil, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 10, *u.InputTokens)
	assert.Equal(t, 20, *u.OutputTokens)
}

// TestNewTurnError_message非空 验证 message 必填。
// Python: results.py:132-145
func TestNewTurnError_message非空(t *testing.T) {
	_, err := NewTurnError("", nil, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "turn error message must not be empty")

	e, err := NewTurnError("boom", strPtr("E1"), strPtr("provider"), boolPtr(true), nil)
	require.NoError(t, err)
	assert.Equal(t, "boom", e.Message)
}

// TestNewTurnResult_failed必须有error 验证互斥规则一。
// Python: results.py:175-178 "failed turn result requires error"
func TestNewTurnResult_failed必须有error(t *testing.T) {
	_, err := NewTurnResult(TurnStatusFailed, ResultOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed turn result requires error")
}

// TestNewTurnResult_非failed不得有error 验证互斥规则二。
// Python: results.py:177-178 "only failed turn result may contain error"
func TestNewTurnResult_非failed不得有error(t *testing.T) {
	te, _ := NewTurnError("boom", nil, nil, nil, nil)
	_, err := NewTurnResult(TurnStatusCompleted, ResultOptions{Error: &te})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only failed turn result may contain error")
}

// TestNewTurnResult_interrupted必须有termination 验证互斥规则三。
// Python: results.py:179-180 "interrupted turn result requires termination"
func TestNewTurnResult_interrupted必须有termination(t *testing.T) {
	_, err := NewTurnResult(TurnStatusInterrupted, ResultOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interrupted turn result requires termination")
}

// TestNewTurnResult_非interrupted不得有termination 验证互斥规则四。
// Python: results.py:181-182 "only interrupted turn result may contain termination"
func TestNewTurnResult_非interrupted不得有termination(t *testing.T) {
	term, _ := NewTurnTermination(TurnTerminationKindUserAbort, nil, nil, nil)
	_, err := NewTurnResult(TurnStatusCompleted, ResultOptions{Termination: &term})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only interrupted turn result may contain termination")
}

// TestNewTurnResult_时间戳校验 验证有限、非负、completed>=started。
// Python: results.py:183-189
func TestNewTurnResult_时间戳校验(t *testing.T) {
	neg := -1.0
	_, err := NewTurnResult(TurnStatusCompleted, ResultOptions{StartedAt: &neg})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "finite Unix timestamp")

	start, end := 100.0, 50.0
	_, err = NewTurnResult(TurnStatusCompleted, ResultOptions{StartedAt: &start, CompletedAt: &end})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not precede started_at")

	bad := -1
	_, err = NewTurnResult(TurnStatusCompleted, ResultOptions{DurationMS: &bad})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duration_ms must be non-negative")
}

// TestNewTurnResult_完整正例 验证终态与便捷投影齐备。
// Python: results.py:148-189 + test_protocol.py:609
func TestNewTurnResult_完整正例(t *testing.T) {
	block, _ := NewContentBlock("b1", "text", "done", nil)
	msg, _ := NewTurnMessage("m1", MessageRoleAssistant, []ContentBlock{block}, nil)
	in := 10
	usage, _ := NewTurnUsage(&in, nil, nil, nil, nil, nil)
	cost, _ := NewMonetaryAmount(1200, "USD")
	start, end := 100.0, 101.5
	dur := 1500

	r, err := NewTurnResult(TurnStatusCompleted, ResultOptions{
		Messages:         []TurnMessage{msg},
		FinalOutput:      "done",
		StructuredOutput: map[string]any{"ok": true},
		StopReason:       strPtr("end_turn"),
		Usage:            &usage,
		Cost:             &cost,
		StartedAt:        &start,
		CompletedAt:      &end,
		DurationMS:       &dur,
	})
	require.NoError(t, err)
	assert.Equal(t, TurnStatusCompleted, r.Status)
	assert.Equal(t, "done", r.FinalOutput)
	assert.Len(t, r.Messages, 1)
	assert.Equal(t, "end_turn", *r.StopReason)
	assert.Equal(t, int64(1200), r.Cost.Micros)
	assert.Equal(t, 1500, *r.DurationMS)
}

// ──────────── 测试助手 ────────────

// strPtr 返回字符串指针。
func strPtr(s string) *string { return &s }

// boolPtr 返回布尔指针。
func boolPtr(b bool) *bool { return &b }

// TestHarnessProtocolError_Error与Is 验证基类错误文案与哨兵归类。
// Python: errors.py:21-22 HarnessProtocolError
func TestHarnessProtocolError_Error与Is(t *testing.T) {
	e := &HarnessProtocolError{Msg: "invariant broken"}
	assert.Equal(t, "invariant broken", e.Error())
	assert.True(t, errors.Is(e, ErrHarnessProtocol))
	assert.True(t, errors.Is(e, ErrHarness))
	assert.False(t, errors.Is(e, ErrEventStreamDone))
}

// TestNewTurnResult_providerData冻结 验证 provider_data 深拷贝。
// Python: results.py:174
func TestNewTurnResult_providerData冻结(t *testing.T) {
	src := map[string]any{"k": []any{1}}
	r, err := NewTurnResult(TurnStatusCompleted, ResultOptions{ProviderData: src})
	require.NoError(t, err)
	src["k"].([]any)[0] = 999
	assert.Equal(t, []any{1}, r.ProviderData["k"])
}

// TestNewTurnResult_非JSON输出报错 验证 final/structured 冻结失败上抛。
// Python: results.py:172-173
func TestNewTurnResult_非JSON输出报错(t *testing.T) {
	_, err := NewTurnResult(TurnStatusCompleted, ResultOptions{FinalOutput: math.NaN()})
	require.Error(t, err)

	_, err = NewTurnResult(TurnStatusCompleted, ResultOptions{StructuredOutput: struct{}{}})
	require.Error(t, err)
}

// TestNewTurnUsage_冻结providerData 验证 provider_data 深拷贝。
// Python: results.py:129
func TestNewTurnUsage_冻结providerData(t *testing.T) {
	src := map[string]any{"k": []any{1}}
	u, err := NewTurnUsage(nil, nil, nil, nil, nil, src)
	require.NoError(t, err)
	src["k"].([]any)[0] = 999
	assert.Equal(t, []any{1}, u.ProviderData["k"])
}

// TestNewTurnError_冻结providerData 验证 provider_data 深拷贝。
// Python: results.py:145
func TestNewTurnError_冻结providerData(t *testing.T) {
	src := map[string]any{"k": []any{1}}
	e, err := NewTurnError("boom", nil, nil, nil, src)
	require.NoError(t, err)
	src["k"].([]any)[0] = 999
	assert.Equal(t, []any{1}, e.ProviderData["k"])
}

// TestNewContentBlock_冻结data 验证 data 深拷贝。
// Python: results.py:60
func TestNewContentBlock_冻结data(t *testing.T) {
	src := map[string]any{"k": []any{1}}
	b, err := NewContentBlock("b1", "text", "hi", src)
	require.NoError(t, err)
	src["k"].([]any)[0] = 999
	assert.Equal(t, []any{1}, b.Data["k"])
}

// TestNewTurnMessage_冻结data 验证 data 深拷贝。
// Python: results.py:76
func TestNewTurnMessage_冻结data(t *testing.T) {
	src := map[string]any{"k": []any{1}}
	m, err := NewTurnMessage("m1", MessageRoleTool, nil, src)
	require.NoError(t, err)
	src["k"].([]any)[0] = 999
	assert.Equal(t, []any{1}, m.Data["k"])
}

// TestNewContentBlock_content非法 验证 content 冻结失败上抛。
// Python: results.py:59
func TestNewContentBlock_content非法(t *testing.T) {
	_, err := NewContentBlock("b1", "text", math.NaN(), nil)
	require.Error(t, err)
}

// TestNewTurnTermination_非法providerData 验证冻结失败上抛。
// Python: results.py:89
func TestNewTurnTermination_非法providerData(t *testing.T) {
	_, err := NewTurnTermination(TurnTerminationKindTimeout, nil, nil, map[string]any{"k": math.NaN()})
	require.Error(t, err)
}

// TestNewTurnError_非法providerData 验证冻结失败上抛。
// Python: results.py:145
func TestNewTurnError_非法providerData(t *testing.T) {
	_, err := NewTurnError("boom", nil, nil, nil, map[string]any{"k": math.NaN()})
	require.Error(t, err)
}

// TestNewTurnUsage_非法providerData 验证冻结失败上抛。
// Python: results.py:129
func TestNewTurnUsage_非法providerData(t *testing.T) {
	_, err := NewTurnUsage(nil, nil, nil, nil, nil, map[string]any{"k": math.NaN()})
	require.Error(t, err)
}

// TestNewTurnMessage_非法data 验证冻结失败上抛。
// Python: results.py:76
func TestNewTurnMessage_非法data(t *testing.T) {
	_, err := NewTurnMessage("m1", MessageRoleUser, nil, map[string]any{"k": math.NaN()})
	require.Error(t, err)
}
