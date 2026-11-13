package recommendation

// topic_pipeline_b_test.go 对齐 topic_pipeline.py:1032-1850 的用例：
// V6 调用器包装、宿主证据、项目续聊/画像兴趣/热榜计划、来源分类器、
// 标题复述相似度、新闻 query 归一化。

import (
	"context"
	"strings"
	"testing"
)

// TestBuildV6ArkStageInvoker_系统提示词分发 对齐 :1053-1058 invoke 闭包。
func TestBuildV6ArkStageInvoker(t *testing.T) {
	config := &ArkBackgroundConfig{Model: "m", BaseURL: "https://ark", ContextWindowTokens: 128}
	invoker := BuildV6ArkStageInvoker(config, NewDefaultChatClientFactory(), nil)
	if invoker == nil || invoker.base == nil {
		t.Fatalf("invoker nil")
	}
	// V6SystemPrompt 分发：memory_init 走内置提示词
	if V6SystemPrompt("memory_init", "supplied") != pipelineV6MemoryInitSystemPrompt {
		t.Fatalf("memory_init prompt dispatch")
	}
	if V6SystemPrompt("v6_session_summarizer", "supplied") != "supplied" {
		t.Fatalf("supplied prompt passthrough")
	}
	// max tokens 只对 summarizer 生效
	if mt := V6MaxTokens("v6_session_summarizer"); mt == nil || *mt != 16384 {
		t.Fatalf("summarizer max tokens")
	}
	if V6MaxTokens("v6_utility_scorer") != nil {
		t.Fatalf("other stages keep default")
	}
}

// TestV6ResponseSchema_分发 对齐 :944-990。
func TestV6ResponseSchema(t *testing.T) {
	// 行为映射 schema
	schema, err := V6ResponseSchema("behavior_map_batch", "{}")
	if err != nil || schema == nil {
		t.Fatalf("behavior_map_batch: %v", err)
	}
	// slot 决策按槽数收紧
	payload := `{"candidate_slots":[{"slot_id":"s1"},{"slot_id":"s2"}]}`
	schema, err = V6ResponseSchema("v6_global_interest_generator", payload)
	if err != nil || schema == nil {
		t.Fatalf("slot schema: %v", err)
	}
	candidates := mapAnyOf(mapAnyOf(schema["properties"])["candidates"])
	if intOf(candidates["minItems"]) != 2 || intOf(candidates["maxItems"]) != 2 {
		t.Fatalf("slot min/max=%v/%v", candidates["minItems"], candidates["maxItems"])
	}
	// utility 按候选收紧并枚举 ID
	utilityPayload := `{"candidates":[{"candidate_id":"a"},{"candidate_id":"b"}]}`
	schema, err = V6ResponseSchema("v6_utility_scorer", utilityPayload)
	if err != nil || schema == nil {
		t.Fatalf("utility schema: %v", err)
	}
	scores := mapAnyOf(mapAnyOf(schema["properties"])["scores"])
	if intOf(scores["minItems"]) != 2 || intOf(scores["maxItems"]) != 2 {
		t.Fatalf("utility min/max")
	}
	items := mapAnyOf(scores["items"])
	properties := mapAnyOf(items["properties"])
	if len(anyListOf(mapAnyOf(properties["candidate_id"])["enum"])) != 2 {
		t.Fatalf("candidate enum")
	}
	// 未知 v6 阶段报错
	if _, err := V6ResponseSchema("v6_bogus", "{}"); err == nil {
		t.Fatalf("unknown stage should fail")
	}
	// 非 v6 阶段回 nil
	schema, err = V6ResponseSchema("other_stage", "{}")
	if err != nil || schema != nil {
		t.Fatalf("non-v6 stage should be nil")
	}
}

// TestAuditedArkStageInvoker_请求身份 对齐 :1075-1092。
func TestAuditedArkStageInvoker(t *testing.T) {
	store := newTestStore(t)
	audit, err := NewExternalInvocationAudit(store, ProductPolicyID, "snap1",
		GlobalNewChatScope, "visit-1")
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	config := &ArkBackgroundConfig{Model: "m", BaseURL: "https://ark", ContextWindowTokens: 128}
	base := BuildArkStageInvoker(config, NewDefaultChatClientFactory(), nil, false, nil, nil, nil, nil, false)
	wrapper := AuditedArkStageInvoker(base, audit)
	// payload 无 request_id → 拒绝
	_, err = wrapper.Invoke(context.Background(), "v6_utility_scorer", "p", `{"x":1}`)
	if err == nil || pyStr(map[string]any{"r": err.Error()}["r"]) != "external_invocation_request_id_missing" {
		t.Fatalf("missing request id: %v", err)
	}
	// 非法 JSON → 契约拒绝
	_, err = wrapper.Invoke(context.Background(), "v6_utility_scorer", "p", "not json")
	if _, isRejection := err.(*TerminalGenerationRejection); !isRejection {
		t.Fatalf("bad json should be rejection: %v", err)
	}
	// 带 request_id 的合法 payload：进入 base 调用（fake client 会失败，但审计已记录）
	rows, _ := store.QueryEvents(QueryEventsOptions{
		EventType: EXTERNAL_INVOCATION_ATTEMPT_EVENT_TYPE,
		PolicyID:  &productPolicyIDPtr, Limit: 8})
	countBefore := len(rows)
	_, _ = wrapper.Invoke(context.Background(), "v6_utility_scorer", "p", `{"request_id":"r1"}`)
	rows, _ = store.QueryEvents(QueryEventsOptions{
		EventType: EXTERNAL_INVOCATION_ATTEMPT_EVENT_TYPE,
		PolicyID:  &productPolicyIDPtr, Limit: 8})
	if len(rows) != countBefore+1 {
		t.Fatalf("attempt should be recorded: %d -> %d", countBefore, len(rows))
	}
	// evidence extractor 不记审计
	_, _ = wrapper.Invoke(context.Background(), "v6_evidence_extractor", "p", `{"request_id":"r2"}`)
	rows, _ = store.QueryEvents(QueryEventsOptions{
		EventType: EXTERNAL_INVOCATION_ATTEMPT_EVENT_TYPE,
		PolicyID:  &productPolicyIDPtr, Limit: 8})
	if len(rows) != countBefore+1 {
		t.Fatalf("extractor should skip audit")
	}
}

// TestProjectContinuationPlan 对齐 :1208-1320。
func TestProjectContinuationPlan(t *testing.T) {
	source := &V6SourceMessage{EventID: "evt-1", SessionID: "s1", Ordinal: 3,
		Content: "讨论陶瓷烧制温度", ObservedAt: 500.0}
	snapshot := &V6ContextSnapshot{ContextScope: "project:p1", ProjectID: "p1",
		InputSnapshotID: "snap1"}
	plan := ProjectContinuationPlan("cand-1", "i1", "问烧制温度", "陶瓷烧制温度",
		source, snapshot, "陶瓷兴趣摘要")
	if pyStr(plan["category"]) != "project_continuation" ||
		pyStr(plan["freshness_class"]) != "evergreen_corpus" {
		t.Fatalf("category=%v", plan["category"])
	}
	evidence := mapAnyOf(plan["project_evidence"])
	if pyStr(evidence["source_url"]) != "project://p1/s1/3" {
		t.Fatalf("source_url=%v", evidence["source_url"])
	}
	if pyStr(evidence["document_revision_id"]) != "project-history:evt-1" {
		t.Fatalf("revision")
	}
	receipt := mapAnyOf(evidence["host_receipt"])
	if pyStr(receipt["lane"]) != "host_project_exact_locator" ||
		pyStr(receipt["receipt_version"]) != ProjectLocatorReceiptVersion {
		t.Fatalf("receipt=%v", receipt)
	}
	if len(anyListOf(plan["source_locators"])) != 1 {
		t.Fatalf("locators")
	}
}

// TestHotNewsAndListPlans 对齐 :1402-1505。
func TestHotNewsAndListPlans(t *testing.T) {
	entry := &HotListEntry{EntryID: "e1", ProviderID: "p", Rank: 3, Word: "围棋比赛"}
	pull := &HotListPull{ReceiptID: "pr1", ProviderID: "p", ListURL: "https://www.example.com/list", PulledAt: 100.0}
	interest := &LongTermProfileInterestInput{InterestSummaryID: "i1", SummaryText: "学围棋"}
	plan := HotNewsPlan([]string{"news-c", "hot-c"}, entry, pull, interest,
		"matched", "围棋 比赛", "围棋学棋", GlobalNewChatScope)
	if pyStr(plan["news_match"]) != "matched" ||
		pyStr(plan["freshness_class"]) != NewsFreshnessClass {
		t.Fatalf("plan=%v", plan)
	}
	if mapAnyOf(plan["hot_list_entry"])["candidate_id"] == nil {
		t.Fatalf("hot list entry wiring")
	}
	// closest：无兴趣
	closest := HotNewsPlan([]string{"n2", "h2"}, entry, pull, nil,
		"closest", "围棋", nil, GlobalNewChatScope)
	if closest["interest_summary_id"] != nil {
		t.Fatalf("closest should have nil interest")
	}
	// hot list plan：无报道的新闻候选
	listPlan := HotListPlan(closest, "snap1")
	evidence := mapAnyOf(listPlan["hot_list_evidence"])
	if pyStr(evidence["source_kind"]) != HotListSourceKind ||
		pyStr(evidence["document_revision_id"]) != "hot-list:pr1:e1" {
		t.Fatalf("evidence=%v", evidence)
	}
	receipt := mapAnyOf(evidence["host_receipt"])
	if pyStr(receipt["receipt_version"]) != HotListEntryReceiptVersion ||
		pyStr(receipt["lane"]) != "host_hot_list_entry" {
		t.Fatalf("receipt")
	}
	// 槽记录
	slot := NewsSlotRecord([]map[string]any{listPlan}, "")
	if pyStr(slot["candidate_id"]) != "h2" || pyStr(slot["hot_entry_id"]) != "e1" {
		t.Fatalf("slot=%v", slot)
	}
	absent := NewsSlotRecord(nil, "hot_list_unavailable")
	if absent["candidate_id"] != nil || pyStr(absent["absent_reason"]) != "hot_list_unavailable" {
		t.Fatalf("absent=%v", absent)
	}
}

// TestDefaultV6SourceClassifier 对齐 :1535-1581。
func TestDefaultV6SourceClassifier(t *testing.T) {
	cases := []struct {
		url  string
		kind string
	}{
		{"https://arxiv.org/abs/2401.12345", "original"},
		{"https://www.nature.com/articles/s41586-x", "original"},
		{"https://www.nature.com/news/title", "reliable_report"},
		{"https://ai.meta.com/blog", "authoritative"},
		{"https://sub.gov.cn/page", "authoritative"},
		{"https://reuters.com/tech", "reliable_report"},
		{"https://en.wikipedia.org/wiki/Go", "stable"},
		{"https://github.com/user/repo", "original"},
		{"https://github.com/topics/go", "unknown"},
		{"https://pypi.org/project/pkg/", "stable"},
		{"https://unknown.example.com/x", "unknown"},
	}
	for _, tc := range cases {
		got := DefaultV6SourceClassifier(tc.url, "")
		if got.Kind != tc.kind {
			t.Fatalf("url=%s got=%q want=%q", tc.url, got.Kind, tc.kind)
		}
	}
	// github owner publisher
	got := DefaultV6SourceClassifier("https://github.com/User/Repo", "")
	if got.Publisher != "github.com/user" {
		t.Fatalf("publisher=%q", got.Publisher)
	}
}

// TestOfferedTitleSimilarity_复述判定 对齐 :1180-1206。
func TestOfferedTitleSimilarity(t *testing.T) {
	// 完全同题
	sim := bigramJaccard("围棋布局的三个要点", "围棋布局的三个要点")
	if sim != 1.0 {
		t.Fatalf("same title sim=%v", sim)
	}
	// 无关
	if bigramJaccard("围棋布局", " Rails 单元测试 ") > 0.1 {
		t.Fatalf("unrelated should be low")
	}
	// 复述计划
	plan := map[string]any{"candidate_id": "c1", "interest_summary_id": "i1",
		"direction": "围棋", "title": "围棋布局的三个要点"}
	restating := RestatingPlans([]map[string]any{plan},
		[]string{"围棋布局的三个要点", "别的"})
	if len(restating) != 1 {
		t.Fatalf("restating=%d", len(restating))
	}
	detail := restating[0].Detail
	if toFloatOr(detail["similarity"], 0) < RestatedOfferedSimilarity {
		t.Fatalf("similarity=%v", detail["similarity"])
	}
	// 未复述
	if len(RestatingPlans([]map[string]any{plan}, []string{"完全不同的标题"})) != 0 {
		t.Fatalf("different title should not restate")
	}
}

// TestNormalizeNewsQuery 对齐 topic_news.py:64-72。
func TestNormalizeNewsQuery(t *testing.T) {
	if NormalizeNewsQuery("围棋 比赛 v2.1") != "围棋 比赛" {
		t.Fatalf("version drop: %q", NormalizeNewsQuery("围棋 比赛 v2.1"))
	}
	if NormalizeNewsQuery("围棋 2026年 比赛结果") != "围棋 比赛结果" {
		t.Fatalf("date drop")
	}
	if NormalizeNewsQuery("  ") != "" {
		t.Fatalf("blank fallback to strip")
	}
}

// TestCarriedPlanAndSlots 对齐 :1322-1338。
func TestCarriedPlanAndSlots(t *testing.T) {
	plan := map[string]any{"candidate_id": "c1"}
	carried := CarriedPlan(plan, map[string]any{
		"generation_input_snapshot_id": "snap-old", "expire_time": 900.0})
	if pyStr(carried["retained_from_snapshot_id"]) != "snap-old" ||
		toFloatOr(carried["retained_expire_time"], 0) != 900.0 {
		t.Fatalf("carried=%v", carried)
	}
	slots := CandidateSlots([]RankedInterest{{InterestSummaryID: "i1", Rank: 1}})
	if len(slots) != 1 || !strings.HasPrefix(pyStr(slots[0]["slot_id"]), "candidate-") {
		t.Fatalf("slots=%v", slots)
	}
	// HostEvidence 三键优先
	plan1 := map[string]any{"project_evidence": map[string]any{"k": "p"},
		"profile_evidence": map[string]any{"k": "f"}}
	if pyStr(HostEvidence(plan1)["k"]) != "p" {
		t.Fatalf("project first")
	}
	plan2 := map[string]any{"profile_evidence": map[string]any{"k": "f"},
		"hot_list_evidence": map[string]any{"k": "h"}}
	if pyStr(HostEvidence(plan2)["k"]) != "f" {
		t.Fatalf("profile second")
	}
}
