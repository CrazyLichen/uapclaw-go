package recommendation

// topic_interest_feedback_test.go 对齐 topic_product.py InterestFeedback
// 契约的单元用例：scope 过滤、版本化、宿主字段剔除、卡片否定、暂停与
// 恢复、strike 阶梯与永久、撤销、方向键与状态判定。

import (
	"strings"
	"testing"
)

// feedbackFixture 构造带两个 Memory 单元的 state。
func feedbackFixture() map[string]any {
	return map[string]any{
		"memory": map[string]any{
			"units": []any{
				map[string]any{
					"companion": map[string]any{
						"unit_id":                   "unit-1",
						"evidence_ids":              []any{"s1:e1", "s1:e2"},
						"canonical_source_locators": []any{[]any{"s1", 0}},
					},
				},
				map[string]any{
					"companion": map[string]any{
						"unit_id":      "unit-2",
						"evidence_ids": []any{"s2:e1"},
					},
				},
			},
		},
		"memory_sidecar": map[string]any{
			"negative_preferences": []any{
				map[string]any{
					"reason": "explicit_not_interested", "context_scope": "global_new_chat",
					"project_id": nil, "record_id": "rec-1",
				},
				map[string]any{
					"reason": "explicit_not_interested", "context_scope": "project:other",
					"project_id": "proj-b", "record_id": "rec-2",
				},
				map[string]any{
					"reason": "explicit_not_interested", "context_scope": "global_new_chat",
					"record_id": "rec-3",
				},
			},
		},
	}
}

// TestInterestFeedbackFromState_Scope过滤 对齐 :5606-5620/:5702-5735。
func TestInterestFeedbackFromState_ScopeFiltering(t *testing.T) {
	feedback := InterestFeedbackFromState(feedbackFixture(), "global_new_chat", nil, 1000.0)
	// 只留 scope 匹配且带 project_id 键的记录（rec-1）；rec-2 scope 不符，
	// rec-3 无 project_id 键。
	if len(feedback.NegativePreferences) != 1 || feedback.NegativePreferences[0]["record_id"] != "rec-1" {
		t.Fatalf("preferences: %v", feedback.NegativePreferences)
	}
	if feedback.Version() != 1 {
		t.Fatalf("version: %d", feedback.Version())
	}
	if feedback.EvidenceByUnit["unit-1"]["s1:e1"] != true ||
		feedback.SessionsByUnit["unit-1"]["s1"] != true {
		t.Fatalf("evidence: %v", feedback.EvidenceByUnit)
	}

	// 项目 scope 只留该项目
	projectFeedback := InterestFeedbackFromState(feedbackFixture(), "project:other", "proj-b", 1000.0)
	if len(projectFeedback.NegativePreferences) != 1 || projectFeedback.NegativePreferences[0]["record_id"] != "rec-2" {
		t.Fatalf("project preferences: %v", projectFeedback.NegativePreferences)
	}
}

// TestInterestFeedbackAsWritten 对齐 :5822-5825。
func TestInterestFeedbackAsWritten(t *testing.T) {
	state := feedbackFixture()
	sidecar := state["memory_sidecar"].(map[string]any)
	sidecar["negative_preferences"] = append(sidecar["negative_preferences"].([]any),
		map[string]any{
			"reason": "explicit_not_interested", "context_scope": "global_new_chat",
			"project_id": nil, "record_id": "rec-4",
		})
	feedback := InterestFeedbackFromState(state, "global_new_chat", nil, 1000.0)
	if feedback.Version() != 2 {
		t.Fatalf("version: %d", feedback.Version())
	}
	earlier := feedback.AsWritten(1, 500.0)
	if earlier.Version() != 1 || earlier.Now != 500.0 {
		t.Fatalf("as written: %d %v", earlier.Version(), earlier.Now)
	}
	if feedback.Version() != 2 {
		t.Fatalf("original unchanged: %d", feedback.Version())
	}
}

// TestInterestFeedbackModelPayloads 对齐 :5804-5814 剔除宿主字段。
func TestInterestFeedbackModelPayloads(t *testing.T) {
	record := map[string]any{
		"reason": "explicit_not_interested", "context_scope": "global_new_chat",
		"project_id": nil, "record_id": "rec-1", "dismissed_at": 100.0,
		"card_id": "c1", "exposure_id": "exp-1", "aliases": []any{"问题A"},
		"evidence_session_ids": []any{"s1"},
	}
	state := map[string]any{
		"memory":         map[string]any{"units": []any{}},
		"memory_sidecar": map[string]any{"negative_preferences": []any{record}},
	}
	feedback := InterestFeedbackFromState(state, "global_new_chat", nil, 1000.0)
	payloads := feedback.ModelPayloads(true)
	if len(payloads) != 1 {
		t.Fatalf("payloads: %v", payloads)
	}
	for _, hostField := range []string{"dismissed_at", "card_id", "exposure_id", "evidence_session_ids"} {
		if _, present := payloads[0][hostField]; present {
			t.Fatalf("host field %s leaked", hostField)
		}
	}
	if payloads[0]["reason"] != "explicit_not_interested" {
		t.Fatalf("model field kept: %v", payloads[0])
	}
}

// TestInterestFeedbackCardDismissal 对齐 card 分支与 still_wanted。
func TestInterestFeedbackCardDismissal(t *testing.T) {
	record := map[string]any{
		"reason": "explicit_not_interested", "context_scope": "global_new_chat",
		"project_id": nil, "record_id": "rec-1", "dismissal_kind": "card",
		"card_id": "c1", "aliases": []any{"问题A"},
	}
	state := map[string]any{
		"memory":         map[string]any{"units": []any{}},
		"memory_sidecar": map[string]any{"negative_preferences": []any{record}},
	}
	feedback := InterestFeedbackFromState(state, "global_new_chat", nil, 1000.0)
	titles := feedback.DismissedCardTitles()
	if len(titles) != 1 || titles[0] != "问题A" {
		t.Fatalf("titles: %v", titles)
	}
	// 同 ID 卡不再展示
	if feedback.StillWanted(map[string]any{"card_id": "c1", "intent": "x", "title": "y", "prompt": "z"}) {
		t.Fatalf("dismissed card should not be wanted")
	}
	// 其他卡不受影响
	if !feedback.StillWanted(map[string]any{"card_id": "c2", "intent": "x", "title": "y", "prompt": "z"}) {
		t.Fatalf("unrelated card should be wanted")
	}
}

// TestInterestFeedbackDismissalStrikes 对齐 :5987-6006 strike 阶梯。
func TestInterestFeedbackDismissalStrikes(t *testing.T) {
	preferences := []any{}
	for index, at := range []float64{100, 200, 300} {
		preferences = append(preferences, map[string]any{
			"reason": "explicit_not_interested", "context_scope": "global_new_chat",
			"project_id": nil, "record_id": index, "dismissed_at": at,
			"source_event_ids": []any{"s9:e1"},
		})
	}
	state := map[string]any{
		"memory":         map[string]any{"units": []any{}},
		"memory_sidecar": map[string]any{"negative_preferences": preferences},
	}
	// 三次否定后 → permanent（否定条目不受 now 截断，见 :5987-5993 分支序）
	permanent := InterestFeedbackFromState(state, "global_new_chat", nil, 400.0)
	if len(permanent.PermanentDirections()) != 1 {
		t.Fatalf("permanent after three strikes")
	}
	status := permanent.StatusOf(nil, []string{"s9:e1"}, "")
	if status != "permanent" {
		t.Fatalf("status: %s", status)
	}
	// 单次否定后 5 天 → hidden
	one := []any{preferences[0]}
	hidden := InterestFeedbackFromState(map[string]any{
		"memory":         map[string]any{"units": []any{}},
		"memory_sidecar": map[string]any{"negative_preferences": one},
	}, "global_new_chat", nil, 100+5*DaySeconds)
	if status := hidden.StatusOf(nil, []string{"s9:e1"}, ""); status != "hidden" {
		t.Fatalf("hidden: %s", status)
	}
}

// TestInterestFeedbackPauseResume 对齐 pause 分支与 _resumes。
func TestInterestFeedbackPauseResume(t *testing.T) {
	pause := map[string]any{
		"reason": "explicit_not_interested", "context_scope": "global_new_chat",
		"project_id": nil, "record_id": "rec-1", "dismissal_kind": "pause",
		"dismissed_at": 100.0, "source_id": "unit-1",
		"evidence_session_ids": []any{"s1"},
	}
	state := map[string]any{
		"memory": map[string]any{
			"units": []any{
				map[string]any{
					"companion": map[string]any{
						"unit_id":                   "unit-1",
						"evidence_ids":              []any{"s1:e1"},
						"canonical_source_locators": []any{[]any{"s1", 0}},
					},
				},
			},
		},
		"memory_sidecar": map[string]any{"negative_preferences": []any{pause}},
	}
	// 暂停期间 → 单元被持有
	paused := InterestFeedbackFromState(state, "global_new_chat", nil, 200.0)
	if !paused.DismissedUnits()["unit-1"] {
		t.Fatalf("paused unit should be dismissed")
	}
	// 自然会话在暂停后落进同一单元 → 恢复
	resumed := InterestFeedbackFromState(state, "global_new_chat", nil, 500.0)
	resumed.NaturalConversations = []NaturalConversation{{
		CompletedAt: 300.0,
		Events:      map[string]bool{"s1:e1": true},
	}}
	if resumed.DismissedUnits()["unit-1"] {
		t.Fatalf("resumed unit should be released")
	}
}

// TestInterestFeedbackWithdrawn 对齐 :5881-5885 撤销。
func TestInterestFeedbackWithdrawn(t *testing.T) {
	state := map[string]any{
		"memory": map[string]any{"units": []any{}},
		"memory_sidecar": map[string]any{"negative_preferences": []any{
			map[string]any{
				"reason": "explicit_not_interested", "context_scope": "global_new_chat",
				"project_id": nil, "record_id": "rec-1", "source_event_ids": []any{"s9:e1"},
				"dismissed_at": 100.0,
			},
			map[string]any{
				"reason": "withdrawn", "context_scope": "global_new_chat",
				"project_id": nil, "record_id": "rec-2", "withdraws": "rec-1",
			},
		}},
	}
	feedback := InterestFeedbackFromState(state, "global_new_chat", nil, 500.0)
	// 被撤销的否定不再计入方向
	if len(feedback.PermanentDirections()) != 0 {
		t.Fatalf("withdrawn dismissal should not strike")
	}
}

// TestInterestFeedbackWithheldAndHolding 对齐 :6154-6249。
func TestInterestFeedbackWithheldAndHolding(t *testing.T) {
	record := map[string]any{
		"reason": "explicit_not_interested", "context_scope": "global_new_chat",
		"project_id": nil, "record_id": "rec-1", "source_id": "unit-1",
		"source_event_ids": []any{"s1:e1"},
	}
	state := map[string]any{
		"memory": map[string]any{
			"units": []any{
				map[string]any{
					"companion": map[string]any{
						"unit_id":      "unit-1",
						"evidence_ids": []any{"s1:e1", "s1:e2"},
					},
				},
			},
		},
		"memory_sidecar": map[string]any{"negative_preferences": []any{record}},
	}
	feedback := InterestFeedbackFromState(state, "global_new_chat", nil, 1000.0)
	// 兴趣事件全部落在被否定单元的证据内 → withheld
	withheld := feedback.Withheld(map[string][]string{"int-a": {"s1:e1", "s1:e2"}})
	if !withheld["int-a"] {
		t.Fatalf("fully covered interest withheld: %v", withheld)
	}
	// 部分覆盖不 withheld
	withheld2 := feedback.Withheld(map[string][]string{"int-b": {"s1:e1", "s3:e1"}})
	if withheld2["int-b"] {
		t.Fatalf("partial coverage not withheld")
	}
	// holding_units：首个证据涵盖全部事件的候选
	units := feedback.HoldingUnits([]string{"s1:e1", "s1:e2"}, [][]string{{"unit-2"}, {"unit-1"}})
	if len(units) != 1 || units[0] != "unit-1" {
		t.Fatalf("holding: %v", units)
	}
	// units_sharing：任一事件命中
	sharing := feedback.UnitsSharing([]string{"s2:e1"})
	if sharing["unit-1"] || !feedback.UnitsSharing([]string{"s1:e1"})["unit-1"] {
		t.Fatalf("sharing: %v", sharing)
	}
}

// TestInterestFeedbackDirectionAnchor 对齐 direction 键与 direction_payloads。
func TestInterestFeedbackDirectionAnchor(t *testing.T) {
	record := map[string]any{
		"reason": "explicit_not_interested", "context_scope": "global_new_chat",
		"project_id": nil, "record_id": "rec-1", "direction_anchor": "想学大模型评测",
		"dismissed_at": 100.0, "source_id": "unit-1",
	}
	state := map[string]any{
		"memory":         map[string]any{"units": []any{}},
		"memory_sidecar": map[string]any{"negative_preferences": []any{record}},
	}
	feedback := InterestFeedbackFromState(state, "global_new_chat", nil, 150.0)
	payloads := feedback.DirectionPayloads()
	if len(payloads) != 1 || payloads[0]["direction"] != "想学大模型评测" {
		t.Fatalf("direction payloads: %v", payloads)
	}
	if payloads[0]["status"] != "hidden" {
		t.Fatalf("status: %s", payloads[0]["status"])
	}
	// 具名方向的否定不影响无名单元
	if len(feedback.DismissedUnits()) != 0 {
		t.Fatalf("directional dismissal keeps unit cards: %v", feedback.DismissedUnits())
	}
	// 同锚点文本命中否定 → MatchesDirection true
	if !MatchesDirection("我想学大模型评测的方法", "想学大模型评测") {
		t.Fatalf("anchor should match")
	}
	if MatchesDirection("完全无关的文本", "想学大模型评测") {
		t.Fatalf("unrelated should not match")
	}
}

// TestInterestFeedbackLoad_RowsSince 对齐 :5737-5791 store 读取。
func TestInterestFeedbackLoad_RowsSince(t *testing.T) {
	store := newTestStore(t)
	state := map[string]any{
		"memory": map[string]any{"units": []any{}},
		"memory_sidecar": map[string]any{"negative_preferences": []any{
			map[string]any{
				"reason": "explicit_not_interested", "context_scope": "global_new_chat",
				"project_id": nil, "record_id": "rec-1", "dismissed_at": 100.0,
			},
		}},
		"history_ledger": map[string]any{"sessions": map[string]any{}},
	}
	ok, err := store.RecordEvent(map[string]any{
		"session_id": "s1", "slate_id": "s1", "event_type": "session_summary",
		"topic_id": "topic:1", "policy_id": POLICY_ID,
		"payload": map[string]any{
			"context_scope":            "global_new_chat",
			"natural_revisit_eligible": true,
			"completed_at":             300.0,
			"session_id":               "s1",
			"source_event_ids":         []any{"e1"},
			"source_locators":          []any{},
		},
		"created_at": 300.0,
	})
	if err != nil || !ok {
		t.Fatalf("summary: ok=%v err=%v", ok, err)
	}
	feedback, err := InterestFeedbackLoad(store, state, "global_new_chat", nil, 1000.0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(feedback.NaturalConversations) != 1 {
		t.Fatalf("natural conversations: %v", feedback.NaturalConversations)
	}
	if !feedback.NaturalConversations[0].Events["s1:e1"] {
		t.Fatalf("evidence ids: %v", feedback.NaturalConversations[0].Events)
	}
	if feedback.NaturalConversations[0].CompletedAt != 300.0 {
		t.Fatalf("completed: %v", feedback.NaturalConversations[0].CompletedAt)
	}
	// 无计时否定 → 不读账本
	feedback2, err := InterestFeedbackLoad(store, map[string]any{
		"memory":         map[string]any{"units": []any{}},
		"memory_sidecar": map[string]any{"negative_preferences": []any{}},
	}, "global_new_chat", nil, 1000.0)
	if err != nil || len(feedback2.NaturalConversations) != 0 {
		t.Fatalf("no since: %v %v", feedback2.NaturalConversations, err)
	}
}

// TestInterestFeedbackHelpers 对齐 :6336-6368 助手。
func TestInterestFeedbackHelpers(t *testing.T) {
	if !isDismissal(map[string]any{}) {
		t.Fatalf("default reason is dismissal")
	}
	if isDismissal(map[string]any{"reason": "restored"}) {
		t.Fatalf("restored is not dismissal")
	}
	if dismissalStatus(0, 0, 0) != "available" {
		t.Fatalf("no strikes")
	}
	if dismissalStatus(3, 100, 200) != "permanent" {
		t.Fatalf("three strikes permanent")
	}
	if dismissalStatus(1, 100, 100+3*DaySeconds) != "hidden" {
		t.Fatalf("one strike hidden")
	}
	if dismissalStatus(1, 100, 100+20*DaySeconds) != "reduced" {
		t.Fatalf("one strike reduced after 7d-30d")
	}
	if dismissalStatus(1, 100, 100+120*DaySeconds) != "available" {
		t.Fatalf("one strike available after 90d")
	}
	if !withinEvents([]string{"a"}, map[string]bool{"a": true}) {
		t.Fatalf("within")
	}
	if withinEvents(nil, map[string]bool{"a": true}) {
		t.Fatalf("empty events never within")
	}
	if !belongsDirection("", map[string]bool{"a": true}, nil, map[string]bool{"a": true}) {
		t.Fatalf("evidence overlap")
	}
	if !belongsDirection("u1", map[string]bool{}, map[string]bool{"u1": true}, nil) {
		t.Fatalf("unit membership")
	}
	_ = strings.TrimSpace("")
}
