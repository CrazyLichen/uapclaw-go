package recommendation

// topic_product_contract_test.go 对齐 tests/unit_tests/test_topic_recommendation.py
// 中卡片文案拒绝码 / 失败小票 / v6 触发键 / 覆盖 topic_id 的用例。

import "testing"

// TestCardCopyRejectionReason_拒绝码枚举 对齐 :617-648 全部分支。
func TestCardCopyRejectionReason(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{nil, "empty"}, {"", "empty"}, {" x ", "untrimmed"},
		{"done.", "no_op"}, {"(something)", "meta_wrapped"},
		{"错误: 一切崩溃", "error_message"}, {"标题: 你好", "prefixed_label"},
		{"无可推荐", "meta_text"},
		{string(make([]rune, 70)), "too_long"}, // 标题上限 60
		{"**加粗**标题", "has_formatting"},
		{"谢谢！", "evaluative"}, {"great, thanks", "evaluative"},
		{"我来帮你", "assistant_voice"}, {"作为一个AI，我认为", "assistant_voice"},
		{"需要我帮你出方案吗？", "assistant_voice"},
		{"围棋布局有三种开局。第二种是星位。", "multiple_sentences"},
		{"围棋布局的三种开局", ""},
	}
	for _, tc := range cases {
		if got := CardCopyRejectionReason(tc.value, CardTitleMaxChars); got != tc.want {
			t.Fatalf("value=%q got=%q want=%q", tc.value, got, tc.want)
		}
	}
	// prompt 上限 180
	long := make([]rune, 200)
	if CardCopyRejectionReason(string(long), CardPromptMaxChars) != "too_long" {
		t.Fatalf("prompt limit")
	}
}

// TestValidNativeSearchFailureReceipt 对齐 :767-808。
func TestValidNativeSearchFailureReceipt(t *testing.T) {
	receipt := map[string]any{
		"receipt_version": OpenjiuwenNativeSearchFailureReceiptVersion,
		"provider_id":     OpenjiuwenNativeSearchProviderID,
		"host_request_id": "h1", "engine": "openjiuwen-free-search",
		"failure_kind": "timeout", "http_status": nil,
	}
	if !ValidNativeSearchFailureReceipt(receipt, "provider_timeout") {
		t.Fatalf("timeout receipt should pass")
	}
	// kind 与 reason 不匹配
	if ValidNativeSearchFailureReceipt(receipt, "provider_unknown") {
		t.Fatalf("reason mismatch should fail")
	}
	// http_status 分支
	httpReceipt := map[string]any{
		"receipt_version": OpenjiuwenNativeSearchFailureReceiptVersion,
		"provider_id":     OpenjiuwenNativeSearchProviderID,
		"host_request_id": "h1", "engine": "openjiuwen-free-search",
		"failure_kind": "http_status", "http_status": 503,
	}
	if !ValidNativeSearchFailureReceipt(httpReceipt, "provider_http_status") {
		t.Fatalf("http receipt should pass")
	}
	httpReceipt["http_status"] = 99
	if ValidNativeSearchFailureReceipt(httpReceipt, "provider_http_status") {
		t.Fatalf("status < 100 should fail")
	}
	// 非 http_status kind 的 http_status 必须为 nil
	if ValidNativeSearchFailureReceipt(map[string]any{
		"receipt_version": OpenjiuwenNativeSearchFailureReceiptVersion,
		"provider_id":     OpenjiuwenNativeSearchProviderID,
		"host_request_id": "h1", "engine": "openjiuwen-free-search",
		"failure_kind": "timeout", "http_status": 503,
	}, "provider_timeout") {
		t.Fatalf("non-http kind with status should fail")
	}
}

// TestBuildV6GenerationTriggerKey_守卫 对齐 :3287-3373。
func TestBuildV6GenerationTriggerKey(t *testing.T) {
	receipt := map[string]any{
		"receipt_version": InputRevisionReceiptVersion,
		"context_scope":   GlobalNewChatScope, "revision_digest": "abc",
	}
	components := map[string]string{
		"icebreaker_rid": "v1", "pa_bridge_fixed_retriever": "v1",
		"verbatim_web_evidence": "v1", "interaction_utility_scorer": "v1",
		"diversity_scorer": "v1",
	}
	key, err := BuildV6GenerationTriggerKey("2026-01-01", GlobalNewChatScope, nil,
		1, 0, 0, components, "m", GenerationConfigVersionV6, CorpusVersion,
		"doc-v1", RetrievalConfigVersion, "ext-v1", "provider", receipt)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if key["policy_id"] != ProductPolicyID || key["payload_version"] != ProductPayloadVersion {
		t.Fatalf("identity missing: %v", key)
	}
	// scope 与 project 不一致拒绝
	if _, err := BuildV6GenerationTriggerKey("2026-01-01", "project:p1", nil,
		1, 0, 0, components, "m", GenerationConfigVersionV6, CorpusVersion,
		"doc-v1", RetrievalConfigVersion, "ext-v1", "provider", receipt); err == nil {
		t.Fatalf("scope disagreement should fail")
	}
	// 组件版本不全拒绝
	short := map[string]string{"icebreaker_rid": "v1"}
	if _, err := BuildV6GenerationTriggerKey("2026-01-01", GlobalNewChatScope, nil,
		1, 0, 0, short, "m", GenerationConfigVersionV6, CorpusVersion,
		"doc-v1", RetrievalConfigVersion, "ext-v1", "provider", receipt); err == nil {
		t.Fatalf("incomplete components should fail")
	}
	// 小票缺失拒绝
	if _, err := BuildV6GenerationTriggerKey("2026-01-01", GlobalNewChatScope, nil,
		1, 0, 0, components, "m", GenerationConfigVersionV6, CorpusVersion,
		"doc-v1", RetrievalConfigVersion, "ext-v1", "provider", nil); err == nil {
		t.Fatalf("missing receipt should fail")
	}
	// 负版本拒绝
	if _, err := BuildV6GenerationTriggerKey("2026-01-01", GlobalNewChatScope, nil,
		-1, 0, 0, components, "m", GenerationConfigVersionV6, CorpusVersion,
		"doc-v1", RetrievalConfigVersion, "ext-v1", "provider", receipt); err == nil {
		t.Fatalf("negative version should fail")
	}
	// serving key 剔除新鲜度字段
	serving := BuildV6ServingCompatibilityKey(key)
	for _, fresh := range []string{"input_revision_receipt", "negative_preference_version",
		"local_date", "document_snapshot_version", "profile_semantic_version", "short_intent_version"} {
		if _, present := serving[fresh]; present {
			t.Fatalf("freshness key %q should be excluded", fresh)
		}
	}
	if serving["policy_id"] != ProductPolicyID {
		t.Fatalf("identity keys must stay")
	}
}

// TestSessionSummaryCoverageTopicID 对齐 :952-959。
func TestSessionSummaryCoverageTopicID(t *testing.T) {
	coverage := map[string]any{
		"input_snapshot_id": "snap1", "summary_config_version": "cfg1",
		"sessions": []any{
			map[string]any{"session_summary_id": "summary:a"},
			map[string]any{"session_summary_id": "summary:b"},
		},
	}
	got := SessionSummaryCoverageTopicID(coverage)
	if got != "snap1|cfg1|[\"summary:a\",\"summary:b\"]" {
		t.Fatalf("got=%q", got)
	}
}

// TestNewsOnlyPool 对齐 :2427-2434。
func TestNewsOnlyPool(t *testing.T) {
	manifest := map[string]any{"news_slot": map[string]any{
		"candidate_id": "news-card", "absent_reason": nil}}
	if !NewsOnlyPool(manifest, []map[string]any{{"card_id": "news-card"}}) {
		t.Fatalf("news-only pool")
	}
	if NewsOnlyPool(manifest, []map[string]any{{"card_id": "other"}}) {
		t.Fatalf("different card should fail")
	}
	// absent → 无 lead
	absent := map[string]any{"news_slot": map[string]any{
		"candidate_id": "x", "absent_reason": "hot_list_unavailable"}}
	if NewsLeadCardId(absent) != "" {
		t.Fatalf("absent slot should have no lead")
	}
	// 多卡不是 news-only
	if NewsOnlyPool(manifest, []map[string]any{
		{"card_id": "news-card"}, {"card_id": "c2"}}) {
		t.Fatalf("two cards should fail")
	}
}

// TestValidInputRevisionReceipt 对齐 :3276-3285。
func TestValidInputRevisionReceipt(t *testing.T) {
	receipt := map[string]any{
		"receipt_version": InputRevisionReceiptVersion,
		"context_scope":   GlobalNewChatScope, "revision_digest": "abc",
	}
	if !ValidInputRevisionReceipt(receipt, GlobalNewChatScope) {
		t.Fatalf("should pass")
	}
	// digest 可选但必须非空
	withDigest := jsonRoundTrip(receipt)
	withDigest["discussion_coverage_digest"] = ""
	if ValidInputRevisionReceipt(withDigest, GlobalNewChatScope) {
		t.Fatalf("blank digest should fail")
	}
	withDigest["discussion_coverage_digest"] = "d1"
	if !ValidInputRevisionReceipt(withDigest, GlobalNewChatScope) {
		t.Fatalf("non-blank digest should pass")
	}
	// scope 不匹配
	if ValidInputRevisionReceipt(receipt, "project:p1") {
		t.Fatalf("scope mismatch should fail")
	}
	// 多余键拒绝
	extra := jsonRoundTrip(receipt)
	extra["extra"] = 1
	if ValidInputRevisionReceipt(extra, GlobalNewChatScope) {
		t.Fatalf("extra key should fail")
	}
}
