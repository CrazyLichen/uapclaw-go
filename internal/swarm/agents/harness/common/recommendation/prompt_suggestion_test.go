package recommendation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 用例：switches ───────────────────────────────

// TestPromptSuggestionEnabled_默认关闭
// Python: common/recommendation_switches.py:79 + :26 DEFAULT=False
func TestPromptSuggestionEnabled_默认关闭(t *testing.T) {
	assert.False(t, PromptSuggestionEnabled(nil))
	assert.False(t, PromptSuggestionEnabled(map[string]any{}))
	assert.False(t, PromptSuggestionEnabled(map[string]any{"recommendation": map[string]any{}}))
	assert.True(t, PromptSuggestionEnabled(map[string]any{
		"recommendation": map[string]any{"prompt_enabled": true}}))
	assert.False(t, PromptSuggestionEnabled(map[string]any{
		"recommendation": map[string]any{"prompt_enabled": false}}))
}

// TestResolveRecommendationSwitch_四写法与回落
// Python: recommendation_switches.py:44 resolve_recommendation_switch
func TestResolveRecommendationSwitch_四写法与回落(t *testing.T) {
	cases := []struct {
		value any
		want  bool
	}{
		{true, true},
		{false, false},
		{nil, false},
		{"1", true},
		{"true", true},
		{"yes", true},
		{"on", true},
		{" TRUE ", true},
		{"0", false},
		{"false", false},
		{"no", false},
		{"off", false},
		{"garbage", false},
		{123, false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, ResolveRecommendationSwitch(c.value, false), "value=%v", c.value)
	}
	assert.True(t, ResolveRecommendationSwitch(nil, true), "null 回落传入的默认值")
}

// TestConsentEpoch_关闭时不活跃 None 等价：开关关 → Active=false，值不消费。
// Python: prompt_suggestion.py:150-160 consent_epoch -> None when off
func TestConsentEpoch_关闭时不活跃(t *testing.T) {
	assert.False(t, ConsentEpoch(nil).Active)
	assert.False(t, ConsentEpoch(map[string]any{}).Active)
}

// TestConsentEpoch_开启且无consent记录 开启 + 无 consent 表 → epoch=0.0（Python 的 or 0.0）。
func TestConsentEpoch_开启且无consent记录(t *testing.T) {
	resetWorkspaceForTest(t)
	epoch := ConsentEpoch(map[string]any{"recommendation": map[string]any{"prompt_enabled": true}})
	assert.True(t, epoch.Active)
	assert.Equal(t, 0.0, epoch.Value)
}

// TestConsentEpochValue_Equal None 与值不等；值按相等比较；两个 None 相等。
func TestConsentEpochValue_Equal(t *testing.T) {
	assert.True(t, ConsentEpochValue{}.Equal(ConsentEpochValue{}))
	assert.False(t, ConsentEpochValue{}.Equal(ConsentEpochValue{Value: 1.0, Active: true}))
	assert.False(t, ConsentEpochValue{Value: 0.0, Active: true}.Equal(ConsentEpochValue{Value: 1.0, Active: true}))
	assert.True(t, ConsentEpochValue{Value: 0.0, Active: true}.Equal(ConsentEpochValue{Value: 0.0, Active: true}))
}

// ──────────────────────────── 用例：Forks ───────────────────────────────

// enabledConfig 返回一份开启推荐的配置。
func enabledConfig() map[string]any {
	return map[string]any{"recommendation": map[string]any{"prompt_enabled": true}}
}

// newEnabledForks 返回已注入开启配置的登记台。
func newEnabledForks() *PromptSuggestionForks {
	forks := NewPromptSuggestionForks()
	forks.SetConfig(enabledConfig())
	return forks
}

// TestForks_RequestStarted_人驱动轮才登记 对齐 :360-370。
func TestForks_RequestStarted_人驱动轮才登记(t *testing.T) {
	f := newEnabledForks()
	f.RequestStarted("s1", true)
	assert.True(t, f.hasTurnForTest("s1"))

	f.RequestStarted("s1", false)
	assert.False(t, f.hasTurnForTest("s1"), "非人驱动轮不登记")
}

// TestForks_RequestStarted_取消上一条在飞 对齐 :362-365。
func TestForks_RequestStarted_取消上一条在飞(t *testing.T) {
	f := newEnabledForks()
	cancelled := false
	f.setTaskForTest("s1", func() { cancelled = true })
	f.RequestStarted("s1", true)
	assert.True(t, cancelled)
	assert.Equal(t, 0, f.inFlightCountForTest())
}

// TestForks_SawMainCall_仅登记在跑的轮 对齐 :372-374。
func TestForks_SawMainCall_仅登记在跑的轮(t *testing.T) {
	f := newEnabledForks()
	f.SawMainCall("s1", &MainCall{Model: "m"})
	assert.Nil(t, f.TurnForTest("s1"), "未 RequestStarted 的会话不登记")

	f.RequestStarted("s1", true)
	f.SawMainCall("s1", &MainCall{Model: "m"})
	require.NotNil(t, f.TurnForTest("s1"))
	assert.Equal(t, "m", f.TurnForTest("s1").Model)
}

// TestForks_AnsweredCall_requestID不匹配返回nil 对齐 :412-415。
func TestForks_AnsweredCall_requestID不匹配返回nil(t *testing.T) {
	f := newEnabledForks()
	f.setAnsweredForTest("s1", "req-1", &MainCall{Model: "m"})
	assert.Nil(t, f.AnsweredCall("s1", "req-2"))
	require.NotNil(t, f.AnsweredCall("s1", "req-1"))
}

// TestForks_CancelSuggestion 对齐 :420-425。
func TestForks_CancelSuggestion(t *testing.T) {
	f := newEnabledForks()
	assert.False(t, f.CancelSuggestion("s1"), "无在飞返回 false")
	f.setTaskForTest("s1", func() {})
	assert.True(t, f.CancelSuggestion("s1"))
}

// TestForks_SessionDeleted_清两个表并取消在飞 对齐 :428-434。
func TestForks_SessionDeleted_清两个表并取消在飞(t *testing.T) {
	f := newEnabledForks()
	cancelled := false
	f.RequestStarted("s1", true)
	f.setAnsweredForTest("s1", "req-1", &MainCall{Model: "m"})
	f.setTaskForTest("s1", func() { cancelled = true })

	f.SessionDeleted("s1")
	assert.True(t, cancelled)
	assert.False(t, f.hasTurnForTest("s1"))
	assert.Nil(t, f.AnsweredCall("s1", "req-1"))
	assert.Equal(t, 0, f.inFlightCountForTest())
}

// TestForks_Shutdown_取消全部在飞 对齐 :436-441（无 error）。
func TestForks_Shutdown_取消全部在飞(t *testing.T) {
	f := newEnabledForks()
	count := 0
	f.setTaskForTest("s1", func() { count++ })
	f.setTaskForTest("s2", func() { count++ })
	f.Shutdown()
	assert.Equal(t, 2, count)
	assert.Equal(t, 0, f.inFlightCountForTest())
}

// TestForks_TurnEnded_七条抑制原因 逐条对齐 :390-408。
func TestForks_TurnEnded_七条抑制原因(t *testing.T) {
	cases := []struct {
		name string
		call *MainCall
		opts TurnEndOptions
		want string
	}{
		{"aborted", &MainCall{}, TurnEndOptions{Status: "cancelled"}, "aborted"},
		{"last_response_error", &MainCall{}, TurnEndOptions{Status: "error"}, "last_response_error"},
		{"pending_interaction", &MainCall{}, TurnEndOptions{Status: "success", WaitingForUser: true}, "pending_interaction"},
		{"no_model_call", nil, TurnEndOptions{Status: "success"}, "no_model_call"},
		{"plan_mode", &MainCall{Planning: true}, TurnEndOptions{Status: "success"}, "plan_mode"},
		{"unanswered_tool_call", &MainCall{Answer: toolCallAnswer()}, TurnEndOptions{Status: "success"}, "unanswered_tool_call"},
		{"cache_cold", &MainCall{Answer: answerWithOutputTokens(MaxForkAnswerTokens + 1)}, TurnEndOptions{Status: "success"}, "cache_cold"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetWorkspaceForTest(t)
			f := newEnabledForks()
			f.RequestStarted("s1", true)
			if c.call != nil {
				f.SawMainCall("s1", c.call)
			}
			f.TurnEnded("s1", "req-1", c.opts)
			assert.Equal(t, 0, f.inFlightCountForTest(), "抑制分支不得起任务")
			assert.Equal(t, c.want, f.lastSuppressedReasonForTest())
			assert.False(t, f.hasTurnForTest("s1"), "轮已出队")
		})
	}
}

// TestForks_TurnEnded_成功分支起任务并收敛 对齐 :404-408。
func TestForks_TurnEnded_成功分支起任务并收敛(t *testing.T) {
	resetWorkspaceForTest(t)
	f := newEnabledForks()
	f.RequestStarted("s1", true)
	f.SawMainCall("s1", &MainCall{Model: "m", Answer: answerWithOutputTokens(20)})
	f.TurnEnded("s1", "req-1", TurnEndOptions{Status: "success"})
	assert.Equal(t, "", f.lastSuppressedReasonForTest(), "成功分支无抑制原因")
	f.waitInFlightForTest(2 * time.Second)
	assert.Equal(t, 0, f.inFlightCountForTest(), "在飞任务最终收敛")
}

// TestForks_TurnEnded_推荐关闭时不起任务 对齐 :386-388。
func TestForks_TurnEnded_推荐关闭时不起任务(t *testing.T) {
	resetWorkspaceForTest(t)
	f := NewPromptSuggestionForks() // 未注入配置 = 默认关闭
	f.RequestStarted("s1", true)
	f.SawMainCall("s1", &MainCall{Model: "m"})
	f.TurnEnded("s1", "req-1", TurnEndOptions{Status: "success"})
	assert.Equal(t, 0, f.inFlightCountForTest(), "关闭时不起生成任务")
	// 关键：Python 的 _answered 写入在 consent_epoch 检查之前（:382-385 → :386），
	// 所以推荐关闭时仍保留 answered 记录，只是不生成建议。
	require.NotNil(t, f.AnsweredCall("s1", "req-1"), "关闭时仍记录 answered（对齐 :382-385）")
}
