package recommendation

// topic_pipeline_c8_test.go 对齐 topic_product.py:3762-3840/:3069-3084 与
// topic_pipeline.py:4135-4341 契约的单元用例。

import (
	"strings"
	"testing"
)

// TestCardExpireTime 对齐 :3762-3778。
func TestCardExpireTime(t *testing.T) {
	// 常规类一周
	week := CardExpireTime(map[string]any{"category": "tech-ai-ml"}, 1000.0)
	if week != 1000.0+7*24*60*60 {
		t.Fatalf("week: %v", week)
	}
	// 近期类一天
	day := CardExpireTime(map[string]any{"category": "profile_recent_progress"}, 1000.0)
	if day != 1000.0+24*60*60 {
		t.Fatalf("day: %v", day)
	}
	// 保留值优先
	retained := CardExpireTime(map[string]any{
		"category": "profile_recent_progress", "retained_expire_time": 5000.0}, 1000.0)
	if retained != 5000.0 {
		t.Fatalf("retained: %v", retained)
	}
	// 新闻事件远早于过期窗口：72 小时截断生效
	news := CardExpireTime(map[string]any{
		"category": "news", "freshness_class": NewsFreshnessClass,
		"evidence_bundle": map[string]any{"published_at": "1970-01-03T00:00:00+00:00"},
	}, 1000.0)
	if news != 2*24*60*60+72*60*60 {
		t.Fatalf("news cut: %v", news)
	}
}

// TestBuildTerminalCard 对齐 :3780-3840。
func TestBuildTerminalCard(t *testing.T) {
	triggerKey := map[string]any{
		"search_provider_id": "ark", "profile_semantic_version": 3,
		"short_intent_version": 2, "generation_config_version": "gen-v1",
		"corpus_version": "corpus-v1", "document_snapshot_version": "doc-v1",
		"retrieval_config_version": "ret-v1", "evidence_extractor_version": "ext-v1",
	}
	plan := map[string]any{
		"candidate_id": "cand-1", "category": "tech-ai-ml", "intent": "想学",
		"direction": "大模型", "news_match": "matched", "title": "标题",
		"prompt": "提示", "evidence_bundle": map[string]any{"source_url": "https://e"},
		"search_receipt":         map[string]any{"provider_id": "ark"},
		"source_memory_unit_ids": []any{"unit-1"},
		"source_locators":        []any{}, "project_source_locators": []any{},
	}
	card := BuildTerminalCard(plan, 2, "batch-1", "project:p1", "p1", "snap-1",
		triggerKey, map[string]any{"rid": "r"}, "list-v1", nil, 1000.0)
	if card["card_id"] != "cand-1" || card["selector_rank"] != 2 {
		t.Fatalf("identity: %v", card["card_id"])
	}
	if !strings.HasPrefix(card["card_revision_id"].(string), "v6-revision-") {
		t.Fatalf("revision: %v", card["card_revision_id"])
	}
	if card["direction"] != "大模型" || card["news_match"] != "matched" {
		t.Fatalf("optional fields: %v %v", card["direction"], card["news_match"])
	}
	if card["expire_time"] != 1000.0+7*24*60*60 {
		t.Fatalf("expire: %v", card["expire_time"])
	}
	if card["policy_id"] != POLICY_ID || card["payload_version"] != PAYLOAD_VERSION {
		t.Fatalf("versions: %v", card)
	}
	if _, present := card["state_maintenance_receipt"]; present {
		t.Fatalf("nil receipt should be absent")
	}
	// 无方向/news_match 时不写键
	plain := BuildTerminalCard(map[string]any{
		"candidate_id": "c2", "category": "c", "intent": "i", "title": "t",
		"prompt": "p", "evidence_bundle": map[string]any{},
	}, 1, "b", GlobalNewChatScope, nil, "s", triggerKey, map[string]any{}, "l", nil, 0)
	if _, present := plain["direction"]; present {
		t.Fatalf("no direction key expected")
	}
}

// TestProviderReceiptAppliesToCard 对齐 :3069-3084。
func TestProviderReceiptAppliesToCard(t *testing.T) {
	card := map[string]any{"card_id": "c1", "source_starter_id": "st-1"}
	if !ProviderReceiptAppliesToCard(map[string]any{
		"lineage": map[string]any{"binding_scope": "all_cards"}}, card) {
		t.Fatalf("all_cards")
	}
	if !ProviderReceiptAppliesToCard(map[string]any{
		"lineage": map[string]any{"binding_scope": "candidate", "candidate_ids": []any{"c1"}}}, card) {
		t.Fatalf("candidate hit")
	}
	if ProviderReceiptAppliesToCard(map[string]any{
		"lineage": map[string]any{"binding_scope": "candidate", "candidate_ids": []any{"c2"}}}, card) {
		t.Fatalf("candidate miss")
	}
	if !ProviderReceiptAppliesToCard(map[string]any{
		"lineage": map[string]any{"binding_scope": "starter", "starter_ids": []any{"st-1"}}}, card) {
		t.Fatalf("starter hit")
	}
	if ProviderReceiptAppliesToCard(map[string]any{}, card) {
		t.Fatalf("no lineage")
	}
}

// successorPredecessor 构造前任池 manifest 与卡。
func successorPredecessor() (map[string]any, []map[string]any) {
	manifest := map[string]any{
		"manifest_id":                  "m-old",
		"generation_input_snapshot_id": "snap-1",
		"generation_trigger_key":       map[string]any{"search_provider_id": "ark"},
		"state_maintenance_receipt":    map[string]any{"refresh_reason": "behavior_mapping"},
		"list_search":                  map[string]any{"search_version": "list-v1"},
		"news_slot":                    map[string]any{"candidate_id": "lead"},
		"component_receipts": map[string]any{
			RID_COMPONENT_ID:     map[string]any{"request_id": "rid-req"},
			UTILITY_COMPONENT_ID: map[string]any{"request_id": "util-req"},
		},
		"component_invocation_receipts": []any{
			map[string]any{"component_id": UTILITY_COMPONENT_ID, "request_id": "util-req",
				"binding_scope": "candidate", "candidate_ids": []any{"lead"}},
			map[string]any{"component_id": RID_COMPONENT_ID, "request_id": "rid-req",
				"binding_scope": "all_cards", "candidate_ids": []any{}},
		},
		"ark_invocation_receipts": []any{
			map[string]any{"stage": "v6_hot_news_matcher", "lineage": map[string]any{
				"binding_scope": "candidate", "candidate_ids": []any{"lead", "news-cand"}}},
			map[string]any{"stage": "v6_slot_decision", "lineage": map[string]any{
				"binding_scope": "all_cards", "candidate_ids": []any{}}},
		},
		"grounding_decision_receipt": map[string]any{
			"decision": "admit",
			"candidate_decisions": []any{
				map[string]any{"candidate_id": "lead", "decision": "admit"},
				map[string]any{"candidate_id": "keep-1", "decision": "admit"},
			},
		},
		"grounding_evidence_receipt": map[string]any{
			"document_snapshot": map[string]any{
				"document_revision_ids": []any{"doc-keep", "doc-lead"},
				"documents": []any{
					map[string]any{"document_revision_id": "doc-keep"},
					map[string]any{"document_revision_id": "doc-lead"},
				},
			},
			"candidates": []any{
				map[string]any{"candidate_id": "lead",
					"fetches":   []any{map[string]any{"document_revision_id": "doc-lead"}},
					"extractor": nil},
				map[string]any{"candidate_id": "keep-1",
					"fetches":   []any{map[string]any{"document_revision_id": "doc-keep"}},
					"extractor": nil},
			},
		},
		"external_invocation_audit": map[string]any{"invocations": []any{
			map[string]any{"candidate_id": "lead", "sequence": 1,
				"operation": "search", "status": "success"},
			map[string]any{"candidate_id": "keep-1", "sequence": 2,
				"operation": "search", "status": "success"},
		}},
		"rid_interest_selection": map[string]any{"selected": []any{"int-a"}},
	}
	cards := []map[string]any{
		{"card_id": "lead", "card_revision_id": "rev-lead", "search_receipt": nil,
			"component_receipts": map[string]any{}},
		{"card_id": "keep-1", "card_revision_id": "rev-keep", "search_receipt": nil,
			"component_receipts": map[string]any{}},
	}
	return manifest, cards
}

// TestNewsOnlySuccessorResult 对齐 :4135-4341。
func TestNewsOnlySuccessorResult(t *testing.T) {
	predecessor, cards := successorPredecessor()
	news := map[string]any{
		"candidate_id": "news-cand", "category": "tech-ai-ml", "intent": "新闻",
		"title": "新闻标题", "prompt": "新闻提示",
		"evidence_bundle": map[string]any{"source_url": "https://n"},
	}
	utilityReceipt, err := PipelineSuccessReceipt(mustBinding(t, UTILITY_COMPONENT_ID), "snap-1", "util-new")
	if err != nil {
		t.Fatalf("receipt: %v", err)
	}
	config := &ArkBackgroundConfig{}
	result := NewsOnlySuccessorResult(predecessor, cards, news, nil, utilityReceipt,
		[]map[string]any{}, map[string]any{"invocations": []any{}}, config, 1000.0)
	manifest := result.Manifest
	if manifest["successor_kind"] != NewsOnlySuccessor {
		t.Fatalf("kind: %v", manifest["successor_kind"])
	}
	if manifest["trigger_origin"] != "news_check" {
		t.Fatalf("origin: %v", manifest["trigger_origin"])
	}
	if manifest["supersedes_manifest_id"] != "m-old" {
		t.Fatalf("supersedes: %v", manifest["supersedes_manifest_id"])
	}
	// 卡：新闻卡在首位（rank 1），lead 被移除，keep-1 保留且换修订/批次
	if len(result.Cards) != 2 {
		t.Fatalf("cards: %d", len(result.Cards))
	}
	if result.Cards[0]["card_id"] != "news-cand" || result.Cards[0]["selector_rank"] != 1 {
		t.Fatalf("news card first: %+v", result.Cards[0])
	}
	if result.Cards[1]["card_id"] != "keep-1" ||
		result.Cards[1]["card_revision_id"] == "rev-keep" ||
		result.Cards[1]["generation_batch_id"] != manifest["generation_batch_id"] {
		t.Fatalf("kept card: %+v", result.Cards[1])
	}
	// news-only 前任的效用调用被剔除（本前任非 news-only → 保留）
	invocations := pyListMaps(manifest["component_invocation_receipts"])
	if len(invocations) != 3 { // rid 全卡 + utility 候选 + 新效用候选
		t.Fatalf("invocations: %d", len(invocations))
	}
	// 厂商小票：hot_news_matcher 血缘的整条离开，slot_decision 保留
	arkReceipts := pyListMaps(manifest["ark_invocation_receipts"])
	if len(arkReceipts) != 1 || pyStr(arkReceipts[0]["stage"]) != "v6_slot_decision" {
		t.Fatalf("ark receipts: %v", arkReceipts)
	}
	// 接地决策：lead 被剔，keep-1 保留
	decisionReceipt := payloadMapOrEmpty(manifest["grounding_decision_receipt"])
	decisions := pyListMaps(decisionReceipt["candidate_decisions"])
	if len(decisions) != 1 || pyStr(decisions[0]["candidate_id"]) != "keep-1" {
		t.Fatalf("decisions: %v", decisions)
	}
	if decisionReceipt["decision"] != "admit" {
		t.Fatalf("aggregate: %v", decisionReceipt["decision"])
	}
	// 文档快照：只留被引用文档 + 新批文档
	documentSnapshot := payloadMapOrEmpty(manifest["document_snapshot"])
	documents := pyListMaps(documentSnapshot["documents"])
	if len(documents) != 1 || pyStr(documents[0]["document_revision_id"]) != "doc-keep" {
		t.Fatalf("documents: %v", documents)
	}
	// 外部审计：lead 条目被剔
	audit := payloadMapOrEmpty(manifest["external_invocation_audit"])
	auditInvocations := pyListMaps(audit["invocations"])
	if len(auditInvocations) != 1 || pyStr(auditInvocations[0]["candidate_id"]) != "keep-1" {
		t.Fatalf("audit: %v", auditInvocations)
	}
	// rid_interest_selection 保留并清空 delta
	selection := payloadMapOrEmpty(manifest["rid_interest_selection"])
	if pyListStrings(selection["short_trigger_delta_event_ids"]) == nil &&
		pyListMaps(selection["selected"]) == nil && len(selection) == 0 {
		t.Fatalf("selection: %v", selection)
	}
	if _, present := selection["selected"]; !present {
		t.Fatalf("selection keys: %v", selection)
	}
	if delta := pyListStrings(selection["short_trigger_delta_event_ids"]); len(delta) != 0 {
		t.Fatalf("delta: %v", delta)
	}
}

// mustBinding 组件绑定快捷构造。
func mustBinding(t *testing.T, componentID string) *LearnedComponentBinding {
	t.Helper()
	binding, err := PipelineAPIBinding(componentID, "ark", "v1", "cfg")
	if err != nil {
		t.Fatalf("binding: %v", err)
	}
	return binding
}

// TestNewsOnlySuccessorResult_NewsOnlyPredecessor 对齐 :4175-4189 效用剔除。
func TestNewsOnlySuccessorResult_NewsOnlyPredecessor(t *testing.T) {
	predecessor, _ := successorPredecessor()
	// 前任只有 lead 一张卡 → news-only 前任：旧效用调用剔除
	predecessorCards := []map[string]any{
		{"card_id": "lead", "card_revision_id": "rev-lead", "search_receipt": nil,
			"component_receipts": map[string]any{}},
	}
	news := map[string]any{
		"candidate_id": "news-cand", "category": "tech-ai-ml", "intent": "新闻",
		"title": "标题", "prompt": "提示", "evidence_bundle": map[string]any{},
	}
	utilityReceipt, _ := PipelineSuccessReceipt(mustBinding(t, UTILITY_COMPONENT_ID), "snap-1", "util-new")
	config := &ArkBackgroundConfig{}
	result := NewsOnlySuccessorResult(predecessor, predecessorCards, news, nil,
		utilityReceipt, []map[string]any{}, map[string]any{"invocations": []any{}}, config, 1000.0)
	invocations := pyListMaps(result.Manifest["component_invocation_receipts"])
	// 旧 utility 候选调用被剔；只剩 rid 全卡与新 utility 候选
	countUtility := 0
	for _, item := range invocations {
		if pyStr(item["component_id"]) == UTILITY_COMPONENT_ID &&
			pyStr(item["request_id"]) == "util-req" {
			countUtility++
		}
	}
	if countUtility != 0 {
		t.Fatalf("news-only predecessor utility should be dropped: %v", invocations)
	}
	// 池级 component_receipts 的 utility 换成新小票
	componentReceipts := payloadMapOrEmpty(result.Manifest["component_receipts"])
	newUtility := payloadMapOrEmpty(componentReceipts[UTILITY_COMPONENT_ID])
	if pyStr(newUtility["request_id"]) != "util-new" {
		t.Fatalf("utility replaced: %v", newUtility)
	}
}
