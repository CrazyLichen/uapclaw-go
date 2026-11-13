package recommendation

// topic_pipeline_c4_test.go 对齐 topic_pipeline.py:2991-3406 助手契约的
// 单元用例：最新摘要读取并列秩、画像/讨论投影、去标识画像候选、
// 短增量摘要与行为消息构造。

import (
	"fmt"
	"testing"
)

// recordSummaryEvent 写入一条 session_summary 事件。
func recordSummaryEvent(t *testing.T, store *TopicStarterStore, scope, sessionID string,
	completedAt float64, sourceEventIDs []string, configVersion string) {
	t.Helper()
	payload := []any{}
	for _, eventID := range sourceEventIDs {
		payload = append(payload, eventID)
	}
	ok, err := store.RecordEvent(map[string]any{
		"session_id":     sessionID,
		"slate_id":       scope,
		"event_type":     "session_summary",
		"topic_id":       fmt.Sprintf("topic:%s:%v", sessionID, completedAt),
		"policy_id":      POLICY_ID,
		"payload": map[string]any{
			"session_id":             sessionID,
			"completed_at":           completedAt,
			"source_event_ids":       payload,
			"summary_config_version": configVersion,
			"summary_text":           "会话摘要：" + sessionID,
			"assistance_origin":      "none",
		},
		"created_at": completedAt,
	})
	if err != nil || !ok {
		t.Fatalf("record summary: ok=%v err=%v", ok, err)
	}
}

// TestLatestSessionSummaries_并列秩 对齐 :3006-3054 并列时当前配置版本优先、
// 覆盖更多轮次优先。
func TestLatestSessionSummaries(t *testing.T) {
	store := newTestStore(t)
	sessions := map[string]bool{"s1": true}

	// 基线摘要
	recordSummaryEvent(t, store, GlobalNewChatScope, "s1", 10.0, []string{"e1"}, SessionSummaryConfigVersion+":old")
	latest, err := LatestSessionSummaries(store, GlobalNewChatScope, sessions)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(latest) != 1 || latest["s1"]["summary_config_version"] != SessionSummaryConfigVersion+":old" {
		t.Fatalf("baseline: %v", latest)
	}

	// 同一完成边界的并列：当前配置版本胜出
	recordSummaryEvent(t, store, GlobalNewChatScope, "s1", 10.0, []string{"e1"}, SessionSummaryConfigVersion+":v9")
	latest2, _ := LatestSessionSummaries(store, GlobalNewChatScope, sessions)
	if latest2["s1"]["summary_config_version"] != SessionSummaryConfigVersion+":v9" {
		t.Fatalf("current config should win tie: %v", latest2["s1"])
	}

	// 并列且同配置：覆盖更多轮次胜出
	recordSummaryEvent(t, store, GlobalNewChatScope, "s1", 10.0,
		[]string{"e1", "e2"}, SessionSummaryConfigVersion+":v9")
	latest3, _ := LatestSessionSummaries(store, GlobalNewChatScope, sessions)
	if ids := pyListStrings(latest3["s1"]["source_event_ids"]); len(ids) != 2 {
		t.Fatalf("more turns should win tie: %v", latest3["s1"])
	}

	// 更晚的完成时间直接胜出
	recordSummaryEvent(t, store, GlobalNewChatScope, "s1", 20.0, []string{"e1"}, SessionSummaryConfigVersion+":old")
	latest4, _ := LatestSessionSummaries(store, GlobalNewChatScope, sessions)
	if toFloatOr(latest4["s1"]["completed_at"], 0) != 20.0 {
		t.Fatalf("newest boundary wins: %v", latest4["s1"])
	}

	// 空会话集合直接返回空表
	empty, err := LatestSessionSummaries(store, GlobalNewChatScope, map[string]bool{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty sessions: %v %v", empty, err)
	}
}

// TestProfileProjectDiscussion 对齐 :3057-3106 仅非全局桶项目计入。
func TestProfileProjectDiscussion(t *testing.T) {
	store := newTestStore(t)
	state := map[string]any{
		"memory": map[string]any{
			"units": []any{
				map[string]any{
					"companion": map[string]any{
						"unit_id":                  "unit-1",
						"canonical_source_locators": []any{[]any{"p1", 0}},
					},
				},
			},
		},
	}
	projectBySession := map[string]string{"p1": "proj-a", "g1": ""}
	recordSummaryEvent(t, store, "project:proj-a", "p1", 10.0, []string{"e1"}, "cfg")
	// 全局桶会话属于另一 scope，不触发项目读取
	recordSummaryEvent(t, store, GlobalNewChatScope, "g1", 12.0, []string{"e2"}, "cfg")

	coverage, err := ProfileProjectDiscussion(store, state, projectBySession,
		map[string][]string{"p1": {"proj-a"}})
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if len(coverage) != 1 {
		t.Fatalf("coverage entries: %v", coverage)
	}
	entry := coverage[0]
	if entry["from_another_scope"] != true || entry["assistance_origin"] != "none" {
		t.Fatalf("entry fields: %v", entry)
	}
	if summary := entry["summary"].(string); summary == "" ||
		summary == "会话摘要：p1" /* 标识 proj-a 所在 session id 剔除 */ {
		t.Fatalf("summary: %q", summary)
	}
}

// TestProjectUnitGlobalDiscussion 对齐 :3109-3181 只有卡片点击开启的会话计入。
func TestProjectUnitGlobalDiscussion(t *testing.T) {
	store := newTestStore(t)
	state := map[string]any{
		"memory": map[string]any{
			"units": []any{
				map[string]any{
					"companion": map[string]any{
						"unit_id":                  "unit-1",
						"canonical_source_locators": []any{[]any{"p1", 0}},
					},
				},
			},
		},
	}
	projectBySession := map[string]string{"p1": "proj-a", "g1": "", "g2": ""}

	// 记录两条全局 session_bound，g1 由本单元卡片开启，g2 不是
	bindEvent := func(t *testing.T, sessionID, topicID string) {
		t.Helper()
		ok, err := store.RecordEvent(map[string]any{
			"session_id": sessionID,
			"slate_id":   sessionID,
			"event_type": "session_bound",
			"topic_id":   topicID,
			"policy_id":  POLICY_ID,
			"payload":    map[string]any{"context_scope": GlobalNewChatScope},
			"created_at": 5.0,
		})
		if err != nil || !ok {
			t.Fatalf("bind: ok=%v err=%v", ok, err)
		}
	}
	bindEvent(t, "g1", "topic:g1")
	bindEvent(t, "g2", "topic:g2")
	cardEvent := func(t *testing.T, sessionID, topicID string, withUnit bool) {
		t.Helper()
		units := []any{}
		if withUnit {
			units = append(units, "unit-1")
		}
		ok, err := store.RecordEvent(map[string]any{
			"session_id": sessionID,
			"slate_id":   topicID,
			"event_type": "starter_card",
			"topic_id":   topicID,
			"policy_id":  POLICY_ID,
			"payload":    map[string]any{"source_memory_unit_ids": units},
			"created_at": 6.0,
		})
		if err != nil || !ok {
			t.Fatalf("card: ok=%v err=%v", ok, err)
		}
	}
	cardEvent(t, "g1", "topic:g1", true)
	cardEvent(t, "g2", "topic:g2", false)
	recordSummaryEvent(t, store, GlobalNewChatScope, "g1", 10.0, []string{"e1"}, "cfg")

	result, err := ProjectUnitGlobalDiscussion(store, state, "proj-a", projectBySession)
	if err != nil {
		t.Fatalf("discussion: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("only g1 should count: %v", result)
	}
	if result[0]["summary"] != "会话摘要：g1" {
		t.Fatalf("summary: %v", result[0])
	}

	// 项目无单元 → 空
	emptyState := map[string]any{"memory": map[string]any{"units": []any{}}}
	empty, err := ProjectUnitGlobalDiscussion(store, emptyState, "proj-a", projectBySession)
	if err != nil || len(empty) != 0 {
		t.Fatalf("no units: %v %v", empty, err)
	}
}

// TestDeprojectedLongProfile 对齐 :3184-3275 三类兴趣簇与标识剔除。
func TestDeprojectedLongProfile(t *testing.T) {
	state := map[string]any{
		"memory": map[string]any{
			"units": []any{
				// 命中 taxonomy 的单元，源会话来自项目 → 剔除项目标识
				map[string]any{
					"core": map[string]any{
						"behavior_pattern":    "tech-ai-ml",
						"preference_summary":  "用户想深入 proj-a 的大模型评测",
						"preferred_entities":  []any{"proj-a", "评测基准"},
						"temporal_activity":   "工作日上午活跃",
					},
					"companion": map[string]any{
						"unit_id":                  "unit-1",
						"evidence_ids":             []any{"unit-1:evt1", "  "},
						"canonical_source_locators": []any{[]any{"p1", 0}},
					},
				},
				// provisional 前缀
				map[string]any{
					"core":     map[string]any{"behavior_pattern": "provisional:tea-brewing"},
					"companion": map[string]any{"unit_id": "unit-2"},
				},
				// 未映射 → general-interest，无源会话 → 摘要取 taxonomy 文案
				map[string]any{
					"core":     map[string]any{"behavior_pattern": "unknown-pattern"},
					"companion": map[string]any{"unit_id": "unit-3"},
				},
				// 缺 unit_id → 跳过
				map[string]any{"core": map[string]any{"behavior_pattern": "tech-ai-ml"},
					"companion": map[string]any{}},
			},
		},
	}
	interests, err := DeprojectedLongProfile(state, 3,
		map[string]string{"p1": "proj-a"}, map[string][]string{"p1": {"proj-a"}})
	if err != nil {
		t.Fatalf("interests: %v", err)
	}
	if len(interests) != 3 {
		t.Fatalf("expected 3 interests: %d", len(interests))
	}
	first := interests[0]
	if first.InterestSummaryID != "long-term-profile:3:unit-1" {
		t.Fatalf("id: %s", first.InterestSummaryID)
	}
	if first.InterestCluster != "tech-ai-ml" {
		t.Fatalf("cluster: %s", first.InterestCluster)
	}
	if first.SummaryText == "" || first.SummaryText == "用户想深入 proj-a 的大模型评测" {
		t.Fatalf("identifiers should be stripped: %q", first.SummaryText)
	}
	if interests[1].InterestCluster != "specialized-interest" {
		t.Fatalf("provisional: %s", interests[1].InterestCluster)
	}
	if interests[2].InterestCluster != "general-interest" || interests[2].SummaryText == "" {
		t.Fatalf("general fallback: %+v", interests[2])
	}
}

// TestShortDeltaSummaryIDs 对齐 :3278-3289。
func TestShortDeltaSummaryIDs(t *testing.T) {
	snapshot := snapshotForC3Tests()
	snapshot.Messages = []*V6SourceMessage{
		{EventID: "e1", InputOrigin: "manual_free_text", AssistanceOrigin: "none"},
	}
	snapshot.PendingShortIntentTriggerLineage = []map[string]any{
		lineageFixture([]string{"e1"}, nil, nil),
	}
	summary := summaryForTests(t, "summary:s1:aaa", "s1", 10.0)
	got := ShortDeltaSummaryIDs(snapshot, []*SessionSummaryArtifact{summary})
	if len(got) != 1 || got[0] != "summary:s1:aaa" {
		t.Fatalf("delta: %v", got)
	}
	// 无血缘 → 空
	snapshot.PendingShortIntentTriggerLineage = []map[string]any{}
	got2 := ShortDeltaSummaryIDs(snapshot, []*SessionSummaryArtifact{summary})
	if len(got2) != 0 {
		t.Fatalf("no lineage: %v", got2)
	}
}

// TestMessageFromBehavior 对齐 :3386-3406 默认值与事件 ID 回退。
func TestMessageFromBehavior(t *testing.T) {
	behavior := CanonicalBehavior{
		SessionID: "s1", Ordinal: 2, BehaviorIndex: 7, TimestampOr0: 100.0,
		Content: "用户的问题",
	}
	message := MessageFromBehavior(behavior, map[string]any{},
		map[string]any{"tag": "tech-ai-ml", "action_type": "correction"}, "p1")
	if message.EventID != "behavior:7" {
		t.Fatalf("fallback id: %s", message.EventID)
	}
	if message.InputOrigin != "pasted_or_unknown" || message.AssistanceOrigin != "unknown" {
		t.Fatalf("origins: %s/%s", message.InputOrigin, message.AssistanceOrigin)
	}
	if message.IntentCluster != "tech-ai-ml" || message.ActionType != "correction" {
		t.Fatalf("mapping: %s/%s", message.IntentCluster, message.ActionType)
	}

	message2 := MessageFromBehavior(behavior, map[string]any{}, map[string]any{}, nil)
	if message2.EventID != "rid-rec" && message2.EventID != "behavior:7" {
		t.Fatalf("id: %s", message2.EventID)
	}
	if message2.IntentCluster != "unmapped" || message2.ActionType != "natural_question" {
		t.Fatalf("defaults: %s/%s", message2.IntentCluster, message2.ActionType)
	}

	behavior.RecordID = "rec-9"
	message3 := MessageFromBehavior(behavior, map[string]any{}, map[string]any{}, nil)
	if message3.EventID != "rec-9" {
		t.Fatalf("record id: %s", message3.EventID)
	}
}

// TestTaxonomyTags_与Python一致 校验分类法成员逐字对齐 topic_history.py:86-105。
func TestTaxonomyTags_PythonAlignment(t *testing.T) {
	expected := []string{
		"tech-ai-ml", "tech-software-dev", "tech-hardware-devices",
		"science-math-research", "science-life-medicine", "finance-investment",
		"business-career", "education-learning", "culture-history-humanities",
		"arts-media-entertainment", "gaming-interactive", "sports-fitness",
		"travel-outdoor", "food-cooking", "home-lifestyle", "society-news-current",
		"language-writing", "psychology-self",
	}
	if len(TaxonomyTags) != len(expected) {
		t.Fatalf("count: %d vs %d", len(TaxonomyTags), len(expected))
	}
	for _, tag := range expected {
		if TaxonomyTags[tag] == "" {
			t.Fatalf("missing tag: %s", tag)
		}
	}
}
