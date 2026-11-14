package recommendation

// topic_product_validate3a.go 一比一对齐上游 topic_product.py 校验族 v3a：
// :4136-4185 _news_slot_rejection_reason、:2777-2845 组件调用三件、
// :2845-2937 _component_lineage_rejection_reason、:2939-3086
// _valid_provider_receipt、:3087-3189 _provider_lineage_rejection_reason、
// :3192-3240 terminal_model_binding_rejection_reason（⤴️31c 归位）。

import (
	"context"
	"math"
	"strings"

	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 常量 ────────────────────────────

// manifestFields manifest 必需字段集（topic_product.py:330-362）。
var manifestFields = map[string]bool{
	"manifest_id": true, "generation_batch_id": true, "card_revision_ids": true,
	"trigger_origin": true, "local_date": true, "context_scope": true,
	"project_id": true, "generation_trigger_key": true,
	"generation_input_snapshot_id": true, "state_maintenance_receipt": true,
	"serving_compatibility_key": true, "profile_semantic_version": true,
	"short_intent_version": true, "negative_preference_version": true,
	"component_receipts": true, "component_invocation_receipts": true,
	"ark_invocation_receipts": true, "search_provider_id": true,
	"search_receipts": true, "session_summary_artifacts": true,
	"session_summary_coverage_receipt": true, "rid_result": true,
	"rid_interest_selection": true, "grounding_decision_receipt": true,
	"grounding_evidence_receipt": true, "external_invocation_audit": true,
	"list_search": true, "preference_pairs": true,
	"generation_config_version": true, "supersedes_manifest_id": true,
	"policy_id": true, "payload_version": true,
}

// providerReceiptFields 厂商小票必需字段（:378-388）。
var providerReceiptFields = map[string]bool{
	"stage": true, "provider_response_id": true, "response_object": true,
	"actual_model": true, "finish_reason": true, "usage": true,
	"latency_ms": true, "structured_output_mode": true, "lineage": true,
}

// providerLineageFields 厂商血缘字段（:380-387）。
var providerLineageFields = map[string]bool{
	"binding_scope": true, "candidate_ids": true, "starter_ids": true,
	"component_request_id": true, "input_snapshot_id": true,
}

// slotDecisionProviderStages 槽决策厂商阶段（:389-391）。
var slotDecisionProviderStages = map[string]bool{
	"v6_global_interest_generator": true, "v6_project_continuation_generator": true,
}

// skippedSlotFields 跳过槽字段（:392-397）。
var skippedSlotFields = map[string]bool{
	"slot_id": true, "interest_summary_id": true, "blocked_by": true,
	"nearest_covered": true, "new_angle": true,
}

// allCardProviderStages 全卡厂商阶段（:552-554）。
var allCardProviderStages = map[string]bool{
	"v6_session_summarizer": true, "rid_interest_distiller": true,
}

// candidateProviderStages 候选厂商阶段（:555-567）。
var candidateProviderStages = map[string]bool{
	"v6_hot_news_matcher": true, "v6_recent_candidate_generator": true,
	"v6_recent_finalizer": true, "v6_project_domain_candidate_generator": true,
	"v6_project_continuation_generator": true, "v6_global_interest_generator": true,
	"v6_evidence_extractor": true, "v6_utility_scorer": true,
	"v6_diversity_scorer": true,
}

// starterProviderStages starter 厂商阶段（:568-571）。
var starterProviderStages = map[string]bool{
	"v6_pa_fixed_retriever": true, "v6_i4_fixed_enricher": true,
}

// providerComponentByStage 阶段 → 组件 ID（:572-578）。
var providerComponentByStage = map[string]string{
	"rid_interest_distiller": "icebreaker_rid",
	"v6_pa_fixed_retriever":  "pa_bridge_fixed_retriever",
	"v6_evidence_extractor":  "verbatim_web_evidence",
	"v6_utility_scorer":      "interaction_utility_scorer",
	"v6_diversity_scorer":    "diversity_scorer",
}

// providerStagesByCategory 类目 → 必需厂商阶段（:579-600）。
var providerStagesByCategory = map[string]map[string]bool{
	"profile_recent_progress": {"v6_recent_candidate_generator": true, "v6_recent_finalizer": true},
	"adjacent_explanation":    {"v6_i4_fixed_enricher": true},
	"broad_timely_practical":  {"v6_i4_fixed_enricher": true},
	"project_continuation":    {"v6_project_continuation_generator": true},
	ProfileInterestCategory:   {"v6_global_interest_generator": true},
	"project_domain_progress": {"v6_project_domain_candidate_generator": true, "v6_recent_finalizer": true},
}

// commonCardProviderStages 公共卡厂商阶段（:601-603）。
var commonCardProviderStages = map[string]bool{"v6_session_summarizer": true}

// newsSlotFieldsSet 新闻槽字段集（:98-100 newsSlotFields 的集合形态）。
var newsSlotFieldsSet = func() map[string]bool {
	set := map[string]bool{}
	for _, field := range newsSlotFields {
		set[field] = true
	}
	return set
}()

// componentInvocationFields 组件调用小票字段（:265-273）。
var componentInvocationFields = map[string]bool{
	"schema_version": true, "component_id": true, "request_id": true,
	"input_snapshot_id": true, "binding_scope": true,
	"candidate_ids": true, "starter_ids": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewsSlotRejectionReason 全局池以其唯一新闻卡领衔，或记录缺席原因
// （topic_product.py:4136-4185 _news_slot_rejection_reason）。
func NewsSlotRejectionReason(manifest map[string]any, cards []map[string]any) string {
	if _, present := manifest["news_slot"]; !present {
		return ""
	}
	const failure = "manifest_field_news_slot_contract_failed"
	if pyStr(manifest["context_scope"]) != GlobalNewChatScope {
		return failure
	}
	slot, isMap := manifest["news_slot"].(map[string]any)
	if !isMap || len(slot) != len(newsSlotFieldsSet) {
		return failure
	}
	for _, field := range newsSlotFields {
		if _, present := slot[field]; !present {
			return failure
		}
	}
	for _, field := range []string{"candidate_id", "hot_entry_id", "hot_list_pull_receipt_id"} {
		if slot[field] != nil && pyStr(slot[field]) == "" {
			return failure
		}
	}
	if slot["match"] != nil && pyStr(slot["match"]) != "matched" &&
		pyStr(slot["match"]) != "closest" {
		return failure
	}
	if slot["card_form"] != nil && pyStr(slot["card_form"]) != NewsSourceKind &&
		pyStr(slot["card_form"]) != HotListSourceKind {
		return failure
	}
	var newsCards []map[string]any
	for _, card := range cards {
		if CardIsNews(card) {
			newsCards = append(newsCards, card)
		}
	}
	if slot["absent_reason"] != nil {
		if NewsSlotAbsentReasons[pyStr(slot["absent_reason"])] && len(newsCards) == 0 {
			return ""
		}
		return failure
	}
	var lead map[string]any
	for _, card := range cards {
		if pyStr(card["card_id"]) == pyStr(slot["candidate_id"]) {
			lead = card
			break
		}
	}
	if lead == nil {
		return failure
	}
	if len(newsCards) != 1 || newsCards[0]["card_id"] != lead["card_id"] {
		return failure
	}
	if pyIntOf(lead["selector_rank"]) != 1 {
		return failure
	}
	leadBundle := payloadMapOrEmpty(lead["evidence_bundle"])
	if pyStr(leadBundle["source_kind"]) != pyStr(slot["card_form"]) {
		return failure
	}
	if slot["match"] == nil {
		return failure
	}
	if (pyStr(slot["match"]) == "closest") != (lead["interest_summary_id"] == nil) {
		return failure
	}
	return ""
}

// ValidComponentInvocation 组件调用小票校验（:2777-2817）。
func ValidComponentInvocation(receipt any) bool {
	root, isMap := receipt.(map[string]any)
	if !isMap || len(root) != len(componentInvocationFields) {
		return false
	}
	for field := range componentInvocationFields {
		if _, present := root[field]; !present {
			return false
		}
	}
	if pyStr(root["schema_version"]) != ComponentInvocationReceiptVersion {
		return false
	}
	if !TerminalComponentIds[pyStr(root["component_id"])] {
		return false
	}
	if pyStr(root["request_id"]) == "" || pyStr(root["input_snapshot_id"]) == "" {
		return false
	}
	componentID := pyStr(root["component_id"])
	candidateIDs, candidateIsList := root["candidate_ids"].([]any)
	starterIDs, starterIsList := root["starter_ids"].([]any)
	if !candidateIsList || !starterIsList {
		return false
	}
	switch componentID {
	case "icebreaker_rid":
		if pyStr(root["binding_scope"]) != "all_cards" ||
			len(candidateIDs) > 0 || len(starterIDs) > 0 {
			return false
		}
	case "pa_bridge_fixed_retriever":
		if pyStr(root["binding_scope"]) != "starter" || len(candidateIDs) > 0 {
			return false
		}
		if len(starterIDs) != 0 && !uniqueNonemptyStrings(starterIDs) {
			return false
		}
	default:
		if pyStr(root["binding_scope"]) != "candidate" ||
			!uniqueNonemptyStrings(candidateIDs) || len(starterIDs) > 0 {
			return false
		}
	}
	return true
}

// ComponentInvocationMatchesReceipt 调用与小票的三键对齐（:2832-2843）。
func ComponentInvocationMatchesReceipt(invocation map[string]any,
	componentID string, receipt any) bool {
	receiptMap, isMap := receipt.(map[string]any)
	if !isMap {
		return false
	}
	return pyStr(invocation["component_id"]) == componentID &&
		pyStr(invocation["request_id"]) == pyStr(receiptMap["request_id"]) &&
		pyStr(invocation["input_snapshot_id"]) == pyStr(receiptMap["input_snapshot_id"])
}

// ComponentLineageRejectionReason 组件血缘契约（:2845-2937）。
func ComponentLineageRejectionReason(manifest any, cards []map[string]any) string {
	manifestMap, isMap := manifest.(map[string]any)
	if !isMap {
		return "component_lineage_contract_failed"
	}
	const failure = "component_lineage_contract_failed"
	rawInvocations, isList := manifestMap["component_invocation_receipts"].([]any)
	if !isList || len(rawInvocations) == 0 {
		return failure
	}
	for _, raw := range rawInvocations {
		if !ValidComponentInvocation(raw) {
			return failure
		}
	}
	type invocationKey struct{ componentID, requestID string }
	manifestInvocations := map[invocationKey]map[string]any{}
	var manifestOrder []invocationKey
	for _, raw := range rawInvocations {
		invocation := raw.(map[string]any)
		key := invocationKey{pyStr(invocation["component_id"]),
			pyStr(invocation["request_id"])}
		if _, dup := manifestInvocations[key]; dup {
			return failure
		}
		manifestInvocations[key] = invocation
		manifestOrder = append(manifestOrder, key)
	}
	manifestReceipts, receiptsIsMap := manifestMap["component_receipts"].(map[string]any)
	if !receiptsIsMap {
		return failure
	}
	referencedKeys := map[invocationKey]bool{}
	for componentID, rawReceipt := range manifestReceipts {
		key := invocationKey{componentID, pyStr(payloadMapOrEmpty(rawReceipt)["request_id"])}
		invocation, present := manifestInvocations[key]
		if !present || !ComponentInvocationMatchesReceipt(invocation, componentID, rawReceipt) {
			return failure
		}
		referencedKeys[key] = true
	}
	for _, card := range cards {
		if card == nil {
			return failure
		}
		rawCardInvocations, isList := card["component_invocation_receipts"].([]any)
		if !isList || len(rawCardInvocations) == 0 {
			return failure
		}
		for _, raw := range rawCardInvocations {
			if !ValidComponentInvocation(raw) {
				return failure
			}
		}
		cardInvocations := map[invocationKey]map[string]any{}
		var cardOrder []invocationKey
		for _, raw := range rawCardInvocations {
			invocation := raw.(map[string]any)
			key := invocationKey{pyStr(invocation["component_id"]),
				pyStr(invocation["request_id"])}
			if _, dup := cardInvocations[key]; dup {
				return failure
			}
			cardInvocations[key] = invocation
			cardOrder = append(cardOrder, key)
		}
		expected := map[invocationKey]map[string]any{}
		for _, key := range manifestOrder {
			if ComponentInvocationAppliesToCard(manifestInvocations[key], card) {
				expected[key] = manifestInvocations[key]
			}
		}
		if len(cardInvocations) != len(expected) {
			return failure
		}
		for key, invocation := range cardInvocations {
			expectedInvocation, present := expected[key]
			if !present || !stableJSONEqual(invocation, expectedInvocation) {
				return failure
			}
		}
		cardReceipts, isMap := card["component_receipts"].(map[string]any)
		if !isMap {
			return failure
		}
		if len(cardReceipts) != len(cardInvocations) {
			return failure
		}
		for componentID, rawReceipt := range cardReceipts {
			key := invocationKey{componentID,
				pyStr(payloadMapOrEmpty(rawReceipt)["request_id"])}
			invocation, present := cardInvocations[key]
			if !present || !ComponentInvocationMatchesReceipt(invocation, componentID, rawReceipt) {
				return failure
			}
			referencedKeys[key] = true
		}
	}
	// 被拒列表上的新闻单卡保留从未成卡的常规候选的多样性判定。
	unreferencedAllowed := map[string]bool{"verbatim_web_evidence": true}
	if NewsOnlyPool(manifestMap, cards) {
		unreferencedAllowed["diversity_scorer"] = true
	}
	for _, key := range manifestOrder {
		if !referencedKeys[key] && !unreferencedAllowed[key.componentID] {
			return failure
		}
	}
	return ""
}

// ValidProviderReceipt 厂商小票校验（topic_product.py:2939-3086）。
func ValidProviderReceipt(receipt any) bool {
	root, isMap := receipt.(map[string]any)
	if !isMap {
		return false
	}
	optional := map[string]bool{
		"reasoning_effort": true, "prompt_version": true,
		"reused_for_input_snapshot_id": true,
	}
	for field := range root {
		if !providerReceiptFields[field] && !optional[field] {
			return false
		}
	}
	for field := range providerReceiptFields {
		if _, present := root[field]; !present {
			return false
		}
	}
	stage := pyStr(root["stage"])
	lineage, lineageIsMap := root["lineage"].(map[string]any)
	if stage == "" {
		return false
	}
	if !allCardProviderStages[stage] && !candidateProviderStages[stage] &&
		!starterProviderStages[stage] {
		return false
	}
	if pyStr(root["provider_response_id"]) == "" {
		return false
	}
	if pyStr(root["response_object"]) != "chat.completion" {
		return false
	}
	if pyStr(root["actual_model"]) == "" {
		return false
	}
	if pyStr(root["finish_reason"]) != "stop" {
		return false
	}
	if _, isMap := root["usage"].(map[string]any); !isMap {
		return false
	}
	latency, latencyIsNum := toFloat(root["latency_ms"])
	if !latencyIsNum || math.IsNaN(latency) || math.IsInf(latency, 0) || latency < 0 {
		return false
	}
	if pyStr(root["structured_output_mode"]) != "json_schema_strict" {
		return false
	}
	if !lineageIsMap {
		return false
	}
	for field := range lineage {
		if field != "skipped_slots" && !providerLineageFields[field] {
			return false
		}
	}
	for field := range providerLineageFields {
		if _, present := lineage[field]; !present {
			return false
		}
	}
	if pyStr(lineage["input_snapshot_id"]) == "" {
		return false
	}
	if hasSecretField(root) {
		return false
	}
	// 槽生成器的小票记录它跳过的槽；更早的写入一个都不留。
	if _, hasSkipped := lineage["skipped_slots"]; hasSkipped {
		skipped, isList := lineage["skipped_slots"].([]any)
		if !slotDecisionProviderStages[stage] || !isList {
			return false
		}
		for _, raw := range skipped {
			item, isMap := raw.(map[string]any)
			if !isMap || len(item) != len(skippedSlotFields) {
				return false
			}
			for field := range skippedSlotFields {
				if _, present := item[field]; !present {
					return false
				}
			}
		}
	}
	if reasoningEffort, present := root["reasoning_effort"]; present &&
		pyStr(reasoningEffort) == "" {
		return false
	}
	if promptVersion, present := root["prompt_version"]; present &&
		pyStr(promptVersion) == "" {
		return false
	}
	if reused, present := root["reused_for_input_snapshot_id"]; present {
		if stage != "rid_interest_distiller" || pyStr(reused) == "" ||
			pyStr(reused) == pyStr(lineage["input_snapshot_id"]) {
			return false
		}
	}
	candidateIDs, candidateIsList := lineage["candidate_ids"].([]any)
	starterIDs, starterIsList := lineage["starter_ids"].([]any)
	if !candidateIsList || !starterIsList {
		return false
	}
	requestID := pyStr(lineage["component_request_id"])
	if allCardProviderStages[stage] {
		if pyStr(lineage["binding_scope"]) != "all_cards" ||
			len(candidateIDs) > 0 || len(starterIDs) > 0 {
			return false
		}
	} else if candidateProviderStages[stage] {
		if pyStr(lineage["binding_scope"]) != "candidate" || len(starterIDs) > 0 {
			return false
		}
		allowsEmpty := stage == "v6_recent_candidate_generator" ||
			stage == "v6_project_domain_candidate_generator" ||
			stage == "v6_project_continuation_generator" ||
			stage == "v6_global_interest_generator" ||
			stage == "v6_recent_finalizer"
		if len(candidateIDs) == 0 && !allowsEmpty {
			return false
		}
		if len(candidateIDs) != 0 && !uniqueNonemptyStrings(candidateIDs) {
			return false
		}
	} else {
		if pyStr(lineage["binding_scope"]) != "starter" || len(candidateIDs) > 0 {
			return false
		}
		allowsEmpty := stage == "v6_pa_fixed_retriever"
		if len(starterIDs) == 0 && !allowsEmpty {
			return false
		}
		if len(starterIDs) != 0 && !uniqueNonemptyStrings(starterIDs) {
			return false
		}
	}
	return requestID != ""
}

// ProviderLineageRejectionReason 厂商血缘契约
// （topic_product.py:3087-3189 _provider_lineage_rejection_reason）。
func ProviderLineageRejectionReason(manifest any, cards []map[string]any) string {
	manifestMap, isMap := manifest.(map[string]any)
	if !isMap {
		return "provider_lineage_contract_failed"
	}
	const failure = "provider_lineage_contract_failed"
	manifestReceipts, isList := manifestMap["ark_invocation_receipts"].([]any)
	if !isList || len(manifestReceipts) == 0 {
		return failure
	}
	for _, raw := range manifestReceipts {
		if !ValidProviderReceipt(raw) {
			return failure
		}
	}
	manifestByID := map[string]map[string]any{}
	models := map[string]bool{}
	for _, raw := range manifestReceipts {
		receipt := raw.(map[string]any)
		responseID := pyStr(receipt["provider_response_id"])
		if _, dup := manifestByID[responseID]; dup {
			return failure
		}
		manifestByID[responseID] = receipt
		models[pyStr(receipt["actual_model"])] = true
	}
	if len(models) != 1 {
		return failure
	}
	materialization, materializationIsMap := manifestMap["session_summary_materialization_receipt"].(map[string]any)
	summaryStageRequired := !materializationIsMap ||
		len(pyListStrings(materialization["generated_session_summary_ids"])) > 0
	retainedCandidates := map[string]bool{}
	if retained, ok := manifestMap["retained_candidates"].(map[string]any); ok {
		for candidateID := range retained {
			retainedCandidates[candidateID] = true
		}
	}
	newsLeadID := NewsLeadCardId(manifestMap)
	for _, card := range cards {
		if card == nil {
			return failure
		}
		cardReceipts, isList := card["provider_invocation_receipts"].([]any)
		if !isList || len(cardReceipts) == 0 {
			return failure
		}
		for _, raw := range cardReceipts {
			if !ValidProviderReceipt(raw) {
				return failure
			}
		}
		cardByID := map[string]map[string]any{}
		for _, raw := range cardReceipts {
			receipt := raw.(map[string]any)
			responseID := pyStr(receipt["provider_response_id"])
			if _, dup := cardByID[responseID]; dup {
				return failure
			}
			cardByID[responseID] = receipt
		}
		expectedByID := map[string]map[string]any{}
		for responseID, receipt := range manifestByID {
			if ProviderReceiptAppliesToCard(receipt, card) {
				expectedByID[responseID] = receipt
			}
		}
		if len(cardByID) != len(expectedByID) {
			return failure
		}
		for responseID, receipt := range cardByID {
			expected, present := expectedByID[responseID]
			if !present || !stableJSONEqual(receipt, expected) {
				return failure
			}
		}
		lineage := payloadMapOrEmpty(cardReceipts[0].(map[string]any)["lineage"])
		_ = lineage
		for _, raw := range cardReceipts {
			receipt := raw.(map[string]any)
			lineageMap := payloadMapOrEmpty(receipt["lineage"])
			effective := pyStrOr(receipt["reused_for_input_snapshot_id"],
				pyStr(lineageMap["input_snapshot_id"]))
			if effective != pyStr(card["generation_input_snapshot_id"]) {
				return failure
			}
		}
		componentReceipts, isMap := card["component_receipts"].(map[string]any)
		if !isMap {
			return failure
		}
		stages := map[string]bool{}
		for _, raw := range cardReceipts {
			stages[pyStr(raw.(map[string]any)["stage"])] = true
		}
		category := pyStrOr(card["category"], "")
		var required map[string]bool
		if retainedCandidates[pyStrOr(card["card_id"], "")] {
			required = map[string]bool{}
		} else if pyStrOr(card["card_id"], "") == newsLeadID {
			bundle := payloadMapOrEmpty(card["evidence_bundle"])
			required = newsProviderStagesBySourceKind(pyStrOr(bundle["source_kind"], ""))
		} else {
			required = providerStagesByCategory[category]
			if required == nil {
				required = map[string]bool{}
			}
		}
		for stage, componentID := range providerComponentByStage {
			if _, present := componentReceipts[componentID]; present {
				required[stage] = true
			}
		}
		if summaryStageRequired {
			for stage := range commonCardProviderStages {
				required[stage] = true
			}
		}
		for stage := range required {
			if !stages[stage] {
				return failure
			}
		}
		for stage := range stages {
			if !required[stage] && !commonCardProviderStages[stage] {
				return failure
			}
		}
		for _, raw := range cardReceipts {
			stage := pyStr(raw.(map[string]any)["stage"])
			componentID, known := providerComponentByStage[stage]
			if !known {
				continue
			}
			componentReceipt, present := componentReceipts[componentID]
			if !present {
				continue
			}
			componentMap, isMap := componentReceipt.(map[string]any)
			requestID := pyStr(payloadMapOrEmpty(raw.(map[string]any)["lineage"])["component_request_id"])
			if !isMap || pyStr(componentMap["request_id"]) != requestID {
				return failure
			}
		}
	}
	return ""
}

// TerminalModelBindingRejectionReason 组件与厂商小票绑定同一可信模型
// （topic_product.py:3192-3240 terminal_model_binding_rejection_reason）。
func TerminalModelBindingRejectionReason(manifest any, cards []map[string]any,
	expectedModel string) string {
	if strings.TrimSpace(expectedModel) == "" {
		return "expected_model_contract_failed"
	}
	manifestMap, manifestIsMap := manifest.(map[string]any)
	if !manifestIsMap {
		return "provider_model_binding_contract_failed"
	}
	for _, card := range cards {
		if card == nil {
			return "provider_model_binding_contract_failed"
		}
	}
	rawSummaries, isList := manifestMap["session_summary_artifacts"].([]any)
	if !isList {
		return "session_summary_model_binding_contract_failed"
	}
	for _, item := range rawSummaries {
		summary, err := SessionSummaryArtifactFromPayload(item)
		if err != nil || summary.SummaryProviderID != "ark" ||
			summary.SummaryProviderVersion != expectedModel {
			return "session_summary_model_binding_contract_failed"
		}
	}
	receiptSets := []any{manifestMap["component_receipts"]}
	for _, card := range cards {
		receiptSets = append(receiptSets, card["component_receipts"])
	}
	for _, rawSet := range receiptSets {
		receipts, isMap := rawSet.(map[string]any)
		if !isMap {
			return "component_model_binding_contract_failed"
		}
		for _, rawReceipt := range receipts {
			receipt, isMap := rawReceipt.(map[string]any)
			if !isMap {
				return "component_model_binding_contract_failed"
			}
			binding, isMap := receipt["binding"].(map[string]any)
			if !isMap || pyStr(binding["provider_version"]) != expectedModel {
				return "component_model_binding_contract_failed"
			}
		}
	}
	providerSets := []any{manifestMap["ark_invocation_receipts"]}
	for _, card := range cards {
		providerSets = append(providerSets, card["provider_invocation_receipts"])
	}
	for _, rawSet := range providerSets {
		receipts, isList := rawSet.([]any)
		if !isList {
			return "provider_model_binding_contract_failed"
		}
		for _, raw := range receipts {
			receipt, isMap := raw.(map[string]any)
			if !isMap || pyStr(receipt["actual_model"]) != expectedModel {
				return "provider_model_binding_contract_failed"
			}
		}
	}
	var searchReceipts []any
	for _, receipt := range payloadMapOrEmpty(manifestMap["search_receipts"]) {
		searchReceipts = append(searchReceipts, receipt)
	}
	for _, card := range cards {
		searchReceipts = append(searchReceipts, card["search_receipt"])
	}
	for _, raw := range searchReceipts {
		receipt, isMap := raw.(map[string]any)
		if isMap && pyStr(receipt["provider_id"]) == ArkWebSearchProviderID &&
			pyStr(receipt["actual_model"]) != expectedModel {
			return "search_model_binding_contract_failed"
		}
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stableJSONEqual 稳定 JSON 等值。
func stableJSONEqual(a, b any) bool { return stableJSON(a) == stableJSON(b) }

// uniqueNonemptyStrings 非空去重字符串列表（Python _unique_nonempty_strings）。
func uniqueNonemptyStrings(items []any) bool {
	if len(items) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, raw := range items {
		value := pyStr(raw)
		if value == "" || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

// newsProviderStagesBySourceKind 新闻领衔卡按来源类别的必需阶段
// （topic_product.py _NEWS_PROVIDER_STAGES_BY_SOURCE_KIND）。
func newsProviderStagesBySourceKind(sourceKind string) map[string]bool {
	switch sourceKind {
	case NewsSourceKind:
		// NEWS_SOURCE_KIND: {v6_hot_news_matcher, v6_recent_finalizer}（:602-605）
		return map[string]bool{"v6_hot_news_matcher": true, "v6_recent_finalizer": true}
	case HotListSourceKind:
		return map[string]bool{"v6_hot_news_matcher": true}
	}
	return map[string]bool{}
}

var (
	_ = context.Background
	_ = swarmsession.GetSessionMetadata
)
