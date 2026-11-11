package recommendation

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
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

// ──────────────────────────── 用例：LatestCompletedRequest ───────────────────────────────

// writeHistoryRows 测试助手：同步写若干行（读改写 history.json，不经异步队列）。
func writeHistoryRows(t *testing.T, sid string, rows ...map[string]any) {
	t.Helper()
	dir := filepath.Join(session.GetSessionsDir(), sid)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	existing, _ := session.ReadHistoryRecords(sid)
	data, err := utils.MarshalIndentNoEscape(append(existing, rows...))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "history.json"), data, 0o644))
}

// TestLatestCompletedRequest_末尾成功行 末尾完成行 success 且其后无他轮 → 返回该 rid。
func TestLatestCompletedRequest_末尾成功行(t *testing.T) {
	resetWorkspaceForTest(t)
	sid := "ps-latest-1"
	writeHistoryRows(t, sid,
		map[string]any{"role": "user", "request_id": "r1", "content": "hi"},
		map[string]any{"role": "assistant", "request_id": "r1", "content": "yo"},
		map[string]any{"role": "assistant", "request_id": "r1", "content": "",
			"event_type": string(swarmschema.EventTypeSessionRequestCompleted), "status": "success", "feedback_only": true},
	)
	assert.Equal(t, "r1", LatestCompletedRequest(sid))
}

// TestLatestCompletedRequest_其后有新轮 完成行之后又出现别的 request → 过期返回空。
func TestLatestCompletedRequest_其后有新轮(t *testing.T) {
	resetWorkspaceForTest(t)
	sid := "ps-latest-2"
	writeHistoryRows(t, sid,
		map[string]any{"role": "assistant", "request_id": "r1", "content": "",
			"event_type": string(swarmschema.EventTypeSessionRequestCompleted), "status": "success", "feedback_only": true},
		map[string]any{"role": "user", "request_id": "r2", "content": "next"},
	)
	assert.Equal(t, "", LatestCompletedRequest(sid))
}

// TestLatestCompletedRequest_末尾非success 末尾完成行 error → 空。
func TestLatestCompletedRequest_末尾非success(t *testing.T) {
	resetWorkspaceForTest(t)
	sid := "ps-latest-3"
	writeHistoryRows(t, sid,
		map[string]any{"role": "assistant", "request_id": "r1", "content": "",
			"event_type": string(swarmschema.EventTypeSessionRequestCompleted), "status": "error", "feedback_only": true},
	)
	assert.Equal(t, "", LatestCompletedRequest(sid))
}

// TestLatestCompletedRequest_无完成行 空历史 → 空。
func TestLatestCompletedRequest_无完成行(t *testing.T) {
	resetWorkspaceForTest(t)
	assert.Equal(t, "", LatestCompletedRequest("ps-latest-4"))
}

// ──────────────────────────── 用例：recordLedgerEvent / usagePayload ───────────────────────────────

// TestRecordLedgerEvent_落库与公共字段 epoch 有效时写入，公共字段齐全。
func TestRecordLedgerEvent_落库与公共字段(t *testing.T) {
	resetWorkspaceForTest(t)
	f := newEnabledForks()
	epoch := ConsentEpoch(f.config)

	f.recordLedgerEvent(epoch, "prompt_suggestion_generation", "s1", "r1", "sug1",
		map[string]any{"outcome": "candidate", "text": "hi"})

	store, err := NewTopicStarterStore(filepath.Join(workspaceDirForTest(), ConsentDBName))
	require.NoError(t, err)
	defer store.Close()
	rows, err := store.QueryEvents(QueryEventsOptions{EventType: "prompt_suggestion_generation", Limit: 5})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "s1", rows[0]["session_id"])
	assert.Equal(t, "prompt-suggestion:r1", rows[0]["slate_id"])
	assert.Equal(t, "prompt-suggestion:sug1", rows[0]["topic_id"])
	assert.Equal(t, PolicyID, rows[0]["policy_id"])
	assert.Equal(t, "prompt_suggestion", rows[0]["selection_mode"])
	assert.Equal(t, "not_estimated", rows[0]["propensity_reason"])
	payload, _ := rows[0]["payload"].(map[string]any)
	assert.Equal(t, "sug1", payload["suggestion_id"])
	assert.Equal(t, "s1", payload["session_id"])
	assert.Equal(t, "r1", payload["request_id"])
	assert.Equal(t, PromptVersion, payload["prompt_version"])
	assert.Equal(t, "candidate", payload["outcome"])
	assert.Equal(t, "hi", payload["text"])
}

// TestRecordLedgerEvent_epoch失效不写 epoch 变化后静默不写。
func TestRecordLedgerEvent_epoch失效不写(t *testing.T) {
	resetWorkspaceForTest(t)
	f := newEnabledForks()
	stale := ConsentEpoch(f.config)
	stale.Value = 999.0 // 模拟 epoch 已变

	f.recordLedgerEvent(stale, "prompt_suggestion_generation", "s1", "r1", "sug1",
		map[string]any{"outcome": "candidate"})

	store, err := NewTopicStarterStore(filepath.Join(workspaceDirForTest(), ConsentDBName))
	require.NoError(t, err)
	defer store.Close()
	rows, _ := store.QueryEvents(QueryEventsOptions{EventType: "prompt_suggestion_generation", Limit: 5})
	assert.Empty(t, rows)
}

// TestRecordLedgerEvent_写失败不炸 坏路径只 Warn 不 panic（账本失败不伤主流程）。
func TestRecordLedgerEvent_写失败不炸(t *testing.T) {
	f := NewPromptSuggestionForks()
	f.SetConfig(enabledConfig())
	epoch := ConsentEpoch(f.config)
	// 注入一个不存在的目录作工作区，NewTopicStarterStore 报错路径
	t.Setenv("UAPCLAW_DATA_DIR", filepath.Join(t.TempDir(), "no-such", "deep"))
	path.ResetCache()
	t.Cleanup(path.ResetCache)
	assert.NotPanics(t, func() {
		f.recordLedgerEvent(epoch, "prompt_suggestion_generation", "s1", "r1", "sug1",
			map[string]any{"outcome": "candidate"})
	})
}

// TestUsagePayload_形状 有用量时字段齐全；无用量 nil。
func TestUsagePayload_形状(t *testing.T) {
	reasoning := 3
	msg := &llmschema.AssistantMessage{UsageMetadata: &llmschema.UsageMetadata{
		InputTokens: 10, OutputTokens: 4, TotalTokens: 14, CacheTokens: 8, ReasoningTokens: &reasoning,
	}}
	payload := usagePayload(msg)
	require.NotNil(t, payload)
	assert.Equal(t, 10, payload["prompt_tokens"])
	assert.Equal(t, 4, payload["completion_tokens"])
	assert.Equal(t, 14, payload["total_tokens"])
	details, _ := payload["prompt_tokens_details"].(map[string]any)
	assert.Equal(t, 8, details["cached_tokens"])
	compDetails, _ := payload["completion_tokens_details"].(map[string]any)
	assert.Equal(t, 3, compDetails["reasoning_tokens"])
	assert.Nil(t, usagePayload(&llmschema.AssistantMessage{}))
	assert.Nil(t, usagePayload(nil))
}

// ──────────────────────────── 用例：StageDelivery / RecordPageOutcome ───────────────────────────────

// newLedgerTestForks 建一个开启推荐 + 临时工作区的 forks。
func newLedgerTestForks(t *testing.T) *PromptSuggestionForks {
	t.Helper()
	resetWorkspaceForTest(t)
	f := NewPromptSuggestionForks()
	f.SetConfig(enabledConfig())
	return f
}

// openTestStore 打开工作区内的账本库。
func openTestStore(t *testing.T) *TopicStarterStore {
	t.Helper()
	store, err := NewTopicStarterStore(filepath.Join(workspaceDirForTest(), ConsentDBName))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// seedGeneration 写一条 candidate generation 事件。
func seedGeneration(t *testing.T, store *TopicStarterStore, sessionID, requestID, suggestionID, text string) {
	t.Helper()
	ok, err := store.RecordEvent(map[string]any{
		"session_id": sessionID, "slate_id": "prompt-suggestion:" + requestID,
		"event_type": "prompt_suggestion_generation", "topic_id": "prompt-suggestion:" + suggestionID,
		"policy_id": PolicyID, "selection_mode": "prompt_suggestion",
		"propensity_reason": "not_estimated", "created_at": float64(time.Now().UnixNano()) / 1e9,
		"payload": map[string]any{"suggestion_id": suggestionID, "session_id": sessionID,
			"request_id": requestID, "outcome": "candidate", "text": text},
	})
	require.True(t, ok)
	require.NoError(t, err)
}

// completionRow 测试助手：经真实队列写一条完成行并等落盘。
func completionRow(t *testing.T, sid, rid string) {
	t.Helper()
	receipt, err := session.EnqueueHistoryRequestCompletion(sid, rid, "success")
	require.NoError(t, err)
	require.NotNil(t, receipt)
	select {
	case <-receipt:
	case <-time.After(5 * time.Second):
		t.Fatal("完成行 5s 未落盘")
	}
}

// TestStageDelivery_新鲜请求 记 delivery:pushed 并返回页面载荷。
func TestStageDelivery_新鲜请求(t *testing.T) {
	f := newLedgerTestForks(t)
	store := openTestStore(t)
	seedGeneration(t, store, "s1", "r1", "sug1", "试试 git push")
	completionRow(t, "s1", "r1")

	shown := StageDelivery(f, store, "s1", "r1", map[string]any{"suggestion_id": "sug1", "text": "试试 git push"})
	require.NotNil(t, shown)
	assert.Equal(t, ReadyEvent, shown["event_type"])
	assert.Equal(t, "s1", shown["session_id"])
	assert.Equal(t, "r1", shown["request_id"])
	assert.Equal(t, "sug1", shown["suggestion_id"])
	assert.Equal(t, "试试 git push", shown["text"])

	rows, _ := store.QueryEvents(QueryEventsOptions{EventType: "prompt_suggestion_delivery", Limit: 5})
	require.Len(t, rows, 1)
	payload, _ := rows[0]["payload"].(map[string]any)
	assert.Equal(t, "pushed", payload["status"])
	assert.Nil(t, payload["reason"])
}

// TestStageDelivery_过期请求 完成行不是该请求 → delivery:suppressed 且返回 nil。
func TestStageDelivery_过期请求(t *testing.T) {
	f := newLedgerTestForks(t)
	store := openTestStore(t)
	seedGeneration(t, store, "s1", "r1", "sug1", "text")
	completionRow(t, "s1", "r2") // 会话最新完成行是 r2

	shown := StageDelivery(f, store, "s1", "r1", map[string]any{"suggestion_id": "sug1", "text": "text"})
	assert.Nil(t, shown)
	rows, _ := store.QueryEvents(QueryEventsOptions{EventType: "prompt_suggestion_delivery", Limit: 5})
	require.Len(t, rows, 1)
	payload, _ := rows[0]["payload"].(map[string]any)
	assert.Equal(t, "suppressed", payload["status"])
	assert.Equal(t, "stale_request", payload["reason"])
}

// TestStageDelivery_推荐关闭 什么都不记。
func TestStageDelivery_推荐关闭(t *testing.T) {
	resetWorkspaceForTest(t)
	f := NewPromptSuggestionForks()
	store := openTestStore(t)
	assert.Nil(t, StageDelivery(f, store, "s1", "r1", map[string]any{"suggestion_id": "sug1", "text": "t"}))
	rows, _ := store.QueryEvents(QueryEventsOptions{EventType: "prompt_suggestion_delivery", Limit: 5})
	assert.Empty(t, rows)
}

// TestRecordPageOutcome_四类合法路径 shown/accepted/ignored/suppressed 各落一条。
func TestRecordPageOutcome_四类合法路径(t *testing.T) {
	f := newLedgerTestForks(t)
	_ = f
	store := openTestStore(t)
	for _, sug := range []string{"a", "b", "c", "d"} {
		seedGeneration(t, store, "s1", "r1", sug, "text")
	}
	cases := []struct {
		eventType string
		params    map[string]any
	}{
		{"shown", map[string]any{}},
		{"accepted", map[string]any{"method": "click", "time_to_accept_ms": 120}},
		{"ignored", map[string]any{"reason": "dismissed"}},
		{"suppressed", map[string]any{"reason": "timing"}},
	}
	for i, tc := range cases {
		sug := []string{"a", "b", "c", "d"}[i]
		params := map[string]any{"session_id": "s1", "suggestion_id": sug, "event_type": tc.eventType}
		for k, v := range tc.params {
			params[k] = v
		}
		recorded := RecordPageOutcome(store, params)
		require.NotNil(t, recorded, tc.eventType)
		assert.True(t, *recorded, tc.eventType)
	}
	for _, et := range []string{"prompt_suggestion_shown", "prompt_suggestion_accepted",
		"prompt_suggestion_ignored", "prompt_suggestion_suppressed"} {
		rows, _ := store.QueryEvents(QueryEventsOptions{EventType: et, Limit: 5})
		assert.Len(t, rows, 1, et)
	}
}

// TestRecordPageOutcome_非法路径校验矩阵 每条非法输入返回 nil 且不写库。
func TestRecordPageOutcome_非法路径校验矩阵(t *testing.T) {
	newLedgerTestForks(t)
	store := openTestStore(t)
	seedGeneration(t, store, "s1", "r1", "sug1", "text")
	invalids := []map[string]any{
		{"session_id": "s1", "suggestion_id": "sug1", "event_type": "unknown"},
		{"session_id": "s1", "suggestion_id": "sug1", "event_type": "accepted", "method": "swipe", "time_to_accept_ms": 5},
		{"session_id": "s1", "suggestion_id": "sug1", "event_type": "accepted", "method": "click", "time_to_accept_ms": "120"},
		{"session_id": "s1", "suggestion_id": "sug1", "event_type": "accepted", "method": "click", "time_to_accept_ms": -1},
		{"session_id": "s1", "suggestion_id": "sug1", "event_type": "ignored", "reason": "whatever"},
		{"session_id": "s1", "suggestion_id": "sug1", "event_type": "suppressed", "reason": "stale"},
		{"session_id": "s1", "suggestion_id": "", "event_type": "shown"},
		{"session_id": "s2", "suggestion_id": "sug1", "event_type": "shown"}, // session 不符
	}
	for i, params := range invalids {
		assert.Nil(t, RecordPageOutcome(store, params), "case %d", i)
	}
	// 非候选 outcome 的 generation 也不收
	_, err := store.RecordEvent(map[string]any{
		"session_id": "s1", "slate_id": "prompt-suggestion:r9", "event_type": "prompt_suggestion_generation",
		"topic_id": "prompt-suggestion:sug9", "policy_id": PolicyID, "selection_mode": "prompt_suggestion",
		"propensity_reason": "not_estimated", "created_at": float64(time.Now().UnixNano()) / 1e9,
		"payload": map[string]any{"suggestion_id": "sug9", "session_id": "s1", "request_id": "r9", "outcome": "suppressed"},
	})
	require.NoError(t, err)
	assert.Nil(t, RecordPageOutcome(store, map[string]any{"session_id": "s1", "suggestion_id": "sug9", "event_type": "shown"}))
}

// TestRecordPageOutcome_幂等 同一建议重复 accepted 只落一条。
func TestRecordPageOutcome_幂等(t *testing.T) {
	newLedgerTestForks(t)
	store := openTestStore(t)
	seedGeneration(t, store, "s1", "r1", "sug1", "text")
	params := map[string]any{"session_id": "s1", "suggestion_id": "sug1", "event_type": "accepted",
		"method": "tab", "time_to_accept_ms": 30}
	first := RecordPageOutcome(store, params)
	require.NotNil(t, first)
	assert.True(t, *first)
	second := RecordPageOutcome(store, params)
	require.NotNil(t, second)
	assert.False(t, *second)
}
