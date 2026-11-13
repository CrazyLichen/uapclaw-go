package recommendation

// topic_product_grounding_test.go 对齐上游摘要/grounding 辅助校验用例。

import "testing"

// summaryPayloadFor 构造摘要载荷（复用既有构造器后取载荷）。
func summaryPayloadFor(t *testing.T, summaryID, sessionID string) map[string]any {
	t.Helper()
	return summaryForTests(t, summaryID, sessionID, 10.0).AsPayload()
}

// TestValidSessionSummaryArtifacts 对齐 :921-950。
func TestValidSessionSummaryArtifacts(t *testing.T) {
	manifest := map[string]any{
		"context_scope": GlobalNewChatScope,
		"session_summary_artifacts": []any{
			summaryPayloadFor(t, "summary:s1:"+hex64("a"), "s1"),
		},
	}
	if !ValidSessionSummaryArtifacts(manifest) {
		t.Fatalf("valid manifest should pass")
	}
	// 重复摘要 ID
	dup := map[string]any{
		"context_scope": GlobalNewChatScope,
		"session_summary_artifacts": []any{
			summaryPayloadFor(t, "summary:s1:"+hex64("a"), "s1"),
			summaryPayloadFor(t, "summary:s1:"+hex64("a"), "s1"),
		},
	}
	if ValidSessionSummaryArtifacts(dup) {
		t.Fatalf("duplicate ids should fail")
	}
	// 摘要 scope 与清单不一致
	other := summaryPayloadFor(t, "summary:s2:"+hex64("b"), "s2")
	mapAnyOf(other)["context_scope"] = "project:p1"
	badScope := map[string]any{
		"context_scope": GlobalNewChatScope, "session_summary_artifacts": []any{other},
	}
	if ValidSessionSummaryArtifacts(badScope) {
		t.Fatalf("scope mismatch should fail")
	}
}

// TestValidSessionSummaryCoverageReceipt 对齐 :961-1030。
func TestValidSessionSummaryCoverageReceipt(t *testing.T) {
	summary := summaryForTests(t, "summary:s1:"+hex64("a"), "s1", 10.0)
	manifest := map[string]any{
		"generation_input_snapshot_id": "snap1",
		"context_scope":                GlobalNewChatScope,
		"session_summary_artifacts":    []any{summary.AsPayload()},
	}
	receipt := map[string]any{
		"schema_version":         SessionSummaryCoverageReceiptVersion,
		"input_snapshot_id":      "snap1",
		"context_scope":          GlobalNewChatScope,
		"summary_config_version": "cfg-v1",
		"sessions":               []any{},
	}
	manifest["session_summary_coverage_receipt"] = receipt
	// expected sessions 由摘要推导
	manifestSessions := []any{map[string]any{
		"session_id": summary.SessionID, "session_summary_id": summary.SessionSummaryID,
		"input_origin": summary.InputOrigin, "assistance_origin": summary.AssistanceOrigin,
		"rid_eligible": true, "rid_exclusion_reason": nil,
	}}
	receipt["sessions"] = manifestSessions
	if !ValidSessionSummaryCoverageReceipt(manifest, false) {
		t.Fatalf("valid coverage should pass")
	}
	// 会话条目与摘要不符
	receipt["sessions"] = []any{}
	if ValidSessionSummaryCoverageReceipt(manifest, false) {
		t.Fatalf("mismatched sessions should fail")
	}
	// legacy 在不允许时拒绝
	legacy := map[string]any{
		"schema_version":    LegacySessionSummaryCoverageReceiptVersion,
		"input_snapshot_id": "snap1", "context_scope": GlobalNewChatScope,
		"sessions": manifestSessions,
	}
	manifest["session_summary_coverage_receipt"] = legacy
	if ValidSessionSummaryCoverageReceipt(manifest, false) {
		t.Fatalf("legacy without allow should fail")
	}
	// legacy 允许且无物化小票 → 通过
	if !ValidSessionSummaryCoverageReceipt(manifest, true) {
		t.Fatalf("legacy allowed should pass")
	}
}

// TestValidSessionSummaryMaterializationReceipt 对齐 :1032-1109。
func TestValidSessionSummaryMaterializationReceipt(t *testing.T) {
	summary := summaryForTests(t, "summary:s1:"+hex64("a"), "s1", 10.0)
	manifest := map[string]any{
		"generation_input_snapshot_id": "snap1",
		"context_scope":                GlobalNewChatScope,
		"session_summary_artifacts":    []any{summary.AsPayload()},
		"session_summary_coverage_receipt": map[string]any{
			"summary_config_version": "cfg-v1"},
	}
	// 无物化小票 → 直接通过
	if !ValidSessionSummaryMaterializationReceipt(manifest) {
		t.Fatalf("absent receipt should pass")
	}
	receipt := map[string]any{
		"schema_version":                SessionSummaryMaterializationReceiptVersion,
		"input_snapshot_id":             "snap1",
		"context_scope":                 GlobalNewChatScope,
		"summary_config_version":        "cfg-v1",
		"generated_session_summary_ids": []any{summary.SessionSummaryID},
		"reused_session_summary_ids":    []any{},
	}
	manifest["session_summary_materialization_receipt"] = receipt
	if !ValidSessionSummaryMaterializationReceipt(manifest) {
		t.Fatalf("generated-only should pass")
	}
	// generated 与 reused 交集拒绝
	receipt["reused_session_summary_ids"] = []any{summary.SessionSummaryID}
	if ValidSessionSummaryMaterializationReceipt(manifest) {
		t.Fatalf("overlap should fail")
	}
	// 并集不等于工件集拒绝
	receipt["generated_session_summary_ids"] = []any{}
	receipt["reused_session_summary_ids"] = []any{"other"}
	if ValidSessionSummaryMaterializationReceipt(manifest) {
		t.Fatalf("union mismatch should fail")
	}
}

// TestValidGroundingPublicationDateProvenance 对齐 :1885-1939。
func TestValidGroundingPublicationDateProvenance(t *testing.T) {
	// nil 放行
	if !ValidGroundingPublicationDateProvenance(nil, "https://a.com", 100.0) {
		t.Fatalf("nil provenance should pass")
	}
	// published_at 缺失时非 linked 分支拒绝
	if ValidGroundingPublicationDateProvenance(map[string]any{}, "https://a.com", nil) {
		t.Fatalf("nil published_at should fail")
	}
	// structured_search_result 合格
	provenance := map[string]any{
		"source_type": "structured_search_result", "source_field": "date",
		"raw_value": "2026-01-01", "search_provider_id": "p",
		"search_receipt_id": "r", "search_result_url": "https://a.com",
	}
	if !ValidGroundingPublicationDateProvenance(provenance, "https://a.com", 100.0) {
		t.Fatalf("structured provenance should pass")
	}
	// search_result_url 与 source_url 不一致拒绝
	bad := jsonRoundTrip(provenance)
	bad["search_result_url"] = "https://b.com"
	if ValidGroundingPublicationDateProvenance(bad, "https://a.com", 100.0) {
		t.Fatalf("url mismatch should fail")
	}
	// page_explicit_metadata 搜索字段必须为空
	page := map[string]any{
		"source_type": "page_explicit_metadata", "source_field": "date",
		"raw_value": "2026-01-01", "search_provider_id": nil,
		"search_receipt_id": nil, "search_result_url": nil,
		"listed_at": "2026-01-01",
	}
	if !ValidGroundingPublicationDateProvenance(page, "https://a.com", 100.0) {
		t.Fatalf("page metadata should pass")
	}
	pageBad := jsonRoundTrip(page)
	pageBad["search_provider_id"] = "p"
	if ValidGroundingPublicationDateProvenance(pageBad, "https://a.com", 100.0) {
		t.Fatalf("page metadata with search fields should fail")
	}
}

// TestValidGroundingComponentReceipt 对齐 :1941-1979。
func TestValidGroundingComponentReceipt(t *testing.T) {
	success := map[string]any{
		"receipt_version": GroundingEvidenceReceiptVersion,
		"binding": map[string]any{
			"component_id": "verbatim_web_evidence", "contract_version": "v1",
			"provider_id": "p", "provider_version": "pv", "config_version": "cv",
		},
		"input_snapshot_id": "snap1", "request_id": "req1",
		"score_semantics": "uncalibrated_api_proxy", "output_status": "success",
		"fallback_reason": nil,
	}
	if !ValidGroundingComponentReceipt(success, "snap1", "req1", "success", nil) {
		t.Fatalf("success receipt should pass")
	}
	// 失败小票
	failed := map[string]any{
		"receipt_version": "v1",
		"binding": map[string]any{
			"component_id": "verbatim_web_evidence", "contract_version": "v1",
			"provider_id": "p", "provider_version": "pv", "config_version": "cv",
		},
		"input_snapshot_id": "snap1", "request_id": "req1",
		"score_semantics": "not_scored", "output_status": "failed",
		"fallback_reason": "provider_timeout",
	}
	if !ValidGroundingComponentReceipt(failed, "snap1", "req1", "failed", "provider_timeout") {
		t.Fatalf("failed receipt should pass")
	}
	// fallback_reason 不匹配拒绝
	if ValidGroundingComponentReceipt(failed, "snap1", "req1", "failed", "other") {
		t.Fatalf("reason mismatch should fail")
	}
}

// TestGroundingSearchResultUrls 对齐 :1981-2021。
func TestGroundingSearchResultUrls(t *testing.T) {
	native := map[string]any{
		"receipt_version": OpenjiuwenNativeSearchReceiptVersion,
		"provider_id":     OpenjiuwenNativeSearchProviderID, "host_request_id": "h1", "requested_query": "围棋",
		"engine": "duckduckgo", "result_urls": []any{"https://a.com", "https://b.com"},
		"result_count": 2,
	}
	urls := GroundingSearchResultUrls(native, "围棋", OpenjiuwenNativeSearchProviderID)
	if len(urls) != 2 || urls[0] != "https://a.com" {
		t.Fatalf("urls=%v", urls)
	}
	// query 不匹配
	if GroundingSearchResultUrls(native, "象棋", OpenjiuwenNativeSearchProviderID) != nil {
		t.Fatalf("query mismatch should fail")
	}
	// Ark 附加 result_urls 字段
	ark := arkReceiptMap()
	ark["result_urls"] = []any{"https://a.com"}
	arkURLs := GroundingSearchResultUrls(ark, "围棋", ArkWebSearchProviderID)
	if len(arkURLs) != 1 || arkURLs[0] != "https://a.com" {
		t.Fatalf("ark urls=%v", arkURLs)
	}
	// Ark result_count < len(urls) 拒绝
	bad := jsonRoundTrip(ark)
	bad["result_urls"] = []any{"https://a.com", "https://b.com"}
	if GroundingSearchResultUrls(bad, "围棋", ArkWebSearchProviderID) != nil {
		t.Fatalf("count underflow should fail")
	}
}

// TestExternalOperationCoversCandidate 对齐 :1578-1604。
func TestExternalOperationCoversCandidate(t *testing.T) {
	manifest := map[string]any{"grounding_evidence_receipt": map[string]any{
		"candidates": []any{
			map[string]any{"candidate_id": "owner", "fetches": []any{
				map[string]any{"operation_id": "op1", "request_id": "r1",
					"requested_url": "https://a.com", "final_url": "https://a.com/f",
					"http_status_code": "200"},
			}},
			map[string]any{"candidate_id": "other", "fetches": []any{
				map[string]any{"operation_id": "op1", "request_id": "r1",
					"requested_url": "https://a.com", "final_url": "https://a.com/f",
					"http_status_code": "200"},
			}},
		},
	}}
	audit := map[string]any{"candidate_id": "owner"}
	// 自己 → 覆盖
	if !ExternalOperationCoversCandidate(manifest, audit,
		[3]string{"fetch", "op1", "r1"}, "owner") {
		t.Fatalf("self should cover")
	}
	// 共享 fetch 且元数据一致 → 覆盖
	if !ExternalOperationCoversCandidate(manifest, audit,
		[3]string{"fetch", "op1", "r1"}, "other") {
		t.Fatalf("shared fetch should cover")
	}
	// search 操作不共享
	if ExternalOperationCoversCandidate(manifest, audit,
		[3]string{"search", "op1", "r1"}, "other") {
		t.Fatalf("search should not cover others")
	}
}

// TestTerminalStageMatchesOperations 对齐 :1606-1619。
func TestTerminalStageMatchesOperations(t *testing.T) {
	if !TerminalStageMatchesOperations("search", []string{"search"}) {
		t.Fatalf("search exact")
	}
	if TerminalStageMatchesOperations("search", []string{"fetch"}) {
		t.Fatalf("search must be exact")
	}
	if !TerminalStageMatchesOperations("fetch", []string{"search", "fetch"}) {
		t.Fatalf("fetch last")
	}
	if TerminalStageMatchesOperations("fetch", []string{"fetch", "search"}) {
		t.Fatalf("fetch must be last")
	}
	if !TerminalStageMatchesOperations("extraction", []string{"fetch", "extractor"}) {
		t.Fatalf("extractor last")
	}
	if !TerminalStageMatchesOperations("unknown", []string{}) {
		t.Fatalf("unknown stage always true")
	}
}

// hex64 生成 64 位十六进制摘要串（a-f 循环填充）。
func hex64(seed string) string {
	out := make([]rune, 0, 64)
	for i := 0; len(out) < 64; i++ {
		out = append(out, rune('a'+(i+int(seed[0]))%6))
	}
	return string(out)
}
